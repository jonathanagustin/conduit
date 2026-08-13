#!/usr/bin/env python3
"""Record Apollo-named Maven coordinates at their actual evidence grain.

Fourteen `com.palantir.apollo*:*` coordinate/version strings were retained by a
flattened, four-seed Maven closure captured through the authenticated Eddie proxy.
The later POM trace found all inspected Apollo occurrences under
`dependencyManagement`, with zero direct dependencies, and exact POM/JAR probes
returned 404. The names are therefore discovery vocabulary, not proof of a
published artifact, linked dependency, backend module decomposition, or version
maturity.

Two tiers, kept apart:
  - the coordinate NAMES and retained VERSIONS are PRIMARY for the bounded
    historical closure only;
  - the jar CONTENTS - the Conjure types and routes, i.e. the wire protocol -
    are UNKNOWN. A live fetch across every repository the tenant token could see
    returned "the tenant knows no version". This does not prove whether the
    coordinates are published elsewhere and does not recover H38/H18.

    record-apollo-api-coordinates.py --out docs/evidence/apollo-api-coordinates.json
"""

from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path

CLOSURE = Path("../mcp/foundry/evidence/artifacts/foundry-maven-coordinate-closure-2026-07-17.json")
CONTRACTS = Path("../mcp/foundry/contracts/jar-contracts.json")

# Coordinate -> (lexical capability-family hint, lexical role hint). Both are
# Platform interpretations of the artifact name, never recovered behavior.
LEXICAL_ROLE_HINT: dict[str, tuple[str, str]] = {
    "apollo-deployment-state-api": ("environments/entity", "installations, product releases, and entity deployment state"),
    "apollo-deployment-state-api-objects": ("environments/entity", "deployment-state object beans"),
    "apollo-deployment-state-primitive-objects": ("environments/entity", "deployment-state primitives"),
    "apollo-catalog-api-objects": ("bundle/module", "product/release catalog object beans"),
    "apollo-catalog-primitive-objects": ("bundle/module", "catalog primitives"),
    "apollo-bundling-api-objects": ("pipelines", "bundling pipeline object beans"),
    "apollo-bundling-api-primitive-objects": ("pipelines", "bundling primitives"),
    "apollo-artifact-registry-api-objects": ("artifact-registry", "artifact registry object beans"),
    "apollo-labels-primitive-objects": ("label", "label primitives"),
    "apollo-entity-id": ("entity", "entity identifier types"),
    "apollo-maven-api": ("artifact", "maven artifact API"),
    "apollo-auth-broker-api": ("H18 auth broker", "Hub/Agent auth-broker contract"),
    "apollo-auth-broker-client-java-core": ("H18 auth broker", "auth-broker client core"),
    "scoped-token-service-api": ("H18 auth broker", "scoped-token service contract"),
}


