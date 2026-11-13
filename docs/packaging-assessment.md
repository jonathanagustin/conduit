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

`python3 scripts/test-packaging-gates.py` is the shared CI/local regression gate.
It requires Python 3.12, creates a temporary isolated venv, installs the exact
requirements pins and runs both validator and Codex-protocol regression suites.
Any install/test failure or timeout fails the gate. The Trunk Go lane calls it
under the existing Go-relevant scope; scripts/ and workflow edits select that
lane. `verify-local.sh` calls the same command and includes Python scripts in
its reverse CI-coverage check. No new permissions, Actions, model calls or
credential requirements are introduced. Existing head authentication, draft
skip policy, concurrency and runtime limits remain unchanged. Draft CI can
therefore still skip; wiring is not evidence that hosted CI ran or passed.

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

## Codex runtime feasibility and handshake-only preflight

Current main and the separate #342 UI branch were inspected read-only before
this step. UI remains fixture-only/unavailable and is not changed here. The
machine's global Codex was 0.166.0 while mise.toml pins 0.148.0. Rather than
silently change the repository contract, an official @openai/codex@0.148.0 copy
was acquired in an isolated temporary prefix with lifecycle scripts disabled.
No global installation, personal configuration or stored authentication was
copied/changed. Official documentation consulted:
https://learn.chatgpt.com/docs/app-server .

The exact executable generated its JSON schemas. Crucial mismatch: current
online documentation describes restricted read roots, but the pinned 0.148.0
TurnStartParams SandboxPolicy readOnly variant only exposes type/networkAccess,
not restricted readable roots. Read-only is therefore NOT enough evidence that
an agent cannot read unrelated host files. No model turn was launched on that
assumption, and no sandbox/security setting was changed.

Generated schema hashes (SHA-256):
- v1/InitializeParams.json:
  6f0094be9a65242ec779a40794cbd4fdfa32fca1e45084a16adfb50501d33ea2
- v1/InitializeResponse.json:
  62ad689c2cb6379913c1d72749cfd8de5089d35760214123518eb92eef11acc9
- v2/TurnStartParams.json:
  ff2e7e0796fbe2ad99e5ec7d489cc8c8630b75f2ab8f17857711107587e3197d

`scripts/probe-codex-runtime.py --executable PATH --sha256 HASH` accepts only the
reviewed official Windows 0.148.0 executable, SHA-256
`2ad2cf8a732da68b8f141634f92db1a03016c5faf533a7225fbc0fb740130410`.
It uses fresh HOME/CODEX_HOME/workspace/config directories and a filtered process
environment. It sends ONLY initialize and initialized with experimentalApi=false.
No thread/start, turn/start, login, model request, MCP tool or approval response
is sent. The actual server-reported home must match the isolated directory.

Protocol output is bounded through shutdown. Any server request, malformed/extra
response, unknown notification, capture failure, nonzero exit, timeout or cleanup
failure blocks success. The one observed startup notification,
remoteControl/status/changed, is accepted only with disabled status and no remote
environment. Its private identity fields are never echoed. This is a process
preflight, NOT a security sandbox or operational agent adapter.

Actual verified result: handshake-verified-model-disabled, remoteControl=disabled,
runtimeAvailable=false, threadStarted=false, turnStarted=false. Eight protocol /
orchestration tests pass, covering wrong IDs/home/binaries, duplicate/oversized
frames, credential exclusion, extra approvals, bad exits, timeout, overflow,
cleanup failures and connected remote-control state. Test command:
`python -I -B scripts/test-codex-probe.py`.

**Live runtime blocker:** establish an independently confined inference
workspace (or explicitly review a different pinned protocol and verify its
platform enforcement), plus a dedicated authorized authentication flow. Merely
setting cwd or read-only does not prove that scope. No HTTP runtime endpoint,
backend-owned agent session, real model response, tool bridge or UI integration
is implemented by this probe. #342 remains open; UI availability must stay false.

## Existing-resource scaffold profile

`internal-existing-resources/v1` is a standalone, operator-selected scaffold
profile, not an expansion of the reviewed-plan contract. It requires one or both:

- `existingClaim: {"name":"sample-data","mountPath":"/opt/geoserver_data"}`
- `existingSecretFile: {"name":"sample-admin","key":"password","env":"GEOSERVER_ADMIN_PASSWORD_FILE"}`

Names are bounded same-namespace DNS labels. A data mount is exactly one bounded
path component beneath `/opt/` or `/var/lib/`; traversal, nested paths, Windows
paths and template expressions are rejected. The secret projects only the named
key to `/run/secrets/conduit/value`, read-only and nonoptional. Its mode is 0444,
readable to container UIDs. Only a validated uppercase `*_FILE` variable is emitted,
with that fixed filename, never a credential value or arbitrary environment value.

The output retains one replica, deny-by-default networking and the existing
nonroot restrictions; Deployment strategy is Recreate. No PVC or Secret is
created, fetched or adopted. No hostPath, subPath, init container, fsGroup, chown,
root workaround, credential generation or deployment is performed. Recreate does
not establish exclusive PVC ownership or zero downtime. References bind names,
not immutable live resource identities. PVC access mode, binding, permissions,
backup/rollback, mount shadowing, secret rotation and application `*_FILE` support
remain unverified.

Zarf 0.65.1 and UDS Core pins and their emitted contracts remain unchanged. The
additional fields use Kubernetes Deployment volume/reference shapes; Helm/schema
checks do not prove Kubernetes admission or runtime behavior. No live test is
claimed. Existing stateless inputs retain their serialized shape and digest;
stateless rejects resource references. The evidence-to-plan path explicitly
rejects this profile and any resource references until a separate lifecycle-aware
review contract exists. GeoServer bootstrap/admin lifecycle, startup downloads,
identity and nonroot compatibility therefore remain blockers, not silently
resolved by a Secret/PVC reference.

Verified synthetic resource-profile fixture: all ten `schema-helm/v1` checks
passed using the pinned schemas and real Helm 3.19.0 lint/render. Artifact digest:
`55742cc066f4648e1a1844bf4c4722d0ba1b6549a28f8beb92914fdfdf03ed57`.
The receipt remains unsigned, structural-only and deploymentAuthorized=false.
Focused Go tests/vet and Zenith/domain gates pass; independent review checked
path bounds, selected-key projection, reference digest changes and profile
separation. No GeoServer deployment, credential lifecycle, PVC or admission test
is implied by this synthetic result.
