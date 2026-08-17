#!/usr/bin/env python3
"""Fail-closed integrity checks for the checked-in third-party license bundle."""

from __future__ import annotations

import argparse
from datetime import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
from typing import Any


REPO = Path(__file__).resolve().parents[1]
MANIFEST_PATH = REPO / "LICENSES" / "manifest.json"
DOCKERFILE_FRONTEND = (
    "docker.io/docker/dockerfile:1.7@"
    "sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e"
)
NODE_BUILD_IMAGE = (
    "docker.io/library/node:22-bookworm-slim@"
    "sha256:d649c27dae7ba0137b3cef5dd75baa422c08dc3d9e3fc0c23dfb172dc3cc6436"
)
GO_BUILD_IMAGE = (
    "docker.io/library/golang:1.26.6-bookworm@"
    "sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36"
)
GO_VERSION = "1.26.6"
GO_MOD_SHA256 = "5986a1fc21c69d3448b5c193182b6780dffa03f9467b6012d1052d536b10dfa4"
GO_SUM_SHA256 = "5c6c5595aea3aa07497aa7e25f8e5a64e702151902e3433446ca176736168ab9"
PACKAGE_LOCK_SHA256 = "8ff3b5eacb566d47fbfe183d3897f15f2251a36de76f3a3ba97aa76e1c89ea0a"
ROLLDOWN_NOTICE_URL = (
    "https://raw.githubusercontent.com/rolldown/rolldown/"
    "v1.1.5/THIRD-PARTY-LICENSE"
)
ROLLDOWN_NOTICE_SHA256 = "a877291d800ed43692f3f9ae09d8e01cc6f7293ad39d43896059c188ffbb8b7c"
REVIEW_DECISION_SCHEMA_VERSION = 1
SHA256_PATTERN = re.compile(r"[0-9a-f]{64}")
RFC3339_UTC_PATTERN = re.compile(
    r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z"
)
REVIEW_PLACEHOLDERS = {
    "example",
    "n/a",
    "none",
    "not reviewed",
    "pending",
    "placeholder",
    "replace me",
    "tbd",
    "todo",
    "unknown",
}

# Reviewed originals for the one supported artifact profile. Keeping this
# allowlist independent from the manifest prevents a rewritten manifest from
# approving a removed or substituted notice by itself.
GO_MODULE_FILES: dict[tuple[str, str], dict[str, str]] = {
    ("github.com/dustin/go-humanize", "v1.0.1"): {
        "LICENSE": "a973b4498c13eb74baa2a8e5c351426a6826f2fcdd909916dbe53ee2e755fd71",
    },
    ("github.com/google/uuid", "v1.6.0"): {
        "LICENSE": "0a8d61ed3cbfd5312326e8126c31ce9c627a283adc99131b56896d29ada04b2d",
    },
    ("github.com/remyoudompheng/bigfft", "v0.0.0-20230129092748-24d4a6f8daec"): {
        "LICENSE": "dd26a7abddd02e2d0aba97805b31f248ef7835d9e10da289b22e3b8ab78b324d",
    },
    ("golang.org/x/image", "v0.45.0"): {
        "LICENSE": "911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad",
        "PATENTS": "96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc",
    },
    ("golang.org/x/sys", "v0.47.0"): {
        "LICENSE": "911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad",
        "PATENTS": "96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc",
    },
    ("golang.org/x/text", "v0.41.0"): {
        "LICENSE": "911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad",
        "PATENTS": "96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc",
    },
    ("modernc.org/libc", "v1.72.0"): {
        "LICENSE": "95ff867eb55a56935fa7492406cfa953fb7c13ca73f4c0a86ae05756b4605600",
        "LICENSE-3RD-PARTY.md": "f597097efe3d97021f89170746bd3a0fb9a8b6fb26b82043ed68a4e0283bee6c",
    },
    ("modernc.org/mathutil", "v1.7.1"): {
        "LICENSE": "bfa9bf72a72ca009fd62a8f84fca3dca67e51d93af96352723646599898b6cf5",
    },
    ("modernc.org/memory", "v1.11.0"): {
        "LICENSE": "59895e669f48f168b6b858358f6005779cdf40a265f7828813061b56af67b496",
        "LICENSE-GO": "2d36597f7117c38b006835ae7f537487207d8ec407aa9d9980794b2030cbc067",
        "LICENSE-MMAP-GO": "c2eba69f20d05414538c3a5df7694dde392e065ff70882e1625e90f5d6659fff",
    },
    ("modernc.org/sqlite", "v1.50.0"): {
        "LICENSE": "c6fe05491a60ae13bcd223088d2705e36dede24e5587226231d2459ada5c4822",
        "SQLITE-LICENSE": "8438c9c89b849131ead81d5435cb97fcf052df5b0b286dda8a2d4c29e6cb3fd0",
    },
}
GO_TOOLCHAIN_FILES = {
    "LICENSE": "911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad",
    "PATENTS": "96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc",
}
NPM_COMPONENTS: dict[tuple[str, str], tuple[str, dict[str, str]]] = {
    ("react", "19.2.7"): (
        "browser-runtime",
        {"LICENSE": "da6d3703ed11cbe42bd212c725957c98da23cbff1998c05fa4b3d976d1a58e93"},
    ),
    ("react-dom", "19.2.7"): (
        "browser-runtime",
        {"LICENSE": "da6d3703ed11cbe42bd212c725957c98da23cbff1998c05fa4b3d976d1a58e93"},
    ),
    ("scheduler", "0.27.0"): (
        "browser-runtime",
        {"LICENSE": "da6d3703ed11cbe42bd212c725957c98da23cbff1998c05fa4b3d976d1a58e93"},
    ),
    ("vite", "8.1.4"): (
        "browser-injected-runtime",
        {"LICENSE.md": "b1d741c26b53de1bbc0d4d7d3365b79888f9fe511527544a8a7b8e24dec43147"},
    ),
    ("rolldown", "1.1.5"): (
        "browser-injected-modulepreload-runtime",
        {
            "LICENSE": "23ecfff35a5a2e80d92142f75228912c3b1abc4b5a8337a821ff4397e2f9f734",
            "THIRD-PARTY-LICENSE": ROLLDOWN_NOTICE_SHA256,
        },
    ),
}


