#!/usr/bin/env bash
set -euo pipefail

# keeper-metrics.sh — the 9 no-regress metric recomputes + the out-of-band
# acceptance oracle for the keeper vertical (T13; measurement-design §6 + §7).
#
# Every metric is recomputed from raw artifacts with jq/grep/sort/comm +
# `go test` — NEVER from the keeper's own report (D13: "the thing under
# repair cannot be its own oracle"). Zero-daemon, zero-token, deterministic.
#
# Two measured sides per applicable metric:
#   FROZEN — jq/grep over the read-only frozen baseline log
#            ($KEEPER_BASELINE); must be BIT-IDENTICAL to the §7 anchors.
#   REPLAY — jq/grep over the NEW reactor's persisted replayed stream
#            (all 507 corpus cycles re-enveloped to an events.jsonl by
#            TestMetricsExport_ReplayedStream); must match the SR9-SHIFTED
#            anchors: the 1 recorded unterminated cycle now terminates as a
#            bounded degraded completion (427→428 complete, 347→348
#            degraded, 1→0 unterminated — measurement-design §4 required
#            divergence).
#
# ADAPTATION (Constraint: NO DAEMON): §7 prescribes recomputing the live
# side against `.harmonik/events/events.jsonl` after a dogfood soak. There
# is no daemon and no live log in this phase, so the post-change side is the
# replayed corpus stream (the same jq commands over a persisted events.jsonl
# — a different code path from the reactor, per §6.3). The live-soak
# recompute slots in later by pointing the same commands at the live log.
#
# Metric-2 headline discipline (§7): degraded-completion must NOT RISE.
# The gate asserts exact match to the SR9-shifted anchor (348/428 = 81.3%);
# it deliberately does NOT assert improvement — the rebuild characterizes
# the baseline; driving the number DOWN is a later, separately-measured
# change.
#
# Usage:
#   KEEPER_BASELINE=<path-to-frozen-events.jsonl> scripts/keeper-metrics.sh
# Env:
#   KEEPER_BASELINE         frozen log (default:
#                           $REPO_ROOT/.harmonik/events/baseline-2026-07-13/events.jsonl)
#   KEEPER_METRICS_N        metric-9 oracle iterations (default 10; 0 skips —
#                           run scripts/keeper-oracle-n10.sh separately)
#   KEEPER_METRICS_SKIP_COVERAGE=1   skip the §6.4 coverage-floor gate
#
# Exit 0: every metric matches its anchor. Exit 1: any mismatch (reported).

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

B="${KEEPER_BASELINE:-$REPO_ROOT/.harmonik/events/baseline-2026-07-13/events.jsonl}"
N="${KEEPER_METRICS_N:-10}"
MANIFEST="$REPO_ROOT/testdata/keeper-cycles/baseline-2026-07-13/manifest.json"

command -v jq >/dev/null || { echo "keeper-metrics: jq is required" >&2; exit 2; }
[ -r "$B" ] || {
    echo "keeper-metrics: frozen baseline log not readable: $B" >&2
    echo "  (set KEEPER_BASELINE=<path>; the frozen log is read-only and not in git)" >&2
    exit 2
}
[ -r "$MANIFEST" ] || { echo "keeper-metrics: missing corpus manifest: $MANIFEST" >&2; exit 2; }

WORK="$(mktemp -d -t keeper-metrics)"
trap 'rm -rf "$WORK"' EXIT

FAILURES=0
pass() { echo "  PASS  $1"; }
fail() { echo "  FAIL  $1" >&2; FAILURES=$((FAILURES + 1)); }
check_eq() { # <label> <got> <want>
    if [ "$2" = "$3" ]; then pass "$1 = $2 (anchor $3)"; else fail "$1 = $2, anchor $3 — DOES NOT REPRODUCE"; fi
}

# Composite-key extraction (D7: (agent_name, cycle_id)) for one event type.
keys_of() { # <type> <src> -> sorted unique key list on stdout
    grep "\"type\":\"$1\"" "$2" | jq -r '"\(.payload.agent_name)|\(.payload.cycle_id)"' | sort -u
}
count_of() { # <type> <src> -> raw line count (grep -c exits 1 on zero: guard)
    grep -c "\"type\":\"$1\"" "$2" || true
}

rate() { awk -v n="$1" -v d="$2" 'BEGIN { printf "%.1f", 100 * n / d }'; }

# ---------------------------------------------------------------------------
# REPLAY side: persist the NEW reactor's replayed stream (out-of-band file).
# ---------------------------------------------------------------------------
R="$WORK/replayed.jsonl"
echo "keeper-metrics: exporting the replayed stream (507 corpus cycles -> $R)"
KEEPER_METRICS_EXPORT="$R" go test -count=1 -run TestMetricsExport_ReplayedStream \
    ./internal/keepertest/ >/dev/null
[ -s "$R" ] || { echo "keeper-metrics: replay export produced no file" >&2; exit 2; }

