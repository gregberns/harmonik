# C2 design — Process/session lifecycle

## Current state

Handler and process specs own the necessary edges, but single, review, and DOT
assemble them separately. Owned registrations/subscriptions are not retained
by production callers. Cleanup ordering and wait bounds vary. Production
watcher/WaitOwner shapes drift from the reviewed HC/PL target.

## Target state

RAC will define a mode-neutral `PhaseLifecycle` capability and one
constructor-complete phase request:

```text
PhaseRequest:
  sealed Run/phase/generation identity
  LaunchSpec
  explicit hosting regime
  readiness and deadline policy
  shutdown disposition
  narrow InputPort requirement

PhaseObservation:
  launch/ready result
  process-exit observation
  handler Outcome observation
  watcher result
  cancel reason
  cleanup result
  recovery disposition
```

The phase owner retains exactly one Session, watcher/join (where applicable),
`SessionRegistration`, every owned event subscription, `InputPort`,
heartbeat/watchdog joins, cancel function, and ordered cleanup scope.

### Lifecycle sequence

```text
construct -> register -> launch -> observe-ready -> accept-input
-> observe-completion | cancel
-> close-input/hook-window -> join-auxiliaries
-> terminate-and-reap | detach-for-adoption
-> close
```

Every edge cites its HC or PL owner. Input stops at HC-069 `InputPort`.
`detach-for-adoption` is legal only for the explicit single-mode independent
session variant; review and DOT must terminate and join.

The exact Ack shape is intentionally not stated here: HC-070 and RSM-027
contradict each other. `INPUT-ACK-CONTRACT-01` in C6 is a blocking coordinated
owner-spec amendment. RAC spec drafting cannot begin until that reviewed
decision exists; PhaseLifecycle then consumes it unchanged.

Cleanup is a typed sequence, not arbitrary callbacks. Session observation and
raw OS wait/reap remain separate: phase code calls the normative Session
surface and never receives `*exec.Cmd`.

### Drift treatment

RAC treats HC/PL as target authority. The current separate progress watcher and
`WaitOwner`, current Handler/Session signatures, watcherless substrate
sentinel, type-asserted input, and unbounded DOT waits are named
nonconforming migration states.

The executable route is:

- PS-01 owns the pure lifecycle protocol at
  `internal/runloop/phase/phase.go` and focused tests.
- `PS-HCPL-CONFORMANCE-01` adds a compiling, one-way compatibility bridge.
  `handler.ContractLaunchMapper` has the exact shape
  `func(context.Context, *handlercontract.LaunchSpec) (handler.LaunchSpec,
  error)`. `handler.NewContractHandlerAdapter(legacy handler.Handler,
  agentType core.AgentType, mapSpec ContractLaunchMapper)
  handlercontract.Handler` returns a `ContractHandlerAdapter`. Its target
  `AgentType() string` returns `string(agentType)` unchanged; empty or invalid
  agent type makes `NewContractHandlerAdapter` panic as a composition defect,
  matching its no-error constructor signature.
  Its target
  `Launch(ctx, *handlercontract.LaunchSpec)
  (handlercontract.Session, error)` maps the spec, requires the mapped
  `handler.LaunchSpec.Substrate` to be nil, invokes the one legacy launch, and
  returns
  `NewContractSessionAdapter(legacy handler.Session,
  watcher *handlercontract.Watcher) handlercontract.Session`.
  `ContractSessionAdapter` is the conforming Session object: `ID` delegates to
  `Watcher.SessionID`; `SendInput` and `Kill` delegate to the wrapped legacy
  Session; `Attach` opens the path captured by `Watcher.LogLocation`;
  `LogLocation` returns that captured value; and `Wait` waits for both cached
  process completion and watcher completion, then returns the exact
  `core.Outcome` captured by `Watcher.Outcome`, or a typed structural error
  when no outcome was delivered. Thus the adapter wraps exactly one legacy
  Handler and its exactly-one legacy Session/Watcher pair; it never launches
  or watches a second process.
