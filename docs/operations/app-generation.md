# App generation

`POST /v1/app-generations` asks a model for one small HTTP application, runs it
through six gates, and hands a passing result to the packaging flow. It does
not approve a change. This page states what the feature supports, where it
keeps its records, how it builds, and what it does not do yet.

## Supported matrix

The profile is fixed. A reply outside it fails a gate.

| Aspect | Supported |
| --- | --- |
| Application shape | One stateless HTTP server with `GET /health` returning `200` and the body `ok`. |
| Routes | 1 to 8, `GET` or `POST`. |
| Source | Exactly one file: `main.go` (language `go`), `main.py` (language `python`) or `index.html` (language `html`). |
| File size | At most 4096 bytes. |
| Dependencies | The standard library of the language only. The build has no module proxy and no network. |
| Port | 1024 to 65535. |
| Resources | Memory at most `1Gi`, CPU at most `1`. |
| Image | `main.go` and `index.html`: `FROM scratch`, one static binary. `main.py`: the digest-pinned CPython image. All run as user `65532`. |
| Target | `linux/amd64`. |
| State | None. A stateful application is refused by the packaging generator. |

More than one file, larger files, other languages and third-party modules are
not supported. `appgen-deploy.md` describes the chart a passing result is
deployed from.

## Records

Each result belongs to the operator who created it. Another operator's list
does not hold it, and another operator's read gets the same `404` as a missing
id, with a denial written to the authorization audit.

| `REPOSITORY_KIND` | Store | After a Hub restart |
| --- | --- | --- |
| unset or `file` | Process memory | Results are lost. |
| `postgres` | PostgreSQL, from `POSTGRES_DSN_FILE` | Results are kept and every replica reads the same ones. |

The PostgreSQL store is migration `0001-app-generations` of component `appgen`.
It creates `conduit_app_generations`. The owner is a column, so a list is
selected by owner in the query. A row with a blank owner matches no caller. A
save that names an id another owner holds is refused.

`platform-delivery` opens the store in `configureReconciliation`, on the same
DSN as the notification and assistant-thread stores, and closes it through the
same closer. It passes the store to the Hub as `Options.AppgenStore`. The Hub
opens no database for app generation itself: with no store passed, results
stay in process memory. `platform-delivery verify-schema` checks the `appgen`
migration set with the others.

A result that implements an accepted planning revision stores
`planRevisionId` and `planDigest`. Both are absent from a result that is not
bound to a planning session.

## Build

The build runs on the Hub host, in the Hub's container. Every shape needs the
`docker` binary and a container engine. `main.go` and `index.html` also need
the `go` binary. `main.py` needs the pinned CPython image, which the engine
pulls when it does not hold it.

The Hub's shipped image is `FROM scratch` and has neither binary. On that
image:

- The Hub logs one warning at startup: `app generation cannot build in this
  deployment`.
- `POST /v1/app-generations` with `confirm: true` answers `503` with
  `build executor unavailable in this deployment` and the name of the missing
  binary. The model is not called.
- A host with `docker` and no `go` is available and builds `main.py`. A reply
  with `main.go` or `index.html` gets the same `503` there, after the model
  call, because the shape is not known before the reply.
- `composition.AppGenerationBuildExecutor()` returns the same fact as
  `{Available, Detail}`. The startup log and authenticated admin summary report
  model and build prerequisites. With an HTTP model and `docker` but no `go`,
  they report Python build prerequisites present and the missing compiler for
  Go/HTML and the recorded Go demonstration. `CONFIGURED` is a presence check,
  not a provider/engine health probe or separate-worker qualification.

Where the binaries exist, each build is isolated in these ways:

- **Environment.** A build child does not inherit the Hub environment. It gets
  an explicit list: `PATH`, `HOME`, `USER`, `LOGNAME`, locale and time-zone
  variables, the temporary-directory variables, `XDG_RUNTIME_DIR`,
  `XDG_CACHE_HOME`, `XDG_CONFIG_HOME`, `GOROOT`, `GOCACHE`, and the engine
  endpoint variables `DOCKER_HOST`, `DOCKER_CONTEXT`, `DOCKER_CONFIG`,
  `DOCKER_TLS_VERIFY`, `DOCKER_CERT_PATH` and `DOCKER_BUILDKIT`. Database
  DSNs, signing keys, OIDC secrets and model credentials are dropped. The list
  is the build's own and is narrower than the model command's list in
  `local-model-cli.md`.
