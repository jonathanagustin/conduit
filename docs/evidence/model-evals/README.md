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
- `transcripts/agent-exec-deterministic/`: one recorded exchange per prompt
  sha256, produced by the offline `packaging-workflow agent-exec` agent. They
  are a canned responder's replies, not a model's.
- `baselines/agent-exec-deterministic.json`: the score file for those
  transcripts.

Do not edit a transcript or a baseline by hand. `conduit-eval record` writes
both.
