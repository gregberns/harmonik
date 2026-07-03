# hitl-decisions — Integration Plan

**Work:** hitl-decisions (bead hk-0zxv6, P3, feature) · **Codename:** hitl-decisions · **Jig:** plan · **Pass 6 (integration)**
**Scope:** DESIGN-ONLY. This pass orders the components for implementation and records cross-cutting contracts. No code is changed by this work; **paul owns the daemon-infra implementation.**

---

## 1. Locked inputs (record at top)

### 1.1 §9 policy sign-off — the only open gate, now CLOSED

The change-spec carried **exactly one open gate**: the §9 resolution policy. It is now **signed and LOCKED (operator-signed 2026-06-13)**:

- **SINGLE human answerer.** v1 assumes one human answers decisions.
- **FIRST-WRITER-WINS on `decision_id`.** The first `decision_resolved` for a given `decision_id` is authoritative; any later `decision_resolved` (a second human, a replay) is a no-op with no second wake (N3).
- **Multi-human arbitration is DEFERRED (NG1).** Out of v1 scope — no routing rules, no multi-approver chains, no arbitration.

State it plainly: **this was the only thing blocking change-spec → integration → tasks. With the sign-off recorded, the gate is closed and the work advances.** (Sign-off path: operator offline → captain-relayed per paul's mission; recorded in the change-spec §9 and here as a locked input.)

### 1.2 Adopted design decisions D1–D4 (LOCKED at problem-space)

These are locked inputs to this integration plan; they shaped the component decomposition and the build order below.

- **D1 — Ownership seam.** harmonik/comms layer owns the mechanism (events, aggregation, answer-routing); kerf & session-keeper merely *reference* it. One mechanism, one home: the harmonik bus + daemon. Reuses the existing bus, daemon, and named-queues supervise lane with zero new transport or service (C1, C3, C4).
- **D2 — v1 surface.** Both surfaces on ONE shared event contract, **harmonik-side FIRST**. harmonik emits/routes + ships `harmonik decisions` (list/answer) first; the kerf cross-works "what-needs-me" view reads the **same** projection second.
- **D3 — Resolution semantics.** Pick-an-option; decisions stay open until answered (no auto-default in v1, honoring NG4). The event schema reserves an optional free-text `value` field as a v1.1 hook, but v1 parsing accepts enumerated options only.
- **D4 — Durable home.** The event-log projection (§3) is the **source of truth** — `decision_needed − (decision_resolved ∪ decision_withdrawn)`, deduped on `event_id`, keyed on `decision_id`, computed on demand. An optional bead "blocked-on-human" marker is **daemon-written only** (agents never write terminal bead state — C5).

---

## 2. Integration / build order

The components have a strict data dependency: everything depends on the **event types existing** (K1); the **projection** (K3) is a pure fold over those events and depends only on K1; the **CLI surfaces** (K2, K4) depend on K1 to emit and K3 to read; the **reaper** (K5) and **keeper seam** (K6) depend on K3 to know what is open; the **kerf view** (K7) is an out-of-band reader, deferred to a separate repo. Build order:

```
K1 (events)
  ├─→ K3 (projection)
  │     ├─→ K2 (raise/wait CLI)
  │     ├─→ K4 (operator list/answer CLI)
  │     ├─→ K5 (orphan reaper)
  │     └─→ K6 (keeper seam)
  └─ (K7 kerf view — DEFERRED / out-of-band, separate kerf repo)
```

### 2.1 K1 — Event contract — **FIRST**

Type constants + payload structs (`Valid()`) + `RegisterEventType` registration + **fsync-boundary map entries**, all in `internal/core` (`eventtype.go`, `eventregistry.go:79`, new `…payloads` file modeled on `agentcommspayloads_djqc9.go:77`, `busimpl.go:115` for N1).

**Rationale (edges into everything):** the three typed events (`decision_needed` / `decision_resolved` / `decision_withdrawn`) are the shared vocabulary. Nothing can emit, fold, read, or reap a decision before the type constants and payload schemas exist. K1 also carries the N1 fsync-boundary entries — a cross-cutting durability concern (see §4(a)) that must land *with* the types, not bolted on later. K1 is the only component with no upstream dependency.

### 2.2 K3 — Open-decision projection — **second (depends on K1 only)**

A pure fold over `events.jsonl` (`decisionsProjection()` mirroring `ComputePresenceRegistry()`, `comms.go:759-847`, via `ScanAfter`, `jsonlwriter.go:312`).

**Edge K1 → K3:** the fold pattern-matches on the K1 type constants and reads the K1 payload fields (`decision_id`, etc.); it cannot be written before those exist. K3 depends on K1 and **nothing else** — it is a pure function of the log, no socket, no daemon op. It is built second because it is the shared source of truth that *both* CLI surfaces and the reaper read. Landing it early de-risks the readers.

### 2.3 K2 — Agent raise/wait CLI — **third (depends on K1 emit + K3 read)**

New route at `cmd/harmonik/main.go:465`, new `cmd/harmonik/decisions.go` (mirror `comms.go:88`); `raise`→`decisions-raise` and `withdraw`→`decisions-withdraw` daemon emit-ops; `wait` is a **client-side `subscribe` stream, NO new daemon op**, with the N8 arm-then-check ordering.

**Edge K1 → K2:** `raise`/`withdraw` emit K1 events through the daemon op path — they need the K1 constructors and registration. **Edge K3 → K2:** the N8 arm-then-check re-projection (re-scan the log for an already-terminal `decision_id`) calls into the K3 projection logic; the restart re-derive (S7b) also reads K3. So K2 sits below both K1 and K3.

### 2.4 K4 — Operator list/answer CLI — **third, parallel to K2 (depends on K1 emit + K3 read)**

Same `cmd/harmonik/decisions.go` file; daemon op-cases at `socket.go:394`. `list`/`show`→`decisions-list` (the read-op), `answer`→`decisions-answer` (emits `decision_resolved`).

**Edge K1 → K4:** `answer` emits a K1 `decision_resolved` event. **Edge K3 → K4:** `list`/`show` *are* the K3 projection rendered (the what-needs-me queue); `answer` validates "is this `decision_id` open?" against the K3 projection before emitting. K4's `list` is the most direct consumer of K3. K2 and K4 share the same `decisions.go` file and the same upstream pair (K1, K3), so they can be built together once K1 and K3 land.

### 2.5 K5 — Orphan reaper — **fourth (depends on K3 + the keeper tick)**

Keeper-tick is the **sole emitter** of `decision_withdrawn(reason=orphaned, by="keeper")`; `decisions list` (K4) only *flags* orphaned-pending (read-pure). NOT the 1h reconciliation sweep (`daemon.go:405`).

**Edge K3 → K5:** the reaper iterates the K3 open set to find decisions whose `blocked_agent` is gone (`leave`d or Offline ~10min, never Stale — N9 predicate). **Edge "keeper tick is sole emitter" → K5:** the emission must hang off the keeper watch tick (sub-10-min + one-tick latency bound, §5), not the 1h sweep, so K5 also depends on the keeper tick existing as the single-writer host. It is sequenced after K3 (it reads the projection) and naturally pairs with K6 since both live in the keeper.

### 2.6 K6 — session-keeper seam — **fourth, parallel to K5 (depends on K3)**

Keeper consults the K3 projection before reaping an idle agent (`internal/keeper/gates.go:53-88`, `watcher.go:208`); a blocked-on-decision agent with a fresh heartbeat is *blocked*, not *hung* — exempt from the 120s reaper.

**Edge K3 → K6:** the keeper's "is this agent legitimately blocked?" check is a lookup into the K3 projection for an open decision naming that agent. K6 is the *complement* of K5 — K6 protects the live blocked agent; K5 reaps the decision when the agent is truly gone. Both live in the keeper and both read K3, so they are built together, after K3.

### 2.7 K7 — kerf reader view — **DEFERRED / out-of-band**

The kerf cross-works "what-needs-me" view reads the same K3 projection (+ optional daemon-written bead marker for visibility). This lives in the **separate `/Users/gb/github/kerf` repo** and is sequenced **v1-second / out-of-band** (D2).

**Edge K3 → K7 (cross-repo):** K7 consumes the K3 projection's output contract but is implemented in a different codebase on a different release cadence. It is explicitly **out of the harmonik v1 build order** — the harmonik daemon can only document the contract; the kerf-side reader is implemented in the kerf repo. Per D2 the harmonik surface ships first and fully; K7 follows without blocking v1.

---

## 3. Shared state & resources

- **`events.jsonl` is the single source of truth.** There is **no shared in-memory aggregator** — the §3 projection is recomputed on demand by each reader (K3 fold). This is the C3 "no new always-on service" constraint made concrete: state lives only in the append-only log; readers fold it fresh. Durability and restart-survival come for free because the log is durable and the daemon replays it on boot (`daemon.go:1514-1528`).
- **The daemon socket is the shared transport.** All daemon-bound verbs (`raise`/`answer`/`withdraw`/`list`/`show`) dial `<project>/.harmonik/daemon.sock` with `{"op":...,"payload":{}}` → `{ok,result,error}`. **Exit 17 when the socket is absent** (daemon not running) is the shared failure mode every emit/read verb shares. `wait` is the exception — it is a pure client-side `subscribe` stream, but still rides the same daemon's subscribe op.
- **The `fsyncBoundaryEventTypes` map is shared cross-cutting config.** All three K1 event types must be registered in this map (`busimpl.go:115-131`). It is touched once (in K1) but is load-bearing for every downstream component's correctness (N1, see §4(a)).

---

## 4. Cross-cutting concerns

These span component boundaries — each must be honored by more than one component, so they cannot be owned by a single bead in isolation.

### (a) N1 — F-class fsync durability (cross-cutting K1 → all)

All three events (`decision_needed`, `decision_resolved`, `decision_withdrawn`) MUST be in `fsyncBoundaryEventTypes` (`busimpl.go:115-131`). If any is missing, a **terminal can be lost on crash** before the blocked agent reads it → the agent waits forever (Risk R1). The map entry lands in K1, but its correctness is consumed by K2 (the waiting agent), K4 (the answer that must survive), and K5 (the orphan-withdraw that must survive). **N1 is load-bearing** — flag it in the K1 review.

### (b) N8 — arm-then-check ordering (cross-component contract: K2 wait ↔ K4 answer)

This is a **contract between two components**: K2's `wait`/`raise --wait` and K4's `answer`. A subscribe stream only delivers events that arrive *after* it is armed. If K4's `answer` fires `decision_resolved` between the agent's read ("open") and its stream-arm, the terminal is already logged and the fresh stream never sees it → the agent waits forever. K2 MUST therefore: (1) **arm** the `subscribe --types decision_resolved,decision_withdrawn` stream first; (2) **then re-project §3** for this `decision_id`; (3) if already terminal, return immediately; (4) else block. This prevents the **answer-vs-arm race**. It is a K2/K4 *joint* invariant: K4 may emit at any moment, so K2's ordering is the only defense. Call it out as a normative cross-component contract in both beads' reviews.

### (c) N3 — first-writer-wins idempotency (shared by K2, K4, K5)

Resolution/withdrawal is keyed on `decision_id` and idempotent: the **first** terminal for a `decision_id` wins; any later `decision_resolved`/`decision_withdrawn` (second human, replay, race between K4-answer and K5-reap) is a **no-op with no second wake**. This is shared by:
- **K2** — the waiting agent applies the first terminal once, ignores the rest (dedupe on `event_id` per N2 *plus* no-op a second `decision_id` terminal per N3).
- **K4** — `answer` on an unknown/terminal id is a no-op.
- **K5** — `withdraw(orphaned)` on an already-resolved decision is a no-op (handles the answer-just-landed-while-reaping race).

Because all three can produce or consume a terminal for the same `decision_id`, N3 must be enforced consistently in each — it is not localizable to one component.

### (d) Error propagation

- **Socket exit-17** when the daemon socket is absent — uniform across all daemon-bound verbs (§3). Surfaced to the operator/agent as the standard "daemon not running" signal.
- **No-op (not error)** on an unknown or already-terminal `decision_id` for `answer`/`withdraw` (N3). No error return, no second wake — a clean idempotent no-op.

### (e) N6 — git-clean-tree

All writes (events from `raise`/`answer`/`withdraw`, any daemon-written bead marker) land under the **gitignored `.harmonik/`** directory. `decisions answer`/`raise` MUST leave `git status` unchanged (mirrors agent-comms SC-3). This is cross-cutting: every emit path (K2, K4, K5) and any marker write (K7's optional bead marker, daemon-written) must respect it.

---

## 5. Integration testing strategy

### 5.1 The two gate beads (change-spec → integration gate, filed 2026-06-13, label `codename:hitl-decisions`)

- **`hk-rz4`** (scenario-test) — `scenario: hitl-decisions K2+K4 — raise→block→answer→wake end-to-end`. Drives `harmonik decisions raise --wait` + `answer`; asserts the `decision_resolved` JSONL record + agent wake with `chosen_option`, plus first-writer-wins (N3). **Covers S1, S3, S4, S8.** This is the end-to-end happy-path-plus-idempotency gate across K1+K2+K3+K4.
- **`hk-1vl`** (exploratory-test) — `explore: hitl-decisions K4 — decisions list what-needs-me queue across agents`. Drives `harmonik decisions list [--json]`; asserts the cross-agent open-decision queue renders with no aggregator process (S2, S6) and flags Offline-blocked decisions orphaned-pending (N9). **Covers S2, S6** and the K5 *flagging* (read-side) half.

### 5.2 Coverage gap — flagged by an independent reviewer (CALL OUT)

An independent reviewer flagged that the two gate beads do **NOT** cover two success criteria:

- **S5 — durability across restart.** "The open set is identical after a daemon restart; resolved/withdrawn drop out (§3 replay)." Neither gate bead restarts the daemon. This is the K3-projection-replay property and is currently **uncovered**.
- **S7 — orphan reap + agent survival.** "(a) kill a blocked agent → K5 withdraws its decision, it leaves the queue; (b) restart the agent → it re-establishes the wait and still resolves." `hk-1vl` only asserts the *display-side* orphaned-pending flag, not the keeper-tick *emission* of `decision_withdrawn(orphaned)` nor the agent re-establishing its wait. The K5 *emit* path and the S7b restart-rewait are currently **uncovered**.

**Recommendation:** the **K3 impl bead** should carry its own **restart scenario test** (emit → restart daemon → assert the open set is unchanged → resolve → assert it drops out, covering S5), and the **K5 impl bead** should carry its own **reap/re-wait scenario test** (emit → kill the blocked agent → assert the keeper tick withdraws it and it leaves the queue, S7a → restart the agent → answer → assert it wakes or is cleanly withdrawn, S7b). These restart/reap scenarios should be minted as part of those beads when the **tasks pass** runs — the two gate beads alone are insufficient for S5 and S7. (Scenario tests boot real daemons and exceed the daemon's 30-min commit budget; per the project's scenario-test authoring convention they are authored via a worktree sub-agent and cherry-picked, and the daemon gate skips `//go:build scenario` tests — so they must be run independently.)

---

**Next kerf pass:** tasks — mint the K1–K6 impl beads (K7 deferred to the kerf repo) carrying the restart/reap scenario tests above, then `kerf square` / `kerf finalize` for paul's lane.
