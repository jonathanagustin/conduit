# Heterogeneous evaluator batch — 2026-08-09

This directory preserves a read-only three-family review of immutable Palantir
commit `78af3be3bbd73202cc06bbe0f5ef1639e8159a10`. All arms used the same prompt
and content-addressed `platform/` snapshot. Findings remain hypotheses until an
independent repository reproduction records a terminal disposition and an
executable gate.

The raw Cursor receipts are retained for provenance. Model-level monetary cost
was not present in those receipts and is recorded as unavailable, never zero.

Independent reproduction has resolved seven findings into three canonical
defect clusters. The two stored `EvaluatorResolution` records bind the exact
source revisions and executable gates. The provider-neutral Zenith export binds
both resolution records by repository-relative path and SHA-256; unresolved
findings remain hypotheses.
