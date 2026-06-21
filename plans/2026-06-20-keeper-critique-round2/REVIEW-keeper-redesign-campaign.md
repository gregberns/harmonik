# Keeper-redesign campaign — skeptical review + verification (2026-06-20)

Reviewed epic hk-gffc (10 commits on main) per HANDOFF-keeper DoD: critics + architect + QA,
treated skeptically, actual testing as definition-of-done.

## Method
5 parallel review agents (restart/idle/blind state machine; heartbeat/noise/foreign_session;
test-quality QA; band-guardrail + EV-036 verifier; captain-tools shell + per-crew keeper) +
actual test runs (build, vet, short unit, real-env integration with tmux leak-guard).

## Test results (actual)
- `go build ./...` clean; `go vet` clean.
- `go test -short ./internal/keeper/... ./internal/core/...` → PASS (keeper 2.9s, core 2.6s).
- `go test -tags integration -run TestIntegration_Watcher_{GaugeStaleAlive,HighCtxWarnThreshold,BlindAlarmFires}`
  → PASS (8.0s), **zero tmux leak** (0 sessions before and after).
- Pre-existing reds (NOT campaign-caused, left untouched):
  - `TestIntegration_OperatorAttached_SuppressesAndResumes`, `TestIntegration_LivePaneRecover_FiresOnStaleGaugeLivePane` (handoff-flagged).
  - `TestWorkflowModeDot_ValidRefPassesFlagParse` (hk-4fvid) — dies "open terminal failed: not a terminal" in non-TTY shells; environmental, not keeper.

## Verified clean
- **Band guardrail INTACT**: warn=200K / act=215K / force=240K unchanged; only 3f60cf23 touched
  thresholds.go, adding the separate 280K hard-ceiling. Locked guardrail held.
- **EV-036 + rename**: no registered payload has a token/secret field; Tokens→ContextLen rename complete.
- **Per-crew keeper**: captain-launch.sh copies identical; verified-restart loop genuine + bounded (30s
  await-ack timeout); .sid seeded before keeper spawn; `--warn-only` blocks every restart/respawn path
  (no fork-bomb); crew-stop kills the crew-specific `hk-keeper-<name>` session, not a shared `*-default`.
- **Heartbeat/noise/foreign_session**: no impactful defect; miss-budget suppression self-corrects.

## Issues found → RESOLVED this session
- **hk-4i0s (P1, real defect, FIXED 4349f061)**: RunForIdle armed the 30-min idle cooldown *before*
  runCycle, so an aborted cycle (handoff-nonce timeout) wedged the idle crew for 30 min. Fix: unwind the
  stamp on `lastFireWasAbort`. Test + independent review APPROVE.
- **hk-gkyd (P2, false-green tests, FIXED f6903cf8)**: 4 tests passed regardless of correctness
  (tautological crew-name test; dead warn-only respawn spy; self-hint test gated off via TmuxTarget:"";
  blind-alarm emission never covered in CI). Strengthened all 4 (fail-before/pass-after) + 2
  behavior-preserving seams (crewKeeperSessionName, SelfHintInjectFn). Independent review APPROVE.

## Noted, out of scope (filed / deferred)
- **hk-wqdc (P3, pre-existing, OPEN)**: 2 emitted keeper events unregistered (session_keeper_live_pane_recover,
  session_keeper_ack_timeout) → won't consumer-decode. Predates campaign.
- Idle-restart "thin band" only on a 200K window; live deployment is 1M window → idle band [150K,215K) is
  healthy. Robustness note, no fix.
- Suggested follow-ups: surface per-crew keeper-spawn failure in `crew list`; confirm crew gauge wiring via
  `keeper doctor` on the live deployment.
