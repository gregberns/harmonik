# ON Pilot r1 — Decomposition-quality review

**Reviewer:** decomposition-quality (per pilot-review-protocol §3.2)
**Inputs:** `specs/operator-nfr.md` v0.4.1; `docs/decompose-to-tasks/on-pilot.md` v0.1.0; `docs/decompose-to-tasks/on-pilot-data.yaml` v0.1.0; `docs/decompose-to-tasks/discipline.md` v0.9; `docs/decompose-to-tasks/pl-pilot-data.yaml` (F8b precedent comparison).
**Sample size:** 17 beads — 4 invariants, 1 error-taxonomy, 3 test-infra, the 9 ON-027 cluster (umbrella + 8 step beads), plus first-class beads ON-005, ON-008, ON-013, ON-014, ON-040, ON-041, ON-027a, ON-029, ON-028, ON-030a, ON-INV-005-spot.

---

## Per-bead findings

### F-pilot-ON-5 lane — ON-027 split umbrella + 8 step beads (CRITICAL)

**Beads:** `on-027` umbrella + `on-027.s1`, `on-027.s2`, `on-027.s3`, `on-027.s3a`, `on-027.s4`, `on-027.s5`, `on-027.s6`, `on-027.s7` (9 beads total).

**Q1 description match.** ALL nine descriptions are faithful to the spec. Spot checks:

- `on-027` umbrella description recites the 8 ordered steps verbatim and re-asserts the §4.3 ON-008 drain-gate precondition (ALL 8 steps complete → `pausing → paused`). Faithful.
- `on-027.s2` cites EM §4.5 for "next durable checkpoint then suspends" + PL-003a JSON-RPC for completion signal — both anchored in spec body. Faithful.
- `on-027.s3a` cites BI-029/BI-030 intent-log + BI-031 status-check classification + `BrUnavailable` escalation to step 4 with next-restart Cat 3a routing — verbatim from spec ON-027 step 3a clause. Faithful.
- `on-027.s4` distinguishes code 21 (non-recoverable error) from code 11 (timeout-escalation) and notes the silent-hang synthesis sequencing — faithful to ON-040 amendment language.
- `on-027.s7` correctly captures the dual exit semantics (process exit vs `enter paused`) and the precondition role for ON-008. Faithful.

**No descriptive blocker found.** Per-step descriptions correctly delegate to the cited subsystem (EM/HC/BI/EV/memory/WM/orchestrator) in each step's body.

**Q3 split-soundness analysis (the F-pilot-ON-5 question).**

§2.2 split signals — ALL THREE FIRE:
1. ≥3 steps ✓ (8 steps; the largest count in the corpus).
2. Independent testability ✓ — each step has its own bound (`timeout.step_N` per ON-029), its own escalation (code 11) and own error mode (code 21 with `drain_error.step` discriminator); ON-027a per-step durable marker provides crash-mid-drain resume from next-uncompleted step. The §10.2 obligation is "graceful-shutdown scenario tests for all eight steps" — the obligation itself reads as an 8-fold independent harness.
3. Umbrella-loses-meaning-when-stripped ✓ — without the steps the rule reduces to "drain in some order."

§2.2 F8b shared-function-body tiebreaker — applied to ON-027:

The pilot's argument (F-pilot-ON-5 long-form): in plausible Go, ON-027's step bodies sit in **distinct subsystems' code paths** — orchestrator stop, EM checkpoint suspension, HC subprocess wait, BI intent-log, EV bus fsync, memory layer, WM unlock, orchestrator exit. The drain orchestration glue is a `for step := range steps { execute(step) }` loop ~30 lines; the BODIES live elsewhere. Each step is a delegating call into a subsystem-owned routine.

**Comparison with PL-005 (collapsed via F8b):** PL-005's 11-step `bootstrap_daemon` is one cohesive sequence inside the `internal/daemon` package — composition root, lock acquire, sweep, socket bind, Cat-0 check, git walk, Beads query, model build, dispatch. Every step touches the same Go package and shares accumulated state (error variables, in-memory model under construction, `daemon_instance_id`). The F8b "shared function body" reading is genuinely "one cohesive function with checkpointed control flow." PL-006 / PL-011 / PL-027 follow the same pattern.

**Verdict: ON-027 split is CORRECT as-is. Severity MINOR. Lane: class (precedent-setting).**

