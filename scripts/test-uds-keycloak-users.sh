#!/usr/bin/env bash
# Exercises scripts/uds-keycloak-users.sh without a cluster or a live
# Keycloak. The users file must be mode 0600, and neither stdout nor stderr
# may contain a password.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
script="${root}/scripts/uds-keycloak-users.sh"

fail() {
  echo "$1" >&2
  exit 1
}

plan="$("${script}" --print-plan)"
printf '%s\n' "${plan}" | grep -qx -- 'author=conduit-author' || fail "default author must be conduit-author"
printf '%s\n' "${plan}" | grep -qx -- 'reviewer=conduit-reviewer' || fail "default reviewer must be conduit-reviewer"
printf '%s\n' "${plan}" | grep -qx -- 'group=conduit-operators' || fail "default group must be conduit-operators"
printf '%s\n' "${plan}" | grep -q -- 'users_file=' || fail "plan must include the users file path"
if printf '%s\n' "${plan}" | grep -Ei -- 'PASSWORD=|password=' >/dev/null; then
  fail "print-plan must not include passwords"
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
users="${work}/uds-local-users"
mkdir -p "${work}/bin" "${work}/state"
log="${work}/kubectl.log"

admin_password="stub-admin-password-NOT-FOR-USE"
admin_b64="$(printf '%s' "${admin_password}" | base64 | tr -d '\n')"

cat >"${work}/bin/kubectl" <<STUB
#!/usr/bin/env bash
printf '%s\n' "\$*" >>"${log}"
if [[ "\$*" == *jsonpath='{.data.username}'* ]]; then
  printf '%s' "admin" | base64 | tr -d '\n'
  exit 0
fi
if [[ "\$*" == *jsonpath='{.data.password}'* ]]; then
  printf '%s' "${admin_b64}"
  exit 0
fi
exit 1
STUB
chmod 0755 "${work}/bin/kubectl"

cat >"${work}/bin/curl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
method=GET
url=""
args=("$@")
i=0
while [[ $i -lt ${#args[@]} ]]; do
  case "${args[$i]}" in
    -X) i=$((i+1)); method="${args[$i]}" ;;
    -H|--data|--data-binary) i=$((i+1)) ;;
    -sS|-s|-S|-f|-k) ;;
    http*) url="${args[$i]}" ;;
  esac
  i=$((i+1))
done
printf '%s %s\n' "${method}" "${url}" >>"${CURL_LOG}"
joined="${args[*]}"
if [[ "${joined}" == *"stub-admin-password-NOT-FOR-USE"* ]]; then
  echo "admin password appeared on curl argv" >&2
  exit 1
