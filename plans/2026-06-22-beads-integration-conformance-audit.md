# Beads Integration Spec-vs-Code Conformance Audit

**Spec version audited:** v0.7.0 (last-updated: 2026-06-21, header in `specs/beads-integration.md`)  
**Audit date:** 2026-06-22  
**Auditor:** Agent (bead hk-l06w4)  
**Iteration:** 2 (iter-1 corrections in §0)  
**Scope:** `specs/beads-integration.md` v0.7.0 vs `internal/brcli/` + daemon-side integration points (`internal/daemon/workloop.go`, `internal/daemon/daemon.go`, `internal/daemon/beadsmergedriver.go`, `internal/daemon/orphansweep.go`, `internal/lifecycle/orphansweepbr.go`, `internal/lifecycle/orphansweepbeads.go`, `cmd/harmonik/beadsmerge.go`, `.gitattributes`)  
**Note:** Epic hk-872 was scoped against v0.4.1. A dedicated delta section (§5) covers v0.4.1→v0.7.0 additions.

---

## 0. Iter-1 Corrections

Iter-1 contained two false-positive gaps that were based on insufficient search scope (only `internal/brcli/` and `internal/daemon/daemon.go` were searched; the `internal/lifecycle/` and `internal/daemon/orphansweep.go` files were not examined). Both are corrected here.

**False positive 1 — G3 (iter-1): "BI-031 startup intent-log scan not located"**  
The scan IS implemented. `ScanIntentLog` in `internal/lifecycle/orphansweepbeads.go:315` scans `.harmonik/beads-intents/` and classifies intent files into provenance/claims/mutations. `GCRetiredIntents` in `internal/lifecycle/orphansweep.go:441` performs the BI-031 step-3 check (ShowBead → if already at IntendedPostState → GC the intent file). Both are wired into `daemon/orphansweep.go` via `RunOrphanSweep`. **The real gap** is BI-031 step-4: re-driving stale pre-state intents at adapter startup is NOT implemented; non-landed intent files are retained for Cat 3a reconciliation rather than re-driven at startup. G3 is rewritten accordingly. Fix site is `lifecycle/` or `daemon/orphansweep.go` (NOT paul hard-hold).

**False positive 2 — G4 (iter-1): "BI-014a orphan br sweep not implemented"**  
`SweepOrphanBr` in `internal/lifecycle/orphansweepbr.go:109` is FULLY IMPLEMENTED: enumerates `br` processes re-parented to init (PPID==1) via `ps -eo pid,ppid,comm`, sends SIGTERM, waits 5s (`orphanSweepGracePeriod`), then SIGKILL — exactly matching the spec. Wired at `daemon/orphansweep.go:823`. G4 is removed; BI-014a is now CONFORMS.

---

## 1. Per-Section Conformance Table

### §4.1 — Beads Selection

| Req | Description | Code Location | Status | Note |
|-----|-------------|---------------|--------|------|
| BI-001 | SQLite fork adopted; no Dolt; no fork | `internal/brcli/adapter.go` (external binary only) | CONFORMS | No library link; `br` invoked as subprocess only |

### §4.2 — `br` CLI Access

| Req | Description | Code Location | Status | Note |
|-----|-------------|---------------|--------|------|
| BI-002 | All Beads interactions via `br` CLI | `internal/brcli/adapter.go` — sole `os/exec` site for `br` | CONFORMS | Enforced by `internal/specaudit/oninv006_no_control_surface_bypass_test.go` |
| BI-003 | `br serve` not used | No `br serve` invocation found | CONFORMS | — |
| BI-004 | Daemon invokes `br` directly; agents via Beads-CLI skill | Daemon uses `Adapter`; skill delivery is `handler-contract` scope | CONFORMS | Adapter is the sole daemon path; skill enforcement is handler-contract-owned |

### §4.3 — Beads-Managed Data

