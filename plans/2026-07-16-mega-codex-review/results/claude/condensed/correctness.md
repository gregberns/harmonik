# Condensed Review — Correctness / Concurrency / Data-Integrity Lens

Scope of this lens: bugs that can actually break in production — wrong results, silent data loss, races, deadlocks, goroutine/resource leaks, integrity violations. Deduped and ranked most-severe first. Maintainability-only, spec-drift-only, and pure test-theater findings are excluded except where they create a real runtime hazard. Source RU cited on each item.

---

## CRITICAL

### C1. Operator verdict control surface is dead — no daemon handler registered
`cmd/harmonik/confirm_verdict.go:131`, `veto_verdict.go:163` — **RU-16a**
`confirm-verdict` / `veto-verdict` send socket ops (`confirm`, `veto_verdict`) that `buildSocketRouter` (socketdispatch.go:362-386) never registers. The daemon returns "unknown op" (code 0), CLI exits 1.
**Failure:** a daemon genuinely blocked on a `confirm_required` reconciliation verdict can NEVER be confirmed or vetoed through the shipped, documented commands — the entire RC-027 operator control path is non-functional end-to-end. A stuck run stays stuck.

---

## HIGH

### H1. Data loss: gitignore hygiene commits onto the operator's current branch
`internal/workspace/gitignorehygiene.go:199` — **RU-04b**
`gitignoreCommit` runs `git add`+`git commit --allow-empty` against whatever HEAD points at, never checking out the dedicated `harmonik/gitignore-init` branch its docstring/WM-013e promise. At daemon startup HEAD is usually `main` / the operator's working branch.
**Failure:** a harmonik daemon-state commit is silently injected onto the user's branch (the exact "daemon state leaking into user commits" outcome WM-013e exists to prevent). `--allow-empty` forces a commit even with nothing to record.

### H2. Data loss: corrupt lease-lock downgraded to "no lock" → active worktree garbage-collected
`internal/workspace/discoverworktrees.go:161` (+ `orphansweep.go:206`, `:238`) — **RU-04b**
`DiscoverWorktrees` discards read/parse errors from `readDiscoveredLeaseLock` (`err==nil` guard), so a corrupt/truncated `lease.lock` becomes indistinguishable from absent → `LeaseLock=nil` → classified `NoLock`. `RemoveAgedNoLockWorktrees` then force-`git worktree remove --force --force`s it once aged.
**Failure:** a live run whose lease.lock got truncated by a crash-mid-fsync (the exact WM-003a scenario) is GC'd out from under an active session — uncommitted work discarded. Compounded by H2b below.

### H2b. Aged-worktree reaper uses directory mtime as "inactive" proxy
`internal/workspace/orphansweep.go:206` — **RU-04b**
Dir mtime only bumps on entry add/remove; an agent editing files in place or writing into subdirs never bumps the top-level worktree mtime. `--force --force` discards uncommitted work.
**Failure:** an actively-in-use worktree reads as "aged" and is destroyed. Blast radius stacks with H2.

### H3. Cat 3c auto-close fabricates done-status for reverted/superseded work
`internal/lifecycle/orphansweepbeads.go:248` (+ silent close `:700`) — **RU-12x**
`HasMergeCommitForBead` scans the target branch's ENTIRE history for any commit ever carrying the bead's trailer with a non-docs diff — no check the change is still in the current tree. `SweepCloseBead` then closes with NO needs-attention marker.
**Failure:** work merges, is later found broken and `git revert`ed (or superseded); the trailer commit remains in history, so the next startup sweep silently marks the still-in_progress bead DONE for work no longer on the branch. Self-hiding: closed beads drop out of `br list --status in_progress`. Data-integrity corruption of the ledger.

### H4. queue-submit read-modify-write bypasses the B1 mutation lock (lost update + single-active TOCTOU)
`internal/queue/rpc.go:161` — **RU-09**
`HandleQueueSubmit` does Load→Validate(QM-027 single-active)→Persist entirely UNLOCKED; the B1 fix only covered the append path.
**Failure:** two concurrent submits for the same new queue name both pass the single-active check, both mint distinct queue_ids, both Persist to the same `<name>.json` — last-writer-wins silently drops one queue. Submit Persist can also clobber the workloop's in-flight locked status Persist. Defeats the single-active-per-name invariant.

