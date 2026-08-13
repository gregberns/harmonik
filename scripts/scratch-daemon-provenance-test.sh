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
#   DISCLOSED    `status` must print the provenance the BINARY carries, not only
#                the provenance this script measured. The two disagreed for a
#                whole assessment because nothing ever printed them together.
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
printf 'provenance-test: %d assertion(s), %d failure(s)\n' "$assertions" "$failures"
[ "$failures" -eq 0 ] || exit 1
