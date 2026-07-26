# Giant retirement — `internal/socketrouter` extraction from `handleSocketConn`: design

> Read-only design pass (Plan agent, 2026-07-16). Target: the SECOND named "giant retirement"
> de-scoped from M5 (COORD c051/c052). Companion to the `boot-config` cut (first giant,
> `startWithHooks`). This carve extracts the op→handler **dispatch table** in
> `handleSocketConn` (`internal/daemon/socket.go:387`) into a new pure `internal/socketrouter`
> subsystem, leaving every effectful handler injected. **Wire protocol MUST stay byte-identical.**
>
> Method mirrors M5 (c043/c050/c052): design pass → adversarial seam review → worktree
> implementer → cherry-pick `-x` → re-verify green → independent agent-reviewer → trailer-stamp.
> Every `file:line` below verified against live source on 2026-07-16.
>
> **Naming note (load-bearing):** prose says "socket-router"; in code the directory is
> `internal/socketrouter` and the package is `socketrouter` (Go package names take no hyphen).
> The depguard rule key is `socketrouter`. "socket-router" and `socketrouter` are the same thing.

---

## 0. Why this needs its own subsystem (not an orchestrator/policy cut)

Verified in-source and already ruled by the planner (COORD c051, c052): `handleSocketConn` is a
**protocol dispatch table** — `switch req.Op { … }` routing 26 ops to ~10 injected handler
interfaces. It contains **no** work-loop / queue-selection / drain brain (that all lives in the
handlers it calls, and was already harvested into `internal/orchestrator` / `internal/policy`).
So the only thing to carve here is the **routing mechanism itself**: envelope classification,
op→handler lookup, decode/validate, and response-envelope construction. That is a genuinely
pure, table-driven surface — but it is a *new* concern, orthogonal to the three M5 packages,
hence its own leaf subsystem.

