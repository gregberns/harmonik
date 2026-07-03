# PLAN — State Source-of-Truth & Sync

> **STATUS: PLANNING DOC. NOT APPLIED.** Nothing here edits a live skill, mission,
> config, or code file. Every `[OPEN-Q]` is a place for the operator's red pen. This
> is the *system-state* companion to the admiral-framework's *agent-facing* half (see
> §5 — the two plans share one principle, REFRESH-THEN-ACT, applied at two altitudes).

---

## The operator's framing (captured faithfully, then designed)

> "Where is state REALLY stored for all this stuff, and how is it getting out of sync?
> If one agent is actively working on something, we don't want another agent touching
> it. But if an agent stops or doesn't correctly update the state, how do we identify
> that and get it updated? Could we go purely off of what's in main, and make the beads
> reflect that? But are handoffs then out of date? We might want this as its own whole
> plan. Maybe we start with a SMALL change now, then have a plan to think through a more
> comprehensive set of things."

Four distinct questions are bundled in there, and the plan keeps them separate:

1. **The map** — where each kind of state actually lives, and who is allowed to be
   right when two of them disagree.
2. **Mutual exclusion** — keep an agent actively-on-X from being stomped, *while still*
   detecting an agent that stopped and left state stale. These pull in opposite
   directions; the resolution is a liveness signal, not a lock.
3. **"Go purely off main"** — can git-main be the single truth and beads a pure
   projection of it? What does that do to HANDOFF freshness?
4. **Start small now, comprehensive plan to follow** — one low-risk high-signal change
   this week, then the scope of the bigger effort.

---

## Part 1 — The state map (WHERE state lives + its authority domain)

Harmonik's state is layered. The locked model (memory `State Source of Truth`,
user-confirmed 2026-04-21) is: **git is the completion authority; every other store is
a re-derivable cache; on a completion disagreement, git wins and the divergence is
surfaced, not silently reconciled.** The full inventory:

| # | Store | What it owns | Authority domain | Durability | Re-derivable from? |
|---|---|---|---|---|---|
| 1 | **Git (main / integration)** | what actually LANDED (commits, `Harmonik-Bead-ID:` trailers) | **Completion — ultimate decider.** | durable, externally verifiable | nothing; it is the root |
| 2 | **Beads (SQLite, `.beads/`)** | task ledger: open / in_progress / closed, deps, priority, assignee | terminal transitions (claim/close/reopen) — **the daemon owns these writes** | medium (committed JSONL) | partially, from git completion + run state |
| 3 | **JSONL event log** (`.harmonik/events/`) | workflow EXECUTION state: run_started, agent_heartbeat, run_completed, failure_class | live workflow / run state | medium (append-only log) | no — only place run-level execution lives |
| 4 | **Workflow graph (DOT)** | how execution happens: nodes, edges, policies | normative process definition | durable (in repo) | no — it's a design input |
| 5 | **Spec (`specs/`)** | design intent: what/why/acceptance | normative design | durable (in repo) | no |
| 6 | **HANDOFF.md / HANDOFF-\<role\>.md** | THIS-SESSION claims about where work stands | **none — a claim, not ground truth** | ephemeral (per session) | yes — fully derivable from 1+2+3 |
| 7 | **`.harmonik/context/` tier docs** (`project.yaml`, `captain-lanes.md`, admiral-initiatives, direction-log) | medium-term orchestration intent: lanes, parked/active, sequencing | orchestration intent (human-readable snapshot) | durable-ish (committed) | partially — *intent* isn't, *status* is |
| 8 | **Runtime control state** (`.harmonik/keeper/*.{sid,ctx,hold,managed}`, `queue.json`, comms cursors, registry, `daemon.sock`) | live process/session control | live, gitignored | ephemeral | yes — rebuilt at startup |

**The authority spine (read top-down on any disagreement):**
`git (1) ⟶ beads (2) ⟶ JSONL (3)` for *completion/execution*; everything below (6, 7)
is a **claim layer** that must be reconciled UP to the spine, never trusted over it.
Specs/DOT (4, 5) are an orthogonal *design* axis, not part of the completion spine.

### The concrete DESYNC MODES

These are the failure shapes the comprehensive plan must detect. Grouped by which pair
of stores disagrees.

**A. Beads ⟂ git (the classic, partially-handled today)**

- **A1 — Inverse premature-close (Cat 3c):** bead `in_progress` but its work already
  merged (a commit with `Harmonik-Bead-ID: <id>` exists on the target branch).
  *Status: HANDLED* by `harmonik reconcile` + the daemon's `RunOrphanSweep`.
