#!/bin/sh

# Usage: ./lint-api.sh [PROTO_IMPORTS] [PROTO_ROOT] [PROTO_FILES]

PROTO_IMPORTS="$1"
PROTO_ROOT="$2"
shift 2

api_linter_output=$(api-linter --set-exit-status "$PROTO_IMPORTS" --config "$PROTO_ROOT/api-linter.yaml" --output-format json "$@")
api_linter_status=$?

# Exit early if api-linter finds no issues
if [ $api_linter_status -eq 0 ]; then
    exit 0
fi

# Continue with the script if issues are found
# shellcheck disable=SC2016
jq_expr='
    map(select(.problems != [])
        | . as $file
        | .problems[]
        | {
            rule: .rule_doc_uri,
            location: "\($file.file_path):\(.location.start_position.line_number)"
        }
    )
    | group_by(.rule)
    | .[]
    | .[0].rule + ":\n"
      + (map("\t" + .location) | join("\n"))
'

gojq_output=$(echo "$api_linter_output" | gojq -r "$jq_expr")
gojq_status=$?

if [ $gojq_status -ne 0 ]; then
    echo "gojq processing failed" >&2
    exit $gojq_status
fi

echo "$gojq_output"
exit $api_linter_status
