# Captain & Crew — Integration

> Kerf integration-pass artifact for the **Captain & Crew** plan. It documents how
> the four components (C1 `epic_completed`, C2 `crew start/stop`, C3 crew launch
> context + handoff schema, C4 captain operating context) connect into one working
> slice, and it RESOLVES the three cross-component gaps C4 flagged. The per-component
> contracts are normative in `05-specs/<c>-spec.md`; this document adds only the
> wiring and the integration-level decisions (§4 gap resolutions, §5 cross-cutting).
>
> Scope reminder: this slice is the MECHANICAL wiring — start crew, deliver work +
> status between captain and crew, notify the captain structurally on epic
> completion. The captain's JUDGMENT (ranking, failure handling, rebalancing) is OUT
> of scope and is the future "judgment layer" referenced throughout.

---

## 1. End-to-end data flow

The whole slice is one loop from captain assignment to captain notification. Each hop
names the exact artifact or event that crosses the component boundary.

1. **Operator → Captain (C4).** The operator hands the Captain a crew assignment (a
   `{crew_name, queue, epic_id}` intent) over the comms bus (`comms recv --follow
   --json`, deduped on `event_id`) or directly at session start. The Captain makes no
   ranking decision — it acts on what the operator handed it.

2. **Captain (C4) writes a mission handoff in the C3 schema.** The Captain writes a
   Markdown-with-YAML-frontmatter file in the **C3 locked schema**
   `{schema_version: 1, crew_name, queue, epic_id, goal, captain_name}` at the
   path convention `.harmonik/crew/missions/<crew_name>.md`. **Artifact at this hop:**
   the mission-handoff file (C3 owns the schema, C4 authors the instance).

3. **Captain (C4) → `harmonik crew start` (C2).** The Captain calls
   `harmonik crew start <crew_name> --queue <queue> --mission <handoff-path>`. C2
   (daemon RPC): mints the `session_id` UUID and writes the crew registry record
   `.harmonik/crew/<crew_name>.json` (`{name, session_id, queue, epic, handle,
   started_at}`) *before* launch; ensures the named queue exists (persists a minimal
   `Queue{Name: queue, Workers: 1}` if absent); launches the long-lived interactive
   `claude --remote-control "<crew_name>" --session-id <uuid>` pane; bracketed-pastes
   the kick-off line *"Please read `<handoff-path>` and run `/session-resume` on it,
   then begin your operating loop."*; stands up the keeper-attach inputs
   (`.harmonik/keeper/<crew_name>.ctx` env + `.managed` marker + `Handle`). **Artifacts
   at this hop:** the crew registry record (C2 owns) + the seeded session. C2 returns
   the minted `session_id` to the Captain (informational).

