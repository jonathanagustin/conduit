# Local model CLI

Set `CONDUIT_MODEL_CMD` to the explicit argv of an installed local command. Conduit supplies no vendor command defaults.

A model served over HTTP is configured with `CONDUIT_MODEL_BASE_URL` instead; see [HTTP model endpoint](model-http.md). The Hub refuses to start when both are set.

Leave `CONDUIT_MODEL_CMD` unset to keep the recorded proposals and the recorded HTTP application, and make no model call. App generation uses the same command and timeout. A confirmed request asks for one JSON spec, then the Hub renders the image definition, chart values, and package documents and runs the gates. The command does not approve a change. `CONDUIT_MODEL_TIMEOUT` defaults to 45s. Set `PACKAGING_FIXTURE_DIR` to the worker fixture directory so the prompt includes that run's chart values or resources excerpt, memory request, memory limit, image ref, and image digest. The Hub writes that packaging context to the command's standard input, validates one JSON object, and runs the same memory ceiling as the packaging gate. A passing proposal is stored as a change request that still needs a person. Credentials stay in the command's own configuration on the machine that runs the Hub.

## Environment allowlist

The model command does not inherit the Hub's environment. It receives only an allowlist: `PATH`, `HOME`, `USER`, the locale, `TZ` and temp-directory variables, the `XDG_*` base directories, the proxy and CA variables (`HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY`, `SSL_CERT_FILE`, `SSL_CERT_DIR`, `NODE_EXTRA_CA_CERTS`, `REQUESTS_CA_BUNDLE`, `CURL_CA_BUNDLE`), the provider variables the CLIs read (`ANTHROPIC_*` key, token, base URL and model, `CLAUDE_CODE_OAUTH_TOKEN`, `CLAUDE_CONFIG_DIR`, `OPENAI_API_KEY`, `OPENAI_BASE_URL`, `OPENAI_ORG_ID`, `CODEX_HOME`, `CODEX_API_KEY`, `GEMINI_API_KEY`, `GOOGLE_API_KEY`, `GOOGLE_APPLICATION_CREDENTIALS`, `GOOGLE_CLOUD_PROJECT`, `GOOGLE_CLOUD_LOCATION`, `GOOGLE_GENAI_USE_VERTEXAI`, `XAI_API_KEY`, `GROK_API_KEY`), and `CONDUIT_AGENT_PROVIDER`, `CONDUIT_MODEL_TIMEOUT` and `CONDUIT_PROVIDER_<NAME>_CMD` for `agent-exec`. Hub settings such as database DSNs, bundle signing keys, OIDC client secrets and notification credentials are excluded by default. To pass anything else (for example `AWS_PROFILE` and `AWS_REGION` for a Bedrock-backed CLI), set `CONDUIT_MODEL_ENV_ALLOW` on the Hub to a comma-separated list of exact variable names; the list is the operator's decision, so do not name a Hub secret in it. The exact list is `internal/localcmd/env.go`.

## Provider presets

Every delegated provider call uses the same environment allowlist and temporary
working directory as a direct model command, including when `agent-exec` is run
on its own. Provider stderr is withheld from returned errors because it can
contain credentials or source content. A timeout still reports that the command
timed out.

