# Runtime overlays

- Overlays may select production integrations but must not install or own their
  cluster-wide operators. Bootstrap repositories own SPIRE, secret providers,
  admission controllers, and their CRDs.
- Preserve the base runtime's bounded RBAC and single ownership. An overlay may
  mount an identity socket or externally owned Secret but may not grant
  cluster-admin or create application namespaces.
- Production identity must fail closed and use renewable workload identity;
  static certificate files remain a supported migration path, not the
  preferred profile.
