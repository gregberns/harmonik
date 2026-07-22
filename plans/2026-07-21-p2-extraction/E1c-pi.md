# Unit E1c — Pi harness implementation → `internal/harness/pi`

**Status:** NEEDS PREPARATORY SLICE
**Depends on:** **E1a (hard)** — E1a must have landed the cross-harness commit/env helpers in
`internal/harness/shared` (not in `internal/harness/codex`) *and* the `harness-*` depguard rules.
E1b (claude) is **not** a dependency; E1c and E1b are independent after E1a.
Plus **prep slice B** (§4, Step B) — a design decision this unit owns: the exported
`pi.RunCtx` / `pi.BuildLaunchSpec` contract.
**Size:** ~4,955 LOC across 13 moved files (1,458 non-test in 5 files, 3,497 test in 8 files),
plus edits to 9 files that stay in `internal/daemon`.
**Risk:** MEDIUM — the impl move itself is mechanical and touches zero `*Daemon`/`*workLoop`/`*Runner`
receivers, but two cross-harness daemon tests force a *public* `pi.RunCtx` (including the test-only
`SkipBillingGuard` knob), and the live-pi oracle is wired to `./internal/daemon/...` by a Makefile
target that will go silently green if the test move is not paired with a Makefile fix.

**Where recon and the adversarial challenge disagreed, this document takes the CHALLENGE**, on every
point. I re-verified each disputed claim against the tree; the challenge was right in every case and
recon was demonstrably incomplete. The specific reversals are called out inline as
**[CHALLENGE WINS]**, with the evidence. One place I went *further* than both: recon claimed
`ExportedPiHarnessBaseURLFields` is consumed by `harnessregistry_pi_hkf8u5j_test.go`; it is consumed
by **nothing** (`grep -rn ExportedPiHarnessBaseURLFields --include='*.go' .` → 2 hits, both its own
declaration in `export_test.go:2887-2891`). It is dead code, so `BaseURL()`/`API()` accessors are
**not** needed and the accessor count drops from 6 to 4.

---

## 1. What moves

### 1.1 Implementation files → `internal/harness/pi/` (5 files, 1,458 LOC)

| file | LOC | destination | notes |
|---|---|---|---|
| `internal/daemon/piharness.go` | 267 | `internal/harness/pi/harness.go` | `PiHarness` + `NewPiHarness` + the `handlercontract.Harness` methods. Imports only `core` + `handlercontract`. Gains 4 field accessors (§3b). |
| `internal/daemon/pilaunchspec.go` | 499 | `internal/harness/pi/launchspec.go` | `piRunCtx` (16 fields), `buildPiLaunchSpec`, `buildPiEnv`, `buildPiModelsJSON`, `resolvePiAPIKeyValue`. Already imports `internal/harness/shared` (line 50). `piRunCtx` → exported `RunCtx`, `buildPiLaunchSpec` → exported `BuildLaunchSpec` (§4 Step B). |
| `internal/daemon/picommit.go` | 133 | `internal/harness/pi/commit.go` | `piRefsOutcome` alias + 4 const aliases + `ensurePiRefsTrailer` + `commitAllWithPiRefsTrailer`. Holds 5 of the 6 outbound couplings. |
| `internal/daemon/pijsonlparser.go` | 341 | `internal/harness/pi/ndjsonparser.go` | `piEventKind`, `parsePiNDJSONEvent`, `piRunArtifacts`, `capturePiUsage`, `newPiSessionIDInterceptor`. **stdlib-only imports** — zero coupling. |
| `internal/daemon/pibillingguard.go` | 218 | `internal/harness/pi/billingguard.go` | `runPiBillingGuard`, `emitPiBillingGuard`, `piDefaultHome`, `piAuthIndicatesPersistentCredential`, `piRunIDIsNil`. Imports `uuid` + `core` + `handlercontract`. |

### 1.2 Test files that MOVE → `internal/harness/pi/` (8 files, 3,497 LOC)

All are `package daemon_test` today and become **`package pi`** (internal test package) after the move,
except where the exported contract makes `package pi_test` equally viable — use `package pi` uniformly,
it is the smaller diff.

| file | LOC | shims it consumes (all pi-local) |
|---|---|---|
| `pilaunchspec_test.go` | 1489 | `ExportedPiRunCtx` (14 literals), `ExportedBuildPiLaunchSpec`, `ExportedBuildPiEnv`, `ExportedBuildPiModelsJSON`, `ExportedResolvePiAPIKeyValue`, `ExportedRunCtxForPi`, `daemon.NewPiHarness` |
| `pibillingguard_test.go` | 657 | `ExportedPiRunCtx` (2), `ExportedBuildPiLaunchSpec`, `ExportedRunPiBillingGuard`, `ExportedPiAuthIndicatesPersistentCredential`, `daemon.NewPiHarness` |
| `piharness_test.go` | 405 | `ExportedNewPiHarness`, `ExportedNewPiSessionIDInterceptor`, `ExportedParsePiNDJSONEvent`, `ExportedPiEventKind{Session,AgentEnd,Other}` |
| `pijsonlparser_ws1d_test.go` | 300 | `ExportedCapturePiUsage`, `ExportedParsePiNDJSONEvent`, `ExportedPiRunArtifacts`, `ExportedPiEventKind{AgentEnd,MessageStart,MessageEnd}` |
| `picommit_test.go` | 288 | `ExportedEnsurePiRefsTrailer(+ViaRunner)`, `ExportedPiRefs{AlreadyPresent,Amended,Committed,NoChange}`, **`ExportedWorktreeHEADHasRefsTrailer`** (→ `shared.WorktreeHEADHasRefsTrailer` after E1a) |
| `pi_live_hktwin_test.go` | 190 | `ExportedNewPiSessionIDInterceptor`, `ExportedParsePiNDJSONEvent`. `PI_LIVE=1`-gated; owns `TestPiA_LiveSingleTurn` (line 53). **Two hazards: `Makefile:294` and the fixture path at line 141.** |
| `pi_twin_parser_drive_test.go` | 116 | `ExportedNewPiSessionIDInterceptor`, `ExportedCapturePiUsage`, `ExportedPiRunArtifacts`. **Fixture path hazard at line 107.** |
| `pilaunchspec_pi042_via_spec_hk6g5iu_test.go` | 52 | `ExportedPiRunCtx` (2), `ExportedBuildPiLaunchSpec`, `ExportedRunPiBillingGuard` |

None of the eight imports `testify` or `goleak`; none shares a `daemon_test` fixture with a staying
test (the one apparent hit, `rsb12RequireSSHOrSkip` in `pi_live_hktwin_test.go:13`, is a comment); no
helper they define is consumed by a staying test. Verified.

### 1.3 Files that STAY in `internal/daemon` — and why

**Implementation:**

