# RC Pilot r1 — Decomposition-Quality Review

`reviewer: decomposition-quality reviewer (r1)` · `date: 2026-04-30` · `pilot-version: rc-pilot.md v0.1.0 + rc-pilot-data.yaml v0.1.0` · `discipline: v0.9` · `spec: specs/reconciliation/spec.md v0.4.0 + specs/reconciliation/schemas.md v0.4.0`

Sample size: **15 beads** sampled per protocol §3.2 (2 invariants + 4 schemas + 12 §8 taxonomy beads + 1 test-infra + 4 weighted §4 reqs incl. RC-018 + RC-025a — totals 23 individual touches; effective unique-bead sample is the 15 below since several reqs appear under multiple lenses). Findings are per bead unless a class-lane signal aggregates several into one finding.

---

## 1. Per-bead findings

### 1.1 Invariants (Q4 — sensor verification)

**`rc-inv-001` — Reconciliation-as-workflow uniqueness across daemon lifecycle (RC-INV-001).**
- Q1 (description fidelity): Description faithfully cites the spec body (cuts across RC-001/002/002a/021/025) and reproduces the three-part Sensor (a) registry tag, (b) JSONL-join audit, (c) per-lifecycle sample-rate. Matches spec.md §5 RC-INV-001 verbatim within paraphrase tolerance. **Clean.**
- Q4 (sensor real?): Yes — three-part sensor names a concrete lint surface (Workflow registry tag), a concrete query surface (JSONL join), and a concrete cadence (one tag check per N=10 verdicts; full scan at shutdown). All three are mechanically implementable. **Clean.**
- Edge directionality: blocks-on rc-001/002/002a/021/025 — all impl reqs the body cuts across. Sensor→sensor edge `rc-inv-001 → em-inv-005` per body explicit ID cite to `[execution-model.md §5 EM-INV-005]` (F-refs-EV-6 v0.8 extension). **Clean — direction correct.**
- Discrepancy (minor, non-blocking): pilot prose §6 enumeration claims `RC-INV-001 → umbrella` (consumer→`rc-error.taxonomy`); yaml does NOT emit this edge (yaml has rc-inv-001 → workflow-class-extension instead). The omission is defensible — RC-INV-001's body does not cite the §8 taxonomy by category; it cites Workflow-Class. The yaml's edge set is correct; the pilot prose enumeration is loose. **Minor (lane: local; severity: doc-tightening).**

**`rc-inv-004` — Evidence-corroboration guarantee across detector runs (RC-INV-004).**
- Q1: Description faithful. Cuts across detector dispatch points + EV contract. Sensor — detector-emission-layer validation of `corroboration ∈ {git-corroborated, beads-corroborated}` + audit-log sample at startup — matches spec.md §5 RC-INV-004 body. **Clean.**
- Q4: Sensor is mechanically implementable (emission-time validator + audit sampler). **Clean.**
- Edges: blocks-on rc-014, rc-019a, rc-020a (per spec body explicit cite to "across detector dispatch points (startup, on-demand, scheduled per RC-020a)"), ev-023a (cross-spec), rc-error.cat-6b (Cat 6b corroboration carve-out). Direction correct. **Clean.**

### 1.2 Schemas (Q5 — field/enum completeness)

**`rc-schema.snapshot-token` (schemas.md §6.1 RECORD).**
- 3 fields enumerated correctly: `git_head_hash` (String), `beads_audit_entry_id` (String), `captured_at_timestamp` (Timestamp). Description correctly notes the timestamp's "advisory display only per [event-model.md §4.3]" qualifier. Predecessors: none required (root schema). Consumed by RC-015 dispatch + RC-024 staleness check, both edges emitted. **Clean.**

**`rc-schema.verdict` (schemas.md §6.1 ENUM, 7 values).**
- All 7 values enumerated with one-line glosses: resume-here, resume-with-context, reset-to-checkpoint, reopen-bead, accept-close-with-note, no-op-accept, escalate-to-human. Matches spec.md §3 glossary "seven enum values" + schemas.md §6.1 ENUM Verdict body. Consumer edges (RC-020 vocabulary lock + RC-025 mechanical execution) named correctly in description. **Clean.**

