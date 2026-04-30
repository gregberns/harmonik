# Harmonik Tasks

> Actionable work items. Grouped by phase. Check off as resolved or move to a log entry.
>
> Last updated: 2026-04-27 — **BI smoke-loaded into `.beads/` (66 beads, prefix `hk`, epic `hk-872`, zero cycles). Discipline at v0.4 (6 deltas F11–F16 applied). Pilot at v0.1.3. Pilot review protocol drafted at v0.1.** All 10 specs `reviewed`. AR pilot is the next concrete task.

## Spec corpus — Phase 0b (in progress)

### Batch 1 — REVIEWED (patch-bumped by citation cleanup)

- [x] **`specs/architecture.md`** (AR, 644 lines, v0.3.1 reviewed).
- [x] **`specs/execution-model.md`** (EM, 1093 lines, v0.3.2 reviewed).
- [x] **`specs/event-model.md`** (EV, 1034 lines, v0.3.2 reviewed).
- [x] **`specs/handler-contract.md`** (HC, 951 lines, v0.3.2 reviewed).
- [x] **`specs/control-points.md`** (CP, 1126 lines, v0.3.2 reviewed).

### Batch 2 — progress this session

- [x] **`specs/workspace-model.md`** (WM, 1242 lines, v0.4.1 **reviewed**). Full 2-round + 2 integrations + citation-cleanup patch. WM IDs FROZEN.
- [x] **`specs/process-lifecycle.md`** (PL, 877 lines, v0.4.0 **reviewed**). R2 integration: fd-passing on exec-upgrade; rlimit-derived ceiling; new PL-002b/003b/009b/025a; PL-011 mechanical predicate; PL-005 step 3a socket-bind ordering; OQ-PL-005 RESOLVED. PL IDs FROZEN.
- [x] **`specs/operator-nfr.md`** (ON, 992 lines, v0.4.0 **reviewed**). R2 integration: in_flight(run) lowercased per EM glossary; codes 22/23 absorbed; new ON-005a/013a/013c/020a/027a/030a/050/051/053/054; ON-027 grew step 3a (now 8 steps); ON-022 redactor fail-closed; ON-040 drain-forced silent-hang synthesis. ON IDs FROZEN.
- [x] **`specs/reconciliation/{spec,schemas}.md`** (RC, 991+240 lines, v0.4.0 **reviewed/supplement**). R2 integration: spec-category fix to foundation-cross-cutting; RC-019a/RC-014/RC-INV-004 reframed against EV-023a actual semantics; ON-008 fabrication fix; BI §4.10 cite migration; RC-002a lock primitive migrated to disk-based flock; new RC-002b/003b/012a/015a/020b/022a/025a/026a; §8.4a Cat 3a aligned with BI-031 status-check protocol. RC IDs FROZEN.
- [x] **`specs/beads-integration.md`** (BI, 810 lines, v0.4.0 **reviewed**). R2 integration: fabricated EV constructs replaced with `divergence_inconclusive` and structured-log emissions; BI-031 step 3 disambiguation via audit-log + step 4 error-handling branch; BI-030 full atomicity (temp+rename+fsync(parent_dir) on create AND delete); BI-025c subprocess termination; new BI-014a (orphan `br` sweep) and BI-025e (concurrent invocation); BI-024a step number corrected (4 not 6); BI-010a failure_class enum aligned to EM §8; §6.1 IntentLogEntry gained `intended_post_state`. BI IDs FROZEN.

### Cross-cutting cleanup — COMPLETE

- [x] **Cross-spec citation drift cleanup.** Two passes: pass 1 migrated `architecture.md §1.N → §4.N` (57 sites) + `handler_type → agent_type` rename (7 sites). Pass 2 migrated ~145 more cites across 7 files (EV `§3.N`, WM `§5.N`, ON `§7.N`, PL `§8.N`, BI `§10.N`, CP misnumbered `§6.N`) + fixed reconciliation multi-file path form. Each batch-2 R1 integration also cleaned its own outbound cites.
- [x] **`handler_type` → `agent_type` rename** (AR-MIG-001 complete).
- [ ] **`depended-on-by` reverse index** — removed from front matter v1.1; computation tool not built. Deferred post-MVH.

