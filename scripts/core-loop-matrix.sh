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
#     --gate                     (WS4-5) FORCED LT-gate exit: non-zero unless EVERY cell is
#                                green — any red OR pending OR skip fails (the T9 zero-PENDING
#                                gate). Without it the default lenient exit (red-only) applies.
#     --json                     emit the per-cell grid as a machine-readable JSON object on the
#                                LAST stdout line (marker `MATRIX_JSON `) for the assessor to fold.
#
# ENV:
#   MATRIX_REMOTE_WORKER   same as --remote-worker
#   MATRIX_SEED_MAP        path to a `cell<TAB>bead_id` map file (per-cell fixtures, T2);
#                          a cell absent from the map (and without --seed-bead) → PENDING
#   SCRATCH_BATCH_TIMEOUT  forwarded to scratch-daemon.sh batch (per-cell terminal wait)
#
# EXIT (default, lenient): 0 iff no red cell. PENDING/SKIP are printed loud and counted but do
#   not flip the exit on their own. EXIT (--gate, forced LT): 0 iff EVERY cell is green — any
#   red OR pending OR skip → non-zero (the T9 zero-PENDING gate; a partial matrix never passes).
#
# PROVENANCE IS PART OF THE VERDICT (hk-48zdw). Before it runs anything, and again beside
#   the grid, this runner asks `scratch-daemon.sh provenance` whether the scratch binary
#   carries a Go vcs stamp that names the pinned commit with vcs.modified=false. The WORSE
#   of the two answers is the one that stands — a later `clean` never clears an earlier
#   refusal, because the cells were graded by the binary that refusal named. It is not
#   advice: `all_green` is false and --gate exits non-zero for any answer but `clean`, and
#   the answer is printed on the `MATRIX_PROVENANCE` line and in the `provenance` field of
#   MATRIX_JSON. A green grid from a binary that cannot name its commit is not a pass, and
#   this runner used to print exactly that.

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
GATE=0
JSON=0

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
        --gate)           GATE=1; shift;;
        --json)           JSON=1; shift;;
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

