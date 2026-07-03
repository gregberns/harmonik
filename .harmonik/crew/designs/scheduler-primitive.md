# Design note — `harmonik schedule`: a generic recurring-job primitive

**Epic** hk-rqq · **impl bead** hk-0es · **crew** leto · **2026-06-13**
**Status:** DRAFT — surfaced to captain for OK before build (spec-first).

> Generic, product-grade scheduling primitive. A scheduled job = `{schedule, action}`.
> NOT tied to crew-member names, NOT logmine-specific. logmine daily@noon-local is the
> FIRST CONSUMER, wired as data (one schedule row), with zero "logmine"/"liet" in the
> primitive's code. harmonik OWNS the launch: the daemon's supervise loop fires schedules
> and manages the spawned job lifecycle (fresh process per fire). Option B (durable, no OS
> scheduler) over the launchd/cron stopgap.

---

## 1. Schedule spec

A small structured schedule, discriminated by `kind`, designed to extend without breaking
stored rows:

```yaml
schedule:
  kind: daily          # v1 ships "daily"; "cron" and "interval" are planned extensions
  at:   "12:00"        # HH:MM, 24h
  tz:   "local"        # "local" = daemon host TZ; or an IANA name e.g. "America/New_York"
```

- **"noon local"** = `{kind: daily, at: "12:00", tz: "local"}`.
- **Next-fire computation:** take "today at HH:MM in `tz`"; if already past, roll to tomorrow.
  `tz: "local"` resolves to `time.Local`; an explicit IANA name via `time.LoadLocation`.
- **Storage discipline (code-aligned):** the daemon stores no `time.Time` on disk — it keeps
  `next_fire` / `last_fire` as **RFC3339 UTC strings** (matches the existing convention:
  `core/snapshottoken.go`, `crew.Record.StartedAt`). Local time is computed only at fire-check
  and at display.

**DECISION FOR CAPTAIN/OPERATOR (D1):** v1 = `daily@HH:MM + tz` only; `cron`-expression `kind`
deferred (the `kind` discriminator reserves the slot). Rationale: cron parsing is a rabbit hole
and over-general for the first consumer; the *generic* requirement the operator set is about the
**action** being un-coupled from crew names, not maximal schedule expressiveness. **OK to ship
daily-only v1?**

## 2. Action spec — the genericity that matters

The action is a **tagged union**; the primitive code knows only action *kinds*, never a crew name:

```yaml
# kind=command — run an arbitrary argv as a fresh detached process (fully generic)
action: { kind: command, argv: ["/path/to/script.sh", "arg1"] }

# kind=spawn-crew — invoke the SAME subscription-billed crew-start path the daemon already uses
action: { kind: spawn-crew, crew: "liet", queue: "liet-q", mission: "<path>" }
```

- **`command`** — daemon runs an arbitrary argv; the most generic action. Inherits the daemon's
  sanitized env (no credential keys → credit-safe, see §5).
- **`spawn-crew`** — daemon calls the existing programmatic crew-spawn entry point
  (`crewstart.go:142 HandleCrewStart` → `buildCrewLaunchSpec` → `substrate.SpawnCrewSession`).
  This reuse is the whole design: a scheduled crew spawn is **identical** to a hand-typed
  `harmonik crew start`, so it is subscription-billed (`claude --remote-control`, never `claude -p`)
  and budget-guarded **by construction** (§5).

The logmine job is then just a config row with `kind: spawn-crew, crew: liet`. **There is no
"liet" or "logmine" symbol anywhere in the primitive.** Any crew, or any command, is schedulable.
This satisfies "generic, not crew-tied" while preserving v1 trigger condition T1 (subscription-billed
crew spawn) for the logmine consumer.

## 3. Persistence

- **Store:** `.harmonik/schedules.json` — a list of `ScheduledJob` records, loaded at daemon boot,
  atomic-write on every mutation. Survives daemon restart.
- **Pattern (code-aligned):** mirror `internal/daemon/queuestore_hkj808w.go:23-95` `QueueStore` —
  a `ScheduleStore` wrapping an id-keyed `map[string]*ScheduledJob` under a `sync.RWMutex`
  single-writer (QM-060), atomic JSON persist (like `queue.Queue.Persist()`), and a wake channel
  signalled on mutation so the supervise loop reloads immediately (no restart needed).
- **Why a file, not beads:** schedules are *daemon config that produces work*, not work-items.
  Beads are the work ledger; a schedule is a recurring trigger. Same rationale that keeps
  `queue.json` out of beads.

Each record carries `last_fire` (RFC3339 UTC) for the missed-run logic (§4).

## 4. Overlap + missed-run handling

Two orthogonal concerns, each a per-job policy field:

**Overlap** — `overlap_policy: skip | allow` (default **skip**). "Is the prior run still active?" is
action-kind-specific:
- `spawn-crew`: reuse the **validated v1 guard** — `harmonik comms who --json`, skip the fire iff
  a row for the crew has `status == "online"` (NOT a name-grep — `who` also lists `stale`/dead rows).
  This is v1 trigger condition T2, preserved verbatim.
- `command`: track the spawned PID in the job record; skip if the prior process is still alive.

