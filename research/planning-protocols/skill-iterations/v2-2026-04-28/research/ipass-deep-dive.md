# I-PASS Deep Dive — Translation to Coding-Agent Handoff

## 1. The five sections, as actually defined

I-PASS was authored by Starmer et al. (Boston Children's, published in *Pediatrics* 2012, validated in NEJM 2014 across nine hospitals).

- **I — Illness severity.** A *one-word* tag: `stable`, `watcher`, or `unstable`. Forced-choice, not prose. Chosen because pilot observers found acuity was the single most-commonly-omitted item.
- **P — Patient summary.** Brief diagnosis + treatment-to-date + assessment. The narrative anchor.
- **A — Action list.** To-dos for the *receiver*, with owner and (where relevant) timing. Not history; future work only.
- **S — Situation awareness / contingency plans.** Explicit `if X happens, then do Y` pairs. The format is prescribed because pilots showed contingency thinking was rarely volunteered without a slot for it.
- **S — Synthesis by receiver.** The receiver, *out loud*, restates what they heard.

## 2. Time budget

NEJM 2014 measured oral handoff at **2.4 min/patient before, 2.5 min/patient after** I-PASS. The protocol added structure without adding time. Written handoff documents are correspondingly short — closer to a structured note than a narrative.

## 3. Evidence base

NEJM 2014: 30% reduction in preventable medical errors across 9 hospitals; 23% reduction in all medical errors; no workflow penalty. A 32-hospital follow-up reported 47% reduction in adverse events. Synthesis-by-receiver compliance jumped from 31% to 83%.

What I-PASS *doesn't* fix: cross-team handoffs (it targets within-unit shift change), missing data the sender never had, and — per a 2019 adherence study — "synthesis" is the section receivers most often skip ("seems redundant", "takes too much time"). The protocol cannot force a tired receiver to engage.

## 4. Translation map

| I-PASS | Coding-agent analog | Translates? |
|---|---|---|
| Illness severity | Run health: `green / blocked / broken` | Cleanly. One word. Currently absent from our schema. |
| Patient summary | Intent + brief state of work | Cleanly — this is what `Intent` already is. |
| Action list | What to start with | Cleanly — this is `What to Start With`. |
| Contingency (if-then) | Decisions Parked + Open Questions | **Partially.** I-PASS forces *if-then* pairing; our schema lets parked items float without trigger conditions. Tightening to `if X, then do Y` is a real gain. |
| Synthesis by receiver | `/session-resume` back-brief | Cleanly — already paired. |

**No I-PASS analog:** Load-Bearing Tokens (vocabulary preservation across a stateless boundary — no medical equivalent because both nurses speak the same dialect), Autonomy Scope (a hospital resident's authority is institutional, not per-shift), Out of Scope.

**No coding-agent analog for:** Illness severity as a *forced-choice tag*. We don't have one and probably should.

## 5. What synthesis actually looks like

The literature is surprisingly thin on form. The original paper calls it "read-back by the receiving resident." Adherence studies score it as present if the receiver produces *any* synthesis statement — verbatim, paraphrase, or structured all count. Empirically it is paraphrase plus questions, not verbatim. This matches our `/session-resume` back-brief design: paraphrase + load-bearing-token readback. We are doing it slightly stricter than I-PASS, which is fine.

## 6. Proposed structure (cuts dominate)

I-PASS would keep five things and trim ours from eight to **five**:

```
## Run health
<one word: green | blocked | broken>

## Intent
<2-3 sentences: what we're doing and why>

## What to start with
<the next concrete action — one item, not a menu>

## If-then
- If <trigger>, then <response>
- (Only items with a real trigger. Empty section is fine.)

## Load-bearing tokens
<flat list, only terms whose *exact spelling* matters>
```

**Cut outright:**
- **Decisions Made** — git log is authoritative; restating it is the procedural-permission-seeking generator.
- **Decisions Parked** *as a separate section* — fold into If-then with explicit triggers, or drop. A parked decision with no trigger is a TODO, not a handoff item.
- **Open Questions** — same treatment. Either it's blocking (→ Run health = blocked, with the question as Intent) or it's contingent (→ If-then) or it doesn't belong.
- **Autonomy Scope** — belongs in the project's standing instructions, not in every handoff.
- **Out of Scope** — implied by Intent. If the sender feels they have to spell it out, Intent is too vague.

The 50+ load-bearing tokens problem is solved by I-PASS discipline: tokens earn their slot only if a *substitute spelling would change behavior*. Most won't survive that test.

## Sources

- [Starmer et al., NEJM 2014](https://www.nejm.org/doi/full/10.1056/NEJMsa1405556)
- [I-PASS mnemonic primer (Pediatrics 2012)](https://pmc.ncbi.nlm.nih.gov/articles/PMC9923540/)
- [Adherence study — synthesis often skipped](https://pmc.ncbi.nlm.nih.gov/articles/PMC6570451/)
- [AHRQ TeamSTEPPS I-PASS tool](https://www.ahrq.gov/teamstepps-program/curriculum/communication/tools/ipass.html)
- [Multi-setting adaptation review](https://pmc.ncbi.nlm.nih.gov/articles/PMC7382547/)
