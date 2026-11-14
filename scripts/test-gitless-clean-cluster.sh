#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tool_cache="${PLATFORM_GITLESS_CACHE:-/tmp/workspace-offload/conduit/gitless-tools}"
test_root="$(mktemp -d "${TMPDIR:-/tmp}/conduit-gitless.XXXXXX")"
bin_dir="${tool_cache}/bin"
cluster_name="conduit-gitless-${$}"
registry_pid=""

cleanup() {
	local status=$?
	if [[ "${status}" -ne 0 ]]; then
		for log in "${test_root}/zot.stdout" "${test_root}/zot.log"; do
			[[ -f "${log}" ]] && sed -n '1,160p' "${log}" >&2
		done
	fi
  if [[ -n "${registry_pid}" ]] && kill -0 "${registry_pid}" 2>/dev/null; then
    kill "${registry_pid}" 2>/dev/null || true
    wait "${registry_pid}" 2>/dev/null || true
  fi
  if [[ -x "${bin_dir}/k3d" ]]; then
    "${bin_dir}/k3d" cluster delete "${cluster_name}" >/dev/null 2>&1 || true
  fi
  case "${test_root}" in
    "${TMPDIR:-/tmp}"/conduit-gitless.*) rm -rf -- "${test_root}" ;;
    *) echo "refusing to remove unexpected test root ${test_root}" >&2 ;;
  esac
}
trap cleanup EXIT

for command in curl sha256sum tar jq base64 docker kubectl python3; do
  command -v "${command}" >/dev/null || { echo "${command} is required" >&2; exit 1; }
done
docker info >/dev/null
mkdir -p "${bin_dir}"

fetch() {
  local url="$1" sha="$2" target="$3"
  if [[ ! -f "${target}" ]] || ! echo "${sha}  ${target}" | sha256sum --check --status; then
    local temporary="${target}.tmp-${$}"
    curl --fail --silent --show-error --location "${url}" --output "${temporary}"
    echo "${sha}  ${temporary}" | sha256sum --check --status
    mv "${temporary}" "${target}"
  fi
}

fetch \
  "https://github.com/k3d-io/k3d/releases/download/v5.7.4/k3d-linux-amd64" \
  "1ac1da365236736a8df8c32107b54aca208384ab1d9a06771443c85ad698a5eb" \
  "${bin_dir}/k3d"
fetch \
  "https://github.com/sigstore/cosign/releases/download/v3.1.4/cosign-linux-amd64" \
  "424e2c97d0b45fd7a6b608a87f090f35633f4c23ece5f092d48bba11aaf4bbfa" \
  "${bin_dir}/cosign"
fetch \
  "https://github.com/project-zot/zot/releases/download/v2.1.20/zot-linux-amd64" \
  "a32e42d042d1f17b5b1317e55cc1a415a744c873dcd05c25c56b665478258bcb" \
  "${bin_dir}/zot"
fetch \
  "https://github.com/fluxcd/flux2/releases/download/v2.9.4/flux_2.9.4_linux_amd64.tar.gz" \
  "c2c397a52930f52d2005c01d276116b059d062de379386d58e98115380a766a2" \
  "${tool_cache}/flux.tar.gz"
fetch \
  "https://github.com/oras-project/oras/releases/download/v1.3.3/oras_1.3.3_linux_amd64.tar.gz" \
  "9ce999f8d2de03fc03968b29d743077a58783e545e5eaa53917ca177352d0e59" \
  "${tool_cache}/oras.tar.gz"
if [[ ! -x "${bin_dir}/flux" ]]; then
  tar -xzf "${tool_cache}/flux.tar.gz" -C "${bin_dir}" flux
fi
if [[ ! -x "${bin_dir}/oras" ]]; then
  tar -xzf "${tool_cache}/oras.tar.gz" -C "${bin_dir}" oras
fi
chmod 0755 "${bin_dir}/k3d" "${bin_dir}/cosign" "${bin_dir}/zot" "${bin_dir}/flux" "${bin_dir}/oras"
export PATH="${bin_dir}:${PATH}"

