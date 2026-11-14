# syntax=docker/dockerfile:1.28@sha256:bb22d9815c728170f72750f4e5b0d672e06176142e1d602c7e66c050100b7e5b

# Node and pnpm match mise.toml. The console bundle is platform-independent,
# so this stage runs on the build machine and the Go stage embeds the result.
FROM --platform=$BUILDPLATFORM node:24.22.3-bookworm@sha256:9d266751ca3d7fdbe651c878ffd5ad0a42fed49a18c283509267c97f025b7496 AS console
WORKDIR /src
COPY mise.toml mise.toml
COPY scripts/build-console.sh scripts/build-console.sh
COPY internal/consoleui/dist/README.txt internal/consoleui/dist/README.txt
COPY web/ web/
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
RUN corepack enable \
 && corepack prepare pnpm@11.28.11 --activate \
 && ./scripts/build-console.sh

FROM cgr.dev/chainguard/static:latest@sha256:fc42af33e643f20d08baf03cddcc1c02ba57052dfb02edb0d79f3fc47bcea8d9 AS runtime-base

FROM --platform=$BUILDPLATFORM golang:1.27.4-bookworm@sha256:4fa7c968ba93f1635670522dbb01a2c78150443e1385746924566079f36570ea AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
WORKDIR /cosign-source
COPY tools/cosign-build/go.mod tools/cosign-build/go.sum ./
COPY tools/cosign-build/compat ./compat
# Build the reviewed Cosign 3.1.4 command from the locked dependency graph
# under the same patched Go toolchain as Conduit. The trusted-supply-chain
# gate holds the upstream version and dependency overrides to their reviewed pins.
RUN go mod download && \
  mkdir -p /out && \
  CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false \
    -ldflags="-s -w" -o /out/cosign github.com/sigstore/cosign/v3/cmd/cosign
WORKDIR /source
COPY go.mod go.sum ./
COPY cmd cmd
COPY internal internal
COPY gen gen
RUN rm -rf internal/consoleui/dist
COPY --from=console /src/internal/consoleui/dist/ internal/consoleui/dist/
RUN mkdir -p /out/state && touch /out/state/.keep && \
  CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X github.com/jonathanagustin/conduit/internal/buildinfo.Version=$VERSION -X github.com/jonathanagustin/conduit/internal/buildinfo.Commit=$COMMIT -X github.com/jonathanagustin/conduit/internal/buildinfo.BuildDate=$BUILD_DATE" \
    -o /out/platform-delivery ./cmd/platform-delivery && \
  CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X github.com/jonathanagustin/conduit/internal/buildinfo.Version=$VERSION -X github.com/jonathanagustin/conduit/internal/buildinfo.Commit=$COMMIT -X github.com/jonathanagustin/conduit/internal/buildinfo.BuildDate=$BUILD_DATE" \
    -o /out/platform-module-publisher ./cmd/platform-module-publisher && \
	CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false \
	  -ldflags="-s -w -X github.com/jonathanagustin/conduit/internal/buildinfo.Version=$VERSION -X github.com/jonathanagustin/conduit/internal/buildinfo.Commit=$COMMIT -X github.com/jonathanagustin/conduit/internal/buildinfo.BuildDate=$BUILD_DATE" \
	  -o /out/platform-grype-scanner ./cmd/platform-grype-scanner && \
  CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false \
    -ldflags="-s -w -X github.com/jonathanagustin/conduit/internal/buildinfo.Version=$VERSION -X github.com/jonathanagustin/conduit/internal/buildinfo.Commit=$COMMIT -X github.com/jonathanagustin/conduit/internal/buildinfo.BuildDate=$BUILD_DATE" \
    -o /out/platform-operator ./cmd/platform-operator
COPY --from=runtime-base /etc/ssl/certs/ca-certificates.crt /out/runtime/etc/ssl/certs/ca-certificates.crt
RUN chmod 0755 /out/runtime /out/runtime/etc /out/runtime/etc/ssl /out/runtime/etc/ssl/certs && \
  chmod 0644 /out/runtime/etc/ssl/certs/ca-certificates.crt

# Assemble the static Hub runtime in scratch so its binary, state, and CA-bundle
# paths have explicit modes and ownership under the declared non-root UID/GID.
# Keep only the system CA bundle needed by outbound HTTPS clients.
FROM scratch
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
LABEL org.opencontainers.image.title="Platform Hub" \
      org.opencontainers.image.description="Platform delivery control plane" \
      org.opencontainers.image.source="https://github.com/jonathanagustin/conduit" \
      org.opencontainers.image.version=$VERSION \
      org.opencontainers.image.revision=$COMMIT \
      org.opencontainers.image.created=$BUILD_DATE \
      org.opencontainers.image.licenses="LicenseRef-Proprietary"
COPY --from=build --chown=65532:65532 --chmod=0755 /out/platform-delivery /platform-delivery
COPY --from=build --chown=65532:65532 --chmod=0755 /out/platform-module-publisher /platform-module-publisher
COPY --from=build --chown=65532:65532 --chmod=0755 /out/platform-grype-scanner /platform-grype-scanner
COPY --from=build --chown=65532:65532 --chmod=0755 /out/platform-operator /platform-operator
COPY --from=build --chown=65532:65532 --chmod=0755 /out/cosign /usr/local/bin/cosign
COPY --from=build --chown=65532:65532 --chmod=0770 /out/state /var/lib/platform-delivery
COPY --from=build /out/runtime/ /
ENV LISTEN_ADDRESS=:8081
ENV DATA_DIRECTORY=/var/lib/platform-delivery
ENV PATH=/usr/local/bin:/usr/bin:/bin
EXPOSE 8081
USER 65532:65532
ENTRYPOINT ["/platform-delivery"]
