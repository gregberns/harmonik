# SESSION — 2026-07-14-run-state-machine (M3, run-state-machine)

## Resolution: CLOSED as resolved-out-of-jig (not `kerf finalize`d) — 2026-07-15

**M3 implementation is complete and landed.** The runexec state machine + explicit merge
queue (RSM spec) shipped across waves RT0–RT12 on branch `phase1-session-restart-substrate`;
final acceptance (RT12) landed at **`104a9ca7`** — N=10 relaunch oracle 10/10 green, every
daemon failure classified (pinned / environmental / pre-existing flake) with isolation +
baseline proof, evidence bundle independently reviewed **APPROVE**. The normative spec is
`specs/run-state-machine.md` (RSM vocabulary, 52 clause refs).

### Why this work is closed out-of-jig instead of `kerf finalize`d
`kerf finalize` is a **pre-implementation** packaging step: it copies `05-spec-drafts/` into
`specs/` and cuts an implementation branch. This work is **post-implementation** —
`specs/run-state-machine.md` has already evolved *past* the bench draft during RT5–RT12
(the RSM-017 build-class amendment; Amendment A1 = RSM-031/032 failed-Dispatch reopen edge).
Running finalize would copy the older draft **backward over the amended normative spec** — a
regression. The finalize square-check also expects six per-component spec drafts, but this
work legitimately produced **one combined RSM spec** (already normative in `specs/`); splitting
it would fabricate redundant docs that immediately drift. Closing out-of-jig preserves the
normative record and is the correct disposition. The bench draft has been synced UP to the
shipped `specs/` version so this record is not stale.

### Collision resolution (see COLLISION-NOTE.md)
Two design sessions raced. **RSM lineage (the combined spec) is canonical** — it is what the
code, reviews, and `specs/` were built against. The **RX lineage (Set A: one `run-state-machine.md`
with RX-001..020 + RX-INV-001..005, plus per-component drafts)** is **superseded and archived**
under `_complete-set-A-2026-07-14T1539Z/` and `05-spec-drafts/_archive-collision/`.

**RX↔RSM ids are NOT 1:1** (verified against the normative spec): e.g. RX-INV-003 (SR9 peer)
≡ RSM-INV-001; RX-020 (hclifecycle projection) ≠ RSM-020 (single close ladder). Therefore the
surviving 04-design docs retain their RX-* ids as informative-historical (banner added); they
were **not** blind-remapped, which would have corrupted them. The normative RSM ids in
`specs/run-state-machine.md` are authoritative.

### M2 carry-over (load-bearing)
The M3-4→M2-1 reactor input/ack contract was carried into the M2 track as AIS-001/003/004
(planner COORD c031, M2-1 handoff). No open carry-over remains.

### Bench layout note
`04-design/` mixes canonical names with a few `cN-`-prefixed survivors; these were left as-is
(renames would be cosmetic since finalize is intentionally not run). Extra docs
(`00-decisions.md`, `rt7-single-mode-failure-mapping.md`) are kept as the design record.
