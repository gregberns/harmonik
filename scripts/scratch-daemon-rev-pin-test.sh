#!/usr/bin/env bash
# scratch-daemon-rev-pin-test.sh — proves scripts/scratch-daemon.sh cannot audit
# a tree it has not been told to audit.
#
# THE DEFECT THIS HOLDS SHUT (hk-scratch-daemon-audits-wrong-tree-zljvm).
# scratch-daemon.sh is the instrument an external assessor points at a candidate
# commit. It had four separate ways to grade the wrong code and call it green:
#
#   1. `init` defaulted its clone source to `git remote get-url origin`. Cloning a
#      URL takes the remote's DEFAULT BRANCH, which is not the candidate commit.
#   2. `init` skipped the clone entirely when <scratch>/.git already existed, so a
#      second run graded whatever the first run left behind.
#   3. `git fetch`, `git checkout`, `git reset` and `git pull` appeared NOWHERE in
#      the file. Nothing ever moved the tree to a named commit, because the script
#      had no argument in which to name one.
#   4. `build` printed the tree's own HEAD, so a stale tree read as a deliberately
#      chosen revision rather than an accident.
#
# hk-xy9ym patched one caller — `make core-loop-lt` wipes its scratch and passes
# $(CURDIR) — and left DIRECT invocation, which is what the assessor's own
# operating contract tells it to use, completely uncovered.
#
# WHAT IS ASSERTED, AND WHY EACH CASE IS NEEDED. The cases are built so that no
# one of them passes for another one's reason:
#
#   REQUIRED     omitting --rev must fail. Without this the other cases could all
#                be satisfied by a script that still guesses when unasked.
#   EXACT        a run against a named commit must land on THAT commit. The named
#                commit is deliberately NOT the source's default branch head, so a
#                script that ignores --rev and clones the default branch fails
#                here. That is exactly defect 1.
#   NO-REUSE     an existing scratch tree must be a hard error, not a silent skip.
#                That is defect 2.
#   REUSE-FORCED --reuse must still move the tree to --rev. An opt-in that merely
#                tolerates the old tree re-opens defect 2 under a new name, so the
#                flag is only acceptable with this assertion beside it.
#   NAMED        every verdict must name the revision it came from. A build or a
#                batch summary that cannot say which commit it graded is defect 4.
#   UNPINNED     a tree this script did not place must be refused outright rather
#                than graded on its HEAD.
#   DRIFT        a tree moved after init must be refused. init verifying its own
#                work is not enough if anything can move HEAD afterwards.
#   STRUCTURAL   the script must actually contain the checkout it claims to do.
#                A behavioural case only covers the path it happened to take; this
#                covers the defect as originally measured — the count of
#                revision-moving git commands in the file, which was zero.
#
# The fixture is a real local git repo with two commits on two branches, which is
# the smallest thing that can tell "checked out what I asked for" apart from
# "cloned the default branch and got lucky". It is also a real, dependency-free
# Go module, so `build` genuinely compiles and genuinely writes its revision
# stamp — the value `up` and `batch` read. No daemon is ever STARTED: the one
# case that calls `up` swaps in a stub binary that fails at `project-hash`, so
# `up` stops before `tmux new-session`. Needs Go and git; no network, no tmux.
#
# WHAT THIS FILE CANNOT PROVE. The fixture is synthetic, so no case here runs the
# real `harmonik init`. The LOCAL EDITS cases therefore check that the exclusion
# list behaves correctly, NOT that the list is complete for the real binary. Its
# completeness rests on measuring a real scratch clone, and one entry
# (.claude/skills) was already missed that way once. Re-measure after any change
# to what `harmonik init --force` writes.
#
# Refs: hk-scratch-daemon-audits-wrong-tree-zljvm.

set -uo pipefail   # NOT -e: assertions report and keep going.

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
SD="bash $SELF_DIR/scratch-daemon.sh"

failures=0
assertions=0

fail() { printf 'rev-pin-test: FAIL: %s\n' "$*" >&2; failures=$((failures + 1)); }
pass() { printf 'rev-pin-test: ok: %s\n' "$*"; }

# assert_eq <want> <got> <label>
assert_eq() {
    assertions=$((assertions + 1))
    if [ "$1" = "$2" ]; then
        pass "$3"
    else
        fail "$3 (want '$1', got '$2')"
    fi
}

