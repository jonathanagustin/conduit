# Packaging assessment prototype

Conduit-native, local-only first slice of #341. This command checks a supplied
assessment; the separate pinned-source discovery mode is described below. It does not verify assessment citations, invoke a model,
certify a deployable package, or authorize deployment. #342 owns the future UI/runtime.

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
selection, production packaging profiles, dependency/airgap/identity assessment,
Codex runtime integration and isolated live validation. No deployable package
is produced by these commands.

## Deterministic internal-only scaffold

`go run ./cmd/conduit-assess scaffold < input.json` emits a JSON envelope of
sorted artifact paths/content and a SHA-256 over their canonical JSON array.
It does not write files, invoke Helm/Zarf, contact Kubernetes, or deploy.
`internal/packaging/domain/testdata/scaffold-input.json` is a SYNTHETIC example;
its reserved example.invalid image and fabricated source hashes are not usable
release inputs. Input provenance remains operator-declared, not independently
verified by this command. Use the earlier pinned discovery evidence to fill it.

Only `internal-stateless/v1` is supported: one replica, one digest-pinned image,
an explicit unprivileged HTTP port, UID and resource budget. It emits separate
application and UDS config Helm charts and a Zarf package. Network is
**deny-by-default**, with no exposure or allow rules, so a Service object does
not mean the application is reachable. This follows the pinned core deny-all
fixture, not a complete application integration.

Unknown fields and unsupported pins fail closed. There are no secret/env inputs,
persistent volumes, arbitrary templates/scripts, public endpoints, automatic
Pepr code or database assumptions. GeoServer's durable data/admin-bootstrap
requirements mean it is OUTSIDE this stateless profile; its actual source trial
must not be marketed as a successful GeoServer package generation/deployment.
Nonroot compatibility, probes, identity, egress, supply-chain verification,
offline behavior and user/upgrade journeys remain required review work.

Pins and references:
- Generator profile: internal-stateless/v1. Its exact input is included in the
  artifacts, so changing source evidence, target pins or parameters changes the
  artifact digest. This digest is a review identifier, NOT a release signature. The verifier accepts trusted generated input, not arbitrary externally supplied templates.
- Zarf v0.65.1 commit d15db6ddad0fc2ea7255a8aaf2b0005a8c96c09a:
  https://raw.githubusercontent.com/zarf-dev/zarf/d15db6ddad0fc2ea7255a8aaf2b0005a8c96c09a/zarf.schema.json
  Save as zarf-v0.65.1.json. SHA-256
  7da2226c9c5deeed754eacd8c481f818a020b21a197ce3564705abd258b956f6.
  This version's images field contains STRINGS, unlike newer examples.
- UDS Core commit 9c808fd0af6bd0dca138da05f4a0d4785f109c32:
  https://raw.githubusercontent.com/defenseunicorns/uds-core/9c808fd0af6bd0dca138da05f4a0d4785f109c32/src/pepr/uds-cluster-crds/templates/packages.uds.dev.yaml
  Save as uds-package-v1alpha1.yaml. SHA-256
  57e898e5a8e66c69ac64640c4b7c0e0c8edbff0cd1333d9f632573f5bcc5bc3a.
  Precedent: src/test/app-curl-packages.yaml, curl-pkg-deny-all examples.
- Official template layout reviewed at
  defenseunicorns/uds-package-template@aa2668bcf0c169995a5ea361788fa3b7f3b6bfe3.
  No upstream template source is copied into generated charts.

Validation performed on the synthetic envelope:
`python scripts/verify-packaging-scaffold.py envelope.json --schema-dir CACHE --helm PATH_TO_HELM --helm-sha256 VERIFIED_BINARY_SHA256`
The cache must contain the two unmodified files above. The checker verifies their
hashes, checks the Zarf schema and JSON-schema-compatible UDS CRD constraints,
then runs actual Helm 3.19.0 strict lint and render on both charts in a temporary
directory. Use an isolated Python virtual environment installed from scripts/packaging-validator-requirements.txt and invoke its interpreter with -I. Both charts passed.
This is NOT Kubernetes admission/operator validation, Zarf package creation,
image pull/provenance verification or live workload testing. No cluster is used.

## Evidence-bound source-to-scaffold path

`go run ./cmd/conduit-assess plan ROOT COMMIT Dockerfile [SUPPORTING_FILE...]`
reads a bounded JSON request on stdin and emits the `reviewed-packaging/v1`
contract. First pass with `{}` returns a `needs-review` packet: selected source
path/blob/content hashes, an aggregate evidence digest, literal observations
and explicit blockers. Raw source text is not echoed; parsed literals are not a secret-redaction guarantee.

