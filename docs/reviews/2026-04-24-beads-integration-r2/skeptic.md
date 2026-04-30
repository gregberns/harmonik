# Round 2 Skeptic Review — beads-integration.md v0.3.0

**Spec under review:** `/Users/gb/github/harmonik/specs/beads-integration.md` (v0.3.0, 696 lines, 2026-04-24, status `draft`)
**Reviewer role:** Skeptic (R2 round)
**Scope:** audit R1 integration claims, pressure-test new §4.8a CLI surface, hunt fabricated citations, check status-mapping table against EM/HC vocabularies, verify BI-031 reframe actually closes the idempotency hole, check new divergence_kind values against EV's closed enum, surface over/under-specification in the inserted requirements.

---

## 1. Verdict summary

**Changes required.** The v0.3 integration moved the spec in the right direction on several fronts: the §4.8a CLI surface contract is now concrete enough to write an adapter against, the §4.10 recovery protocol no longer relies on "Beads's own idempotency," the status-mapping table closes the `deferred`/`tombstone` hole, and invariants carry sensors. But the integration introduced **six new failure modes** of its own, most severe of which is **fabricated event payloads**: BI-025a, BI-031, BI-031b, and BI-024a emit events and enum values that do not exist in event-model.md v0.3.0. The reframe of BI-031 to "status-check-before-reissue" is incomplete — BI-032 and RC Cat 3a §8.4a still assume the idempotency-keyed Beads audit-log query the reframe claims to eliminate, so BI is now internally and cross-spec inconsistent on whether the recovery is Beads-idempotency-independent. The status-mapping table uses a failure-class vocabulary (`transient`, `recoverable`) that is not EM's taxonomy.

Load-bearing findings:

1. **Three invented `divergence_kind` enum values** (`br_unrecognized_exit_code`, `beads_status_unexpected`, `beads_schema_drift`) in BI-025a, BI-031 step 5, and BI-031b — EV §8.9 declares `divergence_kind` as a closed six-value enum; adding three new values requires a coordinated EV amendment that is neither filed nor acknowledged (lines 310, 374, 383).
2. **One fabricated event name** — `bead_terminal_transition_recovered` at BI-031 step 3 (line 372). No such event exists in EV §8.
3. **BI-031 still depends on the `--idempotency-key` Beads argument** in step 4 ("passed as `--idempotency-key` if the pinned Beads CLI supports it; otherwise as a positional metadata argument per the adapter's pinned-version contract"), contradicting the rationale paragraph's claim that the recovery is "Beads-idempotency-independent." The reissue path still assumes Beads accepts AND honors the key. If Beads does not honor it, step 4 double-writes.
4. **BI-031 reframe contradicts RC Cat 3a §8.4a detection rule**, which still reads: "the Beads audit log at restart time either shows no corresponding entry OR shows an entry matching the idempotency key." RC was not updated. BI-032 also still says "The intent log and Beads's audit log MUST be the evidence sources consumed by the Cat 3a torn-Beads-write detector" — keeping the dependency the reframe claims to eliminate.
5. **BI-024a cites the wrong PL step.** The `br --version` handshake is claimed to run "during PL-005 step 6, the Beads availability check." PL §4.2 step 6 is not a version handshake; it is the `br ready` query plus the BI-013/BI-016 reads. The `br --version` handshake runs in RC-012's Cat 0 pre-check, which corresponds to **PL-005 step 4** (not step 6). See process-lifecycle.md line 219 vs 221.
6. **BI-010a uses a failure-class vocabulary EM does not declare.** Row 3 cites `failure_class ∈ {transient, recoverable}` and row 4 cites `{structural, compilation_loop}`. EM §8 declares six classes: `{transient, structural, deterministic, canceled, budget_exhausted, compilation_loop}` — `recoverable` is not an EM class, and `deterministic` / `canceled` / `budget_exhausted` are missing from the table entirely. `ErrCanceled` is punted to OQ-BI-004 (acceptable); `ErrBudget` and deterministic failures are silently dropped.

Less severe but worth flagging: `divergence_kind` string values use hyphens in EV (e.g., `br_missing`, `br_version_incompatible`) but BI-024a emits `failure_mode="br-version-incompatible"` with a hyphen rather than an underscore; the enum value does not exist in `daemon_startup_failed.failure_mode` in the first place (EV §8.7.4 says `failure_mode` is "per [operator-nfr.md §8]" exit-code taxonomy); §4.9 cite in BI-025c points at ON's observability envelope (not operator-tunables, which are in §4.1 ON-004); BI-INV-004's sensor language remains vague.

The spec is not far from reviewable. Five targeted corrections — (a) coordinate the three new `divergence_kind` values with EV or rewrite to use existing values, (b) kill the fabricated `bead_terminal_transition_recovered` event or register it in EV, (c) resolve the BI-031 / BI-032 / RC Cat 3a internal inconsistency in one direction, (d) correct the PL step citation in BI-024a, (e) align BI-010a's failure-class vocabulary with EM §8 — close the worst of the blocking gaps. The §4.8a surface is otherwise sound and the reverse-drift map is well-formed.

---

## 2. Integration-fix audit — walking every R1 claim

The v0.3 revision-history row (line 665) lists eleven R1-integration items. Walking each against what v0.3.0 actually delivers:

### 2.1 Status-mapping table (BI-010a) — PARTIAL

Claim: "closed deferred/tombstone hole." Table lands at lines 160–168.

