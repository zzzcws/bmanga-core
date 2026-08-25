#!/usr/bin/env python3
"""Regenerate the checked-in third-party license-text bundle.

The generator deliberately covers one artifact profile only: linux/amd64,
CGO_ENABLED=0, built from the exact Go and npm locks in this checkout. Every
copied byte is checked against a reviewed SHA-256. It does not infer license
families, approve compatibility, or claim coverage for another platform.
"""

from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
from typing import Any


REPO = Path(__file__).resolve().parents[1]
LICENSES = REPO / "LICENSES"
GO_VERSION = "1.26.6"
NODE_VERSION = "24.19.0"
GO_MOD_SHA256 = "40074b892b43b4fd4e6e97f2dae1bbe39d2bf3e8c6e44c6702e89daee36d0d4f"
GO_SUM_SHA256 = "bfb24c4b1829ebf9b9c64751bfc27a089c756609ddb1a01f7bd699b80f0d7234"
PACKAGE_JSON_SHA256 = "95eec68b5cae0e48454a8f2eb7c1b42080e67ab42908885e061daa761dbd5db7"
PACKAGE_LOCK_SHA256 = "4d0bebbe7c97d2369da450762839f51a5eb47e6e03cf8b21527c5da06295ff4f"
SQLITE_TECHNICAL_REVIEW = (
    "LICENSES/reviews/sqlite-v1.57.0-linux-amd64-technical.json"
)
SQLITE_TECHNICAL_REVIEW_SHA256 = (
    "9d2ebd298da913fbe33f70dc98b82de9606b4067cc15f96919ad4f2c05a6f36d"
)
REVIEWED_SQLITE_PACKAGES = {
    "modernc.org/sqlite",
    "modernc.org/sqlite/lib",
    "modernc.org/sqlite/vtab",
}
EXCLUDED_SQLITE_PACKAGES = {"modernc.org/sqlite/vec"}
SQLITE_VEC_LICENSE_SHA256 = (
    "6ce72bbe12d975bd5286e5ab0a064c069693300c47bccbc57bec18485f1621ea"
)
GO_LICENSE_HASH = "911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad"
GO_PATENTS_HASH = "96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc"
DOCKERFILE_FRONTEND = (
    "docker.io/docker/dockerfile:1.7@"
    "sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e"
)
NODE_BUILD_IMAGE = (
    "docker.io/library/node:24-bookworm-slim@"
    "sha256:3638d9a6fe4030bd716be989438248074489337ba3275657f93595428be4fc03"
)
GO_BUILD_IMAGE = (
    "docker.io/library/golang:1.26.6-bookworm@"
    "sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36"
)

# module, version, [(source filename, reviewed SHA-256)]
GO_MODULES: list[tuple[str, str, list[tuple[str, str]]]] = [
    (
        "github.com/dustin/go-humanize",
        "v1.0.1",
        [("LICENSE", "a973b4498c13eb74baa2a8e5c351426a6826f2fcdd909916dbe53ee2e755fd71")],
    ),
    (
        "github.com/google/uuid",
        "v1.6.0",
        [("LICENSE", "0a8d61ed3cbfd5312326e8126c31ce9c627a283adc99131b56896d29ada04b2d")],
    ),
    (
        "github.com/remyoudompheng/bigfft",
        "v0.0.0-20230129092748-24d4a6f8daec",
        [("LICENSE", "dd26a7abddd02e2d0aba97805b31f248ef7835d9e10da289b22e3b8ab78b324d")],
    ),
    (
        "golang.org/x/image",
        "v0.45.0",
        [("LICENSE", GO_LICENSE_HASH), ("PATENTS", GO_PATENTS_HASH)],
    ),
    (
        "golang.org/x/sys",
        "v0.47.0",
        [("LICENSE", GO_LICENSE_HASH), ("PATENTS", GO_PATENTS_HASH)],
    ),
    (
        "golang.org/x/text",
        "v0.41.0",
        [("LICENSE", GO_LICENSE_HASH), ("PATENTS", GO_PATENTS_HASH)],
    ),
    (
        "modernc.org/libc",
        "v1.74.4",
        [
            ("LICENSE", "95ff867eb55a56935fa7492406cfa953fb7c13ca73f4c0a86ae05756b4605600"),
            (
                "LICENSE-3RD-PARTY.md",
                "f597097efe3d97021f89170746bd3a0fb9a8b6fb26b82043ed68a4e0283bee6c",
            ),
        ],
    ),
    (
        "modernc.org/mathutil",
        "v1.7.1",
        [("LICENSE", "bfa9bf72a72ca009fd62a8f84fca3dca67e51d93af96352723646599898b6cf5")],
    ),
    (
        "modernc.org/memory",
        "v1.11.0",
        [
            ("LICENSE", "59895e669f48f168b6b858358f6005779cdf40a265f7828813061b56af67b496"),
            ("LICENSE-GO", "2d36597f7117c38b006835ae7f537487207d8ec407aa9d9980794b2030cbc067"),
            (
                "LICENSE-MMAP-GO",
                "c2eba69f20d05414538c3a5df7694dde392e065ff70882e1625e90f5d6659fff",
            ),
        ],
    ),
    (
        "modernc.org/sqlite",
        "v1.57.0",
        [
            ("LICENSE", "c6fe05491a60ae13bcd223088d2705e36dede24e5587226231d2459ada5c4822"),
            (
                "LICENSE-SQLITE",
                "8438c9c89b849131ead81d5435cb97fcf052df5b0b286dda8a2d4c29e6cb3fd0",
            ),
        ],
    ),
]

