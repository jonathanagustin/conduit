#!/usr/bin/env bash
# Exercise lifecycle command boundaries without contacting a real engine or cluster.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
python3 - "${root}" <<'PY'
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

root = Path(sys.argv[1])
up, down = root / "scripts/uds-up.sh", root / "scripts/uds-down.sh"
fake = r'''#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
role, args = Path(sys.argv[0]).name, sys.argv[1:]
if role == "uds" and args[:3] == ["zarf", "tools", "kubectl"]:
    role, args = "kubectl", args[3:]
entry = {"role": role, "args": args, "kubeconfig": os.getenv("KUBECONFIG"),
         "docker_host": os.getenv("DOCKER_HOST"), "docker_context": os.getenv("DOCKER_CONTEXT")}
def record():
    with open(os.environ["UDS_FIXTURE_TRACE"], "a") as stream:
        stream.write(json.dumps(entry) + "\n")
def flag(name):
    return os.getenv("UDS_FIXTURE_" + name) == "1"
if role in ("docker", "podman", "custom-engine"):
    record()
    if args == ["info"]:
        sys.exit(1 if flag("ENGINE_DOWN") or (role == "docker" and flag("DOCKER_DOWN")) else 0)
    if args[:2] == ["network", "inspect"]:
        if "-f" in args:
            print("other-container" if flag("NETWORK_BUSY") else "")
            if flag("NETWORK_LIST_FAIL"):
                sys.exit(1)
        sys.exit(0 if flag("NETWORK_EXISTS") else 1)
    if args[:2] in (["network", "create"], ["network", "rm"]):
        sys.exit(0)
elif role == "uds":
    record()
    if args and args[0] == "deploy":
        sys.exit(1 if flag("DEPLOY_FAIL") else 0)
elif role == "k3d":
    record()
    if args[:2] == ["kubeconfig", "get"]:
        context = "k3d-" + args[2]
        print(json.dumps({"apiVersion": "v1", "kind": "Config",
            "current-context": "k3d-other" if flag("WRONG_CONTEXT") else context,
            "contexts": [] if flag("MISSING_CONTEXT") else [{"name": context, "context": {}}]}))
        sys.exit(1 if flag("EXPORT_FAIL") else 0)
    if args[:2] == ["cluster", "delete"]:
        sys.exit(1 if flag("DELETE_FAIL") else 0)
elif role == "kubectl":
    if len(args) < 4 or args[0] != "--kubeconfig" or args[2] != "--context":
        record()
        sys.exit("kubectl must explicitly bind both config and context")
    config, context, command = Path(args[1]), args[3], args[4:]
    expected = "k3d-" + os.getenv("UDS_K3D_CLUSTER", "uds")
    if context != expected:
        record()
        sys.exit("wrong requested context")
    if command == ["config", "current-context"]:
        record()
        print(json.loads(config.read_text())["current-context"])
        sys.exit(1 if flag("CURRENT_FAIL") else 0)
    if command == ["config", "get-contexts", context, "-o", "name"]:
        record()
        if any(item["name"] == context for item in json.loads(config.read_text())["contexts"]):
            print(context)
        sys.exit(1 if flag("LIST_FAIL") else 0)
    if command == ["get", "ns"]:
        record()
        sys.exit(1 if flag("API_DOWN") else 0)
    if command[:5] == ["-n", "uds-dev-stack", "get", "ds", "nginx"]:
        record()
        print(json.dumps({"metadata": {"name": "nginx", "managedFields": []}, "status": {},
            "spec": {"template": {"spec": {"containers": [{"ports": [{"containerPort": 80, "hostPort": 19080}]}],
            "initContainers": [{"ports": [{"containerPort": 81, "hostPort": 19081}]}]}}}}))
        sys.exit(0)
    if command[:5] == ["-n", "istio-system", "get", "cm", "istio-cni-config"]:
        record()
        print(json.dumps({"kind": "ConfigMap", "data": {"cni.conf": "AMBIENT_ENABLED=true"}}))
        sys.exit(0)
    if command[:5] == ["-n", "istio-system", "get", "ds", "istio-cni"] or command == ["-n", "istio-system", "set", "env", "ds/istio-cni", "AMBIENT_ENABLED=false"]:
        record()
        sys.exit(0)
    if command == ["apply", "-f", "-"]:
        entry["document"] = json.load(sys.stdin)
        record()
        sys.exit(0)
    record()
elif role == "sleep":
    sys.exit(0)
elif role == "uname":
    # Keep the normal fixture on Linux so the shared selector's absolute
    # macOS /opt/podman fallback can never escape this fake command path.
    print(os.getenv("UDS_FIXTURE_OS", "Linux") if args == ["-s"] else "arm64")
    sys.exit(0)
else:
    record()
sys.exit("unexpected fixture command: " + role + " " + repr(args))
'''

