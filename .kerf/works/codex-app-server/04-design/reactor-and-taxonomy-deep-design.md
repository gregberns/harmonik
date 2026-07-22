# 04 — Reactor & Taxonomy deep-design (corpus-INDEPENDENT): codex-app-server Phase 1

> codename:codex-app-server · parent epic hk-q3ovr · Codex lane (piter)
> **Direct-writing deep-design, NO code / NO daemon.** Front-loads the layers whose logic depends on
> the **protocol SHAPE** (Thread→Turn→Item, JSON-RPC 2.0), not on which concrete fields appear in
> captured frames — so they are fully designable before the corpus exists.
>
> **Authority / grounding (cited per choice below):**
> - Plan (ratified): `plans/2026-07-11-codex-app-server-replan/PHASE-1-tap-serializer-reactor.md` — §1 tap, §2 serializer, §3 reactor, §twin, §test-taxonomy. Referenced as **[PLAN §n]**.
> - Tasks: `07-tasks.md` — T2 hk-tg5mo, T3 hk-5co9a, T4 hk-swc8p, T5 hk-oe86p, gates. Referenced as **[TASKS Tn]**.
> - Skeleton: `07-step0-build-skeleton.md` — packages `internal/codextap`, `internal/codexwire`; flat-package convention; depguard; testhelpers. Referenced as **[SKEL]**.
> - Wire surface: `03-research/C1-protocol/findings.md` — methods, JSON-RPC framing, `-32001`, 30-min unload, server-side context. Referenced as **[C1 §n]**.
>
> Every "deferred-to-build" flag marks a genuinely **corpus-dependent** choice that cannot be settled
> until real captured frames exist.

---

## 0. What is corpus-independent, and why these layers qualify

The wire is JSON-RPC 2.0 with a fixed conceptual model: a **Thread** contains **Turns**, a Turn
contains **Items**, and the server streams `item/*` + `turn/*` notifications until `turn/completed`
[C1 §1]. The *envelope* framing, the *request/notification/response* trichotomy, the *ordering and
lifecycle* of turns and items, the *backpressure* code `-32001`, and the *resume-replays-history*
semantics are all part of that shape and are already confirmed [C1 §1–5]. None of them depend on the
exact field names inside `params`. Therefore:

- **T2 envelope half** (JSON-RPC 2.0 framing) — shape-only → designable now (§4).
- **T3 reactor** (state machine over typed events; two invariants; resume-as-pure-state) — its logic is
  driven by turn/item *lifecycle*, not by params fields → designable now (§1).
- **T5 taxonomy** (L0–L3 levels, make targets, drift canary) — a policy layer over "replay vs live" →
  designable now (§2).
- **T4 twin** (fault-injection over the real transport) — its fault model is transport-level → designable
  now (§3).

Corpus-dependent (deferred): the **payload structs** (which fields, required vs optional), the JSON-RPC
`id` concrete type, token-usage field names, and semantic-equal number/whitespace canonicalization
[SKEL §"Open decisions"]. Flagged inline as **⟪DEFERRED-TO-BUILD⟫**.

---

## 1. T3 — REACTOR: EventSource → Reactor → Effector

**Bead hk-5co9a. Package (proposed): `internal/codexreactor/`** — flat leaf, same convention as
`codextap`/`codexwire` [SKEL §"Convention note"]. Depends on T2 typed events [TASKS T3].

### 1.1 The one seam (three interfaces)

[PLAN §3]: "One seam: `EventSource → Reactor → Effector`. Reactor consumes **typed** events, fed
identically by live socket, replayed fixture, or synthetic scenario — indistinguishable. Every side
effect is an `Action` through one interface; `FakeEffector` records them."

**Interface sketch (shapes, not bodies):**

