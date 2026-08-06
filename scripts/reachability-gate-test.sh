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
#
# EVERY mutation happens in a throwaway copy of the tree, never in the checkout
# you are working in. The first version of this file wrote the canary into the
# live internal/lifecycle and deleted it afterwards. That was honest and it was
# also a mutation of the working tree inside `make fast`, with two costs. Two
# `make fast` runs in one checkout deleted each other's canary mid-analysis, and
# the resulting failures named deadcode's roots and the Go build cache — neither
# of which was wrong — so the first fix attempt went looking in the wrong place.
# And a killed run left a canary behind that failed the real gate afterwards.
# A gate that fabricates its own red is worse than no gate. Refs hk-ky66d.
#
# The copy MIRRORS THE WORKING TREE, not HEAD: `git archive HEAD` for the bulk,
# then the uncommitted delta on top. About 1.4 seconds and 99 MB.
#
# Mirroring matters, and an earlier draft of this rewrite got it wrong by copying
# HEAD alone. The gate under test is always your LIVE script, so HEAD source
# paired with a live gate desynchronizes the moment an edit spans both. Add a new
# cmd/ main and the matching prod_mains entry — the ordinary working state of the
# commit that adds a binary — and the gate's entry-point guard reports "listed but
# gone" against a tree that is perfectly correct, then advises an edit that would
# break it. Nothing else catches that: the real gate passes on the same tree.
#
# One consequence to know. A working tree that does not compile now fails HERE,
# a few steps before `go build ./...` would have said so more plainly. That is a
# true red reported early, not a false one, and the gate's own stderr carries the
# compiler's diagnosis through. The trade is deliberate: a misleading message on
# a tree that is genuinely broken costs less than a red on a tree that is fine.

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd "$script_dir/.." && pwd)

tmp=$(mktemp -d "${TMPDIR:-/tmp}/harmonik-reachability-test.XXXXXX")
cleanup() { rm -rf "$tmp"; }
trap cleanup EXIT

fail() {
    echo "reachability-gate-test: FAILED: $*" >&2
    exit 1
}

cd "$repo_root"

canary_name=zz_reachability_gate_canary
live_canary=$repo_root/internal/lifecycle/$canary_name.go
live_canary_test=$repo_root/internal/lifecycle/${canary_name}_test.go

# A canary in the CHECKOUT can only be debris from a killed run of the version of
# this script that wrote them there. Say so in milliseconds and name the cure. The
# end-of-run check in case 5 would otherwise spend the whole analysis first and
# then accuse THIS run of writing it.
for leftover in "$live_canary" "$live_canary_test"; do
    [[ ! -e $leftover ]] || fail "a canary from an earlier run is still in your checkout — delete $leftover"
done

# The throwaway tree. `git archive` and not `git worktree add`, because a
# worktree leaves an administrative record under .git/worktrees that survives a
# killed run and has to be pruned. A tarball extraction leaves nothing but the
# directory this script's own trap removes.
tree=$tmp/tree
mkdir -p "$tree"
git archive HEAD | tar -x -C "$tree" || fail "cannot export HEAD into a scratch tree"
[[ -f $tree/go.mod ]] || fail "the scratch tree has no go.mod, so nothing was exported"

