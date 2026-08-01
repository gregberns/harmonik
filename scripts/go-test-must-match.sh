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
# reads the output and counts two markers against the number of packages:
#
#   [no tests to run]   The package compiled and the -run filter matched no
#                       test in it.
#
#   [no test files]     The package has no test files at all.
#
# THE RULE DEPENDS ON THE PACKAGE ARGUMENTS, and getting this wrong costs a
# false red, which is as bad as the false green.
#
#   Every argument names ONE package. Then the caller listed each package on
#   purpose, and a package that reported a marker contributed nothing to a list
#   the caller wrote by hand. Fail if ANY package reported.
#
#   Any argument carries a `...` wildcard. Then the caller asked for a subtree
#   and go chose the members. A subtree normally holds packages the filter was
#   never meant to match. `./internal/daemon/...` is four packages, and a filter
#   aimed at a test in `internal/daemon` leaves the other three printing
#   `[no tests to run]` while the run is perfectly healthy. Fail only when EVERY
#   package reported, which means no package ran a test at all.
#
# The first version of this script applied the strict rule to `[no tests to
# run]` under a wildcard too. That turned `make test-e2e-real-claude` and `make
# capture-claude-fixtures` permanently red on a credentialed box where the named
# test passes. Review caught it. Do not restore that rule.
#
# WHAT IT DOES NOT DO. It cannot see a package where the filter matched a test
# and that test called t.Skip. Go prints a plain `ok` for an all-skipped
# package, with no marker. A slot that skips is still a false green, and the
# only defense against it is to not write one.
#
# It also does not check that the filter matched the test you meant. A pattern
# that matches one unrelated test satisfies this script.
#
# Under a wildcard it cannot tell a healthy partial match from a subtree where
# the ONE package that matters went quiet and a sibling still ran. Telling those
# apart needs to know which package the caller meant, and the command line does
# not say. Name the package instead of the subtree when that matters.

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

# Does any PACKAGE argument ask for a subtree?
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

# Every pattern below is anchored to the shape of go's own per-package summary
# line. An unanchored search reads a test's OWN output, so a -v run of a test
# that prints the marker string would fail the target. The command line already
# carries -v on four of the wrapped recipes.
#
# The command exited 0, so every package summary is `ok` or `?`. There is
# exactly one such line per package.
#
# The package count demands a TAB, because go separates the summary fields with
# one and a streamed line of test output does not have one in that position.
# Count a stray line as a package and the count goes up without the marker count
# following it, which reads as "one package ran" and passes an empty subtree.
tab=$(printf '\t')
pkg_lines=$(grep -cE "^(ok|\\?)[^${tab}]*${tab}" "$out" || true)
empty_filter=$(grep -E '^ok[[:space:]].*\[no tests to run\]$' "$out" || true)
no_files=$(grep -E '^\?[[:space:]].*\[no test files\]$' "$out" || true)

count_lines() {
    [ -z "$1" ] && echo 0 && return
    printf '%s\n' "$1" | grep -c '' || true
}
reported=$(($(count_lines "$empty_filter") + $(count_lines "$no_files")))

# Both branches demand at least one package. A run that named no package tested
# nothing, whatever the markers say.
if [ "$pkg_lines" -gt 0 ]; then
    if [ "$wildcard" -eq 1 ]; then
        # A subtree is healthy as long as ONE package ran something.
        if [ "$reported" -lt "$pkg_lines" ]; then
            exit 0
        fi
    elif [ "$reported" -eq 0 ]; then
        exit 0
    fi
fi

echo "" >&2
echo "go-test-must-match.sh: FAIL — this command passed while testing nothing." >&2
echo "  command: $*" >&2

if [ "$pkg_lines" -eq 0 ]; then
    echo "  The command matched no package at all." >&2
elif [ "$wildcard" -eq 1 ]; then
    echo "  All $pkg_lines package(s) in the subtree reported. Not one ran a test:" >&2
fi

if [ -n "$empty_filter" ]; then
    echo "  The -run filter matched no test in these packages:" >&2
    # shellcheck disable=SC2001 # Prefixing EVERY line needs sed. ${v//} cannot reach the first one.
    echo "$empty_filter" | sed 's/^/    /' >&2
fi

if [ -n "$no_files" ]; then
    echo "  These packages have no test files at all:" >&2
    # shellcheck disable=SC2001 # Same as above.
    echo "$no_files" | sed 's/^/    /' >&2
fi

echo "  Fix the target, not this guard. The test it names was renamed, moved," >&2
echo "  or deleted, or the package list is wrong. go test exits 0 on an empty" >&2
echo "  match, so without this guard the gate reports green and asserts nothing." >&2
exit 1
