# Charlie implementation backlog

Each task has one architectural claim.
Charlie must keep implementation commits separate from this review.
Charlie must run focused tests with `-count=1`.
Charlie must deliberately break each new claim test before accepting it.

## Charlie status

Updated: 2026-08-10

- Owner: Charlie.
- Active slice: C12 is complete. C13 is next.
- Start revision: `b49210d67` on `work/alpha-integration-merge`.
- State: C01 through C04 are complete and independently approved.
- Implementation commit: `de0a6ca3`.
- Integration reconciliation commit: `b0cac51a`.
- Stop gate: C05 through C07 received independent approval before C08.
- Coordination note: the queue RPC overlap retained both the active-run status work and the event-intent path.
- Later tasks: C13 through C31 remain unstarted.

### C12 evidence

- GC eligibility uses only an explicit trusted, synchronized, non-regressed UTC observation.
- Unavailable, unsynchronized, regressed, and pre-retention observations delete nothing.
- Unreleased receipts with no release marker are never selected.
- GC revalidates the exact marker and receipt pair immediately before deletion.
- GC removes and syncs the receipt before it removes and syncs the marker.
- Marker-only cleanup first makes receipt absence durable.
- Result phases distinguish durable receipt absence, durable marker absence, and indeterminate sync cuts.
- Initial-sync, unlink, post-unlink sync, deletion-time CAS, symlink-root, and non-regular file faults preserve the required facts.
- A later pass completes durability after an earlier root-sync failure.
- QueueStore serializes GC with completion marker installation.
- Live loop maintenance calls the QueueStore port with an untrusted observation and reports phased faults.
- Production deletion remains fail-safe disabled until a platform owner supplies trusted clock status.
- Focused queue, queue-store, and daemon tests pass with `-count=1`.
- Repository compilation, `go vet ./...`, and the diff check pass.
- A retention-gate mutation made the pre-retention transaction test delete both records and fail.
- Independent reviewer verdict: `APPROVE` after one blocking correction round.

### C11 evidence

- The queue store samples release time only after it releases ownership.
- Marker installation validates the exact receipt and never replaces a valid marker.
- A root-sync retry keeps the first durable marker time.
- Marker failure retains the receipt and does not reacquire or block the queue name.
- Startup recovers both the C09 pending handoff and a receipt-only crash state.
- Startup refuses a wrong receipt-root type.
- Recovery rejects an old live identity, corrupt canonical identity, symlinks, and other non-regular canonical state before it samples time.
- A newer same-name queue with a different valid ID does not block marker recovery.
- Focused queue, queue-store, and lifecycle tests pass with `-count=1`.
- Repository compilation, `go vet ./...`, and the diff check pass.
- Bypassing the production startup marker call makes the lifecycle marker test fail.
- Independent reviewer verdict: `APPROVE` after two blocking correction rounds.

### C10 evidence

- Status returns an exact live queue before it considers a receipt.
- An absent live queue can return exact completion facts from one receipt.
- A watched group above the final receipt group does not report completion.
- Invalid IDs, corrupt receipts, ambiguous receipts, symlinks, directories, and FIFOs fail closed.
- Repeated status reads create no files and change no receipt data.
- The text client reports the receipt ID, final status, counts, and completion time.
- Focused queue, queue CLI, queue-store, and lifecycle tests pass with `-count=1`.
- Repository compilation, `go vet ./...`, and the diff check pass.
- A mutation that bypassed receipt lookup made the receipt-backed status test fail.
- Independent reviewer verdict: `APPROVE` after one blocking correction round.

### C09 evidence

- Startup routes completion intents through a receipt-aware recovery path.
- Recovery reuses the exact bound receipt. It never retries the final observation.
- Exact precommit facts roll back. Exact committed facts finish receipt installation and cleanup.
- Third states refuse and preserve canonical, candidate, receipt, and intent bytes.
- The result reports only a proven durable phase. It hands a markerless receipt to C11.
- An unresolved intent now fails startup closed before queue loading.
- The process-stop table covers every completion intent boundary and replay.
- Intent removal and intent parent-sync cuts do not report cleanup as durable.
- Focused queue, lifecycle, and queue-store tests pass with `-count=1`.
- Repository compilation, `go vet ./...`, and the pinned changed-line lint pass.
- Removing the completion-specific startup route fails the public recovery test.
- Independent reviewer verdict: `APPROVE` after two blocking correction rounds.

