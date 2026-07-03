# Captain & Crew — SPEC

> The single self-contained document an implementing agent reads FIRST for the
> **Captain & Crew** kerf plan. It is a FAITHFUL ASSEMBLY of the per-component change
> specs — it adds no new requirements and changes no locked decision. Each component
> section is a tight summary plus a pointer to the full `05-specs/<c>-spec.md`; the
> integration wiring + cross-component decisions live in `06-integration.md`, which
> this document references rather than duplicates.
>
> Read order for implementation: this SPEC → the relevant `05-specs/<c>-spec.md` for
> the component you are building → `06-integration.md` for the wiring and the resolved
> cross-component gaps.

---

## Overview

A **Captain** is a long-lived orchestrator that brings up and coordinates a pool of
long-lived **crew** agents, each owning a major initiative (an epic) and its own work
queue. This work defines the **mechanical wiring** to:

1. **Start a captain and its crew** as persistent sessions (the captain spawns crew —
   option A).
2. **Deliver work and status between them** over the comms bus (assignments
   captain→crew, status crew→captain).
3. **Notify the captain structurally** when an initiative (epic) completes — its last
   child bead closes.

**In-scope = wiring; out-of-scope = judgment.** This slice builds the mechanics: how
crew start, how work and status flow, and how completion is signaled. It deliberately
EXCLUDES the captain's *judgment* layer — ranking which initiative to assign,
deciding what to do when a crew is stuck/failed, and rebalancing load. On any
completion / stuck / contention moment the captain's mechanical behavior is to
**surface to the operator and await assignment**; the human operator makes the
ranking/failure call in this slice. Where the future judgment layer plugs in is noted
throughout, not built.

**No Go supervisor.** The captain is itself an LLM session running an operating
context (a skill) — there is no Go "captain-supervisor" process in this slice. The
only net-new Go is C1 (the `epic_completed` event) and C2 (the `harmonik crew`
command + crew registry). C3 and C4 are instruction/skill artifacts.

**Programmatic session drive IS supported.** A process starts a persistent Claude
Code session and re-tasks/restarts it by `session_id` (`claude --resume <id>`); crew
are long-lived interactive `claude --remote-control "<name>" --session-id <uuid>`
panes — autonomous, pasteable, and cloud-watchable — addressed by comms name (tasking)
and `session_id` (restart). This corrects the earlier "human-only / not drivable"
misread.

---

## Success criteria

Verbatim from `01-problem-space.md` (concrete, verifiable):

1. A captain session brings up ≥2 named crew sessions; `harmonik comms who` shows all
   of them online, each on its own named queue.
2. The captain assigns an epic to a crew member via comms; the crew member receives it
   (deduped on `event_id`) and begins dispatching that epic's beads to its own queue.
3. A crew member writes progress to a durable, captain-readable feed as it works
   (`comms --topic status` and/or `br comments`).
4. When the last child bead of an assigned epic closes, an `epic_completed` event fires
   carrying the epic id, and a captain subscribed to it receives the notification.
5. On keeper-driven restart (context-fill), a crew member re-hydrates its assignment
   from durable state (its queue + assigned epic) and continues — captain coordination
   is unaffected by the restart.
6. The captain spawns a NEW crew member non-interactively: it writes a mission handoff
   and starts the session with `/session-resume`; the crew member boots,
   **auto-subscribes to its comms inbox**, and claims its named queue with no human
   steps.

---

## Components

Four components. **C1–C2 are code (Go); C3–C4 are launch-context / instruction
artifacts.** Reuse surfaces (comms bus, named queues, beads, keeper) are dependencies,
not build-components.

### C1 — `epic_completed` event   [Go: daemon + core]

**Locked decision.** Emit a structural `epic_completed{epic_id, last_child_bead_id,
closed_at}` event when an epic's last child bead closes. At the bead-close site (right
after `emitBeadClosed`), look up the just-closed bead's parent via its `parent-child`
edge; if that parent now has **zero remaining open children**, emit. Emission is
**at-most-once per epic** (an `emittedEpics` guard on `workLoopDeps`, seeded on boot
from an `events.jsonl` scan so it survives a daemon restart) — necessary because crew/
humans can `br close` out-of-band, outside the daemon's merge lock, racing the
"last-child?" check. The completion query uses one `br show <parent> --format json`
(children inline in `dependents[]` with status), which requires surfacing the child
`status` the adapter currently drops: add `EndpointStatus CoarseStatus` to
`core.DependencyEdge` and `Status string` to `brShowEdge`. Empty/unknown child status
counts as NOT closed (fail-safe). Event flows through `subscribe --types
epic_completed` unchanged.

