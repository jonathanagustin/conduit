#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="$root/scripts/verify-pr-fidelity.sh"
workflow="$root/.github/workflows/platform-ci.yml"

for required in \
  '  statuses: write' \
  '      - name: Publish pending Trunk status' \
  "            -f state=pending" \
  "            -f context='Trunk gate'" \
  '      - name: Publish gate summary' \
  '      - name: Publish final Trunk status' \
  '        if: ${{ always() }}'; do
  grep -Fq -- "$required" "$workflow" || {
    printf 'manual Trunk status bridge is incomplete: %s\n' "$required" >&2
    exit 1
  }
done

# The status bridge must not be able to fail the run it is reporting on.
#
# Run 49 is the case: `POST /statuses/{sha}` answered 503, the step exited 1
# under `bash -e`, and the gate stopped before running any of the thirty
# verifications it exists for. The same call in the two `always()` steps would
# have turned an all-green run red. Both are checked structurally rather than
# by grepping the file for `continue-on-error` anywhere in it, because that
# would pass while the marker sat on some unrelated step.
#
# The block also ends at a job header, not only at the next step. The workflow
# is several jobs since #213, so a status step that is the last step of its job
# would otherwise absorb every line up to the next job's first step and pass on
# a marker belonging to somebody else.
step_block() {
  awk -v name="      - name: $1" '
    $0 == name { inside = 1; next }
    inside && /^      - (name|uses):/ { exit }
    inside && /^  [^ ]/ { exit }
    inside { print }
  ' "$workflow"
}

for status_step in \
  'Publish pending Trunk status' \
  'Publish gate summary' \
  'Publish final Trunk status'; do
  block="$(step_block "$status_step")"
  [[ -n "$block" ]] || {
    printf 'the Trunk status bridge no longer has a step named %s\n' "$status_step" >&2
    exit 1
  }
  grep -Fq 'continue-on-error: true' <<<"$block" || {
    printf 'step %s can fail the run it only reports on; a GitHub outage would skip or invert the gate\n' "$status_step" >&2
    exit 1
  }
  grep -Fq 'scripts/retry-github-api.sh' <<<"$block" || {
    printf 'step %s calls the GitHub API without retrying a transient failure\n' "$status_step" >&2
    exit 1
  }
done

