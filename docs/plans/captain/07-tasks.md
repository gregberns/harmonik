# Captain & Crew — Task Decomposition (07-tasks)

> Kerf tasks-pass artifact for the **Captain & Crew** plan. It breaks the assembled
> spec (`SPEC.md` + `06-integration.md` + the four `05-specs/<c>-spec.md`) into
> right-sized, dependency-aware implementation tasks, each with a **proposed bead**.
>
> **These are PROPOSALS only.** No bead is created here — the operator check-in comes
> first. Every proposed bead carries the `codename:captain` LABEL (the kerf-work
> attachment). **Do NOT attach via `br dep` on the epic** — a dep on an open epic
> silently insta-fails dispatch (known harmonik gotcha; see MEMORY
> `reference_beads_epic_dep_blocks_dispatch`). Attachment is the label, full stop.
>
> Read order for an implementer: `SPEC.md` → the relevant `05-specs/<c>-spec.md` →
> `06-integration.md` for the wiring. This file tells you WHICH beads, in WHAT order.

---

## A. Task list (ordered, dependency-aware)

Build order honors `06-integration.md §2`: **C1 ∥ C2** (logically independent and
parallelizable Go) → **C3** (schema doc first, then the crew-launch skill) → **C4**
(captain skill) → the two test beads. Every proposed bead's title is imperative,
≤72 chars, and every bead carries `codename:captain`.

> **Shared-file caveat (one file).** C1 and C2 are logically independent and can be
> *developed* in parallel — no edge links their chains — BUT **T3 (C1) and T7 (C2)
> both edit `internal/daemon/daemon.go`** (T3 threads the boot-seed; T7 wires the
> `CrewHandler`). Their edits will therefore **contend at merge**. There is no cycle
> and no data race; the daemon's one-at-a-time merge handles this safely (the two
> edits are additive and in distinct regions of `daemon.go`), so it is auto-mergeable
> in practice — but they should **not** be expected to merge as a literal simultaneous
> no-conflict pair. **Dispatch note:** land T3's and T7's `daemon.go` edits mindful of
> that one shared file (rebase/merge order — prefer landing T3's boot-seed touch
> first, or accept the small region-distinct merge); everything else across C1 and C2
> is conflict-free.

---

### C1 — `epic_completed` event (Go: daemon + core)

C1 splits into three impl tasks + one scenario test. The status-surfacing change
(`brShowEdge.Status` + `DependencyEdge.EndpointStatus`) is carved out as its own
prereq task because it is small, self-contained, and the emit logic (T2) depends on
it — landing it first de-risks the larger change.

#### T1 — Surface child status on dependency edges (`brShowEdge` + `DependencyEdge`)

- **Proposed bead:** "C1: surface child status on br show dependency edges" · `type=task` · `priority=2` · label `codename:captain`
- **What to build:** Add `Status string` to `brShowEdge` (`internal/brcli/show.go:59-62`)
  and `EndpointStatus CoarseStatus` to `core.DependencyEdge` (`internal/core/dependencyedge.go:6`).
  Parse the inline `status` into `EndpointStatus` in BOTH the `Dependencies` and
  `Dependents` edge-construction loops (`show.go:162-184`), tolerating empty (empty →
  zero `CoarseStatus` = "unknown", NOT closed; non-empty → `UnmarshalText`, error on bad).
  `DependencyEdge.Valid()` is UNCHANGED (the new field is additive/optional).
- **Spec reference:** SPEC.md §C1 ("surface the child `status`…"); `c1-spec.md §3 D3`,
  Files-row `internal/brcli/show.go` + `internal/core/dependencyedge.go`.
- **Deliverables:**
  - Modify `internal/brcli/show.go` (`brShowEdge.Status` + parse in both loops).
  - Modify `internal/core/dependencyedge.go` (`EndpointStatus` field + godoc; `Valid()` untouched).
  - Create `internal/brcli/show_endpointstatus_test.go` — table-driven: dependent with
    inline `status` populates `EndpointStatus`; empty → zero/unknown; bad status → error.
- **Acceptance criteria:** C1 §AC-6 (status surfacing — closed child → `CoarseStatusClosed`),
  C1 §AC-7 (additive/non-breaking — `Valid()` semantics unchanged, all existing
  `brcli`/`core` tests pass). Task check: `go test ./internal/brcli/ ./internal/core/` green.
- **Dependencies:** none (root task).

#### T2 — Emit `epic_completed` at the bead-close site (event + helper + guard)

- **Proposed bead:** "C1: emit epic_completed on last-child close (at-most-once)" · `type=task` · `priority=2` · label `codename:captain`
- **What to build:**
  1. New event type `EventTypeEpicCompleted = "epic_completed"` in
     `internal/core/eventtype.go` (run-lifecycle const block near `EventTypeBeadClosed`,
     durability class O in the doc comment).
  2. `EpicCompletedPayload{EpicID, LastChildBeadID, ClosedAt}` + `Valid()` beside
     `BeadClosedPayload` in `internal/core/agentlifecyclepayloads_gjyks.go:387`.
  3. Register `epic_completed` in `registerRunLifecycle()` (`eventreg_hqwn59.go`) after
     the `bead_closed` line; add to the durability-class doc comment.
  4. Daemon-side `epicCompletedPayload` struct beside `beadClosedPayload`
     (`workloop.go:3419`) + `maybeEmitEpicCompleted(ctx, deps, runID, closedBeadID)` helper
     near `emitBeadClosed` (`workloop.go:4020`): `ShowBead(closedBead)` → find parent via
     `parent-child` edge → `ShowBead(parent)` → if every child `EndpointStatus ==
     CoarseStatusClosed` AND the `emittedEpics` guard hasn't claimed this parent → claim
     (under `emittedEpicsMu`, BEFORE emit) → `EmitWithRunID(... EventTypeEpicCompleted ...)`.
  5. `emittedEpics map[core.BeadID]struct{}` + `emittedEpicsMu *sync.Mutex` fields on
     `workLoopDeps` (`workloop.go:98`); default to empty map + fresh mutex in
     `newWorkLoopDeps` (`workloop.go:493`) and `ExportedWorkLoopDeps`
     (`export_test.go:253`).
  6. RECOMMENDED: a tiny `emitBeadClosedAndMaybeEpic(ctx, deps, runID, beadID)` wrapper
     that calls both; replace the SEVEN close sites (`workloop.go:1813, 1875, 1973, 1986,
     2525, 2560, 2577`) with it (single insertion point, no missed site).
