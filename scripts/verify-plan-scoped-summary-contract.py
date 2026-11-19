#!/usr/bin/env python3
"""Constrain the separate scoped Plan-summary capability and native bounds."""
import json
from pathlib import Path
import re
import sys


def require(condition, message):
    if not condition:
        raise SystemExit("Scoped Plan summary contract drift: " + message)


def check(descriptor):
    files = descriptor.get("file", [])
    matches = [item for item in files if item["name"] == "conduit/plans/v1/plan_scoped_summary.proto"]
    require(len(matches) == 1, "scoped declaration missing or duplicated")
    source = matches[0]
    require(source.get("package") == "conduit.plans.v1", "native namespace changed")
    messages = {item["name"]: item for item in source.get("messageType", [])}
    require(set(messages) == {"ListPlanSummariesInSpaceRequest", "ListPlanSummariesInSpaceResponse"}, "unreviewed message scope")
    expected = {
        "space_id": (1, "TYPE_STRING", False),
        "limit": (2, "TYPE_INT32", True),
        "cursor": (3, "TYPE_STRING", False),
        "environment": (4, "TYPE_STRING", False),
        "status": (5, "TYPE_STRING", False),
        "installation_id": (6, "TYPE_STRING", False),
        "search": (7, "TYPE_STRING", False),
    }
    request = messages["ListPlanSummariesInSpaceRequest"]
    fields = {item["name"]: item for item in request.get("field", [])}
    require(set(fields) == set(expected), "request must contain only native selectors; no authority fields")
    require(len(request.get("oneofDecl", [])) == 1, "limit presence must be independent")
    for name, (number, kind, optional) in expected.items():
        field = fields[name]
        require(field.get("number") == number and field.get("type") == kind and field.get("label") == "LABEL_OPTIONAL", "request type/cardinality changed: " + name)
        require(field.get("proto3Optional", False) == optional, "request presence changed: " + name)
        require(("oneofIndex" in field) == optional, "selector presence coupled: " + name)
    response = messages["ListPlanSummariesInSpaceResponse"]
    fields = {item["name"]: item for item in response.get("field", [])}
    require(set(fields) == {"space_id", "page"} and not response.get("oneofDecl"), "selection echo and page must be independent")
    for name, number, kind in [("space_id", 1, "TYPE_STRING"), ("page", 2, "TYPE_MESSAGE")]:
        field = fields[name]
        require(field.get("number") == number and field.get("type") == kind and field.get("label") == "LABEL_OPTIONAL" and "oneofIndex" not in field, "response field changed: " + name)
    require(fields["page"].get("typeName") == ".conduit.plans.v1.ListPlanSummariesResponse", "must reuse the native summary page")
    services = source.get("service", [])
    require(len(services) == 1 and services[0]["name"] == "PlanScopedReadService", "separate capability service changed")
    methods = services[0].get("method", [])
    require(len(methods) == 1, "unreviewed method scope")
    method = methods[0]
    require(method["name"] == "ListPlanSummariesInSpace" and method["inputType"] == ".conduit.plans.v1.ListPlanSummariesInSpaceRequest" and method["outputType"] == ".conduit.plans.v1.ListPlanSummariesInSpaceResponse", "scoped method binding changed")
    require(not method.get("clientStreaming") and not method.get("serverStreaming"), "method must remain unary")
    require(method.get("options", {}).get("idempotencyLevel") == "NO_SIDE_EFFECTS", "method must remain read-only")
    native = Path("internal/controlplane/pagination.go").read_text()
    bounds = []
    for name in ["defaultPageLimit", "maxPageLimit"]:
        match = re.search(r"^\s*" + name + r"\s*=\s*([0-9]+)\s*$", native, re.M)
        require(match is not None, "native summary bound must be a reviewed literal: " + name)
        bounds.append(int(match[1]))
    require(1 <= bounds[0] <= bounds[1] <= 2147483647, "native summary bounds do not fit wire values")
    proto = Path("proto/conduit/plans/v1/plan_scoped_summary.proto").read_text()
    require(f"default {bounds[0]}; present must be 1 through {bounds[1]}." in proto, "declared native summary bounds are stale")


if __name__ == "__main__":
    check(json.loads(Path(sys.argv[1]).read_text()))
    print("PASS scoped Plan summary contract: separate read-only unary capability, independent selectors, native page reuse and bounds")
