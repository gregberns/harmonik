<!-- PP-TRIAL:v2 2026-06-08 main — controlpoints thread. LANE COMPLETE: the codename:productization onboarding-docs tier is DONE and has held for 5 days. There is NO major standalone controlpoints work left — everything remaining is daemon/DOT CODE owned by named-queues. The shared HANDOFF.md + flywheel/named-queues threads are SEPARATE concurrent work — do NOT clobber. -->

ROLE: orchestrator. Delegate via the persistent daemon's queue (skill `harmonik-dispatch`), your OWN `--queue controlpoints`. Don't edit main's working tree while a queue is ACTIVE (escape-detector). Failed-twice → investigator, never a 3rd blind re-dispatch.

# State: CLEAN (no controlpoints work in flight; controlpoints queue empty/archived). Local main = origin @2169450a.
- Tree has ambient `.beads/issues.jsonl` churn — NOT mine (peer ledger drift); leave it for named-queues, who is actively working the ledger.

## Bottom line — is there anything major left for controlpoints? NO.
The onboarding-docs tier is **done and verified on main** 5 days on: README.md (227 ln), AGENT_OPERATING_MANUAL.md (230 ln), its template (278 ln), AGENT_INDEX bridge links, AGENTS.md work-project-deploy section. A new deployer can read README → install → `harmonik init` → run. The "hand-someone-a-link" onboarding goal is achieved.

## What's actually left (7 beads) — all named-queues' zone, NOT controlpoints solo work
`br list --label codename:productization --status open` → hk-p0kum, hk-tldws, hk-tnmjy, hk-n7fw3, hk-30vlb, hk-4rkrg, hk-gax8v. All daemon/DOT **code**: the standard-bead.dot review-embedding process (p0kum/30vlb/n7fw3), queue-submit workflow_mode stamp bug (tldws, P1), review_gate_anomaly alarm (tnmjy), smoke-bead self-verification (4rkrg), `harmonik promote` PR helper (gax8v, needs design). These need DESIGN + named-queues coordination, not raw dispatch — they edit the exact dispatch path named-queues keeps churning.

## Daemon is NOT safe to dispatch to right now (gating the above)
- Daemon (pid 39721, -c6) is running a **STALE Jun-3 binary**; main has moved ahead (session-keeper Phase-2 67a74def/2169450a, CHB-023, hk-15b83 d87c71a8).
- named-queues' launch-stall fix **hk-9vp51 was REVERTED** (P0 spawn regression: deterministic sessionName broke spawn under supervisor tmux-nesting) — daemon reliability is still unsettled; fix-forward pending.
- The `main` queue is stuck `paused-by-failure` on a STALE hk-mgoo7 group (the bead is closed/landed). named-queues said TODAY (06-08 09:22) it will clear that group + fix-forward hk-9vp51 + decide on a rebuild.
- **So: do NOT dispatch controlpoints work until named-queues broadcasts "daemon redeployed/healthy."**

## Next step (one decision, then optional work)
1. Read the bus (`harmonik comms log --since 24h`); wait for named-queues' daemon-healthy broadcast.
2. **Decision to surface to the user / peers:** the 7 remaining beads aren't really "controlpoints" — fold them into named-queues' queue, OR stand up a kerf work for the standard-bead.dot DOT-process design (3 beads) and take only that. If you want to keep moving solo, hk-tldws (P1 dispatch bug) is the most self-contained, but coordinate with named-queues first (their code).
3. Minor open risk only: the README pins `br` install as `cargo install --git …beads_rust` UNVERIFIED on a clean machine — a real deployer could hit a snag. Low priority; verify if a deploy is imminent.

## Files to open first
1. Memory `project_productization_initiative.md` (P0 + onboarding-docs tiers DONE; the 7 code beads listed).
2. `br list --label codename:productization --status open`.
3. `harmonik comms log --since 24h` — named-queues' daemon-stabilization status.

## Translations glossary
- **codename:productization** — make harmonik deployable on new/work repos (onboarding docs DONE; review-embedding DOT process remains).
- **onboarding-docs tier** — README + AGENT_OPERATING_MANUAL + templates + bridge links + work-deploy docs. DONE.
- **standard-bead.dot** — the DOT graph defining a bead's process (implement→commit-gate→review); goal is non-bypassable review as a DOT floor. Beads hk-p0kum/hk-30vlb/hk-n7fw3.
- **hk-tldws** — bug: `queue submit --beads` must stamp the resolved `workflow_mode` per item (relates to the historical review-off root cause).
- **hk-9vp51** — named-queues' daemon launch-stall fix; REVERTED (P0 spawn regression), fix-forward pending. Daemon reliability not yet settled.
- **stale Jun-3 binary** — the live daemon predates 5 days of main commits; a rebuild+restart is named-queues' call (their daemon work).
- **controlpoints queue** — my isolated `--queue controlpoints`; clears on success, `paused-by-failure` on BLOCK.
