#!/usr/bin/env python3
"""Run both Agent/Hub source-version directions against real Helm and Kubernetes."""
import argparse
from contextlib import contextmanager
import json
import os
from pathlib import Path
import re
import shutil
import socket
import ssl
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "tools/upgrade-data"))
from rehearse import command, previous_release, sha256, POSTGRES  # noqa: E402

K3S = "rancher/k3s@sha256:53cf744fe2fabf140cee240d2db70d13a4f2d98f1a13c98f58f457f423096917"
REGISTRY = "registry@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373"


def runtime_environment(work):
    # Do not inherit operator OIDC, provider credentials, material-preparer
    # commands, Helm plugins/registry credentials or a personal kubecontext.
    names = ("PATH", "HOME", "LANG", "HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
             "http_proxy", "https_proxy", "all_proxy", "no_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR")
    env = {key: os.environ[key] for key in names if key in os.environ}
    for name, path in {
        "HELM_CONFIG_HOME": work / "helm-config", "HELM_CACHE_HOME": work / "helm-cache",
        "HELM_DATA_HOME": work / "helm-data", "TMPDIR": work / "tmp",
    }.items():
        path.mkdir(parents=True, exist_ok=True)
        env[name] = str(path)
    env["HELM_REGISTRY_CONFIG"] = str(work / "registry.json")
    return env


def wait_for(check, description, timeout=120):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        value = check()
        if value:
            return value
        time.sleep(0.5)
    raise RuntimeError("timed out: " + description)


class Fixture:
    def __init__(self, work):
        self.work = work
        self.containers = []
        self.processes = []
        self.logs = []

    def container(self, suffix, image, *args):
        name = "conduit-wire-" + self.work.name.rsplit("-", 1)[-1] + "-" + suffix
        command("docker", "run", "--detach", "--name", name, "--label", "conduit.rehearsal=wire",
                *args, image)
        self.containers.append(name)
        return name

    def start(self, label, binary, env):
        stream = (self.work / (label + ".log")).open("w+")
        process = subprocess.Popen([str(binary)], env=env, stdout=stream, stderr=stream)
        self.processes.append(process)
        self.logs.append((label, stream))
        return process

    def close(self, failed):
        for process in reversed(self.processes):
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait(timeout=10)
        if failed:
            # Only fixture binaries receive these logs; private key contents and
            # kubeconfig are never read into them. Bound failure diagnostics.
            for label, stream in self.logs:
                stream.flush()
                stream.seek(0)
                print(label + ":\n" + "\n".join(stream.read().splitlines()[-25:]), file=sys.stderr)
        for _, stream in self.logs:
            stream.close()
        for name in reversed(self.containers):
            subprocess.run(["docker", "rm", "--force", name], stdout=subprocess.DEVNULL, check=True)


@contextmanager
def fixture(work):
    instance = Fixture(work)
    failed = True
    try:
        yield instance
        failed = False
    finally:
        instance.close(failed)


def listener(container, port):
    address = command("docker", "port", container, str(port) + "/tcp")
    if not re.fullmatch(r"127\.0\.0\.1:\d+", address):
        raise RuntimeError("unexpected fixture listener")
    return address


def archive_and_build(work, label, revision):
    source = work / label
    source.mkdir()
    with tempfile.TemporaryFile() as archive:
        subprocess.run(["git", "archive", revision], cwd=ROOT, stdout=archive, check=True)
        archive.seek(0)
        subprocess.run(["tar", "-x", "-C", str(source)], stdin=archive, check=True)
    env = os.environ.copy()
    env["CGO_ENABLED"] = "0"
    binaries = {}
    version = (source / "VERSION").read_text().strip()
    flags = ("-X github.com/jonathanagustin/conduit/internal/buildinfo.Version=" + version +
             " -X github.com/jonathanagustin/conduit/internal/buildinfo.Commit=" + revision)
    for name in ("platform-delivery", "platform-spoke-agent"):
        binary = work / (label + "-" + name)
        subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", "-ldflags", flags,
                        "-o", str(binary), "./cmd/" + name], cwd=source, env=env, check=True)
        binaries[name] = binary
    return binaries