| Req | Description | Code Location | Status | Note |
|-----|-------------|---------------|--------|------|
| BI-005 | Beads owns bead content; no authoritative cache | `adapter.go:18-25` — explicitly no caching; `ShowBead` always invokes `br` | CONFORMS | — |
| BI-006 | Beads owns typed dependency edges | `show.go:161-199` — parses `dependencies`/`dependents` arrays | CONFORMS | — |
| BI-007 | Write-subset 5 values; read surface tolerates full enum; `draft` excluded at submit-time | `internal/core` — `CoarseStatus` extensible; `HarmonikWriteStatus` 5-value; `ready.go:183` excludes `needs-attention`; submit-time check in `workloop.go:1911+` | CONFORMS | `draft` excluded via `br ready` native exclusion + submit-time `br show` status check |
| BI-008 | Bead IDs stable | No ID mutation anywhere in adapter | CONFORMS | — |
| BI-008a | Bead-ID opaque; no parsing/minting/rewriting | `show.go`, `terminaltransition_bi010.go` — all use `core.BeadID` opaque type | CONFORMS | — |
| BI-009 | Atomic-claim semantics via `br update --claim` | `terminaltransition_bi010.go:202` — `br update <id> --claim` | CONFORMS | Delegates atomicity to `br` |
| BI-009a | `workflow:<mode>` label conflict detection | `workflowlabelconflict.go` — `DetectWorkflowLabelConflict`; bus nil path uses structured-log fallback | CONFORMS | Event bus wiring pending hk-872.57; ON-035 structured-log fallback active |
| BI-009b | `## Branching` section parse at claim time | `internal/daemon/workloop.go:2663` — `parseBranchingSection(beadRecord.Description)` | CONFORMS | `bead_body_parse_error` event emission not verified in this audit (daemon scope); parse errors treated as absent per BI-009b |

### §4.4 — Harmonik Write Surface

| Req | Description | Code Location | Status | Note |
|-----|-------------|---------------|--------|------|
| BI-010 | Writes only at terminal transitions | `terminaltransition_bi010.go` — `ClaimBead`, `CloseBead`, `ReopenBead`, `ResetBead`, `SweepCloseBead` | CONFORMS | — |
| BI-010a | Status-mapping table (all 6 trigger rows) | `terminaltransition_bi010.go` — all 4 ops; `SweepCloseBead` for Cat 3c | CONFORMS | `deferred`/`tombstone` submission rejected at submit-time via `workloop.go` |
| BI-010b | Reconciliation-driven writes route through adapter | `SweepCloseBead` in `terminaltransition_bi010.go:521` — Cat 3c path; serialized via `terminalMu` | CONFORMS | — |
| BI-010c | Agents MUST NOT add/remove `workflow:<...>` labels | `workflowlabelwrite_bi010c.go` — adapter-gate; Beads-CLI skill enforcement (skill scope) | CONFORMS | Skill-side documentation enforcement; skill body not audited here |
| BI-010d | `claim` as activity-marker write; `reset` op | `terminaltransition_bi010.go:390-504` — `ResetBead`; idempotency key `<project_hash>:<bead_id>:reset:<daemon_start_ns>` | CONFORMS | — |
| BI-010e | Child-bead-spawn `br create` (agent-issued) | Constraint on agents; no adapter enforcement needed (BI-011 covers it) | N-A | Enforcement is convention/skill-side; no adapter code required |
| BI-011 | Permitted/prohibited intra-run writes table | Adapter gates daemon writes; agent terminal writes documented as prohibited | CONFORMS | Failure contract acknowledged in spec; BL-MRG-004 (post-merge `br sync --import-only`) is a separate gap (see §2) |
| BI-012 | All terminal-transition writes via adapter | All ops route through `terminalTransitionWrite` or `SweepCloseBead` which hold `terminalMu` | CONFORMS | `internal/specaudit/oninv006_no_control_surface_bypass_test.go` enforces no bypass |