# The shape of the job graph, which is what the split into parallel lanes (#213)
# made possible to get wrong. Three properties, none of which the greps above can
# see, and each of which is a hole rather than a slowdown if it breaks:
#
#   - head authentication is one job and EVERY other lane needs it, so no lane
#     can verify a tree nobody proposed. A lane added without that edge would run
#     against any dispatched ref;
#   - exactly one job publishes the `Trunk gate` commit status, and it needs
#     every other job, so the status cannot be written while a lane is still
#     running or be written twice with different verdicts;
#   - that job derives its verdict from the lanes' results and not from
#     `job.status`, which is its own status and is green no matter what the lanes
#     did. Reading `job.status` there is the specific mistake that would publish
#     success over a failed lane.
#
# Since #195 the gate also starts on a `pull_request` event, and three further
# properties are checked below for that reason. Each is a hole rather than a
# slowdown if it breaks, and none is visible in a passing run:
#
#   - the gate runs on both triggers. Losing `pull_request` puts a hand
#     dispatch back in front of every pull request; losing `workflow_dispatch`
#     makes a re-run on an unchanged tree impossible, because no event fires
#     when nothing was pushed;
#   - `concurrency` collapses per pull request rather than per ref. The two
#     paths name the same pull request with different refs, so a key of
#     `github.ref` lets a dispatched re-run bill alongside the automatic run of
#     the same tree -- the stacking ADR 0006 permits this trigger only because
#     it prevents;
#   - every checkout and every status names the authenticated head sha. On a
#     pull_request event GITHUB_SHA is the ephemeral merge commit, so a lane
#     that omits the pin verifies a tree nobody proposed, and a status posted
#     against GITHUB_SHA lands on a commit that disappears -- leaving
#     scripts/verify-release-preconditions.sh, which reads the head's combined
#     status, unable to find the gate that ran.
ruby -ryaml -e '
  workflow = YAML.safe_load(File.read(ARGV[0], encoding: "UTF-8"), aliases: true)
  jobs = workflow.fetch("jobs")
  fail_with = lambda { |message| warn message; exit 1 }

  # Psych implements YAML 1.1 and decodes an unquoted GitHub `on` key as true.
  triggers = workflow["on"] || workflow[true]
  fail_with.call("the gate must run on pull_request and stay dispatchable, found: #{triggers.keys.sort.inspect}") unless
    triggers.is_a?(Hash) && triggers.keys.sort == %w[pull_request workflow_dispatch]
  # A dispatch input GitHub refuses to accept as empty would be a lie about the
  # other path, where nothing supplies it; the authenticator is what requires it
  # on the path that has no other source.
  fail_with.call("the dispatch input must not be required, since the gate also starts from a pull_request event") if
    triggers.dig("workflow_dispatch", "inputs", "pull_request", "required") == true

  concurrency = workflow.fetch("concurrency", {})
  fail_with.call("the gate must cancel superseded runs (ADR 0006 guardrail 1)") unless
    concurrency["cancel-in-progress"] == true
  fail_with.call("the concurrency group must collapse per pull request, or the two trigger paths do not supersede each other: #{concurrency["group"].inspect}") unless
    concurrency["group"].to_s.include?("github.event.pull_request.number")

  gate_sha = workflow.fetch("env", {})["GATE_SHA"].to_s
  fail_with.call("GATE_SHA must resolve to the pull-request head on the event path and the dispatched commit otherwise, found: #{gate_sha.inspect}") unless
    gate_sha.include?("github.event.pull_request.head.sha") && gate_sha.include?("github.sha")
  jobs.each do |id, job|
    checkouts = job.fetch("steps", []).select { |step| step["uses"].to_s.start_with?("actions/checkout@") }
    fail_with.call("job #{id} has no checkout to pin") if checkouts.empty?
    checkouts.each do |checkout|
      fail_with.call("job #{id} checks out #{checkout.dig("with", "ref").inspect} instead of the authenticated head; on a pull_request event that is the merge commit, which nobody proposed") unless
        checkout.dig("with", "ref").to_s.include?("env.GATE_SHA")
    end
  end

  step_named = lambda do |job, name|
    job.fetch("steps", []).find { |step| step["name"] == name }
  end
  jobs_with = lambda do |name|
    jobs.select { |_id, job| step_named.call(job, name) }.keys
  end

  authenticating = jobs.select do |_id, job|
    job.fetch("steps", []).any? { |step| step["run"].to_s.include?("verify-pr-fidelity.sh") }
  end.keys
  fail_with.call("exactly one job must authenticate the pull-request head, found: #{authenticating.inspect}") unless
    authenticating.length == 1
  preflight = authenticating.first

  # Both paths, or the one that is not wired up runs with no identity
  # established at all. The authenticator refuses an event it was not given the
  # number for, so a missing env line here fails the run rather than skipping the
  # check -- but it fails every run on that path, which is a worse way to find
  # out than this.
  authenticator = jobs.fetch(preflight).fetch("steps")
    .find { |step| step["run"].to_s.include?("scripts/authenticate-pull-request-head.sh") }
  fail_with.call("#{preflight} must authenticate the head through scripts/authenticate-pull-request-head.sh") if
    authenticator.nil?
  {
    "EVENT_PULL_REQUEST" => "github.event.pull_request.number",
    "INPUT_PULL_REQUEST" => "inputs.pull_request",
  }.each do |name, source|
    fail_with.call("head authentication must read #{name} from #{source}, or the #{name == "EVENT_PULL_REQUEST" ? "pull_request" : "workflow_dispatch"} path names no pull request") unless
      authenticator.fetch("env", {}).fetch(name, "").to_s.include?(source)
  end

  pending = jobs_with.call("Publish pending Trunk status")
  fail_with.call("exactly one job may publish the pending Trunk status, found: #{pending.inspect}") unless
    pending == [preflight]

  finalizers = jobs_with.call("Publish final Trunk status")
  fail_with.call("exactly one job may publish the final Trunk status, found: #{finalizers.inspect}") unless
    finalizers.length == 1
  finalizer = finalizers.first

  needs_of = lambda do |id|
    Array(jobs.fetch(id)["needs"])
  end

  lanes = jobs.keys - [preflight, finalizer]
  fail_with.call("the gate has no verification lanes left") if lanes.empty?
  lanes.each do |lane|
    fail_with.call("lane #{lane} does not need #{preflight}, so it would run without head authentication") unless
      needs_of.call(lane).include?(preflight)
  end

  fail_with.call("#{finalizer} must need every other job, missing: #{((jobs.keys - [finalizer]) - needs_of.call(finalizer)).inspect}") unless
    needs_of.call(finalizer).sort == (jobs.keys - [finalizer]).sort
  fail_with.call("#{finalizer} must run under always(), or a failed lane gets no verdict") unless
    jobs.fetch(finalizer)["if"].to_s.include?("always()")

  # Comments are stripped first: the step explains in prose why it cannot read
  # `job.status`, and a check that failed on the explanation would push the
  # explanation out of the file.
  verdict = step_named.call(jobs.fetch(finalizer), "Publish final Trunk status")
    .fetch("run").lines.reject { |line| line.strip.start_with?("#") }.join
  fail_with.call("the Trunk verdict must not read job.status: in the aggregating job that is its own status, which is green whatever the lanes did") if
    verdict.include?("job.status")
  fail_with.call("the Trunk verdict must be computed from the lanes results (toJSON(needs))") unless
    jobs.fetch(finalizer).fetch("steps").any? { |step| step.fetch("env", {}).values.any? { |value| value.to_s.include?("toJSON(needs)") } }

  # Where the three statuses land. GITHUB_SHA is the right commit on a dispatch
  # and the wrong one on a pull_request event, and the difference is invisible
  # in the run: the status is written, it is green, and it is attached to a
  # merge commit that is not the head anyone will merge or release from.
  ["Publish pending Trunk status", "Publish gate summary", "Publish final Trunk status"].each do |name|
    body = jobs.values.flat_map { |job| job.fetch("steps", []) }
      .find { |step| step["name"] == name }.fetch("run")
    next unless body.include?("/statuses/")

    fail_with.call("#{name} must post to the authenticated head (GATE_SHA); GITHUB_SHA is the merge commit on a pull_request event") unless
      body.include?("statuses/${GATE_SHA}")
  end
