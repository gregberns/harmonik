#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)
test_root=$(mktemp -d)
trap 'rm -rf "$test_root"' EXIT

tools_dir="$test_root/fake-tools"
state_dir="$test_root/state"
mkdir -p "$tools_dir" "$state_dir"

fake_linter="$tools_dir/golangci-lint"
cat > "$fake_linter" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

allow_parallel=0
report=
for arg in "$@"; do
    case "$arg" in
        --allow-parallel-runners) allow_parallel=1 ;;
        --output.json.path=*) report=${arg#*=} ;;
    esac
done

if [[ $allow_parallel -ne 1 ]]; then
    if ! mkdir "$FAKE_STATE/runner.lock" 2>/dev/null; then
        echo "runner lock already held" >&2
        exit 4
    fi
    trap 'rmdir "$FAKE_STATE/runner.lock" 2>/dev/null || true' EXIT
fi
[[ -n "$report" ]] || { echo "missing JSON report path" >&2; exit 2; }

: > "$FAKE_STATE/active.$BASHPID"
for _ in {1..100}; do
    active=$(find "$FAKE_STATE" -name 'active.*' -type f | wc -l | tr -d ' ')
    [[ $active -ge 2 ]] && break
    sleep 0.02
done
if [[ ${active:-0} -lt 2 ]]; then
    echo "lint invocations did not overlap" >&2
    exit 5
fi
: > "$FAKE_STATE/overlap.$BASHPID"
printf '{"Issues":[]}\n' > "$report"
printf '%s\n' "$report" > "$FAKE_STATE/report.$BASHPID"
exit "${FAKE_EXIT_AFTER_REPORT:-0}"
EOF
chmod +x "$fake_linter"

run_lint() {
    FAKE_STATE="$state_dir" make -s -C "$repo_root" lint-full-count TOOLS_DIR="$tools_dir"
}

run_lint > "$test_root/first.out" 2> "$test_root/first.err" &
first_pid=$!
run_lint > "$test_root/second.out" 2> "$test_root/second.err" &
second_pid=$!

status=0
wait "$first_pid" || status=$?
wait "$second_pid" || status=$?
if [[ $status -ne 0 ]]; then
    cat "$test_root/first.err" "$test_root/second.err" >&2
    exit "$status"
fi

[[ $(find "$state_dir" -name 'overlap.*' -type f | wc -l | tr -d ' ') -eq 2 ]]
grep -q '^full lint findings: 0$' "$test_root/first.out"
grep -q '^full lint findings: 0$' "$test_root/second.out"

# A valid report must not mask a linter process failure. Make commonly maps a
# failing recipe to its own nonzero status, so assert failure plus the original
# status in the diagnostic rather than requiring Make itself to return 7.
find "$state_dir" -name 'report.*' -type f -delete
set +e
FAKE_STATE="$state_dir" FAKE_EXIT_AFTER_REPORT=7 make -s -C "$repo_root" \
    lint-full-count TOOLS_DIR="$tools_dir" \
    > "$test_root/failure.out" 2> "$test_root/failure.err"
failure_status=$?
set -e
if [[ $failure_status -eq 0 ]]; then
    echo "valid JSON report masked fake linter exit 7" >&2
    exit 1
fi
grep -q 'golangci-lint failed (exit 7)' "$test_root/failure.err"
if grep -q '^full lint findings:' "$test_root/failure.out"; then
    echo "failed linter was incorrectly published as a valid count" >&2
    exit 1
fi
failure_report_record=$(find "$state_dir" -name 'report.*' -type f -print -quit)
failure_report=$(cat "$failure_report_record")
if [[ -e "$failure_report" ]]; then
    echo "lint report was not cleaned after linter failure: $failure_report" >&2
    exit 1
fi

echo "lint-full-count-concurrency-test: PASS"