# A short /tmp base, not $TMPDIR: the same 104-byte unix-socket cap that
# scratch-daemon-smoke.sh documents applies to anything under a scratch path.
ROOT="$(mktemp -d "/tmp/hk-rp.XXXXXX")"
cleanup() { [ "${KEEP_DIR:-0}" = "1" ] && { echo "rev-pin-test: KEEP_DIR=1 — leaving $ROOT"; return; }; rm -rf "$ROOT" 2>/dev/null; }
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Fixture: a source repo whose DEFAULT BRANCH is deliberately not the commit
# under test.
#
#   main      A ── B      <- the default branch, and what a naive clone takes
#   candidate A ── C      <- the commit an audit would actually be pointed at
#
# marker.txt differs between B and C, so "which commit is checked out" is
# observable from the working tree and not only from `rev-parse`.
# ---------------------------------------------------------------------------
SRC="$ROOT/source"
mkdir -p "$SRC"
git -C "$SRC" init -q -b main
git -C "$SRC" config user.email "rev-pin-test@example.invalid"
git -C "$SRC" config user.name "rev-pin-test"
printf 'base\n' >"$SRC/marker.txt"
# A real, dependency-free Go module. `build` must actually SUCCEED here, because
# the revision stamp it writes is what `up` and `batch` read; a fixture that
# cannot compile would leave that stamp untested. No network is needed — the
# module has no requirements.
mkdir -p "$SRC/cmd/harmonik"   # cmd_build's checkout sniff looks for this path
printf 'module scratchfixture\n\ngo 1.21\n' >"$SRC/go.mod"
printf 'package main\n\nfunc main() {}\n' >"$SRC/cmd/harmonik/main.go"
# A Makefile with a `tools:` recipe, because init now provisions the pinned dev
# toolchain into the scratch clone before any daemon dispatches into it, and it
# refuses a tree that has none. It stands in for the real Makefile the same way
# .harmonik/config.yaml above stands in for the real config. `@:` is the shell
# no-op, so the two `go install` lines are read by the name derivation and run by
# nothing — this fixture must not want the network or minutes of wall time.
# scripts/scratch-daemon-toolchain-test.sh is where that step is really tested.
# /.tools matches the real repo's .gitignore entry: without it the installed
# binaries read as untracked local edits and every case here stamps
# +local-edits.
{
	printf 'TOOLS_DIR := $(shell pwd)/.tools\n\n'
	printf '.PHONY: tools\ntools:\n'
	printf '\t@mkdir -p $(TOOLS_DIR) && touch $(TOOLS_DIR)/alpha $(TOOLS_DIR)/beta && chmod +x $(TOOLS_DIR)/alpha $(TOOLS_DIR)/beta\n'
	printf '\t@: go install example.com/a/cmd/alpha@v1.0.0\n'
	printf '\t@: go install example.com/b/cmd/beta@v2.0.0\n'
} >"$SRC/Makefile"
printf '/.tools\n' >"$SRC/.gitignore"
# `harmonik init --force` rewrites both of these on every real scratch, so they
# stand in for init's own edits. .claude/skills is the one that is easy to miss:
# init re-provisions all ten embedded skills there unconditionally under --force,
# and those files are tracked.
printf 'agent instructions\n' >"$SRC/AGENTS.md"
mkdir -p "$SRC/.claude/skills/demo"
printf 'demo skill\n' >"$SRC/.claude/skills/demo/SKILL.md"
# A committed .harmonik/ so init takes the "already a harmonik project" path and
# skips the bootstrap. The bootstrap compiles the daemon, which this fixture is
# not and does not need to be: every case here is about which commit lands in the
# tree, and building a real binary would add a Go toolchain dependency and
# minutes of wall time without testing anything more.
mkdir -p "$SRC/.harmonik"
printf 'project: fixture\n' >"$SRC/.harmonik/config.yaml"
printf 'branching:\n  start_from: main\n  lands_on: main\n' >"$SRC/.harmonik/branching.yaml"
git -C "$SRC" add -A && git -C "$SRC" commit -qm "A: base"
COMMIT_A="$(git -C "$SRC" rev-parse HEAD)"

git -C "$SRC" checkout -q -b candidate
printf 'candidate\n' >"$SRC/marker.txt"
git -C "$SRC" commit -qam "C: the commit under audit"
COMMIT_C="$(git -C "$SRC" rev-parse HEAD)"

git -C "$SRC" checkout -q main
printf 'default-branch\n' >"$SRC/marker.txt"
git -C "$SRC" commit -qam "B: default branch moves on"
COMMIT_B="$(git -C "$SRC" rev-parse HEAD)"

echo "rev-pin-test: fixture main=$COMMIT_B candidate=$COMMIT_C"

# run_init <scratch> [args...] — run init, capture status + output.
# Sets RUN_STATUS and RUN_OUT (a path). Not a $(...) capture: the caller needs
# both, and a subshell would lose one.
run_init() {
    local scratch="$1"; shift
    RUN_OUT="$ROOT/init-$$-$RANDOM.out"
    $SD init "$scratch" "$@" >"$RUN_OUT" 2>&1
    RUN_STATUS=$?
}

# ---------------------------------------------------------------------------
# REQUIRED — omitting the revision fails instead of guessing.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
run_init "$ROOT/s-norev" --source "$SRC"
if [ "$RUN_STATUS" -eq 0 ]; then
    fail "REQUIRED: init with no --rev exited 0 — it guessed a revision"
