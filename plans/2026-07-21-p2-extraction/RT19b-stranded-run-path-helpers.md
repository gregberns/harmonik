# RT19b — the eleven stranded run-path helpers leave `internal/daemon`

**Status:** READY

**Depends on:** RT13 (merge path → `internal/runmerge`) and E4c (`buildWorkerRegistry` →
`internal/workers/bootwire.go`) must be **committed** and the tree must build green. Nothing else.

> **RT19b is NOT gated on RT15–RT19, and this document proves it.** E5 §4 step 19 files RT19b at the
> tail of the prep stream, implying it waits for the port work. It does not have to. **All eleven
> helpers already take their inputs as explicit parameters — not one of them reads `deps`
> (`workLoopDeps`).** Verified by reading every signature (§1). That makes RT19b executable the moment
> RT13 lands, and it makes it the *cheapest* remaining item in the E5 stream: it drains eleven of the
> ~twenty back-edges E5 §7.15 says survive RT19, without touching the ports, the god-struct, or a
> single signature that carries `deps`.

**Size:** **~357 non-test LOC leave `internal/daemon`** (285 from `workloop.go`, 72 from
`agentready.go`), 16 symbols move, **~88 production call sites** rewritten across **7 daemon files**,
plus 1 test file relocated, ~6 `export_test.go` shims re-pointed, one new leaf package, one
`.golangci.yml` block, one freeze-gate script, two `Makefile` lines. **Three commits.**

**Risk:** **MEDIUM — entirely from merge-conflict exposure, not from logic.** Every edit is a
qualifier substitution the compiler verifies; the danger is that the substitutions land in
`workloop.go` (331 commits/90d), `export_test.go` (229), `dot_cascade.go` (104) and `reviewloop.go`
(98) at 88 separate points, so a long-open window is how this slice fails.

---

## 0. Corrections to the E5 plan (it was written before RT13 and E1b-prep landed)

Read this section before `E5-dot-runloop.md` §3a's "third band" row — nine of its statements are now
wrong or incomplete. Each correction below was verified by reading the current tree.

| E5 says | Truth as of this document | Evidence |
|---|---|---|
| `artifactAgentType(a claudeRunArtifacts)` "is signature-coupled to an **E1b** private type" and "cannot be resolved until E1b re-homes `claudeRunArtifacts`" | **DISSOLVED.** E1b-prep (`c3fff27d`) renamed the DTO and moved it to `internal/harness/shared`. The signature today is `func artifactAgentType(a shared.LaunchArtifacts) core.AgentType`. It reads one field of a type that already lives in a daemon-free leaf. **This is now the single easiest helper in the set, not the blocked one.** | `internal/daemon/workloop.go:6582`; `internal/harness/shared/launchctx.go:185,214` |
| The stranded set is **eleven** helpers | **Sixteen symbols must move.** `preExecMsgType` (a private callee of `emitPreExecBeforeLaunch`) was omitted; and `emitAgentReadyTimeout` reads `defaultAgentReadyTimeout`, which drags the four-symbol HC-056 deadline family out of `agentready.go` with it (§1c). | `workloop.go:5803, 5826, 6595`; `agentready.go:42-116` |
| The eleven are "called by files this unit moves" | **Two of them are also called by a file that STAYS.** `emitSpawnCapBlocked` and `emitTmuxNewWindowTimeout` are called from `bootstate.go` — boot-time spawn-semaphore instrumentation, not the run path. This is decisive: they must land in a **leaf both can import**, never inside `internal/runloop`. | `internal/daemon/bootstate.go:275, :278` |
| `forceTeardownSession` is listed under §3b **"Never exported"** | Wrong category. "Never exported" is the list of daemon-private symbols the lift must not surface (`workLoopDeps`, `runWorkLoop`, …). `forceTeardownSession` is a four-line `sess.Kill(context.Background())` with five run-path callers and zero daemon state — it is a **mover**. | `workloop.go:5768-5773` |
| `beadAlreadySubsumedInMain` call sites: implied to be workloop-only | Also called from **`dot_cascade.go:2063`** (the DOT no-change subsumed check, hk-9v5yo). Four production call sites, not three. | `internal/daemon/dot_cascade.go:2063` |
| Line numbers `:1217 :147 :5440 :5760 :5807 :5841 :8012 :8044 :8077 :8106 :8117` | **All stale.** They predate RT13, which cut `workloop.go` from 8,378 to **6,854** lines. Current line numbers are in §1. Use the `grep` anchors given there, not the numbers, if E4c has shifted the file again. | `wc -l internal/daemon/workloop.go` → 6854 |
| RT19b is sequenced after RT15–RT19 | Not required. Zero of the sixteen symbols reads `deps`. RT19b is executable now. | §1, every signature |
| — (not noted) | **A real defect this slice fixes for free:** `artifactAgentType` was spliced into the MIDDLE of `emitAgentReadyTimeout`'s doc comment. Lines 6572–6574 ("…The event carries run_id, claude_session_id, and") and 6589–6592 ("timeout_ms so post-hoc analysis…") are one severed sentence with an unrelated function wedged between them. Moving `artifactAgentType` out rejoins it. | `workloop.go:6572-6593` |
| — (not noted) | `internal/harness/shared/refstrailer.go` **already owns the two primitives `beadAlreadySubsumedInMain` open-codes** — `RefsTrailerLine` (the exact `"Refs: <id>"` string) and `ContainsExactLine` (the line-exact, CR-tolerant match). Its doc comment names `beadAlreadySubsumedInMain` as the contract three times. The rule currently has two copies in two packages that can drift. | `internal/harness/shared/refstrailer.go:9, :31, :91, :94, :101, :240` |

---

## 1. What changes

### 1a. The sixteen symbols, their current homes, and their destinations

Line spans are **doc comment through closing brace**, measured on the post-RT13 tree
(`internal/daemon/workloop.go` = 6,854 lines, `agentready.go` = 207 lines).

| # | Symbol | File:lines | LOC | Reads `deps`? | Destination | Exported as |
|---|---|---|---|---|---|---|
| 1 | `clockAfter` | `workloop.go:1215-1233` | 19 | no | `internal/substrate/clock.go` | `substrate.After` |
| 2 | `artifactAgentType` | `workloop.go:6575-6587` | 13 | no | `internal/harness/shared/launchctx.go` | `shared.ArtifactAgentType` |
| 3 | `beadAlreadySubsumedInMain` | `workloop.go:5438-5470` | 33 | no | `internal/harness/shared/refstrailer.go` | `shared.MainHistoryHasRefsTrailer` |
| 4 | `agentReadyKillReapTimeout` (`var`) | `workloop.go:136-155` | 20 | no | `internal/runlaunch/deadlines.go` | `runlaunch.KillReapTimeout` |
| 5 | `forceTeardownSession` | `workloop.go:5742-5773` | 32 | no | `internal/runlaunch/teardown.go` | `runlaunch.ForceTeardownSession` |
| 6 | `emitPreExecMessage` | `workloop.go:5775-5799` | 25 | no | `internal/runlaunch/events.go` | `runlaunch.EmitPreExecMessage` |
| 7 | `preExecMsgType` | `workloop.go:5801-5811` | 11 | no | `internal/runlaunch/events.go` | **stays unexported** |
| 8 | `emitPreExecBeforeLaunch` | `workloop.go:5813-5833` | 21 | no | `internal/runlaunch/events.go` | `runlaunch.EmitPreExecBeforeLaunch` |
| 9 | `emitImplementerPhaseComplete` | `workloop.go:6480-6510` | 31 | no | `internal/runlaunch/events.go` | `runlaunch.EmitImplementerPhaseComplete` |
| 10 | `emitSpawnCapBlocked` | `workloop.go:6512-6542` | 31 | no | `internal/runlaunch/events.go` | `runlaunch.EmitSpawnCapBlocked` |
| 11 | `emitTmuxNewWindowTimeout` | `workloop.go:6544-6570` | 27 | no | `internal/runlaunch/events.go` | `runlaunch.EmitTmuxNewWindowTimeout` |
| 12 | `emitAgentReadyTimeout` | `workloop.go:6572-6574` + `:6589-6607` | 22 | no | `internal/runlaunch/events.go` | `runlaunch.EmitAgentReadyTimeout` |
| 13 | `defaultAgentReadyTimeout` | `agentready.go:42-64` | 23 | no | `internal/runlaunch/deadlines.go` | `runlaunch.DefaultAgentReadyTimeout` |
| 14 | `defaultRemoteAgentReadyTimeout` | `agentready.go:66-80` | 15 | no | `internal/runlaunch/deadlines.go` | `runlaunch.DefaultRemoteAgentReadyTimeout` |
| 15 | `effectiveAgentReadyTimeout` | `agentready.go:82-105` | 24 | no | `internal/runlaunch/deadlines.go` | `runlaunch.EffectiveAgentReadyTimeout` |
| 16 | `ErrAgentReadyTimeout` | `agentready.go:107-116` | 10 | no | `internal/runlaunch/deadlines.go` | `runlaunch.ErrAgentReadyTimeout` |

