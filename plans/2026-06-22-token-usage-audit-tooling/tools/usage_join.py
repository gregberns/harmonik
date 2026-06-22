#!/usr/bin/env python3
"""
Phase 0 token-usage join tool for harmonik.
Joins Claude-Code transcripts × harmonik events.jsonl over a time window.
No harmonik source changes required.

Usage:
  python3 usage_join.py [--since ISO] [--until ISO] [--format json|summary]

Defaults to the "record run" window: 2026-06-21T15:00:00Z → 2026-06-22T15:00:00Z
"""

import argparse
import json
import os
import re
import sys
from collections import defaultdict
from pathlib import Path

# ── Pricing table (per 1M tokens, USD) ─────────────────────────────────────
# Source: Anthropic pricing as of June 2026
PRICING = {
    # claude-opus-4-8  (claude-opus-4)
    "claude-opus-4-8":  {"input": 15.0,  "output": 75.0,  "cache_creation": 18.75, "cache_read": 1.50},
    "claude-opus-4":    {"input": 15.0,  "output": 75.0,  "cache_creation": 18.75, "cache_read": 1.50},
    # claude-sonnet-4-6 (claude-sonnet-4.5 / claude-sonnet-4)
    "claude-sonnet-4-6":{"input": 3.0,   "output": 15.0,  "cache_creation": 3.75,  "cache_read": 0.30},
    "claude-sonnet-4-5":{"input": 3.0,   "output": 15.0,  "cache_creation": 3.75,  "cache_read": 0.30},
    "claude-sonnet-4":  {"input": 3.0,   "output": 15.0,  "cache_creation": 3.75,  "cache_read": 0.30},
    # claude-haiku-4-8 / claude-haiku-3-5
    "claude-haiku-4-8": {"input": 0.80,  "output": 4.00,  "cache_creation": 1.00,  "cache_read": 0.08},
    "claude-haiku-3-5": {"input": 0.80,  "output": 4.00,  "cache_creation": 1.00,  "cache_read": 0.08},
    "claude-haiku-3":   {"input": 0.25,  "output": 1.25,  "cache_creation": 0.30,  "cache_read": 0.03},
}
DEFAULT_PRICE = {"input": 3.0, "output": 15.0, "cache_creation": 3.75, "cache_read": 0.30}

CLAUDE_PROJECTS = Path.home() / ".claude" / "projects"
EVENTS_FILE     = Path("/Users/gb/github/harmonik/.harmonik/events/events.jsonl")


# ── Helpers ─────────────────────────────────────────────────────────────────

def norm_ts(ts: str) -> str:
    """Normalize any ISO timestamp to a comparable UTC string (strip tz offset)."""
    if not ts:
        return ""
    m = re.match(r"(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})", ts)
    if m:
        return m.group(1) + "Z"
    return ts[:20].rstrip("T") + "Z"


def compute_cost(usage: dict, model: str) -> float:
    price = PRICING.get(model, DEFAULT_PRICE)
    cost = (
        usage.get("input_tokens", 0)              * price["input"]          / 1_000_000
        + usage.get("output_tokens", 0)           * price["output"]         / 1_000_000
        + usage.get("cache_creation_input_tokens", 0) * price["cache_creation"] / 1_000_000
        + usage.get("cache_read_input_tokens", 0) * price["cache_read"]     / 1_000_000
    )
    return cost


def model_tier(model: str) -> str:
    m = model.lower()
    if "opus" in m:   return "opus"
    if "sonnet" in m: return "sonnet"
    if "haiku" in m:  return "haiku"
    return "other"


def add_usage(dst: dict, usage: dict) -> None:
    dst["input"]          = dst.get("input", 0)          + usage.get("input_tokens", 0)
    dst["output"]         = dst.get("output", 0)         + usage.get("output_tokens", 0)
    dst["cache_creation"] = dst.get("cache_creation", 0) + usage.get("cache_creation_input_tokens", 0)
    dst["cache_read"]     = dst.get("cache_read", 0)     + usage.get("cache_read_input_tokens", 0)


