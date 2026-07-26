# Unit E6 — `internal/core` split by type-family (PARKED; ships an inventory + a growth ratchet, not a move)

**Status:** **PARKED** for this program cycle. When unparked, the unit is **NEEDS PREPARATORY SLICE** — and the first preparatory slice (PS-1, `internal/core/ids`) itself needs a preparatory slice (PS-0), because two verified stoppers make PS-1 land red as written.
**Depends on:** E1a, E1b, E1c, E2, E3, E4, E5 all landed and released. Program gate: no family split may land while any of E1–E5 is in flight (two moving import graphs at once = the merge-conflict warfare A5 R3 exists to prevent).
**Size:** `internal/core` = **105,397 LOC across 504 files** — **239 non-test / 31,821 LOC** and **265 test / 73,576 LOC** (2.3:1 test-to-source). Measured 2026-07-22 on `phase1-session-restart-substrate`. **This cycle moves 0 files and 0 LOC.** The first deferred slice (PS-1) is 38 files / 3,684 LOC (18 non-test / 831; 20 test / 2,853).
**Risk:** **HIGH if executed, LOW as parked** — every one of the 13 type-families sits in a mutual symbol cycle with at least one other family, so any move today either drags a second family along or creates an import cycle that core's own leaf deny rule (`.golangci.yml:211-217`) forbids outright; the deliverable here is therefore an inventory plus a mechanical brake on core's growth.

---

## 0. Which input won, where they disagreed

The recon and the adversarial challenge were reconciled claim-by-claim. **The challenge wins everywhere it produced a reproduction or a command output**, because in every disputed case it ran the check and the recon estimated. The specific reversals, all adopted into this document:

| Disputed claim | Recon said | Challenge said | Taken | Why |
|---|---|---|---|---|
| depguard allow/deny precedence | "longest matching prefix, allow should win — verify before assuming" | Refuted by reproduction: depguard v2 `listModeOriginal` computes `allowed = inAllowed && !inDenied`; deny is unconditional. `list-mode: strict` is the only correct fix; `lax` drops the allowlist entirely | **CHALLENGE** | It reproduced the denial with the repo's own `.tools/golangci-lint` and read the depguard source. Confirmed here: `grep -n 'list-mode\|listMode' .golangci.yml` → **zero hits**, so every rule in this repo runs in original mode. |
| A separate `coreids:` depguard block is additive protection | Yes — copy the `gitprobe` idiom | No — depguard loops every rule whose glob matches, so `**/internal/core/**` also governs `internal/core/ids/**`; the two blocks AND together | **CHALLENGE** | Glob semantics are mechanical and checkable; `**/internal/core/**` demonstrably matches `internal/core/ids/x.go`. |
| EventType constant count | 369 | 182 | **CHALLENGE** | Verified here: `grep -cE '^\tEventType[A-Za-z0-9]+ +EventType += ' internal/core/eventtype.go` → **182**. |
| PS-1 alias list | includes `type IdempotencyKey = ids.IdempotencyKey` and `type SubWorkflowNamespaceID = ids.SubWorkflowNamespaceID` | Neither compiles — `IdempotencyKey` is a func; `SubWorkflowNamespaceID` does not exist as a symbol at all | **CHALLENGE** | Verified: `internal/core/idempotencykey.go:23` is `func IdempotencyKey(runID RunID, transitionID TransitionID, op TerminalOp) string`; `internal/core/subworkflownamespaceid.go:25` declares `func NamespaceNodeID(parentNodeID, subNodeID NodeID) NodeID` and nothing named `SubWorkflowNamespaceID`. |
| `nodeid_test.go` moves with PS-1 | listed | does not exist; `beadid_prop_test.go` and `subworkflownamespaceid_test.go` were omitted | **CHALLENGE** | Verified by `ls`. |
| budget↔policy mutual edge via `CostBasis`/`ScopeTarget` | the edge to cut first | not mutual — those are doc-comment references only; the real inbound edges are `kindpayload.go:30`, `node.go:59`, `s02registrar_hka8bg45.go`, `eventreg_hqwn59.go` | **CHALLENGE** | Verified: `kindpayload.go:30` is `Budget *BudgetPayload`, `node.go:59` is `BudgetRef *BudgetRef`. |
| budget outbound cross-family symbols | 4 | 10 (compile-probe with `-gcflags=-e`) | **CHALLENGE** | A compile probe enumerates undefined symbols exhaustively; a grep does not. |
| budget family size | 13 files / 1,366 LOC | 14 files / 1,488 LOC | **CHALLENGE** | Verified: `ls internal/core/budget*.go \| grep -v _test \| wc -l` → **14**; `wc -l` → **1,488**. |
| The event-registration spine | 5 files, 20 `register*` funcs | 6 files, 21 funcs — `internal/core/eventreg_wkzlc.go` was missed | **CHALLENGE** | Verified: the file exists and `grep -c '^func register'` → 20 in `eventreg_hqwn59.go`, **1 in `eventreg_wkzlc.go`**. |
| PS-1 is move-ready and self-contained | yes | no — it fails the existing coverage gate at 91.9% against a 95% floor it prefix-inherits, and its alias shim is depguard-denied | **CHALLENGE** | Verified the inheritance mechanism here: `scripts/coverage-gate.sh:186` lists `${MODULE_PREFIX}/internal/core` in `HIGH_THRESHOLD_PACKAGES`, `:194` sets `CORE_THRESHOLD=95.0`, and the match at `:227-228` is `[[ "${pkg}" == "${ht_pkg}" \|\| "${pkg}" == "${ht_pkg}/"* ]]` — a `/`-prefix match, so `internal/core/ids` inherits 95%. `coverage.baseline:29` pins `internal/core 95.3` with `REGRESSION_MAX=0.3`. |

**Where the recon won:** its structural core. The challenge independently reproduced the 18-file id-primitive slice as a standalone module and it built, vetted and tested clean with zero edits — so *"id-primitives is the only family with zero intra-core outbound references"* stands, and it is the spine of everything below. The recon's headline measurements are also exact and were re-verified here: 239 non-test files / 31,821 LOC; 265 test files / 73,576 LOC; 831 LOC across the 18 id files; core imports nothing from `internal/*`. The recon's four parking rationales stand unchallenged and are adopted verbatim in §7.

**Net effect of the reconciliation:** the *verdict* does not change (E6 stays parked), but the *unpark procedure* gains a new first slice (PS-0) that the recon did not have, and the unpark criterion is explicitly flagged as measured on biased data (§7 R-08).

---

## 1. What moves

### 1a. This cycle: nothing moves

**Zero `.go` files change. Do not run any `git mv` in this unit.** The E6 deliverable for this cycle is four artifacts:

| Artifact | Kind | Purpose |
|---|---|---|
| `plans/2026-07-21-p2-extraction/_plan.md` §2 line 89 | edit | Replace the 6-family candidate list (which covers 92 of 239 non-test files = 38%) with the measured 13-family inventory below. |
| `internal/core/FAMILIES.md` | new | Ratify the 13 family names + globs + measured baselines. This is the vocabulary the growth gate checks against. |
| `scripts/core-family-gate.sh` | new | Forward-only `// Family:` header check on newly-added core files + a size ratchet. |
| `scripts/core-size.baseline` | new | `nontest_files 239` / `nontest_loc 31821`, measured 2026-07-22 on `phase1-session-restart-substrate`. |

### 1b. The 13 families — every one STAYS, and why

Counts are non-test files / non-test LOC / commits in the trailing 90 days / packages importing the family's symbols from outside core / % of those commits touching no other family / % also touching the event-registration spine. **The 12 topical families sum to exactly 239 files; `id-primitives` is a 13th, cross-cutting family whose 18 files are drawn from inside those 12 — the inventory is a naming, not a partition (see §7 R-07).**