| file | LOC | why it stays |
|---|---|---|
| `pi_profile_resolve.go` | 146 | **Claim-time daemon wiring, not harness impl.** Consumes `PiHarnessConfig`/`PiProfileConfig` (`projectconfig.go`), `emitBeadLabelConflict` (`moderesolve.go`), `labelPrefixModel` (`modelpreference.go`); its three symbols (`resolvePiProfile`, `hasSingleModelLabel`, `emitProviderSelected`) are called from `workloop.go:3356/3371/3390`. Moving it drags the whole `projectconfig` type-family out of daemon and turns a 1.5k-LOC mechanical move into a config-package extraction. **This is the file most likely to be wrongly swept in by `pi_*.go` name-matching — do not move it.** |
| `harnessregistry.go` | 338 | Composition root. `_plan.md` §6 resolved this: keep `newHarnessRegistry` in daemon. It calls `NewPiHarness` (`:55`) and `effectiveModel` (`:78-86`) does an `h.(*PiHarness)` assertion reading the private `.model` field — that field read is the one inbound break this unit fixes with an accessor. |
| `codexcommit.go` | 353 | Owned by E1a. Holds 5 of E1c's 6 outbound symbols — see §3a and the E1a gate in §4 Step A. |
| `codexlaunchspec.go` | 334 | Owned by E1a. Holds the 6th (`envKey`, `:328`). |
| `reviewtrailers_hkdyim.go` | — | Stays, but **`:73-74` call `containsExactLine`**, which prep slice A moves to `shared`. Rewire, do not duplicate. **[CHALLENGE WINS — recon never named this consumer.]** |

**Tests that STAY (17 files):** `pi_profile_resolve_test.go`, `pi_unknown_profile_refuse_test.go`,
`pi_provider_selected_hk8ziid2_test.go`, `harnessregistry_pi_remote_runner_m4c4_test.go`,
`pi_dgx_reasoning_test.go`, `pi_no_tier3_leak_test.go`, `pi_toolcalls_per_provider_test.go`,
`pi_provider_slots_hk8ziid1_test.go`, `pi_retain_on_failure_hkj6wm7_test.go`,
`harnessregistry_pi_hkf8u5j_test.go`, `harnessregistry_stdin_devnull_hkj0p1r_test.go`,
`hk_6atjk_pi_path_e2e_test.go`, `hk_lfrub_dot_pi_launch_e2e_test.go`, `hk_pkugu_pi_launch_e2e_test.go`,
`hk_pkugu_pi_model_leak_test.go`, `scenario_sandbox_pi_i0377_test.go`, `srt_pi_egress_e2e_test.go` —
all exercise daemon composition (registry assembly, project config, routed launch-spec builder,
work-loop e2e), not pi-harness internals.

**Six MORE staying tests recon's inventory missed entirely** — every one of them touches a symbol this
unit moves, so all six need an edit. **[CHALLENGE WINS on all six; I re-derived each with a
per-symbol `grep -rl` over `internal/daemon/*_test.go`.]**

| file:line | what it uses | what E1c must do |
|---|---|---|
| `agentseedprompt_test.go:18,53` | `daemon.ExportedPiRunCtx{... SkipBillingGuard: true}` + `ExportedBuildPiLaunchSpec`, alongside 2 codex tests in the same file | Rewrite to `pi.RunCtx` / `pi.BuildLaunchSpec`. **Cross-harness → cannot move.** |
| `crossharness_empty_model_test.go:68` | `ExportedCodexRunCtx`+`ExportedBuildCodexLaunchSpec` **and** `ExportedPiRunCtx`+`ExportedBuildPiLaunchSpec` in ONE test that deliberately locks the codex-vs-pi empty-model asymmetry | Rewrite the pi half to `pi.RunCtx`/`pi.BuildLaunchSpec`. **Splitting it destroys the test's entire point — do not split.** |
| `sandboxgate_hkr4p0l_test.go:27` | `daemon.ExportedNewPiHarness(...)` → `ExportedResolveGateAgentType` | `pi.NewPiHarness(...)`; add the pi import |
| `hk_lfrub_dot_node_model_leak_test.go:56` | `daemon.ExportedNewPiHarness` + `ExportedEffectiveModel` | `pi.NewPiHarness`; depends on `Model()` landing |
| `dot_gate_reviewer_harness_hk01vs0_test.go:185` | `reg.Register(core.AgentTypePi, daemon.NewPiHarness(...))` | `pi.NewPiHarness` |
| `conformance_m4c7_test.go:135,223` | in-package (`package daemon`) `NewPiHarness(...)` ×2 | add `internal/harness/pi` import, qualify both calls |

Plus `harnessregistry_pi_remote_runner_m4c4_test.go:69,92,143` — recon listed it as STAYS but described
it as "registry + rc.runner threading"; it calls **`NewPiHarness` directly three times** as an
in-package daemon test, so it needs the same import rewrite.

---

## 2. The seam it exits behind

**Pre-existing seam:** `handlercontract.Harness` (interface, `internal/handlercontract/harness.go:202`),
registered through `handlercontract.HarnessRegistry` (`internal/handlercontract/harnessregistry.go:50`)
and resolved by `(*HarnessRegistry).ForAgent` (`harnessregistry.go:119`). The per-run inputs cross the
seam as `handlercontract.RunCtx` (`harness.go:85`); the process-exit contract crosses as
`CompletionProcessExit` (`harness.go:32`).

**No new seam is invented.** Every production consumer of pi already goes through the interface, never
the concrete type:

- `workloop.go:4626` — spawn via the registry-resolved harness
- `reviewloop.go:440`, `dot_cascade.go:1588` — `Completion()` / `SessionIDPolicy()` through the interface
- `workloop.go:5123-5124` — `deps.harnessRegistry.ForAgent(agType)` then `h.Completion() == CompletionProcessExit`

