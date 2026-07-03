# keeper-redesign — Deployment, Test & Validation Plan (operator-mandated)

The keeper is a PER-SESSION watcher invoked as `harmonik keeper` — it is NOT the daemon, and the daemon supervisor does NOT auto-revive keeper sessions. Deploying a keeper change therefore requires (a) rebuilding the binary AND (b) relaunching the keeper-watched sessions so launchers pass the new flags. Every task below carries a code change, a deployment step, a test step, and a validation step.

## DEPLOYMENT (live fleet)
1. After each core bead merges to main: `go install ./cmd/harmonik` (rebuild).
2. Relaunch keeper-watched sessions so they pass `--session-id <uuid4>`:
   - captain: `~/.claude/captain-tools/captain-launch.sh` mints a UUIDv4 (line 56) -> wire that SID into `harmonik keeper --session-id $SID`.
   - crews: `crewstart.go resolveSessionID` -> thread the SID into the keeper launch.
   - hooks: `keeper-stop-hook.sh` / `keeper-precompact-hook.sh` on the unified `HARMONIK_AGENT` env (hk-p9kw).
3. Confirm live binding: `harmonik keeper doctor --agent <name>` reports BoundSessionID == the minted UUIDv4 and `.managed` contains that lowercase UUIDv4 (NOT scraped from a transcript filename).
4. Rollback: keeper changes are per-session; revert = `go install` of the prior SHA + relaunch. No daemon restart, no schema migration.

## TEST (automated, per bead — gated BEFORE reviewers by opus-triple-review.dot commit_gate)
- Unit: `go test ./internal/keeper/...`
- Integration: `go test -tags=integration ./internal/keeper/...` (real throwaway tmux pane `hksav-twin-`, no daemon).
- RED-THEN-GREEN per bead: the FIRST worktree commit is a test that FAILS on today's main.
- Standing invariants asserted by EVERY bead: defaults-PIN (Act 300k/0.85, Warn 270k/0.70, Force +40k/0.95 — NO band retune), WriteManagedSessionFn-called-once / no-auto-clear, gauge-state-transition table.

## VALIDATION (end-to-end — proves it actually works, not merely compiles)
1. Phase-0 empirical gate (P0-B, BEFORE any deletion): prove `--session-id` survives `/clear` (capture `.ctx` before+after, assert equal). If false, STOP + surface to captain.
2. Headline acceptance gate (DETERMINISTIC): extended `cycle_twin_e2e_integration_test.go` in the operator's REAL environment (high context >=300k on a 1M window + gauge-stale-while-pane-alive + idle/remote-control client) asserts the full handoff -> confirm-nonce -> /clear -> SID-flip -> /session-resume cycle completes and rebinds `.managed`. RED on main; GREEN only after all P0/P1 land.
3. Net-LOC check (P0-B "break the pattern" gate): `git diff` confirms watcher.go:664-888 heuristic block + ~6 config knobs + rebind CLI are DELETED; net LOC for identity machinery is NEGATIVE.
4. LIVE-SOAK (manual, INDEPENDENT verifier — NOT the implementer): reproduce stale-gauge-on-live-agent; confirm the gauge writer keeps the gauge fresh through a >5-min idle window AND across a `/clear`. A green suite alone is insufficient for this subsystem.
5. Live event-trace: after deploy, `harmonik subscribe --types session_keeper_*` over a real warn->act->clear->resume cycle shows `cycle_complete` (no gauge-NA stall, no operator-attached false-suppress).

---

## Per-bead expansion (deploy / test / validate specifics)

The five sections above are the standing mechanics. Below, each bead's specifics — what to
rebuild, what to relaunch, the RED-THEN-GREEN test, and the validation that proves it.

### hk-7rmv — P0-B authoritative UUIDv4 + DELETE 664-888 (LANDS FIRST)
- **Deploy:** `go install`; relaunch captain via `captain-launch.sh` (UUIDv4 minted line 56 → `keeper --session-id $SID`) and crews via crew start. No daemon restart.
- **Test:** Unit — `WriteManagedSessionFn` called exactly once; transcript-UUID NEVER written to `.managed`; grep gate that deleted identifiers (latchSuppressed, consecutiveRapidClears, lastAutoClearAt, consecutiveForeignTicks, suppressedStableSID, consecutiveStableSuppressedTicks, StaleBindingThreshold, AutoClearCooldown, AutoClearMaxAttempts, SuppressRecoverThreshold, Write/Clear/ReadSuppressFn, isUppercaseUUID, `keeper rebind`) = 0 hits. Integration — bind from passed `--session-id`.
- **Validate:** Net-LOC NEGATIVE (`git diff --stat internal/keeper/ cmd/harmonik/keeper_cmd.go`). Phase-0 empirical gate BEFORE merge: `--session-id` survives `/clear` (`.ctx` equal before+after) — STOP if false.

