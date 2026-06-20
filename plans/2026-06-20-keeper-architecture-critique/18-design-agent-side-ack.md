# Design — Agent-side half of the keeper ACK handshake (hk-uldg)

> **STATUS: REQUIRES-OPERATOR-REVIEW.** This proposes a NEW NORMATIVE MECHANISM
> (a new CLI subcommand + an escalation event + a skill-contract change). No code
> has been changed. Per the working-style "check in first" threshold, a new
> normative contract gets operator sign-off before implementation.

Bead: **hk-uldg** — "keeper: agent-side ACK timer + auto-investigate (close the
restart-now handshake)". P1, feature.

Companion (DO NOT collide): **hk-vpnp** — the AUTOMATIC ACT-when-idle watcher
loop/handoff-truncation bug. See §6.

---

## 0. What already exists (keeper -> pane side, commit 89852bb3 on main)

- `keeper.RestartNow(ctx, cfg, nonce)` — verifies session id + handoff freshness,
  then injects `[KEEPER ACK <nonce>] received restart`, `/clear`, `/session-resume`
  **synchronously** in the CLI process. `internal/keeper/restartnow.go`.
- `keeper.Ping(ctx, cfg, nonce)` — injects ONLY `[KEEPER ACK <nonce>] received ping`.
  Pure liveness, no cycle, no gates.
- `keeper.AckLine(nonce, kind)` — formats `[KEEPER ACK <nonce>] received <kind>`.
  `internal/keeper/injector.go`.
- CLI: `harmonik keeper restart-now --agent <name>` prints `nonce=rn-<unix_millis>`;
  `harmonik keeper ping --agent <name> [--nonce N]`. Both flag-only (positionals
  exit 2). `cmd/harmonik/keeper_cmd.go`.
- `keeper.ResolveTmuxTarget(projectDir, agentName, explicit, sessionExistsFn)` —
  resolves the agent's own pane. `internal/keeper/tmuxresolve.go`.

**What is missing (this design):** the agent never *confirms* the ACK. It fires
the command, trusts the exit code, and moves on. The operator's mechanism wants a
**timer + pane-watch**: ACK observed = keeper proven alive; timer expires = keeper
broken, investigate. That is the agent side, and it does not exist.

---

## 1. Where the agent-side timer/watcher lives — RECOMMENDATION: option (a)

### Recommendation: a new CLI subcommand `harmonik keeper await-ack`

```
harmonik keeper await-ack --agent <name> --nonce <N> [--kind restart|ping] \
    [--timeout 15s] [--poll 1s] [--project DIR]
```

It blocks until it sees `[KEEPER ACK <nonce>]` in the agent's OWN pane scrollback
(via `tmux capture-pane`), then **exits 0**. On timeout it **exits non-zero**,
emits an escalation event, and prints the failure reason. The pane-capture
function is an **injectable seam** so tests substitute a fake capturer.

### Why (a) over (b) skill-prose and (c) hook

| Criterion | (a) `await-ack` CLI | (b) skill prose | (c) hook |
|---|---|---|---|
| Deterministically testable | **Yes** — inject a fake capture-pane that returns/withholds the ACK; assert exit code + event. | No — prose can't be unit-tested; relies on the LLM running `tmux capture-pane` + grep correctly every time. | Partly, but hooks are harness-coupled and fire on harness lifecycle events, not on a fired command. |
| Reuses existing code | `ResolveTmuxTarget`, `AckLine`, the event emitter — all already in `internal/keeper`. | Reinvents pane resolution in shell each call. | Same reinvention. |
| Single source of truth for the match | One Go matcher. | N copies of a grep pattern scattered across captain/crew skills. | Hook script. |
| Survives `/clear` for restart-now | Caller is a separate OS process; `/clear` wiping the agent's context does not kill it. | The agent IS the context being cleared — it can't watch for its own ACK after `/clear`. | Hook runs out-of-process; OK, but see harness-coupling. |
| Escalation | Emits a structured keeper event + non-zero exit; trivially wired to comms. | LLM must remember to escalate. | Hook would have to shell to comms anyway. |

