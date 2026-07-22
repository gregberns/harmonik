# Giant retirement — `internal/daemon/router` extraction from `handleSocketConn`: design **v2 (final)**

> Revision pass (2026-07-16). v2 folds **every** fix-list item from both adversarial reviews
> (`socket-router-REVIEW-scope.md`, `socket-router-REVIEW-wire.md`, both APPROVE-WITH-CHANGES) into
> a single implementable design. All `file:line` re-verified against **live source** on 2026-07-16.
>
> **Source-location correction (v1 → v2):** v1's prose and the admiral handoff referred to
> `cmd/harmonik/socket.go`. The dispatch table actually lives in **`internal/daemon/socket.go`**
> (`handleSocketConn` at `:387`, the op switch at `:441-807`, both writers at `:837`/`:847`,
> `handleQueueOp` at `:816`). Every line number below is against `internal/daemon/socket.go`.
>
> **Naming note (load-bearing):** prose says "socket-router"; in code the directory/import path is
> `internal/daemon/router` (OG-1 resolved — daemon sub-package) and the package is declared
> `package socketrouter` (Go package names take no hyphen; the package identifier `socketrouter`
> is retained across all code snippets below). The depguard rule key is `socketrouter`. "socket-router",
> `socketrouter`, and `internal/daemon/router` all refer to the same thing.
>
> **✅ Operator gate OG-1 RESOLVED (operator, 2026-07-16): daemon-internal SUB-PACKAGE.** The router is
> the package `socketrouter` living at **`internal/daemon/router`** (a sub-package under `daemon`), NOT
> a top-level `internal/socketrouter` sibling. This matches `subsystem-organization.md`'s Deferred
> section, which anticipates a router as a *sub-package* (verbatim: *"Sub-package structure inside each
> subsystem (e.g. `orchestrator/runner`, `orchestrator/router`)"*). The router's *code* (Result / Kind
> / Classify / HandlerFunc / Router / Dispatch) is unchanged; the sub-package **depguard variant (§4
> Variant B)** is selected and the top-level variant is dropped; only the import path
> (`internal/daemon/router`) and doc-comment are fixed accordingly. **Implementer caveat (import
> cycle):** `internal/daemon/router` must NOT import `daemon` back — the daemon threads handler
> closures IN (mergeq/runexec pattern). If the leaf ever appears to need a `daemon` type, thread a
> small shared primitive type instead of promoting to a top-level package; the `$gostd`-only edge
> already keeps this one-way.

---

## Review resolution — scope review (`socket-router-REVIEW-scope.md`)