keys_of session_keeper_handoff_started "$B" > "$WORK/f_started"
keys_of session_keeper_cycle_complete "$B" > "$WORK/f_complete"
keys_of session_keeper_cycle_aborted "$B" > "$WORK/f_aborted"
keys_of session_keeper_clear_unconfirmed "$B" > "$WORK/f_unconf"
keys_of session_keeper_handoff_started "$R" > "$WORK/r_started"
keys_of session_keeper_cycle_complete "$R" > "$WORK/r_complete"
keys_of session_keeper_cycle_aborted "$R" > "$WORK/r_aborted"
keys_of session_keeper_clear_unconfirmed "$R" > "$WORK/r_unconf"

f_started=$(wc -l < "$WORK/f_started" | tr -d ' ')
f_complete=$(wc -l < "$WORK/f_complete" | tr -d ' ')
f_unconf=$(comm -12 "$WORK/f_unconf" "$WORK/f_complete" | wc -l | tr -d ' ')
r_started=$(wc -l < "$WORK/r_started" | tr -d ' ')
r_complete=$(wc -l < "$WORK/r_complete" | tr -d ' ')
r_aborted=$(wc -l < "$WORK/r_aborted" | tr -d ' ')
r_unconf=$(comm -12 "$WORK/r_unconf" "$WORK/r_complete" | wc -l | tr -d ' ')

# --- Metric 1: restart-completion (complete/started, composite key) --------
echo "metric 1 — restart-completion (frozen anchor 427/507 = 84.2%; must not drop)"
check_eq "frozen started" "$f_started" 507
check_eq "frozen complete" "$f_complete" 427
check_eq "frozen rate%" "$(rate "$f_complete" "$f_started")" 84.2
check_eq "replay started" "$r_started" 507
check_eq "replay complete (SR9-shifted: +1 fixed cycle)" "$r_complete" 428
if [ $((r_complete * f_started)) -ge $((f_complete * r_started)) ]; then
    pass "replay rate $(rate "$r_complete" "$r_started")% >= frozen 84.2% (did not drop)"
else
    fail "replay rate $(rate "$r_complete" "$r_started")% DROPPED below frozen 84.2%"
fi

# --- Metric 2: degraded-completion (HEADLINE; must not rise) ----------------
echo "metric 2 — degraded-completion clear_unconfirmed/complete (frozen anchor 347/427 = 81.3%; must NOT rise; target DOWN, floor not-worse)"
check_eq "frozen clear_unconfirmed (over completes)" "$f_unconf" 347
check_eq "frozen rate%" "$(rate "$f_unconf" "$f_complete")" 81.3
check_eq "replay clear_unconfirmed (SR9-shifted: +1, the fixed cycle terminates degraded)" "$r_unconf" 348
# No-rise gate on the SR9-shifted basis: 348/428 is the shifted anchor.
if [ $((r_unconf * 428)) -le $((348 * r_complete)) ]; then
    pass "replay rate $(rate "$r_unconf" "$r_complete")% <= shifted anchor 81.3% (did not rise)"
else
    fail "replay rate $(rate "$r_unconf" "$r_complete")% ROSE above the shifted anchor 81.3%"
fi

# --- Metric 3: unterminated cycles (SR9) — 1 recorded -> 0 after rebuild ---
echo "metric 3 — unterminated cycles (frozen anchor 1; replay MUST be 0 — the SR9 fix)"
check_eq "frozen (manifest reconciliation)" "$(jq -r .unterminated "$MANIFEST")" 1
sort -u "$WORK/r_complete" "$WORK/r_aborted" > "$WORK/r_terminated"
r_unterminated=$(comm -23 "$WORK/r_started" "$WORK/r_terminated" | wc -l | tr -d ' ')
check_eq "replay (started keys with no terminal)" "$r_unterminated" 0

# --- Metric 4: aborts always explicit-reasoned ------------------------------
echo "metric 4 — aborts explicit-reasoned (frozen anchor 79/79 handoff_timeout; no empty reason)"
f_abort_reasons=$(grep '"type":"session_keeper_cycle_aborted"' "$B" | jq -r '.payload.reason' | sort | uniq -c | awk '{$1=$1; print}' | tr '\n' ';')
check_eq "frozen reason buckets" "$f_abort_reasons" "79 handoff_timeout;"
f_empty=$(grep '"type":"session_keeper_cycle_aborted"' "$B" | jq -r '.payload.reason // ""' | grep -c '^$' || true)
check_eq "frozen empty-reason aborts" "$f_empty" 0
check_eq "replay aborted keys" "$r_aborted" 79
r_empty=$(grep '"type":"session_keeper_cycle_aborted"' "$R" | jq -r '.payload.reason // ""' | grep -c '^$' || true)
check_eq "replay empty-reason aborts" "$r_empty" 0