- **A2 — Premature close:** bead `closed` but NO merge commit carries its ID — work was
  never actually landed (advisory-reviewer landed dead code, or a manual close). *Status:
  NOT auto-detected today.* This is the dangerous inverse of A1 — git would say "not
  done," beads says "done," and nobody is scanning for it. (See memory
  `Advisory REQUEST_CHANGES hides incomplete work`.)
- **A3 — Stranded in_progress with no live run:** bead `in_progress`, no merge commit,
  AND no live run-node activity (no recent `agent_heartbeat` / `run_started`). The agent
  stopped without finishing. *Status: PARTIALLY handled* — the daemon auto-resets
  stranded beads to `open`, but the VERIFY-before-reset hazard is real (memory
  `in_progress + claim-skip flood`): a live run can carry `bead_id=None`, so a naive
  "no activity for this bead" check false-positives and resetting double-dispatches.

**B. JSONL ⟂ git/beads**

- **B1 — Committed-but-not-reconciled run:** a run failed (`context_cancelled`,
  reviewer-stall) yet the implementer already committed a `Refs:<bead>` SHA on
  `run/<id>`. JSONL says "failed," git has the work. *Status: salvage procedure exists
  (`harmonik promote`), not auto-applied.*
- **B2 — Silent-hang liveness ambiguity:** a run looks alive (window exists) but the
  agent idled forever (mid-run API 500). Authoritative liveness is
  `agent_heartbeat` + `implementer_phase_complete` recency in JSONL, NOT tmux
  window/mtime (memory `tmux window/mtime = false wedge signal`). A desync detector that
  reads the wrong signal manufactures false positives.

**C. Claim layer (HANDOFF / tier docs) ⟂ the spine**

- **C1 — HANDOFF stale vs main:** HANDOFF says "X in progress / X blocked" but X landed
  (git) or X's bead is closed (beads). The handoff is a snapshot from before the work
  moved. *This is the operator's "are handoffs then out of date?" — answer: structurally
  YES, by design; HANDOFF is a claim layer (row 6) and is expected to lag.*
- **C2 — Doc-says-PARKED but the lane is actually done/active:** `captain-lanes.md` or
  `admiral-initiatives.md` marks a lane PARKED while its epic is closed (done) or a crew
  is actively on it (active). This is the exact drift the admiral-framework retro found
  (the 2026-06-19 scale-out block, the parked-reads-as-gated trap). *Status: NOT
  auto-detected.*
- **C3 — Dated directive expired-but-present:** a tier-2 directive block with an implicit
  or past expiry that nobody struck, silently still acted on. (admiral-framework Part
  1(b) / OPEN-Q 2.)

**D. Runtime control ⟂ session reality**

- **D1 — Keeper `.sid` flips on `/clear`:** the launch session-id goes dead after the
  first keeper `/clear`; control files keyed off a stale id (memory
  `keeper session_id flips on /clear`). *Partially handled* via `.sid` re-resolution.
- **D2 — Dormant captain absent from comms `who`:** post-wake-economy, a dormant captain
  drops out of presence, tripping a false `captain_down` (memory
  `Dormant captain comms-presence gap`).

**The two modes the operator named explicitly map to: A3 (in_progress with no run), C1
(HANDOFF stale vs main), C2 (doc says PARKED but lane is done).**

---

## Part 2 — Mutual exclusion vs stale-state detection

The operator's tension, stated precisely: **"don't let agent B touch what agent A is
actively working on" (mutual exclusion) directly fights "detect agent A stopped and
left state stale" (liveness).** A strict lock satisfies the first and *breaks* the
second — a crashed lock-holder wedges the work forever. A loose lock satisfies the
second and breaks the first — a slow-but-alive agent gets stomped. The resolution is not
a binary lock; it is **a claim backed by a liveness signal with a timeout**.

### What already provides mutual exclusion today

- **`in_progress` is the claim.** The daemon sets it on dispatch; agents must NOT pre-set
  it (memory `Daemon submit: don't pre-set in_progress` — pre-setting trips a false
  `bead_already_dispatched`). So beads-status *is* the lock, and only the daemon writes
  it. Good: single writer.
- **`br ready` hides claimed/blocked beads**, so a second dispatch won't pick a claimed
  bead. Good: the lock is respected at selection time.
- **Worktree isolation** keeps two agents from physically colliding in the tree.

### The gap

