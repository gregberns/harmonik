# C5 — Captain commissioning & restart continuity on a `codex app-server` substrate

**Codename:** codex-app-server · **Component:** C5 (commissioning + restart continuity, no live tmux pane)
**Synthesizes with:** C1 (app-server protocol), C4 (integration). Where an answer depends on
app-server protocol details not yet fixed, it is called out as an explicit dependency, not guessed.

All citations are `file:line` against `/Users/gb/github/harmonik`.

---

## 1. How the captain commissions a Claude crew TODAY

Commissioning is five mechanisms (captain SKILL §2–§5,
`.claude/skills/captain/SKILL.md:15-17`, `:309-528`). Substrate-neutral vs. pane-assuming:

### Substrate-NEUTRAL

1. **Mission handoff file (C3 schema).** Captain writes `.harmonik/crew/missions/<crew_name>.md`
   FIRST, before launch (`SKILL.md:313-314`, `:356-408`). Locked six-field frontmatter
   `{schema_version, crew_name, queue, epic_id, goal, captain_name}` (`SKILL.md:362-364`,
   `specs/crew-handoff-schema.md`). Plain on-disk file — no substrate coupling. `session_id` is
   explicitly NOT in the handoff — C2 mints/owns it (`SKILL.md:406-408`).

2. **Durable crew registry.** `internal/crew/registry.go` — per-crew JSON at
   `.harmonik/crew/<name>.json`, atomic temp-write+rename (`registry.go:85-149`). `Record`
   (`registry.go:38-49`): `{SchemaVersion, Name, Type, SessionID, Queue, Epic, Handle, StartedAt}`.
   - **No `thread_id` and no tmux `pane_id`/label field exist today.** `Handle` (`registry.go:47`)
     is a free-form string, NOT a structured pane target. `SessionID` (`registry.go:44`) is the
     Claude session UUID for `--resume`. `UpdateSessionID` (`registry.go:217-226`) is the only
     field-level mutator.
   - **OPEN QUESTION (C1/C4 dep):** app-server needs a durable `thread_id`. The registry has
     nowhere to put it — a new `Record` field is required. Depends on C1 naming the handle.

3. **Epic attribution via `br show <epic> --assignee`** (see §5 — fully substrate-neutral).

4. **Comms mail of the epic.** `harmonik comms send --to <crew_name> --topic assign -- "<epic_id> ..."`
   (`SKILL.md:418`); broadcast via `--broadcast --topic announce` (`SKILL.md:421`). The bus is
   daemon-mediated/substrate-neutral; the delivery-to-agent step holds the pane assumption (§2).

5. **Subscribe to completion.** `harmonik subscribe --types epic_completed --json`
   (`SKILL.md:442-445`) attaches to the daemon (substrate-neutral). On each
   `epic_completed{epic_id, last_child_bead_id, closed_at}` the captain attributes via the
   `--assignee` mirror (`SKILL.md:454-472`).

### PANE-ASSUMING (breaks / must be re-homed)

- **Spawn = `harmonik crew start <crew> --queue <q> --mission <path>`** (`SKILL.md:317-318`,
  `cmd/harmonik/crew.go:239-341`). CLI sends a `crew-start` JSON-RPC op (`crew.go:308-328`); the
  daemon C2 handler (`internal/daemon/crewstart.go:3-9`) does collision-check, registry write,
  queue-ensure, **session launch** (tmux window running a Claude REPL), paste-seed, keeper-attach.
- **Boot-seed = tmux bracketed paste into the crew's agent pane.** `pasteCrewBriefSeedViaTmux`
  (`crew.go:200-237`) derives pane target `harmonik-<hash>-crew-<name>:<WindowAgent>`
  (`crew.go:210-212`) and SendKeysEnter+WriteToPane+SendKeysEnter to type
  "Please run `harmonik agent brief`..." + Enter (`crew.go:215-236`). **This is the substrate
  seam** — assumes a live REPL pane. On app-server the "first user turn" must be a JSON-RPC turn
  into the resident thread (C1/C4 dep).
- **Liveness confirm = poll `harmonik comms who` until `<crew>` appears** (`SKILL.md:329-335`).
  Neutral in mechanism, but depends on the crew's boot loop running `comms join` — which happens
  today because the pane booted a Claude session (see §4).

---

## 2. Re-tasking with NO pane

**Today:** re-tasking a LIVE crew is NOT a new `crew start` — it is a `--topic assign` comms send
naming the new `epic_id`; the crew picks it up via its armed `comms recv --follow` loop and
re-adopts (re-mirroring `--assignee`) (`SKILL.md:424-427`, crew-launch `:243-250`). But delivery
lands only if the crew is actively listening: a Claude idle session processes a delivered message
only when EITHER (a) an armed `comms recv --follow --json` stream is still running, OR (b) its
tmux pane gets a nudge — `comms send --to <crew> --wake` (capture-pane + inject Enter) or a manual
poke (crew-launch `:194-236`; captain notes wake is pane-nudge, best-effort, `SKILL.md:662-668`).
The fallback wake path is pane-bound today.

