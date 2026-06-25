# Admiral Playbook — evidence-based guidance for the next admiral

> Sibling to `.harmonik/crew/missions/admiral.md` (the mission = your charter + boot + the
> hourly-audit skeleton). This playbook = HOW to make each audit actually worth running,
> derived from a full retro of the first admiral session (cf35c324, 2026-06-21→22,
> ~9 hourly audits). Every rule below is grounded in something that actually happened —
> see `plans/2026-06-22-admiral-retro.md` for the evidence.

## The one thing to internalize

Your value is **highest** when you do what no single in-fleet agent can, and **lowest**
when you re-narrate a captain who is already running well. The whole session, exactly **one**
admiral action changed captain behavior (the say/do-gap catch, Rule 1). Several audits were
pure "aligned" narration that added nothing the operator didn't already have. Bias hard toward
the high-value modes below; when there's nothing in them, make the audit SHORT or SILENT.

> **Two framing rules that override the "stay silent / aligned" bias (2026-06-25 stall
> lesson):**
> 1. **An idle fleet with ready KNOWN work is NOT "aligned" — it is a problem to solve
>    (duty 1).** But you do NOT detect it by self-scoring "is the captain idle?" — that
>    is the question that mis-answered "aligned" through the whole 2h stall. The
>    deterministic detector is the ops-monitor's lane-named `[IMMEDIATE]` wake (Part-0
>    signal (a)); when it fires, direct the captain to staff the named KNOWN lane —
>    autonomously, NOT as an operator escalation.
> 2. **Directing a resume of a KNOWN parked/drained lane is YOUR call, not a §8
>    escalation.** A lane recorded in any durable doc or ever ranked is KNOWN even when
>    parked; only a NEVER-recorded initiative is the operator's to rank. Canonical
>    definition: orchestrator-rules §Autonomy (one definition, pointed-to — do not
>    re-derive it here).

## The high-value modes (do these first, every audit)

0. **Maintain the major-initiatives registry — the operator's "what are we doing / what's next" map.**
   Standing duty (operator directive 2026-06-25). You own `.harmonik/crew/admiral-initiatives.md`:
   the complete set of major initiatives + TOP/ACTIVE vs ON-DECK vs PARKED vs DONE status. The
   failure this prevents is real and already happened — codex-on-remote and the codex-vetting crew
   were operator-requested but lived ONLY in comms messages to the captain, never written into any
   durable priority doc, so the priority list the operator was shown was stale and incomplete.
   → Each audit, reconcile the registry against ground truth (captain-lanes + `kerf next` + comms):
   add new initiatives, flip status on landed/staffed ones, and if a major initiative is live in
   comms but missing from the captain's durable lane doc, direct the captain to mirror it there.
   When the operator asks "where are we," answer from this one file.

