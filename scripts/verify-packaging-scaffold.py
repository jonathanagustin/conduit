"""Offline schema + Helm render check; never installs or contacts a cluster.
Requires Python jsonschema/PyYAML and explicitly supplied local schema cache/Helm.
Use trusted generated input only; the envelope digest is NOT authenticity.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import jsonschema
import yaml

p = argparse.ArgumentParser()
p.add_argument("envelope", type=Path)
p.add_argument("--schema-dir", type=Path, required=True)
p.add_argument("--helm", type=Path, required=True)
a = p.parse_args()

def schema_file(name, digest):
    raw = (a.schema_dir / name).read_bytes()
    if hashlib.sha256(raw).hexdigest() != digest:
        raise ValueError("schema checksum mismatch: " + name)
    return raw

zarf = json.loads(schema_file("zarf-v0.65.1.json", "7da2226c9c5deeed754eacd8c481f818a020b21a197ce3564705abd258b956f6"))
crd = yaml.safe_load(schema_file("uds-package-v1alpha1.yaml", "57e898e5a8e66c69ac64640c4b7c0e0c8edbff0cd1333d9f632573f5bcc5bc3a"))
uds = next(v["schema"]["openAPIV3Schema"] for v in crd["spec"]["versions"] if v["name"] == "v1alpha1")
bundle = json.loads(a.envelope.read_text(encoding="utf-8-sig"))
expected = {
 "charts/application/Chart.yaml", "charts/application/values.yaml",
 "charts/application/templates/deployment.yaml", "charts/application/templates/service.yaml",
 "charts/uds-config/Chart.yaml", "charts/uds-config/values.yaml",
 "charts/uds-config/templates/package.yaml", "scaffold-input.json", "zarf.yaml",
}
files = bundle["files"]
if len(files) != len(expected) or {f["path"] for f in files} != expected:
    raise ValueError("unexpected artifact set")
if bundle["status"] != "scaffold-needs-validation":
    raise ValueError("invalid scaffold status")
# Contract uses canonical Go JSON; current profile permits only ASCII literals.
canonical = json.dumps(files, separators=(",", ":"), ensure_ascii=False).encode()
if hashlib.sha256(canonical).hexdigest() != bundle["sha256"]:
    raise ValueError("artifact digest mismatch")
with tempfile.TemporaryDirectory(prefix="conduit-scaffold-check-") as tmp:
    root = Path(tmp)
    for f in files:
        if "{{" in f["content"] or "}}" in f["content"]:
            raise ValueError("this profile never emits Helm template expressions")
        json.loads(f["content"])
        target = root / f["path"]
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(f["content"], encoding="utf-8", newline="")
    jsonschema.Draft202012Validator(zarf).validate(json.loads((root / "zarf.yaml").read_text()))
    package = json.loads((root / "charts/uds-config/templates/package.yaml").read_text())
    # Kubernetes structural CRDs use OpenAPI extensions; this only checks their
    # JSON-schema-compatible constraints, not admission/defaulting/operator behavior.
    jsonschema.Draft7Validator(uds).validate(package)
    env = dict(os.environ, KUBECONFIG=str(root / "no-cluster-config"))
    version = subprocess.check_output([str(a.helm), "version", "--short"], text=True, env=env).strip()
    if not version.startswith("v3.19.0+"):
        raise ValueError("expected Helm v3.19.0")
    for chart in ["application", "uds-config"]:
        path = str(root / "charts" / chart)
        subprocess.run([str(a.helm), "lint", "--strict", path], check=True, env=env)
        rendered = subprocess.check_output([str(a.helm), "template", "scaffold", path], text=True, env=env)
        docs = [d for d in yaml.safe_load_all(rendered) if d]
        if not docs:
            raise ValueError("empty Helm render")
print("PASS pinned Zarf schema, UDS structural constraints, Helm 3.19.0 lint/render; not live validation")