elif ! grep -q -- '--rev' "$RUN_OUT"; then
    fail "REQUIRED: init with no --rev failed (exit $RUN_STATUS) but never mentioned --rev, so it failed for some other reason"
    cat "$RUN_OUT" >&2
elif [ -d "$ROOT/s-norev/.git" ]; then
    fail "REQUIRED: init with no --rev failed but still created a clone at $ROOT/s-norev"
else
    pass "REQUIRED: init refuses to run without --rev, and clones nothing"
fi

# A revision that does not exist must fail too. Otherwise "requires --rev" is
# satisfied by a script that accepts the flag and ignores its value.
assertions=$((assertions + 1))
run_init "$ROOT/s-badrev" --source "$SRC" --rev "no-such-ref"
if [ "$RUN_STATUS" -eq 0 ]; then
    fail "REQUIRED: init accepted --rev no-such-ref"
else
    pass "REQUIRED: init rejects a --rev that names no commit"
fi

# ---------------------------------------------------------------------------
# EXACT — a run against a named commit checks out THAT commit.
#
# The source's default branch is main (COMMIT_B). Asking for the candidate
# branch must produce COMMIT_C. A script that ignores --rev lands on COMMIT_B and
# fails here, which is the original defect stated as a test.
# ---------------------------------------------------------------------------
S1="$ROOT/s-exact"
run_init "$S1" --source "$SRC" --rev candidate
assert_eq 0 "$RUN_STATUS" "EXACT: init --rev candidate succeeded"
assert_eq "$COMMIT_C" "$(git -C "$S1" rev-parse HEAD 2>/dev/null)" \
    "EXACT: HEAD is the requested commit, not the source's default branch"
assert_eq "candidate" "$(cat "$S1/marker.txt" 2>/dev/null)" \
    "EXACT: the WORKING TREE holds the requested commit's content"
assert_eq "$COMMIT_C" "$(cut -f1 <"$S1/.harmonik/audit-revision" 2>/dev/null)" \
    "EXACT: init recorded the audit revision"

# The same by raw SHA, not only by branch name. An assessor is given a commit.
S2="$ROOT/s-exact-sha"
run_init "$S2" --source "$SRC" --rev "$COMMIT_A"
assert_eq 0 "$RUN_STATUS" "EXACT: init --rev <sha> succeeded"
assert_eq "$COMMIT_A" "$(git -C "$S2" rev-parse HEAD 2>/dev/null)" \
    "EXACT: a raw SHA checks out that SHA"
assert_eq "base" "$(cat "$S2/marker.txt" 2>/dev/null)" \
    "EXACT: a raw SHA leaves that SHA's content in the working tree"

# ---------------------------------------------------------------------------
# NAMED — the verdict names the revision.
# ---------------------------------------------------------------------------
# Redirect to a file rather than piping into grep. Under `pipefail` a matching
# `grep -q` exits early, the writer takes SIGPIPE, and the pipeline reports
# failure even though the text was found — the assertion would then be measuring
# the plumbing instead of the script.
assertions=$((assertions + 1))
$SD status "$S1" >"$ROOT/status-exact.out" 2>&1
if grep -q "revision: $COMMIT_C" "$ROOT/status-exact.out"; then
    pass "NAMED: status names the audited revision"
else
    fail "NAMED: status does not name the audited revision $COMMIT_C"
    cat "$ROOT/status-exact.out" >&2
fi

# The build verdict must name it too — that is the line that used to print a
# stale tree's own HEAD as if it were a chosen revision.
assertions=$((assertions + 1))
$SD build "$S1" >"$ROOT/build-exact.out" 2>&1
if grep -q "revision $COMMIT_C" "$ROOT/build-exact.out"; then
    pass "NAMED: build names the audited revision it is building"
else
    fail "NAMED: build does not name the audited revision $COMMIT_C"
    cat "$ROOT/build-exact.out" >&2
fi

# ---------------------------------------------------------------------------
# NO-REUSE — an existing scratch tree is a hard error, not a silent skip.
#
# S1 currently sits on the candidate commit. Re-initing it to a DIFFERENT commit
# without --reuse must fail, and — the half that matters — must leave the tree
# untouched rather than half-moved.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
run_init "$S1" --source "$SRC" --rev main
if [ "$RUN_STATUS" -eq 0 ]; then
    fail "NO-REUSE: a second init over an existing tree exited 0 — the stale tree survived silently"
elif ! grep -q -- '--reuse' "$RUN_OUT" || ! grep -q -- 'rm -rf' "$RUN_OUT"; then
    # Both concrete routes, not merely the word "reuse" somewhere in the output.
    # A bare word match is also satisfied by a usage dump, which would let this
    # assertion pass while the message told the caller nothing.
    fail "NO-REUSE: the second init failed (exit $RUN_STATUS) but the message does not name BOTH ways forward (--reuse and rm -rf)"
    cat "$RUN_OUT" >&2
else
    pass "NO-REUSE: a second init over an existing tree is refused, and names both ways forward"