- Watcherless substrate hosting remains a valid legacy result and is not
  misrepresented as an HC Session. Production
  `(*handler).launchViaSubstrate` continues to return `(Session, nil, nil)`
  when `SubstrateSession.Stdout()` is nil, and the four legacy mode callers
  continue to pair that Session with `HookSessionStore.WaitForOutcome`.
  The contract adapter rejects every mapped non-nil `Substrate` with a typed
  structural error **before** invoking `legacy.Launch`; therefore it cannot
  launch a watcherless process and then dereference or fabricate watcher ID,
  outcome, or log-location state. PS-02 later owns the conforming hook-session
  completion adapter. This restriction leaves `NewContractHandlerAdapter`
  constructible for direct/watcher-backed launches and leaves the normal
  legacy nil-watcher path unchanged.
- The compatibility boundary preserves `handler.NewHandler`,
  `handler.Handler.Launch(ctx, handler.LaunchSpec)
  (handler.Session, *handlercontract.Watcher, error)`, and
  `handler.Session.Wait(ctx) error` for the unchanged callers
  `workloop.go` `beadRunOne`, `reviewloop.go` `runReviewLoop`,
  `dot_cascade_core.go` `dispatchDotAgenticNode`, and `dot_gate.go`
  `executeCognitionGate`. New PS consumers receive only
  `handlercontract.Handler` from `NewContractHandlerAdapter`; the indexed
  WL/BR/RL/DOT migrations later delete the legacy surface after their callers
  move.
- Raw wait ownership is singular during the intermediate.
  The selected private constructor is exactly:
  `newSessionWithIDs(ctx context.Context, cmd *exec.Cmd, sessID, runID string,
  wireTap io.WriteCloser) (Session, *sessionReapHandoff, error)`.
  Passing nil `wireTap` is valid. From entry the constructor owns a non-nil
  pre-opened tap and closes it on every pipe/setup/`cmd.Start` failure.
  `cmd.StdinPipe` is retired. Before `cmd.Start`, the constructor creates all
  three streams with direct `os.Pipe`: it assigns the stdin child reader to
  `cmd.Stdin`, retains the stdin parent writer, and assigns the stdout/stderr
  child writers to `cmd.Stdout`/`cmd.Stderr` while retaining their parent
  readers. Every one of those six file handles and the optional tap receives
  exactly one `ownedCloser`/`sync.Once` guard at creation. Any later pre-Start
  pipe/setup failure, or `cmd.Start` failure, closes every acquired handle
  through those guards; no Session, raw-wait owner, or goroutine exists on
  that path. Because none of the `exec.Cmd` pipe-helper methods is used,
  `exec.Cmd.Wait` owns and closes none of these handles.
- After successful `cmd.Start`, the constructor's first operation is to
  construct the concrete `*session`, `WaitOwner`, and `sessionReapHandoff` in
  atomic state `launch_owned`; no fallible close, lifecycle transition,
  callback, or returned interface access precedes that assignment. The
  handoff initially owns the stdin parent-writer guard, stdout/stderr
  parent-reader guards, the three parent-held copies of child ends, and the
  optional tap guard. The concrete Session receives that same stdin
  parent-writer guard—not a second wrapper: `SendInput` borrows its writer and
  `CloseStdin` closes through the shared guard. Thus Session API use and
  lifecycle cleanup have one close authority, while `exec.Cmd.Wait` has none.
- Before either fallible constructor operation, the constructor calls the
  no-error `sessionReapHandoff.startIO()`. It replaces
  `bridgeStdout(*os.File) *io.PipeReader` with
  `newStdoutBridge(source *ownedCloser) *stdoutBridge`.
  Under the handoff mutex, `startIO` removes the stdout-source
  `*ownedCloser` from the handoff and gives that same close guard—not a second
  wrapper—to `stdoutBridge`; this is one atomic ownership transfer. The bridge
  exclusively owns the source, pipe reader/writer, and a
  `Done() <-chan struct{}` closed by its sole copy goroutine. Its
  `AbortAndWait()` closes the pipe reader, pipe writer, and source through
  those once-guards before joining `Done`, so copy unblocks whether reading
  the OS pipe or writing an unread `io.Pipe`. `CloseAndWait()` is safe for
  every Watcher exit, not only an EOF-drained exit: after the Watcher has
  stopped consuming and the raw reap has returned, it first closes the pipe
  reader and source through those same once-guards, thereby unblocking a copy
  stalled on either an unread `io.Pipe` write or an OS-pipe read; it then joins
  `Done` and closes the remaining pipe-writer guard. Closing an already
  EOF-drained bridge follows the same idempotent path.
  `startIO` installs `stdoutBridge.Reader()` on the concrete Session.
