# Reviewed Cosign source build

The Hub builds Cosign 3.1.4 from the locked module graph in
`tools/cosign-build`, using the same pinned Go 1.27.4 compiler as the Hub.
Publication and keyless signing keep their existing manual authority boundaries.

The source build needs two security updates beyond the upstream release graph:

- [GO-2026-6865](https://pkg.go.dev/vuln/GO-2026-6865) affects
  `github.com/klauspost/compress` before 1.20.2. The reviewed graph selects
  1.20.3, which also satisfies the maintained credential helper's dependencies.
- [GO-2026-6225](https://pkg.go.dev/vuln/GO-2026-6225) affects the legacy
  `github.com/chrismellard/docker-credential-acr-env` helper. Its unanchored
  hostname expression accepts names such as `registry.azurecr.io.attacker.example`
  before exchanging an Azure token. There is no fixed legacy-module release.
  The [upstream report](https://github.com/chrismellard/docker-credential-acr-env/issues/21)
  directs consumers to the maintained
  [osscontainertools helper](https://github.com/osscontainertools/docker-credential-acr).

`compat/acr` is a local compatibility adapter for the legacy constructor import
that Cosign still consumes. It delegates to the maintained helper at 1.0.1,
commit `6cc928aa5b98262c59cfdac3727088b29d835267`. It contains no copied legacy
token-exchange implementation and does not assert that the vulnerable upstream
version was fixed. The replacement and maintained module version are explicit
in the module graph; both the Docker context and the Hub Containerfile include
the adapter source. The supply-chain verifier holds these pins together.

`scripts/test-cosign-credential-helper.sh` exercises the constructor actually
consumed by Cosign. Attacker suffixes, user information and malformed references
must be refused before credential acquisition. Recognized Azure hosts, including
dedicated data endpoints and sovereign suffixes, must reach a deterministic
invalid-cloud configuration error before any token acquisition or network call.
These tests use no Azure credentials and do not establish a live Azure integration.

The separate compiled-binary vulnerability scan and networkless local-key
sign/verify/tamper checks remain part of maintenance validation. Local-key checks
without transparency-log verification do not establish keyless publication,
registry authorization or public verifiability.
