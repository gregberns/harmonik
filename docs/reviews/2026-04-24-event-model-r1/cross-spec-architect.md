# Event Model — Round 1 Cross-Spec Architect Review

**Spec under review:** `/Users/gb/github/harmonik/specs/event-model.md` (v0.1.0, 2026-04-23)
**Reviewer role:** Cross-Spec Architect
**Scope:** verify §8 taxonomy against every event cited by the nine sibling specs.

## 1. Verdict

**Changes required.** The envelope (§4.1–§4.9) and the taxonomy-first structure are sound, and event-model correctly owns payload SHAPE while deferring WHEN to emitters per EV-025. However, the §8 taxonomy has **≥22 concrete gaps** against the emissions declared in the other nine specs. Three classes of defect appear: (a) events emitters cite that §8 does not declare (22 cases); (b) events §8 declares that no emitter cites (4 orphans); (c) events that collide in name or semantics between §8 and the emitter (3 primary conflicts). Every gap is load-bearing — each one is either a §4.10 redaction-check bypass risk (EV-036 scans only registered events), a §4.7 schema-versioning risk (unregistered events have no `schema_version` contract), or a direct §8.9 taxonomy-membership violation (cross-subsystem emissions not in §8 violate EV-027). None of the defects falsifies the spec's architectural posture — the envelope, the three-class consumer taxonomy, the fsync semantics, the replay split, and the Go tagged-union shape all hold. All defects are resolvable by a §8 amendment pass co-sequenced with edits in the five offending emitter specs (control-points, operator-nfr, process-lifecycle, handler-contract, reconciliation).

**Priority ordering for remediation.** (1) Missing `daemon_ready` (RTO measurement endpoint; ON-033 depends on it); (2) naming/semantic conflicts C1–C3 (current state breaks control-points' EV-027 conformance); (3) missing upgrade events G1/G2 (operator-nfr's ON-020 contract currently cites unregistered events); (4) silent-hang event family G16–G18 (handler-contract §7.1 FSM refers to four unregistered events); (5) remaining gaps and orphans in any order.

## 2. Event inventory — gap analysis

§8 declares 54 events. Cross-checking every sibling spec's emission list:

### 2.1 Events emitters cite but §8 does NOT declare (missing)

| # | Emitting spec | Event name | Context | Category |
|---|---|---|---|---|
| G1 | operator-nfr §6.5, ON-013 | `operator_upgrade_completed` | `upgrading` → `running` post-exec-replace | §8.7 |
| G2 | operator-nfr §6.5 | `operator_upgrade_rejected` | commit-hash mismatch | §8.7 |
| G3 | operator-nfr §8 (exit codes) | `daemon_startup_failed` | prereq failure | §8.7 |
| G4 | operator-nfr §8 | `daemon_degraded` | RTO breach / degraded | §8.7 |
| G5 | operator-nfr §8 | `operator_command_rejected` | invalid state transition | §8.7 |
| G6 | operator-nfr §8 | `dispatch_deferred` | machine-ceiling exhausted | §8.7 |
| G7 | process-lifecycle §6.2, PL-009 | `daemon_ready` | startup complete (RTO measurement endpoint) | §8.7 |
| G8 | control-points §6.5, CP-009 | `gate_denied` | Gate `deny` verdict | §8.2 |
| G9 | control-points §6.5 | `gate_allowed` | Gate `allow` verdict | §8.2 |
| G10 | control-points §6.5 | `gate_escalated` | deny→escalate-to-human | §8.2 |
| G11 | control-points §6.5 | `hook_failed` | Hook evaluator typed failure | §8.2 |
| G12 | control-points §6.5 | `guard_reordered` | non-identity Guard reorder | §8.2 |
| G13 | control-points §6.5, CP-040 | `hook_verdict_persisted` | cognition-Hook verdict written to git | §8.2 |
| G14 | control-points §7.1 (pseudocode) | `control_points_registered` | S02 registry startup event | §8.8 |
| G15 | control-points §8 | `guard_failed` | Guard invocation error → `ErrStructural` | §8.2 |
| G16 | handler-contract §7.1 (silent-hang FSM) | `agent_warning_silent_hang` | T-threshold breach | §8.3 |
| G17 | handler-contract §7.1 | `agent_resumed_after_warning` | progress resumed after warning | §8.3 |
| G18 | handler-contract §7.1 | `agent_soft_terminating` / `agent_hard_terminating` | escalation stages | §8.3 |
| G19 | reconciliation §7.1 (pseudocode) | `node_dispatch_requested` | reconciliation dispatches outer run | §8.1 |
| G20 | task prompt expectation | `daemon_started`, `daemon_shutdown` | process lifecycle begin/end | §8.7 |
| G21 | task prompt expectation | `bead_claimed`, `bead_closed`, `bead_reopened` | Beads terminal-transition events | (new §8.X) |