registry_port="$(python3 - <<'PY'
import socket
s = socket.socket()
s.bind(("127.0.0.1", 0))
print(s.getsockname()[1])
s.close()
PY
)"
publisher_user="publisher"
publisher_password="conduit-test-publisher"
puller_user="puller"
puller_password="conduit-test-puller"

# Zot v2 accepts bcrypt entries only. These hashes bind the fixed, synthetic
# credentials above and are safe to commit because the registry is disposable
# and reachable only for this acceptance run.
publisher_password_hash='$2a$10$ppWQTPosFZ93V6MvLvibh.BEMKXeIZYJBgz2dBObb0FvSGYgeCZxe'
puller_password_hash='$2a$10$Yk54pPhhOfPu1LP6ayh59OVE9HRuev6fJj9L5LgtJAwyRIr4SYYn.'
printf '%s:%s\n%s:%s\n' \
  "${publisher_user}" "${publisher_password_hash}" \
  "${puller_user}" "${puller_password_hash}" \
  >"${test_root}/htpasswd"
cat >"${test_root}/zot.json" <<EOF
{
  "distSpecVersion": "1.1.1",
  "storage": {"rootDirectory": "${test_root}/registry"},
  "http": {
    "address": "0.0.0.0",
    "port": "${registry_port}",
    "realm": "conduit-gitless-acceptance",
    "auth": {"htpasswd": {"path": "${test_root}/htpasswd"}, "failDelay": 0},
    "accessControl": {
      "repositories": {
        "conduit/**": {
          "policies": [
            {"users": ["${publisher_user}"], "actions": ["read", "create", "update"]},
            {"users": ["${puller_user}"], "actions": ["read"]}
          ]
        }
      }
    }
  },
  "log": {"level": "info", "output": "${test_root}/zot.log"}
}
EOF
zot verify "${test_root}/zot.json" >/dev/null
zot serve "${test_root}/zot.json" >"${test_root}/zot.stdout" 2>&1 &
registry_pid=$!
for _ in $(seq 1 60); do
  if curl --fail --silent --user "${publisher_user}:${publisher_password}" \
    "http://127.0.0.1:${registry_port}/v2/" >/dev/null 2>&1; then
    break
  fi
  sleep 0.25
done
curl --fail --silent --user "${publisher_user}:${publisher_password}" \
  "http://127.0.0.1:${registry_port}/v2/" >/dev/null

commit="$(git -C "${repo_root}" rev-parse HEAD)"
mkdir -p "${test_root}/source/runtime"
cat >"${test_root}/source/runtime/kustomization.yaml" <<'EOF'
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - marker.yaml
EOF
cat >"${test_root}/source/runtime/marker.yaml" <<EOF
apiVersion: v1
kind: ConfigMap
metadata:
  name: conduit-runtime-source
  namespace: conduit-acceptance
data:
  sourceRepository: https://github.com/jonathanagustin/conduit
  sourceCommit: ${commit}
  transport: signed-private-oci
EOF

host_registry="127.0.0.1:${registry_port}"
right_repository="${host_registry}/conduit/runtime"
wrong_repository="${host_registry}/conduit/runtime-wrong"
right_push="$(flux push artifact "oci://${right_repository}:acceptance" \
  --path="${test_root}/source" \
  --source="https://github.com/jonathanagustin/conduit" \
  --revision="codex/complete-cutover-backlog@sha1:${commit}" \
  --creds="${publisher_user}:${publisher_password}" \
  --insecure-registry --reproducible --output=json)"
wrong_push="$(flux push artifact "oci://${wrong_repository}:acceptance" \
  --path="${test_root}/source" \
  --source="https://github.com/jonathanagustin/conduit" \
  --revision="codex/complete-cutover-backlog@sha1:${commit}" \
  --creds="${publisher_user}:${publisher_password}" \
  --insecure-registry --reproducible --output=json)"
right_digest="$(jq -er '.digest | select(test("^sha256:[a-f0-9]{64}$"))' <<<"${right_push}")"
wrong_digest="$(jq -er '.digest | select(test("^sha256:[a-f0-9]{64}$"))' <<<"${wrong_push}")"
[[ "${right_digest}" == "${wrong_digest}" ]] || {
  echo "reproducible byte-identical source produced different digests" >&2
  exit 1
}