**`rc-schema.reconciliation-category` (schemas.md §6.1 ENUM, 11 values).**
- All 11 values enumerated (cat-0/1/2/3/3a/3b/3c/4/5/6a/6b). Description correctly identifies this as "the vocabulary backing the 11 §8 taxonomy beads" — the schema-shaped predecessor of the umbrella per discipline §2.11(c.2). The umbrella's only direct predecessor is this enum bead (verified at edge `rc-error.taxonomy → rc-schema.reconciliation-category`, line 1430 of yaml). **Clean.**

**`rc-schema.workflow-class-extension` (schemas.md §6.5).**
- Description faithfully reproduces the §6.5 extension declaration (`EXTENSION OF ExecutionModel.Workflow: workflow_class : WorkflowClass`) plus the WorkflowClass ENUM (`reconciliation` value, with reserved-future-use note for `improvement-loop` / `operator-cli-handler`). Semantics paragraph (subject to RC-002, RC-002a, RC-INV-001) and ownership paragraph (RC-owned per OQ-RC-002 acceptance) both reproduced.
- **Discrepancy (minor):** yaml field `schema_kind: record` (line 1133) but `extra_labels: [kind:enum]` (line 1138). The §6.5 declaration is BOTH a record extension (the `workflow_class` field added to Workflow) AND an enum (WorkflowClass). The pilot's choice to label the bead `kind:enum` reflects a judgment that the ENUM is the "primary" shape; the `schema_kind: record` field-name is then misleading. **Recommended fix:** either rename to `schema_kind: enum` for consistency or split into two beads (record-extension + enum) — the latter is over-decomposition; recommend the former. **Minor (lane: local; severity: bookkeeping; DOES NOT block r1).**

### 1.3 §8 Taxonomy beads (Q1 + §2.11(c) split verdict)

**Sampled all 12 §8 beads (1 umbrella + 11 per-category).** This is the canonical large-§8 case per F-pilot-RC-4.