' "$workflow"

# The verdict logic itself, executed rather than read. This is the one piece of
# the bridge whose failure mode is silent: a status that says success over a
# failed lane looks exactly like a green run. So the step body is lifted out of
# the workflow as written and run against synthetic lane results, with the
# GitHub call stubbed. Every case that is not "every lane succeeded" must come
# out `failure`, including results this shell has never heard of and an empty
# set of lanes, because the mistakes that reach here are the ones nobody
# enumerated.
verdict_root="$(mktemp -d)"
trap 'rm -rf "$verdict_root"' EXIT
mkdir -p "$verdict_root/scripts"
cat >"$verdict_root/scripts/retry-github-api.sh" <<'STUB'
#!/usr/bin/env bash
# Stands in for the real GitHub call so the verdict can be exercised offline.
printf 'stubbed: %s\n' "$*"
STUB
chmod 0755 "$verdict_root/scripts/retry-github-api.sh"
ruby -ryaml -e '
  workflow = YAML.safe_load(File.read(ARGV[0], encoding: "UTF-8"), aliases: true)
  step = workflow.fetch("jobs").values
    .flat_map { |job| job.fetch("steps", []) }
    .find { |candidate| candidate["name"] == "Publish final Trunk status" }
  print step.fetch("run")
' "$workflow" >"$verdict_root/verdict.sh"

