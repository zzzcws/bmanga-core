from __future__ import annotations

import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch


SOURCE_REPO = Path(__file__).resolve().parents[1]
CHECKER_PATH = SOURCE_REPO / "tools" / "check-third-party-licenses.py"
SPEC = importlib.util.spec_from_file_location("bmanga_third_party_checker", CHECKER_PATH)
if SPEC is None or SPEC.loader is None:  # pragma: no cover - import machinery failure
    raise RuntimeError(f"cannot load {CHECKER_PATH}")
CHECKER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(CHECKER)


class ThirdPartyLicenseGateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory(prefix="bmanga-license-gate-test-")
        self.repo = Path(self.temp.name)
        for name in ("go.mod", "go.sum", "Dockerfile", "compose.yaml", ".dockerignore"):
            shutil.copy2(SOURCE_REPO / name, self.repo / name)
        (self.repo / "web-v2").mkdir()
        for name in ("package.json", "package-lock.json"):
            shutil.copy2(
                SOURCE_REPO / "web-v2" / name,
                self.repo / "web-v2" / name,
            )
        shutil.copytree(SOURCE_REPO / "LICENSES", self.repo / "LICENSES")
        self.manifest = json.loads(
            (self.repo / "LICENSES" / "manifest.json").read_text(encoding="utf-8")
        )
        self.original_repo = CHECKER.REPO
        CHECKER.REPO = self.repo

    def tearDown(self) -> None:
        CHECKER.REPO = self.original_repo
        self.temp.cleanup()

    def review_decision(self, manifest: dict) -> dict:
        return {
            "schemaVersion": 1,
            "artifactProfileSha256": CHECKER.canonical_json_sha256(
                manifest["artifactProfile"]
            ),
            "reviewedBy": "@maintainer",
            "reviewedAt": "2026-08-17T00:00:00Z",
            "components": [
                {
                    "id": component["id"],
                    "decision": "approved",
                    "licenseConclusion": "redistribution approved for the reviewed artifact profile",
                    "obligations": [
                        "retain the mapped license and notice files in distributions"
                    ],
                    "files": [
                        {"path": record["path"], "sha256": record["sha256"]}
                        for record in component["files"]
                    ],
                }
                for component in manifest["components"]
            ],
        }

    def install_review_decision(self, manifest: dict, decision: dict) -> Path:
        decision_path = self.repo / "LICENSES" / "reviews" / "linux-amd64.json"
        decision_path.parent.mkdir(parents=True, exist_ok=True)
        decision_path.write_text(
            json.dumps(decision, ensure_ascii=False, indent=2) + "\n",
            encoding="utf-8",
        )
        decision_hash = hashlib.sha256(decision_path.read_bytes()).hexdigest()
        for component in manifest["components"]:
            component["reviewStatus"] = "human-reviewed"
            component["reviewEvidence"] = {
                "decisionFile": "LICENSES/reviews/linux-amd64.json",
                "sha256": decision_hash,
            }
        manifest["releaseReadiness"] = {"ready": True, "blockingReasons": []}
        return decision_path

    def pending_manifest(self) -> dict:
        manifest = copy.deepcopy(self.manifest)
        for component in manifest["components"]:
            component["reviewStatus"] = "pending-human-review"
            component.pop("reviewEvidence", None)
        manifest["releaseReadiness"] = {
            "ready": False,
            "blockingReasons": [
                {
                    "code": "human-license-review-pending",
                    "detail": "original texts and component mappings require human review",
                }
            ],
        }
        (self.repo / "LICENSES" / "reviews" / "linux-amd64.json").unlink(
            missing_ok=True
        )
        return manifest

    def test_pending_bundle_integrity_passes(self) -> None:
        CHECKER.verify_integrity(self.pending_manifest())

    def test_pending_bundle_is_not_release_ready(self) -> None:
        manifest = self.pending_manifest()
        CHECKER.verify_integrity(manifest)
        with self.assertRaisesRegex(CHECKER.VerificationError, "human-license-review-pending"):
            CHECKER.verify_release_readiness(manifest)

    def test_human_review_evidence_can_clear_release_blocker(self) -> None:
        manifest = copy.deepcopy(self.manifest)
        self.install_review_decision(manifest, self.review_decision(manifest))
        CHECKER.verify_integrity(manifest)
        CHECKER.verify_release_readiness(manifest)

    def test_reviewed_status_without_evidence_is_rejected(self) -> None:
        manifest = self.pending_manifest()
        manifest["components"][0]["reviewStatus"] = "human-reviewed"
        with self.assertRaisesRegex(CHECKER.VerificationError, "component schema"):
            CHECKER.verify_integrity(manifest)

    def test_partial_human_review_transition_is_rejected(self) -> None:
        manifest = copy.deepcopy(self.manifest)
        decision = self.review_decision(manifest)
        self.install_review_decision(manifest, decision)
        manifest["components"][-1].pop("reviewEvidence")
        manifest["components"][-1]["reviewStatus"] = "pending-human-review"
        manifest["releaseReadiness"] = copy.deepcopy(self.manifest["releaseReadiness"])
        with self.assertRaisesRegex(CHECKER.VerificationError, "cover every component"):
            CHECKER.verify_integrity(manifest)

    def test_human_review_decision_hash_mismatch_is_rejected(self) -> None:
        manifest = copy.deepcopy(self.manifest)
        decision_path = self.install_review_decision(manifest, self.review_decision(manifest))
        decision_path.write_text(
            decision_path.read_text(encoding="utf-8") + " ", encoding="utf-8"
        )
        with self.assertRaisesRegex(CHECKER.VerificationError, "decision file hash changed"):
            CHECKER.verify_integrity(manifest)

    def test_human_review_decision_requires_exact_component_set(self) -> None:
        for mutation in ("missing", "extra"):
            with self.subTest(mutation=mutation):
                manifest = copy.deepcopy(self.manifest)
                decision = self.review_decision(manifest)
                if mutation == "missing":
                    decision["components"].pop()
                else:
                    extra = copy.deepcopy(decision["components"][0])
                    extra["id"] = "unexpected-component"
                    decision["components"].append(extra)
                self.install_review_decision(manifest, decision)
                with self.assertRaisesRegex(CHECKER.VerificationError, "component set differs"):
                    CHECKER.verify_integrity(manifest)

    def test_human_review_decision_requires_current_artifact_profile(self) -> None:
        manifest = copy.deepcopy(self.manifest)
        decision = self.review_decision(manifest)
        decision["artifactProfileSha256"] = "0" * 64
        self.install_review_decision(manifest, decision)
        with self.assertRaisesRegex(CHECKER.VerificationError, "artifact profile hash"):
            CHECKER.verify_integrity(manifest)

    def test_human_review_decision_requires_exact_file_set_and_hashes(self) -> None:
        cases = ("missing", "extra", "wrong-hash")
        for mutation in cases:
            with self.subTest(mutation=mutation):
                manifest = copy.deepcopy(self.manifest)
                decision = self.review_decision(manifest)
                files = decision["components"][0]["files"]
                if mutation == "missing":
                    files.pop()
                elif mutation == "extra":
                    files.append(
                        {"path": "LICENSES/reviews/unexpected", "sha256": "0" * 64}
                    )
                else:
                    files[0]["sha256"] = "0" * 64
                self.install_review_decision(manifest, decision)
                expected = (
                    "file hash differs" if mutation == "wrong-hash" else "file set differs"
                )
                with self.assertRaisesRegex(CHECKER.VerificationError, expected):
                    CHECKER.verify_integrity(manifest)

    def test_human_review_decision_rejects_placeholders(self) -> None:
        cases = ("conclusion", "obligation")
        for mutation in cases:
            with self.subTest(mutation=mutation):
                manifest = copy.deepcopy(self.manifest)
                decision = self.review_decision(manifest)
                if mutation == "conclusion":
                    decision["components"][0]["licenseConclusion"] = "TBD after release"
                else:
                    decision["components"][0]["obligations"] = ["TODO later"]
                self.install_review_decision(manifest, decision)
                with self.assertRaisesRegex(CHECKER.VerificationError, "placeholder"):
                    CHECKER.verify_integrity(manifest)

    def test_human_review_decision_requires_explicit_approval(self) -> None:
        manifest = copy.deepcopy(self.manifest)
        decision = self.review_decision(manifest)
        decision["components"][0]["decision"] = "pending"
        self.install_review_decision(manifest, decision)
        with self.assertRaisesRegex(CHECKER.VerificationError, "not approved"):
            CHECKER.verify_integrity(manifest)

    def test_human_review_decision_rejects_invalid_utc_dates(self) -> None:
        invalid_dates = ("2026-02-30T00:00:00Z", "2026-08-17T08:00:00+08:00")
        for reviewed_at in invalid_dates:
            with self.subTest(reviewed_at=reviewed_at):
                manifest = copy.deepcopy(self.manifest)
                decision = self.review_decision(manifest)
                decision["reviewedAt"] = reviewed_at
                self.install_review_decision(manifest, decision)
                with self.assertRaisesRegex(CHECKER.VerificationError, "reviewedAt"):
                    CHECKER.verify_integrity(manifest)

    def test_human_review_decision_rejects_extra_fields(self) -> None:
        manifest = copy.deepcopy(self.manifest)
        decision = self.review_decision(manifest)
        decision["selfApproved"] = True
        self.install_review_decision(manifest, decision)
        with self.assertRaisesRegex(CHECKER.VerificationError, "decision schema"):
            CHECKER.verify_integrity(manifest)

    def test_tampered_source_kind_is_rejected(self) -> None:
        manifest = copy.deepcopy(self.manifest)
        manifest["components"][0]["files"][0]["source"]["kind"] = "unverified-copy"
        with self.assertRaisesRegex(CHECKER.VerificationError, "source mapping changed"):
            CHECKER.verify_integrity(manifest)

    def test_tampered_source_locator_is_rejected(self) -> None:
        manifest = copy.deepcopy(self.manifest)
        manifest["components"][0]["files"][0]["source"]["locator"] = "somewhere/LICENSE"
        with self.assertRaisesRegex(CHECKER.VerificationError, "source mapping changed"):
            CHECKER.verify_integrity(manifest)

    def test_tampered_bundled_text_is_rejected(self) -> None:
        record = self.manifest["components"][0]["files"][0]
        license_path = self.repo / record["path"]
        license_path.write_bytes(license_path.read_bytes() + b"tampered\n")
        with self.assertRaisesRegex(CHECKER.VerificationError, "license size changed"):
            CHECKER.verify_integrity(self.manifest)

    def test_tampered_sqlite_technical_review_is_rejected(self) -> None:
        review_path = self.repo / CHECKER.SQLITE_TECHNICAL_REVIEW
        review_path.write_bytes(review_path.read_bytes() + b" ")
        with self.assertRaisesRegex(
            CHECKER.VerificationError,
            "technical-review evidence changed",
        ):
            CHECKER.verify_integrity(self.manifest)

    def test_sqlite_technical_review_mapping_is_required(self) -> None:
        manifest = copy.deepcopy(self.manifest)
        manifest["artifactProfile"]["go"]["technicalReviewEvidence"]["sha256"] = (
            "0" * 64
        )
        with self.assertRaisesRegex(
            CHECKER.VerificationError,
            "technical-review evidence mapping changed",
        ):
            CHECKER.verify_integrity(manifest)

    def test_unreviewed_scratch_copy_is_rejected(self) -> None:
        dockerfile = self.repo / "Dockerfile"
        content = dockerfile.read_text(encoding="utf-8")
        content = content.replace(
            "FROM scratch\n",
            "FROM scratch\nCOPY go.mod /app/unreviewed-go.mod\n",
            1,
        )
        dockerfile.write_text(content, encoding="utf-8")
        with self.assertRaisesRegex(
            CHECKER.VerificationError,
            "final-stage copies differ",
        ):
            CHECKER.verify_integrity(self.manifest)

    def test_any_compose_init_key_is_rejected(self) -> None:
        compose = self.repo / "compose.yaml"
        compose.write_text(compose.read_text(encoding="utf-8") + "\n  init: yes\n", encoding="utf-8")
        with self.assertRaisesRegex(CHECKER.VerificationError, "docker-init"):
            CHECKER.verify_integrity(self.manifest)

    def test_node_artifact_profile_is_exact(self) -> None:
        manifest = copy.deepcopy(self.manifest)
        manifest["artifactProfile"]["web"]["nodeVersion"] = "24.19.1"
        with self.assertRaisesRegex(CHECKER.VerificationError, "Node 24.19.0"):
            CHECKER.verify_integrity(manifest)

    def test_node_type_profile_is_exact(self) -> None:
        manifest = copy.deepcopy(self.manifest)
        manifest["artifactProfile"]["web"]["typeOnlyPackages"][0]["version"] = "26.1.1"
        with self.assertRaisesRegex(CHECKER.VerificationError, "Node type package set"):
            CHECKER.verify_integrity(manifest)

    def test_package_json_node_types_are_exact(self) -> None:
        package_json_path = self.repo / "web-v2" / "package.json"
        package_json = json.loads(package_json_path.read_text(encoding="utf-8"))
        package_json["devDependencies"]["@types/node"] = "26.1.1"
        package_json_path.write_text(json.dumps(package_json) + "\n", encoding="utf-8")
        with self.assertRaisesRegex(CHECKER.VerificationError, "package.json changed"):
            CHECKER.verify_integrity(self.manifest)

    def test_node_build_image_is_exact(self) -> None:
        manifest = copy.deepcopy(self.manifest)
        manifest["artifactProfile"]["container"]["buildImages"]["node"] = (
            "docker.io/library/node:24-bookworm-slim@sha256:" + "0" * 64
        )
        with self.assertRaisesRegex(CHECKER.VerificationError, "reviewed Node and Go build images"):
            CHECKER.verify_integrity(manifest)

    def test_dockerfile_node_toolchain_guard_is_required(self) -> None:
        dockerfile = self.repo / "Dockerfile"
        dockerfile.write_text(
            dockerfile.read_text(encoding="utf-8").replace("v24.19.0", "v24.19.1"),
            encoding="utf-8",
        )
        with self.assertRaisesRegex(CHECKER.VerificationError, "Node toolchain guard"):
            CHECKER.verify_integrity(self.manifest)