```
// EventSource yields typed wire events. THREE implementations feed it identically —
// the Reactor cannot tell them apart (that indistinguishability IS the design):
//   • liveSource     — decodes codexwire.Envelope off the live JSON-RPC socket
//   • replaySource   — reads a captured/synthetic .jsonl and yields the same typed events
//   • scenarioSource — hand-authored synthetic stream for a taxonomy family (§1.6)
// It yields Events, not raw frames: envelope+payload already parsed by T2 (codexwire).
EventSource interface {
    Next(ctx) (Event, bool, error)   // (event, ok, err); ok=false at end-of-stream
}

// Reactor is a PURE state machine. Given the current state + one Event, it returns
// the next state + zero-or-more Actions. NO I/O, NO clock read, NO effect — pure fn.
// This is what makes reconnect/backpressure provable offline with crafted streams.
Reactor interface {
    // conceptually: Step(state State, ev Event) (State, []Action)
    Step(ev Event) []Action          // Reactor owns its State internally; returns Actions
}

// Effector is the ONE side-effect boundary. Real impl talks to comms/queue/beads;
// FakeEffector records Actions in order for the "given stream → these actions" test.
Effector interface {
    Apply(ctx, a Action) error
}

// FakeEffector (test double): appends every Action to an ordered slice; asserts nothing.
// The whole test shape: feed a scenarioSource → collect FakeEffector.Recorded → compare
// to the family's expected Action sequence (§1.6). Zero live effects, sub-ms. [PLAN §3]
```

**Event** is the typed union the T2 serializer produces (envelope discriminated + payload resolved via
the method registry [SKEL T2]). The Reactor switches on the event's **kind** — a small closed set drawn
from the confirmed method/notification vocabulary [C1 §2]:

```
EventKind (closed enum — protocol-shape, not corpus):
  ThreadStarted        (from thread/started)          — carries server-minted thread id [C1 §3]
  TurnStarted          (from turn/started)
  ItemStarted          (from item/started)
  ItemCompleted        (from item/completed)
  ItemDelta            (from item/agentMessage/delta & friends)  — streaming chunk
  TurnCompleted        (from turn/completed)           — carries final status + token usage
  TokenUsageUpdated    (from thread/tokenUsage/updated)
  CompactStarted/Done  (from thread/compact/start progress)
  RPCError             (from a JSON-RPC error frame; code -32001 = backpressure) [C1 §4]
  ThreadClosed         (from thread/closed; 30-min idle unload) [C1 §5]
```

