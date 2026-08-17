#!/usr/bin/env python3
"""Fail closed when the prospective public Git surface contains private data.

The scanner examines indexed files and non-ignored untracked files. This makes
it useful before the first commit as well as in CI.

An optional project-specific literal dictionary may be supplied with
--privacy-terms-file or BMANGA_PRIVACY_TERMS_FILE. It must be a JSON file
outside the repository with schema
{"schemaVersion": 1, "categories": {"non_sensitive_label": ["literal"]}}.
Category labels are emitted in reports and therefore must not be sensitive.
"""

from __future__ import annotations

import argparse
import hashlib
import ipaddress
import json
import os
import re
import subprocess
import unicodedata
from collections.abc import Mapping, Sequence
from pathlib import Path, PurePosixPath
from urllib.parse import urlsplit


ROOT = Path(__file__).resolve().parents[1]
MAX_TRACKED_FILE_BYTES = 2 * 1024 * 1024
MAX_PRIVACY_TERMS_FILE_BYTES = 1024 * 1024
MAX_PRIVACY_TERMS = 4096
PRIVACY_TERMS_ENV = "BMANGA_PRIVACY_TERMS_FILE"
PRIVACY_CATEGORY_PATTERN = re.compile(r"[a-z][a-z0-9_-]{0,63}")

FindingValue = str | int
Finding = dict[str, FindingValue]
PrivacyRule = tuple[str, str, str]


class PrivacyTermsError(ValueError):
    """A non-sensitive description of an unusable external privacy dictionary."""


RUNTIME_DIRECTORY_NAMES = {
    ".cache",
    ".venv",
    "bin",
    "cache",
    "data",
    "intake-staging",
    "models",
    "node_modules",
    "python-wheels",
    "runs",
    "vendor",
}
SENSITIVE_BASENAMES = {
    "next_chat_prompt.md",
    "project_handoff.md",
    "project_handoff_compact.md",
}
BLOCKED_FILE_SUFFIXES = {
    ".7z",
    ".bak",
    ".cbz",
    ".db",
    ".der",
    ".key",
    ".log",
    ".mobi",
    ".p12",
    ".pfx",
    ".pdf",
    ".pem",
    ".sqlite",
    ".sqlite3",
    ".zip",
}
TEXT_SUFFIXES = {
    "",
    ".css",
    ".dockerignore",
    ".env",
    ".example",
    ".gitignore",
    ".go",
    ".html",
    ".ini",
    ".js",
    ".json",
    ".jsx",
    ".md",
    ".mjs",
    ".ps1",
    ".py",
    ".sh",
    ".sql",
    ".toml",
    ".ts",
    ".tsx",
    ".txt",
    ".vbs",
    ".webmanifest",
    ".xml",
    ".yaml",
    ".yml",
}
TEXT_BASENAMES = {
    ".dockerignore",
    ".gitattributes",
    ".gitignore",
    "dockerfile",
    "go.mod",
    "go.sum",
}

# Unknown outbound hosts are rejected. Adding one requires an explicit review,
# so a forgotten private domain cannot silently enter the public snapshot.
ALLOWED_URL_HOSTS = {
    "127.0.0.1",
    "git-lfs.github.com",
    "github.com",
    "keepachangelog.com",
    "localhost",
    "opencollective.com",
    "raw.githubusercontent.com",
    "registry.npmjs.org",
    "tidelift.com",
    "www.apache.org",
}
ALLOWED_URL_HOST_SUFFIXES = (
    ".example",
    ".example.com",
    ".example.invalid",
    ".invalid",
    ".test",
)


def joined(*parts: str) -> str:
    """Keep scanner signatures from matching their own source literals."""

    return "".join(parts)


def configured_privacy_terms_path(
    explicit: str | None,
    environment: Mapping[str, str] | None = None,
) -> Path | None:
    """Resolve CLI-over-environment selection without reading the dictionary."""

    if explicit is not None:
        if not explicit.strip():
            raise PrivacyTermsError("external privacy dictionary path is empty")
        return Path(explicit)

    value = (environment if environment is not None else os.environ).get(
        PRIVACY_TERMS_ENV
    )
    if value is None or not value.strip():
        return None
    return Path(value)