class VerificationError(RuntimeError):
    pass


def sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def canonical_json_sha256(value: Any) -> str:
    encoded = json.dumps(
        value,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")
    return hashlib.sha256(encoded).hexdigest()


def meaningful_review_text(value: Any) -> bool:
    if not isinstance(value, str):
        return False
    text = value.strip()
    if (
        len(text) < 3
        or len(text) > 1000
        or any(ord(character) < 32 for character in text)
    ):
        return False
    normalized = re.sub(r"[_-]+", " ", text.casefold()).strip(" .:;!?")
    return not any(
        normalized == placeholder
        or normalized.startswith(placeholder + " ")
        or normalized.startswith(placeholder + ":")
        for placeholder in REVIEW_PLACEHOLDERS
    )


def validate_reviewed_at(value: Any) -> None:
    if not isinstance(value, str) or RFC3339_UTC_PATTERN.fullmatch(value) is None:
        raise VerificationError("human-review reviewedAt must be RFC3339 UTC using Z")
    try:
        datetime.fromisoformat(value[:-1] + "+00:00")
    except ValueError as exc:
        raise VerificationError("human-review reviewedAt is not a real UTC date") from exc


def load_strict_json(path: Path) -> Any:
    def reject_duplicate_keys(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
        result: dict[str, Any] = {}
        for key, value in pairs:
            if key in result:
                raise VerificationError(f"duplicate JSON key in human-review decision: {key}")
            result[key] = value
        return result

    try:
        return json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=reject_duplicate_keys)
    except json.JSONDecodeError as exc:
        raise VerificationError(f"invalid human-review decision JSON: {exc}") from exc


def verify_review_decision(manifest: dict[str, Any], path: Path) -> None:
    decision = load_strict_json(path)
    top_level_keys = {
        "schemaVersion",
        "artifactProfileSha256",
        "reviewedBy",
        "reviewedAt",
        "components",
    }
    if not isinstance(decision, dict) or set(decision) != top_level_keys:
        raise VerificationError("unexpected human-review decision schema")
    if decision["schemaVersion"] != REVIEW_DECISION_SCHEMA_VERSION:
        raise VerificationError("unsupported human-review decision schemaVersion")
    expected_profile_hash = canonical_json_sha256(manifest["artifactProfile"])
    if decision["artifactProfileSha256"] != expected_profile_hash:
        raise VerificationError("human-review decision artifact profile hash changed")
    if not meaningful_review_text(decision["reviewedBy"]):
        raise VerificationError("human-review reviewedBy is missing or a placeholder")
    validate_reviewed_at(decision["reviewedAt"])

    manifest_components = {component["id"]: component for component in manifest["components"]}
    reviewed_components = decision["components"]
    if not isinstance(reviewed_components, list) or not reviewed_components:
        raise VerificationError("human-review decision components are missing")
    if any(not isinstance(component, dict) for component in reviewed_components):
        raise VerificationError("human-review decision contains a non-object component")
    reviewed_by_id = {component.get("id"): component for component in reviewed_components}
    if len(reviewed_by_id) != len(reviewed_components) or None in reviewed_by_id:
        raise VerificationError("human-review decision component ids are missing or duplicated")
    if set(reviewed_by_id) != set(manifest_components):
        raise VerificationError(
            "human-review decision component set differs from the manifest; "
            f"missing={sorted(set(manifest_components) - set(reviewed_by_id))}, "
            f"unexpected={sorted(set(reviewed_by_id) - set(manifest_components))}"
        )

    component_keys = {"id", "decision", "licenseConclusion", "obligations", "files"}
    file_keys = {"path", "sha256"}
    for component_id, manifest_component in manifest_components.items():
        reviewed = reviewed_by_id[component_id]
        if set(reviewed) != component_keys:
            raise VerificationError(f"unexpected human-review component schema: {component_id}")
        if reviewed["decision"] != "approved":
            raise VerificationError(f"human-review decision is not approved: {component_id}")
        if not meaningful_review_text(reviewed["licenseConclusion"]):
            raise VerificationError(
                f"human-review licenseConclusion is missing or a placeholder: {component_id}"
            )
        obligations = reviewed["obligations"]
        if (
            not isinstance(obligations, list)
            or not obligations
            or any(not meaningful_review_text(item) for item in obligations)
            or len(set(obligations)) != len(obligations)
        ):
            raise VerificationError(
                f"human-review obligations are missing, duplicated, or placeholders: {component_id}"
            )

        reviewed_files = reviewed["files"]
        if not isinstance(reviewed_files, list):
            raise VerificationError(f"human-review files are missing: {component_id}")
        if any(
            not isinstance(record, dict) or set(record) != file_keys
            for record in reviewed_files
        ):
            raise VerificationError(f"unexpected human-review file schema: {component_id}")
        reviewed_files_by_path = {record["path"]: record for record in reviewed_files}
        if len(reviewed_files_by_path) != len(reviewed_files):
            raise VerificationError(f"human-review file paths are duplicated: {component_id}")
        expected_files = {
            record["path"]: record["sha256"] for record in manifest_component["files"]
        }
        if set(reviewed_files_by_path) != set(expected_files):
            raise VerificationError(
                f"human-review file set differs from the manifest: {component_id}"
            )
        for file_path, expected_hash in expected_files.items():
            if reviewed_files_by_path[file_path]["sha256"] != expected_hash:
                raise VerificationError(
                    f"human-review file hash differs from the manifest: {file_path}"
                )


def load_manifest() -> dict[str, Any]:
    try:
        manifest = json.loads(MANIFEST_PATH.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise VerificationError(f"cannot read {MANIFEST_PATH}: {exc}") from exc
    if not isinstance(manifest, dict) or manifest.get("schemaVersion") != 1:
        raise VerificationError("unsupported or missing license manifest schemaVersion")
    return manifest


def safe_repo_path(value: str) -> Path:
    if not isinstance(value, str) or not value.startswith("LICENSES/"):
        raise VerificationError(f"invalid bundled file path: {value!r}")
    candidate = (REPO / value).resolve()
    licenses_root = (REPO / "LICENSES").resolve()
    if candidate == licenses_root or licenses_root not in candidate.parents:
        raise VerificationError(f"bundled path escapes LICENSES: {value}")
    return candidate


def load_package_lock() -> dict[str, Any]:
    try:
        lock = json.loads((REPO / "web-v2" / "package-lock.json").read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise VerificationError(f"cannot read package-lock: {exc}") from exc
    if not isinstance(lock, dict) or not isinstance(lock.get("packages"), dict):
        raise VerificationError("package-lock has no packages map")
    return lock


def expected_artifact_sets(manifest: dict[str, Any]) -> None:
    """Require the manifest to describe exactly the reviewed artifact profile."""

    try:
        go_profile = manifest["artifactProfile"]["go"]
        binary_linkage = go_profile["binaryLinkage"]
        if {entry["package"] for entry in binary_linkage} != {
            "cmd/bmanga-go",
            "cmd/bmanga-scan",
        } or len(binary_linkage) != 2:
            raise VerificationError("manifest must describe both shipped Go binaries exactly once")
        expected_modules = set(GO_MODULE_FILES)
        for binary in binary_linkage:
            modules = {
                (item["module"], item["version"])
                for item in binary["externalModules"]
            }
            if modules != expected_modules or len(binary["externalModules"]) != len(expected_modules):
                raise VerificationError(
                    f"{binary['package']} does not match the reviewed external-module set"
                )

        web_profile = manifest["artifactProfile"]["web"]
        production = {
            (item["name"], item["version"])
            for item in web_profile["productionPackages"]
        }
        injected = {
            (item["name"], item["version"])
            for item in web_profile["injectedBuildRuntime"]
        }
        if production != {
            ("react", "19.2.7"),
            ("react-dom", "19.2.7"),
            ("scheduler", "0.27.0"),
        } or len(web_profile["productionPackages"]) != 3:
            raise VerificationError("manifest production npm set differs from the reviewed bundle")
        if injected != {("vite", "8.1.4"), ("rolldown", "1.1.5")} or len(
            web_profile["injectedBuildRuntime"]
        ) != 2:
            raise VerificationError("manifest injected npm runtime set differs from the reviewed bundle")
    except (KeyError, TypeError) as exc:
        raise VerificationError(f"manifest artifact profile is incomplete: {exc}") from exc


def verify_source_mappings(manifest: dict[str, Any], lock: dict[str, Any]) -> set[Path]:
    """Validate every component, filename, reviewed hash, and source locator."""

    components = manifest.get("components")
    if not isinstance(components, list) or not components:
        raise VerificationError("manifest components are missing")
    if any(not isinstance(component, dict) for component in components):
        raise VerificationError("manifest contains a non-object component")
    by_id = {component.get("id"): component for component in components}
    if len(by_id) != len(components) or None in by_id:
        raise VerificationError("component ids are missing or duplicated")

    expected: dict[str, dict[str, Any]] = {
        f"go-toolchain-{GO_VERSION}": {
            "name": "Go toolchain and standard library",
            "version": GO_VERSION,
            "roles": ["bmanga-go", "bmanga-scan"],
            "files": GO_TOOLCHAIN_FILES,
            "prefix": f"LICENSES/go/toolchain-go{GO_VERSION}",
            "source": lambda filename: {
                "kind": "go-toolchain",
                "locator": f"go{GO_VERSION}/{filename}",
                "verifiedLocalToolchain": f"go{GO_VERSION}",
            },
        }
    }
    for (module, version), files in GO_MODULE_FILES.items():
        expected[f"go-module:{module}@{version}"] = {
            "name": module,
            "version": version,
            "roles": ["bmanga-go", "bmanga-scan"],
            "files": files,
            "prefix": f"LICENSES/go/{module}@{version}",
            "source": lambda filename, module=module, version=version: {
                "kind": "go-module-cache",
                "locator": f"{module}@{version}/{filename}",
                "moduleZipAuthenticatedBy": "go.sum",
            },
        }
    for (name, version), (role, files) in NPM_COMPONENTS.items():
        package = lock["packages"].get(f"node_modules/{name}")
        if not isinstance(package, dict) or package.get("version") != version:
            raise VerificationError(f"package-lock does not resolve reviewed {name}@{version}")

        def npm_source(filename: str, name: str = name, package: dict[str, Any] = package) -> dict[str, Any]:
            if name == "rolldown" and filename == "THIRD-PARTY-LICENSE":
                return {
                    "kind": "fixed-upstream-tag",
                    "locator": ROLLDOWN_NOTICE_URL,
                    "reason": "the npm package LICENSE links to this file but omits it",
                }
            return {
                "kind": "npm-ci-install",
                "locator": f"node_modules/{name}/{filename}",
                "lockIntegrity": package.get("integrity"),
                "lockResolved": package.get("resolved"),
            }

        expected[f"npm:{name}@{version}"] = {
            "name": name,
            "version": version,
            "roles": [role],
            "files": files,
            "prefix": f"LICENSES/npm/{name}@{version}",
            "source": npm_source,
        }

    if set(by_id) != set(expected):
        raise VerificationError(
            "component mapping differs from the reviewed profile; "
            f"missing={sorted(set(expected) - set(by_id))}, "
            f"unexpected={sorted(set(by_id) - set(expected))}"
        )

    referenced: set[Path] = set()
    component_keys = {"id", "name", "version", "artifactRoles", "reviewStatus", "files"}
    reviewed_component_keys = component_keys | {"reviewEvidence"}
    review_evidence_keys = {"decisionFile", "sha256"}
    record_keys = {"path", "bytes", "sha256", "source"}
    review_references: set[tuple[Path, str]] = set()
    for component_id, spec in expected.items():
        component = by_id[component_id]
        review_status = component.get("reviewStatus")
        expected_keys = reviewed_component_keys if review_status == "human-reviewed" else component_keys
        if set(component) != expected_keys:
            raise VerificationError(f"unexpected component schema: {component_id}")
        if (
            component["name"] != spec["name"]
            or component["version"] != spec["version"]
            or component["artifactRoles"] != spec["roles"]
            or review_status not in {"pending-human-review", "human-reviewed"}
        ):
            raise VerificationError(f"component metadata changed: {component_id}")
        if review_status == "human-reviewed":
            evidence = component["reviewEvidence"]
            if not isinstance(evidence, dict) or set(evidence) != review_evidence_keys:
                raise VerificationError(f"invalid human-review evidence: {component_id}")
            decision_file = evidence.get("decisionFile")
            decision_hash = evidence.get("sha256")
            if (
                not isinstance(decision_file, str)
                or not decision_file.startswith("LICENSES/reviews/")
                or not decision_file.endswith(".json")
                or not isinstance(decision_hash, str)
                or SHA256_PATTERN.fullmatch(decision_hash) is None
            ):
                raise VerificationError(f"invalid human-review evidence: {component_id}")
            decision_path = safe_repo_path(decision_file)
            if not decision_path.is_file():
                raise VerificationError(f"human-review decision is missing: {component_id}")
            review_references.add((decision_path, decision_hash))
        records = component["files"]
        if not isinstance(records, list) or len(records) != len(spec["files"]):
            raise VerificationError(f"license file set changed: {component_id}")
        records_by_path = {
            record.get("path"): record
            for record in records
            if isinstance(record, dict)
        }
        expected_paths = {
            f"{spec['prefix']}/{filename}" for filename in spec["files"]
        }
        if len(records_by_path) != len(records) or set(records_by_path) != expected_paths:
            raise VerificationError(f"license file paths changed: {component_id}")
        for filename, expected_hash in spec["files"].items():
            expected_path = f"{spec['prefix']}/{filename}"
            record = records_by_path[expected_path]
            if set(record) != record_keys:
                raise VerificationError(f"unexpected file-record schema: {expected_path}")
            if record["sha256"] != expected_hash:
                raise VerificationError(f"reviewed license hash changed in manifest: {expected_path}")
            if record["source"] != spec["source"](filename):
                raise VerificationError(f"license source mapping changed: {expected_path}")
            path = safe_repo_path(expected_path)
            if not path.is_file():
                raise VerificationError(f"bundled license file is missing: {expected_path}")
            if path.stat().st_size != record["bytes"]:
                raise VerificationError(f"bundled license size changed: {expected_path}")
            if sha256(path) != expected_hash:
                raise VerificationError(f"bundled license hash changed: {expected_path}")
            referenced.add(path)

    review_statuses = {component["reviewStatus"] for component in components}
    if review_references:
        if review_statuses != {"human-reviewed"}:
            raise VerificationError("human-review transition must cover every component")
        if len(review_references) != 1:
            raise VerificationError("all components must reference one human-review decision")
        decision_path, expected_decision_hash = next(iter(review_references))
        if sha256(decision_path) != expected_decision_hash:
            raise VerificationError("human-review decision file hash changed")
        verify_review_decision(manifest, decision_path)
        referenced.add(decision_path)
    return referenced


def verify_integrity(manifest: dict[str, Any]) -> None:
    readiness = manifest.get("releaseReadiness")
    if not isinstance(readiness, dict) or not isinstance(readiness.get("ready"), bool):
        raise VerificationError("releaseReadiness.ready must be a boolean")
    blockers = readiness.get("blockingReasons")
    if not isinstance(blockers, list):
        raise VerificationError("release readiness blockers are missing")

    profile = manifest.get("artifactProfile")
    if not isinstance(profile, dict):
        raise VerificationError("artifactProfile is missing")
    go_profile = profile.get("go", {})
    if (go_profile.get("goos"), go_profile.get("goarch"), go_profile.get("cgoEnabled")) != (
        "linux",
        "amd64",
        False,
    ):
        raise VerificationError("license bundle must remain scoped to linux/amd64 CGO_ENABLED=0")
    if go_profile.get("version") != GO_VERSION:
        raise VerificationError(f"license bundle must remain scoped to Go {GO_VERSION}")
    go_mod = (REPO / "go.mod").read_bytes()
    if re.search(rb"(?m)^\s*replace(?:\s|\()", go_mod):
        raise VerificationError("go.mod replace directives require a new provenance review")
    if sha256(REPO / "go.mod") != GO_MOD_SHA256 or go_profile.get("goModSha256") != GO_MOD_SHA256:
        raise VerificationError("go.mod changed without regenerating the license bundle")
    if sha256(REPO / "go.sum") != GO_SUM_SHA256 or go_profile.get("goSumSha256") != GO_SUM_SHA256:
        raise VerificationError("go.sum changed without regenerating the license bundle")
    expected_artifact_sets(manifest)

    web_profile = profile.get("web", {})
    lock_value = web_profile.get("packageLock")
    if lock_value != "web-v2/package-lock.json":
        raise VerificationError("unexpected package-lock path in manifest")
    if (
        sha256(REPO / lock_value) != PACKAGE_LOCK_SHA256
        or web_profile.get("packageLockSha256") != PACKAGE_LOCK_SHA256
    ):
        raise VerificationError("package-lock changed without regenerating the license bundle")
    lock = load_package_lock()

    container = profile.get("container", {})
    if container.get("dockerfileFrontend") != DOCKERFILE_FRONTEND:
        raise VerificationError("manifest does not pin the reviewed Dockerfile frontend")
    if container.get("buildImages") != {"node": NODE_BUILD_IMAGE, "go": GO_BUILD_IMAGE}:
        raise VerificationError("manifest does not pin the reviewed Node and Go build images")
    if container.get("runtimeBase") != "scratch" or container.get("user") != "65532:65532":
        raise VerificationError("manifest does not describe the reviewed scratch runtime")
    if container.get("buildTarget") != {
        "targetplatform": "linux/amd64",
        "targetos": "linux",
        "targetarch": "amd64",
        "enforcement": "Dockerfile pre-build guard",
    }:
        raise VerificationError("manifest does not fail closed to the reviewed linux/amd64 target")
    dockerfile = (REPO / "Dockerfile").read_text(encoding="utf-8")
    if dockerfile.splitlines()[0] != f"# syntax={DOCKERFILE_FRONTEND}":
        raise VerificationError("Dockerfile frontend is not pinned to the reviewed digest")
    for stage, image in (("web-build", NODE_BUILD_IMAGE), ("go-build", GO_BUILD_IMAGE)):
        if dockerfile.count(f"FROM --platform=$BUILDPLATFORM {image} AS {stage}") != 1:
            raise VerificationError(f"Dockerfile {stage} image differs from the reviewed digest")
    if re.search(r"(?m)^ARG\s+(?:NODE_IMAGE|GO_IMAGE)(?:=|\s|$)", dockerfile):
        raise VerificationError("Dockerfile build images must not be build-arg overrideable")
    final_stage = dockerfile.rsplit("\nFROM ", 1)[-1]
    for marker in (
        "scratch",
        "USER 65532:65532",
        "COPY --chown=65532:65532 LICENSES /app/LICENSES",
    ):
        if marker not in final_stage:
            raise VerificationError(f"Dockerfile final stage is missing: {marker}")
    if "RUNTIME_IMAGE" in dockerfile or "distroless" in dockerfile.casefold():
        raise VerificationError("Dockerfile reintroduced an unreviewed runtime base")
    for marker in (
        "ARG TARGETPLATFORM",
        "ARG TARGETOS",
        "ARG TARGETARCH",
        '"${TARGETPLATFORM}" != "linux/amd64"',
        '"${TARGETOS}" != "linux"',
        '"${TARGETARCH}" != "amd64"',
    ):
        if marker not in dockerfile:
            raise VerificationError(f"Dockerfile target guard is missing: {marker}")
    if (
        dockerfile.count('"${TARGETPLATFORM}" != "linux/amd64"') != 2
        or dockerfile.count('"${TARGETOS}" != "linux"') != 2
        or dockerfile.count('"${TARGETARCH}" != "amd64"') != 2
    ):
        raise VerificationError("both Docker build stages must reject targets outside linux/amd64")
    for marker in ('actual="$(go env GOVERSION)"', '"${actual}" != "go1.26.6"'):
        if dockerfile.count(marker) != 1:
            raise VerificationError(f"Dockerfile Go toolchain guard is missing or ambiguous: {marker}")
    target_build = 'GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" CGO_ENABLED=0 go build'
    if dockerfile.count(target_build) != 2:
        raise VerificationError("both shipped Go binaries must use the guarded TARGETOS/TARGETARCH")
    compose = (REPO / "compose.yaml").read_text(encoding="utf-8")
    if re.search(r"(?mi)^\s*init\s*:", compose):
        raise VerificationError("Compose injects an unbundled host docker-init helper")
    if sum(row.strip() == "platform: linux/amd64" for row in compose.splitlines()) != 2:
        raise VerificationError("both Compose services must select the reviewed linux/amd64 platform")
    dockerignore = (REPO / ".dockerignore").read_text(encoding="utf-8")
    for marker in ("!LICENSES/", "!LICENSES/**"):
        if marker not in dockerignore.splitlines():
            raise VerificationError(f"Docker build context excludes the license bundle: {marker}")

    referenced = verify_source_mappings(manifest, lock)

    pending_review = any(
        component.get("reviewStatus") != "human-reviewed"
        for component in manifest.get("components", [])
        if isinstance(component, dict)
    )
    expected_blockers = (
        [
            {
                "code": "human-license-review-pending",
                "detail": "original texts and component mappings require human review",
            }
        ]
        if pending_review
        else []
    )
    if blockers != expected_blockers or readiness["ready"] is pending_review:
        raise VerificationError(
            "release readiness does not match the component human-review evidence"
        )

    actual_files = {
        path.resolve()
        for path in (REPO / "LICENSES").rglob("*")
        if path.is_file() and path.name not in {"manifest.json", "README.md"}
    }
    if referenced != actual_files:
        raise VerificationError(
            "LICENSES contains unmapped or missing files; "
            f"unmapped={[str(p.relative_to(REPO)) for p in sorted(actual_files - referenced)]}, "
            f"missing={[str(p.relative_to(REPO)) for p in sorted(referenced - actual_files)]}"
        )


def run(*args: str, env: dict[str, str] | None = None) -> str:
    completed = subprocess.run(
        args,
        cwd=REPO,
        env=env,
        check=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        encoding="utf-8",
    )
    return completed.stdout.strip()


def linked_modules(metadata: str) -> set[tuple[str, str]]:
    modules: set[tuple[str, str]] = set()
    for line in metadata.splitlines():
        fields = line.strip().split("\t")
        if len(fields) >= 3 and fields[0] == "dep":
            modules.add((fields[1], fields[2]))
    return modules


def binary_go_version(metadata: str) -> str:
    first_line = metadata.splitlines()[0] if metadata.splitlines() else ""
    return first_line.rsplit(": ", 1)[-1]


def verify_original_copy(original: Path, record: dict[str, Any]) -> None:
    bundled = safe_repo_path(record["path"])
    if not original.is_file():
        raise VerificationError(f"original license source is missing: {original}")
    if sha256(original) != record["sha256"] or original.read_bytes() != bundled.read_bytes():
        raise VerificationError(f"bundled license differs from original source: {record['path']}")


def verify_go_linkage(manifest: dict[str, Any]) -> None:
    profile = manifest["artifactProfile"]["go"]
    go_version = run("go", "version")
    version_match = re.search(r"\bgo version go([^\s]+)", go_version)
    if version_match is None or version_match.group(1) != profile["version"]:
        raise VerificationError(
            f"Go {profile['version']} is required for linkage verification; found {go_version}"
        )
    expected_by_package = {
        entry["package"]: {
            (item["module"], item["version"])
            for item in entry["externalModules"]
        }
        for entry in profile["binaryLinkage"]
    }
    build_env = os.environ.copy()
    build_env.update(
        {
            "GOOS": "linux",
            "GOARCH": "amd64",
            "CGO_ENABLED": "0",
            "GOWORK": "off",
            "GOFLAGS": "",
        }
    )
    if run("go", "mod", "verify", env=build_env) != "all modules verified":
        raise VerificationError("Go module cache verification did not complete cleanly")
    goroot = Path(run("go", "env", "GOROOT", env=build_env))
    modcache = Path(run("go", "env", "GOMODCACHE", env=build_env))
    with tempfile.TemporaryDirectory(prefix="bmanga-license-verify-") as temp:
        for package, expected in expected_by_package.items():
            output = str(Path(temp) / Path(package).name)
            run(
                "go",
                "build",
                "-buildvcs=false",
                "-mod=readonly",
                "-trimpath",
                "-ldflags=-s -w -buildid=",
                "-o",
                output,
                f"./{package}",
                env=build_env,
            )
            metadata = run("go", "version", "-m", output, env=build_env)
            if binary_go_version(metadata) != f"go{GO_VERSION}":
                raise VerificationError(
                    f"{package} binary toolchain changed: {binary_go_version(metadata)}"
                )
            actual = linked_modules(metadata)
            if actual != expected:
                raise VerificationError(
                    f"{package} linked modules changed; missing={sorted(expected - actual)}, "
                    f"unexpected={sorted(actual - expected)}"
                )

    by_id = {component["id"]: component for component in manifest["components"]}
    toolchain = by_id[f"go-toolchain-{GO_VERSION}"]
    for record in toolchain["files"]:
        verify_original_copy(goroot / Path(record["path"]).name, record)
    for module, version in GO_MODULE_FILES:
        component = by_id[f"go-module:{module}@{version}"]
        source_root = modcache / f"{module}@{version}"
        for record in component["files"]:
            verify_original_copy(source_root / Path(record["path"]).name, record)


def npm_dependency_names(node: dict[str, Any]) -> set[tuple[str, str]]:
    found: set[tuple[str, str]] = set()
    for name, dependency in (node.get("dependencies") or {}).items():
        if not isinstance(dependency, dict):
            continue
        version = dependency.get("version")
        if isinstance(version, str):
            found.add((name, version))
        found.update(npm_dependency_names(dependency))
    return found


def verify_web_sources(manifest: dict[str, Any]) -> None:
    npm = shutil.which("npm")
    if npm is None:
        raise VerificationError("npm is required for web source verification")
    try:
        tree = json.loads(run(npm, "--prefix", "web-v2", "ls", "--omit=dev", "--all", "--json"))
    except (subprocess.CalledProcessError, json.JSONDecodeError) as exc:
        raise VerificationError(f"cannot inspect installed production npm tree: {exc}") from exc
    expected = {
        (item["name"], item["version"])
        for item in manifest["artifactProfile"]["web"]["productionPackages"]
    }
    actual = npm_dependency_names(tree)
    if actual != expected:
        raise VerificationError(
            f"production npm tree changed; missing={sorted(expected - actual)}, "
            f"unexpected={sorted(actual - expected)}"
        )

    by_id = {component["id"]: component for component in manifest["components"]}
    for name, version in expected | {
        (item["name"], item["version"])
        for item in manifest["artifactProfile"]["web"]["injectedBuildRuntime"]
    }:
        component = by_id[f"npm:{name}@{version}"]
        for record in component["files"]:
            source = record["source"]
            if source["kind"] == "fixed-upstream-tag":
                if not (
                    name == "rolldown"
                    and version == "1.1.5"
                    and Path(record["path"]).name == "THIRD-PARTY-LICENSE"
                    and source["locator"] == ROLLDOWN_NOTICE_URL
                    and record["sha256"] == ROLLDOWN_NOTICE_SHA256
                ):
                    raise VerificationError("unexpected fixed-upstream npm source exception")
                continue
            if source["kind"] != "npm-ci-install":
                raise VerificationError(f"unsupported npm license source kind: {source['kind']}")
            local = REPO / "web-v2" / source["locator"]
            if not local.is_file() or sha256(local) != record["sha256"]:
                raise VerificationError(f"installed npm license source changed: {source['locator']}")


def verify_release_readiness(manifest: dict[str, Any]) -> None:
    readiness = manifest["releaseReadiness"]
    reasons = readiness["blockingReasons"]
    if readiness["ready"] is True and not reasons:
        return
    summary = "; ".join(f"{item['code']}: {item['detail']}" for item in reasons)
    raise VerificationError(f"release is blocked: {summary or 'readiness is not approved'}")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--verify", action="store_true", help="verify manifest, locks, and all file hashes")
    parser.add_argument("--verify-go-linkage", action="store_true", help="rebuild both linux/amd64 binaries")
    parser.add_argument("--verify-web-sources", action="store_true", help="inspect npm production tree and sources")
    parser.add_argument(
        "--release-readiness",
        action="store_true",
        help="fail while any explicit artifact-release blocker remains",
    )
    args = parser.parse_args()
    if not any(vars(args).values()):
        parser.error("select at least one verification mode")
    try:
        manifest = load_manifest()
        verify_integrity(manifest)
        if args.verify_go_linkage:
            verify_go_linkage(manifest)
        if args.verify_web_sources:
            verify_web_sources(manifest)
        if args.release_readiness:
            verify_release_readiness(manifest)
    except (OSError, KeyError, TypeError, VerificationError, subprocess.CalledProcessError) as exc:
        print(f"third-party license verification failed: {exc}", file=sys.stderr)
        return 1
    print("third-party license bundle verification passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
