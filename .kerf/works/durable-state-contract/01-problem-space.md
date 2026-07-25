# Shared durable-state write and corruption contract

## Decision frame

This work translates the durable-state risks identified in
`plans/2026-07-24-code-health-audit/_plan.md` and the sequencing requirement in
`plans/2026-07-24-code-health-audit/INTEGRATED-EXECUTION-PLAN.md` into one
normative contract before any consumer-specific remediation begins.

The planning assumption is that durable state is evidence used for recovery and
operator decisions. A write that returns successfully must therefore either be
recoverable as a complete previous or complete new record, or be reported as a
durability failure. A reader must not silently reinterpret torn or malformed
state as absence.

## Goals

1. Specify one reusable atomic-replacement protocol: unique temporary file,
   write, file sync, close, rename, then parent-directory sync.
2. Specify reader behavior for absent, torn, malformed, and version-invalid
   state, including which cases are recoverable and which must surface to the
   caller/operator.
3. Establish an ownership and compatibility model so schedule sleep/wake,
   lifecycle branch-tip persistence, replay, and session metrics do not create
   incompatible one-off durability mechanisms.
4. Define the fault-injection and restart/readback evidence required before a
   consumer implementation can claim the contract.

## In scope

- The shared atomic write and read-validation contract.
- The policy boundary for the following consumers named by the integrated
  plan: schedule sleep/wake sidecar, lifecycle branch-tip persistence, replay
  corruption accounting, and sessiondata append/read visibility.
- Explicit treatment of parent-directory sync, file permissions, replacement
  atomicity, and error propagation.
- A consumer matrix identifying where append-only data needs a different
  contract from replacement-style state.

## Out of scope

- Implementing any consumer-specific repair before this contract is settled.
- Changing queue persistence; it has its own transaction/recovery workstream.
- Eventbus delivery and drain semantics except where replay needs a precise
  corruption signal from its persisted input.
- Primary-daemon runtime verification; the integrated plan forbids it while
  the daemon is down.

## Constraints

- The implementation must not create four bespoke atomic-write variants.
- Existing valid state must remain readable through any migration.
- No consumer may silently downgrade malformed persisted evidence to a clean
  first-observation or skip it without accounting.
- Durable-state acceptance requires injected failures at every write, rename,
  and sync boundary plus restart/readback evidence.
- Work remains file-disjoint from the active runloop/reviewloop spine.

## Success criteria

- A normative spec states the replacement and append-only durability rules,
  error vocabulary, and recovery behavior.
- Each named consumer is classified as replacement, append-only, or a
  transaction spanning multiple records, with its required compatibility
  behavior.
- Implementation tasks can be assigned by exact file ownership without
  duplicating policy or leaving corruption behavior implicit.
- The specification identifies any operator decision that is genuinely needed
  before implementation rather than silently choosing one.

## Preliminary affected areas

| Area | Current risk to resolve |
| --- | --- |
| `internal/schedule` | Sleep disables work before a separately written, non-durable restore sidecar exists. |
| `internal/lifecycle` | Branch-tip persistence can tear and then look like a first observation. |
| `internal/replay` | Malformed event input can be skipped without contributing to corruption accounting. |
| `internal/sessiondata` | Append/read behavior can silently undercount malformed or partial records. |
