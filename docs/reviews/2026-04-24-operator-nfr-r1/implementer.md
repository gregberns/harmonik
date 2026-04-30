# Round 1 Implementer Review — operator-nfr.md v0.2.0

## Verdict summary

Implementable for the large-surface-area obligations — the exit-code taxonomy, the drain protocol, the upgrade pseudocode, and the state machine are concrete enough to stub in Go tomorrow. It falls short on six load-bearing surfaces:

1. **Pervasive cross-reference drift.** Roughly a third of inter-spec citations name sections that do not exist in their target spec: every `[reconciliation.md §9.x]` (reconciliation uses §4 and §8), every `[event-model.md §3.x]` (EV uses §4, §6, §7, §8), every `[beads-integration.md §10.x]` (BI uses §4, §5, §6), and the `[process-lifecycle.md §8.x]` family (PL uses §4). An implementer cannot follow a reference to a section that does not exist.
2. **Event-type drift against event-model.** ON §6.5 and §7.1 declare `operator_pausing` and `operator_paused` as separate emitted events; event-model.md §8.7.6 registers these as a single merged paired-phase type `operator_pause_status` with a `status ∈ {pausing, paused}` field (per EV §8.9(h)). ON either needs to rewrite its emission list to the merged form or EV needs to split — one of the two is wrong, and both specs currently ship the conflict.
3. **Axes-line undercoverage.** 41 of 51 requirements omit the `Axes:` line. Many of those requirements mutate durable state (ON-022 redaction pre-emission, ON-016 startup-time schema check, ON-022 write rejection, ON-027 shutdown ordering — present; but ON-005 event emission on failure, ON-015/016 schema check on disk, ON-025 provisioning mutation, ON-028 SIGKILL, ON-030 restart reconstruction, ON-032/033 RTO measurement effect, ON-038 audit derivation — several missing). The pattern is inconsistent with template §4.N+1.
4. **Invariants do not name sensors (AR-042 violation).** None of the five `ON-INV-*` blocks cites a sensor. Template §5 selection-test is fine (the invariants are genuinely cross-cutting), but AR-042 is an architectural invariant the spec must satisfy.
5. **Missing §4.a subsystem envelope and `spec-category`.** Front matter lacks `spec-category`; no §4.a envelope. Per AR-052/AR-053, a spec is either `runtime-subsystem` (MUST declare envelope) or `foundation-cross-cutting` (exempt). The current state is under-declared.
6. **Several "obligation" requirements (ON-002, ON-003, ON-004, ON-014, ON-020, ON-041) hand-wave their MVH artifact.** ON-002 is satisfied by §8 (present). ON-014, ON-020, ON-041 are satisfied by the text of ON itself. But ON-003 (startup failure-mode catalog co-owned with PL) and ON-004 (config inventory) name no concrete location; §10.1 says "the §8 taxonomy table satisfies ON-002" but the corresponding sentence for ON-003 is "production of a co-owned startup failure-mode catalog by spec-draft satisfies ON-003" — that catalog artifact does not exist anywhere in the corpus today.

Implementability score: **6 / 10.** The state machine (§7.1), drain/upgrade protocols (§7.2, §7.3), and exit-code taxonomy (§8) are high-quality implementer surfaces. The cross-reference hygiene is, at present, blocker-grade.

## Requirements I attempted

### ON-001 — Operator-observable exit codes are structured — IMPLEMENTABLE

```go
type ExitCode int

const (
    ExitSuccess                   ExitCode = 0
    ExitGenericFailure            ExitCode = 1
    ExitQueueFormatUnsupported    ExitCode = 2
    ExitCheckpointSchemaUnsupported ExitCode = 3
    ExitEventSchemaUnsupported    ExitCode = 4
    ExitPidfileLocked             ExitCode = 5
    ExitSocketBindFailed          ExitCode = 6
    ExitGitBadState               ExitCode = 7
    ExitBeadsUnavailable          ExitCode = 8
    ExitFilesystemUnwritable      ExitCode = 9
    ExitDiskFull                  ExitCode = 10
    ExitDrainTimeoutEscalated     ExitCode = 11
    ExitRTOHardCeilingExceeded    ExitCode = 12
    ExitUpgradeRequiresPaused     ExitCode = 13
    ExitUpgradeHashMismatch       ExitCode = 14
    ExitUpgradeSchemaIncompatible ExitCode = 15
    ExitOperatorControlInvalid    ExitCode = 16
    ExitMultiDaemonTargetMissing  ExitCode = 17
    ExitMachineCeilingExhausted   ExitCode = 18
)

// Dispatch from operator command handler to exit code:
func (cmd *Command) Run(ctx context.Context) ExitCode {
    if err := cmd.Validate(ctx); err != nil {
        return MapErrorToExitCode(err)   // look-up table from §8
    }
    ...
}
```

