# Plan-mining candidates — valuable untracked work from plans/ (survey 2026-07-18)

> Admiral-dispatched survey of the last month's `plans/` dirs for valuable work that was proposed
> but never completed and is NOT already done/tracked. Hand the top items to the captain to run
> through kerf. **Partial: agent-1 (10 recent dirs) done below; agents 2 & 3 (20 older dirs) were
> still running at keeper-handoff — re-run or check their output if this list feels short.**

## Ranked candidates (agent-1: recent dirs)

1. **Fix the remote-worker concurrency ceiling (fleet falls over at ~6 runs)** —
   `plans/2026-07-11-remote-concurrency/`. Remote gb-mbp worker proven only at ~3 concurrent, fails
   at 6 (reviewer `agent_ready_timeout`); operator rejects "cold start", suspects a structural
   serialization point (single lock / blocking SSH / shared tmux). Investigation never started.
   **Gates the operator's ~9-concurrent scale goal.** Existing beads (hk-5z1f0/hk-zno2t/hk-sfy7f)
   are band-aids, not the architectural fix. kerf-ready as an `explore`/spike; needs real-box + disk
   for a 6-slot canary. **Rank 1 — highest value.**

2. **Retire the rotted captain boot chain → cut over to `harmonik agent brief`** —
   `plans/2026-07-11-captain-startup-revamp/`. New lean boot ships & works but AGENTS.md/STARTUP.md
   still point at the old ~1,700-line path (~25–50k tokens/boot vs ~116). 5 real defects to fix
   first (watcher-set contradiction, renderer dropping standing-rules refs, `_skills/` twin drift).
   Recurring context/cost win on every boot + keeper-restart. Re-validate the 5 defects against
   current HEAD before executing (freeze-and-carve moved boot flows). Size M.

3. **Capacity/cost-aware scheduler (4 missing signals + N-machine fleet)** —
   `plans/2026-07-09-scheduling-assessment/`. Dispatch today is a flat counter admission loop; a real
   scheduler needs per-box load, per-(box,model) capacity, per-bead weight, live per-provider
   cost/quota, and lifting the one-worker cap into an N-machine fleet. Structural enabler for cost
   control + the 9-concurrent goal. Size L; sequences AFTER candidate #1. Needs carving into slices at
   problem-space — scope first, don't start as one work.

4. **Complete the Codex cross-check lane of the mega code-review** —
   `plans/2026-07-16-mega-codex-review/`. Claude lane landed (code-revamp Track 4); Codex lane ran only
   a single-RU pilot. Only worth resurrecting if the operator still wants independent Codex cross-check
   on high-risk subsystems. Needs a go/no-go decision before scoping. Size M.

## Ranked candidates (agent-2: mid-era dirs) — several hit tonight's token/cost goal

1. **Model-aware dispatch routing (category→tier table + verifier-cascade)** —
   `plans/2026-07-05-model-selection/` thread-5. Config table picks Claude tier (Haiku/Sonnet/Opus)
   per task category; start cheap, escalate one tier only on a quality gate-fail. Harmonik's objective
   test/lint/review gate is a free verifier → reliable cascade, no ML. **Directly attacks Claude token
   burn — biggest cost lever.** Untouched (flywheel-internal routing at `54a20e55` is separate). kerf-ready
   (thread-5 is a phased spec); hook = existing `resolveHarness`. Size M/L; depends on #2.
2. **Fix stale model pricing table** — `plans/2026-07-05-model-selection/`. `internal/sessiondata/sessiondata.go:67`
   prices Opus-4.8 at $15/$75 (Opus-3 era; ~3× overstated) and omits Fable-5/Sonnet-5/Haiku-4.5 (emit no
   cost). Verified still broken. Prereq for any trustworthy cost/routing decision. Size S (bead-sized);
   make config-loadable not hardcoded (see [[no-external-version-binding]]). Fold as Phase 0 of #1.
3. **Fix reconcile false-close (match fix CONTENT, not a bead-ID mention)** —
   `plans/2026-07-12-codebase-census/` REPORT addendum. Daemon closed hk-2hfyt because an unrelated docs
   commit mentioned the string "hk-2hfyt" — fix never applied. Close/reconcile signal is untrustworthy.
   General fix untracked (only the narrow hk-8juwz instance is). Size M; `internal/daemon/reconciliation.go`.
