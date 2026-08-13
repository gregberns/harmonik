#!/usr/bin/env bash
# scratch-daemon-provenance-test.sh — proves that a gate binary built by the
# documented assessor sequence can PROVE which commit it is, and that a binary
# built from a genuinely edited tree still cannot.
#
# THE DEFECT THIS HOLDS SHUT (hk-gate-clean-but-binary-dirty-7gwil).
# Two tools measured the cleanliness of one scratch tree and gave two answers:
#
#   scratch-daemon.sh status   "pinned, HEAD matches, no local edits"
#   harmonik version --binary  "contains-dirty ... vcs.modified=true", exit 3
#
# `scratch-daemon.sh init` runs `harmonik init --force`, which rewrites tracked
# files and writes .harmonik/.gitignore — a file whose content is `*` followed by
# `!config.yaml`, `!branching.yaml`, `!.gitignore`, so it deliberately re-admits
# this scratch's own runtime config into git's view. The documented `build` step
# runs AFTER that, so Go stamped vcs.modified=true. local_edits() excludes every
# one of those paths, so this script reported a clean tree.
#
# The consequence: `harmonik version --binary <bin> --contains <rev>` returned
# exit 3 for EVERY binary the documented sequence produced. A provenance check
# that can never return 0 checks nothing, and the assessor contract makes the
# bare commit hash the whole basis for calling a result an audit of one commit.
#
# WHAT IS ASSERTED, AND WHY EACH CASE IS NEEDED.
#
#   CLEAN-STAMP  the documented `init` then `build` sequence must leave the tree
#                byte-identical to the pin in git's WHOLE-TREE view, and the
#                binary must carry vcs.modified=false. This is the case that
#                fails before the fix.
#   PROVABLE     the real `harmonik version --binary <bin> --contains <rev>` must
#                exit 0 with status `contains` on that binary. CLEAN-STAMP alone
#                would pass for a script that got the stamp right while the
#                shipped tool still refused the artefact, and it is the tool's
#                answer the assessor quotes.
#   STILL-DIRTY  a tree with a real edit to compiled code must STILL stamp
#                vcs.modified=true and STILL be refused with exit 3. Without this
#                the whole bead could be "fixed" by making the check pass always,
#                which is the same defect facing the other way.
#   NO-CLOBBER   that edit must survive the build. The reconcile restores tracked
#                files, so a version of it that ran unconditionally would silently
#                delete the developer's work in the inner loop the runbooks
#                document.
#   BATCH-LABELLED
#                the revision on the BATCH_SUMMARY line must be a bare commit
#                hash only when the binary can prove it is that commit. The
#                fixture is an untracked review-loop.dot, which local_edits
#                excludes and Go counts, so the harness and the toolchain
#                disagree — and the harness is the optimistic one.
#   DISCLOSED    `status` must print the provenance the BINARY carries, not only
#                the provenance this script measured. The two disagreed for a
#                whole assessment because nothing ever printed them together.
#
# Printing it was not enough (hk-48zdw): the live-verify gate printed a DIRTY
# stamp and an all-green grid in one log, twenty-five lines apart, and the run was
# recorded as a valid PASS. The `provenance` subcommand turns that line into an
# exit code, and these four cases hold both directions of it shut.
#
#   VERDICT-ACCEPTS      exit 0, token `clean`, for the binary the documented
#                        sequence produces. Without it the gate can never pass.
#   VERDICT-FAILS-CLOSED no binary, no pin and no vcs stamp each REFUSE by name.
#                        A check that shrugs when it cannot answer passes whenever
#                        it is broken.
#   VERDICT-REFUSES      exit non-zero, token `dirty`, for the edited tree above.
#   GATE-REFUSES         scripts/core-loop-matrix.sh --gate must STOP on that
#                        binary and print no grid. The verdict command being right
#                        while the gate prints green beside it is the whole bead.
#   GATE-LENIENT-REPORTS the mode that does NOT stop must still report
#                        all_green:false and the provenance token in MATRIX_JSON.
#                        Lenient exits 0 by design, so the exit code is not the
#                        signal and those two fields are.
#   MONOTONIC            the stamp is read twice and a later `clean` may not clear
#                        an earlier refusal — otherwise a lenient run that graded
#                        every cell with a refused binary reports itself clean.
#                        Both directions, so the rule cannot be satisfied by a
#                        token that is simply stuck.
#
# The fixture is a real local git repo that is also a real, dependency-free Go
# module, so `build` genuinely compiles and Go genuinely writes the vcs stamp
# this test reads back. Its `cmd/harmonik/main.go` is a stub that reproduces what
# `harmonik init --force` was MEASURED to do to a real scratch clone on
# 2026-08-10 — five tracked rewrites and three newly visible runtime files — so
# no daemon, no network and no tmux are needed. Needs Go and git.
#
# WHAT THIS FILE CANNOT PROVE. The stub stands in for the real `harmonik init`.
# If init starts writing something outside .harmonik/ and outside the tracked
# files it already rewrites, this fixture will not know. The reconcile reads the
# paths to restore from git rather than from a list, so it should still cover
# them — but re-measure a real scratch clone after any change to what
# `harmonik init --force` writes.
#
# Refs: hk-gate-clean-but-binary-dirty-7gwil.

set -uo pipefail   # NOT -e: assertions report and keep going.

