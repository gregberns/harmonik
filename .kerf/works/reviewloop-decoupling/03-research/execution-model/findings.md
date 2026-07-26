# Execution semantics findings

## Questions

1. Which decisions are already normative?
2. Where does shipped behavior conflict with those requirements?
3. What is the smallest justified amendment?
4. What must remain implementation-only?

## Evidence and findings

- EM-012 and EM-015d/e already own review-loop state, iteration, session continuity, verdict routing,
  cap/no-progress behavior, needs-attention and terminal ordering.
- Event-model §8.1a owns the seven review-loop event schemas and ordering.
- Shipped progress uses task-branch HEAD advancement (`reviewLoopState.lastIterHeadSHA`), while EM-015e
  still specifies cumulative diff-hash equality. Diff hash remains event evidence.
- After `REQUEST_CHANGES`, unchanged HEAD emits `review_fixup_stalled` and completes with
  `fixup_stalled`. Event-model and code agree; EM-015e and the completion-reason grammar do not.
- Flagless `REQUEST_CHANGES` is normalized to approval by shipped code. This changes terminal routing and
  must be made explicit if behavior is preserved.
- A churn-only commit does not satisfy the implementer work-product obligation.
- The CHB-023 context checkpoint SHA becomes the first-iteration no-commit baseline.
- Declared `Run.context` review-loop keys are not durably maintained by the current driver.
- Current event helpers mint unrelated handler session IDs and use implementer Claude continuity IDs in
  reviewer events; this violates event-model correlation and is an implementation defect.
- Cancellation after implementer/reviewer wait can return without the required cycle-complete event.
- Remote feedback/archive and several failure paths do not meet existing MUSTs; specs must not be weakened
  to bless them.

## Required minimal amendments

- EM-015e: HEAD advancement is the routing predicate; add `fixup_stalled`; keep `no_progress` for
  compatibility.
- Execution grammar: add `fixup_stalled` and durable prior-HEAD context state.
- EM-015d: qualify “commit” as work-product commit and cite the context-checkpoint baseline.
- EM-015e: specify the chosen flagless-REQUEST_CHANGES normalization and raw-versus-normalized audit
  representation.
- EM and EV conformance: cover all seven events, exhaustive transitions, exactly-one cycle-complete,
  cancellation/failure traces and session correlation.
- Event-model cap-hit final verdict should match the shipped `REQUEST_CHANGES`-only path.

## Pure projection pattern

`CycleState + Observation -> NextState + OrderedIntentValues + TerminalDecision`.

The projection may contain iteration/verdict/progress/session-selection/payload logic. It must not contain
git, filesystem, clock, emitter, tmux, network, goroutine or cleanup behavior.

## Risks

- Unit-only purity can hide missing durable `Run.context` updates.
- Payload-shape tests can miss identity correlation.
- Removing compatibility enum values would break historical readers.
- “Fixing” remote or feedback failures inside the extraction would mix behavior changes into the kernel.

## Resolved decisions

- Execution-model remains the sole semantic owner.
- HEAD advancement is the intended progress rule; diff hash is evidence.
- `fixup_stalled` is synchronized from event-model into execution-model.
- No new event type is needed.
- Implementation package/interface layout stays out of normative specs.