### 2.2 Events §8 declares that NO emitter cites (orphaned)

| # | Event | Declared at | Risk |
|---|---|---|---|
| O1 | `reconciliation_started` | §8.6.1 | Declared but reconciliation §6.5 lists nine events and does NOT include this one. Either the emitter-side emission requirement is missing (RC-013 covers category-assigned but not workflow-started) or the taxonomy entry is dead. |
| O2 | `guard_denied` | §8.2.3 | Semantically impossible per CP-018: Guards MAY NOT deny edges. Name collides with the control-points `guard_reordered` declaration. See C3 below. |
| O3 | `policy_violation` | §8.2.4 | No emitter is identified in any sibling spec. `source_subsystem = policy-engine` implies S02, but control-points never declares this emission. |
| O4 | `health_check` | §8.8.1 | ON-036 obligates a health-check *interface* returning a value; no spec obligates emission of a `health_check` *event*. |

### 2.3 Naming / semantic conflicts

| # | Event-model declares | Emitter spec says | Conflict |
|---|---|---|---|
| C1 | §8.2.2 `gate_evaluated` (single event with `verdict` field `allow`/`deny`/`escalate-to-human`) | control-points §6.5 / CP-009 emits three distinct events: `gate_allowed`, `gate_denied`, `gate_escalated` | **Primary design conflict.** Either collapse to the event-model single-event shape (then CP §6.5 must be rewritten) or split event-model into three. CP's rationale — distinct consumers per verdict — favors the split. |
| C2 | §8.2.3 `guard_denied` | control-points CP-018: Guards cannot deny; CP §6.5 emits `guard_reordered` | Name and semantics both wrong in §8. Rename to `guard_reordered`. |
| C3 | §8.1.8 `outcome_emitted` emitter = `orchestrator-core` | handler-contract HC-007, HC-008: handler subprocess emits `outcome_emitted` as its final progress event | **Emitter ownership conflict.** Per HC-008 the handler is authoritative; event-model row is mis-attributed. Fix: change emitter to "handler (via daemon watcher)". |
| C4 | §8.3.4 `agent_completed` payload includes `exit_code` | HC-008: exit code is liveness-only and not part of the outcome contract | Minor. The field is not wrong, but the spec surface should state exit_code is observational, not authoritative. |
| C5 | §8.3.5 `agent_failed` payload `error_category` enum includes `ErrProtocolMismatch` | handler-contract §4.5 HC-021: `ErrProtocolMismatch` wraps `ErrStructural` | Fine under the errors.Is wrapping rule; but the enum-in-event-payload choice should be explicit in §6.3 that the value is the narrow sentinel where present. |
| C6 | No `retry_event` declared | execution-model §8.1, §8.2 reference "a retry event (transient in-run)" and "a structured retry event" | Either add a typed `run_retry` event to §8.1, or rewrite execution-model §8.1/§8.2 to drop the reference (fold retries into `run_failed` or intra-run node re-dispatch). Current state: execution-model cites an event that does not exist. |
| C7 | §8.6.9 `operator_escalation_required` payload `reason` is reconciliation-centric (Cat 6a/6b) | operator-nfr escalations from non-reconciliation paths (Cat 3 stale writes, budget-exhausted, Cat 3c auto-escalations) all flow through this same event | Widen the `reason` enum, or declare that non-reconciliation operator-escalations use a different event (not currently declared). |
| C8 | §8.7.4 `operator_stopped` single event with `mode` field | operator-nfr §7.1 state machine uses same event but with different mode values (`graceful` | `immediate`); §6.5 redundantly listed | Minor: operator-nfr §6.5 wording "emitted on entry to `stopped`" should point to §8.7.4 without re-declaring. |