The lock (`in_progress`) has **no liveness backing in the general case.** The daemon's
stranded-bead auto-reset is the liveness-timeout half, but it relies on run-activity
correlation that has the `bead_id=None` blind spot (desync A3). So today: a *cleanly
crashed* agent is recovered (auto-reset), but a *silently hung* agent (B2) or a run whose
events don't carry the bead_id can either wedge (no recovery) or get double-dispatched
(premature reset). **The lock and the liveness signal are not reliably joined.**

### The right primitive: lease, not lock

A bead claim should be a **lease** = `(claim, holder, last-heartbeat, ttl)`. While the
holder emits `agent_heartbeat` within ttl → the lease is live, mutual exclusion holds,
nobody else touches it. When heartbeats stop for > ttl → the lease is **expired**, and
*only then* is the bead a reset candidate. This makes "actively working" and "stopped and
stale" the **same mechanism at two points on a clock**, which is what dissolves the
operator's tension. The authoritative heartbeat already exists (`agent_heartbeat` +
`implementer_phase_complete` in JSONL, memory-confirmed). The missing piece is **joining
that heartbeat to the bead-claim reliably** (fixing the `bead_id=None` correlation so the
run that holds the lease is unambiguous).

### Evaluating the operator's periodic-reconciler-agent idea

The operator proposes a periodic agent (the ~30-min crew-watch cadence, or a spawned
sonnet/opus) whose job is to verify relevant state against ground-truth (main + beads).
**Assessment: yes, but with a sharp division of labor between deterministic and
agentic.**

- **What should be DETERMINISTIC (Go, not an agent):** every desync detector that is a
  pure query — A1/A2/A3 (scan git trailers vs bead status vs heartbeat recency),
  C1/C2 (diff HANDOFF/tier-doc claims against git+beads), C3 (expired-directive flag).
  These are `grep`-shaped; an LLM adds cost, latency, and nondeterminism for no benefit.
  This is the locked principle "deterministic done-check beats reviewer" applied to
  state. The deterministic layer should **emit a drift report**, not act.
- **What should be AGENTIC (the investigator, per the locked reconciliation model):**
  the *resolution* of an ambiguous case (Cat 2/3/6 in memory `Reconciliation`) — read
  WIP files, read the session log, decide `reopen-bead` / `accept-close-with-note` /
  `escalate`. This is judgment, and it's already designed as a harmonik workflow.
- **The periodic agent's real job, then, is THIN:** run the deterministic drift report
  on a cadence, and for each *non-trivial* drift, spawn (or queue) the investigator
  workflow. It is a scheduler + triager, not the detector and not the resolver. Cheapest
  model that can route (haiku) suffices for the triage step; the investigator picks its
  own model.
- **Cadence:** fold into the existing **watch tier** (always-on Sonnet consuming the bus)
  rather than a new 30-min cron — the watch already has the right shape (record, triage,
  escalate event-driven) and the wake-economy cutover deliberately killed standalone
  polling crons. The watch runs the deterministic drift report and escalates only
  *actionable* drift. `[OPEN-Q A]`

**One hard safety rule, from the existing scars:** the reconciler must **VERIFY before
it resets** (memory `in_progress + claim-livelock`): prove no live run-node activity for
the bead (handling `bead_id=None`) before treating an `in_progress` as stranded.
Resetting a bead that's mid-commit double-dispatches. Detection writes a *report*;
*mutation* (reset/close) goes through the lease-expiry check or the investigator verdict.

---

## Part 3 — "Go purely off main" + make beads reflect it

The operator's proposal: treat git-main as the single truth and make beads a projection
of it (auto-reconcile beads from what merged). Evaluated honestly:

### Pros

- **It's already the locked model for COMPLETION** (memory `State Source of Truth`):
  git wins, beads is a cache. "Go off main for done-ness" is not a new decision; it's
  finishing one already made. `harmonik reconcile` is the first 20% of it.
- **Kills A1 and A2 simultaneously.** If beads-closed ⟺ a merge commit with the bead's
  trailer exists, then both "closed but not merged" (A2) and "merged but not closed"
  (A1) become detectable/auto-correctable by one scan. Git-main becomes the arbiter both
  directions.
- **Removes a whole class of "which store is right" arguments.** One arbiter, mechanical.
- **Externally verifiable.** Git history is the only store auditable without harmonik
  running.

### Cons / limits

