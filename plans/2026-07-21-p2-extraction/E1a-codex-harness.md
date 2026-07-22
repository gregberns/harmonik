# Unit E1a — Codex harness implementation → `internal/harness/codex`

**Status:** NEEDS PREPARATORY SLICE — ship as two consecutive releases, **E1a-0** (shared trailer/env leaf + pi/daemon rewire + depguard) then **E1a-1** (the codex move itself).
**Depends on:** nothing outside the working tree. It builds on two things already staged-but-uncommitted on `phase1-session-restart-substrate`: `internal/gitprobe/` (with its own depguard block, `.golangci.yml:164-178`) and `internal/harness/shared/seedprompt.go`. Do not revert either.
**Size:** ~5,522 LOC across 16 moving files (2,083 non-test in 7 files, 3,439 test in 9 files), plus edits to 6 daemon non-test files, 8 daemon test files, 1 `cmd/harmonik` test file, `.golangci.yml`, and `internal/specaudit/wminv003_task_branch_append_only_test.go`.
**Risk:** LOW-MEDIUM — the code itself is cold (41 commits across all 7 files in 90 days) and compiles standalone with zero daemon edges, but the eight call-site edits land in `workloop.go` (323 commits/90d) and `dot_cascade.go` (99), and four CI tiers outside `go test ./...` can fail on a tree that otherwise looks green.

---

## 0. How the two inputs were reconciled

Both a recon and an adversarial challenge to it were supplied. **Where they conflict, this document takes the challenge**, because the challenge verified by compiling and by grepping the whole repo rather than by reading `internal/daemon` alone, and I re-verified each contested point independently before writing. Specifically:

| Contested point | Recon said | Challenge said | **Taken** | Why |
|---|---|---|---|---|
| Who consumes the ~40 `Exported*` codex seams in `export_test.go` | "the 8 movable external test files only" | six stay-behind daemon test files also consume them | **Challenge** | Re-verified: `agentseedprompt_test.go:81,91,109,117`, `crossharness_empty_model_test.go:47,53`, `picommit_test.go:148,187,259`, `regression_golden_no_selection_hkhwwlk_test.go:393`, `process_exit_harness_hkf6g7_test.go:121`, `pasteinject_hkzlo8_test.go:184`. Cutting the four blocks wholesale breaks all six. |
| `ExportedCodexProcessExitLaunchSpecBuilder` | recipe says cut it AND keep it, in one sentence | three consumers, must stay | **Challenge** | Re-verified 3 consumers. It builds a `claudeRunCtx`/`claudeRunArtifacts` closure — it is not a codex-unit symbol at all. It stays untouched. |
| `CodexHarness` as a **type** | absent from the inbound list | `harnessregistry_test.go:144-145` and `codex_spawnproof_hk47u9z_test.go:115` use it | **Challenge** | Re-verified both. `harnessregistry_test.go` appears in no recon list at all. |
| `internal/harness/shared` is "stdlib only" | proposes putting `refstrailer.go` there needing core + gitprobe + tmux | that contradicts the resolved architecture and the package's own doc comment | **Challenge** | Re-verified `internal/harness/shared/seedprompt.go:7` literally says "The package is a leaf — stdlib only." §5 below re-states the leaf rule in the form that is actually true and enforces it in YAML. |
| One blanket `internal/harness/**` depguard rule | proposed | permits `shared` → `codex`, destroying the leaf | **Challenge** | Re-verified: every existing block (`crew`, `gitprobe`, `keeper`, `socketrouter`, `core`, `queue`) is per-package. Two rules, §5. |
| Lint pressure to rename | implies revive forces `NewCodexHarness`/`ErrMissingCodexStaleWALMaxBytes` renames | revive flags only the package comment and the `CodexHarness` type stutter | **Challenge** | Taken, but the decision lands the same way for a different reason — see §3b. |
| Verification gate | `go build` / `go vet` / `go test ./...` / golangci-lint | those compile **zero** build-tagged files; `make test-scenario`, `make specaudit-lint`, `make fmt-check` are missing | **Challenge** | Re-verified: `Makefile:99-100` (`-tags=scenario` over `./test/scenario/... ./internal/daemon/...`), `Makefile:570-571`, `Makefile:397`. §6 uses the full gate. |
| Feasibility shape | one PR, "move-with-modest-export-churn" | needs a preparatory slice | **Challenge** | The `codexcommit.go` split rewires the **pi** harness (`picommit.go` has `type piRefsOutcome = codexRefsOutcome`) — that is E1c's subject matter, and plan §3 says every slice must be complete, green and releasable. |

**Where the recon was stronger and is kept:** its central structural claim — that the 7 non-test files reference **zero** package-level daemon symbols and import only clean leaves — survived the challenge's own compile probe and is the fact this whole unit rests on. Its file destinations, the split of `codexcommit.go`, the four stay-behind test files, the LOC counts (2,083 / 3,439, re-measured), and the `os.Getwd()` ambient-dependency note are all correct and adopted verbatim.

**One thing neither input found, and it changes the recipe** (verified below, §3b): because Go test seams in `export_test.go` are visible **only inside that package's own test binary**, the stay-behind daemon tests cannot reach a shim living in `internal/harness/codex/export_test.go`. `daemon`'s `export_test.go` can only re-export symbols that are genuinely **exported** from `codex`. Therefore `codexRunCtx` → `codex.RunCtx` (11 exported fields) and `buildCodexLaunchSpec` → `codex.BuildLaunchSpec` must become real exports. Going through the `handlercontract.Harness` interface instead is not an option: `handlercontract.RunCtx` (`internal/handlercontract/harness.go:85`) carries no `CodexHome`, `BillingEmitter` or `SkipBillingGuard`, so `Harness.LaunchSpec` would run the WAL guard against `os.Getwd()` and the fail-closed billing guard — a behavior change in the tests, not a rewrite.

---

## 1. What moves

### 1.1 E1a-0 — preparatory slice: the cross-harness leaf

