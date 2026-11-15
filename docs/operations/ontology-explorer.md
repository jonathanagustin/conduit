# Native ontology explorer

The existing `s19-ontology` registry reads the repository-owned ontology SDK's
`fullMetadata`, presents declared properties and links, and requests authorized
25-row object pages. Previous/next navigation uses returned page tokens.
Inspect opens the existing shared inspector. The relationship graph links to
this same catalog; work intake and the existing assistant remain the entry to
app generation. No additional assistant or approval path is introduced.

The source contract is `testdata/ontology/full-metadata.json` and
`web/src/api/ontology-client`. These are Conduit-native contracts, not proof of
private Apollo or Foundry behavior. Metadata grants neither entity access nor
action authority. Per-object permission editing, schema publishing, a source
editor and commit history remain unavailable rather than simulated.

The action panel sends only SDK `actions.validate`, whose request specifies
`VALIDATE_ONLY`. Accepted validation neither applies edits nor stores a change
request. Parameters must be a JSON object of at most 65,536 characters. Sample
values use the existing sensitive-field redactor; this is a display safeguard,
not an authorization boundary or comprehensive data-loss prevention policy.

Metadata and page failures display unavailable states. A successful empty
catalog stays empty. The graph no longer substitutes fixture environments,
objects, counts or relationships. An edge requires exactly two declarations
sharing a nonempty link identifier on different types; ambiguous links remain
visible in the registry declarations without an invented graph edge. Graphs
show at most 256 types and sample pages at most 25 rows. The registry bounds
property displays at 128 and types/actions at 256.

Both views discard late reads after native identity or selected-type changes.
Explorer validation results also expire when the action or parameter draft
changes. Background Hub refresh preserves the current registry form only for
the same native identity. Reads and validation have a 15-second UI deadline;
this bounds waiting but does not cancel an already submitted server request.
There is no retry that can turn validation into execution.

Regression evidence includes the checked-in metadata projection, empty and
malformed metadata, ambiguous links, zero-valued primary keys, bounded parameter
objects, and a browser-held response that returns after another type has been
selected. The browser verifies escaped/redacted samples, paging denial and the
exact validation-only request body.