- **Main only knows COMPLETION, not EXECUTION or INTENT.** Git cannot tell you a bead is
  *in_progress* (no commit yet), *which run* owns it, *why* it's parked, or *what order*
  to resume lanes. So "purely off main" can make beads reflect **done-ness**, but the
  **claim/lease state (Part 2) and the orchestration intent (tier docs) are NOT derivable
  from main.** Going "purely off main" for those would erase exactly the WIP-protection
  the operator wants in question 1.
- **Trailer discipline becomes load-bearing.** The whole scheme rests on every landed
  commit carrying `Harmonik-Bead-ID:`. A squash-merge that drops the trailer, or
  human-side cherry-picks, would make real work invisible to the projection and
  auto-*reopen* a legitimately-closed bead. The projection is only as good as the trailer
  coverage. `[OPEN-Q B]`
- **A pure projection deletes manual annotations.** "Closed with note: salvaged" or
  "closed as won't-fix" don't correspond to a merge commit; a naive projection would
  reopen them. The projection needs an *exception channel* for legitimately-closed-without-
  merge beads.

### What this does to HANDOFF freshness

This is the operator's "but are handoffs then out of date?" — and the answer is the
cleanest part of the plan:

> **HANDOFF and the tier docs become DERIVED and DISPOSABLE, not authoritative — and
> that is the correct direction, it's already the stated model (HANDOFF is "a claim, not
> ground truth").**

Concretely: if git+beads (the spine) is the single truth for *what's done* and the
lease layer is the truth for *what's actively held*, then a HANDOFF is just a *cached
human-readable view* of the spine at session-end. It being stale (C1) stops being a bug
and becomes *expected* — you don't trust it, you **regenerate it** from the spine on
boot. This is precisely the admiral-framework's REFRESH-THEN-ACT at boot: "the live boot
digest overrides the HANDOFF claim." Going-off-main *strengthens* that: the boot digest
becomes computable (diff HANDOFF vs spine, show only the drift), so the stale-handoff
problem self-heals every boot instead of festering.

**The nuance to preserve:** intent (the WHY and the RETURN-PATH, admiral-framework's
direction-log) is **NOT** derivable from main and must stay authoritative-where-written.
So the rule is split:
- **Status/done-ness in HANDOFF/tier docs → derived from the spine, disposable, regenerated.**
- **Sequencing intent (direction-log) → authoritative, never derived, survives `/clear`.**

That split is the whole answer to "purely off main": **yes for status, no for intent.**

---

## Part 4 — The SMALL change now + the COMPREHENSIVE plan

### SMALL change to start NOW (low-risk, high-signal, read-only)

**Add a `--report` (drift-only, non-mutating) mode that extends `harmonik reconcile`
into a full desync detector covering A1+A2+C1+C2 — and emits a report, mutating
nothing.**

Why this one:
- **Lowest risk:** read-only. It scans git trailers, bead status, HANDOFF claims, and
  tier-doc lane markings, and prints a drift report. It cannot double-dispatch, cannot
  reset, cannot close. (`reconcile` already does the git-trailer scan for A1; this adds
  the inverse A2 check and the doc-vs-spine diff C1/C2, behind a flag that suppresses the
  existing close.)
- **Highest signal:** it immediately surfaces the three desync modes the operator named
  (A3-adjacent, C1, C2) plus the dangerous-but-unmonitored A2 (closed-but-not-merged),
  with zero behavior change to the running fleet.
- **Composable next step:** once the report is trusted, the watch tier (Part 2) calls it
  on cadence and escalates actionable drift — no new subsystem, just a scheduled
  invocation of a read-only command.

Scope of the small change (concrete):
1. `harmonik reconcile --report` (or `harmonik drift`): emit, don't mutate. `[OPEN-Q C — extend reconcile vs new verb]`
2. Checks: A1 (merged-not-closed, already have it), A2 (closed-no-trailer), C1 (HANDOFF
   lines naming a bead/lane whose spine-state contradicts the claim), C2 (tier-doc lane
   marked PARKED whose epic is closed OR has a live crew).
3. Output: a short grouped report (`drift: A2 x2, C1 x1, C2 x1`) + machine-readable
   `--json`. Each row names the *thing* (bead + what git says vs what beads says), not
   just an ID, so the report is operator-readable per the plain-English rule.
4. **No reset, no close, no doc edit.** Mutation stays in the existing `reconcile` (A1)
   and the lease/investigator path (later).

This is a few-hours change, gated by the review-on-every-commit rule, and it gives the
operator the visibility to decide what the comprehensive plan should actually fix.

### COMPREHENSIVE plan (the scope to follow, NOT to build now)

