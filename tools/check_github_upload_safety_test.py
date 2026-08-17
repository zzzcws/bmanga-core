from __future__ import annotations

import importlib.util
import hashlib
import json
import struct
import subprocess
import tempfile
import unittest
import zlib
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).with_name("check-github-upload-safety.py")
SPEC = importlib.util.spec_from_file_location("publication_safety", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
CHECKER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CHECKER)


class PublicationSafetyTest(unittest.TestCase):
    def scan(
        self,
        files: dict[str, str | bytes],
        *,
        strict: bool = True,
        worktree_updates: dict[str, str | bytes] | None = None,
        privacy_terms=(),
    ):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(
                ["git", "init", "--quiet"],
                cwd=root,
                check=True,
            )
            for relative, content in files.items():
                target = root / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                if isinstance(content, bytes):
                    target.write_bytes(content)
                else:
                    target.write_text(content, encoding="utf-8")
            subprocess.run(["git", "add", "."], cwd=root, check=True)
            for relative, content in (worktree_updates or {}).items():
                target = root / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                if isinstance(content, bytes):
                    target.write_bytes(content)
                else:
                    target.write_text(content, encoding="utf-8")
            previous = CHECKER.ROOT
            CHECKER.ROOT = root
            try:
                return CHECKER.scan(strict, privacy_terms)
            finally:
                CHECKER.ROOT = previous

    def load_privacy_terms(self, categories: dict[str, list[str]]):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "privacy-terms.json"
            path.write_text(self.privacy_dictionary_text(categories), encoding="utf-8")
            return CHECKER.load_privacy_terms(path)

    @staticmethod
    def privacy_dictionary_text(categories: dict[str, list[str]]) -> str:
        return json.dumps({"schemaVersion": 1, "categories": categories})

    @staticmethod
    def png_bytes(width: int = 4, height: int = 3) -> bytes:
        def chunk(kind: bytes, data: bytes) -> bytes:
            return (
                struct.pack(">I", len(data))
                + kind
                + data
                + struct.pack(">I", zlib.crc32(kind + data) & 0xFFFFFFFF)
            )

        ihdr = struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0)
        scanlines = b"".join(
            b"\0" + bytes((32, 64, 96, 255)) * width for _ in range(height)
        )
        return (
            CHECKER.PNG_SIGNATURE
            + chunk(b"IHDR", ihdr)
            + chunk(b"IDAT", zlib.compress(scanlines))
            + chunk(b"IEND", b"")
        )

    @staticmethod
    def doc_asset_manifest(
        records: list[tuple[str, bytes, int, int]],
    ) -> str:
        return json.dumps(
            {
                "schemaVersion": CHECKER.DOC_ASSET_SCHEMA_VERSION,
                "assets": [
                    {
                        "path": path,
                        "size": len(raw),
                        "sha256": hashlib.sha256(raw).hexdigest(),
                        "dimensions": {"width": width, "height": height},
                    }
                    for path, raw, width, height in sorted(records)
                ],
            }
        )

    def test_safe_source_and_placeholders_pass(self):
        blockers, warnings = self.scan(
            {
                "README.md": "Documentation: https://example.invalid/project\n",
                "config/app.env.example": "APP_SECRET=CHANGE_ME\n",
                "main.go": "package main\n",
                "web/manifest.webmanifest": '{"name":"Example"}\n',
            }
        )
        self.assertEqual(blockers, [])
        self.assertEqual(warnings, [])

    def test_staged_reviewed_document_png_passes(self):
        path = "docs/assets/home-desktop.png"
        raw = self.png_bytes(1440, 960)
        blockers, warnings = self.scan(
            {
                path: raw,
                CHECKER.DOC_ASSET_MANIFEST: self.doc_asset_manifest(
                    [(path, raw, 1440, 960)]
                ),
            }
        )

        self.assertEqual(blockers, [])
        self.assertEqual(warnings, [])

    def test_untracked_reviewed_document_png_passes(self):
        path = "docs/assets/library-mobile.png"
        raw = self.png_bytes(390, 844)
        blockers, warnings = self.scan(
            {"README.md": "safe staged content\n"},
            worktree_updates={
                path: raw,
                CHECKER.DOC_ASSET_MANIFEST: self.doc_asset_manifest(
                    [(path, raw, 390, 844)]
                ),
            },
        )

        self.assertEqual(blockers, [])
        self.assertEqual(warnings, [])

    def test_extra_and_missing_document_pngs_are_blocked(self):
        reviewed_path = "docs/assets/home-desktop.png"
        extra_path = "docs/assets/unlisted.png"
        missing_path = "docs/assets/missing.png"
        reviewed = self.png_bytes(12, 8)
        extra = self.png_bytes(8, 12)
        missing = self.png_bytes(5, 5)
        blockers, _ = self.scan(
            {
                reviewed_path: reviewed,
                extra_path: extra,
                CHECKER.DOC_ASSET_MANIFEST: self.doc_asset_manifest(
                    [
                        (reviewed_path, reviewed, 12, 8),
                        (missing_path, missing, 5, 5),
                    ]
                ),
            }
        )

        keys_by_path = {(row["key"], row["path"]) for row in blockers}
        self.assertIn(("doc_asset_unreviewed", extra_path), keys_by_path)
        self.assertIn(("doc_asset_missing", missing_path), keys_by_path)

    def test_untracked_document_png_hash_change_is_blocked(self):
        path = "docs/assets/library-desktop.png"
        reviewed = self.png_bytes(16, 9)
        changed = self.png_bytes(16, 10)
        blockers, _ = self.scan(
            {"README.md": "safe staged content\n"},
            worktree_updates={
                path: changed,
                CHECKER.DOC_ASSET_MANIFEST: self.doc_asset_manifest(
                    [(path, reviewed, 16, 9)]
                ),
            },
        )

        keys = {row["key"] for row in blockers}
        self.assertIn("doc_asset_hash_mismatch", keys)
        self.assertIn("doc_asset_dimensions_mismatch", keys)

    def test_document_png_size_hash_and_dimensions_are_exact(self):
        path = "docs/assets/home-desktop.png"
        raw = self.png_bytes(24, 16)
        base_record = {
            "path": path,
            "size": len(raw),
            "sha256": hashlib.sha256(raw).hexdigest(),
            "dimensions": {"width": 24, "height": 16},
        }
        mutations = {
            "doc_asset_size_mismatch": {**base_record, "size": len(raw) + 1},
            "doc_asset_hash_mismatch": {**base_record, "sha256": "0" * 64},
            "doc_asset_dimensions_mismatch": {
                **base_record,
                "dimensions": {"width": 25, "height": 16},
            },
        }
        for expected_key, record in mutations.items():
            with self.subTest(expected_key=expected_key):
                manifest = json.dumps(
                    {"schemaVersion": CHECKER.DOC_ASSET_SCHEMA_VERSION, "assets": [record]}
                )
                blockers, _ = self.scan(
                    {path: raw, CHECKER.DOC_ASSET_MANIFEST: manifest}
                )
                self.assertIn(expected_key, {row["key"] for row in blockers})

    def test_invalid_png_framing_is_blocked_even_when_hash_matches(self):
        path = "docs/assets/home-desktop.png"
        raw = b"not-a-png-but-long-enough-for-a-manifest-record"
        blockers, _ = self.scan(
            {
                path: raw,
                CHECKER.DOC_ASSET_MANIFEST: self.doc_asset_manifest(
                    [(path, raw, 10, 10)]
                ),
            }
        )

        self.assertIn("doc_asset_invalid_png", {row["key"] for row in blockers})

    def test_manifest_rejects_path_escape_and_unsupported_format(self):
        raw = self.png_bytes()
        invalid_paths = (
            "docs/assets/../private.png",
            "docs/assets/reviewed.jpg",
            "docs/assets/nested/reviewed.png",
        )
        for invalid_path in invalid_paths:
            with self.subTest(invalid_path=invalid_path):
                manifest = self.doc_asset_manifest([(invalid_path, raw, 4, 3)])
                blockers, _ = self.scan(
                    {
                        "README.md": "safe\n",
                        CHECKER.DOC_ASSET_MANIFEST: manifest,
                    }
                )
                self.assertIn(
                    "doc_asset_manifest_invalid",
                    {row["key"] for row in blockers},
                )

    def test_binary_outside_reviewed_document_assets_remains_blocked(self):
        blockers, _ = self.scan({"docs/archive.bin": b"\0opaque-binary"})

        self.assertIn("binary_or_unknown_file", {row["key"] for row in blockers})

    def test_secret_shape_is_blocked(self):
        token = "sk" + "-" + "a" * 32
        blockers, _ = self.scan({"config.json": '{"token":"' + token + '"}'})
        self.assertIn("openai_key", {row["key"] for row in blockers})

    def test_runtime_data_path_is_blocked(self):
        blockers, _ = self.scan({"data/catalog.json": "{}"})
        self.assertIn("runtime_path", {row["key"] for row in blockers})

    def test_private_address_and_unknown_host_are_blocked(self):
        private_address = "192" + ".168.42.9"
        private_host = "operator" + ".personal-domain.example.org"
        blockers, _ = self.scan(
            {
                "settings.txt": (
                    f"http://{private_address}:8080\n"
                    f"https://{private_host}/api\n"
                )
            }
        )
        keys = {row["key"] for row in blockers}
        self.assertIn("rfc1918_192", keys)
        self.assertIn("unreviewed_url_host", keys)

    def test_test_file_does_not_bypass_host_path_check(self):
        browser_path = "C:" + "\\\\Program Files\\\\Google\\\\Chrome\\\\Application\\\\chrome.exe"
        blockers, _ = self.scan(
            {"web-v2/tests/browser.test.mjs": f'const browser = "{browser_path}";\n'}
        )
        self.assertIn(
            "absolute_windows_path",
            {row["key"] for row in blockers},
        )

    def test_unreviewed_host_path_is_blocked(self):
        separator = chr(92)
        private_path = "D:" + separator + "OperatorData" + separator + "catalog.sqlite"
        blockers, _ = self.scan(
            {"internal/example_test.go": f'const fixture = "{private_path}"\n'}
        )
        self.assertIn("absolute_windows_path", {row["key"] for row in blockers})

    def test_go_printf_newline_is_not_a_windows_path(self):
        printf_fragment = "%q:" + "\\" + "n%s"
        blockers, _ = self.scan(
            {"internal/query_test.go": f"package query\n// failure: {printf_fragment}\n"}
        )
        self.assertNotIn(
            "absolute_windows_path",
            {row["key"] for row in blockers},
        )

    def test_exact_invalid_url_parser_fixture_is_allowed(self):
        blockers, _ = self.scan(
            {"web-v2/tests/url.test.mjs": 'const value = "http://[invalid";\n'}
        )
        self.assertEqual(blockers, [])

    def test_reviewed_apache_license_host_is_allowed(self):
        blockers, _ = self.scan(
            {
                "LICENSE": (
                    "Apache License, Version 2.0\n"
                    "http://www.apache.org/licenses/\n"
                )
            }
        )
        self.assertEqual(blockers, [])

    def test_reviewed_badge_host_is_allowed(self):
        blockers, _ = self.scan(
            {
                "README.md": (
                    "[![release](https://img.shields.io/badge/release-alpha-blue)]"
                    "(https://github.com/example/project)\n"
                )
            }
        )
        self.assertEqual(blockers, [])

    def test_original_license_text_preserves_attribution_hosts(self):
        attribution = "https" + "://" + "historical-project" + ".example.org/license\n"
        blockers, _ = self.scan(
            {
                "LICENSES/example@1.0/LICENSE": "Original attribution: " + attribution
            }
        )
        self.assertNotIn("unreviewed_url_host", {row["key"] for row in blockers})

    def test_private_posix_path_is_blocked(self):
        private_path = "/" + "home/operator/private/catalog.json"
        blockers, _ = self.scan({"settings.txt": private_path})
        self.assertIn("private_posix_path", {row["key"] for row in blockers})

    def test_local_uri_in_test_source_is_blocked(self):
        local_uri = "file:" + "///" + "/".join(
            ("home", "operator", "private", "catalog.json")
        )
        blockers, _ = self.scan(
            {"web-v2/tests/local-uri.test.mjs": f'const value = "{local_uri}";\n'}
        )
        self.assertIn("local_uri", {row["key"] for row in blockers})

    def test_staged_secret_cannot_be_hidden_by_safe_worktree(self):
        token = "github" + "_pat_" + "a" * 48
        blockers, _ = self.scan(
            {"settings.txt": token},
            worktree_updates={"settings.txt": "safe\n"},
        )
        self.assertIn(
            "github_fine_grained_token",
            {row["key"] for row in blockers},
        )

    def test_unstaged_secret_is_scanned_too(self):
        token = "gl" + "pat-" + "a" * 32
        blockers, _ = self.scan(
            {"settings.txt": "safe\n"},
            worktree_updates={"settings.txt": token},
        )
        self.assertIn("gitlab_token", {row["key"] for row in blockers})

    def test_external_privacy_term_reports_only_non_sensitive_metadata(self):
        private_term = "ProjectOrchidVault"
        privacy_terms = self.load_privacy_terms(
            {"operator_alias": [private_term]}
        )
        blockers, _ = self.scan(
            {"notes.txt": f"public heading\nowner={private_term.swapcase()}\n"},
            privacy_terms=privacy_terms,
        )

        matches = [
            row for row in blockers if row["key"] == "project_privacy_term"
        ]
        self.assertEqual(len(matches), 1)
        self.assertEqual(
            matches[0],
            {
                "level": "blocker",
                "key": "project_privacy_term",
                "category": "operator_alias",
                "path": "notes.txt",
                "line": 2,
                "short_hash": matches[0]["short_hash"],
            },
        )
        self.assertRegex(matches[0]["short_hash"], r"^sha256:[0-9a-f]{12}$")
        rendered = json.dumps(matches, ensure_ascii=False).casefold()
        self.assertNotIn(private_term.casefold(), rendered)

    def test_external_privacy_term_is_checked_in_unstaged_worktree(self):
        private_term = "ShelfAliasQ7"
        privacy_terms = self.load_privacy_terms({"device_alias": [private_term]})
        blockers, _ = self.scan(
            {"notes.txt": "safe staged content\n"},
            worktree_updates={"notes.txt": f"line one\n{private_term}\n"},
            privacy_terms=privacy_terms,
        )

        matches = [
            row for row in blockers if row["key"] == "project_privacy_term"
        ]
        self.assertEqual(len(matches), 1)
        self.assertEqual(matches[0]["path"], "notes.txt")
        self.assertEqual(matches[0]["line"], 2)

    def test_external_privacy_term_does_not_block_non_matching_content(self):
        privacy_terms = self.load_privacy_terms(
            {"operator_alias": ["ProjectOrchidVault"]}
        )
        blockers, warnings = self.scan(
            {"notes.txt": "Project Orchid public documentation\n"},
            privacy_terms=privacy_terms,
        )

        self.assertNotIn(
            "project_privacy_term",
            {row["key"] for row in blockers},
        )
        self.assertEqual(warnings, [])

    def test_staged_privacy_dictionary_schema_is_blocked_without_echoing_terms(self):
        private_term = "StagedOrchidAliasQ7"
        blockers, _ = self.scan(
            {
                "config/publish-labels.json": self.privacy_dictionary_text(
                    {"operator_alias": [private_term]}
                )
            }
        )

        matches = [
            row
            for row in blockers
            if row["key"] == "privacy_dictionary_in_repository"
        ]
        self.assertEqual(len(matches), 1)
        self.assertNotIn(
            private_term.casefold(),
            json.dumps(blockers, ensure_ascii=False).casefold(),
        )

    def test_untracked_privacy_dictionary_schema_is_blocked(self):
        blockers, _ = self.scan(
            {"README.md": "safe staged content\n"},
            worktree_updates={
                "config/publish-labels.json": self.privacy_dictionary_text(
                    {"operator_alias": ["UntrackedOrchidAliasQ7"]}
                )
            },
        )

        self.assertIn(
            "privacy_dictionary_in_repository",
            {row["key"] for row in blockers},
        )

    def test_unstaged_privacy_dictionary_schema_is_blocked(self):
        blockers, _ = self.scan(
            {"config/publish-labels.json": '{"safe":true}\n'},
            worktree_updates={
                "config/publish-labels.json": self.privacy_dictionary_text(
                    {"operator_alias": ["ModifiedOrchidAliasQ7"]}
                )
            },
        )

        self.assertIn(
            "privacy_dictionary_in_repository",
            {row["key"] for row in blockers},
        )

    def test_explicit_privacy_path_overrides_environment_path(self):
        explicit = CHECKER.configured_privacy_terms_path(
            "explicit.json",
            {CHECKER.PRIVACY_TERMS_ENV: "environment.json"},
        )
        environment = CHECKER.configured_privacy_terms_path(
            None,
            {CHECKER.PRIVACY_TERMS_ENV: "environment.json"},
        )
        absent = CHECKER.configured_privacy_terms_path(
            None,
            {CHECKER.PRIVACY_TERMS_ENV: "   "},
        )

        self.assertEqual(explicit, Path("explicit.json"))
        self.assertEqual(environment, Path("environment.json"))
        self.assertIsNone(absent)

    def test_privacy_dictionary_inside_repository_is_rejected_without_path_leak(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            private_term = "PrivateOperatorAlias"
            path = root / "privacy-terms.json"
            path.write_text(
                json.dumps(
                    {
                        "schemaVersion": 1,
                        "categories": {"operator_alias": [private_term]},
                    }
                ),
                encoding="utf-8",
            )

            with self.assertRaises(CHECKER.PrivacyTermsError) as caught:
                CHECKER.load_privacy_terms(path, root=root)

            message = str(caught.exception)
            self.assertNotIn(private_term, message)
            self.assertNotIn(str(path), message)

    def test_external_privacy_dictionary_symlink_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            private_term = "SymlinkedOrchidAliasQ7"
            target = root / "privacy-terms-target.json"
            target.write_text(
                self.privacy_dictionary_text(
                    {"operator_alias": [private_term]}
                ),
                encoding="utf-8",
            )
            link = root / "privacy-terms-link.json"
            try:
                link.symlink_to(target)
            except OSError:
                # Windows may deny symlink creation outside Developer Mode.
                # Exercise the same fail-closed branch deterministically there;
                # CI on Linux exercises a real filesystem symlink.
                with mock.patch.object(Path, "is_symlink", return_value=True):
                    with self.assertRaises(CHECKER.PrivacyTermsError) as caught:
                        CHECKER.load_privacy_terms(link)
            else:
                with self.assertRaises(CHECKER.PrivacyTermsError) as caught:
                    CHECKER.load_privacy_terms(link)

            message = str(caught.exception)
            self.assertNotIn(private_term, message)
            self.assertNotIn(str(target), message)
            self.assertNotIn(str(link), message)

    def test_oversized_external_privacy_dictionary_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "privacy-terms.json"
            path.write_bytes(b" " * (CHECKER.MAX_PRIVACY_TERMS_FILE_BYTES + 1))

            with self.assertRaises(CHECKER.PrivacyTermsError) as caught:
                CHECKER.load_privacy_terms(path)

            self.assertNotIn(str(path), str(caught.exception))

    def test_invalid_privacy_dictionary_does_not_echo_term_or_path(self):
        with tempfile.TemporaryDirectory() as directory:
            private_term = "PrivateMalformedValue"
            path = Path(directory) / "privacy-terms.json"
            path.write_text(
                '{"schemaVersion":1,"categories":{"operator_alias":["'
                + private_term,
                encoding="utf-8",
            )

            with self.assertRaises(CHECKER.PrivacyTermsError) as caught:
                CHECKER.load_privacy_terms(path)

            message = str(caught.exception)
            self.assertNotIn(private_term, message)
            self.assertNotIn(str(path), message)

    def test_duplicate_privacy_dictionary_key_fails_closed(self):
        with tempfile.TemporaryDirectory() as directory:
            private_term = "DuplicateKeyPrivateValue"
            path = Path(directory) / "privacy-terms.json"
            path.write_text(
                '{"schemaVersion":1,"categories":{"operator_alias":["safe"]},'
                '"categories":{"operator_alias":["'
                + private_term
                + '"]}}',
                encoding="utf-8",
            )

            with self.assertRaises(CHECKER.PrivacyTermsError) as caught:
                CHECKER.load_privacy_terms(path)

            message = str(caught.exception)
            self.assertNotIn(private_term, message)
            self.assertNotIn(str(path), message)


if __name__ == "__main__":
    unittest.main()
