# Local model CLI

Set `CONDUIT_MODEL_CMD` to the argv of a local command. The documented default is `codex exec --skip-git-repo-check --sandbox read-only --ephemeral -`.

A model served over HTTP is configured with `CONDUIT_MODEL_BASE_URL` instead; see [HTTP model endpoint](model-http.md). The Hub refuses to start when both are set.

Leave `CONDUIT_MODEL_CMD` unset to keep the recorded proposals and the recorded HTTP application, and make no model call. App generation uses the same command and timeout. A confirmed request asks for one JSON spec, then the Hub renders the image definition, chart values, and package documents and runs the gates. The command does not approve a change. `CONDUIT_MODEL_TIMEOUT` defaults to 45s. Set `PACKAGING_FIXTURE_DIR` to the worker fixture directory so the prompt includes that run's chart values or resources excerpt, memory request, memory limit, image ref, and image digest. The Hub writes that packaging context to the command's standard input, validates one JSON object, and runs the same memory ceiling as the packaging gate. A passing proposal is stored as a change request that still needs a person. Credentials stay in the command's own configuration on the machine that runs the Hub.

## Environment allowlist

The model command does not inherit the Hub's environment. It receives only an allowlist: `PATH`, `HOME`, `USER`, the locale, `TZ` and temp-directory variables, the `XDG_*` base directories, the proxy and CA variables (`HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY`, `SSL_CERT_FILE`, `SSL_CERT_DIR`, `NODE_EXTRA_CA_CERTS`, `REQUESTS_CA_BUNDLE`, `CURL_CA_BUNDLE`), the provider variables the CLIs read (`ANTHROPIC_*` key, token, base URL and model, `CLAUDE_CODE_OAUTH_TOKEN`, `CLAUDE_CONFIG_DIR`, `OPENAI_API_KEY`, `OPENAI_BASE_URL`, `OPENAI_ORG_ID`, `CODEX_HOME`, `CODEX_API_KEY`, `GEMINI_API_KEY`, `GOOGLE_API_KEY`, `GOOGLE_APPLICATION_CREDENTIALS`, `GOOGLE_CLOUD_PROJECT`, `GOOGLE_CLOUD_LOCATION`, `GOOGLE_GENAI_USE_VERTEXAI`, `XAI_API_KEY`, `GROK_API_KEY`), and `CONDUIT_AGENT_PROVIDER`, `CONDUIT_MODEL_TIMEOUT` and `CONDUIT_PROVIDER_<NAME>_CMD` for `agent-exec`. Hub settings such as database DSNs, bundle signing keys, OIDC client secrets and notification credentials never reach it. To pass anything else (for example `AWS_PROFILE` and `AWS_REGION` for a Bedrock-backed CLI), set `CONDUIT_MODEL_ENV_ALLOW` on the Hub to a comma-separated list of exact variable names; the list is the operator's decision, so do not name a Hub secret in it. The exact list is `internal/localcmd/env.go`.

## Provider presets

`packaging-workflow agent-exec` is a ready-made `CONDUIT_MODEL_CMD`. Its `--provider` flag (or `CONDUIT_AGENT_PROVIDER`) chooses the engine:

| Provider | Delegates to | Default argv |
| --- | --- | --- |
| `deterministic` (default) | built-in offline agent | none |
| `codex` | Codex CLI | `codex exec --skip-git-repo-check --sandbox read-only --ephemeral -` |
| `claude` | Claude Code CLI | `claude -p --output-format text` |
| `gemini` | Gemini CLI | `gemini -p "<instruction>"` (prompt on stdin) |
| `grok` | Grok CLI | `grok -p` |

Example: `CONDUIT_MODEL_CMD="packaging-workflow agent-exec --provider claude"`.

Each turn's prompt goes to the CLI on stdin. Markdown fences are stripped and, where JSON is expected, the first balanced JSON object is extracted from surrounding prose. Replace any default argv with `CONDUIT_PROVIDER_<NAME>_CMD`. Credentials stay in each CLI's own configuration.

Only the `codex` argv comes from this document's existing contract. The `claude`, `gemini` and `grok` defaults are unverified against real installs, so confirm them with a local run before relying on them. The adapter is covered by tests that use a fake CLI.

## Workspace editor ACP agent

The Hub's workspace ACP bridge (`DEVWORKSPACE_ACP_BRIDGE=node`) launches one coding agent that speaks ACP over stdio. Choose it with `DEVWORKSPACE_ACP_AGENT`:

| Value | Launches |
| --- | --- |
| `codex` (default) | the bundled `codex-acp` |
| `claude` | `claude-code-acp` |
| `gemini` | `gemini --experimental-acp` |
| `grok` | no preset; requires `DEVWORKSPACE_ACP_AGENT_CMD` |

`DEVWORKSPACE_ACP_AGENT_CMD` replaces any preset with an explicit whitespace-separated command. An unknown or unusable setting disables the bridge with a log line; it never substitutes a simulated agent. Every file write and command still passes through the Hub's governed permission mapping, so the choice of agent adds no authority. Only the `codex` default is exercised here; the `claude` and `gemini` presets are unverified against real installs.

## Post-Accept compute

A model command configures who writes the Dockerfile and package files. It does
not admit an image builder. Set `CONDUIT_IMAGE_BUILDER=docker` and
`DEVWORKSPACE_GIT_ROOT` to run the governed build after Accept. Leave
`CONDUIT_MODEL_CMD` unset, or set it to `packaging-workflow agent-exec`, for the
deterministic writer. A live model is the same variable pointed at that CLI,
for example `CONDUIT_MODEL_CMD="packaging-workflow agent-exec --provider claude"`.
The Hub still refuses to invent an image digest when the builder is not
admitted. See [native workspace compute](native-workspace-compute.md).
