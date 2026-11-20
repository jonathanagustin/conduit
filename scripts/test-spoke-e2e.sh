#!/usr/bin/env bash
set -euo pipefail

for command in docker k3d kubectl helm curl jq openssl; do
  if ! command -v "${command}" >/dev/null; then
    echo "required command is missing: ${command}" >&2
    exit 2
  fi
done

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
test_id="${PLATFORM_E2E_ID:-$RANDOM}"
cluster_name="platform-e2e-${test_id}"
registry_name="platform-e2e-registry-${test_id}"
registry_port="${PLATFORM_E2E_REGISTRY_PORT:-$((5200 + RANDOM % 500))}"
host_registry="localhost:${registry_port}"
cluster_registry="k3d-${registry_name}:5000"
test_root="$(mktemp -d)"
port_forward_pid=""

cleanup() {
  local status=$?
  if [[ "${status}" -ne 0 ]] && kubectl config get-contexts "k3d-${cluster_name}" >/dev/null 2>&1; then
    kubectl get pods,pvc -n platform-system -o wide >&2 || true
    kubectl describe pods -n platform-system >&2 || true
    kubectl logs -n platform-system -l app.kubernetes.io/component=hub --all-containers --tail=200 >&2 || true
    kubectl logs -n platform-system -l app.kubernetes.io/component=spoke --all-containers --tail=200 >&2 || true
  fi
  if [[ -n "${port_forward_pid}" ]] && kill -0 "${port_forward_pid}" 2>/dev/null; then
    kill "${port_forward_pid}" 2>/dev/null || true
    wait "${port_forward_pid}" 2>/dev/null || true
  fi
  k3d cluster delete "${cluster_name}" >/dev/null 2>&1 || true
  k3d registry delete "${registry_name}" >/dev/null 2>&1 || true
  rm -rf "${test_root}"
  return "${status}"
}
trap cleanup EXIT

k3d registry create "${registry_name}" --port "${registry_port}"
k3d cluster create "${cluster_name}" \
  --servers 1 --agents 1 \
  --registry-use "k3d-${registry_name}:5000" \
  --k3s-arg '--disable=traefik@server:*' \
  --wait
kubectl config use-context "k3d-${cluster_name}" >/dev/null
kubectl apply -f "${repo_root}/deploy/local/prerequisites.yaml" >/dev/null

version="$(tr -d '\n' < "${repo_root}/VERSION")"
commit="$(git -C "${repo_root}" rev-parse HEAD)"
created="$(git -C "${repo_root}" show -s --format=%cI HEAD)"
docker build -f "${repo_root}/Containerfile" \
  --build-arg VERSION="${version}" --build-arg COMMIT="${commit}" --build-arg BUILD_DATE="${created}" \
  -t "${host_registry}/platform-hub:${version}" "${repo_root}" >/dev/null
docker build -f "${repo_root}/Containerfile.agent" \
  --build-arg HELM_IMAGE="${PLATFORM_HELM_IMAGE:-alpine/helm:4.2.3@sha256:b97ba4f9b27fe7af16ee3d37e6815783c9d4a51289b6240a9024ec471611ae9b}" \
  --build-arg USE_UPSTREAM_HELM="${PLATFORM_USE_UPSTREAM_HELM:-false}" \
  --build-arg VERSION="${version}" --build-arg COMMIT="${commit}" --build-arg BUILD_DATE="${created}" \
  -t "${host_registry}/platform-spoke-agent:${version}" "${repo_root}" >/dev/null
docker push "${host_registry}/platform-hub:${version}" >/dev/null
docker push "${host_registry}/platform-spoke-agent:${version}" >/dev/null
hub_digest="$(docker image inspect "${host_registry}/platform-hub:${version}" --format '{{index .RepoDigests 0}}' | sed 's/.*@//')"
agent_digest="$(docker image inspect "${host_registry}/platform-spoke-agent:${version}" --format '{{index .RepoDigests 0}}' | sed 's/.*@//')"

openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 1 \
  -subj '/CN=platform-e2e-ca' \
  -addext 'basicConstraints=critical,CA:TRUE' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign' \
  -keyout "${test_root}/ca.key" -out "${test_root}/ca.crt" >/dev/null 2>&1
openssl req -new -newkey rsa:2048 -nodes -sha256 \
  -subj '/CN=platform-hub' \
  -addext 'subjectAltName=DNS:platform-hub.platform-system.svc,DNS:platform-hub.platform-system.svc.cluster.local,IP:127.0.0.1' \
  -addext 'extendedKeyUsage=serverAuth' \
  -keyout "${test_root}/hub.key" -out "${test_root}/hub.csr" >/dev/null 2>&1