mkdir -p "${test_root}/docker"
export DOCKER_CONFIG="${test_root}/docker"
printf '%s' "${publisher_password}" | cosign login "${host_registry}" \
  --username "${publisher_user}" --password-stdin >/dev/null
# Exercise the production mounted-file credential adapters against this real,
# authenticated registry. The ordinary ocipublish acceptance uses an anonymous
# local registry; this is the case that proves the Hub's private write path.
#
# The verification case runs the real cosign binary through Conduit's verifier,
# with argv and environment decided entirely by the adapter. This suite used to
# invoke cosign only directly, which is how an adapter passing a flag cosign does
# not have shipped in #220: the acceptance ran a correct command line while the
# product ran an impossible one. The artifact is not signed yet at this point in
# the script, and that is deliberate -- an unsigned artifact must come back as an
# observation rather than as a verifier error, and an invented flag destroys
# exactly that distinction.
(
  cd "${repo_root}"
  GITLESS_REGISTRY_ACCEPTANCE_REPOSITORY="${host_registry}/conduit/hub-manifests" \
  GITLESS_REGISTRY_CONFIG="${DOCKER_CONFIG}/config.json" \
  GITLESS_COSIGN_ACCEPTANCE_PIN="${right_repository}@${right_digest}" \
  GITLESS_COSIGN_ACCEPTANCE_PLAINTEXT=true \
    go test ./internal/gitless/adapters/registryauth \
      -run 'TestPrivateRegistryPublicationAcceptance|TestPrivateRegistryVerificationAcceptance' \
      -count=1 -v
)
export COSIGN_PASSWORD="conduit-test-key-password"
cosign generate-key-pair --output-key-prefix "${test_root}/trusted" >/dev/null
cosign generate-key-pair --output-key-prefix "${test_root}/wrong" >/dev/null
cosign sign --yes --key "${test_root}/trusted.key" --allow-insecure-registry \
  "${right_repository}@${right_digest}" >/dev/null
cosign sign --yes --key "${test_root}/wrong.key" --allow-insecure-registry \
  "${wrong_repository}@${wrong_digest}" >/dev/null
unset COSIGN_PASSWORD

printf '%s' "${puller_password}" | oras login --plain-http "${host_registry}" \
  --username "${puller_user}" --password-stdin >/dev/null
oras manifest fetch --plain-http "${right_repository}@${right_digest}" >/dev/null
if oras push --plain-http "${host_registry}/conduit/forbidden:acceptance" \
  "${test_root}/source/runtime/marker.yaml:application/yaml" \
  >"${test_root}/puller-push.stdout" 2>"${test_root}/puller-push.stderr"; then
  echo "cluster pull identity unexpectedly has registry write authority" >&2
  exit 1
fi

export KUBECONFIG="${test_root}/kubeconfig"
k3d cluster create "${cluster_name}" \
  --servers 1 --agents 0 --no-lb --wait --timeout 180s \
  --k3s-arg "--disable=traefik@server:0" \
  --kubeconfig-update-default=false --kubeconfig-switch-context=false >/dev/null
k3d kubeconfig get "${cluster_name}" >"${KUBECONFIG}"
flux install --components=source-controller,kustomize-controller \
  --network-policy=false --timeout=5m >/dev/null
kubectl -n flux-system rollout status deployment/source-controller --timeout=180s >/dev/null
kubectl -n flux-system rollout status deployment/kustomize-controller --timeout=180s >/dev/null

kubectl create namespace conduit-acceptance >/dev/null
cat <<'EOF' | kubectl apply -f - >/dev/null
apiVersion: v1
kind: ServiceAccount
metadata:
  name: conduit-runtime-reconciler
  namespace: flux-system
automountServiceAccountToken: false
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: conduit-runtime-reconciler
  namespace: conduit-acceptance
rules:
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: conduit-runtime-reconciler
  namespace: conduit-acceptance
subjects:
  - kind: ServiceAccount
    name: conduit-runtime-reconciler
    namespace: flux-system
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: conduit-runtime-reconciler
EOF

cluster_registry="host.k3d.internal:${registry_port}"
pull_auth="$(printf '%s:%s' "${puller_user}" "${puller_password}" | base64 | tr -d '\r\n')"
docker_config="$(jq -cn --arg server "${cluster_registry}" --arg auth "${pull_auth}" \
  '{auths: {($server): {auth: $auth}}}')"
