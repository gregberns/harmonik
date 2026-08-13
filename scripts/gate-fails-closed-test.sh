#!/usr/bin/env bash
# gate-fails-closed-test.sh — proves `make fast` and `make full` cannot approve
# work that did not pass.
#
# The gate this replaced (scripts/scenario-gate.sh) had five ways to allow a
# merge that never went green: a compile failure, a timeout, a signal kill, an
# exit code it did not recognise, and a retry that allowed when the second run
# passed. It had one way to block. This test exists so that shape cannot come
# back without the build going red.
#
# Two kinds of evidence, because either one alone can pass for the wrong reason.
#
#   BEHAVIOURAL — put a stub `go` first on PATH, make it exit with the code
#   under test, and run the REAL gate recipe. Assert two things together: the
#   gate exited non-zero, AND the stub was actually reached. The second half
#   matters. "make exited non-zero" is satisfied for free by a gate that died
#   before it ran anything, which is exactly how an unverified probe reports a
#   pass while measuring nothing.
#
#   STRUCTURAL — expand the whole step list with `make -n` and refuse any
#   status-swallowing construct in it. A behavioural probe only covers the step
#   it happened to hit. The structural pass covers every step there is.
#
# The recipes under test run the command through `timeout` and through
# scripts/with-lane-gocache.sh. Those wrappers are where an exit code could
# quietly change, so the behavioural cases drive the real wrapped recipe rather
# than a bare `go test`.

set -uo pipefail

# Recursion guard. `make full` runs script-tests, which runs this file, and the
# behavioural cases below run `make full`. Without a guard that never returns.
# The OUTER invocation does all the work; the nested one has nothing to add.
if [ -n "${HARMONIK_GATE_SELFTEST:-}" ]; then
    echo "gate-fails-closed-test: nested under the gate; the outer run holds the assertions"
    exit 0
fi

repo_root=$(git rev-parse --show-toplevel) || {
    echo "gate-fails-closed-test: not inside a git worktree" >&2
    exit 1
}
cd "$repo_root" || exit 1

failures=0
assertions=0

fail() {
    printf 'gate-fails-closed-test: FAIL: %s\n' "$*" >&2
    failures=$((failures + 1))
}

pass() {
    printf 'gate-fails-closed-test: ok: %s\n' "$*"
}

# ---------------------------------------------------------------------------
# stub `go`
#
# Records every invocation to $GATE_STUB_MARKER, one subcommand per line, then
# exits with $GATE_STUB_RC. GATE_STUB_RC_AFTER, when set, is used from the
# second invocation onward — that is how the "a passing second run must not
# rescue a failing first run" case is built.
# ---------------------------------------------------------------------------
make_stub_dir() {
    local dir="$1"
    mkdir -p "$dir"
    cat >"$dir/go" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "${1:-<none>}" >>"$GATE_STUB_MARKER"
n=$(wc -l <"$GATE_STUB_MARKER" | tr -d ' ')
if [ "$n" -gt 1 ] && [ -n "${GATE_STUB_RC_AFTER:-}" ]; then
    exit "$GATE_STUB_RC_AFTER"
fi
exit "${GATE_STUB_RC:-0}"
STUB
    chmod +x "$dir/go"
}

# run_gate <target> <injected-rc> [rc-after-first-call]
#
# Runs the real make target with the stub `go` first on PATH. Sets three
# globals rather than echoing, because a `$(...)` capture would run this in a
# subshell and the marker path would never reach the caller:
#   RUN_STATUS  make's exit status
#   RUN_CALLS   how many times the stub `go` was reached
#   RUN_DIR     the scratch directory, for the caller to remove
run_gate() {
    local target="$1" rc="$2" rc_after="${3:-}"
    RUN_DIR=$(mktemp -d "${TMPDIR:-/tmp}/gate-fc-XXXXXX")
    make_stub_dir "$RUN_DIR/bin"
    : >"$RUN_DIR/marker"
    env PATH="$RUN_DIR/bin:$PATH" \
        GATE_STUB_MARKER="$RUN_DIR/marker" \
        GATE_STUB_RC="$rc" \
        GATE_STUB_RC_AFTER="$rc_after" \
        HARMONIK_GATE_SELFTEST=1 \
        make "$target" >"$RUN_DIR/out" 2>&1
    RUN_STATUS=$?
    RUN_CALLS=$(wc -l <"$RUN_DIR/marker" | tr -d ' ')
}

