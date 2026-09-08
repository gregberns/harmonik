# Keeper on tmux AND herdr — substrate abstraction plan

**Date:** 2026-09-07 (rev 2, after the live-schema pass) · **Status:** PLAN — no code yet.
Companion file: [`touchpoints.md`](touchpoints.md) — the exhaustive tmux touchpoint inventory.
Note: touchpoints.md was written before the schema pass; where a row says OPEN QUESTION, the
resolution in §5 of this file is authoritative.

Schema ground truth used in this revision: `herdr api schema --json`, protocol 22
(herdr 0.9.0, socket `~/.config/herdr/herdr.sock`) — `AgentInfo`, `PaneProcessInfo`,
`PaneSplitParams`, `AgentStartParams`, `ClientShellSurfaceSetParams`,
`ClientWindowTitleSetParams`, and the `agent.*` target-by-name surface.

## 1. Problem & goal

The operator is evaluating **herdr** (a Rust tmux replacement with a JSON control socket and
first-class agent state) as the terminal substrate for harmonik agents. The keeper — the
per-session context-fill watcher that drives handoff → `/clear` → `/session-resume` before a
pane overflows — talks to tmux directly via `os/exec` and works only there. The goal: the
keeper runs against **both** tmux and herdr, selected **per session on a given box**, not
globally; tmux behavior does not change; the abstraction lives **inside the keeper's own
boundary** (operator directive — no fleet-wide substrate refactor), so it survives the
restructure move to `tools/keeper/`.

## 2. First-pass scope (P0) — the captain CLI-direct path

The first pass ships **one capability: launch a captain on herdr through the CLI-direct
path and have the keeper restart it there.**

**Why the captain, not a crew (operator decision, Option B):** `harmonik crew start` RPCs
the daemon, and the daemon spawns the panes (`internal/daemon/crewstart.go` →
`tmuxsubstrate.go`) — crew launch is a **daemon** concern, not keeper-local. The captain
CLI launcher (`cmd/harmonik/captain.go` `runCaptainLaunchWithOps` with the
`captainTmuxOps`/`osCaptainTmuxOps` seam) creates its own session and calls
`agentlaunch.SpawnKeeperWindow` directly — no daemon involvement, and it is exactly the
`agentlaunch` surface this plan already inventories. So P0 proves the whole keeper restart
mechanism while staying inside the keeper boundary + `cmd/harmonik`, with **zero daemon
change**. Crew-on-herdr through the daemon is the **committed end-state**, sequenced LAST
as parcel KH-8 (§9).

The P0 slice:

1. resolve/target the captain agent **by name** (§4.4) —
2. detect context-fill exactly as today (gauge file + transcript; unchanged, §5.2) —
3. deliver handoff + `/clear` + resume via inject (§4.2), escalating to the two-step
   respawn (§5.1, §4.5) when injection cannot recover —
4. confirm the agent is back and working (agent status + gauge liveness).

Everything else is a **later pass**, named so nobody scope-creeps it into P0: crew-on-herdr
via the daemon (KH-8), operator-attach nuance beyond the decided §5.5 gates, the event-lean
optimization (`pane.wait_for_output`, status-event caching — §6), remote/SSH multi-box, and
pushing keeper state into herdr's sidebar (`pane.report_metadata` beyond the minimal spawn
tag). P0 is delivered by parcels KH-1 through KH-7 in §9; KH-1 and KH-2 are the first two
dispatchable parcels.

### 2.1 Validation discipline — implement, then prove, as you go

Operator's explicit instruction: every parcel is **implement-then-prove**. A parcel is not
done when its code and tests are written — it is done when its own validation has actually
been **RUN and its output shown**. Each parcel in §9 therefore carries two lines:

- **Self-check** — a concrete runnable command (never a bare "tests pass") whose observed
  output proves the parcel works; the implementer runs it before declaring done. Where a
  live herdr server is involved, the self-check uses the real socket on this box
  (`~/.config/herdr/herdr.sock` is up), not only the fake.
- **Oversight re-check** — the same or a stronger check the orchestrator re-runs
  independently on the landed `integration` commit. **No dependent parcel dispatches until
  the oversight re-check of everything it depends on has passed.**

## 3. Coordination & where the code lives

Repo state at planning time: `tools/` exists but holds only dev tooling (`commentcut`,
`lintreport`, `forbid-import`, `testreport`) — there is **no `tools/keeper`**; the keeper is
100% in `internal/keeper` on `integration`. A live harmonik daemon runs on `integration` but
is idle (no crews, no in-progress beads). Another Claude session is actively working the
restructure/kernel scaffold on branch `work/restructure-scaffold`; it has not touched keeper.

