# Design — session-keeper.md changes (K1–K5)

Codename: `2026-07-18-keeper-restart-delivery` · pass 4 (change-design)
Grounded by `03-research/{delivery-reachability,message-cluster}/findings.md` (all file:line
citations live there; this doc states the chosen design and points back).

## Design principle (why K1 is the spine)

The collision is structural, not a tuning bug: the warn nudge always fires the same
`tmux paste + 750ms settle + 3× Enter` once quiesced (`injector.go:144-184`,
`watcher.go:1424`), and the operator-attached guard only swaps the *text*, never suppresses
the paste (`watcher.go:1450-1451`). And the operator-present read **cannot be made accurate
from the keeper side** — tmux exposes no keystroke-recency finer than the 5-min
`client_activity` window, and remote/mobile input bypasses tmux entirely
(`tmuxresolve.go:180-186`). Therefore the fix is not "detect the operator better" — it is
**stop writing into the operator's pane at all** for leader sessions. Delivering the nudge
over comms is safe *even when the operator is present*, which dissolves the need for a
perfect attach read. K1 is the spine; K5 becomes best-effort.

## K1 — Delivery channel & reachability

**Delivery = a comms `agent_message`, sent by shelling `harmonik comms send`.** The keeper
cannot import the daemon (depguard `.golangci.yml:189`), so it sends the same way it drives
tmux — a subprocess: `harmonik comms send --from keeper --to <agent> --topic keeper -- <body>`.
The message reaches the agent as a *turn* only if the agent has `comms recv --follow` armed
(hk-b51bg); that is why reachability (G2) is a precondition, not an afterthought.

**Do NOT use `comms send --wake`.** `--wake` re-enters the exact pane-paste primitive
(`comms.go:451-471`) K1 exists to avoid (`03-research/delivery-reachability/findings.md` Q2).
The non-collision property comes from the *subscribe* path (no pane write), not from wake.

**Reachability check = presence-Online, read in-process.** The keeper IS depguard-allowed to
import `internal/presence` (`.golangci.yml:178`). A live `comms recv --follow` emits its own
presence refresh beat every 60s (`comms.go:1517,1662`), so *presence-Online (age < TTL 120s)*
is the one existing signal that tracks a live follower. The keeper reads it directly
(`presence.ComputePresenceRegistry` + `GetPresenceState`) on the warn tick.

**Delivery decision table (deterministic — SC-1, SC-2; no silent no-op):**

| Session role | Reachability (presence) | Delivery |
|---|---|---|
| Leader (captain/admiral) | Online (< 120s) | **comms** `agent_message` (defer message + self-restart command) — no pane write |
| Leader | Stale / Offline | **terminal fallback** = the *existing* warn path (`injectTextClocked`, operator-attached-gated text, retry loop preserved per NG3) |
| Crew | (unchanged this work) | existing behavior; crew message rides K4 config, default off (K7) |

**Known limitation, stated in the spec (not hidden):** presence-Online is *necessary but
not sufficient* — a bare `comms join` also shows Online with no reader attached, and a
follower's 60s beat can keep presence Online for a moment *after* a `/clear` before the new
session re-arms a reader (OQ-1). So the comms path can occasionally deliver to an unread
inbox. This is **non-catastrophic**: the unchanged FORCE-ACT / hard-ceiling ladder remains
the backstop — an unread nudge means the session eventually hits the force ceiling and is
cut, exactly as today. The spec records presence-Online as the v1 reachability definition and
names a sharper "recv-follow-armed" signal (a distinct beat reason, or a daemon subscriber
list) as a **future enhancement**, not a v1 requirement.

## K2 — Deferral framing & the good-stopping-point contract

The comms message body is a normative template with four required structural elements
(prose tunable per K4):
1. **Defer condition A:** if mid-conversation with the operator, finish the exchange first.
2. **Defer condition B:** if mid-task, finish the unit first.
3. **The good-stopping-point self-test** (from `01-problem-space.md` Constraints / Q3): a
   good stop is one where nothing needed to continue lives only in context — (i) between
   discrete units; (ii) in-flight work saved/re-derivable; (iii) no unanswered operator
   question held; (iv) next session resumes from handoff + substrate with no redo.
4. **The self-restart command** (K3), carrying the cycle nonce.

**The deferral is legitimized only because the backstop is unchanged.** Telling the agent
"take your time" is safe *because* FORCE-ACT (`step.go:872-876`, `aboveForceThreshold`) still
cuts a never-idle session unconditionally and the 280k hard ceiling
(`thresholds.go:95`) still trips SID-independently. NG1/SC-9: zero threshold changes.

## K3 — Agent-run self-restart as the default payload

