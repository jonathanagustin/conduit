# Generated application deploy probe

`mise run appgen-deploy` builds the recorded `hello-http` application, pushes
the image, stamps its registry digest into the chart and the package manifest,
creates the package, deploys it on a new k3d or kind cluster, and requests the
Service. Success is HTTP 200 from `GET /` and from `GET /health`.

The probe is manual. The trunk Go lane is limited to 20 minutes and already
runs the race suite, the PostgreSQL gate, and the packaging gates. Creating a
cluster and initializing the package runner does not fit that budget, so the
workflow does not call this script.

## What the package contains

The generator accepts exactly one source file:

| Shape | Image |
| --- | --- |
| `main.go` | Non-root scratch image. The sandbox compiles the file. |
| `main.py` | Digest-pinned CPython 3.12 interpreter. The build copies `main.py` and has no network. |
| `index.html` | Non-root scratch image. The sandbox compiles a fixed server that serves the file at `/` and answers `GET /health` with `ok`. |

A second entrypoint is rejected. The error names the allowed set.

The chart renders a Deployment and a Service. The Deployment pins
`repository@sha256:…`, runs as uid 65532, sets the spec resource limits, and
uses `/health` for readiness and liveness. The package manifest installs that
chart and a Package resource. `spec.network.expose` names the Service, the
Service port, and the same pod selector the Service uses.

## Run

Install the engine, `zarf`, `kubectl`, and either `k3d` or `kind`. The engine
must accept an HTTP registry on `127.0.0.1:5001`. Then:

```sh
mise run appgen-deploy
```

The script creates and deletes a cluster named `conduit-appgen` and a registry
container named `conduit-appgen-registry`. Set `APPGEN_SHAPE` to `go`, `python`,
or `html` to choose the source shape. The default is `go`.

k3d is used when it is on `PATH`, and kind otherwise. The k3d cluster uses the
host-gw flannel backend, sets `failCgroupV1: false` so a cgroup v1 kubelet can
start, and clamps a negative oom score to zero before the runtime creates a
pod. It does not install the default DNS, metrics, or local-volume controllers,
so the only workload is the generated application. A cgroup v2 host still
accepts this cluster.

The cluster in this probe does not run a package controller. The Package
resource is still applied, and the HTTP check uses the Service that resource
exposes. Gateway routing requires the controller.
