> Archived from .kerf/extqueue/05-changelog.md on 2026-05-15

# extqueue v0.1 — Spec-draft Changelog

This is the package-level changelog for the `extqueue` kerf work, summarizing every spec change applied across the 6 drafted spec files. Per-spec changelog entries are recorded in each spec's own §A.4 / §12 revision-history block.

## Package summary

extqueue v0.1 separates execution from project management in the harmonik daemon. The daemon no longer polls `br ready` to select work; an external orchestrator agent submits an ordered queue of waves and streams via the CLI, and the daemon executes that queue exactly as ordered. Scope is the smallest cohesive system that supports submit + append + dry-run + status. Remove, pause, resume, and clear are deferred to v0.2.

## Files in the package

| File | Status | Lines (before → after) | Version bump |
|---|---|---|---|
| `specs/queue-model.md` | **new** | 0 → 604 | v0.1.0 (new) |
| `specs/execution-model.md` | edit | 1276 → 1347 | v0.4.x → v0.5.0 |
| `specs/beads-integration.md` | edit | ~700 → 922 | v0.4.1 → v0.5.0 |
| `specs/process-lifecycle.md` | edit | 1043 → 1072 | v0.4.5 → v0.4.6 |
| `specs/event-model.md` | edit | 1248 → 1356 | v0.4.x → v0.5.0 |
| `specs/operator-nfr.md` | edit | 1038 → 1041 | v0.4.1 → v0.4.2 |

## Cross-spec invariants (locked in this package)

- **Method-naming convention**: bare-kebab verbs on the JSON-RPC wire (`queue-submit`, `queue-append`, `queue-status`, `queue-dry-run`). No dotted form. Consistent with PL-003a's existing surface (`claim-next`, `emit-outcome`, etc.).
- **`enqueue` retired** with no alias. Touchpoints amended: PL-003a, PL-028, ON-013a, ON-050. No new behavior depends on the name; no spec text constrains retire vs alias; retire chosen for naming consistency.
- **6-event cohort** in event-model.md §8.10. Dropped events (`queue_item_dispatched`, `queue_item_completed`, `queue_item_failed`) are not registered — reconstructible from `run_*` with optional `queue_id` / `queue_group_index` per §6.4 row 1 (additive non-breaking change).
- **`.harmonik/queue.json`** is the persisted execution plan. WM-026 atomic-write discipline (temp + fsync + rename + fsync(parent_dir)) per QM-001. Loaded at PL-005 step 8a alongside `daemon.state` and `daemon.upgrading`. Unlinked on queue completion per QM-003. Added to ON-018 N-1 enumeration and ON-INV-001 sensor enumeration.
- **`queue_id`** is daemon-minted UUIDv7. Never client-supplied. Returned in the `queue-submit` JSON-RPC response. Carried on every §8.10 event payload and as an optional field on `run_*` events when the run originated from a queued submission.
- **Concurrency primitives spec'd**: EM-049 (in-flight-run capacity gate), EM-050 (claim-write semaphore), EM-051 (`--max-concurrent` configuration). Previously code-only (hk-e61c3.*); now anchored in execution-model.md §4.11.
- **Ledger remains authoritative** for bead identity, status, and `blocks` edges. Queue narrows parallelism but never overrides ledger constraints. Within-group ledger-blocks cause `queue_item_deferred_for_ledger_dep` emission at submit time (informational, not rejecting per QM-025).

## Per-spec change inventory

### `specs/queue-model.md` (NEW, v0.1.0)

Introduces the foundational data model, persistence discipline, identity, group state machine, validation contract, append semantics, queue lifecycle, and concurrency composition. 26 QM-NNN requirement IDs across 9 sections plus 4 appendices.

### `specs/execution-model.md` (v0.4.x → v0.5.0)

