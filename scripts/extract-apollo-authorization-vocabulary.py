#!/usr/bin/env python3
"""Extract the Apollo authorization vocabulary from a captured multipass roles response.

Per AGENTS.md 0a this vocabulary is never hand-authored: the role, operation,
workflow, and category identifiers are the object keys of a captured
/multipass/api/roles/v2/ response, so they are recovered mechanically and
anchored to the source bytes.

The captures are shape captures. Every *value* is a type placeholder because the
recovery pipeline scrubs them ("shape inferred; values scrubbed; not an authored
schema"). Identifiers are therefore recovered exactly, while the role to
operation, workflow to operation, and category to workflow edges are NOT
recovered and are emitted as explicit unknowns rather than inferred.

Usage:
    extract-apollo-authorization-vocabulary.py <capture.json> [-o out.json]
"""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
from pathlib import Path

API_VERSION = "apollo.platform.example/authorization-vocabulary/v1alpha1"
KIND = "ApolloAuthorizationVocabulary"
PREFIX = "apollo-"

# Maps whose keys are identifiers, by the endpoint that returns them.
CONFIG_MAPS = ("templateRoles", "categories", "workflows", "operations")
ROLE_MAPS = ("roles",)


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1 << 20), b""):
            digest.update(chunk)
    return "sha256:" + digest.hexdigest()


def max_dict_width(node: object) -> int:
    """Widest dict in the document, used to expose capture truncation."""
    if isinstance(node, dict):
        return max([len(node)] + [max_dict_width(v) for v in node.values()] or [0])
    if isinstance(node, list):
        return max([0] + [max_dict_width(v) for v in node])
    return 0


def truncation_suspect(node: object) -> dict[int, int]:
    """Histogram of dict widths at or above 60, to expose a fixed capture cap."""
    counts: dict[int, int] = {}

    def walk(value: object) -> None:
        if isinstance(value, dict):
            if len(value) >= 60:
                counts[len(value)] = counts.get(len(value), 0) + 1
            for item in value.values():
                walk(item)
        elif isinstance(value, list):
            for item in value:
                walk(item)

    walk(node)
    return counts


def iter_recipes(document: object):
    """Yield (method, path, resShape) for every recipe in the capture."""
    if isinstance(document, list):
        for entry in document:
            if isinstance(entry, dict) and "path" in entry:
                yield entry.get("method", "GET"), entry["path"], entry.get("resShape")
    elif isinstance(document, dict):
        for entry in document.get("records", []):
            if isinstance(entry, dict) and "path" in entry:
                yield entry.get("method", "GET"), entry["path"], entry.get("resShape")


def collect(document: object):
    """Collect prefixed identifiers per map, per contributing endpoint."""
    per_endpoint: list[dict] = []
    union: dict[str, set[str]] = {name: set() for name in CONFIG_MAPS + ROLE_MAPS}
    adjacent: set[str] = set()

    for method, path, shape in iter_recipes(document):
        if "/roles/v2/" not in path or not isinstance(shape, dict):
            continue
        contributed: dict[str, dict[str, int]] = {}
        for name in CONFIG_MAPS + ROLE_MAPS:
            node = shape.get(name)
            if not isinstance(node, dict):
                continue
            apollo = sorted(k for k in node if k.startswith(PREFIX))
            # Identifiers owned by another product that nonetheless act on
            # Apollo, e.g. enrollment connection administration. They are part
            # of the boundary and must not be silently dropped, but they are
            # not Apollo-owned vocabulary either.
            adjacent.update(
                k for k in node if "apollo" in k.lower() and not k.startswith(PREFIX)
            )
            if not apollo:
                continue
            union[name].update(apollo)
            contributed[name] = {"total": len(node), "apollo": len(apollo)}
        if contributed:
            per_endpoint.append(
                {"method": method, "path": path, "maps": contributed}
            )

    return (
        {name: sorted(values) for name, values in union.items()},
        per_endpoint,
        sorted(adjacent),
    )


