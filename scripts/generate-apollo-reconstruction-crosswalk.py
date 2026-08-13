#!/usr/bin/env python3
"""Turn the recovered Apollo authorization graph into a reconstruction crosswalk.

The graph is circumstantial evidence for the Apollo Hub's capability surface: its
operations, workflows, categories, and role descriptions name what the Hub does,
even though the private wire protocol is not recovered. Cross-referenced against
what platform already reconstructs (the capability map), that names concrete
reconstruction candidates.

Two tiers, kept apart:
  - the operation and family names, and the role descriptions, are RECOVERED
    (object keys and values from the live tenant, verified in
    apollo-authorization-graph.json);
  - which platform capability reconstructs a family, and what an unreconstructed
    one would need, is INFERRED - a Platform authoring judgment citing the
    capability-map row, never a claim about Apollo's private implementation.

The family-to-platform mapping is the one hand-authored part; every row cites its
platform capability or is marked UNRECONSTRUCTED. Everything else is mechanical.

    generate-apollo-reconstruction-crosswalk.py \
        --graph docs/evidence/apollo-authorization-graph.json \
        --out docs/apollo/RECONSTRUCTION-CROSSWALK.md
"""

from __future__ import annotations

import argparse
import json
from collections import defaultdict
from pathlib import Path

# Family -> (status, platform-capability citation, note). The status is one of
# RECONSTRUCTED, PARTIAL, BOUNDARY, UNRECONSTRUCTED. Citations are capability-map
# H-rows or a boundary doc. Absence was verified against the Go tree, not
# inferred: mesh-manager, webhook, notifications, build-factory, base-image, and
# template appear only in the generated role table, with no service behind them.
MAPPING: dict[str, tuple[str, str, str]] = {
    "environments": ("RECONSTRUCTED", "H1, H5, H10, H32, H33, H47",
                    "Environment identity, settings, promotion, topology, accreditation."),
    "entity": ("RECONSTRUCTED", "H25, H27, H36, H51",
               "Entity commands, removal, Helm-release adoption, unmanage handoff."),
    "bundle": ("RECONSTRUCTED", "H2, H12, H23, H24, H28",
               "Bundle catalog import, chunking, checkpoints, export selectors."),
    "imported-bundles": ("RECONSTRUCTED", "H37, H39, H42",
                        "Target-Hub versions, relayed reported-state transport, import diagnostics."),
    "label": ("RECONSTRUCTED", "H52", "Label-definition administration."),
    "namespace": ("RECONSTRUCTED", "H26", "RFC 1123 namespace lifecycle at the Flux boundary."),
    "team": ("RECONSTRUCTED", "H1", "Teams and hierarchical role grants."),
    "module": ("RECONSTRUCTED", "H6, H49, H66", "Module ENABLE/MERGE, auto-approval, opaque release persistence."),
    "permissions": ("RECONSTRUCTED", "H1, H31", "Role/permission administration and the CEL predicate library."),
    "cel-policy": ("RECONSTRUCTED", "H31", "Published Change Types and the complete CEL predicate library."),
    "hubs": ("RECONSTRUCTED", "H32", "Hub topology gating and connection settings."),
    "artifact": ("PARTIAL", "H7, H55",
                 "Registry mirror and connected-publisher admission exist; container-registry "
                 "operations (push/pull, privileged tiers) are not modelled."),
    "artifact-registry": ("PARTIAL", "H7",
                          "The registry mirror covers artifact resolution; per-registry container "
                          "assign/deleter/downloader/uploader roles and privileged tiers are not."),
    "pipelines": ("PARTIAL", "H41, H46",
                          "Export pipelines cover revisions, scheduling, and pre-build preview. Apollo's "
                          "approve-archive/create/delete/update, trigger-build, cancel-build, download, and "
                          "space/pipeline-level signing keys are a richer surface not yet modelled."),
    "transfer-target": ("PARTIAL", "H37, H39",
                        "Target-Hub versions and relayed transport exist. Apollo's create/delete, "
                        "test-connection, manage-security-settings, update-policy, and the attester role "
                        "are not modelled."),
    "terminal-profile": ("BOUNDARY", "H30",
                         "Deliberately refused. Debug terminal access is a consumer-owned Teleport broker, "
                         "not a Hub-embedded shell."),
    "egress-policy": ("PARTIAL", "H73",
                     "The Cilium network-policy adapter is adjacent, but egress-policy is a distinct Apollo "
                     "concept and its administration is not modelled; it borrows connectivityconfig:network-policy."),
    "mesh-manager": ("UNRECONSTRUCTED", "-",
                    "Service-mesh settings. Platform serves an Envoy edge (foundry-edge) but has no "
                    "mesh-manager capability. Verified absent from the Go tree."),
    "webhook": ("UNRECONSTRUCTED", "-",
               "Outbound webhooks on Hub events. No platform analogue. Verified absent."),
    "notifications": ("UNRECONSTRUCTED", "reference/notifications.md",
                     "Notification subscriptions on Hub events. No implementation yet, but the richest-"
                     "evidenced gap: recovered role hierarchy plus the Data Health event-receiver seam are "
                     "captured in a reconstruction spec. Wire protocol still needs a Hub session."),
    "build-factory": ("UNRECONSTRUCTED", "-",
                     "Build-system settings that feed bundling. No platform analogue. Verified absent."),
    "base-image": ("UNRECONSTRUCTED", "-",
                  "Base-image management for builds. No platform analogue. Verified absent."),
    "template": ("UNRECONSTRUCTED", "-",
                "Apollo product/environment templates. No platform analogue. Verified absent."),
}