### 2.4 Scope-overrun risks

Two categories of over-declaration the current draft should watch:

- **§8.3.3 `agent_output_chunk`.** Event-model §8.9 argues for retention on granularity grounds; control-points CP-024 echoes this for `budget_accrual`. Both are lifecycle-BOUNDARY signals only under a permissive read of §8.9(c). If a future improvement-loop design moves per-chunk consumption to the session log + CASS index instead of the event bus, both events collapse. Flag as OQ candidate (no action now).
- **§8.3.8 `session_log_location`.** Emitter is "agent-runner (S04)" per event-model; handler-contract HC-010 says handler emits it, S04 is the emission authority. Semantic is consistent but the §8.3.8 wording should match HC-010 ("handler subprocess").

## 3. Schema-ownership correctness

Event-model §2.2 and §6.5 correctly assert the ownership split: event-model owns payload SHAPE; emitters own WHEN. Tested against every sibling spec's co-owned-event section:

- **execution-model §6.5** — honors the split; references shapes by event name only. ✓
- **handler-contract §6.4** — honors the split; explicit "event-model is normative for on-the-wire payload". ✓
- **workspace-model §6.3** — honors the split; "WHEN each event fires" phrasing matches. ✓
- **control-points §6.5** — honors the split BUT declares events (`gate_allowed`, `guard_reordered`, `hook_verdict_persisted`, etc.) that event-model §8 does not register. Violates EV-027 (cross-bus additions require foundation amendment).
- **operator-nfr §6.5** — honors the split BUT cites `operator_upgrade_completed` / `operator_upgrade_rejected` not in §8. Same EV-027 violation.
- **reconciliation §6.5** — honors the split cleanly. ✓
- **process-lifecycle §6.2** — honors the split BUT cites `daemon_ready` not in §8. ✓ on posture, ✗ on taxonomy completeness.
- **beads-integration §6.4** — only adds a `bead_id?` field to existing events; does not add new events. ✓ (BUT task prompt expected `bead_claimed` / `bead_closed` / `bead_reopened` as explicit bead-lifecycle events — currently omitted by design; BI-010/BI-011 use run-lifecycle events with bead_id trailers. See recommendation R5.)
- **architecture.md** — no event emissions; names event-model as owner throughout. ✓

Payload-registry invariants (EV-028 through EV-036) are sound as stated. The compile-time secret-name check (EV-036) is load-bearing and will catch drift only for events actually registered; missing-from-§8 events also bypass the check. This is the second-order risk of the §2 gaps.

## 4. Cross-reference issues

- **EV-025 ("exactly one owning spec for payload shape")** reads cleanly, but the observed orphans (O1–O4) reveal that §8 has no reverse-validation: events with no cited emitter currently pass registration. Recommend a lint obligation in §10.2 that every §8 row has at least one sibling-spec emission citation.
- **EV-INV-005 four-axis tagging** — spot-checked §8 entries; most rows are missing the explicit four-axis tags in the table (only §4 requirements carry them). §8.9(e) obligates them; the §8 tables do not carry them in the current draft. Either the tags live in the payload-registry (§6.3) or §8 tables need a fifth column. Current state: under-specified.
- **§9.1 depends-on** cites `[architecture.md]` and `[execution-model.md]` only; missing `[handler-contract.md]` (which declares `outcome_emitted` emitter-side) and `[workspace-model.md]` (which declares `session_log_location` pipeline participation). Depends-on completeness should match the co-ownership matrix in §6.5.
- **§6.3 per-type schemas** — only 7 of 54 payload shapes are given concrete field types. The §6.3 closing statement ("Remaining per-type payloads follow the same pattern") shifts the obligation to the registry code, not the spec. For a normative document, the remaining 47 payload shapes should either be declared in §6.3 or explicitly deferred in an OQ with target revision.
- **§4.10 EV-036 scan** — names the secret-prefix rule from `[handler-contract.md §4.7]`. That dependency is declared in §9.3 co-references, not §9.1 depends-on. Move to depends-on for clarity.
- **Envelope field `source_subsystem` (EV-004).** Declared layout-open (Go package identifier). No registry of valid identifiers. Risk: typos in `source_subsystem` are not caught at startup; two emitters could share an identifier. Recommend: either a startup-time registration (subsystems declare their identifier, duplicates fail init), or accept the risk explicitly in A.3 Rationale.
- **`trace_context` propagation (§6.1).** The field is optional with three sub-fields. No requirement obligates emitters to populate `parent_event_id` when causal linkage exists. EV-008 partial-order contract says "tooling that requires stricter ordering MUST insert explicit causal references into payloads (e.g., `triggering_event_id` on `hook_fired`)." This conflicts with the envelope-level `trace_context.parent_event_id` — two mechanisms for the same concept. Consolidate on one.
- **Consumer taxonomy cross-reference.** EV-010 / EV-011 / EV-012 define three consumer classes; sibling specs (workspace-model §6.3, reconciliation §6.5, operator-nfr §6.5) list "typical consumers" in their event emission sections but do not classify them by class. Consider a §6.5 registry in event-model enumerating which subsystems subscribe to which events in which class. Currently there is no single authoritative view of which consumers are `synchronous` (whose failure halts the producer per EV-010).

