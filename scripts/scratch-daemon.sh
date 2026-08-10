#!/usr/bin/env bash
# scratch-daemon.sh — run a SECOND, fully-isolated harmonik daemon on a separate
# git clone and pkill+rebuild it in seconds, WITHOUT ever touching the fleet daemon.
#
# WHY: the real-daemon reproducer loop (e.g. the remote-substrate localhost e2e)
# is otherwise a ~30-minute round trip. A scratch clone with its own socket, its
# own tmux session, and its own binary lets you `cycle` (down → build → up) in
# seconds against code you just edited — while your production "fleet" daemon
# keeps running untouched on the real project.
#
# ISOLATION (all derived per-project, automatically — no shared global state):
#   socket   <scratch>/.harmonik/daemon.sock          (internal/daemon/daemon.go)
#   pidfile  <scratch>/.harmonik/daemon.pid           (internal/lifecycle/pidfile.go)
#   tmux     harmonik-<projecthash>-default           (DefaultSessionName, internal/lifecycle/tmux)
#            projecthash = first 12 hex of SHA-256(realpath(scratch))  (PL-006a)
#   binary   <scratch>/.harmonik/bin/harmonik         (built FROM the scratch clone)
# Because every handle is keyed off the scratch path, a second daemon on a
# different path can never collide with — or be mistaken for — the fleet daemon.
#
# SAFETY: this script NEVER targets the fleet daemon. `down` kills ONLY the PID
# named in <scratch>/.harmonik/daemon.pid, and only after confirming that live
# process's command line actually contains the scratch path. A blanket
# `pkill harmonik` (or even `pkill -f "harmonik --project"`) would kill the fleet
# daemon — this script deliberately does neither.
#
# THE REVISION UNDER AUDIT IS A REQUIRED INPUT.
#   `init` will not run without --rev. This script exists to grade ONE commit, and
#   until hk-scratch-daemon-audits-wrong-tree-zljvm it had no way to be told which
#   one: it cloned the origin URL (which serves the remote's DEFAULT BRANCH, not
#   your candidate), it skipped the clone when the scratch directory already had a
#   .git (so a second run graded the first run's leftovers), and it never ran
#   fetch, checkout, reset or pull at all. `build` then printed the tree's own HEAD,
#   which made a stale tree look chosen. The result was a green verdict on the
#   wrong code.
#   Now: --rev is required, the tree is FORCED to it, HEAD is READ BACK and
#   compared, and build / up / status / batch each name the revision they act on.
#   An existing scratch tree is a hard error unless you pass --reuse, and --reuse
#   still forces the tree to --rev.
#
# Usage:
#   ./scripts/scratch-daemon.sh init   <scratch-path> --rev <commit-ish> [--source <repo>] [--reuse]
#   ./scripts/scratch-daemon.sh build  <scratch-path>
#   ./scripts/scratch-daemon.sh up     <scratch-path>
#   ./scripts/scratch-daemon.sh status <scratch-path>
#   ./scripts/scratch-daemon.sh down   <scratch-path>
#   ./scripts/scratch-daemon.sh cycle  <scratch-path>   # down + build + up (the fast loop)
#   ./scripts/scratch-daemon.sh batch  <scratch-path> <name> --beads id1,id2,...  # submit + structured pass/fail
#   ./scripts/scratch-daemon.sh batch  <scratch-path> <name> --file  <queue.json> # submit a queue-file batch
#   ./scripts/scratch-daemon.sh batch  <scratch-path> <name> --from-events <ndjson> # OFFLINE: fold a captured
#                             event stream into the same results artifact + BATCH_SUMMARY. No daemon, no submit,
#                             no subscribe. The <ndjson> file is READ ONLY and is never moved or removed.
#   ./scripts/scratch-daemon.sh feedback <results-json> [--batch <name>] [--dry-run] # scratch FAILURES -> deduped MAIN-repo beads
#
# batch writes TWO artifacts under <scratch>/.harmonik/: the results JSON and a RETAINED
# event capture (batch-<name>-<queue_id>.events.ndjson). Both paths are printed on the
# BATCH_SUMMARY line. The capture is kept so an audit step can check event ordering.
#
# Options (env vars):
#   SCRATCH_MAX_CONCURRENT  — daemon --max-concurrent      (default: 1)
#   SCRATCH_WORKFLOW_MODE   — daemon --workflow-mode        (default: dot)
#   SCRATCH_DAEMON_FLAGS    — extra flags appended verbatim to the daemon start
#   SCRATCH_BATCH_TIMEOUT   — batch: max seconds to await terminal events (default: 1800)
#   SCRATCH_DEBUG_WIRING    — HARMONIK_DEBUG_WIRING for the daemon (default: 1). Prints
#                             the composition-root audit table at boot; that table is the
#                             boot record for the queue-only subsystems posture.
#
# init options:
#   --rev <commit-ish>  REQUIRED. The commit, branch or tag under audit.
#   --source <repo>     Where to fetch it from. Default: this script's own checkout,
#                       NOT its origin URL — an unpushed candidate must still work.
#   --reuse             Keep an existing scratch tree instead of failing. The tree is
#                       still forced to --rev, so reuse saves the clone and nothing else.
#                       It also drops the previous run's binary, so `up` cannot start a
#                       daemon built from the revision you just moved away from.
#
# Pairs with the fast remote reproducer:
#   go test -tags=scenario -run TestScenario_RemoteSubstrate_Localhost_DOT_E2E ./internal/daemon/
#
# Refs: hk-4tdlw (scratch-clone standalone test-daemon iteration loop),
#       hk-scratch-daemon-audits-wrong-tree-zljvm (required + verified revision pin).

set -euo pipefail

# --- one-line safety banner (printed on every invocation) -------------------
echo "[scratch-daemon] SAFETY: operates ONLY on the scratch clone you name; never targets the fleet daemon." >&2

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

die() { echo "[scratch-daemon] ERROR: $*" >&2; exit 1; }

# fleet_root: the canonical, symlink-resolved path of THIS script's own git repo
# (the live fleet/production checkout). Empty if the script is not inside a repo.
# Used by guard_path to hard-refuse operating on the fleet checkout itself.
fleet_root() {
    local d
    d="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel 2>/dev/null)" || return 0
    [ -n "$d" ] || return 0
    ( cd "$d" && pwd -P )
}

# guard_path: reject an empty path, "/", or this script's own fleet repo root, so
# a typo (or a symlink-to-fleet) can never turn a kill/rm into a catastrophe.
# Canonicalizes via `pwd -P` (resolves symlinks) so the path used for the pidfile,
# the argv ownership check, and the project-hash-derived tmux session identity all
# agree with harmonik's own filepath.EvalSymlinks (PL-006a). Echoes the resolved
# absolute path on success.
guard_path() {
    local p="${1:-}"
    [ -n "$p" ] || die "scratch-path is required"
    case "$p" in
        /) die "refusing to operate on '/'";;
    esac
    # Resolve to an absolute, symlink-free path. The dir may not exist yet (init),
    # so resolve the parent (symlinks and all) and re-append the leaf.
    local resolved
    if [ -d "$p" ]; then
        resolved="$( cd "$p" && pwd -P )"
    else
        local parent leaf
        parent="$(dirname "$p")"
        leaf="$(basename "$p")"
        [ -d "$parent" ] || die "parent directory of '$p' does not exist"
        resolved="$( cd "$parent" && pwd -P )/$leaf"
    fi
    # scratch≠fleet guard: never operate on this script's own repo root (the live
    # fleet checkout). Both sides are symlink-resolved, so a scratch path that is a
    # symlink pointing at the fleet repo is also caught here.
    local fleet
    fleet="$(fleet_root)"
    if [ -n "$fleet" ] && [ "$resolved" = "$fleet" ]; then
        die "refusing: '$resolved' is this script's own repo root (the fleet checkout) — point at a SEPARATE scratch clone. Drive the loop from your fleet checkout's copy of this script."
    fi
    echo "$resolved"
}

# assert_not_supervised: defense-in-depth atop guard_path. Refuse to start/stop a
# daemon for a project that has a LIVE auto-revive supervisor (hk-<hash>-supervise)
# — that is a supervised fleet deployment, never a throwaway scratch clone. Needs
# the scratch binary to derive the hash; skips silently if it is not built yet
# (build/init are non-killing ops, and `up` builds before this runs in `cycle`).
assert_not_supervised() {
    local scratch="$1" bin hash
    bin="$(scratch_bin "$scratch")"
    [ -x "$bin" ] || return 0
    hash="$("$bin" project-hash --project "$scratch" 2>/dev/null)" || return 0
    [ -n "$hash" ] || return 0
    if tmux has-session -t "hk-${hash}-supervise" 2>/dev/null; then
        die "refusing: project has a live supervisor session (hk-${hash}-supervise) — looks like a supervised fleet daemon, not a scratch clone"
    fi
}

scratch_bin()     { echo "$1/.harmonik/bin/harmonik"; }
scratch_sock()    { echo "$1/.harmonik/daemon.sock"; }
scratch_pidfile() { echo "$1/.harmonik/daemon.pid"; }
scratch_log()     { echo "$1/.harmonik/scratch-daemon.log"; }
scratch_origin()  { echo "$1/.harmonik/scratch-origin.git"; }
# The commit `init --rev` pinned this scratch tree to, plus the spelling the
# caller asked for. Written by init, read by every subcommand that reports a
# result, so no result can be printed without naming the code it came from.
scratch_revfile() { echo "$1/.harmonik/audit-revision"; }
# The revision the CURRENT scratch binary was built from. Deleting the binary is
# not enough on its own — this pairs a binary with a commit so `up` can refuse a
# daemon whose executable and whose tree disagree.
scratch_binrevfile() { echo "$1/.harmonik/bin/built-revision"; }

# ---------------------------------------------------------------------------
# Revision pinning
#
# WHY THIS EXISTS. This script audits a candidate commit. Before hk-scratch-
# daemon-audits-wrong-tree-zljvm it had no way to be told which commit that was:
# `init` cloned the origin URL, which takes the remote's DEFAULT BRANCH; it
# skipped the clone entirely when the scratch already had a .git, so a second
# run graded whatever the first run left behind; and `git fetch`, `git checkout`,
# `git reset` and `git pull` appeared nowhere in the file, so nothing ever moved
# the tree to a named commit. `build` then printed the tree's own HEAD, which
# made a stale tree read as a deliberate choice. A wrong tree reported green.
#
# The rule now: the revision is a REQUIRED input, the tree is FORCED to it, HEAD
# is READ BACK and compared, and every verdict names the commit it came from.
# ---------------------------------------------------------------------------

# resolve_rev <repo> <commit-ish> — print the full SHA a commit-ish names, or
# return 1.
#
# ORDER IS LOAD-BEARING. refs/remotes/scratch-source/* is what the fetch just
# wrote, so it holds the freshest view of the source. The bare spelling is tried
# AFTER it, because in a reused clone the bare name can still resolve to a stale
# LOCAL branch left by an earlier run: the fetch writes only the scratch-source
# namespace, so a bare `main` would quietly name the old commit while the new one
# sat right there unused. A raw SHA and a tag are unaffected — neither resolves
# under scratch-source, so both fall through to the bare spelling.
resolve_rev() {
    local repo="$1" rev="$2" cand out
    for cand in "refs/remotes/scratch-source/$rev" "$rev" "refs/remotes/origin/$rev"; do
        if out="$(git -C "$repo" rev-parse --verify --quiet "${cand}^{commit}" 2>/dev/null)"; then
            printf '%s\n' "$out"
            return 0
        fi
    done
    return 1
}

