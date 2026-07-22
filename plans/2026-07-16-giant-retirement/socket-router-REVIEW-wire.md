# Adversarial wire-review — `socketrouter` extraction

> Lens: **wire byte-identity & protocol correctness**. Read-only, skeptical.
> All `file:line` below verified against live source on 2026-07-16 (not trusting the doc's numbers).
> Reviewer target: `plans/2026-07-16-giant-retirement/socket-router-DESIGN.md`.

## Verdict: **APPROVE-WITH-CHANGES**

The extraction mechanics are sound: `SocketResponse` stays in daemon (field order frozen), both
writers stay daemon-side, and `Dispatch` never touches `conn.Write`. Given those, byte-identity is
*structurally achievable* and *likely to hold*. **But the design's designated proof is invalid**:
the "existing round-trip suites are the primary guard" claim is false for *byte* identity — every
one of those tests decodes into a struct and is blind to field order, trailing newlines, and most
`omitempty` drift. The one test that would actually prove byte-identity (T5) is marked merely
"recommended" and covers only error paths. **Byte-identity is asserted but not proven.** The changes
below are mandatory before implementation; none are architectural blockers.

---

## Findings

### F1 — [CRITICAL] The named "primary guard" is byte-blind; byte-identity is unproven
**Design §6 Tier-2** claims the existing round-trip suites "prove the envelopes did not shift" and
are "the primary guard." Verified false. Every socket response in those suites is read via
`json.NewDecoder(conn).Decode(&resp)` into a `SocketResponse` struct and asserted field-by-field:
- `socket_test.go:113` (`&resp`), `:505` (hook-relay `&ack`) — struct decode.
- `socket_queue_test.go:173` — asserts `resp.ErrorCode != -32099` (field, not bytes).
- `socket_operatorpause_ry8q1_test.go:67,89` — `Decode(&resp)`, asserts `resp.Ok`.
- `socket_comms_nbrmf_test.go`, `socket_commspresence_7t27s_test.go`, `socket_decisions_xz9_test.go`,
  `subscribe_test.go` — all `NewDecoder(conn).Decode`.

`json.Decode` is **order-insensitive** and **silently swallows a trailing newline** (or its
absence). So these tests cannot detect: (a) a `SocketResponse` field reorder, (b) an added/dropped
`'\n'` on op responses, (c) `"result":null` vs an omitted `result`. They prove *decoded-field*
identity, not *wire-byte* identity. No test anywhere asserts raw bytes or the newline (grep for
`ReadString('\n')`/`HasSuffix`/`bytes.Equal` on socket responses returns nothing).

**Required change:** demote the §6 claim (the round-trip suites are a *regression* guard for
decoded semantics, not a byte-identity guard) and **elevate T5 from "recommended" to REQUIRED** as
the sole byte-identity proof. Without T5, this milestone ships an unproven byte-identity assertion.

### F2 — [HIGH] `resultToResponse` is the only real drift surface and T5 doesn't cover it
Because the struct and both writers are untouched, the **only** new code that can shift bytes is the
`Result → SocketResponse` mapper (`resultToResponse`, design §3 skeleton) plus each adapter's
population of `Result`. The genuine trap: the four `{ok:true}` **no-result** ops
(`operator-pause`/`operator-resume` `socket.go:673,684`; `daemon-sleep`/`daemon-wake` `:739,760`)
today emit `SocketResponse{Ok:true}` → `result` **omitted** (`omitempty`). If an adapter sets
`Result.Payload` to a non-nil empty/`"null"` slice, `resultToResponse` yields `"result":null` — a
wire change. T5 as specified drives an **all-nil** `socketDispatch`, so it exercises only the
"not registered" *error* rows; it never exercises a **success** envelope, so this exact trap is
untested.
**Required change:** expand T5 to include, with real stub handlers, the golden bytes of at least:
(a) `operator-pause` success `{"ok":true}` (no `result`, no newline); (b) an `emit-outcome` success
carrying a `result`; (c) a queue-op success (`error_code` absent). This pins every distinct
`Result→SocketResponse` shape, not just the nil-handler errors.

### F3 — [MEDIUM] Newline asymmetry is real, correctly located, but currently unguarded
Verified: `writeHookRelayAck` (`socket.go:837-843`) writes `append(data, '\n')`;
`writeSocketResponse` (`:847-855`) writes `data` with no newline. Writer usage:
- `writeHookRelayAck` (NDJSON+`\n`): initial raw-decode failure (`:395`), hook-relay re-encode fail
  (`:408`), envelope-decode fail (`:413`), nil-`hr` (`:417`), and the normal ack (`:421`).
- `writeSocketResponse` (no newline): op re-encode fail (`:428`), decode-request fail (`:432`), and
  every op response (`:806`).

The design preserves this structurally (writers stay in `handleSocketConn`, `Dispatch` returns a
value). **Trap:** the **initial raw-decode failure** (`:392-400`) uses the *hook-relay* writer
(with `\n`) even for what would have been an op request. The design's `decodeRawMap` "+ bad_envelope
ack on error" (§3 skeleton) MUST call `writeHookRelayAck`, not `writeSocketResponse`. §1a documents
this correctly, but it is easy to get wrong. **Required:** T5 must assert BOTH the trailing `\n` on
the `bad_envelope` decode-fail ack AND its absence on the `unknown op`/`not registered` op
responses — this is the first test in the tree to guard the asymmetry at all.

### F4 — [LOW] `HandlerFunc` signature contradicts the §3 threading recommendation
§2 fixes `HandlerFunc = func(ctx, conn, raw json.RawMessage) Result` (raw only), but §3/Q5
recommends "pass BOTH parsed `SocketRequest` and `reEncoded` via a tiny request-context struct."
These conflict. Many ops read *parsed scalar fields*, not `reEncoded`: `claim-next`→`req.Role`
(`:455`), `operator-*`→`req.Queue` (`:670,681`), and `comms-*`/`decisions-*`/`crew-*`/`daemon-*`
→`req.Payload` (`:556,571,587,608,694,708,731,752`). Under the §2 signature these adapters must
**re-decode `SocketRequest` from `raw`**. That is byte-identical (same `reEncoded` bytes, same
`json.RawMessage` extraction, same `nil` when a key is absent), so **no wire risk** — but the design
must pick one shape. Recommend resolving Q5 explicitly (either widen `HandlerFunc` to carry `req`,
or state "adapters re-decode from raw"); do not leave §2 and §3 contradictory.

### F5 — [LOW] `Classify` truth-table omits the `len>2` edge cases that the heuristic actually admits
`socket.go:403` is `hasType && len(typeRaw) > 2`. `typeRaw` is the raw JSON of the value, so
`{"type":null}` (len 4) and `{"type":123}` (len 3) both classify as **HookRelay**, while
`{"type":""}` (len 2) and `{"type":"x"}`… wait `"x"` is len 3 → HookRelay. The design's T3 table
tests only `"outcome_emitted"`, `""`, `{"op":…}`, `{}`. As long as `Classify` copies the literal
`len(raw["type"]) > 2` expression, these quirks are preserved — but add `{"type":null}`→HookRelay
and `{"type":"x"}`→HookRelay to T3 so a future "cleanup" of the heuristic can't silently pass.

### F6 — [LOW] SR-3 unification changes per-listener error-string prefixes (non-wire)
The three duplicated Accept bodies use distinct error prefixes:
`RunSocketListener*` (`socket.go:334,339,345,367`), `RunSocketListenerWithState`
(`socket_state.go:53,58,63,82`), `RunSocketListenerWithDashboard`
(`socket_dashboard.go:57,62,67,86`). Collapsing to one `Serve` changes these literals unless the
adapters re-wrap. These are **returned startup errors, not wire bytes** — out of this lens's scope —
and `errors.Is(err, errLiveDaemon)` survives (%w chain intact). No test string-matches these
prefixes (grep confirms). Safe to change; noted only so the implementer expects the diff.

### F7 — [OK, verified] Op count, `Ops()` frozen-set guard, and the dropped-`Register` failure mode
Confirmed **26 `case` labels + `default`** (grep count = 26; enumerated list matches §1b exactly).
T4's mechanism is sound: `buildSocketRouter` calls `Register` unconditionally, so
`buildSocketRouter(&socketDispatch{}).Ops()` reflects the registration set regardless of handler
nility; a dropped `Register("crew-start", …)` yields a 25-element `Ops()` that fails the frozen-26
assertion. **And** the runtime failure mode the design names is real and asymmetric: a dropped
register routes a live op to `default` → `daemon: unknown op %q` (`:800`) instead of its
`… not registered` envelope — a genuine silent envelope regression that T4 catches. **T4 must be
mandatory, not optional.**

### F8 — [OK, verified] subscribe streaming + daemon-side uuid validation
`subscribe` (`:519-545`) has four sub-paths: nil-`sub`→error+`break`, decode-fail→error+`break`,
bad-uuid→error+`break` (`uuid.Parse`, `:539`), success→`HandleSubscribe`+`return` (no write). The
`Result{Streamed:true}` model with `handleSocketConn` returning without writing preserves this,
**and** the three error sub-paths still fall through to `writeSocketResponse` (they set `resp` and
`break` today). Keeping uuid validation in the daemon adapter is correct: `uuid.Parse` is
`github.com/google/uuid`, deliberately off the router's `$gostd`-only edge (§4). `subscribe_test.go`
plus a `Streamed` assertion guard the no-write path. No wire risk.

### F9 — [OK, verified] Type-assertion ops preserve comma-ok + exact strings
`comms-presence`/`comms-recv`/`decisions-*` (`:566,582,603,616,636,649`) use the two-value
`ch.(Iface)` form + `!ok || x==nil` guard and emit exact `daemon: <Handler> not registered`
strings. `errcheck check-type-assertions:true` (`.golangci.yml:84`) forces comma-ok; a verbatim
`case`-body lift preserves it. Fine.

---

## Bottom line on the invariant
Wire byte-identity **can** hold and the mechanics make it likely — the untouched `SocketResponse`
struct and the two daemon-side writers are the load-bearing guarantees, and both are correctly kept
out of `socketrouter`. **But as designed it is not proven.** The only real drift surface
(`resultToResponse` + adapter `Result` population, esp. the `{ok:true}` no-result and `error_code`
shapes) is exercised by zero byte-level test, because the "primary guard" round-trip suites are
struct-decode (order/newline/`null`-blind) and T5 is optional + error-only. Make **T4 and T5
mandatory**, expand **T5** to cover success/no-result/`error_code`/both-newline rows, and the
byte-identity claim becomes real rather than asserted.