### §4.5 — Harmonik Read Surface

| Req | Description | Code Location | Status | Note |
|-----|-------------|---------------|--------|------|
| BI-013 | Ready-work query | `ready.go:95-100` — `br ready --sort priority --format json` | CONFORMS | Labels array surfaced per BI-009a |
| BI-013a | `needs-attention` rejected at submit-time | `ready.go:183-189` (read-time filter) + `workloop.go` submit-time label check | CONFORMS | Read-time filtering is additive (defense-in-depth); submit-time path is the normative gate |
| BI-013d | `br ready --sort priority` | `ready.go:67` — `brReadySortPriority = "priority"` constant; `ready.go:99` — pinned | CONFORMS | hk-rp48p regression fixed |
| BI-014 | Dependency-graph query | `listdependencies.go` | CONFORMS | — |
| BI-014a | Orphan `br` subprocess sweep on daemon startup | `internal/lifecycle/orphansweepbr.go:109` — `SweepOrphanBr`; `internal/daemon/orphansweep.go:823` — wired | CONFORMS | SIGTERM→5s grace (`orphanSweepGracePeriod`)→SIGKILL; PPID==1 filter via `ps -eo pid,ppid,comm`; survivors route to Cat 0. Iter-1 false positive — corrected. |
| BI-015 | Bead-detail query | `show.go:93` — `ShowBead` | CONFORMS | — |
| BI-016 | Reconciliation queries | `audit.go` — `AuditLog`; `listinflight.go` — `ListInFlightBeads`; `listbystatus_em031a.go` — `ListByStatus` | CONFORMS | — |

### §4.5a — Submit-Time Validation Read Surface

| Req | Description | Code Location | Status | Note |
|-----|-------------|---------------|--------|------|
| BI-013b | Submit-time bead read uses `br show` | `workloop.go:1911+` — `ShowBead` pre-claim read in queue path | CONFORMS | 5s read timeout applies via `RunWithTimeout` |
| BI-013c | Pre-claim status re-read | `workloop.go:1911-1976` — pre-claim guard; `bead_claim_skipped` event emitted at `workloop.go:1943-1954` | CONFORMS | FIX-SITE = paul hard-hold (workloop.go) — read-only observation only |

### §4.6 — Bead-ID Propagation

| Req | Description | Code Location | Status | Note |
|-----|-------------|---------------|--------|------|
| BI-017 | Run metadata records `bead_id` | execution-model scope (`internal/core` run types) | N-A | Out of brcli scope; execution-model owns run metadata |
| BI-018 | Checkpoint commits carry `Harmonik-Bead-ID` trailer | execution-model / workspace-model scope | N-A | Out of brcli scope |
| BI-019 | Bead-scoped events carry `bead_id` | event-model scope | N-A | Out of brcli scope |
| BI-020 | Session logs carry `bead_id` metadata | workspace-model scope | N-A | Out of brcli scope |

### §4.7 — Store-Authority Rules

| Req | Description | Code Location | Status | Note |
|-----|-------------|---------------|--------|------|
| BI-021 | Beads authoritative for content | `adapter.go:18-25` — no parallel cache; `ShowBead` always queries fresh | CONFORMS | `authoritybi021_test.go` validates |
| BI-022 | Git authoritative for completion | Cat 3 routing in reconciliation spec scope; adapter does not auto-reconcile | CONFORMS | Adapter provides write surface; reconciliation owns classification |
| BI-023 | JSONL observational only | No JSONL-driven writes in adapter | CONFORMS | — |

### §4.8 — Version-Pin + Adapter Layer

| Req | Description | Code Location | Status | Note |
|-----|-------------|---------------|--------|------|
| BI-024 | Beads version pinned | `internal/release/manifest.go:26` — `BeadsVersion = "0.1.45"`; exact-match in `CheckBrVersion` | CONFORMS | — |
| BI-025 | Single adapter module; injectable `br` binary path | `adapter.go` — sole translation layer; `New` / `NewForProject` constructors accept `brPath` | CONFORMS | — |
| BI-026 | Harmonik absorbs breakage; no forking | `breakage.go` — policy documented; adapter is single update point | CONFORMS | — |