Works: `deferred` and `tombstone` are correctly ruled non-harmonik-written (line 170); the in-flight-run-to-tombstone mid-run rule at line 173 is concrete and cites RC §8.4 Cat 3 correctly.

Gaps:
- **EM failure-class vocabulary mismatch.** Rows 3 and 4 split failures into `{transient, recoverable}` vs `{structural, compilation_loop}`. EM §8 (execution-model.md line 36) declares `{transient, structural, deterministic, canceled, budget_exhausted, compilation_loop}`. `recoverable` is not in EM. `deterministic` and `budget_exhausted` have no row. `canceled` is punted to OQ-BI-004. The reader cannot classify a run that fails with `failure_class=deterministic` or `failure_class=budget_exhausted` against this table.
- **Row 3 trigger condition is under-specified.** "`run_failed` with `failure_class ∈ {transient, recoverable}` AND no in-run retry available". "No in-run retry available" is not a defined harmonik concept; EM §8.1 routes retry via node-level attempt caps per §4.10 EM-049 (`run_failed` fires after retry-cap exhaustion). If the retry cap is exhausted, does the failure reclassify to `structural` per EM line 323 ("on cap exhaustion the class reclassifies to `structural`"), and if so, does this row ever fire? Ambiguous.
- **Ordering with merge.** Row 2 says `run_completed` AND merge-completed per WM-007. But WM-007 is in §4.2 (branching model), not the merge event — the citation should be WM-021 (in §4.5 Merge back to integration; workspace-model.md line 420), which emits `workspace_merge_status{status=merged}`. The current cite resolves to the three-level branching rule, which does not carry the "merge completed" semantics being relied on.
- **Edge: run fails AFTER merge has landed.** The skeptic prompt flagged this case. The table does not handle it. If `workspace_merge_status{status=merged}` has fired but before the daemon closes the bead the run also emits `run_failed` (e.g., a post-merge hook fails), which row applies? Row 2's AND-gate evaluates false (run_failed, not run_completed), but Cat 3c (reconciliation/spec.md §8.6) would then fire and auto-close. This ordering race is unaddressed; BI-010b declares a carve-out for reconciliation-driven writes but does not declare the daemon's responsibility in this window.
- **Edge: merge in progress when failure fires.** WM emits `workspace_merge_status{status=pending}` on entry to merge-pending, and `{status=merged}` on success. Between the two, a merge conflict or merge-node handler failure produces `merge_conflict_escalation` (WM-023). This is neither row 2 (merge not completed) nor row 3 (failure class not obviously `transient`/`recoverable`). The table does not cover the transition from merge-pending to failure.

### 2.2 Reconciliation-driven writes (BI-010b) — PARTIAL

Claim: "carve-out for reconciliation-driven writes."

Works: The carve-out exists (line 179–181); reconciliation writes route through the adapter and idempotency key; BI-INV-001 reshape correctly cites both BI-010a and BI-010b.

Gaps:
- **Cat 3b is included in the carve-out but Cat 3b is "re-execute the verdict," not a Beads write per se.** RC §8.5 (line 148): "Auto-resolve via RC-026 re-execution: the daemon performs the staleness check (RC-024); if stale, re-dispatches fresh reconciliation; if not stale, executes the verdict per RC-025 and writes the verdict-executed commit." The verdict itself MAY or MAY NOT produce a Beads write (e.g., `reset-to-checkpoint` writes no Beads; `reopen-bead` does). Saying "Cat 3b ... MAY fire close or reopen" is too strong — Cat 3b fires only whatever the underlying verdict's action is.
- **Cited event is too coarse.** Line 181 cites `reconciliation_verdict_executed` per [event-model.md §8.6]. EV §8.6.4 (event-model.md line 148) is the specific row; §8.6 is the whole group. Should be §8.6.4.
- **Reconciliation-executed `claim`.** The carve-out allows `close` or `reopen`. It does not allow `claim`. Is that intentional? If reconciliation's investigator verdict executes a `resume-with-context` against a bead that has been reopened (`open` status), does the subsequent dispatch require a fresh claim write? Unclear whether that falls under BI-010a row 1 (daemon dispatch loop) or under BI-010b (reconciliation-driven). The rule is silent.

### 2.3 BI-031 reframe — INCOMPLETE, INTERNALLY INCONSISTENT

Claim: "Beads-idempotency-independent status-check-before-reissue."

Works (in isolation): The protocol at steps 1–3 and 5 is coherent. Step 3's "if current status equals intended_post_state, prior write landed" is a valid inference.

