# Synthetic self-Product fixture

`synthetic-prepared.json` contains invented digests and a synthetic source commit.
It is **not a released descriptor and must not be published**. The CLI input
bytes used to generate it are explicitly non-executable synthetic text.

`scripts/fixtures/self-products.mjs` constructs its inputs. The Node test requires
exact regeneration; the Go executable-composition test imports the two generated
native Helm bodies into a disposable Hub repository and checks exact Releases,
configuration receipts, channels and the absence of fabricated assessments.
Neither test uses a registry, deploys a cluster or validates real release assets.