# audit_revision <scratch> — the commit init pinned the tree to. Empty when the
# tree carries no pin.
audit_revision() {
    local f
    f="$(scratch_revfile "$1")"
    [ -f "$f" ] || return 0
    cut -f1 <"$f" | head -1
}

# assert_pinned <scratch> — refuse to act on a tree whose revision is unknown or
# has moved, and print the pinned commit so the caller can name it.
#
# Two separate refusals, because they are two different accidents. NO PIN means
# the tree was never placed by `init --rev`, so nothing can say what it holds. A
# MISMATCH means something moved HEAD after init — a hand checkout, a stray
# script — and the run would report on code that is not the code under audit.
# Neither can be recovered from here, and neither may be reported as a result.
assert_pinned() {
    local scratch="$1" want head
    want="$(audit_revision "$scratch")"
    if [ -z "$want" ]; then
        die "$scratch carries no audit revision — it was not placed by '$0 init <scratch> --rev <commit-ish>'.
  This script cannot name the commit in that tree, so it will not report a result from it.
  Fix: rm -rf '$scratch' && $0 init '$scratch' --rev <commit-ish>"
    fi
    head="$(git -C "$scratch" rev-parse HEAD 2>/dev/null)" \
        || die "$scratch is not a git tree — run: $0 init '$scratch' --rev <commit-ish>"
    if [ "$head" != "$want" ]; then
        die "$scratch has drifted off its audited revision.
  init pinned : $want
  HEAD is now : $head
  A result from this tree would name the wrong commit. Re-init to the revision you mean to audit."
    fi
    printf '%s\n' "$want"
}

# local_edits <scratch> — list every difference between the working tree and the
# pinned commit that would end up INSIDE the binary. Empty output means the
# binary this tree produces really is the pinned commit.
#
# WHY THIS IS SCOPED TO BUILD INPUTS, AND NOT "IS THE TREE CLEAN". A correct init
# does not leave a clean tree: `harmonik init --force` rewrites AGENTS.md,
# AGENT_INDEX.md and STATUS.md while bootstrapping the project. A clean-tree rule
# would therefore accuse every real scratch of tampering. Worse, the daemon under
# test runs `reset --hard` on the project after a successful landing, which
# REVERSES those same rewrites — so a rule keyed on "the tree still looks how
# init left it" flips state when nobody edited anything, and `make core-loop-lt`
# would start failing on its second cycle. Both measured on a real scratch clone,
# not assumed.
#
# Build inputs have neither problem. Measured on a real scratch: init and build
# together modify NOTHING that Go compiles, so the honest baseline here is empty,
# and no baseline file is needed at all.
#
# WHY THIS EXCLUDES RATHER THAN SELECTS. The first version of this listed the
# build inputs it knew about — '*.go' 'go.mod' 'go.sum' 'cmd/harmonik/assets' —
# and missed internal/daemon/standard-bead.dot, which internal/daemon/
# standardgraph.go pulls in with //go:embed and which defines the DOT workflow
# this script runs by DEFAULT. Editing it produced a bare-commit stamp on a
# binary containing a different workflow graph. An allowlist of build inputs has
# to be re-audited every time somebody embeds a new file type, and when it is
# wrong it is wrong SILENTLY, in the direction of calling a modified tree clean.
#
# The exclusion list is the opposite trade. It names what `init` ITSELF writes:
# `harmonik init --force` rewrites the top-level docs and re-provisions all ten
# embedded skills into .claude/skills/ (cmd/harmonik/init_cmd.go provisionSkills
# skips existing files only when --force is absent, and this script always passes
# --force), and .harmonik holds this scratch's own daemon state. Everything else
# counts. When THIS list is wrong, a clean tree gets labelled — visible, loud, and
# safe. Measured on a real scratch clone: after init and build, this returns
# nothing.
#
# .claude/skills is tracked and is NOT a build input: the binary embeds
# cmd/harmonik/assets/skills/, which this sweep still covers. The two are required
# to stay byte-identical, so today they agree and the rewrite is a no-op. The
# first time they drift, every audit of that commit would otherwise stamp
# +local-edits with no way to clear it, because init rewrites those files on every
# run.
#
# --untracked-files=all is load-bearing. `go build` compiles every .go file in a
# package directory whether or not git tracks it, so an untracked
# internal/daemon/zz_patch.go is in the binary. The second sweep adds .go files
# that .gitignore hides, which the first cannot see and Go compiles regardless.
#
# review-loop.dot is excluded because THIS HARNESS writes it, exactly as the
# entries above name what `init` writes. scripts/core-loop-seed.sh copies it to
# the scratch root so the dot cell's `dot:review-loop` label resolves, and it is
# deliberately untracked there (the comment in that script explains why a tracked
# file cannot serve: `git reset --hard HEAD` restores it mid-run). Without this
# entry the REQUIRED `make core-loop-lt` leg dirtied the very tree it pins, every
# run stamped `+local-edits`, and the assessor contract says no result from such a
# binary is an audit of that commit — so the gate could never return a usable
# result (hk-assessor-lt-gate-dirties-its-own-tree-0jz5y).
#
# This exclusion does NOT open a hole, because the file is DERIVED. Its source,
# specs/examples/review-loop.dot, is tracked and stays inside this same sweep, so
# an edit to the workflow this gate runs is still caught — it is caught at the
# source instead of at the copy. Excluding a path whose content is covered
# elsewhere is different from excluding a build input, and only the first is safe.
local_edits() {
    local scratch="$1" tracked ignored
    # NOT `2>/dev/null` on this one. Empty output from here means "no local
    # edits", so a git that could not answer — an index.lock held by the daemon
    # under test is the realistic case, since it runs its own git operations on
    # this tree — would be read as a clean tree and stamp a bare commit on code
    # nobody inspected. That is the silent false-clean this whole function exists
    # to prevent, so it fails instead.
    if ! tracked="$(git -C "$scratch" status --porcelain --untracked-files=all -- . \
            ':(exclude).harmonik' \
            ':(exclude).claude/skills' \
            ':(exclude)AGENTS.md' ':(exclude)AGENT_INDEX.md' ':(exclude)STATUS.md' \
            ':(exclude)CLAUDE.md' ':(exclude)HANDOFF.md' \
            ':(exclude)review-loop.dot' 2>&1)"; then
        die "cannot read the working tree of $scratch: ${tracked%%$'\n'*}
  An unanswered check here would read as 'no local edits' and stamp a bare commit, so it stops instead."
    fi
    # `--ignored` reports whole ignored DIRECTORIES rather than only the paths the
    # pathspec names, so this is filtered down to .go files. Without the filter
    # every run reports .harmonik/bin/ and friends as local edits. A failure here
    # is not fatal: the sweep above already covers everything git tracks or sees.
    ignored="$(git -C "$scratch" status --porcelain --untracked-files=all --ignored=matching -- '*.go' 2>/dev/null \
        | grep -E '\.go$' || true)"
    printf '%s\n%s\n' "$tracked" "$ignored" | grep -v '^[[:space:]]*$' | sort -u || true
}

# build_stamp <scratch> <revision> — what the binary built from this tree may
# honestly call itself.
#
# WHY THIS LABELS INSTEAD OF REFUSING. This script serves two callers. An
# assessor audits one commit and must never get a green verdict on other code. A
# developer edits the scratch tree and re-runs `cycle`, which both
# docs/scratch-daemon-runbook.md and docs/known-workarounds.md document as the
# inner loop. Refusing to build an edited tree would serve the first and delete
# the second.
#
# Naming serves both. A clean tree stamps the bare commit. An edited tree stamps
# a value that is NOT a commit and cannot be mistaken for one, and that label
# travels into the build output, `up`, `status`, BATCH_SUMMARY and the results
# artifact. The developer keeps the loop; the assessor cannot be handed a verdict
# that looks like a clean audit of a commit it was not.
build_stamp() {
    local scratch="$1" rev="$2"
    if [ -n "$(local_edits "$scratch")" ]; then
        printf '%s+local-edits\n' "$rev"
    else
        printf '%s\n' "$rev"
    fi
}

# isolate_push_target makes merge-gate pushes structurally local to the scratch
# project. A normal clone inherits its source as origin; without this rewrite a
# successful "isolated" run can push its landing commit into the source repo.
#
# Keep an origin rather than removing it: the merge gate requires a successful
# push. The throwaway bare repository satisfies that contract without granting
# the scratch daemon a path back to the fleet checkout.
isolate_push_target() {
    local scratch="$1" origin branch branching
    origin="$(scratch_origin "$scratch")"
    branch="scratch/main"
    branching="$scratch/.harmonik/branching.yaml"

    mkdir -p "$(dirname "$origin")"
    if [ ! -d "$origin/objects" ]; then
        git init --bare "$origin" >/dev/null
    fi
    git -C "$scratch" remote set-url origin "$origin"

    # Force both refs to HEAD rather than creating them only when absent. On an
    # `init --reuse` the tree has just been moved to a new audit revision, and a
    # scratch/main left at the PREVIOUS revision would make the daemon land its
    # work on the commit that is no longer under audit.
    git -C "$scratch" branch --force "$branch" HEAD
    git -C "$scratch" push --force origin "$branch:refs/heads/$branch" >/dev/null

    [ -f "$branching" ] || die "scratch branching config missing after init: $branching"
    awk -v branch="$branch" '
        /^[[:space:]]+start_from:/ { print "  start_from: " branch; next }
        /^[[:space:]]+lands_on:/   { print "  lands_on: " branch; next }
        { print }
    ' "$branching" > "$branching.tmp" && mv "$branching.tmp" "$branching"

    echo "[scratch-daemon] isolation: origin=$origin lands_on=$branch"
}

# session_name: derive the deterministic per-project tmux session name using the
# scratch binary itself (harmonik project-hash), so we never reimplement SHA-256
# in bash. Falls back to repo-built binary if the scratch one is absent.
session_name() {
    local scratch="$1" bin
    bin="$(scratch_bin "$scratch")"
    [ -x "$bin" ] || die "scratch binary not built — run: $0 build $scratch"
    local hash
    hash="$("$bin" project-hash --project "$scratch")" || die "project-hash failed"
    echo "harmonik-${hash}-default"
}

# read_pid: first line of the scratch pidfile (the daemon PID). Empty if absent.
read_pid() {
    local pf="$1"
    [ -f "$pf" ] || return 0
    head -n1 "$pf" 2>/dev/null | tr -d '[:space:]'
}

# prov_hash: stable 12-hex provenance key from arbitrary bytes on stdin. Used by
# `feedback` to derive a deterministic dedupe label (prov:<hash>) from
# batch-name + fail-signature, so a re-run UPDATES rather than DUPLICATES a bead.
# Prefers sha256sum; falls back to `shasum -a 256` (macOS) — both are pre-installed.
prov_hash() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum | cut -c1-12
    else
        shasum -a 256 | cut -c1-12
    fi
}

