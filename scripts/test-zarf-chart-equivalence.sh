#!/usr/bin/env bash
# Each assertion of verify-zarf-chart-equivalence.rb must fail when the thing it
# guards is broken. Every case below mutates a copy of the package, the charts
# or the reviewed difference list, and requires the gate to fail for the stated
# reason. A case that still passes is an assertion that checks nothing.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
unset ZARF_BIN PRINT_DIFFERENCES

fresh() {
  rm -rf "$work/tree"
  mkdir -p "$work/tree/deploy" "$work/tree/scripts/lib"
  cp -R "$root/deploy/zarf" "$root/deploy/helm" "$work/tree/deploy/"
  cp "$root/scripts/verify-zarf-chart-equivalence.rb" "$work/tree/scripts/"
  cp "$root/scripts/lib/safe_yaml.rb" "$work/tree/scripts/lib/"
}

fresh
ruby "$work/tree/scripts/verify-zarf-chart-equivalence.rb" > "$work/baseline.log" 2>&1 || {
  cat "$work/baseline.log" >&2
  echo 'unmutated package does not pass the equivalence gate' >&2
  exit 1
}

cases=0
# must_reject <expected message> <description>
must_reject() {
  if ruby "$work/tree/scripts/verify-zarf-chart-equivalence.rb" > "$work/case.log" 2>&1; then
    echo "gate accepted a mutation it must reject: $2" >&2
    exit 1
  fi
  if ! grep -qF -- "$1" "$work/case.log"; then
    cat "$work/case.log" >&2
    echo "gate failed for the wrong reason; expected: $1" >&2
    exit 1
  fi
  cases=$((cases + 1))
}

# rejects <expected message> <file relative to the copy> <sed expression>
rejects() {
  fresh
  before=$(sha256sum < "$work/tree/$2")
  sed -i -e "$3" "$work/tree/$2"
  if [[ "$before" == "$(sha256sum < "$work/tree/$2")" ]]; then
    echo "mutation changed nothing: $2: $3" >&2
    exit 1
  fi
  must_reject "$1" "$2: $3"
}

runtime=deploy/zarf/manifests/runtime.yaml
definition=deploy/zarf/zarf.yaml
contract=deploy/zarf/chart-equivalence.yaml

# Drift in either definition is a difference nobody reviewed.
rejects 'Deployment/platform-delivery spec.template.spec.containers[delivery-control-plane].resources.limits.cpu differs is not an allowed difference' \
  "$runtime" '0,/limits: { cpu: 500m/s//limits: { cpu: 600m/'
rejects 'Deployment/platform-spoke-agent spec.template.spec.containers[spoke-agent].env[LEASE_DURATION].value differs is not an allowed difference' \
  "$runtime" 's/name: LEASE_DURATION, value: 2m/name: LEASE_DURATION, value: 3m/'
rejects 'postgres: Deployment/platform-delivery spec.replicas differs is not an allowed difference' \
  deploy/helm/platform-hub/values.yaml 's/^replicaCount: 2$/replicaCount: 3/'
rejects 'spec.template.spec.containers[delivery-control-plane].securityContext.readOnlyRootFilesystem differs is not an allowed difference' \
  deploy/helm/platform-hub/templates/deployment.yaml '0,/readOnlyRootFilesystem: true/s//readOnlyRootFilesystem: false/'
rejects 'Deployment/platform-packaging-worker spec.template.spec.containers[packaging-worker].args differs is not an allowed difference' \
  deploy/zarf/manifests/packaging-worker.yaml 's/args: \["packaging-worker"\]/args: ["worker"]/'