**The primitive exists and is agent-invocable today**: `harmonik keeper restart-now
--agent <name>` (`keeper_cmd.go:791` → `restartnow.go:69`) runs a fully synchronous
verify → freshness-check → ACK → `/clear` → brief in its own process, **wholly independent of
the cycle's 300s timer** (`restartnow.go:13-29`). This is precisely what closes the
timeout gap (`step.go:349,872-881`): an agent that finishes at T+301s, writes its handoff,
then runs the command still restarts cleanly — because restart-now never consulted the
already-aborted cycle timer (SC-4).

**Net-new work (the one real gap — corrects the plan's nonce claim):** the keeper's cycle
nonce does NOT flow into restart-now today. Two disjoint schemes exist — the auto-cycle
`cyc-...` KEEPER marker (`cycle.go:491`) and restart-now's `rn-<ms>` echo nonce
(`keeper_cmd.go:880`, never validated) — and **restart-now has no `--nonce` flag**. So G4
requires:
- **Add `--nonce <id>` to `restart-now`** (copy `ping`'s existing `--nonce`, `keeper_cmd.go:833`).
- **`RestartNow` records the nonce on its emitted events / journal** (attribution), so the
  self-restart is traceable to the keeper's cycle in `events.jsonl`. **v1 = carry-for-audit,
  not hard-validate** — hard validation would require the separate restart-now process to
  know the keeper's live cycle id, which it does not hold; attribution is sufficient and
  matches how `ping` already treats its nonce.
- **Provenance channel:** keeper mints `cyc-id` at cycle entry → the K2 comms message embeds
  it verbatim in the `restart-now --nonce <cyc-id>` command string → agent runs it verbatim.
  Clean, no shared state.

Note: restart-now writes to the agent's *own* pane at an agent-chosen moment
(`restartnow.go:34,76`) — that is compatible with G1 (it is not an operator-typing
collision; the agent chose the moment).

## K4 — Configurable message text

**Home already exists and is already wired:** `.harmonik/config.yaml` →
`keeper.warn_messages.{default_warn_text,actionable_warn_text}` (`projectconfig.go:335-386`,
threaded to `WatcherConfig`, `watcher.go:380-401`). Editing YAML needs **no rebuild** — G8's
core requirement is met today for the two existing texts. K4 extends this same block with the
new leader defer-message and (K7) crew-message keys.

**Structure-normative / prose-tunable (SC-6b)** is *partially realized already*:
`containsRestartNowCmd` (`watcher.go:893`) rejects a custom actionable text that drops the
`harmonik keeper restart-now` command and falls back to the compiled default. Design: **extend
this validation** to the other required structural elements — cleaner still, **template the
four K2 slots** (defer-A, defer-B, stopping-point test, restart-now+nonce command) the way
`restartNowCmdToken` is templated (`injector.go:23,42-48`), so an override fills only the
prose around fixed slots and cannot silently drop a load-bearing element.

**"On the fly" (the operator's explicit word):** config is read **once at keeper startup**
(`keeper_cmd.go:272`), so today an edit needs a keeper bounce (not a rebuild). To honor "edit
on the fly to improve the wording," **add a mtime-gated per-tick re-read of just the
`warn_messages` block** (cheap: stat the config file each poll, re-parse only that sub-block
on change). This is net-new but small and scoped to `warn_messages` — thresholds and
self-service stay startup-bound (no live-reload of anything load-bearing). Strict unknown-key
validation (`ErrUnknownConfigKey`, `projectconfig.go:113`) still applies to the re-read.

## K5 — Situational-read sharpening (best-effort; the honest scope)

**Grounded constraint:** no sharper operator-present signal is reachable without a Claude
Code / hook-bridge change (`03-research/delivery-reachability/findings.md` Q4). So K5 does NOT
promise to close remote/mobile blindness. What K5 delivers here:
- **In-cycle TOCTOU re-check.** Today operator-attached is sampled once at cycle entry
  (`ports.go:190`) and not re-checked across the up-to-300s wait (SK-011/SK-017). Re-sample
  operator-attached during the wait so an operator who starts typing *after* entry is
  respected. (Relevant to the terminal-fallback path; on the comms path a present operator is
  already harmless.)
- **Reachability/liveness pre-check** feeds the K1 decision (already specified above): a
  Stale/Offline target routes to terminal fallback rather than firing a comms message into a
  dead inbox.
- **Named external dependency (OQ-3):** a hook-bridge keystroke/"operator-actively-here"
  signal is the only true fix for remote/mobile detection. The spec records it as an external
  dependency on claude-hook-bridge.md, **out of scope to implement in this work**. K1 makes
  its absence non-fatal.

## Open questions carried to spec-draft

- **OQ-1** (runtime): does a follower subprocess survive `/clear`, and for how long can
  presence overstate reachability post-restart? Determines whether the spec needs a
  post-restart presence-settle delay before trusting Online.
- **OQ-2** — resolved: `--nonce` is net-new; carry-for-audit chosen.
- **OQ-3** — hook-bridge keystroke signal declared external; not scoped here.
