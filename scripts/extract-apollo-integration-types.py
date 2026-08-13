#!/usr/bin/env python3
"""Extract the Foundry-to-Apollo integration types from the recovered type system.

The com.palantir.apollo:* backend jars are not fetchable from this tenant, but
FOUNDRY's own service jars - control-panel, marketplace, data-health,
aip-hub-broker, edge-packager - integrate with Apollo and declare typed beans
for that seam. Those jars are fetchable, and their types were harvested by
javap into the conjure type system. This pulls the Apollo-named ones out.

Fidelity: the type NAMES, FIELD NAMES, base FIELD TYPES, alias targets, and enum
values are Primary - recovered from Foundry bytecode. The catalog truncates
collection and optional wrappers (a trailing '>' marks a cut list/optional), so
a field's exact cardinality is Derived, not Primary; the full untruncated bean
is in the named fetchable jar. This is the Foundry->Apollo integration surface,
NOT Apollo's own Hub API: it shows what Foundry sends to and stores about Apollo.

    extract-apollo-integration-types.py \
        --type-system ../mcp/foundry/evidence/artifacts-registry/conjure-type-system-2026-07-20.json \
        --out docs/evidence/apollo-integration-types.json
"""

from __future__ import annotations

import argparse
import json
from collections import defaultdict
from pathlib import Path

TYPE_SYSTEM = Path(
    "../mcp/foundry/evidence/artifacts-registry/conjure-type-system-2026-07-20.json"
)

# Capability grouping by name substring, most specific first. The grouping is a
# light Platform judgment; the types themselves are recovered.
GROUPS = [
    ("space-provisioning", ("ApolloSpace", "CreateApolloSpace", "RestoreApolloPermissions", "ApolloFoundrySpace")),
    ("agent", ("ApolloAgent",)),
    ("installation", ("ApolloManagedInstallation", "ApolloUniversalInstallation", "InstallationConfiguration")),
    ("plan", ("ApolloPlan",)),
    ("notification", ("ApolloEventReceiver", "EventReceiverNotification")),
    ("publishing", ("PublishingStatus", "ApolloBlockSet", "ApolloDockerPublish", "ApolloPublishError", "ApolloArtifactUri")),
    ("labels", ("ApolloLabel", "AppliedApolloLabels", "ApolloLabels")),
    ("stack-bootstrap", ("ApolloStack", "ApolloBootstrap", "ApolloCrossStack", "ApolloGranularConfig")),
    ("environment", ("ApolloEnvironment",)),
    ("identity", ("ApolloEntityId", "ApolloSecret", "ApolloHostUri", "ApolloSpaceId")),
]


def group_of(name: str) -> str:
    for group, needles in GROUPS:
        if any(needle in name for needle in needles):
            return group
    return "other"


def render(defn: dict) -> str:
    kind = defn.get("kind")
    if kind == "object":
        fields = ", ".join(f"{f['name']}: {f['type']}" for f in defn.get("fields", []))
        return "{" + fields + "}"
    if kind == "alias":
        return f"alias<{defn.get('valueType')}>"
    if kind == "enum":
        return "enum" + json.dumps(defn.get("values", []))
    if kind == "union":
        return "union"
    return kind or "?"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--type-system", type=Path, default=TYPE_SYSTEM)
    parser.add_argument("--out", type=Path)
    args = parser.parse_args()

    document = json.loads(args.type_system.read_text())
    types = document.get("types", document)

    apollo = {}
    for name, defn in types.items():
        if not isinstance(defn, dict):
            continue
        declared = defn.get("name", name)
        if "Apollo" not in declared:
            continue
        apollo[declared] = defn

    by_group: dict[str, list[dict]] = defaultdict(list)
    by_coordinate: dict[str, int] = defaultdict(int)
    truncated = []
    for name in sorted(apollo):
        defn = apollo[name]
        rendered = render(defn)
        # A trailing '>' on any field type is a catalog-truncated collection.
        is_truncated = any(str(f.get("type", "")).endswith(">") for f in defn.get("fields", []))
        if is_truncated:
            truncated.append(name)
        by_coordinate[defn.get("coordinate", "?")] += 1
        by_group[group_of(name)].append(
            {
                "name": name,
                "kind": defn.get("kind"),
                "coordinate": defn.get("coordinate"),
                "shape": rendered,
                "wrapperTruncated": is_truncated,
            }
        )

    evidence = {
        "apiVersion": "apollo.platform.example/integration-types/v1alpha1",
        "kind": "ApolloIntegrationTypes",
        "source": {
            "typeSystem": str(args.type_system),
            "note": "javap-harvested Conjure type catalog over SHA-verified Foundry -api jars",
        },
        "fidelity": {
            "namesFieldsBaseTypes": "Primary — recovered from Foundry service bytecode.",
            "cardinality": "Derived — the catalog truncates collection/optional wrappers (trailing '>'); "
            "the full bean is in the named fetchable jar.",
            "scope": "This is the Foundry->Apollo integration surface (what Foundry sends to and stores "
            "about Apollo), NOT Apollo's own Hub API. The com.palantir.apollo:* backend jars remain "
            "unfetchable (see apollo-api-coordinates.json).",
        },
        "counts": {
            "types": len(apollo),
            "wrapperTruncated": len(truncated),
            "groups": len(by_group),
        },
        "byCoordinate": dict(sorted(by_coordinate.items())),
        "byGroup": {group: entries for group, entries in sorted(by_group.items())},
        "hypothesisLinks": {
            "space-provisioning": "H56 — enrollment Apollo-space provisioning, previously opaque ConjureBean; now typed.",
            "agent": "H18 — the Hub/Agent surface: ApolloAgentEntityInfo, ApolloAgentSecretType, ApolloHostUri.",
            "installation": "H16 — managed and universal installation configuration.",
            "notification": "docs/apollo/reference/notifications.md — ApolloEventReceiverInfo is fuller than the GraphQL {id,name}.",
        },
    }

    output = json.dumps(evidence, indent=2) + "\n"
    if args.out:
        args.out.write_text(output)
        print(f"wrote {args.out}")
    else:
        print(output)

    unmapped = [e["name"] for entries in by_group.values() for e in entries if group_of(e["name"]) == "other"]
    print(f"apollo integration types: {len(apollo)} | groups: {len(by_group)} | wrapper-truncated: {len(truncated)}")
    if by_group.get("other"):
        print(f"  ungrouped ({len(by_group['other'])}): {', '.join(e['name'] for e in by_group['other'])}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