fi
state="${CURL_STATE}"
mkdir -p "${state}"
case "${method} ${url}" in
  POST\ */realms/master/protocol/openid-connect/token)
    printf '%s' '{"access_token":"stub-token"}'
    ;;
  POST\ */groups)
    echo created >"${state}/group"
    printf '%s' '{"id":"group-1"}'
    ;;
  GET\ */groups?*)
    if [[ -f "${state}/group" ]]; then
      printf '%s' '[{"id":"group-1","name":"conduit-operators"}]'
    else
      printf '%s' '[]'
    fi
    ;;
  POST\ */users)
    if [[ ! -f "${state}/user-author" ]]; then
      echo created >"${state}/user-author"
    else
      echo created >"${state}/user-reviewer"
    fi
    printf '%s' ''
    ;;
  GET\ */users?*conduit-reviewer*)
    if [[ -f "${state}/user-reviewer" ]]; then
      printf '%s' '[{"id":"user-reviewer","username":"conduit-reviewer"}]'
    else
      printf '%s' '[]'
    fi
    ;;
  GET\ */users?*)
    if [[ -f "${state}/user-author" ]]; then
      printf '%s' '[{"id":"user-author","username":"conduit-author"}]'
    else
      printf '%s' '[]'
    fi
    ;;
  PUT\ */reset-password)
    printf '%s' ''
    ;;
  PUT\ */groups/*)
    printf '%s' ''
    ;;
  *)
    echo "unexpected curl ${method} ${url}" >&2
    exit 1
    ;;
esac
STUB
chmod 0755 "${work}/bin/curl"

export PATH="${work}/bin:${PATH}"
export CURL_LOG="${work}/curl.log"
export CURL_STATE="${work}/state"
: >"${CURL_LOG}"

output="$(
  CONDUIT_UDS_USERS_FILE="${users}" \
  UDS_KUBECTL=kubectl \
  UDS_KEYCLOAK_URL=https://keycloak.uds.dev \
    "${script}" 2>&1
)" || fail "users script failed: ${output}"

[[ -f "${users}" ]] || fail "users file was not written"
mode="$(stat -c '%a' "${users}")"
[[ "${mode}" == "600" ]] || fail "users file mode must be 600, got ${mode}"
grep -q '^AUTHOR_USERNAME=conduit-author$' "${users}" || fail "users file must name the author"
grep -q '^REVIEWER_USERNAME=conduit-reviewer$' "${users}" || fail "users file must name the reviewer"
grep -q '^AUTHOR_PASSWORD=.' "${users}" || fail "users file must contain an author password"
grep -q '^OPERATOR_GROUP=conduit-operators$' "${users}" || fail "users file must record the group"

author_pw="$(awk -F= '$1=="AUTHOR_PASSWORD"{print substr($0,index($0,"=")+1)}' "${users}")"
reviewer_pw="$(awk -F= '$1=="REVIEWER_PASSWORD"{print substr($0,index($0,"=")+1)}' "${users}")"
[[ -n "${author_pw}" && -n "${reviewer_pw}" ]] || fail "generated passwords must not be empty"
[[ "${author_pw}" != "${reviewer_pw}" ]] || fail "author and reviewer passwords must differ"

# The command output must not leak any of the secrets we know about.
if printf '%s\n' "${output}" | grep -F -- "${author_pw}" >/dev/null; then
  fail "author password appeared in command output"
fi
if printf '%s\n' "${output}" | grep -F -- "${reviewer_pw}" >/dev/null; then
  fail "reviewer password appeared in command output"
fi
if printf '%s\n' "${output}" | grep -F -- "${admin_password}" >/dev/null; then
  fail "admin password appeared in command output"
fi
if printf '%s\n' "${output}" | grep -Ei -- 'PASSWORD=' >/dev/null; then
  fail "command output must not include password assignments"
fi

grep -q 'POST https://keycloak.uds.dev/realms/master/protocol/openid-connect/token' "${CURL_LOG}" ||
  fail "script must request an admin token"
grep -q '/admin/realms/uds/groups' "${CURL_LOG}" || fail "script must create or fetch the group"
grep -q '/admin/realms/uds/users' "${CURL_LOG}" || fail "script must create the users"
grep -q 'reset-password' "${CURL_LOG}" || fail "script must set user passwords"
grep -q '/groups/group-1' "${CURL_LOG}" || fail "script must add users to the group"

# A second run without --rotate must keep the same passwords.
second="$(
  CONDUIT_UDS_USERS_FILE="${users}" \
  UDS_KUBECTL=kubectl \
  UDS_KEYCLOAK_URL=https://keycloak.uds.dev \
    "${script}" 2>&1
)" || fail "second run failed: ${second}"
author_pw2="$(awk -F= '$1=="AUTHOR_PASSWORD"{print substr($0,index($0,"=")+1)}' "${users}")"
[[ "${author_pw2}" == "${author_pw}" ]] || fail "second run must not rotate passwords"

# Reuse check: stdout of the second run still stays clean.
if printf '%s\n' "${second}" | grep -F -- "${author_pw}" >/dev/null; then
  fail "author password appeared on the second run"
fi

# Policy requirements are deterministic; the random portion carries 192 bits.
for password in "${author_pw}" "${reviewer_pw}"; do
  [[ "${password}" =~ ^[0-9a-f]{48}-#9aZ$ ]] || fail "password must retain 192 random bits and satisfy the realm policy"
done

# An entropy failure or malformed success must fail before creating credentials
# or contacting Keycloak. Rotation must also preserve the existing users file.
cp "${users}" "${work}/original-users"
for mode in failure empty short; do
  cat >"${work}/bin/openssl" <<STUB
#!/usr/bin/env bash
case '${mode}' in
  failure) printf '%s' partial; exit 19 ;;
  empty) exit 0 ;;
  short) printf '%s' abc; exit 0 ;;
esac
STUB
  chmod 0755 "${work}/bin/openssl"
  : >"${CURL_LOG}"
  failed_users="${work}/failed-${mode}"
  if CONDUIT_UDS_USERS_FILE="${failed_users}" UDS_KUBECTL=kubectl UDS_KEYCLOAK_URL=https://keycloak.uds.dev "${script}" >"${work}/entropy-output" 2>&1; then
    fail "invalid entropy must refuse credential creation"
  fi
  [[ ! -e "${failed_users}" ]] || fail "invalid entropy wrote a users file"
  [[ ! -s "${CURL_LOG}" ]] || fail "invalid entropy contacted Keycloak"
  if CONDUIT_UDS_USERS_FILE="${users}" UDS_KUBECTL=kubectl UDS_KEYCLOAK_URL=https://keycloak.uds.dev "${script}" --rotate >"${work}/entropy-output" 2>&1; then
    fail "invalid entropy must refuse rotation"
  fi
  cmp -s "${users}" "${work}/original-users" || fail "failed rotation changed existing credentials"
  [[ ! -s "${CURL_LOG}" ]] || fail "failed rotation contacted Keycloak"
done

echo "UDS Keycloak operator users script verified (policy, reuse, secrecy and entropy failure/rotation controls)"