### H5. Global WaitGroup misuse in eventbus can hard-abort the process
`internal/eventbus/busimpl.go:505` — **RU-11**
Every `Emit*` calls `b.wg.Add(1)` before spawning; `Drain` spawns a goroutine calling `b.wg.Wait()`. The per-run WaitGroup is carefully sealed against this exact hazard; the global `b.wg` is not.
**Failure:** if `Drain` runs (counter momentarily 0) concurrently with an `Emit` doing `Add(1)`, Go aborts the whole process: `fatal error: sync: WaitGroup misuse: Add called concurrently with Wait`. Any shutdown that drains while producers still emit is exposed.

### H6. Stale reconciliation-lock probe releases flock BEFORE unlink (RC-002a violation)
`internal/lifecycle/orphansweep.go:834` — **RU-12**
`reconLockIsStale` does `flock(LOCK_EX|LOCK_NB)` then immediately `flock(LOCK_UN)` and returns stale; the unlink happens much later (`:797`). Between unlock and unlink another daemon can acquire the same lock and start a live reconciliation, which the sweep then unlinks.
**Failure:** boot orphan-sweep races a freshly-launched reconciliation for the same run → TWO concurrent reconciliations for one target_run_id, exactly the "at most one in-flight" invariant the lock protects.

### H7. Remote-session Kill sends SIGTERM/SIGKILL to a local PID
`internal/daemon/tmuxsubstrate.go:2492` — **RU-05a**
`Kill()` calls `killProcessWithGrace(s.pid, ...)` unconditionally with local `syscall.Kill`. For a remote session `s.pid` is the worker's pane PID — meaningless on the daemon host. `runWait` guards every liveness check on `!s.remote`; `Kill` has no such guard.
**Failure:** a remote run finishes, workloop calls Kill, and SIGTERM/SIGKILL fire at whatever local daemon-host process happens to occupy that numeric PID (collisions common for low/mid PIDs) — can terminate an arbitrary unrelated host process.

### H8. JSON-RPC id typed `*int64` — a string id crashes the whole session
`internal/codexwire/codexwire.go:171` — **RU-07**
JSON-RPC 2.0 permits string ids; codex/peers may use them. A non-integer id fails envelope `Unmarshal` → `Parse` error → readLoop treats it as a FATAL decode → session torn down to Exited.
**Failure:** a single unexpected id shape kills an otherwise-healthy session, violating the package's stated forward-compat/round-trip contract on the one field JSON-RPC explicitly allows to vary.

### H9. BI-010c workflow-label write guard bypassed by `--flag=value` argv form
`internal/brcli/workflowlabelwrite_bi010c.go:90` — **RU-18**
`CheckWorkflowLabelWrite` matches `strings.HasPrefix(arg, "workflow:")` on standalone argv elements only. The joined form `--label=workflow:dot` (valid argv) has prefix `--label=`, so the guard returns nil.
**Failure:** the forbidden agent-context workflow-label mutation (BI-INV-001 authority guard) proceeds silently through a trivially-reachable encoding.

### H10. runShell busy-spins a CPU core on ctx-cancel with no armed timer
`internal/daemon/runshell.go:342` — **RU-01b**
`fireOnCancel` is a no-op when `sh.timers` is empty; the outer `drive()` loop only exits on `!InFlight()`, not on ctx cancellation, and nothing closes the tap on cancel.
**Failure:** if the Run machine is still InFlight with no timer armed and the parent ctx is cancelled, `driveOnce` is called repeatedly forever — hot-loops burning a full core, machine never advances. Latent today only because production drive is exercised only with `context.Background()`.

---

## MEDIUM

### M1. PID-reuse races kill unrelated processes (three sites)
`internal/workspace/orphansweep.go:238` (isPIDDead), `internal/lifecycle/orphansweepbr.go:166` (+ `orphansweep.go:360`, `agentwatcherreap.go:215`) — **RU-04b, RU-12**
Two related hazards: (a) `isPIDDead` decides staleness purely by `syscall.Kill(pid,0)`, so after the daemon dies and the OS recycles its PID, an orphaned lease.lock is treated as live forever and the workspace stays wedged "leased" to a dead run. (b) In the orphan-br/handler/watcher sweeps, an unbounded window exists between the final liveness poll and the `SIGKILL`; the target can exit and its PID be recycled, so SIGKILL hits an innocent process on a busy host.

