# hitl-decisions — Tasks (implementation task list)

**Work:** hitl-decisions (bead hk-0zxv6, P3, feature) · **Codename:** hitl-decisions · **Jig:** plan · **Pass 7 (tasks)**
**Scope:** DESIGN-ONLY. This pass decomposes the work into implementer-ready tasks (K1–K6), weaves the test beads into the dependency DAG, maps every spec section and acceptance criterion to its covering task, and proposes the exact `br create` / `br dep add` calls for the orchestrator (paul). **No code is changed by this work; paul owns the daemon-infra implementation.**

**Inputs (locked):** `SPEC.md` (§1–§9, N1–N9, §7 anchor table, build order), `06-integration.md` (build order + cross-cutting + S5/S7 coverage gap), `05-specs/hitl-decisions-spec.md` (normative change-spec). §9 resolution policy is **operator-signed 2026-06-13** (single human answerer · first-writer-wins on `decision_id` · multi-human arbitration deferred NG1) — the only open gate, now closed.

**Build order (encoded as task deps):**
`K1 (no deps) → K3 (deps K1) → {K2, K4 in parallel (deps K1, K3)} and {K5, K6 in parallel (deps K1, K3)}`. K7 is **DEFERRED** to the separate kerf repo (out of scope — see the deferred row at the end of §A).

---

## A. Implementation tasks (K1–K6)

### K1 — Event contract (3 `decision_*` F-events + payloads + registration + fsync boundary)

- **What to build.** Three new typed events on the EV-001 envelope: `decision_needed`, `decision_resolved`, `decision_withdrawn`. Add the three type constants; define three payload structs each with a `Valid()` method (modeled on `AgentMessagePayload`); register all three constructors via `RegisterEventType`; add all three type names to the `fsyncBoundaryEventTypes` map so they are **F-class durable (N1)**. `decision_id` is the `decision_needed` event's own bus-minted `event_id` (UUIDv7); the two terminals carry it as `payload.decision_id` (distinct from their own `event_id` — C7). Add the EV-025 event-model doc entries (§8.x of the event-model doc).
- **Spec reference.** SPEC.md §1 (1.1/1.2/1.3 schemas); §7 K1 row; N1 (fsync), N2 (dedupe-on-event_id is a downstream consumer rule but the schema must carry a stable `event_id`), N7 (`options` is required ≥1 so `chosen_option` validity is checkable), C7 (`decision_id` distinct from terminal `event_id`s). §6.1 cross-cutting concern (a) N1 F-class fsync (K1→all).
- **Deliverables (from §7 anchor table).**
  - `internal/core/eventtype.go` — 3 new type constants (`decision_needed`, `decision_resolved`, `decision_withdrawn`).
  - `internal/core/eventregistry.go:79` — 3 `RegisterEventType` registrations.
  - new payloads file modeled on `internal/core/agentcommspayloads_djqc9.go:77` — 3 payload structs (`DecisionNeededPayload`, `DecisionResolvedPayload`, `DecisionWithdrawnPayload`) each with a `Valid()` method enforcing required fields (`question`+`options≥1` for needed; `decision_id`+`chosen_option` for resolved; `decision_id`+`reason∈{self_obsoleted,orphaned}` for withdrawn).
  - `internal/core/busimpl.go:115` — add all 3 type names to `fsyncBoundaryEventTypes` (N1, load-bearing).
  - event-model doc — EV-025 entries.
- **Acceptance criteria.**
  - The 3 type constants exist and round-trip through `RegisterEventType` (a constructed event of each type marshals/unmarshals with a stable bus-minted `event_id`).
  - Each payload's `Valid()` rejects the missing-required-field cases (no `options` → error; `chosen_option` not required at schema level but enforced against `options` at K4) and accepts a well-formed payload.
  - All 3 type names are present in `fsyncBoundaryEventTypes` — assert via a unit test that each of the 3 types is reported F-class (guards Risk R1).
  - Unit tests pass; `go build ./...` green.
- **Dependencies.** None (first component).

---

### K3 — Open-decision projection (pure fold over `events.jsonl`) + S5 restart-survivability scenario

