# keeper-redesign — SESSION handoff

**Status when shelved:** `decompose` (advanced from `problem-space`).
**Scope of this session:** drafted the four operator-mandated planning artifacts.

## Progress
- `00-deploy-test-validate.md` — verbatim copy of the operator's `/tmp/paul-deploy-validate.md`
  plus a per-bead deploy/test/validate expansion.
- `01-problem-space.md` — goals (G1–G7), non-goals, constraints (C1 defaults-PIN HARD-NO …),
  verifiable success criteria (SC1–SC7), affected spec areas, glossary.
- `02-components.md` — goal→spec-area map; root spec = `specs/keeper-identity-and-liveness.md`.
- `05-spec-drafts/keeper-identity-and-liveness.md` — NORMATIVE draft of the four invariants
  with the EXPLICIT DELETION CHECKLIST (D1–D11, K1–K7) and validation tiers.
- `07-tasks.md` — bead-keyed task breakdown; every task has all FOUR parts
  (code / deploy / test / validate).

## Decisions made
- Architecture is SOUND (per 33-agent deep-dive); this is a REFACTOR, not a replace.
- Research + change-design passes are folded into the single spec draft — the design decision
  (replace inference with authoritative launch-SID identity, DELETE the heuristic block) is
  fixed, not open, so a separate research pass would be ceremony.
- Threshold constants are PINNED, not deleted; only the heuristic identity machinery is deleted.
- `NEW-forcerestart` bead is not yet filed — `07-tasks.md` carries the `br create` command.

## Open questions / follow-ups
- File the `NEW-forcerestart` bead (command in `07-tasks.md`) and dep-attach via
  `codename:keeper-redesign` label (NOT an epic dep).
- Decompose-pass reviewer (per jig-system.md §Review Pattern) has NOT run — if continuing the
  jig formally, run the decompose review, save `decompose-review.md`, then advance to `research`.
  Alternatively, since the spec draft is complete, jump to `spec-draft` review and `finalize`.

## Reading order for a new session
1. `01-problem-space.md` (goals/constraints — note the defaults-PIN HARD-NO).
2. `05-spec-drafts/keeper-identity-and-liveness.md` (the four invariants + deletion checklist).
3. `02-components.md` (what specs change).
4. `07-tasks.md` (bead-keyed work, four-part each) + `00-deploy-test-validate.md`.

## Code anchors (from a read-only investigation; do not edit on the planning thread)
- Heuristic identity block: `internal/keeper/watcher.go:664-888`.
- `rebind` CLI (DELETE): `cmd/harmonik/keeper_cmd.go:315-391` (WriteManagedSessionFn call at :388).
- Threshold constants: `internal/keeper/cycle.go` applyDefaults ~172-203; dup warn gate in
  `watcher.go` ~156/167.
- `OperatorAttached`: `internal/keeper/tmuxresolve.go:101-114`.
- Acceptance gate: `internal/keeper/cycle_twin_e2e_integration_test.go` (`//go:build integration`).
- Env vars: hooks read `HARMONIK_KEEPER_AGENT` (`scripts/keeper-stop-hook.sh`), rest reads
  `HARMONIK_AGENT` — unify on the latter.