The §8 table enumerates 18 categories with detection rule, event, and remediation pointer each. I can generate the const block directly from the table and the `MapErrorToExitCode` switch likewise. Stuck point (minor): ON-001 asserts "stable within the N-1 compatibility window" but exit codes are a wire-like contract — the N-1 rule of §4.5.ON-018 is written for "on-disk or wire artifacts"; exit codes are neither. Clarify whether exit codes are under §4.5 or a separate contract.

### ON-002 — Exit-code taxonomy obligation — IMPLEMENTABLE

The §8 table (line 534–554) fulfills the obligation as §10.1 asserts. Each row has exit code, category, detection rule, emitted event, remediation pointer. Usable. Minor: codes 1 (`generic-failure`) is called out as "MUST be rare" but no lint/test rule enforces rarity — is this a reviewer concern or a runtime guard?

### ON-005 — Commit-hash integrity gate — IMPLEMENTABLE

```go
func verifyUpgradeHash(binaryPath, expectedHash string) error {
    actual, err := computeCommitHashFromBinary(binaryPath)
    if err != nil {
        return err  // binary missing / unreadable
    }
    if expectedHash == "" || actual != expectedHash {
        emitEvent(EventOperatorUpgradeRejected, map[string]any{
            "expected_commit_hash": expectedHash,
            "actual_commit_hash":   actual,
            "reason":               "hash_mismatch",
        })
        return ErrUpgradeHashMismatch
    }
    return nil
}
```

Clear contract: fail-closed on mismatch or missing, emit event, stay in `paused`. Stuck points: (a) "binary's source-commit hash" — unclear whether this is the hash of the source tree that compiled to this binary (via a build-time stamp in the binary) or the git HEAD at build time or the hash of the binary itself. §A.3 rationale doesn't disambiguate. Implementer chooses; specs should name the computation. (b) ON-005 says "handler binaries installed via [handler-contract.md §4.10] MUST ALSO carry the commit-hash check" — HC §4.10 is titled "Agent-to-orchestrator trust (MVH)" and does not define a handler-binary install path. Verify the cross-ref actually lands on the commit-hash clause; it doesn't obviously.

### ON-008 — Pause/upgrade respect between-task invariant — IMPLEMENTABLE

```go
func (d *Daemon) Pause(reason PauseReason) error {
    if d.Status() != StatusReady {
        if d.Status() == StatusReconciling {
            d.enqueuePendingPause(reason)   // ON-010 carve-out
            return nil
        }
        return ErrOperatorControlInvalidState
    }
    d.setState(StatePausing)
    emit("operator_pause_status", map[string]any{
        "status":       "pausing",
        "pause_reason": string(reason),
    })
    // wait for in-flight runs to reach next checkpoint:
    d.runTracker.OnAllRunsCheckpointed(func() {
        d.setState(StatePaused)
        emit("operator_pause_status", map[string]any{"status": "paused", ...})
    })
    return nil
}
```

Writable. Two frictions: (a) the spec uses split `operator_pausing` / `operator_paused` events (§6.5, §7.1), event-model registers the merged `operator_pause_status` (EV §8.7.6). I had to pick one — went with the merged form because EV is `reviewed` and ON is `draft`. Resolve at integration. (b) "durable checkpoint" per `[execution-model.md §4.5]` lands correctly (EM §4.5 is the cadence section) — this one is right.

### ON-011 — Operator-control state machine — IMPLEMENTABLE

Direct translation of §7.1 table:

```go
type StateTransition struct {
    From  State
    Event Event
    Guard func(*Daemon) bool
    To    State
    Emit  EventType
}

var transitions = []StateTransition{
    {StateRunning, EventPause, guardReadyNotReconciling, StatePausing, EventTypePauseStatus},
    {StateRunning, EventImprovementTrigger, guardImprovementPolicy, StateImprovementPausing, EventTypePauseStatus},
    // ... 13 rows, one per §7.1 entry
    {StateAny, EventStopImmediate, guardAlways, StateStopped, EventTypeOperatorStopped},
}
```