# assert_blocks <label> <target> <injected-rc>
assert_blocks() {
    local label="$1" target="$2" rc="$3"
    assertions=$((assertions + 1))
    run_gate "$target" "$rc"
    if [ "$RUN_STATUS" -eq 0 ]; then
        fail "$label: 'make $target' exited 0 with a \`go\` that returns $rc"
    elif [ "$RUN_CALLS" -eq 0 ]; then
        fail "$label: 'make $target' exited $RUN_STATUS but never reached \`go\`, so it proves nothing"
        sed -n '$p' "$RUN_DIR/out" >&2
    else
        pass "$label: 'make $target' blocked (exit $RUN_STATUS) after reaching \`go\` $RUN_CALLS time(s)"
    fi
    rm -rf "$RUN_DIR"
}

# ---------------------------------------------------------------------------
# BEHAVIOURAL — the five approve-on-failure paths the old gate had.
#
# gate-test-compile is the probe target: it is one real recipe line, wrapped in
# `timeout` and in the lane-gocache wrapper exactly as the heavier steps are, so
# it exercises the same status path for a fraction of the wall time.
# ---------------------------------------------------------------------------
assert_blocks "a genuine test failure"          gate-test-compile 1
assert_blocks "a compile failure"               gate-test-compile 2
assert_blocks "a timeout"                       gate-test-compile 124
assert_blocks "a signal kill (OOM / SIGKILL)"   gate-test-compile 137
assert_blocks "an exit code nothing recognises" gate-test-compile 99

# The whole merge decision, not just one step of it.
assert_blocks "the merge decision as a whole"   full 1

# ---------------------------------------------------------------------------
# BEHAVIOURAL — no retry can rescue a failing run.
#
# The stub fails once and then succeeds forever. A gate with a flake-retry goes
# green here. This one must stay red, and it must have called `go` exactly once,
# which is the positive evidence that no second attempt was made at all.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
run_gate gate-test-compile 1 0
if [ "$RUN_STATUS" -eq 0 ]; then
    fail "a passing retry: 'make gate-test-compile' exited 0 after its first run failed"
elif [ "$RUN_CALLS" -ne 1 ]; then
    fail "a passing retry: \`go\` ran $RUN_CALLS times, so something retried the failing step"
else
    pass "a passing retry: blocked (exit $RUN_STATUS) and \`go\` ran exactly once — no retry exists"
fi
rm -rf "$RUN_DIR"

# ---------------------------------------------------------------------------
# BEHAVIOURAL — a dying `go test` outranks a report that parsed cleanly.
#
# THE CASE. `make fast` and `make full` render their run through
# tools/testreport. The renderer reads a `go test -json` stream, and a stream
# that was CUT SHORT still parses: every test that finished before the kill
# passed, so the report says so and exits 0. If the recipe took the renderer's
# word for it, a run killed by a timeout, an OOM or a signal would be reported
# as green. That is the fail-open shape this whole file exists to refuse, and
# the only thing standing against it is the go-test status outranking the
# report status.
#
# WHY IT NEEDS ITS OWN STUB. The stub above exits with one code for every
# subcommand, which never gets past `go build`. This one has to succeed at
# `go build` — and produce a renderer that really runs and really exits 0 —
# and then fail at `go test` while writing a completely valid all-passing
# stream. That is the exact combination the precedence check defends against
# and the only one that can tell it apart from its absence.
#
# WHAT MUTATION THIS CATCHES. Delete the `if [ "$TEST_STATUS" -ne 0 ]` block
# from RUN_TESTS_AND_REPORT in the Makefile. Every other assertion in this file
# still passes, `make -n` still shows no banned construct, and the gate starts
# approving killed runs. This case goes red.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
probe_dir=$(mktemp -d "${TMPDIR:-/tmp}/gate-fc-probe-XXXXXX")
mkdir -p "$probe_dir/bin"
cat >"$probe_dir/bin/go" <<'PROBESTUB'
#!/usr/bin/env bash
# Succeeds at `go build`, writing a renderer that exits 0 no matter what it is
# fed. Fails at `go test` AFTER emitting a valid stream in which every test
# passed — a run that died with its output looking perfectly healthy.
printf '%s\n' "${1:-<none>}" >>"$GATE_STUB_MARKER"
case "${1:-}" in
build)
    out=
    while [ "$#" -gt 0 ]; do
        if [ "$1" = "-o" ]; then out=$2; fi
        shift
    done
    [ -n "$out" ] || exit 3
    printf '#!/bin/sh\nexit 0\n' >"$out"
    chmod +x "$out"
    exit 0
    ;;
