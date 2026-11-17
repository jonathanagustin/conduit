#!/usr/bin/env bash
# Install, upgrade and removal of the signed Conduit Zarf package on a k3d
# cluster (#832).
#
# UNEXERCISED. This script has been syntax-checked and its plan output is
# tested (scripts/test-zarf-package-lifecycle-plan.sh), but as of the commit
# that added it, it has never been run against a cluster. Treat its first run
# as the test of the script, not of the package.
#
# What it does, in order:
#   1. creates a k3d cluster and runs `zarf init` with labeled mutation;
#   2. creates the three Environment-owned Secrets with throwaway material;
#   3. deploys release N-1 from its GitHub Release asset with --verify=always
#      and that release's keyless certificate identity;
#   4. upgrades to release N, pulled from the OCI registry the publication
#      workflow pushes to, with --verify=always and N's identity;
#   5. removes the package;
#   6. asserts the Hub state PersistentVolumeClaim is the same object, by UID,
#      after the upgrade and after the removal.
#
# What it does not do, and #832 also asks for: block node egress, and show a
# Spoke completing a lease. Both use the default `file` profile here; the
# `postgres` profile is not exercised because nothing supplies a database.
#
# Requires docker, k3d, kubectl, zarf, gh, ruby, openssl and sha256sum on PATH, and
# GH_TOKEN for `gh release download`. `--print-plan` prints what it would do
# and touches nothing.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
repository="${GITHUB_REPOSITORY:-jonathanagustin/conduit}"
server="${GITHUB_SERVER_URL:-https://github.com}"
architecture="${LIFECYCLE_ARCHITECTURE:-amd64}"
cluster="${LIFECYCLE_CLUSTER:-conduit-lifecycle}"
version="${LIFECYCLE_VERSION:-$(tr -d '\n' < "${root}/VERSION")}"
previous="${LIFECYCLE_PREVIOUS_VERSION:-}"
issuer="https://token.actions.githubusercontent.com"
zarf="${ZARF_BIN:-zarf}"
namespace=platform-system
claim=platform-delivery-state

fail() {
  echo "$1" >&2
  exit 1
}

semantic='^[0-9]+\.[0-9]+\.[0-9]+$'
[[ "${version}" =~ ${semantic} ]] || fail "LIFECYCLE_VERSION must be X.Y.Z, got '${version}'"
[[ "${architecture}" =~ ^(amd64|arm64)$ ]] || fail "LIFECYCLE_ARCHITECTURE must be amd64 or arm64"

# The identity a release's package was signed with is the publication workflow
# at that release's tag. It is derived, never taken from the package.
identity() {
  printf '%s/%s/.github/workflows/platform-images.yml@refs/tags/v%s' "${server}" "${repository}" "$1"
}
archive() {
  printf 'zarf-package-platform-runtime-%s-%s.tar.zst' "${architecture}" "$1"
}
published="oci://ghcr.io/${repository}/packages/platform-runtime:${version}"

if [[ "${1:-}" == "--print-plan" ]]; then
  [[ -n "${previous}" ]] || previous='<latest release other than this one>'
  cat <<PLAN
cluster=${cluster}
architecture=${architecture}
previous=${previous}
previous_source=release-asset:$(archive "${previous}")
previous_identity=$(identity "${previous}")
version=${version}
version_source=${published}
version_identity=$(identity "${version}")
issuer=${issuer}
verify=always
retained=persistentvolumeclaim/${claim}
PLAN
  exit 0
fi

for command in docker k3d kubectl gh ruby openssl sha256sum "${zarf}"; do
  command -v "${command}" >/dev/null || fail "missing required command: ${command}"
done