# ---------------------------------------------------------------------------
# provision_matrix_config: patch a freshly-init'd scratch config so the
# core-loop-proof matrix can boot + run the pi/codex cells (M6 WS4-3).
#
# It also applies the checked-in run posture, so the name undersells it: this is the
# one place a scratch daemon's config differs from what `harmonik init` writes.
#
# Two gaps in the `harmonik init` config (both fail-loud, no compiled default):
#   1. sentinel.liveness_no_progress_n — shipped commented; daemon.Start refuses to
#      boot without it. Insert `0` (G-liveness off) under the existing sentinel: block
#      so a throwaway matrix daemon never self-kills mid-run.
#   2. harnesses.pi — absent; a pi bead can't resolve provider/model.
# Plus the run posture that init has no opinion about: the queue-only `subsystems:`
# block, the ops-monitor interval, and the ctx-watchdog gate.
# Everything except (1) comes from scripts/scratch-config-overlay.yaml, which is the
# single tracked source. Both edits are idempotent, so re-init on an existing scratch
# is a no-op — but see (2): the overlay's guard compares CONTENT, so editing the
# overlay and re-running `init` really does re-apply.
# ---------------------------------------------------------------------------
provision_matrix_config() {
    local scratch cfg overlay repo_root
    scratch="$1"
    cfg="$scratch/.harmonik/config.yaml"
    [ -f "$cfg" ] || { echo "[scratch-daemon] provision: no config.yaml at $cfg — skipping" >&2; return 0; }
    repo_root="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
    overlay="$repo_root/scripts/scratch-config-overlay.yaml"

    # (0) target_branch ref must exist LOCALLY. daemon.target_branch defaults to `main`
    # (workloop resolveParentCommit does `git rev-parse main` to branch from / merge into).
    # A clone made from a source checkout that is NOT on `main` (e.g. a feature branch, as
    # the matrix does to run the as-built binary) has only `origin/main`, so `git rev-parse
    # main` fails exit-128 and EVERY bead reopens without dispatching. Create the local
    # branch (idempotent) from origin/main when available, else from the current HEAD.
    if ! git -C "$scratch" rev-parse --verify --quiet main >/dev/null 2>&1; then
        if git -C "$scratch" rev-parse --verify --quiet origin/main >/dev/null 2>&1; then
            git -C "$scratch" branch main origin/main >/dev/null 2>&1 || true
            echo "[scratch-daemon] provision: created local 'main' branch from origin/main"
        else
            git -C "$scratch" branch main HEAD >/dev/null 2>&1 || true
            echo "[scratch-daemon] provision: created local 'main' branch from HEAD (no origin/main)"
        fi
    fi

    # (1) liveness_no_progress_n under the existing sentinel: block.
    if grep -qE '^[[:space:]]+liveness_no_progress_n:[[:space:]]*[0-9]' "$cfg"; then
        echo "[scratch-daemon] provision: liveness_no_progress_n already set — skipping"
    else
        awk '{print} /^sentinel:/{print "  liveness_no_progress_n: 0  # WS4-3 matrix provisioning (G-liveness off; scratch-daemon.sh)"}' \
            "$cfg" > "$cfg.tmp" && mv "$cfg.tmp" "$cfg"
        echo "[scratch-daemon] provision: set sentinel.liveness_no_progress_n: 0"
    fi

    # (2) The whole ACTIVE section of the checked-in overlay: harnesses.pi,
    # codex.stale_wal_max_bytes, the queue-only `subsystems:` posture, the ops-monitor
    # interval, and the ctx-watchdog gate.
    #
    # The guard is CONTENT COMPARISON, not key detection, and that choice is the whole
    # point of this step. Twice now a key-detection guard has silently shadowed the
    # overlay: first "skip when any `harnesses:` is present", which hid the ornith pi
    # config once `harmonik init` started shipping a default one (hk-es4f7); then
    # "skip when `provider: ornith` is present", which would hide every key ADDED to
    # the overlay afterwards, because the ornith line was already there. A config block
    # that silently does not apply is worse than no block. So: read the overlay's active
    # section, read whatever this script last appended, and re-apply when they differ.
    #
    # Applying means DELETE the previously appended section, then STRIP any top-level
    # key the overlay owns (init writes its own harnesses:/codex:), then append. The
    # strip is required because a second top-level key of the same name is a
    # duplicate-key YAML error and the daemon refuses to boot.
    local overlay_marker overlay_body applied_body
    overlay_marker="# --- appended by scratch-daemon.sh provision from scratch-config-overlay.yaml ---"
    if [ ! -f "$overlay" ]; then
        echo "[scratch-daemon] provision: WARNING overlay not found ($overlay) — pi cells will not run, and the queue-only subsystems posture is NOT applied" >&2
        return 0
    fi
    overlay_body="$(sed -n '/^# ---8<--- everything below this marker/,$p' "$overlay" | sed '1d')"
    applied_body="$(awk -v m="$overlay_marker" 'found { print } $0 == m { found = 1 }' "$cfg")"
    if [ "$applied_body" = "$overlay_body" ]; then
        echo "[scratch-daemon] provision: overlay already applied and identical — skipping"
        return 0
    fi
    # Drop the previously appended section (marker line through EOF), if any.
    awk -v m="$overlay_marker" '$0 == m { stop = 1 } stop != 1 { print }' "$cfg" > "$cfg.tmp" && mv "$cfg.tmp" "$cfg"
    # Strip any top-level block the overlay owns. A block runs from its bare key line
    # through all following indented or blank lines, up to the next top-level key or EOF.
    awk '
        /^(harnesses|codex|subsystems|opsmonitor|watchdog):[[:space:]]*$/ { skip=1; next }
        skip==1 && /^[[:space:]]/         { next }
        skip==1 && /^[[:space:]]*$/       { next }
        { skip=0 }
        { print }
    ' "$cfg" > "$cfg.tmp" && mv "$cfg.tmp" "$cfg"
    {
        echo ""
        echo "$overlay_marker"
        printf '%s\n' "$overlay_body"
    } >> "$cfg"
    echo "[scratch-daemon] provision: applied overlay from $overlay (harnesses/codex + queue-only subsystems posture)"
}

# ---------------------------------------------------------------------------
# provision_toolchain: install the repo's pinned dev tools into the scratch
# clone's .tools/ BEFORE any daemon dispatches work there.
#
# THE DEFECT THIS CLOSES. The commit gate an implementer runs reaches .tools/
# for gofumpt, gci and golangci-lint. Nothing here ever installed them, so the
# first cell of a live-gate run died three minutes into dispatch with
# `.tools/gofumpt: No such file or directory` and exit 127 — a bare ENOENT that
# names no cause, arrives after the expensive part, and reads like a product
# failure rather than a missing setup step.
#
# It ran at INIT, not at build or up, for two reasons. `git clean -qfdx` above
# deletes .tools/ on every init, including `--reuse`, so any earlier point is
# undone; and init is the last moment where a failure costs seconds instead of a
# dispatched run.
#
# The Makefile is the single source of the pinned versions — this function names
# none of them. It also derives the binaries to verify from the same `tools:`
# recipe, so a tool added there is checked here without a second edit. The check
# matters more than it looks: `go install` can leave a partial .tools/ and still
# exit 0 under a warm cache, and a half-provisioned toolchain fails at exactly
# the same place as no toolchain at all.
# ---------------------------------------------------------------------------
provision_toolchain() {
    local scratch="$1" tools_dir
    tools_dir="$scratch/.tools"
    # No skip-when-absent. This runs on a clone of this repo, where the Makefile
    # is always there — so an absent one means the tree is not what the caller
    # thinks it is, and continuing would hand a daemon a clone with no gate.
    [ -f "$scratch/Makefile" ] \
        || die "toolchain: no Makefile at $scratch — this is not a harmonik checkout, and no toolchain can be installed into it"

    echo "[scratch-daemon] toolchain: installing pinned dev tools → $tools_dir"
    # A subshell cd rather than `make -C`: TOOLS_DIR in the Makefile falls back to
    # $(PWD), which `make -C` does not move.
    ( cd "$scratch" && make tools ) \
        || die "toolchain: 'make tools' failed in $scratch.
  Every dispatched implementer's commit gate reaches .tools/, so a daemon started
  now would fail each bead with a bare ENOENT minutes into the run."

    # What the recipe installs, by name. Each `go install <module path>@<version>`
    # leaves a binary named for the last path element.
    local want missing=""
    want="$(awk '
        /^tools:/     { in_recipe = 1; next }
        in_recipe && /^[^\t]/ { in_recipe = 0 }
        in_recipe && /go install/ {
            for (i = 1; i <= NF; i++) if ($i ~ /@/) {
                sub(/@.*$/, "", $i); n = split($i, parts, "/"); print parts[n]
            }
        }
    ' "$scratch/Makefile")"
    [ -n "$want" ] || die "toolchain: read no tool names out of $scratch/Makefile — the 'tools:' recipe is not in the shape this expects, so nothing was verified"

    local tool
    while IFS= read -r tool; do
        [ -n "$tool" ] || continue
        [ -x "$tools_dir/$tool" ] || missing="$missing $tool"
    done <<<"$want"
    [ -z "$missing" ] \
        || die "toolchain: 'make tools' reported success but these are absent from $tools_dir:$missing"

    echo "[scratch-daemon] toolchain: verified $(echo "$want" | tr '\n' ' ')in $tools_dir"
}

