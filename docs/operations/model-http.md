# HTTP model endpoint

The Hub can call a model served over HTTP instead of starting a local command. The adapter speaks the OpenAI-compatible Chat Completions API, which vLLM, Ollama, LeapfrogAI and hosted APIs expose. The model does not approve a change. Every gate and every human approval that applies to the [local model CLI](local-model-cli.md) applies here unchanged.

## Configuration

| Variable | Required | Meaning |
| --- | --- | --- |
| `CONDUIT_MODEL_BASE_URL` | yes | The API base, for example `https://models.example/v1`. The Hub appends `/chat/completions`. |
| `CONDUIT_MODEL_NAME` | yes | The `model` value sent in each request. |
| `CONDUIT_MODEL_API_KEY_FILE` | no | Path of a file that holds the API key. Leave it unset for a keyless in-cluster server. |
| `CONDUIT_MODEL_CA_FILE` | no | Path of a PEM file with extra root certificates. They are added to the system roots. |
| `CONDUIT_MODEL_ALLOW_INSECURE_HTTP` | no | Set to exactly `true` to allow plain `http` to a host that is not loopback. |
| `CONDUIT_MODEL_TIMEOUT` | no | The same variable the model CLI uses. It bounds one whole request. The default is 45s. |

Set `CONDUIT_MODEL_BASE_URL` or `CONDUIT_MODEL_CMD`, not both. With both set the Hub refuses to start and names the two variables; it does not pick one. With neither set nothing changes: the Hub keeps the recorded proposals and the recorded application and makes no model call. The other variables in the table have no effect without `CONDUIT_MODEL_BASE_URL`.

A configuration error fails startup. That covers a missing model name, a base URL with a query, a fragment or inline credentials, an unreadable or empty key file, a CA file with no certificate, and plain `http` to a host that is not loopback.

## The key

The key is read from the file on every call, so a rotated key takes effect without a restart. No variable accepts the key inline, which keeps it out of the process environment and out of a chart's values. The file must hold one token with no spaces or control characters; surrounding whitespace is trimmed. Mount it from a Secret and make it readable by the Hub's user only.

The Hub does not log the key, the prompt or the response body, and none of them appears in an error. The audit record holds the adapter name `openai-http` and the SHA-256 digests of the prompt and of the reply text, the same fields the model CLI records.

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
| Planning turns | no | The planner adapter runs a local command. With only the HTTP endpoint set, planning turns stay unconfigured, as they are with no model, and the Hub logs one warning at startup. |
| Packaging model proposals and the post-Accept package writer | no | These still read `CONDUIT_MODEL_CMD` only. With only the HTTP endpoint set they keep their no-model behaviour. |
| `packaging-workflow` worker | no | It reads `CONDUIT_MODEL_CMD` only. |

## Limits

- **Not streaming.** The Hub sends `"stream": false` and reads one response. The model ports are request and response, so a streamed reply would add nothing today.
- **One user message.** The whole prompt is sent as a single `user` message. No system message, tool definitions, temperature or token limit are sent; the server's defaults apply.
- **The Anthropic Messages API is not implemented.** Use an OpenAI-compatible gateway in front of such a model, or the model CLI.
- **The served model id is not in the audit record.** The response's `model` field is kept and exposed by `ReportedModel()` on the client and on each adapter, because the ports' provenance has no field for it. Nothing in the Hub reads it yet.
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
