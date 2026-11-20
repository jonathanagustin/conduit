# Assistant feedback confirmation

The packaging context's **Add eval case** action uses the existing native
`governed_write` tool and `add_eval_case` arguments. It asks for the scenario,
memory value and expected outcome; completing those fields does not record a
case. The existing role rule is evaluated again when Confirm is submitted.

The confirmation card shows the server-returned bound object, scenario, memory
value and expected outcome as escaped text. Only those four feedback fields are
rendered. The review is read-only and contributes no additional command fields;
the existing explicit Confirm sends the reviewed arguments with `confirm: true`.
A successful receipt identifies the case in the configured Hub catalog. Repeating
an identical confirmed case returns the same content-derived ID.

This is a Conduit UI extension over the existing native tool contract, not a new
store, permission or evaluation engine. Recording feedback does not run an
evaluation, approve packaging or execute a workflow. The case catalog is shared,
not owner-private; persistence follows its configured store. Browser reload
readback in the local simulation does not establish process-restart durability,
model quality or deployed tenant qualification.