**On app-server:** captain intent is unchanged ("send an `assign` comms message"), but delivery
changes:
- No pane to bracketed-paste, no pane to `--wake`. Re-task becomes **"enqueue a new user turn into
  the resident `thread_id` via JSON-RPC"**, OR feed it through a **daemon-fronted mailbox that the
  app-server client converts into the next user turn** when the current turn completes.
- The "armed `--follow` stream" model may be replaceable: the daemon can deliver the assign
  directly as the next turn rather than relying on the agent self-listening. The
  "idle crew is deaf without a pane nudge" failure mode (crew-launch `:207-223`) is a pane
  artifact — a daemon-fronted crew has no idle-deafness because the daemon owns turn injection.
- **Characterization:** captain-side re-task unchanged (still a comms `assign`); the substrate
  adapter (daemon ↔ app-server) converts a delivered `--to <crew>` message into a JSON-RPC user
  turn on that crew's thread. The pane-nudge/`--wake` branch becomes a no-op / is replaced by
  "post next turn to thread".
- **OPEN QUESTION (C1 dep):** can the server accept a new user turn while a prior turn is still
  streaming, or must the daemon queue until the turn completes? Decides whether the daemon needs a
  per-thread turn-mailbox.

---

## 3. Restart continuity with SERVER-SIDE conversational state

**Today (Claude):** state is client-side in the session context window. Keeper context-full
wind-down: write handoff → `/clear` (wipes context) → `/session-resume` on the SAME `session_id`
(`--resume <uuid>`); the crew re-runs its full boot loop and **re-hydrates `{queue, epic_id}` from
the handoff frontmatter + the durable `br show <epic_id> --assignee == crew_name` mirror**
(captain `SKILL.md:677-689`; crew-launch §Self-restart `:467-500`). The daemon keeps draining the
crew's named queue across the restart, so no in-flight work is lost (`SKILL.md:682-683`).
Idempotent re-boot: re-`join`, re-mirror `--assignee` (no-op if set), re-arm `--follow`
(crew-launch `:477-489`).

**On app-server the model largely COLLAPSES.** If conversational state lives server-side in the
thread, the "context-full → dump-and-rebuild" reason for handoff → `/clear` → `/session-resume`
is no longer the daemon's problem in the same form:

- Restart continuity becomes **"reconnect the client to `thread_id` + replay any missed
  comms/queue events since disconnect."** Server holds the conversation; daemon holds coordination
  state.
- **Daemon MUST persist:**
  1. **`thread_id`** — server-side conversation handle. Needs a new registry field (`registry.go:38-49`
     has none); replaces/augments `SessionID`.
  2. **`queue`** — already persisted (`registry.go:45`); daemon keeps draining across reconnect.
  3. **`epic_id`** — already persisted (`registry.go:46`) AND mirrored bead-side via `--assignee`
     (the durable truth, §5).
  4. **comms membership / identity** (`$HARMONIK_AGENT == crew_name`) so the reconnected client
     re-`join`s and resumes its inbox.
  5. **queue subscription cursor** — at-least-once replay after reconnect (N3 dedupe on
     `event_id` still applies).
- **Server holds** (need NOT be re-derived): full conversation history, working reasoning, prior
  turns. On reconnect the crew does NOT need handoff-frontmatter re-hydration of its *reasoning* —
  only re-attachment to the coordination substrate (bus, queue).
- **C3 handoff file's role shrinks.** Today it is tier-1 memory surviving `/clear` (crew-launch
  `:85-117`). On app-server the conversation survives on the server, so
  `{queue, epic_id, goal}` become bootstrap/attribution redundancy rather than sole memory. Keep
  it as a durable human-readable assignment record, but it is no longer load-bearing for
  conversational continuity.
