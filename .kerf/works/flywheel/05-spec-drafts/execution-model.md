# Execution-model.md — flywheel change-set (draft)

> Spec-draft for `specs/execution-model.md` additions. Source: spec-draft sub-agent (sonnet), 2026-05-30. Three changes: (1) EM-NOTE-WAKE + EM-NOTE-STREAM-CONCURRENCY corrections inline with §4.11; (2) §4.13 eager-refill obligation (EM-062–EM-063); (3) §4.14 check-observed-before-submit guard (EM-064–EM-065).

## Change-set summary
Adds three sections. (1) EM-NOTEs correcting two stale guidance points: hk-24xn1 (wake-on-submit/append) is **closed** and earlier CLAUDE.md "append while idle sits pending" guidance is stale; stream groups DO run concurrently at `max_concurrent>1` (V2 correction — `streamEligible()` skips dispatched items, doesn't HOL-block). (2) §4.13 formalizes the daemon's eager-refill duty (on `run_terminal` OR poll, pull `available` candidates from `kerf next`, pre-screen, append to active stream group). (3) §4.14 formalizes the orchestrator's check-observed-before-submit guard (4-tier authority chain ending at events.jsonl). All pure-code (no LLM); over-aggression structurally impossible (ceiling==target==`max_concurrent`).

---

### Change 1: EM-NOTE inline to §4.11 (after EM-051) — wake-on-submit/append + stream concurrency correction

Insert immediately after §4.11.EM-051, before §4.12:

```markdown
> **EM-NOTE-WAKE (informative, 2026-05-30).** hk-24xn1 (daemon wake on submit/append) is **closed**. `QueueStore.SetQueue` (called on every accepted `queue-submit` and `queue-append`) signals `wakeC` via a buffered-1 channel; the workloop's `workloopSleep`/`workloopIdleWait` helpers select on `wakeC` alongside the poll timer (`internal/daemon/queuestore_hkj808w.go:97-104`). A newly submitted or appended item wakes the workloop at sub-poll-interval latency; no poll-interval wait. Earlier CLAUDE.md guidance ("append while idle sits `pending` until the next workloop tick") was accurate before hk-24xn1; it is now stale. §4.13 eager-refill relies on this sub-poll-interval guarantee.
>
> **EM-NOTE-STREAM-CONCURRENCY (informative, 2026-05-30; V2 correction).** A stream group at `max_concurrent > 1` DOES run multiple items concurrently. `streamEligible()` (`internal/queue/state.go:283`) SKIPS `dispatched` items — does not treat them as HOL blockers. The HOL rule (QM-035) applies only when the head is `deferred-for-ledger-dep`; a `dispatched` head is skipped and the next `pending` item is returned eligible. Earlier "use `--wave` when `--max-concurrent > 1`" guidance was INCORRECT and is SUPERSEDED. `--wave` is for immutable-at-submit group semantics (no mid-flight appends), NOT for enabling concurrency. Stream groups support both concurrency AND mid-flight append; wave groups support concurrency but are append-closed.
```

---

### Change 2: New §4.13 — Eager Refill Obligation

Insert as new §4.13 after §4.12, before §5.

```markdown
## 4.13 Eager refill obligation

This section formalizes the daemon's duty to keep the active stream group's in-flight count as close to `max_concurrent` as possible from the existing ready queue, without crossing the ceiling and without auto-dispatching beads the orchestrator just created. The refill path is **pure code** (mechanism-tagged, no LLM). LLM intervention is outside this section's scope (it belongs to the orchestrator-level replenishment surface; see `docs/orchestration-protocol-v2.md`). The daemon's role here: when a slot opens, fill it from the ready queue; nothing more.

#### EM-062 — Eager-refill trigger and compute
On every `run_terminal` event (after `finalize_run` per §7.4 completes) AND on every dispatch-loop poll tick, the daemon MUST evaluate:
```
FUNCTION eager_refill_eval():
    IF active_queue() IS None: RETURN
    IF active_queue().status NOT IN {active}: RETURN
    group = active_queue().active_group()
    IF group IS None OR group.kind != "stream": RETURN
    available = max_concurrent - in_flight_count()       # §4.11 EM-049
    IF available <= 0: RETURN                            # WIP cap full; hard stop
    pending_in_group = COUNT(item FOR item IN group.items WHERE item.status == "pending")
    deficit = available - pending_in_group
    IF deficit <= 0: RETURN                              # existing pending will fill; no action
    candidates = pre_screen(kerf_next(limit = deficit * OVERFETCH_FACTOR))   # EM-063
    eligible = candidates[: min(deficit, len(candidates))]
    IF eligible IS NOT EMPTY:
        queue_append(active_stream_group=group, bead_ids=eligible)   # [queue-model.md §7 QM-040]
```
`OVERFETCH_FACTOR` SHOULD be 2 at v1 (pre-screen rejections don't leave an avoidable gap). Compile-time constant; not operator-tunable at v1.
Refill MUST fire AFTER all terminal-event processing for the tick (merge, reviewer-launch, `CloseBead`, group-advance evaluation per §4.3 EM-015f) completes for the current run. **Finishing in-flight work takes priority over pulling new work.**
Tags: mechanism · Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent.

#### EM-063 — Pre-screen and provenance guard
`pre_screen` MUST apply a two-phase filter:
**Phase 1 — already-in-queue (queue.json authority, fastest).** For each candidate `bead_id`, inspect the active `queue.json` envelope in-memory. If present with `status ∈ {pending, dispatched, completed, failed}` → SKIP.
**Phase 2 — already-landed (git authority).** For candidates not eliminated by Phase 1, execute `git log --all --grep "Refs: <bead_id>" --oneline` against **`origin/main` (NOT local `main`)**. If ≥1 commit found → SKIP and log a `stale_open_bead_detected` informative event.
> INFORMATIVE: `origin/main` not local `main` avoids false positives during the two-phase terminal sequence (EM-052/EM-053): a bead whose push succeeded but whose local `main` has not yet been fast-forwarded would appear "not landed" on a local-only check, triggering a spurious re-dispatch.
**Provenance guard.** Refill MUST NOT dispatch a bead created by the daemon or orchestrator in the same workloop tick. Newly-created beads land `open` (not yet `ready`); `kerf next` returns only `ready` beads. So the readiness gate of `kerf next` is the normative enforcement; spec does not require a per-tick creation log.
**Result.** Ordered list of survivors in `kerf next` priority order. Ordering preserved into `queue_append`.
Tags: mechanism · Axes: same.
```

---

### Change 3: New §4.14 — Check-Observed-Before-Submit Guard

Insert as new §4.14 after §4.13, before §5.

```markdown
## 4.14 Check-observed-before-submit guard

The orchestrator's idempotency obligation BEFORE `harmonik run --beads X` (i.e. before submitting a queue per [process-lifecycle.md §4.4 PL-003a]). Orchestrator-facing contract; the pre-screen of §4.13.EM-063 is the daemon-side refill contract. Both enforce the same invariant (never dispatch an already-claimed or already-landed bead) at their respective entry points.

#### EM-064 — Read-order and authority chain
Before submitting or appending any bead X to the execution queue, the orchestrator MUST walk this chain in order; skip X if any tier signals "already observed":

| Tier | Source | Check | Skip if |
|---|---|---|---|
| 1 | `queue.json` in-memory (or `queue-status`) | X present, `status ∈ {pending, dispatched, completed, failed}` | yes |
| 2 | `origin/main` git log | `git log origin/main --grep "Refs: X" --oneline` ≥1 commit | yes; optionally `br close X` if still open |
| 3 | Beads ledger | `br show X` `status ∈ {in_progress, closed}` | yes; daemon atomic claim enforces final barrier |
| 4 | `events.jsonl` | `run_started` for X with no subsequent terminal event | yes (in-flight) |

Tier 1 is fastest (in-memory queue scan); MUST be first. Tier 2 uses `origin/main` NOT local `main` (rationale per EM-063). Tiers 3+4 are supplementary; a conforming impl covering 1+2 satisfies the guard at v1 correctness. Tiers 3+4 SHOULD be checked in long-running orchestrator sessions to catch cross-session drift. The read order is normative; MUST NOT reverse (queue.json is the fastest and most current in-memory mirror).
Tags: mechanism · Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent.

#### EM-065 — Submit/append targets the active stream group; no double-queue
Orchestrator MUST submit beads to the single active queue per [queue-model.md §3 QM-002]. If a queue is already active, the orchestrator MUST use `queue-append` against the active stream group (per [queue-model.md §7 QM-040]) rather than `queue-submit` (which would replace). A new submit while a queue is active is a QM-003 violation; the daemon will reject. EM-064 tier 1 catches the common case where the orchestrator's stale view would re-submit an in-flight bead.
> INFORMATIVE: The daemon's Beads atomic claim (`ClaimBead` per [beads-integration.md §4.3 BI-009]) is the final barrier at the execution layer. EM-064 is an orchestrator-layer pre-flight, complementary not replacement.
Tags: mechanism.
```

---

### Supporting additions

**§2.2 Out of scope** — append to the queue-model ownership bullet:
> The eager-refill obligation (§4.13) and the check-observed-before-submit guard (§4.14) are owned by this spec; they describe when and how the execution layer appends to the queue, not the queue's own data model or lifecycle.

**§6.5 Co-owned events** — append to the queue-lifecycle bullet:
> Refill diagnostics (only when eager refill fires per §4.13.EM-062) — `stale_open_bead_detected` (informative; emitted by §4.13.EM-063 Phase 2 when a `kerf next` candidate is found to have already landed on `origin/main`). Event name and payload declared in [event-model.md §8.10]; this spec declares the emission obligation.

**§10.1 Conformance profiles** — extend Core MVH:
> **and EM-062 through EM-065 (eager refill §4.13; check-observed-before-submit guard §4.14).** The §4.13 obligation applies only when an active stream group is present; a conforming impl with no stream group in flight satisfies vacuously. §4.14 is orchestrator-facing; a daemon-only impl satisfies it at the tier-3 Beads-claim barrier (tiers 1+2 are orchestrator-layer pre-flights).

**§12 revision history entry:**
```
| 2026-05-30 | 0.8.0 | agent (flywheel spec-draft) | Flywheel additions: EM-NOTE-WAKE (hk-24xn1 closed; sub-poll wake via SetQueue→wakeC); EM-NOTE-STREAM-CONCURRENCY (V2 correction: streamEligible skips dispatched items; --wave is append-closed not concurrency-required); §4.13 eager-refill EM-062/EM-063 (on run_terminal or poll, available=max_concurrent−in_flight_count, deficit-based pull from kerf next ×2 OVERFETCH_FACTOR, two-phase pre-screen: queue.json then git origin/main, append survivors; readiness gate of kerf next enforces no-self-dispatch); §4.14 check-observed-before-submit EM-064/EM-065 (4-tier authority queue.json → git origin/main → Beads → events.jsonl, tiers 1+2 mandatory for v1; submit/append targets active stream group, daemon atomic claim final barrier). New IDs EM-062, EM-063, EM-064, EM-065. No prior IDs renumbered/retired. |
```

---

### How these compose with existing requirements
- EM-062/063 sit inside the existing §7.4 `orchestrator_main_loop` dispatch loop. Call site = `finalize_run` return path — after `emit_event(run_completed/run_failed)` and after §4.3 EM-015f group-advance, before the outer loop's next iteration. No new state machine; `queue_append` already normative under [queue-model.md §7 QM-040]; capacity gate EM-049 still guards.
- EM-064/065 are orchestrator-layer pre-flight; don't alter daemon dispatch path; prevent redundant round-trips and protect idempotency at the session boundary.
- `origin/main` authority in EM-063+EM-064 is consistent with EM-052/EM-053 merge-to-main contract: daemon pushes to `origin/main` as completion authority, so reading `origin/main` is the correct post-push view.
- The stream-concurrency correction matches `internal/queue/state.go:283` and QM-035 ("after all earlier items have at least entered dispatched" the tail is eligible).