| Finding | Severity | v2 resolution |
|---|---|---|
| **F1** top-level subsystem is the least-justified of the M5 series; design assumes it | ALTITUDE / should-fix | **Surfaced as operator gate OG-1, now RESOLVED (operator, 2026-07-16): daemon sub-package `internal/daemon/router`** (§4 Variant B selected; top-level variant dropped). §9 records the ruling; §0 names the router a "pure routing leaf" placed as a daemon sub-package — not a top-level subsystem. |
| **F2** SR-2 rewires all 26 ops in one slice; hides a SR-2/SR-3 router-construction boundary | SCOPE / METHOD / should-fix | **Folded.** §5 splits SR-2 into **SR-2a** (introduce `socketDispatch` holder + adapter methods, *switch still routes to them* — byte-trivial) and **SR-2b** (replace switch body with `Dispatch`). Construction boundary resolved: **SR-2b builds the router once per listener body, before the Accept loop** (where the switch was), NOT per-conn — satisfies risk #7 at every intermediate commit. SR-3 later hoists that single build into `Serve`. |
| **F3** the only real LOC deletion (~60 lines + 12-param chain death) is in the "droppable" SR-3 | VALUE / should-fix | **Folded — SR-3 is now MANDATORY** (§5), not "droppable." §8 states the mandatory-deliverable value plainly. |
| **F4** `HandlerFunc` carrying `net.Conn` for one op keeps the router impure | ALTITUDE / should-fix | **Folded (flipped Q4).** `HandlerFunc` is now **conn-free** `func(ctx, raw) Result`; **subscribe becomes a daemon pre-branch** (symmetric with hook-relay, keyed on `req.Op=="subscribe"`). `Result.Streamed` is **deleted**; `net` **drops off the router edge** (edge = `context`+`encoding/json`+`fmt` only). Router registers **25** ops; subscribe + hook-relay are the two daemon pre-branches. |
| **F5** "daemon:" vocabulary leaks into the leaf package | nit / consider | **Folded (adopted the reviewer's stronger option).** `Dispatch` no longer builds the `"daemon: unknown op"` string; it returns a neutral `Result{Unknown:true}`. The daemon's `resultToResponse` constructs the exact `daemon: unknown op %q` wire string. ALL wire vocabulary now lives daemon-side; the leaf returns a neutral not-found signal. |
| **F6** byte-identity handling + method-fit are strong | positive | No change. The §1 verbatim enumeration + newline callout are retained verbatim. |
| **Q6** (missing question, = F1) | — | = OG-1, operator gate. RESOLVED (operator, 2026-07-16): daemon sub-package `internal/daemon/router`. |
| **Q7** (missing question, = F2) | — | Folded via F2 above (SR-2a/2b split + construction boundary). |

## Review resolution — wire review (`socket-router-REVIEW-wire.md`)

| Finding | Severity | v2 resolution |
|---|---|---|
| **F1** the named "primary guard" (existing round-trip suites) is **byte-blind**; byte-identity is unproven | **CRITICAL** | **Folded — the load-bearing fix.** §6 demotes the round-trip suites to a **decoded-semantics regression guard** (they `json.Decode` into a struct → order-, newline-, and `null`-blind). **T5 is elevated from "recommended" to MANDATORY** and is the **sole byte-identity proof**. |
| **F2** `resultToResponse` is the only real drift surface; T5 didn't cover success/no-result | **HIGH** | **Folded — the load-bearing fix.** T5 is **expanded** to cover the **success envelope** and the **no-result envelope** with real stub handlers: (a) `operator-pause` success `{"ok":true}` (no `result`, no newline); (b) `emit-outcome` success carrying a `result`; (c) a queue-op success (`error_code` **absent**). See §6 T5 spec. |
| **F3** newline asymmetry is real, correctly located, but unguarded; initial raw-decode failure uses the hook-relay writer even for an op request | MEDIUM | **Folded.** `decodeRawMap`'s bad-envelope path **MUST call `writeHookRelayAck`** (NDJSON+`\n`), per §1a/§3. T5 asserts **both** the trailing `\n` on the `bad_envelope` decode-fail ack **and** its absence on op responses (`unknown op` / `not registered`). |
| **F4** `HandlerFunc` signature (raw-only) contradicts §3 "pass both req+reEncoded" | LOW | **Folded — Q5 resolved decisively.** `HandlerFunc` carries **only `raw json.RawMessage`** (conn-free per scope-F4). **Adapters re-decode `SocketRequest` from `raw`** locally (byte-identical: same `reEncoded` bytes, same `json.RawMessage` extraction, same `nil` on absent key). This also keeps the edge tight — `SocketRequest` stays a daemon type and is **never** named by `socketrouter`. §3 no longer proposes a request-context struct. |
| **F5** `Classify` truth-table omits the `len>2` edge cases the heuristic admits | LOW | **Folded.** T3 gains `{"type":null}` → HookRelay (len 4) and `{"type":"x"}` → HookRelay (len 3), pinning the literal `len(raw["type"]) > 2` so a future heuristic "cleanup" can't silently pass. |
| **F6** SR-3 unification changes per-listener startup error-string prefixes (non-wire) | LOW | **Folded as an implementer note** (§5 SR-3): the three duplicated bodies use distinct prefixes (`RunSocketListenerWithState:` etc.); collapsing to `Serve` unifies them. These are **returned startup errors, not wire bytes**; `errors.Is(err, errLiveDaemon)` survives (%w chain intact); no test string-matches them (grep confirmed). Safe; noted so the diff is expected. |
| **F7** T4 (`Ops()` frozen-set guard) must be **mandatory**, not optional | OK / required | **Folded — T4 is MANDATORY** (§6). Adjusted for scope-F4: the router's frozen set is **25** (subscribe is a pre-branch); T4 asserts `Ops()` == frozen-25 **and** a static assertion that subscribe + hook-relay are the two daemon pre-branches (25 + 1 = the full 26-op surface, no op silently dropped). |
| **F8** subscribe streaming + daemon-side uuid validation | OK / verified | Preserved and **strengthened by scope-F4**: subscribe is now a daemon pre-branch, so `uuid.Parse` (off the `$gostd` edge) and the no-write stream naturally stay daemon-side. `subscribe_test.go` guards the no-write path. |
| **F9** type-assertion ops preserve comma-ok + exact strings | OK / verified | Preserved. Adapters lift the `case` bodies verbatim; `errcheck check-type-assertions:true` (`.golangci.yml:84`) keeps the comma-ok form. |

---

## Mandatory test list (the byte-identity contract) — read this first

Per wire-F1/F2/F7, **T4 and T5 are MANDATORY**, not optional. The existing round-trip suites are a
**regression guard for decoded semantics only** — they `json.Decode` into a `SocketResponse` struct
and are therefore blind to field order, trailing newlines, and `"result":null` vs an omitted
`result`. They do **not** prove wire-byte identity. T5 is the sole byte-identity proof.

- **T4 — wiring-completeness guard (MANDATORY, `package daemon`).** Assert
  `buildSocketRouter(&socketDispatch{}).Ops()` equals the **frozen 25-op set** (all ops except
  `subscribe`, which is a daemon pre-branch). Plus a static assertion enumerating the two daemon
  pre-branches (`subscribe`, hook-relay) so the total handled surface is provably the full 26 ops +
  the hook-relay envelope. Catches the one silent regression the carve can introduce: a dropped
  `Register` routes a live op to the neutral `Unknown` path → `daemon: unknown op %q` instead of its
  `… not registered` envelope. (wire-F7 verified this failure mode is real and asymmetric.)

- **T5 — golden wire-byte table (MANDATORY, `package daemon`, over `net.Pipe`, no daemon boot).**
  Drives `handleSocketConn` and asserts the **exact JSON bytes** (and the presence/absence of the
  trailing `\n`) of every distinct envelope shape. Rows:

  | Row | Driver | Exact expected bytes | Newline |
  |---|---|---|---|
  | error: nil comms-presence | all-nil `socketDispatch` | `{"ok":false,"error":"daemon: CommsPresenceHandler not registered"}` | **no** `\n` |
  | error: nil queue-submit | all-nil | `{"ok":false,"error":"daemon: QueueHandler not registered","error_code":-32099}` | **no** `\n` |
  | error: unknown op | all-nil | `{"ok":false,"error":"daemon: unknown op \"bogus\""}` | **no** `\n` |
  | error: decode-request fail | malformed op JSON | `{"ok":false,"error":"daemon: decode request: <err>"}` | **no** `\n` |
  | **bad_envelope: raw-decode fail** | truncated/garbage bytes | the exact `writeHookRelayAck` ack, e.g. `{"status":"bad_envelope","reason":"decode: <err>"}` | **YES** `\n` |
  | **success: operator-pause (no result)** | stub `oh` returning `nil` | `{"ok":true}` | **no** `\n` |
  | **success: emit-outcome (with result)** | stub `h` returning a `result` | `{"ok":true,"result":<bytes>}` | **no** `\n` |
  | **success: queue-op (error_code absent)** | stub `qh` returning `(result, nil)` | `{"ok":true,"result":<bytes>}` (**no** `error_code`) | **no** `\n` |

  The last three rows (per wire-F2) exercise the **only real drift surface** — `resultToResponse` +
  adapter `Result` population — which no existing test covers. They pin the `omitempty` behavior:
  a nil `Result.Payload` MUST yield an omitted `result` (never `"result":null`), and a nil-error
  success MUST NOT emit `error_code`. Field order is frozen by the untouched `SocketResponse` struct
  (`ok`, `result`, `error`, `error_code`); T5's exact-byte assertions catch any reorder.

- **T1/T2/T3 — pure router unit tests (`package socketrouter_test`, SR-1).** T1 route-hit (each
  registered op invokes exactly its fn, `Result` returned untouched); **T2 unknown-op returns the
  neutral `Result{Unknown:true}`** (the *daemon* builds the wire string — that exact-byte assertion
  lives in T5); T3 `Classify` truth-table including the wire-F5 edge cases (`{"type":null}` and
  `{"type":"x"}` → HookRelay; `{"type":""}`, `{"op":…}`, `{}` → Op); `Ops()` sorted.

No live socket, no daemon composition root, no beads required for any tier.

---

## 0. Why this is a routing leaf (not an orchestrator/policy-style domain brain)

Verified in-source and already ruled by the planner (COORD c051, c052): `handleSocketConn` is a
**protocol dispatch table** — `switch req.Op { … }` routing 26 ops to ~10 injected handler
interfaces. It contains **no** work-loop / queue-selection / drain brain (that all lives in the
handlers it calls, harvested into `internal/orchestrator` / `internal/policy`). So the only thing to
carve is the **routing mechanism itself**: envelope classification, op→handler lookup, decode
delegation, and neutral-outcome construction. That is a genuinely pure, table-driven surface.

**Honest altitude (per scope-F1):** this pure surface is the *thinnest* of the M5-family cuts — a
`map[string]HandlerFunc` + a `len(typeRaw)>2` predicate + an ~8-line `Dispatch`. The 26 per-op
bodies (where the logic lives) **stay in daemon** as adapter methods. The scope review is right that
this is a routing *leaf*, not a domain brain. **OG-1 is RESOLVED (operator, 2026-07-16): the leaf is a
daemon sub-package at `internal/daemon/router`** (§4 Variant B), not a top-level `internal/` subsystem.

The honest scope (mirrors c052's candor): this cut mostly **redistributes** complexity out of one
~420-line giant into a pure tested router core + many small daemon adapter methods. The wins are:
(1) `handleSocketConn` drops under every complexity ceiling with **no `//nolint`**; (2) a pure,
unit-testable routing table with a hard `$gostd`-only edge; (3) SR-3 collapses the 12-positional-param
telescoping listener chain and deletes ~60 lines of triplicated Accept-loop.

---

## 1. Full enumeration of the op switch (`socket.go:441-807`)

26 ops + `default`. For each: the handler it routes to, the success envelope, and the exact
error/`bad_envelope` envelopes it can emit. **All strings below are copied verbatim from source and
are the byte-identity contract.** (Unchanged from v1 — scope-F6/wire-F6 confirmed this section is
correct; only the writer/handler line numbers are pinned to `internal/daemon/socket.go`.)

### 1a. Pre-switch envelope discrimination + decode (`socket.go:390-438`)

| Step | Source | Behavior | Envelope on failure |
|---|---|---|---|
| decode raw map | `:392` | `json.Decode` into `map[string]json.RawMessage` | hookRelayAck `{status:"bad_envelope", reason:"decode: <err>"}` (NDJSON **+ `\n`**) |
| classify | `:403` | `type` key present **and** `len(typeRaw) > 2` → hook-relay; else op-request | — |
| hook-relay re-encode | `:406-409` | re-marshal map | hookRelayAck `{bad_envelope, "re-encode failed"}` (**+ `\n`**) |
| hook-relay unmarshal | `:411-415` | into `hookRelayEnvelope` | hookRelayAck `{bad_envelope, "envelope decode: <err>"}` (**+ `\n`**) |
| hr nil-guard | `:416-419` | | hookRelayAck `{bad_envelope, "no hook-relay handler registered"}` (**+ `\n`**) |
| hook-relay dispatch | `:420-421` | `hr.HandleHookRelay(env)` → its own ack | (handler-produced ack, **+ `\n`**) |
| op re-encode | `:426-430` | re-marshal map → `reEncoded` | SocketResponse `{ok:false, error:"re-encode failed"}` (**no `\n`**) |
| op unmarshal | `:431-438` | into `SocketRequest` | SocketResponse `{ok:false, error:"daemon: decode request: <err>"}` (**no `\n`**) |

**Load-bearing byte asymmetry (wire-F3):** hook-relay acks are written by `writeHookRelayAck`
(`:837`, `append(data, '\n')` at `:842`) as JSON **+ trailing `'\n'`** (NDJSON). Op responses are
written by `writeSocketResponse` (`:847`, plain `conn.Write(data)` at `:854`) as JSON **with no
trailing newline**. The **initial raw-decode failure (`:392-400`) uses the hook-relay writer** — so
`decodeRawMap`'s bad-envelope path MUST call `writeHookRelayAck`. This asymmetry MUST survive the
carve; T5 asserts both directions. Both writers touch `net.Conn`; both **stay daemon-side**.

### 1b. The 26 ops

| # | op | handler call | success | nil/assert-fail envelope | notes |
|---|---|---|---|---|---|
| 1 | `emit-outcome` | `h.EmitOutcome(ctx, OutcomeRequest{...})` | `{ok:true, result}` | (h never nil; noop stub) | plain err → `{ok:false, error: err.Error()}` |
| 2 | `claim-next` | `h.ClaimNext(ctx, req.Role)` | `{ok:true, result}` | — | as above |
| 3 | `queue-submit` | `handleQueueOp(...HandleQueueSubmit)` | `{ok:true, result}` | `{ok:false, error:"daemon: QueueHandler not registered", error_code:-32099}` | rpcErr → `{error:rpcErr.Message, error_code:rpcErr.Code}` |
| 4 | `queue-append` | `…HandleQueueAppend` | ″ | ″ | ″ |
| 5 | `queue-status` | `…HandleQueueStatus` | ″ | ″ | ″ |
| 6 | `queue-dry-run` | `…HandleQueueDryRun` | ″ | ″ | ″ |
| 7 | `queue-list` | `…HandleQueueList(ctx)` | ″ | ″ | no params |
| 8 | `queue-set-concurrency` | `…HandleQueueSetConcurrency` | ″ | ″ | ″ |
| 9 | `queue-cancel` | `…HandleQueueCancel` | ″ | ″ | ″ |
| 10 | `worker-set-enabled` | `…HandleWorkerSetEnabled` | ″ | ″ | routed through QueueHandler |
| 11 | `subscribe` | `sub.HandleSubscribe(ctx, conn, subReq)` | **no response** (`return`, `:545`) | `{ok:false, error:"daemon: SubscribeHandler not registered"}` | **v2: daemon pre-branch, not a router route** (scope-F4). decode-fail → `"daemon: decode subscribe request: <err>"`; bad uuid → `"daemon: since_event_id %q is not a valid UUID: %v"` (`uuid.Parse`, `:539`, off the `$gostd` edge) |
| 12 | `comms-send` | `ch.HandleCommsSend(ctx, req.Payload)` | `{ok:true, result}` | `{ok:false, error:"daemon: CommsSendHandler not registered"}` | err → `err.Error()` |
| 13 | `comms-presence` | `ch.(CommsPresenceHandler).HandleCommsPresence` | ″ | `"daemon: CommsPresenceHandler not registered"` | comma-ok assert on `ch` (`:566`) |
| 14 | `comms-recv` | `ch.(CommsRecvHandler).HandleCommsRecv` | ″ | `"daemon: CommsRecvHandler not registered"` | comma-ok (`:582`) |
| 15 | `decisions-raise` | `ch.(DecisionsHandler).HandleDecisionsRaise` | ″ | `"daemon: DecisionsHandler not registered"` | comma-ok (`:603`) |
| 16 | `decisions-withdraw` | `…HandleDecisionsWithdraw` | ″ | ″ | comma-ok (`:616`) |
| 17 | `decisions-list` | `…HandleDecisionsList` | ″ | ″ | comma-ok (`:636`) |
| 18 | `decisions-answer` | `…HandleDecisionsAnswer` | ″ | ″ | comma-ok (`:649`) |
| 19 | `operator-pause` | `oh.HandleOperatorPause(ctx, req.Queue)` | `{ok:true}` (**no result**) | `"daemon: OperatorControlHandler not registered"` | err → `"daemon: operator-pause: %v"` |
| 20 | `operator-resume` | `oh.HandleOperatorResume(ctx, req.Queue)` | `{ok:true}` (**no result**) | ″ | err → `"daemon: operator-resume: %v"` |
| 21 | `crew-start` | `crewh.HandleCrewStart(ctx, req.Payload)` | `{ok:true, result}` | `"daemon: CrewHandler not registered"` | err → `"daemon: crew-start: %v"` |
| 22 | `crew-stop` | `crewh.HandleCrewStop(ctx, req.Payload)` | ″ | ″ | err → `"daemon: crew-stop: %v"` |
| 23 | `daemon-sleep` | `sleepWakeh.HandleDaemonSleep(ctx, force)` | `{ok:true}` (**no result**) | `"daemon: QuiesceOverrideHandler not registered"` | decodes `{force bool}` from Payload; decode-fail → `"daemon: daemon-sleep: decode payload: %v"` |
| 24 | `daemon-wake` | `sleepWakeh.HandleDaemonWake(ctx, agent, all)` | `{ok:true}` (**no result**) | ″ | decodes `{agent,all}`; decode-fail → `"daemon: daemon-wake: decode payload: %v"` |
| 25 | `state` | `stateh.HandleState(ctx)` | `{ok:true, result}` | `"daemon: StateHandler not registered"` | err → `"daemon: state: %v"` |
| 26 | `dashboard` | `dashh.HandleDashboard(ctx)` | `{ok:true, result}` | `"daemon: DashboardHandler not registered"` | err → `"daemon: dashboard: %v"` |
| — | `default` | — | — | `{ok:false, error:"daemon: unknown op %q"}` (`:800`) | **v2: built daemon-side from the router's neutral `Unknown` flag** (scope-F5) |

**The four no-result success ops (19,20,23,24)** emit `SocketResponse{Ok:true}` → `result` **omitted**
via `omitempty`. This is the exact `resultToResponse` trap wire-F2 flagged: an adapter that sets a
non-nil empty/`"null"` `Result.Payload` would render `"result":null` — a wire change. T5's
`operator-pause` success row pins `{"ok":true}` with no `result`.

Four distinct success/error shapes recur:
- **A. result-or-error:** `{ok,result}` / `{ok:false,error:err.Error()}` — ops 1,2,12–18.
- **A′. result-or-*prefixed*-error:** error wrapped `"daemon: <op>: %v"` — ops 21,22,25,26 (+19,20 no result).
- **B. queue/RPC:** carries `error_code` (`-32099` on nil, else `rpcErr.Code`) — ops 3–10.
- **C. streamed:** subscribe (op 11) writes nothing; `return`s. **v2: modeled as a daemon pre-branch, not a `Result` variant** (scope-F4 — `Result.Streamed` deleted).

`exhaustive` does **not** fire — `req.Op` is a plain `string`, not a typed enum, and the switch has a
`default`. No new enum is introduced.

---

## 2. The seam — what MOVES vs what STAYS injected

### MOVES into `socketrouter` (pure, `$gostd` only: `context`, `encoding/json`, `fmt`)

Note the edge is **tighter in v2**: `net` is gone (scope-F4 dropped `conn` from `HandlerFunc`).

1. **`Result`** — the neutral outcome of one op handler:
   ```go
   package socketrouter // imports: $gostd ONLY (context, encoding/json, fmt) — no net

   // Result is the neutral outcome of dispatching one op. The daemon maps it to
   // its wire SocketResponse (result-or-error, prefixed-error, or error_code shape).
   type Result struct {
       OK        bool            // ok field
       Payload   json.RawMessage // result on success (nil ⇒ result omitted)
       Err       string          // error message on failure
       ErrorCode int             // JSON-RPC code; 0 = plain error (queue ops only)
       Unknown   bool            // op not registered: daemon builds "daemon: unknown op %q" (scope-F5)
   }
   ```
   `Result.Streamed` from v1 is **removed** — subscribe is now a daemon pre-branch.
2. **`Kind` + `Classify`** — the pure envelope-discrimination predicate (`socket.go:403`):
   ```go
   type Kind int
   const ( KindOp Kind = iota; KindHookRelay )
   // Classify mirrors socket.go:403 exactly: type present && len(raw["type"]) > 2 → HookRelay.
   func Classify(raw map[string]json.RawMessage) Kind
   ```
3. **`HandlerFunc` + `Router`** — the table-driven dispatch engine (**conn-free**, scope-F4/wire-F4):
   ```go
   // HandlerFunc handles one op. raw is the re-encoded request bytes; the adapter
   // re-decodes SocketRequest from raw locally when it needs scalar fields. No net.Conn.
   type HandlerFunc func(ctx context.Context, raw json.RawMessage) Result

   type Router struct { routes map[string]HandlerFunc }
   func New() *Router
   func (r *Router) Register(op string, fn HandlerFunc)   // panic on dup op (init-time)
   func (r *Router) Ops() []string                        // sorted, for the T4 wiring guard
   // Dispatch looks up op; unknown op → Result{Unknown:true} (neutral, no daemon: string).
   func (r *Router) Dispatch(ctx context.Context, op string, raw json.RawMessage) Result
   ```
   `Dispatch`'s unknown-op branch returns `Result{Unknown: true}` — **no wire vocabulary in the leaf**
   (scope-F5). The daemon's `resultToResponse` maps `Unknown` → `SocketResponse{Ok:false,
   Error: fmt.Sprintf("daemon: unknown op %q", op)}`, so the exact bytes are unchanged and asserted
   by T5, not by a pure-router test.

### STAYS injected / daemon-side (effectful or daemon-typed)

- **Every handler interface** (`RequestHandler`, `HookRelayHandler`, `QueueHandler`,
  `SubscribeHandler`, `OperatorControlHandler`, `CommsSendHandler`/`…Presence`/`…Recv`,
  `DecisionsHandler`, `CrewHandler`, `QuiesceOverrideHandler`, `StateHandler`, `DashboardHandler`) —
  daemon types, threaded IN as closures (the `mergeq`/`runexec` pattern). `socketrouter` never names
  them.
- **The wire types** `SocketRequest` / `SocketResponse` — **STAY in daemon** (D1 / Q1). Field order
  frozen (`ok`, `result,omitempty`, `error,omitempty`, `error_code,omitempty`).
- **Both writers** `writeHookRelayAck` (NDJSON+`\n`) and `writeSocketResponse` (no newline) — stay
  daemon-side; the router never touches `conn.Write`.
- **The hook-relay path** (`:406-421`) — daemon pre-branch after `Classify`, keyed on the `type`
  field. Daemon-typed / effectful.
- **The subscribe path** (`:519-545`) — **v2: a daemon pre-branch** keyed on `req.Op=="subscribe"`
  (scope-F4). Holds the `uuid.Parse` validation (off the `$gostd` edge), the three error sub-paths
  (which fall through to `writeSocketResponse`), and the no-write stream + `return`. Not a router
  route.
- **`handleQueueOp`** (`:816`) — the `-32099`/rpcErr → response mapper; stays daemon-side, invoked by
  the queue adapter methods.
- **`resultToResponse`** (new, daemon) — maps `socketrouter.Result` → `SocketResponse`, including the
  `Unknown` → `daemon: unknown op %q` construction. **The single real byte-drift surface** (wire-F2);
  T5 pins it.

---

## 3. How the 12-param signature collapses + public API

Two independent collapses, sequenced separately (§5):

### D1 — the dispatch table: a daemon adapter-holder + a `socketrouter.Router`

Daemon-side holder bundling the injected handlers, with one **small named method per op** returning a
`socketrouter.Result`. **Adapters re-decode `SocketRequest` from `raw` when they need scalar fields**
(wire-F4/Q5) — byte-identical to today.

```go
// daemon/socketdispatch.go  (new, daemon package)
type socketDispatch struct {
    h          RequestHandler
    qh         QueueHandler
    oh         OperatorControlHandler
    ch         CommsSendHandler        // comma-ok asserted for presence/recv/decisions
    crewh      CrewHandler
    sleepWakeh QuiesceOverrideHandler
    stateh     StateHandler
    dashh      DashboardHandler
    // NOTE: SubscribeHandler + HookRelayHandler are NOT here — both are daemon
    // pre-branches (scope-F4/Q2), handled before Dispatch.
}

func (d *socketDispatch) emitOutcome(ctx context.Context, raw json.RawMessage) socketrouter.Result {
    var req SocketRequest
    _ = json.Unmarshal(raw, &req)          // byte-identical re-decode from reEncoded bytes
    result, err := d.h.EmitOutcome(ctx, OutcomeRequest{RunID: req.RunID, BeadID: req.BeadID, Outcome: req.Outcome})
    if err != nil { return socketrouter.Result{OK: false, Err: err.Error()} }
    return socketrouter.Result{OK: true, Payload: result}
}
// … 25 methods total (all ops except subscribe), each ~6–12 lines, cyclop 2–3.

func buildSocketRouter(d *socketDispatch) *socketrouter.Router {
    r := socketrouter.New()
    r.Register("emit-outcome", d.emitOutcome)
    r.Register("claim-next",   d.claimNext)
    // … 25 flat Register lines (NO subscribe). cyclop(buildSocketRouter) = 1.
    return r
}
```

`handleSocketConn` collapses to:
```go
func handleSocketConn(ctx context.Context, conn net.Conn, hr HookRelayHandler, sub SubscribeHandler, router *socketrouter.Router) {
    defer conn.Close()
    raw, err := decodeRawMap(conn)            // bad_envelope ack via writeHookRelayAck (+\n) on error
    if err != nil { return }
    if socketrouter.Classify(raw) == socketrouter.KindHookRelay {
        handleHookRelayEnvelope(conn, hr, raw) // daemon pre-branch #1 (unchanged behavior)
        return
    }
    req, reEncoded, ok := decodeSocketRequest(conn, raw) // writeSocketResponse error envelope on failure
    if !ok { return }
    if req.Op == "subscribe" {
        handleSubscribe(ctx, conn, sub, reEncoded)       // daemon pre-branch #2 (uuid validate, stream, return)
        return
    }
    res := router.Dispatch(ctx, req.Op, reEncoded)
    writeSocketResponse(conn, resultToResponse(res, req.Op))
}
```

Signature-wise: `handleSocketConn` goes **12 params → 5** (ctx, conn, hr, sub, router). `hr` and
`sub` stay separate because both are pre-Dispatch branches (hook-relay keyed on `type`; subscribe
keyed on the `subscribe` op and response-shape-breaking) — this is the symmetric, honest model
(scope-F4). Everything else routes through the value-returning `Dispatch`.

### D2 — the telescoping listeners: a `SocketHandlers` struct + one shared Accept loop (SR-3)

Today: 7 `RunSocketListener*` wrappers (`socket.go:278/284/294/323/332`, `socket_state.go:51`,
`socket_dashboard.go:55`) form a telescoping-constructor chain, and **three** of them
(`…WithSleepWake`, `…WithState`, `…WithDashboard`) each duplicate the full
`removeStaleSocket → Listen → Chmod → ctx-close goroutine → Accept loop` body verbatim. The
production composition root is `daemon.go:2047` (`RunSocketListenerWithDashboard`).

Collapse to:
```go
type SocketHandlers struct {
    Request   RequestHandler
    HookRelay HookRelayHandler
    Queue     QueueHandler
    Subscribe SubscribeHandler
    Operator  OperatorControlHandler
    Comms     CommsSendHandler
    Crew      CrewHandler
    SleepWake QuiesceOverrideHandler
    State     StateHandler
    Dashboard DashboardHandler
}

// Serve binds sockPath and accepts until ctx is cancelled. Single shared body.
// Builds the router ONCE (buildSocketRouter) before the Accept loop (risk #7).
func Serve(ctx context.Context, sockPath string, hs SocketHandlers) error
```
`Serve` builds the router once and the shared Accept loop calls
`handleSocketConn(ctx, conn, hs.HookRelay, hs.Subscribe, router)`. The 7 named wrappers are retained
as **thin back-compat adapters** (build a `SocketHandlers`, call `Serve`) so existing test callers
(`socket_test.go`, `socket_queue_test.go`, `subscribe_test.go`, the comms/decisions tests) don't
churn. The composition root (`daemon.go:2047`) switches to `Serve` + struct. This is the death of the
12-positional-param chain.

---

## 4. Depguard edge + proof — **Variant B selected (OG-1 resolved)**

The routing surface touches only `context`, `encoding/json`, `fmt` — **no `net`, no `internal/core`**
(op is a string, payloads are `json.RawMessage`, no `conn`). So the edge is the tightest in the tree.

**OG-1 is RESOLVED (operator, 2026-07-16): daemon sub-package `internal/daemon/router`.** Variant B
below is the selected form; Variant A (top-level `internal/socketrouter` subsystem) is **dropped**.

**Variant B (SELECTED) — daemon sub-package `internal/daemon/router/` with a scoped depguard row.**
The package compiles to the same `$gostd`-only code. To keep the one-way edge machine-enforced (the
§9/header import-cycle caveat), add a **scoped sub-rule** to `.golangci.yml`, sorted near the `daemon`
rules:
```yaml
        # socketrouter: the pure protocol dispatch table (giant-retirement follow-up), a daemon
        # sub-package. Value-in/value-out routing: Classify + Router.Dispatch + neutral Result over
        # json.RawMessage. The daemon threads effectful handler closures IN (mergeq/runexec pattern);
        # the router MUST NOT import daemon back. Tighter than orchestrator/policy — no core, no net
        # (ops are strings, payloads are json.RawMessage, no conn on the edge).
        socketrouter:
          files: ["**/internal/daemon/router/**"]
          allow:
            - "$gostd"
            - "github.com/gregberns/harmonik/internal/daemon/router"   # self-import (external test pattern)
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "router is a pure daemon sub-package; daemon threads handler closures in, never the reverse (import-cycle caveat)" }
```
```
go list -deps ./internal/daemon/router | grep harmonik
# EXPECTED: only github.com/gregberns/harmonik/internal/daemon/router (self). Zero other internal deps.
```
Under this variant the `subsystem-organization.md` "Deferred: sub-package structure" note is satisfied
directly and **no top-level architecture-doc / subsystem-matrix row is added.**

golangci-lint is not installed in this env (same constraint noted in c043/c044/c050/c052); run
`go tool golangci-lint run ./internal/daemon/router/...` on the final merge box to confirm the rule activates.

---

## 5. Ordered, behavior-preserving sub-slices

**Each is independently committable, green-verifiable alone, and gets its own STOP + independent
review** (M5 discipline). All preserve the wire protocol byte-for-byte.

### SR-1 — scaffold the pure package (lowest risk, zero daemon change)
Create the package at **`internal/daemon/router/`** (OG-1 resolved — daemon sub-package) via the
`go-subsystem-add` skill: `doc.go` (purity contract, mirroring `policy/doc.go`), `router.go`
(`Result`, `Kind`, `Classify`, `HandlerFunc`, `Router`, `New`, `Register`, `Ops`, `Dispatch`),
`router_test.go` (external `_test` package). Add the **scoped depguard rule** (§4 Variant B).
**Pure unit tests T1/T2/T3** (no daemon). `go build ./... && go test ./internal/daemon/router/...` green.

### SR-2a — introduce the holder + adapter methods **behind the still-live switch** (scope-F2)
Add `daemon/socketdispatch.go`: the `socketDispatch` holder, 25 adapter methods (each a verbatim lift
of one `case` body → `Result`), `buildSocketRouter`, and `resultToResponse`. **The switch in
`handleSocketConn` is untouched** — it still routes. This commit is a mechanical extraction reviewable
at a glance; nothing calls the new methods on the hot path yet (or the switch cases delegate to them
one-for-one). Byte-trivial. Add **T4** here (it exercises `buildSocketRouter`/`Ops()`).

### SR-2b — replace the switch body with `Dispatch` (the routing-mechanism swap)
Rewrite `handleSocketConn` to decode → `Classify` → hook-relay pre-branch → decode `SocketRequest` →
**subscribe pre-branch** → `Dispatch` → `resultToResponse` → write (§3 D1). **Construction boundary
(scope-F2/Q7):** the router is built **once per listener body, before the Accept loop** (in each of
the three full bodies `RunSocketListenerWithSleepWake`/`WithState`/`WithDashboard`), and passed into
`handleSocketConn` — **never per-conn** (risk #7). Keep `handleQueueOp`, both writers, hook-relay
handling, and subscribe's uuid validation daemon-side. **No wire change.** Add **T5** here (the sole
byte-identity proof). The existing round-trip suites must still pass (decoded-semantics regression
guard). This is where `handleSocketConn` drops under every ceiling. Its own review.

### SR-3 — collapse the telescoping listeners (**MANDATORY**, scope-F3) — widest touch, lowest logic risk
Add `SocketHandlers` + `Serve` (§3 D2); dedupe the three duplicated Accept-loop bodies into `Serve`
(hoisting the single router build there); convert the 7 `RunSocketListener*` wrappers to thin
`Serve`-delegating adapters; point `daemon.go:2047` at `Serve`. Pure refactor — no envelope change.
**Implementer note (wire-F6):** the three bodies' distinct startup error prefixes
(`RunSocketListenerWithState:` etc.) unify under `Serve`; these are returned startup errors (not wire
bytes), `errors.Is(err, errLiveDaemon)` survives, and no test string-matches them — safe to change,
expect the diff. **SR-3 is no longer droppable** — it is the only slice that deletes real LOC (~60
triplicated lines) and kills the 12-param chain, which is the honest payoff (scope-F3). Its own review.

Order rationale: SR-1 stands up the package + edge at ~zero blast radius; SR-2a is a trivially-safe
extraction; SR-2b is the load-bearing behavior-preserving swap; SR-3 is cosmetic-but-wide and best
isolated for review.

---

## 6. Test strategy — proving wire-byte-identical routing without the live daemon

Three tiers. **T4 and T5 are MANDATORY** (wire-F1/F2/F7). See the "Mandatory test list" section at
the top for the full T4/T5 spec; recap:

**Tier 1 — pure router unit tests (`package socketrouter_test`, SR-1).**
- **T1 route-hit**, **T2 unknown-op** → `Result{Unknown:true}` (neutral; the exact `daemon: unknown
  op` bytes are asserted in T5, daemon-side, per scope-F5), **T3 Classify truth-table** including the
  wire-F5 edge cases (`{"type":null}`, `{"type":"x"}` → HookRelay).

**Tier 2 — decoded-semantics regression guard (existing suites, unchanged).**
The existing round-trip suites (`socket_test.go`, `socket_queue_test.go`, `subscribe_test.go`,
`socket_operatorpause_ry8q1_test.go`, `socket_comms_nbrmf_test.go`, `socket_commspresence_7t27s_test.go`,
`socket_decisions_xz9_test.go`) dial a real unix socket and `json.Decode` the response into a
`SocketResponse` struct. **They are byte-BLIND** (order-, newline-, and `null`-insensitive) — per
wire-F1 they prove *decoded-field* identity, **not wire-byte identity**. They MUST still pass (they
catch semantic regressions), but they are **not** the byte-identity proof.

**Tier 3 — byte-identity proof (MANDATORY, no daemon boot).**
- **T4 — wiring-completeness guard (MANDATORY):** `buildSocketRouter(&socketDispatch{}).Ops()` ==
  frozen **25-op** set + static assertion that `subscribe` and hook-relay are the two daemon
  pre-branches (total surface = 26 ops + hook-relay). Catches a dropped `Register` (→ neutral
  `Unknown` → `daemon: unknown op` instead of `… not registered`).
- **T5 — golden wire-byte table (MANDATORY, over `net.Pipe`):** the 8-row table in the "Mandatory
  test list" section — asserts **exact bytes + newline presence/absence** for every envelope shape,
  including the three success/no-result/`error_code`-absent rows (wire-F2) that exercise
  `resultToResponse`, and both newline directions (wire-F3). This is the sole byte-identity proof.

No live socket, no daemon composition root, no beads required for any tier.

---

## 7. Target ceiling + honest expected numbers

Ceilings (`.golangci.yml:88-96`): `funlen` 100 lines / 60 statements · `cyclop` 15 · `gocognit` 20.
golangci-lint is not installed here; estimates below are from the source structure (26-case switch,
~40 `if`s, ~420-line body).

| Symbol | before | after | under ceiling, no `//nolint`? |
|---|---|---|---|
| `handleSocketConn` | ~420 lines / gocognit ≈ 60 / cyclop ≈ 45 (grandfathered) | ~35 lines (incl. 2 pre-branches) / gocognit <10 / cyclop <8 | **yes** |
| each op adapter method (×25) | — | ~6–12 lines / cyclop 2–3 | yes |
| `buildSocketRouter` | — | ~30 lines / 25 statements / cyclop 1 | yes (25 < 60 stmt) |
| `socketrouter.Dispatch` | — | ~8 lines / cyclop 2 | yes |
| `resultToResponse` (new, daemon) | — | ~15 lines / cyclop ~5 | yes |
| `Serve` (SR-3) | (3× duplicated ~30-line loops) | one ~35-line body | yes; deletes ~60 duplicated lines |

