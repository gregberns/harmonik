# Keeper Architecture Critique — Design Soundness Lens

**Critic:** architecture (separation of concerns, layering, boundaries, control flow, Go↔tmux boundary)
**Date:** 2026-06-20
**Scope read:** `internal/keeper/{keeper,watcher,cycle,gauge,gates,heartbeat,injector,respawn,sessionid,thresholds,tmuxresolve}.go`; CLI `cmd/harmonik/{keeper_cmd,keeper_enable_doctor_cmd}.go`; prior investigation `plans/2026-06-20-keeper-investigation-recovery/README.md`.

---

## Verdict

**The keeper is not a "pile of files with tangled responsibilities" — the files are individually well-factored, well-documented, and use dependency injection cleanly. The architectural problem is at a higher level: the design has no single state machine and no single source of truth. Coordination is smeared across ~9 on-disk files written by 4+ independent processes, and the control flow is a flat, ~30-branch tick loop guarding a 7-step destructive side-effecting cycle. The design is *over-elaborated*, not *unstructured* — and the over-elaboration is the direct cause of the recurring failures.**

---

## 1. Module map — responsibilities and dependencies

The package is internally clean. Each file has a coherent job:

| File | Responsibility | Verdict |
|---|---|---|
| `keeper.go` | Lockfile + `.managed` marker + session-id binding read/write | Cohesive |
| `gauge.go` | `.ctx` read/parse (`CtxFile`) | Cohesive; but see §2 (folds `.sid` override in) |
| `sessionid.go` | `.sid` channel read + UUIDv4/v7/uppercase classifiers | Cohesive |
| `thresholds.go` | Single source of truth for the warn/act/force band | **Exemplary** — explicitly the one place the formula lives (`minAbsOrPctCeil`) |
| `gates.go` | `.idle`/`.dispatching`/`.precompact`/`.restart-now` markers + `CrispIdle`/`HoldingDispatch`/`IsSleeping` predicates | Cohesive (grab-bag named "gates" but reasonable) |
| `injector.go` | tmux bracketed-paste delivery (`InjectText`, `SendEscapeKey`, `SetTmuxEnv`) | **The side-effect boundary — see §3. Well-isolated.** |
| `respawn.go` | tmux pane liveness probes + `NewLiveRecoverViaRespawn` | Cohesive |
| `tmuxresolve.go` | session-name derivation + `OperatorAttached` | Cohesive |
| `heartbeat.go` | gauge re-write from transcript JSONL (hk-81wk) | Cohesive but a workaround-on-workaround (§4) |
| `watcher.go` | the tick loop + warn state machine + respawn/recover orchestration | **Overloaded — see §5** |
| `cycle.go` | the 7-step reset cycle + 3 entry points + anti-loop + boot-grace | **Most complex single unit — see §5** |

Dependency direction is correct and acyclic: `cmd/` → `keeper.{Watcher,Cycler}` → leaf files. `tmuxresolve.go`'s comment (lines 18–20) shows the depguard contract is respected (keeper imports only stdlib/core/eventbus/self; it hand-rolls `HarmonikSessionName` to avoid importing `lifecycle`). DI is pervasive and disciplined: **27 injectable `…Fn func(...)` config fields** across `WatcherConfig` and `CyclerConfig`, every one defaulted in `applyDefaults`. This is the part that *is* good architecture.

**So the per-file critique is not where the problem lives. The problems are structural, below.**

---

## 2. Source of truth for keeper state — SPLIT, and that is the core flaw

There is **no single source of truth**. Keeper liveness/identity/decision state is reconstructed every tick from up to **nine** on-disk files under `.harmonik/keeper/<agent>.*`:

`.lock` · `.managed` · `.sid` · `.ctx` · `.idle` · `.dispatching` · `.precompact` · `.restart-now` · `.cycle` (journal) — plus `HANDOFF-<agent>.md` at repo root and `.harmonik/.sleeping.<sessionID>`.

