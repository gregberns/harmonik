# Workflow Modes — Implementation Tasks

Tasks below decompose the seven drafted spec deltas (see `05-changelog.md`) and the two integration-pass follow-ups (`06-integration.md` §7) into bead-sized work items. Each is implementable in one session (XS/S preferred; M only where indivisible).

The four PARA-prep beads already filed (`hk-cdb9f`, `hk-fx6zl`, `hk-7s9z9`, `hk-5zode`) are NOT duplicated here, but tasks that need run_id-keyed concurrency cleanliness depend on them.

---

## Task list (each becomes a bead)

### T-WM-001 — Add `WorkflowMode` enum + glossary types in core
- **Spec anchor:** `execution-model.md §6.1` (ENUM `WorkflowMode`), §3 glossary
- **Where:** `internal/core/` — new file `workflowmode.go` (+ test)
- **Deliverable:** Typed `WorkflowMode` enum with values `single`, `review-loop`, `dot`; constructor + validation + JSON marshal/unmarshal.
- **Acceptance:** Unit tests cover all three values; unknown value returns typed error; round-trip JSON preserves value.
- **Dependencies:** none
- **Size:** XS
- **Parallel-safe with:** T-WM-002, T-WM-003, T-WM-004, T-WM-005

### T-WM-002 — Add review-loop context-key constants in core
- **Spec anchor:** `execution-model.md §4.3 EM-012`
- **Where:** `internal/core/` — extend `run.go` or new `runcontextkeys.go`
- **Deliverable:** Exported string constants for the four reserved `Run.context` keys: `iteration_count`, `last_verdict`, `claude_session_id`, `last_diff_hash`.
- **Acceptance:** Constants compile; key names match spec text verbatim; godoc cites EM-012.
- **Dependencies:** none
- **Size:** XS
- **Parallel-safe with:** T-WM-001, T-WM-003, T-WM-004, T-WM-005

### T-WM-003 — Extend `Run` record with `workflow_mode` field
- **Spec anchor:** `execution-model.md §6.1`, EM-012
- **Where:** `internal/core/run.go` + `run_em012_test.go`
- **Deliverable:** Add immutable `WorkflowMode` field to `Run`; populate at construction; reject mutation post-claim.
- **Acceptance:** Existing run tests pass; new test confirms field set at claim time and round-trips via JSON.
- **Dependencies:** T-WM-001
- **Size:** S
- **Parallel-safe with:** T-WM-004, T-WM-005 (different files)

### T-WM-004 — Define seven new event types in core eventregistry
- **Spec anchor:** `event-model.md §6.3`, §8.1a, §8.8.6
- **Where:** `internal/core/eventreg_hqwn59.go` (or sibling file `reviewloopevents.go`) + payload types
- **Deliverable:** Register seven event types: `implementer_resumed`, `reviewer_launched`, `reviewer_verdict`, `iteration_cap_hit`, `no_progress_detected`, `review_loop_cycle_complete`, `bead_label_conflict`. Define payload structs per §6.3 schemas.
- **Acceptance:** Registry exposes new types; payload structs marshal to canonical JSON; class assignments (O/F) match §8.1a table.
- **Dependencies:** T-WM-001
- **Size:** M
- **Parallel-safe with:** T-WM-001..T-WM-003, T-WM-005

### T-WM-005 — Add optional `workflow_mode` field to run-lifecycle event payloads
- **Spec anchor:** `event-model.md §8.1` payload-field rule extension
- **Where:** `internal/core/runstartedpayload.go`, `runterminalpayload.go`
- **Deliverable:** Optional `WorkflowMode` field on `run_started`, `run_completed`, `run_failed` payload structs; `omitempty` JSON tag.
- **Acceptance:** Existing payload tests still pass; new test confirms field is omitted when zero, emitted when set.
- **Dependencies:** T-WM-001
- **Size:** XS
- **Parallel-safe with:** T-WM-002, T-WM-003, T-WM-004

### T-WM-006 — Extend `LaunchSpec` with four optional review-loop fields
- **Spec anchor:** `handler-contract.md §6.1`, HC-006
- **Where:** `internal/handlercontract/launchspec_hc006.go` + `launchspec_hc006_test.go`
- **Deliverable:** Add optional fields `workflow_mode`, `phase` (enum: `implementer-initial`, `implementer-resume`, `reviewer`), `iteration_count`, `claude_session_id`. Add validation: `phase` and `iteration_count` either both present or both absent; `claude_session_id` present iff `phase = implementer-resume`.
- **Acceptance:** Unit tests cover each field's presence/absence rule; HC-006 contract test pins JSON shape.
- **Dependencies:** T-WM-001
- **Size:** S
- **Parallel-safe with:** T-WM-007 (different file)

