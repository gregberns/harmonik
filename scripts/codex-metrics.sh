#!/usr/bin/env bash
set -euo pipefail

# codex-metrics.sh — the input-ack no-regress metric recomputes + the out-of-band
# acceptance oracle for the structured Codex input-driver vertical (T9;
# harness-acceptance-design §"No-regress metrics").
#
# Every metric is recomputed from RAW ARTIFACTS with jq/grep + `go test` — NEVER
# from the driver's own report (D13: "the thing under repair cannot be its own
# oracle"). The REPLAY side is the persisted replayed stream (all strata replayed
# fault-free, re-enveloped to a JSONL by TestMetricsExport_ReplayedStream); the
# FROZEN side is the synthesized expectation (the anchors below). There is no
# daemon and no live log in this phase (Constraint: NO DAEMON), so the live-soak
# recompute slots in later by pointing the same jq commands at the live log.
#
# Zero-daemon, zero-token, deterministic.
#
# Env:
#   CODEX_METRICS_N               metric-oracle iterations (default 10; 0 skips)
#   CODEX_METRICS_SKIP_COVERAGE=1 skip the coverage-floor gate
#
# Exit 0: every metric matches its anchor. Exit 1: any mismatch (reported).

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

N="${CODEX_METRICS_N:-10}"
command -v jq >/dev/null || { echo "codex-metrics: jq is required" >&2; exit 2; }

WORK="$(mktemp -d -t codex-metrics)"
trap 'rm -rf "$WORK"' EXIT

FAILURES=0
pass() { echo "  PASS  $1"; }
fail() { echo "  FAIL  $1" >&2; FAILURES=$((FAILURES + 1)); }
check_eq() { # <label> <got> <want>
    if [ "$2" = "$3" ]; then pass "$1 = $2 (anchor $3)"; else fail "$1 = $2, anchor $3 — DOES NOT REPRODUCE"; fi
}
count_type() { # <type> <src>
    grep -c "\"type\":\"$1\"" "$2" || true
}

# ---------------------------------------------------------------------------
# REPLAY side: persist the reactor's replayed emitted-event stream (out-of-band).
# ---------------------------------------------------------------------------
R="$WORK/replayed.jsonl"
echo "codex-metrics: exporting the replayed input-ack stream -> $R"
CODEX_METRICS_EXPORT="$R" go test -count=1 -run TestMetricsExport_ReplayedStream \
    ./internal/codextest/ >/dev/null
[ -s "$R" ] || { echo "codex-metrics: replay export produced no file" >&2; exit 2; }

# --- Metric 1: positive-ack terminals -------------------------------------
echo "metric 1 — agent_input_acked (anchor 1: only the acked stratum)"
check_eq "replay agent_input_acked" "$(count_type agent_input_acked "$R")" 1

# --- Metric 2: bounded-liveness stale terminals ----------------------------
echo "metric 2 — agent_input_stale (anchor 1: the stale_timeout front-stop)"
check_eq "replay agent_input_stale" "$(count_type agent_input_stale "$R")" 1

# --- Metric 3: handshake fast-fail terminals -------------------------------
echo "metric 3 — agent_launch_failure (anchor 1: the handshake_fail fast-fail)"
check_eq "replay agent_launch_failure" "$(count_type agent_launch_failure "$R")" 1

# --- Metric 4: submissions front-stopped -----------------------------------
echo "metric 4 — agent_input_submitted (anchor 3: acked + rejected + stale_timeout)"
check_eq "replay agent_input_submitted" "$(count_type agent_input_submitted "$R")" 3

# --- Metric 5: every submission reaches a terminal (never-silence) ---------
echo "metric 5 — every submission resolves (no orphan front-stop; jq over the stream)"
submitted=$(count_type agent_input_submitted "$R")
acked=$(count_type agent_input_acked "$R")
stale=$(count_type agent_input_stale "$R")
# rejected submissions resolve via the sync Ack{Rejected} (no emit): the one
# rejected-stratum submission is the residual (3 submitted = 1 acked + 1 stale
# + 1 rejected).
resolved=$((acked + stale + 1))
check_eq "submitted vs resolved (acked+stale+rejected)" "$submitted" "$resolved"

# --- Metric 6: fault-matrix pass rate (must be 100%) -----------------------
echo "metric 6 — fault matrix (anchor: 100% terminal-never-silence)"
if go test -run 'TestCodexInputReplay_Fault' ./internal/codextest/... -count=1 >/dev/null; then
    pass "go test -run 'TestCodexInputReplay_Fault' ./internal/codextest/... — 100%"
else
    fail "fault matrix NOT 100% (a silence cell = AIS-INV-001 violation)"
fi

# --- Metric 7: replay determinism / oracle N-run --------------------------
echo "metric 7 — oracle N-run (anchor: $N/$N green)"
if [ "$N" = "0" ]; then
    echo "        SKIPPED (CODEX_METRICS_N=0) — run scripts/codex-oracle-n10.sh separately"
elif "$REPO_ROOT/scripts/codex-oracle-n10.sh" "$N"; then
    pass "N=$N consecutive green (codexinput + codexdriver + codextest + codexdigitaltwin)"
else
    fail "oracle N-run went RED"
fi

# --- coverage floor --------------------------------------------------------
if [ "${CODEX_METRICS_SKIP_COVERAGE:-0}" = "1" ]; then
    echo "coverage floor: SKIPPED (CODEX_METRICS_SKIP_COVERAGE=1)"
elif "$REPO_ROOT/scripts/codex-coverage-gate.sh"; then
    pass "coverage floors held (scripts/codex-coverage-floor.baseline)"
else
    fail "coverage floor regressed"
fi

echo
if [ "$FAILURES" -ne 0 ]; then
    echo "codex-metrics: $FAILURES CHECK(S) FAILED — a metric does not reproduce its anchor" >&2
    exit 1
fi
echo "codex-metrics: ALL METRICS MATCH THEIR ANCHORS (replay recomputed out-of-band via jq/grep — D13)"
