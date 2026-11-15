"""Check the reference/acceptance dataset, not application correctness or authority.

No source scripts, package builds, containers, scanners or clusters are invoked.
The frozen candidate predates any solution inspection and still needs independent
operator/evaluator approval. Unsigned structural receipts remain non-authoritative.
"""
# CONDUIT-INVENTED: packaging.ReferenceValidationMatrix
# Displaced-by: A reviewed replacement reference-corpus, independent evaluation and operator-owned validation-profile contract.
import hashlib
import json
from pathlib import Path
import re
import sys

ROOT = Path(__file__).resolve().parent.parent
DATA = Path("docs/evidence/uds-packaging")
PROFILE_SHA256 = "3444397f56449c8decb5d0ce247645c8b2bc811a74a4fe665a023a6a3b2e4b4b"
REQUIRED = {
    "first-process-config", "tls-ca-rotation", "durable-data-restore",
    "disconnected-dependencies", "schemas-render", "real-uds-package",
    "package-build", "security-policy", "upgrade-recovery", "authorization-freshness",
}
NEGATIVE = {
    "unresolvable-dependency", "unsupported-chart-capability", "unknown-migration",
    "unsigned-or-mutable-input", "unauthorized-action", "artifact-changed-after-validation",
    "chart-digest-mismatch", "undeclared-upgrade-only-image", "source-or-template-drift",
    "stale-or-replayed-approval", "changed-target-or-configuration", "scanner-timeout",
    "scanner-malformed-output", "forged-summary", "cancelled-run", "partial-execution",
}
KINDS = {"retrospective-decision", "decision-and-limitations", "static-source",
         "test-source-not-executed", "versioned-platform-guidance", "reference-workflow"}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def unique_object(pairs):
    obj = {}
    for key, value in pairs:
        require(key not in obj, "duplicate JSON key")
        obj[key] = value
    return obj


def decode(raw):
    return json.loads(raw, object_pairs_hook=unique_object)