- TS-1: §7.4 dispatch loop pseudocode replaces `br ready` poll + `pick_one` with `active_queue()` head-pull, ledger-block deferral, claim-token acquire, `spawn_async … THEN …` fan-out.
- TS-2: new EM-015f gates queue-group advance on all-terminal with v0.1-no-resume note.
- TS-3: EM-015a / EM-015b extended with optional `queue_id` + `queue_group_index` on `run_*` payloads.
- TS-4: new §4.11 Concurrency with EM-049 / EM-050 / EM-051 (formerly code-only hk-e61c3.*).
- TS-5: §7.1 INFORMATIVE block extended to point at the per-group state machine layered above the per-run state machine.
- TS-6: MVH-era `pick_one` "oldest-first tiebreak" comment dropped (covered by TS-1).

No requirement IDs renumbered or retired.

### `specs/beads-integration.md` (v0.4.1 → v0.5.0)

- BI-013 demoted: `br ready` is an orchestrator-facing tool, not a daemon input.
- BI-013a relocated: `needs-attention` exclusion moved from adapter read-time filter to submit-time validation rejection (BI-013b).
- BI-024a / BI-025c re-anchored: handshake / 5 s read-timeout now key off first `queue-submit` instead of first `br ready`.
- BI-007 prose cleanup at line 122 ("dispatch loop (BI-013 ready-work)" parenthetical removed).
- New §4.5a "Submit-time validation read surface" with BI-013b (`br show` submit-time read) and BI-013c (pre-claim status re-read, formerly code-only hk-p4xbw guard).
- §4.10 intent-log scope note: queue mutations are NOT Beads writes; BI-029/BI-030 do not cover `.harmonik/queue.json`.

No requirement IDs renumbered or retired (BI-013b / BI-013c are letter-suffix additions).

### `specs/process-lifecycle.md` (v0.4.5 → v0.4.6)

- PL-003a method-set: added `queue-submit`, `queue-append`, `queue-status`, `queue-dry-run`; removed `enqueue`. JSON-RPC error-code block `-32010..-32019` reserved for queue errors.
- PL-028: removed `harmonik enqueue` bullet; added four `hk queue <verb>` bullets in PL-028 one-line style.
- New PL-028c: pre-`flag.Parse` CLI dispatch pattern for `hk queue` (mirror of `tmux-start`/`hook-relay`).
- PL-005 step 8a: `.harmonik/queue.json` read alongside `daemon.state`/`daemon.upgrading`. Forward-incompatible schema → exit 14; corrupt → warn + proceed.
- PL-013 retired entirely (Beads-poll wakeup mechanism is gone post-extqueue); idle-wait obligation preserved as a one-line stub.
- PL-004 file-surface inventory: `.harmonik/queue.json` added.
- PL-027(iii): non-regression note — queue methods inherit listener-fd-across-execve unchanged; queue state reconstructs via step-8a read.
- PL-008a: code 17 (multi-daemon-target-missing) referenced for daemon-down CLI behavior on queue methods.

No requirement IDs renumbered or retired (PL-013 is retired-with-stub, ID preserved).

### `specs/event-model.md` (v0.4.x → v0.5.0)

- New §8.10 cohort with 6 event types: queue_submitted (F), queue_group_started (O), queue_group_completed (F), queue_paused (F), queue_appended (O), queue_item_deferred_for_ledger_dep (O).
- §8.10 Section Axes paragraph.
- §8.10 emission-ordering paragraph: queue_submitted → queue_group_started → (run_* chain per item) → queue_group_completed → optional queue_paused on failure or drain.
- §6.3 YAML schemas for all 6 new types. `queue_paused.reason ∈ {group_failure, operator_drain}`.
- §6.3 `run_started` / `run_completed` / `run_failed` gained optional `queue_id` and `queue_group_index` (non-breaking per §6.4 row 1).
- §6.5 co-ownership bullet for §8.10 emissions, pointing at queue-model.md §4/§6/§7/§8.
- source_subsystem `github.com/harmonik/internal/queue` registered.

No existing event types modified beyond optional-field additions; no schema_version bumps required.

### `specs/operator-nfr.md` (v0.4.1 → v0.4.2)