The honest scope (mirrors c052's candor): this cut mostly **redistributes** complexity out of one
420-line giant into a pure tested router core + many small daemon adapter methods. It does not
delete large swaths of code. The wins are: (1) `handleSocketConn` drops under every complexity
ceiling with **no `//nolint`**; (2) a pure, unit-testable routing table with a hard `$gostd`-only
edge; (3) the 13-positional-param telescoping listener chain collapses to a struct.

---

## 1. Full enumeration of the op switch (`socket.go:441-804`)

26 ops + `default`. For each: the handler it routes to, the success envelope, and the exact
error/`bad_envelope` envelopes it can emit. **All strings below are copied verbatim from source
and are the byte-identity contract.**

### 1a. Pre-switch envelope discrimination + decode (`socket.go:390-438`)

| Step | Source | Behavior | Envelope on failure |
|---|---|---|---|
| decode raw map | `:392` | `json.Decode` into `map[string]json.RawMessage` | hookRelayAck `{status:"bad_envelope", reason:"decode: <err>"}` (NDJSON + `\n`) |
| classify | `:403` | `type` key present **and** `len(typeRaw) > 2` → hook-relay; else op-request | — |
| hook-relay re-encode | `:406-409` | re-marshal map | hookRelayAck `{bad_envelope, "re-encode failed"}` |
| hook-relay unmarshal | `:411-415` | into `hookRelayEnvelope` (= `hook.RelayEnvelope`) | hookRelayAck `{bad_envelope, "envelope decode: <err>"}` |
| hr nil-guard | `:416-419` | | hookRelayAck `{bad_envelope, "no hook-relay handler registered"}` |
| hook-relay dispatch | `:420-421` | `hr.HandleHookRelay(env)` → its own ack | (handler-produced ack) |
| op re-encode | `:426-430` | re-marshal map → `reEncoded` | SocketResponse `{ok:false, error:"re-encode failed"}` |
| op unmarshal | `:431-438` | into `SocketRequest` | SocketResponse `{ok:false, error:"daemon: decode request: <err>"}` |

**Load-bearing byte asymmetry:** hook-relay acks are written by `writeHookRelayAck`
(`:837`) as JSON **+ trailing `'\n'`** (NDJSON). Op responses are written by
`writeSocketResponse` (`:847`) as JSON **with no trailing newline**. This asymmetry MUST survive
the carve. Both writers touch `net.Conn`; both **stay daemon-side**.

### 1b. The 26 ops

| # | op | handler call | success | nil/assert-fail envelope | notes |
|---|---|---|---|---|---|
| 1 | `emit-outcome` | `h.EmitOutcome(ctx, OutcomeRequest{...})` | `{ok:true, result}` | (h never nil; noop stub) | plain err → `{ok:false, error: err.Error()}` |
| 2 | `claim-next` | `h.ClaimNext(ctx, req.Role)` | `{ok:true, result}` | — | as above |
| 3 | `queue-submit` | `handleQueueOp(...HandleQueueSubmit)` | `{ok:true, result}` | `{ok:false, error_code:-32099, error:"daemon: QueueHandler not registered"}` | rpcErr → `{error_code:rpcErr.Code, error:rpcErr.Message}` |
| 4 | `queue-append` | `…HandleQueueAppend` | ″ | ″ | ″ |
| 5 | `queue-status` | `…HandleQueueStatus` | ″ | ″ | ″ |
| 6 | `queue-dry-run` | `…HandleQueueDryRun` | ″ | ″ | ″ |
| 7 | `queue-list` | `…HandleQueueList(ctx)` | ″ | ″ | no params |
| 8 | `queue-set-concurrency` | `…HandleQueueSetConcurrency` | ″ | ″ | ″ |
| 9 | `queue-cancel` | `…HandleQueueCancel` | ″ | ″ | ″ |
| 10 | `worker-set-enabled` | `…HandleWorkerSetEnabled` | ″ | ″ | routed through QueueHandler |
| 11 | `subscribe` | `sub.HandleSubscribe(ctx, conn, subReq)` | **no response** (`return`, `:545`) | `{ok:false, error:"daemon: SubscribeHandler not registered"}` | decode-fail → `"daemon: decode subscribe request: <err>"`; bad uuid → `"daemon: since_event_id %q is not a valid UUID: %v"` (`uuid.Parse`, `:539`) |
| 12 | `comms-send` | `ch.HandleCommsSend(ctx, req.Payload)` | `{ok:true, result}` | `{ok:false, error:"daemon: CommsSendHandler not registered"}` | err → `err.Error()` |
| 13 | `comms-presence` | `ch.(CommsPresenceHandler).HandleCommsPresence` | ″ | `"daemon: CommsPresenceHandler not registered"` | type-assert on `ch` |
| 14 | `comms-recv` | `ch.(CommsRecvHandler).HandleCommsRecv` | ″ | `"daemon: CommsRecvHandler not registered"` | type-assert |
| 15 | `decisions-raise` | `ch.(DecisionsHandler).HandleDecisionsRaise` | ″ | `"daemon: DecisionsHandler not registered"` | type-assert |
| 16 | `decisions-withdraw` | `…HandleDecisionsWithdraw` | ″ | ″ | type-assert |
| 17 | `decisions-list` | `…HandleDecisionsList` | ″ | ″ | type-assert |
| 18 | `decisions-answer` | `…HandleDecisionsAnswer` | ″ | ″ | type-assert |
| 19 | `operator-pause` | `oh.HandleOperatorPause(ctx, req.Queue)` | `{ok:true}` (no result) | `"daemon: OperatorControlHandler not registered"` | err → `"daemon: operator-pause: %v"` |
| 20 | `operator-resume` | `oh.HandleOperatorResume(ctx, req.Queue)` | `{ok:true}` | ″ | err → `"daemon: operator-resume: %v"` |
| 21 | `crew-start` | `crewh.HandleCrewStart(ctx, req.Payload)` | `{ok:true, result}` | `"daemon: CrewHandler not registered"` | err → `"daemon: crew-start: %v"` |
| 22 | `crew-stop` | `crewh.HandleCrewStop(ctx, req.Payload)` | ″ | ″ | err → `"daemon: crew-stop: %v"` |
| 23 | `daemon-sleep` | `sleepWakeh.HandleDaemonSleep(ctx, force)` | `{ok:true}` | `"daemon: QuiesceOverrideHandler not registered"` | decodes `{force bool}` from Payload; decode-fail → `"daemon: daemon-sleep: decode payload: %v"` |
| 24 | `daemon-wake` | `sleepWakeh.HandleDaemonWake(ctx, agent, all)` | `{ok:true}` | ″ | decodes `{agent,all}`; decode-fail → `"daemon: daemon-wake: decode payload: %v"` |
| 25 | `state` | `stateh.HandleState(ctx)` | `{ok:true, result}` | `"daemon: StateHandler not registered"` | err → `"daemon: state: %v"` |
| 26 | `dashboard` | `dashh.HandleDashboard(ctx)` | `{ok:true, result}` | `"daemon: DashboardHandler not registered"` | err → `"daemon: dashboard: %v"` |
| — | `default` | — | — | `{ok:false, error:"daemon: unknown op %q"}` (`:800`) | |

Four distinct success/error shapes recur:
- **A. result-or-error:** `{ok,result}` / `{ok:false,error:err.Error()}` — ops 1,2,12–18.
- **A′. result-or-*prefixed*-error:** same, but error wrapped `"daemon: <op>: %v"` — ops 21,22,25,26 (+19,20 which have no result).
- **B. queue/RPC:** carries `error_code` (`-32099` on nil, else `rpcErr.Code`) — ops 3–10.
- **C. streamed:** subscribe (op 11) writes nothing; `return`s.

`exhaustive` linter does **not** fire here — `req.Op` is a plain `string`, not a typed enum, and the
switch has a `default`. No new enum is introduced.

---

## 2. The seam — what MOVES vs what STAYS injected

### MOVES into `internal/socketrouter` (pure, `$gostd` only)

1. **`Result`** — the neutral outcome of one op handler (replaces the four ad-hoc shapes):
   ```go
   package socketrouter // imports: $gostd ONLY (context, encoding/json, fmt, net)

   // Result is the neutral outcome of dispatching one op. The daemon maps it to
   // its wire SocketResponse (result-or-error, prefixed-error, or error_code shape).
   type Result struct {
       OK        bool            // ok field
       Payload   json.RawMessage // result on success
       Err       string          // error message on failure
       ErrorCode int             // JSON-RPC code; 0 = plain error (queue ops only)
       Streamed  bool            // subscribe: response already written to conn; suppress envelope
   }
   ```
2. **`Kind` + `Classify`** — the pure envelope-discrimination predicate (`socket.go:403`):
   ```go
   type Kind int
   const ( KindOp Kind = iota; KindHookRelay )
   // Classify reports whether a decoded message is a hook-relay envelope (non-empty
   // "type" field) or an op request. Mirrors socket.go:403 exactly: type present && len>2.
   func Classify(raw map[string]json.RawMessage) Kind
   ```
3. **`HandlerFunc` + `Router`** — the table-driven dispatch engine:
   ```go
   // HandlerFunc handles one op. conn is supplied for streaming ops (subscribe);
   // most handlers ignore it. raw is the re-encoded request bytes.
   type HandlerFunc func(ctx context.Context, conn net.Conn, raw json.RawMessage) Result

   type Router struct { routes map[string]HandlerFunc }
   func New() *Router
   func (r *Router) Register(op string, fn HandlerFunc)   // panic on dup op (init-time)
   func (r *Router) Ops() []string                        // sorted, for the wiring-completeness guard
   // Dispatch looks up op and invokes it; unknown op → the exact default envelope.
   func (r *Router) Dispatch(ctx context.Context, conn net.Conn, op string, raw json.RawMessage) Result
   ```
   `Dispatch`'s unknown-op branch returns `Result{OK:false, Err: fmt.Sprintf("daemon: unknown op %q", op)}`
   — the `"daemon:"` prefix is a plain literal, so byte-identity holds even though the string now
   lives in `socketrouter`.

### STAYS injected / daemon-side (effectful or daemon-typed)

- **Every handler interface** (`RequestHandler`, `HookRelayHandler`, `QueueHandler`,
  `SubscribeHandler`, `OperatorControlHandler`, `CommsSendHandler`/`…Presence`/`…Recv`,
  `DecisionsHandler`, `CrewHandler`, `QuiesceOverrideHandler`, `StateHandler`,
  `DashboardHandler`) — these are daemon types; they are threaded IN as closures, exactly like
  `mergeq`/`runexec` thread critical funcs in. `socketrouter` never names them.
- **The wire types** `SocketRequest` / `SocketResponse` — **STAY in daemon** (see §3 decision D1).
- **Both writers** `writeHookRelayAck` (NDJSON+`\n`) and `writeSocketResponse` (no newline) —
  stay daemon-side; the router never touches `conn.Write`.
- **The hook-relay path** (`:406-421`) — keyed on the `type` field, not on an op; stays a daemon
  pre-branch after `Classify`. It re-marshals + unmarshals into `hook.RelayEnvelope`, nil-guards
  `hr`, and calls `hr.HandleHookRelay`. All daemon-typed / effectful.
- **`handleQueueOp`** (`:816`) — the `-32099`/rpcErr → response mapper; stays daemon-side and is
  invoked by the queue adapter methods (it already returns a `SocketResponse`; the adapter maps
  that to `Result`, or is refactored to return `Result` directly — see §6 SR-2).
- **subscribe's uuid.Parse validation** (`:539`) — stays in the subscribe adapter (daemon;
  needs `github.com/google/uuid`, which is NOT on the router's tight edge).

---

## 3. How the 13-param signature collapses + public API

Two independent collapses, sequenced separately (§6):

### D1 — the dispatch table (SR-2): a daemon adapter-holder + a `socketrouter.Router`

Introduce a daemon-side holder bundling the injected handlers, with one **small named method per
op** returning a `socketrouter.Result`:

```go
// daemon/socketdispatch.go  (new, daemon package)
type socketDispatch struct {
    h        RequestHandler
    qh       QueueHandler
    sub      SubscribeHandler
    oh       OperatorControlHandler
    ch       CommsSendHandler        // type-asserted for presence/recv/decisions
    crewh    CrewHandler
    sleepWakeh QuiesceOverrideHandler
    stateh   StateHandler
    dashh    DashboardHandler
}

func (d *socketDispatch) emitOutcome(ctx context.Context, _ net.Conn, raw json.RawMessage) socketrouter.Result { … }
func (d *socketDispatch) claimNext(ctx context.Context, _ net.Conn, raw json.RawMessage) socketrouter.Result { … }
func (d *socketDispatch) queueSubmit(ctx context.Context, _ net.Conn, raw json.RawMessage) socketrouter.Result { … }
// … 26 methods, each ~6–12 lines, cyclop 2–3.

func buildSocketRouter(d *socketDispatch) *socketrouter.Router {
    r := socketrouter.New()
    r.Register("emit-outcome", d.emitOutcome)
    r.Register("claim-next",   d.claimNext)
    // … 26 flat Register lines. cyclop(buildSocketRouter) = 1.
    return r
}
```

`handleSocketConn` collapses to:
```go
func handleSocketConn(ctx context.Context, conn net.Conn, hr HookRelayHandler, router *socketrouter.Router) {
    defer conn.Close()
    raw, err := decodeRawMap(conn)            // + bad_envelope ack on error
    if err != nil { return }
    if socketrouter.Classify(raw) == socketrouter.KindHookRelay {
        handleHookRelayEnvelope(conn, hr, raw) // the daemon pre-branch (unchanged behavior)
        return
    }
    req, reEncoded, ok := decodeSocketRequest(conn, raw) // + error envelope on failure
    if !ok { return }
    res := router.Dispatch(ctx, conn, req.Op, reEncoded)
    if res.Streamed { return }               // subscribe: conn IS the stream
    writeSocketResponse(conn, resultToResponse(res))
}
```
`req` carries `Op`; ops that read scalar fields (`Role`, `Queue`, `Payload`) get them from
`reEncoded` decoded inside their adapter, OR the adapter is handed the parsed `SocketRequest`.
**Recommendation:** pass `reEncoded` (the raw bytes) so each adapter owns its own decode — this
keeps `SocketRequest` decoding identical and lets `queue-*` reuse `reEncoded` exactly as today
(`:468` passes `reEncoded` to the queue handlers verbatim). The three ops that read pre-parsed
scalar fields (`claim-next`→`Role`, `operator-*`→`Queue`) re-decode `SocketRequest` locally, or
`handleSocketConn` decodes once and passes `req` alongside `reEncoded`. **Simplest byte-preserving
choice: decode `SocketRequest` once in `handleSocketConn`, pass BOTH `req` and `reEncoded` into
Dispatch via a tiny request-context struct.** (Adapters that need `reEncoded` — queue, subscribe,
comms, crew — use it; adapters that need `req.Role`/`req.Queue`/`req.Payload` use `req`.) This
matches today's data flow exactly.

> Signature-wise: `handleSocketConn` goes **11 params → 4** (ctx, conn, hr, router). `hr` stays
> separate because the hook-relay branch is pre-Dispatch (keyed on `type`, not op). Alternatively
> fold `hr` into the router too via a sentinel — **rejected** (see open Q2): the hook-relay ack is
> NDJSON and keyed on envelope shape, not op; keeping it a pre-branch is the honest model.

### D2 — the telescoping listeners (SR-3): a `SocketHandlers` struct + one shared Accept loop

Today: 7 `RunSocketListener*` wrappers (`socket.go:278/284/294/323/332`, `socket_state.go:51`,
`socket_dashboard.go:55`) form a telescoping-constructor chain, and **three** of them
(`…WithSleepWake`, `…WithState`, `…WithDashboard`) each duplicate the full
`removeStaleSocket → Listen → Chmod → ctx-close goroutine → Accept loop` body verbatim.

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
func Serve(ctx context.Context, sockPath string, hs SocketHandlers) error
```
`Serve` builds the router once (`buildSocketRouter`) and the shared Accept loop calls
`handleSocketConn(ctx, conn, hs.HookRelay, router)`. The 7 named wrappers are retained as **thin
back-compat adapters** (build a `SocketHandlers`, call `Serve`) so the many existing test callers
(`socket_test.go`, `socket_queue_test.go`, `subscribe_test.go`, the `cmd/harmonik` comms tests,
etc.) don't churn. The production composition root (`daemon.go:2047`) switches to `Serve` +
struct. This is the real death of the 13-positional-param chain.

---

## 4. Depguard edge + proof

New rule, inserted after the `orchestrator` rule (keeping the block core→daemon ordered). The
routing surface touches only `context`, `encoding/json`, `fmt`, `net` — **no `internal/core`
needed** (op is a string, payloads are `json.RawMessage`). So the edge is the tightest in the
tree: `$gostd` + self-import only.

```yaml
        # socket-router: the pure protocol dispatch table (giant-retirement follow-up,
        # codename socket-router). Value-in/value-out routing: Classify + Router.Dispatch +
        # Result envelope over json.RawMessage. The daemon threads effectful handler closures
        # IN (mergeq/runexec pattern); socketrouter MUST NOT import daemon back. Tighter than
        # orchestrator/policy — no core needed (ops are strings, payloads are json.RawMessage).
        socketrouter:
          files: ["**/internal/socketrouter/**"]
          allow:
            - "$gostd"
            - "github.com/gregberns/harmonik/internal/socketrouter"   # self-import (external test pattern)
          deny:
            - { pkg: "github.com/gregberns/harmonik/internal/daemon", desc: "socketrouter is a pure routing leaf; daemon threads handler closures in, never the reverse" }
```

**Proof (golangci-lint is NOT installed in this env — same constraint noted in c043/c044/c050;
use the `go list` proof the prior slices used):**
```
go list -deps ./internal/socketrouter | grep harmonik
# EXPECTED: only  github.com/gregberns/harmonik/internal/socketrouter  (self). Zero other
# internal deps — even tighter than internal/policy (which shows core+self).
```
Run `go tool golangci-lint run ./internal/socketrouter/...` on the final merge box to confirm the
depguard rule activates (as c052 did for the orchestrator rule).

---

## 5. Ordered, behavior-preserving sub-slices

**Each is independently committable, green-verifiable alone, and gets its own STOP + independent
review** (M5 discipline). All three preserve the wire protocol byte-for-byte.

### SR-1 — scaffold the pure package (lowest risk, zero daemon change)
Create `internal/socketrouter/` via the `go-subsystem-add` skill: `doc.go` (purity contract,
mirroring `policy/doc.go`), `router.go` (`Result`, `Kind`, `Classify`, `HandlerFunc`, `Router`,
`New`, `Register`, `Ops`, `Dispatch`), `router_test.go` (external `socketrouter_test`). Add the
depguard rule (§4). **Pure unit tests** (no daemon): Dispatch routes each registered op to its fn;
unknown op returns the exact `daemon: unknown op "x"` envelope; `Classify` truth-table (type
present+len>2 → HookRelay; type absent / len≤2 / op-only → Op); `Ops()` sorted. Proves the package
+ the hard `$gostd`-only edge. `go build ./... && go test ./internal/socketrouter/...` green.

### SR-2 — swap the switch (the actual giant retirement)
Add `daemon/socketdispatch.go`: the `socketDispatch` holder, 26 small adapter methods (each a
verbatim lift of one `case` body → `Result`), and `buildSocketRouter`. Rewrite `handleSocketConn`
to decode → `Classify` → hook-relay pre-branch (unchanged) → `Dispatch` → `resultToResponse` →
write (§3 D1). Keep `handleQueueOp`, `writeHookRelayAck`, `writeSocketResponse`, the hook-relay
handling, and subscribe's uuid validation daemon-side. **No wire change.** The existing
round-trip suites (`socket_test.go`, `socket_queue_test.go`, `subscribe_test.go`, all per-op test
files, the `cmd/harmonik` comms/decisions tests) are the byte-identity guard and MUST pass
unchanged. Add the wiring-completeness guard (§6 test T4). This is where `handleSocketConn` drops
under every ceiling. Its own review.

### SR-3 — collapse the telescoping listeners (widest touch, lowest logic risk)
Add `SocketHandlers` + `Serve` (§3 D2); dedupe the three duplicated Accept-loop bodies
(`socket.go` / `socket_state.go` / `socket_dashboard.go`) into `Serve`; convert the 7
`RunSocketListener*` wrappers to thin `Serve`-delegating adapters; point `daemon.go:2047` at
`Serve`. Pure refactor — no envelope change. Its own review. **Can be deferred / dropped if scope
tightens** — SR-1+SR-2 already retire the giant; SR-3 is purely the param-list cleanup. Sequenced
last because it is independent of the router extraction and touches the most call sites.

Order rationale: SR-1 stands up the package + edge at ~zero blast radius; SR-2 is the load-bearing
behavior-preserving cut; SR-3 is cosmetic-but-wide and best isolated for review.

---

## 6. Test strategy — proving wire-byte-identical routing without the live daemon

Two tiers, neither needs a daemon boot:

**Tier 1 — pure router unit tests (`package socketrouter_test`, SR-1).**
Table-driven over `Dispatch` and `Classify`:
- **T1 route-hit:** register a probe fn per op name; assert each op invokes exactly its fn and
  returns its `Result` untouched.
- **T2 unknown-op:** `Dispatch(ctx,nil,"bogus",nil)` → `Result{OK:false, Err:"daemon: unknown op \"bogus\""}`
  — **exact string**, the default-envelope byte contract.
- **T3 Classify truth-table:** `{"type":"outcome_emitted"}` → HookRelay; `{"type":""}` (len≤2) →
  Op; `{"op":"claim-next"}` → Op; `{}` → Op. Mirrors `socket.go:403` (`len(typeRaw) > 2`).

**Tier 2 — daemon wire-parity (`package daemon` / `cmd`, SR-2/SR-3), no boot.**
- **The existing round-trip suites are the primary guard** — they dial a real unix socket to
  `RunSocketListener*` with stub handlers and assert on the raw `SocketResponse`
  (`socket_test.go`: EmitOutcome/ClaimNext/UnknownOp/HandlerError; `socket_queue_test.go`: the
  `-32099` nil-QueueHandler path + per-op routing; `subscribe_test.go`; the comms/presence/recv/
  decisions/crew/operator-pause files). They MUST pass unchanged. This is what proves the
  envelopes did not shift.
- **T4 wiring-completeness guard (NEW, load-bearing):** assert
  `buildSocketRouter(&socketDispatch{}).Ops()` equals the frozen set of 26 op strings. This
  catches the one silent regression the carve can introduce: a **missing `Register`** would turn a
  real op into an `unknown op` (a different envelope) instead of its `not registered` envelope.
- **T5 golden error-envelope table (NEW, recommended):** drive `handleSocketConn` over a
  `net.Pipe` conn with an all-nil `socketDispatch`, one row per op, asserting the **exact JSON
  bytes** of the nil-handler envelope (e.g. `comms-presence` → `{"ok":false,"error":"daemon:
  CommsPresenceHandler not registered"}` with no trailing newline; queue ops carry
  `"error_code":-32099`; the decode-fail path emits the `bad_envelope` ack **with** `\n`). This
  pins every §1 string + the newline asymmetry in one place, daemon-boot-free. (`net.Pipe`
  already used by the existing subscribe/comms tests, so the harness exists.)

No live socket, no daemon composition root, no beads required for any tier.

---

## 7. Target ceiling + honest expected numbers

Ceilings (`.golangci.yml:88-96`): `funlen` 100 lines / 60 statements · `cyclop` 15 · `gocognit` 20.
golangci-lint is not installed here, so exact pre-numbers can't be printed; the estimates below
are from the source structure (26-case switch, ~40 `if`s, ~420-line body).

| Symbol | before | after | under ceiling, no `//nolint`? |
|---|---|---|---|
| `handleSocketConn` | ~420 lines / gocognit ≈ 60 / cyclop ≈ 45 (grandfathered) | ~30 lines / gocognit <8 / cyclop <6 | **yes** |
| each op adapter method (×26) | — | ~6–12 lines / cyclop 2–3 | yes |
| `buildSocketRouter` | — | ~30 lines / 26 statements / cyclop 1 | yes (26 < 60 stmt) |
| `socketrouter.Dispatch` | — | ~8 lines / cyclop 2 | yes |
| `Serve` (SR-3) | (3× duplicated ~30-line loops) | one ~35-line body | yes; deletes ~60 duplicated lines |

**Honest caveat (mirrors c052):** total daemon LOC is roughly flat across SR-2 — the switch's ~360
lines become 26 methods + a builder of comparable size. The defensible wins are: (1)
`handleSocketConn` retired as a grandfathered giant, under every ceiling with no suppression; (2) a
pure, unit-tested `Router`/`Classify` with the tree's tightest edge; (3) SR-3 actually deletes ~60
lines of triplicated Accept-loop boilerplate and kills the 13-param telescoping chain. Do not
promise a large net line reduction from SR-2 alone.

---

## 8. Risks + load-bearing invariants

1. **Wire byte-identity (THE invariant).** Every §1 string, the `ok`/`result`/`error`/`error_code`
   field set, and the field order in the marshaled JSON must not change. Mitigation: adapters lift
   `case` bodies verbatim; `SocketResponse` struct (hence JSON field order) is untouched (D1 keeps
   it in daemon); both writers stay daemon-side; the existing round-trip suite + T5 golden-bytes
   table guard it. **`SocketResponse` field order is frozen** — do not reorder its struct fields.
2. **Newline asymmetry.** hook-relay ack = JSON + `\n` (`writeHookRelayAck`); op response = JSON,
   no newline (`writeSocketResponse`). Both stay daemon-side and unchanged. T5 asserts both.
3. **subscribe streaming suppression.** Op 11 writes nothing and `return`s; the streamed path is
   modeled as `Result{Streamed:true}` and `handleSocketConn` must `return` without writing.
   Regression modes: an empty `{}` envelope written, or a double-write. Guarded by
   `subscribe_test.go` (existing) + a `Streamed` assertion.
4. **Type-assertion ops (13–18).** `comms-presence`/`comms-recv`/`decisions-*` type-assert `ch` to
   a sub-interface; the two-value assert + nil-guard and the exact "not registered" strings must
   survive. `errcheck` runs with `check-type-assertions: true` (`.golangci.yml:84`) — the adapter
   must use the comma-ok form (it already does at `:566/:582/:603`).
5. **Wiring completeness (handler injection).** A missing `Register` silently degrades a real op to
   `unknown op` (wrong envelope). Guarded by T4 (`Ops()` == frozen 26-set). This is the single most
   likely carve bug.
6. **`HandlerFunc` carries `net.Conn`.** Needed so the subscribe adapter can stream; it pulls `net`
   into `socketrouter` (still `$gostd`, edge intact). Most adapters ignore `conn`. Alternative
   (special-case subscribe as a daemon pre-branch, `HandlerFunc` conn-free) is Open Q4.
7. **Router lifetime.** Build the router **once per `Serve`** (handlers are fixed for the
   listener's life), not per-conn. Document; a per-conn rebuild would be correct but wasteful.
8. **`hr` stays a separate param.** The hook-relay branch is pre-Dispatch (keyed on `type`), so
   `hr` is not a route. Folding it in (Open Q2) would blur the NDJSON-ack model.

### Open questions for reviewers
- **Q1 (wire types):** keep `SocketRequest`/`SocketResponse` in daemon + neutral `Result`
  (**recommended** — minimal blast radius; tests + `cmd/` reference them widely), or move them into
  `socketrouter` (cleaner ownership, wide ripple)? Design assumes **keep in daemon**.
- **Q2 (hook-relay):** keep the hook-relay branch as a daemon pre-branch (**recommended**), or model
  it as a router route on a `type`-sentinel? Recommend pre-branch (NDJSON ack, keyed on envelope
  shape not op).
- **Q3 (SR-3 scope):** include the listener-telescoping collapse in this milestone (where the
  13-param chain truly dies) or defer it — SR-1+SR-2 already retire the giant. Recommend include,
  sequenced last, droppable under time pressure.
- **Q4 (`HandlerFunc` shape):** `net.Conn` in the signature for streaming (recommended, uniform),
  or conn-free `HandlerFunc` with subscribe special-cased daemon-side?
- **Q5 (request threading):** pass both parsed `SocketRequest` and `reEncoded` bytes into Dispatch
  (recommended — matches today's dual data flow: queue ops use `reEncoded`, scalar ops use `req`),
  or re-decode per adapter?
