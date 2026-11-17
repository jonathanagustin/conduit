# CONDUIT-INVENTED: packaging.StructuralReceipt
# Displaced-by: An authenticated validator-producer and freshness contract integrated with existing governance.
"""Offline structural validation with byte-bound, unsigned local receipts.
Never deploys. Use trusted generated artifacts and an explicitly selected Helm.
Receipts are observations, not proof of producer identity or deployment approval.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading
import sys
import importlib.metadata
import jsonschema
import yaml

PROFILE = "schema-helm/v1"
TOOL_TIMEOUT = 20
DEPENDENCIES = {"jsonschema":"4.26.0", "PyYAML":"6.0.3", "attrs":"26.1.0", "jsonschema-specifications":"2025.9.1", "referencing":"0.37.0", "rpds-py":"2026.9.1", "typing-extensions":"4.16.0"}
HELM_VERSION = "v3.19.0+g3d8990f"
SCHEMAS = {
 "zarf-v1.0.0.json": "ffb43dc23c17491a48e081c2ea5fe763089cf746b4d2a7beb162dd82b2b9273d",
 "uds-package-v1alpha1.yaml": "57e898e5a8e66c69ac64640c4b7c0e0c8edbff0cd1333d9f632573f5bcc5bc3a",
}
EXPECTED = {
 "charts/application/Chart.yaml", "charts/application/values.yaml",
 "charts/application/templates/deployment.yaml", "charts/application/templates/service.yaml",
 "charts/uds-config/Chart.yaml", "charts/uds-config/values.yaml",
 "charts/uds-config/templates/package.yaml", "scaffold-input.json", "zarf.yaml",
}
CHECKS = ["validator-runtime", "artifact-binding", "schema-pins", "zarf-schema", "uds-structural-schema", "helm-identity",
 "helm-lint-application", "helm-render-application", "helm-lint-uds-config", "helm-render-uds-config"]

def sha(data):
    return hashlib.sha256(data).hexdigest()

def file_sha(path):
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()

def bounded_read(path, limit):
    with path.open("rb") as f:
        data = f.read(limit + 1)
    if len(data) > limit:
        raise ValueError("byte limit exceeded")
    return data

def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("duplicate JSON key")
        result[key] = value
    return result

def decode(raw):
    return json.loads(raw, object_pairs_hook=unique_object)

def checked_files(raw):
    bundle = decode(raw.decode("utf-8-sig"))
    files = bundle["files"]
    if len(files) != len(EXPECTED) or {f["path"] for f in files} != EXPECTED:
        raise ValueError("unexpected artifact set")
    if bundle["status"] != "scaffold-needs-validation":
        raise ValueError("invalid scaffold status")
    canonical = json.dumps(files, separators=(",", ":"), ensure_ascii=False).encode()
    digest = sha(canonical)
    if digest != bundle["sha256"]:
        raise ValueError("artifact digest mismatch")
    for f in files:
        if set(f) != {"path", "content"} or not isinstance(f["content"], str) or len(f["content"].encode()) > 64 * 1024:
            raise ValueError("invalid artifact")
        if "{{" in f["content"] or "}}" in f["content"]:
            raise ValueError("unsupported template expression")
        decode(f["content"])
    return files, digest

def tool(helm, args, env):
    # Official Helm with generated static charts: no plugins, cluster config,
    # shell execution or source hooks. Bound wall time and captured diagnostics.
    process = subprocess.Popen([str(helm), *args], env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    buffers = [bytearray(), bytearray()]
    overflow = threading.Event()
    capture_failed = threading.Event()
    def capture(stream, index, limit):
        try:
            while True:
                chunk = stream.read(4096)
                if not chunk:
                    break
                if len(buffers[index]) + len(chunk) > limit:
                    overflow.set()
                    try:
                        process.kill()
                    except OSError:
                        pass
                    break
                buffers[index].extend(chunk)
        except BaseException:
            # Never let a background reader silently turn partial output into PASS.
            capture_failed.set()
            try:
                process.kill()
            except OSError:
                pass
        finally:
            try:
                stream.close()
            except BaseException:
                capture_failed.set()
    readers = [threading.Thread(target=capture, args=(process.stdout, 0, 512 * 1024), daemon=True),
               threading.Thread(target=capture, args=(process.stderr, 1, 64 * 1024), daemon=True)]
    for reader in readers:
        reader.start()
    try:
        code = process.wait(timeout=TOOL_TIMEOUT)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait()
        raise
    finally:
        for reader in readers:
            reader.join(timeout=1)
    if any(reader.is_alive() for reader in readers):
        raise TimeoutError("tool output did not close")
    if capture_failed.is_set():
        raise RuntimeError("tool output capture failed")
    if overflow.is_set():
        raise ValueError("tool output limit exceeded")
    if code:
        raise subprocess.CalledProcessError(code, "helm")
    return bytes(buffers[0]).decode("utf-8")

def validate(envelope, schema_dir, helm, expected_helm_sha):
    receipt = {
        "profile": PROFILE, "producer": "local-process-unsigned", "scope": "structural-only",
        "status": "failed", "deploymentAuthorized": False,
        "observedAt": datetime.now(timezone.utc).isoformat(),
        "checks": [{"name": name, "status": "not-run"} for name in CHECKS],
    }
    active = receipt["checks"][0]
    def run(name, operation):
        nonlocal active
        active = next(c for c in receipt["checks"] if c["name"] == name)
        result = operation()
        active["status"] = "passed"
        return result
    try:
        def runtime():
            receipt["validatorSHA256"] = file_sha(Path(__file__))
            versions = {name: importlib.metadata.version(name) for name in DEPENDENCIES}
            if versions != DEPENDENCIES:
                raise ValueError("validator dependency mismatch")
            receipt["runtime"] = {"python": sys.version.split()[0], "dependencies": versions}
        run("validator-runtime", runtime)
        active = next(c for c in receipt["checks"] if c["name"] == "artifact-binding")
        raw = bounded_read(envelope, 1024 * 1024)
        receipt["inputSHA256"] = sha(raw)
        files, digest = run("artifact-binding", lambda: checked_files(raw))
        receipt["artifactSHA256"] = digest
        def schemas():
            values = {}
            for name, expected in SCHEMAS.items():
                data = bounded_read(schema_dir / name, 2 * 1024 * 1024)
                if sha(data) != expected:
                    raise ValueError("schema checksum mismatch")
                values[name] = data
            return values
        data = run("schema-pins", schemas)
        receipt["schemaSHA256"] = dict(SCHEMAS)
        zarf = decode(data["zarf-v1.0.0.json"])
        crd = yaml.safe_load(data["uds-package-v1alpha1.yaml"])
        uds = next(v["schema"]["openAPIV3Schema"] for v in crd["spec"]["versions"] if v["name"] == "v1alpha1")
        with tempfile.TemporaryDirectory(prefix="conduit-scaffold-check-") as tmp:
            root = Path(tmp)
            for f in files:
                target = root / f["path"]
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_text(f["content"], encoding="utf-8", newline="")
            run("zarf-schema", lambda: jsonschema.Draft202012Validator(zarf).validate(decode((root / "zarf.yaml").read_bytes())))
            run("uds-structural-schema", lambda: jsonschema.Draft7Validator(uds).validate(decode((root / "charts/uds-config/templates/package.yaml").read_bytes())))
            # Do not inherit personal Helm configuration, tokens or plugin paths.
            env = {k: v for k, v in os.environ.items() if k.upper() in {"PATH", "SYSTEMROOT", "WINDIR", "TEMP", "TMP"}}
            env.update(KUBECONFIG=str(root / "no-cluster-config"), HOME=str(root), HELM_PLUGINS=str(root / "no-plugins"),
                       HELM_CONFIG_HOME=str(root / "helm-config"), HELM_CACHE_HOME=str(root / "helm-cache"), HELM_DATA_HOME=str(root / "helm-data"))
            def verify_binary():
                if len(expected_helm_sha) != 64 or file_sha(helm) != expected_helm_sha:
                    raise ValueError("Helm binary digest mismatch")
            def identity():
                verify_binary()
                version = tool(helm, ["version", "--short"], env).strip()
                if version != HELM_VERSION:
                    raise ValueError("Helm version mismatch")
                receipt["helm"] = {"version": version, "sha256": expected_helm_sha}
            run("helm-identity", identity)
            for chart in ["application", "uds-config"]:
                path = str(root / "charts" / chart)
                run("helm-lint-" + chart, lambda: tool(helm, ["lint", "--strict", path], env))
                def render():
                    docs = [d for d in yaml.safe_load_all(tool(helm, ["template", "scaffold", path], env)) if d]
                    if not docs:
                        raise ValueError("empty Helm render")
                run("helm-render-" + chart, render)
            run("helm-identity", verify_binary)
        if not all(c["status"] == "passed" for c in receipt["checks"]):
            raise ValueError("incomplete validation")
        receipt["status"] = "passed"
    except Exception as exc:
        active["status"] = "failed"
        # Raw tool/schema exceptions may embed arbitrary input or local paths.
        active["reason"] = type(exc).__name__
    return receipt

def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("envelope", type=Path)
    p.add_argument("--schema-dir", type=Path, required=True)
    p.add_argument("--helm", type=Path, required=True)
    p.add_argument("--helm-sha256", required=True, help="digest established from a trusted Helm acquisition")
    a = p.parse_args()
    result = validate(a.envelope, a.schema_dir, a.helm, a.helm_sha256)
    print(json.dumps(result, separators=(",", ":")))
    return 0 if result["status"] == "passed" else 1

if __name__ == "__main__":
    raise SystemExit(main())