| File | LOC | Destination | Notes |
|---|---|---|---|
| `internal/daemon/codexcommit.go` (harness-agnostic half) | ~190 of 353 | **NEW** `internal/harness/shared/refstrailer.go` | `codexRefsOutcome` + its 4 consts + `String()` (`:75-115`), `codexRefsTrailerLine` (`:116`), `worktreeHEADHasRefsTrailer` (`:130`), `codexWorktreeDirty` (`:162`), `commitAllWithHarnessRefsTrailer` (`:265`), `amendHEADAddRefsTrailer` (`:305`), `containsExactLine` (`:346`). Imports `context/fmt/os-exec/strings` + `internal/core` + `internal/gitprobe` + `internal/lifecycle/tmux`. |
| `internal/daemon/codexlaunchspec.go` (`envKey`, `:326-332`) | 6 | **NEW** `internal/harness/shared/env.go` | Verbatim, renamed `EnvKey`. `pilaunchspec.go:427,441` also calls it. |
| `internal/daemon/picommit.go` | 140 | **STAYS** (rewired) | 10 symbols repointed at `shared.*`. `:50` `type piRefsOutcome = codexRefsOutcome` is the reason this slice exists. |
| `internal/daemon/pilaunchspec.go` | 470 | **STAYS** (rewired) | `:427`, `:441` → `shared.EnvKey`. |
| `internal/daemon/reviewtrailers_hkdyim.go` | 95 | **STAYS** (rewired) | `:73`, `:74` → `shared.ContainsExactLine`. |
| `internal/daemon/export_test.go` | 3,400 | **STAYS** (one shim retargeted) | `ExportedWorktreeHEADHasRefsTrailer` now calls `shared.WorktreeHEADHasRefsTrailer(ctx, nil, wtPath, beadID)`. Keep the exact signature — it passes a nil `tmux.CommandRunner`. |
| `internal/daemon/codexcommit_test.go`, `internal/daemon/picommit_test.go` | 320 / — | **STAY in E1a-0** | Both go on compiling unchanged through the retargeted shim. `codexcommit_test.go` moves in E1a-1. |
| `.golangci.yml` | — | edited | Both depguard rules (§5) land here, in E1a-0, so the brake exists before the mass arrives. |
| `internal/specaudit/wminv003_task_branch_append_only_test.go` | — | edited | Allowlist key `internal/daemon/codexcommit.go` (`:416`) must gain `internal/harness/shared/refstrailer.go` — that is where `git commit --amend` (`codexcommit.go:331,336`) ends up. |

### 1.2 E1a-1 — the move

| File | LOC | Destination | Notes |
|---|---|---|---|
| `internal/daemon/codexharness.go` | 199 | `internal/harness/codex/harness.go` | `CodexHarness`→`Harness`, `NewCodexHarness`→`NewHarness`. Keeps the `os.Getwd()` at `:97` verbatim (§7). |
| `internal/daemon/codexlaunchspec.go` (remainder) | 328 | `internal/harness/codex/launchspec.go` | `codexRunCtx`→exported `RunCtx`, `buildCodexLaunchSpec`→`BuildLaunchSpec`. Already imports `internal/harness/shared`. |
| `internal/daemon/codexcommit.go` (codex half) | ~163 | `internal/harness/codex/commit.go` | `ensureCodexRefsTrailer` (`:205`)→`EnsureRefsTrailer`; `commitAllWithRefsTrailer` (`:291`), the codex fallback message, stays private. |
| `internal/daemon/codexjsonlparser.go` | 374 | `internal/harness/codex/jsonlparser.go` | `CodexEventKind*` consts (`:58-76`) de-stuttered to `EventKind*`; only consumer is the export shim. |
| `internal/daemon/codexwalguard.go` | 412 | `internal/harness/codex/walguard.go` | `ErrMissingCodexStaleWALMaxBytes`→`ErrMissingStaleWALMaxBytes`. |
| `internal/daemon/codexbillingguard.go` | 306 | `internal/harness/codex/billingguard.go` | Self-contained. |
| `internal/daemon/codexnowork_hk368i4.go` | 105 | `internal/harness/codex/nowork.go` | `codexNoWorkSuspected`/`codexNoWorkFloor`/`emitImplementerNoWorkSuspected` exported. |
| — | — | **NEW** `internal/harness/codex/doc.go` | `package codex` doc comment naming the origin and this plan (revive `package-comments`). |
| `internal/daemon/codexharness_test.go` | 474 | `internal/harness/codex/harness_test.go` | `package daemon_test`→`package codex_test`. |
| `internal/daemon/codexlaunchspec_test.go` | 787 | `internal/harness/codex/launchspec_test.go` | Same. |
| `internal/daemon/codexcommit_test.go` | 320 | `internal/harness/codex/commit_test.go` | Same; `ExportedWorktreeHEADHasRefsTrailer` here resolves through `shared`. |
| `internal/daemon/codexjsonlparser_test.go` | 375 | `internal/harness/codex/jsonlparser_test.go` | Same. |
| `internal/daemon/codexbillingguard_test.go` | 485 | `internal/harness/codex/billingguard_test.go` | Same. |
| `internal/daemon/codexnowork_hk368i4_test.go` | 135 | `internal/harness/codex/nowork_test.go` | Same. |
| `internal/daemon/codexthreadid_mzgh_test.go` | 285 | `internal/harness/codex/threadid_test.go` | Same. |
| `internal/daemon/codexwalguard_test.go` | 319 | `internal/harness/codex/walguard_test.go` | **Internal** test: `package daemon`→`package codex`. Uses only in-unit privates (`cleanCodexStaleWAL`, `reapCodexWALBackupDirs`, `walBackupKeepLast`, `walUnchanged`). |
| `internal/daemon/codexwalguard_concurrency_test.go` | 259 | `internal/harness/codex/walguard_concurrency_test.go` | Internal test, same treatment. |
| — | — | **NEW** `internal/harness/codex/export_test.go` | The codex `Exported*` seams cut from `daemon/export_test.go`, recreated verbatim against the now-local privates, so the 9 moved test files are an import-path edit and not a rewrite. |

### 1.3 Files that STAY, and exactly why

