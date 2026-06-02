# Backlog cluster 1 — harmonik epics + P1 items (assessor: backlog1)

Investigation date: 2026-06-02. Read-only. Verified against current code/specs/children, not bead text.

### hk-o52fm — Land the SDLC workflow corpus (specs/examples fixtures + scenario tests)
- VERDICT: DONE
- ACTION: br close (all 21 children CLOSED; corpus landed in specs/examples/)
- NEW_PRIORITY: -
- EVIDENCE: All 21 children hk-o52fm.1–.21 are CLOSED (br show each = CLOSED). `specs/examples/` has 25 .dot fixtures including every named corpus file (dual-review-consolidate, implement-review-fix, plan-review-loop, security-review-loop, …, quality-gate-policy, sentry-triage-faithful). Recent landings: deed4fda (hk-o52fm.20), f7204cf4 (hk-o52fm.21), 36406540 (hk-o52fm.19). The 5 SOON capability beads were the close-condition gate; with all 21 fixture children closed the epic is stale-open.
- CONFIDENCE: high

### hk-8fa9a — Design: .beads/issues.jsonl worktree-merge story (child-bead-spawn safety)
- VERDICT: DONE
- ACTION: br close (design deliverable complete — kerf work `bead-ledger-worktree-merge` reached `ready` 2026-05-30; implementation tracked separately)
- NEW_PRIORITY: -
- EVIDENCE: Bead's explicit NEXT STEP was "spawn kerf work `bead-ledger-worktree-merge` once research lands; this bead is the umbrella tracking that work." That kerf work EXISTS at ~/.kerf/projects/gregberns-harmonik/bead-ledger-worktree-merge/ and its SESSION.md records "Completed: 2026-05-30 … Pass 8 (ready): Square check passes" with all 8 passes done (problem-space → research R1–R4 → design → spec-draft → integration → 5 impl beads filed → ready). The implementation subcommand has even begun landing (`cmd/harmonik/beadsmerge.go` exists). The DESIGN epic is satisfied; remaining work (daemon swap from `--theirs` to import-only) belongs to the kerf work's impl beads, not this umbrella.
- CONFIDENCE: high

### hk-fgy9o — Test uplift epic: lifecycle subsystem crash-recovery undertested
- VERDICT: DONE
- ACTION: br close (lifecycle uplift landed; coverage 81.6%, dedicated uplift test references the bead)
- NEW_PRIORITY: -
- EVIDENCE: Bead premise was "5.7K LOC, 6 unit tests, crash recovery sparsely covered." Now internal/lifecycle/ has 69 test files at 81.6% coverage (coverage.baseline:32). All three required deliverables are covered: (a) pidfile lock (pidfilelock_pl001/pl004_test.go), zombie detection (zombiedetection_pl016_test.go), immediate abort (immediateabort_pl012_test.go); (b) recovery sequencing (crashrecovery_pl024_pl025_pl026_test.go, stalecrashrec_pl024_test.go, startupsequence_pl005_test.go); (c) PL-series crash-recovery specs covered. A test named for this exact bead exists: internal/lifecycle/crashrecovery_uplift_hkfgy9o_test.go with header "Step (a) of hk-fgy9o". Has no open children. The original gap is closed.
- CONFIDENCE: high

### hk-tigaf — Named queues epic
- VERDICT: KEEP
- ACTION: none — keep open until sole remaining child hk-tigaf.11 (deferred P3) resolves; otherwise the SC1–SC5 work is fully landed. (Alternatively close epic + leave .11 standalone — assessor recommends KEEP for the one open child.)
- NEW_PRIORITY: -
- EVIDENCE: 10 of 11 children CLOSED (hk-tigaf.1–.10). Named-queue code landed: internal/queue/ (persistence.go, validation_namedqueues_hktigaf2_test.go, name-keyed QueueStore), per-queue routing/pause/resume verbs, two-level capacity gate (workloop.go selectNextQueue + effectiveMax). Recent: 85593a20 (close hk-xyycc subsumed by landed per-name queues). Only hk-tigaf.11 (NQ-X1 per-queue spend caps, deferred P3, blocked on .10 which is now closed) remains open. SC1–SC5 scenario tests all closed. Keep the epic alive only as the parent of the one deferred child.
- CONFIDENCE: high

