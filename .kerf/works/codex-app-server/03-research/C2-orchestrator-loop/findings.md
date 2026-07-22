# C2 — The Harmonik Orchestrator Loop Contract

**Research question:** Enumerate precisely what a harmonik crew/captain orchestrator DOES over its lifetime that any substrate must support, and separate what depends on the Claude `--remote-control` live-paste channel from what is substrate-neutral (works via CLI/daemon regardless of harness).

**Scope note:** "Orchestrator" here = a long-lived crew or captain LLM session — a resident process that dispatches work to the daemon and coordinates over comms. This is distinct from the ephemeral per-bead worker the daemon spawns to execute a single bead end-to-end. The worker is stateless and short-lived; the orchestrator is stateful and persistent. C2's substrate question is about the *resident orchestrator*.

---

## 1. How the orchestrator is launched today (the `--remote-control` substrate)

`internal/daemon/crewlaunchspec.go:92` `buildCrewLaunchSpec` builds the argv/env for a persistent crew session. The launch shape is:

```
argv = claude --dangerously-skip-permissions --remote-control "<label>" --session-id <uuid> [--model <m>]
env  = HARMONIK_AGENT=<name>, HARMONIK_PROJECT=<projectDir>
WorkDir = projectDir, Role = "crew"
```

Key properties (all cited to `crewlaunchspec.go`):

- **`--remote-control "<label>"`** (line 108–114): registers the session in Claude Code's global-per-host Remote-Control picker. The label is `JoinRemoteControlName(rcPrefix, name)` (line 31–36) and is explicitly **COSMETIC** — "it disambiguates the global-per-host Remote-Control session picker across concurrent projects" (line 27–30). Harmonik's real identity keys (`HARMONIK_AGENT`, crew-registry name, tmux name, `--session-id`) stay bare and MUST NOT be derived from the label (line 28–30, hk-igpg). **This is the live-paste channel:** it is what makes the running Claude REPL externally addressable so the daemon/captain can inject keystrokes into its pane.
- **`--session-id <uuid>`** (line 114): a caller-minted UUID, also written to the crew registry. `--resume <uuid>` is used instead on stale re-launch (line 111–112) so a keeper `/clear`→resume continues the SAME conversation without forking a new session. This is a Claude-Code-specific durable-conversation primitive.
- **`--dangerously-skip-permissions`** (line 84–86): required so the session doesn't wedge on mid-loop permission prompts (e.g. python3 monitor scripts) that would otherwise need a human/captain to approve via tmux.
- **`--model`** (line 120–122): optional per-crew model pin from the mission `model:` front-matter.

**Boot seed injection** (`cmd/harmonik/crew.go:200–235`): after the crew-start RPC succeeds, the launcher pastes a boot seed into the crew's pane via `adapter.WriteToPane` (tmux `load-buffer` + `paste-buffer`, i.e. bracketed-paste), preceded by a splash-dismiss Enter (`SendKeysEnter`) and a 750 ms settle (`crewBriefSeedDelay`, line 198). The seed text is literally:

```
Please run `harmonik agent brief` and begin your operating loop.
```

So the orchestrator's *first instruction* is delivered by pasting into a live REPL — this is a `--remote-control`/tmux-pane operation, not a CLI call. The captain path mirrors this (`captain.go` PasteSeedToAgentPane, T10/hk-ncg9m).

---

## 2. The orchestrator lifetime loop — enumerated, with substrate dependency

Sources: `.claude/skills/crew-launch/SKILL.md` (crew), captain skill + STARTUP, `harmonik comms/queue/subscribe --help`.

For each item: **[LIVE-PASTE]** = depends on the Claude `--remote-control` live keystroke-injection channel; **[NEUTRAL]** = substrate-neutral, works over the daemon Unix-socket CLI regardless of harness.

### (1) Boot / identity establishment
- Confirm `$HARMONIK_AGENT == crew_name`; parse mission handoff frontmatter (`crew-launch/SKILL.md` Steps 1–2, lines 85–123). Mission file = `.harmonik/crew/missions/<crew>.md`.
- **The trigger to start booting is a pasted seed** (§1 above) — **[LIVE-PASTE]**.
- Reading the mission file, checking env, and the one-call `scripts/crew-boot-digest.sh` discovery (lines 76–83) are **[NEUTRAL]** — plain filesystem/CLI.