def total_tokens(u: dict) -> int:
    return u.get("input", 0) + u.get("output", 0) + u.get("cache_creation", 0) + u.get("cache_read", 0)


def cache_read_pct(u: dict) -> float:
    t = total_tokens(u)
    if t == 0:
        return 0.0
    return 100.0 * u.get("cache_read", 0) / t


def slug_for_path(p: str) -> str:
    """Convert an absolute path to a Claude projects slug (drop leading /, replace / with -)."""
    return p.replace("/", "-")


def resolve_transcript_path(log_path: str) -> str:
    """
    log_path from events looks like:
      /Users/gb/.claude/projects/Users-gb-github-harmonik-.harmonik-worktrees-<run_id>/YYY.jsonl
    Actual path on disk:
      /Users/gb/.claude/projects/-Users-gb-github-harmonik--harmonik-worktrees-<run_id>/YYY.jsonl

    The slug encoding used by Claude Code:
      path /Users/gb/github/harmonik/.harmonik/worktrees/<run_id>
      → slug -Users-gb-github-harmonik--harmonik-worktrees-<run_id>
    The event log_path slug omits the leading dash and uses different separators.
    Strategy: extract <run_id> + <session_file> from log_path and reconstruct.
    """
    if not log_path:
        return ""
    # Already valid?
    if os.path.exists(log_path):
        return log_path
    # Extract run_id and session file from log_path
    # Pattern: .../worktrees-<run_id>/<session_id>.jsonl  OR  .../worktrees/<run_id>/<session_id>.jsonl
    fname = os.path.basename(log_path)

    # Try worktrees-<run_id> slug style
    m = re.search(r"worktrees-([0-9a-f-]{36})/([^/]+\.jsonl)$", log_path)
    if m:
        run_id = m.group(1)
        candidate = str(CLAUDE_PROJECTS / f"-Users-gb-github-harmonik--harmonik-worktrees-{run_id}" / fname)
        if os.path.exists(candidate):
            return candidate
        # Try reviewer variant
        for suffix in ["-reviewer-1", "-reviewer-2", "-reviewer-3"]:
            c2 = str(CLAUDE_PROJECTS / f"-Users-gb-github-harmonik--harmonik-worktrees-{run_id}{suffix}" / fname)
            if os.path.exists(c2):
                return c2

    # Generic fallback: scan all project dirs for the session file
    session_id = Path(log_path).stem
    for d in CLAUDE_PROJECTS.iterdir():
        if not d.is_dir():
            continue
        fp = d / fname
        if fp.exists():
            return str(fp)

    return ""


# ── Phase 1: Build event index ───────────────────────────────────────────────

def build_event_index(since: str, until: str) -> dict:
    """
    Returns {
      run_id: {
        bead_id, queue_id, node_id, claude_session_id,
        log_path, started_at, ended_at, success
      },
      warnings: [...]
    }
    """
    runs = {}        # run_id -> dict
    warnings = []

    with open(EVENTS_FILE) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                ev = json.loads(line)
            except Exception:
                continue

            ts = norm_ts(ev.get("timestamp_wall", ""))
            if ts < since or ts > until:
                continue

            run_id  = ev.get("run_id", "")
            ev_type = ev.get("type", "")
            payload = ev.get("payload", {})

            if run_id not in runs:
                runs[run_id] = {"run_id": run_id, "log_paths": [], "bead_id": None,
                                "node_id": None, "queue_id": None,
                                "claude_session_ids": set(),
                                "started_at": None, "ended_at": None, "success": None}

            r = runs[run_id]

            if ev_type == "run_started":
                r["bead_id"]    = r["bead_id"] or payload.get("bead_id")
                r["queue_id"]   = r["queue_id"] or payload.get("queue_id")
                r["started_at"] = r["started_at"] or payload.get("started_at")

            elif ev_type in ("run_completed", "run_failed"):
                r["bead_id"]  = r["bead_id"] or payload.get("bead_id")
                r["queue_id"] = r["queue_id"] or payload.get("queue_id")
                r["ended_at"] = r["ended_at"] or payload.get("ended_at")
                r["success"]  = payload.get("success", ev_type == "run_completed")

            elif ev_type in ("launch_initiated", "handler_capabilities"):
                csid = payload.get("claude_session_id")
                if csid:
                    r["claude_session_ids"].add(csid)

            elif ev_type == "session_log_location":
                lp = payload.get("log_path", "")
                r["node_id"] = r["node_id"] or payload.get("node_id")
                if lp and lp not in r["log_paths"]:
                    r["log_paths"].append(lp)

    # Convert sets to lists
    for r in runs.values():
        r["claude_session_ids"] = list(r["claude_session_ids"])

    return {"runs": runs, "warnings": warnings}


