#!/usr/bin/env bash
# Create the two operator users used for second-person approval in the local
# UDS Keycloak realm, and write their passwords to a file outside the
# repository.
#
# The file is created with mode 0600. This script never prints a password.
# Passwords already in the file are reused so a second run does not rotate
# them unless --rotate is passed.
set -euo pipefail

author_username="${UDS_AUTHOR_USERNAME:-conduit-author}"
reviewer_username="${UDS_REVIEWER_USERNAME:-conduit-reviewer}"
operator_group="${UDS_OPERATOR_GROUP:-conduit-operators}"
users_file="${CONDUIT_UDS_USERS_FILE:-${HOME}/.config/conduit/uds-local-users}"
keycloak_url="${UDS_KEYCLOAK_URL:-https://keycloak.uds.dev}"
realm="${UDS_KEYCLOAK_REALM:-uds}"
admin_realm="${UDS_KEYCLOAK_ADMIN_REALM:-master}"
admin_secret_namespace="${UDS_KEYCLOAK_ADMIN_NAMESPACE:-keycloak}"
admin_secret_name="${UDS_KEYCLOAK_ADMIN_SECRET:-keycloak-admin-password}"
kubectl_bin="${UDS_KUBECTL:-}"
dry_run=false
print_plan=false
rotate=false

usage() {
  cat <<EOF
Usage: $(basename "$0") [--print-plan] [--dry-run] [--rotate]

  --print-plan  Print usernames, group, and the users file path, then exit.
  --dry-run     Make no Keycloak changes and do not write the users file.
  --rotate      Replace passwords in the users file and in Keycloak.

The users file defaults to \$HOME/.config/conduit/uds-local-users and must
stay outside the repository. Passwords are never printed.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --print-plan) print_plan=true ;;
    --dry-run) dry_run=true ;;
    --rotate) rotate=true ;;
    -h|--help) usage; exit 0 ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

print_plan_lines() {
  printf 'author=%s\n' "${author_username}"
  printf 'reviewer=%s\n' "${reviewer_username}"
  printf 'group=%s\n' "${operator_group}"
  printf 'users_file=%s\n' "${users_file}"
  printf 'keycloak_url=%s\n' "${keycloak_url}"
  printf 'realm=%s\n' "${realm}"
}

if "${print_plan}" || "${dry_run}"; then
  print_plan_lines
  exit 0
fi

