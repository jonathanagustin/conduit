---
name: packaging
description: Author a package from a product source. Covers containerization, the two-file package layout, digest-pinned images, probes, resource limits, a non-root security context, what the deterministic gates reject, and one static web container example.
when_to_use: Use when writing a Dockerfile for an application, proposing packaging around a digest-pinned image, writing Kubernetes manifests or package config, or checking a proposal against the deterministic gates.
---

# Packaging

This skill is the packaging convention a proposer must follow. The control plane records the sha256 of this file next to the prompt and response digests of each proposal.

## Package layout

A packaging run reads a product source and writes two files:

- `zarf.yaml` — the package config (`apiVersion: zarf.dev/v1alpha1`, `kind: ZarfPackageConfig`).
- `uds-package.yaml` — the custom resource the package installs (`apiVersion: uds.dev/v1alpha1`, `kind: Package`).

The artifact digest covers both files, in that order, separated by a NUL byte, so one file cannot be swapped for the other. Rendered output lives at `out/<run-id>/zarf.yaml` and `out/<run-id>/uds-package.yaml`.

The product source is a small line-oriented document (`key: value`, `#` comments allowed). `name` is required. Optional keys the renderer understands are `port` (default 8080), `version` (default `0.0.1`), `image`, `mesh` (`ambient` or `sidecar`, default `ambient`), `sso`, `network`, and `monitor`. A section that is absent gets the registered default; `sso: none`, `network: none`, and `monitor: none` omit that section. The source may also carry `memory`, `memory-request`, `chart-values`, `resources`, `image-ref`, `image-digest`, `kind`, `requirement`, `security-evidence`, `lint`, `proposal`, and `rationale` lines; the renderer ignores those keys, and the gates read `memory`, `requirement`, `security-evidence`, and `lint`.

An `image` value must be a repository path with a digest pin: a name that includes at least one `/`, an optional tag, and `@sha256:` followed by 64 lowercase hex characters. The source must be at most 64 KiB and 64 lines. Reserved names (`kube-system`, `kube-public`, `kube-node-lease`, `istio-system`, `zarf`, `uds-system`, `pepr-system`) are rejected.

## Conventions

When writing Kubernetes manifests around a pinned image:

- Pin every image by digest (`@sha256:` plus 64 lowercase hex characters). Do not leave a tag-only reference.
- Set readiness and liveness probes (HTTP or TCP) on the application container.
- Set memory and CPU requests and limits. The packaging memory ceiling is 1Gi; a memory limit must not exceed 1Gi.
- Run as non-root. Set `securityContext.runAsNonRoot: true`, `allowPrivilegeEscalation: false`, and `capabilities.drop: ["ALL"]`. Set `readOnlyRootFilesystem: true` where the application can run that way (a static web container can). Prefer a numeric `runAsUser` and `runAsGroup` other than 0.

## Containerization

When writing a Dockerfile for a product source, follow these conventions. The Dockerfile gate checks them after the package lint and render check when a Dockerfile is present.

- Prefer a multi-stage build: compile or assemble in an earlier stage, and copy only the runtime artifacts into a minimal final image.
- Pin every `FROM` image by digest (`@sha256:` plus 64 lowercase hex characters). `FROM scratch` is the only unpinned exception.
- Run the final stage as a non-root user. Set `USER` to a numeric uid other than 0. Do not use `USER root` or `USER 0`.
- Keep the final stage minimal: do not install compilers or package managers in the runtime image when a prior stage can produce the artifacts.
- Serve a static site with an unprivileged web server that listens on a high port (1024 or above). Do not expose privileged ports.
- Do not copy secrets, credentials, private keys, or `.env` files into the image, and do not bake secret values into `ENV` or `ARG`.
- Include a `.dockerignore` that excludes VCS metadata, dependency caches, build output, and secret files.

If the repository already ships a Dockerfile, reuse it only when it already meets these conventions, and explain why it is being reused. Otherwise write a new Dockerfile.

### Worked example: a static site Dockerfile

```
FROM registry.example/base/node@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa AS build
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM registry.example/base/unprivileged-http@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
COPY --from=build --chown=65532:65532 /src/dist /srv
USER 65532:65532
EXPOSE 8080
CMD ["/usr/bin/http-server", "/srv"]
```