- **Spec reference:** SPEC.md §C1; `c1-spec.md §3 D1/D2/D4`, Files-rows `eventtype.go`,
  `agentlifecyclepayloads_gjyks.go`, `eventreg_hqwn59.go`, `workloop.go`. NOTE the
  `c1-spec.md` line-reference correction header (HEAD `2272d9f1`).
- **Deliverables:**
  - Modify `internal/core/eventtype.go`, `internal/core/agentlifecyclepayloads_gjyks.go`,
    `internal/core/eventreg_hqwn59.go`.
  - Modify `internal/daemon/workloop.go` (struct + helper + wrapper + 7 call sites + deps
    fields), `internal/daemon/export_test.go` (default the new deps fields).
  - Create `internal/core/epiccompleted_test.go` — `EpicCompletedPayload.Valid()` table +
    registry round-trip.
- **Acceptance criteria:** C1 §AC-1 (last child → exactly one emit, correct fields),
  §AC-3 (still-open child → zero emit), §AC-4 (no parent → zero emit, no error). The
  cross-process sibling-race (§AC-2) and boot-survival (§AC-5) are exercised by the
  scenario bead T4. Task check: `go test ./internal/core/ -run 'EpicCompleted|EventRegistry'`
  green; full-package `go test ./internal/core/ ./internal/daemon/` (non-scenario) green.
- **NOTE — only `specs/` edit in the whole plan:** if the optional `specs/event-model.md §8`
  taxonomy row is added (the locked-default §8 additive-row task above), it is the **sole
  `specs/` touch across the entire slice** — everything else in T1–T15 is additive code plus
  gitignored / skill / knowledge-base-doc artifacts. A reviewer should expect exactly that one
  normative-spec touch and nothing more. The row is **EV-029 N-1-safe / additive** (a new
  taxonomy row, no behavior change, no rename of an existing field).
- **Dependencies:** **T1** (the completion check reads `EndpointStatus`).

#### T3 — Boot-seed the `emittedEpics` guard from `events.jsonl`

- **Proposed bead:** "C1: boot-seed epic_completed guard from event log" · `type=task` · `priority=2` · label `codename:captain`
- **What to build:** In `daemon.Start` (after the JSONL writer opens, `daemon.go:738-743`,
  before/at `newWorkLoopDeps` `daemon.go:1391`), scan `cfg.JSONLLogPath` via
  `eventbus.ScanAfter(path, core.EventID{})` for prior `epic_completed` events, build the
  `seed map[core.BeadID]struct{}` (keyed on `EpicCompletedPayload.EpicID`), and thread
  `seed` + a fresh `*sync.Mutex` into `newWorkLoopDeps`. When `cfg.JSONLLogPath == ""`
  (tests), seed an empty map — the in-process guard still works for the session.
- **Spec reference:** SPEC.md §C1 ("seeded on boot from an `events.jsonl` scan"); `c1-spec.md
  §3 D4` (Boot seeding), Files-row `internal/daemon/daemon.go`.
