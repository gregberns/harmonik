# E5 chunk catalogue — the run-loop stream, cut small

**Written:** 2026-07-22, against the working tree at `b423081f` (RT13 **landed**: `379fca71`).
**Governs:** the chunk boundaries and per-chunk gates for **RT15, RT17, RT18, RT19 and the lift (RT20+)**.
**Does not govern:** RT14, RT16, RT19b and the staffing order — those are owned by
`RT14-dispatchsegment-conversion.md`, `RT16-emitterport-conversion.md`,
`RT19b-stranded-run-path-helpers.md` and `E5-STAFFING-SEQUENCE.md`. This file is consistent with them and
does not restate them.

Every count, line number and lint finding below was **re-measured in this tree**, not copied. Where a
decomposition and its adversarial challenge disagreed, §3 records which won and why.

> **Line numbers in `RT15-chunks.md` are stale by exactly +4** (it says `beadRunOne` is at `:3175`;
> it is at **`:3171`**. It says the copy-mutations are at `:3179/:3977/:3987`; they are at
> **`:3176/:3973/:3983`**). All line numbers in *this* file are current as of `b423081f`. **Locate by
> symbol name and grep anchor, never by line number** — the first chunk of any slice invalidates every
> number after it.

---

## 0. Why small, given E5 cannot be parallelized

E5's RT stream is single-writer by construction. Every slice from RT13 to RT20 edits
`internal/daemon/workloop.go` (**332 commits/90d**, one commit every ~6.5 wall-clock hours) and/or the
three mode files (`dot_cascade.go` 104, `reviewloop.go` 99, `dot_gate.go` 21) and/or `export_test.go`
(230). Two agents in `workloop.go` at once is the R3 merge-conflict failure `_plan.md` §0 names.
`E5-STAFFING-SEQUENCE.md` §2.1 already rules: one RT agent, one slice. **So smallness buys nothing in
throughput.** It buys three other things, and they are the whole justification. First, **conflict-window
length**: a chunk that holds the hottest file in the tree for one edit-plus-gate cycle rebases trivially
against whatever landed underneath it; a chunk that holds it for a day does not. Second,
**reviewability**: a 10-line single-token substitution is *visibly* behavior-preserving; a 200-line
threaded-parameter diff is an argument the reviewer has to accept on trust, and the pure-move rule
(`_plan.md` §5.1) makes "behavior-preserving" the only thing review is checking. Third,
**interruptibility**: this stream will be interrupted — by the operator, by a keeper restart, by a
higher-priority initiative — and the difference between "chunks 1–7 landed, tree coherent" and "half a
threaded `RunEnv`" is the difference between banking progress and owing a debt.

The honest cost is that smallness is **not free**: the daemon suite is ~605s and the oracle baseline
(`00-test-oracle-baseline.md` Addendum §4) mandates a serialized run in a clean detached worktree, so
each chunk carries a 30–45 minute gate. Seven chunks is 4–5 hours of gate time for ~190 lines of change,
and the *aggregate* exposure of `workloop.go` is seven windows rather than one longer one. That trade is
worth taking only because each individual window is trivially rebasable. It is not worth taking to the
point of one-line chunks — §1 merges two chunks the decompositions over-split for exactly this reason.

**The design rule, stated plainly: every chunk boundary is a state the tree can sit at indefinitely.**
Green build, green differential suite, one dependency-passing idiom on each side of exactly one boundary,
no half-populated bundle that a later reader can silently misuse. Any chunk that fails that test is not a
chunk — it is a stream wearing a chunk's clothes, and §3 names the four that were rejected on that ground.
This is what makes the E5 plan's §risks warning ("**Do not** abandon partway through RT15–RT18: a
half-threaded `RunEnv` leaves the tree in a worse state than not starting") obsolete for RT15/RT19 and
**still live for RT18 and the lift** — see §2 and §4.

---

## 0a. The three gates, defined once

Every green criterion in §1 is one of these plus a per-chunk assertion. **`golangci-lint run` bare is
never a gate** — it reports **5,076 findings** in `internal/daemon` alone (measured; `Makefile:626`
documents the same: "the release bar in the spec assumed a clean baseline that never existed").
The real gate is `--new-from-rev`.

```bash
# G1 — fast inner loop, seconds. Run after every edit.
go build ./internal/... ./cmd/... && go vet ./internal/... && go vet -tags=scenario ./internal/daemon/

# G2 — per-chunk commit gate. ~5 min. Note: `make check-fast` derives its test set from
# `git diff --name-only HEAD`, which is EMPTY post-commit — so pin the package explicitly.
go build ./internal/... ./cmd/... && go vet ./internal/... && \
go vet -tags=scenario ./internal/daemon/ && go vet -tags=e2e_real_claude ./internal/daemon/ && \
go vet -tags=integration ./internal/daemon/ && \
./.tools/golangci-lint run --new-from-rev=HEAD~1 && \
go test -short ./internal/daemon/

# G3 — differential oracle. ~45 min, SERIALIZED, in a clean detached worktree at your own HEAD.
# Never in the shared tree (it compiles other agents' dirty files). Check `df -h` > 10 GiB first.
go test ./internal/runexectest/... -count=10 && go test ./internal/replay/... -count=1 && \
go test -tags specaudit ./internal/specaudit/... -count=1 && \
go test ./internal/daemon/... ./internal/harness/... -count=1 -timeout 25m 2>&1 \
  | grep -E '^--- FAIL' | sort -u > after-failures.txt
comm -13 before-failures.txt after-failures.txt    # MUST be empty
```

**G3 cadence:** required on every chunk that changes a signature, deletes a nil-default, moves a package,
or edits a test file. Optional (G2 suffices) on pure single-token substitutions inside one function that
change no signature — those are compile-enforced and `runexectest -count=10` cannot see them.

**The `--new-from-rev` trap that both decompositions missed.** `funlen`/`cyclop`/`gocognit` are
grandfathered ONLY by `--new-from-rev`, and they anchor on the **function declaration line**. Measured:

```
internal/daemon/workloop.go:3171:1: cognitive complexity 389 of func `beadRunOne` is high (> 20) (gocognit)
internal/daemon/reviewloop.go:185:1: cognitive complexity 332 of func `runReviewLoop` is high (> 20) (gocognit)
internal/daemon/dot_cascade.go:189:1: cognitive complexity 196 of func `driveDotWorkflow` is high (> 20) (gocognit)
```

**None of the three carries a `//nolint`** (the RT15 challenge asserted `beadRunOne` does at `:3174`;
verified FALSE — the only `//nolint`s in `workloop.go` are at `:3253`, `:3273`, `:4747`). So **any chunk
that edits one of those declaration lines resurrects a grandfathered finding and fails G2**. Such a chunk
must add `//nolint:gocognit,cyclop,funlen // <bead ref>: pre-existing complexity, unchanged by this
signature edit` in the same commit — and `nolintlint` runs with `require-explanation: true,
require-specific: true, allow-unused: false`, so the directive must be specific, justified, and still
earning its keep. This applies to **RT15.5, RT18.5, RT18.6, RT18.7** and **LIFT.13**.

**`hugeParam` and `rangeValCopy` are disabled** (`.golangci.yml:86`). That is the only reason a 19-field
`RunEnv` and a 5-field `SharedHandles` travel by value lint-clean with no `nolint`. Load-bearing and
undocumented in both decompositions.

---

## 1. The chunk table — the payload

Execution order top to bottom. `Blocked on` is a **hard** gate, not a preference.

### RT15 — build `RunEnv`/`SharedHandles`, re-signature `beadRunOne`

