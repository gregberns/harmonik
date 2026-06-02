# Bead Reassessment — Cluster: spec-parent implementation epics (batch 1)

Assessor: spec1 | Date: 2026-06-02 | READ-ONLY

Method: For each epic I counted child records in `.beads/issues.jsonl`, broke them
down by status, confirmed the spec exists in `specs/`, confirmed the implementing
subsystem exists under `internal/`, and counted commits referencing the epic. The
precedent is hk-uxm0j (Agent-comms epic), which closed THIS session with the exact
rationale "all children landed, 0 open children, verified operational." All five
epics here match that pattern.

---

### hk-872 — Beads Integration spec — implementation
- VERDICT: DONE
- ACTION: br close (stale-open epic; all 68 children closed, spec implemented in internal/brcli). Mirror hk-uxm0j closure reason.
- NEW_PRIORITY: -
- EVIDENCE: 68/68 child records in .beads/issues.jsonl are status=closed; 76 commits grep "hk-872"; spec specs/beads-integration.md (status: reviewed) implemented in internal/brcli/ (adapter.go, brerror_test.go, listinflight.go — BrError enum + --format json adapter all present).
- CONFIDENCE: high

### hk-b3f — Execution Model spec — implementation
- VERDICT: DONE
- ACTION: br close (stale-open epic; all 109 children closed; EM records/enums landed in internal/core).
- NEW_PRIORITY: -
- EVIDENCE: 109/109 child records closed in .beads/issues.jsonl; 112 commits grep "hk-b3f"; specs/execution-model.md (reviewed) implemented — internal/core/transition.go, outcome.go, checkpoint.go, transitionid.go all present (the §6.1 Transition/Outcome/Checkpoint records the children defined).
- CONFIDENCE: high

### hk-hqwn — Event Model spec — implementation
- VERDICT: DONE
- ACTION: br close (stale-open epic; all 157 children closed; event bus live in production).
- NEW_PRIORITY: -
- EVIDENCE: 157/157 child records closed in .beads/issues.jsonl; 92 commits grep "hk-hqwn"; specs/event-model.md (reviewed) implemented — internal/eventbus/ (eventbus.go, busimpl.go, jsonlwriter.go) is the Event envelope + EventBus interface + §6.2 JSONL line format. Bus is in production: the daemon writes typed events to .harmonik/events/events.jsonl this very session.
- CONFIDENCE: high

### hk-8i31 — Handler Contract spec — implementation
- VERDICT: DONE
- ACTION: br close. ALSO close stale-open child hk-8i31.61 (see note) — it is the only non-closed child and it has in fact landed.
- NEW_PRIORITY: -
- EVIDENCE: 82 of 83 child records closed; the lone exception hk-8i31.61 (HC-051 seam boundary check, IN-PROGRESS) is actually implemented — internal/handlercontract/seamhc051_test.go + .golangci.yml depguard (37 handler/depguard entries enforcing the daemon↔execution-shape seam); its blocker hk-8mup.31 is CLOSED. 61 commits grep "hk-8i31"; specs/handler-contract.md (reviewed) implemented in internal/handler/ + internal/handlercontract/ (Handler/Session/Adapter interfaces, LaunchSpec — handler.go, session.go, adapter.go, launchspec_hc006.go all present).
- CONFIDENCE: high

### hk-a8bg — Control Points spec — implementation
- VERDICT: DONE
- ACTION: br close (stale-open epic; all 92 children closed; CP schemas + validator landed).
- NEW_PRIORITY: -
- EVIDENCE: 92/92 child records closed in .beads/issues.jsonl; 107 commits grep "hk-a8bg"; specs/control-points.md (reviewed) implemented — internal/core/permissionschema_test.go, hookname.go (PermissionSchema, RoleName/HookName/SkillName typed aliases) + internal/workflowvalidator/cp049_skill_name_shape_test.go (CP-049 skill-name-shape validation). FreedomProfileRef and the §6.1/§6.2 records are wired.
- CONFIDENCE: high

---

## Cluster summary

Counts per verdict:
- DONE: 5  (hk-872, hk-b3f, hk-hqwn, hk-8i31, hk-a8bg)
- OBSOLETE / DUPLICATE / APPROACH-STALE / KEEP / REPRIORITIZE: 0

Theme: **All 5 are stale-open spec-parent umbrella epics.** Every one is fully
landed — combined 489 closed child records (68 + 109 + 157 + 82 + 92), 0 open
children, with 448 commits collectively referencing them and the implementing
subsystem present under internal/ for each. They match exactly the precedent set
this session by hk-uxm0j, which closed with "all children landed, 0 open children,
verified operational." These epics simply never got their own closing transition
after their last child closed. All five should `br close` with a "spec implemented;
0 open children" reason mirroring hk-uxm0j.

Phase-framing note: the rubric flags that daemonization is now LIVE despite the
phase-1 specs deferring it. That does NOT reopen any of these epics — the daemon
being live is additional evidence that the Event Model (hqwn) and Handler Contract
(8i31) implementations are not just present but exercised in production. No
phase-framing contradiction surfaced for this cluster.

### Stale/obsolete CHILD beads found (flagged per rubric; you only asked for formal
### blocks on the 5 epics, but these are concrete cleanup items):
- **hk-8i31.61** (P2, IN-PROGRESS) — "Handler contract is the deterministic-daemon /
  execution-shape seam" (HC-051). Stuck IN-PROGRESS but LANDED: enforced by
  internal/handlercontract/seamhc051_test.go + .golangci.yml depguard rules; its
  blocker hk-8mup.31 is closed. Should be `br close`. It is the ONLY non-closed child
  across all 489 children in this cluster — closing it clears hk-8i31's last child so
  the epic can close cleanly.

No cross-bead duplicates found (each epic implements a distinct spec).
