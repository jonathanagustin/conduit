"""Dataset/negative-contract checks, not application or independent trial results."""
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("corpus", Path(__file__).with_name("verify-packaging-reference-corpus.py"))
v = importlib.util.module_from_spec(spec)
spec.loader.exec_module(v)


class ReferenceCorpusTests(unittest.TestCase):
    def setUp(self):
        root = v.ROOT / v.DATA
        self.corpus = v.decode((root / "corpus-v1.json").read_bytes())
        self.matrix = v.decode((root / "matrix-v1.json").read_bytes())
        self.profile = (root / "held-out/valkey-v1.json").read_bytes()
        self.checksum = (root / "held-out/valkey-v1.sha256").read_text()

    def verify(self):
        return v.verify(self.corpus, self.matrix, self.profile, self.checksum,
                        lambda relative: (v.ROOT / relative).is_file())

    def test_real_dataset_does_not_claim_runtime_or_authority(self):
        result = v.load()
        self.assertEqual(result["datasetContract"], "PASS")
        self.assertEqual(result["applicationCorrectness"], "NOT_RUN")
        self.assertEqual(result["runtimeEvidenceCoverage"], 0)
        self.assertFalse(result["deploymentAuthorized"])

    def test_generator_cannot_change_profile_and_checksum_together(self):
        profile = v.decode(self.profile)
        profile["requiredChecks"].pop()
        self.profile = json.dumps(profile).encode()
        self.checksum = hashlib.sha256(self.profile).hexdigest() + "  valkey-v1.json\n"
        with self.assertRaisesRegex(ValueError, "criteria changed"):
            self.verify()

    def test_unrun_matrix_cannot_forge_pass_or_structural_receipt(self):
        for mutation in [lambda m: m["checks"][0].update(status="PASS"),
                         lambda m: m["checks"][0].update(receipts=[{"status":"PASS", "signatureVerified":True}])]:
            with self.subTest(mutation=mutation):
                original = copy.deepcopy(self.matrix)
                mutation(self.matrix)
                with self.assertRaisesRegex(ValueError, "success receipt"):
                    self.verify()
                self.matrix = original

    def test_cannot_relax_or_remove_operator_required_checks(self):
        self.matrix["checks"][0]["criterion"] = "generated YAML exists"
        with self.assertRaisesRegex(ValueError, "weakened"):
            self.verify()
        self.setUp()
        self.matrix["checks"].pop()
        with self.assertRaisesRegex(ValueError, "remove required"):
            self.verify()

    def test_retrospective_example_cannot_become_normative_or_executed(self):
        self.corpus["knownExamplePlatform"]["normativeForTarget"] = True
        with self.assertRaisesRegex(ValueError, "not target-version"):
            self.verify()
        self.setUp()
        self.corpus["execution"]["independentTrialRun"] = True
        with self.assertRaisesRegex(ValueError, "not executed"):
            self.verify()

    def test_mutable_or_unresolved_source_rejected(self):
        self.corpus["sources"][0]["revision"] = "main"
        with self.assertRaisesRegex(ValueError, "immutable"):
            self.verify()
        self.setUp()
        self.corpus["lessons"][0]["sources"] = ["unfetched-source"]
        with self.assertRaisesRegex(ValueError, "unresolved source"):
            self.verify()

    def test_unsigned_structural_result_cannot_gain_authority(self):
        self.matrix["existingEvidence"][0]["trustedProducerVerified"] = True
        with self.assertRaisesRegex(ValueError, "cannot authenticate"):
            self.verify()

    def test_required_negative_cases_cannot_disappear(self):
        self.matrix["negativeCases"].remove("unknown-migration")
        with self.assertRaisesRegex(ValueError, "negative cases"):
            self.verify()

    def test_known_examples_cannot_be_relabelled_blind(self):
        self.matrix["knownExamplesExcludedFromBlindEvaluation"].remove("GeoServer")
        with self.assertRaisesRegex(ValueError, "cannot become blind"):
            self.verify()

    def test_every_negative_case_has_an_explicit_application_status(self):
        self.matrix["negativeCaseCoverage"].pop()
        with self.assertRaisesRegex(ValueError, "all sixteen"):
            self.verify()
        self.setUp()
        self.matrix["negativeCaseCoverage"][0] = copy.deepcopy(self.matrix["negativeCaseCoverage"][1])
        with self.assertRaisesRegex(ValueError, "all sixteen"):
            self.verify()

    def test_protocol_fixture_cannot_be_promoted_to_application_proof(self):
        for change in [{"status": "PASS"}, {"applicationReceipts": ["unit-test-success"]}]:
            with self.subTest(change=change):
                self.setUp()
                self.matrix["negativeCaseCoverage"][0].update(change)
                with self.assertRaisesRegex(ValueError, "application negative-case"):
                    self.verify()

    def test_retained_candidate_does_not_approve_a_trial(self):
        self.matrix["candidateDisposition"]["trialApproved"] = True
        with self.assertRaisesRegex(ValueError, "trial approval"):
            self.verify()

    def test_licensing_boundary_rejects_copied_implementation(self):
        self.corpus["sources"][0]["implementationCopied"] = True
        with self.assertRaisesRegex(ValueError, "must not vendor"):
            self.verify()

    def test_duplicate_json_keys_fail(self):
        with self.assertRaisesRegex(ValueError, "duplicate JSON key"):
            v.decode('{"status":"NOT_RUN","status":"PASS"}')


if __name__ == "__main__":
    unittest.main()