Blocked on: **RT14** (restructures `workloop.go:4900–4956`, inside RT15's edit span) and **RT16**
(converts 24 `deps.bus` sites inside `beadRunOne`; doing RT15's signature first makes RT16 a 108-site
edit inside a signature change). Not blocked on the `RunEnv.ProjectCfg` / `SharedHandles.RunRegistry`
questions — verified: both bundles stay in `package daemon` for all of RT15, so `ProjectCfg
ProjectConfig` and `RunRegistry *RunRegistry` type-check with zero work. Those block **the lift**.

| # | Chunk | Slice | Files | ~LOC | Green criterion | Safe to stop | Conflict window |
|---|---|---|---|---|---|---|---|
| **RT15.0** | Hoist `itemWorkflowRef = resolveWorkflowRef(beadRecord, itemWorkflowRef)` from `workloop.go:3308` to immediately after the clock default at `:3178`. **The prerequisite that disarms the slice's only silent-break hazard** | RT15 | `workloop.go` | 2 | G1 + G3 + `test "$(grep -c 'itemWorkflowRef = resolveWorkflowRef' internal/daemon/workloop.go)" = 1` | **yes** | `workloop.go`, 2 lines, <20 min |
| **RT15.1** | `func (deps *workLoopDeps) runEnv(...) RunEnv` beside `runPorts()` (`runports.go:336`), populating **all 19 fields**; `env := deps.runEnv(...)` after `rp := deps.runPorts()` at `:3180`; convert the 2-site tail group (`workflowModeDefault`, `allowedRepos`) | RT15 | `runports.go`, `workloop.go` | 35 | G2 + `test -z "$(git diff --name-only HEAD~1 -- . ':!internal/daemon/workloop.go' ':!internal/daemon/runports.go')"` | **yes** | `workloop.go` ~1h (gate-bound); `runports.go` is **7 commits/90d**, effectively cold |
| **RT15.2** | `deps.projectDir` → `env.ProjectDir`, **branching cluster**: `:3284, 3413, 3419, 3421, 3432, 3442, 3481` (7 code sites) | RT15 | `workloop.go` | 14 | G2 + `test -z "$(git diff --name-only HEAD~1 -- . ':!internal/daemon/workloop.go')"` | **yes** | `workloop.go` only, ~1h |
| **RT15.3** | `deps.projectDir` → `env.ProjectDir`, **tail**: `:3641, 3741, 3814, 3876, 3877, 3948, 4122, 4127, 4285, 4427, 4450, 4514, 4534` (13 code sites) | RT15 | `workloop.go` | 26 | same as RT15.2 | **yes** | `workloop.go` only, ~1h |
| **RT15.4** | `targetBranch` (3) + `protectBranches` (2) + `defaultHarness` (2) + `projectCfg` (3) — merged, all pure single-token, no ordering relation | RT15 | `workloop.go` | 20 | same as RT15.2, **plus** `[ "$(git diff -U0 HEAD~1 -- internal/daemon/workloop.go \| grep -c '^+.*deps\.launchSpecBuilder')" = 0 ]` | **yes** | `workloop.go` only, ~1h. Brackets the `launchSpecBuilder` block RT17 rewrites — land before RT17 |
| **RT15.5** | **THE SIGNATURE.** `beadRunOne(ctx, deps, env RunEnv, extraContext, preSelectedWorker, localSlotHeld)` — 11 item params collapse into `env`; 6-line alias preamble replaces the `env :=` line; **add the `//nolint:gocognit,cyclop,funlen`**; move construction into the goroutine body at `:3126` (do **not** collapse the 14-param closure at `:3120` — loop-variable-capture guard). 7 call sites: `workloop.go:3126` + `hk3hozm_slot_leak_test.go:193`, `pi_provider_selected_hk8ziid2_test.go:173/245/306`, `pi_unknown_profile_refuse_test.go:165`, `workloop_gate_n5md3_test.go:351` | RT15 | `workloop.go` + 4 test files | 45 | G2 + G3 + `test -z "$(git diff --name-only HEAD~1 -- internal/daemon/export_test.go)"` | **yes** | `workloop.go` ~1.5h + 4 low-churn test files. **`export_test.go` (230/90d) held for ZERO minutes — zero diff is an exit gate** |
| **RT15.6** | `func (deps *workLoopDeps) sharedHandles() SharedHandles` (all 5 fields at once); `shared := deps.sharedHandles()`; convert `localInFlight` (4) + `agentSpawnSem` (3) + `budgetPort` (1) | RT15 | `runports.go`, `workloop.go` | 25 | G2 + `test -z "$(git diff --name-only HEAD~1 -- . ':!internal/daemon/workloop.go' ':!internal/daemon/runports.go')"` | **yes** | Detachable tail — independent of RT15.0–.5, deferrable indefinitely |
| **RT15.7** | `runRegistry` (6) → `shared.RunRegistry` | RT15 | `workloop.go` | 12 | G2 + assertion anchored on the func decl, **not** a hardcoded `awk NR` range | **yes** | `workloop.go` only, ~1h |
| **RT15.8** | `workerRegistry` (8) → `shared.Workers` | RT15 | `workloop.go` | 16 | same as RT15.7 | **yes** | `workloop.go` only, ~1h. Last chunk; nothing waits on it |

**RT15 creates no package** — so it is exempt from the whole P2 landing ceremony: no depguard block, no
freeze-gate script, no `specaudit` path-allowlist edit, no `go list -deps` boundary test. That exemption
is the only reason nine small chunks is affordable.

### RT17 — `LaunchPort` part 2 (hookStore / timeouts / sandbox / delivery)

Blocked on: **RT16** (the `emit := deps.emitterPort()` derived-local idiom that RT17 mirrors, and the
`deps.bus` drain that RT17's delivery half depends on) and **RT14** (RT14 rewrites `workloop.go:4900`
and `dot_gate.go:475/:493` into `&dispatchSegment{...}` literals — the exact lines RT17.3/RT17.5 touch).

**The structural correction that reshapes this slice:** RT17 must **not** add a `ports RunPorts`
parameter to `driveDotWorkflow` / `dispatchDotAgenticNode` / `dispatchDotGateNode` / `runReviewLoop`.
That is RT18's scope and it is the second dependency-passing idiom the whole catalogue exists to prevent.
RT17 uses a **derived local** (`launch := deps.launchPortFull()`), exactly as `RT15-chunks.md` does for
`env` and `RT16-emitterport-conversion.md` §1a does for `emit`.

| # | Chunk | Slice | Files | ~LOC | Green criterion | Safe to stop | Conflict window |
|---|---|---|---|---|---|---|---|
| **RT17.0** | **The chunk that dissolves most of the claimed atomicity.** Add `func (deps *workLoopDeps) launchPortFull() LaunchPort` in `runports.go` — populates `Launch` NON-NIL by resolving the routed builder (`routedLaunchSpecBuilder` / `claude.BuildLaunchSpec` fallback / pre-injected test builder) exactly as `workloop.go:3971–3989` does today. **Add the `"time"` and `"encoding/json"` imports** (`runports.go` has neither — verified). Widen `LaunchPort` with `CodexNoWorkFloor() time.Duration` and convert its 4 sites (`dot_cascade.go:2034/:2035`, `workloop.go:5151/:5152`) so `unused` cannot fire. Pilot chosen because `deps.codexNoWorkDurationFloor` is **assigned nowhere in the module** and referenced by zero test files | RT17 | `runports.go`, `dot_cascade.go`, `workloop.go` | 45 | G2 + G3 + `! grep -q 'deps\.codexNoWorkDurationFloor' internal/daemon/dot_cascade.go internal/daemon/workloop.go` | **yes** | `runports.go` cold; `dot_cascade.go` 2 lines; `workloop.go` 2 lines. <30 min |
| **RT17.1** | Add `PostReadyHangTimeout()` to `LaunchPort` + `daemonLaunch` (method reads the field ⇒ self-satisfying, no `unused`) | RT17 | `runports.go` | 10 | G2 | **yes** | cold file only |
| **RT17.2** | Convert the 3 `deps.postAgentReadyHangTimeout` readers — the only 3 in the tree: `reviewloop.go:682, 826, 828` | RT17 | `reviewloop.go` | 4 | G2 + `! grep -q 'deps\.postAgentReadyHangTimeout' internal/daemon/reviewloop.go` | **yes** | `reviewloop.go` only, ~15 min |
| **RT17.3** | Add `ReadyTimeout()` **and** `RemoteReadyTimeout()` **in one commit** — irreducible, see §2 | RT17 | `runports.go` | 14 | G2 | **yes** | cold file only |
| **RT17.4** | Apply the pair in `reviewloop.go`: `:587`, `:750`, `:1368` | RT17 | `reviewloop.go` | 3 | G2 + G3 + `! grep -q 'deps\.agentReadyTimeout\|deps\.remoteAgentReadyTimeout' internal/daemon/reviewloop.go` | **yes** | `reviewloop.go` only, ~15 min |
| **RT17.5** | Apply the pair in `dot_cascade.go` (`:1772`, `:1880`) and `dot_gate.go` (`:475`, `:493`). **SEQUENCE AFTER RT14** — RT14 rewrites `dot_gate.go:475/:493` | RT17 | `dot_cascade.go`, `dot_gate.go` | 4 | same as RT17.4, scoped to those two files | **yes** | 2 files, ~20 min |
| **RT17.6** | Apply the pair in `beadRunOne` (`workloop.go:4900`, `:4940`). **SEQUENCE AFTER RT14.** Isolated as its own 2-line commit so the hottest file is held for minutes | RT17 | `workloop.go` | 2 | same, scoped to `workloop.go` | **yes** | `workloop.go`, 2 lines, <20 min |
| **RT17.7** | Add `RegisterHookSession` / `CloseHookSession` / `SetAgentReadyCallback` to `LaunchPort` + `daemonLaunch` with **nil-absorbing** semantics (`if l.hooks == nil { return }`) — that is what makes deleting the caller-side guard byte-identical | RT17 | `runports.go` | 24 | G2 | **yes** | cold file only |
| **RT17.8** | Apply Register+Close in `reviewloop.go`, deleting each guard: `:390-391`, `:611-612`, `:745-746`, `:814-815`, `:1318-1319`, `:1386-1387`, `:1478-1479`, `:1523-1524` | RT17 | `reviewloop.go` | 16 | G2 + G3 | **yes** | `reviewloop.go` only, ~30 min |
| **RT17.9** | Apply Register+Close in `dot_cascade.go`: `:1637-1638`, `:1795-1796`, `:1875-1876`, `:1944-1945` | RT17 | `dot_cascade.go` | 8 | G2 + G3 | **yes** | `dot_cascade.go` only, ~20 min |
| **RT17.10** | Apply Register+Close in `dot_gate.go`: `:402-403`, `:419-420`, `:490-491`, `:511-512` | RT17 | `dot_gate.go` | 8 | G2 + G3 | **yes** | `dot_gate.go` only (21/90d, coldest mode file), ~20 min |
| **RT17.11** | Apply `SetAgentReadyCallback` — its own chunk because deleting the guard **hoists local assignments** out of the conditional (`reviewloop.go:655-656` `capturedImplTap := implTap` etc.; also `:1414-1415`, `dot_cascade.go:1850-1851`, `dot_gate.go:451`). Side-effect-free copies, but the one non-mechanical diff in the family | RT17 | `reviewloop.go`, `dot_cascade.go`, `dot_gate.go` | 20 | G2 + G3 | **yes** | 3 files, ~30 min. Zero `workloop.go` |
| **RT17.12** | Add `LatestOutcome` / `WaitForOutcome` as **pass-throughs, NOT nil-absorbing** (every `waitWithSocketGrace` arg-site is unguarded today; a nil store must keep panicking identically). Add `var _ hookStoreIface = daemonLaunch{}` — with all 5 methods present, `ports.Launch` is directly assignable to the existing `store hookStoreIface` parameter, so **`waitsocketgrace.go` needs no edit**. Convert the 3 sub-driver arg-sites: `reviewloop.go:790`, `:1511`; `dot_cascade.go:1922`; `dot_gate.go:504`. `workloop.go:5040` keeps `deps.hookStore` and still compiles | RT17 | `runports.go`, `reviewloop.go`, `dot_cascade.go`, `dot_gate.go` | 22 | G2 + G3 + `git diff --quiet HEAD~1 -- internal/daemon/waitsocketgrace.go` | **yes** | 3 sub-driver files ~25 min. Zero `workloop.go` |
| **RT17.13** | Extract `func (cfg SandboxConfig) networkCacheProfile() SandboxProfileInput` — the 5 config-derived fields, deduplicating `dot_cascade.go:1536-1543` against `workloop.go:4517-4521`. Independently unit-testable | RT17 | `sandboxgate.go` | 25 | G2 | **yes** | cold file only |
| **RT17.14** | Add `SandboxSpawn(agentType core.AgentType, in SandboxProfileInput) *SrtSpawnConfig` — **note the return type: `*SrtSpawnConfig` (`sandboxgate.go:56`), NOT `*sandboxSpawn`** (that is a local variable name). Apply to the `dot_cascade.go:1531-1543` literal | RT17 | `runports.go`, `dot_cascade.go` | 30 | G2 + G3 + `! grep -q 'deps\.sandboxCfg' internal/daemon/dot_cascade.go` | **yes** | `dot_cascade.go`, one 13-line literal, ~30 min |
| **RT17.15** | Apply to the near-duplicate literal in `beadRunOne` (`workloop.go:4512-4521`). Split from RT17.14 solely to cap the `workloop.go` hold at one commit | RT17 | `workloop.go` | 12 | same, scoped to `workloop.go` | **yes** | `workloop.go`, one 10-line literal, ~20 min |
| **RT17.16** | **PURE MOVE, no port surface.** Relocate `pasteInjectCognitionGate` (`dot_gate.go:671-738`) and `pasteInjectQuitOnGateFile` (`:740-800`) into `pasteinject.go` beside their 7 consumed symbols. **Not byte-identical:** `dot_gate.go:39` imports tmux aliased as `ltmux`, `pasteinject.go:49` imports it bare — the `runner ltmux.CommandRunner` parameter must be rewritten to `tmux.CommandRunner` on arrival. Call sites at `dot_gate.go:499/:501` keep the same unqualified names (same package). **A hard blocker for ever lifting `dot_gate.go`** — landing it early banks the liftability win independent of the rest of delivery | RT17 | `dot_gate.go`, `pasteinject.go` | 132 | G2 + G3 + `test $(grep -c 'func pasteInjectCognitionGate\|func pasteInjectQuitOnGateFile' internal/daemon/dot_gate.go) -eq 0` | **yes** | 2 files, ~40 min. Zero `workloop.go` |
| **RT17.17** | **BLOCKED on RT16's EmitterPort landing.** Route the bus-carrying delivery functions behind `LaunchPort`: `pasteInjectOnLaunch` (`workloop.go`, `reviewloop.go:703/:1435`, `dot_cascade.go:1720`), `pasteInjectQuitOnCommit` (`workloop.go`, `reviewloop.go:722`, `dot_cascade.go:1759`), `pasteInjectCognitionGate` (`dot_gate.go:499`, post-RT17.16). Split into 3 per-function sub-commits when unblocked | RT17 | `runports.go` + 4 run-path files | 40 | G2 + G3 | **yes** | Widest test surface in RT17 — 57 daemon test files reference `pasteInject`, 1 scenario-tagged |

**RT17's exit gate is per-concern, not per-file.** The E5 plan's `grep -c 'deps\.'
{reviewloop,dot_cascade,dot_gate}.go → 0 0 0` is **arithmetically unreachable by RT17's own scope**:
measured today 137 / 88 / 41 = 266, of which RT17's four concerns cover roughly 66 (25%). RT16 owns ~174,
RT15 ~15, RT18 ~24. Use the per-chunk greps above.

### RT18 — drop `deps workLoopDeps` from the run-path signatures

Blocked on: **RT15.5** (`beadRunOne` must already take `env`), **RT16** (the `deps.bus` drain), **RT17**
(the `LaunchPort` widening), **RT19b** (the third-band helper drain). This ordering is not a preference —
see §3, where RT18's original decomposition is rejected precisely for trying to do RT16/RT17's work.

**What RT18 actually is, after rework:** a `clockOrSystem()` accessor that kills the copy-mutation idiom
with **zero signature changes**, then six small signature drops in a strict **leaf-first** walk, then the
`launchSpecBuilder` deletion. The read conversions belong to RT16/RT17 and are not RT18's.

| # | Chunk | Slice | Files | ~LOC | Green criterion | Safe to stop | Conflict window |
|---|---|---|---|---|---|---|---|
| **RT18.0** | Delete the **proven-dead** clock default in `dispatchDotAgenticNode` (`dot_cascade.go:1259-1261`). Dead-by-fixture, not dead-by-construction: both prod callers (`dot_cascade.go:900`, `sub_workflow_runner.go:323`) sit downstream of `driveDotWorkflow`'s own default at `:220`, and `swMakeRunner` (`sub_workflow_runner_hkoe6_test.go:118`) builds a nil-clock `workLoopDeps` but every fixture there uses a single non-agentic node. **Say so in the commit body** | RT18 | `dot_cascade.go` | 4 | G2 + G3 | **yes** | `dot_cascade.go`, 4 lines, <20 min |
| **RT18.1** | Add `func (deps *workLoopDeps) clockOrSystem() substrate.ClockPort` (non-mutating). Convert the ~27 `deps.clock` reads **file by file, no signature change**: `dot_cascade.go` (12), `reviewloop.go` (11), `workloop.go` (10), `runbridge.go` (2). **Split per file — 4 independent chunks, each green.** This is the move that takes the copy-mutation idiom off the critical path of every signature drop | RT18 | one run-path file per chunk | ~10 ea | G2 + G3 | **yes** | one file each, ~20 min each |
| **RT18.2** | Delete the 4 now-redundant `deps.clock = substrate.SystemClock{}` blocks: `workloop.go:3176`, `dot_cascade.go:220`, `reviewloop.go:230` (+ RT18.0's already gone). **Trivial commit, no test-file edits, no runtime nil-deref risk** — the whole point of RT18.1 | RT18 | 3 run-path files | 12 | G2 + G3 + `test "$(grep -c 'deps\.clock = ' internal/daemon/*.go \| grep -vc ':0$')" = 0` | **yes** | 3 files, ~20 min |
| **RT18.3** | `executeCognitionGate` + `buildCognitionGateEval` (`dot_gate.go:203/:237`) → `(env RunEnv, ports RunPorts)`. **One atom** — `buildCognitionGateEval` has zero `deps` reads of its own; its entire job is to capture `deps` in a returned `handler.GateEvalFunc` and forward it. Splitting them means the closure still captures `deps` (zero progress). Also handles `dot_gate.go:341-360`, where the gate **swaps** the builder (`pinnedHarnessLaunchSpecBuilder`) rather than reading it — RT17.0's `launchPortFull()` must expose that swap or this chunk escalates | RT18 | `dot_gate.go`, `export_test.go` | 60 | G2 + G3 | **yes** | `dot_gate.go` + `export_test.go`. `ExportedExecuteCognitionGate` keeps its exported signature ⇒ consuming test files see zero diff |
| **RT18.4** | `dispatchDotGateNode` (`dot_gate.go:74`) → `(env, ports)`. 2 `deps` reads. 2 prod call sites (`dot_cascade.go:1019`, `sub_workflow_runner.go:351`), **zero** test callers. Must follow RT18.3 (`dot_gate.go:224` forwards into `buildCognitionGateEval`) | RT18 | `dot_gate.go`, `dot_cascade.go`, `sub_workflow_runner.go` | 15 | G2 + G3 | **yes** | 3 files, 1–2 lines each outside `dot_gate.go`. Zero `workloop.go` |
| **RT18.5** | `dispatchDotAgenticNode` (`dot_cascade.go:1225`) → `(env, ports)`. **+ the `//nolint` for gocognit 196 on the decl line.** Callers `dot_cascade.go:900`, `sub_workflow_runner.go:323`. Zero test callers | RT18 | `dot_cascade.go`, `sub_workflow_runner.go` | 20 | G2 + G3 | **yes** | 2 files, ~25 min. Zero `workloop.go` |
| **RT18.6** | `dotSubWorkflowRunner` ctor param (`sub_workflow_runner.go:44`) + struct field (`:111`) → `env` + `ports`; nested-runner copy at `:249`. Must follow RT18.4 **and** RT18.5 (it forwards into both). 1 prod call site (`dot_cascade.go:1039`), 1 test call site (`sub_workflow_runner_hkoe6_test.go:127`) | RT18 | `sub_workflow_runner.go`, `dot_cascade.go`, + 1 test | 30 | G2 + G3 | **yes** | ~30 min. Zero `workloop.go` |
| **RT18.7** | `driveDotWorkflow` (`dot_cascade.go:189`) → `(env, ports)`. **+ the `//nolint`.** The 4 `export_test.go` shims (`ExportedDriveDotWorkflow`, `…Full`, `…WithRunner`, `…WithModelEffort`) absorb the change in their bodies; their **exported signatures are preserved**, so 34 consuming test files see zero diff. Prod call site `workloop.go:4182` = 1 line | RT18 | `dot_cascade.go`, `export_test.go`, `workloop.go` (1 line) | 40 | G2 + G3 | **yes** | `dot_cascade.go` + `export_test.go` ~60 min; `workloop.go` ONE line |
| **RT18.8** | `runReviewLoop` (`reviewloop.go:185`) → `(env, ports)`. **+ the `//nolint` for gocognit 332.** Verified: `runReviewLoop` reads **zero** `SharedHandles`-family fields, so it takes `(env, ports)` only — `unparam` is enabled and an always-unused bundle param fails G2. 2 shims (`ExportedRunReviewLoop`, `…WithRunner`) absorb it; 31 test files see zero diff. Prod call site `workloop.go:4025` = 1 line | RT18 | `reviewloop.go`, `export_test.go`, `workloop.go` (1 line) | 30 | G2 + G3 | **yes** | `reviewloop.go` + `export_test.go` ~40 min; `workloop.go` ONE line |
| **RT18.9** | `runBridge`: BOTH the struct field (`runbridge.go:32`) and the `newRunBridge(deps workLoopDeps, rp RunPorts, …)` param (`:76`) — the E5 plan names only the field. **BLOCKED — operator escalation on `deps.tidGen`** (see §2). Note `deps.clock` is read at `:84` inside `newRunBridge` itself, not only via `b.deps.clock` | RT18 | `runbridge.go`, `runports.go`, `workloop.go` (1 line) | 25 | G2 + G3 | **yes** | ~30 min; `workloop.go` ONE line |
| **RT18.10** | `beadRunOne` (`workloop.go:3171`) drops `deps workLoopDeps` → `(env RunEnv, ports RunPorts, shared SharedHandles)` — **the only function on the run path that genuinely reads all three**. **+ the `//nolint` for gocognit 389.** 7 call sites (1 prod + 6 in-package test in 4 files); there is **no `ExportedBeadRunOne` shim**, so those 4 test files edit directly and are **atomic** with the deletion. Hard prereq: RT18.9 (`beadRunOne` constructs the bridge at `:3321`) | RT18 | `workloop.go` + 4 test files | 30 | G2 + G3 | **yes** | Small diff but must not race another `workloop.go` writer — take it immediately after RT18.9 |
| **RT18.11** | **DELETE THE `launchSpecBuilder` SMUGGLE — the chunk that makes RT18 non-cosmetic.** Delete **both** assignments (`workloop.go:3973` routed, `:3983` `claude.BuildLaunchSpec` — the E5 plan cites "3967-3979" and straddles only the first) plus the now-redundant `rp.Launch = launchPort(deps.launchSpecBuilder)` at `:3990`. The resolution already lives in RT17.0's `launchPortFull()`. **Preserve the test-injection carve-out**: the constructor returns the pre-injected builder when non-nil, or many fixtures silently stop being consulted with no compiler check. **REPORTING RULE: RT18 is NOT done until this lands.** Stopping at RT18.10 leaves a coherent, green, indefinitely-safe tree — but it is converted signatures with the smuggle still live, which E5 §7.2 calls cosmetic. Report that state as **RT18 INCOMPLETE** | RT18 | `workloop.go`, `runports.go` | 35 | G2 + G3 + `test "$(grep -c 'deps\.launchSpecBuilder = ' internal/daemon/workloop.go)" = 0` | **yes, but report INCOMPLETE if you stop before it** | `workloop.go` ~20 lines around `:3971-3990`, ~45 min |

### RT19 — split `export_test.go`

Blocked on: **nothing in the RT stream.** Every chunk is a same-package `*_test.go` relocation:
`export_test.go` is `package daemon` (verified, line 1), so all 359 `package daemon_test` files keep
resolving `daemon.ExportedX` byte-identically after every chunk. **Zero production bytes change. Zero
minutes held in `workloop.go`.** The E5 "do not abandon partway" warning does not apply here at all.

Two standing rules for the whole slice: **(a) locate every declaration by symbol name — the line numbers
below are HEAD-relative and invalid after RT19.0.** **(b) `import` block churn (lines 12–41) is the most
conflict-prone region; expect two fixpoint rounds of pruning per chunk.**

| # | Chunk | Slice | Files | ~LOC | Green criterion | Safe to stop | Conflict window |
|---|---|---|---|---|---|---|---|
| **RT19.0** | **THE PREREQUISITE.** Fix-or-`//nolint` the **10 pre-existing lint findings** in `export_test.go`, **in place, before any decl moves**. Measured today: `:223` containedctx, `:425` ineffassign, `:762`/`:1203`/`:1248`/`:2553` gocritic unnamedResult, `:1317` errcheck (`w.observe`), `:1660` tooManyResultsChecker, `:1892` importShadow of `substrate`, `:2160` paramTypeCombine. They pass CI today only because `--new-from-rev` grandfathers them; **a byte-identical move into a new file un-grandfathers all of them and fails G2.** Without this chunk, 8 of the 20 below are unlandable | RT19 | `export_test.go` | ~12 | G2 | **yes** | `export_test.go` ~30 min. Converts 8 blocked chunks into landable ones |
| **RT19.1** | → `export_workloopdeps_test.go`: `WorkLoopDepsParams`, `ExportedWorkLoopDeps`, `ExportedWorkLoopDepsPtr`, `WorkLoopDepsWithProjectCfg`, `ExportedNewWorkLoopDepsWithStore`. **First, always** — these 5 decls are RT15's entire future edit surface (**157 files** reference them), so isolating them means RT15 later edits a ~560-line file, not a 2,895-line one | RT19 | `export_test.go` + new | 565 | G2 + `test -z "$(git show --pretty= --name-only HEAD -- internal cmd \| grep -v '_test\.go$')"` | **yes** | `export_test.go` ~30–45 min |
| **RT19.2** | → `export_reviewloop_test.go`: the 14 `reviewloop.go` + `reviewerharness_hkiv748.go` shims. Lift target | RT19 | ″ | 185 | ″ | **yes** | ″ |
| **RT19.3** | → `export_dotrun_test.go`: the 9 `dot_cascade.go` + `dot_gate.go` shims. Lift target | RT19 | ″ | 180 | ″ | **yes** | ″ |
| **RT19.4** | → `export_readywait_test.go`: the 13 `agentready.go` / `postreadyhang.go` / `waitsocketgrace.go` / `workloopeventsource.go` shims. **Coordinate with RT14**, which deletes 2 of them | RT19 | ″ | 130 | ″ | **yes** | ″ |
| **RT19.5** | → `export_pasteinject_test.go`: the 12 delivery shims | RT19 | ″ | 200 | ″ | **yes** | ″ |
| **RT19.6** | → `export_pasteinject_timeouts_test.go`: the 19 watchdog/timeout knobs. **HAZARD, see §2:** several are re-exported **by pointer** (`ExportedBriefDeliveredTimeout = &briefDeliveredTimeout` etc.). Keep the pointer re-export; never convert to a value alias | RT19 | ″ | 180 | ″ | **yes** | ″ |
| **RT19.7** | → `export_substrate_test.go`: the 11 `tmuxsubstrate.go` / per-run-substrate shims | RT19 | ″ | 115 | ″ | **yes** | ″ |
| **RT19.8** | → `fakes_tmuxadapter_test.go`: `noopTmuxAdapter` + its 15 methods + the `var _ tmuxPkg.Adapter` assertion. **Not irreducible** (interface satisfaction is package-level, proven by experiment) but ship whole — the type and its method set always change together | RT19 | ″ | 45 | ″ | **yes** | ″ |
| **RT19.9** | → `export_projectconfig_test.go`: the contiguous type-alias run + `ExportedProjectCfgOf`. **Coordinate with PC** (the `internal/projectconfig` lift) | RT19 | ″ | 120 | ″ | **yes** | ″ |
| **RT19.10** | → `fakes_launchbuilders_test.go`: the 5 capture/minimal launch-spec test builders. Test fixtures, not shims — the real content of a future `internal/runlooptest` | RT19 | ″ | 110 | ″ | **yes** | ″ |
| **RT19.11** | → `export_launchrouting_test.go`: the 8 `harnessregistry.go` routed/pinned/process-exit builders. **These are the shims RT18.11 re-points** — isolating them shrinks that diff | RT19 | ″ | 145 | ″ | **yes** | ″ |
| **RT19.12** | → `export_branching_test.go`: the contiguous 11-decl `branching.go` run | RT19 | ″ | 95 | ″ | **yes** | ″ |
| **RT19.13** | → `export_hookstore_test.go`: the 10 `hookrelay_chb025.go` shims. **Coordinate with RT17.12** | RT19 | ″ | 90 | ″ | **yes** | ″ |
| **RT19.14** | → `export_meters_pause_test.go`: the 15 spend-meter + pause-controller shims | RT19 | ″ | 150 | ″ | **yes** | ″ |
| **RT19.15a/b/c** | → `export_extpkg_claude_codex_pi_test.go` / `…_queuewiring_test.go` / `…_runmerge_lifecycle_test.go`: the 18 thin `= pkg.Symbol` re-exports of already-extracted packages, **split per source package** so RT19.19's delete-outright decision maps 1:1 onto files. **Verified: 6 of the 7 freeze-gate scripts exclude `*_test.go` on both find and grep; `scripts/transport-freeze-gate.sh:28-30` does NOT filter its find** (harmless — none of these filenames matches its `*reversetunnel*`/`*codesync*` patterns — but the "all seven exclude tests" claim is false and must not be relied on again) | RT19 | ″ | 180 | ″ | **yes** | ″ |
| **RT19.16** | → `export_resolvers_test.go`: the 14 `moderesolve` / `harnessresolve` / `modelpreference` / `pi_profile_resolve` / `standardgraph` shims | RT19 | ″ | 135 | ″ | **yes** | ″ |
| **RT19.17** | → `export_maintenance_test.go`: the 12 maintenance-loop shims | RT19 | ″ | 125 | ″ | **yes** | ″ |
| **RT19.18** | → `export_runregistry_test.go`: the 11 run-registry / run-wait / event-tap shims (incl. `noopExportedEmitter` + its methods) | RT19 | ″ | 140 | ″ | **yes** | ″ |
| **RT19.19** | → `export_sandbox_session_test.go`: the 16 sandbox / session-context / bead-guard shims | RT19 | ″ | 160 | ″ | **yes** | ″ |
| **RT19.20** | **RESIDUE SWEEP AND DELETE.** Everything left → `export_workloop_test.go`, then `git rm internal/daemon/export_test.go`. **Deleting the file to zero IS the completeness check.** Residue includes `ExportedRunAutoStatusInspection` (`:844`) — the one decl of 257 that neither decomposition assigned | RT19 | `export_test.go` (DELETED) + new | 165 | G2 + `test ! -e internal/daemon/export_test.go` + the tagged vets | **yes** | After this the 230-commit/90d conflict surface **no longer exists** |

**Deferred, not scheduled:** promoting `stubEventCollector` (300 refs / 145 files) or
`NewSealedAdapterRegistryForTest` (206 refs / 112 files) to `internal/testhelpers`. Re-measured:
`workloop_test.go` is **19 commits/90d — cold**, so the conflict-surface argument for these evaporates.
The `daemon:` depguard block already allows `internal/` wholesale, so nothing forces them. Both would
take a tree-wide test-file lock for 1–2 hours. Do not schedule without a concrete external consumer.

### LIFT (RT20+) — see §4 for entry criteria and abort

Chunk IDs LIFT.0 … LIFT.13 are enumerated in §4. They are **not** listed here because none of them may
begin until every RT chunk above has landed and §4's entry criteria all hold.

---

## 2. What is irreducible, and why

Six things. Each has a language- or call-graph-level cause, not a size argument.

**1. `beadRunOne`'s parameter change is one atom — and so is its extraction.** The function spans
`workloop.go:3171–5399` — **2,229 lines, a single top-level function** (next `^func ` is
`isWatcherErrCanceled` at `:5401`). Go rejects a call-arity mismatch, so RT15.5's signature change must
update all 7 call sites in the same commit. The only way to split it is "add `env` as a 12th parameter,
then delete the 11" — rejected, because step 1 leaves every value reachable through two parameters at
once (the exact two-idiom state this catalogue prevents), doubles call-site churn in a 332-commit/90d
file, and buys zero interruption safety since Go arity already makes the one-step version
compile-enforced. **Sized honestly: RT15.5 is ~45 lines because of the 6-line alias preamble** — the 79
`runID` reads, 19 `beadRecord` reads and the `itemWorkflowRef` handling stay untouched. Without the
preamble it is ~200 lines. RT18.10 (dropping `deps`) is ~30 lines for the same reason. **The extraction
of the body (LIFT.13) is a different matter — see §4.**

**2. An unused local is a compile error; an unused method is a lint error.** RT15.1 and RT15.6 cannot be
bare "add the constructor" commits: `env := deps.runEnv(...)` with no `env.` reader will not build. That
is why each constructor ships with at least one reader. Note the mechanism the RT15 decomposition got
wrong: an uncalled *method* compiles fine — what blocks it is the `unused` linter (`.golangci.yml:13`,
confirmed live: it already reports `func resolveHEAD is unused` in this package). So the minimum reader
count is **one**, which is what unlocks RT17.1/.3/.7's "add the method alone" chunks.

**3. `ReadyTimeout()` and `RemoteReadyTimeout()` must be declared in one commit.** Five of the nine
timeout sites read **both fields in one expression on one line** —
`effectiveAgentReadyTimeout(deps.agentReadyTimeout, deps.remoteAgentReadyTimeout, runner != nil)` at
`workloop.go:4900`, `reviewloop.go:587`/`:1368`, `dot_cascade.go:1772`, `dot_gate.go:475`. Splitting the
pair means editing the same line twice. The **method pair** is irreducible; the **nine application
sites** are not, which is why RT17.4–.6 exist.

**4. Each `SandboxProfileInput` composite literal is atomic.** `dot_cascade.go:1531-1543` and
`workloop.go:4512-4521` each read five or six `deps.sandboxCfg` sub-fields inside one literal. You cannot
convert half a composite literal and compile. Two blocks, two chunks. (Verified against E5 §3a, which is
wrong here: `reviewloop.go` and `dot_gate.go` have **zero** sandbox references.)

**5. `buildCognitionGateEval` + `executeCognitionGate` are one commit.** `buildCognitionGateEval` has zero
`deps` reads of its own; its entire body captures `deps` in a returned `handler.GateEvalFunc` and
forwards it. Split them and the closure still captures `deps` — zero progress. Cause: Go closure capture.

**6. A move and its path-pinned source sensors are one commit — `go build` cannot see the coupling.**
Verified sensors that read Go source by literal path or grep for literal source text:
`internal/daemon/runner_seam_hkhd2w6_test.go:47-49` (`filepath.Join(repoRoot, "internal", "daemon",
"dot_gate.go" / "dot_cascade.go" / "reviewloop.go")`);
`internal/daemon/reviewer_never_inherits_captured_hkpkxju_test.go:282/:290` and
`internal/daemon/dot_gate_reviewer_harness_hk01vs0_test.go:479` (literal strings
`"revNodeDefault := reviewerDefaultHarness("`, `"reviewerInheritedHarness :=
dotReviewerInheritedHarnessOverride("`, `"gateInheritedHarness := dotReviewerInheritedHarnessOverride("`
— **exporting these symbols breaks the greps even though the scanned files do not move**); and
`internal/specaudit/wminv003_task_branch_append_only_test.go:373`, keyed on the literal string
`"internal/daemon/workloop.go"` behind `//go:build specaudit` — **invisible to `go build`, `go test` and
`golangci-lint`**. That is why G3 includes `go test -tags specaudit`.

### And one hazard that is not irreducible but is silent

**Mutable package `var`s re-exported BY POINTER.** The run-path timing knobs are `var`, not `const`,
precisely so tests can mutate them, and `export_test.go` re-exports several by address:
`ExportedBriefDeliveredTimeout = &briefDeliveredTimeout`, `&noChangeKillDelay`, `&postQuitKillGrace`,
`&defaultPostAgentReadyHangTimeout`, plus `ExportedSetAgentReadyKillReapTimeout` writing
`agentReadyKillReapTimeout`. The lift's `var X = pkg.X` move-and-alias pattern **compiles green and
silently severs the mutation** — the test sets daemon's copy, the moved code reads the package's. No
compiler, no linter, no non-timing test catches it. **Vars must be pointer-repointed or moved outright,
never value-aliased.** This affects RT19.6, RT19b's `agentReadyKillReapTimeout` drain, and LIFT.5.

### What is NOT irreducible, contrary to the plan documents

- **`RunEnv.ProjectCfg` narrowing and `SharedHandles.RunRegistry` interface extraction.** E5 §4 calls
  them RT15 preconditions. Verified: both bundles stay in `package daemon` for all of RT15, so
  `ProjectCfg ProjectConfig` and `RunRegistry *RunRegistry` type-check with zero work. They block **the
  lift**, not RT15. (`E5-STAFFING-SEQUENCE.md` §7.6 reaches the same conclusion.)
- **The `waitWithSocketGrace` signature.** Once RT17.7 + RT17.11 + RT17.12 land, `LaunchPort`'s method
  set is a strict superset of `hookStoreIface`'s five (`hookrelay_chb025.go:51-64` — verified exactly
  those five), so `ports.Launch` is directly assignable to the existing `store hookStoreIface` parameter.
  No signature change, no new narrowing interface, no escalation. This is the single best idea in the
  RT17 decomposition and it survives everything else.
- **The three mode files.** E5 treats `dot_gate.go` / `reviewloop.go` / `dot_cascade.go` as a knot. They
  are a **DAG**: `dot_gate.go`'s apparent references to `dispatchDotAgenticNode` / `driveDotWorkflow` and
  `reviewloop.go:1249`'s reference to `dispatchDotAgenticNode` are all **comments**. Leaves-first, three
  commits. This also dissolves E5 §7.13's "five new `dot_cascade → reviewloop` cross-package exports".

### The one open escalation — `deps.tidGen`, and it must be raised BEFORE RT18 starts

`deps.tidGen *core.TransitionIDGenerator` has 10 reads in `beadRunOne` and no chartered home.
`RunPorts.TIDGen` would be an **eighth port** — `_plan.md` §1's no-new-seam rule makes that an operator
call, not an implementer's. **Recommended resolution for sign-off:** `SharedHandles.TIDGen`. The field's
own doc (`workloop.go:272-274`, "a single shared generator enforces…") says it is by construction
cross-goroutine state shared by reference, which is `SharedHandles`' documented charter verbatim
(`runports.go:321-323`). Cite that line in the commit body so review does not read it as a new seam.
`E5-STAFFING-SEQUENCE.md` §3.2 defaults instead to a pre-minted `TransitionID` on `RunEnv`; **either is
acceptable and neither is an implementer's choice.** Raise it before RT18.0 — it gates RT18.9 → RT18.11,
i.e. the majority of RT18's value.

---

## 3. Chunks that were REJECTED by the challenge

Four rejections. Each was a decomposition that read as sound and does not compile or does not hold.

### 3.1 RT18's read-conversion chunks — REJECTED, and this is the most valuable lesson in the file

The RT18 decomposition proposed converting `deps` reads to bundle fields **in place** inside
`executeCognitionGate` (39 reads), `dispatchDotAgenticNode` (80), `runReviewLoop` (143) and `beadRunOne`
(164), each as a no-signature-change chunk. **They have nothing to convert into.**

Verified against `runports.go:275-330`: `RunEnv` carries 19 fields (9 config, 10 identity), `RunPorts` 7
ports, `SharedHandles` 5 handles. `LaunchPort` has **exactly one method** (`BuildSpec`,
`runports.go:165-170`) against a documented charter of seven concerns. The run path also reads
`hookStore`, `harnessRegistry`, `adapterRegistry`, `substrate`, `reviewerSubstrate`, `handlerBinary`,
`handlerArgs`, `handlerEnv`, `daemonBinaryPath`, `agentReadyTimeout`, `remoteAgentReadyTimeout`,
`postAgentReadyHangTimeout`, `codexNoWorkDurationFloor`, `sandboxCfg`, `intentLogDir`, `brTimeoutCfg`,
`worktreeCreateMu`, `tidGen`, `runner`, `worktreeFactory` — **none of which has a home on any bundle
today.** "Mechanically rewrite the 143 reads" has no target.

The relation is not optional: **before RT16/RT17 those chunks are unwritable; after them they are empty**
(RT17's own exit gate is `grep -c 'deps\.'` on those files → 0). So roughly half the original RT18 plan
was RT16/RT17 re-labelled. **The lesson: an "in-place read conversion" chunk is only real if the
destination field already exists.** That is a one-command check —
`grep -n 'FieldName' internal/daemon/runports.go` — and it was not run.

**What survives:** the leaf-first ordering, the `clockOrSystem()` accessor idea (which removes the
copy-mutation from the critical path of every signature drop, and is the best idea in either RT18
document), the fourth clock default at `workloop.go:3176` that E5 misses, the verified isolation of
`SharedHandles` to `workloop.go`, and the a/b split as a reviewing device.

**Also rejected inside RT18:** the `grep -c 'deps\.' → 0` **entry gate**. It sees struct-field reads only.
`reviewloop.go` has 137 `deps.` reads but **39 distinct undefined package-level symbols** when relocated,
of which exactly one is `workLoopDeps`. The only sound entry gate is the compiler: relocate the file into
a scratch `package runloop` and require `go build -gcflags=-e` to report zero `undefined:`.

### 3.2 RT17's `ports RunPorts` threading (its C1/C2/C3) — REJECTED on two independent grounds

**(a) Wrong slice.** Adding a `ports RunPorts` parameter to `driveDotWorkflow` /
`dispatchDotAgenticNode` / `dispatchDotGateNode` / `runReviewLoop` **is RT18's scope**, and it
contradicts the idiom two sibling plans have already committed to disk: `RT15-chunks.md`'s stated
organizing idea ("a derived local is not a second passing idiom — it is an alias") and
`RT16-emitterport-conversion.md` §1a (`emit := deps.emitterPort()`, zero signature changes). Landing it
would put the tree in exactly the half-threaded state this catalogue exists to prevent.

**(b) Nil `Launch`.** `runPorts()` (`runports.go:336-344`) **deliberately leaves `Launch` nil** — the
header comment says so, and `rp := deps.runPorts()` runs at `workloop.go:3180`, **~810 lines before**
`rp.Launch` is assigned at `:3990`. Every conversion in the family turns a nil-safe field read into a
method call on a possibly-nil interface. `dot_gate.go:105` builds its *own* `deps.runPorts()` (Launch
nil) inside the very function the threading targets, and six `export_test.go` entry points have no
`Launch` at all. The sharpest case: a "nil-absorbing" `RegisterHookSession` converts a test-path **no-op**
(`if deps.hookStore != nil`) into a **panic**. **Fix adopted: RT17.0, a `launchPortFull()` derived local
that populates `Launch` non-nil by construction.** It also makes `unparam` irrelevant (no parameter
exists), keeps `export_test.go` at a zero diff, and drops `workloop.go` from five touches to two.

**Also rejected:** RT17's proposed `SandboxSpawn(...) *sandboxSpawn` — `sandboxSpawn` is **not a type**,
it is the local variable name at `dot_cascade.go:1531`. The real return type is `*SrtSpawnConfig`
(`sandboxgate.go:56`). And `LaunchPort.QuitOnReviewFile` — `pasteInjectQuitOnReviewFile`
(`pasteinject.go:2326`) reads **zero** `deps` fields, so a port method wrapping it reads nothing from
`daemonLaunch`. That is not a dependency port, it is pure indirection to enable a future package split —
the exact rejection class the operator denied a waiver for on E2b. **Dropped, not escalated.**

### 3.3 RT19's green criterion — REJECTED (the shape was right; the gate was wrong)

Every chunk's stated criterion was `make check-fast`. Two independent failures, both measured:

**(a) 8 of 21 chunks fail it.** `export_test.go` carries **10 lint findings today** that pass CI only
because `--new-from-rev` grandfathers them. Every line of a newly-created file is an addition in that
diff, so a byte-identical move un-grandfathers the finding. Verified by running the linter — the exact
lines are listed in RT19.0. **Fix: RT19.0, a ~12-line in-place cleanup, first.**

**(b) It runs zero tests.** `check-fast` derives its test set from `git diff --name-only HEAD`
(`Makefile:497-502`), which is **empty post-commit** — the criterion's own cost model describes a
`go test -short ./internal/daemon` run that never fires. Run it pre-commit and it instead tests whatever
unrelated packages a concurrent workflow left dirty. **Fix: pin the package explicitly (G2).**

**Also rejected:** two of RT19's four "irreducible" floors misstate Go. A type and its constructor do
*not* have to share a file (same-package files split freely), and an interface-satisfaction assertion
needs the methods **in the package, not the file** — proven by experiment (moving `noopTmuxAdapter`'s 15
methods away from the type and its `var _` assertion: `go vet` exit 0). RT19.1 and RT19.8 still ship
whole, but for the honest reason (the struct and its 82-field literal always change together in RT15;
splitting them doubles RT15's file span), not an invented language constraint. **This matters beyond
RT19 — the same false reasoning applied to production code would over-constrain the lift.**

### 3.4 LIFT chunks that do not compile — REJECTED

Six of the fourteen, verified by relocating each file into a real scratch `package runloop` and building.
Two are structural, not clerical, and force a re-cut:

- **L2 (`runshell.go`).** `runshell_test.go` declares `recordingEffectors` (`:21`) and `pumpUntilDone`
  (`:129`); `dispatchsegment_test.go` — also `package daemon`, and it stays in daemon until L6 — consumes
  both at `:122`, `:169`, `:247`. Removing `runshell_test.go` from daemon yields three `undefined:`
  errors. **The "a production file moves with its in-package test" rule collides with its own chunk
  ordering.** Fix: lift the two helpers into `internal/testhelpers` first, as a test-only chunk with an
  empty conflict window in every hot file — the same move RT19 already plans for `stubEventCollector`.
- **L10 (`dot_gate.go`).** `dot_gate_heartbeat_hkvjsv_test.go` is `package daemon` and calls
  `dispatchDotToolNode` (declared in `dot_cascade.go`, which does not move until L12) plus a fixture from
  `dot_cascade_tool_hkl8rpd_test.go`. Moved to runloop it sees neither; left in daemon it cannot see
  `dot_gate`'s internals. **The leaves-first ordering is violated by exactly one test file.**

Clerical but real: **L1** needs the result *type* and its fields exported too, not just the function —
`runbridge.go:234-236` reads `sgr.blocked` / `sgr.reason` (verified). **L7**'s "clean leaf, zero
staying-daemon symbols" is false — `harnessresolve.go:80/:84` call `emitBeadLabelConflict`, declared in
`moderesolve.go:200`, which stays. **L13** is not a chunk at all — see §4.

---

## 4. The lift (RT20+)

### 4.1 Can it be chunked? Yes — mostly. Not one atomic commit.

The E5 plan implies a knot. It is a **DAG**, verified by compiling each candidate file in a scratch
`package runloop`: `dot_gate.go`'s six apparent edges into `dot_cascade` and `reviewloop.go:1249`'s edge
are **all comments**. So the three mode files move as three separate leaves-first commits, and seven
early chunks (L0–L7, ~1,985 non-test LOC + ~765 test LOC) are gated on **nothing in the RT prep stream**.
`daemon → runloop` is the legal import direction and `export_test.go` absorbs the signature churn for all
359 `daemon_test` files, so each boundary is a state the tree can sit at.

**Two exceptions, and they are the honest answer to the question.**

**LIFT.12 — `dot_cascade.go` + `sub_workflow_runner.go` is one irreducible commit: 3,296 non-test LOC.**
They are mutually dependent at the symbol level (`dot_cascade.go:1039` calls `newDotSubWorkflowRunner`;
`sub_workflow_runner.go:317/:323/:351` call `dispatchDotToolNode` / `dispatchDotAgenticNode` /
`dispatchDotGateNode`). Go forbids an import cycle, so **no intermediate state exists**. The E5 recipe
that placed `sub_workflow_runner.go` in an early chunk would have created a hard `runloop → daemon` edge
failing depguard on its first build. *Mitigation that should be taken first:* a **pure intra-package file
split** of `dot_cascade.go` inside `package daemon` — zero behavior risk, zero signature change, no
depguard rule can object — isolating the `rl*` reviewloop helpers and the tool-node dispatcher from the
`newDotSubWorkflowRunner`-coupled core. The cycle is real; it does not implicate all 3,296 lines.

**LIFT.13 — `beadRunOne` is NOT a chunk as anyone has scoped it. Say this plainly.** It is a **single
2,229-line function** (`workloop.go:3171`, next `^func ` at `:5401`), so there is no partial extraction.
Relocating it into a real package yields **66 distinct undefined symbols**, against the 9 the lift
decomposition accounted for. Beyond the named callees it needs `resolveBranching`,
`parseBranchingSection`, `resolveWorkflowMode`, `resolveWorkflowRef`, `resolveOwningEpicFromRecord`,
`resolveParentCommit`, `resolvePiProfile`, `PiProfileConfig`, `ResolveModelPreference`,
`loadStandardGraph`, `isInAllowedRepos`, `hasAPIKeyInEnv`, `hasSingleModelLabel`, `exitCodeClean`,
`transitionToTerminated`, `productionWorktreeFactory`, `CrossRepoUnsafeError`, `LandsOnProtectedError`,
the `emit*` family, `sandboxSpawnForRun`, `sandboxWrapExecArgv`, `verifySandboxEngaged`,
`srtEngagementCanaryPath`, `SandboxProfileInput`, `perRunSubstrate`, `newPerRunSubstrate`, `launchPort`,
`worktreePort`, `spineArgs`, `noCommitGuardShouldReopen`, `beadAlreadySubsumedInMain`,
`isWatcherErrCanceled`, `substrateSpawnStats`, `forceTeardownSession`, `quitSender` and more. Neither
RT15 nor RT19b covers these. **The honest scoping: LIFT.13 is a themed drain sequence (4–6
alias-preserving chunks, clustered as branching/workflow resolution, model/profile resolution, sandbox,
guards, emitters) followed by a final pure relocation of the caller.** Attempting it as one commit is
exactly the abandon-partway hazard, in the hottest file in the tree.

### 4.2 Entry criteria — all must hold, each returns a number

| # | Criterion | Command | Required |
|---|---|---|---|
| 1 | `deps.` reads in the three mode files | `grep -c 'deps\.' internal/daemon/{reviewloop,dot_cascade,dot_gate}.go` | **0 / 0 / 0** (today 137 / 88 / 41) |
| 2 | Copy-mutation idiom gone | `grep -c 'deps\.clock = \|deps\.launchSpecBuilder = ' internal/daemon/*.go` | **0** (today **6**: `workloop.go:3176/:3973/:3983`, `dot_cascade.go:220/:1260`, `reviewloop.go:230`) |
| 3 | `RunEnv` / `SharedHandles` are live **and fully populated** | `grep -c 'env\.RunID' internal/daemon/*.go` > 0 **and** every construction site passes `runID`, `beadRecord` and the queue/item fields — a half-populated `RunEnv` threaded through seven functions with `env.RunID` legally returning the zero UUID is a silent trap no compiler catches | non-zero, identity half populated |
| 4 | `SharedHandles.RunRegistry` is not a daemon concrete type | read `runports.go` | 3-method interface (`SetAgentType`/`SetMachine`/`SetResolvedProvider`) — **operator-blessed before the lift, not during** |
| 5 | `RunEnv.ProjectCfg` is a daemon-free type | read `runports.go` | `projectconfig.ProjectConfig` (leaf) or a narrowed run-scoped view. Owner: **PC**, which needs the `cmd/harmonik/**` write-lock escalation (`E5-STAFFING-SEQUENCE.md` §3.1) |
| 6 | `export_test.go` is gone | `test ! -e internal/daemon/export_test.go` | true (RT19.20) |
| 7 | **Per-file compile probe, not a grep.** For each candidate file: relocate into a scratch `package runloop` and build | `go build -gcflags=-e` | **zero `undefined:`** |
| 8 | `internal/runloop` depguard block drafted **and reviewed** | read `.golangci.yml` | allows `$gostd, core, handler, handlercontract, workspace, substrate, runexec, runmerge, mergeq, gitprobe, queue, workers, policy, workflow, workflow/dot, brcli, lifecycle/tmux, sessiondata, harness/{shared,claude,codex,pi}, transport/{tunnel,codesync}, lifecycle, self`; denies `internal/daemon`. **Missing `policy` / `workflow` / `workflow/dot` / `brcli` fails lint on the lift's first commit** |
| 9 | Freeze gate ships in the **same commit** as the depguard block | read `Makefile` | wired into `check-fast` and `check-short`. The harness unit did not do this and the hole is still open (`PROGRESS.md` §5 item 7) |
| 10 | **Fleet quiesce — per chunk, re-measured immediately before starting, not at stream start** | `git branch -a --no-merged` filtered to the chunk's files | zero unmerged branches touching them. A package move rewrites every line of a file; git cannot three-way-merge that against a divergent branch. Today **71** unmerged local branches carry commits on lift-set files and 14 worktrees are registered |
| 11 | Differential oracle is trustworthy | `df -h` | **> 10 GiB free** (the daemon's dispatch watermark). Below it, every timing failure is suspect, not real — `00-test-oracle-baseline.md` Addendum §3 |
| 12 | Nothing else in Lane A is in flight | operator | verified. This is the one irreversible step in P2 |

### 4.3 Abort / rollback

**Before the point of no return (LIFT.0 through LIFT.11):** each chunk is one commit that moves whole
files and re-points call sites. Abort is `git revert <sha>` of that single commit — nothing downstream
depends on it because `daemon → runloop` is the legal direction and every chunk left the tree green. If
the revert conflicts with an upstream commit landed under it, revert forward instead: `git mv` the file
back and re-inline the export. **Do not `git reset --hard` and do not `git clean`** — the P2 stream
nearly swept 49 of another agent's files that way (`PROGRESS.md` §2).

**At and after LIFT.12 (`dot_cascade.go` + `sub_workflow_runner.go`), abort is expensive, not
impossible.** The commit is a 3,296-LOC file move; reverting it re-creates the files in `internal/daemon`
and re-inlines ~30 exports. It is a mechanical revert *if taken within the same session* — the cost grows
with every commit that lands on top, at 104 commits/90d for `dot_cascade.go` alone. **Therefore: LIFT.12
and LIFT.13 must not be started unless the fleet-quiesce check (criterion 10) was re-run within the last
hour and Lane A is otherwise empty.**

**The abort procedure, literally:**
1. Stop. Do not attempt a fix-forward on a failing package move — the failure mode is a partially
   re-pointed call graph, and every additional edit widens it.
2. `git stash list` / `git status --short` — record what is dirty, by explicit pathspec.
3. `git revert --no-commit <sha>` for the lift commit; resolve any conflict in favor of the pre-lift
   state; `git revert --continue`.
4. Run **G3 in a clean detached worktree**. The gate is the differential oracle, not "green" —
   `internal/daemon` is already red at HEAD.
5. Re-run criterion 7 (the compile probe) on the file that failed. The failure is almost always a symbol
   the probe would have found: run it, do not guess.
6. Record the abort in `PROGRESS.md` with the undefined-symbol list. That list is the input to the
   themed-drain chunk that must land before the move is retried.

### 4.4 The one-sentence answer

**The lift is not one atomic commit — L0 through L11 are genuinely chunkable and seven of them are ready
before the prep stream finishes — but LIFT.12 is one irreducible 3,296-LOC commit, LIFT.13 is a
themed-drain sequence and not a chunk, and neither may be attempted until every RT chunk has landed, the
compile probe is clean, and the tree is quiet.**

---

## 5. Progress metric

Five numbers. Each is one command, each is monotone in the right direction, and none of them can be
faked by a green build.

```bash
# M1 — deps. reads in the three mode files.  Today: 137 / 88 / 41.  Target: 0 / 0 / 0.
grep -c 'deps\.' internal/daemon/reviewloop.go internal/daemon/dot_cascade.go internal/daemon/dot_gate.go

# M2 — deps.bus sites converted.  Today: 34 / 51 / 23 / 7 / 3 / 2 = 120 raw (118 code, 2 comments).
#      Target: 0 on the five run-path files; workloop.go stays at exactly 10 (the outer runWorkLoop
#      sites, which correctly never move).  Owner: RT16 (108 of the 118).
for f in workloop reviewloop dot_cascade dot_gate runbridge sub_workflow_runner; do \
  printf '%-22s %s\n' "$f" "$(grep -c 'deps\.bus' internal/daemon/$f.go)"; done

# M3 — the copy-mutation idiom.  Today: 6.  Target: 0.  THIS IS THE ONE THAT SAYS WHETHER THE PORT
#      WORK WAS REAL.  E5 §7.2: "any port work that does not delete this idiom is cosmetic."
#      Owners: the 2 launchSpecBuilder sites -> RT18.11; the 4 clock sites -> RT18.1+RT18.2.
grep -n 'deps\.clock = \|deps\.launchSpecBuilder = ' internal/daemon/*.go | wc -l

# M4 — export drain.  201 symbols need exporting across the whole unit (E5 §"Total symbols needing
#      export": 201).  RT13 drained 21; RT19b drains 15.  Today 36/201 = 18%.  Every remaining symbol
#      must appear in RT19's or a LIFT chunk's ENUMERATED list before the lift starts — a symbol with
#      no named slice is a build break waiting at lift time.
#      Measure the residual by compile probe, not by grep:
#        (relocate file into a scratch package runloop) && go build -gcflags=-e 2>&1 | grep -c 'undefined:'

# M5 — export_test.go, the single largest shim surface.  Today: 2,895 LOC, 214 Exported* decls,
#      257 top-level decls, 359 package daemon_test consumers, 230 commits/90d.
#      Target: the file DOES NOT EXIST.  Binary, not a threshold.
wc -l internal/daemon/export_test.go 2>/dev/null || echo "RT19 COMPLETE"
```

**Two rules about reading these numbers.**

M1 falling is **not** evidence of progress on its own — it falls when reads are converted *and* when
reads are deleted, and only M3 distinguishes a real port drain from a cosmetic one. Report M1 and M3
together or neither.

M4 cannot be measured by grep. `grep -c 'deps\.'` sees struct-field reads only: `reviewloop.go` has 137
`deps.` reads but **39 distinct undefined package-level symbols** when relocated, of which exactly one is
`workLoopDeps`. Use the compile probe.

**Secondary, for the shrink narrative** (`E5-STAFFING-SEQUENCE.md` §6 owns the projection, do not
duplicate it): `internal/daemon` is at **103 non-test files / ~49,064 LOC** against a baseline of
126 / 57,197 (−14.2%). `workloop.go` is at **6,864** lines, from 8,378.

---

## 6. Recommended next chunk after RT13 / RT14 / RT16 land

## → **RT19.0** — fix the 10 grandfathered lint findings in `export_test.go`, in place.

`internal/daemon/export_test.go` — roughly 12 lines: name four gocritic `unnamedResult` returns
(`:762`, `:1203`, `:1248`, `:2553`), check the `w.observe` error (`:1317`), rename the shadowing
`substrate` local (`:1892`), combine the param types (`:2160`), remove the ineffectual `adapterReg`
assignment (`:425`), collapse `:1660`'s six results into a struct, and add one justified
`//nolint:containedctx` for the `context.Context` field at `:223`.

Why this one, ahead of everything else on the list:

1. **It is the only ready chunk that unblocks eight other chunks.** Without it, RT19.1, .2, .5, .13,
   .17, .18, .19 and .20 fail `--new-from-rev` on a byte-identical move. Every one of those is otherwise
   pure relocation with zero risk. This is the highest unblock-per-line ratio in the stream.
2. **It holds `workloop.go` for zero minutes and changes zero production bytes.** RT15 and RT18 are both
   gated behind RT14/RT16/RT17 and all of them want the hottest file in the tree. RT19.0 wants none of it.
3. **It is measured, not inferred.** The ten findings were produced by running
   `./.tools/golangci-lint run ./internal/daemon/...` in this tree; the line numbers above are literal
   linter output, not a plan's estimate.
4. **RT19.1 follows immediately and pays off RT15.** Isolating the five `workLoopDeps` builder
   declarations into their own file means RT15 later edits a ~560-line file instead of a 2,895-line one —
   a 5x reduction in RT15's own conflict window on the second-hottest file in the tree, banked before
   RT15 starts rather than discovered during it.
5. **It is trivially reversible and trivially reviewable.** One commit, twelve lines, no signature, no
   move, no package. If the stream stops immediately after, the tree is strictly better than before.

The chunk after that is **RT19.1**, then the rest of RT19 in any order, then **RT17.0** the moment RT16
is committed. `E5-STAFFING-SEQUENCE.md` §5's recommendation of **RT19b first** is unchanged and takes
precedence — RT19.0 is what to run when Lane A is blocked or when a second writer needs safe work that
touches none of Lane A's files.