| Family | Anchor file | Files | LOC | 90d | Fan-in | Solo% | Spine% | Destination | Why it stays |
|---|---|---|---|---|---|---|---|---|---|
| event-infra | `internal/core/eventtype.go` (1,463) | 22 | 5,371 | 142 | 28 | 44% | 76% | **STAYS** | Hottest and least splittable: it is the registry that names every other family's payload type. Blocked until PS-2 collapses the spine. |
| workflow-graph | `internal/core/workflow.go` (179) | 47 | 4,048 | 79 | 20 | 69% | 1% | **STAYS** | Largest by file count; 98 inbound symbol edges from 10 families, 34 outbound to 11. The densest knot in core. Fails unpark criterion 4. |
| agent+lifecycle | `internal/core/agentevents_hqwn59.go` (912) | 23 | 4,069 | 69 | 19 | 24% | 71% | **STAYS** | Fails criterion 3 hardest — 71% spine co-change. Splitting spreads "add an agent event" across two packages. |
| verdict+review | `internal/core/verdict.go` (88) | 26 | 3,590 | 41 | 18 | 56% | 14% | **STAYS** | 49 inbound symbol edges from 11 families. Fails criteria 1 and 4. |
| reconciliation+daemon-status | `internal/core/failureclass.go` (83) | 13 | 2,795 | 27 | 12 | 62% | 18% | **STAYS** | Fails criteria 1 and 4. |
| policy+permission | `internal/core/policydocument.go` (494) | 22 | 2,563 | 36 | 9 | 55% | 2% | **STAYS** | Mutually cyclic with cp+guard (15 symbols out, 5 back); also the third family in hook's dependency set via `PolicyExpression`. |
| cp+guard | `internal/core/cpregistry_hka8bg2.go` (235) | 19 | 2,537 | 29 | 2 | 65% | 3% | **STAYS** | Lowest external fan-in of any family but the highest intra-core outbound: 61 symbols across 9 families. |
| workspace+git+queue-payloads | `internal/core/workspaceevents_hqwn59.go` (409) | 26 | 2,534 | 44 | 18 | 45% | 22% | **STAYS** | Fails criteria 1 and 4. |
| budget | `internal/core/budgetpayload.go` (93) | **14** | **1,488** | 19 | 4 | **78%** | 5% | **STAYS** | Loosest family in core and the first to move when E6 unparks — but it fails criterion 1 (heat: 19 < 40) and criterion 4 (10 outbound symbols, cyclic). |
| run | `internal/core/run.go` (114) | 12 | 1,258 | 29 | 20 | 65% | 6% | **STAYS** | `RunID` alone is referenced by 19 packages. Fails criteria 1 and 4. |
| gate | `internal/core/gatepayload.go` (92) | **10** | 934 | 15 | 4 | 46% | 26% | **STAYS** | Cold and small; no split value today. Fails criteria 1 and 2. |
| hook | `internal/core/hookpayload.go` (110) | 5 | 575 | 9 | 1 (`internal/hooksystem`) | 44% | 11% | **STAYS** | Coldest and smallest — and still mutually cyclic with cp+guard, event-infra, verdict+review and policy+permission. Proof that size is not the binding constraint. |
| id-primitives (cross-cutting) | `internal/core/runid.go` (41) | 18 | 831 | 21 | 25 | n/a | n/a | **STAYS this cycle**; `internal/core/ids` at PS-1 | The only slice with **zero** intra-core outbound references — reproduced by building it as a standalone module. Still blocked by PS-0 (depguard + coverage). |

Where the recon and challenge disagreed on a count, the challenge's re-measured figure is in **bold** above (budget 14/1,488; gate 10).

### 1c. The deferred PS-1 manifest (does NOT move this cycle)

Recorded here so the future slice is a copy-paste, not a re-derivation. All 38 files verified to exist on `phase1-session-restart-substrate` at 2026-07-22.

| File | LOC | Destination | Notes |
|---|---|---|---|
| `internal/core/runid.go` | 41 | `internal/core/ids/runid.go` | `type RunID uuid.UUID`; highest fan-in symbol in core (19 pkgs) |
| `internal/core/beadid.go` | 19 | `internal/core/ids/beadid.go` | `type BeadID string`; 11 pkgs |
| `internal/core/eventid.go` | 48 | `internal/core/ids/eventid.go` | `type EventID uuid.UUID`; 15 pkgs |
| `internal/core/workflowid.go` | 33 | `internal/core/ids/workflowid.go` | 9 pkgs. `WorkflowMode*` constants are **workflow-graph** and stay |
| `internal/core/nodeid.go` | 6 | `internal/core/ids/nodeid.go` | `type NodeID string`. **No `nodeid_test.go` exists** |
| `internal/core/stateid.go` | 34 | `internal/core/ids/stateid.go` | |
| `internal/core/sessionid.go` | 13 | `internal/core/ids/sessionid.go` | `type SessionID string` |
| `internal/core/transitionid.go` | 41 | `internal/core/ids/transitionid.go` | |
| `internal/core/workspaceid.go` | 44 | `internal/core/ids/workspaceid.go` | |
| `internal/core/suiteid.go` | 33 | `internal/core/ids/suiteid.go` | |
| `internal/core/subworkflownamespaceid.go` | 27 | `internal/core/ids/subworkflownamespaceid.go` | Declares `func NamespaceNodeID`, **not** a type named `SubWorkflowNamespaceID` |
| `internal/core/projecthash.go` | 45 | `internal/core/ids/projecthash.go` | `type ProjectHash string` |
| `internal/core/snapshottoken.go` | 40 | `internal/core/ids/snapshottoken.go` | `type SnapshotToken struct` |
| `internal/core/idempotencykey.go` | 49 | `internal/core/ids/idempotencykey.go` | **Func, not a type.** Its only cross-file need is `TerminalOp`, satisfied by moving `terminalop.go` too |
| `internal/core/terminalop.go` | 57 | `internal/core/ids/terminalop.go` | Carries 4 **constants** (`TerminalOpClaim/Close/Reopen/Reset`) that need `const` forwarding, not `var` |
| `internal/core/eventidgen.go` | 115 | `internal/core/ids/eventidgen.go` | Holds the unexported `increment128` / `uuidGT` used by `transitionidgen.go` — the edge vanishes because both move |
| `internal/core/eventidghwm.go` | 109 | `internal/core/ids/eventidghwm.go` | |
| `internal/core/transitionidgen.go` | 77 | `internal/core/ids/transitionidgen.go` | |
| **non-test subtotal** | **831** | | 18 files |
| `internal/core/runid_test.go` | 116 | `internal/core/ids/` | |
| `internal/core/beadid_test.go` | 115 | `internal/core/ids/` | |
| `internal/core/beadid_prop_test.go` | 61 | `internal/core/ids/` | **Omitted by the recon** |
| `internal/core/eventid_test.go` | 174 | `internal/core/ids/` | |
| `internal/core/eventidgen_test.go` | 230 | `internal/core/ids/` | |
| `internal/core/eventidgen_stress_test.go` | 335 | `internal/core/ids/` | |
| `internal/core/eventidghwm_test.go` | 227 | `internal/core/ids/` | |
| `internal/core/transitionid_test.go` | 42 | `internal/core/ids/` | |
| `internal/core/transitionidgen_test.go` | 229 | `internal/core/ids/` | |
| `internal/core/transitionidgen_stress_test.go` | 335 | `internal/core/ids/` | |
| `internal/core/workflowid_test.go` | 109 | `internal/core/ids/` | |
| `internal/core/stateid_test.go` | 42 | `internal/core/ids/` | |
| `internal/core/sessionid_test.go` | 57 | `internal/core/ids/` | |
| `internal/core/workspaceid_prop_test.go` | 70 | `internal/core/ids/` | Property test; verified it does **not** consume `drawNonNilUUID` |
| `internal/core/suiteid_test.go` | 116 | `internal/core/ids/` | |
| `internal/core/projecthash_test.go` | 143 | `internal/core/ids/` | |
| `internal/core/snapshottoken_test.go` | 58 | `internal/core/ids/` | |
| `internal/core/idempotencykey_test.go` | 152 | `internal/core/ids/` | |
| `internal/core/terminalop_test.go` | 141 | `internal/core/ids/` | |
| `internal/core/subworkflownamespaceid_test.go` | 101 | `internal/core/ids/` | **Omitted by the recon** |
| **test subtotal** | **2,853** | | 20 files |
| **PS-1 total** | **3,684** | | 38 files |

### 1d. Files that explicitly STAY inside `internal/core` even at PS-1

