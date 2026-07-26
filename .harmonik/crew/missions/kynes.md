schema_version: 1
crew_name: kynes
queue: kynes-q
epic_id: hk-hcrvb
captain_name: captain
goal: |
  TRACK A ONLY (operator-directed, comms event 019f4dc3). Flagship = core-loop-proof
  epic hk-hcrvb: live-verify the real task-processing loop across {claude,codex,pi}.

  DO, in order:
  1. REBASE ABANDONED (captain-approved 2026-07-10 ~20:48Z). integration/core-loop-proof is
     SUPERSEDED, not merely behind: the hk-g6plo series already landed the ENTIRE core-loop-proof
     pipeline on main + evolved it further; T9 assert-test = pass=28/fail=0 ON MAIN; the only
     unique file's logic is already inlined in main's MATRIX_SEED_MAP. Retire the branch; do NOT
     re-attempt the rebase. Work directly on MAIN.
  2. Drive T9 (hk-jjt6w: full-matrix green + clean-reset reproducibility) to GREEN — its remaining
     piece is a LIVE full-matrix run on MAIN via a scratch daemon (real claude+pi tokens):
     - Claude leg GREEN and pi leg GREEN are REQUIRED.
     - codex leg is a KNOWN-SKIP — do NOT block T9 on codex.
  3. Report progress on comms --topic status AND br comments (bead-close + <=10min while
     dispatching / <=15min idle-drain, + boot/drain bookends) per crew contract.

  DO NOT:
  - Pull kerf-next grab-bag beads. This lane is Track A ONLY.
  - Cleanup / churn beads: allowed ONLY if slots remain free after T9 is actively moving.

  ═══ RE-TASK 2026-07-11 ~01:25Z (OPERATOR DIRECTIVE via admiral, event 019f4ec1) — GATE-0 ═══
  The pi redeploy is now DECOUPLED from hk-x2spu + hk-ih5k6 (those are a parallel quality
  track owned by stilgar, NOT the flagship blocker). The SOLE operator-mandated gate on the
  pi redeploy is a single ISOLATED GATE-0 E2E TEST. Build it — it is the flagship critical path.

  GATE-0 (your #1 task, on a scratch/isolated daemon — NOT the fleet queue):
  1. Reproduce the pi in-daemon hang on the OLD binary (d7abf34a, the live fleet binary,
     WITHOUT afa32372 / without --no-extensions).
  2. Prove it GREEN with a binary that carries afa32372 (--no-extensions at pi launch).
  3. That one test is BOTH the deploy gate AND the root-cause confirmation.

  CAPTAIN HEAD-START FINDINGS (2026-07-11 ~01:25Z — read before you start; they CHALLENGE the
  old hk-ttina root-cause and you must resolve them empirically):
  - `.pi/extensions/*` is NOT tracked on current origin/main — the flywheel tree was deleted
    Jul 2 by 353fc3c1. A fresh worktree from main inherits NO tracked extension.
  - No `~/.pi/extensions/` exists in the operator HOME dir, and none in a live sample worktree.
  - The live daemon binary d7abf34a is dated Jul 8 — AFTER the 353fc3c1 deletion.
  - CONSEQUENCE: "pi wedges on extension-load without --no-extensions" cannot hold if there is
    nothing to load. Either the A/B green had a DIFFERENT cause, or the A/B worktree carried an
    extension from an older base. GATE-0 must OBSERVE whether any extension is actually present
    in the run's worktree/home at launch and report it — do not assume the old mechanism.
  4. REPORT to captain: the e2e result (hang-on-old / green-on-new) AND whether extensions were
     actually present in the worktree/home at launch (confirms or refutes the mechanism).

  ON GATE-0 GREEN: captain redeploys on own authority (announce HOLD->GREEN); then you re-run
  the pi seed against the redeployed daemon to prove the flagship pi:local leg GREEN -> hk-hcrvb
  complete. If GATE-0 does NOT reproduce/green as expected, STOP and surface — do not redeploy on
  an unconfirmed root cause.

  (The old "NO redeploy until hk-x2spu+hk-ih5k6 close" gate is SUPERSEDED by this directive.)

  Escalate genuine blockers (T9 leg wedged, GATE-0 can't reproduce, mechanism refuted) to captain.
  Use your OWN queue kynes-q; never submit to main. Scratch-daemon e2e does not use the fleet queue.

## Current State (2026-07-10 ~20:15 PT — FLAGSHIP COMPLETE, epic CLOSED)
hk-hcrvb CLOSED (captain-approved). Chain done: hk-j0p1r (fix) merged 21f03c3d/PR#30 + closed;
fleet redeployed SHA 59089968; production canary hk-m7xnb green (commit_landed 63.9s) + closed.
Close posted to operator + captain; PushNotification sent. Only follow-up: hk-vmxgk (reviewer
agent_ready_timeout, pi/ornith, 150s budget — non-blocking, distinct from the hang).
AWAITING captain re-task to top kerf-next lane (captain keeps me OFF hawat's internal/daemon
reversetunnel surface). No pending work on this epic. If re-tasked, re-hydrate {queue, epic} from the
new handoff. Scratch daemon /tmp/hkg0 still up (idle; can be torn down).

## Prior: PRODUCTION CANARY GREEN (2026-07-10 ~20:10 PT) — SUPERSEDED (epic now closed)
Captain REDEPLOYED fleet daemon to SHA 59089968 (hk-j0p1r routed fix LIVE, health window survived + pinned last-good).
Production pi canary PASSED: seed hk-m7xnb (throwaway queue pi-canary-q), routed pi/ornith on LIVE fleet daemon ->
implementer_phase_complete exit_code=0 commit_landed=true 63.9s. PI HANG DEAD IN PRODUCTION.
PENDING: captain's epic-close call on hk-hcrvb. Canary greens his criterion (commit_landed), but full pi:local
END-TO-END still needs the reviewer leg (hk-vmxgk, 150s agent_ready_timeout / slow-ornith). I recommended close-now
(option a) + carry hk-vmxgk as follow-up; HELD the actual close (hard rule: no terminal-write). On his call I ping the
operator. Do NOT re-run; canary done. hk-m7xnb is throwaway (its own run will run_failed on the reviewer timeout — expected).

## Prior: captain go/no-go delivered (2026-07-10 ~20:05 PT) — SUPERSEDED
BOTH captain deliverables DONE:
1. hk-j0p1r MERGED to origin/main: commit 21f03c3d via PR #30 (merge commit 59089968). agent-reviewer APPROVE. Pushed + review-gate + merged. Redeploy off main is safe.
2. Reviewer-timeout VERDICT = KNOWN slow-ornith agent_ready vs 150s defaultAgentReadyTimeout — NON-BLOCKING for redeploy, NOT the hang. Reviewer shares the fixed routedLaunchSpecBuilder->buildCodexRoutedLaunchSpec (reviewloop.go:1303); reviewers run silently (stalewatch.go). Filed hk-vmxgk (P2, harness:pi) to make the timeout harness-aware = remaining piece for FULL pi:local end-to-end green on hk-hcrvb.
AWAITING: captain redeploys off main on his authority (HOLD->GREEN), then I re-run pi seed vs the redeployed FLEET daemon to prove pi:local implementer leg green. Full-green pi:local still needs hk-vmxgk (reviewer timeout). Scratch daemon /tmp/hkg0 UP on 21f03c3d.

## Prior State (2026-07-10 ~19:52 PT / keeper-restart resume) — SUPERSEDED by above
GATE-0 = GREEN. hk-j0p1r fix committed on branch `hk-j0p1r-stdin-devnull` (SHA 21f03c3d):
StdinDevNull threaded through handlercontract.SpawnSpec + Pi/Codex LaunchSpec + buildCodexRoutedLaunchSpec,
plus routed-builder regression test (fails if the wire is removed). Proven on scratch daemon /tmp/hkg0:
seed hk-i10 pi/ornith via routed path -> implementer_phase_complete exit_code=0, commit_landed, 29.5s
(pre-fix: 207s stall, zero POST). Hang is DEAD.
- Reported to captain on comms + hk-hcrvb comment; RECOMMENDED merge + lift pi redeploy HOLD (captain's authority).
- Downstream the run_failed at "reviewer agent_ready_timeout iter1" (~150s) = KNOWN ornith slow-agent_ready
  budget issue (same now-fed routed path), a DISTINCT matter, NOT the stdin hang. Do not conflate.
NEXT (awaiting captain): on captain merge+redeploy, re-run pi seed against the redeployed FLEET daemon to
prove pi:local leg green -> hk-hcrvb. The reviewer agent_ready_timeout likely needs its own bead/fix for a
full end-to-end pi:local green. Branch committed LOCALLY (local branch + worktree, NOT pushed to GitHub
remote — sufficient for local merge/redeploy); no rework needed. Scratch daemon /tmp/hkg0 is UP on 21f03c3d.