fi
assert_eq "$COMMIT_C" "$(git -C "$S1" rev-parse HEAD 2>/dev/null)" \
    "NO-REUSE: the refused run left the tree where it was"

# ---------------------------------------------------------------------------
# REUSE-FORCED — --reuse keeps the tree AND still forces it to --rev.
#
# Without this assertion, --reuse would be the original defect with a flag in
# front of it. A stray untracked file is planted first: it belongs to the
# previous run's state, and reusing the directory must not carry it into the new
# revision's tree.
# ---------------------------------------------------------------------------
printf 'left over from the previous run\n' >"$S1/stray.txt"
# A sentinel that exists BEFORE the clean runs. The audit-revision file cannot
# serve here: init WRITES it after the clean, so its presence afterwards proves
# nothing about what the clean spared.
printf 'daemon state from the previous run\n' >"$S1/.harmonik/sentinel.txt"
run_init "$S1" --source "$SRC" --rev main --reuse
assert_eq 0 "$RUN_STATUS" "REUSE-FORCED: init --reuse succeeded"
assert_eq "$COMMIT_B" "$(git -C "$S1" rev-parse HEAD 2>/dev/null)" \
    "REUSE-FORCED: the reused tree was moved to the newly requested revision"
assert_eq "default-branch" "$(cat "$S1/marker.txt" 2>/dev/null)" \
    "REUSE-FORCED: the reused WORKING TREE holds the new revision's content"
assert_eq "$COMMIT_B" "$(cut -f1 <"$S1/.harmonik/audit-revision" 2>/dev/null)" \
    "REUSE-FORCED: the recorded audit revision was updated"
assertions=$((assertions + 1))
if [ -e "$S1/stray.txt" ]; then
    fail "REUSE-FORCED: a leftover untracked file from the previous run survived into the audited tree"
else
    pass "REUSE-FORCED: the previous run's leftover files are gone"
fi
# .harmonik is this scratch's own daemon state, not part of the revision, so it
# is the one thing the clean step must keep.
assertions=$((assertions + 1))
if [ -f "$S1/.harmonik/sentinel.txt" ]; then
    pass "REUSE-FORCED: the scratch's own .harmonik state survived the clean"
else
    fail "REUSE-FORCED: the clean step destroyed the scratch's .harmonik state"
fi

# ---------------------------------------------------------------------------
# STALE BINARY — a binary built from the previous revision must not survive
# --reuse into the next one.
#
# WHY THIS IS SEPARATE FROM REUSE-FORCED. The clean step deliberately spares
# .harmonik, and the built binary lives at .harmonik/bin/harmonik. So the TREE
# moves to the new revision while the EXECUTABLE stays on the old one, and `up`
# only ever checked that the file exists. The daemon would run the previous
# commit's code with every line of output naming the new commit — the wrong-tree
# defect again, now wearing the fix's own label. Found in review, not by the
# first version of this file.
# ---------------------------------------------------------------------------
S5="$ROOT/s-binary"
run_init "$S5" --source "$SRC" --rev candidate
assert_eq 0 "$RUN_STATUS" "STALE BINARY: fixture init succeeded"
mkdir -p "$S5/.harmonik/bin"
printf '#!/bin/sh\nexit 0\n' >"$S5/.harmonik/bin/harmonik"
chmod +x "$S5/.harmonik/bin/harmonik"
printf '%s\n' "$COMMIT_C" >"$S5/.harmonik/bin/built-revision"
run_init "$S5" --source "$SRC" --rev main --reuse
assert_eq 0 "$RUN_STATUS" "STALE BINARY: init --reuse to a new revision succeeded"
assertions=$((assertions + 1))
if [ -e "$S5/.harmonik/bin/harmonik" ]; then
    fail "STALE BINARY: the previous revision's binary survived --reuse; 'up' would run it and name the new revision"
else
    pass "STALE BINARY: --reuse drops the previous revision's binary"
fi
assertions=$((assertions + 1))
out="$($SD up "$S5" 2>&1)"; st=$?
if [ "$st" -eq 0 ]; then
    fail "STALE BINARY: 'up' started a daemon after the tree was moved to a new revision"
else
    pass "STALE BINARY: 'up' refuses to start until the binary is rebuilt"
fi

# The pairing is checked, not only the deletion: a binary put back by hand, or
# built from another tree, must still be caught. The stub exits NON-ZERO so that
# if the pairing check were removed, `up` would still stop at project-hash rather
# than start a daemon — this case must not depend on the check it is testing.
printf '#!/bin/sh\nexit 1\n' >"$S5/.harmonik/bin/harmonik"
chmod +x "$S5/.harmonik/bin/harmonik"
printf '%s\n' "$COMMIT_C" >"$S5/.harmonik/bin/built-revision"
assertions=$((assertions + 1))
out="$($SD up "$S5" 2>&1)"; st=$?
if [ "$st" -eq 0 ]; then
    fail "STALE BINARY: 'up' started a binary stamped $COMMIT_C on a tree pinned to $COMMIT_B"