# ---- D1: local-model (pi) readiness preflight -----------------------------
# Before running any pi cell we POST a tiny completion to the ornith /v1/completions
# endpoint (the same loopback tunnel -> DGX vLLM the pi harness uses). A real non-empty
# completion => the model is live, proceed. A 0-byte reply / timeout / non-200 => the vLLM
# behind the tunnel is wedged, so the cell is marked SKIP-loud (never green; fails --gate)
# with detail "pi endpoint wedged/no response" rather than letting the daemon spend minutes
# discovering the wedge as a run_failed. Always-on for pi (no --no-preflight escape hatch):
# a green pi cell MUST be backed by a live model, and catching the wedge here is the point.
# Endpoint, model and key are READ FROM THE SCRATCH'S OWN harnesses.pi config, which is
# what the pi harness will actually dial. Override via PI_BASE_URL / PI_MODEL / PI_KEY_FILE.
#
# WHY THIS IS READ AND NOT WRITTEN DOWN. These three were hardcoded here, with a comment
# claiming they "default to the overlay's harnesses.pi values". They did not. The overlay
# moved to port 8553 and this probe stayed on 8551, so the probe dialled a port nothing was
# listening on, every pi cell reported "pi endpoint wedged/no response", and the gate skipped
# the only cell it runs. The endpoint was healthy the whole time. That message was then read
# as evidence of a wedged vLLM and copied into a handoff and a bead, and the wrong thing was
# believed for days. A probe that does not dial what the harness dials is not a preflight, it
# is a second source of truth that silently disagrees.
#
# The model matters for the same reason: vLLM 404s an unknown model id, and a 404 here is
# indistinguishable from a wedge in the output above.
harness_cfg_value() {
    # Pull one scalar out of the harnesses.<harness> block of the scratch config. Flat
    # scalars only; trailing `# ...` comments and surrounding quotes are stripped.
    awk -v want="$2" -v harness="  $1:" '
        /^harnesses:/            { in_h=1; next }
        in_h && /^[^[:space:]]/  { in_h=0 }
        in_h && $0 ~ "^" harness "$" { in_pi=1; next }
        in_pi && /^  [^[:space:]]/ { in_pi=0 }
        in_pi {
            line=$0
            sub(/#.*/, "", line)
            if (match(line, /^[[:space:]]*[A-Za-z_]+:/)) {
                key=substr(line, RSTART, RLENGTH-1); gsub(/[[:space:]]/, "", key)
                if (key == want) {
                    val=substr(line, RSTART+RLENGTH)
                    gsub(/^[[:space:]]+|[[:space:]]+$/, "", val)
                    gsub(/^["'"'"']|["'"'"']$/, "", val)
                    print val; exit
                }
            }
        }
    ' "$SCRATCH/.harmonik/config.yaml" 2>/dev/null
}
PI_BASE_URL="${PI_BASE_URL:-$(harness_cfg_value pi base_url)}"
PI_MODEL="${PI_MODEL:-$(harness_cfg_value pi model)}"
PI_KEY_FILE="${PI_KEY_FILE:-$HOME/.config/harmonik/ornith.key}"
pi_preflight() {
    command -v curl >/dev/null 2>&1 || { log "preflight: curl missing — cannot probe pi endpoint"; return 1; }
    # An unreadable config is a failed preflight, not a fallback to a guess. Guessing is
    # exactly what produced the false wedge.
    [ -n "$PI_BASE_URL" ] || { log "preflight: no harnesses.pi.base_url in $SCRATCH/.harmonik/config.yaml"; return 1; }
    [ -n "$PI_MODEL" ]    || { log "preflight: no harnesses.pi.model in $SCRATCH/.harmonik/config.yaml"; return 1; }
    local key="" body
    [ -f "$PI_KEY_FILE" ] && key="$(tr -d '[:space:]' < "$PI_KEY_FILE" 2>/dev/null)"
    body="$(curl -sS -m 12 -X POST "$PI_BASE_URL/completions" \
        -H "Authorization: Bearer $key" -H "Content-Type: application/json" \
        -d "{\"model\":\"$PI_MODEL\",\"prompt\":\"ping\",\"max_tokens\":16}" 2>/dev/null)" || return 1
    [ -n "$body" ] || return 1
    # A model the server does not serve returns a 404 body with no choices. Report that as
    # itself rather than as a wedge — they need opposite fixes.
    if printf '%s' "$body" | jq -e '.error' >/dev/null 2>&1; then
        log "preflight: endpoint answered but rejected the request: $(printf '%s' "$body" | jq -r '.error.message // .error' 2>/dev/null | head -1)"
        return 1
    fi
    printf '%s' "$body" \
        | jq -e '((.choices[0].text // .choices[0].message.content // "") | tostring | length) > 0' \
          >/dev/null 2>&1
}

# ---- model resolution for the cell specs ----------------------------------
# cells.json does NOT name any model. It cannot: a model name written into a gate spec is a
# copy of a fact that lives somewhere else, and it goes stale the day the model changes
# without anything noticing. That happened — the spec pinned a model the endpoint had stopped
# serving, and the gate could not pass until somebody hand-edited four files.
#
# So the spec says WHERE the model comes from and this resolves it, from the same two places
# harmonik itself resolves it from (EM-012b):
#
#   a per-bead `model:<alias>` label   — the pin under test (the claude seed uses this), or
#   harnesses.<harness>.model in config — when no label pins one (the pi seeds use this).
#
# A seed with neither resolves to empty, and the model check is skipped for that cell — which
# is right, because nothing pinned a model, so there is nothing to be faithful to.
seed_model_for() {
    # seed_model_for <seed-key> <harness> — the model this seed's run must select.
    local key="$1" harness="$2" label
    label="$(jq -r --arg k "$key" '
        .seeds[] | select(.key == $k) | .labels[]? | select(startswith("model:"))
    ' "$REPO_ROOT/scenarios/core-loop-proof/seed-beads.json" 2>/dev/null | head -1)"
    if [ -n "$label" ]; then printf '%s' "${label#model:}"; return 0; fi
    harness_cfg_value "$harness" model
}

# foreign_models_for <seed-key> <harness> — every OTHER harness family's model, which is
# exactly the set that must never appear on this cell's runs. Derived, so adding a harness or
# changing a model updates the leak check for free instead of needing a second edit.
foreign_models_for() {
    local own_key="$1" own_harness="$2" out="" k h m
    while IFS=$'\t' read -r k h; do
        [ -n "$k" ] || continue
        m="$(seed_model_for "$k" "$h")"
        [ -n "$m" ] || continue
        [ "$m" = "$(seed_model_for "$own_key" "$own_harness")" ] && continue
        case "$out" in *"|$m|"*) continue;; esac
        out="$out|$m|"
    done <<EOF
$(jq -r '.seeds[] | "\(.key)\t\(.harness)"' "$REPO_ROOT/scenarios/core-loop-proof/seed-beads.json" 2>/dev/null)
EOF
    printf '%s' "$out" | tr '|' '\n' | grep -v '^$' | jq -R . | jq -sc .
}