### T-WM-007 — Implement conditional 4-tuple idempotency key in handler
- **Spec anchor:** `handler-contract.md §4.2 HC-004`
- **Where:** `internal/handlercontract/handler_hc001.go` (idempotency key derivation) + new test file
- **Deliverable:** Idempotency key picker: returns `(run_id, node_id)` when `phase`/`iteration_count` absent; returns `(run_id, node_id, phase, iteration_count)` when both present.
- **Acceptance:** Test confirms both shapes; concurrent-launch test confirms same key returns same Session; distinct `(phase, iteration)` produce distinct keys.
- **Dependencies:** T-WM-006
- **Size:** S
- **Parallel-safe with:** single track on `handler_hc001.go`

### T-WM-008 — Add `workflow_mode_default` daemon config field
- **Spec anchor:** `process-lifecycle.md §4.1 PL-004a`, PL-ENV-001(e), PL-005 step 0
- **Where:** `internal/daemon/daemon.go` (config struct) + project-config loader
- **Deliverable:** Daemon-startup config value `workflow_mode_default` (default `single`); read once at PL-005; immutable for daemon lifetime; surfaced via accessor for the claim path.
- **Acceptance:** Test: setting config to `review-loop` is observable via accessor; unknown value rejected at startup.
- **Dependencies:** T-WM-001
- **Size:** S
- **Parallel-safe with:** T-WM-009 (config vs. claim path)

### T-WM-009 — Implement mode-resolution precedence in claim path
- **Spec anchor:** `execution-model.md §4.3 EM-012a`
- **Where:** `internal/daemon/workloop.go` (claim path) + new file `moderesolve.go`
- **Deliverable:** At claim time, resolve `workflow_mode` in order: per-bead `workflow:<mode>` label → project config (reserved no-op) → daemon default → `single`. Store resolved mode in Run record (immutable).
- **Acceptance:** Table-driven test covers all four precedence tiers; resolved value matches expected for each combination.
- **Dependencies:** T-WM-003, T-WM-008, T-WM-010 (label reader)
- **Size:** S
- **Parallel-safe with:** T-WM-011, T-WM-012

### T-WM-010 — Beads adapter: surface `workflow:<mode>` labels on ready-work
- **Spec anchor:** `beads-integration.md §4.3 BI-009a`, BI-013 amendment
- **Where:** `internal/brcli/ready.go`, `listbystatus_em031a.go`, `show.go` (response types)
- **Deliverable:** Ready-work and show queries return labels in their response payloads; expose `workflow:<mode>` labels to callers.
- **Acceptance:** Integration test against `br` (or fixture) confirms labels surface; existing ready-work tests unaffected.
- **Dependencies:** none (br already exposes labels — wiring through adapter)
- **Size:** S
- **Parallel-safe with:** T-WM-011, T-WM-012

### T-WM-011 — Beads adapter: exclude `needs-attention` from ready-work
- **Spec anchor:** `beads-integration.md §4.5 BI-013a`
- **Where:** `internal/brcli/ready.go` + `ready_test.go`
- **Deliverable:** Ready-work query filters out beads carrying `needs-attention` label, even when `status = open`.
- **Acceptance:** Test: bead with `status=open` + `needs-attention` label is excluded; same bead without label is included.
- **Dependencies:** T-WM-010
- **Size:** S
- **Parallel-safe with:** T-WM-012

### T-WM-012 — Detect multi-`workflow:`-label conflict; emit `bead_label_conflict`
- **Spec anchor:** `beads-integration.md §4.3 BI-009a`, `event-model.md §8.8.6`
- **Where:** `internal/brcli/` (new file `workflowlabelconflict.go`) consumed by daemon claim path
- **Deliverable:** Helper detects >1 `workflow:<mode>` label on a bead; emits `bead_label_conflict` event; daemon falls back to next-precedence tier on conflict.
- **Acceptance:** Unit test: two labels emit one conflict event; falls back deterministically; structured-log fallback when bus unavailable.
- **Dependencies:** T-WM-004, T-WM-010
- **Size:** S
- **Parallel-safe with:** T-WM-011

