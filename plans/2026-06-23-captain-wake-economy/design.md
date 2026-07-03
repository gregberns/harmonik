# Design: Watch Tier + Captain Wake-Economy

> **Status:** design REVISED for the 4 OPERATOR RULINGS (§12) **and the 2 operator follow-ups** (2026-06-24,
> post-round-3): **(A)** couple the watch-standup + sender-redirect into ONE coordinated MVP rollout — never
> ship the redirect with no watch online to drain it (the captain would go blind and the fleet stall); defer
> native scheduled-send + full mutual-liveness + ledger polish to follow-on beads. **(B)** scheduled-send is
> native-first-class only — the `bash -c` command-wrapper option is **DROPPED**. These follow-ups re-scoped §7 +
> §11, and the **ROUND-4 CRITIC GATE on the revised sequencing is now CLEAN** (3 fresh critics: C1 coupling
> **REVISE** — 2 blockers + 1 major, ALL transcribed-fix-applied; C2 scope/code-claim **APPROVE** (all code
> claims verified TRUE); C3 stale-ref sweep **APPROVE** (ZERO stale refs) — no architecture rejected).
> Kerf work `wake-economy`, epic hk-var9b, label `codename:wake-economy`. Inputs: `README.md` +
> `01-problem-space.md` + a 5-dimension read-only codebase audit (`research-notes.md`) +
> **`.kerf/works/wake-economy/critic-findings.md`** (round-1 = 3 critics → REVISE → 7 fixes; round-2 = 3 fresh
> critics → APPROVE + 2 transcribed-majors; round-3 = 3 fresh critics on the rulings deltas → ALL closed;
> round-4 = 3 fresh critics on the follow-up re-scope → C1-B1 default-safe redirect target, C1-B2 concrete
> heartbeat backstop, C1-M1 honest stalled-watch residual, + line-citation/diagram fixes — ALL applied).
> **Gate:** reviewers WITH critics must approve before any build (operator hard requirement) — **SATISFIED across
> 4 rounds.** The `hk-8hr1` precondition is met (landed on main). **Next: captain go → build the MVP group
> (WE1–WE5, WE7, WE8) via paul's queue DOT, then the follow-on group (WE6, WE9, WE10).**

> **Revision provenance (what the critics changed):** The original draft assumed captain-wake triggers live
> in skill *prose*. They do not. The two biggest wake sources are **daemon Go** (`QuiesceArbiter`) and a
> **bash script** (`ops-monitor-check.sh`), both pointed at the captain. The fixes below redirect the real
> code at the SENDER (operator ruling 2 — no filter), satisfy O5 by addressing not filtering, forbid hardcoded
> intervals (the same `hk-drygf` mistake), and harden the watch against being a silent single point of failure.

---

## 0. Name decision (Q1) — RULED

**Operator ruled: the role is `watch` (informal).** Used for the role, the skill
(`.claude/skills/watch/`), the mission (`watch.md`), the crew/agent name (`--to watch`,
`harmonik start crew watch`), the queue (`watch-q`), and the liveness component (`watch`). It is a **role**,
not a Dune-named crew instance. Referred to below as **the watch**.

## 1. Architecture — where the watch sits

```
  operator ──(direct to captain, never intercepted: O5 — see §6)──────────────────────────┐
                                                                                           ▼
  bus events (events.jsonl, ~163 types) ─┐                                       ┌──────────────────┐
  ops-monitor [DIGEST]/[IMMEDIATE] (bash)─┼──► ┌───────────────────┐  escalation  │  CAPTAIN (Opus)  │
  crew status posts (→ watch) ────────────┘    │  WATCH            │─────(wake)──►│  judgment only   │
                                               │  (Sonnet session) │              │  wakes on a real │
  ops-monitor [IMMEDIATE-CRITICAL] ────────────┼──── DIRECT bypass ──────────────►│  escalation only │
  daemon decision_required / epic_completed ───┘    (most-urgent, §4)             └──────────────────┘
                                               │  • record→ledger  │                     ▲
                                               │  • triage         │    captain PULLs digest on own idle
                                               │  • escalate actn. │    (reads latest.json — NOT pushed)
                                               └───────────────────┘
                                                       ▲
                                hourly mutual liveness (O4, §5 — basic watch-down MVP; dual-probe/cursor-advance = follow-on WE9)
```

Three tiers, cheapest-first — **plus a DIRECT bypass for the most-urgent IMMEDIATEs** so the watch is never a
single point of failure on the events the captain must not miss:

- **ops-monitor (bash, every@5m)** — deterministic checks. *Already exists (CE4).* Its `send_comms` target
  changes from `captain` to the watch (config-driven, §6) **except** the most-urgent IMMEDIATE class
  (daemon-down / supervisor-down / paused-queue), which keeps a DIRECT path to the captain.
- **watch (Sonnet session, always-on)** — *new.* Consumes the bus + ops-monitor reports + crew
  reports; records all to a ledger; triages; escalates only actionable summaries to the captain.
- **captain (Opus)** — judgment only; wakes on a watch escalation, a direct-bypass IMMEDIATE, or an
  operator message; long heartbeat for liveness; PULLs the digest on its own idle.

This is **net-new beyond CE4**: CE4 moved the deterministic *checks* to bash; this work moves the *triggers*
(crew churn + the 12-min timer) off the captain onto a Sonnet session, and **repoints the real senders in
code** rather than asking the captain to ignore them in prose.

## 2. The watch role (O1)

### Inputs
- **Bus:** `harmonik subscribe --types <set> --since-event-id <cursor>` (live + replay). The full bus, not
  just agent_message. Backpressure is 256-slot drop-oldest → on `subscription_gap` the watch re-scans
  `events.jsonl` from its cursor (worst-case escalation latency under a burst is bounded by one re-scan —
  stated in WE2/WE3).
