# Local model CLI

Set `CONDUIT_MODEL_CMD` to the argv of a local command. The documented default is `codex exec --skip-git-repo-check --sandbox read-only --ephemeral -`.

Leave `CONDUIT_MODEL_CMD` unset to keep the recorded proposals and make no model call. `CONDUIT_MODEL_TIMEOUT` defaults to 45s. Set `PACKAGING_FIXTURE_DIR` to the worker fixture directory so the prompt includes that run's chart values or resources excerpt, memory request, memory limit, image ref, and image digest. The Hub writes that packaging context to the command's standard input, validates one JSON object, and runs the same memory ceiling as the packaging gate. A passing proposal is stored as a change request that still needs a person. Credentials stay in the command's own configuration on the machine that runs the Hub.