openssl x509 -req -sha256 -days 1 -copy_extensions copy \
  -in "${test_root}/hub.csr" -CA "${test_root}/ca.crt" -CAkey "${test_root}/ca.key" -CAcreateserial \
  -out "${test_root}/hub.crt" >/dev/null 2>&1

issue_operator_certificate() {
  local name="$1"
  local subject="$2"
  openssl req -new -newkey rsa:2048 -nodes -sha256 \
    -subj "/CN=${name}" \
    -addext "subjectAltName=URI:spiffe://platform.delivery/operator/${subject}" \
    -addext 'extendedKeyUsage=clientAuth' \
    -keyout "${test_root}/${name}.key" -out "${test_root}/${name}.csr" >/dev/null 2>&1
  openssl x509 -req -sha256 -days 1 -copy_extensions copy \
    -in "${test_root}/${name}.csr" -CA "${test_root}/ca.crt" -CAkey "${test_root}/ca.key" -CAcreateserial \
    -out "${test_root}/${name}.crt" >/dev/null 2>&1
}
issue_operator_certificate operator local-developer
issue_operator_certificate reviewer e2e-reviewer@example.test

openssl req -new -newkey rsa:2048 -nodes -sha256 \
  -subj '/CN=spoke-local' \
  -addext 'subjectAltName=URI:spiffe://platform.delivery/environment/local/agent/spoke-local' \
  -addext 'extendedKeyUsage=clientAuth' \
  -keyout "${test_root}/agent.key" -out "${test_root}/agent.csr" >/dev/null 2>&1
openssl x509 -req -sha256 -days 1 -copy_extensions copy \
  -in "${test_root}/agent.csr" -CA "${test_root}/ca.crt" -CAkey "${test_root}/ca.key" -CAcreateserial \
  -out "${test_root}/agent.crt" >/dev/null 2>&1
openssl genpkey -algorithm ED25519 -out "${test_root}/bundle-signing-key.pem" >/dev/null 2>&1

kubectl create secret tls platform-hub-tls -n platform-system \
  --cert="${test_root}/hub.crt" --key="${test_root}/hub.key" >/dev/null
kubectl create secret tls platform-agent-tls -n platform-system \
  --cert="${test_root}/agent.crt" --key="${test_root}/agent.key" >/dev/null
kubectl create configmap platform-external-ca -n platform-system \
  --from-file=ca.pem="${test_root}/ca.crt" >/dev/null
kubectl create secret generic platform-bundle-signing -n platform-system \
  --from-file=bundle-signing-key.pem="${test_root}/bundle-signing-key.pem" >/dev/null

helm upgrade --install platform-hub "${repo_root}/deploy/helm/platform-hub" \
  --namespace platform-system \
  --values "${repo_root}/deploy/helm/platform-hub/ci/test-values.yaml" \
  --set repository.kind=file \
  --set replicaCount=1 \
  --set planning.backend=local \
  --set imagePullSecrets=null \
  --set 'runtimeSupplementalGroups[0]=0' \
  --set-string 'bootstrapAdministrators[0]=local-developer' \
  --set-string "image=${cluster_registry}/platform-hub:${version}@${hub_digest}" \
  --timeout 3m >/dev/null
kubectl wait --for=jsonpath='{.status.phase}'=Bound pvc/platform-hub-state \
  -n platform-system --timeout=60s >/dev/null
kubectl run platform-hub-storage-init -n platform-system \
  --image='busybox:1.37.0@sha256:9db7b59979c38555a39def84a31fb98b5296952f9e3afd4f6f11f05b07adfab0' \
  --restart=Never \
  --overrides='{
    "spec": {
      "automountServiceAccountToken": false,
      "securityContext": {"seccompProfile": {"type": "RuntimeDefault"}},
      "containers": [{
        "name": "platform-hub-storage-init",
        "image": "busybox:1.37.0@sha256:9db7b59979c38555a39def84a31fb98b5296952f9e3afd4f6f11f05b07adfab0",
        "command": ["sh", "-c", "chown 65532:65532 /state && chmod 0700 /state"],
        "securityContext": {
          "runAsUser": 0,
          "runAsNonRoot": false,
          "allowPrivilegeEscalation": false,
          "readOnlyRootFilesystem": true,
          "capabilities": {"drop": ["ALL"], "add": ["CHOWN", "FOWNER"]}
        },
        "volumeMounts": [{"name": "state", "mountPath": "/state"}]
      }],
      "volumes": [{"name": "state", "persistentVolumeClaim": {"claimName": "platform-hub-state"}}]
    }
  }' >/dev/null