These controls do not confine the command's filesystem or network access. A local
command runs as the Hub's operating-system user and can read files available to
that user, including mounted credentials; a CLI's read-only mode does not hide
those files. The allowlist extension is trusted operator configuration and can
explicitly pass a Hub secret. Use the existing HTTP model transport when the
model must have no local process access. A container-backed command transport
remains outstanding under [#809](https://github.com/jonathanagustin/conduit/issues/809).

Planning inspection metadata and filenames, as well as packaging tool results
(including image inspection failures), are carried in escaped untrusted-data
blocks. Filenames retain their exact JSON string value even when they contain
newlines or delimiter text. This framing helps the model distinguish evidence
from instructions; it grants no execution or approval authority.

`packaging-workflow agent-exec` has one implicit provider:

| Provider | Delegates to | Default argv | Status | Evidence |
| --- | --- | --- | --- | --- |
| `deterministic` (default) | built-in offline agent | none | VERIFIED | Exercised by the `cmd/packaging-workflow` tests in CI. |

The former Codex, Claude, Gemini and Grok command defaults were removed because
this repository has no recorded conformance run against those real CLIs. A
fake-CLI test verifies process plumbing and does not establish vendor support.
The table is `Presets` in `internal/localcmd/presets.go`.

To delegate a turn, choose an operator-defined label with `--provider` (or
`CONDUIT_AGENT_PROVIDER`) and supply its exact command in
`CONDUIT_PROVIDER_<NAME>_CMD`. For example, `--provider custom` requires
`CONDUIT_PROVIDER_CUSTOM_CMD`. Existing explicit vendor-labelled commands
continue to work; selecting a label without a command fails with the missing
setting's name. Quoted argv is parsed without shell expansion. Credentials
stay in the CLI's configuration. Confirm the command with your installed
version before relying on it.

Each turn's prompt goes to the CLI on stdin. Markdown fences are stripped and,
where JSON is expected, the first balanced JSON object is extracted from prose.

## Model name

A model CLI does not tell the Hub which model it ran. Set `CONDUIT_MODEL_NAME` to the name you configured in the CLI; the Hub binds that name at Accept and writes it to the audit event as `configuredModel`. With `CONDUIT_MODEL_NAME` unset the binding reads `unverified (model CLI, CONDUIT_MODEL_NAME not set)`. The Hub never takes the name from the model's reply: a `"model"` field in a reply is model-authored text and is ignored. The name is the operator's statement, so the Hub records it and does not check it against the CLI. An [HTTP model endpoint](model-http.md) also records the id the provider returned.

## Per-capability selection

The global `CONDUIT_MODEL_NAME`, `CONDUIT_MODEL_CMD` and
`CONDUIT_MODEL_TIMEOUT` remain the defaults. Set
`CONDUIT_<CAPABILITY>_MODEL_NAME`, `_CMD` or `_TIMEOUT` for
`ASSISTANT`, `PLANNING`, `PACKAGING` or `APPGEN` to override that
capability. An HTTP name is sent as the model id to the same endpoint and key.
An explicit capability command selects CLI execution for that capability,
shadowing the global HTTP transport. For a CLI, select the actual model in its
argv and set the name to the operator assertion you want recorded; changing a
name does not invent a vendor-specific command-line flag. There is no
capability-specific endpoint or credential override.

Planning sessions and immutable Accept bindings retain separate
`configuredModel` and `providerModel` fields alongside the existing display
label, and the Accept audit carries both. Legacy records leave the new fields
absent. A provider that omits its id leaves it absent for that call; a previous
call cannot fill it in. The console shows the serving id and configuration
separately, including when the provider id is unavailable.

The shipped HTTP-only chart exposes `model.capabilities.<capability>.name`
and `.timeout` for the same four lower-case capability names. The Zarf
package exposes `<CAPABILITY>_MODEL_NAME` and `<CAPABILITY>_MODEL_TIMEOUT`.
Empty values inherit the global setting. Both continue sharing the configured
endpoint and mounted key. Neither shipped artifact supplies a local model CLI.

## Workspace editor ACP agent

The Hub's workspace ACP bridge (`DEVWORKSPACE_ACP_BRIDGE=node`) launches one coding agent that speaks ACP over stdio. Choose it with `DEVWORKSPACE_ACP_AGENT`:

The empty setting or `codex` selects the bundled `codex-acp`. Its recorded
real-executable check covers ACP initialization and protocol negotiation only;
it does not establish authenticated model, editing, terminal or cancellation
conformance. Every other agent requires `DEVWORKSPACE_ACP_AGENT_CMD` with explicit
argv for an ACP-speaking command. The unverified Claude and Gemini implicit
launch commands were removed. An unknown or unusable setting disables the
bridge with a log line; it never substitutes a simulated agent. Every file
write and command still passes through the Hub's governed permission mapping.

After installing the locked web dependencies, run
`node scripts/verify-codex-acp-initialize.mjs` from the repository root to repeat
the bounded smoke. It launches the installed adapter and Codex subprocess with
fresh temporary profile directories and no inherited credentials or
configuration, sends one `initialize` request, requires the pinned protocol and
adapter identity, then terminates its process group. It sends no authentication,
session or prompt request. The [recorded receipt](../evidence/codex-acp-initialize.json)
identifies the exact adapter, Codex and Node versions, lockfile and adapter
digests, request, and response. FakeACP tests cover Conduit's bridge behavior
separately and do not prove vendor conformance.

## Post-Accept compute

A model command configures who writes the Dockerfile and package files. It does
not admit an image builder. Set `CONDUIT_IMAGE_BUILDER=docker` and
`DEVWORKSPACE_GIT_ROOT` to run the governed build after Accept. Leave
`CONDUIT_MODEL_CMD` unset, or set it to `packaging-workflow agent-exec`, for the
deterministic writer. A live model is the same variable pointed at that CLI,
with an explicitly configured provider command as described above.
The Hub still refuses to invent an image digest when the builder is not
admitted. See [native workspace compute](native-workspace-compute.md).