# ---------------------------------------------------------------------------
# Subcommand: init
# ---------------------------------------------------------------------------
cmd_init() {
    local scratch source_repo="" rev="" reuse=0
    scratch="$(guard_path "${1:-}")"
    shift || true

    # Back-compat: a bare second positional is the source repo. Everything after
    # it is a flag. The REVISION has no positional spelling on purpose — it must
    # be named, and a caller that forgets it gets an error, not a guess.
    case "${1:-}" in
        ""|--*) ;;
        *) source_repo="$1"; shift;;
    esac
    while [ $# -gt 0 ]; do
        case "$1" in
            --rev)      [ $# -ge 2 ] || die "init: --rev needs a value"; rev="$2"; shift 2;;
            --rev=*)    rev="${1#--rev=}"; shift;;
            --source)   [ $# -ge 2 ] || die "init: --source needs a value"; source_repo="$2"; shift 2;;
            --source=*) source_repo="${1#--source=}"; shift;;
            --reuse)    reuse=1; shift;;
            *) die "init: unknown flag '$1' (use --rev <commit-ish> [--source <repo>] [--reuse])";;
        esac
    done

    [ -n "$rev" ] || die "init: --rev <commit-ish> is REQUIRED.
  This script audits ONE commit. Without --rev it would clone whatever branch the
  source happens to point at and report that as the result for the commit you meant.
  Usage: $0 init '$scratch' --rev <commit-ish> [--source <repo>] [--reuse]"

    # Default source: the LOCAL checkout this script lives in, NOT its origin URL.
    # The origin URL serves the remote's default branch and does not necessarily
    # carry the candidate commit at all — a local branch or an unpushed commit is
    # exactly the case an audit has to handle.
    if [ -z "$source_repo" ]; then
        source_repo="$(fleet_root)"
        [ -n "$source_repo" ] || die "init: no --source given and this script is not inside a git repo"
    fi

    # Resolve the revision in the SOURCE first, when the source is a local repo.
    # A typo must fail before anything is cloned, and the source is the authority
    # on what the caller meant.
    local want=""
    if [ -d "$source_repo" ]; then
        want="$(resolve_rev "$source_repo" "$rev")" \
            || die "init: --rev '$rev' does not name a commit in source repo '$source_repo'"
    fi

    # An existing tree is the second half of the wrong-tree defect: the old code
    # skipped the clone and audited whatever the previous run left. Refuse it.
    # --reuse keeps the tree on purpose, and the tree is still forced to --rev
    # below, so reuse saves the clone without ever changing what gets graded.
    if [ -d "$scratch/.git" ]; then
        if [ "$reuse" -eq 0 ]; then
            die "init: '$scratch' already holds a git tree (HEAD $(git -C "$scratch" rev-parse HEAD 2>/dev/null || echo unknown)).
  Reusing it silently would audit whatever the previous run left there.
  Start clean:     rm -rf '$scratch' && $0 init '$scratch' --rev '$rev'
  Or reuse it:     $0 init '$scratch' --rev '$rev' --reuse   (still forced to '$rev')"
        fi
        echo "[scratch-daemon] --reuse: keeping the tree at $scratch — forcing it to '$rev'"
    else
        echo "[scratch-daemon] cloning $source_repo → $scratch"
        git clone "$source_repo" "$scratch"
    fi

    # Move the tree to the named revision. Nothing above this point guarantees the
    # tree holds the code under audit: a clone takes the source's default branch,
    # and --reuse keeps the previous run's checkout.
    #
    # The fetch is allowed to fail. A fresh clone already carries every branch, and
    # a reused tree usually does too, so a failure here is only fatal if the
    # revision then does not resolve — which the next block checks and dies on. The
    # verdict comes from reading HEAD back, never from this command's status.
    local fetch_err=""
    if ! fetch_err="$(git -C "$scratch" fetch --quiet --tags --force "$source_repo" \
            '+refs/heads/*:refs/remotes/scratch-source/*' 2>&1)"; then
        fetch_err=" (fetch from '$source_repo' also failed: ${fetch_err%%$'\n'*})"
    else
        fetch_err=""
    fi

    if [ -z "$want" ]; then
        # A non-local source (a URL) could not be pre-resolved, so the scratch tree
        # is the only place left to resolve in. That is safe ONLY if the fetch
        # succeeded. If it did not, every ref here is whatever a previous run left,
        # and resolving against them is exactly how a stale tree gets audited under
        # a fresh commit's name. Fail instead of resolving on stale data.
        [ -z "$fetch_err" ] \
            || die "init: cannot reach source '$source_repo', so '$rev' can only be resolved against refs a previous run left in '$scratch'.${fetch_err}"
        want="$(resolve_rev "$scratch" "$rev")" \
            || die "init: --rev '$rev' does not name a commit in '$scratch'${fetch_err}"
    else
        git -C "$scratch" rev-parse --verify --quiet "${want}^{commit}" >/dev/null \
            || die "init: commit $want ('$rev') is not present in '$scratch'${fetch_err}"
    fi

    echo "[scratch-daemon] checking out $rev → $want"
    git -C "$scratch" checkout --quiet --detach --force "$want" \
        || die "init: cannot check out $want ('$rev') in $scratch"
    git -C "$scratch" reset --quiet --hard "$want"
    # An untracked leftover from a previous run is still the previous run's code.
    # .harmonik is this scratch's own daemon state (binary, socket, pidfile,
    # config) rather than part of the revision, so it is the one thing kept.
    git -C "$scratch" clean -qfdx -e .harmonik

    # VERIFY. Every step above can fail in a way that leaves the tree on the wrong
    # commit, and a wrong tree reporting green is the whole failure this guards
    # against. Read HEAD back and compare rather than trusting the commands.
    local head
    head="$(git -C "$scratch" rev-parse HEAD)"
    [ "$head" = "$want" ] \
        || die "init: checkout did not take — asked for '$rev' ($want), HEAD is $head"

    # Drop any binary a previous run built. The clean step above deliberately
    # spares .harmonik, and the binary lives at .harmonik/bin/harmonik, so without
    # this an `init --reuse` to a NEW revision leaves the OLD revision's executable
    # in place. `up` only checks that the file exists, so the daemon would run the
    # previous commit's code while every line of output named the new one — the
    # wrong-tree defect again, wearing the fix's own label. Removing it makes `up`
    # fail with "scratch binary not built" until someone rebuilds.
    rm -f "$(scratch_bin "$scratch")" "$(scratch_binrevfile "$scratch")"

    mkdir -p "$scratch/.harmonik"
    printf '%s\t%s\n' "$want" "$rev" >"$(scratch_revfile "$scratch")"
    echo "[scratch-daemon] AUDIT REVISION: $want ($rev)"

    # Bootstrap harmonik state only if absent — a clone of a harmonik-managed repo
    # already carries .harmonik/config.yaml, so this is a no-op there.
    if [ ! -f "$scratch/.harmonik/config.yaml" ]; then
        echo "[scratch-daemon] no .harmonik/config.yaml — running harmonik init"
        cmd_build "$scratch"
        # --project is REQUIRED: `harmonik init` reads the project ONLY from
        # --project and otherwise falls back to os.Getwd() (cmd/harmonik/init_cmd.go),
        # which would re-init the CWD repo's beads DB. A positional path is ignored.
        "$(scratch_bin "$scratch")" init --project "$scratch" --force --no-supervise
    else
        echo "[scratch-daemon] .harmonik/config.yaml present — no init needed"
    fi
    isolate_push_target "$scratch"
    provision_matrix_config "$scratch"
    provision_toolchain "$scratch"
    # isolate_push_target and the harmonik bootstrap both touch the tree. Confirm
    # the pin one more time so init cannot report success on a moved tree.
    assert_pinned "$scratch" >/dev/null
    echo "[scratch-daemon] init complete at $want ($rev). Next: $0 build $scratch && $0 up $scratch"
}

# ---------------------------------------------------------------------------
# Subcommand: build
# ---------------------------------------------------------------------------
cmd_build() {
    local scratch bin
    scratch="$(guard_path "${1:-}")"
    [ -d "$scratch/cmd/harmonik" ] || die "$scratch is not a harmonik checkout (no cmd/harmonik) — run init first"
    bin="$(scratch_bin "$scratch")"
    mkdir -p "$(dirname "$bin")"
    # The commit comes from the PIN, not from whatever HEAD happens to be. The old
    # code read HEAD and fell back to the literal string "unknown", so a tree
    # nobody had placed on purpose still produced a build that looked stamped and
    # deliberate. assert_pinned refuses instead, and there is no fallback value.
    local commit_hash stamp
    commit_hash="$(assert_pinned "$scratch")"
    # What this binary may honestly be called. Equal to commit_hash for a clean
    # tree; suffixed when the tree differs from the commit it is pinned to.
    stamp="$(build_stamp "$scratch" "$commit_hash")"
    if [ "$stamp" != "$commit_hash" ]; then
        # Say what was actually measured. This line used to read "local edits to
        # code that Go compiles", which described an allowlist of build inputs
        # that local_edits deliberately does NOT use — it sweeps everything git
        # sees, minus what this harness itself writes. A reader who took the old
        # wording literally went looking for a Go change that was not there.
        echo "[scratch-daemon] WARNING: this tree differs from the commit it is pinned to. Any file below can change what the binary does or how it behaves:" >&2
        local_edits "$scratch" >&2
        echo "[scratch-daemon] WARNING: the binary will be labelled '$stamp'. It is NOT $commit_hash, and no result from it is an audit of that commit." >&2
    fi
    echo "[scratch-daemon] building scratch binary → $bin (revision $stamp)"
    # Build FROM the scratch clone's source so the daemon runs exactly the code in
    # that checkout. Same ldflags stamp as the Makefile's build-harmonik target.
    go build -C "$scratch" -ldflags "-X main.commitHash=${stamp}" -o "$bin" ./cmd/harmonik
    # Record what this binary is, so `up` and `batch` can refuse or disclose. Written
    # only after a successful build: a failed build must not leave a stamp claiming
    # the binary is current.
    printf '%s\n' "$stamp" >"$(scratch_binrevfile "$scratch")"
    echo "[scratch-daemon] build OK at revision $stamp"
}

# ---------------------------------------------------------------------------
# Subcommand: up
# ---------------------------------------------------------------------------
cmd_up() {
    local scratch bin sess log sock rev
    scratch="$(guard_path "${1:-}")"
    assert_not_supervised "$scratch"
    # The daemon about to start runs the code in this tree. Refuse to start one
    # nobody can name the revision of.
    rev="$(assert_pinned "$scratch")"
    bin="$(scratch_bin "$scratch")"
    [ -x "$bin" ] || die "scratch binary not built — run: $0 build $scratch"
    # The tree being pinned says nothing about the BINARY. Confirm the executable
    # about to run was built from the revision under audit; otherwise the daemon
    # runs one commit while every line of output names another.
    local binrev
    binrev="$(cat "$(scratch_binrevfile "$scratch")" 2>/dev/null || true)"
    if [ -z "$binrev" ]; then
        die "the scratch binary carries no build revision — it was not built by '$0 build'. Run: $0 build $scratch"
    fi
    # The binary may be the bare commit, or that commit plus the local-edits
    # label. Anything else was built from a DIFFERENT commit and must not run
    # under this pin's name.
    if [ "$binrev" != "$rev" ] && [ "$binrev" != "${rev}+local-edits" ]; then
        die "the scratch binary was built from a different revision than the tree holds.
  tree  : $rev
  binary: $binrev
  Rebuild before starting the daemon: $0 build $scratch"
    fi
    if [ "$binrev" != "$rev" ]; then
        echo "[scratch-daemon] WARNING: starting a daemon built from $binrev — this is not a clean $rev, and no result from it is an audit of that commit." >&2
    fi
    sess="$(session_name "$scratch")"
    log="$(scratch_log "$scratch")"
    sock="$(scratch_sock "$scratch")"

    if tmux has-session -t "$sess" 2>/dev/null; then
        die "tmux session '$sess' already exists — run '$0 down $scratch' first (or it is already up)"
    fi

    local max_concurrent="${SCRATCH_MAX_CONCURRENT:-1}"
    local workflow_mode="${SCRATCH_WORKFLOW_MODE:-dot}"
    local extra_flags="${SCRATCH_DAEMON_FLAGS:-}"

    # Report the BINARY's label, not the tree's pin: the binary is what runs.
    echo "[scratch-daemon] starting standalone daemon (session=$sess, project=$scratch, revision=$binrev)"
    # Standalone start = the bare `harmonik --project <path>` binary run INSIDE a
    # tmux session. This script starts no `harmonik supervise` process. That alone
    # does NOT give you a supervisor-free daemon: the daemon carries its own
    # supervisor watchdog. The watchdog probes .harmonik/cognition/supervisor.pid
    # every 60 s, and a scratch clone has never had a supervisor, so the FIRST
    # tick after boot runs `harmonik supervise restart --watch-restart` for this
    # project (up to 3 attempts). That supervisor then revives the daemon, so a
    # plain `down`/pkill can come back about a minute later.
    #
    # To hold a scratch daemon down, switch the watchdog off in the scratch
    # clone's .harmonik/config.yaml before `up`:
    #     subsystems:
    #       supervisor_watchdog:
    #         enabled: false
    # With that set the daemon builds no watchdog at all, so `down` stays down
    # and a shutdown drain can be watched to the end.
    #
    # API keys are stripped so the
    # run bills the subscription pool (codename:credfence), matching smoke-scratch.
    # -c "$scratch": the daemon MUST run with CWD == its ProjectDir. Guards that
    # read config via os.Getwd() (e.g. the codex stale-WAL guard) assume this
    # invariant; without -c the daemon inherits the CALLER's cwd (the driver
    # checkout) and reads the wrong .harmonik/config.yaml.
    # M6 WS4-3: default eager-refill OFF for scratch daemons so `queue submit` is
    # the sole deterministic dispatcher (the daemon must not auto-dispatch ready
    # seed beads at boot before a cell's subscribe arms). Override with
    # SCRATCH_DISABLE_EAGER_REFILL=0 for scratch runs that want the flywheel.
    local disable_eager="${SCRATCH_DISABLE_EAGER_REFILL:-1}"
    # Composition-root wiring audit ON by default for scratch daemons. It prints one
    # row per boot singleton with constructed / ABSENT, derived by reflection from the
    # live bootState (internal/daemon/wiringlog.go). That table is the BOOT RECORD for
    # the queue-only subsystems posture: it is how an assessor confirms that a
    # subsystem switched off in config really was never constructed, rather than
    # trusting the config. It is a stderr diagnostic only. Set SCRATCH_DEBUG_WIRING=0
    # to silence it.
    local debug_wiring="${SCRATCH_DEBUG_WIRING:-1}"
    # shellcheck disable=SC2016
    tmux new-session -d -s "$sess" -c "$scratch" \
        "env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN \
          HARMONIK_DISABLE_EAGER_REFILL='$disable_eager' \
          HARMONIK_DEBUG_WIRING='$debug_wiring' \
          '$bin' --project '$scratch' \
          --max-concurrent $max_concurrent \
          --workflow-mode $workflow_mode \
          $extra_flags \
          2>&1 | tee '$log'"

    echo "[scratch-daemon] waiting for daemon socket ($sock)..."
    local i
    for i in $(seq 1 45); do
        if [ -S "$sock" ]; then
            echo "[scratch-daemon] daemon ready (${i}s) — session=$sess log=$log"
            return 0
        fi
        sleep 1
    done
    echo "[scratch-daemon] WARNING: socket not ready after 45s — last log lines:" >&2
    tail -20 "$log" 2>/dev/null || true
    die "daemon did not come up; inspect $log"
}

