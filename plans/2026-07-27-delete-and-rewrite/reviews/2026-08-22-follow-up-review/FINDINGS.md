# Findings

## P0 — prevent unsafe work and unblock safe movement

### F01. Exact-path lint grandfathering blocks every package extraction

There are 248 grandfathered daemon file/linter pairs. Moving a file changes its
key, and the ratchet forbids adding the new key. This is a policy decision, not an
engineering ambiguity. No extraction should begin until rename-preserving behavior
is defined and tested.

### F02. The dispatch replay subsystem must remain inert

The new subsystem has strong typed decisions and tests, but no live producer calls
`Store.Create`. The replay executor supports only a subset of possible actions and
boot rejects any unresolved stored intent. Wiring the producer now can permanently
wedge restart. Preserve the code, document the prohibition, and design activation
as its own program rather than treating it as the next C21 step.

### F03. The daemon is accumulating architecture faster than it sheds it

The package grew 4,350 production lines, or 9.7 percent, since the preceding review
despite a real extraction. Several largest functions also grew. A visible landing
scoreboard and a “why did daemon grow?” commit requirement are now architectural
controls, not reporting niceties.

## P1 — execute proved seams

### F04. Run registry is the keystone extraction

The move has already been performed in a scratch tree and compiler-checked. It
requires two narrow API changes and removes dependencies for several later slices.
Once F01 is resolved, this is the smallest high-leverage first implementation.

### F05. Three leaf extractions can proceed independently after the keystone

Harness selection (1,218 lines), spend meters (638), and handler pause (2,056) do
not share files. They are appropriate churn work because the compiler enumerates
their boundary and each can carry its tests and delete its shims.

### F06. Pure queue selection is already complete

The current plan calls C22 unstarted, but source contains a pure snapshot-based
`internal/orchestrator.SelectNextQueue` and the daemon delegates to it. Do not assign
or recreate C22. Use its shape as the next pure-decision exemplar.

## P2 — shrink the orchestration spine, not merely its file count

### F07. The largest functions remain state machines encoded as stack frames

`runWorkLoop` is 1,432 lines with complexity 158. The graph driver and agentic
dispatcher have 26 and 32 parameters respectively. File movement will not fix this.
Name loop state, extract one pure decision at a time, and require monotonic reductions
in function LOC or signature width.

### F08. Run inputs and ownership remain too wide

`RunEnv` has 29 fields and `SharedHandles` 18. The run goroutines still lack one
supervisor-owned lifecycle. C23 and C26 remain more valuable than broad literal or
comment cleanup because they make ownership compiler-visible.

### F09. The control-plane cut is viable but too large for the first wave

The measured legal cut is 30 files and 8,370 lines, not the earlier 24-file guess.
It should wait until the lint rename rule and at least two smaller extraction landings
prove the process. Its tests are already external, which makes it a strong later
batch—not a safe first assignment.

## P3 — operational prerequisites and bounded cleanup

### F10. Crew routing claims do not match the wire

Subscriptions broadcast by event type. `owning_epic_assignee` is not populated in
the measured live events; `queue_id` is the only useful discriminator and stale-run
events lack it. Do not assume multiple implementation crews are isolated until the
routing contract is corrected and proven end to end.

### F11. Test speed needs gate engineering, not optimistic package splits

Most daemon time sits in full-work-loop integration tests that remain after file
moves. Parallel freeze checks, reusable Go caches, cache cleanup, and affected-package
selection are the near-term wins. Package decomposition primarily buys ownership and
parallel development.

### F12. Detector cleanup did not become safer

Total findings rose by 167 while the safe same-package A1 set remained exactly 54.
The evidence still rejects bulk repeated-literal replacement, bundle splitting by
width, and automated error-text rewrites. Only bounded, reviewed candidates belong
after core structural work.

## Rejected or deferred work

- Do not wire dispatch intent production.
- Do not redo C22.
- Do not bulk-apply 2,565 detector findings.
- Do not move the scheduler or work loop into a new package.
- Do not begin the 30-file control-plane move as the first extraction.
- Do not claim comment deletion improves tests or complexity; it only reduces reader
  context and requires source-sensitive verification.
- Do not start the terminal-substrate move before the capability-interface contract.
- Do not use package splitting as the sole test-speed strategy.
