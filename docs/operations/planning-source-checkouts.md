# Planning source checkout identity

Hub planning and the packaging agent resolve the requested repository and ref
through the same isolated Git checkout adapter. Each checkout returns the real
commit resolved from `HEAD^{commit}`. It does not return a workspace's mutable
directory, cached metadata or an invented commit identifier.

An existing workspace for the same repository may contain another branch or
uncommitted files. Planning does not reset, delete or overwrite that workspace,
and another owner's matching workspace does not become an implicit source cache.
`CONDUIT_DEMO_SOURCE_DIR` no longer replaces the requested repository. Offline
tests use temporary Git repositories served over loopback HTTP, with real refs
and commits, rather than relabeling a plain directory as a remote commit.

Intake rejects option-like refs and refspec destinations. The clone boundary
also validates refs when called directly. Missing branches, tags or commits fail;
they do not fall back to an unrelated `HEAD`. A branch moving between planning
turns is detected by the existing source binding and requires a new planning run.

The clone adapter's transport proxy restricts Git requests to the requested host.
This is not a process filesystem or network sandbox; workspace-container
isolation remains a separate boundary. The Excalidraw recording in #615 must use
actual pinned source and build evidence, not a fixture source identifier.

Tests exercise distinct branches, moved heads, dirty and broken cached
workspaces, ignored demo overrides, invalid refs, checkout cleanup and existing
workspace preservation. Both Hub and executable-agent entry points are covered.

## Source-host policy

The Hub decides which hosts a planning clone may reach. The operator sets the
policy in the Hub environment. A value that does not parse stops the Hub at
startup.

| Variable | Default | Meaning |
| --- | --- | --- |
| `CONDUIT_SOURCE_ALLOWED_HOSTS` | unset | Comma-separated host entries. Unset means any host with a public address. |
| `CONDUIT_SOURCE_CONNECTIONS` | unset | JSON array of source connections. It holds file paths and no secret. |
| `CONDUIT_SOURCE_CLONE_TIMEOUT` | `2m` | Time limit for one clone, as a Go duration. |
| `CONDUIT_SOURCE_CLONE_MAX_BYTES` | `536870912` | Size limit for one checkout, in bytes. Objects and files count together. |
| `CONDUIT_SOURCE_CACHE_ENTRIES` | `8` | Number of commits kept for reuse, from 0 to 256. 0 turns reuse off. |

An allowlist entry has one of three forms:

- `git.example.com` is an exact host on any port.
- `10.0.0.5:3000` is an exact host on one port.
- `.example.com` is a suffix. `*.example.com` means the same. It matches
  `git.example.com` and does not match `example.com`.

The address rule applies to every clone, with or without an allowlist:

- Link-local addresses (`169.254.0.0/16`, `fe80::/10`), the instance-metadata
  addresses (`169.254.169.254`, `fd00:ec2::254`, `100.100.100.200`,
  `192.0.0.192`), unspecified and multicast addresses are always refused. No
  allowlist entry permits them.
- Loopback, RFC 1918, carrier-grade NAT (`100.64.0.0/10`) and IPv6 unique-local
  addresses are refused unless the operator named the host exactly. An exact
  allowlist entry or a source connection names a host exactly. A suffix entry
  does not, so `.svc.cluster.local` does not open every in-cluster service.
  To permit an in-cluster Git server, list it by its full name, for example
  `gitea-http.gitea.svc.cluster.local`.

The Hub checks the address when it opens the connection, and not only when it
reads the name. A name that resolves to a public address for one lookup and a
private address for the next is refused at the connection. A clone does not use
`HTTP_PROXY` or `HTTPS_PROXY` from the Hub environment, because the Hub can only
check an address it connects to itself.

A redirect to another host is never followed, even when that host is also on
the allowlist. Request the repository at its final URL.

## Source connections

A source connection is a credential for one host. The operator manages it. The
Hub selects it by the host of the repository URL.

```json
[
  {
    "id": "corp-git",
    "host": "git.corp.example",
    "tokenFile": "/var/run/secrets/conduit/source/corp-git/token",
    "caFile": "/var/run/secrets/conduit/source/corp-git/ca.crt",
    "scheme": "bearer"
  }
]
```