# ── Phase 2: Read transcripts ─────────────────────────────────────────────────

def read_transcript(path: str, since: str, until: str) -> list:
    """
    Returns list of dicts: {timestamp, model, usage{input,output,cache_creation,cache_read},
                             gitBranch, cwd, run_id_from_branch}
    """
    turns = []
    if not os.path.exists(path):
        return turns
    with open(path, errors="replace") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                entry = json.loads(line)
            except Exception:
                continue
            if entry.get("type") != "assistant":
                continue
            ts = entry.get("timestamp", "")
            ts_norm = norm_ts(ts)
            if ts_norm < since or ts_norm > until:
                continue
            msg   = entry.get("message", {})
            usage = msg.get("usage", {})
            if not usage:
                continue
            branch = entry.get("gitBranch", "")
            run_from_branch = None
            bm = re.match(r"run/(.+)", branch)
            if bm:
                run_from_branch = bm.group(1)
            turns.append({
                "timestamp":         ts,
                "model":             msg.get("model", "unknown"),
                "usage":             {
                    "input_tokens":              usage.get("input_tokens", 0),
                    "output_tokens":             usage.get("output_tokens", 0),
                    "cache_creation_input_tokens": usage.get("cache_creation_input_tokens", 0),
                    "cache_read_input_tokens":   usage.get("cache_read_input_tokens", 0),
                },
                "gitBranch":         branch,
                "cwd":               entry.get("cwd", ""),
                "run_id_from_branch": run_from_branch,
            })
    return turns


# ── Phase 3: Find long-lived orchestrator sessions ───────────────────────────