### M2. Default 64KB bufio.Scanner aborts on large-but-valid event lines (multiple CLIs)
`cmd/harmonik/smoke.go:381`, `goalkeeper_cmd.go:156`, `run_via_daemon.go` (also `orphansweepbeads.go:269` git-log scan) — **RU-16a, RU-12x**
Several scanners use the default 64KB `bufio.MaxScanTokenSize` while `run_via_daemon.go:303` bumps it to 512KB. A single NDJSON event/directive line >64KB makes `Scan()` stop with `ErrTooLong`.
**Failure:** a big-but-valid reviewer_verdict/run_completed event spuriously fails the smoke run (exit 1); a long operator directive makes every `goal-keeper` run hard-fail, silently starving the flywheel realign of ALL directives; a large git-log line can drop a real trailer match.

### M3. Path-traversal: keeper statusline & stop hooks lack the guard their siblings have
`cmd/harmonik/assets/scripts/keeper-statusline.sh:65`, `keeper-stop-hook.sh:56` — **RU-24**
`keeper-precompact-hook.sh` and `keeper-sessionstart-hook.sh` reject agent names matching `*/*|*..*`; the statusline and stop hooks derive AGENT the same way (env / tmux session name / positional) but build `${AGENT}.ctx` / `${AGENT}.idle` with no guard.
**Failure:** a tmux session name or env value with `/` or `..` (e.g. `../../../tmp/x`) makes the atomic write/`touch` land outside `.harmonik/keeper`. The statusline runs on every UI repaint — the most frequently executed hook.

### M4. Argv joined without shell-quoting — reintroduces hk-rpr6 argv shattering
`internal/daemon/tmuxsubstrate.go:1680` (SpawnCrewSession), `:1772` (SpawnRunSession) — **RU-05a**
`spawnWindowVia` shell-quotes every argv element (tmux new-window feeds `sh -c`), but these two paths use a bare `strings.Join(argv, " ")`.
**Failure:** any crew/run argv element containing a space (multi-word system-prompt/seed, path/flag value with spaces) is re-word-split by `sh -c`, so claude/crew receives a mangled argv and mis-parses or exits-2.

