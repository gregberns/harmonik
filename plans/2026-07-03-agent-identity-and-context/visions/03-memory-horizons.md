# Vision 3 — Memory Horizons & Identity Homeostasis

The single-HANDOFF model conflates three things that decay at different rates: *identity* (never),
*role-cohesion* (days), *episodic state* (one session). Bundling them means the durable stuff rots
at the speed of the volatile stuff — exactly the observed drift. Split by **time horizon**; make
identity a *maintained equilibrium*, not a remembered fact.

## (a) Memory-horizons model — four streams, declared per agent
Each agent declares a `horizons:` block naming streams it reads (R) / writes (W). A stream = named
file + write cadence + owner.

| Stream | Horizon | Owner (W) | Readers (R) | Cadence |
|---|---|---|---|---|
| **PIN** (identity) | eternal | role author (checked-in) | self | never rewritten — only re-*read* |
| **EPISODE** (handoff) | this session | self | self | on keeper `/clear` |
| **LEDGER** (role cohesion) | multi-day rolling | self, append-mostly | self + peer role | end of each audit/loop cycle, not each session |
| **FLEET** (shared) | ambient | many | many (subscribed) | event-driven (comms bus) |

Key move: **EPISODE and LEDGER are different files with different lifetimes.** EPISODE is
replaced every restart. LEDGER *survives restarts untouched* — a restart reads it but does NOT
trigger a rewrite; it changes only when the agent completes a unit of role-work (an audit, an
epic-landing). So it accumulates at the role's natural rhythm, not the context window's. That's why
the admiral "forgets its program view": today it lives in the same HANDOFF churned every ~30k
tokens, so multi-day signal gets averaged out by single-session noise. Separate streams → churn
can't touch slow memory. **`admiral-initiatives.md` already IS the admiral's LEDGER** — right
cadence already ("update each audit"); the design just *names* it a horizon and forbids the restart
cycle from rewriting it.

## (b) Admiral multi-day cohesion, concretely
- **Artifact:** `admiral-initiatives.md` (LEDGER) + a thin `cohesion-digest` — a ≤15-line
  *deterministically generated* (not LLM-written) head-block prepended at each resume:
  `ACTIVE:3 · ON-DECK:5 · oldest-unreconciled: 4 audits ago · knobs:{concurrency=4, DOT=sonnet-triple}`.
- **Updated when:** the admiral closes an audit cycle (its loop rhythm), NOT when the keeper fires.
  One audit = one append to a dated ledger-log + one in-place status flip. Mid-audit restarts resume
  from EPISODE; LEDGER untouched until the audit *finishes*.
- **By whom:** admiral writes; ops-monitor writes the deterministic digest head-block (slow-memory
  summary can't be confabulated). Captain may **read** the admiral's LEDGER (R) but never writes it —
  that read is how "program view" propagates down without a message.
- **Guarantee:** LEDGER write-cadence decoupled from restart-cadence → N restarts in 6h produce
  *zero* spurious LEDGER edits. Drift-by-recopying can't occur in a stream nothing recopies.

## (c) Identity homeostasis — what re-pins, and how
Homeostasis ≠ memory. Memory answers "what happened"; homeostasis answers "what am I" and must be
**re-asserted from an external immutable source every cycle** so error can't accumulate. Today
KEEPER-IDENTITY (`cycle.go:456`) already re-pins on `/clear` — but re-pins it *to the HANDOFF*, the
drifting file. **That's the bug: the setpoint is downstream of the thing that drifts.** Re-point the
re-pin at the immutable PIN stream. Three parts:
1. **PIN is checked-in, read-only to the agent:** `I am <role>. My job is <X>. I do NOT do <Y — the
   adjacent role's job>. I escalate to <Z>.` The negative clause is load-bearing — "admiral does NOT
   dispatch crews / edit code; that's the captain" is what stops role-bleed.
2. **Re-pin on every `/clear`:** keeper injects PIN verbatim (not paraphrased, not merged with
   state) as the first block of the resume seed — reuse the existing injection point, swap its source
   HANDOFF→PIN. Immutable file → six hours of restarts inject the *identical* setpoint; no integrator to wind up.
3. **Drift *sensor* + corrective (not just a setpoint):** at resume, a cheap deterministic check
   compares recent actions against PIN's negative clause — e.g. "did this session's comms log contain
   a `queue submit` (a captain verb) while role=admiral?" A hit fires an `identity_violation`
   self-signal that re-injects PIN with the boundary bolded. Today there's a setpoint but **no
   sensor** — drift is only caught by the operator hours later.

## (d) Per-agent handoff differences (fall out of horizon-declaration, not special-cased)
- **Crew:** EPISODE only. No LEDGER (no multi-day role); reads FLEET narrowly.
- **Captain:** EPISODE + reads admiral's LEDGER. Cohesion via *reading up*, not its own slow memory.
- **Admiral:** thin EPISODE + heavy LEDGER. "Handoff" is 90% LEDGER-digest, 10% episode.
- **Watch:** EPISODE nearly empty (event-driven); almost pure FLEET-reader.
One schema, four horizon-declarations — no per-type handoff code.

## (e) Constraint to DELETE
**"The handoff is the identity carrier across restarts."** Stop re-pinning KEEPER-IDENTITY from
HANDOFF (`cycle.go:456`). Identity must never travel in the same file as episodic state — that
coupling *is* the drift mechanism. Once PIN is the setpoint and EPISODE is disposable, the "agent
forgot its job after many restarts" failure has no substrate.