### T-WM-013 — Reject agent writes to `workflow:<mode>` labels
- **Spec anchor:** `beads-integration.md §4.3 BI-010c`
- **Where:** `internal/brcli/terminaltransition_bi010.go` (or shared write-discipline guard)
- **Deliverable:** Adapter write-paths refuse any `br update` carrying a `workflow:<mode>` label mutation from an agent path; daemon-as-orchestrator path bypass allowed.
- **Acceptance:** Test: agent-context write with `workflow:single` label returns typed error; daemon-context write succeeds.
- **Dependencies:** none
- **Size:** S
- **Parallel-safe with:** T-WM-010, T-WM-011, T-WM-012

### T-WM-014 — Workspace: extend `.gitignore` hygiene for review-loop artifacts
- **Spec anchor:** `workspace-model.md §4.5 WM-013e`
- **Where:** `internal/workspace/gitignorehygiene.go` + `gitignorehygiene_wm013e_test.go`
- **Deliverable:** `.gitignore` template / enforcer includes `.harmonik/review.json` and `.harmonik/review.iter-*.json`.
- **Acceptance:** Test confirms both patterns present in generated `.gitignore`; existing entries preserved.
- **Dependencies:** none
- **Size:** XS
- **Parallel-safe with:** T-WM-015, T-WM-016

### T-WM-015 — Review-verdict file: writer schema validator (daemon-side reader)
- **Spec anchor:** `workspace-model.md §4.7 WM-027a`, `event-model.md §8.1a.3`
- **Where:** `internal/workspace/` — new file `reviewverdict.go` + test
- **Deliverable:** Reader: opens `${workspace_path}/.harmonik/review.json`, validates against `agent-reviewer` JSON schema v1 (`schema_version`, `verdict`, `flags[]`, `notes`). Returns typed verdict struct or `ErrMalformed`.
- **Acceptance:** Valid file parses; missing field, unknown verdict, schema_version mismatch each return ErrMalformed; tests cover the three rejection paths.
- **Dependencies:** none
- **Size:** S
- **Parallel-safe with:** T-WM-014, T-WM-016

### T-WM-016 — Review-verdict archive rotation
- **Spec anchor:** `workspace-model.md §4.7 WM-027a`
- **Where:** `internal/workspace/reviewverdict.go` (same file as T-WM-015) — separate function
- **Deliverable:** Function `ArchiveVerdict(workspacePath, iterationN)` renames `.harmonik/review.json` to `.harmonik/review.iter-<N>.json` before next iteration. Atomic rename.
- **Acceptance:** Test: archive places file at correct path; double-archive at same N returns error; absent source returns ErrNotFound.
- **Dependencies:** T-WM-015 (same file — serialize)
- **Size:** XS
- **Parallel-safe with:** T-WM-014 (different file)

### T-WM-017 — Diff-hash no-progress detector
- **Spec anchor:** `execution-model.md §4.3 EM-015e` (no-progress detector)
- **Where:** `internal/workspace/` — new file `diffhash.go` + test
- **Deliverable:** `ComputeDiffHash(worktreePath, parentSHA, headSHA) -> string` returns SHA-256 of `git diff <parent>..<head>` output. Pure function over git output.
- **Acceptance:** Test against fixture worktrees: identical diff produces identical hash; one-line diff produces different hash; empty diff is non-error.
- **Dependencies:** none
- **Size:** S
- **Parallel-safe with:** T-WM-014, T-WM-015, T-WM-018

### T-WM-018 — Capture Claude session_id from initial implementer launch
- **Spec anchor:** `execution-model.md §4.3 EM-015d` (capture clause), `handler-contract.md §4.1`
- **Where:** `internal/handlercontract/session.go` — parse `claude -p ... --output-format json` output
- **Deliverable:** On `phase = implementer-initial` launch, parse Claude Code session identifier from subprocess output and surface it on the handler's `Session` result.
- **Acceptance:** Test with stubbed claude output (`--output-format json` fixture) extracts session id; missing id returns typed error.
- **Dependencies:** T-WM-006
- **Size:** S
- **Parallel-safe with:** T-WM-017, T-WM-019

