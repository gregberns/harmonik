#!/usr/bin/env bash
# Run a `go test` command and fail if a package contributed zero tests.
#
# WHY THIS EXISTS. `go test -run <pattern> <pkg>` exits 0 when the pattern
# matches nothing. It prints `ok <pkg> 1.4s [no tests to run]` and returns
# success. A Makefile gate built on a `-run` filter therefore reports green
# for as long as the filter matches, and it also reports green the moment the
# test it names is renamed, moved to another package, or deleted. The gate does
# not get louder as it gets emptier. It goes silent.
#
# This is not a theory. Commit ec66da798 deleted the two keeper acceptance
# corpus registration files. From that commit until this one,
# `make test-keeper-conformance` printed
#
#     ok  github.com/gregberns/harmonik/internal/keeper  1.391s [no tests to run]
#     ok  github.com/gregberns/harmonik/cmd/harmonik      1.564s
#
# and exited 0. The keeper package ran nothing. The exit code stayed 0 because
# the second package on the command line still matched one test. The same
# commit's own review caught a near-miss of the same shape in `test-pi-live`,
# and the fix there was a hand-written grep in
# scripts/harnesspi-freeze-gate.sh that pins one target to one package. That
# grep defends one line. This script defends the class.
#
# USAGE.
#
#     scripts/go-test-must-match.sh go test -run 'TestFoo' ./internal/foo/
#     CODEX_LIVE=1 scripts/go-test-must-match.sh go test -run TestL3_ ./pkg/...
#
# It runs the command unchanged and streams the output. It returns the
# command's own exit code when the command fails. When the command succeeds it
# then reads the output and fails if either marker is present:
#
#   [no tests to run]   The package compiled and the -run filter matched no
#                       test in it. Always an error. This is the hazard above.
#
#   [no test files]     The package has no test files at all. An error only
#                       when every package argument names one package. A `...`
#                       wildcard normally expands over leaf packages that carry
#                       no tests, and that is not a defect.
#
# WHAT IT DOES NOT DO. It cannot see a package where the filter matched a test
# and that test called t.Skip. Go prints a plain `ok` for an all-skipped
# package, with no marker. A slot that skips is still a false green, and the
# only defense against it is to not write one.
#
# It also does not check that the filter matched the test you meant. A pattern
# that matches one unrelated test satisfies this script.

set -uo pipefail

if [ "$#" -eq 0 ]; then
    echo "go-test-must-match.sh: usage: go-test-must-match.sh <go test command...>" >&2
    exit 2
fi

out=$(mktemp)
trap 'rm -f "$out"' EXIT

"$@" 2>&1 | tee "$out"
status=${PIPESTATUS[0]}

if [ "$status" -ne 0 ]; then
    exit "$status"
fi

# A `...` in any package argument means the caller asked for a subtree. A leaf
# with no test files is expected there, so only the empty-filter marker counts.
#
# Read the package arguments only. A `-run` or `-bench` pattern is a regex, and
# `...` is legal in one. Counting it here would silently switch off the check
# below, which is the same class of quiet failure this script exists to stop.
wildcard=0
skip_next=0
for arg in "$@"; do
    if [ "$skip_next" -eq 1 ]; then
        skip_next=0
        continue
    fi
    case "$arg" in
    -run | -bench | -test.run | -test.bench) skip_next=1 ;;
    -*) : ;;
    *...*) wildcard=1 ;;
    esac
done

empty_filter=$(grep -F '[no tests to run]' "$out" || true)
no_files=""
if [ "$wildcard" -eq 0 ]; then
    no_files=$(grep -F '[no test files]' "$out" || true)
fi

if [ -z "$empty_filter" ] && [ -z "$no_files" ]; then
    exit 0
fi

echo "" >&2
echo "go-test-must-match.sh: FAIL — this command passed while testing nothing." >&2
echo "  command: $*" >&2

if [ -n "$empty_filter" ]; then
    echo "  The -run filter matched no test in these packages:" >&2
    # shellcheck disable=SC2001 # Prefixing EVERY line needs sed. ${v//} cannot reach the first one.
    echo "$empty_filter" | sed 's/^/    /' >&2
fi

if [ -n "$no_files" ]; then
    echo "  These named packages have no test files at all:" >&2
    # shellcheck disable=SC2001 # Same as above.
    echo "$no_files" | sed 's/^/    /' >&2
fi

echo "  Fix the target, not this guard. The test it names was renamed, moved," >&2
echo "  or deleted, or the package list is wrong. go test exits 0 on an empty" >&2
echo "  match, so without this guard the gate reports green and asserts nothing." >&2
exit 1
