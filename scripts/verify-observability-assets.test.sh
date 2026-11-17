#!/usr/bin/env bash
# Proves verify-observability-assets.rb refuses each thing it claims to refuse.
#
# The tree it guards is correct, so a check that had stopped reading its inputs
# would print the same PASS as one that read them. This copies the inputs to a
# fixture, confirms the gate passes there, then plants one violation at a time
# and requires each to be refused for the stated reason. Every planted file is
# still valid YAML, JSON or Markdown: the gate has to refuse the meaning, not
# get a green score from a parse error.
set -euo pipefail
root="$(git rev-parse --show-toplevel)"
gate="${root}/scripts/verify-observability-assets.rb"
fixture="$(mktemp -d)"
trap 'rm -rf -- "${fixture}"' EXIT

inputs=(
  deploy/observability/exported-metrics.txt
  deploy/observability/prometheus-rules.yaml
  deploy/observability/dashboard.json
  deploy/helm/platform-hub/files/prometheus-rules.yaml
  deploy/helm/platform-hub/files/dashboard.json
  deploy/zarf/manifests/observability.yaml
  docs/operations/runbooks.md
  internal/observability/domain/metrics.go
)
reset() {
  for path in "${inputs[@]}"; do
    mkdir -p "${fixture}/$(dirname "${path}")"
    cp "${root}/${path}" "${fixture}/${path}"
  done
}
run() { CONDUIT_OBSERVABILITY_ROOT="${fixture}" ruby "${gate}" "$@"; }

reset
run >/dev/null || { echo "the gate fails on an unmodified copy of the tree" >&2; exit 1; }

rules="${fixture}/deploy/observability/prometheus-rules.yaml"
dashboard="${fixture}/deploy/observability/dashboard.json"
runbooks="${fixture}/docs/operations/runbooks.md"

# refuse <name> <expected message fragment> <command...>: plant, regenerate the
# copies so that only the planted violation is wrong, and require the refusal.
refuse() {
  local name="$1" expected="$2"
  shift 2
  reset
  "$@"
  local log="${fixture}/${name}.log"
  if [[ "${name}" != copy-* ]]; then
    run --write >/dev/null 2>&1 || true
  fi
  if run >"${log}" 2>&1; then
    echo "the gate accepted: ${name}" >&2
    exit 1
  fi
  if ! grep -qF -- "${expected}" "${log}"; then
    echo "the gate refused ${name} for the wrong reason; expected '${expected}', got:" >&2
    cat "${log}" >&2
    exit 1
  fi
  echo "refused ${name}"
}
substitute() { ruby -e 'path, from, to = ARGV; text = File.read(path); abort "fixture anchor missing: #{from}" unless text.include?(from); File.write(path, text.sub(from, to))' "$@"; }

refuse rule-unknown-metric 'conduit_reconciliation_backlog is not a series the Hub exports' \
  substitute "${rules}" 'max by (queue) (conduit_reconciliation_pending) > 0' 'max by (queue) (conduit_reconciliation_backlog) > 0'
refuse rule-foreign-metric 'http_requests_total is not a series the Hub exports' \
  substitute "${rules}" 'sum(rate(conduit_http_requests_total[5m])) > 0.1' 'sum(rate(http_requests_total[5m])) > 0.1'
refuse rule-bare-histogram 'a histogram; use conduit_http_request_duration_seconds_bucket' \
  substitute "${rules}" 'absent(conduit_build_info)' 'absent(conduit_http_request_duration_seconds)'
refuse rule-tenant-label 'label environment is not on the metrics allowlist' \
  substitute "${rules}" 'max by (queue) (conduit_reconciliation_oldest_pending_seconds) > 900' 'max by (environment) (conduit_reconciliation_oldest_pending_seconds) > 900'
refuse rule-tenant-matcher 'label product is not on the metrics allowlist' \
  substitute "${rules}" '{class="agent",outcome="limited"}' '{class="agent",product="catalog"}'