### v0.4.x cross-spec coordination patch wave — LANDED 2026-04-25

- [x] **PL → v0.4.1** — 9 items (PL-INTERIM dropped on 22/23; daemon_instance_id UUIDv7; pidfile line 3; PL-009/PL-011a monotonic fields; PL-005 step 8a marker reads; get-agent-count RPC; PL-006 orphan sweep for `br` + reconciliation-locks).
- [x] **EV → v0.3.3** — 7 new event types (§8.6.11–14, §8.7.16–17, §8.8.5); daemon_shutdown class F confirmed (resolves OQ-PL-012); monotonic-companion fields; daemon_degraded enum exhaustive; divergence_kind post-MVH note.
- [x] **EM → v0.3.3** — EM-005a + Outcome.kind discriminator + OutcomeKind enum (resolves OQ-RC-010); 2 RC-owned trailers added (resolves OQ-RC-002).
- [x] **WM → v0.4.2** — WM-036 verdict-disposition `no-op-accept` row (resolves OQ-RC-011).
- [x] **HC → v0.3.3** — HC-016a orphan-reconnect retry; HC-026b drain-forced silent-hang acceptance.
- [x] **ON → v0.4.1** — OQ-RC-009 resolution acknowledgment (decline normative `quarantined` state at MVH).

All §12 revision-history rows added; all spec IDs FROZEN; net new IDs (EM-005a, HC-016a, HC-026b) minted in pre-existing gaps.

### Decompose-to-tasks pilot — LANDED 2026-04-25 / 2026-04-26 / 2026-04-27

- [x] **`docs/decompose-to-tasks/discipline.md` v0.4** (was v0.3) — 6 deltas F11–F16: step→umbrella implicit via parent-child; sensor↔impl one-way; bidirectional inline cite disambiguation; mnemonic vs Beads-assigned IDs (zsh implementation pattern); default priority P2 accepted; corpus prefix `hk` single DB (new §2.12). Vocabulary fully matches live `br` v0.1.45.
- [x] **`docs/decompose-to-tasks/bi-pilot.md` v0.1.3** (was v0.1.2) — 5 deltas F-pilot-1..5: §7 tally fix (40 first-class req beads, total 66); removed bidirectional `bi-004 ↔ bi-027`; removed wrong-direction impl→sensor edges; removed redundant step→umbrella edges; added `bi-schema.harmonik-write-status` per BI v0.4.1.
- [x] **`docs/decompose-to-tasks/bi-smoke-load-findings.md` v0.1** — full report of 2026-04-27 smoke load surfacing the 11 findings.
- [x] **`docs/decompose-to-tasks/pilot-review-protocol.md` v0.1** — 3-reviewer parallel pass (Coverage / Decomposition-quality / Reference) + synthesis (BLOCKER / MAJOR / MINOR) + load gate. Gates every remaining pilot.
- [x] **BI smoke-load** completed twice. First run under `--prefix bi` surfaced 5 cycle bugs (now fixed in v0.4 + v0.1.3). Second run under `--prefix hk` clean: 66 beads, 110 edges, zero cycles. State preserved in `<repo>/.beads/`.
- [x] **`br` (Beads CLI) installed** at `/Users/gb/.local/bin/br` v0.1.45.
- [x] **`.beads/` added to `.gitignore`** at MVH (regenerable from JSONL).
- [x] **BI → v0.4.1 (status-enum reconciliation per reviewer F2).** Unchanged this session.

### Decompose-to-tasks — REMAINING