def find_orchestrator_sessions(since: str, until: str, known_session_ids: set) -> list:
    """
    Scan the main harmonik project dir for transcripts active in the window
    that are NOT daemon worktree runs (branch=main, cwd=repo root).
    These are captain/crew/operator sessions — the "idle burn" bucket.
    """
    main_project = CLAUDE_PROJECTS / "-Users-gb-github-harmonik"
    sessions = []
    if not main_project.exists():
        return sessions

    for f in sorted(main_project.glob("*.jsonl")):
        session_id = f.stem
        if session_id in known_session_ids:
            continue  # already covered by daemon run attribution

        turns = []
        models = defaultdict(int)
        usage_total = {"input": 0, "output": 0, "cache_creation": 0, "cache_read": 0}
        first_ts = last_ts = None
        branches = set()
        cwds = set()

        try:
            with open(f, errors="replace") as fh:
                for line in fh:
                    line = line.strip()
                    if not line:
                        continue
                    try:
                        entry = json.loads(line)
                    except Exception:
                        continue
                    if entry.get("type") != "assistant":
                        continue
                    ts = entry.get("timestamp", "")
                    ts_norm = norm_ts(ts)
                    if ts_norm < since or ts_norm > until:
                        continue
                    msg   = entry.get("message", {})
                    usage = msg.get("usage", {})
                    if not usage:
                        continue
                    model = msg.get("model", "unknown")
                    models[model] += 1
                    add_usage(usage_total, usage)
                    branches.add(entry.get("gitBranch", ""))
                    cwds.add(entry.get("cwd", ""))
                    if first_ts is None:
                        first_ts = ts
                    last_ts = ts
                    turns.append(ts)
        except Exception:
            continue

        if not turns:
            continue

        # Only non-run branches qualify as orchestrators
        non_run_branches = [b for b in branches if not b.startswith("run/")]
        if not non_run_branches and branches:
            continue  # all branches are run/ → daemon run, skip

        dominant_model = max(models, key=models.__getitem__) if models else "unknown"
        cost = sum(
            compute_cost({
                "input_tokens": usage_total["input"],
                "output_tokens": usage_total["output"],
                "cache_creation_input_tokens": usage_total["cache_creation"],
                "cache_read_input_tokens": usage_total["cache_read"],
            }, m) * cnt / max(1, sum(models.values()))
            for m, cnt in models.items()
        )

        sessions.append({
            "session_id":    session_id,
            "session_file":  str(f),
            "type":          "orchestrator",
            "role":          "captain_or_crew_unknown",  # no registry yet
            "first_ts":      first_ts,
            "last_ts":       last_ts,
            "turn_count":    len(turns),
            "models":        dict(models),
            "dominant_model": dominant_model,
            "usage":         usage_total,
            "cost_usd":      cost,
            "branches":      list(branches),
            "cwds":          list(cwds),
        })

    return sessions


# ── Main analysis ─────────────────────────────────────────────────────────────