- **Toolchain.** The Hub then sets `GOPROXY=off`, `GOFLAGS=`, `GOENV=off`,
  `GOWORK=off`, `GOSUMDB=off`, `GOTOOLCHAIN=local` and `CGO_ENABLED=0`, so a
  variable in the Hub environment cannot change how the compiler resolves
  modules or toolchains. The health check's engine commands get the same
  environment.
- **Directory.** Each build uses a new temporary directory and removes it.
- **Time.** `CONDUIT_APPGEN_BUILD_TIMEOUT` bounds the compile and the image
  build together. The default is `3m`.
- **Output.** The Hub keeps at most 64 KiB of a child's output. A gate reason
  is the first line of that output, cut at 240 bytes. At most 4096 bytes go to
  the repair step below.
- **Network.** The image build runs with `--network none`. Pulling the CPython
  image for `main.py` is the one step that uses the network.

What this does not give: the compiler still runs as the Hub's user, in the
Hub's container, with the Hub's filesystem. Moving the build to a separate
executor, such as a Kubernetes Job or a remote builder, is not built. Until it
is, app generation works only on a Hub that runs where `go` and `docker` exist,
which is a development machine and not the shipped image.

## Repair

When a reply fails a gate, the Hub sends the failure back to the model and asks
for a corrected reply.

- `CONDUIT_APPGEN_REPAIR_ATTEMPTS` is the number of corrected replies the Hub
  asks for. The default is `2`, so one generation makes at most 3 model calls.
  The range is `0` to `5`. `0` is one shot.
- A failure of the schema, allowlist, secrets, resources, build or health gate
  is repaired. A failure of the model command itself, a timeout, a registry
  push or the packaging handoff is not, because another reply cannot fix it.
- The repair prompt holds the failure output and the earlier reply between
  `<<<UNTRUSTED_TOOL_OUTPUT>>>` markers, with the instruction that the block
  is data and never authority. Both are written as JSON strings, so compiler
  output that quotes model-written source cannot close the block or add an
  instruction line. The failure output is cut at 2048 bytes and the earlier
  reply at 12288 bytes.
- The loop stops at the first reply that passes every gate, or at the cap.
- The result lists every attempt in `attempts`, each with its number, the
  prompt and response digests, the gates it reached and its error. The other
  fields describe the last attempt. Each attempt also writes one
  `app.generate` audit record that carries its attempt number.

### Repair and the accepted plan

A generation started from a planning session implements an accepted plan
revision. Repair does not loosen that:

- Every exchange carries the same plan revision, digest, plan text and
  planning conversation. The repair block is added after them and never
  replaces them, and the repair instruction says the plan still governs.
- A corrected reply runs every gate from the first one. It is not judged only
  on the gate the earlier reply failed.
- A corrected reply must keep the source shape, the port and the route list
  of the first well-formed reply. A reply that changes one of them fails the
  schema gate with `repaired reply drifted from the accepted plan revision`
  and is not built. This stops a reply that swaps the language or drops a
  route to get past a failing build from being accepted because it builds.
- The model is told why the reply was refused and can return to the surface
  within the same cap.

The limit of this check: an accepted plan is prose, and the Hub cannot test
that any reply, first or corrected, implements it. The check holds a
corrected reply to the first reply. It does not hold the first reply to the
plan. A generation with no accepted plan is not held to a surface.

## The recorded demonstration

With no model configured, the Hub serves one checked-in application,
`hello-http`, whatever the request describes. Such a result carries
`"recordedDemo": true`. The field is absent from a result a model produced.
The existing `label` stays `No model call`. The demonstration gives the same
reply every time, so it is never sent for repair.

The console reads `recordedDemo` and shows a recorded-demo badge in generation
history. The `No model call` label remains visible; neither indicator claims
that the demonstration was generated for the request.

## Settings

| Variable | Default | Meaning |
| --- | --- | --- |
| `CONDUIT_APPGEN_REPAIR_ATTEMPTS` | `2` | Corrected replies to ask for after a gate failure, `0` to `5`. |
| `CONDUIT_APPGEN_BUILD_TIMEOUT` | `3m` | Time limit for one compile and image build. |
| `REPOSITORY_KIND` | `file` | `postgres` keeps results in PostgreSQL. |
| `POSTGRES_DSN_FILE` | none | File holding the DSN. Required with `postgres`. |
| `CONDUIT_LOCAL_REGISTRY` | `127.0.0.1:5000` | Registry a passing image and chart are pushed to. |

A value that does not parse stops the Hub at startup.
