#!/usr/bin/env bash
set -euo pipefail
root=$(cd "${1:-.}" && pwd)
stage=$(mktemp -d "$root/temporal/.stable-api.XXXXXX")
destination="$root/temporal/api"
backup="$stage/previous"
cleanup() {
  status=$?
  if [[ -d "$backup" && ! -e "$destination" ]]; then
    mv "$backup" "$destination" || { echo "restore failed; previous tree is at $backup" >&2; exit 1; }
  fi
  rm -rf "$stage"
  exit "$status"
}
trap cleanup EXIT
"${GO:-go}" -C "$root/cmd/generate-stable-protos" run . -root "$root" -out "$stage"
(cd "$stage" && "${BUF:-buf}" format -w --config '{"version":"v2","modules":[{"path":"."}]}' .)
if [[ -e "$destination" ]]; then mv "$destination" "$backup"; fi
mv "$stage/temporal/api" "$destination"
