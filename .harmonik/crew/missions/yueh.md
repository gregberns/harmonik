schema_version: 1
crew_name: yueh
queue: yueh2-q
epic_id: hk-7l1w8
captain_name: captain
goal: |
  LANE #5 (operator priority order, admiral event 019f4ed0) — COMMS-TEST-HARNESS.
  Lane bead hk-7l1w8 (label codename:comms-test-harness), ~20/27 done. Systematic
  context-cancel implementation across the comms test-harness — NOT heavy-diff work.

  DISPATCH (your OWN queue yueh2-q — NOTE: the old yueh-q is paused-by-failure, use yueh2-q;
  never main), file-disjoint, the codename:comms-test-harness ready set:
  - Implement context-cancel in the remaining comms-test-harness beads (the lane bead lists
    the sub-set: n0wb0 x2, x8fc6, vm8ym, mpel5, etc.). These are systematic, mechanical,
    file-disjoint edits to the comms test files.
  - IMPORTANT (root-cause already fixed): these beads previously carried a `harness:codex`
    label with no model → empty-model codex stdin-hang. That label was REMOVED; they now
    route to claude-code/sonnet like every passing lane. If you see a bead re-acquire
    harness:codex or an empty-model launch, STOP and surface — do not re-introduce it.

  Model posture: Sonnet (systematic lane-drain, file-disjoint, not heavy-diff). Escalate to
  captain on ANY run_failed — do NOT self-classify a failure as benign.

  Progress feed per crew contract: comms --topic status AND br comments — on bead-close +
  <=10min while dispatching / <=15min idle-drain, + boot/drain bookends. Arm comms recv --follow.
  Escalate genuine blockers to captain. This lane is file-disjoint from daemon + hooks work.