# Now the uncommitted delta, so the scratch tree is the WORKING tree. Only the
# changed paths are touched, which is why this costs milliseconds rather than the
# 5.6 seconds a whole-tree `git ls-files` copy measured.
#
# One rule covers almost every status code: a path that is a file or a symlink on
# disk is copied, and a path that is neither is removed from the scratch tree.
# Deletions and renames fall out of it — a rename's old path is simply a path that
# no longer exists. `-z` emits a rename as new\0old\0, so the extra field is
# consumed, and it also writes paths verbatim, so a name holding a newline or a
# quote needs no unescaping.
#
# `-f || -L` and NOT `-e`, because `-e` is true for a DIRECTORY, and two ordinary
# trees arrive here holding one. Replace a tracked file with a directory of the
# same name and git reports the file as deleted while the directory stands in its
# place. An untracked directory git will not descend into — a nested repository,
# which this project creates under .claude/worktrees/ — arrives as a single entry
# with a trailing slash. Under `-e` both reached `cp` without `-R`, which refuses a
# directory, and the script died at exit 1 printing a bare "cp: ... is a directory"
# with no FAILED: line: a false red of exactly the shape this file exists to
# remove. Skipping directories is correct and not merely safe — --untracked-files=all
# delivers a real directory's contents as their own entries, so essentially the only
# thing `-R` would add here is the inside of a nested repository, .git and all. (The
# other carrier is a dirty submodule, which reports ` M` while being a directory on
# disk; `git archive` omits submodule contents too, so `-R` would not reach it
# either. A directory whose contents are all ignored is NOT a carrier — git does not
# emit it in any form, so it never reaches this function at all.)
#
# `-f || -L` also declines anything that is neither file nor symlink, which matters
# for one case beyond directories. Replace a TRACKED file with a FIFO and git reports
# an ordinary modification; under `-e` that sent `cp` to READ the pipe, where it
# blocks forever — a hang inside `make fast`, which is worse than a false red.
# Measured, including which carrier is real: an UNTRACKED fifo is reported by
# nothing, not `status --untracked-files=all` and not `ls-files -o`, so the tracked
# type change is the only way in. `-L` is required as well as `-f` so that a dangling
# symlink, and a symlink pointing at a directory, both still mirror as links instead
# of being followed.
#
# ORDER MATTERS, and git supplies it: changed entries come as a group before
# untracked ones, so the ` D path` for a file that became a directory is processed
# BEFORE the `?? path/child` entries that rebuild it. Reversed, `mkdir -p` would meet
# the HEAD file still sitting at that path and fail. The `|| fail` guards below mean
# that arrives as a named failure alongside the bare `mkdir:` line, which mkdir
# prints itself. Naming it is the only part of this the script can control.
#
# What the mirror does NOT carry: anything gitignored, because `git status` does not
# report it and `git archive` does not export it. Go does not read .gitignore, so an
# ignored file the build needs would make the scratch tree fail to compile and blame
# the caller's tree. Latent today — this repo has no ignored .go files, no go.work,
# and every //go:embed target is tracked. Nothing asserts that. Refs hk-hrb5b.
#
# The destination is REMOVED before it is written. `cp -P` governs how the source
# is read, not what the destination may be, so copying onto an existing symlink
# follows it and overwrites the LINK'S TARGET — a file the mirror was never asked
# to touch. Every destination is inside the throwaway tree, so no checkout was ever
# at risk; what it did was make the mirror quietly misrepresent the tree. This repo
# has 12 tracked symlinks, CLAUDE.md -> AGENTS.md among them, so a symlink that
# becomes a regular file hits it. The same removal is what makes a
# directory-to-file change land as a file instead of a file INSIDE the directory.
mirror_path() {
    rm -rf "${tree:?}/${1:?}"
    if [[ -f $1 || -L $1 ]]; then
        mkdir -p "$tree/$(dirname "$1")" ||
            fail "cannot create the scratch directory for $1, so the mirror is incomplete"
        cp -Pp "$1" "$tree/$1" ||
            fail "cannot mirror $1 into the scratch tree, so the mirror is incomplete"
    fi
}
# Read the status into a file first. `done < <(git status ...)` discards git's
# exit status, and neither `set -e` nor `pipefail` sees a process substitution —
# so a git that failed would leave the loop running zero times, the tree silently
# at HEAD, and the false red this whole change exists to remove back in place with
# nothing printed. The guard is one line and the failure it covers is silent, which
# is reason enough on its own. No specific trigger is claimed: the obvious candidate
# does not fire — an index.lock held by another lane was measured here, and status
# still exits 0.
#
# `--no-optional-locks` because reading the status can otherwise refresh and rewrite
# the real checkout's .git/index. That was the one thing this script still did to
# the tree it promises not to touch.
status_z=$tmp/status.z
git --no-optional-locks status --porcelain=v1 -z --untracked-files=all >"$status_z" ||
    fail "cannot read the working-tree status, so the scratch tree would silently be HEAD"
while IFS= read -r -d '' entry; do
    xy=${entry:0:2}
    if [[ $xy == R* || $xy == C* || $xy == ?R || $xy == ?C ]]; then
        IFS= read -r -d '' old_path || fail "git status ended mid-rename"
        mirror_path "$old_path"
    fi
    mirror_path "${entry:3}"
done <"$status_z"

