# hk-8dtiv — Close() errcheck coverage: migration recon (RECON ONLY)

**Decision (operator-locked):** MAXIMIZE Close() errcheck coverage. Drop the four `Close`
`exclude-functions` entries in `.golangci.yml` (keep `check-blank: true`), add ONE `_test.go`-scoped
errcheck exclusion so test noise stays zero, and migrate the production Close sites that then fire to
the house Close idiom. Config flip lands LAST.

All counts below are from the **pinned linter** `.tools/golangci-lint` **v2.3.0** (`built with go1.26.1`),
run against the working tree on branch `phase1-session-restart-substrate`. Where a claim comes from
plain `grep` instead of the linter it is labelled `[grep]`.

---

## 0. Headline numbers (linter-verified)

| Measurement | Count | Source |
|---|---|---|
| Baseline (current config, errcheck-only, full tree): prod Close findings | **0** | linter |
| Drop the 4 Close exclusions: prod Close findings | **99** | linter |
| Drop the 4 Close exclusions: `_test.go` Close findings | **251** | linter |
| **Delta = production Close sites that NEWLY fire** | **99** | linter (99 − 0) |
| — of which `internal/daemon/**` (DEFERRED, sequence with extraction) | **17** | linter |
| — of which non-daemon (this migration) | **82** | linter |
| With the 4 dropped **and** the `_test.go` exclusion added: prod Close | **99** | linter |
| With the 4 dropped **and** the `_test.go` exclusion added: test Close | **0** | linter |
| Raw `grep '\.Close()'` prod sites tree-wide (internal/ cmd/ tools/, non-test) | 266 | `[grep]` |

The gap between the 266 raw grep sites and the 99 linter-flagged sites is exactly what the config
comment predicts: most `.Close()` calls are on receivers matching the four dropped exclusions AND are
currently *checked* (folded into `errors.Join`, error-returned, or on the success path), or are on
receiver types the exclusions never covered. Only the **99** unchecked production sites on the four
receiver families newly fire. The delta is *exactly* the Close findings — nothing else changes (baseline
prod Close = 0, so 99 − 0 = 99).

### Gate mechanics — read this before sequencing

The merge gate does **not** run a full `golangci-lint run`. `Makefile` `check-fast` (line 568) and
`check-short` (line 601) both use `--new-from-rev` (`HEAD~1` / `origin/main`); a full run "fails on
~5666 pre-existing legacy issues" and is explicitly *not* the merge gate (`Makefile` lines 719–723).
Consequences:

- Dropping the four exclusions does **not** turn untouched files red at merge time — errcheck fires
  only on new/changed lines under `--new-from-rev`. It DOES turn `make check` / `make lint` (full run,
  line 646/738) red by exactly the unmigrated Close findings.
- Because the gate is diff-scoped, the "flip last" ordering is about the **full run** and future new
  code: land the site migrations first so a full run is Close-clean, then flip the config so any future
  Close on these receivers is caught mechanically.
- Every migration commit that *touches* a Close line will itself trip `--new-from-rev` for that line —
  so each migration commit must convert the line it touches to the idiom in the same commit (which is
  the plan anyway).

---

## 1. The house Close idiom (from `.claude/skills/agent-reviewer/SKILL.md` §"Deferred `Close()`")

There are **three** landed forms; the executor picks by whether the close error is *material* (a failure
means bytes may not have landed) — NOT a single naive rewrite. `_ = x.Close()` is rejected by
`check-blank`; `//nolint:errcheck` is rejected by the no-new-nolint bar. Use:

```go
// FORM A — MATERIAL (write / commit / fsync): join into a named return.
// Landed home: internal/queue/cli/cancel.go (emitQueueCancelEvent, deferred);
//              internal/supervise/daemon_watchdog.go (openCrashLog, non-deferred single-path).
func write(...) (err error) {
    ...
    defer func() { err = errors.Join(err, f.Close()) }()
    ...
}

// FORM B — MATERIAL but must not mask an earlier failure: first error wins.
// Landed home: internal/keeper/watcher.go (FileEmitter.EmitWithRunID).
defer func() {
    if closeErr := file.Close(); closeErr != nil && err == nil {
        err = closeErr
    }
}()

// FORM C — IMMATERIAL (read-only open) but observable: log and continue.
// WarnContext (noctx), context.Background() in a defer with no ctx in scope.
// Landed home: internal/keeper/tmuxresolve.go (recentTranscriptTurn).
defer func() {
    if closeErr := f.Close(); closeErr != nil {
        slog.WarnContext(ctx, "…: close X", "err", closeErr, "path", path)
    }
}()
```

The brief's `if closeErr := x.Close(); closeErr != nil { … }` is Forms B/C. Write-path closes should use
Form A (or B where an earlier error must win). §2's `atomicWriteHandlerState` is cited by the skill as
"the shape that is correct" for temp-file+rename (handle close *before* the rename).

---

## 2. Current form distribution of the 99 production sites

| Current form | Count | Rewrite path |
|---|---|---|
| `_ = x.Close()` (immediate, blank) | 57 | inline `if closeErr := …` (Form B/C) — semantics unchanged (already immediate) |
| `defer func() { _ = x.Close() }()` (deferred closure, blank) | 28 | swap the inner `_ =` for the if-check **inside the existing closure** — deferral preserved |
| `defer x.Close()` (bare defer) | 14 | **DEFER CAVEAT — see §6.** Must become `defer func() { … }()`, never an inline check |

57 + 28 + 14 = 99. ✔

---

## 3. Per-package inventory

Legend for **Form**: `blank-immediate` = `_ = x.Close()`; `blank-defer` = `defer func(){ _ = x.Close() }()`;
`bare-defer` = `defer x.Close()` (⚠ defer-caveat, §6).

### 3a. NON-DAEMON — migrate in this batch (82 sites)

#### cmd/harmonik (28)
| file:line | receiver | form |
|---|---|---|
| cmd/harmonik/beadsmerge.go:164 | f | blank-defer |
| cmd/harmonik/comms.go:318 | conn | blank-defer |
| cmd/harmonik/comms.go:1026 | conn | blank-defer |
| cmd/harmonik/comms.go:1415 | conn | blank-defer |
| cmd/harmonik/comms.go:1560 | conn | blank-defer |
| cmd/harmonik/comms.go:1615 | conn | blank-defer |
| cmd/harmonik/comms.go:1770 | conn | blank-immediate |
| cmd/harmonik/comms.go:1780 | conn | blank-immediate |
| cmd/harmonik/comms.go:1803 | conn | blank-immediate |
| cmd/harmonik/comms.go:1835 | conn | blank-immediate |
| cmd/harmonik/comms.go:2029 | conn | blank-defer |
| cmd/harmonik/comms.go:2047 | conn | blank-immediate |
| cmd/harmonik/crew.go:638 | conn | blank-defer |
| cmd/harmonik/decisions.go:411 | conn | blank-defer |
| cmd/harmonik/decisions.go:595 | conn | blank-defer |
| cmd/harmonik/decisions.go:658 | conn | blank-immediate |
| cmd/harmonik/decisions.go:666 | conn | blank-immediate |
| cmd/harmonik/eval_cmd.go:284 | f | blank-defer |
| cmd/harmonik/eval_cmd.go:314 | f | **bare-defer ⚠** |
| cmd/harmonik/eval_report_cmd.go:158 | f | **bare-defer ⚠** |
| cmd/harmonik/run_via_daemon.go:52 | conn | blank-immediate |
| cmd/harmonik/run_via_daemon.go:102 | subConn | blank-defer |
| cmd/harmonik/run_via_daemon.go:149 | subConn | blank-immediate |
| cmd/harmonik/smoke.go:398 | conn | blank-defer |
| cmd/harmonik/smoke.go:403 | conn | blank-immediate |
| cmd/harmonik/supervise/config.go:184 | dirFd | blank-immediate (dir-fsync close, see note) |
| cmd/harmonik/supervise/loopstatus.go:175 | dirFd | blank-immediate (dir-fsync close, see note) |
| cmd/harmonik/sync_assets_cmd.go:900 | conn | blank-immediate |