### 4.1 Emitter-spec conformance summary

| Spec | Events emitted per its §6 | Events absent from §8 | EV-027 conformance |
|---|---:|---:|---|
| execution-model.md | 10 | 0 (but references a non-declared "retry event") | ✓ with caveat C6 |
| handler-contract.md | 11 | 4 (silent-hang FSM family G16–G18) | ✗ — §7.1 FSM cites unregistered events |
| workspace-model.md | 7 | 0 | ✓ |
| control-points.md | 11 | 8 (G8–G15) | ✗ — §6.5 entire list non-conformant |
| operator-nfr.md | 7 + 6 exit-code events | 6 (G1–G6) | ✗ — ON-013 cites G1, G2 |
| process-lifecycle.md | 3 | 1 (G7 daemon_ready) | ✗ — PL-009 cites G7 |
| reconciliation.md | 9 | 1 (G19 node_dispatch_requested) | ✓ on §6.5, ✗ on §7.1 pseudocode |
| beads-integration.md | 0 new (bead_id on existing) | 3 if explicit bead-lifecycle events are required (see R5) | ✓ under current design |

Five of nine sibling specs emit at least one cross-bus event that §8 does not register. This is the dominant risk to event-model's normative authority. The pattern is consistent: emitter specs were written assuming a larger taxonomy than event-model §8 currently declares. Resolution can move in either direction (expand §8 to cover cited events, or trim emitter specs to only cite §8-declared events) but must be explicit and cross-referenced.

### 4.2 Payload coverage in §6.3

§6.3 currently declares concrete YAML payload schemas for 7 events: `run_started`, `run_failed`, `checkpoint_written`, `transition_event`, `agent_output_chunk`, `store_divergence_detected`, `reconciliation_verdict_executed`, `infrastructure_unavailable`, `daemon_orphan_sweep_completed` (9 total on close reading). The remaining 45+ declared events in §8 have payload fields named in the §8 tables but NO §6.3 entry. The closing paragraph of §6.3 shifts the obligation to "the registry of EV-032" — i.e., the Go code. For a normative spec this is under-specified: a reader wanting to write a non-Go consumer (dashboards, export tools) has only the §8 table cell text, not a typed schema. Either declare every payload in §6.3, or explicitly scope the normative contract to the field-name list in §8 and mark the §6.3 entries as "selected examples."

**R1. §8 amendment pass (blocking).** Add events G1–G21 to §8 under their proper sub-section. Prioritize G7 `daemon_ready` (RTO measurement endpoint; operator-nfr ON-033 depends on it), G1/G2 (upgrade contract ON-020 depends on them), and G8–G13 (control-points §6.5 is currently non-conforming with EV-027). Co-sequence with emitter-spec edits where names differ.