Works with two caveats. Caveat 1: `StateAny` source in the "any → stopped on stop-immediate" row needs discipline in lookup — the table shape assumes `From` is a concrete state; `any` is a meta-state. Implementer chooses encoding (sentinel value, fallback branch). Caveat 2: the table does not distinguish `improvement-pausing` transitions from `pausing` transitions on their outbound edges — both go to `resuming` but `improvement-paused` resumes via "improvement loop completes" (no operator action) whereas `paused` resumes via operator `resume`. The table rows are present but the guard column is weak ("none"). Pseudocode helps; guard naming does not.

### ON-013 — Operator-control events per state transition — PARTIALLY

Stuck on the event-type drift with event-model (ON-013 names six emitted types: `operator_pausing`, `operator_paused`, `operator_resuming`, `operator_stopped`, `operator_upgrading`, `operator_upgrade_completed`). Event-model.md §8.7 registers `operator_pause_status` as a merged paired-phase type (§8.9(h) asserts paired-phase lifecycles MUST NOT split). ON-013 emits two types that conflict with an event-model MUST NOT. This is a direct spec contradiction.

Axes line on ON-013 carries `idempotency=non-idempotent` — correct (event emission is non-idempotent). Good.

### ON-015–ON-017 — Queue-format compatibility — PARTIALLY

```go
func checkBeadsSchemaOnStartup() error {
    beadsV := br.GetSchemaVersion()          // via br CLI adapter
    overlayV := readOverlaySchemaVersion()   // harmonik's half
    supported := binaryMetadata().SupportedSchemas
    if !contains(supported.Beads, beadsV) || !contains(supported.Overlay, overlayV) {
        return ErrQueueFormatUnsupported
    }
    return nil
}
```

Stuck points:
1. ON-015 asserts "the overlay schema compat" is made of three artifacts (trailers in checkpoint commits, bead-ID references in events, session-log bead-ID metadata). None of those carry an explicit overlay-version number as a top-level field. The `Harmonik-Schema-Version` trailer is per-commit; there is no "harmonik overlay schema version" as a singleton. Where does the reader read `overlayV` from? Implementer must invent.
2. ON-016 refers to `[beads-integration.md §10.1–§10.3]`. Beads-integration.md §10 is Conformance; §10.1–§10.3 are `Conformance profiles`, `Test-surface obligations`, `Excluded conformance claims`. Intended targets are BI §4.1, §4.2, §4.3 (Beads selection / `br` CLI / Beads-managed data). Drift.
3. ON-017 "Beads pre-1.0 breakage is absorbed, not forked" references `[beads-integration.md §10.8]` — should be BI §4.8 (`Version-pin + adapter layer`). Drift.

### ON-020 — `harmonik upgrade` contract — IMPLEMENTABLE

§7.3 pseudocode is exactly the five-sub-obligation shape ON-020 names. I can compile against it. Stuck point is the one from ON-005: the commit-hash computation procedure is not named. And: `exec_replace(new_binary_path)` is the atomicity linchpin for the whole contract — the daemon hands off the listening socket to the new process. Spec says "same socket path; clients retry per §4.6.ON-020". What mechanism preserves the socket FD across exec? Unix-socket FD inheritance requires `SOCK_CLOEXEC=0` + env-var threading or `systemd`-style FD-passing. Decide and name.

### ON-027 / ON-028 / ON-029 — Graceful-shutdown ordering — IMPLEMENTABLE

Seven ordered steps; each observable; exit codes mapped. §7.2 `drain_graceful` pseudocode is close to directly compilable. Stuck points:
1. Step 5 — "memory layer flushes indexing" — memory layer is not one of the foundation 10 specs and is not in `depends-on`. Is it a post-MVH subsystem? If so, the step should be conditional-on-presence.
2. Step 6 — "cleans up incomplete adze setups per [workspace-model.md §5.1]" — WM §5.1 is `Worktree primitive` (line 135 in WM); the cleanup-incomplete-adze rule lands at WM §4.3 Lease model or §4.4 Lifecycle states, not §5.1. Drift.
3. Step 7 — returns "the exit code for `drain-timeout-escalated`" — code 11 in §8. Works.

ON-028 is clean (skip steps 2–3; rely on next-startup reconciliation). ON-029 (drain timeout config) is declarative only — storage location (via ON-004 config inventory) is an OQ per the spec's own OQ-ON-001.

### ON-030–ON-033 — Restart RTO — PARTIALLY