Most cmd/harmonik sites are `conn` (net.Conn to the daemon socket) — read/cleanup closes on an already-torn-down
RPC path; Form C or Form B as materiality dictates. `config.go:184` / `loopstatus.go:175` are parent-dir
fsync-close pairs (same pattern as the handlerpause bug in §4 — check whether the paired `dirFd.Sync()` on the
preceding line is also blanked; if so, that is a durability drop worth handling, not just wrapping).

#### internal/lifecycle (20)
| file:line | receiver | form |
|---|---|---|
| internal/lifecycle/orphansweep.go:835 | held | blank-immediate |
| internal/lifecycle/orphansweep.go:884 | f | blank-immediate |
| internal/lifecycle/orphansweep.go:896 | f | blank-immediate |
| internal/lifecycle/orphansweep.go:902 | f | blank-immediate |
| internal/lifecycle/orphansweep.go:949 | f | blank-defer |
| internal/lifecycle/orphansweep.go:981 | dirFd | blank-defer |
| internal/lifecycle/pidfile.go:102 | fd | blank-immediate |
| internal/lifecycle/pidfile.go:111 | fd | blank-immediate |
| internal/lifecycle/pidfile.go:116 | fd | blank-immediate |
| internal/lifecycle/pidfile.go:123 | fd | blank-immediate |
| internal/lifecycle/pidfile.go:129 | fd | blank-immediate |
| internal/lifecycle/pidfile.go:140 | fd | blank-immediate |
| internal/lifecycle/pidfile.go:144 | pfd | blank-immediate |
| internal/lifecycle/pidfile.go:146 | fd | blank-immediate |
| internal/lifecycle/pidfile.go:262 | dirFd | blank-defer |
| internal/lifecycle/pidfilelock.go:87 | fd | blank-defer |
| internal/lifecycle/reconciliationlock_rc002a.go:147 | fd | blank-immediate |
| internal/lifecycle/reconciliationlock_rc002a.go:156 | fd | blank-immediate |
| internal/lifecycle/reconciliationlock_rc002a.go:160 | fd | blank-immediate |
| internal/lifecycle/reconciliationlock_rc002a.go:166 | fd | blank-immediate |

Mostly pidfile / lock-fd closes. Note `orphansweep.go:981` and `pidfile.go:262` are `dirFd` (parent-dir fsync
closes) — inspect the paired `Sync()` for a durability drop as with §4.

#### internal/keeper (9)
| file:line | receiver | form |
|---|---|---|
| internal/keeper/cycle.go:705 | tmp | blank-immediate (temp-file write path — check materiality) |
| internal/keeper/heartbeat.go:216 | tmp | blank-immediate (temp-file write path) |
| internal/keeper/keeper.go:79 | fd | blank-immediate |
| internal/keeper/keeper.go:88 | fd | blank-immediate |
| internal/keeper/keeper.go:92 | fd | blank-immediate |
| internal/keeper/keeper.go:96 | fd | blank-immediate |
| internal/keeper/keeper.go:126 | fd | blank-defer |
| internal/keeper/keeper.go:222 | tmp | blank-immediate (temp-file write path) |
| internal/keeper/keeper.go:228 | tmp | blank-immediate (temp-file write path) |

#### internal/schedule (5)
| file:line | receiver | form |
|---|---|---|
| internal/schedule/store.go:571 | fd | blank-immediate |
| internal/schedule/store.go:576 | fd | blank-immediate |
| internal/schedule/store.go:632 | f | blank-immediate |
| internal/schedule/store.go:637 | f | blank-immediate |
| internal/schedule/store.go:661 | d | blank-immediate (dir handle) |

