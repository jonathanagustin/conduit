#!/usr/bin/env python3
"""Replace two real Hub images while a production Agent renews in-flight work."""
import argparse
from datetime import datetime, timezone
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import threading
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools/upgrade-wire"))
import wire
from rehearse import compare_snapshots
from invariants import inspect, SNAPSHOT_SQL
from proxy import Passthrough


def api_and_chart(f, work):
    env = wire.runtime_environment(work)
    api = "conduit-rolling-api-" + work.name.rsplit("-", 1)[-1]
    wire.command("docker", "run", "--detach", "--name", api, "--label", "conduit.rehearsal=rolling", "--cap-drop", "ALL", "--publish", "127.0.0.1::6443", "--entrypoint", "/bin/k3s", wire.K3S,
                 "server", "--disable-agent", "--egress-selector-mode=disabled", "--disable=traefik,servicelb,metrics-server,local-storage,coredns")
    f.containers.append(api)
    config = work / "kubeconfig"
    def ready():
        copied = subprocess.run(["docker", "cp", api + ":/etc/rancher/k3s/k3s.yaml", str(config)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if copied.returncode:
            return False
        config.chmod(0o600)
        config.write_text(config.read_text().replace("https://127.0.0.1:6443", "https://" + wire.listener(api, 6443)))
        env["KUBECONFIG"] = str(config)
        return subprocess.run(["kubectl", "get", "--raw", "/readyz"], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0
    wire.wait_for(ready, "Kubernetes API", 120)
    registry = wire.listener(f.container("registry", wire.REGISTRY, "--publish", "127.0.0.1::5000"), 5000)
    def registry_ready():
        try:
            with urllib.request.urlopen("http://" + registry + "/v2/", timeout=2) as response:
                return response.status == 200
        except urllib.error.URLError:
            return False
    wire.wait_for(registry_ready, "OCI registry")
    wire.command("helm", "package", str(ROOT / "testdata/e2e-chart"), "--destination", str(work), env=env)
    pushed = subprocess.run(["helm", "push", str(work / "platform-e2e-0.1.0.tgz"), "oci://" + registry, "--plain-http"], env=env, capture_output=True, text=True, check=True)
    match = re.search(r"Digest:\s+(sha256:[0-9a-f]{64})", pushed.stdout + pushed.stderr)
    if not match:
        raise RuntimeError("missing registry chart digest")
    return env, registry, match[1]


def build_probe(work, label):
    source = work / label
    target = source / "tools/rolling-fixture"
    target.mkdir(parents=True)
    shutil.copyfile(ROOT / "tools/upgrade-rolling/testdata/probe/main.go", target / "main.go")
    binary = work / (label + "-probe")
    env = dict(os.environ, CGO_ENABLED="0")
    subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", "-o", str(binary), "./tools/rolling-fixture"], cwd=source, env=env, check=True)
    return binary


def fixture_image(work, label, binary):
    tag = "conduit-rolling-fixture:" + label + "-" + work.name.rsplit("-", 1)[-1]
    # Import an empty-base rootfs with only the real binary. No build frontend,
    # network access or accumulating BuildKit cache is needed for this fixture.
    with tempfile.TemporaryFile() as rootfs:
        with tarfile.open(fileobj=rootfs, mode="w") as archive:
            entry = tarfile.TarInfo("hub")
            entry.mode, entry.size = 0o755, binary.stat().st_size
            with binary.open("rb") as reader:
                archive.addfile(entry, reader)
        rootfs.seek(0)
        subprocess.run(["docker", "import", "--change", 'ENTRYPOINT ["/hub"]', "--change", "LABEL conduit.rehearsal=rolling", "-", tag], stdin=rootfs, stdout=subprocess.DEVNULL, check=True)
    return tag, wire.command("docker", "image", "inspect", tag, "--format", "{{.Id}}")


def materials_archive(materials):
    archive = io.BytesIO()
    with tarfile.open(fileobj=archive, mode="w") as writer:
        directory = tarfile.TarInfo("fixture")
        directory.type, directory.mode = tarfile.DIRTYPE, 0o700
        writer.addfile(directory)
        for path in sorted(materials.iterdir()):
            data = path.read_bytes()
            entry = tarfile.TarInfo("fixture/" + path.name)
            entry.mode, entry.size = 0o600, len(data)
            # TarInfo defaults uid/gid to container root, independently of the
            # host UID which docker cp directory archives otherwise preserve.
            writer.addfile(entry, io.BytesIO(data))
    return archive.getvalue()


def start_hub(f, work, name, network, image, materials):
    # Copy only needed fixture credentials into the container's own layer.
    # This avoids rootful/rootless bind-mount UID differences for 0600 files.
    wire.command("docker", "create", "--name", name, "--label", "conduit.rehearsal=rolling", "--cap-drop", "ALL", "--network", network, "--publish", "127.0.0.1::8081",
                 "--env", "LISTEN_ADDRESS=:8081", "--env", "REPOSITORY_KIND=postgres", "--env", "POSTGRES_DSN_FILE=/fixture/postgres.dsn",
                 "--env", "PLATFORM_BOOTSTRAP_ADMINISTRATORS=wire-operator", "--env", "AGENT_CLIENT_CA_FILE=/fixture/ca.crt",
                 "--env", "TLS_CERT_FILE=/fixture/hub.crt", "--env", "TLS_KEY_FILE=/fixture/hub.key", "--env", "BUNDLE_SIGNING_KEY_FILE=/fixture/bundle.key", image)
    f.containers.append(name)
    subprocess.run(["docker", "cp", "-", name + ":/"], input=materials_archive(materials), stdout=subprocess.DEVNULL, check=True)
    started = time.monotonic()
    wire.command("docker", "start", name)
    address = wire.listener(name, 8081)
    api = wire.API(address, work, "operator")
    wire.wait_for(api.ready, "Hub replica " + name, 120)
    return address, int((time.monotonic() - started) * 1000)


def sql(database, query):
    return json.loads(wire.command("docker", "exec", database, "psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-U", "conduit", "-d", "rolling_fixture", "-c", query))


def metadata_counts(database):
    return sql(database, """SELECT json_build_object(
        'metadataSnapshotRows', (SELECT count(*) FROM conduit_repository_state),
        'migrations', (SELECT json_agg(json_build_object('component',component,'name',name) ORDER BY component,name) FROM conduit_schema_migrations)
    )""")


def parsed_time(value):
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def live_lease(record, identity=None, previous_expiry=None, now=None):
    lease = record.get("lease") or {}
    actual = (lease.get("agentId"), lease.get("attempt"), lease.get("issuedAt"))
    if record["status"] != "RUNNING" or actual[0] != "wire-agent" or not all(actual):
        raise RuntimeError("in-flight Plan lost its running lease")
    expires = parsed_time(lease["expiresAt"])
    if (identity is not None and actual != identity) or expires <= (now or datetime.now(timezone.utc)):
        raise RuntimeError("lease identity changed or expired")
    if previous_expiry and expires < previous_expiry:
        raise RuntimeError("lease expiry moved backwards")
    return actual, expires


class Sampler:
    def __init__(self, api, plan_id, database, initial):
        self.api, self.plan_id, self.database, self.initial = api, plan_id, database, initial
        self.samples, self.errors = [], []
        self.stop = threading.Event()
        self.thread = threading.Thread(target=self.run, daemon=True)
        self.identity = None

    def run(self):
        try:
            previous_expiry = None
            while not self.stop.is_set():
                record = self.api.call("/v1/plans/" + self.plan_id)
                identity, previous_expiry = live_lease(record, self.identity, previous_expiry)
                if self.identity is None:
                    self.identity = identity
                boundary = inspect(sql(self.database, SNAPSHOT_SQL), self.initial)
                self.samples.append({"agentId": identity[0], "attempt": identity[1], "issuedAt": identity[2], "expiresAt": record["lease"]["expiresAt"], "decisions": boundary["decisions"], "proposals": len(boundary["proposalIDs"])})
                self.stop.wait(0.5)
        except Exception as error:
            self.errors.append(str(error))

    def check(self):
        if self.errors:
            raise RuntimeError("live rollout sample: " + self.errors[0])

    def close(self):
        self.stop.set()
        self.thread.join(timeout=15)
        if self.thread.is_alive():
            raise RuntimeError("rollout sampler did not stop")
        self.check()


def exercise(f, work, new, old_probe, new_probe, network, images):
    env, registry, chart_digest = api_and_chart(f, work)
    environment = "rolling-upgrade"
    wire.command("kubectl", "create", "namespace", environment, env=env)
    wire.tls_material(work, environment)
    database = f.container("database", wire.POSTGRES, "--network", network, "--env", "POSTGRES_USER=conduit", "--env", "POSTGRES_PASSWORD=rolling_fixture", "--env", "POSTGRES_DB=rolling_fixture", "--publish", "127.0.0.1::5432")
    streak = 0
    def database_ready():
        nonlocal streak
        ready = subprocess.run(["docker", "exec", database, "pg_isready", "-U", "conduit", "-d", "rolling_fixture"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0
        streak = streak + 1 if ready else 0
        return streak >= 3
    wire.wait_for(database_ready, "PostgreSQL steady readiness")
    probe_env = dict(os.environ, CONDUIT_UPGRADE_DSN="postgres://conduit:rolling_fixture@" + wire.listener(database, 5432) + "/rolling_fixture?sslmode=disable",
                     CONDUIT_UPGRADE_FOUNDATION=str(work / "previous/examples/example-foundation.json"))
    print("Seeding old constructors: 500 Teams, 1000 audits, 100 valid pending Plans", flush=True)
    expected = json.loads(wire.command(str(old_probe), "seed", env=probe_env))
    materials = work / "hub-material"
    materials.mkdir(mode=0o700)
    for name in ("ca.crt", "hub.crt", "hub.key", "bundle.key"):
        shutil.copyfile(work / name, materials / name)
        (materials / name).chmod(0o600)
    (materials / "postgres.dsn").write_text("postgres://conduit:rolling_fixture@" + database + ":5432/rolling_fixture?sslmode=disable")
    (materials / "postgres.dsn").chmod(0o600)
    addresses, startup = {}, []
    for index in range(2):
        name = "rolling-old-" + str(index) + "-" + work.name.rsplit("-", 1)[-1]
        address, elapsed = start_hub(f, work, name, network, images["previous"][0], materials)
        addresses[name] = ("127.0.0.1", int(address.rsplit(":", 1)[1]))
        startup.append({"replica": name, "source": "previous", "readyMilliseconds": elapsed})
    def populated_boundary():
        snapshot = inspect(sql(database, SNAPSHOT_SQL))
        return snapshot if snapshot["decisions"] >= 100 else False
    initial = wire.wait_for(populated_boundary, "populated reconciliation decisions", 120)
    previous_physical = metadata_counts(database)
    proxy = Passthrough(addresses.items())
    sampler = None
    try:
        address = proxy.address[0] + ":" + str(proxy.address[1])
        operator, reviewer = wire.API(address, work, "operator"), wire.API(address, work, "reviewer")
        wire.configure_review(operator, environment)
        # Delay only the real mutating Helm call. No result is fabricated: the
        # wrapper execs the selected Helm binary after the barrier is released.
        barrier = work / "helm-arrived"
        permit = work / "helm-permit"
        wrappers = work / "wrappers"
        wrappers.mkdir()
        real_helm = shutil.which("helm")
        (wrappers / "helm").write_text("#!/usr/bin/env python3\nimport os,sys,time\nfrom pathlib import Path\n" +
            "if sys.argv[1:2] == ['upgrade']:\n" +
            " Path(" + repr(str(barrier)) + ").touch()\n end=time.monotonic()+150\n" +
            " while not Path(" + repr(str(permit)) + ").exists():\n  if time.monotonic()>end: sys.exit(124)\n  time.sleep(0.1)\n" +
            "os.execv(" + repr(real_helm) + ",[" + repr(real_helm) + "]+sys.argv[1:])\n")
        (wrappers / "helm").chmod(0o755)
        agent_env = wire.runtime_environment(work / "agent-state")
        agent_env.update(KUBECONFIG=env["KUBECONFIG"], PATH=str(wrappers) + os.pathsep + agent_env["PATH"],
                         CONTROL_PLANE_URL=operator.url, ENVIRONMENT_ID=environment, AGENT_ID="wire-agent", POLL_INTERVAL="500ms", LEASE_DURATION="20s",
                         HELM_TIMEOUT="120s", HEALTH_POLL_INTERVAL="5s", HELM_PLAIN_HTTP="true", CONTROL_PLANE_CA_FILE=str(work / "ca.crt"),
                         CONTROL_PLANE_CLIENT_CERT_FILE=str(work / "agent.crt"), CONTROL_PLANE_CLIENT_KEY_FILE=str(work / "agent.key"))
        agent = f.start("rolling-agent", new["platform-spoke-agent"], agent_env)
        value = "real-helm-after-two-replica-roll"
        plan = operator.call("/v1/plans", wire.desired(environment, registry, chart_digest, value))
        if plan["status"] != "AWAITING_APPROVAL" or plan["requiredApprovals"] < 1:
            raise RuntimeError("rolling Plan was not held for explicit review")
        reviewer.call("/v1/plans/" + plan["id"] + "/approvals", {"rationale": "isolated live upgrade rehearsal"})
        wire.wait_for(barrier.exists, "real Helm execution barrier", 60)
        held_since = time.monotonic()
        for backend in addresses.values():
            record = wire.API(backend[0] + ":" + str(backend[1]), work, "operator").call("/v1/plans/" + plan["id"])
            if record["status"] != "RUNNING" or record.get("lease", {}).get("agentId") != "wire-agent":
                raise RuntimeError("both old replicas did not serve the same in-flight Plan")
        sampler = Sampler(operator, plan["id"], database, initial)
        sampler.thread.start()
        wire.wait_for(lambda: sampler.samples or (sampler.check() and False), "first live lease sample", 20)
        old_names = list(addresses)
        for index, old_name in enumerate(old_names):
            sampler.check()
            del addresses[old_name]
            proxy.set_backends(addresses.items())
            wire.command("docker", "stop", "--timeout", "15", old_name)
            name = "rolling-new-" + str(index) + "-" + work.name.rsplit("-", 1)[-1]
            print("Replacing replica " + str(index) + " with current image while lease renews", flush=True)
            address, elapsed = start_hub(f, work, name, network, images["current"][0], materials)
            addresses[name] = ("127.0.0.1", int(address.rsplit(":", 1)[1]))
            startup.append({"replica": name, "source": "current", "readyMilliseconds": elapsed})
            proxy.set_backends(addresses.items())
            # Witness new connections to each remaining serving backend.
            for _ in addresses:
                if operator.call("/v1/plans/" + plan["id"])["status"] != "RUNNING":
                    raise RuntimeError("Plan stopped running during image replacement")
            sampler.check()
        while time.monotonic() - held_since < 45:
            sampler.check()
            time.sleep(0.25)
        sampler.close()
        renewals = len({sample["expiresAt"] for sample in sampler.samples}) - 1
        if renewals < 3:
            raise RuntimeError("fewer than three real lease renewals observed")
        connections = proxy.counts()
        if any(connections.get(row["replica"], 0) < 1 for row in startup):
            raise RuntimeError("not every old/new replica served proxy traffic")
        held_seconds = round(time.monotonic() - held_since, 3)
        permit.touch()
        def completed():
            if agent.poll() is not None:
                raise RuntimeError("production Agent exited")
            record = operator.call("/v1/plans/" + plan["id"])
            if record["status"] in ("FAILED", "CANCELLED"):
                raise RuntimeError("rolling Plan terminated: " + record["status"] + " " + record.get("failure", ""))
            return record if record["status"] == "SUCCEEDED" else False
        terminal = wire.wait_for(completed, "real Helm completion after rolling", 90)
        configmap = json.loads(wire.command("kubectl", "get", "configmap", "platform-e2e", "-n", environment, "-o", "json", env=env))
        if configmap["data"]["value"] != value:
            raise RuntimeError("post-roll real Helm value mismatch")
        def exact_report():
            report = operator.call("/v1/environments/" + environment + "/reported-state")
            try:
                wire.assert_report(report, terminal, environment)
                return report
            except RuntimeError:
                return False
        report = wire.wait_for(exact_report, "post-roll exact report", 30)
        agent.terminate()
        agent.wait(timeout=10)
        final = inspect(sql(database, SNAPSHOT_SQL), initial)
        # Interrogate the live migrated database before a verifier constructor
        # could repair missing audit schema or adopt unprocessed legacy data.
        audit_rows = sql(database, "SELECT to_json(count(*)) FROM conduit_authorization_audit_events")
        if audit_rows < 1000:
            raise RuntimeError("legacy audit adoption did not complete in serving replicas")
        actual = json.loads(wire.command(str(new_probe), "read", env=probe_env))
        compare_snapshots(expected, actual)
        return {"planID": plan["id"], "status": terminal["status"], "report": report, "chartDigest": chart_digest,
                "startup": startup, "proxyConnections": connections, "leaseSeconds": 20, "heldSeconds": held_seconds,
                "renewalsObserved": renewals, "leaseSamples": sampler.samples, "before": initial, "after": final,
                "dataset": {"teams": 500, "authorizationAudits": 1000, "pendingPlans": 100, "preservedSnapshotEntries": len(actual)},
                "previousPhysicalMetadata": previous_physical, "currentPhysicalMetadata": metadata_counts(database),
                "currentAuthorizationAuditRows": audit_rows, "seedSnapshotSHA256": hashlib.sha256(json.dumps(expected, sort_keys=True).encode()).hexdigest(),
                "kubernetesVersion": json.loads(wire.command("kubectl", "version", "-o", "json", env=env))["serverVersion"]}
    finally:
        if sampler and sampler.thread.is_alive():
            sampler.close()
        proxy.close()


def rehearse(output):
    current = wire.command("git", "rev-parse", "HEAD")
    tag, previous = wire.previous_release(current)
    receipt = {"schema": "conduit-hub-image-rolling-rehearsal.v1", "previousTag": tag, "previousCommit": previous, "currentCommit": current,
               "goVersion": wire.command("go", "version"), "helmVersion": wire.command("helm", "version", "--short"),
               "helmSHA256": wire.sha256(Path(shutil.which("helm"))), "kubectlSHA256": wire.sha256(Path(shutil.which("kubectl"))),
               "kubectlVersion": json.loads(wire.command("kubectl", "version", "--client", "-o", "json"))["clientVersion"],
               "dockerVersion": wire.command("docker", "version", "--format", "{{.Server.Version}}"),
               "runnerSHA256": wire.sha256(Path(__file__)), "probeSHA256": wire.sha256(ROOT / "tools/upgrade-rolling/testdata/probe/main.go"),
               "proxySHA256": wire.sha256(ROOT / "tools/upgrade-rolling/proxy.py"), "observerSHA256": wire.sha256(ROOT / "tools/upgrade-rolling/invariants.py"),
               "images": {"postgres": wire.POSTGRES, "kubernetes": wire.K3S, "registry": wire.REGISTRY},
               "scope": "two production Hub binaries in imported empty-base fixture images; driver-managed rolling replacement, not a Kubernetes Deployment controller; real Agent/Helm with controller-only Kubernetes API; no production image packaging/publication claim"}
    images = {}
    network = None
    network_created = False
    with tempfile.TemporaryDirectory(prefix="conduit-rolling-") as directory:
        work = Path(directory)
        print("Building old/current production binaries and constructor probes", flush=True)
        old = wire.archive_and_build(work, "previous", previous)
        new = wire.archive_and_build(work, "current", current)
        old_probe, new_probe = build_probe(work, "previous"), build_probe(work, "current")
        try:
            for label, binaries in (("previous", old), ("current", new)):
                images[label] = fixture_image(work, label, binaries["platform-delivery"])
            network = "conduit-rolling-" + work.name.rsplit("-", 1)[-1]
            wire.command("docker", "network", "create", "--label", "conduit.rehearsal=rolling", network)
            network_created = True
            with wire.fixture(work) as f:
                try:
                    receipt["result"] = exercise(f, work, new, old_probe, new_probe, network, images)
                except Exception:
                    for name in f.containers:
                        if name.startswith("rolling-"):
                            subprocess.run(["docker", "logs", "--tail", "15", name], check=False)
                    raise
            receipt["hubFixtureImageIDs"] = {label: image_id for label, (_, image_id) in images.items()}
            receipt["hubBinarySHA256"] = {"previous": wire.sha256(old["platform-delivery"]), "current": wire.sha256(new["platform-delivery"])}
            receipt["agentBinarySHA256"] = wire.sha256(new["platform-spoke-agent"])
            receipt["probeBinarySHA256"] = {"previous": wire.sha256(old_probe), "current": wire.sha256(new_probe)}
        finally:
            if network_created:
                subprocess.run(["docker", "network", "rm", network], stdout=subprocess.DEVNULL, check=True)
            for tag_name, _ in images.values():
                subprocess.run(["docker", "image", "rm", tag_name], stdout=subprocess.DEVNULL, check=True)
    receipt["status"] = "PASSED"
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(receipt, indent=2) + "\n")
    print("PASSED: live two-replica image replacement; receipt " + str(output), flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--receipt", required=True, type=Path)
    args = parser.parse_args()
    rehearse(args.receipt.resolve())
