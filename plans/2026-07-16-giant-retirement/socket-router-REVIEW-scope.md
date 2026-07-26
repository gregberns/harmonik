# Adversarial scope review — `internal/socketrouter` extraction from `handleSocketConn`

> Read-only adversarial review (2026-07-16). Lens: SCOPE · VALUE · ALTITUDE · METHOD-FIT.
> Target: `plans/2026-07-16-giant-retirement/socket-router-DESIGN.md`.
> All `file:line` claims below re-verified against live source on 2026-07-16.

## Verdict: **APPROVE-WITH-CHANGES**

The design is honest, meticulously grounded (the §1 byte-identity enumeration is the best part of
it), and method-fit is strong — it mirrors the proven c043/c050/c052 loop and follows the c043
precedent of replicating validation to keep wire acks byte-identical. It will produce *correct*
code. It is **not** blocked on correctness.

But on its own central question — *does this retire the giant or reshape it, and is the new
subsystem worth its weight?* — the design under-interrogates two things: (1) whether a routing
table earns a **top-level `internal/` subsystem + depguard row** at all, and (2) whether **SR-2**
(the actual retirement) is safely reviewable as one 26-op rewire. Both are altitude/scope calls,
not bugs. Resolve them before implementing.

## Crux judgment

**This reshapes the giant more than it retires it — but the reshape is legitimate and the design
says so.** `handleSocketConn` genuinely leaves the grandfather list (420→~30 lines, under every
ceiling, no `//nolint`) and that is a real win. But the "subsystem" being stood up is a
**generic routing table** — `map[string]HandlerFunc` + a one-line `Classify` predicate + an
~8-line `Dispatch`. That is by far the **thinnest pure core of the four M5-family cuts**: hook
extracted a dedup state machine, policy a hysteresis reducer, orchestrator a queue-selection
algorithm — each a genuine *domain brain* worth isolating. socketrouter's pure content is
infrastructure (a lookup + a `len>2` check). The 26 per-op bodies — where the actual logic lives
— **stay in daemon** as 26 adapter methods. And per §7 the *only* real line deletion (~60 lines of
triplicated Accept-loop) lives in **SR-3, which the design itself marks "droppable."** So the
mandatory deliverable (SR-1+SR-2) is: one giant off the list + a tiny pure router, at flat LOC.
Worth doing; worth right-sizing.

---

## Findings

### F1 — [ALTITUDE, should-fix] The top-level subsystem is the least-justified of the series; the design assumes it rather than earning it.
`subsystem-organization.md` models S01–S09 as **domain** subsystems with envelopes (architecture
§1.4). `socketrouter` is a **net-new row absent from that doc** — unlike hook (S05), policy (S02),
orchestrator (S01), which all had reserved rows the prior cuts filled. Its entire content is a
domain-free routing data structure. Two facts sharpen this:
- The doc's own **Deferred** section anticipates a router as a **sub-package** — verbatim: *"Sub-package structure inside each subsystem (e.g. `orchestrator/runner`, `orchestrator/router`)."* It does **not** anticipate `router` as a top-level sibling subsystem.
- The genuinely-pure surface is a map lookup + a `len(typeRaw)>2` predicate. The design (§0) calls this "a genuinely pure, table-driven surface" — true, but thin. A hard `$gostd`-only depguard edge is real machinery to erect around a `map[string]func`.

The design never asks *"does this need to be a top-level `internal/` package, or would a
daemon-internal `router` type / an `internal/daemon/router` sub-package suffice?"* c051/c052 named
"socket-router subsystem" **loosely** as a follow-up direction; that ruling does not bind the
design to a top-level depguard row vs a sub-package. **Change:** add an open question that
seriously weighs top-level-package vs daemon-internal-type vs sub-package, and either justify the
top-level row against the doc's domain-subsystem model or demote it. This is the single biggest
scope call and it is currently assumed.

### F2 — [SCOPE/METHOD, should-fix] SR-2 rewires all 26 ops in one slice, and hides a SR-2/SR-3 sequencing tension.
Every prior M5 cut (hook, policy, orchestrator) was split A→B→C **because a single big cut was too
much to review**. SR-2 is monolithic: it lifts 26 `case` bodies → 26 methods **and** swaps the
switch for `Dispatch`, all in one commit whose byte-identity must be checked by eye against §1.
The guards (existing round-trip suites, T4 wiring-completeness, T5 golden-bytes) are good and
mitigate the risk — but 26 verbatim lifts is still a lot for one review gate.

Worse, there is an **unresolved construction boundary** between SR-2 and SR-3. §3 D1 changes
`handleSocketConn`'s signature to take `router *socketrouter.Router`. Risk #7 says "build the
router **once per `Serve`**" — but `Serve` is **SR-3**. So in SR-2-without-SR-3, where is the
router built? Either (a) inside each of the 7 `RunSocketListener*` wrappers / Accept loops — which
is SR-3's plumbing, so SR-2 and SR-3 are **not** cleanly separable as claimed; or (b) per-conn
inside `handleSocketConn` — which **violates risk #7** (wasteful rebuild per connection). The
design must pick one and say so.
**Change:** split SR-2 into **SR-2a** (introduce `socketDispatch` holder + 26 methods, *switch
still routes to them* — trivially byte-identical, switch untouched) and **SR-2b** (replace the
switch body with `Dispatch`). 2a is a mechanical extraction reviewable at a glance; 2b is the small
routing-mechanism swap. This also resolves the construction boundary: 2b builds the router where
the switch was. Recommended over the current monolithic SR-2.

