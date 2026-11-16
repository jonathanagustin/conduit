# Packaging recording

`demo/record-browser.sh` records `tests/demo/packaging.demo.ts` in headed Chrome
on Xvfb. ffmpeg captures the display at 30 fps. The walkthrough's `start()`
mark is video time zero.

`demo/style.py` draws the title and end cards with the Conduit mark
(`demo/brand-mark.svg`) through FFmpeg's SVG decoder, Inter for the titles,
and JetBrains Mono for the digest line. `demo/assemble.sh` joins the clips at
1920x1080, H.264, yuv420p.

`demo/tools/check-media.py` renders one second of video and checks the codec.
It does not start a Hub or a cluster. Pull requests that touch this directory
run that preflight. The Demo Video workflow records and uploads the MP4 on
`workflow_dispatch`. Videos are not committed.

Start a Hub first:

```bash
PACKAGING_HUB_ONLY=1 CONDUIT_PACKAGING_BUILD=source \
  PACKAGING_REPO_URL=https://github.com/excalidraw/excalidraw \
  PACKAGING_REF=a2ec2889babf7d2295469c6d90ebe77fae57df84 \
  ./scripts/governed-packaging-run.sh
./demo/record-browser.sh packaging tests/demo/packaging.demo.ts
./demo/assemble.sh
```

`CONDUIT_MODEL_CMD` selects the writer. Unset, the Hub uses the deterministic
writer. A live model CLI is documented in `docs/operations/local-model-cli.md`.
