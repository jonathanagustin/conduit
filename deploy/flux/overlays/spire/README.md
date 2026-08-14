# SPIRE production runtime profile

This overlay is the preferred production identity profile. It consumes the
SPIFFE Workload API through the SPIFFE CSI driver and lets the official
`go-spiffe` client maintain rotating X509-SVIDs and trust bundles. It does not
install SPIRE or own cluster-wide identity policy.

The bootstrap repository must:

1. install the official SPIRE hardened chart and CRDs at reviewed immutable
   versions;
2. create `platform-delivery` and `platform-spoke-agent` ServiceAccounts in
   `platform-system`;
3. attest `platform-delivery` as `spiffe://platform.delivery/hub`;
4. attest the Spoke as
   `spiffe://platform.delivery/environment/<environment>/agent/<agent>` using
   selectors that include its namespace and ServiceAccount;
5. create the externally owned `platform-bundle-signing` Secret with a single
   `bundle-signing-key.pem` PKCS#8 Ed25519 private key, managed and rotated by
   the Environment's audited external secret system; and
6. set the Flux Kustomization path to `./overlays/spire`, keep both insecure
   identity substitutions `false`, and grant its bounded reconciler permission
   only for the runtime resource kinds it owns.

The current reviewed component baseline is recorded in
`docs/security/TRUSTED-TECHNOLOGY-BASELINE.md`. Environment repositories retain
the exact chart/image digests and identity registrations because those are
deployment authority, not reusable Conduit source.
