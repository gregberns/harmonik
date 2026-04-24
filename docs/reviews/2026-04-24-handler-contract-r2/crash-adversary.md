# Crash-Recovery Adversary Review — handler-contract.md v0.2

**Reviewer role.** Crash-Recovery Adversary (round 2).
**Target.** `/Users/gb/github/harmonik/specs/handler-contract.md` v0.2 (853 lines, 53 requirements + sub-requirements, 5 invariants, 7 OQs).
**Lens.** Pressure the spec against subprocess death at every boundary: mid-output, mid-handshake, in the shutdown window, on a broken socket, under a wedged watcher, across daemon generations, and during adapter-mediated account rotation. Find requirements whose guarantees do not survive the subprocess going away at the wrong instant.
**Date.** 2026-04-24.

## Verdict summary

The spec handles the "happy crash" (subprocess exits non-zero, watcher observes, `agent_failed` fires) cleanly and its post-outcome shutdown window (HC-008a) closes one of v0.1's worst gaps. But six real crash-time gaps remain: (a) a subprocess death mid-`agent_output_chunk` has no defined durability point — the watcher may publish a truncated chunk and the spec is silent on whether that counts as observed output; (b) the shutdown-window kill path (HC-008a) races the watcher's own read-loop cleanup and the order is unspecified, allowing double-emission of terminal events; (c) the socket half-close or `EPIPE` mid-session is not classified — the spec names crash (exit non-zero) and silent-hang (no messages) but not "socket dead while subprocess alive"; (d) HC-INV-001 (exactly one watcher per session) has no stated enforcement path when the watcher goroutine itself panics or wedges on a syscall; (e) HC-004's "per daemon generation" idempotency scope plus OQ-HC-006's deferral leaves a concrete crash-recovery hole: orphaned subprocesses from the prior generation are owned by nobody at the moment reconciliation starts; (f) `Adapter.RotateAccount` (HC-013) is defined as a synchronous callback but its interaction with an in-flight progress stream, partial LLM turn, and the silent-hang state machine is unspecified. Fixes are local and additive; none reopen a locked decision.

## Scenarios tested

### Scenario 1 — Agent dies mid-`agent_output_chunk`

**Affected requirements.** HC-007 (progress-stream messages), HC-007a (NDJSON framing), HC-008 (outcome delivery), HC-024 (subprocess crash emits `agent_failed`), HC-INV-004 (ordering).

**Failure mode.** The handler subprocess is writing `agent_output_chunk` line 47 — 8 KiB into a 32 KiB JSON object — when it SIGSEGVs. The watcher's NDJSON reader has already consumed the trailing `\n` of line 46 and is now in a partial-read state on line 47: it has 8 KiB in the socket buffer, the peer has closed, and the decoder returns `io.ErrUnexpectedEOF` (or a JSON parse error on an unterminated object).

HC-007a specifies "no embedded unescaped newlines inside a JSON object" but says nothing about what the watcher does when EOF arrives mid-object. Three defensible behaviors exist and the spec picks none:

1. Treat the partial buffer as a structural protocol violation and publish `agent_failed` with class `ErrStructural`.
2. Discard the partial and publish `agent_failed` with class `ErrTransient` (the subprocess died; the surviving messages up to line 46 are the session's observed output).
3. Synthesize a redacted/truncated `agent_output_chunk` so downstream consumers see the partial bytes the agent actually produced.

The "last durable state" question is load-bearing for reconciliation: if a downstream consumer (e.g., a session-log archiver or an orchestrator-agent that reads chunks) saw chunks 1–46 but not 47, and the run is retried, does chunk 47's content replay? HC-004's idempotency is on `(run_id, node_id)` — it does not cover the output-stream replay dimension.

**Spec coverage.** Silent. HC-024 specifies emission of `agent_failed` on non-zero exit without `outcome_emitted`, but does not distinguish "exited cleanly mid-stream" from "socket EOF with partial message pending." HC-INV-004 orders `handler_capabilities → session_log_location → skills_provisioned → agent_ready → work dispatch` but has nothing to say about mid-session message-boundary durability.

**Recommendation.** Add `HC-007b — Message-boundary durability`: a progress-stream message is considered **observed** iff the watcher has successfully JSON-decoded it and published the corresponding bus event. Partial messages (socket EOF, decoder error, malformed JSON) MUST be discarded; the watcher MUST emit `agent_failed` with class `ErrTransient` (subprocess died) if the stream ended with bytes buffered, and `ErrStructural` with sub-reason `malformed_progress_message` if a syntactically invalid JSON object was received. Pin in §7 that decoder errors on a live socket do NOT restart the subprocess; they close the session.

Consequence for downstream consumers: `agent_output_chunk` is a best-effort stream — replay on retry is NOT guaranteed to reproduce identical chunks. Consumers that need exactly-once chunk delivery (e.g., a hypothetical session-log fork) must rely on the session-log file on disk, not on bus-observed chunks. This deserves explicit naming in §10.3 excluded conformance claims.

### Scenario 2 — Crash inside the post-outcome shutdown window

**Affected requirements.** HC-008 (outcome delivery), HC-008a (post-outcome shutdown window), HC-026 (silent-hang detection suspended during shutdown), HC-024 (crash emits `agent_failed`).

**Failure mode.** The subprocess emits `outcome_emitted`, the watcher acknowledges bus publication (shutdown window starts), and then the subprocess SIGABRTs 2s later (still within `T_shutdown = 10s`). The watcher observes non-zero exit.

What fires? HC-008 says "exit 0 = clean shutdown after `outcome_emitted`, non-zero = crash." Combined with HC-024 ("non-zero exit without preceding `outcome_emitted` MUST be classified as agent failure"), the logical negation is: non-zero exit WITH a preceding `outcome_emitted` is... not a failure? The spec does not say.

HC-008a specifies the happy path (clean exit within `T_shutdown`) and the deadline path (SIGKILL on expiry, `agent_failed` with `post_outcome_shutdown_timeout`). It does not specify the dirty-exit path: non-zero inside the window. Candidate behaviors:

1. Treat the outcome as durable (it was acknowledged to subscribers), emit `agent_completed` with a warning flag, ignore the non-zero exit.
2. Emit both `agent_completed` (outcome was observed) AND `agent_failed` (subprocess crashed) — breaks the implicit "one terminal event per session" assumption that downstream routing in execution-model §8 probably relies on.
3. Emit only `agent_failed` and discard the just-published `outcome_emitted` — but that's a bus event, it's already been observed by subscribers, you can't unpublish.

The race is worse: the shutdown-window cleanup path (watcher sends SIGKILL on timeout, HC-008a) and the normal-crash path (watcher detects non-zero exit, HC-024) can fire in either order if a crash lands at `T_shutdown - 10ms`.

**Spec coverage.** Gap. HC-008a addresses timeout only. HC-024's "crash without outcome" rule leaves the "crash with outcome" case undefined.

A parallel race: HC-008a says the shutdown window "begins when the watcher has acknowledged `outcome_emitted` to the subscribers (bus publication completed)." But the subprocess's own exit occurs on its own timeline. If the subprocess exits in `<1ms` after the `outcome_emitted` write flushes (common for agents that call `exit(0)` immediately after the final write), the watcher may see subprocess-exit *before* it completes bus-publication of `outcome_emitted`. The window then begins and ends in the same instant, with terminal-event ordering subject to goroutine scheduling.

**Recommendation.** Amend HC-008a with an explicit dirty-exit clause: "If the subprocess exits non-zero during the shutdown window after `outcome_emitted` has been published to the bus, the watcher MUST NOT emit `agent_failed`; the outcome is durable. The watcher MUST emit `agent_completed` with an additional payload field `shutdown_exit_code` carrying the non-zero exit status for operator observability. Exactly one terminal event per session is invariant." Cross-link HC-INV-004 with a new sibling `HC-INV-006 — Exactly one terminal event per session` (chosen from the set `{agent_completed, agent_failed}`; `budget_exhausted` is pre-launch and does not count).

Additionally, pin watcher shutdown ordering: the watcher MUST complete bus-publication of a received terminal message before observing subprocess exit status. If `Wait()` returns while a terminal message is still pending publication, the watcher MUST publish it before emitting the exit-derived terminal event. This collapses the race into a total order.

### Scenario 3 — Socket breaks mid-session, subprocess still alive

**Affected requirements.** HC-007 (Unix domain socket as sole channel), HC-044 (socket authenticity), HC-026 (silent-hang — absent messages), HC-011 (watcher owns read-loop and cleanup).

**Failure mode.** The Unix domain socket at `.harmonik/daemon.sock` is held open by both sides. An operator runs `rm .harmonik/daemon.sock` (or the FD-backed kernel object closes due to a resource limit, or the filesystem unmounts, or a namespace change severs the bind). The daemon's watcher gets `EPIPE` on its next read. The subprocess — which was merely a consumer of the socket, not the bind-holder — is still alive and still attempting to write progress messages.

Three distinct states now exist: (i) socket dead (watcher knows), (ii) subprocess alive (watcher does not know for sure — `Wait()` has not returned), (iii) no progress messages arriving (looks identical to silent-hang).

HC-026 keys silent-hang off "absent messages," which is satisfied here, so the silent-hang state machine will eventually fire and soft-terminate the subprocess. But the spec does not distinguish "socket broken, subprocess presumed alive" from "subprocess hung, socket intact." The consequences differ:

- If the spec treated socket break as a distinct error class, the watcher could skip the 10-minute silent-hang timer and go straight to subprocess termination.
- If the subprocess is doing real work but cannot report it, its side effects on the worktree are happening unobserved; on termination, reconciliation sees a partially-modified worktree with no corresponding progress events.

HC-007 says the progress stream is the *sole* bidirectional channel, implying socket loss = session loss, but never states that explicitly as a failure mode.

A further subtlety: HC-044 says socket authenticity is "filesystem-permission-based for MVH." If an operator `rm`s the socket, the *existing* socket FD pair keeps working (Unix semantics: unlink removes the name, not the connection). But a *handler restart or reconnect attempt* cannot bind to the removed path; the session cannot reattach even if both ends are healthy.

**Spec coverage.** Silent on socket-level errors distinct from subprocess-level errors. HC-INV-001 ("exactly one watcher per active session") is silent on what happens when the watcher's read-loop errors out but the session is still considered "active" by the daemon's session table.

**Recommendation.** Add `HC-024a — Socket-level I/O error terminates the session`: any error other than clean EOF from the progress-stream read-loop (EPIPE, ECONNRESET, socket unlink under foot, watcher decoder error per HC-007b) MUST cause the watcher to (a) emit `agent_failed` with class `ErrStructural`, sub-reason `progress_stream_broken`; (b) send SIGKILL to the subprocess immediately (the subprocess has no other channel to the daemon and any work product it continues to produce is unobservable); (c) mark the session terminated. Reconnect is NOT permitted at MVH — sessions are single-socket-lifetime (cross-link to HC-007's "sole bidirectional channel"). Add a matching test obligation in §10.2.

### Scenario 4 — Watcher goroutine wedged or panicked; subprocess alive and emitting

**Affected requirements.** HC-011 (daemon owns exactly one watcher per session), HC-INV-001 (exactly one watcher per active session), HC-014 (channel closure), HC-026 (silent-hang is keyed off progress messages).

**Failure mode.** The watcher goroutine is blocked in a syscall that won't return — the event-bus publication channel is full and a downstream subscriber is deadlocked, or a redaction-middleware panic was recovered but left the watcher in a wedged state, or a subtle `chan` misuse holds the watcher off the read-loop. The subprocess is emitting heartbeats every 300s, writes are buffering in the socket until the socket's send buffer fills and the subprocess blocks on write.

From the *subprocess's* perspective, it is healthy and ticking. From the *daemon's session-table* perspective, the session is active and the watcher exists. From the *silent-hang state machine's* perspective, no progress messages are arriving (because the watcher is not consuming them), so the silent-hang timer will fire and kill the subprocess — a false-positive silent-hang attributed to the agent.

HC-026a tries to prevent false-positive silent-hang by requiring handler heartbeats, but the heartbeat signal travels *through* the watcher; if the watcher is the stuck component, heartbeats cannot save it. HC-INV-001 is stated as an observable invariant ("more than one watcher per session is a daemon defect; zero watchers with an active session is a daemon defect") but the spec gives no liveness check for *wedged* watchers.

A concrete causal chain that produces this wedge: (1) a downstream subscriber to `agent_output_chunk` has a bug that panics and never returns, (2) HC-027's dead-letter route fires after some timeout, (3) but the watcher's *write* to its publish channel is still blocked during the interval, (4) during which the subprocess is producing chunks that back up in the socket buffer, (5) until the subprocess's `write()` blocks on a full socket, (6) and heartbeat emission is therefore blocked at the subprocess, (7) so the subprocess *will* miss its `T/2` heartbeat cadence — at which point silent-hang (HC-026) fires and kills the subprocess, attributing the failure to the agent rather than to the wedged subscriber.

The causal chain compounds: the originally-buggy subscriber may have been downstream of `agent_output_chunk` specifically, but the backpressure wedges the watcher which wedges the heartbeat which triggers silent-hang which terminates the subprocess. The agent is blamed for a subscriber bug.

**Spec coverage.** Gap. HC-011 mandates exactly one watcher goroutine per session; HC-027 routes undeliverable bus events to a dead-letter destination; but neither addresses a watcher that is alive-but-not-draining. HC-015's mutex discipline ("event publication MUST NOT block the state lock") reduces the blast radius but does not eliminate the wedge. HC-INV-003 (no secret in event log) depends on the redaction middleware completing on every event — a wedged subscriber downstream of redaction does not threaten the invariant, but a panicked redaction middleware recovering into a wedged state does.

**Recommendation.** Add `HC-011a — Watcher liveness probe`: the daemon MUST maintain a per-watcher `last_read_event_at` timestamp, updated on every successful `read()` return from the progress socket (distinct from `last_progress_event_at` which is updated on successful message decode). A daemon-level supervisor MUST check, at cadence ≤ `T/4`, that every active watcher has advanced `last_read_event_at` within `T/2`; a wedged watcher (no read in `T/2` despite the subprocess writing heartbeats at `T/2` cadence) MUST be classified as a **daemon defect**, logged, and the session terminated with `agent_failed` class `ErrStructural`, sub-reason `watcher_wedged`. This distinguishes watcher failure from agent silent-hang in the event record, which matters for post-mortem and for reconciliation's rule table.

Secondarily: name `recover()` obligation on the watcher goroutine body — panics MUST be converted to `agent_failed` class `ErrStructural`, sub-reason `watcher_panic`, not bring down the daemon. Same obligation on subscriber goroutines: a subscriber panic MUST NOT block the bus-publication pipeline, and MUST be isolated per-subscriber so that one subscriber's misbehavior does not wedge the watcher.

Tertiarily: the publish channel's buffering is under-specified. HC-014 names channel closure but not capacity. A 0-capacity (unbuffered) channel makes the wedge scenario above deterministic on any subscriber slowdown; a large buffer masks the problem until it spills. Recommend pinning a small bounded buffer (8-16 events) with explicit dead-letter routing on buffer-full per HC-027.

### Scenario 5 — Daemon restart discovers orphaned session artifacts

**Affected requirements.** HC-004 (idempotency is per daemon generation), HC-INV-001 (one watcher per active session), HC-044 (subprocess is child of daemon), OQ-HC-006 (cross-generation GC deferral).

**Failure mode.** The daemon process dies (SIGKILL from the operator, OOM, a supervisor restart). Its handler subprocesses were direct children (HC-044). Depending on the host platform and subprocess behavior:

- Linux with `PR_SET_PDEATHSIG` not configured: subprocesses are reparented to PID 1 (`init`/`systemd`/`launchd`) and continue running, still holding worktree file handles, still writing to session-log files, still connected (or disconnected) from the vanished socket.
- macOS (the declared platform): no equivalent to `PR_SET_PDEATHSIG`. Subprocesses *always* survive parent death. They become orphans owned by `launchd`.

The new daemon generation starts, reads its session table from... where? HC-004 pins idempotency to a single daemon generation and states that re-launch after daemon restart is a new launch. But the orphan subprocesses are still alive, still holding the old session's worktree (possibly still writing to files), and a reconciliation-driven re-launch (HC-004) will spawn a *second* subprocess that may now race the orphan for the worktree.

HC-INV-005 ("no launch without verified binary path") passes — the new launch is a legitimate new subprocess. But no invariant forbids *two concurrent subprocesses for the same `(run_id, node_id)`* across generations. HC-INV-001 ("exactly one watcher per active session") applies within a generation; it says nothing about zombie subprocesses with no watcher in the new generation.

OQ-HC-006 acknowledges this exact gap and defers it to reconciliation's startup sweep. But reconciliation's startup sweep has not landed; meanwhile, the handler-contract spec does not specify what `Launch` does when the worktree is held open by a process the daemon does not own.

**Spec coverage.** Explicitly deferred via OQ-HC-006. The deferral is reasonable, but the spec should at minimum name the *current-daemon-generation* obligation, which is stronger than what HC-INV-005 gives us.

**Recommendation.** Promote OQ-HC-006 to an MVH requirement with minimal surface: add `HC-044a — Launch MUST fail-fast on orphan-held workspace`. Before `Launch` returns a Session, the daemon MUST probe the target `workspace_path` for any open file handles it does not own (on macOS: `lsof +D`; on Linux: `/proc/*/cwd` and `/proc/*/fd/*` scan). If any non-daemon-owned process holds files in the workspace, `Launch` MUST return `ErrStructural` with sub-reason `workspace_held_by_orphan` and emit `agent_failed` carrying the offending PID for operator attention. This is a fail-fast stub that reconciliation's eventual startup sweep can replace; it prevents the concurrent-subprocess-on-worktree corruption mode.

Secondarily: HC-044 should state that handler subprocesses MUST install a parent-death signal handler on Linux (`PR_SET_PDEATHSIG(SIGTERM)`) and document the macOS limitation. This does not fix orphan-survival on macOS but at least limits the Linux blast radius.

Tertiary concern: the socket file at `.harmonik/daemon.sock` from the prior generation persists in the filesystem. A new daemon generation must `unlink` before `bind` to claim the path, and an orphan subprocess may still be holding the old socket's peer FD. An orphan that writes to its open socket after daemon restart writes into a kernel buffer that nobody reads; eventually the subprocess blocks on `write()`. Combined with HC-044a's lsof probe, this produces a diagnosable state ("PID N is blocked on write to a severed socket") rather than silent wedge.

### Scenario 6 — Account rotation mid-session with in-flight LLM turn

**Affected requirements.** HC-013 (`Adapter.RotateAccount` on the adapter surface), HC-025 (rate-limit events), HC-026a (heartbeat obligation), HC-INV-004 (ordering invariants).

**Failure mode.** The handler subprocess is mid-LLM-turn (a 3-minute extended-thinking call to Anthropic). The orchestrator-agent policy triggers account rotation — the current provider account hit a quota cap. The adapter's `RotateAccount(ctx)` is invoked synchronously per HC-013.

What happens to:

- The in-flight LLM API call? The subprocess holds an open HTTPS connection on the old account's API key. Does `RotateAccount` abort it (connection reset, partial response lost), drain it (wait for completion, which might be another 3 minutes), or leave it hanging (new account serves future calls, old call races to complete)?
- The heartbeat stream? Heartbeats must continue at `T/2` per HC-026a. During rotation, is the subprocess expected to keep emitting heartbeats? If the adapter's `RotateAccount` is blocking the subprocess's event loop, heartbeats pause and silent-hang fires.
- The secrets environment? HC-028 delivers secrets via `HARMONIK_SECRET_*` env vars set at spawn time. The env is not re-mutable from the daemon side — rotation would have to deliver the new secret *via a progress-stream message*, but HC-007's message type list does not include one.
- Partial work? If the rotation interrupts a tool-call-result write to the worktree, the worktree state is mid-transaction.

HC-INV-004 orders startup events but has nothing on mid-session state transitions. OQ-HC-002 acknowledges that the rotation surface may need extending ("rotated to account X of N remaining") but not that rotation semantics are under-specified.

**Spec coverage.** Thin. HC-013 defines the callback signature. HC-INV-004 is silent. §7 has no state machine for "session is rotating." Secrets spec (§4.7) declares secret rotation out of scope via the front-matter and §2.2, but conflates *secret rotation* (a new API key for the same account) with *account rotation* (a different account). These are different operations; only one is declared out of scope.

The "subprocess crashes during rotation" sub-case is doubly hazardous: if `RotateAccount` is mid-execution and the subprocess dies (say, the adapter rotation writes a stdin command and the subprocess SIGSEGVs on receipt), the watcher observes non-zero exit and fires `agent_failed`, but the adapter's `RotateAccount` return value may arrive *after* `agent_failed` has been published. Does `RotateAccount` return `ErrCanceled`? `ErrTransient`? Something session-lifecycle specific? The adapter surface does not say.

**Recommendation.** Resolve OQ-HC-002 with a scoped "account rotation is pre-turn only" rule: add `HC-013a — RotateAccount suspends work, does not interrupt it`. The adapter's `RotateAccount(ctx)` MUST NOT be invoked while the subprocess has an in-flight LLM turn. The watcher observes turn boundaries via `agent_output_chunk` stream quiescence; `RotateAccount` is scheduled at a clean boundary. If the watcher detects no quiescent boundary within a configurable window, rotation fails with `ErrTransient` and the orchestrator retries after the current turn completes. During rotation, the subprocess MUST continue emitting `agent_heartbeat` with `phase = "rotating"` (a new phase value added to the HC-026a enumeration) so silent-hang does not false-positive.

Secondarily: clarify in §4.7 that MVH out-of-scope is *provider-secret rotation* (new API key value for the same provider); account rotation (different pool member) is in scope and uses the HC-013 callback only at clean boundaries.

## Secondary observations

**HC-018's 500ms cancellation bound is adversarially tight.** HC-018 requires `ctx` cancellation to produce Go-side return within 500ms and subprocess cleanup within 5s. Under a NUMA-busy host or a paused Docker container (common CI scenario), a goroutine's scheduling latency alone can exceed 500ms. The bound is achievable for a healthy watcher but not under the crash-adjacent conditions this review targets. Recommend softening to "SHOULD complete within 500ms; MUST complete within 2s" or adding an explicit best-effort carve-out for pathological host conditions.

**HC-048a's retry budget vs. SIGKILL on daemon shutdown.** HC-048a specifies 4 provisioning attempts with exponential backoff (base 1s, cap 16s) bounded by `provisioning_timeout = 60s`. If the daemon receives SIGTERM during attempt 3 (at the 5s sleep), what happens? HC-017 says every public method takes `ctx`; provisioning presumably honors cancellation. But §7.2's handshake pseudocode does not thread `ctx` into the retry loop, and the spec's Scenario-3 cancellation rule ("cancellation supersedes silent-hang") does not obviously extend to provisioning retry. Daemon shutdown with a provisioning-retry-in-flight may hold the shutdown window open for up to 16s. Recommend explicitly naming `ctx` propagation through HC-048a's retry loop.

**HC-INV-004's ordering is asserted at publication, not at subscription.** HC-INV-004 says the watcher "MUST NOT publish `agent_ready` to subscribers before it has delivered `handler_capabilities`, `session_log_location`, and `skills_provisioned` to subscribers in that order." This is producer-side ordering. A multi-subscriber bus can still deliver out-of-order to any given subscriber if the bus is per-subscriber-channel with independent draining (which HC-015's mutex discipline implies). The invariant is met at publish time and violated at observe time. Recommend clarifying: "publication ordering is strict per-subscriber" — which event-model.md §3.7 likely already guarantees but this spec does not say so.

**HC-INV-005's "verified binary path" is vulnerable to TOCTOU.** The spec mandates a commit-hash check "before launch" (HC-043) and a verified launch path (HC-INV-005). But between the check and the `exec()`, a malicious or buggy actor could replace the binary at that path. This is a textbook time-of-check/time-of-use race. On macOS, `O_CLOEXEC` + `fexecve`-equivalent is platform-specific. Post-MVH signing (per §10.1) resolves this structurally; for MVH the gap is worth naming in an OQ.

## Summary of proposed additions

Six additive requirements, none reopening a locked decision:

1. **HC-007b** (Scenario 1) — Message-boundary durability; partial messages are discarded; classify EOF-with-buffered-bytes as `ErrTransient`.
2. **HC-008a dirty-exit clause + HC-INV-006** (Scenario 2) — Non-zero exit inside shutdown window after published outcome is `agent_completed` with `shutdown_exit_code`; exactly one terminal event per session.
3. **HC-024a** (Scenario 3) — Socket-level I/O error is a distinct failure from subprocess crash; immediate SIGKILL, no reconnect at MVH.
4. **HC-011a** (Scenario 4) — Watcher liveness probe distinguishes wedged watcher from silent-hang agent; mandatory `recover()` on watcher body.
5. **HC-044a** (Scenario 5) — Launch fail-fast if workspace held by orphan; Linux `PR_SET_PDEATHSIG`; documents macOS limitation as OQ deferral.
6. **HC-013a** (Scenario 6) — `RotateAccount` is pre-turn only; new `rotating` phase in HC-026a enumeration; split provider-secret-rotation (out of scope) from account-rotation (in scope at boundaries).

All six can be delivered as local edits inside v0.3 without structural changes. The v0.2 architecture is sound; these patches close the residual subprocess-death surface.

## Scope notes and non-findings

A few scenarios from the prompt produced weaker findings and are deliberately not turned into requirements:

**Skill provisioning succeeds then agent crashes; rollback provisioned skills?** The adapter's view is that provisioned skills are installed into the agent-process shape (file drops in the worktree, CLI binaries on `$PATH`, MCP registrations). On crash, the subprocess is gone; its process-level state (registrations, env-derived paths) evaporates with it. File-drop skills land inside the run's worktree per HC-046/workspace-model.md §5.3a and are governed by the worktree's lifecycle — cleaned up on worktree reset, not by a handler-side rollback. Rollback is therefore a non-problem by construction. The only edge case is a skill that mutates global state (installs a system service, writes to `~/.config`), which is out-of-MVH-scope per §4.10's MVH sandbox posture and deserves only a sentence in §2.2.

**SIGKILL → SIGTERM ordering during daemon crash.** Handler-side signal discipline is bounded by what the subprocess sees. If the daemon is SIGKILLed, handler subprocesses get no signal from the daemon at all (they get orphaned, per Scenario 5). If the daemon is SIGTERMed, the daemon's own shutdown path (process-lifecycle.md §8) is the actor, not the handler contract. The handler contract's only handler-side signal obligation is honoring `ctx` cancellation (HC-018); signals are the daemon's concern. No finding here beyond the orphan-workspace recommendation of Scenario 5.

These are noted for completeness; the six recommendations above are the actionable surface.

## Priority ordering for v0.3 integration

Of the six proposed additions, the crash-severity ordering (worst-if-ignored first) is:

1. **HC-044a** (orphan workspace) — data-corruption mode; two subprocesses writing to one worktree is the only scenario here that can silently corrupt a run's committed artifacts.
2. **HC-011a** (watcher liveness) — attribution-correctness mode; misattributes subscriber bugs to agent silent-hang, corroding the reconciliation rule table's utility.
3. **HC-024a** (socket-level failure) — observability mode; sessions can look silent-hung when they're actually socket-dead, extending detection latency by up to 600s.
4. **HC-008a dirty-exit clause + HC-INV-006** — routing-correctness mode; double terminal events break downstream routing assumptions.
5. **HC-013a** (rotation at turn boundaries) — feature-correctness mode; only bites handlers that support rotation, but bites them predictably.
6. **HC-007b** (message-boundary durability) — clarification rather than correction; existing implementations will likely do the right thing, but the spec should say so.

v0.3 should land 1-3 at minimum; 4-6 can ride a subsequent revision if scope pressure demands.
