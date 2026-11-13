import copy
from datetime import datetime, timedelta, timezone
import unittest
from rolling import live_lease


class LiveLeaseTest(unittest.TestCase):
    def test_same_live_owner_renews_without_reacquisition_or_expiry(self):
        now = datetime(2026, 11, 13, tzinfo=timezone.utc)
        record = {"status": "RUNNING", "lease": {"agentId": "wire-agent", "attempt": 1, "issuedAt": now.isoformat(), "expiresAt": (now + timedelta(seconds=20)).isoformat()}}
        identity, expiry = live_lease(record, now=now)
        # The first sample has no previous expiry to constrain it. Equality
        # with now is already expired, not a live lease boundary.
        for expired in (now, now - timedelta(seconds=1)):
            broken = copy.deepcopy(record)
            broken["lease"]["expiresAt"] = expired.isoformat()
            with self.subTest(initial_expiry=expired), self.assertRaises(RuntimeError):
                live_lease(broken, now=now)
        self.assertEqual(live_lease(record, identity, expiry, now), (identity, expiry))
        renewed = copy.deepcopy(record)
        renewed["lease"]["expiresAt"] = (now + timedelta(seconds=30)).isoformat()
        self.assertGreater(live_lease(renewed, identity, expiry, now)[1], expiry)
        for field, value in (("agentId", "other"), ("attempt", 2), ("issuedAt", (now + timedelta(seconds=1)).isoformat()), ("expiresAt", now.isoformat()), ("expiresAt", (now + timedelta(seconds=19)).isoformat())):
            broken = copy.deepcopy(record)
            broken["lease"][field] = value
            with self.subTest(field=field, value=value), self.assertRaises(RuntimeError):
                live_lease(broken, identity, expiry, now)
        for status in ("AWAITING_APPROVAL", "LEASED", "FAILED", "SUCCEEDED"):
            broken = copy.deepcopy(record)
            broken["status"] = status
            with self.subTest(status=status), self.assertRaises(RuntimeError):
                live_lease(broken, identity, expiry, now)


if __name__ == "__main__":
    unittest.main()
