#!/usr/bin/env python3
"""Constrain the declared full index to native records, metadata and read inputs."""
import json
from pathlib import Path
import re
import sys


def require(condition, message):
    if not condition:
        raise SystemExit("Environment catalog contract drift: " + message)


def native_fields(path, name):
    source = Path(path).read_text()
    match = re.search(r"type " + re.escape(name) + r" struct \{\n(.*?)\n\}", source, re.S)
    require(match is not None, "native type missing: " + name)
    result = {}
    for line in match.group(1).splitlines():
        line = line.strip()
        if not line or line.startswith("//"):
            continue
        if line == "PageMetadata":
            require(name == "EnvironmentCatalogPage", "unexpected embedded metadata")
            result.update(native_fields("internal/controlplane/pagination.go", "PageMetadata"))
            continue
        field = re.fullmatch(r'\w+\s+(\S+)\s+`json:"([^",]+)(?:,[^"]*)?"`', line)
        require(field is not None, "unreviewed native field in " + name + ": " + line)
        require(field[2] not in result, "duplicate native field " + field[2])
        result[field[2]] = field[1]
    return result


def check(descriptor):
    files = descriptor.get("file", [])
    matches = [item for item in files if item["name"] == "conduit/environments/v1/environment_catalog.proto"]
    require(len(matches) == 1, "full index declaration missing or duplicated")
    source = matches[0]
    require(source.get("package") == "conduit.environments.v1", "native namespace changed")
    messages = {item["name"]: item for item in source.get("messageType", [])}
    require(set(messages) == {"ListEnvironmentsRequest", "ListEnvironmentsResponse"}, "unreviewed message scope")
    request = {item["name"]: item for item in messages["ListEnvironmentsRequest"].get("field", [])}
    expected_request = {"search": (1, "TYPE_STRING", False), "cursor": (2, "TYPE_STRING", False), "limit": (3, "TYPE_INT32", True), "space_id": (4, "TYPE_STRING", True)}
    require(set(request) == set(expected_request), "request must contain only full-index selectors; no authority fields")
    for name, (number, kind, optional) in expected_request.items():
        field = request[name]
        require(field["number"] == number and field["type"] == kind and field.get("label") == "LABEL_OPTIONAL", "request type/cardinality changed: " + name)
        require(bool(field.get("proto3Optional")) == optional, "request presence changed: " + name)
        require(("oneofIndex" in field) == optional, "request selectors became coupled: " + name)
    require(request["limit"]["oneofIndex"] != request["space_id"]["oneofIndex"], "limit and Space must have independent presence")
    native = native_fields("internal/controlplane/catalog_projection.go", "EnvironmentCatalogPage")
    expected_native = {"environments": "[]EnvironmentCatalogRecord", "total": "int", "pageSize": "int", "limit": "int", "nextCursor": "string", "previousCursor": "string", "hasPrevious": "bool"}
    require(native == expected_native, "native page/metadata changed; review complete field coverage")
    response = messages["ListEnvironmentsResponse"]
    expected_response = {
        "environments": (1, "TYPE_MESSAGE", "LABEL_REPEATED", ".conduit.environments.v1.EnvironmentRecord"),
        "total": (2, "TYPE_INT64", "LABEL_OPTIONAL", None),
        "pageSize": (3, "TYPE_INT32", "LABEL_OPTIONAL", None),
        "limit": (4, "TYPE_INT32", "LABEL_OPTIONAL", None),
        "nextCursor": (5, "TYPE_STRING", "LABEL_OPTIONAL", None),
        "previousCursor": (6, "TYPE_STRING", "LABEL_OPTIONAL", None),
        "hasPrevious": (7, "TYPE_BOOL", "LABEL_OPTIONAL", None),
    }
    fields = {item["jsonName"]: item for item in response.get("field", [])}
    require(set(fields) == set(native), "response does not cover the complete native page")
    for name, (number, kind, label, message) in expected_response.items():
        field = fields[name]
        require((field["number"], field["type"], field["label"], field.get("typeName")) == (number, kind, label, message), "response type/cardinality changed: " + name)
        require("oneofIndex" not in field and not field.get("proto3Optional"), "page fields became mutually exclusive or gained scalar presence")
    services = source.get("service", [])
    require(len(services) == 1 and services[0]["name"] == "EnvironmentCatalogService", "unreviewed service scope")
    methods = services[0].get("method", [])
    require(len(methods) == 1, "unreviewed method scope")
    method = methods[0]
    require(method["name"] == "ListEnvironments" and method["inputType"] == ".conduit.environments.v1.ListEnvironmentsRequest" and method["outputType"] == ".conduit.environments.v1.ListEnvironmentsResponse", "full-index request/response binding changed")
    require(not method.get("clientStreaming") and not method.get("serverStreaming") and method.get("options", {}).get("idempotencyLevel") == "NO_SIDE_EFFECTS", "index must remain a unary side-effect-free read")
    pagination = Path("internal/controlplane/pagination.go").read_text()
    match = re.search(r"^[ \t]*fullCatalogPageLimit[ \t]*=[ \t]*(\d+)[ \t]*(?://[^\r\n]*)?$", pagination, re.M)
    require(match is not None, "native full-list bound unavailable")
    limit = int(match[1])
    require(1 <= limit <= 2**31 - 1, "native full-list bound exceeds wire width")
    proto = Path("proto/conduit/environments/v1/environment_catalog.proto").read_text()
    require(f"default of {limit}; present must be 1 through {limit}." in proto, "declared full-list default/bound is stale")
    return limit


if __name__ == "__main__":
    bound = check(json.loads(Path(sys.argv[1]).read_text()))
    print(f"PASS Environment catalog contract: complete native page, full-record reuse, independent input presence; native limit {bound}")
