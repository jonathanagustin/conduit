# Executable composition

- Keep binaries thin: parse externally supplied configuration, assemble
  services, and own process lifecycle. Domain behavior belongs under
  `internal`.
- Fail startup when production identity or signing configuration is incomplete;
  never silently enable insecure identity headers.
- Development identity flags must remain explicit, local-only opt-ins. Keep
  Agent and operator identities distinct.
- Signing is identity too, and it fails closed the same way: a Hub with no
  `BUNDLE_SIGNING_KEY_FILE` starts only when it declares
  `ALLOW_INSECURE_EPHEMERAL_BUNDLE_SIGNING_KEY=true`, which may not be combined
  with a mounted key or with production identity. Never restore the silent
  ephemeral fallback — a Hub that invents a signer comes up healthy and its
  Bundles stop verifying against the fleet.
- Read credentials and trust material from mounted files or injected resolvers;
  do not persist or log their contents. A key *identifier* may be logged when it
  is the public half, so an operator can tell which signer a Hub came up with.