SELF_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd "$SELF_DIR/.." && pwd -P)"
SD="bash $SELF_DIR/scratch-daemon.sh"

failures=0
assertions=0

fail() { printf 'provenance-test: FAIL: %s\n' "$*" >&2; failures=$((failures + 1)); }
pass() { printf 'provenance-test: ok: %s\n' "$*"; }

# A short /tmp base, not $TMPDIR: the same 104-byte unix-socket cap that
# scratch-daemon-smoke.sh documents applies to anything under a scratch path.
ROOT="$(mktemp -d "/tmp/hk-prov.XXXXXX")"
cleanup() { [ "${KEEP_DIR:-0}" = "1" ] && { echo "provenance-test: KEEP_DIR=1 — leaving $ROOT"; return; }; rm -rf "$ROOT" 2>/dev/null; }
trap cleanup EXIT

# vcs_field <binary> <key> — one Go build setting embedded in a binary, e.g.
# vcs.modified. Empty when the binary carries no stamp. This is the same data
# `harmonik version --binary` reads, so the two cannot disagree by construction.
vcs_field() {
    go version -m "$1" 2>/dev/null \
        | awk -v key="$2" '$0 ~ (key "=") && v == "" { sub(".*" key "=", ""); v = $0 } END { print v }'
}

# ---------------------------------------------------------------------------
# Fixture: a source repo shaped like this one where it matters for the stamp.
#
#   - .gitignore hides /.harmonik/* and re-admits /.harmonik/context, exactly as
#     the real repo does. That is what makes `harmonik init`'s own
#     .harmonik/.gitignore load-bearing: without the root rule the runtime files
#     would be visible anyway, and the case would pass for the wrong reason.
#   - .harmonik/config.yaml is NOT committed, so a fresh clone has none and
#     scratch-daemon.sh takes its bootstrap path — build, then `harmonik init`.
#     That ordering is the defect: the build the assessor runs comes after init.
# ---------------------------------------------------------------------------
SRC="$ROOT/source"
mkdir -p "$SRC/cmd/harmonik"
git -C "$SRC" init -q -b main
git -C "$SRC" config user.email "provenance-test@example.invalid"
git -C "$SRC" config user.name "provenance-test"

printf 'module scratchfixture\n\ngo 1.21\n' >"$SRC/go.mod"

# The stub daemon binary. It answers only the two subcommands scratch-daemon.sh
# calls, and `init` reproduces what the real `harmonik init --force` was measured
# to leave behind on a real scratch clone.
cat >"$SRC/cmd/harmonik/main.go" <<'FIXTURE_MAIN'
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// flagValue returns the value that follows name in args.
func flagValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// write puts content at project/rel, creating parents.
func write(project, rel, content string) {
	path := filepath.Join(project, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("fixture init: wrote %s\n", rel)
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		return
	}
	switch args[0] {
	case "project-hash":
		fmt.Println("0123456789ab")
	case "init":
		project := flagValue(args, "--project")
		if project == "" {
			fmt.Fprintln(os.Stderr, "fixture init: --project is required")
			os.Exit(2)
		}
		// Runtime state the daemon needs, plus the re-admit file that makes the
		// first two VISIBLE to git even though the repo's own .gitignore hides
		// .harmonik/. This is verbatim the shape the real init writes.
		write(project, ".harmonik/.gitignore", "*\n!config.yaml\n!branching.yaml\n!.gitignore\n")
		write(project, ".harmonik/config.yaml", "sentinel:\n  enabled: true\n")
		write(project, ".harmonik/branching.yaml", "branching:\n  start_from: main\n  lands_on: main\n")
		// Tracked documentation and context that init regenerates from its own
		// templates on every --force run.
		write(project, "AGENTS.md", "agent instructions — regenerated by init\n")
		write(project, "AGENT_INDEX.md", "index — regenerated by init\n")
		write(project, "STATUS.md", "status — regenerated by init\n")
		write(project, ".harmonik/context/project.yaml", "phase: regenerated-by-init\n")
		write(project, ".harmonik/context/captain-lanes.md", "lanes — regenerated by init\n")
	default:
		fmt.Fprintf(os.Stderr, "fixture: unknown subcommand %q\n", args[0])
		os.Exit(2)
	}
}
FIXTURE_MAIN

# /.tools matches the real repo's entry: without it the provisioned toolchain
# reads as untracked local edits and every case here stamps +local-edits.
#
# The editor-backup rules are copied from the real repo's .gitignore, where
# `.DS_Store`, `*.swp`, `*.swo` and `*~` are the first four lines. They are not
# decoration here: they are what makes an editor backup file left inside an
# embed tree invisible to git and therefore to Go's vcs.modified, which is the
# accident the HIDDEN-INPUT cases below reproduce. Drop them and those cases
# pass for the wrong reason, against an untracked file the first sweep sees.
printf '/.tools\n/.harmonik/*\n!/.harmonik/context\n.DS_Store\n*.swp\n*.swo\n*~\n' >"$SRC/.gitignore"

