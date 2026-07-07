#!/usr/bin/env bash
# core-loop-matrix.sh — the core-loop-proof matrix runner (T1 skeleton).
#
# WHAT: proves the real task-processing loop — bead → queue → correct-model harness →
#   real change → provider-comms through the sandbox → DOT review-back → terminal
#   transition — across the {harness}×{substrate} matrix, on a throwaway SCRATCH daemon
#   (never the fleet daemon, never main).
#
# THIS FILE IS THE SKELETON (epic hk-hcrvb / T1, codename:quality-system):
#   - It CYCLES a clean scratch daemon, ITERATES the cells, SUBMITS the per-cell seed
#     bead via `scratch-daemon.sh batch`, FOLDS each cell's batch-results artifact into a
#     green/red grid, and EXITS non-zero on any red.
#   - It carries NO per-gap assertions yet — those are T2 (assertion library) consuming
#     the same captured event stream. A cell is GREEN here iff its seed bead reached a
#     terminal `pass` on the scratch daemon; the deep per-gap contract lands in T2.
#   - It is HONEST about coverage: a cell with no fixture seed bead is reported PENDING
#     (loud), NEVER green. A remote cell with no reachable tcp:// worker is SKIP (loud).
#     Neither PENDING nor SKIP is counted as a pass — no false-green.
#
# REUSE (no new machinery — mission operating-model C):
#   - scratch daemon lifecycle + batch fold: scripts/scratch-daemon.sh (init/cycle/batch).
#   - harness selection: the seed bead's `harness:<family>` label (tier-1 per-bead pin,
#     internal/core/agentevents_hqwn59.go). No queue-submit --harness flag exists.
#   - red-cell → deduped bead: scripts/scratch-daemon.sh feedback (wired by T3).
#
# USAGE:
#   scripts/core-loop-matrix.sh <scratch-path> [flags]
#     --enable-claude            include claude cells (default: pi,codex only — cap-thrift)
#     --harnesses  a,b,c         override the harness set (default: pi,codex [+claude])
#     --substrates local,remote  override the substrate set (default: local[,remote])
#     --remote-worker tcp://H:P  a reachable tcp:// worker enabling the remote substrate
#                                (or env MATRIX_REMOTE_WORKER); absent/unreachable → remote SKIP-loud
#     --seed-bead  <id>          run this ONE bead in every enabled cell (skeleton smoke;
#                                its own harness:<family> label decides which harness it
#                                actually exercises — real per-cell fixtures arrive in T2)
#     --keep                     leave the scratch daemon up after the run (default: cycle only)
#     --no-cycle                 reuse an already-up scratch daemon (skip the clean reset)
#     --feedback                 file a deduped FLEET bead per red cell via scratch-daemon.sh
#                                feedback (T3, hk-9cw6q); green cells file nothing
#     --assert                   (T9) capture each cell's FULL event stream and fold it through
#                                the assertion library; the cell verdict is the per-gap fold
#                                (green=all pass, red=any fail incl known-RED, pending=SKIP-LOUD)
#     --specs <cells.json>       expected-cell specs for --assert (default:
#                                scenarios/core-loop-proof/cells.json)
#
# ENV:
#   MATRIX_REMOTE_WORKER   same as --remote-worker
#   MATRIX_SEED_MAP        path to a `cell<TAB>bead_id` map file (per-cell fixtures, T2);
#                          a cell absent from the map (and without --seed-bead) → PENDING
#   SCRATCH_BATCH_TIMEOUT  forwarded to scratch-daemon.sh batch (per-cell terminal wait)
#
# EXIT: 0 iff every ENABLED, FIXTURED cell is green. Any red → 1. PENDING/SKIP cells do
#   not flip the exit on their own, but are printed loud and counted in the summary so a
#   partial matrix is never mistaken for a full green (T9 gates on zero PENDING).

set -euo pipefail

SELF="$(basename "$0")"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRATCH_DAEMON="$REPO_ROOT/scripts/scratch-daemon.sh"

log()  { echo "[core-loop-matrix] $*"; }
die()  { echo "[core-loop-matrix] ERROR: $*" >&2; exit 1; }

command -v jq >/dev/null 2>&1 || die "jq is required"
[ -x "$SCRATCH_DAEMON" ] || die "missing runner seam: $SCRATCH_DAEMON"

# ---- parse args -----------------------------------------------------------
SCRATCH=""
ENABLE_CLAUDE=0
HARNESSES_CSV=""
SUBSTRATES_CSV=""
REMOTE_WORKER="${MATRIX_REMOTE_WORKER:-}"
SEED_BEAD=""
KEEP=0
NO_CYCLE=0
FEEDBACK=0
ASSERT=0
SPECS=""

