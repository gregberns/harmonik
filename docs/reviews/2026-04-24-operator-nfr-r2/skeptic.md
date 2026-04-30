# Round 2 Skeptic Review — operator-nfr.md v0.3.0

## 1. Verdict summary

**Return to `draft` for a targeted tightening pass.** The R1 integration was broad, competent, and closed most of the cross-reference hygiene gaps that blocked the sibling-spec-ecosystem audit. The state machine now names mechanical guards, the co-owned events list matches EV §8.7 on naming, §A.4 publishes the reverse-drift migration table, and the three budget requirements (ON-047/048/049) close the §4.11 hand-wave that R1 critic flagged. This is a materially stronger spec than v0.2.

But three blocking bugs landed inside the R1 integration itself, and two cross-spec coordination obligations that the PL R2 integration explicitly requested are still absent:

1. **The `in_flight(run)` mechanical definition cites an enum that does not exist.** ON §3 (line 73) defines `in_flight(run) ≡ run.state ∈ RunState \ {PARKED, COMPLETED, FAILED, CANCELED}` and says `[execution-model.md §7.1]` is the source of the `RunState` enum. EM §7.1 declares no such enum. EM's state names are `pending`, `running`, `completed`, `failed`, `canceled` (lowercase, in a transition table). `PARKED` is not an EM state at all — it appears nowhere in EM or any other spec. EM glossary (line 69) defines "in-flight run" as "a run whose current state is neither `completed`, `failed`, nor `canceled`" — three states, lowercase, no PARKED. ON fabricated `RunState` as a type and `PARKED` as an enum value, then cited EM §7.1 as the source. This is the exact fabrication pattern the prompt warned would appear. BLOCKING.

2. **ON §8 exit-code taxonomy is missing codes 22 and 23.** PL v0.4.0's integration (just landed) explicitly requests ON to absorb PL-INTERIM codes 22 (`ntm-unavailable`) and 23 (`orchestrator-agent-unavailable`) during ON's next revision, and says so in PL's §12 v0.4.0 row ("Codes 22 and 23 are PL-INTERIM and MUST be hoisted into ON §8 by ON's next revision (cross-spec coordination request)"). ON v0.3 added codes 19–21 per its own R1 critic Challenge 3 but did NOT pull in 22 and 23. PL has two taxonomy entries pointing at ON §8 addresses that are still vacant. BLOCKING — this is a named cross-spec coordination obligation, not a judgment call.

3. **ON continues to redeclare event payload shape in violation of R1 architect finding 6.** R1 cross-spec-architect explicitly identified that ON-013 and §6.5 must not specify payload field names because EV owns shape. The R1 integration claim ("I3 collapsed operator_pausing + operator_paused into operator_pause_status") addressed event *type naming* but did NOT address payload-field-name re-declaration. ON-013 still declares `operator_pause_status` "carries `pause_reason ∈ {operator, improvement}`" — but EV §8.7.6 payload is `status, changed_at, operator_id?`; `pause_reason` is not in EV's payload. ON-013 also says `operator_stopped` "carries `stop_mode ∈ {graceful, immediate}`" but EV §8.7.8 uses field name `mode`. ON-013 says `operator_upgrading` "carries `expected_commit_hash`" but EV §8.7.9 uses `upgrade_version`. Three name mismatches between ON's claimed payload and EV's actual payload. Either EV needs to add these fields (plausible — `pause_reason` is a real discriminator) or ON needs to stop naming them. IMPORTANT.

The rest of the spec is tight enough to ship once the above are fixed. Specifically: cross-reference migration is complete (systematic drift from §3.N/§5.N/§8.N/§9.N/§10.N anchors is gone); invariant selection test holds for all four retained invariants plus the new INV-006; every retained invariant names a sensor (AR-042 satisfied); §4.a envelope is published correctly; exit-code taxonomy is cross-referenced cleanly; rationale section honestly frames the 300s ceiling and N-1 window choices. My recommendation is one focused pass closing the blocking/important findings below, then advance to `reviewed`.

## 2. Integration-fix audit (did R1 fixes actually fix things?)

Walk of every major claim in the §12 v0.3.0 row.

### Front matter

- ✓ `spec-category: foundation-cross-cutting` added (line 8). Correct per AR-052.
- ✓ `depends-on` retains `process-lifecycle` with note that PL drops ON in its own integration; consistent with PL R2's landed state (PL v0.4.0 does not list ON in depends-on).

### BLOCKING B1 — `in_flight(run)` mechanical definition

- ✗ **FABRICATED CITATION.** ON §3 line 73 defines `in_flight(run)` as `run.state ∈ RunState \ {PARKED, COMPLETED, FAILED, CANCELED}` and says `[execution-model.md §7.1] is referenced for the enum only`. EM §7.1 is the run state machine; it is a transition table using lowercase names (`pending`, `running`, `completed`, `failed`, `canceled`). There is no `RunState` enum. There is no `PARKED` state anywhere in the corpus — `grep -rn "PARKED\|parked"` across `specs/` returns only the ON v0.3 uses plus the v0.3.0 revision-history row.
- ✗ **CASE DRIFT.** ON uses uppercase (`COMPLETED`); EM uses lowercase (`completed`). The two halves cannot be mechanically reconciled until one is renamed.
- ✓ Propagation of the predicate is present in ON-008, ON-009, ON-027, ON-030, §7.1 guards — but every one of them now routes on a predicate whose enum definition is fabricated. Implementer R1 could have mechanically written ON-008 against an EM enum that did not exist and did not notice.
- Suggested resolution: (a) rewrite ON §3 to route on EM's glossary definition lowercase-style: `run.state ∈ {completed, failed, canceled}` → NOT in_flight; else in_flight. (b) Drop `PARKED` entirely — if ON wants to distinguish "pre-dispatch bead loaded but not yet entered execution," that's a bead-state distinction (beads-integration is authoritative on bead states per MEMORY: "loaded beads must not auto-start (parked state + readiness workflow)") and belongs in a cross-reference to `[beads-integration.md ...]`, not in `RunState`. (c) Ask EM to declare a first-class `RunState` enum in §6.1 so there's a real type to cite — but that's an EM edit, not an ON edit. Pick (a); it's the smallest-scope fix.

### BLOCKING B2 — `pausing → paused` transition guard

