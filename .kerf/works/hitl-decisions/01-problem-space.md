# hitl-decisions — Problem Space

> **Status: problem-space FINALIZED (liet, 2026-06-11).** Captain re-tasked this as a **DESIGN-ONLY** push to a change-spec — the agent→human dual of the FINALIZED `agent-comms` bus. The three formerly-open questions (Q1 ownership seam, Q2 v1 surface, Q3 resolution semantics) are **RESOLVED with their proposed defaults** below — each is the option most consistent with constraints C1–C6; rationale recorded inline. Captain/operator may **redirect any pick before a commit lands** (none will until the change-spec is reviewed). No implementation is in scope — paul owns daemon-infra impl. *(Meta-note: stalling this work on a buried Q1 would be the exact failure hitl-decisions exists to prevent — so the decisions are taken, surfaced, and left open to redirect: the very pattern this work formalizes.)*

**Bead:** hk-0zxv6 (P3, feature) · **Codename:** hitl-decisions · **Jig:** plan
**Parent context:** the **agent-to-HUMAN dual** of `agent-comms` (hk-uxm0j, CLOSED) — same event bus, same typed-message + durable-cursor + dedupe-on-`event_id` model, but routed to and from the human and tracked as a *decision-with-resolution*. Sibling of `session-keeper` (hk-ekap1): an agent blocked-on-a-decision is a supervisor-known state.

## Summary

Agents work through issues and periodically hit a **decision point** that needs a human — a product call, an authorization, a pick among options. Today that decision-context is **buried in a long agent thread**: the human sees it only by actively reading that thread, the agent stalls or guesses, and the decision can get lost. Nothing aggregates open decisions-awaiting-a-human across agents and works. `hitl-decisions` makes the decision point a **first-class, addressable surface**: an agent emits a structured `decision_needed` event (question + concrete options + context-link + who-is-blocked) on the existing bus and stalls cleanly; harmonik/kerf aggregate all open decisions into **one cross-agent "what-needs-me" queue**; the human answers with one command; the answer routes back as a `decision_resolved` event and unblocks the waiting agent.

## Goals

