# Local model CLI

Set `CONDUIT_MODEL_CMD` to the argv of a local command. The documented default is `codex exec --skip-git-repo-check --sandbox read-only --ephemeral -`.

Leave `CONDUIT_MODEL_CMD` unset to keep the recorded proposals and the recorded HTTP application, and make no model call. App generation uses the same command and timeout. A confirmed request asks for one JSON spec, then the Hub renders the image definition, chart values, and package documents and runs the gates. The command does not approve a change. `CONDUIT_MODEL_TIMEOUT` defaults to 45s. Set `PACKAGING_FIXTURE_DIR` to the worker fixture directory so the prompt includes that run's chart values or resources excerpt, memory request, memory limit, image ref, and image digest. The Hub writes that packaging context to the command's standard input, validates one JSON object, and runs the same memory ceiling as the packaging gate. A passing proposal is stored as a change request that still needs a person. Credentials stay in the command's own configuration on the machine that runs the Hub.

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