[ $# -ge 1 ] || die "usage: $SELF <scratch-path> [flags] (see header)"
SCRATCH="$1"; shift
case "$SCRATCH" in --*) die "first arg must be <scratch-path>, got flag '$SCRATCH'";; esac

while [ $# -gt 0 ]; do
    case "$1" in
        --enable-claude)  ENABLE_CLAUDE=1; shift;;
        --harnesses)      [ $# -ge 2 ] || die "--harnesses needs a value"; HARNESSES_CSV="$2"; shift 2;;
        --harnesses=*)    HARNESSES_CSV="${1#--harnesses=}"; shift;;
        --substrates)     [ $# -ge 2 ] || die "--substrates needs a value"; SUBSTRATES_CSV="$2"; shift 2;;
        --substrates=*)   SUBSTRATES_CSV="${1#--substrates=}"; shift;;
        --remote-worker)  [ $# -ge 2 ] || die "--remote-worker needs a value"; REMOTE_WORKER="$2"; shift 2;;
        --remote-worker=*) REMOTE_WORKER="${1#--remote-worker=}"; shift;;
        --seed-bead)      [ $# -ge 2 ] || die "--seed-bead needs a value"; SEED_BEAD="$2"; shift 2;;
        --seed-bead=*)    SEED_BEAD="${1#--seed-bead=}"; shift;;
        --keep)           KEEP=1; shift;;
        --no-cycle)       NO_CYCLE=1; shift;;
        --feedback)       FEEDBACK=1; shift;;
        --assert)         ASSERT=1; shift;;
        --specs)          [ $# -ge 2 ] || die "--specs needs a value"; SPECS="$2"; shift 2;;
        --specs=*)        SPECS="${1#--specs=}"; shift;;
        *) die "unknown flag '$1' (see header for usage)";;
    esac
done

# ---- resolve the cell axes ------------------------------------------------
# Harnesses: default pi,codex (cap-thrift); claude only behind --enable-claude.
if [ -n "$HARNESSES_CSV" ]; then
    IFS=',' read -r -a HARNESSES <<< "$HARNESSES_CSV"
else
    HARNESSES=(pi codex)
    [ "$ENABLE_CLAUDE" -eq 1 ] && HARNESSES+=(claude)
fi

# Substrates: default local[,remote]. Remote is only a live axis when a tcp:// worker is
# reachable; otherwise it stays in the grid as SKIP-loud so the gap is visible, not hidden.
if [ -n "$SUBSTRATES_CSV" ]; then
    IFS=',' read -r -a SUBSTRATES <<< "$SUBSTRATES_CSV"
else
    SUBSTRATES=(local remote)
fi

# Probe a tcp://host:port worker for reachability (bash /dev/tcp; no external deps).
# Returns 0 if a TCP connect succeeds within ~3s, else 1. Uses coreutils `timeout` when
# present, else a portable background-connect + kill-after-sleep fallback — stock macOS
# has no `timeout`, and depending on it would mislabel a reachable worker as unreachable.
remote_reachable() {
    local url="$1" hostport host port
    hostport="${url#tcp://}"
    host="${hostport%%:*}"; port="${hostport##*:}"
    [ -n "$host" ] && [ -n "$port" ] || return 1
    if command -v timeout >/dev/null 2>&1; then
        timeout 3 bash -c "exec 3<>/dev/tcp/$host/$port" 2>/dev/null
    else
        # background the blocking connect, kill it if it outlives the 3s budget.
        ( exec 3<>"/dev/tcp/$host/$port" ) 2>/dev/null &
        local probe=$!
        ( sleep 3; kill "$probe" 2>/dev/null ) 2>/dev/null &
        local killer=$!
        if wait "$probe" 2>/dev/null; then kill "$killer" 2>/dev/null; return 0; fi
        return 1
    fi
}

REMOTE_OK=0
REMOTE_REASON="no --remote-worker / MATRIX_REMOTE_WORKER set"
if [ -n "$REMOTE_WORKER" ]; then
    if remote_reachable "$REMOTE_WORKER"; then
        REMOTE_OK=1
    else
        REMOTE_REASON="tcp:// worker '$REMOTE_WORKER' unreachable"
    fi
fi

# ---- per-cell seed-bead resolver ------------------------------------------
# Precedence: --seed-bead (one bead, every cell) > MATRIX_SEED_MAP row for the cell >
# unset → PENDING. Real per-cell, harness-labelled fixtures are authored in T2; the
# skeleton stays honest by reporting an un-fixtured cell as PENDING (never green).
seed_for_cell() {
    local cell="$1"
    if [ -n "$SEED_BEAD" ]; then echo "$SEED_BEAD"; return 0; fi
    if [ -n "${MATRIX_SEED_MAP:-}" ] && [ -f "$MATRIX_SEED_MAP" ]; then
        awk -F'\t' -v c="$cell" '$1==c {print $2; found=1} END{exit !found}' "$MATRIX_SEED_MAP" && return 0
    fi
    return 1
}