class GoLinkageColdCacheTest(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory(prefix="bmanga-go-cold-cache-test-")
        self.addCleanup(self.temp.cleanup)
        self.repo = Path(self.temp.name)
        for name in ("go.mod", "go.sum"):
            shutil.copy2(SOURCE_REPO / name, self.repo / name)
        self.manifest = json.loads((SOURCE_REPO / "LICENSES/manifest.json").read_text(encoding="utf-8"))
        self.modcache = self.repo / "empty-module-cache"
        self.license = self.modcache / "modernc.org/sqlite@v1.58.0/LICENSE-SQLITE_VEC"
        self.license_bytes = b"synthetic optional-license fixture\n"
        self.events: list[str] = []
        self.commands: list[tuple[str, ...]] = []
        self.materialize = True
        self.corrupt_license = False
        self.fail_download = False
        self.change_sum = False
        self.verify_result = "all modules verified"
        self.original_sha = CHECKER.sha256
        for context in (
            patch.object(CHECKER, "REPO", self.repo),
            patch.object(CHECKER, "SQLITE_VEC_LICENSE_SHA256", hashlib.sha256(self.license_bytes).hexdigest()),
            patch.object(CHECKER, "run", side_effect=self.run_go),
            patch.object(CHECKER, "verify_original_copy"),
            patch.object(CHECKER, "sha256", side_effect=self.sha),
            patch.dict(os.environ, {"GOMODCACHE": str(self.modcache)}),
        ):
            context.start()
            self.addCleanup(context.stop)

    def sha(self, path: Path) -> str:
        if path == self.license:
            self.events.append("license-read")
        return self.original_sha(path)

    def run_go(self, *args: str, env: dict[str, str] | None = None) -> str:
        self.commands.append(args)
        if args == ("go", "version"):
            return f"go version go{CHECKER.GO_VERSION} linux/amd64"
        self.assertIsNotNone(env)
        self.assertEqual(env["GOWORK"], "off")
        self.assertEqual(env["GOFLAGS"], "-mod=readonly")
        self.assertEqual(env["GOMODCACHE"], str(self.modcache))
        self.assertEqual((env["GOOS"], env["GOARCH"], env["CGO_ENABLED"]), ("linux", "amd64", "0"))
        if args == ("go", "mod", "download"):
            self.events.append("download")
            if self.fail_download:
                raise subprocess.CalledProcessError(1, args)
            if self.change_sum:
                with (self.repo / "go.sum").open("ab") as target:
                    target.write(b"unexpected change\n")
            if self.materialize:
                self.license.parent.mkdir(parents=True)
                self.license.write_bytes(b"changed" if self.corrupt_license else self.license_bytes)
            return ""
        if args == ("go", "mod", "verify"):
            self.events.append("verify")
            return self.verify_result
        if args == ("go", "env", "GOROOT"):
            return str(self.repo / "go-root")
        if args == ("go", "env", "GOMODCACHE"):
            return str(self.modcache)
        if args[:2] == ("go", "list"):
            return "\n".join(sorted(CHECKER.REVIEWED_SQLITE_PACKAGES))
        if args[:2] == ("go", "build"):
            self.assertIn("-mod=readonly", args)
            return ""
        if args[:3] == ("go", "version", "-m"):
            package = "cmd/" + Path(args[3]).name
            entry = next(item for item in self.manifest["artifactProfile"]["go"]["binaryLinkage"] if item["package"] == package)
            return f"binary: go{CHECKER.GO_VERSION}\n" + "\n".join(
                f"\tdep\t{item['module']}\t{item['version']}\th1:fixture" for item in entry["externalModules"]
            )
        if args[:3] == ("go", "tool", "nm"):
            return ""
        self.fail(f"unexpected Go command: {args}")

    def test_empty_cache_download_precedes_verify_and_source_read(self) -> None:
        self.assertFalse(self.modcache.exists())
        before = {name: (self.repo / name).read_bytes() for name in ("go.mod", "go.sum")}
        CHECKER.verify_go_linkage(self.manifest)
        self.assertEqual(self.events, ["download", "verify", "license-read"])
        self.assertEqual(before, {name: (self.repo / name).read_bytes() for name in before})

    def test_failed_download_cannot_verify_read_or_build(self) -> None:
        self.fail_download = True
        with self.assertRaises(subprocess.CalledProcessError):
            CHECKER.verify_go_linkage(self.manifest)
        self.assertEqual(self.events, ["download"])
        self.assertEqual(self.commands, [("go", "version"), ("go", "mod", "download")])

    def test_failed_cache_verification_cannot_read_or_build(self) -> None:
        self.verify_result = "not verified"
        with self.assertRaisesRegex(CHECKER.VerificationError, "cache verification"):
            CHECKER.verify_go_linkage(self.manifest)
        self.assertEqual(self.events, ["download", "verify"])

    def test_missing_and_changed_source_are_distinct(self) -> None:
        self.materialize = False
        with self.assertRaisesRegex(CHECKER.VerificationError, "source is missing after module download"):
            CHECKER.verify_go_linkage(self.manifest)
        self.assertEqual(self.events, ["download", "verify"])
        self.events.clear()
        self.materialize = True
        self.corrupt_license = True
        with self.assertRaisesRegex(CHECKER.VerificationError, "license source changed"):
            CHECKER.verify_go_linkage(self.manifest)
        self.assertEqual(self.events, ["download", "verify", "license-read"])

    def test_changed_module_input_rejected_before_download(self) -> None:
        (self.repo / "go.mod").write_bytes(b"unreviewed module\n")
        with self.assertRaisesRegex(CHECKER.VerificationError, "reviewed go.mod changed"):
            CHECKER.verify_go_linkage(self.manifest)
        self.assertEqual(self.commands, [("go", "version")])

    def test_download_must_not_change_pinned_module_inputs(self) -> None:
        self.change_sum = True
        with self.assertRaisesRegex(CHECKER.VerificationError, "reviewed go.sum changed"):
            CHECKER.verify_go_linkage(self.manifest)
        self.assertEqual(self.events, ["download"])


if __name__ == "__main__":
    unittest.main()