**DECISION:** build the `PaneHost` interface and both adapters **in place at
`internal/keeper/panehost/`**, in an isolated git worktree off `integration`, designed to
`git mv` cleanly into `tools/keeper/` when the restructure program reaches the keeper. Do
**not** port the keeper into `tools/keeper` as part of this work — that move belongs to the
restructure program and would collide with the kernel agent's branch. Every dispatched
parcel (§9) works in its own worktree; parcels land on `integration` in order.

**Daemon boundary:** P0 (KH-1..KH-7) touches **no daemon code** — that is what the
captain CLI-direct launch path buys (§2). The one parcel that does touch shared daemon
code, KH-8 (crew-on-herdr via the daemon), is sequenced LAST and must be coordinated with
the kernel agent on `work/restructure-scaffold` before dispatch.

## 4. Current state, and the abstraction: `PaneHost`

What exists today (full inventory in [`touchpoints.md`](touchpoints.md)):

- **The cycle core is already ported.** `CycleDeps` (`internal/keeper/cycle_deps.go`) takes
  `PaneWriter`, `OperatorPresenceProbe`, `ContextStore`, `ActivityProbe`, etc.; the pure
  reactor (`step.go`) emits `Action` values and the thin shell (`shell.go`) executes them.
  tmux enters only through the default wiring (`configPaneWriter` → `injectTextClocked`,
  `CycleDepsFromConfig` → `OperatorAttached`).
- **The watcher is seam-rich but tmux-defaulted.** Every `WatcherConfig` `*Fn` seam
  (`InjectFn`, `IsPaneIdleFn`, `IsPaneAliveFn`, `OperatorAttachedFn`,
  `ResolveTmuxTargetFn`, …) defaults to a tmux free function in the same package. The
  refactor is "give the defaults one owner", not "invent ports".
- **Five tmux verbs carry everything:** bracketed-paste inject, pane capture
  (`awaitack.go`), pane-process probe (`respawn.go`), operator-attach
  (`tmuxresolve.go`), and target resolution (naming convention + `has-session`).
- **Respawn is already substrate-opaque:** `WatcherConfig.RespawnCmd` /
  `NewLiveRecoverViaRespawn` run a launcher-supplied string via `sh -c`; the tmux knowledge
  lives in `cmd/harmonik/captain_respawn.go`, not in the keeper. The port needs no
  kill/spawn methods.
- **The trigger is not tmux:** the gauge is a file (`gauge.go` `ReadCtxFile` ← the
  statusLine hook → `.harmonik/keeper/<agent>.ctx`) and the operator-turn gates read the
  transcript tail (`recentTranscriptTurn`). Unchanged under herdr.
- **depguard** (`.golangci.yml` `keeper` rule): `internal/keeper/**` may import only
  `$gostd`, `internal/core`, `internal/eventbus`, `internal/presence`,
  `internal/substrate`, and itself; `daemon`/`workloop` denied. That is why the keeper
  shells out instead of importing `internal/lifecycle/tmux`.

### 4.1 The interface

**What it buys (PRINCIPLES.md):** a second real implementation — herdr — that exists as a
concrete target today, plus one owner for the five tmux verbs currently spread across ~11
free functions. Scoped to exactly what the keeper calls; no kill, no spawn, no layout.

```go
// internal/keeper/panehost/panehost.go (git mv's to tools/keeper/internal/panehost later)

// Target is the substrate-specific address of the agent's pane.
// tmux: "session:window". herdr: the STABLE AGENT NAME (§4.4) — never a raw pane id.
// Opaque to the keeper core; only the PaneHost that minted it interprets it.
type Target string

// ForegroundState is what runs in the pane's foreground.
type ForegroundState int
const (
    ForegroundUnknown ForegroundState = iota // probe failed — fail closed, take no action
    ForegroundShell                          // agent exited to a shell; respawn-eligible
    ForegroundAgent                          // agent process present
)

type PaneHost interface {
    // Resolve finds the agent's pane. explicit passes through when non-empty.
    // "" = no usable target (keeper proceeds inject-less).
    Resolve(projectDir, agentName, explicit string) Target

    // Inject delivers text + submit (paste, settle, Enter with bounded retry).
    // The herdr implementation refuses while agent_status is `working` (§5.5).
    Inject(ctx context.Context, t Target, text string) error
    SendEscape(ctx context.Context, t Target) error
    // SetSessionEnv is advisory (shell.go treats failure as non-fatal).
    // herdr: documented no-op — env is set at pane.split during respawn (§5.6).
    SetSessionEnv(ctx context.Context, t Target, key, value string) error

    // Capture returns pane text plus a bounded scrollback tail (ACK watch).
    Capture(ctx context.Context, t Target) (string, error)
    // Foreground reports what runs in the pane. Errors map to ForegroundUnknown.
    Foreground(ctx context.Context, t Target) ForegroundState

    // OperatorAttached reports recent HUMAN keystroke activity. False on error
    // (fail-open). herdr: always false — see §5.5 for the decided substitute gates.
    OperatorAttached(t Target) bool
}
```