# The derived profile settings, executed from zarf.yaml as Zarf would.
rejects 'postgres profile must run two Hub replicas when planning is shared' "$definition" 's/postgres:shared) echo 2 ;;/postgres:shared) echo 3 ;;/'
# A second replica without a planning frontend is the defect the chart refuses
# to render and the Hub refuses to start with.
rejects 'postgres-single: more than one Hub replica without a shared planning frontend' "$definition" 's/postgres:shared) echo 2 ;; postgres:|file:|file:shared) echo 1 ;;/postgres:shared|postgres:) echo 2 ;; file:|file:shared) echo 1 ;;/'
rejects 'file: more than one Hub replica without a shared planning frontend' "$definition" 's/postgres:|file:|file:shared) echo 1 ;;/postgres:|file:shared) echo 1 ;; file:) echo 2 ;;/'
rejects 'postgres: more than one Hub replica without a shared planning frontend' "$runtime" 's/name: CONDUIT_TEMPORAL_SHARED, value: "true"/name: CONDUIT_TEMPORAL_SHARED, value: "false"/'
rejects 'postgres: Hub is told HUB_REPLICA_COUNT=1 but runs 2 replicas' "$runtime" 's/name: HUB_REPLICA_COUNT, value: "###ZARF_VAR_HUB_REPLICAS###"/name: HUB_REPLICA_COUNT, value: "1"/'
rejects 'profile postgres-single must leave empty PLANNING_TEMPORAL_ADDRESS' "$contract" '/^  postgres-single:$/,/^    hubValues:$/s/^      AGENT_ID: example-agent$/      AGENT_ID: example-agent\n      PLANNING_TEMPORAL_ADDRESS: "temporal:7233"/'
rejects 'profile postgres must set PLANNING_TEMPORAL_ADDRESS' "$contract" '/^      PLANNING_TEMPORAL_ADDRESS: planning-frontend/d'
# The planning and model wiring is compared like for like in the postgres
# profile, so drift there is a difference, not an allowed one.
rejects 'postgres: Deployment/platform-delivery spec.template.spec.containers[delivery-control-plane].env[CONDUIT_TEMPORAL_TASK_QUEUE].value differs is not an allowed difference' \
  "$definition" 's/^    default: planning-sessions$/    default: planning/'
rejects 'postgres: Deployment/platform-delivery spec.template.spec.containers[delivery-control-plane].env[CONDUIT_MODEL_API_KEY_FILE].value differs is not an allowed difference' \
  deploy/helm/platform-hub/templates/deployment.yaml 's|value: /run/conduit-model/api-key }|value: /run/model/api-key }|'
rejects 'postgres: Deployment/platform-delivery spec.template.spec.containers[delivery-control-plane].env[CONDUIT_TEMPORAL_ADDRESS].value differs is not an allowed difference' \
  deploy/helm/platform-hub/values.yaml 's/address: planning-frontend.platform-system.svc:7233/address: planning.platform-system.svc:7233/'
# The package does not offer app-generation compute; turning it on in the
# manifest without the chart render following is a difference.
rejects 'Deployment/platform-delivery spec.template.spec.containers[delivery-control-plane].env[CONDUIT_IMAGE_BUILDER] zarf-only is not an allowed difference' \
  "$runtime" 's/^            - { name: CONDUIT_TEMPORAL_SHARED, value: "true" }$/&\n            - { name: CONDUIT_IMAGE_BUILDER, value: docker }/'
rejects 'postgres profile must roll without losing a serving Hub' "$definition" 's/maxUnavailable: 0, maxSurge: 1/maxUnavailable: 1, maxSurge: 0/'
rejects 'file profile must stop the old Hub before starting the new one' "$definition" 's/file) echo "{type: Recreate}"/file) echo "{type: RollingUpdate}"/'
rejects 'postgres profile must not share a ReadWriteOnce claim between replicas' \
  "$definition" 's/postgres) echo "emptyDir: {}"/postgres) echo "persistentVolumeClaim: {claimName: platform-delivery-state}"/'
rejects 'file profile must keep Hub state on the retained claim' \
  "$definition" 's/file) echo "persistentVolumeClaim: {claimName: platform-delivery-state}"/file) echo "emptyDir: {}"/'