- ON-004: quiet deletion of `queue-empty re-query cadence` from the config inventory (knob obsolete; daemon no longer polls).
- ON-009a: appended terminology note disambiguating Beads needs-attention queue from execution queue.
- ON-013a: replaced `enqueue` in the supervised-command list with itemized `queue-submit`, `queue-append`, `queue-status`, `queue-dry-run`.
- ON-015 sentence-1 rewrite: "Beads is the catalog of work" (not the queue); sentence 2 unchanged.
- ON-018: inserted `queue execution plan ([queue-model.md §3 QM-001..003], persisted as .harmonik/queue.json with schema_version field)` between `queue overlay` and `policy schema`.
- ON-027 step (1): "daemon stops advancing the queue" (replaces "pulling new tasks from the queue").
- ON-041 (b): added `queue (subcommands: submit, status, append, dry-run)`.
- ON-050 (d): removed `enqueue` from the `harmonik attach` inline-command subset.
- ON-INV-001 Sensor: parallel enumeration extension matching ON-018.
- §7.2 drain pseudocode line ~733: parallel `stop_queue_advancement()` call.

ID-FREEZE preserved: no ON-NNN added or retired.

## Spec-draft reviewer round 1 — corrections applied

The first spec-draft reviewer pass (`spec-draft-review.md`) raised 2 critical issues + 5 cleanups. All resolved in-package, no second reviewer round needed:

- **C1 resolved (queue-model.md).** `queue_validation_failed` demoted from "event-bus event" to "JSON-RPC error" exclusively. Section 6 preamble rewritten; QM-028 retitled to "Validation failures are not events" with normative MUST-NOT-emit-events on validation failure; QM-029 reframed as JSON-RPC error reason enum (no event-payload schema bump path); QM-064 reference updated. The error-code block `-32010..-32019` is the wire-level surface, owned in PL-003a.
- **C2 resolved (event-model.md §8.7.16).** `operator_command_failed.command` enum updated: removed `enqueue`; added `queue-submit`, `queue-append`, `queue-status`, `queue-dry-run`.
- **R1 resolved (operator-nfr.md ON-001).** `enqueue` replaced with `queue (with subcommands per [process-lifecycle.md §4.4 PL-003a])` in the structured-exit-code command enumeration.
- **R2 resolved (4 dotted-form leaks).** All `queue.resume / queue.remove / queue.clear` references switched to bare-kebab `queue-resume / queue-remove / queue-clear`. queue-model.md:52, event-model.md:1075 + 1327.
- **R3 resolved (queue-model.md §5.1 transition table + execution-model.md:1086).** `queue.status == active` guard clause changed to `Queue.status == active` (RECORD field access) for clarity; same change applied in execution-model.md §7.4 pseudocode (`Queue.status IN {...}` with inline `RECORD field access` clarifier).
- **R5 resolved (failure_mode enum gap).** Path (b) chosen: `queue-schema-incompatible` retired; both `.harmonik/queue.json` schema mismatch and Beads overlay schema mismatch now use the existing `queue-format-unsupported` failure_mode (exit code 2). Process-lifecycle.md §4.2 PL-005 step 8a + §10.2 test obligations + §12 changelog updated. Operator-nfr.md §8 row 2 description extended to cover both schema sources.

R4 (this section in the package changelog updated to record resolutions) — done by writing this entry.

Optional polish items O1-O5 from the review deferred (minor section-ref drift, numbering anomaly, ON-009a phrasing — none load-bearing for integration).

## Deferred to v0.2 (out of scope for v0.1)

- `queue-remove` (remove a queued item before dispatch).
- `queue-pause` / `queue-resume` (live operator control over queue advance).
- `queue-clear` (wholesale queue replacement).
- Multi-orchestrator submission semantics.
- Stream priorities, weighted scheduling, conditional ordering.
- Queue replay / time-travel.
- Pause / stop / kill of *running* beads (separate work, separate spec pass).