# The gate UNDER TEST is the live script. The mirror above already carries it if
# it is edited; this is the one property the whole file rests on, so it does not
# depend on that. It derives the tree it analyzes from its own location, so
# putting the live script inside the scratch tree points it there.
gate=$tree/scripts/reachability-gate.sh
cp "$script_dir/reachability-gate.sh" "$gate"

# The scratch tree is not a git repository, so the gate's own tool lookup — which
# walks to the main checkout's .tools via --git-common-dir — cannot resolve there.
# Resolve it here, the same way, against the real checkout.
if [[ -z ${DEADCODE_BIN:-} ]]; then
    if ! tools_home=$(git -C "$repo_root" rev-parse --path-format=absolute --git-common-dir 2>/dev/null | sed 's|/\.git/*$||'); then
        tools_home=$repo_root
    fi
    [[ -n $tools_home ]] || tools_home=$repo_root
    if [[ -x $tools_home/.tools/deadcode ]]; then
        DEADCODE_BIN=$tools_home/.tools/deadcode
    elif DEADCODE_BIN=$(command -v deadcode); then
        echo "reachability-gate-test: WARNING: no pinned tool, using $DEADCODE_BIN — run 'make tools'" >&2
    else
        fail "no deadcode binary; run 'make tools'"
    fi
    export DEADCODE_BIN
fi

canary_file=$tree/internal/lifecycle/$canary_name.go
canary_test_file=$tree/internal/lifecycle/${canary_name}_test.go
canary_symbol='github.com/gregberns/harmonik/internal/lifecycle.reachabilityGateCanary'

# One configuration is enough to prove the gate can fail, and it halves the
# cost of a self-test that runs in the inner loop. The gate itself still
# measures both. Entries for the other config read as "left the set" and raise
# a notice, which is why the clean-tree case below accepts a notice.
REACHABILITY_CONFIGS="$(go env GOOS) $(go env GOARCH)"
export REACHABILITY_CONFIGS

# The baseline is MEASURED from the scratch tree, not read from the ratified
# file. Reading the ratified file would couple every case below to whether that
# file currently matches HEAD — so ratifying a new seam and then running `make
# fast` would false-fail this self-test, which is the same class of defect this
# rewrite exists to remove. Measuring it here also makes case 1 a true ratchet
# test: the canary is the ONLY difference between this baseline and the next
# measurement.
#
# This is also the first thing in `make fast` that compiles the whole program, so
# it is where a working tree that does not build stops. The gate's stderr is not
# swallowed above — only its stdout is — so the compiler's own diagnosis reaches
# the reader with the message below.
baseline=$tmp/measured.baseline
REACHABILITY_BASELINE=$baseline bash "$gate" --write-baseline >/dev/null ||
    fail "the gate could not measure a baseline from your working tree (if it does not compile, that is why — the gate's output above says which package)"
[[ -s $baseline ]] || fail "the measured baseline is empty"

[[ ! -e $canary_file ]] || fail "a canary file is committed at internal/lifecycle: $canary_file"
[[ ! -e $canary_test_file ]] || fail "a canary test file is committed at internal/lifecycle: $canary_test_file"

# 1. Break it on purpose. An unexported, uncalled function in a linked package
#    is exactly the shape this gate exists to catch.
cat >"$canary_file" <<'CANARY'
package lifecycle

// zz_reachability_gate_canary.go is written into a THROWAWAY copy of the tree
// by scripts/reachability-gate-test.sh. It must never appear in a checkout.

// reachabilityGateCanary is unreachable from every production entry point.
func reachabilityGateCanary() string { return "canary" }
CANARY

if out=$(REACHABILITY_BASELINE=$baseline bash "$gate" 2>&1); then
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

// zz_reachability_gate_canary_test.go is written into a THROWAWAY copy of the
// tree by scripts/reachability-gate-test.sh. It must never appear in a checkout.

import "testing"

func TestReachabilityGateCanaryIsCalledFromATest(t *testing.T) {
	if reachabilityGateCanary() != "canary" {
		t.Fatal("canary changed")
	}
}
CANARYTEST

if ! (cd "$tree" && go vet ./internal/lifecycle/ >/dev/null 2>&1); then
    fail "the canary pair does not compile, so the test-root case proves nothing"