### T-WM-019 — LaunchSpec construction: assemble review-loop fields
- **Spec anchor:** `handler-contract.md §6.1 HC-006`, `execution-model.md §4.3 EM-015d`
- **Where:** `internal/daemon/workloop.go` (or new helper `internal/daemon/launchspecbuild.go`)
- **Deliverable:** When dispatching a review-loop run, build LaunchSpec with `workflow_mode`, `phase`, `iteration_count`, and `claude_session_id` (when resuming). Three call sites: implementer-initial, implementer-resume, reviewer.
- **Acceptance:** Three table-driven cases produce LaunchSpec with correct field shape per HC-006 rules.
- **Dependencies:** T-WM-006, T-WM-003
- **Size:** S
- **Parallel-safe with:** T-WM-020

### T-WM-020 — Daemon work-loop: review-loop dispatch driver (core)
- **Spec anchor:** `execution-model.md §4.3 EM-015d`, EM-015e
- **Where:** `internal/daemon/` — new file `reviewloop.go`
- **Deliverable:** Mode-specific driver: claim → spawn implementer (initial) → wait for outcome → archive prior verdict (if any) → spawn reviewer → wait for verdict file → parse verdict → route (APPROVE/REQUEST_CHANGES/BLOCK/cap/no-progress) → resume implementer or close. Single `run_id` across the cycle. Increments `iteration_count` before each implementer dispatch after the first. Honors PARA-1 run_id-keyed event emission.
- **Acceptance:** Twin smoke test (success path → APPROVE; one REQUEST_CHANGES then APPROVE; cap-hit). Driver records `last_verdict`, `claude_session_id`, `last_diff_hash` in `Run.context`. See T-WM-026, T-WM-027.
- **Dependencies:** T-WM-003, T-WM-009, T-WM-015, T-WM-016, T-WM-017, T-WM-018, T-WM-019, T-WM-021, T-WM-022, hk-cdb9f (PARA-1)
- **Size:** M
- **Parallel-safe with:** single track (new file, but the only large piece)

### T-WM-021 — Iteration-cap enforcement + cap-hit termination path
- **Spec anchor:** `execution-model.md §4.3 EM-015e` (cap clause)
- **Where:** `internal/daemon/reviewloop.go` — extracted helper or inline
- **Deliverable:** Cap = 3 (constant). When `REQUEST_CHANGES` arrives at `iteration_count = 3`, emit `iteration_cap_hit` then terminate via `needs-attention` close path.
- **Acceptance:** Test: forced three `REQUEST_CHANGES` verdicts → `iteration_cap_hit` event present; bead's terminal transition carries `needs-attention` label.
- **Dependencies:** T-WM-020, T-WM-023, T-WM-024
- **Size:** S
- **Parallel-safe with:** T-WM-022 (same file — serialize after T-WM-020 lands)

### T-WM-022 — No-progress termination path
- **Spec anchor:** `execution-model.md §4.3 EM-015e` (no-progress clause)
- **Where:** `internal/daemon/reviewloop.go`
- **Deliverable:** Before launching reviewer from iteration 2 onward, compare current diff hash to `Run.context.last_diff_hash`. If equal, emit `no_progress_detected` and terminate via `needs-attention` BEFORE launching reviewer.
- **Acceptance:** Test with fixture: identical implementer output across iterations triggers `no_progress_detected` and skips reviewer; bead closes with `needs-attention`.
- **Dependencies:** T-WM-017, T-WM-020
- **Size:** S
- **Parallel-safe with:** T-WM-021 (same file — serialize)

### T-WM-023 — Apply `needs-attention` label on terminal-transition write
- **Spec anchor:** `execution-model.md §4.3 EM-015e`, `operator-nfr.md §4.3 ON-009a`
- **Where:** `internal/brcli/terminaltransition_bi010.go`
- **Deliverable:** Terminal-transition writer accepts a `NeedsAttention bool` flag; when true, applies `needs-attention` label on bead close.
- **Acceptance:** Test: terminal write with flag=true → bead has label after write; flag=false → no label.
- **Dependencies:** T-WM-013 (so the label add does NOT trip the workflow-label guard)
- **Size:** S
- **Parallel-safe with:** T-WM-024