# ---- landing-branch resolution for the cell specs -------------------------
# Same rule as the model above, for the same reason: cells.json names NO branch. It named
# branches once, and the names disagreed with the daemon and with the seeds the cells come
# from. Which cells said what is recorded in ONE place — the "//lands_on" notes at the top of
# scenarios/core-loop-proof/cells.json. This header keeps no second tally, because several
# copies of one count going stale together is the same defect one level up. The defect has one
# shape: a fixture keeping a private copy of a fact another component owns. So read the fact
# from its owners.
#
# TWO owners, and the precedence here mirrors the daemon's own
# (internal/daemon/branching.go resolveBranchingFrom: bead body > project defaults > spec
# default):
#
#   a seed's `target_branch`  — becomes the bead's ## Branching target_branch, tier 1, or
#   defaults.lands_on         — .harmonik/branching.yaml, tier 2, when the seed asks for none.
#
# Tier 1 is the bead body and nothing else. internal/daemon/branching.go branchingYAMLShape
# maps the body key `target_branch` onto LandsOn, so a seed that declares one wins outright,
# and a seed that declares none drops to the tier-2 project default.
# scripts/scratch-daemon.sh isolate_push_target rewrites that key to `scratch/main`, and
# scripts/core-loop-seed.sh already reads defaults.start_from out of the same file with the
# same awk shape. Reader and writer cannot drift when there is one written value.

# daemon_default_lands_on: defaults.lands_on out of the SCRATCH daemon's branching.yaml.
# This is both the landing branch for a seed with no target_branch AND the trunk t10 requires
# not to move. Fatal on a missing file or key — a silent fall back to `main` is how the
# original defect survived.
daemon_default_lands_on() {
    local branching="$SCRATCH/.harmonik/branching.yaml" val
    [ -f "$branching" ] \
        || die "no branching config at $branching — this scratch was not prepared by scratch-daemon.sh init, so the branch the daemon lands on cannot be read"
    val="$(awk '/^[[:space:]]+lands_on:/ { print $2; exit }' "$branching")"
    [ -n "$val" ] \
        || die "$branching has no defaults.lands_on — refusing to guess the branch the daemon lands on"
    printf '%s' "$val"
}

# seed_lands_on_for <seed-key> — the branch THIS seed's run must land on.
seed_lands_on_for() {
    local key="$1" tb
    tb="$(jq -r --arg k "$key" '.seeds[] | select(.key == $k) | .target_branch // empty' \
            "$REPO_ROOT/scenarios/core-loop-proof/seed-beads.json" 2>/dev/null | head -1)"
    if [ -n "$tb" ]; then printf '%s' "$tb"; return 0; fi
    printf '%s' "$TRUNK_BRANCH"
}

# cell_slug: a queue-name-safe + filesystem-safe token for a cell name (':' and '/' -> '-'),
# so a cell whose name is NOT harness:substrate (e.g. the D4 extra cell pi-dot:local) gets
# its own capture file and batch/queue name, and never collides with pi:local's. Hyphen
# (not underscore) because the daemon's queue-name validator rejects '_' (queue_name_invalid).
cell_slug() { printf '%s' "$1" | tr ':/' '--'; }

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

# The core-loop-proof cells pin dispatch.workflow_mode = "dot" (cells.json). The scratch
# daemon must therefore boot in dot mode. The scratch daemon also defaults to dot.
# Export the value so both the cycle-up and batch-triggered cmd_up calls use it.
# An explicit operator override still wins.
export SCRATCH_WORKFLOW_MODE="${SCRATCH_WORKFLOW_MODE:-dot}"

# ---- clean the scratch daemon ---------------------------------------------
if [ "$NO_CYCLE" -eq 0 ]; then
    log "cycling a clean scratch daemon at $SCRATCH (down → build → up) [workflow-mode=$SCRATCH_WORKFLOW_MODE]"
    "$SCRATCH_DAEMON" cycle "$SCRATCH"
else
    log "reusing already-up scratch daemon at $SCRATCH (--no-cycle)"
fi