**Total: 357 non-test LOC out of `internal/daemon`** (285 from `workloop.go`, 72 from `agentready.go`).

Anchors, if E4c has moved the lines: `grep -n '^func clockAfter\|^var agentReadyKillReapTimeout\|^func beadAlreadySubsumedInMain\|^func forceTeardownSession\|^func emitPreExec\|^func preExecMsgType\|^func emitImplementerPhaseComplete\|^func emitSpawnCapBlocked\|^func emitTmuxNewWindowTimeout\|^func artifactAgentType\|^func emitAgentReadyTimeout' internal/daemon/workloop.go`

### 1b. Files edited

| File | Lines/sites | What | Why |
|---|---|---|---|
| `internal/daemon/workloop.go` | −285 LOC; 42 call sites re-qualified | 12 symbols cut out; remaining in-file callers re-qualified | the twelve are stranded outside both E5 extraction regions |
| `internal/daemon/agentready.go` | −72 LOC; 3 call sites re-qualified (`:104`, `:162`, `:202`) | HC-056 deadline family cut out; `waitAgentReady` stays | `emitAgentReadyTimeout` cannot move without `defaultAgentReadyTimeout` (§1c) |
| `internal/daemon/reviewloop.go` | 21 sites | re-qualify | 98 commits/90d — the second-hottest mover |
| `internal/daemon/dot_cascade.go` | 18 sites | re-qualify | 104 commits/90d |
| `internal/daemon/dot_gate.go` | 8 sites | re-qualify | — |
| `internal/daemon/dispatchsegment.go` | 1 site (`:198`) | `clockAfter` → `substrate.After` | already imports `substrate` |
| `internal/daemon/bootstate.go` | 2 sites (`:275`, `:278`) | `emitSpawnCapBlocked` / `emitTmuxNewWindowTimeout` → `runlaunch.*` | **the proof a leaf is required** — this file never moves |
| `internal/daemon/export_test.go` | 6 shims re-pointed | forwarders to the new homes | keeps 7 daemon test files compiling with **zero** edits |
| `internal/daemon/clockport_rt1_test.go` | whole file (62 LOC) | relocate → `internal/substrate/after_test.go` | it is a `substrate.FakeClock` contract test wearing a daemon package clause |
| `internal/substrate/clock.go` | +19 | `After` | the ClockPort determinism seam's own charter |
| `internal/harness/shared/launchctx.go` | +13 | `ArtifactAgentType` | reads one field of the type declared 370 lines above it |
| `internal/harness/shared/refstrailer.go` | +~25 | `MainHistoryHasRefsTrailer` | consolidates onto `RefsTrailerLine` + `ContainsExactLine` |
| `internal/runlaunch/{doc,events,deadlines,teardown}.go` | +~240 | **new leaf package** | §2 |
| `.golangci.yml` | +1 block | `runlaunch` allow/deny | the import fence |
| `scripts/runlaunch-freeze-gate.sh` | new | symbol ratchet | the creation fence |
| `Makefile` | +2 lines (`check-fast`, `check-short`) + a `.PHONY` target | wire the gate | hard CI failure, per the operator decision |

### 1c. Why the HC-056 deadline family (#13–#16) travels with `emitAgentReadyTimeout`

`emitAgentReadyTimeout` is not self-contained: at `workloop.go:6595` it substitutes
`defaultAgentReadyTimeout` for a zero argument. That constant lives in `agentready.go`. So one of
three things must happen, and only one of them is a pure move:

- **Move the whole HC-056 family (TAKE).** `defaultAgentReadyTimeout` (150 s),
  `defaultRemoteAgentReadyTimeout` (210 s), `effectiveAgentReadyTimeout` (the local/remote resolver)
  and `ErrAgentReadyTimeout` (the sentinel) are one feature — all four carry
  `Spec ref: specs/handler-contract.md §4.9 HC-056` in their own doc comments, and all four are
  *already* run-path back-edges: `effectiveAgentReadyTimeout` is called from `dot_cascade.go:1772`,
  `dot_gate.go:475`, `reviewloop.go:587/:1368`; `ErrAgentReadyTimeout` is matched at
  `dot_cascade.go:1866`, `dot_gate.go:479`, `reviewloop.go:730/:1457`. Draining them costs 9 extra
  call-site rewrites and removes 4 more back-edges the lift would otherwise hit.
- **Move only `defaultAgentReadyTimeout` (FALLBACK, if the slice must be cut short).** Legal, compiles,
  but splits the 150 s/210 s pair across two packages so a future tuning change has to be made twice.
  Acceptable under time pressure; say so in the commit body if taken.
- **Give `emitAgentReadyTimeout` a mandatory non-zero timeout argument.** **REJECT** — that is a logic
  change to a fallback path, and `_plan.md` §5.1 rejects logic deltas inside an extraction.

**Consequence for RT14:** RT14 (E5 §4 steps 14–18) plans to delete `agentready.go` after converting
its two `waitAgentReady` callers. RT19b makes that easier, not harder — after this slice
`agentready.go` holds only `waitAgentReady` + `agentEventSource` + their doc block, so RT14's step 17
becomes a whole-file delete instead of a partial one. **Land RT19b before RT14.**

### 1d. What deliberately does NOT move

- `emitRunStarted` (`workloop.go:5916`), `emitRunCompleted` (`:5935`), `emitImplementerEscapedWorktree`,
  `transitionToTerminated`, `emitWorkloopLifecycleTransition` and the ~25 other daemon `emit*`
  functions. They are **inside** `beadRunOne` / an E5 lift target, or read `deps`. They are the lift's
  problem, not RT19b's. Do not let the freeze gate or a "while I'm here" impulse drag them in.
- `noCommitGuardShouldReopen` (`workloop.go:5409-5436`). It **calls**
  `beadAlreadySubsumedInMain` but is itself run-path policy inside the `beadRunOne` band; it stays and
  becomes a caller of `shared.MainHistoryHasRefsTrailer`.
- `waitAgentReady` / `agentEventSource` (`agentready.go:118-207`). RT14 owns these.
- `resumeReadyProbeDelay` (`dispatchsegment.go:61`). Private to a file that moves whole in the lift.

---

## 2. The seam it exits behind

**No new seam is invented. No new interface, port, or injected closure is created anywhere in this
slice.** Every one of the sixteen symbols is *already* a free function or a package-level scalar whose
inputs arrive as explicit parameters. The move is a change of package clause and a qualifier
rewrite — nothing is threaded, wrapped, or abstracted.

Concretely, per destination:

| Destination | Pre-existing seam / charter it lands under | Verified at |
|---|---|---|
| `internal/substrate` | the **ClockPort determinism port** — the package doc names "ClockPort + Ticker with a real and a fake implementation, so a vertical can replay timeouts and poll races in virtual time (RS-015)" as one of its four charters. `clockAfter` is `time.After` re-expressed on that port; it uses nothing but `context`, `time` and `ClockPort`. | `internal/substrate/doc.go:17-19`; `internal/substrate/clock.go:9-18`; `workloop.go:1225-1233` |
| `internal/harness/shared` | the **cross-harness leaf** created by E1a-0, whose depguard comment states its charter as "the pieces that more than one harness implementation needs and none of them owns … the `Refs:<bead>` trailer machinery (refstrailer.go)". `ArtifactAgentType` reads one field of `shared.LaunchArtifacts`, declared in the same package. `MainHistoryHasRefsTrailer` is the *definition* of the contract `refstrailer.go` already enforces. | `.golangci.yml:180-192`; `internal/harness/shared/launchctx.go:185,214`; `internal/harness/shared/refstrailer.go:90-104, :239-241` |
| `internal/runlaunch` (**new leaf package**) | the same **freeze-then-strangle leaf pattern** every landed P2 slice used — `gitprobe`, `harness/shared`, `harness/{claude,codex,pi}`, `transport/{tunnel,codesync}`, `queuewiring`, `crewrun`, `runmerge`. Its nearest sibling is `internal/runmerge/events.go`, landed by RT13, whose functions have the identical `(ctx, bus handlercontract.EventEmitter, runID, …)` shape. | `.golangci.yml:905-918`; `internal/runmerge/events.go:48-134` |

