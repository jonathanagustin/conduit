#!/usr/bin/env bash
# Sourced by the lifecycle lane with its private work directory, pinned CLI,
# identity/archive helpers and qualified_sources map. No cluster setup here.
deploy() {
  local source="$1" release="$2" definition api
  local -a inputs
  if [[ "${source}" == oci://* ]]; then
    # Resolve the tag once into a fresh local archive. Inspection, deployment
    # and readback must not resolve independently mutable registry tags.
    mkdir "${work}/pulled-${release}"
    "${zarf}" package pull "${source}" \
      --verify=always --certificate-identity "$(identity "${release}")" \
      --certificate-oidc-issuer "${issuer}" --architecture "${architecture}" \
      --output-directory "${work}/pulled-${release}"
    source="${work}/pulled-${release}/$(archive "${release}")"
  fi
  [[ -f "${source}" && ! -L "${source}" ]] || fail "missing regular release archive for ${release}"
  definition="${work}/verified-${release}.yaml"
  "${zarf}" package inspect definition "${source}" \
    --verify=always --certificate-identity "$(identity "${release}")" \
    --certificate-oidc-issuer "${issuer}" --architecture "${architecture}" \
    --log-level error --no-color > "${definition}"
  api="$(ruby "${root}/scripts/zarf-lifecycle-definition.rb" api "${release}" < "${definition}")"
  ruby "${root}/scripts/zarf-lifecycle-definition.rb" hub "${release}" < "${definition}" > /dev/null
  case "${api}" in
    zarf.dev/v1alpha1) inputs=(--set-variables ENVIRONMENT_ID=lifecycle --set-variables AGENT_ID=spoke-lifecycle) ;;
    zarf.dev/v1beta1)
      printf '%s\n' '{"ENVIRONMENT_ID":"lifecycle","AGENT_ID":"spoke-lifecycle"}' > "${work}/values-${release}.yaml"
      inputs=(--values "${work}/values-${release}.yaml") ;;
    *) fail "unsupported release package API: ${api}" ;;
  esac
  qualified_sources["${release}"]="${source}"
  "${zarf}" package deploy "${source}" \
    --verify=always \
    --certificate-identity "$(identity "${release}")" \
    --certificate-oidc-issuer "${issuer}" \
    --architecture "${architecture}" \
    "${inputs[@]}" \
    --confirm
  kubectl -n "${namespace}" rollout status deploy/platform-delivery --timeout=300s
  kubectl -n "${namespace}" rollout status deploy/platform-spoke-agent --timeout=300s
}