### hk-baf4 — twin harness flags --emit-na / --suppress-statusline-after
- **Deploy:** build-only (`go build -tags=integration ./internal/keeper/...`); nothing relaunched (test-scoped).
- **Test:** Integration smoke — twin honors each flag (emits NA / stops `.ctx` writes after the delay).
- **Validate:** flags consumed by hk-81wk + hk-nlio; no prod-behavior diff outside the harness file.

### hk-psds — CLI flag-parity (--agent vs positional)
- **Deploy:** `go install`; relaunch any launcher using old positional form.
- **Test:** Unit table-test per subcommand — `--agent foo` parses identically across enable/doctor/set-dispatching/clear-dispatching; leading-dash value rejected. Misparse case RED on main.
- **Validate:** `doctor --agent X` and `set-dispatching --agent X` resolve the SAME agent live; no silent-empty-agent path.

### hk-p9kw — hooks env-var unification onto HARMONIK_AGENT
- **Deploy:** `go install`; re-run `harmonik keeper enable --agent <name>` per session (rewrites hook scripts + settings.json export); relaunch sessions.
- **Test:** Unit — `keeper_enable_doctor_cmd_test.go` env assertions on `HARMONIK_AGENT`; crew idle/precompact marker correlates to its gauge agent (RED on main).
- **Validate:** crew keeper emits gauge + idle marker under the SAME agent; `doctor --agent <crew>` shows no de-correlation `no_gauge`.

### hk-81wk — gauge-liveness DeriveOccupancyFromTranscript (depends hk-7rmv)
- **Deploy:** `go install`; relaunch sessions; confirm `no_gauge:stale` stops on a live pane via `harmonik subscribe --types session_keeper_*`.
- **Test:** Integration (uses hk-baf4 `--suppress-statusline-after`) — suppress twin statusline writes, assert keeper refreshes `.ctx` from transcript, gates still evaluate, fresh gauge NOT clobbered. Unit — `DeriveOccupancyFromTranscript` plausible on a fixture transcript.
- **Validate:** LIVE-SOAK (independent verifier) — stale-gauge-on-live-agent reproduced; `.ctx` mtime current through a >5-min idle window. Defaults-PIN green.

### hk-0t5s — OperatorAttached → activity-recency (depends hk-7rmv)
- **Deploy:** `go install`; relaunch (captain under iOS/remote workflow especially).
- **Test:** Integration — idle/detached-feeling client attached → NO suppress (RED on main); simulated recent activity → suppress. Unit — recency predicate truth table. Apply at all 3 sites.
- **Validate:** Live event-trace over warn→act with a remote-control client: NO operator-attached false-suppress; captain not parked warn-only.

### hk-75mr — P0-A gauge-independent live-pane recovery (depends hk-7rmv + hk-0t5s)
- **Deploy:** `go install`; relaunch sessions.
- **Test:** Integration (uses hk-baf4 `--emit-na`) — kill twin gauge, pane alive: `maybeLivePaneRecover` fires the gated restart only when preconditions hold AND operator not recently active.
- **Validate:** LIVE-SOAK — dead-gauge-live-pane recovers without depending on the gauge and without a false restart during operator activity.

### NEW-forcerestart — wire ForceRestartFn behind --force-restart (TO BE FILED; depends hk-7rmv + hk-75mr + hk-0t5s)
- **Deploy:** `go install`; relaunch with `--force-restart` where gauge-independent recovery is wanted (default OFF).
- **Test:** Integration — flag OFF → NO restart even on dead-gauge-live-pane; flag ON + preconditions met → restart fires. Unit — default OFF; fail-closed truth table.
- **Validate:** Live — armed flag recovers a dead-gauge-live-pane; unset degrades to warn-only (no surprise restarts).

### hk-nlio — VALIDATION GATE acceptance test (RED on main, GREEN after all P0/P1)
- **Deploy:** test-scoped; `go build -tags=integration ./internal/keeper/...`. Runs in opus-triple-review `commit_gate` before reviewers.
- **Test:** `go test -tags=integration ./internal/keeper/ -run CycleTwinE2E` — full handoff→confirm-nonce→/clear→SID-flip→/session-resume under high-ctx >=300k on 1M window + gauge-stale-pane-alive + idle/remote client (staged via hk-baf4 flags). RED on main; GREEN only after all P0/P1 merge.
- **Validate:** gate DETERMINISTIC (no daemon, no 30-min budget, real `hksav-twin-` pane); PAIR with manual LIVE-SOAK — green suite alone INSUFFICIENT.

### hk-bpkv — P3 consolidate threshold constants (NO value change)
- **Deploy:** `go install`; relaunch (no behavior change).
- **Test:** defaults-PIN test asserts every constant unchanged (RED if any value moves); unit test that watcher + cycle resolve from the SAME source.
- **Validate:** status-line behavior unchanged (27% warn on 1M window); `git diff` shows no numeric literal changed.