- ✓ §7.1 state-machine row "pausing → paused" correctly requires "no run satisfies `in_flight(run)` per §3 AND every ON-027 step has completed (or drain-timeout escalated per ON-029)" (line 595).
- ✓ ON-008 text updated to name "the full drain sequence of §4.7.ON-027 (all seven steps)" and gate the transition on "(a) no run satisfies `in_flight(run)` AND (b) every drain step of ON-027 has completed" (line 185).
- ✓ ON-021 tightened: "entry into `paused` implies drain-completion" (iff form, line 285).
- Caveat: The guard is consistent across §3, §4.3, §4.7, §7.1 — but it depends on `in_flight(run)` being mechanically defined, which it isn't (B1 above). Fix B1 and B2 becomes fully coherent.

### BLOCKING B3 — Citation migration (~30 sites)

- ✓ Spot-checked for legacy `[event-model.md §3.N]`, `[reconciliation.md §9.N]`, `[process-lifecycle.md §8.N]`, `[workspace-model.md §5.N]`, `[beads-integration.md §10.N]`, `[control-points.md §6.5/§6.8/§6.9/§6.11]` — ALL purged from body text. The only remaining `§3.8` references are self-referential (prose explaining the promotion) and in §12 revision history.
- ✓ `[event-model.md §4.9] EV-034a` (cited in ON-035) resolves correctly — `source_subsystem` registration is at §4.9 EV-034a.
- ✓ `[event-model.md §8.9(h)]` (cited in ON-013 and §6.5) resolves correctly — paired-phase rule is at §8.9(h).
- ✓ `[handler-contract.md §7.1]` for silent-hang FSM resolves correctly.
- ✓ `[process-lifecycle.md §4.6] PL-018a` for panic barrier resolves correctly.
- ✓ `[reconciliation/spec.md §8.12]` for action-mapping resolves correctly.
- Verdict: citation migration is genuinely complete at the body-text level. Good work.

### IMPORTANT I1 — Reverse-drift §A.4

- ✓ §A.4 published with 12 legacy-to-current anchor mappings (lines 853–873). Matches the inbound-citation inventory from R1 cross-spec-architect.
- Caveat: The table row for legacy `§7.1` ("Operator-control state machine") maps to "`§4.3` (between-task semantics) PLUS `§7.1` (state-machine table, ON v0.3 retains this number)." This means inbound citations to legacy `§7.1` that intended the operator-control SECTION resolve partly correctly (the new §7.1 does retain the state-machine table content) but the semantics content moved to §4.3. Downstream readers following the legacy pointer land on the pseudocode only, not the semantics. This is an acceptable compromise but worth noting: §7.1 has two intended meanings and §A.4 names both.

### IMPORTANT I2 — `handler_silent_hang → agent_warning_silent_hang` rename

- ✓ ON-040 line 419 names `agent_warning_silent_hang` per EV §8.3.10. Correct.

### IMPORTANT I3 — `operator_pausing` + `operator_paused` → `operator_pause_status`

