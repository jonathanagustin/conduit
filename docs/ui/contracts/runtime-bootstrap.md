# Console runtime bootstrap contract

The Hub serves one immutable frontend bundle for every tenant. The tenant
document is not fetched: the Hub renders it once at handler construction into
an `x-conduit-console-bootstrap` `<meta>` in the application shell, replacing
the `<!--conduit-console-bootstrap-->` marker the built `index.html` carries
(`hydrateShellDocument`). Before React is mounted, the bundle reads that meta
and validates the complete document against `conduit.console.bootstrap/v1`
(`loadRuntimeBootstrap`). An invalid, missing, oversized, or unknown field
stops rendering and shows a generic configuration error; the browser never
guesses tenant branding. A shell served by anything other than the Hub arrives
unhydrated, and an unhydrated shell is a refusal rather than a render with
invented branding.

There is deliberately no `GET /console/bootstrap.json` route. An earlier design
fetched one, and the route is now gone rather than left serving a second,
drifting copy of the same document; `handler_test.go` asserts it answers 404.

The closed runtime surface contains only:

- tenant `id` and `displayName`;
- brand `title`, `subtitle`, six-digit hexadecimal `accent`, and one of the
  `application`, `cloud`, or `dashboard` icons;
- provenance class `tenant-observation` with an `evidence://` coordinate, or
  `conduit-extension` with a `conduit://` coordinate.

There are deliberately no fields for credentials, executable code, API
origins, authentication, CSP, capabilities, navigation, feature flags, or
security policy. React renders the approved strings as text. The Hub retains
its fixed same-origin CSP, and [`session-capabilities.md`](session-capabilities.md)
remains the sole application-navigation authority.

The Hub decodes a configured `CONSOLE_BOOTSTRAP_FILE` with a 64 KiB limit,
unknown-field rejection, and the same value rules as the browser. An unset path
selects an explicit Conduit-native default; a configured file that cannot be
read or validated fails Hub startup. The Helm chart writes the typed
`consoleBootstrap` values to a read-only ConfigMap mount and hashes the document
into the Pod template so an approved change rolls the Hub.

Shared synthetic fixtures under `testdata/console-bootstrap` pass through both
the Go and TypeScript validators. Negative tests reject security-policy fields,
scripts, external API origins, arbitrary CSS colors/icons, invented claim
classes, and evidence-scheme mismatches.

Source anchors: `internal/consoleui/bootstrap.go`,
`internal/consoleui/handler.go`, `web/src/runtime/bootstrap.ts`, and
`deploy/helm/platform-hub/templates/console-bootstrap.yaml`.
