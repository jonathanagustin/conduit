# Immutable Product artifact publication

Reviewed 2026-08-02 UTC from Apollo's public versioned-Helm guidance captured
in the 2026-08-01 documentation inventory. This note records the durable
deployment contract without copying the source page.

## Contract

- A container image alone is not a complete portable Release. The resource,
  configuration, exposure, and execution instructions around it are equally
  versioned deployment content.
- A Helm Product must package and publish an immutable, semantically versioned
  chart to a chart registry. A Product Release addresses that chart location
  with a Maven coordinate.
- Environment-specific variance belongs in Helm values. Environment branches,
  tags, folders, or repositories may review source, but must not become
  divergent environment-local definitions of the deployable chart version.
- Source Git tags can drive the SemVer build version. The publication boundary
  is still `helm package` followed by `helm push` to the registry.
- Immutable chart identity enables fleet inventory and deterministic recall or
  roll-off to a known-good Release.

## Platform consequence

- Platform Release models require both the chart tag and exact SHA-256 digest,
  and do the same for referenced images. The digest requirement is a deliberate
  strengthening of the prose's versioned-registry contract.
- Git and Flux reconcile reviewed control-plane/runtime source. They do not
  replace immutable Product chart publication, and Flux must not also own Helm
  releases executed by the Spoke.
- The Spoke pulls and verifies the selected registry chart digest before Helm
  execution. Environment registry assignments may rewrite repository hosts but
  preserve path, tag, and digest.
- Product repositories own chart source, packaging, and immutable publication;
  this reusable repository owns only the admission and execution contracts.

## Primary source

- <https://www.palantir.com/docs/apollo/core/publishing-versioned-helm-charts/>
- Snapshot SHA-256:
  `37e5cfd801151a7604c62c22e17c864e3210d0ec6c2afa93e32700feb66ed004`
