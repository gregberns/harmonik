# Spec-Drift Audit — 2026-06-02

**Auditor:** hk-12ke1 (spec-drift audit bead)
**Method:** For each of the 10 closed spec-parent epics, diffed the spec from its pinned (epic-beaded) version to HEAD. Identified new §4 requirements; confirmed bead coverage via `git log --grep` and `br list`. Flagged orphans with no git hit and no covering bead.
**Scope:** Read-only. No spec or code modifications. Follow-up beads filed per GAP.

---

## Per-Spec Results

| # | Epic | Spec | Pinned | HEAD | New Reqs | Orphans | Verdict |
|---|------|------|--------|------|----------|---------|---------|
| 1 | hk-872 | beads-integration.md | v0.4.1 | v0.6.2 | 8 | 1 | **GAP** |
| 2 | hk-b3f | execution-model.md | v0.3.3 | v0.8.2 | 30 | 2 | **GAP** |
| 3 | hk-hqwn | event-model.md | v0.3.3 | v0.5.5 | 10 | 7 | **GAP** |
| 4 | hk-8i31 | handler-contract.md | v0.3.3 | v0.5.4 | 25 | 4 | **GAP** |
| 5 | hk-a8bg | control-points.md | v0.3.2 | v0.4.3 | 8 | 0 | **PASS** |
| 6 | hk-8mwo | workspace-model.md | v0.4.2 | v0.4.5 | 7 | 0 | **PASS** |
| 7 | hk-8mup | process-lifecycle.md | v0.4.1 | v0.4.8 | 13 | 1 | **GAP** |
| 8 | hk-sx9r | operator-nfr.md | v0.4.1 | v0.5.3 | 24 | 2 | **GAP** |
| 9 | hk-63oh | reconciliation/spec.md | v0.4.0 | v0.4.5 | 0 | 0 | **PASS** |
| 10 | hk-i0tw | scenario-harness.md | v0.2.0 | v0.2.2 | 2 | 0 | **PASS** |

**Totals:** 127 new requirements across all 10 specs. 17 orphans across 6 specs. 4 specs PASS cleanly.

---

## Orphan Requirements Detail

### beads-integration.md (epic hk-872)

| Req | Description | Orphan Reason |
|-----|-------------|---------------|
| **BI-013c** | Pre-claim status re-read between dispatcher selection and `claim` write | 0 git hits, no open bead |

*Context:* Part of the §4.13 claim-atomicity group (BI-013a/b are covered, BI-013d is covered). BI-013c's pre-read obligation was added in v0.5.x alongside the enum splits but not beaded.

---

### execution-model.md (epic hk-b3f)

| Req | Description | Orphan Reason |
|-----|-------------|---------------|
| **EM-063** | Pre-screen + provenance guard in eager-refill path | 0 git hits, no bead |
| **EM-065** | Submit/append double-queue guard | 0 git hits, no bead |

*Context:* Both are queue auto-refill obligations added in §4.13–4.14 (v0.7.x). EM-063 guards against dispatching already-dispatched beads during eager-refill; EM-065 prevents double-queuing a bead that is already in the queue via submit or append. Related to named-queues work.

---

### event-model.md (epic hk-hqwn)

| Req | Description | Orphan Reason |
|-----|-------------|---------------|
| **EV-037a** | Subscribe watermark MUST NOT regress across reconnects | 0 git hits, no bead |
| **EV-039** | Heartbeat carries `last_event_id` + `active_runs`; consumer uses to detect gaps | 0 git hits, no bead |
| **EV-040** | Missing heartbeat → consumer MUST treat as daemon liveness failure; reconnect with backoff | 0 git hits, no bead |
| **EV-041** | Git-done-but-no-terminal-event heuristic: consumer infers completion from git log | 0 git hits, no bead |
| **EV-043** | Unacknowledged `decision_required` event blocks dispatch for that run | 0 git hits, no bead |
| **EV-043a** | Daemon startup MUST restore `decision_required` blocking state from persisted log | 0 git hits, no bead |
| **EV-044** | Unacknowledged `decision_required` is a digest exception (MUST appear in digest even if quiet) | 0 git hits, no bead |

*Context:* The subscribe consumer contract (EV-037a, EV-039–041) and `decision_required` dispatch-blocking semantics (EV-043, EV-043a, EV-044) were added in v0.4.x–v0.5.x as part of the flywheel/subscribe bundle. The subscribe implementation landed (hk-6ynv4) but the client-side consumer obligations and the startup-restore requirement for blocking state have no implementation beads. This is the largest gap cluster.

---

### handler-contract.md (epic hk-8i31)