4. **Crew (C3) boots.** Triggered by the C2 paste seed, the crew session loads the
   `crew-launch` skill and runs its boot sequence: parse the handoff frontmatter →
   confirm identity (`$HARMONIK_AGENT == crew_name`) → `harmonik comms join` (now
   visible in `comms who` — **success-criterion #1**) → **mirror the assignment to
   beads** via `br update <epic_id> --assignee <crew_name>` (metadata-only write) →
   `harmonik comms recv --follow --json` (subscribe to its inbox, dedupe on
   `event_id`) → post a boot `--topic status`. **Artifacts at this hop:** the crew's
   `agent_presence online` event + the durable `--assignee` mirror on the epic.

5. **Captain (C4) → crew assignment over comms.** The Captain mails the assignment:
   `harmonik comms send --to <crew_name> --topic assign -- "<epic_id> + 1-line goal"`.
   **Event at this hop:** an `agent_message{topic: assign, to: crew_name}` on the bus
   (durable in `events.jsonl`). The crew receives it (deduped on `event_id`) and adopts
   the epic — re-mirrors `br update <new_epic> --assignee <crew_name>` and begins
   dispatching (**success-criterion #2**).

6. **Crew (C3) dispatches its epic's beads to its OWN named queue.** The crew finds
   the epic's ready children and submits them to its own queue:
   `harmonik queue submit --queue <queue> --beads <id1>,<id2>,...` — **never** the
   shared `main` queue. **Artifact at this hop:** bead items appended to
   `.harmonik/queues/<queue>.json`. The crew arms its own
   `harmonik subscribe --types run_completed,run_failed,run_stale,heartbeat` monitor
   and emits `--topic status` + `br comments add <epic_id>` progress on each observed
   bead close / a ≤10-minute timer (**success-criterion #3**).

7. **Daemon dispatches, merges, closes beads.** The daemon picks up the crew's queue,
   dispatches each bead in an isolated worktree, merges one-at-a-time to the target
   branch, and on success calls `CloseBead` → `emitBeadClosed` at the close site
   (`internal/daemon/workloop.go`). The crew does NOT close its own beads (beads-cli
   write discipline) — the daemon owns that terminal transition. **Event at this hop:**
   `bead_closed{RunID, BeadID}` per merged bead.

8. **C1 emits `epic_completed` on the last-child close.** Immediately after each
   `emitBeadClosed`, C1's `maybeEmitEpicCompleted` reads the closed bead's parent
   (parent-child edge) and the parent's children + their statuses (one `br show
   <child>` + one `br show <parent>`, child status now surfaced via the
   `DependencyEdge.EndpointStatus` field). If every child is `closed` and the
   at-most-once guard (`emittedEpics`) has not already claimed this parent, C1 emits
   `epic_completed{epic_id, last_child_bead_id, closed_at}` (**success-criterion #4**).
   **Event at this hop:** one `epic_completed` per epic, at-most-once.

9. **Captain (C4) `subscribe --types epic_completed` surfaces-and-awaits.** The
   Captain's running `harmonik subscribe --types epic_completed --json` receives the
   event, attributes it to the owning crew (see §4 Gap 1 — via the durable beads
   `--assignee` mirror, not the registry's spawn-time `Record.Epic`), and SURFACES:
   a status line + `harmonik comms send --to operator --topic status -- "epic
   <epic_id> completed (crew <name>); awaiting next assignment"`. It then AWAITS the
   operator's next assignment — it does NOT pick the crew's next epic (judgment-out).
   This closes the loop back to hop 1.

**Restart sub-flow (success-criterion #5, orthogonal to the main loop):** when the
keeper winds a crew down (context-full) and resumes its **same** `session_id`
(`--resume <uuid>`), the crew re-runs its boot sequence (hop 4), re-deriving
`{queue, epic_id}` from the handoff frontmatter AND the durable `br show <epic_id>
--assignee == crew_name` mirror. The daemon kept draining `<queue>` across the
restart, so no in-flight work is lost; the Captain keeps coordinating off durable
state and treats the restart as a non-event.

---

## 2. Integration order / build sequence

The dependency edges are: **C1 ⫫ C2** (both pure Go, independent); **C3 depends on
C2's seed + registry contract and C1's event**; **C4 depends on C1 + C2 + C3**. So the
build order is:

```
  ┌── C1 (Go: epic_completed event + DependencyEdge.EndpointStatus) ──┐
  │                                                                    ├──► C3 (schema doc → crew-launch skill) ──► C4 (captain skill) ──► E2E smoke
  └── C2 (Go: crew start/stop + registry + crew-launch-spec) ─────────┘
```

### Step 1 — C1 ∥ C2 (Go, parallel, independently buildable)

- **C1** = event-emit + `brShowEdge`/`DependencyEdge` status surfacing + the
  at-most-once guard + boot seeding. Touches `internal/core`, `internal/brcli`,
  `internal/daemon`. **Validatable at this step alone:** `go test
  ./internal/core/ ./internal/brcli/` (unit, fast), the `//go:build scenario`
  daemon emit/idempotency test run by hand (C1 §6), and the manual
  `harmonik subscribe --types epic_completed --json` confirmation against a live
  daemon that has closed an epic's last child. C1 is self-contained — it does not
  need C2/C3/C4 to be testable.
- **C2** = `harmonik crew start/stop/list` + the `internal/crew` registry package +
  `buildCrewLaunchSpec` + the keeper-attach inputs. Touches `cmd/harmonik`,
  `internal/crew` (new package + `.golangci.yml` depguard entry), `internal/daemon`.
  **Validatable at this step alone:** `go test ./internal/crew/...` and `go test
  ./internal/daemon/ -run 'CrewLaunchSpec|CrewStart'` (unit), the C2 scenario test
  against the `harmonik-twin-claude` binary (run by hand), and the **mandatory
  §6 manual smoke** (preflight `claude --remote-control "preflight" --session-id
  <uuid>` to confirm the pane + statusLine hook; then a real `crew start`). C2 does
  not need C1/C3/C4 to be testable — the seeded crew need not yet have the C3 skill
  to confirm the registry/queue/marker land.

Because C1 and C2 share no files (C1 = core/brcli/daemon-close-path; C2 =
cmd/crew/daemon-crew-handler) they can be dispatched as two parallel beads with no
merge conflict.

### Step 2 — C3 (schema doc FIRST, then the crew-launch skill)

C3 depends on both prior steps: its handoff schema is what C2's seed points at and
what C4 writes, and its operating loop assumes C1's `epic_completed` is the
completion signal the crew must NOT pre-empt (it must not `br close`). Within C3,
**author the handoff-schema doc before the crew-launch skill** — the skill
references the schema's field-contract and path convention, so the byte-for-byte
contract must be fixed first.

- **Validatable at this step:** the schema doc is reviewable as a contract (do C2's
  read and C4's write agree byte-for-byte?). The `crew-launch` skill is validated by
  the C3 manual smoke (C3 §6) — author a handoff, `crew start` a crew with the skill
  in its context, and observe AC-1..AC-4 (auto-subscribe + dedupe, own-queue
  dispatch, progress feed, restart re-hydration). This step requires C2 landed (to
  `crew start`) and C1 landed (so the crew's "do not `br close`" rule is exercised
  end-to-end against a real `epic_completed`).

### Step 3 — C4 (captain skill)

C4 consumes all three: it writes C3's handoff schema, calls C2's verbs, and
subscribes to C1's event. **Validatable at this step:** the C4 manual smoke (C4 §6)
— drive a Captain session loaded with the `captain` skill, hand it two assignments,
observe two `crew start` calls, two handoffs, two assignments mailed, and one
`epic_completed` surfaced-and-awaited (AC-1..AC-4).

### Step 4 — End-to-end smoke (the definitive integration validation)

The full §6 end-to-end smoke (below): captain brings up ≥2 crew on distinct queues →
assigns epics via C3 handoffs → crew dispatch + work → last child of an epic closes →
`epic_completed` fires → captain surfaces. This is the only step that exercises all
four components against each other on the live binary under the supervisor.

---

## 3. Shared state & cross-component contracts

Every artifact or event that crosses a component boundary, with its owner, readers,
format, and where it is normatively defined.

| Contract | Owner (defines) | Readers / writers | Format | Defined in |
|---|---|---|---|---|
| **Mission-handoff** `{schema_version, crew_name, queue, epic_id, goal, captain_name}` | **C3** owns the schema | **C4 writes** the instance; **C2 reads** the path (delivers it to the crew via the paste seed); **C3/crew resumes into** it | Markdown + YAML frontmatter at `.harmonik/crew/missions/<crew_name>.md` | `c3-spec.md §3.1`; doc home is `specs/crew-handoff-schema.md` (RESOLVED 2026-06-08: operator chose `specs/`, spec-first) |
| **Crew registry record** `{name, session_id, queue, epic, handle, started_at}` | **C2** owns (`internal/crew`) | **C2 writes** (`crew start`/`stop`); **C4 reads** via `crew list` | JSON at `.harmonik/crew/<name>.json` (`schema_version: 1`, atomic temp-write+rename) | `c2-spec.md §3.3` |
| **Named queue** | **C2** creates/ensures (`queue.Persist`) | **Crew (C3) dispatches** to it (`queue submit --queue <q>`); **daemon drains** it | `.harmonik/queues/<name>.json` (`Queue{Name, Workers}`) | `c2-spec.md §3.1, R-C2.1`; `specs/queue-model.md` |
| **`epic_completed`** `{epic_id, last_child_bead_id, closed_at}` | **C1** emits | **C4 consumes** read-only via `subscribe --types epic_completed` | typed `core.EpicCompletedPayload`; event in `events.jsonl`; durability class **O** | `c1-spec.md §3 (D1/D2)`; registry `eventreg_hqwn59.go` |
| **Comms topic `assign`** (C4 → crew) | C4 sends | crew (C3) receives + adopts the epic | `agent_message{topic: assign, to: crew_name, body: epic_id+goal}` | `c4-spec.md §3.1 step 5`; `c3-spec.md §3.2 (message handling)`; `agent-comms` skill |
| **Comms topic `status`** (crew → captain; captain → operator) | C3 emits (crew→captain); C4 emits (captain→operator) | C4 reads (`comms log --from <crew> --topic status`); operator reads | `agent_message{topic: status}` in `events.jsonl` | `c3-spec.md §3.3`; `c4-spec.md §3.1 step 6, §6 surface` |
| **Beads `--assignee` mirror** (the durable Captain→crew→epic attribution) | **C3** writes (`br update <epic> --assignee <crew_name>` on adopt) | **C4 reads** (`br show <epic> --format json` → `assignee`) for `epic_completed` attribution; **crew reads** on restart re-hydration | beads metadata field (SQLite + git-tracked `.beads/issues.jsonl`); fallback label `crew:<crew_name>` if `--assignee` unsupported | `c3-spec.md §3.1 (durable mirror)`; attribution use defined in **§4 Gap 1** of this doc |

---

## 4. Resolved cross-component gaps (integration decisions)

C4's CONTRACT NOTES flagged three places where the slice needed a cross-component
decision. These are resolved here (this is the integration pass's job); the rationale
is recorded so the choice is conscious.

### Gap 1 — re-task attribution (stale `Record.Epic`)

**Problem.** When the Captain re-tasks a *live* crew over comms (`--topic assign` with
a new `epic_id`), the crew adopts the new epic — but C2's `crew start` only set
`Record.Epic` at spawn and exposes no `UpdateEpic` verb. So the registry's `Epic`
field goes stale after a comms re-task, and a later `epic_completed` lookup that keyed
on `Record.Epic` would mis-attribute or fail to find the owning crew.

**RESOLUTION.** C4 attributes an `epic_completed` to its owning crew via the **durable
beads `--assignee` mirror**, NOT the registry's spawn-time `Record.Epic` and NOT a C4
in-memory map. On each `epic_completed{epic_id}`, the Captain runs `br show <epic_id>
--format json` and reads `assignee` (which equals the owning `crew_name`). This is the
attribution source of truth because:

- It is **durable** (SQLite + git-tracked JSONL) — survives the Captain's own keeper
  restart, where an in-memory map would not.
- It is **kept current by the party that knows** — the crew, which sets it the moment
  it adopts an epic (boot AND re-task), so there is no window where a re-tasked crew's
  attribution is stale.
- It needs **no new C2 surface** — no `crew set-epic` / `UpdateEpic` verb, no new RPC.

**Integration-driven amendment to C3 (record against `c3-spec.md`).** The crew's
adopt-assignment step MUST run `br update <newepic> --assignee <crew_name>` for
**EVERY** epic it adopts — at boot (already specified in `c3-spec.md §3.2 boot step 4)
AND on every comms re-task (`c3-spec.md §3.2` message-handling, the `topic ==
assignment` branch already mirrors `br update <new_epic> --assignee <crew_name>` — this
amendment makes that mirror NORMATIVE for attribution, not just for restart
re-hydration). The crew adds nothing new; this elevates the existing mirror step to
"the captain's attribution depends on it, so it MUST run on every adopt." Note `crew
set-epic` as an OPTIONAL future convenience only (if a later slice wants the registry
to also track the live epic for `crew list` display, that is additive and does not
change attribution).

**Consequence for C4.** Drop the "look up the owning crew via `Record.Epic`" wording in
the C4 mechanical loop (`c4-spec.md §3.1 step 6a` and §5 step 1) in favor of "look up
the owning crew via `br show <epic_id> --assignee`." The C4 skill is the artifact, so
this is an instruction-level change folded in when the `captain` skill is authored —
the SPEC.md C4 summary reflects the resolved form.

### Gap 2 — no structural crew-offline / stuck event

**Problem.** C4 detects a crew offline only by `comms who` TTL drop + absence of a
`--topic status` presence heuristic. There is no structural `crew_offline` /
`crew_stuck` event.

**RESOLUTION.** **OUT of scope for this slice.** C4's heuristic stands: a crew is
"appears offline" when it drops from `harmonik comms who` past the ~120s TTL and stops
emitting `--topic status`; the Captain SURFACES that and AWAITS, never declaring
failure or re-homing. A structural `crew_offline`/`crew_stuck` event is a **follow-up
owned by the future judgment layer** — that layer will want a real signal rather than
a presence-poll, and emitting/consuming it is judgment-layer work (deciding *what to
do* when a crew is stuck is the very judgment this slice excludes). Noted, not built.

### Gap 3 — undefined `operator` identity

**Problem.** C4 surfaces via `comms send --to operator --topic status`, but no
component defines an agent named `operator` on the bus.

**RESOLUTION.** Lock the **dual-surface convention**: the Captain surfaces via BOTH
(a) a status line AND (b) `comms send --to operator --topic status`. The operator
SHOULD run `harmonik comms join --name operator` to receive the directed messages,
but `harmonik comms log --from <captain> --topic status` always works as a **no-join
fallback** (`comms log` is a durable, daemon-independent operator view that does not
require the `operator` agent to be online or even to exist). So the surface lands
regardless:

- If `operator` has joined → it receives the directed `--to operator` message live.
- If `operator` has not joined → the message is still durable in `events.jsonl`, and
  the operator reads it via `comms log --from <captain> --topic status`, plus sees the
  status line.

**Document this as the operator-onboarding convention** (in the `captain` skill's
"surface-and-await" section and any operating-manual): "To receive Captain status
directly, run `harmonik comms join --name operator`; otherwise read `harmonik comms
log --from <captain> --topic status`." This is purely conventional — no new code, no
contractually-required `operator` agent.

---

## 5. Cross-cutting concerns

- **Error propagation — exit 17 is uniform.** Every daemon-RPC surface in the slice
  returns **exit 17 when the daemon is down**: `crew start`/`crew stop`
  (`c2-spec.md §3.1, §7`), all `comms send/recv/join/leave`, and `subscribe`
  (`c4-spec.md §2, §7`). The local read-only surfaces — `crew list`, `comms log`,
  `comms who` — work without the daemon. The uniform Captain response to exit 17 is
  **surface-and-await** ("daemon not running"), never proceed-to-spawn. This single
  convention means the Captain's daemon-down handling is one rule across all verbs.
- **The shared event journal.** All structural signals in the slice land in the one
  daemon event journal `.harmonik/events/events.jsonl`: `bead_closed`,
  `epic_completed` (C1), every `agent_message` (`assign`/`status` comms),
  `agent_presence` (`comms join`/`who`). `eventbus.ScanAfter` is the single replay
  primitive (`subscribe`, `comms recv`, and C1's boot seed all use it). One log, one
  ordering, one replay mechanism — no per-component side journals.
- **Schema versioning.** The mission-handoff carries `schema_version: 1` (C3); the
  crew registry record carries `schema_version: 1` (C2). Both bump on any breaking
  field change. `epic_completed` is an additive event type (durability class O,
  EV-029 N-1-readable unaffected — no envelope-schema bump);
  `DependencyEdge.EndpointStatus` and `brShowEdge.Status` are additive/optional
  fields (C1 §8). The whole slice is purely additive — no migration, no event-log
  rewrite.
- **Instruction-level (non-enforced) contracts.** Two load-bearing rules in this
  slice are **instruction-level, not daemon-enforced**:
  1. *"A crew dispatches only to its OWN queue, never `main`."* The daemon does not
     enforce per-crew queue isolation (`c3-spec.md §7`); the `crew-launch` skill
     states it as a HARD RULE and review/smoke catches a violation. Isolation is what
     keeps crews separable and what the Captain's attribution relies on.
  2. *"Agents never issue terminal `br` writes (`claim`/`close`/`reopen`)."* The
     daemon owns those transitions (beads-cli write discipline, BI-010). The crew
     (C3) and Captain (C4) skills both restate this MUST-NOT; the daemon does not
     block an out-of-band `br close`, so the contract is enforced by the launch
     contexts, not by code. (C1's at-most-once guard is precisely the defense against
     an out-of-band close racing the daemon's close — see C1 §D4 / §AC-2.)

---

## 6. Integration testing strategy

Each code component carries its own gate; the slice is then validated end-to-end under
the supervisor. Per `reference_harmonik_daemon_session_nesting`, **session/spawn code
MUST be live-smoked** — reviewer reasoning + `go test` is not sufficient for
tmux/session/spawn paths — and per `reference_scenario_test_authoring` **the daemon
merge gate SKIPS `//go:build scenario` tests, so they must be run by hand** (a worktree
agent or manually).

### Per-component gates

- **C1** — unit (`go test ./internal/core/ -run 'EpicCompleted|EventRegistry'`,
  `go test ./internal/brcli/ -run 'ShowBead.*Endpoint|EndpointStatus'`) + scenario
  (`go test -tags scenario ./internal/daemon/ -run 'EpicCompleted'` — emit,
  at-most-once sibling race, boot-seed survives restart). The scenario test is run by
  hand. Full-package regression for AC-7.
- **C2** — unit (`go test ./internal/crew/...`; `go test ./internal/daemon/ -run
  'CrewLaunchSpec|CrewStart'`) + scenario (real daemon driving `crew start` against the
  `harmonik-twin-claude` twin, asserting registry/queue/`.managed` appear and `crew
  stop` tears them down — run by hand) + **the mandatory §6 manual smoke**: preflight
  `claude --remote-control "preflight" --session-id <uuid>` (confirm pane +
  statusLine hook fire) BEFORE handler code, then a real `crew start` under the
  supervisor.
- **C3** — manual smoke only (skills have no Go test): author a handoff, `crew start`
  a crew with the skill, observe AC-1..AC-4 (dedupe, own-queue dispatch, progress
  feed, restart re-hydration). Live under the supervisor.
- **C4** — manual smoke only: drive a Captain session with the `captain` skill,
  observe AC-1..AC-4 (≥2 spawns, two handoffs, two assignments mailed, one
  `epic_completed` surfaced-and-awaited, zero autonomous judgment actions).

### The definitive end-to-end smoke (all four, under the supervisor)

1. Boot a daemon under the supervisor; confirm `harmonik crew list` + `harmonik queue
   status` respond.
2. Drive a Captain session (loaded with `captain` + `agent-comms` + `beads-cli`).
   Hand it (as operator) **two** crew assignments referencing two distinct epics, each
   with ≥2 ready child beads.
3. Observe **≥2 crew come up on distinct queues** — two C3 handoffs written, two `crew
   start` calls, `harmonik comms who` shows both crew online, `crew list` shows two
   distinct records (criterion #1, #6).
4. Observe **each assignment land** — the Captain mails each crew its epic
   (`comms log --to <crew> --topic assign`); each crew adopts it, `br update <epic>
   --assignee <crew_name>` mirrors, and dispatches the epic's ready beads to its OWN
   queue (`.harmonik/queues/<queue>.json`, nothing in `main`) (criterion #2).
5. Observe **progress feeds** — `comms log --from <crew> --topic status` and `br
   comments list <epic>` both get updates as beads run (criterion #3).
6. Observe **`epic_completed` fire and surface** — when the last child of one assigned
   epic merges + closes (daemon-owned), C1 fires exactly one `epic_completed`; the
   Captain's `subscribe --types epic_completed` receives it, attributes it via `br
   show <epic> --assignee`, and surfaces-and-awaits WITHOUT assigning the next epic
   (criterion #4 + judgment-out).
7. Observe **restart-continuity** (criterion #5) — trigger a keeper cycle (or simulate
   via `--resume <uuid>`) on one crew; confirm it re-appears in `comms who` on the
   same name, re-hydrates its `{queue, epic_id}` from the handoff + `--assignee`
   mirror, and continues; the daemon kept draining its queue; the Captain treats the
   restart as a non-event (no failure-surface, no re-spawn).

---

## 7. Success-criteria traceability matrix

Every criterion #1–#6 from `01-problem-space.md`, its owning component(s), and the
spec section / acceptance criterion that satisfies it. No criterion is orphaned.

| # | Criterion (abbreviated) | Owning component(s) | Satisfying spec section / AC |
|---|---|---|---|
| **#1** | Captain brings up ≥2 named crew; `comms who` shows all online, each on its own queue | **C2** (mechanism: `crew start`), **C4** (drives ≥2 spawns), **C3** (the crew's `comms join` makes it online) | C2 §AC-1, §AC-2 (distinct registry + queue per crew); C3 §AC-1 boot `comms join`; C4 §AC-1 (drives ≥2 `crew start`) |
| **#2** | Captain assigns an epic via comms; crew receives it (deduped) and dispatches its beads to its own queue | **C3** (receive-and-dispatch behavior), **C4** (writes handoff + mails assignment), **C2** (triggers the boot loop) | C3 §AC-1 (dedupe on `event_id`), §AC-2 (own-queue, not `main`); C4 §AC-2 (handoff + mail); C2 §AC-2 (mission seed triggers loop) |
| **#3** | Crew writes progress to a durable, captain-readable feed | **C3** (owns the emit — `--topic status` + `br comments`), **C4** (reads it) | C3 §3.3, §AC-3 (both surfaces, on bead-close + 10-min timer + boot/drain bookends); C4 §AC-5 (reads without acting) |
| **#4** | Last child of an epic closes → `epic_completed` fires carrying the epic id → subscribed captain receives it | **C1** (emits), **C4** (subscribes + surfaces) | C1 §AC-1 (exactly-one emit), §AC-2 (sibling race → one emit), §AC-5 (boot survives restart); C4 §AC-3 (receive + surface) |
| **#5** | On keeper restart, crew re-hydrates assignment from durable state and continues; coordination unaffected | **C3** (re-hydration steps), **C4** (keeps coordinating off durable state) | C3 §AC-4 (re-derive from handoff + `--assignee`, daemon kept draining); C4 §AC-6 (no failure-surface, no re-spawn on restart) |
| **#6** | Captain spawns a NEW crew non-interactively: writes handoff + starts session; crew boots, auto-subscribes, claims its queue, no human steps | **C2** (the spawn mechanism), **C4** (the spawn trigger), **C3** (the auto-subscribe/claim boot behavior) | C2 §AC-1 (mission seed + queue-ensure + `.managed`); C3 §AC-1 (auto `join` + `recv --follow`); C4 §AC-1 spawn step |

All six criteria are covered with no orphan. C1 and C2 are the Go mechanisms; C3 and
C4 are the instruction artifacts that drive and consume them. Criterion #4 is the only
one wholly owned by a single Go component (C1) on the emit side; every other criterion
is a captain (C4) + crew (C3) behavior riding on the C1/C2 mechanisms.
