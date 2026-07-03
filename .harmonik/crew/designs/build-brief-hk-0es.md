# Build brief — hk-0es `harmonik schedule` (READY TO DISPATCH on captain/operator OK)

This is the self-contained prompt for the **worktree build sub-agent**. The worktree is a fresh
checkout of `main`, so the design note under gitignored `.harmonik/` will NOT be present — this brief
carries the full design inline. Defaults assume **D1 = daily-only v1** and **D2 = coalesce-within-24h**
(my recommendations); adjust the two marked spots if the OK differs.

After the agent returns its branch+SHA: leto runs scenario tests, cherry-picks to main in a daemon
lull (coordinate push window with stilgar via captain), pushes, `br close hk-0es`, reports to captain.

---

## AGENT PROMPT (paste into Agent(isolation: worktree, subagent_type: general-purpose))

You are implementing **hk-0es**: a generic `harmonik schedule` recurring-job primitive for the
harmonik daemon. You are in an isolated git worktree off `main` at /Users/gb/github/harmonik. Work
ONLY in this worktree; use `git -C <worktree-root>` / `go -C <worktree-root>` and never `cd` elsewhere.

### What you're building (full design — authoritative)

A scheduled job = `{schedule, action}`. GENERIC: zero crew-name / "logmine" symbols in the primitive.
The daemon's work loop fires due jobs; `spawn-crew` actions reuse the existing crew-start path so
billing guards apply by construction.

**Data model** — new package `internal/schedule`:
```go
type ScheduledJob struct {
    ID            string        `json:"id"`
    Schedule      Schedule      `json:"schedule"`
    Action        Action        `json:"action"`
    Enabled       bool          `json:"enabled"`
    OverlapPolicy string        `json:"overlap_policy"` // "skip" (default) | "allow"
    Catchup       string        `json:"catchup"`        // "coalesce-within-window" (default) | "off"
    CatchupWindow string        `json:"catchup_window,omitempty"` // duration; default = schedule interval (24h for daily)
    LastFire      string        `json:"last_fire,omitempty"`      // RFC3339 UTC
    LastPID       int           `json:"last_pid,omitempty"`       // for command-action overlap check
}
type Schedule struct {
    Kind string `json:"kind"` // v1: "daily" ONLY (D1). Reserve "cron"/"interval".
    At   string `json:"at"`   // "HH:MM" 24h
    TZ   string `json:"tz"`   // "local" | IANA name
}
type Action struct {
    Kind    string   `json:"kind"`              // "command" | "spawn-crew"
    Argv    []string `json:"argv,omitempty"`    // kind=command
    Crew    string   `json:"crew,omitempty"`    // kind=spawn-crew
    Queue   string   `json:"queue,omitempty"`   // kind=spawn-crew
    Mission string   `json:"mission,omitempty"` // kind=spawn-crew
}
```

**Next-fire computation** (`internal/schedule/clock.go` or similar): given a `Schedule{kind:daily,at:"HH:MM",tz}`
and a reference `time.Time`, return the next fire instant. `tz=="local"` → `time.Local`; else
`time.LoadLocation(tz)`. Compute "ref-date at HH:MM in tz"; if <= ref, add 24h. **Store next/last fire
as RFC3339 UTC strings**; do all comparisons in UTC; convert to local only at the fire-boundary
computation and at CLI display. NO `time.Time` fields persisted. (Matches `core/snapshottoken.go`,
`crew.Record.StartedAt`.) Unit-test the DST/rollover/past-time-today cases with an injected reference
time (do NOT call `time.Now()` in the pure next-fire function — take the reference as a param).

**Persistence** — `ScheduleStore`, mirror `internal/daemon/queuestore_hkj808w.go:23-95` `QueueStore`:
id-keyed `map[string]*ScheduledJob` under `sync.RWMutex` (single-writer, QM-060), atomic JSON write to
`.harmonik/schedules.json` (write-temp-then-rename, like `queue.Queue.Persist()`), `Load()` on boot,
and a wake channel signalled on every mutation so the work loop reloads without a daemon restart. Put
the store under `internal/daemon` (next to QueueStore) if it needs daemon-private types; otherwise
`internal/schedule`. Follow whatever the existing depguard component-matrix in `.golangci.yml` requires
for a new package (see `.claude/skills/go-subsystem-add` if you scaffold a new top-level package).

**Supervise-loop hook** — in `runWorkLoop()` (`internal/daemon/workloop.go:933`), add a schedule check
AFTER the dispatch-context check (~line 1038) and BEFORE the capacity gate. IN-LOOP, not a parallel
goroutine (reuse the capacity gate + claim-write serialization, per hk-e61c3.3). Each pass: for every
`Enabled` job, if `now_utc >= next_fire(job, last_fire)` AND the overlap policy permits, fire the
action, set `LastFire=now_utc`, persist. Overlap check:
- `spawn-crew`: run the equivalent of `harmonik comms who --json`, skip iff a row for `job.Action.Crew`
  has `status=="online"` (NOT a name-grep — `who` also emits stale/dead rows). Reuse the daemon's
  in-process comms presence accessor if one exists; else shell out to `comms who --json` is acceptable.
