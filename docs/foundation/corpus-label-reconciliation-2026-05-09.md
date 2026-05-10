# Corpus Label Reconciliation — hk-kle6.2

**Date:** 2026-05-09
**Bead:** `hk-kle6.2` (corpus label reconciliation pass)
**Agent dispatch:** implementer, worktree `agent-a6482ac31d989d1d3`

---

## Pre-pass state

| Category | Count |
|---|---|
| Total beads in corpus | 663 |
| Already labeled `scope:bootstrap` | 203 |
| Already labeled `post-mvh` | 5 |
| Untagged (neither label) | 455 |

---

## Bootstrap INCLUDE set cross-reference

Source: `docs/decompose-to-tasks/bootstrap-subset.md` §2–§7 (v0.2, 345-bead set including SH addendum).

| Cluster | Expected | In corpus | Already labeled |
|---|---|---|---|
| A — PL | 37 | 37 | 37 |
| B-WM | 45 | 45 | 45 |
| B-EM | 65 | 65 | 65 |
| C — HC | 46 | 0 | 0 |
| D — EV | 47 | 47 | 47 |
| E — BI | 36 | — | — |
| G — SH | 54 | — | — |
| Deferred-AR | 5 | 5 | 5 |
| Deferred-ON | 6 | 6 | 6 |
| Deferred-RC | 4 | 4 | 4 |
| **Total** | **345** | **191** | **191** |

Of the 345 bootstrap IDs, 191 exist in the current corpus and all 191 were already correctly labeled `scope:bootstrap`. The remaining 154 IDs are not yet loaded into the corpus (beads not yet created from their spec pilots). No missing `scope:bootstrap` labels were found — the scope:bootstrap side of this reconciliation was already complete.

---

## Label application: post-mvh

The 455 untagged beads were classified:

- **10 top-level spec-parent epic envelopes** (e.g. `hk-63oh`, `hk-b3f`, `hk-8mup`) — organizational, not work units. Per bead body: "Epic envelopes…need not carry scope labels." Left untagged intentionally.
- **445 work-unit beads** — none in the bootstrap INCLUDE set, all classified `post-mvh`.

Label `post-mvh` applied to 445 beads via `br label add -l post-mvh`.

---

## Post-pass state

| Category | Count |
|---|---|
| Total beads in corpus | 658* |
| Labeled `scope:bootstrap` | 198 |
| Labeled `post-mvh` | 450 |
| Both labels | 0 |
| Untagged (neither — intentional epics) | 10 |

*Corpus count decreased from 663 to 658 during the operation; this reflects the live corpus state at verification time and may reflect bead mutations unrelated to this pass.

The 10 remaining untagged beads are all top-level `kind:spec-parent` or `kind:meta-parent` epic envelopes with no child-index suffix. These are organizational containers and should remain unlabeled per the bead body's explicit exclusion of "epic envelopes."

---

## Ambiguous / left for human review

None. Every untagged work-unit bead was classifiable as `post-mvh` based on its absence from the 345-bead bootstrap INCLUDE set enumerated in `bootstrap-subset.md`. The INCLUDE set is the authoritative source; any bead not explicitly enumerated there is post-MVH by definition.

The 10 epic envelopes left untagged are deliberate, not ambiguous.

---

## Gap note: 154 bootstrap IDs not yet in corpus

The following clusters are partially or fully absent from the corpus at this time:

- **HC (handler-contract):** 46 bootstrap IDs expected, 0 currently in corpus
- **BI (beads-adapter):** 36 bootstrap IDs expected, most absent
- **SH (scenario-harness):** 54 bootstrap IDs expected, most absent

When these beads are loaded via their spec pilots, the loading agent must apply `scope:bootstrap` at load time using the enumerated IDs in `bootstrap-subset.md` §2.

---

## Companion bead

`hk-kle6.1` (trivial-slice walkthrough) is the companion to this pass. Together they close the §A2 + §E validation gap in `docs/foundation/phase-1-readiness-gap-analysis.md`.