# Families with a reconstruction spec grounded in a second, corroborating evidence
# channel beyond the recovered authorization vocabulary.
SPEC_FAMILIES = {"notifications": "reference/notifications.md"}


def confidence_of(status: str, family: str) -> str:
    """Evidence fidelity per docs/DEPLOYMENT-EVIDENCE.md, marked per capability.

    Existence is Primary for every family - the roles, operations, and
    descriptions are recovered from the live tenant (object keys and values).
    The reconstruction claim's fidelity varies by status.
    """
    if status == "RECONSTRUCTED":
        return "Primary (Apollo authorization) + Derived (platform capability, gated)"
    if status == "PARTIAL":
        return "Primary (authorization) + Derived (implemented base) + Unknown (gap behavior)"
    if status == "BOUNDARY":
        return "Local choice (deliberate refusal)"
    # UNRECONSTRUCTED
    if family in SPEC_FAMILIES:
        return "Primary (authorization) + Corroborating (shape, second channel) + Unknown (wire protocol)"
    return "Primary (authorization / name) + Unknown (shape and behavior)"


# Borrowed foreign families are operations Apollo grants but other products own.
# Operation families use singular or tool-specific names where their role family
# uses plural; bridge them so operations attach to the right capability.
OP_FAMILY_TO_CANONICAL = {
    "environment": "environments",
    "bundling-pipeline": "pipelines",
    "imported-bundle": "imported-bundles",
}

BORROWED = {"containers", "ccs", "connectivityconfig", "repositories", "marketplace",
            "secure-development-space-manager"}