These are written by **at least four independent writers**:
- `keeper-statusline.sh` writes `.ctx` (only on UI repaint, skips on NA pct).
- `keeper-sessionstart-hook.sh` writes `.sid`.
- `keeper-precompact-hook.sh` writes `.precompact`.
- the Go keeper itself writes `.managed`, `.cycle`, `.ctx` (heartbeat), and reads everything.
- the orchestrator CLI writes `.dispatching` / `.restart-now`.

**Identity alone is resolved from three of these with a precedence ladder.** `ReadCtxFile` (gauge.go:49-61) reads `.ctx` for pct/tokens but then *overrides* `SessionID` from `.sid` when `isPrimarySID`. The watcher (watcher.go:681-721) then runs a *further* reconciliation: compare `.managed` vs gauge `SessionID`, and on mismatch re-read `.sid` again to decide adopt-vs-foreign. That is the same identity value consulted in three places with three different trust rules. The whole `hk-igt → hk-mejt → hk-mzdm → hk-lap → hk-0tvm → hk-3391 → hk-8prq → hk-1tn2` bead chain in the comments is the archaeology of this one decision being re-litigated repeatedly. The `.sid` single-writer channel (hk-8prq) was the right fix — but it was *added alongside* the old heuristics rather than *replacing* them, so the watcher still carries the foreign-session branch, the latch branch, the uppercase guard (`isUppercaseUUID`), and the UUIDv7 guard. **The known failure "`--session-id` goes DEAD after the 1st `/clear`" and "stale `.managed` (foreign_session)" are both direct symptoms of this multi-file identity split.**

This is *the* load-bearing weakness: a context-fill watcher should hold its state machine in memory and treat the disk as a single observable input (the gauge). Instead disk *is* the state machine, shared across processes with no transactional boundary, reconciled ad hoc on every tick.

---

## 3. The Go↔tmux side-effecting boundary — well isolated (the one clean boundary)

This is the design's strongest point. All world-mutation goes through `injector.go` (`InjectText`/`SendEscapeKey`/`SetTmuxEnv`) and `respawn.go` (pane probes, `sh -c` respawn). The cycler never calls `tmux` directly — it calls `c.cfg.InjectFn` / `SendEscapeFn` / `SetTmuxEnvFn`, all injectable and all defaulted to the real implementations. Tests pass spies. The pure logic (`operatorActiveSince`, `minAbsOrPctCeil`, `isUUIDv4`, `heartbeatSessionID`) is split out from the tmux-calling shells specifically for unit testing.

The boundary is **isolated behind an interface and not smeared through the logic.** Credit where due. The fragility at this boundary (the "unreliable, timing not executability" `/clear`) is *not* an isolation failure — it is inherent to driving an interactive REPL through bracketed-paste + heuristic settle delays (`submitSettle=750ms`, 2 retries). That is a known-hard problem, mitigated as well as the medium allows; it is not an architecture defect per se. **But:** it means the most critical operation (the destructive `/clear`) has a *probabilistic* success boundary, and the architecture builds an elaborate journal + freshness-gate + anti-loop machine *on top of* that unreliable primitive rather than making the primitive verifiable. The ACK-handshake design in the investigation README (§4) is the correct architectural response: make the side effect *prove* it landed, instead of inferring success from later gauge polls.

---

## 4. Control flow — a flat tick loop guarding a destructive multi-step cycle

`Watcher.Run` (watcher.go:594-846) is a single `for/select` with the tick body containing, in order: orphan reaper → gauge read → absent branch → parse-error branch → heartbeat → stale branch → identity reconciliation (adopt/foreign/latch) → fresh-reset → idle-gate → gate-predicate logging → **Cycler.MaybeRun** → **precompact** → **restart-now** → warn state machine → deferred inject. That is roughly **a dozen decision points and four separately-reachable action paths** in one function body, with five boolean state vars (`warnArmed`, `warnFired`, `pendingInject`, `gaugeStaleSince`, plus per-path `last*At` timers) tracked as loop-local variables — i.e. an **implicit, undocumented state machine encoded in local-variable mutation**.

