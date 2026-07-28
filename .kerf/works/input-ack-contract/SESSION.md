# Session — input-ack-contract

- **Task:** `INPUT-ACK-CONTRACT-01` (plan `2026-07-24-code-health-audit`).
- **Base:** `176d3fe87`. Branch `work/input-ack-contract`, worktree
  `/Users/gb/github/harmonik-wt/input-ack-contract`.
- **Scope:** normative drift correction only. No production Go, no tests, no
  event schemas, no `Ack` record, no `TASK-INDEX.yaml`.

## State

All eight spec-jig passes are written. Status is `ready`. `kerf finalize` is
deliberately NOT invoked, per the card.

| Pass | Artifact |
|---|---|
| 1 Problem Space | `01-problem-space.md` |
| 2 Decompose | `02-components.md` |
| 3 Research | `03-research/input-ack-semantics/findings.md` |
| 4 Change Design | `04-design/input-ack-semantics-design.md` |
| 5 Spec Draft | `05-spec-drafts/input-ack-semantics.md`, `05-changelog.md` |
| 6 Integration | `06-integration.md` |
| 7 Tasks | `07-tasks.md` |
| 8 Ready | this file |

## Decisions taken

1. **Drift, not a decision.** Git chronology shows the owner spec landed the
   binary `Ack` the day AFTER RSM wrote the three-valued model, and the
   operator-ratified COORD c021 row reaffirms binary. Corrected RSM, kept the
   owner model.
2. **RSM gained a revision-history section (§14).** It had none. The card
   requires one dated row per changed spec; the section header notes the table
   starts at v0.2.1 and earlier versions are in Git.
3. **`.kerf/project-identifier` reverted.** A prior claim attempt had scoped it
   to `gregberns-harmonik-input-ack`; that file is not in this card's write
   lease, so the change was reverted.

## Evidence

`plans/2026-07-24-code-health-audit/tasks/evidence/INPUT-ACK-CONTRACT-01.yaml`
— pinned blob IDs, chronology, per-site before/after, grep proofs, and the
code-contradiction check (none: `internal/handler/input_port.go` already ships
the binary `DeliveryOutcome`).
