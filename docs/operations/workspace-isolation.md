# Isolated workspace container and default-deny egress

Each user's workspace runs in an isolated container on its own git branch (`container isolation now, stronger isolation planned`).

## Rootless builder and zero cluster credentials

Workspace containers run as non-root (`runAsNonRoot: true`, UID/GID `10001`), with a read-only root filesystem (`readOnlyRootFilesystem: true`), `allowPrivilegeEscalation: false`, `procMount: "Default"`, and all Linux capabilities dropped (`drop: ["ALL"]`). Container builds inside a workspace use rootless builder configuration (`BUILDKIT_ROOTLESS=1`, `BUILDAH_ISOLATION=chroot`, `STORAGE_DRIVER=vfs`) and never mount a host Docker daemon socket (`/var/run/docker.sock`) or `hostPath` volumes.

Cluster credentials are absent from the container:

- `automountServiceAccountToken: false` and `enableServiceLinks: false` are enforced on the Pod spec.
- `/var/run/secrets/kubernetes.io/serviceaccount`, `KUBECONFIG`, `KUBERNETES_SERVICE_HOST`, `KUBERNETES_SERVICE_PORT`, and `DOCKER_HOST` are rejected by container spec validation and stripped from the workspace process environment.

## Default-deny egress allowlist (`workspace.egress`)

Outbound network access from the workspace is default-deny. Only endpoints listed in the platform-operator-controlled `workspace.egress` configuration (plus cluster DNS on port 53 UDP/TCP) are permitted.

```yaml
workspace:
  egress:
    # Required; set per environment during bundle deploy. No default.
    gitHost: "git.example.internal"
    # Optional; defaults to the in-cluster registry service.
    registry: "registry.uds.svc.cluster.local:5000"
    allow:
      - name: "git-host"
        host: "git.example.internal"
        port: 443
        protocol: "TCP"
      - name: "local-registry"
        host: "registry.uds.svc.cluster.local"
        port: 5000
        protocol: "TCP"
```

Validation rules enforced by `domain.ParseEgressValuesYAML` and `domain.NormalizeAndValidateEgress`:

- `workspace.egress.gitHost` is required and has no default value. An empty or missing `gitHost` fails validation rather than falling back to allow-all.
- `workspace.egress.registry` defaults to the in-cluster registry service (`registry.uds.svc.cluster.local:5000`).
- When `workspace.egress.allow` is omitted, it defaults to two entries: `git-host` (`<gitHost>:443/TCP`) and `local-registry` (`registry.uds.svc.cluster.local:5000/TCP`).
- Wildcard hosts (`*`, `*.example.com`), allow-all CIDRs (`0.0.0.0/0`, `::/0`), unspecified IP addresses, and empty entries are rejected at validation time.
- Validated entries are rendered into both the workspace's UDS `Package` CR `spec.network.allow` section and its Kubernetes `NetworkPolicy`.

## Governance and change control

The egress allowlist is never read from inside the workspace container. Workspace users, the packaging workflow, and the coding-agent model cannot edit `workspace.egress` or modify the workspace's network policy (`ErrForbidden`, recorded in the authorization audit trail). Platform operators propose changes to `workspace.egress` through a reviewed change request (`ProposeEgressAllowlistChange`), which requires separate human approval and records `workspace.egress_config_change` in the audit trail.
