import copy
import unittest
from wire import assert_report


class ExactReportTest(unittest.TestCase):
    def test_each_coordinate_and_terminal_observation_is_required(self):
        plan = {"id": "plan-a", "plan": {"steps": [{"releaseDigest": "sha256:release", "configDigest": "sha256:config"}]}}
        report = {"environment": "env-a", "agentId": "wire-agent", "planId": "plan-a", "observedAt": "now", "revision": 1,
                  "entities": [{"installationId": "platform-e2e", "product": "example.test:platform-e2e", "version": "0.1.0", "namespace": "env-a",
                                "releaseDigest": "sha256:release", "configDigest": "sha256:config", "health": "UNKNOWN", "healthDetails": {"quorum": "ALL", "nodes": []},
                                "rolloutStatus": "UP_TO_DATE", "configStatus": "APPLIED", "lifecycle": "INSTALLED", "observedAt": "now"}]}
        assert_report(report, plan, "env-a")
        for key in report:
            broken = copy.deepcopy(report)
            del broken[key]
            with self.subTest(field=key), self.assertRaises(RuntimeError):
                assert_report(broken, plan, "env-a")
        for key in report["entities"][0]:
            broken = copy.deepcopy(report)
            del broken["entities"][0][key]
            with self.subTest(entity_field=key), self.assertRaises(RuntimeError):
                assert_report(broken, plan, "env-a")
        for field in ("planId", "agentId", "environment"):
            broken = copy.deepcopy(report)
            broken[field] = "other"
            with self.subTest(changed=field), self.assertRaises(RuntimeError):
                assert_report(broken, plan, "env-a")
        broken = copy.deepcopy(report)
        broken["entities"].append(copy.deepcopy(broken["entities"][0]))
        with self.assertRaises(RuntimeError):
            assert_report(broken, plan, "env-a")


if __name__ == "__main__":
    unittest.main()