test)
    pkg=github.com/gregberns/harmonik/internal/sentinel
    printf '{"Action":"run","Package":"%s","Test":"TestOne"}\n' "$pkg"
    printf '{"Action":"pass","Package":"%s","Test":"TestOne","Elapsed":0.01}\n' "$pkg"
    printf '{"Action":"pass","Package":"%s","Elapsed":0.02}\n' "$pkg"
    # Killed here. The stream above is complete, well formed and entirely green.
    exit 137
    ;;
esac
exit 0
PROBESTUB
chmod +x "$probe_dir/bin/go"
: >"$probe_dir/marker"
env PATH="$probe_dir/bin:$PATH" \
    GATE_STUB_MARKER="$probe_dir/marker" \
    HARMONIK_GATE_SELFTEST=1 \
    make gate-test-report-probe >"$probe_dir/out" 2>&1
probe_status=$?
probe_calls=$(grep -c . "$probe_dir/marker" 2>/dev/null || echo 0)

# The recipe exits 137 and make reports its own 2 for any failed recipe, so the
# status alone cannot show WHICH failure won. The message the precedence block
# prints is the evidence, and it names the number it acted on. Both halves are
# required: a non-zero exit, and the go-test status being the reason for it.
if [ "$probe_status" -eq 0 ]; then
    fail "a killed run with a green partial stream: the test step exited 0, so a dead run reads as a pass"
    cat "$probe_dir/out" >&2
elif ! grep -q '^test$' "$probe_dir/marker"; then
    fail "a killed run with a green partial stream: \`go test\` was never reached (calls: $probe_calls), so this proves nothing"
    cat "$probe_dir/out" >&2
elif ! grep -q 'go test exited 137' "$probe_dir/out"; then
    fail "a killed run with a green partial stream: blocked (exit $probe_status) but NOT because of the go-test status — the report status won, or the precedence check is gone"
    cat "$probe_dir/out" >&2
else
    pass "a killed run with a green partial stream: blocked because \`go test\` exited 137 — the go-test status outranks the report"
fi
rm -rf "$probe_dir"

# ---------------------------------------------------------------------------
# STRUCTURAL — no step of either target may swallow a status.
#
# `make -n` expands variables and recurses into the sub-makes, so this reads the
# real step list rather than the Makefile text. Anything that can turn a failure
# into an exit 0 is refused by name.
# ---------------------------------------------------------------------------
banned_pattern='\|\| true|\|\| exit 0|\|\| :|; *true$|set \+e|--issues-exit-code=0|continue-on-error'

for target in fast full; do
    assertions=$((assertions + 1))
    steps=$(HARMONIK_GATE_SELFTEST=1 make -n "$target" 2>/dev/null)
    if [ -z "$steps" ]; then
        fail "structural: 'make -n $target' produced nothing, so nothing was checked"
        continue
    fi
    # No exceptions and no exclusion list. Every allowance here is a place a
    # future step can hide, and two "harmless" ones were already in this
    # Makefile when this check was written. Both were rewritten to not need the
    # construct rather than added to a list.
    # A line that is entirely a shell comment cannot change a status, and the
    # Makefile's own prose about these constructs would otherwise match itself.
    # That is a shape test, not an exclusion list.
    offenders=$(printf '%s\n' "$steps" | grep -vE '^[[:space:]]*#' | grep -nE "$banned_pattern")
    if [ -n "$offenders" ]; then
        fail "structural: 'make $target' has step(s) that can swallow a failure:"
        printf '%s\n' "$offenders" >&2
    else
        pass "structural: every step of 'make $target' propagates its exit status"
    fi
done

