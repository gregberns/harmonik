# COLLISION NOTE — two design sessions raced on this work (2026-07-14)

Two independent design agents advanced this kerf work concurrently:

- **Set A (complete, status was advanced to `ready` at 15:39:45Z by Set A's
  session):** archived intact under `_complete-set-A-2026-07-14T1539Z/` —
  research synthesis (4 dossiers), 04-design (00-decisions **M3-D1..D14**,
  component designs), **one spec draft `run-state-machine.md` prefix RX**
  (RX-001..020 + RX-INV-001..005), 06-integration (14 verified checks),
  07-tasks (RT0..RT12 wave graph). Internally consistent end-to-end.
- **Set B (in flight at the time of this note, writing into the canonical
  paths):** per-component research `03-research/c1..c6/` (independently
  reviewed, `research-review.md`, APPROVE) + a `00-decisions.md` ("Fable
  synthesis", decision ids **D1..**, spec prefix **RSM**) + per-component
  design docs landing incrementally.

**Facts agree everywhere checked** (mergeMu regions incl. hk-zguy6/hk-lt091
coupling; the 2 dead workLoopDeps fields; the 85-field census; byte-identical
exit-0/agent_completed blocks; the 2s caulk's missing run_id; the uncaulked DOT
resume). The divergences are NAMING (RX vs RSM prefix; M3-D vs D ids) and
artifact granularity (one spec vs per-component).

**For the reconciler (planner):** pick ONE canonical set; do not interleave.
Set A is complete and self-consistent; Set B's research organization is
square-conformant and its review is worth keeping either way. The M3-4→M2-1
reactor input/ack contract is pinned in Set A as RX-009..011 / M3-D11 and has
been reported to the planner for the M2 track — if Set B is chosen, that
contract must be carried over verbatim or re-issued to M2.
