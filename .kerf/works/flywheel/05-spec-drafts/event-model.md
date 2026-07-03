# Event-model.md — flywheel change-set (draft)

> Spec-draft for `specs/event-model.md` additions. Source: spec-draft sub-agent (sonnet), 2026-05-30. Three changes: (1) §4.11 + EV-037–EV-041 — `harmonik subscribe` consumer contract; (2) §8.12 + §6.3 + §4.12 + EV-042–EV-044 — new `decision_required`/`decision_acknowledged` event types; (3) §10.3 note — CLI help text divergence fix.

## Change-set summary
Change 1 (§4.11, EV-037–EV-041) formalizes the CONSUMER contract for `harmonik subscribe` — obligations around watermark persistence, `since_event_id` replay-and-dedup, `subscription_gap` forced-resync, heartbeat-driven watermark advancement, and the daemon-crashed-mid-terminal wake heuristic. Change 2 (§8.12, §6.3, §4.12, EV-042–EV-044) introduces `decision_required`/`decision_acknowledged` cross-bus event types: schema, emission idempotency, acknowledgement surface, unacknowledged-as-dispatch-blocker rule, §8.9 compliance. Change 3 flags the stale CLI help for `--since-event-id` and declares CLI-spec canonicity.

---

### Change 1: §4.11 — `harmonik subscribe` consumer contract (EV-037–EV-041)

Insert as new §4.11 after the existing §4.10 Redaction (after EV-036), before §5 Invariants.

```markdown
### 4.11 `harmonik subscribe` consumer contract

`harmonik subscribe` is the primary push transport for external consumers (e.g. the flywheel cognition loop). These obligations are ADDITIONAL to the in-process consumer rules (§4.3 EV-009–EV-014d); they apply specifically to the out-of-process, NDJSON-over-stdout subscribe interface.

#### EV-037 — External consumers MUST persist a watermark of `last_processed_event_id`
The consumer MUST persist the UUIDv7 of the last fully-processed event to a stable location (e.g. `.harmonik/cognition/watermark.json`). On reconnect or cold-start it MUST supply this as `--since-event-id` to `harmonik subscribe`, triggering server-side replay before live-stream resumes. The consumer MUST NOT advance its watermark BEFORE recording any reaction to the processed event; required ordering: effect → ledger-entry → watermark-advance. Crash between effect and ledger-entry is recovered via effect idempotency (keyed on `event_id`); crash between ledger-entry and watermark-advance causes a re-read that finds the ledger entry and skips. Watermark keys MUST be UUIDv7 `event_id`, NEVER a byte offset (byte offsets are rotation-unsafe and undefined after log compaction).
Tags: mechanism · Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### EV-037a — Watermark MUST NOT regress
Safe advance rule: `watermark = max(persisted_watermark, incoming_event_id)`. On heartbeat whose `last_event_id > current watermark`, advance even when no actionable event was processed — this is how quiet periods advance the watermark without LLM work. No-regression invariant prevents a context-reset crash from re-processing already-reacted events.
Tags: mechanism · Axes: same.

#### EV-038 — Consumers MUST treat `subscription_gap` as a forced re-sync trigger
On `subscription_gap{dropped:N}` (drop-oldest overflow per `internal/daemon/subscribe.go`), the consumer MUST NOT continue as if no events were lost. It MUST: (a) `ScanAfter(watermark)` on `events.jsonl` to replay the gap; (b) re-sense `queue.json` and the git completion log (for any run_id whose terminal event may have been dropped). Only after re-sync may live-stream processing resume. Required because a dropped event might be a Tier-2 judgment event (e.g. `run_failed`, `merge_conflict_escalation`, `iteration_cap_hit`) whose loss would silently leave the consumer in an incorrect state.
Tags: mechanism · Axes: same.

#### EV-039 — Heartbeat carries `last_event_id` + `active_runs`; consumers MUST use both
Heartbeat (default 60s) payload: `last_event_id` (UUIDv7 string), `active_runs[]` array of `{bead_id: String, age_seconds: Integer}`. Consumers MUST advance their watermark to `last_event_id` on every heartbeat (per EV-037a). Consumers SHOULD inspect `active_runs[].age_seconds`; if any run exceeds a configured stall threshold, treat as a synthetic Tier-2 wake to investigate. **The `active_runs` array carries `bead_id` + `age_seconds` ONLY; it does NOT carry `run_id`.** Consumers MUST NOT assume `run_id` is present; run-level correlation requires reading `queue.json`.
Tags: mechanism · Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent.

#### EV-040 — Missing heartbeats = daemon liveness failure; reconnect with backoff
No heartbeat for `K × heartbeat_interval` (recommended K=2 → 120s at 60s) → treat as daemon liveness failure. Reconnect with exponential backoff (suggested 5s/10s/30s). If `harmonik subscribe` exits 17 (daemon-not-running sentinel), emit a synthetic `daemon_down` signal to the consumer's reaction layer. Reconnection MUST supply `--since-event-id=<watermark>` (per EV-037); MUST NOT start from live-stream head on reconnect — terminal events emitted during the outage would be missed.
Tags: mechanism · Axes: same as EV-039.

#### EV-041 — Git-done-but-no-terminal-event after K heartbeats SHOULD trigger a wake
If after K consecutive heartbeats (suggested K=2) a `bead_id` has disappeared from `active_runs` yet no terminal event has been processed for that run since the watermark, the consumer SHOULD git-check the missing `run_id` (`git log --all --grep "Harmonik-Run-ID: <run_id>"`). A merged commit without a terminal event = daemon crashed mid-terminal-emission; treat the git completion as authoritative (per EV-INV-001) and synthesize a Tier-1 reaction (advance kerf baseline, close stale bead) without waiting for an event that will never arrive. This SHOULD does not override EV-022 (state reconstruction MUST walk git+Beads); it's an observational heuristic.
Tags: mechanism · Axes: same.
```

