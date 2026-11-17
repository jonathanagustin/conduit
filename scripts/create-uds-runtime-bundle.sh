#!/usr/bin/env bash
# Assemble a native bundle from one authenticated beta package. Publishes
# nothing; the outer bundle must be signed separately before deployment.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
architecture=${1:-amd64}
output=${2:-deploy/zarf}
[[ "$architecture" =~ ^(amd64|arm64)$ ]] || { echo 'unsupported bundle architecture' >&2; exit 1; }
version=$(tr -d '\n' < "$root/VERSION")
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'invalid package version' >&2; exit 1; }
zarf=${ZARF_BIN:-zarf}
uds=${UDS_BIN:-uds}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
archive="zarf-package-platform-runtime-${architecture}-${version}.tar.zst"
source=${UDS_PACKAGE_ARCHIVE:-"$root/deploy/zarf/$archive"}
[[ -f "$source" && ! -L "$source" ]] || { echo 'missing regular runtime package archive' >&2; exit 1; }
cp "$source" "$work/$archive"
cp "$root/deploy/zarf/"{bundle.uds.hcl,runtime-values.yaml} "$work/"
trust=(--certificate-identity "https://github.com/jonathanagustin/conduit/.github/workflows/platform-images.yml@refs/tags/v${version}" --certificate-oidc-issuer https://token.actions.githubusercontent.com)
if [[ -n "${UDS_PACKAGE_VERIFICATION_KEY_PATH:-}" ]]; then
  [[ -f "$UDS_PACKAGE_VERIFICATION_KEY_PATH" ]] || { echo 'missing package verification public key' >&2; exit 1; }
  cp "$UDS_PACKAGE_VERIFICATION_KEY_PATH" "$work/package-publisher.pub"
  trust=(--key "$work/package-publisher.pub")
  ruby -e '
    path = ARGV.fetch(0)
    text = File.read(path)
    pattern = /  # BEGIN PACKAGE TRUST\n.*?  # END PACKAGE TRUST/m
    abort "missing package trust block" unless text.match?(pattern)
    File.write(path, text.sub(pattern, "  signature_verification {\n    public_key = file(\"package-publisher.pub\")\n  }"))
  ' "$work/bundle.uds.hcl"
fi
"$zarf" package inspect definition "$work/$archive" --verify=always "${trust[@]}" \
  --architecture "$architecture" --log-level error --no-color > "$work/definition.yaml"
api=$(ruby "$root/scripts/zarf-lifecycle-definition.rb" api "$version" < "$work/definition.yaml")
[[ "$api" == zarf.dev/v1beta1 ]] || { echo 'native runtime bundle requires a beta package; legacy variables cannot receive its values file' >&2; exit 1; }
ruby "$root/scripts/zarf-lifecycle-definition.rb" hub "$version" < "$work/definition.yaml" > /dev/null
"$uds" --features NextMode bundle create "$work" --unsigned --architecture "$architecture" --tmp-dir "$work"
bundle="uds-bundle-conduit-${architecture}-${version}.tar.zst"
[[ -s "$work/$bundle" && ! -L "$work/$bundle" ]] || { echo 'UDS did not write the expected bundle' >&2; exit 1; }
mkdir -p "$output"
[[ ! -e "$output/$bundle" && ! -L "$output/$bundle" ]] || { echo 'bundle output already exists' >&2; exit 1; }
cp "$work/$bundle" "$output/$bundle"
printf '%s\n' "$output/$bundle"