### C08 evidence

- The queue transaction owner installs the completed canonical and exact receipt before observation.
- The result names each durable phase through ownership release.
- The canonical cleanup uses queue identity and byte digest checks.
- The observer runs without the queue lock. A temporary quarantine refuses all other mutation entry points.
- A missing observer is refused before filesystem I/O.
- Boundary tests cover receipt-root safety, each canonical and receipt write cut, cleanup cuts, and ownership retention.
- Focused queue and queue-store tests pass with `-count=1`.
- Repository compilation, `go vet ./...`, and the pinned changed-line lint pass.
- Removing the observer requirement fails its pre-I/O test.
- Removing the raw mutation guard fails the observation-window ownership test.
- Independent reviewer verdict: `APPROVE` after four correction rounds.

### C05 through C07 evidence

- C05 adds strict completion receipt, binding, and release-marker values.
- C06 fixes detached candidate bytes, receipt bytes, identities, time, and marker inputs before I/O.
- C07 permits the binding only on a completion replace intent.
- Focused queue and queue-store tests pass with `-count=1`.
- Repository compilation and `go vet ./...` pass.
- Removing candidate timestamp normalization fails the exact-byte test.
- Removing intent coupling lets an append intent accept a completion binding and fails its test.
- Independent reviewer verdict: `APPROVE` after two correction rounds.

### C01 through C04 evidence

- Focused queue, daemon, scenario, and event-bus tests pass with `-count=1`.
- Repository `go vet ./...` passes.
- A clock-read mutation makes the fixed-time group-intent test fail.
- An intent-order mutation makes the several-deferred-items test fail.
- A removed-persist mutation makes the persist-before-emit test fail.
- A removed stale-identity guard makes the replacement-window test fail.
- A commit-phase mutation makes the cleanup-failure observation test fail.
- Independent reviewer verdict: `APPROVE` after two correction rounds and a final integration review.
- `make core` stops in `scripts/agent-reviewer-prompt-parity-test.sh` because macOS Bash has no `mapfile`.

## P0 — ready now

### C01. Add a detached queue event intent type

**Problem:** Queue decisions return `core.Event` even though they do not own envelope identity or envelope time.

**Scope:** Add a queue-owned value with `core.EventType` and detached JSON payload bytes. Add a constructor that marshals a typed payload and performs no clock, UUID, or I/O work.

**Acceptance:** Value tests assert exact type and bytes. A source check shows no clock or UUID import in the intent constructor. A payload mutation after construction cannot change stored bytes.

**Limits:** Do not change event bus behavior or payload schemas.

### C02. Migrate `AdvanceGroup` to event intents

**Problem:** `AdvanceGroup` calls `newEvent` and has hidden effects.

**Scope:** Return ordered event intents. Preserve the group-completed, queue-paused, and successor-start order. Use only the supplied completion time in payload timestamps.

**Acceptance:** Existing state tables pass after conversion. New tests compare exact payload bytes and order. Fixed input produces byte-identical output. No UUID or system-clock call remains on this path.

**Limits:** Do not redesign final durability or change group timestamp fields.

### C03. Migrate `AppendItems` to event intents

**Problem:** `AppendItems` uses the same hidden event helper.

**Scope:** Return detached intents for `queue_appended` and each deferred item. Keep the current order and supplied acceptance time.

**Acceptance:** Tests cover zero, one, and several deferred items. Exact payload bytes and intent order stay stable. The function creates no UUID and reads no clock.

**Limits:** Do not change append validation, persistence, or item status policy.

### C04. Adapt queue callers to emit intents at the bus edge

**Problem:** Callers currently unpack queue-created envelopes and discard fields.

