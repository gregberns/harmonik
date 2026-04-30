# Crash-Recovery Adversary Review — operator-nfr.md v0.3.0

**Reviewer role.** Crash-Recovery Adversary (round 2).
**Target.** `/Users/gb/github/harmonik/specs/operator-nfr.md` v0.3.0 (873 lines; 49 numbered ON-NNN + 3 live ON-INV, 2 retired invariants, 1 envelope ON-ENV-001, 7 OQs).
**Lens.** Pressure the v0.3.0 integrated spec against every physical-reality boundary: SIGKILL, power loss, partial fsync, torn writes, kernel panics, concurrent operator commands, signal races, RTO-measurement regressions, drain failures, redactor panics, multi-daemon coordination races, secrets-redaction under abort, structured-log durability gaps, audit-record tail truncation, and the queue-format N-1 migration window under in-flight upgrade.
**Date.** 2026-04-24.
**Status.** R2 integration input. ON v0.3.0 currently `status: draft`; R2 transitions to `reviewed` on acceptance.

---

## 1. Verdict summary

ON v0.3.0 got a lot right. The paired-phase pause rewrite (I3), the `in_flight(run)` mechanical predicate in §3, the drain-completion gate on the `pausing → paused` transition (ON-008 + §7.1), and the exit-code expansion to codes 19–21 are all well-targeted; they close real holes in v0.2 and they are already consistent with PL v0.4.0's panic barrier (PL-018a) and stop-advancing predicate (PL-011). The commit-hash integrity gate (ON-005) is mechanically right and the §7.3 upgrade pseudocode is close to directly compilable given PL-027's fd-passing.

But ON is the home of cross-cutting guarantees that every sibling spec imports, and several of those guarantees do not survive the kernel boundary as stated. Concretely:

1. **ON-027's seven-step drain is declared ordered but has no recovery rule for a crash *between* steps.** PL-024 covers "daemon crashed; pidfile stale," but ON never says which of the seven steps are atomic, what happens when `stop --graceful` is interrupted at step 4 by SIGKILL, or what the next restart's reconciliation sees when step 6 (workspace unlock) completed but step 4 (event-bus fsync) was lost. The drain is presented as a linear pseudocode with no crash-boundary semantics.

2. **The operator-control state machine (§7.1) is declared in-memory (§4.a(e): "State owned — none persistently") but §4.3.ON-008 names `paused` as a real observable state that operators reason about and issue commands against.** After a crash in `paused`, the next restart's startup sequence has NO rule for re-entering `paused`; PL-005 transitions `starting → reconciling → ready → running` and there is no path to `paused` without an operator re-issuing `pause`. This means a crash in the `upgrading` or `paused` state silently loses operator intent.

3. **ON-031's RTO measurement is specified as "SIGTERM (or crash) to `daemon_ready`" (ON-033) — a subtraction of two wall-clock timestamps across a process boundary.** The spec never says whether the subtraction is monotonic (it can't be — different processes), never says what happens when the wall clock regressed between SIGTERM and the new daemon's `ready` (possible under VM-pause, NTP fix, laptop sleep), and never provides an external witness analogous to EV-002c's `event_id_hwm`. ON-031's "sensor" is declared as a test harness, which is fine for the p95 but useless at runtime: the daemon itself cannot compute its own SIGTERM timestamp because it is dead at the moment SIGTERM was delivered.

4. **ON-022/ON-023 redaction is required "pre-emission," but the redactor itself is Go code and can panic.** A panic inside the redaction path lands in PL-018a's top-level barrier, but ON-022 asserts redaction is "mechanism-tagged and MUST be enforced pre-emission" — if the redactor panics, the *only* safe interpretation is to drop the event, but ON never says this. A naive implementer could emit-unredacted-on-panic, exactly the failure mode ON-INV-003 exists to prevent.

5. **ON-016 queue-schema-version check happens "on daemon startup" but ON-018 N-1 compat is a migration boundary that is crossed by `harmonik upgrade` while the daemon is live.** Between PL-027(i) exec-replace and PL-027(iv) re-bind, the new binary's schema-version check (ON-016) runs against on-disk state that MAY include intent-log files (BI-030) written by the *old* binary under the *old* Beads overlay schema. ON does not say what the new binary does with a pending intent file whose idempotency key encodes an old schema. The spec handwaves at "cross-version state compat" in ON-020(d) but never makes the intent-log case normative.

6. **ON-041 machine-level agent-subprocess ceiling is declared (with OQ-ON-003 naming the implementation locus as unresolved) but no crash recovery is specified for the shared lock / counter.** If two daemons race to acquire the machine-ceiling counter and daemon A crashes mid-acquire, the counter's state on disk may be inconsistent (decremented by a dying process that never committed the decrement, or incremented by a process that then crashed before spawning). The default-if-unresolved resolution ("filesystem-based shared-counter lock") has every known anti-pattern of filesystem-based counters (no atomicity across `read-modify-write`, no fsync discipline, no fencing on lock holder death). ON inherits this hazard into MVH and does not yet own it.

7. **OQ-ON-004 (concurrent operator attach) punts on serializing concurrent pause/upgrade — but the default-if-unresolved ("second command observes the state-machine in the post-first-command state") assumes the two operator-command-dispatch goroutines execute atomically.** In reality, `pause` from terminal A and `upgrade <hash>` from terminal B can both observe `daemon.state == "running"` simultaneously; terminal A transitions `running → pausing`, terminal B (which was concurrent) transitions `running → upgrading` (invalid from `running`! `upgrade` requires `paused`). §7.1's state-machine table would emit `operator_command_rejected` for terminal B if atomicity holds, but there's nothing in the spec to enforce atomicity.

8. **Audit records (ON-038) are declared as a subset of transition records, i.e., git-tree-committed sibling files. Good. But ON-039 declares audit-record *derivation* mechanism-tagged and ON-022 says secret-redaction applies. A crash mid-audit-derivation — between the transition landing in git and the audit projection being computed — leaves the git record durable but the audit trail gapped.** ON doesn't say how an investigator agent knows the audit projection is incomplete, and there's no sensor for audit-record coverage against the transition-record set.

9. **The structured-log channel (ON-035) is declared ON-owned with a minimum wire format, but its durability class is not named.** EV-019 says panic-handler flushes the structured-log channel "before exit," EV-019 implies aggressive fsync, but ON-035 says only "newline-delimited JSON" and defers the detailed schema to OQ-ON-007. Exit code 19 (`runtime-panic` per §8) points the operator to "structured-log records around the panic timestamp" — but if the structured-log writer is not fsync-per-write (the spec does not require it), a panic 10 ms before the next flush loses the diagnostic evidence the operator is pointed at.

10. **ON-048 exhaustion-protocol step 4 ("Emit `dispatch_deferred`… if the exhaustion cascades to a multi-run ceiling breach") is declared but the emission-or-not is mechanically underspecified at the crash boundary.** A crash mid-cascade leaves the machine-counter in an unknown state AND the `dispatch_deferred` event possibly not emitted. Recovery semantics are not named.

11. **ON-047 category defaults table is operator-configurable per the config inventory (ON-004). But ON-004 is obligation-satisfied by the bootstrap allowance in §10.1 — the config inventory doesn't fully exist yet.** Meanwhile, ON-047 says defaults "exist to make 'no policy declared' a safe state"; a crash mid-config-load (e.g., disk read failing partway through parsing the policy file) can leave the in-memory budget map in a silently-partial state that falls back to DIFFERENT defaults than the operator's file declares. There is no "config load is transactional" requirement.

12. **Exit code 20 (signal-terminated) is named in §8 as "no clean emission path," which is correct for SIGKILL. But this means every crash via SIGKILL or SIGBUS produces a `daemon_exit(code=20)` that the daemon itself never emits — i.e., code 20 is synthesized by `harmonik list` or an external supervisor reading the pidfile.** ON never says who assigns code 20. PL-025a's pairing-tolerance rule is the consumer-side answer but ON doesn't name the RTO impact: if SIGKILL produces no emission, RTO measurement (ON-031) has no SIGTERM timestamp to start from.

13. **ON-INV-005 (reconstruction-contribution interface) is declared; its sensor is a test harness.** But ON does not declare what happens mid-reconstruction when ONE subsystem's reconstruct call panics or times out. Does the daemon exit `degraded`? Does it skip that subsystem? PL-018a covers *the* top-level panic, but a panic inside the reconstruction of one subsystem (say, workspace manager) during the `reconciling` prefix is not covered: does it escalate to the top-level barrier (terminating the whole daemon) or does it emit `daemon_degraded` and carry on?

