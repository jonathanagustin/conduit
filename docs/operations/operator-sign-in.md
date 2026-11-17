# Operator sign-in

How a person signs in to the Hub: through the console in a browser, and through
`conduit login` at a terminal. Service accounts are not covered here; they keep
using a bearer token or the client-credentials grant.

The Hub is the OAuth client. The browser never holds a token: it is handed one
opaque cookie that names a record the Hub keeps. The authorization code, the
PKCE verifier, the client secret and the ID token stay on the Hub.

## What turns it on

Browser sign-in runs only when **both** of these are configured:

| Variable | Meaning |
| --- | --- |
| `OPERATOR_OIDC_ISSUER` (with `OPERATOR_OIDC_CLIENT_ID`) | The issuer whose tokens the Hub already verifies. |
| `CONSOLE_PUBLIC_URL` | The `https` origin people reach the console at, with no path. |

With either one absent, `GET /v1/auth/login`, `GET /v1/auth/callback` and
`POST /v1/auth/logout` answer `404` with a reason, the console shows no sign-in
control, and bearer tokens and client certificates work as before.

`CONSOLE_PUBLIC_URL` is never inferred. It becomes the redirect address,
`<CONSOLE_PUBLIC_URL>/v1/auth/callback`, and the only origin a session cookie is
accepted from. The `Host` header of a request contributes to neither. If people
reach the console at an address other than the one configured, sign-in fails at
the identity provider rather than redirecting somewhere a caller chose.

Optional settings, each refused at start unless `CONSOLE_PUBLIC_URL` is set:

| Variable | Default | Meaning |
| --- | --- | --- |
| `OPERATOR_OIDC_CLIENT_SECRET_FILE` | empty | Path of a file holding the client secret on one line. Empty means a public client, which relies on PKCE alone. The secret is never read from a variable's value. |
| `OPERATOR_SESSION_MAX_LIFETIME` | `8h` | Longest a session may last: a Go duration from `5m` to `24h`. |
| `OPERATOR_OIDC_LOGIN_SCOPES` | `profile email` | Space-separated scopes requested besides `openid`. |

A setting that depends on something missing stops the Hub at start with a
message naming the variable. `ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS` cannot
be combined with an issuer, so a Hub runs either the header simulation or real
sign-in, never both.

The identity provider must list `<CONSOLE_PUBLIC_URL>/v1/auth/callback` as a
redirect address of the client, exactly. An issuer whose discovery document
lists PKCE methods without `S256`, or whose authorization or token endpoint is
not `https`, is refused at start.

The ID token is checked by the same verifier and claim policy as a bearer token,
so it must carry what that policy reads: by default `preferred_username`, and
the groups claim if one is configured. The `cognito-access` profile accepts only
access tokens; the Hub logs a warning at start, and browser sign-in is refused
under it.

## What happens

1. The console navigates to `/v1/auth/login?returnTo=<the current view>`.
2. The Hub records a sign-in attempt — a `state`, a `nonce`, a PKCE verifier and
   a binding to this browser — and redirects to the issuer with the `S256`
   challenge. The attempt lasts ten minutes.
3. The issuer returns the browser to `/v1/auth/callback`. The Hub redeems the
   `state` exactly once, checks the browser binding, exchanges the code with the
   verifier (and the client secret, for a confidential client), verifies the ID
   token, and checks its `nonce`, its `aud` and, when present, its `azp`.
4. The Hub opens a session and redirects to the view the person started from.
   The return address is accepted only as a path beneath `/console/`; anything
   else becomes `/console/`.

Every refusal ends at `/console/?sign-in=failed`. The address is the same for
every reason; the Hub's log names the step that refused, and never a value.

## The session

- The cookie is `__Host-conduit-session`: `HttpOnly`, `Secure`, `SameSite=Lax`,
  `Path=/`, no `Domain`. Its value is 256 random bits.
- The Hub stores the SHA-256 of that value and the claims the policy produced,
  not the value and not the token.
- A session ends at the earlier of the ID token's `exp` and
  `OPERATOR_SESSION_MAX_LIFETIME`. There is no refresh: when it ends, the person
  signs in again. The console offers "Sign in again" on a `401` and returns to
  the view that was on screen.
- With `REPOSITORY_KIND=postgres` sessions are in PostgreSQL (the
  `operator-sessions` component in
  [`schema-migrations.md`](schema-migrations.md)), so every replica honours a
  session and a sign-out reaches all of them. With the file repository they are
  in memory, and a restart signs everybody out.
- Sign out posts to `/v1/auth/logout`, which deletes the record on the Hub and
  clears the cookie. If the issuer advertises an `https` end-session endpoint
  the console then follows it. The ID token itself is not revoked at the issuer.

