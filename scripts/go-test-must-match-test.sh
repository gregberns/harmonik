#!/usr/bin/env bash
# Tests for scripts/go-test-must-match.sh. Each case names the claim it defends.
#
# Every claim here is about a SILENT failure, which is why the guard exists and
# why it needs its own tests. A guard that stops firing does not announce it.
# It reports green, exactly like the empty gate it was written to catch.
#
# No case runs a real `go test`. The subject runs whatever command it is given,
# so a `printf` that prints go's own markers is a complete and deterministic
# stand-in, and the suite stays under a second.

set -uo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
subject="$script_dir/go-test-must-match.sh"

failures=0

fail() {
    echo "FAIL: $1" >&2
    failures=$((failures + 1))
}

# run_subject sets $code to the subject's exit status and $captured to its
# output. It is deliberately NOT called through command substitution: that
# forks a subshell, and both variables would be discarded with it.
captured=""
code=""
run_subject() {
    captured=$("$subject" "$@" 2>&1)
    code=$?
}

# CLAIM: a normal green run stays green. The guard must not add failures.
run_subject printf 'ok  \tgithub.com/x/y\t1.4s\n' './internal/y/'
[ "$code" = "0" ] || fail "a clean run must pass, got exit $code"

# CLAIM: the hazard itself. go test exits 0 on an empty -run match, so the
# guard must turn that into a failure. This is the ec66da798 keeper case.
run_subject printf 'ok  \tgithub.com/x/y\t1.4s [no tests to run]\n' './internal/y/'
[ "$code" = "1" ] || fail "an empty -run match must fail, got exit $code"
case "$captured" in
*"no test in these packages"*) : ;;
*) fail "the empty-match failure must name the packages, got: $captured" ;;
esac
case "$captured" in
*"github.com/x/y"*) : ;;
*) fail "the empty-match failure must quote go's own line, got: $captured" ;;
esac

# CLAIM: a partly empty run fails too. This is the exact shape that hid the
# keeper gap — one package matched nothing, the other matched, exit stayed 0.
run_subject printf 'ok  \tgithub.com/x/keeper\t1.3s [no tests to run]\nok  \tgithub.com/x/cmd\t1.5s\n' './internal/keeper/' './cmd/harmonik/'
[ "$code" = "1" ] || fail "one empty package among several must fail, got exit $code"

# CLAIM: the guard reports the command's own failure, not its own. A red test
# run must not be re-labelled as an empty match.
run_subject sh -c 'echo "FAIL github.com/x/y"; exit 3'
[ "$code" = "3" ] || fail "a failing command must keep its exit code, got $code"
case "$captured" in
*"go-test-must-match.sh: FAIL"*) fail "a real test failure must not print the guard's own verdict" ;;
*) : ;;
esac

# CLAIM: a named package with no test files at all is the same empty gate.
run_subject printf '?   \tgithub.com/x/y\t[no test files]\n' './internal/y/'
[ "$code" = "1" ] || fail "a named package with no test files must fail, got exit $code"

# CLAIM: but a `...` wildcard is allowed to expand over leaves that carry no
# tests. Failing there would make the guard unusable on every subtree target.
run_subject printf '?   \tgithub.com/x/y/sub\t[no test files]\nok  \tgithub.com/x/y\t1.0s\n' './internal/y/...'
[ "$code" = "0" ] || fail "no-test-files under a ... wildcard must pass, got exit $code"

# CLAIM: THE PARTIAL SUBTREE MATCH IS GREEN. This is the case whose absence let
# a false red ship. `./internal/daemon/...` is four packages. A filter aimed at
# a test in internal/daemon leaves the siblings printing `[no tests to run]`
# while the run is healthy. The first version of this guard failed on ANY
# occurrence of that marker, which made `make test-e2e-real-claude` and `make
# capture-claude-fixtures` permanently red on a credentialed box. The case that
# used to sit here asserted that wrong behaviour and pinned the defect in place.
run_subject printf 'ok  \tgithub.com/x/daemon\t9.0s\nok  \tgithub.com/x/daemon/bootconfig\t0.2s [no tests to run]\nok  \tgithub.com/x/daemon/router\t0.4s [no tests to run]\n' -run TestFoo './internal/daemon/...'
[ "$code" = "0" ] || fail "a partial match under a ... wildcard must PASS, got exit $code"