**Two concrete-type reads survive the move and are the whole of the inbound work (§3b):**
`harnessregistry.go:55` (construction — correct, that is the composition root's job) and
`harnessregistry.go:79` (`h.(*PiHarness)` + `piH.model` inside `effectiveModel`).

**RED FLAG, named and deliberately not fixed here:** `effectiveModel(h handlercontract.Harness, rc
claudeRunCtx)` branches on the **concrete** `*PiHarness` type inside the composition root, and takes a
`claudeRunCtx` while doing it. That is a real seam leak the extraction *exposes* but must not *fix* —
fixing it is a behavior change plus a new seam, which the pure-move rule (`_plan.md` §5.1) forbids.
File a follow-up bead; leave the assertion, just re-qualify the type and swap the field read for
`Model()`.

**Secondary pre-existing seam used by the moved code:** `lifecycle/tmux.CommandRunner`
(`internal/lifecycle/tmux/runner.go:16`) — `picommit.go` already routes every git op through it
(PI-031) and already reads HEAD via `gitprobe.ResolveWorktreeHEADVia`. Nothing to build.

---

## 3. Coupling to break

### 3a. Outbound (unit → daemon) — 6 symbol groups, all in E1a's files

I independently re-ran the free-identifier sweep (package-level decls of all 126 non-test daemon
files, including const/var-block members, intersected with comment-stripped identifiers of the five
moving files). The result matches recon **at first order**: exactly these six groups, and **zero**
method calls on `*Daemon`, `*Runner` or `*workLoop` anywhere in the unit.

| symbol | defined in | used by | resolution |
|---|---|---|---|
| `envKey` | `codexlaunchspec.go:328` | `pilaunchspec.go:427,441` (`buildPiEnv`) | → `shared.EnvKey`. 7 lines of `strings.IndexByte`, no deps. |
| `codexRefsOutcome` + `String()` | `codexcommit.go:75,97` | `picommit.go:50` (`type piRefsOutcome = codexRefsOutcome`) | → `shared.RefsOutcome`. Also consumed daemon-side by `codexnowork_hk368i4.go:76` and printed at `workloop.go:5131`, so daemon imports `shared` after the move — a legal leaf-ward edge (daemon already imports `shared` from `codexlaunchspec.go:38` and `pilaunchspec.go:50`). |
| `codexRefsAlreadyPresent` / `codexRefsAmended` / `codexRefsCommitted` / `codexRefsNoChange` | `codexcommit.go:79-` | `picommit.go:53-57` | → `shared.RefsAlreadyPresent` / `RefsAmended` / `RefsCommitted` / `RefsNoChange` |
| `worktreeHEADHasRefsTrailer` | `codexcommit.go:130` | `picommit.go:85` (VERIFY step) | → `shared.WorktreeHEADHasRefsTrailer`. Already runner-routed + gitprobe-based. |
| `codexWorktreeDirty` | `codexcommit.go:162` | `picommit.go:110` | → `shared.WorktreeDirty` — the "codex" in the name is vestigial; the body is a generic `git status --porcelain`. |
| `amendHEADAddRefsTrailer` | `codexcommit.go:305` | `picommit.go:102` | → `shared.AmendHEADAddRefsTrailer` |
| `commitAllWithHarnessRefsTrailer` | `codexcommit.go:265` | `picommit.go:131` (via `commitAllWithPiRefsTrailer`) | → `shared.CommitAllWithRefsTrailer`. Its name already declares it harness-generic; it takes `msgPrefix`, so both harnesses parameterise it. |
| `shared.ImplementerResumeSeedPrompt` | `internal/harness/shared/seedprompt.go` | `pilaunchspec.go:273` | **no-op — already done** in the working tree. |
| `core.*` / `handler.LaunchSpec` / `handlercontract.*` / `gitprobe.ResolveWorktreeHEADVia` / `tmux.CommandRunner` / `github.com/google/uuid` | leaf/seam packages outside daemon | all five moving files | **no-op** — already clean. |

**TRANSITIVE CLOSURE — the two symbols recon missed. [CHALLENGE WINS; verified by grep.]**
The prep slice does not compile without them:

| symbol | defined in | called by | resolution |
|---|---|---|---|
| `codexRefsTrailerLine` | `codexcommit.go:116` | `codexcommit.go:143` (inside `worktreeHEADHasRefsTrailer`), `:266` (inside `commitAllWithHarnessRefsTrailer`), `:321` (inside `amendHEADAddRefsTrailer`) | → `shared.RefsTrailerLine` — it is transitively required by three of the four helpers being moved. |
| `containsExactLine` | `codexcommit.go:346` | `codexcommit.go:326` (inside `amendHEADAddRefsTrailer`) **and `reviewtrailers_hkdyim.go:73,74`** | → `shared.ContainsExactLine`, and rewire `reviewtrailers_hkdyim.go` to call it. Do **not** leave a daemon-local duplicate. |

### 3b. Inbound (daemon → unit)

| current name | proposed exported name | call sites to rewrite |
|---|---|---|
| `ensurePiRefsTrailer` (`picommit.go:75`) | `pi.EnsureRefsTrailer(ctx, runner, wtPath, parentSHA, beadID) (shared.RefsOutcome, error)` | `workloop.go:5126` (prod), `export_test.go:3325,3335` (shims — deleted, callers move) |
| `piRunCtx` (`pilaunchspec.go:95`, 16 unexported fields) | **`pi.RunCtx` with 16 exported fields**, names taken verbatim from the existing `ExportedPiRunCtx` shim (`export_test.go:3466-3508`) | `piharness.go:157` (in-unit), `agentseedprompt_test.go`, `crossharness_empty_model_test.go`, + 18 literals in moving tests |
| `buildPiLaunchSpec` (`pilaunchspec.go:213`) | **`pi.BuildLaunchSpec(rc RunCtx) (handler.LaunchSpec, error)`** | `piharness.go:172` (in-unit), `agentseedprompt_test.go`, `crossharness_empty_model_test.go` |
| `PiHarness.provider` | `func (h *PiHarness) Provider() string` | `export_test.go:2883` `ExportedPiHarnessFields` → `harnessregistry_pi_hkf8u5j_test.go:49,95` |
| `PiHarness.model` | `func (h *PiHarness) Model() string` | `harnessregistry.go:83` (**production**, `effectiveModel`) + `ExportedPiHarnessFields` |
| `PiHarness.apiKeyEnv` | `func (h *PiHarness) APIKeyEnv() string` | `ExportedPiHarnessFields` |
| `PiHarness.apiKeyFile` | `func (h *PiHarness) APIKeyFile() string` | `ExportedPiHarnessFields` |
| `NewPiHarness` / `PiHarness` | `pi.NewPiHarness` / `*pi.PiHarness` — already exported, only the import path changes; keep the stuttering names (smaller diff) | `harnessregistry.go:55,79`; staying tests `conformance_m4c7_test.go:135,223`, `harnessregistry_pi_remote_runner_m4c4_test.go:69,92,143`, `dot_gate_reviewer_harness_hk01vs0_test.go:185`, `sandboxgate_hkr4p0l_test.go:27`, `hk_lfrub_dot_node_model_leak_test.go:56`, `hk_pkugu_pi_model_leak_test.go:119` |
| `buildPiEnv`, `buildPiModelsJSON`, `resolvePiAPIKeyValue`, `runPiBillingGuard`, `piAuthIndicatesPersistentCredential`, `piDefaultHome`, `parsePiNDJSONEvent`, `capturePiUsage`, `piRunArtifacts`, `piEventKind*`, `newPiSessionIDInterceptor` | **stay unexported inside `package pi`** | consumed only by the 8 moving tests, which become `package pi` internal tests and call them directly |

**Total symbols needing export: 8** — `EnsureRefsTrailer`, `RunCtx` (+ its 16 fields already named by
the shim), `BuildLaunchSpec`, and the four accessors `Provider`/`Model`/`APIKeyEnv`/`APIKeyFile`.

**[CHALLENGE WINS — recon said 17 and said the run-ctx could stay unexported. It cannot.]** The
falsifying evidence: `agentseedprompt_test.go:18` and `crossharness_empty_model_test.go:68` both build
a full `ExportedPiRunCtx{... SkipBillingGuard: true}` and call `ExportedBuildPiLaunchSpec`, *in the
same file as codex assertions*. They cannot become `package pi` internal tests, and they cannot be
rewritten through the `handlercontract.Harness` seam because that path offers **no way to set
`skipBillingGuard`** — `PiHarness.LaunchSpec` (`piharness.go:156-170`) constructs `piRunCtx` without
it — so `buildPiLaunchSpec` would run the fail-closed PI-040 guard and error without a real provider
key. Exporting `RunCtx` + `BuildLaunchSpec` is therefore forced.

**Where I go beyond both inputs:** because the exported field names are *identical* to today's
`ExportedPiRunCtx` field names, exporting `RunCtx` **eliminates** recon's heaviest rewrite step — the
18 `ExportedPiRunCtx{...}` literals need only `daemon.ExportedPiRunCtx{` → `RunCtx{`, with **zero**
field-case edits. Recon's STEP 8 planned 16 per-literal field renames that this design makes
unnecessary.

**Accessor count is 4, not 6.** `BaseURL()`/`API()` are unnecessary:
`ExportedPiHarnessBaseURLFields` (`export_test.go:2887`) has **no consumer anywhere in the repo** —
delete it as dead code in this unit rather than porting it. Same for the equally-dead
`ExportedPiDefaultHome` (`:3593`), `ExportedPiRefsOutcome` (`:3309`, referenced only by other shims)
and `ExportedPiTokenUsage` (`:3374`, same).

---

## 4. Step-by-step recipe

All commands run from `/Users/gb/github/harmonik`. Never `cd` into a worktree; use `git -C` with
absolute paths.

### Step A — GATE on E1a's shared placement (verify, do not assume)

```bash
grep -rn 'func EnvKey\|func WorktreeHEADHasRefsTrailer\|func WorktreeDirty\|func AmendHEADAddRefsTrailer\|func CommitAllWithRefsTrailer\|func RefsTrailerLine\|func ContainsExactLine\|type RefsOutcome' \
  /Users/gb/github/harmonik/internal/harness/shared/
```

- **All 8 present** → skip to Step B.
- **Absent** → do prep slice A below as its **own standalone commit** before touching pi.
- **Present but under `internal/harness/codex/`** → **STOP.** `harness/pi` importing `harness/codex` is
  exactly the "daemon back-edge in disguise" `_plan.md` §6 forbids. Move them to `shared` first.
  Tell whoever runs E1a *before they start*; the shared symbol is literally named
  `commitAllWithHarnessRefsTrailer`, so the correct outcome is the natural one.

**Prep slice A (only if the gate fails) — one standalone commit, no pi files touched.**
Move from `internal/daemon/codexcommit.go` → new `internal/harness/shared/gitcommit.go`:
`codexRefsOutcome` (`:75`) + its `String()` (`:97`) + the four `codexRefs*` constants (`:79-`) +
`codexRefsTrailerLine` (`:116`) + `worktreeHEADHasRefsTrailer` (`:130`) + `codexWorktreeDirty` (`:162`)
+ `commitAllWithHarnessRefsTrailer` (`:265`) + `amendHEADAddRefsTrailer` (`:305`) +
`containsExactLine` (`:346`). Move `envKey` (`codexlaunchspec.go:328`) → new
`internal/harness/shared/env.go`. Export as `RefsOutcome`, `RefsAlreadyPresent`, `RefsAmended`,
`RefsCommitted`, `RefsNoChange`, `RefsTrailerLine`, `WorktreeHEADHasRefsTrailer`, `WorktreeDirty`,
`CommitAllWithRefsTrailer`, `AmendHEADAddRefsTrailer`, `ContainsExactLine`, `EnvKey`.

Rewire **seven** daemon call sites — recon named four, the two extra come from the transitive closure
and one from the missed consumer: `codexcommit.go`, `codexlaunchspec.go:278`,
`codexnowork_hk368i4.go:76`, `workloop.go:5131`, **`reviewtrailers_hkdyim.go:73-74`**,
**`codexcommit_test.go:138,160,229,272`**, **`picommit_test.go:148,187,259`** (the last two both use
`daemon.ExportedWorktreeHEADHasRefsTrailer`; recon flagged only the pi one).

Widen `internal/harness/shared`'s depguard allow-list to `$gostd` + `core` + `gitprobe` +
`lifecycle/tmux` + self, and **update the `seedprompt.go` package header**, which today claims
"The package is a leaf — stdlib only." That sentence becomes false the moment the commit helpers land.

Verify prep slice A: `go build ./... && go test ./internal/daemon/ -run 'Codex|Refs' -count=1`.

### Step B — decide and record the export contract (do this before writing code)

Land the decision from §3b: `piRunCtx` becomes exported `pi.RunCtx` with the 16 field names already
used by `ExportedPiRunCtx` (`export_test.go:3466-3508`), and `buildPiLaunchSpec` becomes
`pi.BuildLaunchSpec`. Write the justification into the commit body up front (see §7, risk R4) —
`SkipBillingGuard` becoming public API is the single largest reviewer target in this unit and must be
pre-empted, not defended after the fact.

### Step 1 — create the package

```bash
mkdir -p /Users/gb/github/harmonik/internal/harness/pi
```

Add `internal/harness/pi/doc.go`, mirroring the header style of
`internal/harness/shared/seedprompt.go`:

```go
// Package pi implements handlercontract.Harness for the Pi agent
// (specs/pi-harness.md, PI-010…PI-050).
//
// It was lifted out of internal/daemon by P2 unit E1c so a container can link
// the pi harness without linking the 57k-LOC daemon monolith. The composition
// root (newHarnessRegistry) stays in daemon and constructs this package's
// PiHarness through the pre-existing handlercontract.HarnessRegistry seam.
//
// Origin: internal/daemon/{piharness,pilaunchspec,picommit,pijsonlparser,
// pibillingguard}.go.
package pi
```

### Step 2 — `git mv` the five impl files

```bash
git -C /Users/gb/github/harmonik mv internal/daemon/piharness.go      internal/harness/pi/harness.go
git -C /Users/gb/github/harmonik mv internal/daemon/pilaunchspec.go   internal/harness/pi/launchspec.go
git -C /Users/gb/github/harmonik mv internal/daemon/picommit.go       internal/harness/pi/commit.go
git -C /Users/gb/github/harmonik mv internal/daemon/pijsonlparser.go  internal/harness/pi/ndjsonparser.go
git -C /Users/gb/github/harmonik mv internal/daemon/pibillingguard.go internal/harness/pi/billingguard.go
```

Change `package daemon` → `package pi` in all five.

### Step 3 — rewrite the six outbound references, and nothing else

Inside the moved files only:

- `launchspec.go` (was `:427,441`): `envKey(kv)` → `shared.EnvKey(kv)`
- `commit.go:50`: `type piRefsOutcome = codexRefsOutcome` → `= shared.RefsOutcome`
- `commit.go:53-57`: the four consts → `shared.RefsAlreadyPresent` / `RefsAmended` / `RefsCommitted` / `RefsNoChange`
- `commit.go:85`: `worktreeHEADHasRefsTrailer` → `shared.WorktreeHEADHasRefsTrailer`
- `commit.go:102`: `amendHEADAddRefsTrailer` → `shared.AmendHEADAddRefsTrailer`
- `commit.go:110`: `codexWorktreeDirty` → `shared.WorktreeDirty`
- `commit.go:131`: `commitAllWithHarnessRefsTrailer` → `shared.CommitAllWithRefsTrailer`

Add `"github.com/gregberns/harmonik/internal/harness/shared"` to `commit.go`; `launchspec.go` already
imports it (line 50).

### Step 4 — export the boundary symbols

1. `commit.go`: rename `ensurePiRefsTrailer` → `EnsureRefsTrailer` (all 5 internal error strings say
   `"daemon: ensurePiRefsTrailer: …"` — update the prefix to `"pi: EnsureRefsTrailer: …"`; this is a
   log-string change, not behavior, and must be called out in the commit body).
2. `launchspec.go`: rename `piRunCtx` → `RunCtx` and export all 16 fields using **exactly** the names
   in `export_test.go:3466-3508`: `PiBinary`, `WorkspacePath`, `BeadID`, `Provider`, `Model`,
   `APIKeyEnv`, `APIKeyFile`, `PriorSessionID`, `IterationCount`, `BaseEnv`, `BillingEmitter`,
   `RunID`, `SkipBillingGuard`, `BaseURL`, `API`, `PiHome`. Rename `buildPiLaunchSpec` →
   `BuildLaunchSpec`. Keep every doc comment; move the shim's extra prose into the real struct.
3. `harness.go`: update `PiHarness.LaunchSpec` (`:156-172`) to build `RunCtx{PiBinary: …,
   WorkspacePath: …, …}` with the exported names and call `BuildLaunchSpec(prc)`.
4. `harness.go`: add four accessors. Struct fields keep their unexported names.

```go
// Provider returns the configured Pi provider. It exists because the daemon
// composition root and its config→harness seam test read this value across a
// package boundary after P2 unit E1c; it is a mechanical field read, not a seam.
func (h *PiHarness) Provider() string   { return h.provider }
func (h *PiHarness) Model() string      { return h.model }
func (h *PiHarness) APIKeyEnv() string  { return h.apiKeyEnv }
func (h *PiHarness) APIKeyFile() string { return h.apiKeyFile }
```

### Step 5 — rewire the daemon production call sites (2 files)

- `internal/daemon/workloop.go:5126`: `ensurePiRefsTrailer(ctx, runRunner, wtPath, headSHA, beadID)`
  → `pi.EnsureRefsTrailer(...)`; the two `fmt.Fprintf` messages on `:5128` and `:5131` keep their
  text (they are the pre/post-extraction log oracle — do not reword them).
- `internal/daemon/harnessregistry.go:55`: `NewPiHarness(` → `pi.NewPiHarness(`.
- `internal/daemon/harnessregistry.go:79,83`: `h.(*PiHarness)` → `h.(*pi.PiHarness)`; `piH.model` →
  `piH.Model()`.
- Add `"github.com/gregberns/harmonik/internal/harness/pi"` to both files.

### Step 6 — `git mv` the eight harness-only test files

```bash
cd /Users/gb/github/harmonik  # for readability only; every path below is repo-relative to it
git -C /Users/gb/github/harmonik mv internal/daemon/pilaunchspec_test.go                     internal/harness/pi/launchspec_test.go
git -C /Users/gb/github/harmonik mv internal/daemon/pibillingguard_test.go                   internal/harness/pi/billingguard_test.go
git -C /Users/gb/github/harmonik mv internal/daemon/piharness_test.go                        internal/harness/pi/harness_test.go
git -C /Users/gb/github/harmonik mv internal/daemon/pijsonlparser_ws1d_test.go               internal/harness/pi/ndjsonparser_ws1d_test.go
git -C /Users/gb/github/harmonik mv internal/daemon/picommit_test.go                         internal/harness/pi/commit_test.go
git -C /Users/gb/github/harmonik mv internal/daemon/pi_live_hktwin_test.go                   internal/harness/pi/live_hktwin_test.go
git -C /Users/gb/github/harmonik mv internal/daemon/pi_twin_parser_drive_test.go             internal/harness/pi/twin_parser_drive_test.go
git -C /Users/gb/github/harmonik mv internal/daemon/pilaunchspec_pi042_via_spec_hk6g5iu_test.go internal/harness/pi/launchspec_pi042_via_spec_hk6g5iu_test.go
```

Convert each from `package daemon_test` to `package pi` and delete the
`"github.com/gregberns/harmonik/internal/daemon"` import.

### Step 7 — mechanically rewrite the shim call sites in the eight moved tests

| from | to |
|---|---|
| `daemon.ExportedPiRunCtx{` | `RunCtx{` — **field names unchanged** (18 literals: 14 in `launchspec_test.go`, 2 in `billingguard_test.go`, 2 in `launchspec_pi042_*`) |
| `daemon.ExportedBuildPiLaunchSpec` | `BuildLaunchSpec` |
| `daemon.ExportedBuildPiEnv` | `buildPiEnv` |
| `daemon.ExportedBuildPiModelsJSON` | `buildPiModelsJSON` |
| `daemon.ExportedResolvePiAPIKeyValue` | `resolvePiAPIKeyValue` |
| `daemon.ExportedRunCtxForPi` | inline the shim body (`export_test.go:3541-3553`) as a local test helper — it returns `handlercontract.RunCtx`, no daemon dependency |
| `daemon.ExportedRunPiBillingGuard(bus, beadID, keyFile, keyEnv, piHome)` | `runPiBillingGuard(ctx, bus, runID, beadID, keyFile, keyEnv, piHome)` — the shim (`:3580`) drops `ctx`/`runID`; re-add `context.Background()` and `core.RunID{}` at each call site |
| `daemon.ExportedPiAuthIndicatesPersistentCredential` | `piAuthIndicatesPersistentCredential` |
| `daemon.ExportedNewPiHarness` / `daemon.NewPiHarness` | `NewPiHarness` |
| `daemon.ExportedNewPiSessionIDInterceptor` | `newPiSessionIDInterceptor` (returns the concrete `*piSessionIDInterceptor`; the shim narrowed it to `io.Reader` — assign to `var r io.Reader = …` where the test relied on that) |
| `daemon.ExportedParsePiNDJSONEvent(line)` | `parsePiNDJSONEvent(line)` — the shim flattens to a 5-tuple `(kind, rawType, sessionID, usage, err)`; the real func returns `(piEvent, error)`. **Unflatten at each call**: `ev, err := parsePiNDJSONEvent(line)` then `ev.Kind`, `ev.RawType`, `ev.SessionID`, `ev.Usage.InputTokens`, `ev.Usage.OutputTokens`. |
| `daemon.ExportedPiTokenUsage{...}` | `piTokenUsage{...}` (the exported mirror is dead elsewhere — see §3b) |
| `daemon.ExportedCapturePiUsage(arts, line)` | inline the shim body: `ev, err := parsePiNDJSONEvent(line); if err != nil { … }; capturePiUsage(arts, ev)` |
| `daemon.ExportedPiRunArtifacts` | `piRunArtifacts` |
| `daemon.ExportedPiEventKind{Session,AgentEnd,Other,MessageStart,MessageEnd}` | `piEventKind{Session,AgentEnd,Other,MessageStart,MessageEnd}` |
| `daemon.ExportedEnsurePiRefsTrailer(ctx, wt, parent, bead)` | `EnsureRefsTrailer(ctx, nil, wt, parent, bead)` |
| `daemon.ExportedEnsurePiRefsTrailerViaRunner(ctx, runner, …)` | `EnsureRefsTrailer(ctx, runner, …)` |
| `daemon.ExportedPiRefs{AlreadyPresent,Amended,Committed,NoChange}` | `shared.Refs{AlreadyPresent,Amended,Committed,NoChange}` |
| `daemon.ExportedWorktreeHEADHasRefsTrailer(ctx, wt, bead)` | `shared.WorktreeHEADHasRefsTrailer(ctx, nil, wt, bead)` (3 sites in `commit_test.go`) |

### Step 8 — fix the two fixture paths and the Makefile **[CHALLENGE WINS — recon caught one of three]**

The moved tests sit one directory deeper (`internal/daemon` → `internal/harness/pi`), so every
`filepath.Join("..","..")` needs a third `".."`:

- `twin_parser_drive_test.go:107`: `filepath.Join("..","..","testdata","twin-parity","pi","happy-path-sample","ndjson")` → `filepath.Join("..","..","..","testdata",…)`
- `live_hktwin_test.go:141`: `filepath.Join("..","..","testdata","twin-parity","pi",piLiveScenario)` → same fix

`runPiTwin` (`twin_parser_drive_test.go:34-46`) is **safe** — it shells
`go run github.com/gregberns/harmonik/cmd/harmonik-twin-pi`, a full module path, not `./cmd/...`.

**`Makefile:294` must change in the same commit:**

```make
	PI_LIVE=1 go test -timeout 180s -count=1 -run TestPiA_ ./internal/daemon/...
```

`TestPiA_LiveSingleTurn` lives at `pi_live_hktwin_test.go:53` and is the **only** `TestPiA_` match.
After the move that target matches zero tests and **exits 0** — a silent green on the only real-pi
oracle gate. Change it to:

```make
	PI_LIVE=1 go test -timeout 180s -count=1 -run TestPiA_ ./internal/harness/pi/...
```

Then prove it is not vacuous: `make test-pi-live 2>&1 | grep -q 'no tests to run' && echo BROKEN`.

### Step 9 — prune `internal/daemon/export_test.go`

**Delete** (now unreachable): `ExportedPiRefsOutcome` + the four `ExportedPiRefs*` consts +
`ExportedEnsurePiRefsTrailer` + `ExportedEnsurePiRefsTrailerViaRunner` (`:3309-3338`);
`ExportedNewPiSessionIDInterceptor`, `ExportedParsePiNDJSONEvent`, `ExportedPiTokenUsage`,
`ExportedPiRunArtifacts`, `ExportedCapturePiUsage`, the five `ExportedPiEventKind*` consts
(`:3358-3412`); `ExportedPiRunCtx`, `ExportedBuildPiLaunchSpec`, `ExportedBuildPiModelsJSON`,
`ExportedRunCtxForPi`, `ExportedBuildPiEnv`, `ExportedResolvePiAPIKeyValue` (`:3459-3572`);
`ExportedRunPiBillingGuard`, `ExportedPiAuthIndicatesPersistentCredential` (`:3580-3592`).

**Delete as dead code** (zero consumers anywhere in the repo, verified):
`ExportedPiHarnessBaseURLFields` (`:2887-2891`), `ExportedPiDefaultHome` (`:3593-3598`).

**DO NOT DELETE `ExportedNewPiHarness` (`:3352-3355`). [CHALLENGE WINS — recon's step 9 deletes it
and breaks the build.]** Three staying tests use it: `hk_pkugu_pi_model_leak_test.go:119`,
`hk_lfrub_dot_node_model_leak_test.go:56`, `sandboxgate_hkr4p0l_test.go:27`. Either keep the shim as
`var ExportedNewPiHarness = pi.NewPiHarness`, or (preferred — one fewer indirection) delete it and
point those three files straight at `pi.NewPiHarness`.

**KEEP** (serve staying tests): `ExportedNewHarnessRegistry`, `ExportedNewHarnessRegistryWithPi`,
`ExportedEffectiveModel`, `ExportedPiProcessExitLaunchSpecBuilder`, `ExportedResolvePiProfile`.

**REWRITE** `ExportedPiHarnessFields` (`:2883-2885`):

```go
func ExportedPiHarnessFields(h *pi.PiHarness) (provider, model, apiKeyEnv, apiKeyFile string) {
	return h.Provider(), h.Model(), h.APIKeyEnv(), h.APIKeyFile()
}
```

### Step 10 — rewire the 8 staying test files listed in §1.3

`agentseedprompt_test.go`, `crossharness_empty_model_test.go`, `sandboxgate_hkr4p0l_test.go`,
`hk_lfrub_dot_node_model_leak_test.go`, `hk_pkugu_pi_model_leak_test.go`,
`dot_gate_reviewer_harness_hk01vs0_test.go`, `conformance_m4c7_test.go`,
`harnessregistry_pi_remote_runner_m4c4_test.go`.

### Step 11 — add the depguard rules (§5)

### Step 12 — add the freeze grep-guard (§5)

### Step 13 — verify (§6)

### Step 14 — runtime proof, corrected

Dispatch one bead labelled `harness:pi` through DOT impl→review→merge and confirm, byte-for-byte
against a pre-extraction run: the captured session id, the `models.json` / `PI_CODING_AGENT_DIR` path
(only if `harnesses.pi.base_url` is configured), the `Refs:` trailer outcome, and the two
`daemon: workloop: ensurePiRefsTrailer bead …` stderr lines from `workloop.go:5128/5131`.

**Do NOT include "the `pi_billing_guard` event" in the runtime proof. [CHALLENGE WINS — recon's
STEP 13 asks for something unobservable.]** Verified: `PiHarness.LaunchSpec` (`piharness.go:156-170`)
builds `piRunCtx` **without** `billingEmitter`, and it is the **only** production caller of
`buildPiLaunchSpec`. `emitPiBillingGuard` (`pibillingguard.go:133-135`) returns immediately on a nil
bus. The event fires in neither the before nor the after run, so it proves nothing. File the
never-wired `billingEmitter` as a separate bead; do not wire it inside a pure-move diff.

---

## 5. Freeze tripwire

### 5.1 depguard — the literal YAML

Insert immediately after the existing `gitprobe:` block in `.golangci.yml` (its deny line is `:178`),
copying that block's idiom.

**Use per-package rules, NOT one umbrella `harness:` rule. [CHALLENGE WINS.]** depguard `allow`
entries are **prefix** matches, so an umbrella allow of
`"github.com/gregberns/harmonik/internal/harness"` (recon's proposal) silently permits
`harness/pi` → `harness/codex` — precisely the sibling back-edge `_plan.md` §6 calls a blocker. If
E1a already landed an umbrella `harness:` rule, **replace it** with these.

```yaml
        # harness/shared: the cross-harness leaf (P2 E1a). stdlib + core + the two
        # execution seams (gitprobe read-only git probes, tmux.CommandRunner) + self.
        # It holds the Refs:-trailer commit helpers and EnvKey, which codex, pi and
        # the daemon all call. MUST NOT import daemon: shared sits underneath every
        # extracted harness, so a daemon edge here re-couples all of them at once.
        # Rationale: plans/2026-07-21-p2-extraction/_plan.md §3 freeze-then-strangle.
        harness-shared:
          files: ["**/internal/harness/shared/**"]
          allow:
            - "$gostd"
            - "github.com/gregberns/harmonik/internal/core"
            - "github.com/gregberns/harmonik/internal/gitprobe"
            - "github.com/gregberns/harmonik/internal/lifecycle/tmux"
            - "github.com/gregberns/harmonik/internal/harness/shared"   # self-import (external test pattern)
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "harness/shared is the leaf under every extracted harness; a daemon edge re-couples all of them (P2 E1a)" }

        # harness/pi: the concrete handlercontract.Harness impl for Pi (P2 unit E1c).
        # This package MUST be linkable WITHOUT internal/daemon — that linkable-unit
        # property is the whole point of the extraction and is what lets P3 put a
        # harness in a container without dragging the 57k-LOC monolith. The sibling
        # denies matter as much as the daemon deny: harness/pi importing harness/codex
        # would be a daemon back-edge in disguise (_plan.md §6 watch-item). Anything
        # genuinely shared goes to internal/harness/shared.
        # Rationale: plans/2026-07-21-p2-extraction/E1c-pi.md.
        harness-pi:
          files: ["**/internal/harness/pi/**"]
          allow:
            - "$gostd"
            - "github.com/google/uuid"
            - "github.com/gregberns/harmonik/internal/core"
            - "github.com/gregberns/harmonik/internal/handler"
            - "github.com/gregberns/harmonik/internal/handlercontract"
            - "github.com/gregberns/harmonik/internal/gitprobe"
            - "github.com/gregberns/harmonik/internal/lifecycle/tmux"
            - "github.com/gregberns/harmonik/internal/harness/shared"
            - "github.com/gregberns/harmonik/internal/harness/pi"        # self-import (external test pattern)
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "harness impls MUST NOT import daemon back (P2 E1: the harness must link without the monolith)" }
            - { pkg: "github.com/gregberns/harmonik/internal/harness/codex", desc: "sibling harnesses MUST NOT import each other; genuinely shared code goes to internal/harness/shared (_plan.md §6)" }
            - { pkg: "github.com/gregberns/harmonik/internal/harness/claude", desc: "sibling harnesses MUST NOT import each other; genuinely shared code goes to internal/harness/shared (_plan.md §6)" }
```

No daemon-side rule change is needed: the `daemon:` block already allows the blanket prefix
`"github.com/gregberns/harmonik/internal/"`, so `daemon` → `harness/pi` and `daemon` → `harness/shared`
are already legal.

### 5.2 The "no new pi files in daemon" grep-guard

depguard cannot express "no new file for this concern," so this is a script. Create
`scripts/p2-freeze-guard.sh` (if E1a already created it, **extend its `PATTERNS`** rather than adding
a second script):

```bash
#!/usr/bin/env bash
# P2 freeze-then-strangle tripwire (_plan.md §3.2). HARD CI FAILURE, not a warning.
# Once a concern has been extracted out of internal/daemon, the door is closed:
# no new file for that concern may appear in the god package again.
set -euo pipefail
cd "$(dirname "$0")/.."

fail=0

# E1c (pi harness). pi_profile_resolve.go is whitelisted BY NAME, not by glob:
# it is claim-time daemon wiring bound to projectconfig's PiHarnessConfig /
# PiProfileConfig with three call sites in workloop.go. It is not harness impl.
while IFS= read -r f; do
  case "$(basename "$f")" in
    pi_profile_resolve.go|pi_profile_resolve_test.go) continue ;;
  esac
  echo "P2 E1c FREEZE VIOLATION: $f — the pi harness lives in internal/harness/pi." >&2
  fail=1
done < <(ls internal/daemon/pi*.go internal/daemon/*pi*harness*.go 2>/dev/null | sort -u)

exit "$fail"
```

```bash
chmod +x /Users/gb/github/harmonik/scripts/p2-freeze-guard.sh
```

Hook it into `Makefile` `check-short` (the CI Tier-2 gate that `.github/workflows/ci.yml:43` runs) and
into `check-fast` (the pre-commit tier), immediately after the `go build ./...` line in each:

```make
	./scripts/p2-freeze-guard.sh
```

Note the whitelist keeps `pi_profile_resolve.go` **and its test**; the guard as written would
otherwise fail on day one.

---

## 6. Verification gate

Run in this order, **serialized** — do not run the suite concurrently with an agent fan-out or a
parallel build; per `plans/2026-07-21-p2-extraction/00-test-oracle-baseline.md` that is what
manufactured five of the six baseline failures.

| # | command | pass criterion |
|---|---|---|
| 1 | `go build ./internal/... ./cmd/...` | exit 0 |
| 2 | `go vet ./internal/...` | exit 0 |
| 3 | `go test ./internal/harness/... -count=1` | all green; `launchspec_test.go`'s ~14 `RunCtx` cases and `commit_test.go`'s trailer cases in particular |
| 4 | `go test ./internal/daemon/... ./internal/harness/... -count=1 -timeout 25m 2>&1 \| grep -E '^--- FAIL' \| sort -u > after-failures.txt` then `comm -13 before-failures.txt after-failures.txt` | **EMPTY.** `internal/daemon` is already red at HEAD — the oracle is *differential*, not zero. Capture `before-failures.txt` with the identical command **before** starting the unit. Known pre-existing: `TestThroughput_TenBeadsAtMaxFour` (hard failure at HEAD) plus five load-sensitive flakes. Do not "fix" any of them inside this commit. |
| 5 | `PI_LIVE=1 make test-pi-live` | `TestPiA_LiveSingleTurn` actually **runs** (needs `pi` on PATH / `PI_BIN`, `PI_PROVIDER`, `PI_MODEL`, valid auth). Belt and braces: `HARMONIK_REQUIRE_PI_LIVE=1 make test-pi-live` turns a can't-run skip into a `Fatalf`. A `no tests to run` line here means Step 8's Makefile fix was missed. |
| 6 | `./scripts/p2-freeze-guard.sh` | exit 0 |
| 7 | `.tools/golangci-lint run ./internal/harness/... ./internal/daemon/...` | **no new depguard findings.** This IS the boundary test (`_plan.md` §3.4). Note the full run reports ~5,666 pre-existing legacy issues repo-wide; CI gates on `--new-from-rev=origin/main`, so compare against that. |
| 8 | `ubs internal/harness/pi/*.go internal/daemon/workloop.go internal/daemon/harnessregistry.go internal/daemon/export_test.go` | exit 0 |
| 9 | `ls internal/daemon/*.go \| grep -v _test.go \| wc -l` and `ls internal/daemon/*.go \| grep -v _test.go \| xargs wc -l \| tail -1` | see below |

**Success metric — the LOC/file drop to publish in the bead close comment.**
Measured baseline on this working tree, 2026-07-22 (pre-E1a): **126 non-test files, 57,197 non-test
LOC** in `internal/daemon` (top level). E1a and E1b land first, so **re-measure immediately before
starting E1c** and report the delta, not the absolute.

- **Non-test LOC:** −**1,458** (267 + 499 + 133 + 341 + 218)
- **Non-test files:** −**5**
- **Test LOC:** −3,497; **test files:** −8
- **Total files leaving `internal/daemon`:** **13**

`_plan.md` §0's "631 non-test .go files" is a miscount worth correcting in passing: 641 is the *total*
`.go` count; 515 of those are tests; **126** are non-test.

---

## 7. Risks and how each is mitigated

**R1 — HARD dependency on E1a's placement decision.** Five of E1c's six outbound symbols live in
`codexcommit.go`. If E1a parks them in `internal/harness/codex` instead of `internal/harness/shared`,
E1c is **blocked**: `harness/pi` importing `harness/codex` is the sibling back-edge `_plan.md` §6
forbids, and §5.1's depguard `deny` will (correctly) refuse it. *Mitigation:* tell whoever runs E1a
**before they start**; Step A is a hard gate, not an assumption. The symbols are already pre-named for
the shared home (`commitAllWithHarnessRefsTrailer`), so the correct outcome is the natural one.

**R2 — Prep slice A does not compile as recon specified.** It omits `codexRefsTrailerLine`
(`codexcommit.go:116`, called at `:143`, `:266`, `:321`) and `containsExactLine` (`:346`, called at
`:326`). *Mitigation:* §3a lists the transitive closure explicitly; Step A moves all of it.

**R3 — `containsExactLine` has a third consumer nobody planned for.**
`reviewtrailers_hkdyim.go:73-74`. *Mitigation:* Step A rewires it to `shared.ContainsExactLine`
(daemon→shared is legal and already exists). Do **not** leave a daemon-local duplicate — a
silently-diverging copy of a trailer matcher is a latent correctness bug in both the review-trailer
and the Refs-trailer paths.

**R4 — `SkipBillingGuard` becomes public API.** Exporting `pi.RunCtx` promotes a field whose own
doc-comment says it "exists SOLELY so unit tests … do not require a real api key" into the package's
public surface. `agent-reviewer`'s unwanted-abstraction check *will* flag it, and it is the largest
single review target in the unit. *Mitigation:* pre-empt it in the commit body with the falsifying
evidence — `agentseedprompt_test.go` and `crossharness_empty_model_test.go` are cross-harness tests
that cannot become `package pi` internal tests and cannot reach the guard-skip through the
`handlercontract.Harness` seam. State plainly that the alternative (splitting
`crossharness_empty_model_test.go`) destroys the only test that locks the codex-vs-pi asymmetry in one
place, and is therefore worse. File a follow-up bead to give the guard a real injection point so the
knob can be un-exported later.

**R5 — The four `PiHarness` accessors look gratuitous.** Only `Model()` has a production reader
(`effectiveModel`); the other three serve one test shim. *Mitigation:* commit-body pre-empt — they are
the mechanical cost of a private field read crossing a package boundary, not a new seam. (Cost is
lower than recon estimated: `BaseURL()`/`API()` are **not** needed because
`ExportedPiHarnessBaseURLFields` has zero consumers.)

**R6 — `effectiveModel`'s concrete-type assertion is a real seam leak.** The composition root still
branches on `*PiHarness`, and takes a `claudeRunCtx` while doing it. The extraction exposes it and does
not fix it. *Mitigation:* **do not fix it in this unit** — that is a behavior change plus a new seam,
which the pure-move rule forbids. File a follow-up bead.

**R7 — Silent green on the live-pi oracle.** `Makefile:294` runs `-run TestPiA_ ./internal/daemon/...`;
`TestPiA_LiveSingleTurn` is the only match and it moves. Go exits **0** when a `-run` filter matches
nothing. *Mitigation:* Step 8 changes the Makefile in the same commit; gate 5 asserts the test actually
runs, with `HARMONIK_REQUIRE_PI_LIVE=1` as the anti-false-green backstop.

**R8 — Two fixture paths break by one directory level.** `twin_parser_drive_test.go:107` and
`live_hktwin_test.go:141` both `filepath.Join("..","..","testdata",…)`. Recon caught one file and got
the reason wrong (it blamed the `go run` invocation, which is actually safe — it uses the full module
path). *Mitigation:* Step 8 fixes both; gate 3 catches a bad compare, gate 5 catches the live one.

**R9 — `pi_live_hktwin_test.go` moves green whether or not the rewrite is correct.** It is `PI_LIVE=1`
gated and normally skips. *Mitigation:* run it once with real credentials (gate 5) before declaring the
unit done. If credentials are unavailable, **leave this one file in `internal/daemon`** and note it —
an unverifiable move of an oracle test is worse than a deferred one.

**R10 — `piRunArtifacts` + `capturePiUsage` have no production caller.** Every non-test hit in daemon
is a comment or an `export_test` shim. *Mitigation:* move them **as-is**. Deleting them is a behavior
change outside a pure-move diff. File a separate dead-code question.

**R11 — `internal/harness/shared` ships unfenced today.** `grep harness .golangci.yml` returns only
comments — the `gitprobe` extraction added its own block but `shared` did not get one. Whichever unit
lands first must add it; if E1a skipped it, E1c inherits the debt and §5.1 is **mandatory**, not
optional.

**R12 — `shared` stops being stdlib-only.** After prep slice A it pulls `core` + `gitprobe` +
`lifecycle/tmux`, contradicting `seedprompt.go`'s header ("The package is a leaf — stdlib only").
Still leaf-ward and legal. *Mitigation:* update that comment in the same commit or the next reader
concludes the rule was broken.

**R13 — Renaming `ensurePiRefsTrailer` changes five error strings.** They are prefixed
`"daemon: ensurePiRefsTrailer: …"`. *Mitigation:* update the prefix to `"pi: EnsureRefsTrailer: …"`
and call the string change out explicitly in the commit body as the one non-mechanical delta. Leave
`workloop.go:5128/5131`'s stderr text alone — those are the runtime-proof oracle.

**R14 — `pi_profile_resolve.go` gets swept in by name-matching.** It is the single most likely
mistake in this unit. *Mitigation:* §1.3 states the reason; §5.2's guard whitelists it **by name, not
by glob**; the reviewer checks for it explicitly.

**R15 — The daemon still knows pi exists by name after the move.** Five `core.AgentTypePi` branches
survive: `workloop.go:3382` (`resolvedProvider` / `SetResolvedProvider` / `emitProviderSelected`),
`workloop.go:4354` (retain-on-failure), `workloop.go:5125` (the `EnsureRefsTrailer` call site itself),
`dot_cascade.go:1565` (pi stdout capture dir), `bandwidthtuner.go:117` (PI-073 rate-limit isolation) —
plus the pi-named stdout-capture block at `workloop.go:4594-4600`. Recon said four and missed
`:3382`. **[CHALLENGE WINS.]** None references a unit symbol, so none blocks the move.
*Mitigation:* **do not attempt to abstract them in this unit.** Note them in the close comment as
residual daemon knowledge for a later unit.

**R16 — Test LOC is 2.4× the impl LOC.** 3,497 test vs 1,458 impl; ~1,800 of the test LOC needs the
Step 7 rewrite. That, not the impl move, is where the hours go. *Mitigation:* the exported-`RunCtx`
decision (§3b) removes the single heaviest sub-task — the 18 struct literals need no field renames at
all. Budget accordingly and do not let the impl move's simplicity set the estimate.

---

## 8. Rollback

The unit is one bead branch and, given prep slice A, **two commits**. Both are pure `git mv` +
mechanical rewrite, so rollback is a revert, not a repair.

1. **Mid-flight, nothing committed:**
   `git -C /Users/gb/github/harmonik checkout -- . && git -C /Users/gb/github/harmonik clean -fd internal/harness/pi`
   (`git mv` stages, so `checkout --` restores the daemon paths; `clean -fd` removes the new package
   directory and `scripts/p2-freeze-guard.sh` if it was newly created).

2. **E1c committed, prep slice A keeping:** `git revert <E1c-commit>`. Prep slice A is independently
   green and independently useful (it is E1a's own dependency), so leave it. The revert restores the
   five impl files and eight test files to `internal/daemon`, restores the `export_test.go` shims,
   and removes the `harness-pi` depguard rule.

3. **Both commits going:** `git revert <E1c-commit> <prep-A-commit>` in that order. The
   `harness-shared` depguard rule and the `seedprompt.go` header edit revert with it; `internal/harness/shared`
   returns to its stdlib-only pre-E1c shape.

4. **Freeze guard must revert with the code.** If `scripts/p2-freeze-guard.sh` is reverted but the
   `Makefile` hook is not (or vice versa), CI fails on a missing script or passes a concern that is no
   longer frozen. Keep the script, its `chmod +x`, and both `Makefile` hook lines in the **same commit**
   as the move.

5. **Do not roll back by hand-copying files back.** `git mv` preserves rename detection; a manual copy
   destroys the `git log --follow` history for five cold files that have three-to-fifteen commits each
   and are the only record of the PI-030/031/040/042/050 decisions.

**Abandon criterion.** If the verification gate's differential test oracle (gate 4) shows a **new**
failure that is not traceable to a mechanical rewrite within ~1 hour, revert and re-scope rather than
patching forward — the whole warrant for this unit is "zero behavior change," and a behavior delta
means the seam assumption is wrong, not that the diff needs one more fix.