### §4.8a — `br` CLI Surface Contract

| Req | Description | Code Location | Status | Note |
|-----|-------------|---------------|--------|------|
| BI-024a | `br --version` handshake at daemon startup (before first queue-submit) | `version.go:50` — `CheckBrVersion` exists; **NOT called explicitly at startup**; daemon.go:1148 comment says handshake done "lazily" on first br invocation | GAP | `CheckBrVersion` function exists and is correct but is not wired into daemon startup sequence; spec requires EXPLICIT handshake before first queue-submit RPC; severity: major |
| BI-025a | `br` exit-code → `BrError` taxonomy | `brerror.go` — `BrErrorFromExitCode`; 7-value enum | CONFORMS | `BrOther` emits structured-log (event bus pending hk-872.57) |
| BI-025b | `--format json` mandatory | `adapter.go:206-221` — `runFormatJSON` wrapper; carve-outs for `br --version` and `br audit log` documented | CONFORMS | — |
| BI-025c | Subprocess timeout discipline (5s read / 10s write; SIGTERM→SIGKILL) | `timeout.go` — `RunWithTimeout`; `sigtermGrace = 5s`; defaults `5s`/`10s`; HC-018 sequence | CONFORMS | `dblockretry.go` — `RunWithDBLockedRetry` retry discipline (3× for reads, 10× for terminal writes) |
| BI-025d | stderr capture: 1 MiB cap + truncation marker | `stderrcap.go` — `stderrCapWriter` struct + `StderrTruncationSuffix` defined; **NOT wired into `Run()` or `RunWithTimeout()`** | GAP | `adapter.go:160` and `timeout.go` both use raw `bytes.Buffer` for stderr; hk-872.31 tracks the wiring; `stderrCapWriter` is dead code in production paths; severity: minor |
| BI-025e | Terminal writes serialized via `terminalMu`; reads concurrent | `adapter.go:43-48` — `terminalMu sync.Mutex`; held in `ClaimBead`, `CloseBead`, `ReopenBead`, `ResetBead`, `SweepCloseBead` | CONFORMS | hk-hdbls fix |

### §4.8b — Bead-Ledger Merge Contract (BL-MRG)

| Req | Description | Code Location | Status | Note |
|-----|-------------|---------------|--------|------|
| BL-MRG-001 | `.gitattributes` and `.git/config` driver registration | `.gitattributes` has `merge=beads-merge`; `beadsmergedriver.go` registers `merge.beads-merge.driver`; spec requires attribute name `beads-union` | GAP | Naming mismatch: spec mandates `merge=beads-union` / `[merge "beads-union"]` in `.gitattributes` / `.git/config`; code uses `beads-merge` throughout; functionally equivalent (attribute name and config key match each other) but spec non-compliant; severity: minor |
| BL-MRG-002 | Union-by-ID algorithm with `updated_at` LWW; array field union | `cmd/harmonik/beadsmerge.go:196-271` — `mergeBeadRows`; `unionLabelsAndDeps` | CONFORMS | Union algorithm correct; labels + deps are set-unioned per spec |
| BL-MRG-003 | Semantic conflict logging format | `beadsmerge.go:418-436` — `appendConflictLog`; format: `<timestamp> bead=<id> reason=<str> current_at=<ts> other_at=<ts>` | GAP | Spec format: `<iso8601-timestamp> CONFLICT bead=<id> field=status a=<A_value> b=<B_value> resolution=took-ours`; code omits `CONFLICT` keyword, `field=status`, `a=`/`b=` status values, `resolution=took-ours`; severity: minor |
| BL-MRG-004 | `br sync --import-only` after any merge touching `.beads/issues.jsonl` | No `br sync --import-only` found anywhere; `syncflushonly.go` does `--flush-only` (export SQLite→JSONL, opposite direction) | GAP | `--flush-only` exports SQLite→JSONL; `--import-only` imports JSONL→SQLite; post-merge the union-merged JSONL must be imported back to SQLite before any subsequent `br` operations; spec says daemon MUST call this; without it, subsequent `br close` etc. operate on stale SQLite; severity: major |
| BL-MRG-005 | `mergeRebaseAutoResolveBeadsLedger` MUST be removed | `internal/daemon/workloop.go:5489, 5700, 5786-5853` — function still present and called at two sites | GAP | Spec says this function MUST be removed as it uses `git checkout --theirs .beads/issues.jsonl` which suppresses the registered merge driver; FIX-SITE = paul hard-hold (workloop.go); severity: major |
| BL-MRG-006 | Phase 2 shared-DB migration (informative) | N-A | N-A | Informative; not normative |