| File | LOC | Why it stays |
|---|---|---|
| `internal/core/eventtype.go` | 1,463 | 182 `EventType` constants; `EventType` is referenced by 25 packages — the single most widely-used core symbol. PS-2 changes how the constants are *declared*, never the exported name or the owning package. |
| `internal/core/eventreg_hqwn59.go` | 660 | `init()` constructs `&BudgetWarningPayload{}`, `&WorkspaceCreatedPayload{}`, `&AgentReadyPayload{}` … by name. Any family move forces `core → core/<family> → core` = hard import cycle. 20 `register*` funcs. |
| `internal/core/eventreg_wkzlc.go` | — | **Second spine file** (`func registerStalenessEvents`). Missed by the recon; PS-2 must collapse it too. |
| `internal/core/pertypecompat_hqwn38.go` | 406 | `allPayloadCompatEntries` — exhaustive N-1 compat table (EV-029). A family move must break either the exhaustiveness assertion or locality. |
| `internal/core/eventregistry.go` | — | Owns the global registry + `eventRegistryReset` used by 6 test files. Its only `reflect` use (`:319`) inspects struct **field** names, never package paths — verified, so relocating types causes no serialization drift. |
| `internal/core/eventtype_coverage_gjyks_test.go` | 269 | `allEventTypeCohort` — package-private table pairing constants with constructors. Cannot move with any family. Blocks every family move until registry-driven. |
| `internal/core/ev027_amendment_guard_hqwn36_test.go` | 109 | Pins `const wantCount = 128` (a subset of the 182 constants, per `:15`/`:23`/`:84`). Edited by every add-an-event-type commit. |
| `internal/core/pertypecompat_hqwn38_test.go` | 345 | Asserts the compat table covers every registered type. |
| `internal/core/events_valid_falsify_hkmcbr4_test.go` | 760 | Cross-family `Valid()` falsification suite spanning all payload families. |
| `internal/core/proptest_uuid_helpers_test.go` | — | Package-private `drawNonNilUUID`, consumed by **10** property-test files across 7 families. PS-3 promotes it. |
| `internal/core/eventpayloads_hqwn59_prop_helpers_test.go` | — | Package-private `drawNonEmptyString` + the `allXxx` constant slices; same consumers, same PS-3 promotion. |
| `internal/core/cpinv001_registry_single_source_hka8bg54_test.go`, `cpinv002_events_only_hka8bg55_test.go`, `cpinv003_replay_safety_hka8bg56_test.go` | — | Control-point invariant sensors that read the global registry; must follow whichever package ends up owning registration. |
| all 221 other non-test core files | ~28k | Parked by family, per §1b. |

---

## 2. The seam it exits behind

**There is no seam, and that is correct — E6 is not an extraction.** E1–E5 move implementations out from behind a pre-existing interface (`handlercontract.HarnessRegistry`, `handler.Substrate`, `lifecycle/tmux.CommandRunner`, `workers.Registry`, the `queue` RPC surface, the RT ports). E6 has no interface to hide behind: `internal/core` is a **pure type-and-constant leaf** with zero outbound coupling to any `internal/*` package. A family split is a package-boundary relocation, not a de-implementation.

The nearest thing to a seam is the **structural guarantee that core is a leaf**, expressed mechanically at:

```
.golangci.yml:211-217
        core:
          files: ["**/internal/core/**"]
          allow:
            - "$gostd"
            - "github.com/google/uuid"
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/", desc: "core is a leaf; no subsystem imports" }
```

**No new seam is invented by this unit, and none may be.** A "core service interface" or a "payload provider port" would be exactly the unwanted abstraction the plan's §3.1 pure-move review rejects.

**RED FLAG — the one thing E6 does need, and it is not a port.** PS-1's compatibility shim (`type RunID = ids.RunID` inside `internal/core`) requires `internal/core` to import `internal/core/ids`, which the deny above forbids. Making that legal means **changing the semantics of a load-bearing leaf guard** by adding `list-mode: strict` to the `core` rule. That is a config change with a blast radius across every file matched by `**/internal/core/**`, and it must land and be verified **as its own slice (PS-0), before any file moves.** Treat it with the same care as an API rename: it is the difference between core being a guaranteed leaf and core being a leaf-by-convention.

---

## 3. Coupling to break

### 3a. Outbound (unit → daemon)

**None. `internal/core` has ZERO outbound coupling to `internal/daemon` or to any other `internal/*` package** — the deny at `.golangci.yml:217` makes it structurally impossible, and the challenge independently confirmed core imports nothing from `internal/*`. E6's coupling problem is entirely **intra-core, cross-family**. That is the table that matters:

| Symbol(s) | Defined in (family) | Used by | Resolution |
|---|---|---|---|
| 20 `register*` funcs (`registerBudgetEvents`, `registerWorkspaceEvents`, `registerAgentEvents`, `registerGateDispatchEvents`, `registerHandlerPauseEvents`, …) called from one `init()` | `eventreg_hqwn59.go` (event-infra, 660 LOC) | every payload family — the `init()` constructs `&BudgetWarningPayload{}`, `&WorkspaceCreatedPayload{}`, `&AgentReadyPayload{}`, `&GateDeniedPayload{}` by name | **BLOCKER until PS-2.** Moving budget out forces `core → core/budget → core`: a hard import cycle *and* a `.golangci.yml:217` violation. Fix = one declaration table with generated registration, so a family move relocates a *row*, not a call site. |
| `registerStalenessEvents` (21st register func) | `eventreg_wkzlc.go` (event-infra) — **second spine file, missed by the recon** | staleness payloads | Same as above. PS-2 has 6 files to collapse, not 5. |
| 182 `EventType` constants (`EventTypeBudgetWarning`, `EventTypeGateDenied`, `EventTypeAgentReady`, …) | `eventtype.go` (event-infra, 1,463 LOC, hottest file in core) | all 12 payload families; 25 packages outside core | **BLOCKER until PS-2.** The constant and the payload struct it names live in different families by construction, so every family split severs a declaration from its constant. Same fix: one table, generated constants. |
| `allPayloadCompatEntries` (exhaustive N-1 compat table, EV-029) | `pertypecompat_hqwn38.go` (event-infra, 406 LOC) | every payload family; `pertypecompat_hqwn38_test.go` asserts coverage of every registered type | **BLOCKER until PS-2.** A moving family must take its compat rows (breaking exhaustiveness) or leave them (breaking locality). Fold into the PS-2 table. |
| `allEventTypeCohort` | `eventtype_coverage_gjyks_test.go` (event-infra, 269 LOC) | `TestAllEventTypeConstantsHaveRegistryEntries` — the hk-gjyks drift sensor | **BLOCKER until PS-2.** Package-private test table naming payload structs from many families (128 of the 182, per `ev027_amendment_guard_hqwn36_test.go:97` `wantCount`). Must become registry-driven or move to a top-level conformance package that blank-imports every family. |
| `drawNonNilUUID` | `proptest_uuid_helpers_test.go` (package-private test fixture) | **10** property-test files across 7 families | **BLOCKER until PS-3.** Promote to an exported `internal/core/coretest` package and repoint consumers. A family that moves today loses its property tests at the boundary. |
| `drawNonEmptyString` + `allReconciliationTriggers` / `allXxx` constant slices | `eventpayloads_hqwn59_prop_helpers_test.go` (package-private) | same consumer set (`keeperinteriorevents_prop_test.go` consumes **only** this one, not `drawNonNilUUID`) | **BLOCKER until PS-3** — same promotion. |
| `eventRegistryReset` | `eventregistry.go` test surface (event-infra) | 6 test files across families needing registry isolation | Export into the PS-3 `internal/core/coretest` package alongside the fixtures. |
| `CostBasis`, `PolicyBudget`, `ScopeTarget`, `ScopeTargetSingleton` (policy+permission) and `VerdictEvent`, `VerdictEscalateToHuman` (verdict+review), plus `RunID`, `SessionID`, `SnapshotToken`, `WorkflowID` | consumed **by** budget | budget's 14 non-test files | **budget's true outbound set is 10 symbols, not 4** (compile probe, `-gcflags=-e`). PS-1 clears the 4 id types; the remaining 6 span **two** other families. The recon's "cut the CostBasis/ScopeTarget edge" is aimed wrong: `costbasis.go:7,13` and `scopetarget.go:27,32` reference `Budget*` **only in doc comments**. The real inbound cycle is `kindpayload.go:30` (`Budget *BudgetPayload`), `node.go:59` (`BudgetRef *BudgetRef`), `s02registrar_hka8bg45.go:337-375` and `eventreg_hqwn59.go:273-275`. |
| `CognitionMeta`, `ErrorCategory`, `EventID`, `IdempotencyClass`, `IdempotencyClassNonIdempotent`, `PolicyExpression`, `RunID`, `SideEffect`, `SideEffectKind` | consumed **by** hook | hook's 5 non-test files | **hook's outbound set is 9 symbols spanning 3 families** (event-infra, verdict+review, **policy+permission** via `PolicyExpression`) — the recon named 2. Inbound is wider too: 11 non-hook core files reference `Hook*` (`cognitionmeta.go`, `cp017_hook_cognition_s05.go`, `eventreg_hqwn59.go`, `gatependingrecord.go`, `kindpayload.go`, `permissionschema.go`, `runterminalpayload.go`, `s02registrar_hka8bg45.go`, `sideeffectkind.go`, `trigger.go`, `transitionpath.go`) = ≥5 families. Even the 5-file family cannot move alone. |
| `increment128`, `uuidGT` | `eventidgen.go` (id-primitives) | `transitionidgen.go` (id-primitives) | **Not a real cross-family edge** — both files move together in PS-1, so the edge disappears rather than needing an export. |
| 15 id types + 4 `TerminalOp` constants + 10 funcs | the 18 id-primitive files | all 12 other families **and** 25 packages outside core | **The only move-ready slice.** Verified by construction: the 18 files + 20 test siblings build, vet and `go test` clean as a standalone module with zero edits. Destination `internal/core/ids`, with alias/forwarding declarations left in `core`. |