- **ops-monitor reports:** the watch **reacts to ops-monitor's existing `[IMMEDIATE]`/`[DIGEST]` comms event**
  and reads `.harmonik/ops-monitor/latest.json` **only on receipt of that event** — never on its own timer
  (avoids re-introducing a poll loop; critic 3 #1).
- **Crew reports:** crews send status `comms --to watch` (redirected from `--to captain` — see §6).

### Outputs
- **Ledger writes** (every intercepted event — §3).
- **Escalations** to the captain (only actionable — §4): `comms send --to captain --wake --topic escalation`.
- **Digest** maintained for the captain to PULL (`.harmonik/watch/latest.json`) — NOT pushed (§4).
- **Liveness beats** (`comms join`/presence) for the mutual check (§5).

### Boundary — what the watch MAY decide vs MUST escalate (O7)
- **MAY (autonomous):** record to ledger; classify event severity; batch/summarize; nudge a stale crew
  (capture-pane + `comms --wake`) once before escalating; de-dupe; suppress all-green noise.
- **MUST escalate (never decides):** crew-failure/kill, new-initiative ranking (work not in `kerf next`),
  locked-decision reversal, destructive ops, **the staffing decision** (which crew + which epic). The watch
  may *flag* "ready work + free slot exists"; the captain picks. (O7, leanfleet D6.)

## 3. Event ledger (Q2)

**Decision: reuse `events.jsonl` + a watch cursor; add a thin query surface. No new event store.**

- The bus already persists every event durably to `.harmonik/events/events.jsonl` with `event_id` (UUIDv7),
  `type`, `timestamp_wall`, `run_id`, `payload`. That **is** the ledger of record.
- The watch keeps its own forward cursor at `.harmonik/watch/cursor` (last processed `event_id`). It
  reads via the **read-pure `ScanAfter` / `comms log --since`** path (advances NO `comms recv` cursor — critic
  2 confirmed the recv cursor would contaminate agent state). The watch maintains its own watermark only.
- **Dedupe on `event_id`** (N3 at-least-once / EV-018) using an in-memory `seen` set, rebuilt from the cursor
  on restart — re-processing is idempotent.
- **Query surface for the captain on demand:** `harmonik comms log --since <dur> [--from --topic]` already
  scans `events.jsonl` with no daemon. For richer "what happened in lane X" queries the watch maintains a rolling
  **summary digest** at `.harmonik/watch/latest.json` (mirrors the ops-monitor pattern: a small typed
  JSON the captain can `jq` on demand). No SQLite, no new socket op.

## 4. Escalation taxonomy (Q4) — event-driven, no polling (O2)

The captain wakes **only** on IMMEDIATE escalations + direct-bypass IMMEDIATEs + operator messages + a long
heartbeat. **The batched digest is PULL, not push** — there is no timed comms-send to the captain (a timed
send is a poll loop by another name; O2). Grounded in the real bus event types and ops-monitor's existing
IMMEDIATE/DIGEST topics (R3).

| Class | Trigger | Watch action |
|---|---|---|
| **IMMEDIATE — via watch** (wake captain now) | single-mode, review-bypass, `decision_required` needing judgment, `run_failed` needing judgment, crew-failure/kill, captain liveness breach | `comms send --to captain --wake --topic escalation -- "<plain summary + what decision is needed>"` |
| **IMMEDIATE — DIRECT bypass** (NOT through watch) | daemon-down, supervisor-down, paused-queue | ops-monitor keeps its existing DIRECT `--to captain` path — the watch is never in the critical path for "the fleet is down" (critic 3 #5c) |
| **PULL-DIGEST** (no wake; captain reads on own idle) | backlog-ready + free slot (staffing flag), idle-fleet / lull, crew-staleness (slow-recovery) | accumulate into `.harmonik/watch/latest.json`; **never timed-send**; optionally fold ONLY into the next genuine IMMEDIATE |
| **LEDGER-ONLY** (never wake) | `epic_completed` (see note), routine crew status posts, `run_started`/`run_completed`, `agent_output_chunk`, `metric`, `agent_heartbeat`, `session_keeper_warn`/`cycle_complete` | record to ledger only |

- **`epic_completed` is LEDGER-ONLY at the watch** (critic 1 M1). The daemon already wakes a parked captain on
  `epic_completed` (`quiesce.go:627-636`) AND the captain subscribes to it directly (`STARTUP.md:506`).
  Listing it IMMEDIATE at the watch too would *triple-wake*. It stays on the captain's **direct** subscribe; the
  watch only records it. (This is the one event we deliberately leave on the captain's own stream.)
- **Escalation message contract:** the watch writes the summary **in the captain's own words** — what happened,
  which lane/subsystem, what decision is needed — never a raw event dump or a tracking ID the captain can't
  dereference.
- **Wake path:** the just-fixed `comms send --to captain --wake` (09c112c5) reaches the bare
  `harmonik-<hash>-captain` pane (STEP-1 prereq, satisfied). Note `SKILL.md:606` flags pane-nudge wake as
  best-effort; for a **parked** captain the more reliable wake is the daemon `QuiesceArbiter` (§6, B1).

## 5. Mutual liveness (O4) — reuse component-liveness, hardened against a stalled watch

**Decision: reuse the just-shipped component-liveness alerting (paul's prior lane); add dual-probe +
escalation-cursor-advancement so an *alive-but-stalled* watch is caught (critic 3 #5 — the single biggest risk).**

**Phase split (operator follow-up A).** **Basic watch-down** detection (process/tmux-down → escalate → captain
respawns) stays IN the MVP (WE7/WE8) — a redirected-to but *dead* watch is the same blind-captain failure the
MVP exists to prevent. The **alive-but-stalled** hardening below (dual-probe + escalation-cursor-advancement)
and the hourly *bidirectional* ping defer to **follow-on WE9** (see §11).

The component-liveness lane (plans/2026-06-22-component-liveness-alerting, landed in `ops-monitor-check.sh`)
already delivers: captain-liveness via `comms who` last_seen + tmux probe (IMMEDIATE if absent >10m);
escalation tiers (component >5m down → escalate, >30m → ops-CRITICAL); shortened IMMEDIATE_COOLDOWN for
critical components (30m→5m); per-crew keeper coverage (`ps` for `harmonik keeper --agent <name>` — macOS
uses `ps`, not `pgrep`).

Bidirectional check:
- **Watch → captain:** ops-monitor already checks the captain; the watch additionally escalates a
  captain-absence IMMEDIATE.
- **Captain → watch:** add the watch as a **tracked critical component** in component-liveness. The
  watch-down check MUST use the **DUAL probe** (comms-absence AND no-tmux), the same as the captain check — NOT
  `last_seen` alone (a process pinned on the bus still updates `last_seen` while buffering every escalation).
  **Plus an escalation-cursor-advancement check:** the watch is healthy only if its escalation cursor
  (`.harmonik/watch/cursor`) advances over a window, not merely if it is alive. A watch that is alive but
  whose cursor is frozen while events accumulate → ops-monitor → captain IMMEDIATE; the captain restarts it.
  **Seam (WE9 — follow-on):** reuse ops-monitor's existing prior-tick state machinery — `state.json` already persists
  `prev_*` values and runs consecutive-miss counters (`ops-monitor-check.sh:121-155,255-278`). Store the
  last-seen cursor value there and flag if it is unchanged over N consecutive ticks (same pattern as
  `prev_misses`). No new state surface.
- **Cadence:** the "once-an-hour (configurable)" mutual ping is a **scheduled task** (§7), not a poll loop.
  Interval is **config-or-fail-loud** (§7), never a Go literal.

## 6. Captain & sender rewrites (O6) — redirect the real code, survive restart

The original draft put this in skill prose. The two biggest wake sources are **in code**, so the rewrite
repoints code first, prose second.

### 6.1 Redirect the senders (critic 1 B1/B2/M4 — fix #1)

Every `--to captain` sender, enumerated and repointed (R3 + critic audit located exact lines):

1. **ops-monitor** (`scripts/ops-monitor-check.sh`) — **this is a message-PARTITION, not a target swap**
   (critic A). `send_comms()` (:1005-1010, line numbers verified round-4 C2) is ONE function with `--to captain`
   hardcoded, and the IMMEDIATE branch (:1016-1036) joins ALL 8 immediate signals (:750-771 — daemon-down,
   supervisor-down, paused-queue, single-mode, review-bypass, captain-down, keeper-missing, release-due) into a
   SINGLE `[IMMEDIATE]` message via one `send_comms` call. So WE7 must **restructure the send to emit TWO
   messages to TWO targets**: (a) a **direct-class** send (`daemon-down / supervisor-down / paused-queue` → `--to
   captain`, the §4 SPOF bypass) and (b) a **watch-class** send (everything else + DIGEST + ops-CRITICAL → the
   config-driven watch target `watch.opsmonitor_target`, default `captain` until the §11 flip). The watch
   re-escalates any watch-class IMMEDIATE/CRITICAL it receives so
   nothing is dropped. **The §4 DIRECT-bypass SPOF guarantee DEPENDS on this partition being built** — if WE7
   instead routes ALL immediate to the watch, the §4 bypass is void and a stalled watch buffers "fleet-is-down".
   We commit to the partition (the all-to-watch path reintroduces the exact SPOF the design hardens against).
2. **supervisor asset-skew** (`cmd/harmonik/supervise/assetskew.go:79-86`, `--from supervisor --to captain
   --topic status`) — low volume; classify as **allowed-direct** (it is a boot-time status, not churn) and
   leave it, OR redirect to the watch. Recommend leave-direct + document it as an allowed direct sender.
3. **crews** — the crews' MANDATORY status feed lives in `crew-launch/SKILL.md` (:359,378 — `--to
   <captain_name> --topic status`), an **EMBEDDED asset** with a mirror at
   `cmd/harmonik/assets/skills/crew-launch/SKILL.md` (critic A). The redirect = repoint that target from the
   `<captain_name>` placeholder to a **configurable status target `watch.status_target` defaulting to `captain`**
   (round-4 C1-B1 — default-safe so the redirect is inert at merge time and takes effect only when the operator
   flips the key to `watch` as the rollout's final step, §11 coupling guarantee; a parameterization, not a
   literal `--to captain`→`--to watch` swap). **Editing it trips
   `TestSkillAssetsEmbedInSync` exactly like the captain skill**, so WE7 MUST carry its own `cp` repo→embed +
   `go test ./cmd/harmonik/` + grep done-check for crew-launch/SKILL.md (repo+embed = 2 files). The watch digest
   is the backstop if a crew misses the redirect.

### 6.2 The daemon parked-session interlock (QuiesceArbiter — operator ruling 2)

`internal/daemon/quiesce.go:638-654` (`handleAgentMessage`) fires `wakeSignal{captainWake:true}` whenever
`payload.To=="captain"` — but this only nudges a *parked* captain (the `a.sleeping` map,
`executeWake:661-667`). A session on an armed `comms recv --follow` Monitor is instead woken by delivery to
its subscribe stream. Two daemon-Go changes (WE5):
- **Wake the parked WATCH too:** the watch is now the routine recipient (§6.1) and **is** subject to quiesce
  parking (`parkAllSessions`, `quiesce.go:466-475`, parks the captain AND every crew record into `a.sleeping`
  keyed by agentName), so `handleAgentMessage` must ALSO wake a parked watch on `payload.To=="watch"`.
  **Plumbing note (WE5):** this is more than a `to==watch` check — `executeWake` (`quiesce.go:661-687`) has
  only a `captainWake bool` (hardwired to `captainAgentName`) and a `queueName` branch; there is no
  wake-parked-agent-by-name path on the `wakeC`→`executeWake` route (`HandleDaemonWake:849` has a by-name
  lookup but that is the operator-CLI path, not the event path). WE5 must add an `agentName`/target field to
  `wakeSignal` + a matching `a.sleeping[sig.agentName]` branch in `executeWake` (or map watch→its queueName).
  Small + localized, but not a one-liner.
- **No captain allowlist / filter needed:** once §6.1 redirects every routine sender to the watch, the
  residual `to==captain` directed mail is just {operator, watch escalations} — so the existing
  wake-on-`to==captain` is already *correct*, not noisy. We add NO sender-filter (the firehose was a
  non-problem — §6.3).

### 6.3 O5 operator-direct — by ADDRESSING, not filtering (operator ruling 2)

**Operator ruled: SENDER-REDIRECT model; NO filter of any kind.** Ground truth: `comms recv --agent X`
already delivers only mail addressed `to==X` plus broadcasts — the captain is **not** on a firehose, so the
draft's client-side-routing (and the server-side repeatable-`--from` filter alternative) **solved a
non-problem and are DROPPED.** The fix is entirely **sender-side** (§6.1): redirect every routine `--to
captain` sender to `--to watch`. After that, the captain's plain `comms recv --agent captain` naturally
contains only {watch escalations, operator-direct, allowed-direct boot status, broadcasts} — no client-side
routing required.
- **The operator stays `--to captain`** — O5 ("directly, never intercepted") is satisfied by *addressing*,
  not filtering; operator mail reaches the captain with no interception and **structurally cannot be dropped**
  (there is no filter to drop it).
- **The watch observes ALL traffic via the comms event-log ledger** (§3: `comms log --since` / `ScanAfter`
  over `events.jsonl`), NOT by being a recv recipient of everything. No client-side LLM filtering anywhere.

### 6.4 Skill rewrite — remove the poll triggers (critic 3 #4 — fix #5)

Normative edits to the captain skill (the durable lever — a running captain reads the **repo** path
`.claude/skills/captain/STARTUP.md`; `start captain → ensureBootAssets → provisionSkills force=false` SKIPS
existing files and never clobbers, so removing the *text* is what makes O6 stick; `init --force` is the only
clobber path and is never run against a live captain):

1. **Remove the `/loop` health tick** — present in **3+ places** as the explicit `/loop 12m` invocation
   (`STARTUP.md:515,664,694,723`; `SKILL.md:184,187,399`) **AND as bare-`/loop` re-arm prose** that the
   literal `/loop 12m` string misses: `STARTUP.md:490` ("the `/loop` health tick must be re-armed after
   `/clear`" — an ACTIVE re-arm instruction) and `:513` (the watcher-section header introducing the loop).
   A single surviving bare-`/loop` re-arm line silently decays O6 (critic B). WE4 must remove/rewrite the
   bare-`/loop` lines too, not only the `/loop 12m` invocations.
2. **Captain's watcher set becomes:** {**plain** `comms recv --agent captain` (sender-redirect makes it
   naturally clean — NO client-side routing/filter, §6.3), `epic_completed` direct subscribe (kept — §4 note),
   a **long-heartbeat liveness fallback**}. Keep the explicit ban on the `run_stale,heartbeat` "observe
   everything" subscribe (`STARTUP.md:495-503`).
   **The heartbeat fallback is concrete, not a new self-`/loop` (round-4 C1-B2):** captain-liveness is now owned
   **externally** by the already-shipped ops-monitor captain-liveness probe (§5: `comms who` last_seen + tmux
   probe → IMMEDIATE if absent >10m, landed in component-liveness — NO new MVP bead). The captain runs NO
   self-`/loop` of any interval; "long heartbeat" means the captain relies on (a) the watch's escalations and
   (b) ops-monitor probing *it* — so removing `/loop` does not leave a gap, and the `grep=0 "/loop"` done-check
   stays correct (it is NOT replaced by a longer-interval `/loop`). WE4's done-check additionally asserts the
   captain skill states that captain-liveness is ops-monitor-owned, not self-`/loop`-owned.
3. **Resume/wake re-arm prose** (`STARTUP.md:489-491`, `:719-724`; `SKILL.md:183-189`, `:399-400`): re-arm
   only the unfiltered comms + heartbeat; note the watch tier runs independently and needs no re-arm.
4. **New watch skill** (`.claude/skills/watch/SKILL.md`) + mission template (role, ledger,
   taxonomy, boundary — §2).

**WE4 done-check (mandatory, critic 3 #4 + critic B):** **`grep=0 "/loop"`** (broadened from `/loop 12m`,
which passes while bare-`/loop` re-arm prose at `:490`/`:513` survives) across **STARTUP.md + SKILL.md, repo
AND embedded copy = 4 files** (`.claude/skills/captain/{STARTUP,SKILL}.md` +
`cmd/harmonik/assets/skills/captain/{STARTUP,SKILL}.md`), then `cp` repo→embed and **`go test
./cmd/harmonik/`** green (`TestSkillAssetsEmbedInSync` byte-compares embed vs canonical; fires on a repo-only
edit). If any legitimate non-health-tick `/loop` use remains, switch to an explicit allowlist grep — but the
default is zero.

## 7. Config-driven scheduled tasks (O3) — reuse `harmonik schedule`, NO Go-literal intervals

**Decision: reuse the existing `harmonik schedule` primitive.** It provides config-driven, durable,
restart-surviving jobs (`.harmonik/schedules.json`, `every@<dur>` / `daily@HH:MM tz`, overlap/catchup, daemon
2s poll). The generic `schedule add` path is clean — `parseInterval` fails loud with no fallback
(`internal/schedule/clock.go:14-27`).

**Hardcoded-interval landmine (critic 3 #2 — fix #3, = the HELD `hk-drygf` mistake).** The Go-literal
auto-registered schedule helpers WE6/WE7 **MUST NOT copy** are: `opsmonitor_schedule.go:37` (`Interval:"5m"`),
`ctx_watchdog_schedule.go:46` (`Interval:"5m"`), **and `seedGoalKeeperSchedule` (`init_cmd.go:841-869`,
`Interval:"1h"`)** — the last is the closest template to the watch's 1h liveness ping and the most likely
thing an implementer reaches for ("there's already a 1h backstop, copy that"); copying it reintroduces the
exact `hk-drygf` mistake. Instead:
- Every watch interval + **behavioral** target resolves via a **`ResolveKeeperConfig`-style config-or-fail-loud**
  accessor (keys e.g. `watch.digest_interval`, `watch.liveness_interval`, `watch.escalation_target`). A missing
  key **fails loud naming the key + pointing at `--example`** — never a silent default, never a Go literal.
  (`off` / `0s` remain valid *explicit* values.)
- **Exception — the two SENDER redirect-target keys default to `captain`, NOT fail-loud (round-4 C1-B1, load-bearing).**
  `watch.status_target` (crew status feed) and `watch.opsmonitor_target` (ops-monitor watch-class send) are the
  ONE place a default is mandatory and correct: their default is **`captain`**. This is load-bearing for the §11
  coupling guarantee — a fail-loud redirect target would crash the senders on a merged-but-unflipped redirect
  (dropping crew completions + ops alerts), reintroducing the blind-captain window through a different door.
  Config-or-fail-loud governs only the watch's OWN behavioral keys (intervals, `escalation_target`); these two
  redirect-target keys default-safe to `captain` and are flipped to `watch` only as the §11 rollout's final step.
- **Every setting carries a DESCRIPTION (operator ruling 4) — from a SINGLE source (critic 2 B1/B2).** Ruling
  4 requires the description in BOTH the fail-loud error AND the `--example` schema. In the keeper code these
  are TWO unrelated hand-maintained surfaces with NO shared source: the missing-key error
  (`resolve_keeper_config.go:116-128`) emits only `strings.Join(e.Missing, ", ")` — bare key paths,
  description-LESS (`requiredKeeperValue` has no description field), and the `--example` descriptions live in a
  separate hand-written const (`keeper_config_example.go:36-86`). So the carrier **must NOT copy the keeper one
  verbatim** (it would inherit a description-less error and the §7 example error below would be aspirational).
  Instead, **WE7 INTRODUCES** ONE source of truth for the MVP redirect-target keys — a
  `requiredWatchValue{KeyPath, Description string, satisfied}` (EXTENDING the keeper `requiredKeeperValue` shape
  with a `Description` field) — that feeds BOTH (a) the missing-key error, rendered `KeyPath — Description` per
  line (carrier becomes `Missing []struct{KeyPath, Description}`, NOT keeper's `[]string`), AND (b) the generated
  `--example` block; **plus a parity done-check test** asserting every key in the error path also appears with
  the SAME description in `--example`. **WE6 (follow-on) EXTENDS** that same carrier with the scheduled-task
  interval keys (`watch.digest_interval`, `watch.liveness_interval`) — it does not introduce a second carrier.
  Target error text (now backed by plumbing): `watch.digest_interval — how often the watch refreshes the
  captain's pull-digest (e.g. 30m); set in .harmonik/config.yaml or run harmonik ... config --example`.
- Jobs are registered via **`schedule add` from operator-edited config**, NOT a daemon `ensureXSchedule`
  Go-literal helper. **Template/example values (digest ~30m, liveness ~1h) live ONLY in the operator-edited
  `--example` output — never as runtime fallbacks.**
- WE7 must not deepen ops-monitor's own pre-existing hardcodes (`5m` / `STALE=150` / `CAPTAIN_ABSENT=600` /
  `IMMEDIATE_COOLDOWN=1800`).

**`comms-send` action (R1) — operator ruling 3 + FOLLOW-UP B: native-only.** Schedule action kinds are today
only `spawn-crew` and `command` — there is no `comms-send` action. **Operator picked FIRST-CLASS definitively:
build the native `comms-send` schedule ACTION kind.** The earlier `bash -c` command-wrapper fallback
(ship-as-v1-if-materially-faster) is **DROPPED** — there is no wrapper path. The native action also sidesteps
the `bash -c`-in-JSON quoting-bug class (cf. SSHRunner `#{pane_id}` truncation, wake pane-hash bugs), so it is
the safer surface as well as the chosen one. This is a **FOLLOW-ON bead (WE6)** — off the MVP wake-burn-cut
path (§11).

## 8. Launch & keep-alive (Q5) + model (Q6) — gated on a live keeper

**Decision: the watch is a crew launched via the existing crew+keeper machinery, on Sonnet — with a keeper-doctor
launch gate, because the watch is a SPOF (critic 2 #3 + critic 3 #5a).**

- `harmonik start crew watch --queue watch-q --mission .harmonik/crew/missions/watch.md`
- Mission frontmatter `model: sonnet` (the per-crew model field injects `--model`; confirmed
  `crewstart.go:244,252 → crewlaunchspec.go:120-122`).
- Dedicated epic (`watch` anchor) + queue; survives keeper restart via durable queue + beads assignee
  re-hydration (crew-handoff-schema.md:159-169).
- **WE8 launch gate (load-bearing):** crew-start does NOT reliably auto-launch a keeper watcher
  (memory `reference_crew_start_no_auto_keeper_watcher`; plus the keeper statusLine gauge ships OFF for crews
  on the live deploy — KNOWN DRIFT). So WE8 MUST: run `keeper enable --agent watch --tmux <T>
  --yes-destructive`, then **verify `keeper doctor watch` green** before declaring the watch up. A
  keeper-less watch silently dies → captain starved. (If the gauge can't be made reliable for the watch, fall back
  to token-light wake-tasks so context fill is slow — but the doctor gate is the primary requirement.)
- **Respawn after host reboot/kill (critic 2 #3b):** there is no in-daemon crew auto-respawn
  (`crewstart.go:281-284`). The respawn owner is **§5: ops-monitor detects watch-down → escalates → the captain
  respawns it** (`harmonik start crew watch ...`). Stated explicitly so nothing silently fails to
  revive.
- **Model:** **Sonnet** — the watch's job is triage judgment + summarization. Deterministic checks are
  ops-monitor's (bash) already. (Revisit Haiku later with measurement.)
- **Distinction from a normal crew:** a crew drains a bead queue; the watch's "work" is the monitor/triage loop.
  Its queue is mostly idle (used for scheduled wake-tasks). **Idle is SAFE** — critic 2 confirmed no path
  tears down an idle watch: quiesce never kills sessions and is not empty-queue-triggered
  (`quiesce.go:430,775`); orphan-sweep is tmux/PID-based, exempts live crews, boot-only
  (`orphansweep.go:592-659`); keeper has no idle teardown and cycle preserves `session_id`. **Caveat
  (critic 1 m1):** `ops-monitor-check.sh:655` (line verified round-4 C2) excludes {captain, ops-monitor,
  ctx-watchdog, daemon, operator} from crew-stale but NOT the watch — WE7/WE8 MUST add `watch` to that
  `NON_CREW` exclusion or the watch self-alerts as stale.

## 9. Preserve Opus-only judgment (O7)

Encoded as the §2 boundary and reasserted in the captain skill: the watch flags, the captain decides, for
crew-failure/kill, new-initiative ranking, locked-decision reversal, destructive ops, and staffing. No
escalation summary is a directive — it always names the decision for the captain to make.

## 10. Expected outcome — honest per-source ledger (critic 1 M3 — fix #7)

Captain-wake budget restated per source, with the residual *after CE4 and after this work*. "~90%" holds
**only if the ops-monitor redirect (§6.1) and the pull-digest (§4) both land** — the original "~90%" assumed
prose redirects that don't exist.

| Wake source | Today (~/day) | After CE4 only | After wake-economy | How |
|---|---|---|---|---|
| 12-min `/loop` health tick | ~120 | ~120 (invocation still re-armed) | **0** | §6.4 removes the loop from 4 files |
| per-crew status churn | ~120–250 | ~120–250 | **~0** | §6.1 crews → `--to watch`; watch batches to PULL-digest |
| ops-monitor DIGEST/non-critical IMMEDIATE | (folded above) | direct to captain | **~0** | §6.1 redirect → watch |
| ops-monitor most-urgent IMMEDIATE (daemon/supervisor/paused) | few | few | **few (unchanged, by design)** | §4 DIRECT bypass — must NOT drop |
| `epic_completed` | ~5–10 | ~5–10 | **~5–10 (unchanged)** | stays on captain direct subscribe (§4 — avoid triple-wake) |
| real escalations needing judgment | tens | tens | **tens** | the genuine-decision rate — this is the floor |
| operator messages | as they come | as they come | **unchanged** | O5 direct (§6.3) |

(The "ops-monitor DIGEST/non-critical IMMEDIATE" row is **the same physical comms traffic** counted in the
crew/timer churn it rides alongside — "(folded above)" means it is not an independent additive slice.)

Net: the two dominant churn sources (12-min timer ~120/day + crew churn ~120–250/day) go to ~0; the residual
is the genuine-decision rate (epic_completed + real escalations + operator) ≈ tens/day. That is the ~90%
reduction — **contingent on §6.1 and §4 landing**, not on prose. §6.1 (redirect = WE7), the `/loop` removal
(§6.4 = WE4), and the escalation/pull-digest (§4 = WE3) are **all MVP beads**, so the cut arrives with the MVP
rollout — the follow-on group (WE6/WE9/WE10) does not gate it.

## 11. Implementation breakdown (beads, label `codename:wake-economy`) — MVP rollout + follow-ons

**Operator follow-up A — the sequencing is load-bearing.** The wake-burn cut comes from redirecting the routine
senders to the watch. A redirect that lands with **no watch online to drain it** sends every crew completion +
ops alert into the event log unseen → the captain stops re-tasking → the fleet stalls. So the redirect and the
watch standup are **coupled into one coordinated MVP rollout**: stand the watch up + verify it live, THEN flip
the senders to it. Native scheduled-send, full mutual-liveness hardening, and ledger polish are **deferred to
follow-on beads** — they sharpen the tier but are off the wake-burn-cut critical path.

**Coupling guarantee (no blind-captain window) — default-safe, not just config-driven (round-4 C1-B1).** The two
sender redirect-target keys (`watch.status_target` for the crew status feed, `watch.opsmonitor_target` for the
ops-monitor watch-class send) have a **post-edit DEFAULT of `captain`** — NOT `watch`, and (uniquely) NOT
config-or-fail-loud. WE4 + WE7 merging changes the *code path* to read the key, but the resolved value stays
`captain` until an operator explicitly sets it to `watch`. (Config-or-fail-loud per §7 governs the watch's OWN
behavioral keys — `watch.digest_interval`, `watch.liveness_interval`, `watch.escalation_target` — NOT these two
redirect-target keys, which MUST default-safe to `captain` so a merged-but-unflipped redirect is provably inert;
a fail-loud redirect target would instead crash the senders on merge and drop crew completions — the same
blind-captain window by another door.) The flip `captain`→`watch` is the rollout's final runtime step, performed
ONLY after the standup beads land AND `keeper doctor watch` is verified green. Because the pre-flip resolved
target is `captain`, the senders point at the captain unchanged until that flip — the standalone-redirect hazard
operator follow-up A forbids cannot occur, regardless of merge order.

**Which blind-captain windows the MVP closes — and the one it does NOT (round-4 C1-M1, honest).** A watch that
is *redirected-to but dead* is the SAME blind-captain failure as a redirect with no watch, so the MVP keeps
**basic watch-down detection** (WE7/WE8: add the watch to ops-monitor's critical-component check →
process/tmux-down escalates → captain respawns, §8; + the `NON_CREW` self-stale exclusion). This closes the
**no-watch** and **dead-watch** windows. It does **NOT** close the **alive-but-stalled** window — a bus-pinned
watch with a frozen escalation cursor passes basic process/tmux liveness while silently buffering escalations
(§5, critic-3 #5b). That residual is ACCEPTED for the MVP window **on the condition that WE9** (dual-probe +
escalation-cursor-advancement) **is the FIRST follow-on bead after the MVP rollout**, not deferred indefinitely.
Until WE9 lands, the captain's ops-monitor captain-liveness probe (§6.4 backstop) is the only — partial —
backstop: a stalled watch that stops escalating does not stop the captain from being probed as alive, so this is
a real, time-boxed residual, stated so the operator sees it rather than a buried gap.

**Bead numbers are stable IDs** (cross-referenced throughout §5–§8); the MVP/follow-on split is a PHASE overlay,
NOT a renumber. Build order within the MVP is the coordinated rollout in the table.

| Phase | Beads | Build order |
|---|---|---|
| **MVP — standup** | WE1, WE2, WE3, WE5, WE8 | land first; then launch the watch crew + verify `keeper doctor watch` green |
| **MVP — redirect** (config-gated, flips behind the verified watch) | WE4, WE7 | land + flip the config target `captain`→`watch` as the rollout's final step |
| **Follow-on** (off the wake-burn-cut path) | WE6, WE9, WE10 | after the MVP rollout |

- **WE1** *(MVP)* — watch skill + mission template (role, ledger, taxonomy, boundary — §2). *(skill asset; +embed mirror)*
- **WE2** *(MVP)* — event ledger (MVP scope): cursor file + a minimal `.harmonik/watch/latest.json` summary
  digest + `event_id` dedupe; read-pure `ScanAfter`/`comms log` (no recv-cursor contamination); consume
  `subscription_gap` by re-scan; reads `latest.json` only on ops-monitor `[IMMEDIATE]/[DIGEST]` receipt, never on
  a timer. *(Richer per-lane query surface = follow-on WE10.)*
- **WE3** *(MVP)* — escalation engine: taxonomy partition (§4) + actionable-summary send + **PULL-digest** (no
  timed send); `epic_completed` ledger-only.
- **WE4** *(MVP — redirect/captain-side, config-gated)* — captain skill rewrite (O6): remove `/loop`, plain
  unfiltered recv (NO client-side routing — §6.3), new watcher set, resume/wake re-arm edits + embed mirror.
  **Done-check: `grep=0 "/loop"` across 4 files + `cp` repo→embed + `go test ./cmd/harmonik/`.** Goes live with
  the WE7 redirect flip (behind the verified watch).
- **WE5** *(MVP)* — daemon QuiesceArbiter (§6.2; rescoped per operator ruling 2 — client-side routing DROPPED):
  extend `handleAgentMessage`/`wakeSignal`/`executeWake` to wake a parked **watch** on `to==watch` via a
  wake-by-name target field (not a one-liner — `executeWake` has only a `captainWake bool` today); captain recv
  stays plain `comms recv --agent captain` with NO filter; RED test for the watch-wake path.
- **WE6** *(FOLLOW-ON)* — scheduled tasks: register hourly mutual-liveness + "verify services up" via
  `harmonik schedule` with **config-or-fail-loud intervals/targets** (NO Go literals — and do NOT copy
  `opsmonitor_schedule.go` / `ctx_watchdog_schedule.go` / `seedGoalKeeperSchedule`). **Scheduled-send uses the
  native `comms-send` action ONLY** (ruling 3 / follow-up B — the `bash -c` wrapper is DROPPED). **EXTENDS** the
  WE7 `requiredWatchValue{KeyPath, Description, satisfied}` carrier with the interval keys
  (`watch.digest_interval`, `watch.liveness_interval`) + their descriptions + the parity test (every error-path
  key appears with the same description in `--example`) — ruling 4.
- **WE7** *(MVP — redirect-side, config-gated)* — sender-redirect + basic watch-liveness:
  - **PARTITION** ops-monitor's batched IMMEDIATE into a **direct-class** send (`--to captain`:
    daemon/supervisor/paused — the §4 SPOF bypass) + a **watch-class** send (everything else + DIGEST → the
    config target) — restructure `send_comms`, NOT a target swap (§6.1).
  - redirect the crew status feed in `crew-launch/SKILL.md` (repo+embed) to the `watch.status_target` key +
    its own `cp` + `go test ./cmd/harmonik/` grep done-check.
  - the two redirect-target keys (`watch.status_target`, `watch.opsmonitor_target`) **default to `captain`**
    (NOT fail-loud — §7 exception, round-4 C1-B1) so the merged redirect is provably inert until the operator
    flip; done-check asserts the un-set default resolves to `captain`. WE7 **INTRODUCES** the single-source
    per-setting DESCRIPTION carrier `requiredWatchValue{KeyPath, Description, satisfied}` (extends keeper's
    `requiredKeeperValue` with a `Description` field; carrier `[]struct{KeyPath,Description}`, not `[]string`)
    feeding BOTH the missing-key error (`KeyPath — Description` lines) AND `--example` + a parity test. (WE6
    later extends it with the interval keys.)
  - **basic watch-liveness:** add `watch` to the `NON_CREW` self-stale exclusion (`:655`, line verified round-4
    C2, single set-literal add covering both the crew-stale `:659` and keeper-coverage `:702` loops) + the basic
    critical-component process/tmux-down escalate. (The alive-but-stalled dual-probe + cursor-advancement is
    follow-on WE9.)
  - **Rollout gate (operator follow-up A):** the config target flips `captain`→`watch` (and WE4's captain-skill
    rewrite goes live) ONLY after the MVP-standup beads land and `keeper doctor watch` is verified green.
    Done-check asserts the flip is documented + gated.
- **WE8** *(MVP — standup)* — launch wiring: watch crew mission `model: sonnet`, dedicated epic + `watch-q`,
  **keeper-doctor launch gate** (`keeper enable --yes-destructive` + verify `keeper doctor`), ops-monitor→captain
  respawn owner documented, restart-survival verification.
- **WE9** *(FOLLOW-ON)* — full mutual-liveness hardening: add the watch as a tracked critical component (the
  **~4-site hand-edit** — probe block + present/down derivation + signal-append + checks-map entry +
  `CRITICAL_PREFIXES` tuple, NOT a list-append); **dual-probe + escalation-cursor-advancement** watch-down
  detection (alive-but-stalled; cursor stored in `state.json`, reusing the `prev_*_misses` consecutive-miss
  machinery); the hourly **bidirectional** mutual-liveness ping via the WE6 native schedule action
  (config-or-fail-loud interval). *(Basic watch-down is already in WE7.)*
- **WE10** *(FOLLOW-ON)* — ledger polish: richer per-lane query surface + summary-digest refinement beyond the
  WE2 minimal `latest.json` (the "what happened in lane X" queries — §3).

Each WE bead carries root-cause/spec + RED test + done-check, dispatched serialized through the daemon DOT
review-loop on paul's queue. The **MVP rollout** (WE1–WE5, WE7, WE8) is built first as the coordinated unit; the
**follow-on group** (WE6, WE9, WE10) after.

## 12. Operator decisions — RULED 2026-06-24 (verbatim)

1. **Name = `watch`** (informal) — for the role, skill, mission, and liveness-component (§0).
2. **Comms-filter = SENDER-REDIRECT** (§6.1/§6.3). The client-side filter (and the server-side `--from`
   alternative) are DROPPED — they solved a non-problem (`comms recv --agent X` already delivers only
   `to==X` + broadcasts, so the captain is not on a firehose). Redirect every routine `--to captain` sender
   (crew status templates, `ops-monitor`, daemon QuiesceArbiter Go) to `--to watch`; the operator STAYS
   `--to captain` (O5 by addressing, not filtering); the watch observes ALL traffic via the comms event-log
   ledger and pings the captain only on action. NO client-side LLM filtering.
3. **Scheduled comms-send = native first-class `comms-send` action** (§7) — operator FOLLOW-UP B picked this
   definitively; the `bash -c` command-wrapper fallback is **DROPPED** (no wrapper path). FOLLOW-ON bead (WE6).
4. **Intervals = config-or-fail-loud, zero product defaults** (§7) — CONFIRMED, plus: every setting's
   fail-loud error AND config schema MUST carry a DESCRIPTION of what it does (optimize for agent
   setup/tuning); recommended template values (digest ~30m, liveness ~1h) ship in operator-edited config,
   never as runtime literals.

**Operator FOLLOW-UPS — 2026-06-24 (post-round-3, verbatim intent):**
- **(A) Couple the MVP.** Do NOT ship the `--to captain`→`--to watch` sender-redirect standalone — with no
  watch online to drain it, crew completions + ops alerts land only in the event log, the captain goes blind
  (no re-tasking → fleet stalls). The watch MVP stands up the watch session AND redirects the routine senders in
  ONE coordinated rollout (§11 MVP group, config-gated flip behind a verified-live watch). Defer native
  scheduled-send + full mutual-liveness + ledger polish to FOLLOW-ON beads. **Basic watch-down detection stays
  IN the MVP** — a redirected-to but dead watch is the same blind-captain hazard (§11 preamble + §5 phase split).
- **(B) Scheduled-send native-only** (ruling 3) — the bash-wrapper option is DROPPED entirely.

(`hk-drygf` stays HELD/untouched. ROUND-4 CRITIC GATE on this revised sequencing is **CLEAN** — C1 REVISE (all
transcribed fixes applied: default-safe `captain` redirect target, concrete ops-monitor heartbeat backstop,
honest alive-but-stalled MVP residual with WE9 as first follow-on), C2 + C3 APPROVE. Build the MVP group on
captain GO.)
