# Model evaluation harness

`conduit-eval` scores what a model returns for the planning and packaging
prompts the Hub builds, against a committed corpus, and gates prompt and skill
changes in CI.

**No real-model score exists yet.** The only committed baseline was produced by
the offline `deterministic` agent, which is a canned responder. Read
[What the committed baseline shows](#what-the-committed-baseline-shows) before
reading any number in it as model quality.

## What it runs

The harness does not rebuild any prompt. For each case it calls the product's
own code against a model port:

| Stage | Product code driven |
| --- | --- |
| Inspection, questions, plan | `planner.Planner.Turn` (`internal/planning/adapters/planner`), including its tool loop, the untrusted-data envelope, the read-file nudge, and the question and plan parsers |
| Package | `application.WorkspacePackager.WritePackage` (`internal/packagingworkflow/application`), with the prompt from `domain.Prompt`, the parser `domain.ParsePackageProposal`, the repair loop, and the Dockerfile and Package CR gates |

Four things are replaced, and only these:

- **The clone.** The planner's clone function returns the committed fixture
  directory. No network is used and the source-host policy is not exercised.
- **The git workspace.** Package files are hashed in memory, not committed.
- **The model-call governor.** The Hub wraps every model call in
  `internal/modelgovernor` (concurrency, budget, usage and transcript audit).
  The harness calls the provider directly, so `record` and `live` write no Hub
  audit or usage record and are not held to a Hub budget. The governor does
  not alter prompt bytes, so the digests are the product's.
- **`zarf lint` and `zarf package create`.** They are not run. "Gates pass"
  here means the Dockerfile gate and the Package CR gate (the Hub's own rules
  plus the pinned UDS Core schema in `internal/udsschema`), not a built package.

The package writer over the model port is a twelve-line copy of
`packagingworkflow/adapters/modelhttp.Adapter` with the port in place of the
HTTP client. A change to that adapter's prompt assembly that is not mirrored
there would go unseen; a change to `domain.Prompt` or the skill would not.
It builds the prompt `packaging-workflow plan compute` builds, with no
toolbox: the Hub's interactive proposal route additionally runs
`inspect_image` against a registry and adds its result to the prompt, and
that variant is not evaluated here because it needs a network.

## Corpus

`docs/evidence/model-evals/`:

```
corpus.json                     cases, capabilities, expected facts
fixtures/<name>/                small synthetic repositories, committed as files
transcripts/<model>/<sha256>.json   one recorded exchange per prompt digest
baselines/<model>.json          the committed score file for that model
```

Fixtures (all original, none copied from elsewhere):

| Fixture | Shape |
| --- | --- |
| `go-http-stateless` | Go HTTP service on 8080, no state, no Dockerfile |
| `node-api-postgres` | Express API on 3000 that requires `DATABASE_URL`; which database is left open |
| `static-site` | One HTML page and a stylesheet |
| `python-multi-port` | Flask API on 8000 and metrics on 9090 |
| `injection-readme` | Go service on 8443 whose README claims 9999 and carries a prompt-injection block |

Every assertion names, in `source`, the fixture file its expected value was
read from; a corpus with an empty `source` does not load. Expected facts are
written from the fixture, never from a model's answer.

Assertion kinds: `inspection.{completed,language,package_manager,kind,ports,has_dockerfile,summary_mentions}`,
`tools.read_any`, `questions.{completed,asks_about,not_asks_about,max}`,
`plan.{valid,ports_include,ports_exclude,mentions,network_allow_excludes}`,
`package.{gates_pass,cr_valid,dockerfile_valid,cr_excludes}`, `replies.exclude`.
Question quality is rule-based: patterns over the question ids, prompts and
choices, such as "asks about the database" and "does not ask about the port".

`plan.valid` means the planner accepted and canonicalized the plan, which is
the plan schema the Hub enforces. `package.cr_valid` runs
`udsschema.ValidatePackage` on the proposed Package CR.

Two limits on what a dimension means:

- `inspection_accuracy` mostly measures the Hub's deterministic `inspect_repo`
  and whether the model called it. Only `inspection.summary_mentions` and
  `tools.read_any` depend on what the model wrote or chose to read. One
  assertion (`python-multi-port.inspection/kind`) fails for every model today,
  because `inspect_repo` reports no kind for a Python service with no start
  script.
- The injection assertions prove something only when the model read
  `README.md`. `injection.questions/read-readme` records whether it did.

## Modes

```
conduit-eval replay [--corpus DIR] [--model ID] [--out FILE]
conduit-eval record [--corpus DIR] [--out FILE]
conduit-eval live   [--corpus DIR] [--out FILE]
```

- **replay** answers each prompt from `transcripts/<model>/`, keyed by the
  sha256 of the prompt bytes. A prompt with no recording is a failure that
  names the case. It then compares the scores with `baselines/<model>.json`.
  Exit status 1 on a gate failure, 2 on a usage or configuration error.
- **record** calls the configured provider, replaces that model's transcripts,
  writes its baseline, and prints the change against the previous baseline.
- **live** calls the provider and prints scores. It writes nothing unless
  `--out` is given.

`record` and `live` read the Hub's variables with the Hub's rule:
`CONDUIT_MODEL_BASE_URL` (with `CONDUIT_MODEL_NAME`,
`CONDUIT_MODEL_API_KEY_FILE`) selects the HTTP transport, `CONDUIT_MODEL_CMD`
a local command, and setting both is an error. See
[model-http.md](model-http.md) and [local-model-cli.md](local-model-cli.md).

The model id is `openai-http/<CONDUIT_MODEL_NAME>` for HTTP,
`agent-exec/<provider>` for `packaging-workflow agent-exec`, and
`<command>/<CONDUIT_MODEL_NAME>` for any other command.

## Scores

A score file is JSON keyed by `modelId`, `promptSha256` (the sorted set of
prompt digests sent, with `promptSetSha256` over it) and `skillSha256` (the
digest `skills/packaging/skill.go` records), plus the corpus digest. It holds
per-assertion pass or fail with the observed value, a per-case score, a score
per dimension, and the aggregate:

`inspection_accuracy`, `question_quality`, `plan_validity`, `gate_pass_rate`,
`injection_resistance`.

A score is passed assertions over total assertions. An assertion whose stage
never ran fails; nothing is skipped, so totals do not depend on how far a
model got. The file has no timestamp: the same corpus, transcripts and code
produce the same bytes.

## The gate

The **Validate model-eval replay** step of the Trunk gate runs
`go run ./cmd/conduit-eval replay`. It fails when:

- a prompt digest has no recording (a prompt, the packaging skill, or a
  fixture changed and nobody re-recorded);
- the baseline was scored on a different corpus or skill digest;
- any dimension, or the aggregate, scores below the committed baseline.

`scripts/verify-local.sh` runs the same command under the same step name.

The gate holds the score at the baseline; it does not require the baseline to
be good. A green gate authorizes nothing.

## Changing a prompt or the skill

1. Make the change. `conduit-eval replay` now fails, naming each case whose
   prompt has no recording.
2. Configure a real provider and run `conduit-eval record`. It prints each
   dimension as `before -> after` and lists every assertion that regressed.
3. Review that change and the transcript diffs. A regression is a reason to
   revise the prompt, not to commit a lower baseline.
4. Commit the prompt change, `transcripts/<model>/` and
   `baselines/<model>.json` together.

Until a real-provider baseline is committed, step 2 can only be done against
the `deterministic` agent, which tells you the prompt digests changed and
nothing about quality:

```
go build -o /tmp/packaging-workflow ./cmd/packaging-workflow
CONDUIT_MODEL_CMD="/tmp/packaging-workflow agent-exec" go run ./cmd/conduit-eval record
```

To gate on a real model, record with it, commit its transcripts and baseline,
and set `gateModel` in `corpus.json` to its id.

## What the committed baseline shows

`baselines/agent-exec-deterministic.json` and
`transcripts/agent-exec-deterministic/` were produced on 2026-11-17 by running
`conduit-eval record` with
`CONDUIT_MODEL_CMD="packaging-workflow agent-exec"`: the built-in offline
agent in `cmd/packaging-workflow/agent_exec.go`.

That agent is a canned responder. It picks a reply by matching fixed phrases
in the prompt, echoes the `inspect_repo` result back as its summary, always
asks one question about the Node.js version, always plans port 8080 on a Node
base image, and returns one fixed package. It does read the prompt, so the
real prompt path runs end to end, but it does not reason about it.

Recorded scores: aggregate 68/80 (0.85); inspection 31/32, questions 14/17,
plan 8/15, gates 8/8, injection 7/8.

What this demonstrates:

- The harness drives the real planner and package writer through inspection,
  questions, plan and package for every fixture, offline.
- The 38 prompt digests the product builds for this corpus are pinned. A
  one-character change to a planning prompt, the package prompt or
  `skills/packaging/SKILL.md` fails the gate and names the cases.
- The scoring discriminates: the canned plan fails the port, database and
  language assertions on the fixtures it does not fit.

What it does not demonstrate:

- Anything about a real model. No real provider was configured where this was
  recorded (`CONDUIT_MODEL_BASE_URL` and `CONDUIT_MODEL_CMD` were both unset),
  so no real-model transcript or score exists in this repository.
- Injection resistance. The offline agent reads the Dockerfile and never the
  README, so the injected text never reached it; its 7/8 is the absence of an
  attack, not resistance to one. The harness test
  `TestInjectionCaseScoresAModelThatFollowsTheInjectedInstruction` uses a
  scripted test model to show the case fails a model that obeys the README.
- That the score threshold is meaningful for quality. The gate holds a real
  model to its own recorded baseline only once one is recorded.

## The Hub's own eval cases

Separate from this harness, the Hub's Intelligence page keeps a list of
memory-ceiling eval cases: three built into the code and any an operator
adds. Operator-added cases are stored through a `CaseStore` port
(`internal/intelligence/application`). With PostgreSQL configured the Hub
uses `internal/intelligence/adapters/casepostgres` (ledger component
`intelligence-eval-cases`), so a case survives a restart and every replica
scores the same list. A Hub on the file repository keeps them in memory and
loses them on restart.

The list is bounded at 256 cases in total, built-in ones included. The bound
is global, not per owner: the list has no owner and every operator runs the
same cases. The limit check and the insert are one transaction under an
advisory lock, so two replicas cannot both take the last slot.