# ---- provenance: the binary this grid grades must name its own commit ------
# WHY THIS IS HERE AND NOT IN A README. This runner used to print an all-green
# grid, and BATCH_SUMMARY used to print a bare commit hash beside it, for a
# binary the Go toolchain had stamped vcs.modified=true. The assessor contract
# reads a bare hash as "clean" and treats a dirty stamp exactly as +local-edits —
# no result from such a binary is an audit of that commit. So the two truths sat
# twenty-five lines apart in one log, and the run read as a valid PASS. It was
# not a rare accident either: scripts/core-loop-seed.sh writes an untracked
# review-loop.dot into the tree before the build, Go's stamp counts untracked
# files, and the gate therefore dirtied itself on EVERY run (hk-48zdw).
#
# A gate has to assert its own provenance. `scratch-daemon.sh provenance` is that
# assertion with an exit code on it: 0 only when the built binary carries a Go
# vcs stamp naming the pinned commit with vcs.modified=false.
#
# IT IS READ TWICE, ON PURPOSE. Once here, so a void run costs seconds instead of
# a full matrix of real agents; once again beside the verdict, because the claim
# being made is about the binary that actually ran, and a matrix run is minutes of
# real agents editing this tree. `batch` itself never rebuilds — it calls cmd_up,
# which refuses a binary whose recorded revision is not the pin — so the second
# read is for the case where something ELSE replaced the binary. Do not read it as
# a re-measurement that can clear the first: the token is monotonic (below) and a
# later `clean` is recorded and ignored.
#
# WHAT EACH MODE DOES WITH IT.
#   --gate (the LT leg)  refuses. The gate exists to produce a result an assessor
#                        may fold into a PASS, and there is no such result here.
#   default (lenient)    reports. scripts/scratch-daemon.sh deliberately keeps the
#                        edit-and-cycle developer loop working on a modified tree,
#                        and failing that loop here would delete it. But the JSON
#                        this run emits still carries all_green:false and the
#                        provenance token, so a lenient run cannot be quoted as a
#                        clean one either.
#
# PROVENANCE_TOKEN starts at `unchecked`, which is not `clean`, so any path that
# reaches the verdict without reading the stamp fails the same way a dirty one does.
#
# THE TOKEN IS MONOTONIC — it may only ever get WORSE (hk-48zdw). The stamp is read
# twice, and without this rule the second reading simply overwrote the first. Two
# ways that hands back a `clean` nobody earned:
#   - lenient mode does not stop on a bad pre-flight, so a run whose FIRST reading
#     said `dirty` grades every cell with the binary that reading refused. If
#     anything cleans the tree and rebuilds before the second reading, the run then
#     reports `clean`, and the cell verdicts it reports were never audited.
#   - a `provenance` that prints a clean line and THEN exits non-zero used to leave
#     the token at `clean`, because only the return value carried the refusal and
#     nothing downstream reads the return value. `all_green` and --gate both key on
#     the token alone, so the token has to carry it.
# A later `clean` is therefore recorded and ignored, never adopted.
PROVENANCE_TOKEN="unchecked"
PROVENANCE_LINE="provenance was never read"
PROVENANCE_FRESH_TOKEN="unchecked"   # what the LAST read alone said (for reporting)
PROVENANCE_READS=0

read_provenance() {
    local out status fresh_token fresh_line
    out="$("$SCRATCH_DAEMON" provenance "$SCRATCH" 2>&1)"; status=$?
    # The token, not the exit code, is what the verdict keys on: a subcommand this
    # scratch-daemon.sh is too old to have exits non-zero with no token at all, and
    # that must read as "not proven", never as "no news is good news".
    fresh_token="$(awk '$1=="SCRATCH_PROVENANCE" && t=="" {t=$2} END {print (t==""?"unreadable":t)}' <<<"$out")"
    fresh_line="$(awk '$1=="SCRATCH_PROVENANCE" && l=="" {l=$0} END {print l}' <<<"$out")"
    [ -n "$fresh_line" ] || fresh_line="SCRATCH_PROVENANCE $fresh_token (no machine-readable line; '$SCRATCH_DAEMON provenance' exited $status)"
    # A clean line from a command that then refused is not a clean answer. The two
    # halves disagree about one binary and only the pessimistic half may be believed.
    if [ "$status" -ne 0 ] && [ "$fresh_token" = "clean" ]; then
        fresh_line="SCRATCH_PROVENANCE inconsistent ('$SCRATCH_DAEMON provenance' printed a clean line and then exited $status) — $fresh_line"
        fresh_token="inconsistent"
    fi
    PROVENANCE_TEXT="$out"
    PROVENANCE_FRESH_TOKEN="$fresh_token"
    if [ "$PROVENANCE_READS" -eq 0 ] || [ "$fresh_token" != "clean" ] || [ "$PROVENANCE_TOKEN" = "clean" ]; then
        PROVENANCE_TOKEN="$fresh_token"
        PROVENANCE_LINE="$fresh_line"
    else
        log "provenance now reads clean, but an earlier read of this run said '$PROVENANCE_TOKEN' — keeping the earlier one. Every cell above was graded by the binary that reading refused."
    fi
    PROVENANCE_READS=$((PROVENANCE_READS + 1))
    # The CUMULATIVE token is the verdict, so that is what the caller is told.
    [ "$PROVENANCE_TOKEN" = "clean" ]
}

