# Mega Code-Review — FINDINGS (Claude lane)

Consolidated, de-duplicated, and ranked from four condensed lenses (correctness/concurrency, architecture/rot, coverage/test-theater, completeness-critic) over 31 review units. Findings the critic recovered (dropped by the first-pass condensers) are folded in and tagged **[recovered]**. Architectural/systemic problems are ranked first, then subsystem Critical/High, then Medium/Low. A per-RU coverage-confidence table closes the report — thin coverage is labeled thin.

---

## (a) Executive summary

- **The operator control path is dead-on-arrival.** `confirm-verdict` / `veto-verdict` (the entire RC-027 operator override surface) send socket ops the daemon never registers — a run blocked on a `confirm_required` reconciliation verdict can never be confirmed or vetoed through the shipped commands. This is the single worst finding: a documented safety valve that does nothing.
- **Multiple confirmed data-loss / ledger-corruption paths.** Gitignore hygiene commits daemon state onto the operator's real branch; corrupt/truncated lease-locks get downgraded to "no lock" and the active worktree is force-GC'd out from under a live run; and Cat-3c auto-close silently marks reverted/superseded beads DONE by scanning *all* of branch history for a trailer with no check the work is still in the tree.
- **Entire subsystems are dead but green-tested — false coverage at scale.** `internal/workflowvalidator` (a whole second DOT parser/validator, ~2k test LOC), `internal/operatornfr` (unwired production API + 22 pure spec-mirror test files, ~13.6k test LOC that assert literals against themselves), and three `internal/scenario` subsystems (matrix-expansion, leak-sensor, crash-recovery) the real runner never calls. These prove conformance of code no one runs and are drift landmines.
- **Concurrency invariants rest on unlocked read-modify-write and undocumented runtime behavior.** `queue-submit` bypasses the B1 mutation lock (lost-update + single-active TOCTOU); the global eventbus WaitGroup can trigger `fatal error: WaitGroup misuse` and hard-abort the process on any drain-while-emitting shutdown; strict-FIFO merge ordering relies on Go's *undocumented* channel sender-release order.
- **Remote-worker paths are systematically unsafe.** `Kill()` fires SIGTERM/SIGKILL at a local PID for remote sessions (can kill an unrelated host process); SSH transport failures on verdict/auto-status reads are swallowed as "absent," silently mis-gating the review outcome on remote workers **[recovered HIGHs]**; remote windows leak because cleanup uses the local adapter.
- **God-functions concentrate the highest maintainability risk.** `runWorkLoop` (~1560 lines), `beadRunOne` (self-admitted, load-bearing nolint), `driveDotWorkflow` (~1000 lines with an overlapping merge-exemption lattice where each branch independently decides whether valid work is merged or stranded). Review-loop and DOT drivers are two ~2000-line copies of the same machinery; every fix lands twice and they can drift.
- **Overall code-health read:** the core loop *works* but is carrying heavy architectural debt — parallel dead implementations, copy-paste drivers, stringly-typed control flow on hardcoded node-IDs (`"commit_gate"`), and an event taxonomy spelled independently in three hand-maintained lists with no compile-time link. Test suites substantially overstate real coverage. The correctness surface has a cluster of genuine production-breaking bugs (the Critical + most Highs below), not just style debt.
- **Coverage is uneven and partly thin.** RU-23 (37-file grab-bag), RU-10b (237-file skim), RU-01b/RU-02 (god-function bodies/tails not line-audited), and RU-16b/RU-25 (skimmed) are thin. Treat clean results in those units as *unproven*, not *safe*.
- **The two flagged-thin units were re-reviewed and both contained real bugs — the multi-agent re-review was load-bearing.** The deep passes over the previously-*partial* RU-08 and the previously-*thin* RU-14 surfaced a new HIGH-class latent stall (a hook `agent_ready` lost-wakeup, `sessionstore.go:285`), a systemic dead-contract finding (a large fraction of the HC-xxx behavioral contract is asserted-but-not-enforced against the shipping code — see A9), and eight more medium/low defects the sampling first pass missed. "Suspiciously clean" was, in both units, under-review — not safety.

---

## (b) Critical / High findings

### Operator control surface

**C1 — [CRITICAL, adversarially CONFIRMED] confirm/veto-verdict commands are dead: no daemon handler registered**
`cmd/harmonik/confirm_verdict.go:131`, `cmd/harmonik/veto_verdict.go:163` (RU-16a)
The commands send socket ops (`confirm`, `veto_verdict`) that `buildSocketRouter` (socketdispatch.go:360) never registers among its 25 ops -> daemon returns "unknown op", CLI exits 1.
*Adversarial verification (`verify/C1.json`, verdict CONFIRMED):* a whole-tree hunt found **no alternate release path** — the subscribe/hook-relay pre-branches in `socket.go:455` do not handle these ops either, and the core `VerdictOverride` domain logic (`internal/core/verdictoverride_rc027.go:97`) is implemented but never invoked from any daemon dispatch/handler path (test-only). A latent second defect: `confirm-verdict` sends op `"confirm"` while the shared helper's doc claims `"confirm_verdict"` — moot, since neither spelling is registered.
*Why it matters:* the entire RC-027 operator control path is non-functional end-to-end. A daemon blocked on a `confirm_required` reconciliation verdict can never be released through the shipped, documented commands.
*Failure:* a stuck run stays stuck forever; the operator's only escape hatch is dead.
*Fix direction:* register `confirm`/`veto_verdict` ops in `buildSocketRouter` and wire them to `VerdictOverride`; add an end-to-end smoke asserting a `confirm_required` run can be released. Root cause is the dispatch-table gap in `main.go`'s ~40-branch `run()` (see A5) — a verb can ship unwired.

### Workspace / data integrity

**H1 — Gitignore hygiene commits daemon state onto the operator's current branch**
`internal/workspace/gitignorehygiene.go:199` (RU-04b)
`gitignoreCommit` runs `git add` + `git commit --allow-empty` against whatever HEAD points at, never checking out the dedicated `harmonik/gitignore-init` branch its docstring/WM-013e promises. At startup HEAD is usually `main` / the operator's working branch.
*Failure:* a daemon-state commit is silently injected onto the user's branch — the exact "daemon state leaking into user commits" outcome WM-013e exists to prevent; `--allow-empty` forces it even with nothing to record.
*Fix direction:* check out (or create) the dedicated branch before committing; refuse to commit onto a non-harmonik branch.

**H2 — Corrupt lease-lock downgraded to "no lock" -> active worktree garbage-collected**
`internal/workspace/discoverworktrees.go:161` (+ `orphansweep.go:206`, `:238`) (RU-04b)
`DiscoverWorktrees` discards read/parse errors from `readDiscoveredLeaseLock` (`err==nil` guard), so a corrupt/truncated `lease.lock` is indistinguishable from absent -> `LeaseLock=nil` -> `NoLock`. `RemoveAgedNoLockWorktrees` then force-`git worktree remove --force --force`s it once aged.
*Failure:* a live run whose lease.lock was truncated by a crash-mid-fsync (the exact WM-003a scenario) is GC'd out from under an active session — uncommitted work discarded.
*Fix direction:* treat a read/parse error as "lock present, state unknown" (fail safe / quarantine), never as absent.

**H2b — Aged-worktree reaper uses directory mtime as an "inactive" proxy**
`internal/workspace/orphansweep.go:206` (RU-04b)
Dir mtime bumps only on entry add/remove; an agent editing files in place never bumps the top-level worktree mtime. `--force --force` then discards uncommitted work. Blast radius stacks with H2.
*Fix direction:* derive activity from lease-lock liveness / recent file mtimes within the tree, not the top-dir mtime.