### §4.9 — Beads-CLI Skill

| Req | Description | Code Location | Status | Note |
|-----|-------------|---------------|--------|------|
| BI-027 | Beads-CLI skill is agent-facing access path | Skill location pending `control-points.md §4.11` bootstrap | N-A | OQ-BI-002 tracks path binding; skill existence not verified in this audit |
| BI-028 | Every agent has Beads-CLI skill by default | `handler-contract` scope | N-A | Out of brcli / daemon scope |

### §4.10 — `br`-Adapter Idempotency

| Req | Description | Code Location | Status | Note |
|-----|-------------|---------------|--------|------|
| BI-029 | Deterministic idempotency key `<run_id>:<transition_id>:<op>` | `terminaltransition_bi010.go:77` — `core.IdempotencyKey(runID, transitionID, op)` | CONFORMS | `ResetBead` uses separate formula per BI-010a note |
| BI-030 | Pre-write intent log with fsync durability (6-step protocol) | `intentlogwrite.go` — steps 1-4 via `WriteIntentLogTmp`, `RenameIntentLogTmpToFinal`, `FsyncIntentLogParentDir`; step 6 via `DeleteIntentLogAndSyncParent` | CONFORMS | Full temp+rename+fsync(temp)+fsync(dir) on create; unlink+fsync(dir) on delete |
| BI-031 | Crash-recovery via status-check-before-reissue on startup | **Partially implemented.** Step-3 (GC already-landed intents): `lifecycle/orphansweep.go:441` — `GCRetiredIntents` calls `ShowBead` to verify `IntendedPostState`, deletes landed files. Step-3b (scan + classify): `lifecycle/orphansweepbeads.go:315` — `ScanIntentLog`. **Step-4 (re-drive pre-state intents) NOT implemented**: non-landed intent files are retained for Cat 3a reconciliation rather than re-driven by the adapter at startup per spec requirement. | GAP | Spec requires MUST re-issue the `br` write at adapter startup when status is still pre-state; current code defers this to Cat 3a reconciliation; FIX-SITE = `lifecycle/orphansweepbeads.go` or new adapter function (NOT paul hard-hold); severity: major |
| BI-031b | `BrSchemaMismatch` → `divergence_inconclusive` emit | `classifyreconciliation_bi031b.go` — `BrErrReconciliationCategoryWithEmit`; `emitSchemaMismatchInconclusive` | CONFORMS | Structured-log fallback when bus nil |
| BI-032 | Intent log is Cat 3a detector evidence source | Intent log shape in `intentlogwrite.go`; `AuditLog` in `audit.go` | CONFORMS | Reconciliation spec owns the Cat 3a detector |

### §5 Invariants