---

### Change 2: §8.12 + §6.3 + §4.12 — `decision_required` event (EV-042–EV-044)

**2a. New §8.12 taxonomy block (after §8.11 Handler-pause lifecycle):**

```markdown
### 8.12 Decision-required lifecycle

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.12.1 | `decision_required` | F | daemon-core | operator-observability, cognition-loop, audit | `subject`{`kind`,`id`}, `reason`, `suggested_action`, `ack_required`, `ack_token`, `triggering_event_id` |

> **Emission conditions (§8.12.1).** Daemon MUST emit `decision_required` on:
> (a) bead fails twice in a daemon session without an intervening success (`run_failed`×2 on same `bead_id`);
> (b) `iteration_cap_hit` fires with `final_verdict ∈ {REQUEST_CHANGES, BLOCK}`;
> (c) `merge_conflict_escalation` is emitted (§8.5.6);
> (d) `queue_paused{reason: group_failure}` is emitted (§8.10.4).
>
> At-most-one per triggering event (idempotency-keyed on `triggering_event_id`). Re-processing the same trigger after restart MUST NOT re-emit for an already-emitted pending `ack_token`. Enforced via `.harmonik/decision_acks/<ack_token>` file presence + `status=pending` check.
>
> **Acknowledgement surface.** Either (a) `harmonik decision ack <ack_token>` CLI (operator), OR (b) `note(kind=warning|defer, refs=[subject.id], ...)` from the cognition loop (Tier-2 judgment implicitly ACKs). Daemon updates the `ack_token` record → `{status:acknowledged, acked_at, ack_method:operator|note}` and emits `decision_acknowledged` (§8.12.2).
>
> **TTL.** `ack_token` valid 24h (configurable, default 86400s). After TTL without ACK, daemon MUST re-emit `decision_required` with a fresh `ack_token` (same subject/reason; new token) AND `daemon_degraded{reason: decision_ack_timeout}` (requires EV-027 amendment for new variant).
>
> **Dispatch-blocking rule (EV-043).** While any `decision_required` for `subject kind=bead,id=X` is unacknowledged, daemon MUST NOT dispatch a new run for bead X. While any `decision_required` for `subject kind=queue,id=Q` is unacknowledged, daemon MUST NOT advance queue Q past the paused group. Applies across daemon restarts: at startup, daemon scans `.harmonik/decision_acks/` for pending tokens and restores dispatch-blocking BEFORE the workloop resumes.

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.12.2 | `decision_acknowledged` | F | daemon-core | operator-observability, cognition-loop, audit | `ack_token`, `subject`{`kind`,`id`}, `ack_method`{`operator`|`note`}, `acked_at` |

> §8.12 is NOT a paired-phase per §8.9(h) — payloads differ in shape; `decision_required` carries the problem; `decision_acknowledged` carries the resolution reference. §8.9(h) status-merge does not apply.
> §8.9 compliance evidence: (a) cross-subsystem consumers: operator-observability, cognition-loop, audit; (b) lifecycle-boundary signal: dispatch-eligibility transition; (c) single summary event per condition (no per-heartbeat re-emit); (d) schema in §6.3; (e) class F (loss silently unblocks dispatch); (f) idempotent on `ack_token`; (g) cited by cognition-loop spec as Tier-2 wake.
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent.
```