# ---- clean the scratch daemon ---------------------------------------------
if [ "$NO_CYCLE" -eq 0 ]; then
    log "cycling a clean scratch daemon at $SCRATCH (down → build → up)"
    "$SCRATCH_DAEMON" cycle "$SCRATCH"
else
    log "reusing already-up scratch daemon at $SCRATCH (--no-cycle)"
fi

# ---- iterate the matrix ---------------------------------------------------
# Grid rows accumulate as: cell<TAB>verdict<TAB>detail  (verdict ∈ green|red|pending|skip)
GRID=()
RED_ARTIFACTS=()
had_red=0; n_green=0; n_red=0; n_pending=0; n_skip=0

# ---- assert-mode wiring (T9) ----------------------------------------------
# In --assert mode each cell's FULL event stream (not just batch's 3 terminal types) is
# captured and folded through the assertion library; the cell verdict comes from the fold.
ASSERT_CELL="$REPO_ROOT/scripts/core-loop-assert-cell.sh"
SCRATCH_BIN="$SCRATCH/.harmonik/bin/harmonik"
SCRATCH_SOCK="$SCRATCH/.harmonik/daemon.sock"
CAP_TYPES="harness_selected,model_selected,run_started,run_completed,run_failed,workspace_merge_status,implementer_phase_complete,agent_ready,agent_ready_timeout,agent_ready_stall_detected,post_agent_ready_hang,launch_stall_detected"
CAP_DIR="$SCRATCH/.harmonik/matrix-captures"
if [ "$ASSERT" -eq 1 ]; then
    [ -x "$ASSERT_CELL" ] || die "--assert needs $ASSERT_CELL"
    [ -n "$SPECS" ] || SPECS="$REPO_ROOT/scenarios/core-loop-proof/cells.json"
    [ -f "$SPECS" ] || die "--assert: specs file not found: $SPECS"
    mkdir -p "$CAP_DIR"
fi

[ "${#HARNESSES[@]}" -gt 0 ] || die "no harnesses to run (empty --harnesses?)"
[ "${#SUBSTRATES[@]}" -gt 0 ] || die "no substrates to run (empty --substrates?)"
for h in "${HARNESSES[@]}"; do
    for s in "${SUBSTRATES[@]}"; do
        cell="${h}:${s}"

        # substrate gating — remote needs a reachable tcp:// worker
        if [ "$s" = "remote" ] && [ "$REMOTE_OK" -eq 0 ]; then
            log "SKIP  $cell — $REMOTE_REASON"
            GRID+=("$cell	skip	$REMOTE_REASON")
            n_skip=$((n_skip+1))
            continue
        fi

        # fixture gating — no seed bead → pending (loud, not green)
        local_seed=""
        if ! local_seed="$(seed_for_cell "$cell")" || [ -z "$local_seed" ]; then
            log "PENDING $cell — no fixture seed bead (T2 wires per-cell fixtures)"
            GRID+=("$cell	pending	no fixture seed bead")
            n_pending=$((n_pending+1))
            continue
        fi

        # run the cell: submit the seed bead through the scratch daemon's batch fold.
        # batch exits 0 iff every submitted bead reached a terminal pass; it always
        # writes a results artifact whose path we echo for T2/T3 to consume.
        batch_name="matrix-${h}-${s}"
        log "RUN   $cell — batch '$batch_name' seed=$local_seed"

        # --assert: arm a FULL-type capture BEFORE submitting (no missed-event race).
        cap_pid=""; cap_file="$CAP_DIR/${h}-${s}.ndjson"
        if [ "$ASSERT" -eq 1 ]; then
            [ -x "$SCRATCH_BIN" ] || die "--assert: scratch binary not built ($SCRATCH_BIN)"
            "$SCRATCH_BIN" subscribe --socket "$SCRATCH_SOCK" --types "$CAP_TYPES" --heartbeat 30s \
                > "$cap_file" 2>/dev/null &
            cap_pid=$!
        fi

        batch_out=""
        if batch_out="$("$SCRATCH_DAEMON" batch "$SCRATCH" "$batch_name" --beads "$local_seed" 2>&1)"; then
            batch_verdict="green"
        else
            batch_verdict="red"
        fi
        results_path="$(printf '%s\n' "$batch_out" | sed -n 's/.*results=\([^ ]*\).*/\1/p' | tail -1)"
        printf '%s\n' "$batch_out" | grep -E '^BATCH_(ITEM|SUMMARY)' || true

        # Determine the cell verdict. Without --assert it is the batch terminal outcome.
        # With --assert it is the assertion fold over the full captured stream.
        cell_verdict="$batch_verdict"; detail="${results_path:-no-artifact}"
        if [ "$ASSERT" -eq 1 ]; then
            [ -n "$cap_pid" ] && kill "$cap_pid" 2>/dev/null || true
            # resolve the cell spec, overriding seed_bead with the real dispatched id.
            spec="$(jq -c --arg c "$cell" --arg sb "$local_seed" \
                      '.cells[] | select(.cell==$c) | .seed_bead=$sb' "$SPECS" 2>/dev/null || true)"
            if [ -z "$spec" ]; then
                cell_verdict="pending"; detail="no spec for cell in $SPECS"
            else
                # gap2 remote cells fold against the local cell's captured stream.
                ref="-"; [ "$s" = "remote" ] && [ -f "$CAP_DIR/${h}-local.ndjson" ] && ref="$CAP_DIR/${h}-local.ndjson"
                fold_out="$(bash "$ASSERT_CELL" "$cap_file" "$spec" "$ref" 2>&1)"; fold_rc=$?
                printf '%s\n' "$fold_out" | grep '^GAP' || true
                case "$fold_rc" in
                    0) cell_verdict="green" ;;
                    2) cell_verdict="pending" ;;
                    *) cell_verdict="red" ;;
                esac
                detail="$(printf '%s\n' "$fold_out" | grep '^CELL_VERDICT' | tail -1)"
            fi
        fi

        case "$cell_verdict" in
            green)   n_green=$((n_green+1)) ;;
            red)     n_red=$((n_red+1)); had_red=1 ;;
            pending) n_pending=$((n_pending+1)) ;;
        esac
        GRID+=("$cell	$cell_verdict	$detail")

        # T3 (hk-9cw6q): red cells → deduped fleet bead. Stash the (batch,artifact) pair;
        # green cells file nothing. Feedback runs AFTER the grid (it reads the persisted
        # results JSON and writes the FLEET beads DB — independent of the scratch daemon).
        if [ "$cell_verdict" = "red" ] && [ -n "$results_path" ]; then
            RED_ARTIFACTS+=("$batch_name	$results_path")
        fi
    done