- ✓ §6.5 lists `operator_pause_status` as paired-phase (line 574).
- ✓ §7.1 state-machine table emits `operator_pause_status (status=pausing)` on entry, `operator_pause_status (status=paused)` on drain-completion.
- ✓ ON-013 text names the paired-phase merge.
- ✗ **Payload field-name drift uncaught (see §1 blocking #3).** ON-013 says the event "carries `pause_reason ∈ {operator, improvement}` and `changed_at` (ms resolution)." EV §8.7.6 payload declares `status`, `changed_at`, `operator_id?` — no `pause_reason`. This is a genuine conflict: if ON wants `pause_reason` to be in the payload, EV §8.7 needs to add it; otherwise ON needs to strip the payload-field-name claim and put `pause_reason` in some other vehicle (e.g., a hint that the emission site attaches `pause_reason` via EV's structured-fields mechanism, not as a top-level payload field).

### IMPORTANT I4 — Add `operator_command_rejected` and `dispatch_deferred` to §6.5

- ✓ Both events present in ON §6.5 bullets and ON-013 enumeration. §8 exit codes 16 and 18 cite the right events. Clean.

### IMPORTANT I7 — Exit-code taxonomy expansion (codes 19–21)

- ✓ Codes 19 (runtime-panic), 20 (signal-terminated), 21 (drain-step-errored) added to §8 table (lines 678–680).
- ✗ **Codes 22 (ntm-unavailable) and 23 (orchestrator-agent-unavailable) absent.** PL v0.4.0 requests these explicitly as a cross-spec coordination obligation. Absent from ON v0.3. See §1 blocking #2.

### IMPORTANT I8 — Invariants audit

- ✓ ON-INV-002 retired (operational posture moved to §2.1a); ID permanently marked as not-reused.
- ✓ ON-INV-004 retired (restatement of §4); ID permanently marked as not-reused.
- ✓ ON-INV-001 sensor named (corpus-wide compat-matrix test harness). AR-042 honored.
- ✓ ON-INV-003 sensor named (two-part sensor: compile-time linter + regression). AR-042 honored.
- ✓ ON-INV-005 rewritten as cross-subsystem reconstruction-contribution invariant; sensor named (fixture-backed restart-recovery test harness). AR-042 honored.
- ✓ ON-INV-006 new: no subsystem introduces a control surface bypassing the between-task invariant; sensor is "corpus-wide grep-plus-reviewer audit." AR-042 honored.
- Caveat on ON-INV-006: the sensor is "grep-plus-reviewer" pending a mechanical lint. That's a legal sensor per AR-042 (reviewer-enforced sensors count), but it's close to the line. For an invariant whose violation surface is "a subsystem spec declared a control surface that bypasses the state machine," a concrete reviewer-persona assignment (conformance-auditor? critic?) would be stronger than "grep-plus-reviewer." Not a blocker; a polish item.

### IMPORTANT I9 — RTO target binding

- ✓ ON-031 line 352 binds to "30 seconds nominal fixture target (p95 under the fixture defined in §4.8.ON-032 criterion 1)" and "300-second hard ceiling (§4.8.ON-032 criterion 3)." Sensor named ("a restart-RTO test harness backed by a standard fixture").
- ✓ OQ-ON-005 honestly tracks residual ambiguity (auto-escalate vs notify-only; fixture-tightening vs target-relaxation).
- ✗ **Fixture defined only informally.** §4.8.ON-032 criterion 1 says "≤ a few hundred open beads, ≤ a few dozen in-flight runs" — this is R1 critic's Challenge 6 language and R1 critic explicitly flagged it as "not a bound." R1 integration kept the same language. The sensor ("restart-RTO test harness backed by a standard fixture") names a harness but defers the fixture's concrete bounds. If the harness is to serve as the AR-042 sensor for ON-INV-005's "300-second ceiling invariant" angle, the fixture needs a concrete bound. Suggested: "≤ 500 open beads, ≤ 50 in-flight runs, git-log depth ≤ 10,000 commits since the oldest open bead's first checkpoint."

### IMPORTANT I10 — §4.11 expansion (ON-047, ON-048, ON-049)

- ✓ ON-047 category defaults table present with five rows. Clean.
- ✓ ON-048 exhaustion protocol enumerates four steps. Default `pause-and-escalate` with `pause-on-exhaustion=false` default is explicit.
- ✓ ON-049 attribution shape `(run_id, role, node_id, category, amount)` plus `delegation_path` for cognition-tagged steps.
- ✗ **ON-048 and ON-049 re-declare EV payload shape.** ON-048 says to emit `budget_exhausted` with `category`, `scope`, `exhausted_at`. EV §8.4.3 `budget_exhausted` payload is `run_id`, `session_id?`, `budget_ref`, `attempted_dispatch_cost` — no `category`, no `scope`, no `exhausted_at`. Similarly ON-049's five-field shape (`run_id, role, node_id, category, amount`) is inconsistent with EV §8.4.1 (`run_id, session_id?, budget_ref, threshold_fraction, remaining`) and §8.4.2 (`run_id, session_id, chunk_index?, cost_units, cost_basis`). None of EV §8.4.1/2/3 carry `role`, `node_id`, `category` at top level. Either ON is unilaterally redefining payloads (ownership conflict — EV is authoritative per §6.5 co-ownership rule) or ON means "the emitter's structured evidence map contains these fields" and needs to say so. The current text reads as the former, which conflicts with EV.
- Caveat on ON-047's warning threshold row: the table says warning = "80% of budget … [control-points.md §4.5] CP-025." CP-025 is the 80% warning threshold rule. Consistent.

### IMPORTANT I11 — ON-041 clarification

- ✓ "Scope clarification" paragraph at line 429 distinguishes per-daemon ceiling (PL-014a) from machine-level ceiling (ON-041c). Correct and readable.
- ✗ **ON-041 daemon-discovery mechanism still unspecified.** R1 implementer flagged: "`discoverProjects()` has no declared mechanism." OQ-ON-003 addresses the coordinator/lock choice for the ceiling mechanism, but NOT the daemon-enumeration mechanism for `harmonik list`. How does a freshly-installed operator invoking `harmonik list` on a new terminal discover running daemons from other terminals? Scan `$HOME` for `.harmonik/daemon.pid`? Walk a machine-level registry? Read from a shared socket directory? Silent. An implementer cannot code this without choosing; the spec should name the choice or open an OQ.

### IMPORTANT I12 — OQ honesty labels

- ✓ OQ-ON-001 and OQ-ON-003 marked "UNRESOLVED" with architect-honest rationale. Good.

### Template obligations

- ✓ §4.a envelope (ON-ENV-001) declares (a)–(h). The envelope-requirement IDs use the reserved `ON-ENV-NNN` range per AR-053.
- ✓ The voluntary-envelope note at §4.a correctly cites AR-052/AR-053.
- ✓ Every requirement carries `Tags:`.
- ✓ ON-008, ON-011, ON-013, ON-005, ON-016, ON-022, ON-027, ON-028, ON-031, ON-037, ON-048, ON-INV-005 have `Axes:` lines (I/O or state mutation).
- ✗ **Missing Axes: line at ON-049** (budget-attribution event emission is non-idempotent state-mutation to event log — emission is an external I/O per the template trigger). Should carry axes. R1 implementer's axes-sweep list flagged ON-046; ON-049 is a new addition with the same shape and should also carry the line.
- ✗ **Likely missing Axes: at ON-025** (skill provisioning mutation; external I/O) and **ON-030** (git walk + Beads query is external I/O). These were flagged by R1 implementer's axes-sweep and not addressed.

## 3. Hidden assumptions v0.3.0 relies on but hasn't proven

1. **Every subsystem can cheaply compute `in_flight(run)`.** The drain gate (ON-008, ON-027 step 2) requires "no run satisfies `in_flight(run)`" before `pausing → paused` transition. The predicate is evaluated against the orchestrator's in-memory model (per the §3 glossary's "authoritative in this spec" statement, though this is implied rather than stated). Subsystems that hold independent in-flight-run views (reconciliation dispatcher, agent runner, workspace manager) may hold a stale view. Spec is silent on whether subsystem-level views must be reconciled before the predicate evaluates. Resolution: name the authoritative view explicitly (orchestrator-core's in-memory run table) and obligate subsystems to consult it via a named query rather than hold parallel state.

2. **RTO measurement tooling exists to enforce the 30s p95.** ON-031/032/033 depend on a "restart-RTO test harness" that is named (ON-031) but not specified. The fixture is "a standard fixture (≤ a few hundred open beads, ≤ a few dozen in-flight runs)" — neither "standard" nor the bounds are precise. The sensor cannot run until the fixture exists. MVH testing infrastructure may not include it at bootstrap. OQ-ON-005 defers to "first RTO measurement" as the revisit trigger. Resolution: either concretize the fixture bounds (500 beads, 50 runs, 10,000 commits depth) or declare the sensor as a post-MVH obligation rather than an MVH invariant-sensor.

3. **Operator-attached terminal supports tmux pass-through.** ON-040 cites HC §4.6 and EV §8.3.10 for silent-hang detection; the enforcement obligation is via `ntm`/tmux per PL-021a. On a host without tmux (minimal Linux container, Windows shell, CI box with a restrictive image), the silent-hang surface exists but the detection cannot fire cleanly because the subprocess isn't in a tmux pane. ON-040's observability obligation is non-trivially dependent on PL's tmux assumption. The silent-hang observable consequence "a subsystem `degraded` classification per §4.9.ON-037" is OK on tmux-less hosts but the event payload `fsm_state` (EV §8.3.10) is tmux-FSM-specific. Resolution: §4.10 should note that tmux-less MVH hosts carry a known silent-hang detection gap, and OQ-ON-006 (currently about PL drain-adoption) could be extended or a new OQ tracks the tmux dependency.

4. **Config inventory will build from running source, not hand-curated.** ON-004 obligates a config inventory "enumerating every operator-configurable knob referenced across foundation specs." The inventory at MVH has no mechanical builder (no lint that walks every `operator-configurable` mention in every foundation spec and diffs against the inventory). An implementer asked to produce the inventory today would hand-curate it; a subsequent spec change adding a knob would silently break the inventory's "every knob" obligation. Resolution: name the sensor (an OQ-ON-008 "inventory-completeness lint: CI walks each foundation spec's §4 for the phrase 'operator-configurable' and fails if any mention lacks a corresponding inventory entry").

5. **Structured-log wire format is wirelock-stable across releases.** ON-035 specifies the minimum shape (`ts, level, subsystem, run_id?, node_id?, msg, fields`) as newline-delimited JSON. The spec does not say whether this shape is N-1 readable (per ON-INV-001) or freely evolvable. If structured-log format changes break consumers (log-parsers, `harmonik attach` formatters), ON-INV-001 is violated — but §4.5.ON-018 enumerates "every versioned on-disk or wire artifact declared by foundation specs" and does not include structured logs in the enumeration. Either structured logs are under N-1 (enumerate them in ON-018) or they're explicitly exempt (say so). Resolution: amend ON-018's enumeration to include structured logs, with the version field declared in the minimum wire shape (e.g., add `log_schema_version` as a top-level field).

6. **Direct-to-main development is safe for foundation specs.** §2.1a says "no PR-based merge gate is the MVH enforcement model." This is an assumption about the operator's discipline — agent-reviewer-every-commit catches regressions at commit time. But nothing in ON enforces that subsystem specs authored under this posture actually run the agent reviewer; that's a build-practices obligation. If a subsystem spec lands without reviewer sign-off, ON's invariants (particularly ON-INV-006 "no control surface bypass") can silently drift. Resolution: not ON's problem to solve, but name the dependency explicitly — "ON-INV-006 relies on the agent-reviewer discipline of build-practices.md §Agent review; absent that, ON-INV-006's sensor cannot fire."

7. **Operator-supplied expected commit-hash is trustworthy.** ON-005's integrity gate depends on the operator providing a correct hash. The spec does not address the supply-chain of that hash (how the operator obtained it — Slack message? release page? `git rev-parse`?). R1 critic's Challenge 7 flagged this and was not addressed in v0.3. The R1 integration claim did not mention it. Resolution: ON-005 should acknowledge that the commit-hash check is a version-identity check, not a cryptographic integrity check (per R1 critic's suggested ON-005c text), and defer the supply-chain question to post-MVH signing (ON-006). The spec's §A.3 rationale already half-makes this case; promoting it to a sub-requirement would close the loop.

8. **Machine-ceiling lock file is race-free across crashes.** OQ-ON-003 defaults to `~/.harmonik/machine-ceiling.lock` with advisory locking. R1 critic Challenge 8 and counter-example 6 flagged that crashed-daemon lock orphans are unhandled. OS-level advisory locks (`flock`, `fcntl`) are released on process death — but the lock FILE remains on disk as a marker. A naive implementation that checks file presence as "lock held" races with crash recovery. Resolution: OQ-ON-003 should name the lock primitive (advisory `flock(2)` on the file, not file existence as the signal) and explicitly state that file existence alone is NOT sufficient to conclude a daemon holds the lock.

## 4. R1 regressions

Places R1 lost context or over-reached.

1. **R1 integration claim on I3 over-states what was fixed.** The row says "collapsed `operator_pausing` + `operator_paused` into `operator_pause_status` paired-phase event per [event-model.md §8.9(h)]; ON-013, §6.5, §7.1 rewritten." True for event-TYPE naming but NOT true for payload-FIELD naming — ON-013 still asserts a `pause_reason` payload field that EV does not declare, and `operator_upgrading` carries `expected_commit_hash` per ON-013 vs `upgrade_version` per EV. R1 architect's finding 6 was specifically about payload-shape redeclaration; the fix closed the type-naming half and left the field-naming half open.

2. **R1 integration claim on I7 over-claimed taxonomy completeness.** The v0.3.0 row says "expanded exit-code taxonomy (§8) with codes 19 (runtime-panic), 20 (signal-terminated), 21 (drain-step-errored)." This is R1 critic's Challenge 3 addressed. But ON R2 is happening simultaneously with PL R2, and PL R2 landed codes 22/23 in PL as PL-INTERIM pending ON. A careful integration pass would have coordinated the PL R2 landing and ON R2 to absorb 22/23 in the same cycle. This one isn't "an R1 regression" strictly — it's "an R1 integration that did not look sideways at the concurrent PL R2 integration." But it lands the same way: ON v0.3 is shipped with a cross-spec coordination gap that PL v0.4.0 explicitly names.

3. **`RunState` enum fabrication.** R1 implementer's recommended fix for "in-flight run" was to define a predicate routing on EM's state lifecycle (Challenge 1 stronger-alternative). R1 integration reached for a type (`RunState`) and enum values (`PARKED`, `COMPLETED`, etc.) that the target spec does not declare. This is a regression of "did not read the target spec carefully before citing it" — EM §7.1 is less than 30 lines and its lowercase-vs-uppercase state names are obvious on inspection.

4. **Section-heading hygiene: ON §6 lacks subsections §6.1–§6.3 that the template expects.** Template §6 says "Every schema MUST use one of the presentations below" and enumerates §6.1 pseudocode records, §6.2 YAML/JSON snippets, §6.3 tabular. ON §6 jumps directly to a prose introduction then §6.4 Schema evolution then §6.5 Co-owned event payloads. This is legitimate ("This spec does not introduce new persistent data types" at line 556) but the numbering implies §6.1/§6.2/§6.3 exist and don't. Either (a) add "§6.1 Types — none; see [owning specs]" placeholder subsections OR (b) explicitly state "§6 omits §6.1–§6.3 per the declaration at line 556; §6.4 and §6.5 are the only subsections used." Clean up.

## 5. Over-specification vs under-specification

### Over-specified

1. **Structured-log inline schema (ON-035, line 387).** ON-035 declares the minimum wire format inline: `ts, level, subsystem, run_id?, node_id?, msg, fields`. This is OWNED by ON per the I6 promotion. But OQ-ON-007 says the "detailed schema" belongs in a dedicated `quality-checks.md` work. The current shape is over-specified for the deferred-owner state: ON commits to field names that `quality-checks.md` may want to change. Resolution: reduce ON-035's inline shape to "the minimum set of required fields (timestamp, level, subsystem, message)" and leave `run_id?`, `node_id?`, `fields` to the deferred schema.

2. **ON-047 warning-threshold 80% duplicates CP-025.** ON-047's table row "Warning threshold | 80% of budget | all categories | [control-points.md §4.5] CP-025" is a restatement of CP-025 (which ON cross-references in the override locus column). The row is informative, not normative — ON-047 is the category-defaults table and the warning threshold is owned by CP. The row could be a footnote rather than a table row, or simply omitted (the cross-reference is explicit already). Minor — not blocking.

3. **ON-031 naming both the sensor AND the fixture bounds in one requirement.** ON-031 is about the target; the fixture bounds belong in ON-032 (which already has them) and the sensor is a harness. Splitting into "ON-031a: target" and "ON-031b: sensor/fixture" would be cleaner. Stylistic, not blocking.

### Under-specified

1. **ON-005 commit-hash check — unnamed hash-computation procedure, unnamed operator-hash source, unnamed trust model.** R1 critic Challenge 7's three questions (who computes actual_hash, where does expected_hash come from, what's the verifier trust model) are not addressed in v0.3. An implementer cannot code the gate without choosing. Promote to at least ON-005a: "The daemon MUST compute `actual_hash` from the build-time embedded ldflags stamp; binaries without an ldflags stamp MUST fail the integrity gate."

2. **ON-020(e) socket-rebind mechanism.** ON-020 obligates "daemon MUST re-bind the same socket path after exec-replace"; R1 implementer flagged this assumes FD inheritance (`SOCK_CLOEXEC=0` or equivalent) and no mechanism is named. PL R2 landed PL-027(iii) socket-rebind fd-passing as a normative mechanism (nginx/HAProxy pattern with `HARMONIK_LISTENER_FD` env var). ON-020 does not cross-reference PL-027(iii) even though PL-027 delegates the contract to ON. Resolution: ON-020(e) should cross-reference PL-027(iii) explicitly OR adopt the fd-passing language inline.

3. **ON-029 drain timeout — per-step vs global.** ON-029 says "the drain timeout (the bound on steps 2 and 3)" (singular "timeout") but §7.2 pseudocode uses `timeout.step_2` and `timeout.step_3` as separate fields. R1 critic MUST/SHOULD item 2 flagged the contradiction; v0.3 did not reconcile. Either name the bound as global (one timeout, shared across steps 2–3) or name it as per-step (two independently-configurable timeouts, neither more than `drain_timeout`). The §7.2 pseudocode suggests per-step; ON-029 prose suggests global. Pick one.

4. **ON-037 heartbeat default cadence.** "Operator-configurable per §4.1.ON-004" is correct but ON-004 is the obligation to produce the inventory, not the inventory itself. Default cadence is needed for MVH boot-safety ("no policy declared" case). R1 critic observability-envelope gap item — not addressed in v0.3. Resolution: ON-037 names a default cadence (e.g., "10 seconds; 3 missed heartbeats triggers degraded") with the caveat that the inventory's future refinement overrides.

5. **ON-041 daemon-discovery mechanism.** Per §2 integration-fix audit item I11. `harmonik list` cannot be implemented without choosing a discovery mechanism; ON-041 is silent.

6. **ON-048 exhaustion-protocol step-2 "safe boundary."** ON-048 step 2 says "Terminate the in-flight LLM call or tool invocation at the next safe boundary (post-chunk for token budgets; post-iteration for iterations budgets; post-step for wall-clock budgets)." Post-step for wall-clock is not defined in ON — a "step" is an execution-model concept but ON does not cite a specific EM requirement for "step boundary." Clarify with `[execution-model.md §4.X]` cross-reference.

7. **ON-INV-005 reconstruction-contribution interface.** "The specific interface (a Go method or a startup-probe event) is per subsystem" — OK for flexibility but the sensor ("each subsystem emits a reconstruction-completed signal before `ready`") assumes a runtime-observable emission exists. If some subsystem chooses the "Go method" path, the sensor has no runtime signal to scan. Resolution: obligate the runtime-observable emission (startup-probe event) as the canonical surface, with Go methods as an additional optional API.

## 6. Cross-spec promises — OQ realism, which should be blocking

Seven OQs. Each named owner; each named default-if-unresolved; each named blocks-what.

### OQ-ON-001 — Config inventory authoritative location

- Status: UNRESOLVED. Owner: foundation-author. Blocks: ON-004 completeness.
- Default: sibling file `specs/operator-nfr/config-inventory.md`.
- Realism: honest. The default is a reasonable starting point. The "~300 lines / multiple non-NFR owners" criterion for escalation is concrete.
- Not blocking for R2 advance.

### OQ-ON-002 — testing.md migration

- Status: open. Owner: foundation-author. Blocks: none.
- Default: keep prose obligations; migrate after testing.md lands.
- Realism: honest. testing.md does not exist yet; the spec admits this.
- Not blocking.

### OQ-ON-003 — Machine-ceiling coordinator implementation locus

- Status: UNRESOLVED. Owner: foundation-author. Blocks: ON-041 implementation shape.
- Default: filesystem-based shared-counter lock at `~/.harmonik/machine-ceiling.lock`.
- Realism: partial. The default glosses over the "stale lock" problem (see §3 hidden assumption 8). Counter-example 6 (lock orphan) is unaddressed.
- **Should be expanded** to name the lock primitive (`flock(2)` advisory) and the stale-lock recovery procedure (next daemon's startup removes-and-reacquires if parent PID is gone). Not blocking R2 advance but a quick polish.

### OQ-ON-004 — Concurrent-operator arbitration

- Status: open. Owner: foundation-author. Blocks: none.
- Default: "second command observes the state-machine in the post-first-command state."
- Realism: honest. Single-operator MVH makes this a known silence.
- Not blocking.

### OQ-ON-005 — RTO ceiling behavior (notify-only vs auto-escalate)

- Status: open. Owner: foundation-author. Blocks: none.
- Default: notify-only via `daemon_degraded`.
- Realism: OK. But the OQ also rolls in the "fixture tightening vs target relaxation" question. That's two distinct decisions bundled — split them.
- Not blocking.

### OQ-ON-006 — PL adopting ON-027 drain steps

- Status: open. Owner: foundation-author. Blocks: none ("ON is normative for the seven-step sequence; PL's alignment is the deferred coordination").
- Realism: honest; PL is the edit side.
- **But the edit is a real outstanding coordination debt** — PL v0.4.0 did not adopt ON-027's step list explicitly; PL-011 still has its own shutdown-drain sequence that overlaps but doesn't match ON-027 step-for-step. This is not strictly blocking R2 advance of ON but the user should be aware that ON R2 advancing does NOT unblock the PL-drain-adoption loop.

### OQ-ON-007 — structured-log wire format home

- Status: open. Owner: foundation-author. Blocks: none.
- Default: inline shape in ON-035 sufficient for MVH.
- Realism: honest.
- Not blocking.

### OQ promoted-to-R2 candidates

Two items in this review's §5 under-specification list would merit OQs if not addressed directly:
- `harmonik list` discovery mechanism (ON-041) — candidate OQ-ON-008.
- Commit-hash computation procedure (ON-005) — candidate OQ-ON-009 or inline fix.

## 7. Definitional drift

Terms whose meaning shifted or is inconsistent across §3 glossary, §4 requirements, §6 schemas, §7 state machine.

1. **`pausing` / `paused` / `pause_reason`.** §3 glossary does not define `pausing` or `paused` as state-machine states. §7.1 state-machine table uses both. ON-013 names `pause_reason ∈ {operator, improvement}`. §6.5 names the same. Consistent across §4 / §6 / §7 but absent from §3. Minor: add a glossary entry "`pausing` — transient state during drain; `paused` — state after drain completes, ready for resume or upgrade."

2. **`draining` / `drain` / `quiescing`.** §3 defines `drain` as the ordered shutdown sequence. §7.1 does NOT have a `draining` state; the state between `running` and `paused` is `pausing` or `improvement-pausing`. R1 critic's Challenge 2 suggested adding `quiescing` as an intermediate state; v0.3 resolved by requiring all seven drain steps inside `pausing`. OK, but the word "drain" is used both for the shutdown sequence (noun) and the act of drain-ing (verb). Glossary could tighten.

3. **`stopping` vs `stopped`.** §7.1 has `stopped` as a terminal-recoverable state (via `start`). It does NOT have `stopping`. A `stop --graceful` command transitions `running → drain → stopped` atomically per the table row; there's no intermediate `stopping` state. That's consistent, but the §3 glossary does not name any of `stopping`, `stopped`, or their distinction. Define.

4. **`improvement-pause` subtype.** §3 glossary: "a subtype of pause with a scheduled or triggered onset." §7.1 state machine: separate states `improvement-pausing` and `improvement-paused`. ON-012 says "an improvement-pause MUST transition `running → pausing → paused` via the same path as an operator pause." But §7.1 shows `running → improvement-pausing → improvement-paused` — a separate state chain, not the same path. §4 text contradicts §7 table. Resolution: either rewrite ON-012 to say "via a state-chain parallel to the operator pause, distinguished by subtype" OR collapse improvement-pausing/paused into the same states as pausing/paused with `pause_reason` as the only discriminator. The latter is what §6.5 implies (one event type, `pause_reason` payload), so §7.1 should collapse too. Currently the states are over-split.

5. **`degraded` — subsystem-level vs daemon-level.** ON-036 says every subsystem returns `health_status ∈ {OK, degraded, failed}`. ON-037 says missing heartbeats trigger subsystem `degraded`. ON-031/032 says the DAEMON enters `degraded` on RTO ceiling breach. Two different `degraded` surfaces: subsystem-level (aggregated to harmonik-wide) vs daemon-level (emitted to operator). Are they the same state? If yes, the operator sees `degraded` with different semantics depending on the source. If no, the two should be distinguished terminologically. Resolution: rename one of them (e.g., `subsystem_degraded` vs `daemon_degraded`) or clarify that the daemon's `degraded` is the aggregate.

6. **`task` vs `run`.** ON-007 says operator surfaces use "task", specs use "run". §3 glossary line 72 says the same. ON-013 and ON-008 consistently use "run". Not drift — clean and consistent.

## 8. Template conformance

- **Envelope (§4.a / ON-ENV-001).** Present. (a)–(h) all addressed. Verdict: PASS.
- **`spec-category: foundation-cross-cutting` in front matter (line 8).** Present. PASS.
- **Tags line on every requirement.** Spot-check: ON-001 through ON-049, ON-INV-001/003/005/006, ON-ENV-001 all carry `Tags: mechanism`. PASS.
- **Axes line on I/O- or state-mutating requirements.** Partial: ON-005, ON-008, ON-011, ON-013, ON-016, ON-022, ON-027, ON-028, ON-031, ON-037, ON-048, ON-INV-005 carry axes. Missing axes on ON-025 (skill provisioning mutation), ON-030 (git + Beads I/O), ON-049 (event emission to durable sink). R1 implementer flagged ON-005 event emission, ON-025 provisioning, ON-030 I/O, ON-046 emission — none fully swept in v0.3. MINOR regression.
- **Sensor named on every invariant (AR-042).** ON-INV-001 ✓, ON-INV-003 ✓, ON-INV-005 ✓, ON-INV-006 ✓. ON-INV-002 and ON-INV-004 retired. PASS.
- **§9.2 Reverse dependencies.** INFORMATIVE placeholder (line 734). Template permits. PASS.
- **§11 OQ format (Owner / Blocks / Default-if-unresolved).** OQ-ON-001 ✓, OQ-ON-002 ✓ (Blocks: none), OQ-ON-003 ✓, OQ-ON-004 ✓, OQ-ON-005 ✓, OQ-ON-006 ✓, OQ-ON-007 ✓. PASS.
- **§12 revision history density.** Three rows: v0.1 (initial), v0.2 (citation cleanup), v0.3 (R1 integration). Each names the changes; v0.3 row is densely detailed. PASS.
- **Section completeness: §§1–12, §A.** All present. PASS.
- **§6 subsection sequencing.** §6 lacks §6.1/§6.2/§6.3 despite numbering §6.4 and §6.5. Template expects §6.1–§6.5; ON's absence is legal (the intro at line 556 says "does not introduce new persistent data types") but the jump from intro to §6.4 reads as if sections are missing. MINOR cleanup.

## 9. New failure modes surfaced by v0.3.0

Requirements added in v0.3 that surface or enable failure shapes prior reviews couldn't have flagged.

1. **ON-048 `dispatch_deferred` cascade may create a run wedge.** ON-048 step 4 says emit `dispatch_deferred` when exhaustion cascades to multi-run ceiling breach. An operator watching `harmonik status` sees a run at "dispatch_deferred" with no bound on how long it stays deferred (step 4 says "cascade to multi-run ceiling breach" but does not name retry or escalation cadence). A deferred dispatch could sit in the queue indefinitely while other runs proceed. Resolution: ON-048 step 4 should name a retry policy or escalation path, or cross-reference the owning spec's policy.

2. **ON-049 attribution for non-agentic nodes.** The five-field shape `(run_id, role, node_id, category, amount)` assumes every node has a `role`. Non-agentic nodes (mechanism nodes per EM §4.2 EM-006) may not have a role in the role-taxonomy sense (per AR §4.8). What does `role` hold for a non-agentic node that consumes wall-clock budget? An empty string? The mechanism-subsystem identifier? Silent.

3. **ON-047 per-reconciliation-workflow budget default 10 minutes.** Reconciliation spec RC-017 obligates a wall-clock budget per reconciliation workflow and RC-018 says budget-exhaustion produces a fallback verdict (`escalate-to-human`). ON-047's 10-minute default may collide with RC-017's per-category bounds (which are set per the reconciliation spec's §4.4 RC-017). A default of "10 minutes" at ON is either a global floor (RC must stay ≥ 10 min) or a fallback (RC overrides). ON-047's "Override locus" column says "`[reconciliation/spec.md §4.4]` policy" — RC wins. OK but worth naming explicitly.

4. **Operator-control-state-machine entry assumes ready.** §7.1 informative note says "operator-control entry (`running`) occurs only at `ready`." But the `running` state has outgoing transitions via `pause`, `stop`, `upgrade` — what happens if the operator issues these while the daemon is in `reconciling`? ON-010 says "pause is queued." Stop? Upgrade? §7.1 does not show `reconciling → pausing` or `reconciling → stopped`. The state machine is written for operator-control only and silently assumes the daemon is in operator-control state. Counter-example 4 from R1 critic (two-operator race) is a symptom. Resolution: §7.1 should either note explicitly "transitions shown apply only when daemon status (per [process-lifecycle.md §4.2]) is `ready`; see ON-010 for queuing rules during `reconciling`" OR add `reconciling` rows to the state machine.

5. **ON-INV-006 sensor is reviewer-enforced, which means a subsystem-spec author can inadvertently introduce a bypass and have it land through direct-to-main with only agent review catching it.** Per §2.1a, MVH runs direct-to-main; the agent reviewer catches regressions but is not infallible. If a subsystem spec declares, say, `harmonik force-abandon-run <run_id>` as a CLI command (a bypass of the drain gate), ON-INV-006 flags it — but only at spec-draft review time. Between spec-draft pass and spec finalization there's a window. Resolution: accept the window as the known MVH discipline, or obligate a mechanical lint in the long term (tracked as a post-MVH OQ).

## 10. Affirmations

Decisions that survive R1+R2 pressure and SHOULD NOT be reopened.

1. **Between-task invariant as the operator-control backbone (ON-008, ON-009, ON-INV-006).** `stop --immediate` as the single carve-out; all other controls drain. This is locked decision #10 and the spec's coherence is strongest at this surface. Reopening would destroy the checkpoint-trail contract of EM §4.5. Leave alone.

2. **N-1 compatibility window as a corpus-joint invariant (ON-INV-001).** The invariant's framing as a joint property across EV + EM + CP + BI + the queue overlay is correct; individual relaxation breaks the joint property. The rationale at §A.3 is well-argued.

3. **Improvement-pause as a subtype of operator-pause (ON-012).** Not a new state class; a pause with auto-resume on improvement-loop completion. MVH-minimal and compositionally clean. (Note: the §7.1 state-machine representation splits into separate `improvement-pausing`/`improvement-paused` states, which conflicts with the §3 "subtype" framing. Fix §7.1 to collapse states and use `pause_reason` as the sole discriminator — see §7 drift item 4 — but keep the subtype conceptual framing.)

4. **Secrets-redaction compile-time schema check (ON-023) + multi-sink runtime sensor (ON-INV-003).** Closes the redaction-obligation loop at both compile-time (no `Secret`-typed field possible) and runtime (regression harness scans every durable sink). This is exemplary defense-in-depth.

5. **Exit-code taxonomy as a tabular authoritative contract (§8 + ON-001).** Every non-zero code has a detection rule, emitted event, and remediation pointer. Even with codes 22/23 missing (blocking fix) and minor rare-code policy ambiguity, the SHAPE is right. Preserve.

6. **§6.5 co-ownership pattern (normative for WHEN, not WHAT).** The closing statement "This spec is normative for the *when*; event-model is normative for the *shape*" is the correct template §6.5 shape. Other specs should cite this as an exemplar. (Caveat: the current ON text slips and names payload fields; that's a finding against ON-013's implementation of the pattern, not against the pattern itself.)

7. **ON-INV-002 retirement with content preservation at §2.1a.** The "no PR-gated rollout for MVH" content IS load-bearing for the whole spec corpus — it tells subsystem authors not to design contracts that assume a pre-merge human review gate. Retiring the invariant (correct per §5 selection test) AND preserving the content as a scope assumption (correct per template) is the right move.

8. **ON-049's `delegation_path` field for cognition-tagged cost attribution.** Naming the CP-039 delegation path on budget events ties cost back to the specific model-class invoked. This is the right hook for post-MVH per-tenant / per-model cost analysis without committing to a cost-attribution spec now.

9. **ON-031's two-criterion split (nominal p95 vs hard ceiling).** Separating "relaxable with reason" (nominal) from "non-negotiable" (ceiling) is the right MVH posture: the nominal target is an estimate for operator expectations, the ceiling is an operator-observability boundary.

10. **§A.4 reverse-drift migration map.** Publishing the §7.N legacy-to-current anchor table in ON's §12/§A.4 unblocks ~70 inbound citations in the corpus cleanup. Without this, every downstream reviewer rediscovers the rename; with it, they consult the table. Cheap insurance.

## 11. Recommendation — concrete checklist for the integration author

### BLOCKING — must land before advance to `reviewed`

1. **Fix the `in_flight(run)` fabrication.** Rewrite ON §3 line 73 to route on EM's actual glossary predicate: `in_flight(run) ≡ run.state ∉ {completed, failed, canceled}`. Delete the `RunState` type reference and the `PARKED` state. If a pre-dispatch/parked concept is needed, route through `[beads-integration.md §4.3]` bead-state (not `RunState`). Propagate the casing fix through ON-008 / ON-009 / ON-027 / ON-030 / §7.1 guards. Estimated edit: 5 lines in §3, ~10 lines across requirements.

2. **Add exit codes 22 and 23 to §8.** Insert rows for `ntm-unavailable` (code 22, emits `infrastructure_unavailable{failed_prerequisite=ntm_unavailable}`, remediation per PL-021a) and `orchestrator-agent-unavailable` (code 23, emits `infrastructure_unavailable{failed_prerequisite=orchestrator_agent_unavailable}`, remediation per PL-028). Cross-reference PL-021a and PL-028 in the detection-rule column. Remove PL-INTERIM markers in PL is a follow-on edit owned by the next PL revision; this ON edit hoists the codes. Estimated edit: 2 table rows.

3. **Fix ON-013's payload-field-name redeclaration.** Either: (a) remove the phrases "carries `pause_reason ∈ {operator, improvement}` and `changed_at`" / "carries `stop_mode ∈ {graceful, immediate}`" / "carries `expected_commit_hash`" from ON-013, replacing them with "see [event-model.md §8.7.N] for payload shape"; OR (b) raise an EV amendment to add `pause_reason` to `operator_pause_status` payload and rename EV §8.7.8 `mode` → `stop_mode` and §8.7.9 `upgrade_version` → `expected_commit_hash`. Path (a) is cheaper and respects the §6.5 co-ownership split; path (b) requires an EV edit. Recommend (a). Estimated edit: 3 bullets in ON-013 and §6.5.

### IMPORTANT — should land before advance to `reviewed`, but deferrable with an OQ

4. **Reconcile §3 glossary / §7.1 state-machine representation for improvement-pause.** Collapse the `improvement-pausing`/`improvement-paused` separate states into `pausing`/`paused` with `pause_reason = improvement` as the discriminator, matching §6.5's event-payload treatment. Update §7.1 table rows accordingly. ON-012 already says "via the same path"; §7.1 should reflect this. Estimated edit: ~4 table rows consolidated to 2.

5. **Rename `degraded` to disambiguate subsystem-vs-daemon.** Either `subsystem_degraded` for ON-036/037 and `daemon_degraded` for ON-031/032, or explicitly name the relationship ("daemon `degraded` is the aggregate of subsystem `degraded`"). Minor; improves clarity.

6. **Resolve ON-029 per-step vs global drain-timeout apportionment.** The pseudocode uses per-step (`timeout.step_2`, `timeout.step_3`); ON-029 prose uses singular ("the drain timeout"). Pick one and align. Recommend per-step (matches the pseudocode and is more granular). Estimated edit: ~2 sentences in ON-029.

7. **Concretize nominal-fixture bounds in §4.8.ON-032 criterion 1.** Replace "a few hundred open beads, a few dozen in-flight runs" with concrete numbers (e.g., "≤ 500 open beads, ≤ 50 in-flight runs, git-log depth ≤ 10,000 commits"). Named fixture enables the sensor; unnamed fixture defers the sensor. Estimated edit: 1 sentence.

8. **Add Axes lines to ON-025, ON-030, ON-049.** Per §8 template-conformance finding. Each carries external I/O and/or non-idempotent emission. Estimated edit: 3 lines.

9. **Fix ON-047/048/049 payload-field conflicts with EV.** Same pattern as BLOCKING #3 — ON names fields in EV's payload that EV doesn't declare. Either strip the field names from ON-048/049 and point to EV §8.4, or raise an EV amendment. Recommend strip. Estimated edit: 2 sentences in ON-048, 1 sentence in ON-049.

10. **Name the `harmonik list` daemon-discovery mechanism or open an OQ.** Three candidate mechanisms (pidfile scan of `$HOME/**/.harmonik/daemon.pid`; socket-directory scan; machine-level registry at `~/.harmonik/daemons.toml`). Pick one with a default, or open OQ-ON-008. Estimated edit: either 1 sentence in ON-041 or a new OQ block.

11. **ON-005 commit-hash computation source.** Per R1 critic Challenge 7 (unaddressed in v0.3). Add an ON-005a sub-requirement: "`actual_hash` MUST be computed from the build-time embedded ldflags stamp; binaries without an ldflags stamp MUST fail the integrity gate." Estimated edit: 1 new sub-requirement.

12. **ON-020(e) cross-reference PL-027(iii) for socket-rebind mechanism.** PL R2 landed the fd-passing mechanism; ON-020 should point there. Estimated edit: 1 clause in ON-020(e).

### POLISH — nice-to-have; not gating

13. **§6 subsections.** Add explicit `§6.1 Types — none (this spec introduces no persistent types); see [owning specs]` for discoverability. Or explicitly state subsection omissions.

14. **ON-INV-006 sensor persona assignment.** Name the reviewer persona ("critic" or "conformance-auditor" per AR §10.2 persona block) rather than "reviewer-enforced" generic.

15. **OQ-ON-005 split the bundled questions.** Separate "auto-escalate vs notify-only" from "fixture-tightening vs target-relaxation" into two OQs.

16. **OQ-ON-003 expand default** to name `flock(2)` advisory locking primitive and stale-lock recovery procedure.

17. **ON-037 default heartbeat cadence.** Name "10 seconds cadence, 3-missed tolerance" as the pre-inventory default so `no policy declared` is boot-safe.

18. **Tighten the structured-log versioning story in ON-035.** Add `log_schema_version` to the minimum shape and cross-reference ON-018 (N-1 applies to structured logs) or explicitly exempt structured logs from ON-018.

19. **§2.1a reference to build-practices.md is load-bearing.** Current prose says "subsystem specs SHOULD NOT design contracts that assume a pre-merge human review gate." This is directionally right but "design contracts that assume" is fuzzy. Tighten: "subsystem specs MUST NOT design runtime contracts that are only satisfiable under a pre-merge human review discipline (e.g., obligating human sign-off on individual commits as the enforcement surface)."

20. **ON-048 step 2 "post-step for wall-clock" cross-reference.** Name the EM requirement that defines "step" for wall-clock termination boundary.

---

**Estimated total edit volume to close the blocking + important items: ~80 lines of changes across §3, §4.2, §4.3, §4.9, §4.11, §6.5, §7.1, §8. All mechanical; no re-architecture. Once landed, advance to `reviewed`.**
