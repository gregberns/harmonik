# Unit E1b — Claude harness implementation → `internal/harness/claude`

**Status:** NEEDS PREPARATORY SLICE (two commits, two releases: **E1b-prep** then **E1b**)
**Depends on:** nothing hard. `internal/harness/shared` already exists (landed with E1a's `seedprompt.go`). If **E1a (codex)** lands first it will have added the `harness-impl` depguard block — then E1b only *widens* the allow-list instead of creating it. Either order works; whichever lands first owns the block.
**Size:** ~843 non-test LOC leave `internal/daemon` across 2 file moves + 2 in-place struct evictions; ~1,378 test LOC move with them; ~327 in-place reference edits across 42 daemon test files and 6 daemon non-test files.
**Risk:** **MEDIUM** — the move itself is mechanical and compiler-checked, but the type named `claudeRunCtx` is *not* a claude type (it is the daemon's universal launch DTO, threaded through codex and pi too), so the unit cannot be a `git mv` the way E1a was; and two of its break points (a `//go:build specaudit` path assertion, and three test fixtures consumed by staying tests) are invisible to the default build/test/lint gate.

---

### Reconciliation note — where the recon and the challenge disagreed

The challenge wins on **every** point of disagreement. Each of its claims was re-verified against the tree before this plan was written; all six were confirmed, and two are *stronger* than stated:

| Disputed point | Recon said | Challenge said | **Taken** | Why |
|---|---|---|---|---|
| Test files move cleanly | 4 files `git mv` | 2 must be **split**: `claudelaunchspec_test.go` and `claudelaunchspec_remote_hkz8ek_test.go` export helpers to staying tests | **Challenge** | Verified: `claudeLaunchSpecFixtureWorkspace` is called 6× from `modelpreference_hkxo03m_test.go` (lines 61/83/103/123/148/172); `z8ekRunID` / `newNoOpRecorderZ8ek` / `decodeBase64FromScript` are called from `conformance_m4c7_test.go`, `harnessregistry_remote_hkr36v_test.go`, `harnessregistry_pi_remote_runner_m4c4_test.go`. The recon only checked outbound edges. |
| `export_test.go` shims | delete them | **keep** them as passthroughs; 10 staying files call `ExportedBuildClaudeLaunchSpec` | **Challenge** | Verified: 12 files reference `ExportedBuildClaudeLaunchSpec`; 10 of them stay. `ExportedNewClaudeHarness` is still used by `regression_golden_no_selection_hkhwwlk_test.go:399`. |
| Exported field spellings | mechanical Title-case (`runID`→`RunID`, …) | pin to the **existing mirror**: `APIKeyEnv`, `APIKeyFile`, `BaseURL`, `API` (Title-case rule yields `ApiKeyEnv` and silently breaks 18 files) | **Challenge** | Verified at `export_test.go:1388-1447`. |
| Affected test-file count | 37 | **42** (37 unexported-name users ∪ 18 shim users; the sets overlap but neither contains the other) | **Challenge** | Verified by set union: 37 ∪ 18 = 42. |
| Build gate | `go build ./...` | `./...` is **already red** (unvendored zmq deps under `plans/2026-07-15-agent-substrate-v2/investigate/10-zeromq-experiments/`); use `./internal/... ./cmd/...` | **Challenge** | Verified: `go build ./...` fails today on 3 experiment files; `go build ./internal/... ./cmd/...` is clean. |
| depguard block shape | one `**/internal/harness/**` rule allowing `workspace` | **two** rules — a tight one for `shared`, a wider one for the impls — otherwise the "keep workspace out of shared" constraint never bites | **Challenge** | Confirmed self-contradictory in the recon. Two rules also stop a future harness impl being dumped into `shared`. |
| Error-string policy | keep `ModelPreferenceError`'s `"daemon:"` text but *rewrite* the six `"daemon: buildClaudeLaunchSpec:"` prefixes | inconsistent — pick one | **Challenge** | **Policy adopted: keep ALL error strings byte-verbatim.** No test asserts on either (verified), so the pure-move review has no exception to argue about. Prefix hygiene is a follow-up bead, not this unit. |

Two of the recon's "implied couplings" the challenge cleared were re-checked and are genuinely clear: `envValue` (defined in `claudelaunchspec_configdir_hk8juwz_test.go:19`) is used only inside its own file — the `envValue` in `pibillingguard_test.go:279` is a struct field; `claudeHarnessFixtureWorkspace` is used only inside `claudeharness_test.go` (`harnessregistry_test.go:41` only *mentions* it in a comment).

The challenge's specaudit finding is **worse than it stated**: `scripts/scenario-gate.sh:384-386` triggers the specaudit leg only for changes under `specs/`, `cmd/harmonik/`, or `internal/daemon/socket.go`. Deleting `internal/daemon/claudeharness.go` triggers *nothing*. The break would surface only on a later unrelated spec edit.

The recon's **headline** finding is correct and is the spine of this plan: `claudeRunCtx` is the universal launch DTO. Both inputs agree; independently re-verified at `harnessregistry.go:202` (`buildCodexRoutedLaunchSpec(… rc claudeRunCtx …)`) and `runports.go:166` (the `LaunchPort.BuildSpec` seam signature).

---

## 1. What moves

### Non-test files

| File | LOC | Destination | Notes |
|---|---|---|---|
| `internal/daemon/claudelaunchspec.go` lines **47–231** (`claudeRunCtx` 31 fields, `claudeRunArtifacts` 6 fields) | ~185 | `internal/harness/shared/launchctx.go` as `shared.LaunchCtx` / `shared.LaunchArtifacts` | **Prep slice.** Renamed, not "claude"-named — this DTO carries codex and pi runs too. Doc comments move verbatim. |
| `internal/daemon/modelpreference.go` lines **43–115** (`modelRegex`, `modelMaxLen`, `validEffortLevels`, `ModelPreferenceError`, `validateModel`, `validateEffort`) | ~73 | `internal/harness/shared/modelpreference.go` | **Prep slice.** Daemon keeps `type ModelPreferenceError = shared.ModelPreferenceError` (load-bearing — see §3a). The 4-tier `ResolveModelPreference` walk **stays** in daemon (it reads project config + emits bus events). |
| `internal/daemon/claudelaunchspec.go` (remainder) | 430 | `internal/harness/claude/launchspec.go` | **Move slice.** `buildClaudeLaunchSpec` → `claude.BuildLaunchSpec`; `isHarmonikManagedWorktree` (line 573) stays unexported — verified zero users outside the file. |
| `internal/daemon/claudeharness.go` | 155 | `internal/harness/claude/harness.go` | **Move slice.** `ClaudeHarness` / `NewClaudeHarness` keep their names (`harnessregistry.go:156,184` assert on the concrete type). **Must retain the literal string `HC-045a`** — a specaudit sensor greps for it (§7 R6). |

### Test files that MOVE

| File | LOC | Destination | Notes |
|---|---|---|---|
| `internal/daemon/claudeharness_test.go` | 481 | `internal/harness/claude/harness_test.go` (`package claude_test`) | All 6 of its package-scope helpers are file-local (verified). Needs `ExportedRunCtxFromClaudeRunCtx` ported in as a package-local helper. |
| `internal/daemon/claudelaunchspec_test.go` | 548 | `internal/harness/claude/launchspec_test.go` (`package claude_test`) | **SPLIT:** `claudeLaunchSpecFixtureWorkspace` (line 38) must be *copied back* into daemon — see below. The other 5 helpers are file-local. |
| `internal/daemon/claudelaunchspec_configdir_hk8juwz_test.go` | 127 | `internal/harness/claude/launchspec_configdir_test.go` (`package claude`) | Clean. `envValue` (line 19) is file-local. |
| `internal/daemon/claudelaunchspec_remote_hkz8ek_test.go` | 222 | `internal/harness/claude/launchspec_remote_test.go` (`package claude`) | **SPLIT:** `z8ekRunID`, `newNoOpRecorderZ8ek`, `decodeBase64FromScript` must be *copied back* into daemon. |

**New leave-behind files created by the split** (this is the part a plain `git mv` gets wrong):

| New file | Package | Contents | Why |
|---|---|---|---|
| `internal/daemon/claudelaunchspec_fixture_test.go` | `daemon_test` | `claudeLaunchSpecFixtureWorkspace` verbatim (from `claudelaunchspec_test.go:35-48`) | `modelpreference_hkxo03m_test.go` calls it 6× and stays. |
| `internal/daemon/z8ekfixtures_test.go` | `daemon` | `newNoOpRecorderZ8ek`, `z8ekRunID`, `decodeBase64FromScript` verbatim | `conformance_m4c7_test.go`, `harnessregistry_remote_hkr36v_test.go`, `harnessregistry_pi_remote_runner_m4c4_test.go` all stay and use them. The moved copies live on in `internal/harness/claude/launchspec_remote_test.go` (both `claudelaunchspec_remote_*` and `claudelaunchspec_configdir_*` need them there). ~25 duplicated fixture lines — accept the duplication, do not invent a shared test package. |

### Files that STAY (and why)

| File | LOC | Why it stays |
|---|---|---|
| `internal/daemon/claudeheartbeat.go` + `_test.go` | 78 + 165 | Misnamed. Its only symbol `newDaemonHeartbeatEmitter(handlercontract.EventEmitter, core.RunID) handler.HeartbeatEmitter` is the run loop's **harness-blind** `agent_heartbeat` emitter. Callers verified: `workloop.go:4838`, `reviewloop.go:640`, `dot_cascade.go:1843` and `:2265`, `dot_gate.go:437` (the cognition gate — which routes codex and pi too). Moving it creates a `daemon → harness/claude` edge on **every non-claude run**. Follow-up bead: rename to `runheartbeatemitter.go`. |
| `internal/daemon/claudeworktreesweep.go` + `claudeworktreesweep_hkyhq3m_test.go` | 314 + 425 | Named in the plan's E1 file list but **not behind the `HarnessRegistry` seam**. It is a boot-time janitor for `.claude/worktrees/agent-*` dirs left by the interactive Claude Code CLI; sole caller is `orphansweep.go:995` inside `RunOrphanSweep`. Its test drives daemon's `RunOrphanSweep`/`OrphanSweepConfig` through package-private fixtures and cannot leave. Settled by the §3.4 boundary test: not reachable from `handlercontract.HarnessRegistry` ⇒ not in E1b. |
| `internal/daemon/pasteinject.go` | 2,637 | The **actual** claude Seed/Retask/Teardown driver (`pasteInjectImplementerInitial`/`Resume`/`Reviewer`, splash dismiss, `/quit` grace). Called *directly* by workloop/dot_cascade/dot_gate/reviewloop, **not** through the `Harness` seam, and interleaved with harness-blind run-loop machinery (commit polling, heartbeat staleness, reviewer budget sentinel). Belongs to **E5**, or needs its own slice *after* the `Harness` seam is widened. Expect this question at review; this is the answer. |
| `internal/daemon/harnessregistry.go` | — | Registry **assembly** is the composition root's job (plan §6, resolved). Only the impls leave. |
| `internal/daemon/modelpreference.go` (remainder) | — | The 4-tier resolution walk reads project config and emits `bead_label_conflict` on the bus; it is daemon wiring, not harness logic. |
| `internal/daemon/e2e_real_claude_{capture,reviewloop,single}_test.go` | 166+397+662 | `//go:build e2e_real_claude` daemon-level integration tests driving `internal/daemon/scenariotest` against a real `claude` binary. They exercise the daemon, not the harness impl. |
| `internal/daemon/scenario_remote_substrate_t4_claude_test.go` | 493 | `//go:build scenario`. Imports `internal/brcli`, `internal/daemon`, `internal/workers`; drives remote dispatch. |

---

## 2. The seam it exits behind

**Pre-existing seam:** `handlercontract.Harness` / `handlercontract.HarnessRegistry`, defined in `internal/handlercontract/harness.go` and `internal/handlercontract/harnessregistry.go`, owned by `internal/handlercontract` (a core-only leaf). The daemon-side wiring already exists at `internal/daemon/harnessregistry.go:33` (`newHarnessRegistry`, which does `reg.Register(core.AgentTypeClaudeCode, NewClaudeHarness())` at line 49).

**A second pre-existing seam carries the actual claude launch:** `LaunchPort.BuildSpec`, defined at `internal/daemon/runports.go:166` (the RT4 run-ports seam):

```go
BuildSpec(ctx context.Context, rc claudeRunCtx) (handler.LaunchSpec, claudeRunArtifacts, error)
```

**No new seam is invented.** But note the honest shape of this unit, because a reviewer will ask:

- Codex and pi exit *entirely* through `handlercontract.Harness`: `CodexHarness.LaunchSpec` / `PiHarness.LaunchSpec` take a `handlercontract.RunCtx` and convert to their own private `codexRunCtx` / `piRunCtx` inside their own files. Clean detach.
- Claude does **not**. `harnessregistry.go:156` and `:184` short-circuit:
  ```go
  if _, ok := h.(*ClaudeHarness); ok {
      return buildClaudeLaunchSpec(ctx, rc)
  }
  ```
  because `handlercontract.SpawnSpec` cannot carry the artifacts (`claudeSessionID`, `sessionLogPath`, `preExecMsgs`) that the run loop needs after launch. Routing claude through `Harness` would mean **widening `SpawnSpec`** — a new seam, which the plan forbids.

**Therefore E1b exits behind the existing `LaunchPort` seam, and the prep slice's job is to make that seam's DTO live in a package neither side owns (`internal/harness/shared`) rather than inside the claude file.** That is a *relocation* of an existing type, not a new port. **This is not a red flag, but it IS the reason E1b is two commits and E1a was one.**

**Dead-code note (do NOT fix during the move):** `ClaudeHarness.LaunchSpec` (`claudeharness.go:45-88`) is unreachable in production — the only `h.LaunchSpec(...)` call site in the repo is `harnessregistry.go:270`, which sits *past* the `*ClaudeHarness` short-circuit. Its `RunCtx → claudeRunCtx` conversion silently drops `rc.runner` and `rc.workerBinaryPath`, so if it were ever wired it would break remote claude runs (hk-z8ek). It is exercised only by `claudeharness_test.go`. **Move it verbatim; file a follow-up bead.**

---

## 3. Coupling to break

### 3a. Outbound (unit → daemon)

Complete: neither `claudeharness.go` nor `claudelaunchspec.go` declares or calls a method on any daemon receiver type. Their entire non-local surface is `core.*`, `handler.*`, `handlercontract.*`, `tmux.CommandRunner`, `workspace.*`, `uuid.NewV*` — plus exactly these three daemon symbols:

| Symbol | Defined in | Used by (in the unit) | Resolution |
|---|---|---|---|
| `validateModel` | `modelpreference.go:85` | `claudelaunchspec.go:471` | **Move to shared** as `shared.ValidateModel` (with `modelRegex`, `modelMaxLen`). Daemon rewrites its own remaining call at `modelpreference.go:243`. |
| `validateEffort` | `modelpreference.go:109` | `claudelaunchspec.go:476` | **Move to shared** as `shared.ValidateEffort` (with `validEffortLevels`). Daemon rewrites `modelpreference.go:305`. **Also `modelpreference.go:278`** does a bare `if _, ok := validEffortLevels[val]; !ok` — the recon missed this third site. Rewrite it to `if shared.ValidateEffort(val) != nil` (behaviorally identical: `ValidateEffort` is exactly that map lookup). |
| `ModelPreferenceError` | `modelpreference.go:65` | returned verbatim from both validators | **Move to shared + alias forward.** Leave `type ModelPreferenceError = shared.ModelPreferenceError` in `modelpreference.go`. Load-bearing: `export_test.go:777` aliases it as `ExportedModelPreferenceError`, and `modelpreference_hkxo03m_test.go:131/155/184` does `errors.As` against that. A fresh struct instead of an alias silently breaks those three assertions. **Keep `Error()`'s text byte-verbatim, including the `"daemon: ModelPreference: …"` prefix** — no test asserts it, and changing it violates pure-move. Flag it in the PR body so a reviewer does not call it a defect. |

### 3b. Inbound (daemon → unit)

| Current name | Proposed exported name | Call sites to rewrite |
|---|---|---|
| `claudeRunCtx` (31 fields) | **`shared.LaunchCtx`** (`internal/harness/shared`) — *not* `claude.RunCtx` | `runports.go:166,205,208,215`; `harnessregistry.go:78,141,142,175,176,202`; `workloop.go:355` (`workLoopDeps.launchSpecBuilder` field type) + construction ~`:4296-4340`; `reviewloop.go:269,1199`; `dot_cascade.go:1378`; `dot_gate.go:274`; `export_test.go:1388` + 6 capture-builders; 42 test files |
| `claudeRunArtifacts` (6 fields) | **`shared.LaunchArtifacts`** | `harnessregistry.go:147,180,205,235,272,279,286,303,331`; `runports.go:166,205,208,215`; plus field reads throughout the run loop |
| `buildClaudeLaunchSpec` | `claude.BuildLaunchSpec` | `harnessregistry.go:157,185`; `workloop.go:3979`; `dot_cascade.go:1450`; `dot_gate.go:358`; `reviewloop.go:308,1266`; `export_test.go` shim |
| `ClaudeHarness` | `claude.ClaudeHarness` (already exported — import path + 2 type assertions) | `harnessregistry.go:156,184`; `harnessregistry_test.go:97`; `dot_gate_reviewer_harness_hk01vs0_test.go:179` |
| `NewClaudeHarness` | `claude.NewClaudeHarness` (already exported — import path only) | `harnessregistry.go:49`; `export_test.go:2860`; `dot_gate_reviewer_harness_hk01vs0_test.go:179`; `regression_golden_no_selection_hkhwwlk_test.go:399` |
| `isHarmonikManagedWorktree` | *stays unexported* in `internal/harness/claude` | none outside `claudelaunchspec.go:502` (verified repo-wide) |
| `validateModel` / `validateEffort` / `ModelPreferenceError` + `modelRegex` / `modelMaxLen` / `validEffortLevels` | `shared.ValidateModel` / `shared.ValidateEffort` / `shared.ModelPreferenceError` (+ 3 unexported in shared) | see §3a |

**Symbols needing export: 42.** = 31 `LaunchCtx` fields + 6 `LaunchArtifacts` fields + `BuildLaunchSpec` + `ValidateModel` + `ValidateEffort` + `ModelPreferenceError` + `LaunchCtx`/`LaunchArtifacts` type names − 2 already-exported (`ClaudeHarness`, `NewClaudeHarness` need no export change).

**Exported field spellings — PIN THESE, do not derive them.** `export_test.go:1388-1447` already has a partial mirror that 18 test files build by keyed literal. Match it exactly or those 18 files break in ways the compiler reports as "unknown field", one at a time:

`RunID`, `BeadID`, `WorkspacePath`, `DaemonSocket`, `WorkflowMode`, `Phase`, `IterationCount`, `PriorClaudeSessID`, `HandlerBinary`, `DaemonBinaryPath`, `BaseEnv`, `BeadTitle`, `BeadDescription`, `NodePrompt`, `AgentTaskReAttach`, `PriorVerdictFile`, `PriorVerdictSummary`, `ReviewBaseSHA`, `ReviewHeadSHA`, `Model`, `Effort`, **`Provider`, `APIKeyEnv`, `APIKeyFile`, `BaseURL`, `API`**, `WorktreeRootPath`, `ExtraContext`, `BaseBranch`, `Runner`, `WorkerBinaryPath`.

Artifacts: `ClaudeSessionID`, `SessionLogPath`, `HandlerSessionID`, `PreExecMsgs`, `Substrate`, `ResolvedAgentType`.

A mechanical Title-case rule produces `ApiKeyEnv`/`ApiKeyFile`/`BaseUrl`/`Api` — **wrong**. The existing mirror is also *incomplete*: it has 24 of 31 ctx fields (missing `BeadTitle`, `AgentTaskReAttach`, `PriorVerdictFile`, `PriorVerdictSummary`, `ReviewBaseSHA`, `ReviewHeadSHA`, `ExtraContext`, `BaseBranch`) and 5 of 6 artifact fields (missing `ResolvedAgentType`). Those additions are purely additive to keyed composite literals, so aliasing works — but only with the spellings above.

---

## 4. Step-by-step recipe

### Pre-flight (do this before writing any code)

**0.1** The prep slice edits `workloop.go`, `dot_cascade.go`, `reviewloop.go`, `dot_gate.go` — **all four are staged-modified in the working tree right now** (`harnessregistry.go`, `runports.go`, `modelpreference.go`, `export_test.go` are clean). Land, commit, or stash that in-flight work first:
```
git -C /Users/gb/github/harmonik status --porcelain internal/daemon/workloop.go internal/daemon/dot_cascade.go internal/daemon/reviewloop.go internal/daemon/dot_gate.go
```
Must print nothing before you start. Then branch:
```
git -C /Users/gb/github/harmonik switch -c p2-e1b-prep
```

**0.2** Record the baseline (you will publish the delta in the bead close comment):
```
find /Users/gb/github/harmonik/internal/daemon -name '*.go' ! -name '*_test.go' | wc -l
find /Users/gb/github/harmonik/internal/daemon -name '*.go' ! -name '*_test.go' -exec cat {} + | wc -l
```
Measured 2026-07-22: **134 non-test files, 58,971 non-test LOC.**

**0.3** Confirm the baseline gate is green *before* touching anything (so a later red is unambiguously yours):
```
go build ./internal/... ./cmd/...
go vet ./internal/...
go vet -tags=scenario ./internal/daemon/
go vet -tags=e2e_real_claude ./internal/daemon/
go vet -tags=specaudit ./internal/specaudit/
go test ./internal/daemon/... -count=1
```
Do **not** use `go build ./...` — it is red today on unvendored zmq deps under `plans/2026-07-15-agent-substrate-v2/investigate/10-zeromq-experiments/`.

---

### SLICE 1 — `E1b-prep` (daemon-only rename; ZERO files move; its own commit and release)

**1.** Create `internal/harness/shared/launchctx.go`, `package shared`. Copy `internal/daemon/claudelaunchspec.go` **lines 47–231** verbatim, renaming the two types to `LaunchCtx` / `LaunchArtifacts` and every field to the exported spelling pinned in §3b. Keep every doc comment word-for-word. Imports needed: `encoding/json`, `internal/core`, `internal/handlercontract`, `internal/lifecycle/tmux`.

Add a package doc note: *"LaunchCtx is the launch DTO for every harness, not just claude — `buildCodexRoutedLaunchSpec` (daemon/harnessregistry.go) feeds it to codex and pi, and the five Pi provider fields are read only on the pi path."*

**2.** Create `internal/harness/shared/modelpreference.go`, `package shared`. Move `internal/daemon/modelpreference.go` **lines 43–115** — `modelRegex`, `modelMaxLen`, `validEffortLevels` (all stay unexported in `shared`), `ModelPreferenceError` + its `Error()` **with the string byte-verbatim**, `validateModel`→`ValidateModel`, `validateEffort`→`ValidateEffort`.

**3.** Update `internal/harness/shared/seedprompt.go`'s package header: it currently says *"The package is a leaf — stdlib only."* That is now false. Change to *"The package is a leaf: it imports only `core`, `handlercontract`, `lifecycle/tmux` and stdlib — never `daemon`, and never `workspace` (a harness impl that needs workspace imports it directly)."*

**4.** Edit `internal/daemon/modelpreference.go`: delete lines 43–115; add `type ModelPreferenceError = shared.ModelPreferenceError`; rewrite the three internal call sites — `:243` → `shared.ValidateModel(envModel)`, `:278` → `if shared.ValidateEffort(val) != nil {` (invert the condition; the old form was `if _, ok := validEffortLevels[val]; !ok`), `:305` → `shared.ValidateEffort(envEffort)`. Drop the now-unused `regexp` import.

**5.** Delete lines 47–231 from `internal/daemon/claudelaunchspec.go` and rewrite every daemon reference to the shared types. Go **leaf-ward first**, running `go build ./internal/daemon` after each file:

  1. `runports.go` (4 refs: `:166`, `:205`, `:208`, `:215`)
  2. `harnessregistry.go` (32 refs incl. comments)
  3. `dot_gate.go` (construction ~`:274-300`; artifact field reads ~`:401-510`)
  4. `dot_cascade.go` (`:1378-1450`; `:1634-1994`)
  5. `reviewloop.go` (`:269-308`; `:1199-1266`)
  6. `workloop.go` (`:355` field type; `:4296-4340`; `:4634-5161`; `:8106`)
  7. `claudelaunchspec.go` and `claudeharness.go` themselves

  Field renames are compiler-checked, so this is drive-the-compiler-to-zero, not a judgment exercise. Order-of-magnitude churn per file (re-derive with the grep below if you want exact):
```
grep -cE '\b(claudeRunCtx|claudeRunArtifacts|buildClaudeLaunchSpec)\b' internal/daemon/workloop.go   # 15
grep -cE '\b(claudeRunCtx|claudeRunArtifacts|buildClaudeLaunchSpec)\b' internal/daemon/harnessregistry.go  # 32
```

**6.** Rewrite the **42** affected test files (182 unexported-name references + 145 shim references). In `export_test.go`, replace the two hand-written mirrors with aliases:
```go
type ExportedClaudeRunCtx = shared.LaunchCtx
type ExportedClaudeRunArtifacts = shared.LaunchArtifacts
```
then collapse `ExportedBuildClaudeLaunchSpec` (`:1464`), `ExportedRunCtxFromClaudeRunCtx` (`:3100`) and the six capture-builder helpers (`:165`, `:914`, `:925`, `:939`, `:955`, `:1071`) to plain passthroughs — the field-by-field translation blocks disappear. Re-derive the affected set at any time with:
```
{ grep -rl 'claudeRunCtx\|claudeRunArtifacts\|buildClaudeLaunchSpec' internal/daemon/*_test.go;
  grep -rl 'ExportedClaudeRunCtx\|ExportedClaudeRunArtifacts\|ExportedBuildClaudeLaunchSpec\|ExportedNewClaudeHarness\|ExportedRunCtxFromClaudeRunCtx' internal/daemon/*_test.go; } | sort -u
```

**7.** Run the **full** §6 gate (including all three tagged `go vet` legs — two of the 42 files are behind `//go:build scenario` / `//go:build e2e_real_claude` and are compiled by *none* of the default commands).

**8.** Diff review: must be a pure rename, no logic delta. Commit and **release slice 1 on its own** — do not bundle it with the move. It touches the three hottest files in the tree (`workloop.go` 316 commits/90d, `dot_cascade.go` 94, `reviewloop.go`). **Land it the same day it is written; do not leave a half-renamed tree overnight.**

---

### SLICE 2 — `E1b` proper (the move)

```
git -C /Users/gb/github/harmonik switch -c p2-e1b-claude
```

**9.** Add the depguard blocks to `.golangci.yml` immediately after the `gitprobe` block (currently ending at line 178). **There is no `harness` rule today** — `gitprobe` got one, `internal/harness/shared` did not — so without this, the freeze tripwire simply does not exist. Literal YAML in §5. **If E1a already added `harness-impl`, only widen its allow-list** (add `internal/workspace`) rather than creating a second block.

**10.** Move the harness file:
```
git -C /Users/gb/github/harmonik mv internal/daemon/claudeharness.go internal/harness/claude/harness.go
```
Change `package daemon` → `package claude`. Keep `ClaudeHarness` / `NewClaudeHarness` names. Its only two free identifiers were `claudeRunCtx` and `buildClaudeLaunchSpec` — now `shared.LaunchCtx` and a package-local `BuildLaunchSpec`. **Verify the literal string `HC-045a` survives in the file** (it is in the header comment at line 9) — a specaudit sensor greps for it.

**11.** Move the launchspec file:
```
git -C /Users/gb/github/harmonik mv internal/daemon/claudelaunchspec.go internal/harness/claude/launchspec.go
```
Change `package daemon` → `package claude`. Rename `buildClaudeLaunchSpec` → `BuildLaunchSpec` with signature:
```go
func BuildLaunchSpec(ctx context.Context, rc shared.LaunchCtx) (handler.LaunchSpec, shared.LaunchArtifacts, error)
```
Rewrite the two validator calls (old lines 471/476) to `shared.ValidateModel` / `shared.ValidateEffort`. Leave `isHarmonikManagedWorktree` unexported. **Leave all eleven `"daemon: buildClaudeLaunchSpec: …"` error prefixes byte-verbatim** (lines 263, 270, 288, 299, 322, 352, 401, 407, 414, 509, 524). No test asserts on them (verified) — keeping them makes the diff a clean pure move. File a follow-up bead for a prefix-hygiene sweep across the extracted packages.

**12.** Patch the **build-tag-hidden** specaudit path assertion — `internal/specaudit/hc045a_claudecode_bridge_pointer_test.go`:
  - line **92**: `filepath.Join(repoRoot, "internal", "daemon", "claudeharness.go")` → `filepath.Join(repoRoot, "internal", "harness", "claude", "harness.go")`
  - prose references at lines **33**, **46**, **89**, **184**, **293**, **310**, **319**.

  This file is `//go:build specaudit`. It is compiled by **none** of `go build ./internal/...`, `go test ./internal/...`, or `golangci-lint run`, and `scripts/scenario-gate.sh:384-386` triggers the specaudit leg only for changes under `specs/`, `cmd/harmonik/`, or `internal/daemon/socket.go` — so this break would ship green and surface later on an unrelated spec edit. Verify with `make specaudit-lint`.

**13.** Split-and-move the test files:
```
git -C /Users/gb/github/harmonik mv internal/daemon/claudeharness_test.go                    internal/harness/claude/harness_test.go
git -C /Users/gb/github/harmonik mv internal/daemon/claudelaunchspec_test.go                 internal/harness/claude/launchspec_test.go
git -C /Users/gb/github/harmonik mv internal/daemon/claudelaunchspec_configdir_hk8juwz_test.go internal/harness/claude/launchspec_configdir_test.go
git -C /Users/gb/github/harmonik mv internal/daemon/claudelaunchspec_remote_hkz8ek_test.go   internal/harness/claude/launchspec_remote_test.go
```
  - `harness_test.go`, `launchspec_test.go` → `package claude_test`; calls become `claude.NewClaudeHarness`, `claude.BuildLaunchSpec`, `shared.LaunchCtx`.
  - `launchspec_configdir_test.go`, `launchspec_remote_test.go` → `package claude`.
  - Port `ExportedRunCtxFromClaudeRunCtx` (`export_test.go:3100`) into a new `internal/harness/claude/export_test.go` as a package-local helper (`harness_test.go` calls it 9×).
  - **Create the two leave-behind fixture files** (§1): `internal/daemon/claudelaunchspec_fixture_test.go` (`package daemon_test`, `claudeLaunchSpecFixtureWorkspace`) and `internal/daemon/z8ekfixtures_test.go` (`package daemon`, `newNoOpRecorderZ8ek` + `z8ekRunID` + `decodeBase64FromScript`). Keep copies of the three z8ek helpers in `launchspec_remote_test.go` too — both moving `package claude` test files need them.

**14.** Rewire `internal/daemon`. Add `harnessclaude "github.com/gregberns/harmonik/internal/harness/claude"` and rewrite:
  - `harnessregistry.go:49` → `harnessclaude.NewClaudeHarness()`
  - `harnessregistry.go:156` and `:184` → `if _, ok := h.(*harnessclaude.ClaudeHarness); ok {`
  - `harnessregistry.go:157` and `:185` → `return harnessclaude.BuildLaunchSpec(ctx, rc)`
  - `workloop.go:3979`, `dot_cascade.go:1450`, `dot_gate.go:358`, `reviewloop.go:308`, `reviewloop.go:1266` → `= harnessclaude.BuildLaunchSpec`

**15.** **KEEP the `export_test.go` shims — do not delete them.** Ten *staying* daemon test files call `daemon.ExportedBuildClaudeLaunchSpec` (`regression_golden_no_selection_hkhwwlk_test.go`, `modelpreference_hkxo03m_test.go`, `reviewloop_hkgql2015_test.go`, `harnessregistry_test.go`, `hk2jxqg_pinnedharness_test.go`, `dot_prompt_hksdnzj_test.go`, `chb023_review_loop_resume_test.go`, `ci003_ci004a_hk24d72_test.go`, `chb024_qo08q24_test.go`, plus `export_test.go` itself), and `regression_golden_no_selection_hkhwwlk_test.go:399` still calls `ExportedNewClaudeHarness`. Convert both to passthroughs:
```go
var ExportedNewClaudeHarness = harnessclaude.NewClaudeHarness

func ExportedBuildClaudeLaunchSpec(ctx context.Context, rc ExportedClaudeRunCtx) (handler.LaunchSpec, ExportedClaudeRunArtifacts, error) {
    return harnessclaude.BuildLaunchSpec(ctx, rc)
}
```
`export_test.go` therefore **gains** an import of `internal/harness/claude`. `ExportedRunCtxFromClaudeRunCtx` may be deleted from `export_test.go` (its only callers moved with `harness_test.go` — verify with `grep -rn ExportedRunCtxFromClaudeRunCtx internal/`).

**16.** Run the full §6 gate. Then the linkability proof, then `ubs`, then the runtime proof (§6 steps 7–9).

---

## 5. Freeze tripwire

### 5.1 The deny-back edge (literal YAML)

Insert into `.golangci.yml` under `linters-settings.depguard.rules`, immediately after the `gitprobe` block (currently ends line 178). Two rules, not one — the tight `harness-shared` rule is what stops a harness impl from being dumped into `shared`, and is why `internal/workspace` can be allowed to the impls without leaking into the shared leaf.

```yaml
        # harness-shared: the cross-harness leaf (launch DTO, model/effort validators,
        # seed prompts). TIGHTER than the impls on purpose: no workspace, no gitprobe.
        # If a helper here needs workspace it is not shared — it belongs in one impl.
        # Rationale: plans/2026-07-21-p2-extraction/_plan.md §6 "Shared harness helpers".
        harness-shared:
          files: ["**/internal/harness/shared/**"]
          allow:
            - "$gostd"
            - "github.com/google/uuid"
            - "github.com/gregberns/harmonik/internal/core"
            - "github.com/gregberns/harmonik/internal/handler"
            - "github.com/gregberns/harmonik/internal/handlercontract"
            - "github.com/gregberns/harmonik/internal/lifecycle/tmux"
            - "github.com/gregberns/harmonik/internal/harness/shared"
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "P2 E1: harness/shared is a leaf; a daemon edge re-links the 58k-LOC monolith into every P3 container" }
            - { pkg: "github.com/gregberns/harmonik/internal/workspace", desc: "P2 E1b: workspace is an IMPL dependency (claude settings/trust/theme materialization), not a shared one — keep the shared leaf narrow" }

        # harness-impl: the concrete harness implementations (claude, codex, pi).
        # Each must be linkable WITHOUT internal/daemon — that linkability IS the
        # P3 "minimal harmonik in a container" property (plan §4). If any impl ever
        # imports daemon, the container silently drags the monolith back in.
        # Rationale: plans/2026-07-21-p2-extraction/_plan.md units E1a/E1b/E1c, §3.1.
        harness-impl:
          files:
            - "**/internal/harness/**"
            - "!**/internal/harness/shared/**"
          allow:
            - "$gostd"
            - "github.com/google/uuid"
            - "github.com/gregberns/harmonik/internal/core"
            - "github.com/gregberns/harmonik/internal/handler"
            - "github.com/gregberns/harmonik/internal/handlercontract"
            - "github.com/gregberns/harmonik/internal/workspace"
            - "github.com/gregberns/harmonik/internal/lifecycle/tmux"
            - "github.com/gregberns/harmonik/internal/gitprobe"
            - "github.com/gregberns/harmonik/internal/harness/shared"
            - "github.com/gregberns/harmonik/internal/harness/claude"
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "P2 E1: harness impls MUST NOT import daemon back; a daemon edge re-links the 58k-LOC monolith into every P3 container" }
```

(E1c will add `internal/harness/pi` and E1a `internal/harness/codex` to the `harness-impl` allow list — self-import is required by the external-test-package pattern, same idiom as `gitprobe`/`crew`/`keeper`.)

**Note on the reverse direction:** after E1b, `internal/daemon`'s **test** tree imports `internal/harness/claude` (via the retained `export_test.go` shims) and `internal/harness/shared`. That is not a cycle and not a violation — there is no depguard rule scoped to `internal/daemon`'s imports at all today (only the two global bans, `beads-direct-access-ban` and `llm-sdk-ban`). So the *only* valid linkability proof is the one-way `go list -deps` check in §6 step 6. Do **not** expect the daemon→harness edge to vanish.

### 5.2 The "no new files in daemon for this concern" guard — **DEFERRED, and say so**

Plan §3.2 wants a guard failing the build on a new `internal/daemon/*harness*.go` or `*launchspec*.go`. **It cannot be armed at E1b** and the plan does not say so: after E1b the daemon still *legitimately* holds `harnessregistry.go`, `harnessresolve.go`, `reviewerharness_hkiv748.go`, `crewlaunchspec.go`, `codexlaunchspec.go`, `pilaunchspec.go`. Arming it now fails the build on files that are supposed to be there.

**E1b arms §3.1 (the deny-back edge) only.** State that explicitly in the PR body so the plan's headline enforcement is not silently skipped.

When **E1c** lands (all three impls out), arm it as a CI grep-guard — add to `scripts/scenario-gate.sh` alongside the specaudit block, and to the `Makefile` `lint` target:

```sh
# P2 §3.2 freeze: extracted concern = closed door. Arm ONLY after E1c.
if ls internal/daemon/*launchspec*.go 2>/dev/null | grep -qv -e 'crewlaunchspec\.go$'; then
    echo "FREEZE VIOLATION: harness launchspec files belong in internal/harness/<impl>/ (P2 §3.2)" >&2
    exit 1
fi
```
(`crewlaunchspec.go` is the standing exception until unit E2 moves it; `harnessregistry.go` / `harnessresolve.go` / `reviewerharness_hkiv748.go` are registry *assembly*, which stays in daemon as the composition root per plan §6 — so glob on `*launchspec*.go`, not `*harness*.go`.)

---

## 6. Verification gate

Run in order. Every command must be run from `/Users/gb/github/harmonik`. Run the **whole list** at the end of the prep slice **and** at the end of the move slice.

| # | Command | Pass criterion |
|---|---|---|
| 1 | `go build ./internal/... ./cmd/...` | Exit 0, no output. **Not `./...`** — that is red today on unvendored zmq deps under `plans/2026-07-15-agent-substrate-v2/investigate/10-zeromq-experiments/`, and would be misread as extraction damage. |
| 2 | `go vet ./internal/...` | Exit 0 |
| 3 | `go vet -tags=scenario ./internal/daemon/` <br> `go vet -tags=e2e_real_claude ./internal/daemon/` <br> `go vet -tags=specaudit ./internal/specaudit/` | Exit 0 each. **Non-optional.** Three of the affected files are tag-gated (`scenario_remote_substrate_t4_claude_test.go`, `e2e_real_claude_reviewloop_test.go`, `internal/specaudit/hc045a_claudecode_bridge_pointer_test.go`) and are compiled by *none* of steps 1/2/4/5. |
| 4 | `go test ./internal/daemon/... ./internal/harness/... -count=1` | All green. Same test names, same count as the pre-change baseline (record it in step 0.3). |
| 5 | `golangci-lint run` | Green. The new `harness-shared` / `harness-impl` depguard blocks are the boundary test (plan §3.4). |
| 6 | `go list -deps ./internal/harness/claude \| grep gregberns/harmonik/internal/daemon` | **Prints NOTHING.** This is the machine-checkable proof the unit is linkable without the monolith — the whole point of E1 (plan §4). Also run it for `./internal/harness/shared`. |
| 7 | `make specaudit-lint` | Green. This is the only gate that catches the `hc045a_claudecode_bridge_pointer_test.go` path assertion — `scripts/scenario-gate.sh` will **not** trigger it for a change under `internal/daemon/`. |
| 8 | `ubs $(git -C /Users/gb/github/harmonik diff --name-only --cached)` | Exit 0 |
| 9 | Runtime proof (plan §5.4) | With the daemon up: run one claude bead through DOT impl→review→merge, **and** one cognition-gate node (`dot_gate.go` path, which is the codex/pi-routing site that shares `LaunchCtx`). Confirm identical event sequence vs pre-extraction. |
| 10 | LOC measurement | See below — publish in the bead close comment. |

**Step 10 — the success metric:**
```
find /Users/gb/github/harmonik/internal/daemon -name '*.go' ! -name '*_test.go' | wc -l
find /Users/gb/github/harmonik/internal/daemon -name '*.go' ! -name '*_test.go' -exec cat {} + | wc -l
```

| | non-test files | non-test LOC |
|---|---|---|
| Baseline (2026-07-22) | 134 | 58,971 |
| After **E1b-prep** (structs + validators evicted, no file moves) | 134 | ~58,713 (**−258**) |
| After **E1b** (2 files move out) | **132** (**−2**) | ~58,128 (**−585**) |
| **Total for the unit** | **−2 files** | **−843 non-test LOC** |

Plus **1,378 test LOC** leaving `internal/daemon` (481 + 548 + 127 + 222), minus ~40 LOC of leave-behind fixtures. Net moved: **~2,148 LOC**.

---

## 7. Risks and how each is mitigated

**R1 — `claudeRunCtx` is a misnomer, not a claude type. THE HEADLINE RISK.**
It is the daemon's *universal* launch DTO: it is the parameter type of the `LaunchPort.BuildSpec` seam (`runports.go:166`) and is consumed by codex and pi via `buildCodexRoutedLaunchSpec` (`harnessregistry.go:202`). It carries five pi-only fields (`provider`, `apiKeyEnv`, `apiKeyFile`, `baseURL`, `api`) that `buildClaudeLaunchSpec` never reads. 5 of the 18 files that build it through the exported shim are pi tests and 1 is a codex test. **Anyone who plans E1b as "a `git mv` like E1a" discovers this mid-move and is tempted to fix it with a duplicate struct.** Mitigation: the prep slice, and the name `shared.LaunchCtx` — never `claude.RunCtx`.

**R2 — E1b is structurally harder than E1a/E1c even though the plan groups them.**
Codex and pi detach through `handlercontract.Harness` with their own private ctx types. Claude does not: `harnessregistry.go:156,184` short-circuit past the seam because `handlercontract.SpawnSpec` cannot carry `claudeRunArtifacts`. Widening `SpawnSpec` would be a NEW seam, which the plan forbids. Mitigation: exit behind `LaunchPort` with a relocated (not new) DTO; call this out explicitly in the PR so the "unwanted abstraction" reviewer check has the answer up front.

**R3 — Merge-conflict exposure (plan R3).**
The prep slice edits `workloop.go`, `dot_cascade.go`, `reviewloop.go`, `dot_gate.go` — the three hottest files in the tree plus the gate — and **all four are staged-modified on this branch right now**. Mitigation: pre-flight step 0.1 (working tree clean on those four before you start), and land the prep slice **the same day it is written**. Do not leave a half-renamed tree overnight.

**R4 — Three test fixtures are consumed by tests that STAY.**
`claudeLaunchSpecFixtureWorkspace` (6 calls from `modelpreference_hkxo03m_test.go`); `z8ekRunID` / `newNoOpRecorderZ8ek` / `decodeBase64FromScript` (calls from `conformance_m4c7_test.go`, `harnessregistry_remote_hkr36v_test.go`, `harnessregistry_pi_remote_runner_m4c4_test.go` — all pi/codex-remote tests). A plain `git mv` breaks the daemon test binary. Mitigation: recipe step 13's two leave-behind files. Caught by gate step 4 if missed.

**R5 — Ten staying test files depend on the `export_test.go` shims.**
The recon said to delete them; that is flatly wrong. Mitigation: recipe step 15 converts them to passthroughs. `export_test.go` gains an import of `internal/harness/claude` — that is expected and is not a depguard violation.

**R6 — A `//go:build specaudit` test hardcodes `internal/daemon/claudeharness.go`.**
`internal/specaudit/hc045a_claudecode_bridge_pointer_test.go:92` builds the path and `t.Fatalf`s if `os.Open` fails, then greps the file for `"HC-045a"`. It is caught by **none** of the default gate commands, and `scripts/scenario-gate.sh:384-386` triggers the specaudit leg only for `specs/*`, `cmd/harmonik/*`, `internal/daemon/socket.go` — so this ships green and detonates later. Mitigation: recipe step 12 (patch the constant + 7 prose refs) and gate step 7 (`make specaudit-lint`). Also preserve the literal `HC-045a` in the moved header comment.

**R7 — Build-tag blind spot in the gate generally.**
Two of the 42 affected daemon test files are tag-gated (`scenario`, `e2e_real_claude`). Today both mention `buildClaudeLaunchSpec` only in comments, so they survive the rename by **luck, not verification**. Mitigation: gate step 3 compiles all three tag sets.

**R8 — Exported field spellings.**
`export_test.go`'s existing mirror spells `APIKeyEnv` / `APIKeyFile` / `BaseURL` / `API` / `PriorClaudeSessID` / `WorkerBinaryPath`. A mechanical Title-case rule gives `ApiKeyEnv`/`ApiKeyFile`/`BaseUrl`/`Api` and breaks the 18 files that build the shim by keyed field name. Mitigation: §3b pins the exact spelling list; pin to the mirror, not to a rule.

**R9 — `ModelPreferenceError` changes package identity.**
`modelpreference_hkxo03m_test.go:131/155/184` does `errors.As` against `*daemon.ExportedModelPreferenceError`, which is a type alias (`export_test.go:777`) of `ModelPreferenceError`. Mitigation: `type ModelPreferenceError = shared.ModelPreferenceError` in `modelpreference.go`. A fresh struct definition instead of an alias silently breaks those three assertions.

**R10 — Error strings say `"daemon:"` from a non-daemon package.**
`shared.ModelPreferenceError.Error()` emits `"daemon: ModelPreference: field %q value %q is invalid: %s (HC-055a)"`, and `claude.BuildLaunchSpec` emits eleven `"daemon: buildClaudeLaunchSpec: …"` prefixes. **Policy: keep every one byte-verbatim** — no test asserts on either (verified), and keeping them is what makes the diff a defensible pure move. Mitigation: say so in the PR body, and file a follow-up bead for a prefix-hygiene sweep after E1c.

**R11 — `ClaudeHarness.LaunchSpec` is production-dead and has a latent bug.**
Its `RunCtx → claudeRunCtx` conversion drops `rc.runner` and `rc.workerBinaryPath`; if it were ever routed it would break remote claude runs (hk-z8ek). Its only exerciser is `claudeharness_test.go`. Mitigation: **move it verbatim, resist fixing it during the extraction**, file a follow-up bead. A behavior change smuggled into a pure-move PR is a review reject.

**R12 — `internal/harness/shared` is documented as "stdlib only" and is about to stop being.**
`seedprompt.go`'s header says the package "is a leaf — stdlib only." The prep slice widens it to `core` + `handler` + `handlercontract` + `lifecycle/tmux`. Mitigation: recipe step 3 updates the header in the same commit; the `harness-shared` depguard block denies `workspace` explicitly so the leaf cannot creep further. Import-cycle risk verified nil: `go list -deps` over `core`, `handler`, `handlercontract`, `workspace`, `lifecycle/tmux` yields a closure of `{brcli, core, handler, handlercontract, handlercontract/lifecycle, lifecycle, lifecycle/tmux, queue, workspace}` — zero daemon packages.

**R13 — Scope creep from the two other `claude*` files.**
`claudeheartbeat.go` and `claudeworktreesweep.go` are both named `claude*` and both listed in the plan's E1 file set, but neither is behind `handlercontract.HarnessRegistry`. Pulling them in turns an 843-LOC move into a ~1,235-LOC move with a split test file that provably cannot leave (`TestHkYhq3m_RunOrphanSweep_ClaudeWorktreesSwept` drives daemon's `RunOrphanSweep` through package-private fixtures). Mitigation: settle it with the plan §3.4 boundary test — **if it is not reachable from `handlercontract.HarnessRegistry`, it is not in E1b.**

**R14 — "Why does the claude harness extraction leave the claude REPL driver behind?"**
`pasteinject.go` (2,637 LOC, 94 claude mentions) is the real claude Seed/Retask/Teardown implementation. It is called *directly* by workloop/dot_cascade/dot_gate/reviewloop rather than through the `Harness` seam, and is interleaved with harness-blind run-loop machinery. It belongs to E5, or needs its own slice after the seam is widened. Mitigation: put this paragraph in the PR body pre-emptively.

**R15 — Plan §3.2's freeze tripwire cannot be armed at E1b.**
See §5.2. Mitigation: state the deferral explicitly in the PR body so the plan's headline enforcement is a deliberate deferral, not an omission.

---

## 8. Rollback

Both slices are single-commit, independently revertable, and touch no persisted state, no schema, no wire format, and no on-disk layout.

**Mid-flight in the prep slice (nothing committed):**
```
git -C /Users/gb/github/harmonik checkout -- internal/daemon internal/harness
git -C /Users/gb/github/harmonik clean -fd internal/harness/shared
git -C /Users/gb/github/harmonik switch -   # leave p2-e1b-prep
```
Nothing else in the tree knows the branch existed.

**Prep slice landed, move slice going wrong:**
```
git -C /Users/gb/github/harmonik reset --hard HEAD    # or: git switch - && git branch -D p2-e1b-claude
```
Leaving the prep slice landed is a **valid resting state** — that is exactly why it is a separate release. `shared.LaunchCtx` / `shared.LaunchArtifacts` / `shared.ValidateModel` / `shared.ValidateEffort` are strictly better factoring on their own merits (the DTO stops being mis-named after a claude file, and the validators stop being daemon-private), and they leave E1a/E1c strictly easier. **Do not roll the prep slice back just because the move failed.**

**Move slice landed, defect found later:**
```
git -C /Users/gb/github/harmonik revert <move-commit-sha>
```
A single revert restores `internal/daemon/claudeharness.go` + `claudelaunchspec.go`, deletes `internal/harness/claude/`, restores the four test files and removes the two leave-behind fixture files, and drops the two depguard blocks. Re-run gate steps 1–7 to confirm. **Do not hand-unwind the move file-by-file** — the depguard blocks and the specaudit path constant are easy to leave half-applied, and a stale `harness-impl` block with no `internal/harness/claude` directory is silently inert (depguard file-globs that match nothing do not error).

**Point of no return:** none within the unit. The only irreversible act is `harmonik promote` / merge to `main`, and both slices are behaviorally inert by construction (zero logic delta, proven by gate steps 4 and 9). If the runtime proof (gate step 9) shows any event-sequence divergence, revert first and diagnose from the reverted tree — do not patch forward.