| Invariant | Description | Code Location | Status | Note |
|-----------|-------------|---------------|--------|------|
| BI-INV-001 | No intra-run writes to Beads | All writes route through adapter ops; `terminalTransitionWrite`/`SweepCloseBead` are the only write paths | CONFORMS | `adaptergate_bi002_test.go` + `terminaltransiteroute_bi012_test.go` enforce |
| BI-INV-002 | Bead ID stable across harmonik lifetime | `core.BeadID` is opaque; no alternate IDs minted | CONFORMS | — |
| BI-INV-003 | Git wins on completion disagreement | Cat 3 classification owned by reconciliation spec; adapter does not auto-reconcile | CONFORMS | Adapter provides write surface; reconciliation owns classification |
| BI-INV-004 | Beads status changes auditable via intent log or flagged as divergence | Intent log protocol in place (BI-030); `AuditLog` for disambiguation (BI-031 step 3i) | CONFORMS | See BI-031 gap for step-4 re-drive |

### §6 Schemas

| Schema | Description | Code Location | Status | Note |
|--------|-------------|---------------|--------|------|
| `BeadRecord` | 7-field record | `internal/core` — `BeadRecord` struct | CONFORMS | `AuditTrailRef` = string(bead_id) |
| `CoarseStatus` | Beads-owned extensible 8+-value enum | `internal/core` — `CoarseStatus` with `UnmarshalText` tolerating unknown values | CONFORMS | — |
| `HarmonikWriteStatus` | 5-value write subset | `internal/core` — enforced in adapter write ops | CONFORMS | — |
| `DependencyEdge` | 4-field edge | `show.go:162-199` — builds from `dependencies`/`dependents` arrays | CONFORMS | — |
| `EdgeKind` | 4-value enum `{parent-child, blocks, conditional-blocks, waits-for}` | `internal/core` — `EdgeKind` | CONFORMS | All 4 values declared |
| `BrError` | 7-value closed enum | `brerror.go` | CONFORMS | All 7 values declared; `Valid()` enforced |
| `IntentLogEntry` | 8-field record including `schema_version` | `intentlogwrite.go:53-62` — `intentLogEntryWire`; `terminaltransition_bi010.go:44` — `IntentLogEntrySchemaVersion = 1` | CONFORMS | `intended_post_state` field populated |
| `TerminalOp` | 4-value enum `{claim, close, reopen, reset}` | `internal/core` — `TerminalOp` constants | CONFORMS | All 4 ops including `reset` |

---

## 2. Gaps Summary

