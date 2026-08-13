# Executable composition

- Keep binaries thin: parse externally supplied configuration, assemble
  services, and own process lifecycle. Domain behavior belongs under
  `internal`.
- Fail startup when production identity or signing configuration is incomplete;
  never silently enable insecure identity headers.
- Development identity flags must remain explicit, local-only opt-ins. Keep
  Agent and operator identities distinct.
- Read credentials and trust material from mounted files or injected resolvers;
  do not persist or log their contents.
