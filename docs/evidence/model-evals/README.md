# Model evaluation corpus

Machine-consumed input to `conduit-eval` and to the **Validate model-eval
replay** step of the Trunk gate. How to use it, and what the committed
baseline does and does not show, is in
[docs/operations/model-evals.md](../../operations/model-evals.md).

- `corpus.json`: the cases and their expected facts. Each assertion's `source`
  names the fixture file the expected value was read from.
- `fixtures/`: small synthetic repositories written for this corpus. They are
  test inputs, not products: nothing builds, runs or deploys them, and their
  dependency pins are never updated. `fixtures/injection-readme/README.md`
  deliberately states a wrong port and contains a prompt-injection block.
- `transcripts/openai-http-fixture-requested/` and its matching baseline:
  current replay gate, freshly recorded through the supported loopback HTTP
  fixture using the offline deterministic agent's reply function. These are
  canned replies, not a real model's evaluation. Corpus 1.1.0 preserves all
  expected facts and the prior 68/80 score.
- `transcripts/agent-exec-deterministic/` and its matching baseline: frozen
  historical evidence from the retired host-command recording route. They are
  retained without relabelling their old prompts or responses. Use the earlier
  repository revision for a matching historical corpus/prompt snapshot.

To refresh only the labelled current fixture after reviewing a prompt change:

```sh
CONDUIT_RECORD_EVAL_FIXTURE=1 go test -count=1 \
  -run '^TestDeterministicHTTPModelEvaluationFixture$' -v ./cmd/packaging-workflow
```

The generator uses the real HTTP model port, refuses a different gate provider,
and refuses a changed aggregate or any previously passing assertion regressing.
Its ordinary test compares a fresh recording with the committed report. This
never establishes real-model quality or refreshes captured-model evidence.

Do not edit a transcript or a baseline by hand. `conduit-eval record` writes
both. The explicit fixture generator above uses that same recording API.