#### internal/sessiondata (4) — contains **latent bug 1**
| file:line | receiver | form |
|---|---|---|
| internal/sessiondata/sessiondata.go:361 | f | **bare-defer ⚠ — LATENT BUG 1 (write path, §4)** |
| internal/sessiondata/sessiondata.go:382 | f | bare-defer ⚠ (ReadAll — read path, benign) |
| internal/sessiondata/sessiondata.go:437 | f | bare-defer ⚠ (buildRunEventData — read path, benign) |
| internal/sessiondata/sessiondata.go:530 | f | bare-defer ⚠ (readTranscript — read path, benign) |

#### internal/workspace (3)
| file:line | receiver | form |
|---|---|---|
| internal/workspace/claudetrust_wm040b.go:467 | lockFd | **bare-defer ⚠** (advisory lock fd) |
| internal/workspace/claudetrust_wm040b.go:587 | lockFd | **bare-defer ⚠** |
| internal/workspace/claudetrust_wm040b.go:758 | lockFd | **bare-defer ⚠** |

#### internal/crew (3)
| file:line | receiver | form |
|---|---|---|
| internal/crew/registry.go:116 | f | blank-immediate |
| internal/crew/registry.go:122 | f | blank-immediate |
| internal/crew/registry.go:142 | d | blank-immediate (dir handle) |

#### cmd/harmonik-twin-claude (2)
| file:line | receiver | form |
|---|---|---|
| cmd/harmonik-twin-claude/main.go:236 | conn | blank-defer |
| cmd/harmonik-twin-claude/replaydriver.go:52 | f | blank-defer |

#### Singletons (8 packages, 1 each)
| file:line | receiver | form |
|---|---|---|
| internal/supervise/daemon_watchdog.go:431 | f | **bare-defer ⚠ — latent-bug-3 candidate (§4)** |
| internal/usage/usage.go:534 | f | **bare-defer ⚠** |
| internal/twinparity/read.go:21 | f | blank-defer |
| internal/queue/cli/client.go:95 | conn | blank-defer |
| internal/handler/session.go:460 | c | blank-immediate |
| internal/codexdriver/session.go:714 | s.stdinPipe | blank-immediate |
| internal/apptap/tap.go:130 | childIn | blank-immediate |
| tools/forbid-import/main.go:185 | f | **bare-defer ⚠** |

### 3b. DAEMON — DEFERRED, sequence with the internal/daemon extraction (17 sites) — DO NOT migrate in this batch

| file:line | receiver | form |
|---|---|---|
| internal/daemon/commscursor.go:175 | lockFd | **bare-defer ⚠** |
| internal/daemon/commscursor.go:211 | tmp | blank-immediate |
| internal/daemon/dot_cascade.go:1597 | piStdoutFile | blank-defer |
| internal/daemon/followup_ledger_ac1.go:42 | f | **bare-defer ⚠** |
| internal/daemon/handlerpause_persist_m0k0a.go:206 | tmp | blank-immediate (error-path cleanup) |
| internal/daemon/handlerpause_persist_m0k0a.go:211 | tmp | blank-immediate (error-path cleanup) |
| internal/daemon/handlerpause_persist_m0k0a.go:229 | dirF | blank-immediate — **LATENT BUG 2 (§4)** |
| internal/daemon/reconciliation.go:346 | f | blank-immediate |
| internal/daemon/restartbackoff.go:224 | tmp | blank-immediate |
| internal/daemon/scenariotest/timeout.go:90 | f | blank-defer (scenario test-support, not `_test.go`) |
| internal/daemon/scenariotest/timeout.go:145 | f | blank-defer (scenario test-support, not `_test.go`) |
| internal/daemon/sessioncontext_chb023.go:320 | c | blank-immediate |
| internal/daemon/socket.go:247 | conn | blank-immediate |
| internal/daemon/socket.go:397 | ln | blank-defer |
| internal/daemon/socket.go:407 | ln | blank-immediate |
| internal/daemon/socket.go:448 | conn | blank-defer |
| internal/daemon/workloop.go:4572 | piStdoutFile | blank-defer |