| Field | Meaning |
| --- | --- |
| `id` | Name written to the log. Lowercase letters, digits, dot, dash, underscore. |
| `host` | One exact host, with an optional port. A suffix is refused. |
| `tokenFile` | Path of a mounted file that holds the token on one line. |
| `caFile` | Path of a PEM bundle that Git trusts for this host only. |
| `scheme` | `bearer` (default) sends `Authorization: Bearer <token>`. `basic` sends `Authorization: Basic` with `username` and the token. |
| `username` | Basic-auth user. Default `x-access-token`. |
| `allowHTTP` | `true` lets the token travel over plain `http`. Default `false`. |

Mount the token from a Kubernetes Secret and give the path. The JSON accepts no
field for a secret value: an unknown field such as `token` stops the Hub at
startup, and the error does not repeat the value. The Hub reads the token file
at each clone, so a rotated Secret takes effect without a restart.

The Hub gives Git the credential as configuration in the Git process
environment (`GIT_CONFIG_COUNT`, `GIT_CONFIG_KEY_0`, `GIT_CONFIG_VALUE_0`). The
key is `http.<scheme>://<host>/.extraHeader`. This mechanism was chosen over
the two alternatives for these reasons:

- A `-c http.extraHeader=...` argument puts the token on the command line,
  where a process listing shows it.
- An askpass helper needs a helper executable, which the `FROM scratch` Hub
  image does not have, and Git calls it only after a `401` that offers Basic
  authentication. A server that accepts only a bearer token never asks.

The key names the connection's own origin, so Git applies the header to no
other URL. The Hub also selects the connection by host, pins the transport to
the requested host, and follows no redirect. A clone of an `http` URL on a host
whose connection does not set `allowHTTP` is refused before any request.

Clone errors and logs replace the token, and its Basic encoding, with
`[redacted]`. Each clone writes one log line, `planning source checkout`, with
the host, the resolved commit, the connection `id` and whether the checkout was
reused. The line never holds the credential. `repoclone.Checkout.ConnectionID`
carries the same `id` to the caller.

Limits of this change:

- Connections are per Hub and selected by host. Selection per session or per
  tenant is not built.
- There is no console screen for connections. They are environment
  configuration.
- The planner's own audit record (`planning.model`) does not yet name the
  connection. The planner can copy `Checkout.ConnectionID` into it.
- An abbreviated commit is expanded through the host's web endpoint with no
  credential. On a private repository, give a branch, a tag or a full commit.
- Only `http` and `https` remotes are supported. There is no SSH deploy key.

## Limits

A clone that runs past `CONDUIT_SOURCE_CLONE_TIMEOUT` is stopped and reports
`source checkout exceeded the time limit`.

The Hub measures the checkout directory every 100 ms while Git fetches, and
once more after the files are written. A checkout over
`CONDUIT_SOURCE_CLONE_MAX_BYTES` is stopped, removed, and reports
`source checkout exceeds the size limit`. Because the measure is a sample, a
fetch can pass the limit by what Git writes in 100 ms before it is stopped.

## Checkout reuse

Each planning turn asks for the repository again. The Hub now asks the remote
only which commit the ref names. When it already holds that commit, it copies
the held files and downloads nothing. A full 40-character commit is served
without contacting the remote.

- The cache key is the repository URL, the resolved commit and the connection
  `id`. A branch that moved resolves to another commit, so it is fetched and
  the existing source binding still detects the move.
- A checkout fetched with one connection is never given to a clone that would
  use another connection, or none.
- Every caller receives its own copy and removes it when the turn ends. A
  write into one copy does not change the cache or another caller's files.
- The cache keeps `CONDUIT_SOURCE_CACHE_ENTRIES` commits and drops the least
  recently used one first. It is in the Hub's temporary directory and does not
  survive a restart. Its disk use is at most the entry count times the size
  limit.

## Callers outside the Hub

`repoclone.Clone`, the package-level function, applies the address rule with
one difference: it permits loopback. It has no allowlist, no connection and no
reuse. `packaging-workflow agent-exec` and the planner's fallback (a `Planner`
with no `Clone` set) call it. The Hub always sets `Clone` from the policy
above, so the Hub path does not use it.