Gaps:
- **Step 4 still depends on `--idempotency-key`.** Line 373: "re-issue the `br` write with the same `idempotency_key` (passed as `--idempotency-key` if the pinned Beads CLI supports it; otherwise as a positional metadata argument per the adapter's pinned-version contract)." If the pinned Beads version does not honor the key, the reissue is a plain write, and a race between step 2 (reads pre-state) and step 4 (reissue) where a concurrent caller flips the status produces a **double write** on the second caller's attempted claim. The "Beads-idempotency-independent" claim in the rationale paragraph (line 376) is true only of step 3's success-path detection; it is false of step 4's reissue correctness.
- **Race handling at step 4.** Between step 2's read and step 4's reissue, another caller (e.g., a Cat 3c auto-resolver in reconciliation, or an operator-driven `br` from outside harmonik) may flip the status to post-state. Then step 4's reissue will produce `BrConflict` (from the exit-code taxonomy at §6.1a). The spec does not declare whether `BrConflict` at step 4 is a success (someone else did the work) or a failure (retry). Silent.
- **BI-032 retains the audit-log dependency.** Line 389: "The intent log (§4.10.BI-030) **and Beads's audit log** MUST be the evidence sources consumed by the Cat 3a torn-Beads-write detector per [reconciliation/spec.md §4.3]." This is precisely the dependency BI-031 claims to eliminate. If BI-031's status-check approach is the new model, BI-032 should read "The intent log and Beads's coarse-status query MUST be the evidence sources…" The reframe is partial — it touches BI-031 but not BI-032.
- **RC Cat 3a §8.4a was not updated.** reconciliation/spec.md line 138 still reads: "the Beads audit log at restart time either shows no corresponding entry OR shows an entry matching the idempotency key." Line 140: "read the audit-log entry to determine whether the write landed". RC's detection rule still requires an idempotency-keyed audit-log query; BI's recovery protocol does not. **BI and RC are now inconsistent on how Cat 3a is detected vs resolved.**

### 2.4 §4.8a `br` surface contract — MOSTLY SOUND

Claim: "BI-024a `br --version` handshake, BI-025a exit-code taxonomy, BI-025b mandatory `--format json`, BI-025c subprocess timeout, BI-025d stderr capture."

Works: The five sub-requirements are the right shape; the BI-025a `BrError` enum and the §6.1a mapping table are concrete; BI-025c's 5s/10s defaults match PL-005 step 6's existing 5s constraint.

Gaps — see §3 hidden assumptions and §5 over-specification.

### 2.5 §6.1a BrError taxonomy — WORKS

The enum and the mapping table (lines 465–480) are concrete and internally consistent. One ambiguity: "(exec error)" mapping (`br` not on PATH / fork failure) is declared to produce `Unavailable`. This is correct; but line 474's "| `br` exit code | `BrError` | Meaning |" leaves `(exec error)` as a non-integer entry in the code column, which is a cosmetic issue only.

### 2.6 §8 adapter error+failure taxonomy — WORKS

The §8 table (lines 536–544) cleanly maps every `BrError` to a reconciliation category. `NotFound → Cat 3` is reasonable; `Conflict → Cat 3a` is correct; `DbLocked / Unavailable → Cat 0` is correct; `SchemaMismatch → Cat 0 → daemon startup failure` is honest.

Gaps:
- `NotFound` routed to "Cat 3 generic" is correct but the scenario is worth naming: a bead-id that was valid at claim-time is no longer present in Beads at close-time. This is distinct from a write-race. Consider a separate row or a note.
- Routing for `Other` is "Cat 3 (generic)" plus "divergence-detected; investigator dispatch." But BI-025a says unknown exit codes emit `store_divergence_detected{divergence_kind="br_unrecognized_exit_code"}` — see §3 below; that emission payload is fabricated.

### 2.7 BI-031 reframed — see §2.3 above — INCOMPLETE

### 2.8 BI-031b `br show` JSON-consistency — FABRICATED ENUM

Claim: parse failure emits `store_divergence_detected{divergence_kind="beads_schema_drift"}`.

Gap: `beads_schema_drift` is not in EV's `divergence_kind` enum (event-model.md line 748). Fabricated.

### 2.9 AR-042 sensors on all 4 invariants — PARTIAL

Every invariant now carries a `Sensor:` line. Structurally compliant with AR-042. Quality-of-sensor:
- BI-INV-001: three sensors (reviewer-persona corpus scan + contract test + cross-spec scenario test). Strong.
- BI-INV-002: two sensors (cross-spec test + reviewer-persona scan). Adequate.
- BI-INV-003: one sensor (RC-013 detector + §10.2 BI-021..BI-023 scenario test). Adequate.
- BI-INV-004: three sensors (RC-014 / §8.4a + adapter unit tests + cross-spec out-of-band write scenario). Adequate but "out-of-band `br` write" is itself under-specified as a test fixture — it would require a test harness that spawns an adversarial `br` process bypassing the adapter. Flag as test-infrastructure risk, not an AR-042 miss.

### 2.10 BI-INV-004 reshape — WORKS WITH ONE WRINKLE

The v0.2 form was "every Beads status write carries key + intent" (single-subsystem). The v0.3 reshape (line 418) is "Every Beads status-change commit observed downstream MUST be verifiable via the conjunction of (a) an idempotency-keyed intent log entry (or its post-success absence), and (b) Beads's recorded transition." The cross-subsystem span: BI writes the intent log, RC's Cat 3a detector reads it, BI's adapter verifies the conjunction. That spans two subsystems (BI + RC) through EV (divergence event).

Wrinkle: the sensor cites BI-029/BI-030 adapter unit tests AND a cross-spec out-of-band-write scenario. The scenario test's emission target is Cat 3a per RC — which still relies on the audit-log-by-idempotency-key query (§2.3 above). If RC-014 / RC §8.4a does not in practice catch an out-of-band write (because Beads has no such audit query), the sensor fails to fire. The reshape is semantically correct; the sensor's realizability depends on the audit-log contract being true. This is the same hidden assumption flagged in §3 below.

### 2.11 BI-008a bead-ID scoping — WORKS

Line 130–131 is the right shape; opacity + project-scoping + OQ-BI-005 carve-out for post-MVH cross-project. Clean.

