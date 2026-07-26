# IO-Boundaries Dossier — Agent-Input (tmux) & Remote-Worker (SSH)

Factual anatomy of the two "ack-free channel" IO boundaries flagged for rebuild.
No recommendations — this documents what IS, with exact `file:line` and verbatim
excerpts. All line numbers are as of the read on 2026-07-13.

Files inspected:
- `internal/handler/substrate.go`
- `internal/daemon/pasteinject.go` (2634 LOC total)
- `internal/daemon/tmuxsubstrate.go` (~2.7k LOC)
- `internal/lifecycle/tmux/osadapter.go`, `internal/lifecycle/tmux/runner.go`
- `internal/workspace/remotematerialize.go`, `internal/workspace/createworktree.go`
- `internal/workers/registry.go`, `internal/daemon/workloop.go`

---

## PART A — TMUX / AGENT INPUT

### A1. The `handler.Substrate` seam (the clean plug-in point)

`internal/handler/substrate.go:30-38` — the entire interface is one method:

```go
type Substrate interface {
	// SpawnWindow creates a new hosted window/session for the given parameters
	// and returns a handle to the running session.
	//
	// Returns a non-nil error if the spawn fails (e.g. tmux session missing,
	// window-name collision). Errors SHOULD be wrapped with ErrStructural for
	// daemon-level routing.
	SpawnWindow(ctx context.Context, in SubstrateSpawn) (SubstrateSession, error)
}
```

Contract notes (from the doc comment and the input struct):
- `internal/handler/substrate.go:5-8` — handler NEVER imports `internal/lifecycle/tmux`;
  the concrete substrate is injected by the daemon composition root (depguard
  forbids the handler↔tmux cross-import). Spec: `specs/process-lifecycle.md §4.7 PL-021b`, `handler-contract.md HC-054`.
- Input `SubstrateSpawn` (`:46-89`): `WindowName`, `Cwd`, `Env []string`, `Argv []string`,
  `StdinDevNull bool` (`:63-74` — /dev/null redirect for codex ProcessExit harnesses;
  MUST NOT be set for claude), `Terminal bool` (`:76-88` — reserves a spawn-semaphore slot for consolidate/join nodes).

The returned handle `SubstrateSession` (`:101-122`) is deliberately NARROW — **it has no input/write method**:
```go
type SubstrateSession interface {
	Kill(ctx context.Context) error        // tmux kill-window; MUST be idempotent
	Wait(ctx context.Context) error        // blocks until subprocess exits
	Outcome() Outcome                      // zero-value before Wait returns
	PID() int                              // pane_pid; 0 if unknown
	Stdout() io.Reader                     // nil for tmux-hosted (uses Unix-socket hook-relay)
}
```
`:96-98` states explicitly: **"SendInput and CloseStdin are not part of this interface;
the substrate owns the child's stdin (typically the pty managed by tmux)."** The seam
gives the daemon NO typed channel to push agent input — that is done entirely out-of-band
via the paste-inject path (A2) against the tmux pane, using OPTIONAL side interfaces that
the daemon type-asserts for (`enterSender`, `paneCapturer`, `quitSender`, `paneLivenessChecker`,
`paneOutputSizer`, `commandRunnerProvider` — all declared in `pasteinject.go:187-495`), NOT
methods on `Substrate`/`SubstrateSession`.

Adapter: `substrateSessionAdapter` (`:129-199`) wraps a `SubstrateSession` back into a
`handler.Session`. Its `SendInput` (`:140-142`) and `CloseStdin` (`:173-175`) are hard **no-ops
returning nil** — confirming input never flows through the Session abstraction for tmux hosts.

### A2. How paste-injection actually works

Delivery is a per-run tmux dance built from four tmux verbs. The daemon-side wiring
(`perRunSubstrate`, `internal/daemon/tmuxsubstrate.go:2214-2274`) forwards to the tmux
adapter against the pane target captured at SpawnWindow time (`paneTarget()`, `:2208-2212`):

```go
// tmuxsubstrate.go:2218
func (p *perRunSubstrate) WriteLastPane(ctx context.Context, bufferName string, payload []byte) error {
	target := p.paneTarget()
	if target == "" {
		return fmt.Errorf("daemon: perRunSubstrate.WriteLastPane: no window spawned yet: %w", tmux.ErrStructural)
	}
	return p.pasteAdapter().WriteToPane(ctx, bufferName, target, payload)
}
```

