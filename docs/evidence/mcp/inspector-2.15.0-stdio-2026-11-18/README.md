# MCP Inspector: local packaging stdio

This is an executed interoperability record, using the official Inspector CLI
2.15.0 and a real `packaging-mcp` child process. It is not a simulated client,
Hub deployment, model call or image-registry operation.

`receipt.json` binds every raw capture and the Inspector dependency lock by
SHA-256. `commands.json` retains the exact executed argument vectors and exit
codes. Each invocation establishes its own stdio connection. The local absolute
paths identify this recording worker; they are not portable installation paths.

## Reproduce

Use Go 1.27.4 and Node 26.13.1 on Linux/amd64. The recorded server build used
`go build -p 1 -ldflags='-s -w' -o <binary> ./cmd/packaging-mcp` at commit
`48bb0bf6e49b9919bcba07ce105c8bbacb1f4a9c`. Only the workflow differs from main
`e6cd1c07`; packaging code and module declarations are identical. The binary
digest identifies this build; reproducible binary identity is not claimed.

Obtain the official Inspector 2.15.0 npm archive from
`https://registry.npmjs.org/@modelcontextprotocol/inspector/-/inspector-2.15.0.tgz`
and verify its SHA-512 integrity against `receipt.json` before extracting it.
Replace its package lock with `inspector-package-lock.json`, then install with
`npm ci --ignore-scripts --omit=dev`. Package scripts were disabled in the
recording. TLS verification remained enabled; the worker's system CA bundle was
used for its network proxy. No Inspector dependency is added to Conduit.

Create a disposable directory containing `README.md` with exactly:

```text
Conduit MCP interoperability fixture.
```

The file ends in one newline. Create `outside-marker.txt` beside that directory
with `Synthetic outside marker; must not be returned.` and a newline. Inside the
directory, make `escape.txt` a symlink to that outside file. Adjust the binary
and fixture paths in a copy of `session-config.json`; leave the `unbound` entry
without a repository argument. The config is read-only to Inspector.

Run each argument vector in `commands.json`, adjusting only these local paths.
Supply `MCP_INSPECTOR_SECRET_STORE=memory` and the executable search path, with
the rest of the caller environment removed. The launcher entry point is
`clients/launcher/build/index.js` inside the verified Inspector package.

## Results and limits

- Bound tool list: six tools; unbound list: four, without repository readers.
- README read: exit 0, exact content and independently verified SHA-256.
- Traversal and symlink escape: exit 5, `isError: true`, no outside content.
- Strict schema list: exit 0, zero errors and nine retained warnings about
  legal nullable-array schemas and narrower model-provider dialects.

No credential was supplied. Inspector used its memory-only secret store.
The Hub HTTP transport, OIDC/mTLS, IDEs, desktop assistants, provider-specific
schema translation and deployed workloads remain unqualified by this record.
The earlier Hub test-client session remains separate in `../hub-session.json`.