### T-WM-024 — Emit `review_loop_cycle_complete` on cycle termination
- **Spec anchor:** `event-model.md §8.1a.6`, `execution-model.md §4.3 EM-015e`
- **Where:** `internal/daemon/reviewloop.go`
- **Deliverable:** Emit exactly one `review_loop_cycle_complete` (class F) carrying `completion_reason ∈ {approved, cap_hit, blocked, no_progress, error}` before the run's terminal `run_completed`/`run_failed`.
- **Acceptance:** Test confirms event present exactly once per cycle for all five termination paths; ordering test confirms it precedes terminal run event.
- **Dependencies:** T-WM-004, T-WM-020
- **Size:** S
- **Parallel-safe with:** T-WM-023 (different files)

### T-WM-025 — Emit per-iteration review-loop events with run_id
- **Spec anchor:** `event-model.md §8.1a` (all six event emissions)
- **Where:** `internal/daemon/reviewloop.go`, helpers in `internal/eventbus/`
- **Deliverable:** Wire emissions for `implementer_resumed`, `reviewer_launched`, `reviewer_verdict`, `iteration_cap_hit`, `no_progress_detected` at the correct points in the driver. Use `EmitWithRunID` (PARA-1, commit 87cd69a) so each event carries `run_id`.
- **Acceptance:** End-to-end test: cycle with one REQUEST_CHANGES + one APPROVE emits the expected ordered sequence; all events carry the same `run_id`; reviewer_verdict payload conforms to agent-reviewer schema v1.
- **Dependencies:** T-WM-004, T-WM-015, T-WM-020, hk-cdb9f (PARA-1)
- **Size:** S
- **Parallel-safe with:** T-WM-024 (same file — serialize after T-WM-020)

### T-WM-026 — Smoke test: review-loop happy path (APPROVE iter 1)
- **Spec anchor:** problem-space success criterion 6
- **Where:** `internal/daemon/reviewloop_smoke_test.go` (new file)
- **Deliverable:** End-to-end twin-handler smoke test. Stub implementer writes file; stub reviewer writes `review.json` with `verdict=APPROVE`; assert run terminates with `outcome.status=SUCCESS`, no `needs-attention` label, `review_loop_cycle_complete.completion_reason=approved`.
- **Acceptance:** Test green; event log captures full ordered sequence.
- **Dependencies:** T-WM-020, T-WM-024, T-WM-025
- **Size:** M
- **Parallel-safe with:** T-WM-027 (same package, different file)

### T-WM-027 — Smoke test: review-loop REQUEST_CHANGES → APPROVE (iter 2)
- **Spec anchor:** problem-space success criterion 7
- **Where:** `internal/daemon/reviewloop_smoke_test.go` (new file or same as T-WM-026)
- **Deliverable:** Stub reviewer returns `REQUEST_CHANGES` iter 1 then `APPROVE` iter 2; assert implementer is resumed with `claude --resume <id>`; verdict file from iter 1 archived to `review.iter-1.json`; final terminal events correct.
- **Acceptance:** Test green; archive file exists; resume command observed.
- **Dependencies:** T-WM-020, T-WM-016, T-WM-018, T-WM-026 (if same file)
- **Size:** M
- **Parallel-safe with:** T-WM-026 if split into different files; otherwise serialize

### T-WM-028 — `harmonik status` inline review-loop iteration state
- **Spec anchor:** `operator-nfr.md §4.3 ON-035a`
- **Where:** `internal/operatornfr/` or the status command wherever rendered
- **Deliverable:** When a run is in review-loop mode, `harmonik status` renders `iteration_count`, `last_verdict`, current phase inline. No new subcommand.
- **Acceptance:** Snapshot test: status output for a review-loop run includes the three fields; single-mode run output unchanged.
- **Dependencies:** T-WM-003
- **Size:** S
- **Parallel-safe with:** all (separate package)

### T-WM-029 — Operator config inventory: workflow_mode + cap value
- **Spec anchor:** `operator-nfr.md §4.1 ON-004a`
- **Where:** `internal/operatornfr/` (config-inventory enumeration)
- **Deliverable:** Inventory output lists `workflow_mode` with its four-tier precedence and the iteration-cap value (3, hardcoded).
- **Acceptance:** Snapshot test of inventory includes both rows.
- **Dependencies:** T-WM-008
- **Size:** XS
- **Parallel-safe with:** T-WM-028

