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
# Usage:
#   ./scripts/scratch-daemon.sh init   <scratch-path> [<source-repo>]
#   ./scripts/scratch-daemon.sh build  <scratch-path>
#   ./scripts/scratch-daemon.sh up     <scratch-path>
#   ./scripts/scratch-daemon.sh status <scratch-path>
#   ./scripts/scratch-daemon.sh down   <scratch-path>
#   ./scripts/scratch-daemon.sh cycle  <scratch-path>   # down + build + up (the fast loop)
#   ./scripts/scratch-daemon.sh batch  <scratch-path> <name> --beads id1,id2,...  # submit + structured pass/fail
#   ./scripts/scratch-daemon.sh batch  <scratch-path> <name> --file  <queue.json> # submit a queue-file batch
#   ./scripts/scratch-daemon.sh feedback <results-json> [--batch <name>] [--dry-run] # scratch FAILURES -> deduped MAIN-repo beads
#
# Options (env vars):
#   SCRATCH_MAX_CONCURRENT  — daemon --max-concurrent      (default: 1)
#   SCRATCH_WORKFLOW_MODE   — daemon --workflow-mode        (default: review-loop)
#   SCRATCH_DAEMON_FLAGS    — extra flags appended verbatim to the daemon start
#   SCRATCH_BATCH_TIMEOUT   — batch: max seconds to await terminal events (default: 1800)
#
# Pairs with the fast remote reproducer:
#   go test -tags=scenario -run TestScenario_RemoteSubstrate_Localhost_E2E ./internal/daemon/
#
# Refs: hk-4tdlw (scratch-clone standalone test-daemon iteration loop).

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
# Subcommand: init
# ---------------------------------------------------------------------------
cmd_init() {
    local scratch source_repo
    scratch="$(guard_path "${1:-}")"
    source_repo="${2:-}"

    if [ -z "$source_repo" ]; then
        # Default: this repo (the one the script lives in). Prefer the origin URL
        # so the clone is a true independent checkout; fall back to the local path.
        local repo_root
        repo_root="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
        source_repo="$(git -C "$repo_root" remote get-url origin 2>/dev/null || echo "$repo_root")"
    fi

    if [ -d "$scratch/.git" ]; then
        echo "[scratch-daemon] clone already present at $scratch — skipping git clone"
    else
        echo "[scratch-daemon] cloning $source_repo → $scratch"
        git clone "$source_repo" "$scratch"
    fi

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
    echo "[scratch-daemon] init complete. Next: $0 build $scratch && $0 up $scratch"
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
    local commit_hash
    commit_hash="$(git -C "$scratch" rev-parse HEAD 2>/dev/null || echo unknown)"
    echo "[scratch-daemon] building scratch binary → $bin (commit $commit_hash)"
    # Build FROM the scratch clone's source so the daemon runs exactly the code in
    # that checkout. Same ldflags stamp as the Makefile's build-harmonik target.
    go build -C "$scratch" -ldflags "-X main.commitHash=${commit_hash}" -o "$bin" ./cmd/harmonik
    echo "[scratch-daemon] build OK"
}