### 2.12 §A.4 reverse-drift map — WORKS

The table at lines 685–694 is well-formed; the mapping of legacy §10.N to current §4.N is accurate; the note at line 696 correctly cites WM §A.4 precedent.

### 2.13 New OQs — WORK

OQ-BI-004 through OQ-BI-007 are correctly shaped. OQ-BI-004 (operator-cancel / ErrCanceled) honestly defers the status-mapping row (§2.1 above) that the R1 critic flagged. OQ-BI-005 (cross-project scoping) correctly defers. OQ-BI-006 and OQ-BI-007 are appropriately post-MVH.

### 2.14 MUST/SHOULD cleanups — WORKS

BI-005 (cache-as-authoritative MUST tightening), BI-021 (dropped "within its domain"), BI-024 ("MUST be accompanied" for backwards-incompatible) are all applied. Clean.

---

## 3. Hidden assumptions

Eight assumptions v0.3 makes that could turn out false:

1. **Beads accepts `--idempotency-key` on status-change commands.** BI-031 step 4 says "passed as `--idempotency-key` if the pinned Beads CLI supports it; otherwise as a positional metadata argument per the adapter's pinned-version contract." This is a conditional surface contract. The Beads SQLite fork at Dicklesworthstone/beads_rust (line 74) has not been verified to expose this flag; the spec defers the test to "the pinned-version contract" which is itself a forward reference. Crash-adversary role is the natural owner of this verification, but the spec should name the fallback behavior explicitly: if Beads does not honor the key, what does the adapter do? Silently lose idempotency? Route every retry to reconciliation Cat 3a? Both answers have consequences.

2. **Beads's JSON output mode on `br show`.** BI-025b mandates `--format json` on every command. BI-031b's status-check protocol requires `br show <bead_id> --format json` to return structured output. If the pinned Beads version has JSON output on `br ready`, `br claim`, `br close`, and `br reopen` but not on `br show` (or vice-versa), BI-031 fails at step 2 before it begins. The spec fences "commands lacking a JSON output mode" (line 317) but does not enumerate which commands are known to support JSON at the pinned version. This is the `br` CLI surface verification that R1 implementer review called "the BLOCKER" — still not resolved.

3. **Pinned Beads version produces stable JSON across patch releases.** BI-024a's version regex `br\s+(\d+)\.(\d+)\.(\d+)(?:[-.][a-zA-Z0-9]+)?` admits any patch release within the compatibility window; BI-025b's JSON parsing requires the JSON shape to be stable across those patches. BI-026's "absorb breakage" contract admits this. But patch releases of pre-1.0 SQLite-fork upstream may ship JSON-shape tweaks; the spec does not declare whether JSON-shape is a MINOR (breaks compat) or PATCH (safe) release boundary at upstream. Pre-1.0 version semantics are unreliable by definition.

4. **Beads's SQLite is local-only, single-writer.** BI-009 (atomic claim) relies on SQLite's single-writer semantics. The spec does not check what happens if an operator uses NFS-mounted `.harmonik/` (fcntl locks are SQLite-hazardous on NFS). Silently assumed out-of-scope; should state it.

5. **Status-check-before-reissue is race-free.** BI-031's rationale paragraph says "Any race in which Beads completes the write between step 2 and step 4 is observed by reconciliation as the same divergence pattern (intent log present, post-state reached) and routes safely." But step 3 already filtered the post-state case. A race between step 2 (reads pre-state) and step 4 (reissue) in which another caller lands the transition first causes step 4 to produce `BrConflict` (per §6.1a). The spec does not route BrConflict at step 4 to success — which it should if the post-state is reached — but to Cat 3a per §8, which may re-enter the same recovery protocol. Infinite-loop risk if the race is persistent. The routing should cite "if BrConflict observed AND status-check subsequently reads post-state, succeed silently."

6. **`br show` is cheap enough for every crash-restart intent.** BI-031 executes `br show` once per stale intent file. A daemon that crashed with N pending intents pays N `br` fork+exec on startup. If N is large (thousands), this is slow. No budget is declared. Not MVH-blocking but observable at scale.

7. **The intent-log entry carries `intended_post_state`.** BI-031 step 1 reads "`op`, `bead_id`, `idempotency_key`, `intended_post_state`" from the intent file. But §6.1 `IntentLogEntry` (lines 485–493) declares fields `idempotency_key`, `run_id`, `transition_id`, `op`, `bead_id`, `requested_at`, `schema_version` — **no `intended_post_state` field.** `intended_post_state` is derivable from `op` (`claim → in_progress`, `close → closed`, `reopen → open`), but the schema does not carry it. Either step 1's read is deriving it from `op` (in which case the prose is misleading — it's not a stored field), or the schema needs an `intended_post_state` field. Inconsistency between §4.10 BI-031 prose and §6.1 IntentLogEntry schema.

8. **Beads audit log query by idempotency key.** BI-032 line 389 names "Beads's audit log" as one of the two evidence sources for Cat 3a detection. RC §8.4a detection rule reads the audit log by the idempotency-key match. Neither BI nor RC has filed an OQ against Beads that declares this query shape is a requirement of the external tool. If Beads's audit log is timestamp-indexed only (not key-indexed), the detector's query is a full-scan on every Cat 3a detection — expensive and probabilistic at scale. BI-031's reframe aimed to eliminate this dependency at the adapter level but left it at the detector level.

---

## 4. R1 regressions