**Scope:** Convert intent type and payload bytes to existing event-bus calls after durable success. Cover append and group transition callers.

**Acceptance:** Positive counters prove persist happens before emit. Commit failure emits nothing. Event bus tests prove it creates envelope identity and time once.

**Limits:** Do not add envelope creation back to queue code.

## P0 — transaction prerequisite

### C05. Define completion receipt and binding values

**Problem:** The transaction model has failed-recovery receipt values but no completion receipt values.

**Scope:** Add the QM-005 completion receipt, binding, and release marker records. Keep exact IDs, basenames, canonical bytes, and digests in the binding.

**Acceptance:** Strict JSON round-trip tests cover every field. Invalid identity, basename, digest, version, and record type are rejected.

**Limits:** Do not execute filesystem changes in this task.

### C06. Prepare one completion transaction before I/O

**Problem:** Final completion needs completed canonical bytes and receipt bytes fixed before the first namespace write.

**Scope:** Build a pure preparation function. It allocates through supplied ID and time values. It returns candidate bytes, transaction ID, receipt ID, binding, and marker inputs.

**Acceptance:** Fixed inputs produce exact bytes. A repeated call with the same inputs is identical. The function reads no clock, UUID source, or filesystem.

**Limits:** Do not install the transaction.

### C07. Extend replacement intents for completion bindings

**Problem:** Replace intents cannot carry a completion receipt binding.

**Scope:** Add the binding to the intent and validation rules. Require it only for completion. Reject a completion with no binding and a non-completion with one.

**Acceptance:** Exhaustive operation-kind tests cover valid and invalid combinations. Startup decoding rejects unknown or conflicting bindings.

**Limits:** Keep failed-recovery and archive behavior unchanged.

### C08. Execute the QM-053 completion transaction

**Problem:** No owner performs completed canonical install, receipt install, observation point, cleanup, and ownership release as one typed protocol.

**Scope:** Add a completion operation to the queue transaction owner. Return explicit phases for not committed, committed, receipt durable, cleaned, ownership released, and marker failure.

**Acceptance:** Fault tests stop after every filesystem boundary and assert the result phase. No event or cancel is authorized before its required phase.

**Limits:** Do not route daemon policy in the transaction package.

### C09. Recover interrupted completion transactions at startup

**Problem:** A process can stop between completed canonical install, receipt install, cleanup, release, and marker install.

**Scope:** Classify every supported namespace combination. Finish the exact transaction or refuse inconsistent third data.

**Acceptance:** A table covers every process-death boundary in QM-053. Replaying a resolved state is idempotent. Corrupt or mismatched bytes never cause deletion.

**Limits:** Do not use events as durable evidence.

### C10. Serve receipt-backed queue status

**Problem:** After canonical cleanup, status must still report exact completed identity from the durable receipt.

**Scope:** Read and validate completion receipts by queue ID and optional receipt ID. Return the receipt-backed status shape from the existing query boundary.

**Acceptance:** Exact-ID lookup succeeds. Wrong IDs and corrupt receipts return typed errors. A repeated status call creates no data.

**Limits:** Do not infer completion from an event.

### C11. Install and retain completion release markers

**Problem:** Receipt retention starts only after ownership release and marker durability.

**Scope:** Install the marker after release. Report marker failure without reacquiring the queue name. Keep retention data from trusted supplied time.

**Acceptance:** Marker failure leaves the receipt and released name. A retry installs the same marker. Tests prove no second receipt is minted.

**Limits:** Do not add garbage collection yet.

### C12. Add receipt and marker garbage collection

**Problem:** Durable receipts and release markers need the specified bounded retention path.

**Scope:** Implement receipt-first deletion after the retention limit. Sync each root. Delete the marker only after durable receipt absence.

**Acceptance:** Backward clock movement does not delete early. Partial failure remains retryable. Unreleased receipts are never collected.

**Limits:** Do not mix this with completion dispatch.

## P1 — group completion after C05 through C11

### C13. Add a queue-owned deep clone

**Problem:** Queue values contain nested slices, maps, and pointers. A shallow copy can mutate the input.