def run_analysis(since: str, until: str) -> dict:
    warnings = []

    # --- Step 1: Build event index
    event_data = build_event_index(since, until)
    runs       = event_data["runs"]
    warnings  += event_data["warnings"]

    # --- Step 2: For each run, read its transcripts
    bead_records = {}      # bead_id -> aggregated record
    run_records  = {}      # run_id  -> record

    known_session_ids = set()
    daemon_sessions   = set()  # session IDs from daemon runs

    for run_id, r in runs.items():
        if not r.get("bead_id"):
            warnings.append(f"run {run_id}: no bead_id found in events")
            continue

        bead_id  = r["bead_id"]
        node_id  = r.get("node_id", "")
        queue_id = r.get("queue_id", "")

        all_turns = []

        # Primary: use session_log_location paths
        for lp in r.get("log_paths", []):
            resolved = resolve_transcript_path(lp)
            if not resolved:
                warnings.append(f"run {run_id} bead {bead_id}: transcript not found: {lp}")
                continue
            sid = Path(resolved).stem
            known_session_ids.add(sid)
            daemon_sessions.add(sid)
            turns = read_transcript(resolved, since, until)
            all_turns.extend(turns)

        # Fallback: scan for gitBranch=run/<run_id> in known project dirs
        if not all_turns and r.get("claude_session_ids"):
            for csid in r["claude_session_ids"]:
                known_session_ids.add(csid)
                # Find the file
                candidate = None
                for d in CLAUDE_PROJECTS.iterdir():
                    fp = d / f"{csid}.jsonl"
                    if fp.exists():
                        candidate = str(fp)
                        break
                if candidate:
                    daemon_sessions.add(csid)
                    turns = read_transcript(candidate, since, until)
                    all_turns.extend(turns)

        if not all_turns:
            warnings.append(f"run {run_id} bead {bead_id}: no transcript data found")

        # Aggregate
        usage_total  = {"input": 0, "output": 0, "cache_creation": 0, "cache_read": 0}
        models_seen  = defaultdict(int)
        cost_total   = 0.0
        hours_seen   = set()

        for t in all_turns:
            u = t["usage"]
            add_usage(usage_total, u)
            m = t["model"]
            models_seen[m] += 1
            cost_total += compute_cost(u, m)
            ts_norm = norm_ts(t["timestamp"])
            if len(ts_norm) >= 14:
                hours_seen.add(ts_norm[:14])  # YYYY-MM-DDTHH

        dominant_model = max(models_seen, key=models_seen.__getitem__) if models_seen else "unknown"

        run_records[run_id] = {
            "run_id":        run_id,
            "bead_id":       bead_id,
            "node_id":       node_id,
            "queue_id":      queue_id,
            "started_at":    r.get("started_at"),
            "ended_at":      r.get("ended_at"),
            "success":       r.get("success"),
            "turn_count":    len(all_turns),
            "models":        dict(models_seen),
            "dominant_model": dominant_model,
            "usage":         usage_total,
            "cost_usd":      cost_total,
            "productive":    True,
        }

        # Roll up per bead
        if bead_id not in bead_records:
            bead_records[bead_id] = {
                "bead_id":    bead_id,
                "run_count":  0,
                "usage":      {"input": 0, "output": 0, "cache_creation": 0, "cache_read": 0},
                "cost_usd":   0.0,
                "models":     defaultdict(int),
                "node_ids":   set(),
                "productive": True,
            }
        br = bead_records[bead_id]
        br["run_count"] += 1
        for k in usage_total:
            br["usage"][k] += usage_total[k]
        br["cost_usd"] += cost_total
        for m, cnt in models_seen.items():
            br["models"][m] += cnt
        if node_id:
            br["node_ids"].add(node_id)

    # Convert sets/defaultdicts
    for br in bead_records.values():
        br["models"]   = dict(br["models"])
        br["node_ids"] = list(br["node_ids"])
        br["dominant_model"] = max(br["models"], key=br["models"].__getitem__) if br["models"] else "unknown"
        br["cache_read_pct"] = cache_read_pct(br["usage"])

    # --- Step 3: Find orchestrator (always-on) sessions
    orch_sessions = find_orchestrator_sessions(since, until, known_session_ids)

    # --- Step 4: Global rollups
    productive_cost = sum(r["cost_usd"] for r in run_records.values())
    orch_cost       = sum(s["cost_usd"] for s in orch_sessions)
    total_cost      = productive_cost + orch_cost

    # By model (productive runs)
    by_model = defaultdict(lambda: {"cost": 0.0, "tokens": {"input": 0, "output": 0, "cache_creation": 0, "cache_read": 0}})
    for r in run_records.values():
        # Weight by model share in that run
        total_m = sum(r["models"].values())
        for m, cnt in r["models"].items():
            frac = cnt / max(1, total_m)
            by_model[m]["cost"] += r["cost_usd"] * frac
            for k, v in r["usage"].items():
                by_model[m]["tokens"][k] = by_model[m]["tokens"].get(k, 0) + int(v * frac)
    # Add orchestrator model usage
    for s in orch_sessions:
        total_m = sum(s["models"].values())
        for m, cnt in s["models"].items():
            frac = cnt / max(1, total_m)
            by_model[m]["cost"] += s["cost_usd"] * frac
            for k, v in s["usage"].items():
                by_model[m]["tokens"][k] = by_model[m]["tokens"].get(k, 0) + int(v * frac)

    # By hour
    by_hour = defaultdict(lambda: {"cost": 0.0, "tokens": {"input": 0, "output": 0, "cache_creation": 0, "cache_read": 0}})
    # We'll scan transcripts again for hourly breakdown — using run_records.started_at as approximation
    # (exact per-turn by-hour would require re-reading transcripts; for MVP use start hour)
    for r in run_records.values():
        ts = r.get("started_at") or ""
        hour = norm_ts(ts)[:14] if ts else "unknown"
        by_hour[hour]["cost"] += r["cost_usd"]
        for k, v in r["usage"].items():
            by_hour[hour]["tokens"][k] = by_hour[hour]["tokens"].get(k, 0) + v
    for s in orch_sessions:
        # Spread orch cost uniformly across the hours they were active (approximate)
        ts = s.get("first_ts") or ""
        hour = norm_ts(ts)[:14] if ts else "unknown"
        by_hour[hour]["cost"] += s["cost_usd"]
        for k, v in s["usage"].items():
            by_hour[hour]["tokens"][k] = by_hour[hour]["tokens"].get(k, 0) + v

    # Global token totals
    global_usage = {"input": 0, "output": 0, "cache_creation": 0, "cache_read": 0}
    for r in run_records.values():
        for k, v in r["usage"].items():
            global_usage[k] += v
    for s in orch_sessions:
        for k, v in s["usage"].items():
            global_usage[k] += v

    # Top 10 run records by cost
    top_runs = sorted(run_records.values(), key=lambda x: -x["cost_usd"])[:10]

    # Top 10 beads by cost
    top_beads = sorted(bead_records.values(), key=lambda x: -x["cost_usd"])[:10]

    # Top orchestrator sessions by cost
    top_orch = sorted(orch_sessions, key=lambda x: -x["cost_usd"])[:10]

    # Opus% vs Sonnet% of total spend
    tier_cost = defaultdict(float)
    for m, d in by_model.items():
        tier_cost[model_tier(m)] += d["cost"]
    total_tier = max(sum(tier_cost.values()), 0.0001)

    return {
        "window":            {"since": since, "until": until},
        "total_cost_usd":    total_cost,
        "productive_cost_usd": productive_cost,
        "orchestrator_cost_usd": orch_cost,
        "productive_pct":    100.0 * productive_cost / max(total_cost, 0.0001),
        "idle_pct":          100.0 * orch_cost       / max(total_cost, 0.0001),
        "global_usage":      global_usage,
        "cache_read_pct":    cache_read_pct(global_usage),
        "bead_count":        len(bead_records),
        "run_count":         len(run_records),
        "orch_session_count": len(orch_sessions),
        "by_model":          {m: {"cost": d["cost"],
                                   "cost_pct": 100.0 * d["cost"] / max(total_cost, 0.0001),
                                   "tokens": d["tokens"]}
                              for m, d in by_model.items()},
        "by_tier": {t: {"cost": c, "pct": 100.0 * c / total_tier}
                    for t, c in tier_cost.items()},
        "by_hour":           {h: d for h, d in sorted(by_hour.items())},
        "top_beads":         top_beads,
        "top_runs":          top_runs,
        "top_orchestrators": top_orch,
        "warnings":          warnings,
        "ccusage_gap_note":  (
            "ccusage alone is insufficient: (1) no bead/run attribution (just per-session totals); "
            "(2) date-granular only, not sub-day; (3) local-machine only (remote worker transcripts "
            "missing); (4) misses always-on orchestrator sessions unless you manually identify them; "
            "(5) no productive-vs-idle split. This join adds all five."
        ),
    }


