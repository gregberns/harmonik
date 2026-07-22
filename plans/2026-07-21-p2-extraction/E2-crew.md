# Unit E2 — Crew wiring → `internal/crewrun`

**Status:** NEEDS PREPARATORY SLICE — and the unit is **split in two**.
 - **E2a (this document's executable half) is READY and ships now**: the crew launch-spec builder, the
   crew-scoped harness resolver, the mission front-matter readers, the crew RPC wire contract, and the
   idle-crew reaper. ~616 non-test LOC, zero new seams, mechanical.
 - **E2b (the `crewstart.go` handler) is BLOCKED** on (i) preparatory slice **E2-P** inside `internal/daemon`
   and (ii) an **explicit operator waiver of `_plan.md` §1's no-new-seam rule**. Do not start E2b without both.

**Depends on:** nothing. The plan (`_plan.md` §2, unit E2) says "Depends on E1 landing first (crew
launchspec resolves a harness)". **That is false at the code level and this document overrides it.**
`resolveCrewHarness` (`internal/daemon/crewlaunchspec.go:107`) returns a bare `string`, and
`buildCrewLaunchSpec` (`:145`) rejects anything except the literal `"claude"` with
`crew harness %q not yet supported`. Neither `crewstart.go`, `crewlaunchspec.go`, nor `crewidlereap.go`
references `handlercontract.HarnessRegistry`, `newHarnessRegistry`, or any `core.AgentType`. E2a can land
before, beside, or after E1. Sequence it after E1a for release-cadence reasons only, never as a blocker.

**Size:**
 - **E2a:** ~1,968 LOC across 9 files — **616 non-test**, ~1,293 test, plus a 59-line deletion in
   `internal/daemon/export_test.go`.
 - **E2b (deferred):** ~715 non-test LOC (the `crewstart.go` residue) + ~1,613 test LOC.

**Risk:** **E2a = LOW.** Every moving file is value-in/value-out (argv building, YAML front-matter
parsing, JSON wire structs, a ticker-driven reaper whose sweep is operator-disabled). A mechanical
free-identifier sweep of `crewlaunchspec.go` and `crewidlereap.go` returns **zero** out-of-unit daemon
references, and `go list -deps` on `crew`/`queue`/`handler` shows **no** edge back into `internal/daemon`,
so there is no import cycle. **E2b = MEDIUM-HIGH** — it is not a move; it is a refactor that changes an
exported constructor signature, rewrites every substrate test double in the unit, and (as recon scoped it)
ships a silent behavior delta in the keeper probe.

---

### Where recon and the adversarial challenge disagreed, and what this document took

| Question | Recon said | Challenge said | **Taken** | Why |
|---|---|---|---|---|
| Is the whole unit one slice? | One prep slice (E2-P) + one move slice, all of `crewstart.go` included | Split it: `crewlaunchspec.go` + `crewidlereap.go` move cleanly today; `crewstart.go` needs a sanctioned seam | **Challenge** | Verified. `crewstart.go:596` type-asserts `h.substrate.(substrateWithAdapter)`, whose only method `tmuxAdapter() tmux.Adapter` (`tmuxsubstrate.go:1364,1370`) is **unexported** — package-qualified, so no out-of-package type can ever satisfy or perform that assertion. The two other files have no such tie. |
| Is recon's prep step P3 (a new `crewSpawner` port) neutral code motion? | Yes — "ZERO behavior change" | No — it invents a seam, which `_plan.md` §1 forbids and §5.1 rejects at the review gate | **Challenge** | The landed E1a work (`internal/gitprobe/gitprobe.go` package doc) cites that exact rule as its reason for duplicating rather than injecting. E2b therefore needs an operator waiver, not a recipe. |
| Does P3 preserve behavior? | "ZERO behavior change" | False — the async keeper probe fires only inside the `crewSessionSpawner` arm | **Challenge** | Verified at `crewstart.go:355-358`: `if h.keeperCfg.FlockAcquireGrace > 0 { go h.probeKeeperLiveness(...) }` sits **inside** the independent-session branch; the fallback `SpawnWindow` arm (`:366-387`) never launches it. Hoisting the branch into a port while leaving the probe in the handler arms it on both paths. |
| Are the moving tests a `git mv`? | Yes — "self-contained", "zero out-of-unit daemon deps" | No — every substrate double feeds `newTestCrewHandler`, whose signature P3 changes | **Challenge** | True for `crewstart_*` tests. **Not** true for the E2a set: `crewidlereap_hks2eac_test.go`, `crewlaunchspec_test.go`, and `crewlaunchspec_twoproj_hk25bg_test.go` really are mechanical, which is exactly why E2a is the slice that ships. |
| `windowHandleExposer` / `crewPaneStopper` — doc comments only? | Yes | No — `crewstart_hku5tgh_test.go:140,145` performs a live `sess.(windowHandleExposer)` assertion in a file that STAYS | **Challenge** | Verified by grep. Recon's claim would have deleted a live interface. |
| Both crew scenario tests are `//go:build scenario`? | Yes | Only `scenario_captain_crew_e2e_hkzi4ej_test.go` is | **Challenge** | Verified: `scenario_orphan_sweep_crew_pl006d_hkndq_test.go` opens with a bare `package daemon` and runs under `go test ./...`. |
| Why export `crewKeeperEventBus`? | "an exported func must not have an unexported param type" | Not a Go rule; the real reason is `bootsocket.go:237` **names** `crewKeeperCommsBus` in a `var` declaration | **Challenge** | Right conclusion, wrong reason. Deferred to E2b either way — both interfaces live in `crewstart.go`. |
| The `M9` freeze-tripwire shell one-liner | `git ls-files … \| grep -v … && exit 1` | Inverted: the final `grep` exits 1 on the clean tree | **Challenge** | Correct. §5 below gives a working guard. |
| `loc_estimate: 1206` | Headline number for the unit | Disagrees with the file table (849+185+297 = 1,331) | **Challenge** | Neither number is E2a's. This document publishes measured E2a numbers instead (§6). |
| Recon's file-completeness sweep, no-import-cycle finding, `JoinRemoteControlName` cross-module callers, "socket layer holds `CrewHandler` only as an interface value" | — | Independently re-verified as correct | **Recon** | Re-confirmed here by grep; these are the load-bearing facts E2a rests on. |

One coupling **both** inputs missed, found while writing this: `crewidlereap.go:289` marshals a
`CrewStopRequest`, whose type is declared in `crewstart.go:97`. A launchspec+reaper-only move therefore
does **not** compile unless the crew RPC wire contract moves too. That is why E2a's file list below is
larger than the challenge's "482 LOC clean fallback".

---

## 1. What moves

### E2a — moves in this PR