# ---------------------------------------------------------------------------
# Subcommand: up
# ---------------------------------------------------------------------------
cmd_up() {
    local scratch bin sess log sock
    scratch="$(guard_path "${1:-}")"
    assert_not_supervised "$scratch"
    bin="$(scratch_bin "$scratch")"
    [ -x "$bin" ] || die "scratch binary not built — run: $0 build $scratch"
    sess="$(session_name "$scratch")"
    log="$(scratch_log "$scratch")"
    sock="$(scratch_sock "$scratch")"

    if tmux has-session -t "$sess" 2>/dev/null; then
        die "tmux session '$sess' already exists — run '$0 down $scratch' first (or it is already up)"
    fi

    local max_concurrent="${SCRATCH_MAX_CONCURRENT:-1}"
    local workflow_mode="${SCRATCH_WORKFLOW_MODE:-review-loop}"
    local extra_flags="${SCRATCH_DAEMON_FLAGS:-}"

    echo "[scratch-daemon] starting standalone daemon (session=$sess, project=$scratch)"
    # Standalone start = the bare `harmonik --project <path>` binary run INSIDE a
    # tmux session. NO `harmonik supervise` => NO auto-revive, so a plain
    # `down`/pkill stays down for a clean rebuild. API keys are stripped so the
    # run bills the subscription pool (codename:credfence), matching smoke-scratch.
    # shellcheck disable=SC2016
    tmux new-session -d -s "$sess" -c "$scratch" \
        "env -u ANTHROPIC_API_KEY -u ANTHROPIC_AUTH_TOKEN \
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
#       { "bead", "run_id"|null, "verdict": pass|fail|incomplete, "fail_signature"|null }
#     where fail_signature is a one-line (<=200ch) excerpt of the run's failure summary.
#     This artifact is the authoritative machine input for the feedback-bead step.
#   - Stable stdout lines (grep-able), one BATCH_ITEM per item, tab-separated:
#       BATCH_SUBMIT  name=<name> queue_id=<id> items=<n>
#       BATCH_ITEM\t<bead>\t<verdict>\t<run_id|->\t<fail_signature|->
#       BATCH_SUMMARY name=<name> total=<n> pass=<p> fail=<f> incomplete=<i> results=<path>
#   - 'incomplete' = no terminal event before SCRATCH_BATCH_TIMEOUT elapsed.
#
# Exit: 0 if every item passed; 1 if any item failed or stayed incomplete.
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
        raw="$(mktemp "${TMPDIR:-/tmp}/scratch-batch.XXXXXX")"
        "$bin" subscribe --socket "$sock" \
            --types run_started,run_completed,run_failed --heartbeat 30s \
            >"$raw" 2>>"$(scratch_log "$scratch")" &
        sub_pid=$!
        # Tear down the background reader + temp stream on any exit; keep the results file.
        trap 'kill "$sub_pid" 2>/dev/null || true; rm -f "$raw" 2>/dev/null || true' EXIT

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
    printf '%s\n' "$results" >"$results_file"
    total="$(printf '%s' "$results" | jq 'length')"
    pass="$(printf '%s' "$results" | jq '[.[] | select(.verdict=="pass")] | length')"
    fail="$(printf '%s' "$results" | jq '[.[] | select(.verdict=="fail")] | length')"
    printf '%s' "$results" \
        | jq -r '.[] | "BATCH_ITEM\t\(.bead)\t\(.verdict)\t\(.run_id // "-")\t\(.fail_signature // "-")"'
    echo "BATCH_SUMMARY name=$name total=$total pass=$pass fail=$fail incomplete=$incomplete results=$results_file"

    # Non-zero exit if anything failed or stayed incomplete, so callers can branch on it.
    [ "$fail" -eq 0 ] && [ "$incomplete" -eq 0 ]
}

# ---------------------------------------------------------------------------
# Subcommand: feedback  (scratch batch FAILURES -> deduped MAIN/fleet-repo beads)
# ---------------------------------------------------------------------------
# Reads a batch results artifact (the JSON array `batch` writes — an array of
#   { "bead", "run_id"|null, "verdict": pass|fail|incomplete, "fail_signature"|null })
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
    local item bead run_id sig hash label_prov title body found existing
    while IFS= read -r item; do
        [ -n "$item" ] || continue
        nfail=$((nfail + 1))
        bead="$(printf '%s' "$item"   | jq -r '.bead // "?"')"
        run_id="$(printf '%s' "$item" | jq -r '.run_id // "-"')"
        sig="$(printf '%s' "$item"    | jq -r '.fail_signature // empty')"
        [ -n "$sig" ] || sig="(no signature; bead $bead)"

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
        body="$(printf 'Auto-filed from a scratch-daemon batch failure (scripts/scratch-daemon.sh feedback).\n\nbatch: %s\nscratch_bead: %s\nscratch_run_id: %s\nprovenance: %s\nfail_signature: %s\n' \
            "$batch_name" "$bead" "$run_id" "$label_prov" "$sig")"

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
    sed -n '2,42p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
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
