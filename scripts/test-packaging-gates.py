"""Run packaging regression gates in a temporary, dependency-pinned Python venv.

No Codex executable, credentials, model call, Helm download or cluster required.
This runs protocol/validator tests, not a live packaging acceptance test.
"""
from pathlib import Path
import os
import subprocess
import sys
import tempfile


def main():
    if sys.version_info[:2] != (3, 12):
        raise SystemExit("packaging gates require Python 3.12")
    scripts = Path(__file__).resolve().parent
    with tempfile.TemporaryDirectory(prefix="conduit-packaging-gates-") as directory:
        environment = Path(directory) / "venv"
        subprocess.run([sys.executable, "-I", "-m", "venv", str(environment)], check=True, timeout=120)
        python = environment / ("Scripts/python.exe" if os.name == "nt" else "bin/python")
        subprocess.run([
            str(python), "-I", "-m", "pip", "--disable-pip-version-check", "install",
            "--requirement", str(scripts / "packaging-validator-requirements.txt"),
        ], check=True, timeout=300)
        for name in ("test-packaging-validator.py", "test-codex-probe.py"):
            subprocess.run([str(python), "-I", "-B", str(scripts / name)], check=True, timeout=120)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
