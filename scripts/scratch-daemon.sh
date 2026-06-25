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
#
# Options (env vars):
#   SCRATCH_MAX_CONCURRENT  — daemon --max-concurrent      (default: 1)
#   SCRATCH_WORKFLOW_MODE   — daemon --workflow-mode        (default: review-loop)
#   SCRATCH_DAEMON_FLAGS    — extra flags appended verbatim to the daemon start
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
    tmux new-session -d -s "$sess" \
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
# Dispatch
# ---------------------------------------------------------------------------
usage() {
    sed -n '2,40p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
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
        ""|-h|--help|help) usage;;
        *) die "unknown subcommand '$sub' — run '$0 --help'";;
    esac
}

main "$@"
