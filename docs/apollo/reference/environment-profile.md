# Environment profile and compliance metadata

Sources reviewed:

- [Editing Environment management settings](https://www.palantir.com/docs/apollo/managing-environments/editing-environment-management-settings/)
- [Configure Environment settings](https://www.palantir.com/docs/apollo/managing-environments/environment-settings/)
- [Change type API reference](https://www.palantir.com/docs/apollo/cel-policies/input-types/)

## Public semantics adapted

- Environment display metadata comprises a display name, short description,
  and Markdown README. It is presentation context, not desired workload state.
- Management metadata identifies the Environment's immutable ID, accreditation,
  primary/additional contact teams, operational-responsibility routing,
  namespace registrations, and optional support override.
- Accreditation is compliance input. Apollo may use it in approval workflows;
  Platform exposes it to CEL as an explicit Environment resource attribute
  rather than introducing an implicit Plan constraint.
- Operational responsibility selects alert/support routing between Environment
  and Product contact teams. Platform records the switch and contacts but leaves
  notification delivery to the external notification integration.

## Platform contract and evidence

`internal/controlplane/environment_profiles.go` stores this Hub-owned profile
outside Git-owned `DesiredState`. Every optimistic revision records source,
operator, rationale, and timestamps and remains available in immutable history.
Environment RBAC protects read and update HTTP routes. Contact teams must exist.
HTTP updates create `UPDATE_ENVIRONMENT_PROPERTIES` Change Requests. A profile
is not visible while review is pending; immediate policy approval or a later
eligible reviewer applies the exact proposed revision and records terminal
execution evidence. A stale approved revision fails rather than overwriting a
newer profile.

The profile never changes Plan identity, steps, eligibility, or desired-state
digest by itself. Approval policies can deliberately read attributes such as
`referencedResources.environment.resource.accreditation`; tests prove both the
non-interference default and the explicit-policy case.

This is a Platform-native metadata and policy contract, not compatibility with
private Apollo management APIs or notification routing.