- **`rc-error.taxonomy` (umbrella).** Description carries the priority order (`Cat 0 → 6b → 6a → 5 → 3c → 3b → 3a → 3 → 2 → 4 → 1`), the dual-table ownership note (§8.12 prose semantics ↔ schemas.md §6.3 mechanical dispatch), the cross-category invariants (priority order + dual-table sync + amendment-protocol gate), AND the §2.11(c.2) vocabulary-owner clause ("consumer §4 reqs that cite a category by ID block-on this umbrella OR the specific per-category bead"). The umbrella's `rc-schema.reconciliation-category` predecessor edge is the only correct predecessor per discipline §2.11(c.2). **Clean.**
- **`rc-error.cat-0` (§8.1 infrastructure unavailable).** Detection rule (br --version + Beads SQLite + git index + .harmonik/ + disk-full) + default response (halt + degraded + retry) + emitted event (`infrastructure_unavailable`) + investigator/auto-resolver columns (No / Yes wait-and-retry) + co-ownership note (PL-010 degraded-state co-owned). All match spec.md §8.1. **Clean.**
- **`rc-error.cat-1` (§8.2 idempotent rerun).** Detection rule (`idempotency_class = idempotent` per EM-009) + auto-respawn + investigator/auto-resolver columns (No / Yes spawn) + RC-002 carve-out note (recon-workflow nodes never classified here). Matches spec. **Clean.**
- **`rc-error.cat-2` (§8.3 non-idempotent in-flight).** Tri-conjunct detection rule reproduced (idempotency-class ∈ {non-idempotent, recoverable-non-idempotent} + bead in_progress + no run_completed/failed since checkpoint) + Cat-2-uses-controlled-indexed-query qualifier (RC-014) + investigator-yes / playbook-required column. Matches. **Clean.**
- **`rc-error.cat-3` (§8.4 store-disagreement-generic).** Detection rule (git/Beads/JSONL inconsistent + no Cat 3 sub-category matches) + examples (duplicate transition_id; bead in_progress + worktree-missing) + investigator-yes (git-wins per RC-INV-001). Matches. **Clean.**
- **`rc-error.cat-3a` (§8.4a torn-Beads-write).** Detection rule (intent-file present + bead status neither pre nor post) + auto-resolver via BI-031 + escalation to Cat 3 generic on BrSchemaMismatch. Matches. **Clean.**
- **`rc-error.cat-3b` (§8.5 verdict-unexecuted).** Detection rule (verdict commit + no Harmonik-Verdict-Executed commit) + RC-026 re-execution path + RC-026a retry cap N=5 → Cat 6b. Matches. **Clean.**
- **`rc-error.cat-3c` (§8.6 inverse premature-close).** Detection rule (merge commit + bead in_progress + no in-flight checkpoints) + auto-verdict accept-close-with-note via idempotency-keyed adapter + rationale (git authoritative for completion per RC-INV-001). Matches. **Clean.**
- **`rc-error.cat-4` (§8.7 recoverable-known-state).** Detection rule (retry/backoff state) + auto-resume re-arm + retry-cap-reached reclassification per EM §8. Matches. **Clean.**
- **`rc-error.cat-5` (§8.8 clean restart).** Detection rule (nothing in-flight, incl. orphaned branches per RC-010 AND crashed reconciliation-workflow branches per RC-003b discriminated by Workflow-Class trailer). The RC-003b carve-out is correctly absorbed into Cat 5's description, NOT into Cat 6a. Matches. **Clean.**
- **`rc-error.cat-6a` (§8.11 integrity-violation, LLM-triageable).** All four detector triggers reproduced (workspace-missing + sibling-absent; trailer/sibling mismatch; in-progress git op; multi-task-branch with no verdict-executed). Default verdict `escalate-to-human`. Matches. **Clean.**
- **`rc-error.cat-6b` (§8.11a integrity-violation, mechanically-unrecoverable).** Detection rule (JSONL corrupt; checkpoint hash missing from ODB; `git fsck` fails) + auto-escalate without investigator + corroboration carve-out per RC-019a noted. Matches. **Clean.**

### 1.4 Test-infra (Q4 — verification surface real)

**`rc-test.crash-recovery-suite` (gates RC-031 + every detector + verdict-execution path).** Description enumerates 6 crash-injection points (between RC-013/dispatch; between RC-018 events; between RC-022/RC-025 commits; between RC-024 re-capture and RC-025; inside RC-025a; inside RC-018) and routes each to the documented restart classification. The 6-point enumeration is concrete and testable. **Clean.**

### 1.5 Weighted §4 reqs

**`rc-018` — Budget exhaustion 5-step handler (Q3 — F8b verdict; F-pilot-RC-5 anchor).**
- Q1: Description reproduces the 5-step sequence: (1) terminate investigator SIGTERM/SIGKILL after HC-018; (2) wait watcher per HC-011; (3) emit `budget_exhausted` (class F per EV §8.4.3); (4) emit fallback `escalate-to-human`; (5) verdict-executor (RC-025a) consumes fallback. All 5 steps in spec.md lines 502-504 match. **Faithful.**
- Q3 verdict: Pilot collapses to one bead per F8b. **My verdict: COLLAPSE IS CORRECT (a) yes.** Argument:
  - Steps 1+2 are kill+await — single subprocess-management goroutine; share the investigator pid + watcher channel as locals.
  - Steps 3+4 are two event emissions — same emitter goroutine; share event-envelope construction state.
  - Step 5 is a function-call handoff to the verdict-executor (RC-025a is "a deterministic Go subroutine, NOT a workflow node" per the spec body of RC-025a). The handoff is in-process; no subsystem boundary.
  - Operationally, this matches EM-016's atomic git sequence (writeTree → commitTree → updateRef): conceptually distinct but cohesive within one function body.
  - **Distinct from ON-027:** ON-027's 8 steps each delegate to a DIFFERENT subsystem owner (orchestrator/EM/HC/BI/EV/memory/WM/orchestrator-exit). RC-018's 5 steps are kill+await+emit+emit+handoff — all within the daemon's budget-exhaustion handler.