The structural distinction is real and load-bearing. ON-027 is a **delegating orchestrator pattern** — each step has an owning subsystem; the drain loop is the conductor. Discipline §2.2 F8b's "shared function body" criterion as currently worded does not crisply discriminate between (a) one cohesive function with checkpointed state (PL posture, F8b → collapse) and (b) a per-step delegating loop where bodies live in distinct subsystems (ON-027 posture, F8b → split). Both decisions are defensible under v0.9 §2.2 / F8b; the rule's envelope wants clarification.

The pilot's `protocol:multi-step` tag, F8c constraint application (ON-027a + ON-029 own beads with edges from umbrella), and the §11 §2.4 test-infra bead `on-test.security-and-shutdown-harness` all hang together correctly given the split decision.

**Recommended discipline-lane action:** Add a §2.2 F8b worked example pair codifying:

- "One cohesive function body with checkpointed state" (PL-005 startup, EM-016 git atomic) → COLLAPSE.
- "Delegating orchestrator with per-step subsystem-owned bodies" (ON-027 drain) → SPLIT.

Sharpens D-PL-1 / D-PL-2 (deferred to discipline-lane batch). May also pre-resolve RC's multi-step Cat-N dispatch protocols.

**Why not collapse to match PL precedent.** Collapsing ON-027 would lose:

- The per-step `timeout.step_N` ↔ ON-029 wiring (ON-029's per-step apportionment is only meaningful if the steps are bead-addressable).
- The per-step error/timeout discriminator (`drain_error.step` is a per-step field; without step beads the §8 code 21 `drain_error.step=6` field has no bead-addressable home).
- The crash-mid-drain "resume from next-uncompleted step" discipline of ON-027a (the durable marker has step granularity; the bead set should mirror it).
- The cross-subsystem dependency surface — step 3a's BI dep, step 4's EV dep, step 6's WM-cite (informational) all live correctly on individual step beads where the cross-spec edges fire from the right place.

**Why not defer to discipline batch with no immediate pilot rewrite.** Defer is wrong here. The pilot's decision is correct under v0.9; the discipline rule clarification is additive precedent, not corrective. Rewriting ON-027 to collapse would be the wrong action; documenting the F8b worked-example clarification in a discipline patch is the right action and does NOT require pilot rework.

### ON-INV-001 — N-1 compat sensor

**Q1.** Description recites the spec verbatim and reproduces the `Sensor:` line (corpus-wide compat-matrix harness). Faithful.
**Q4.** Sensor description names a real verification mechanism: "for every artifact, write at N + parse at reader pinned to N-1; failure of any pair flips." This is a concrete harness, not a restatement.
**Edges (spot check):** `on-inv-001 → on-018` and `on-inv-001 → on-019`. One-way, sensor-blocks-on-impl per F12. Correct.

### ON-INV-003 — Secrets-never-unredacted sensor

**Q1.** Description faithful; covers joint-hold across ON-022 + ON-023 + HC injection. The WM session-log cite is correctly noted as F-pilot-ON-6 informational (no edge fires; depends-on baseline applies).
**Q4.** Two-part sensor (compile-time linter + regression substring scan) is real verification. Concrete.
**Edges:** `on-inv-003 → on-022`, `on-inv-003 → on-023`. Correct one-way.

### ON-INV-005 — Reconstruction-contribution sensor

**Q1.** Description recites the (a)/(b)/(c) clauses correctly.
**Q4.** Fixture-backed harness ("inject pre-restart state across every subsystem; assert reconstruction-completed signal before `ready`") is a real mechanism. CONCRETE.
**Edge spot-check.** `on-inv-005 → on-error.taxonomy` is emitted (justified: clause (c) — categorized exit code on subsystem reconstruction failure). Per §2.5 / §2.6 — vocabulary ownership cite, consumer-direction. CORRECT.
**Forward-deferred to RC:** present per F-pilot-EM-2 / Option B. Correct.

### ON-INV-006 — No-bypass-control-surface sensor

**Q1.** Description correctly captures the no-local-escape-hatch normative content.
**Q4.** "Corpus-wide grep-plus-reviewer audit" — names a reviewer-persona scan (analog of AR-INV-001 conformance-auditor). Real verification, but **borderline restatement-y** ("any operation not routing through §7.1 flips the invariant"). The ON spec body itself says "Reviewer-enforced pending a mechanical lint." This is honest about the limit and matches the spec; not a bug.
**Edges:** `on-inv-006 → on-008`, `on-inv-006 → on-009`, `on-inv-006 → ar-013`. Correct — `ar-013` is the AR-013 subsystem-envelope-registration anchor cited by the sensor body ("every subsystem spec's §4.a Subsystem envelope per AR-013").

### `on-error.taxonomy` — Single-bead 23-code error taxonomy

**Q1.** Description enumerates all 23 codes verbatim and notes the "authoritative for the corpus" posture (PL §8.x cites by reference).

**Sentinel enumeration completeness check.** Spec §8 lists codes 0..23 (24 entries including code 0 = success). All 24 enumerated in the bead description — codes 1=generic-failure, 2=queue-format-unsupported, 3=checkpoint-schema-unsupported, 4=event-schema-unsupported, 5=pidfile-locked, 6=socket-bind-failed, 7=git-bad-state, 8=beads-unavailable, 9=filesystem-unwritable, 10=disk-full, 11=drain-timeout-escalated, 12=rto-hard-ceiling-exceeded, 13=upgrade-requires-paused, 14=upgrade-hash-mismatch, 15=upgrade-schema-incompatible, 16=operator-control-invalid-state, 17=multi-daemon-target-missing, 18=machine-ceiling-exhausted, 19=runtime-panic, 20=signal-terminated, 21=drain-step-errored, 22=ntm-unavailable, 23=orchestrator-agent-unavailable. **Complete; no missing codes.**

**Single-bead form per F-pilot-ON-4.** Correct application of F-pilot-WM-2 SHAPE-not-COUNT precedent. The 23 codes are sentinel values, not 23 independent codepaths — implementation work for each row lives in the consumer §4 req that emits the code. Same posture as `bi-error.taxonomy`, `wm-error.taxonomy`, `hc-error.taxonomy`. Correct.

### §2.11(c.2) edge-direction sanity (ALL 19 consumer→taxonomy edges)

**Q3.5 (special).** All 19 emitted edges run consumer → owner direction:

`{on-001, on-002, on-003, on-013, on-016, on-019, on-020, on-020a, on-027, on-027.s4, on-027.s5, on-027.s6, on-027.s7, on-029, on-031, on-041, on-048, on-053, on-inv-005} → on-error.taxonomy`

ALL 19 are `from: <consumer>, to: on-error.taxonomy`. **Direction is uniformly consumer→owner per discipline v0.9 §2.11(c.2).** Zero inverted edges. Same anti-pattern that hit EM r1 #15, EV r1 #15, HC r1 LOAD has NOT recurred here. CLEAN.

The taxonomy bead's own predecessors are empty (leaf-shaped on producer side per the description). Correct.

### Test-infra spot checks (3 of 11)

**`on-test.security-and-shutdown-harness`** — gates `on-022..on-029, on-027a, on-027.s1..s7, on-027.s3a, on-040, on-inv-003`. Description names per-step bound exhaustion → §8 code 11 + per-step error → §8 code 21; ON-027a crash-mid-drain assertion; ON-040 silent-hang synthesis sequencing; ON-INV-003 secrets sensor (compile-time linter + regression scan). **Real shared infrastructure**; gates 18 impl beads. Correctly extracted per §2.4.

**`on-test.restart-rto-harness`** — gates `on-030..033, on-053, on-030a, on-inv-005`. Description names p95 measurement under standard fixture, monotonic companion field verification, `rto_undefined` boundary cases (boot-transition + SIGKILL), pause-state durable marker survival, post-panic forensic file content, per-subsystem reconstruction-completed signal. **Real shared infrastructure**; correctly extracted.

**`on-test.upgrade-contract-harness`** — gates `on-020, on-020a, on-021`. Description covers all 7 sub-obligations of ON-020 (binary-source / hash check / drain-vs-reconciliation / cross-version state / fd-passing / rollback / post-exec-replace recovery), ON-020a marker durability + hash-mismatch-on-restart, ON-021 cross-version state preservation (iff drain-completed). **Real shared infrastructure**; correctly extracted.

### First-class §4 spot checks

**ON-005 (commit-hash integrity gate).** Description faithful (fail-closed; `paused` post-mismatch; emits `operator_upgrade_rejected`). Edges: ON-005 has no outgoing edges (per body — gate is a stand-alone normative assertion). ON-005a → ON-005, ON-006 → ON-005 emitted (ldflags source + signing-deferral cite ON-005). F-pilot-ON-2(a) coalesce considered + rejected (test 3 — ldflags stamp + post-MVH carve-out testable independently). Correct.

**ON-008 (between-task invariant).** Description faithful. Edges: `on-008 → on-027` (drain-gate cite), `on-008 → em-017` (EM-017 trailer dep via "next durable checkpoint per EM §4.5"). `cite:wide-fanout` tag applied for `[execution-model.md §4.5]` fanout. Correct.

**ON-013 (per-transition events).** Description recites all 8 emission triggers (operator_pause_status paired-phase, operator_resuming, operator_stopped, operator_upgrading, operator_upgrade_completed, operator_upgrade_rejected, operator_command_rejected, dispatch_deferred). Edges: 8 ev-events.<name> dual-ownership edges per §2.11(d.2) + on-error.taxonomy (codes 16, 18). `cite:wide-fanout` applied for [event-model.md §8.7] fanout. Correct.

**ON-014 (reconciliation operator override).** Description correctly captures CLI naming (`harmonik confirm-verdict` / `veto-verdict --promote-to escalate-to-human`), default-proceeds posture, Cat 2/3/6a applicability. Forward-deferred edge to `forward:rc-NNN` per F-pilot-EM-2 / Option B. Correct.

**ON-040 (silent-hang detection + drain-forced synthesis).** Description faithful: HC §4.6 obligation; agent_warning_silent_hang event; drain-forced synthesis BEFORE SIGKILL within step 4 wait window. Edges: hc-007, ev-events.agent-warning-silent-hang, on-027 (drain step 4 sequencing), on-029 (timeout escalation trigger), on-037 (degraded classification), forward:rc-NNN. Correct.

**ON-041 (multi-daemon commands).** Description faithful: 3 sub-rules (a)/(b)/(c) (`harmonik list` / identification flags / machine-level ceiling); per-daemon vs machine-level ceiling distinction; daemon-discovery scope ($HOME + $HARMONIK_PROJECT_ROOTS); output columns. Edges: pl-014a (per-daemon ceiling contrast), pl-028 (CLI surface), on-error.taxonomy (codes 17, 18), on-049 (budget_summary). `cite:wide-fanout` tag applied. Correct.

**ON-027a (drain step atomicity F8c constraint).** Description faithful (sequential single-goroutine; per-step durable marker; mid-drain crash resumes from next-uncompleted step; idempotent on completed steps). Edges: on-027 (umbrella cite per F8c — `blocks` from umbrella), on-030a (durable marker discipline), on-error.taxonomy (code 21). F8c application correct.

**ON-029 (per-step drain timeouts F8c constraint).** Description faithful. Edges: on-027 (umbrella `blocks`), on-004 (config inventory cite), on-error.taxonomy (code 11). F8c application correct.

**ON-028 (`stop --immediate` skips steps 2–3).** Description faithful. Edges: on-027 (cites the steps it skips), on-009 (carve-out), forward:rc-NNN. **Correctly NOT a step bead** — separate code path / behavior variant. Independent code path with independent failure mode (kill subprocesses, no graceful drain). Same posture as pilot author's intent.

**ON-030a (pause-state durable marker).** Description faithful (atomic temp+rename+fsync+parent-fsync, every transition producing paused/pausing/upgrade-prepare/stopped, PL-005 step 0 read on startup). Edges: on-011 (state machine), pl-005 (startup hook), pl-schema.daemon-status (DaemonStatus enum). WM-026 cite informational per F-pilot-ON-6. Correct.

---

## Q2 (coalesce) — spot checks of rejected candidates

**Yaml claims 0 §2.3 coalesces; 12 candidates considered + rejected.** Spot checks of 2:

**(a) ON-005 + ON-005a + ON-006.** Test 3 reasoning verified — ON-005a (binary-without-stamp test → `failure_mode=binary-stamp-missing`) is independently testable from ON-005's mismatch case. ON-006 deferred-signing carve-out is a conformance-shape rule, not a gate-mechanism rule. Rejection is sound.

**(j) ON-045..049 (budget family).** Test 1 reasoning verified — ON-045 (declared/enforced/attributed pipeline) vs ON-046 (operator-observability surface) vs ON-047 (5-row default table) vs ON-048 (4-step exhaustion protocol) vs ON-049 (5-tuple attribution shape) are 5 distinct rule-shapes addressing 5 orthogonal contracts. Coalescing would force the bead description into 5 sub-bullets that are not anchor + clarifications. Rejection is sound.

**No missing-coalesce smell** observed in the §4.7 and §4.10 clusters.

---

## Q3.b (over-split smell)

**Examined.** ON-027.s1 ("orchestrator dispatcher boolean toggle, ~10 LOC") and ON-027.s5 ("memory layer flush") are the two minimal step beads. Each is a candidate for "could be a sub-bullet." Defended:

- s1 has its own ON-029 timeout knob (`timeout.step_1` in the config inventory by convention) and its own ON-027a durable mark. The boolean toggle is small but the durability discipline pulls it up to bead level.
- s5 is a leaf delegation to memory subsystem (post-MVH owner; not yet specced). Its own bead lets the cross-subsystem edge fire from the right place; collapsing into s4 or s6 would mis-attribute the work.

**No over-split finding.** The 8-step granularity matches the §10.2 obligation enumeration and the per-step discipline of ON-027a + ON-029.

---

## Q5 (schema) — N/A

Yaml claims 0 schema beads (§6.1/§6.2/§6.3 explicitly omitted per spec body). Verified — F-pilot-ON-3 (class — RESOLVED/CONFIRMATION lane) is correct precedent application, mirroring F-pilot-PL-1's zero-§8-ownership instance.

---

## Severity & lane summary

| Finding | Severity | Lane | Rationale |
|---|---|---|---|
| F-pilot-ON-5 (ON-027 split correct as-is) | MINOR | class | Discipline rule envelope wants F8b worked-example pair codifying delegating-orchestrator vs cohesive-function-body distinction. Documentation-only patch; no pilot rewrite. |
| All other sampled beads | (clean) | — | Descriptions faithful; sensors concrete; edges directionally correct; coalesce/split decisions sound. |

**§2.11(c.2) direction sanity:** 19/19 edges correct consumer→owner. CLEAN.

---

## F-pilot-ON-5 lane verdict (explicit)

**Verdict: (a) CORRECT AS-IS.** ON-027's split into umbrella + 8 step beads is the right decision.

**Justification.** F8b's "shared function body" criterion as written in v0.9 §2.2 captures the PL family correctly (one cohesive `bootstrap_daemon` / `graceful_shutdown` / `upgrade_handoff` function with checkpointed state). It does not capture ON-027's delegating-orchestrator structure where each step's body lives in a different subsystem (orchestrator/EM/HC/BI/EV/memory/WM/exit). Three concrete consequences argue against collapsing:

1. ON-029's per-step timeout structure (`timeout.step_2`, `timeout.step_3`, etc.) is a per-step contract — collapsing ON-027 to a single bead would orphan the per-step knob structure.
2. ON-027a's durable mark per step ("resume from next-uncompleted step") IS step-granular crash recovery — the bead set should mirror the durability granularity.
3. §8 code 21 carries `drain_error.step={2,3,3a,4,5,6}` — a per-step error discriminator that is meaningful only if the steps are bead-addressable for the test harness obligation in §10.2.

**Recommended discipline-lane action (NOT a pilot patch):** Add a §2.2 F8b worked-example pair to discipline v0.9 → v0.10 contrasting:

- "Cohesive function body with checkpointed state" (PL-005 startup, EM-016 git atomic) → COLLAPSE.
- "Delegating orchestrator with per-step subsystem-owned bodies" (ON-027 drain) → SPLIT.

This is documentation-tightening; sharpens the boundary between the two patterns and pre-resolves analogous decisions in RC's multi-step Cat-N dispatch protocols.

---

## Summary

**CLEAN** on description-faithfulness, sensor-mechanism-concreteness, schema-completeness, coalesce-soundness (12/12 rejections sound), and §2.11(c.2) edge-direction sanity (19/19 consumer→owner).

**One MINOR class finding (F-pilot-ON-5)**: ON-027 split is correct as-is; discipline §2.2 F8b would benefit from a worked-example pair codifying the delegating-orchestrator vs cohesive-function-body distinction. No pilot patch required; documentation-only discipline patch recommended at v0.9 → v0.10.

No BLOCKER or MAJOR findings. The pilot is structurally sound and ready to load.