> Note: `internal/daemon/scenariotest/timeout.go` is a scenario **test-support** package but *not* a
> `_test.go` file, so the `_test.go` exclusion does NOT suppress it and it counts as a daemon prod site.
> Latent-bug-2 (`handlerpause_persist_m0k0a.go:229`) lives in this daemon-deferred set — but it is a real
> durability bug and should get its **fix pulled forward** as a standalone bugfix commit rather than
> waiting on the whole extraction (see §4).

---

## 4. Latent bugs (verified by reading the code)

### BUG 1 — `internal/sessiondata/sessiondata.go:361` — `Append` lost-flush. **CONFIRMED REAL.**
`Append` opens the session-data JSONL with `os.O_APPEND|os.O_CREATE|os.O_WRONLY` (a write path), writes
one line with `f.Write(...)`, and does `defer f.Close()` (line 361). The write's flush error surfaces at
`Close`, which is dropped — `Append` can return `nil` while the record never durably landed (ENOSPC /
delayed-allocation). **Fix (material, Form A):** propagate the close error, e.g.
```go
defer func() { err = errors.Join(err, f.Close()) }()   // in a named-return Append(...) (err error)
```
(or check `f.Close()` after `f.Write` on the success path and return it). The other three sessiondata
sites (382/437/530) are read-only opens — benign, wrap with Form C.

### BUG 2 — `internal/daemon/handlerpause_persist_m0k0a.go:229` — dropped parent-dir fsync. **CONFIRMED REAL.**
`atomicWriteHandlerStateDaemon` does temp-write → `tmp.Sync()` → `tmp.Close()` → `os.Rename` correctly
(those close errors ARE handled, lines 216/end-of-write). But Step 6, the parent-directory fsync that
makes the rename crash-durable, drops **both** errors:
```go
if dirF, openErr := os.Open(dir); openErr == nil {
    _ = dirF.Sync()   // line 227 — durability barrier failure swallowed
    _ = dirF.Close()  // line 229 — errcheck finding
}
```
If the directory fsync fails, the new dirent may not survive a crash, yet the function returns `nil`.
**Fix (material):** capture and return `dirF.Sync()` (and handle `dirF.Close()`); do not swallow the
barrier. This is in the daemon-deferred set but is a genuine durability defect — pull the fix forward as
its own bugfix commit ahead of the extraction batch.

### BUG 3 — `internal/supervise/daemon_watchdog.go:431` — crash-log close. **NOT a durability bug (weaker than 1 & 2).**
`reviveWith` opens the crash log via `openCrashLog`, sets `cmd.Stdout = f; cmd.Stderr = f`, then
`defer f.Close()` and `cmd.Start()`. The parent **never writes to `f` itself** — the detached child
inherits its own duplicated fd and does the writing. After `cmd.Start()` the parent's copy is safely
closed (the standard pattern), and its `Close` error carries no unflushed parent data. So this is a
correct parent-side close, not a lost-flush. **Recommendation:** wrap for idiom compliance (Form C, log
& continue) — no real behavioral fix is warranted. (Note the *open* side, `openCrashLog`, is already
cited by agent-reviewer §2 as a correct Form-A home; the finding here is only the caller-side deferred
close.) Flagging honestly: the prior fan-out's "3 real bugs" holds firmly for 1 & 2; #3 is
idiom-only.

---

## 5. The `.golangci.yml` diff — apply LAST

Two edits. Remove the four `Close` `exclude-functions` entries (keep `check-blank: true`); add one
`_test.go`-scoped errcheck exclusion in `exclusions.rules`. The syntax mirrors the existing
`SC6-DRIVER-CLOCKPORT` `text:`+`path:` rules (lines 87–97) and the forbidigo path-scoping (line 50, 63,
80) — it is in fact **already written verbatim in the config's own rationale block** (lines 132–134) and
in `.claude/skills/agent-reviewer/SKILL.md` §2 (lines ~199–202).

