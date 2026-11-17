# UDS CLI v0.41.0 NextMode. Wraps one signed beta runtime package;
# UDS Core remains an Environment-owned prerequisite.
uds {
  bundle_api_version = "uds.dev/v1alpha1"
}

locals {
  version = "0.18.0" # x-release-please-version
}

metadata {
  name = "conduit"
  description = "Conduit Hub and Spoke runtime for a cluster with UDS Core"
  version = local.version
}

package "platform_runtime" {
  source = "./zarf-package-platform-runtime-${sys.arch}-${local.version}.tar.zst"
  optional_components = ["uds"]
  values_files = ["runtime-values.yaml"]
  # BEGIN PACKAGE TRUST
  signature_verification {
    keyless {
      certificate_identity = "https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/tags/v${local.version}"
      certificate_oidc_issuer = "https://token.actions.githubusercontent.com"
    }
  }
  # END PACKAGE TRUST
}