**H3 — Cat-3c auto-close fabricates done-status for reverted/superseded work**
`internal/lifecycle/orphansweepbeads.go:248` (+ silent close `:700`) (RU-12x)
`HasMergeCommitForBead` scans the target branch's *entire* history for any commit ever carrying the bead's trailer with a non-docs diff — no check the change is still in the current tree. `SweepCloseBead` then closes with no needs-attention marker.
*Failure:* work merges, is later `git revert`ed or superseded; the trailer commit stays in history, so the next startup sweep silently marks the still-in_progress bead DONE. Self-hiding — closed beads drop out of `br list --status in_progress`. Ledger integrity corruption.
*Fix direction:* verify the change is still present in the tree (e.g. `git merge-base --is-ancestor` + content presence), and mark auto-closed beads with a needs-attention flag.

**H4 — [recovered] SSH transport failure on verdict read swallowed as "verdict absent"**
`internal/workspace/reviewverdict.go:176` (RU-04) — *dropped by first-pass condensers*
A transport/SSH error reading the reviewer verdict is mapped to the same outcome as a genuinely-absent verdict -> wrong review-gate decision on a remote worker.
*Failure:* an unreachable remote read greenlights or blocks a run as if no verdict were written; the review gate silently mis-decides.
*Fix direction:* distinguish transport error (inconclusive -> retry/escalate) from a confirmed-absent verdict.

**H5 — [recovered] SSH transport failure on `auto_status` read swallowed as absent**
`internal/workspace/autostatusmarker.go:99` (RU-04) — *dropped by first-pass condensers*
Same failure class as H4 on the auto-status marker path: an unreachable remote read is indistinguishable from "marker not written," silently mis-gating the run outcome.
*Fix direction:* same as H4 — propagate transport errors as inconclusive.

### Daemon / concurrency

**H6 — queue-submit read-modify-write bypasses the B1 mutation lock (lost update + single-active TOCTOU)**
`internal/queue/rpc.go:161` (RU-09)
`HandleQueueSubmit` does Load->Validate(QM-027 single-active)->Persist entirely unlocked; the B1 fix only covered the append path.
*Failure:* two concurrent submits for the same new queue name both pass the single-active check, both mint queue_ids, both Persist to the same `<name>.json` — last-writer-wins drops one queue. Submit Persist can also clobber the workloop's in-flight locked status Persist.
*Fix direction:* take `LockForMutation` across the whole submit read-modify-write, same as append.

**H7 — Global WaitGroup misuse in eventbus can hard-abort the process**
`internal/eventbus/busimpl.go:505` (RU-11)
Every `Emit*` calls `b.wg.Add(1)` before spawning; `Drain` spawns a goroutine calling `b.wg.Wait()`. The per-run WaitGroup is sealed against this; the global `b.wg` is not.
*Failure:* if `Drain` runs (counter momentarily 0) concurrently with an `Emit` doing `Add(1)`, Go aborts the whole process: `fatal error: sync: WaitGroup misuse`. Any shutdown that drains while producers still emit is exposed.
*Fix direction:* gate Add behind a seal check under a lock, or restructure so Add cannot race Wait (the same seal the per-run WG already uses).

**H8 — Remote-session Kill sends SIGTERM/SIGKILL to a local PID**
`internal/daemon/tmuxsubstrate.go:2492` (RU-05a)
`Kill()` calls `killProcessWithGrace(s.pid, ...)` unconditionally with local `syscall.Kill`. For a remote session `s.pid` is the worker's pane PID — meaningless on the daemon host. `runWait` guards every liveness check on `!s.remote`; `Kill` has none.
*Failure:* a remote run finishes, workloop calls Kill, SIGTERM/SIGKILL fire at whatever local host process occupies that numeric PID (collisions common at low/mid PIDs) — can terminate an arbitrary unrelated process.
*Fix direction:* branch on `s.remote` and route Kill through the SSH remote adapter.