# A Makefile with a `tools:` recipe, because init provisions the pinned dev
# toolchain into the scratch clone and refuses a tree that has none. `@:` is the
# shell no-op, so the two `go install` lines are read by the name derivation and
# run by nothing — this fixture must not want the network.
{
	printf 'TOOLS_DIR := $(shell pwd)/.tools\n\n'
	printf '.PHONY: tools\ntools:\n'
	printf '\t@mkdir -p $(TOOLS_DIR) && touch $(TOOLS_DIR)/alpha $(TOOLS_DIR)/beta && chmod +x $(TOOLS_DIR)/alpha $(TOOLS_DIR)/beta\n'
	printf '\t@: go install example.com/a/cmd/alpha@v1.0.0\n'
	printf '\t@: go install example.com/b/cmd/beta@v2.0.0\n'
} >"$SRC/Makefile"

# The tracked files init rewrites, committed with their ORIGINAL content so the
# rewrite is a real diff.
printf 'agent instructions — committed\n' >"$SRC/AGENTS.md"
printf 'index — committed\n' >"$SRC/AGENT_INDEX.md"
printf 'status — committed\n' >"$SRC/STATUS.md"
mkdir -p "$SRC/.harmonik/context"
printf 'phase: committed\n' >"$SRC/.harmonik/context/project.yaml"
printf 'lanes — committed\n' >"$SRC/.harmonik/context/captain-lanes.md"

git -C "$SRC" add -A && git -C "$SRC" commit -qm "the commit under audit"
REV="$(git -C "$SRC" rev-parse HEAD)"
echo "provenance-test: fixture revision $REV"

# The REAL provenance tool. The whole bead is that this command refused every
# binary the documented sequence produced, so nothing short of running it proves
# the fix. Built once, into the throwaway tree.
HK="$ROOT/harmonik"
if ! go build -C "$REPO_ROOT" -o "$HK" ./cmd/harmonik >"$ROOT/hk-build.log" 2>&1; then
    fail "could not build $REPO_ROOT/cmd/harmonik, so the provenance tool could not be exercised"
    cat "$ROOT/hk-build.log" >&2
    exit 1
fi

# ---------------------------------------------------------------------------
# CLEAN-STAMP — the documented assessor sequence leaves a tree Go calls clean.
#
#   scratch-daemon.sh init  <scratch> --rev <rev> --source <src>
#   scratch-daemon.sh build <scratch>
#
# This is roles/assessor/operating.md §Merge-gate step 1, minus `up` (starting a
# daemon needs tmux and a real binary; the stamp is written by `build`).
# ---------------------------------------------------------------------------
SCRATCH="$ROOT/s-clean"
if ! $SD init "$SCRATCH" --source "$SRC" --rev "$REV" >"$ROOT/init.out" 2>&1; then
    fail "CLEAN-STAMP: init failed, so nothing downstream could be measured"
    cat "$ROOT/init.out" >&2
    exit 1
fi
$SD build "$SCRATCH" >"$ROOT/build.out" 2>&1
BUILD_STATUS=$?
if [ "$BUILD_STATUS" -ne 0 ]; then
    fail "CLEAN-STAMP: build failed (exit $BUILD_STATUS)"
    cat "$ROOT/build.out" >&2
    exit 1
fi
BIN="$SCRATCH/.harmonik/bin/harmonik"

assertions=$((assertions + 1))
RESIDUE="$(git -C "$SCRATCH" status --porcelain --untracked-files=all 2>&1)"
if [ -n "$RESIDUE" ]; then
    fail "CLEAN-STAMP: after init+build the tree still differs from the pin in git's whole-tree view:
$RESIDUE"
else
    pass "CLEAN-STAMP: init+build leaves a tree git reports as identical to the pin"
fi

assertions=$((assertions + 1))
MODIFIED="$(vcs_field "$BIN" 'vcs\.modified')"
if [ "$MODIFIED" != "false" ]; then
    fail "CLEAN-STAMP: the gate binary carries vcs.modified='$MODIFIED' (want false) — it cannot prove it is $REV"
else
    pass "CLEAN-STAMP: the gate binary carries vcs.modified=false"
fi

assertions=$((assertions + 1))
STAMPED_REV="$(vcs_field "$BIN" 'vcs\.revision')"
if [ "$STAMPED_REV" != "$REV" ]; then
    fail "CLEAN-STAMP: the gate binary names revision '$STAMPED_REV', not the pinned $REV"
else
    pass "CLEAN-STAMP: the gate binary names the pinned revision"
fi

# The daemon's own runtime state must survive the reconcile. Restoring the tree
# is only acceptable because it touches nothing the daemon reads.
assertions=$((assertions + 1))
MISSING=""
for f in config.yaml branching.yaml; do
    [ -f "$SCRATCH/.harmonik/$f" ] || MISSING="$MISSING .harmonik/$f"
done
if [ -n "$MISSING" ]; then
    fail "CLEAN-STAMP: the reconcile removed daemon runtime state:$MISSING"
else
    pass "CLEAN-STAMP: the daemon's runtime config survives the reconcile"
fi

# ---------------------------------------------------------------------------
# PROVABLE — the shipped provenance tool accepts that binary.
#
# This is the bead's own acceptance line. CLEAN-STAMP could pass while the tool
# still refused the artefact, and it is the tool's answer an assessor quotes.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
PROV_OUT="$("$HK" version --binary "$BIN" --contains "$REV" --repo "$SCRATCH" 2>&1)"
PROV_STATUS=$?
if [ "$PROV_STATUS" -ne 0 ]; then
    fail "PROVABLE: 'harmonik version --binary --contains $REV' exited $PROV_STATUS (want 0) on the gate binary:
$PROV_OUT"
elif ! grep -q '^status:   contains$' <<<"$PROV_OUT"; then
    fail "PROVABLE: exit 0 but the status token is not 'contains':
$PROV_OUT"
else
    pass "PROVABLE: 'harmonik version --binary --contains <rev>' exits 0 with status contains"
fi

# ---------------------------------------------------------------------------
# DISCLOSED — `status` prints what the BINARY says, beside what it measured.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
STATUS_OUT="$($SD status "$SCRATCH" 2>&1)"
if ! grep -q 'vcs\.modified=false' <<<"$STATUS_OUT"; then
    fail "DISCLOSED: status does not report the binary's own vcs.modified, so it can disagree with 'harmonik version --binary' unnoticed:
$STATUS_OUT"
else
    pass "DISCLOSED: status reports the provenance the binary carries"
fi

# ---------------------------------------------------------------------------
# VERDICT-ACCEPTS — `provenance` is `status` with an exit code on it.
#
# `status` prints the stamp for a human to read. Nobody read it: the live-verify
# gate printed a DIRTY stamp and an all-green grid in one log and the run was
# recorded as a PASS (hk-48zdw). A gate needs a verdict it cannot walk past, so
# `provenance` exits 0 for exactly one state — stamp present, revision equal to
# the pin, vcs.modified=false — and refuses every other state by name.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
ACCEPT_OUT="$($SD provenance "$SCRATCH" 2>&1)"
ACCEPT_STATUS=$?
if [ "$ACCEPT_STATUS" -ne 0 ]; then
    fail "VERDICT-ACCEPTS: 'provenance' exited $ACCEPT_STATUS on the binary the documented sequence produced, so no gate could ever pass:
$ACCEPT_OUT"
elif ! grep -q "^SCRATCH_PROVENANCE clean revision=$REV modified=false pinned=$REV\$" <<<"$ACCEPT_OUT"; then
    fail "VERDICT-ACCEPTS: exit 0 but the machine-readable line is not the expected clean line:
$ACCEPT_OUT"
else
    pass "VERDICT-ACCEPTS: 'provenance' exits 0 and prints the clean token for a correctly built binary"
fi

# ---------------------------------------------------------------------------
# VERDICT-FAILS-CLOSED — every state in which the question cannot be ANSWERED
# must refuse, not shrug.
#
# This is the direction a "make the gate green" change breaks first. A check that
# treats "no binary", "no pin" or "no stamp" as anything but a refusal is a check
# that passes whenever it is broken, which is worse than not having it: it is the
# same false PASS the bead was filed for, wearing the fix's own label.
# ---------------------------------------------------------------------------
for closed_case in no-binary no-pin no-stamp; do
    CLOSED="$ROOT/s-$closed_case"
    rm -rf "$CLOSED"
    case "$closed_case" in
        no-binary)
            cp -R "$SCRATCH" "$CLOSED" && rm -f "$CLOSED/.harmonik/bin/harmonik"
            want_token="no-binary"
            ;;
        no-pin)
            # A directory that no `init --rev` ever placed. Nothing can say what
            # code it holds, so nothing may report a result from it.
            mkdir -p "$CLOSED"
            want_token="not-pinned"
            ;;
        no-stamp)
            # -buildvcs=false is one flag away from every real build command, and
            # it produces a binary that names no revision at all.
            cp -R "$SCRATCH" "$CLOSED"
            go build -C "$CLOSED" -buildvcs=false -o "$CLOSED/.harmonik/bin/harmonik" ./cmd/harmonik \
                >"$ROOT/build-nostamp.out" 2>&1
            want_token="no-stamp"
            ;;
    esac

    assertions=$((assertions + 1))
    CLOSED_OUT="$($SD provenance "$CLOSED" 2>&1)"
    CLOSED_STATUS=$?
    if [ "$CLOSED_STATUS" -eq 0 ]; then
        fail "VERDICT-FAILS-CLOSED/$closed_case: 'provenance' exited 0 for a scratch whose provenance cannot be read:
$CLOSED_OUT"
    elif ! grep -q "^SCRATCH_PROVENANCE $want_token " <<<"$CLOSED_OUT"; then
        fail "VERDICT-FAILS-CLOSED/$closed_case: refused, but not with the '$want_token' token a caller keys on:
$CLOSED_OUT"
    else
        pass "VERDICT-FAILS-CLOSED/$closed_case: refuses with token '$want_token'"
    fi
    rm -rf "$CLOSED"
done

# ---------------------------------------------------------------------------
# HIDDEN-INPUT — a build input git does not report must still be seen.
#
# This is the half that keeps the gate from lying by accident rather than by
# edit. Both fixtures below reach the binary while `git status` in its ordinary
# spelling says nothing, so Go stamps vcs.modified=false and the provenance tool
# exits 0. The only thing standing between that and a false ACCEPT is whether
# local_edits SEES them, so that the build labels itself +local-edits.
#
#   embed-tree     .gitignore hides `*~`, and cmd/harmonik/init_skill_assets.go
#                  embeds the whole assets subtree, so an editor backup file
#                  left in it is compiled in. No malice required.
#   quoted-path    git C-quotes an unusual path and wraps it in double quotes,
#                  so a match on the reported line does not match its own
#                  target. The same embedded asset with a non-ASCII name is
#                  reported as "...SKILL-caf\\303\\251.md~" and was dropped
#                  from the sweep even once the pathspec covered its directory.
# ---------------------------------------------------------------------------
HIDDEN_EMBED="$SCRATCH/cmd/harmonik/assets/skills/keeper/SKILL.md~"
HIDDEN_QUOTED="$SCRATCH/cmd/harmonik/assets/skills/keeper/SKILL-café.md~"