if read_provenance; then
    log "$PROVENANCE_LINE"
else
    printf '%s\n' "${PROVENANCE_TEXT:-}" >&2
    if [ "$GATE" -eq 1 ]; then
        die "--gate: the scratch binary cannot prove it is the pinned commit (provenance=$PROVENANCE_TOKEN).
  Nothing this run could print would be an audit of that commit, so it stops before running the matrix.
  The message above says what is wrong with the binary and what to do about it."
    fi
    log "WARNING: provenance=$PROVENANCE_TOKEN — this run cannot be quoted as an audit of the pinned commit (all_green will be false)"
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
# gap6 (D4) needs the dot round-trip events too: reviewer_verdict (verdict), and the
# node_dispatch_requested/decided that mark the implementer re-dispatch after a
# REQUEST_CHANGES. Harmless extras for the single/codex/claude cells (which never emit them).
CAP_TYPES="harness_selected,model_selected,run_started,run_completed,run_failed,workspace_merge_status,implementer_phase_complete,reviewer_verdict,node_dispatch_requested,node_dispatch_decided,agent_ready,agent_ready_timeout,agent_ready_stall_detected,post_agent_ready_hang,launch_stall_detected"
CAP_DIR="$SCRATCH/.harmonik/matrix-captures"
# The branch the daemon lands a seed on when the seed asks for none — and the branch t10
# requires NOT to move. Read once, AFTER the cycle above, because `cycle` may rebuild the
# scratch. Only --assert needs it, and only --assert may pay its fatal failure.
TRUNK_BRANCH=""
if [ "$ASSERT" -eq 1 ]; then
    [ -x "$ASSERT_CELL" ] || die "--assert needs $ASSERT_CELL"
    [ -n "$SPECS" ] || SPECS="$REPO_ROOT/scenarios/core-loop-proof/cells.json"
    [ -f "$SPECS" ] || die "--assert: specs file not found: $SPECS"
    mkdir -p "$CAP_DIR"
    TRUNK_BRANCH="$(daemon_default_lands_on)"
    log "daemon defaults.lands_on = '$TRUNK_BRANCH' — cells with no seed target_branch land here, and t10 requires this branch not to move for the cells that do"
fi

[ "${#HARNESSES[@]}" -gt 0 ] || die "no harnesses to run (empty --harnesses?)"
[ "${#SUBSTRATES[@]}" -gt 0 ] || die "no substrates to run (empty --substrates?)"

# ---- build the ordered run list (HARNESSES×SUBSTRATES + EXTRA_CELLS) -------
# Each entry is "cell<TAB>harness<TAB>substrate". EXTRA_CELLS (D4) lets a cell whose name
# is NOT harness:substrate — e.g. the dot-mode pi-dot:local cell — join the run WITHOUT
# forcing the whole matrix off its {harness}×{substrate} shape (codex/claude cells stay
# intact). Format: comma-separated "cell|harness|substrate" tuples in env EXTRA_CELLS,
# e.g. EXTRA_CELLS='pi-dot:local|pi|local'. Its seed resolves through MATRIX_SEED_MAP /
# cells.json exactly like every other cell (the cell must have a row + a spec).
RUN_CELLS=()
for h in "${HARNESSES[@]}"; do
    for s in "${SUBSTRATES[@]}"; do
        RUN_CELLS+=("${h}:${s}	${h}	${s}")
    done
done
if [ -n "${EXTRA_CELLS:-}" ]; then
    IFS=',' read -r -a _extra_cells <<< "$EXTRA_CELLS"
    for _ec in "${_extra_cells[@]}"; do
        [ -n "$_ec" ] || continue
        IFS='|' read -r _ec_cell _ec_h _ec_s <<< "$_ec"
        [ -n "$_ec_cell" ] && [ -n "$_ec_h" ] && [ -n "$_ec_s" ] \
            || die "EXTRA_CELLS entry '$_ec' malformed — want cell|harness|substrate"
        RUN_CELLS+=("${_ec_cell}	${_ec_h}	${_ec_s}")
        log "extra cell queued: $_ec_cell (harness=$_ec_h substrate=$_ec_s)"
    done