### (2) Comms join + presence refresh
- `harmonik comms join` (Step 3, lines 126–132) emits `agent_presence{online, reason:"join"}` so `comms who` shows the agent. **[NEUTRAL]** — daemon RPC, exit 17 if daemon down.
- **Presence refresh cadence:** presence ages out at ~120s if the session crashes without `comms leave` (crew-launch line 528; memory `comms_presence_refresh_120s`). Staying "online" requires periodic re-beats — driven by ongoing comms traffic / the loop, **[NEUTRAL]** but the orchestrator must actively keep emitting.
- Clean shutdown: `harmonik comms leave` emits `offline` (lines 522–530). **[NEUTRAL]**.

### (3) Assignment mirror to beads (attribution — load-bearing Gap 1)
- `br update <epic_id> --assignee <crew_name>` on EVERY epic adoption, boot AND every re-task (Step 4, lines 133–165). This is the captain's attribution source for all run events. **[NEUTRAL]** — a `br` metadata write.

### (4) Subscribe to comms inbox (the wake channel)
- `harmonik comms recv --follow --json` kept running for the whole session (Step 5 / § lines 177–239). Delivers directed + broadcast messages; at-least-once (N3); **dedupe on `event_id`** (NORMATIVE). **[NEUTRAL]** — daemon subscribe transport, anchored at the durable cursor (`comms recv --help`).
- **CRITICAL nuance (the live-paste dependency in disguise):** an armed `--follow` stream lets the session *receive* a message, but a fully idle Claude pane does **not reliably wake/process** a delivered message on its own (lines 196–239). Waking requires EITHER the armed stream re-invoking a turn OR a **pane nudge** — see (7).

### (5) Named-queue submit / arm dispatch monitor
- Find ready beads: `br list --label codename:<epic> ∩ br ready --limit 0` (lines 314–331). **[NEUTRAL]**.
- `harmonik queue submit --queue <queue> --beads ...` — HARD RULE: crew submits to its OWN named queue, never `main` (lines 334–345). `queue submit --help`: absent `--queue` defaults to `main`; queues auto-create on first submit. **[NEUTRAL]**.
- `harmonik subscribe --types run_completed,run_failed,run_stale,heartbeat --heartbeat 60s --json` to watch its queue drain (lines 348–359). **[NEUTRAL]** — Unix-socket event stream, `--follow` auto-reconnects across daemon restart with cursor resume (`subscribe --help`).
- `harmonik queue append` to add beads to a live run (`queue --help`). **[NEUTRAL]**.

### (6) Event-wake → react
- On `run_completed`/`run_failed` events from the subscribe stream: post status, submit next batch, classify + re-submit-once-if-transient failures (lines 361–382). **[NEUTRAL]** at the CLI layer — but the orchestrator must be *processing the stream*, which for a Claude session means the stream keeps re-invoking turns. On a non-Claude substrate this is just a read-loop.
- The daemon owns terminal bead transitions; the orchestrator must NOT `br close`/`claim`/`reopen` (lines 361–366, 560–564). **[NEUTRAL]** (a discipline constraint, not a capability).

### (7) Re-task mid-session  ← the hard-to-replicate one (see §3)
- Captain sends `harmonik comms send --to <crew> --topic assign --wake -- <new epic>` (crew-launch lines 233–250; message-handling table line 245). Adopting a re-task = update working `epic_id`, re-run the `--assignee` mirror, begin dispatching new epic's beads.
- The comms *delivery* is **[NEUTRAL]**. The **`--wake` pane nudge is [LIVE-PASTE]** — see §3.

### (8) Progress-feed posting (mandatory, dual-surface)
- Surface 1: `harmonik comms send --to <STATUS_TARGET> --topic status -- <update>` (lines 405–427). **[NEUTRAL]**.
- Surface 2: `br comments add <epic_id> "<update>"` (lines 429–436). **[NEUTRAL]**.
- Four cadence triggers: boot, bead-close, ≤10-min timer while dispatching / ≤15-min idle-or-draining, drain (lines 438–463). The **timer** requires the orchestrator to self-schedule wakeups — on Claude this is a turn-driven timer loop; on any substrate it's a periodic tick. **[NEUTRAL]** capability, but requires the substrate to support self-timed action.

### (9) Drain / park / idle-down
- Drain: exhaust ready beads → post drain status → idle on comms inbox, keep `--follow` armed (lines 376–396). **[NEUTRAL]** except idle-wake (below).
- Park: on a `comms recv --follow` delivery of `{topic:"park", from:"daemon"}` followed by exit code 0, QUIESCE all loops — stop subscribe, do NOT re-arm `--follow`, pause the timer, **await a pane nudge** (§ Park/wake, lines 254–306). **WAKE is [LIVE-PASTE]:** "The daemon will inject Enter into your pane when new work arrives" (lines 269–271, 286–288). On wake, re-run the full boot sequence.
- Clean shutdown / `crew stop`: `comms leave` + final status (lines 520–530). Captain may also `harmonik crew stop <name>` to kill the pane entirely (lines 300–304). **[NEUTRAL]**.