ON-031 leaves the RTO target as "X seconds" and defers to ON-032 for criteria. This works as a contract; the implementation cares about the hard ceiling, not the nominal.

```go
func measureRestartRTO() time.Duration {
    sigTermTime := readCrashTimestamp()          // from /proc or pidfile mtime
    readyEventTime := firstReadyEventTimestamp() // from event log
    return readyEventTime.Sub(sigTermTime)
}

// enforce hard ceiling:
if rto := measureRestartRTO(); rto > 300*time.Second {
    emit("daemon_degraded", map[string]any{"reason": "rto_breach", "rto_seconds": rto.Seconds()})
}
```

Stuck points:
1. **No named sensor.** ON-031's "X seconds" begs a test method; ON-032 criterion 1 says "95th percentile" — an aggregate metric, not a single-run measurement. The spec does not say who computes the p95 (the daemon itself across restarts? A test harness? A regression gate in `make check-full`?). Testing.md not yet landing makes this unverifiable today.
2. **Cross-ref** to `[process-lifecycle.md §8.2]` for the ready event — PL §8.2 is Reverse dependencies; intended is PL §4.3 (Ready-state transition) which owns PL-009. Drift.
3. ON-033's "SIGTERM (or daemon crash timestamp recorded by the OS)" — OSes do not consistently record daemon crash timestamps; what's the reference? An implementer will use pidfile mtime or systemd journal, and the spec should name one.

### ON-038 — Audit records are a subset of traces — PARTIALLY

```go
func deriveAuditRecords(run *Run) []AuditRecord {
    var out []AuditRecord
    for _, tx := range run.Transitions {
        if isPrivilegedRole(tx.ActorRole) && affectsPolicyOrBudget(tx.ChosenAction) {
            out = append(out, AuditRecord{...})
        }
    }
    return out
}
```

Stuck points: (a) "transition records per [execution-model.md §4.4]" — EM §4.4 is `Checkpoint contract`; transition records are defined there via the sibling-file pattern. Correct. (b) "actor_role is in a privileged role (per [architecture.md §4.8])" — AR §4.8 is about `spec-category`, not role taxonomy. The role taxonomy is actually at AR §4.8 Role taxonomy — wait, let me check — the architecture.md scan showed AR-052 at §4.8 (line 78). Drift.

> Correction on re-read: arch §4.8 does contain role taxonomy in a later block; see §9.1 cross-ref "[architecture.md §4.8] — role taxonomy". Citation is probably correct and §4.8 covers both. Flag for author to verify.

(c) "chosen_action affected policy, role permissions, or budget" — is `affectsPolicyOrBudget` a runtime predicate the implementer writes, or a compile-time classification on the `chosen_action` descriptor? Spec is silent.

### ON-041 — Multi-daemon commands obligation — PARTIALLY

```go
// harmonik list
func commandList(ctx context.Context) []DaemonEntry {
    entries := []DaemonEntry{}
    for _, proj := range discoverProjects() {            // how?
        pid := readPidfile(proj.Root + "/.harmonik/daemon.pid")
        if alive(pid) {
            sock := proj.Root + "/.harmonik/daemon.sock"
            entries = append(entries, DaemonEntry{ProjectPath: proj.Root, PID: pid, Socket: sock, Status: queryStatus(sock)})
        }
    }
    return entries
}
```

Stuck point: `discoverProjects()` has no declared mechanism. Scan `$HOME`? Walk `/` for `.harmonik/daemon.pid`? Read a machine-level registry? OQ-ON-003 acknowledges the machine-ceiling coordinator is undecided; discovery is a related gap not acknowledged.

---

## Exit-code taxonomy (§4.1 + §8)

Is it enumerated enough to stub a Go `const` block + dispatch? **Yes.** 18 codes; every row has a category, detection rule, emitted event, and remediation pointer. An implementer can:

1. Generate the `const` block directly from column 1.
2. Generate the `MapErrorToExitCode` switch from columns 2–3.
3. Generate the remediation table for `harmonik status` from columns 4–5.

Minor gaps that the author should close before `reviewed`:

- **Rare-code policy for 1.** Code 1 (`generic-failure`) is called "MUST be rare; presence in a release indicates missing taxonomy entry." This is a process claim, not a runtime claim. Is there a static check that fails a release if code 1 is reachable from any call-site? The spec should say so or drop the "MUST be rare" line.
- **Code 17 (multi-daemon-target-missing) has no emitted event** — column shows `—`. Every other non-zero code has an event. Is that intentional? If so, it's the one exception and should be flagged in the INFORMATIVE note.
- **Code numbering** is dense (1–18, no gaps). Given the N-1 stability obligation, future additions will need to append, not insert. A sentence stating "new codes MUST append at the next unused integer" would lock the monotonicity.