def certificate(work, name, san, usage):
    common = ["openssl", "req", "-new", "-newkey", "rsa:2048", "-nodes", "-sha256",
              "-subj", "/CN=" + name, "-addext", "basicConstraints=critical,CA:FALSE",
              "-addext", "keyUsage=critical,digitalSignature,keyEncipherment",
              "-addext", "subjectAltName=" + san,
              "-addext", "extendedKeyUsage=" + usage, "-keyout", str(work / (name + ".key")),
              "-out", str(work / (name + ".csr"))]
    subprocess.run(common, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True)
    subprocess.run(["openssl", "x509", "-req", "-sha256", "-days", "1", "-copy_extensions", "copy",
                    "-in", str(work / (name + ".csr")), "-CA", str(work / "ca.crt"),
                    "-CAkey", str(work / "ca.key"), "-CAcreateserial", "-out", str(work / (name + ".crt"))],
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True)
    (work / (name + ".key")).chmod(0o600)


def tls_material(work, environment):
    subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-sha256", "-days", "1",
                    "-subj", "/CN=wire-fixture",
                    "-addext", "basicConstraints=critical,CA:TRUE",
                    "-addext", "keyUsage=critical,keyCertSign,cRLSign",
                    "-keyout", str(work / "ca.key"), "-out", str(work / "ca.crt")],
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True)
    (work / "ca.key").chmod(0o600)
    certificate(work, "hub", "IP:127.0.0.1", "serverAuth")
    certificate(work, "operator", "URI:spiffe://platform.delivery/operator/wire-operator", "clientAuth")
    certificate(work, "reviewer", "URI:spiffe://platform.delivery/operator/wire-reviewer", "clientAuth")
    certificate(work, "agent", "URI:spiffe://platform.delivery/environment/" + environment + "/agent/wire-agent", "clientAuth")
    subprocess.run(["openssl", "genpkey", "-algorithm", "ED25519", "-out", str(work / "bundle.key")],
                   stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True)
    (work / "bundle.key").chmod(0o600)


