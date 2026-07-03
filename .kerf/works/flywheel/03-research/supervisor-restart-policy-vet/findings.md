# Vet — Supervisor restart-policy port (gateway leverage item #2)

> Component: `supervisor-restart-policy-vet`. Round-4 vet. Source: sub-agent (opus), 2026-05-30. **Verdict: YES-TRIMMED. Real wins (max-restarts cap + exp backoff + crash-loop guard); drop 3 things (HTTP health-check, in-process log ring, `restartPolicy=always`).**

## TL;DR
- TS supervisor (~600 LOC) is the right anchor for harmonik's `--watch-restart` shim; the 30-50 LOC placeholder in PL-019(f) materially undersells what we need.
- Port as `internal/supervise/` (~250-350 LOC Go), but **trim 3 things**: drop HTTP `healthEndpoint` (replace with `kill(pid,0)` + heartbeat-file freshness — supervisee is a tmux-resident Pi extension, not an HTTP service); drop in-process log ring (tmux pane is v1's log surface per PL-028d "the pane IS the log"); drop `restartPolicy=always` (flywheel exits 0 by intent — operator never wants "restart on clean exit"; keep `on-failure | never` only).
- **2 beads** proposed (`internal/supervise` package + `cmd/harmonik/supervise/start.go` wiring). All `codename:flywheel`.

## §1 — TS shape
`apps/gateway/src/services/supervisor.service.ts`:
- Class `SupervisorService` (~145-620). Constructor takes `DaemonSpec[]`; 7 Maps (daemons/processes/specs/logs/healthCheckIntervals/startingTimeouts/restartTimeouts).
- Spawn (267-272): `Bun.spawn(spec.command, {stdout:"pipe", stderr:"pipe", env, onExit: handleExit})`; `collectLogs()` pipes both streams through `readStream()` → split-on-newline → `addLog()` with secret redaction.
- `handleExit` (442-501): clears starting+health timers; state→"stopped"; evaluates `policy==="always" || (policy==="on-failure" && code!==0)`; gates on `restartCount < spec.maxRestarts`; `setTimeout(start, restartDelayMs)`.
- RestartPolicy enum (line 17): `"always" | "on-failure" | "never"`.
- Health-check ticker (503-540): `setInterval(checkHealth, 5000).unref()`; `fetch("http://localhost:{port}{healthEndpoint}")` with 3s timeout; success transitions starting→running and emits `daemon.started`. **Failures while running only log a warning — do NOT trigger restart** (restart is only via `onExit`).
- Log ring (171): `MAX_LOG_ENTRIES=1000`; `addLog()` appends + splices front on overflow (571-582); `getLogs(name, limit)` returns last-N.
- Starting-timeout (281-290): if no `healthEndpoint`, "assume-running" timer; cleared in `stopDaemon` (335) + `handleExit` (451).

## §2 — Harmonik commits (file:line)
- `05-spec-drafts/process-lifecycle.md:78-82` (PL-019(f)): `--watch-restart` normative; "lightweight wrapper shim … holds the advisory lock fd … monitors via wait(2), respawns on non-zero exit." **Spec silent on max-restarts cap, backoff, crash-loop guards — gap this port fills.**
- `process-lifecycle.md:55-67` (PL-019(e)): config snapshot `.harmonik/cognition/config.json`, atomic write per WM-026, no hot-reload. Restart-policy fields live here.
- `process-lifecycle.md:229-230` (PL-028d stop): SIGTERM→bounded-wait→SIGKILL mirrors PL-011 — port MUST use this, not Bun `kill()`.
- `process-lifecycle.md:238-240` (PL-028d logs): "tmux capture-pane … the pane IS the log surface at v1" → in-process ring redundant.
- `03-research/harmonik-supervisor-surface/findings.md:38`: invites ~30-50 LOC framing as "no external supervisor needed" — port is a richer impl of the same shim contract.
- Existing idioms: `internal/handler/session.go:303-342` battle-tested `ringBuffer`; `internal/handlercontract/provisioningretry_hc048a.go:24-44` canonical Go backoff shape (`Base`/`Cap`/`MaxAttempts`).

## §3 — Go design
**Home:** `internal/supervise/` (parallels `internal/daemon/` for separated lifecycle per PL-019(b)). Entry: `cmd/harmonik/supervise/{start,stop,status,attach,restart,logs}.go` dispatched pre-`flag.Parse` after the `queue` case (PL-028d).
```go
package supervise

type RestartPolicy string
const (
    PolicyNever     RestartPolicy = "never"      // crash → leave pane, exit shim
    PolicyOnFailure RestartPolicy = "on-failure" // restart iff exit!=0
)

type BackoffConfig struct {  // mirrors handlercontract.ProvisioningBackoffConfig
    Base        time.Duration // default 1s
    Cap         time.Duration // default 60s
    Jitter      float64       // 0..1, default 0.2
    MaxRestarts int           // default 5; -1 = unlimited
}

type Spec struct {
    Command       []string                    // argv to exec
    Env           []string
    WorkDir       string
    HeartbeatPath string                      // .harmonik/cognition/heartbeat.json
    HeartbeatTTL  time.Duration               // e.g. 90s
    Policy        RestartPolicy
    Backoff       BackoffConfig
    StartTimeout  time.Duration               // assume-running gate; default 30s
}

type State struct {
    PID          int
    Status       string    // "starting" | "running" | "stopped" | "crashloop"
    StartedAt    time.Time
    RestartCount int
    LastExitCode int
}

type Supervisor struct {
    spec   Spec
    cmd    *exec.Cmd
    state  atomic.Pointer[State]
    sigCh  chan os.Signal      // forwards SIGTERM/SIGINT to child
    doneCh chan struct{}
    log    *slog.Logger
}

func New(spec Spec, log *slog.Logger) *Supervisor
func (s *Supervisor) Run(ctx context.Context) error  // blocking; lock-fd already held by caller
func (s *Supervisor) Snapshot() State
func (s *Supervisor) Stop(timeout time.Duration) error  // PL-011: SIGTERM → wait → SIGKILL
```
**Internals:** `Run()` is a for-loop. Each iteration: build `exec.Cmd` with `Setpgid:true`, wire stdout/stderr to os.Stdout/os.Stderr (tmux pane absorbs them — no in-process ring), `Start()`, transition "starting", `time.AfterFunc(StartTimeout, →"running")`, `cmd.Wait()` in a goroutine, `select` on `wait|ctx.Done|sigCh`. On wait: evaluate policy+count; if restart, `time.Sleep(backoff_with_jitter)`, continue; else return. **Crash-loop guard:** if MaxRestarts hit within sliding window (e.g. 5 restarts in 60s), mark "crashloop" and exit — operator sees stopped pane via `remain-on-exit`.
**Health probe** (replaces TS HTTP fetch): `time.Ticker(15s)` goroutine doing two cheap checks: (a) `syscall.Kill(pid, 0)` for process liveness; (b) `os.Stat(HeartbeatPath)` + age check vs `HeartbeatTTL`. Health failures **do not** force restart (matches TS); only update `State.Status` + emit structured log for `harmonik supervise status` to surface. The process-exit path is the only restart trigger.
**Caller:** `cmd/harmonik/supervise/start.go` acquires flock at `.harmonik/cognition/supervisor.lock` (PL-019(c)), writes `supervisor.pid` + `supervisor.sentinel` (PL-006d), atomic-writes `config.json`, then either (a) `--detach`: re-execs self in shim mode under tmux + returns, or (b) foreground: `supervise.New(spec).Run(ctx)` directly. Shim is lock-holder; supervisee is its child.

## §4 — Vet (real wins vs premature)
- **max-restarts cap + exp backoff** — KEEP. Naive `for { cmd.Wait(); cmd.Start() }` pins CPU + daemon socket when supervisee is broken. HC-048a already enshrines `Base/Cap/MaxAttempts` for transient retries.
- **restart policy enum** — TRIM from 3 to 2: drop `always`. Flywheel exits 0 by intent (operator-initiated stop or budget end); neither warrants automatic restart. `on-failure`+`never` cover real choices; map cleanly to one flag.
- **log ring buffer** — DROP. PL-028d settles it: "the pane IS the log surface at v1." In-process ring = second source of truth competing with tmux scrollback.
- **starting-timeout** — KEEP, rename to "assume-running gate." Without it, a supervisee that successfully `exec()`s but immediately wedges never transitions starting→running, confusing `supervise status`.
- **HTTP `healthEndpoint`** — DROP entirely. Supervisee is a Pi extension in tmux, not HTTP. Replace with `kill(pid,0)` + heartbeat-file freshness (`.harmonik/cognition/heartbeat.json` per PL-019(g)). Pi extension is already writing this file for `supervise status`; reuse.
- **Verdict: yes-trimmed.** Port spawn loop + onExit + restart policy + backoff + max-restarts + crash-loop guard + starting-timeout (~250-350 LOC). Drop HTTP health-check (~80 LOC), in-process log ring (~50 LOC), `always` policy. Net ~half the TS LOC, none of the value lost.

## §5 — Proposed beads (2, all `codename:flywheel`, type=feature, priority=2)

**Bead 1 — `internal/supervise` package:**
```
br create --title="Port flywheel_gateway supervisor.service.ts → internal/supervise (richer --watch-restart shim)" \
  --type=feature --priority=2
# Description:
# Replace "~30-50 LOC wrapper shim" placeholder in PL-019(f) (process-lifecycle.md:78-82) with a real Go port of
# Dicklesworthstone's apps/gateway/src/services/supervisor.service.ts.
#
# Scope: new package internal/supervise/ exporting:
# - Supervisor{spec, state, ...} with New/Run/Stop/Snapshot
# - RestartPolicy enum ("on-failure" | "never" — drop TS's "always")
# - BackoffConfig{Base, Cap, Jitter, MaxRestarts} mirroring handlercontract ProvisioningBackoffConfig
# - Health probe: kill(pid,0) + .harmonik/cognition/heartbeat.json freshness (NOT HTTP)
# - Crash-loop guard: N restarts within sliding window → "crashloop" state, exit
# Out of scope: in-process log ring (pane IS the log per PL-028d:238-240); CLI surface (separate bead).
#
# Acceptance:
# - internal/supervise/supervisor.go compiles, vet+lint clean.
# - Run(ctx) handles exit→backoff→respawn loop with MaxRestarts cap.
# - SIGTERM forwarded to child; Stop(timeout) follows PL-011 SIGTERM→bounded-wait→SIGKILL.
# - Test: simulated child exiting code=1 three times → 3 restarts with monotonic backoff;
#   child exiting code=0 with policy=on-failure → no restart;
#   MaxRestarts=2 → 2 restarts then "crashloop" + Run() returns.
# - Test: heartbeat-file staleness → state.Status="unhealthy", no restart.
#
# Refs: PL-019(f), HC-048a backoff idiom.
# Labels: codename:flywheel, supervise
```

**Bead 2 — CLI wiring:**
```
br create --title="Wire harmonik supervise start --watch-restart to use internal/supervise.Supervisor" \
  --type=feature --priority=2
# Description:
# Replace start.go's placeholder shim path (PL-019(f) "lightweight wrapper") with a real call into
# internal/supervise.New(spec).Run(ctx) when --watch-restart is set. start.go remains lock-holder (PL-019(c))
# + sentinel-writer (PL-006d); supervise.Supervisor is its in-process child manager.
#
# Scope:
# - cmd/harmonik/supervise/start.go: build supervise.Spec from config.json (RestartPolicy from new config field;
#   default on-failure when --watch-restart, never otherwise)
# - Add config.json fields: restart_policy, restart_max, restart_base_ms, restart_cap_ms
#   (schema_version bump per ON-018 N-1 rule)
# - --detach mode: re-exec self under tmux with --internal-shim flag → that invocation calls
#   supervise.New(spec).Run(ctx) in foreground
# - supervise stop: SIGTERM to shim PID; shim forwards to child per PL-011
# Out of scope: status/attach/restart/logs verbs (already wired against file-surface; unchanged).
#
# Acceptance:
# - harmonik supervise start --watch-restart launches tmux pane in which crashed supervisee respawns up to
#   restart_max times with backoff.
# - harmonik supervise stop cleanly terminates both shim and child within timeout; flock released; sentinel
#   removed.
# - Integration test under internal/testhelpers: fake supervisee binary exits code=1 → shim restarts 2x then
#   enters crashloop state visible via supervise status --json.
# - config.json schema_version bumped; N-1 readers tolerate new fields.
#
# Depends on: bead 1 (internal/supervise package).
# Refs: PL-019(b,c,e,f), PL-028d, PL-006d sentinel discipline.
# Labels: codename:flywheel, supervise, cli
```
