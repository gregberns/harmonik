# Session Notes

## Session 1 (2026-05-14)

Completed all 8 kerf passes for the `extqueue` v0.1 spec work in a single agent-orchestrated session.

1. **Problem Space** — Captured D1–D6: external orchestrator owns scheduling, harmonik daemon executes only. Queue is an ordered sequence of group primitives — `wave` (closed parallel set) and `stream` (open-ended ordered list with append-in-flight). All-terminal group advance; failure pauses-by-failure (no auto-retry, no auto-skip); ledger `blocks` edges remain authoritative; Unix-socket CLI transport; in-memory primary + `.harmonik/queue.json` crash sync; v0.1 surface = submit + append + dry-run + status only. v0.2 deferrals enumerated (remove, pause, resume, clear, multi-orchestrator).

2. **Decompose** — 8 spec files mapped (1 new + 5 edited + 1 small + 1 review-only): `queue-model.md` (new), `execution-model.md`, `beads-integration.md`, `process-lifecycle.md`, `event-model.md`, `operator-nfr.md`, plus `scenario-harness.md` (small) and `workspace-model.md` (review-only). Reviewer caught two real issues (missed `operator-nfr.md`; redundant `control.sock` proposal — should reuse existing `daemon.sock`); both fixed.

3. **Research** — 6 parallel research agents (one per Tier-A/B spec) produced findings docs. Key cross-cutting findings: bare-kebab method names match house style (not dotted); 9 candidate events trim to 6 per EV-016a tandem-emission rule; `enqueue` retire (not alias) — no spec text forces alias; ON-026 was a typo, actual config-inventory ID is ON-004.

4. **Change Design** — 6 design docs (one per Tier-A/B spec) produced in parallel by independent agents grounded in the queue-model design + research findings + cross-cutting decisions. Reviewer found 5 cross-doc inconsistencies (1 missing `queue_completed` event reconciliation, 1 missing paused-by-drain transition, 3 typo/anchor issues); all fixed.

5. **Spec Draft** — 6 spec-draft agents produced full updated spec files in parallel. Total 6342 lines across the 6 drafts. Reviewer found 2 critical issues + 5 cleanups: `queue_validation_failed` phantom-event resolved by demoting to JSON-RPC-error-only; `operator_command_failed` enum updated to drop `enqueue` and add 4 `queue-*` methods; 4 dotted-form leaks fixed; ON-001 residual `enqueue` retired (changelog-flagged); `queue-schema-incompatible` failure_mode unified with `queue-format-unsupported` (path b chosen).

6. **Integration** — Cross-corpus audit confirmed zero residue from retirements (`enqueue`, `br ready` as daemon input, dropped event names) in non-amended specs. One mechanical fix: `WM-026` cited at `§4.6` should be `§4.7` in queue-model.md (two occurrences). One pre-existing miscitation noted (beads-integration:205 cites WM-007 in §4.5 but it's in §4.2) — flagged but out-of-scope for this work.

7. **Tasks** — 22 tasks across 8 tiers (T01–T84). Critical path ~7 implementer cycles; Wave 3-4 supports 4-5 parallel implementers. Spec-traceability table covers every changelog entry; every task traces to a specific spec section. Reviewer found 4 single-line issues (missing `queue_id` propagation task scope, two dep inconsistencies, line-range correction T50 `391-421` → `391-495`); all fixed.

8. **Ready** — All artifacts present; advancing to `kerf square` then `kerf finalize`.

### Key decisions locked (D1–D6 + cross-cutting)

- D1: Wave (`{}`) + stream (`[]`) primitives, both in one queue.
- D2: Group advances only on all-terminal-success.
- D3: Failure pauses queue at group boundary; no auto-retry/skip; v0.1 recovery = restart + re-submit.
- D4: Ledger `blocks` edges narrow parallelism in-place (informational event, not rejection).
- D5: Unix domain socket transport on existing `.harmonik/daemon.sock` (no second socket).
- D6: v0.1 = submit + append + dry-run + status only. Remove/pause/resume/clear defer to v0.2.
- Bare-kebab method names (`queue-submit`, `queue-append`, `queue-status`, `queue-dry-run`).
- 6-event cohort in event-model §8.10 (dropped: queue_item_dispatched/completed/failed; reserved for v0.2: queue_resumed).
- `enqueue` retired without alias.
- `queue_id` daemon-minted UUIDv7, optional on `run_*` events.
- `.harmonik/queue.json` WM-026 atomic-write; loaded at PL-005 step 8a.

### Agents dispatched this session

Doc-staleness audit (1), research (6), change-design (6), change-design review (1), spec-draft (6), spec-draft review (1), integration audit (1), tasks review (1). Plus a doc-fix agent for the MVH/parallelism prose update. ~22 sub-agent invocations.

### Open follow-ups (not in this work)

- Pre-existing miscitation `beads-integration.md:205` (WM-007 location) — flagged for housekeeping pass.
- `reconciliation/spec.md:57` has slightly stale BI-013a "dispatch-filter signal" prose; ID still resolves but framing predates BI-013a relocation. Cosmetic.