A subsequent request has two objects:
- `scaffold`: the strict input documented above. Its source revision and content
  hash must match the actual primary Git blob, not a supplied assertion alone.
- `review`: `evidenceSHA256` from that exact source set, plus `decisions`, each
  containing `topic`, `disposition` and a bounded nonempty `rationale`. Required
  topics are persistence, secrets, startup-downloads and identity. Dispositions
  are none, required or unknown; only an explicit none for all four is supported
  by this deliberately narrow internal-stateless profile.

The decision values are caller declarations, not proof a human reviewed them
and NOT deployment approval. Missing/stale decisions, changed source scope,
unknown/required capabilities, runtime VOLUME, missing/unresolved/mismatched
numeric USER, or a proposed port without final-stage literal TCP evidence yield
blockers with NO artifacts or proposal digest. Current planning conservatively
rejects multi-stage and other unsupported Dockerfile syntax even when discovery
can show partial facts. The final effective literal USER wins over intermediate
USER instructions. This is not a complete Dockerfile interpreter.

On success, the plan contains `scaffold-needs-validation` artifacts and a separate
`planSHA256` binding the evidence set, canonical review decisions and artifact
digest. Changing the rationale/source/configuration invalidates that identity;
reordering identical decisions does not. Existing Conduit approval/execution
controls remain authoritative. This command does not submit a delivery Plan or
create an approval. The lower-level `scaffold` command remains an explicit-input
renderer without this evidence-binding layer.

Verified end to end: an isolated synthetic Git repository was read, a review
packet issued, explicit test decisions supplied, and actual emitted artifacts
passed the pinned schema and Helm lint/render checker. The test identities and
example.invalid images are synthetic, not deployment candidates.

Real GeoServer negative trial: pinned revision above, selecting Dockerfile,
README.md and startup.sh, evidence SHA-256
`95247295415aed92bf40cafc55e85883c1cfd7333eec3628c2d1bc873a691db0`.
Source-backed review identified persistence, administrative-secret lifecycle,
unresolved startup extension downloads and missing UDS identity integration.
The workflow returned those four blockers plus unsupported Dockerfile syntax
and missing final numeric USER evidence. It emitted NO artifacts. This is an
honest coverage limit, not a successful GeoServer packaging result.

## Machine validation receipts (structural-only)

The checker now emits one `schema-helm/v1` JSON receipt on stdout and exits 0
only when every mandatory check passed. Failure exits 1 with the failed check's
exception category and unexecuted checks marked `not-run`; raw schema/tool
errors and their potentially sensitive arguments are not echoed. Inputs,
per-file contents, source schemas, subprocess output and runtime are bounded.
Helm receives a clean temporary configuration/plugin/cache environment and no
inherited Kubernetes config or application tokens. This is process/environment
isolation, NOT an OS security sandbox.

Receipt bindings: exact input bytes, canonical artifact digest, checksum-pinned
source schemas, actual validator script bytes, pinned Python dependency versions,
Helm version and a caller-supplied previously verified binary SHA-256. The tool
hash is checked before and after execution. Do not establish trust by hashing an
unknown executable and treating that same hash as verification; use a trusted
release acquisition first. Tested Windows Helm binary SHA-256:
`a18c49a4cd16f8b162031159eff6b4d657e04ec2df0c2be5544ad11ddf8fae79`.
The official Helm 3.19.0 archive checksum was verified before extraction.

Use a fresh venv plus `pip install -r scripts/packaging-validator-requirements.txt`;
invoke that venv's Python with `-I`. The exact installed dependency versions are
checked and recorded. `python -I -B scripts/test-packaging-validator.py` exercises
negative cases; its mocked tools are explicitly NOT live-validation evidence.

Actual isolated execution produced all-passed structural checks for the generated
end-to-end fixture. A separate actual CLI call with altered content returned
exit 1, artifact-binding failed and all later checks not-run. Nine unit/process
tests cover digest/schema/tool drift, wrong versions, malformed/template/path
inputs, output bounds, real process timeout, failed tools and diagnostic redaction.

**Authority limit:** receipts say `producer: local-process-unsigned`,
`scope: structural-only`, `deploymentAuthorized: false`. They are local process
observations, not independent cryptographic attestation or proof of human review.
There is deliberately no importer that accepts arbitrary supplied `passed` JSON
as a release gate. A future governed integration must bind the actual trusted
producer/run, freshness/policy, exact target and artifact identities through
existing Conduit controls. Image authenticity, package build, runtime/airgap
behavior and deployment permission are outside this validation profile.