# ---------------------------------------------------------------------------
# Subcommand: status
# ---------------------------------------------------------------------------
cmd_status() {
    local scratch sess pf sock pid
    scratch="$(guard_path "${1:-}")"
    pf="$(scratch_pidfile "$scratch")"
    sock="$(scratch_sock "$scratch")"
    pid="$(read_pid "$pf")"

    echo "[scratch-daemon] project : $scratch"
    # status is a read-only reporter, so it describes the pin instead of dying on
    # a bad one. It still has to say plainly when the tree cannot be named, and
    # when it has moved off the revision init placed it on.
    local want head
    want="$(audit_revision "$scratch")"
    head="$(git -C "$scratch" rev-parse HEAD 2>/dev/null || true)"
    if [ -z "$want" ]; then
        echo "[scratch-daemon] revision: NOT PINNED — no audit revision recorded; results from this tree name no commit"
    elif [ "$want" != "$head" ]; then
        echo "[scratch-daemon] revision: DRIFTED — init pinned $want but HEAD is ${head:-<not a git tree>}"
    elif [ -n "$(local_edits "$scratch")" ]; then
        echo "[scratch-daemon] revision: $want  (pinned, HEAD matches) — MODIFIED: the tree carries local edits to compiled code"
        local_edits "$scratch" | sed 's/^/[scratch-daemon]   /'
    else
        echo "[scratch-daemon] revision: $want  (pinned, HEAD matches, no local edits)"
    fi
    if [ -f "$(scratch_binrevfile "$scratch")" ]; then
        echo "[scratch-daemon] binary  : built from $(cat "$(scratch_binrevfile "$scratch")")"
    else
        echo "[scratch-daemon] binary  : not built by '$0 build'"
    fi
    # session_name needs the binary; degrade gracefully if it is missing.
    if [ -x "$(scratch_bin "$scratch")" ]; then
        sess="$(session_name "$scratch")"
        echo "[scratch-daemon] tmux    : $sess$(tmux has-session -t "$sess" 2>/dev/null && echo '  (alive)' || echo '  (absent)')"
    else
        echo "[scratch-daemon] tmux    : (binary not built; cannot derive session name)"
    fi
    echo "[scratch-daemon] socket  : $sock$( [ -S "$sock" ] && echo '  (present)' || echo '  (absent)')"
    if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
        echo "[scratch-daemon] daemon  : RUNNING (pid $pid)"
    elif [ -n "$pid" ]; then
        echo "[scratch-daemon] daemon  : pidfile names $pid but process is gone (stale)"
    else
        echo "[scratch-daemon] daemon  : not running (no pidfile)"
    fi
    if [ -f "$(scratch_log "$scratch")" ]; then
        echo "[scratch-daemon] --- last 10 log lines ---"
        tail -10 "$(scratch_log "$scratch")"
    fi
}

# ---------------------------------------------------------------------------
# Subcommand: down  (SAFE — kills ONLY the scratch daemon)
# ---------------------------------------------------------------------------
cmd_down() {
    local scratch pf sock pid confirmed_scratch=0
    scratch="$(guard_path "${1:-}")"
    assert_not_supervised "$scratch"
    pf="$(scratch_pidfile "$scratch")"
    sock="$(scratch_sock "$scratch")"
    pid="$(read_pid "$pf")"

    if [ -z "$pid" ]; then
        echo "[scratch-daemon] no pidfile at $pf — daemon not running via this script"
    elif ! [[ "$pid" =~ ^[0-9]+$ ]]; then
        die "pidfile $pf has a non-numeric first line ('$pid') — refusing to kill"
    elif ! kill -0 "$pid" 2>/dev/null; then
        echo "[scratch-daemon] pidfile names $pid but process is already gone — cleaning up"
    else
        # Belt-and-suspenders: confirm the live process really is THIS scratch
        # daemon (its argv must contain the scratch path) before sending a signal.
        # This makes it impossible to kill the fleet daemon even if a pidfile were
        # somehow corrupted or recycled to an unrelated PID.
        local cmdline
        cmdline="$(ps -p "$pid" -o command= 2>/dev/null || true)"
        case "$cmdline" in
            *"$scratch"*)
                # Argv contains the scratch path → provably the scratch daemon.
                confirmed_scratch=1
                echo "[scratch-daemon] stopping scratch daemon pid $pid"
                kill -TERM "$pid" 2>/dev/null || true
                local i
                for i in $(seq 1 10); do
                    kill -0 "$pid" 2>/dev/null || break
                    sleep 1
                done
                if kill -0 "$pid" 2>/dev/null; then
                    echo "[scratch-daemon] still alive after 10s — SIGKILL pid $pid"
                    kill -KILL "$pid" 2>/dev/null || true
                fi
                ;;
            *)
                die "pid $pid does NOT look like the scratch daemon (argv lacks '$scratch') — refusing to kill. cmdline: $cmdline"
                ;;
        esac
    fi

    # Tear down the tmux session ONLY when we provably confirmed (via the live
    # process's argv above) that this is the scratch daemon. The session name is a
    # per-project hash, and harmonik freezes a `harmonik-<hash>-default` session as
    # the SINGLE spawn target for a project — killing one we did NOT confirm could,
    # in the stale-pidfile + symlinked-to-fleet tail case, take down the FLEET's
    # spawn-target session. So this is gated on the SAME ownership proof as the kill.
    if [ "$confirmed_scratch" = "1" ] && [ -x "$(scratch_bin "$scratch")" ]; then
        local sess
        sess="$(session_name "$scratch")"
        tmux kill-session -t "$sess" 2>/dev/null && echo "[scratch-daemon] killed tmux session $sess" || true
    elif [ -x "$(scratch_bin "$scratch")" ]; then
        local sess
        sess="$(session_name "$scratch")"
        if tmux has-session -t "$sess" 2>/dev/null; then
            echo "[scratch-daemon] NOTE: tmux session $sess exists but scratch-ownership was NOT confirmed (no live argv match) — leaving it untouched. Verify and 'tmux kill-session -t $sess' by hand if you are sure it is the scratch one." >&2
        fi
    fi

    # Remove a stale socket only after the process is confirmed gone.
    if [ -S "$sock" ] && { [ -z "$pid" ] || ! kill -0 "$pid" 2>/dev/null; }; then
        rm -f "$sock" && echo "[scratch-daemon] removed stale socket $sock"
    fi
    echo "[scratch-daemon] down complete"
}

# ---------------------------------------------------------------------------
# Subcommand: cycle  (the fast pkill+rebuild loop)
# ---------------------------------------------------------------------------
cmd_cycle() {
    local scratch
    scratch="$(guard_path "${1:-}")"
    echo "[scratch-daemon] cycle: down → build → up"
    cmd_down "$scratch"
    cmd_build "$scratch"
    cmd_up "$scratch"
}