No finding reopens a locked decision. Three (#1 drain-atomicity, #2 persist-pause-state, #7 concurrent-operator-serialization) are small-design-tweaks. Six are single-or-two-sentence normative additions. The remaining four are cross-spec coordination items that must land in ON's R2 integration AND be echoed in sibling spec next-revision cycles (PL v0.4.1, BI v0.3.1, EV v0.3.1).

---

## 2. Scenarios probed

Thirteen scenarios, covering the suggested list plus two additions surfaced while tracing the spec.

### Scenario 1 — SIGKILL during ON-027 drain mid-step

**Affected requirements.** ON-008 (§4.3, pause drain gate), ON-027 (§4.7 seven-step drain), ON-028 (stop --immediate skip path), ON-029 (drain timeout), §7.2 `drain_graceful` pseudocode, §8 exit codes 0 / 11 / 20 / 21.

**What the spec says.** ON-027 declares steps 1–7 "each step completing before the next begins." Step 4 is event-bus fsync (EV §4.4). Step 6 is workspace lease unlock. Step 7 is process exit with code 0. The sequence is single-path; no "what if I am SIGKILL'd at step 5" recovery rule.

**What actually happens.** Consider SIGKILL at three points:

- **SIGKILL between steps 1 (stop_dispatch) and 2 (runs checkpoint).** Dispatch stopped in-memory only; no persistent marker. Next restart looks identical to a clean crash from `ready`: reconciliation reclassifies in-flight runs. Safe.
- **SIGKILL between steps 4 (bus flush) and 6 (workspace unlock).** Event bus fsynced — `operator_pause_status{status=pausing}` durable if emitted. Workspace lease locks still held (step 6 did not run). PL-006 orphan sweep on next restart removes the stale lease locks (WM-013b's crash-tolerant unlink-on-observation). Reconciliation classifies in-flight runs. But: the daemon was drain-completed up to step 4 — should the next restart come up in `paused`? It does NOT, per Scenario 2. So the operator's pause intent is lost across the crash even though the drain got far enough to be meaningful.
- **SIGKILL between steps 6 (workspace unlock) and 7 (exit).** All durable side effects landed; only process exit was pre-empted. Next restart is indistinguishable from a clean shutdown except the pidfile is stale (PL-024) and the exit code is 20 (signal-terminated) rather than 0. RTO measurement miscomputes because there is no SIGTERM timestamp (the draining daemon got SIGKILL'd while drain was succeeding; it never emitted `daemon_shutdown`).

**Safe / partially safe / unsafe / unbounded.** **Partially safe.** State recovery works because git + Beads are authoritative. But: (a) operator's pause intent is silently lost in the mid-drain-SIGKILL case; (b) exit code 20 means no witness of what the drain accomplished; (c) no ordering or atomicity claim is stated between ON-027 steps. A future implementer who interleaves step 4 (fsync) and step 6 (unlock) for performance would be violating an invariant ON never declared.

**Concrete spec-text proposal.** Add **ON-027a — Drain-step atomicity and crash semantics**:

> ON-027's seven steps MUST be executed strictly sequentially with no parallelism. Each step MUST complete (as defined by that step's owning subsystem's durability contract) before the next begins. A crash (SIGKILL, SIGSEGV, OOM, kernel panic, power loss) between any two steps MUST leave the system in a state reconstructible by the next restart via PL-006 orphan sweep plus reconciliation per [reconciliation/spec.md §4.2]. No ON-027 step MAY create a durable side effect that cannot be safely observed by the next restart's reconciliation. The operator-control state (if `draining` as part of pause/upgrade) MUST NOT be assumed persistent across the crash; see ON-030a for the pause-state-persistence rule.

### Scenario 2 — Power loss during `pause → paused` transition; FSM state recovery

**Affected requirements.** ON-008 (pause drain gate), ON-011 (§4.3 state machine declared), ON-013 (paired-phase pause status event), §7.1 FSM, §4.a(e) ("State owned — none persistently"), ON-030 (restart reconstruction path).

**What the spec says.** §4.a(e): "The operator-control state-machine state (§7.1) is daemon-in-memory; reconstruction on restart is per §4.8.ON-030 via git + Beads." ON-030 says state reconstruction walks git checkpoint trails + queries Beads. Nothing in git or Beads records "the operator issued pause and drained successfully."

**What actually happens.** Operator issues `pause`. Daemon transitions `running → pausing`, emits `operator_pause_status{status=pausing}` (durability class is not declared in §6.5 or §4.3.ON-013 — assumed F by convention of "operator-observable," but not mandated by ON). Drain executes; all seven ON-027 steps complete. Daemon transitions `pausing → paused`, emits `operator_pause_status{status=paused}`. Power loss. On restart:

- git: no new commits since the last checkpoint (drain did not produce a commit).
- Beads: no terminal transitions for any run since the pause (pause didn't close any beads).
- JSONL: possibly durable if the status events were class F and fsynced.

The restart sequence (PL-005, PL-009) transitions `starting → reconciling → ready → running`. There is NO path to `paused` in §7.1 except via operator `pause` — not via startup. Unless the operator re-issues `pause`, the daemon resumes normal dispatch.

This **silently loses operator pause intent across a crash.** If the operator issued pause because a migration release was about to be installed, or because they needed the daemon quiescent for an external operation, the restart immediately starts dispatching runs against the operator's expressed wish.

**Safe / partially safe / unsafe / unbounded.** **Unsafe.** The pause intent is durable only in the event log. Nothing in the state-reconstruction path (ON-030) consults the event log for operator-control intent (EV-017: "events may be lost between fsyncs"; EV-INV-002: "consumers tolerate gaps"). If the last `operator_pause_status{status=paused}` event is lost in a between-fsync window, there is not even a durable audit trail.

This is a concrete correctness problem for `upgrade`: ON-019 requires an operator pause to install a migration release. If the operator issues `pause`, drain completes, operator runs `harmonik upgrade <hash>` from terminal A, the upgrade begins (`operator_upgrading` emitted), then the daemon crashes (panic, power loss) after the exec marker is written but before the new daemon reaches `ready`. The restart sees no marker of "we were in the middle of an upgrade" and comes up in `running` — exactly the state that ON-019 forbids for a migration release.

**Concrete spec-text proposal.** Add **ON-030a — Pause-state persistence**:

> When the daemon transitions to `paused`, `improvement-paused`, or `upgrading`, it MUST durably persist a marker at `.harmonik/daemon.state` (JSON: `{"state": "paused"|"improvement-paused"|"upgrading", "pause_reason": ..., "entered_at": <rfc3339>, "expected_commit_hash": ... }`) via the atomic-write idiom (temp + rename + parent fsync per [workspace-model.md §4.7] discipline). The marker MUST be removed on successful transition to `resuming` (for pause) or `running` (after upgrade exec-replace reaches `ready`). On daemon startup, the presence of the marker MUST gate PL-005 step 9's `running` transition: the daemon MUST enter the recorded state (`paused` / `upgrading`) rather than `running`, AND MUST emit a `daemon_degraded{reason=recovered-operator-state}` observability signal so operators can tell a restart-after-pause-crash from an operator-initiated pause.

> Exception: if the marker records `state=upgrading` and the new binary's commit hash does NOT match `expected_commit_hash`, the daemon MUST emit `operator_upgrade_rejected{reason=crash-during-upgrade, actual_hash, expected_hash}` and enter `paused` (not `upgrading`) — the crash interrupted the install, and the operator must re-invoke `harmonik upgrade` to proceed.

### Scenario 3 — Crash during `harmonik upgrade` between drain-complete and exec-replace

**Affected requirements.** ON-005 (commit-hash check), ON-008 (drain gate), ON-020 (upgrade contract), ON-021 (recoverability invariant), §7.3 upgrade pseudocode, PL-027 (daemon-internal mechanics: exec marker, fd-passing, startup skip-path).

**What the spec says.** §7.3 pseudocode:

```
IF daemon.status != "paused": RETURN exit_code("upgrade-requires-paused")
actual_hash = compute_commit_hash(new_binary_path)
IF actual_hash != expected_hash: emit("operator_upgrade_rejected"); RETURN …
…
daemon.state = "upgrading"
emit("operator_upgrading", expected_hash)
exec_replace(new_binary_path)   -- same socket path; clients retry per ON-020
```

**What actually happens.** The sequence has FIVE distinct durable substeps:

1. `operator_upgrading` emitted (EV class not declared — assumed F).
2. PL-027(iii) fd-passing preparation: `fcntl(listener_fd, F_SETFD, 0)` to clear CLOEXEC.
3. `HARMONIK_UPGRADE=1` + `HARMONIK_LISTENER_FD=<n>` env set.
4. `.harmonik/daemon.upgrading` filesystem marker written (PL-027(iv) declares this "MAY be written … informative, not normative" — so not a durable contract).
5. `execve(2)` called on the new binary.

Crash points:

- **Between (1) and (5).** `operator_upgrading` durable; exec never happened. Pidfile locked by dying process (released on exit). Next restart: fresh daemon binary (possibly old, possibly new — depends on what the operator's `harmonik upgrade` script has done to the on-disk binary). Pidfile stale (PL-024). ON-020(a) binary-source mechanism is unspecified here; if the operator rolled-back the binary, recovery is clean. If the operator replaced-in-place AND the crash happened AFTER the binary was replaced but BEFORE exec, the next daemon invocation runs the NEW binary without the pause-verified exec flow. **Catastrophic**: a migration release can now install without operator pause confirmation because the commit-hash-check (ON-005) is a live-daemon-state check, not a crash-recovery check.
- **Between (4) and (5).** Same as above but with `.harmonik/daemon.upgrading` marker on disk. Spec calls the marker "informative, not normative" so recovery cannot depend on it. Missed opportunity: make it normative for recovery.
- **After (5), during new binary's PL-005 startup, before PL-009 `ready`.** PL-027(ii) says the new binary skips PL-005 step 3 (orphan sweep) because `HARMONIK_UPGRADE=1`. But if the new binary crashes at step 0 (composition-root bootstrap fails), the exec-side env marker is gone (child process is dead); a future `harmonik daemon` invocation won't know to skip the orphan sweep. PL-025a's lifecycle-event pairing tolerance covers it on the event side, but the RUN-side implication (running agent subprocesses bearing the old daemon's PGID) is not covered by ON. PL-INV-005 says agent-subprocess parentage is daemon-originated; a re-exec failure followed by a fresh daemon start violates this for the outgoing daemon's still-alive agents.

**Safe / partially safe / unsafe / unbounded.** **Partially unsafe.** The commit-hash check does not survive the crash boundary. On any crash between drain-complete and exec-replace, the next restart runs whatever binary is on disk at `harmonik daemon` invocation, bypassing ON-005. This is a silent integrity-gate failure.

**Concrete spec-text proposal.** Promote the `.harmonik/daemon.upgrading` marker to normative and make it carry the commit-hash witness:

> **ON-020a — Upgrade-intent marker is normative.** Before calling `execve(2)`, the outgoing daemon MUST write `.harmonik/daemon.upgrading` atomically (temp + rename + parent fsync) containing `{"expected_commit_hash": <hex>, "marked_at": <rfc3339>, "outgoing_pid": <pid>, "schema_version_before": <int>}`. The marker MUST be removed (unlink + parent fsync) after the new daemon reaches `ready` AND emits `operator_upgrade_completed`. On any daemon startup (fresh or exec-followup), the presence of the marker MUST gate PL-005 step 0: (a) compute the starting binary's commit hash and compare to `expected_commit_hash`; (b) on match, proceed with PL-027(ii) skip-path startup AND schedule removal of the marker in the `operator_upgrade_completed` emission path; (c) on mismatch, emit `operator_upgrade_rejected{reason=crash-during-upgrade,actual_hash,expected_hash}`, remove the marker, AND enter `paused` (not `running`) per ON-030a.

Cross-coord: PL-027(iv) to be amended in PL v0.4.1 promoting the marker from "informative" to normative under this rule.

### Scenario 4 — Crash during structured-log write; ON-035 durability under crash

**Affected requirements.** ON-035 (structured-log wire format), EV-019 (panic flushes structured-log channel), §8 exit code 19 remediation pointer ("Inspect structured-log records around the panic timestamp").

**What the spec says.** ON-035: "newline-delimited JSON record carrying… `ts` (RFC3339 with ms), `level`, `subsystem`, `run_id?`, `node_id?`, `msg`, `fields`." Secrets-redaction applies pre-emission. Detailed schema + log-rotation policy deferred to OQ-ON-007. Durability class NOT named.

**What actually happens.** Structured-log is plausibly going to be backed by `zerolog` / `zap` / equivalent, with an in-process ring buffer and async flush. EV-019 says "the daemon's top-level recovery handler MUST flush the structured-log channel before exit." This works for Go `panic`. It does NOT work for:

- SIGKILL: no handler runs; anything in the buffer is lost.
- SIGSEGV from cgo: Go's panic barrier may not catch it; buffer lost.
- Power loss: OS page cache loss; even fsync'd data may not have reached disk if the filesystem wasn't in data=journal mode.
- Log rotation mid-write: if the writer renames the file for rotation while another goroutine is writing, the in-flight bytes land in the old (now-unlinked) file.

Exit code 19's remediation pointer sends operators to "structured-log records around the panic timestamp" — but if the panic itself corrupted the log buffer (e.g., a panic in the JSON encoder's marshaling path), the last-durable-log-record is N ms BEFORE the panic, not at the panic. Operators will look at logs and see silence, which is the worst possible diagnostic experience.

**Safe / partially safe / unsafe / unbounded.** **Partially unsafe.** EV-019 covers the panic case via the top-level `recover()` flush, but:
- Double-panic (panic in the panic handler) is covered by PL-018a as an accepted limitation, but ON's §8 code 19 remediation pointer does not acknowledge it.
- Structured-log durability class is unstated; defaults to whatever the implementer chooses.
- Log-rotation atomicity is tracked in OQ-ON-007 (deferred).

**Concrete spec-text proposal.** Amend **ON-035** with a durability clause:

> The structured-log writer MUST implement the following durability discipline: (a) `level ∈ {error}` records MUST be written with per-record fsync (writer-backed `bufio.Writer.Flush()` + `file.Sync()`); (b) `level ∈ {warn, info, debug}` records MAY be batched with an opportunistic-flush timer (operator-configurable, default 100 ms) and MUST be force-flushed on: top-level `recover()` per EV-019; daemon startup (via a startup-sentinel record); daemon clean-shutdown per PL-011; and on every F-class JSONL emission. (c) Log rotation MUST use the write-to-new-file + atomic-rename idiom (NOT rename-old-then-reopen), preserving in-flight writers until their flush completes.

Add to §8 exit code 19's remediation pointer: "Note: SIGKILL, SIGSEGV-outside-panic-barrier, and double-panic MAY produce empty structured-log tails; reconciliation's Cat 0 and Cat 6 detectors are the diagnostic fallback for those crash shapes."

### Scenario 5 — RTO measurement under wall-clock regression

**Affected requirements.** ON-031 (RTO target SIGTERM → ready), ON-032 criterion 3 (300-s ceiling), ON-033 (RTO measurement boundary), EV-002c (UUIDv7 HWM clock-regression handling), PL-025a (lifecycle-event pairing tolerance).

**What the spec says.** ON-033: "The RTO of §4.8.ON-031 MUST be measured from SIGTERM (or daemon crash timestamp recorded by the OS) to the daemon's `ready` status event emission per [process-lifecycle.md §4.2]." Measurement MUST NOT start from `harmonik daemon` invocation time.

**What actually happens.** The SIGTERM timestamp is external to the new daemon's process; it must be recorded somewhere the new daemon can read. PL-011 step 5 (`daemon_shutdown` emission) is the natural carrier — `{shutdown_at}` is the payload field. But: SIGKILL skips PL-011 entirely. PL-025a names the pairing-tolerance rule but does not hand the next restart a SIGTERM timestamp.

Then: the new daemon emits `daemon_ready` with `{ready_at}` (PL-009 line 302). `ready_at` is "the wall-clock time at emission" (not specified, assumed RFC3339). RTO = `ready_at - shutdown_at`. This is a subtraction of two wall-clock timestamps from two distinct processes.

Under wall-clock regression (NTP fix, VM pause, laptop sleep resumed after the outage), `ready_at` can be BEFORE `shutdown_at`, producing a negative RTO or (more perniciously) an RTO that silently appears within tolerance when it actually exceeded 300 seconds. EV-002c addresses this for UUIDv7 ordering via the HWM file; ON-033 has no analog.

Compare to PL v0.4.0's R2 findings: PL-025a acknowledges unpaired events via the pairing-tolerance rule; but PL does not mandate that `daemon_shutdown{shutdown_at}` be monotonic-clock-corrected, and ON does not mandate that RTO computation reject implausibly-negative intervals.

**Safe / partially safe / unsafe / unbounded.** **Unsafe.** RTO measurement is a load-bearing MVH target that can silently miscompute under clock regression. A 400-second RTO (ceiling breach, should emit `daemon_degraded`) can appear as 5 seconds of elapsed wall-clock if the wall clock regressed by 395s mid-restart.

**Concrete spec-text proposal.** Amend **ON-033**:

> RTO measurement MUST use a dual-source timestamp for anti-regression: the `daemon_shutdown{shutdown_at}` event (PL-011a, PL-025a) carries BOTH `shutdown_at_wall` (RFC3339) AND `shutdown_at_ns_since_boot` (the kernel's `CLOCK_BOOTTIME` reading at SIGTERM handling). The new daemon, on emitting `daemon_ready`, reads its own `CLOCK_BOOTTIME` (same kernel = same boot epoch across this single restart unless boot occurred between shutdown and restart, in which case `CLOCK_BOOTTIME` restarts from zero — detectable and the RTO measurement MUST fall back to `shutdown_at_wall` and tag the measurement `{boot_transition: true}`). The subtraction is `boot_time_at_ready - shutdown_at_boot` (monotonic where possible, fallback to wall with explicit flag). SIGKILL-style crashes with no `daemon_shutdown` emission produce an unknown SIGTERM timestamp; RTO MUST be tagged `{rto_undefined_reason: no_shutdown_event}` rather than computed from a fallback. Operators observing `rto_undefined` MUST be notified that RTO cannot be audited for that restart.

Cross-coord: EV §8.7.3 `daemon_shutdown` payload schema must grow a `shutdown_at_ns_since_boot` field (class F already per OQ-PL-012 coordination); `daemon_ready` payload likewise grows `ready_at_ns_since_boot`.

### Scenario 6 — Panic in operator-command-dispatch goroutine

**Affected requirements.** PL-018a (panic barrier), ON-008 (pause drain gate), ON-013 (operator-control events), §7.1 FSM (state transitions atomic in-memory).

**What the spec says.** PL-018a: "panics inside… daemon goroutines (dispatcher, reconciler, subsystem loops) MUST be caught by per-goroutine `recover()` and escalate to the top-level barrier only on repeated failure." ON does not declare the operator-command-dispatch goroutine's supervision.

**What actually happens.** Operator issues `pause`; the command is received over the socket, dispatched to an operator-command handler goroutine. The handler transitions `daemon.state = "pausing"`, emits `operator_pause_status{status=pausing}`, starts the drain. A panic in the drain orchestration (say, a nil-pointer in the workspace manager's unlock path) fires. Per PL-018a: per-goroutine `recover()` catches the panic; the drain goroutine dies. What happens to `daemon.state`?

- §7.1 has no "on panic, revert to prior state" rule.
- `operator_pause_status{status=pausing}` was emitted; `operator_pause_status{status=paused}` will not be emitted because drain never completed.
- The pause command's caller (socket client) is waiting for an ack; it sees a hung socket or `ECONNREFUSED` depending on the handler's error-handling shape.

PL-018a's "escalate on repeated failure" suggests: the daemon stays up in a partially-transitioned state (`pausing`, no drain progress). Operator sees `harmonik status = pausing` forever. `stop --graceful` might rescue via PL-011's fresh drain loop. `stop --immediate` will work but abort in-flight runs (ON-009 violation of operator's original intent, which was to drain cleanly).

**Safe / partially safe / unsafe / unbounded.** **Partially unsafe.** The state machine can wedge in `pausing` with no escape except `stop --immediate` or kill-and-restart. The operator's pause intent is partially served (drain started) but not completed (runs may still be in-flight with no further action toward completion).

**Concrete spec-text proposal.** Add **ON-013a — Operator-command-dispatch supervision**:

> The operator-command-dispatch goroutine(s) that execute pause, resume, stop, and upgrade transitions MUST be supervised by a per-command-execution `recover()` barrier. On panic during a transition, the barrier MUST: (a) emit a `operator_command_failed{command, state_before, panic_msg}` event (new event type, registration needed in EV §8.7); (b) revert `daemon.state` to `state_before` if the panic occurred BEFORE any durable side effect (operator_pause_status emission, drain step completion) — otherwise leave the state as it was at panic time and emit `daemon_degraded{reason=command_panic}`; (c) close the socket connection that issued the command with a typed error so the client sees a definitive failure rather than hanging. Retries are the operator's prerogative; the daemon MUST NOT auto-retry a panicked command.

Cross-coord: EV §8.7 add `operator_command_failed` event (class F); HC/socket error vocabulary gains a `command-panicked` typed error.

### Scenario 7 — Two operators issuing pause concurrently from different terminals

**Affected requirements.** OQ-ON-004 (concurrent-operator-attach arbitration, default deferred), ON-013 (operator-control events), §7.1 FSM (state transitions), ON-011.

**What the spec says.** OQ-ON-004: "Single-operator MVH assumption makes this acceptable; revisit when multi-operator deployments appear." Default-if-unresolved: "The second command observes the state-machine in the post-first-command state and either no-ops (both paused) or errors (if incompatible). No explicit lock."

**What actually happens.** Terminal A runs `pause` at t=0. Terminal B runs `upgrade <hash>` at t=1ms. Both socket connections arrive, dispatched to two operator-command handler goroutines. Both goroutines read `daemon.state`. Depending on interleaving:

- **Read-read-write-write race.** Both read `state=running`. Goroutine A writes `state=pausing` and starts the drain. Goroutine B writes `state=upgrading` (from running! — violation of §7.1; upgrade requires paused). The goroutine B code path might emit `operator_upgrading` (now the daemon claims to be upgrading while it's actually draining for pause). State is inconsistent.
- **With a mutex (implicit in OQ-ON-004's default).** Goroutine A wins, takes lock, transitions to `pausing`, releases lock. Goroutine B wakes, sees `state=pausing`, fails (upgrade requires paused). Emits `operator_command_rejected`. Safe.

The spec does NOT require the mutex. "No explicit lock" is the stated default.

**Safe / partially safe / unsafe / unbounded.** **Unsafe under any plausible implementation without a mutex.** The state-machine transition table §7.1 is presented as atomic; the spec does not declare the atomicity primitive. A naive implementer reading §7.1 could write non-atomic code because there is no ON requirement to use any particular synchronization.

**Concrete spec-text proposal.** Amend **ON-011**:

> The daemon MUST implement the operator-control state machine defined in §7.1 as a serializable state machine: every state transition (read-current-state → validate-guard → write-new-state → emit-event) MUST execute atomically with respect to other operator-command dispatches AND with respect to automatic transitions (drain-complete, improvement-loop trigger). Implementers MUST use a single mutex (OR equivalent CAS-with-retry primitive) protecting the state field; multiple operators attaching concurrently AND issuing commands simultaneously MUST observe serializable semantics. A second command arriving during the first command's execution MUST observe the post-first-command state (OR block if the first is still transitioning, bounded by a 1-second fairness timeout before rejection).

Upgrade OQ-ON-004 from "deferred default" to "required serializability" for MVH.

### Scenario 8 — Operator stops daemon while reconciliation Cat 6 escalation is pending

**Affected requirements.** ON-009 (`stop --immediate` aborts in-flight), ON-010 (reconciliation carve-out), RC-INV Cat 6a (operator escalation surface), §8 `operator_escalation_required` event, PL-010 (degraded state).

**What the spec says.** ON-010: "Pause MUST NOT interrupt reconciliation workflows." But ON-009 allows `stop --immediate` to abort ANY in-flight run including reconciliation workflows. Cat 6 escalations are specifically surfaced via `operator_escalation_required` events per [reconciliation/spec.md §8.11a], awaiting operator action.

**What actually happens.** A Cat 6a integrity-violation-investigator workflow has completed its reasoning and emitted `operator_escalation_required{run_id, escalation_reason}`. The workflow has NOT yet emitted a terminal verdict — it's awaiting operator decision. Operator issues `stop --immediate`. Per ON-009, the in-flight investigator workflow is aborted (emits `run_failed{class=canceled}` on next restart per ON-009). The `operator_escalation_required` event was durable (F class — or is it? EV §8.11a classification is not clearly stated in ON; need cross-ref).

On next restart, reconciliation detects:
- The target run (whose Cat 6a was being investigated) is still in its original in-flight state — the investigator's verdict-execution never ran.
- The investigator's run itself failed with class=canceled, routed to Cat 3b (verdict-unexecuted).
- The original `operator_escalation_required` event is in the JSONL event log but no UI subsystem reads the event log for surface-rendering (UI is a §4.a out-of-scope concern).

So: the operator issued stop; the escalation surface is durably in the event log but nobody renders it. On next `harmonik status`, does the operator see the pending escalation?

**Safe / partially safe / unsafe / unbounded.** **Partially unsafe, silent loss of escalation visibility.** The escalation is recoverable (it's in the event log) but the ON-declared operator surface (ON-036 health-check, ON-046 budget events via `harmonik status`) does not name pending-escalation surface visibility. The operator can re-query via reconciliation workflow state if they know to, but the spec doesn't make it observable by default.

**Concrete spec-text proposal.** Add to **ON-036 (health-check interface)** or create **ON-036a — Pending-operator-escalation surface**:

> `harmonik status` MUST report any undelivered `operator_escalation_required` events (per [reconciliation/spec.md §8.11a]) that have NOT been matched by a subsequent operator resolution (confirm-verdict, veto-verdict per ON-014). The list MUST survive daemon restart: on restart, the reconstruction contribution of reconciliation (per ON-INV-005) MUST include scanning for unmatched escalation events since the last `operator_escalation_cleared` event (new event, registration required) OR the most recent prior daemon_started. Operators MUST see the pending escalation in `harmonik status` output to avoid silently-deferred Cat 6 issues.

Cross-coord: EV §8.7 / §8.11a add `operator_escalation_cleared` event; RC §4.5 names the confirm-verdict/veto-verdict event shape.

### Scenario 9 — Crash during in-flight queue-format migration

**Affected requirements.** ON-016 (queue-schema check on startup), ON-017 (Beads pre-1.0 absorbed), ON-018 (N-1 compat), ON-019 (migration releases at operator pause), BI-030 (intent log fsync).

**What the spec says.** ON-019 says migration releases require operator pause; ON-020(d) requires upgrade to refuse if the new binary's schema is incompatible with on-disk state. But: what's "on-disk state" mid-migration? Intent-log entries (BI-030) are on-disk state. They encode Beads queries at a particular Beads CLI version (BI-025b JSON output mode). A Beads SQLite schema bump between writing an intent and processing it invalidates the intent's consistency.

**What actually happens.** Operator pauses; drain completes; `harmonik upgrade` fires with `--expected-hash` for a new binary carrying both a new ON overlay schema AND pinning a new Beads version (via ON-017 version-pin). The upgrade exec-replaces. The new binary starts; runs PL-005; hits BI-031 intent-recovery — reads an intent file written by the OLD adapter against the OLD Beads version. The intent file's `intended_post_state` field encodes the old Beads schema's state vocabulary. New adapter attempts `br show <bead_id>` with new Beads — which may use a different status vocabulary or JSON schema (BI-031b classifies this as `BrSchemaMismatch`).

ON-019's migration-pause discipline is supposed to handle this — but the pause was issued BEFORE the binary swap, and the intent file exists because the old binary's last action before the pause was a terminal transition that wrote the intent and was SIGKILL'd at step N. Actually no — the drain is supposed to flush all intents before emitting `paused` per ON-027. But ON-027 does NOT name intent-log completion as a drain step. Steps 4 (event-bus flush), 5 (memory indexing), 6 (workspace unlock), 7 (exit) — no mention of intent-log drain.

**Safe / partially safe / unsafe / unbounded.** **Unsafe.** Pause-then-upgrade across a schema-migration release can inherit intent files from a prior-schema adapter that the new adapter mishandles. ON-027's drain does not include intent-log drain as a step, so the gate is not tight enough.

**Concrete spec-text proposal.** Amend **ON-027** step list:

> … (3) agent runners wait for handler subprocesses to complete or reach the drain timeout; **(3a) `br`-CLI-adapter intent-log drain: all pending intent-log entries (per [beads-integration.md §4.10 BI-030]) MUST be drained via the BI-031 recovery protocol — each intent resolved to either "committed" (status match; intent deleted) or "pending reissue" (intent retained, will be retried on next `ready`). The drain timeout (ON-029) applies; intent files unable to resolve within the timeout MUST block `paused` entry (or escalate to `drain-step-errored` exit code 21) because their presence across a schema bump is unsafe.** (4) event bus flushes pending events; …

Cross-coord: BI §4.10 amend BI-031 with "drain-time-complete" mode flag that tightens the resolution rule to fail-rather-than-retry.

### Scenario 10 — SIGTERM with in-flight runs; agent subprocess hung

**Affected requirements.** ON-008 (pause drain gate), ON-027 (seven-step drain), ON-029 (drain timeout operator-configurable), §3 `in_flight(run)` predicate, PL-011 step 3 classification, HC-011 watcher, §8 exit code 11 (drain-timeout-escalated).

**What the spec says.** ON-027 step 2: "every run for which `in_flight(run)` holds… proceeds to its next checkpoint, then suspends." Step 3: "agent runners wait for handler subprocesses to complete OR reach the drain timeout." ON-029: drain timeout is operator-configurable; default in config inventory. §8 code 11: drain-timeout-escalated.

**What actually happens.** A handler subprocess is hung (deadlocked in a tool call, infinite-loop in the agent's prompt interpretation, JIT-recompiling a stuck Python import, whatever). `in_flight(run)` returns true. SIGTERM arrives; drain enters step 2. The run's agent subprocess does not reach a checkpoint because the handler is hung. Step 2 waits until `drain_timeout` (via ON-029 knob).

On drain-timeout expiry, per §7.2 pseudocode: `IF any_step_exceeded_its_bound: RETURN exit_code("drain-timeout-escalated")` — i.e., code 11. But per PL-011 step 4: "On drain-timeout expiry, the daemon MUST send SIGKILL to surviving agent subprocesses and proceed to step 5." So the daemon SIGKILLs the hung agent, continues drain, exits with code 11.

The hung agent's output work product MAY or MAY NOT be checkpoint-committed depending on where it was when SIGKILL hit. The workspace is NOT released cleanly (step 6 runs after SIGKILL, but the worktree might be in a mid-write state). Next restart reconciliation classifies the run as Cat 2 (non-idempotent in-flight) or Cat 1 (idempotent rerun) depending on the node's idempotency class.

ON-027 and ON-029 look correct for this scenario. What's NOT named: what the operator observes. The exit-code-11 remediation in §8 points to "Increase drain timeout… investigate stuck handler." OK, but the STUCK handler is gone (SIGKILL'd); the operator has no diagnostic surface unless the structured-log emissions of the silent-hang detector (ON-040) landed. ON-040 says silent-hang detection is in handler-contract, but ON does not require the detector to fire BEFORE the drain timeout. The drain-timeout-plus-silent-hang race is not resolved.

**Safe / partially safe / unsafe / unbounded.** **Partially safe but operator-diagnostically-weak.** Reconciliation handles recovery. But the operator-observable story is: daemon took 5 minutes to shut down, then exit 11, with possibly no silent-hang event if detection hadn't fired yet. The `agent_warning_silent_hang` event (HC §7.1, EV §8.3) is the intended signal but its threshold is not coupled to the drain timeout.

**Concrete spec-text proposal.** Amend **ON-040**:

> When the drain timeout (ON-029) expires on an in-flight run whose handler subprocess is still alive AND no `agent_warning_silent_hang` has been emitted for that subprocess, the drain path MUST synthesize an `agent_warning_silent_hang{reason=drain_forced}` event before SIGKILL per PL-011 step 4. This ensures every drain-timeout-escalated exit (§8 code 11) carries a diagnostic signal in the event log for the operator to correlate with the stuck handler, rather than leaving the cause unannounced.

### Scenario 11 — Secret-redaction failure mid-write

**Affected requirements.** ON-022 (secrets injected at handler launch, never logged), ON-023 (compile-time secret-typed-field check), ON-INV-003 (secrets never in durable sinks unredacted), EV-035 (redaction hook on emit).

**What the spec says.** ON-022: "Redaction is mechanism-tagged and MUST be enforced pre-emission: handler implementations MUST apply prefix-regex matching and per-handler redaction patterns before any write to a durable sink (event bus, session log, audit record)." ON-INV-003 two-part sensor: compile-time schema-linter + regression-test harness.

**What actually happens.** Redaction is Go code (prefix-regex matching). Prefix-regex matching can panic on pathological inputs (unusual UTF-8, extremely long strings exhausting stack). EV-035 is the redaction hook inside `Emit`; `Emit` returns after redaction + JSONL append + sync dispatch. If the redactor panics:

- The panic is NOT caught by the caller's `recover()` because `Emit` is called from many call sites.
- Per PL-018a, per-goroutine `recover()` catches it; escalates to top-level on repeated failure.
- But: what's the event's fate? The JSONL append did NOT happen (panic pre-empted it). The sync dispatch did NOT happen. The event is lost.

Worse alternative: an implementer who wants to be robust might write `if redact(payload) fails, write payload anyway with a "REDACTION_FAILED" marker`. This directly violates ON-INV-003.

Worst alternative: `if redact(payload) fails, write a fallback "redacted" placeholder`. Looks safe but the placeholder's content may leak information ("payload length was 1247 bytes") that correlates with known secrets by size.

**Safe / partially safe / unsafe / unbounded.** **Partially unsafe.** ON-022's "MUST be enforced pre-emission" is underspecified at the panic boundary. Without a fail-closed rule, implementers might unintentionally violate ON-INV-003 in the name of robustness.

**Concrete spec-text proposal.** Amend **ON-022**:

> Redaction MUST fail-closed: if the redactor raises an error or panics during its operation on an event payload, the emission MUST be aborted, the event MUST NOT be written to any durable sink, and a `redaction_failed{event_type, payload_length, redactor_error}` observability event (new event type, class F) MUST be emitted via a secondary redaction-free path that writes ONLY the metadata (no payload). The original event's producer MUST receive an error return from `Emit`; per EV-018 the producer is responsible for idempotent retry. Under no circumstance MAY the redactor's failure cause the unredacted payload to land in a durable sink. ON-INV-003 sensor (part b: regression test harness) MUST include a panic-injection test that asserts no redactor-panic-induced payload escape.

Cross-coord: EV §8 add `redaction_failed` event (class F, `mechanism`-tagged).

### Scenario 12 — Multi-daemon ceiling under daemon B crash mid-claim

**Affected requirements.** ON-041 (machine-ceiling mechanism), ON-042 (multi-tenancy deferral), OQ-ON-003 (coordinator-vs-lock unresolved), §8 code 18 `machine-ceiling-exhausted`, ON-048 (budget cascade to `dispatch_deferred`).

**What the spec says.** ON-041 names a machine-level agent-subprocess ceiling "enforced by a shared lock OR a machine-level coordinator process." OQ-ON-003 default is "filesystem-based shared-counter lock at `~/.harmonik/machine-ceiling.lock` with advisory locking."

**What actually happens.** Daemon A and Daemon B both want to spawn an agent subprocess. Shared counter currently at N; ceiling is N+1.

- Daemon A acquires flock on `~/.harmonik/machine-ceiling.lock`.
- Daemon A reads counter file `~/.harmonik/machine-ceiling.count` → reads N.
- Daemon A writes N+1 back (not yet fsync'd).
- Daemon A is SIGKILL'd between write and fsync. Kernel releases flock on process exit. OS may or may not flush the write to disk depending on page-cache state.
- Daemon B acquires flock.
- Daemon B reads counter → MIGHT read N (write lost in page cache on SIGKILL) OR N+1 (write landed).
- If Daemon B reads N, it writes N+1, spawns agent. Real count = N+2 (A's orphan + B's). **Ceiling violated silently.**

Even worse: if A wrote the counter to N+1, started the spawn, then SIGKILL'd before the spawn succeeded — now counter says N+1 but there are only N agents actually running. Over time, counter drifts upward, reporting exhausted when actual load is low.

ON-041 says "enforced by a shared lock"; the lock is released on process exit, but the counter value is not atomically rolled back. No fencing mechanism is declared.

ON-042's "deferred" posture covers the POLICY question (shared quotas), not the INTEGRITY question (counter consistency).

**Safe / partially safe / unsafe / unbounded.** **Unbounded drift.** Every crash of a daemon that was mid-counter-update leaves an inconsistent counter. There is no periodic reconciliation of the counter against the live-daemon-plus-live-agents set. Over many crashes, the counter becomes meaningless.

**Concrete spec-text proposal.** Amend **ON-041** (or add ON-041a):

> The machine-level agent-subprocess counter MUST be reconcilable from ground-truth observations: at regular cadence (operator-configurable, default 60 s) OR on acquisition of the machine-ceiling lock, the acquiring daemon MUST (a) enumerate live harmonik daemons via `harmonik list` mechanism (pidfile scan + process liveness probe per PL-002a); (b) for each live daemon, query the per-daemon concurrency-counter via its socket (new JSON-RPC method: `get-agent-count`); (c) sum the per-daemon counts into a ground-truth total; (d) overwrite the shared counter atomically (temp-file + rename + parent-fsync) with the reconciled value. This drift-reconciliation prevents unbounded counter drift from partial-write crashes. OQ-ON-003 SHOULD resolve with a coordinator-daemon approach if measured drift under crash-injection exceeds 5% false-positive ceiling-exhausted rate.

### Scenario 13 — Concurrent operator attach; daemon X crashes mid-session

**Affected requirements.** ON-041 (`harmonik list` + daemon identification flags), PL-003a (JSON-RPC over socket), PL-009b (external-caller ready protocol), OQ-ON-004 (concurrent-operator-attach).

**What the spec says.** ON-041 declares multi-daemon commands. Attach is mentioned by ON-007 (operator-facing "task" terminology) and PL-028 (`harmonik attach`). OQ-ON-004 defers serialization.

**What actually happens.** Operator runs `harmonik attach --daemon-id X`. The socket is opened; a bidirectional stream carries events and commands. Daemon X crashes (panic, SIGSEGV). The attached terminal sees:

- If the kernel closes the socket cleanly: `EOF` on the attach session. The CLI can print "daemon disconnected" and exit.
- If the kernel leaves the socket in an indeterminate state: client blocks on `read(2)`, producing a hung terminal.

ON does not name the attach client's error-handling contract. PL-009b covers *readiness detection* for external callers but not *disconnection detection* for attached sessions.

Worse: after Daemon X crashes, a subsequent `harmonik daemon` invocation acquires the stale pidfile (per PL-024) and binds a new socket. An attached terminal that auto-reconnects (common `attach` behavior) now talks to a DIFFERENT daemon instance without any signal that the daemon identity changed. The operator thinks they're attached to the same daemon; they are actually interacting with a fresh daemon that has reconstructed state via reconciliation.

**Safe / partially safe / unsafe / unbounded.** **Partially unsafe.** The operator UX on daemon crash during attach is unspecified. No typed error is declared for "daemon disconnected mid-session"; no daemon-identity token is declared that would let an attached client detect "I reconnected but it's a different daemon now."

**Concrete spec-text proposal.** Add **ON-013b — Attach session daemon-identity contract**:

> Every `harmonik attach` session MUST observe a daemon-instance-identifier exchanged on attach handshake: the daemon's `daemon_instance_id` (a UUIDv7 minted per PL-005 step 0 bootstrap, durably recorded in `.harmonik/daemon.instance-id` and in the pidfile payload per PL-002b). On any reconnection attempt within an attach session, the client MUST verify the reconnected daemon's `daemon_instance_id` matches the original; a mismatch MUST produce a typed error `attach-daemon-instance-changed` and MUST cause the attach CLI to exit non-zero with a clear operator message ("the daemon you were attached to has crashed and been replaced; re-attach manually"). This prevents silent cross-instance state reads.

Cross-coord: PL-002b add `daemon_instance_id` field to pidfile payload; PL-005 step 0 mint the UUID.

### Scenario 14 — Audit-record crash-window coverage

**Affected requirements.** ON-038 (audit records are subset of traces), ON-039 (observability mechanism-tagged), ON-INV-003 (secret redaction), EV-014b (consumer tail-truncation tolerance), execution-model §4.4 transition-record sibling files.

**What the spec says.** ON-038: "Audit records MUST be produced as a subset of transition records… No separate audit-log store is introduced; audit is a query over the transition-record sibling files and their projections." So audit = git-committed transition-records + JSONL event log projection.

**What actually happens.** Transition-records are committed atomically with their checkpoint commits (EM-016 tree-construction atomicity). Audit projection is a READ over git + JSONL. A crash mid-audit-READ is not a problem — the reader restarts. BUT: a crash mid-audit-DERIVATION might be — if audit derivation involves writing a cache or index somewhere, that index can be stale across restart. ON-038 does not name whether audit derivation is pure-read or maintains state.

More subtle: JSONL tail truncation (EV-014b) means the post-crash JSONL may be missing the last N events. Audit-derivation over those N events is WRONG (under-reports privileged-actor-affected-policy transitions). The tail-truncation signal (EV-014b `on_tail_truncation` callback) is a consumer-side contract, but ON-038's audit is declared as a query, not a consumer. Is audit derivation required to subscribe to `on_tail_truncation`?

**Safe / partially safe / unsafe / unbounded.** **Partially unsafe.** Audit's completeness under crash is not specified. An operator querying audit after a crash may get silently-truncated results unless the query explicitly reconciles JSONL tail-truncation against git + transition-records (git is authoritative per locked decision; JSONL is observational).

**Concrete spec-text proposal.** Amend **ON-038**:

> Audit derivation is a query over (a) git transition-records (authoritative per locked decision #12) AND (b) JSONL event log (observational, subject to tail-truncation per EV-017). Audit queries MUST reconcile: where a transition-record exists in git but no corresponding `transition_event` is present in JSONL (post-truncation), the audit record MUST be synthesized from the git transition-record alone, with a `derivation_source="git-only"` annotation. Where a `transition_event` exists in JSONL with no corresponding git transition-record, the audit derivation MUST emit `store_divergence_detected` per EV-023 and the audit query MUST fail-closed (return an error; audit MUST NOT report the JSONL-only signal as authoritative). This closes the tail-truncation gap for audit completeness.

### Scenario 15 — Improvement-pause handoff across crash

**Affected requirements.** ON-012 (improvement-pause subtype), ON-013 (`operator_pause_status` paired-phase), §7.1 FSM improvement-paused path, ON-030a (proposed in Scenario 2).

**What the spec says.** ON-012: improvement-pause "transitions `running` → `pausing` → `paused` via the same path as operator pause, with the additional invariant that `paused → resuming` is triggered automatically when the improvement loop completes." Events carry `pause_reason = improvement`.

**What actually happens.** Daemon in `improvement-paused` state. Improvement loop is running (it's a workflow, per S09). Daemon crashes. Next restart:

- Per Scenario 2's analysis, the daemon comes up in `running` (no pause state persisted).
- The improvement loop's workflow was interrupted mid-run; reconciliation classifies it (likely Cat 2 or Cat 3 depending on idempotency).
- The pause_reason ("improvement") is lost. If ON-030a (proposed above) is accepted, the marker carries `pause_reason`; otherwise, no.

If the improvement loop's reconciliation auto-resumes its workflow (Cat 1 idempotent rerun), the loop completes — but the daemon is already in `running` (no paused state to resume from). The "paused → resuming" trigger never fires because there is no paused state. The improvement loop's completion emits a signal that nobody is listening for.

**Safe / partially safe / unsafe / unbounded.** **Partially unsafe, correctness issue.** Improvement-pause's "auto-resume on loop completion" semantic is fragile across daemon crash. Without pause-state persistence (ON-030a proposed), the improvement-pause is effectively lost on crash.

**Concrete spec-text proposal.** Covered by ON-030a. Additionally amend **ON-012**:

> The `paused → resuming` auto-trigger on improvement-loop completion MUST be gated on the daemon being in `improvement-paused` state at the moment of completion. On daemon restart after a crash during improvement-paused, the pause-state marker of ON-030a MUST record `pause_reason=improvement`; the restart MUST enter `improvement-paused` (not `paused`); on subsequent improvement-loop completion (which reconciliation has auto-resumed per Cat 1 or similar), the auto-resume path fires as normal. If the pause-state marker is absent and a stale improvement-loop-completion event is observed post-restart, the daemon MUST emit `operator_command_rejected{reason=stale-improvement-completion}` and take no action.

---

## 3. Atomicity claims audit

| Claim location | Atomicity primitive named | Reality check | Verdict |
|---|---|---|---|
| ON-005 commit-hash check (§7.3 pseudocode) | `exec_replace` assumed atomic | Yes, `execve(2)` is atomic but only for the SUCCESSFUL path; pre-exec setup (fd-cloexec clear, env write) is NOT atomic with the exec | Insufficient; see Scenario 3 |
| ON-008 drain gate (`pausing → paused`) | "entry into `paused` is forbidden until all drain steps complete" | Steps themselves are not atomic wrt crash; no durable record of drain progress | Insufficient; see Scenario 1 |
| ON-011 state-machine transitions | §7.1 state machine — atomicity NOT named | Naive impl races; mutex MUST be required | Insufficient; see Scenario 7 |
| ON-013 operator-control event emission per transition | Emit-on-transition-only (§8.9(h) from EV) | Holds producer-side; consumer durability depends on event class | OK given class F |
| ON-016 queue-schema version check | Single `br --version` handshake + SQL schema read | Atomic at read time; does not cover mid-run migration | OK for startup; see Scenario 9 for running case |
| ON-022 redaction pre-emission | "MUST apply prefix-regex matching … before any write" | Panic-fragile; fail-closed unstated | Insufficient; see Scenario 11 |
| ON-027 drain sequence | "each step completing before the next begins" | Sequential but no inter-step crash-recovery rule | Insufficient; see Scenario 1 |
| ON-038 audit derivation | "query over transition-record files and projections" | Read-only; tail-truncation not reconciled | Insufficient; see Scenario 14 |
| ON-041 machine-ceiling shared lock | flock, but counter RMW not atomic | No fencing, no drift reconciliation | Insufficient; see Scenario 12 |
| ON-048 exhaustion-protocol | "emit + terminate at next safe boundary" | OK for in-flight; doesn't address mid-cascade crash | Minor gap; Finding 10 |

---

## 4. Fsync discipline audit

| Artifact ON owns or co-owns | Fsync required? | Where declared | Crash-safety verdict |
|---|---|---|---|
| Pidfile (contents + path) | Yes | PL-002b (R2 integration) | Safe; PL owns |
| Socket file | N/A (socket inode) | PL-003 | OK; kernel-managed |
| Event-bus JSONL (F-class events) | Yes | EV-016, EV-016a | Safe; EV owns |
| Event-bus JSONL (O/L-class events) | No (class-dependent) | EV-016, EV-017 | Tail-loss acceptable per EV-INV-002 |
| `event_id_hwm` file | Yes (piggyback on F-fsync) | EV-002c | Safe; EV owns |
| Spill files (`.harmonik/events/spill-<consumer>.jsonl`) | `O_DSYNC` on create | EV-011a | Safe |
| Dead-letter log | EV-011 (O class by row) | Unclear — row 8.8.3 is O | **Gap; ordinary class = possible tail loss. Dead letters losing on crash seems wrong** |
| Transition-record sibling files | Atomic via git commit | EM-017, EM-018 | Safe; git owns |
| Commit-hash integrity gate (on-disk binary) | No declaration | ON-005, §7.3 | **Gap; see Scenario 3; propose ON-020a marker** |
| Structured-log channel | Unstated | ON-035 | **Gap; see Scenario 4** |
| Operator-control state (daemon.state field) | Not persisted | §4.a(e) | **Gap; see Scenarios 2 & 15; propose ON-030a** |
| `.harmonik/daemon.upgrading` marker | "MAY be written … informative" | PL-027(iv) | **Gap; promote to normative per Scenario 3** |
| Machine-ceiling counter (`~/.harmonik/machine-ceiling.count`) | Unstated (OQ-ON-003 default) | ON-041 | **Gap; see Scenario 12; no atomic update** |
| `daemon.ready` file | PL-009b "MAY write" | Not MUST; informative | OK (fallback only) |
| Config-inventory in-memory snapshot | Not discussed | ON-004 (obligation) | **Potential gap; see Scenario 11 for partial load** |
| Redaction pattern table | Not discussed | ON-022 (handler-owned) | Out of ON scope but see Scenario 11 |
| Audit-derivation cache (if any) | Not specified | ON-038 | **Gap if derivation caches; see Scenario 14** |

---

## 5. Recovery rules coverage

| Scenario | Reconciliation category on restart | Correctly routed? |
|---|---|---|
| SIGKILL during drain (Scenario 1) | Per-run: Cat 1/2/3/4 based on run idempotency-class | Yes for runs; No for operator pause intent (silent loss — Scenario 2) |
| Power loss during pause→paused (Scenario 2) | N/A — no category for "operator intent lost" | **NO — proposal ON-030a covers** |
| Crash during upgrade (Scenario 3) | Per-run via PL-027(ii) skip-path | Partial — no category for crashed-mid-upgrade; proposal ON-020a covers |
| Structured-log mid-write crash (Scenario 4) | N/A (observability layer, not reconciliation) | Not applicable; diagnostic loss only |
| Wall-clock regression RTO (Scenario 5) | N/A | Not a reconciliation concern; measurement-correctness concern |
| Command-dispatch panic (Scenario 6) | N/A (daemon may stay up wedged) | **NO — no rule for state-machine wedge** |
| Concurrent pause race (Scenario 7) | Depends on interleaving | Unsafe unless serializability mandated |
| Stop during Cat 6 escalation (Scenario 8) | Cat 3b (verdict-unexecuted) for the investigator run | Mechanically yes; operator-surface visibility: **NO** |
| Intent-log under migration (Scenario 9) | Cat 3a (torn Beads write) | Yes via BI-031; drain-time integrity not guaranteed |
| Hung handler drain (Scenario 10) | Cat 1 / Cat 2 per idempotency | Yes; operator diagnostic: weak |
| Redactor panic (Scenario 11) | N/A (event not emitted) | Fail-closed not stated |
| Machine-ceiling drift (Scenario 12) | N/A (policy-level; not per-run) | **NO reconciliation** — needs drift correction |
| Attach-daemon-instance mismatch (Scenario 13) | N/A (operator UX) | **NO — needs instance-id** |
| Audit tail-truncation (Scenario 14) | N/A (query-level) | **NO — needs git-authoritative fallback** |
| Improvement-pause crash (Scenario 15) | Cat 1 for the loop workflow | Yes for workflow; **NO** for improvement-pause state |

---

## 6. Temp+rename pattern audit

**Does ON own any files?** Per §4.a(e): "State owned — none persistently." ON declares no files directly. But ON PROPOSES several new files via Scenarios 2, 3, 12, 13:

| Proposed file | Atomic-write discipline | Parent-fsync |
|---|---|---|
| `.harmonik/daemon.state` (ON-030a) | Temp+rename + parent fsync | REQUIRED |
| `.harmonik/daemon.upgrading` (ON-020a) | Temp+rename + parent fsync | REQUIRED |
| `~/.harmonik/machine-ceiling.count` (ON-041a) | Temp+rename + parent fsync | REQUIRED |
| `.harmonik/daemon.instance-id` (ON-013b) | Written ONCE at PL-005 step 0; temp+rename + parent fsync | REQUIRED |

These match the corpus idiom established by WM-013a / WM-026 / EM-017. The proposals are consistent with the canonical atomic-write pattern used elsewhere in the spec set. Each MUST write: (1) open temp file with O_CREAT|O_WRONLY; (2) write payload; (3) fsync(fd); (4) rename(temp, canonical); (5) fsync(parent_dir_fd). PL-002b's exception (truncate-rewrite-keep-fd because flock binds to fd) does NOT apply to these files — no lock binding.

---

## 7. Retry vs re-run discipline

ON is silent on operator-command retry semantics. Sibling specs fill some of this: PL-009b defines client retry for socket probe; BI-030/BI-031 define intent-log retry for terminal Beads writes; EV-011 defines consumer retry for async delivery. ON itself does not name:

- **Pause command retry.** If `harmonik pause` returns an error (daemon unreachable, socket timeout), is the client allowed to retry blindly? What if the first attempt succeeded and only the ack was lost? ON-013 says `operator_pause_status{status=pausing}` is emitted on `running → pausing`, so a retry would observe state=pausing and presumably no-op — but §8 code 16 `operator-control-invalid-state` would fire (invalid: re-issuing pause from pausing). This is a false negative from the operator's view; their pause succeeded but the CLI said "rejected."
- **Upgrade retry.** If `harmonik upgrade` returns an error during the pre-exec handshake (hash mismatch, schema check failure), can the operator re-issue? Typically yes; but if the failure was mid-exec (Scenario 3), there's no clean retry because the daemon may have already started exec.
- **Stop retry.** If `stop --graceful` times out (no response within some bound), should the operator retry with `stop --immediate`? The spec doesn't say.

**Re-run contract under restart.** ON-030 restart reconstruction walks git + Beads; reconciliation dispatches Cat-N workflows. Operator re-runs ARE just the normal Cat 1 / Cat 2 auto-resume paths. For operator commands (pause/stop/upgrade), there is no "re-run after restart" concept because operator commands are not runs — they are synchronous state-machine inputs. The asymmetry is not declared anywhere; it is implicit.

**Concrete spec-text proposal.** Add **ON-013c — Operator-command retry semantics**:

> Operator commands (pause, resume, stop, upgrade, confirm-verdict, veto-verdict) MUST be idempotent at the command-issue level: a second pause issued while the daemon is already `pausing` or `paused` MUST return success with a note (not emit `operator_command_rejected`). A second `upgrade <hash>` with the same expected_hash while the daemon is already `upgrading` with that hash MUST return success. Command idempotency is keyed on (command, state_transition_target); a retry that would produce a NO-OP transition MUST succeed without error. This closes the "lost-ack retry" failure mode for operator CLIs. Note: `stop --immediate` after `stop --graceful` is NOT a retry — it is a semantic escalation and MUST be allowed to fire even if graceful stop is in progress (it will abort the drain).

---

## 8. Cross-spec coordination for each crash scenario

| Scenario | Sibling spec required | Status |
|---|---|---|
| 1 (drain SIGKILL mid-step) | PL-027a drain-crash semantics | Cross-coord needed: PL next revision |
| 2 (pause-state crash loss) | PL-005 reading of daemon.state marker at startup | Cross-coord needed: PL next revision |
| 3 (upgrade mid-exec crash) | PL-027(iv) promote marker to normative + startup-read | Cross-coord needed: PL next revision |
| 4 (structured-log durability) | Possibly quality-checks.md (OQ-ON-007) | Self-contained in ON |
| 5 (RTO clock regression) | PL `daemon_shutdown{shutdown_at_ns_since_boot}` field; EV §8.7.2 / §8.7.3 payload amendment | Cross-coord needed: PL + EV |
| 6 (command-dispatch panic) | EV §8.7 new `operator_command_failed` event | Cross-coord needed: EV |
| 7 (concurrent pause/upgrade) | Self-contained in ON-011 amendment | Self-contained |
| 8 (Cat 6 escalation lost across stop) | RC §8.11a `operator_escalation_cleared` event; EV §8.6 or §8.7 registration | Cross-coord needed: RC + EV |
| 9 (intent-log migration crash) | BI-031 drain-time mode; ON-027 new step 3a | Cross-coord needed: BI |
| 10 (hung handler drain timeout) | HC §4.6 drain-forced silent-hang synthesis | Cross-coord needed: HC |
| 11 (redaction panic) | EV §8 `redaction_failed` event | Cross-coord needed: EV |
| 12 (machine-ceiling drift) | PL-003a new `get-agent-count` JSON-RPC method | Cross-coord needed: PL |
| 13 (attach daemon-instance mismatch) | PL-002b `daemon_instance_id` field; PL-005 step 0 mint | Cross-coord needed: PL |
| 14 (audit tail-truncation) | EV-023 `store_divergence_detected` for audit derivation | Cross-coord: EV (minor — existing event) |
| 15 (improvement-pause crash) | Covered by ON-030a | Self-contained |

Summary: **eight cross-spec coordination items**, with PL being the most-often-coordinated (five). This matches the ON↔PL front-matter-cycle the v0.3.0 integration broke.

---

## 9. Requirements that held up well

- **ON-005 commit-hash integrity gate.** Mechanically right at the pre-exec check point. The gap is the crash-after-check-before-exec hole (Scenario 3), not ON-005 itself.
- **ON-009 `stop --immediate` is the only in-flight aborter.** Clear, locked, consistent with ON-INV-006. Concurrent-command serialization (Scenario 7) is an independent concern.
- **ON-010 reconciliation carve-out.** Queueing pause during `reconciling` is the right primitive and matches PL-010's `degraded` semantics.
- **ON-018 N-1 compat window.** Tight, testable, holds up under the scenarios probed. Migration-boundary tightening (Scenario 9) is an integrity-layer refinement, not a compat-window reopening.
- **ON-INV-001 N-1 holds jointly.** Corpus-level invariant with a named sensor; sensor is compile-time + regression.
- **ON-INV-006 no-control-surface-bypass.** Structural invariant with a reviewer-enforced grep-plus-audit sensor. Sound.
- **§8 exit codes 19, 20, 21.** The I7 amendments add the panic and signal-termination codes cleanly. Code 21 `drain-step-errored` gives a clear surface for ON-027 step-level errors distinct from timeout (code 11).
- **ON-ENV-001 voluntary envelope declaration.** Good corpus hygiene — makes ON downstream-cite-friendly.

---

## 10. Hidden crash-safety assumptions

Numbered list of assumptions that are true iff implementers follow the corpus idiom; not stated explicitly in ON.

1. **JSONL event-log fsync for F-class events is assumed durable to disk (not merely to kernel page cache).** EV-015 uses `fsync(2)` which is contingent on filesystem write-barrier + device flush; consumer SSDs without power-loss-protection silently weaken this.
2. **`rename(2)` is atomic on all target filesystems.** True on local POSIX; weak on NFS, SMB, some FUSE filesystems. Not named in ON.
3. **`fsync(parent_dir_fd)` after rename is assumed when using temp+rename.** ON never mandates this; corpus idiom (WM-013a, WM-026) implies it. New ON-owned files proposed here MUST repeat the pattern.
4. **The daemon's composition-root (PL-005 step 0) runs before any file write.** Assumed but not co-requisite with ON obligations.
5. **Go `recover()` is assumed to catch all panics including SIGSEGV and cgo panics.** It does NOT — cgo-caused segfaults can bypass Go's runtime. PL-018a acknowledges double-panic as accepted limit but ON inherits the assumption silently.
6. **`flock(LOCK_EX|LOCK_NB)` release on process exit is assumed synchronous with the exit.** PL-002a says so; ON inherits it.
7. **Clock monotonicity within a single process is assumed via `CLOCK_MONOTONIC`.** Go exposes this via `runtime.nanotime`; not named in ON-031/033.
8. **The commit-hash check uses a binary-available-on-disk at the moment of check.** An attacker who replaces the binary between hash-check and exec can bypass ON-005 (TOCTOU). Not addressed by ON.
9. **Shared operator LLM budget (ON-042 deferred) is not exercised under crash.** Assumed acceptable at MVH scale.
10. **The operator's terminal will handle CLI exit-code non-zero gracefully.** Some shells report exit codes; many scripts ignore them. ON-001's "structured exit code" is moot if scripts don't check them.
11. **`syscall.Setsid()` at PL-005 step 0 is assumed BEFORE any filesystem write that could race with a handler PGID.** PL v0.4.0 made this explicit; ON inherits the timing.
12. **`harmonik.meta.json` sidecar writes (WM-026) are atomic per WM's discipline.** Cross-referenced but not audited by ON.
13. **Structured-log consumer (operator CLI `harmonik attach`, or external `tail -f`) will tolerate line-level truncation.** Implicit; not named.
14. **Pause-state marker (if ON-030a accepted) is assumed writable — `.harmonik/` is writable.** PL-007 / ON-003 infrastructure-prereq catalog covers startup write-failure but not mid-run write-failure.

---

## 11. Proposed amendments (consolidated)

| ID | Type | Scope | Summary |
|---|---|---|---|
| ON-013a | New | Operator-command supervision | Per-command `recover()` barrier; emit `operator_command_failed` on panic; revert or degrade |
| ON-013b | New | Attach daemon-instance contract | `daemon_instance_id` handshake; typed error on reconnect mismatch |
| ON-013c | New | Operator-command retry semantics | Command idempotency on no-op transitions |
| ON-020a | New | Upgrade-intent marker | Promote `.harmonik/daemon.upgrading` to normative; commit-hash witness; startup-read gate |
| ON-022 (amend) | Amend | Redactor fail-closed | Redactor-panic MUST abort emission; emit `redaction_failed{}`; no payload leak |
| ON-027a | New | Drain atomicity | Steps strictly sequential, no parallelism; crash-between-steps reconcilable |
| ON-027 (amend) | Amend | Add drain step 3a | Intent-log drain before event-bus flush |
| ON-030a | New | Pause-state persistence | `.harmonik/daemon.state` marker; temp+rename+fsync; startup gates on marker |
| ON-033 (amend) | Amend | RTO monotonic measurement | Dual-source timestamps `shutdown_at_ns_since_boot`; boot-transition fallback; SIGKILL case explicitly `rto_undefined` |
| ON-035 (amend) | Amend | Structured-log durability discipline | Error-class per-record fsync; warn/info/debug batched; rotation pattern |
| ON-036a | New | Pending-operator-escalation surface | `harmonik status` reports unmatched `operator_escalation_required` events across restart |
| ON-038 (amend) | Amend | Audit git-authoritative fallback | Reconcile JSONL truncation against git transition-records; fail-closed on JSONL-only |
| ON-040 (amend) | Amend | Silent-hang on drain-forced | Synthesize `agent_warning_silent_hang{reason=drain_forced}` on drain-timeout pre-SIGKILL |
| ON-041a | New | Machine-ceiling drift reconciliation | Periodic ground-truth re-counting; per-daemon `get-agent-count` query; atomic counter overwrite |
| ON-011 (amend) | Amend | State-machine serializability | Mandate single mutex OR equivalent CAS primitive; serializable transitions |

Net additions: 8 new requirements (ON-013a, ON-013b, ON-013c, ON-020a, ON-027a, ON-030a, ON-036a, ON-041a). 6 amendments to existing requirements (ON-011, ON-022, ON-027, ON-033, ON-035, ON-038, ON-040).

---

## 12. Cross-spec implications

Amendments needed in sibling specs (to be coordinated in R2 integration OR tracked as post-R2 cross-spec coordination OQs):

**Process-lifecycle (PL v0.4.0 → v0.4.1):**
- PL-027(iv): promote `.harmonik/daemon.upgrading` marker from "informative" to normative per ON-020a.
- PL-005 step 0: mint `daemon_instance_id` (UUIDv7); write to `.harmonik/daemon.instance-id` via atomic discipline.
- PL-002b: add `daemon_instance_id` field to pidfile payload (line 3).
- PL-009 / PL-011a: `daemon_ready` and `daemon_shutdown` payloads grow `_at_ns_since_boot` fields per ON-033 amendment.
- PL-005 startup: read `.harmonik/daemon.state` and `.harmonik/daemon.upgrading` markers; gate step 9 transition appropriately.
- PL-003a: add JSON-RPC method `get-agent-count` for ON-041a drift reconciliation.
- PL-027 drain: adopt ON-027's step 3a intent-log drain explicitly (already flagged in OQ-ON-006).

**Event-model (EV v0.3.0 → v0.3.1):**
- §8.7 add `operator_command_failed` event (class F).
- §8 add `redaction_failed` event (class F).
- §8.7.3 `daemon_shutdown` payload: confirm class F (OQ-PL-012 resolution) AND add `shutdown_at_ns_since_boot` field.
- §8.7.2 `daemon_ready` payload: add `ready_at_ns_since_boot` field.
- §8.6 or §8.7 add `operator_escalation_cleared` event.

**Beads-integration (BI v0.3.0 → v0.3.1):**
- BI-031 add drain-time mode: during ON-027 step 3a, intent-log resolution is fail-fast rather than retry.

**Handler-contract (HC):**
- §4.6 silent-hang: accept the drain-forced synthesis per ON-040 amendment.

**Reconciliation (RC):**
- §8.11a: name `operator_escalation_cleared` as the clearing event for pending escalations.
- §4.5: align verdict-execution with confirm-verdict/veto-verdict per ON-014 naming.

---

## 13. Strongest single finding

**Scenario 2 — Power loss during pause → paused transition silently loses operator intent.**

Why this is the bug most likely to be missed by a linear reader: it hides behind `§4.a(e)`'s confident declaration that "the operator-control state-machine state is daemon-in-memory; reconstruction on restart is per §4.8.ON-030 via git + Beads." ON-030's reconstruction path walks git and Beads — neither of which records "operator wanted daemon paused." The restart silently resumes dispatch even though the operator's last durable command was "pause."

This is a concrete correctness hole for the ON-019 migration-release ceremony: operator pauses, operator begins `harmonik upgrade`, daemon crashes before the exec marker lands, restart comes up `running`, and the migration release installs the next time `harmonik daemon` is invoked with the new binary on disk. The ON-005 commit-hash gate ONLY fires on the live-daemon upgrade path; it does not fire on a fresh daemon startup. A migration release can therefore install without the operator-pause protection that ON-019 nominally provides.

The fix (ON-030a pause-state marker + ON-020a upgrade-intent marker) is a single pair of atomic-written filesystem markers, read at PL-005 step 0. Small change; big semantic tightening. If reviewed integrators accept only ONE finding from this review, it should be this one.

---

## 14. Reviewer note

ON v0.3.0 is an unusually thorough cross-cutting spec for an MVH foundation set. The R1 integration pass was substantive (21 labeled blocking/important findings applied; see §12 revision history), and most of the v0.3.0 text that this review challenges was added in R1 as a good-faith response to earlier findings. The crash-adversary findings above are NOT evidence of systemic weakness — they are the natural next layer of depth once the "what about SIGKILL at line N" question is asked of a spec this dense.

Priority for R2 integration:

**Must-land (blocking R2 → reviewed):**
- ON-030a (pause-state persistence) — Scenario 2, strongest finding.
- ON-020a (upgrade-intent marker normative) — Scenario 3.
- ON-011 amendment (serializable state machine) — Scenario 7.
- ON-022 amendment (redactor fail-closed) — Scenario 11.
- ON-027a (drain-step atomicity) + ON-027 amendment (step 3a intent-log drain) — Scenarios 1 + 9.

**Should-land (important but mechanically simple):**
- ON-033 amendment (dual-source RTO timestamps) — Scenario 5.
- ON-013a (command-dispatch supervision), ON-013c (command retry semantics) — Scenarios 6 + retry discipline gap.
- ON-040 amendment (drain-forced silent-hang synthesis) — Scenario 10.

**Cross-coord (track as R2-exit OQs):**
- ON-013b (attach daemon-instance), ON-035 amendment (structured-log durability discipline), ON-036a (escalation surface), ON-038 amendment (audit git-authoritative), ON-041a (ceiling drift) — each requires a sibling-spec event/field/method addition that cannot land in ON alone.

The spec is close to crash-hard. One more pass on the pause-state persistence question and the spec moves from "operator-surface correct under nominal faults" to "operator-surface correct under kernel-grade faults."

— Crash-Recovery Adversary, 2026-04-24