| File | Why it stays |
|---|---|
| `internal/daemon/harnessregistry.go` (260) | Daemon is the composition root (plan §6). `newHarnessRegistry` calls `NewCodexHarness("", "")` once at `:52`; `buildCodexRoutedLaunchSpec` is daemon-side routing. Rewire the one line. |
| `internal/daemon/workloop.go` (8,207) | E5's subject. Four call sites at `:5135, :5147, :5148, :5152`. |
| `internal/daemon/dot_cascade.go` (2,683) | E5's subject. Four call sites at `:2016, :2026, :2027, :2031`. |
| `internal/daemon/export_test.go` (3,400) | Retains four seams whose consumers stay: `ExportedCodexRunCtx`, `ExportedBuildCodexLaunchSpec`, `ExportedNewCodexHarness`, `ExportedWorktreeHEADHasRefsTrailer`; plus `ExportedCodexProcessExitLaunchSpecBuilder` and `ExportedShellQuoteArg`, which are not codex-unit symbols. |
| `internal/daemon/codex_spawnproof_hk47u9z_test.go` (280) | A **stalewatch** test wearing a codex name. Needs `newMutableClock`, `nsrBuildWatcher`, `nsrNewRunID`, `nsrSimulateEvent` from `stalewatch_neverSpawnedReaper_hk0z5x_test.go`, plus `RunRegistry`. Rewire `&daemon.CodexHarness{}` at `:115` → `&codex.Harness{}`. |
| `internal/daemon/codex_daemon_commit_hkgd9r_test.go` (185) | Full `RunWorkLoop` integration test on `workloop_test.go` stubs. **This is E1a's behavior oracle** — it must stay green across the move. Uses `ExportedCodexProcessExitLaunchSpecBuilder` at `:115`. |
| `internal/daemon/codex_empty_model_hkd170r_test.go` (365) | Tests daemon-side harness **routing** (`routedLaunchSpecBuilder`, `resolveHarnessAgentTypeQuiet`, `resolveModelPreference`, `ProjectConfig`) and defines `hkd170rGatedRunCtx`, consumed by `harnessregistry_stdin_devnull_hkj0p1r_test.go`. |
| `internal/daemon/crossharness_empty_model_test.go` | Locks the deliberate codex-vs-pi empty-model **asymmetry in one table**. Splitting it would destroy the invariant it exists to pin. Stays until E1c, then the whole file moves. |
| `internal/daemon/agentseedprompt_test.go` | **The fifth cross-harness blocker, and it is not codex-named.** Asserts pi + codex resume-seed-prompt parity in one table (`:18,30,53,63` pi; `:81,91,109,117` codex). Its production half already moved to `internal/harness/shared/seedprompt.go`. Same shape as `crossharness_empty_model_test.go`; stays until E1c. |
| `internal/daemon/picommit_test.go` | Pi's own tests; reaches codex-origin machinery only via `ExportedWorktreeHEADHasRefsTrailer` (`:148,187,259`), which E1a-0 retargets at `shared`. |
| `internal/daemon/harnessregistry_test.go` | Absent from the recon entirely. `:144-145` does `h.(*daemon.CodexHarness)` and prints `want *daemon.CodexHarness`. Rewire both, including the message string. |
| `internal/daemon/conformance_m4c7_test.go` (`:102,189`), `internal/daemon/harnessregistry_remote_hkr36v_test.go` (`:47,102`) | **Internal** (`package daemon`) tests calling bare `NewCodexHarness`. Rewire to `codex.NewHarness`, adding the import. |
| `internal/daemon/dot_gate_reviewer_harness_hk01vs0_test.go` (`:182`) | External test; `daemon.NewCodexHarness` → `codex.NewHarness`. |
| `internal/daemon/regression_golden_no_selection_hkhwwlk_test.go` (`:393`) | Uses `daemon.ExportedNewCodexHarness`; compiles unchanged against the retained shim. |
| `internal/daemon/process_exit_harness_hkf6g7_test.go` (`:121`), `internal/daemon/pasteinject_hkzlo8_test.go` (`:184`) | Use `ExportedCodexProcessExitLaunchSpecBuilder`; unchanged. |
| `cmd/harmonik/init_codex_block_hkyhvrh_test.go` | **Outside `internal/daemon` — the easiest thing to miss.** `:94` `daemon.NewCodexHarness` and `:101` `var missing *daemon.ErrMissingCodexStaleWALMaxBytes` both break. |
| `cmd/harmonik/codex_config_example.go` (`:37-38`) | Non-test, comment-only. Documents the round-trip invariant in terms of `daemon.CodexHarness.LaunchSpec` and `*daemon.ErrMissingCodexStaleWALMaxBytes` and tells the next author the guard lives "in internal/daemon". No compile break; update the prose or it lies. |
| `test/scenario/codex_adapter_lifecycle_hkvfmn9_test.go` (`//go:build scenario`) | Drives the full codex-adapter DOT lifecycle through `daemon.Config` and the twin binary. Touches no unit private, so it will not break — which is exactly why it is the best behavior-preservation oracle available. Run it (§6). |

---

## 2. The seam it exits behind

