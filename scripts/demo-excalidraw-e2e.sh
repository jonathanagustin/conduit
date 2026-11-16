#!/usr/bin/env bash
# A native packaging recording requires an admitted builder/inspector/package creator.
set -euo pipefail
cat >&2 <<'MESSAGE'
The native Excalidraw packaging demo is unavailable: workspace compute has no
admitted image builder, inspector or package creator. It returns
COMPUTE_NOT_CONFIGURED after Accept. No processes or packaging receipts were
created. See docs/operations/native-workspace-compute.md and issue #615.
MESSAGE
exit 69