elif ! printf '%s' "$out" | grep -q 'built from a different revision'; then
    fail "STALE BINARY: 'up' failed (exit $st) but not because the binary and the tree disagree"
    printf '%s\n' "$out" >&2
else
    pass "STALE BINARY: 'up' refuses a binary built from a different revision than the tree holds"
fi

# ---------------------------------------------------------------------------
# LOCAL EDITS — code the pin does not name must never be reported as the pin.
#
# HEAD still matches the pin in every case here, so assert_pinned cannot see any
# of it. The rule is NOT "the tree must be clean": a real `harmonik init --force`
# rewrites tracked docs, and the daemon under test runs `reset --hard` on the
# project after a landing, so a clean-tree rule would accuse a healthy scratch of
# tampering and break `make core-loop-lt` on its second cycle. The rule is scoped
# to what Go actually compiles, and it LABELS rather than refuses, because
# editing the scratch and re-running `cycle` is a documented workflow
# (docs/scratch-daemon-runbook.md, docs/known-workarounds.md).
# ---------------------------------------------------------------------------
S6="$ROOT/s-edits"
run_init "$S6" --source "$SRC" --rev candidate
assert_eq 0 "$RUN_STATUS" "LOCAL EDITS: fixture init succeeded"

# A clean tree stamps the bare commit.
$SD build "$S6" >"$ROOT/build-clean.out" 2>&1
assert_eq "$COMMIT_C" "$(cat "$S6/.harmonik/bin/built-revision" 2>/dev/null)" \
    "LOCAL EDITS: a clean tree stamps the bare commit"

# A MODIFIED tracked .go file must change the label.
printf 'package main\n\n// edited by hand\nfunc main() {}\n' >"$S6/cmd/harmonik/main.go"
$SD build "$S6" >"$ROOT/build-edited.out" 2>&1
assert_eq "${COMMIT_C}+local-edits" "$(cat "$S6/.harmonik/bin/built-revision" 2>/dev/null)" \
    "LOCAL EDITS: a modified .go file labels the binary, so no result can claim the bare commit"
assertions=$((assertions + 1))
if grep -q 'WARNING' "$ROOT/build-edited.out"; then
    pass "LOCAL EDITS: build warns out loud about the edits"
else
    fail "LOCAL EDITS: build did not warn about the local edits"
    cat "$ROOT/build-edited.out" >&2
fi
# Labelling, not refusing: the documented edit-then-rebuild loop must still work.
assertions=$((assertions + 1))
$SD build "$S6" >"$ROOT/build-edited2.out" 2>&1
if [ $? -eq 0 ] && [ -x "$S6/.harmonik/bin/harmonik" ]; then
    pass "LOCAL EDITS: build still SUCCEEDS on an edited tree (the iteration loop is intact)"
else
    fail "LOCAL EDITS: build refused an edited tree; that deletes the documented edit-then-cycle workflow"
    cat "$ROOT/build-edited2.out" >&2
fi
# `up` must accept the labelled binary and say what it is starting.
#
# The real binary is swapped for a stub that fails on `project-hash`, so `up`
# dies at session_name and never reaches `tmux new-session`. That keeps the
# assertion on the disclosure while guaranteeing this test starts no daemon.
printf '#!/bin/sh\nexit 1\n' >"$S6/.harmonik/bin/harmonik"
chmod +x "$S6/.harmonik/bin/harmonik"
assertions=$((assertions + 1))
out="$($SD up "$S6" 2>&1)"
if printf '%s' "$out" | grep -q "${COMMIT_C}+local-edits"; then
    pass "LOCAL EDITS: 'up' names the labelled revision rather than the bare commit"
else
    fail "LOCAL EDITS: 'up' did not name the labelled revision"
    printf '%s\n' "$out" >&2
fi
# "waiting for daemon socket" is printed only AFTER `tmux new-session` has run,
# so it is the line that would prove a daemon was started. Grepping for the word
# "tmux" would not: no line on the success path contains it, so that assertion
# could never fail.
assertions=$((assertions + 1))
if printf '%s' "$out" | grep -q 'waiting for daemon socket'; then
    fail "LOCAL EDITS: 'up' started a daemon; this test must never start one"
else
    pass "LOCAL EDITS: 'up' stopped before starting anything"
fi
# status must disclose it too.
assertions=$((assertions + 1))
$SD status "$S6" >"$ROOT/status-edits.out" 2>&1
if grep -q 'MODIFIED' "$ROOT/status-edits.out"; then
    pass "LOCAL EDITS: status discloses that the tree carries local edits"
else
    fail "LOCAL EDITS: status does not disclose the local edits"
    cat "$ROOT/status-edits.out" >&2
fi
git -C "$S6" checkout -- cmd/harmonik/main.go

