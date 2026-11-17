# HTTP model endpoint

The Hub can call a model served over HTTP instead of starting a local command. The adapter speaks the OpenAI-compatible Chat Completions API, which vLLM, Ollama, LeapfrogAI and hosted APIs expose. The model does not approve a change. Every gate and every human approval that applies to the [local model CLI](local-model-cli.md) applies here unchanged.

## Configuration

| Variable | Required | Meaning |
| --- | --- | --- |
| `CONDUIT_MODEL_BASE_URL` | yes | The API base, for example `https://models.example/v1`. The Hub appends `/chat/completions`. |
| `CONDUIT_MODEL_NAME` | yes | The `model` value sent in each request, and the configured model name the Hub records. |
| `CONDUIT_MODEL_API_KEY_FILE` | no | Path of a file that holds the API key. Leave it unset for a keyless in-cluster server. |
| `CONDUIT_MODEL_CA_FILE` | no | Path of a PEM file with extra root certificates. They are added to the system roots. |
| `CONDUIT_MODEL_ALLOW_INSECURE_HTTP` | no | Set to exactly `true` to allow plain `http` to a host that is not loopback. |
| `CONDUIT_MODEL_TIMEOUT` | no | The same variable the model CLI uses. It bounds one whole request. The default is 45s. |

Set `CONDUIT_MODEL_BASE_URL` or `CONDUIT_MODEL_CMD`, not both. With both set the Hub refuses to start and names the two variables; it does not pick one. The `packaging-workflow` binary applies the same rule in every subcommand, the worker and `agent-exec` included. With neither set nothing changes: the Hub keeps the recorded proposals and the recorded application and makes no model call. The other variables in the table have no effect without `CONDUIT_MODEL_BASE_URL`.

A configuration error fails startup. That covers a missing model name, a base URL with a query, a fragment or inline credentials, an unreadable or empty key file, a CA file with no certificate, and plain `http` to a host that is not loopback.

## The key

The key is read from the file on every call, so a rotated key takes effect without a restart. No variable accepts the key inline, which keeps it out of the process environment and out of a chart's values. The file must hold one token with no spaces or control characters; surrounding whitespace is trimmed. Mount it from a Secret and make it readable by the Hub's user only.