rejects 'runtime component must derive exactly HUB_REPLICAS, HUB_STRATEGY and HUB_STATE_VOLUME' "$definition" 's/- name: HUB_STRATEGY$/- name: HUB_ROLLOUT/'
rejects 'derived Hub settings must not also be operator-settable variables' "$definition" 's/^  - name: AGENT_TLS_SECRET$/  - name: HUB_REPLICAS/'
rejects 'REPOSITORY_KIND must be the profile switch' "$definition" 's/pattern: "^(file|postgres)\$"/pattern: "^.*\$"/'
rejects 'manifest uses an unresolved Zarf placeholder: ###ZARF_VAR_HUB_REPLICA_COUNT###' "$runtime" 's/ZARF_VAR_HUB_REPLICAS/ZARF_VAR_HUB_REPLICA_COUNT/'
rejects 'Hub PodDisruptionBudget must select the Hub Pods and keep all but one' "$runtime" 's/^  maxUnavailable: 1$/  maxUnavailable: 2/'

# The packaging worker beside a file-backed Hub.
rejects 'packaging-worker guard accepted a file-backed Hub' "$definition" 's/test "\${ZARF_VAR_REPOSITORY_KIND}" = postgres/test -n "${ZARF_VAR_REPOSITORY_KIND}"/'
rejects 'packaging-worker guard refused the postgres profile' "$definition" 's/test "\${ZARF_VAR_REPOSITORY_KIND}" = postgres/test "${ZARF_VAR_REPOSITORY_KIND}" = neither/'
rejects 'packaging-worker guard must say why it refused' "$definition" 's/echo "packaging-worker requires REPOSITORY_KIND=postgres/echo "refused/'
rejects 'packaging-worker must carry a deploy-time profile guard' "$definition" '/Refuse the packaging worker beside a file-backed Hub/,+1d'
rejects 'profile file selects the packaging worker beside a file-backed Hub' "$contract" '0,/^    repositoryKind: file$/s//    repositoryKind: file\n    packagingWorker: true/'

# Packaging's Hub client and worker use one explicitly enabled configuration.
rejects 'packaging Temporal must default to disabled with its established namespace and queue' \
  "$definition" '/^  - name: PACKAGING_TEMPORAL_ADDRESS$/,/^  - name: PACKAGING_TEMPORAL_NAMESPACE$/s/default: ""/default: "127.0.0.1:7233"/'
rejects 'file: Hub must forward the shared PACKAGING_TEMPORAL_ADDRESS' "$runtime" '/name: PACKAGING_TEMPORAL_ADDRESS,/d'
rejects 'postgres: Hub and worker must share PACKAGING_TEMPORAL_TASK_QUEUE' \
  deploy/zarf/manifests/packaging-worker.yaml 's/ZARF_VAR_PACKAGING_TEMPORAL_TASK_QUEUE/ZARF_VAR_PACKAGING_TEMPORAL_NAMESPACE/'
rejects 'runtime packaging guard must refuse only a configured file-backed Hub' "$definition" 's/file:) ;;/file:*) ;;/'
rejects 'packaging-worker address guard must refuse an empty frontend' \
  "$definition" 's/test -n "${ZARF_VAR_PACKAGING_TEMPORAL_ADDRESS}"/test -z "${ZARF_VAR_PACKAGING_TEMPORAL_ADDRESS}"/'
rejects 'PACKAGING_TEMPORAL_ADDRESS accepts a credential, URI or unsafe manifest value' \
  "$definition" '/^  - name: PACKAGING_TEMPORAL_ADDRESS$/,/^  - name: PACKAGING_TEMPORAL_NAMESPACE$/s/pattern: .*/pattern: "^.*$"/'
rejects 'PACKAGING_TEMPORAL_ADDRESS must preserve bracketed IPv6 endpoints and interface zones' \
  "$definition" 's/%\[A-Za-z0-9_.-\]/%[0-9]/'
