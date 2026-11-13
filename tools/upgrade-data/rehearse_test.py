import unittest
from unittest.mock import patch
from rehearse import compare_snapshots, previous_release


class PreservationTest(unittest.TestCase):
    def test_baseline_is_stable_and_differs_from_current_commit(self):
        def git(*args):
            if args[1] == "tag":
                return "v2.0.0-rc1\nv1.2.0\nv1.1.0\nv1.0.0"
            return {"v1.2.0^{commit}": "current", "v1.1.0^{commit}": "previous"}[args[-1]]
        with patch("rehearse.command", side_effect=git):
            self.assertEqual(previous_release("current"), ("v1.1.0", "previous"))
        with patch("rehearse.command", return_value=""):
            with self.assertRaises(RuntimeError):
                previous_release("current")

    def test_additive_fields_survive_without_weakening_old_values(self):
        old = {"plan/id": {"status": "PENDING", "steps": [{"id": "a"}]}}
        compare_snapshots(old, {"plan/id": {"status": "PENDING", "newField": 1, "steps": [{"id": "a", "newField": True}]}})
        for bad in (
            {},
            {"plan/other": old["plan/id"]},
            {"plan/id": {"steps": [{"id": "a"}]}},
            {"plan/id": {"status": "SUCCEEDED", "steps": [{"id": "a"}]}},
            {"plan/id": {"status": "PENDING", "steps": []}},
            {"plan/id": {"status": "PENDING", "steps": [{"id": "b"}]}},
            {"plan/id": old["plan/id"], "plan/extra": {}},
        ):
            with self.subTest(snapshot=bad), self.assertRaises(RuntimeError):
                compare_snapshots(old, bad)


if __name__ == "__main__":
    unittest.main()