def verify(corpus, matrix, profile_bytes, checksum, existing_file):
    require(hashlib.sha256(profile_bytes).hexdigest() == PROFILE_SHA256,
            "held-out v1 criteria changed; both profile and checksum cannot self-authorize a revision")
    require(checksum.split() == [PROFILE_SHA256, "valkey-v1.json"], "invalid frozen profile checksum")
    profile = decode(profile_bytes)
    for document in [corpus, matrix, profile]:
        require(type(document.get("schemaVersion")) is int and document["schemaVersion"] == 1,
                "unsupported dataset schema")
        require(document.get("valueTier") == "CONDUIT_NATIVE", "dataset must disclose native scope")
    require(profile["state"] == "PROPOSED_OPERATOR_PROFILE", "candidate is not operator-approved")
    require(not any(profile["authority"][key] for key in ["profileApproved", "independentEvaluatorAssigned",
                "deploymentAuthorized", "clusterOrDeviceActionsAuthorized", "generatorMayRelaxRequiredChecks"]),
            "frozen proposal cannot self-issue approval or execution authority")
    require(profile["result"]["status"] == "NOT_RUN", "held-out trial was not run")
    require(corpus["platform"]["version"] == "1.15.0" and
            corpus["platform"]["revision"] == "338b8dde2081b3e7aab12acbff225c3f2a0865b9", "target platform pin changed")
    require(corpus["knownExamplePlatform"]["udsCoreVersion"] == "1.16.0" and
            corpus["knownExamplePlatform"]["normativeForTarget"] is False, "TAK example is not target-version policy")
    require(corpus["execution"]["status"] == "NOT_RUN" and
            all(value is False for key, value in corpus["execution"].items() if key != "status"),
            "reference inspection is not executed workload evidence")
    sources = corpus["sources"]
    ids = {source["id"] for source in sources}
    require(len(sources) == len(ids) and len(sources) >= 9, "missing or duplicate reference sources")
    for source in sources:
        require(re.fullmatch(r"[0-9a-f]{40}", source["revision"]) is not None, "source revision must be immutable")
        require(re.fullmatch(r"[0-9a-f]{64}", source["sha256"]) is not None, "source content digest missing")
        expected = f'https://github.com/{source["repository"]}/blob/{source["revision"]}/{source["path"]}'
        require(source["url"] == expected, "source URL differs from its immutable coordinate")
        require(source["kind"] in KINDS and source["scope"] and source["licenseObserved"], "source needs bounded class and licensing")
        require(source["implementationCopied"] is False, "references must not vendor upstream implementation")
        require(type(source["lineCount"]) is int and source["lineCount"] > 0, "invalid source extent")
    lessons = corpus["lessons"]
    require(len({lesson["id"] for lesson in lessons}) == len(lessons) and len(lessons) >= 9, "missing/duplicate lessons")
    for lesson in lessons:
        require(lesson["sources"] and set(lesson["sources"]) <= ids, "lesson has unresolved source")
        require(lesson["checks"] and set(lesson["checks"]) <= REQUIRED, "lesson has unresolved validation check")
        require(lesson["evidenceLimit"] and lesson["alternativesAndRejections"], "lesson must retain limitations and alternatives")
    require(matrix["profile"] == "held-out/valkey-v1.json", "wrong held-out candidate")
    require(set(matrix["knownExamplesExcludedFromBlindEvaluation"]) == {"TAK", "GeoServer"}, "known examples cannot become blind evaluation")
    expected_checks = {check["id"]: check for check in profile["requiredChecks"]}
    require(set(expected_checks) == REQUIRED, "required acceptance checks changed")
    checks = matrix["checks"]
    require(len(checks) == len(REQUIRED) and {check["id"] for check in checks} == REQUIRED, "matrix cannot remove required checks")
    for check in checks:
        frozen = expected_checks[check["id"]]
        require(check["level"] == frozen["level"] and check["criterion"] == frozen["criterion"], "matrix weakened frozen acceptance")
        require(check["status"] == "NOT_RUN" and check["receipts"] == [], "unrun matrix cannot contain a success receipt")
        require(check["requiresSeparateRuntimeAuthorization"] == (check["level"] in {"integration", "restore", "build"}),
                "runtime/build authorization boundary changed")
    for evidence in matrix["existingEvidence"]:
        require(existing_file(evidence["path"]), "existing evidence path is missing")
        require(evidence["trustedProducerVerified"] is False and evidence["runtimeValidation"] is False,
                "structural/protocol evidence cannot authenticate runtime results")
    require(set(matrix["negativeCases"]) == NEGATIVE, "required negative cases changed")
    disposition = matrix["candidateDisposition"]
    require(disposition["decision"] == "RETAIN_UNSUPPORTED_NEGATIVE_BASELINE" and
            disposition["profile"] == matrix["profile"] and disposition["reason"] and
            disposition["independentAssignment"] is False and disposition["trialApproved"] is False and
            disposition["executionIssue"] == 639, "candidate disposition cannot imply trial approval")
    coverage = matrix["negativeCaseCoverage"]
    require(len(coverage) == len(NEGATIVE) and {case["id"] for case in coverage} == NEGATIVE,
            "negative-case coverage must reconcile all sixteen cases exactly once")
    for case in coverage:
        require(case["status"] == "NOT_RUN" and case["applicationReceipts"] == [] and
                case["reason"] and case["followUpIssue"] == 639,
                "protocol fixtures cannot become application negative-case receipts")
    require(corpus["licensePolicy"] and all(source["implementationCopied"] is False for source in sources),
            "corpus must remain references and original factual paraphrases")
    require(matrix["ownership"]["generatorIsApprovalAuthority"] is False and
            matrix["ownership"]["liveTrialAuthorized"] is False, "dataset grants no execution authority")
    measured = matrix["measurement"]
    require(measured["applicationCorrectness"] == "NOT_RUN" and measured["runtimeEvidenceCoverage"] == 0 and
            measured["runtimeReproducibility"] == "NOT_RUN" and measured["explicitUnrunChecks"] == len(REQUIRED) and
            measured["failures"], "measurement must expose unrun checks and blockers")
    return {"valueTier": "CONDUIT_NATIVE", "datasetContract": "PASS", "sourceDocuments": len(sources),
            "boundedLessons": len(lessons), "requiredApplicationChecks": len(REQUIRED),
            "applicationCorrectness": "NOT_RUN", "runtimeEvidenceCoverage": 0,
            "producerAuthorityVerified": False, "deploymentAuthorized": False}


def load(root=ROOT):
    data = root / DATA
    corpus = decode((data / "corpus-v1.json").read_bytes())
    matrix = decode((data / "matrix-v1.json").read_bytes())
    profile = (data / "held-out/valkey-v1.json").read_bytes()
    checksum = (data / "held-out/valkey-v1.sha256").read_text()
    def existing_file(relative):
        path = root / relative
        return not Path(relative).is_absolute() and path.resolve().is_relative_to(root.resolve()) and path.is_file()
    return verify(corpus, matrix, profile, checksum, existing_file)


if __name__ == "__main__":
    try:
        require(len(sys.argv) == 1, "this checker takes no generated-workspace or authority arguments")
        print(json.dumps(load(), sort_keys=True))
    except (ValueError, KeyError, TypeError, OSError) as error:
        raise SystemExit(f"reference dataset check failed: {error}")
