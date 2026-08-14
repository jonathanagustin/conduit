# Console runtime bootstrap contract

The Hub serves one immutable frontend bundle for every tenant. Before React is
mounted, the bundle fetches `GET /console/bootstrap.json` from the same origin
and validates the complete document against `conduit.console.bootstrap/v1`.
An invalid, missing, oversized, or unknown field stops rendering and shows a
generic configuration error; the browser never guesses tenant branding.

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