### M5. Remote windows leak — KillAllWindows uses the local adapter
`internal/daemon/tmuxsubstrate.go:991` — **RU-05a**
Remote spawn handles are appended to `s.spawnedHandles`, but `KillAllWindows` calls `s.adapter.KillWindow` (box A's local adapter), not the per-run SSH remoteAdapter.
**Failure:** on wave completion / daemon exit remote worker windows are never cleaned up and accumulate across daemon lifetimes on the worker (or worse, kill a same-named local window).

### M6. Squash-conflict "detector" mutates the worktree and never resets
`internal/workspace/mergedispatch_wm018a.go:135` — **RU-04b**
`DetectSquashMergeConflict` runs a real `git merge --squash`, staging into index/worktree; on conflict it returns with no `git reset --hard`, and `--squash` sets no MERGE_HEAD so `--abort` can't recover.
**Failure:** the worktree is left with conflict markers + half-staged index; subsequent detect calls fail "dirty tree" masking true state, and even the success path leaves a staged squash that a later real merge double-applies.

### M7. Lease-lock write is unconditional overwrite, not test-and-set
`internal/workspace/leaselock.go:47` — **RU-04b**
`WriteLeaseLockAtomic` renames onto target unconditionally — no O_EXCL, no unheld-check. Atomicity guarantees only a non-torn file, not exclusive acquisition.
**Failure:** two processes leasing the same workspace_path both "succeed", each believing it holds the lease. Masked today by run_id-unique paths, but any shared/reused path (reviewer worktrees, retried run_ids) gets a silent double-lease.

### M8. Persist temp file keyed only on PID — concurrent in-process writes collide
`internal/queue/persistence.go:110` — **RU-09**
`tmpPath = target.tmp-<pid>` — two concurrent `Persist` calls in the same daemon for the same queue produce identical tmpPath; the O_EXCL second call fails `ErrPersistFailed`.
**Failure:** spurious internal-error (-32099) to the caller. Reachable now via the unlocked submit path (H4) racing a locked append/workloop Persist.

### M9. Unbounded group-index access in append path can panic the socket handler
`internal/queue/rpc.go:510` (+ `append.go:124`, `:166`) — **RU-09**
`mutated.Groups[req.GroupIndex]` with no bounds check; `req.GroupIndex` comes straight from decoded JSON, relying entirely on Validate having rejected out-of-range first.
**Failure:** any regression/skip in GroupIndex validation panics on index-out-of-range instead of returning typed `append_target_invalid`, taking down the socket handler goroutine.

### M10. PlanForVerdict panics on an unknown verdict derived from event data
`internal/core/verdictexecution_rc025.go:223` — **RU-10b**
Default branch `panic`s. VerdictEvent values originate from decoded bus/event-log payloads.
**Failure:** a forward-compat/newer-emitter verdict string or any caller forgetting the "validate first" contract turns a data condition into a hard daemon-goroutine crash.

### M11. Two of six reconciliation verdict actions recorded as executed but do nothing
`internal/daemon/verdictexecutor_rc025a.go:439` — **RU-02**
`resume-here` / `reset-to-checkpoint` return nil (TODO "not yet wired"), then flow into `commitVerdictExecuted` + emit `reconciliation_verdict_executed`.
**Failure:** the system durably records the verdict as EXECUTED (commit trailer + event) while performing no re-dispatch/rollback; operators/consumers believe the run resumed or rolled back, and the Cat 3b idempotency guard never retries it. Silent no-op.

### M12. Eager-refill target-branch bugs re-dispatch already-landed work
`internal/daemon/eagerfill_em063.go:251` (hardcoded origin/main) & `:252` (substring `--grep` without `--fixed-strings`) — **RU-01b**
(a) Phase-2 "already landed" check greps a literal `main` while the run lands on `deps.targetBranch` (this repo is on a non-main integration branch); it never detects the landed bead → eager-refill re-appends completed beads (duplicated work). (b) The grep is an unanchored regex substring, so a bead id that is a prefix of a landed longer id is wrongly classified as landed and silently dropped. The sibling `beadOnOriginMain` does both correctly; the two "has it landed?" checks disagree.

### M13. queue-already-active append fallback attributes an unrelated group's outcome
`cmd/harmonik/run_via_daemon.go:214` — **RU-16a**
On `queue_already_active`, the run's beads are appended to group 0 of the pre-existing queue and the CLI blocks on `queue_group_completed` for the whole group.
**Failure:** group 0 may already hold other submitters' in-flight beads; the event's aggregate final_status reflects ALL group-0 beads, so `harmonik run` returns exit 1 when another submitter's bead fails even though the caller's own beads succeeded (or vice-versa). Exit code no longer reflects the caller's work.

### M14. Watcher-synthesized agent_failed events carry no session_id/run_id
`internal/handlercontract/watcher_hc011.go:670` — **RU-08**
`buildWatcherFailedPayload` discards sessionID (`_ = sessionID`); every watcher self-defect terminal (panic barrier, line-too-long, framing/JSON error, HC-061) emits with no envelope correlation.
**Failure:** a wedged/crashed session surfaces an unattributable failure event; daemon/ops-monitor/reconciler cannot correlate it to the run/session — failures can't be attributed or auto-recovered.

### M15. SendInput ignores ctx; stdin write can block forever
`internal/handler/session.go:370` (contract asserted at `handler.go:320`) — **RU-08**
Signature discards ctx; body is a bare `io.WriteString` to the stdin pipe, while `handler.Launch` documents "ctx bounds the write".
**Failure:** if the child never drains stdin and the ~64KiB pipe buffer fills (large HandlerSpec, wedged child), the write blocks forever, the delivery goroutine leaks, CloseStdin is never reached — a latent hang the surrounding code believes is bounded.

### M16. config.yaml block-absent sentinels ignore most fields → operator config silently dropped
`internal/daemon/projectconfig.go:1287` (watch, 2 of 9) & `:1290` (pi harness, 4 of 9) — **RU-06**
When `schema_version` is omitted, the empty-file sentinel returns a zero-value config only if every block reads absent; but `watchBlockAbsent`/`harnessesAbsent` only test a couple of fields.
**Failure:** a config with only `watch.absent_thresh_s` (or only `harnesses.pi.base_url`) and no `schema_version` is treated as an empty file and silently discarded — the daemon boots on defaults with the operator's tuning lost, instead of failing fast with ErrUnsupportedConfigVersion.

### M17. Server-originated JSON-RPC requests misclassified and silently dropped
`internal/codexwire/codexwire.go:211` — **RU-07**
Parse keys on `hasID && hasMethod` → ClientRequest, but the codex app-server sends REQUESTS to the client (exec/apply-patch approval prompts) that also carry id+method; they fall into handleFrame's ignored default.
**Failure:** a turn needing an approval response hangs on the server with no client reply and no InputAck timer backstop — the turn stalls until CloseInput/ctx.

### M18. RunHealthCheck flips the single registry worker for every configured worker
`internal/workers/health.go:82` — **RU-04**
Loops over `cfg.Workers` but calls `reg.SetEnabled` (acts on the one registry worker) instead of the existing `SetEnabledByName`.
**Failure:** latent (Version-1 caps at one worker) but the moment the cap lifts, health verdicts clobber each other — a failing worker A gets re-enabled by a passing worker B.

### M19. queue.Persist (fsync) runs under the LockForMutation write lock
`internal/daemon/workloop.go:2193` (also 2472/2574/2617/2638/2813/2962) — **RU-01a**
Disk marshal+write+fsync happens while holding the in-memory-state write lock.
**Failure:** every per-run goroutine needing the lock (run-completion accounting via `evaluateGroupAdvanceWithOutcome`) blocks for full disk-write latency each poll tick; under N runs + multiple queues a poll tick becomes a serialized disk-I/O chain that stalls completion accounting.

### M20. adoptLiveRunSession goroutines untracked by the shutdown WaitGroup
`internal/daemon/workloop.go:1716` — **RU-01a**
Boot spawns one adopt goroutine per surviving session without `wg.Add`; `exitClean` waits only on `wg`. Their teardown (a `br` write / ReopenBead+queue-revert on ctx cancel) is neither awaited nor bounded by shutdownDrainTimeout.
**Failure:** on SIGTERM the process can exit mid-write, leaving a bead/queue-item in an inconsistent state (QM-002a must clean up) or racing the exit to emit onto a bus being torn down.

### M21. max-attempts branch leaves the item pending → potential dispatch live-lock
`internal/daemon/workloop.go:2599` — **RU-01a**
On `Attempts >= maxItemAttempts` the code sets LastFailureReason and `break`s but never sets Status=Failed (unlike the cross-queue-duplicate branch at 2569), relying on `evaluateGroupAdvanceWithOutcome` to fail it.
**Failure:** if that evaluator doesn't transition a pending item to failed, the item stays pending, is re-selected next tick, re-hits the `>=` guard — infinite dispatch-attempt spin that never advances the group.

### M22. Symlinked parent dir escapes scenario workspace guard
`internal/scenario/asserteval.go:300` — **RU-22**
`checkSymlinkSafety` Lstats only the leaf; if the leaf isn't a symlink it returns immediately, never running EvalSymlinks on intermediate components.
**Failure:** a fixture with a symlinked intermediate dir (`escape -> /etc`, path `escape/passwd`) passes the guard; `file_contents_equal/match` assertions then read arbitrary host files — defeats SH-022 isolation, host-data leakage into ActualValue.

### M23. Session orphan-detection hardcodes "zsh" as the idle-shell name
`internal/lifecycle/tmux/orphansession.go:161` — **RU-05b**
`countNonZshWindows` treats a window as idle only if named exactly `zsh`; tmux names windows after the running command basename.
**Failure:** on bash/fish/sh hosts (Linux CI, $SHELL override) an idle shell window counts as active workload, so orphaned daemon sessions are never reclaimed by condition 1 and leak/accumulate across restarts (defeats PL-006). Falls back to PID-liveness which itself skips on any PanePID read error.

### M24. Window-name truncation still exceeds the stated 64-byte max
`internal/lifecycle/tmux/windowname.go:106` — **RU-05b**
Truncation yields `raw[:56] + "~" + 8hex = 65 bytes` for the bead portion alone, before the 9-byte sentinel prefix and phase suffix — always ≥65, up to ~78 bytes, never satisfying the `windowNameMaxBytes=64` invariant it claims to enforce (`beadIDMaxBytes=56` is mis-sized).

### M25. Strict-FIFO merge ordering rests on undocumented Go channel sendq behavior
`internal/mergeq/mergeq.go:66` — **RU-03**
The central RSM-002/012 guarantee (merges execute strictly in arrival order) relies on blocked-sender release order, which the Go spec does NOT guarantee (stable but undocumented sudog queue).
**Failure:** a future runtime change could reorder merges with zero test failure until an ordering-sensitive race surfaces on main. The queue should carry an explicit intake sequence number rather than delegating ordering to the scheduler.

### M26. eval collect appends with no dedup — re-run double-counts the training set
`cmd/harmonik/eval_cmd.go:173` — **RU-16b**
Collect writes one record per run every invocation with no keying on run_id; report aggregates every line.
**Failure:** a routine re-`collect` over the same events.jsonl writes each run twice, skewing N, pass_rate, and median wall_time — silently corrupts the router training-set comparison table.

### M27. Bootstrap/destructive CLIs silently ignore unknown flags
`cmd/harmonik/init_cmd.go:93` (init) & `branch_reap_cmd.go:36` (gc branches) — **RU-16b**
Neither arg loop has a default/else branch.
**Failure:** a mistyped `--target-branc` makes `init` bootstrap the whole project against the default `main` with exit 0 and no diagnostic; a mistyped `--dryrun` makes `gc branches` perform a live `git branch -D` reap the operator believed was a preview.

### M28. G5 test-file-touch guardrail false-positives on legitimately added tests
`cmd/harmonik/eval_guardrails_lygpp.go:76` — **RU-16b**
`evalDiffTouchesTestFile` flags any `_test.go` hunk with no add-vs-modify or shipped-vs-new discrimination.
**Failure:** a solution that adds its own new test trips `test_file_touched=true`, feeding a false gaming signal that degrades the quality score the judge consumes.

### M29. Worker CLI overrides only ever applied to Workers[0]
`cmd/harmonik/workers_boot.go:23` — **RU-17**
`applyWorkerOverrides` writes `--worker-host`/`--worker-enabled` into `out.Workers[0]` regardless of count, with no selection flag or warning.
**Failure:** on a multi-worker config the override silently applies to only the first entry — partial, order-dependent misconfiguration when repointing a deployment.

### M30. br exit-code 1 hard-mapped to BrNotFound → spurious Cat-3 divergence dispatch
`internal/brcli/brerror.go:124` — **RU-18**
Exit 1 (a generic-error convention for many CLIs) is mapped unconditionally to BrNotFound, which routes to Cat 3 (store disagreement → investigator dispatch). High-level read methods re-parse the JSON envelope to protect themselves, but any caller consuming `Result.BrErr` directly inherits the misclassification.
**Failure:** a br version drift that uses exit 1 generically silently converts routine failures into spurious divergence-investigation dispatches.

---

## LOW (real but lower-impact)

- **RU-01a workloop.go:1668** — exitClean drain goroutine leaks a `wg.Wait()` goroutine when the drain times out.
- **RU-01a workloop.go:1202** — `clockAfter` spawns a non-cancellable sleep goroutine on `context.Background()`.
- **RU-01b runbridge.go:349** — `CloseBrUnavailable` maps to a success terminal without an actual bead close or `bead_closed` emission (integrity gap).
- **RU-01b dispatchsegment.go:196** — `resumeReadyProbe` fabricates `agent_ready` on a fixed 2s timer regardless of real REPL readiness.
- **RU-01b eagerfill_em063.go:80** — `eagerRefillEval` computes slot deficit from an inFlight snapshot read outside the append lock (TOCTOU).
- **RU-01b dispatchsegment.go:146** — blocking launch closure inside `launchAgent` not interruptible by ctx cancellation.
- **RU-03 dispatch.go:268** — ReadyTimeout→Failed on agent exit leaves the kill-reap timer armed (no CancelTimer) — timer leak.
- **RU-03 registry.go:52** — atomic write not durable and not safe for concurrent same-runID writers.
- **RU-03 run.go:214** — single-mode terminal latches path label only when Detail!="", silently degrading merge-failure strings on empty payload.
- **RU-04 createworktree.go:276** — `break` inside `select` breaks only the select, not the retry loop, on ctx cancel during backoff.
- **RU-04 createworktree.go:213** — `cleanupPartialState` uses the possibly-cancelled ctx → remote stale worktree/branch left behind.
- **RU-04 remotematerialize.go:78** — base64 content inlined into the remote command line → ARG_MAX risk for large briefs.
- **RU-04b leaselock.go:209** — interrupt/released markers hand-build JSON with `fmt %q` (Go-quoting, not JSON-quoting) → malformed JSON on special chars.
- **RU-04b wipcapture_rc019.go:118** — porcelain untracked-file parsing breaks on quoted/renamed paths, no `-z`.
- **RU-05a tmuxsubstrate.go:1143** — `callNewWindowBounded` orphans a tmux window created just past the timeout.
- **RU-05a tmuxsubstrate.go:2190** — `remoteAdapter` written/read without the sync used for `cachedPaneTarget` (data race).
- **RU-05b osadapter.go:541** — `WriteToPane` leaks the loaded tmux buffer when PasteBuffer fails.
- **RU-05b osadapter.go:617** — `parseTmuxMajorVersion` rejects tmux dev/master builds ("next-3.x").
- **RU-05b orphansession.go:93** — `SweepOrphanTmuxSessions` increments killed even when KillSession errored.
- **RU-05b subcommand.go:283** — tmux resolved from passed env PATH but Probe/EnsureSession use process PATH (inconsistent binary).
- **RU-06 socket.go:228** — `removeStaleSocket`→Listen TOCTOU window against a racing daemon.
- **RU-07 session.go:460** — `finalize()` unconditionally marks every wind-down as a launch failure.
- **RU-07 reactor.go:384** — mid-turn steer orphans the previous TurnID with no terminal.
- **RU-07 codexreactor/reactor.go:204** — EventTypeConnected leaves stale ThreadID.
- **RU-07 codexwire.go:379** — server response with explicit `"result": null` doesn't round-trip (null dropped).
- **RU-08 watcher_hc011.go:391** — readLoop observes ctx cancel only between scans, not while blocked in `scanner.Scan()`.
- **RU-08 session.go:406** — `Session.Kill` spawns a fresh `waitOwner.Wait` goroutine per call → accumulation under repeated Kill/ctx-cancel.
- **RU-08 replay.go:114** — `Twin.Events` replay goroutine leaks if consumer abandons the channel without cancelling ctx.
- **RU-09 rpc.go:1295** — `HandleQueueSetConcurrency` doc claims "validates N>=1" but no validation exists.
- **RU-10 eventregistry.go:69** — `secretPrefixRe` matches substrings without word boundaries ("auth" trips on Author/AuthorID) → over-redaction.
- **RU-11 busimpl.go:1504** — Drain/DrainRun leak the waiter goroutine when ctx is cancelled.
- **RU-11 busimpl.go:1220** — `Seal` sets sealed=true but nothing blocks a concurrent Emit during startup replay.
- **RU-11 jsonlwriter.go:209** — processBatch comment overstates O_APPEND atomicity for batched writes exceeding PIPE_BUF (interleaving risk).
- **RU-11 jsonlwriter.go:263** — Append holds `mu` across a blocking channel send → Close can stall behind a full queue.
- **RU-12 orphansweep.go:843** — `reconLockIsStale` treats an unparseable creator_pid as stale→removable, even for a just-created lock.
- **RU-12 startup_pl005_qm002.go:956** — `reconcileQueueTerminalState` returns done=true on CompleteAndUnlink failure, leaving an active-status file on disk.
- **RU-12x orphansweepbeads.go:269** — bufio.Scanner 64KB default can drop a real trailer match on large git-log output (see M2).
- **RU-12x terminaltransition_bi010.go:521** — `SweepCloseBead` does not re-check bead status before closing; trusts caller's stale list.
- **RU-13 watcher.go:1173** — blind-keeper episode clock (`blindSince`) not reset on absent/stale gaps.
- **RU-13 watcher.go:1082** — gauge parse-error branch omits the warnArmed/warnFired reset that other branches perform.
- **RU-13 step.go:356** — `AwaitModelDone` ignores `EvSessionChanged` → a mid-wait session flip clears into the wrong session.
- **RU-13 cycle.go:491** — `newCycleIDGen` sequence is per-generator; two Cyclers in the same process/second mint colliding cycle IDs.
- **RU-14 hookrelay.go:422** — `truncate4KiB` can emit an invalid trailing UTF-8 rune when cutting mid multi-byte sequence.
- **RU-14 sessionstore.go:158** — `CloseHookSession` doesn't wake `WaitForOutcome` waiters → no-outcome close blocks callers until ctx timeout.
- **RU-15 dot/parser.go:1036** — edge-condition operator split ignores quoted RHS; `!=`/`==` inside a single-quoted literal mis-parses.
- **RU-15 workflowvalidator/dotparser.go:172** — bare-node statement overwrites a previously-declared node's attributes with an empty map.
- **RU-16a harness.go:719** — SH-002 wrong-extension rejection misses uppercase `.YML`.
- **RU-16a harness.go:340** — double-SIGINT hard-exit is racy: both signal goroutine and interruptExit consume from sigCh.
- **RU-16b crew.go:304** — `crew start` performs irreversible host side effects before the daemon RPC that may fail exit 17.
- **RU-16b eval_cmd.go:181** — eval collect emits records in nondeterministic map-iteration order.
- **RU-16b eval_metrics_cmd.go:211** — `evalDeriveTaskID` strips only the final `-` segment; wrong taskID runs vet/hidden-test/deadcode against a nonexistent dir.
- **RU-16b eval_metrics_cmd.go:350** — hidden-test pass count inflated by subtests and `-run` substring match.
- **RU-16b schedule.go:186** — `schedule add` accepts `--catchup-window` without validating it as a Go duration.
- **RU-16b migrate_rc_prefix_cmd.go:197** — `insertRCPrefixLine` anchors on any matching field line, not scoped to the `daemon:` block, and writes the prefix unquoted.
- **RU-17 promote_cmd.go:370** — build gate re-runs on every push retry even when the tree is unchanged.
- **RU-17 promote_cmd.go:306** — temp worktree cleanup swallows all errors → can leak registered worktrees.
- **RU-17 remote_control_prefix_cmd.go:40** — bare `--project=` (empty value) falls through to cwd silently.
- **RU-18 show.go:133** — `ShowBead` treats a 0-element success array as schema mismatch rather than not-found.
- **RU-18 dblockretry.go:190** — `time.After` timer in backoff select leaks until fire when ctx cancelled.
- **RU-18 beadsownedsentinel.go:50** — sentinel path built from bead ID verbatim with no separator/traversal guard.
- **RU-22 asserteval.go:167** — `jsonValuesEqual` only equates float64 numbers; YAML-decoded int `payload_match` values never match.
- **RU-23 gitdone_ev041.go:127** — EV-041 tracker re-triggers the same bead on every heartbeat after threshold, unbounded.
- **RU-23 ledger.go:180** — watch ledger seen-set grows unbounded for the always-on session (memory leak).
- **RU-23 sessioncapture.go:219** — OUTPUT persistWriter buffers an unbounded partial line when no newline arrives.
- **RU-23 usage.go:776** — `dominantKey` tie-break nondeterministic (map iteration order).
- **RU-23 trip_ev043b.go:446** — `EmitTrip` appends to events.jsonl directly, bypassing the eventbus writer (interleaving/integrity).
- **RU-24 keeper-statusline.sh:121** — `HARMONIK_KEEPER_1M_EFFECTIVE_FRACTION` interpolated unescaped into an awk program (injection/crash surface).
- **RU-24 keeper-statusline.sh:143** — `.ctx.tmp.$$` orphaned if the script dies between write and rename.

---

## NITS (cosmetic / non-runtime)

- **RU-08 claudehandler_chb006_024.go:705** — `os.UserHomeDir` failure falls back to literal `~` → non-expandable transcript path.
- **RU-12 orphansweep.go:368** — `SweepOrphanHandlers` "killed" count includes processes already dead before SIGTERM (metric only).
- **RU-13 watcher.go:1104** — staleness check uses pre-heartbeat modTime → a just-refreshed gauge takes the stale branch for one tick.
- **RU-16b eval_metrics_cmd.go:296** — `evalGocycloMax` conflates "no gocyclo output" with a real max of 0.
- **RU-18 version.go:98** — observed version discards pre-release suffix → guaranteed mismatch warning if pinned carries one.
- **RU-19 l2_integration_test.go:163** — `drainTwin` 2s wall-clock idle timeout can produce a false failure under load/-race (test-only).
- **RU-21 ar025_agent_type_regex_test.go:79** — regex matches the first `agent_type :=` anywhere, not scoped to §6.1 (test-only).

---

## COVERAGE GAPS (failed / partial review units)

- **RU-08 — status: PARTIAL.** Reviewed the substrate/Handler-contract interface seam and the largest ~5k LOC of non-test source, but did NOT exhaustively line-audit the whole scope. Correctness/concurrency findings here (M14, M15, and three LOWs) are from a partial pass — the handler/substrate/handlercontract packages are not fully covered.

No review unit reported `status: failed`. All others reported `status: reviewed` (several with a stated focus-lens or selective-read note, but claiming full scope coverage).
