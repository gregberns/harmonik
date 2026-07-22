# Components (Affected Spec Areas)

Codename: `2026-07-18-keeper-restart-delivery` · pass 2 (decompose)
Goals G1–G8 from `01-problem-space.md`.

## Affected Existing Specs

### session-keeper.md  (the core — most requirements land here)

- **Change summary:** Redesign the leader-session nudge: deliver over comms (not a terminal
  paste), verify reachability first, frame it to defer to a real pause, make the agent-run
  restart command the default payload, and externalize the message text as tunable config.
  Plus sharpen the keeper's situational read.
- **Requirements** (grouped; each group is a coherent design unit):
  - **K1 — Delivery channel & reachability (G1, G2).** For a leader session, the nudge is an
    `agent_message` on the comms bus. Before choosing comms the keeper runs a named
    reachability check (is the target's `comms recv --follow` armed / inbox live?). Delivery
    channel is a deterministic function of that check with a defined terminal fallback; no
    branch is a silent no-op.
  - **K2 — Deferral framing & good-stopping-point contract (G3).** The message instructs the
    agent to hold if (a) mid-conversation with the operator or (b) mid-task, and to hand off
    at the next good stopping point, defined by the concrete Q3 self-test. The deferral sits
    under the unchanged FORCE-ACT backstop (never-idle sessions still get cut).
  - **K3 — Agent-run self-restart as the default payload (G4).** Every nudge carries the
    command the agent runs to trigger its own handoff-confirm → clear → reboot, carrying the
    keeper's cycle nonce. The restart completes independent of the keeper's 300 s watch
    window (a handoff written at T+301 s still restarts cleanly). Built on `restartnow.go`.
  - **K4 — Configurable message text (G8).** All nudge wording lives in external config the
    operator edits without a rebuild. The four structural elements (defer conditions,
    stopping-point test, restart command, nonce) are normative; the prose is tunable. The
    crew message (K7) is delivered through this same mechanism, defaulted off.
  - **K5 — Situational-read sharpening (G5).** Replace/augment the 5-minute `client_activity`
    operator-present signal so a slow-typing or remote/mobile operator is not misread as
    absent; add a reachability/liveness pre-check before a cycle fires so a dead-watcher /
    unreachable target defers instead of firing into the void.
- **Dependencies:** K1 depends on agent-input.md (comms as delivery substrate). K2/K3/K4 are
  one message cluster (share the message template). K5 is independent of the message but
  feeds the K1 delivery decision.

### agent-input.md  (comms as the keeper's delivery substrate)

- **Change summary:** Document that the keeper is now a producer on the comms/agent-input
  surface (it was not before), and the reachability signal a producer can rely on.
- **Requirements:** K1's reachability check reads whatever agent-input/comms exposes about a
  live armed inbox; if nothing adequate exists, define the minimal signal. Keep this thin —
  the keeper is a new *caller*, not a redesign of the bus.
- **Dependencies:** none upstream; K1 in session-keeper.md consumes it.

### scenario-harness.md  (testing — operator-mandated, not optional)

- **Change summary:** Add the scenario/integration/e2e coverage this work requires, using the
  twin (`cmd/harmonik-twin-claude`) per the parity audit (`docs/twin-parity-audit-2026-05-14.md`).
- **Requirements (K6, G6):** a named test at the stated level for each failure, each failing
  before and passing after — (a) operator-typing collision; (b) late-handoff-after-300 s;
  (c) comms-unreachable fallback; (d) operator-present misread; (e) FORCE-ACT still cuts a
  never-idle session.
- **Dependencies:** exercises the behaviors specified in session-keeper.md K1–K5.

### crew-handoff-schema.md / park-resume-protocol.md  (crew extension — decision only)

- **Change summary:** Record the disposition for extending the finish-then-self-restart
  message to crews.
- **Requirements (K7, G7):** written disposition — operator direction is **defer the crew
  message but deliver it through K4's config (default off) so it can be turned on and tuned
  on the fly**, gated on the `keeper-reliability` beads (esp. hk-220lv dead-watcher, hk-4tjyj
  discarded-handoff) landing first. No crew-side implementation in this work beyond the
  config hook.
- **Dependencies:** K4 (config mechanism); external dep on the `keeper-reliability` bug track.

## New Specs

None. All changes extend existing specs.

## Dependency Map

```
agent-input.md (reachability signal)
        │
        ▼
session-keeper.md
   K1 delivery+reachability ──┐
   K2 deferral framing        ├─ message cluster (K2+K3+K4 share the template)
   K3 self-restart default    │
   K4 configurable text ──────┘──► K7 crew disposition (config hook, default off)
   K5 situational read ──► feeds K1's delivery decision
        │
        ▼
scenario-harness.md  K6 (tests every K1–K5 behavior + the FORCE-ACT backstop)
```

Ordering: agent-input reachability signal → session-keeper K1–K5 → scenario-harness K6.
K4 config lands before K7 (crew message rides the config). External `keeper-reliability`
beads gate K7's *activation*, not this work's landing.

## Goal → Area Traceability

| Goal | Component | Spec area |
|---|---|---|
| G1 deliver over comms | K1 | session-keeper.md, agent-input.md |
| G2 verify reachable | K1 | session-keeper.md, agent-input.md |
| G3 defer to a pause | K2 | session-keeper.md |
| G4 self-restart command | K3 | session-keeper.md |
| G5 sharpen read | K5 | session-keeper.md |
| G6 real testing | K6 | scenario-harness.md |
| G7 crew disposition | K7 | crew-handoff-schema.md / park-resume-protocol.md |
| G8 configurable text | K4 | session-keeper.md |

## Research targets for pass 3 (per cluster)

- **R-A (K1+K5+agent-input):** how the keeper delivers today (`injector.go`, `watcher.go`,
  `cycle.go`), how comms delivery works (`cmd/harmonik/comms.go`, `commsInjectTmuxPane`), and
  what reachability/liveness signal exists (`keeper doctor`, presence, `comms who`).
- **R-B (K2+K3+K4 message cluster):** `restartnow.go` invocation + nonce provenance; where
  warn/restart text is selected today (`selectWarnText`, `ActionableWarnText`); any existing
  externalized-config surface the keeper reads.
- **R-C (K6 testing):** the twin's reachable paths (parity audit), existing keeper scenario
  tests, how to script an operator-typing collision and a late-handoff in the harness.