4. **Wire assessor/done-check gate INTO promote+deploy, fail-closed** — `plans/2026-07-07-quality-enforcement/`
   WS-B. Deterministic done-check between APPROVE and close + move the assessor block-query into
   `harmonik promote` so it mechanically refuses to ship with open P0/P1 found-by beads. WS-A landed;
   B/C/D open. Size M.
5. **Finish census Move 1 — delete dead event-registry surface + collapse specaudit to a CI lint** —
   `plans/2026-07-12-codebase-census/`. Delete zero-consumer decode/validate surface; move specaudit
   markdown-regex out of the test suite into one CI lint. Partial (theater delete `c14ad11d`, specaudit
   build-tagged `32791808`); dead-registry deletion + lint-collapse remain. Size M.
6. **Spec failure-modes as a gated kerf artifact** — `plans/2026-07-05-quality-process/` P1. Require every
   spec to name failure modes + boundaries crossed + shared state; `kerf square` blocks without it. ~57%
   of defect mass came from unnamed failure modes. Superseded-but-resurrectable; process/jig, not product.
7. **One canonical liveness contract** — `plans/2026-07-05-quality-process/` P4 + `2026-07-02-stall-sentinel/`.
   Single tested definition of alive-vs-wedged grounded in `events.jsonl` (not pane presence / worktree
   mtime). Kills the mis-diagnosis defect class (~20 of 95 bugs). Cross-cutting; scope first. See
   [[verify-crew-liveness-not-presence]].
   *(agent-2 omits agent-world-models = ABANDONED; stall-sentinel/eval/quality-system = active kerf works.)*

## Ranked candidates (agent-3: older dirs — mostly expansion-phase, deferred not abandoned)

1. **"Publish, don't narrate" — structured state-publishing seam** —
   `plans/2026-07-03-fleet-state-and-dashboard-data/`. Agents call commands that write structured state
   to a store flushed to a git-tracked artifact (the `br sync` pattern); the human-readable file becomes
   a generated view, not hand-edited source. Kills stale-prose-file rot; the data layer the
   operator-dashboard kerf needs underneath. Partially mitigated by tiered context files but the general
   seam is unbuilt. Size M–L; scope first (README names 2 distinct data structures).
2. **Quality-gate enforcement remainder** — `plans/2026-07-04-quality-loop/`. Still-open escape hatches:
   `scenario.yml` `continue-on-error: true`, main branch protection, a `kerf finalize` gate needing a
   PASSING scenario test, gating daemon redeploy on `harmonik smoke`. Serves the release-gate goal.
   **Coverage-diff against the active `quality-system` kerf work first** — only un-absorbed items are net-new.
3. **Container/build-cache sandbox for a full CREW (not just per-bead)** —
   `plans/2026-06-30-distributed-fleet/02-container-sandbox/`. Run a whole queue-owning crew in an isolated
   container; solve heavy-compile-per-sandbox so spin-up ≈ main box. Next rung after landed pi-sandbox
   (per-bead only). Size L; scoping stub only.
4. **Enforce comms subscription at agent BOOT (code, not seed-prompt)** —
   `plans/2026-06-30-distributed-fleet/04-auto-comms-startup/`. Spawn path wires the comms join at boot so
   agents (Pi + Claude) don't rely on the LLM remembering. Verified: no `comms join` in crewstart.go — it's
   an instruction today. Removes a silent go-dark failure mode. Size S; close to kerf-ready.
5. **Reusable transcript-extraction tool** — `plans/2026-06-25-transcript-retro-tool/`. Extract/normalize
   session-transcript JSONL for repeatable retros. Untouched but **value now marginal** — retro process is
   largely codified in `major-issue-fanout` + `logmine`; confirm non-redundant before investing. Size S.

## Excluded (done/gated/tracked)
code-revamp (all code landed, remainder operator-gated) · keeper-restart-timing (kerf
`2026-07-18-keeper-restart-delivery`) · codex-app-server-replan (beads hk-160yb/hk-5h759/hk-g0ror +
kerf `codex-app-server`) · giant-retirement (landed as Track 3) · assessor-daemon-campaign +
prod-readiness-watch (live ops campaigns).