- By contrast, the stderr read-end `ownedCloser` remains in the
  handoff/Session for the entire lifecycle; the stderr drain receives only its
  borrowed `io.Reader` view and never closes it. `startIO` starts that sole
  drain and records its `stderrDone` together with `stdoutBridge.Done` before
  returning. It has no error return and performs no user callback or lifecycle
  transition, so both auxiliary goroutines have explicit owners before the
  next fallible instruction.
- The constructor then closes the parent-held child stdin reader and
  stdout/stderr writers through
  `sessionConstructionOps.closeParentChildEnds
  func(*sessionReapHandoff) error` and performs
  `Spawning -> Initializing` through
  `sessionConstructionOps.transitionToInitializing
  func(*hclifecycle.Machine) error`. Either error calls
  `handoff.abort` before returning. `closeParentChildEnds` always attempts, in
  order, the stdin child-reader guard, stdout child-writer guard, and stderr
  child-writer guard and returns their joined close error; every attempt
  consumes its once-guard even when `Close` reports an error. Therefore its
  failure cut reaches `abort` only after all three parent-held child ends have
  been retired, and abort cannot close one twice. Successful close marks all
  three child ends released in the handoff; successful transition returns
  `(Session, handoff, nil)`. It does not start `go s.runWait(ctx)`.
- Production uses
  `newSessionWithIDsUsingOps(..., defaultSessionConstructionOps)`, while the
  exact private test seam
  `newSessionWithIDsUsingOps(ctx, cmd, sessID, runID, wireTap,
  ops sessionConstructionOps) (Session, *sessionReapHandoff, error)` injects
  only those two post-start operations. This selects one implementation for
  the parent-held-child-end-close and lifecycle-transition failures; the old
  `abandonStartedSession`/direct-`cmd.Wait` path is retired. The returned
  handoff holds the concrete session, so `(*handler).Launch` never reaches
  through interface `Session`.
- `sessionReapHandoff` exposes only these package-private lifecycle
  operations:
  `startIO()`,
  `transferToWatcher() (reap func() error, onReaped func(error),
  wireTap io.WriteCloser, ok bool)`,
  `completeNormal(waitErr error)`, `abort(ctx context.Context) error`, and
  `startLegacyOwner() bool`.
  `transferToWatcher` performs the sole CAS
  `launch_owned -> watcher_owned` and returns
  `handoff.completeNormal` as `onReaped`, not metadata-only `finishWait`.
  `startLegacyOwner` likewise invokes `completeNormal` after its one reap.
- `completeNormal` owns successful descriptor retirement in exact order:
  after Watcher exit and raw reap, close stdin through its guard; call the
  unblock-before-join `stdoutBridge.CloseAndWait`; wait `stderrDone` for
  `stderrDrainGrace`, closing the handoff-owned stderr read end through its
  guard on timeout; join `stderrDone`; close that same stderr guard on the
  normal-EOF path; call metadata-only `finishWait`; and mark the handoff
  `reaped`. Thus framing-error, cancellation, and recovered-panic exits cannot
  strand the stdout copy merely because the Watcher stopped reading before
  process output was drained. Parent stdout/stderr child-write guards and the
  parent-held stdin child-reader guard were already released by successful
  construction. If public `CloseStdin` already closed the stdin parent writer
  to deliver EOF, this cleanup close is the same guarded no-op. The Watcher
  separately closes the transferred tap after `onReaped` returns.
- `abort` performs
  `launch_owned -> launch_reaping -> reaped`, starts the sole abort-reap
  goroutine calling `WaitOwner.WaitAndReap`, uses
  `context.WithTimeout(context.WithoutCancel(ctx), sessionAbortTimeout)` with
  exact `sessionAbortTimeout = 5 * time.Second` to Kill (including
  escalation), closes only handles still owned by the handoff (the stdin
  parent writer, stderr read end, and any unreleased parent-held child ends),
  calls the separately-owned `stdoutBridge.AbortAndWait`, joins `stderrDone`,
  then joins the one reap, calls `finishWait`, and closes the tap. Closing the
  stdin parent writer may race the raw reap but cannot race an `exec.Cmd`
  close: direct `os.Pipe` left `exec.Cmd.Wait` no ownership, and every
  Session/abort close reaches the same guard. `finishWait` therefore observes an
  already-closed `stderrDone` and cannot enter its drain-grace wait. The abort
  does not return until both auxiliary goroutines are joined; the same
  independent cleanup deadline bounds termination and both joins.
  `startLegacyOwner` performs
  `launch_owned -> legacy_owned` and starts the compatibility reap goroutine
  used only by public `NewSession`. `NewSession` calls
  `newSessionWithIDs(ctx, cmd, "", "", nil)`, must win
  `startLegacyOwner()` before returning the unchanged `Session`, and panics as
  an internal construction defect if that fresh handoff cannot transfer.
  Losing any other CAS performs no Kill, Wait, finish, or close. `finishWait`
  is metadata-only and separately once-guarded; every descriptor and the tap
  retain the single close guard created before ownership transfer. Ownership
  state—not `WaitOwner.sync.Once`—prevents two raw-wait callers.