| Req | Description | Orphan Reason |
|-----|-------------|---------------|
| **HC-003a** | Workflow-mode is dispatch-level (not handler-selector); handler MUST NOT branch on it | 0 git hits, no bead |
| **HC-045a** | `claude-code` agent type MUST be governed by claude-hook-bridge.md; cross-spec pointer | 0 git hits, no bead |
| **HC-045b** | Hook-bridge connection regime for short-lived subprocesses | 0 git hits, no bead |
| **HC-061** | Sub-workflow boundary handlers MUST NOT emit an Outcome | 0 git hits, no bead |

*Context:* HC-003a was added to close a clause-gap (handler isolation from dispatch mode). HC-045a/b are the hook-bridge cross-spec obligations (added when claude-hook-bridge.md was created). HC-061 governs sub-workflow composition semantics — no bead exists to enforce the prohibition.

---

### process-lifecycle.md (epic hk-8mup)

| Req | Description | Orphan Reason |
|-----|-------------|---------------|
| **PL-017a** | Hook-bridge relay subprocesses are grandchildren of daemon; orphan-sweep MUST NOT target them or count them against concurrency ceiling | 0 git hits, no dedicated sensor or bead |

*Context:* Relay subprocess code exists but the two prohibitions in PL-017a (exclusion from orphan-sweep targeting AND exclusion from concurrency count) have no sensor and no implementation bead.

---

### operator-nfr.md (epic hk-sx9r)

| Req | Description | Orphan Reason |
|-----|-------------|---------------|
| **ON-008a** | `harmonik supervise start` credential injection + `budget-paused` operator surface | 0 dedicated git hits; partial code but `budget-paused` surface obligation untracked |
| **ON-013d** | Workflow mode immutability: no `harmonik set-mode` verb; iteration cap not runtime-tunable | 0 dedicated impl commits; commented in tests only, no sensor |

*Context:* ON-008a's credential injection part has partial code in `supervisor.go` but the `budget-paused` surface obligation is not explicitly tracked. ON-013d's prohibition (no runtime workflow-mode mutation) is referenced in test comments but has no enforcement sensor.

---

## Follow-Up Beads Filed

The following beads were opened to cover each orphan. All are type=task, priority=P2 unless noted.

| Bead | Req | Title |
|------|-----|-------|
| hk-79x3v | BI-013c | BI-013c: pre-claim status re-read between dispatcher selection and claim write |
| hk-9321v | EM-063 | EM-063: pre-screen + provenance guard in eager-refill path |
| hk-xizhl | EM-065 | EM-065: submit/append double-queue guard |
| hk-u2ko5 | EV-037a | EV-037a: subscribe watermark MUST NOT regress across reconnects |
| hk-qv3bc | EV-039 | EV-039: heartbeat carries last_event_id + active_runs for gap detection |
| hk-ek3fl | EV-040 | EV-040: missing heartbeat → consumer treats as liveness failure + reconnect with backoff |
| hk-p1uz5 | EV-041 | EV-041: git-done-but-no-terminal-event heuristic for subscribe consumers |
| hk-a6e24 | EV-043 | EV-043: unacknowledged decision_required blocks dispatch for that run |
| hk-pbmsq | EV-043a | EV-043a: daemon startup restores decision_required blocking state from log |
| hk-3jcqm | EV-044 | EV-044: decision_required is a digest exception even when quiet |
| hk-c6idw | HC-003a | HC-003a: handler MUST NOT branch on workflow-mode (dispatch-level invariant) |
| hk-ezo2f | HC-045a | HC-045a: claude-code agent type governed by claude-hook-bridge spec |
| hk-iljnj | HC-045b | HC-045b: hook-bridge connection regime for short-lived subprocesses |
| hk-emggz | HC-061 | HC-061: sub-workflow boundary handlers MUST NOT emit an Outcome |
| hk-bjatv | PL-017a | PL-017a: hook-bridge relay grandchildren excluded from orphan-sweep and concurrency count |
| hk-cy8rp | ON-008a | ON-008a: supervise start budget-paused operator surface |
| hk-vj96j | ON-013d | ON-013d: workflow mode immutability enforcement sensor |

---

## Audit Confidence Notes

- **High confidence PASS:** control-points, workspace-model, reconciliation, scenario-harness — small version deltas, implementation fully traced.
- **High confidence GAP:** event-model (EV-037a, 039–041, 043, 043a, 044) — the flywheel subscribe bundle added consumer-side obligations that were never beaded; 0 git hits across all 7 is definitive.
- **Medium confidence GAP:** ON-008a (partial code exists for credential injection but `budget-paused` surface is ambiguous), ON-013d (test-comment references exist but no sensor).
- **Baseline commit:** `b82b2aff` ("Land spec corpus v0.4.x") holds all 5 batch-1 specs at their pinned versions.