# --- Metric 5: terminal exclusivity + no dup terminals/cycle ----------------
echo "metric 5 — terminal exclusivity + dup terminals (frozen anchor 0 overlaps, 0 dups; stays 0)"
term_stats() { # <src> -> "<overlaps> <dups>"
    grep -E '"type":"session_keeper_cycle_(complete|aborted)"' "$1" \
        | jq -r '"\(.payload.agent_name)|\(.payload.cycle_id)\t\(.type)"' | sort > "$WORK/terms"
    overlaps=$(sort -u "$WORK/terms" | cut -f1 | uniq -d | wc -l | tr -d ' ')
    dups=$(cut -f1 "$WORK/terms" | uniq -c | awk '$1 > 1' | wc -l | tr -d ' ')
    echo "$overlaps $dups"
}
set -- $(term_stats "$B"); check_eq "frozen overlaps" "$1" 0; check_eq "frozen dup-terminal keys" "$2" 0
set -- $(term_stats "$R"); check_eq "replay overlaps" "$1" 0; check_eq "replay dup-terminal keys" "$2" 0

# --- Metric 6: interior ordering SR3/SR4/SR6 (post-change only) -------------
echo "metric 6 — interior ordering (frozen: types ABSENT; replay: handoff_written < model_done < clear_sent [< new_session_up] < terminal per key)"
check_eq "frozen model_done count (absent pre-change)" "$(count_of session_keeper_model_done "$B")" 0
order_violations=$(jq -s '
    def idx(t): (map(.type) | index(t));
    group_by(.payload.agent_name + "|" + .payload.cycle_id)
    | map(select(idx("session_keeper_clear_sent") != null))
    | map(select(
        (   idx("session_keeper_handoff_written") != null
        and idx("session_keeper_model_done") != null
        and idx("session_keeper_handoff_written") < idx("session_keeper_model_done")
        and idx("session_keeper_model_done") < idx("session_keeper_clear_sent")
        and (idx("session_keeper_cycle_complete") // -1) > idx("session_keeper_clear_sent")
        and (   idx("session_keeper_new_session_up") == null
             or (   idx("session_keeper_new_session_up") > idx("session_keeper_clear_sent")
                and idx("session_keeper_new_session_up") < (idx("session_keeper_cycle_complete") // -1)))
        ) | not))
    | length' "$R")
check_eq "replay ordering violations (jq over persisted stream)" "$order_violations" 0
# Same invariants through the independent typed-decode path (internal/replay
# SR3/SR4/SR6/SR7/SR9 checkers over a re-enveloped stream):
if go test -count=1 -run 'TestL1_ReplayedStreamInvariants' ./internal/keepertest/ >/dev/null; then
    pass "internal/replay SR3/SR4/SR6/SR7/SR9 checkers: zero violations"
else
    fail "internal/replay checker run over the replayed stream FAILED"
fi

# --- Metric 7: fleet run-completion (context guard; informational) ----------
echo "metric 7 — fleet run-completion (frozen anchor 1155/2142 = 53.9%; informational floor)"
check_eq "frozen run_started" "$(count_of run_started "$B")" 2142
check_eq "frozen run_completed" "$(count_of run_completed "$B")" 1155
echo "        (keeper is per-session; no daemon in this phase, so no post-change fleet side to recompute — Constraint: NO DAEMON)"

# --- Metric 8: fault-matrix pass rate (must be 100%) -------------------------
echo "metric 8 — fault matrix (anchor: 100% terminal-never-silence)"
if go test -run 'TestKeeperReplay_Fault' ./internal/keepertest/... -count=1 >/dev/null; then
    pass "go test -run 'TestKeeperReplay_Fault' ./internal/keepertest/... -count=1 — 100%"
else
    fail "fault matrix NOT 100% (a silence cell = SR9 violation)"
fi

# --- Metric 9: replay determinism / oracle N-run -----------------------------
echo "metric 9 — oracle N-run (anchor: $N/$N green)"
if [ "$N" = "0" ]; then
    echo "        SKIPPED (KEEPER_METRICS_N=0) — run scripts/keeper-oracle-n10.sh separately"
elif "$REPO_ROOT/scripts/keeper-oracle-n10.sh" "$N"; then
    pass "N=$N consecutive green (keeper + keepertest + keepertwin)"
else
    fail "oracle N-run went RED"
fi

# --- §6.4: measured coverage floor -------------------------------------------
if [ "${KEEPER_METRICS_SKIP_COVERAGE:-0}" = "1" ]; then
    echo "coverage floor: SKIPPED (KEEPER_METRICS_SKIP_COVERAGE=1)"
elif "$REPO_ROOT/scripts/keeper-coverage-gate.sh"; then
    pass "coverage floors held (scripts/keeper-coverage-floor.baseline)"
else
    fail "coverage floor regressed"
fi

echo
if [ "$FAILURES" -ne 0 ]; then
    echo "keeper-metrics: $FAILURES CHECK(S) FAILED — a metric does not reproduce its anchor" >&2
    exit 1
fi
echo "keeper-metrics: ALL METRICS MATCH THEIR ANCHORS (frozen bit-identical; replay = SR9-shifted; no-regress holds)"
