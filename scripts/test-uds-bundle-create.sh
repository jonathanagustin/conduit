#!/usr/bin/env bash
# Runs the pinned `uds create` on deploy/zarf/uds-bundle.yaml and on the real
# udsconverge renderers' output, with no cluster or container runtime. The Go
# integration test below owns a disposable loopback registry, with no external
# registry access, and fails rather than skips when either CLI is missing.
#
# A bundle needs a built Zarf package, and building the real one pulls the Hub
# and Agent images. So this builds the same package definition with its
# `images` lists removed: every component, variable, action and manifest is the
# real one, and only the image layers are absent. That is enough for the UDS
# CLI to read the package, resolve the bundle's ref and optionalComponents
# against it, and assemble a bundle. `uds create` does not validate the bundle
# file against its schema (it accepts an unknown key), so the file's shape is
# held by scripts/verify-zarf-package.rb instead. What this adds is the CLI's
# own view: the bundle builds, the CLI loads uds-config.yaml, and every
# variable that file sets is one the built bundle lists.
#
# It is not a deployable bundle and proves nothing about images. The release
# bundle is created from the signed package in the publication workflow.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
: "${ZARF_BIN:?ZARF_BIN must name the pinned Zarf executable}"
: "${UDS_BIN:?UDS_BIN must name the pinned UDS CLI executable}"

# #835: schema validation alone does not establish that generated bundles
# build. Keep both production renderer entry points and the real pinned CLI in
# this gate. Run before isolating HOME below, retaining Go's normal module and
# build caches; each CLI subprocess gets its own isolated home inside the test.
(cd "$root" && CONDUIT_TEST_UDS_CREATE=1 ZARF_BIN="$ZARF_BIN" UDS_BIN="$UDS_BIN" \
  go test -count=1 -run '^TestRenderedBundlesCreateWithPinnedCLI$' -v ./internal/gitless/adapters/udsconverge)

version=$(tr -d '\n' < "$root/VERSION")
architecture=amd64
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export HOME="$work/home" ZARF_NO_PROGRESS=true
mkdir -p "$HOME"

cp -R "$root/deploy/zarf" "$work/package"
ruby -ryaml -e '
  path = ARGV.fetch(0)
  definition = YAML.safe_load(File.read(path))
  definition.fetch("components").each { |component| component.delete("images") }
  File.write(path, YAML.dump(definition))
' "$work/package/zarf.yaml"

hub="ghcr.io/example/platform-hub:${version}@sha256:$(printf 'a%.0s' {1..64})"
agent="ghcr.io/example/platform-spoke-agent:${version}@sha256:$(printf 'b%.0s' {1..64})"
"$ZARF_BIN" package create "$work/package" --architecture "$architecture" --confirm --skip-sbom \
  --output "$work/package" --no-color \
  --set "PLATFORM_VERSION=${version}" --set "HUB_IMAGE=${hub}" --set "SPOKE_AGENT_IMAGE=${agent}" \
  > "$work/zarf.log" 2>&1 || { cat "$work/zarf.log" >&2; echo 'zarf package create failed for the image-less package' >&2; exit 1; }
if [[ ! -s "$work/package/zarf-package-platform-runtime-${architecture}-${version}.tar.zst" ]]; then
  cat "$work/zarf.log" >&2
  echo "zarf package create did not write zarf-package-platform-runtime-${architecture}-${version}.tar.zst" >&2
  exit 1
fi

(cd "$work/package" && "$UDS_BIN" create . --architecture "$architecture" --confirm --no-color --no-progress --no-log-file \
  --uds-cache "$work/uds-cache" --tmpdir "$work") > "$work/uds.log" 2>&1 ||
  { cat "$work/uds.log" >&2; echo 'uds create failed' >&2; exit 1; }
bundle="$work/package/uds-bundle-conduit-${architecture}-${version}.tar.zst"
if [[ ! -s "$bundle" ]]; then
  cat "$work/uds.log" >&2
  echo "uds create reported success without writing ${bundle##*/}" >&2
  exit 1
fi

(cd "$work/package" && "$UDS_BIN" inspect "${bundle##*/}" --list-variables --architecture "$architecture" --no-color --no-progress --no-log-file \
  --uds-cache "$work/uds-cache" --tmpdir "$work") > "$work/variables.log" 2>&1 ||
  { cat "$work/variables.log" >&2; echo 'uds inspect --list-variables failed' >&2; exit 1; }
# The CLI finds uds-config.yaml beside the bundle and parses it strictly, so
# this is also the pinned CLI accepting the example's shape.
grep -q 'Using config file .*uds-config.yaml' "$work/variables.log" || {
  cat "$work/variables.log" >&2
  echo 'uds did not load the uds-config.yaml beside the bundle' >&2
  exit 1
}
mapfile -t configured < <(ruby -ryaml -e 'puts YAML.safe_load(File.read(ARGV.fetch(0))).fetch("variables").fetch("platform-runtime").keys' "$work/package/uds-config.yaml")
((${#configured[@]} > 0)) || { echo 'uds-config.yaml configures no variables' >&2; exit 1; }
for variable in "${configured[@]}"; do
  grep -qx -- "  - name: ${variable}" "$work/variables.log" || {
    cat "$work/variables.log" >&2
    echo "uds-config.yaml sets ${variable}, which the created bundle does not list as a variable" >&2
    exit 1
  }
done
echo "uds create assembled uds-bundle-conduit-${architecture}-${version}.tar.zst from the image-less package; it lists all ${#configured[@]} variables uds-config.yaml sets"
