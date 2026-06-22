# Overnight directive — 2026-06-22 (operator asleep, back in the morning)

**Operator brief (verbatim intent):** Research + build out plans on the items below, review the hell out of them like normal. **If there's consensus → build it. If there's a blocker → leave it for the morning.** Try running work on the other box (gb-mbp); if so, bump up the max worker count (push that box — it has lots of RAM/CPU; watch session limits). Cut a release of a build we know works well.

**Captain operating posture overnight:** keep the 3 live lanes draining (paul=daemon-reliability, stilgar=keeper-coverage, logmine=remote/log). Research/design is safe. Build consensus items via the daemon DOT (worktree-isolated + review gate). Do NOT make risky irreversible/destructive moves unattended (don't wedge the fleet; remote-flip + concurrency-bump only if proven solid + reversible).

## Clusters

### C1 — workflow_mode must live in config (operator: HIGH frustration)
`queue submit --beads` defaults to **review-loop (single reviewer)** instead of the sonnet **triple-review DOT** → we've run for HOURS on the wrong process repeatedly = sub-par results. Research: how is workflow_mode set? Must every captain set it per-daemon, or is it config? **It should be set ONCE in config and stay.** Deliverable: code-grounded design + the exact config-owned-default change. Likely BUILDABLE.

### C2 — process supervision + health checklist (the A/B/C governor decision, operator-directed)
Operator's direction (this is the answer to the A/B/C I posed — leans **daemon-native + supervision**):
- An agent that walks a **checklist** to verify things are running; raises issues to the captain.
- The **daemon spawns AND tracks** keeper, watchdog (or its keeper), and other helper processes. On suspected issues it emits a comms message like *"Check this issue and call <cmd> if good — or fix it and call <other-cmd>."*
- **Daemon should check crew keepers are running** (they're just processes; `LiveKeeperPresent` already exists). Report issues via comms. Keepers configurable (can disable) but cheap to run.
- **Watchdog** must be auto-started (configured + checked-up), promoted to **first-class** (today its restart path is tmux-only and bypasses the daemon keeper-spawn). The daemon should check it's up.
Folds in held beads: hk-sbitr (watchdog auto-relaunch), hk-u5tgh (watchdog↔daemon keeper-spawn). Deliverable: supervision architecture + checklist contents + comms-escalation protocol + which parts are BUILDABLE-now vs need a morning decision.

### C3 — multi-box execution routing + worker scaling (operator: "nice easy next step")
gb-mbp (remote macOS, lots of RAM/CPU) is available. We already run work on a remote box; next step = run on **both** remote + local simultaneously. Want a **flexible/configurable routing system**: defaults, random assignment, whole-queue→box pin, force-local (for testing), OS-targeting (macOS/Linux), etc. Don't need it all at once — a straightforward router. Also: **3 crews monitoring 4 slots** is manager-heavy. Worker scaling: if we shift to gb-mbp we can experiment with more workers (watch session limits). Deliverable: current remote-substrate readiness (go/no-go for pointing the daemon at gb-mbp tonight + reversibility), routing design, phased V1, worker-scaling plan.

### C4 — cut a known-good release
Release a build we know works well. Research the release process (epic hk-brc3z = release-pipeline: create/validate/certify/rollback). Identify the known-good commit (origin/main has wixms proven). Deliverable: which commit, how to tag/build/certify, can-cut-tonight vs needs-sign-off.

## Pipeline
1. Phase 1 (RESEARCH+DESIGN): 1 agent per cluster → `plans/2026-06-22-overnight/0X-<cluster>/DESIGN.md`. Each must: ground in real code (file:line), give options + a recommendation, and a **BUILDABLE-NOW vs NEEDS-OPERATOR-DECISION** verdict with the exact change.
2. Phase 2 (REVIEW): ≥2 adversarial reviewers per design → consensus or blocker.
3. Phase 3 (DECIDE): consensus → file beads + dispatch via the **triple-review DOT** (or build directly for tiny/operational). Blocker → leave a crisp morning-decision note here.

## Status log
- 2026-06-22 ~05:05Z: brief captured; Phase-1 design agents dispatched (C1–C4).