Does every exit code have a definition and a caller-side handling rule? **Definitions: yes** (each row). **Caller-side handling rule: weak.** The remediation column tells operators what to do; it does NOT tell the CLI caller what to do. For example, a `harmonik attach` session observing exit 12 (`rto-hard-ceiling-exceeded`) — does the session retry, reconnect-after-a-delay, or propagate to the terminal? Callers need a table row.

## Operator-control semantics (§4.3): BETWEEN-tasks discipline

**Mechanical boundary.** ON-008 names it: a pause transitions `running` → `pausing` at request time, but `pausing` → `paused` only when "each in-flight run" reaches its "next durable checkpoint per [execution-model.md §4.5]." The boundary is the durable-transition checkpoint defined in EM-023a. That EM requirement is concrete enough (and `reviewed`) to count as the backstop for the between-task invariant.

**Drain protocol (§7.2) concreteness.** The 7-step sequence is writable directly. Key concern: step 2 `wait_for_runs_to_checkpoint(timeout.step_2)` — how is the per-step timeout apportioned from a single `drain_timeout` knob (ON-029)? Is it N equal slices, one slice per step, or a single global clock consumed across steps? Spec does not say.

**Writing the FSM from §7.1.** Yes, directly. 13 rows; each row compiles to one `StateTransition` struct. The two ambiguities noted above (meta-state `any`, weak guard naming on improvement-* flows) are surmountable but worth tightening.

## Queue-format compatibility (§4.4)

**Contract.** ON-015: "the union of (a) Beads schema compat (managed upstream) AND (b) harmonik's overlay schema compat." Both halves N-1 readable.

**Compatibility-window rule.** ON-018 asserts every versioned artifact holds N-1. Queue overlay is one of those artifacts. The mapping to BI is:

- BI §4.9 (version-pin + adapter layer) — harmonik version-pins Beads; BI-owned contract.
- EV §4.7 (Schema versioning) — events carry per-type schema versions with N-1 compat per EV-029.
- ON §4.5.ON-018 — the cross-cutting N-1 rule.

**Gap.** The "overlay schema version" as a singleton field is not declared anywhere. ON-015 names three distinct artifacts (trailers, event bead-IDs, session-log metadata) but there is no single version number that covers their union. Either declare an overlay-version integer per-commit (or per-session-log) OR explicitly name that the overlay version is the max of the individual artifact versions. Without this, ON-016 "check both the Beads SQLite schema version and harmonik's overlay schema version" has no target for `overlayV`.

## `harmonik upgrade` contract (§4.6)

**ON-side protocol (operator-facing).** Concrete. §4.6 names five sub-obligations; §7.3 pseudocode traces every sub-obligation to a branch. The operator surface is `harmonik upgrade <binary_path> --expected-hash <hash>` (implied; flag names are out-of-scope per §2.2).

**PL-side protocol (daemon-facing).** PL-027 delegates entirely to ON. PL's only obligation is that its startup (PL-005) and shutdown (PL-011) sequences be consistent with whatever ON produces. The split is honest; the drift is that PL references ON §7.3 and §7.5, and ON is currently §4.3, §4.6 — both drafts need to land the same numbering at finalize.

**ON-side stub.**

```go
type UpgradeRequest struct {
    NewBinaryPath string
    ExpectedHash  string
}

func (d *Daemon) Upgrade(ctx context.Context, req UpgradeRequest) ExitCode {
    if d.Status() != StatePaused {
        emit(EventTypeOperatorUpgradeRejected, map[string]any{"reason": "not_paused"})
        return ExitUpgradeRequiresPaused
    }
    actualHash, err := computeBinaryCommitHash(req.NewBinaryPath)
    if err != nil {
        return ExitGenericFailure   // unclear — spec doesn't map
    }
    if actualHash != req.ExpectedHash {
        emit(EventTypeOperatorUpgradeRejected, map[string]any{
            "reason": "hash_mismatch", "expected": req.ExpectedHash, "actual": actualHash,
        })
        return ExitUpgradeHashMismatch
    }
    newSchemas, err := readSupportedSchemas(req.NewBinaryPath)
    if err != nil {
        return ExitGenericFailure   // also unclear
    }
    if !compatible(d.onDiskSchemas(), newSchemas) {
        emit(EventTypeOperatorUpgradeRejected, map[string]any{"reason": "schema_incompatible"})
        return ExitUpgradeSchemaIncompatible
    }
    d.setState(StateUpgrading)
    emit(EventTypeOperatorUpgrading, map[string]any{"expected_commit_hash": req.ExpectedHash})
    if err := execReplace(req.NewBinaryPath); err != nil {
        // exec failed — daemon is in limbo; state per ON-021 says recoverable
        return ExitGenericFailure   // also unclear
    }
    unreachable()  // after exec
}
```