# package, version, artifact role, [(source filename, reviewed SHA-256)]
NPM_COMPONENTS: list[tuple[str, str, str, list[tuple[str, str]]]] = [
    (
        "react",
        "19.2.8",
        "browser-runtime",
        [("LICENSE", "da6d3703ed11cbe42bd212c725957c98da23cbff1998c05fa4b3d976d1a58e93")],
    ),
    (
        "react-dom",
        "19.2.8",
        "browser-runtime",
        [("LICENSE", "da6d3703ed11cbe42bd212c725957c98da23cbff1998c05fa4b3d976d1a58e93")],
    ),
    (
        "scheduler",
        "0.27.0",
        "browser-runtime",
        [("LICENSE", "da6d3703ed11cbe42bd212c725957c98da23cbff1998c05fa4b3d976d1a58e93")],
    ),
    (
        "vite",
        "8.2.1",
        "browser-injected-runtime",
        [("LICENSE.md", "387dd7baa307083401a27c58c362c30832f5ba1dba84f10cc22c33401523f45c")],
    ),
    (
        "rolldown",
        "1.2.4",
        "browser-injected-modulepreload-runtime",
        [
            ("LICENSE", "23ecfff35a5a2e80d92142f75228912c3b1abc4b5a8337a821ff4397e2f9f734"),
            (
                "THIRD-PARTY-LICENSE",
                "a877291d800ed43692f3f9ae09d8e01cc6f7293ad39d43896059c188ffbb8b7c",
            ),
        ],
    ),
]