The underlying tmux primitives (`internal/lifecycle/tmux/osadapter.go`), each a fresh
`runner.Command(ctx,"tmux",...)` (local `exec` OR `ssh` — see B5):

- `LoadBuffer` (`:379`): `tmux load-buffer -b <bufferName> -` with payload streamed on **stdin**.
- `PasteBuffer` (`:405`): `tmux paste-buffer -b <bufferName> -t <paneTarget> -d` (`-d` deletes buffer, PL-021d cleanup).
- `WriteToPane` (`:541`): the composed helper = `LoadBuffer` then `PasteBuffer` then a `daemon_pane_write` slog line.
- `SendKeysEnter` (`:464`): `tmux send-keys -t <paneTarget> Enter` — a REAL key event (NOT `-l` literal). Comment `:451-462`: bracketed-paste `\n` is NOT dispatched as a keypress, so only bare send-keys generates a true Enter the React/ink TUI sees.
- `SendKeysQuit` (`:486`): `tmux send-keys -t <paneTarget> /quit Enter`.
- `CapturePane` (`:512`): `tmux capture-pane -p -t <paneTarget> -S -<scrollback>` (the read seam for verification).

The full kick-off sequence for an implementer-initial launch — `pasteInjectImplementerInitial`,
`internal/daemon/pasteinject.go:1554-1601`:
1. `statTaskFileVia` — confirm `.harmonik/agent-task.md` exists (on worker for remote) (`:1556`).
2. `SendEnterToLastPane` — splash-dismiss Enter (`:1564`), then `splashDismissWait` = **750ms** (`:1570`).
3. `injectAndVerifySeed(...,"agent-task.md",...)` — the paste + verify loop (`:1579`, see A4).
4. `splashDismissWait` again — 750ms settle (`:1590`).
5. `sendSubmitEnterWithRetry` — blind submit Enter x3 total (`:1598`).

Seed = the literal string `"Please read .harmonik/agent-task.md and begin.\n"` (`:1574`).

### A3. Ack-freeness — every LIVE (non-test) sleep/timeout in the tmux path

**VERIFIED census claim.** There is exactly **1 production `time.Sleep`** in the whole
tmux-substrate/paste path, and it is in a process-kill grace poll, NOT the input path:

- `internal/daemon/tmuxsubstrate.go:2487` — `time.Sleep(100 * time.Millisecond)` inside
  `killProcessWithGrace` (SIGTERM→poll→SIGKILL). This is the only `time.Sleep` in
  `tmuxsubstrate.go` (`grep -c` = 1) and in `pasteinject.go` (`grep -c` = 0).

`pasteinject.go` uses **`time.After` inside `select`** (10 occurrences) rather than `time.Sleep`,
but the wait behaviour is identical — blind timed waits with no ack. Live values (all declared
as `var`/`atomic.Int64` so tests can shrink them):

| Knob | Value | file:line | Purpose |
|---|---|---|---|
| `splashDismissDelay` | **750ms** | `pasteinject.go:74` (init), waited at `:1531` | grace between splash-dismiss Enter and paste |
| `resumeSubmitRetryDelay` | **400ms** | `:120` (init), waited at `:1803` | delay between blind submit-Enter retries |
| `resumeSubmitRetries` | **2** (→ 3 total Enters) | `:114` | blind Enter re-sends |
| `pasteVerifyAttempts` | **3** | `:161` | seed-render capture retries |
| `pasteVerifyBackoff` | **1500ms** | `:169` (init), waited at `:1762` | backoff between capture attempts |
| `pasteVerifyScrollback` | **200** lines | `:162` | capture-pane history tail |
| `implementerReseedGrace` | **75s** | `:753` | one-shot reseed Enter if no commit |
| `launchHeartbeatTimeout` | **180s** | `:685` | first-heartbeat-or-kill window |
| `heartbeatStalenessThreshold` | **8m** | `:658` | missed-heartbeat kill trigger |
| `commitPollInterval` | **500ms** | `:603` | git HEAD poll cadence |
| `commitPollTimeout` | **30m** | `:623` | per-progress commit budget |
| `commitHardCeiling` | **90m** | `:639` | absolute wall-clock kill backstop |
| `launchSuppressionCeiling` | **12m** | `:719` | cap on active-pane suppression |
| `briefDeliveredTimeout` | **2m** | `:595` | wait for brief-delivered channel |
| `noChangeKillDelay` | **30s** | `:730` | /quit→SIGKILL grace |
| `postQuitKillGrace` | **60s** | `:769` | post-commit /quit→kill grace |