Gaps: (a) several error paths map to `generic-failure` for lack of a declared code; (b) `execReplace` primitive is not named at the Go level — `syscall.Exec` on Unix passes the current FDs through, but the socket listener FD needs `SOCK_CLOEXEC=0`. Implementer must invent.

## RTO and observability envelope (§4.8, §4.9)

**RTO as a testable metric with a named sensor.** Named? No. Testable? In principle, yes — SIGTERM → `daemon_ready` event timestamp delta is a single clock read each side. But who measures and where does the measurement surface? The spec mentions "the daemon MUST enter `degraded` reporting `reconciling` with progress markers" on hard-ceiling breach, which implies the daemon self-measures. The p95 nominal target ("Criterion 1") is a distributed-sample metric that the daemon alone cannot compute (it needs observations across restarts).

Suggested fix: add an `ON-033a` — "A testing.md-layer benchmark harness MUST sample SIGTERM-to-`daemon_ready` across N representative restarts and enforce p95 ≤ 30s; a runtime sensor MUST emit `daemon_degraded` on single-restart 300s breach." Split the sensors by layer.

**Observability envelope as a declared set of required signals.** ON-034 through ON-040 enumerate: typed events (EV §8), structured logs (EV §6.2), health-check interface (inline in ON-036), liveness heartbeats (inline in ON-037), audit records (EM §4.4 transition records, privileged-role subset), mechanism-tagging (AR §4.1, §4.2), silent-hang detection (HC §4.6). This is usable as a per-subsystem checklist. Minor gap: ON-036 asserts `health_status ∈ {OK, degraded, failed}` but does not say whether the health check is an in-process Go interface method or a wire-exposed surface (HTTP? socket?). §6 lists it inline with no wire form. Implementer assumes Go interface; should be explicit.

## Resource budgets (§4.11)

Budget categories: ON-045 names "token, wall-clock, iterations" and defers the specification to `[control-points.md §6.9]`. CP §6.9 does not exist — control-points §6 is Schemas; §6.5 is co-owned events; §6.9 is vacant. Budget semantics are at CP §4.5 (`Budget semantics`).

Missing from ON-045: memory, CPU, disk, handle count. The review prompt asked if these budget categories are declared with per-category caps. Answer: **only token, wall-clock, iterations are named; memory / CPU / disk / handle count are not budgetable in the MVH contract.** That's either by design (acceptable; add a sentence saying so) or a gap. §2.2 out-of-scope does not list them.

Per-category default caps: none specified at the ON layer. ON-046 says `budget_warning`, `budget_exhausted`, `budget_accrual` events are operator-observable. Defaults are expected to land via ON-004's config inventory (not yet written).

## Protocol-side concreteness

**§7.2 drain pseudocode.** Traceable to defined endpoints? Mostly. `stop_dispatch_loop`, `wait_for_runs_to_checkpoint`, `wait_for_handler_subprocess_exit`, `flush_event_bus` all have natural Go-level counterparts. `flush_memory_indexing` names a subsystem not in the foundation 10 (memory layer) and should be made conditional. `unlock_leased_workspaces` lands in WM §4.3 Lease model, not §5.1 (citation is drifted). `any_step_exceeded_its_bound` is a compound predicate; implementer has to decide the compositional semantics of "any step exceeds its bound" when the overall timeout is a single knob.

**§7.3 upgrade pseudocode.** Concrete end-to-end modulo the `exec_replace` primitive. Every branch point maps to a requirement (ON-008, ON-005, ON-019, ON-021, ON-020). Good tracing. Branch point missing: what happens if `compute_commit_hash(new_binary_path)` fails (binary missing, not-an-executable, unreadable)? No branch; spec assumes the hash is computable. Add a branch or an explicit precondition.

## Cross-spec reach-ins