def family_of(identifier: str) -> str:
    body = identifier[len("apollo-"):] if identifier.startswith("apollo-") else identifier
    return body.split(":", 1)[0]


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--graph", type=Path, default=Path("docs/evidence/apollo-authorization-graph.json"))
    parser.add_argument("--out", type=Path)
    args = parser.parse_args()

    graph = json.loads(args.graph.read_text())["graph"]

    # atomic operations by family
    ops_by_family: dict[str, list[str]] = defaultdict(list)
    for op in sorted(graph["operations"]):
        fam = OP_FAMILY_TO_CANONICAL.get(family_of(op), family_of(op))
        ops_by_family[fam].append(op.split(":", 1)[1])

    # one representative description per Apollo family (administrator, else any)
    desc_by_family: dict[str, str] = {}
    display_by_family: dict[str, str] = {}
    for rid, role in graph["roles"].items():
        fam = family_of(rid)
        if fam not in desc_by_family or rid.endswith(":administrator"):
            if role.get("description"):
                desc_by_family[fam] = role["description"]
            display_by_family.setdefault(fam, role.get("displayName", ""))

    # entity is a real capability (its own category and 13 operations) with no role
    # family of its own, so add it to the canonical set derived from the roles.
    apollo_families = sorted({family_of(r) for r in graph["roles"]} | {"entity"})

    status_order = {"UNRECONSTRUCTED": 0, "PARTIAL": 1, "BOUNDARY": 2, "RECONSTRUCTED": 3}
    counts: dict[str, int] = defaultdict(int)
    rows = []
    for fam in apollo_families:
        status, citation, note = MAPPING.get(fam, ("UNMAPPED", "-", "No mapping recorded."))
        counts[status] += 1
        rows.append((fam, status, citation, note))
    rows.sort(key=lambda r: (status_order.get(r[1], -1), r[0]))

    lines = []
    lines.append("# Apollo capability reconstruction crosswalk\n")
    lines.append(
        "Generated by `scripts/generate-apollo-reconstruction-crosswalk.py` from the recovered\n"
        "authorization graph. The graph is circumstantial evidence for the Apollo Hub's capability\n"
        "surface: operation names, workflow structure, and role descriptions are recovered from the\n"
        "live tenant (object keys and values, verified in `docs/evidence/apollo-authorization-graph.json`),\n"
        "while the mapping to platform capabilities and the reconstruction notes are a Platform\n"
        "authoring judgment. No claim is made about Apollo's private wire protocol.\n"
    )
    lines.append("## Summary\n")
    lines.append(
        f"- RECONSTRUCTED: {counts['RECONSTRUCTED']}\n"
        f"- PARTIAL: {counts['PARTIAL']}\n"
        f"- BOUNDARY (deliberately refused): {counts['BOUNDARY']}\n"
        f"- UNRECONSTRUCTED: {counts['UNRECONSTRUCTED']}\n"
    )
    lines.append(
        "\nThe UNRECONSTRUCTED and PARTIAL rows are the reconstruction roadmap. Each is grounded in the\n"
        "recovered operation set and role description below, which name what the capability does; the\n"
        "wire protocol still requires an authorized Apollo Hub session (H38/H18).\n"
    )

    lines.append("\n## Evidence fidelity\n")
    lines.append(
        "Each capability is marked against the confidence vocabulary in\n"
        "[`../DEPLOYMENT-EVIDENCE.md`](../DEPLOYMENT-EVIDENCE.md): **Primary** (directly observed - here the\n"
        "recovered authorization graph), **Derived** (mechanically follows from primary - a gated platform\n"
        "capability), **Corroborating** (a second channel consistent with the claim - the circumstantial\n"
        "tier, e.g. a GraphQL seam), **Unknown** (the corpus does not prove it - here the Hub wire\n"
        "protocol), and **Local choice** (a reconstruction decision, never a claim about Apollo).\n"
    )
    lines.append("\n## Crosswalk\n")
    lines.append("| Apollo family | Status | Evidence fidelity | Platform capability | Note |")
    lines.append("| --- | --- | --- | --- | --- |")
    for fam, status, citation, note in rows:
        lines.append(f"| `apollo-{fam}` | **{status}** | {confidence_of(status, fam)} | {citation} | {note} |")

    lines.append("\n## Recovered evidence per unreconstructed or partial family\n")
    for fam, status, citation, note in rows:
        if status in ("RECONSTRUCTED", "BOUNDARY"):
            continue
        ops = ops_by_family.get(fam, [])
        atomic = [o for o in ops if fam not in BORROWED]
        lines.append(f"### `apollo-{fam}` — {status}\n")
        lines.append(f"- Evidence fidelity: {confidence_of(status, fam)}")
        if desc_by_family.get(fam):
            lines.append(f"- Recovered description: *{desc_by_family[fam]}*")
        if atomic:
            lines.append(f"- Recovered operations ({len(atomic)}): {', '.join(atomic)}")
        lines.append(f"- Platform: {citation if citation != '-' else 'no reconstruction'} — {note}\n")

    output = "\n".join(lines) + "\n"
    if args.out:
        args.out.write_text(output)
        print(f"wrote {args.out}")
    else:
        print(output)

    # A family Apollo serves but this script forgot to map is a silent gap.
    unmapped = [fam for fam in apollo_families if fam not in MAPPING]
    if unmapped:
        print(f"WARNING: {len(unmapped)} unmapped families: {', '.join(unmapped)}")
        return 1
    print(
        f"families: {len(apollo_families)} | reconstructed {counts['RECONSTRUCTED']} "
        f"partial {counts['PARTIAL']} boundary {counts['BOUNDARY']} "
        f"unreconstructed {counts['UNRECONSTRUCTED']}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
