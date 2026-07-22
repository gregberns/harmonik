# R-A — Keeper nudge DELIVERY and agent REACHABILITY (code grounding)

Cluster: **R-A (K1 + K5 + agent-input)**. Read-only code grounding for pass-3
research feeding session-keeper.md K1/K5 and agent-input.md. Every claim is cited to
`file:line`. Plan confirmed/corrected: `plans/2026-07-17-keeper-restart-timing/_plan.md`.

Bottom line: the plan's grounded map is accurate on all four fronts. The two most
consequential NEW facts it omits: (a) the keeper package is already depguard-allowed
to import `internal/presence` (`.golangci.yml:178`), so a reachability read is
in-process; and (b) `comms recv --follow` drives its own presence refresh beat every
60 s (`cmd/harmonik/comms.go:1517,1662`), making presence-Online the only existing
proxy for "inbox armed" — a proxy, not a proof.

---

## Q1 — How the keeper delivers today

Finding: the warn nudge is a tmux paste + 750ms settle + 3× Enter, fired
unconditionally once quiesced.

- `injectTextClocked` (`internal/keeper/injector.go:144-184`): `load-buffer -b
  hk-keeper-inject -` (:154) → `paste-buffer … -t <target> -d` (:158) →
  `clock.Sleep(submitSettle)` (:164) → first `sendEnter` (:169) → loop of
  `submitRetries` more Enters `submitRetryDelay` apart (:176-181). Constants:
  submitSettle=750ms (:84), submitRetries=2 (:90), submitRetryDelay=400ms (:95). Three
  Enters over ~1.55s; `sendEnter` is a real key event, not bracketed paste (:188-193).
  Confirms plan `_plan.md:78-89`.
- No line-clear on the warn path: `injectTextClocked` never issues C-u/C-c.
  `SendEscapeKey` (`injector.go:200-209`) is called ONLY by the reset cycle before
  `/session-handoff`, never on warn-inject. Confirms `_plan.md:85-88`.
- Warn delivery in watcher: `watcher.go:1424-1459`, guard `if pendingInject &&
  gaugeQuiesced` (:1424). Only preconditions are a warn crossing + pane quiesce, then a
  sleeping gate (:1428). gaugeQuiesced is a turn/render quiesce, not a task boundary.
- Operator-attached guard does NOT suppress the paste — only picks the text.
  `operatorAttached := OperatorAttachedFn(TmuxTarget)` (`watcher.go:1450`) →
  `selectWarnText(ctxFile, crispIdle, operatorAttached)` (:1451). In `selectWarnText`
  (`watcher.go:867-879`) an attached operator only falls through to the lighter
  advisory TEXT; a warn is still pasted. Comment: "The warn is still delivered; only
  the self-restart command is withheld" (`watcher.go:1446-1448`). Confirms C4 mechanism
  (`_plan.md:135-139`, problem-space G1).
- Reset cycle + 300s timeout: `DefaultHandoffTimeout = 300 * time.Second`
  (`internal/keeper/thresholds.go:157`), bound in applyDefaults (`cycle.go:340-341`;
  field doc `cycle.go:68`). AwaitingHandoff arms it; fire-with-no-fresh-handoff →
  Aborted. Confirms `_plan.md:117`.

Design implication: the leader nudge must move OFF `injectTextClocked` — the collision
is structural (paste-at-cursor + 3× Enter, no clear, guard only swaps text). K1 must
specify the leader nudge as an `agent_message`; the terminal-fallback branch is the
only path that may still call `injectTextClocked`, and it must preserve the
settle+retry loop (NG3, hk-89g, `injector.go:79-95`). The 300s timeout
(`thresholds.go:157`) is why K3's agent-run restart must complete independent of the
watch window (SC-4).

---

## Q2 — How comms delivery works

Finding: `comms send` mints an F-class `agent_message` on the daemon bus; delivery to
the agent as a TURN is a separate subscribe/recv concern.

- CLI send (`cmd/harmonik/comms.go:300-357`) dials the daemon socket, writes a
  `comms-send` request, prints the event_id. It does NOT deliver to the recipient pane
  except the optional `--wake` tail (:349-355).
- Daemon handler `commsSend` (`internal/daemon/socketdispatch.go:154`, registered :418)
  → `HandleCommsSend` (`internal/daemon/commshandler_nbrmf.go:140`) emits an
  `agent_message` event via the bus (`commshandler_nbrmf.go:6-8,101-160`), F-class
  fsync-before-return. The daemon never pushes into the recipient process.
- Becomes a TURN via subscribe: `comms recv --follow` opens a `subscribe` op
  (types:["agent_message"], to:<agent>) and streams live messages
  (`comms.go:1624-1720`, body :1687-1699). Per hk-b51bg this follower must be ARMED via
  the Monitor tool for messages to surface as a turn. Absent an armed follower the
  message is durable-but-unread until a `comms recv` drain or a `--wake` nudge.
