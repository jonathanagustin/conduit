#!/usr/bin/env bash
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
hub="ghcr.io/example/hub@sha256:$(printf 'a%.0s' {1..64})"
spoke="ghcr.io/example/spoke@sha256:$(printf 'b%.0s' {1..64})"
render="$root/scripts/render-zarf-runtime.sh"
"$render" "$work/package" 0.0.0 "$hub" "$spoke" > "$work/render.log" 2>&1
grep -q '^apiVersion: zarf.dev/v1beta1$' "$work/package/zarf.gen.yaml"
grep -qF "$hub" "$work/package/zarf.gen.yaml"
grep -qF "$spoke" "$work/package/zarf.gen.yaml"
for input in values.yaml values.schema.json manifests/runtime.yaml; do
  cmp "$root/deploy/zarf/$input" "$work/package/$input"
done
if "$render" "$work/package" 0.0.0 "$hub" "$spoke" > "$work/existing.log" 2>&1; then
  echo 'renderer overwrote an existing output directory' >&2; exit 1
fi
[[ -s "$work/package/zarf.gen.yaml" ]]
ln -s "$work/package" "$work/link"
if "$render" "$work/link" 0.0.0 "$hub" "$spoke" > "$work/link.log" 2>&1; then
  echo 'renderer followed an existing output symlink' >&2; exit 1
fi
for image in ghcr.io/example/hub:latest "$hub,OTHER=injected" "$hub'" "\$(touch $work/injected)"; do
  if "$render" "$work/invalid" 0.0.0 "$image" "$spoke" > "$work/invalid.log" 2>&1; then
    echo 'renderer accepted a mutable or unsafe image reference' >&2; exit 1
  fi
  grep -q 'immutable registry digest reference' "$work/invalid.log"
  [[ ! -e "$work/invalid" && ! -e "$work/injected" ]]
done
cat > "$work/failing-zarf" <<'STUB'
#!/bin/sh
if [ "$1" = version ]; then echo v1.0.1; else exit 1; fi
STUB
chmod +x "$work/failing-zarf"
if ZARF_BIN="$work/failing-zarf" "$render" "$work/failed" 0.0.0 "$hub" "$spoke" > "$work/failed.log" 2>&1; then
  echo 'renderer accepted failed compilation' >&2; exit 1
fi
[[ ! -e "$work/failed" ]]
echo 'Runtime template compiles with native Zarf; unsafe inputs, overwrite, symlink and failed-build cases refused'