- **G1.** Define a typed `decision_needed` event an agent can emit at a HITL point, carrying: question, concrete options, free-form context-link (bead/work/thread/run), and who-is-blocked.
- **G2.** Let a stalled agent **block cleanly** on its own `decision_needed` and resume on the matching `decision_resolved` — no busy-wait, no guessing, no silent drop.
- **G3.** Aggregate all *open* decisions across every agent/work into **one** human-facing "what-needs-me" view (the anti-burial property).
- **G4.** Provide a single human command to **answer** a decision (pick an option / supply a value), routing the result back to the blocked agent.
- **G5.** Track each decision's lifecycle (open → resolved | withdrawn) durably, so the queue is the source of truth and survives session restarts.
- **G6.** **No zombie decisions.** Every open decision has a defined terminal path even when the blocking agent never returns: an agent may **withdraw** its own decision when it self-obsoletes; an **orphaned** open decision (the agent died/restarted and isn't waiting anymore) is reaped by the daemon/session-keeper, not left to accrete. The anti-burial property must survive agent death — otherwise the queue becomes the burial.

## Non-goals

- **NG1.** **Not** a general approvals / workflow / BPM engine. Scope is a flat list of open point-decisions with a resolution slot — no routing rules, no multi-approver chains, no SLAs/escalation policies.
- **NG2.** **Not** a GUI. The surfaces are CLI (`harmonik` / `kerf`) plus the existing event log; any richer UI is out of scope here.
- **NG3.** **Not** a replacement for `agent-comms` (hk-uxm0j). hitl-decisions *builds on* that bus; it adds a typed event + a human-addressed resolution flow, it does not re-implement transport, cursors, or dedupe.
- **NG4.** **Not** auto-deciding. The system surfaces and routes; it never answers on the human's behalf or applies a default-on-timeout in v1.
- **NG5.** **Not** redesigning bead/work state machines. A "blocked-on-human" marker may *reference* a decision, but inventing a new normative bead lifecycle is out of scope (see Q1).

## Constraints

- **C1.** Builds on the **existing event bus** (hk-uxm0j): `decision_needed` / `decision_resolved` are new typed events on the same `events.jsonl` transport, delivered via `harmonik subscribe`. No new transport.
- **C2.** Respects the bus delivery contract: **at-least-once** delivery → consumers (including the human-answer path and the blocked agent) **MUST dedupe on `event_id`** (N3). A decision must resolve exactly once even if the resolve event is delivered twice.
- **C3.** **No new always-on service if avoidable.** Aggregation should be a *query/projection* over the durable event log (or beads ledger), computable on demand by a CLI command — not a long-lived aggregator daemon. Reuse the harmonik daemon if any process is needed.
- **C4.** Durable store reuses what exists: the **event log** for the message stream and/or the **beads ledger** for open-decision tracking (so the queue survives restarts). No new datastore.
- **C5.** Plays inside the orchestrator rules: agents MUST NOT issue terminal bead transitions (the daemon owns those); a decision marker on a bead, if used, must respect that write discipline.
- **C6.** A blocked agent's "wait" must be idle-safe and **distinguishable from a hang**: an agent with an open `decision_needed` and no matching `decision_resolved` is in a *known blocked-on-decision* state. `session-keeper` MUST consult the open-decision projection before reaping such an agent — blocked-on-decision is a legitimate idle state, a hang is not. (If a blocked agent IS reaped, its decision becomes orphaned → G6 reap path.)
- **C7.** Every decision carries a stable, unique **`decision_id`** (distinct from `event_id`). Resolution/withdrawal is keyed on `decision_id` and is **idempotent**: resolving an unknown or already-terminal decision is a no-op, beyond the per-event `event_id` dedupe of C2. v1 assumes a **single human** answerer (first-writer-wins on a `decision_id`); multi-human arbitration is out of scope.

## Design decisions (adopted — captain/operator may redirect before commit)

> These were the genuine human-intent calls. Each is now **resolved to the option most consistent with constraints C1–C6**; the alternatives and their consequences are preserved as rationale for the change-spec. Surfaced to captain at problem-space-done with a redirect window.

### D1 — Ownership seam → **(a) harmonik/comms layer owns the mechanism; kerf & session-keeper reference it.** ADOPTED.

The `decision_needed`/`decision_resolved` events, the cross-agent "what-needs-me" aggregation, and the answer-routing flow are **one mechanism with one home: the harmonik bus + daemon.** *Why:* it reuses the existing bus, daemon, and named-queues supervise lane with zero new transport or service (C1, C3, C4); it's the agent→human dual of agent-comms, which already lives there; session-keeper just treats "blocked-on-decision" as a known idle state (C6), and kerf consumes the same projection as a reader (D2) without owning transport.
- *Rejected (b) split events-in-harmonik / view-in-kerf:* splits one mechanism across two codebases + release cadences; needs a fragile seam contract.
- *Rejected (c) new standalone surface:* cleanest boundary but risks a parallel mini-bus/aggregator → violates C3. Least preferred.

### D2 — v1 surface → **(c) both, ONE shared event contract, harmonik-side FIRST.** ADOPTED (sequenced).

harmonik emits/routes + ships `harmonik decisions` (list/answer) first — it's closest to the buried-in-thread evidence and serves agents running under the daemon. The kerf cross-works "what-needs-me" view reads the **same** projection second. *Why:* full coverage on one event contract; v1 ships the high-evidence surface without waiting on the kerf integration.
- *Rejected (a) kerf-only:* weak for non-kerf-attached running agents. *Rejected (b) harmonik-only:* misses kerf-work-level decisions — keep it as the v1 *first step*, not the end state.

### D3 — Resolution semantics → **(a) pick-an-option, decisions stay open until answered (no auto-default in v1).** ADOPTED, with a v1.1 hook.

Enumerated choices only; a decision lives until a human answers (the aggregation queue is why nothing is lost). *Why:* simplest, fully testable, honors NG4 (never auto-decide). **v1.1 hook:** the event schema reserves an optional free-text `value` field (the (b) capability) so authorizations/values land without a schema break — but v1 parsing accepts options only.
- *Rejected for v1 (c) timeout/default policy:* reintroduces auto-deciding (tension with NG4) — deferred past v1.

### D4 — Durable home → **event-log projection is the source of truth; an optional bead marker is daemon-written only.** ADOPTED. (Resolves the C5/NG5 tension.)

The open-decision set is a **projection over `events.jsonl`** — `decision_needed` minus `decision_resolved`/`decision_withdrawn`, deduped on `event_id`, keyed on `decision_id` — computed on demand (C3, C4). This is the source of truth. A bead "blocked-on-human" *marker* is **optional and derived**: if used to make a decision visible in kerf/bead views, **only the daemon writes it** (agents emit events, never terminal bead transitions — C5/orchestrator rule). So an agent's act is purely "emit `decision_needed`"; any bead-state reflection is the daemon's projection of the event stream.
- *Why not bead-ledger as primary:* would force agents toward bead writes (violates C5) and couples the decision surface to bead lifecycle (NG5). Event-log-primary keeps the agent side write-free and the durable set restart-survivable without a new datastore.

## Success criteria (concrete, verifiable)

- **S1.** An agent can emit a `decision_needed` event carrying question + concrete options + a context-link + who-is-blocked, and then **stall cleanly** (no busy-wait, no crash, no guess). *Test: emit the event, assert the agent enters a blocked state and the event lands in `events.jsonl`.*
- **S2.** A human can run **one command** to see **all** open cross-agent decisions in one list (question, options, who's blocked, context-link). *Test: emit decisions from two distinct agents/works, assert both appear in the single command's output.*
- **S3.** Answering a decision with one command routes a `decision_resolved` event back and **unblocks the originating agent**, which resumes with the chosen option. *Test: answer S1's decision, assert the agent wakes and proceeds with the selected value.*
- **S4.** A decision resolves **exactly once** even if the resolve event is delivered more than once (dedupe on `event_id`). *Test: replay the resolve event; assert no double-apply and no second wake.*
- **S5.** The open-decision queue is **durable** — it reflects the same open set after a daemon/session restart, with resolved decisions removed. *Test: emit, restart, assert the queue is unchanged; resolve, assert it drops out.*
- **S6.** Aggregation requires **no new always-on service** — the "what-needs-me" view is computed on demand from the durable log/ledger (or via the existing harmonik daemon). *Test: produce the view with the aggregator-as-process absent.*
- **S7.** **Orphan + agent-survival.** (a) A `decision_needed` whose agent has died/restarted and is no longer waiting is reaped to a terminal `withdrawn` state and drops out of the open queue (no zombie). *Test: emit a decision, kill the agent, assert the daemon/session-keeper withdraws it and it leaves the queue.* (b) An agent that emitted a decision and then restarted re-establishes its wait and still resolves on the answer (or is cleanly withdrawn). *Test: emit, restart the agent, answer, assert it wakes — or assert clean withdrawal if it didn't re-establish.*
- **S8.** **Idempotent resolution.** Resolving the same `decision_id` twice, or resolving an unknown/already-withdrawn `decision_id`, is a no-op with no second wake and no error. *Test: answer a decision, replay the answer + answer a bogus id; assert single apply, single wake, clean no-ops.*

## Related

- **hk-uxm0j** — agent-comms bus (CLOSED): shared transport, durable cursor, dedupe-on-`event_id`. hitl-decisions is its agent-to-human dual.
- **hk-ekap1** — session-keeper: an agent blocked-on-decision is a supervisor-known idle state; the two must not fight (C6).