- `--wake` (`commsWakePaneForAgent` `comms.go:429-441` → `commsInjectTmuxPane`
  :451-471) is the SAME tmux paste primitive: load-buffer → paste-buffer -d → ONE
  send-keys Enter (:465). Same operator-collision hazard as the keeper, minus the 2
  retries. Corroborates `_plan.md:95-109`: comms is not mechanically less disruptive at
  the tmux layer. The non-collision property of comms comes from the SUBSCRIBE path (no
  pane write at all), not from `--wake`.

Design implication: K1's "deliver as an `agent_message`" = delivery to the durable bus,
which reaches the agent as a turn ONLY if `comms recv --follow` is armed (this is why
G2/SC-2 exists). agent-input.md must record the keeper as a NEW producer that shells
`harmonik comms send` (keeper cannot import daemon — see Q3). Spec must NOT use `--wake`
as the leader delivery mechanism: it re-introduces the exact pane-paste collision K1
removes.

---

## Q3 — Reachability / liveness signals available

Finding: presence (`comms who`) is the only existing "on the bus" signal, driven by the
recv-follow refresh beat, and the keeper can read it IN-PROCESS.

- Windows: `presence.TTL = 120s`, `presence.StaleCutoff = 10m`
  (`internal/presence/presence.go:48,58`). GetPresenceState → Online [0,120s), Stale
  [120s,10m), Offline ≥10m or explicit leave (`presence.go:96-135`). `comms who` prints
  Online+Stale over events.jsonl, no daemon needed (`comms.go:1062-1063,1106-1126`).
- What feeds Online: `agent_presence` beats reason in join/refresh/leave
  (`comms.go:506-507`). Critically `comms recv --follow` emits its OWN refresh beat
  every 60s on an independent timer: `commsFollowPresenceBeatInterval = 60s`
  (`comms.go:1517`, ticked :1662), and a leave beat on signal teardown
  (`comms.go:1650-1657`, guarded on sigCtx.Err so a park-exit does not falsely leave).
  A live recv --follow ⇒ Online within 120s; a dead follower ages to Stale within 120s.
  Closest existing thing to an "inbox-armed" signal.
- BUT it is a PROXY, not proof. A bare `comms join` (or the subscribe heartbeat) also
  produces Online with no reader attached. Nothing distinguishes a recv-follow refresh
  beat from a stale join beat. No dedicated "recv --follow armed" signal exists anywhere
  the keeper can read. G2 must either accept presence-Online-within-TTL as "reachable"
  (owning the residual false-positive) or DEFINE a new sharper signal.
- `keeper doctor` has NO comms/reachability check. Checks
  (`cmd/harmonik/keeper_enable_doctor_cmd.go:504-762`) are all local: config, binary,
  statusLine/Stop/PreCompact/SessionStart hooks, gauge .ctx, .sid, .idle, .managed,
  live-watcher (flock via keeper.LiveKeeperPresent :707-720), tmux-pane, api-key. None
  consults presence/comms. Confirms `_plan.md:155-157`. live-watcher answers "is a
  keeper watcher alive," not "is the agent reachable on comms."
- Depguard: keeper CAN read presence in-process; CANNOT import daemon or send comms
  directly. `.golangci.yml:172-189` allows internal/keeper → $gostd, core, eventbus,
  internal/presence (:178), dashboard, digest, substrate; DENIES internal/daemon
  (:189) and internal/workloop (:190). So a reachability READ is a direct
  presence.ComputePresenceRegistry + GetPresenceState call in the watch tick (depguard
  comment :167-171 names presence as a leaf the tick may consume). To SEND, the keeper
  must shell out to `harmonik comms send` (same exec pattern it uses for tmux), because
  comms-send goes through the daemon socket and the keeper holds no daemon handle.

Design implication: an existing signal exists but is a proxy. K1/K5's reachability check
= agent presence-Online (age < presence.TTL=120s) read in-process via internal/presence.
SC-2 fallback: when NOT Online (Stale/Offline), fall back to the terminal path
(operator-attached-gated paste), never emit into the void. agent-input.md should note
presence-Online is NECESSARY-but-not-SUFFICIENT for an armed recv --follow; proof-of-
armed-inbox requires DEFINING a new signal (a beat-reason distinguishing recv-follow
refresh from a bare join, or a daemon subscriber list) — none exists today. Sending is a
`harmonik comms send` subprocess, not a library call (depguard :189).

Carries OQ-1: the follower is a background subprocess armed by Monitor; a keeper `/clear`
resets Claude context but does not necessarily kill that subprocess, and its 60s refresh
beat is independent of the Claude context — so presence can stay Online across a `/clear`
while the NEW session has not re-armed a reader. presence-Online can momentarily
OVERSTATE reachability right after a restart. Code alone cannot settle whether the
subprocess survives `/clear`; flag for runtime confirmation (OQ-1).

---

## Q4 — Operator-present detection (K5)

Finding: the 5-minute client_activity window misreads exactly as the plan claims, and no
sharper signal is reachable without a Claude Code change.