rejects 'PACKAGING_TEMPORAL_ADDRESS must refuse ports outside 1 through 65535' \
  "$definition" 's/6553\[0-5\]/6553[0-9]/'

# The reviewed list itself.
rejects 'Service/platform-delivery spec.type chart-only is not an allowed difference' "$contract" '/^  - object: Service\/platform-delivery$/{N;/spec.type/{N;N;N;d}}'
rejects 'allowed difference no longer occurs and must be removed: Service/platform-delivery spec.clusterIP chart-only' \
  "$contract" '$a\  - object: Service/platform-delivery\n    path: "spec.clusterIP"\n    side: chart-only\n    profiles: [file]\n    reason: "Not a difference the charts and the package actually have."'
rejects 'allowlist entry needs object, path, side, profiles and a reason' "$contract" '/^allow:$/,/^    reason: /s/^    reason: .*/    reason: "because"/'
rejects 'chartObjectNames entry needs chart, zarf and a reason' "$contract" '/^chartObjectNames:$/,/^    reason: /s/^    reason: .*/    reason: "because"/'
rejects 'chartOnlyLabels needs keys and a reason' "$contract" '/^chartOnlyLabels:$/,/^  reason: /s/^  reason: .*/  reason: "because"/'
rejects 'chartObjectNames lists objects the charts do not render: Deployment/platform-delivery-worker' "$contract" 's|chart: Deployment/platform-delivery-packaging|chart: Deployment/platform-delivery-worker|'
rejects 'allowlist entries must be unique' "$contract" '$a\  - object: Service/platform-delivery\n    path: "spec.type"\n    side: chart-only\n    profiles: [file]\n    reason: "ClusterIP is the Kubernetes default the chart writes out."'
rejects 'chartOnlyLabels must not hide the label the package selects Pods by' "$contract" 's|^    - helm.sh/chart$|    - app.kubernetes.io/name|'
rejects 'chartOnlyLabels lists labels the charts no longer render: example.test/unused' "$contract" 's|^    - helm.sh/chart$|    - helm.sh/chart\n    - example.test/unused|'
rejects 'chart equivalence must cover exactly the file, postgres and postgres-single profiles' "$contract" 's/^  postgres:$/  production:/'
rejects 'profile file must select itself with REPOSITORY_KIND' "$contract" 's/^    repositoryKind: file$/    repositoryKind: postgres/'

# A label present only in nested Pod metadata is sufficient. The same key in
# annotations, or a label with only a matching prefix, cannot rescue a stale
# allowlist entry. These fixtures render the actual charts for every profile.
label_fixture() {
  fresh
  ruby -ryaml - "$work/tree/$contract" "$1" "$2" <<'RUBY'
path, field, rendered_key = ARGV
contract = YAML.safe_load_file(path, aliases: true)
contract.fetch("chartOnlyLabels").fetch("keys") << "example.test/structural-only"
contract.fetch("profiles").each_value do |profile|
  profile.fetch("hubValues")[field] = {rendered_key => "fixture"}
end
File.write(path, YAML.dump(contract))
RUBY
}

label_fixture podLabels example.test/structural-only
ruby "$work/tree/scripts/verify-zarf-chart-equivalence.rb" > "$work/nested-label.log" 2>&1 || {
  cat "$work/nested-label.log" >&2
  echo 'gate rejected a label rendered only in nested Pod metadata' >&2
  exit 1
}

label_fixture podAnnotations example.test/structural-only
must_reject 'chartOnlyLabels lists labels the charts no longer render: example.test/structural-only' 'allowlisted key exists only as an annotation'
label_fixture podLabels example.test/structural-only-suffix
must_reject 'chartOnlyLabels lists labels the charts no longer render: example.test/structural-only' 'only a different label with a matching prefix exists'

echo "Zarf chart-equivalence gate rejects ${cases} mutations of the package, the charts and the reviewed difference list, and accepts nested-only labels"
