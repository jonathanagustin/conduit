import copy
import unittest
from invariants import inspect, proposal_id


class AtomicBoundaryTest(unittest.TestCase):
    def test_pending_proposal_can_complete_but_cannot_disappear_or_tear(self):
        decision = {"id": "decision-a", "kind": "PROPOSE_PLAN", "environment": "env", "installation": "app", "inputDigest": "sha256:input"}
        proposal = {key: decision[key] for key in ("environment", "installation", "inputDigest")}
        proposal["id"] = proposal_id(decision["id"])
        snapshot = {"decisions": [decision], "audits": [decision["id"]], "outbox": [proposal], "receipts": []}
        initial = inspect(snapshot)
        completed = copy.deepcopy(snapshot)
        completed.update(outbox=[], receipts=[proposal["id"]])
        self.assertEqual(inspect(completed, initial)["receipts"], 1)
        for missing in ("decisions", "audits", "outbox"):
            broken = copy.deepcopy(snapshot)
            broken[missing] = []
            with self.subTest(missing=missing), self.assertRaises(RuntimeError):
                inspect(broken, initial)
        duplicate = copy.deepcopy(snapshot)
        duplicate["receipts"] = [proposal["id"]]
        with self.assertRaises(RuntimeError):
            inspect(duplicate)
        unrelated = copy.deepcopy(snapshot)
        unrelated["outbox"][0]["inputDigest"] = "sha256:other"
        with self.assertRaises(RuntimeError):
            inspect(unrelated)
        replacement = copy.deepcopy(completed)
        replacement["decisions"][0]["id"] = "decision-b"
        replacement["audits"] = ["decision-b"]
        replacement["receipts"] = [proposal_id("decision-b")]
        with self.assertRaises(RuntimeError):
            inspect(replacement, initial)


if __name__ == "__main__":
    unittest.main()
