<!-- TIER: 2 (operational state, sequencing intent across direction changes)
     LOADED BY: admiral + captain @ boot, AFTER tier-3 (project.yaml) + tier-2 (captain-lanes.md), BEFORE acting.
     OWNER: admiral. APPEND-ONLY. ONE entry per direction CHANGE (never a status update, never per-tick, never by crews).
     Newest-first. ~3-5 lines/entry. Capped ~10 entries / ~60 lines; delete oldest on overflow (no archive).
     Four load-bearing fields per entry: WHAT / WHY / RETURN-PATH(sequence) / expires:.
     ON EXPIRY the DEFAULT is LAPSE -> revert to the standing autonomous posture, NEVER a hold.
     The admiral audit OWNS flagging an expired-but-present entry: re-confirm with the operator or strike it.
     See .harmonik/context/AGENTS.md for the full forced-write/forced-read discipline. -->

# Direction log — temporal sequencing intent across direction changes

> The one thing no other doc holds: WHY we paused X for Y and IN WHAT ORDER we resume.
> This is what a fresh /clear destroys. Read the newest RETURN-PATH as ground truth for sequencing.

## 2026-06-25 ~21:29Z — operator (via admiral) · expires: 2026-07-02
WHAT: TWO additions elevated INTO the active remote lane (out of parked/on-deck). (1) CONCURRENT
      LOCAL+REMOTE: routing mechanism already LANDED today (hk-f10xl — Queue.LocalOnly/WorkerTarget
      gate SelectWorker); remaining = LIVE concurrent-run proof + live worker on/off toggle (hk-xjbvi).
      (2) TEST-DAEMON HARNESS: operator clarified the long-misread "two daemons" idea = a STANDING
      isolated test daemon in a separate worktree/clone pinned to remote (submit issues to main daemon)
      = MOVE ① (scratch-clone), NOT the skipped move ④ (two daemons on the SAME repo dir). PROMOTE
      from scope-only to BUILT.
WHY:  per-queue routing landed makes "run on both at once" a validation+polish task, not a build; the
      standing test-daemon is the fast-loop unblock that ACCELERATES the remote last-mile (move ① was
      always the #1 do-now), so building it now serves the headline rather than competing.
ORDER: remote reviewer-consistency last-mile → live concurrent local+remote proof (same path) ‖ build
      test-daemon harness (parallel, scratch-clone). hk-xjbvi toggle folds in. Multi-remote scheduler = later.
RETURN-PATH: captain scoping both (directive comms 019f00af); admiral-initiatives.md STALE on routing
      (listed "not designed/on-deck" — it's LANDED) → admiral to correct. Resume by checking captain's
      kerf-work + bead set for the test-daemon harness + the concurrent-validation bead.

## 2026-06-25 ~19:39Z — operator (via admiral) · expires: 2026-07-02
WHAT: 3 parallel side-quests added ALONGSIDE the headline (remote stays #1). (a) NOW: stand up an
      orphan-backlog SCAVENGER — one standing crew, 1-item serial queue, codex-first + DOT review,
      drains the starved low-pri/orphan tail. (b) NEXT: DECOMPOSE token-opt #2 into ready beads. (c)
      WHEN-SLOTS: SCOPE (not build) a separated test-daemon kerf harness (test codex/daemon changes
      without restarting the live daemon; unifies with the remote test-daemon spike).
WHY:  ~125 ready beads / 71 P2 sit starved; fill slots + drain backlog in parallel while remote deploys.
ORDER: scavenger NOW → token-opt #2 decomposed/staged → test-daemon harness scoped when slots free.
RETURN-PATH: scavenger=thufir LIVE (epic hk-0kr4j); token-opt #2 decomposed (hk-017sc/hk-ln48u; AO held
      for scope-reconcile vs live watch cutover); test-daemon scope = NOT STARTED (resume here when slots free).

## 2026-06-25 ~19:10Z — operator (via admiral) · expires: 2026-07-09
WHAT: confirmed the active priority SEQUENCE (lanes run PARALLEL where slots + disjoint work allow).
WHY:  get remote working RELIABLY first — it is the unlock to raise concurrency 4→8 (more active work).
ORDER: (1) remote-worker reliable  [gurney, headline; GOAL = bump concurrency 4→8 once proven]
       (2) token-opt               [resume as ready beads appear]
       (3) codex production routing [leto pilot live; also offloads Opus/Sonnet cost]
       (4) flywheel = the admiral/captain framework changes (PLAN-v2 + the stall-detector)
RETURN-PATH: remote proven reliable → raise 4→8 → staff token-opt when ready → codex scales → framework lands.
