# Model transport migration

Host model subprocesses are retired. Every nonempty `CONDUIT_MODEL_CMD` or `CONDUIT_<CAPABILITY>_MODEL_CMD` setting is an explicit configuration error, with or without an HTTP endpoint. The Hub and standalone worker refuse it before reading model credentials or selecting an offline path. Commands are never silently ignored or replaced by HTTP.

Use the existing [HTTP model endpoint](model-http.md), including a separately operated local endpoint, for model inference. Endpoint credentials and trust belong to that endpoint. The Hub sends bounded request/response messages and retains configured/provider model provenance; no provider process runs under the Hub's UID. The endpoint's own execution and isolation policy is its operator's responsibility.

With no endpoint configured, existing recorded proposals/application and deterministic package-writing paths remain offline. Planning and assistant turns remain unavailable where no offline adapter is configured. HTTP errors do not select an offline response automatically.

## Per-capability selection

`CONDUIT_ASSISTANT_MODEL_NAME`, `CONDUIT_PLANNING_MODEL_NAME`, `CONDUIT_PACKAGING_MODEL_NAME` and `CONDUIT_APPGEN_MODEL_NAME` select models on the shared endpoint. Corresponding `_MODEL_TIMEOUT` settings override the global deadline. Empty settings inherit the global value. Endpoint/key overrides remain absent, and every legacy `_MODEL_CMD` setting is refused.

## Deterministic fixture utility

| Utility | Status | Bound |
| --- | --- | --- |
| `deterministic` | VERIFIED | Direct offline fixture turns only; not real model-provider evidence. |

`packaging-workflow agent-exec` remains a deterministic offline utility. It may be invoked directly with no provider or `--provider deterministic`; it cannot be configured as the Hub's model command. Any other provider label, including a custom label with `CONDUIT_PROVIDER_<NAME>_CMD`, returns the fixed unsupported-command refusal before reading stdin. No vendor CLI is selected and there is no provider fallback.

The former environment allowlist and temporary working directory never confined filesystem access. Retirement removes that host execution path. Prompt framing remains a data-handling control, not an operating-system security boundary.

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

An HTTP model can write the Dockerfile and package files; it does not admit an image builder. Set `CONDUIT_IMAGE_BUILDER=docker` and `DEVWORKSPACE_GIT_ROOT` to run the separately governed build after Accept. Without a model endpoint the existing deterministic writer is used. The Hub still refuses to invent an image digest when the builder is not admitted. See [native workspace compute](native-workspace-compute.md).