for hidden_case in embed-tree quoted-path; do
    case "$hidden_case" in
        embed-tree)
            mkdir -p "$(dirname "$HIDDEN_EMBED")"
            printf 'hidden asset\n' >"$HIDDEN_EMBED"
            hidden_path="$HIDDEN_EMBED"
            ;;
        quoted-path)
            mkdir -p "$(dirname "$HIDDEN_QUOTED")"
            printf 'hidden asset\n' >"$HIDDEN_QUOTED"
            hidden_path="$HIDDEN_QUOTED"
            ;;
    esac

    # Confirm the fixture is actually hidden. If .gitignore stops covering it the
    # case still passes for the wrong reason, so this is checked, not assumed.
    assertions=$((assertions + 1))
    if git -C "$SCRATCH" check-ignore -q -- "$hidden_path"; then
        pass "HIDDEN-INPUT/$hidden_case: the fixture is genuinely ignored by git"
    else
        fail "HIDDEN-INPUT/$hidden_case: the fixture is NOT ignored, so this case no longer tests a hidden input"
    fi

    $SD build "$SCRATCH" >"$ROOT/build-hidden-$hidden_case.out" 2>&1
    HIDDEN_BUILD_STATUS=$?

    assertions=$((assertions + 1))
    if [ "$HIDDEN_BUILD_STATUS" -ne 0 ]; then
        fail "HIDDEN-INPUT/$hidden_case: build failed (exit $HIDDEN_BUILD_STATUS)"
        cat "$ROOT/build-hidden-$hidden_case.out" >&2
    else
        HIDDEN_STAMP="$(cat "$SCRATCH/.harmonik/bin/built-revision" 2>/dev/null || true)"
        if [ "$HIDDEN_STAMP" = "${REV}+local-edits" ]; then
            pass "HIDDEN-INPUT/$hidden_case: a build input git hides is still labelled +local-edits"
        else
            fail "HIDDEN-INPUT/$hidden_case: a build input git hides was labelled '$HIDDEN_STAMP', not '${REV}+local-edits' — the gate would accept a binary holding code that is not in $REV"
        fi
    fi

    rm -f "$hidden_path"
done

# ---------------------------------------------------------------------------
# BATCH-LABELLED — the revision on the BATCH_SUMMARY line is a bare commit hash
# only when the binary can prove it is that commit.
#
# This reproduces the exact shape of hk-48zdw rather than a stand-in for it. The
# fixture is `review-loop.dot` at the project root, which is the file
# scripts/core-loop-seed.sh writes before the gate builds, and which local_edits
# EXCLUDES by name. So the two measurements disagree in the dangerous direction:
# the harness calls the tree clean and labels the build a bare hash, while Go
# counts the untracked file and stamps vcs.modified=true. The assessor contract
# reads a bare hash as "built from a clean tree", and the bare hash is the
# machine-readable half.
#
# The first two assertions establish that the disagreement is REAL. Without them
# the third could pass because the harness noticed the file, which would be a
# different fix and would leave this one untested.
#
# WHY A LISTENING SOCKET. `batch` brings a daemon up when it finds no socket, and
# a self-test must not start one. A socket file that nothing serves takes the
# already-up path, so the label is computed and printed and the submit then fails
# harmlessly a few lines later. The label is what this case reads.
# ---------------------------------------------------------------------------
LABEL_SOCK="$SCRATCH/.harmonik/daemon.sock"
cp "$SCRATCH/go.mod" "$SCRATCH/review-loop.dot"
$SD build "$SCRATCH" >"$ROOT/build-label.out" 2>&1
LABEL_BUILD_STATUS=$?

assertions=$((assertions + 1))
LABEL_STAMP="$(cat "$SCRATCH/.harmonik/bin/built-revision" 2>/dev/null || true)"
if [ "$LABEL_BUILD_STATUS" -ne 0 ]; then
    fail "BATCH-LABELLED: build failed (exit $LABEL_BUILD_STATUS)"
    cat "$ROOT/build-label.out" >&2
elif [ "$LABEL_STAMP" != "$REV" ]; then
    fail "BATCH-LABELLED: the harness labelled the build '$LABEL_STAMP', so local_edits no longer excludes review-loop.dot and this case no longer reproduces hk-48zdw"
else
    pass "BATCH-LABELLED: the harness still labels this tree a bare $REV (the disagreement is real)"
fi

assertions=$((assertions + 1))
LABEL_MODIFIED="$(vcs_field "$BIN" 'vcs\.modified')"
if [ "$LABEL_MODIFIED" != "true" ]; then
    fail "BATCH-LABELLED: Go stamped vcs.modified='$LABEL_MODIFIED' (want true) for a tree holding an untracked review-loop.dot — the two measurements no longer disagree, so this case tests nothing"
else
    pass "BATCH-LABELLED: Go still stamps vcs.modified=true for the same tree"
fi

assertions=$((assertions + 1))
if ! command -v python3 >/dev/null 2>&1; then
    fail "BATCH-LABELLED: python3 is absent, so no listening socket could be placed and 'batch' could not be driven without starting a daemon — the label this case exists to prove was NOT tested"