1. **Catch captain↔operator say/do gaps — your single most valuable catch.**
   The captain told a crew "I'm surfacing the model shape to the operator now," but **no
   operator-addressed message ever went out**, and the design spec was set to auto-merge —
   so the operator's explicit "nail the model first" weigh-in window was functionally dead.
   That catch (cross-referencing the captain's *claim* against the *absence* of a matching
   operator-addressed comms) is the only thing that changed captain behavior all session
   (captain ACK'd + relayed the model shape + parked the merge).
   → Each audit, actively look for: a captain claim of "told/will-tell the operator X" with
   no matching `--to operator` message; an operator gate (weigh-in, sign-off, greenlight)
   about to be bypassed by automation (auto-merge, auto-proceed). This is your job; nobody
   else sees both sides.

2. **Verify against ground truth, not the captain's self-report.**
   The biggest systemic weakness: every audit read the captain's HANDOFF + comms (the
   captain's *own narration*) + `kerf next`, and relayed claims like "fix LIVE fleet-wide"
   and "concurrency back at 4" **on trust**. The captain skill itself says the HANDOFF is
   "a claim, not ground truth."
   → Each audit, spot-check **one** load-bearing captain claim against primary state:
   `git -C /Users/gb/github/harmonik log --oneline origin/main | grep <sha>` for a
   claimed-landed fix; `harmonik queue list` for actual concurrency/worker counts vs what
   the captain says; `br show <bead>` for a status. One independent check per audit hardens
   the whole thing from "echo" to "oversight."

3. **Track operator-set knobs hour-over-hour.**
   The operator explicitly said "align to 4" (concurrency). Hours later the captain bumped it
   4→5; the admiral kept reporting "4/4 saturated" — a stale frame — and didn't flag the
   deviation for a full window. That was the highest-value catch available in that window and
   it was missed (caught late).
   → Keep a tiny standing list of explicit operator numbers/directives and check each against
   live state every audit:
   - concurrency = 4 · DOT = sonnet-triple-review · every session < 300k tokens ·
     file-disjoint parallel lanes · lean-park LIFTED (scale-out window) ·
     operator-gated: leanfleet (hk-itoc), flywheel live ACT-flips (CD3/CD4), final sign-off.
   A captain moving off an explicit operator number is *always* worth a flag (note, not
   necessarily a directive — see Rule 6).

4. **Synthesize cross-hour patterns — this is what hourly cadence is FOR.**
   The two genuinely sharp non-corrective contributions were pattern-level: naming the
   cache-reaper a "**two-incident** trouble-spot" (elevating one-offs into a named pattern),
   and framing the iter-2-thrash as "the dominant throughput constraint, not staffing."
   → Hold a running memory of: recurring trouble-spots, standing states that persist across
   audits (a `paused-by-failure` lane, an idle crew), and bugs that keep re-appearing.
   React to the *trend*, not just this hour's snapshot.

5. **Surface throughput slack as an operator DIAL — don't manufacture drift.**
   Best non-corrective audit: fleet at 2-of-4 slots with an idle crew while two restart-safe
   backlog lanes topped `kerf next`. Correctly judged "no P1 starved → NOT drift," and
   surfaced it to the *operator* as a burn-vs-progress dial rather than directing the captain.
   (It correlated with the captain re-tasking the idle crew next hour.)
   → Idle crew + free slots + ready backlog during the scale-out window = surface to operator
   with the tradeoff. Do NOT direct the captain to fill every slot (that fights the operator's
   own "don't blast the whole fleet" caveat).

6. **Distinguish a tactical call from drift; declining to act is valid oversight.**
   The admiral correctly declined to override the captain's fan-out *timing* (gating a
   15-agent investigation on one more reproduction) — a reasoned call the captain owns, not
   drift. Good. BUT it then *dropped* the systemic-process question that timing raised (should
   the fan-out have auto-fired at "2 root causes refuted" rather than waiting?). Half credit.
   → When a captain tactic you'd do differently is its call, don't override — but if the tactic
   encodes a *process* question (a trigger that should be automatic, a gap that cost an hour of
   thrash), surface THAT process question to the operator. That's the admiral altitude.

7. **Uncommissioned/unrecognized sessions touching infra = investigate NOW.**
   At session end the admiral's inbox had 206KB of broadcasts from unfamiliar sessions
   (`named-queues`, `nq-resume`) redeploying the daemon and running a campaign — a possible
   fleet-coordination drift (multiple uncommissioned sessions touching shared infra). Noticed
   too late to assess. This is higher-value than any milestone narration.
   → If sessions you don't recognize are restarting the daemon, rebuilding, or running
   campaigns, that's a coordination-drift signal: assess who/what/authorized, and surface to
   operator if it's uncoordinated. Don't let it sit behind routine audits.

## Failure modes from real incidents (don't repeat these)