fi
if out=$(REACHABILITY_BASELINE=$baseline bash "$gate" 2>&1); then
    echo "$out" >&2
    fail "a _test.go caller made the canary look reachable. Test binaries ARE roots"
fi
grep -q "$canary_symbol" <<<"$out" || {
    echo "$out" >&2
    fail "the gate stopped naming $canary_symbol once a test called it"
}

rm -f "$canary_file" "$canary_test_file"

# Confirm the mutation really applied and really reverted. An unverified
# mutation is evidence of nothing. This one also proves the two failures above
# were caused by the canary and not by a tree that was already failing.
[[ ! -e $canary_file && ! -e $canary_test_file ]] || fail "a canary file survived cleanup"
REACHABILITY_BASELINE=$baseline bash "$gate" >/dev/null 2>&1 ||
    fail "the gate did not recover after the canary was removed"

# 3. A name that leaves the unreachable set is a NOTICE, not a failure.
{ head -1 "$baseline"; echo "linux/amd64 example.com/nonexistent.Ghost"; grep -v '^#' "$baseline"; } >"$tmp/ghost.baseline"
if ! out=$(REACHABILITY_BASELINE=$tmp/ghost.baseline bash "$gate" 2>&1); then
    echo "$out" >&2
    fail "a stale baseline entry failed the gate instead of raising a notice"
fi
grep -q 'NOTICE' <<<"$out" || fail "a stale baseline entry produced no notice"

# 3b. The caller's locale must not change the verdict.
#
# Case 3 above passed while the gate was reporting the SAME name as both newly
# unreachable and newly reachable, which is arithmetically impossible. Every
# sort in the gate said LC_ALL=C; comm did not, so comm compared C-sorted input
# under the caller's en_US.UTF-8. On the real tree that fabricated 12 failures
# and hid 16 real improvements. Case 3's single ghost sorts where the two
# collations agree, so it never desynchronized comm and never caught it.
#
# These ghosts are chosen to disagree: C puts .Z before .a, a UTF-8 collation
# puts aaa before ZZZ. Removing the export from the gate makes this case report
# 6 phantom names — the six lowercase internal/sentinel functions, under the one
# configuration this self-test pins. The gate itself measures both configurations,
# which is why the same defect counted 12 on the real tree. A stale entry is still
# only ever a NOTICE.
ghost_pkg=github.com/gregberns/harmonik/internal/sentinel

# The ghosts only desynchronize comm because they land INSIDE a real cluster of
# lowercase entries for this package. That precondition is borrowed from the
# tree's current contents, and nothing else here would notice it going away:
# wire up or delete internal/sentinel's unreachable functions and this case
# degrades to a silent vacuous pass. So assert it.
# LOWERCASE specifically. The desync needs a name starting with a lowercase
# letter to sort against the .Z ghost, and this package currently has 6 of those
# beside 4 uppercase. A bare package match would still pass once the lowercase
# ones are wired up or deleted, and the case would go vacuous underneath it.
grep -qE "$ghost_pkg\.[a-z]" "$baseline" ||
    fail "the collation case needs lowercase-initial baseline entries for $ghost_pkg to sort against; pick a package that still has some"

{
    head -1 "$baseline"
    grep -v '^#' "$baseline"
    for c in darwin/arm64 linux/amd64; do
        echo "$c $ghost_pkg.ZZZGhostUpper"
        echo "$c $ghost_pkg.aaaGhostLower"
    done
} >"$tmp/collation.baseline"

# Pick a locale whose COLLATION differs from C, which is not the same as picking
# one whose encoding is UTF-8. C.UTF-8 is UTF-8 encoding with C collation, so it
# can never exercise this case — and on glibc `locale -a` lists it BEFORE
# en_US.utf8, so the earlier selector accepted `C` and took the one UTF-8 locale
# guaranteed to prove nothing. Every CI run on ubuntu therefore skipped the
# regression test for the defect this case exists for, while script-tests still
# printed OK. A skip that reads as a pass is the failure mode this whole file is
# written against, so prefer en_US, then any UTF-8 locale that is not C or POSIX.
utf8_locale=$(locale -a 2>/dev/null | grep -iE '^en_US\.(utf-?8)$' | head -1 || true)
if [[ -z $utf8_locale ]]; then
    utf8_locale=$(locale -a 2>/dev/null | grep -iE '\.(utf-?8)$' | grep -viE '^(C|POSIX)\.' | head -1 || true)