fi

for _run_cell in "${RUN_CELLS[@]}"; do
        IFS=$'\t' read -r cell h s <<< "$_run_cell"

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

        # D1: local-model preflight — pi cells only. A wedged vLLM (0-byte/timeout/non-200)
        # is SKIP-loud (never green; fails --gate), not a slow run_failed discovered minutes
        # later. Always-on; the readiness of the model IS the precondition for a pi green.
        if [ "$h" = "pi" ]; then
            if pi_preflight; then
                log "preflight OK — pi endpoint answered ($PI_BASE_URL)"
            else
                log "SKIP  $cell — pi endpoint wedged/no response ($PI_BASE_URL) — restart vLLM on dgx"
                GRID+=("$cell	skip	pi endpoint wedged/no response")
                n_skip=$((n_skip+1))
                continue
            fi
        fi

        # run the cell: submit the seed bead through the scratch daemon's batch fold.
        # batch exits 0 iff every submitted bead reached a terminal pass; it always
        # writes a results artifact whose path we echo for T2/T3 to consume.
        batch_name="matrix-$(cell_slug "$cell")"
        log "RUN   $cell — batch '$batch_name' seed=$local_seed"

        # --assert: arm a FULL-type capture BEFORE submitting (no missed-event race).
        cap_pid=""; cap_file="$CAP_DIR/$(cell_slug "$cell").ndjson"
        if [ "$ASSERT" -eq 1 ]; then
            [ -x "$SCRATCH_BIN" ] || die "--assert: scratch binary not built ($SCRATCH_BIN)"
            "$SCRATCH_BIN" subscribe --socket "$SCRATCH_SOCK" --types "$CAP_TYPES" --heartbeat 30s \
                > "$cap_file" 2>/dev/null &
            cap_pid=$!
            # Reap the capture child on EVERY exit path, not only the assert-verdict block
            # below. A `die`, a `set -e` abort, or a Ctrl-C between here and that block
            # orphans a live `subscribe` that holds the daemon socket and keeps writing to
            # $cap_file. Single quotes are load-bearing: the trap body expands when it
            # fires, so it always reads the CURRENT cell's pid.
            trap 'kill "${cap_pid:-}" 2>/dev/null || true' EXIT INT TERM
        fi

        # The seed KEY that ties this cell to its fixture. Read BEFORE the git baseline below,
        # because the landing sentinel resolves through it, and before .seed_bead is
        # overwritten with the dispatched bead id in the spec build further down.
        spec_seed_key="$(jq -r --arg c "$cell" '.cells[]|select(.cell==$c)|.seed_bead // empty' "$SPECS" 2>/dev/null || true)"
        spec_harness="$(jq -r --arg c "$cell" '.cells[]|select(.cell==$c)|.harness // empty' "$SPECS" 2>/dev/null || true)"

        # D2: git landing baseline (record BEFORE submit). The intended branch is the cell
        # spec's expect.lands_on, which is the "@resolved" sentinel — resolved here from the
        # seed's target_branch, else from the daemon's own defaults.lands_on. We snapshot the
        # trunk + that branch tip so that AFTER the run we can prove from GIT which branch
        # actually advanced — the workspace_merge_status event is never emitted
        # (dead/aspirational), so the merge must be verified from the repo.
        #
        # The trunk snapshot used to read the literal `main`. The daemon never lands there in a
        # scratch: isolate_push_target points defaults.lands_on at scratch/main. A landing on
        # scratch/main therefore read as "nothing moved", which is a true verdict reached for a
        # false reason, and the next branch rename would have made it a wrong verdict.
        land_want=""; base_trunk=""; base_target=""
        if [ "$ASSERT" -eq 1 ] && command -v git >/dev/null 2>&1; then
            land_want="$(jq -r --arg c "$cell" '.cells[]|select(.cell==$c)|.expect.lands_on // empty' "$SPECS" 2>/dev/null || true)"
            if [ "$land_want" = "@resolved" ]; then
                [ -n "$spec_seed_key" ] \
                    || die "cell '$cell' says expect.lands_on = '@resolved' but names no seed_bead — nothing to resolve the landing branch from"
                land_want="$(seed_lands_on_for "$spec_seed_key")"
                [ -n "$land_want" ] \
                    || die "cell '$cell' seed '$spec_seed_key': could not resolve expect.lands_on — no target_branch on the seed and no defaults.lands_on to fall back to"
            fi
            base_trunk="$(git -C "$SCRATCH" rev-parse --verify -q "$TRUNK_BRANCH" 2>/dev/null || echo -)"
            [ -n "$land_want" ] && base_target="$(git -C "$SCRATCH" rev-parse --verify -q "$land_want" 2>/dev/null || echo -)"
        fi

        batch_out=""
        if batch_out="$("$SCRATCH_DAEMON" batch "$SCRATCH" "$batch_name" --beads "$local_seed" 2>&1)"; then
            batch_verdict="green"
        else
            batch_verdict="red"
        fi
        results_path="$(printf '%s\n' "$batch_out" | sed -n 's/.*results=\([^ ]*\).*/\1/p' | tail -1)"
        printf '%s\n' "$batch_out" | grep -E '^BATCH_(ITEM|SUMMARY)' || true

        # D2: recompute tips + derive the OBSERVED landing branch from git truth. Landed-on =
        # the branch whose tip advanced; if the TRUNK advanced while the cell asked for a
        # different branch that is always a fail (the trunk must NOT move). This observed
        # value is fed to the t10 assertion below.
        #
        # The trunk-advanced test is written second on purpose, so it wins. When a cell's own
        # target IS the trunk — the seed declares no target_branch — both lines set the same
        # name and the cell reads as a clean landing, which is right: that cell asked for the
        # project default and got it.
        observed_lands_on=""
        if [ "$ASSERT" -eq 1 ] && [ -n "$land_want" ] && command -v git >/dev/null 2>&1; then
            new_trunk="$(git -C "$SCRATCH" rev-parse --verify -q "$TRUNK_BRANCH" 2>/dev/null || echo -)"
            new_target="$(git -C "$SCRATCH" rev-parse --verify -q "$land_want" 2>/dev/null || echo -)"
            observed_lands_on="none"
            [ "$new_target" != "$base_target" ] && observed_lands_on="$land_want"
            [ "$new_trunk" != "$base_trunk" ] && observed_lands_on="$TRUNK_BRANCH"
            log "landing: want='$land_want' observed='$observed_lands_on' (trunk '$TRUNK_BRANCH' ${base_trunk:0:8}->${new_trunk:0:8}, target ${base_target:0:8}->${new_target:0:8})"
        fi

        # Determine the cell verdict. Without --assert it is the batch terminal outcome.
        # With --assert it is the assertion fold over the full captured stream.
        cell_verdict="$batch_verdict"; detail="${results_path:-no-artifact}"
        if [ "$ASSERT" -eq 1 ]; then
            [ -n "$cap_pid" ] && kill "$cap_pid" 2>/dev/null || true
            # resolve the cell spec, overriding seed_bead with the real dispatched id and
            # injecting the git-observed landing branch (D2) so assert_t10 compares intent
            # (expect.lands_on) against reality (._observed_lands_on).
            # Resolve the THREE sentinels the spec carries instead of literal names: the two
            # model ones, and expect.lands_on, which was resolved to $land_want at the git
            # baseline above and is written back here so assert_t10 reads a branch name rather
            # than the placeholder. ._trunk_branch rides along because t10 must be able to
            # NAME the branch it requires not to move.
            # spec_seed_key / spec_harness were read above, before .seed_bead is overwritten
            # with the dispatched bead id below — the key is what ties a cell to its fixture.
            want_model="$(seed_model_for "$spec_seed_key" "$spec_harness")"
            foreign_models="$(foreign_models_for "$spec_seed_key" "$spec_harness")"
            [ -n "$foreign_models" ] || foreign_models='[]'
            spec="$(jq -c --arg c "$cell" --arg sb "$local_seed" --arg obs "$observed_lands_on" \
                      --arg wm "$want_model" --argjson fm "$foreign_models" \
                      --arg lw "$land_want" --arg tb "$TRUNK_BRANCH" \
                      '.cells[] | select(.cell==$c) | .seed_bead=$sb | ._observed_lands_on=$obs
                       | ._trunk_branch=$tb
                       | if (.expect.lands_on? == "@resolved" and $lw != "")
                         then .expect.lands_on = $lw
                         else . end
                       | if (.expect.model_selected.model? == "@resolved")
                         then .expect.model_selected.model = (if $wm == "" then null else $wm end)
                         else . end
                       | if (.expect.model_selected.no_leak_models? == ["@foreign"])
                         then .expect.model_selected.no_leak_models = $fm
                         else . end' "$SPECS" 2>/dev/null || true)"
            if [ -z "$spec" ]; then
                cell_verdict="pending"; detail="no spec for cell in $SPECS"
            else
                # gap2 remote cells fold against the local cell's captured stream.
                ref="-"; [ "$s" = "remote" ] && [ -f "$CAP_DIR/$(cell_slug "${h}:local").ndjson" ] && ref="$CAP_DIR/$(cell_slug "${h}:local").ndjson"
                # A red cell's fold exits non-zero; capture rc WITHOUT tripping `set -e`
                # (a bare `x="$(cmd)"; rc=$?` aborts under errexit before rc is read).
                if fold_out="$(bash "$ASSERT_CELL" "$cap_file" "$spec" "$ref" 2>&1)"; then fold_rc=0; else fold_rc=$?; fi
                printf '%s\n' "$fold_out" | grep '^GAP' || true
                case "$fold_rc" in
                    0) cell_verdict="green" ;;
                    2) cell_verdict="pending" ;;
                    *) cell_verdict="red" ;;
                esac
                detail="$(printf '%s\n' "$fold_out" | grep '^CELL_VERDICT' | tail -1 || true)"
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
# Re-read the stamp of the binary that actually ran, and print it WITH the grid.
# The grid and the provenance used to live in different parts of the log, which is
# how a dirty binary got a green verdict quoted off it.
#
# The token is monotonic, so this read can only make the verdict worse. When the
# fresh read is clean and the verdict is not, read_provenance has already said why,
# and dumping its (clean) output here would read as a contradiction, so it is not.
if ! read_provenance && [ "$PROVENANCE_FRESH_TOKEN" != "clean" ]; then
    printf '%s\n' "${PROVENANCE_TEXT:-}" >&2