The operator's own friction-mining rule is explicit: **"a CLI subcommand with
injectable tmux-capture is far more testable than skill prose."** Option (a)
honors that. The skills still get a thin contract change (§4) — they CALL
`await-ack` — but the *logic* (timer, poll, match, escalate) lives in tested Go.

### Critical asymmetry: restart-now vs ping (who watches?)

- **`ping`** — the SAME live agent fires `ping` and then runs `await-ack`; the
  ACK lands in its own pane and the agent reads `await-ack`'s exit code. Clean,
  fully agent-driven.
- **`restart-now`** — the `/clear` **wipes the firing agent's context**, so the
  firing agent CANNOT wait for its own ACK (it is mid-suicide). The ACK is
  injected *before* `/clear` precisely so an **external** observer can verify it.
  Therefore `await-ack` for restart-now must be run by an **external watcher**,
  NOT the restarting agent. Two viable external watchers:
  1. **The keeper-restart launch wrapper** — if/when restart-now is wrapped by a
     script (e.g. a future `restart-now.sh` mirroring `captain-launch.sh`), that
     wrapper backgrounds `await-ack --kind restart` as a detached process before
     it triggers the restart. **(Recommended.)**
  2. **A peer agent / captain** — for crews, the captain can `await-ack` on the
     crew's pane after telling the crew to restart.

  This means for restart-now, the design's value is: the firing agent fires and
  immediately /clears; a separate `await-ack` process (wrapper or peer) confirms
  the keeper actually delivered the ACK and escalates if not. The restarting
  agent's *own* timer is moot — it's gone. **`ping` is the primary self-service
  liveness check; restart-now's confirmation is delegated.**

---

## 2. How the ACK is observed

- **Mechanism:** poll `tmux capture-pane -p -t <resolved-target>` (optionally
  `-S -<N>` to include a bounded scrollback tail, e.g. `-S -200`, so a fast ACK
  that already scrolled is still caught) and substring-match the resolved target
  line. Match on the **full bracket token** `[KEEPER ACK <nonce>]`, not just the
  nonce, to be unambiguous.