# ---------------------------------------------------------------------------
# STRUCTURAL — the checks that refuse a commit must actually be CALLED.
#
# WHY THIS SHAPE OF ASSERTION. scripts/secret-scan.sh had no caller for twenty
# days, from 2026-07-23 to 2026-08-12. `make -n fast`, `make -n full`, `make -n
# gate-static` and `make -n core` held zero occurrences of its name; the only
# invocation anywhere was a leaf `make secret-scan` target that nothing depended
# on, while four documents said it ran via the gates. It had no unit test at all
# for that whole span, and when one was written it found the scan admitting a
# key — so "a working scan that nobody called", which an earlier draft of this
# comment said, was wrong twice over. Either way no test OF a script can tell
# whether anything calls the script. This reads the expanded step list, which is
# the only place that answer lives.
#
# It is written as a class, not as one instance: every entry below is a check
# whose whole value is that a gate runs it. The entries are not equally strong,
# though. Each is a substring match against the expanded step list, and the
# blind spot named in scripts/secret-scan.sh applies to all of them — a step
# make is told to ignore prints the same as one it obeys.
# ---------------------------------------------------------------------------
# runs_command <step-list> <needle> — true when a step that REALLY RUNS holds
# the needle.
#
# THE COMMENT STRIP IS THE WHOLE POINT. `make -n` prints a recipe's comment
# lines verbatim, so a gate switched off the ordinary way —
#
#     # scripts/secret-scan.sh --head-only
#
# — is still in the expanded step list, and a plain name match calls it wired
# while it runs nothing. Deleting the line was caught and dropping the flag was
# caught; only the commented form went through, and commenting out is how a
# gate is switched off in practice. The strip now exists, so the state that
# demonstrated the gap cannot be reached from this tree and no assertion here
# reproduces it — do not read the paragraph above as a live measurement. What
# IS reproducible is the self-test below, which feeds runs_command a live step,
# a commented step, an indented commented step and an empty list, and reddens
# if the strip is put back to a plain name match.
#
# An empty step list is a refusal, not a pass. A here-string of an empty capture
# is one EMPTY line, so a match against it can look like an answer when nothing
# was read.
runs_command() {
    local steps="$1" needle="$2" live
    [ -n "$steps" ] || return 1
    live=$(printf '%s\n' "$steps" | grep -vE '^[[:space:]]*#')
    [ -n "$live" ] || return 1
    # A here-string, not a pipe. `grep -q` leaves at the first match and the
    # writer of a pipe would die of SIGPIPE, which `pipefail` reports as the
    # status of the whole pipeline — a MATCH coming back as a failure. The step
    # list is tens of kilobytes, which is exactly the size where that bites.
    grep -qF -- "$needle" <<<"$live"
}

wiring_case() {
    # wiring_case <target> <expected-substring> <why>
    local target="$1" needle="$2" why="$3"
    assertions=$((assertions + 1))
    local steps
    steps=$(HARMONIK_GATE_SELFTEST=1 make -n "$target" 2>/dev/null)
    if [ -z "$steps" ]; then
        fail "wiring: 'make -n $target' produced nothing, so nothing was checked"
        return
    fi
    if runs_command "$steps" "$needle"; then
        pass "wiring: 'make $target' runs '$needle' — $why"
    else
        fail "wiring: 'make $target' does NOT run '$needle'. $why"
    fi
}

# Every wiring assertion in this file rests on runs_command telling a live step
# from a commented one, so that is asserted rather than assumed. Four synthetic
# step lists, one behaviour each. Put the strip back to a plain name match and
# this goes red.
assertions=$((assertions + 1))
selftest_needle='scripts/secret-scan.sh --head-only'
selftest_live=$(printf 'go build ./...\n%s\ngo vet ./...\n' "$selftest_needle")
selftest_commented=$(printf 'go build ./...\n# %s\ngo vet ./...\n' "$selftest_needle")
selftest_indented=$(printf 'go build ./...\n\t  #%s\ngo vet ./...\n' "$selftest_needle")
selftest_broken=''
runs_command "$selftest_live" "$selftest_needle" \
    || selftest_broken="$selftest_broken a step that runs was not found;"
runs_command "$selftest_commented" "$selftest_needle" \
    && selftest_broken="$selftest_broken a commented-out step read as wired;"
runs_command "$selftest_indented" "$selftest_needle" \
    && selftest_broken="$selftest_broken an indented commented-out step read as wired;"
runs_command "" "$selftest_needle" \
    && selftest_broken="$selftest_broken an empty step list read as wired;"
if [ -n "$selftest_broken" ]; then
    fail "wiring: the wiring check itself cannot tell a live step from a comment —$selftest_broken every wiring assertion below is worthless"