---

## 8. Mandatory-deliverable value proposition (scope-F3, stated plainly)

With **SR-3 now mandatory**, the deliverable is honest end-to-end:
1. `handleSocketConn` retired as a grandfathered giant — ~420 → ~35 lines, under every ceiling, **no
   suppression**.
2. A pure, unit-tested `Router` / `Classify` with the **tightest edge in the tree** (`$gostd` only,
   no `net`, no `core`).
3. **SR-3 deletes ~60 lines** of triplicated Accept-loop boilerplate and **kills the 12-positional-param
   telescoping chain** (`daemon.go:2047`'s 11-arg call collapses to a struct). This is the smell
   everyone can see; it is now in-scope, not droppable.

**Honest caveat (mirrors c052):** total daemon LOC is roughly flat across SR-2a/2b — the switch's
~360 lines become 25 methods + a builder + `resultToResponse` of comparable size. The net line
reduction comes from SR-3. Do not promise a large net reduction from the router extraction alone.
**Altitude caveat (scope-F1):** this is the lightest-weight of the M5-family cuts; OG-1 (§9) is
RESOLVED — the leaf lands as a daemon sub-package (`internal/daemon/router`), not a top-level package —
and the value above holds.

---

## 9. Risks + load-bearing invariants

1. **Wire byte-identity (THE invariant).** Every §1 string, the `ok`/`result`/`error`/`error_code`
   field set, and the JSON field order must not change. Mitigation: adapters lift `case` bodies
   verbatim; `SocketResponse` struct (hence field order) untouched, kept in daemon; both writers stay
   daemon-side; `resultToResponse` is the one new drift surface and **T5 pins it byte-for-byte**.
   **`SocketResponse` field order is frozen** (`ok`, `result`, `error`, `error_code`) — do not reorder.
2. **Newline asymmetry (wire-F3).** hook-relay ack = JSON + `\n` (`writeHookRelayAck`); op response =
   JSON, no newline (`writeSocketResponse`). The **initial raw-decode failure uses the hook-relay
   writer** — `decodeRawMap`'s bad-envelope path MUST call `writeHookRelayAck`. Both stay daemon-side.
   T5 asserts both directions.
3. **subscribe streaming (v2: daemon pre-branch, scope-F4).** Op 11 writes nothing and `return`s; its
   three error sub-paths fall through to `writeSocketResponse`; its uuid validation stays daemon-side
   (`uuid.Parse` off the `$gostd` edge). Modeled as a pre-branch, **not** a `Result` variant
   (`Result.Streamed` deleted). Guarded by `subscribe_test.go` + the pre-branch's own no-write path.
4. **Type-assertion ops (13–18).** comma-ok `ch.(Iface)` + nil-guard + exact "not registered"
   strings must survive. `errcheck check-type-assertions:true` (`.golangci.yml:84`) forces comma-ok;
   verbatim `case`-body lift preserves it (`:566/582/603/616/636/649`).
5. **Wiring completeness (handler injection).** A dropped `Register` degrades a real op to the neutral
   `Unknown` path → `daemon: unknown op` (wrong envelope). Guarded by **T4** (`Ops()` == frozen-25 +
   the two-pre-branch assertion). The single most likely carve bug.
6. **Router purity (v2).** `HandlerFunc` is conn-free; the leaf's edge is `context`+`encoding/json`+
   `fmt` only. `net` and `internal/core` are both off the edge. Wire vocabulary (`daemon:` strings)
   lives entirely daemon-side (scope-F5).
7. **Router lifetime.** Build the router **once per listener body / per `Serve`** (handlers are fixed
   for the listener's life), never per-conn. SR-2b builds it before each Accept loop; SR-3 hoists to
   `Serve`.
8. **`hr` and `sub` stay separate params.** Both are pre-Dispatch branches (hook-relay keyed on
   `type`; subscribe on the `subscribe` op). Symmetric, honest model.

### Resolved design questions (v1 open questions, now decided)
- **Q1 (wire types):** **keep `SocketRequest`/`SocketResponse` in daemon** + neutral `Result`.
  (Both reviews agree; minimal blast radius; keeps the leaf edge daemon-free.)
- **Q2 (hook-relay):** **daemon pre-branch.** (Both reviews agree; NDJSON ack keyed on envelope shape.)
- **Q3 (SR-3 scope):** **include, MANDATORY** (scope-F3). Sequenced last, not droppable.
- **Q4 (`HandlerFunc` shape):** **conn-free + subscribe-as-pre-branch** (scope-F4 flip). `Result.Streamed`
  removed; edge sheds `net`.
- **Q5 (request threading):** **adapters re-decode `SocketRequest` from `raw`** (wire-F4). Byte-identical;
  no request-context struct; keeps the leaf edge free of the daemon `SocketRequest` type.

### ✅ Operator gate OG-1 — RESOLVED (operator, 2026-07-16)
- **OG-1 (package layout, = scope-F1 / v1-Q6): RESOLVED — daemon sub-package `internal/daemon/router`**
  (§4 Variant B selected; top-level `internal/socketrouter` subsystem variant dropped).
  `subsystem-organization.md`'s Deferred section anticipates a router as a *sub-package*, and the
  router's content is a domain-free routing structure, the thinnest of the M5-family cuts — so it lands
  as a daemon sub-package with a scoped depguard row, not a top-level sibling. **SR-1 is UNBLOCKED:**
  scaffold at `internal/daemon/router/` with the §4 Variant B depguard rule. **Implementer caveat
  (import cycle):** `internal/daemon/router` must NOT import `daemon` back (daemon threads handler
  closures in); if a `daemon` type seems needed, thread a small shared primitive type rather than
  promoting to a top-level package — the `$gostd`-only edge already keeps this one-way.