done

if [ "$KEEP" -eq 0 ] && [ "$NO_CYCLE" -eq 0 ]; then
    "$SCRATCH_DAEMON" down "$SCRATCH" >/dev/null 2>&1 || true
fi

# ---- print the grid -------------------------------------------------------
echo
echo "================ core-loop-proof matrix ================"
printf '%-16s %-8s %s\n' "CELL" "VERDICT" "DETAIL"
for row in "${GRID[@]:-}"; do
    [ -n "$row" ] || continue
    IFS=$'\t' read -r cell verdict detail <<< "$row"
    case "$verdict" in
        green)   mark="✅ GREEN" ;;
        red)     mark="❌ RED" ;;
        pending) mark="⏳ PENDING" ;;
        skip)    mark="⚠️  SKIP" ;;
        *)       mark="$verdict" ;;
    esac
    printf '%-16s %-8s %s\n' "$cell" "$mark" "$detail"
done
echo "--------------------------------------------------------"
echo "green=$n_green red=$n_red pending=$n_pending skip=$n_skip"
echo "MATRIX_SUMMARY green=$n_green red=$n_red pending=$n_pending skip=$n_skip"
echo "========================================================"

# ---- T3 (hk-9cw6q): red-cell → deduped fleet bead -------------------------
# For each red cell, hand its persisted results artifact to scratch-daemon.sh feedback,
# which files-or-updates ONE fleet bead per distinct fail-signature (dedupe key =
# sha256(batch-name 0x1f fail_signature)). Green cells were never stashed, so file
# nothing. Best-effort: a feedback failure must not flip the matrix's own exit code.
if [ "$FEEDBACK" -eq 1 ] && [ "${#RED_ARTIFACTS[@]}" -gt 0 ]; then
    echo
    log "feedback: filing deduped fleet beads for ${#RED_ARTIFACTS[@]} red cell(s)"
    for pair in "${RED_ARTIFACTS[@]}"; do
        IFS=$'\t' read -r fb_batch fb_path <<< "$pair"
        [ -f "$fb_path" ] || { log "feedback: results artifact gone for $fb_batch ($fb_path) — skipping"; continue; }
        "$SCRATCH_DAEMON" feedback "$fb_path" --batch "$fb_batch" || log "feedback: non-zero for $fb_batch (continuing)"
    done
elif [ "$FEEDBACK" -eq 1 ]; then
    log "feedback: no red cells — nothing to file"
fi

# Exit non-zero on any red. PENDING/SKIP are surfaced but do not by themselves fail the
# skeleton — the full-green gate (T9) is what forbids residual PENDING.
[ "$had_red" -eq 0 ]
