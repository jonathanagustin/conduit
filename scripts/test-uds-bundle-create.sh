#!/usr/bin/env bash
# Checks existing generated legacy bundles, executor trust/value contracts,
# and the owned native runtime bundle. The latter uses one image-free beta
# package signed with an ephemeral fixture key, retaining its real guards,
# manifests and schema. No cluster or production signing/publication occurs.
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

# The existing hosted/local Zarf gate must exercise the actual signature and
# configuration boundary too. Run before replacing HOME below so Go retains
# its verified toolchain/module/build caches. This proof isolates its own CLIs.
"$root/scripts/test-uds-executor.sh"
version=$(tr -d '\n' < "$root/VERSION")
architecture=amd64
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
export HOME="$work/home" ZARF_NO_PROGRESS=true
mkdir -p "$HOME"
hub="ghcr.io/example/platform-hub:${version}@sha256:$(printf 'a%.0s' {1..64})"
agent="ghcr.io/example/platform-spoke-agent:${version}@sha256:$(printf 'b%.0s' {1..64})"
"$root/scripts/render-zarf-runtime.sh" "$work/package" "$version" "$hub" "$agent" > "$work/render.log" 2>&1
ruby -ryaml -e '
  path = ARGV.fetch(0)
  definition = YAML.safe_load_file(path)
  definition.fetch("components").each { |component| component.delete("images") }
  File.write(path, YAML.dump(definition))
' "$work/package/zarf.gen.yaml"
"$ZARF_BIN" package create "$work/package/zarf.gen.yaml" --architecture "$architecture" --confirm --skip-sbom \
  --output "$work/package" --no-color > "$work/zarf.log" 2>&1 || { cat "$work/zarf.log" >&2; exit 1; }
package="$work/package/zarf-package-platform-runtime-${architecture}-${version}.tar.zst"
[[ -s "$package" ]] || { echo 'missing image-free beta package' >&2; exit 1; }
python3 "$root/scripts/lib/generate-fixture-key.py" "$ZARF_BIN" "$work/test-key"
# Ephemeral local fixture signing, never production keys or publication.
COSIGN_PASSWORD='' "$ZARF_BIN" package sign "$package" --signing-key "$work/test-key/cosign.key" --confirm > "$work/sign.log" 2>&1 || { cat "$work/sign.log" >&2; exit 1; }
UDS_PACKAGE_ARCHIVE="$package" UDS_PACKAGE_VERIFICATION_KEY_PATH="$work/test-key/cosign.pub" \
  "$root/scripts/create-uds-runtime-bundle.sh" "$architecture" "$work/output" > "$work/bundle.log" 2>&1 || { cat "$work/bundle.log" >&2; exit 1; }
bundle="$work/output/uds-bundle-conduit-${architecture}-${version}.tar.zst"
[[ -s "$bundle" ]] || { echo 'native bundle output missing' >&2; exit 1; }
"$UDS_BIN" --features NextMode bundle inspect "$bundle" --output json --tmp-dir "$work" > "$work/inspect.json" 2> "$work/inspect.log"
"${NODE_BIN:-node}" - "$work/inspect.json" "$version" <<'JS'
const fs = require('node:fs');
const value = JSON.parse(fs.readFileSync(process.argv[2], 'utf8'));
if (value.name !== 'conduit' || value.version !== process.argv[3] || value.packages.length !== 1 ||
    JSON.stringify(value.packages[0].valuesFiles) !== JSON.stringify(['runtime-values.yaml'])) {
  throw new Error('native bundle lost identity or its runtime values file');
}
JS
# Authenticated packaging must reject unsigned input before producing a bundle.
"$ZARF_BIN" package create "$work/package/zarf.gen.yaml" --architecture "$architecture" --confirm --skip-sbom \
  --output "$work/unsigned" --no-color > "$work/unsigned.log" 2>&1
if UDS_PACKAGE_ARCHIVE="$work/unsigned/zarf-package-platform-runtime-${architecture}-${version}.tar.zst" \
  UDS_PACKAGE_VERIFICATION_KEY_PATH="$work/test-key/cosign.pub" \
  "$root/scripts/create-uds-runtime-bundle.sh" "$architecture" "$work/rejected" > "$work/rejected.log" 2>&1; then
  echo 'native runtime bundle accepted an unsigned package' >&2; exit 1
fi
grep -q 'not signed' "$work/rejected.log"
[[ ! -e "$work/rejected/uds-bundle-conduit-${architecture}-${version}.tar.zst" ]]
python3 "$root/scripts/lib/generate-fixture-key.py" "$ZARF_BIN" "$work/wrong-key"
if UDS_PACKAGE_ARCHIVE="$package" UDS_PACKAGE_VERIFICATION_KEY_PATH="$work/wrong-key/cosign.pub" \
  "$root/scripts/create-uds-runtime-bundle.sh" "$architecture" "$work/wrong-key-output" > "$work/wrong-key.log" 2>&1; then
  echo 'native runtime bundle accepted the wrong publisher key' >&2; exit 1
fi
grep -qi 'signature' "$work/wrong-key.log"
[[ ! -e "$work/wrong-key-output/uds-bundle-conduit-${architecture}-${version}.tar.zst" ]]
mkdir "$work/alpha"
cat > "$work/alpha/zarf.yaml" <<ALPHA
apiVersion: zarf.dev/v1alpha1
kind: ZarfPackageConfig
metadata:
  name: platform-runtime
  version: '${version}'
components:
  - name: fixture
    required: true
    actions:
      onDeploy:
        before:
          - cmd: 'true'
ALPHA
"$ZARF_BIN" package create "$work/alpha" --confirm --skip-sbom --architecture "$architecture" --output "$work/alpha" > "$work/alpha-create.log" 2>&1
alpha="$work/alpha/zarf-package-platform-runtime-${architecture}-${version}.tar.zst"
COSIGN_PASSWORD='' "$ZARF_BIN" package sign "$alpha" --signing-key "$work/test-key/cosign.key" --confirm > "$work/alpha-sign.log" 2>&1
if UDS_PACKAGE_ARCHIVE="$alpha" UDS_PACKAGE_VERIFICATION_KEY_PATH="$work/test-key/cosign.pub" \
  "$root/scripts/create-uds-runtime-bundle.sh" "$architecture" "$work/alpha-output" > "$work/alpha-refusal.log" 2>&1; then
  echo 'native runtime bundle accepted legacy alpha package inputs' >&2; exit 1
fi
grep -q 'requires a beta package' "$work/alpha-refusal.log"
[[ ! -e "$work/alpha-output/uds-bundle-conduit-${architecture}-${version}.tar.zst" ]]
echo "Native UDS assembled one authenticated image-free beta package with runtime-values.yaml; unsigned, wrong-key and alpha inputs refused. No cluster exercised."