- **OPEN QUESTIONS (C1/C4 dep):**
  - Does the thread persist across daemon restarts, or only across client disconnects? If the
    server evicts idle threads, the daemon needs a thread-liveness probe + re-create-from-handoff
    fallback (which resurrects the handoff's load-bearing role). C1 owns thread lifetime.
  - Is there a per-thread context ceiling server-side that still forces a "compact / new thread"
    cycle? If so, continuity does NOT fully collapse — it becomes "server-side compaction" and the
    keeper's role changes rather than disappears.
  - Keeper role: today the keeper drives the `/clear` cycle. If server-side state removes
    client-context-fill pressure, the keeper's watcher may become unnecessary for these crews —
    but ONLY if the server has no analogous ceiling (previous bullet). Flag, do not assume.

---

## 4. Presence / liveness with no interactive session

**Today:** `harmonik comms who` shows online iff the latest `agent_presence` beat has
`status="online"` and `last_seen` within a **120s TTL** (`cmd/harmonik/comms.go:1146`, `:790`
"TTL=120s, StaleCutoff=10m"; test `comms_presence_who_hk6vwi3_test.go:8`, `:169`). (Separate
`internal/sentinel` `DefaultPresenceTTL = 15m`, `signals.go:145-147`, is a coarser sentinel-snapshot
window — the operator-facing `comms who` window is 120s.) Beats come from:
- `harmonik comms join` → `agent_presence{online, reason:"join"}` (`comms.go:503`).
- Any `comms send` refreshes `last_seen` (test `:145`).
- The `comms recv --follow` / subscribe connection heartbeat (`heartbeat_seconds:60`,
  `comms.go:1504`, `:1587-1621`) — a receive-only agent with a refresh beat within 120s is online
  (test `:169`). The armed `--follow` stream is what keeps an idle Claude crew present today.

**Problem:** an app-server crew has no interactive session voluntarily keeping a
`comms recv --follow` client alive between turns; between turns nothing heartbeats. **Who sends the
join beats?**

- **The daemon, fronting the crew, must own presence.** It already holds the `thread_id` and queue
  subscription, so it is the natural place to emit the crew's `agent_presence` beats (join on
  commission, periodic refresh < 120s, leave on teardown/reap) on the crew's behalf — a
  **daemon-driven presence proxy**.
- Cleaner than today: today presence is coupled to a fragile client-side `--follow` stream that
  dies on `/clear` and needs manual re-arm (crew-launch `:219-223`, captain `:660-675`). A
  daemon-fronted crew's presence is as durable as the daemon.
- The `crewstart.go` post-spawn keeper liveness probe (`crewstart.go:47-50`, `keeperProbePollInterval`)
  is an analogous "daemon confirms the spawned thing is live" pattern that could generalize to a
  per-thread heartbeat loop.
- **OPEN QUESTION (C4 dep):** where the presence-proxy loop lives (daemon crew-manager vs.
  per-thread supervisor goroutine) and whether presence reflects *thread liveness* (server says
  thread alive) vs. *agent responsiveness* (last completed turn). C4 owns daemon wiring; C1 owns
  whether the server exposes a thread-liveness ping. Recommend: presence beat = daemon can reach
  the thread AND the queue is being serviced.

---

## 5. Epic attribution — carries over UNCHANGED (CONFIRMED)

The `--assignee`-on-every-adopt mechanism is **bead-side and fully substrate-neutral** — confirmed,
no change for app-server.

- The crew runs `br update <epic_id> --assignee <crew_name>` on **every** epic adoption — at boot
  AND on every `topic == assign` comms re-task (crew-launch Step 4, `.claude/skills/crew-launch/SKILL.md:134-150`,
  `:245-250`). Metadata-only write, permitted by write discipline (NOT a terminal transition)
  (`:140-141`).
- The captain attributes `epic_completed` (and `run_failed`/`run_stale` for beads) by reading
  `br show <epic_id> --format json` → `assignee` (captain `SKILL.md:454-472`). It explicitly does
  NOT use the registry's spawn-time `Record.Epic` (`registry.go:46`) — attribution is the durable
  bead mirror, not the in-memory/registry map (`SKILL.md:466-469`). This is exactly why it is
  substrate-neutral: it never touches the pane, session, or thread — only the beads ledger.
- Fallback if `br` lacks `--assignee`: `br update <epic_id> --add-label crew:<crew_name>`;
  re-hydration checks the `crew:<crew_name>` label (crew-launch `:158-164`).
- Load-bearing constraint preserved: `--assignee` goes on the EPIC ONLY, never a child bead (the
  daemon's `br claim` refuses an already-assigned bead → `max_attempts_exceeded`), crew-launch
  `:152-156`. Independent of substrate.

**Conclusion:** §5 is a no-op for the migration. Adjacent risk: *who runs the `br update`* — today
the crew agent inside its boot/re-task turn; on app-server still the crew agent executing a tool
call within a turn, so unchanged. Only confirm the app-server crew's turn environment has `br` tool
access (minor C4 integration dependency).

---

## Cross-component dependency summary

| Question | Depends on | Owner |
|---|---|---|
| Registry `thread_id` field name + shape | server-side conversation handle naming | C1 |
| Re-task = new user turn: server accept a turn mid-stream? | turn semantics → per-thread mailbox y/n | C1 |
| Thread survives daemon restart / idle eviction? | thread lifetime | C1 |
| Server-side context ceiling → keeper still needed? | thread token limits | C1 |
| Presence-proxy + turn-injection loop location | daemon crew-manager wiring | C4 |
| Crew turn env has `br` tool access | turn tool provisioning | C4 |
