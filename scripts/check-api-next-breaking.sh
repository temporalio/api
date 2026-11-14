#!/usr/bin/env bash
set -euo pipefail

baseline=${1:?baseline is required}
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

# Use the current selection rules even before the baseline has a buf.next.yaml.
if ! buf build "$baseline" --config "$(cat buf.next.yaml)" --output "$stage/baseline.binpb" 2>"$stage/error"; then
  if grep -Eq '^Failure: Module .* had no \.proto files$' "$stage/error"; then
    echo 'api_next has no baseline schema yet; next-to-next compatibility begins after its first landing.'
    exit 0
  fi
  cat "$stage/error" >&2
  exit 1
fi

buf breaking --config buf.next.yaml --against "$stage/baseline.binpb"