**R2. Resolve C1 (gate_evaluated vs split) at the design level.** Preferred: split to `gate_allowed`, `gate_denied`, `gate_escalated` per CP §6.5. Rationale: distinct consumers (audit vs reconciliation vs operator-escalation), and the current single-event shape requires consumers to parse `verdict` enum at dispatch time. Amend §8.2 accordingly.

**R3. Fix C2 `guard_denied` → `guard_reordered`.** Delete §8.2.3 current row; replace with `guard_reordered` emitting only on non-identity reorders per CP-018. Add `guard_failed` per G15. Strengthens CP-INV-004 (Guards are mechanism-only).

**R4. Fix C3 emitter attribution.** Change §8.1.8 `outcome_emitted` emitter column from `orchestrator-core` to `handler (via daemon watcher)` per HC-008. Update §6.5 accordingly.

**R5. Decide on explicit bead-lifecycle events.** Current design routes bead transitions through `run_started`/`run_completed`/`run_failed` with `bead_id?`. Task-review expectation was explicit `bead_claimed` / `bead_closed` / `bead_reopened`. Either justify the current routing in A.3 Rationale (with cross-reference to BI-010) or add dedicated events. Recommend the current routing (no new events) but document the decision.

**R6. Resolve orphans O1–O4.** For each: add the corresponding emission requirement in the appropriate sibling spec (`reconciliation_started` in RC-013 sibling for RC-001 dispatch; `policy_violation` in control-points §4.7 if kept; `health_check` either as a recurring event with ON-037 cadence or as an OQ-deferred non-event health-interface only).

**R7. §8 four-axis tagging discipline (EV-INV-005).** Add a fifth column to every §8.N sub-section table, or move the tags into the §6.3 payload registry with a §8.9 check. Current state violates §8.9(e).

**R8. §6.3 payload completion.** Declare the remaining 47 payload shapes in §6.3, OR add OQ-EV-005 tracking their landing within a revision cycle. Without this, the "normative for SHAPE" posture is formally hollow for 87% of declared events.

**R9. Add §10.2 lint obligation.** "Every §8 row must have at least one cited emission in a sibling spec" closes the orphan-event drift path.

**R10. §9.1 completeness.** Add `handler-contract` (for `outcome_emitted` co-ownership) and `workspace-model` (for `session_log_location`) to depends-on; move `handler-contract §4.7` redaction registry from §9.3 co-references to §9.1 depends-on since EV-035 depends on it normatively.

**R11. Taxonomy-amendment process documentation.** The current §8.9 acceptance criteria list six gates (cross-subsystem consumer, boundary, granularity, payload, tags, replay-side-effect). It does not name the lifecycle — who proposes, who reviews, which specs must co-change. Add a short §4.10 amendment-mechanics paragraph cross-referencing `architecture.md §1.5` (foundation amendment protocol), listing the three required artifacts for a §8 addition: (a) the new §8 row with payload, tags, and emitter; (b) the emitter-spec edit adding the emission requirement; (c) at least one consumer declared in another spec. Without this, EV-027 currently points to an amendment protocol without naming the §8-specific evidence required.

**R12. Explicit `run_retry` event or retract the reference.** Execution-model §8.1 and §8.2 cite "a retry event (transient in-run)" twice. Either add §8.1.11 `run_retry` with payload `{run_id, node_id, attempt_index, next_retry_at}`, or edit execution-model §8 to drop the reference and fold in-run retry signaling into `outcome_emitted` with `outcome_status = RETRY`. Recommend the latter (consistent with EM-015 which already says `outcome.status = RETRY` transitions are intra-run loops, not durable events).

## 6. Out-of-scope observations (informative)

These are outside the cross-spec-architect remit but worth surfacing to other reviewers:

- **Ordering across §8.3 agent events.** HC-INV-004 pins the sequence `handler_capabilities → session_log_location → skills_provisioned → agent_ready → (work dispatch)`. Event-model declares `agent_started` (§8.3.2) and `agent_ready` (§8.3.1) as separate events but the §4.2 clock-and-ordering requirements don't give a consumer a mechanism to assert the HC-INV-004 ordering without consulting handler-contract. Consider a §6.1 `causally_after` trace-context field or an explicit cross-reference in §4.2 to HC-INV-004.
- **`agent_output_chunk` and session-log boundary.** Per EV-005, events are lifecycle-boundary signals and agent-internal detail lives in session logs. But `agent_output_chunk` is borderline — per-chunk is arguably agent-internal. §8.9 rationale retains it for improvement-loop needs; the §4.5 replay-semantics do not reconcile how a replay-safety=safe event with per-chunk cardinality interacts with EV-017 event-loss-between-fsyncs. A hard crash between two chunk events loses chunks silently; that is acceptable under the observational-only posture but worth calling out in A.3 Rationale.