- [ ] **Draft AR pilot** at `docs/decompose-to-tasks/ar-pilot.md` against `specs/architecture.md` (52 reqs, mostly declarations). Use `bi-pilot.md` v0.1.3 as the structural template.
- [ ] **Run 3-reviewer protocol on AR pilot** per `pilot-review-protocol.md` §6 (Task tool, three parallel subagents). Apply BLOCKER and MAJOR findings to the pilot before loading.
- [ ] **Load AR into existing `.beads/`** (no `br init` — append to current DB; epic for AR will be a new top-level `hk-<suffix>`). Verify `br dep cycles` clean (covers BI + AR union).
- [ ] **Scale pilot pass to remaining 8 specs**: EM → EV → HC → CP → WM → PL → ON → RC. RC second-to-last because it's the first to exercise discipline §2.11 (multi-file, retired IDs, large §8 taxonomy).
- [ ] **Cross-spec cycle check across the union** — single `br dep cycles` after all 10 loads. Single DB makes this trivial.
- [ ] **Patch discipline / protocol** if the AR or subsequent passes surface bug classes the v0.1 protocol missed.

### Decompose-to-tasks — DEFERRED

- [ ] **Ingestion-validation-requirements stub doc** (proposed mid-session 2026-04-27 but not written). Two-paragraph capture of "what harmonik's runtime task-ingestion validation will do." Cheap to write later; defer until nearer to implementation.

### Phase 1 implementation gate — pending

- [ ] Loaded bead set → readiness workflow (per memory: loaded beads start in `parked`; readiness workflow promotes them to dispatchable).
- [ ] Begin implementation.

## Spec template + tooling (complete)

- [x] Spec template at `docs/foundation/spec-template.md` v1.1 (594 lines).
- [x] Prefix registry at `specs/_registry.yaml` (10 reservations).
- [x] Foundation spec corpus copied to `docs/foundation/{problem-space.md, components.md}`.
- [x] OVERVIEW.md scannable positions doc.
- [x] core-scope.md walkthrough alignment record (10 sections).
- [x] 5 project-level docs at `docs/foundation/project-level/`.

## Review artifact census (2026-04-24)

| Spec | R1 reviews | R1 integration | R2 reviews | R2 integration | Status |
|---|---|---|---|---|---|
| AR | ✅ (3) | ✅ | ✅ (3) | ✅ | v0.3.1 reviewed |
| EM | ✅ (3) | ✅ | ✅ (3) | ✅ | v0.3.2 reviewed |
| EV | ✅ (3) | ✅ | ✅ (3) | ✅ | v0.3.2 reviewed |
| HC | ✅ (3) | ✅ | ✅ (3) | ✅ | v0.3.2 reviewed |
| CP | ✅ (3) | ✅ | ✅ (3) | ✅ | v0.3.2 reviewed |
| WM | ✅ (3) | ✅ | ✅ (3) | ✅ | v0.4.1 reviewed |
| PL | ✅ (3) | ✅ | ✅ (3) | ✅ | v0.4.0 reviewed |
| **ON** | ✅ (3) | ✅ | ✅ (3) | ✅ | **v0.4.0 reviewed** |
| **RC** | ✅ (3) | ✅ | ✅ (3) | ✅ | **v0.4.0 reviewed** |
| **BI** | ✅ (3) | ✅ | ✅ (3) | ✅ | **v0.4.0 reviewed** |

## Foundation kerf work — Round 2 (COMPLETE)

Round 2 landed 2026-04-24. (Prior content preserved — see git history for the 36-finding delta.)

## Phase 0: Refine the Plan (current)

The knowledge base captures decisions made so far, but several decisions are still open and several subsystems are out of date with the latest framing. Goal of this phase: get to a plan firm enough that bootstrap implementation can start.

### A. User decisions on bootstrap.md

[docs/bootstrap.md](docs/bootstrap.md) has explicit "Decisions needed" sections. Each one is a blocker for the next phase.

- [ ] **§2 MVH cut.** Is the proposed minimum viable harmonik the right scope?
- [ ] **§3 Workflow lifecycle.** Human gates, revision_loop retry cap, cycle granularity.
- [ ] **§4 Operator controls.** Stop default, queue compat contract, control interface, config scope.
- [ ] **§5 Build order.** Orchestrator before policy engine? Scenario stub timing? Twin format day 1?
- [ ] **§6 Risk specifics.** Regression baseline shape? Human review cadence?

### B. Refresh subsystems still on pre-2026-04-19 framing

(See prior TASKS.md entries — not updated this session.)