| File | LOC | Destination | Notes |
|---|---:|---|---|
| `internal/daemon/crewlaunchspec.go` | 185 | `internal/crewrun/launchspec.go` | Whole file. `JoinRemoteControlName`, `crewLaunchCtx`, `crewHarnessClaude`, `resolveCrewHarness`, `buildCrewLaunchSpec`. Imports only `fmt` + `internal/handler`. |
| `internal/daemon/crewidlereap.go` | 297 | `internal/crewrun/idlereap.go` | Whole file. `CrewIdleReaper`, `CrewIdleReaperConfig`, `NewCrewIdleReaper`, plus the `crewStopper` / `crewQueueLookup` / `crewListFunc` seams. Imports only stdlib + `internal/crew` + `internal/queue`. |
| `internal/daemon/crewstart.go` **lines 52–112** | 61 | `internal/crewrun/wire.go` (new) | The `CrewHandler` interface + `CrewStartRequest` + `CrewStopRequest` + `CrewStartResult`. **Forced**: `idlereap.go` needs `CrewStopRequest`. Imports `context` + `encoding/json` only. |
| `internal/daemon/crewstart.go` **lines 622–694** | 73 | `internal/crewrun/missionfrontmatter.go` (new) | `missionFrontMatter`, `readMissionFrontMatter`, `readMissionModel`, `readMissionHarness`, `frontMatterBlock`. Verified exclusive to `crewstart.go` (no other daemon file references them). They feed tiers 2 of `resolveCrewHarness`, so they belong beside it. |
| **Non-test subtotal** | **616** | | `crewstart.go` shrinks 849 → 715. |
| `internal/daemon/crewlaunchspec_test.go` | 684 | `internal/crewrun/launchspec_test.go` | `package daemon_test` → `package crewrun`. Drops `daemon.ExportedBuildCrewLaunchSpec` / `ExportedCrewLaunchCtx` / `ExportedReadMissionModel` / `ExportedReadMissionHarness` / `ExportedResolveCrewHarness` in favour of direct calls. |
| `internal/daemon/crewlaunchspec_twoproj_hk25bg_test.go` | 196 | `internal/crewrun/launchspec_twoproj_hk25bg_test.go` | Same conversion; its only daemon symbol is `daemon.ExportedCrewLaunchCtx`. |
| `internal/daemon/crewidlereap_hks2eac_test.go` | 413 | `internal/crewrun/idlereap_hks2eac_test.go` | `package daemon` → `package crewrun`, no other change. Drives `scan()`/`checkCrew()` directly; imports only stdlib + `crew` + `queue`. |
| `internal/daemon/export_test.go` **lines 2682–2740** | −59 | *deleted* | The five `Exported*` crew-launchspec seams, dead after the conversion above. |
| **Test subtotal** | **1,293 moved, 59 deleted** | | |

### Files that STAY in `internal/daemon` (and why)

| File | Why it stays |
|---|---|
| `internal/daemon/crewstart.go` (715-LOC residue) | Deferred to E2b. Contains the structural blocker: `crewstart.go:596` asserts `h.substrate.(substrateWithAdapter)`, an interface whose only method `tmuxAdapter()` is unexported (`tmuxsubstrate.go:1364`). Also holds `crewHandlerImpl`, `NewCrewHandler`, `WithKeeperProbe`, `WithCrewsConfig`, `windowHandleExposer`, `crewPaneStopper`, `pasteCrewMission`, `crewPasteInjector`, `pasteCrewMissionToSession`, `probeKeeperLiveness`, `createCrewManagedMarker`. |
| `internal/daemon/orphansweep.go` (1,049) | Different concern: worktree lease-locks, `br` subprocess reaping, coordinator/captain sentinels, tmux session sweep. Only ~115 LOC (`probeCrewRegistrySessions`) touch the crew registry, and as a **reader**. |
| `internal/daemon/quiesce.go`, `statedisk.go`, `stategather.go` | Crew-registry **readers** (quiesce state, `harmonik state`, live-state builder). Reading `internal/crew` does not make a file crew wiring. |
| `internal/daemon/tmuxsubstrate.go` | Owns `crewSessionSpawner` (`:1585`), `crewSessionStopper` (`:1599`), `paneTargeter` (`:1350`), `substrateWithAdapter` (`:1364`), `paneCaptureAdapter` (`:2342`), `newPerRunSubstrate` (`:2029`). All tmux-native. |
| `internal/daemon/pasteinject.go` | Owns `injectAndVerifySeed` (`:1703`), `sendSubmitEnterWithRetry` (`:1799`), `splashDismissWait` (`:1494`), `bufferName` (`:2596`), `errPaneCaptureUnsupported` (`:224`), `enterSender` (`:188`) and the `pasteVerify*`/`splashDismissDelay*`/`resumeSubmitRetry*` tunables. Genuinely cross-cutting (4 call sites) — see §7 R7. |
| `internal/daemon/crewstart_hkdvcc7_test.go` (185), `crewstart_hkjzpqo_test.go` (149) | Pure `pasteCrewMission` verify-before-submit / paste-ordering tests; mutate `pasteVerifyAttempts`, `pasteVerifyBackoffNs`, `splashDismissDelayNs`, `resumeSubmitRetries`. Tests follow the code, and the code stays. |
| `internal/daemon/crewstart_hku5tgh_test.go` (215) | **Named `crewstart_` but is not a crew-handler test.** It is a `*tmuxSubstrate.SpawnCrewSession` collision-recovery test, and at `:140`/`:145` it performs a live `sess.(windowHandleExposer)` assertion. |
| `internal/daemon/hknddg1_crewqueue_provenance_test.go` (173) | **Named `crewqueue` but is not crew wiring.** Tests `daemon.ExportedLoadQueueProvenance` + `daemon.RunOrphanSweep`. |
| `internal/daemon/scenario_captain_crew_e2e_hkzi4ej_test.go` (1,066, `//go:build scenario`) | Boots the whole daemon via `daemon.StartForTesting` and **scripts** the crew-start mechanical effects (`crew.Write`/`crew.List` directly) rather than calling the handler. |
| `internal/daemon/scenario_orphan_sweep_crew_pl006d_hkndq_test.go` (465) | Orphan-sweep scenario. **No build tag** — it runs under a bare `go test ./...`. |
| `internal/daemon/crewstart_hk5tg5o_test.go` (474), `crewstart_hkp006e_test.go` (70), `crewstart_hkvrnh3_test.go` (112), `crewstart_type_hkdy5gw_test.go` (77), `crewstart_keeper_probe_hkqgfme_test.go` (255), `crewstart_hkmmlqt_test.go` (425) | All drive `crewHandlerImpl` through `newTestCrewHandler`. They move only with E2b. |

---

## 2. The seam it exits behind

**Pre-existing seam #1 — the `internal/crew` registry leaf.** Declared a depguard-fenced leaf at
`.golangci.yml:154-162` (`crew:` rule, `allow: [$gostd, internal/core, internal/crew]`,
`deny: internal/daemon`), rationale `docs/plans/captain/05-specs/c2-spec.md §3.3`, bead `hk-i1ue4`.
`crewidlereap.go` reaches durable crew state exclusively through `crew.List` / `crew.Record`
(`internal/daemon/crewidlereap.go:71`, the `crewListFunc` seam).