refuse rule-unknown-function 'ratio() is not a PromQL function' \
  substitute "${rules}" 'absent(conduit_build_info)' 'ratio(conduit_build_info)'
refuse rule-no-series 'ConduitMetricsAbsent: names no series' \
  substitute "${rules}" 'absent(conduit_build_info)' 'vector(1)'
refuse rule-no-severity 'severity must be warning or critical' \
  substitute "${rules}" 'severity: critical' 'severity: page'
refuse rule-wrong-runbook 'runbook_url must be' \
  substitute "${rules}" 'runbooks.md#conduithubwedged' 'runbooks.md#wedged'
refuse rule-required-removed 'required alert ConduitMetricsAbsent is missing' \
  substitute "${rules}" 'alert: ConduitMetricsAbsent' 'alert: ConduitScrapeGone'
refuse runbook-section-missing "has no '## ConduitHubWedged' section" \
  substitute "${runbooks}" '## ConduitHubWedged' '## A wedged Hub'
refuse runbook-part-missing "ConduitAgentRateLimited: no '### What not to do' part" \
  ruby -e 'path = ARGV[0]; text = File.read(path); head, tail = text.split("## ConduitAgentRateLimited", 2); abort "fixture anchor missing" unless tail; File.write(path, head + "## ConduitAgentRateLimited" + tail.sub("### What not to do", "### Warnings"))' "${runbooks}"
refuse runbook-orphan 'section ConduitDiskFull describes an alert' \
  ruby -e 'File.write(ARGV[0], File.read(ARGV[0]) + "\n## ConduitDiskFull\n\n### Meaning\n\nNo such alert.\n")' "${runbooks}"
refuse dashboard-unknown-metric 'conduit_http_request_total is not a series the Hub exports' \
  substitute "${dashboard}" 'sum by (code_class) (rate(conduit_http_requests_total[5m]))' 'sum by (code_class) (rate(conduit_http_request_total[5m]))'
refuse dashboard-tenant-label 'label installation is not on the metrics allowlist' \
  substitute "${dashboard}" 'sum by (route) (rate(conduit_http_requests_total[5m]))' 'sum by (installation) (rate(conduit_http_requests_total[5m]))'
refuse dashboard-empty-panel 'no query' \
  ruby -rjson -e 'path = ARGV[0]; board = JSON.parse(File.read(path)); board["panels"].find { |panel| panel["type"] == "timeseries" }["targets"] = []; File.write(path, JSON.pretty_generate(board) + "\n")' "${dashboard}"
refuse manifest-metric-removed 'conduit_http_streams_open is not a series the Hub exports' \
  substitute "${fixture}/deploy/observability/exported-metrics.txt" 'conduit_http_streams_open gauge' '# removed'
refuse copy-chart-rules-drift 'deploy/helm/platform-hub/files/prometheus-rules.yaml is not what its source generates' \
  substitute "${fixture}/deploy/helm/platform-hub/files/prometheus-rules.yaml" '> 300' '> 3000'
refuse copy-chart-dashboard-drift 'deploy/helm/platform-hub/files/dashboard.json is not what its source generates' \
  substitute "${fixture}/deploy/helm/platform-hub/files/dashboard.json" 'Conduit Hub' 'Another Hub'
refuse copy-zarf-drift 'deploy/zarf/manifests/observability.yaml is not what its source generates' \
  substitute "${fixture}/deploy/zarf/manifests/observability.yaml" 'severity: critical' 'severity: warning'

# Negative control: a threshold is not a name, and changing one must pass.
reset
substitute "${rules}" '> 900' '> 1200'
run --write >/dev/null
run >/dev/null || { echo "the gate refused a changed threshold, which it does not claim to check" >&2; exit 1; }
echo "accepted a changed threshold (the gate checks names, not values)"
echo "PASS observability asset gate self-test"