case "${users_file}" in
  */.git/*|*/deploy/*|*/scripts/*|*/docs/*)
    echo "users file path looks like a repository path; choose a location outside the repository" >&2
    exit 1
    ;;
esac

if [[ -z "${kubectl_bin}" ]]; then
  if command -v kubectl >/dev/null 2>&1; then
    kubectl_bin=kubectl
  elif command -v uds >/dev/null 2>&1; then
    kubectl_bin="uds zarf tools kubectl"
  else
    echo "kubectl is required to read the Keycloak admin secret" >&2
    exit 1
  fi
fi

read_field() {
  local key="$1"
  [[ -f "${users_file}" ]] || return 1
  awk -F= -v key="${key}" '$1 == key { print substr($0, index($0, "=") + 1); exit }' "${users_file}"
}

# Satisfy the uds realm policy while retaining 192 bits of random entropy.
# Check the entropy command separately: printf must never mask its failure.
generate_password() {
  local random
  random="$(openssl rand -hex 24)" || return 1
  if [[ ! "${random}" =~ ^[0-9a-f]{48}$ ]]; then
    echo "password entropy generation returned an invalid result" >&2
    return 1
  fi
  printf '%s%s' "${random}" '-#9aZ'
}

mkdir -p "$(dirname "${users_file}")"
author_password=""
reviewer_password=""
if [[ -f "${users_file}" && "${rotate}" == "false" ]]; then
  author_password="$(read_field AUTHOR_PASSWORD || true)"
  reviewer_password="$(read_field REVIEWER_PASSWORD || true)"
fi
if [[ -z "${author_password}" ]]; then
  author_password="$(generate_password)"
fi
if [[ -z "${reviewer_password}" ]]; then
  reviewer_password="$(generate_password)"
fi

tmp_file="$(mktemp)"
chmod 0600 "${tmp_file}"
umask 077
cat >"${tmp_file}" <<EOF
# Local Keycloak operators for the UDS cluster. Do not commit this file.
AUTHOR_USERNAME=${author_username}
AUTHOR_PASSWORD=${author_password}
REVIEWER_USERNAME=${reviewer_username}
REVIEWER_PASSWORD=${reviewer_password}
OPERATOR_GROUP=${operator_group}
EOF
chmod 0600 "${tmp_file}"
mv "${tmp_file}" "${users_file}"
chmod 0600 "${users_file}"

admin_user="$($kubectl_bin get secret -n "${admin_secret_namespace}" "${admin_secret_name}" -o jsonpath='{.data.username}' 2>/dev/null || true)"
if [[ -n "${admin_user}" ]]; then
  admin_user="$(printf '%s' "${admin_user}" | base64 -d)"
else
  admin_user="admin"
fi
admin_password_b64="$($kubectl_bin get secret -n "${admin_secret_namespace}" "${admin_secret_name}" -o jsonpath='{.data.password}')"
if [[ -z "${admin_password_b64}" ]]; then
  echo "could not read ${admin_secret_namespace}/${admin_secret_name}; bootstrap a Keycloak admin first" >&2
  exit 1
fi
admin_password="$(printf '%s' "${admin_password_b64}" | base64 -d)"

token_body="$(mktemp)"
chmod 0600 "${token_body}"
cleanup() { rm -f "${token_body}"; }
trap cleanup EXIT
printf 'grant_type=password&client_id=admin-cli&username=%s&password=%s' \
  "$(printf '%s' "${admin_user}" | jq -sRr @uri)" \
  "$(printf '%s' "${admin_password}" | jq -sRr @uri)" >"${token_body}"

token="$(curl -sS -f -X POST "${keycloak_url}/realms/${admin_realm}/protocol/openid-connect/token" \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-binary "@${token_body}" | jq -r .access_token)"
rm -f "${token_body}"
if [[ -z "${token}" || "${token}" == "null" ]]; then
  echo "failed to obtain a Keycloak admin token" >&2
  exit 1
fi

kc() {
  local method="$1" path="$2"
  shift 2
  curl -sS -f -X "${method}" "${keycloak_url}/admin/realms/${realm}${path}" \
    -H "Authorization: Bearer ${token}" \
    -H 'Content-Type: application/json' \
    "$@"
}

ensure_group() {
  local name="$1" encoded body id
  encoded="$(printf '%s' "${name}" | jq -sRr @uri)"
  body="$(kc GET "/groups?search=${encoded}" || echo '[]')"
  id="$(printf '%s' "${body}" | jq -r --arg name "${name}" '.[] | select(.name == $name) | .id' | head -1)"
  if [[ -z "${id}" || "${id}" == "null" ]]; then
    kc POST "/groups" --data "$(jq -n --arg name "${name}" '{name:$name}')" >/dev/null || true
    body="$(kc GET "/groups?search=${encoded}")"
    id="$(printf '%s' "${body}" | jq -r --arg name "${name}" '.[] | select(.name == $name) | .id' | head -1)"
  fi
  if [[ -z "${id}" || "${id}" == "null" ]]; then
    echo "failed to ensure Keycloak group ${name}" >&2
    exit 1
  fi
  printf '%s' "${id}"
}

ensure_user() {
  local username="$1" password="$2" encoded body id payload pw_file
  encoded="$(printf '%s' "${username}" | jq -sRr @uri)"
  body="$(kc GET "/users?username=${encoded}&exact=true" || echo '[]')"
  id="$(printf '%s' "${body}" | jq -r --arg username "${username}" '.[] | select(.username == $username) | .id' | head -1)"
  if [[ -z "${id}" || "${id}" == "null" ]]; then
    payload="$(jq -n --arg username "${username}" '{username:$username, enabled:true, emailVerified:true}')"
    kc POST "/users" --data "${payload}" >/dev/null
    body="$(kc GET "/users?username=${encoded}&exact=true")"
    id="$(printf '%s' "${body}" | jq -r --arg username "${username}" '.[] | select(.username == $username) | .id' | head -1)"
  fi
  if [[ -z "${id}" || "${id}" == "null" ]]; then
    echo "failed to ensure Keycloak user ${username}" >&2
    exit 1
  fi
  pw_file="$(mktemp)"
  chmod 0600 "${pw_file}"
  jq -n --arg password "${password}" '{type:"password", value:$password, temporary:false}' >"${pw_file}"
  kc PUT "/users/${id}/reset-password" --data-binary "@${pw_file}" >/dev/null
  rm -f "${pw_file}"
  printf '%s' "${id}"
}

add_user_to_group() {
  local user_id="$1" group_id="$2"
  kc PUT "/users/${user_id}/groups/${group_id}" >/dev/null
}

group_id="$(ensure_group "${operator_group}")"
author_id="$(ensure_user "${author_username}" "${author_password}")"
reviewer_id="$(ensure_user "${reviewer_username}" "${reviewer_password}")"
add_user_to_group "${author_id}" "${group_id}"
add_user_to_group "${reviewer_id}" "${group_id}"

echo "Wrote operator users to ${users_file}"
