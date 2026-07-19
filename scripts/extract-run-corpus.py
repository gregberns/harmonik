#!/usr/bin/env python3
# scripts/extract-run-corpus.py — build the daemon run-lifecycle replay corpus +
# goldens from a frozen baseline event log (liveness-parity-design §3; RT10).
#
# Daemon peer of scripts/extract-keeper-corpus.py. Where the keeper extractor
# joins on the (agent_name, cycle_id) composite, this one joins on the
# run-lifecycle run_id carried in payloads (RunStarted/RunCompleted/RunFailed/
# LaunchInitiated/AgentReady/AgentReadyTimeout/ImplementerResumed/
# ReviewLoopCycleComplete/OutcomeEmitted/RunStale). run_stale carries run_id as a
# string; the others as a UUID string — both render identically here.
#
# Usage:
#   python3 scripts/extract-run-corpus.py \
#     <SRC events.jsonl> <OUT dir>
# Defaults reproduce the committed baseline:
#   python3 scripts/extract-run-corpus.py
#
# One deterministic pass over the frozen log. Produces, under OUT:
#   runs/<run_id>.jsonl          per-run event stream (EventID-sorted, D9)
#   runs/<run_id>.summary.json   per-run golden (see SUMMARY_KEYS)
#   manifest.json                aggregate self-check incl. strata counts (D13)
#   EXTRACT-LOG.md               ledger: source, script sha, counts
#
# Strata (liveness-parity-design §3): single | review-loop-resume | dot |
# merge-failure | run-stale | hung-relaunch. Inferred from terminal type, mode,
# and resume presence; pinned in the manifest at extraction (P1 anchor-pinning).
import hashlib
import json
import os
import re
import sys

DEFAULT_SRC = "testdata/daemon-runs/baseline-2026-07-14/source/events.jsonl"
DEFAULT_OUT = "testdata/daemon-runs/baseline-2026-07-14"

SRC = sys.argv[1] if len(sys.argv) > 1 else DEFAULT_SRC
OUT = sys.argv[2] if len(sys.argv) > 2 else DEFAULT_OUT
RUNS = os.path.join(OUT, "runs")
os.makedirs(RUNS, exist_ok=True)

# Run-lifecycle event families.
RUN_TERMINALS = {"run_completed", "run_failed", "review_loop_cycle_complete"}
FAILURE_CLASS = {"agent_ready_timeout", "run_stale"}
RESUMED = "implementer_resumed"


def sanitize(s):
    return re.sub(r"[^A-Za-z0-9._-]", "_", s)


def run_id_of(payload):
    rid = payload.get("run_id")
    if rid in (None, "", "00000000-0000-0000-0000-000000000000"):
        return None
    return rid


def stratum_of(terminal, mode, resumed, fail_reason):
    if terminal == "agent_ready_timeout":
        return "hung-relaunch"
    if terminal == "run_stale":
        return "run-stale"
    if mode == "dot":
        return "dot"
    if terminal == "run_failed" and "merge" in (fail_reason or "").lower():
        return "merge-failure"
    if resumed:
        return "review-loop-resume"
    return "single"


runs = {}  # run_id -> list[event]  (arrival order; UUIDv7 event_id preserves it)
with open(SRC, "rb") as f:
    for raw in f:
        try:
            o = json.loads(raw)
        except json.JSONDecodeError:
            continue
        p = o.get("payload") or {}
        rid = run_id_of(p)
        if rid is None:
            continue  # events with no joinable run_id are excluded (strict)
        runs.setdefault(rid, []).append(o)


def eid(ev):
    return ev.get("event_id") or ""


def ts(ev):
    return ev.get("timestamp_wall")


agg = {
    "count": 0,
    "resumed": 0,
    "terminated": 0,
    "unterminated": 0,
    "failure_class": 0,
    "strata": {},
    "terminals": {},
}

for rid in sorted(runs):
    evs = runs[rid]
    evs.sort(key=eid)  # D9: sort by UUIDv7 event_id, not file order
    types = [e["type"] for e in evs]
    terminal = next((t for t in types if t in RUN_TERMINALS), None)
    fail_evt = next((e for e in evs if e["type"] in FAILURE_CLASS), None)
    fail_class = fail_evt["type"] if fail_evt else None
    # A run's "terminal_type" is the run terminal if present, else the
    # failure-class event (agent_ready_timeout / run_stale), else None.
    terminal_type = terminal or fail_class
    resumed = RESUMED in types
    mode = next(
        (e["payload"].get("workflow_mode") for e in evs if e.get("payload", {}).get("workflow_mode")),
        None,
    )
    fail_reason = next(
        (e["payload"].get("reason") for e in evs if e["type"] == "run_failed"),
        None,
    )
    stratum = stratum_of(terminal_type, mode, resumed, fail_reason)
    iterations = max(
        [e["payload"].get("iteration_count", 0) for e in evs if "iteration_count" in e.get("payload", {})]
        + [e["payload"].get("final_iteration_count", 0) for e in evs if "final_iteration_count" in e.get("payload", {})]
        + [0]
    )
    merge_outcome = next(
        (e["payload"].get("merge_outcome") for e in evs if e.get("payload", {}).get("merge_outcome")),
        None,
    )
    summary = {
        "run_id": rid,
        "stratum": stratum,
        "terminal_type": terminal_type,
        "outcome": terminal_type or "unterminated",
        "mode": mode,
        "iterations": iterations,
        "resumed": resumed,
        "merge_outcome": merge_outcome,
        "event_count": len(evs),
        "types": types,
        "started_at": ts(evs[0]),
        "terminal_at": ts(evs[-1]) if terminal_type else None,
    }
    base = os.path.join(RUNS, sanitize(rid))
    with open(base + ".jsonl", "w") as fh:
        for e in evs:
            fh.write(json.dumps(e, separators=(",", ":"), sort_keys=True) + "\n")
    with open(base + ".summary.json", "w") as fh:
        json.dump(summary, fh, indent=2, sort_keys=True)
        fh.write("\n")

    agg["count"] += 1
    agg["resumed"] += int(resumed)
    if terminal:
        agg["terminated"] += 1
    elif fail_class is None:
        agg["unterminated"] += 1
    if fail_class:
        agg["failure_class"] += 1
    agg["strata"][stratum] = agg["strata"].get(stratum, 0) + 1
    if terminal_type:
        agg["terminals"][terminal_type] = agg["terminals"].get(terminal_type, 0) + 1

with open(os.path.join(OUT, "manifest.json"), "w") as fh:
    json.dump(agg, fh, indent=2, sort_keys=True)
    fh.write("\n")

with open(os.path.abspath(__file__), "rb") as fh:
    script_sha = hashlib.sha256(fh.read()).hexdigest()
with open(os.path.join(OUT, "EXTRACT-LOG.md"), "w") as fh:
    fh.write(
        "# Daemon run corpus extraction ledger\n\n"
        f"- **Source:** frozen baseline `{SRC}` (read-only)\n"
        "- **Baseline date:** 2026-07-14\n"
        f"- **Extractor:** `scripts/extract-run-corpus.py` (sha256 `{script_sha}`)\n"
        "- **Join key:** payload `run_id` (run-lifecycle track); events EventID-sorted (D9)\n"
        "- **Strata:** single | review-loop-resume | dot | merge-failure | run-stale | hung-relaunch\n\n"
        "## Counts\n\n```json\n"
        + json.dumps(agg, indent=2, sort_keys=True)
        + "\n```\n"
    )

print(json.dumps(agg, indent=2, sort_keys=True))
