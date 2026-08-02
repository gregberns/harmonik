#!/usr/bin/env bash
# Checks the reviewer-result boundary without invoking an LLM.

set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
runner="$repo_root/.claude/skills/agent-reviewer/run"

require_accepts() {
    local name="$1"
    local verdict="$2"

    if ! printf '%s\n' "$verdict" | "$runner" --validate >/dev/null; then
        echo "agent-reviewer-run-test: expected $name to pass" >&2
        exit 1
    fi
}

require_rejects() {
    local name="$1"
    local verdict="$2"

    if printf '%s\n' "$verdict" | "$runner" --validate >/dev/null 2>&1; then
        echo "agent-reviewer-run-test: expected $name to fail" >&2
        exit 1
    fi
}

require_accepts approve '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"All checks pass."}'
require_accepts request_changes '{"schema_version":1,"verdict":"REQUEST_CHANGES","notes":"Add the missing test."}'
require_accepts block '{"schema_version":1,"verdict":"BLOCK","flags":["missing-tests"],"notes":"The required test is absent."}'

require_rejects pass_and_summary '{"schema_version":1,"verdict":"PASS","summary":"Looks good."}'
require_rejects missing_notes '{"schema_version":1,"verdict":"APPROVE","flags":[]}'
require_rejects wrong_schema_version '{"schema_version":"1","verdict":"APPROVE","notes":"Looks good."}'
require_rejects non_string_flags '{"schema_version":1,"verdict":"APPROVE","flags":[1],"notes":"Looks good."}'
require_rejects unknown_field '{"schema_version":1,"verdict":"APPROVE","notes":"Looks good.","summary":"Wrong key."}'

echo "agent-reviewer-run-test: passed"
