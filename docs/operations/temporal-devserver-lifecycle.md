# Local Temporal development server lifecycle

The packaging and planning demonstrations start the pinned Temporal CLI on
loopback with a persistent SQLite database. This is a local proof harness.

Conduit reserves distinct frontend and UI listeners together until launch. On
Linux, closing an accepted loopback connection from the server side first leaves
TIME_WAIT protection: subsequent automatic `bind(:0)` calls cannot select those
server ports, while explicit Go listeners can bind them with SO_REUSEADDR. This
follows the pinned CLI's own allocator in
[`internal/devserver/freeport.go` at v1.10.0](https://github.com/temporalio/cli/blob/v1.10.0/internal/devserver/freeport.go).
Darwin and Windows follow that allocator's sequential-port approach without the
connection step. Linux tests inspect the actual TCP state before launch and
verify explicit rebind. This handoff does not exclude another process deliberately
binding the same explicit port.

The harness owns the process from launch, including failed readiness. It waits
for the default namespace RPC connection, rejects observed fatal listener output
or early process exit, and checks cancellation before acknowledging readiness.
Stdout and stderr are forwarded; only a bounded suffix is retained for detecting
split fatal messages. The readiness client closes when the process exits.

Shutdown interrupts only the owned child and waits for it. Windows uses a new
process group and CTRL_BREAK_EVENT. If graceful shutdown exceeds ten seconds,
the harness kills that child and reports an error; reaping has a further bounded
wait. Failed startup uses the same cleanup path. Successful readiness is a point
in time, not a guarantee that the child cannot fail later.

The UI and frontend remain bound to `127.0.0.1`; UI news fetching and server
version checks remain disabled. Database state persists across process restarts.
The pinned-CLI recovery test exercises a workflow waiting across a restart and
its subsequent handoff, rather than treating an open port as recovery evidence.

```sh
CONDUIT_TEMPORAL_CLI="$PWD/.cache/temporal/temporal" \
  go test -race -count=3 ./cmd/packaging-workflow \
  -run TestCLIPlanningWorkflowEndToEndWithDevServer
CONDUIT_TEMPORAL_CLI="$PWD/.cache/temporal/temporal" \
  go test -race ./internal/packagingworkflow/pipeline \
  -run TestDevServerRestartRecoversWaitAndHandoff
```