### T-WM-030 — Sidecar metadata: carry resolved `workflow_mode`
- **Spec anchor:** `beads-integration.md §4.4 BI-020` amendment
- **Where:** `internal/workspace/sessionmetadatasidecar_wm063.go`
- **Deliverable:** Sidecar metadata MAY include `workflow_mode`; populate when daemon writes sidecar.
- **Acceptance:** Test: sidecar of a review-loop run contains `workflow_mode=review-loop`; single-mode sidecar omits or carries `single`.
- **Dependencies:** T-WM-003
- **Size:** XS
- **Parallel-safe with:** T-WM-014, T-WM-028, T-WM-029

### T-WM-031 — Cite-fix: re-target ON-009a's handler-contract citation
- **Spec anchor:** `06-integration.md` §7 follow-up T-INT-1
- **Where:** `specs/operator-nfr.md` ON-009a (post-finalize) — record now as a task against the draft `05-spec-drafts/operator-nfr.md`
- **Deliverable:** Replace ON-009a's `[handler-contract.md §4.2 HC-006]` cite with `[workspace-model.md §4.7 WM-027a]` + `[event-model.md §8.1a.3]`.
- **Acceptance:** Cite updated; cross-ref audit re-run shows no remaining WEAK rows.
- **Dependencies:** none
- **Size:** XS
- **Parallel-safe with:** T-WM-032

### T-WM-032 — Reconciliation scope-out note for `needs-attention`
- **Spec anchor:** `06-integration.md` §7 follow-up T-INT-2
- **Where:** `specs/reconciliation/spec.md` (scope-out section or Cat 6 enumeration)
- **Deliverable:** One-line clarifying note: `needs-attention`-labeled closed beads are NOT a reconciliation surface (they are a BI-013a dispatch-filter surface).
- **Acceptance:** Line present; reconciliation spec still passes existing tests.
- **Dependencies:** none
- **Size:** XS
- **Parallel-safe with:** T-WM-031

---

## Dependency graph

```
T-WM-001 (WorkflowMode enum)
 ├── T-WM-003 (Run.workflow_mode)
 │    ├── T-WM-009 (mode resolution)
 │    ├── T-WM-019 (LaunchSpec build)
 │    ├── T-WM-020 (review-loop driver) ──┬── T-WM-021 (cap)
 │    ├── T-WM-028 (status render)        ├── T-WM-022 (no-progress)
 │    └── T-WM-030 (sidecar)              ├── T-WM-024 (cycle_complete)
 ├── T-WM-004 (event types)               └── T-WM-025 (per-iter events)
 │    ├── T-WM-012 (label conflict)             │
 │    ├── T-WM-024                              ├── T-WM-026 (smoke approve)
 │    └── T-WM-025                              └── T-WM-027 (smoke iter-2)
 ├── T-WM-005 (run-lifecycle event field)
 ├── T-WM-006 (LaunchSpec fields)
 │    ├── T-WM-007 (idempotency key)
 │    ├── T-WM-018 (session capture)
 │    └── T-WM-019
 └── T-WM-008 (daemon default config) ── T-WM-009, T-WM-029

T-WM-010 (label surface) ── T-WM-011 (needs-attn filter), T-WM-012, T-WM-009
T-WM-013 (label write guard) ── T-WM-023 (needs-attn writer)
T-WM-014, T-WM-015 ── T-WM-016 ── T-WM-020
T-WM-017 ── T-WM-022
hk-cdb9f (PARA-1) ── T-WM-020, T-WM-025
T-WM-031, T-WM-032 — independent (cite/note fixes)
```

**Max chain depth:** 6 (T-WM-001 → T-WM-006 → T-WM-018 → T-WM-020 → T-WM-025 → T-WM-027).

## Parallelization plan

**Batch A (parallel, foundational types) — no deps among themselves:**
T-WM-001, T-WM-002, T-WM-004, T-WM-005, T-WM-010, T-WM-013, T-WM-014, T-WM-017, T-WM-031, T-WM-032.

**Batch B (parallel, depend on Batch A):**
T-WM-003, T-WM-006, T-WM-008, T-WM-011, T-WM-012, T-WM-015, T-WM-023.

**Batch C (parallel, depend on Batch B):**
T-WM-007, T-WM-009, T-WM-016, T-WM-018, T-WM-028, T-WM-029, T-WM-030.

**Batch D (mostly serial — driver + dependents):**
T-WM-019 → T-WM-020 → {T-WM-021, T-WM-022, T-WM-024, T-WM-025 — serialize same-file} → {T-WM-026, T-WM-027 — different test files, parallel-safe}.