storage_initialized=false
for _ in $(seq 1 60); do
  phase="$(kubectl get pod platform-hub-storage-init -n platform-system -o jsonpath='{.status.phase}')"
  if [[ "${phase}" == "Succeeded" ]]; then
    storage_initialized=true
    break
  fi
  if [[ "${phase}" == "Failed" ]]; then
    break
  fi
  sleep 1
done
if [[ "${storage_initialized}" != true ]]; then
  kubectl describe pod platform-hub-storage-init -n platform-system >&2
  kubectl logs platform-hub-storage-init -n platform-system >&2
  echo "bootstrap-owned k3d storage initialization failed" >&2
  exit 1
fi
kubectl rollout status deployment/platform-hub -n platform-system --timeout=180s >/dev/null
helm upgrade --install platform-spoke "${repo_root}/deploy/helm/platform-spoke" \
  --namespace platform-system \
  --values "${repo_root}/deploy/helm/platform-spoke/ci/test-values.yaml" \
  --set imagePullSecrets=null \
  --set 'runtimeSupplementalGroups[0]=0' \
  --set helmPlainHTTP=true \
  --set serviceAccount.create=false \
  --set serviceAccount.name=platform-spoke-agent \
  --set-string "image=${cluster_registry}/platform-spoke-agent:${version}@${agent_digest}" \
  --set-string environmentId=local \
  --set-string agentId=spoke-local \
  --wait --timeout 3m >/dev/null
kubectl rollout status deployment/platform-spoke -n platform-system --timeout=180s >/dev/null

helm package "${repo_root}/testdata/e2e-chart" --destination "${test_root}" >/dev/null
helm push "${test_root}/platform-e2e-0.1.0.tgz" "oci://${host_registry}" --plain-http \
  >"${test_root}/helm-push.log" 2>&1
chart_digest="$(awk '/Digest:/ {print $2}' "${test_root}/helm-push.log" | tail -1)"
if [[ ! "${chart_digest}" =~ ^sha256:[0-9a-f]{64}$ ]]; then
  sed -n '1,120p' "${test_root}/helm-push.log" >&2
  echo "Helm did not report an OCI chart digest" >&2
  exit 1
fi

kubectl port-forward -n platform-system service/platform-hub 18083:8081 \
  >"${test_root}/port-forward.log" 2>&1 &
port_forward_pid=$!
hub_url="https://127.0.0.1:18083"
public_curl=(curl --fail --silent --show-error --cacert "${test_root}/ca.crt")
operator_curl=("${public_curl[@]}" --cert "${test_root}/operator.crt" --key "${test_root}/operator.key")
hub_forward_ready=false
for _ in $(seq 1 60); do
  if "${public_curl[@]}" "${hub_url}/health/ready" >/dev/null 2>&1; then
    hub_forward_ready=true
    break
  fi
  if ! kill -0 "${port_forward_pid}" 2>/dev/null; then
    sed -n '1,120p' "${test_root}/port-forward.log" >&2
    echo "Hub port-forward exited before readiness" >&2
    exit 1
  fi
  sleep 0.5
done
if [[ "${hub_forward_ready}" != true ]]; then
  sed -n '1,120p' "${test_root}/port-forward.log" >&2
  echo "Hub port-forward did not become ready within 30 seconds" >&2
  exit 1
fi
"${public_curl[@]}" "${hub_url}/health/ready" >/dev/null

(cd "${repo_root}" && go build -trimpath -buildvcs=false -o "${test_root}/conduit" ./cmd/conduit)
conduit=("${test_root}/conduit" --server "${hub_url}" --ca-file "${test_root}/ca.crt" \
  --client-cert-file "${test_root}/operator.crt" --client-key-file "${test_root}/operator.key" --output json)
reviewer_conduit=("${test_root}/conduit" --server "${hub_url}" --ca-file "${test_root}/ca.crt" \
  --client-cert-file "${test_root}/reviewer.crt" --client-key-file "${test_root}/reviewer.key" --output json)

"${operator_curl[@]}" --request PUT \
  --header 'content-type: application/json' \
  --header 'X-Platform-Subject: local-developer' \
  --data '{"displayName":"E2E release reviewers","adminUsers":["local-developer"],"memberUsers":["e2e-reviewer@example.test"]}' \
  "${hub_url}/v1/teams/e2e-release-reviewers" >/dev/null
