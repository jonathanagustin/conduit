#!/usr/bin/env bash
# Compile the owned package template into a fresh, caller-owned build directory.
set -euo pipefail
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
if [[ $# != 4 ]]; then
  echo 'usage: render-zarf-runtime.sh OUTPUT_DIRECTORY VERSION HUB_IMAGE SPOKE_AGENT_IMAGE' >&2
  exit 2
fi
output=$1
version=$2
hub=$3
spoke=$4
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([+-][A-Za-z0-9.+-]+)?$ ]] || { echo 'invalid package version' >&2; exit 1; }
for image in "$hub" "$spoke"; do
  [[ "$image" =~ ^[A-Za-z0-9._:/-]+@sha256:[a-f0-9]{64}$ ]] || { echo 'runtime image must be an immutable registry digest reference' >&2; exit 1; }
done
zarf=${ZARF_BIN-zarf}
[[ -n "$zarf" ]] || { echo 'ZARF_BIN must name a Zarf executable' >&2; exit 1; }
[[ $("$zarf" version) == v1.0.0 ]] || { echo 'requires qualified Zarf v1.0.0' >&2; exit 1; }
# Never overwrite an existing package or follow a caller-supplied output symlink.
mkdir -m 700 -- "$output"
complete=false
trap 'if [[ "$complete" != true ]]; then rm -rf -- "$output"; fi' EXIT
cp "$root/deploy/zarf/"{zarf.tpl.yaml,values.yaml,values.schema.json} "$output/"
cp -R "$root/deploy/zarf/manifests" "$output/"
"$zarf" dev template "$output/zarf.tpl.yaml" \
  --set "PLATFORM_VERSION=$version" --set "HUB_IMAGE=$hub" --set "SPOKE_AGENT_IMAGE=$spoke"
[[ -s "$output/zarf.gen.yaml" ]] || { echo 'Zarf did not produce a package definition' >&2; exit 1; }
complete=true
printf '%s\n' "$output/zarf.gen.yaml"
