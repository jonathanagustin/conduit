#!/usr/bin/env python3
"""Previous-release database rehearsal; never connects to an operator's DB."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]
POSTGRES = "postgres@sha256:a1d02e4bd40c94d3bf2bdd3678c137388e76d9efcd23c285e9429d336a834b44"


def command(*args, cwd=ROOT, env=None):
    return subprocess.check_output(args, cwd=cwd, env=env, text=True).strip()


def previous_release(current):
    tags = command("git", "tag", "--sort=-version:refname", "--list", "v*").splitlines()
    for tag in tags:
        if re.fullmatch(r"v\d+\.\d+\.\d+", tag):
            sha = command("git", "rev-parse", "--verify", tag + "^{commit}")
            if sha != current:
                return tag, sha
    raise RuntimeError("no previous stable release found; fetch the repository's tags")


def compare(expected, actual, path="snapshot"):
    """All old fields and all rows must survive; additive object fields may grow."""
    if type(expected) is not type(actual):
        raise RuntimeError(f"{path}: type changed")
    if isinstance(expected, dict):
        for key, value in expected.items():
            if key not in actual:
                raise RuntimeError(f"{path}/{key}: missing")
            compare(value, actual[key], f"{path}/{key}")
    elif isinstance(expected, list):
        if len(expected) != len(actual):
            raise RuntimeError(f"{path}: row count changed")
        for index, (left, right) in enumerate(zip(expected, actual)):
            compare(left, right, f"{path}/{index}")
    elif expected != actual:
        raise RuntimeError(f"{path}: value changed")


def compare_snapshots(expected, actual):
    # Top-level keys are entity identities, so extra keys here are unexpected
    # rows, not additive fields. Never silently accept duplicate/new entities.
    if expected.keys() != actual.keys():
        raise RuntimeError("snapshot identities changed")
    compare(expected, actual)


def assert_physical_counts(result, current=False):
    expected = {
        "conduit_repository_state": 1, "conduit_repository_blobs": 10,
        "conduit_reconciliation_desired": 200, "conduit_reconciliation_work": 100,
        "conduit_reconciliation_decisions": 100, "conduit_reconciliation_audit": 100,
        "conduit_reconciliation_proposal_outbox": 100, "conduit_gitless_resolutions": 50,
    }
    if current:
        expected.update(conduit_authorization_audit_events=1000, conduit_authorization_audit_head=1)
    for table, count in expected.items():
        if result["physicalRows"].get(table) != count:
            raise RuntimeError(f"{table}: unexpected physical row count")


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def build(work, label, revision, probe):
    source = work / label
    source.mkdir()
    # Archive committed sources, rather than modifying an old checkout or
    # accidentally testing the working tree in both halves of the comparison.
    with tempfile.TemporaryFile() as archive:
        subprocess.run(["git", "archive", revision], cwd=ROOT, stdout=archive, check=True)
        archive.seek(0)
        subprocess.run(["tar", "-x", "-C", str(source)], stdin=archive, check=True)
    target = source / "tools" / "upgrade-data-fixture"
    target.mkdir(parents=True)
    shutil.copyfile(probe, target / "main.go")
    binary = work / (label + "-probe")
    env = os.environ.copy()
    env["CGO_ENABLED"] = "0"
    subprocess.run(["go", "build", "-trimpath", "-buildvcs=false", "-o", str(binary),
                    "./tools/upgrade-data-fixture"], cwd=source, env=env, check=True)
    return source, binary


def rehearse(output):
    current = command("git", "rev-parse", "HEAD")
    tag, previous = previous_release(current)
    probe = ROOT / "tools/upgrade-data/testdata/probe/main.go"
    receipt = {
        "schema": "conduit-old-data-rehearsal.v1",
        "previousTag": tag, "previousCommit": previous, "currentCommit": current,
        "goVersion": command("go", "version"), "postgresImage": POSTGRES,
        "probeSHA256": sha256(probe), "runnerSHA256": sha256(Path(__file__)),
        "dataset": {"teams": 500, "policies": 100, "plans": 100, "reports": 50,
                    "authorizationAudits": 1000, "blobs": 10,
                    "desiredDecisionAuditProposalGroups": 100, "queuedWork": 100, "gitlessResolutions": 50},
        "scope": "production constructors linked into source-version fixture binaries; no Hub server, Agent, or rolling-upgrade claim",
    }
    with tempfile.TemporaryDirectory(prefix="conduit-upgrade-data-") as directory:
        work = Path(directory)
        print(f"Building {tag} ({previous}) and current ({current})", flush=True)
        old_source, old = build(work, "previous", previous, probe)
        _, new = build(work, "current", current, probe)
        receipt["previousBinarySHA256"] = sha256(old)
        receipt["currentBinarySHA256"] = sha256(new)
        receipt["foundationSHA256"] = sha256(old_source / "examples/example-foundation.json")
        name = "conduit-upgrade-data-" + work.name.rsplit("-", 1)[-1]
        # Fixed fixture password, disposable loopback-only DB, no supplied DSN.
        command("docker", "run", "--detach", "--name", name, "--label", "conduit.rehearsal=old-data",
                "--env", "POSTGRES_USER=conduit", "--env", "POSTGRES_PASSWORD=conduit_fixture",
                "--env", "POSTGRES_DB=conduit_fixture", "--publish", "127.0.0.1::5432", POSTGRES)
        try:
            streak = 0
            for _ in range(90):
                ready = subprocess.run(["docker", "exec", name, "pg_isready", "-U", "conduit", "-d", "conduit_fixture"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0
                streak = streak + 1 if ready else 0
                if streak >= 3:
                    break
                time.sleep(1)
            else:
                raise RuntimeError("disposable PostgreSQL did not become ready")
            address = command("docker", "port", name, "5432/tcp")
            if not re.fullmatch(r"127\.0\.0\.1:\d+", address):
                raise RuntimeError("unexpected PostgreSQL listener")
            env = os.environ.copy()
            env["CONDUIT_UPGRADE_DSN"] = f"postgres://conduit:conduit_fixture@{address}/conduit_fixture?sslmode=disable"
            env["CONDUIT_UPGRADE_FOUNDATION"] = str(old_source / "examples/example-foundation.json")
            print("Seeding previous-release constructors and reading every seeded object", flush=True)
            before = json.loads(command(str(old), "seed", env=env))
            assert_physical_counts(before)
            print("Opening current constructors and comparing preserved state", flush=True)
            after = json.loads(command(str(new), "read", env=env))
            assert_physical_counts(after, current=True)
            compare_snapshots(before["snapshot"], after["snapshot"])
            restarted = json.loads(command(str(new), "read", env=env))
            assert_physical_counts(restarted, current=True)
            compare_snapshots(after["snapshot"], restarted["snapshot"])
            receipt["firstCurrentConstructorMilliseconds"] = after["constructorMilliseconds"]
            receipt["reopenedConstructorMilliseconds"] = restarted["constructorMilliseconds"]
            receipt["snapshotEntries"] = len(before["snapshot"])
            receipt["previousPhysicalRows"] = before["physicalRows"]
            receipt["currentPhysicalRows"] = after["physicalRows"]
            receipt["status"] = "PASSED"
        finally:
            subprocess.run(["docker", "rm", "--force", name], stdout=subprocess.DEVNULL, check=True)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(receipt, indent=2) + "\n")
    print(f"PASSED: receipt written to {output}", flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--receipt", required=True, type=Path)
    args = parser.parse_args()
    rehearse(args.receipt.resolve())
