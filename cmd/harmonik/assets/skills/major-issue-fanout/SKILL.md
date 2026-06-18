---
name: major-issue-fanout
description: >
  Protocol for diagnosing major, recurring critical-path blockers via parallel
  agent fan-out. Triggers when root cause has been refuted ≥2× or a wedge has
  survived ≥2 fix attempts. Core rule: NEVER hand-grep events.jsonl by run_id
  (false negatives); use harmonik subscribe --json or jq structured queries.
  Fan out 10-15 agents on DISTINCT angles + ≥2 adversarial verifiers that can
  OVERRULE a wrong synthesis. Captain orchestrates (spawns + synthesizes),
  never debugs inline. Source: logmine F14 + 2026-06-09-concurrent-dispatch-wedge postmortem.

sources:
  - docs/major-issue-fanout-protocol.md
  - docs/postmortems/2026-06-09-concurrent-dispatch-wedge.md
  - docs/orchestrator-rules.md
---

# Major-Issue Fan-Out Skill

Load this skill when: a daemon wedge or failure class has survived ≥2 fix attempts, the
root cause has flip-flopped ≥2×, and you're considering "let me look at X one more time."
That instinct is wrong. This protocol replaces it.

---

## The one rule that matters most

**NEVER hand-grep `events.jsonl` by `run_id`.**

```bash
# WRONG — produces false negatives; drove 18h of wrong diagnoses:
grep "019eae67" .harmonik/events/events.jsonl

# RIGHT — structured, ordered, complete:
jq 'select(.run_id == "019eae67-b1f0-7e4c-8f96-14e2ad3c3353")' \
  $HARMONIK_PROJECT/.harmonik/events/events.jsonl

# RIGHT — live stream, filtered:
harmonik subscribe --json \
  --types run_completed,run_failed,run_stale,launch_stall_detected
```

Events may carry `run_id` under a nested key or may not carry it at all at the top level.
Substring grep silently drops those events. Structured `jq select()` does a full-object match.

---

## Escalation gate (when to trigger)

Fire this protocol when ALL hold:
1. Wedge survived **≥2 fix attempts** without resolution.
2. Root cause refuted/retracted **≥2×** (different hypotheses each time).
3. Issue is blocking the critical path (daemon down, no-spawn, no-merge).
4. At least one refutation came from a **live-smoke**, not reasoning alone.

> **NOT for deciding open questions** — that is the captain skill's §0.1
> consensus-first gate (a 3-agent consensus run before any surface-and-await). This
> protocol DIAGNOSES a stuck BLOCKER only; it does not DECIDE a question.

---

## The protocol (abbreviated)

Full detail: `docs/major-issue-fanout-protocol.md`

### Step 1 — Pause and announce

```bash
harmonik comms send --from captain --broadcast \
  -- "MAJOR-ISSUE fan-out starting for <wedge description>. Hold restarts/deploys."
```

### Step 2 — Collect durable artifacts

```bash
# Event dump (structured):
harmonik subscribe --json --types run_failed,run_stale,launch_stall_detected \
  | head -200 > /tmp/event-dump.json

# Bead state:
br show <bead_id> --format json

# Recent commits:
git -C $HARMONIK_PROJECT log --oneline -20

# Run stale goroutine count:
jq 'select(.event_type == "run_stale") | {run_id, goroutine_count, active_run_count}' \
  $HARMONIK_PROJECT/.harmonik/events/events.jsonl | tail -5
```

### Step 3 — Fan out 10–15 agents at DISTINCT angles