**Scope:** Move or add one queue-owned deep clone for decision functions and transaction adapters.

**Acceptance:** Tests mutate every nested result field and prove the input remains unchanged.

**Limits:** Do not export mutable aliases.

### C14. Define total group-completion inputs and results

**Problem:** The daemon completion function uses indexes, strings, booleans, and side effects as an implicit result.

**Scope:** Define typed item outcome, location, expected queue identity, completion disposition, no-change result, and conflict or corrupt-state errors.

**Acceptance:** Every admitted input shape has a named result. Exhaustive switches pass lint.

**Limits:** Do not perform effects.

### C15. Implement `DecideGroupCompletion`

**Problem:** Queue transition policy is split between `AdvanceGroup` and the daemon shell.

**Scope:** Return a detached next queue, ordered receipt-bound intents, changed state, and disposition. Handle stale identity, repeated outcomes, conflicts, pause, successor, and final completion.

**Acceptance:** Table tests cover every branch. Tests prove the input is unchanged. One supplied time controls every payload time.

**Limits:** Do not write files, wake dispatch, cancel contexts, or emit events.

### C16. Define daemon durability policy from transaction phases

**Problem:** The daemon flattens commit and cleanup failures.

**Scope:** Map decision disposition plus transaction phase to persist, clear, wake, cancel, emit, refill, and log actions.

**Acceptance:** Pure tables cover intermediate, paused, final success, commit failure, cleanup failure, released marker failure, and stale no-change.

**Limits:** Do not call effect ports in the policy function.

### C17. Replace `evaluateGroupAdvanceWithOutcome` with a thin shell

**Problem:** The live function owns policy, durability, memory mutation, cancellation, wake, emission, and refill.

**Scope:** Call the decision, execute the receipt-aware transaction, apply the durability policy, then perform outer effects. Keep slow work outside locks when the writer rule allows it.

**Acceptance:** Positive effect counters prove exact calls and order for each disposition. Focused force-reap, claim-failure, reservation-failure, and normal-completion tests pass.

**Limits:** Do not extend `CompleteAndUnlink` or preserve it as the target path.

### C18. Add the group-completion fault matrix

**Problem:** Green scenario tests do not prove process-death behavior.

**Scope:** Add fault injection at each durable boundary from decision through release marker.

**Acceptance:** Each named test is observed failing under a deliberate mutation. The report records which transition each test defends.

**Limits:** Do not use sleeps as the proof condition.

## P1 — dispatch ownership

### C19. Write the live bead state model

**Problem:** Queue item, bead, run registry, run record, worktree, merge, and session states have no single valid-combination model.

**Scope:** Add a normative table or typed model that names every valid combination and transition owner.

**Acceptance:** Every process-death point from reservation through bead close has one recovery result.

**Limits:** Do not change code until the model closes.

### C20. Define a dispatch intent and transaction result

**Problem:** Reservation and bead claim are two durable writes with compensation between them.

**Scope:** Define one typed intent for reservation, claim, and run identity. Define committed, replayable, refused, and repair-required results.

**Acceptance:** The type cannot represent a claimed bead with no queue or run identity. JSON decoding rejects partial records.

**Limits:** Do not add another best-effort repair record.

### C21. Make startup replay dispatch intents

**Problem:** Recovery policy is spread across scheduler branches and boot repair.

**Scope:** Replay each durable dispatch intent to one terminal result before new dispatch starts.

**Acceptance:** Fault tests cover stop after reservation, claim, run record, and launch handoff. Replay never double-dispatches.

**Limits:** Do not use log text or event presence as authority.

### C22. Extract pure queue selection

**Problem:** `runWorkLoop` mixes candidate selection, fairness, admission, repair, dispatch, and shutdown.

**Scope:** Create a value decision that returns no work, wait, reject, repair command, or one dispatch candidate with reasons.

**Acceptance:** Table tests cover fairness and admission order. The function reads no clock and performs no I/O. Time is an input.

**Limits:** Do not launch or reserve work.

### C23. Give run goroutines one supervisor