A matching `.dockerignore` excludes `.git`, `node_modules`, `dist`, `.env`, and `*.pem`.

## Deterministic gates

The package gate is the lint and render check. Unknown security evidence and unsupported requirements are terminal failures, not passes and not retries. It applies these rules, in order:

1. If the source contains the bytes `requirement: unsupported`, the outcome is `UNSUPPORTED` with reason `application requirement is unsupported`.
2. If the source contains the bytes `security-evidence: unknown`, the outcome is `UNKNOWN_SECURITY` with reason `security evidence is unknown`.
3. If the source contains the bytes `lint: fail`, the outcome is `FAIL_REQUIRED` with reason `required lint failed`.
4. If the source has a `memory:` value, it is scored with the packaging memory ceiling (1024 MiB). A value that is not a size in `Mi` or `Gi` fails with reason `memory limit is not a size`. A value above 1Gi fails with reason `memory limit <value> exceeds the 1Gi ceiling`.
5. Otherwise the source is rendered. Invalid declared needs fail with reason `declared needs are invalid`. If the bytes of `zarf.yaml` or `uds-package.yaml` do not match the render, the outcome is `FAIL_REQUIRED` with reason `rendered package failed check`.
6. If the render matches, the outcome is `PASS` with reason `lint and render passed`.

A later digest mismatch against the proposal fails with reason `validation digest does not match the proposal`.

When a Dockerfile is present, the Dockerfile gate runs after a package lint and render pass, and applies these rules in order:

1. If any `FROM` line other than `scratch` is not pinned by `@sha256:` plus 64 lowercase hex characters, the outcome is `FAIL_REQUIRED` with reason `dockerfile: base image must be pinned by a sha256 digest`.
2. If the final stage has no `USER`, or the user is `root` or `0`, the outcome is `FAIL_REQUIRED` with reason `dockerfile: final stage must run as a non-root user`.
3. If the Dockerfile copies secret material or bakes a secret value into `ENV` or `ARG`, the outcome is `FAIL_REQUIRED` with reason `dockerfile: must not include secrets`.
4. If an `EXPOSE` lists a port below 1024, the outcome is `FAIL_REQUIRED` with reason `dockerfile: must listen on a high port`.
5. If a `.dockerignore` is missing, the outcome is `FAIL_REQUIRED` with reason `dockerfile: a dockerignore is required`.

A missing or empty Dockerfile is not checked by this gate. A package gate failure is returned as-is and is not replaced by a later Dockerfile reason.

Live model proposals are also scored with the same memory ceiling before a change request is opened: the proposer must reply with one JSON object whose `field` is `"memory"`, `value` is a size in `Mi` or `Gi`, and `rationale` is one sentence. The gate rule a proposer is told is `memory limit must not exceed 1Gi`.

## Worked example: a static web container

Product source:

```
name: static-web
port: 8080
version: 1.0.0
image: registry.example/apps/static-web:1.0.0@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
memory-request: 32Mi
memory: 128Mi
sso: none
```

`memory: 128Mi` is within the 1Gi ceiling, so the memory gate passes. The renderer writes `zarf.yaml` and `uds-package.yaml` for `static-web`, pinning that image digest in the package config.

Kubernetes manifests around the same digest should look like this:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: static-web
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: static-web
  template:
    metadata:
      labels:
        app.kubernetes.io/name: static-web
    spec:
      securityContext:
        runAsNonRoot: true
        runAsUser: 65532
        runAsGroup: 65532
        fsGroup: 65532
      containers:
        - name: web
          image: registry.example/apps/static-web:1.0.0@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
          ports:
            - name: http
              containerPort: 8080
          resources:
            requests:
              cpu: 50m
              memory: 32Mi
            limits:
              cpu: 200m
              memory: 128Mi
          readinessProbe:
            httpGet:
              path: /
              port: http
          livenessProbe:
            httpGet:
              path: /
              port: http
          securityContext:
            runAsNonRoot: true
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
          volumeMounts:
            - name: tmp
              mountPath: /tmp
      volumes:
        - name: tmp
          emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: static-web
spec:
  selector:
    app.kubernetes.io/name: static-web
  ports:
    - name: http
      port: 8080
      targetPort: http
```
