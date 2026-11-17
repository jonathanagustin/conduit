#!/usr/bin/env python3
"""Generate an ephemeral, empty-password fixture key through pinned Zarf's PTY prompt."""
import os
import pathlib
import pty
import select
import subprocess
import sys
import time

binary, directory = sys.argv[1:]
root = pathlib.Path(directory)
root.mkdir(mode=0o700, parents=True, exist_ok=False)
master, slave = pty.openpty()
environment = {"PATH": os.environ["PATH"], "HOME": str(root), "XDG_STATE_HOME": str(root / "state")}
process = subprocess.Popen([binary, "tools", "gen-key"], cwd=root, env=environment,
                           stdin=slave, stdout=slave, stderr=slave, close_fds=True)
os.close(slave)
seen = b""
answered = 0
prompts = [b"Private key password (empty for no password)", b"Private key password again (empty for no password)"]
deadline = time.monotonic() + 20
last_output = time.monotonic()
try:
    while time.monotonic() < deadline and process.poll() is None:
        if select.select([master], [], [], 0.05)[0]:
            try:
                block = os.read(master, 4096)
            except OSError:
                break
            if not block:
                break
            seen += block
            last_output = time.monotonic()
            for _ in range(block.count(b"\x1b[6n")):
                os.write(master, b"\x1b[1;1R")
        if answered < len(prompts) and prompts[answered] in seen and time.monotonic() - last_output > 0.25:
            os.write(master, b"\r")
            answered += 1
    result = process.wait(timeout=2)
    if result or answered != 2 or not (root / "cosign.key").is_file() or not (root / "cosign.pub").is_file():
        raise RuntimeError("pinned Zarf fixture-key generation failed")
finally:
    os.close(master)
    if process.poll() is None:
        process.kill()
        process.wait()