**`handlercontract.Harness`** — the interface, defined in `internal/handlercontract/harness.go` (the `RunCtx` it takes is at `:85`, and the interface's doc references `CodexHarness` at `:249`). The registry is `handlercontract.HarnessRegistry`, and the wiring already exists: `internal/daemon/harnessregistry.go:52` does `reg.Register(core.AgentTypeCodex, NewCodexHarness("", ""))`, and `codexharness.go:72` already asserts `var _ handlercontract.Harness = (*CodexHarness)(nil)`.

The two effectful ports the unit needs are also pre-existing interface parameters, injected by the daemon, with no daemon type on the edge:

- `tmux.CommandRunner` — `internal/lifecycle/tmux/runner.go:16` — every git helper in `codexcommit.go` already takes it, and already handles `nil` as "bare local exec".
- `handlercontract.EventEmitter` — used by `codexbillingguard.go`, `codexnowork_hk368i4.go`, and `codexlaunchspec.go` (`rc.billingEmitter`).

**No new seam is invented, and none is needed.** If an implementer finds themselves defining a new interface to complete this move, stop — that is a RED FLAG and means something outside the unit boundary got dragged in. The correct response is to shrink the unit, not to widen the seam.

---

## 3. Coupling to break

### 3a. Outbound (unit → daemon)

| Symbol | Defined in | Used by | Resolution |
|---|---|---|---|
| *(none)* | `internal/daemon/*.go` | all 7 unit files | **NO ACTION.** Whole-package type resolution found zero package-level daemon symbols referenced by the 7 files; an independent compile probe (7 files copied into a standalone package) built and vetted clean with zero undefined symbols. This is the fact the unit rests on. |
| direct imports (verified exhaustively) | — | the 7 files | `bytes`, `context`, `encoding/json`, `errors`, `fmt`, `io`, `io/fs`, `log/slog`, `os`, `os/exec`, `path/filepath`, `sort`, `strings`, `sync`, `time` + `internal/core`, `internal/gitprobe`, `internal/handler`, `internal/handlercontract`, `internal/harness/shared`, `internal/lifecycle/tmux` + `github.com/google/uuid`, `gopkg.in/yaml.v3`. **No `internal/workspace`** — the plan's §2 claim of a workspace ref does not hold for the codex third. |
| `ImplementerResumeSeedPrompt` | `internal/harness/shared/seedprompt.go:47` | `codexlaunchspec.go:195` | Already resolved in the working tree; `codexlaunchspec.go` already imports `internal/harness/shared`. |
| `ResolveWorktreeHEADVia` | `internal/gitprobe/gitprobe.go` | `codexcommit.go:223` | Already resolved; `gitprobe` is depguard-fenced (`.golangci.yml:171-178`) and the codex allow list includes it. |
| `tmux.CommandRunner` | `internal/lifecycle/tmux/runner.go:16` | every git helper in `codexcommit.go` | Inject-as-port; already an interface parameter. Add `internal/lifecycle/tmux` to both new depguard allow lists. |
| `handlercontract.EventEmitter` | `internal/handlercontract` | `codexbillingguard.go`, `codexnowork_hk368i4.go`, `codexlaunchspec.go` | Inject-as-port; already the seam. |
| `newMutableClock`, `nsrBuildWatcher`, `nsrNewRunID`, `nsrSimulateEvent`, `RunRegistry` | `internal/daemon/stalewatch_neverSpawnedReaper_hk0z5x_test.go`, `runregistry.go` | `codex_spawnproof_hk47u9z_test.go` | **Do not move that file.** It is a stalewatch test. |
| `stubBeadLedger`, `stubEventCollector`, `workloopFixtureGitRepo`, `workloopFixtureProjectDir`, `ExportedRunWorkLoop`, `WorkLoopDepsParams` | `internal/daemon/workloop_test.go`, `testhelpers_adapterregistry_test.go` | `codex_daemon_commit_hkgd9r_test.go` | **Do not move that file.** It is the runtime oracle. |
| `ProjectConfig`, `ExportedRoutedLaunchSpecBuilder`, `ExportedResolveHarnessAgentTypeQuiet`, `ExportedResolveModelPreference` | `internal/daemon/projectconfig.go`, `export_test.go` | `codex_empty_model_hkd170r_test.go` | **Do not move that file.** It tests routing, and it defines `hkd170rGatedRunCtx` for a sibling. |
| `ExportedPiRunCtx`, `ExportedBuildPiLaunchSpec` | `internal/daemon/export_test.go` | `crossharness_empty_model_test.go`, `agentseedprompt_test.go` | Blocked until E1c. Both files stay; keep the codex-side shims alive for them (§3b). |

### 3b. Inbound (daemon → unit)

**Total: 18 symbols need a new export** (12 into `shared`, 6 into `codex`), plus 3 already-exported names renamed to shed the `Codex` stutter, plus 5 already-exported enum constants renamed, plus 11 struct fields exported as part of `RunCtx`.

**E1a-0 — into `internal/harness/shared` (12):**

| Current (unexported, `internal/daemon`) | Proposed | Call sites to rewrite |
|---|---|---|
| `codexRefsOutcome` (`codexcommit.go:75`) | `shared.RefsOutcome` | `picommit.go:50` (`type piRefsOutcome = …`), `export_test.go` type alias |
| `codexRefsAlreadyPresent` (`:78-94`) | `shared.RefsAlreadyPresent` | `picommit.go:53`, `codexcommit.go` internal, `export_test.go` |
| `codexRefsAmended` | `shared.RefsAmended` | `picommit.go:54`, `export_test.go` |
| `codexRefsCommitted` | `shared.RefsCommitted` | `picommit.go:55`, `export_test.go` |
| `codexRefsNoChange` | `shared.RefsNoChange` | `picommit.go:56`, `export_test.go` |
| `codexRefsTrailerLine` (`:116`) | `shared.RefsTrailerLine` | `commitAllWithHarnessRefsTrailer:266`, `amendHEADAddRefsTrailer` |
| `worktreeHEADHasRefsTrailer` (`:130`) | `shared.WorktreeHEADHasRefsTrailer` | `picommit.go:85`, `codexcommit.go:215`, `export_test.go` shim (passes nil runner) |
| `codexWorktreeDirty` (`:162`) | `shared.WorktreeDirty` | `picommit.go:110`, `codexcommit.go:240` |
| `commitAllWithHarnessRefsTrailer` (`:265`) | `shared.CommitAllWithHarnessRefsTrailer` | `picommit.go:131`, `codexcommit.go:291` |
| `amendHEADAddRefsTrailer` (`:305`) | `shared.AmendHEADAddRefsTrailer` | `picommit.go:102`, `codexcommit.go:233` |
| `containsExactLine` (`:346`) | `shared.ContainsExactLine` | `reviewtrailers_hkdyim.go:73`, `:74`, `amendHEADAddRefsTrailer` |
| `envKey` (`codexlaunchspec.go:328`) | `shared.EnvKey` | `codexlaunchspec.go:278`, `pilaunchspec.go:427`, `pilaunchspec.go:441` |

**E1a-1 — into `internal/harness/codex` (6 new exports):**

| Current | Proposed | Call sites to rewrite |
|---|---|---|
| `ensureCodexRefsTrailer` (`codexcommit.go:205`) | `codex.EnsureRefsTrailer` | `workloop.go:5135`, `dot_cascade.go:2016` |
| `codexNoWorkSuspected` (`codexnowork_hk368i4.go:76`) | `codex.NoWorkSuspected` | `workloop.go:5147`, `dot_cascade.go:2026` |
| `codexNoWorkFloor` (`:61`) | `codex.NoWorkFloor` | `workloop.go:5148`, `dot_cascade.go:2027` |
| `emitImplementerNoWorkSuspected` (`:90`) | `codex.EmitImplementerNoWorkSuspected` | `workloop.go:5152`, `dot_cascade.go:2031` |
| `codexRunCtx` (`codexlaunchspec.go`) **+ its 11 fields** | `codex.RunCtx` with `CodexBinary`, `WorkspacePath`, `BeadID`, `Model`, `PriorThreadID`, `IterationCount`, `BaseEnv`, `CodexHome`, `BillingEmitter`, `RunID`, `SkipBillingGuard` | `daemon/export_test.go` (`ExportedCodexRunCtx` becomes `type ExportedCodexRunCtx = codex.RunCtx`), reached by `agentseedprompt_test.go:81,109` and `crossharness_empty_model_test.go:47` |
| `buildCodexLaunchSpec` | `codex.BuildLaunchSpec` | `daemon/export_test.go` (`var ExportedBuildCodexLaunchSpec = codex.BuildLaunchSpec`), reached by `agentseedprompt_test.go:91,117` and `crossharness_empty_model_test.go:53` |

> **Why the last two rows exist.** They are the item neither input found. `export_test.go` seams live in the package's own test binary only, so `daemon`'s external tests cannot see `codex`'s shim. Picking the exported field names from the existing `ExportedCodexRunCtx` struct (`daemon/export_test.go:2602-2625`) makes the daemon shim a two-line type alias + var, and the two stay-behind parity test files compile **completely unchanged**. Once E1c lands and both harnesses are out, those parity tests move into a cross-harness test package and `RunCtx`/`BuildLaunchSpec` can be un-exported again — record that as an E1c follow-up.

**Renames of already-exported names (3 + 5 enum constants):**

| Current | Proposed | Call sites |
|---|---|---|
| `CodexHarness` (type) | `codex.Harness` | `harnessregistry_test.go:144,145` (incl. the `want *daemon.CodexHarness` message), `codex_spawnproof_hk47u9z_test.go:115`, `cmd/harmonik/codex_config_example.go:37` (comment) |
| `NewCodexHarness` | `codex.NewHarness` | `harnessregistry.go:52`, `conformance_m4c7_test.go:102,189`, `harnessregistry_remote_hkr36v_test.go:47,102`, `dot_gate_reviewer_harness_hk01vs0_test.go:182`, `cmd/harmonik/init_codex_block_hkyhvrh_test.go:94`, `daemon/export_test.go:3129` |
| `ErrMissingCodexStaleWALMaxBytes` | `codex.ErrMissingStaleWALMaxBytes` | `codexwalguard.go:126,128,184`, `codexwalguard_test.go:144,161`, `cmd/harmonik/init_codex_block_hkyhvrh_test.go:101`, `cmd/harmonik/codex_config_example.go:38` (comment) |
| `CodexEventKindOther/ThreadStarted/TurnStarted/TurnCompleted/TurnFailed` (`codexjsonlparser.go:58-76`) | `codex.EventKind*` | `daemon/export_test.go:3135-3140` only — which is itself being cut into `codex/export_test.go`. Zero external consumers, so the rename is free. |

**Rename decision, made now and not mid-move.** revive's ruleset (`.golangci.yml:88`) flags exactly two things when the 7 files are renamed to `package codex`: the missing package comment, and `CodexHarness` as `codex.CodexHarness`. The other renames are **not lint-required**. Do them anyway, because every one of those call sites has to change its qualifier regardless (`daemon.` → `codex.`), so de-stuttering costs zero extra edits and leaves the package with a clean public surface for P3's container. If review pushes back and wants a smaller diff, keeping `NewCodexHarness`/`ErrMissingCodexStaleWALMaxBytes` is defensible — but `CodexHarness`→`Harness` is not optional, revive will fail the build.

---

## 4. Step-by-step recipe

### E1a-0 — the cross-harness leaf (release 1 of 2)

1. **Extend the `internal/harness/shared` package doc.** Edit `internal/harness/shared/seedprompt.go:7` — the sentence "The package is a leaf — stdlib only." becomes false the moment `refstrailer.go` lands. Replace it with the rule that is actually true and is what §5 enforces:
   > The package is a leaf **with respect to the daemon and to every concrete harness**: it may reach the same seam packages the harness impls reach (`core`, `handler`, `handlercontract`, `gitprobe`, `lifecycle/tmux`) and nothing else. It must never import `internal/daemon`, `internal/harness/codex`, `internal/harness/claude`, or `internal/harness/pi`.

   This is an architecture re-confirmation, not a mechanical edit — say so in the commit body and in the review request.

2. **Create `internal/harness/shared/refstrailer.go`.** Move, verbatim except for the identifier renames in §3b, these ranges out of `internal/daemon/codexcommit.go`: `:75-115` (`codexRefsOutcome`, its 4 constants, `String()`), `:116-129` (`codexRefsTrailerLine`), `:130-161` (`worktreeHEADHasRefsTrailer`), `:162-204` (`codexWorktreeDirty`), `:265-288` (`commitAllWithHarnessRefsTrailer`), `:305-345` (`amendHEADAddRefsTrailer`), `:346-353` (`containsExactLine`). Import block: `context`, `fmt`, `os/exec`, `strings`, `internal/core`, `internal/gitprobe`, `internal/lifecycle/tmux`. **Do not touch error strings** — `fmt.Errorf("git commit --amend: %w…")` and friends stay byte-identical; they are observable output.

3. **Create `internal/harness/shared/env.go`** with `EnvKey`, moved verbatim from `internal/daemon/codexlaunchspec.go:326-332`.

4. **Rewire the daemon.** `picommit.go`: `:50` `type piRefsOutcome = shared.RefsOutcome`; `:53-56` the four constants; `:85` `shared.WorktreeHEADHasRefsTrailer`; `:102` `shared.AmendHEADAddRefsTrailer`; `:110` `shared.WorktreeDirty`; `:131` `shared.CommitAllWithHarnessRefsTrailer`; and update the file-header comments at `:17,30,31,47,49` that point at `codexcommit.go`. `pilaunchspec.go:427,441` → `shared.EnvKey`. `codexlaunchspec.go:278` → `shared.EnvKey`. `reviewtrailers_hkdyim.go:73,74` → `shared.ContainsExactLine`. `codexcommit.go` keeps `ensureCodexRefsTrailer` and `commitAllWithRefsTrailer`, now calling `shared.*`.

5. **Retarget the one export shim.** In `internal/daemon/export_test.go`, `ExportedWorktreeHEADHasRefsTrailer` becomes `return shared.WorktreeHEADHasRefsTrailer(ctx, nil, wtPath, beadID)`. Preserve the signature exactly — `picommit_test.go:148,187,259` and `codexcommit_test.go:138,160,229,272` depend on it.

6. **Add both depguard rules** from §5 to `.golangci.yml`, immediately after the `gitprobe:` block (which ends at `:178`).

7. **Update the WM-INV-003 allowlist.** In `internal/specaudit/wminv003_task_branch_append_only_test.go`, add alongside the `internal/daemon/codexcommit.go` entry at `:416`:
   ```go
   "internal/harness/shared/refstrailer.go": "codex-harness C2/T9 (hk-bpxci), relocated by P2 E1a; post-exit Refs-trailer amend before workspace_leased; not a rewrite of an observed task branch",
   ```
   The scanner is `filepath.Walk` over all of `internal/` (`:446`) matching by relative-path prefix (`:463-467`), so a two-level path is scanned and an un-allowlisted `--amend` fails. Verify with `grep -n '"--amend"\|"rebase"\|"--force"' internal/harness/shared/*.go internal/harness/codex/*.go` — every file that matches needs a key. Comment-only lines are skipped by the scanner (`:487-491`), so `codex/commit.go`'s prose references need no entry.

8. **Gate E1a-0** with the full §6 checklist. It must be green *before* any codex file moves.

### E1a-1 — the move (release 2 of 2)

9. **Scaffold.** Create `internal/harness/codex/doc.go` with a `package codex` doc comment naming the origin (`internal/daemon/codex*.go`) and this plan, styled after `internal/harness/shared/seedprompt.go:1-10`.

10. **Move the seven non-test files.** From the repo root, never from a worktree:
    ```bash
    git -C /Users/gb/github/harmonik mv internal/daemon/codexharness.go        internal/harness/codex/harness.go
    git -C /Users/gb/github/harmonik mv internal/daemon/codexlaunchspec.go     internal/harness/codex/launchspec.go
    git -C /Users/gb/github/harmonik mv internal/daemon/codexcommit.go         internal/harness/codex/commit.go
    git -C /Users/gb/github/harmonik mv internal/daemon/codexjsonlparser.go    internal/harness/codex/jsonlparser.go
    git -C /Users/gb/github/harmonik mv internal/daemon/codexwalguard.go       internal/harness/codex/walguard.go
    git -C /Users/gb/github/harmonik mv internal/daemon/codexbillingguard.go   internal/harness/codex/billingguard.go
    git -C /Users/gb/github/harmonik mv internal/daemon/codexnowork_hk368i4.go internal/harness/codex/nowork.go
    ```
    Change `package daemon` → `package codex` in each. Keep every error string byte-identical, including the ones that say `"daemon: ensureCodexRefsTrailer: …"` (`commit.go:206,209,225,234,241,251`) — rewording them is a behavior change and belongs in a follow-up, not in a pure move.

11. **Apply the exports and renames** from §3b: the 6 new codex exports (including `RunCtx` with its 11 exported fields and `BuildLaunchSpec`), `CodexHarness`→`Harness`, `NewCodexHarness`→`NewHarness`, `ErrMissingCodexStaleWALMaxBytes`→`ErrMissingStaleWALMaxBytes`, `CodexEventKind*`→`EventKind*`. Nothing else changes visibility.

12. **Rewire the eight hot-file call sites.** `workloop.go:5135,5147,5148,5152` and `dot_cascade.go:2016,2026,2027,2031` → `codex.EnsureRefsTrailer` / `codex.NoWorkSuspected` / `codex.NoWorkFloor` / `codex.EmitImplementerNoWorkSuspected`, adding the import to each file. Then `harnessregistry.go:52` → `codex.NewHarness("", "")`. **Land these fast** — `workloop.go` took 323 commits in 90 days; a PR that sits will conflict.

13. **Cut and recreate the export shim.** In `internal/daemon/export_test.go`, remove the codex-private seams from the four blocks (around `:2600-2650`, `:3122-3269`, `:3340-3349`) and recreate them **verbatim** in a new `internal/harness/codex/export_test.go` targeting the now-local privates, keeping the exact same `Exported*` names. **Retain in `daemon/export_test.go`, rewritten as thin aliases:**
    ```go
    type ExportedCodexRunCtx = codex.RunCtx
    var ExportedBuildCodexLaunchSpec = codex.BuildLaunchSpec
    var ExportedNewCodexHarness = codex.NewHarness
    ```
    **Leave `ExportedCodexProcessExitLaunchSpecBuilder` (`:2926-2952`) untouched** — it closes over `claudeRunCtx`/`claudeRunArtifacts`, is not a codex-unit symbol, and has three consumers (`process_exit_harness_hkf6g7_test.go:121`, `pasteinject_hkzlo8_test.go:184`, `codex_daemon_commit_hkgd9r_test.go:115`). Likewise leave `ExportedShellQuoteArg` — `shellQuoteArg` lives in `tmuxsubstrate.go:2907`, not in this unit.

14. **Move the nine test files.**
    ```bash
    git -C /Users/gb/github/harmonik mv internal/daemon/codexharness_test.go             internal/harness/codex/harness_test.go
    git -C /Users/gb/github/harmonik mv internal/daemon/codexlaunchspec_test.go          internal/harness/codex/launchspec_test.go
    git -C /Users/gb/github/harmonik mv internal/daemon/codexcommit_test.go              internal/harness/codex/commit_test.go
    git -C /Users/gb/github/harmonik mv internal/daemon/codexjsonlparser_test.go         internal/harness/codex/jsonlparser_test.go
    git -C /Users/gb/github/harmonik mv internal/daemon/codexbillingguard_test.go        internal/harness/codex/billingguard_test.go
    git -C /Users/gb/github/harmonik mv internal/daemon/codexnowork_hk368i4_test.go      internal/harness/codex/nowork_test.go
    git -C /Users/gb/github/harmonik mv internal/daemon/codexthreadid_mzgh_test.go       internal/harness/codex/threadid_test.go
    git -C /Users/gb/github/harmonik mv internal/daemon/codexwalguard_test.go            internal/harness/codex/walguard_test.go
    git -C /Users/gb/github/harmonik mv internal/daemon/codexwalguard_concurrency_test.go internal/harness/codex/walguard_concurrency_test.go
    ```
    For the seven external ones: `package daemon_test` → `package codex_test`, drop the `internal/daemon` import, add `internal/harness/codex`, and `s/daemon\./codex./` on the `Exported*` references. For `walguard_test.go` and `walguard_concurrency_test.go`: `package daemon` → `package codex` (they are internal tests using only in-unit privates). `commit_test.go`'s `ExportedWorktreeHEADHasRefsTrailer` now resolves through the codex shim, which forwards to `shared`.

15. **Rewire the eleven stay-behind test call sites** listed in §1.3: `harnessregistry_test.go:144,145`, `codex_spawnproof_hk47u9z_test.go:115`, `conformance_m4c7_test.go:102,189`, `harnessregistry_remote_hkr36v_test.go:47,102`, `dot_gate_reviewer_harness_hk01vs0_test.go:182`.

16. **Rewire `cmd/harmonik`.** `init_codex_block_hkyhvrh_test.go:94` → `codex.NewHarness("codex", codexHome)` and `:101` → `var missing *codex.ErrMissingStaleWALMaxBytes`, swapping the import. Update the stale prose in `codex_config_example.go:37-38` and `init_codex_block_hkyhvrh_test.go:16,72` so it names `internal/harness/codex` instead of `internal/daemon`. **This directory is outside `internal/daemon`; a grep scoped to the daemon misses it and the build breaks at the very end of the move.**

17. **Add the freeze tripwire** (§5.2) and wire it into CI.

18. **Gate** with the full §6 checklist, then **publish the metric** (§6, last row) in the bead close comment, then run the **runtime proof**: one codex bead through DOT (implement → review → merge) with an outcome identical to a pre-extraction run, per plan §5.4.

---

## 5. Freeze tripwire

### 5.1 The depguard deny edges

Two rules, not one. A single `files: ["**/internal/harness/**"]` rule whose allow list names both `harness/shared` and `harness/codex` would permit `shared` to import `codex` — the exact back-edge that destroys `shared`'s leaf status and the whole reason the split exists. Every existing block in this file (`crew:` `:155`, `gitprobe:` `:171`, `keeper:` `:188`, `core:`, `queue:`, `socketrouter:`) is per-package; follow that idiom. Insert immediately after the `gitprobe:` block ends (`.golangci.yml:178`), inside `linters.settings.depguard.rules`, at the same 8-space indent:

```yaml
        # harness-shared: the cross-harness leaf (P2 unit E1a). Holds the pieces
        # that more than one harness implementation needs and none of them owns:
        # the resume seed prompt (seedprompt.go) and the Refs:<bead> trailer
        # machinery (refstrailer.go, shared by codex and pi — picommit.go's
        # piRefsOutcome is literally an alias of it). It may reach the same seam
        # leaves the harness impls reach, and NOTHING else: not the daemon, and
        # not any concrete harness. Allowing a harness edge here would turn the
        # leaf into a hub and re-couple pi to codex through the back door.
        # Rationale: plans/2026-07-21-p2-extraction/E1a-codex-harness.md §5.
        harness-shared:
          files: ["**/internal/harness/shared/**"]
          allow:
            - "$gostd"
            - "github.com/gregberns/harmonik/internal/core"
            - "github.com/gregberns/harmonik/internal/handler"
            - "github.com/gregberns/harmonik/internal/handlercontract"
            - "github.com/gregberns/harmonik/internal/gitprobe"
            - "github.com/gregberns/harmonik/internal/lifecycle/tmux"
            - "github.com/gregberns/harmonik/internal/harness/shared"
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "harness/shared MUST NOT import daemon — the container links the harness, not the monolith (P2 E1a)" }
            - { pkg: "github.com/gregberns/harmonik/internal/harness/codex", desc: "harness/shared is a leaf BELOW every harness impl; a codex edge re-couples pi to codex (P2 E1a)" }
            - { pkg: "github.com/gregberns/harmonik/internal/harness/claude", desc: "harness/shared is a leaf BELOW every harness impl (P2 E1b)" }
            - { pkg: "github.com/gregberns/harmonik/internal/harness/pi", desc: "harness/shared is a leaf BELOW every harness impl (P2 E1c)" }

        # harness-codex: the codex harness implementation lifted out of
        # internal/daemon (P2 unit E1a). Its whole point is that a P3 container
        # can link THIS package without dragging the 57k-LOC daemon in with it,
        # so the deny edge below is the unit's success criterion expressed in CI:
        # if it ever goes green while codex imports daemon, the extraction has
        # been silently undone. The allow list is the exact, verified import set
        # of the seven moved files — nothing speculative. A denied import is a
        # FINDING: prove the dependency belongs before widening this list.
        # Rationale: plans/2026-07-21-p2-extraction/E1a-codex-harness.md §5.
        harness-codex:
          files: ["**/internal/harness/codex/**"]
          allow:
            - "$gostd"
            - "github.com/gregberns/harmonik/internal/core"
            - "github.com/gregberns/harmonik/internal/handler"
            - "github.com/gregberns/harmonik/internal/handlercontract"
            - "github.com/gregberns/harmonik/internal/gitprobe"
            - "github.com/gregberns/harmonik/internal/lifecycle/tmux"
            - "github.com/gregberns/harmonik/internal/harness/shared"
            - "github.com/gregberns/harmonik/internal/harness/codex"
            - "github.com/google/uuid"
            - "gopkg.in/yaml.v3"
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "harness impls MUST NOT import daemon back — the container links the harness, not the monolith (P2 E1a)" }
```

### 5.2 The "no new files in daemon for this concern" guard

depguard cannot express "this filename may not exist", so this is a grep-guard. Plan §3.2 and the operator's confirmation make it a **hard CI failure**, not a warning. Add to the `Makefile` next to `specaudit-lint` (`:570`) and call it from the same CI job as `golangci-lint`:

```makefile
freeze-check:  ## P2 freeze tripwire: an extracted concern may not reappear in internal/daemon
	@LEAKED=$$(ls internal/daemon/*codex*.go 2>/dev/null | grep -v '_test\.go$$' || true); \
	if [ -n "$$LEAKED" ]; then \
		echo "P2 FREEZE VIOLATION: the codex harness was extracted to internal/harness/codex (unit E1a)."; \
		echo "These files must not exist — put the code in internal/harness/codex instead:"; \
		echo "$$LEAKED"; \
		exit 1; \
	fi
```

Two deliberate choices: it excludes `_test.go` (the four daemon tests that legitimately keep codex names are listed in §1.3 and must stay), and it is a separate target so `make check-short` and the `ci.yml` lint job can both invoke it without duplicating the shell. Hook it in by adding `freeze-check` to the `check-short` recipe (`Makefile:438`) right after `fmt-check`.

---

## 6. Verification gate

Run in this order, from `/Users/gb/github/harmonik`, at the end of **each** of the two slices. The first four are necessary but **not sufficient** — none of them compiles a single build-tagged file, so a tree that passes all four can still fail two CI tiers.

| # | Command | Pass criterion |
|---|---|---|
| 1 | `make fmt-check` | Exit 0. `gofumpt` + `gci` produce no diff — the new import blocks in `workloop.go`, `dot_cascade.go`, `picommit.go`, `pilaunchspec.go` and the new packages must be grouped correctly, or CI Tier 2 fails on formatting alone. |
| 2 | `go build ./internal/... ./cmd/...` | Exit 0, no output. |
| 3 | `go vet ./internal/... ./cmd/...` | Exit 0. |
| 4 | `go test ./internal/daemon/... ./internal/harness/... ./cmd/harmonik/... -count=1` | All green. `codex_daemon_commit_hkgd9r_test.go` in particular — it is the unit's behavior oracle. |
| 5 | `go test ./... -count=1` | All green. Catches import cycles and broken fan-in outside the three trees above. |
| 6 | `.tools/golangci-lint run ./internal/harness/... ./internal/daemon/... ./cmd/...` | Exit 0. Confirm the new rules are actually **loaded**, not just absent: temporarily add `import _ "github.com/gregberns/harmonik/internal/daemon"` to `internal/harness/codex/doc.go`, re-run, see the deny fire, then remove it. A depguard rule nobody has seen fail is not a brake. |
| 7 | `make specaudit-lint` | Exit 0. This is the **only** gate that compiles `-tags specaudit` and therefore the only one that surfaces a missing `wmInv003FixtureAllowlist` entry for the relocated `git commit --amend`. |
| 8 | `make test-scenario` | Exit 0. `-race -tags=scenario` over `./test/scenario/... ./internal/daemon/...` — re-compiles the whole daemon under a second build tag and runs `test/scenario/codex_adapter_lifecycle_hkvfmn9_test.go`, the end-to-end codex DOT lifecycle (`run_started` → implementer Refs commit → reviewer APPROVE → `run_completed` → bead closed) plus `TestScenario_Codex_EmptyModel_FullLifecycle`. |
| 9 | `make freeze-check` | Exit 0 after the move; and confirm it **fails** on a scratch file — `touch internal/daemon/codexfoo.go && make freeze-check` must exit 1, then `rm` it. |
| 10 | `ubs $(git -C /Users/gb/github/harmonik diff --name-only HEAD)` | Exit 0. |
| 11 | Runtime proof (plan §5.4) | One codex bead through DOT — implement → review → merge — with an outcome identical to a pre-extraction run. Required before close; a green test suite is not this. |

### The success metric — measure it, publish it

Baseline, measured on this tree at 2026-07-22 (top-level `internal/daemon`, excluding sub-packages):

```bash
ls internal/daemon/*.go | grep -v _test.go | wc -l          # 126 files
cat $(ls internal/daemon/*.go | grep -v _test.go) | wc -l   # 57,197 LOC
```

Expected after E1a-1: **119 non-test files (−7)** and **≈55,120 non-test LOC (−2,077)** — 2,083 LOC leave, ~6 import lines arrive across the six rewired files. Test side: 9 files and 3,439 LOC leave, and `export_test.go` sheds ~200 lines net of the retained shims. Total mass relocated out of the god package: **5,522 LOC across 16 files.** Publish those two before/after numbers in the bead close comment, per plan §5.6 — the point of the stream is that its progress is legible.

---

## 7. Risks and how each is mitigated

1. **`codexcommit.go` is not movable as a unit, and the plan implies it is.** Plan §2 E1 says "each harness's private run-ctx is self-contained" and reads as a pure `git mv`. True for 6 of 7 files, false for `codexcommit.go`: `picommit.go` consumes ten of its symbols and `:50` is literally `type piRefsOutcome = codexRefsOutcome`. An implementer who trusts the plan and moves the file wholesale breaks the pi harness. **Mitigation:** the `shared/refstrailer.go` split is E1a-0, a separate release, gated green before anything else moves. It is mandatory, not optional.

2. **Cutting `export_test.go`'s codex blocks breaks six stay-behind test files.** `agentseedprompt_test.go`, `crossharness_empty_model_test.go`, `picommit_test.go`, `regression_golden_no_selection_hkhwwlk_test.go`, `process_exit_harness_hkf6g7_test.go`, `pasteinject_hkzlo8_test.go` all consume seams the recon claimed were used only by movable tests. **Mitigation:** §4 step 13 names exactly which seams stay (as aliases) and which are cut, and step 11 exports `RunCtx`/`BuildLaunchSpec` so the aliases can exist at all.

3. **Four — really five — codex-named test files cannot move.** `codex_spawnproof_hk47u9z_test.go`, `codex_daemon_commit_hkgd9r_test.go`, `codex_empty_model_hkd170r_test.go`, `crossharness_empty_model_test.go`, and the non-codex-named `agentseedprompt_test.go`. Moving them fails to compile; deleting them silently destroys the behavior oracle and two cross-harness invariant locks. **Mitigation:** §1.3 lists each with its reason. They keep exercising the extracted package through the registry, which is what you want.

4. **`internal/harness` has no depguard rule today, even though `internal/harness/shared` already exists in the working tree.** Until §5 lands, nothing stops `shared` or `codex` from importing the daemon back. **Mitigation:** both rules land in E1a-0 — the *first* slice — not alongside the mass in E1a-1. Plan §3 freeze-then-strangle: the brake precedes the load.

5. **A blanket `internal/harness/**` depguard rule silently permits `shared` → `codex`.** That is the precise back-edge the split exists to prevent, and it would re-couple pi to codex through the leaf. **Mitigation:** two per-package rules with explicit cross-harness denies (§5.1), matching the `crew`/`gitprobe`/`keeper` idiom already in the file.

6. **The obvious verify list passes on a broken tree.** `go build`, `go vet`, `go test ./...` and `golangci-lint` compile **zero** build-tagged files. Three CI tiers do: `make test-scenario` (`-race -tags=scenario`, `Makefile:99-100`), `make specaudit-lint` (`-tags specaudit`, `Makefile:570`), and `make fmt-check`'s gofumpt+gci diff gate (`Makefile:397`). **Mitigation:** §6 runs all eleven, and step 1 is `fmt-check` precisely because new import blocks are what breaks it.

7. **The WM-INV-003 append-only audit will fail, invisibly to every untagged gate.** `internal/specaudit/wminv003_task_branch_append_only_test.go` walks every `.go` file under `internal/` for `--amend`/`rebase`/`--force` and fails any hit not keyed by literal relative path in `wmInv003FixtureAllowlist`. `:416` allowlists `internal/daemon/codexcommit.go`; after the move the `git commit --amend` at `codexcommit.go:331,336` lives at `internal/harness/shared/refstrailer.go`. **Mitigation:** §4 step 7, in the same slice that moves the code, with the exact grep to confirm no other path matches.

8. **`cmd/harmonik` breaks at the very end.** `init_codex_block_hkyhvrh_test.go:94,101` depends on both `daemon.NewCodexHarness` and `daemon.ErrMissingCodexStaleWALMaxBytes`, and `codex_config_example.go:37-38` documents the load-bearing round-trip invariant in terms of both — telling future authors the guard lives "in internal/daemon". A grep scoped to `internal/daemon` misses all of it. **Mitigation:** §4 step 16 is an explicit step, and §6 step 4 includes `./cmd/harmonik/...`.

9. **The eight call-site edits land in the two hottest files in the repo.** `workloop.go` took 323 commits in the last 90 days and `dot_cascade.go` 99, against 41 across all seven moved files combined. The move itself is cold; the rewire is not. **Mitigation:** land E1a-1 fast and do not let the PR sit. If it does conflict, the conflicts are in eight one-line qualifier changes — re-apply, do not re-plan.

10. **`Harness.LaunchSpec` calls `os.Getwd()` to derive `projectRoot` for the WAL guard** (`codexharness.go:97`). That is an ambient dependency on the daemon's CWD which survives the move unchanged, so the new package is less pure than its depguard closure suggests — a P3 container will have to set CWD deliberately. **Mitigation:** preserve it verbatim; behavior preservation beats purity in a pure-move PR. File it as an E1 follow-up bead, do not "improve" it here.

11. **Exporting `RunCtx` with 11 fields widens the public surface more than a pure move suggests.** It exists only so `daemon/export_test.go` can keep two cross-harness parity tests compiling. **Mitigation:** say so in the doc comment on `RunCtx`, and record the E1c follow-up: once pi is out too, both parity tests move to a cross-harness test package and `RunCtx`/`BuildLaunchSpec` go back to unexported.

12. **Error strings and comments still say "daemon:".** `commit.go` will emit `"daemon: ensureCodexRefsTrailer: …"` from a package called `codex`. **Mitigation:** deliberate. Changing observable output is not a pure move. Follow-up bead.

---

## 8. Rollback

The two slices roll back independently, and that is most of the reason for splitting them.

**Mid-flight in E1a-1 (nothing committed):**
```bash
git -C /Users/gb/github/harmonik checkout -- .
git -C /Users/gb/github/harmonik clean -fd internal/harness/codex
```
`git mv` stages the rename, so `checkout -- .` restores the daemon files and `clean -fd` removes the new directory. E1a-0 is already merged and unaffected; the daemon still builds against `internal/harness/shared`.

**After E1a-1 is committed but before merge:** `git revert` the single commit (or drop the branch). The move is one commit by construction — plan §3 forbids dribbling it across several — so the revert is atomic. Re-verify with §6 steps 2, 4 and 8.

**After E1a-1 has merged and something surfaces in production:** do **not** hand-copy files back into `internal/daemon`; that trips the §5.2 freeze-check and leaves the depguard rules pointing at a package that no longer holds the code. Revert the merge commit, which restores the daemon files, the `export_test.go` blocks, the eight call sites, and the freeze-check target together. Then leave the two depguard rules in place: `internal/harness/shared` still exists and still needs its brake.

**Abandoning E1a-0 specifically:** revert it and E1a-1 becomes unstartable, which is correct — the pi harness cannot be rewired twice. If E1a-0 is reverted after E1a-1 merged, revert E1a-1 first; the dependency is one-directional.

**What you must not do at any point:** revert `internal/gitprobe/` or `internal/harness/shared/seedprompt.go`. They are already-landed working-tree state that this unit builds on, and `codexlaunchspec.go` already imports the latter. Rolling those back breaks the daemon independently of anything E1a did.