def sha256(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


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


def read_checked(path: Path, expected: str) -> bytes:
    data = path.read_bytes()
    actual = sha256(data)
    if actual != expected:
        raise RuntimeError(f"unexpected SHA-256 for {path.name}: {actual}, want {expected}")
    return data


def write_license(relative: str, data: bytes, source: dict[str, Any]) -> dict[str, Any]:
    destination = LICENSES / relative
    destination.parent.mkdir(parents=True, exist_ok=True)
    destination.write_bytes(data)
    return {
        "path": f"LICENSES/{relative}",
        "bytes": len(data),
        "sha256": sha256(data),
        "source": source,
    }


def module_dir(modcache: Path, module: str, version: str) -> Path:
    # The reviewed module paths are lowercase; Go's uppercase path escaping is
    # therefore not needed for this exact profile.
    return modcache / f"{module}@{version}"


def build_linkage() -> list[dict[str, Any]]:
    expected = {(module, version) for module, version, _ in GO_MODULES}
    evidence: list[dict[str, Any]] = []
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
    with tempfile.TemporaryDirectory(prefix="bmanga-license-linkage-") as temp:
        for command in ("bmanga-go", "bmanga-scan"):
            package = f"./cmd/{command}"
            imports = set(
                run(
                    "go",
                    "list",
                    "-deps",
                    "-f={{.ImportPath}}",
                    package,
                    env=build_env,
                ).splitlines()
            )
            imported_sqlite_packages = {
                item
                for item in imports
                if item == "modernc.org/sqlite" or item.startswith("modernc.org/sqlite/")
            }
            if imported_sqlite_packages != REVIEWED_SQLITE_PACKAGES:
                raise RuntimeError(
                    f"{command} SQLite package scope changed; "
                    f"missing={sorted(REVIEWED_SQLITE_PACKAGES - imported_sqlite_packages)}, "
                    f"unexpected={sorted(imported_sqlite_packages - REVIEWED_SQLITE_PACKAGES)}"
                )
            if imports & EXCLUDED_SQLITE_PACKAGES:
                raise RuntimeError(
                    f"{command} imports excluded optional SQLite packages: "
                    f"{sorted(imports & EXCLUDED_SQLITE_PACKAGES)}"
                )
            output = str(Path(temp) / command)
            run(
                "go",
                "build",
                "-buildvcs=false",
                "-mod=readonly",
                "-trimpath",
                "-ldflags=-s -w -buildid=",
                "-o",
                output,
                package,
                env=build_env,
            )
            metadata = run("go", "version", "-m", output)
            binary_version = metadata.splitlines()[0].rsplit(": ", 1)[-1]
            if binary_version != f"go{GO_VERSION}":
                raise RuntimeError(
                    f"{command} was built by {binary_version}, want go{GO_VERSION}"
                )
            linked: set[tuple[str, str]] = set()
            for line in metadata.splitlines():
                fields = line.strip().split("\t")
                if len(fields) >= 3 and fields[0] == "dep":
                    linked.add((fields[1], fields[2]))
            if linked != expected:
                raise RuntimeError(
                    f"{command} linkage changed; missing={sorted(expected - linked)}, "
                    f"unexpected={sorted(linked - expected)}"
                )
            evidence.append(
                {
                    "package": f"cmd/{command}",
                    "externalModules": [
                        {"module": module, "version": version}
                        for module, version in sorted(linked)
                    ],
                }
            )
    return evidence


def lock_package(lock: dict[str, Any], name: str, version: str) -> dict[str, Any]:
    package = lock["packages"].get(f"node_modules/{name}")
    if not isinstance(package, dict) or package.get("version") != version:
        raise RuntimeError(f"package-lock does not resolve {name}@{version}")
    return package


def generate() -> None:
    go_mod_path = REPO / "go.mod"
    go_mod = go_mod_path.read_bytes()
    if not re.search(rb"(?m)^go 1\.26\.6\r?$", go_mod):
        raise RuntimeError("go.mod no longer pins the reviewed Go version")
    if re.search(rb"(?m)^\s*replace(?:\s|\()", go_mod):
        raise RuntimeError("go.mod replace directives require a new provenance review")
    if sha256(go_mod) != GO_MOD_SHA256:
        raise RuntimeError("go.mod changed; update the reviewed artifact profile first")
    go_sum = (REPO / "go.sum").read_bytes()
    if sha256(go_sum) != GO_SUM_SHA256:
        raise RuntimeError("go.sum changed; update the reviewed artifact profile first")
    technical_review_bytes = read_checked(
        REPO / SQLITE_TECHNICAL_REVIEW,
        SQLITE_TECHNICAL_REVIEW_SHA256,
    )
    try:
        technical_review = json.loads(technical_review_bytes.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise RuntimeError("SQLite technical review evidence is not valid UTF-8 JSON") from exc
    expected_subject = {
        "module": "modernc.org/sqlite",
        "fromVersion": "v1.56.0",
        "toVersion": "v1.57.0",
        "requiredTransitiveModule": "modernc.org/libc",
        "requiredTransitiveVersion": "v1.74.4",
    }
    if technical_review.get("subject") != expected_subject:
        raise RuntimeError("SQLite technical review subject changed")
    dockerfile = (REPO / "Dockerfile").read_text(encoding="utf-8")
    if dockerfile.splitlines()[0] != f"# syntax={DOCKERFILE_FRONTEND}":
        raise RuntimeError("Dockerfile frontend is not pinned to the reviewed digest")
    for stage, image in (("web-build", NODE_BUILD_IMAGE), ("go-build", GO_BUILD_IMAGE)):
        if f"FROM --platform=$BUILDPLATFORM {image} AS {stage}" not in dockerfile:
            raise RuntimeError(f"Dockerfile {stage} image is not the reviewed immutable input")
    if re.search(r"(?m)^ARG\s+(?:NODE_IMAGE|GO_IMAGE)(?:=|\s|$)", dockerfile):
        raise RuntimeError("Dockerfile build images must not be build-arg overrideable")
    for marker in (
        "FROM scratch",
        "USER 65532:65532",
        "ARG TARGETOS",
        "ARG TARGETARCH",
        "ARG TARGETPLATFORM",
        '"${TARGETOS}" != "linux"',
        '"${TARGETARCH}" != "amd64"',
        '"${TARGETPLATFORM}" != "linux/amd64"',
        'go env GOVERSION',
        '"go1.26.6"',
    ):
        if marker not in dockerfile:
            raise RuntimeError(f"Dockerfile no longer matches the no-base runtime profile: {marker}")
    for marker in ('actual="$(node --version)"', f'"${{actual}}" != "v{NODE_VERSION}"'):
        if dockerfile.count(marker) != 1:
            raise RuntimeError(f"Dockerfile Node toolchain guard changed: {marker}")
    if dockerfile.count('"${TARGETOS}" != "linux"') != 2 or dockerfile.count(
        '"${TARGETARCH}" != "amd64"'
    ) != 2 or dockerfile.count('"${TARGETPLATFORM}" != "linux/amd64"') != 2:
        raise RuntimeError("both build stages must reject targets outside linux/amd64")
    target_build = 'GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" CGO_ENABLED=0 go build'
    if dockerfile.count(target_build) != 2:
        raise RuntimeError("both shipped Go binaries must use the guarded TARGETOS/TARGETARCH")
    compose = (REPO / "compose.yaml").read_text(encoding="utf-8")
    if re.search(r"(?mi)^\s*init\s*:", compose):
        raise RuntimeError("Compose must not inject a host docker-init binary into scratch")
    if sum(row.strip() == "platform: linux/amd64" for row in compose.splitlines()) != 2:
        raise RuntimeError("both Compose services must select the reviewed linux/amd64 platform")

    go_version = run("go", "version")
    version_match = re.search(r"\bgo version go([^\s]+)", go_version)
    if version_match is None or version_match.group(1) != GO_VERSION:
        raise RuntimeError(f"generator requires Go {GO_VERSION}; found {go_version}")
    goroot = Path(run("go", "env", "GOROOT"))
    modcache = Path(run("go", "env", "GOMODCACHE"))
    verify_env = os.environ.copy()
    verify_env.update({"GOWORK": "off", "GOFLAGS": ""})
    if run("go", "mod", "verify", env=verify_env) != "all modules verified":
        raise RuntimeError("Go module cache verification did not complete cleanly")
    read_checked(
        module_dir(modcache, "modernc.org/sqlite", "v1.57.0")
        / "LICENSE-SQLITE_VEC",
        SQLITE_VEC_LICENSE_SHA256,
    )
    linkage = build_linkage()

    lock_path = REPO / "web-v2" / "package-lock.json"
    package_json_path = REPO / "web-v2" / "package.json"
    if sha256(package_json_path.read_bytes()) != PACKAGE_JSON_SHA256:
        raise RuntimeError("package.json changed; update the reviewed artifact profile first")
    package_json = json.loads(package_json_path.read_text(encoding="utf-8"))
    if (package_json.get("devDependencies") or {}).get("@types/node") != "24.13.3":
        raise RuntimeError("generator requires @types/node 24.13.3 for the Node 24 profile")
    if sha256(lock_path.read_bytes()) != PACKAGE_LOCK_SHA256:
        raise RuntimeError("package-lock changed; update the reviewed artifact profile first")
    lock = json.loads(lock_path.read_text(encoding="utf-8"))
    lock_packages = lock.get("packages") or {}
    if {
        ("@types/node", (lock_packages.get("node_modules/@types/node") or {}).get("version")),
        ("undici-types", (lock_packages.get("node_modules/undici-types") or {}).get("version")),
    } != {("@types/node", "24.13.3"), ("undici-types", "7.18.2")}:
        raise RuntimeError("locked Node type packages differ from the reviewed Node 24 profile")
    node_modules = REPO / "web-v2" / "node_modules"
    if not node_modules.is_dir():
        raise RuntimeError("run `npm --prefix web-v2 ci` before regenerating the bundle")

    components: list[dict[str, Any]] = []
    toolchain_files = [
        write_license(
            f"go/toolchain-go{GO_VERSION}/{name}",
            read_checked(goroot / name, expected),
            {
                "kind": "go-toolchain",
                "locator": f"go{GO_VERSION}/{name}",
                "verifiedLocalToolchain": f"go{GO_VERSION}",
            },
        )
        for name, expected in (("LICENSE", GO_LICENSE_HASH), ("PATENTS", GO_PATENTS_HASH))
    ]
    components.append(
        {
            "id": f"go-toolchain-{GO_VERSION}",
            "name": "Go toolchain and standard library",
            "version": GO_VERSION,
            "artifactRoles": ["bmanga-go", "bmanga-scan"],
            "reviewStatus": "pending-human-review",
            "files": toolchain_files,
        }
    )

    for module, version, source_files in GO_MODULES:
        files = []
        root = module_dir(modcache, module, version)
        for filename, expected in source_files:
            files.append(
                write_license(
                    f"go/{module}@{version}/{filename}",
                    read_checked(root / filename, expected),
                    {
                        "kind": "go-module-cache",
                        "locator": f"{module}@{version}/{filename}",
                        "moduleZipAuthenticatedBy": "go.sum",
                    },
                )
            )
        components.append(
            {
                "id": f"go-module:{module}@{version}",
                "name": module,
                "version": version,
                "artifactRoles": ["bmanga-go", "bmanga-scan"],
                "reviewStatus": "pending-human-review",
                "files": files,
            }
        )

    for name, version, role, source_files in NPM_COMPONENTS:
        package = lock_package(lock, name, version)
        files = []
        for filename, expected in source_files:
            files.append(
                write_license(
                    f"npm/{name}@{version}/{filename}",
                    read_checked(node_modules / name / filename, expected),
                    {
                        "kind": "npm-ci-install",
                        "locator": f"node_modules/{name}/{filename}",
                        "lockIntegrity": package.get("integrity"),
                        "lockResolved": package.get("resolved"),
                    },
                )
            )
        components.append(
            {
                "id": f"npm:{name}@{version}",
                "name": name,
                "version": version,
                "artifactRoles": [role],
                "reviewStatus": "pending-human-review",
                "files": files,
            }
        )

    manifest = {
        "schemaVersion": 1,
        "bundlePurpose": (
            "machine-verifiable license-text inventory for one candidate artifact profile; "
            "not a legal conclusion"
        ),
        "releaseReadiness": {
            "ready": False,
            "blockingReasons": [
                {
                    "code": "human-license-review-pending",
                    "detail": "original texts and component mappings require human review",
                },
            ],
        },
        "artifactProfile": {
            "go": {
                "version": GO_VERSION,
                "goos": "linux",
                "goarch": "amd64",
                "cgoEnabled": False,
                "goModSha256": sha256(go_mod),
                "goSumSha256": sha256(go_sum),
                "technicalReviewEvidence": {
                    "path": SQLITE_TECHNICAL_REVIEW,
                    "sha256": SQLITE_TECHNICAL_REVIEW_SHA256,
                },
                "binaryLinkage": linkage,
            },
            "web": {
                "nodeVersion": NODE_VERSION,
                "packageJson": "web-v2/package.json",
                "packageJsonSha256": sha256(package_json_path.read_bytes()),
                "packageLock": "web-v2/package-lock.json",
                "packageLockSha256": sha256(lock_path.read_bytes()),
                "productionPackages": [
                    {"name": "react", "version": "19.2.8"},
                    {"name": "react-dom", "version": "19.2.8"},
                    {"name": "scheduler", "version": "0.27.0"},
                ],
                "injectedBuildRuntime": [
                    {"name": "vite", "version": "8.2.1"},
                    {"name": "rolldown", "version": "1.2.4"},
                ],
                "typeOnlyPackages": [
                    {"name": "@types/node", "version": "24.13.3"},
                    {"name": "undici-types", "version": "7.18.2"},
                ],
            },
            "container": {
                "dockerfileFrontend": DOCKERFILE_FRONTEND,
                "buildImages": {
                    "node": NODE_BUILD_IMAGE,
                    "go": GO_BUILD_IMAGE,
                },
                "runtimeBase": "scratch",
                "user": "65532:65532",
                "buildTarget": {
                    "targetplatform": "linux/amd64",
                    "targetos": "linux",
                    "targetarch": "amd64",
                    "enforcement": "Dockerfile pre-build guard",
                },
                "includedFilesystem": (
                    "two static Go binaries, generated web assets, project license, "
                    "third-party notices, and this LICENSES tree"
                ),
            },
        },
        "components": sorted(components, key=lambda component: component["id"]),
    }
    (LICENSES / "manifest.json").write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
        newline="\n",
    )
    print(f"wrote {len(components)} components to {LICENSES / 'manifest.json'}")


def main() -> int:
    try:
        generate()
    except (OSError, RuntimeError, subprocess.CalledProcessError) as exc:
        print(f"license bundle generation failed: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
