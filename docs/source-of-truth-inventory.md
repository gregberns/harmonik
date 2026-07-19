# Source-of-Truth Inventory: Subsystems vs. Normative Specs

*Last verified 2026-06-01 (second pass: `Done means...` added to plans 001–006, 008; no new subsystem drift found). Prior pass 2026-05-18 (post-77ae7ee handler-pause elevation + claude-hook-bridge normative confirmation). Re-run whenever a new subsystem is added under `internal/` or a spec is promoted/added under `specs/`.*

---

## 1. Source-of-Truth Hierarchy

`specs/` > `docs/` > code: a normative spec under `specs/` is always right; code is expected to match it. `docs/` is informational (not normative). Plans under `plans/` are pre-promotion drafts and are **non-normative** until promoted to `specs/`.

---

## 2. Subsystem Inventory Table

| Subsystem | Spec location | Status | Notes |
|---|---|---|---|
| `branching` | `specs/workspace-model.md` §4.2 WM-005b | CANONICAL | Package doc explicitly cites WM-005b. Branching config loader is governed by workspace-model. |
| `brcli` | `specs/beads-integration.md` §4.8 BI-024–026 | CANONICAL | Adapter, version-check, and terminal-transition routing are governed by beads-integration. |
| `core` | `specs/architecture.md` + `specs/execution-model.md` + `specs/event-model.md` + `specs/handler-pause.md` | CANONICAL | `core` is the shared type library; its types are individually owned by multiple foundation specs. Four-axis classification and mechanism/cognition tags apply per AR-001–004. `handlerpauseevents_ifqnj.go` and `eventreg_hqwn59.go` are governed by handler-pause.md (HP-NNN). No single "core subsystem spec" is needed; each type family cites its owning spec. |
| `daemon` | `specs/process-lifecycle.md` + `specs/handler-pause.md` | CANONICAL | process-lifecycle.md owns startup sequence, composition root, socket/pidfile layout, orphan sweep, and crash semantics. handler-pause.md (HP-NNN, elevated 2026-05-18 at 77ae7ee) owns handler-pause persistence, policy composition, and the work-loop dispatcher gate (`handlerpause_9hwbw.go`, `handlerpause_policy_37zy8.go`, `handlerpause_persist_m0k0a.go`). |
| `eventbus` | `specs/event-model.md` | CANONICAL | event-model.md owns the EventBus interface contract, consumer-class taxonomy, and durability classes (EV-001–063). |
| `handler` | `specs/handler-contract.md` | CANONICAL | handler-contract.md owns the handler launch interface, progress-stream wire protocol, session-id discipline, and twin-parity rules. |
| `handlercontract` | `specs/handler-contract.md` | CANONICAL | Go realization of handler-contract; package doc cites the spec explicitly. |
| `hookrelay` | `specs/claude-hook-bridge.md` | CANONICAL | hookrelay is the relay subprocess described in claude-hook-bridge.md §4.3. The spec owns its wire format and error codes. |
| `lifecycle` | `specs/process-lifecycle.md` + `specs/execution-model.md` | CANONICAL | Process lifecycle is in process-lifecycle.md; run/state/transition/checkpoint lifecycle is in execution-model.md. |
| `operatornfr` | `specs/operator-nfr.md` + `specs/control-points.md` | CANONICAL | operator-nfr.md owns attach-surface, drain sequence, restart RTO, upgrade contract, and ON-NNN requirements. control-points.md §6.3 governs the schema-compat window used in `schemacompatwindow_test.go`. |
| `queue` | `specs/queue-model.md` | CANONICAL | queue-model.md owns queue schema, group state machine, persistence layout, CLI dispatch shape, and append semantics. |
| `release` | `specs/beads-integration.md` §4.8 BI-024 | CANONICAL | Package holds the pinned Beads version constant; normatively governed by BI-024. No separate release spec needed. |
| `scenario` | `specs/scenario-harness.md` | CANONICAL | scenario-harness.md owns the declarative scenario file format, fixture lifecycle, twin-binary discovery, and SH-NNN requirements. |
| `specaudit` | `specs/architecture.md` | CANONICAL | specaudit tests enforce AR-NNN structural invariants on the spec corpus. It is a test-only enforcement package, not a runtime subsystem; architecture.md is its normative source. |
| `t5probe` | — | NON-NORMATIVE | Exploratory test probes only (package doc: "NOT part of the production build"). No spec needed; not a runtime subsystem. |
| `testhelpers` | `specs/process-lifecycle.md` §4.1 PL-004 | NON-NORMATIVE | Test scaffolding that materializes a `.harmonik/` sandbox per PL-004. Helper, not a runtime subsystem; no normative spec of its own is required. |
| `workspace` | `specs/workspace-model.md` | CANONICAL | workspace-model.md owns the worktree lifecycle, branch naming, lease protocol, session-log layout, interrupt-state, merge-back discipline, and WM-NNN requirements. |

