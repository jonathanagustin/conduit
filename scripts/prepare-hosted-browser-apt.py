#!/usr/bin/env python3
"""Select the canonical signed Ubuntu archive on ephemeral hosted runners only."""
import os
from pathlib import Path
import platform
import re
import stat
import sys

CANONICAL = "https://archive.ubuntu.com/ubuntu"
REGIONAL_ARCHIVE = re.compile(
    r"https?://[a-z]{2}(?:-[a-z]+)+-[0-9]+\.ec2\.archive\.ubuntu\.com/ubuntu(/?)(?=\s|$)"
)
HOSTED_ARCHIVE_MIRRORLIST = re.compile(
    r"(?<!\S)mirror\+file:/etc/apt/apt-mirrors\.txt(?=\s|$)"
)


def rewrite_sources(text):
    """Change source URI tokens only; retain suite, architecture and trust fields."""
    result = []
    uri_field = False
    for line in text.splitlines(keepends=True):
        if line.lstrip().startswith("#"):
            result.append(line)
            continue
        field = re.match(r"^([A-Za-z][A-Za-z-]*):", line)
        if field:
            uri_field = field.group(1).lower() == "uris"
        elif not line.strip() or not line[:1].isspace():
            uri_field = False
        if uri_field or re.match(r"^\s*deb(?:-src)?\s", line):
            line = REGIONAL_ARCHIVE.sub(lambda match: CANONICAL + match.group(1), line)
            line = HOSTED_ARCHIVE_MIRRORLIST.sub(CANONICAL, line)
        result.append(line)
    return "".join(result)


def prepare_sources(paths):
    changed = 0
    for path in paths:
        try:
            info = path.lstat()
        except FileNotFoundError:
            continue
        if not stat.S_ISREG(info.st_mode):
            raise ValueError(f"APT source must be a regular file: {path}")
        original = path.read_bytes()
        revised = rewrite_sources(original.decode("utf-8")).encode("utf-8")
        if revised != original:
            if path.read_bytes() != original:
                raise ValueError(f"APT source changed during preparation: {path}")
            path.write_bytes(revised)
            changed += 1
    return changed


def main():
    # Act and self-hosted/local machines must retain their operator's APT setup.
    if (os.environ.get("GITHUB_ACTIONS") != "true"
            or os.environ.get("RUNNER_ENVIRONMENT") != "github-hosted"
            or os.environ.get("ACT", "").lower() == "true"):
        print("Refusing APT preparation outside native GitHub-hosted Actions", file=sys.stderr)
        return 1
    if platform.system() != "Linux" or platform.machine() != "x86_64" or os.geteuid() != 0:
        print("APT preparation requires the hosted Linux x86_64 root setup step", file=sys.stderr)
        return 1
    if not re.search(r'^ID="?ubuntu"?$', Path("/etc/os-release").read_text(), re.MULTILINE):
        print("APT preparation requires Ubuntu", file=sys.stderr)
        return 1
    # Ubuntu owns these two standard locations; leave third-party feeds alone.
    paths = [Path("/etc/apt/sources.list"), Path("/etc/apt/sources.list.d/ubuntu.sources")]
    changed = prepare_sources(paths)
    print(f"Canonical Ubuntu archive preparation: changed {changed} source file(s); suites and signing keys retained")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
