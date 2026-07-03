# Captain & Crew — Research findings

Open questions from decompose, resolved against the code + Claude Code docs.

## R1 (C1) — epic-completion query + idempotency
- `br show <parent> --format json` returns `dependents[]` with `status` inline and
  `dependency_type:"parent-child"` (verified vs `hk-tigaf`). One call, not N+1.
- The adapter `internal/brcli/show.go` (`brShowEdge`, `:20-23`) currently parses edges into
  `core.DependencyEdge` and **drops the child `status` field** → C1 must extend the edge
  struct / add a method to surface child status.
- **Idempotency:** out-of-band `br close` (crew on their own queues, or a human) bypasses the
  daemon's one-at-a-time merge lock → sibling race on the "all children closed?" check.
  Strategy: after `CloseBead` + `emitBeadClosed`, re-read the parent; if all children closed
  AND no `epic_completed` already emitted for this epic, emit. Maintain an emitted-epics guard
  (in-memory set, backed by a scan of the event log on boot) for at-most-once.

## R2 (C2) — programmatic long-lived crew + session_id
- `claude remote-control --name <n>` starts a persistent, cloud-watchable,
  programmatically-drivable session (docs: `remote-control.md`). `claude -p --resume <id>
  --output-format json` and the Agent SDK (`resume=<id>`) re-task / read output headlessly
  (docs: `headless.md`, agent-sdk overview). Confirmed by the operator ("we got it working").
- **session_id capture:** from the launch command's JSON stdout / remote-control output.
- **Addressing:** `name` (stable) + `session_id` (current). A keeper restart (`/clear`) may
  rotate `session_id`; the crew registry updates it on each (re)launch — `name` + `queue`
  stay stable. So crew identity for coordination = `name`/`queue`, never the raw `session_id`.
- **Crew registry:** small new durable record per crew member at `.harmonik/crew/<name>.json`:
  `{name, session_id, queue, epic, handle, started_at}`.

## R3 (C2/C3) — keeper integration (the keeper itself is OUT of scope; we provide its inputs)
- The keeper keys anti-loop identity on `session_id` (its gauge file) and injects into a
  `--tmux <target>` it is *handed* (`keeper_cmd.go:102`, `injector.go:25-26`). For crew, C2
  must at spawn: (a) stand up the per-crew gauge (`scripts/keeper-statusline.sh` →
  `.harmonik/keeper/<name>.ctx`), (b) create the `.managed` marker, (c) register the crew's
  pane/handle so the keeper can attach — else the keeper no-ops (`keeper_cmd.go:85`).
- **Dependency:** correct wind-down on 1M-context crew needs the keeper token-threshold fix
  (the 90%-of-1M bug; flagged to flywheel, out of this work).

## R4 (C3) — handoff + progress
- Handoff schema `{crew_name, queue, epic_id, goal, captain_name}`; durable assignment also
  mirrored to beads `--assignee/--owner`. Progress feed = `comms --topic status` +
  `br comments` (both durable, events.jsonl / SQLite-backed).

## Net code surface confirmed small
- **C1:** new event type + payload + emit-at-close + a tiny `brShowEdge` status-field change.
- **C2:** new `harmonik crew start/stop` command + the crew registry + keeper-attach setup.
- **C3/C4:** instruction/skill artifacts + the handoff schema. No Go supervisor.