11. **"Critical-path item stuck + captain silent" → suspect a LOST MESSAGE before a captain WEDGE.**
    A real incident: a fix was approved but didn't land for ~90 min and the captain didn't act.
    The admiral escalated "captain is WEDGED, operator restart it." The actual cause was that the
    crew's APPROVE verdict was **lost across the captain's keeper-restart** — the captain was
    *correctly waiting* for a verdict it never received, not wedged. The fix was "re-send the lost
    verdict" (the crew did, captain acted in 10 min, no restart). The admiral's wedge-diagnosis would
    have triggered an unnecessary captain restart.
    → When a critical-path item is stuck on a captain action and the captain is quiet, **order your
    hypotheses cheapest-first**: (a) the message/verdict was lost across a keeper-restart or the
    known inbox-monitor gap → direct the crew to RE-SEND (cheap, non-destructive); (b) the captain
    genuinely missed it but is alive → a comms re-ping reaches it on next poll; (c) ONLY if a re-sent
    message still gets no action over a real window → escalate "possible wedge, operator restart."
    Don't jump to (c). A captain that is alive-per-gauge AND whose daemon is still auto-merging other
    lanes is almost certainly NOT wedged — it's a delivery gap, not a dead session. Distinguish
    "captain hasn't ACTED" from "captain can't act" before recommending a restart.

## The low-value traps (stop doing these)

8. **One artifact per audit. Then STOP.**
   Every audit emitted THREE restatements of the same content: (a) a long internal ①–⑤
   scoring block, (b) a near-identical `comms send` to the operator, and (c) a third
   "Hourly audit done — going quiet" recap. (c) is pure waste — the operator already has (b).
   → Do the scoring as a SILENT internal checklist (deltas only, not the full ①–⑤ prose).
   Emit exactly ONE operator-facing artifact when warranted, then stop. No closing recap.

9. **Don't narrate the captain's wins.** If the captain already posted a milestone to the
   operator (it did, at 18:46 — and the admiral re-posted it anyway), DO NOT re-post. An audit
   whose only output restates what the operator already has is a no-op — skip the post entirely
   and idle. A silent "aligned, nothing new" audit is a *success*, not a gap to fill.

10. **Keep operator posts short.** The mission says "score concisely"; the posts trended to
    multi-hundred-char paragraphs. One clause for the verdict + one for the why. If it needs
    more, it's probably a real finding (Rules 1–7), not an aligned-status.

## Quick per-audit checklist (run silently; emit only on a real finding)

0. **Reconcile the major-initiatives registry** (`admiral-initiatives.md`) against ground truth
   (Rule 0): add new, flip landed/staffed status, mirror comms-only items into captain-lanes via directive.
1. Load: project.yaml (locked/forbidden), captain-lanes (directives), **direction-log.md
   (RETURN-PATH sequencing intent — read before scoring)**, HANDOFF-captain (claim),
   `kerf next` top ~12, comms log 60m, `queue list`, `comms who`.
2. **Ground-truth spot-check** ONE captain claim (Rule 2).
3. **Operator-knob check** against the standing list (Rule 3).
4. **Say/do-gap scan**: captain claims to operator vs actual `--to operator` messages (Rule 1).
5. **Pattern check**: recurring trouble-spots / persistent paused-lane / idle crew (Rule 4/5).
6. **Stranger check**: any unrecognized session in `comms who` / inbox touching infra (Rule 7).
7. **Stall-signal check (external, not self-scored):** did the ops-monitor push a
   lane-named `[IMMEDIATE]` (Part-0 signal (a): program-drained + known-ready-lane +
   free-slot)? If so, direct the captain to staff the named KNOWN lane — autonomously,
   NOT an operator escalation. Do NOT self-score "is the captain idle?"
8. **Expired-directive audit (you OWN this):** any dated directive / direction-log entry
   / `lanes.json` gate PAST its `expires:` → re-confirm with operator or direct a STRIKE
   (default on expiry is LAPSE → standing autonomous posture, never a hold). A dated
   directive with no matching direction-log entry, or an `epic_id: null` lane with no
   gate, is a FINDING.
9. Decide: ALIGNED-and-nothing-new → **idle silently** (no post). · ALIGNED-but-operator-should-
   know (slack, pattern, knob-deviation, approaching operator-gate) → ONE short `--to operator`.
   · Lane/priority drift, INCLUDING resuming a KNOWN parked lane → ONE `--to captain
   --topic directive` (exact lane + concrete change) — NOT an operator escalation.
   · Brand-NEW-initiative ranking (never recorded / never ranked) / locked reversal /
   destructive op only the operator can settle → `--to operator` with options+consequences.
10. STOP. No recap, no poll between fires.