Pick angles from this taxonomy (don't repeat angles across agents):

| Angle | Focus |
|---|---|
| Code structure | File:line in wedge event; goroutine lifecycle; channel ownership |
| Event timeline | Ordered event sequence; timing gaps; missing events |
| **run_id lifecycle (context_cancel precondition)** | **Before any other hypothesis: was a reaper-spawn / `agent_ready` observed for this run_id? A context_cancel with NO observed reaper lifecycle means investigate the lifecycle, NOT the deadline or cache.** See §run_id lifecycle check below. |
| Config drift | Binary version, `--workflow-mode`, daemon flags at wedge time |
| Concurrency model | Channel consumers; mutex holders; race under N>1 |
| Regression window | First-bad commit; what changed? `git bisect` direction |
| Reproducer | Minimal reproducing case |
| Contrast (working vs broken) | Event diff between a healthy run and wedged run |
| External dependencies | tmux state, flock holders, trust-file size, disk/fd |
| Prior hypotheses | Evidence for each prior diagnosis; why was each wrong? |
| Canary isolation | Last known-good vs first known-bad configuration delta |

**Spawn all in parallel, `run_in_background=True`.**

---

### run_id lifecycle check (mandatory precondition for `context_cancel`)

**Before accepting ANY timing / cache / graph-size hypothesis for a `context_cancel` event:**

1. Pull the full event sequence for the affected `run_id` via structured query:
   ```bash
   jq 'select(.run_id == "<run_id>")' $HARMONIK_PROJECT/.harmonik/events/events.jsonl \
     | jq -s 'sort_by(.timestamp) | .[] | {timestamp, event_type, run_id}'
   ```
2. Confirm that `agent_ready` (or equivalent reaper-spawn) appears **before** the `context_cancel`.
3. If **no** `agent_ready` / reaper-spawn event exists for this `run_id`:
   - **Reject all timing/cache/graph-size hypotheses immediately.**
   - The cancel was fired by a reaper whose `stalewatch.observe()` received a nil `run_id` event — it cancelled PER-RUN contexts for a run that never spawned. Investigate the lifecycle gap, not the deadline.
   - Reference fix: `960deafc` (stalewatch nil-run_id skip).

**Lesson source:** 2026-06-18 DOT context-cancel saga — ~270 min burned on two refuted hypotheses (cold-cache, DOT-deadline) before the nil-run_id lifecycle gap surfaced via hk-wths.

### Step 4 — Synthesize, then verify adversarially

After agents report, draft ONE candidate root cause. Then spawn ≥2 adversarial verifiers:

```
Your job: REFUTE the synthesis below if you can.
Look for code evidence that contradicts it, events that don't fit,
or a simpler explanation for the same symptoms.
Default to REFUTED if uncertain — you have veto power.

Synthesis: [paste candidate root cause + evidence]

Report: CONFIRMED or REFUTED, with concrete file:line or event evidence.
```

If either verifier REFUTES → discard synthesis, return to step 3 with the refutation as a new angle.

### Step 5 — Convergence gate

Exit when: ≥2 verifiers CONFIRM the same root cause AND you have a concrete artifact
(file:line, structured event, or reproducing test). Reasoning alone is not sufficient.

### Step 6 — Fix + validate correctly

- **Concurrency bugs require ≥2 real concurrent beads** to validate — not single-bead or trivial doc smokes.
- **Trivial smokes gave false "validated" signals 3× in the 2026-06-09 incident.**
- Live-smoke daemon-code deploys before declaring done.
- Get a lane-owner **independent fresh-context APPROVE** before merging.

---

## Captain's role during fan-out

- Spawn agents → synthesize → route to verifiers → announce result. That's it.
- **Do NOT read code inline on the main thread.** Anchoring bias + context exhaustion.
- **Do NOT restart the daemon or deploy code** without announcing via comms first.
- **File a bead for the root cause** before dispatching the fix.

---

## Anti-patterns (what burned 18h)

| Pattern | Cost |
|---|---|
| `grep run_id events.jsonl` | False negatives; drove 4 wrong root causes |
| Iterating on one hypothesis without a reproducer | Sequential "definitive" diagnoses, each overturned |
| Treating a reasoning chain as evidence | "MOOT" call retracted next hour |
| Single-bead trivial smoke as concurrency validator | Masked the real bug 3× |
| Captain debugging inline | Context exhaustion + blocked main thread |
| Declaring fixed without independent lane review | Required by orchestrator-rules.md |
| Accepting timing/cache hypothesis for `context_cancel` without checking run_id lifecycle | ~270 min burned on two refuted hypotheses (2026-06-18); nil-run_id skip in stalewatch was the real cause (hk-wths / `960deafc`) |

---

## References

- Full protocol: `docs/major-issue-fanout-protocol.md`
- Motivating incident: `docs/postmortems/2026-06-09-concurrent-dispatch-wedge.md` §8
- Orchestrator delegation rule: `docs/orchestrator-rules.md` §"Delegate Investigation to Sub-Agents"