### (10) Keeper self-restart (context-fill cycle)
- The keeper watches context fill and drives handoff → `/clear` → `/session-resume` on the SAME `session_id` (lines 467–517). `/clear` and `/session-resume` are Claude-Code REPL slash-commands injected into the pane. **[LIVE-PASTE]** and Claude-Code-specific. In-flight work is not lost because the named queue keeps draining on the daemon independent of the session (lines 484–486). Restart re-hydration (re-read mission, re-join, re-arm `--follow`) is **[NEUTRAL]**.

---

## 3. How an orchestrator is RE-TASKED mid-session today (the hardest thing to replicate)

Two-part mechanism — a durable channel plus a keystroke-injection nudge:

**Part A — durable message (substrate-neutral):**
The captain calls `harmonik comms send --to <crew> --topic assign -- <new epic_id/instructions>`. This lands in the daemon's durable event log (`events.jsonl`) and is delivered at-least-once to the crew's `comms recv` cursor. If the crew has an armed `comms recv --follow --json` stream, the message is delivered to the stream.

**Part B — the pane nudge (LIVE-PASTE, Claude-specific):**
A delivered message is **not sufficient** to make an *idle* Claude pane act. Per `crew-launch/SKILL.md` lines 196–239 and `agent-comms/SKILL.md` §"Waking an idle peer" (lines 249–265): "a bare `send` alone is not guaranteed to rouse an idle Claude pane." The captain MUST add `--wake`:

```
harmonik comms send --to <crew> --topic assign --wake -- <retask>
```

`--wake` (per `comms send --help` and `agent-comms` lines 95–103): after delivery, the daemon **nudges the recipient's tmux pane** by resolving the pane target from the crew registry handle (fallback `harmonik-<projectHash>-crew-<name>`) and injecting a keystroke **"via bracketed-paste (the same mechanism the keeper uses)."**

**The exact injection primitive** (`internal/lifecycle/tmux/adapter.go:204–260`, `osadapter.go:369–411`, spec process-lifecycle.md §4.7 PL-021d):
1. `tmux load-buffer -b harmonik-<session-id>-<purpose> -` — load payload into a named tmux buffer from stdin (adapter.go:204–216).
2. `tmux paste-buffer -b <buffer> -t <paneTarget> -d` — paste into the pane in bracketed-paste mode and delete the buffer atomically (adapter.go:218–229). Bracketed-paste is required so Claude's React/ink TUI treats the bytes as a paste, not raw keystrokes.
3. `tmux send-keys -t <paneTarget> Enter` (`SendKeysEnter`, adapter.go:245–260) — a *real key event* (not bracketed-paste) so the TUI's key-event path sees a submit. This is the "inject Enter into the pane" that dismisses the splash / submits the buffer.
- Pane target must be the slash-free tmux pane ID (e.g. `%1964`) from `WindowPaneID` (adapter.go:181–192), because window names can be filesystem paths.
- Short (<512B, no newline) payloads may use the `SendKeysLiteral` (`send-keys -l`) fallback (adapter.go:231–243); the bare non-`-l` `send-keys` form is FORBIDDEN for daemon-injected payloads (shell-metachar interpretation).

**Why this is the hard part for a non-Claude substrate:** the re-task *content* is fully substrate-neutral (it rides comms/`events.jsonl`). What is Claude-specific is the **liveness/attention model**: a resident Claude LLM session is an interactive REPL that only "thinks" when its pane receives input, so the daemon reaches into the terminal and fakes a keypress to force a turn. A non-Claude resident orchestrator that is a genuine long-running program with its own event loop does NOT need pane injection at all — it can simply block on `comms recv --follow --json` (or `subscribe`) and act on each delivered message immediately. The entire `--wake` / bracketed-paste / splash-dismiss / keeper-`/clear` apparatus exists **only to compensate for the fact that a Claude REPL is not an event-loop process.** Replicating harmonik on a non-Claude substrate means this apparatus can be *dropped*, not reimplemented — provided the substrate offers a real blocking receive.

---

## 4. Substrate requirements — minimal capability set for any resident orchestrator

A non-Claude resident orchestrator substrate must provide (or the daemon must provide on its behalf):

