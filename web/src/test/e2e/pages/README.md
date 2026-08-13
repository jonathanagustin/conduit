# Console page objects and locators

Browser specs interact with recurring console surfaces through the page objects
in this directory. A page object exposes domain language (`release`,
`resourceDiagnostics`, `promotionTransition`) and owns the underlying locator.

- Use `data-testid` for stable structural identity: page roots, immutable
  resource rows, ordered tasks, pipelines, transitions, and detail regions.
- Use role/name locators inside a page object when the accessible name is itself
  the contract under test.
- Keep visible-text assertions in the spec: changing operator wording should be
  review-visible, but it should not break the locator used to reach the surface.
- Do not export raw CSS selectors from a spec. Add a semantic method to the
  nearest page object instead.
- A test ID locates; it does not prove accessibility. Retain independent role,
  keyboard, mobile, and axe assertions.

Test IDs are deliberately local to their rendered domain object. Dynamic
suffixes use immutable IDs or exact sequence/version coordinates already in the
Hub contract, never array indexes invented by the browser test.
