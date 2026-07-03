# Note C — "work must not stop before it's done" (work-generates-work + two-phase done)

> **STRAWMAN for operator redline (crew `kynes`, 2026-06-15).** Focused proposal for open question **C**
> from `01-problem-space.md`. The operator named *"work must not stop before it's done"* as the
> flywheel's **top concern** — this note designs exactly that property. Not confirmed; redline freely.

## The property, restated
A unit of work is **not "done" when merged** — it's done when its *terminal* state is reached
(deployed + verified, for deploy-relevant work). Two failure modes to kill:
1. The system treats **merged-but-undeployed/unverified** work as complete and goes idle on top of it
   (← exactly last session's stall: the keeper-warn fix sat landed-but-undeployed for hours).
2. A completion that *should* spawn the next step (deploy, verify, fix) spawns nothing, so motion
   decays instead of compounding.

## Mechanism = two deterministic pieces (a definition + a generator)

### C-1. Two-phase "done" (the DEFINITION — makes the tail visible)
Redefine completion as a per-class **`done_definition`**:
- **Phase 1 — merged:** the `run_id` trailer is on `origin/main` (today's notion of done).
- **Phase 2 — terminal:** deployed + verified (or the class's declared terminal check passed).

Until Phase 2, the work is an **in-flight tail** and **counts as ACTIONABLE WORK** for the sentinel.
So "merged but not deployed" satisfies the sentinel's idle-with-work trip — the system *cannot* go
quiescent while a tail exists. Default `done_definition` stays **merged** (no surprise infinite tails);
deploy-relevant classes opt into the Phase-2 check. *(Reuses the cognition-loop's two-phase "done"
primitive, but extends it from push-lag-safety to a deploy/verify tail.)*

### C-2. Work-generates-work (the GENERATOR — the positive-feedback fuel)
On a Phase-1 completion of a class **with a declared follow-up rule**, the system **deterministically
enqueues the next step** — from the system's *own* output. This is the momentum source §3 of the
design doc called the "fuel."

**Allowed refill SOURCES (the question-C answer — propose this set, operator to trim/extend):**
| # | Source event | Deterministic follow-up | Priority |
|---|---|---|---|
| a | bead merged, class=deploy-relevant, not yet deployed | **deploy+verify** bead | **highest — the named top concern** |
| b | reviewer verdict = REQUEST_CHANGES / BLOCK after cap | **fix** bead targeting the verdict | high |
| c | logmine/harvest detects a recurring failure pattern | **investigation** bead | medium |
| d | a completed bead *explicitly declares* follow-ups | enqueue them (`open`, not same-tick dispatch) | as-declared |

### Guardrail (anti-busywork / anti-runaway — the other half of question C)
The positive loop must **compound, not spin**. Four bounds, all deterministic:
1. **Rule-derived only.** A follow-up is generated *only* from a declared rule (table above), **never
   LLM-invented** — no speculative bead generation in the refill path.
2. **Provenance gate.** Loop-created beads land **`open`**, never auto-dispatched the same tick; only
   already-`ready` or rule-derived beads enter dispatch. *(Reuses CL-queue-pressure provenance.)*
3. **WIP ceiling == refill target == `max_concurrent`.** Over-aggression ("fan out into new work")
   is structurally impossible — the same gate that keeps the queue full caps it.
4. **At-most-once per source event.** Idempotency via `reacted_ledger` keyed by `event_id`, so a
   deploy+verify follow-up is spawned exactly once (no duplicate tails).

## How the two pieces together deliver the property
- A merged-but-undeployed fix is **simultaneously** (i) actionable work for the sentinel (C-1 → can't
  go idle on it) **and** (ii) auto-followed-up by the positive loop (C-2.a → the deploy+verify bead
  gets created). It **cannot be silently dropped**.
- Motion compounds: each completion tends to *create* its next unit of work (deploy → verify → close),
  so the backlog refills from progress instead of draining to idle-then-operator-restart.
- The guardrail keeps it from degenerating into self-generated busywork or a WIP blow-up.

## Open sub-questions for the operator (drive these)
- **C.i** Is the source set {a,b,c,d} right? Trim (e.g. drop logmine for v1) or extend?
- **C.ii** What is the **Phase-2 "verified" check** concretely — a smoke run? a `harmonik promote`
  deploy + health probe? per-class declared command? (This is the crux of "deployed+verified".)
- **C.iii** Does deploy+verify (source a) run **autonomously** in a lull, or only **stage** the bead
  for the captain to greenlight? (Ties to safety-gate scope, question F, and the no-auto-push rules.)
- **C.iv** Where does `done_definition` live — bead field? class config in `.harmonik/config.yaml`?
- **C.v** Minimal v1: ship **just source (a)** (deploy+verify tail) — the named top concern — and add
  b/c/d once observed? *(Lean yes — smallest slice that delivers the stated priority.)*