`Cycler` then carries its *own* implicit state machine across calls: `lastFiredSID`, `seenLowPctAfterLastFire`, `lastForcedAttemptAt`, `consecutiveHandoffTimeouts`, `currentSessionID`, `currentSessionIDSince`, `seenSessionIDs`, `bootGraceFirstArmAt`. `MaybeRun` has **7+ gates** with a 3-way force-retry exception nested inside Gate 6 (cycle.go:650-677), and the boot-grace logic (cycle.go:564-613) is a sub-state-machine of its own with a burst window, a per-SID timer, AND a `MaxBootGraceTotal` ceiling — three interacting time bounds, each added by a separate follow-up bead (hk-4f8, hk-ibb fix 1/2/3, hk-hz9 fix 1/2/3/5).

The `CycleJournal` (cycle.go:23-29) is a genuine state machine done *right* — explicit phases (`opened→handoff_injected→confirmed→cleared→resumed→complete`/`aborted`) persisted to disk with a `RecoverFromCrash` reader. **The irony is that the journal proves the team knows how to model state explicitly — yet the warn machine, the anti-loop machine, and the boot-grace machine are all left as scattered booleans.** The "ACT-when-idle path LOOPS … re-fires with a new nonce … truncated the handoff file" bug (investigation README, bug 3) is exactly the failure mode of an under-specified state machine: a transition (handoff-timeout abort) that doesn't correctly suppress re-entry, compounded by truncate-before-confirm.

**Three near-duplicate cycle paths** (`runCycle`, `runOnDemandCycleTail`, `RunForPrecompact`→`runCycle`) re-implement steps 3b–7 with subtle gate differences. `runOnDemandCycleTail` (cycle.go:1240-1315) is a hand-copied tail of `runCycle` (cycle.go:813-879). This is the over-build: three entry points (auto / precompact / restart-now) each justified, but each bolted on with its own gate ordering rather than a shared parameterized cycle.

---

## 5. Is the CLI doing core's job? Partially — and the doctor file is a god-file

`keeper_cmd.go` is mostly correct: it parses flags, resolves the threshold precedence (CLI > config.yaml > compiled), and *constructs* the `Watcher`/`Cycler` with the right `…Fn` wiring (keeper_cmd.go:198-258). That construction-and-wiring belongs in `cmd/`. Good.

`keeper_enable_doctor_cmd.go` (1163 lines) is a **god-file** by composition: `enable` (~27%), `doctor` (~31%), and **~37% settings.json/hook-stanza manipulation** (`mergeHookStanza`, `findHookForScript`, `appendHookGroup`, etc.). The settings.json plumbing is legitimately CLI-only and *should* live outside core — but it should be its own file (`keeper_settings.go`), not co-resident with the diagnostic command. More importantly, the file leaks keeper *policy* into the CLI:

- **`.managed` is created by hand in the CLI** (path + timestamp written directly, ~line 331) instead of via a `keeper.CreateManaged()` — the file format is owned in two places.
- **`.ctx`/`.sid`/`.idle` paths are reconstructed by hand** in `doctor` (~lines 592/615/636) instead of through exported helpers — the on-disk layout knowledge is duplicated.
- **A 5-minute gauge-freshness threshold is hard-coded in the CLI** (~line 598) — a keeper policy constant living in the wrong layer; if `Staleness` (120s) changes, doctor's notion of "fresh" silently diverges.

Notably `doctor` does *not* re-implement UUID validation or the threshold math — it calls `keeper.IsPrimarySID`, `keeper.ReadCtxFile`, `keeper.ReadManagedSessionID`. So the leakage is **path/format/threshold knowledge**, not duplicated algorithms. The "gauge not wired for crews" failure surfacing via `keeper doctor` is the symptom of doctor having to *probe* the same split state from outside — a single-source-of-truth design wouldn't need a 10-check diagnostic command to tell you whether the watcher can see its own session.

---

## 6. Abstractions: missing vs over-built

