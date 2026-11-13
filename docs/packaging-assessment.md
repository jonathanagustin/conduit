# Packaging assessment prototype

Conduit-native, local-only first slice of #341. This command checks a supplied
assessment; the separate pinned-source discovery mode is described below. It does not verify assessment citations, invoke a model,
generate a package, or authorize deployment. #342 owns the future UI/runtime.

Run `go run ./cmd/conduit-assess < assessment.json` from a shell supporting stdin
redirection. Input is limited to 128 KiB and unknown fields are rejected.

```json
{"revision":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","udsVersion":"1.15.0","findings":[]}
```

The example revision is synthetic. Empty findings produce eight explicit
missing-evidence blockers. Each finding has topic, summary, basis and evidence
(path and positive line). Topics are build, images, network, persistence,
identity, privileges, airgap and licensing. Basis is source-observed,
reference-derived, inferred or unknown. Source-observed requires citations.
Only source-observed findings can remove a completeness blocker; even a complete
report is `reviewable-not-validated`, never a package-readiness certificate.
Citation paths are lexical checks only: no file is opened, and source existence,
symlink safety, content validity and provenance still require the future reader.
Do not submit credentials or secret contents in summaries.

The requirement categories are a local review contract, not a UDS schema.
The generator must pin a target UDS version and corresponding template/schema,
prefer suitable upstream Helm charts, use UDS Package declarations for integration,
and retain application-specific output in its product repository.

Sources:
- https://docs.defenseunicorns.com/core/v1-15/concepts/configuration--packaging/package-requirements/
- https://docs.defenseunicorns.com/core/v1-14/how-to-guides/packaging-applications/create-uds-package/

Next: broader evidence extraction and
isolated artifact generation. No live app-server, auth configuration, shell
execution of upstream content, or deployment is part of this slice.

Gate: `go test ./internal/packaging/... ./cmd/conduit-assess`.

## Bounded source evidence

`go run ./cmd/conduit-assess evidence ROOT README.md Dockerfile` reads only the
explicit relative paths (1-16), at most 64 KiB each. It uses os.Root to prevent
symlink escapes, rejects non-regular/binary files, hashes exact bytes, and emits
`source-evidence-not-assessed`. Select an authorized immutable checkout yourself;
the reader does not prove a Git revision, redact arbitrary secrets, or execute
source. No partial JSON is returned when any requested file fails.

Source path and content limits and a real symlink-escape rejection are tested.
This collection step is independent of the supplied-assessment completeness
checker: it does not yet turn source into a semantic assessment.

## GeoServer evaluation plan (not an executed integration test)

Official 3.0.x Docker documentation identifies HTTP on 8080, a persistent
/opt/geoserver_data directory and optional extension downloads during startup.
Those downloads must be accounted for before a disconnected-package claim.
Production guidance warns that environment information can expose secrets.
Sources reviewed: https://docs.geoserver.org/3.0.x/en/user/installation/docker/
and https://docs.geoserver.org/stable/en/user/production/config/ . These rolling
pages are evaluator guidance, not immutable application evidence.

Freeze an upstream revision, intended capabilities, data fixture and expected
requirements before any generation trial. Verify persistence, extension/image
completeness, ingress and administrative access separately. Do not transplant
TAK's Pepr adapters or device-test assumptions. No GeoServer image was run, no
cluster was created and no result is claimed here. Because these requirements
were reviewed during development, a future run must disclose that exposure;
it is not a fully blind benchmark.

## Revision-pinned discovery milestone

`go run ./cmd/conduit-assess commit-evidence ROOT COMMIT FILE...` reads regular
Git blobs directly from an existing full commit ID. Dirty worktrees and index
changes cannot alter the snapshot. Replacement objects, lazy fetching, global
Git configuration and inherited Git environment overrides are disabled. No
checkout, filters, hooks, remote acquisition or repository scripts run. Commit
and blob IDs plus SHA-256 bind the selected bytes. Git must already be installed
and the objects must already exist locally. Explicit file selection is still
required and arbitrary source text is not automatically secret-redacted.

`go run ./cmd/conduit-assess discover ROOT COMMIT Dockerfile` performs a narrow,
deterministic literal pass and emits source path/line/stage facts without raw
source text. Supported observations: literal base images, EXPOSE ports,
JSON-array VOLUME paths and USER declarations. These are not runtime guarantees.
Build arguments, continuation expressions, heredocs and alternate escape syntax
are not interpreted. Stage-scoped observations are never merged into a false
final-image claim. Status stays `needs-assessment`.

Actual source trial: official `geoserver/docker` commit
`a82a7782c670e0bb969c197aceea3549a726e0a3`, Dockerfile blob
`2c1d2a2a901e4392e02f08e1e117b1d21fa2129b`, SHA-256
`8908204361efe6d46648cb04ea75538191cd67ca0f80a57ca28d54027ed9ad93`.
The CLI reported `root` at line 30 in build stage 1 and `8080` at line 345 in
stage 2, with unresolved base-image expressions and multiline warnings. This
was source inspection, not a container test or blind benchmark. No health path,
PostgreSQL dependency, nonroot startup guarantee or deployment readiness was
invented. The synthetic GeoServer-informed acceptance test is labeled as such.

Remaining: full application evidence extraction, artifact/schema version
selection, Helm/Zarf/UDS generation, dependency/airgap/identity assessment,
Codex runtime integration and isolated live validation. No deployable package
is produced by these commands.