A kerf work, sequenced:

1. **Lease layer (Part 2).** Promote the `in_progress` claim to a heartbeat-backed lease.
   Fix the `bead_id=None` correlation so a run's heartbeat is unambiguously joined to the
   bead it holds. This is the core fix — it makes mutual-exclusion and stale-detection
   one mechanism. Highest design risk; needs spec-first.
2. **Bidirectional beads⟷main projection (Part 3).** Generalize `reconcile` to both
   directions with the trailer-coverage + manual-close exception channel. Make
   beads-done-ness a true projection of main.
3. **Reconciler scheduling in the watch tier (Part 2).** Wire the `--report` detector to
   the watch's cadence; route non-trivial drift to the investigator workflow (the locked
   Cat 1-6 model already specifies the resolution playbooks).
4. **HANDOFF-as-derived (Part 3).** Make boot regenerate the HANDOFF view from the spine
   (the boot digest already does half of this); formalize "status derived, intent
   authoritative" so C1 self-heals every boot.
5. **Doc-drift auto-flag (C2/C3).** Fold the tier-doc-vs-spine and expired-directive
   checks into the admiral audit (this *is* admiral-framework Part 1(b)/Part 3 item 6 —
   the two plans converge here).

---

## Part 5 — Open questions + relation to the admiral-framework

### How this relates to the admiral-framework v1 plan

The admiral-framework (`plans/2026-06-25-admiral-framework/STRAWMAN-v0.md`) and this plan
are **two halves of one idea**, and they share a single principle:

> **REFRESH-THEN-ACT** (admiral-framework §2.2): "Before acting on any durable doc,
> refresh the actual state, reconcile/update the doc if it has drifted, THEN act."

- **Admiral-framework = the AGENT-FACING half.** It tells an *agent* "don't trust the
  stale doc; refresh the one fact you're betting on before you act." It's a behavioral
  discipline, applied by judgment, kept light.
- **This plan = the SYSTEM-STATE half.** It builds the *machinery* that makes "refresh
  the actual state" cheap, reliable, and mostly automatic: the lease layer, the
  beads⟷main projection, the deterministic drift detector. REFRESH-THEN-ACT is only as
  good as the cost of refreshing — this plan drives that cost toward zero so the agent's
  refresh is a fast deterministic query, not a manual re-derivation.

They **converge** at Part 4 step 5 / admiral-framework Part 1(b): doc-vs-spine drift (C2)
and expired-directive flagging (C3) are *the same detector*, owned by the admiral audit.
Build it once; both plans point at it. The clean division: **admiral-framework owns the
behavioral principle and the intent artifacts (direction-log); this plan owns the
detectors, the lease, and the projection that the principle stands on.**

### Open questions (top 3 first)

1. **`[OPEN-Q A]` — Reconciler home: watch tier vs standalone agent vs daemon-internal.**
   The deterministic detector could run inside the daemon (cheapest, always-on, no LLM),
   in the watch tier (already has triage/escalate shape), or as a spawned periodic agent
   (operator's original framing). Recommendation: **detector in the daemon/CLI
   (deterministic), scheduling + escalation in the watch tier, resolution in the
   investigator workflow.** Confirm this three-layer split.

2. **`[OPEN-Q B]` — Trailer-coverage as the load-bearing invariant.** The whole
   beads⟷main projection rests on every landed commit carrying `Harmonik-Bead-ID:`. Is
   trailer coverage actually 100% today (squash-merge, human cherry-picks, salvage-promote
   all preserve it)? If not, the projection needs a fallback before it can auto-mutate
   beads, or it will reopen real work. This needs a measurement before Part 4 step 2.

3. **`[OPEN-Q C]` — Lease ttl + the `bead_id=None` fix scope.** What heartbeat ttl makes
   a lease "expired" without false-positiving on a slow-but-alive agent (the silent-hang
   B2 case sits right at this boundary)? And is fixing `bead_id=None` correlation a small
   patch (stamp the bead_id onto run events) or a deeper run-model change? This gates the
   entire lease layer (Part 4 step 1).

Secondary:
4. Extend `reconcile --report` vs a new `harmonik drift` verb for the small-now change.
5. The manual-close exception channel (Part 3) — how does a legitimately-closed-without-
   merge bead (won't-fix, salvaged-elsewhere) signal "don't reopen me" to the projection?
6. Does "status derived / intent authoritative" (Part 3) need a spec change to the locked
   three-stores model, or is it already implied by "HANDOFF is a claim"?
