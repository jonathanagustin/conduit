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

- **https is required.** Plain `http` is accepted without an opt-in only for `localhost` and loopback addresses, which is what a local Ollama uses. An in-cluster plaintext Service needs `CONDUIT_MODEL_ALLOW_INSECURE_HTTP=true`. With that opt-in the key and every prompt cross the cluster network unencrypted, so use it only where a mesh or the network already protects that path. The Helm runtime gate treats this variable as the one insecure opt-in a chart-rendered Hub may carry; see [Helm](#helm).
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

Each path still needs the tools it runs beside the model. See [What the shipped image cannot run](#what-the-shipped-image-cannot-run).

## Model identity

The model name the Hub records comes from two sources only: the operator's configuration and the provider's response metadata. It is never read from the reply text. A `"model"` field written inside a reply is model-authored and is ignored.

| Source | Where it is recorded |
| --- | --- |
| `CONDUIT_MODEL_NAME` | `configuredModel` in the planning audit events (`planning.inspect`, `planning.questions`, `planning.plan`) and in the packaging proposal and workspace package audit events. |
| The `model` field of the provider's HTTP response | `providerModel` in the same events. It is bounded to 128 bytes and control characters are dropped. |

The Accept binding has one `model` field. It holds the configured name. When the provider reported a different id it holds both, as `configured (provider reported served-id)`. That same string is the `model` value in the `planning.accept` audit event. A separate field for the provider id in the binding needs a change to the planning domain types and is not made here.

A provider that reports a different id than the one requested is recorded, not refused. Aliases make that normal: a request for `my-model` can be served by `my-model-2026-01`.

## Helm

The `platform-hub` chart configures the HTTP transport through typed values. `values.schema.json` validates them and rejects unknown keys. With `model.baseURL` empty the chart renders nothing for the model, and the default render is unchanged.

| Value | Sets | Notes |
| --- | --- | --- |
| `model.baseURL` | `CONDUIT_MODEL_BASE_URL` | `https://` or `http://`, with no credentials, query or fragment. |
| `model.name` | `CONDUIT_MODEL_NAME` | Required with `model.baseURL`. |
| `model.timeout` | `CONDUIT_MODEL_TIMEOUT` | Optional, for example `90s`. |
| `model.apiKeySecret.name`, `model.apiKeySecret.key` | `CONDUIT_MODEL_API_KEY_FILE=/run/conduit-model/api-key` and a read-only Secret mount | The key is never a chart value. The Secret is required once named. Leave the name empty for a keyless server. |
| `model.caConfigMap.name`, `model.caConfigMap.key` | `CONDUIT_MODEL_CA_FILE=/run/conduit-model-ca/ca.pem` and a read-only ConfigMap mount | Optional private CA. |
| `model.allowInsecureHTTP` | `CONDUIT_MODEL_ALLOW_INSECURE_HTTP=true` | Required with an `http://` base URL and refused with `https://`. |

The chart refuses a base URL with no model name, `http://` without the opt-in, the opt-in without `http://`, any other model value with no base URL, and a key written into values. It has no value for `CONDUIT_MODEL_CMD`, because the Hub image carries no model CLI.

```sh
kubectl -n platform-system create secret generic model-api-key --from-file=api-key=./model-api-key
helm upgrade platform-hub deploy/helm/platform-hub --namespace platform-system --reuse-values \
  --set-string model.baseURL=https://models.example/v1 \
  --set-string model.name=my-served-model \
  --set model.apiKeySecret.name=model-api-key
```

**The insecure opt-in and the runtime gate.** `scripts/verify-helm-runtime.rb` used to match insecure opt-ins by the prefix `ALLOW_INSECURE_`, which this variable does not have, so the gate never saw it. The gate now matches `ALLOW_INSECURE` anywhere in a name and refuses it in every profile it renders. One render is the exception: the plaintext model profile may carry `CONDUIT_MODEL_ALLOW_INSECURE_HTTP`, by that exact name, with the value `true`, beside an `http://` base URL, and with no other insecure opt-in. The reason is that a model inside the cluster is usually a plaintext Service, and under UDS Core the mesh encrypts that hop. The opt-in weakens the model hop only. It is not an identity opt-in, and the same Hub must still render its signing and mTLS inputs. Whether a mesh covers the hop in your cluster is yours to confirm; this repository holds no run that shows it.

## Zarf and UDS

The Zarf package takes the same settings as deploy variables. Each defaults to no model call.

| Variable | Default | Meaning |
| --- | --- | --- |
| `MODEL_BASE_URL` | empty | The API base. Empty makes every other model variable inert. |
| `MODEL_NAME` | empty | Required with `MODEL_BASE_URL`. Zarf cannot check one variable against another, so a base URL with no name is a Hub startup failure that names the variable. |
| `MODEL_TIMEOUT` | empty | Optional request bound. |
| `MODEL_API_KEY_SECRET` | `platform-model-api-key` | Secret whose `api-key` key holds the key. The mount is optional. |
| `MODEL_API_KEY_FILE` | empty | Set to `/run/conduit-model/api-key` to send the key. That is the only accepted value. |
| `MODEL_CA_CONFIGMAP` | `platform-model-ca` | ConfigMap whose `ca.pem` key holds a private CA. The mount is optional. |
| `MODEL_CA_FILE` | empty | Set to `/run/conduit-model-ca/ca.pem` to trust it. That is the only accepted value. |
| `MODEL_ALLOW_INSECURE_HTTP` | `false` | `true` permits a plain `http` base URL. |
| `MODEL_SERVICE_NAMESPACE`, `MODEL_SERVICE_NAME_LABEL` | `conduit-model-unconfigured` | Used only by the `uds` and `uds-metrics` components; see below. |

UDS Core denies egress by default. Both UDS Package manifests carry one allow from the Hub to the Pods labelled `app.kubernetes.io/name: <MODEL_SERVICE_NAME_LABEL>` in `<MODEL_SERVICE_NAMESPACE>`. A Zarf variable cannot make a list entry optional, so the entry is always present and its defaults name a namespace that does not exist. The rendered policy therefore grants nothing until you set both variables. The entry lists no port: the Package schema requires a port to be a number, a Zarf variable renders a string, and model servers do not share one port. Only the Hub is granted this egress.

The model's own package must allow the matching ingress from `platform-system`. A model outside the cluster needs a `remoteHost` allow that this package does not ship.

This allow is checked for shape only, as the [UDS package note](uds-package.md) says of the issuer, registry and Temporal allows. Both manifests validate against the UDS Core 1.17.1 Package schema, which makes the port optional. No cluster run has shown the generated NetworkPolicy admitting the traffic. That an allow with no port admits every TCP port on the selected Pods is UDS Core behaviour as recalled, not read from this repository; confirm it before relying on it.

### Air-gapped recipe: a model inside a UDS cluster

1. Deploy the model server from its own package, for example vLLM in namespace `vllm` with Pods labelled `app.kubernetes.io/name: vllm`, serving the OpenAI-compatible API on port 8000. Allow ingress from `platform-system` in that package.
2. Read the label and the port from the cluster instead of assuming them: `uds zarf tools kubectl get pods -n vllm --show-labels`.
3. Deploy Conduit with the endpoint and the egress allow:

   ```sh
   uds zarf package deploy zarf-package-platform-runtime-*.tar.zst --components uds --confirm \
     --set-variables MODEL_BASE_URL=http://vllm.vllm.svc.cluster.local:8000/v1 \
     --set-variables MODEL_NAME=my-served-model \
     --set-variables MODEL_ALLOW_INSECURE_HTTP=true \
     --set-variables MODEL_SERVICE_NAMESPACE=vllm \
     --set-variables MODEL_SERVICE_NAME_LABEL=vllm
   ```

4. Read `/v1/admin/status` as an administrator and check the `modelProvider` component. Then send one assistant message to confirm the endpoint answers.

No request leaves the cluster in this recipe. It is written from the configuration and has not been run against a cluster. The Zarf package, its lint and the chart render are verified; the traffic is not.

## Admin status

`GET /v1/admin/status` reports the model provider and each AI capability as entries in `components`. The Hub writes the same lines to its log at startup.

| Component id | Needs |
| --- | --- |
| `modelProvider` | Reports the provider kind: `none`, `cli`, `http`, or `invalid` with the reason. |
| `aiAssistant` | A model provider. |
| `aiPlanning` | A model provider and `git`. |
| `aiAppGeneration` | A model provider, `go` and `docker`. |
| `aiPackagingProposals` | A model provider. |
| `aiPackagingCompute` | `CONDUIT_IMAGE_BUILDER=docker`, `DEVWORKSPACE_GIT_ROOT`, and `docker`, `git` and `zarf`. It runs with or without a model. |

The statuses are the ones the status contract already has. `CONFIGURED` means the prerequisites are present. `UNCONFIGURED` means no model provider is set. `UNAVAILABLE` means a prerequisite is missing, and the detail names it, for example `binary not found: git`. No entry is `AVAILABLE`: the Hub looks for each binary on its `PATH` (and `ZARF_BIN` for zarf) and reads its configuration, but it calls no model and runs no tool, so it does not claim that a capability works.

The status shows the endpoint's scheme and host, a command's base name, and the configured model name. It does not show the key, the key file path, the endpoint path, or the text of a configuration error, because that text can quote a path or a URL.

## What the shipped image cannot run

Configuring a model is not enough to run every AI capability from the shipped Hub image. The image is built `FROM scratch` and contains the Hub binary, three helper binaries and `cosign`. It contains no `git`, `go`, `docker`, `zarf`, Node or model CLI. This change does not alter the image.

| Capability | In the shipped image with `model.baseURL` set | Why |
| --- | --- | --- |
| Assistant | Runs. | It needs only the HTTP endpoint. |
| Packaging model proposals and intelligence live evals | Run. | They need only the HTTP endpoint. |
| Planning turns | Do not run. | Each turn clones the repository with `git`, which is absent. The Hub Pod also has a read-only root filesystem and no writable temporary directory in the chart or the Zarf manifest, and the clone writes to one. |
| App generation | Does not run with a model. | The gates run `go build` and `docker build`. With no model the recorded application is unchanged. |
| Packaging compute after Accept | Does not run. | It needs `docker`, `git` and `zarf`, and the chart has no value for `CONDUIT_IMAGE_BUILDER` or `DEVWORKSPACE_GIT_ROOT`. |
| A model CLI (`CONDUIT_MODEL_CMD`) | Does not run. | No CLI is in the image, and the chart has no value for it. |
| Workspace ACP agent | Does not run. | It needs Node and an agent binary. |

Admin status reports these honestly: in the shipped image `aiPlanning` and `aiAppGeneration` read `UNAVAILABLE` with the missing binary named. The first two rows follow from what the code needs and what the image holds. They have not been exercised in a cluster from this repository.

Closing the gap needs a worker image that carries those tools, or moving the clone and the gates behind an API. That is the open half of issue #807.

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