fi
echo "MATRIX_PROVENANCE $PROVENANCE_TOKEN ($PROVENANCE_LINE)"
if [ "$PROVENANCE_TOKEN" != "clean" ]; then
    echo "MATRIX_PROVENANCE VOID — no cell verdict above is an audit of the pinned commit; this run cannot pass."
fi
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

# ---- WS4-5: machine-readable per-cell grid (JSON) -------------------------
# Emitted LAST on stdout (marker `MATRIX_JSON `) so the assessor can fold the grid into
# its LT-leg verdict without scraping the human table. `all_green` is the forced-gate
# result; `gate` echoes whether --gate was in effect for this run.
if [ "$JSON" -eq 1 ]; then
    cells_json="$(
        for row in "${GRID[@]:-}"; do
            [ -n "$row" ] || continue
            IFS=$'\t' read -r cell verdict detail <<< "$row"
            jq -cn --arg c "$cell" --arg v "$verdict" --arg d "$detail" \
                '{cell:$c, verdict:$v, detail:$d}'
        done | jq -cs '.'
    )"
    all_green="false"
    # Provenance is a term of all_green, not a note beside it. The assessor folds
    # this field; a true here on a binary that cannot name its commit is the exact
    # false PASS hk-48zdw was filed for.
    [ "$n_red" -eq 0 ] && [ "$n_pending" -eq 0 ] && [ "$n_skip" -eq 0 ] \
        && [ "$PROVENANCE_TOKEN" = "clean" ] && all_green="true"
    jq -cn \
        --argjson cells "${cells_json:-[]}" \
        --argjson green "$n_green" --argjson red "$n_red" \
        --argjson pending "$n_pending" --argjson skip "$n_skip" \
        --argjson gate "$GATE" --argjson all_green "$all_green" \
        --arg prov "$PROVENANCE_TOKEN" --arg provline "$PROVENANCE_LINE" \
        '{summary:{green:$green, red:$red, pending:$pending, skip:$skip},
          gate:($gate==1), all_green:$all_green,
          provenance:{status:$prov, detail:$provline}, cells:$cells}' \
        | sed 's/^/MATRIX_JSON /'
fi

# ---- exit -----------------------------------------------------------------
# Default (lenient): non-zero on any red only. --gate (forced LT, WS4-5): non-zero unless
# EVERY cell is green — any red OR pending OR skip fails (the T9 zero-PENDING gate), so the
# assessor's forced-local LT leg never mistakes a partial matrix for a pass.
if [ "$GATE" -eq 1 ]; then
    [ "$n_red" -eq 0 ] && [ "$n_pending" -eq 0 ] && [ "$n_skip" -eq 0 ] \
        && [ "$PROVENANCE_TOKEN" = "clean" ]
else
    [ "$had_red" -eq 0 ]
fi
