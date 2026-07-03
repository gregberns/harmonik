# Research: Reconciliation Extension (C4)

**Pass 3 — 2026-05-30**

## Research questions

1. What are the current reconciliation categories in specs/reconciliation.md?
2. Is there existing coverage for "child bead exists, parent run discarded" (orphan-bead)?
3. Does the investigator run automatically on startup?
4. What is the escalation path for unresolvable cases?

---

## Current 11 reconciliation categories

| Cat | Name | Auto-resolve? |
|-----|------|--------------|
| Cat 0 | Infrastructure unavailable (br CLI missing, git locked, disk full) | Halt — no classification |
| Cat 1 | Idempotent rerun (node is idempotency_class=idempotent) | Auto-resume |
| Cat 2 | Non-idempotent in-flight (no run-terminal event yet) | Investigator required |
| Cat 3 | Generic store disagreement (git/Beads/JSONL inconsistent) | Investigator |
| Cat 3a | Torn Beads write (intent file present, wrong pre/post state) | Adapter auto-resolve |
| Cat 3b | Verdict-unexecuted (verdict commit exists, no Executed trailer) | Auto-re-execute |
| Cat 3c | Inverse premature-close (merge commit exists, bead still in_progress) | Auto-close |
| Cat 4 | Recoverable known state (agent in retry/backoff or gate) | Auto-resume |
| Cat 5 | Clean restart (nothing in-flight, orphaned branches from re-claimed beads) | No-op |
| Cat 6a | Integrity violation, LLM-triageable | Investigator dispatched, default: escalate-to-human |
| Cat 6b | Integrity violation, mechanically unrecoverable (corrupt JSONL, missing git object) | Auto-escalate to operator |

---

## Orphan-bead coverage gap

The "child bead exists but parent run discarded" scenario is **not explicitly modeled**. The spec classifies runs, not bead hierarchies. No current category handles:

- A bead with `codename:hk-<parent-id>` label on `main` where the parent run has no merge commit (discarded).
- An agent-spawned child bead that persists after its parent run fails.

The closest existing categories:
- **Cat 5** covers orphaned *branches* from re-claimed beads — not orphaned child beads.
- **Cat 3** (generic disagreement) would catch "bead `in_progress` but worktree missing" but not "open bead whose parent run was discarded."

**Three new categories needed (Cat-BL1/BL2/BL3)** as defined in `02-components.md` are confirmed as gaps:
- **Cat-BL1:** Child-bead orphan (parent run discarded, child bead persists with `codename:hk-<parent-id>` label).
- **Cat-BL2:** Bead-ledger-import-failure (daemon's `br sync --import-only` failed post-merge).
- **Cat-BL3:** Merge-conflict-log has entries (`.beads/merge-conflicts.log` non-empty after merge).

---

## Investigator dispatch points

Reconciliation runs at three points (RC-020a):
1. **Daemon startup** — automatic full scan BEFORE daemon reaches `ready`. Workflow dispatch is gated until detection completes.
2. **On-demand** — `harmonik reconcile [--run <run_id>]` operator command.
3. **Scheduled** — hourly background scan (configurable).

**Implication for Cat-BL1:** Orphaned child beads will be detected automatically on next daemon startup. No separate trigger needed. The startup scan is the natural sweep point.

---

## Escalation path

- Cat 2, Cat 3, Cat 6a → Investigator agent; if verdict is `escalate-to-human`, daemon emits `operator_escalation_required`. No quarantine state.
- Budget exhaustion (RC-018) → default verdict `escalate-to-human`.
- Malformed verdict (RC-023) → fallback `escalate-to-human`.
- Cat 3b retry cap (N=5) exceeded → routes to Cat 6b (auto-escalate, no investigator).
- Cat 6b → operator must restore from backup / repair git objects.

**Cat-BL1/BL2/BL3 escalation:** Cat-BL1 should be Cat 6a-style (LLM-triageable: investigator identifies orphaned child IDs, emits `orphaned_child_bead` event, default resolution: `br close <child-id>`). Cat-BL2 should retry once then escalate (Cat 6b-style for repeated failure). Cat-BL3 should be operator notification (surface conflicted bead IDs, no auto-resolution).