`Foreground` replaces the `IsPaneIdle`/`IsPaneAlive` pair — today both run the same probe
and are documented mutually exclusive (`respawn.go`), which is one enum, not two booleans
(PRINCIPLES §2). `ForegroundUnknown` preserves both fail-closed behaviors: `maybeRespawn`
requires `ForegroundShell`; live-recover requires `ForegroundAgent`; an error triggers
neither. The existing `CycleDeps` ports and `WatcherConfig` `*Fn` test seams **stay**;
`PaneHost` is what their production defaults delegate to.

### 4.2 tmux mapping vs. herdr mapping

| PaneHost method | tmux (today's code, moved) | herdr (protocol 22, dial-per-call) |
|---|---|---|
| `Resolve` | `ResolveTmuxTarget`: naming convention + `tmux has-session` | agent-name targeting (§4.4); `pane.agent_status_changed` carries `agent` + `pane_id` for the cache |
| `Inject` | `load-buffer` → `paste-buffer -d` → settle → `send-keys Enter` ×(1+2) (`injectTextClocked`) | status pre-check (§5.5) → `pane.send_input{pane_id,text}` / `agent.send_keys{target}` → settle → Enter ×(1+2) |
| `SendEscape` | `send-keys Escape` (`SendEscapeKey`) | `pane.send_input{keys:Escape}` / `agent.send_keys` |
| `SetSessionEnv` | `setenv -t` (`SetTmuxEnv`) | no-op (decided, §5.6); env travels via `pane.split{env}` at respawn |
| `Capture` | `capture-pane -p -S -200` (`CaptureTmuxPane`) | `pane.read` (recent) |
| `Foreground` | `display-message '#{pane_current_command}'` vs `shellCmds` (`IsPaneIdle`/`IsPaneAlive`) | `pane.process_info` → `foreground_processes[].name/argv` (§5.4) |
| `OperatorAttached` | `list-clients -F '#{client_activity}'` + recency (`OperatorAttached`) | always false; protection moves to transcript gates + `agent_status` gating (§5.5) |

### 4.3 Where it lives — the depguard answer

**Everything lands under `internal/keeper/` — zero `.golangci.yml` change.** The `keeper`
depguard rule's `files: ["**/internal/keeper/**"]` glob covers subpackages, and the allow
list already carries the `internal/keeper` prefix plus `$gostd`. The herdr client needs only
`net.Dial("unix", …)` + newline-delimited JSON — all `$gostd` — so the import fence holds
without widening:

```
internal/keeper/
  panehost/            # PaneHost, Target, ForegroundState
  panehost/tmuxhost/   # today's exec-tmux code, moved verbatim
  panehost/herdrhost/  # the herdr adapter
  panehost/herdrwire/  # minimal NDJSON socket client ($gostd only: net, encoding/json, bufio)
```

`cmd/harmonik/keeper_cmd.go` (outside the fence) selects the implementation and passes it
in, the same way it already builds `CycleDeps` and `WatcherConfig`.

**Restructure fit:** per §3, no move happens in this work. Because `panehost` never imports
`internal/lifecycle/tmux` or the daemon, the eventual move to
`tools/keeper/internal/panehost/` is a `git mv` plus import rewrites. Do **not** put the
herdr client in `shared/` — the restructure's admission rule requires a second consumer in
committed code, and the keeper is the only one. If the daemon later wants herdr spawning,
that is a `lifecycle/herdr` sibling — a separate work.

**Wire types:** hand-write the ~9 request/response pairs the P0 set needs (small,
reviewable, no build tooling); record the schema `protocol` number (22) and check it at
dial, refusing a mismatch loudly. Adopt codegen from `herdr api schema --json` only when a
second harmonik component speaks herdr.

### 4.4 Resolve and target by STABLE AGENT NAME — not a recorded pane id

herdr pane ids (`w1:p1`) are per-server and change on every respawn — but herdr's own
addressing model makes them irrelevant to the keeper: `agent.start{name}` names the agent,
and `agent.send_keys{target}` / `agent.prompt{target}` / `agent.wait{target}` /
`agent.explain{target}` all accept that name as the target. `pane.agent_status_changed`
events carry both `agent` (name) and `pane_id`.

**Resolution:** the launcher names the agent at `agent.start` with the same collision-proof
convention the tmux path uses — `HarmonikSessionName` / `HarmonikCrewSessionName`
(`harmonik-<hash12>-[crew-]<agent>`), which keeps two projects on one herdr server from
colliding. `herdrhost.Resolve` returns that name as the `Target`. Calls that require a
`pane_id` (`pane.read`, `pane.process_info`, `pane.send_input`, `pane.split`, `pane.close`)
look it up by name per call, through a cache **held only as an optimization** and refreshed
from status events or re-lookup on any pane-gone error. Nothing persists a pane id across
restarts; there is no `.pane` file. This supersedes the recorded-pane-id scheme in
touchpoints.md rows 2 and 12 — the rebind concern (hk-9cqtm's herdr analogue) collapses
into "re-lookup by name", which is the normal path, not a rescue path.

### 4.5 The respawn command grows a herdr variant

`cmd/harmonik/captain_respawn.go` is where tmux respawn actually lives
(`buildCaptainRespawnWindowCmd`: `tmux respawn-window -k -e HARMONIK_AGENT=… claude
--resume <sid>`; `buildCaptainPanePIDCmd` + `refreshCaptainPID` for `captain.pid`). Plan:
the respawn command gains the substrate dimension and implements the confirmed herdr
primitives —

1. `pane.split{env: {HARMONIK_AGENT: <name>}, cwd: <project>}` — a fresh shell pane, split
   from the keeper's own surviving pane, env delivered here (§5.6);
2. `agent.start{kind: claude, name: <convention name>, pane_id: <new>, timeout_ms}` running
   `claude --resume <sid>` — supervised, with the startup timeout;
3. `agent.wait{target: <name>, until: [...], timeout_ms}` to confirm up;
4. `pane.close` the OLD pane — **new-before-old**, so a crash mid-sequence leaves either the
   old pane or both, never zero;
5. refresh `captain.pid` from `pane.process_info.shell_pid` (§5.4 — the PID question is
   answered; the orphan sweep keeps working).

The keeper itself keeps running `sh -c <RespawnCmd>` and never learns the difference — the
existing seam holds. `captainRespawnCmdString` and `agentlaunch.KeeperWindowOpts.RespawnCmd`
carry the variant.

## 5. herdr design constraints — resolved against the live schema

### 5.1 Restart is two-step (constraint 1) — sequence confirmed

`agent.start` needs an existing shell pane at an interactive prompt; there is no
`respawn-window -k` equivalent. The confirmed sequence is §4.5's new-before-old:
`pane.split{env,cwd}` → `agent.start{kind,name,pane_id,timeout_ms}` → `agent.wait` →
`pane.close` old. The agent pane's id changes on every respawn — harmless under name-based
targeting (§4.4). The logic lives in the respawn *command*, not the keeper.

### 5.2 No context-fill signal (constraint 2)

herdr's agent states (idle/working/blocked/done/unknown) do not include "context nearly
full". herdr replaces the restart **mechanism**, never the **reason**. The gauge stays
exactly as it is — statusLine hook → `.harmonik/keeper/<agent>.ctx` → `ReadCtxFile`.
Nothing in that path names tmux; this costs zero work, and the plan states it so nobody
"simplifies" the gauge away in favor of herdr state. (Do not confuse
`pane.report_metadata` — a ≤32-key metadata channel for the sidebar — with any of this;
sidebar push is a later pass, §2.)

### 5.3 Per-pane subscriptions (constraint 3)

`pane.agent_status_changed` and `pane.output_matched` require a `pane_id`; subscribe per
pane, after learning it from a global `pane.created` (or a name lookup). P0 does not
subscribe at all — it keeps its poll loops (dial-per-call is cheap on a unix socket), so
this constraint bites only the later event-lean pass (§6), where the stream is an
accelerator over polling, never the sole source.

### 5.4 Pane foreground process — RESOLVED: yes, richer than tmux

`pane.process_info` returns `PaneProcessInfo{pane_id, shell_pid, tty,
foreground_process_group_id, foreground_processes[]}` with each process
`{pid, name, argv, argv0, cmdline, cwd}`. The `Foreground` probe therefore ports cleanly
and is strictly better than tmux `#{pane_current_command}`: shell in the foreground group =
agent exited (`ForegroundShell`); `claude`/`codex` in the foreground = running
(`ForegroundAgent`); RPC error or empty = `ForegroundUnknown`. Combine with `agent_status`
as corroboration, never as the sole source (§5.7). **Self-heal respawn and live-pane
recovery are supported on herdr** — they are NOT disabled, reversing the earlier draft.
`shell_pid` also answers the `captain.pid` refresh (§4.5). The heartbeat gate
(`heartbeat.go`, which requires pane-not-idle) ports on the same probe.

### 5.5 Operator-attach — RESOLVED: no direct signal, protection retained by other gates

The schema has **no per-client keystroke-recency signal**: `AgentInfo` exposes `focused`
(bool) + `agent_status` but no last-input timestamp, and the only client-scoped params are
`ClientShellSurfaceSetParams{active}` (app foreground/background) and
`ClientWindowTitleSetParams{title}` — none is `#{client_activity}`.

**DECIDED resolution — drop the probe, keep the protection:**

- The keeper's operator-race protection is already substrate-independent: the
  transcript-turn gates (`GateSnapshot.LastUserTurnAt` / Gate 5d via
  `OperatorTurnLookback`, `LastAssistantTurnAt` / Gate 5e via `PostAnswerGrace`) read the
  transcript, not tmux. They carry the load on herdr unchanged.
- **Additionally, on herdr, gate injection on `agent_status`:** never inject while
  `working`; deliver resume keystrokes only when `agent_status` is `idle` or `blocked` AND
  `interactive_ready` is true. This lives in `herdrhost.Inject` as a pre-check, so every
  inject path (warn text, handoff, `/clear`, resume, ACK lines) inherits it.
- `herdrhost.OperatorAttached` returns false (fail-open, matching tmux's error path), and
  `keeper doctor` prints this substitution for herdr sessions.

**Accepted residual gap (first pass):** a human typing raw into an idle shell pane before
any transcript turn exists is undetectable on herdr. Accepted for P0; revisit in the
operator-attach later pass (§2).

### 5.6 Session env — RESOLVED: no live setenv; `pane.split{env}` covers the need

`PaneSplitParams` carries `env` (object), `cwd`, `focus`, `direction`, `ratio`,
`target_pane_id`. `AgentStartParams` has **no** env (kind/name/pane_id/args/timeout_ms
only) — the agent inherits the shell pane's env. There is no way to mutate a running pane's
env, and the keeper does not need one: the restart flow is exactly close → split(fresh
shell, env) → start, so env-at-split delivers `HARMONIK_AGENT` (§4.5).
`herdrhost.SetSessionEnv` is therefore a documented no-op — safe because `shell.go` already
treats `ActSetTmuxEnv` as advisory and non-fatal.

### 5.7 `unknown` is not `done`

herdr reports `unknown` when it sees an agent it cannot classify. Any mapping that treats
`unknown` as "exited" or "idle" would fire respawn on a healthy agent. `ForegroundUnknown`
satisfies neither the respawn gate nor the live-recover gate — matching today's
fail-closed error behavior — and destructive decisions key on `pane.process_info` (§5.4),
with `agent_status` as corroboration only.

### 5.8 herdr is 0.9.0, protocol 22

Pre-1.0 wire drift is likely. `herdrwire` pins the protocol number at dial and refuses a
mismatch with a clear error — drift becomes a loud boot failure, not silent misbehavior.

## 6. What herdr's agent-state events buy — later passes, and limits

Where the keeper can lean on pushed state (all post-P0, per §2):

- **AwaitAck** (`awaitack.go`): today a `capture-pane` poll loop hunting `AckMatchToken`.
  `pane.wait_for_output` with the token is a direct replacement — server-side match, no
  poll, no scrollback-window race. The cleanest single win. `agent.prompt{target, text,
  wait{until[], timeout_ms}}` (send + block until a state, one call) may similarly collapse
  inject-then-wait sequences.
- **Foreground/status caching**: subscribe to `pane.agent_status_changed`, cache, and only
  dial `pane.process_info` on staleness. §5.3 re-subscribe rules apply.
- **Sidebar visibility**: `pane.report_metadata` with tokens/band. Pure output, zero risk.

Hard limits: the context-fill trigger (§5.2) — never; and every destructive gate (respawn,
live-recover) must re-verify by direct probe at the moment of firing, the same
defense-in-depth `NewLiveRecoverViaRespawn` applies to `.sid` identity today.

## 7. Selection & config

Today: the launcher builds the argv (`agentlaunch.KeeperWindowArgv`: `harmonik keeper
--agent <n> --tmux <session>:agent …`), `runKeeperSubcommand`
(`cmd/harmonik/keeper_cmd.go`) parses flags, loads the `keeper:` block of
`.harmonik/config.yaml` (`ResolveKeeperConfig`), resolves the target, and constructs the
watcher + cycler.

Plan — **explicit selection, config-defaulted, no auto-detection**:

- **Flag:** `harmonik keeper --substrate tmux|herdr` (default `tmux`). Under herdr the
  target is the agent name (§4.4), so `--agent` suffices; `--tmux` with `--substrate herdr`
  is a hard error.
- **Config default:** `keeper.substrate:` and `keeper.herdr_socket:` (default
  `~/.config/herdr/herdr.sock`) in the `keeper:` config block, resolved through
  `ResolveKeeperConfig` like every other knob; flag wins. Config is per project per box —
  "per session/box, not globally" for free; the flag overrides for mixed fleets.
- **Why not detect** (socket present → herdr): both substrates can run on one box during
  evaluation, and the keeper must bind to the one hosting ITS agent. Detection guesses; the
  launcher knows. Mirrors the repo's deliberate-harness rule.
- **Launcher (P0):** `KeeperWindowOpts` grows a substrate field; `KeeperWindowArgv` emits
  it; the **captain CLI launcher** (`cmd/harmonik/captain.go`, §2) grows `--substrate`
  (D2 positional-XOR-flags rule applies); the respawn-cmd builder emits the matching
  variant (§4.5). `harmonik crew start` gains `--substrate` only in KH-8, because it routes
  through the daemon.
- **Doctor:** `keeper doctor` / `keeper enable` learn the substrate: probe the socket,
  check protocol 22, verify the agent/pane resolves, print the §5.5 operator-attach
  substitution note.

## 8. Testing

- **Unit layer — no change.** `internal/keepertest` (`Scenario`, `RecordingPorts`) and the
  `WatcherConfig` `*Fn` spies are substrate-free; they keep defending the reactor's claims.
- **PaneHost conformance suite** (matching the existing `conformance_keeper_test.go`
  naming): one table of claims — inject delivers AND submits; inject refuses during
  `working` (herdr); Escape clears partial input; Capture sees a just-injected token;
  Foreground reports shell vs. agent vs. Unknown correctly; Resolve finds the pane and
  returns "" cleanly; OperatorAttached fails open. Run three ways: real tmux (gated on
  `exec.LookPath("tmux")`, as `cycle_twin_e2e_integration_test.go` gates today), real herdr
  (gated on the socket), and the fake herdr server (always). Carry forward the encoded
  lessons (the `display-message`-exits-0 trap from `tmuxresolve_integration_test.go`, the
  operator-collision scenario).
- **The fake herdr server** — an in-process unix-socket NDJSON server, scriptable per test —
  is where fault injection lives (restructure §3.3: the socket is a real process boundary,
  so verification-first applies): dial refused; close mid-response; truncated/duplicated
  frames; slow past deadline; `error` replies per method; pane id invalidated between
  lookup and call (must re-resolve by name); status flapping to `unknown`. Claims defended:
  probes degrade to `ForegroundUnknown` (no false respawn), injects surface errors without
  wedging the cycle, and the keeper never busy-loops a dead socket.
- **Twin e2e:** a herdr twin of `cycle_twin_e2e_integration_test.go` — the P0 acceptance
  gate (parcel KH-7).
- Per PRINCIPLES §7: watch every new conformance/fault test fail on purpose before
  trusting it.

## 9. Dispatchable parcels

Each parcel is sized for one subagent and works in its own worktree off `integration`
(§3). Per §2.1, every parcel carries a **Self-check** (the implementer runs it and shows
the output before declaring done) and an **Oversight re-check** (the orchestrator re-runs
it independently on the landed commit); **a parcel's dependents do not dispatch until its
oversight re-check has passed.** KH-1 and KH-2 are first and parallel-safe.

---

**KH-1 — Extract `PaneHost` behind tmux; zero behavior change.**
*Scope:* create `internal/keeper/panehost` (interface, `Target`, `ForegroundState`) and
`panehost/tmuxhost` holding today's tmux code moved verbatim (`injectTextClocked`,
`SendEscapeKey`, `SetTmuxEnv`, `CaptureTmuxPane`, `IsPaneIdle`+`IsPaneAlive` →
`Foreground`, `OperatorAttached`, `ResolveTmuxTarget` + naming helpers). Thin back-compat
wrappers stay in `internal/keeper` so callers and tests compile unchanged; the production
defaults in `CycleDepsFromConfig`, `WatcherConfig.applyDefaults`, and `AwaitAck` route
through one `PaneHost` value.
*Files/symbols:* `internal/keeper/{tmuxresolve,injector,respawn,awaitack,
cycle_config_adapters,cycle_deps,watcher}.go`, new `internal/keeper/panehost/**`,
`cmd/harmonik/keeper_cmd.go` (wiring only). No `.golangci.yml` change (§4.3).
*Self-check:* `go test ./internal/keeper/... ./internal/keepertest/... ./cmd/harmonik/...`
green UNCHANGED (output shown), plus the new tmux-argv byte-identity recording-fake test —
shown failing under a deliberate argv mutation, then passing; then `/check` (`make fast`).
*Oversight re-check:* re-run the same suite + byte-identity test on the landed
`integration` commit; `make full` before KH-3/KH-4 dispatch.
*Depends on:* nothing.

**KH-2 — herdr wire client + fake herdr server + fault matrix.**
*Scope:* `panehost/herdrwire`: dial-per-call NDJSON client (`$gostd` only), protocol-22 pin
(refuse mismatch loudly), typed request/response pairs for the P0 method set —
`pane.list`, `pane.process_info`, `pane.read`, `pane.send_input`, `pane.split`,
`pane.close`, `agent.start`, `agent.send_keys`, `agent.wait`. Plus
`herdrwire/herdrfake`: the in-process scriptable fake server, and the §8 fault matrix.
*Files/symbols:* new `internal/keeper/panehost/herdrwire/**` only.
*Self-check:* `go test ./internal/keeper/panehost/herdrwire/...` (fault cases shown
fail-closed, each watched failing on purpose) AND a smoke run against the **live** herdr
socket on this box (`~/.config/herdr/herdr.sock`): split a throwaway pane, send text, read
it back, close it; assert the observed JSON matches the pinned protocol-22 shapes. Output
of both runs shown.
*Oversight re-check:* re-run the wire tests AND the live-socket smoke on the landed commit.
*Depends on:* nothing (package path is fixed by this plan; parallel-safe with KH-1).

**KH-3 — `herdrhost` adapter + PaneHost conformance suite.**
*Scope:* `panehost/herdrhost` implementing `PaneHost` per §4.2/§4.4/§5.5: name-based
`Resolve` (convention names via `HarmonikSessionName`), `Inject` with the agent-status
pre-check, `Capture`, `Foreground` from `pane.process_info`, no-op `SetSessionEnv`,
always-false `OperatorAttached`. The conformance suite (§8) run three ways (tmux-gated /
herdr-socket-gated / fake).
*Files/symbols:* new `internal/keeper/panehost/herdrhost/**`, new conformance test family
under `internal/keeper/panehost/`; folds shared setup out of the existing tmux integration
tests without deleting their claims.
*Self-check:* the conformance table shown green **all three ways** — real tmux, the fake,
and the live herdr socket — plus the adapter-level fault case (stale pane id between
lookup and call re-resolves by name).
*Oversight re-check:* re-run the conformance suite against live herdr on the landed commit
before KH-4/KH-5 dispatch.
*Depends on:* KH-1, KH-2.

**KH-4 — Keeper substrate selection.**
*Scope:* `--substrate` flag + `keeper.substrate` / `keeper.herdr_socket` config keys;
`runKeeperSubcommand` constructs the selected `PaneHost`; `keeper doctor` /
`keeper enable` substrate-aware (§7). New config fields land in `internal/projectconfig`
`KeeperConfig` (no such fields exist yet) and resolve through `ResolveKeeperConfig`.
*Files/symbols:* `cmd/harmonik/keeper_cmd.go`, `cmd/harmonik/resolve_keeper_config.go`,
`cmd/harmonik/keeper_config_example.go`, `cmd/harmonik/keeper_enable_doctor_cmd.go`,
`internal/projectconfig`.
*Self-check:* precedence unit tests (flag > config > default; the `--tmux`+`--substrate
herdr` hard error) shown green; `harmonik keeper doctor` run under `--substrate herdr`
against the live socket with its output shown (socket probe, protocol check, §5.5 caveat
printed); KH-1's tmux byte-identity assertion re-run to prove the tmux default path
regressed nothing.
*Oversight re-check:* re-run `keeper doctor` against live herdr + the byte-identity test on
the landed commit.
*Depends on:* KH-3.

**KH-5 — herdr respawn command + target-gate relaxation.**
*Scope:* the respawn surface in `cmd/harmonik` grows the herdr variant implementing §4.5:
split{env,cwd} → `agent.start` (`claude --resume <sid>`, timeout) → `agent.wait` → close
old; pid refresh from `pane.process_info.shell_pid`; `captainRespawnCmdString` emits the
variant. **Confirmed wiring fix:** today `--respawn-cmd` requires `--tmux`, and
`keeperHardCeilingRestartFn` plus the live-recover wiring gate on a non-empty tmux target
— but `--tmux` is a hard error under `--substrate herdr` (§7). This parcel populates the
herdr target (the agent NAME, §4.4) into that same target variable and relaxes the flag
validation and gates, so respawn, hard-ceiling restart, and live-pane recovery all fire
under `--substrate herdr` without `--tmux`.
*Files/symbols:* `cmd/harmonik/captain_respawn.go`, `cmd/harmonik/keeper_cmd.go`
(`keeperHardCeilingRestartFn`, `keeperLiveRecoverFn`, flag validation), consuming
`herdrwire`.
*Self-check:* fake-server tests assert the exact call sequence and new-before-old ordering
(including crash-between-steps cases), shown green; then a **live** run: start an agent on
herdr, kill the claude process, run the respawn command, show the agent back under
`--resume` with the same sid — and show the keeper's respawn gate armed without `--tmux`
(watcher log line, no `no_tmux_target` abort).
*Oversight re-check:* repeat the live kill-and-respawn on the landed commit.
*Depends on:* KH-2, KH-3 (KH-4 for the flag surface it relaxes).

**KH-6 — Captain launch on herdr (CLI-direct; zero daemon change).**
*Scope:* the captain CLI launcher (`cmd/harmonik/captain.go` `runCaptainLaunchWithOps`,
`captainTmuxOps`/`osCaptainTmuxOps` seam) gains `--substrate herdr` (D2 flag rules):
create the agent pane via `herdrwire` (split + `agent.start` with the convention name,
§4.4), spawn the sibling keeper pane through a herdr `WindowSpawner` for
`agentlaunch.SpawnKeeperWindow` (the seam is already an interface, but its params are
`ltmux` types — adapt to them or give `agentlaunch` its own param struct; smallest diff
wins, decide at implementation); `KeeperWindowOpts`/`KeeperWindowArgv` emit
`--substrate herdr`; the respawn-cmd string wired to the KH-5 variant; minimal
`pane.report_metadata` spawn tag. **No daemon code.**
*Files/symbols:* `cmd/harmonik/captain.go`,
`internal/agentlaunch/{keeperargv,keeperwindow}.go`.
*Self-check:* actually launch `harmonik start captain --substrate herdr` on this box and
show: both panes exist (`pane.list` output), the agent visible by its convention name, the
keeper lock held (`keeper.AcquireLock`), and the gauge file appearing within a poll
interval.
*Oversight re-check:* the orchestrator repeats the live launch on the landed commit.
*Depends on:* KH-4, KH-5.

**KH-7 — P0 acceptance: herdr twin e2e (captain).**
*Scope:* the herdr twin of `cycle_twin_e2e_integration_test.go`: captain on herdr → drive
the gauge over the act band → observe handoff → `/clear` → resume; then the dead-pane
path: kill the agent process → keeper respawns via the KH-5 command → agent back and
working (agent status + fresh gauge). Scripted-fake variant always on; real-herdr variant
socket-gated.
*Files/symbols:* new `internal/keeper/*herdr*_integration_test.go` (or under `panehost/`),
reusing keepertest scaffolding.
*Self-check:* the twin shown passing both ways (fake + live herdr), plus one live manual
end-to-end run narrated (launch → fill → cycle → kill → respawn → working); `make full`
green.
*Oversight re-check:* re-run the twin against live herdr + `make full` on the landed
commit. This parcel closing IS the P0 definition of done (§2).
*Depends on:* KH-6.

**KH-8 — Crew-on-herdr via the daemon (post-P0; the committed end-state; LAST).**
*Scope:* plumb `--substrate` through the `crew-start` RPC payload and
`crewrun.CrewStartRequest`; add a herdr crew-session spawner in
`internal/daemon/crewstart.go` + `tmuxsubstrate.go`. Note the boundary: the daemon has its
own `handler.Substrate` seam (`internal/handler/substrate.go`), distinct from the keeper's
`PaneHost` — the herdr crew spawner is a **herdr sibling of `tmuxSubstrate` behind
`handler.Substrate`**, not part of `PaneHost`. This parcel touches shared daemon code and
MUST be coordinated with the active kernel agent on `work/restructure-scaffold` before
dispatch (§3).
*Files/symbols:* `internal/daemon/crewstart.go`, `internal/daemon/tmuxsubstrate.go` (new
sibling), `internal/crewrun` (`CrewStartRequest`), `cmd/harmonik/crew.go`.
*Self-check:* start a crew through the daemon with `--substrate herdr` and show: agent +
keeper panes live, crew visible by name, gauge file appears, and one observed
keeper-driven crew restart.
*Oversight re-check:* repeat the live crew start + restart on the landed commit;
`make full`.
*Depends on:* KH-7 (P0 proven first).

---

**Explicitly post-P0** (named, unscheduled beyond KH-8): operator-attach nuance beyond
§5.5; event-lean (`pane.wait_for_output` for AwaitAck, status caching, `agent.prompt
--wait`); sidebar metadata push; remote/multi-box; the `tools/keeper` move (owned by the
restructure program, §3).

## 10. Open questions & risks

**Remaining open questions (small, resolvable at implementation):**

1. **`interactive_ready` semantics** (§5.5): confirm during KH-3 exactly when herdr sets it
   (agent at a prompt vs. pane merely alive) before the inject pre-check keys on it.
2. **Crew respawn path** (KH-8): whether crew respawn shares `captain_respawn.go` or needs
   a sibling — a repo detail to pin down when the daemon parcel is scoped, not a design
   unknown. P0's KH-5 covers the captain path only.
3. **Agent-name length/charset limits** in herdr for the `harmonik-<hash12>-crew-<name>`
   convention (§4.4) — verify against the schema's name constraints during KH-2.

**Top risks:**

- **Quiet contract loss in extraction (KH-1).** The tmux path carries years of encoded
  fixes (bracketed-paste submit race hk-89g, keystroke-recency attach hk-0t5s, rebind
  hk-9cqtm, fail-closed probes). The byte-identity assertion and the conformance suite
  exist to carry those claims across; watch each test fail on purpose (PRINCIPLES §7).
- **Pre-1.0 protocol drift (§5.8).** The dial-time pin turns drift into a loud boot error;
  still expect churn during evaluation.
- **Residual operator-race gap on herdr (§5.5).** Accepted for P0 and printed by
  `keeper doctor`; a human typing raw into an idle pane pre-transcript is unprotected.
- **Restructure collision.** Mitigated by §3: in-place build, isolated worktrees, no
  `tools/keeper` move here; P0 touches zero daemon code. The one daemon parcel, KH-8, is
  sequenced last and requires explicit coordination with the kernel agent on
  `work/restructure-scaffold` before dispatch.
