#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture_dir="$(mktemp -d)"
trap 'rm -rf "${fixture_dir}"' EXIT

cat >"${fixture_dir}/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
url="${!#}"
case "${url}" in
  */repos/example/target)
    printf '{"id":42,"full_name":"example/target"}\n'
    ;;
  */users/example/packages/container/example-package)
    printf '%s\n' "${PACKAGE_FIXTURE}"
    ;;
  *)
    echo "unexpected URL: ${url}" >&2
    exit 1
    ;;
esac
EOF
chmod +x "${fixture_dir}/curl"

run_verifier() {
  PATH="${fixture_dir}:${PATH}" \
    GH_TOKEN=test-token \
    GH_API_URL=https://api.example.test \
    PACKAGE_FIXTURE="$1" \
    "${root}/scripts/verify-ghcr-package-cutover.sh" \
    example/target example-package 7
}

valid='{"id":7,"visibility":"private","repository":{"id":42,"full_name":"example/target"}}'
run_verifier "${valid}" >/dev/null

if run_verifier '{"id":7,"visibility":"private","repository":{"id":41,"full_name":"example/retired"}}' >/dev/null 2>&1; then
  echo "verifier admitted a package linked to the retired repository" >&2
  exit 1
fi
if run_verifier '{"id":8,"visibility":"private","repository":{"id":42,"full_name":"example/target"}}' >/dev/null 2>&1; then
  echo "verifier admitted a replacement package identity" >&2
  exit 1
fi
if run_verifier '{"id":7,"visibility":"public","repository":{"id":42,"full_name":"example/target"}}' >/dev/null 2>&1; then
  echo "verifier admitted changed package visibility" >&2
  exit 1
fi

echo "GHCR package cutover verifier tests passed"