class API:
    def __init__(self, address, work, identity):
        self.url = "https://" + address
        self.tls = ssl.create_default_context(cafile=str(work / "ca.crt"))
        self.tls.load_cert_chain(str(work / (identity + ".crt")), str(work / (identity + ".key")))

    def call(self, path, body=None, method=None):
        data = None if body is None else json.dumps(body).encode()
        request = urllib.request.Request(self.url + path, data=data, method=method,
                                         headers={"Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(request, context=self.tls, timeout=5) as response:
                return json.loads(response.read())
        except urllib.error.HTTPError as error:
            raise RuntimeError(f"{request.method} {path}: HTTP {error.code}") from None

    def ready(self):
        try:
            self.call("/health/ready")
            return True
        except (urllib.error.URLError, RuntimeError):
            return False


def configure_review(api, environment):
    api.call("/v1/teams/wire-reviewers", {"displayName": "Wire fixture reviewers", "adminUsers": ["wire-operator"], "memberUsers": ["wire-reviewer"]}, "PUT")
    api.call("/v1/resource-grants", {"resourceRid": "environment/" + environment, "team": "wire-reviewers", "operations": ["change:propose", "resource:read", "resource:operate"]})
    api.call("/v1/approval-policies/wire-review", {"displayName": "Explicit wire review", "expression": 'require_team("wire-reviewers", "wire fixture review")', "enabled": True}, "PUT")


def desired(environment, registry, chart_digest, value):
    image_digest = "sha256:" + "c" * 64
    return {
        "apiVersion": "platform.delivery/v1alpha1", "kind": "EnvironmentDeployment",
        "environment": {"id": environment, "generation": "0123456789abcdef", "helmExecution": {"imagePrePull": {"enabled": False}}},
        "releases": [{"product": "example.test:platform-e2e", "version": "0.1.0",
                      "chart": {"repository": registry + "/platform-e2e", "tag": "0.1.0", "digest": chart_digest},
                      "images": [{"repository": "example.invalid/platform-e2e", "tag": "1.0.0", "digest": image_digest}]}],
        "entities": [{"name": "platform-e2e", "product": "example.test:platform-e2e", "version": "0.1.0", "namespace": environment,
                      "helmValues": {"image": "example.invalid/platform-e2e:1.0.0@" + image_digest, "value": value}}],
    }


def assert_report(report, plan, environment):
    expected = {"environment": environment, "agentId": "wire-agent", "planId": plan["id"]}
    for key, value in expected.items():
        if report.get(key) != value:
            raise RuntimeError("report mismatch: " + key)
    if len(report.get("entities", [])) != 1 or not report.get("observedAt") or report.get("revision", 0) < 1:
        raise RuntimeError("report cardinality or observation missing")
    step = plan["plan"]["steps"][0]
    expected_entity = {"installationId": "platform-e2e", "product": "example.test:platform-e2e", "version": "0.1.0",
                       "namespace": environment, "releaseDigest": step["releaseDigest"], "configDigest": step["configDigest"],
                       "health": "UNKNOWN", "healthDetails": {"quorum": "ALL", "nodes": []},
                       "rolloutStatus": "UP_TO_DATE", "configStatus": "APPLIED", "lifecycle": "INSTALLED"}
    for key, value in expected_entity.items():
        if report["entities"][0].get(key) != value:
            raise RuntimeError("entity report mismatch: " + key)
    if not report["entities"][0].get("observedAt"):
        raise RuntimeError("entity observation missing")


def run_case(f, work, label, hub, agent, kube_env, registry, chart_digest):
    work.mkdir()
    tls_material(work, label)
    env = runtime_environment(work)
    env.update({"KUBECONFIG": kube_env["KUBECONFIG"]})
    command("kubectl", "create", "namespace", label, env=env)
    database = f.container(label + "-db", POSTGRES, "--env", "POSTGRES_USER=conduit", "--env", "POSTGRES_PASSWORD=wire_fixture", "--env", "POSTGRES_DB=wire_fixture", "--publish", "127.0.0.1::5432")
    streak = 0
    def database_ready():
        nonlocal streak
        ok = subprocess.run(["docker", "exec", database, "pg_isready", "-U", "conduit", "-d", "wire_fixture"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0
        streak = streak + 1 if ok else 0
        return streak >= 3
    wait_for(database_ready, "PostgreSQL steady readiness")
    dsn = work / "postgres.dsn"
    dsn.write_text("postgres://conduit:wire_fixture@" + listener(database, 5432) + "/wire_fixture?sslmode=disable")
    dsn.chmod(0o600)
    with socket.socket() as reservation:
        reservation.bind(("127.0.0.1", 0))
        address = "127.0.0.1:" + str(reservation.getsockname()[1])
    hub_env = dict(env, LISTEN_ADDRESS=address, REPOSITORY_KIND="postgres", POSTGRES_DSN_FILE=str(dsn),
                   PLATFORM_BOOTSTRAP_ADMINISTRATORS="wire-operator", AGENT_CLIENT_CA_FILE=str(work / "ca.crt"),
                   TLS_CERT_FILE=str(work / "hub.crt"), TLS_KEY_FILE=str(work / "hub.key"), BUNDLE_SIGNING_KEY_FILE=str(work / "bundle.key"))
    hub_process = f.start(label + "-hub", hub, hub_env)
    operator, reviewer = API(address, work, "operator"), API(address, work, "reviewer")
    wait_for(operator.ready, "Hub mTLS readiness")
    configure_review(operator, label)
    agent_env = dict(env, CONTROL_PLANE_URL=operator.url, ENVIRONMENT_ID=label, AGENT_ID="wire-agent", POLL_INTERVAL="500ms",
                     LEASE_DURATION="20s", HELM_TIMEOUT="90s", HEALTH_POLL_INTERVAL="5s", HELM_PLAIN_HTTP="true",
                     CONTROL_PLANE_CA_FILE=str(work / "ca.crt"), CONTROL_PLANE_CLIENT_CERT_FILE=str(work / "agent.crt"), CONTROL_PLANE_CLIENT_KEY_FILE=str(work / "agent.key"))
    agent_process = f.start(label + "-agent", agent, agent_env)
    value = "real-helm-" + label
    plan = operator.call("/v1/plans", desired(label, registry, chart_digest, value))
    if plan.get("status") != "AWAITING_APPROVAL" or plan.get("requiredApprovals", 0) < 1:
        raise RuntimeError("Plan was not held for explicit review")
    # Leave the real Agent polling while the Plan is awaiting approval.
    time.sleep(1.5)
    if operator.call("/v1/plans/" + plan["id"])["status"] != "AWAITING_APPROVAL":
        raise RuntimeError("unapproved Plan started")
    if command("kubectl", "get", "configmap", "platform-e2e", "-n", label, "--ignore-not-found", "-o", "json", env=env):
        raise RuntimeError("unapproved Plan mutated the cluster")
    reviewer.call("/v1/plans/" + plan["id"] + "/approvals", {"rationale": "isolated source-version acceptance"})
    def completed():
        if hub_process.poll() is not None or agent_process.poll() is not None:
            raise RuntimeError("fixture production process exited")
        record = operator.call("/v1/plans/" + plan["id"])
        if record["status"] in ("FAILED", "CANCELLED"):
            raise RuntimeError("Plan terminated: " + record["status"] + " " + record.get("failure", ""))
        return record if record["status"] == "SUCCEEDED" else False
    terminal = wait_for(completed, "approved Plan completion", 150)
    configmap = json.loads(command("kubectl", "get", "configmap", "platform-e2e", "-n", label, "-o", "json", env=env))
    if configmap["data"]["value"] != value or configmap["data"]["image"] != "example.invalid/platform-e2e:1.0.0@sha256:" + "c" * 64:
        raise RuntimeError("real Helm value mismatch")
    annotations = configmap["metadata"]["annotations"]
    for key, expected in {"environment.id": label, "entity.id": "aeid:" + label + ":helm-chart:platform-e2e", "product.group": "example.test", "product.name": "platform-e2e", "product.version": "0.1.0"}.items():
        if annotations.get("apollo.palantir.com/metadata." + key) != expected:
            raise RuntimeError("real Helm post-render metadata missing: " + key)
    def exact_report():
        report = operator.call("/v1/environments/" + label + "/reported-state")
        try:
            assert_report(report, terminal, label)
            return report
        except RuntimeError:
            return False
    report = wait_for(exact_report, "exact terminal reported state", 30)
    agent_process.terminate()
    agent_process.wait(timeout=10)
    hub_process.terminate()
    hub_process.wait(timeout=10)
    return {"case": label, "planID": terminal["id"], "status": terminal["status"], "explicitApprovalRequired": True,
            "report": report, "hubBinarySHA256": sha256(hub), "agentBinarySHA256": sha256(agent), "configMapValue": configmap["data"]["value"]}


def rehearse(output):
    current = command("git", "rev-parse", "HEAD")
    tag, previous = previous_release(current)
    receipt = {"schema": "conduit-wire-skew-rehearsal.v1", "previousTag": tag, "previousCommit": previous,
               "currentCommit": current, "goVersion": command("go", "version"), "runnerSHA256": sha256(Path(__file__)),
               "images": {"postgres": POSTGRES, "kubernetes": K3S, "registry": REGISTRY},
               "helmVersion": command("helm", "version", "--short"), "helmSHA256": sha256(Path(shutil.which("helm"))),
               "kubectlSHA256": sha256(Path(shutil.which("kubectl"))), "kubectlVersion": json.loads(command("kubectl", "version", "--client", "-o", "json"))["clientVersion"],
               "scope": "source-version production binaries; current compiler and pinned host Helm; controller-only Kubernetes API, no Pod scheduling; no rolling upgrade claim"}
    with tempfile.TemporaryDirectory(prefix="conduit-wire-") as directory:
        work = Path(directory)
        print("Building distinct previous/current production Hub and Agent binaries", flush=True)
        old = archive_and_build(work, "previous", previous)
        new = archive_and_build(work, "current", current)
        with fixture(work) as f:
            env = runtime_environment(work)
            # No kubelet/container runtime: the real chart contains a ConfigMap
            # only. This supported server mode also works under rootless Docker.
            api_name = "conduit-wire-" + work.name.rsplit("-", 1)[-1] + "-api"
            command("docker", "run", "--detach", "--name", api_name, "--label", "conduit.rehearsal=wire", "--cap-drop", "ALL", "--publish", "127.0.0.1::6443", "--entrypoint", "/bin/k3s", K3S,
                    "server", "--disable-agent", "--egress-selector-mode=disabled", "--disable=traefik,servicelb,metrics-server,local-storage,coredns")
            f.containers.append(api_name)
            kubeconfig = work / "kubeconfig"
            def api_ready():
                copied = subprocess.run(["docker", "cp", api_name + ":/etc/rancher/k3s/k3s.yaml", str(kubeconfig)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                if copied.returncode:
                    return False
                kubeconfig.chmod(0o600)
                kubeconfig.write_text(kubeconfig.read_text().replace("https://127.0.0.1:6443", "https://" + listener(api_name, 6443)))
                env["KUBECONFIG"] = str(kubeconfig)
                return subprocess.run(["kubectl", "get", "--raw", "/readyz"], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0
            wait_for(api_ready, "isolated Kubernetes API", 120)
            receipt["kubernetesVersion"] = json.loads(command("kubectl", "version", "-o", "json", env=env))["serverVersion"]
            registry_name = f.container("registry", REGISTRY, "--publish", "127.0.0.1::5000")
            registry = listener(registry_name, 5000)
            def registry_ready():
                try:
                    with urllib.request.urlopen("http://" + registry + "/v2/", timeout=2) as response:
                        return response.status == 200
                except urllib.error.URLError:
                    return False
            wait_for(registry_ready, "local OCI registry")
            command("helm", "package", str(ROOT / "testdata/e2e-chart"), "--destination", str(work), env=env)
            pushed = subprocess.run(["helm", "push", str(work / "platform-e2e-0.1.0.tgz"), "oci://" + registry, "--plain-http"], env=env, text=True, capture_output=True, check=True)
            match = re.search(r"Digest:\s+(sha256:[0-9a-f]{64})", pushed.stdout + pushed.stderr)
            if not match:
                raise RuntimeError("registry did not report a chart digest")
            chart_digest = match[1]
            receipt["chartDigest"] = chart_digest
            receipt["cases"] = []
            for label, hub, agent, hub_commit, agent_commit in (("old-agent-new-hub", new["platform-delivery"], old["platform-spoke-agent"], current, previous),
                                                               ("new-agent-old-hub", old["platform-delivery"], new["platform-spoke-agent"], previous, current)):
                print("Executing " + label + " with real Helm and explicit review", flush=True)
                result = run_case(f, work / label, label, hub, agent, env, registry, chart_digest)
                result.update(hubSourceCommit=hub_commit, agentSourceCommit=agent_commit)
                receipt["cases"].append(result)
    receipt["status"] = "PASSED"
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(receipt, indent=2) + "\n")
    print("PASSED: both source-version directions; receipt " + str(output), flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--receipt", required=True, type=Path)
    args = parser.parse_args()
    rehearse(args.receipt.resolve())
