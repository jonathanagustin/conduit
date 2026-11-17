# Native workspace compute requires observed build evidence

Planning, NeedsInput, plan review and Accept remain available. Accept binds a plan;
it does not establish that an image was built or a Zarf archive was created.

`POST /v1/planning-sessions/{id}/compute` checks the caller's planning session and
accepted binding. It returns HTTP 501 with `COMPUTE_NOT_CONFIGURED` until
`CONDUIT_IMAGE_BUILDER=docker` admits the local engine. A Git root, a model
command, or a demo source directory does not admit a builder by itself.
Unauthorized callers and requests before Accept retain their existing failures.
No workspace, artifact digest, validation outcome or packaging-success audit is
created by the unconfigured request. The accepted binding remains available.

The former Hub implementation returned a fixed image digest and invented
inspection facts without building an image. Missing Zarf could produce a text
file named as a compressed package archive. Those adapters have been removed
from native composition; they are not evidence of a completed package.

With `CONDUIT_IMAGE_BUILDER=docker` and `DEVWORKSPACE_GIT_ROOT` set, an accepted
onboard session clones the bound repository into a workspace branch, commits a
Dockerfile written for that workspace, builds it with the local engine, and
records the engine image id. `zarf dev lint` and `zarf package create` then run
against the committed `zarf.yaml` and Package CR. Success is recorded only when
the image id, both gates, and the package archive are present. A missing engine
or `zarf` binary fails the run. The image id is the pin; it is not a registry
manifest digest unless the image was pushed.

`CONDUIT_MODEL_BASE_URL` selects an HTTP model to write the Dockerfile and
package files. Without an endpoint the existing deterministic writer is used.
All global and capability model command settings are refused; the Hub never
starts a model subprocess.
`CONDUIT_PACKAGING_BUILD=source` builds with a pulled `node:22-bookworm` base
pinned by the repo digest the engine reports. A repository URL containing
`excalidraw` uses that source build unless `CONDUIT_PACKAGING_BUILD=scratch`.

`scripts/governed-packaging-run.sh` drives the path. `CONDUIT_HTTP_WRITE_TIMEOUT`
must cover a source build; the Hub's default write timeout is 30s.

The CLI `plan-compute` fixture path is not this builder. It remains a separate
local stand-in and does not qualify a native image.
