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

## 2026-06-30 ~20:40Z — operator (via admiral) · expires: 2026-07-04
WHAT: NEXT-PHASE TRIGGER for gurney's remote lane (gurney's separate-daemon pivot is making progress —
      scratch-daemon.sh harness up, conc 3, hardening). ONCE the gb-mbp proof is solid (beads reliably +
      3-concurrent execute ON gb-mbp, not local-fallback), gurney routes a LARGE BATCH of the scavenger/
      orphan backlog (~120 ready) through THIS separate daemon -> gb-mbp at volume (start conc 3, push
      higher to find the break point). Purpose: drain real backlog through remote AND load-test remote to
      surface issues (launch-gap stall, review.json truncation, worktree races, worker overload, tunnel
      flake). thufir/codex STAYS paused on the main daemon — the backlog WORK moves to gurney's separate
      remote daemon, NOT a revived scavenger crew.
WHY:  decoupled separate daemon makes a high-volume real-load remote stress test cheap + safe (no prod
      restart); doubles as backlog drain. Volume is how the remaining intermittent remote bugs surface.
ORDER: gurney harden harness -> prove gb-mbp reliable+concurrent -> THEN volume scavenger-through-separate
       -daemon (report throughput + failure-mode tally + break-point concurrency).
RETURN-PATH: queued to gurney (topic remote, no-wake) + captain heads-up. Resume by checking whether the
      gb-mbp proof holds yet, then whether the volume backlog batch is flowing through the separate daemon.

## 2026-06-30 ~20:20Z — operator (via admiral) · expires: 2026-07-04
WHAT: REMOTE APPROACH CHANGE + slot reallocation (operator, frustrated — ~2wk, can't get remote right).
      STOP testing remote by toggling gb-mbp in the LIVE primary daemon's workers.yaml + restarting it
      (done 8+ times, each a fresh failure + revert; risks health-window false-revert). INSTEAD: gurney
      runs a SEPARATE standing test daemon (own worktree/clone, own .harmonik/sock/workers.yaml) PINNED
      to gb-mbp (enabled there only, max_slots:3, concurrency 3) and HAMMERS small THROWAWAY no-merge
      beads to exercise the remote path fast + concurrently — decoupled from production. SLOTS: gurney=3,
      leto=1, codex/scavenger PAUSED (thufir-q paused). DATA backing the change: only 3 clean single-run
      gb-mbp proofs ever (proof-dot/rfix/ssh-fetch); ZERO runs ever through a separate test daemon; every
      concurrent attempt fell back to local or hung in the launch_initiated->agent_ready gap (hk-1s1or).
WHY:  the main-daemon restart-toggle loop is slow + brittle + risks bricking production; a standing
      remote-pinned test daemon turns a 2-week stall into a tight reproduce-and-fix loop, no merge needed.
ORDER: gurney aborts STAGE-2 primary-daemon cycle NOW -> stands up remote-pinned test daemon ->
       throwaway-bead hammer until gb-mbp execution is reliable + concurrent -> THEN fold fixes back.
RETURN-PATH: directed gurney directly (--wake) + captain (topic remote). Resume by checking the test
      daemon's gb-mbp landings/30min + which failure mode recurs (launch-gap stall / review.json / wt-HEAD).

## 2026-06-30 ~14:50Z — operator (via admiral) · expires: 2026-07-04
WHAT: Operator SETTLED the keeper-architecture open question (synthesis item f). Decision = HYBRID:
      keep the per-crew DETERMINISTIC keepers (skeleton) AND add a PROBABILISTIC overseer ON TOP that
      intervenes when a keeper fails. "Centralized" = overseer sits on top of keepers, not instead of.
      The hk-u5tgh fix = route the overseer/watchdog restart THROUGH the daemon crew-start path
      (HandleCrewStart -> spawnCrewKeeperWindow) so the keeper window survives a restart instead of
      being stripped. This UN-GATES hk-u5tgh (P1). Also: killed the dead orphan ctx-watchdog tmux
      session (never seeded, wrong CWD, ran no loop — half-failed boot spawn).
WHY:  per-crew keepers are reliable locally but a tmux-level restart bypasses them; the overseer adds
      a recovery layer, and daemon-routed restarts make that layer durable rather than hand-armed.
ORDER: paul finishes hk-xxcv9 (crew-boot auto-arm) → takes hk-u5tgh (daemon-routed overseer restart);
      keeper lane otherwise unchanged. Interim seeded overseer optional, lower priority than the fix.
RETURN-PATH: hold:operator-design-decision label removed from hk-u5tgh; lanes.json keeper gate -> null;
      settled design relayed to captain over comms (topic keeper). Resume by checking paul's hk-u5tgh progress.

## 2026-06-30 ~12:00Z — operator (via admiral) · expires: 2026-07-04
WHAT: Operator has codex/ChatGPT session tokens available — MAXIMIZE implementer work through the
      codex harness to offload cost OFF the Anthropic budget. ADDITIVE + LOWER PRIORITY than the 3
      lanes: must NOT disrupt remote/pilot/keeper staffing (operator: "hold off if it gets in the way").
      Two fits (captain's call): (a) route file-disjoint build/bugfix beads through the codex harness
      as implementer; (b) revive the codex-first scavenger crew (thufir, queue thufir-q, 1-item serial
      + DOT review) to drain the ~120-bead backlog on ChatGPT tokens. CAVEAT: codex asleep ~4 days →
      RE-CANARY one local run before leaning on it. Codex is LOCAL-only (not gb-mbp), bills ChatGPT.
WHY:  cost-per-landed-outcome + model-fit: ChatGPT-billed throughput is free against the Anthropic
      budget; the backlog is starved (~120 ready) so codex is pure additive throughput.
ORDER: 3 lanes first (unchanged) → codex re-canary → route codex-eligible work / revive scavenger.
RETURN-PATH: relayed to captain over comms (topic directive). Resume by checking whether codex is
      re-canaried + running (leto-codex / thufir-q active with codex harness), throughput off-budget.

## 2026-06-30 ~11:40Z — operator (via admiral) · expires: 2026-07-04
WHAT: Fleet woke from a ~4-day operator-directed sleep onto the security-fix daemon (7a9bf2e5,
      deploy daemon-20260630-01). Operator confirmed STAFF 3 LANES, remote #1: (1) remote-worker
      e2e proof (hk-nepva, blocker hk-t1t00 now CLOSED) — but VERY THOROUGH LOCAL testing FIRST via
      the L0–L5 pyramid / isolated test-daemon (NO live-daemon restart needed); gb-mbp is UP for the
      live portion. (2) Pi-harness core build (hk-4rmj1, codename:pilot) — operator-UNGATED now. (3)
      Keeper reliability (hk-u5tgh + hk-xxcv9). All 2026-06-25 priority blocks EXPIRED → history.
WHY:  remote reliability is the unlock to raise concurrency 4→8; pi-harness adds a 2nd implementer
      harness; keeper-less crew restarts are a recurring fleet-reliability tax. Local-first remote
      testing keeps blast radius low and the live daemon untouched.
ORDER: remote LOCAL pyramid → remote live on gb-mbp ‖ pi-harness build ‖ keeper fixes (file-disjoint).
RETURN-PATH: captain spawned (harmonik-a3dc45482890-captain); admiral relayed via comms. Resume by
      checking the captain's crew/queue staffing for the 3 lanes + nepva's local-pyramid progress.

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
