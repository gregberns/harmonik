# Captain & Crew — Analysis (current-state map)

Factual map of the areas this work touches. Most capabilities already exist; the net-new
surface is small. Every claim below is traceable to a file:line from the grounding pass.

## Affected areas

### A1. Event system + `epic_completed` (NET-NEW emission)
- Event-type constants: `internal/core/eventtype.go` — `bead_closed` at `:80`,
  `run_completed`/`run_failed` at `:31/:35`, `run_stale` at `:655`, `reviewer_verdict` at
  `:531`. Payload registry: `internal/core/eventreg_hqwn59.go`.
- Emit helper `emitBeadClosed(...)`: `internal/daemon/workloop.go:3981`, called from the
  merge/close path at `workloop.go:1784,1846,1934,1947,2486,2521,2538`, each right after
  `deps.brAdapter.CloseBead(...)` (`workloop.go:1780`).
- `BeadClosedPayload` carries ONLY `RunID` + `BeadID`
  (`internal/core/agentlifecyclepayloads_gjyks.go:387-395`) — **no parent/epic field, and
  no close-cascade logic anywhere** (grep: zero `epic` hits in daemon/queue/core source).
- Subscribe surface: client `cmd/harmonik/subscribe.go:12`; wire contract
  `internal/daemon/subscribe.go:91` (empty `Types` = all). New event type would flow
  through `subscribe --types epic_completed` for free.
- **Constraint / where to build:** to know an epic finished, emit at the bead-close site —
  after `emitBeadClosed`, query the closed bead's parent and its remaining open children via
  the br adapter; if none open, emit `epic_completed{epic_id}`. Parent/child edges exist in
  br JSON (`dependency_type:"parent-child"`) but the daemon never reads them for close today.

### A2. Session spawn substrate (NET-NEW: persistent variant)
- Spawn: `tmuxSubstrate.SpawnWindow` `internal/daemon/tmuxsubstrate.go:309` → `NewWindowIn`
  `:381` (`tmux new-window` in the daemon's own session). Naming:
  `internal/lifecycle/tmux/windowname.go:36`. Launch argv: `buildClaudeLaunchSpec`
  `internal/daemon/claudelaunchspec.go:220` — `--session-id <uuid>` (fresh) or
  `--resume <uuid>` (`:357-362`). Initial prompt pasted via `pasteinject.go:878`.
- tmux drive: `internal/lifecycle/tmux/osadapter.go:352` (SendKeysLiteral), `:384`
  (SendKeysEnter), `:406` (SendKeysQuit); targeting by `paneTarget` captured at spawn
  (`tmuxsubstrate.go:505-511,666`).
- **Constraint:** every run ends `sess.Kill` (`workloop.go:2211,2332,2854`) +
  `removeWorktree` (`:2795,2868`). There is **no persistent / self-submitting spawn path**,
  **no `rename-window/session`**, and targeting is by pane-id, not name. The persistent
  crew-start path is the new lifecycle; it reuses `new-window` + bracketed-paste injection.

### A3. Keeper (DEPENDENCY — not modified here, except pattern reuse)
- `internal/keeper/`: `watcher.go:175` (5s poll of `.harmonik/keeper/<agent>.ctx`),
  `cycle.go:267` (`runCycle`: handoff→clear→resume), `injector.go:29-53` (bracketed-paste),
  `gates.go` (CrispIdle), `keeper_cmd.go:99-119` (Cycler wired live, hk-lm9it).
- Gauge: `scripts/keeper-statusline.sh` writes `{pct, session_id, ts}` from the statusLine
  JSON's `.context_window.used_percentage`.
- **Relevance:** the keeper's resume-injection is the exact mechanism the crew-start path
  reuses. Restart itself is in-flight (session-keeper Phase-2, epic `hk-ekap1` live-validation
  open). Token-threshold calibration (90%-of-1M bug) flagged to flywheel — out of this work.

### A4. comms bus (REUSE — tasking + status)
- `cmd/harmonik/comms.go`: `send` `:113`, `recv` `:103`, `log` `:399-552` (scans
  `.harmonik/events/events.jsonl`, durable, no daemon needed). `--topic` is first-class
  (`AgentMessagePayload.Topic`, `internal/core/agentcommspayloads_djqc9.go:77`). Durable
  per-agent cursor: `internal/daemon/commscursor.go:12-14,49`. Handlers:
  `commshandler_nbrmf.go`, `commsrecvhandler_nnwaa.go`.
- **Pattern:** at-least-once delivery, dedupe on `event_id` (agent-comms skill, N3).
  `who` presence is the only ephemeral piece (~120s).

### A5. named queues (REUSE — per-crew work)
- `internal/queue/types.go`: `Name` = stable durable routing key (`:245-260`), persisted to
  `.harmonik/queues/<name>.json` (`internal/queue/persistence.go:66,89`). `Workers` +
  two-level concurrency gate (`:262-276`). Per-queue pause/resume (named-queues NQ-C1).
- **Constraint:** no auto-resume across a *daemon* restart (`specs/queue-model.md:283`).

### A6. beads / `br` (REUSE — roster + journal)
- Roster: `br update --assignee/--owner`, atomic `--claim`. Journal: `br comments add/list`
  (append-only) + structured `--notes/--design/--acceptance-criteria`. SQLite + git-tracked
  `.beads/issues.jsonl`. Adapter: `internal/brcli/`.

## Conventions to follow
- New event = a typed payload struct in `internal/core` + registration in
  `eventreg_hqwn59.go` + a constant in `eventtype.go`; daemon emits via an `emitX` helper.
- New CLI verb = a subcommand under `cmd/harmonik/` (see `comms.go`, `subscribe.go`).
- New subsystem package = `internal/<name>/` per subsystem-organization.md + a depguard
  matrix entry in `.golangci.yml` (the `go-subsystem-add` skill).
- Tests: table-driven Go unit tests beside the code; daemon-level behavior gets a
  `//go:build scenario` test (NOTE: the daemon gate skips scenario tests — run them yourself).

## Recent git activity (affected areas)
- session-keeper Phase-2: Cycler wiring (`hk-lm9it`), PreCompact backstop (`hk-aalsm`),
  anti-loop + crash-recovery (`hk-kct9t`) — all last few days; live-validation `hk-ekap1` open.
- `CHB-023`: persist `claude_session_id` to `Run.context` (implementer sessions, for
  review-loop `--resume`; NOT orchestrator addressing).
- named-queues + agent-comms landed (durable queues + the comms bus this work composes on).
