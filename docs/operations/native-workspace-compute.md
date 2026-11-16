# Native workspace compute requires observed build evidence

Planning, NeedsInput, plan review and Accept remain available. Accept binds a plan;
it does not establish that an image was built or a Zarf archive was created.

`POST /v1/planning-sessions/{id}/compute` checks the caller's planning session and
accepted binding, then returns HTTP 501 with `COMPUTE_NOT_CONFIGURED` while the
Hub lacks an admitted image builder, inspector and package creator. A Git root,
model command or demo source directory does not configure those services.
Unauthorized callers and requests before Accept retain their existing failures.
No workspace, artifact digest, validation outcome or packaging-success audit is
created by the unconfigured request. The accepted binding remains available.

The former Hub implementation returned a fixed image digest and invented
inspection facts without building an image. Missing Zarf could produce a text
file named as a compressed package archive. Those adapters have been removed
from native composition; they are not evidence of a completed package.

The end-to-end Excalidraw packaging script now stops before starting processes,
rather than recording a synthetic native packaging success. Issue #615 still
requires a real pinned application build and packaging demonstration. The CLI's
separate `plan-compute` fixture path is not qualification of native compute or
proof of a real image build. A future native implementation must preserve the
accepted source, use observed immutable image/package coordinates, propagate
failed build/inspection/creation, and record success only after those checks.
