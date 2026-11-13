# Packaging assessment prototype

Conduit-native, local-only first slice of #341. This command checks a supplied
assessment; it does not discover source, verify citations, invoke a model,
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

Next: bounded revision-pinned source reader, actual evidence extraction and
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
