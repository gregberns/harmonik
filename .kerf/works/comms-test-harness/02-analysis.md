# 02 — Analysis

Condensed from `plans/2026-07-06-quality-system/12-comms-test-design.md` §1–2 (authoritative source read
in full for this pass).

## Affected areas
- `cmd/harmonik/comms.go` (1907 lines) — thin socket client: send/recv/log/join/leave/who, `--follow`,
  `--wake`, `runCommsRecvFollowIO`, `commsWakePaneCandidates`, `resolveProjectPath`.
- `internal/eventbus/{busimpl.go,jsonlwriter.go}` — `NewBusImpl*`, `Subscribe`/`Seal`/`Emit`,
  `ScanAfter(path, sinceID)`.
- `internal/daemon/{commscursor.go,commsrecvhandler_nnwaa.go,subscribe.go,scheduletick.go}` —
  `NewCursorStore`, `HandleCommsRecv`, `NewSubscribeHub(SubscribeHubConfig{Bus, EventsJSONLPath,
  PresenceEmitter, Now, NewTimer})`, `HandleSubscribe(ctx, conn, req)`.
- `internal/presence/presence.go` — `ComputeRegistry`, `GetState/IsOnline/IsStale/IsOffline`,
  `EffectiveLastSeen = max(beat, latest send ts)`, TTL=120s, StaleCutoff=10m.

## Existing test patterns (already in tree — this is harden-and-close-gaps)
- `scenario_comms_recv_follow_gap_hk7xvf_test.go` — lost-gap `ScanAnchor` fallback.
- `scenario_comms_n3_redelivery_dedupe_hkpg0w5_test.go` — single-consumer N3 redelivery+dedupe.
- `commscursor_race_hkfvo9e_test.go` — cursor monotonicity under race.
- `comms_send_scheduled_hk0lwje_test.go` — scheduled-send argv correctness.
- `scenario_comms_recv_follow_e2e_hkyw5c_test.go` — e2e follow.
These four are keep-green sentinels: fold into the epic's CI gate, do not rebuild.

## Injection seams (confirmed in source, already used by shipped tests)
- `eventbus.ScanAfter`, `eventbus.NewBusImpl*` — pure/deterministic, no daemon needed (L0).
- `daemon.NewSubscribeHub` takes `Now`/`NewTimer` as explicit injection points — enables deterministic
  clock-driven tests without real sleeps (L1).
- `net.Pipe()` gives a fake `net.Conn` for `HandleSubscribe` — no real socket needed (L1).
- `runCommsRecvFollowIO(..., w io.Writer)` drives `--follow` output without capturing global stdout (L1).
- `scratch-daemon.sh` — existing clean-reset real-daemon harness, reused as-is for L2 (no new infra).

## Constraints
- `MatchAgentMessage` must be a SINGLE shared predicate (spec N1) — replay, live-offer, and durable
  `comms-recv` must all route through it; a second copy would let catch-up/live message-sets diverge.
- Cursor writes are daemon-owned and shared: both `HandleCommsRecv` (one-shot) and `SubscribeHub` (follow)
  advance the *same* per-agent cursor file. This is a hard constraint the test suite must assert around, not
  route around.
- Presence has no fsync guarantee on `agent_presence` — a daemon crash can drop refresh beats; tests must
  treat this as an accepted characteristic, not a bug to fix.
- `--wake` failures are swallowed to stderr with exit 0 by design (best-effort) — tests must assert this
  behavior, not "fix" it.

## Code health / tech debt relevant to this work
- Two known-but-not-yet-covered semantics (B1 cursor-sharing, B2 idle-follow-no-refresh) are currently
  undocumented in code; this work adds pinning assertions + doc notes as the actual deliverable (not a
  behavior change).
- No existing test asserts N-consumer fan-out — the single clearest NEW gap (G6).

## Recent activity
- `hk-z365` (symlink-hash fix for `resolveProjectPath`), `hk-5xuvc` (backoff reconnect), `EV-037a`
  (heartbeat `last_event_id` anti-regression) are all already landed on `main` per the design doc; this work
  adds regression coverage for them, does not re-implement them.