Places where the integration either introduced a new defect or failed to repair a known one:

1. **PL-005 step citation error.** BI-024a line 303: "The adapter MUST invoke `br --version` at daemon startup (during PL-005 step 6, the Beads availability check)". PL-005 step 6 is not the availability check; it is `br ready` query + BI-013/BI-016 reads (process-lifecycle.md line 221). The `br --version` check is in RC-012 (reconciliation/spec.md line 356), which runs as PL-005 **step 4** (Cat 0 pre-check, per PL-005 line 219 and PL-006 line 233's phrasing "Before the daemon executes the Cat 0 pre-check (§PL-005 step 4)"). BI-024a's citation is a factual misreading of PL-005 introduced in v0.3.

2. **ON §4.9 citation error.** BI-025c line 324: "operator-tunable per [operator-nfr.md §4.9]". ON §4.9 is "Observability envelope" (ON-034 / ON-035 / ON-036 / ON-037 / ON-038 / ON-039 / ON-040). Operator-tunables for timeouts live in ON §4.1 (ON-004 config inventory obligation) per operator-nfr.md line 156 which enumerates "timer-flush cadence, budget warning threshold, drain timeout, RTO thresholds, queue-empty re-query cadence, Cat 0 pre-check retry cadence, per-Cat reconciliation budgets." The BI-025c timeouts should be added to that list and the citation should resolve to §4.1 ON-004.

3. **EV event fabrication (§2.8, 2.9 above).** Three `divergence_kind` values and one event name invented in v0.3 without EV coordination. Regression from v0.2 which had no such fabrications.

4. **`daemon_startup_failed.failure_mode` payload value fabrication.** BI-024a line 303: `daemon_startup_failed{failure_mode="br-version-incompatible"}`. EV §8.7.4 row (event-model.md line 163) says `failure_mode` is "per [operator-nfr.md §8]" — which is ON's exit-code taxonomy. ON §8 (operator-nfr.md line 668) has exit code 8 as `beads-unavailable`, not `br-version-incompatible`. Either BI-024a should emit `failure_mode="beads-unavailable"` (the registered code-8 value), or an OQ should be filed against ON §8 to add a `br-version-incompatible` row. As written, the payload value doesn't exist. Related: the hyphen/underscore drift — EV's `infrastructure_unavailable.failed_prerequisite` (event-model.md line 775) uses `br_version_incompatible` with underscore; BI-024a uses `br-version-incompatible` with hyphen. Consistency error.

5. **Rationale contradiction in BI-031.** v0.3 added the status-check-before-reissue protocol AND retained BI-032's dependency on the Beads audit log. The intent-log-and-audit-log rationale paragraph in §A.3 (line 675) still reads: "Beads's own audit log captures completion. Their conjunction lets the Cat 3a detector decide whether to re-issue or declare done without an investigator." This is now contradictory with BI-031's claim of Beads-idempotency-independence. The rationale was not updated.

6. **BI-010a failure-class vocabulary (§2.1 above).** `recoverable` does not exist in EM §8; three classes silently missing.

7. **EM citation drift.** Line 181: "`reconciliation_verdict_executed` per [event-model.md §8.6]" — should be §8.6.4. Line 372: "`bead_terminal_transition_recovered{...}` per [event-model.md §8.6]" — and the event doesn't exist regardless. Line 374: "`store_divergence_detected{divergence_kind="beads_status_unexpected"}` per [event-model.md §8.6]" — divergence_kind value fabricated. Three EV-pointer citations in v0.3 are too coarse (naming §8.6 group instead of a specific row); two point at events or values that don't exist.

---

## 5. Over-specification vs under-specification

### 5.1 Over-specified

- **BI-025c 5s/10s defaults.** Pinning read timeout to 5s and write timeout to 10s is prescriptive for MVH; operators may legitimately need a longer read timeout on a big audit-log query or a shorter write timeout on a hot path. Better: declare that read and write have independent tunables with the 5s/10s serving as default, and cite ON-004's config inventory as the tunable location. The current phrasing does all of this, except the ON cite points to §4.9 (observability) not §4.1 (config inventory).
- **BI-024a version regex.** The regex `br\s+(\d+)\.(\d+)\.(\d+)(?:[-.][a-zA-Z0-9]+)?` pins a version-string grammar at the spec level. If Beads changes its `--version` output format (e.g., `br version 0.8.0` → `beads-rust 0.8.0`), the regex becomes a structural version match that BI-024a requires to fail startup. This is tight; `br --version` output-format is not a contract Beads publishes. Consider softening to "the adapter MUST parse `br --version` into a three-component semantic version" without pinning the surrounding grammar.
- **BI-025d stderr capture "fully".** "capture `br` stderr fully on every invocation" — on a pathological `br` that streams unbounded stderr, "fully" is a memory risk. Bound it (e.g., "up to 64KB; truncate and tag beyond").

### 5.2 Under-specified

- **BI-031 step 4 race semantics.** See §3.5 and §4.5 above. The concurrent-writer case is silent.
- **BI-031 step 4 reissue flag fallback.** If pinned Beads does not accept `--idempotency-key`, the "positional metadata argument per the adapter's pinned-version contract" is a forward reference to a contract that doesn't exist. The fallback is not specified.
- **BI-024a version-mismatch fatal vs non-fatal.** "A version mismatch outside the compatibility window of BI-026, OR an unparseable output, MUST fail daemon startup with exit code 8." The compatibility window is declared by BI-026 ("remain pinned to the prior Beads version and delay upgrade"), which is directional (harmonik pins, Beads moves). The window's width (zero? N-1? N ± 1?) is not declared. See R1 critic's Challenge 6 (not resolved in v0.3).
- **Intent-log cleanup for abandoned runs.** R1 implementer flagged this. v0.3 does not add a TTL or GC rule. The operator-nfr informative note at line 351 ("Operator-nfr clean-install / cleanup protocols MUST preserve this directory") hardens against accidental deletion but does not introduce hygiene for long-dead keys. Inode leak over daemon lifetime.
- **`intended_post_state` field.** §6.1 IntentLogEntry schema (lines 485–493) does not carry the field BI-031 step 1 reads.
- **BI-010a row 3's "no in-run retry available."** Term not defined.
- **Edge: merge-pending to failure.** §2.1 above.
- **Edge: failure after merge landed.** §2.1 above.

---

## 6. Cross-spec promises — OQ realism

The R1 integration adds cross-spec coordination requests to RC, ON, EV, HC (v0.3 revision line 665). Audit of those requests:

1. **EV amendment for three new `divergence_kind` values and one new event.** NOT filed. The spec emits `store_divergence_detected{divergence_kind="br_unrecognized_exit_code"}` (BI-025a), `{divergence_kind="beads_status_unexpected"}` (BI-031 step 5), `{divergence_kind="beads_schema_drift"}` (BI-031b), and `bead_terminal_transition_recovered{...}` (BI-031 step 3), each as a first-class requirement. No OQ. EV §8.9 `divergence_kind` is a closed enum (event-model.md line 748); EV does not list `bead_terminal_transition_recovered` as an event type at all. **MUST be filed as an OQ or rewritten to use existing EV vocabulary.**

2. **ON coordination for the BI-025c timeout tunables.** NOT filed. ON §4.1 ON-004 owns the config inventory; BI-025c declares two new operator-tunable values (read timeout, write timeout) without declaring them against the inventory. Should be an OQ or an inline note.

3. **RC alignment of Cat 3a detection with BI-031 reframe.** NOT filed. BI-031 declares Beads-idempotency-independence; RC §8.4a still assumes idempotency-keyed audit-log query. Unaligned specs. **MUST be filed or BI-031 rolled back.**

4. **HC coordination for OQ-BI-007 (Beads-CLI skill capability partition).** Filed as OQ. HC is named as the owner (line 654). Acceptable.

5. **PL-005 step-6 citation.** Not a cross-spec amendment; it's a citation-correctness fix internal to BI. Flagged in §4 above.

6. **WM session-log sidecar `bead_id`.** R1 architect flagged the WM sidecar as not declaring `bead_id`. Rechecked: WM-026 (workspace-model.md line 486) explicitly declares "bead_id (present iff the run is bead-tied ...)" on the sidecar schema. The concern was resolved in WM v0.3; BI's BI-020 citation to WM §4.7 now resolves. No action needed.

---

## 7. Definitional drift

Terms used but not rigorously defined in v0.3:

- **"Terminal-transition write"** (glossary line 62) vs **"reconciliation-driven write"** (BI-010b). BI-INV-001 now names both categories but the glossary treats them as if they are the same shape. A reconciliation-driven write is not at a "workflow boundary" (the glossary's definition) — it's at a reconciliation-verdict-execution point. Either extend the glossary or declare that reconciliation-driven writes are a sub-category of terminal-transition-writes.
- **"Workflow boundary"** (glossary line 62). Unchanged from v0.2. Used once; not defined. The R1 critic flagged this — unaddressed in v0.3.
- **"Audit log"** in BI-032 (line 389) and §A.3 (line 675). Never defined. Presumably Beads's internal audit log (not harmonik's JSONL), but the term is unqualified. Contrast with "intent log" which is clearly defined in the glossary. Add a glossary entry or qualify every use-site as "Beads's audit log."
- **"Compatibility window"** (BI-024a, BI-026). Not bounded. R1 critic flagged — unaddressed.
- **"Intended post-state"** (BI-031 step 1). Treated as a read from the intent file, but no field of that name exists in the schema. Derivation from `op` is implicit.
- **"Status-check-before-reissue"** (BI-031 title and rationale). New term introduced in v0.3; not in the glossary. The phrase suggests the recovery is a single check-then-act, but the protocol has five steps. Consider renaming to "post-state-check recovery" or similar, and add a glossary entry.
- **"Post-state / pre-state"** (BI-031 steps 3–5). Used repeatedly; not defined. Could be confused with `pending`/`merged` in workspace events. A terse glossary or inline table mapping `op → pre-state → post-state` (`claim: open → in_progress`; `close: in_progress → closed`; `reopen: closed → open`) would resolve.
- **"Intent log"** vs **"beads-intents directory."** The glossary defines "intent log" as a single entry ("the on-disk record of a pending terminal-transition write at `.harmonik/beads-intents/<idempotency_key>.json`"). §4.10 and §A.3 use "intent log" to mean the directory-as-a-whole. Distinguish: a single file is an "intent entry"; the directory is the "intent log."

---

## 8. Template conformance

Running v0.3 against `docs/foundation/spec-template.md v1.1`:

- ✓ Front matter: complete; `spec-category: foundation-cross-cutting` added per AR-052; `depends-on` expanded to 9 peers per AR-022 and WM v0.3 precedent.
- ✓ `requirement-prefix: BI` in registry.
- ✓ `spec-shape: requirements-first`; `spec-template-version: 1.1`.
- ✓ Sections §1–§6, §8, §9, §10, §11, §12, §A all present. §7 absent (optional per template).
- ✓ Every requirement has `BI-NNN` heading and `Tags: mechanism`.
- ✓ Revision history has five rows; v0.3.0 row is thorough (perhaps overly long — ~70 lines of narrative; the template does not cap this).
- ✓ Total line count 696 — under 1000; no split.
- ✓ No TODO/TBD/FIXME.
- ✓ §A.4 reverse-drift map present.

Lint-level gaps:
- ✗ **Several cross-spec citations resolve to non-existent targets or wrong-granularity anchors**, newly introduced in v0.3:
  - Line 181 `[event-model.md §8.6]` → should be §8.6.4
  - Line 303 `PL-005 step 6` → should be step 4
  - Line 310 `{divergence_kind="br_unrecognized_exit_code"}` → enum value DNE in EV
  - Line 324 `[operator-nfr.md §4.9]` → should be §4.1 ON-004
  - Line 372 `bead_terminal_transition_recovered` → event DNE in EV
  - Line 374 `{divergence_kind="beads_status_unexpected"}` → enum value DNE
  - Line 383 `{divergence_kind="beads_schema_drift"}` → enum value DNE
- ✗ **§6.1 IntentLogEntry schema missing `intended_post_state`** (or the reference in BI-031 needs reframing).
- ✗ **BI-010a row 3 uses `failure_class` values `{transient, recoverable}`** that are not EM's taxonomy.

Reviewer-level gaps:
- Glossary entries for "audit log" and "pre/post state" missing.
- Rationale (§A.3) still asserts the pre-reframe "conjunction of intent log and audit log" model at line 675, not updated for BI-031's new protocol.

---

## 9. New failure modes surfaced by v0.3.0

Six issues introduced or newly sharpened in v0.3:

1. **Double-write on step-4 race (§3.5).** A concurrent writer between BI-031 steps 2 and 4 causes a spurious reissue; the spec says `BrConflict` but does not route the conflict back through the status-check success path.

2. **Infinite Cat 3a loop risk.** If step 4 emits `BrConflict` and BrConflict → Cat 3a per §8, the Cat 3a detector triggers adapter re-issue, which re-enters the same recovery protocol. The spec does not declare a bound on recovery iterations.

3. **EV-023a corroboration violation.** EV-023a (event-model.md line 425) requires every `store_divergence_detected` emission to carry `corroboration ∈ {git-corroborated, beads-corroborated}` with ≥2-store evidence. BI's new emissions (`br_unrecognized_exit_code`, `beads_status_unexpected`, `beads_schema_drift`) are single-source by construction (only the adapter observes them). They would fail EV-023a at emission time and should produce `divergence_inconclusive` (§8.6.10) instead — or the emissions should not be `store_divergence_detected` at all (they are adapter-internal surface failures, not store divergence). The category is wrong.

4. **Beads's silent upgrade during daemon uptime.** BI-024a runs at startup only. A long-running daemon whose operator upgrades Beads under it (`brew upgrade`, apt upgrade) has no in-flight detection. BI-024 says "silent upgrades are forbidden" but the mechanism is a one-shot startup check. Mitigation: BI-025a would classify subsequent unrecognized exit codes as `BrOther`, but that's reactive, not preventive. Flag as post-MVH or name the MVH mitigation.

5. **`intended_post_state` schema drift.** See §3.7 above. The BI-031 protocol reads a field that does not exist in §6.1 schema.

6. **Timeout-vs-Cat-0 race (BI-025c / RC-012).** BI-025c's 5s read timeout maps a `br` read that exceeds 5s to `BrUnavailable`. RC-012's Cat 0 pre-check has its own T=5s timeout on `br --version` and `br list --limit 1`. If a `br` invocation times out at BI-025c's level (5s), does it also register as RC-012's Cat 0 trigger? BI §8 routes `Unavailable → Cat 0` per the table, which suggests yes — but this creates a race between the adapter's timeout handling and the reconciliation detector's pre-check. Worth naming "the adapter's BrUnavailable emission during Cat 0 pre-check OR at any later operational point both route to RC-012 halt classification".

---

## 10. Affirmations

Parts of v0.3 I would not change:

1. **§4.8a CLI surface contract exists.** The single biggest R1 gap (the unspecified `br` surface) is now concrete. BI-024a / BI-025a / BI-025b / BI-025c / BI-025d cover version handshake, exit-code taxonomy, JSON-mode mandate, timeout discipline, stderr capture. An adapter can be written against this surface.

2. **§6.1a BrError taxonomy + §8 error-to-category map.** Clean, complete, internally consistent.

3. **§A.4 reverse-drift map.** Publishes the §10.N → §4.N migration for downstream specs. The map is accurate and follows the WM v0.3 precedent cited at line 696.

4. **BI-008a bead-ID scoping.** Clean opacity + project-scope + OQ-BI-005 deferral.

5. **Status-mapping table's `deferred`/`tombstone` handling.** The rows at lines 170–174 correctly treat these as operator-facing states harmonik does not write; the mid-run tombstone rule routes to Cat 3.

6. **BI-010b carve-out for reconciliation-driven writes + BI-INV-001 reshape.** The central-call structural integrity is preserved: intra-run writes remain forbidden; reconciliation writes are named as a distinct but adapter-routed category.

7. **`Axes:` hygiene across new requirements.** BI-024a, BI-025a–d, BI-031b, BI-010a, BI-010b all carry correct `Axes:` lines where external I/O or idempotency is at stake.

8. **OQ shape.** OQ-BI-004 through OQ-BI-007 are sharp and correctly scoped.

9. **Revision history discipline.** The v0.3 row is long but informative; the "citation re-grep verified: zero legacy-anchor matches in this spec body as of v0.3.0" claim at line 665 is verifiable (I checked — all legacy `§9.N` / `§5.N` / `§6.11` / `§7.N` / `§8.N` anchors are gone from the body).

---

## 11. Recommendation (ordered by severity)

**Blocking on R3.** Four corrections are required before advancing to `reviewed`:

**R1 — File EV amendment OR rewrite the three fabricated `divergence_kind` values and one fabricated event.** Options:
- **(a) File coordinated EV amendment.** Add `br_unrecognized_exit_code`, `beads_status_unexpected`, `beads_schema_drift` to EV §8.9 `divergence_kind` enum; add `bead_terminal_transition_recovered` to EV §8.6 taxonomy. Update BI §9.1 to coordinate with EV R3.
- **(b) Rewrite to existing values.** Use `beads_closed_no_commit` or `schema_mismatch` where applicable; drop the recovery event (use `reconciliation_verdict_executed` if the recovery is verdict-executed, else a log record).
- Either way, also address the EV-023a corroboration rule: single-source divergence evidence must emit `divergence_inconclusive`, not `store_divergence_detected`. The current BI emissions would fail EV-023a.

**R2 — Resolve BI-031 / BI-032 / RC §8.4a internal inconsistency.** Pick one model:
- **(a) Idempotency-independent recovery (adopted by BI-031).** Update BI-032 to name "coarse-status query" as the evidence source instead of "Beads's audit log." File cross-spec amendment against RC §8.4a to rewrite detection rule in terms of status+intent-log conjunction, not audit-log-by-key.
- **(b) Retain audit-log dependency.** Roll back BI-031's reframe; declare an adapter-level requirement that the pinned Beads version MUST expose `br audit --idempotency-key <k>` or equivalent; accept that the adapter is Beads-version-coupled at this shape.
- Also: specify step-4 race semantics (BrConflict on reissue + post-state observed = success).

**R3 — Correct PL-005 step citation in BI-024a** (line 303): `step 6` → `step 4` (Cat 0 pre-check per RC-012); also correct the associated prose "Beads availability check" to "Beads version + liveness handshake" since step 4 is Cat 0 pre-check and step 6 is the `br ready` query.

**R4 — Align BI-010a failure-class vocabulary with EM §8.** Replace `{transient, recoverable}` with actual EM classes. Handle `deterministic`, `budget_exhausted` explicitly (typical: no Beads write; failure is a run-level concept). `canceled` can remain punted to OQ-BI-004; the table should cite the OQ inline.

**Strongly recommended (should fix):**

**R5 — Fix the cited ON section in BI-025c:** `§4.9` → `§4.1 ON-004` (config inventory), and file a note against ON-004 to add `br` read-timeout and `br` write-timeout to the inventory.

**R6 — Correct EV-row-granularity citations:** `§8.6` → `§8.6.4` (verdict executed), and drop BI-024a's `failure_mode="br-version-incompatible"` (does not exist in ON §8; registered value is `beads-unavailable` at code 8). If a finer-grained failure mode is desired, file OQ against ON §8.

**R7 — Either add `intended_post_state` to IntentLogEntry schema** (§6.1) **or rewrite BI-031 step 1** to derive intended_post_state from `op` inline. The prose and the schema do not agree.

**R8 — Glossary adds for "audit log," "pre-state / post-state," and either rename "intent log" (single entry) vs "beads-intents directory" (directory) or add a qualifying entry.**

**R9 — §A.3 rationale paragraph update.** Line 675 still asserts the "intent log + audit log conjunction" idempotency model. Update to reflect BI-031's new status-check protocol OR (if R2 chooses option b) keep but re-narrate the audit-log dependency as a declared external requirement.

**Nice-to-have:**

**R10 — Handle the merge-pending-to-failure and failure-after-merge cases in BI-010a.** Both are concrete reliability hazards flagged by the skeptic prompt; neither is currently covered.

**R11 — Bound intent-log retention (TTL / GC).** R1 implementer flagged; still unaddressed.

**R12 — Bound BI-025d "fully" stderr capture.** 64KB cap + truncate marker.

**R13 — Soften BI-024a version regex to a semantic-version parser without fixing the surrounding grammar.** Makes harmonik resilient to upstream `--version` format tweaks within the compatibility window.

---

**Budget report:** 541 lines. Within range.

The spec's central calls — `br`-only access, terminal-only writes, git-wins, adapter-as-module, intent-log pattern — remain sound. The R1 integration made the spec materially more implementable by landing §4.8a and §6.1a, but introduced new cross-spec fabrications (§3, §4, §9.3) that need coordinated fixes with EV and RC before advancement. The BI-031 reframe is the most load-bearing correction still open: the spec claims Beads-idempotency-independence but retains the dependency in two other places (BI-032 and RC §8.4a) and at the reissue step itself. R2 proceed-with-revisions; R3 (after the above fixes) should be the reviewed-gate review.