**H9 — [recovered] Staleness re-capture swallows a beads-audit fetch error, defeating RC-024**
`internal/daemon/verdictexecutor_rc025a.go:151` (RU-02) — *dropped by first-pass condensers*
The RC-024 staleness re-capture eats the audit-fetch error, so a stale verdict can execute against out-of-date state — the exact condition RC-024 exists to block. (The condensers carried this file's `:439` no-op medium, M below, but dropped this HIGH.)
*Fix direction:* on audit-fetch error, treat state as unknown and refuse to execute the verdict (fail closed).

**H10 — runShell busy-spins a CPU core on ctx-cancel with no armed timer**
`internal/daemon/runshell.go:342` (RU-01b)
`fireOnCancel` is a no-op when `sh.timers` is empty; `drive()` exits only on `!InFlight()`, not ctx cancel, and nothing closes the tap on cancel.
*Failure:* if the Run machine is still InFlight with no timer armed and the parent ctx is cancelled, `driveOnce` is called forever — burning a full core, machine never advancing. Latent today only because production drive runs with `context.Background()` (and this drive loop is itself production-dead — see A-test-theater below).
*Fix direction:* make `drive()` select on ctx.Done() and exit; or delete the dead drive loop entirely.

**H13 — `agent_ready` has no latch: a relay signal arriving before `SetAgentReadyCallback` is silently lost (lost-wakeup)**
`internal/hook/sessionstore.go:285` (RU-14 deep)
`notifyAgentReady()` reads `sess.agentReadyCallback` under the mutex and, if it is nil, does nothing — there is no stored "ready already happened" latch. But the daemon registers the session and launches the handler subprocess *before* installing the callback (workloop.go:4566 register + Launch, then :4738 `SetAgentReadyCallback`; same shape in reviewloop.go:385/651 and dot_gate.go:326/375). The subprocess's `SessionStart` hook fires asynchronously and drives `Dispatch("agent_ready")`.
*Failure:* if that relay message lands in the window after `RegisterHookSession` but before `SetAgentReadyCallback` runs (scheduling skew, a warm/fast resume, or a subprocess that boots quicker than the daemon goroutine advances), `notifyAgentReady` finds `cb==nil` and drops the signal. Nothing re-delivers it, so `waitAgentReady` blocks until `ErrAgentReadyTimeout`/HC-056 and dispatch for that run stalls. This is the exact failure class the extensive comments at workloop.go:4710 and reviewloop.go:643 describe as already having bitten smoke v6 (hk-lj1p9.4); the current fix only orders the two daemon statements — it does NOT order them against the subprocess's independently-timed `SessionStart`.
*Fix direction:* edge-latch the signal — add a `readyFired`/`pendingReady` bool: `notifyAgentReady` sets it (and fires `cb` if present); `SetAgentReadyCallback`, on install, immediately invokes `cb` (outside the lock) if `readyFired` was already set.

### Codex wire / session

**H11 — JSON-RPC id typed `*int64` — a string id crashes the whole session**
`internal/codexwire/codexwire.go:171` (RU-07)
JSON-RPC 2.0 permits string ids; codex/peers may use them. A non-integer id fails envelope `Unmarshal` -> `Parse` error -> readLoop treats it as a fatal decode -> session torn down to Exited.
*Failure:* a single unexpected id shape kills an otherwise-healthy session, violating the package's forward-compat/round-trip contract on the one field JSON-RPC explicitly allows to vary.
*Fix direction:* type the id as `json.RawMessage` (or a string-or-int union) and preserve it verbatim.

### brcli / beads guard

**H12 — BI-010c workflow-label write guard bypassed by `--flag=value` argv form**
`internal/brcli/workflowlabelwrite_bi010c.go:90` (RU-18)
`CheckWorkflowLabelWrite` matches `strings.HasPrefix(arg, "workflow:")` on standalone argv only. The joined form `--label=workflow:dot` has prefix `--label=`, so the guard returns nil.
*Failure:* the forbidden agent-context workflow-label mutation (BI-INV-001 authority guard) proceeds silently through a trivially-reachable encoding.
*Fix direction:* normalize argv (split `--flag=value`) before the prefix check; match the label value, not the raw token.

### Architecture — dead parallel implementations (HIGH, systemic)

**A1 — Entire `internal/workflowvalidator` package is a dead second DOT parser + validator**
`internal/workflowvalidator/validator.go` (+`dotparser.go`, ~1085 impl + ~2000 test LOC) (RU-15)
A complete second workflow-graph pipeline (own DOT parser, Tarjan SCC, BFS reachability, EM-038 rules) with zero non-test references; the live path is `internal/workflow/dot`. Migration bead landed, old package never deleted. **The prime rot finding.**
*Companion drift:* the two validators enforce *opposite* rules for `handler_ref` on non-agentic nodes (live: required; dead: forbidden) and disagree on `start_node` vs `start_node_id` — a landmine if anyone resurrects/copies the dead package.
*Fix direction:* delete the package.

**A2 — Entire `internal/operatornfr` production API is unwired; 22 of 37 test files are pure spec-mirror theater**
`internal/operatornfr/exitcode.go` + all exports; `restartrto_sx9r81_test.go:29,115`; `statemachinetransition_test.go:21` (RU-20)
No product code imports the package; exit-code taxonomy, upgrade-integrity gate, sandbox path check, and marker I/O are enforced elsewhere or nowhere. 22 test files (~13.6k LOC) declare inline fixture constants and assert them against themselves (`const X = 30 ... if X != 30`) — zero regression protection. The state-machine test reimplements the transition logic locally and tests the copy.
*Fix direction:* reviewer verdict is DELETE the 22 self-referential test files; wire the real API to the daemon or delete it; keep only the exit-code cross-checks that touch real code.

**A3 — `internal/scenario` matrix-expansion + leak-sensor + crash-recovery never invoked by the real runner**
`matrixexpand.go` (`ExpandMatrix`, 325+685 LOC), `postsuiteleaksensor.go` (`CheckPostSuiteLeaks`, ~360 LOC), `crashrecovery.go` (296 LOC) (RU-22)
The production runner (`cmd/harmonik/harness.go:356`) ranges raw scenarios and never expands, never leak-checks, never arms a crash point. A `matrix:` YAML with `{{.param}}` runs once *unsubstituted*; leaked run processes/held leases/open fds are silently undetected; crash-recovery conformance is advertised but unexecutable. SH-030/005/007 and SH-INV-002 unreached in production.
*Fix direction:* wire `ExpandMatrix` + `CheckPostSuiteLeaks` into the runner (contract says MUST), or delete the advertised-but-dead surface. Also: the timeout branch (`harness.go:481`) never reads the partial JSONL nor evaluates assertions (SH-026) -> empty AssertionResults on timeout.

**A4 — EV-036 secret-field startup guard never invoked at daemon startup**
`internal/core/eventregistry.go:331` (RU-10)
`ScanRegisteredPayloadsForSecretFields` MUST run after registration, before the bus is sealed (event-model.md §4.10). Zero callers in cmd/ or internal/daemon/ — only tests invoke it.
*Failure:* a future payload with a field like `SecretToken` ships and emits to the durable JSONL log with no startup failure; the fail-closed guarantee lives only in a unit test.
*Fix direction:* call the scan in the daemon startup sequence before bus seal; make it fatal.

### Architecture — god-functions (HIGH maintainability risk)

**A5 — `runWorkLoop` is a ~1560-line god-function** — `internal/daemon/workloop.go:1531-3097` (RU-01a). Interleaves >=12 concerns; nesting 6-7 deep; dozens of post-sleep `continue`s. Trivially easy to leak a held `LockForMutation`/`lq.Done()` or skip a state reset on a new branch.

**A6 — `beadRunOne` is a self-admitted god-function** — `internal/daemon/workloop.go:3121-4100+` (RU-01a). Carries `//nolint:gocognit,cyclop,funlen` promising an "M5 reactorization" that never landed -> load-bearing nolint debt. Every guard duplicates a reopen-bead-and-return idiom with discarded error (`reopenTID, _ :=`).

**A7 — `driveDotWorkflow` is a ~1000-line god-function with an overlapping merge-decision exemption lattice** — `internal/daemon/dot_cascade.go:~499-747` (RU-02). The no-progress block stacks >=5 completion/suppression exemptions keyed on the same predicates, guarded only by prose comments citing bead IDs. **Each branch independently decides whether valid committed work is MERGED or STRANDED** — any edit can silently merge un-reviewed work or strand approved work. Highest maintainability risk in scope.

**A8 — Review-loop and DOT drivers duplicate the entire no-progress / HEAD-advancement / feedback machinery** — `internal/daemon/reviewloop.go` vs `dot_cascade.go` (RU-02). Two ~2000-line copies of HEAD-advancement, diff-hash payload, session-id interceptor, no-commit guard, reviewer-feedback write, verdict threading. Every fix lands twice (hk-togxq/m1wqp/wixms/fxy9) and they already diverge (enum vs free-form summary; different REMOTE feedback handling).

**A9 — The `handlercontract` (HC-xxx) behavioral contract has quietly diverged from the shipping handler: MUST-clauses asserted but not enforced** — `internal/handlercontract/*` (RU-08 deep). The deep pass over the ~50 one-clause HC files (skipped by pass-1 sampling) found that a large fraction of the *behavioral* contract helpers have **zero non-test callers** and sit on no production path, while the real `internal/handler` implementation does the work a different way — so the contract and the code that ships have silently split. Concrete unwired MUSTs:
- **HC-044a orphan/pidfile machinery** (`orphancheck_hc044a.go:155`) — `CheckOrphanHeldWorkspace`/`WriteWorkerPidfileAtomic`/`RemoveWorkerPidfile` have no non-test caller; the doc says "before Launch returns a Session, the daemon MUST call this," but Launch/session.go never does. The real orphan handling is a completely different mechanism (`RunOrphanSweep`).
- **HC-048a provisioning backoff** (`provisioningretry_hc048a.go:59`) — `RetryProvisionWithBackoff` is unused; the adapters (`adapter_claudecode.go`, `adapter_pi.go`) implement backoff independently, so the base=1s/cap=16s/maxAttempts=4 discipline is asserted where nothing runs it.
- **HC-004 launch idempotency** (`handler_hc001.go:73`) — the contract `Handler.Launch` MUST be idempotent on `LaunchKey(spec)`; no production type implements the interface and `LaunchKey` is dead. The real `handler.Launch` mints a fresh UUID and unconditionally spawns — a daemon restart mid-run that re-dispatches the same `(run_id,node_id)` spawns a second subprocess (duplicate work / double budget), exactly what HC-004 exists to prevent.
- Also unwired: HC-013a rotate boundary, HC-033 startup secret-leak schema guard, HC-015 run-lock discipline, HC-016 per-role work queues, and the HC-018 cancellation bounds (see the Low list). *Fix direction:* for each, either wire the helper into the real Launch/adapter/daemon path as the contract states, or delete the helper and re-point the HC clause at the mechanism that actually ships — the current state is test-theater that hides drift.

---

## (c) Medium / Low findings by subsystem

### daemon / workloop / dispatch
- **M — Two reconciliation verdict actions recorded as executed but do nothing** — `verdictexecutor_rc025a.go:439` (RU-02). `resume-here`/`reset-to-checkpoint` return nil (TODO "not yet wired") then commit + emit `reconciliation_verdict_executed`. Operators believe the run resumed/rolled back; the Cat-3b idempotency guard never retries. Silent no-op.
- **M — Eager-refill target-branch bugs re-dispatch already-landed work** — `eagerfill_em063.go:251,252` (RU-01b). Hardcoded `origin/main` + unanchored `--grep` (no `--fixed-strings`). On a non-main integration branch, completed beads are re-appended; a bead id that prefixes a longer landed id is wrongly dropped. Sibling `beadOnOriginMain` does both correctly — the two checks disagree.
- **M — queue.Persist (fsync) runs under the LockForMutation write lock** — `workloop.go:2193` (+2472/2574/2617/2638/2813/2962) (RU-01a). Completion accounting serializes behind disk-I/O per poll tick under N runs.
- **M — adoptLiveRunSession goroutines untracked by the shutdown WaitGroup** — `workloop.go:1716` (RU-01a). Teardown neither awaited nor bounded -> SIGTERM can exit mid-write, inconsistent bead/queue state.
- **M — max-attempts branch leaves the item pending -> potential dispatch live-lock** — `workloop.go:2599` (RU-01a). Never sets Status=Failed (unlike the sibling at 2569); if the evaluator doesn't transition it, infinite re-select spin.
- **L — exitClean drain goroutine leaks a `wg.Wait()` goroutine on drain timeout** — `workloop.go:1668` (RU-01a).
- **L — `clockAfter` spawns a non-cancellable sleep goroutine on `context.Background()`** — `workloop.go:1202` (RU-01a).
- **L — [recovered] pervasive silent discard of `bus.Emit`/`Marshal`/`ReopenBead` errors** across loop/run paths — `workloop.go:1849` (RU-01a). Systemic.
- **L — [recovered] cross-queue duplicate scan is O(queues*groups*items) under the write lock every dispatch** — `workloop.go:2532` (RU-01a). Hot-path perf.
- **L — RunID placeholder pointer (`&runUUIDStr` empty) is a brittle two-phase patch** — `workloop.go:2608,2794` (RU-01a). queue.json persists `RunID=""` between stamp and patch; a crash leaves it durably empty -> defeats QM recovery keyed on RunID presence.
- **L — `normalize()` silently no-ops load-bearing effectors (`closeBead`/`submitMerge`)** — `runshell.go` (RU-01b). A forgotten `wireSpine` arm produces a run that hangs/mis-terminates with zero error.
- **L — `CloseBrUnavailable` maps to success terminal without an actual bead close/emit** — `runbridge.go:349` (RU-01b).
- **L — `resumeReadyProbe` fabricates `agent_ready` on a fixed 2s timer** — `dispatchsegment.go:196` (RU-01b).
- **L — `eagerRefillEval` computes slot deficit outside the append lock (TOCTOU)** — `eagerfill_em063.go:80` (RU-01b).
- **L — blocking launch closure in `launchAgent` not ctx-interruptible** — `dispatchsegment.go:146` (RU-01b).
- **L — [recovered] cognition-gate ready-wait uses `==` on the timeout sentinel** — `dot_gate.go:402` (RU-02). A wrapped/ctx-cancel return falls through to paste-inject on a dead session.
- **L — [recovered] run-context JSON marshal error ignored** building the gate-task brief — `dot_gate.go:473` (RU-02).

### daemon / config / socket / router
- **M — config.yaml block-absent sentinels ignore most fields -> operator config silently dropped** — `projectconfig.go:1287,1290` (RU-06). A config with only `watch.absent_thresh_s` (no `schema_version`) is treated as empty-file and discarded -> daemon boots on defaults, tuning lost.
- **M — `Config` god-struct interleaves production surface with test-injection seams** — `daemon.go:581` (RU-06). ~50-field root still carries `Runner`/`QueueStore`/`Skip*` despite `daemonTestHooks` existing to hold them.
- **M — Unknown-keeper-key detection couples to yaml.v3 internal error text via regex** — `projectconfig.go:1481` (RU-06). A yaml.v3 upgrade silently degrades precise `*ErrUnknownConfigKey` to a generic error.
- **M — Event taxonomy is three hand-maintained parallel string lists with no compile-time link** — `pertypecompat_hqwn38.go`+`eventtype.go`+`eventreg_*.go` (RU-10). ~169 names spelled independently; a typo/added-type in one is invisible to the compiler.
- **L — `parseStallSentinelBlock` allocates 7 throwaway `*time.Duration` and dereferences by position** — `projectconfig.go:1882` (RU-06).
- **L — multiple hand-maintained `*BlockAbsent` invariants; two (watch, harnesses) already drifted** — `projectconfig.go:402/766/1794/1134/1280` (RU-06).
- **L — `removeStaleSocket`->Listen TOCTOU** against a racing daemon — `socket.go:228` (RU-06).
- **L — Router `Classify` hinges on `len(rawJSON)>2`** — `router/router.go:44` (RU-06). `{"type":null}` misroutes as a hook-relay envelope.
- **L — six-level telescoping `RunSocketListener*` constructor ladder** survives after `SocketHandlers`/`Serve` replaced it — `socket.go:279` (RU-06).
- **L — `state_cmd.go` still honors retired `HK_PROJECT` env var** — `state_cmd.go:107` (RU-16a).
- **L — [recovered] `daemonExpandHomePath` error swallowed** in `parseHarnessesBlock` — `projectconfig.go:1923` (RU-06).
- **L — `keeper.hard_ceiling.cooldown` parsed + strict-validated but never consumed** — `projectconfig.go:1664` (RU-06). Config trap.

### queue
- **M — Persist temp file keyed only on PID — concurrent in-process writes collide** — `persistence.go:110` (RU-09). Two concurrent Persist for the same queue produce identical tmpPath; O_EXCL second fails `ErrPersistFailed`. Reachable via the unlocked submit (H6).
- **M — Unbounded group-index access can panic the socket handler** — `rpc.go:510` (+append.go:124,166) (RU-09). `mutated.Groups[req.GroupIndex]` from decoded JSON, no bounds check.
- **L — `HandleQueueSetConcurrency` doc claims "validates N>=1" but no check** — `rpc.go:1295` (RU-09).
- **L — [recovered] event payload marshal errors silently drop lifecycle events** — `rpc.go:1023` (RU-09).

### codex driver / wire / twin
- **M — Server-originated JSON-RPC requests misclassified and silently dropped** — `codexwire.go:211` (RU-07). The app-server sends *requests to the client* (exec/apply-patch approval prompts) with id+method; they fall into the ignored default -> a turn needing approval hangs.
- **M — `codexSession` god-struct: five concerns under four overlapping mutexes** — `session.go` (RU-07). Concurrency contract in prose, easy to regress.
- **L — `finalize()` unconditionally marks every wind-down as a launch failure** — `session.go:460` (RU-07).
- **L — mid-turn steer orphans the previous TurnID with no terminal** — `reactor.go:384` (RU-07).
- **L — EventTypeConnected leaves stale ThreadID** — `codexreactor/reactor.go:204` (RU-07).
- **L — server response with explicit `"result": null` doesn't round-trip** — `codexwire.go:379` (RU-07).

### handler / handlercontract / substrate (RU-08 — DEEP re-review)
- **M — Subprocess orphaned when `openWireTap` fails after the child is already spawned** — `handler.go:342` (+ `launchViaSubstrate` :426) (RU-08 deep). `Launch` returns `(nil,nil,err)` on the wire-tap error path *without* killing the already-started session, so the running claude/pi/codex subprocess + its `runWait`/`drainStderr` goroutines leak with no reaping handle until the daemon orphan sweep. Reachable via an unwritable `HARMONIK_WIRE_CAPTURE_DIR`. Fix: `sess.Kill(ctx)` / `subSess.Kill(ctx)` before returning the error.
- **M — Version negotiation hardcodes `selected_version:1`, ignores handler `SupportedVersions` and the capabilities timeout** — `daemon/sessioncontext_chb023.go:373` (RU-08 deep). HC-009 is normative (select max(intersection), `ErrProtocolMismatch` on empty, abort at `HandlerCapabilitiesTimeout`=5s if capabilities absent), but the implementation never reads `SupportedVersions` and the timeout constant has zero production references. A handler advertising `[2]` (or `[]`) is still told version 1; a handler that never emits `handler_capabilities` hangs forever instead of aborting at 5s.
- **M — `ScheduleRotateAccount` TOCTOU — rotate can fire during a new in-flight turn** — `turnboundary_hc013a.go:133` (RU-08 deep). After waiting one `MarkTurnCompleted`, `RotateAccount` is called without re-checking `inFlight` or holding the lock; a concurrent `MarkTurnStarted` re-sets `inFlight` so rotate runs mid-turn — exactly what HC-013a forbids (and the helper has no production caller, so the adapters' real rotate path is unconstrained). Fix: hold the lock (or a rotation-in-progress flag) across the rotate.
- **M — PID recycling not detected — false orphan -> spurious `ErrStructural`** — `orphancheck_hc044a.go:208` (RU-08 deep). Liveness is only `syscall.Kill(pid,0)`; a recycled PID reads `Held=true` and Launch fails with `workspace_held_by_orphan` against an innocent process. The recorded `RunID` is read but never compared, though `Stale`'s doc claims it covers recycling. Fix: cross-check process start-time/identity before declaring Held.
- **M — Watcher-synthesized agent_failed events carry no session_id/run_id** — `watcher_hc011.go:670` (RU-08). Every watcher self-defect terminal emits with no envelope correlation -> wedged sessions surface unattributable failures the reconciler can't auto-recover.
- **M — SendInput ignores ctx; stdin write can block forever** — `session.go:370` (contract at handler.go:320) (RU-08). Bare `io.WriteString` while the contract documents "ctx bounds the write"; a wedged child that fills the ~64KiB pipe buffer blocks forever, leaking the delivery goroutine.
- **L — HC-018 cancellation bounds (`CancelGoSideBound`=500ms, `CancelSubprocessBound`=5s) declared but never enforced** — `cancelbounds.go:11` (RU-08 deep). Zero non-test references; nothing measures cancel latency or escalates to SIGKILL on breach, so a handler whose clean-exit blocks 30s lingers past the ceiling the spec guarantees.
- **L — `SessionLogLocationTimeout` (10s) declared but not enforced by the watcher** — `sessionlogloc_hc010.go:77` (RU-08 deep). A handler that negotiates version then hangs before emitting `session_log_location` waits forever instead of aborting with `ErrStructural` at 10s.
- **L — `lastReadEventAt` advances per-`Scan` (per NDJSON line), not per-`Read`** — `watcher_hc011.go:405` (RU-08 deep). A subprocess trickling one very long line advances `Read` but not `Scan`, so liveness stagnates and the supervisor can misclassify a live watcher as wedged and kill a healthy session (coarser than the HC-011a contract).
- **L — `NewSessionID` mints UUIDv4 but WM-018/harness docs say UUIDv7** — `sessionid.go:19` (RU-08 deep). Inconsistent across the contract; any consumer relying on v7 time-ordering is broken.
- **L — Dead invariant panic + unused helper in `NewWorkQueueSet` (HC-016 unenforced)** — `workqueue.go:48` (RU-08 deep). The post-construction guard iterates the same slice used to fill the map two lines earlier, so it can never fire; `WorkQueueSet` has no production caller, so "one queue per role" is unenforced.
- **L — stdout bridge goroutine + OS pipe leak if the watcher stops reading without closing the pipe** — `handler.go:256` (RU-08 deep). `io.Copy(stdoutPW, stdoutR)` blocks forever, leaking the goroutine and the real OS fd.
- **L — readLoop observes ctx cancel only between scans, not while blocked in `Scan()`** — `watcher_hc011.go:391` (RU-08).
- **L — `Session.Kill` spawns a fresh `waitOwner.Wait` goroutine per call** — `session.go:406` (RU-08).
- **L — `Twin.Events` replay goroutine leaks if consumer abandons the channel** — `replay.go:114` (RU-08).
- **L — two overlapping exit-classification paths** (`classify_hc023.go` vs `handlercontract/outcomedelivery.go:179`) encode the same rule over different structs; which one the daemon consults determines whether cancellation is honored (RU-08).

### tmux substrate / adapter / lifecycle-tmux
- **M — Argv joined without shell-quoting — reintroduces hk-rpr6 argv shattering** — `tmuxsubstrate.go:1680,1772` (RU-05a). `SpawnCrewSession`/`SpawnRunSession` use bare `strings.Join(argv," ")`; any argv element with a space is re-word-split by `sh -c`.
- **M — Remote windows leak — KillAllWindows uses the local adapter** — `tmuxsubstrate.go:991` (RU-05a).
- **M — Session orphan-detection hardcodes "zsh" as the idle-shell name** — `lifecycle/tmux/orphansession.go:161` (RU-05b). On bash/fish/sh hosts an idle shell counts as active -> orphaned sessions never reclaimed (defeats PL-006).
- **M — Window-name truncation still exceeds the stated 64-byte max** — `lifecycle/tmux/windowname.go:106` (RU-05b). `beadIDMaxBytes=56` mis-sized -> >=65 bytes.
- **L — `callNewWindowBounded` orphans a tmux window created just past the timeout** — `tmuxsubstrate.go:1143` (RU-05a).
- **L — `remoteAdapter` written/read without the sync used for `cachedPaneTarget` (data race)** — `tmuxsubstrate.go:2190` (RU-05a).
- **L — `hasChildProcess`/`hasAnyDirectChild`/`commandMatchesLiveAgent` production-dead** (two near-identical copies) — `pasteinject.go:353` (RU-05a).
- **L — `WriteToPane` leaks the loaded tmux buffer when PasteBuffer fails** — `osadapter.go:541` (RU-05b).
- **L — `parseTmuxMajorVersion` rejects tmux dev/master builds** — `osadapter.go:617` (RU-05b).
- **L — `SweepOrphanTmuxSessions` increments killed even when KillSession errored** — `orphansession.go:93` (RU-05b).
- **L — tmux resolved from passed env PATH but Probe/EnsureSession use process PATH** — `subcommand.go:283` (RU-05b).
- **L — `windowSweepAnyRemain` takes a `prefix` param it immediately discards** — `orphanwindow.go:166` (RU-05b).

### lifecycle sweeps / reconcile
- **H — Stale reconciliation-lock probe releases flock BEFORE unlink (RC-002a violation)** — `orphansweep.go:834` (RU-12). Between unlock and unlink another daemon can acquire the lock and start a live reconciliation the sweep then unlinks -> TWO concurrent reconciliations for one target_run_id.
- **M — PID-reuse races kill unrelated processes (three sites)** — `orphansweep.go:238`, `orphansweepbr.go:166` (+`:360`, `agentwatcherreap.go:215`) (RU-04b, RU-12). `isPIDDead` decides staleness purely by `Kill(pid,0)` -> recycled PID reads live forever (workspace wedged) or SIGKILL hits an innocent process after the liveness poll.
- **M — `reconcileThreeWay` is a ~315-line god-function; identical payload build appears 5x** — `startup_pl005_qm002.go` (RU-12).
- **L — `reconLockIsStale` treats an unparseable creator_pid as stale->removable** — `orphansweep.go:843` (RU-12).
- **L — `reconcileQueueTerminalState` returns done=true on CompleteAndUnlink failure**, leaving an active-status file on disk — `startup_pl005_qm002.go:956` (RU-12).
- **L — `SweepCloseBead` doesn't re-check bead status before closing** — `terminaltransition_bi010.go:521` (RU-12x).
- **L — `EnumerateStaleIntents` doesn't filter dirs/`.tmp-`/non-`.json`** — `orphansweep.go:956` (RU-12).
- **L — [recovered] `reapOrphanWorktreesFromArchives` re-reads every archive per Class-B bead** — O(archives*beads) startup — `startup_pl005_qm002.go:783` (RU-12).

### eventbus
- **M — Five near-identical emit pipelines copy-pasted** — `busimpl.go:412/540/661/751/858` (RU-11). The ConsumerClass fan-out switch is verbatim x5. `EmitWithRunID` already uniquely tracks `runWG` — a future edit silently diverges.
- **L — Drain/DrainRun leak the waiter goroutine when ctx is cancelled** — `busimpl.go:1504` (RU-11).
- **L — `Seal` sets sealed=true but nothing blocks a concurrent Emit during startup replay** — `busimpl.go:1220` (RU-11).
- **L — processBatch comment overstates O_APPEND atomicity beyond PIPE_BUF** — `jsonlwriter.go:209` (RU-11).
- **L — Append holds `mu` across a blocking channel send -> Close can stall** — `jsonlwriter.go:263` (RU-11).
- **L — [recovered] `jsonlwriter.go:383`** Filter logs a spurious error for a missing JSONL file (RU-11).

### core (events / registry / payloads) — RU-10b thin (237-file skim)
- **M — EV-034 registry sealing never implemented** — `eventregistry.go:57` (RU-10b). `RegisterEventTypeAtVersion` can succeed after dispatch begins -> late registration silently mutates the dispatch table.
- **M — `PlanForVerdict` panics on an unknown verdict derived from event data** — `verdictexecution_rc025.go:223` (RU-10b). VerdictEvent values originate from decoded payloads -> a forward-compat verdict is a hard daemon-goroutine crash.
- **M — Per-type schema-version / N-1 compat machinery entirely speculative** — `pertypecompat_hqwn38.go:52` (RU-10). ~169 rows all identical; the one live reader checks an unconditionally-true field -> dead branch.
- **L — `secretPrefixRe` matches substrings without word boundaries** ("auth" trips on Author) -> over-redaction — `eventregistry.go:69` (RU-10).
- **L — pervasive deferred-typing debt** (dozens of identifier fields as raw `string`) — `core/*_hqwn59.go` (RU-10b). Malformed commit hashes / empty control-point names round-trip silently.
- **L — payload field self-flagged "stale, needs cross-spec amendment"** — `daemonevents_hqwn59.go:272` (RU-10b).

### keeper (watcher / step / cycle)
- **M — `Watcher.Run` ~450-line god-function; `WatcherConfig`/`CyclerConfig` ~60-field god-structs** — `keeper/watcher.go:965` (RU-13). Load-bearing ordering is a prose comment.
- **M — [recovered] `stepIdleGaugeTick` derefs `ev.CF` with no nil guard** — `keeper/step.go:447` (RU-13). Keeper crash on a CF-less event (the precompact path nil-checks; this one doesn't).
- **L — `WarnOnly` gates `RunForIdle` but NOT `MaybeRun`/`RunForPrecompact`** — `watcher.go:1279` (RU-13). A `WarnOnly=true` caller with a non-nil Cycler can fire the destructive handoff+clear in "warn-only" mode.
- **L — blind-keeper episode clock not reset on absent/stale gaps** — `watcher.go:1173` (RU-13).
- **L — gauge parse-error branch omits the warnArmed/warnFired reset** — `watcher.go:1082` (RU-13).
- **L — `AwaitModelDone` ignores `EvSessionChanged`** — `step.go:356` (RU-13).
- **L — `newCycleIDGen` sequence is per-generator** -> two Cyclers same second mint colliding cycle IDs — `cycle.go:491` (RU-13).

### hook / hookrelay / hooksystem (RU-14 — DEEP re-review; the `agent_ready` lost-wakeup is H13 above)
- **M — hookrelay retry never covers the socket-absent startup race** — `hookrelay.go:544` (RU-14). Retry entered only on `daemon_not_ready` ACK; a `DialContext` failure (socket not yet listening — the normal cold-boot / in-place-swap case per `docs/daemon-redeploy.md`) returns immediately as `bridge_dial_failed` with no retry -> the whole 25s backoff/startup-window budget is unreachable for the connect phase, defeating CHB-016. Fix: treat connection-refused/ENOENT dial errors as retryable within `wallMax`.
- **L — dispatcher derefs `cp.Payload.Hook` unconditionally, but `LookupByTrigger` can also return Gates** — `hooksystem/dispatcher.go:147` (RU-14 deep). `MapRegistry.LookupByTrigger` (core/cpregistry_hka8bg2.go:178) returns any `KindGate||KindHook` CP whose `Trigger.Name` matches; a Gate's `Payload.Hook` is nil. Today the `on_`-prefixed hook namespace and gate attach-point encodings don't collide, so it's not reachable — but there is no `cp.Kind==KindHook` guard, so the moment a gate trigger ever overlaps the hook namespace it's a nil-pointer panic in the observer goroutine that (via `OnPanicRecoverAndLog`) silently aborts every later hook in the sorted chain — a fail-open. Fix: filter to `KindHook` before sorting/dereferencing.
- **L — `truncate4KiB` can emit an invalid trailing UTF-8 rune** — `hookrelay.go:422` (RU-14).
- **L — `CloseHookSession` doesn't wake `WaitForOutcome` waiters** — `sessionstore.go:158` (RU-14).
- **L — `session.closed` never set true; guard + doc are dead/misleading** — `sessionstore.go:84` (RU-14).
- **L — [recovered] `computeHookEnvelopeHash` derefs `cp.Evaluator.DelegationPath` with no nil guard** — `hooksystem/cognition_cp017.go:78` (RU-14).

### workflow / dot
- **M — `SubstituteTemplateParams` dead product code kept alive by its own tests** — `workflow/params.go:56` (RU-15). Live path is `substituteGraphParams`.
- **M — `LoadDotWorkflowWithPolicy` (CP-057 skills_ref) never called at runtime** — `workflow/loader.go:262` (RU-15). A spec obligation silently doesn't run.
- **M — Cap-hit salvage / resume-nudge branch on hardcoded node-ID `"commit_gate"`** — `dot_cascade.go:1122,851` (RU-02). A graph naming its gate anything else silently loses cap-hit salvage (tip stranded) and gate-failure feedback.
- **L — `policy_ref` rejection routed by `strings.Contains(err,"CP-056")`** — `workflow/loader.go` (RU-15).
- **L — edge-condition operator split ignores quoted RHS** — `dot/parser.go:1036` (RU-15).
- **L — bare-node statement overwrites a declared node's attributes with an empty map** — `workflowvalidator/dotparser.go:172` (RU-15) (dead package, but a copy trap).
- **L — Condition bridge keys evaluator lookup by raw condition string, not edge identity** — `workflow/dispatcher.go` (RU-15).

### cmd/harmonik (RU-16a/16b — 16b thin)
- **M — `run()` in main.go is a ~700-line god-function with ~40 `os.Args[1]==` branches** — `main.go:148-923` (RU-16a). No dispatch table; already produced the shipped-but-unwired confirm/veto commands (C1).
- **M — Default 64KB bufio.Scanner aborts on large-but-valid event lines (multiple CLIs)** — `smoke.go:381`, `goalkeeper_cmd.go:156`, `run_via_daemon.go`, `orphansweepbeads.go:269` (RU-16a, RU-12x). A big reviewer_verdict fails the smoke run; a long operator directive hard-fails every `goal-keeper` run (starving flywheel realign of ALL directives).
- **M — smoke Signal 3 greps for a `Refs:` trailer the smoke task never writes** — `smoke.go:239,534` (RU-16a). A correct commit reports FAIL -> smoke exits 1/2 on a healthy daemon.
- **M — queue-already-active append fallback attributes an unrelated group's outcome** — `run_via_daemon.go:214` (RU-16a). Exit code reflects ALL group-0 beads, not the caller's.
- **M — eval collect appends with no dedup — re-run double-counts the training set** — `eval_cmd.go:173` (RU-16b).
- **M — Bootstrap/destructive CLIs silently ignore unknown flags** — `init_cmd.go:93`, `branch_reap_cmd.go:36` (RU-16b). A mistyped `--target-branc` bootstraps against `main` with exit 0; a mistyped `--dryrun` makes `gc branches` perform a live `git branch -D`.
- **M — G5 test-file-touch guardrail false-positives on legitimately added tests** — `eval_guardrails_lygpp.go:76` (RU-16b).
- **M — `decisionsClientProjection` is production-file code used only by tests** — `decisions_k4.go:548` (RU-16a).
- **L — SH-002 wrong-extension rejection misses uppercase `.YML`** — `harness.go:719` (RU-16a).
- **L — double-SIGINT hard-exit is racy** — `harness.go:340` (RU-16a).
- **L — `crew start` performs irreversible host side effects before the daemon RPC** — `crew.go:304` (RU-16b).
- **L — `evalDeriveTaskID` strips only the final `-` segment** -> wrong taskID runs against a nonexistent dir — `eval_metrics_cmd.go:211` (RU-16b).
- **L — hidden-test pass count inflated by subtests and `-run` substring match** — `eval_metrics_cmd.go:350` (RU-16b).
- **L — `schedule add` accepts `--catchup-window` without validating it as a Go duration** — `schedule.go:186` (RU-16b).
- **L — `insertRCPrefixLine` anchors on any matching field, not the `daemon:` block, writes unquoted** — `migrate_rc_prefix_cmd.go:197` (RU-16b).
- **L — [recovered] human digest slices `EventID[:8]`/`RunID[:8]` with no length guard** -> panic on a short id — `digest.go:243` (RU-16b).

### supervise / workers / promote
- **M — RunHealthCheck flips the single registry worker for every configured worker** — `workers/health.go:82` (RU-04). Uses `SetEnabled` instead of `SetEnabledByName`; latent while capped at one worker.
- **M — Worker CLI overrides only ever applied to Workers[0]** — `workers_boot.go:23` (RU-17).
- **L — build gate re-runs on every push retry even when the tree is unchanged** — `promote_cmd.go:370` (RU-17).
- **L — temp worktree cleanup swallows all errors -> leaks registered worktrees** — `promote_cmd.go:306` (RU-17).
- **L — bare `--project=` (empty value) falls through to cwd silently** — `remote_control_prefix_cmd.go:40` (RU-17).
- **L — [recovered] bead-ID auto-detection error fully swallowed, no diagnostic** — `promote_cmd.go:349` (RU-17).

### brcli / beads
- **M — br exit-code 1 hard-mapped to BrNotFound -> spurious Cat-3 divergence dispatch** — `brerror.go:124` (RU-18).
- **L — `ShowBead` treats a 0-element success array as schema mismatch, not not-found** — `show.go:133` (RU-18).
- **L — `time.After` timer in backoff select leaks until fire when ctx cancelled** — `dblockretry.go:190` (RU-18).
- **L — sentinel path built from bead ID verbatim with no separator/traversal guard** — `beadsownedsentinel.go:50` (RU-18).
- **L — `TerminalOpReset`/`TerminalOpReopen` re-issue byte-identical br argv** — `reissueintent_bi031.go` (RU-18). A reset that should clear assignee under-applies.
- **L — `ReopenBead` doc says `br reopen --reason` but code issues `br update --status open --notes`** — `terminaltransition_bi010.go:336` (RU-12x).
- **L — [recovered] escalation diagnostic reports empty stderr** for the BrUnavailable-timeout path — `dblockretry.go:152` (RU-18).

### runexec / mergeq / run
- **M — Strict-FIFO merge ordering rests on undocumented Go channel sendq behavior** — `mergeq/mergeq.go:66` (RU-03). RSM-002/012 relies on blocked-sender release order the Go spec does NOT guarantee. Fix: carry an explicit intake sequence number.
- **M — FIFO test proves only the sleep-tuned choreography it constructs** — `mergeq_test.go` (RU-03). Never exercises genuinely-concurrent submits.
- **M — `RunState`/`run.go` fragile god-module: correctness is "byte-exact strings preserved as data"** — `runexec/run.go` (RU-03). A label typo silently changes an observable outcome string.
- **L — ReadyTimeout->Failed leaves the kill-reap timer armed (timer leak)** — `dispatch.go:268` (RU-03).
- **L — registry atomic write not durable, not safe for concurrent same-runID writers** — `registry.go:52` (RU-03).
- **L — single-mode terminal latches path label only when Detail!=""** — `run.go:214` (RU-03).
- **L — [recovered] `List` silently drops registry records that fail to load** — `run/registry.go:121` (RU-03).

### workspace (merge / conflict / lease)
- **M — Squash-conflict "detector" mutates the worktree and never resets** — `mergedispatch_wm018a.go:135` (RU-04b). On conflict no `git reset --hard`, `--squash` sets no MERGE_HEAD -> left with conflict markers + half-staged index; success path leaves a staged squash a later real merge double-applies.
- **M — Lease-lock write is unconditional overwrite, not test-and-set** — `leaselock.go:47` (RU-04b). Two processes leasing the same path both "succeed." Masked today by run_id-unique paths.
- **L — `createworktree.go:276`** `break` inside `select` breaks only the select, not the retry loop, on ctx cancel (RU-04).
- **L — `cleanupPartialState` uses the possibly-cancelled ctx** — `createworktree.go:213` (RU-04).
- **L — base64 content inlined into the remote command line** -> ARG_MAX risk — `remotematerialize.go:78` (RU-04).
- **L — interrupt/released markers hand-build JSON with `fmt %q`** — `leaselock.go:209` (RU-04b).
- **L — porcelain untracked-file parsing breaks on quoted/renamed paths, no `-z`** — `wipcapture_rc019.go:118` (RU-04b).
- **L — inconsistent local/remote runner classification across the `*Via` surface** — `diffhash.go:47`/`remotematerialize.go` vs `reviewverdict.go:162`/`autostatusmarker.go:96` (RU-04).

### scenario / specaudit / eval
- **M — Symlinked parent dir escapes scenario workspace guard** — `asserteval.go:300` (RU-22). `checkSymlinkSafety` Lstats only the leaf -> a symlinked intermediate dir reads arbitrary host files, defeating SH-022 isolation.
- **L — `jsonValuesEqual` only equates float64** -> YAML-decoded int `payload_match` values never match — `asserteval.go:167` (RU-22).
- **L — `TestSHINV005ImplementationAudit` asserts a source substring, not behavior** — `sh_inv005_declarative_loadable_test.go` (RU-21).

### usage / structuredlog / sentinel / misc (RU-23 thin)
- **M — [recovered] structured-log `Handle` after `Close` nil-derefs the log file** — `structuredlog/handler.go:264` (RU-23). Panic during shutdown/teardown ordering.
- **L — Orchestrator-session discovery hardcodes the harmonik repo path and `$USER`** — `usage/usage.go:424` (+sessiondata.go:623) (RU-23). Any other checkout path yields zero orchestrator sessions -> `harmonik usage` under-reports burn.
- **L — `DefaultHarmonikPath` hardcodes `/Users/gb/go/bin/harmonik`** — `workers/workers.go` (RU-04). Any non-`gb`/non-macOS worker falls back to a nonexistent binary.
- **L — `EmitTrip` appends to events.jsonl directly, bypassing the eventbus writer** — `trip_ev043b.go:446` (RU-23).
- **L — EV-041 tracker re-triggers the same bead on every heartbeat after threshold, unbounded** — `gitdone_ev041.go:127` (RU-23).
- **L — watch ledger seen-set grows unbounded for the always-on session (memory leak)** — `ledger.go:180` (RU-23).
- **L — OUTPUT persistWriter buffers an unbounded partial line when no newline arrives** — `sessioncapture.go:219` (RU-23).
- **L — `knownSessionIDs` created + threaded but never populated** — `usage/usage.go:275` (RU-23).
- **L — redundant if/else where both branches are identical** — `sentinel/governor.go:517` (RU-23).
- **L — context entries with `as:instruction` pass validation but are silently dropped from the boot doc** — `agentmanifest/brief.go:74` (RU-23).

### keeper hook scripts (RU-24)
- **M — Path-traversal: keeper statusline & stop hooks lack the guard their siblings have** — `keeper-statusline.sh:65`, `keeper-stop-hook.sh:56` (RU-24). AGENT from env/tmux session name with no `*/*|*..*` reject -> writes land outside `.harmonik/keeper`. The statusline runs on every UI repaint.
- **L — `HARMONIK_KEEPER_1M_EFFECTIVE_FRACTION` interpolated unescaped into an awk program** — `keeper-statusline.sh:121` (RU-24).
- **L — `.ctx.tmp.$$` orphaned if the script dies between write and rename** — `keeper-statusline.sh:143` (RU-24).

### specs / spec-registry
- **M — Nine specs declare a requirement-prefix absent from the prefix registry** — `specs/_registry.yaml:24` (RU-25). CI, DC, FW (NORMATIVE), HD, PR, RP, SW, SS, WG — the duplicate-prefix lint can't catch a colliding re-reservation.
- **L — header says "~79 constants" but 179 EventType constants defined** — `core/eventtype.go:6` (RU-10b).

---

## (d) Coverage-confidence table (per RU)

| RU | Scope | Depth | Note |
|----|-------|-------|------|
| RU-01a | workloop.go 1-4100 | deep | two god-fns fully engaged |
| RU-01b | workloop.go 4100-end + 5 files | THIN | self-noted: 4000-line workloop body read only selectively |
| RU-02 | dot_cascade/reviewloop/gate/verdict | THIN | read dot_cascade to ~1693/2669, reviewloop to ~997/2000; god-fn tails unaudited; its own HIGH (:151) was dropped by the condenser |
| RU-03 | runexec/mergeq/run | deep | thorough |
| RU-04 | workspace remote/worktree + workers/tmux | deep review / THIN survival | good review but both its HIGHs (H4/H5) were dropped downstream |
| RU-04b | workspace merge/conflict/lease | deep | strongest workspace pass |
| RU-05a / 05b | tmux substrate/adapter | deep | dense, specific |
| RU-06 | daemon/config/socket/router | deep | |
| RU-07 | codex driver/wire/twin | deep | |
| RU-08 | substrate/handler/handlercontract | DEEP (was PARTIAL) | re-reviewed line-by-line over the ~50 HC-xxx contract files pass-1 only sampled; deep pass found what the first missed — 2 HIGH (A9 systemic unwired-contract + its members), 4 MEDIUM, 6+ LOW. `substrate/` confirmed clean. Evidence the re-review was load-bearing. |
| RU-09 | queue rpc/append/state/persist | deep | |
| RU-10 | core eventregistry | deep | |
| RU-10b | core 237 type/payload files | THIN | explicit skim/classify over ~26k LOC; dead/deferred-typed fields likely under-counted |
| RU-11 | eventbus | deep | |
| RU-12 / 12x | lifecycle sweeps + reconcile-close | deep | |
| RU-13 | keeper watcher/step/cycle | deep review | its MEDIUM nil-deref (step.go:447) was dropped by the condenser |
| RU-14 | hook/hooksystem/hookrelay/policy/orchestrator (13 files) | DEEP (was THIN) | re-reviewed in full; the "suspiciously clean" `internal/policy/` and `internal/orchestrator/` were confirmed genuinely clean pure reducers, but the real seams yielded a new HIGH lost-wakeup (`sessionstore.go:285`, H13) + a dispatcher nil-deref fail-open the thin pass missed. Deep pass was load-bearing. |
| RU-15 | workflow/dot + validators + goalstate | deep | strong dead-code catches |
| RU-16a | cmd/harmonik core verbs (18 files) | deep | found the dead confirm/veto surface |
| RU-16b | cmd/harmonik eval + lifecycle verbs (20 files) | THIN | dashboard/ops_monitor/release/sync_assets/keeper_*/resolve_*/sleepwake/asset_manifest only skimmed |
| RU-17 | supervise/workers_boot/promote | deep | |
| RU-18 | brcli (20 files) | deep | |
| RU-19 | keepertest/codexdigitaltwin | deep | classify-lens, sound |
| RU-20 | operatornfr | deep | decisive delete verdict |
| RU-21 | specaudit | deep | |
| RU-22 | scenario harness | deep | three strong dead-subsystem catches |
| RU-23 | 37-file supporting grab-bag | THIN | broad-but-shallow; many packages zero findings; dropped its own MEDIUM nil-deref |
| RU-25 | 39 specs / ~2.5MB | THIN | sampled; verification hand-done on highest-risk clauses only |

**Thin-coverage units to treat as unproven, not clean:** RU-16b, RU-23, RU-10b, RU-01b, RU-02, RU-25. (RU-08 and RU-14 have since been DEEP re-reviewed — both contained real bugs, confirming the re-review was worth doing; `internal/policy/` and `internal/orchestrator/` are now confirmed genuinely clean.)

**Critic note:** the first-pass condensers' failure mode was *omission*, not misranking — 21 real findings (3 HIGH, 2 MEDIUM nil-derefs, 16 LOW/nit) fell out entirely, concentrated in RU-04 vs RU-04b conflation and a policy of dropping efficiency/error-swallow LOWs. All folded back in above, tagged [recovered]. No finding was buried that should have been Critical.