# ---------------------------------------------------------------------------
# Subcommand: batch  (submit a NAMED batch + collect a structured pass/fail summary)
# ---------------------------------------------------------------------------
# Submits a named batch of beads to the SCRATCH daemon's queue, waits for every item
# to reach a terminal transition, then emits a structured pass/fail summary so a later
# step (hk-1gkc8) can turn failures into feedback beads.
#
# Targets ONLY the scratch socket — `queue submit --project <scratch>` resolves the
# socket to <scratch>/.harmonik/daemon.sock, and `subscribe --socket <scratch-sock>`
# reads only that socket. The fleet daemon is never addressed. guard_path (scratch≠
# fleet) and assert_not_supervised run exactly as the other commands' do.
#
# Arg surface (the <name> is the named queue AND the summary label — never hardcoded):
#   batch <scratch-path> <name> --beads hk-a,hk-b,...   submit those bead IDs
#   batch <scratch-path> <name> --file  <queue.json>    submit a queue-submit JSON doc
#   batch <scratch-path> <name> --from-events <ndjson>  OFFLINE: fold a captured event
#       stream into the SAME results artifact + BATCH_SUMMARY, with NO live daemon /
#       subscribe / submit. Test seam (hk-6eqv9) so the end-to-end smoke can exercise
#       the REAL fold + fail_signature derivation hermetically (a live batch needs a
#       claude binary spawning agents — slow + non-deterministic). Expected bead set is
#       taken from --beads if given, else derived from the stream's run_started events.
# Exactly one of --beads / --file is required (or --from-events). The in-file `queue` field is ignored by
# `queue submit`, so we always pass --queue <name> explicitly (named-queue route).
#
# Output contract (stable + parseable — documented for the reviewer and hk-1gkc8):
#   - A JSON artifact at <scratch>/.harmonik/batch-<name>-<queue_id>.json: an array of
#       { "bead", "run_id"|null, "verdict": pass|fail|incomplete, "fail_signature"|null,
#         "revision" }
#     where fail_signature is a one-line (<=200ch) excerpt of the run's failure summary,
#     and revision is the audited commit the result came from (see AUDIT REVISION above).
#     This artifact is the authoritative machine input for the feedback-bead step.
#   - A RETAINED event capture at <scratch>/.harmonik/batch-<name>-<queue_id>.events.ndjson:
#     the raw NDJSON subscribe stream the fold above was computed from. It is kept
#     after the run, not deleted, because the audit step needs it to judge event
#     ORDERING once the batch is over — a verdict with no evidence behind it cannot
#     be checked. The capture is armed BEFORE the submit that mints the queue_id, so
#     it is written as batch-<name>.events.ndjson and renamed at the end. An
#     interrupted run leaves its evidence under the un-renamed name, but only until
#     the next batch of the same <name> truncates that path when it arms its reader.
#     Completed runs carry the queue_id and never collide. Nothing prunes these.
#   - Stable stdout lines (grep-able), one BATCH_ITEM per item, tab-separated:
#       BATCH_SUBMIT  name=<name> queue_id=<id> items=<n>
#       BATCH_ITEM\t<bead>\t<verdict>\t<run_id|->\t<fail_signature|->
#       BATCH_SUMMARY name=<name> total=<n> pass=<p> fail=<f> incomplete=<i> results=<path> events=<path> revision=<sha>
#   - 'incomplete' = no terminal event before SCRATCH_BATCH_TIMEOUT elapsed.
#
# Exit: 0 if every item passed; 1 if any item failed or stayed incomplete.
# SCRATCH_BATCH_EVENT_TYPES is what the batch capture subscribes to.
#
# The first three drive the pass/fail/incomplete fold below and are the minimum
# the batch needs to reach a verdict. The rest are for the audit step, which
# judges ORDERING and cannot do so from run terminals alone (hk-ze9mz):
#
#   run_started/run_completed/run_failed  the fold's own inputs
#   bead_closed                           did the work item actually close, and after what
#   outcome_emitted                       the outcome the daemon recorded for the run
#   bead_ledger_recovered                 a bead the reconciler had to repair. Only fires
#                                         after a failed bead sync is retried, so it is
#                                         quiet on a healthy daemon — but it CAN fire.
#
# Two recovery events are deliberately NOT here. Both would add a filter clause
# that can never match, which is worse than no clause at all: it implies evidence
# nobody can produce, in a capture whose whole point is evidence.
#
#   bead_terminal_transition_recovered  internal/core/eventtype.go marks it
#     deferred with no emitter. Nothing writes it. Add it when something does.
#   queue_item_reconciled  it HAS an emitter, and the emitter still cannot reach
#     us. It fires from reconcileDispatchedItems inside loadStartupState, which
#     runs before bindSocket, and a subscribe stream is live-only unless it is
#     given --since-event-id. So the event is always already in the past by the
#     time any subscriber exists. Capturing it would need a cursor-seeded read
#     of events.jsonl, not a wider live filter.
readonly SCRATCH_BATCH_EVENT_TYPES="run_started,run_completed,run_failed,bead_closed,outcome_emitted,bead_ledger_recovered"

cmd_batch() {
    local scratch name mode="" beads_csv="" file="" events_file="" timeout
    scratch="$(guard_path "${1:-}")"
    shift || true
    name="${1:-}"
    [ -n "$name" ] || die "batch: <name> is required (the named queue / summary label)"
    case "$name" in --*) die "batch: <name> must come before flags, got '$name'";; esac
    # <name> becomes both a --queue value and a path segment of the results file, so
    # restrict it to a safe charset (rejects '/', '..', etc. — no writes outside .harmonik).
    case "$name" in *[!A-Za-z0-9._-]*) die "batch: <name> must match ^[A-Za-z0-9._-]+\$ (got '$name')";; esac
    shift || true

    # Parse the input-mode flags. Exactly one of --beads / --file is accepted.
    while [ $# -gt 0 ]; do
        case "$1" in
            --beads)   [ $# -ge 2 ] || die "batch: --beads needs a value"; beads_csv="$2"; mode="beads"; shift 2;;
            --beads=*) beads_csv="${1#--beads=}"; mode="beads"; shift;;
            --file)    [ $# -ge 2 ] || die "batch: --file needs a value";  file="$2";       mode="file";  shift 2;;
            --file=*)  file="${1#--file=}";       mode="file";  shift;;
            --from-events)   [ $# -ge 2 ] || die "batch: --from-events needs a value"; events_file="$2"; shift 2;;
            --from-events=*) events_file="${1#--from-events=}"; shift;;
            *) die "batch: unknown flag '$1' (use --beads id,id,... | --file <queue.json> | --from-events <ndjson>)";;
        esac
    done
    # --from-events is the OFFLINE fold mode; it wins over --beads (which, if also given,
    # only supplies the expected bead set). One of the three input modes is required.
    if [ -n "$events_file" ]; then
        mode="events"
        [ -f "$events_file" ] || die "batch: --from-events file '$events_file' not found"
    fi
    [ -n "$mode" ] || die "batch: one of --beads <ids>, --file <queue.json>, or --from-events <ndjson> is required"

    # Which revision this batch is a verdict ON.
    #
    # The LIVE modes run the scratch tree's code through a real daemon, so the
    # tree must be pinned and the result carries that commit. The OFFLINE
    # --from-events mode re-folds an event stream that was captured earlier; it
    # reads no source tree, so it cannot claim one. It reports the pin when the
    # directory happens to carry one and says so plainly when it does not, rather
    # than leaving the field blank and letting a reader assume a commit.
    local batch_rev
    if [ "$mode" = "events" ]; then
        # Prefer the BINARY's label over the tree's pin, exactly as the live path
        # does. The events were produced by a daemon, and the binary is the only
        # record of what that daemon was. Reading the pin alone launders the
        # local-edits label: edit, cycle, batch (correctly stamped
        # <sha>+local-edits), then re-fold the SAME retained capture and the rows
        # would say a bare <sha>, which `feedback` then writes into a fleet bead.
        batch_rev="$(cat "$(scratch_binrevfile "$scratch")" 2>/dev/null || true)"
        [ -n "$batch_rev" ] || batch_rev="$(audit_revision "$scratch")"
        if [ -z "$batch_rev" ]; then
            batch_rev="none-offline-events-fold"
        elif [ "${batch_rev%%+*}" != "$(git -C "$scratch" rev-parse HEAD 2>/dev/null)" ]; then
            # The events came from whatever ran earlier, and the tree has moved
            # since. The pin still names the right commit for THIS event file only
            # if the file was captured before the move, which cannot be checked
            # from here. Say so rather than let the stamp look verified.
            echo "[scratch-daemon] WARNING: the scratch tree has moved off $batch_rev; this fold stamps a revision it cannot verify against the event file" >&2
        fi
    else
        batch_rev="$(assert_pinned "$scratch")"
        # A live batch is a verdict on the code the DAEMON ran, and the daemon ran
        # the binary. If that binary carries the local-edits label, the verdict
        # must carry it too, or a modified run reads as a clean audit of the pin.
        local live_binrev
        live_binrev="$(cat "$(scratch_binrevfile "$scratch")" 2>/dev/null || true)"
        if [ -n "$live_binrev" ] && [ "$live_binrev" != "$batch_rev" ]; then
            # Warn BEFORE reassigning, so the message can name both the binary and
            # the pinned commit it differs from.
            echo "[scratch-daemon] WARNING: this batch runs a binary labelled '$live_binrev', not a clean build of $batch_rev. No result below is an audit of that commit." >&2
            batch_rev="$live_binrev"
        fi
    fi
    echo "[scratch-daemon] batch '$name' — revision: $batch_rev"

    timeout="${SCRATCH_BATCH_TIMEOUT:-1800}"
    command -v jq >/dev/null 2>&1 || die "batch: jq is required to parse the event stream"

    # Fleet-safety: same guard the kill/stop paths use, even on the already-up path.
    # The live (daemon) path needs the scratch binary + the not-supervised guard; the
    # OFFLINE --from-events path touches no daemon, so it skips both.
    local bin sock
    if [ "$mode" != "events" ]; then
        assert_not_supervised "$scratch"
        bin="$(scratch_bin "$scratch")"
        [ -x "$bin" ] || die "scratch binary not built — run: $0 build $scratch"
        sock="$(scratch_sock "$scratch")"

        # 1) Ensure the scratch daemon is up — reuse cmd_up; never duplicate up-logic.
        if [ -S "$sock" ]; then
            echo "[scratch-daemon] batch: scratch daemon already up (socket $sock)"
        else
            echo "[scratch-daemon] batch: scratch daemon not up — bringing it up"
            cmd_up "$scratch"
        fi
    fi

    # 2) Compute the expected bead set (drives the wait + the summary rows).
    local expected_json item_count
    if [ "$mode" = "beads" ]; then
        expected_json="$(printf '%s' "$beads_csv" | jq -R -c 'split(",") | map(gsub("^\\s+|\\s+$";"")) | map(select(length>0))')"
    elif [ "$mode" = "events" ]; then
        if [ -n "$beads_csv" ]; then
            expected_json="$(printf '%s' "$beads_csv" | jq -R -c 'split(",") | map(gsub("^\\s+|\\s+$";"")) | map(select(length>0))')"
        else
            # Derive the expected set from the captured stream's run_started events.
            expected_json="$(jq -s -c '[.[] | select(.type=="run_started") | (.payload.bead_id // .bead_id // "?")] | unique' "$events_file")" \
                || die "batch: cannot derive bead IDs from --from-events '$events_file'"
        fi
    else
        [ -f "$file" ] || die "batch: queue file '$file' not found"
        expected_json="$(jq -c '[.groups[].items[].bead_id]' "$file")" || die "batch: cannot read bead IDs from '$file'"
    fi
    item_count="$(printf '%s' "$expected_json" | jq 'length')"
    [ "$item_count" -gt 0 ] || die "batch: no bead IDs resolved from the $mode input"

    # 3+4) Acquire the event stream + announce the batch.
    #   live  : arm a subscribe reader BEFORE submitting (no missed-event race), then
    #           submit the named batch to the SCRATCH queue (always --queue: the in-file
    #           queue field is ignored, so a named route requires the explicit flag).
    #   events: the stream is already captured on disk — fold it directly, no daemon.
    local raw sub_pid="" queue_id
    if [ "$mode" = "events" ]; then
        raw="$events_file"
        queue_id="events"
        echo "[scratch-daemon] batch: OFFLINE fold of captured events ($raw) — no live daemon/subscribe/submit (hk-6eqv9 test seam)"
        echo "BATCH_SUBMIT name=$name queue_id=$queue_id items=$item_count"
    else
        # The capture is RETAINED, not a temp file (hk-ze9mz). It used to be a
        # mktemp file removed on EXIT/INT/TERM, so no evidence survived a batch
        # and the audit step had nothing to read. It is named from <name> alone
        # because the reader MUST be armed before the submit that mints the
        # queue_id; step 7 renames it to carry the queue_id once that is known.
        raw="$scratch/.harmonik/batch-${name}.events.ndjson"
        : >"$raw"
        "$bin" subscribe --socket "$sock" \
            --types "$SCRATCH_BATCH_EVENT_TYPES" --heartbeat 30s \
            >"$raw" 2>>"$(scratch_log "$scratch")" &
        sub_pid=$!
        # Tear down the background reader on any exit; KEEP the results file and the
        # event capture.
        # sub_pid is a function-local but the EXIT trap fires at SCRIPT exit, by which
        # point it is out of scope — under `set -u` a bare "$sub_pid" then aborts the
        # trap with "unbound variable" (leaking the subscribe child). Guard it with :-
        # so the cleanup always runs.
        # EXIT alone is not enough: a non-interactive bash does NOT run the EXIT trap on an
        # untrapped SIGINT or SIGTERM, so a Ctrl-C during a batch leaked the child anyway.
        trap 'kill "${sub_pid:-}" 2>/dev/null || true' EXIT INT TERM

        echo "[scratch-daemon] batch: submitting $item_count item(s) to queue '$name' (project=$scratch)"
        local submit_out
        if [ "$mode" = "beads" ]; then
            submit_out="$("$bin" queue submit --project "$scratch" --queue "$name" --beads "$beads_csv" --json)" \
                || die "batch: queue submit failed (--beads)"
        else
            submit_out="$("$bin" queue submit --project "$scratch" --queue "$name" "$file" --json)" \
                || die "batch: queue submit failed (--file)"
        fi
        queue_id="$(printf '%s' "$submit_out" | jq -r '.queue_id // empty')"
        [ -n "$queue_id" ] || queue_id="noqid"
        echo "BATCH_SUBMIT name=$name queue_id=$queue_id items=$item_count"
    fi

    # jq program: fold the NDJSON event stream into one result row per expected bead.
    # Builds run_id→bead_id from run_started, takes the LAST terminal per bead (review-
    # loop may retry), and marks any bead with no terminal as 'incomplete'.
    local jq_filter
    jq_filter='