with tempfile.TemporaryDirectory(prefix="conduit-uds-test-") as directory:
    fixture = Path(directory)
    bin_dir, tools_dir = fixture / "bin", fixture / "tools"
    bin_dir.mkdir()
    tools_dir.mkdir()
    # Curate PATH so a fixture failure cannot invoke installed cluster clients.
    for tool in ("bash", "dirname", "basename", "cat", "mktemp", "mkdir", "mv", "rm", "head", "tr"):
        executable = shutil.which(tool)
        assert executable, tool
        (tools_dir / tool).symlink_to(executable)
    (tools_dir / "python3").symlink_to(sys.executable)
    for role in ("docker", "podman", "custom-engine", "uds", "k3d", "kubectl", "sleep", "uname"):
        script = bin_dir / role
        script.write_text(fake)
        script.chmod(0o755)
    ambient = fixture / "ambient.kubeconfig"
    ambient.write_text("ambient context must stay untouched\n")
    baseline = {key: value for key, value in os.environ.items()
                if not key.startswith("UDS_") and key not in
                ("CONDUIT_CONTAINER_ENGINE", "DOCKER_HOST", "DOCKER_CONTEXT", "K3D_EXTRA_ARGS")}
    baseline.update(PATH=f"{bin_dir}:{tools_dir}", KUBECONFIG=str(ambient))
    cases = 0

    def run(name, *, script=up, flags=(), success=True, existing_context=None, **overrides):
        global cases
        cases += 1
        config = fixture / name / "cluster.kubeconfig"
        if existing_context is not None:
            config.parent.mkdir()
            config.write_text(json.dumps({"current-context": existing_context,
                "contexts": [{"name": existing_context, "context": {}}]}))
        trace = fixture / (name + ".jsonl")
        env = dict(baseline, UDS_KUBECONFIG=str(config), UDS_FIXTURE_TRACE=str(trace))
        env.update({key: str(value) for key, value in overrides.items()})
        result = subprocess.run([str(script), *flags], env=env, text=True, capture_output=True, timeout=30)
        assert (result.returncode == 0) == success, (name, result.returncode, result.stdout, result.stderr)
        events = [json.loads(line) for line in trace.read_text().splitlines()] if trace.exists() else []
        assert ambient.read_text() == "ambient context must stay untouched\n", name
        assert all("delete-context" not in row["args"] and "secret" not in row["args"] for row in events), name
        return result, events, config

    def assert_up(name, *, engine="docker", cluster="uds", **overrides):
        result, events, config = run(name, UDS_K3D_CLUSTER=cluster, **overrides)
        deployments = [row for row in events if row["role"] == "uds"]
        assert len(deployments) == 1, (name, events)
        args = deployments[0]["args"]
        assert args[:3] == ["deploy", "oci://ghcr.io/defenseunicorns/packages/uds/bundles/k3d-core-slim-dev:1.17.1", "--confirm"], args
        assert args[3:5] == ["--set", "CLUSTER_NAME=" + cluster], args
        assert args[5] == "--set" and args[6].startswith("K3D_EXTRA_ARGS="), args
        expected_ports = []
        for kind in ("API", "HTTP", "HTTPS"):
            if "UDS_K3D_" + kind + "_PORT" in overrides:
                expected_ports += ["--set", "K3D_" + kind + "_PORT=" + str(overrides["UDS_K3D_" + kind + "_PORT"])]
        assert args[7:] == expected_ports, args
        assert all(row["kubeconfig"] == str(config) for row in events if row["role"] in ("uds", "k3d", "kubectl")), events
        assert [row["args"] for row in events if row["role"] == "k3d"] == [["kubeconfig", "get", cluster]], events
        assert all(row["role"] == engine for row in events if row["args"][:1] == ["network"]), events
        kubectl = [row for row in events if row["role"] == "kubectl"]
        assert kubectl[0]["args"][4:] == ["config", "current-context"], kubectl
        assert kubectl[1]["args"][4:] == ["config", "get-contexts", "k3d-" + cluster, "-o", "name"], kubectl
        assert all(row["args"][:4] == ["--kubeconfig", str(config), "--context", "k3d-" + cluster] for row in kubectl[2:]), kubectl
        applied = [row["document"] for row in kubectl if "document" in row]
        assert len(applied) == 2, applied
        nginx, cni = applied
        assert "status" not in nginx and "managedFields" not in nginx["metadata"], nginx
        for kind in ("containers", "initContainers"):
            assert all("hostPort" not in port and "containerPort" in port for container in nginx["spec"]["template"]["spec"][kind] for port in container["ports"]), nginx
        assert cni["data"] == {"AMBIENT_ENABLED": "false", "cni.conf": "AMBIENT_ENABLED=false"}, cni
        assert json.loads(config.read_text())["current-context"] == "k3d-" + cluster
        assert config.stat().st_mode & 0o777 == 0o600, config.stat()
        assert not list(config.parent.glob("cluster.kubeconfig.*")), name
        return result, events

    result, events, _ = run("plan", flags=("--print-plan",))
    for line in ("network=uds-k3d", "cluster=uds", "context=k3d-uds", "bundle=oci://ghcr.io/defenseunicorns/packages/uds/bundles/k3d-core-slim-dev:1.17.1",
                 "k3d_extra_args=--network uds-k3d --k3s-arg --flannel-backend=host-gw@server:0", "host_adaptations=clear-nginx-hostports,ambient-off"):
        assert line in result.stdout.splitlines(), (line, result.stdout)
    assert not events and not any(word in result.stdout.lower() for word in ("password", "secret", "token"))
    result, events, _ = run("custom-plan", flags=("--print-plan",), UDS_K3D_CLUSTER="task-922", UDS_CORE_VERSION="1.16.0")
    assert "network=task-922-k3d\n" in result.stdout and "context=k3d-task-922\n" in result.stdout and "slim-dev:1.16.0\n" in result.stdout and not events
    result, events, _ = run("dry", flags=("--dry-run",))
    assert "docker network inspect uds-k3d || docker network create uds-k3d" in result.stdout and "--set CLUSTER_NAME=uds --set K3D_EXTRA_ARGS=" in result.stdout
    assert [(row["role"], row["args"]) for row in events] == [("docker", ["info"])]
    result, events, _ = run("unknown", flags=("--unknown",), success=False)
    assert "unknown argument: --unknown" in result.stderr and not events
    _, events, _ = run("unsafe-cluster", success=False, UDS_K3D_CLUSTER="--all")
    assert not events
    assert_up("docker")
    assert_up("absolute-podman", engine="podman", CONDUIT_CONTAINER_ENGINE=str(bin_dir / "podman"), DOCKER_HOST="unix:///fixture/podman.sock")
    assert_up("mac-podman", engine="podman", CONDUIT_CONTAINER_ENGINE="podman", DOCKER_HOST="unix:///fixture/podman.sock", UDS_FIXTURE_OS="Darwin")
    assert_up("port-boundaries", UDS_K3D_API_PORT=1, UDS_K3D_HTTP_PORT=65535)
    _, events = assert_up("scoped-ports", cluster="task-ports", UDS_K3D_API_PORT=16550, UDS_K3D_HTTP_PORT=18080, UDS_K3D_HTTPS_PORT=18443)
    deploy = next(row for row in events if row["role"] == "uds")
    assert deploy["args"][7:] == ["--set", "K3D_API_PORT=16550", "--set", "K3D_HTTP_PORT=18080", "--set", "K3D_HTTPS_PORT=18443"], deploy
    for name, overrides in (("zero-port", {"UDS_K3D_API_PORT": 0}),
            ("large-port", {"UDS_K3D_HTTP_PORT": 65536}),
            ("argument-port", {"UDS_K3D_HTTPS_PORT": "443 --all"}),
            ("list-config", {"UDS_KUBECONFIG": "/one:/two"}),
            ("relative-config", {"UDS_KUBECONFIG": "relative"}),
            ("argument-network", {"UDS_K3D_NETWORK": "--all"})):
        _, events, _ = run(name, success=False, **overrides)
        assert not events, (name, events)
    _, events = assert_up("podman", engine="podman", UDS_FIXTURE_DOCKER_DOWN=1, DOCKER_HOST="unix:///fixture/podman.sock", DOCKER_CONTEXT="unrelated")
    assert all(row["docker_context"] is None and row["docker_host"] == "unix:///fixture/podman.sock" for row in events), events
    _, events = assert_up("explicit", engine="custom-engine", cluster="task-922", CONDUIT_CONTAINER_ENGINE="custom-engine", DOCKER_HOST="unix:///fixture/custom.sock", UDS_K3D_NETWORK="task-922-net", K3D_EXTRA_ARGS="--network task-922-net -p 19443:443@loadbalancer")
    deploy = next(row for row in events if row["role"] == "uds")
    assert deploy["args"][-1] == "K3D_EXTRA_ARGS=--network task-922-net -p 19443:443@loadbalancer", deploy
    assert all(row["args"][-1] == "task-922-net" for row in events if row["args"][:1] == ["network"]), events
    (bin_dir / "kubectl").rename(bin_dir / "hidden-kubectl")
    try:
        assert_up("bundled-kubectl")
    finally:
        (bin_dir / "hidden-kubectl").rename(bin_dir / "kubectl")
    for name, overrides in (("missing-endpoint", {"CONDUIT_CONTAINER_ENGINE": "podman"}),
            ("context-only", {"DOCKER_CONTEXT": "somewhere"}),
            ("unreachable-engine", {"CONDUIT_CONTAINER_ENGINE": "custom-engine", "DOCKER_HOST": "unix:///fixture/api.sock", "UDS_FIXTURE_ENGINE_DOWN": 1}),
            ("command-string-engine", {"CONDUIT_CONTAINER_ENGINE": "podman --remote", "DOCKER_HOST": "unix:///fixture/api.sock"})):
        _, events, _ = run(name, success=False, **overrides)
        assert all(row["args"] == ["info"] for row in events), (name, events)
    for flag in ("EXPORT_FAIL", "WRONG_CONTEXT", "MISSING_CONTEXT", "CURRENT_FAIL", "LIST_FAIL", "DEPLOY_FAIL"):
        _, events, config = run(flag.lower(), success=False, **{"UDS_FIXTURE_" + flag: 1})
        assert not config.exists() and not list(config.parent.glob("cluster.kubeconfig.*")), flag
        assert all(row["args"][4:5] == ["config"] for row in events if row["role"] == "kubectl"), (flag, events)
    _, events, config = run("existing-wrong", success=False, existing_context="k3d-unrelated")
    assert json.loads(config.read_text())["current-context"] == "k3d-unrelated"
    assert all(row["role"] == "kubectl" or row["args"] == ["info"] for row in events), events
    _, events, _ = run("existing-correct", existing_context="k3d-uds")
    assert [row["role"] for row in events[:3]] == ["docker", "kubectl", "kubectl"], events
    assert events[3]["args"][:2] == ["network", "inspect"], events
    _, events, config = run("existing-export-failure", success=False, existing_context="k3d-uds", UDS_FIXTURE_EXPORT_FAIL=1)
    assert json.loads(config.read_text())["current-context"] == "k3d-uds"
    assert not list(config.parent.glob("cluster.kubeconfig.*"))
    assert all(row["args"][4:5] == ["config"] for row in events if row["role"] == "kubectl"), events
    _, events, _ = run("api-down", success=False, UDS_FIXTURE_API_DOWN=1)
    assert all(row["args"][4:5] == ["config"] or row["args"][4:] == ["get", "ns"] for row in events if row["role"] == "kubectl"), events
    result, events, _ = run("down-plan", script=down, flags=("--print-plan",))
    assert "k3d cluster delete uds\n" in result.stdout and "context=k3d-uds\n" in result.stdout and not events
    _, events, config = run("down-podman", script=down, CONDUIT_CONTAINER_ENGINE="podman", DOCKER_HOST="unix:///fixture/podman.sock", UDS_K3D_CLUSTER="task-922", UDS_FIXTURE_NETWORK_EXISTS=1)
    assert [(row["role"], row["args"]) for row in events] == [("podman", ["info"]), ("k3d", ["cluster", "delete", "task-922"]), ("podman", ["network", "inspect", "task-922-k3d"]), ("podman", ["network", "inspect", "-f", "{{range .Containers}}{{.Name}} {{end}}", "task-922-k3d"]), ("podman", ["network", "rm", "task-922-k3d"])], events
    assert all(row["kubeconfig"] == str(config) for row in events if row["role"] == "k3d"), events
    _, events, _ = run("down-busy", script=down, UDS_FIXTURE_NETWORK_EXISTS=1, UDS_FIXTURE_NETWORK_BUSY=1)
    assert not any(row["args"][:2] == ["network", "rm"] for row in events), events
    _, events, _ = run("down-network-inspection-failed", script=down, success=False, UDS_FIXTURE_NETWORK_EXISTS=1, UDS_FIXTURE_NETWORK_LIST_FAIL=1)
    assert not any(row["args"][:2] == ["network", "rm"] for row in events), events
    _, events, _ = run("down-failed", script=down, success=False, UDS_FIXTURE_DELETE_FAIL=1)
    assert [(row["role"], row["args"]) for row in events] == [("docker", ["info"]), ("k3d", ["cluster", "delete", "uds"])], events
    (bin_dir / "k3d").unlink()
    _, events, _ = run("down-no-k3d", script=down, success=False)
    assert not events, events
    _, events, _ = run("up-no-k3d", success=False)
    assert all(row["args"] == ["info"] for row in events), events
    print(f"UDS lifecycle command boundaries verified ({cases} cases; no real cluster commands)")
PY