else
    rm -f "$LABEL_SOCK"
    python3 -c "
import socket, sys, time
s = socket.socket(socket.AF_UNIX)
s.bind(sys.argv[1])
s.listen(1)
time.sleep(120)
" "$LABEL_SOCK" >/dev/null 2>&1 &
    LABEL_SOCK_PID=$!
    for _ in 1 2 3 4 5 6 7 8 9 10; do [ -S "$LABEL_SOCK" ] && break; sleep 0.5; done
    if [ ! -S "$LABEL_SOCK" ]; then
        fail "BATCH-LABELLED: could not place a listening socket at $LABEL_SOCK, so 'batch' would have started a daemon — case NOT tested"
    else
        LABEL_OUT="$($SD batch "$SCRATCH" provlabel --beads no-such-bead 2>&1)"
        if ! grep -q "batch 'provlabel' — revision:" <<<"$LABEL_OUT"; then
            fail "BATCH-LABELLED: 'batch' never reached the line that names the revision, so the label was not exercised:
$LABEL_OUT"
        elif grep -q "batch 'provlabel' — revision: ${REV}\$" <<<"$LABEL_OUT"; then
            fail "BATCH-LABELLED: 'batch' labelled the run a BARE $REV while Go had stamped the binary vcs.modified=true. BATCH_SUMMARY carries this value, and a bare hash is what the assessor contract reads as clean:
$LABEL_OUT"
        else
            pass "BATCH-LABELLED: 'batch' refuses to name a bare commit for a binary Go stamped dirty"
        fi
    fi
    kill "$LABEL_SOCK_PID" 2>/dev/null || true
    wait "$LABEL_SOCK_PID" 2>/dev/null || true
    rm -f "$LABEL_SOCK"
fi
rm -f "$SCRATCH/review-loop.dot"

# ---------------------------------------------------------------------------
# STILL-DIRTY + NO-CLOBBER — a real edit to compiled code is still refused, and
# is not silently reverted.
#
# This is the half that keeps the fix from being a no-op. If the reconcile ran
# unconditionally, or if the check were relaxed to ignore vcs.modified, the two
# assertions below would fail while every assertion above still passed.
# ---------------------------------------------------------------------------
printf 'package main\n\nfunc main() { _ = "edited by provenance-test" }\n' >"$SCRATCH/cmd/harmonik/main.go"
$SD build "$SCRATCH" >"$ROOT/build-dirty.out" 2>&1
DIRTY_BUILD_STATUS=$?

assertions=$((assertions + 1))
if [ "$DIRTY_BUILD_STATUS" -ne 0 ]; then
    fail "STILL-DIRTY: build of an edited tree failed (exit $DIRTY_BUILD_STATUS)"
    cat "$ROOT/build-dirty.out" >&2
else
    DIRTY_MODIFIED="$(vcs_field "$BIN" 'vcs\.modified')"
    DIRTY_STAMP="$(cat "$SCRATCH/.harmonik/bin/built-revision" 2>/dev/null || true)"
    if [ "$DIRTY_MODIFIED" != "true" ]; then
        fail "STILL-DIRTY: an edited build input produced vcs.modified='$DIRTY_MODIFIED' (want true) — the provenance check has been made unable to fail"
    elif [ "$DIRTY_STAMP" != "${REV}+local-edits" ]; then
        fail "STILL-DIRTY: an edited build input was labelled '$DIRTY_STAMP', not '${REV}+local-edits'"
    else
        pass "STILL-DIRTY: an edited build input still stamps vcs.modified=true and labels +local-edits"
    fi
fi

assertions=$((assertions + 1))
DIRTY_PROV="$("$HK" version --binary "$BIN" --contains "$REV" --repo "$SCRATCH" 2>&1)"
DIRTY_PROV_STATUS=$?
if [ "$DIRTY_PROV_STATUS" -ne 3 ]; then
    fail "STILL-DIRTY: 'harmonik version --binary --contains' exited $DIRTY_PROV_STATUS (want 3, contains-dirty) on a binary built from an edited tree:
$DIRTY_PROV"
else
    pass "STILL-DIRTY: the provenance tool still refuses a binary built from an edited tree (exit 3)"
fi

assertions=$((assertions + 1))
if ! grep -q 'edited by provenance-test' "$SCRATCH/cmd/harmonik/main.go"; then
    fail "NO-CLOBBER: the build reverted a real edit to compiled code — the reconcile ran on a tree it had no business touching"
else
    pass "NO-CLOBBER: a real edit to compiled code survives the build"
fi

# ---------------------------------------------------------------------------
# VERDICT-REFUSES + GATE-REFUSES — the dirty binary above must be refused by the
# verdict command AND by the gate that consumes it.
#
# The second half is the one that matters. The verdict command could be perfect
# and the live-verify gate could still print a green grid beside it, which is
# precisely the shape of hk-48zdw: the truthful line existed, twenty-five lines
# above the verdict, and nothing acted on it. So this drives the real runner and
# requires it to stop before it grades anything.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
REFUSE_OUT="$($SD provenance "$SCRATCH" 2>&1)"
REFUSE_STATUS=$?
if [ "$REFUSE_STATUS" -eq 0 ]; then
    fail "VERDICT-REFUSES: 'provenance' exited 0 for a binary built from an edited tree:
