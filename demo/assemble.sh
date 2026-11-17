#!/usr/bin/env bash
# Join the title card, the browser capture, and the end card at 1920x1080.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
browser="${1:-demo/out/packaging.mp4}"
out="${2:-demo/out/conduit-excalidraw-packaging.mp4}"
mkdir -p demo/out
python3 demo/style.py card demo/out/title.mp4 "Conduit" "Governed packaging from source"
python3 demo/style.py card demo/out/end.mp4 "Pinned image" "Dockerfile, gates, and review stay in the workspace" "sha256 recorded by the engine"
scale="[0:v]scale=1920:1080:force_original_aspect_ratio=decrease,pad=1920:1080:(ow-iw)/2:(oh-ih)/2,setsar=1,fps=30[v]"
ffmpeg -y -i "${browser}" -filter_complex "${scale}" -map "[v]" -an -c:v libx264 -pix_fmt yuv420p demo/out/browser-scaled.mp4
title_d="$(ffprobe -v error -show_entries format=duration -of csv=p=0 demo/out/title.mp4)"
browser_d="$(ffprobe -v error -show_entries format=duration -of csv=p=0 demo/out/browser-scaled.mp4)"
offset1="$(python3 -c "print(max(float('${title_d}') - 0.4, 0))")"
offset2="$(python3 -c "print(max(float('${title_d}') + float('${browser_d}') - 0.8, 0))")"
ffmpeg -y -i demo/out/title.mp4 -i demo/out/browser-scaled.mp4 -i demo/out/end.mp4 \
  -filter_complex "[0:v][1:v]xfade=transition=fade:duration=0.4:offset=${offset1}[v1];[v1][2:v]xfade=transition=fade:duration=0.4:offset=${offset2}[v]" \
  -map "[v]" -an -c:v libx264 -pix_fmt yuv420p -r 30 "${out}"
echo "wrote ${out}"