"${operator_curl[@]}" \
  --header 'content-type: application/json' \
  --header 'X-Platform-Subject: local-developer' \
  --data '{"resourceRid":"environment/local","team":"e2e-release-reviewers","operations":["change:propose","resource:read","resource:operate","bundle:import","fleet:bulk-change"]}' \
  "${hub_url}/v1/resource-grants" >/dev/null
"${operator_curl[@]}" --request PUT \
  --header 'content-type: application/json' \
  --header 'X-Platform-Subject: local-developer' \
  --data '{"displayName":"E2E release review","expression":"require_team(\"e2e-release-reviewers\", \"cluster-level review\")","enabled":true}' \
  "${hub_url}/v1/approval-policies/e2e-release-review" >/dev/null

image_digest="sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
image_reference="example.invalid/platform-e2e:1.0.0@${image_digest}"

"${conduit[@]}" environment create local --contact-team e2e-release-reviewers \
  >"${test_root}/conduit-environment.json"
jq --exit-status '.kind == "EnvironmentDescriptor" and .environment.id == "local"' \
  "${test_root}/conduit-environment.json" >/dev/null
"${conduit[@]}" environment manifest generate local \
  --namespace platform-system \
  --image "${cluster_registry}/platform-spoke-agent:${version}@${agent_digest}" \
  --control-plane-url https://platform-hub.platform-system.svc:8081 \
  --client-tls-secret platform-agent-tls --external-ca-configmap platform-external-ca \
  >"${test_root}/conduit-spoke-manifest.json"
jq --exit-status '.kind == "List" and (.items | length == 2)' \
  "${test_root}/conduit-spoke-manifest.json" >/dev/null

"${conduit[@]}" module create --module-directory "${repo_root}/examples/modules" \
  >"${test_root}/conduit-modules.json"
foundation_version="$(jq -er '.releases[] | select(.name == "example-foundation") | .version' \
  "${test_root}/conduit-modules.json")"
"${conduit[@]}" module install "module:example-foundation:${foundation_version}" --environment local \
  --variable k8sNamespace=example-product --variable apiServiceAccount=example-api \
  --variable databaseCidr=10.0.0.0/24 >"${test_root}/conduit-module-installation.json"
jq --exit-status --arg version "${foundation_version}" \
  '.environment == "local" and .module == "example-foundation" and .version == $version' \
  "${test_root}/conduit-module-installation.json" >/dev/null

mkdir -p "${test_root}/conduit-release"
"${conduit[@]}" product-release helm-chart init \
  --repository-url "oci://${cluster_registry}/platform-e2e:0.1.0@${chart_digest}" \
  --maven-coordinate example.test:platform-e2e:0.1.0 --name platform-e2e --version 0.1.0 \
  --values "${repo_root}/testdata/e2e-chart/values.yaml" --output-dir "${test_root}/conduit-release" \
  --artifact "${image_reference}" >"${test_root}/conduit-release-init.json"
"${conduit[@]}" product-release create --manifest "${test_root}/conduit-release/manifest.yml" \
  >"${test_root}/conduit-release-create.json"
jq --exit-status '.created == true and .mavenCoordinate == "example.test:platform-e2e:0.1.0"' \
  "${test_root}/conduit-release-create.json" >/dev/null

latest_plan_id=""
latest_release_digest=""
latest_config_digest=""