def release_cohorts(recovered: dict) -> dict:
    """Group the retained coordinates by exact version string.

    No new capture: this is a reading of the closure already recorded here, and
    it exists because the per-coordinate lexical hints miss it. Artifacts
    sharing an exact version were published by one release of one build, so a
    cohort is a module boundary in Apollo's layout rather than in ours — which
    is obtainable even though the jar contents are not.
    """
    cohorts: dict[str, list[str]] = {}
    for name, meta in recovered.items():
        cohorts.setdefault(meta["retainedVersion"], []).append(name)

    return {
        "derivation": "Grouping of the retained coordinates above by exact version string. No new "
        "capture; an unextracted reading of the same closure.",
        "claim": "Artifacts sharing an exact version string were published by one release of one "
        "build, so each cohort is a module boundary in Apollo's own layout rather than in ours.",
        "cohorts": {
            version: {
                "artifacts": sorted(names),
                # The middle component of a gitversion-shaped string is a commit
                # count, so it orders churn across cohorts.
                "commitOrdinal": int(version.split(".")[1]),
            }
            for version, names in sorted(cohorts.items())
        },
        "findingsTheLexicalHintsMiss": [
            "scoped-token-service-api shares a version with the auth broker, so it ships with it. "
            "Its name carries no apollo-auth-broker prefix, so the lexical hint files it apart.",
            "apollo-entity-id and apollo-maven-api share a version with apollo-artifact-registry-api-objects, "
            "so they belong to that release unit rather than being free-standing commons, despite the "
            "com.palantir.apollo.commons group.",
        ],
        "hypotheses": [
            "Two cohorts sharing a middle component across different majors would be consistent with one "
            "repository releasing two major lines. Nothing here settles it; a coincidence of commit counts "
            "produces the same string.",
        ],
        "boundary": "This groups coordinate strings and nothing more. It does not establish publication, "
        "dependency direction, jar contents, or that a cohort is one repository rather than one release "
        "pipeline over several. The contents fidelity above is unchanged.",
    }


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--closure", type=Path, default=CLOSURE)
    parser.add_argument("--contracts", type=Path, default=CONTRACTS)
    parser.add_argument("--out", type=Path)
    args = parser.parse_args()

    closure = json.loads(args.closure.read_text())
    coordinates = closure.get("coordinates", {})

    recovered = {}
    for coord, version in coordinates.items():
        if "com.palantir.apollo" not in coord:
            continue
        name = coord.split(":", 1)[1] if ":" in coord else coord
        family, role = LEXICAL_ROLE_HINT.get(name, ("?", "unmapped"))
        recovered[coord] = {
            "retainedVersion": version,
            "lexicalCapabilityFamilyHint": family,
            "lexicalRoleHint": role,
            "interpretationEvidence": "inferred-from-artifact-name",
        }

    # Indirect-recovery check: if any recovered Foundry service method signature
    # referenced an Apollo type, that consuming service's dialogue jar - which IS
    # fetchable - would be a window onto the Apollo API shape. Count them. Zero
    # is a tiered negative for this inspected contract corpus only. It cannot prove
    # transitive depth, linkage, or absence from a different publication tier.
    apollo_type_refs = 0
    if args.contracts.is_file():
        blob = args.contracts.read_text()
        apollo_type_refs = blob.count("com/palantir/apollo") + blob.count("com.palantir.apollo")

    evidence = {
        "apiVersion": "apollo.platform.example/api-coordinates/v1alpha2",
        "kind": "ApolloApiCoordinates",
        "source": {
            "closure": str(args.closure),
            "sha256": hashlib.sha256(args.closure.read_bytes()).hexdigest(),
            "rawProvenanceIncluded": False,
            "capturedAt": closure.get("capturedAt", ""),
        },
        "fidelity": {
            "namesAndRetainedVersions": "Primary only for the bounded historical closure — exact "
            "coordinate/version strings read over the authenticated Eddie proxy.",
            "publicationAndLinkage": "Unknown — the flattened object has no dependency edges or "
            "scopes; the later trace found Apollo entries only in dependencyManagement.",
            "contents": "Unknown — the jar contents (Conjure types and routes, the wire protocol) are "
            "not recovered.",
        },
        "fetchResult": {
            "channel": "POST /artifacts/api/search/maven across every repository the tenant token can see",
            "outcome": "NOT_RESOLVED_ON_INSPECTED_TENANT_CHANNELS — exact Apollo POM/JAR probes "
            "returned 404. This does not prove publication absence outside those repositories.",
            "probed": sorted(recovered.keys()),
        },
        "coordinates": dict(sorted(recovered.items())),
        "indirectRecovery": {
            "check": "Apollo type references in the recovered Foundry contracts corpus (jar-contracts.json)",
            "referenceCount": apollo_type_refs,
            "outcome": "NOT_OBSERVED_IN_INSPECTED_CONTRACT_CORPUS — no Apollo type spelling appears "
            "in these recovered wire signatures. This does not prove transitive depth, non-linkage, or "
            "absence from another contract tier. A nonzero count would be a new lead."
            if apollo_type_refs == 0 else
            f"LEAD — {apollo_type_refs} Apollo type reference(s) found; a consuming service may expose the shape",
        },
        "releaseCohorts": release_cohorts(recovered),
        "boundary": "The closure establishes only that a historical four-seed BOM vocabulary retained "
        "these coordinate/version strings. Artifact-name roles are lexical hints. It does not establish "
        "publication, direct dependency, linkage, module decomposition, version maturity, deployment, "
        "or wire behavior, and it does not close H38 or H18.",
    }

    output = json.dumps(evidence, indent=2) + "\n"
    if args.out:
        args.out.write_text(output)
        print(f"wrote {args.out}")
    else:
        print(output)

    unmapped = [c for c, v in recovered.items() if v["lexicalCapabilityFamilyHint"] == "?"]
    print(f"apollo coordinates: {len(recovered)} | unmapped: {len(unmapped)}")
    if unmapped:
        for c in unmapped:
            print(f"  UNMAPPED: {c}")
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