# CLAIM: the same subtree fails when NOT ONE package ran. This is the real
# test-e2e-real-claude-reviewloop shape: the named test exists nowhere, so every
# package in the subtree reports.
run_subject printf 'ok  \tgithub.com/x/daemon\t0.3s [no tests to run]\nok  \tgithub.com/x/daemon/bootconfig\t0.2s [no tests to run]\nok  \tgithub.com/x/daemon/router\t0.4s [no tests to run]\n' -run TestGone './internal/daemon/...'
[ "$code" = "1" ] || fail "a subtree where no package ran must fail, got exit $code"

# CLAIM: a one-package subtree fails when that sole package reports. This is the
# real test-codex-live shape — ./internal/codextest/... is one package and it has
# no TestL3_ at all. Every package reported, so the rule above still catches it.
run_subject printf 'ok  \tgithub.com/x/y\t1.0s [no tests to run]\n' -run TestL3_ './internal/y/...'
[ "$code" = "1" ] || fail "a sole subtree package that reports must fail, got exit $code"

# CLAIM: the two markers are counted together against the package count, not
# separately. A subtree with one of each still passes while a third package runs.
run_subject printf 'ok  \tgithub.com/x/y\t1.0s\n?   \tgithub.com/x/y/a\t[no test files]\nok  \tgithub.com/x/y/b\t0.1s [no tests to run]\n' './internal/y/...'
[ "$code" = "0" ] || fail "a mixed-marker subtree with one live package must pass, got exit $code"

# CLAIM: a subtree that matched no package at all fails. Nothing ran, and no
# marker can say so because there is no package line to read.
run_subject printf 'no packages matched\n' './internal/nope/...'
[ "$code" = "1" ] || fail "a subtree matching no package must fail, got exit $code"

# CLAIM: the markers are read from go's own summary lines, never from a test's
# own output. Four of the wrapped recipes pass -v, so a test that prints the
# marker string would otherwise redden its target.
run_subject printf '=== RUN   TestFoo\n    foo_test.go:9: saw [no tests to run] in the child\n--- PASS: TestFoo (0.00s)\nok  \tgithub.com/x/y\t1.0s\n' -v -run TestFoo './internal/y/'
[ "$code" = "0" ] || fail "a marker inside test output must not fail the target, got exit $code"

# CLAIM: a `...` inside a -run PATTERN is not a package wildcard. Reading it as
# one would switch the no-test-files check off without a word, which is the same
# quiet failure this script exists to stop.
run_subject printf '?   \tgithub.com/x/y\t[no test files]\n' -run 'TestFoo.*Bar' './internal/y/'
[ "$code" = "1" ] || fail "a ... in a -run pattern must not excuse a package with no tests, got exit $code"

# CLAIM: a flag value is never read as a package. -run and its pattern are one
# unit, and a flag on its own says nothing about the package list. The live
# package here is what makes the subtree healthy — without it every package
# reported and the run would fail for the right reason, proving nothing.
run_subject printf '?   \tgithub.com/x/y/sub\t[no test files]\nok  \tgithub.com/x/y\t1.0s\n' -count=1 -run 'TestFoo' './internal/y/...'
[ "$code" = "0" ] || fail "a real ... package arg must still excuse no-test-files, got exit $code"

# CLAIM: the command's output reaches the caller. A gate whose output the guard
# swallowed would be worse than the gate it replaced.
run_subject printf 'ok  \tgithub.com/x/y\t1.4s\n'
case "$captured" in
*"github.com/x/y"*) : ;;
*) fail "the wrapped command's output must stream through, got: $captured" ;;
esac

# CLAIM: a streamed line that starts with "ok" is not a package. go separates
# the summary fields with a tab and test output does not. Counting a stray line
# as a package raises the package count without raising the marker count, which
# reads as "one package ran" and passes an empty subtree.
run_subject printf 'ok  fake line from a subprocess\nok  \tgithub.com/x/y\t1.0s [no tests to run]\n' './internal/y/...'
[ "$code" = "1" ] || fail "a tab-less line must not count as a package, got exit $code"

# CLAIM: a run with no package line at all fails, whether or not a wildcard is
# present. Nothing ran, and no marker can say so.
run_subject printf 'some unrelated chatter\n' './internal/y/'
[ "$code" = "1" ] || fail "a named-package run with no package line must fail, got exit $code"

# CLAIM: no arguments is a usage error, not a pass.
run_subject
[ "$code" = "2" ] || fail "no arguments must exit 2, got $code"

if [ "$failures" -ne 0 ]; then
    echo "go-test-must-match-test.sh: FAIL — $failures case(s)" >&2
    exit 1
fi
echo "go-test-must-match-test.sh: OK — the guard fails a gate that tests nothing"
