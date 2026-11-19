#!/usr/bin/env python3
"""Compare the full Environment read descriptor with its native JSON sources."""
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
PREFIX = ".conduit.environments.v1."
SOURCES = [
    "internal/controlplane/model.go",
    "internal/environments/domain/configuration.go",
    "internal/catalog/domain/release.go",
    "internal/delivery/plan/plan.go",
    "internal/controlplane/catalog_projection.go",
]
RENAME = {"Override": "MaintenanceOverride"}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def native_structs():
    structs, unsupported = {}, {}
    for source in SOURCES:
        text = (ROOT / source).read_text()
        for match in re.finditer(r"^type (\w+) struct \{([\s\S]*?)^}", text, re.M):
            fields, unknown = [], []
            for line in match[2].splitlines():
                field = re.search(r'^\s*(\w+)\s+(.+?)\s+`json:"([^"]+)"`', line)
                if field and field[1][0].isupper() and field[3] != "-":
                    fields.append((field[1], field[2], field[3].split(",")[0]))
                elif not field and re.match(r"^\s*[A-Z]\w*(?:\s|,|$)", line):
                    unknown.append(line.strip())
            require(match[1] not in structs, f"ambiguous native type {match[1]}")
            structs[match[1]] = fields
            unsupported[match[1]] = unknown
    return structs, unsupported


def descriptors(image):
    messages = {}
    def add(message, prefix):
        key = prefix + message["name"]
        messages[key] = message
        for nested in message.get("nestedType", []):
            add(nested, key + ".")
    for file in image["file"]:
        for message in file.get("messageType", []):
            add(message, "." + file["package"] + ".")
    return messages


def verify(image):
    messages = descriptors(image)
    structs, unsupported = native_structs()
    pending, visited = ["Environment"], set()
    def check_type(field, native, location, owner):
        repeated = native.startswith("[]")
        require((field.get("label") == "LABEL_REPEATED") == (repeated or native.startswith("map[")), location + ": collection cardinality drift")
        require("oneofIndex" not in field or field.get("proto3Optional") is True, location + ": independent native fields became a oneof")
        if repeated:
            native = native[2:]
        if native.startswith("map[string]"):
            require(field["type"] == "TYPE_MESSAGE", location + ": map must retain entries")
            entry = messages[field["typeName"]]
            require(entry.get("options", {}).get("mapEntry") is True, location + ": map encoding drift")
            key, value = entry["field"]
            require(key["type"] == "TYPE_STRING", location + ": map key drift")
            check_type(value, native[len("map[string]"):], location + ".value", owner)
            return
        if native.removeprefix("*") in {"string", "bool", "int"}:
            require(bool(field.get("proto3Optional")) == native.startswith("*"), location + ": scalar pointer presence drift")
        native = native.removeprefix("*")
        native = {"MaintenanceOverride": "Override"}.get(native, native)
        primitive = {"string": "TYPE_STRING", "bool": "TYPE_BOOL", "int": "TYPE_INT64"}
        if native in primitive:
            require(field["type"] == primitive[native], location + ": native scalar width/type drift")
        elif native == "time.Time":
            require(field.get("typeName") == ".google.protobuf.Timestamp", location + ": timestamp drift")
        else:
            require(native in structs, location + ": unreviewed native type " + native)
            require(field.get("typeName") == PREFIX + RENAME.get(native, native), location + ": native message drift")
            pending.append(native)

    while pending:
        native = pending.pop()
        if native in visited:
            continue
        visited.add(native)
        require(not unsupported[native], native + ": unreviewed untagged/embedded JSON field " + str(unsupported[native]))
        message = messages[PREFIX + RENAME.get(native, native)]
        fields = {field["jsonName"]: field for field in message.get("field", [])}
        expected = {"configJson" if native == "Environment" and name == "Config" else tag for name, _, tag in structs[native]}
        require(set(fields) == expected, native + ": native field coverage drift")
        for name, go_type, tag in structs[native]:
            location = native + "." + name
            if native == "Environment" and name == "Config":
                require(go_type == "map[string]any", location + ": arbitrary JSON source type drift")
                config = fields["configJson"]
                require(config["type"] == "TYPE_BYTES" and config.get("label") != "LABEL_REPEATED" and ("oneofIndex" not in config or config.get("proto3Optional") is True), location + ": arbitrary JSON must retain bytes")
            else:
                check_type(fields[tag], go_type, location, native)

    request = messages[PREFIX + "GetEnvironmentRequest"]["field"]
    require([f["name"] for f in request] == ["environment", "space_id"], "request scope drift: authority belongs to authenticated metadata")
    require(all(f["type"] == "TYPE_STRING" for f in request), "request identity type drift")
    require(all(f["label"] == "LABEL_OPTIONAL" for f in request) and not request[0].get("proto3Optional") and "oneofIndex" not in request[0], "request cardinality/presence drift")
    require(request[1].get("proto3Optional") is True, "Space selection presence drift")
    record = messages[PREFIX + "EnvironmentRecord"]["field"]
    expected_record = {"environment": "Environment", "reported": "ReportedEnvironmentState", "spoke": "SpokeLiveness"}
    require({f["name"]: f.get("typeName") for f in record} == {k: PREFIX + v for k, v in expected_record.items()}, "detail record/reused observation contract drift")
    require(all(f["label"] == "LABEL_OPTIONAL" and ("oneofIndex" not in f or f.get("proto3Optional") is True) for f in record), "record cardinality/presence drift")
    require(not unsupported["EnvironmentCatalogRecord"] and {tag: typ for _, typ, tag in structs["EnvironmentCatalogRecord"]} == {"environment": "Environment", "reported": "*EnvironmentReport", "spoke": "SpokeLiveness"}, "native record coverage/type drift")
    response = messages[PREFIX + "GetEnvironmentResponse"]["field"]
    require(len(response) == 1 and response[0]["name"] == "record" and response[0].get("typeName") == PREFIX + "EnvironmentRecord", "response scope drift")
    file = next(f for f in image["file"] if f["name"].endswith("/environment_detail.proto"))
    services = file["service"]
    require(len(services) == 1 and services[0]["name"] == "EnvironmentDetailService", "detail service scope drift")
    methods = services[0]["method"]
    require(len(methods) == 1 and methods[0]["name"] == "GetEnvironment" and methods[0].get("options", {}).get("idempotencyLevel") == "NO_SIDE_EFFECTS" and not methods[0].get("clientStreaming") and not methods[0].get("serverStreaming"), "read method authority/cardinality drift")
    print(f"PASS Environment detail contract: {len(visited)} native configuration types; request authority and reused observation types preserved")


if __name__ == "__main__":
    try:
        verify(json.loads(Path(sys.argv[1]).read_text()))
    except (ValueError, KeyError, IndexError) as error:
        raise SystemExit(str(error)) from error
