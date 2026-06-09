# Major-Issue Fan-Out Diagnosis Protocol

**Version:** 1.0 (2026-06-09)
**Source:** logmine finding F14 + postmortem `docs/postmortems/2026-06-09-concurrent-dispatch-wedge.md`

---

## What this is

A protocol for diagnosing **major, recurring critical-path blockers** — situations where the root
cause has been refuted ≥2× or a wedge has survived ≥2 fix attempts. The 2026-06-09 tapCh incident
(~18h burned, 6 refuted root causes, 79 correction messages) is the motivating case.

The single highest-impact rule: **never hand-grep `events.jsonl` by `run_id` — that method
produces false negatives.** Use `harmonik subscribe --json` or structured event queries instead.

---

## Escalation threshold

Trigger this protocol when ALL of the following hold:

1. A wedge or failure class has survived **≥2 distinct fix attempts** without resolution.
2. The proposed root cause has been **refuted or retracted ≥2×** (different hypotheses each time).
3. The issue is **blocking the critical path** (daemon down, no-spawn, no-merge, etc.).
4. **At least one refutation came from a live-smoke** — not just reasoning alone.

Do NOT trigger for first-attempt bugs or single-hypothesis investigations. The fan-out is for
situations where iterating on one diagnosis thread has already failed.

---

## Protocol

### 1. Pause single-thread investigation

Stop iterating on the current hypothesis. Every additional "definitive" diagnosis on the same
thread adds noise, not signal (the 2026-06-09 incident: 4 sequential "definitive" diagnoses,
all overturned).

Announce via comms:

```bash
harmonik comms send --from captain --broadcast \
  -- "MAJOR-ISSUE fan-out starting for <description>. Hold daemon restarts / code deploys. ETA ~20min."
```

### 2. Gather durable artifacts first

Before spawning agents, collect the concrete artifacts they will anchor to:

```bash
# Structured event stream (NOT hand-grep):
harmonik subscribe --json --types run_completed,run_failed,run_stale,launch_stall_detected \
  | head -200 > /tmp/event-dump.json

# Bead state:
br show <bead_id> --format json

# Recent commit log:
git -C /Users/gb/github/harmonik log --oneline -20

# Goroutine count / process state at time of wedge (if available in events):
grep -F '"event_type":"run_stale"' /Users/gb/github/harmonik/.harmonik/events/events.jsonl \
  | tail -5 | jq '{run_id, goroutine_count, active_run_count}'
```

**NEVER hand-grep `events.jsonl` by `run_id` directly.** Events are not sorted or indexed by
`run_id` — a manual grep misses events that lack the run_id in the top-level object (e.g.,
events emitted under a nested key, or events emitted before the run_id was assigned). The false
negative rate is high enough to drive diagnosis in the wrong direction repeatedly.

Use the structured surfaces:
- **Live stream:** `harmonik subscribe --json --types <event_types>` — server-filtered, ordered.
- **Structured query:** `jq 'select(.run_id == "<id>")' events.jsonl` — full-object match, not substring.
- **Bead history:** `br show <id>` + `br comments list <id>` — state at each transition.

### 3. Fan out 10–15 agents at DISTINCT angles

Spawn agents in parallel, each anchored to a **different** aspect of the failure. Diversity is
the point — if all agents look at the same file/log/hypothesis, the fan-out adds nothing.