| # | Spec Req | Code Location | Severity | Description | FIX-SITE |
|---|----------|---------------|----------|-------------|----------|
| G1 | BI-024a | `internal/brcli/version.go:50`, `internal/daemon/daemon.go:1148` | **major** | `CheckBrVersion` function exists but is NOT called explicitly at daemon startup; spec requires EXPLICIT `br --version` handshake before first queue-submit RPC; daemon comment acknowledges this is done "lazily" on first br invocation, which does not satisfy the ordering guarantee | daemon.go or daemon/orphansweep.go |
| G2 | BI-025d | `internal/brcli/stderrcap.go`, `internal/brcli/adapter.go:160`, `internal/brcli/timeout.go:145` | minor | `stderrCapWriter` struct with 1 MiB cap + truncation marker is defined but NOT wired into `Run()` or `RunWithTimeout()`; both use raw `bytes.Buffer` for stderr; hk-872.31 tracks wiring | brcli/adapter.go, brcli/timeout.go |
| G3 | BI-031 step-4 | `internal/lifecycle/orphansweep.go:441`, `internal/daemon/orphansweep.go` | **major** | Step-3 (GC landed intents via ShowBead) and step-3b (scan+classify) are implemented. Step-4 (re-drive stale pre-state intents at adapter startup) is NOT implemented: non-landed intent files are retained for Cat 3a reconciliation rather than re-driven by the adapter. Spec says adapter MUST re-issue the `br` write at startup when status is still pre-state. | `lifecycle/orphansweepbeads.go` or new adapter function (NOT paul hard-hold) |
| G4 | BL-MRG-001 | `.gitattributes`, `internal/daemon/beadsmergedriver.go` | minor | Merge strategy name mismatch: spec requires `merge=beads-union` / `[merge "beads-union"]`; code uses `merge=beads-merge` / `[merge "beads-merge"]` throughout; functionally equivalent (attribute and config key match) but spec non-compliant naming | `.gitattributes`, beadsmergedriver.go |
| G5 | BL-MRG-003 | `cmd/harmonik/beadsmerge.go:418-436` | minor | Conflict log format differs from spec: code emits `<timestamp> bead=<id> reason=<str> current_at=<ts> other_at=<ts>`; spec requires `<iso8601-timestamp> CONFLICT bead=<id> field=status a=<A_value> b=<B_value> resolution=took-ours`; missing: `CONFLICT` keyword, `field=status`, `a=`/`b=` old/new status values, `resolution=took-ours` | cmd/harmonik/beadsmerge.go |
| G6 | BL-MRG-004 | `internal/daemon/workloop.go` (merge path) | **major** | `br sync --import-only` NOT called after any rebase/merge that touches `.beads/issues.jsonl`; `brcli/syncflushonly.go` implements only `--flush-only` (export SQLite→JSONL, opposite direction); post-merge the union-merged JSONL must be imported back to SQLite before any subsequent `br` operations | FIX-SITE = paul hard-hold (workloop.go) |
| G7 | BL-MRG-005 | `internal/daemon/workloop.go:5489, 5700, 5786-5853` | **major** | `mergeRebaseAutoResolveBeadsLedger` still present and called at two sites (lines 5489, 5700); spec MUST remove it as it uses `git checkout --theirs .beads/issues.jsonl` which suppresses the registered merge driver and reintroduces lossy merge behavior | FIX-SITE = paul hard-hold (workloop.go) |

---

## 3. v0.4.1 → v0.7.0 Delta Assessment

This section identifies requirements added or substantially changed between v0.4.1 (epic hk-872 scope) and v0.7.0 (current), and assesses code conformance for each.

### v0.5.0 additions (extqueue)

| Added/Changed | Assessment |
|---------------|-----------|
| BI-013 demoted — `br ready` no longer daemon dispatch input; daemon uses submitted queue | CONFORMS — `ready.go` is orchestrator-facing; `workloop.go` uses queue path |
| BI-013a relocated — `needs-attention` rejection moves to submit-time via `workloop.go` | CONFORMS — submit-time check in `workloop.go`; `ready.go` does additional read-time filter |
| BI-013b NEW — submit-time `br show` per bead | CONFORMS — `workloop.go:1911+` pre-claim `ShowBead` |
| BI-013c NEW — pre-claim status re-read with `bead_claim_skipped` event | CONFORMS — `workloop.go:1943-1954` |
| BI-024a re-anchored — handshake ordering vs first queue-submit | **GAP G1** — explicit handshake missing |
| §4.5a NEW — submit-time validation read surface section | CONFORMS for BI-013b/c; see G1 for BI-024a ordering |

### v0.6.0 additions (imrest — activity-marker vs truth-claim split)

| Added/Changed | Assessment |
|---------------|-----------|
| BI-010d NEW — `claim` as activity-marker; `reset` op for orphan-sweep | CONFORMS — `ResetBead` in `terminaltransition_bi010.go:390` |
| `reset` added to BI-010a table and BI-INV-001 | CONFORMS — all 4 ops in adapter |
| `TerminalOp` enum gains `reset` | CONFORMS — `internal/core` |

### v0.6.1 additions (retry budget widening)

| Added/Changed | Assessment |
|---------------|-----------|
| BI-031 step (4c-transient) — 10-retry budget for terminal writes vs 3-retry for reads | CONFORMS — `dblockretry.go:36-61` — `UnavailableRetryMax=10`, `DBLockedRetryMax=3` |
| `UnavailableRetryCap=15s` reasoning (hk-5dewt) | CONFORMS — `dblockretry.go:55-61` |

