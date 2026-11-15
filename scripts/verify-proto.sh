#!/usr/bin/env bash
# Lint, compare the existing contract and verify committed stubs without editing them.
set -euo pipefail
root="$(git rev-parse --show-toplevel)"
cd "$root"
version=1.75.0
case "$(uname -s)/$(uname -m)" in
  Linux/x86_64) asset=buf-Linux-x86_64; digest=fad1e38d6909bbb583a4da81d8c02a01046e2cd663bde7e337dd24277393764b ;;
  Linux/aarch64) asset=buf-Linux-aarch64; digest=00eb190aa82df17f393e05b04889cc60a0b13b3259d336e21a87d5f23ab4fd26 ;;
  Darwin/arm64) asset=buf-Darwin-arm64; digest=f71b009ebd8530b12fd85b060c6c245e15d6e5110ccc8da48d8b6fa71e59330b ;;
  Darwin/x86_64) asset=buf-Darwin-x86_64; digest=ff6d0979143c661d59178a032c57be883b2ac03dc9bf7d00ffca222b0354bd46 ;;
  *) echo 'unsupported buf platform' >&2; exit 1 ;;
esac
export PLATFORM_VERIFY_CACHE="${PLATFORM_VERIFY_CACHE:-$root/.cache/platform-verify}"
cache="$PLATFORM_VERIFY_CACHE/proto-tools"
mkdir -p "$cache"
buf_bin="$cache/buf-$version"
verify_buf() { python3 -c 'import hashlib,pathlib,sys;sys.exit(hashlib.sha256(pathlib.Path(sys.argv[1]).read_bytes()).hexdigest()!=sys.argv[2])' "$1" "$digest"; }
if [[ ! -x "$buf_bin" ]] || ! verify_buf "$buf_bin"; then
  download="$(mktemp "$cache/buf.XXXXXXXX")"
  trap 'rm -f "$download"' EXIT
  curl --fail --silent --show-error --location --max-time 180 "https://github.com/bufbuild/buf/releases/download/v$version/$asset" --output "$download"
  verify_buf "$download"
  chmod 0755 "$download"
  mv "$download" "$buf_bin"
fi
export GOBIN="$cache/bin"
mkdir -p "$GOBIN"
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
go install connectrpc.com/connect/v2/cmd/protoc-gen-connect-go@v2.0.2
export PATH="$GOBIN:$root/web/node_modules/.bin:$PATH"
python3 scripts/test-normalize-connect-stubs.py
"$buf_bin" lint
base="${PROTO_BASE_SHA:-}"
if [[ -z "$base" ]]; then
  base="$(git merge-base HEAD refs/remotes/origin/main)"
  if [[ "$base" == "$(git rev-parse HEAD)" ]]; then base=HEAD^; fi
fi
base_sha="$(git rev-parse --verify "$base^{commit}")"
temporary="$(mktemp -d "${TMPDIR:-/tmp}/conduit-proto.XXXXXXXX")"
trap 'rm -rf "$temporary"' EXIT
mkdir -p "$temporary/base" "$temporary/generated"
git archive "$base_sha" buf.yaml proto | tar -xf - -C "$temporary/base"
"$buf_bin" breaking --against "$temporary/base"
export PROTO_VERIFY_TEMP="$temporary"
python3 - <<'PY'
import json,os,pathlib,subprocess,runpy
root=pathlib.Path.cwd(); target=pathlib.Path(os.environ['PROTO_VERIFY_TEMP'])/'generated'
config={'version':'v2','plugins':[
 {'local':'protoc-gen-go','out':str(target/'gen/go'),'opt':['paths=source_relative']},
 {'local':'protoc-gen-connect-go','out':str(target/'gen/go'),'opt':['paths=source_relative']},
 {'local':'protoc-gen-es','out':str(target/'web/src/api/gen'),'opt':['target=ts','import_extension=js']}]}
subprocess.run([str(pathlib.Path(os.environ['PLATFORM_VERIFY_CACHE'])/'proto-tools/buf-1.75.0'),'generate','--template',json.dumps(config)],check=True)
rewrite=runpy.run_path('scripts/normalize-connect-stubs.py')['rewrite']
for path in target.rglob('*'):
 if path.suffix in {'.go','.ts'}: rewrite(path)
go_files=list((target/'gen/go').rglob('*.go'))
if go_files: subprocess.run(['gofmt','-w',*map(str,go_files)],check=True)
subprocess.run(['pnpm','--dir','web','exec','vp','fmt',str(target/'web/src/api/gen')],check=True)
expected={str(path.relative_to(target)):path.read_bytes() for path in target.rglob('*') if path.is_file()}
actual={str(path.relative_to(root)) for directory in ['gen/go','web/src/api/gen'] for path in (root/directory).rglob('*') if path.suffix in {'.go','.ts'} and b'DO NOT EDIT.' in path.read_bytes()}
errors=[]
for path,body in expected.items():
 if not (root/path).is_file() or (root/path).read_bytes()!=body: errors.append(path)
errors+=sorted(actual-set(expected))
if errors: raise SystemExit('stale proto output; run task proto:\n'+'\n'.join(sorted(set(errors))))
print('Committed Go and TypeScript stubs match the proto source.')
PY
echo "Proto breaking baseline: $base_sha"