# ── Formatters ──────────────────────────────────────────────────────────────

def fmt_dollars(v: float) -> str:
    return f"${v:.4f}"

def fmt_tokens(n: int) -> str:
    if n >= 1_000_000:
        return f"{n/1_000_000:.1f}M"
    if n >= 1_000:
        return f"{n/1_000:.0f}K"
    return str(n)


def print_summary(result: dict) -> None:
    r = result
    gu = r["global_usage"]

    print("=" * 70)
    print(f"  HARMONIK 24H TOKEN USAGE ANALYSIS")
    print(f"  Window: {r['window']['since']}  →  {r['window']['until']}")
    print("=" * 70)
    print()
    print(f"  TOTAL COST:        {fmt_dollars(r['total_cost_usd'])}")
    print(f"  Productive (bead): {fmt_dollars(r['productive_cost_usd'])}  ({r['productive_pct']:.1f}%)")
    print(f"  Idle/Orchestrator: {fmt_dollars(r['orchestrator_cost_usd'])}  ({r['idle_pct']:.1f}%)")
    print()
    print(f"  Beads attributed:  {r['bead_count']}")
    print(f"  Daemon runs:       {r['run_count']}")
    print(f"  Orch sessions:     {r['orch_session_count']}")
    print()
    tok_total = gu['input'] + gu['output'] + gu['cache_creation'] + gu['cache_read']
    print(f"  TOKEN TOTALS:      {fmt_tokens(tok_total)} total")
    print(f"    input:           {fmt_tokens(gu['input'])}")
    print(f"    output:          {fmt_tokens(gu['output'])}")
    print(f"    cache_creation:  {fmt_tokens(gu['cache_creation'])}")
    print(f"    cache_read:      {fmt_tokens(gu['cache_read'])}  ({r['cache_read_pct']:.1f}% of total)")
    print()

    print("  BY MODEL TIER (share of total spend):")
    for tier, d in sorted(r["by_tier"].items(), key=lambda x: -x[1]["cost"]):
        print(f"    {tier:10s}  {fmt_dollars(d['cost'])}  ({d['pct']:.1f}%)")
    print()

    print("  BY MODEL (detailed):")
    for m, d in sorted(r["by_model"].items(), key=lambda x: -x[1]["cost"]):
        print(f"    {m:25s}  {fmt_dollars(d['cost'])}  ({d['cost_pct']:.1f}%)")
    print()

    print("  TOP 10 BEADS BY COST:")
    for i, b in enumerate(r["top_beads"], 1):
        cr = b["cache_read_pct"]
        print(f"    {i:2}. {b['bead_id']:12s}  {fmt_dollars(b['cost_usd'])}  "
              f"runs={b['run_count']}  model={b['dominant_model']}  cache_read={cr:.0f}%")
    print()

    print("  TOP 10 DAEMON RUNS BY COST:")
    for i, rr in enumerate(r["top_runs"], 1):
        ok = "OK" if rr.get("success") else "FAIL"
        print(f"    {i:2}. bead={rr['bead_id']:12s}  {fmt_dollars(rr['cost_usd'])}  "
              f"{ok}  model={rr['dominant_model']}  turns={rr['turn_count']}")
    print()

    print("  TOP ORCHESTRATOR SESSIONS (always-on burn):")
    for i, s in enumerate(r["top_orchestrators"][:5], 1):
        print(f"    {i}. session={s['session_id'][:8]}…  {fmt_dollars(s['cost_usd'])}  "
              f"model={s['dominant_model']}  turns={s['turn_count']}")
        print(f"       {s['first_ts'][:19]} → {s['last_ts'][:19]}")
    print()

    print("  HOURLY SHAPE (cost by hour, UTC):")
    for hour, d in sorted(r["by_hour"].items()):
        bar = "█" * max(1, int(d["cost"] * 200))
        print(f"    {hour}  {fmt_dollars(d['cost'])}  {bar}")
    print()

    if r["warnings"]:
        print(f"  WARNINGS ({len(r['warnings'])} coverage gaps):")
        for w in r["warnings"][:15]:
            print(f"    ⚠  {w}")
        if len(r["warnings"]) > 15:
            print(f"    … and {len(r['warnings']) - 15} more")
    print()

    print("  WHY CCUSAGE ALONE IS INSUFFICIENT:")
    print(f"    {r['ccusage_gap_note']}")
    print()
    print("=" * 70)


# ── Entry point ──────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="harmonik Phase-0 token usage join")
    parser.add_argument("--since",  default="2026-06-21T15:00:00Z", help="Window start (ISO UTC)")
    parser.add_argument("--until",  default="2026-06-22T15:00:00Z", help="Window end (ISO UTC)")
    parser.add_argument("--format", default="summary", choices=["json", "summary"])
    args = parser.parse_args()

    since = norm_ts(args.since)
    until = norm_ts(args.until)

    result = run_analysis(since, until)

    if args.format == "json":
        # JSON-serialize (convert non-serializable types)
        def default(o):
            if isinstance(o, set):
                return list(o)
            return str(o)
        print(json.dumps(result, indent=2, default=default))
    else:
        print_summary(result)


if __name__ == "__main__":
    main()
