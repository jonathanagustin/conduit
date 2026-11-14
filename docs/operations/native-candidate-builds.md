# Local native candidate builds

Optimizer candidate builds remain separate from lifecycle admission and
publication. A local build does not satisfy the outstanding native architecture,
reproducibility, registry provenance, shadow-calibration or owner licensing
decisions. Historical evidence retains its original source and toolchain pins.

The CP-SAT Containerfile stages checksum-verified inputs in a small scratch
stage and performs compilation in one Rust build step. This avoids repeated
copies of the large compiler filesystem on Docker's `vfs` storage driver.
The Cargo and Bazel cache mounts retain their existing locked sharing policy.

In a proxy-managed environment, the operator can supply the host's existing
public CA trust stores as optional BuildKit mounts:

```sh
docker buildx build --load --platform linux/amd64 \
  --secret id=platform-build-ca,src=/etc/ssl/certs/ca-certificates.crt \
  --secret id=platform-build-java-cacerts,src=/etc/ssl/certs/java/cacerts \
  --build-arg BUILDARCH=amd64 --build-arg TARGETARCH=amd64 \
  --build-arg "REVISION=$(git rev-parse HEAD)" \
  --build-arg SOURCE_DATE_EPOCH=1768232310 \
  --tag conduit-cpsat-candidate:local -f Containerfile.optimizer-cpsat .
```

The PEM bundle configures Curl, Cargo and Bazelisk; the Java trust store
configures Bazel's JVM. TLS verification remains enabled. These mounts are
available during the build step and are not copied into the candidate rootfs.
Use the operator's reviewed trust configuration; do not commit trust-store
contents or private keys. Builds on ordinary public-network infrastructure can
omit both mounts and use the builder's default trust stores.