**Over-built:**
- 24 distinct `SessionKeeper*` event types in `core/keeperevents.go` — every internal decision (`no_gauge`, `warn`, `handoff_started`, `cycle_complete`, `cycle_aborted`, `clear_unconfirmed`, `cycle_recovered`, `precompact_blocked`, `restart_now_blocked`, `operator_attached`, `respawn_attempted`, `live_pane_recover`) is its own event. Several emitters are already no-ops (`emitOperatorAttached`, cycle.go:1359) — vestigial.
- Three time-bound sub-systems inside boot-grace (per-SID timer + burst window + total ceiling).
- Three cycle entry points with copied tails.
- The force-retry exception duplicated in two arms of Gate 6.

**Missing:**
- An explicit, named **keeper state machine** (the warn/anti-loop/grace booleans should be one typed state, like `CycleJournal` already is).
- A single **identity resolver** (one function: disk → authoritative session id or "no trustworthy identity"), removing the latch/foreign/adopt/uppercase/UUIDv7 logic from the watcher loop.
- A **verifiable side-effect primitive** (the ACK handshake) so the journal doesn't have to infer `/clear` success from gauge re-polling.

---

## 7. The three most load-bearing architectural weaknesses

**1. State is split across ~9 multi-writer disk files with no single source of truth and an ad-hoc per-tick reconciliation. (FUNDAMENTAL, but boundable.)**
This is the root of the identity bugs (`--session-id` dead after `/clear`, stale `.managed`/foreign-session, gauge-not-wired-for-crews). It is *fundamental* in that the keeper observes an external process it cannot share memory with, so *some* disk coupling is unavoidable. But it is **boundable**: collapse identity to the single-writer `.sid` channel (hk-8prq already exists — finish the job and *delete* the watcher's latch/foreign/adopt/uppercase/UUIDv7 branches), and hold all keeper-internal state (warn/anti-loop/grace) in memory + the one `CycleJournal`. The disk should be *input* (gauge) + *one* journal, not nine coordination files.

**2. The reset cycle is a destructive multi-step operation built on a probabilistic side-effect (bracketed-paste `/clear`) with success inferred, not confirmed. (FIXABLE — the fix is already designed.)**
The journal, freshness-gate, anti-loop, and boot-grace machinery all exist to *compensate* for not knowing whether an injection landed. The ACK-handshake (investigation README §4, already smoke-tested in unmerged `89852bb3`) makes the primitive verifiable and lets much of the compensating machinery be deleted. This is the highest-leverage fix.

**3. Control flow is an implicit state machine spread across a ~250-line tick loop + 8 cross-call Cycler fields + three copied cycle paths. (FIXABLE — high effort.)**
Each branch is individually defensible and individually tested (74% coverage), but the *combinatorial* interaction is what produces the ACT-loop/handoff-truncation class of bug and makes every fix a new `hk-…fix-N` bead bolted onto the same function. The investigation's own audit concluded "sound core, no full rewrite warranted" — I partially agree: the *leaf files* don't need a rewrite, but `watcher.go`'s loop and `cycle.go`'s gate stack should be re-expressed as one explicit state machine (the `CycleJournal` pattern, generalized) before more features are added, or the bead-per-fix accretion will continue.

---

## 8. Note on the "complexity causes failures" question

Yes. The complexity is not incidental — it is *accreted-fix* complexity. The comment density is a tell: nearly every gate cites 2-4 bead IDs documenting a prior failure that the gate works around (hk-jgzg F45 split-brain, hk-qoz force-clear catch-22, hk-6qf operator-attached, hk-2yvx event spam, hk-l3gs sleep gate, hk-0t5s remote-control discriminator…). Each fix was locally correct and locally tested. The architecture never got the consolidating refactor that would let a fix *remove* a branch instead of *adding* one. That is why it "keeps breaking": the failure surface grows with each patch because the underlying split-state + inferred-side-effect design forces every new edge case to become a new special-case branch rather than a new state in an explicit machine.
