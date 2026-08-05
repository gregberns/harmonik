#!/usr/bin/env bash

set -euo pipefail

# reachability-gate-test.sh proves that scripts/reachability-gate.sh can fail.
#
# A gate that reports "nothing to do" is making a claim, and it can be wrong the
# same way a test can. So this does not check that the gate passes on a clean
# tree — that proves nothing. It adds a real production-unreachable function to
# a real package, runs the real gate, and requires the gate to name it. Then it
# checks the fail-closed paths: a missing tool, a missing baseline, and a
# malformed baseline must all stop the build rather than report success.

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)
gate=$script_dir/reachability-gate.sh
real_baseline=$script_dir/reachability.baseline

# The canary lives in a package the daemon links, so a function added here is
# genuinely in the program and genuinely unreachable. A package outside the
# production import graph would not prove anything.
canary_file=$repo_root/internal/lifecycle/zz_reachability_gate_canary.go
canary_test_file=$repo_root/internal/lifecycle/zz_reachability_gate_canary_test.go
canary_symbol='github.com/gregberns/harmonik/internal/lifecycle.reachabilityGateCanary'

tmp=$(mktemp -d "${TMPDIR:-/tmp}/harmonik-reachability-test.XXXXXX")
cleanup() {
    rm -f "$canary_file" "$canary_test_file"
    rm -rf "$tmp"
}
trap cleanup EXIT

fail() {
    echo "reachability-gate-test: FAILED: $*" >&2
    exit 1
}

cd "$repo_root"

# One configuration is enough to prove the gate can fail, and it halves the
# cost of a self-test that runs in the inner loop. The gate itself still
# measures both. Entries for the other config read as "left the set" and raise
# a notice, which is why the clean-tree case below accepts a notice.
REACHABILITY_CONFIGS="$(go env GOOS) $(go env GOARCH)"
export REACHABILITY_CONFIGS

[[ -f $real_baseline ]] || fail "no ratified baseline at $real_baseline"
[[ ! -e $canary_file ]] || fail "canary file already exists: $canary_file"
[[ ! -e $canary_test_file ]] || fail "canary test file already exists: $canary_test_file"

# 1. Break it on purpose. An unexported, uncalled function in a linked package
#    is exactly the shape this gate exists to catch.
cat >"$canary_file" <<'CANARY'
package lifecycle

// zz_reachability_gate_canary.go is written and deleted by
// scripts/reachability-gate-test.sh. It must never be committed.

// reachabilityGateCanary is unreachable from every production entry point.
func reachabilityGateCanary() string { return "canary" }
CANARY

if out=$(bash "$gate" 2>&1); then
    echo "$out" >&2
    fail "the gate passed with a production-unreachable function present"
fi
grep -q "$canary_symbol" <<<"$out" || {
    echo "$out" >&2
    fail "the gate failed but did not name $canary_symbol"
}

# 2. THE claim the whole gate rests on: a test does not make code reachable.
#    Every one of the five bugs this gate exists for had passing tests. If test
#    binaries were roots, the gate would go quiet on exactly the cases that
#    matter. Give the canary a caller in a _test.go file and require the gate to
#    keep naming it. Without this case, step 1 would pass identically even if
#    the roots were wrong.
cat >"$canary_test_file" <<'CANARYTEST'
package lifecycle

// zz_reachability_gate_canary_test.go is written and deleted by
// scripts/reachability-gate-test.sh. It must never be committed.

import "testing"

func TestReachabilityGateCanaryIsCalledFromATest(t *testing.T) {
	if reachabilityGateCanary() != "canary" {
		t.Fatal("canary changed")
	}
}
CANARYTEST

if ! go vet ./internal/lifecycle/ >/dev/null 2>&1; then
    fail "the canary pair does not compile, so the test-root case proves nothing"
fi
if out=$(bash "$gate" 2>&1); then
    echo "$out" >&2
    fail "a _test.go caller made the canary look reachable. Test binaries ARE roots"
fi
grep -q "$canary_symbol" <<<"$out" || {
    echo "$out" >&2
    fail "the gate stopped naming $canary_symbol once a test called it"
}

rm -f "$canary_file" "$canary_test_file"

# Confirm the mutation really applied and really reverted. An unverified
# mutation is evidence of nothing.
[[ ! -e $canary_file && ! -e $canary_test_file ]] || fail "a canary file survived cleanup"
bash "$gate" >/dev/null 2>&1 || fail "the gate did not recover after the canary was removed"

# 3. A name that leaves the unreachable set is a NOTICE, not a failure.
{ head -1 "$real_baseline"; echo "linux/amd64 example.com/nonexistent.Ghost"; grep -v '^#' "$real_baseline"; } >"$tmp/ghost.baseline"
if ! out=$(REACHABILITY_BASELINE=$tmp/ghost.baseline bash "$gate" 2>&1); then
    echo "$out" >&2
    fail "a stale baseline entry failed the gate instead of raising a notice"
fi
grep -q 'NOTICE' <<<"$out" || fail "a stale baseline entry produced no notice"

# 4. Fail-closed paths. Each must exit 2 — a setup problem is not a pass.
set +e
DEADCODE_BIN=/nonexistent/deadcode PATH=/usr/bin:/bin bash "$gate" >/dev/null 2>&1
[[ $? -eq 2 ]] || fail "a missing deadcode binary did not exit 2"

# An analysis that reports nothing is making a claim, and it can be wrong the
# same way a test can. A tool that succeeds and prints nothing must not read as
# "the tree is clean".
printf '#!/bin/sh\nexit 0\n' >"$tmp/silent-deadcode"
chmod +x "$tmp/silent-deadcode"
DEADCODE_BIN=$tmp/silent-deadcode bash "$gate" >/dev/null 2>&1
[[ $? -eq 2 ]] || fail "a deadcode run that printed nothing did not exit 2"

REACHABILITY_BASELINE=$tmp/absent.baseline bash "$gate" >/dev/null 2>&1
[[ $? -eq 2 ]] || fail "a missing baseline did not exit 2"

echo 'this line has three fields here' >"$tmp/malformed.baseline"
REACHABILITY_BASELINE=$tmp/malformed.baseline bash "$gate" >/dev/null 2>&1
[[ $? -eq 2 ]] || fail "a malformed baseline did not exit 2"
set -e

echo "reachability-gate-test: OK (the gate fails on a new unreachable function and fails closed on setup errors)"