# The OFFLINE fold must not launder the label.
#
# The tree is clean again now, but the BINARY is still the labelled one, and the
# events in any capture came from that binary. A fold that reads the tree's pin
# instead of the binary's stamp would report a bare commit for a run that was not
# one — and `feedback` writes that value into a bead on the fleet ledger. Local
# edits never move HEAD, so a pin-only check cannot see this at all.
#
# The assertion reads the revision line, which `batch` prints BEFORE it requires
# jq, so this case does not add a jq dependency to `make fast`.
printf '{"type":"run_started","payload":{"run_id":"r1","bead_id":"hk-x"}}\n' >"$ROOT/fold.ndjson"
assertions=$((assertions + 1))
$SD batch "$S6" foldcheck --from-events "$ROOT/fold.ndjson" --beads hk-x >"$ROOT/fold.out" 2>&1
if grep -q "revision: ${COMMIT_C}+local-edits" "$ROOT/fold.out"; then
    pass "LOCAL EDITS: the offline fold reports the binary's label, not a bare commit"
else
    fail "LOCAL EDITS: the offline fold laundered the label into a bare commit"
    grep -i revision "$ROOT/fold.out" >&2
fi

# An UNTRACKED .go file is the case a tracked-only check misses entirely: `go
# build` compiles every .go file in the package directory regardless of git.
S6B="$ROOT/s-untracked"
run_init "$S6B" --source "$SRC" --rev candidate
printf 'package main\n' >"$S6B/cmd/harmonik/zz_patch.go"
$SD build "$S6B" >"$ROOT/build-untracked.out" 2>&1
assert_eq "${COMMIT_C}+local-edits" "$(cat "$S6B/.harmonik/bin/built-revision" 2>/dev/null)" \
    "LOCAL EDITS: an UNTRACKED .go file labels the binary — go build compiles it, so git tracking is not the test"

# The exclusion list names what INIT ITSELF writes. A change to one of those
# files must NOT be labelled, or every genuine audit would be accused: `harmonik
# init --force` rewrites AGENTS.md on every real scratch.
S6C="$ROOT/s-docs"
run_init "$S6C" --source "$SRC" --rev candidate
printf 'the bootstrap rewrote this\n' >"$S6C/AGENTS.md"
$SD build "$S6C" >"$ROOT/build-docs.out" 2>&1
assert_eq "$COMMIT_C" "$(cat "$S6C/.harmonik/bin/built-revision" 2>/dev/null)" \
    "LOCAL EDITS: a file init itself rewrites is NOT labelled, so a real init is never accused"

# .claude/skills is the same rule and the one most easily missed. `harmonik init
# --force` re-provisions every embedded skill there on EVERY run, and the files
# are tracked. If this were labelled, an audit could never clear the label: the
# next init would just rewrite them again, wedging the merge gate.
S6CS="$ROOT/s-skills"
run_init "$S6CS" --source "$SRC" --rev candidate
printf 'reprovisioned by init\n' >"$S6CS/.claude/skills/demo/SKILL.md"
$SD build "$S6CS" >"$ROOT/build-skills.out" 2>&1
assert_eq "$COMMIT_C" "$(cat "$S6CS/.harmonik/bin/built-revision" 2>/dev/null)" \
    "LOCAL EDITS: .claude/skills is NOT labelled — init rewrites it every run, so a label there could never be cleared"

# Everything else counts, including files Go does not compile. That is the
# deliberate direction of the trade: this check excludes a known-small set rather
# than selecting a guessed set of build inputs, because a missed EXCLUSION shows
# up as a loud false label while a missed BUILD INPUT silently calls a modified
# tree clean. An embedded .dot is the case that proved it — internal/daemon/
# standard-bead.dot is //go:embed'd and an allowlist of Go-ish extensions missed
# it entirely.
S6D="$ROOT/s-other"
run_init "$S6D" --source "$SRC" --rev candidate
printf 'edited\n' >"$S6D/marker.txt"
$SD build "$S6D" >"$ROOT/build-other.out" 2>&1
assert_eq "${COMMIT_C}+local-edits" "$(cat "$S6D/.harmonik/bin/built-revision" 2>/dev/null)" \
    "LOCAL EDITS: any other difference from the pinned commit is labelled, extension notwithstanding"

# An EMBEDDED non-Go asset is the concrete file the first design missed.
S6E="$ROOT/s-embed"
run_init "$S6E" --source "$SRC" --rev candidate
printf 'digraph { a -> b }\n' >"$S6E/cmd/harmonik/graph.dot"
$SD build "$S6E" >"$ROOT/build-embed.out" 2>&1
assert_eq "${COMMIT_C}+local-edits" "$(cat "$S6E/.harmonik/bin/built-revision" 2>/dev/null)" \
    "LOCAL EDITS: an embedded .dot asset is labelled — the file type an allowlist missed"

