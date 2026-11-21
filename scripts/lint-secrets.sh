#!/usr/bin/env bash
# lint-secrets checks the repository's fnox configuration without a decryption key: every secret
# in a tracked fnox.toml has a description, every age-provider value is age ciphertext, no tracked
# file holds an age secret key, and git ignores fnox.local.toml. It needs git, jq, and mise's pinned
# mikefarah yq, reads no network, writes nothing, and exits non-zero on any failure.
# See README.md, "Secrets"
set -uo pipefail
cd "$(git rev-parse --show-toplevel)" || exit

fail=0
refuse() {
  echo "FAIL: $1" >&2
  fail=1
}

if ! command -v jq >/dev/null; then
  echo "FAIL: jq is not on PATH; run mise install" >&2
  exit 1
fi
if ! yq=$(mise which yq 2>/dev/null); then
  echo "FAIL: mise has no yq installed; pin yq in mise.toml and run mise install" >&2
  exit 1
fi

secrets_of() {
  "$yq" -p toml -o json '.' "$1" | jq -r '
    def age_providers($p): [($p // {}) | to_entries[] | select(.value.type == "age") | .key];
    def default_of($p): .default_provider // (($p // {}) | keys | if length == 1 then .[0] else null end);
    . as $c
    | [{scope: "secrets", secrets: $c.secrets, providers: $c.providers}]
      + [($c.profiles // {}) | to_entries[]
         | {scope: "profiles.\(.key).secrets", secrets: .value.secrets,
            providers: (($c.providers // {}) + (.value.providers // {}))}]
    | .[] | .scope as $scope | .providers as $p
    | ($c | default_of($p)) as $default
    | (.secrets // {}) | to_entries[]
    | (.value.provider // $default) as $provider
    | [$scope, .key,
       ((.value.description // "") | gsub("^\\s+|\\s+$"; "") | length > 0 | tostring),
       ((age_providers($p) | index($provider)) != null and (.value | has("value")) | tostring),
       ((.value.value // "") | startswith("fnox-age-batch-v1:")
         or ((.[0:32] | @base64d?) // "" | startswith("age-encryption.org/v1\n")) | tostring)]
    | @tsv'
}

check_config() {
  local file=$1 rows scope name described age ciphertext
  if ! rows=$(secrets_of "$file"); then
    refuse "$file does not parse as fnox TOML; fix the syntax yq reports above"
    return
  fi
  [[ -n $rows ]] || return
  while IFS=$'\t' read -r scope name described age ciphertext; do
    if [[ $described != true ]]; then
      refuse "$file: $scope.$name has no description; add description = \"<what it is, what reads it, how to generate your own>\" with fnox set $name --description"
    fi
    if [[ $age == true && $ciphertext != true ]]; then
      refuse "$file: $scope.$name's value is not age ciphertext; set it through fnox -c ./$file set $name, which encrypts it, and rotate the value if plaintext was ever committed"
    fi
  done <<<"$rows"
}

check_secret_keys() {
  local file
  while IFS= read -r file; do
    refuse "$file holds an age secret key; remove it from the file and from history, and generate a new identity, since the old one is exposed"
  done < <(git grep -lE 'AGE-SECRET-KEY-1[QPZRY9X8GF2TVDW0S3JN54KHCE6MUA7L]{58}')
}

check_local_ignored() {
  local name
  for name in fnox.local.toml .fnox.local.toml; do
    if ! git check-ignore -q --no-index "$name"; then
      refuse "git does not ignore $name; add $name to .gitignore"
    fi
  done
  while IFS= read -r name; do
    refuse "$name is tracked; git rm --cached $name, since it holds this machine's own values"
  done < <(git ls-files -- '*fnox.local.toml')
}

while IFS= read -r file; do
  check_config "$file"
done < <(git ls-files -- fnox.toml .fnox.toml '*/fnox.toml' '*/.fnox.toml')
check_secret_keys
check_local_ignored
exit "$fail"