**Key files.** `internal/core/eventtype.go` (new const), `internal/core/
agentlifecyclepayloads_gjyks.go` (new `EpicCompletedPayload`), `internal/core/
eventreg_hqwn59.go` (registration), `internal/core/dependencyedge.go`
(`EndpointStatus` field), `internal/brcli/show.go` (`brShowEdge.Status` + parse),
`internal/daemon/workloop.go` (`maybeEmitEpicCompleted` + the seven close-site calls +
`emittedEpics` deps field), `internal/daemon/daemon.go` (boot seed via
`eventbus.ScanAfter`). Verifies **success-criterion #4**.

→ Full spec: `05-specs/c1-spec.md`.

### C2 — Persistent crew-start path   [Go: new command]

**Locked decision.** A new daemon-RPC verb `harmonik crew start <name> --queue <q>
--mission <handoff-path>` (plus `crew stop <name> [--pause-queue]` and read-only-local
`crew list [--json]`). `crew start` mints a `session_id` UUID and writes the crew
registry record **before** launch; ensures the named queue exists (persist minimal
`Queue{Name, Workers:1}` if absent; idempotent); launches a long-lived **interactive**
`claude --remote-control "<name>" --session-id <uuid>` session (NOT server-mode, NOT
the ephemeral worktree+kill path — it is never `sess.Kill`'d on bead completion);
seeds the mission via bracketed-paste of a `/session-resume` kick-off line; and stands
up the keeper-attach inputs (`HARMONIK_AGENT`/`HARMONIK_PROJECT` env so the statusLine
hook writes `.harmonik/keeper/<name>.ctx`, the `.managed` marker, the pane `Handle`).
Crew are addressed by **comms name** (tasking) and **`session_id`** (restart via
`--resume <uuid>`, same id) — NOT tmux pane-id. The crew→queue binding lives ONLY in
the registry (`Record.Queue`); the queue model gets no owner field. Rejected: the
Managed Agents / sessions API (bills the API credit pool, not the Max subscription).

**Key files.** `cmd/harmonik/crew.go` (new), `cmd/harmonik/main.go` (dispatch
branch), `internal/crew/registry.go` (new subsystem package: `Record` +
`Write`/`Load`/`List`/`UpdateSessionID`/`Remove`, atomic temp-write+rename, name
validation), `internal/daemon/crewstart.go` (new handler + `CrewHandler` interface),
`internal/daemon/crewlaunchspec.go` (new `buildCrewLaunchSpec` sibling), `internal/
daemon/socket.go` (`crew-start`/`crew-stop` ops), `internal/daemon/daemon.go`
(wiring), `.golangci.yml` (depguard entry for `internal/crew`). Verifies
**success-criteria #1 and #6**. A **mandatory manual smoke** (`05-specs/c2-spec.md
§6`, incl. a preflight `claude --remote-control` step) gates "done" — session/spawn
code must be live-smoked.

→ Full spec: `05-specs/c2-spec.md`.

### C3 — Crew launch context + mission-handoff schema   [instructions + shared contract]

**Locked decision.** Two artifacts: (1) the **mission-handoff schema** — a Markdown
file with YAML frontmatter carrying `{schema_version, crew_name, queue, epic_id, goal,
captain_name}` at `.harmonik/crew/missions/<crew_name>.md` (C4 writes it, C2 reads the
path to seed, C3/crew `/session-resume`s into it). The assignment is ALSO mirrored to
beads via `br update <epic_id> --assignee <crew_name>` (the durable source-of-truth a
restart re-hydrates from; `--assignee` is the locked field, with a `crew:<name>` label
fallback). `session_id` is deliberately NOT in the handoff (C2 owns it). (2) A new
**`crew-launch` skill** at `.claude/skills/crew-launch/SKILL.md`: boot sequence
(parse handoff → confirm identity → `comms join` → mirror `--assignee` → `comms recv
--follow --json` deduping on `event_id` → boot status); operating loop (dispatch the
epic's ready beads to its OWN `--queue <name>`, NEVER `main`; arm a `subscribe`
monitor; do NOT `br close` beads — the daemon does); a MANDATORY **progress feed**
(success-criterion #3 owner) — both `comms send --to <captain> --topic status` AND `br
comments add <epic_id>`, on each observed bead-close + a ≤10-min timer + boot/drain
bookends; keeper-restart re-hydration; MUST/MUST-NOT guardrails.

**Key files.** `.claude/skills/crew-launch/SKILL.md` (new), `docs/components/internal/
crew-handoff-schema.md` (new — the byte-for-byte contract; doc home is an open
decision, see below), `.harmonik/crew/missions/example-handoff.md` (new gitignored
example). Verifies **success-criteria #2, #3, #5**.

> **Integration-driven amendment** (`06-integration.md §4 Gap 1`): the crew's
> `br update <epic> --assignee <crew_name>` mirror MUST run on EVERY epic it adopts
> (boot AND comms re-task), because the Captain's `epic_completed` attribution depends
> on it. This elevates an already-specified C3 step to NORMATIVE-for-attribution; it
> adds no new behavior.

→ Full spec: `05-specs/c3-spec.md`.

### C4 — Captain operating context   [instructions]

**Locked decision.** A new **`captain` skill** at `.claude/skills/captain/SKILL.md` —
the captain's mechanical operating loop, JUDGMENT EXCLUDED. The captain is an LLM
session running this context (no Go supervisor). The loop: boot (`comms join`, daemon
check) → spawn (write the C3 handoff, `harmonik crew start`, confirm via `comms who`)
→ assign/mail (`comms send --to <crew> --topic assign`) → watch (`subscribe --types
epic_completed --json` as the structural trigger) → on each `epic_completed`,
**surface-and-await** (status line + `comms send --to operator --topic status`; do NOT
pick the next epic) → read progress (`comms log --from <crew> --topic status`, `br
comments list <epic>`, read-only) → receive operator direction (`comms recv --follow
--json`, dedupe on `event_id`). The captain's permitted `br` writes are comments only
(NO terminal transitions). On every completion/stuck/contention moment: surface +
await, never decide.

**Key files.** `.claude/skills/captain/SKILL.md` (new). Cross-cuts **success-criteria
#1–#6** as the orchestrating role.

> **Integration-driven amendment** (`06-integration.md §4 Gap 1 + Gap 3`): the captain
> attributes an `epic_completed` to its owning crew via `br show <epic_id> --assignee`
> (the durable mirror), NOT the registry's spawn-time `Record.Epic`. And it surfaces
> via the dual-surface convention — a status line AND `comms send --to operator
> --topic status` — with `comms log --from <captain> --topic status` as the operator's
> no-join fallback (so no `operator` agent need exist).

→ Full spec: `05-specs/c4-spec.md`.

---

## Integration

The wiring, build order, data flow, and resolved cross-component gaps are documented
in full in **`06-integration.md`**. Summary:

**Build order.** **C1 ∥ C2** (independent Go, parallel-buildable: C1 = event emit +
`brShowEdge`/`DependencyEdge` status; C2 = crew command + registry — no shared files)
→ **C3** (schema doc FIRST so the byte-for-byte contract is fixed, then the
`crew-launch` skill) → **C4** (the `captain` skill) → **end-to-end smoke** (all four
under the supervisor). C3 depends on C2's seed + registry contract and C1's event; C4
depends on C1 + C2 + C3.

**Data flow** (one loop). Captain (C4) writes a mission handoff in the C3 schema →
`harmonik crew start` (C2) reads `--mission`, mints `session_id` + writes the registry,
ensures the queue, and seeds the interactive `claude --remote-control` session → crew
(C3) boots, `comms join`s, mirrors `--assignee`, subscribes to comms, and dispatches
its epic's beads to its OWN named queue → daemon merges + closes beads → C1 emits
`epic_completed` on the last-child close → C4 (`subscribe --types epic_completed`)
attributes via `br show <epic> --assignee`, surfaces-and-awaits. Each hop's exact
artifact/event is named in `06-integration.md §1`.

**Resolved cross-component gaps** (`06-integration.md §4`):
- **Gap 1 — re-task attribution.** The captain attributes `epic_completed` to a crew
  via the durable beads `--assignee` mirror, NOT the stale spawn-time `Record.Epic`
  and NOT a C4 in-memory map. C3 amendment: the crew MUST `br update <epic> --assignee
  <crew_name>` on EVERY adopt (boot + re-task). No new C2 verb needed (`crew set-epic`
  is an optional future convenience only).
- **Gap 2 — no structural crew-offline/stuck event.** OUT of scope; C4's `comms who`
  TTL + `--topic status` absence heuristic stands. A `crew_offline`/`crew_stuck` event
  is a follow-up owned by the future judgment layer.
- **Gap 3 — undefined `operator` identity.** Lock the dual-surface convention (status
  line AND `comms send --to operator --topic status`); operator SHOULD `comms join
  --name operator`, but `comms log --from <captain> --topic status` is the always-works
  no-join fallback.

**Cross-cutting** (`06-integration.md §5`): exit 17 = daemon-down uniformly →
surface-and-await; the shared `events.jsonl` journal + `eventbus.ScanAfter` replay; the
slice is purely additive (`schema_version: 1` on handoff + registry, additive event
type + edge fields, no migration); and the two instruction-level (non-enforced)
contracts — "crew dispatches only to its own queue, never `main`" and "agents never do
terminal `br` writes" — are enforced by the launch contexts and review/smoke, not by
daemon code (C1's at-most-once guard is the defense against an out-of-band close
racing the daemon).

**Testing strategy** (`06-integration.md §6`): each code component's own gate (C1
unit + scenario; C2 unit + scenario + mandatory manual smoke) PLUS the definitive
end-to-end smoke under the supervisor — captain brings up ≥2 crew on distinct queues →
assigns epics via C3 handoffs → crew dispatch + work → last child closes →
`epic_completed` fires → captain surfaces. The daemon gate SKIPS `//go:build scenario`
tests (run them yourself), and session/spawn code MUST be live-smoked
(`reference_harmonik_daemon_session_nesting`).

---

## Traceability matrix

Every success criterion #1–#6, its owning component(s), and the satisfying spec
section / acceptance criterion. (Full version with both per-component and integration
refs in `06-integration.md §7`.)

| # | Criterion (abbreviated) | Owning component(s) | Satisfying spec section / AC |
|---|---|---|---|
| **#1** | ≥2 named crew up; `comms who` shows all online, each on own queue | C2 (`crew start`), C4 (drives spawns), C3 (`comms join`) | C2 §AC-1/§AC-2, C3 §AC-1, C4 §AC-1 |
| **#2** | Captain assigns epic via comms; crew receives (deduped) + dispatches to own queue | C3 (receive+dispatch), C4 (handoff+mail), C2 (triggers boot) | C3 §AC-1/§AC-2, C4 §AC-2, C2 §AC-2 |
| **#3** | Crew writes progress to a durable, captain-readable feed | C3 (emit), C4 (read) | C3 §3.3/§AC-3, C4 §AC-5 |
| **#4** | Last child closes → `epic_completed` fires → subscribed captain receives | C1 (emit), C4 (subscribe+surface) | C1 §AC-1/§AC-2/§AC-5, C4 §AC-3 |
| **#5** | Crew re-hydrates after keeper restart; coordination unaffected | C3 (re-hydrate), C4 (off durable state) | C3 §AC-4, C4 §AC-6 |
| **#6** | Captain spawns a NEW crew non-interactively; crew auto-subscribes + claims queue | C2 (mechanism), C4 (trigger), C3 (auto-subscribe boot) | C2 §AC-1, C3 §AC-1, C4 §AC-1 |

No criterion is orphaned. C1/C2 are the Go mechanisms; C3/C4 are the instruction
artifacts that drive and consume them.

---

## Open decisions

Items still open at the end of the spec pass, carried forward for the implementer /
operator. **Locked-default** = the spec records a default and the implementer proceeds
unless the operator overrides; **needs-operator** = a genuine decision the operator
should make.

### Locked-default (C1 — proceed as specified unless operator overrides)

- **Nested-epic roll-up = single-level.** C1 fires `epic_completed` for the **direct**
  parent of the closed bead only; a grandparent epic gets its own event only when its
  own last direct child closes. Multi-level roll-up is judgment-layer territory.
  *(Locked default; C1 Open Question 1.)*
- **At-least-once-on-crash guard.** The at-most-once guard is in-memory, seeded by a
  boot scan of `events.jsonl` (no second persisted file). A crash *between* the
  guard-claim and the durable emit yields one possible duplicate on reboot — judged
  acceptable (a crash before the durable event is exactly when a retry is wanted).
  *(Locked default; C1 Open Question 2.)*
- **`specs/event-model.md §8` amendment.** Adding `epic_completed` implies an additive
  §8 taxonomy row (durability class O, EV-029-safe). The implementer SHOULD add it as
  part of C1. *(Locked default; flag to operator only if §8 is treated as frozen; C1
  Open Question 3.)*
- **Two `br show` calls per close.** The helper does `ShowBead(closedBead)` +
  `ShowBead(parent)`. Closes are serialized behind the merge lock, so throughput is
  not at risk; threading a cached `BeadRecord` is a larger change deferred. *(Locked
  default; C1 Open Question 4.)*

### Needs-operator (C3 — a genuine choice)

- **Handoff-schema doc home. RESOLVED 2026-06-08: operator chose
  `specs/crew-handoff-schema.md` (spec-first).** The mission-handoff schema is placed at
  `specs/crew-handoff-schema.md` — normative-spec-grade, so C2/C4 are bound by
  `specs/`-level review and C2's Go must conform to it. *(This was C3's only genuine open
  decision; it is now resolved — everything else in C3 was already locked.)*