### v0.6.2 additions (br-ready sort priority)

| Added/Changed | Assessment |
|---------------|-----------|
| BI-013d NEW — `--sort priority` required | CONFORMS — `ready.go:67` — `brReadySortPriority = "priority"` |

### v0.6.3 amendments (standard-bead-dot)

| Added/Changed | Assessment |
|---------------|-----------|
| BI-009a amended — built-in fallback flipped `single` → `dot`; `review-loop` floor on parse failure | CONFORMS — `workflowlabelconflict.go` handles detection; tier resolution is daemon-side (execution-model scope) |

### v0.7.0 additions (bead-ledger-worktree-merge)

| Added/Changed | Assessment |
|---------------|-----------|
| BI-010e NEW — child-bead-spawn creates (agent-issued `br create`) | N-A — agent convention; no adapter enforcement required |
| BI-011 amended — permitted-write table with 3 categories | CONFORMS for daemon side |
| §4.8b BL-MRG NEW — 6 clauses | See G4 (BL-MRG-001), G5 (BL-MRG-003), **G6 (BL-MRG-004)**, **G7 (BL-MRG-005)** |
| BL-MRG-001 | GAP G4 (naming mismatch: `beads-merge` vs `beads-union`) |
| BL-MRG-002 | CONFORMS |
| BL-MRG-003 | GAP G5 (conflict log format differs) |
| BL-MRG-004 | **GAP G6 — `br sync --import-only` not called post-merge** |
| BL-MRG-005 | **GAP G7 — `mergeRebaseAutoResolveBeadsLedger` still present** |
| BL-MRG-006 | N-A (informative) |

### Summary — v0.4.1→v0.7.0 delta gaps

The four highest-value gaps are concentrated in v0.7.0 additions (BL-MRG) and one pre-existing protocol obligation (BI-024a explicit startup handshake). The entire §4.8b merge contract landed in v0.7.0 but has:
- Naming mismatch (G4 — minor)
- Wrong conflict log format (G5 — minor)
- Missing `br sync --import-only` post-merge (G6 — **major**: SQLite stays stale after merge)
- Unremediated `mergeRebaseAutoResolveBeadsLedger` (G7 — **major**: old lossy workaround still active and suppresses the driver)

G7 is the most critical: as long as `mergeRebaseAutoResolveBeadsLedger` is called in `workloop.go`, the `beads-merge` driver registered in BL-MRG-001 is never invoked for daemon-driven rebases. The union-merge algorithm exists and is correct, but the daemon bypasses it.

---

## 4. Final Verdict

**GAPS FOUND: 7**

| Severity | Count | Gap IDs |
|----------|-------|---------|
| **major** | 4 | G1 (BI-024a startup wiring), G3 (BI-031 step-4 re-drive), G6 (BL-MRG-004 import-only), G7 (BL-MRG-005 workaround removal) |
| minor | 3 | G2 (BI-025d stderr cap unwired), G4 (BL-MRG-001 naming), G5 (BL-MRG-003 log format) |

Iter-2 corrections reduced the gap count from 8 to 7 by:
- Removing G4 (iter-1) BI-014a false positive — `SweepOrphanBr` IS fully implemented in `lifecycle/orphansweepbr.go`
- Reclassifying G3 (iter-1) — startup intent-log scan EXISTS (`GCRetiredIntents` + `ScanIntentLog`); the real gap is BI-031 step-4 re-drive at adapter startup

The `internal/brcli/` adapter module itself is generally well-implemented; the gaps are concentrated in the daemon integration layer and in the newest spec section (§4.8b BL-MRG, v0.7.0). G3 and G6/G7 require changes to non-paul-hard-hold lifecycle code and paul-hard-hold `workloop.go` respectively.