kubectl -n flux-system create secret generic conduit-runtime-pull \
  --type=kubernetes.io/dockerconfigjson \
  --from-literal=.dockerconfigjson="${docker_config}" >/dev/null
kubectl -n flux-system create secret generic conduit-runtime-trust \
  --from-file=cosign.pub="${test_root}/trusted.pub" >/dev/null

cat <<EOF | kubectl apply -f - >/dev/null
apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: conduit-runtime
  namespace: flux-system
spec:
  interval: 1m
  timeout: 1m
  insecure: true
  url: oci://${cluster_registry}/conduit/runtime
  secretRef:
    name: conduit-runtime-pull
  ref:
    digest: ${right_digest}
  verify:
    provider: cosign
    secretRef:
      name: conduit-runtime-trust
---
apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: conduit-runtime-wrong-signer
  namespace: flux-system
spec:
  interval: 1m
  timeout: 1m
  insecure: true
  url: oci://${cluster_registry}/conduit/runtime-wrong
  secretRef:
    name: conduit-runtime-pull
  ref:
    digest: ${wrong_digest}
  verify:
    provider: cosign
    secretRef:
      name: conduit-runtime-trust
---
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: conduit-runtime
  namespace: flux-system
spec:
  interval: 1m
  retryInterval: 5s
  timeout: 2m
  path: ./runtime
  prune: true
  wait: true
  serviceAccountName: conduit-runtime-reconciler
  sourceRef:
    kind: OCIRepository
    name: conduit-runtime
EOF

kubectl -n flux-system wait --for=condition=ready ocirepository/conduit-runtime --timeout=180s >/dev/null
kubectl -n flux-system wait --for=condition=ready kustomization/conduit-runtime --timeout=180s >/dev/null
for _ in $(seq 1 120); do
  if kubectl -n flux-system get ocirepository conduit-runtime-wrong-signer -o json | jq -e '
    any(.status.conditions[]?; .type == "SourceVerified" and .status == "False" and .reason == "VerificationError") and
    (.status.artifact == null)
  ' >/dev/null; then
    break
  fi
  sleep 1
done
kubectl -n flux-system get ocirepository conduit-runtime-wrong-signer -o json | jq -e '
  any(.status.conditions[]?; .type == "SourceVerified" and .status == "False" and .reason == "VerificationError") and
  (.status.artifact == null)
' >/dev/null

kubectl -n flux-system get ocirepository conduit-runtime -o json | jq -e --arg digest "${right_digest}" '
  .status.artifact.revision == $digest and
  any(.status.conditions[]?; .type == "SourceVerified" and .status == "True")
' >/dev/null
kubectl -n flux-system get kustomization conduit-runtime -o json | jq -e --arg digest "${right_digest}" '
  .status.lastAppliedRevision == $digest and
  any(.status.conditions[]?; .type == "Ready" and .status == "True")
' >/dev/null
kubectl -n conduit-acceptance get configmap conduit-runtime-source -o json | jq -e --arg commit "${commit}" '
  .data.sourceRepository == "https://github.com/jonathanagustin/conduit" and
  .data.sourceCommit == $commit and .data.transport == "signed-private-oci"
' >/dev/null

git_repository_count="$(kubectl get gitrepositories.source.toolkit.fluxcd.io -A -o json | jq '.items | length')"
[[ "${git_repository_count}" == "0" ]] || {
  echo "clean cluster contains GitRepository objects" >&2
  exit 1
}
kubectl -n flux-system get secrets -o json | jq -e '
  ([.items[] | select(.type == "kubernetes.io/ssh-auth" or .type == "kubernetes.io/basic-auth")] | length) == 0 and
  ([.items[] | (.data // {}) | keys[] | select(. == "identity" or . == "identity.pub" or . == "known_hosts" or . == "username" or . == "password")] | length) == 0
' >/dev/null

printf 'Gitless clean-cluster acceptance passed: digest=%s pull_only=1 wrong_signer_rejected=1 git_repositories=%s\n' \
  "${right_digest}" "${git_repository_count}"