A session resolves to the same principal, with the same authorization, as a
bearer token carrying the same claims. A request with an `Authorization` header
or a verified client certificate never consults the cookie, including when that
credential is refused.

## Cross-site requests

A cookie is sent by the browser whether or not the console caused the request,
so the Hub accepts one as a credential only under these rules:

- If the request has an `Origin` header, it must equal `CONSOLE_PUBLIC_URL`. If
  it has `Sec-Fetch-Site`, that must be `same-origin`, or `none` for a read.
- A request that is not `GET`, `HEAD` or `OPTIONS` must also carry an `Origin`
  header and `X-Conduit-CSRF: 1`.

A cookie that fails these rules is answered `401`, as if it were absent. The
console sends the header on every write. Bearer-token and client-certificate
requests are exempt, because neither is attached by a browser on its own. The
Hub sends no CORS headers.

## Rate limiting

The three routes are public and spend from the anonymous bucket described in
[`hub-rate-limiting.md`](hub-rate-limiting.md). That bucket is what bounds how
many sign-in attempts an unauthenticated caller can leave in the store. A
request carrying a valid session spends from that operator's bucket.

## Deploying

### Zarf package

| Variable | Notes |
| --- | --- |
| `CONSOLE_PUBLIC_URL` | Defaults to `https://conduit.uds.dev`. Inert while `OPERATOR_OIDC_ISSUER` is empty. |
| `OPERATOR_OIDC_CLIENT_SECRET` | Name of the Secret whose `client-secret` key is mounted at `/run/conduit-sso`. Defaults to `conduit-hub-sso`. The mount is optional. |
| `OPERATOR_OIDC_CLIENT_SECRET_FILE` | Set to `/run/conduit-sso/client-secret` for a confidential client. Empty by default. |
| `OPERATOR_SESSION_MAX_LIFETIME` | Empty by default. |

With the `uds` component, the Package CR registers `conduit-hub` as a
confidential client whose only redirect address is
`<CONSOLE_PUBLIC_URL>/v1/auth/callback`, and has UDS Core write the generated
secret to `conduit-hub-sso` under the key `client-secret`.
`deploy/zarf/uds-config.yaml` sets the file path. See
[`uds-package.md`](uds-package.md).

If `OPERATOR_OIDC_CLIENT_SECRET_FILE` is set and the Secret is missing, the Hub
fails at start naming the file. It does not fall back to a public client.

### Helm chart

```yaml
operatorOIDC:
  enabled: true
  issuer: https://sso.example.test/realms/uds
  clientID: conduit-hub
  signIn:
    enabled: true
    publicURL: https://conduit.example.test
    clientSecret:
      secretName: conduit-hub-sso   # empty for a public client
      key: client-secret
    sessionMaxLifetime: ""          # empty for the default of 8h
    loginScopes: []                 # empty for profile email
```

`signIn.enabled` requires `operatorOIDC.enabled` and a `publicURL`; the chart
refuses to render otherwise. A named Secret is mounted as a required volume. The
chart has no value that carries the secret itself.

## `conduit login`

```sh
conduit login --oidc-issuer https://sso.example.test/realms/uds --oidc-client-id conduit-cli
conduit logout
```

`conduit login` signs a person in as a **public** client; it refuses
`--oidc-client-secret-file`. With `--flow auto` (the default) it uses the device
authorization grant when the issuer advertises one, and otherwise the
authorization code grant with PKCE, `state` and `nonce` over a redirect to an
ephemeral port on `127.0.0.1`. `--flow device` and `--flow loopback` choose one.
The identity provider must allow the grant for that client, and for the loopback
flow must accept a `http://127.0.0.1:<port>/callback` redirect address.

The token is written with mode `0600` to the bearer token file
(`--bearer-token-file`, or `token` beside the profile document) and is never
printed. A destination that is a link or a directory is refused before anybody
is asked to sign in. `--token id` (the default) stores the ID token, which is
what the Hub's generic policy verifies; `--token access` stores the access
token. `--login-timeout` bounds the wait, five minutes by default.

`conduit logout` removes the file. It does not revoke the token at the issuer.

## Limits

- No refresh token is kept by the Hub or the CLI. An expired session or token
  means signing in again.
- Sign-out ends the Hub session. It does not revoke the ID token, and it ends
  the identity provider's own session only if the issuer advertises an
  end-session endpoint and the person follows it.
- The Hub issues no personal API tokens.
- The Hub must be reached over TLS at `CONSOLE_PUBLIC_URL`. A `__Host-` cookie
  is not stored by a browser over plain HTTP, so the header simulation remains
  the way to run the console locally without TLS.
- This has been exercised against an in-process OIDC provider in tests. A
  browser sign-in against a deployed Keycloak with insecure headers off has not
  been run; see the status in [`uds-package.md`](uds-package.md).