if [[ -z "${previous}" ]]; then
  previous="$(gh release list --repo "${repository}" --exclude-drafts --exclude-pre-releases --limit 30 --json tagName \
    --jq "[.[].tagName | select(test(\"^v[0-9]+\\\\.[0-9]+\\\\.[0-9]+\$\")) | select(. != \"v${version}\")][0] // \"\"")"
  previous="${previous#v}"
fi
[[ "${previous}" =~ ${semantic} ]] || fail "could not determine the previous release; set LIFECYCLE_PREVIOUS_VERSION"
[[ "${previous}" != "${version}" ]] || fail "the previous release must differ from ${version}"

work="$(mktemp -d)"
cleanup() {
  status=$?
  if [[ "${status}" -ne 0 ]]; then
    kubectl --context "k3d-${cluster}" -n "${namespace}" get deploy,pods,pvc,events 2>&1 | tail -60 || true
  fi
  k3d cluster delete "${cluster}" >/dev/null 2>&1 || true
  rm -rf "${work}"
  exit "${status}"
}
trap cleanup EXIT

echo "== fetch release ${previous} from its GitHub Release"
gh release download "v${previous}" --repo "${repository}" --dir "${work}" \
  --pattern "$(archive "${previous}")" --pattern "$(archive "${previous}").sha256"
(cd "${work}" && sha256sum --check "$(archive "${previous}").sha256")

echo "== create cluster ${cluster}"
k3d cluster delete "${cluster}" >/dev/null 2>&1 || true
k3d cluster create "${cluster}" --wait
kubectl config use-context "k3d-${cluster}" >/dev/null
"${zarf}" tools download-init --output-directory "${work}" --architecture "${architecture}"
(cd "${work}" && "${zarf}" init --confirm --agent-mutation-policy=labeled --architecture "${architecture}")

echo "== create the Environment-owned Secrets with throwaway material"
(
  cd "${work}"
  openssl req -x509 -newkey rsa:2048 -nodes -keyout ca.key -out ca.crt -days 2 -subj "/CN=conduit-lifecycle-ca" 2>/dev/null
  openssl req -newkey rsa:2048 -nodes -keyout hub.key -out hub.csr \
    -subj "/CN=platform-delivery.${namespace}.svc.cluster.local" 2>/dev/null
  printf 'subjectAltName=DNS:platform-delivery.%s.svc.cluster.local,DNS:platform-delivery.%s.svc,DNS:platform-delivery\nextendedKeyUsage=serverAuth\n' \
    "${namespace}" "${namespace}" > hub.ext
  openssl x509 -req -in hub.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out hub.crt -days 2 -extfile hub.ext 2>/dev/null
  openssl req -newkey rsa:2048 -nodes -keyout agent.key -out agent.csr -subj "/CN=spoke-lifecycle" 2>/dev/null
  printf 'extendedKeyUsage=clientAuth\n' > agent.ext
  openssl x509 -req -in agent.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out agent.crt -days 2 -extfile agent.ext 2>/dev/null
  openssl genpkey -algorithm Ed25519 -out bundle-signing-key.pem
)
kubectl create namespace "${namespace}"
kubectl label namespace "${namespace}" zarf.dev/agent=mutate
kubectl -n "${namespace}" create secret generic platform-hub-tls \
  --from-file=ca.crt="${work}/ca.crt" --from-file=tls.crt="${work}/hub.crt" --from-file=tls.key="${work}/hub.key"
kubectl -n "${namespace}" create secret generic platform-agent-tls \
  --from-file=ca.crt="${work}/ca.crt" --from-file=tls.crt="${work}/agent.crt" --from-file=tls.key="${work}/agent.key"
kubectl -n "${namespace}" create secret generic platform-bundle-signing \
  --from-file=bundle-signing-key.pem="${work}/bundle-signing-key.pem"

deploy() {
  local source="$1" release="$2"
  "${zarf}" package deploy "${source}" \
    --verify=always \
    --certificate-identity "$(identity "${release}")" \
    --certificate-oidc-issuer "${issuer}" \
    --architecture "${architecture}" \
    --set-variables ENVIRONMENT_ID=lifecycle \
    --set-variables AGENT_ID=spoke-lifecycle \
    --confirm
  kubectl -n "${namespace}" rollout status deploy/platform-delivery --timeout=300s
  kubectl -n "${namespace}" rollout status deploy/platform-spoke-agent --timeout=300s
}
# What the cluster is actually running, read back two ways so a deploy that
# reported success without changing anything is not a pass: the version Zarf
# recorded for the deployed package, and the Hub image in the Deployment
# against the HUB_IMAGE constant of the package that was just verified.
definition_field() {
  ruby -ryaml -e '
    definition = YAML.safe_load(STDIN.read)
    case ARGV.fetch(0)
    when "version" then puts definition.dig("metadata", "version")
    when "hub" then puts definition.fetch("constants").find { |item| item["name"] == "HUB_IMAGE" }.fetch("value")
    end
  ' "$1"
}
assert_running() {
  local source="$1" release="$2" recorded expected actual
  recorded="$("${zarf}" package inspect definition platform-runtime --no-color | definition_field version)"
  [[ "${recorded}" == "${release}" ]] || fail "Zarf records platform-runtime ${recorded}, expected ${release}"
  expected="$("${zarf}" package inspect definition "${source}" --architecture "${architecture}" --no-color | definition_field hub)"
  actual="$(kubectl -n "${namespace}" get deploy/platform-delivery -o jsonpath='{.spec.template.spec.containers[0].image}')"
  [[ "${expected}" =~ @sha256:[a-f0-9]{64}$ ]] || fail "package ${release} declares no digest-pinned Hub image: ${expected}"
  [[ "${actual}" == "${expected}" ]] || fail "the Hub runs ${actual}, but package ${release} declares ${expected}"
}
claim_uid() {
  kubectl -n "${namespace}" get "pvc/${claim}" -o jsonpath='{.metadata.uid}'
}

echo "== install ${previous} with signature verification"
deploy "${work}/$(archive "${previous}")" "${previous}"
assert_running "${work}/$(archive "${previous}")" "${previous}"
installed_uid="$(claim_uid)"
[[ -n "${installed_uid}" ]] || fail "the Hub state claim does not exist after install"

echo "== refuse the same package under the wrong signer"
if "${zarf}" package verify "${work}/$(archive "${previous}")" \
  --certificate-identity "$(identity "${previous}")-not-the-signer" --certificate-oidc-issuer "${issuer}" >/dev/null 2>&1; then
  fail "verification accepted a certificate identity that did not sign the package"
fi

echo "== upgrade to ${version} from ${published}"
deploy "${published}" "${version}"
assert_running "${published}" "${version}"
[[ "$(claim_uid)" == "${installed_uid}" ]] || fail "the upgrade replaced the Hub state claim"

echo "== remove"
"${zarf}" package remove platform-runtime --confirm
remaining="$(kubectl -n "${namespace}" get deploy -o name 2>/dev/null | grep -c 'platform-' || true)"
[[ "${remaining}" == "0" ]] || fail "removal left ${remaining} platform Deployments behind"
[[ "$(claim_uid)" == "${installed_uid}" ]] || fail "removal did not retain the Hub state claim ${claim}"

echo "Zarf package lifecycle: installed ${previous}, upgraded to ${version}, removed, and ${claim} (${installed_uid}) was retained throughout"
