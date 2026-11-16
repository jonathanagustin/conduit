#!/usr/bin/env python3
"""Branded title and end cards for the packaging recording.

The mark is demo/brand-mark.svg, drawn by FFmpeg's SVG decoder. Titles use
Inter and the digest line uses JetBrains Mono. Missing fonts fail the render.
"""
import shutil
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent
MARK = ROOT / "brand-mark.svg"
W, H, FPS = 1920, 1080, 30


def font(style: str) -> str:
    pattern = f"Inter:style={style}" if style != "mono" else "JetBrains Mono:style=Regular"
    found = subprocess.check_output(["fc-match", "-f", "%{file}", pattern], text=True).strip()
    name = Path(found).name.lower()
    if style == "mono":
        if "jetbrains" not in name:
            raise SystemExit(f"JetBrains Mono is not installed (fc-match returned {found})")
    elif "inter" not in name:
        raise SystemExit(f"Inter {style} is not installed (fc-match returned {found})")
    return found


def escape(text: str) -> str:
    return text.replace("\\", "\\\\").replace(":", "\\:").replace("'", "\\'")


def card(out: Path, title: str, subtitle: str, mono: str = "") -> None:
    if shutil.which("ffmpeg") is None:
        raise SystemExit("ffmpeg is required")
    semibold = font("SemiBold")
    medium = font("Medium")
    filters = (
        f"[1:v]scale=160:160[logo];"
        f"[0:v][logo]overlay=x=(W-w)/2:y=250,"
        f"drawtext=fontfile='{semibold}':text='{escape(title)}':fontsize=64:fontcolor=white:x=(w-text_w)/2:y=460,"
        f"drawtext=fontfile='{medium}':text='{escape(subtitle)}':fontsize=32:fontcolor=0xc5d4e8:x=(w-text_w)/2:y=550"
    )
    if mono:
        filters += (
            f",drawtext=fontfile='{font('mono')}':text='{escape(mono)}':fontsize=24:"
            f"fontcolor=0x8abbff:x=(w-text_w)/2:y=640"
        )
    out.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(
        [
            "ffmpeg", "-y", "-f", "lavfi", "-i", f"color=c=0x0e1a2b:s={W}x{H}:r={FPS}:d=4",
            "-i", str(MARK), "-filter_complex", filters, "-frames:v", str(FPS * 4),
            "-c:v", "libx264", "-pix_fmt", "yuv420p", "-r", str(FPS), str(out),
        ],
        check=True,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.PIPE,
        text=True,
    )


def main() -> None:
    if len(sys.argv) < 2:
        raise SystemExit("usage: style.py card OUT.png-or-mp4 TITLE SUBTITLE [MONO]")
    if sys.argv[1] != "card":
        raise SystemExit("only card is implemented")
    card(Path(sys.argv[2]), sys.argv[3], sys.argv[4], sys.argv[5] if len(sys.argv) > 5 else "")


if __name__ == "__main__":
    main()