$REFUSE_OUT"
elif ! grep -q '^SCRATCH_PROVENANCE dirty ' <<<"$REFUSE_OUT"; then
    fail "VERDICT-REFUSES: refused, but not with the 'dirty' token a caller keys on:
$REFUSE_OUT"
else
    pass "VERDICT-REFUSES: 'provenance' refuses a binary built from an edited tree, with token 'dirty'"
fi

assertions=$((assertions + 1))
if ! command -v jq >/dev/null 2>&1; then
    fail "GATE-REFUSES: jq is absent, so scripts/core-loop-matrix.sh could not be driven — the wiring this case exists to prove was NOT tested"
else
    GATE_OUT="$(bash "$REPO_ROOT/scripts/core-loop-matrix.sh" "$SCRATCH" \
        --no-cycle --gate --json --harnesses pi --substrates local 2>&1)"
    GATE_STATUS=$?
    if [ "$GATE_STATUS" -eq 0 ]; then
        fail "GATE-REFUSES: core-loop-matrix.sh --gate exited 0 on a binary that cannot name its commit:
$GATE_OUT"
    elif grep -q 'MATRIX_JSON' <<<"$GATE_OUT"; then
        fail "GATE-REFUSES: the gate printed a MATRIX_JSON grid for a binary that cannot name its commit — a grid beside a bare hash is what an assessor folds into a PASS:
$GATE_OUT"
    elif ! grep -q 'provenance=dirty' <<<"$GATE_OUT"; then
        fail "GATE-REFUSES: the gate stopped, but did not say the provenance was the reason:
$GATE_OUT"
    else
        pass "GATE-REFUSES: core-loop-matrix.sh --gate stops on a dirty binary, before it grades anything"
    fi
fi

# ---------------------------------------------------------------------------
# GATE-LENIENT-REPORTS — without --gate the runner keeps going, and the field the
# assessor folds must still say the run was not an audit.
#
# GATE-REFUSES above only covers the mode that STOPS. The lenient mode is the one
# that produced hk-48zdw: it ran the whole matrix on a binary that could not name
# its commit, exited 0, and printed a grid. Lenient still exits 0 on purpose — the
# edit-and-cycle developer loop lives there — so the exit code is NOT the signal,
# and `all_green` and `provenance.status` in MATRIX_JSON are. If those two ever go
# back to describing cells alone, the exact false PASS comes back with the gate's
# own fix in place.
#
# HONEST LIMIT OF THIS CASE. The fixture has no seed map, so its one pi:local cell
# is PENDING, and pending alone already forces all_green:false. This case therefore
# proves that lenient does not stop, that the token reaches the machine-readable
# output where a consumer can key on it, and that a VOID line is printed beside the
# grid. It does NOT, on its own, prove the provenance term is what falsifies
# all_green — that needs a grid with every cell green, which needs a real daemon and
# a real agent. `make core-loop-lt` is where that gets exercised for real.
# ---------------------------------------------------------------------------
assertions=$((assertions + 1))
if ! command -v jq >/dev/null 2>&1; then
    fail "GATE-LENIENT-REPORTS: jq is absent, so scripts/core-loop-matrix.sh could not be driven — the JSON contract this case exists to prove was NOT tested"
else
    LENIENT_OUT="$(bash "$REPO_ROOT/scripts/core-loop-matrix.sh" "$SCRATCH" \
        --no-cycle --json --harnesses pi --substrates local 2>&1)"
    LENIENT_JSON="$(grep '^MATRIX_JSON ' <<<"$LENIENT_OUT" | tail -1 | sed 's/^MATRIX_JSON //')"
    if [ -z "$LENIENT_JSON" ]; then
        fail "GATE-LENIENT-REPORTS: the lenient run emitted no MATRIX_JSON line, so the field the assessor folds was not produced:
$LENIENT_OUT"
    else
        LENIENT_PROV="$(jq -r '.provenance.status // "<absent>"' <<<"$LENIENT_JSON" 2>/dev/null)"
        LENIENT_GREEN="$(jq -r '.all_green | tostring' <<<"$LENIENT_JSON" 2>/dev/null)"
        if [ "$LENIENT_PROV" != "dirty" ]; then
            fail "GATE-LENIENT-REPORTS: MATRIX_JSON reported provenance.status='$LENIENT_PROV' (want dirty) for a binary built from an edited tree:
$LENIENT_JSON"
        elif [ "$LENIENT_GREEN" != "false" ]; then
            fail "GATE-LENIENT-REPORTS: MATRIX_JSON reported all_green=$LENIENT_GREEN for a run whose binary cannot name its commit. That field IS the assessor's fold:
$LENIENT_JSON"
        elif ! grep -q '^MATRIX_PROVENANCE VOID' <<<"$LENIENT_OUT"; then
            fail "GATE-LENIENT-REPORTS: no 'MATRIX_PROVENANCE VOID' line was printed beside the grid, so a human reading the table is back to having to notice a warning elsewhere in the log:
$LENIENT_OUT"
        else
            pass "GATE-LENIENT-REPORTS: the lenient run keeps going but reports all_green=false, provenance.status=dirty, and a VOID line beside the grid"
        fi
    fi
fi

