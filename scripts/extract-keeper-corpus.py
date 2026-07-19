#!/usr/bin/env python3
# scripts/extract-keeper-corpus.py — build the keeper replay corpus + goldens
# from a frozen baseline event log (measurement-design §1; T9, SK-R10).
#
# Usage:
#   python3 scripts/extract-keeper-corpus.py \
#     .harmonik/events/baseline-2026-07-13/events.jsonl \
#     testdata/keeper-cycles/baseline-2026-07-13
#
# One deterministic pass over the frozen log. Produces, under OUT:
#   cycles/<agent>__<cycle_id>.jsonl          per-cycle event stream (EventID-sorted, D9)
#   cycles/<agent>__<cycle_id>.summary.json   per-cycle golden (schema: design §1.4)
#   manifest.json                             aggregate self-check (frozen anchors §1.4)
#   EXTRACT-LOG.md                            ledger: source, script sha, counts
#
# Cycle join key is the COMPOSITE (agent_name, cycle_id) — D7. cycle_id alone
# collides (476 distinct vs 507 composite). Keeper events carry no envelope
# run_id; the join is on payload.cycle_id + payload.agent_name exclusively.
#
# The extractor is granularity-agnostic: it slices on
# source_subsystem == "internal/keeper" && payload.cycle_id != null, which
# captures both the boundary cohort (pre-EV-U1: 2-3 events/cycle) and the
# interior cohort (post-EV-U1: 6-7 events/cycle) — re-run after the interior
# events ship to lift the corpus without structural rework (design §1.5).
import hashlib
import json
import os
import re
import sys

DEFAULT_SRC = ".harmonik/events/baseline-2026-07-13/events.jsonl"
DEFAULT_OUT = "testdata/keeper-cycles/baseline-2026-07-13"

SRC = sys.argv[1] if len(sys.argv) > 1 else DEFAULT_SRC
OUT = sys.argv[2] if len(sys.argv) > 2 else DEFAULT_OUT
CYC = os.path.join(OUT, "cycles")
os.makedirs(CYC, exist_ok=True)

TERMINALS = {"session_keeper_cycle_complete", "session_keeper_cycle_aborted"}
STARTED = "session_keeper_handoff_started"
UNCONF = "session_keeper_clear_unconfirmed"


def sanitize(s):
    return re.sub(r"[^A-Za-z0-9._-]", "_", s)


cycles = {}  # ckey -> list[event]  (log/arrival order; UUIDv7 event_id preserves it)
with open(SRC, "rb") as f:
    for raw in f:
        try:
            o = json.loads(raw)
        except json.JSONDecodeError:
            continue
        if o.get("source_subsystem") != "internal/keeper":
            continue
        p = o.get("payload") or {}
        cid, agent = p.get("cycle_id"), p.get("agent_name")
        if cid is None or agent is None:
            continue  # warns lack cycle_id -> excluded (strict)
        ckey = f"{agent}|{cid}"
        cycles.setdefault(ckey, []).append(o)


def ts(ev):
    return ev.get("timestamp_wall")


def eid(ev):
    return ev.get("event_id")


agg = {
    "started": 0,
    "complete": 0,
    "aborted": 0,
    "clear_unconfirmed": 0,
    "unterminated": 0,
    "abort_reasons": {},
    "count": 0,
}
for ckey in sorted(cycles):
    evs = cycles[ckey]
    evs.sort(key=lambda e: (eid(e) or ""))  # D9: sort by UUIDv7 event_id, not file order
    agent, cid = ckey.split("|", 1)
    types = [e["type"] for e in evs]
    started = STARTED in types
    term = next((t for t in types if t in TERMINALS), None)
    unconf = UNCONF in types
    reason = None
    if term == "session_keeper_cycle_aborted":
        reason = next(
            (e["payload"].get("reason") for e in evs if e["type"] == "session_keeper_cycle_aborted"),
            None,
        )
    outcome = (
        "complete"
        if term == "session_keeper_cycle_complete"
        else "aborted"
        if term == "session_keeper_cycle_aborted"
        else "unterminated"
    )
    start_ev = next((e for e in evs if e["type"] == STARTED), evs[0])
    term_ev = next((e for e in reversed(evs) if e["type"] in TERMINALS), None)
    summary = {
        "ckey": ckey,
        "agent_name": agent,
        "cycle_id": cid,
        "outcome": outcome,
        "abort_reason": reason,
        "clear_unconfirmed": unconf,
        "started_at": ts(start_ev),
        "terminal_at": ts(term_ev) if term_ev else None,
        "session_id_start": start_ev.get("payload", {}).get("session_id"),
        "event_count": len(evs),
        "types": types,
    }
    # Filename = composite key, each half sanitized, joined by "__" (§1.2
    # layout: skdog1__cyc-...; the raw "|" join key is kept in summary.ckey).
    base = os.path.join(CYC, sanitize(agent) + "__" + sanitize(cid))
    with open(base + ".jsonl", "w") as fh:
        for e in evs:
            fh.write(json.dumps(e, separators=(",", ":"), sort_keys=True) + "\n")
    with open(base + ".summary.json", "w") as fh:
        json.dump(summary, fh, indent=2, sort_keys=True)
        fh.write("\n")
    agg["count"] += 1
    agg["started"] += started
    if outcome == "complete":
        agg["complete"] += 1
    if outcome == "aborted":
        agg["aborted"] += 1
        agg["abort_reasons"][reason or ""] = agg["abort_reasons"].get(reason or "", 0) + 1
    if outcome == "unterminated":
        agg["unterminated"] += 1
    if unconf:
        agg["clear_unconfirmed"] += 1

with open(os.path.join(OUT, "manifest.json"), "w") as fh:
    json.dump(agg, fh, indent=2, sort_keys=True)
    fh.write("\n")

with open(os.path.abspath(__file__), "rb") as fh:
    script_sha = hashlib.sha256(fh.read()).hexdigest()
with open(os.path.join(OUT, "EXTRACT-LOG.md"), "w") as fh:
    fh.write(
        "# Keeper corpus extraction ledger\n\n"
        f"- **Source:** frozen baseline `{DEFAULT_SRC}` (read-only)\n"
        "- **Baseline date:** 2026-07-13\n"
        f"- **Extractor:** `scripts/extract-keeper-corpus.py` (sha256 `{script_sha}`)\n"
        "- **Join key:** composite `(agent_name, cycle_id)` (D7); events EventID-sorted (D9)\n"
        "- **Granularity:** boundary (pre-EV-U1 interior events); re-run after a new\n"
        "  baseline capture to lift to interior granularity (design §1.5)\n\n"
        "## Counts\n\n```json\n"
        + json.dumps(agg, indent=2, sort_keys=True)
        + "\n```\n"
    )

print(json.dumps(agg, indent=2, sort_keys=True))