## Coverage check

| Changelog entry (per spec) | Covered by |
|---|---|
| PL-004a (daemon default mode) | T-WM-008 |
| PL-ENV-001(e) extension | T-WM-008 |
| PL-005 step 0 amendment | T-WM-008 |
| PL-018 clarification | (spec-only; no code task — informative) |
| BI-009a (label encoding + conflict) | T-WM-010, T-WM-012 |
| BI-010c (write prohibition) | T-WM-013 |
| BI-013 (labels in ready payload) | T-WM-010 |
| BI-013a (needs-attention exclusion) | T-WM-011 |
| BI-020 (sidecar mode) | T-WM-030 |
| EM-012 (workflow_mode + context keys) | T-WM-002, T-WM-003 |
| EM-012a (precedence) | T-WM-009 |
| EM-015d (review-loop lifecycle) | T-WM-018, T-WM-019, T-WM-020, T-WM-025 |
| EM-015e (cap + early-exit + no-progress) | T-WM-017, T-WM-021, T-WM-022 |
| EM §6.1 WorkflowMode enum | T-WM-001 |
| EM glossary entries | T-WM-001, T-WM-002 |
| HC-003a (dispatch-level, not handler-selection) | (architectural — verified via T-WM-006 test that handler doesn't branch on field) |
| HC-004 (conditional 4-tuple key) | T-WM-007 |
| HC-006 (LaunchSpec fields) | T-WM-006 |
| HC §6.1 LaunchSpec record | T-WM-006 |
| EV §8.1a six events | T-WM-004, T-WM-024, T-WM-025 |
| EV §8.8.6 bead_label_conflict | T-WM-004, T-WM-012 |
| EV §8.1 optional workflow_mode field | T-WM-005 |
| EV §6.3 schemas | T-WM-004 |
| EV emission ordering rule | T-WM-024 (test asserts) |
| Reviewer-verdict schema-reuse | T-WM-015 (validator uses agent-reviewer schema) |
| WM-011 informative note | (spec-only; no code task) |
| WM-013a (one lease per run) | (no code change — existing impl already conforms) |
| WM-013e gitignore | T-WM-014 |
| WM-014 paragraph | (spec-only) |
| WM-027a review.json artifact + archive | T-WM-015, T-WM-016 |
| WM-030 session-log clarification | (spec-only) |
| WM §6.2 path-table rows | (spec-only; T-WM-014/15/16 implicitly cover) |
| ON-002 (no new exit-code categories) | (spec-only; T-WM-021/22 honor) |
| ON-004 / ON-004a (config inventory) | T-WM-029 |
| ON-008 amendment (pause checkpoint admission) | (spec-only) |
| ON-009a (drain discipline) | T-WM-011 + T-WM-023 |
| ON-013d (mode sealed at claim) | T-WM-003 (immutability) + T-WM-009 |
| ON-035a (status inline render) | T-WM-028 |
| Integration follow-up T-INT-1 | T-WM-031 |
| Integration follow-up T-INT-2 | T-WM-032 |

All changelog entries map to ≥1 task or are explicitly spec-only.

## Out-of-scope follow-ups

- **`dot` mode normative spec.** All seven drafts reserve the `dot` enum value but do not normatively specify dot-mode dispatch. Post-MVH; new kerf work.
- **Project-config tier of mode resolution.** EM-012a reserves "project config" as a no-op tier at MVH; future work to wire an actual project-config file.
- **Alternate no-progress detectors.** EM-015e §4.3 mentions Jaccard-on-files and Jaccard-on-hunks as post-MVH alternates via amendment.
- **Operator-tunable iteration cap.** ON-013d locks cap=3 for v1; tunability is a v2 candidate.
- **Per-mode handler skill packs.** HC-006 informative note mentions `agent-reviewer` skill for reviewer phase; skill-pack registry work tracked separately.
- **Harness fixtures for §8.1a events.** Integration pass §2 noted scenario-harness will eventually need fixtures for the six review-loop events; non-blocking for MVH.
- **Daemonization-era concurrent review-loop runs.** MVH ships foreground binary; concurrent runs already gated on PARA-* family; first post-MVH unlock per memory `project_harmonik_mvh_daemon_deferral`.
- **WM-027a (a) → handler-contract back-cite.** Tolerable asymmetry noted in integration §4; potential future hygiene pass.