verdict_for() {
  (
    cd "$verdict_root"
    # GATE_SHA, not GITHUB_SHA: the step publishes against the authenticated
    # head, and under `set -u` an unset one would fail the lifted body for a
    # reason that has nothing to do with the verdict being measured.
    #
    # GITHUB_ENV is a real file rather than /dev/null because the verdict does
    # not stop at the status any more: it is handed to the step that concludes
    # this job, and that handoff is checked below.
    LANE_RESULTS="$1" GH_TOKEN=stub GITHUB_REPOSITORY=owner/repo GATE_SHA=deadbeef \
      GITHUB_RUN_ID=1 GITHUB_ENV="$verdict_root/github-env" bash -euo pipefail verdict.sh
  ) | sed -n 's/^Trunk gate verdict: //p'
}

expect_verdict() {
  local name="$1" expected="$2" lanes="$3" actual
  actual="$(verdict_for "$lanes")"
  [[ "$actual" == "$expected" ]] || {
    printf 'Trunk verdict for %s was %s, expected %s\n' "$name" "${actual:-<none>}" "$expected" >&2
    exit 1
  }
}

expect_verdict all_green success '{"go":{"result":"success"},"checks":{"result":"success"}}'
expect_verdict one_failed failure '{"go":{"result":"failure"},"checks":{"result":"success"}}'
expect_verdict one_cancelled failure '{"go":{"result":"cancelled"},"checks":{"result":"success"}}'
expect_verdict one_skipped failure '{"go":{"result":"skipped"},"checks":{"result":"success"}}'
expect_verdict unknown_result failure '{"go":{"result":"neutral"},"checks":{"result":"success"}}'
expect_verdict no_lanes failure '{}'
# A lane entry carrying no `result` at all: `.result` is null, which is not the
# string `success`, and a rule written as "no lane said failure" would pass it.
expect_verdict lane_without_a_result failure '{"go":{"outputs":{}},"checks":{"result":"success"}}'
# `needs` is an object keyed by job id. An array satisfies a bare
# `all(.[]; ...)` just as well, so the shape is checked rather than assumed.
expect_verdict a_lane_array failure '[{"result":"success"}]'

# The verdict does not stop at the status. It is written to GITHUB_ENV for the
# step that concludes this job, and a handoff that silently stopped happening
# would leave that step reading an unset variable -- which it treats as failure,
# so the gate would jam red rather than go wrongly green, but for a reason no
# log would explain. Both directions are checked, since only one of them is the
# one that must survive.
expect_handoff() {
  local name="$1" expected="$2" lanes="$3" recorded
  : >"$verdict_root/github-env"
  verdict_for "$lanes" >/dev/null
  recorded="$(sed -n 's/^TRUNK_STATE=//p' "$verdict_root/github-env")"
  [[ "$recorded" == "$expected" ]] || {
    printf 'the verdict step handed on TRUNK_STATE=%s for %s, expected %s\n' \
      "${recorded:-<nothing>}" "$name" "$expected" >&2
    exit 1
  }
}
expect_handoff all_green success '{"go":{"result":"success"},"checks":{"result":"success"}}'
expect_handoff one_skipped failure '{"go":{"result":"skipped"},"checks":{"result":"success"}}'

# The verdict only binds if the job concludes by it. Every other step in
# `trunk-status` is `continue-on-error` -- correctly, so a failed status publish
# cannot swallow the verdict -- and that is exactly how the job came to conclude
# `success` while publishing `failure` on pull request 245: the check run and
# the commit status, both called `Trunk gate`, disagreed. So the one step that
# exists in order to fail is held to still being able to. Without this, adding
# `continue-on-error` back would restore the split surface with every verdict
# case above still green.
ruby -ryaml -e '
  workflow = YAML.safe_load(File.read(ARGV[0], encoding: "UTF-8"), aliases: true)
  name = "Conclude the Trunk gate job with its own verdict"
  step = workflow.fetch("jobs").values
    .flat_map { |job| job.fetch("steps", []) }
    .find { |candidate| candidate["name"] == name }
  abort "the Trunk gate job has no #{name.inspect} step, so nothing makes its conclusion match the status it publishes" if step.nil?
  abort "#{name.inspect} is continue-on-error, so the job cannot fail with its verdict" if step["continue-on-error"]
  abort "#{name.inspect} never exits non-zero, so it cannot fail the job" unless step.fetch("run").include?("exit 1")
  abort "#{name.inspect} must run under always(), or a failed lane skips the step that reports it" unless step["if"].to_s.include?("always()")