**Pre-existing seam #2 — `handler.LaunchSpec`.** `buildCrewLaunchSpec`
(`internal/daemon/crewlaunchspec.go:134`) returns `handler.LaunchSpec` (`internal/handler`), the same
value type every other launch-spec builder in the tree returns. Nothing new.

**Pre-existing seam #3 — the `internal/queue` read API.** `crewQueueLookup`
(`internal/daemon/crewidlereap.go:66`) is `QueueByName(name string) *queue.Queue`, satisfied by
`*QueueStore.QueueByName`. It already exists as an interface in the moving file.

**Pre-existing seam #4 — the `CrewHandler` RPC contract.** `internal/daemon/crewstart.go:60`. The socket
layer holds it **only as an interface value**: `socket.go:330`/`:339`/`:357` take it as a parameter,
`socketdispatch.go:36` stores it in a field and `:313-334` registers the two ops,
`bootstate.go:56` caches it, `socket_state.go:49` and `socket_dashboard.go:53` pass it through. Six
call sites, all import-path rewrites. `crewidlereap.go:60`'s `crewStopper` interface is already the
narrow read of it.

**No new seam is invented in E2a.** Every interface that crosses the new package boundary
(`CrewHandler`, `crewStopper`, `crewQueueLookup`, `crewListFunc`, `handler.LaunchSpec`) exists today,
in the files being moved or in a package they already import.

### 🚩 RED FLAG — E2b needs a new port, and that needs an operator waiver

To move `crewstart.go`, recon's recipe creates `internal/daemon/crewspawnport.go` and a brand-new
`crewSpawner` interface. `_plan.md` §1 states the goal as *"moving implementations out of the two god
packages behind seams that **ALREADY EXIST** — not by inventing new seams"*, and §5.1 makes it a review-gate
**rejection**: *"agent-reviewer unwanted-abstraction check: extraction must NOT invent a new seam."* The
already-landed E1a work honoured this literally — `internal/gitprobe/gitprobe.go`'s package doc says
injecting the git probe as a function field *"would have paid for a seam twice and invented one the
extraction plan says not to invent."*

E2b is therefore **not executable under the current plan**. It requires the operator to either (a) waive
§1/§5.1 for this unit, or (b) accept `crewstart.go` staying in `internal/daemon` indefinitely. Ship E2a,
then put the waiver question to the operator with the §7 R2/R3 costs attached. Do not smuggle the port in.

---

## 3. Coupling to break

### 3a. Outbound (unit → daemon)

After E2a's file selection, `internal/crewrun` has **zero** outbound couplings into `internal/daemon`.
The table below records what was checked and why each row is resolved.

| Symbol | Defined in | Used by | Resolution |
|---|---|---|---|
| `CrewStopRequest` | `internal/daemon/crewstart.go:97` | `crewidlereap.go:289` (`json.Marshal(CrewStopRequest{...})`) | **Moves with the unit** into `internal/crewrun/wire.go`. This is the coupling that forces the wire contract into E2a. |
| `readMissionModel` / `readMissionHarness` | `crewstart.go:668` / `:676` | `crewstart.go:298` / `:303` — the *only* callers | **Move with the unit.** After the move, the daemon-side `crewstart.go` calls `crewrun.ReadMissionModel` / `crewrun.ReadMissionHarness` (exported — see §3b). |
| `readMissionFrontMatter`, `missionFrontMatter`, `frontMatterBlock` | `crewstart.go:643`/`:630`/`:683` | `readMissionModel`/`readMissionHarness` only — grep-verified no other daemon file touches them | **Move with the unit, stay unexported** in `crewrun`. |
| `agentmanifest.Load` | `internal/agentmanifest` | `bootsocket.go:258`, inside the `CrewIdleReaperConfig.PersistentType` **func literal** | **Already injected.** `PersistentType` is a `func(string) bool` field (`crewidlereap.go:105`), so the manifest read stays in the daemon composition root. No change. |
| `*QueueStore` | `internal/daemon` | `bootsocket.go:253` sets `Queues: bs.qs` | **Structural satisfaction.** `crewQueueLookup` has one exported method (`QueueByName`); `*QueueStore` satisfies the `crewrun`-declared copy with no daemon-side change. |
| `crewHandlerImpl` | `crewstart.go:135` | `bootsocket.go:254` sets `Stopper: bs.crewHandler` | **Structural satisfaction.** `crewStopper`'s one method `HandleCrewStop` is exported. |
| `substrateWithAdapter` (`tmuxAdapter() tmux.Adapter`) | `tmuxsubstrate.go:1364` (impl `:1370`) | `crewstart.go:596` | **E2b BLOCKER — not in E2a scope.** The method is unexported, so it is package-qualified: an identically-shaped interface declared in `internal/crewrun` can **never** be satisfied by `*tmuxSubstrate`, and the assertion itself cannot be written outside `internal/daemon`. This is a compile error, not a lint warning. |
| `newPerRunSubstrate` / `perRunSubstrate` | `tmuxsubstrate.go:2029` / `:1227` | `crewstart.go:369` (fallback `SpawnWindow` branch) | E2b. Unexported constructor returning an unexported concrete type. |
| `injectAndVerifySeed`, `sendSubmitEnterWithRetry`, `splashDismissWait`, `bufferName`, `errPaneCaptureUnsupported` | `pasteinject.go:1703`, `:1799`, `:1494`, `:2596`, `:224` | `crewstart.go:531`, `:543`, `:514`/`:541`, `:516`, `:576` | E2b. `errPaneCaptureUnsupported` is a sentinel that `injectAndVerifySeed` does `errors.Is()` against — it **cannot** be duplicated; it must live in the same package as the verify loop. |
| `pasteInjecter`, `enterSender`, `paneCaptureAdapter`, `paneTargeter`, `crewSessionSpawner`, `crewSessionStopper` | `tmuxsubstrate.go:78`, `pasteinject.go:188`, `tmuxsubstrate.go:2342`, `:1350`, `:1585`, `:1599` | `crewstart.go:507`, `:509`/`:542`, `:574`, `:588`, `:334`, `:811` | E2b. All have exported method sets, so they are structurally re-declarable — cheap **individually**, but they only matter once the blocker above is resolved. |
| `OperatorControlHandler` | `operatorpause.go:42` | `crewstart.go:139`, `:211`, `:837` | E2b. Only `HandleOperatorPause(ctx, queueName)` is ever called; a narrow `QueuePauser` port in `crewrun` would be satisfied structurally by `*OperatorPauseController`. |
| `KeeperConfig` | `projectconfig.go:537` | `crewstart.go:144`, `:168`, `:356` | E2b prep. A 40+-field struct of which `crewstart.go` reads exactly one field (`FlockAcquireGrace`). Narrow it to a `time.Duration`. |
| `CrewConfig` | `projectconfig.go:1213` | `crewstart.go:152`, `:180`, `:193` | E2b prep. A one-field struct `{Harness string}`; narrow `map[string]CrewConfig` → `map[string]string`. |

