# Process Lifecycle v0.3.0 — R2 Review: Daemon-Author

- **Reviewer persona:** senior Go engineer, long-running Unix daemons.
- **Spec reviewed:** `specs/process-lifecycle.md` @ v0.3.0 (draft), 789 lines.
- **Review date:** 2026-04-24.
- **Anchoring:** this is a round-2 review; R1 implementer flagged the major gaps (flock vs fcntl, SIGTERM→SIGKILL interval, panic recovery, composition-root bootstrap, agent-reap discipline) and v0.3.0 landed PL-002a, PL-006a, PL-008a, PL-009a, PL-011a, PL-014a, PL-018a, PL-020a, PL-021a, PL-027, and invariants PL-INV-004/005 in response. This review takes the v0.3.0 text as-given and asks: from the perspective of someone who has shipped Unix daemons in production, does the daemon lifecycle hold together as *daemon craft*, not just as an IDL-consistent delegation graph?

---

## 1. Verdict summary

**Partially sound; several daemon-craft load-bearing mechanisms are named but under-specified, and two are missing outright.**

What R1 earned v0.3.0: the primitive-selection clauses (PL-002a, PL-006a) are correct at the taxonomic level — fd-lifetime lock rejected process-lifetime fcntl, provenance-marker uses env+PGID. The skeleton is sound.

What v0.3.0 still leaves open and would burn an implementer at 03:00 when the daemon wedges on some operator's box:

1. **EINTR / SA_RESTART discipline is unstated.** Go's runtime installs `SA_RESTART` on nearly all syscalls by default, but the spec does not say the daemon must not rely on non-restart EINTR semantics, nor does it name the signal-delivery model (self-pipe? `os/signal.Notify`? `signalfd` is not Go-idiomatic but `signal.Notify` is). See §16.
2. **SIGCHLD / zombie reap is not named anywhere.** PL-014 delegates subprocess watch to HC-011 but no requirement — here or in HC — says "every spawned process MUST be `cmd.Wait()`-reaped or explicitly SIGCHLD-handled." A watcher goroutine is not the same as a SIGCHLD handler and the distinction matters when the watcher crashes or gets starved. See §8.
3. **Ready protocol is internal-only.** `daemon_ready` is an event on the bus and a state transition on `DaemonStatus`; there is no externally-observable ready signal (no socket-activation, no ready-fd, no notify-file, no exit-zero-after-ready sentinel for systemd Type=notify). A supervisor (systemd, launchd, s6, tmux wrapper) cannot tell the daemon is ready without speaking the socket. See §6.
4. **`execve` FD-inheritance on upgrade is unspecified.** PL-027 says "exec-replacement preserves the PID" and "socket MUST NOT be unlinked" but does not say whether the socket fd survives exec, whether `FD_CLOEXEC` is set on any fd, or how child-process fds (agent subprocesses spawned pre-upgrade) relate to the new parent's fd table after `execve`. See §9.
5. **ntm version-probe is underspecified.** PL-021a says "version pin per the external-inputs protocol" but the protocol is not named here and the version format is not declared (semver? date-stamp? ntm is Python — it may not even expose `--version` parsably). See §10.
6. **Parent-death handling on Linux.** PL-014 and PL-INV-005 say agent subprocesses are children of the daemon and get re-parented to init on crash — fine. But harmonik's orphan-sweep *assumes* the next daemon can kill them. If the operator uses a process-supervisor that terminates the daemon tree before the new daemon runs (e.g., `systemd-run --scope`, or `launchd` with `KillMode=control-group`), the subprocesses die with the daemon and no orphan sweep is needed; but if not, and the subprocesses stay alive, the spec does not name `prctl(PR_SET_PDEATHSIG)` on Linux for the inverse: making the subprocesses *die with the daemon* so the sweep is trivial. The current design intentionally inverts this (leave subprocesses alive so they can continue checkpointing) but the intent is not documented. See §13.
7. **`setrlimit` discipline is absent.** The daemon will open fds for event log, pidfile, socket, per-agent-subprocess stderr/stdout, per-agent control-socket connection, per-CLI-client connection, git-subprocess fds, beads-subprocess fds. With `RLIMIT_NOFILE` at macOS default 256 or Linux default 1024, an unbounded-ceiling daemon (PL-014a default "unbounded") will hit EMFILE on a project with ~100 handler sessions. See §17.

The spec remains *more* complete than most R1→R2 deltas I see. The eight issues above are the ones I would not ship without.

---

## 2. Fd-lifetime lock — PL-002a (lines 156–161)

### 2.1 Why fd vs fcntl: correctly chosen, correctly justified

PL-002a's rejection of POSIX `fcntl(F_SETLK)` in favor of `flock` / `fcntl(F_OFD_SETLK)` is textbook-correct. The POSIX fcntl hazard is the "close any fd → release all locks held by the process on that file" semantic, which breaks cleanly-written code the moment an fd is duplicated into a goroutine and closed there. fd-lifetime primitives (`flock` on macOS/Linux; `F_OFD_SETLK` on Linux) avoid this because the lock is attached to the open-file-description, not the PID.

**Line 158** correctly names both. Good.

### 2.2 NFS and filesystem-specific hazards: not addressed

**Gap.** `flock` on NFS is notoriously unreliable. Pre-Linux-2.6.12, `flock` on NFS was a no-op; post-2.6.12 it became a client-side-only advisory lock that does NOT coordinate across NFS clients. `F_OFD_SETLK` on NFS falls back to NFS's own byte-range-lock RPC protocol (`NLM`), which has well-known silent-failure modes if the NFS server restarts. A developer running harmonik on a CI box with `/home` on NFS will see PL-INV-001 violated silently.

**Proposed text to add as PL-002b (or inline to PL-002a):**

> The pidfile MUST reside on a filesystem that supports fd-lifetime advisory locks. If `.harmonik/daemon.pid` is on NFS, SMB/CIFS, or any filesystem where `flock(2)` returns `ENOTSUP` or `EOPNOTSUPP`, the daemon MUST refuse to start with exit code `9` "filesystem-unwritable" (per PL-008a) and a specific error message identifying the filesystem type. Detection SHOULD use `fstatfs(2)` / `statfs(2)` f_type against a conservative allow-list (ext*, xfs, btrfs, apfs, hfs+, zfs, tmpfs); unknown filesystems MUST probe by attempting `flock(LOCK_EX|LOCK_NB)` on a temp file and checking for `ENOTSUP`.

### 2.3 Lock on pidfile vs separate file

PL-002a locks the pidfile itself. This is the common pattern (`systemd-pid-file`, `nginx`, many others) and is fine. One subtle trap v0.3.0 does not address:

**Trap.** A process that holds `flock(LOCK_EX)` on a pidfile and truncates/rewrites that pidfile mid-lifetime can lose the lock if it closes the fd. The safe pattern is: open-with-`O_CREAT|O_RDWR` → flock → truncate → write PID → fsync → **keep the fd open for the daemon's lifetime**. Never close and reopen.

**Proposed amendment to PL-002a:**

> The daemon MUST open the pidfile with `O_CREAT|O_RDWR|O_CLOEXEC`, acquire the `flock(LOCK_EX|LOCK_NB)`, then `ftruncate(0)`, write the PID as ASCII followed by `\n`, and `fsync`. The fd MUST be retained for the daemon's lifetime and MUST NOT be duplicated into a child's fd table (set `FD_CLOEXEC`). The fd close-on-termination is what releases the lock; any intermediate close is FORBIDDEN.

### 2.4 Fallback: missing entirely

If `flock` fails for a reason other than contention — `ENOLCK` (kernel out of locks), `EBADF`, `EINVAL` on a corrupt fd — the spec says nothing. These are rare but not unheard of on exhausted hosts.

**Proposed addition:**

> On `flock` failure with `errno != EAGAIN && errno != EWOULDBLOCK`, the daemon MUST exit with exit code `9` "filesystem-unwritable" and emit `daemon_startup_failed{failure_mode=pidfile-lock-error}`; it MUST NOT fall back to process-lifetime `fcntl` locks (per PL-002a prohibition) or retry without bound.

### 2.5 Lock-and-PID atomicity