**Missed-run / catch-up** — the daemon (now the scheduler) can't fire while it's down, so a noon
fire can be missed. Policy `catchup: coalesce-within-window | off` (default **coalesce-within-window**):
- On boot and each tick, if a scheduled fire-time fell between `last_fire` and now, fire **at most
  ONE** catch-up run (coalesced — NOT one-per-missed-day) **and only if** the most recent missed
  fire is within `catchup_window` (default = the schedule interval, i.e. 24h for daily).
- Beyond the window: skip and log. **This bounds credit exposure** — a daemon down for a week
  triggers ONE catch-up (today's missed noon), never seven backfill spawns.

**DECISION FOR CAPTAIN/OPERATOR (D2):** catch-up = **coalesce-one-within-24h** (recommended) vs.
never-catch-up. This is the replacement for v1 trigger condition T4 (daemon-down degrade): in v1 the
OS scheduler fired even with the daemon down (harvest ran read-only); now the daemon owns the fire,
so a down daemon = no fire, and the bounded catch-up restores the day's run on next boot. **Semantic
shift worth a nod — OK?**

## 5. Billing / credit guard (LOAD-BEARING)

A scheduled spawn MUST pass through the **exact same guard** as a queue-dispatched / hand-typed spawn.
The design enforces this by **reuse, not re-implementation**:
- `spawn-crew` calls `buildCrewLaunchSpec()` + `substrate.SpawnCrewSession()` — the same path
  `harmonik crew start` uses — so the subscription path (`claude --remote-control`) and the
  no-credential-keys baseEnv (`claudelaunchspec.go:90-93`; codex `codexbillingguard.go:12-20`
  `forced_login_method=chatgpt`) apply **unchanged**. The scheduler NEVER `exec`s claude directly.
- `command` actions inherit the daemon's sanitized env (no `ANTHROPIC_API_KEY` leak — the
  2026-05-30 credit-burn fix). Documented: scheduled commands run credit-safe by default; an
  operator who scripts a metered call in a `command` action owns that choice.

## 6. CLI surface — `harmonik schedule`

```
harmonik schedule add --id <id> --schedule "daily@12:00 local" \
      --action spawn-crew --crew <name> --queue <q> --mission <path>
harmonik schedule add --id <id> --schedule "daily@09:30 local" \
      --action command -- /path/to/script.sh arg1
harmonik schedule list [--json]        # id, next-fire, last-fire, enabled, action summary
harmonik schedule remove <id>
harmonik schedule enable <id> | disable <id>
harmonik schedule run-now <id>         # fire immediately (ad-hoc/test); still honors overlap guard
```

- **Registration (code-aligned):** a `schedule` case in the `cmd/harmonik/main.go` `os.Args[1]`
  switch (~line 510, mirroring the `queue` subsystem), dispatching to a new `internal/schedule` pkg.
- Verbs talk to the daemon if up (mutate the in-memory store + signal the wake channel); if the
  daemon is down they mutate `.harmonik/schedules.json` for next boot. `run-now` mirrors the v1
  "manual fire" integration test.

## 7. Supervise-loop hook (code-aligned)

The schedule check lives **inside** `runWorkLoop()` (`internal/daemon/workloop.go:933`), hooked after
the dispatch-context check (~line 1038) and before the capacity gate — **NOT** a parallel goroutine.
Rationale (from the code map): living in the work loop reuses the existing capacity gate and
claim-write serialization (QM-060 / hk-e61c3.3) rather than racing them. Each pass: for every enabled
job, if `now >= next_fire` and the overlap policy allows, fire the action, set `last_fire`, recompute
`next_fire`, persist. The loop's existing sleep/wake (`workloopPollInterval = 2s`, wake-channel at
~line 1059) gives sub-minute fire latency with no new ticker.

## 8. First consumer — logmine daily@noon-local (data, not code)

```json
{ "id": "logmine-daily",
  "schedule": { "kind": "daily", "at": "12:00", "tz": "local" },
  "action":   { "kind": "spawn-crew", "crew": "liet", "queue": "liet-q",
                "mission": ".harmonik/crew/missions/liet.md" },
  "overlap_policy": "skip",
  "catchup": "coalesce-within-window" }
```

Installed with one `harmonik schedule add`. Replaces `scripts/hk-logmine-daily.sh` + the launchd
plist. Preserves every v1 trigger condition:
- **T1** subscription-billed spawn → via the reused crew-start path (§5).
- **T2** overlap guard (`comms who` `status==online`) → `overlap_policy: skip` (§4).
- **T3** high-water cursor → unchanged; lives in the harvest's `findings-iterN.md` footer.
- **T4** daemon-down degrade → replaced by bounded catch-up (§4, D2).
- **T5** fresh-spawn default → each fire is a fresh crew, clean context.

## 9. Build plan (after captain OK)

Daemon/supervise-loop code; scenario/real-daemon tests exceed the daemon's 30-min commit budget →
author via a **worktree sub-agent** (no cap), fast-gate (`go build ./...`, `go test -short ./...`),
run scenario tests myself, cherry-pick to main in a daemon lull, push, `br close` (daemon-owned),
report to captain. Coordinate the push window with **stilgar** (hk-tigaf.11 per-queue spend caps also
touches queue infra) via captain.

**Open decisions for captain/operator:** D1 (daily-only v1 vs. cron now), D2 (catch-up coalesce-within-24h
vs. off). Recommended answers: D1 daily-only, D2 coalesce-within-24h. Build proceeds on OK.