def family_of(identifier: str) -> str:
    """Resource family, e.g. apollo-environments:administrator -> environments."""
    body = identifier[len(PREFIX):]
    return body.split(":", 1)[0] if ":" in body else body


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("capture", type=Path)
    parser.add_argument("-o", "--output", type=Path)
    args = parser.parse_args()

    if not args.capture.is_file():
        print(f"capture not found: {args.capture}", file=sys.stderr)
        return 2

    document = json.loads(args.capture.read_text())
    union, per_endpoint, adjacent = collect(document)

    if not any(union.values()):
        print("no apollo identifiers found in any /roles/v2/ response", file=sys.stderr)
        return 1

    # Roles appear both as config.templateRoles and as all[-including-deleted].roles.
    roles = sorted(set(union["templateRoles"]) | set(union["roles"]))
    families = sorted({family_of(r) for r in roles})

    widths = truncation_suspect(document)
    widest = max_dict_width(document)
    # A capture cap is a width that repeats across many distinct maps AND that
    # nothing in the document exceeds. A merely popular width (a genuine
    # repeated catalog) is not a cap, so it must also be the document maximum.
    capped = [w for w, n in widths.items() if n >= 5 and w == widest]

    evidence = {
        "apiVersion": API_VERSION,
        "kind": KIND,
        "source": {
            "path": str(args.capture),
            "sha256": sha256_file(args.capture),
            "bytes": args.capture.stat().st_size,
        },
        "captureIntegrity": {
            "maxDictWidth": widest,
            "repeatedWidthsAtOrAbove60": {str(k): v for k, v in sorted(widths.items())},
            "suspectedTruncationWidths": sorted(capped),
            "truncated": bool(capped),
            "note": (
                "A width repeated across many distinct maps indicates a capture cap, "
                "not a server response size. A truncated capture cannot support any "
                "claim that an identifier is absent."
            ),
        },
        "contributingEndpoints": per_endpoint,
        "vocabulary": {
            "roles": roles,
            "operations": union["operations"],
            "workflows": union["workflows"],
            "categories": union["categories"],
        },
        "counts": {
            "roles": len(roles),
            "operations": len(union["operations"]),
            "workflows": len(union["workflows"]),
            "categories": len(union["categories"]),
            "resourceFamilies": len(families),
        },
        "resourceFamilies": families,
        "adjacentForeignIdentifiers": adjacent,
        "recovered": [
            "Exact identifier spelling for roles, operations, workflows, and categories.",
            "Resource-family partition derived from the identifier grammar.",
            "Foreign identifiers that act on Apollo, listed separately.",
        ],
        "notRecovered": [
            "role -> operations edges (captured as a scrubbed array placeholder).",
            "role -> canAssigns edges (scrubbed).",
            "role -> trackedWorkflows edges (scrubbed).",
            "workflow -> operations edges (scrubbed).",
            "category -> workflows edges (scrubbed).",
            "displayName and description text for every identifier (scrubbed).",
            "Whether this tenant's role set equals the Apollo product default set.",
        ],
        "boundary": (
            "This is a tenant-observed identifier vocabulary. It does not establish "
            "Apollo authorization semantics, private Hub behavior, or that a Platform "
            "role granting the same identifier grants the same authority."
        ),
    }

    output = json.dumps(evidence, indent=2, sort_keys=False) + "\n"
    if args.output:
        args.output.write_text(output)
        print(f"wrote {args.output}", file=sys.stderr)
    else:
        sys.stdout.write(output)

    summary = evidence["counts"]
    print(
        "roles={roles} operations={operations} workflows={workflows} "
        "categories={categories} families={resourceFamilies}".format(**summary),
        file=sys.stderr,
    )
    if evidence["captureIntegrity"]["truncated"]:
        print(
            "WARNING: source capture appears truncated at widths "
            f"{evidence['captureIntegrity']['suspectedTruncationWidths']}; "
            "absence claims are unsound",
            file=sys.stderr,
        )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