### hk-j3hrn — Core subsystem coverage uplift 73.1%→>85%
- VERDICT: KEEP
- ACTION: none — gap still real (core at 73.0%, target >85%)
- NEW_PRIORITY: -
- EVIDENCE: coverage.baseline:26 shows `internal/core 73.0` — essentially unchanged from the 73.1% the bead cites (2026-05-20). The >85% target is NOT met. No children to close it out; the uplift simply hasn't been done. Approach (per-package floor tracked in coverage.baseline) is still valid — that file is live and ratcheting (header cites hk-41cns). Note for apply phase: daemon at 52.2% and queue at 69.4% are the bigger drags now, but those are separate packages outside this bead's "core" scope.
- CONFIDENCE: high

### hk-lj0pb — extqueue v0.1 implementation
- VERDICT: DONE
- ACTION: br close (all 24 children CLOSED; spec + code + CLI + scenario tests landed)
- NEW_PRIORITY: -
- EVIDENCE: All 24 children CLOSED (br show hk-8vokz, hk-eblue, hk-nomxl, hk-45ude, … hk-te8zy = CLOSED). specs/queue-model.md present (73KB, updated 2026-06-02). internal/queue/ package fully built (types.go, validation.go, rpc.go, persistence.go, cli/{submit,cancel}.go). The extqueue follow-up cluster the task prompt flagged (hk-c6grw/hk-cb5ow/hk-febd6/hk-40r9b) landed this session — e.g. 6232b303 (hk-febd6 pre-claim guard). The named-queues epic (hk-tigaf) explicitly "Layers on extqueue v0.1 (hk-lj0pb) … all landed." Stale-open umbrella.
- CONFIDENCE: high

### hk-iuaed — imrest: bead in_progress as activity-marker; orphan reset on restart
- VERDICT: DONE
- ACTION: br close (all 6 children + blocking dep hk-11xkn CLOSED; spec + sweep code landed)
- NEW_PRIORITY: -
- EVIDENCE: All 6 children hk-iuaed.1–.6 CLOSED and the blocking dep hk-11xkn CLOSED. Spec amendments landed (specs/beads-integration.md + specs/process-lifecycle.md contain the activity-marker/BI-010d text — grep hit). Code landed: internal/lifecycle/orphansweep.go, orphansweepbeads.go, beadsowned_hk11xkn_test.go, provenance.go; specaudit/bi010d_reset_adapter_sensor_test.go. The hk-11xkn sentinel ProvenanceChecker landed this session as 5c51df8f / 5d67ccaa. Epic fully satisfied.
- CONFIDENCE: high

### hk-tigaf.11 — NQ-X1 deferred: per-queue spend budget caps
- VERDICT: KEEP
- ACTION: none — keep open (legitimate deferred P3 follow-up; its blocker hk-tigaf.10/NQ-E1 is now closed → newly actionable)
- NEW_PRIORITY: -
- EVIDENCE: Single global spend meter exists (bead cites internal/daemon/spendmeter_hkk3f8g.go); per-queue caps explicitly deferred as v0.1 non-goal N2, documented as the user-flagged follow-up. Its blocker hk-tigaf.10 (NQ-E1 dedicated investigate-queue) is now CLOSED, so the "feature must exist first" precondition is met — it is unblocked and actionable, correctly P3. No reprio needed.
- CONFIDENCE: high

