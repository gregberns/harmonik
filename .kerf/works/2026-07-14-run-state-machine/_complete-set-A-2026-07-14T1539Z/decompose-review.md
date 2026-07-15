# Decompose Review — `run-state-machine` (M3)

> Self-review under waived signoffs (2026-07-14, design agent). Review protocol per
> jig-system.md §Review Pattern; inputs: `02-components.md`, `01-problem-space.md`,
> the P1 exemplar (`.kerf/works/session-restart-substrate/`), ROADMAP/PLANNING-LOG.

## Verdict: APPROVE — advance to `research`

## Criteria check

1. **Each affected area listed with description, requirements, dependencies** — YES.
   Six components (C1–C6), each with what/why-separable/dependency-order/open-questions.
   Spec surface named (new `specs/run-state-machine.md`, prefix TBD; `.golangci.yml`
   depguard edges; minimal event-model touch).
2. **Every goal maps to ≥1 component** — YES (traceability table at 02:238–244 covers
   all five §2 goals).
3. **No unjustified area** — YES. Each component traces to a goal or a named constraint
   (C1 → liveness testability; C2 → merge-queue goal + M4 prereq; C3/C4 → thin-driver +
   named-states goals; C5 → resume-hang goal; C6 → single terminal spine).
4. **Requirements are what-should-be-true, not text edits** — YES.
5. **All relevant existing spec files accounted for** — YES with one note: the daemon run
   lifecycle has NO existing normative spec (verified: no `specs/` file owns beadRunOne);
   touched-by-reference specs are `session-keeper.md` (SK-INV-005 as template, read-only)
   and `event-model.md` (possible additive event — design question, carried).
6. **Dependencies correctly identified** — YES; independently re-verified against
   PLANNING-LOG Fable verification (all 6 M3 claims CONFIRMED: 17 params, :3072–:5438,
   mergeMu :384 + 5 call-sites, 26 time.Now(), worktreeCreateMu split partial,
   SK-INV-005 at session-keeper.md:264).

## Notes carried into Research

- The decompose intentionally left every design question open ("stops at the Decompose
  boundary") — that hold has now been LIFTED: P1's keeper reactor is landed through T7
  (`internal/keeper/step.go`, `shell.go` on this branch), which is the un-hold condition
  (ROADMAP: "hold design until P1 proves the reactor method generalizes"). The proof
  exists in-tree; design may proceed against the landed template.
- Research must (a) verify every `file:line` against the CURRENT working tree (T5–T7
  keeper commits may have shifted nothing in daemon, but re-verify), (b) settle the
  C2 true-critical-section question with a step-by-step walkthrough of
  `mergeRunBranchToMain`, (c) produce the workLoopDeps field census that fixes the
  C3 cut line, and (d) ground C5 in the real resume-path mechanics + why today's
  staleness detectors miss the hang.
- One cross-work obligation not stated in 02: M3-4 → M2-1 is the single M3→M2 edge
  (ROADMAP §phase-map note); the C4 design MUST pin the reactor Step input/ack contract
  explicitly for M2 to implement against. Carried as a research+design requirement.