- **Deliverables:**
  - Modify `internal/daemon/daemon.go` (boot scan + thread seed/mutex into deps).
  - (Covered by T2's deps-field plumbing; this task wires the boot-time population.)
- **Acceptance criteria:** C1 §AC-5 (after an `epic_completed` for epic E is in the log, a
  fresh boot seeds E into `emittedEpics`; a later last-child re-close of E emits zero). Asserted
  end-to-end by the T4 scenario sub-test (pre-write event → boot → re-close → zero).
- **Dependencies:** **T2** (needs the `emittedEpics`/`emittedEpicsMu` deps fields + the event type).

#### T4 — C1 scenario test (emit + at-most-once + boot-seed)

- **Proposed bead:** "scenario: C1 epic_completed emit + at-most-once + boot-seed" · `type=task` · `priority=2` · label `codename:captain`
- **What to build:** A `//go:build scenario` daemon-level test
  (`internal/daemon/epiccompleted_scenario_<bead>_test.go` — namespace the file/helpers
  per the parallel-helper-collision lesson). Three sub-tests:
  (1) boot a daemon with a stub `beadLedger` whose `ShowBead` returns a parent with one
  then-closed child; drive a close through a close branch; assert exactly one
  `epic_completed` on the bus.
  (2) two concurrent closes of two children of the same parent (one via the daemon path,
  one simulating out-of-band by invoking the helper directly); assert the bus sees the
  event exactly once (`t.Parallel` race sub-test acceptable).
  (3) pre-write an `epic_completed` to the log, boot, assert the boot scan seeds the guard
  (re-close → zero).
- **Spec reference:** `c1-spec.md §6` (Daemon emit + idempotency scenario); `06-integration.md
  §6` per-component C1 gate.
- **Deliverables:** Create `internal/daemon/epiccompleted_scenario_<bead>_test.go` (`//go:build scenario`).
- **Acceptance criteria:** C1 §AC-1, §AC-2 (sibling race → one emit; idempotent re-close →
  zero), §AC-5 (boot survives restart). RUN BY HAND: the daemon merge gate SKIPS
  `//go:build scenario` tests (`reference_scenario_test_authoring`). `go test -tags scenario
  ./internal/daemon/ -run 'EpicCompleted'` green when run manually.
- **Dependencies:** **T2, T3** (exercises the emit + the boot-seed).

---

### C2 — Persistent crew-start path (Go: new command)

C2 splits into the registry package (independent prereq), the launch-spec builder, the
daemon handler + socket ops, the CLI, unit tests, and the mandatory manual smoke.

#### T5 — Add the `internal/crew` registry package (+ depguard entry)

- **Proposed bead:** "C2: add internal/crew registry package + depguard entry" · `type=task` · `priority=2` · label `codename:captain`
- **What to build:** A new subsystem package `internal/crew/registry.go` per the
  `go-subsystem-add` skill: `Record{SchemaVersion, Name, SessionID, Queue, Epic, Handle,
  StartedAt}` + `Write`/`Load`/`List`/`UpdateSessionID`/`Remove` (atomic temp-write+rename,
  WM-026 pattern as in `queue.Persist`); local name validation (reject `/`, `..`; charset
  `[a-z0-9-]`, length 1–64 — do NOT depend on `keeper.validateAgent`, it is unexported).
  Stored at `.harmonik/crew/<name>.json` (`schema_version: 1`). Package depends only on
  stdlib + `internal/core`; MUST NOT import `internal/daemon`. Add the `internal/crew`
  depguard component-matrix entry to `.golangci.yml` (allow stdlib + `internal/core`; deny
  `internal/daemon`). Register any test helpers in `internal/testhelpers/` if used.
- **Spec reference:** SPEC.md §C2; `c2-spec.md §3.3` (registry), Files-rows
  `internal/crew/registry.go` + `.golangci.yml`; the `go-subsystem-add` skill.
- **Deliverables:**
  - Create `internal/crew/registry.go`.
  - Create `internal/crew/registry_test.go` (see T9, or land minimal smoke here and full in T9).
  - Modify `.golangci.yml` (depguard entry).
- **Acceptance criteria:** C2 §AC-4 (round-trip `Write`→`Load`; `UpdateSessionID` mutates
  only `session_id`; `List` returns all sorted by name; invalid names rejected). Task check:
  `go test ./internal/crew/...` green; `golangci-lint run ./internal/crew/...` clean (depguard
  passes).
- **Dependencies:** none (independent of C1; root of the C2 chain).

#### T6 — Add `buildCrewLaunchSpec` (persistent-session argv builder)

- **Proposed bead:** "C2: add buildCrewLaunchSpec persistent-session argv builder" · `type=task` · `priority=2` · label `codename:captain`
- **What to build:** `internal/daemon/crewlaunchspec.go` — `buildCrewLaunchSpec(...)`, a
  SIBLING of `buildClaudeLaunchSpec` (does NOT modify it). Produces argv
  `[<claudeBinary> --remote-control "<name>" --session-id <uuid>]` with the caller-minted
  UUID, env `HARMONIK_AGENT=<name>` + `HARMONIK_PROJECT=<projectDir>`, NO worktree, NO
  `--dangerously-skip-permissions`. `<claudeBinary>` resolves like the daemon's
  `HandlerBinary` (`daemon.go:99`), default `claude`.
- **Spec reference:** SPEC.md §C2; `c2-spec.md §3.2` (Launch construction) + Files-row
  `internal/daemon/crewlaunchspec.go`.
- **Deliverables:**
  - Create `internal/daemon/crewlaunchspec.go`.
  - Create/extend the launch-spec unit test (see T9) — argv + env + no-worktree assertions.
- **Acceptance criteria:** C2 §AC-5 (argv `[<claude> --remote-control "<name>" --session-id
  <uuid>]` with the caller UUID, the two env vars, no worktree / no skip-permissions). Task
  check: `go test ./internal/daemon/ -run CrewLaunchSpec` green.
- **Dependencies:** none required to BUILD (it produces argv only); land **in parallel with
  T5**. (It is consumed by T7.)

#### T7 — Daemon `crew-start`/`crew-stop` handler + socket ops + queue-ensure + keeper-attach

- **Proposed bead:** "C2: daemon crew-start/stop handler + socket ops + keeper-attach" · `type=task` · `priority=1` · label `codename:captain`
- **What to build:** `internal/daemon/crewstart.go` — the `crew-start`/`crew-stop` handler
  + a `CrewHandler` interface (declared here, registered in `daemon.go` like
  `CommsSendHandler`). `crew-start` flow (ordering per `c2-spec.md §7`): check collision →
  mint `session_id` UUID + `crew.Write` the registry record → ensure the named queue
  (`queue.Load`; if absent persist minimal `Queue{Name:q, Workers:1}` via `queue.Persist`;
  idempotent) → launch via `buildCrewLaunchSpec` into a daemon-owned tmux window (reuse
  `NewWindowIn`-style creation; NEVER `sess.Kill`'d on bead completion) → bracketed-paste
  the kick-off line *"Please read `<handoff-path>` and run `/session-resume` on it, then begin
  your operating loop."* (reuse `pasteInjectImplementerInitial` mechanism) → keeper-attach
  inputs (set `HARMONIK_AGENT`/`HARMONIK_PROJECT` env on the window; create
  `.harmonik/keeper/<name>.managed`; persist the pane `Handle` into the registry; optionally
  seed `.ctx`) → return the minted `session_id`. On launch failure roll back (`crew.Remove`).
  `crew-stop` flow (`c2-spec.md §3.5`): `crew.Load` → quit→grace→kill the pane via `Handle` →
  remove `.managed` marker → `crew.Remove` → optional `--pause-queue` via
  `queue-set-concurrency <q> 0`. Add `case "crew-start":`/`case "crew-stop":` to the op switch
  (`socket.go:383`). Wire the `CrewHandler` impl in `daemon.go` (thread `HandlerBinary`,
  project dir, tmux substrate, queue access). Exit 17 surfaced by the CLI (T8) when the daemon
  is down.
- **Spec reference:** SPEC.md §C2; `c2-spec.md §3.1` (daemon-RPC rationale), §3.2 (launch),
  §3.4 (keeper-attach), §3.5 (stop), §7 (ordering/edge cases); Files-rows
  `internal/daemon/crewstart.go`, `internal/daemon/socket.go`, `internal/daemon/daemon.go`.
- **Deliverables:**
  - Create `internal/daemon/crewstart.go` (handler + `CrewHandler` interface).
  - Modify `internal/daemon/socket.go` (two op cases).
  - Modify `internal/daemon/daemon.go` (construct + register the handler).
  - Handler unit test seeds (full in T9): `go test ./internal/daemon/ -run CrewStart` with a
    fake substrate + fake queue access (queue-ensured, registry-written before launch,
    `.managed` created).
- **Acceptance criteria:** C2 §AC-1 (start exits 0, prints session_id, registry record with
  non-empty minted session_id, queue ensured, `.managed` + env present, pane not killed),
  §AC-3 (stop tears down pane + record + `.managed`; `--pause-queue` halts the queue).
  Task check: `go test ./internal/daemon/ -run 'CrewStart'` green. **Session/spawn behavior is
  NOT proven by `go test`** — gated by the T10 manual smoke (`reference_harmonik_daemon_session_nesting`).
- **Dependencies:** **T5** (registry API), **T6** (launch-spec builder).

#### T8 — `harmonik crew` CLI (`start`/`stop`/`list`)

- **Proposed bead:** "C2: add harmonik crew CLI (start/stop/list)" · `type=task` · `priority=1` · label `codename:captain`
- **What to build:** `cmd/harmonik/crew.go` — `runCrewSubcommand(subArgs)` with `start`/`stop`/`list`
  verb routing, flag parsing (`--queue`, `--mission`, `--pause-queue`, `--json`, `--project`,
  `--socket`), `{op,payload}` marshal + dial `<project>/.harmonik/daemon.sock` + `SocketResponse`
  decode (mirrors `cmd/harmonik/comms.go`). `crew start`/`crew stop` are daemon RPCs (exit 17 when
  the daemon is down). `crew list [--json]` is **read-only and LOCAL** — reads `.harmonik/crew/*.json`
  via `crew.List`, works with the daemon down. Add the `os.Args[1] == "crew"` dispatch branch in
  `cmd/harmonik/main.go` beside the `comms` branch (`main.go:447`). Include usage text.
- **Spec reference:** SPEC.md §C2; `c2-spec.md §3.1` (CLI surface), Files-rows
  `cmd/harmonik/crew.go` + `cmd/harmonik/main.go`.
- **Deliverables:**
  - Create `cmd/harmonik/crew.go`.
  - Modify `cmd/harmonik/main.go` (dispatch branch).
- **Acceptance criteria:** C2 §AC-1/§AC-2 (CLI surfaces the start that produces a distinct
  record + queue per crew; `crew list` shows both; exit 17 when daemon down on start/stop;
  `crew list` works locally). Task check: `go build ./cmd/harmonik` succeeds; `harmonik crew
  list` runs with the daemon down. Operator-CLI surface validated end-to-end by the exploratory
  bead T13.
- **Dependencies:** **T7** (the daemon ops it dials), **T5** (`crew.List` for the local `crew list`).

#### T9 — C2 unit tests (registry + launch-spec + handler)

- **Proposed bead:** "C2: unit tests for crew registry, launch-spec, handler" · `type=task` · `priority=2` · label `codename:captain`
- **What to build:** Table-driven Go unit tests covering the three C2 code units:
  (a) `internal/crew/registry_test.go` — round-trip, `UpdateSessionID`-only mutation,
  `List` sort, name validation (`/`, `..`, uppercase, >64 → reject);
  (b) `go test ./internal/daemon/ -run CrewLaunchSpec` — argv + env + no-worktree
  (`buildCrewLaunchSpec`);
  (c) `go test ./internal/daemon/ -run CrewStart` — handler with a fake substrate + fake
  queue access: asserts queue-ensured, registry-written-before-launch (minted UUID present),
  `.managed` created, and `crew stop` tears them down.
  (If T5/T6/T7 each landed minimal tests, this bead completes the coverage and the regression
  full-package run.)
- **Spec reference:** `c2-spec.md §6` (Unit gate); `06-integration.md §6` per-component C2 gate.
- **Deliverables:** Create/complete `internal/crew/registry_test.go` +
  `internal/daemon/crewlaunchspec_test.go` + `internal/daemon/crewstart_test.go` (names per
  the parallel-helper-collision lesson — namespace helpers to avoid redeclare-collisions).
- **Acceptance criteria:** C2 §AC-4 (registry unit), §AC-5 (launch-spec unit), plus the
  handler-logic assertions for §AC-1/§AC-3. Task check: `go test ./internal/crew/... ./internal/daemon/`
  (non-scenario) green.
- **Dependencies:** **T5, T6, T7** (the units under test).

#### T10 — C2 mandatory manual smoke (incl. `claude --remote-control` preflight step 0)

- **Proposed bead:** "smoke: C2 crew start/stop on real claude (remote-control)" · `type=task` · `priority=1` · label `codename:captain`
- **What to build:** The MANDATORY manual smoke from `c2-spec.md §6` — session/spawn code MUST
  be live-smoked under the supervisor (`reference_harmonik_daemon_session_nesting`).
  **Step 0 (preflight, BEFORE merging handler code is declared done):** run a throwaway
  `claude --remote-control "preflight" --session-id <uuid>` and confirm the pane comes up AND
  the statusLine hook fires (writes a `.ctx` gauge). This settles the two runtime unknowns
  (statusLine hook under `--remote-control`; bracketed-paste acceptance) before relying on them.
  Then, with a live daemon under the supervisor: `harmonik crew start <name> --queue <q>
  --mission <handoff>` against REAL `claude` — confirm (1) `--remote-control "<name>"
  --session-id <uuid>` composes on the minted id as a pasteable, cloud-watchable pane; (2) the
  bracketed-paste mission seed lands and the crew begins; (3) `claude -p --resume <uuid> "<msg>"`
  re-tasks the same session; (4) `harmonik comms who` eventually shows the crew; (5)
  `harmonik crew stop <name>` tears down the pane + record + `.managed`.
- **Spec reference:** `c2-spec.md §Risks` + §6 (Manual smoke, REQUIRED before declaring done);
  `06-integration.md §6` C2 gate.
- **Deliverables:** A smoke run log / note recording each observation (no code file; a
  `comms send` summary or a bead comment recording the result is the durable artifact).
- **Acceptance criteria:** C2 §AC-1 keeper-input branch resolved (statusLine hook fires → `.ctx`
  is written reporting the same session_id; else the weakened env-vars+`.managed` form). The
  preflight + start/stop observations all pass. Task check: documented live-smoke pass under the
  supervisor.
- **Dependencies:** **T7, T8** (the handler + CLI it exercises). RUN BY HAND under the supervisor.

---

### C3 — Crew launch context + mission-handoff schema (instructions + shared contract)

C3 depends on C1 + C2. Within C3, the schema doc lands FIRST (the byte-for-byte contract
C2 reads and C4 writes), then the `crew-launch` skill references it.

#### T11 — Author the mission-handoff schema doc (+ gitignored example)

- **Proposed bead:** "C3: author mission-handoff schema doc + example handoff" · `type=docs` · `priority=2` · label `codename:captain`
- **What to build:** The normative handoff-schema doc — the byte-for-byte contract:
  the field-contract table `{schema_version, crew_name, queue, epic_id, goal, captain_name}`
  (all six required; types/charsets per `c3-spec.md §3.1`), the Markdown+YAML-frontmatter
  format, the path convention `.harmonik/crew/missions/<crew_name>.md`, the `--assignee`
  durable-mirror rule (LOCKED field, with the `crew:<name>` label fallback), the
  `session_id`-NOT-in-handoff rationale, and a worked example. **Plus** a concrete filled-in
  gitignored example handoff at `.harmonik/crew/missions/example-handoff.md` (the
  `alpha`/`alpha-q`/`hk-tigaf` case) as a copy-paste template for C2's smoke and C4's author.
  > **DOC-HOME RESOLVED 2026-06-08: operator chose `specs/crew-handoff-schema.md` (spec-first).**
  > The doc HOME was the one genuine operator decision; it is now resolved to
  > `specs/crew-handoff-schema.md` (normative-spec-grade, so C2/C4 are bound by `specs/`-level
  > review and C2's Go must conform to it). T11 is no longer blocked-pending-operator on WHERE.
  > **Implementer: author the schema doc at `specs/crew-handoff-schema.md`.** Everything else in
  > C3 was already locked.
- **Spec reference:** SPEC.md §C3 + Open ("Handoff-schema doc home"); `c3-spec.md §3.1`
  (schema, LOCKED), Files-rows `specs/crew-handoff-schema.md` +
  `.harmonik/crew/missions/example-handoff.md`; `06-integration.md §3` (Mission-handoff row).
- **Deliverables:**
  - Create the schema doc at the operator-resolved path
    `specs/crew-handoff-schema.md`.
  - Create `.harmonik/crew/missions/example-handoff.md` (gitignored example).
- **Acceptance criteria:** C2's seed-read and C4's write agree byte-for-byte with the doc
  (contract review). The example parses as valid frontmatter with all six required fields.
  (No Go test — this is a contract artifact.)
- **Dependencies:** **T2 ONLY** (C1's `epic_completed` is the completion signal the crew must NOT
  pre-empt — the doc's "do not `br close`" rule rides on it). The schema doc is a pure CONTRACT
  artifact (field-contract table + path convention + worked example); its content needs only the
  C1 event framing to be fixed, NOT the C2 Go to exist. It is therefore **NOT gated on T7/T8** —
  authoring the contract early (in parallel with the C2 build) lets C2 itself read a fixed schema.
  (06 §2 Step 2's "C3 requires C1 + C2 landed" rationale applies to the crew-launch SKILL's smoke
  in T12, not to the byte-for-byte schema doc here.)

#### T12 — Author the `crew-launch` skill

- **Proposed bead:** "C3: author crew-launch skill (boot/dispatch/progress feed)" · `type=docs` · `priority=2` · label `codename:captain`
- **What to build:** `.claude/skills/crew-launch/SKILL.md` per `c3-spec.md §3.2` outline:
  frontmatter (`name: crew-launch`, `sources:` → this spec + the schema doc + the three
  composed skills `agent-comms`/`beads-cli`/`harmonik-dispatch`); **boot sequence** (parse
  handoff frontmatter → confirm identity `$HARMONIK_AGENT == crew_name` → `comms join` →
  **mirror `br update <epic_id> --assignee <crew_name>` — NORMATIVE-for-attribution, on
  EVERY adopt, boot AND re-task, per `06-integration.md §4 Gap 1`** → `comms recv --follow
  --json` deduping on `event_id` → boot status); **operating loop** (find ready beads under the
  epic → `harmonik queue submit --queue <queue> --beads ...` — **HARD RULE: NEVER `main`** →
  arm a `subscribe` monitor → do NOT `br close` beads (daemon owns the close that fires C1) →
  loop, idle on the inbox); **progress feed (MANDATORY, success-criterion #3 owner)** — BOTH
  `comms send --to <captain> --topic status` AND `br comments add <epic_id>`, on each observed
  bead-close + a ≤10-min timer + boot/drain bookends; **keeper-restart re-hydration** (re-derive
  `{queue, epic_id}` from handoff + `br show <epic> --assignee`, re-process backlog idempotently);
  **MUST/MUST-NOT guardrails** (dedupe; own-queue only; never terminal `br` writes; no Agent-tool
  sub-agents; JSON output only). Add `crew-launch` to any skill manifest the
  `agent-config-reviewer` tracks (additive).
- **Spec reference:** SPEC.md §C3 + the integration-driven amendment (Gap 1); `c3-spec.md §3.2`
  + §3.3, Files-row `.claude/skills/crew-launch/SKILL.md`.
- **Deliverables:** Create `.claude/skills/crew-launch/SKILL.md`; add to the skill manifest if one exists.
- **Acceptance criteria:** C3 §AC-1 (auto `join` + `recv --follow` + dedupe), §AC-2 (own-queue,
  not `main`), §AC-3 (both progress surfaces, on the locked cadence), §AC-4 (resumable / re-hydrate).
  Validated live by the scenario bead T14 (and exercised in the C3 §6 manual smoke). The Gap-1
  `--assignee`-on-every-adopt rule is present and called out as load-bearing.
- **Dependencies:** **T11** (references the schema doc's field-contract + path convention — must
  be fixed first), plus **T7/T8** (C2): the skill's authoring rides on T11's contract, but its
  **manual smoke needs C2 landed** (`crew start` must work to exercise the boot/dispatch/feed
  loop), so T12 carries the C2 prereqs that T11 no longer does.

---

### C4 — Captain operating context (instructions)

C4 depends on C1 + C2 + C3 (it writes C3's handoff, calls C2's verbs, subscribes to C1's event).

#### T13 — Author the `captain` skill

- **Proposed bead:** "C4: author captain skill (mechanics-only operating loop)" · `type=docs` · `priority=2` · label `codename:captain`
- **What to build:** `.claude/skills/captain/SKILL.md` per `c4-spec.md §4` content outline:
  the mechanical operating loop — BOOT (`comms join`, daemon check via `crew list` +
  `queue status`; exit 17 ⇒ surface, do not spawn) → SPAWN (write the C3 handoff in the LOCKED
  schema → `harmonik crew start <name> --queue <q> --mission <path>` → confirm via `comms who`)
  → ASSIGN/MAIL (`comms send --to <crew> --topic assign`; re-task a LIVE crew over comms, NOT a
  new `crew start`) → WATCH (`subscribe --types epic_completed --json` as the structural trigger;
  on each event, **attribute via `br show <epic_id> --assignee` — the durable mirror, NOT
  `crew list`/`Record.Epic`, per `06-integration.md §4 Gap 1`** → **surface-and-await**: status
  line + `comms send --to operator --topic status`; do NOT pick the next epic) → READ PROGRESS
  (`comms log --from <crew> --topic status`, `br comments list <epic>`, read-only) → RECEIVE
  OPERATOR DIRECTION (`comms recv --follow --json`, dedupe on `event_id`). Encode the
  **judgment-out boundary** (R-C4.6): on completion/stuck/contention → surface + await, never
  rank/fail/rebalance. Permitted `br` writes = comments only (NO terminal transitions). Document
  the **dual-surface operator convention** (Gap 3): status line AND `comms send --to operator
  --topic status`, with `comms log --from <captain> --topic status` as the no-join fallback.
  Include the §7 error-table behaviors (daemon down, `crew start` non-0, crew offline,
  unknown/unassigned epic, duplicate `epic_completed`, sub-epic-before-top-level).
- **Spec reference:** SPEC.md §C4 + the integration-driven amendments (Gap 1 + Gap 3);
  `c4-spec.md §3` (operating loop), §7 (error table), §CONTRACT NOTES; Files-row
  `.claude/skills/captain/SKILL.md`.
- **Deliverables:** Create `.claude/skills/captain/SKILL.md`.
- **Acceptance criteria:** C4 §AC-1 (drives ≥2 spawns on distinct queues), §AC-2 (writes the
  exact-schema handoff + mails the assignment), §AC-3 (receives `epic_completed`, surfaces, no
  assignment action), §AC-4 (judgment-out: zero autonomous ranking/failure/rebalance), §AC-5
  (reads progress without acting), §AC-6 (survives a crew keeper restart — no failure-surface,
  no re-spawn). The Gap-1 attribution-via-`--assignee` and Gap-3 dual-surface conventions are
  present. Validated live by the exploratory bead T13's operator-CLI flow + the E2E scenario T14.
- **Dependencies:** **T11, T12** (writes the C3 handoff schema + drives the crew that runs the
  crew-launch skill), **T2/T3** (subscribes to `epic_completed`), **T7/T8** (calls `crew start/stop/list`).

---

## C. The two MANDATORY test beads (kerf tasks-pass requirement)

Both appear in the dependency graph as dependents of the core impl tasks. **Neither the
plan nor any of its impl beads (T1–T13) may CLOSE until BOTH of these close** — they are
the integration-validation gate for the whole slice.

#### T14 — Scenario test: end-to-end captain + crew

- **Proposed bead:** "scenario: captain — end-to-end captain+crew (twin/real-claude)" · `type=task` · `priority=1` · label `codename:captain`
- **What to build:** The `06-integration.md §6` definitive end-to-end smoke: boot a daemon under
  the supervisor → drive a Captain session (loaded with `captain` + `agent-comms` + `beads-cli`)
  → hand it (as operator) TWO crew assignments referencing two distinct epics, each with ≥2 ready
  child beads → observe ≥2 crew come up on DISTINCT queues (two C3 handoffs, two `crew start`,
  `comms who` shows both, `crew list` shows two records) → each assignment lands (Captain mails
  each crew; each crew `br update <epic> --assignee`-mirrors + dispatches its epic's ready beads
  to its OWN queue, NOTHING in `main`) → progress feeds update (`comms log --from <crew> --topic
  status` + `br comments list <epic>`) → when one epic's last child merges + closes (daemon-owned),
  C1 fires exactly one `epic_completed`; the Captain's `subscribe` receives it, attributes via
  `br show <epic> --assignee`, and surfaces-and-awaits WITHOUT assigning the next epic → restart
  continuity: trigger a keeper cycle (or `--resume <uuid>`) on one crew; confirm it re-appears in
  `comms who`, re-hydrates `{queue, epic_id}`, continues; the daemon kept draining; the Captain
  treats it as a non-event.
- **Spec reference:** `06-integration.md §6` (the definitive end-to-end smoke, steps 1–7);
  SPEC.md §Integration (Testing strategy); cross-cuts success-criteria #1–#6.
- **Deliverables:** A `//go:build scenario` / manual-smoke harness + a run log recording each of
  the seven observations. **NOTE:** this is a `//go:build scenario` / manual-smoke — the daemon
  merge gate SKIPS scenario tests, and session/spawn code MUST be live-smoked under the supervisor
  (`reference_scenario_test_authoring` + `reference_harmonik_daemon_session_nesting`). RUN BY HAND.
- **Acceptance criteria:** All six success criteria observed end-to-end; exactly one
  `epic_completed` per completed epic; zero crew writes to `main`; the Captain takes zero
  autonomous judgment actions; restart continuity holds. Documented live-smoke pass under the
  supervisor.
- **Dependencies:** **T1–T13** (the full C1–C4 impl set; depends on T4 + T10 having proven the
  per-component scenarios/smokes too).

#### T15 — Exploratory test: operator CLI surface

- **Proposed bead:** "explore: captain — operator CLI surface" · `type=task` · `priority=2` · label `codename:captain`
- **What to build:** An exploratory pass over the operator-facing surface — confirm the human
  operator's commands behave as documented and the convention holds without surprises:
  - `harmonik crew start <name> --queue <q> --mission <path>` / `crew stop <name> [--pause-queue]`
    / `crew list [--json]` (start/stop = daemon RPC, exit 17 when down; `list` = local read,
    works daemon-down).
  - `harmonik subscribe --types epic_completed` (the operator watching completions directly).
  - The comms `assign`/`status` flow: `comms send --to <crew> --topic assign`, `comms log
    --from <captain> --topic status` (the dual-surface no-join fallback, Gap 3), `comms who`.
  - Probe rough edges: bad flag combos, daemon-down messaging, `crew list --json` shape,
    re-task vs re-`crew start`. Record findings (usability gaps, surprising errors) as follow-up
    beads if any surface.
- **Spec reference:** SPEC.md §C2 (CLI surface) + §C4 (operator interaction / Gap 3 convention);
  `c2-spec.md §3.1`; `c4-spec.md §3` + §7; `06-integration.md §5` (exit-17 uniformity).
- **Deliverables:** An exploratory run log + any follow-up beads filed for friction found.
- **Acceptance criteria:** Every documented operator command behaves per spec; exit-17 is
  uniform across daemon-RPC verbs; `crew list`/`comms log`/`comms who` work daemon-down; the
  dual-surface operator convention lands (status reachable via `comms log` with no `operator`
  agent online). Documented pass + filed follow-ups.
- **Dependencies:** **T8** (the C2 CLI), **T13** (the C4 captain skill driving the surface).

---

## D. Dependency graph (DAG)

Adjacency list (`X → Y` means **Y depends on X** / X must complete before Y):

```
# C1 chain (Go: core/brcli/daemon)
T1  → T2
T2  → T3
T2  → T4
T3  → T4

# C2 chain (Go: crew/daemon/cmd) — independent of C1 (but T3 & T7 share daemon.go at merge)
T5  → T7
T6  → T7
T5  → T8
T7  → T8
T5  → T9
T6  → T9
T7  → T9
T7  → T10
T8  → T10

# C3 (docs/skill) — T11 (schema doc) depends on C1 (T2) ONLY; T12 (skill) adds C2 (T7/T8)
T2  → T11
T11 → T12
T7  → T12      # T12's manual smoke needs C2 landed (crew start) — T11 no longer carries these
T8  → T12

# C4 (skill) — depends on C1 + C2 + C3
T11 → T13
T12 → T13
T2  → T13
T3  → T13
T7  → T13
T8  → T13

# Mandatory test beads — dependents of the core impl
T1..T13 → T14      # E2E scenario depends on the full C1–C4 set (incl. T4 + T10)
T8  → T15
T13 → T15
```

ASCII view of the critical path + parallelization:

```
            ┌─ T1 ─ T2 ─┬─ T3 ─┐
   (C1)     │           └─ T4 ──┤
            │                   │
START ──────┤                   ├──► T11 ─ T12 ─┐
            │                   │  (T11 schema   ├──► T13 ──┬──► T14  (E2E scenario)
   (C2)     └─ T5 ─┬─ T7 ─┬─ T8 ─┘   doc: dep     │  (C4)    │
                   └─ T6 ─┘  │        T2 ONLY;     │         └──► T15  (explore CLI)
                            T9  T10   T12 skill    │
                          (unit)(smoke) adds T7/T8)
```

- **C1 ∥ C2 run concurrently from START** (logically independent — C1 = core/brcli/daemon-close-path;
  C2 = cmd/crew/daemon-crew-handler — and parallelizable). T1∥T5∥T6 are all root-dispatchable on
  day one. **One shared file:** T3 (C1) and T7 (C2) both edit `internal/daemon/daemon.go`, so their
  edits contend at merge (no cycle, no data race; the daemon's one-at-a-time merge handles it, but
  they are not a literal no-conflict simultaneous pair — see the §A shared-file caveat; prefer
  landing T3's boot-seed `daemon.go` touch before T7's `CrewHandler`-wiring edit).
- **C3:** **T11 (schema doc)** gates on **T2 ONLY** (the C1 event framing) — the contract can be
  authored in parallel with the C2 build. **T12 (crew-launch skill)** gates on T11 and adds T7/T8
  (its manual smoke needs `crew start`, i.e. C2 landed).
- **C4 (T13)** gates on C3 (T11, T12) + the C1/C2 surfaces it consumes (T2/T3, T7/T8).
- **T14** is the final integration gate (all impl); **T15** rides on the CLI (T8) + captain (T13).

**Cycle check:** the adjacency list is a strict partial order — every edge points from a
lower-or-prerequisite task to a later one (T1→T2→T3/T4; T5/T6→T7→T8/T9/T10; T2→T11→T12;
T11/T12→T13→T14/T15). No edge points backward; **no cycles** (re-verified after relaxing
T11's deps to T2-only — dropping the T7→T11 and T8→T11 edges removes constraints, so the
graph stays acyclic). Topological order (one valid schedule, still valid):
`T1, T5, T6 │ T2, T7 │ T3, T8, T9 │ T4, T10, T11 │ T12 │ T13 │ T14, T15`.
(T11 now has only T2 as a predecessor, so it may legally float earlier — e.g. into the
`T3, T8, T9` band right after T2 — but keeping it where it is remains a valid extension.)

---

## E. Coverage check

Every SPEC.md section / component-spec AC mapped to the task ID(s) that cover it. No gaps.

### Success criteria (SPEC.md §Success criteria / §Traceability matrix)

| Criterion | Covered by | Validated by |
|---|---|---|
| #1 ≥2 crew up, each on own queue (C2/C4/C3) | T5, T7, T8 (mechanism), T12 (crew `comms join`), T13 (drives spawns) | T14, T15 |
| #2 assign epic via comms; crew dispatches own queue (C3/C4/C2) | T12 (receive+dispatch), T13 (handoff+mail), T7/T8 (trigger) | T14 |
| #3 crew writes durable progress feed (C3/C4) | T12 (emit, both surfaces), T13 (read) | T14 |
| #4 last child → `epic_completed` → captain receives (C1/C4) | T1, T2, T3 (emit), T13 (subscribe+surface) | T4, T14 |
| #5 crew re-hydrates after keeper restart (C3/C4) | T12 (re-hydrate steps), T13 (off durable state) | T14 |
| #6 captain spawns NEW crew non-interactively (C2/C4/C3) | T6, T7, T8 (mechanism), T13 (trigger), T12 (auto-subscribe boot) | T10, T14 |

### Component acceptance criteria

| Component AC | Task(s) |
|---|---|
| **C1** §AC-1 (exactly-one emit) | T2 (impl) + T4 (scenario) |
| C1 §AC-2 (sibling race → one emit) | T2 (guard impl) + T4 (race sub-test) |
| C1 §AC-3 (still-open child → no emit) | T2 + T4 |
| C1 §AC-4 (no parent → no emit) | T2 |
| C1 §AC-5 (boot survives restart) | T3 (boot seed) + T4 (sub-test) |
| C1 §AC-6 (status surfacing) | T1 |
| C1 §AC-7 (additive/non-breaking) | T1 (+ regression in T2/T9) |
| **C2** §AC-1 (start exits 0 + record + queue + keeper inputs) | T7 (impl) + T10 (keeper-input live-smoke) |
| C2 §AC-2 (two distinct crew + queues; `crew list`) | T7, T8 + T14 (comms-online) |
| C2 §AC-3 (stop teardown + `--pause-queue`) | T7 + T9 |
| C2 §AC-4 (registry unit) | T5 + T9 |
| C2 §AC-5 (launch-spec unit) | T6 + T9 |
| **C3** §AC-1 (auto-subscribe + dedupe) | T12 + T14 |
| C3 §AC-2 (own-queue, not `main`) | T12 + T14 |
| C3 §AC-3 (progress feed both surfaces) | T12 + T14 |
| C3 §AC-4 (resumable after keeper cycle) | T12 + T14 |
| **C4** §AC-1 (≥2 spawns, distinct queues) | T13 + T14 |
| C4 §AC-2 (exact-schema handoff + mail) | T13 + T14 |
| C4 §AC-3 (receive `epic_completed` + surface) | T13 + T14 |
| C4 §AC-4 (judgment-out, no autonomous decision) | T13 + T14 (transcript check) |
| C4 §AC-5 (reads progress without acting) | T13 + T15 |
| C4 §AC-6 (survives crew keeper restart) | T13 + T14 |

### SPEC.md sections / cross-component gaps

| SPEC.md / integration item | Task(s) |
|---|---|
| §C1 (event + status + guard + boot seed) | T1, T2, T3, T4 |
| §C2 (crew command + registry + launch + keeper-attach + stop) | T5, T6, T7, T8, T9, T10 |
| §C3 (handoff schema + crew-launch skill) | T11, T12 |
| §C4 (captain skill) | T13 |
| §Integration build order (C1∥C2 → C3 → C4 → E2E) | encoded in §D DAG |
| 06 §4 Gap 1 (attribution via `--assignee` mirror) | T12 (mirror-on-every-adopt) + T13 (read `br show --assignee`) |
| 06 §4 Gap 2 (no crew-offline event — OUT) | T13 (`comms who` TTL + status-absence heuristic; surface-only) |
| 06 §4 Gap 3 (operator identity — dual-surface) | T13 (status line + `comms send --to operator` + `comms log` fallback) |
| 06 §5 cross-cutting (exit 17 uniform; shared journal; additive) | T7/T8 (exit 17), T2/T3 (shared journal via ScanAfter), T15 (exit-17 uniformity check) |
| C1 §8 / `specs/event-model.md §8` additive row (locked-default) | T2 (implementer SHOULD add the §8 row; flag operator only if §8 frozen) |
| Open decision: handoff-schema doc home (RESOLVED 2026-06-08: operator chose `specs/crew-handoff-schema.md`, spec-first) | T11 (authors at `specs/crew-handoff-schema.md`; see §F) |

**Gaps:** none. Every SPEC.md section, every component AC, every success criterion, and all
three integration gaps map to at least one task; the two mandatory test beads cover the
end-to-end + operator-CLI surfaces.

---

## F. Open-decision note (RESOLVED — carried into a task)

**The one genuine operator decision was the handoff-schema doc HOME — WHERE, not WHETHER.
RESOLVED 2026-06-08: the operator chose `specs/crew-handoff-schema.md` (spec-first).**
It is carried into **T11**, now resolved:

- **Chosen home:** `specs/crew-handoff-schema.md` — normative-spec-grade, so C2/C4 are bound
  by `specs/`-level review and C2's Go must conform to the spec.
- **(Rejected alternative:** `docs/components/internal/crew-handoff-schema.md` — a knowledge-base
  component doc, the former spec-locked default.)
- **Why it mattered:** the path determines the review regime the contract is held to; content is
  byte-identical either way and relocation is purely additive.
- **Implementer instruction (in T11):** author the schema doc at `specs/crew-handoff-schema.md`
  so C2 (T7) and C4 (T13) reference the same one. **Everything else across all four components
  was already locked** — this was the sole genuine open decision in the plan, now closed.

The four C1 "locked-default" items (nested-epic single-level roll-up; at-least-once-on-crash
guard; `specs/event-model.md §8` additive row; two `br show` calls per close) are NOT operator
gates — the implementer proceeds as specified unless the operator overrides. T2 carries the
guard + single-level behavior; T2 also carries the §8 additive row (flag operator only if §8 is
treated as frozen).