- `handlercontract.SpawnWatcherConfig` gains the **optional** callback
  `TakeReapOwnership func() (reap func() error, onReaped func(error),
  wireTap io.WriteCloser, ok bool)`. Nil means exact legacy
  **observation-only** mode: `SpawnWatcher` starts the current read/event
  goroutine, performs no process Wait/finalization, owns no external closer,
  and changes none of the current fixture behavior. A non-nil callback is
  validated and invoked synchronously exactly once after all panic-capable
  validation/allocation and immediately before `go w.runLoop`. Its success
  must return either both `reap` and `onReaped` non-nil (Watcher process-owner
  mode) or both nil (legacy process owner plus Watcher tap-owner mode);
  `wireTap` may be nil in either mode. A false result or mismatched callback
  pair panics before any watcher goroutine; after a valid transfer there is no
  panic-capable operation before the goroutine starts. `Watcher.runLoop`
  always defers a transferred tap close and, only when the pair is non-nil,
  calls `reap` then `onReaped` on every normal, cancellation, framing-error,
  or recovered-panic exit. In callback mode the existing `Config.WireTap`
  field must be nil and the returned `io.WriteCloser` is both the effective
  tap writer and its close owner; direct and substrate production literals
  remove their separate `WireTap` assignment. In nil callback mode the legacy
  `Config.WireTap io.Writer` remains allowed and keeps its current
  caller-owned-close semantics.
- Consequently the 27 existing `SpawnWatcherConfig` literals remain compiling
  and byte-identical: they intentionally use nil observation-only mode across
  `watcher_hc011_test.go`, `hc061_subworkflow_boundary_test.go`,
  `watcher_w4_correlation_test.go`,
  `watcher_protocolmismatch_hk9ngiv_test.go`,
  `hcinv007_watcher_publisher_hk8i3170_test.go`,
  `cp024_budget_accrual_per_chunk_test.go`, and
  `watcher_deadletterfailure_hk0eqik_test.go`. They do not spawn a process or
  open a tap, so nil ownership is safe and requires no lease expansion.
- `(*handler).Launch` opens the optional wire tap first and passes it into
  `newSessionWithIDs`. After a successful start, the Launch goroutine is the
  temporary `launch_owned` owner and installs one recovery defer. If stdout
  wrapping or Watcher construction returns, panics, or produces invalid state,
  that defer converts the result to a typed structural error and calls
  `handoff.abort` with an independent bounded cleanup context. A nil
  `StdoutWrapper` means use raw stdout and proceed. A non-nil wrapper that
  returns nil is invalid and aborts; a wrapper panic is recovered and aborts.
  On successful `SpawnWatcher` return the callback has atomically transferred
  reap and tap ownership, so the Launch defer is disarmed. Thus constructor
  failure, wrapper failure, Watcher validation failure, and success each have
  one total owner and one tap close.
- The failure tests use one private assembly seam rather than global hooks:
  `launchAssemblyOps` contains
  `openWireTap func() (io.WriteCloser, error)` and
  `spawnWatcher func(context.Context,
  handlercontract.SpawnWatcherConfig) *handlercontract.Watcher`.
  Production `(*handler).Launch` and `launchViaSubstrate` call private
  `launchWithOps`/`launchViaSubstrateWithOps` with
  `defaultLaunchAssemblyOps`; focused tests inject open failure or a
  validation panic without changing exported APIs.