**On whether a new package counts as a new seam — it does not, and here is the boundary.** `_plan.md`
§1's rule is "move implementations out … behind seams that ALREADY EXIST — not by inventing new
seams." A *seam* is an injection point: an interface the daemon implements and threads in (the thing
E2b was blocked for, needing a brand-new `crewSpawner` port). A *leaf package of plain functions* is
the mechanism every one of the nine landed slices used and is what §3.1 of `_plan.md` prescribes
("New package deny-back edge"). RT19b creates zero interfaces and injects zero closures. **No
escalation is required.**

**Charter sentence for `internal/runlaunch`, to be pasted into its `doc.go`:** *the effects a bead run
performs around an agent launch — the CHB-018 pre-exec announcement relay, the HC-056 readiness
deadlines and their sentinel, the spawn-cap / tmux-window / agent-ready anomaly events, the
implementer phase-complete report, and the force-teardown backstop.* It is the effect vocabulary that
pairs with `internal/runexec`'s pure `Dispatch` machine, which models exactly the same launch → ready
→ working segment (`internal/runexec/dispatch.go:59-73`, whose `DispatchConfig.ReadyTimeout` /
`ReadyKillReap` fields are populated from symbols #13 and #4 at `reviewloop.go:587-589`,
`dot_cascade.go:1772-1774`).

---

## 3. Coupling to break

### 3a. Outbound (moved code → `internal/daemon`): **ZERO**

This is the property that makes RT19b safe and is why it can run out of numerical order. Every one of
the sixteen symbols was read in full; none references any daemon package-level symbol, and none reads
`deps` / `workLoopDeps`. Full closure of what the moved code needs:

| Moved to | Needs | All already available there? |
|---|---|---|
| `substrate.After` | `context`, `time`, `substrate.ClockPort` | yes — same package |
| `shared.ArtifactAgentType` | `shared.LaunchArtifacts`, `core.AgentType` | yes — `launchctx.go` imports `core` |
| `shared.MainHistoryHasRefsTrailer` | `context`, `os/exec`, `strings`, `core.BeadID` | yes — `refstrailer.go` imports all four |
| `runlaunch/*` | `context`, `encoding/json`, `errors`, `time`, `core`, `handlercontract`, `handler` | new package; §4 step 12 lists the exact allow-list |

The one thing to be careful about: `beadAlreadySubsumedInMain` uses a **bare `exec.CommandContext`**
with `cmd.Dir = projectDir`, while its new neighbours in `refstrailer.go` route through
`tmux.CommandRunner`. **Do not add runner routing.** That would be a behavior change (it would make a
local-only probe remote-capable) and is out of scope. Move it exactly as written; note the asymmetry
in its doc comment so the next reader does not "fix" it either.

### 3b. Inbound (`internal/daemon` → moved code): **15 exports, 88 call sites**

`preExecMsgType` is the sixteenth symbol and stays unexported (its only caller,
`emitPreExecBeforeLaunch`, moves with it).

| Symbol | Exported as | Production call sites to rewrite (file:line) |
|---|---|---|
| `clockAfter` | `substrate.After` | `dispatchsegment.go:198`; `dot_cascade.go:1871`; `reviewloop.go:735,1468`; `workloop.go:4922` — **5** |
| `artifactAgentType` | `shared.ArtifactAgentType` | `dot_cascade.go:1476,1527,1531,1569,1695,1700,1704,2018`; `dot_gate.go:381`; `reviewloop.go:371,565,570,575,1294,1353,1357`; `workloop.go:4358,4362,4481,4516,4866,4871,4875,5130` — **24** |
| `beadAlreadySubsumedInMain` | `shared.MainHistoryHasRefsTrailer` | `dot_cascade.go:2063`; `workloop.go:5323,5435,5549` — **4** |
| `agentReadyKillReapTimeout` | `runlaunch.KillReapTimeout` | `dot_cascade.go:1774,1871`; `dot_gate.go:486`; `reviewloop.go:589,735,741,1370,1468,1474`; `workloop.go:4922,4934` — **11** |
| `forceTeardownSession` | `runlaunch.ForceTeardownSession` | `dot_cascade.go:1901`; `dot_gate.go:448`; `reviewloop.go:774,1500`; `workloop.go:4770` — **5** |
| `emitPreExecMessage` | `runlaunch.EmitPreExecMessage` | `dot_cascade.go:1821`; `dot_gate.go:428`; `reviewloop.go:634,1405`; `workloop.go:4719` — **5** |
| `emitPreExecBeforeLaunch` | `runlaunch.EmitPreExecBeforeLaunch` | `dot_cascade.go:1659`; `dot_gate.go:415`; `reviewloop.go:528,1328`; `workloop.go:4653` — **5** |
| `emitImplementerPhaseComplete` | `runlaunch.EmitImplementerPhaseComplete` | `dot_cascade.go:1940`; `reviewloop.go:808`; `workloop.go:5106` — **3** |
| `emitSpawnCapBlocked` | `runlaunch.EmitSpawnCapBlocked` | **`bootstate.go:275`**; `dot_cascade.go:1809`; `reviewloop.go:619,1392`; `workloop.go:4700` — **5** |
| `emitTmuxNewWindowTimeout` | `runlaunch.EmitTmuxNewWindowTimeout` | **`bootstate.go:278`**; `dot_cascade.go:1812`; `reviewloop.go:625,1398`; `workloop.go:4707` — **5** |
| `emitAgentReadyTimeout` | `runlaunch.EmitAgentReadyTimeout` | `dot_cascade.go:1880`; `dot_gate.go:493`; `reviewloop.go:750`; `workloop.go:4944` — **4** |
| `effectiveAgentReadyTimeout` | `runlaunch.EffectiveAgentReadyTimeout` | `dot_cascade.go:1772`; `dot_gate.go:475`; `reviewloop.go:587,1368`; `workloop.go:4904` — **5** |
| `ErrAgentReadyTimeout` | `runlaunch.ErrAgentReadyTimeout` | `agentready.go:202`; `dot_cascade.go:1866`; `dot_gate.go:479`; `reviewloop.go:730,1457`; `workloop.go:4908` — **6** |
| `defaultAgentReadyTimeout` | `runlaunch.DefaultAgentReadyTimeout` | `agentready.go:162` — **1** |
| `defaultRemoteAgentReadyTimeout` | `runlaunch.DefaultRemoteAgentReadyTimeout` | none outside the moved `effectiveAgentReadyTimeout` — **0** |