### 3b. Inbound (daemon → unit)

**E6 has no unexported-symbol export problem.** `internal/core` is already a public leaf consumed by **35 packages**: `cmd/harmonik`, `internal/daemon`, `internal/daemon/bootconfig`, `internal/daemon/scenariotest`, `internal/brcli`, `internal/cognition`, `internal/digest`, `internal/eventbus`, `internal/handler`, `internal/handlercontract`, `internal/hooksystem`, `internal/keeper`, `internal/keepertest`, `internal/keepertwin`, `internal/lifecycle`, `internal/lifecycle/tmux`, `internal/orchestrator`, `internal/policy`, `internal/presence`, `internal/queue`, `internal/replay`, `internal/runexec`, `internal/scenario`, `internal/sentinel`, `internal/specaudit`, `internal/t5probe`, `internal/testhelpers`, `internal/twinparity`, `internal/watch`, `internal/workers`, `internal/workflow`, `internal/workflow/dot`, `internal/workflow/scenario`, `internal/workspace`, `test/scenario`.

**Total symbols needing export: 19** — all of them *intra-core cross-family* (the fixtures and registry helpers in §3a), **not** daemon→core. The 29 PS-1 declarations below are already exported; they need *forwarding*, not exporting.

| Current name (in `internal/core`) | Kind | Proposed forwarding declaration in `internal/core/idsalias.go` | Call sites to rewrite |
|---|---|---|---|
| `RunID` | type | `type RunID = ids.RunID` | 0 (19 pkgs unaffected) |
| `EventID` | type | `type EventID = ids.EventID` | 0 (15 pkgs) |
| `BeadID` | type | `type BeadID = ids.BeadID` | 0 (11 pkgs) |
| `WorkflowID` | type | `type WorkflowID = ids.WorkflowID` | 0 (9 pkgs) |
| `StateID` | type | `type StateID = ids.StateID` | 0 |
| `NodeID` | type | `type NodeID = ids.NodeID` | 0 |
| `TransitionID` | type | `type TransitionID = ids.TransitionID` | 0 |
| `SessionID` | type | `type SessionID = ids.SessionID` | 0 |
| `WorkspaceID` | type | `type WorkspaceID = ids.WorkspaceID` | 0 |
| `SuiteID` | type | `type SuiteID = ids.SuiteID` | 0 |
| `ProjectHash` | type | `type ProjectHash = ids.ProjectHash` | 0 |
| `SnapshotToken` | type | `type SnapshotToken = ids.SnapshotToken` | 0 |
| `TerminalOp` | type | `type TerminalOp = ids.TerminalOp` | 0 |
| `EventIDGenerator` | type | `type EventIDGenerator = ids.EventIDGenerator` | 0 |
| `TransitionIDGenerator` | type | `type TransitionIDGenerator = ids.TransitionIDGenerator` | 0 |
| `TerminalOpClaim` | **const** | `const TerminalOpClaim = ids.TerminalOpClaim` | 0 — **must be `const`, not `var`**, or const-ness is lost |
| `TerminalOpClose` | **const** | `const TerminalOpClose = ids.TerminalOpClose` | 0 |
| `TerminalOpReopen` | **const** | `const TerminalOpReopen = ids.TerminalOpReopen` | 0 |
| `TerminalOpReset` | **const** | `const TerminalOpReset = ids.TerminalOpReset` | 0 |
| `IdempotencyKey` | **func** | `var IdempotencyKey = ids.IdempotencyKey` — **NOT `type`** | 0 |
| `ResetBeadIdempotencyKey` | func | `var ResetBeadIdempotencyKey = ids.ResetBeadIdempotencyKey` | 0 |
| `NamespaceNodeID` | func | `var NamespaceNodeID = ids.NamespaceNodeID` (there is **no** `SubWorkflowNamespaceID` symbol) | 0 |
| `ReadEventIDHWM` | func | `var ReadEventIDHWM = ids.ReadEventIDHWM` | 0 |
| `WriteEventIDHWMAtomicNoSync` | func | `var WriteEventIDHWMAtomicNoSync = ids.WriteEventIDHWMAtomicNoSync` | 0 |
| `IsHWMClockRegression` | func | `var IsHWMClockRegression = ids.IsHWMClockRegression` | 0 |
| `ExtractUUIDv7Timestamp` | func | `var ExtractUUIDv7Timestamp = ids.ExtractUUIDv7Timestamp` | 0 |
| `NewEventIDGenerator` | func | `var NewEventIDGenerator = ids.NewEventIDGenerator` | 0 |
| `NewEventIDGeneratorWithHWM` | func | `var NewEventIDGeneratorWithHWM = ids.NewEventIDGeneratorWithHWM` | 0 |
| `NewTransitionIDGenerator` | func | `var NewTransitionIDGenerator = ids.NewTransitionIDGenerator` | 0 |

**29 declarations, not 18.** Go type aliases carry methods, so all 25 fan-in packages compile unchanged and the PS-1 PR touches **zero files outside `internal/core`** — the A5 §5 R3 "never a rename storm" rule made literal. That property holds **only if all 29 are enumerated correctly**; the recon's list got 2 of them wrong (both would fail to compile).

Later-family blast radii, recorded so the first real family move is chosen on evidence:

| Family | Exported symbols | Used outside core | Alias shims needed |
|---|---|---|---|
| budget | 46 | 4 (`BudgetRef`, `BudgetAccrualPayload`, `BudgetExhaustedEventPayload`, `BudgetScopeHandlerAccount`) | 4 |
| hook | — | 6, all in `internal/hooksystem` only | 6 |
| cp+guard | 57 | 5 (`ControlPoint`, `Registry`, `AttachPoint`, `DetectorClass`, `VerdictEnvelopeMismatchPayload`) across 2 pkgs | 5 |
| event-infra | — | `EventType` alone spans 25 pkgs | **fails criterion 5 outright** |

---

## 4. Step-by-step recipe

### PART A — this cycle (steps 1–6). Executable now. No `.go` file changes.

**Step 1 — Replace the plan's E6 candidate list.** Edit `plans/2026-07-21-p2-extraction/_plan.md` line 89. Delete the 6-family bullet and substitute the 13-family inventory table from §1b of this document. *Check:* `grep -c 'workflow-graph' plans/2026-07-21-p2-extraction/_plan.md` returns ≥ 1. Rationale to record in the commit body: the old list named 6 families covering 92 of 239 non-test files (38%); it omitted workflow-graph (47 files), workspace+git+queue-payloads (26), verdict+review (26), policy+permission (22), reconciliation+daemon-status (13), run (12) and id-primitives (18) — 147 files / 61% of core. **Any estimate built on the old list is low by roughly 2.5x.**