- `command`: if `LastPID` is still alive (`syscall.Kill(pid, 0)` == nil), skip.
- Catchup (D2): on boot/tick, if a fire was missed between `LastFire` and now, fire AT MOST ONE
  coalesced catch-up, and only if the most-recent missed fire is within `CatchupWindow` (default 24h
  for daily). Beyond the window: skip + log. `catchup=="off"` disables.

**Action execution**:
- `spawn-crew`: call the SAME programmatic crew-spawn entry point `harmonik crew start` uses —
  `crewstart.go:142 HandleCrewStart` → `buildCrewLaunchSpec` → `substrate.SpawnCrewSession`. Construct
  the `HandleCrewStart` request JSON from `{crew, queue, mission}`. DO NOT `exec.Command("claude", ...)`
  directly — reusing the path is what gives subscription-billing (`--remote-control`, never `claude -p`)
  and the no-credential-keys baseEnv (`claudelaunchspec.go:90-93`, codex `codexbillingguard.go:12-20`).
  Inject the `CrewHandler` into the work loop's deps at daemon composition (`daemon.go` / `main.go`
  around the workloop wiring) the same way other handlers are injected.
- `command`: `exec.Command(argv[0], argv[1:]...)` as a fresh detached process, inheriting the daemon's
  sanitized env (no `ANTHROPIC_API_KEY`). Record the PID in `LastPID`. Do not block the loop on it.

**CLI** — `harmonik schedule` subcommand. In `cmd/harmonik/main.go`, add a `schedule` case to the
`os.Args[1]` switch (~line 510, mirror the `queue` subsystem dispatch), calling
`runScheduleSubcommand(os.Args[2:]) int`. Verbs:
```
schedule add --id <id> --schedule "daily@HH:MM <tz>" --action spawn-crew --crew <c> --queue <q> --mission <p>
schedule add --id <id> --schedule "daily@HH:MM <tz>" --action command -- <argv...>
schedule list [--json]      # id, enabled, next-fire (local), last-fire, action summary
schedule remove <id>
schedule enable <id> | disable <id>
schedule run-now <id>       # fire now (test/ad-hoc), still honoring overlap policy
```
Parse `"daily@12:00 local"` → `Schedule{kind:daily, at:"12:00", tz:"local"}`. If the daemon is up,
mutate the in-memory store via its RPC/handler + signal the wake channel; if down, mutate
`.harmonik/schedules.json` directly for next boot. Match the exit-code convention of the `queue` verbs
(e.g. exit 17 when a daemon connection is required and absent — check how queuecli does it).

### Constraints / guardrails
- NO new abstraction layers beyond what's described. Match surrounding idiom (error wrapping, logging,
  struct/json tags). Read the neighbors before writing.
- The primitive MUST contain no "liet"/"logmine" literals. logmine is wired by a config row only (the
  daemon does not ship that row; it's added via `schedule add` post-merge).
- Single-writer discipline on the store (RWMutex). RFC3339-UTC on disk.
- Tests: `go build ./...` and `go test -short ./...` MUST pass. Add table-driven unit tests for
  next-fire (DST, past-time-today, tz=local vs IANA, rollover) and for the store (add/remove/enable/
  persist-reload round-trip, atomic-write). Tests that boot a real daemon → tag `//go:build scenario`
  (leto runs those separately; the daemon gate skips them). Do NOT write a scenario test that the
  fast-gate would try to run.

### Deliverable
Commit on your worktree branch with a Conventional-Commit message and a `Refs: hk-0es` trailer, e.g.:
```
feat(daemon): generic harmonik-schedule recurring-job primitive

<body: data model, ScheduleStore, workloop tick, spawn-crew reuse, CLI surface>

Refs: hk-0es
```
Then **return in your final message**: (1) the branch name, (2) the commit SHA, (3) a file-by-file
summary of what changed, (4) `go build`/`go test -short` results verbatim, (5) any scenario tests you
added (paths) for leto to run, (6) any deviations from this brief and why. Do NOT push, do NOT touch
`main`, do NOT `br close` anything.

---

## leto's post-build steps (after the agent returns)
1. `go -C /Users/gb/github/harmonik test -tags scenario -run TestSchedule ...` on the worktree branch.
2. Coordinate the push window with stilgar via captain (hk-tigaf.11 also touches queue infra).
3. In a TRUE daemon lull (0 merging): `git -C ... cherry-pick -x <sha>` onto main, `go build`+`go test -short`, push.
4. `br close hk-0es --reason "..."` (hand-landed; daemon can't dispatch this 30-min+ bead), `br comments add hk-rqq "..."`, report to captain.
5. Then wire the logmine consumer row (D-phase): `harmonik schedule add --id logmine-daily --schedule "daily@12:00 local" --action spawn-crew --crew liet --queue liet-q --mission .harmonik/crew/missions/liet.md` — verify with `schedule list`.