**Angle taxonomy (pick from, don't repeat):**

| Angle | What to examine |
|---|---|
| **Code structure** | The file:line in the wedge event; call-graph reading; goroutine lifecycle |
| **Event timeline** | Ordered event sequence from `subscribe --json`; timing gaps; missing events |
| **Config drift** | Binary version, `--workflow-mode`, `--max-concurrent`, daemon flags at time of wedge |
| **Concurrency model** | Channel ownership, goroutine lifetimes, mutex holders; race conditions under N>1 |
| **Regression window** | `git bisect` style — when did this first appear? what commits are in the window? |
| **Reproducer** | Minimal reproducing case; what's the smallest bead/config that wedges? |
| **Contrast: working vs broken** | Diff between a working run's events and a broken run's events |
| **External dependencies** | tmux session state, flock holders, trust-file size, disk/fd limits |
| **Prior hypotheses** | Each prior "definitive" diagnosis — what was the evidence? why was it wrong? |
| **Canary isolation** | What changed between the last known-good run and the first bad run? |

**Spawning pattern (via the Agent tool):**

```python
# Parallel fan-out — all in one message, run_in_background=True
Agent(
  description="Angle: code structure — dot_cascade.go spawn path",
  prompt="Start at internal/daemon/dot_cascade.go:549. Read the perRunEventTap usage: who creates it, who receives from it, are there multiple goroutines consuming the same channel? Report: is the channel shared between >1 reader? Anchor to file:line, not inference.",
  run_in_background=True
)
Agent(
  description="Angle: event timeline — subscribe output",
  prompt="Read /tmp/event-dump.json. For the wedging run_id, list every event in arrival order with its timestamp. Identify which expected event is absent. Report: last event seen, expected-but-missing event, time gap.",
  run_in_background=True
)
# ... 8-13 more agents at distinct angles
```

Use `model=sonnet`, `run_in_background=True`. Do NOT wait for one before spawning the next.

### 4. Require ≥2 adversarial verifiers that can OVERRULE

After the angle agents report, synthesize a candidate root cause. Then spawn **at least 2
adversarial verifiers** with explicit authority to reject the synthesis:

```python
Agent(
  description="Adversarial verifier A — can OVERRULE",
  prompt=f"""
  The synthesis claims: {synthesis}.
  
  Your job is to REFUTE this if you can. Look for:
  - Code evidence that contradicts it.
  - Events that don't fit the proposed mechanism.
  - A simpler explanation for the same symptoms.
  - Prior fix attempts that should have worked if this were the cause.
  
  Default to REFUTED if uncertain. OVERRULE if the evidence doesn't hold.
  Report: CONFIRMED or REFUTED, with concrete evidence (file:line or event).
  """,
  run_in_background=True
)
```

**The verifiers have veto power.** If either REFUTES, discard the synthesis and return to step 3
with the refutation as a new angle. Do not iterate on a refuted synthesis.

### 5. Convergence gate

The protocol exits when:

- ≥2 verifiers CONFIRM the same root cause, AND
- The root cause is supported by a **concrete artifact** (file:line, event with timestamp, reproducing test).

A verbal/reasoning chain without a file:line or reproducing test is NOT sufficient for convergence.
File the reproducing test alongside the fix.

### 6. Fix + validate under the correct conditions

From the postmortem §8:
- **Single-bead smokes can hide concurrency bugs.** If the wedge is concurrency-only, validate
  with **≥2 real concurrent beads** of the correct complexity (not trivial doc smokes).
- **Trivial smokes gave "validated" false signals 3× in the 2026-06-09 incident.**
- Live-smoke the daemon-code deploy before declaring done. `go test` alone is not enough for
  tmux/session/spawn paths.

---

## Diagnosis method: what NOT to do

| Anti-pattern | Why it fails |
|---|---|
| `grep run_id events.jsonl` | False negatives — events may not carry run_id at top level; manual grep skips them |
| Iterating on one hypothesis without a reproducer | 4 sequential "definitive" diagnoses in the incident; each was overturned |
| Treating reasoning as evidence | "MOOT" call on an entire code path without a test; retracted next hour |
| Single-bead / trivial doc smoke as concurrency validator | Masked the tapCh bug 3× in one session |
| Declaring fixed without a lane-owner independent review | Lane owner's fresh-context APPROVE is required (§ orchestrator-rules.md) |
| Captain debugging inline instead of delegating | Context exhaustion + anchoring bias + main-thread blockage |

---

## Diagnosis method: what to use instead

| Need | Command |
|---|---|
| Live event stream | `harmonik subscribe --json --types run_completed,run_failed,run_stale,launch_stall_detected` |
| Structured event query for a run_id | `jq 'select(.run_id == "<id>")' .harmonik/events/events.jsonl` |
| Bead state | `br show <id> --format json` |
| Bead comments / journal | `br comments list <id>` |
| Queue state | `harmonik queue status` |
| Recent daemon events | `harmonik subscribe --since 30m --json` |
| Which runs are currently active | `jq 'select(.event_type == "run_started") | .run_id' events.jsonl \| sort \| uniq` |
| Goroutine / process state | `harmonik subscribe --json \| jq 'select(.event_type == "run_stale") \| {run_id, goroutine_count}'` |

---

## Captain's role during fan-out

- **Spawn the agents and watch for results.** Do NOT read code inline on the main thread.
- **Write the synthesis** — the agents return raw findings; the captain synthesizes and routes to verifiers.
- **Announce + coordinate** via comms before and after the fan-out.
- **Do NOT restart the daemon or deploy code** while the fan-out is running without announcing first.
- **File a bead for the root cause** before dispatching the fix — the bead is the durable record.

---

## Postmortem reference

The full incident narrative, timeline, 6 refuted hypotheses, and fix are in:
`docs/postmortems/2026-06-09-concurrent-dispatch-wedge.md`

§8 (Process Lessons) is required reading before applying this protocol for the first time.

---

## Cross-references

- `docs/postmortems/2026-06-09-concurrent-dispatch-wedge.md` — motivating incident
- `docs/orchestrator-rules.md` — Monitor pattern (`harmonik subscribe`), delegate investigation rule
- `.claude/skills/major-issue-fanout/SKILL.md` — agent-loadable version of this protocol
- logmine finding F14 (`.kerf/works/logmine/04-research/findings.md`)