**Step 2 — Write `internal/core/FAMILIES.md`.** One section per family from §1b, each carrying: the family name, its file-prefix globs, and the measured 2026-07-22 baseline (files / LOC / 90d commits / fan-in packages / solo% / spine%). Add an explicit header note: *"`id-primitives` is a cross-cutting naming, not a partition member — its 18 files are counted inside the 12 topical families, which sum to exactly 239."* *Check:* the file lists exactly 13 family names and the 12 topical file counts sum to 239 (22+47+23+26+26+22+19+12+14+13+10+5 = 239).

**Step 3 — Adopt a forward-only `// Family: <name>` header convention.** New core files only. Do **not** sweep the existing 239 — only 60 carry any filename header, 29 carry a Spec ref, and **zero** carry a family tag, so a retroactive sweep would itself be the 239-file rename storm E6 exists to avoid. Document the convention in `FAMILIES.md`.

**Step 4 — Write `scripts/core-family-gate.sh`**, modelled on the repo's existing ratchet idiom (`scripts/coverage-gate.sh`, `scripts/codex-coverage-gate.sh` + `scripts/codex-coverage-floor.baseline`, `scripts/keeper-coverage-gate.sh` + `scripts/keeper-coverage-floor.baseline`). It MUST:

  a. List files **added** under `internal/core/` in the PR: `git diff --diff-filter=A --name-only "$BASE"...HEAD -- internal/core/`.
  b. Fail if any added `.go` file lacks a `// Family: <name>` line within its first 10 lines.
  c. Fail if `<name>` is not one of the 13 names in `internal/core/FAMILIES.md`.
  d. Compare `ls internal/core/*.go | grep -v _test | wc -l` and the non-test `wc -l` total against `scripts/core-size.baseline`, and fail if either grew without the same PR bumping the baseline.

  *Check:* `bash -n scripts/core-family-gate.sh` parses; running it on a clean tree exits 0; running it with a hand-made added file lacking a family header exits non-zero.

**Step 5 — Seed `scripts/core-size.baseline`:**

```
# internal/core size ratchet — P2 unit E6 (parked).
# Raising either number requires the PR to name the family being grown and to
# carry a reviewer sign-off trailer. See internal/core/FAMILIES.md.
# Measured 2026-07-22 on phase1-session-restart-substrate.
nontest_files 239
nontest_loc 31821
```

*Check:* `ls internal/core/*.go | grep -v _test | wc -l` → `239`; `ls internal/core/*.go | grep -v _test | xargs wc -l | tail -1` → `31821 total`.

**Step 6 — Wire the gate as a HARD failure.** Add a `core-family-gate` target to `Makefile` next to `specaudit-lint` (`Makefile:569-571`):

```make
.PHONY: core-family-gate
core-family-gate:  ## internal/core growth ratchet + family-header check (P2 E6)
	scripts/core-family-gate.sh
```

Add a step to the `check` job in `.github/workflows/ci.yml` immediately after the `make check-short` step (`ci.yml:42-43`), **without `continue-on-error`** — matching the operator's hard-CI-failure decision:

```yaml
      - name: make core-family-gate
        run: make core-family-gate
```

Copy the *shape* of the `specaudit-lint` step at `ci.yml:55-57` but **not** its `continue-on-error: true` — that is the non-blocking form to avoid. *Check:* `grep -A 3 'core-family-gate' .github/workflows/ci.yml` shows no `continue-on-error`.

**Step 7 — STOP.** Part B does not run in this cycle. Record in the unit's close comment: `internal/daemon` LOC delta = 0, `internal/core` LOC delta = 0, and the reason (parked; see §7).

### PART B — the unpark procedure (steps 8–14). Runs ONLY when §5's unpark criterion fires AND E1–E5 have all released.

**Step 8 — PS-0a: prove the depguard list-mode change.** This is the slice the recon did not have. On a scratch branch, change the existing `core` rule to add `list-mode: strict` and allow the future ids package:

```yaml
        # core: leaf kernel. list-mode MUST be `strict` (kebab-case — golangci v2
        # silently ignores the camelCase `listMode` spelling used in depguard's own
        # docs). Under the default `original` mode depguard computes
        #   allowed = inAllowed && !inDenied
        # so the blanket internal/ deny below would reject internal/core/ids and the
        # P2 E6 PS-1 alias shim could not compile. Under `lax` it computes
        #   allowed = !inDenied || ...
        # which drops the allowlist entirely and would let core import ANY package —
        # that would gut the leaf guarantee. Only `strict` gives
        #   allowed = inAllowed && (!inDenied || longer-allow-prefix)
        # which admits internal/core/ids and still rejects everything else.
        # Rationale: plans/2026-07-21-p2-extraction/E6-internalcore.md §2, §4 PS-0a.
        core:
          files: ["**/internal/core/**"]
          list-mode: strict
          allow:
            - "$gostd"
            - "github.com/google/uuid"
            - "github.com/gregberns/harmonik/internal/core/ids"
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/", desc: "core is a leaf; no subsystem imports" }
```

*Check (all three must hold, run locally — see the CI caveat in §7 R-09):*
  - `.tools/golangci-lint run ./internal/core/...` is green with a throwaway `internal/core/ids` package and an alias file present.
  - Adding `import "example.com/random/pkg"` to a core file makes that same command **fail** (this is what proves `strict` and not `lax`).
  - The camelCase spelling is not used anywhere: `grep -n 'listMode' .golangci.yml` returns nothing.

**Do NOT add a separate `coreids:` block.** `depguard.go` loops over *every* rule whose `files` glob matches, and `**/internal/core/**` matches `internal/core/ids/**`, so a sibling block is ANDed with `core`, not additive — and its gitprobe-style self-import allow (`.../internal/core/ids`, the external-test-package idiom at `.golangci.yml:171-178`) would be denied by `core`'s blanket `internal/` deny. The `strict` change above is the whole fix.

**Step 9 — PS-0b: clear the coverage floor.** `scripts/coverage-gate.sh:186` puts `${MODULE_PREFIX}/internal/core` in `HIGH_THRESHOLD_PACKAGES`; the match at `:227-228` is a `/`-prefix match, so `internal/core/ids` inherits `CORE_THRESHOLD=95.0` (`:194`) the day it exists. The slice as manifested measures **91.9%** of statements — **3.1pp under the floor**. Before any `git mv`:
  - Add tests to the 18 id-primitive files in place (they are still in `internal/core`) until the *subset* measures ≥ 95.0%. Reproduce the measurement by copying the 38 files into a scratch module and running `go test -cover`.
  - Plan the same-PR update to `coverage.baseline:29` (`github.com/gregberns/harmonik/internal/core 95.3`, `REGRESSION_MAX=0.3`): core's own number must be re-measured after 831 LOC leave, or the ≤0.3pp regression check fails.
  *Check:* scratch-module `go test -cover ./...` ≥ 95.0%.

**Step 10 — PS-1: move id-primitives.** Only after steps 8 and 9 are green and merged.

```bash
git -C /Users/gb/github/harmonik mkdir -p internal/core/ids 2>/dev/null || mkdir -p /Users/gb/github/harmonik/internal/core/ids
cd /Users/gb/github/harmonik && git mv \
  internal/core/runid.go internal/core/beadid.go internal/core/eventid.go \
  internal/core/workflowid.go internal/core/nodeid.go internal/core/stateid.go \
  internal/core/sessionid.go internal/core/transitionid.go internal/core/workspaceid.go \
  internal/core/suiteid.go internal/core/subworkflownamespaceid.go internal/core/projecthash.go \
  internal/core/snapshottoken.go internal/core/idempotencykey.go internal/core/terminalop.go \
  internal/core/eventidgen.go internal/core/eventidghwm.go internal/core/transitionidgen.go \
  internal/core/ids/
git mv \
  internal/core/runid_test.go internal/core/beadid_test.go internal/core/beadid_prop_test.go \
  internal/core/eventid_test.go internal/core/eventidgen_test.go internal/core/eventidgen_stress_test.go \
  internal/core/eventidghwm_test.go internal/core/transitionid_test.go internal/core/transitionidgen_test.go \
  internal/core/transitionidgen_stress_test.go internal/core/workflowid_test.go internal/core/stateid_test.go \
  internal/core/sessionid_test.go internal/core/workspaceid_prop_test.go internal/core/suiteid_test.go \
  internal/core/projecthash_test.go internal/core/snapshottoken_test.go internal/core/idempotencykey_test.go \
  internal/core/terminalop_test.go internal/core/subworkflownamespaceid_test.go \
  internal/core/ids/
```