### F3 — [VALUE, should-fix] The only real LOC deletion is in the "droppable" slice — sharpen the honest bar.
§7 concedes SR-2 is flat-LOC and the ~60-line deletion + the 13-param-chain death are **entirely
in SR-3**, which §5 marks "can be deferred/dropped." So if SR-3 drops, the "retirement" is pure
complexity-redistribution at flat LOC. The design is candid about this (good), but it should state
the mandatory-deliverable value proposition plainly: *SR-1+SR-2 alone buy one giant off the
grandfather list + a small pure router; the smell everyone can see (the 13-positional-param
telescoping chain) is only fixed by the optional slice.* Recommend **making SR-3 non-optional** if
this work is greenlit at all — dropping it leaves the least-satisfying half. (The 13-param chain,
verified: `handleSocketConn` at `socket.go:387` really does take 12 params; the wrappers at
`socket.go:278/284/294/323/332`, `socket_state.go:51`, `socket_dashboard.go:55` really do
telescope. That chain is the honest eyesore — don't ship the cut without killing it.)

### F4 — [ALTITUDE, should-fix] `HandlerFunc` carrying `net.Conn` for one op keeps the router impure; pre-branching subscribe (like hook-relay already is) would make it truly pure.
§2 / risk #6 / Open Q4: `HandlerFunc` carries `net.Conn` **solely so the subscribe adapter can
stream** — every other adapter ignores it, and it pulls `net` onto the router's edge. But
**hook-relay is already modeled as a daemon pre-branch** (§3, keyed on `type`). Symmetry argues
subscribe — the *other* non-standard, response-shape-breaking op — should **also** be a daemon
pre-branch. Then `HandlerFunc` drops `conn`, `Result.Streamed` disappears, and the edge sheds
`net` → `context`+`encoding/json`+`fmt` only, and `Dispatch` becomes a genuinely pure
`(ctx, op, raw) → Result`. The design recommends the *opposite* (Q4: keep `conn`, uniform). For a
package whose entire selling point is purity + the tightest edge in the tree, admitting `net` for
one streaming op undercuts the thesis. **Change:** flip the Q4 recommendation toward conn-free
`HandlerFunc` + subscribe-as-pre-branch, or justify why the two special ops are treated
asymmetrically.

### F5 — [nit, consider] "daemon:" vocabulary leaks into the leaf package.
`Dispatch`'s unknown-op branch returns `"daemon: unknown op %q"` — a **daemon-prefixed** string now
literally living in `socketrouter` (§2). Byte-identity holds (it's a literal), so this is not a
bug. But a leaf package called `socketrouter` emitting `daemon:`-prefixed errors is a small
ownership smell that slightly weakens the "clean subsystem" framing. Acceptable; worth a
one-line acknowledgement. (If subscribe/hook-relay both pre-branch per F4, consider whether the
unknown-op envelope should be constructed daemon-side too, keeping ALL wire vocabulary in daemon
and leaving socketrouter returning a neutral "not found" signal.)

### F6 — [METHOD, positive] Byte-identity handling and method-fit are strong.
§1's verbatim-string enumeration + the newline asymmetry callout (verified: `writeHookRelayAck`
`socket.go:842` appends `'\n'`; `writeSocketResponse` `:854` does not) + T5 golden-bytes table +
T4 wiring-completeness guard (the *right* guard for the one silent regression — a missing
`Register` degrading a real op to `unknown op`) together form a solid behavior-preservation net.
The comma-ok type-assertion note (risk #4, `errcheck check-type-assertions:true`) is correctly
flagged. This half of the design needs no changes.

---

## On the design's 5 open questions
- **Q1 (wire types stay in daemon):** agree — keep. Minimal blast radius; `cmd/`+tests reference them widely. Correct call.
- **Q2 (hook-relay pre-branch):** agree — keep pre-branch. NDJSON ack keyed on envelope shape, not op.
- **Q3 (SR-3 scope):** include — and per **F3**, make it non-optional, not "droppable." The 13-param death is the honest payoff.
- **Q4 (`HandlerFunc` shape):** **disagree with the recommendation** — see **F4**; lean conn-free + subscribe-as-pre-branch.
- **Q5 (thread `req`+`reEncoded`):** agree — matches today's dual data flow.

**Missing questions the design should add:**
- **Q6 (F1):** top-level subsystem vs daemon-internal type vs `daemon/router` sub-package — the biggest unasked call.
- **Q7 (F2):** split SR-2 into 2a (holder+methods behind the switch) / 2b (switch→Dispatch), and resolve where the router is constructed in a SR-2-without-SR-3 world.

---

## Bottom line
APPROVE-WITH-CHANGES. Correctness and method are sound; ship *after* right-sizing the altitude:
justify or demote the top-level subsystem (F1), split SR-2 and fix its construction boundary (F2),
commit to SR-3 as the real payoff (F3), and prefer a truly-pure conn-free router (F4). This is a
legitimate cleanup that retires one grandfathered giant — but it is the lightest-weight of the
M5-family cuts, and its new-subsystem framing is the part most worth challenging before it becomes
a permanent depguard row.
