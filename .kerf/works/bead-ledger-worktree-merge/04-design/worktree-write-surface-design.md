# Design: Worktree Agent Write Surface (C2)

**Pass 4 — 2026-05-30**

---

## Current state

`specs/beads-integration.md`:
- **BI-010:** Daemon owns all terminal transitions. Agents MUST NOT call `br close` or `br update --status=closed`.
- **BI-011:** No intra-run writes except the `claim` write (daemon's `br update --status=in_progress` before agent spawn). Agents MUST NOT call `br create`.
- **BI-025e:** Concurrent `br` invocations across processes are permitted (SQLite WAL).

`br create` from inside a worktree agent is currently prohibited under BI-011.

---

## Target state

### Amendment to BI-011

Amend BI-011 to permit two new write categories:

**BI-010e (new clause).** Child-bead-spawn creates. An implementer agent MAY call `br create` inside a worktree to spawn child beads (e.g. research decomposition → task beads). Constraints:
1. Child beads MUST include the parent bead ID as a label: `codename:hk-<parent-id>` (orphan-sweep anchor per Cat-BL1).
2. Before creating, agent MUST check for existing child beads: `br list --label=codename:hk-<parent-id>` — if an expected child already exists, skip the create (idempotency). `br create` has no native dedup; agent is responsible.
3. Terminal transitions remain daemon-only (BI-010 unchanged). Child beads start as `open`; the daemon closes them via the normal terminal-transition adapter.

**BI-011 amendment.** Permitted intra-run write categories:
- `claim`: daemon's `br update --status=in_progress` (existing).
- `child-bead-spawn`: agent's `br create` with `codename:` label (new, BI-010e).
- `parent-bead-update`: agent's `br update` to add labels or notes to the parent bead (new, informational — not status transitions).

Prohibited (unchanged):
- Terminal transitions (`br update --status=closed`, `br update --status=failed`, `br close`) from inside worktrees. BI-010 enforced by skill discipline; no process-level guardrail exists.

**BI-011 failure contract (explicit).** If an agent issues a prohibited terminal write, the daemon's post-merge `br sync --import-only` will re-import the merged JSONL, which reflects the merge-driver's LWW pick. The daemon's terminal-close then runs on top. Net effect: prohibited terminal write from inside worktree MAY persist if LWW happens to favor the worktree row. Risk is pre-existing (acknowledged in BI-010); this amendment makes it explicit rather than leaving it implicit.

---

## Rationale

Child-bead-spawn is the central use case this kerf work exists to enable. Without amending BI-011, agents cannot drive workflows that decompose a research bead into N task beads. The merge-driver (C1) handles the JSONL union correctly; this spec amendment defines the write surface that agents operate against.

The idempotency requirement (check before create) is important: agents may be retried, and duplicate child-bead creates would produce phantom beads that the reconciliation investigator would need to sweep. Requiring the `codename:hk-<parent-id>` label makes child beads mechanically discoverable by the Cat-BL1 reconciliation rule.

---

## Implementation sites

- `specs/beads-integration.md` — amend BI-011, add BI-010e clause.
- `docs/components/internal/beads-integration.md` (if exists) — update agent-facing write surface docs.
- Skill injection templates (handler contract) — update agent skill text to document BI-010e write category.