def reject_duplicate_json_keys(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise PrivacyTermsError(
                "external privacy dictionary contains a duplicate JSON key"
            )
        result[key] = value
    return result


def load_privacy_terms(path: Path, root: Path | None = None) -> tuple[PrivacyRule, ...]:
    """Load literal project-specific privacy terms from an external JSON file.

    Expected schema::

        {"schemaVersion": 1, "categories": {"category_name": ["literal"]}}

    Error messages intentionally omit both the external path and dictionary data.
    """

    try:
        if path.is_symlink():
            raise PrivacyTermsError(
                "external privacy dictionary must be a regular, non-symlink file"
            )
        resolved = path.resolve(strict=True)
        repository_root = (root if root is not None else ROOT).resolve(strict=True)
    except PrivacyTermsError:
        raise
    except (OSError, RuntimeError):
        raise PrivacyTermsError("external privacy dictionary is not readable") from None

    try:
        resolved.relative_to(repository_root)
    except ValueError:
        pass
    else:
        raise PrivacyTermsError(
            "external privacy dictionary must be located outside the repository"
        )

    try:
        if not resolved.is_file():
            raise PrivacyTermsError(
                "external privacy dictionary is not a regular file"
            )
        if resolved.stat().st_size > MAX_PRIVACY_TERMS_FILE_BYTES:
            raise PrivacyTermsError(
                "external privacy dictionary exceeds the size limit"
            )
        payload = json.loads(
            resolved.read_text(encoding="utf-8"),
            object_pairs_hook=reject_duplicate_json_keys,
        )
    except PrivacyTermsError:
        raise
    except (OSError, UnicodeError, json.JSONDecodeError):
        raise PrivacyTermsError(
            "external privacy dictionary is not valid UTF-8 JSON"
        ) from None

    if not isinstance(payload, dict) or set(payload) != {
        "schemaVersion",
        "categories",
    }:
        raise PrivacyTermsError("external privacy dictionary has an invalid schema")
    if type(payload["schemaVersion"]) is not int or payload["schemaVersion"] != 1:
        raise PrivacyTermsError("external privacy dictionary has an unsupported schema")

    categories = payload["categories"]
    if not isinstance(categories, dict) or not categories:
        raise PrivacyTermsError("external privacy dictionary has no categories")

    rules: list[PrivacyRule] = []
    for category, values in categories.items():
        if not isinstance(category, str) or not PRIVACY_CATEGORY_PATTERN.fullmatch(
            category
        ):
            raise PrivacyTermsError(
                "external privacy dictionary contains an invalid category"
            )
        if (
            not isinstance(values, list)
            or not values
            or any(not isinstance(value, str) for value in values)
        ):
            raise PrivacyTermsError(
                "external privacy dictionary contains an invalid term list"
            )

        seen: set[str] = set()
        for value in values:
            if (
                not value
                or value != value.strip()
                or len(value) > 512
                or any(unicodedata.category(char).startswith("C") for char in value)
            ):
                raise PrivacyTermsError(
                    "external privacy dictionary contains an invalid term"
                )
            normalized = unicodedata.normalize("NFKC", value).casefold()
            if normalized in seen:
                raise PrivacyTermsError(
                    "external privacy dictionary contains a duplicate term"
                )
            seen.add(normalized)
            digest = hashlib.sha256(
                f"{category}\0{normalized}".encode("utf-8")
            ).hexdigest()[:12]
            rules.append((category, normalized, f"sha256:{digest}"))
            if len(rules) > MAX_PRIVACY_TERMS:
                raise PrivacyTermsError(
                    "external privacy dictionary exceeds the term limit"
                )

    return tuple(rules)


def add_privacy_findings(
    relative: str,
    text: str,
    privacy_terms: Sequence[PrivacyRule],
    blockers: list[Finding],
) -> None:
    """Add non-disclosing findings for literal external-dictionary matches."""

    if not privacy_terms:
        return
    for line_number, line in enumerate(text.splitlines(), start=1):
        normalized_line = unicodedata.normalize("NFKC", line).casefold()
        for category, normalized_term, short_hash in privacy_terms:
            if normalized_term in normalized_line:
                blockers.append(
                    {
                        "level": "blocker",
                        "key": "project_privacy_term",
                        "category": category,
                        "path": relative,
                        "line": line_number,
                        "short_hash": short_hash,
                    }
                )


def has_privacy_dictionary_schema(text: str) -> bool:
    """Recognize a dictionary accidentally placed on the publishable surface."""

    try:
        payload = json.loads(text)
    except json.JSONDecodeError:
        return False
    return (
        isinstance(payload, dict)
        and type(payload.get("schemaVersion")) is int
        and payload["schemaVersion"] == 1
        and isinstance(payload.get("categories"), dict)
    )


SECRET_PATTERNS = {
    "private_key": re.compile(
        joined("-----BEGIN ", "(?:RSA |EC |OPENSSH |DSA )?", "PRIVATE KEY-----")
    ),
    "github_token": re.compile(joined(r"\bgh", r"[opsu]_[A-Za-z0-9]{24,}\b")),
    "github_fine_grained_token": re.compile(
        joined(r"\bgithub", r"_pat_[A-Za-z0-9_]{40,}\b")
    ),
    "gitlab_token": re.compile(joined(r"\bgl", r"pat-[A-Za-z0-9_-]{20,}\b")),
    "openai_key": re.compile(
        joined(r"\bsk", r"-(?:proj-)?[A-Za-z0-9_-]{20,}\b")
    ),
    "huggingface_token": re.compile(
        joined(r"\bh", r"f_[A-Za-z0-9_-]{20,}\b")
    ),
    "aws_access_key": re.compile(joined(r"\bAK", r"IA[0-9A-Z]{16}\b")),
    "google_api_key": re.compile(joined(r"\bAI", r"za[0-9A-Za-z_-]{32,}\b")),
    "npm_token": re.compile(joined(r"\bnpm", r"_[A-Za-z0-9]{30,}\b")),
    "pypi_token": re.compile(joined(r"\bpypi", r"-[A-Za-z0-9_-]{40,}\b")),
    "sendgrid_key": re.compile(
        joined(r"\bS", r"G\.[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{40,}\b")
    ),
    "telegram_bot_token": re.compile(
        joined(r"\b[0-9]{8,10}", r":[A-Za-z0-9_-]{35,}\b")
    ),
    "slack_token": re.compile(
        joined(r"\bxox", r"[abprs]-[A-Za-z0-9-]{10,}\b")
    ),
    "stripe_live_key": re.compile(
        joined(r"\b[rs]k", r"_live_[A-Za-z0-9]{16,}\b")
    ),
    "jwt": re.compile(
        r"\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b"
    ),
    "authorization_value": re.compile(
        joined(
            r"(?i)\b(?:authorization|proxy-authorization)\b\s*[:=]\s*[\"']?",
            r"(?:bearer|basic)\s+[A-Za-z0-9+/=_-]{12,}",
        )
    ),
    "cookie_value": re.compile(
        joined(
            r"(?i)\b(?:cookie|set-cookie)\b\s*[:=]\s*[\"']?",
            r"[A-Za-z0-9_.-]+=[A-Za-z0-9%+/=_-]{12,}",
        )
    ),
    "credential_assignment": re.compile(
        joined(
            r"(?im)^\s*(?:export\s+)?[A-Z0-9_]*(?:PASSWORD|PASSWD|SECRET|TOKEN|API_KEY|COOKIE)[A-Z0-9_]*\s*[:=]\s*[\"']?",
            r"(?!\$\{|\$\(|<|CHANGE[-_]|REPLACE[-_]|PASTE[-_]|EXAMPLE|YOUR[-_]|\*+|[\"']?\s*$)",
            r"[^\s#\"']{8,}",
        )
    ),
    "credential_literal": re.compile(
        joined(
            r"(?i)\b[A-Za-z_][A-Za-z0-9_]*(?:password|passwd|secret|token|api[_-]?key|apikey|cookie)[A-Za-z0-9_]*\s*[:=]\s*[\"']",
            r"(?!change[-_]|replace[-_]|paste[-_]|example|your[-_]|\*+)",
            r"[^\"'\r\n]{8,}[\"']",
        )
    ),
    "credential_uri": re.compile(
        joined(
            r"(?i)\b(?:postgres(?:ql)?|mysql|mariadb|mongodb(?:\+srv)?|redis|rediss|amqp|amqps)",
            r"://[^\s/:@]+:[^\s/@]+@",
        )
    ),
}

PRIVATE_NETWORK_PATTERNS = {
    "rfc1918_10": re.compile(r"(?<![0-9])10(?:\.[0-9]{1,3}){3}(?![0-9])"),
    "rfc1918_172": re.compile(
        r"(?<![0-9])172\.(?:1[6-9]|2[0-9]|3[01])(?:\.[0-9]{1,3}){2}(?![0-9])"
    ),
    "rfc1918_192": re.compile(
        r"(?<![0-9])192\.168(?:\.[0-9]{1,3}){2}(?![0-9])"
    ),
    "link_local_ipv4": re.compile(
        r"(?<![0-9])169\.254(?:\.[0-9]{1,3}){2}(?![0-9])"
    ),
    "cgnat_ipv4": re.compile(
        r"(?<![0-9])100\.(?:6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7])(?:\.[0-9]{1,3}){2}(?![0-9])"
    ),
    "ipv6_ula": re.compile(r"(?i)(?<![0-9a-f:])f[cd][0-9a-f]{2}:[0-9a-f:]+"),
}

WINDOWS_ABSOLUTE_PATH = re.compile(
    r"(?i)(?<![A-Za-z0-9])[A-Z]:(?:\\+[A-Za-z0-9 _().@#$%&+,\-\[\]{}]+)+"
)
GO_PRINTF_NEWLINE_PATH_FALSE_POSITIVE = re.compile(
    joined(r"q:", r"\\+", r"n%s"), re.IGNORECASE
)
UNC_PATH = re.compile(
    r"(?i)(?<![A-Za-z0-9_.:\\])\\{2,4}[A-Za-z0-9._-]+(?:\\+[A-Za-z0-9 _().@#$%&+,\-\[\]{}]+)+"
)
URL_PATTERN = re.compile(r"https?://[^\s\"'()<>]+", re.IGNORECASE)
LARGE_BASE64 = re.compile(
    r"(?:data:[^;,\s]+;base64,)?[A-Za-z0-9+/]{4096,}={0,2}"
)
PRIVATE_POSIX_PATH = re.compile(
    r"(?<![A-Za-z0-9])/(?:Users|home|volume[0-9]+|mnt)/[A-Za-z0-9._ -]+(?:/[A-Za-z0-9._ @()+,\-]+)*"
)
LOCAL_URI = re.compile(r"(?i)\b(?:file|smb|nfs)://[^\s\"'()<>]+")
LFS_POINTER_PREFIX = "version " + "https://git-lfs.github.com/spec/v1"


def git_bytes(arguments: list[str]) -> bytes:
    result = subprocess.run(
        ["git", *arguments],
        cwd=ROOT,
        check=True,
        stdout=subprocess.PIPE,
    )
    return result.stdout


def indexed_entries() -> list[tuple[str, str, str]]:
    entries: list[tuple[str, str, str]] = []
    output = git_bytes(["ls-files", "--stage", "-z"])
    for record in output.decode("utf-8", errors="strict").split("\0"):
        if not record:
            continue
        header, relative = record.split("\t", 1)
        mode, object_id, stage = header.split(" ", 2)
        if stage != "0":
            entries.append((relative.replace("\\", "/"), "unmerged", object_id))
            continue
        entries.append((relative.replace("\\", "/"), mode, object_id))
    return sorted(entries)


def changed_worktree_paths() -> list[str]:
    paths: set[str] = set()
    for arguments in (
        ["ls-files", "-m", "-z"],
        ["ls-files", "--others", "--exclude-standard", "-z"],
    ):
        output = git_bytes(arguments)
        paths.update(
            value.replace("\\", "/")
            for value in output.decode("utf-8", errors="strict").split("\0")
            if value
        )
    return sorted(paths)


def indexed_blob(object_id: str) -> tuple[int, bytes]:
    size_output = git_bytes(["cat-file", "-s", object_id])
    size = int(size_output.decode("ascii", errors="strict").strip())
    if size > MAX_TRACKED_FILE_BYTES:
        return size, b""
    return size, git_bytes(["cat-file", "blob", object_id])


def add(
    rows: list[Finding],
    level: str,
    key: str,
    path: str,
    detail: str,
) -> None:
    rows.append({"level": level, "key": key, "path": path, "detail": detail})


def is_example_env(path: PurePosixPath) -> bool:
    name = path.name.casefold()
    return name.endswith((".example", ".example.env", ".env.example"))


def validate_path(relative: str, blockers: list[Finding]) -> bool:
    posix = PurePosixPath(relative)
    lowered_parts = tuple(part.casefold() for part in posix.parts)
    name = posix.name.casefold()
    suffix = posix.suffix.casefold()

    if any(part in RUNTIME_DIRECTORY_NAMES for part in lowered_parts[:-1]):
        add(
            blockers,
            "blocker",
            "runtime_path",
            relative,
            "runtime/cache/dependency content is not publishable",
        )
    if name in SENSITIVE_BASENAMES or name.startswith("project_handoff"):
        add(
            blockers,
            "blocker",
            "handoff_file",
            relative,
            "private operational handoff must not be published",
        )
    if name.startswith(".env") and not is_example_env(posix):
        add(
            blockers,
            "blocker",
            "environment_file",
            relative,
            "only placeholder .env examples may be tracked",
        )
    if suffix in BLOCKED_FILE_SUFFIXES:
        add(
            blockers,
            "blocker",
            "blocked_artifact",
            relative,
            f"{suffix or name} is outside the source-only public boundary",
        )
    if any(
        marker in name
        for marker in ("inventory", "credential", "credentials", "secret-backup")
    ):
        add(
            blockers,
            "blocker",
            "sensitive_filename",
            relative,
            "filename suggests private operational data",
        )

    return name in TEXT_BASENAMES or suffix in TEXT_SUFFIXES


def allowed_url_host(hostname: str | None) -> bool:
    if not hostname:
        return False
    host = hostname.rstrip(".").casefold()
    if host in ALLOWED_URL_HOSTS:
        return True
    try:
        address = ipaddress.ip_address(host)
    except ValueError:
        address = None
    if address is not None:
        documentation_ranges = (
            ipaddress.ip_network("192.0.2.0/24"),
            ipaddress.ip_network("198.51.100.0/24"),
            ipaddress.ip_network("203.0.113.0/24"),
            ipaddress.ip_network("2001:db8::/32"),
        )
        return any(address in network for network in documentation_ranges)
    return any(host.endswith(suffix) for suffix in ALLOWED_URL_HOST_SUFFIXES)


def is_test_path(relative: str) -> bool:
    name = PurePosixPath(relative).name.casefold()
    return (
        name.endswith(("_test.go", "_test.py", ".test.mjs"))
        or relative.startswith("web-v2/tests/")
    )


def scan_content(
    relative: str,
    raw: bytes,
    size: int,
    strict_paths: bool,
    blockers: list[Finding],
    warnings: list[Finding],
    privacy_terms: Sequence[PrivacyRule] = (),
) -> None:
    is_text = validate_path(relative, blockers)
    if size > MAX_TRACKED_FILE_BYTES:
        add(
            blockers,
            "blocker",
            "oversized_file",
            relative,
            "tracked file exceeds the reviewed size limit",
        )
        return
    if not is_text or b"\0" in raw:
        add(
            blockers,
            "blocker",
            "binary_or_unknown_file",
            relative,
            "binary and unreviewed file types require an explicit provenance policy",
        )
        return
    try:
        text = raw.decode("utf-8", errors="strict")
    except UnicodeDecodeError:
        add(
            blockers,
            "blocker",
            "invalid_utf8",
            relative,
            "text file is not valid UTF-8",
        )
        return
    if any(unicodedata.category(char) == "Cc" and char not in "\t\r\n" for char in text):
        add(
            blockers,
            "blocker",
            "control_characters",
            relative,
            "text file contains unexpected control characters",
        )
    if text.startswith(LFS_POINTER_PREFIX):
        add(
            blockers,
            "blocker",
            "git_lfs_pointer",
            relative,
            "Git LFS pointers are outside the source-only release boundary",
        )

    if has_privacy_dictionary_schema(text):
        add(
            blockers,
            "blocker",
            "privacy_dictionary_in_repository",
            relative,
            "project privacy dictionaries must remain outside the repository",
        )

    add_privacy_findings(relative, text, privacy_terms, blockers)

    config_like = Path(relative).suffix.casefold() in {
        ".env",
        ".example",
        ".ini",
        ".json",
        ".toml",
        ".yaml",
        ".yml",
    }
    for key, pattern in SECRET_PATTERNS.items():
        if key == "credential_assignment" and not config_like:
            continue
        if pattern.search(text):
            add(
                blockers,
                "blocker",
                key,
                relative,
                "possible credential material",
            )

    path_rows = blockers if strict_paths else warnings
    path_level = "blocker" if strict_paths else "warning"
    for match in WINDOWS_ABSOLUTE_PATH.finditer(text):
        # A Go quoted-value format followed by an escaped newline can resemble
        # a drive path. Exempt only that exact language-level shape.
        if (
            match.start() > 0
            and text[match.start() - 1] == "%"
            and GO_PRINTF_NEWLINE_PATH_FALSE_POSITIVE.fullmatch(match.group(0))
        ):
            continue
        add(
            path_rows,
            path_level,
            "absolute_windows_path",
            relative,
            "host-specific absolute Windows path",
        )
    for match in UNC_PATH.finditer(text):
        add(
            path_rows,
            path_level,
            "unc_path",
            relative,
            "host/share-specific UNC path",
        )
    for match in PRIVATE_POSIX_PATH.finditer(text):
        add(
            path_rows,
            path_level,
            "private_posix_path",
            relative,
            "host-specific user, mount, or NAS path",
        )
    for match in LOCAL_URI.finditer(text):
        add(
            path_rows,
            path_level,
            "local_uri",
            relative,
            "local file or network-share URI",
        )
    for key, pattern in PRIVATE_NETWORK_PATTERNS.items():
        if pattern.search(text):
            add(
                path_rows,
                path_level,
                key,
                relative,
                "private or non-routable network address; use a documentation address",
            )

    if LARGE_BASE64.search(text):
        add(
            blockers,
            "blocker",
            "embedded_base64",
            relative,
            "large embedded payload needs provenance and visual/OCR review",
        )

    for match in URL_PATTERN.finditer(text):
        value = match.group(0).rstrip(".,;:")
        if re.search(
            r"%(?:s|d|\([^)]+\)[a-z])|\$\{|\{\{|\{[A-Za-z_][^}]*\}",
            value,
        ):
            continue
        try:
            parsed = urlsplit(value)
        except ValueError:
            if is_test_path(relative) and value == joined("http", "://[invalid"):
                continue
            add(
                blockers,
                "blocker",
                "invalid_url",
                relative,
                "URL is not structurally valid",
            )
            continue
        if parsed.username or parsed.password:
            add(
                blockers,
                "blocker",
                "url_userinfo",
                relative,
                "URL contains username/password material",
            )
        # Original third-party texts must remain byte-for-byte intact, including
        # historical attribution URLs. Their file set and hashes are enforced
        # separately by check-third-party-licenses.py; do not turn those links
        # into an application-wide outbound-host exception.
        license_evidence = relative.startswith("LICENSES/") and relative not in {
            "LICENSES/manifest.json",
            "LICENSES/README.md",
        }
        if not license_evidence and not allowed_url_host(parsed.hostname):
            add(
                blockers,
                "blocker",
                "unreviewed_url_host",
                relative,
                "outbound URL host is not allowlisted",
            )


def deduplicate(rows: list[Finding]) -> list[Finding]:
    unique = {
        (
            row["level"],
            row["key"],
            row["path"],
            row.get("detail", ""),
            row.get("category", ""),
            row.get("line", -1),
            row.get("short_hash", ""),
        ): row
        for row in rows
    }
    return [unique[key] for key in sorted(unique)]


def scan(
    strict_paths: bool,
    privacy_terms: Sequence[PrivacyRule] = (),
) -> tuple[list[Finding], list[Finding]]:
    blockers: list[Finding] = []
    warnings: list[Finding] = []

    for relative, mode, object_id in indexed_entries():
        if mode not in {"100644", "100755"}:
            add(
                blockers,
                "blocker",
                "unsafe_git_mode",
                relative,
                "symlinks, gitlinks, and unmerged index entries are not allowed",
            )
            continue
        try:
            size, raw = indexed_blob(object_id)
        except (OSError, subprocess.SubprocessError, ValueError) as exc:
            add(
                blockers,
                "blocker",
                "read_error",
                relative,
                f"cannot read staged blob ({type(exc).__name__})",
            )
            continue
        scan_content(
            relative,
            raw,
            size,
            strict_paths,
            blockers,
            warnings,
            privacy_terms,
        )

    for relative in changed_worktree_paths():
        path = ROOT / Path(relative)
        if not path.exists():
            continue
        if path.is_symlink() or not path.is_file():
            add(
                blockers,
                "blocker",
                "unsafe_worktree_type",
                relative,
                "worktree symlinks and special files are not allowed",
            )
            continue
        try:
            size = path.stat().st_size
            raw = b"" if size > MAX_TRACKED_FILE_BYTES else path.read_bytes()
        except OSError as exc:
            add(
                blockers,
                "blocker",
                "read_error",
                relative,
                f"cannot read worktree file ({type(exc).__name__})",
            )
            continue
        scan_content(
            relative,
            raw,
            size,
            strict_paths,
            blockers,
            warnings,
            privacy_terms,
        )

    return deduplicate(blockers), deduplicate(warnings)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--strict-paths",
        action="store_true",
        help="treat host paths and private addresses as blockers",
    )
    parser.add_argument(
        "--json",
        action="store_true",
        help="print machine-readable JSON",
    )
    parser.add_argument(
        "--privacy-terms-file",
        metavar="PATH",
        help=(
            "load an external project privacy dictionary; overrides "
            f"{PRIVACY_TERMS_ENV}"
        ),
    )
    args = parser.parse_args()

    configuration_blockers: list[Finding] = []
    privacy_terms: tuple[PrivacyRule, ...] = ()
    try:
        privacy_terms_path = configured_privacy_terms_path(args.privacy_terms_file)
        if privacy_terms_path is not None:
            privacy_terms = load_privacy_terms(privacy_terms_path)
    except PrivacyTermsError as exc:
        add(
            configuration_blockers,
            "blocker",
            "privacy_terms_configuration",
            "<external>",
            str(exc),
        )

    blockers, warnings = scan(args.strict_paths, privacy_terms)
    blockers = deduplicate([*configuration_blockers, *blockers])
    payload = {
        "ok": not blockers,
        "blockers": blockers,
        "warnings": warnings,
        "summary": {"blockers": len(blockers), "warnings": len(warnings)},
    }
    if args.json:
        print(json.dumps(payload, ensure_ascii=False, indent=2))
    else:
        print(
            f"Publication safety: {len(blockers)} blockers / "
            f"{len(warnings)} warnings"
        )
        for row in blockers:
            if row["key"] == "project_privacy_term":
                print(
                    f"BLOCKER {row['key']} {row['path']}:{row['line']}: "
                    f"category={row['category']} short_hash={row['short_hash']}"
                )
            else:
                print(f"BLOCKER {row['key']} {row['path']}: {row['detail']}")
        for row in warnings:
            print(f"WARNING {row['key']} {row['path']}: {row['detail']}")
    return 1 if blockers else 0


if __name__ == "__main__":
    raise SystemExit(main())