# oneline: collapse a run summary into the <=200ch fail_signature used as the
# cross-run dedup key (feedback derives prov:<hash> = sha256(batch 0x1f signature)).
# fail_signature STABILITY (hk-6eqv9): the SAME logical failure MUST yield the SAME
# signature run-to-run (so feedback UPDATES, not duplicates), while DIFFERENT logical
# failures MUST stay distinct (so two real bugs become two beads, never one). We redact
# only VOLATILE tokens and, for filesystem paths, only the volatile PREFIX — the
# identity-bearing tail (package/file/test name) is preserved, so e.g.
# ".../foo.go missing return" and ".../bar.go missing return" do NOT false-merge.
# Redacted classes: ISO-8601 + epoch timestamps, UUIDs, hex addresses (0x…), Go panic
# "goroutine N", worktree-agent run-ids, bare run-/wt-ids, pid/port numbers, duration
# literals (Ns/N.Ns/Nms), git SHAs (7–40 lowercase hex), and tmpdir roots (/tmp,
# /var/folders/…/T) collapsed to <TMP> while keeping the path tail. Order matters: the
# tmpdir-root rule runs LAST so earlier rules see the full path; SHA runs after UUID/0x
# so it never eats a UUID/address. Single shared def — used by BOTH the live subscribe
# fold and the offline --from-events fold.
def oneline:
  (. // "")
  | gsub("[\r\n\t]+"; " ")
  | gsub("(?<ts>[0-9]{4}-[0-9]{2}-[0-9]{2}[T ][0-9]{2}:[0-9]{2}:[0-9]{2}(\\.[0-9]+)?(Z|[+-][0-9]{2}:?[0-9]{2})?)"; "<TS>")  # ISO-8601 timestamps
  | gsub("(?<uuid>[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})"; "<UUID>")                    # UUIDs (incl. UUID-form run-ids)
  | gsub("0x[0-9a-fA-F]+"; "0x<ADDR>")                                                                                        # hex pointers/addresses (panic dumps)
  | gsub("goroutine [0-9]+"; "goroutine <N>")                                                                                 # Go panic goroutine ids
  | gsub("worktree-agent-[0-9A-Za-z._-]+"; "worktree-agent-<ID>")                                                             # run-id embedded in a worktree path segment
  | gsub("\\b(run|wt)[-_][0-9A-Za-z]{6,}\\b"; "<RUNID>")                                                                      # bare run-/wt-ids
  | gsub("(?<k>[Pp][Ii][Dd]|[Pp]ort) [0-9]+"; "\(.k) <N>")                                                                    # pids / ports
  | gsub("\\b[0-9]+(\\.[0-9]+)?m?s\\b"; "<DUR>")                                                                              # durations: 1800s / 1.3s / 30ms
  | gsub("\\b[0-9]{10,13}\\b"; "<TS>")                                                                                        # epoch-style timestamps
  | gsub("\\b(?=[0-9a-f]*[0-9])[0-9a-f]{7,40}\\b"; "<SHA>")                                                                   # git SHAs (7–40 lowercase hex, ≥1 digit — spares pure-hex words/basenames e.g. deadbeef, cafebabe.log)
  | gsub("(?<vr>(/private)?(/var/folders/[A-Za-z0-9_]+/[A-Za-z0-9_]+/T|/tmp))/[A-Za-z0-9._@%+-]+"; "<TMP>")                   # tmpdir ROOT only — path tail preserved
  | .[0:200];
[inputs] as $events
| ( [ $events[] | select(.type=="run_started")
      | { key: (.payload.run_id // (.run_id | tostring)), value: (.payload.bead_id // "?") } ]
    | from_entries ) as $r2b
| ( [ $events[] | select(.type=="run_completed" or .type=="run_failed")
      | { bead: ($r2b[(.payload.run_id // (.run_id | tostring))] // "?"),
          run_id: (.payload.run_id // (.run_id | tostring)),
          success: (.payload.success // false),
          summary: (.payload.summary // "") } ]
    | group_by(.bead) | map(.[-1]) | map({ (.bead): . }) | add // {} ) as $byBead
| [ $expected[] | . as $b | ($byBead[$b] // null) as $t
    | if $t == null
      then { bead: $b, run_id: null, verdict: "incomplete", fail_signature: null }
      else { bead: $b, run_id: $t.run_id,
             verdict: (if $t.success then "pass" else "fail" end),
             fail_signature: (if $t.success then null else ($t.summary | oneline) end) }
      end ]'

    # 5) Poll the growing stream until every expected bead is terminal, or timeout.
    # The background subscribe is concurrently appending to $raw, so a poll can catch a
    # half-written trailing line and make jq error. A transient parse error MUST NOT be
    # read as "all complete" (that would write an empty results file and exit 0 — a
    # silent false-pass that corrupts the verdict hk-1gkc8 consumes). So we ONLY accept a
    # poll whose parse SUCCEEDED and yields all $item_count rows; anything else keeps
    # waiting until the deadline. The baseline 'results' (every item incomplete) is kept
    # until the first clean parse, so a real timeout still reports incompletes + exits 1.
    echo "[scratch-daemon] batch: awaiting terminal transitions (timeout ${timeout}s)..."
    local deadline results parsed incomplete
    deadline=$(( $(date +%s) + timeout ))
    results="$(printf '%s' "$expected_json" | jq -c 'map({bead: ., run_id: null, verdict: "incomplete", fail_signature: null})')"
    while :; do
        if parsed="$(jq -n --argjson expected "$expected_json" "$jq_filter" "$raw" 2>/dev/null)" \
            && [ "$(printf '%s' "$parsed" | jq 'length')" -eq "$item_count" ]; then
            results="$parsed"
            incomplete="$(printf '%s' "$results" | jq '[.[] | select(.verdict=="incomplete")] | length')"
            if [ "$incomplete" -eq 0 ]; then break; fi
        fi
        # OFFLINE fold: the captured stream is FINAL — no more events will arrive, so
        # fold once and stop (don't burn the live-path timeout waiting on absent events).
        if [ "$mode" = "events" ]; then
            incomplete="$(printf '%s' "$results" | jq '[.[] | select(.verdict=="incomplete")] | length')"
            break
        fi
        if [ "$(date +%s)" -ge "$deadline" ]; then
            incomplete="$(printf '%s' "$results" | jq '[.[] | select(.verdict=="incomplete")] | length')"
            echo "[scratch-daemon] batch: timeout after ${timeout}s — $incomplete item(s) still incomplete" >&2
            break
        fi
        sleep 3
    done

    # 6) Emit the structured summary: JSON artifact + stable stdout lines.
    local results_file total pass fail
    results_file="$scratch/.harmonik/batch-${name}-${queue_id}.json"
    # Stamp the revision onto every row before the artifact is written. The
    # BATCH_SUMMARY line names the revision too, but that goes to stdout and
    # nobody keeps it. This file is the durable evidence, and `feedback` reads it
    # to open beads against a failure, so the commit has to travel with the rows
    # rather than with the console output.
    results="$(printf '%s' "$results" | jq -c --arg rev "$batch_rev" 'map(. + {revision: $rev})')"
    printf '%s\n' "$results" >"$results_file"
    total="$(printf '%s' "$results" | jq 'length')"
    pass="$(printf '%s' "$results" | jq '[.[] | select(.verdict=="pass")] | length')"
    fail="$(printf '%s' "$results" | jq '[.[] | select(.verdict=="fail")] | length')"
    printf '%s' "$results" \
        | jq -r '.[] | "BATCH_ITEM\t\(.bead)\t\(.verdict)\t\(.run_id // "-")\t\(.fail_signature // "-")"'

    # 7) Pair the retained event capture with the results artifact by renaming it
    # to carry the queue_id. The subscribe child is stopped FIRST so nothing is
    # still appending as we rename. The OFFLINE --from-events path is skipped
    # here: $raw is the caller's own input file and is not ours to move.
    local events_out="$raw"
    if [ "$mode" != "events" ]; then
        if [ -n "$sub_pid" ]; then
            kill "$sub_pid" 2>/dev/null || true
            wait "$sub_pid" 2>/dev/null || true
        fi
        events_out="$scratch/.harmonik/batch-${name}-${queue_id}.events.ndjson"
        mv -f "$raw" "$events_out" || events_out="$raw"
    fi
    # revision= is LAST so the existing prefix-anchored readers of this line keep
    # matching. It is not optional: a pass/fail count that cannot say which commit
    # produced it is not a verdict.
    echo "BATCH_SUMMARY name=$name total=$total pass=$pass fail=$fail incomplete=$incomplete results=$results_file events=$events_out revision=$batch_rev"

    # Non-zero exit if anything failed or stayed incomplete, so callers can branch on it.
    [ "$fail" -eq 0 ] && [ "$incomplete" -eq 0 ]
}

# ---------------------------------------------------------------------------
# Subcommand: feedback  (scratch batch FAILURES -> deduped MAIN/fleet-repo beads)
# ---------------------------------------------------------------------------
# Reads a batch results artifact (the JSON array `batch` writes — an array of
#   { "bead", "run_id"|null, "verdict": pass|fail|incomplete, "fail_signature"|null,
#     "revision" })
# and, for every FAIL item, creates-or-updates an actionable bead on the MAIN/FLEET
# repo's beads DB so a scratch-run failure becomes work the real daemon can pick up.
# `pass` and `incomplete` items are ignored.
#
# DELIBERATE FLEET WRITE: unlike every other subcommand (which targets ONLY the
# scratch clone), THIS command intentionally writes to the fleet repo's ledger —
# that is its entire purpose. The target is made explicit: `br` is run with the
# fleet repo (located via fleet_root) as its CWD in an isolated subshell, so it
# auto-discovers the fleet's .beads/*.db and NEVER the scratch clone's.
#
# IDEMPOTENCY / DEDUPE: each fail maps to a stable provenance key
#   hash = sha256(<batch-name> 0x1f <fail_signature>)[:12]   ->   label `prov:<hash>`
# Before creating, we `br list --label prov:<hash>` (default excludes closed). A hit
# means this (batch, signature) failure already has an OPEN bead → we UPDATE it
# (refresh --notes with the latest run_id/excerpt + append a recurrence comment)
# instead of filing a second one. Re-running this command on the same artifact never
# spawns a duplicate bead. (A previously-CLOSED bead is intentionally NOT reused: a
# failure that recurs after being marked resolved files a fresh, actionable bead.)
# Within a single invocation, an in-memory set collapses multiple beads that share
# one signature to a single create, independent of DB read-after-write timing.
#
# Hard project rules honored: NEVER --assignee (the daemon owns claiming) and NEVER
# status=in_progress — beads are created OPEN. Codename label `codename:test-daemon-harness`
# + topical `scratch-feedback` make the work traceable.
#
# Arg surface:
#   feedback <results-json> [--batch <name>] [--priority N] [--dry-run]
#     <results-json>  the batch artifact (e.g. <scratch>/.harmonik/batch-<name>-<qid>.json)
#     --batch <name>  override the batch name used in the provenance key + title/body.
#                     Default: parsed from the artifact filename ('batch-<name>-<qid>.json').
#                     The queue_id is deliberately EXCLUDED from the key so re-runs dedupe.
#     --priority N    priority for newly-created beads (0-4; default 2).
#     --dry-run       print the create/update plan (FEEDBACK_DRYRUN lines); touch no DB.
#
# Stable stdout (grep-able, tab-separated where noted):
#   FEEDBACK_ITEM\t<create|update>\t<scratch-bead>\t<prov-label>\t<fleet-bead-id>
#   FEEDBACK_SUMMARY batch=<name> fail_items=<n> created=<c> updated=<u> db=<fleet>/.beads
cmd_feedback() {
    local results_file batch_name="" priority="2" dry_run=0
    results_file="${1:-}"
    [ -n "$results_file" ] || die "feedback: <results-json> is required (a batch results artifact)"
    case "$results_file" in --*) die "feedback: <results-json> must come before flags, got '$results_file'";; esac
    shift || true
    while [ $# -gt 0 ]; do
        case "$1" in
            --batch)      [ $# -ge 2 ] || die "feedback: --batch needs a value"; batch_name="$2"; shift 2;;
            --batch=*)    batch_name="${1#--batch=}"; shift;;
            --priority)   [ $# -ge 2 ] || die "feedback: --priority needs a value"; priority="$2"; shift 2;;
            --priority=*) priority="${1#--priority=}"; shift;;
            --dry-run)    dry_run=1; shift;;
            *) die "feedback: unknown flag '$1' (use --batch <name>, --priority N, --dry-run)";;
        esac
    done

    [ -f "$results_file" ] || die "feedback: results file '$results_file' not found"
    command -v jq >/dev/null 2>&1 || die "feedback: jq is required to parse the results artifact"
    jq -e 'type=="array"' "$results_file" >/dev/null 2>&1 \
        || die "feedback: '$results_file' is not a JSON array (expected the batch results artifact)"

    # Locate the FLEET beads DB. This command writes there ON PURPOSE (see header).
    #
    # HERMETIC TEST SEAM (hk-6eqv9): SCRATCH_FEEDBACK_FLEET_ROOT, when set, redirects
    # the fleet write to a THROWAWAY beads-managed repo instead of this script's own
    # live fleet checkout — so the end-to-end smoke can prove the create/dedup loop
    # WITHOUT ever touching the live fleet beads DB. Unset (the normal case) it falls
    # back to fleet_root() exactly as before. Kept tiny and fail-loud on a bad path.
    local fleet
    if [ -n "${SCRATCH_FEEDBACK_FLEET_ROOT:-}" ]; then
        [ -d "$SCRATCH_FEEDBACK_FLEET_ROOT" ] \
            || die "feedback: SCRATCH_FEEDBACK_FLEET_ROOT='$SCRATCH_FEEDBACK_FLEET_ROOT' is not a directory"
        fleet="$( cd "$SCRATCH_FEEDBACK_FLEET_ROOT" && pwd -P )"
        echo "[scratch-daemon] feedback: SCRATCH_FEEDBACK_FLEET_ROOT override → targeting throwaway fleet '$fleet' (NOT the live fleet DB)" >&2
    else
        fleet="$(fleet_root)"
    fi
    [ -n "$fleet" ] || die "feedback: cannot locate the fleet repo root (script not inside a git repo) — no beads DB to target"
    [ -d "$fleet/.beads" ] || die "feedback: no .beads dir under fleet root '$fleet' — is this a beads-managed repo?"

    # Derive the batch name (used in the provenance key, title, and body). Prefer
    # --batch; else parse the artifact filename 'batch-<name>-<queue_id>.json' by
    # stripping the 'batch-' prefix and the trailing '-<queue_id>.json'.
    if [ -z "$batch_name" ]; then
        local base="${results_file##*/}"
        base="${base%.json}"
        case "$base" in
            batch-*) batch_name="${base#batch-}"; batch_name="${batch_name%-*}";;
            *)       batch_name="$base";;
        esac
        [ -n "$batch_name" ] || die "feedback: could not derive a batch name from '$results_file' — pass --batch <name>"
    fi

    # Pull just the FAIL items as compact NDJSON. pass/incomplete are skipped entirely.
    local fails
    fails="$(jq -c '.[] | select(.verdict=="fail")' "$results_file")"
    if [ -z "$fails" ]; then
        echo "[scratch-daemon] feedback: no failed items in $results_file — nothing to file"
        echo "FEEDBACK_SUMMARY batch=$batch_name fail_items=0 created=0 updated=0 db=$fleet/.beads"
        return 0
    fi

    # Portable (bash-3.2, no associative arrays) in-invocation dedupe set: a string of
    # space-padded provenance hashes already filed this run.
    local seen_hashes=" "
    local created=0 updated=0 nfail=0
    local item bead run_id sig item_rev hash label_prov title body found existing
    while IFS= read -r item; do
        [ -n "$item" ] || continue
        nfail=$((nfail + 1))
        bead="$(printf '%s' "$item"   | jq -r '.bead // "?"')"
        run_id="$(printf '%s' "$item" | jq -r '.run_id // "-"')"
        sig="$(printf '%s' "$item"    | jq -r '.fail_signature // empty')"
        [ -n "$sig" ] || sig="(no signature; bead $bead)"
        # Which commit produced this failure. Older artifacts predate the field, so
        # say so plainly rather than leaving the line blank or omitting it — a
        # reader must be able to tell "not recorded" from "recorded as nothing".
        item_rev="$(printf '%s' "$item" | jq -r '.revision // empty')"
        [ -n "$item_rev" ] || item_rev="(not recorded)"

        # Stable dedupe key: batch-name + 0x1f + signature (queue_id deliberately excluded).
        hash="$(printf '%s\x1f%s' "$batch_name" "$sig" | prov_hash)"
        label_prov="prov:$hash"

        # In-invocation dedupe: a second fail sharing this provenance key was already
        # filed/updated above — skip so we never create twice nor double-count.
        case "$seen_hashes" in
            *" $hash "*) continue;;
        esac
        seen_hashes="$seen_hashes$hash "

        title="[scratch-fail] ${batch_name}: ${sig}"
        title="${title:0:160}"
        body="$(printf 'Auto-filed from a scratch-daemon batch failure (scripts/scratch-daemon.sh feedback).\n\nbatch: %s\nrevision: %s\nscratch_bead: %s\nscratch_run_id: %s\nprovenance: %s\nfail_signature: %s\n' \
            "$batch_name" "$item_rev" "$bead" "$run_id" "$label_prov" "$sig")"

        # Look up an existing OPEN feedback bead by the provenance label (fleet DB).
        found="$( cd "$fleet" && br list --label "$label_prov" --json 2>/dev/null )" || found=""
        existing="$(printf '%s' "$found" | jq -r '(.issues // [])[0].id // empty' 2>/dev/null)"

        if [ -n "$existing" ]; then
            # Dedupe hit → UPDATE in place: refresh the latest run_id/excerpt in --notes
            # (idempotent) and append a recurrence comment (the audit trail). NO new bead.
            if [ "$dry_run" = "1" ]; then
                echo "FEEDBACK_DRYRUN action=update scratch_bead=$bead prov=$label_prov target=$existing"
            else
                ( cd "$fleet" && br update "$existing" --notes "$body" >/dev/null ) \
                    || die "feedback: br update $existing failed"
                ( cd "$fleet" && br comments add "$existing" \
                    --message "Recurrence: scratch batch '$batch_name' bead $bead run $run_id — $sig" >/dev/null ) || true
            fi
            updated=$((updated + 1))
            printf 'FEEDBACK_ITEM\tupdate\t%s\t%s\t%s\n' "$bead" "$label_prov" "$existing"
            continue
        fi

        # No existing bead → CREATE one, OPEN, never assigned (daemon owns claiming).
        if [ "$dry_run" = "1" ]; then
            echo "FEEDBACK_DRYRUN action=create scratch_bead=$bead prov=$label_prov title=$title"
            created=$((created + 1))
            continue
        fi
        local out newid
        out="$( cd "$fleet" && br create \
            --title "$title" \
            --type bug \
            --priority "$priority" \
            --labels "codename:test-daemon-harness,scratch-feedback,$label_prov" \
            --description "$body" \
            --json )" || die "feedback: br create failed for scratch bead $bead"
        newid="$(printf '%s' "$out" | jq -r '.id // empty')"
        [ -n "$newid" ] || die "feedback: br create returned no id (output: $out)"
        created=$((created + 1))
        printf 'FEEDBACK_ITEM\tcreate\t%s\t%s\t%s\n' "$bead" "$label_prov" "$newid"
    done <<< "$fails"

    echo "FEEDBACK_SUMMARY batch=$batch_name fail_items=$nfail created=$created updated=$updated db=$fleet/.beads"
}

# ---------------------------------------------------------------------------
# Dispatch
# ---------------------------------------------------------------------------
usage() {
    # Print the header comment block: every line from line 2 until the first
    # line that is not a comment. This used to be a hardcoded '2,42p' range,
    # which silently truncated the help text the moment anyone added a line to
    # the header — the help is derived from the header, so it must not depend on
    # the header's length.
    awk 'NR==1 {next} /^#/ {sub(/^# ?/, ""); print; next} {exit}' "${BASH_SOURCE[0]}"
}

main() {
    local sub="${1:-}"
    shift || true
    case "$sub" in
        init)   cmd_init   "$@";;
        build)  cmd_build  "$@";;
        up)     cmd_up     "$@";;
        status) cmd_status "$@";;
        down)   cmd_down   "$@";;
        cycle)  cmd_cycle  "$@";;
        batch)  cmd_batch  "$@";;
        feedback) cmd_feedback "$@";;
        ""|-h|--help|help) usage;;
        *) die "unknown subcommand '$sub' — run '$0 --help'";;
    esac
}

main "$@"
