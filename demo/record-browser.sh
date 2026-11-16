#!/usr/bin/env bash
# Record a headed Chrome walkthrough with ffmpeg x11grab at 30 fps.
# The walkthrough's start() mark is video time zero.
# usage: demo/record-browser.sh NAME SPEC
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
name="${1:?name}"
spec="${2:?spec}"
spec_arg="${spec}"
if [[ "${spec}" == */* ]]; then
  spec_arg="$(basename "${spec}")"
fi
width=1920
height=1080
fps=30
display=":${DEMO_DISPLAY:-99}"
out="$(pwd)/demo/out"
work="${out}/browser-${name}"
rm -rf "${work}"
mkdir -p "${work}" "${out}/shots"

Xvfb "${display}" -screen 0 "${width}x${height}x24" -nolisten tcp >"${work}/xvfb.log" 2>&1 &
xvfb=$!
ffmpeg_pid=""
cleanup() {
  if [[ -n "${ffmpeg_pid}" ]] && kill -0 "${ffmpeg_pid}" 2>/dev/null; then
    kill -INT "${ffmpeg_pid}" 2>/dev/null || true
    wait "${ffmpeg_pid}" || true
  fi
  kill "${xvfb}" 2>/dev/null || true
}
trap cleanup EXIT
for _ in $(seq 50); do
  if [[ -S "/tmp/.X11-unix/X${display#:}" ]]; then
    break
  fi
  sleep 0.1
done

ffmpeg -nostdin -y -f x11grab -draw_mouse 0 -framerate "${fps}" -video_size "${width}x${height}" -i "${display}" \
  -c:v libx264 -preset veryfast -crf 18 -pix_fmt yuv420p "${work}/screen.mkv" >"${work}/ffmpeg.log" 2>&1 &
ffmpeg_pid=$!
for _ in $(seq 100); do
  if grep -q 'start: [0-9]' "${work}/ffmpeg.log"; then
    break
  fi
  sleep 0.1
done
if ! kill -0 "${ffmpeg_pid}" 2>/dev/null; then
  cat "${work}/ffmpeg.log" >&2
  echo "screen capture did not start" >&2
  exit 1
fi

status=0
if command -v google-chrome >/dev/null 2>&1 || command -v google-chrome-stable >/dev/null 2>&1; then
  export DEMO_BROWSER_CHANNEL="${DEMO_BROWSER_CHANNEL:-chrome}"
fi
DISPLAY="${display}" \
  DEMO_WINDOW="${width},${height}" \
  DEMO_EVENTS="${work}/events.json" \
  DEMO_SHOTS="${out}/shots" \
  NODE_PATH="$(pwd)/web/node_modules" \
  pnpm --dir web exec playwright test --config "$(pwd)/tests/playwright.demo.config.ts" "${spec_arg}" \
  || status=$?

kill -INT "${ffmpeg_pid}" 2>/dev/null || true
wait "${ffmpeg_pid}" || true
ffmpeg_pid=""

if [[ ! -f "${work}/events.json" ]]; then
  echo "the walkthrough wrote no events" >&2
  exit 1
fi
offset="$(python3 - "${work}/events.json" "${work}/ffmpeg.log" <<'PY'
import json, re, sys
events = json.load(open(sys.argv[1]))
started = float(next(event["value"] for event in events if event["kind"] == "start"))
capture = float(re.search(r"start: ([\d.]+)", open(sys.argv[2]).read()).group(1))
print(f"{max(started - capture, 0):.3f}")
PY
)"
ffmpeg -v error -y -ss "${offset}" -i "${work}/screen.mkv" -an -c:v libx264 -pix_fmt yuv420p -r "${fps}" "${out}/${name}.mp4"
cp "${work}/events.json" "${out}/${name}-events.json"
exit "${status}"