fi
# A skip must not read as a pass. Both hatches below used to print to stderr and
# let the script exit 0, so `make script-tests` stayed green while the regression
# test for an already-shipped defect ran nowhere. Nobody reads CI stderr under a
# green tier. So they fail now, and a box that genuinely cannot run the case has
# to say so out loud by setting the override.
skip_ok=${REACHABILITY_ALLOW_NO_UTF8_LOCALE:-}
no_locale_case() {
    if [[ -n $skip_ok ]]; then
        echo "reachability-gate-test: SKIPPED by REACHABILITY_ALLOW_NO_UTF8_LOCALE: $*" >&2
        echo "  The gate was NOT checked against the collation defect." >&2
        return 0
    fi
    fail "$* — set REACHABILITY_ALLOW_NO_UTF8_LOCALE=1 to accept running without this case"
}

if [[ -z $utf8_locale ]]; then
    no_locale_case "no usable UTF-8 locale on this box, so the locale-independence case cannot run"
elif [[ "$(LC_ALL=$utf8_locale printf 'a\nB\n' | LC_ALL=$utf8_locale sort | head -1)" \
      == "$(printf 'a\nB\n' | LC_ALL=C sort | head -1)" ]]; then
    no_locale_case "$utf8_locale collates like C on this box, so the locale-independence case proves nothing"
else
    if ! out=$(LC_ALL=$utf8_locale REACHABILITY_BASELINE=$tmp/collation.baseline bash "$gate" 2>&1); then
        echo "$out" >&2
        fail "stale baseline entries failed the gate under $utf8_locale: the gate's comparison depends on the caller's locale"
    fi
    grep -q 'became production-unreachable' <<<"$out" &&
        fail "the gate named a new unreachable function that does not exist, under $utf8_locale"
fi

# 4. Fail-closed paths. Each must exit 2 — a setup problem is not a pass.
#
# The silent-deadcode case NEEDS the measured baseline. Without it the gate falls
# back to the ratified scripts/reachability.baseline as mirrored into the scratch
# tree, and an absent or malformed ratified file then makes the gate exit 2 on the
# BASELINE and never reach the behaviour under test. `[[ $? -eq 2 ]]` cannot tell
# those apart, so the case would pass while proving nothing.
#
# The missing-deadcode case that opens this section is given the same baseline for
# consistency only: the gate checks its tool before it reads any baseline, so that
# one was never exposed. The last two cases override the baseline on purpose, which
# is the thing they test.
set +e
REACHABILITY_BASELINE=$baseline DEADCODE_BIN=/nonexistent/deadcode PATH=/usr/bin:/bin bash "$gate" >/dev/null 2>&1
[[ $? -eq 2 ]] || fail "a missing deadcode binary did not exit 2"

# An analysis that reports nothing is making a claim, and it can be wrong the
# same way a test can. A tool that succeeds and prints nothing must not read as
# "the tree is clean".
printf '#!/bin/sh\nexit 0\n' >"$tmp/silent-deadcode"
chmod +x "$tmp/silent-deadcode"
REACHABILITY_BASELINE=$baseline DEADCODE_BIN=$tmp/silent-deadcode bash "$gate" >/dev/null 2>&1
[[ $? -eq 2 ]] || fail "a deadcode run that printed nothing did not exit 2"

REACHABILITY_BASELINE=$tmp/absent.baseline bash "$gate" >/dev/null 2>&1
[[ $? -eq 2 ]] || fail "a missing baseline did not exit 2"

echo 'this line has three fields here' >"$tmp/malformed.baseline"
REACHABILITY_BASELINE=$tmp/malformed.baseline bash "$gate" >/dev/null 2>&1
[[ $? -eq 2 ]] || fail "a malformed baseline did not exit 2"
set -e

# 5. The checkout this ran in is untouched. This is the regression test for
#    hk-ky66d: every case above mutated a throwaway tree, so a canary in the
#    live tree means one of them reached the wrong root and two concurrent runs
#    can false-fail each other again.
[[ ! -e $live_canary && ! -e $live_canary_test ]] ||
    fail "this run wrote a canary into the live checkout at internal/lifecycle"

echo "reachability-gate-test: OK (the gate fails on a new unreachable function, fails closed on setup errors, and mutates no checkout)"