- **Discrepancy (minor):** pilot doc table (line 104) lists `rc-018 → rc-025a` and `rc-018 → rc-error.cat-3b` as blocks edges; yaml has neither directly emitted. Yaml has `rc-018 → rc-026a` (which transitively reaches Cat 3b via cap-exceeded escalation) and rc-018 has no direct rc-025a edge. Step 5 of the spec body says "verdict-executor (RC-025a) consumes fallback" — supporting cite per F-pilot-AR-10 (removing RC-025a leaves RC-018's "issue fallback verdict" obligation testable). The yaml's omission is defensible under F-pilot-AR-10. The pilot prose's listing is loose. **Minor (lane: local).**

**`rc-025a` — Daemon-side verdict-executor 7-step subroutine (Q3 — F8b verdict; F-pilot-RC-5 anchor).**
- Q1: Description reproduces the 7-step sequence: (1) validate per RC-020/023; (2) re-snapshot per RC-024; (3) commit verdict-emitted; (4) mechanically apply (BI/WM/EM dispatch); (5) commit verdict-executed; (6) emit both events; (7) release RC-002a lock per RC-002b. Panic-safe per PL-018a; on panic mid-step, re-classifies via Cat 3b. Matches spec.md lines 597-607. **Faithful.**
- Q3 verdict: Pilot collapses to one bead per F8b. **My verdict: COLLAPSE IS CORRECT (a) yes,** but the case is closer than RC-018:
  - Spec body explicitly says "a deterministic Go subroutine, NOT a workflow node" — strong language for single-function-body.
  - Steps 1-3 + 5-7 share goroutine state (verdict envelope, snapshot, investigator-run handle, lock fd).
  - **Step 4 is the one weak point:** "mechanically applies the verdict's action per the table" delegates to BI adapter (reopen-bead), WM worktree-reset (reset-to-checkpoint), or EM dispatch (resume-here / resume-with-context). At first glance this looks like ON-027's delegating-orchestrator pattern.
  - **Why collapse still wins:** the dispatch in step 4 is a per-verdict subroutine call within the executor — analogous to RC-018 step 1's `kill investigator per HC-018` (also a delegated call). The delegation is method-call-within-function-body, NOT handoff-to-another-agent. Each per-verdict subroutine is short (BI reopen invocation; WM reset call; EM dispatch wakeup) and inherits the executor's snapshot + envelope.
  - **Counter-argument considered:** if step 4 were 7 steps with each delegating to a different subsystem, we would split. But RC-025a step 4 is one method-call-with-7-shape-cases (the 7 verdict values) — a switch statement, not a sequence. Each case is short. PL-005's 11-step startup is the closer analog (collapsed via F8b per PL precedent).
  - **My position: RC-025a's collapse is correct; F-pilot-RC-5 verdict (a) yes.** The verdict-executor IS a single function body in any plausible Go implementation.

**`rc-007` — Action-mapping is the dispatch contract.**
- Q1: Faithful to spec.md §4.2 RC-007 (taxonomy classifies what went wrong; action-mapping table is what daemon does by default; deviation MUST be declared in YAML with rationale; dual-table ownership; divergence is lint failure). **Clean.**
- Edge: rc-007 → rc-error.taxonomy (consumer→owner per §2.11(c.2)). **Direction correct.**

**`rc-008` — Auto-resolver categories MUST have a deterministic resolver.**
- Q1: Faithful — every auto-resolver category (Cat 0/1/3a/3b/3c/4/5/6b) MUST have deterministic Go-code implementation; investigator-required (Cat 2/3/6a) MUST have a playbook per RC-015/RC-016. **Clean.**
- Edges: rc-008 → rc-error.taxonomy (umbrella) + 8 per-category edges (cat-0/1/3a/3b/3c/4/5/6b). All consumer→owner direction. The 8-category enumeration matches the spec body's parenthetical. **Clean — direction correct.**

---

## 2. F-pilot-RC-5 explicit verdict (cohesive-function-body collapse)

**Verdict: (a) yes — both RC-018 and RC-025a F8b collapses are correct.**

Rationale (combined):
- **RC-018:** 5 steps live in one budget-exhaustion handler goroutine. Steps 1-2 are kill+await on a single investigator pid; steps 3-4 are two event emissions from the same emitter; step 5 is an in-process function-call handoff to RC-025a. Matches EM-016 atomic-sequence shape.
- **RC-025a:** 7 steps live in one verdict-executor goroutine. Spec explicitly says "a deterministic Go subroutine, NOT a workflow node." Step 4's per-verdict dispatch is a switch-statement-with-short-method-calls, not a multi-subsystem handoff. Matches PL-005 collapse posture.
- **Distinct from ON-027 (split):** ON-027's 8 steps each invoke a DIFFERENT subsystem owner. RC-018 + RC-025a's steps are method-calls within one function body.
- **Confirms F-pilot-ON-5 by complementary worked example.** ON-027 = delegating-orchestrator (split); RC-018 + RC-025a = cohesive-function-body (collapse). The pair codifies the SHAPE-not-COUNT distinction.
- **PL precedent (PL-005, PL-006, PL-011, PL-027) all collapsed via F8b** — RC matches PL exactly. **EM-016 atomic sequence collapsed via F8b** — RC-018's kill+emit+emit shape mirrors EM-016's writeTree+commitTree+updateRef. Three-pilot precedent (PL collapse + EM-016 collapse + RC two collapses) versus one-pilot counter (ON-027 split) sets a clear F8b worked-example pair for the v0.10 discipline patch.

**Lane: class (RESOLVED-CONFIRMATION). Severity: confirmation, not patch-blocker.**

The pilot's framing of F-pilot-RC-5 as "v0.10 candidate F8b worked-example pair anchor" is correct. r1 reviewer accepts the collapses as canonical-precedent; the discipline-patch (v0.10 §2.2 worked-example pair documenting both shapes) is a separate scope item, not a finding here.

---

## 3. F-pilot-RC-4 explicit verdict (per-category §8 split)

**Verdict: (a) yes — per-category split is correct; this IS the canonical large-§8 case.**

Per-category independence verification (the 11 categories tested for SHAPE-not-COUNT):
- **Cat 0 (infrastructure)**: detector reads `br --version` exit code + filesystem probes. Codepath: pre-classification halt + degraded-state transition.
- **Cat 1 (idempotent rerun)**: detector reads node's `idempotency_class` attribute. Codepath: spawn-the-node.
- **Cat 2 (non-idempotent in-flight)**: detector reads `idempotency_class` + bead status + JSONL liveness probe. Codepath: investigator dispatch + playbook.
- **Cat 3 (generic store mismatch)**: detector compares git/Beads/JSONL records + falls through Cat 3 sub-categories. Codepath: investigator dispatch.
- **Cat 3a (torn Beads write)**: detector reads adapter intent file + Beads status. Codepath: BI-031 status-check-before-reissue protocol.
- **Cat 3b (verdict-unexecuted)**: detector walks investigator branch for verdict-emitted + verdict-executed commits. Codepath: RC-026 staleness-checked re-execution.
- **Cat 3c (inverse premature close)**: detector reads merge commit on target branch + bead status. Codepath: idempotency-keyed adapter close-write.
- **Cat 4 (recoverable retry/backoff)**: detector reads agent's last retry/backoff state. Codepath: re-arm timer / re-present gate.
- **Cat 5 (clean restart)**: detector observes nothing in-flight. Codepath: no-op proceed-to-ready.
- **Cat 6a (integrity, LLM-triageable)**: detector observes structural-data violations. Codepath: investigator dispatch with `escalate-to-human` default.
- **Cat 6b (integrity, mechanically unrecoverable)**: detector observes JSONL corrupt / git ODB corrupt. Codepath: auto-escalate without investigator.

**11 distinct detector functions + 11 distinct dispatch paths + 11 distinct emitted events + 4 distinct response shapes (halt-degraded, auto-resume, investigator-dispatch, auto-escalate).** This is NOT a sentinel-vs-sentinel-set vocabulary (BI's 5 BrError values, WM's 12 typed errors, ON's 24 exit codes). Each category IS independent code/work. The per-category split is the only correct decomposition.

**Direction check (§2.11(c.2)):** Verified — 26 consumer→`rc-error.*` edges (3 to umbrella for `rc-003a` priority order property + `rc-007/009` framing locks; 23 to per-category beads for specific dispatch). Umbrella's only direct predecessor is `rc-schema.reconciliation-category` (the ENUM bead). **No reverse edges (`rc-error.* → §4 req`) emitted.** The §2.11(c.2) anti-pattern (taxonomy-as-slot-rule) is NOT present. **Clean.**

**Lane: class (RESOLVED-CONFIRMATION). Severity: confirmation; NOT a discipline patch.**

The pilot's claim that "F-pilot-RC-4 RESOLVES F-pilot-WM-2 SHAPE-not-COUNT class-lane question" is correct. WM applied SHAPE-not-COUNT to COLLAPSE 12 typed-error sentinels; RC applies SHAPE-not-COUNT to SPLIT 11 categories. The shape distinction (independent codepath vs sentinel value of single vocabulary) is the determinative axis. The two pilots together demonstrate both poles.

---

## 4. Cross-cutting findings

### 4.1 §2.11(c.2) edge direction sanity (the v0.9 anti-pattern check)

The discipline v0.9 added §2.11(c.2) precisely to prevent the `<spec>-error.taxonomy → <req>` anti-pattern that surfaced 3× across pilots (EM r1 #15, EV r1 #15, HC r1 LOAD-time). RC is the FIRST canonical-large-§8 pilot to apply v0.9 §2.11(c.2). **Verified clean:**
- All 26 consumer→taxonomy edges flow consumer→owner (`rc-NNN → rc-error.*`).
- The umbrella's only predecessor is `rc-schema.reconciliation-category`, the ENUM bead — schema-shaped, identical to a record-shape cite per §2.6.
- No `rc-error.* → rc-NNN` edges exist (would be the anti-pattern).
- Per-category beads have predecessor edges to non-RC beads only when the category cites a SPECIFIC schema/req from another spec (e.g., `rc-error.cat-3a → bi-029/bi-031/bi-031b/bi-032` for adapter intent-file detection; `rc-error.cat-0 → pl-010` for degraded-state co-ownership; `rc-error.cat-N → ev-events.*` for emitted events). All these are consumer→owner (the per-category bead consumes the BI/PL/EV declaration to detect or emit). **All edge directions correct.**

**RC is the canonical clean-direction example for v0.9 §2.11(c.2).** Lane: class (RESOLVED-CONFIRMATION).

### 4.2 §2.3 coalesce + §2.1a + §2.2 mechanical applications (Q2)

Pilot doc §F-pilot-RC-2 enumerates 12 §2.3 coalesce candidates and rejects all 12 with cited test-failures. Spot-checked 4 candidates:
- **(a) RC-001+004+005+006:** RC-004's S01 packaging contract (DOT + YAML + prompt templates) is independently testable from RC-001's reconciliation-as-workflow framing. **Reject correctly.**
- **(c) RC-003+003a+003b:** RC-003a's first-match priority order is testable independently from RC-003's bounded-recursion claim; RC-003b's Workflow-Class trailer carve-out is a discrete codepath. **Reject correctly.**
- **(i) RC-017+018:** declared-default-values vs SIGTERM/SIGKILL/emit/fallback 5-step sequence — distinct codepaths. **Reject correctly.**
- **(j) RC-020+021+022+022a:** RC-022a's outcome-envelope routing is the HC-handoff layer; RC-022's commit-atomicity is the daemon-side commit obligation. **Reject correctly.**

§2.1a: pilot rejects all candidates correctly. RC-009 was the closest call (one-sentence pointer + amendment protocol cite), but contains substantive lock-in normative content ("authoring agents MUST NOT re-open"). **Reject correctly.**

§2.2 splits: pilot mints 0 splits. The two candidates that pass the §2.2 syntactic 3-signal test (RC-018, RC-025a) both correctly fall through to F8b cohesive-function-body collapse per F-pilot-RC-5 verdict above.

**Lane: local. Verdict: clean — no over-split, no missing-coalesce.**

### 4.3 Bead count + tally

Pilot tally:
- 1 spec-parent + 43 §4 reqs + 2 sensors + 15 schemas + 12 taxonomy + 7 test-infra = **80 beads**.
- Direct count of yaml `mnem:` entries ratifies this (verified).
- 79 / 43 = 1.84× multiplier — highest in corpus. Driven by 15-schema + 12-taxonomy.

**Tally is consistent.** Lane: local; severity: clean.

### 4.4 Minor discrepancies (recap)

The following are documentation-tightening lane (local; non-blocking):

1. `rc-schema.workflow-class-extension` has `schema_kind: record` + `kind:enum` label. Recommend resolve to `schema_kind: enum` (the WorkflowClass ENUM is the load-bearing declaration; the record-extension is one optional field and is documentary-only at MVH per OQ-RC-002).
2. Pilot prose §6 enumeration listing claims `rc-013 → umbrella`, `rc-022a/rc-025/rc-025a → umbrella`, `rc-inv-001 → umbrella` consumer edges. The yaml does NOT emit these — each emits to a more-specific schema or per-category bead instead. The yaml is correct; the prose is loose. Recommend tightening the §6 enumeration in pilot doc to match yaml.
3. Pilot doc table cell for rc-018 (line 104) lists `rc-025a` and `rc-error.cat-3b` as direct blocks edges; yaml emits via `rc-026a` (transitive Cat 3b reach) and omits direct rc-025a (supporting-cite per F-pilot-AR-10). The yaml is correct; the prose is loose.

None of these block r1 or change the bead structure.

---

## 5. Summary

**Severity-and-lane breakdown:**
- 0 BLOCKING findings.
- 0 MAJOR findings.
- 3 MINOR / DOC-TIGHTENING findings (all local lane): the workflow-class-extension `schema_kind` label inconsistency; loose pilot-prose enumerations of consumer→umbrella edges; loose pilot-prose listing of rc-018's transitive blocks edges.
- 2 RESOLVED-CONFIRMATION class-lane findings: **F-pilot-RC-4 (canonical large-§8 split — correct)** and **F-pilot-RC-5 (cohesive-function-body collapse for RC-018 + RC-025a — correct)**.

**The bead set is structurally faithful to the spec.** Every sampled bead's description traces back to its §N source with paraphrase tolerance only. Edge directions are correct per §2.11(c.2) (RC is the canonical clean-direction case for v0.9). The per-category §8 split is mechanically correct for RC's 11 independent codepaths. The F8b cohesive-function-body collapse for RC-018 + RC-025a is correct under the SHAPE-not-COUNT principle.

**CLEAN summary (with 3 minor doc-tightening notes for the synthesizer to optionally bundle).**

---

## 6. Reviewer notes (for the synthesizer)

- F-pilot-RC-4 and F-pilot-RC-5 are RESOLVED-CONFIRMATION class-lane findings, NOT open discipline-patch candidates. The v0.10 F8b worked-example pair documenting cohesive-function-body vs delegating-orchestrator IS a separate scope item but does not rest on this review's verdict; r1 confirms the precedent is set.
- The 3 minor doc-tightening items can be bundled into a single pilot-prose patch (rc-pilot.md only; no yaml changes; no spec changes; no discipline changes). If the synthesizer prefers, they can also be deferred to v0.10 when the pilot is re-touched.
- RC is the LAST pilot in the corpus. RC's mnem-map publication closes the corpus-wide forward-deferred backfill cycle. The decomposition-quality lens is therefore terminal: there are no future pilots to recheck, only the corpus-wide load-gate.