---

## 3. GAP Analysis

All runtime subsystems map to a canonical spec. Two packages are declared non-normative:

| Package | Recommendation |
|---|---|
| `t5probe` | Declare non-normative (exploratory test probes; not a runtime subsystem). No spec needed. |
| `testhelpers` | Declare non-normative (test helper / internal-only). No spec needed beyond the PL-004 sandbox contract it already implements. |

### Gaps corrected in this pass (2026-05-18)

| Gap | Resolution |
|---|---|
| `specs/handler-pause.md` (HP prefix) not cited in inventory | Elevated to normative at 77ae7ee. Added to `daemon` and `core` rows. |
| `specs/control-points.md` (CP prefix) not cited in inventory | CP-049 implemented in `workflowvalidator`; §6.3 referenced in `operatornfr`. Added to both rows. |

### `_registry.yaml` gaps corrected in this pass (2026-05-18)

Three prefix reservations were missing from `specs/_registry.yaml` for specs with active requirement IDs. Added in the same commit:

| Prefix | Spec |
|---|---|
| `HP` | `specs/handler-pause.md` |
| `CHB` | `specs/claude-hook-bridge.md` |
| `QM` | `specs/queue-model.md` |

### Gaps corrected in second pass (2026-06-01, hk-ux915)

| Gap | Resolution |
|---|---|
| Plans 001–006, 008 missing `## Done means...` section | Added behavioral acceptance criteria to each `_plan.md`; plan 007 was covered by the first pass; plan 009 already had the section. |
| Remaining terminology drift scan | No new `MVH scope` / `bootstrap scope` / `Phase-1-only` drift found in `docs/`, `specs/`, or `plans/` beyond `docs/foundation/` and `docs/subsystems/` which use `MVH` as a historical milestone label (not a per-feature scope qualifier) — permitted under `AGENTS.md §"Terminology — avoid MVH"`. |

---

## 4. PARTIAL Coverage Notes

No subsystems are in PARTIAL status. The following nuances are worth noting:

- **`core`** — no single `specs/core.md` exists, and none is needed. The types in `internal/core` are individually owned by whichever foundation spec introduced them (execution-model, event-model, handler-contract, handler-pause, etc.). If `core` ever introduces a type family with no owning spec, that type family is a gap and needs either a new spec or an amendment to an existing one.

- **`lifecycle`** — straddles two specs by design: process-lifecycle governs the daemon process lifecycle; execution-model governs the run/state/transition lifecycle. This split is intentional and documented in execution-model.md §1 ("scope exclusions"). No ambiguity exists as long as both specs remain in sync on their shared vocabulary (daemon state machine, orphan sweep trigger).

- **`daemon`** (composition root) — process-lifecycle.md §1 notes that the CLI command surface and socket wire format are explicitly excluded and deferred. These aspects are currently undocumented. If `cmd/harmonik/` or a daemon-CLI spec is added post-MVH, it should either extend process-lifecycle.md or be filed as a new `specs/daemon-cli.md`.

---

## 5. How to Keep This in Sync

1. **New subsystem rule.** Every new `internal/<subsystem>/` package that is a runtime subsystem (per AR-016: a Go package inside the daemon binary) MUST either:
   - Map to an existing spec (add a row to this table citing the spec and requirement prefix), OR
   - Have a new `specs/<subsystem>.md` written before (or in the same commit as) the package is added.

2. **Test-only / helper packages.** Packages that are test scaffolding, exploratory probes, or pure helpers (no envelope) MUST be explicitly declared non-normative in this table. Silence is not a valid declaration.

3. **Plans are not specs.** A `plans/NNN_*/` plan document is non-normative until its content is promoted to `specs/`. If a plan ships an implementation, the corresponding spec promotion MUST accompany the implementation commit.

4. **Spec registry.** When a new `specs/<name>.md` is created for a runtime subsystem, its requirement prefix MUST be reserved in `specs/_registry.yaml` in the same commit (per AR conventions).

5. **Re-run this inventory** at every kerf pass advance, or run `internal/specaudit` tests (AR-016, AR-052) to catch structural drift automatically.
