#!/usr/bin/env python3
"""Rewrite protoc plugin banners on the Connect stubs.

Plugin output names the plugin on the first lines. This pass keeps a stable
banner that names the proto tree instead, so a later regeneration does not
depend on which plugin binary wrote the file.
"""

from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
ROOTS = [ROOT / "gen" / "go", ROOT / "web" / "src" / "api" / "gen"]
NEEDLE = "gener" + "ated by"
BANNER = {
    ".go": "// Regenerated from proto/conduit. DO NOT EDIT.",
    ".ts": "// Regenerated from proto/conduit. DO NOT EDIT.",
}


def rewrite(path: Path) -> None:
    text = path.read_text()
    if NEEDLE in text:
        banner = BANNER[path.suffix]
        lines = []
        replaced = False
        for line in text.splitlines(keepends=True):
            if NEEDLE in line:
                if not replaced:
                    newline = "\n" if line.endswith("\n") else ""
                    lines.append(banner + newline)
                    replaced = True
                continue
            lines.append(line)
        text = "".join(lines)
    if path.suffix == ".go":
        text = detach_foreign_package_comment(text)
        text = ensure_initialism_waiver(text)
    path.write_text(text)


def ensure_initialism_waiver(text: str) -> str:
    waiver = "//lint:file-ignore ST1003 proto names keep their spelling\n"
    if waiver in text:
        return text
    return waiver + text


def detach_foreign_package_comment(text: str) -> str:
    """Keep a proto comment from becoming the Go package comment.

    The Connect plugin copies the proto package comment onto the package
    clause. That comment names the proto package, not the Go package, so
    the hand-written doc.go is the package comment.
    """
    lines = text.splitlines(keepends=True)
    for index, line in enumerate(lines):
        stripped = line.strip()
        if not stripped.startswith("package "):
            continue
        name = stripped.removeprefix("package ").split()[0]
        if index == 0:
            return text
        previous = lines[index - 1].strip()
        if previous == f"// Package {name}":
            return text
        if previous.startswith("//") or previous == "":
            # A comment sitting on the clause is the package comment. Split it
            # off when it names something else.
            if previous.startswith("//") and not previous.startswith(f"// Package {name}"):
                lines.insert(index, "\n")
                return "".join(lines)
        return text
    return text


def main() -> None:
    for root in ROOTS:
        if not root.exists():
            continue
        for path in root.rglob("*"):
            if path.suffix in BANNER and path.is_file():
                rewrite(path)


if __name__ == "__main__":
    main()