Requirements that silently consume another spec without declaring it in `depends-on`, or consume a section that does not exist:

1. **ON-030 consumes `[beads-integration.md §10.8]`.** Beads §10.8 does not exist; §10 is Conformance. Intended: BI §4.8 Version-pin + adapter layer. Drift.
2. **ON-015 consumes `[beads-integration.md §10.1–§10.3, §10.6, §10.8]`.** None exist. Intended: BI §4.1, §4.2, §4.3, §4.6, §4.8. Drift.
3. **ON-003, ON-016, ON-031 and multiple cross-refs** cite `[process-lifecycle.md §8.2]` and `§8.3`, `§8.4`. PL §8 is Error and failure taxonomy; §8.2 does not exist. Intended: PL §4.2, §4.10 (or PL-005/PL-009/etc. by ID). Drift — symmetric with PL's own ON §7.x drift.
4. **ON-007, ON-010, ON-014 and others** cite `[reconciliation.md §9.x]`. Reconciliation §9 is Cross-references; §9.1–§9.5b do not exist. Intended: RC §4.1 (reconciliation-as-workflow), §4.2 (dispatch), §4.4 (investigator), §4.5 (verdict), §8.x (categories). All drifted.
5. **ON-034, ON-035, ON-038 and multiple cross-refs** cite `[event-model.md §3.1, §3.2, §3.4, §3.5, §3.6, §3.7, §3.8]`. EV §3 is Glossary. Intended: EV §4.1 (Envelope), §4.4 (durability / fsync), §4.5 (replay), §4.7 (schema versioning), §6.2 (JSONL format), §7.2 (overflow/dead-letter), §8 (taxonomy). All drifted.
6. **ON-025 consumes `[control-points.md §6.5]`** (for policy schema) and `[control-points.md §6.11]` (for skill declaration). CP §6.5 is co-owned events, not policy schema; §6.11 does not exist. Intended: CP §6.3 Policy YAML and CP §4.11 Skill declaration.
7. **ON-045 consumes `[control-points.md §6.9]`** for budget. CP §6.9 does not exist; intended CP §4.5 Budget semantics.
8. **ON-038 consumes `[execution-model.md §4.4]`** for transition records and `[architecture.md §4.8]` for role taxonomy. EM §4.4 is correct. AR §4.8 — needs author verification; see note above.
9. **ON-007 consumes `[execution-model.md §3 run]`** (not `§3.X`, just `§3 run`). EM §3 is Glossary and does define `run`. That's correct.

The `depends-on` front-matter list is right (architecture, event-model, execution-model, handler-contract, control-points, process-lifecycle, reconciliation, beads-integration). The in-body citations are where drift lives.

## Template conformance

- **AR-042 (invariants MUST name their sensor).** FAIL. Five `ON-INV-*` blocks, none name a sensor. Proposed sensors: ON-INV-001 → corpus-wide lint that walks every versioned artifact's schema-version field + N-1 reader test; ON-INV-002 → review-agent scenario test; ON-INV-003 → lint pass asserting no `Secret`-typed field in any payload schema (already obligated by ON-023 — name it as the sensor); ON-INV-004 → scenario test over the §7.1 state machine + reconciliation-carve-out carve-out; ON-INV-005 → the RTO benchmark of §10.2.
- **Tags and Axes coverage.** All 51 requirements carry a `Tags:` line (good; all `mechanism`). 10 carry an `Axes:` line. Undercounting: ON-016 (state mutation on startup, external I/O) carries Axes — correct; ON-005 event emission on failure does NOT carry Axes and probably should (non-idempotent side effect: event emission to durable sink); ON-022 secret-redaction enforcement does (good); ON-023 compile-time check probably is declaration-only (fine); ON-027 has Axes (good); ON-028 has Axes (good); ON-031 has Axes (good); ON-037 has Axes (good); ON-013 has Axes (good). Suspected misses: ON-005 (event emission on failure), ON-025 (provisioning mutation), ON-030 (git walk + Beads query are I/O), ON-032 (measurement read not mutation, likely baseline-exempt), ON-046 (budget-threshold event emission is non-idempotent). Reviewer should sweep.
- **§4.a envelope + `spec-category` per AR-052/AR-053.** The spec's front-matter `spec-category` is **missing**. AR-052 requires it. AR-052's examples list operator-nfr as `foundation-cross-cutting`, so envelope is exempt — but the front-matter declaration is not exempt. Add `spec-category: foundation-cross-cutting`.
- **Spec-shape.** `requirements-first`. Fine; this spec is not a taxonomy even though §8 is a taxonomy (the bulk of normative content is obligations, not enumerations).
- **Section completeness.** §§1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, A all present. §9.2 INFORMATIVE placeholder is the template-permitted form. §10 conformance is present with profiles + test obligations + excluded claims. §11 open questions has 5. §12 revision history has 2 rows.
- **ID allocation.** ON-001 through ON-046 (46 requirements, source-ordered, no gaps). Invariants ON-INV-001 through ON-INV-005. OQs OQ-ON-001 through OQ-ON-005. All within the `specs/_registry.yaml` prefix reservation (`ON`).
- **Cross-reference form.** Every citation is in `[spec.md §N.N]` form — syntactic correctness good. Target validity — see the reach-ins section above.