- The existing non-nil-stdout substrate branch uses a separate, exact
  compatibility handoff without claiming HC/PL conformance.
  `newSubstrateLaunchHandoff(subSess SubstrateSession)
  *substrateLaunchHandoff` is the first operation after successful
  `SpawnWindow`; it is infallible and enters `launch_owned` before
  `newSubstrateAdapter`, `Stdout`, wire-tap open, wrapper invocation, or
  Watcher construction. `launchViaSubstrate` immediately installs the same
  named-return recovery defer used by the direct path. `abort(ctx)` wins
  `launch_owned -> launch_reaping`, starts the sole
  `SubstrateSession.Wait` goroutine, performs bounded
  `SubstrateSession.Kill`, joins Wait, and closes an attached tap exactly once.
  Adapter failure, `Stdout` panic, wire-tap open failure, wrapper nil return or
  panic, and Watcher validation panic all recover through that abort.
- If `Stdout()` is nil,
  `substrateLaunchHandoff.releaseToLegacySession() bool` transfers the sole
  Wait obligation to the returned legacy `substrateSessionAdapter`, preserving
  `(Session, nil, nil)` and HookSessionStore completion. If stdout is non-nil,
  the handoff attaches the opened tap and supplies
  `transferLegacySessionAndWatcherTap` as `TakeReapOwnership`; its one CAS
  returns `(nil, nil, tap, true)`, transferring process Wait to the legacy
  Session caller and tap close to the observation-only Watcher immediately
  before `Watcher.runLoop`. Successful Watcher completion closes the tap once;
  the later legacy `Session.Wait(ctx) error` remains the one
  `SubstrateSession.Wait`. This is an explicit temporary PL-016 exception,
  isolated to `launchViaSubstrate` and removed by PS-02's conforming
  hook-session completion adapter; the direct exec/contract path remains
  Watcher-owned.
- The existing `session.runWait` raw-wait body becomes
  `session.finishWait(waitErr error)`, which only finalizes cached process
  metadata. Both legacy `Session.Wait` and
  `ContractSessionAdapter.Wait` only observe the cached result. The Watcher
  additionally captures the decoded `outcome_emitted` and
  `session_log_location` values behind read-only `Outcome()` and
  `LogLocation()` accessors; no second stdout reader or watcher is created.