else
    pass "wiring: the wiring check finds a step that runs, and refuses a commented one and an empty list"
fi

# The credential scan, at both ends. --head-only in the inner loop, because the
# commit is the tip and amending it costs nothing; --range in the merge
# decision, because a secret can arrive by merge from a lane whose own
# gate-static never ran.
#
# The flag is part of each assertion and not decoration. The script's DEFAULT
# scope is the staged index, and every gate here runs after the commit is made,
# when the ordinary flow leaves nothing staged. A bare `scripts/secret-scan.sh`
# in either target would read whatever a developer happened to leave in the
# index rather than the change under test. It is not a call site that CAN never
# find anything — stage a key and it does block — but it answers a question
# nobody asked, and it is silent when it answers nothing.
wiring_case gate-static 'scripts/secret-scan.sh --head-only' \
    'a credential added by the commit just made must fail the inner loop'
wiring_case full 'scripts/secret-scan.sh --range' \
    'the merge decision must read every line this branch adds, including whatever arrived by merge'

# The commit-message tip check, for the same reason: it is the only place a bad
# message or a fabricated review trailer fails a build.
wiring_case gate-static 'scripts/commit-msg-gate.sh --head-only' \
    'a fabricated review trailer on the commit just made must fail the inner loop'

# END TO END, against what `make -n` really prints rather than a synthetic list.
# Take the real gate-static expansion, comment out the credential scan in the
# copy exactly as a person switching it off would, and require the wiring check
# to say NO. The mutation is verified to have changed something first: a
# replacement that matched nothing would leave this "passing" while measuring
# nothing, which is the same defect the whole file is about.
assertions=$((assertions + 1))
e2e_needle='scripts/secret-scan.sh --head-only'
e2e_steps=$(HARMONIK_GATE_SELFTEST=1 make -n gate-static 2>/dev/null)
e2e_disabled=$(printf '%s\n' "$e2e_steps" \
    | awk -v n="$e2e_needle" '$0 == n { print "# " $0; next } { print }')
if [ -z "$e2e_steps" ]; then
    fail "wiring end-to-end: 'make -n gate-static' produced nothing, so nothing was checked"
elif [ "$e2e_disabled" = "$e2e_steps" ]; then
    fail "wiring end-to-end: commenting out '$e2e_needle' changed nothing, so this case measures nothing"
elif runs_command "$e2e_disabled" "$e2e_needle"; then
    fail "wiring end-to-end: '$e2e_needle' commented out in the real step list still reads as wired — the credential scan can be switched off with every assertion here green"
else
    pass "wiring end-to-end: '$e2e_needle' commented out in the real step list reads as NOT wired"
fi

# The gate that was removed must not come back through a side door. Go code
# names scripts as string literals, so a resurrected caller breaks at run time
# rather than at build time.
assertions=$((assertions + 1))
if [ -e scripts/scenario-gate.sh ]; then
    fail "scripts/scenario-gate.sh is back; it approves on compile-fail, timeout, signal-kill, an unknown exit code, and a passing retry"
else
    pass "scripts/scenario-gate.sh is gone"
fi

assertions=$((assertions + 1))
# Comment lines are excluded by SHAPE, not by filename: the Makefile and this
# file both explain why the script is gone, and that prose must stay allowed
# while a real caller in any of them stays refused.
resurrected=$(grep -rnF 'scenario-gate.sh' \
    Makefile workflow.dot sonnet-triple-review.dot specs .github scripts cmd/harmonik/assets \
    2>/dev/null | grep -vE ':[0-9]+: *(#|//|\*)' | grep -v 'gate-fails-closed-test.sh:')
if [ -n "$resurrected" ]; then
    fail "these still call the deleted scenario-gate.sh:"
    printf '%s\n' "$resurrected" >&2
else
    pass "nothing in the build, the workflow graphs, CI or the shipped assets calls scenario-gate.sh"
fi

# ---------------------------------------------------------------------------
printf 'gate-fails-closed-test: %d assertions, %d failed\n' "$assertions" "$failures"
if [ "$assertions" -lt 17 ]; then
    printf 'gate-fails-closed-test: only %d assertions ran; this file expects 17\n' "$assertions" >&2
    exit 1
fi
[ "$failures" -eq 0 ]