1. **Durable identity** — a stable agent name (`HARMONIK_AGENT`) usable as `--from`/`--agent`/`--assignee` across restarts. Today carried in env (`crewlaunchspec.go:124–127`). **[substrate-neutral; trivially replicable]**
2. **A long-lived process with its own event loop** — able to block on and react to a message/event stream without external keystroke injection. THIS is what replaces `--remote-control` + `--wake` + pane-nudge. **[the key non-neutral requirement — but it REPLACES the Claude machinery rather than reimplementing it]**
3. **Comms client** — `join`/`leave`/`send`/`recv --follow --json`, dedupe on `event_id` (N3 at-least-once). All plain daemon-socket RPC. **[neutral]**
4. **Queue client** — `queue submit --queue <own> --beads`, `append`, own-queue-only discipline. **[neutral]**
5. **Event subscription** — `subscribe --types run_completed,run_failed,run_stale,heartbeat --follow --json` with cursor resume. **[neutral]**
6. **Beads read + metadata-write** — `br ready --limit 0`, `br list --label`, `br show --format json`, `br update --assignee`, `br comments add`; MUST NOT do terminal transitions (daemon-owned). **[neutral]**
7. **Presence heartbeat** — re-beat before the ~120s age-out. **[neutral, requires self-timed action — see #8]**
8. **Self-timed action** — a periodic tick for the ≤10/≤15-min progress feed and presence refresh. A real process gets this from a timer; the Claude substrate gets it from turn-driven loops. **[neutral capability]**
9. **Durable conversation / restart continuity** — today `--session-id`/`--resume` + keeper `/clear`. A real-process substrate replaces this with ordinary process restart + re-hydration from the mission file + beads `assignee`. In-flight work already survives independently (the named queue drains on the daemon regardless of the orchestrator's liveness — crew-launch lines 484–486). **[Claude-specific mechanism; substrate replaces, does not reimplement]**
10. **Mission/handoff bootstrap** — read `.harmonik/crew/missions/<name>.md` frontmatter + `## Current State`. Today the *trigger* to read it is a pasted seed; a real process just reads the file on startup. **[content neutral; trigger is Claude-specific and droppable]**

**Bottom line:** Everything the orchestrator *communicates and coordinates* (comms, queue, subscribe, beads, presence, progress feed, park semantics) is already substrate-neutral daemon-socket CLI. The ONLY things bound to the Claude `--remote-control` live-paste channel are the mechanisms that exist to **drive an interactive REPL that has no event loop of its own**: the boot-seed paste, the `--wake` pane nudge for mid-session re-task, the splash-dismiss Enter, and the keeper `/clear`+`/session-resume` context-cycle. A resident orchestrator on a substrate that IS a normal event-loop process replaces this whole cluster with a blocking `comms recv --follow` and ordinary process restart — it does not need to reimplement pane injection.

---

## OPEN QUESTIONS

- **OQ1 — Captain vs crew loop deltas.** This research read the crew loop (crew-launch/SKILL.md) in full and the launch spec; the captain skill was consulted via its registry description, not line-by-line here. The captain additionally: organizes backlog into lanes, spawns crews (`harmonik crew start`), writes C3 mission handoffs, mails epics, subscribes to `epic_completed`, and runs `keeper await-ack` for crew restart verification. These are all comms/CLI (neutral) EXCEPT the crew-spawn boot-seed paste and `await-ack` (which watches for a pane-ACK). Confirm the captain has no additional live-paste dependency beyond spawning/waking crews. Needs a direct read of `.claude/skills/captain/SKILL.md` + `STARTUP.md`.
- **OQ2 — Does `comms recv --follow` guarantee wake on a NON-Claude blocking process?** The `--wake` machinery is documented as needed for *idle Claude panes*. For a real process blocked on `recv --follow`, delivery should suffice with no nudge — but this is asserted, not verified against the daemon delivery path. Confirm the subscribe/recv transport actually pushes to a blocked reader promptly (no polling gap).
- **OQ3 — Keeper equivalent on a non-Claude substrate.** The keeper exists to cycle a context-bounded Claude session. A non-Claude orchestrator with bounded context would need an equivalent handoff→restart trigger, or an unbounded/streaming context. Out of scope for C2's loop enumeration but load-bearing for substrate selection.
- **OQ4 — `--session-id` semantics.** Whether the resident-orchestrator substrate needs any equivalent of Claude's `--session-id`/`--resume` durable-conversation replay, or whether mission-file + beads re-hydration is fully sufficient (crew-launch claims in-flight work is not lost regardless). Leaning "sufficient" but unverified end-to-end.