### 3b. Inbound (daemon → unit)

| Current name | Proposed exported name | Call sites to rewrite |
|---|---|---|
| `CrewHandler` *(already exported)* | `crewrun.CrewHandler` | `bootstate.go:56`, `socket.go:330`/`:339`/`:357`, `socketdispatch.go:36`/`:313-334`, `socket_state.go:49`, `socket_dashboard.go:53`, `bootsocket.go:241`/`:254`/`:305`, `bootworkloop.go:163`, `scheduletick.go:66-67` (comment), `crewstart.go` (the residual `crewHandlerImpl` now returns `crewrun.CrewHandler`) |
| `CrewStartRequest`, `CrewStopRequest`, `CrewStartResult` *(already exported)* | `crewrun.CrewStartRequest`, `crewrun.CrewStopRequest`, `crewrun.CrewStartResult` | `scheduletick.go:277`, `crewstart.go` (residue), `scheduletick_test.go`, `internal/sentinel/adversary.go:56-59` (**comment only** — `adversaryCrewStartRequest` deliberately mirrors the shape without importing daemon; update the comment to say `crewrun.CrewStartRequest`) |
| `CrewIdleReaper`, `CrewIdleReaperConfig`, `NewCrewIdleReaper` *(already exported)* | `crewrun.CrewIdleReaper`, `crewrun.CrewIdleReaperConfig`, `crewrun.NewCrewIdleReaper` | `bootsocket.go:251-260`, `bootstate.go:57`, `bootworkloop.go:238`, `branchreapwatcher.go:12` (comment) |
| `JoinRemoteControlName` *(already exported)* | `crewrun.JoinRemoteControlName` | `cmd/harmonik/captain.go:258`, `cmd/harmonik/captain_respawn.go:90` (**cross-module**, called as `daemon.JoinRemoteControlName`), `cmd/harmonik/captain.go:248` (comment), `internal/daemon/projectconfig.go:677` (comment), `crewstart.go:202` (comment). `cmd/harmonik/captain_rcprefix_hkw8ex_test.go:6` mentions it in a comment only. |
| `buildCrewLaunchSpec` | **`BuildCrewLaunchSpec`** | `crewstart.go:304` |
| `crewLaunchCtx` | **`CrewLaunchCtx`** (fields exported: `ClaudeBinary`, `Name`, `RcPrefix`, `SessionID`, `ProjectDir`, `Resume`, `Model`, `Harness` — same names `ExportedCrewLaunchCtx` already uses at `export_test.go:2686`) | `crewstart.go:304` |
| `resolveCrewHarness` | **`ResolveCrewHarness`** | `crewstart.go:303` |
| `readMissionModel` | **`ReadMissionModel`** | `crewstart.go:298` |
| `readMissionHarness` | **`ReadMissionHarness`** | `crewstart.go:303` |
| `crewStopper`, `crewQueueLookup`, `crewListFunc` | **stay unexported** in `crewrun` | `bootsocket.go:253-254` sets `Queues`/`Stopper` via a composite literal without naming the types — legal today, legal after. No rewrite needed. |
| `crewHarnessClaude` | stays unexported | internal to `launchspec.go` |
| `missionFrontMatter`, `readMissionFrontMatter`, `frontMatterBlock` | stay unexported | internal to `missionfrontmatter.go` |

**Symbols needing a new export: 5** — `BuildCrewLaunchSpec`, `CrewLaunchCtx` (+ its 8 fields),
`ResolveCrewHarness`, `ReadMissionModel`, `ReadMissionHarness`. All five already have an exported
mirror in `export_test.go:2682-2740`; this move **deletes** those mirrors, so the net export surface
does not grow.

---

## 4. Step-by-step recipe (E2a)

Run every command from the repo root `/Users/gb/github/harmonik`. Never `cd` into a worktree.

**Step 0 — confirm tree state.** `internal/gitprobe/` and `internal/harness/shared/` are staged-but-
uncommitted, and `.golangci.yml` is staged-modified with the `gitprobe:` depguard block your new block
sits next to. A rebase that drops that work also drops your insertion anchor.

```bash
git -C /Users/gb/github/harmonik status --porcelain .golangci.yml internal/gitprobe internal/harness
# expect: "M  .golangci.yml", "A  internal/gitprobe/…", "A  internal/harness/shared/…"
grep -n "gitprobe:" /Users/gb/github/harmonik/.golangci.yml   # expect a hit around line 172
```

**Step 1 — capture the differential test oracle.** `internal/daemon` is red before you start; see
`plans/2026-07-21-p2-extraction/00-test-oracle-baseline.md`. Do this with nothing else running.

```bash
cd /Users/gb/github/harmonik && go test ./internal/daemon/... ./internal/harness/... -count=1 -timeout 25m 2>&1 \
  | grep -E '^--- FAIL' | sort -u > /tmp/e2a-before-failures.txt
```

**Step 2 — create the package and move the two whole files.**

```bash
mkdir -p /Users/gb/github/harmonik/internal/crewrun
git -C /Users/gb/github/harmonik mv internal/daemon/crewlaunchspec.go internal/crewrun/launchspec.go
git -C /Users/gb/github/harmonik mv internal/daemon/crewidlereap.go   internal/crewrun/idlereap.go
```

**Step 3 — cut the two blocks out of `crewstart.go` into new files.** Move verbatim; do not reflow.

- `internal/daemon/crewstart.go` **lines 52–112** → new `internal/crewrun/wire.go`
  (`CrewHandler` interface, `CrewStartRequest`, `CrewStopRequest`, `CrewStartResult`).
  Imports: `context`, `encoding/json`.
- `internal/daemon/crewstart.go` **lines 622–694** → new `internal/crewrun/missionfrontmatter.go`
  (`missionFrontMatter`, `readMissionFrontMatter`, `readMissionModel`, `readMissionHarness`,
  `frontMatterBlock`). Imports: `os`, `strings`, `gopkg.in/yaml.v3`. Carry the `//nolint:gosec // G304`
  directive on the `os.ReadFile` at `:650` verbatim — `nolintlint` is configured
  `require-explanation: true, require-specific: true` (`.golangci.yml:87`).

**Step 4 — set the package clause and write a package doc.** In all four `internal/crewrun/*.go` files
change `package daemon` → `package crewrun`. `revive` runs with `{name: package-comments}` and
`{name: exported}` (`.golangci.yml:85`), so exactly one file must carry a package doc **immediately
above** the package clause. `crewlaunchspec.go`'s existing header comment sits *below* its package
clause and does **not** count. Put this at the top of `internal/crewrun/launchspec.go`:

```go
// Package crewrun owns the daemon-side crew launch contract: the crew-start /
// crew-stop RPC payloads, the persistent-session launch-spec builder, the
// crew-scoped harness resolver, the mission-handoff front-matter readers, and
// the idle-completed-crew reaper.
//
// It is a leaf behind the internal/crew registry seam (.golangci.yml `crew:`
// rule) and the handler.LaunchSpec value type. The daemon is the composition
// root: it threads the queue lookup and the crew stopper IN. crewrun MUST NOT
// import internal/daemon back — see the `crewrun:` depguard rule.
//
// Spec ref: docs/plans/captain/05-specs/c2-spec.md §3.1–§3.5.
// Plan ref: plans/2026-07-21-p2-extraction/_plan.md unit E2 (slice E2a).
package crewrun
```

**Step 5 — export the five symbols.** Rename in place inside `internal/crewrun`, keeping every doc
comment and starting each one with the new name (`revive`'s `exported` rule requires
`// BuildCrewLaunchSpec …`, not `// buildCrewLaunchSpec …`):

| Old | New |
|---|---|
| `buildCrewLaunchSpec` | `BuildCrewLaunchSpec` |
| `crewLaunchCtx` + fields `claudeBinary,name,rcPrefix,sessionID,projectDir,resume,model,harness` | `CrewLaunchCtx` + `ClaudeBinary,Name,RcPrefix,SessionID,ProjectDir,Resume,Model,Harness` |
| `resolveCrewHarness` | `ResolveCrewHarness` |
| `readMissionModel` | `ReadMissionModel` |
| `readMissionHarness` | `ReadMissionHarness` |

Leave `crewHarnessClaude`, `missionFrontMatter`, `readMissionFrontMatter`, `frontMatterBlock`,
`crewStopper`, `crewQueueLookup`, `crewListFunc` unexported.

**Step 6 — carry the operator-disabled reaper across verbatim.** `CrewIdleReaper.StartWatcher`
(`idlereap.go`, was `crewidlereap.go:166-169`) has an intentionally empty body — the sweep was disabled
by operator directive 2026-07-18 (beads `hk-98at0`, `hk-do173`) because it was killing live crews.
`loop`/`scan`/`checkCrew`/`reap` are retained unreferenced behind `//nolint:unused // retained for a
one-line revert of the operator-disabled sweep (hk-s2eac)` (`:172`). **Do not "tidy up dead code."**
Carry the empty body, the `//nolint` directives, and the explanatory comment block unchanged.

**Step 7 — rewrite the daemon and cmd call sites.** Add
`"github.com/gregberns/harmonik/internal/crewrun"` and qualify:

```
internal/daemon/crewstart.go        :52-112 and :622-694 deleted; :298 → crewrun.ReadMissionModel;
                                    :303 → crewrun.ResolveCrewHarness(req.Harness,
                                           crewrun.ReadMissionHarness(req.MissionPath), …);
                                    :304 → crewrun.BuildCrewLaunchSpec(crewrun.CrewLaunchCtx{…});
                                    NewCrewHandler return type → crewrun.CrewHandler
internal/daemon/bootstate.go        :56 crewHandler crewrun.CrewHandler; :57 *crewrun.CrewIdleReaper
internal/daemon/bootsocket.go       :251 crewrun.NewCrewIdleReaper(crewrun.CrewIdleReaperConfig{…})
internal/daemon/bootworkloop.go     :163, :238 — no type names, verify only
internal/daemon/socket.go           :330, :339, :357 — crewh/Crew param+field → crewrun.CrewHandler
internal/daemon/socket_state.go     :49  crewh crewrun.CrewHandler
internal/daemon/socket_dashboard.go :53  crewh crewrun.CrewHandler
internal/daemon/socketdispatch.go   :36  crewh crewrun.CrewHandler; :313-334 comment fixups
internal/daemon/scheduletick.go     :277 crewrun.CrewStartRequest; :66-67 comment fixup
internal/daemon/branchreapwatcher.go:12  comment fixup (CrewIdleReaper)
cmd/harmonik/captain.go             :258 daemon.JoinRemoteControlName → crewrun.JoinRemoteControlName
                                    :248 comment fixup
cmd/harmonik/captain_respawn.go     :90  daemon.JoinRemoteControlName → crewrun.JoinRemoteControlName
                                    :78  comment fixup
internal/daemon/projectconfig.go    :677 comment fixup
internal/sentinel/adversary.go      :56-59 comment fixup (mirrors crewrun.CrewStartRequest now)
cmd/harmonik/crew.go                :414 comment: createCrewManagedMarker still lives in daemon — no change
```

**Step 8 — move and convert the tests.**

```bash
git -C /Users/gb/github/harmonik mv internal/daemon/crewidlereap_hks2eac_test.go \
    internal/crewrun/idlereap_hks2eac_test.go
git -C /Users/gb/github/harmonik mv internal/daemon/crewlaunchspec_test.go \
    internal/crewrun/launchspec_test.go
git -C /Users/gb/github/harmonik mv internal/daemon/crewlaunchspec_twoproj_hk25bg_test.go \
    internal/crewrun/launchspec_twoproj_hk25bg_test.go
```

- `idlereap_hks2eac_test.go`: `package daemon` → `package crewrun`. Nothing else — it imports only
  stdlib + `internal/crew` + `internal/queue`.
- `launchspec_test.go` and `launchspec_twoproj_hk25bg_test.go`: `package daemon_test` → `package crewrun`;
  drop the `"github.com/gregberns/harmonik/internal/daemon"` import; replace
  `daemon.ExportedBuildCrewLaunchSpec` → `BuildCrewLaunchSpec`,
  `daemon.ExportedCrewLaunchCtx` → `CrewLaunchCtx`,
  `daemon.ExportedReadMissionModel` → `ReadMissionModel`,
  `daemon.ExportedReadMissionHarness` → `ReadMissionHarness`,
  `daemon.ExportedResolveCrewHarness` → `ResolveCrewHarness`,
  `daemon.JoinRemoteControlName` → `JoinRemoteControlName`.
- **Name-collision guard.** `argvHasFlag` is declared twice in the tree today —
  `crewlaunchspec_test.go:36` (`package daemon_test`) and `crewstart_hkmmlqt_test.go:404`
  (`package daemon`) — legal only because the packages differ, and both have the identical signature
  `func(  []string, string) bool`. `crewstart_hkmmlqt_test.go` **stays in daemon** under E2a, so this
  is safe now. It becomes a redeclaration error the moment E2b moves that file; note it in the E2b
  handoff. `containsPair` (`crewstart_hkmmlqt_test.go:415`) has the same shape and the same hazard.

