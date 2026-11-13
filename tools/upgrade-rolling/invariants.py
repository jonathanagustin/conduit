"""Independent observer of the persisted reconciliation transaction boundary."""
import hashlib


# One SQL statement gives all cross-table reads one MVCC snapshot, including
# while replicas are committing decisions or completing outbox dispatches.
SNAPSHOT_SQL = """
SELECT json_build_object(
 'decisions', COALESCE((SELECT json_agg(json_build_object('id',id,'kind',kind,'environment',environment,'installation',installation,'inputDigest',input_digest)) FROM conduit_reconciliation_decisions), '[]'::json),
 'audits', COALESCE((SELECT json_agg(decision_id) FROM conduit_reconciliation_audit), '[]'::json),
 'outbox', COALESCE((SELECT json_agg(json_build_object('id',proposal_id,'environment',environment,'installation',installation,'inputDigest',convert_from(document,'UTF8')::jsonb->>'inputDigest')) FROM conduit_reconciliation_proposal_outbox), '[]'::json),
 'receipts', COALESCE((SELECT json_agg(proposal_id) FROM conduit_reconciliation_plan_receipts), '[]'::json)
);
"""


def proposal_id(decision_id):
    # Stable proposal identity is rcp_ + the first 16 SHA-256 bytes of decision
    # identity, as defined in reconciliation/domain/types.go for both versions.
    return "rcp_" + hashlib.sha256(decision_id.encode()).hexdigest()[:32]


def inspect(snapshot, previous=None):
    decisions = {row["id"]: row for row in snapshot["decisions"]}
    audits = set(snapshot["audits"])
    outbox = {row["id"]: row for row in snapshot["outbox"]}
    receipts = set(snapshot["receipts"])
    if not decisions or set(decisions) != audits:
        raise RuntimeError("empty or torn decision/audit boundary")
    expected = {proposal_id(row["id"]): row for row in decisions.values() if row["kind"] == "PROPOSE_PLAN"}
    if not expected or set(expected) != set(outbox) | receipts or set(outbox) & receipts:
        raise RuntimeError("lost, extra or torn proposal/receipt boundary")
    if any(any(row[key] != expected[row["id"]][key] for key in ("environment", "installation", "inputDigest")) for row in outbox.values()):
        raise RuntimeError("proposal has no matching input decision")
    observed = {"decisionIDs": sorted(decisions), "proposalIDs": sorted(expected),
                "decisions": len(decisions), "audits": len(audits), "pendingProposals": len(outbox), "receipts": len(receipts)}
    if previous and (not set(previous["decisionIDs"]).issubset(decisions) or not set(previous["proposalIDs"]).issubset(expected)):
        raise RuntimeError("previously observed decisions or proposals disappeared")
    return observed
