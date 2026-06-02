# Cluster: harmonik backlog — tasks, features, misc (P2–P4)

Assessor: backlog2. Read-only. 10 beads assessed against current code/specs on `main`.

### hk-0ziuw — supervise start: fail-closed when no credential source resolves (CI-006)
- VERDICT: DONE
- ACTION: br close (landed as d1ffb5b4 — `--require-api-key` flag on main)
- NEW_PRIORITY: -
- EVIDENCE: `cmd/harmonik/supervise/start.go` on main HAS the flag end-to-end — `var requireAPIKey` (L44), `--require-api-key` parse (L54-55), `resolveAPIKey(projectDir, requireAPIKey)` (L161), and the fail-closed error "no ANTHROPIC_API_KEY source resolved…" (L284). Confirmed via `git show main:cmd/harmonik/supervise/start.go`. Commit `d1ffb5b4` is on main. The bead's "merge-to-main failed: rebase_conflict" note refers to a LATER duplicate re-attempt (`0e31ea73`, sits only on an orphan run/* branch) — the original work already landed. Bead is stale-open.
- CONFIDENCE: high

### hk-ynjnf — explore: no-auto-pull daemon boot + historical-topology boot (EM-066)
- VERDICT: APPROACH-STALE
- ACTION: revise desc: the flag surface it asks to "observe" shipped + is test-covered; narrow to the only-uncovered facet (manual historical-topology boot observation) or close. Otherwise downgrade.
- NEW_PRIORITY: P3
- EVIDENCE: EM-066 behavior is now the DEFAULT (`b74826d6`/`f66be371` hk-8vy18 flipped auto-pull OFF; `--auto-pull` is opt-in) and is codified in `internal/daemon/noautopull_em066_em067_test.go` (commit `9d0fda7d`, on main): TestScenario_NoAutoPull_ZeroRunsStarted_EM066 asserts zero run_started + Ready() never called over the observation window — exactly the "zero-dispatch quiet behavior" the bead wants observed. The "--no-auto-pull/--queue-only flag surface" premise the bead reads from no longer matches (it's now the default, not a special boot mode). Only the "historical-topology boot" half is uncovered. As a manual exploratory-test bead its value is now marginal vs the automated coverage.
- CONFIDENCE: med

### hk-m8zqv — Integration smoke: 4h unattended flywheel run + CL conformance scenarios
- VERDICT: KEEP
- ACTION: none — keep (all 7 blocking deps now CLOSED; bead is unblocked and actionable)
- NEW_PRIORITY: P2
- EVIDENCE: All deps closed — hk-62w9a, hk-umzwe, hk-qx702, hk-xrygh, hk-6ul9r, hk-50dxt, hk-e3bnw all show CLOSED (`br show`). The flywheel/supervise substrate it smoke-tests is built; the persistent daemon + `harmonik supervise` are live in production this session. This is genuine unbuilt integration-test/runbook work (deliverable: `.flywheel/runbooks/v0.1-smoke.md`, no such file exists). Scope and P2 still correct now that it's unblocked.
- CONFIDENCE: high

### hk-1k5as — named-queues CLI polish: list mislabels --queue submission as 'main'; status/append ignore --queue-id
- VERDICT: KEEP
- ACTION: none — keep; note sibling-overlap with hk-40r9b (fold into the same name-routing fix effort)
- NEW_PRIORITY: P3
- EVIDENCE: Distinct facet of the named-queue CLI name-routing surface, NOT a duplicate of hk-40r9b. hk-40r9b (P2, in_progress) = submit/dry-run DROPS the name → request collapses to main. hk-1k5as = the read/target side: `queue list` mislabels, and `--queue-id` is unwired — `internal/queue/cli/status.go:23` literally comments "`--queue-id <uuid>` optional; for future filtering (currently informational)". Real unfixed gap. The analogous cancel-verb bug was just fixed (`66b6836d` "honour the <name> positional arg instead of always cancelling main"), so list/status/append are the next siblings in the same surface. P3 polish framing correct (functionally two queues coexist + dispatch fine — display/routing only).
- CONFIDENCE: high

### hk-nlhys — CAPTURE-ONLY inventory of emergent tooling/safety patterns
- VERDICT: KEEP
- ACTION: none — keep (intentionally-open multi-day aggregation bead, per task instruction; serving its purpose)
- NEW_PRIORITY: P3
- EVIDENCE: Bead body is an actively-growing inventory with a 2026-06-02 comment folding in the peer's `capture-daemon-lane.md` contributions (credit-burn guard, reconciler/orphan-sweep, dispatch-guard caveat, named-queue lifecycle flakiness, dormant auto-tune lane). `contrib-open` label + "@named-queues please append" prompt confirm it's a live collaboration surface, not stale. Last updated today. Still serving its capture purpose; do not formalize/dispatch yet by design.
- CONFIDENCE: high

### hk-ulp7v — Rename workloop.go NewRunRegistry→newLocalRunRegistry; document non-shared
- VERDICT: KEEP
- ACTION: none — keep; deferral-for-operator still right, but it's a 1-line safety-rename: dispatch-eligible any time
- NEW_PRIORITY: P3
- EVIDENCE: NOT done — `internal/daemon/workloop.go:518` still calls `NewRunRegistry()` (un-renamed), and `internal/daemon/daemon.go:656` has the shared `sharedRunRegistry := NewRunRegistry()`. The two-call silent-desync hazard the bead names (composition-root-wiring-map.md) still exists. HANDOFF.md L23 explicitly lists hk-ulp7v as a deferred-for-operator item. It's a pure-mechanical rename + comment; deferral is "fine to hold" but there's no real operator decision here — could land in any cleanup batch. P3 correct.
- CONFIDENCE: high

### hk-0zxv6 — HITL decision-surfacing: aggregate decisions-needed-by-human
- VERDICT: KEEP
- ACTION: none — keep; it's a fresh (2026-06-02) idea write-up, likely-future kerf work; consider routing the design home to kerf
- NEW_PRIORITY: P3
- EVIDENCE: Created today, `contrib-open`, explicitly "idea write-up… likely a kerf work later." Its referenced dual (hk-uxm0j, the agent↔agent comms bus) is now CLOSED/live — so the agent↔human dual it proposes (a `decision_needed` event on the same bus + `harmonik decisions` surface) is a coherent, un-started next step, not superseded. Premise holds; nothing built. P3 idea-stage correct.
- CONFIDENCE: high

### hk-bm9qm — Handler-pause research: cross-handler task transfer when one handler paused
- VERDICT: KEEP
- ACTION: none — keep (research-only memo, post-MVH scope, unwritten)
- NEW_PRIORITY: P3
- EVIDENCE: Research deliverable (`docs/research/` memo) does not exist — `docs/research/` has no cross-handler/fallback-agent_type file. The source it cites, `specs/handler-pause.md §9.2` "Cross-handler task transfer (research-only)", is present and still explicitly marks the agent_type-fallback-list question as "research-only at MVH" / would touch `execution-model.md §4.2`. Premise intact, no implementation, correctly `scope:post-mvh`. P3 fits a research question with no downstream blocker.
- CONFIDENCE: high

### hk-x6j6r — Move RedactionRegistry + DeadLetterSink from handlercontract to core (eventbus layering)
- VERDICT: KEEP
- ACTION: none — keep; deferral-for-operator still right (it's a layering/architecture move). Priority correct.
- NEW_PRIORITY: P3
- EVIDENCE: NOT done and explicitly tracked as live debt. `internal/eventbus/busimpl.go:11` still imports `internal/handlercontract`; `RedactionRegistry` lives in `internal/handlercontract/redactionregistry.go` and `DeadLetterSink` in `internal/handlercontract/deadlettersink.go` — neither is in `internal/core`. `.golangci.yml:156-168` documents this as "KNOWN SPEC DRIFT (hk-obb5w): eventbus currently imports handlercontract… these types should move to core… Tracked in follow-up bead hk-x6j6r" (added by `7a77ade3`, which *accommodated* the violation via allow-list rather than fixing it). HANDOFF.md L23 lists it as deferred, "may want operator input." Deferral correct (a layering migration is a deliberate architecture call), P3 correct.
- CONFIDENCE: high

### hk-zueat — real auto_status: derive non-SUCCESS outcome from work-product/embedded signal
- VERDICT: KEEP
- ACTION: none — keep (genuine future feature, reserved-not-built; P4 correct)
- NEW_PRIORITY: P4
- EVIDENCE: Unbuilt. Spec firmly reserves it: `specs/workflow-graph.md §4 WG-041` ("`auto_status` is NOT accepted as a node attribute at v1… reserved for a future status-derivation feature"). Code enforces the reservation, not the feature: `internal/workflow/dot/parser.go:699-709` is the REJECT handler ("attribute `auto_status` is reserved-and-rejected at v1"). The parent bead hk-gv5n5 ("attractor-parity v2: real auto_status…") is CLOSED but its body says "T7 v2 follow-up (FILE, do not implement)… Reserved at v1. Defer." — i.e. hk-gv5n5 closed precisely *by filing this carrier bead*; not a duplicate, hk-zueat is the live future-work record. P4 backlog framing correct (no demand yet; multi-spec design questions open).
- CONFIDENCE: high

## Cluster summary

Counts per verdict (10 total):
- DONE: 1 — hk-0ziuw (`--require-api-key` landed as d1ffb5b4; bead is stale-open)
- APPROACH-STALE: 1 — hk-ynjnf (EM-066 no-auto-pull is now the default + test-covered in `noautopull_em066_em067_test.go`; the special-flag-surface premise is stale; only the manual historical-topology-boot half remains, marginal)
- KEEP: 8 — hk-m8zqv, hk-1k5as, hk-nlhys, hk-ulp7v, hk-0zxv6, hk-bm9qm, hk-x6j6r, hk-zueat
- OBSOLETE / DUPLICATE / REPRIORITIZE: 0

Cross-bead themes:
- **Named-queue CLI name-routing surface** has three live sibling bugs, NOT duplicates: hk-40r9b (P2, in_progress — submit/dry-run drops the name) and hk-1k5as (P3 — list mislabels + `--queue-id` unwired), with the cancel-verb facet already fixed (`66b6836d`). Recommend fixing hk-1k5as in the same effort as hk-40r9b. The `--queue-id`-informational comment at `status.go:23` is the concrete unfixed anchor.
- **"Deferred-for-operator" trio (hk-ulp7v, hk-x6j6r, hk-ymav1)** — for the two in my cluster: deferral is *correct* for hk-x6j6r (a deliberate layering/architecture migration, real debt, properly recorded in `.golangci.yml` + HANDOFF). For hk-ulp7v the deferral is *low-stakes* — it's a 1-line mechanical safety-rename with no genuine operator decision, so it could land in any cleanup batch rather than waiting; priority P3 is fine either way. (hk-ymav1 not in my cluster; its blocking dep hk-ohiaf set-concurrency is now CLOSED/landed `1cc2b88e`, so it's newly unblocked — flag for the cluster that owns it.)
- **Three "future/reserved" beads are correctly parked, not stale**: hk-zueat (auto_status, spec-reserved + parser actively rejects), hk-bm9qm (research-only memo, post-MVH), hk-0zxv6 (today's idea write-up). None superseded; all premises verified intact against current code/spec.
- **hk-0ziuw is the only false-open**: implementation on main, bead never closed — the "rebase_conflict" note misled (it was a duplicate re-dispatch, not the landed work).