## Implementability score

**6 / 10.** Surface concreteness — exit codes, state machine, drain, upgrade — is high (8/10). Cross-reference hygiene is low (3/10). Invariant sensor-naming is missing (0/10 for AR-042 discipline). Front-matter missing `spec-category` (easy fix, 1 line). Event-type conflict with event-model is spec-level contradiction (blocker for finalize, not for draft).

The recommendation shape for the author:

1. **Blocker-grade.** Fix the event-type contradiction with event-model.md §8.7.6 — either split the paired-phase event type in EV (and argue against §8.9(h)) or rewrite ON §6.5 + §7.1 + ON-013 to emit `operator_pause_status` with a status field. Probably the latter.
2. **Blocker-grade.** Sweep all in-body cross-references. There are roughly 30 drifted section citations. The `v0.2.0` revision-history row noted a prior `§1.N → §4.N` architecture.md cleanup; the same cleanup is unfinished for event-model, process-lifecycle, reconciliation, beads-integration, and control-points targets. This is not cosmetic — an implementer following a cite to a non-existent section has to guess the intended target every time.
3. **Should-fix.** Add `spec-category: foundation-cross-cutting` to front matter.
4. **Should-fix.** Name a sensor for each of the five ON-INV-* invariants.
5. **Should-fix.** Axes-line sweep.
6. **Should-fix.** Declare an explicit "overlay schema version" field (or the "max of artifact versions" rule) to give ON-016 a read target.
7. **Should-fix.** Resolve the per-step vs global drain-timeout apportionment (ON-029 + §7.2).
8. **Should-fix.** Name the commit-hash computation procedure for ON-005.
9. **Nice-to-have.** Exit-code caller-side handling rule in §8 (how does `harmonik attach` observe and respond to each code).
10. **Nice-to-have.** Name the `exec_replace` Unix mechanism for ON-020 (FD inheritance / SOCK_CLOEXEC discipline).

## Affirmations

1. **§7.1 state machine table** is the best format for the operator-control semantics: 13 rows covering every state transition with guard and emission. Directly writable as a `[]StateTransition` table in Go. The improvement-pause subtype is a lightweight addition (two extra rows) rather than a new state class — good compositional discipline.
2. **§8 exit-code taxonomy** is the exemplar the template §8 asks for: every row has detection rule + event + remediation pointer. Even code 1 `generic-failure` is called out as "MUST be rare" with a release-quality signal. The 18-code surface is small enough to audit, large enough to cover the named failure modes.
3. **§4.5 N-1 compatibility window** is stated once, cross-cutting. The invariant ON-INV-001 holds the jointness claim correctly ("every versioned artifact simultaneously"), so the spec-drafter cannot back-door a single-artifact relaxation. Good cross-cutting discipline.
4. **§4.10.ON-042 "deferred ≠ dismissed" posture.** The spec explicitly names the three multi-tenancy concerns (shared LLM budgets, shared operator identity, shared skill registries) that per-project daemon isolation does NOT solve, and says so. This prevents a later reader from assuming per-project isolation is a complete answer. Honest architectural framing.
5. **§4.3.ON-009 anti-requirement.** ON-009 explicitly rejects `pause --immediate` and `upgrade --immediate` as violations of ON-008. Stating the anti-requirement inline ("proposals to add X MUST be rejected") protects the between-task invariant from future erosion better than a rationale appendix would.
6. **§A.3 rationale on locked decisions.** The "why N-1 and not N-2" and "why 300s is non-negotiable" paragraphs trace back to locked decisions #10 and #12 with evidence, not assertion. A later amendment has concrete ground to argue against.
