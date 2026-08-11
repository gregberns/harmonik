# Queue execution graphs

This plan tests how Harmonik takes a bead graph from queue submission through implementation and merge.

The work has six ordered stages. Each stage has one document. Update `STATUS.md` after each material result.

1. Define the problem and the acceptance claims in `01-PROBLEM.md`.
2. Map plans, specs, code, and prior evidence in `02-RESEARCH.md`.
3. Define real runtime scenarios in `03-SCENARIOS.md`.
4. Record observed behavior in `04-EVIDENCE.md`.
5. Track confirmed gaps and fixes in `05-ISSUES.md`.
6. State the ownership model and final recommendations in `06-DECISION.md`.

This is research first. Do not change product behavior until a scenario or code trace proves a gap.

Kerf note: `kerf new queue-execution-graphs --jig plan` could not run from the delta worktree. The global kerf project link points at the shared checkout. Delta will not change that link while other lanes use the checkout. This folder mirrors the kerf pass order without changing global state.