# review-loop.dot at the scratch ROOT is provisioned by scripts/core-loop-seed.sh,
# the same category as the files init writes. Before this exclusion existed the
# REQUIRED `make core-loop-lt` leg dirtied the tree it had just pinned, so every
# run of that gate stamped +local-edits and the assessor contract voided its own
# result (hk-assessor-lt-gate-dirties-its-own-tree-0jz5y).
S6F="$ROOT/s-reviewloop"
run_init "$S6F" --source "$SRC" --rev candidate
printf 'digraph { impl -> review }\n' >"$S6F/review-loop.dot"
$SD build "$S6F" >"$ROOT/build-reviewloop.out" 2>&1
assert_eq "$COMMIT_C" "$(cat "$S6F/.harmonik/bin/built-revision" 2>/dev/null)" \
    "LOCAL EDITS: the seed-provisioned review-loop.dot is NOT labelled, so the live-verify gate can return a usable result"

# The exclusion above is only safe because that file is DERIVED. Its tracked
# source must stay inside the sweep, or an edit to the workflow this gate runs
# would ride in unlabelled. This is the case that proves the hole is not open:
# same file name, one directory deeper, and it MUST still be labelled.
S6G="$ROOT/s-reviewloop-src"
run_init "$S6G" --source "$SRC" --rev candidate
mkdir -p "$S6G/specs/examples"
printf 'digraph { impl -> review }\n' >"$S6G/specs/examples/review-loop.dot"
$SD build "$S6G" >"$ROOT/build-reviewloop-src.out" 2>&1
assert_eq "${COMMIT_C}+local-edits" "$(cat "$S6G/.harmonik/bin/built-revision" 2>/dev/null)" \
    "LOCAL EDITS: the SOURCE specs/examples/review-loop.dot is still labelled — the root exclusion is scoped to the derived copy only"

# ---------------------------------------------------------------------------
# UNREACHABLE SOURCE — when the source cannot be reached, the revision must not
# be resolved against refs a previous run left behind.
#
# A non-directory --source (a URL) cannot be pre-resolved, so resolution falls to
# the scratch tree. If the fetch also failed, every ref there is stale, and a
# bare branch name would quietly name the OLD commit. That is the original defect
# reachable through a supported flag combination. Found in review.
# ---------------------------------------------------------------------------
S7="$ROOT/s-unreachable"
run_init "$S7" --source "$SRC" --rev candidate
assert_eq 0 "$RUN_STATUS" "UNREACHABLE SOURCE: fixture init succeeded"
assertions=$((assertions + 1))
run_init "$S7" --source "file:///nonexistent/repo.git" --rev main --reuse
if [ "$RUN_STATUS" -eq 0 ]; then
    fail "UNREACHABLE SOURCE: init exited 0 with an unreachable source; it resolved 'main' against a previous run's refs"
    cat "$RUN_OUT" >&2
else
    pass "UNREACHABLE SOURCE: init refuses to resolve a revision against a previous run's refs"
fi
assert_eq "$COMMIT_C" "$(git -C "$S7" rev-parse HEAD 2>/dev/null)" \
    "UNREACHABLE SOURCE: the refused run left the tree where it was"

# ---------------------------------------------------------------------------
# STALE LOCAL REF — a reachable URL source whose branch has MOVED.
#
# A non-directory --source cannot be pre-resolved, so the revision is resolved
# inside the scratch tree. The fetch writes only refs/remotes/scratch-source/*,
# so a reused clone still carries its own local refs/heads/main from the first
# run. Resolving the bare name first would pick that stale local branch and audit
# the OLD commit while the new one sat in the tree unused — the original defect,
# reachable through a supported flag combination. The fix is the resolution
# ORDER, and this is the case that holds it in place. Found in review.
# ---------------------------------------------------------------------------
S8="$ROOT/s-staleref"
run_init "$S8" --source "file://$SRC" --rev main
assert_eq 0 "$RUN_STATUS" "STALE LOCAL REF: fixture init from a URL source succeeded"
assert_eq "$COMMIT_B" "$(git -C "$S8" rev-parse HEAD 2>/dev/null)" \
    "STALE LOCAL REF: the first run landed on the source's main"

# The source's main now moves on, exactly as a branch under audit does.
printf 'moved on\n' >"$SRC/marker.txt"
git -C "$SRC" commit -qam "D: main advances after the first clone"
COMMIT_D="$(git -C "$SRC" rev-parse HEAD)"

run_init "$S8" --source "file://$SRC" --rev main --reuse
assert_eq 0 "$RUN_STATUS" "STALE LOCAL REF: the second init succeeded"
assert_eq "$COMMIT_D" "$(git -C "$S8" rev-parse HEAD 2>/dev/null)" \
    "STALE LOCAL REF: 'main' resolved to the source's CURRENT main, not the clone's stale local branch"
assert_eq "moved on" "$(cat "$S8/marker.txt" 2>/dev/null)" \
    "STALE LOCAL REF: the working tree holds the source's current main"