**Problem:** Scheduler code owns goroutine launch, registry state, adoption, drain, and completion hooks.

**Scope:** Give one supervisor typed start, stop, adopt, and terminal-result operations.

**Acceptance:** The scheduler does not own goroutines. Shutdown tests prove who owns a live run after timeout.

**Limits:** Keep run policy in the run machine, not the supervisor.

## P2 — full run machine and boundaries

### C24. Extend the run machine through plan and provision

**Problem:** `runexec.Run` owns the terminal spine only.

**Scope:** Add typed plan-resolution, worktree, session, and launch actions and outcomes. Keep effect execution in the shell.

**Acceptance:** One table drives success and each failure outcome without filesystem or process fixtures.

**Limits:** Keep DOT traversal as a child machine.

### C25. Make DOT traversal a typed child outcome

**Problem:** `driveDotWorkflow` and `dispatchDotAgenticNode` mix traversal, launch, gate, and output effects.

**Scope:** Return typed node actions and terminal workflow outcomes to the parent run machine.

**Acceptance:** The parent handles one child outcome type. Tests cover tool, agent, gate, retry, and terminal nodes.

**Limits:** Do not copy the parent lifecycle into the DOT machine.

### C26. Narrow run inputs by phase

**Problem:** `RunEnv` and `SharedHandles` expose broad dependency sets to every run phase.

**Scope:** After C24, derive small phase inputs and consumer-owned effectors from actual use.

**Acceptance:** Each phase receives only fields it reads. Import tests prevent inward packages from importing daemon adapters.

**Limits:** Do not split by field count alone.

### C27. Add a minimal core composition root

**Problem:** The charter core is not a compiler-enforced package boundary.

**Scope:** Compose config, event bus, queue, bead adapter, worktree, one harness, run machine, and merge without optional services.

**Acceptance:** A focused build constructs this path with optional services absent. An import fence rejects keeper, dashboard, crew, schedule, and sentinel imports.

**Limits:** Do not build the future dataplane.

### C28. Move optional admission providers outside the scheduler

**Problem:** Optional maintenance and policy services shape the core loop even when disabled.

**Scope:** Compose enabled admission providers outside the core. Give the selection decision one typed combined result.

**Acceptance:** Disabled providers are not constructed. Removing one provider needs no scheduler edit.

**Limits:** Do not add new optional behavior.

## P3 — bounded follow-up work

### C29. Replace clear same-package queue and daemon literals

**Problem:** A small set of current literals bypasses a same-package named value.

**Scope:** Review and replace the clear queue validation reason, failed-recovery operation, decision status, sleep-marker prefix, and parent-label prefix sites.

**Acceptance:** Wire bytes remain unchanged. Each replacement compiles through the named type where one exists.

**Limits:** Exclude ambiguous, cross-package, test-twin, struct-tag, and CLI syntax findings.

### C30. Classify workflow-loader parse errors once

**Problem:** Three workflow loader paths inspect error text.

**Scope:** Determine whether the parser is owned. Return a typed classification from the parser boundary and preserve the raw cause with `Unwrap`.

**Acceptance:** Changing the human message does not change classification. Non-matching errors retain their original type and detail.

**Limits:** Do not create one sentinel for unrelated parser failures.

### C31. Classify tmux not-found errors at the OS adapter

**Problem:** The tmux adapter uses two string fragments to identify absence.

**Scope:** Parse process exit facts and stderr once at the adapter. Return a typed absence result to callers.

**Acceptance:** Message wording variants and unrelated failures have separate table cases. Raw stderr remains available for diagnostics.

**Limits:** Do not change tmux capability policy.

### C32. Review the remaining CLI and socket error-text sites

**Problem:** Comms, subscribe, decisions, and smoke commands still inspect error text.

**Scope:** Group sites by producer. Add typed results only where Harmonik owns the boundary. Record external protocol parsing as adapter behavior.

**Acceptance:** Each changed site has one producer-side test and one caller-side test. No global text-matching helper is introduced.

**Limits:** This is several small tasks if file ownership conflicts.