- **Poll interval:** default **1s**. **Timeout:** default **15s** for ping
  (matches the protocol doc's "~15s for ping"). For restart-now confirmation a
  longer window (e.g. **30s**) is reasonable because the keeper does freshness
  checks + three injects before/around the ACK; make it a flag so the operator
  tunes it.
- **Avoiding false matches across cycles (nonce uniqueness):**
  - restart-now nonces are `rn-<unix_millis>` — monotonic, unique per fire.
  - ping nonces are caller-supplied; the skill contract (§4) REQUIRES a fresh
    unique nonce per ping (e.g. `ping-<unix_millis>` or a short random suffix).
    `await-ack` matches the EXACT nonce it was given, so a stale ACK from a
    previous cycle with a different nonce never matches.
  - Because the match includes the nonce, scrollback from older cycles is inert —
    there is no "first ACK wins" ambiguity. (One residual risk: re-using the same
    nonce twice would match an old line; the contract forbids nonce reuse.)
- **Capture seam for tests:** define
  `type PaneCapturer func(ctx, tmuxTarget string) (string, error)` defaulting to
  the real `tmux capture-pane`. `AwaitAck` takes it (or a config field). Tests
  pass a fake — see §5.

---

## 3. On timeout (keeper broken) — escalation

When the timeout elapses with no matching ACK:

1. **Emit a new keeper event** `session_keeper_ack_timeout` (new
   `core.EventType`, sibling of the existing `session_keeper_*` family in
   `internal/core/eventtype.go`). Fields: `agent`, `nonce`, `kind`
   (restart|ping), `timeout`, `tmux_target`, `reason` ("ack_not_observed").
   This makes the failure **durable** in `events.jsonl` — an orchestrator's
   `harmonik subscribe` / a postmortem can find it.
2. **Exit non-zero** (suggest exit 3 — distinct from the flag-misuse exit 2 the
   keeper CLI already uses). Print a one-line reason to stderr:
   `keeper await-ack: no [KEEPER ACK <nonce>] within <timeout> — keeper may be
   dead, wrong pane, or unverifiable sid; investigate.`
3. **Surface to the operator via comms (NOT done by `await-ack` itself).** Keep
   `await-ack` a pure, side-effect-light Go binary (event + exit code only). The
   CALLER (skill/wrapper) is responsible for the comms send on non-zero exit:
   `harmonik comms send --to operator --topic keeper-alert --from <lane> \
     "keeper ACK timeout for <agent> nonce <N> — investigating"`.
   Rationale: comms identity (`--from <lane>`) is the caller's, not the keeper
   CLI's; baking a hardcoded `--from` into the binary would risk the
   "uncommissioned --from captain freezes the fleet" footgun (see memory). The
   skill knows its own lane.
4. **What the agent does next:** the skill contract says on `await-ack` non-zero,
   the agent (a) does NOT trust the restart/ping as successful, (b) runs the
   documented investigation steps from `docs/keeper-restart-now-ack-protocol.md`
   §6 — check the fired command's exit code + stderr reason
   (`no_tmux_target` / `sid_not_primary` / `handoff_missing` / `handoff_stale` /
   `ack_inject_failed`), and (c) for a crew, the captain restarts the keeper
   (`harmonik keeper enable ...` / rebind) rather than the crew silently
   continuing to overflow.

---

## 4. Integration with the existing flow + the `ping` subcommand

### Contract changes (small, additive)

- **New subcommand** `harmonik keeper await-ack` (this design).
- **Skill prose** (`keeper`, `captain`, `crew-launch` skills) gains a short
  "verify the ACK" step that CALLS `await-ack`. No logic in prose.
- **No change** to `RestartNow` / `Ping` / `AckLine` / the existing CLI — the
  keeper->pane half stays exactly as landed in 89852bb3.

### Sequence — ping (self-service liveness, primary use)

```
Agent (live)                         keeper CLI proc            tmux pane
   |                                      |                         |
   |-- nonce = ping-<millis> ------------>|                         |
   |-- harmonik keeper ping --nonce N --->|                         |
   |                                      |-- inject AckLine(N,ping)-->  [KEEPER ACK N] received ping
   |                                      |-- exit 0 -------------->|   (line now in scrollback)
   |                                                                |
   |-- harmonik keeper await-ack --nonce N --timeout 15s ----------+
   |        (poll capture-pane every 1s)                           |
   |   sees "[KEEPER ACK N]" -> exit 0  ==> keeper ALIVE           |
   |   OR 15s elapse, no match -> emit session_keeper_ack_timeout, |
   |        exit 3  ==> agent: comms-alert operator + investigate  |
```

### Sequence — restart-now (delegated confirmation)

```
Firing agent            wrapper/peer watcher       keeper CLI proc        pane
   |                          |                          |                  |
   | /session-handoff (fresh) |                          |                  |
   |-- restart-now --agent X -+------------------------->|                  |
   |   (prints nonce=rn-...)  |   (wrapper captured it)  |                  |
   |                          |                          |- inject ACK ----->  [KEEPER ACK rn-...]
   |   <=== /clear wipes me ==|                          |- inject /clear -->
   |   (firing agent is gone) |                          |- inject /resume ->
   |                          |                          |- exit 0 -------->|
   |                          |-- await-ack --kind restart --nonce rn-... --timeout 30s
   |                          |   sees ACK -> exit 0 ==> keeper delivered   |
   |                          |   OR timeout -> event + exit 3 ==> alert    |
```

Key point reflected from the protocol doc: the ACK is injected **before** the
gated `/clear`, so even if a safety gate later blocks the destructive step,
liveness is still proven to the external watcher.

---

## 5. Test plan (deterministic)

All in `internal/keeper/awaitack_test.go` against an `AwaitAck(ctx, cfg)` core
that takes an injectable `PaneCapturer` and an injectable clock/`Now` (mirror the
`RestartNowConfig` pattern — `Now func() time.Time`, default `time.Now`).

1. **ACK present immediately** → fake capturer returns a buffer containing
   `[KEEPER ACK rn-123] received restart` on the first poll → `AwaitAck` returns
   nil, no timeout event.
2. **ACK appears on the 3rd poll** → fake capturer returns the line only after N
   calls (use a call counter) → returns nil; assert it polled ≥3 times.
3. **ACK never appears** → fake capturer always returns unrelated pane text →
   with a fake clock advanced past `timeout`, `AwaitAck` returns a timeout error
   AND emits exactly one `session_keeper_ack_timeout` event (assert via the test
   event emitter `em.EventsOfType(...)`, same pattern as the existing keeper
   cycle tests).
4. **Wrong nonce in pane** → buffer has `[KEEPER ACK rn-OTHER]` but not the
   requested nonce → treated as no-match → timeout path (proves nonce
   discrimination / no false positive across cycles).
5. **Capturer error** → fake returns an error every poll → bounded retries, then
   timeout-with-reason (not a panic); assert the error names the capture failure.
6. **Match includes scrollback tail** → ACK is in the `-S -200` region only →
   still matched (asserts the capture command requests scrollback).
7. **CLI exit-code mapping** (`cmd/harmonik` test): success→0, timeout→3,
   flag misuse / unknown flag→2, consistent with the existing keeper CLI tests.

No live tmux required for the core tests — the `PaneCapturer` seam removes it.
A single optional `//go:build integration` test can exercise the real
`tmux capture-pane` against a scratch session (mirrors the existing
integration-tagged injector tests), but it is NOT in the default `go test` run.

---

## 6. Conflict check vs hk-vpnp (the AUTOMATIC cycle bug) — DISTINCT, no collision

- **hk-vpnp** is the **automatic watcher** path: `MaybeRun`/`runCycle` in
  `cycle.go` / `watcher.go` loops on the ACT-when-idle threshold — injects
  `/session-handoff`, times out before `/clear` lands, re-fires with a new nonce,
  truncates the handoff to 0 lines. It is about the keeper DRIVING a cycle on its
  own timer and not confirming `/clear` landed before re-firing.
- **hk-uldg (this design)** is the **manual, agent-initiated** path
  (`restart-now` / `ping`) + a NEW out-of-process `await-ack` observer. It adds
  zero code to `cycle.go` / `watcher.go` and changes none of the automatic
  cycle's nonce/handoff logic.

**Non-collision guarantees:**
- New files only: `internal/keeper/awaitack.go` + `_test.go`, one new
  `core.EventType`, one new CLI subcommand. No edits to `cycle.go`, `watcher.go`,
  `restartnow.go`, or `injector.go`.
- Different nonce namespaces: automatic-cycle nonces (`-NNNNNN` suffix) vs
  manual `rn-<millis>` / `ping-<millis>`. `await-ack` matches an EXACT supplied
  nonce, so it can never accidentally match an automatic-cycle ACK.
- **Shared-insight, not shared-code:** both bugs are "fire-and-don't-confirm".
  hk-vpnp's eventual real fix (confirm `/clear` landed before re-firing) is
  conceptually the SAME confirm-before-proceed discipline this design adds for
  the manual path — but they are implemented in separate code paths and can land
  independently in either order. If hk-vpnp later wants to reuse a pane-confirm
  helper, the `PaneCapturer` + match logic from `awaitack.go` is a natural shared
  primitive to extract THEN — not a reason to couple them now.

---

## 7. Open questions for the operator

1. **restart-now watcher ownership.** Recommendation §1 is a launch-wrapper runs
   `await-ack --kind restart` as a detached process. Is that acceptable, or do
   you prefer the captain always be the restart-now confirmer for crews (and the
   captain's own restart-now goes unconfirmed except by you reading scrollback)?
2. **Default timeouts** — ping 15s / restart-now 30s. OK, or tune?
3. **comms escalation** stays in the skill (caller owns `--from <lane>`), keeping
   the binary identity-free. Confirm you don't want the binary itself to comms.
4. **New event name** `session_keeper_ack_timeout` — confirm naming fits the
   existing `session_keeper_*` family.