**Step 9 — delete the dead export seams.** Remove `internal/daemon/export_test.go` **lines 2682–2740**:
the `buildCrewLaunchSpec test seams (hk-kbqto C2)` banner, `ExportedCrewLaunchCtx`,
`ExportedBuildCrewLaunchSpec`, `ExportedReadMissionModel`, `ExportedReadMissionHarness`,
`ExportedResolveCrewHarness`. Nothing else in the tree references them (grep-verified). **This is the
most rebase-fragile hunk in the PR** — `export_test.go` is a shared, high-contention file (a sibling
`export_cjqyn_test.go` exists specifically "to avoid a shared-index race with other crews editing
export_test.go"). Do this hunk last and rebase immediately before pushing.

**Step 10 — add the depguard rule.** Insert immediately after the existing `gitprobe:` block in
`.golangci.yml` (currently ending ~line 179), copying that block's idiom exactly. Indentation is
8 spaces for the rule name, matching its neighbours.

```yaml
        # crewrun: the daemon-side crew launch contract — crew-start/crew-stop RPC
        # payloads, the persistent-session launch-spec builder, the crew-scoped
        # harness resolver, the mission front-matter readers, and the idle-crew
        # reaper — extracted from internal/daemon behind the already-fenced
        # internal/crew registry leaf and the handler.LaunchSpec value type
        # (P2 unit E2, slice E2a). The daemon composition root threads the queue
        # lookup and the crew stopper IN. crewrun MUST NOT import daemon back —
        # that back-edge re-links the ~59k-LOC monolith into the crew launch path
        # and voids the extraction.
        # Rationale: plans/2026-07-21-p2-extraction/_plan.md unit E2.
        crewrun:
          files: ["**/internal/crewrun/**"]
          allow:
            - "$gostd"
            - "gopkg.in/yaml.v3"
            - "github.com/gregberns/harmonik/internal/crew"
            - "github.com/gregberns/harmonik/internal/handler"
            - "github.com/gregberns/harmonik/internal/queue"
            - "github.com/gregberns/harmonik/internal/crewrun"
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "crewrun is a leaf behind the crew-registry + handler.LaunchSpec seams; importing daemon back voids P2 E2" }
```

> If and only if E2b later lands, append these four to `allow:` (they are `crewstart.go`'s remaining
> imports): `github.com/google/uuid`, `github.com/gregberns/harmonik/internal/core`,
> `github.com/gregberns/harmonik/internal/keeper`,
> `github.com/gregberns/harmonik/internal/lifecycle/tmux`. Do **not** add them pre-emptively —
> an over-wide `allow` list is a silent invitation.

**Step 11 — add the freeze tripwire.** See §5.

**Step 12 — run the verification gate.** See §6. Publish the measured LOC/file drop in the bead close
comment, per `_plan.md` §5.6.

---

## 5. Freeze tripwire

Two mechanisms, both hard CI failures (operator-resolved: hard failure, not a warning).

### 5a. The depguard deny edge

Already given in Step 10 — the `deny:` entry:

```yaml
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "crewrun is a leaf behind the crew-registry + handler.LaunchSpec seams; importing daemon back voids P2 E2" }
```

**Caveat you must handle:** CI runs `golangci-lint run --new-from-rev=origin/main`
(`Makefile:442`), which reports issues only in changed lines. That is fine for a brand-new package
(every line is new) but means a *later* back-edge added to an otherwise-untouched `crewrun` file might
slip past. The grep-guard below is the belt to that suspenders, and §6 runs the linter **without**
`--new-from-rev` over the new package.

### 5b. The "no new files in daemon for this concern" grep-guard

A depguard file-glob cannot express "no *new* file", so this is a grep-guard. Note the explicit `if`:
the naive `git ls-files … | grep -v … && exit 1` form is **inverted** — the final `grep` exits 1 when it
matches nothing, so under `set -e` a clean tree fails and a dirty tree also fails.

Create `/Users/gb/github/harmonik/scripts/p2-freeze-guard.sh`:

```bash
#!/usr/bin/env bash
# p2-freeze-guard.sh — P2 EXTRACTION freeze tripwire (_plan.md §3.2).
# Fails the build when a NEW non-test file for an already-extracted concern
# appears under internal/. Extracted concern = closed door.
set -euo pipefail
cd "$(dirname "$0")/.."

fail=0

# E2a: crew launch-spec / harness resolver / idle reaper now live in
# internal/crewrun. internal/daemon/crewstart.go is the ONE sanctioned residue
# (the tmux-native crew-start handler, deferred to E2b).
offenders="$(git ls-files 'internal/daemon/crew*.go' \
  | grep -v '_test\.go$' \
  | grep -vx 'internal/daemon/crewstart.go' || true)"
if [ -n "$offenders" ]; then
  echo "P2 FREEZE VIOLATION (unit E2a): crew wiring lives in internal/crewrun." >&2
  echo "$offenders" >&2
  echo "Add the file to internal/crewrun, or amend the sanctioned list in scripts/p2-freeze-guard.sh with a written rationale." >&2
  fail=1
fi

exit "$fail"
```

Make it executable (`chmod +x scripts/p2-freeze-guard.sh`) and hook it into the gating CI tier by
adding one line to the `check-short:` recipe in `Makefile` (immediately after `go build ./...`,
`Makefile:441`):

```make
	./scripts/p2-freeze-guard.sh
```

`.github/workflows/ci.yml:42-43` runs `make check-short` as the merge-blocking Tier-2 job, so this
becomes a hard failure with no further workflow edit. When E2b lands, delete the
`grep -vx 'internal/daemon/crewstart.go'` line and the guard closes the door completely.

---

## 6. Verification gate

Run in this order, from the repo root. Every step must pass before the next.

| # | Command | Pass criterion |
|---|---|---|
| 1 | `grep -rn 'internal/daemon' /Users/gb/github/harmonik/internal/crewrun/` | **Prints nothing.** The boundary test (`_plan.md` §3.4). If this prints, stop — the extraction is void. |
| 2 | `cd /Users/gb/github/harmonik && go build ./internal/... ./cmd/...` | Exit 0. Catches the `cmd/harmonik` `JoinRemoteControlName` swap and every import-path rewrite. |
| 3 | `cd /Users/gb/github/harmonik && go vet ./internal/... ./cmd/...` | Exit 0. |
| 4 | `cd /Users/gb/github/harmonik && go test ./internal/crewrun/... -count=1` | **All green, no exceptions.** The new package is small and hermetic; a failure here is a real defect, never a flake. |
| 5 | `cd /Users/gb/github/harmonik && go test ./internal/daemon/... ./internal/harness/... -count=1 -timeout 25m 2>&1 \| grep -E '^--- FAIL' \| sort -u > /tmp/e2a-after-failures.txt; comm -13 /tmp/e2a-before-failures.txt /tmp/e2a-after-failures.txt` | **Empty output.** Differential green per `00-test-oracle-baseline.md` — `internal/daemon` is red at baseline (`TestThroughput_TenBeadsAtMaxFour` is a hard pre-existing failure; five others are load-sensitive flakes). Serialize this run: do **not** run it concurrently with an agent fan-out or a parallel build. Any new name in the after-set gets re-run in isolation before being called a regression. |
| 6 | `cd /Users/gb/github/harmonik && go test -tags scenario ./internal/daemon/... -count=1 -timeout 25m` | Green (or same-as-baseline). **Required**: `scenario_captain_crew_e2e_hkzi4ej_test.go` carries `//go:build scenario` and does **not** run in step 5. It calls `daemon.StartForTesting` and asserts the crew registry + queue surfaces. Skipping this ships a break undetected. (`scenario_orphan_sweep_crew_pl006d_hkndq_test.go` has **no** tag and is already covered by step 5.) |
| 7 | `cd /Users/gb/github/harmonik && ./bin/golangci-lint run ./internal/crewrun/... ./internal/daemon/... ./cmd/...` | Exit 0. Run **without** `--new-from-rev` so the `crewrun:` depguard rule and `revive`'s `exported` / `package-comments` rules are evaluated across the whole new package, not just changed lines. |
| 8 | `cd /Users/gb/github/harmonik && ./scripts/p2-freeze-guard.sh` | Exit 0. |
| 9 | `cd /Users/gb/github/harmonik && ubs $(git diff --name-only HEAD)` | Exit 0. |
| 10 | The measurement below | Numbers match §1 within ±5 LOC. |

### Success metric — the LOC/file drop out of `internal/daemon`

Measured baseline, working tree, 2026-07-22 (note: `_plan.md` §0's "631 non-test files" counts something
else; these are the numbers the guard and the close comment should use):

```bash
cd /Users/gb/github/harmonik
find internal/daemon -name '*.go' ! -name '*_test.go' | wc -l           # BEFORE: 134
find internal/daemon -name '*.go' ! -name '*_test.go' -exec cat {} + | wc -l   # BEFORE: 58,971
```

**Expected after E2a:**

| Metric | Before | After | Delta |
|---|---:|---:|---:|
| `internal/daemon` non-test files | 134 | **132** | **−2** |
| `internal/daemon` non-test LOC | 58,971 | **58,355** | **−616** |
| `internal/daemon` test LOC (the 3 moved files) | — | — | **−1,293** |
| `internal/daemon/export_test.go` | — | — | **−59** |
| New `internal/crewrun` | 0 | 4 non-test files, 616 LOC + 3 test files, 1,293 LOC | — |

Publish `−616 non-test LOC / −2 non-test files out of internal/daemon` in the bead close comment
(`_plan.md` §5.6). Do **not** publish `1,206` or `1,331` — those are recon's whole-unit estimates and
include the deferred E2b residue.

---

## 7. Risks and how each is mitigated

**R1 — `substrateWithAdapter` makes a wholesale `crewstart.go` move a compile error, not a lint warning.**
`tmuxsubstrate.go:1364` declares `type substrateWithAdapter interface { tmuxAdapter() tmux.Adapter }` —
one method, **unexported**, implemented at `:1370`. `crewstart.go:596` asserts `h.substrate` to it to
reach the tmux adapter for the crew paste injector. Unexported method names are package-qualified in Go:
no `internal/crewrun` type can satisfy it and no `internal/crewrun` code can perform the assertion.
*Mitigation:* E2a does not move `crewstart.go`. Anyone who "just tries the move" will hit this in the
first `go build`; that is the intended failure mode, not a surprise.

**R2 — E2b requires inventing a seam that `_plan.md` §1 forbids and §5.1 rejects at the review gate.**
Recon's prep step P3 creates a new `crewSpawner` port in `internal/daemon/crewspawnport.go`. The landed
E1a `internal/gitprobe` package doc cites that exact rule as its reason for *not* doing the equivalent.
*Mitigation:* E2b is gated on an explicit operator waiver, stated as a red flag in §2. Do not fold the
port into an "extraction" PR and let the reviewer discover it.

**R3 — Recon's "ZERO behavior change" claim for E2b is false.** The async keeper-liveness probe
(`crewstart.go:355-358`) fires **only** inside the `if css, ok := h.substrate.(crewSessionSpawner)` arm;
the fallback `SpawnWindow` arm (`:366-387`) never launches it. Recon says the probe "must stay in the
HANDLER, not the port" — but hoisting the branch into the port while leaving the probe in the handler
arms it on **both** paths, silently. *Mitigation:* if E2b is ever sanctioned, its design must state
explicitly which of three routes it takes — port returns which path it took, probe moves into the port,
or the delta is accepted and documented. This document does not pre-decide it.

**R4 — E2b's test budget is roughly double what recon states.** `NewCrewHandler`'s
`substrate handler.Substrate` parameter is replaced by the new port, which invalidates
`newTestCrewHandler(t, sub handler.Substrate, opCtrl)` (`crewstart_hk5tg5o_test.go:85`) and **every**
substrate double that feeds it: `fakeSubstrate` (`:31`), `spawnCheckSubstrate` (`:200`),
`managedCheckSubstrate` (`crewstart_hkp006e_test.go:29`), `hkmmlqtCrewSessionSubstrate`
(`crewstart_hkmmlqt_test.go:102`). ~1,600 test LOC of seam rewrites, not `git mv`. Recon calls them
"self-contained" / "zero out-of-unit daemon deps" — true about *imports*, false about *mechanicality*.
*Mitigation:* stated here so the E2b estimate is honest before anyone commits to it.

**R5 — E2b's four independent-session tests must NOT move to `crewrun`.**
`TestCrewStart_IndependentSession_hkmmlqt` (`:155`), `TestCrewStop_IndependentSession_hkmmlqt` (`:194`),
`TestCrewStart_FallbackToSpawnWindow_hkmmlqt` (`:214`), `TestCrewStop_FallbackToStopWindowByHandle_hkmmlqt`
(`:228`) assert exactly the `crewSessionSpawner`-vs-`SpawnWindow` and
`crewSessionStopper`-vs-`crewPaneStopper` dispatch that a spawn port relocates into `internal/daemon`.
Recon's M6 sends them to `internal/crewrun/handler_independentsession_test.go`, where they would assert
nothing. *Mitigation:* recorded here; they follow the port, like `crewstart_hkdvcc7_test.go` and
`crewstart_hkjzpqo_test.go` already do.

**R6 — Filenames lie.** Three files are not what their names suggest: `crewstart_hku5tgh_test.go` is a
`*tmuxSubstrate.SpawnCrewSession` collision test (and performs a live `windowHandleExposer` assertion at
`:140`/`:145`); `hknddg1_crewqueue_provenance_test.go` tests `ExportedLoadQueueProvenance` +
`RunOrphanSweep`; `scenario_orphan_sweep_crew_pl006d_hkndq_test.go` is an orphan-sweep scenario. Trusting
the `crew*` prefix produces a broken move. *Mitigation:* §1's STAY table names all three explicitly, and
the freeze guard's sanctioned-file list is `-vx`-exact rather than a prefix glob.

**R7 — The seed-paste cluster is genuinely shared and must not be quietly duplicated.**
`injectAndVerifySeed`, `sendSubmitEnterWithRetry`, `splashDismissWait`, `bufferName`,
`errPaneCaptureUnsupported`, `pasteInjecter`, `enterSender`, plus the
`pasteVerify*`/`splashDismissDelay*`/`resumeSubmitRetry*` tunables serve four call sites
(implementer-initial, implementer-resume, reviewer, crew-init). `errPaneCaptureUnsupported` is a sentinel
compared with `errors.Is`, so duplicating it silently breaks the verify loop's fallback.
*Mitigation:* out of E2 scope entirely. If it is ever extracted to a stdlib+handler leaf, that is its own
sanctioned slice — E1 and E5 both want it — and its cost is real: the tunables are mutated by name from
daemon test files and `export_test.go`, so they must become exported setters.

**R8 — "Tidying" the disabled idle reaper silently re-enables a sweep the operator turned off.**
`CrewIdleReaper.StartWatcher` is a deliberate no-op (operator directive 2026-07-18; the FEATURE bead is
`hk-s2eac`, the DISABLE is tracked under `hk-98at0` and `hk-do173`), with `loop`/`scan`/`checkCrew`/`reap`
retained unreferenced behind `//nolint:unused`. It was disabled because it was killing live crews.
*Mitigation:* Step 6 makes carrying it verbatim an explicit instruction. Two regression tests in
`crewidlereap_hks2eac_test.go` — `TestCrewIdleReaper_StartWatcher_Disabled_NeverReaps` and
`TestCrewIdleReaper_StartWatcher_Disabled_NeverScans` — fail if the body is reverted, and they move with
the file, so a silent re-enable cannot land.

**R9 — `export_test.go` is the most rebase-fragile hunk in the PR.** Lines 2682–2740 sit in a shared,
high-contention file; a sibling `export_cjqyn_test.go` exists specifically to dodge shared-index races
with other crews editing it. *Mitigation:* Step 9 does that hunk last, and the PR rebases immediately
before pushing.

**R10 — `argvHasFlag` / `containsPair` become redeclaration errors under E2b.** Both are declared twice
today with identical signatures — `crewlaunchspec_test.go:36`/`:26` in `package daemon_test` and
`crewstart_hkmmlqt_test.go:404`/`:415` in `package daemon` — legal only because the packages differ. E2a
converts the first pair into `package crewrun` while the second stays in daemon: safe. The moment E2b
moves `crewstart_hkmmlqt_test.go` into `crewrun`, they collide. *Mitigation:* recorded in Step 8 and in
the E2b handoff; rename the hkmmlqt copies at that point, not now.

**R11 — Lint obligations make the diff non-mechanical in three specific places.** `revive` runs with
`{name: exported}` and `{name: package-comments}` (`.golangci.yml:85`); `nolintlint` requires
`require-explanation: true, require-specific: true` (`:87`). So: (a) `internal/crewrun` needs a package
doc directly above the package clause — `crewlaunchspec.go`'s existing header sits *below* it and does
not count; (b) the five renamed exports need doc comments starting with the **new** name; (c) the
`//nolint:gosec // G304` and `//nolint:unused // …` directives must carry their explanations across.
Recon's stutter worry is a non-issue by contrast: `revive`'s stutter check tests
`HasPrefix(name, pkgName)`, and `CrewHandler` does not start with `crewrun`. *Mitigation:* Steps 4–6 spell
all three out; gate step 7 runs the linter over the whole new package rather than only changed lines.

**R12 — The staged-but-uncommitted `gitprobe` / `harness/shared` work is your `.golangci.yml` anchor.**
The new `crewrun:` block is inserted immediately after the `gitprobe:` block, which exists only in the
staged `.golangci.yml`. A rebase that drops that work drops the anchor and your insertion lands in the
wrong place or conflicts. *Mitigation:* Step 0 verifies tree state before any edit.

**R13 — The daemon test suite is red at baseline; a naive "all green" gate is unreachable.**
`TestThroughput_TenBeadsAtMaxFour` fails identically at HEAD in isolation, and five more tests are
load-sensitive flakes (`00-test-oracle-baseline.md`). *Mitigation:* gate step 5 is differential
(`comm -13 before after` must be empty), gate step 4 is absolute for the new package only, and the
verification run is serialized.

---

## 8. Rollback

E2a is a self-contained PR on its own bead branch; abandoning it is a branch discard, not a surgery.

**Mid-flight, uncommitted:**

```bash
cd /Users/gb/github/harmonik
git checkout -- internal/daemon/crewstart.go internal/daemon/bootsocket.go \
  internal/daemon/bootstate.go internal/daemon/bootworkloop.go internal/daemon/socket.go \
  internal/daemon/socket_state.go internal/daemon/socket_dashboard.go \
  internal/daemon/socketdispatch.go internal/daemon/scheduletick.go \
  internal/daemon/branchreapwatcher.go internal/daemon/projectconfig.go \
  internal/daemon/export_test.go internal/sentinel/adversary.go \
  cmd/harmonik/captain.go cmd/harmonik/captain_respawn.go
git checkout -- .golangci.yml Makefile          # drops the crewrun depguard block + the guard hook
git rm -r --cached internal/crewrun 2>/dev/null || true
rm -rf internal/crewrun scripts/p2-freeze-guard.sh
git checkout -- internal/daemon/crewlaunchspec.go internal/daemon/crewidlereap.go \
  internal/daemon/crewlaunchspec_test.go internal/daemon/crewlaunchspec_twoproj_hk25bg_test.go \
  internal/daemon/crewidlereap_hks2eac_test.go
```

**Careful:** `git checkout -- .golangci.yml` restores the **staged** version — which is what you want,
because the staged version is the `gitprobe` work. It does **not** un-stage `internal/gitprobe/` or
`internal/harness/shared/`. Verify with Step 0's `git status --porcelain` before and after; if the
`gitprobe:` block is missing from `.golangci.yml` after a rollback, you have destroyed unrelated
uncommitted work — recover it from the index (`git checkout-index`) before doing anything else.

**Committed but unmerged:** `git branch -D <bead-branch>` after confirming nothing else was stacked on
it. Nothing in `internal/daemon` was rewritten in place beyond import qualification and two block
deletions, so there is no half-migrated state to unwind.

**Post-merge:** `git revert` the single merge commit. `internal/crewrun` disappears, the depguard block
and the freeze guard go with it, and `internal/daemon` is byte-identical to its pre-unit state — there is
no data migration, no on-disk format, and no runtime state involved. The one thing to re-check after a
revert is `Makefile:441-442`, since the guard hook line lives inside the `check-short:` recipe that other
work also edits.

**Partial rollback (keep the launchspec, drop the reaper, or vice versa) is not supported and not worth
building:** `crewidlereap.go:289` needs `CrewStopRequest` from `wire.go`, and `wire.go`'s `CrewHandler`
is what the socket layer imports. The four non-test files are one atomic unit.
