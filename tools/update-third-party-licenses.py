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
from urllib.request import Request, urlopen


REPO = Path(__file__).resolve().parents[1]
LICENSES = REPO / "LICENSES"
GO_VERSION = "1.26.6"
GO_MOD_SHA256 = "5986a1fc21c69d3448b5c193182b6780dffa03f9467b6012d1052d536b10dfa4"
GO_SUM_SHA256 = "5c6c5595aea3aa07497aa7e25f8e5a64e702151902e3433446ca176736168ab9"
PACKAGE_LOCK_SHA256 = "8ff3b5eacb566d47fbfe183d3897f15f2251a36de76f3a3ba97aa76e1c89ea0a"
GO_LICENSE_HASH = "911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad"
GO_PATENTS_HASH = "96f408bfae65bf137fc2525d3ecb030271c50c1e90799f87abf8846d8dd505cc"
ROLLDOWN_NOTICE_URL = (
    "https://raw.githubusercontent.com/rolldown/rolldown/"
    "v1.1.5/THIRD-PARTY-LICENSE"
)
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
        "v1.72.0",
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
        "v1.50.0",
        [
            ("LICENSE", "c6fe05491a60ae13bcd223088d2705e36dede24e5587226231d2459ada5c4822"),
            (
                "SQLITE-LICENSE",
                "8438c9c89b849131ead81d5435cb97fcf052df5b0b286dda8a2d4c29e6cb3fd0",
            ),
        ],
    ),
]

# package, version, artifact role, [(source filename, reviewed SHA-256)]
NPM_COMPONENTS: list[tuple[str, str, str, list[tuple[str, str]]]] = [
    (
        "react",
        "19.2.7",
        "browser-runtime",
        [("LICENSE", "da6d3703ed11cbe42bd212c725957c98da23cbff1998c05fa4b3d976d1a58e93")],
    ),
    (
        "react-dom",
        "19.2.7",
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
        "8.1.4",
        "browser-injected-runtime",
        [("LICENSE.md", "b1d741c26b53de1bbc0d4d7d3365b79888f9fe511527544a8a7b8e24dec43147")],
    ),
    (
        "rolldown",
        "1.1.5",
        "browser-injected-modulepreload-runtime",
        [("LICENSE", "23ecfff35a5a2e80d92142f75228912c3b1abc4b5a8337a821ff4397e2f9f734")],
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


def fetch_checked(url: str, expected: str) -> bytes:
    request = Request(url, headers={"User-Agent": "bmanga-core-license-bundle/1"})
    with urlopen(request, timeout=30) as response:
        data = response.read()
    actual = sha256(data)
    if actual != expected:
        raise RuntimeError(f"unexpected SHA-256 for {url}: {actual}, want {expected}")
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
    linkage = build_linkage()

    lock_path = REPO / "web-v2" / "package-lock.json"
    if sha256(lock_path.read_bytes()) != PACKAGE_LOCK_SHA256:
        raise RuntimeError("package-lock changed; update the reviewed artifact profile first")
    lock = json.loads(lock_path.read_text(encoding="utf-8"))
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
        if name == "rolldown":
            files.append(
                write_license(
                    f"npm/{name}@{version}/THIRD-PARTY-LICENSE",
                    fetch_checked(
                        ROLLDOWN_NOTICE_URL,
                        "a877291d800ed43692f3f9ae09d8e01cc6f7293ad39d43896059c188ffbb8b7c",
                    ),
                    {
                        "kind": "fixed-upstream-tag",
                        "locator": ROLLDOWN_NOTICE_URL,
                        "reason": "the npm package LICENSE links to this file but omits it",
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
                "binaryLinkage": linkage,
            },
            "web": {
                "packageLock": "web-v2/package-lock.json",
                "packageLockSha256": sha256(lock_path.read_bytes()),
                "productionPackages": [
                    {"name": "react", "version": "19.2.7"},
                    {"name": "react-dom", "version": "19.2.7"},
                    {"name": "scheduler", "version": "0.27.0"},
                ],
                "injectedBuildRuntime": [
                    {"name": "vite", "version": "8.1.4"},
                    {"name": "rolldown", "version": "1.1.5"},
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