assert_eq "$COMMIT_D" "$(cut -f1 <"$S8/.harmonik/audit-revision" 2>/dev/null)" \
    "STALE LOCAL REF: the recorded audit revision is the source's current main"

# ---------------------------------------------------------------------------
# UNPINNED — a tree this script did not place is refused, not graded.
#
# This is the assessor's real exposure: a directory that looks like a checkout
# and reports a plausible HEAD. build must refuse it rather than stamp it.
# ---------------------------------------------------------------------------
S3="$ROOT/s-unpinned"
git clone -q "$SRC" "$S3"
for sub in build up; do
    assertions=$((assertions + 1))
    out="$($SD "$sub" "$S3" 2>&1)"
    st=$?
    if [ "$st" -eq 0 ]; then
        fail "UNPINNED: '$sub' exited 0 on a tree that carries no audit revision"
    elif ! printf '%s' "$out" | grep -q 'audit revision'; then
        fail "UNPINNED: '$sub' failed (exit $st) but not because the tree is unpinned"
        printf '%s\n' "$out" >&2
    else
        pass "UNPINNED: '$sub' refuses a tree that carries no audit revision"
    fi
done

# ---------------------------------------------------------------------------
# DRIFT — a tree moved after init is refused.
#
# init verifies its own checkout, but that only covers init. Anything that moves
# HEAD afterwards would otherwise be graded and reported under the pinned
# revision's name.
# ---------------------------------------------------------------------------
S4="$ROOT/s-drift"
run_init "$S4" --source "$SRC" --rev candidate
assert_eq 0 "$RUN_STATUS" "DRIFT: fixture init succeeded"
git -C "$S4" checkout -q --detach --force "$COMMIT_A"
assertions=$((assertions + 1))
out="$($SD build "$S4" 2>&1)"; st=$?
if [ "$st" -eq 0 ]; then
    fail "DRIFT: build exited 0 on a tree that was moved off its audited revision"
elif ! printf '%s' "$out" | grep -qi 'drift'; then
    fail "DRIFT: build failed (exit $st) but not because the tree drifted"
    printf '%s\n' "$out" >&2
else
    pass "DRIFT: build refuses a tree moved off its audited revision"
fi
assertions=$((assertions + 1))
$SD status "$S4" >"$ROOT/status-drift.out" 2>&1
if grep -q 'DRIFTED' "$ROOT/status-drift.out"; then
    pass "DRIFT: status reports the drift rather than printing a plausible revision"
else
    fail "DRIFT: status does not report the drift"
    cat "$ROOT/status-drift.out" >&2
fi

# ---------------------------------------------------------------------------
# STRUCTURAL — the script contains the checkout it claims to perform.
#
# The defect was originally measured as a COUNT: fetch, checkout, reset and pull
# appeared zero times in 1,072 lines, so nothing could possibly have moved the
# tree. Behavioural cases cover the paths they take; this covers the file. It
# also refuses the specific line that caused the defect — defaulting the clone
# source to the origin URL.
# ---------------------------------------------------------------------------
SCRIPT="$SELF_DIR/scratch-daemon.sh"

assertions=$((assertions + 1))
# Comment lines are excluded by SHAPE: this file's own header explains the
# defect and would otherwise match the prose it is describing.
code="$(grep -vE '^[[:space:]]*#' "$SCRIPT")"
if printf '%s\n' "$code" | grep -q 'git -C "\$scratch" checkout'; then
    pass "STRUCTURAL: the script really checks the scratch tree out to a revision"
else
    fail "STRUCTURAL: no 'git checkout' of the scratch tree — nothing moves it to a named commit"
fi

assertions=$((assertions + 1))
if printf '%s\n' "$code" | grep -q 'remote get-url origin'; then
    fail "STRUCTURAL: the clone source is derived from the origin URL again — that serves the remote's DEFAULT BRANCH, not the commit under audit"
else
    pass "STRUCTURAL: the clone source is not derived from the origin URL"
fi

assertions=$((assertions + 1))
if printf '%s\n' "$code" | grep -q 'skipping git clone'; then
    fail "STRUCTURAL: init can skip the clone for an existing tree again — that is how a stale tree survives a second run"
else
    pass "STRUCTURAL: init has no silent skip-the-clone path"
fi

# ---------------------------------------------------------------------------
printf 'rev-pin-test: %d assertions, %d failed\n' "$assertions" "$failures"
# A count floor, for the same reason gate-fails-closed-test.sh carries one: a
# file that dies early reports zero failures and reads as a pass.
# The floor is set to the number this file actually runs today, with no slack.
# Slack lets deleted assertions pass silently, which is the failure this floor
# exists to prevent. Raise it deliberately when you add a case. Counting the
# `assertions=` lines by grep gives one less than the real total, because one
# case runs inside a loop.
if [ "$assertions" -lt 55 ]; then
    printf 'rev-pin-test: only %d assertions ran; this file expects 55\n' "$assertions" >&2
    exit 1
fi
[ "$failures" -eq 0 ]