**Gap.** PL-002a describes `flock` + `kill(pid,0)` as the disambiguation path (live vs stale). It does not name the ordering inside the **holding** daemon: *when* does the daemon write its own PID vs *when* does it start accepting work? If the daemon acquires the lock, spawns some work, then crashes before writing the PID, the pidfile has PID=0 (or the prior daemon's PID). The stale-detection path reads the PID and probes `kill(0)`; a zero PID will probe process "0" (or fail). Spec the write-before-work ordering:

> The daemon MUST complete the lock+truncate+write+fsync sequence BEFORE emitting `daemon_started` (PL-005 step 2). An incomplete write — lock held, PID unwritten — MUST NOT occur on any code path; if the write fails, the daemon MUST release the lock (via close) and exit with code `9`.

---

## 3. Socket + JSON-RPC 2.0 — PL-003, PL-003a (lines 163–175)

### 3.1 Justification for JSON-RPC 2.0: underspecified, and probably wrong

**Concern.** JSON-RPC 2.0 is a reasonable choice — it's simple, well-documented, has multiple Go implementations, and handles notification vs request/response semantics. But v0.3.0 declares "JSON-RPC 2.0 request/response stream framed as newline-delimited JSON" and then promptly violates the spirit of JSON-RPC 2.0 on the next line:

> CLI clients MUST issue one JSON-RPC request per connection and close the connection on receipt of the response.

That is not JSON-RPC 2.0 — that's HTTP/1.0-style one-shot request/response over a Unix socket with JSON-RPC envelope shape. JSON-RPC 2.0 is designed for connection multiplexing with ID-correlated responses; forbidding multiplexing discards the main reason to use JSON-RPC over a custom verb-noun protocol.

**This is fine as a product choice** — single-shot is simpler — but the spec should either (a) drop "JSON-RPC 2.0" and call it "JSON-RPC 2.0 request/response envelope over one-shot NDJSON connections" (accurate), or (b) permit multiplexing for agent subprocesses (PL-003a line 173: "Agent subprocesses MAY hold their connection for the lifetime of the session" — this IS multiplexing and needs the id-correlation machinery spelled out).

### 3.2 Error envelope: implicit

JSON-RPC 2.0 defines a standard error envelope: `{jsonrpc: "2.0", id: N, error: {code: int, message: string, data?: any}}` with reserved code ranges (-32700 parse error, -32600 invalid request, -32601 method not found, -32602 invalid params, -32603 internal error, -32000..-32099 reserved for server implementation).

**Gap.** The spec does not inherit or restate the error envelope, nor does it declare whether the daemon maps its exit-code taxonomy (PL-008a) into the `error.code` space, or uses a separate reason-code taxonomy. OQ-PL-005 tracks the method inventory but not the error-envelope semantics.

**Proposed text:**

> Error responses MUST conform to JSON-RPC 2.0 error-object structure (`{code, message, data?}`). Reserved JSON-RPC 2.0 codes (-32700 through -32603) MUST be used for their canonical meanings. Server-defined codes in -32000..-32099 MUST follow the daemon's error taxonomy declared in [operator-nfr.md §8]; code-to-meaning mapping is tracked as OQ-PL-005a. The daemon MUST NOT emit non-conforming error envelopes (e.g., plain strings, HTTP-style status integers).

### 3.3 Batch requests: unhandled

JSON-RPC 2.0 supports request batches: `[req1, req2, req3]` producing `[resp1, resp2, resp3]` in a single round-trip. The spec implicitly forbids this (via the "one request per connection" rule for CLI, and unstated for agents). If a future CLI needs to enqueue 10 beads atomically, the spec says they must open 10 connections — wasteful.

**Proposed text:**

> Batch requests as defined in JSON-RPC 2.0 §6 are OUT OF SCOPE for MVH; the daemon MUST return `-32600 Invalid Request` if it receives a JSON array at the root of an NDJSON frame. Post-MVH support for batches is tracked as an open question.

### 3.4 Framing: stated, good

PL-003a correctly declares NDJSON framing with a 1 MiB line cap and abort-on-overflow. Good — this matches HC-007a.

**One nit.** NDJSON + JSON-RPC 2.0 has a canonical edge case: a request body containing a literal newline inside a string (JSON permits `\n` escape but not a raw newline) — the framing is safe. But the spec should name the canonical-form requirement: messages MUST be emitted as RFC 8259 JSON with no embedded raw newlines between tokens (pretty-printed JSON is FORBIDDEN on the wire — one frame = one line).

### 3.5 Notification vs request/response

JSON-RPC 2.0 defines "notifications" — requests with no `id`, for which the server MUST NOT respond. The spec does not say whether the daemon supports notifications at all.

**Daemon-author observation.** This matters for the agent-facing surface: `emit-outcome` could be a fire-and-forget notification if the agent doesn't care about the ack. But fire-and-forget is dangerous on a drain path (agent emits outcome, connection closes, daemon never processes the frame because it was mid-drain) — every `emit-outcome` should be ack'd. So the answer is probably "no notifications from agents."

**Proposed text:**

> Agent-originated requests MUST carry an `id` (JSON-RPC 2.0 request form, not notification form). Daemon-originated requests (if any — OQ-PL-005) similarly MUST carry an `id`. The daemon MUST return `-32600 Invalid Request` to any frame lacking `id`.

### 3.6 Socket mode 0600 + HC-044: load-bearing assumption

PL-003 line 165 says mode `0600` and effective-uid-owned, with HC-044 naming the authenticity model. Good. The daemon-author concern:

**Concern.** Setting `chmod(0600)` *after* `bind()` creates a race window — between `bind()` and `chmod()`, the socket is world-writable (umask-dependent) and any local user can `connect()`. The fix is to `umask(0077)` before `bind()`, or to bind inside a `0700`-protected parent directory. The spec does not name either.

**Proposed text (amend PL-003):**

> The daemon MUST ensure the socket file is not accessible to other local users at any point during its lifetime. The daemon MUST either (a) `umask(0077)` immediately before `bind(2)` and restore the prior umask after, OR (b) ensure the parent directory (`.harmonik/`) has mode `0700` and owner == effective-uid before bind. Option (b) is preferred because `umask` is process-global state and interferes with concurrent goroutines touching other files.

---

## 4. Project-hash + provenance marker — PL-006a (lines 220–230)

### 4.1 Collision probability: fine

12 hex chars = 48 bits of SHA-256 truncation. Birthday collision at ~2^24 = 16M projects on one machine — not a concern. **Good.**

### 4.2 Hash input: not authoritative

**Gap.** PL-006a says `SHA-256(abspath(project_root))`. What is `abspath`?

- On macOS with `/Users/gb/github/harmonik` symlinked from `/private/Users/gb/github/harmonik` (via APFS firmlinks), which form is hashed?
- On Linux with bind-mounts, does `/home/x/project` vs `/mnt/data/project` (same inode, different path) hash the same?
- On case-insensitive filesystems (macOS APFS default), does `/Users/gb/HARMONIK` hash the same as `/Users/gb/harmonik`? If not, two daemons could legitimately claim the same project — same inode, different hashes, both take the pidfile lock by racing. PL-INV-001 violated.

**Proposed text:**

> `abspath(project_root)` MUST be the output of `filepath.Abs` (Go stdlib) on the `--project-root` flag value or the current working directory, with symlinks resolved via `filepath.EvalSymlinks`. On case-insensitive filesystems, the hash MUST be computed from the case-folded form (NFC + lowercase on macOS APFS). Filesystems on which path canonicalization is ambiguous (e.g., ZFS case-retain-case-fold mode) MUST be detected at startup and MUST fail with exit code `9` "filesystem-unwritable"; a specific case-fold-ambiguity error message is tracked as OQ-PL-008a.

Alternative (simpler): hash the inode pair `(st_dev, st_ino)` of the `.harmonik/` directory. This is filesystem-level identity, not path-level, and sidesteps every symlink/case-fold issue. But it is not portable across moves (moving the project directory changes `st_ino` on some filesystems). Trade-off — path-based is more stable, inode-based is more collision-free. I'd document the trade-off and stick with path-based.

### 4.3 PGID fragility on fork-exec chains

**Concern (daemon-author core expertise).** PL-006a says "setpgid-on-spawn with a per-daemon-instance group-leader PID recorded in the pidfile." This has several fragility modes:

1. **Setpgid race.** On Linux, `setpgid(child_pid, pgid)` can fail with `EACCES` if the child has already called `execve`. The safe pattern is for the CHILD to call `setpgid(0, desired_pgid)` before `exec`, OR for the parent to call `setpgid(child_pid, pgid)` in a retry loop tolerating `EACCES`. Go's `exec.Cmd.SysProcAttr.Setpgid` with `Pgid: 0` makes the child its own group leader; setting `Pgid: nonzero` is brittle.

2. **Leader-exit breaks the group.** If the "per-daemon-instance group-leader PID" is the daemon's own PID, and the daemon dies, the process group persists (processes can stay in the group even after the leader dies — the PGID is just a number) but becomes *orphaned*. An orphaned process group is subject to SIGHUP/SIGCONT on terminal disconnect in a login session, which is not the daemon's concern, but the PGID can eventually be reused by an unrelated process after all group members exit. The orphan sweep's PGID match can then false-match a new, unrelated process.

3. **Grandchildren.** If an agent subprocess spawns a grandchild (compiler, git, etc.) without `setpgid`, the grandchild inherits the parent's PGID — good, the sweep catches it. But if the agent subprocess or its shell wrapper changes its own PGID (e.g., `setsid`), the grandchild tree escapes the sweep. Handlers that use `setsid` to daemonize sub-tools will leak.

4. **macOS gotcha.** macOS `setpgid` has the same semantics as Linux but `/proc` is absent, so the sweep relies on PGID exclusively (OQ-PL-008). The `ps -o pgid` invocation is the only cross-platform way to enumerate — and `ps` output is not structured, varies by version, and has ~100ms invocation cost per sweep on a cold machine.

**Proposed amendment to PL-006a:**

> (iii) The per-daemon-instance process group leader MUST be a dedicated setsid-created session leader, NOT the daemon's own PID. On startup, the daemon MUST call `syscall.Setsid()` (or equivalent) before spawning any subprocess, producing a process group whose PGID equals the daemon's PID at that moment. This PGID MUST be recorded in the pidfile alongside the PID (format: two ASCII decimal integers, one per line). The daemon MUST set child-side `setpgid(0, pgid)` via Go `SysProcAttr{Setpgid: true, Pgid: <recorded_pgid>, Foreground: false}` on every spawn, and MUST retry once on `EACCES` (child has already exec'd).
>
> (iv) Subprocess trees that call `setsid` internally (e.g., handler wrappers using nohup-style tricks) escape the PGID marker. Handlers documented to use `setsid` are out of conformance with PL-INV-005; the sweep cannot reap their descendants. This hazard MUST be declared in the handler-contract spec per [handler-contract.md §4.1 HC-001] (OQ-PL-008b).

### 4.4 Recording PGID in the pidfile: a concrete proposal

The pidfile currently contains just the PID (PL-002). Adding PGID on a second line:

```
12345
12345
```

is backward-compatible (consumers reading line 1 for PID still work) and gives the orphan sweep its PGID input without a second file. I'd add this explicitly:

> PL-002 amendment: the pidfile MUST contain exactly two ASCII decimal integers, each terminated by `\n`: line 1 = daemon PID, line 2 = daemon PGID (per PL-006a(iii)). Readers MUST tolerate a one-line pidfile for backward compatibility with v0.2.0 format.

---

## 5. Startup sequence — PL-005 (lines 187–202)

### 5.1 Step ordering: mostly sound, one reordering

v0.3.0's step 0 (composition-root bootstrap) is correct — the event bus must exist before `daemon_started` at step 2 emits. Step 1 (pidfile lock) correctly precedes step 2 so that a second-daemon-start doesn't emit a spurious started event. Steps 3–9 are reasonable.

**One observed reordering opportunity:** step 2 emits `daemon_started` with `{started_at, pid, binary_commit_hash}` but step 3 (orphan sweep) may kill tmux sessions and subprocesses — operations with observable external effects. An observer seeing `daemon_started` at T0 and then observing their tmux session vanish at T0+50ms has no in-between event to explain the delete. Consider:

> Step 2a: emit `daemon_pre_sweep` (new event) with `{project_hash, pidfile_pgid}` immediately before step 3. This lets operators correlate tmux-session kills to a specific daemon generation. Deferred as OQ-PL-010 if cost-prohibitive.

This is a nice-to-have, not a blocker.

### 5.2 All-or-fail-back vs progressive: ambiguous

**Gap.** If step 3 (orphan sweep) partially fails — say the tmux kill succeeds but one subprocess refuses SIGKILL — does the daemon:

(a) abort startup (exit with some failure code), or
(b) proceed to step 4 and report the partial failure via `daemon_orphan_sweep_completed` with an `errors[]` field?

v0.3.0 does not say. PL-006 step "subprocess cleanup" says "kill them via SIGTERM followed by SIGKILL after a bounded 5-second interval" — but SIGKILL can be *ignored* in two cases: (1) the process is uninterruptible (D state, kernel-side stuck), or (2) the process is zombied (the parent is gone and nobody reaped it — `kill(pid, SIGKILL)` returns success but the zombie persists until reaped). In case (2) the sweep cannot proceed; in case (1) the sweep must wait or fail.

**Proposed text amendment to PL-006:**

> Subprocess-cleanup bullet amendment: After the SIGKILL MUST be issued, the daemon MUST wait up to 2 seconds (OQ-PL-002) for the process to disappear from `/proc` or fail to respond to `kill(pid, 0)`. If the process is still alive after 2 seconds:
> - If the process is in state `D` (uninterruptible sleep; readable from `/proc/<pid>/stat` field 3 on Linux), the daemon MUST log a warning, add the PID to `daemon_orphan_sweep_completed.errors[]`, and proceed.
> - If the process is in state `Z` (zombie), the daemon MUST issue `waitpid(pid, _, WNOHANG)` to reap it (this requires the daemon to be a descendant of the process's original parent — if it is not, the reap attempt silently fails and the zombie lingers until init reaps it at next daemon exit).
> - Otherwise the process is ignoring SIGKILL, which is impossible for user-space Linux processes; this condition MUST emit `daemon_startup_failed{failure_mode=orphan-unreapable}` and exit with a new exit code `13` "orphan-unreapable" (coordinate with ON-003 catalog).

### 5.3 Step 5 (git walk) cost

**Concern.** PL-005 step 5 walks the git log for every branch matching `run/<run_id>`. On a long-running project, this could be thousands of branches. The walk happens on every restart and the current RTO target (operator-nfr ON-031) is unstated numerically — but most daemons target <5s boot. A `git for-each-ref refs/heads/run/` enumerates fast; walking each branch's commit trail to find `Harmonik-Run-ID` trailers scales with total-run-commits.

Not a blocker for MVH but worth a note:

> INFORMATIVE: Step 5's cost scales with the product (branch-count × commits-per-branch). For projects with >1k historical runs, operators SHOULD prune completed run branches (via a post-merge gc hook in [workspace-model.md]) to keep startup within RTO. Pruning policy is out of scope here; tracked as OQ-PL-011.

---

## 6. Ready-state — PL-009, PL-009a (lines 268–296)

### 6.1 Ready criteria: internally sound

The five conditions of PL-009 are correct for internal consumers (the daemon itself, operator-nfr's RTO measurement). Good.

### 6.2 Ready protocol for external callers: MISSING

**Major gap, daemon-author core expertise.** `harmonik daemon` will be invoked by:

- `systemd` unit files (`Type=notify` with `sd_notify("READY=1")`, or `Type=exec` with file-descriptor-based ready semantics)
- `launchd` plists (`RunAtLoad=true`, `KeepAlive=true` — launchd assumes the daemon is ready when it's alive)
- `s6` / `runit` supervisors
- `tmux new-session 'harmonik daemon'` (operator use; no supervisor)
- `harmonik runner` (PL-028; described as "wait for `daemon_ready`")

None of these can observe the event-bus `daemon_ready` without connecting to the daemon's socket — which is only available AFTER the socket is bound, which is not explicitly required to be before `ready`. The spec says PL-003 binds on startup (step 0-ish via composition-root) but the socket's role as ready-signal is not named.

**Concrete problem.** `harmonik runner` step (2) says "wait for `daemon_ready`" but a newly-started daemon takes ~seconds to bind socket + run orphan sweep + walk git + query beads + become ready. The runner must poll the socket. There is no retry/backoff/timeout bounded here.

**Proposed text (new requirement PL-009b):**

> PL-009b — Ready-protocol surface for external callers.
>
> External processes (operator CLI, process supervisors, `harmonik runner`) MUST detect daemon readiness through one of the following mechanisms, in order of preference:
>
> (a) **Socket probe.** Connect to `.harmonik/daemon.sock`, send JSON-RPC `status` request, receive response with `status ∈ {ready, degraded, reconciling}`. `reconciling` means the daemon is alive but not yet ready; the caller MUST retry with exponential backoff (initial 100ms, max 2s, capped at T_ready_wait = 60s default per OQ-PL-002). `ready` means ready.
>
> (b) **systemd notify (Linux).** When launched under systemd `Type=notify`, the daemon MUST call `sd_notify("READY=1")` at the same point PL-009 emits `daemon_ready`. Detection: presence of `$NOTIFY_SOCKET` environment variable.
>
> (c) **Ready-file (portable fallback).** The daemon MAY write `.harmonik/daemon.ready` at the moment of PL-009 emission and MUST remove it on any transition out of `ready` (drain start, exit, degraded). This is informative, not normative, and exists for solo-dev fswatch-based setups.
>
> External callers MUST NOT assume the daemon is ready simply because the pidfile or socket file exists. Connection refusal (`ECONNREFUSED`) from the socket means the daemon has not yet called `listen()`; connection success with `reconciling` status means the socket is bound but startup is incomplete.

### 6.3 ready_at wall-clock timestamp

**Nit.** PL-009 line 280 says `ready_at` is wall-clock. If the operator changes the system clock between `daemon_started` and `daemon_ready` (rare, but common in VMs waking from suspend), the RTO measurement (SIGTERM → `daemon_ready`) will be wrong. Monotonic clock is safer.

**Proposed text amendment to PL-009:**

> `ready_at` MUST be the wall-clock time at emission (for operator-observability and log correlation); the RTO measurement per [operator-nfr.md §4.8] MUST use the monotonic clock, not wall-clock, to avoid NTP/suspend skew. Implementations MUST include a monotonic-duration field `ready_duration_ms` in the event payload (schema coordination with [event-model.md §8.7.2]).

---

## 7. SIGTERM drain — PL-011 (lines 311–329)

### 7.1 Signal handling primitive: not named

**Concern.** Go's `os/signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)` is the idiomatic primitive. It uses the runtime's internal self-pipe under the hood. The spec does not require this — in principle a daemon could use `signalfd` via cgo or `golang.org/x/sys/unix` directly.

**Why it matters.** `signal.Notify` coalesces duplicate signals (two SIGTERMs in rapid succession → one channel delivery). For a drain that needs to distinguish "single SIGTERM = graceful" from "double SIGTERM = immediate" (a common Unix convention — second SIGTERM after grace period → escalate to kill), coalescing is a bug.

**Proposed text as amendment to PL-011:**

> The daemon MUST use a signal-delivery mechanism that preserves signal counts (not just presence): Go's `os/signal.Notify` with a buffered channel of size ≥ 2 is the reference implementation. On receipt of a second SIGTERM within T_drain (the operator-configurable drain timeout per [operator-nfr.md §4.7 ON-029]), the daemon MUST escalate to the immediate-shutdown path per PL-012 (skip drain, SIGKILL surviving agents). This "double-tap" behavior matches the Unix convention and is the standard operator rescue for a stuck drain.

### 7.2 In-flight agent completion guarantee

**Concern.** PL-011 step 3 says "Allow in-flight runs to proceed to their next durable checkpoint per EM-017, then stop advancing them." The guarantee that a run *reaches* its next checkpoint is contingent on the agent subprocess's cooperation — it may hang, it may be rate-limited, it may be blocked on a git lock. PL-011 step 4 says "Wait for agent-subprocess termination (bounded by drain timeout)" but does not say **what happens between step 3 and step 4**: does the daemon block on each run's checkpoint completion? Poll? Use the watcher-goroutine's completion signal?

**Proposed text:**

> PL-011 step 3 amendment: the daemon signals its dispatch loop to stop pulling new beads; existing agent subprocesses are signaled via a "drain" message on their socket connections (method TBD under OQ-PL-005: `daemon_draining`) asking them to complete their current unit-of-work and exit. Agents MUST treat `daemon_draining` as an advisory: they MAY finish their current step, but MUST NOT start a new step. The watcher goroutine per HC-011 observes agent exit; when all watchers report exit, step 3 completes. The step-3-complete signal to step 4 is the watcher-completion aggregation.
>
> PL-011 step 4 amendment: the "drain timeout" is measured from step 3 start (signal-dispatched) to the step-3-complete aggregation. On expiry, any still-running agent receives SIGTERM, wait 5s (HC-018 bound), then SIGKILL. The watcher goroutines' `cmd.Wait()` reaps the exit status.

### 7.3 Step 8 order: potentially wrong

Step 8 says "Release the pidfile lock AND remove the pidfile on clean shutdown ... Remove the socket file." The ordering inside step 8:

- Remove the socket file FIRST (prevent new client connections and interleave with already-connected clients' EOF on socket close).
- Close the listener (EOF to connected clients).
- Drain any remaining in-flight socket requests (bounded).
- Release the pidfile lock (via fd-close).
- Remove the pidfile (rm; race-safe because no other daemon can claim the lock until close).

**Proposed text:**

> PL-011 step 8 amendment: the sequence WITHIN step 8 MUST be (a) close the socket listener, (b) close the socket file (unlink), (c) close the pidfile fd (releases flock), (d) unlink the pidfile. Reordering (c) before (a) is FORBIDDEN because a new daemon could acquire the lock and race to `bind()` a socket whose file still exists.

---

## 8. SIGKILL reaping / zombies — §4.5, §4.8

### 8.1 Zombie handling is not named anywhere

**Major gap, daemon-author core expertise.** On any Unix, a dead child process stays as a zombie (state `Z` in ps) until its parent calls `wait`-family syscall to collect its exit status. If the daemon spawns agents and the daemon's watcher-goroutines reap via `cmd.Wait()` (Go's idiom), that's fine. **But the spec does not require this.**

- **PL-014** delegates supervision to HC-011 and HC-024.
- **PL-016** says the watcher is "the exclusive caller of `cmd.Wait()`" — but this is a statement of exclusivity, not of *existence*. No requirement says the watcher MUST call `cmd.Wait()`.
- **Handler-contract HC-011** (not reviewed here; R1 implementer cited it) presumably says this.

**Proposed text (confirm with HC or state here explicitly):**

> PL-014 amendment: every spawn MUST have exactly one Go goroutine that owns the `*exec.Cmd` and MUST call `cmd.Wait()` exactly once to reap the child's exit status. Failure to call `cmd.Wait()` produces a zombie that persists until the daemon exits (at which point init reaps it). Leaking zombies is a conformance violation under PL-INV-005 regardless of whether `kill(pid, 0)` reports the zombie as "alive" (it does).

### 8.2 SIGCHLD handler vs watcher goroutine

**Clarification.** Go's runtime does not install a SIGCHLD handler by default; `cmd.Wait()` uses `waitpid(pid, ...)` which doesn't need SIGCHLD. So a Go daemon that reaps via `cmd.Wait()` per child is zombie-safe without SIGCHLD.

**However.** If the daemon spawns grandchildren (e.g., an agent subprocess that itself forks without reaping), the grandchildren's zombies are the agent's problem, not the daemon's — until the agent dies and its zombies re-parent to init, at which point init reaps them. This is fine and the spec is implicitly correct; worth a note:

> INFORMATIVE: Grandchild zombies (processes spawned by handler subprocesses that those handlers do not reap) re-parent to init on the handler's exit and are reaped by init. The daemon's reap discipline is scoped to its direct children (handler subprocesses); grandchildren are the handler's responsibility per [handler-contract.md §4.3].

### 8.3 Orphaned grandchildren

**Concern.** If an agent subprocess spawns a grandchild that outlives the agent (e.g., a long-running compile that the agent exits without waiting for), the grandchild re-parents to init. The provenance marker env var `HARMONIK_PROJECT_HASH` is preserved across exec (per POSIX `execve` env-inheritance) but the PGID may or may not be — if the grandchild called `setsid`, the PGID is gone.

**Proposed text (amend PL-006):**

> PL-006 subprocess-cleanup amendment: the sweep's process enumeration MUST match on the environment-variable marker `HARMONIK_PROJECT_HASH=<project_hash>` (Linux via `/proc/<pid>/environ`) OR on the PGID (darwin, or Linux where environ is unreadable due to permissions). A grandchild that has called `setsid` (breaking PGID) but retained `HARMONIK_PROJECT_HASH` is still detectable on Linux; the same grandchild on darwin is NOT detectable and leaks until the next OS reboot. This is a known darwin limitation and is tracked as OQ-PL-008.

---

## 9. `exec` replacement — PL-027 (lines 473–494)

### 9.1 FD inheritance across `execve`: unspecified

**Major gap, daemon-author core expertise.** PL-027 (iii) says "The socket file `.harmonik/daemon.sock` MUST be re-bound by the new daemon within a bounded window `T_rebind` (default 2s) after exec-replacement." But the fd rules across `execve` are:

- By default, **all open fds survive `execve`** unless they have `FD_CLOEXEC` (or were opened `O_CLOEXEC`).
- Go (since 1.12) sets `O_CLOEXEC` on all fds by default.

So if the daemon's Go code opens the socket listener, then calls `syscall.Exec`, the socket fd is **closed by the kernel during exec** because Go set `O_CLOEXEC`. The new binary has no inherited socket — it must re-bind, which requires the OLD daemon to have closed the listener before exec (otherwise EADDRINUSE).

**This is a bug in PL-027 as written.** It says:

> The daemon MUST NOT unlink the socket file during upgrade (it is the SAME file the new binary re-binds)

But the old daemon MUST close (not unlink) the socket fd before exec, AND must unlink-and-rebind because `bind()` on an existing socket path returns EADDRINUSE even when no process is listening (unless `SO_REUSEADDR`-equivalent for Unix sockets, which is unlinking).

**Two viable designs:**

**Design A: fd-passing across exec.** The old daemon clears `FD_CLOEXEC` on the listener fd via `fcntl(F_SETFD, 0)` before exec. The new binary inherits the fd (detectable via env var `LISTEN_FD=N` or systemd convention). Zero downtime, no rebind needed.

**Design B: close-and-rebind.** The old daemon closes the listener before exec. The new binary unlinks the socket file (it exists but no process is listening) and rebinds. ~2s of "connection refused" for clients.

PL-027 (iii) describes a muddled Design B ("MUST NOT unlink" + "MUST re-bind within 2s"). This is broken: you cannot rebind without unlinking (EADDRINUSE), so either the spec requires Design A (and needs to describe fd-passing) or Design B (and needs to allow unlink-and-rebind).

**Proposed rewrite of PL-027 (iii):**

> (iii) **Socket continuity via fd-passing.** The outgoing daemon MUST clear `FD_CLOEXEC` on the listener fd (via `fcntl(listener_fd, F_SETFD, 0)`) immediately before `execve`, pass the fd number to the new binary via the environment variable `HARMONIK_LISTENER_FD=<fd_number>` (and via `HARMONIK_UPGRADE=1` as the detection marker per (ii)), and MUST NOT close the fd before exec. The new binary, on detecting `HARMONIK_UPGRADE=1`, MUST NOT call `bind()` — instead it MUST call `net.FileListener(os.NewFile(fd, ""))` to adopt the existing listener. The socket path `.harmonik/daemon.sock` remains bound to the pre-exec listener throughout the exec window; clients observe no connection-refused gap.
>
> The fd MUST then have `FD_CLOEXEC` re-set by the new binary to prevent leaking into future spawns.
>
> Design B (close-and-rebind with ~2s gap) is NOT normative for MVH; systemd-socket-activation-like fd-passing is required.

Design A is the correct choice for production daemons; nginx, HAProxy, envoy all do this (or a variant). v0.3.0 is currently neither — it's a statement that contradicts itself.

### 9.2 FD-inheritance for child processes

**Concern.** Agent subprocesses spawned by the pre-exec daemon have open socket connections to the pre-exec daemon's listener (the same listener fd that gets passed to the new daemon). When the new daemon starts, it inherits the listener but does NOT inherit the per-connection fds (those are in the old daemon's fd table, closed at exec). So:

- Existing agent connections are silently closed (RST or EOF depending on kernel).
- The agent's in-flight request is lost; the agent must reconnect.

This is fine if the agent protocol is retryable (and it should be), but the spec does not say:

> PL-027 amendment: agent subprocess connections to the pre-exec daemon MUST be expected to close on exec. Agents MUST reconnect on connection EOF and retry any in-flight request with the same idempotency key (per [beads-integration.md §4.10 BI-030]). The new daemon, on accepting reconnections, MUST treat them as continuations of in-flight sessions, not new sessions; session-continuity semantics are owned by [handler-contract.md §4.3].

### 9.3 CLOEXEC discipline: stated nowhere

Beyond the upgrade path, the daemon opens many fds: pidfile, event-log, dead-letter log, spill files, intent-log files, per-connection sockets. **All of these MUST have `FD_CLOEXEC`** or they leak into every agent subprocess, which is:

- A security hole (agents can read/write daemon state files).
- A resource leak (each handler launch inherits dozens of fds it doesn't need).
- A debugging nightmare (agents see `/proc/self/fd` full of daemon fds).

Go sets `O_CLOEXEC` by default since 1.12, so in practice this is fine, but the spec should assert it:

> Proposed new requirement PL-014b — FD-CLOEXEC discipline.
>
> Every fd opened by the daemon MUST have `FD_CLOEXEC` set. Go implementations rely on Go 1.12+'s default `O_CLOEXEC`-on-open behavior; lower-level fd creation (raw `syscall.Socket`, `syscall.Open`) MUST explicitly set `O_CLOEXEC` or call `fcntl(F_SETFD, FD_CLOEXEC)` immediately after open. The upgrade path (PL-027 (iii)) is the only exception: the listener fd is deliberately un-CLOEXEC'd for fd-passing, then re-CLOEXEC'd in the new binary.

---

## 10. ntm version-pin — PL-021a (lines 431–435)

### 10.1 Version-probe mechanism: not named

**Gap.** PL-021a says "version-pin per the external-inputs protocol (parallel pattern to ON-017)". The external-inputs protocol is not declared in this spec, and ntm is a Python tool (per harmonik's docs) — the Python convention is `ntm --version` returning something like `ntm 0.4.2` on stdout. But:

- Not all Python tools implement `--version`.
- Some print version to stderr, some to stdout.
- Some return non-zero exit codes on `--version` (rare but seen).
- Some print commit hashes, not semver.

**Proposed amendment:**

> PL-021a amendment: the version probe MUST be `ntm --version` reading stdout for a line matching the regex `ntm\s+(\d+)\.(\d+)\.(\d+)(?:[-.][a-zA-Z0-9]+)?`. Exit codes other than 0 on `--version` MUST classify as "version-unknown" and be treated as an incompatible version (exit code 11 "ntm-unavailable"). The supported version range MUST be declared as a semver range in the release manifest (e.g., `>=0.4.0 <1.0.0`). Out-of-range versions MUST produce exit code 11 with a specific error message naming the detected version and the required range.

### 10.2 Minimum version: undeclared

**Gap.** "Supported `ntm` versions MUST be declared in the release manifest." The release manifest doesn't exist yet. For MVH, the spec should declare a minimum version or admit that the minimum is TBD:

> INFORMATIVE: At MVH, the supported ntm version range is the single version pinned by the harmonik release manifest (no range support). A coordinated version pin is tracked as OQ-PL-012.

### 10.3 Absence detection

PL-021a says "detected during PL-005 step 4 Cat 0 pre-check." Good. The concrete detection mechanism:

> `exec.LookPath("ntm")` returns `ErrNotFound` → exit code 11. `exec.LookPath` succeeds but `ntm --version` times out (>5s per RC-012 timeout rule) or returns non-zero → exit code 11. Version-probe output fails regex match → exit code 11. Version outside supported range → exit code 11 with a specific reason code distinguishing absent-vs-wrong-version for operator-facing triage.

These are all "exit code 11" today. An implementer might reasonably want to distinguish "ntm not installed" from "ntm too old" — both are operator-actionable but the remediation differs. Consider:

> PL-008a amendment: split code 11 "ntm-unavailable" into 11a "ntm-not-found" (PATH miss), 11b "ntm-incompatible-version" (regex match but out of range), and 11c "ntm-probe-failure" (exec error, timeout, unparsable). These are three separate exit codes for operator-triage clarity.

---

## 11. Panic barrier — PL-018a (lines 396–401)

### 11.1 Top-level recover in main: correct

The base requirement is right. Go's `recover()` only works inside `defer`'d functions, so the idiomatic pattern is:

```go
func main() {
    defer func() {
        if r := recover(); r != nil {
            // emit event if bus is up
            // log to stderr
            // exit 12
            os.Exit(12)
        }
    }()
    // ... main loop
}
```

PL-018a implies this but does not state the pattern. **Non-blocker.**

### 11.2 Per-goroutine recover discipline: underspecified

**Concern.** PL-018a line 398–399 says:

> panics inside other daemon goroutines (dispatcher, reconciler, subsystem loops) MUST be caught by per-goroutine `recover()` and escalate to the top-level barrier only on repeated failure (the exact escalation threshold is implementation-defined at MVH).

Two problems:

1. **"Per-goroutine recover" is not automatic.** Go does not install recover in goroutines started with `go f()`; the goroutine crashes the entire process on panic unless f() itself defers a recover. So the requirement is: every `go` statement in the daemon MUST wrap its function body in a recover. This is a code-review / lint concern, not just a runtime requirement.

2. **"Escalate on repeated failure" is vague.** What counts as "repeated"? Three panics in 60s? A panic-loop in one goroutine that goes round-robin with a recover-restart-panic-restart cycle?

**Proposed text:**

> PL-018a amendment: every goroutine launched by daemon code (including subsystem loops, dispatcher, reconciler, watcher goroutines per HC-011, event-bus consumers, socket-connection handlers) MUST begin with `defer func() { if r := recover(); r != nil { ... } }()`. The recover handler MUST: (a) log the panic value and stack trace to the daemon's stderr and the event bus (as an `error` event per [event-model.md]), (b) emit a per-goroutine counter `goroutine_panic_count{goroutine=<name>}`, (c) on N panics within T seconds (N=3, T=60 default, operator-configurable), escalate by `panic()`-ing out of the recover (which propagates to the next recover up, or to the top-level barrier, terminating the daemon with exit code 12). A helper wrapper (`safeGo(name, fn)`) is the idiomatic pattern; a lint rule forbidding bare `go f()` in daemon code is RECOMMENDED.

### 11.3 Panic during startup

**Gap.** PL-018a says the panic exits with code 12 and "emit `daemon_startup_failed` (if the event bus is initialized) or `daemon_shutdown{mode=immediate}` (if ready has been reached) on a best-effort basis." What about panics DURING startup but AFTER the event bus initialized (step 0) but BEFORE ready?

> Proposed amendment: during step 0 (before the event bus exists), a panic MUST log to stderr only and exit 12. During steps 1-8 (event bus up, not ready yet), a panic MUST emit `daemon_startup_failed{failure_mode="panic", panic_message=...}` on a best-effort basis and exit 12. After ready, panic emits `daemon_shutdown{mode=immediate}`. "Best-effort" means: attempt to emit with a 500ms timeout; if the event bus's JSONL writer is also panicking, skip the emit.

---

## 12. Orphan sweep — PL-006 (lines 207–218)

### 12.1 Process discovery mechanism: not named

**Gap (daemon-author core expertise).** The sweep needs to enumerate every process on the machine (or at least every harmonik-provenance-marked process). The mechanism:

- **Linux:** iterate `/proc/*/environ` for `HARMONIK_PROJECT_HASH=<hash>`. This requires read permission on `/proc/<pid>/environ`, which is `-r-------- root <uid>` on modern Linux — the daemon (running as `<uid>`) can read its own-uid processes but NOT root-owned processes. Fine for harmonik (agents run as the daemon's uid).
- **macOS:** `/proc` does not exist. `libproc`'s `proc_listpids` + `proc_pidinfo(PROC_PIDLISTFDS)` can list processes, but reading env vars requires `proc_pidinfo(PROC_PIDTASKINFO)` which only gives command-line-length, not env. The `sysctl` path `kern.procargs2` can read env vars but requires the target process to be owned by the caller (same uid). Fine for harmonik.
- **Fallback `ps`:** the shell-quoted `ps eax` can print env vars on macOS (though permission-restricted), but parsing ps output is fragile (spaces in cmdlines, truncation).

None of this is in the spec.

**Proposed text amendment to PL-006 (subprocess-cleanup bullet):**

> Process enumeration mechanism:
> - **Linux:** iterate `/proc/*/environ` (null-separated key=value pairs), matching on `HARMONIK_PROJECT_HASH=<project_hash>`. Unreadable entries (permission denied) MUST be skipped. PGID fallback: iterate `/proc/*/stat` field 5 (pgid) for matches against the recorded pgid from the pidfile.
> - **darwin:** use `sysctl`'s `CTL_KERN.KERN_PROCARGS2` for each pid returned by `sysctl`'s `CTL_KERN.KERN_PROC.KERN_PROC_ALL`. Env vars appear after argv in the procargs2 payload. PGID fallback: `proc_pidinfo(pid, PROC_PIDTASKINFO)` returns `pbi_pgid`.
> - **Other Unix (FreeBSD, etc.):** out of scope for MVH; tracked as OQ-PL-013.
>
> Go implementations MAY use `github.com/shirou/gopsutil/v3/process` as a cross-platform wrapper, provided the library version is pinned (per `go.mod`).

### 12.2 Walking subprocess trees

**Concern.** A harmonik agent might spawn helper processes (git, compilers, test runners) that inherit `HARMONIK_PROJECT_HASH` by env inheritance but are not direct children of the daemon. These SHOULD be swept (they're leaking resources under harmonik's name) but the current PL-006 text only talks about "processes re-parented to init" — implicitly direct-children-of-daemon that survived the daemon's death.

**Clarification needed:**

> PL-006 amendment: the subprocess-cleanup bullet matches on (a) `HARMONIK_PROJECT_HASH` env var regardless of current parent — so grandchildren that inherited the env var are also candidates — and (b) PGID match regardless of parent. The sweep SHOULD NOT require the process to be parented to init (PID 1); re-parenting to init is one indicator of orphan-ness, but a harmonik-marked process whose parent is ALSO a harmonik-marked process (and both are orphans) is still an orphan.

---

## 13. Subprocess parentage — PL-INV-005 (lines 549–555)

### 13.1 `prctl(PR_SET_PDEATHSIG)`: intentionally omitted?

**Daemon-author core expertise.** On Linux, `prctl(PR_SET_PDEATHSIG, SIGTERM)` makes the kernel send SIGTERM to the calling process when its parent dies. If handler subprocesses set this on themselves (at handler-side, not daemon-side — the syscall must be called by the child post-fork-pre-exec), then when the daemon dies, all handler subprocesses receive SIGTERM and can exit cleanly.

The spec does NOT name `PR_SET_PDEATHSIG`, and its absence appears intentional: PL-INV-005 says agent subprocesses re-parent to init on daemon death and are cleaned by the next daemon's orphan sweep. So the design choice is "let them outlive the daemon" — which makes sense for checkpoint-complete semantics (agent may be mid-checkpoint when daemon dies; letting it finish is better than killing it).

**But this choice is not documented.** A reviewer or implementer reading PL-INV-005 for the first time will ask: "why not `PR_SET_PDEATHSIG`?" Because the design is "survive daemon death, get cleaned on next startup." Say so:

> Proposed text (PL-INV-005 RATIONALE note):
>
> RATIONALE: Agent subprocesses deliberately DO NOT use `prctl(PR_SET_PDEATHSIG)` on Linux (or `kqueue EVFILT_PROC NOTE_EXIT` + self-kill on darwin). The design lets an agent survive daemon crash and continue working toward its next checkpoint; the next daemon's orphan sweep (PL-006) reaps the agent on restart. The alternative — die-with-daemon — would lose in-progress work that the agent could have checkpointed.
>
> Handlers that prefer die-with-daemon semantics for their agents MAY set `PR_SET_PDEATHSIG` in their launch sequence, but this is handler-level policy, not a PL-level requirement, and is tracked as OQ-HC-NNN.

### 13.2 macOS equivalent: kqueue

**Informative.** darwin has no `PR_SET_PDEATHSIG`. The equivalent is `kqueue` with `EVFILT_PROC` and `NOTE_EXIT` filter watching the parent PID; on event, send self SIGTERM. But given the above rationale (we deliberately don't want this), it's a non-issue.

### 13.3 Parentage-verification sensor

**Concern with PL-INV-005 sensor.** The sensor is "every spawn site MUST set the provenance marker." This is a *spawn-time* check, not a *runtime* check — the invariant asserts something about every live subprocess, but the sensor only validates spawn-time behavior. If a spawn site has a bug that occasionally skips the marker, the invariant is silently violated until the orphan sweep fails to reap a survivor.

**Proposed amendment:**

> PL-INV-005 sensor amendment: in addition to spawn-time marker-set validation, the daemon MUST periodically (default every 30s per OQ-PL-002) enumerate its live child processes (by iterating the watcher-goroutine registry per HC-011 OR by platform-specific child enumeration) and assert that each child's env/PGID matches the daemon's project-hash marker. A mismatch is a panic per PL-018a — this is a defensive consistency check, not an operational path.

---

## 14. PID-file discipline — PL-002, PL-024 (lines 149–161, 452–457)

### 14.1 Atomic write: not named

**Gap.** A classic pidfile bug: daemon-A writes its PID partially (first digit only) before crashing; daemon-B reads a truncated PID and probes a different, unrelated process. The safe pattern is:

1. `O_CREAT|O_RDWR|O_CLOEXEC` open.
2. `flock(LOCK_EX|LOCK_NB)`.
3. `ftruncate(fd, 0)`.
4. Write full PID (+ PGID per §4.4 proposal) with a SINGLE `write()` call (partial writes are the caller's problem; short writes must loop).
5. `fsync(fd)`.
6. Keep fd open.

An alternative is atomic-rename: write to `.harmonik/daemon.pid.tmp`, `fsync`, `rename` to `.harmonik/daemon.pid`. But rename destroys the flock — the new pidfile has a different inode and no lock. So rename is NOT safe for a lock+pidfile combo. The truncate-rewrite pattern is correct.

**Proposed text amendment to PL-002:**

> PL-002 amendment: the pidfile MUST be written via the truncate-rewrite pattern: open, flock, ftruncate(0), write complete PID (+ PGID per PL-006a), fsync, retain fd. Atomic-rename patterns (write-to-tmp + rename) are FORBIDDEN because rename breaks the flock association (the new file has a different inode).

### 14.2 Removal on clean shutdown

PL-011 step 8 says "Release the pidfile lock AND remove the pidfile on clean shutdown." **Good** — this is correct and rare (many daemons leave the pidfile on clean shutdown, forcing the next startup into the stale-detect path even when it's unnecessary).

One nit: if the daemon crashes between closing the fd (releasing lock) and `unlink`, the pidfile is stale with a dead PID. The PL-024 stale-detection path handles this. **Fine as-is.**

---

## 15. Watchdog / heartbeat

### 15.1 Nothing specified

**Major gap for production daemons.** A long-running daemon that deadlocks but does not panic is silent — the pidfile lock is held, the socket is bound, but no work advances. There are two standard mitigations:

1. **External watchdog.** systemd `Type=notify` with `WatchdogSec=30` causes systemd to SIGKILL the daemon if it doesn't call `sd_notify("WATCHDOG=1")` every 30s. Equivalent mechanisms exist for launchd (less elegant), s6, runit.

2. **Internal dead-man-switch.** A goroutine that increments a counter every N seconds; a separate goroutine that checks the counter and panics if it hasn't moved.

Neither is specified. PL's silence here is effectively "use nothing; the operator's supervisor handles it." That's fine for MVH but should be documented:

> Proposed new requirement PL-018b — Watchdog-heartbeat integration (informative at MVH).
>
> The daemon MAY implement a watchdog-heartbeat protocol for external supervisors:
>
> - When launched under systemd with `WatchdogSec` set (detectable via `$WATCHDOG_USEC` env var), the daemon MUST call `sd_notify("WATCHDOG=1")` at half the declared interval.
> - When launched without a supervisor, the daemon SHOULD install a dead-man-switch goroutine that panics (triggering PL-018a top-level recover and exit 12) if the main event loop does not tick within T_deadman seconds (default: 300, configurable). The dead-man-switch targets deadlock detection; it is NOT a liveness guarantee.
>
> This requirement is informative at MVH; production deployments will need at least one mechanism to detect silent-hang daemons. Tracked as OQ-PL-014.

---

## 16. EINTR / SA_RESTART

### 16.1 Go's default

Go's runtime installs signal handlers with `SA_RESTART` on nearly all syscalls. The practical effect: `read()`, `write()`, `accept()`, etc., automatically retry on EINTR. Go developers rarely see EINTR.

**Exceptions.**

- `time.Sleep()` is not a syscall; it's a runtime primitive and is cancellable via context, not EINTR.
- `select` on a channel is not a syscall.
- cgo calls that wrap syscalls directly may NOT be SA_RESTART'd unless the wrapper handles EINTR.
- `golang.org/x/sys/unix` raw syscalls (if the daemon uses them) do NOT auto-retry; the caller must loop.

**Proposed text:**

> Proposed new requirement PL-005a — EINTR handling.
>
> The daemon MUST NOT assume `SA_RESTART` semantics for any direct syscall (`golang.org/x/sys/unix.*`, `syscall.*`). Every such call MUST be wrapped in an EINTR-retry loop:
>
> ```go
> for {
>     err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
>     if err == unix.EINTR { continue }
>     break
> }
> ```
>
> The standard `os/exec.Cmd.Wait()` and `net/*` packages are SA_RESTART-safe by default; direct syscalls are not. A lint rule requiring EINTR-retry around raw syscalls is RECOMMENDED. Tracked as OQ-PL-015.

### 16.2 Signal masks in Go goroutines

Go runtime's signal handling is thread-global (process-level), delivered to any non-masked thread. Go code generally does not mask signals per-goroutine (Go abstracts threads). This is fine for SIGTERM/SIGINT handling via `signal.Notify`.

**Edge case.** If the daemon uses cgo and the C code blocks signals on its thread, `signal.Notify` may miss deliveries. Not a concern for an all-Go daemon; worth a note if handler launch uses cgo:

> INFORMATIVE: if the daemon or its vendored libraries use cgo, signal delivery via `signal.Notify` is reliable only on Go-managed threads. Pure-Go daemons (the MVH default) are unaffected.

---

## 17. Resource limits — `setrlimit`

### 17.1 Not specified

**Gap.** No requirement names `setrlimit`. A production daemon should at minimum:

- Check `RLIMIT_NOFILE` at startup; if below a threshold (e.g., 4096), either `setrlimit(RLIMIT_NOFILE, {soft: min(hard, 4096), hard: hard})` OR log a warning and refuse to start.
- Check `RLIMIT_NPROC` if the daemon spawns many subprocesses.
- Check `RLIMIT_CORE` — decide whether to allow core dumps (useful for post-mortem debugging; sensitive data risk).

**Proposed new requirement PL-005b — Resource-limit discipline.**

> PL-005b — On startup (step 0, before the event bus binds), the daemon MUST:
>
> (a) Check `RLIMIT_NOFILE` via `getrlimit`. If the soft limit is below `HARMONIK_MIN_NOFILE` (default 4096), the daemon MUST attempt `setrlimit` to raise the soft limit to `min(4096, hard_limit)`. On failure, the daemon MUST log a warning and continue with the current soft limit; a per-agent-subprocess launch that exceeds the limit will emit `daemon_startup_failed{failure_mode=nofile-exhausted}` or a runtime error.
>
> (b) Check `RLIMIT_NPROC`; if below 1024, log a warning.
>
> (c) Leave `RLIMIT_CORE` at the OS default (typically 0 on Linux, unlimited on macOS); operators who want core dumps MUST configure them via systemd/launchd/shell limits, NOT via the daemon.
>
> (d) Leave `RLIMIT_AS` (virtual memory) at the OS default; the daemon's memory footprint is bounded by event-bus buffers (event-model ON-029) and handler-count (PL-014a ceiling).
>
> Tracked as OQ-PL-016 (finalize default thresholds per operator feedback).

### 17.2 PL-014a (concurrency ceiling) interaction

**Concern.** PL-014a says the default concurrency ceiling is "unbounded (subject to OS limits)." If `RLIMIT_NOFILE=1024` (macOS default) and each handler subprocess opens ~4 fds (stdin, stdout, stderr, socket-conn), the daemon hits EMFILE at ~250 concurrent handlers. An unbounded ceiling default is a footgun.

**Proposed amendment to PL-014a:**

> PL-014a amendment: the default concurrency ceiling MUST be computed as `min(operator_config, floor(RLIMIT_NOFILE_soft / HARMONIK_FDS_PER_HANDLER))` where `HARMONIK_FDS_PER_HANDLER = 8` (conservative; actual average is ~4 but accounts for transient spikes). An unconfigured ceiling means "derived from OS limits," NOT "literally unbounded." Exceeding the derived ceiling is a hard error (exit with new exit code "ceiling-misconfigured") rather than silent EMFILE on spawn.

---

## 18. Minor gaps (briefly)

### 18.1 `os.Exit` in defer is wrong

Go trap: `defer func() { os.Exit(N) }()` does NOT run other deferreds. The daemon's main must either (a) return an exit code that a tiny `main()` wrapper converts to `os.Exit`, or (b) ensure all critical cleanup runs BEFORE the recover-defer's `os.Exit`. Worth a comment in PL-018a.

### 18.2 Working directory

The daemon's CWD is inherited from its launcher. If launched from `/`, any relative file path (unlikely but possible in dependencies) hits unexpected targets. Production daemons often `chdir` to a known safe directory (`$HOME` or the project root). Not specified; probably fine since PL-004 uses absolute paths under `.harmonik/`. Worth a one-line assertion:

> Proposed note: the daemon MUST resolve all file paths relative to `filepath.Abs(project_root)`; `os.Chdir` is NOT performed (to avoid interfering with operator-provided CWD for `harmonik runner` observability).

### 18.3 Umask

`umask(0077)` should be set at startup (step 0) so that every file the daemon creates (event log, spill files, pidfile, socket) is mode 0600 by default. Not specified. The socket-chmod-after-bind race (§3.6) is a special case of this.

> Proposed amendment to PL-005 step 0: the daemon MUST set `syscall.Umask(0077)` immediately on startup, so that all daemon-created files are mode `0600` (owner-only) by default. Files requiring broader permissions (none known) MUST explicitly chmod after creation.

### 18.4 Double-fork daemonization

**NOT required.** v0.3.0 correctly treats the daemon as a foreground process suitable for process-supervisor invocation (PL-028 `harmonik daemon` "Blocks until signaled to stop"). Double-fork daemonization (classic SysV) is anti-pattern for systemd/launchd-era supervisors. **The spec is correct to omit it.** Worth an informative note:

> Proposed informative note in §4.1: `harmonik daemon` is a foreground process. It does NOT daemonize via double-fork. Operators who want background execution SHOULD use a process supervisor (systemd, launchd, nohup, tmux) or the `&` shell operator; the daemon itself will not fork itself into the background.

### 18.5 Signal-safe emission

The panic barrier (PL-018a) emits events as part of exit. Emitting events from a signal handler is generally unsafe (signal handlers must be async-signal-safe; allocation, channel send, mutex lock are NOT signal-safe). Go's `os/signal.Notify` delivers signals on a channel (not in a handler), so the signal-safe constraint doesn't apply to Go daemons — the "handler" is a goroutine reading a channel. **Non-issue.** Worth a note:

> INFORMATIVE: all daemon signal handling is performed by ordinary goroutines reading `os/signal.Notify` channels. The async-signal-safe constraints of raw POSIX signal handlers do NOT apply; arbitrary code (allocation, event emission, mutex) is permitted in signal-delivery goroutines.

---

## 19. Summary of proposed additions (checklist)

For the foundation-author to action in v0.4.0:

1. **PL-002a(b)** — NFS/unsupported-filesystem detection; fd-hold-for-lifetime; EINTR/ENOLCK handling.
2. **PL-002 amendment** — atomic truncate-rewrite pattern; two-line pidfile (PID + PGID).
3. **PL-003 amendment** — umask before bind OR `0700` parent directory (avoid chmod-race).
4. **PL-003a amendment** — error-envelope conformance; batch behavior (reject with `-32600`); canonical-form line discipline; notifications forbidden from agents.
5. **PL-005 step 0 amendment** — umask(0077) and rlimit check.
6. **PL-005a (new)** — EINTR-retry discipline for direct syscalls.
7. **PL-005b (new)** — setrlimit / getrlimit checks with conservative defaults.
8. **PL-006 amendment** — process-enumeration mechanism (proc/sysctl), env+PGID dual-match, grandchild detection, SIGKILL-ignore handling.
9. **PL-006a amendment** — abspath canonicalization (symlinks + case-fold); PGID via setsid session-leader; pidfile records PGID; handler-setsid-breaks-sweep caveat.
10. **PL-008a amendment** — split code 11 into 11a/11b/11c; add code 13 "orphan-unreapable."
11. **PL-009 amendment** — monotonic clock for RTO; `ready_duration_ms` in payload.
12. **PL-009b (new)** — ready protocol for external callers (socket-probe, sd_notify, ready-file fallback).
13. **PL-011 amendment** — signal-delivery mechanism; double-SIGTERM escalation; drain step 3→4 handshake; step 8 internal ordering.
14. **PL-014 amendment** — explicit `cmd.Wait()` reap discipline per child.
15. **PL-014a amendment** — ceiling derived from `RLIMIT_NOFILE`, not literally unbounded.
16. **PL-014b (new)** — CLOEXEC discipline.
17. **PL-018a amendment** — explicit per-goroutine recover wrapping; escalation threshold; startup-vs-runtime emission rules.
18. **PL-018b (new)** — watchdog-heartbeat (systemd, dead-man-switch).
19. **PL-021a amendment** — version-probe mechanism (regex, stdout, exit-code); minimum version declaration; split exit codes.
20. **PL-027(iii) REWRITE** — fd-passing on exec via `FD_CLOEXEC`-clear + `HARMONIK_LISTENER_FD` env var; new binary adopts listener. Current text is self-contradictory.
21. **PL-INV-005 amendment** — runtime sensor via periodic child enumeration; add RATIONALE noting deliberate omission of `PR_SET_PDEATHSIG`.

Line-count-impact estimate for v0.4.0: ~120 additional lines across §4.1, §4.2, §4.4, §4.5, §4.9.

---

## 20. What v0.3.0 gets right (to avoid over-correcting)

- **PL-002a primitive selection.** fd-lifetime vs process-lifetime, `flock` on macOS, `F_OFD_SETLK`-option on Linux. Textbook-correct.
- **PL-006a provenance marker structure.** Env var + PGID dual-marker for cross-platform disambiguation is the right design; darwin-specific fallback correctly deferred as OQ.
- **PL-005 step 0 bootstrap.** Correctly orders the event-bus-before-lock path, letting step 2 emit a meaningful `daemon_started` event.
- **PL-011 durable-checkpoint stop-advancing.** The v0.3.0 rewrite of step 3 (away from "suspend" verb) matches execution-model reality: no new run state, just stop advancing.
- **PL-012 interceptable vs SIGKILL split.** Honest about what's recoverable.
- **PL-018 (the core PL-INV-002 sensor).** The `go-arch-lint` + binary-import-graph dual-sensor is the right granularity.
- **PL-020a AR-INV-007 compliance.** Correctly states that all registries live in the composition root; no out-of-daemon registries for MVH.
- **PL-027 daemon-internal vs operator-facing split.** The boundary with ON-020 is clean. (The content of (iii) is wrong per §9.1 above, but the boundary is right.)
- **PL-INV-004 socket-path exclusivity.** Pairs correctly with PL-INV-001 (one-daemon-per-project) and handles the race that PL-INV-001 alone doesn't cover.

---

## 21. Final recommendation

**Status recommendation:** `draft` → `draft` (do NOT advance to `reviewed` without addressing items 7 (PL-027 (iii) fd-passing), 9 (CLOEXEC), 12 (ready protocol), and 20 (rlimit default) from §19's checklist). The remaining items are tightening rather than correctness-blocking.

**Severity tally:**

- **Correctness blockers (must-fix before implementation):** PL-027(iii) rewrite (fd-passing), PL-014a ceiling default (EMFILE footgun), PL-009b ready protocol (external caller integration), PL-014 reap discipline (zombies).
- **Craft-tightening (should-fix before v1.0):** NFS detection, pidfile atomicity, umask, SIGTERM double-tap, EINTR discipline, ntm version-probe specifics, per-goroutine recover idiom.
- **Nice-to-have (v1.1+):** watchdog/heartbeat, runtime parentage sensor, split ntm exit codes, working-directory assertion.

The spec has the right shape. The daemon-craft surface needs another R2 pass focused on the mechanisms, not the delegation graph.
