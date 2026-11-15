# Governed A2UI planning forms

Needs Input is described with the retained A2UI 1.0 wire schemas at
`a2ui-project/a2ui@3ed9601862ecc78027e20ac80ad8b1b98a5d9079`. Source hashes and
Apache-2.0 notices are under `api/a2ui/upstream`. The approved application
catalog is `conduit:planning-forms/v1`, defined in `api/a2ui/catalog.json`.
It is a Conduit extension, not the upstream basic catalog or a private-platform
UI contract. The Go validator's ASCII-only extension-key restriction is
explicit in `api/a2ui/README.md` and is also enforced by the browser profile.

## Catalog and validation

The catalog contains Text, TextField, TextArea, Select, CheckBox, RadioGroup,
Button, Column and Row. Text is inert and escaped. Input labels, values,
validation messages, IDs and action attributes are escaped; IDs used by labels
are encoded. There is no script, HTML, image, link, URL or embedded component.
Buttons dispatch declarative events rather than functions or native mutations.

Only `required`, `pattern` and `enum` functions are admitted. Patterns come from
three reviewed, bounded expressions in the catalog; arbitrary regular
expressions, function names and renderer/agent function RPC are refused.
Unknown catalogs, overrides, properties or component/function names fail
validation. The native profile additionally limits each renderer to four
surfaces, 64 components per surface, depth eight, a complete non-shared tree,
256 KiB messages and 128 KiB object data models. An initial component-less
surface can wait for one complete component update. Cycles, dangling child
references and unsafe JSON Pointer paths are refused before mounting.

The existing Go JSON Schema engine validates the Hub's produced messages.
AJV 8.20.0 is a pinned build dependency that produces committed standalone ESM
validators for the same retained schemas/catalog. The browser imports those
validators and tiny fixed runtime helpers; it does not compile agent schemas
or execute agent code. Both console build paths check regenerated output, and
schema/catalog/generator files participate in the build stamp. Incoming
messages are validated before surface state changes. A refused message records
a bounded `FORM_REFUSED` diagnostic and produces a visible planning error.

## Binding, errors and human review

Each surface owns its own object data model. `@path` bindings use checked JSON
Pointers; human input changes the bound value locally. `updateDataModel`
changes only that surface. `/` or an omitted path replaces its root model;
null removes a non-root key. Updates are atomic. An update overlapping a human
edited path is skipped as a whole with `PREFILL_SKIPPED_DIRTY`; human drafts
win. This is a deliberate Conduit policy. Drafts survive the fresh renderer
created for each AG-UI run, including snapshot recovery.

Input `@checks` produce adjacent errors and block submission. The optional
owned `error` property displays returned field errors, also blocking submit
until the user changes that field. Labels, native keyboard navigation,
`aria-invalid`, error descriptions and focus on the first failed field are
part of the intake binding. Select/radio choices are checked independently of
optional check declarations. The renderer does not run an expression language.

## AG-UI and authority

A2UI arrives only in AG-UI `CUSTOM` events with `name: "a2ui"`. No separate form
endpoint is mounted. The Hub translates the model adapter's typed native
questions into an approved surface with `createSurface` and
`updateDataModel`; it does not accept arbitrary model-provided catalogs or UI
code. The required Needs Input form retains optional questions in the same
review. Arbitrary model-authored surfaces are not a supported producer port.

Surface IDs bind the current form revision and producing run. A new run owns a
fresh renderer; recovery uses a new ID and snapshot, not SSE replay. Submit
resolves the button's context from the current local model into an A2UI
`action`, carrying name, surface, source component, timestamp and resolved
values. The next AG-UI input carries that action in
`forwardedProps.conduit.a2uiAction` and the existing bound resume entry.
Boolean values become the native answer strings. The Hub validates the action
schema, exact current surface/form/button/name and equality with the resume
payload before applying any answer. Legacy non-A2UI AG-UI resume remains valid.

Prefill messages and submitted actions have digest-only audit receipts with
the authenticated owner and current thread/run correlation. Audit writes
precede publication or answer application. Forged surfaces, changed contexts,
unknown fields and action audit outages cannot change answers. A form action
cannot invoke Accept, Compute, change-request approval or publication. Accept
remains a separate explicit native operation. The planning store and page
ledger remain process-local; no restart durability is claimed.

## Evidence limits

Tests use owned wire fixtures, a scripted planner, the mounted Hub over
loopback HTTP, and Chromium interaction with owned responses. They cover
binding, prefill, action/interrupt resume, human override, recovery, field
errors, injection, schema rejection and audit failure. They establish no live
provider behavior, tenant observation, cluster execution or delivery authority.
The workspace coding-agent activity connection remains #647, not a side door
through these form actions.