# ---------------------------------------------------------------------------
# MONOTONIC — the stamp is read twice, and the SECOND reading may never clear the
# first.
#
# WHY THIS IS A CASE AND NOT A COMMENT. Lenient mode does not stop on a bad
# pre-flight: it warns and grades every cell with the binary that reading refused.
# If anything cleans the tree and rebuilds before the runner re-reads the stamp
# beside the grid, the plain "last read wins" version reports `clean` — for a grid
# produced entirely by the binary it had already refused. That is hk-48zdw's false
# PASS again, arriving through the fix for it. MEASURED: with the monotonic rule
# removed, the run below reports provenance `clean` and prints no VOID line.
#
# HOW IT IS DRIVEN. `scratch-daemon.sh provenance` cannot be made to answer `dirty`
# then `clean` for one real binary, so the collaborator is stubbed and the runner is
# real: core-loop-matrix.sh is COPIED from this repo (never a checked-in duplicate,
# so it cannot drift from the file it tests) next to a stub scratch-daemon.sh whose
# first answer is dirty and whose later answers are clean. This is the same shape as
# the stub `go` in scripts/gate-fails-closed-test.sh.
#
# WHAT IT DOES NOT COVER. With the real provenance command stubbed out, this case
# says nothing about whether the stamp is read correctly — VERDICT-ACCEPTS and
# VERDICT-REFUSES above own that. It covers only how the runner FOLDS the answers.
#
# BOTH DIRECTIONS, because one alone is satisfied by a runner that hardcodes an
# answer: dirty-then-clean must stay dirty, and clean-then-clean must stay clean.
# ---------------------------------------------------------------------------
mono_run() {
    # mono_run <name> <first-answer> — build a stubbed runner tree and run it.
    # Echoes the reported provenance token; exits non-zero if nothing was reported.
    local name="$1" first="$2" mono
    mono="$ROOT/mono-$name"
    rm -rf "$mono"
    mkdir -p "$mono/scripts" "$mono/scratch/.harmonik"
    cp "$REPO_ROOT/scripts/core-loop-matrix.sh" "$mono/scripts/core-loop-matrix.sh"
    # core-loop-matrix.sh reads harnesses.pi out of this file at top level before it
    # reaches the provenance block; an empty file is enough and keeps the pi cell
    # PENDING, so no agent, no daemon and no network are involved.
    : >"$mono/scratch/.harmonik/config.yaml"
    cat >"$mono/scripts/scratch-daemon.sh" <<STUB
#!/usr/bin/env bash
# Stub collaborator: answers '$first' the first time, 'clean' every time after.
[ "\${1:-}" = "provenance" ] || exit 0
echo x >>"$mono/calls"
if [ "\$(wc -l <"$mono/calls" | tr -d ' ')" -eq 1 ]; then
    case "$first" in
        clean) echo "SCRATCH_PROVENANCE clean revision=deadbeef modified=false pinned=deadbeef"; exit 0;;
        *)     echo "SCRATCH_PROVENANCE $first revision=deadbeef modified=true pinned=deadbeef"; exit 1;;
    esac
fi
echo "SCRATCH_PROVENANCE clean revision=deadbeef modified=false pinned=deadbeef"
STUB
    chmod +x "$mono/scripts/scratch-daemon.sh"
    bash "$mono/scripts/core-loop-matrix.sh" "$mono/scratch" \
        --no-cycle --json --harnesses pi --substrates local >"$mono/out.log" 2>&1
    grep -E '^MATRIX_PROVENANCE (clean|dirty|unchecked|unreadable|inconsistent|no-stamp|no-binary|not-pinned|revision-mismatch) ' "$mono/out.log" \
        | tail -1 | awk '{print $2}'
}

assertions=$((assertions + 1))
if ! command -v jq >/dev/null 2>&1; then
    fail "MONOTONIC: jq is absent, so scripts/core-loop-matrix.sh could not be driven — the fold this case exists to prove was NOT tested"
else
    MONO_TOKEN="$(mono_run dirty-then-clean dirty)"
    if [ "$MONO_TOKEN" != "dirty" ]; then
        fail "MONOTONIC: a run whose first stamp read said 'dirty' reported provenance '$MONO_TOKEN' after a later read came back clean. Every cell was graded by the binary the first read refused:
$(cat "$ROOT/mono-dirty-then-clean/out.log" 2>/dev/null)"
    elif ! grep -q '^MATRIX_PROVENANCE VOID' "$ROOT/mono-dirty-then-clean/out.log"; then
        fail "MONOTONIC: the token stayed dirty but no VOID line was printed beside the grid:
$(cat "$ROOT/mono-dirty-then-clean/out.log" 2>/dev/null)"
    else
        pass "MONOTONIC: a later 'clean' reading cannot clear an earlier refusal"
    fi

    assertions=$((assertions + 1))
    MONO_CLEAN_TOKEN="$(mono_run clean-then-clean clean)"
    if [ "$MONO_CLEAN_TOKEN" != "clean" ]; then
        fail "MONOTONIC: a run whose stamp read clean both times reported provenance '$MONO_CLEAN_TOKEN', so the token is stuck rather than monotonic and no gate could ever pass:
$(cat "$ROOT/mono-clean-then-clean/out.log" 2>/dev/null)"
    else
        pass "MONOTONIC: a run that reads clean throughout still reports clean (the rule refuses, it is not stuck)"
    fi
fi

# ---------------------------------------------------------------------------
printf 'provenance-test: %d assertion(s), %d failure(s)\n' "$assertions" "$failures"
[ "$failures" -eq 0 ] || exit 1