- **What to build.** A `decisionsProjection()` function: a pure fold over `events.jsonl` (single `ScanAfter(path, 0)`), folding into `map[decision_id]Decision` — add on `decision_needed` (key = `event_id`), remove on `decision_resolved`/`decision_withdrawn` (key = `payload.decision_id`), dedupe on `event_id` (N2). Open set = `needed − (resolved ∪ withdrawn)`, computed on demand (no persistent aggregator — C3). This is the **shared source of truth** both CLI surfaces (K2/K4) and the reaper (K5/K6) read. Restart-survivability is free because the daemon replays the log on boot.
- **Spec reference.** SPEC.md §3 (open-set projection); §7 K3 row; N2 (dedupe-on-event_id); C3 (no new always-on service); D4 (event-log projection is source of truth); D2 (one projection, read by both surfaces). S5 (durable open set across restart), S6 (renders with no aggregator process).
- **Deliverables (from §7 anchor table).**
  - new `decisionsProjection()` mirroring `ComputePresenceRegistry()` (`internal/daemon/comms.go:759-847`) using `eventbus.ScanAfter` (`internal/eventbus/jsonlwriter.go:312`) — pure fold → open set, returning the open `Decision` set keyed by `decision_id`.
  - a `Decision` value type carrying the fields `decisions list`/`show` render (`question`, `options`, `blocked_agent`, `context_link`, `decision_id`).
  - **REQUIRED: an S5 restart-survivability scenario test** (`//go:build scenario`): emit several `decision_needed` events → restart the daemon → assert the projected open set is **unchanged** (byte-for-byte identical open `decision_id` set) → emit `decision_resolved`/`decision_withdrawn` for a subset → re-project → assert the resolved/withdrawn ones **drop out** of the open set. This scenario is **owned by the K3 task** (the SPEC.md §8.1 coverage gap calls out that the two gate beads do NOT cover S5). Author via a worktree sub-agent + cherry-pick (scenario tests boot real daemons, exceed the 30-min commit budget, and are skipped by the daemon gate's `//go:build scenario` filter — run independently).
- **Acceptance criteria.**
  - Unit test: a synthetic `events.jsonl` with N `decision_needed`, some resolved/withdrawn, and a duplicate `event_id` → projection returns exactly the expected open set (dedupe verified; resolved/withdrawn removed; key on `decision_id`).
  - `decisionsProjection()` is pure (no socket, no daemon op, no side effects) — runs against a log path with no daemon running (de-risks S6).
  - **S5 scenario (REQUIRED, owned here):** open set identical after daemon restart; resolved/withdrawn drop out after re-projection. The `//go:build scenario` test is present and passes when run independently.
- **Dependencies.** K1 (folds on the K1 type constants and reads K1 payload fields).

---

### K2 — Agent raise/wait CLI (`raise` / `wait` / `withdraw` with N8 arm-then-check)

- **What to build.** The agent-side `harmonik decisions` verbs. A new top-level `decisions` route + `runDecisionsSubcommand` verb switch. `raise` → `decisions-raise` daemon op (emits `decision_needed`, returns the minted `decision_id`). `withdraw` → `decisions-withdraw` daemon op (emits `decision_withdrawn(reason=self_obsoleted, by=<agent>)`). `wait` (and `raise --wait`) has **NO new daemon op** — it is a pure **client-side `subscribe` stream** over the existing subscribe op, implementing the **N8 arm-then-check ordering**: (1) arm `subscribe --types decision_resolved,decision_withdrawn` *first*; (2) then re-project §3 (K3) for this `decision_id`; (3) if already terminal, return immediately with the logged result; (4) else block on the stream, dedupe on `event_id` (N2), apply the first terminal once (N3 first-writer-wins). On restart the agent re-derives its open decisions from K3 and re-arms via the same ordering (S7b).
- **Spec reference.** SPEC.md §2 (agent-side verbs + verb→op map); §4 (blocked-wait contract, NORMATIVE; arm-then-check); §7 K2 row; N5 (blocked-wait via held stream), N8 (read-after-arm), N2 (dedupe-on-event_id), N3 (first-writer-wins, no second wake), N6 (clean tree). §6.4 cross-cutting (b) N8 arm-then-check contract (K2↔K4) and (c) N3 idempotency.
- **Deliverables (from §7 anchor table).**
  - `cmd/harmonik/main.go:465` — new top-level `decisions` route (sibling of `comms`).
  - new `cmd/harmonik/decisions.go` — `runDecisionsSubcommand` verb switch (mirror `runCommsSubcommand`, `cmd/harmonik/comms.go:88`); `raise`/`withdraw`/`wait` verbs with flags per §2 (`--question`, repeated `--option`, `--context`, `--from`, `--wait`; `withdraw <id> [--reason self_obsoleted]`; `wait <id>`).
  - daemon emit-ops (mirror `internal/daemon/commshandler_nbrmf.go:39`) — `decisions-raise` (returns `decision_id`), `decisions-withdraw`.
  - `wait`/`raise --wait` client-side subscribe-stream implementation with the N8 arm-then-check ordering (re-projects K3, returns immediately if already terminal, else blocks; prints `chosen_option` on resolve or the withdrawal reason).
- **Acceptance criteria.**
  - `raise` emits a `decision_needed` to `events.jsonl` and prints the minted `decision_id`; exit 17 if the daemon socket is absent.
  - `raise --wait` / `wait <id>` blocks on a held subscribe stream and wakes with `chosen_option` when the matching `decision_resolved` arrives (N5); a `decision_withdrawn` instead prints the withdrawal reason.
  - **N8 race guarded:** if `answer` fires between the agent's read and arm, the agent still returns (the arm-then-check re-projection catches the already-logged terminal) — covered by the hk-rz4 scenario.
  - **N3:** a second terminal for the same `decision_id` is a no-op (no second wake); duplicate `event_id` deliveries deduped (N2).
  - `raise`/`withdraw` leave `git status` unchanged (N6 — writes under gitignored `.harmonik/`).
- **Dependencies.** K1 (emit ops need the K1 constructors/registration), K3 (the N8 arm-then-check + restart re-derive call into the K3 projection).

---

### K4 — Operator list/answer CLI (`list` / `show` / `answer`, read K3 + emit `decision_resolved`)

- **What to build.** The operator-side `harmonik decisions` verbs, in the same `decisions.go` file. `list` (and `show` = `list` filtered to one `decision_id`, client-side) → `decisions-list` daemon read-op: renders the **what-needs-me queue** — every open decision as `question · options · blocked_agent · context_link · decision_id`, across all agents/works (the anti-burial property). `list` also **flags** an open decision whose `blocked_agent` is Offline as *orphaned-pending* (display-only, read-pure — no emit; the keeper tick in K5 does the actual withdrawal). `answer <decision_id> <option>` → `decisions-answer` daemon op: validates `<option>` is one of `decision_needed.options` (N7), validates the `decision_id` is open against the K3 projection, then emits `decision_resolved`; **no-op on an unknown or already-terminal `decision_id` (N3)** — no error, no second wake.
- **Spec reference.** SPEC.md §2 (operator-side verbs); §3 (renders the K3 projection); §5 (`list` flags orphaned-pending, read-pure); §7 K4 row; N3 (idempotent no-op on unknown/terminal), N7 (option validity), N9 (`list` flags only, never emits — the read-side half), N6 (clean tree). S2 (cross-agent list), S6 (no aggregator), S8 (idempotent resolution). §6.4 cross-cutting (b)/(c) (N8 contract counterpart, N3).
- **Deliverables (from §7 anchor table).**
  - `cmd/harmonik/decisions.go` — `list`/`show`/`answer` verbs (`list [--json]`; `show <id>`; `answer <id> <option> [--value <text>] [--resolver <name>]`).
  - daemon op-cases at `internal/daemon/socket.go:394` — `decisions-list` (read-op; renders K3, flags orphaned-pending for Offline `blocked_agent`), `decisions-answer` (validates against K3 + N7, emits `decision_resolved`, no-op on unknown/terminal per N3).
- **Acceptance criteria.**
  - `decisions list` renders open decisions from ≥2 distinct agents in one output with all five fields (S2); `--json` produces machine-readable output.
  - `decisions list` renders with **no aggregator process running** (pure projection read of K3 — S6).
  - `answer <id> <option>` with `<option> ∈ options` emits `decision_resolved` and unblocks the agent (the wake itself is verified in K2/hk-rz4); `<option> ∉ options` is rejected (N7).
  - `answer` on an unknown or already-terminal `decision_id` is a no-op — no error, no second wake (N3, S8).
  - `list` flags an Offline-`blocked_agent` decision as orphaned-pending **without** emitting (read-pure — N9 read-side half).
  - `answer` leaves `git status` unchanged (N6).
- **Dependencies.** K1 (`answer` emits a K1 `decision_resolved`), K3 (`list`/`show` render K3; `answer` validates openness against K3).

---

### K5 — Orphan reaper (keeper-tick sole emitter of `decision_withdrawn(orphaned)`) + S7 reap/re-wait scenario

- **What to build.** The orphan reaper. Iterate the K3 open set on the **keeper watch tick** (NOT the 1h reconciliation sweep — orphan latency must be ≤ Offline-cutoff + one tick, never ~1h). For each open decision whose `blocked_agent` is **truly gone** — predicate (NORMATIVE, N9): the agent emitted an explicit `leave` beat **OR** is **Offline** (past the ~10-min cutoff), **never** merely Stale (120s; a Stale agent is presumed still-blocked because §4 keeps a genuinely-waiting agent Online via its stream heartbeat) — emit `decision_withdrawn(reason=orphaned, by="keeper")`. The **keeper tick is the SOLE emitter** of orphaned-withdrawals (N9); `decisions list` (K4) only *flags* (read-pure). The emit is idempotent: `withdraw(orphaned)` on an already-resolved decision is a no-op (N3 — handles the answer-just-landed-while-reaping race). Because the agent is gone, it never needed the answer → no zombie (S7a).
- **Spec reference.** SPEC.md §5 (orphan reaper predicate + cadence/latency bound, NORMATIVE); §7 K5 row; N9 (predicate + single-writer + latency bound), N3 (idempotent withdraw). S7 (orphan reap + agent survival). §6.4 cross-cutting (c) N3 (the K4-answer vs K5-reap race). Build-order: NOT `daemon.go:405` (1h sweep).
- **Deliverables (from §7 anchor table).**
  - keeper-tick reaper logic — the keeper tick iterates the K3 open set and emits `decision_withdrawn(reason=orphaned, by="keeper")` when the N9 predicate (`leave`d OR Offline, never Stale) holds; **sole emitter**, idempotent (no-op on already-terminal — N3).
  - explicitly **NOT** wired to the 1h reconciliation sweep (`internal/daemon/daemon.go:405`) — the latency bound (≤ Offline-cutoff + one keeper tick) is normative.
  - **REQUIRED: an S7 orphan-reap + re-wait scenario test** (`//go:build scenario`), **owned by the K5 task** (the SPEC.md §8.1 coverage gap calls out that hk-1vl only asserts the display-side flag, not the keeper-tick emission nor the S7b re-wait):
    - **S7a:** emit `decision_needed` → kill the blocked agent → advance to the Offline cutoff → assert the keeper tick emits `decision_withdrawn(orphaned, by=keeper)` and the decision **leaves the open queue** (no zombie); assert a Stale-but-not-Offline agent is **not** reaped.
    - **S7b:** restart the agent → it re-derives its open decisions from K3 and re-arms the wait (N8 ordering) → `answer` → assert it **wakes with `chosen_option`** OR (if already orphaned-withdrawn) is **cleanly withdrawn** with no zombie.
    - Author via a worktree sub-agent + cherry-pick (boots real daemons; skipped by the daemon gate `//go:build scenario` filter; run independently).
- **Acceptance criteria.**
  - The keeper tick emits `decision_withdrawn(orphaned, by=keeper)` for an open decision whose `blocked_agent` is `leave`d or Offline; it does **NOT** emit for a Stale (<10min quiet) agent (N9 predicate).
  - The reaper is the **sole emitter** of orphaned-withdrawals; `decisions list` never emits (verified jointly with K4).
  - `withdraw(orphaned)` on an already-resolved `decision_id` is a no-op (N3 — answer-vs-reap race safe).
  - Orphan latency is ≤ Offline-cutoff (~10min) + one keeper tick — the emit is on the keeper tick, **never** the 1h sweep.
  - **S7 scenario (REQUIRED, owned here):** S7a (kill → keeper-tick withdraw → leaves queue, no zombie; Stale not reaped) and S7b (restart → re-wait → wake-or-clean-withdraw) pass when run independently.
- **Dependencies.** K1 (emits a K1 `decision_withdrawn`), K3 (iterates the K3 open set to find gone-agent decisions).

---

### K6 — session-keeper seam (exempt blocked-on-decision agent from the 120s reaper)

- **What to build.** The keeper-side complement of K5. Before reaping an idle agent as a 120s-silent hang, session-keeper consults the K3 projection: an agent named as the `blocked_agent` of an open decision (and with a fresh heartbeat per §4) is **blocked, not hung** — exempt it from the 120s reaper. Optionally add a `.decision_waiting` keeper staleness-exemption gate (mirroring the `.dispatching` gate) — but the §4 subscribe-stream heartbeat (every 60s) already keeps the gauge fresh, so this gate is belt-and-suspenders / optional. K6 protects the **live blocked agent**; K5 reaps the **decision** when the agent is truly gone — complementary, not in tension.
- **Spec reference.** SPEC.md §4 (keeper-alive via stream heartbeat; optional `.decision_waiting` gate); §5 (keeper seam K6); §7 K6 row; N5 (blocked-wait keeps the agent Online), Risk R2 (an agent that idles bare gets keeper-reaped — the contract must keep it Online). Complement of N9/K5.
- **Deliverables (from §7 anchor table).**
  - `internal/keeper/gates.go:53-88` — exempt a blocked-on-decision agent from the 120s reaper (consult the K3 projection; optional `.decision_waiting` gate mirroring `.dispatching` at `gates.go:79`).
  - `internal/keeper/watcher.go:208` — the 120s-silent-hang reaper consults K3 before reaping (skip an agent that is the `blocked_agent` of an open decision with a fresh heartbeat).
- **Acceptance criteria.**
  - An agent that is the `blocked_agent` of an open decision (per K3) with a fresh heartbeat is **NOT** reaped by the 120s keeper (it is blocked, not hung).
  - An agent with no open decision (or a stale/absent heartbeat past the cutoff) is still reaped normally — K6 does not over-exempt.
  - Unit/integration test: a synthetic K3 open set naming an agent → keeper skips that agent in the 120s reaper; an agent absent from the open set is reaped as before.
- **Dependencies.** K1 (reads K1 event types via K3), K3 (the "is this agent legitimately blocked?" check is a K3 projection lookup).

---

### K7 — kerf reader view — DEFERRED / OUT OF SCOPE (separate kerf repo)

| Item | Detail |
|------|--------|
| **Status** | **DEFERRED — NOT an actionable task in this work.** |
| **Why** | K7 (the kerf cross-works "what-needs-me" reader of the K3 projection, + optional daemon-written bead marker for visibility) lives in the **separate `/Users/gb/github/kerf` repo** on a different release cadence. Per D2 the harmonik surface ships **first and fully**; K7 follows v1-second/out-of-band and does **not block** harmonik v1. The harmonik daemon can only *document* the contract; the kerf-side reader is implemented in the kerf repo. |
| **Action** | Do **NOT** mint a harmonik bead for K7. When the kerf repo is ready, file the reader work there against the K3 projection's output contract (it consumes the K3 open set; no new transport). |

---

## B. Test tasks woven into the DAG

The kerf gate requires test beads to appear as **dependents of the impl tasks** in the DAG. Two gate beads already exist (filed 2026-06-13, label `codename:hitl-decisions`); the two reviewer-mandated coverage additions are **folded as REQUIRED acceptance criteria + a `//go:build scenario` test deliverable WITHIN the K3 and K5 impl tasks** (above), not as separate beads.

### B.1 Existing gate beads (already filed — wire deps only)

| Bead | Kind | What it drives | Covers | Depends on |
|------|------|----------------|--------|------------|
| **hk-rz4** | scenario-test | `harmonik decisions raise --wait` + `answer`; asserts the `decision_resolved` JSONL record + agent wake with `chosen_option`, plus first-writer-wins (N3). End-to-end raise→block→answer→wake. | **S1, S3, S4, S8** | **K1, K2, K3, K4** (needs the events, the raise/wait CLI, the projection the wait re-checks, and the answer CLI) |
| **hk-1vl** | exploratory-test | `harmonik decisions list [--json]`; asserts the cross-agent open-decision queue renders with no aggregator process and flags Offline-blocked decisions orphaned-pending. | **S2, S6** (+ the K5 read-side flag half) | **K3, K4** (needs the projection + the list/answer CLI) |

### B.2 Reviewer-mandated coverage additions (folded into impl tasks — NOT separate beads)

The independent reviewer flagged that the two gate beads do **NOT** cover S5 (restart durability) or the S7 emit/re-wait path. Per the SPEC.md §8.1 / 06-integration.md §5.2 recommendation, these are carried as **REQUIRED acceptance criteria + a `//go:build scenario` test deliverable inside the owning impl task** — they are minted *with* their impl bead, not as standalone test beads:

- **S5 restart-survivability scenario — owned by the K3 task.** Emit `decision_needed` events → restart the daemon → assert the projected open set is **unchanged** → resolve/withdraw a subset → re-project → assert they **drop out**. (See K3 deliverables / acceptance criteria above. Covers **S5**.)
- **S7 orphan-reap + re-wait scenario — owned by the K5 task.** S7a: emit → kill the blocked agent → assert the keeper tick withdraws it (orphaned) and it **leaves the queue** (no zombie); assert Stale-not-Offline is not reaped. S7b: restart the agent → answer → assert it **wakes** or is **cleanly withdrawn**. (See K5 deliverables / acceptance criteria above. Covers **S7**.)

Both scenarios are `//go:build scenario` tests authored via a worktree sub-agent + cherry-pick and run independently (they boot real daemons, exceed the 30-min commit budget, and are skipped by the daemon gate's `//go:build scenario` filter).

---

## C. Dependency graph (DAG — no cycles)

```
                         K1 (events: 3 decision_* F-events + payloads + fsync)
                          │   [no deps — FIRST]
                          ▼
                         K3 (projection: pure fold over events.jsonl)
                          │   [deps K1]  ── carries S5 restart scenario ──┐
                          │                                               │
        ┌─────────────────┼──────────────────┬──────────────────┐        │
        ▼                 ▼                  ▼                  ▼         │
   K2 (raise/wait)   K4 (list/answer)   K5 (orphan reaper)   K6 (keeper  │
   [deps K1,K3]      [deps K1,K3]       [deps K1,K3]          seam)      │
        │                 │              carries S7 scenario  [deps K1,K3]│
        │                 │                  │                  │         │
        └──────┬──────────┘                  │                  │         │
               ▼                             │                  │         │
        hk-rz4 (scenario)                hk-1vl (exploratory)   │         │
        [deps K1,K2,K3,K4]               [deps K3,K4]           │         │
               covers S1,S3,S4,S8        covers S2,S6           │         │
                                                                │         │
  S5 scenario folded into K3 ◄───────────────────────────────────────────┘
  S7 scenario folded into K5 ◄─────────────────────────────────┘

  K7 (kerf reader view) — DEFERRED / out-of-band (separate kerf repo) — NOT in this DAG.
```

**Parallelization.** After K1 → K3 land, the four readers split into two parallelizable pairs: **{K2, K4}** (the `decisions.go` CLI surfaces — same file, same upstream pair) and **{K5, K6}** (the keeper-resident reaper + seam — both read K3). All four depend only on {K1, K3}; none depends on a sibling, so a 2-wide (or 4-wide) wave is sound after K3.

**Acyclicity.** This is a **DAG with no cycles.** Every edge points strictly downward in build order: K1 → K3 → {K2, K4, K5, K6} → {test beads}. No component depends on a descendant; no back-edges; K7 is detached (deferred). The longest path is K1 → K3 → K4 → hk-rz4 (4 hops). Topological order: `K1, K3, [K2|K4|K5|K6], [hk-rz4|hk-1vl]`.

---

## D. Coverage table

### D.1 SPEC.md sections §1–§9 → covering task(s)

| Section | Topic | Covered by | Notes |
|---------|-------|-----------|-------|
| §1 | Event schemas (3 `decision_*`) | **K1** | Type constants, payloads, registration, N1 fsync. |
| §2 | CLI surface `harmonik decisions` | **K2** (agent: raise/wait/withdraw) + **K4** (operator: list/show/answer) | Verb→op map split across K2/K4; `wait` is client-side (K2). |
| §3 | Open-set projection | **K3** | Shared source of truth; read by K2/K4/K5/K6. |
| §4 | Blocked-wait contract (N8 arm-then-check) | **K2** (the wait impl) + **K6** (keeper-alive via heartbeat) | Cross-component contract with K4-answer (N8). |
| §5 | Lifecycle, orphan reaping, keeper seam | **K5** (reaper, sole emitter) + **K6** (keeper exemption) + **K4** (`list` flags orphaned-pending, read-side) | K5 emits; K4 flags; K6 protects the live agent. |
| §6 | Normative conditions N1–N9 | **K1** (N1, N7-schema), **K2** (N5, N8, N2, N3), **K4** (N3, N7, N9-read-flag, N6), **K5** (N9-emit, N3), **K6** (N5/Risk-R2), **all emit paths** (N6) | See §D.3 below for per-N mapping. |
| §7 | Files & changes (anchor table) | **K1–K6** (each row = one impl task) | K7 row = deferred. |
| §8 | Acceptance criteria S1–S8 | hk-rz4 (S1,S3,S4,S8), hk-1vl (S2,S6), K3 scenario (S5), K5 scenario (S7) | See §D.2. |
| §9 | Integration seams & risks | **K1** (Risk R1 / N1 fsync), **K2** (Risk R2 / blocked-wait clause), **K7-deferred** (kerf orthogonality) | §9 policy gate SIGNED — no remaining open decision. |

### D.2 Acceptance criteria S1–S8 → covering test/task

| AC | Statement | Covered by | Status |
|----|-----------|-----------|--------|
| **S1** | raise emits `decision_needed` + agent blocks cleanly (held stream) | **hk-rz4** | Covered (gate bead). |
| **S2** | `decisions list` shows ALL open decisions from ≥2 agents in one output | **hk-1vl** | Covered (gate bead). |
| **S3** | `decisions answer` → originating agent wakes with `chosen_option` | **hk-rz4** | Covered (gate bead). |
| **S4** | replaying the resolve event → no double-apply, no second wake (N2) | **hk-rz4** | Covered (gate bead). |
| **S5** | open set identical after daemon restart; resolved/withdrawn drop out (§3 replay) | **K3 task** (folded `//go:build scenario` restart test — REQUIRED acceptance criterion) | Covered by the K3-owned scenario (closes the gate-bead gap). |
| **S6** | `decisions list` renders with no aggregator process (pure projection) | **hk-1vl** (+ K3 purity AC) | Covered (gate bead + K3 purity unit). |
| **S7** | (a) kill blocked agent → K5 withdraws, leaves queue; (b) restart → re-wait + resolve/clean-withdraw | **K5 task** (folded `//go:build scenario` reap/re-wait test — REQUIRED acceptance criterion) | Covered by the K5-owned scenario (closes the gate-bead gap). |
| **S8** | answering same `decision_id` twice / a bogus id is a no-op (N3) | **hk-rz4** | Covered (gate bead). |

### D.3 Normative conditions N1–N9 → enforcing task(s)

| N | Condition | Enforced by |
|---|-----------|-------------|
| N1 | F-class fsync durability | **K1** (adds 3 types to `fsyncBoundaryEventTypes`); consumed by K2/K4/K5. |
| N2 | dedupe on `event_id` | **K3** (fold dedupe) + **K2** (wait-stream dedupe). |
| N3 | first-writer-wins idempotency | **K2** (apply first terminal once) + **K4** (`answer` no-op on unknown/terminal) + **K5** (`withdraw` no-op on resolved). |
| N4 | write discipline (no agent terminal bead state; daemon-written marker only) | **K4/K5** (any bead marker is daemon-written; agents never write it). |
| N5 | blocked-wait via held stream | **K2** (the wait impl) + **K6** (heartbeat keeps it Online). |
| N6 | clean tree (writes under gitignored `.harmonik/`) | **K2, K4, K5** (every emit path). |
| N7 | option validity (`chosen_option ∈ options`) | **K4** (`answer` validation) + **K1** (schema requires `options ≥1`). |
| N8 | read-after-arm ordering | **K2** (the wait impl) — cross-contract with **K4** (`answer` may emit any time). |
| N9 | orphan-reap predicate + single-writer | **K5** (sole emitter, `leave`d/Offline-not-Stale predicate) + **K4** (`list` flags only, read-pure). |

### D.4 Gap flags

- **No uncovered spec section or acceptance criterion.** S5 and S7 — the two gaps the independent reviewer flagged in the gate beads — are **closed** by the REQUIRED `//go:build scenario` tests folded into the **K3** (S5) and **K5** (S7) impl tasks. Every §1–§9 section and every S1–S8 criterion maps to at least one task.
- **Out of scope (intentional, not a gap):** K7 (kerf reader view) is deferred to the separate kerf repo (D2, v1-second); no harmonik bead. Multi-human arbitration is deferred (NG1, §9 locked). The optional `.decision_waiting` keeper gate (K6) is belt-and-suspenders — the §4 heartbeat already covers keeper-aliveness, so it is optional, not required for coverage.

---

## E. Bead minting plan (for the orchestrator, paul)

**Conventions (all six impl beads):** `--type=task`, `--priority=2` (P2), `--label=codename:hitl-decisions`. Titles are implementer-friendly. **Do NOT pre-assign** any bead (`--assignee` wedges daemon br-claim → instant `max_attempts_exceeded`, no `run_started`; recover via `--assignee ""`). **Do NOT make any impl bead depend on the open epic** (the work's epic / hk-rom): an open-epic dependency makes the task blocked-by the open epic → silent insta-fail at dispatch (`group_failure`, no `run_started`). Attach to the work via the `codename:hitl-decisions` **label**, never a dep on the epic.

> **NOTE — epic dependency hazard (load-bearing).** Per the project's beads conventions, `br dep add <task> <epic>` blocks dispatch while the epic is open. The wiring below adds **only inter-bead (K-to-K and test-to-K) deps** — **never** a dep onto hk-rom (the open epic). The `codename:hitl-decisions` label is the sole attachment to the work.

### E.1 `br create` calls (run, then capture the minted IDs)

```bash
# K1 — no deps
br create --type=task --priority=2 --label=codename:hitl-decisions \
  --title="hitl-decisions K1: 3 decision_* F-events + payloads + registration + fsync-boundary entries"

# K3 — deps K1; carries the S5 restart-survivability //go:build scenario test
br create --type=task --priority=2 --label=codename:hitl-decisions \
  --title="hitl-decisions K3: open-decision projection (pure fold over events.jsonl) + S5 restart scenario"

# K2 — deps K1, K3
br create --type=task --priority=2 --label=codename:hitl-decisions \
  --title="hitl-decisions K2: agent raise/wait/withdraw CLI with N8 arm-then-check"

# K4 — deps K1, K3
br create --type=task --priority=2 --label=codename:hitl-decisions \
  --title="hitl-decisions K4: operator list/show/answer CLI (read K3 + emit decision_resolved)"

# K5 — deps K1, K3; carries the S7 orphan-reap + re-wait //go:build scenario test
br create --type=task --priority=2 --label=codename:hitl-decisions \
  --title="hitl-decisions K5: orphan reaper (keeper-tick sole emitter) + S7 reap/re-wait scenario"

# K6 — deps K1, K3
br create --type=task --priority=2 --label=codename:hitl-decisions \
  --title="hitl-decisions K6: session-keeper seam (exempt blocked-on-decision agent from 120s reaper)"
```

### E.2 Inter-bead dependency wiring (`br dep add <bead> <depends-on>`)

Substitute the minted IDs (`$K1`…`$K6`) captured from §E.1. Convention: `br dep add <task> <prereq>` makes `<task>` blocked-by `<prereq>`.

```bash
# Build order: K1 → K3 → {K2,K4,K5,K6}
br dep add $K3 $K1          # K3 depends on K1

br dep add $K2 $K1          # K2 depends on K1 and K3
br dep add $K2 $K3

br dep add $K4 $K1          # K4 depends on K1 and K3
br dep add $K4 $K3

br dep add $K5 $K1          # K5 depends on K1 and K3
br dep add $K5 $K3

br dep add $K6 $K1          # K6 depends on K1 and K3
br dep add $K6 $K3
```

### E.3 Wire the two existing test beads (hk-rz4, hk-1vl) onto the impl beads

These beads already exist — only add the dependency edges (substitute the minted impl IDs):

```bash
# hk-rz4 (scenario: raise→block→answer→wake end-to-end) depends on K1,K2,K3,K4
br dep add hk-rz4 $K1
br dep add hk-rz4 $K2
br dep add hk-rz4 $K3
br dep add hk-rz4 $K4

# hk-1vl (exploratory: decisions list what-needs-me) depends on K3,K4
br dep add hk-1vl $K3
br dep add hk-1vl $K4
```

### E.4 Reminders for the orchestrator

- **No epic dep.** None of the calls above add a dep onto hk-rom (the open epic) — that would silent-insta-fail dispatch. Attachment to the work is via the `codename:hitl-decisions` label only.
- **No pre-assignment.** Leave every impl bead `open` and unassigned; the daemon owns status transitions (do not `br update --status=in_progress` before `queue submit` — that yields a false `bead_already_dispatched`).
- **Dispatch order.** Submit K1 first; wait for its merge; then K3; wait for its merge; then K2/K4/K5/K6 (a 2- or 4-wide wave is sound once K1+K3 have landed — they all branch off the same {K1,K3} base, so they merge one-at-a-time without conflict on disjoint files). The two test beads (hk-rz4, hk-1vl) dispatch last, after their prereqs merge.
- **Scenario tests run independently.** The S5 (K3) and S7 (K5) `//go:build scenario` tests, plus hk-rz4, boot real daemons and are skipped by the daemon gate's `//go:build scenario` filter — author via a worktree sub-agent + cherry-pick and run them independently before closing the owning bead.

---

**Next kerf pass:** `kerf square` (verify completeness) → `kerf finalize --branch <name>` (package for paul's lane). K7 deferred to the `/Users/gb/github/kerf` repo.