The Hub does not log the key, the prompt or the response body, and none of them appears in an error. The audit record holds the adapter name `openai-http`, the model ids described under [Model identity](#model-identity), and the SHA-256 digests of the prompt and of the reply text, the same digests the model CLI records.

## Transport rules

- **https is required.** Plain `http` is accepted without an opt-in only for `localhost` and loopback addresses, which is what a local Ollama uses. An in-cluster plaintext Service needs `CONDUIT_MODEL_ALLOW_INSECURE_HTTP=true`. With that opt-in the key and every prompt cross the cluster network unencrypted, so use it only where a mesh or the network already protects that path. The Helm runtime gate rejects variables that start with `ALLOW_INSECURE_`; this variable starts with `CONDUIT_MODEL_`, so that gate does not reject it. Review it by hand.
- **Redirects that leave the origin are refused.** A redirect to another host, another port, a subdomain or another scheme fails the call, and the request is not sent to the new location. The registry client in `internal/gitless` strips the credential and follows such a redirect, because registries redirect blob reads to a CDN. A model endpoint has no such need, and a 307 or 308 would send the prompt as well as the key, so here the redirect is refused. A redirect within the same origin is followed.
- **The response body is bounded** at 4 MiB. A larger body is an error and no part of it is used.
- TLS 1.2 is the minimum. The standard proxy variables (`HTTPS_PROXY`, `NO_PROXY`) are honoured.

## Errors

Each failure is a distinct error value in `internal/modelhttp`. Callers see it wrapped in the port's own model error.

| Condition | Error | Port error |
| --- | --- | --- |
| The request exceeds `CONDUIT_MODEL_TIMEOUT` | `ErrTimeout` | model timed out |
| The caller cancels | `ErrCanceled` | model failed |
| Status 401 or 403 | `ErrUnauthorized` | model failed |
| Status 429 | `RateLimitError`, which matches `ErrRateLimited` and carries the `Retry-After` delay in the value and in the message | model failed |
| Status 5xx | `ErrUpstream` | model failed |
| Any other status that is not 200 | `ErrStatus` | model failed |
| The body is not a chat completion with message content | `ErrMalformed` | model failed |
| The body is larger than the bound | `ErrResponseTooLarge` | model failed |
| A redirect leaves the origin | `ErrRedirectRefused` | model failed |
| The key file cannot be used | `ErrKeyFile` | model failed |
| Connection or certificate failure | `ErrTransport` | model failed |

The port error texts still say "local model CLI"; the ports were not changed for this adapter. The Hub does not retry. A 429 reports the delay and leaves the decision to the caller.

## Which model paths use it

| Path | HTTP endpoint | Note |
| --- | --- | --- |
| Assistant | yes | `internal/assistant/adapters/modelhttp` |
| App generation | yes | `internal/appgen/adapters/modelhttp` |
| Intelligence live evals | yes | Uses the packaging proposal adapter, `internal/packagingworkflow/adapters/modelhttp`. |
| Planning turns (inspect, questions, plan) | yes | `planner.HTTPModel` in `internal/planning/adapters/planner`. The prompt, the untrusted-data framing of tool results and the digests are the same as for a model CLI. |
| Packaging model proposals | yes | `internal/packagingworkflow/adapters/modelhttp`, with the inspected image facts in the prompt. |
| Post-Accept package writer | yes | The same adapter's `ProposePackage`. With no model configured the deterministic writer is unchanged. |
| `packaging-workflow` worker (planning activities and plan compute) | yes | It reads the same variables under the same rule. |
| `packaging-workflow agent-exec` | not applicable | It is itself a `CONDUIT_MODEL_CMD` target that runs the built-in agent or a provider CLI. It makes no HTTP model call, and it refuses to run with both transports set. |

## Model identity

The model name the Hub records comes from two sources only: the operator's configuration and the provider's response metadata. It is never read from the reply text. A `"model"` field written inside a reply is model-authored and is ignored.

| Source | Where it is recorded |
| --- | --- |
| `CONDUIT_MODEL_NAME` | `configuredModel` in the planning audit events (`planning.inspect`, `planning.questions`, `planning.plan`) and in the packaging proposal and workspace package audit events. |
| The `model` field of the provider's HTTP response | `providerModel` in the same events. It is bounded to 128 bytes and control characters are dropped. |

The Accept binding has one `model` field. It holds the configured name. When the provider reported a different id it holds both, as `configured (provider reported served-id)`. That same string is the `model` value in the `planning.accept` audit event. A separate field for the provider id in the binding needs a change to the planning domain types and is not made here.

A provider that reports a different id than the one requested is recorded, not refused. Aliases make that normal: a request for `my-model` can be served by `my-model-2026-01`.

## Limits

- **Not streaming.** The Hub sends `"stream": false` and reads one response. The model ports are request and response, so a streamed reply would add nothing today.
- **One user message.** The whole prompt is sent as a single `user` message. No system message, tool definitions, temperature or token limit are sent; the server's defaults apply.
- **The Anthropic Messages API is not implemented.** Use an OpenAI-compatible gateway in front of such a model, or the model CLI.
- **The assistant and app generation do not record the served model id.** Planning and packaging record it, as [Model identity](#model-identity) describes. The assistant and app generation ports' provenance has no field for it; their adapters expose it through `ReportedModel()` and nothing reads it yet.
- **One model for every capability.** `CONDUIT_MODEL_NAME` applies to the assistant, planning and packaging alike. There is no per-capability or per-tenant model choice.
- **Not exercised against a real server.** The tests use loopback `httptest` fakes. No run against vLLM, Ollama, LeapfrogAI or a hosted API is recorded here, so confirm a provider with one local call before relying on it.

## Examples

A local Ollama, with no key and loopback `http`:

```sh
CONDUIT_MODEL_BASE_URL=http://127.0.0.1:11434/v1
CONDUIT_MODEL_NAME=llama3.1
```

A hosted or in-cluster TLS endpoint with a private CA:

```sh
CONDUIT_MODEL_BASE_URL=https://models.internal.example/v1
CONDUIT_MODEL_NAME=my-served-model
CONDUIT_MODEL_API_KEY_FILE=/var/run/secrets/model/api-key
CONDUIT_MODEL_CA_FILE=/etc/conduit/model-ca.pem
```