**Linter-verified:** with both edits, `errcheck` reports **99** production Close findings and **0**
`_test.go` Close findings (run on the working tree with `.tools/golangci-lint` v2.3.0).

```diff
--- a/.golangci.yml
+++ b/.golangci.yml
@@ exclusions.rules (after the last SC6-DRIVER-CLOCKPORT _test.go rule, before `  settings:`) @@
       - linters:
           - forbidigo
         text: 'SC6-DRIVER-CLOCKPORT'
         path: _test\.go$
+      # P2 hk-8dtiv: production Close() is now mechanically errcheck-gated (the four
+      # (io.Closer|*os.File|net.Conn|net.Listener).Close exclude-functions entries were
+      # dropped). Test Close noise stays at zero by matching the finding text on _test.go
+      # paths — the same text:+path: mechanism the SC6 rules above use.
+      - linters:
+          - errcheck
+        text: 'Close` is not checked'
+        path: _test\.go$

@@ settings.errcheck.exclude-functions (remove all four) @@
       exclude-functions:
-        - "(io.Closer).Close"
-        - "(*os.File).Close"
-        - "(net.Conn).Close"
-        - "(net.Listener).Close"
```

> After removing all four entries, `exclude-functions:` has an empty list (YAML null). errcheck treats
> that as "no function exclusions", which is correct and lints clean. If a bare `exclude-functions:` key
> reads oddly, the executor may instead delete the key line entirely — behaviourally identical. (Verified:
> the scratch run with an empty `exclude-functions:` produced the expected 99/251 split.)
>
> **Also update the surrounding prose.** The ~70-line rationale comment above `exclude-functions:` (lines
> ~102–170) argues *for keeping* the exclusions and cites the old 106/303/409 figures. It must be
> rewritten (or removed) to describe the new posture: Close is mechanically gated on production; the
> `_test.go` rule carries the test carve-out. The parallel narrative in
> `.claude/skills/agent-reviewer/SKILL.md` §"Deferred `Close()`" and its byte-identical embed source
> `cmd/harmonik/assets/skills/agent-reviewer/SKILL.md` (per AGENTS.md the two must stay byte-for-byte),
> plus `docs/foundation/project-level/quality-checks.md` §Error handling conventions (names the four
> "lost their mechanical check" closes), all reference the dropped exclusions and need reconciling in the
> config-flip commit. This is a `agent-config-reviewer` "enforced-config drift" surface — expect it to
> flag the mismatch if the prose is not updated.

---

## 6. DEFER CAVEAT — the 14 `defer x.Close()` sites (do not rewrite naively)

A bare `defer x.Close()` closes at **function exit**. Rewriting it to an inline
`if closeErr := x.Close(); closeErr != nil { … }` closes **immediately** — a behavior change that can
close a file/lock/conn while later code still needs it. These 14 sites MUST become a **deferred closure**,
never an inline check:

```go
// WRONG — closes immediately, changes semantics:
if closeErr := f.Close(); closeErr != nil { … }
// RIGHT — preserves function-exit timing:
defer func() {
    if closeErr := f.Close(); closeErr != nil { … }   // Form B/C, or Form A via errors.Join
}()
```

The 14 bare-defer sites:

| file:line | receiver | why deferral matters |
|---|---|---|
| cmd/harmonik/eval_cmd.go:314 | f | file used for the rest of the function body |
| cmd/harmonik/eval_report_cmd.go:158 | f | same |
| internal/daemon/commscursor.go:175 | lockFd | **advisory lock** — early close releases the lock mid-critical-section |
| internal/daemon/followup_ledger_ac1.go:42 | f | file read/written after the defer |
| internal/sessiondata/sessiondata.go:361 | f | **BUG 1** — write path; must close after `f.Write`, Form A |
| internal/sessiondata/sessiondata.go:382 | f | read scan runs after the defer |
| internal/sessiondata/sessiondata.go:437 | f | read scan runs after the defer |
| internal/sessiondata/sessiondata.go:530 | f | read scan runs after the defer |
| internal/supervise/daemon_watchdog.go:431 | f | closed after `cmd.Start()` (parent-side); keep deferred |
| internal/usage/usage.go:534 | f | read scan runs after the defer |
| internal/workspace/claudetrust_wm040b.go:467 | lockFd | **advisory lock** — must hold to function exit |
| internal/workspace/claudetrust_wm040b.go:587 | lockFd | **advisory lock** |
| internal/workspace/claudetrust_wm040b.go:758 | lockFd | **advisory lock** |
| tools/forbid-import/main.go:185 | f | read after the defer |

The lock-fd sites (commscursor, claudetrust ×3) are the highest-risk for a naive rewrite: releasing an
advisory lock early would break mutual exclusion. Reviewer must confirm every bare-defer conversion keeps
the closure form.

The **28 `defer func() { _ = x.Close() }()`** sites already have the closure — just replace the inner
`_ = x.Close()` with the if-check; timing is unchanged, no caveat. The **57 `_ = x.Close()`** sites are
already immediate; an inline if-check preserves timing, no caveat.

---

## 7. Batching recommendation (non-daemon, 82 sites)

Split by package so each commit is independently reviewable and diff-scoped `--new-from-rev` passes stay
green. Suggested commit sequence (bug fixes first, config flip last):

1. **`fix(sessiondata): propagate Append close error` (BUG 1)** — sessiondata.go:361, Form A. Standalone
   bugfix; the other 3 sessiondata read sites can ride along or land in step 5.
2. **`fix(daemon): handle parent-dir fsync in atomicWriteHandlerStateDaemon` (BUG 2)** — pulled forward
   from the daemon-deferred set; handlerpause_persist_m0k0a.go:227/229. Standalone bugfix.
3. **`refactor(cmd): house Close idiom in cmd/harmonik` — 28 sites.** ~half the churn; mostly `conn`
   cleanup closes (Form C/B). eval_cmd.go:314 + eval_report_cmd.go:158 are bare-defer (§6).
4. **`refactor(lifecycle): house Close idiom` — 20 sites.** pidfile/lock/orphansweep; watch the two
   `dirFd` fsync-closes.
5. **`refactor: house Close idiom in keeper+schedule+crew+sessiondata-reads` — 21 sites**
   (keeper 9, schedule 5, crew 3, sessiondata-reads 3 [if not in step 1], plus spillover). Group the
   smaller packages.
6. **`refactor: house Close idiom in remaining singletons` — ~13 sites** (workspace 3 lock-defers,
   twin-claude 2, usage, twinparity, queue/cli, handler, codexdriver, apptap, tools/forbid-import,
   supervise/daemon_watchdog:431). The 3 workspace lock-defers need careful §6 review.
7. **`build(lint): re-arm Close errcheck on production; scope test carve-out` — the §5 config flip
   LAST**, with the agent-reviewer / quality-checks / embed-source prose reconciled in the same commit.

Prior-estimate check: cmd/harmonik = **28** (prior said 26 — correct to 28); internal/lifecycle = **20**
(prior said 20 — ✔). Total non-daemon churn = **82** sites across 16 packages.

**Daemon set (17 sites)** — a separate track sequenced with the `internal/daemon` extraction, EXCEPT
BUG 2's fix which is pulled forward in step 2. Do not fold the remaining 15 daemon sites into this batch.

---

## 8. Reproduction commands (for the executor / reviewer)

```bash
# pinned linter
.tools/golangci-lint version                                   # -> v2.3.0

# scratch config = current .golangci.yml minus the four Close exclude-functions lines (172–175)
sed '172,175d' .golangci.yml > /tmp/golangci-noclose.yml

# absolute production Close set (99)
.tools/golangci-lint run --config /tmp/golangci-noclose.yml --enable-only errcheck ./... 2>&1 \
  | grep -E '\.Close` is not checked' | grep -v '_test\.go:' | wc -l          # -> 99

# with the _test.go exclusion added (see §5): test Close -> 0, prod Close -> 99
```