' "$workflow"

valid='## Outcome

Generate the pinned public API transport interface.

## Tenant fidelity

| Claim or behavior | Class: Public contract / Tenant observation / Corroborating / Conduit extension / Unknown | Immutable source or sanitized evidence | Scope not established |
| --- | --- | --- | --- |
| Public operation shape | Public contract | docs/apollo/snapshots/example/openapi.yml at sha256:abc | Private Hub behavior |

### Deliberate divergences

None; the adapter exposes only the pinned profile.
'

printf '%s' "$valid" | "$verifier" >/dev/null

expect_failure() {
  local name="$1" body="$2"
  if printf '%s' "$body" | "$verifier" >"${TMPDIR:-/tmp}/conduit-pr-fidelity-${name}.out" 2>&1; then
    printf 'expected tenant-fidelity verification to fail: %s\n' "$name" >&2
    exit 1
  fi
}

expect_failure missing_row "${valid/Public operation shape | Public contract | docs\/apollo\/snapshots\/example\/openapi.yml at sha256:abc | Private Hub behavior/ | Public contract | | Private Hub behavior}"
expect_failure empty_divergence "${valid/None; the adapter exposes only the pinned profile./}"
expect_failure unfilled_marker "${valid/Generate the pinned public API transport interface./TODO}"

# Every upper-case marker is still refused in prose. Listed one by one because
# the alternation is the part most likely to be edited down to whichever one
# somebody was thinking about.
expect_failure marker_tbd "${valid/Generate the pinned public API transport interface./TBD}"
expect_failure marker_placeholder "${valid/Generate the pinned public API transport interface./PLACEHOLDER}"
expect_failure marker_fill_me "${valid/Generate the pinned public API transport interface./FILL ME}"
expect_failure marker_fill_hyphen "${valid/Generate the pinned public API transport interface./FILL-ME}"
# A marker with prose around it is still a marker; matching must not depend on
# the line containing nothing else.
expect_failure marker_in_a_sentence "${valid/Generate the pinned public API transport interface./The response shape is TODO once the capture lands.}"

expect_success() {
  local name="$1" body="$2"
  printf '%s' "$body" | "$verifier" >"${TMPDIR:-/tmp}/conduit-pr-fidelity-${name}.out" 2>&1 || {
    printf 'expected tenant-fidelity verification to pass: %s\n' "$name" >&2
    cat "${TMPDIR:-/tmp}/conduit-pr-fidelity-${name}.out" >&2
    exit 1
  }
}

# The two cases that rejected correct work on #137, which is why this gate is
# case-sensitive and code-aware. The first is an ordinary English word; the
# second is this list quoted while explaining the first.
expect_success prose_noun "${valid/Generate the pinned public API transport interface./Apollo publishes the stand-in placeholder value 123, so no locator format is established.}"
expect_success marker_in_code_span "${valid/Generate the pinned public API transport interface./The gate greps for \`TBD|TODO|PLACEHOLDER\` to catch unfilled markers.}"
expect_success lowercase_prose "${valid/Generate the pinned public API transport interface./There is a todo list in the design note, and a tbd column in the table.}"

fenced="${valid/Generate the pinned public API transport interface./Explaining the template:

\`\`\`markdown
## Outcome
TODO
\`\`\`

which is what an unfilled body looks like.}"
expect_success marker_in_fenced_block "$fenced"

echo 'tenant-fidelity verifier tests passed'