Then rewrite `package core` → `package ids` in all 38 files (`sed -i '' 's/^package core$/package ids/' internal/core/ids/*.go`; external-test files, if any, become `package ids_test`). **Note: there is no `internal/core/nodeid_test.go` — do not list it, the `git mv` will abort on a missing path.**

**Step 11 — PS-1 shim: create `internal/core/idsalias.go`** with all **29** forwarding declarations from §3b verbatim — 15 `type X = ids.X`, 4 `const TerminalOpX = ids.TerminalOpX`, 10 `var Fn = ids.Fn`. *Check:* `go build ./... && git diff --name-only | grep -v '^internal/core/' | wc -l` → `0`. If that count is not zero, a forwarding declaration is missing or misspelled — fix the shim, do not rewrite the call site.

**Step 12 — PS-2 (only if a family still qualifies after PS-1): collapse the event-registration spine.** Today adding one event type edits **six** files in lockstep — `eventtype.go`, `eventreg_hqwn59.go`, **`eventreg_wkzlc.go`**, `pertypecompat_hqwn38.go`, `eventtype_coverage_gjyks_test.go`, `ev027_amendment_guard_hqwn36_test.go` (the five-file version was verified line-for-line on commits `a1fcfca2` and `d60d7b9d`; the sixth was missed there). Replace the three hand-maintained tables with **one declaration table** plus generated constants, registration and compat rows, and make `TestAllEventTypeConstantsHaveRegistryEntries` iterate the sealed registry instead of `allEventTypeCohort`. Reconcile `ev027_amendment_guard_hqwn36_test.go:97`'s `wantCount = 128` against the 182 declared constants as part of this slice. *Until PS-2 lands, NO payload family can move.*

**Step 13 — PS-3: promote the shared test fixtures.** Move `internal/core/proptest_uuid_helpers_test.go` (`drawNonNilUUID`) and `internal/core/eventpayloads_hqwn59_prop_helpers_test.go` (`drawNonEmptyString` + the `allXxx` slices) into an exported `internal/core/coretest` package, plus `eventRegistryReset`. Repoint the **10** property-test files across 7 families that consume them. *Check:* `go test ./internal/core/... -count=1` green; `grep -rn 'drawNonNilUUID' internal/core/*_test.go` returns nothing outside `coretest`.

**Step 14 — FIRST REAL FAMILY: budget.** `git mv internal/core/budget*.go internal/core/budget/` moves **14** non-test files / 1,488 LOC. Package `budget`; alias-shim `BudgetRef`, `BudgetAccrualPayload`, `BudgetExhaustedEventPayload`, `BudgetScopeHandlerAccount` in core; extend the `core` rule's allow list with `.../internal/core/budget` (same `strict` mechanism as PS-0a — again, **not** a sibling block). Before the move, cut the **real** inbound cycle: `kindpayload.go:30` (`Budget *BudgetPayload`), `node.go:59` (`BudgetRef *BudgetRef`), `s02registrar_hka8bg45.go:337-375`, `eventreg_hqwn59.go:273-275`. The `CostBasis`/`ScopeTarget` "mutual edge" is doc-comment-only and needs nothing. Budget's residual outbound after PS-1 is `CostBasis`, `PolicyBudget`, `ScopeTarget`, `ScopeTargetSingleton` (policy+permission) + `VerdictEvent`, `VerdictEscalateToHuman` (verdict+review) — two whole families, so budget is a **two-family** move unless those six are broken first. **Never more than one family per release.**

---

## 5. Freeze tripwire

E6 has no "extracted package must not import daemon back" edge to add — core never imported daemon. The tripwire for this unit is inverted: it stops core from **growing**, and it stops new families from being invented outside the ratified vocabulary.

### 5a. The depguard edge (the leaf guarantee, unchanged this cycle)

`.golangci.yml:211-217` stays exactly as-is for the parked cycle:

```yaml
        core:
          files: ["**/internal/core/**"]
          allow:
            - "$gostd"
            - "github.com/google/uuid"
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/", desc: "core is a leaf; no subsystem imports" }
```

The `list-mode: strict` + `internal/core/ids` allow (step 8) is the **only** edit this unit will ever make to it, and it lands as its own slice at unpark time.

### 5b. The "no new files for this concern" guard — a grep-guard, not depguard

depguard cannot express "a new file must carry a family header", so this is a script gate. `scripts/core-family-gate.sh`:

```bash
#!/usr/bin/env bash
# internal/core growth ratchet + family-header check — P2 unit E6 (parked).
# Rationale: plans/2026-07-21-p2-extraction/E6-internalcore.md §5b.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

BASE="${CORE_GATE_BASE:-origin/main}"
FAMILIES_FILE="internal/core/FAMILIES.md"
BASELINE_FILE="scripts/core-size.baseline"
status=0

if [[ ! -f "${FAMILIES_FILE}" ]]; then
    echo "core-family-gate: missing ${FAMILIES_FILE}" >&2
    exit 1
fi

# (a)+(b)+(c) — every NEWLY ADDED core .go file needs a ratified family header.
added="$(git diff --diff-filter=A --name-only "${BASE}"...HEAD -- internal/core/ | grep '\.go$' || true)"
for f in ${added}; do
    fam="$(head -n 10 "${f}" | sed -n 's|^// Family: *\([A-Za-z0-9+_-]*\).*|\1|p' | head -n 1)"
    if [[ -z "${fam}" ]]; then
        echo "core-family-gate: ${f} has no '// Family: <name>' line in its first 10 lines" >&2
        status=1
        continue
    fi
    if ! grep -q "^## ${fam}\$" "${FAMILIES_FILE}"; then
        echo "core-family-gate: ${f} declares unratified family '${fam}' (not in ${FAMILIES_FILE})" >&2
        status=1
    fi
done

# (d) — size ratchet.
cur_files="$(ls internal/core/*.go | grep -v '_test\.go$' | wc -l | tr -d ' ')"
cur_loc="$(ls internal/core/*.go | grep -v '_test\.go$' | xargs wc -l | tail -1 | awk '{print $1}')"
base_files="$(awk '/^nontest_files/ {print $2}' "${BASELINE_FILE}")"
base_loc="$(awk '/^nontest_loc/ {print $2}' "${BASELINE_FILE}")"

if (( cur_files > base_files )); then
    echo "core-family-gate: internal/core grew to ${cur_files} non-test files (baseline ${base_files})." >&2
    echo "  Bump ${BASELINE_FILE} in this PR, naming the family and carrying a reviewer sign-off trailer." >&2
    status=1
fi
if (( cur_loc > base_loc )); then
    echo "core-family-gate: internal/core grew to ${cur_loc} non-test LOC (baseline ${base_loc})." >&2
    echo "  Bump ${BASELINE_FILE} in this PR, naming the family and carrying a reviewer sign-off trailer." >&2
    status=1
fi

[[ ${status} -eq 0 ]] && echo "core-family-gate: OK (${cur_files} files / ${cur_loc} LOC)"
exit ${status}
```

Hook: `Makefile` target `core-family-gate` next to `specaudit-lint` (`Makefile:569-571`), and a `.github/workflows/ci.yml` step in the `check` job right after `make check-short` (`ci.yml:42-43`) **with no `continue-on-error`**.

### 5c. Why the ratchet, not a header sweep

Only 60 of 239 non-test core files carry any filename header, 29 carry a Spec ref in the first 12 lines, and **zero** carry a family tag. Enforcing headers retroactively is a 239-file rename storm — precisely the thing E6 exists to avoid. **Forward-only + size ratchet is the whole design.**

---

## 6. Verification gate

### 6a. This cycle (Part A)

Run in this order from `/Users/gb/github/harmonik`:

| # | Command | Pass criterion |
|---|---|---|
| 1 | `go build ./internal/... ./cmd/...` | Exit 0. **No Go file changed, so this must be identical to the pre-change build.** |
| 2 | `go vet ./internal/...` | Exit 0, no new diagnostics. |
| 3 | `go test ./internal/core/... -count=1` | Exit 0. (E6 touches no daemon or harness code; `./internal/daemon/... ./internal/harness/...` are unaffected and need only the repo-wide run below.) |
| 4 | `go test ./... -count=1` | Exit 0 — proves the doc/script additions broke nothing. |
| 5 | `.tools/golangci-lint run` | depguard green. Note: a *full* run currently reports ~5,666 pre-existing issues (`Makefile:544`); the pass criterion is **no NEW depguard finding**, checked with `.tools/golangci-lint run --new-from-rev=origin/main`. |
| 6 | `bash -n scripts/core-family-gate.sh && scripts/core-family-gate.sh` | Parses; exits 0 on a clean tree. Then verify it FAILS: add a scratch `internal/core/zz_probe.go` with no `// Family:` line, re-run, confirm non-zero exit, delete the probe. |
| 7 | `make core-family-gate` | Exit 0 — proves the Makefile wiring. |
| 8 | `ubs scripts/core-family-gate.sh` | Exit 0. (`FAMILIES.md`, `core-size.baseline` and the plan edit are not code.) |
| 9 | `ls internal/daemon/*.go \| grep -v _test \| wc -l` and `ls internal/daemon/*.go \| grep -v _test \| xargs wc -l \| tail -1` | **Unchanged.** |
| 10 | `ls internal/core/*.go \| grep -v _test \| wc -l` → `239`; `… \| xargs wc -l \| tail -1` → `31821 total` | Matches `scripts/core-size.baseline` exactly. |

**Expected LOC drop from `internal/daemon`: 0. Expected LOC drop from `internal/core`: 0.** E6 is the one P2 unit whose success metric this cycle is **not** a shrink — it is *"core did not grow, and the next person who tries to grow it has to say which family and get a sign-off."* Publish that in the close comment so the stream's progress stays legible: *"E6: 0 LOC moved; `internal/core` frozen at 239 files / 31,821 non-test LOC by `scripts/core-size.baseline`; 13 families ratified."*

### 6b. At unpark (Part B), additionally

| # | Command | Pass criterion |
|---|---|---|
| 1 | `.tools/golangci-lint run ./internal/core/...` with `list-mode: strict` + a probe `import "example.com/random/pkg"` in a core file | **Must FAIL.** If it passes, the mode is `lax` and the leaf guarantee is gone — revert PS-0a. |
| 2 | Same command without the probe import | Green. Proves `internal/core/ids` is admitted. |
| 3 | `go test ./internal/core/... -coverprofile=/tmp/e6.out && go tool cover -func=/tmp/e6.out \| tail -1` | `internal/core/ids` ≥ **95.0%** (inherits `CORE_THRESHOLD` via `scripts/coverage-gate.sh:186,194,227-228`). |
| 4 | `scripts/coverage-gate.sh` | Exit 0, with `coverage.baseline:29` re-measured in the same PR (`REGRESSION_MAX=0.3`). |
| 5 | `go build ./... && git diff --name-only \| grep -v '^internal/core/' \| wc -l` | **`0`** — the PS-1 PR touches zero files outside `internal/core`. |
| 6 | `go test ./internal/daemon/... ./internal/harness/... -count=1` | Green — proves the alias shim is transparent to the two god-package consumers. |
| 7 | New `TestCoreIdsImportsNothingInternal` (plan §3.4 boundary test) | Asserts `internal/core/ids`'s import closure contains no `github.com/gregberns/harmonik/internal/` path. |
| 8 | `scripts/core-family-gate.sh` + record the new counts in `scripts/core-size.baseline` | Each release publishes the number core shrank by. Expected PS-1 drop: **−18 non-test files / −831 non-test LOC** from `internal/core` (net repo LOC unchanged; ~29 lines added back as `idsalias.go`). |

---

## 7. Risks and how each is mitigated

**R-01 — The heat is already gone (parking rationale 1/4).** `internal/core`'s entire history is 77 days old (first commit 2026-05-06) with 473 commits, but only **39 landed in the last 30 days** — churn collapsed from ~6/day to ~1.3/day. *Mitigation:* park. Splitting a package that is cooling on its own buys nothing and spends a 35-package blast radius per family. Revisit only when unpark criterion 1 fires.

**R-02 — The one hot spot is not a package-boundary problem (2/4).** 26 of the last 39 core commits (67%) touch event-infra, and 88% of those touch the registration spine. The real cost is that adding **one** event type edits **six** files in lockstep. *Mitigation:* PS-2 — one declaration table with generated derivatives. Splitting families would spread that single logical change across *more* packages, not fewer. This is a code-shape slice, not an extraction.

**R-03 — No family is a leaf (3/4).** All 13 families sit in a mutual cycle with at least one other. Even hook (5 files, 575 LOC, 9 commits/90d) is cyclic with cp+guard, event-infra, verdict+review and policy+permission. **Zero of 13 can move today** without dragging a second family or violating `.golangci.yml:217`. *Mitigation:* the unpark criterion's clause 4 makes non-cyclicity a hard precondition, not a judgement call.

**R-04 — The test mass is the real cost (4/4).** 265 test files / 73,576 test LOC vs 239 / 31,821 — a **2.3:1** ratio. A family split moves ~2.3x its own LOC in tests, and the cross-family entanglement is worst there: 10 property-test files across 7 families share two package-private fixture files, and three exhaustiveness tables enumerate families by name. *Mitigation:* PS-3 must land before any family moves, or the first family loses its property tests at the boundary.