- Mechanism: `OperatorAttached(target)` runs `tmux list-clients -t <target> -F
  '#{client_activity}'` and returns true iff any client's last activity is within
  `operatorActiveWindow = 5 * time.Minute` (`internal/keeper/tmuxresolve.go:186,
  214-227`); pure comparator `operatorActiveSince` (:233-248). Non-zero exit → fail-open
  NOT-attached (:222-224).
- Misread 1 — slow local typist reads as unattached. client_activity advances only on
  keystrokes through the tmux client; the doc-comment admits the window governs only the
  local-typist case (:181-186), so a local operator pausing >5min between keystrokes
  reads unattached. Confirms `_plan.md:145-148`.
- Misread 2 — remote/mobile NEVER advances client_activity. Comment is explicit: the
  remote-control/iOS channel "input reaches Claude directly and NEVER passes through the
  tmux client, so that client's #{client_activity} is frozen at attach time even while
  the operator drives the session" (`tmuxresolve.go:180-185`). Treated unattached BY
  DESIGN (that was hk-0t5s's intent — lift the permanent warn-only suppression the bare
  any-client probe imposed under the mobile workflow, :200-205). Confirms
  `_plan.md:148-151`: closing remote/mobile blindness is in tension with the fix that
  created the 5-min window.
- TOCTOU on ACT: operator-attached is sampled once per tick into the GateSnapshot
  (`ports.go:190`), gate evaluated only in Idle (Gate 7 `step.go:496-503`; precompact
  Gate 5 `step.go:678-682`, both blocked("operator_attached")). Per SK-011 the ladder
  runs only in Idle and per SK-017 InCycle suppression parks ticks once off-Idle — so
  across the up-to-300s handoff wait attach is NOT re-checked. Confirms
  `_plan.md:149-151`; the cite cycle.go:716 / ports.go:190 is correct (ports.go:190 is
  the sample site).
- Sharper signal without a Claude Code change? No. tmux exposes only list-clients format
  vars — #{client_activity} (already used) and #{client_flags}/#{client_last_session}
  (no finer keystroke recency); no per-keystroke stream, and the remote channel bypasses
  tmux entirely. A real "operator actively here" signal must come from Claude Code / the
  hook bridge (e.g. surfacing idle_prompt/keystroke) — a Claude Code change. The
  transcript path the keeper reads (`recentTranscriptTurn` `tmuxresolve.go:267-352`, used
  by Gate 5d/5e LastUserTurnAt/LastAssistantTurnAt) captures SUBMITTED turns, not
  in-progress typing — cannot see a half-composed unsent message (the C4 case).

Design implication: K5 must name the failure — the 5-min client_activity window
(`tmuxresolve.go:186`) misreads (i) a slow local typist and (ii) a remote/mobile operator
as absent — and state its replacement/augment. Grounded constraint for SC-5: no sharper
operator-present signal is available without a Claude Code / hook-bridge change (tmux
exposes only client_activity; the remote channel bypasses tmux). So K5 cannot fully close
remote/mobile blindness from the keeper side alone. Honest options: (a) keep/widen the
heuristic and lean on comms delivery (K1) so a misread no longer causes a COLLISION —
delivering over comms is safe even when the operator IS present, dissolving the need for
a perfect attach read on the leader path; or (b) declare a hook-bridge keystroke signal as
an external dependency (matches OQ-3). Moving the leader nudge to comms (K1) is the
structural fix that makes attach-detection imperfection non-fatal — the TOCTOU and 5-min
window only hurt because delivery is a pane paste.

---

## Corrections / caveats to the plan (all refinements, not corrections)

1. `_plan.md:90-94` — three inject paths use the same primitive with distinct buffers:
   confirmed (keeper `hk-keeper-inject` `injector.go:152`; comms `hk-comms-wake`
   `comms.go:452`; legacy keeper-warn const `hk-keeper-warn` `injector.go:77`, unused by
   the active path). Nuance the plan understates: comms `--wake` sends ONE Enter
   (`comms.go:465`) with NO settle and NO retries — so `--wake` is LESS reliable at
   submitting (the hk-89g dropped-Enter risk), not just less disruptive.
2. NEW: keeper is already depguard-allowed to import internal/presence
   (`.golangci.yml:178`) — the G2 reachability READ is in-process, no subprocess. Only
   the comms SEND needs a `harmonik comms send` subprocess (daemon denied :189).
3. NEW: `comms recv --follow` is itself a presence PRODUCER (60s refresh beat,
   leave-on-teardown; `comms.go:1517,1650-1666`) — this is what makes presence-Online a
   usable-if-imperfect reachability proxy and is the detail G2's check should build on.
4. restartnow.go grounding for K3 (adjacent cluster R-B, noted for completeness):
   `RestartNow` (`internal/keeper/restartnow.go:69-151`) is synchronous, runs in the
   `harmonik keeper restart-now` process, verifies SID is a primary UUIDv4 (:100-104),
   does ONE handoff-freshness check (HandoffFreshnessWindow=10m :43,117-123), injects
   ACK → /clear → brief. Independent of the watcher's 300s window — the mechanism K3/G4
   wires in as the default payload.
