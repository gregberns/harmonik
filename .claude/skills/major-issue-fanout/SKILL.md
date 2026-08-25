---
name: major-issue-fanout
description: >
  Protocol for diagnosing a recurring critical-path blocker by parallel agent
  fan-out, with adversarial verifiers that can overrule the synthesis.
---

<!-- Generated from cmd/harmonik/assets/skills/major-issue-fanout/SKILL.md
     Edit there and mirror in the same commit; scripts/skill-mirror-check.sh fails on drift. -->

# Major-Issue Fan-Out Skill

Reach for this when a wedge or failure class has survived two or more fix
attempts, the root cause has flip-flopped, and you are thinking "let me look at X
one more time." That instinct is what this protocol replaces.

**The round count is itself the evidence, and rounds that FIND things are the
ones that fool you.** Repairs that each turn up real errors feel like progress,
so nobody asks whether they can reach the obstruction at all. If repeated repairs
to a thing do not make the work move, stop repairing that thing and ask what you
have not been reading. The common shape: every round read the document that
DESCRIBES the work, and the obstruction was in the machine that JUDGES it — a
spec defect and a gate defect look identical from inside the spec.

**One outcome of this protocol is "the blocker is an invariant doing its job."**
Before you relax a rule that is in your way, check whether it fired on a state it
exists to refuse. If it did, it is working, and the repair belongs in the state
rather than in the rule. The tell is that it refused at the exact moment you
wanted to move — which is when it is most tempting, and most load-bearing, to
read a correct refusal as an inconvenience.

It DIAGNOSES a stuck blocker. It does not DECIDE an open question.

## The one rule that matters most

**Never hand-grep `events.jsonl` by `run_id`.**

```bash
# WRONG — false negatives; drove 18h of wrong diagnoses:
grep "019eae67" .harmonik/events/events.jsonl

# RIGHT — structured, ordered, complete:
jq 'select(.run_id == "<full-run-id>")' $HARMONIK_PROJECT/.harmonik/events/events.jsonl

# RIGHT — live and filtered:
harmonik subscribe --json --types run_completed,run_failed,run_stale,queue_paused,launch_stall_detected
```

An event may carry `run_id` under a nested key, or not at the top level at all.
Substring grep silently drops those. `jq select()` matches the whole object. The
same applies to `queue_paused`: its cause is at `.payload.reason`, not at the top
level. A blocker that turns out to be a queue stopped at `paused-by-failure` is
not a fan-out — restart it per the **harmonik-dispatch** skill and move on.

## The protocol

Full detail: `docs/major-issue-fanout-protocol.md`.

**1. Pause and announce.** Broadcast that a fan-out is starting and that restarts
and deploys should hold.

**2. Quiesce the queue.** A fan-out spawns far more parallel agents than a daemon
phase tolerates. Stop submitting beads and let the in-flight work drain first — a
fan-out layered on a live dispatching queue puts the daemon's claude processes
behind your agents in the API rate-limiter. See the **harmonik-dispatch** skill,
§ Do not run the daemon and a sub-agent wave at once.

**3. Collect durable artifacts** — a structured event dump, the bead state, recent
commits. Anchor every agent to artifacts that outlive the moment: file paths,
symbol names, `events.jsonl` entries. Never a tmux pane's contents.

**4. Fan out ten to fifteen agents on DISTINCT angles.** Do not repeat an angle.
Useful ones: the code around the wedge event and its goroutine and channel
lifecycle; the ordered event timeline and its gaps; config and binary drift at
wedge time; the concurrency model under N above 1; the regression window and
first-bad commit; a minimal reproducer; the event diff between a healthy run and
the wedged one; external state such as tmux, flock holders, disk and file
descriptors; and one agent whose whole job is to say why each prior hypothesis
was wrong. Spawn them all in the background, in parallel.

**5. Synthesize once, then verify adversarially.** Draft ONE candidate root
cause, then spawn at least two verifiers that can overrule it:

```
Your job: REFUTE the synthesis below if you can.
Look for code evidence that contradicts it, events that don't fit,
or a simpler explanation for the same symptoms.
You have veto power. Never CONFIRM to be agreeable.

Synthesis: [paste candidate root cause + evidence]

Report exactly one of three verdicts, with concrete evidence — a file plus the
symbol you read, a structured event, or a reproducing case:

  REFUTED   — you found evidence that contradicts the synthesis. Name it.
  CONFIRMED — you found evidence that supports it and none that contradicts it.
  UNCERTAIN — you could not settle it either way. Say what specific evidence
              WOULD settle it, and where you would look for that evidence.

Reasoning alone is not evidence for any of the three.
```

**Three answers, not two.** A verifier told to default to REFUTED can never
contribute a CONFIRM, so the gate becomes unreachable and the fan-out loops
forever on a synthesis that may well be right. UNCERTAIN is the honest third
answer and it is productive: the missing evidence it names becomes the next
angle.

Route the verdicts:

- **Any REFUTED** — discard the synthesis; return to the fan-out with the
  refutation as a new angle.
- **All UNCERTAIN** — the synthesis is unproven, not wrong. Return to the fan-out
  with each verifier's named missing evidence as a new angle. Do not re-run the
  same verifiers on the same synthesis with no new evidence.
- **Two or more CONFIRMED and none REFUTED** — you have converged.

**6. Converge on an artifact, not an argument.** Exit only when two verifiers
confirm the same root cause, none refutes it, and you hold something concrete: a
file plus a symbol, a structured event, or a reproducing test. Reasoning alone is
not enough, and an UNCERTAIN does not count toward the two.

**7. Fix, then validate the way the bug actually manifests.** A concurrency bug
needs two or more real concurrent beads to validate — a single-bead or doc-only
smoke gave false "validated" signals three times in the incident this protocol
came from. Live-smoke any daemon-code deploy before declaring it done, and get an
independent fresh-context approval before merging.

## The captain's role during a fan-out

Spawn, synthesize, route to verifiers, announce the result. That is all.

**Do not read code inline on the main thread** — it costs you the context you
exist to protect and anchors you on the first hypothesis you read. Do not restart
the daemon or deploy code without announcing it first. File a bead for the root
cause before dispatching the fix.
