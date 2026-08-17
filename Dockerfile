# syntax=docker.io/docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

FROM --platform=$BUILDPLATFORM docker.io/library/node:22-bookworm-slim@sha256:d649c27dae7ba0137b3cef5dd75baa422c08dc3d9e3fc0c23dfb172dc3cc6436 AS web-build
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH
RUN if [ "${TARGETPLATFORM}" != "linux/amd64" ] || [ "${TARGETOS}" != "linux" ] || [ "${TARGETARCH}" != "amd64" ]; then \
      echo "unsupported release target: ${TARGETOS}/${TARGETARCH}; license manifest covers linux/amd64 only" >&2; \
      exit 1; \
    fi
WORKDIR /src
COPY web-v2/package.json web-v2/package-lock.json ./web-v2/
RUN npm --prefix web-v2 ci
COPY web-v2 ./web-v2
COPY tools/build-web-assets.mjs ./tools/build-web-assets.mjs
RUN node tools/build-web-assets.mjs

FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36 AS go-build
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=0.0.0-dev
RUN if [ "${TARGETPLATFORM}" != "linux/amd64" ] || [ "${TARGETOS}" != "linux" ] || [ "${TARGETARCH}" != "amd64" ]; then \
      echo "unsupported release target: ${TARGETOS}/${TARGETARCH}; license manifest covers linux/amd64 only" >&2; \
      exit 1; \
    fi
RUN actual="$(go env GOVERSION)"; \
    if [ "${actual}" != "go1.26.6" ]; then \
      echo "unexpected Go toolchain: ${actual}; license manifest covers go1.26.6 only" >&2; \
      exit 1; \
    fi
RUN if [ -z "${VERSION}" ] || [ "${#VERSION}" -gt 64 ]; then \
      echo "invalid release version: expected 1-64 characters from A-Za-z0-9._+-" >&2; \
      exit 1; \
    fi; \
    case "${VERSION}" in \
      *[!A-Za-z0-9._+-]*) \
        echo "invalid release version: expected 1-64 characters from A-Za-z0-9._+-" >&2; \
        exit 1 ;; \
    esac
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN mkdir -p /out/data /out/cache /out/.cache /out/runs \
    && GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" CGO_ENABLED=0 go build \
      -buildvcs=false \
      -mod=readonly \
      -trimpath \
      -ldflags="-s -w -buildid= -X github.com/zzzcws/bmanga-core/internal/prototype.releaseVersion=${VERSION}" \
      -o /out/bmanga \
      ./cmd/bmanga-go \
    && GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" CGO_ENABLED=0 go build \
      -buildvcs=false \
      -mod=readonly \
      -trimpath \
      -ldflags="-s -w -buildid= -X github.com/zzzcws/bmanga-core/internal/prototype.releaseVersion=${VERSION}" \
      -o /out/bmanga-scan \
      ./cmd/bmanga-scan

FROM scratch
ARG VERSION=0.0.0-dev
ARG REVISION=unknown
ARG SOURCE_URL=https://github.com/zzzcws/bmanga-core
LABEL org.opencontainers.image.title="bmanga-core" \
      org.opencontainers.image.description="Local-first, self-hosted manga catalog and reader candidate" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.source="${SOURCE_URL}" \
      org.opencontainers.image.licenses="Apache-2.0 AND BSD-2-Clause AND BSD-3-Clause AND CC0-1.0 AND ISC AND MIT AND LicenseRef-SQLite-Public-Domain"

ENV TZ=UTC
WORKDIR /app
COPY --from=go-build --chown=65532:65532 /out/ /app/
COPY --from=web-build --chown=65532:65532 /src/web/v2 /app/web/v2
COPY --chown=65532:65532 LICENSE THIRD_PARTY_NOTICES.md /app/
COPY --chown=65532:65532 LICENSES /app/LICENSES

USER 65532:65532
EXPOSE 8765
STOPSIGNAL SIGTERM
ENTRYPOINT ["/app/bmanga"]
CMD ["--host", "0.0.0.0", "--port", "8765", "--allow-wildcard-bind", "--db", "/app/data/bmanga.sqlite"]