**2b. §6.3 payload schemas (after `queue_item_reconciled`):**
```yaml
# decision_required
subject:
  kind: <enum: bead | queue>
  id: <String>
reason: <enum: bead_double_failure | iteration_cap_hit | merge_conflict_escalation | queue_group_failure>
suggested_action: <String>     # free text; SHOULD be ≤ 256 bytes
ack_required: <Boolean>        # always true at v1; reserved for future advisory-only signals
ack_token: <String>            # opaque UUIDv4; unique per emission; key for .harmonik/decision_acks/
triggering_event_id: <UUID>    # event_id of the condition event that caused this emission (dedup key)

# decision_acknowledged
ack_token: <String>            # MUST match the ack_token of the matching decision_required
subject:
  kind: <enum: bead | queue>
  id: <String>
ack_method: <enum: operator | note>
acked_at: <Timestamp>
```
`reason` is exhaustive at v1; new variants require an EV-027 amendment. `triggering_event_id` MUST reference a specific event in `events.jsonl`.

**2c. §4.12 normative requirements:**

```markdown
### 4.12 `decision_required` dispatch-blocking rule

#### EV-042 — `decision_required` MUST be emitted on the four canonical conditions
Daemon MUST emit on each condition enumerated in §8.12.1. Emission MUST be fsync-backed (class F) and MUST precede any state mutation the condition would otherwise trigger (e.g. workloop MUST NOT attempt a third dispatch for a double-failed bead before `decision_required` is durable). Emission idempotency-keyed on `triggering_event_id`; re-processing after restart MUST NOT produce a second event for an already-pending `ack_token`.
Tags: mechanism · Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent.

#### EV-043 — Unacknowledged `decision_required` blocks dispatch for its subject
While a `decision_required` for a given `subject` is unacknowledged (no matching `decision_acknowledged` in `events.jsonl` AND `.harmonik/decision_acks/<ack_token>` absent or `status=pending`), daemon MUST NOT dispatch a new run for that subject. Blocking check MUST run at daemon startup (EV-043a) AND at every workloop dispatch attempt for the subject. ACK via `harmonik decision ack <token>` or cognition-loop `note()` unblocks atomically; `decision_acknowledged` MUST be emitted+fsynced BEFORE the workloop is permitted to dispatch.
Tags: mechanism · Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent.

#### EV-043a — Startup MUST restore `decision_required` blocking state
On startup, daemon MUST scan `.harmonik/decision_acks/` for records `status=pending` and restore the corresponding dispatch-blocking state BEFORE the workloop begins dispatching. Loss of a `decision_required` event from JSONL (ordinary tail-truncation) is survived via `.harmonik/decision_acks/`, which is fsynced independently as the authoritative ack-state store. The ack-state file is the durability anchor; the JSONL event is the observational record.
Tags: mechanism · Axes: same.

#### EV-044 — Unacknowledged `decision_required` is a digest exception
Any cognition-loop or external monitoring consumer producing a periodic digest MUST surface every unacknowledged `decision_required` in that digest, regardless of whether a Tier-2 action has been taken. MUST NOT silently suppress on the grounds that the consumer has already "seen" the event; suppression is only valid after `decision_acknowledged` for the matching `ack_token` is observed.
Tags: mechanism · Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent.
```

---

### Change 3: §10.3 — Stale CLI help text fix

Append to §10.3 Excluded conformance claims:

```markdown
#### CLI help text is canonical alongside this spec; stale text MUST be corrected on spec adoption
`harmonik subscribe --since-event-id` is IMPLEMENTED as of 2026-05-27 (hk-a5sil, sha 994c6d2); the CLI help at `cmd/harmonik/subscribe.go:200` currently reads "NOT YET IMPLEMENTED — daemon rejects; hk-a5sil", which is STALE. This spec is normative for the behavior contract; the CLI help is the operator-facing surface for the same contract. The two MUST agree. As part of adopting this revision, `cmd/harmonik/subscribe.go:14` and `:200` MUST be updated to remove the "NOT YET IMPLEMENTED" annotation and describe the replay semantics (e.g. "Resume cursor: replay events strictly after this event_id before delivering live stream"). Divergence is a §8.9(g)-class orphan; this note closes the obligation.
```

---

### Insertion map + new IDs
- §4.11 block (EV-037–EV-041): after `§4.10 Redaction` (after EV-036), before `§5 Invariants`.
- §8.12 block: at the end of §8 taxonomy, after §8.11.
- §4.12 block (EV-042–EV-044): after §4.11.
- §6.3 payload schema additions: after `queue_item_reconciled` YAML block.
- §10.3 CLI note: append.
- §12 revision history: new row `| 2026-05-30 | 0.6.0 | flywheel/spec-draft | ...`.

**New IDs:** EV-037, EV-037a, EV-038, EV-039, EV-040, EV-041 (consumer contract); EV-042, EV-043, EV-043a, EV-044 (decision_required). **New event types:** `decision_required` (8.12.1, class F), `decision_acknowledged` (8.12.2, class F). **New `daemon_degraded` variant** `decision_ack_timeout` flagged as a separate EV-027 amendment. No existing IDs retired/renumbered.