- The bridge is pinned by exact tests:
  `internal/handler/contract_adapter_test.go` contains compile-time
  `handlercontract.Handler`/`Session` assertions, verifies
  `AgentType() == string(agentType)`, proves one mapped direct launch with the
  legacy three-return API still callable, and
  `TestContractHandlerAdapter_RejectsWatcherlessSubstrateBeforeLaunch` proves a
  mapped non-nil Substrate returns structural error with zero legacy Launch
  calls;
  `internal/handler/substrate_hkgql2011_test.go`
  `TestHandler_Launch_SubstrateNilStdout_WatcherIsNil` continues to prove the
  production legacy result is exactly `(non-nil Session, nil Watcher, nil
  error)`;
  `internal/handler/session_waitowner_test.go` proves `newSessionWithIDs`
  starts no raw-wait goroutine and both Wait surfaces return one cached reap
  result after the Watcher handoff;
  `internal/handler/session_reaphandoff_test.go`
  `TestNewSessionWithIDs_ConstructorFailureClosesPreparedWireTap` and
  `TestSessionReapHandoff_ExactlyOneWinningOwner` prove constructor cleanup
  closes every acquired direct-pipe end plus the tap before Start, and prove
  the atomic transfer/abort/legacy-owner state machine;
  `TestNewSessionWithIDs_ParentChildEndCloseFailureAfterStartAbortsOnce` and
  `TestNewSessionWithIDs_InitializingTransitionFailureAfterStartAbortsOnce`
  inject the two post-start constructor failures and assert return before the
  configured cleanup bound, one raw Wait, one finalization, all parent handles
  closed, one prepared-tap close, closed `stdoutBridge.Done` and `stderrDone`,
  and zero surviving stdout-copy/stderr-drain goroutines;
  `TestNewSessionWithIDs_DirectStdinPipeHasOneCloseOwnerAndDeliversEOF`
  runs a child that cannot exit until stdin EOF, calls public `CloseStdin`, and
  proves the child observes EOF, the parent-writer guard records one underlying
  close across repeated `CloseStdin` plus `completeNormal`, and
  `exec.Cmd.Wait` performs no competing close;
  `TestSessionReapHandoff_NormalCompletionTransfersStdoutGuardAndClosesAll`
  proves `startIO` removes the sole stdout-source guard from the handoff,
  `stdoutBridge.CloseAndWait` closes that source and its reader exactly once,
  stderr remains handoff-owned and drain-borrowed, and `completeNormal` closes
  stdin/stderr once and joins both auxiliary goroutines;
  `internal/handler/handler_reaphandoff_test.go`
  `TestLaunch_NilStdoutWrapper_TransfersReapAndTap`,
  `TestLaunch_NormalSuccessClosesDescriptorsAndJoinsAuxiliaryGoroutines`,
  `TestLaunch_WatcherFramingErrorWithUnreadStdoutUnblocksBridge`,
  `TestLaunch_StdoutWrapperReturnsNil_AbortsReapsAndClosesTap`,
  `TestLaunch_StdoutWrapperPanic_AbortsReapsAndClosesTap`, and
  `TestLaunch_WatcherValidationPanic_AbortsReapsAndClosesTap` prove the
  successful nil-wrapper transfer, normal-success one-Wait/one-finalize/
  one-tap-close path with every descriptor guard closed once and zero surviving
  stdout-copy/stderr-drain goroutines, a malformed first frame followed by
  unread stdout returns from `completeNormal` within the cleanup bound with
  closed `stdoutBridge.Done` and each bridge guard closed once, and all
  post-start failure cuts with one raw Wait and one tap close;
  `internal/handler/substrate_reaphandoff_test.go`
  `TestLaunchViaSubstrate_NonNilStdoutTransfersLegacyWaitAndWatcherTap`,
  `TestLaunchViaSubstrate_WrapperReturnsNilAbortsOnce`,
  `TestLaunchViaSubstrate_WrapperPanicAbortsOnce`,
  `TestLaunchViaSubstrate_WatcherValidationPanicAbortsOnce`, and
  `TestLaunchViaSubstrate_WireTapOpenFailureAbortsOnce` prove the
  non-nil-stdout success and every named failure cut. Each abort has one
  `SubstrateSession.Wait`; cases after a successful tap open have one close,
  while open failure proves no tap was acquired and none is leaked;
  `internal/handlercontract/watcher_reap_test.go` proves every normal,
  cancellation, framing-error, and recovered-panic exit invokes `Reap`
  exactly once;
  `internal/handlercontract/watcher_optionalownership_test.go` proves nil
  callback leaves all observation-only fixtures unchanged, rejects a
  half-present reap/finalize pair before goroutine start, and accepts
  `(nil,nil,closer,true)` while closing that transferred tap exactly once; it
  also rejects simultaneous callback ownership and legacy `Config.WireTap`
  before transfer;
  `internal/handlercontract/watcher_sessionstate_test.go` proves exact outcome
  and log-location capture; and
  `internal/lifecycle/spawnwait_pl014_test.go` retains the one-`cmd.Wait`
  invariant.
- PS-02 then binds tmux/substrate/hook/watcher cleanup to the frozen phase
  protocol. C6 fixes prerequisites, lease, intermediate state, and rollback.

ARCH-01 edits none of those files and does not amend HC or PL.

### Failure and restart

The result vocabulary distinguishes pre-launch failure, launch failure,
ready timeout, watcher/protocol failure, process exit with/without Outcome,
cancellation with successful/failed reap, cleanup timeout, and detached
survival. On restart, generation-local resources are absent; durable facts
route through C4/JR recovery. Events never reconstruct a phase.

## Rationale

This adopts RL-01's reviewed ownership handles, preserves all mode-specific
policy, makes teardown order reviewable, and resolves the research drift as a
migration fact rather than silently redefining HC/PL.

## Requirements traceability

| C2 requirement | Target |
| --- | --- |
| Full lifecycle edges | Sequence and PhaseObservation |
| Own session/watchers/inputs/joins | Retained phase scope |
| Separate process/session/Run facts | Request/result boundary |
| Failure/cancel coverage | Closed result vocabulary |
| Restart with volatile resources absent | C4 recovery handoff |
| Exact HC/PL citations | Per-edge citation rule |
| Executable HC/PL migration | named ContractHandler/ContractSession adapters, singular Watcher reap, and named focused tests |
| Stop at InputPort | HC-069 boundary |
| Resolve Ack contradiction | blocking INPUT-ACK-CONTRACT-01 dependency |
| Make HC/PL drift executable | PS-01, conformance slice, then PS-02 |
