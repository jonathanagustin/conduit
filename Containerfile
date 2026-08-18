# syntax=docker/dockerfile:1.26@sha256:ecfaec9ed6d810b56388c508f4121597bfbba70d41a6dfeee4d8cad5f295fc32

FROM cgr.dev/chainguard/static:latest@sha256:399c8cb4858f05aaa33f43f02a2e75f28d40f016c0f86e5ba6075769e3303791 AS runtime-base

FROM --platform=$BUILDPLATFORM golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36 AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
WORKDIR /cosign-source
COPY tools/cosign-build/go.mod tools/cosign-build/go.sum ./
# Cosign 3.1.3 is the current reviewed release, but its published binaries were
# built with dependencies that the release scanner now rejects. Build the exact
# upstream command from the locked module graph instead, under the same patched
# Go toolchain as Conduit. The trusted-supply-chain gate holds both the upstream
# version and the security overrides below to their reviewed coordinates.
RUN go mod download && \
  mkdir -p /out && \
  CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false \
    -ldflags="-s -w" -o /out/cosign github.com/sigstore/cosign/v3/cmd/cosign
WORKDIR /source
COPY go.mod go.sum ./
COPY cmd cmd
COPY internal internal
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