Liveness probes (no sleep, but blind scrapes): `hasAnyDirectChild` = `pgrep -P <pid>`
(`pasteinject.go:372`); `commandMatchesLiveAgent` = `ps -o comm= -p <pid>` (`:390`).
Wait loop polls at 500ms (`tmuxsubstrate.go:2494` doc).

**Test-side sleep count:** 41 `time.Sleep` across `pasteinject*_test.go` +
`tmuxsubstrate*_test.go` (matching the census's "~48"); 244 across all `internal/daemon/*_test.go`.
Confirms the asymmetry: production waits are timed/blind (via `time.After`/one `time.Sleep`),
and the heavy sleep footprint is entirely in tests.

### A4. What signal (if any) confirms the TUI accepted input?

**Claim confirmed: exit 0 ≠ input accepted.** The code says so explicitly and only PARTIALLY
mitigates it. `tmux load-buffer`/`paste-buffer` return exit 0 once tmux has handed the buffer
to the pane — NOT once claude's React/ink TUI rendered it (`pasteinject.go:131-138`).

There is **no ack on the write itself**. Two half-measures exist:

1. **Seed render-verification** (`injectAndVerifySeed`, `pasteinject.go:1708-1778`, hk-zexsj):
   after each `WriteLastPane` it does `CaptureLastPane` (a `capture-pane` scrape) and checks
   `strings.Contains(pane, marker)` for a marker substring (e.g. `"agent-task.md"`). This is a
   best-effort screen-scrape, not a protocol ack. Key excerpt:
   ```go
   pane, capErr := pc.CaptureLastPane(ctx, pasteVerifyScrollback)
   ...
   case strings.Contains(pane, marker):
       ... return ""   // "landed"
   default:
       // Capture succeeded but the marker is genuinely absent — a real paste discard.
       captureEverSucceeded = true
       lastErr = fmt.Errorf("seed marker %q absent from pane", marker)
   ```
   Failure modes are handled by TRUSTING the write: if capture-infra itself failed on every
   attempt (`!captureEverSucceeded`) it returns `""` = success anyway (`:1773-1776`). If the
   substrate has no capture capability (test double) it returns `""` after the first write
   (`:1729-1733`). So verification is skippable and non-authoritative.

2. **Blind submit-Enter retries** (`sendSubmitEnterWithRetry`, `pasteinject.go:1795-1809`):
   the submit Enter is sent, then re-sent `resumeSubmitRetries` more times on a fixed delay
   with **no check that submission happened**:
   ```go
   func sendSubmitEnterWithRetry(ctx context.Context, es enterSender, phase string) {
   	if err := es.SendEnterToLastPane(ctx); err != nil { ... }
   	for i := 0; i < resumeSubmitRetries; i++ {
   		select {
   		case <-ctx.Done(): return
   		case <-time.After(resumeSubmitRetryDelayDur()):
   		}
   		if err := es.SendEnterToLastPane(ctx); err != nil { ... }
   	}
   }
   ```
   The design comment (`pasteinject.go:100-108`) states the acceptance gap in one sentence:
   *"There is no pane-capture primitive on the enterSender interface to detect 'input cleared',
   so we cannot positively confirm submission. Instead we send the submit Enter, wait a short
   settle, and re-send it..."*

Downstream, the ONLY positive signal that input was actually accepted is INDIRECT and async:
the first `agent_heartbeat` event over the hook-bridge socket (tracked in `pasteInjectQuitOnCommit`,
`pasteinject.go:1065-1075`), or a new git commit at HEAD. If neither arrives, blind timeouts
(`launchHeartbeatTimeout` 180s, then kill) clean up. There is no synchronous confirmation the
keystroke/paste was consumed.

---

## PART B — REMOTE / SSH

### B5. How a remote op is issued — fresh `ssh -- '<string>'` per op, ControlMaster OFF

`internal/lifecycle/tmux/runner.go:111-121` — `SSHRunner.Command` builds a **new `ssh` process
per operation**, shell-quoting every token, and lets the remote LOGIN SHELL re-parse the joined string:

```go
func (s SSHRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	quoted := make([]string, 0, 1+len(args))
	quoted = append(quoted, shellQuoteArg(name))
	for _, a := range args {
		quoted = append(quoted, shellQuoteArg(a))
	}
	sshArgs := make([]string, 0, len(s.Opts)+3)
	sshArgs = append(sshArgs, s.Opts...)
	sshArgs = append(sshArgs, s.Host, "--", strings.Join(quoted, " "))
	return exec.CommandContext(ctx, "ssh", sshArgs...)
}
```

The doc comment (`runner.go:72-91`) is emphatic: **"OpenSSH does NOT deliver the post-host
operands as a discrete argv vector — it space-joins them into a single string and runs that
via the remote LOGIN SHELL (`$SHELL -c "<joined>"`)."** So every remote op = `ssh [Opts...]
<Host> -- '<tok>' '<tok>' ...`, a single-string command through the worker's login shell, one
fresh TCP connection per call. `shellQuoteArg` (`:103-105`) single-quotes each token to survive
the re-split (guards against `#{pane_id}` being read as a shell comment — hk-fxy9/hk-538l).

**ControlMaster is explicitly DISABLED per tmux SSHRunner.** `internal/daemon/workloop.go:3389`
and `:3416`:
```go
sshRunner: tmuxpkg.SSHRunner{Host: w.Host, Opts: []string{"-o", "ControlMaster=no", "-o", "ControlPath=none"}},
```
Rationale (`workloop.go:3408-3416`, hk-zexsj): a churning multiplexed master can silently drop a
multiplexed `load-buffer`/`paste-buffer` mid-write (the hk-cnp17 truncation family), discarding
the seed paste. So each tmux command gets a dedicated, non-multiplexed connection. The reverse-tunnel
path pins the same flags (`internal/daemon/reversetunnel.go:239-240`). SSH transport failures are
detected by exit-code 255 (`runner.go:128-137`, `IsSSHConnectionFailure`).

### B6. The embedded Python flock script (`remotematerialize.go`)

`internal/workspace/remotematerialize.go:328-396` — `workerTrustUpsertProgram`, fed on **stdin** to
`python3 -` (`EnsureWorktreeTrustVia`, `:284-285`; NOT `-c`, because `ssh` re-split shreds a
multi-line `-c` program — `:261-283`). Verbatim:

```python
import fcntl, json, os, sys, tempfile
arg = sys.argv[1]
if len(arg) >= 2 and arg[0] == "'" and arg[-1] == "'":
    arg = arg[1:-1]
wt = os.path.realpath(arg)
cfg_path = os.path.join(os.path.expanduser("~"), ".claude.json")
lock_path = cfg_path + ".lock"

def load_cfg():
    try:
        with open(cfg_path) as f:
            cfg = json.load(f)
    except FileNotFoundError:
        return {}
    except ValueError:
        return {}
    return cfg if isinstance(cfg, dict) else {}

def is_trusted(cfg):
    projects = cfg.get("projects")
    if not isinstance(projects, dict):
        return False
    entry = projects.get(wt)
    return isinstance(entry, dict) and entry.get("hasTrustDialogAccepted") is True

# Fast path: probe WITHOUT the lock; a no-op when already trusted.
if is_trusted(load_cfg()):
    sys.exit(0)

# Write path: hold LOCK_EX on the sidecar lockfile across the whole
# read-modify-write so concurrent writers never lose each other's keys.
lock_fd = os.open(lock_path, os.O_CREAT | os.O_RDWR, 0o600)
try:
    fcntl.flock(lock_fd, fcntl.LOCK_EX)
    # Re-read UNDER the lock ...
    cfg = load_cfg()
    if is_trusted(cfg):
        sys.exit(0)
    projects = cfg.get("projects")
    if not isinstance(projects, dict):
        projects = {}
        cfg["projects"] = projects
    entry = projects.get(wt)
    if not isinstance(entry, dict):
        entry = {}
        projects[wt] = entry
    entry["hasTrustDialogAccepted"] = True
    d = os.path.dirname(cfg_path) or "."
    fd, tmp = tempfile.mkstemp(dir=d, prefix=".claude.json.tmp-")
    try:
        with os.fdopen(fd, "w") as f:
            json.dump(cfg, f, indent=2)
            f.write("\n")
        os.replace(tmp, cfg_path)
    except BaseException:
        try:
            os.unlink(tmp)
        except OSError:
            pass
        raise
finally:
    try:
        fcntl.flock(lock_fd, fcntl.LOCK_UN)
    except OSError:
        pass
    os.close(lock_fd)
```

**Race it patches** (`remotematerialize.go:300-327`): the classic cross-process lost-update on the
worker's `~/.claude.json`. Under `max_slots>1` the daemon launches several remote runs at once,
each spawning this program against the SAME `~/.claude.json`. A naive read-modify-write is a
lost-update race — two copies read before either writes, each adds only its own worktree-trust key,
the last `os.replace` clobbers the other's key. The clobbered run's worktree is then untrusted →
Claude Code shows the folder-trust dialog → launch hangs → `agent_ready` never fires (a prior run
proved 5 concurrent unlocked writers → only 1 worktree survived trusted). The fix: `fcntl.flock(LOCK_EX)`
on a **sidecar** lockfile `~/.claude.json.lock` (NOT the config itself — `os.replace` swaps the inode
out from under a lock on the config). Lock held across read→merge→`os.replace`; lock-free fast path
for already-trusted; re-read under the lock before writing.

### B7. Box-A mutexes owning worker/remote state

- **`internal/workers/registry.go:11`** — `Registry.mu sync.Mutex`. The primary owner of worker
  slot accounting: guards `worker Worker`, `hasWorker bool`, `inFlight int` (`:10-15`).
  `SelectWorker` (`:38-53`) atomically checks `Enabled` + `inFlight < MaxSlots` and does `inFlight++`
  under the lock; callers must `ReleaseSlot` (which decrements under `mu`). Live-disable (`SetEnabled`)
  and `SelectWorkerByName` (`:55+`) also serialize on this one mutex.
- **`internal/workspace/createworktree.go:163-166`** — `cfg.createMu` (caller-supplied `*sync.Mutex`)
  serializes the whole worktree-add+HEAD-resolve retry loop against a shared worker repo (hk-5qp7z),
  so N concurrent `git worktree add` calls don't race on HEAD/index resolution.
- **`internal/daemon/daemon.go:619`** — `mergeMu *sync.Mutex` (wired at `:2206`) serializes merges
  into the shared repo. `daemon.go:2078` — `emittedEpicsMu`.
- Slot/in-flight counters at the daemon layer are `deps.localInFlight` (an atomic counter, used at
  `workloop.go:3423-3425`) plus `workerRegistry.HasFreeSlot`/`ReleaseSlot` (which route to `Registry.mu`).
  There is no separate box-A "remote state" mutex beyond `Registry.mu` + `createMu` + `mergeMu`;
  remote worker state (enable/slots) lives entirely behind `workers.Registry.mu`.

### B8. Dual-path branches (`runner != nil` / `runner == nil`) across `internal/workspace`

Counts (`grep -E "runner (!=|==) nil"`):
- **12** `runner != nil`-flavored non-test lines (task's "~49" is the whole-repo count; within
  `internal/workspace` the every-arm count is lower — see below).
- **25** non-test lines matching either arm (`!= nil` OR `== nil`) in `internal/workspace`.
- **28** including test files.

Every `*Via` materializer is a two-arm switch: `runner == nil` → box-A-local
`os.MkdirAll`/`os.WriteFile`/`os.Remove` (byte-identical local, "NFR7"); else → route through the
runner onto the worker. Representative sites:
- `remotematerialize.go:108` (`WriteFileVia`), `:122` (`RemoveFileVia`), `:146` (`WriteReviewTargetVia`),
  `:165` (`RemoveReviewVerdictVia`), `:185` (`MaterializeClaudeSettingsVia`), `:213` (`WriteAgentTaskVia`),
  `:257` (`EnsureWorktreeTrustVia`).
- `createworktree.go:180` (remote `mkdir -p` vs local `os.MkdirAll`), `:214` (remote `rm -rf` vs
  `os.RemoveAll` in `cleanupPartialState`), `:247` (LOCAL fast-return after add).
- `autostatusmarker.go:96`, `diffhash.go:47`.

Note the daemon-side mirror of this same idiom (`resolveWorktreeHEADVia`, `worktreeActivityFingerprintVia`,
`runnerIsLocalFS`) lives in `pasteinject.go:497-579` — same nil-arm delegation pattern. The
whole-repo `runner != nil` census (~49 / ~92 with the nil arm) spans workspace + daemon; within
`internal/workspace` alone it is 12 / 25.

### B9. `createworktree.go` honest-probe (MUST be carried forward)

**`resolveWorktreeHEADViaRunner`** (`internal/workspace/createworktree.go:146-152`):
```go
func resolveWorktreeHEADViaRunner(ctx context.Context, runner tmux.CommandRunner, wtPath string) (string, error) {
	out, err := runner.Command(ctx, "git", "-C", wtPath, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
```

**HEAD validation** in `CreateWorktree` (`:243-266`, hk-iaj1w) — `git worktree add` can exit 0 yet
leave an un-checked-out worktree on a worker; the code refuses to trust exit-0 and re-probes HEAD:
```go
emptyHEADRace := false
if err == nil {
	if cfg.runner == nil {
		return nil          // LOCAL: exit-0 is sufficient
	}
	// REMOTE (hk-iaj1w): concurrent add can race and leave the worktree dir
	// created but un-checked-out, so a later rev-parse HEAD returns empty.
	head, headErr := resolveWorktreeHEADViaRunner(ctx, runner, worktreePath)
	if headErr == nil && head != "" {
		return nil          // genuinely ready
	}
	emptyHEADRace = true
	err = fmt.Errorf("git worktree add exited 0 but HEAD did not resolve in %q (concurrent remote create race): %v",
		worktreePath, headErr)
	out = []byte("(empty HEAD after git worktree add — hk-iaj1w)")
}
```

**`cleanupPartialState`** (`:213-227`) — order-sensitive teardown between retries:
```go
cleanupPartialState := func() {
	if cfg.runner != nil {
		rmCmd := runner.Command(ctx, "rm", "-rf", worktreePath)   // remote dir on worker
		_ = rmCmd.Run()
	} else {
		_ = os.RemoveAll(worktreePath)                            // local
	}
	// Deregister FIRST so branch -D can succeed (branch -D refuses a branch still
	// used by a registered worktree; removing the dir alone does NOT deregister).
	pruneCmd := runner.Command(ctx, "git", "-C", repoRoot, "worktree", "prune")
	_ = pruneCmd.Run()
	delBranch := runner.Command(ctx, "git", "-C", repoRoot, "branch", "-D", branch)
	_ = delBranch.Run()
}
```
Order MATTERS (`:203-212`): `rm dir → worktree prune → branch -D`. Retry is bounded to
`worktreeAddMaxRetries = 3` (`:129`) with 50/100/200ms backoff (`:275`), triggered by either
`emptyHEADRace` or `isTransientWorktreeAddRace` (the macOS/APFS commondir "Undefined error: 0"
signature, `:136-139`). This honest post-add HEAD probe is the guard that turns a silent
downstream stall into a retryable create-time failure — carry it forward.

### B10. Does the remote path emit ANY events?

**Verified: NO event emission from `internal/workspace`.** A grep for actual emission calls
(`\.Emit`, `EmitWithRunID`, `EventEmitter` as a live symbol) across `internal/workspace/*.go`
(non-test) returns **zero code hits** — every match for "emit" is a **doc comment** describing
that the CALLER (the workspace manager / daemon, "the emitter owns the bus call") is responsible.
Examples of the comment pattern: `conflictescalation_wm023.go:15` ("Emit merge_conflict_escalation
on the event bus (emitter owns the bus call)"), `workspace.go:137` ("Transition does NOT emit
events. The caller (workspace manager S06) is [responsible]"), `leaselock.go:39`, `sessionlogdir.go:56`.

Consequently the remote materialize/worktree path (`remotematerialize.go`, `createworktree.go`) is
**silent on the event bus** — it returns wrapped `error` values only (e.g.
`writeRemoteFile` returns `fmt.Errorf("workspace: writeRemoteFile %s: %w\nremote: %s", ...)`,
`remotematerialize.go:85`), and the daemon layer decides what event (if any) to emit. All
`pasteinject_failed` / `implementer_budget_exceeded` / `worker_offline` events are emitted from
`internal/daemon` (`pasteinject.go:1470-1523`; `notifyConnectionFailure`, `tmuxsubstrate.go:2322`),
not from the workspace remote path. The synthesis's "emits none" is correct.

---

## Cross-cutting summary of the ack gap

Both boundaries are **fire-and-forget over exit-0**:
- tmux: `load-buffer`/`paste-buffer`/`send-keys` return 0 when tmux accepted the buffer/keys, not
  when the TUI consumed them. The only positive acceptance signal is the async `agent_heartbeat`/commit,
  guarded by blind timeouts (A3/A4).
- SSH: `ssh -- '<string>'` returns the remote command's exit code, one fresh connection per op,
  ControlMaster off. Correctness on top of it is bolted on by the flock script (B6) and the
  honest HEAD re-probe (B9) — both compensating for the lack of any transactional ack.