---

## Appendix: event-inventory cross-check table

Cross-reference of every §8 declared event against each sibling spec's emission table. ✓ = declared and cited; ✗ = gap; — = not relevant.

| §8 ref | Event | exec | hand | work | cp | ops | proc | rec | beads |
|---|---|---|---|---|---|---|---|---|---|
| 8.1.1–3 | run_started/completed/failed | ✓ | — | — | — | ✓ | — | — | ✓ |
| 8.1.4–5 | state_entered / state_exited | ✓ | — | — | — | — | — | — | — |
| 8.1.6 | transition_event | ✓ | — | — | — | — | — | — | — |
| 8.1.7 | checkpoint_written | ✓ | — | — | — | — | — | — | ✓ |
| 8.1.8 | outcome_emitted | ✓ | ✓ (conflict C3) | — | — | — | — | — | — |
| 8.1.9–10 | sub_workflow_entered/exited | ✓ | — | — | — | — | — | — | — |
| 8.2.1 | hook_fired | — | — | — | ✓ | — | — | — | — |
| 8.2.2 | gate_evaluated | — | — | — | ✗ (conflict C1) | — | — | — | — |
| 8.2.3 | guard_denied | — | — | — | ✗ (conflict C2) | — | — | — | — |
| 8.2.4 | policy_violation | — | — | — | ✗ (orphan O3) | — | — | — | — |
| 8.3.1–10 | agent_* / handler_capabilities / skills_provisioned / session_log_location | — | ✓ | ✓ (session_log) | ✓ (skills) | — | — | — | — |
| 8.4.1–3 | budget_* | — | — | — | ✓ | — | — | — | — |
| 8.5.1–7 | workspace_* / merge_conflict_escalation | — | — | ✓ | — | — | — | ✓ (interrupted) | — |
| 8.6.1 | reconciliation_started | — | — | — | — | — | — | ✗ (orphan O1) | — |
| 8.6.2–8 | reconciliation_* / store_divergence_detected | — | — | — | — | — | — | ✓ | ✓ (divergence) |
| 8.6.9 | operator_escalation_required | — | — | — | — | ✓ | — | ✓ | — |
| 8.7.1–6 | operator_* / daemon_orphan_sweep_completed | — | — | — | — | ✓ | ✓ | — | — |
| 8.7.7 | infrastructure_unavailable | — | — | — | — | — | ✓ | ✓ | — |
| 8.8.1 | health_check | — | — | — | — | ✗ (orphan O4) | — | — | — |
| 8.8.2–4 | metric / consumer_failed / dead_letter_enqueued | — | ✓ (dead_letter) | — | — | — | — | — | — |

**Missing rows** (should exist in §8 but do not): daemon_ready, daemon_started, daemon_shutdown, operator_upgrade_completed, operator_upgrade_rejected, daemon_startup_failed, daemon_degraded, operator_command_rejected, dispatch_deferred, gate_allowed, gate_denied, gate_escalated, hook_failed, hook_verdict_persisted, guard_reordered, guard_failed, control_points_registered, agent_warning_silent_hang, agent_resumed_after_warning, agent_soft_terminating, agent_hard_terminating, node_dispatch_requested. Count: 22 missing events. Combined with four orphans (O1–O4) and three primary naming conflicts (C1–C3), the §8 surface requires substantial amendment before finalize.

**Totals.** 54 declared events, 22 missing emissions, 4 orphan declarations, 3 primary conflicts, 5 minor conflicts (C4–C8). Five of nine sibling specs require coordinated edits. The spec is structurally sound but taxonomically incomplete; reviewer recommends the Round 1 gate not be passed until R1–R5 are resolved.
