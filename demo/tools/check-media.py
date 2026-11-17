#!/usr/bin/env python3
"""No-cluster media preflight. Renders one second of video and checks it."""
import json
import shutil
import subprocess
import tempfile
from pathlib import Path


def main() -> None:
    if shutil.which("ffmpeg") is None or shutil.which("ffprobe") is None:
        raise SystemExit("ffmpeg and ffprobe are required")
    with tempfile.TemporaryDirectory() as raw:
        path = Path(raw) / "preflight.mp4"
        subprocess.run(
            [
                "ffmpeg", "-y", "-f", "lavfi", "-i", "color=c=0x0e1a2b:s=1920x1080:r=30:d=1",
                "-c:v", "libx264", "-pix_fmt", "yuv420p", "-r", "30", str(path),
            ],
            check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
        )
        probe = json.loads(subprocess.check_output([
            "ffprobe", "-v", "error", "-select_streams", "v:0",
            "-show_entries", "stream=codec_name,width,height,pix_fmt,r_frame_rate",
            "-of", "json", str(path),
        ]))
        stream = probe["streams"][0]
        expected = {
            "codec_name": "h264", "width": 1920, "height": 1080,
            "pix_fmt": "yuv420p", "r_frame_rate": "30/1",
        }
        if any(stream.get(key) != value for key, value in expected.items()):
            raise SystemExit(f"preflight video differs: {stream}")
    print("media preflight passed")


if __name__ == "__main__":
    main()