⟪DEFERRED-TO-BUILD⟫ The exact `item/*` sub-notification names beyond the happy set (reasoning, plan,
command-output, tool-progress) come from the corpus [SKEL open-decision #1]; the Reactor treats any
unmodeled item notification as a generic `ItemStarted/Delta/Completed` by its lifecycle position — the
*kind* is shape, the *sub-type* is corpus.

### 1.2 The Action type (orchestrator side-effects — SKETCH/enum)

An orchestrator-reactor emits Actions when the wire stream implies harmonik should *do* something.
Kept a sketch — the concrete payloads firm up when the reactor is wired to real comms/queue/beads
(that wiring is L2/Phase-2, not this design). Grounded in what a harmonik orchestrator already does
(dispatch, comms, bead updates — AGENTS.md daily loop):

```
Action (enum sketch — the side-effect vocabulary):
  StartTurn{threadID, input}         — send a turn/start (one-in-flight gated, §1.3)
  SteerTurn{threadID, turnID, input} — turn/steer into a running turn [C1 §2]
  InterruptTurn{threadID, turnID}    — turn/interrupt (cancel family) [C1 §2]
  Compact{threadID}                  — thread/compact/start under token pressure [C1 §4]
  ResumeThread{threadID, cursor}     — thread/resume + replay-from-cursor (§1.5)
  SubmitQueue{queueRef, work}        — hand downstream work to harmonik queue
  CommsSend{to, topic, body}         — status/coordination over the comms bus
  BrUpdate{beadID, transition}       — advisory bead signal (daemon owns TERMINAL writes;
                                        reactor NEVER pre-sets in_progress — MEMORY, beads-cli)
  Noop                                — event consumed, no side effect (e.g. a dup, §1.4)
```

The Action set is deliberately small and closed: adding one is a one-line enum edit, mirroring the T2
method-registry "drift = one-line edit" discipline [PLAN §2].

### 1.3 Invariant A — one-turn-in-flight backpressure (PURE STATE)

[PLAN §3]: "one-turn-in-flight backpressure". Only ONE `turn/start` may be outstanding **per thread**;
new work queues until `turn/completed`. This is a per-thread guard, because one app-server hosts many
concurrent threads [C1 §6] — the invariant is *per thread*, not per connection.

**Per-thread turn-state machine:**

| State | Meaning | Event → | Next state | Action(s) emitted |
|---|---|---|---|---|
| `Idle` | no turn outstanding | `submit work` (internal) | `Starting` | `StartTurn` |
| `Idle` | | `TurnStarted` (unexpected) | `Idle` | `Noop` (log-anomaly) |
| `Starting` | StartTurn sent, awaiting ack | `TurnStarted` | `Running` | — |
| `Starting` | | `RPCError(-32001)` | `Idle` + requeue | `Noop` (retry-later; work stays queued) |
| `Running` | turn in flight, items streaming | `ItemStarted/Delta/Completed` | `Running` | per-item Action or `Noop` |
| `Running` | | `submit work` (internal) | `Running` (**enqueue**) | — (queued, NOT sent) |
| `Running` | | `TurnCompleted` | `Draining`→`Idle` | drain: pop 1 queued → `StartTurn` |
| `Running` | | `InterruptTurn` requested | `Interrupting` | `InterruptTurn` |
| `Interrupting` | interrupt sent | `TurnCompleted(status=interrupted)` | `Idle` | drain queued work |

The **queue of pending work per thread** is part of pure state. The invariant is provable by a crafted
stream: feed two `submit work` events with no intervening `TurnCompleted` and assert exactly **one**
`StartTurn` Action fired, the second held until the completion event drains it. Backpressure `-32001`
[C1 §4] is modeled as "the StartTurn didn't take; work returns to the queue head" — no Action re-fired
until a retry tick (a retry is itself an internal event, keeping the machine pure).

### 1.4 Invariant B — dedup-by-seq (idempotent at-least-once / replay)

[PLAN §3]: "dedup-by-seq". `item/*` notifications may arrive **more than once** — the comms substrate
is at-least-once (MEMORY: agent-comms N3 dedupe), and on reconnect the server **replays history**
[C1 §5], so already-processed items re-appear. Handling must be idempotent.

**Key on what?** Proposal, in priority order:

1. **Primary key = `(threadId, turnId, itemId)`** when the item carries a stable item id. Item identity
   within a turn is the natural idempotency key — a re-delivered `item/completed` for the same
   `itemId` is a duplicate by definition.
2. **Fallback / ordering key = a monotonic `seq`.** The **tap already stamps a monotonic per-session
   `seq`** on every captured frame [SKEL T1 capture-envelope: `{"seq":0,...}`]. For the *live* path the
   reactor maintains its own **per-thread high-water `lastSeq`** cursor (the last event position it has
   fully processed). Any event whose position ≤ `lastSeq` on the *same thread* after a resume is a
   replay → dropped.

⟪DEFERRED-TO-BUILD⟫ Whether the app-server emits a stable per-item id and/or a per-notification sequence
number on the wire is a corpus question [SKEL open-decision #1/#3]. **Design decision that survives
either way:** dedup keys on `(threadId, turnId, itemId)` when an id is present, else on the reactor's
own high-water cursor derived from arrival order. The tap `seq` is the capture-side ground truth that
lets replay tests assert dedup deterministically.

**Dedup state machine (per thread):**

| State | Event | Condition | Next | Action |
|---|---|---|---|---|
| tracking `seen:set`, `lastSeq` | `ItemX` | key ∉ seen AND pos > lastSeq | advance lastSeq; add key | normal per-item Action |
| | `ItemX` | key ∈ seen OR pos ≤ lastSeq | unchanged | **`Noop`** (dup dropped) |
| | `TurnCompleted` | first time for turnId | mark turn done | drain (§1.3) |
| | `TurnCompleted` | turnId already done | unchanged | `Noop` (replayed completion) |

The two invariants compose: dedup runs **first** (drop replays), then the surviving event drives the
turn-in-flight machine. This ordering is why a replayed `TurnCompleted` after reconnect does **not**
spuriously drain queued work — it's dropped as a dup before it can reach §1.3.

### 1.5 Reconnect-resume as PURE STATE

[C1 §5]: on reconnect the client `initialize`s then `thread/resume`s the stored `threadId`; the server
**reconstructs and replays history**. The reactor must **not re-fire Actions for already-processed
items** — this is where dedup-by-seq (§1.4) earns its keep.

**Model:** the reactor's durable per-thread state is exactly `{threadID, turnState, seen-set,
lastSeq/cursor, pending-work-queue}` — a small bounded record [C1 §"keeper-relevance": "not an
ever-growing transcript"]. Reconnect is modeled as an internal `Reconnect` event:

| State before drop | `Reconnect` event | Emits | Then replay stream… |
|---|---|---|---|
| `Running`, cursor=K | `Reconnect` | `ResumeThread{threadID, cursor=K}` | server replays items 0..N |
| | replayed `Item` pos ≤ K | — | `Noop` (dedup drop) — the crux |
| | replayed `Item` pos > K | — | normal Action (genuinely new, arrived post-drop) |
| | replayed `TurnCompleted` (turn already done) | — | `Noop` |
| | replayed `TurnCompleted` (turn was still Running at drop) | — | drain (legitimate first completion) |

So resume is *not* special-cased in the effect logic — it is just "emit `ResumeThread`, then let dedup
filter the replay." A crafted reconnect scenario (§1.6) proves: after `ResumeThread`, **zero** duplicate
downstream Actions fire for items already processed pre-drop, and exactly the post-drop tail fires.

### 1.6 Scenario taxonomy → expected Action sequence (the T3 test corpus)

[PLAN §3 Gate / TASKS T3 Gate]: "a growing library of scenario `.jsonl` (happy, tool-call-mid-turn,
error, cancel, reconnect-resume, token-pressure, out-of-order/dup) each asserts the reactor's action
sequence." Each family is a synthetic `.jsonl` fed via `scenarioSource`; the test asserts
`FakeEffector.Recorded` equals the expected sequence.

| Family | Input event stream (typed) | Expected Action sequence (asserted) |
|---|---|---|
| **happy** | ThreadStarted → (submit) → TurnStarted → ItemStarted → ItemDelta* → ItemCompleted → TurnCompleted | `StartTurn`, [per-item Actions…], (drain: `Noop` if queue empty) |
| **tool-call-mid-turn** | …Running → ItemStarted(tool) → ItemDelta(tool-progress) → ItemCompleted(tool) → ItemStarted(agentMsg) → … → TurnCompleted | tool item Action(s) interleaved; **still ONE** `StartTurn`; turn stays `Running` across the tool item |
| **error** | Starting → `RPCError(code≠-32001)` (e.g. bad params) | surface Action (`CommsSend` error-report) + return to `Idle`; work NOT silently dropped |
| **backpressure** | Starting → `RPCError(-32001)` → (retry tick) → TurnStarted → … → TurnCompleted | first `StartTurn`, `Noop`(overload), **second** `StartTurn` on retry, then normal — proves work requeued not lost |
| **cancel/interrupt** | Running → (interrupt) → TurnCompleted(status=interrupted) | `InterruptTurn`, then drain; NO further item Actions after interrupt |
| **reconnect-resume** | Running(cursor=K) → Reconnect → ResumeThread → replay 0..N | `ResumeThread`, then **only** Actions for pos>K; zero dup Actions for pos≤K (§1.5) |
| **token-pressure/compact** | Running → TokenUsageUpdated(high) → (policy) → CompactStarted → CompactDone → TurnCompleted | `Compact` emitted once at threshold; compaction progress items produce `Noop`/status only; turn completes normally |
| **out-of-order/dup** | ItemCompleted(id=7) BEFORE ItemStarted(id=7); duplicate ItemCompleted(id=5) | dup id=5 → `Noop`; out-of-order tolerated by keying on id not arrival — assert item id=7 handled once, id=5 once |

⟪DEFERRED-TO-BUILD⟫ The *concrete field values* inside each scenario's frames (real `item` payloads,
real token counts, real error `data`) are authored from captured corpus once T1 lands; the *event kinds
and their order* — which is what these assertions key on — are protocol-shape and fixed now. The token
threshold that triggers `Compact` is a **policy knob** (⟪DEFERRED⟫: automatic vs caller-triggered
compaction is C1 open-question #1 [C1 §4]) — design it as an injected policy so the scenario can set it
deterministically.

---

## 2. T5 — TEST TAXONOMY (L0/L1/L2/L3 + make targets)

**Bead hk-oe86p.** [PLAN §"Test taxonomy"]/[TASKS T5]: "95%+ zero-token by construction." Depends on
T2+T3+T4.

### 2.1 The four levels — what each tests, and why it's zero-token

| Level | Name | Tests exactly | Fed by | Token cost | Why |
|---|---|---|---|---|---|
| **L0** | unit | serializer round-trip / golden / malformed-frame handling; envelope discrimination (§4); dedup & turn-state pure-fn transitions | in-memory literals + T1 captured `raw.jsonl` in `testdata/` | **zero** | pure functions over bytes/events; no process, no socket [PLAN §L0] |
| **L1** | contract | reactor **behavior vs captured `.jsonl`** replayed through the **twin** — "given this real stream, these Actions" | T4 twin replaying captured fixtures over real transport | **zero** | replay of already-captured bytes; the model is never called [PLAN §L1] |
| **L2** | integration | reactor + **faked** comms/queue/beads (FakeEffector, fake queue) — end-to-end orchestration logic | scenario `.jsonl` + FakeEffector | **zero** | every effect boundary is a test double; no live Codex, no live daemon [PLAN §L2] |
| **L3** | live | **only the wire contract itself** — ≤4 scenarios, env-gated `CODEX_LIVE=1`, token-capped | real `codex app-server` | **non-zero (capped)** | the SOLE sanctioned live cost; verifies the real wire still matches our envelope/registry [PLAN §L3] |

**The rule** [PLAN §"Rule"]: a test may hit real Codex **only** to verify the wire contract; everything
verifying **our** logic is a replay test. L0–L2 are 100% of the logic coverage and are zero-token by
construction. L3 is the exception and is *contract-only* — never used to test reactor/queue/comms logic.

### 2.2 make targets

```
make test              # DEFAULT: CODEX_LIVE=0 → runs L0+L1+L2 only. Zero token. [PLAN §"Rule"]
                       #   The everyday gate; green here = all OUR logic verified offline.
make test-live         # CODEX_LIVE=1 → adds L3 (≤4 contract canaries). Token-capped; deliberate.
make capture-fixtures  # DELIBERATE, budget-capped, LEDGERED harvest: drive real app-server through
                       #   the T1 tap to grow the corpus. NOT run by `make test`. Writes a ledger
                       #   entry (scenario, token spend, ts) so capture cost is auditable. [PLAN §"Rule"]
make drift-canary      # The SINGLE pre-deploy live check: re-run L3 happy-path against real wire to
                       #   confirm the contract hasn't drifted since the corpus was captured.
                       #   L3 happy-path == the PRE-DEPLOY E2E GATE artifact. [PLAN §L3 / TASKS T5]
```

- `make test` gates every commit; it must be green with `CODEX_LIVE=0` against real captured frames
  [TASKS T5 Gate].
- **Single drift canary** [PLAN §"Rule"]: exactly one pre-deploy live probe re-checks wire shape — not a
  suite. It answers "did OpenAI change the wire under us?" and nothing else. Its green is the go/no-go
  for deploy; its red means re-capture + re-run the T2 conformance gate, not a code patch.
- `make capture-fixtures` is the only path that spends tokens routinely, and it is opt-in + ledgered so
  the "95%+ zero-token" property is observable, not aspirational.

---

## 3. T4 — DIGITAL TWIN (brief): fault-injection over the real transport

**Bead hk-swc8p.** [PLAN §"Supporting"]/[TASKS T4]: fake app-server replaying captured `.jsonl` over the
**real transport** (newline-delimited JSON stdio [C1 §1]), with fault injection. Depends on T1
(fixtures) + T3 (consumer to drive). "Capture once, replay forever."

**Key property:** the twin replays over the **real transport**, so the T3 reactor's `liveSource` path is
exercised byte-for-byte as it would be against real Codex — only the *bytes' origin* is a fixture, not
the model. This is what makes L1 "contract via the twin" a faithful, zero-token proxy for live behavior.

**Fault-injection model → which T3 machine it exercises:**

| Flag | Transport-level effect | Drives T3 machine | Proves |
|---|---|---|---|
| `--drop-after=N` | close the connection after emitting N frames | §1.5 reconnect-resume | reactor emits `ResumeThread` + dedups the replay tail |
| `--stall[=dur]` | stop emitting mid-turn, hold the socket open | §1.3 turn-in-flight | turn stays `Running`; new work queues; no spurious drain; watchdog surface (Phase-2) |
| `--truncate` | cut a frame mid-line (torn tail) | T2 envelope strict-decode + reactor error path | malformed frame is a red test, not a silent shrug [SKEL: torn-tail convention] |
| `--dup` | re-emit already-sent frames | §1.4 dedup-by-seq | duplicate `item/*`/`TurnCompleted` → `Noop`, exactly-once downstream Actions |

The twin needs no model auth and no tokens: it is a byte pump reading `raw.jsonl` (the T1 capture format
[SKEL T1]) and speaking the real stdio framing. Fault flags are transport mutations layered on top of an
otherwise verbatim replay — so a scenario is "real captured happy turn + one injected fault," keeping
fixtures reusable across fault modes.

⟪DEFERRED-TO-BUILD⟫ WebSocket/Unix-socket transports [C1 §1] are out of scope for Phase-1 twin — stdio
newline-delimited JSON is the confirmed happy path [SKEL T1] and the only transport the tap captures.

---

## 4. T2 — ENVELOPE (the corpus-INDEPENDENT half only)

**Bead hk-tg5mo.** The JSON-RPC 2.0 **trusted envelope** is a KNOWN spec [C1 §1], so it is fully
designable now. The **untrusted payload** structs stay ⟪DEFERRED-TO-BUILD⟫ (corpus-dependent — which
methods, which fields, required-vs-optional [SKEL open-decisions #1–4]).

### 4.1 Strict Envelope type + request/response/notification discrimination

JSON-RPC 2.0 has exactly three frame archetypes, discriminated by field presence:

| Archetype | `method` | `id` | `params` | `result`/`error` |
|---|---|---|---|---|
| **Request** | present | present | optional | — |
| **Notification** | present | **absent** | optional | — |
| **Response (ok)** | — | present (matches request) | — | `result` present |
| **Response (err)** | — | present | — | `error` present |

Discrimination rule (strict — we own this layer [PLAN §2]):
- `method != "" && id present` → **Request**
- `method != "" && id absent` → **Notification** (all the `item/*`, `turn/*`, `thread/*` server pushes
  [C1 §2] are notifications — no id)
- `method == "" && id present && result present` → **Response(ok)**
- `method == "" && id present && error present` → **Response(err)**
- anything else (both method+result, neither, unknown top-level key) → **hard decode error**, a red test,
  never a runtime shrug [PLAN §2: "A real message our types can't handle is a red test"].

Envelope type shape (from [SKEL T2], reproduced as the settled shape):

```
Envelope {
  JSONRPC string           // MUST == "2.0" — strict, else decode error
  ID      *json.RawMessage // present ⇒ request or response; absent ⇒ notification
  Method  string           // present ⇒ request or notification; empty ⇒ response
  Params  json.RawMessage  // untrusted payload — routed via method registry (deferred structs)
  Result  json.RawMessage  // untrusted payload (response ok)
  Error   *RPCError        // response err
}
```

**Strictness** = unknown *top-level* envelope keys are a hard error (we own the framing), while unknown
*keys inside* `params`/`result` are preserved-and-counted in the payload's `Extra` (the untrusted,
deferred half) [PLAN §2 / SKEL T2]. This is the trusted/untrusted seam stated precisely.

### 4.2 Error-object shape

JSON-RPC 2.0 error object [C1 §4] — fully known:

```
RPCError {
  Code    int              // e.g. -32001 "Server overloaded; retry later" (backpressure) [C1 §4]
  Message string
  Data    json.RawMessage  // optional, untrusted — preserved raw
}
```

`-32001` is the one semantically load-bearing code today: the reactor maps it to the backpressure path
(§1.3), distinct from all other errors which take the `error`-family surface path (§1.6). Standard
JSON-RPC reserved codes (`-32700` parse, `-32600` invalid request, `-32601` method-not-found, `-32602`
invalid-params, `-32603` internal) are recognized as shape; their reactor treatment is the generic
error family unless the corpus shows one needs special handling ⟪DEFERRED-TO-BUILD⟫.

### 4.3 Explicitly deferred (corpus-dependent) — do NOT design now

- **Payload structs** (`ThreadStartParams`, `TurnStartParams`, item payloads, token-usage) — modeled
  only for methods the corpus contains, optional-by-default [SKEL T2, open-decision #1/#2].
- **JSON-RPC `id` concrete type** — kept `*json.RawMessage` (string OR number) until the corpus shows
  which [SKEL open-decision #3].
- **`turn/completed` / `thread/tokenUsage/updated` field names** — summarized from a web fetch; model
  from captured bytes, not findings prose [SKEL open-decision #4; C1 open-q #4].
- **Semantic-equal normalization** (number/whitespace canonicalization) — key-order-insensitivity is
  settled; the rest depends on what Codex emits [SKEL open-decision #6].

---

## 5. Cross-cutting design notes

- **Purity is the testability lever.** T3's Reactor is a pure `(state, event) → (state, []action)`
  function; all three EventSources feed it identically [PLAN §3]. This is what lets L0/L1/L2 be
  zero-token: no I/O to stub, only a stream to author and an Action list to compare.
- **Bounded state, no transcript.** The reactor holds only `{threadID, turnState, seen-set, cursor,
  pending-queue}` per thread — the server owns the context window [C1 §4/§"keeper-relevance"]. This is
  why reconnect is cheap (a `thread/resume` + dedup filter), not a buffer replay.
- **Daemon terminal-write discipline.** The `BrUpdate` Action is advisory only; the daemon owns terminal
  bead transitions and the reactor never pre-sets `in_progress` (MEMORY: `daemon_submit_no_inprogress`,
  beads-cli write-discipline).
- **Drift = one-line edit** everywhere: T2 method registry, T3 EventKind, T3 Action enum — each is a
  closed table so protocol evolution is a localized change [PLAN §2].

---

## 6. Deferred-to-build index (corpus-dependent decisions, consolidated)

1. Item sub-notification names beyond the happy set (§1.1) — corpus [SKEL #1].
2. Whether a stable per-item id and/or per-notification seq is on the wire (§1.4) — corpus [SKEL #1/#3].
3. Compaction trigger policy: automatic vs caller-triggered `thread/compact/start` (§1.6) — C1 open-q #1
   [C1 §4].
4. All payload structs + required-vs-optional field tightening (§4.3) — corpus [SKEL #1/#2].
5. JSON-RPC `id` concrete type (§4.3) — corpus [SKEL #3].
6. token-usage / `turn/completed` field names (§4.3) — corpus [SKEL #4; C1 open-q #4].
7. Semantic-equal number/whitespace normalization (§4.3) — corpus [SKEL #6].
8. Non-stdio transports for the twin (§3) — out of Phase-1 scope [C1 §1].

Everything else in §§1–4 is protocol-shape and is designed to build against now.