**R-05 — depguard denies the PS-1 shim, today, as configured.** Not "unverified" — **reproduced**. depguard v2's default `listModeOriginal` computes `allowed = inAllowed && !inDenied`; prefix length is never consulted, and no rule in this repo sets a list mode (`grep 'list-mode\|listMode' .golangci.yml` → zero hits). Adding `.../internal/core/ids` to `core`'s allow list yields `import 'github.com/gregberns/harmonik/internal/core/ids' is not allowed from list 'core'`. *Mitigation:* PS-0a — `list-mode: strict` (kebab-case; the camelCase `listMode` from depguard's docs is **silently ignored** by golangci v2), landed and verified as its own slice. **`lax` is forbidden**: it computes `allowed = !inDenied || …`, dropping the allowlist entirely — a probe import of `example.com/random/pkg` passes clean under it, which would gut core's leaf guarantee. The recon's fallback ("eat the 25-package rewrite") is unnecessary and must not be taken.

**R-06 — A sibling `coreids:` depguard block is ANDed, not additive.** depguard iterates every rule whose `files` glob matches, and `**/internal/core/**` matches `internal/core/ids/**`. A `gitprobe`-style sibling block with a self-import allow would still be denied by `core`'s blanket `internal/` deny. *Mitigation:* §4 step 8 explicitly forbids the sibling block; the `strict` mode change on the existing `core` rule is the whole fix.

**R-07 — The 13 families are a naming, not a partition, and the unpark criterion is measured on the wrong one.** The 12 topical families sum to exactly 239 files; `id-primitives`' 18 files are drawn from inside them. So the per-family baselines the criterion is measured against (run "12 files", workspace, event-infra, workflow-graph) are **pre-PS-1 numbers that PS-1 itself invalidates**. *Mitigation:* `FAMILIES.md` states this explicitly (§4 step 2), and every family baseline must be **re-measured after PS-1** before the criterion is evaluated again.

**R-08 — Every family's coupling was undercounted, biasing the criterion toward "closer to ready than it is."** Compile probes found budget at 10 outbound symbols (recorded: 4) and hook at 9 across 3 families (recorded: 7 across 2), with hook's inbound spanning ≥5 families (recorded: 3). Every family probed came out **more** coupled. *Mitigation:* criterion clause 4 must be evaluated with a **compile probe** (`go build` the family as a standalone package with `-gcflags=-e` and read the undefined-symbol list), never with grep. Record the probe output in the unpark PR.

**R-09 — Neither the leaf deny nor the coverage floor is a hard CI gate today.** `.github/workflows/ci.yml` runs only `make check-short`, whose lint step is `.tools/golangci-lint run --new-from-rev=origin/main` (`Makefile:442`) — depguard is enforced on **changed lines only**. The full `golangci-lint run` and `scripts/coverage-gate.sh` live in `make check` (`Makefile:472,492`), which CI never invokes, and `Makefile:544` records that a full run currently fails on ~5,666 pre-existing issues. *Mitigation:* (a) `core-family-gate` goes into CI **without** `continue-on-error`; (b) PS-0a's depguard verification **cannot** be done via CI as wired — it requires a targeted local `.tools/golangci-lint run ./internal/core/...`, and §4 step 8 says so.

**R-10 — PS-1 lands red on the existing coverage gate.** The extracted slice measures **91.9%** against the 95% floor it prefix-inherits (`scripts/coverage-gate.sh:186,194,227-228`), and `coverage.baseline:29` pins core at 95.3 with `REGRESSION_MAX=0.3`, so core's own number must be re-measured in the same PR when 831 LOC leave. *Mitigation:* PS-0b — bring the id-primitive subset to ≥95% **while the files are still in `internal/core`**, then move. The recon cited `coverage-gate.sh` as the model for its new ratchet without checking that its own first slice passes it.

**R-11 — The alias shim is 29 declarations, not 18, and two of the recon's were uncompilable.** `IdempotencyKey` is a **func** (`idempotencykey.go:23`) and cannot be a `type` alias; `SubWorkflowNamespaceID` **does not exist** (the file declares `func NamespaceNodeID`). The recon also omitted the 4 `TerminalOp` constants entirely — forwarding them as `var` would silently change their const-ness. *Mitigation:* §3b enumerates all 29 with the correct declaration kind; §6b check 5 (`git diff --name-only | grep -v '^internal/core/' | wc -l` → 0) mechanically catches any omission.

**R-12 — Headline counts in the recon were approximations sold as measurements.** 369 EventType constants (actual **182**); budget 13 files / 1,366 LOC (actual **14 / 1,488** — and the recipe's literal `git mv internal/core/budget*.go` moves all 14); gate 11 files (actual **10**); "44 of 239 files carry a header" (actual **60**); `nodeid_test.go` listed but does not exist; `beadid_prop_test.go` and `subworkflownamespaceid_test.go` omitted; `allEventTypeCohort` described as naming every family's payload struct when `ev027_amendment_guard_hqwn36_test.go:97` pins `wantCount = 128` of the 182. *Mitigation:* every count in this document was re-run at 2026-07-22 on `phase1-session-restart-substrate` and the command is given inline so it can be re-verified in one line.

**R-13 — Scope: the plan understates E6 by 61%.** `_plan.md:89` names 6 families covering 92 of 239 non-test files; the four largest unnamed families (workflow-graph 47, workspace+git 26, verdict+review 26, policy+permission 22) are collectively bigger than everything it did name. Any estimate built on the plan's list is low by ~2.5x. *Mitigation:* §4 step 1 replaces the list before anything else in the unit.

**R-14 — Sequencing collision with E1–E5.** Every extracted package (`internal/harness/*`, `internal/gitprobe`, `internal/crew`, `internal/queue`) imports `internal/core`. Changing core's package layout while E1–E5 move files means two moving import graphs and the merge-conflict warfare (A5 R3) that P2's ordering rule exists to avoid. *Mitigation:* hard program gate — no family split lands while any of E1–E5 is in flight; recorded in this unit's **Depends on**.

**R-15 — A rubber-stamp ratchet is worse than no ratchet.** `scripts/core-size.baseline` degrades within a dozen commits if bumping it is routine. *Mitigation:* the baseline file's own header requires the bumping PR to **name the family** and carry a **reviewer sign-off trailer**, exactly as `scripts/codex-coverage-floor.baseline` is treated. Review the bump, not just the diff.

### The unpark criterion (all five must hold; today **zero of 13** families clear all five)

1. **INDEPENDENT HEAT** — ≥ 40 commits in the trailing 90 days touch the family's non-test files: `git log --since='90 days ago' --oneline -- <family paths> | wc -l`. Today only event-infra (142), workflow-graph (79) and agent+lifecycle (69) clear this.
2. **SOLO CHANGE** — ≥ 70% of those commits touch no other family's non-test files. Today only budget (78%) clears it; workflow-graph 69%, cp+guard 65%, run 65%, verdict+review 56%, agent+lifecycle 24%.
3. **SPINE INDEPENDENCE** — ≤ 20% of those commits also touch `eventtype.go` / `eventreg_hqwn59.go` / `eventreg_wkzlc.go` / `pertypecompat_hqwn38.go` / `eventregistry.go`. event-infra is 76% and agent+lifecycle 71% — **the two hottest families both FAIL**, which is exactly why heat alone must never trigger a split.
4. **ONE-DIRECTIONAL EDGES** — the family's combined inbound + outbound cross-family symbol edges are ≤ 10 distinct symbols and form no cycle with any family that stays. **Measured by compile probe, not grep (R-08).** Today 0 of 13 pass; the loosest is hook (9 out / ≥11 inbound files) and it is still cyclic with three other families.
5. **ALIAS-CARRYABLE FAN-IN** — the split ships with forwarding declarations in core so **zero** call sites outside `internal/core` change in the moving PR. Budget qualifies (4 external symbols); event-infra does not (`EventType` alone spans 25 packages).

**Program gate on top:** E1–E5 all landed; PS-0a + PS-0b + PS-1 + PS-2 + PS-3 landed in that order; never more than one family per release; every family baseline re-measured post-PS-1 before the criterion is re-evaluated.

---

## 8. Rollback

**Part A (this cycle) — trivially reversible; nothing in it can break a build.**

```bash
cd /Users/gb/github/harmonik
git rm -f scripts/core-family-gate.sh scripts/core-size.baseline internal/core/FAMILIES.md
git checkout -- Makefile .github/workflows/ci.yml plans/2026-07-21-p2-extraction/_plan.md
go build ./... && go test ./... -count=1
```

If only the CI gate is the problem (e.g. it fires on an unrelated PR before the baseline is trustworthy), do **not** delete it — flip it to advisory for one release by adding `continue-on-error: true` to the `ci.yml` step, file the follow-up bead, and remove the flag in the next cycle. Deleting the ratchet loses the measurement; softening it does not.

**Part B mid-flight — roll back in reverse dependency order. Each PS is its own commit, so each is its own revert.**

- **PS-0a (list-mode) goes wrong** — e.g. `strict` surfaces pre-existing violations elsewhere under `**/internal/core/**`: `git revert <ps0a-sha>`, restoring `.golangci.yml:211-217` byte-for-byte. Nothing depends on it yet. This is the cheapest abort point and is exactly why it is a separate slice.
- **PS-0b (coverage work) goes wrong** — tests are additive and in-place; `git revert` and core returns to 95.3.
- **PS-1 (`git mv` + shim) goes wrong** — `git revert <ps1-sha>` restores all 38 files to `internal/core` and deletes `idsalias.go`. Because the shim guarantees zero files changed outside `internal/core`, the revert cannot break any of the 35 fan-in packages. **This is the safety property that makes PS-1 abortable and is the reason the ≥29-declaration completeness check (§6b step 5) is a gate and not a nicety** — if even one declaration was missing, a call site was edited, and the revert is no longer self-contained.
- **PS-2 (spine collapse) goes wrong** — the largest revert (touches the 6 lockstep files + generated output). Abort criterion: if `TestAllEventTypeConstantsHaveRegistryEntries` or the EV-029 compat exhaustiveness test cannot be made green **within the slice**, revert rather than weaken either assertion. Those two tests are the drift sensors for the whole event taxonomy; a family split is not worth degrading them.
- **PS-3 (fixture promotion) goes wrong** — `git revert`; the 10 property-test files return to the package-private fixtures. No production code is involved.
- **A family move (step 14) goes wrong** — `git revert`, then re-evaluate criterion 4 with a fresh compile probe. Do **not** attempt to fix forward by adding a back-import from the moved family into core: that is an import cycle, `.golangci.yml:217` will reject it, and it converts a clean revert into a half-wired intermediate — the exact state plan §3.2 forbids.

**Universal abort signal for this unit:** if any slice cannot be made green without either (a) inventing a new interface in `internal/core`, or (b) editing a file outside `internal/core`, stop and revert. Both are proof that the family is not yet separable, which is the same finding that keeps E6 parked.