### hk-ekap1 — Auto session-lifecycle for long-running agents (context-threshold handoff)
- VERDICT: KEEP
- ACTION: none — keep open (not started; feasibility-confirmed design, no code yet)
- NEW_PRIORITY: -
- EVIDENCE: No implementation exists — grep for session-keeper/used_percentage/statusLine watcher finds only incidental spec mentions (operator-nfr.md statusLine, not the context-threshold watcher). Created 2026-06-02 (fresh), feasibility confirmed 2026-06-01 with a detailed buildable mechanism (statusLine→file→watcher, Stop-hook idle gate, PreCompact backstop). Reuses live primitives (pasteinject, event bus, session-handoff/resume skills, harmonik supervise). Premise holds — the persistent daemon and supervise are live, which this extends from implementer-claudes to orchestrator-claudes. Sibling hk-uxm0j (agent-comms) already landed this session, validating the supervisor-coordination pattern. Likely a kerf work (codename session-keeper) — P2 reasonable.
- CONFIDENCE: high

### hk-ymav1 — Auto-tune --max-concurrent to subscription bandwidth
- VERDICT: KEEP
- ACTION: none — keep open; its blocker hk-ohiaf (runtime set-concurrency) is now CLOSED → unblocked and ready
- NEW_PRIORITY: -
- EVIDENCE: The auto-tuner is NOT built — workloop.go has only the static maxConcurrent gate + effectiveMax (workloop.go:733-750); no rolling-5h token-rate tuner, no --subscription-token-ceiling flag (grep negative). Its hard dependency hk-ohiaf ("queue set-concurrency N: runtime-adjustable ceiling, drain-down") is now CLOSED (commit 1cc2b88e), and workloop.go:169-178 confirms maxConcurrent is now a controller-read runtime value (the hook this bead needs). So the prerequisite manual knob exists; this bead adds the automatic feedback layer on top. Created 2026-06-02 (fresh), approach still valid (ccusage-style ~/.claude transcript token-rate signal + 429 backstop via existing DetectRateLimit). P2 reasonable.
- CONFIDENCE: high

## Cluster summary

Counts per verdict:
- DONE: 5 — hk-o52fm (SDLC corpus), hk-8fa9a (beads-JSONL-merge design), hk-fgy9o (lifecycle test uplift), hk-lj0pb (extqueue v0.1), hk-iuaed (imrest)
- KEEP: 5 — hk-tigaf (named-queues, 1 open child), hk-j3hrn (core coverage, gap real), hk-tigaf.11 (deferred P3, now unblocked), hk-ekap1 (session-keeper, not started), hk-ymav1 (auto-tune, now unblocked)
- OBSOLETE / DUPLICATE / APPROACH-STALE / REPRIORITIZE: 0

Cross-bead themes:
- **Four stale-open umbrella epics** (hk-o52fm, hk-fgy9o, hk-lj0pb, hk-iuaed) all have 100% of their children CLOSED and verified landed code/specs — same pattern as the hk-uxm0j precedent the rubric cites. These are the highest-confidence closes in the cluster.
- **hk-tigaf (named-queues) is ~91% landed** — 10/11 children closed; only the deferred P3 spend-cap child (hk-tigaf.11) keeps it open. extqueue v0.1 (hk-lj0pb) is its now-landed foundation, so closing hk-lj0pb is safe.
- **Two coverage epics diverged**: hk-fgy9o (lifecycle) is DONE (81.6%, dedicated uplift test landed) but hk-j3hrn (core) is genuinely NOT done (still 73.0% vs >85% target) — do not lump them together.
- **Two fresh 2026-06-02 features (hk-ekap1, hk-ymav1) both just got unblocked** by this session's landings: ymav1 by hk-ohiaf (set-concurrency) closing, ekap1 by the persistent-daemon/supervise/agent-comms infrastructure going live. Both are correctly-scoped KEEPs with no code yet.
- **hk-8fa9a nuance**: closing it as DONE closes the *design* obligation only; the daemon still uses `git checkout --theirs` for JSONL conflicts. The corrective implementation (import-only / beads-merge subcommand) lives in the kerf work's 5 impl beads — confirm those are still tracked before closing, so the impl doesn't get orphaned.