**Total production call sites: 88.** Per-file: `workloop.go` 42, `reviewloop.go` 21,
`dot_cascade.go` 18, `dot_gate.go` 8, `agentready.go` 2, `bootstate.go` 2, `dispatchsegment.go` 1.
(`workloop.go`'s 42 counts the in-file callers of the twelve symbols it loses, at `:4653 :4700 :4707
:4719 :4770 :4904 :4908 :4922 :4934 :4944 :5106 :5130 :5323 :5435 :5549` and the 8 `artifactAgentType`
sites at `:4358 :4362 :4481 :4516 :4866 :4871 :4875`.)

### 3c. Test surface: 8 files, and 7 of them need **zero** edits

`export_test.go` stays in `internal/daemon` (173 external `daemon_test` files depend on it). Re-point
six shims at the new homes and every consumer keeps compiling — the same transparency trick
`EmitterPort` uses as a type alias.

| `export_test.go` shim | line | becomes |
|---|---|---|
| `ExportedSetAgentReadyKillReapTimeout` | `:661-670` | reads/writes `runlaunch.KillReapTimeout` |
| `ExportedForceTeardownSession` | `:1134-1138` | calls `runlaunch.ForceTeardownSession` |
| `ExportedNoCommitGuardShouldReopen` | `:1181-1184` | **unchanged** (`noCommitGuardShouldReopen` stays) |
| `ExportedDefaultAgentReadyTimeout` | `:1410` | `= runlaunch.DefaultAgentReadyTimeout` |
| `ExportedEmitAgentReadyTimeout` | `:1415` | `= runlaunch.EmitAgentReadyTimeout` |
| `ExportedErrAgentReadyTimeout` | `:1393` | `= runlaunch.ErrAgentReadyTimeout` |
| `ExportedBeadAlreadySubsumedInMain` | `:2316-2321` | calls `shared.MainHistoryHasRefsTrailer` |

Consumers that then need **no edit at all** (verified by grep): `concurrent_remote_agent_ready_hk4hso5_test.go`,
`concurrent_review_loop_sess_wait_hkup1pk_test.go`, `pasteinject_hktrjef_test.go`,
`twinparity_timing_property_test.go`, `workloop_nocommit_guard_hk4ie1z_test.go`,
`workloop_predispatch_subsume_hkf38n_test.go`, `worktree_teardown_hk68pvl_test.go`.

Test files that **do** need work:

- `internal/daemon/clockport_rt1_test.go` (62 LOC, `package daemon`, white-box) — **relocates** to
  `internal/substrate/after_test.go`. It is a FakeClock-drives-the-deadline contract test; the daemon
  package clause is incidental.
- `internal/daemon/agentreadyremote_hk96d7w_test.go` (`package daemon`, white-box; uses
  `defaultAgentReadyTimeout`, `defaultRemoteAgentReadyTimeout`, `effectiveAgentReadyTimeout` at
  `:27-29,:31-32,:53,:60,:81,:88,:95,:103,:105`) — **either** relocate to
  `internal/runlaunch/deadlines_test.go` (preferred; it is entirely a test of those four symbols) **or**
  re-qualify in place. Choose relocate.
- `internal/daemon/agentspawnsem_hk5z1f0_test.go:27-28` (`package daemon`) — asserts
  `defaultAgentReadyTimeout == 150*time.Second`. Re-qualify in place to
  `runlaunch.DefaultAgentReadyTimeout`; do **not** move the file (it also covers the spawn semaphore).
- `internal/daemon/agentready_hkgql2018_test.go:172,175` and `reviewloop_hkkqdpf2_test.go:184` —
  reference `ErrAgentReadyTimeout`. If they are `package daemon`, re-qualify; if `daemon_test`, they
  route through the `ExportedErrAgentReadyTimeout` shim and need no edit. Check with `head -1`.

**No `//go:build scenario`, `integration` or `e2e_real_claude` test file references any of the sixteen
symbols or their shims** — verified by scanning all tagged daemon test files. This slice is blind-spot
free on the tagged tier, unusually for E5 work.

**No `internal/specaudit` change is needed** — verified. The `wminv003` path allowlist keys on
`internal/daemon/workloop.go` for *history-rewriting* git execs (`rebase`, `--amend`, `push --force`).
The only git exec moving is `beadAlreadySubsumedInMain`'s `git log --grep`, which the scanner does not
match. Re-run `go test -tags specaudit ./internal/specaudit/...` anyway (§5) and expect the same
7-failure baseline.

---

## 4. Step-by-step recipe

### Ground rules

- **Single writer.** Every commit here edits `workloop.go` (331 commits/90d) and/or `export_test.go`
  (229). **Do not parallelise across crews, and do not start while RT13 or E4c is uncommitted.**
- **Pure move.** `git mv` / cut-paste + package clause + import fixups. **No emission-string changes,
  no signature "improvements", no gofumpt reflow of untouched code.** The one intentional textual
  change in the whole slice is rejoining the severed `emitAgentReadyTimeout` doc comment (§0), and it
  must be called out in the commit body.
- **Stage by explicit pathspec.** Never `git add -A`, `git commit -a`, `git reset --hard`, or
  `git clean` — PROGRESS.md §"We nearly collided at 08:00" records why. On failure restore only your
  own files with `git checkout HEAD -- <path>`.
- **Never `cd` into a worktree.** Operate from `/Users/gb/github/harmonik` with absolute paths.

### Step 0 — preflight (do not skip)

```bash
cd /Users/gb/github/harmonik
git log --oneline -3                      # RT13 and E4c must be COMMITTED
git status --short -- internal/daemon .golangci.yml Makefile scripts   # must be clean
go build ./internal/... ./cmd/...         # must exit 0
df -h .                                   # must show >10 GiB free (daemon disk watermark; see §5)
wc -l internal/daemon/workloop.go internal/daemon/agentready.go
```

Record the baseline for the close comment and for the differential oracle:

```bash
find internal/daemon -maxdepth 1 -name '*.go' ! -name '*_test.go' | wc -l
find internal/daemon -maxdepth 1 -name '*.go' ! -name '*_test.go' -exec cat {} + | wc -l
```

*Measured 2026-07-22 on the post-RT13 tree: **103 files / 49,064 LOC**, `workloop.go` = 6,854,
`agentready.go` = 207.* If your numbers differ, E4c moved things — re-derive the line numbers in §1a
from the `grep` anchor before cutting anything.

---

### Commit 1 of 3 — RT19b-1: `clockAfter` → `internal/substrate.After`

**1.** Cut `internal/daemon/workloop.go:1215-1233` (doc comment + `func clockAfter`) and append it to
`internal/substrate/clock.go`, renamed and with the doc comment's daemon-specific references kept
verbatim except the name:

```go
// After is the ClockPort-backed analogue of time.After for use in a select:
// it returns a channel that receives once, after d has elapsed on clk. Like
// time.After (and UNLIKE a ctx-bound sleep) the deadline fires UNCONDITIONALLY —
// the reap/fallback guards that use it must bound the wait even after the run
// ctx is cancelled, so the internal Sleep is anchored to context.Background().
// Under FakeClock the wake is driven by Advance, making run-path timeouts
// (agent-ready reap, resume-ready fallback) deterministic in tests
// (RSM-013 / M3-D4). The goroutine outlives the caller by at most d, matching
// time.After's un-cancellable timer. Buffered cap 1 so the send never blocks
// when the select picked another case first.
//
// Origin: internal/daemon/workloop.go clockAfter, moved by
// plans/2026-07-21-p2-extraction/RT19b-stranded-run-path-helpers.md.
func After(clk ClockPort, d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	go func() {
		if clk.Sleep(context.Background(), d) {
			ch <- clk.Now()
		}
	}()
	return ch
}
```

`internal/substrate/clock.go` already imports `context` and `time` (`clock.go:3-6`). No new import.

**2.** Rewrite the 5 call sites to `substrate.After(...)`. All 5 files already import
`internal/substrate` (they pass `substrate.ClockPort` values):

```bash
cd /Users/gb/github/harmonik
grep -n 'clockAfter(' internal/daemon/*.go   # expect: dispatchsegment.go:198, dot_cascade.go:1871,
                                             #         reviewloop.go:735, reviewloop.go:1468, workloop.go:4922
```

Preserve the trailing `//nolint:contextcheck` comments on `reviewloop.go:735`, `:1468` and
`dot_cascade.go:1871` exactly.

**3.** Relocate the test:

```bash
git mv internal/daemon/clockport_rt1_test.go internal/substrate/after_test.go
```

Then in the moved file: `package daemon` → `package substrate_test` (matching
`internal/substrate/substrate_test.go:1`); drop the now-self `substrate` import? No — keep it, an
external test package imports its subject. Replace `clockAfter(fc, agentReadyKillReapTimeout)` with
`substrate.After(fc, reapTimeout)` and add, at the top of the test function:

```go
// reapTimeout mirrors the run path's agent-ready kill-reap bound (10s, HC-056,
// now internal/runlaunch.KillReapTimeout). Restated as a local constant so this
// substrate test stays a stdlib-only leaf — importing runlaunch would violate
// the substrate depguard allow-list (.golangci.yml:483-493).
const reapTimeout = 10 * time.Second
```

Substitute `reapTimeout` at the other two uses (originally `:46` and `:49`). Update the doc comment's
`clockAfter` mentions to `substrate.After`. **This is the only place in the slice where a literal
value is restated; it is forced by substrate's stdlib-only fence and must be stated in the commit
body.**

**4.** Gate + commit:

```bash
go build ./internal/... ./cmd/...
go vet ./internal/...
go test ./internal/substrate/... -count=1
$(go env GOPATH)/bin/golangci-lint run   # or the repo's $(TOOLS_DIR)/golangci-lint
git add internal/substrate internal/daemon/workloop.go internal/daemon/dispatchsegment.go \
        internal/daemon/dot_cascade.go internal/daemon/reviewloop.go
git commit
```

No depguard change and no freeze-gate change in this commit — `substrate`'s existing rule
(`.golangci.yml:483-493`) already denies `internal/daemon` and allows only `$gostd` + self, which is
exactly the fence `After` needs.

---

### Commit 2 of 3 — RT19b-2: `artifactAgentType` + `beadAlreadySubsumedInMain` → `internal/harness/shared`

**5.** Cut `workloop.go:6575-6587` and append to `internal/harness/shared/launchctx.go`, immediately
after the `LaunchArtifacts` struct it reads:

```go
// ArtifactAgentType returns the resolved agent type from LaunchArtifacts,
// falling back to core.AgentTypeClaudeCode when the field is empty (e.g. from a
// legacy test fixture that builds artifacts directly without going through
// routedLaunchSpecBuilder).
//
// Used to look up the correct Adapter via adapterRegistry.ForAgent instead of
// hardcoding core.AgentTypeClaudeCode (T12, hk-xhawy).
//
// Origin: internal/daemon/workloop.go artifactAgentType.
func ArtifactAgentType(a LaunchArtifacts) core.AgentType {
	if a.ResolvedAgentType.Valid() {
		return a.ResolvedAgentType
	}
	return core.AgentTypeClaudeCode
}
```

**Keep it a free function.** A method (`a.AgentType()`) would read better but is a signature change,
which `_plan.md` §5.1 rejects inside an extraction. Note the method form as a follow-up in the commit
body; do not do it here.

**6. Heal the severed doc comment** (§0, last row). After the cut, `workloop.go` reads:

```
// emitAgentReadyTimeout emits an agent_ready_timeout event (hk-5cox8) when
// the HC-056 timeout fires — no agent_ready relay message arrived within the
// configured deadline. The event carries run_id, claude_session_id, and
// timeout_ms so post-hoc analysis can correlate which runs never became ready.
```

i.e. old `:6572-6574` now joins directly to old `:6589`. Confirm the sentence reads as one. **This is
the only non-mechanical text change in the slice — call it out in the commit body.**

**7.** Rewrite the 24 `artifactAgentType(` call sites to `shared.ArtifactAgentType(`:

```bash
grep -n 'artifactAgentType(' internal/daemon/*.go   # expect 24 across dot_cascade, dot_gate, reviewloop, workloop
```

All four files already import `internal/harness/shared` (they construct `shared.LaunchCtx` /
`shared.LaunchArtifacts`) — confirm with `grep -n 'harness/shared' internal/daemon/{workloop,reviewloop,dot_cascade,dot_gate}.go`
and add the import only where absent. Also update the prose mention at `internal/daemon/sandboxgate.go:12`.

**8.** Cut `workloop.go:5438-5470` into `internal/harness/shared/refstrailer.go`, rewritten to reuse the
two primitives that package already owns:

```go
// MainHistoryHasRefsTrailer reports whether beadID appears as an exact
// "Refs: <id>" trailer line in any commit on `main` in projectDir.
//
// This is the daemon's post-run subsumption check: after a noChange-timeout kill
// it decides whether the work was already completed by a prior run that merged
// to main — in which case the bead is closed, not reopened.
//
// hk-ly0hg: it uses `--grep` across the FULL main history rather than a fixed
// -20 window, so a restart-interrupted run whose commit landed >20 commits ago is
// still found. --fixed-strings prevents regex interpretation of bead IDs, and the
// line-exact comparison (ContainsExactLine) prevents "Refs: hk-foo.1" from
// matching a commit carrying "Refs: hk-foo.10".
//
// Returns false on any git error (conservative: treat as not subsumed).
//
// UNLIKE its neighbours in this file it does NOT route through a
// tmux.CommandRunner: it probes the LOCAL project checkout's main history, never
// a remote worker's. Do not "fix" that asymmetry — it is the behaviour the
// daemon's noChange path depends on.
//
// Bead: hk-trjef, hk-ly0hg. Origin: internal/daemon/workloop.go
// beadAlreadySubsumedInMain.
func MainHistoryHasRefsTrailer(ctx context.Context, projectDir string, beadID core.BeadID) bool {
	needle := RefsTrailerLine(beadID)
	cmd := exec.CommandContext(ctx, "git", "log", "main", "--format=%B",
		"--fixed-strings", "--grep", needle)
	cmd.Dir = projectDir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return ContainsExactLine(string(out), needle)
}
```

**Verify the two substitutions are byte-equivalent before accepting them** — this is the one place in
RT19b where the diff is not literally the same code:

- `RefsTrailerLine(beadID)` must equal `"Refs: " + string(beadID)`. Check
  `internal/harness/shared/refstrailer.go:94-96`.
- `ContainsExactLine(body, line)` must be the same `strings.Split(body, "\n")` +
  `strings.TrimRight(line, "\r") == needle` loop. Check `refstrailer.go:241`.

If either differs in any respect, **abandon the consolidation and move the function verbatim** with
its own inline loop. A pure move beats a tidy one.

**9.** Rewrite the 4 call sites (`dot_cascade.go:2063`, `workloop.go:5323`, `:5435`, `:5549`) to
`shared.MainHistoryHasRefsTrailer(...)`, and re-point `export_test.go:2316-2321`.

**10.** Gate + commit:

```bash
go build ./internal/... ./cmd/...
go vet ./internal/...
go test ./internal/harness/... -count=1
go list -deps ./internal/harness/shared | grep internal/daemon   # MUST be empty
$(TOOLS_DIR)/golangci-lint run
scripts/harnesscodex-freeze-gate.sh && scripts/harnessclaude-freeze-gate.sh && scripts/harnesspi-freeze-gate.sh
git add internal/harness/shared internal/daemon/{workloop.go,reviewloop.go,dot_cascade.go,dot_gate.go,sandboxgate.go,export_test.go}
git commit
```

---

### Commit 3 of 3 — RT19b-3: the launch effects → new leaf `internal/runlaunch`

**11. PRE-ARM THE FENCE FIRST** (the E1a-0 pattern: the brake exists before the mass arrives). Append
this block to `/Users/gb/github/harmonik/.golangci.yml` **immediately after the `runmerge:` block**
(which starts at `.golangci.yml:905` and ends at `:918` on the RT13 tree — re-locate with
`grep -n 'runmerge:' .golangci.yml`), with the same 8-space key indentation:

```yaml
        # runlaunch: the effects a bead run performs AROUND an agent launch,
        # stranded in workloop.go outside both E5 extraction regions and drained
        # here (P2 unit E5, RT19b). Four families, one concern — everything the
        # run path does at launch/ready time that is not the launch itself:
        #   - the CHB-018 pre-exec announcement relay (EmitPreExecMessage /
        #     EmitPreExecBeforeLaunch), which holds launch_initiated back until
        #     SpawnWindow returns (hk-4l7zs);
        #   - the HC-056 readiness deadlines (DefaultAgentReadyTimeout 150s /
        #     DefaultRemoteAgentReadyTimeout 210s / EffectiveAgentReadyTimeout /
        #     ErrAgentReadyTimeout / KillReapTimeout), which populate
        #     runexec.DispatchConfig.ReadyTimeout and .ReadyKillReap;
        #   - the launch/ready anomaly events (EmitSpawnCapBlocked hk-4l7zs,
        #     EmitTmuxNewWindowTimeout hk-r1rup, EmitAgentReadyTimeout hk-5cox8)
        #     and EmitImplementerPhaseComplete (hk-cd8yu);
        #   - ForceTeardownSession, the hk-68pvl kill-before-worktree-removal
        #     backstop.
        # It is the EFFECT vocabulary paired with internal/runexec's PURE Dispatch
        # machine, which models the same launch -> ready -> working segment.
        # It MUST be a leaf and not a runloop-private file: internal/daemon/
        # bootstate.go:275,:278 calls EmitSpawnCapBlocked / EmitTmuxNewWindowTimeout
        # from the BOOT path, and bootstate.go never moves.
        # internal/handler is required for handler.Session (ForceTeardownSession);
        # handlercontract for EventEmitter; core for the payload types.
        # Rationale: plans/2026-07-21-p2-extraction/RT19b-stranded-run-path-helpers.md.
        runlaunch:
          files: ["**/internal/runlaunch/**"]
          allow:
            - "$gostd"
            - "github.com/gregberns/harmonik/internal/core"
            - "github.com/gregberns/harmonik/internal/handler"
            - "github.com/gregberns/harmonik/internal/handlercontract"
            - "github.com/gregberns/harmonik/internal/runlaunch"
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "runlaunch is a leaf: the run path's launch-time effects must not re-couple to the monolith, and bootstate.go depends on them too (P2 E5 RT19b)" }
```

Run `$(TOOLS_DIR)/golangci-lint run` now. It must be green and must match nothing (the directory does
not exist yet). **Widen the allow-list only on proof** — do not pre-allow `substrate`, `workspace`,
`lifecycle/tmux` or anything else "in case"; PROGRESS.md punch item 9 records speculative allows as a
verifier finding against E1a.

**12.** Create the package and its doc file:

```bash
mkdir -p /Users/gb/github/harmonik/internal/runlaunch
```

`internal/runlaunch/doc.go` — the charter sentence from §2, plus the origin line
(`internal/daemon/workloop.go` and `internal/daemon/agentready.go`, moved by this plan) and the
"pairs with internal/runexec's pure Dispatch machine" note.

**13.** Move the deadline family into `internal/runlaunch/deadlines.go`:

| From | LOC | To |
|---|---|---|
| `workloop.go:136-155` `agentReadyKillReapTimeout` | 20 | `KillReapTimeout` (**stays a `var`** — `export_test.go:661-670` overrides it) |
| `agentready.go:42-64` `defaultAgentReadyTimeout` | 23 | `DefaultAgentReadyTimeout` (`const`) |
| `agentready.go:66-80` `defaultRemoteAgentReadyTimeout` | 15 | `DefaultRemoteAgentReadyTimeout` (`const`) |
| `agentready.go:82-105` `effectiveAgentReadyTimeout` | 24 | `EffectiveAgentReadyTimeout` |
| `agentready.go:107-116` `ErrAgentReadyTimeout` | 10 | `ErrAgentReadyTimeout` |

Imports needed: `errors`, `time`. Doc comments move **verbatim** — they carry the hk-5z1f0 / hk-96d7w /
hk-do7te / hk-4hso5 tuning history and the HC-056 spec refs, and that history is the reason the values
are what they are.

**14.** Move the launch effects into `internal/runlaunch/events.go`
(`workloop.go:5775-5833` and `:6480-6570` and `:6572-6574`+`:6589-6607`) and
`internal/runlaunch/teardown.go` (`workloop.go:5742-5773`). Export per §3b; keep `preExecMsgType`
unexported. Imports: `context`, `encoding/json`, `time`, `core`, `handlercontract` (events);
`context`, `handler` (teardown).

`EmitAgentReadyTimeout`'s in-body reference to `defaultAgentReadyTimeout` becomes the now-in-package
`DefaultAgentReadyTimeout` — an unqualified reference, not a cross-package one.

**15.** Rewrite the 42 remaining production call sites (§3b), adding
`"github.com/gregberns/harmonik/internal/runlaunch"` to the import block of `workloop.go`,
`reviewloop.go`, `dot_cascade.go`, `dot_gate.go`, `bootstate.go` and `agentready.go`. `agentready.go`
needs it for `:104`→(deleted, the resolver moved), `:162` (`DefaultAgentReadyTimeout`) and `:202`
(`ErrAgentReadyTimeout`).

**16.** Re-point the 5 `export_test.go` shims (§3c) and relocate
`internal/daemon/agentreadyremote_hk96d7w_test.go` → `internal/runlaunch/deadlines_test.go`
(`package runlaunch`, in-package — it asserts on unexported-then/exported-now scalars and needs no
daemon symbol). Re-qualify `agentspawnsem_hk5z1f0_test.go:27-28` in place.

**17.** Write `/Users/gb/github/harmonik/scripts/runlaunch-freeze-gate.sh`, modelled on
`scripts/runmerge-freeze-gate.sh`. **Note the deliberate deviation:** the runmerge gate leads with a
file-name scan; that pattern is nearly useless here because `internal/daemon` legitimately keeps ~25
other `emit*` functions (`emitRunStarted`, `emitRunCompleted`, `emitPostAgentReadyHang`,
`emitReviewerVerdict`, …) and a `*events*.go`-shaped scan would be a false-positive machine. This gate
is therefore **symbol-anchored**, with only a narrow name check for names that could not mean anything
else.

```bash
#!/usr/bin/env bash
set -euo pipefail

# runlaunch-freeze-gate.sh — P2 unit E5 RT19b freeze tripwire
# (plans/2026-07-21-p2-extraction/RT19b-stranded-run-path-helpers.md §4 step 17;
#  _plan.md §3.2).
#
# Sixteen symbols that were stranded in internal/daemon/workloop.go and
# internal/daemon/agentready.go — the CHB-018 pre-exec relay, the HC-056
# readiness deadlines, the launch/ready anomaly events, the phase-complete
# report, the force-teardown backstop, the ClockPort After helper, the
# LaunchArtifacts agent-type accessor and the Refs-trailer main-history probe —
# now live in internal/runlaunch, internal/substrate and internal/harness/shared.
# depguard fences the IMPORT edge; it cannot forbid RE-DECLARING a symbol, so
# this ratchet closes the other door.
#
# DELIBERATE DEVIATION from the runmerge/transport gates: no broad file-name
# scan. internal/daemon legitimately retains ~25 other emit* functions
# (emitRunStarted, emitRunCompleted, emitPostAgentReadyHang, emitReviewerVerdict
# and friends) which are E5 LIFT targets, not RT19b targets. A '*events*.go'
# scan here would fire on correct code. Symbols are the precise contract.
#
# Exit 0: clean. Exit 1: the door was reopened.

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

HITS=0

# (1) No re-declaration of the moved funcs/vars/consts anywhere under
#     internal/daemon (recursive — a sub-package is the obvious evasion of a
#     -maxdepth 1 scan). The alternation also catches a re-declaration inside a
#     grouped `var ( … )` / `const ( … )` block, which a bare ^(func|var|const)
#     anchor misses. The trailing grep -v drops legitimate forwarding
#     assignments to the extracted packages (export_test.go shims).
for sym in clockAfter After \
           agentReadyKillReapTimeout KillReapTimeout \
           defaultAgentReadyTimeout DefaultAgentReadyTimeout \
           defaultRemoteAgentReadyTimeout DefaultRemoteAgentReadyTimeout \
           effectiveAgentReadyTimeout EffectiveAgentReadyTimeout \
           ErrAgentReadyTimeout \
           beadAlreadySubsumedInMain MainHistoryHasRefsTrailer \
           forceTeardownSession ForceTeardownSession \
           emitPreExecMessage EmitPreExecMessage \
           preExecMsgType \
           emitPreExecBeforeLaunch EmitPreExecBeforeLaunch \
           emitImplementerPhaseComplete EmitImplementerPhaseComplete \
           emitSpawnCapBlocked EmitSpawnCapBlocked \
           emitTmuxNewWindowTimeout EmitTmuxNewWindowTimeout \
           emitAgentReadyTimeout EmitAgentReadyTimeout \
           artifactAgentType ArtifactAgentType; do
    MATCHES="$(grep -rn --include='*.go' --exclude='*_test.go' -E \
        "^[[:space:]]*(func|var|const)?[[:space:]]*${sym}\b[[:space:]]*(=|struct|interface|func|\()" \
        internal/daemon 2>/dev/null \
      | grep -vE '=[[:space:]]*(shared|claude|codex|pi|crewrun|queuewiring|tunnel|codesync|gitprobe|runmerge|runlaunch|substrate)\.' || true)"
    if [ -n "$MATCHES" ]; then
        echo "runlaunch-freeze-gate: FORBIDDEN re-declaration of ${sym} in internal/daemon:" >&2
        printf '%s\n' "$MATCHES" >&2
        HITS=$((HITS + 1))
    fi
done

# (2) Narrow file-name check — only names that cannot mean anything but the
#     extracted concern. Deliberately NOT '*events*.go' (see header).
while IFS= read -r f; do
    echo "runlaunch-freeze-gate: FORBIDDEN launch-effects file in internal/daemon: $f" >&2
    HITS=$((HITS + 1))
done < <(find internal/daemon -type f \
             \( -name '*preexec*.go' -o -name '*spawncap*.go' \
                -o -name '*agentreadytimeout*.go' -o -name '*forceteardown*.go' \
                -o -name '*clockafter*.go' -o -name '*runlaunch*.go' \) \
             ! -name '*_test.go')

# (3) The run path must not re-acquire a raw time.After for the ready/reap
#     bound. Under RT14 the last wall-clock sites in dot_gate.go / waitsocketgrace.go
#     / postreadyhang.go also close; until then those three are carved out.
MATCHES="$(grep -rn --include='*.go' --exclude='*_test.go' -E 'time\.After\((agentReadyKillReapTimeout|runlaunch\.KillReapTimeout)\)' internal/daemon 2>/dev/null || true)"
if [ -n "$MATCHES" ]; then
    echo "runlaunch-freeze-gate: FORBIDDEN wall-clock time.After on the reap bound — use substrate.After(clk, runlaunch.KillReapTimeout):" >&2
    printf '%s\n' "$MATCHES" >&2
    HITS=$((HITS + 1))
fi

# (4) The destinations must still exist. A gate whose targets have been renamed
#     away silently stops testing what it claims to test.
for f in internal/runlaunch/events.go internal/runlaunch/deadlines.go \
         internal/runlaunch/teardown.go internal/substrate/clock.go \
         internal/harness/shared/refstrailer.go internal/harness/shared/launchctx.go; do
    if [ ! -f "$f" ]; then
        echo "runlaunch-freeze-gate: destination $f is gone — re-derive this gate" >&2
        HITS=$((HITS + 1))
    fi
done

if [ "$HITS" -ne 0 ]; then
    echo "runlaunch-freeze-gate: FAIL — the run path's launch-time effects were extracted in P2 E5 RT19b; build on internal/runlaunch, do not reopen internal/daemon" >&2
    exit 1
fi
echo "runlaunch-freeze-gate: OK — the launch-time effects stay out of internal/daemon"
```

```bash
chmod +x /Users/gb/github/harmonik/scripts/runlaunch-freeze-gate.sh
```

**18.** Wire it into the `Makefile`. Add a `.PHONY` target after the `runmerge-freeze-gate` target
(`Makefile:304-316`):

```make
# runlaunch-freeze-gate: the P2 E5 RT19b extraction ratchet — the run path's
# launch-time effects (pre-exec relay, HC-056 deadlines, spawn/ready anomaly
# events, force teardown) left internal/daemon for internal/runlaunch,
# internal/substrate and internal/harness/shared. Symbol-anchored, not
# name-anchored: internal/daemon legitimately keeps other emit* helpers.
.PHONY: runlaunch-freeze-gate
runlaunch-freeze-gate:  ## P2 E5 RT19b: forbid re-declaring the moved launch-effect symbols in internal/daemon
	scripts/runlaunch-freeze-gate.sh
```

and add `scripts/runlaunch-freeze-gate.sh` to **both** `check-fast` (after `Makefile:508`
`scripts/runmerge-freeze-gate.sh`) and `check-short` (after the matching line ~`:531`). Hard CI
failure is the operator's confirmed decision (PROGRESS.md §6.2).

**19.** Gate + commit (full §5 gate).

---

## 5. Verification gate

Run from `/Users/gb/github/harmonik`, **serialized** — not concurrent with an agent fan-out or a
parallel build. That is what manufactured five of the six failures in the measured baseline
(`00-test-oracle-baseline.md`).

| # | Command | Pass criterion |
|---|---|---|
| 0 | `df -h .` | **>10 GiB free.** Below the daemon's own 10,240 MiB watermark it logs `disk-check: … dispatch paused` and *manufactures* dispatch/throughput/timing failures unrelated to any code change (`00-test-oracle-baseline.md` Addendum §3). Measured 2026-07-22: 22 GiB free — currently OK. **Re-check before the test run, not just before the edits.** |
| 1 | `go build ./internal/... ./cmd/...` | exit 0. **Scoped deliberately** — `go build ./...` is RED at repo root regardless of this slice (`plans/2026-07-15-agent-substrate-v2/investigate/10-zeromq-experiments/*.go` import unvendored zmq4/mangos). Never make an unscoped root build a gate. |
| 2 | `go vet ./internal/...` | exit 0 |
| 3 | `go vet -tags=scenario ./internal/daemon/` **and** `go vet -tags=integration ./internal/daemon/` **and** `go vet -tags=e2e_real_claude ./internal/daemon/` | exit 0 each. 28 daemon test files are behind `//go:build scenario` and plain `go test` never compiles them. **RT19b was verified to reference none of the sixteen symbols from a tagged file — so this gate should be a formality here. If it is not, something else moved.** |
| 4 | `go list -deps ./internal/runlaunch \| grep internal/daemon` | **empty.** Same for `./internal/substrate` and `./internal/harness/shared`. This is the boundary test (`_plan.md` §3.4) — scope disputes are settled here, not by argument. |
| 5 | `go test ./internal/runlaunch/... ./internal/substrate/... ./internal/harness/... -count=1` | exit 0. The newly-populated packages must be green on their own. |
| 6 | `go test -tags specaudit ./internal/specaudit/... -count=1` | **Differential**, not zero — specaudit is already red at HEAD with **7 top-level failures**. The after-set must contain no new name. RT19b is expected to change nothing here (§3c); a new failure means a path-pinned sensor was hit and must be updated **in this commit**. |
| 7 | `go test ./internal/daemon/... -count=1 -timeout 25m` | **Differential green.** Capture `--- FAIL` names before and after; `comm -13 before-failures.txt after-failures.txt` must be EMPTY. **Use the IDENTICAL package scope for both runs** — PROGRESS.md §Phase 5 records a control run invalidated by scope mismatch, because total-suite scope drives temp-dir churn which drives the disk watermark which drives dispatch failures. Run both back-to-back on a **clean detached worktree at your own HEAD**, never the shared working tree (another agent's dirty files compile into it). |
| 8 | `go test ./internal/runexectest/... -count=10` | exit 0. The RT11 fault-matrix + N=10 relaunch oracle is the behavior-preservation gate for the run path. `-count=1` is not sufficient. |
| 9 | `go test ./internal/replay/... -count=1` | exit 0. The RT10 run-keyed replay checkers detect event-stream divergence — i.e. a "pure move" that silently reordered or renamed an emission. **Load-bearing for this slice specifically: seven of the sixteen symbols emit bus events.** |
| 10 | `$(TOOLS_DIR)/golangci-lint run` | exit 0, depguard green. If `runlaunch`'s allow-list is missing `internal/handler` this is where it fails. |
| 11 | `bash scripts/runlaunch-freeze-gate.sh` | prints `runlaunch-freeze-gate: OK` |
| 12 | all six pre-existing gates: `scripts/{transport,queuewiring,crewrun,harnesscodex,harnessclaude,harnesspi,runmerge}-freeze-gate.sh` | each exits 0 — RT19b adds symbols to `harness/shared`, so re-run the three harness gates in particular |
| 13 | `ubs $(git diff --name-only HEAD)` | exit 0 |
| 14 | `agent-reviewer` verdict on each of the three commits | APPROVE, with `is_pure_move: true` |
| 15 | measurement (below) | numbers published in the close comment |

**Known-flaky allowlist — re-run before calling any of these a regression** (`00-test-oracle-baseline.md`):
`TestThroughput_TenBeadsAtMaxFour` (hard pre-existing), `TestScenario_Hk6ynv4_SubscribeStream_EndToEnd`,
`TestStopHookE2E_TwinRelayFastPath`, `TestStopHookE2E_TwinRelayWaitGrace`,
`TestPasteInjectQuitOnCommit_PostQuitWatchdogKillsOnGrace`, `TestT6_10BeadSequentialDrain` —
**confirm these IN ISOLATION** (isolated pass ⇒ load artifact).
`TestMergeToMain_RealConflictWithBeadsLedger_Escalates` — **confirm this IN THE FULL SUITE**
(it is isolation-sensitive, the mirror image of the others; applying the wrong procedure inverts the
answer).

**Measurement, for the close comment:**

```bash
cd /Users/gb/github/harmonik
find internal/daemon -maxdepth 1 -name '*.go' ! -name '*_test.go' | wc -l
find internal/daemon -maxdepth 1 -name '*.go' ! -name '*_test.go' -exec cat {} + | wc -l
wc -l internal/daemon/workloop.go internal/daemon/agentready.go
```

| Metric | Before (post-RT13) | Expected after | Delta |
|---|---|---|---|
| `internal/daemon` top-level non-test files | 103 | 103 | 0 (no whole file leaves) |
| `internal/daemon` top-level non-test LOC | 49,064 | ~48,707 | **−357** |
| `workloop.go` | 6,854 | ~6,569 | −285 |
| `agentready.go` | 207 | ~135 | −72 |
| daemon test files | — | −2 | `clockport_rt1_test.go`, `agentreadyremote_hk96d7w_test.go` |
| new `internal/runlaunch` | — | ~240 non-test LOC | new leaf, depguard-fenced + grep-gated |

**The close comment MUST also state**, per E5 §7.11: *the `_plan.md` §5.4 runtime proof is DEFERRED —
the daemon is DOWN, so no bead was driven end-to-end. The mechanical substitutes (`internal/runexectest`
N=10, `internal/replay`) were run and are green.* Do not omit this; flag it.

**Also update on close:** `E5-dot-runloop.md` §3a's "third band" row and §7 item 15 → RESOLVED for the
eleven (the remaining back-edges in item 15 — the four `sessioncontext_chb023.go` calls,
`standardgraph`/`modelpreference`/`moderesolve`/`harnessresolve`/`sandboxprofile`, `ProjectConfig`,
`*RunRegistry`, the 5 `dot_cascade → reviewloop` symbols — are **not** touched here and stay open).
And `PROGRESS.md`'s slice table.

---

## 6. Merge-conflict exposure

**This is the slice's only real risk, and the mitigation is calendar time, not cleverness.**

90-day commit counts on every file RT19b edits, measured 2026-07-22:

| File | commits/90d | RT19b sites |
|---|---:|---:|
| `internal/daemon/workloop.go` | **331** | 42 |
| `internal/daemon/export_test.go` | **229** | 6 |
| `internal/daemon/dot_cascade.go` | **104** | 18 |
| `internal/daemon/reviewloop.go` | **98** | 21 |
| `internal/daemon/dot_gate.go` | 21 | 8 |
| `internal/daemon/bootstate.go` | 5 | 2 |
| `internal/daemon/agentready.go` | 4 | 2 |
| `internal/daemon/dispatchsegment.go` | 2 | 1 |
| `internal/substrate/clock.go` | 1 | +19 LOC |
| `internal/harness/shared/refstrailer.go` | 1 | +25 LOC |

**Window: land all three commits in ONE sitting — target under 4 hours, hard stop at one working
day.** At 331 commits/90d, `workloop.go` changes roughly every 6.5 hours of project wall time; an
overnight-open RT19b branch is a guaranteed rebase across 42 substitution points.

**What must NOT run concurrently:**

- **Any other E5 slice.** RT14 (dot_gate wall-clock conversion + `agentready.go` deletion) collides
  head-on: it edits `dot_gate.go:486`, the same line RT19b re-qualifies, and it deletes the file RT19b
  cuts four symbols out of. **Land RT19b first, then RT14** — RT19b makes RT14's step 17 a clean
  whole-file delete.
- **RT13 and E4c.** Both are in flight now. RT13 rewrites the `workloop.go` tail; E4c edits
  `workloop.go:1127-1135`, which is 88 lines from `clockAfter` at `:1215`. **Both must be committed
  before step 0.**
- **The E5 lift (RT20+).** Obviously — it moves the four files RT19b edits.
- **Any `.golangci.yml` edit by another agent.** PROGRESS.md §3 lists it as the P2 stream's most
  collision-prone file; every slice appends a block there.
- **The quality audit's recommendation 5** (replacing `beadRunOne`'s `//nolint:gocognit,cyclop,funlen`
  at `workloop.go:3175`). Defer it past RT19b.

**Ordering inside the slice is deliberate: cheapest first.** Commit 1 touches 5 sites in low-churn
files; commit 2 touches 28; commit 3 touches 55 plus the new package and the CI wiring. If the window
closes early, commits 1 and 2 stand alone as complete, releasable, net-positive units — each removes
real back-edges behind a fence that already exists. Only commit 3 creates a package, and it is
self-contained.

**Rollback.** Each commit is a pure move on its own bead branch; `git revert <sha>` restores the
`workloop.go` line count, the `export_test.go` shims, and (for commit 3) the `.golangci.yml` block and
Makefile lines atomically. Nothing depends on `internal/runlaunch` except `internal/daemon`, which
calls it and is never called by it, so a revert cannot orphan a caller. **Verify a revert with the §5
gate, not by eye.**

---

## 7. Risks

1. **88 substitution points across four of the hottest files in the tree.** The compiler catches a
   missed rename, but not a rename applied to the wrong identifier — `emitSpawnCapBlocked` and
   `emitTmuxNewWindowTimeout` sit on adjacent lines at three separate call sites
   (`reviewloop.go:619/625`, `:1392/1398`, `dot_cascade.go:1809/1812`, `workloop.go:4700/4707`).
   **Mitigation:** rewrite one symbol at a time, `go build` after each, and diff the final result
   with `git diff --word-diff` to confirm every changed token is a package qualifier.

2. **`beadAlreadySubsumedInMain`'s consolidation onto `RefsTrailerLine` / `ContainsExactLine` is the
   one non-verbatim edit in the slice.** If those two helpers differ from the inline code in any
   respect — a `\r` handling difference, a trailing-newline difference — the subsumption check
   silently changes meaning, and its failure mode is *closing a bead whose work never landed*.
   **Mitigation:** §4 step 8 requires reading both helpers and confirming byte-equivalence before
   accepting, with an explicit instruction to fall back to a verbatim move if they differ at all.

3. **Seven of the sixteen symbols emit bus events, and a "pure move" that reorders or renames an
   emission is invisible to the compiler.** `emitPreExecBeforeLaunch`'s whole purpose is *ordering* —
   it holds `launch_initiated` back until after `SpawnWindow` returns (hk-4l7zs). Getting that
   backwards restores the bug it was written to fix.
   **Mitigation:** §5 step 9 makes `internal/replay` (the RT10 run-keyed event-stream checkers)
   mandatory, and the pure-move rule forbids touching any emission string or `core.EventType`
   constant.

4. **`KillReapTimeout` stays a mutable exported package `var`.** `export_test.go:661-670` overrides it
   and restores it; after the move that is a cross-package write. It is exactly as safe (and as ugly)
   as today, but it is now *visible* — a reviewer may ask for it to become a config field.
   **Mitigation:** it does not become a config field in this slice. That is a signature change and
   belongs to RT15/RT17, where `runexec.DispatchConfig.ReadyKillReap` already exists as its home.
   Say so in the commit body.

5. **`internal/runlaunch` could become a dumping ground.** Its charter covers exactly four families
   (§2). The next agent with a homeless run-path helper will reach for it.
   **Mitigation:** the depguard comment states the charter in full and names *why* each allowed import
   is there; §1d lists by name the ~25 daemon `emit*` functions that must NOT come here. The
   `_plan.md` §3.3 core-addition rule ("a named type-family in the header + review sign-off") should
   be applied to this package by analogy.

6. **The HC-056 family drag (#13–#16) is scope beyond the eleven, and a reviewer may read it as
   creep.** It is not optional for `emitAgentReadyTimeout` (§1c) and it drains four more back-edges.
   **Mitigation:** §1c states the reasoning, names the rejected alternative and its cost, and gives an
   explicit fallback (move only `defaultAgentReadyTimeout`) if the slice must be cut short. Put the
   §1c table in the commit body so the reviewer does not have to re-derive it.

7. **A `substrate_test` cannot import `runlaunch`**, so the relocated clock test restates the 10 s reap
   bound as a local constant instead of referencing the real one. That is a (tiny) duplication with a
   drift risk: if the reap bound is retuned, the substrate test keeps testing 10 s.
   **Mitigation:** the test is about `After`'s *contract* (fires at exactly d, not before), not about
   the specific bound. The comment added in §4 step 3 says so. Do **not** widen substrate's allow-list
   to fix this — a stdlib-only leaf is worth more than a shared constant.

8. **The freeze gate deviates from the established template** by dropping the broad file-name scan.
   A reviewer comparing it to `runmerge-freeze-gate.sh` will notice.
   **Mitigation:** the deviation is documented in the script's own header with the reason
   (~25 legitimate daemon `emit*` files would false-positive), and the symbol list is strictly more
   precise than a name scan. Steps (2)–(4) still provide a narrow name check, a wall-clock-regression
   check and a destinations-still-exist check.

9. **RT19b's line numbers were measured mid-flight** — RT13 is applied in the working tree but not yet
   committed, and E4c is executing against `workloop.go:1127-1135`.
   **Mitigation:** §0 flags every stale E5 number, §1a gives a `grep` anchor for every symbol, and
   §4 step 0 requires re-deriving from the anchor if the measured file lengths (`workloop.go` 6,854 /
   `agentready.go` 207 / 103 files / 49,064 LOC) do not reproduce.

10. **The `_plan.md` §5.4 runtime proof cannot be run — the daemon is DOWN.** Seven of these symbols
    are on the live launch path.
    **Mitigation:** `internal/runexectest` at `-count=10` and `internal/replay` are the mandatory
    mechanical substitutes (§5 steps 8–9), and the close comment must flag the deferred proof rather
    than quietly omit it (E5 §7.11).