apply_generation() {
  local generation="$1"
  local value="$2"
  jq --null-input \
    --arg generation "${generation}" \
    --arg chartRepository "${cluster_registry}/platform-e2e" \
    --arg chartDigest "${chart_digest}" \
    --arg imageDigest "${image_digest}" \
    --arg imageReference "${image_reference}" \
    --arg value "${value}" \
    '{
      apiVersion: "platform.delivery/v1alpha1",
      kind: "EnvironmentDeployment",
      environment: {
        id: "local",
        generation: $generation,
        helmExecution: {imagePrePull: {enabled: false}}
      },
      releases: [{
        product: "example.test:platform-e2e",
        version: "0.1.0",
        chart: {repository: $chartRepository, tag: "0.1.0", digest: $chartDigest},
        images: [{repository: "example.invalid/platform-e2e", tag: "1.0.0", digest: $imageDigest}]
      }],
      entities: [{
        name: "platform-e2e",
        product: "example.test:platform-e2e",
        version: "0.1.0",
        namespace: "example-product",
        helmValues: {image: $imageReference, value: $value}
      }]
    }' >"${test_root}/desired-${generation}.json"
  "${conduit[@]}" entity install --environment local --product-id example.test:platform-e2e \
    --file "${test_root}/desired-${generation}.json" >"${test_root}/plan-${generation}.json"
  local plan_id
  plan_id="$(jq --raw-output .id "${test_root}/plan-${generation}.json")"
  latest_plan_id="${plan_id}"
  latest_release_digest="$(jq --raw-output '.plan.steps[0].releaseDigest' "${test_root}/plan-${generation}.json")"
  latest_config_digest="$(jq --raw-output '.plan.steps[0].configDigest' "${test_root}/plan-${generation}.json")"
  "${reviewer_conduit[@]}" plan approve "${plan_id}" \
    --rationale "cluster-level GitOps acceptance" >/dev/null
  for _ in $(seq 1 90); do
    status="$("${operator_curl[@]}" "${hub_url}/v1/plans/${plan_id}" | jq --raw-output .status)"
    if [[ "${status}" == "SUCCEEDED" ]]; then
      break
    fi
    if [[ "${status}" == "FAILED" || "${status}" == "CANCELLED" ]]; then
      "${operator_curl[@]}" "${hub_url}/v1/plans/${plan_id}" | jq . >&2
      kubectl logs -n platform-system deployment/platform-spoke --tail=200 >&2
      exit 1
    fi
    sleep 2
  done
  [[ "${status}" == "SUCCEEDED" ]]
  [[ "$(kubectl get configmap platform-e2e -n example-product -o jsonpath='{.data.value}')" == "${value}" ]]

  # The Apollo workload metadata is stamped by a Helm post-renderer, whose flag
  # is spelled differently on Helm 3 and Helm 4. Asserting it here is what proves
  # the post-renderer actually ran against the real Helm this lane installed --
  # a unit test can only check the flags Platform builds, not what Helm does
  # with them.
  kubectl get configmap platform-e2e -n example-product -o json \
    >"${test_root}/stamped-configmap.json"
  jq --exit-status '
    .metadata.annotations["apollo.palantir.com/metadata.environment.id"] == "local" and
    .metadata.annotations["apollo.palantir.com/metadata.entity.id"] == "aeid:local:helm-chart:platform-e2e" and
    .metadata.annotations["apollo.palantir.com/metadata.product.group"] == "example.test" and
    .metadata.annotations["apollo.palantir.com/metadata.product.name"] == "platform-e2e" and
    .metadata.annotations["apollo.palantir.com/metadata.product.version"] == "0.1.0"
  ' "${test_root}/stamped-configmap.json" >/dev/null
}

apply_generation "0123456789abcdef" "installed-from-git"
apply_generation "1123456789abcdef" "upgraded-from-git"

reported_state_ready=false
for _ in $(seq 1 60); do
  "${operator_curl[@]}" "${hub_url}/v1/environments/local/reported-state" \
    >"${test_root}/reported-state.json"
  if jq --exit-status \
    --arg plan_id "${latest_plan_id}" \
    --arg release_digest "${latest_release_digest}" \
    --arg config_digest "${latest_config_digest}" '
      .environment == "local" and
      .agentId == "spoke-local" and
      .planId == $plan_id and
      (.observedAt | type == "string" and length > 0) and
      (.revision | type == "number" and . > 0) and
      (.entities | length == 1) and
      .entities[0].installationId == "platform-e2e" and
      .entities[0].product == "example.test:platform-e2e" and
      .entities[0].version == "0.1.0" and
      .entities[0].namespace == "example-product" and
      .entities[0].releaseDigest == $release_digest and
      .entities[0].configDigest == $config_digest and
      .entities[0].health == "UNKNOWN" and
      .entities[0].healthDetails == {quorum: "ALL", nodes: []} and
      .entities[0].rolloutStatus == "UP_TO_DATE" and
      .entities[0].configStatus == "APPLIED" and
      .entities[0].lifecycle == "INSTALLED" and
      (.entities[0].observedAt | type == "string" and length > 0)
    ' "${test_root}/reported-state.json" >/dev/null; then
    reported_state_ready=true
    break
  fi
  sleep 0.5
done
if [[ "${reported_state_ready}" != true ]]; then
  jq . "${test_root}/reported-state.json" >&2
  kubectl logs -n platform-system deployment/platform-spoke --tail=200 >&2
  echo "Hub did not expose the exact report for terminal Plan ${latest_plan_id}" >&2
  exit 1
fi

echo "Spoke acceptance passed: OCI pull, approved Plan, Helm install/upgrade, and exact reported state"
