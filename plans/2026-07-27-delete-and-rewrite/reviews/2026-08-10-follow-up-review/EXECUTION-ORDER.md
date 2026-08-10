# Execution order for Charlie

## Start sequence

1. C01 — detached event intent.
2. C02 — migrate `AdvanceGroup`.
3. C03 — migrate `AppendItems`.
4. C04 — adapt callers and prove persist-before-emit.

Stop after C04 for review.
This is the only fully unblocked production slice.

## Completion transaction sequence

5. C05 — completion receipt records.
6. C06 — pure transaction preparation.
7. C07 — completion binding in replace intents.
8. C08 — QM-053 transaction execution.
9. C09 — startup recovery.
10. C10 — receipt-backed status.
11. C11 — release markers.
12. C12 — receipt garbage collection.

Stop after C11 for a durability review before any daemon completion migration.
C12 can run later because it does not unblock group completion.

## Group-completion sequence

13. C13 — queue deep clone.
14. C14 — completion input and result types.
15. C15 — pure decision.
16. C16 — pure daemon durability policy.
17. C17 — thin daemon shell.
18. C18 — fault matrix.

C15 depends on a prebound completion receipt from C05 through C08.
C17 depends on recovery and ownership-release results from C09 through C11.

## Dispatch and run sequence

19. C19 — live bead state model.
20. C20 — dispatch intent and result.
21. C21 — startup replay.
22. C22 — pure queue selection.
23. C23 — run supervisor.
24. C24 — extend the parent run machine.
25. C25 — typed DOT child machine.
26. C26 — narrow run inputs by phase.
27. C27 — minimal core composition root and import fence.
28. C28 — optional admission providers.

Do not start C20 until C19 has no undefined process-death state.
Do not split `RunEnv` or `SharedHandles` before C24 defines the phases.

## Independent low-conflict work

C29 through C32 are bounded follow-up tasks.
Run them only when they do not overlap the active core files.
They do not unblock the core sequence.

## Review gates

Each slice needs these checks:

1. Run focused tests with `-count=1` before the edit.
2. Write tests that name the architectural claim.
3. Break the claim and confirm the named test fails.
4. Run `make fast` while working.
5. Run `make full` before integration acceptance.
6. Record any baseline failure separately from the change.

The first handoff to Charlie should assign C01 through C04 only.
The rest of this backlog gives Charlie enough queued work after each review gate.
