# 03 — Research: the substrate / tmux-hosted process regime

**Work:** process-group-provenance · Pass 3 · crew kilo · 2026-07-22
**Assignment:** OD-5 / OD-2 / D5 / D6 — is the darwin-provenance decision (OD-1) the main event or a side quest?
**Method:** source read at HEAD `ae470edc` (repo READ-ONLY), full-history event-log analysis (`.harmonik/events/events.jsonl`, 366 orphan sweeps, 2026-05-14 → 2026-07-22), and a live read-only process/tmux census. Nothing killed, written, or dispatched.

---

## 0. Bottom line first

**OD-1 (the darwin provenance marker) is a side quest as currently framed — but not for the reason the brief expected, and it becomes the main event again under one specific reframing.**

1. **Substrate/tmux is the production path for ~94% of runs** (3,426 of 3,629 all-time `harness_selected` events resolve to `claude-code`, which is *forced* onto the substrate path). Every one skips `handler.go:309`.
2. **The reaper OD-1 is about has killed nothing, ever.** Across all 366 orphan sweeps in the event log: `subprocesses_killed = 0`, `tmux_windows_killed = 0`. The only sweep that has ever fired is the tmux **session** sweep — 44 kills across 35 sweeps.
3. **The premise under OQ-PL-008 is false.** darwin *can* read another process's environment: `ps -AEwww` exposes the full exec-time environment of every same-UID process, and it works today on this box (44 harmonik-marked processes visible, 13 of them `PPID==1`). `ReadProcessEnviron` is `/proc`-only by *implementation choice* (`provenance.go:187-205`), not by platform limitation.

That third point resolves OD-1 and makes B1/B2/B3 three answers to a question with a cheaper fourth answer (**B4**, §8). But it does **not** rescue the work: even with a perfect portable marker, **no marker, group, or session mechanism reaches the processes that are actually leaking**, because Claude Code `setsid()`s every subprocess it spawns. Measured, not inferred (§4).

**OD-1 shrinks from "the gate" to "a 30-line portability fix with a known answer." OD-5 and OD-2 are the main event.**

---

## 1. Which path do real runs take? (verified — headline)

### 1.1 The branch
`/Users/gb/github/harmonik/internal/handler/handler.go:272-273`:
```go
	if spec.Substrate != nil {
		return h.launchViaSubstrate(ctx, sessionID, spec)
	}
```
`handler.go:309` — `cmd.SysProcAttr = lifecycle.SpawnChildSysProcAttr(lifecycle.RecordedPGID())` — is 36 lines below that return. **Confirmed: substrate-hosted runs never receive any `SysProcAttr`.**

### 1.2 What decides the branch — harness identity via `SessionIDPolicy`

| Harness | `SessionIDPolicy` | Path | Why |
|---|---|---|---|
| `claude-code` (`claudeharness.go:136`) | `SessionIDMinted` | **substrate (tmux)** | `workloop.go:4571` `spec.Substrate = runSubstrate` |
| `codex` (`codexharness.go:178`) | `SessionIDCaptured` | direct-exec | `workloop.go:4533-4534` forces `spec.Substrate = nil` |
| `pi` (`piharness.go:241`) | `SessionIDCaptured` | direct-exec | same |

Reason (`workloop.go:4455-4459`): a tmux-hosted session returns `Stdout() == nil` (`handler.go:453-455`), so session-id capture and the `agent_end` watcher never fire. Review loop mirrors this at `reviewloop.go:353-371`.

### 1.3 Which harness is normal in production

Four-tier resolution (`harnessresolve.go:41-118`): bead label → queue default → DOT node attr → `Config.DefaultHarness` → built-in `claude-code`.

- `--default-harness` flag default is `""` (`cmd/harmonik/main.go:1022-1023`); `.harmonik/config.yaml` sets **no** `default_harness`. ⇒ tier 4 always resolves to `claude-code`.
- `daemon.workflow_mode: dot` ⇒ every bead runs `internal/daemon/standard-bead.dot`, whose `implement` node has **no** `harness=` (⇒ tier 4 claude-code) and whose `review` node **pins** `harness="claude-code"` at tier 3.
- 1 of ~115 `.harmonik/queues/*.json` carries `default_harness` at all — and it is a `.failed-*` pi queue.

**Measured over the whole event log (3,629 `harness_selected`):** `claude-code` 3,426 (**94.4%**), `pi` 102 (2.8%), `codex` 101 (2.8%). Recent-200k tier split: claude-code tier-4 2,535 / tier-3 245 / tier-1 13; codex only ever tier-3 (49) or tier-1 (32); pi likewise.

Substrate is tmux unless `HARMONIK_SUBSTRATE=codexdriver` (`cmd/harmonik/substrate_select.go:31,75-76`); the live daemon (pid 65959) does not carry it. `ReviewerSubstrate` is the tmux substrate on *every* path (`substrate_select.go:80`).

> **Unambiguous: substrate/tmux-hosted is the normal production path today, ~17:1. And because `review` is pinned to claude-code at tier 3, even a full codex-implementer flip leaves every reviewer run on the substrate path.** A fix confined to `handler.go:309` addresses the minority branch of the current fleet and ~half the node dispatches of the intended future fleet.

---

## 2. The `PPID == 1` blindness — verified, then narrowed

**The filter is real** — `internal/lifecycle/orphansweep.go:244-246`:
```go
		ppid, err := strconv.Atoi(ppidStr)
		if err != nil || ppid != 1 { continue }
```

**A tmux-hosted process is parented to the tmux server — measured.** The server is **pid 32783** (`PPID=1 PGID=32783 SID=32783`, started 2026-06-21; argv is the stale `tmux new-session … harmonik-a3dc45482890-captain …` that first forked it). Every pane process is its direct child:

```
pid= 32783 ppid=    1 pgid=32783 sid=32783   tmux server
pid= 32159 ppid=32783 pgid=32159 sid=32159   claude … --remote-control hk-captain
pid= 62837 ppid=32783 pgid=62837 sid=62837   claude … --remote-control hk-kilo
pid= 60295 ppid=32783 pgid=60295 sid=60295   harmonik supervise _shim … --watch-restart
pid= 50352 ppid=32783 pgid=50352 sid=50352   harmonik supervise _shim /private/tmp/hk-rel650f-lt
```

Confirmed: D5's contradiction between `PL-INV-005:1133` ("MUST have the daemon as its initial parent") and `PL-021b:732` ("MUST create the subprocess via `tmux new-window`") is real, and the code obeys PL-021b.

**But D6 is broader than the facts.** D6 says the sweep is "structurally blind to substrate-hosted orphans." True of the **pane process**, false of its **descendants**:
- Pane process: tmux-parented ⇒ invisible to the handler sweep — but it is exactly what the tmux **session** sweep covers by name prefix. Not uncovered; owned by a different reaper.
- Descendants that outlive their parent *are* reparented to init and *do* have `PPID==1`. 13 such processes exist on this box right now carrying a harmonik env marker (§6.3). That is the real leak, and the `PPID==1` filter does not exclude it.

**Refinement to D6: the `PPID==1` filter is not the load-bearing defect. The `/proc`-only env read is.**

**Does any current sweep reach a tmux-hosted orphan *process*?** Pane process with a `harmonik-<hash12>-` session name: yes (`orphansweep.go:116-152`, `tmux/orphansession.go:52-98`, via `bootreconcile.go:207` → `RunOrphanSweep`). Pane in an operator-owned session: in theory `SweepOrphanTmuxWindows` (`tmux/orphanwindow.go:43-90`) — never fires, §3.3(a). Orphaned grandchild: **no reaper on darwin**; on Linux only if it carries `HARMONIK_PROJECT_HASH`, which the crew/captain population does not.

---

## 3. What actually covers the substrate path today

**3.1 The portable provenance that works: name prefixes.** Session name `harmonik-<project_hash12>-…` (`provenance.go:106`) consumed at `orphansweep.go:135`, `tmux/orphansession.go:52`, `daemon/bootreconcile.go:207`. Window name `hk-<hash6>-…` (`tmux/orphanwindow.go:128-135`, PL-021c). Per-run independent sessions are `harmonik-<hash12>-run-<12hex>` (`tmuxsubstrate.go:1750-1760`), inside the swept namespace; crew/captain sessions likewise.

**3.2 Exemptions.** `RunOrphanSweep` (`daemon/orphansweep.go:731-860`) excludes the daemon's spawn-target session (hk-9vp51), live coordinator (PL-006d i), live captain (ii), and every live crew in the registry (iii). Measured over 366 sweeps: 820 crew skips, 129 captain skips, 96 coordinator skips, **44 sessions actually killed**.

**3.3 Three concrete gaps.**

**(a) The window sweep can never match a production run window.** `handler.launchViaSubstrate` sets `WindowName: spec.WorkDir` (`handler.go:428`) — an absolute worktree path — and `tmuxSubstrate.spawnWindowVia` passes it through unchanged (`tmuxsubstrate.go:895`; the `hk-`-prefixing fallback at `:896-906` only fires on an empty name). Independent run sessions hardcode `WindowName: tmux.WindowAgent` with the comment *"fixed name; avoids the hk-<hash6>- orphan-window sweep"* (`tmuxsubstrate.go:1787`). **No production window name begins with `hk-<hash6>-`**, which fully explains `tmux_windows_killed = 0` over 366 sweeps. PL-021c is dead code in this deployment.

**(b) PL-021c explicitly refuses to escalate.** `specs/process-lifecycle.md:761`: *"The daemon MUST NOT send SIGKILL to survivors at MVH"*; survivors are *"adopted by the OS init process and not tracked further."* Its survivor probe only checks `#{pane_pid}` — so it cannot even observe a surviving grandchild.

**(c) Killing a tmux session does not reap grandchildren.** §4.2.

**3.4 Both kill paths signal exactly one pid.**
- Substrate: `tmuxSubstrateSession.Kill` (`tmuxsubstrate.go:2555-2596`) → `killProcessWithGrace(s.pid, 3s)` = `syscall.Kill(pid, SIGTERM)` → poll → `SIGKILL`, **positive pid only**, then `KillWindow`. Its own comment (`:2545-2552`): *"killing the tmux window shell alone (which previously sent SIGHUP to the child) is not relied upon."*
- Direct-exec: `handler/session.go:421-450`, positive pid only; the comment at `:405-419` explains `-childPid` is meaningless (child joined the *daemon's* group) and `-daemonPgid` would kill the daemon.

**Neither path issues a group- or tree-directed kill.** hk-n93gq is real on both branches.

---

## 4. The grandchild question (OD-2), settled empirically

### 4.1 The measurement that decides it
Live descendants of two tmux-hosted Claude agents (pane leaders 63154 = crew *lima*, 62837 = crew *kilo*), via `ps` + `os.getsid()`:

```
=== pane leader 63154  pgid=63154 sid=63154        (tmux setsid'd the pane)
  pid=44051 pgid=44051 sid=44051  /bin/zsh   ← Claude Bash-tool child: OWN group AND OWN session
     └ pid=44059 pgid=44051 sid=44051  harmonik comms recv --follow
  pid=45616 pgid=45616 sid=45616  /bin/zsh
     └ node(srt) / tee / zsh / head    all pgid=45616 sid=45616
  pid=49368 pgid=63154 sid=63154  caffeinate  ← the ONLY descendant still in the pane's group
=== pane leader 62837  pgid=62837 sid=62837
  8 × /bin/zsh tool children, every one pgid==pid, sid==pid
```

**Claude Code calls `setsid()` for every Bash-tool subprocess.** A grandchild is in neither the pane's process group nor the pane's terminal session. Its own children *do* inherit its group (44059 in 44051) — so the tool-child is a valid kill handle for great-grandchildren, but nothing ever signals it. Same escape on the daemon side: supervisor shim 60295 is the pane leader (`sid=60295`), and the daemon 65959 it spawns has `sid=65959` (`supervise/daemon_watchdog.go:397` `SysProcAttr{Setsid:true}`).

### 4.2 What `tmux kill-session` / `kill-window` does
tmux 3.6a on this box (`/opt/homebrew/bin/tmux`) imports **both `_kill` and `_killpg`** (`nm -u`). It destroys a pane by signalling the **pane leader's process group** and closing the pty master; closing the master hangs up the tty and the kernel delivers `SIGHUP` to the **foreground process group of that terminal's session**. Both targets are the pane leader's group / the pane's session. Per §4.1 the grandchildren are in **neither**.

I did not run a kill (hard rule), so the exact signal is high-confidence-but-not-source-verified here — **and the conclusion does not depend on it**: every mechanism tmux has (killpg on the pane group, kill on the pane pid, pty hangup on the pane session) targets a group and a session the measured grandchildren have left.

Live corroboration: pid **8610** (`harmonik comms recv --agent mike --follow`) is a Bash-tool grandchild of the *mike* pane, now `PPID=1` with a dead group leader (`pgid=8605`, no such process), alive since 00:29 — while 44 tmux sessions have been killed by sweeps over this log's history.

### 4.3 The matrix
`✅` reachable today · `⚠️` reachable with a change this work could make · `❌` not reachable by any group/session/marker mechanism.

| Branch | Generation | Platform | What identifies it | What can kill it |
|---|---|---|---|---|
| **direct-exec** (codex, pi — ~6%) | child | Linux | `HARMONIK_PROJECT_HASH` (`workloop.go:1093`) + `PPID==1` + PGID == daemon PGID | ✅ handler sweep; ❌ group kill (group == daemon's) |
| | child | darwin | same env; `/proc` absent ⇒ unread today; **⚠️ readable via `ps -AEwww`** | ⚠️ handler sweep once env is read portably |
| | grandchild | Linux | ✅ env inherited transitively; `PPID==1` when orphaned | ✅ handler sweep (works today) |
| | grandchild | darwin | ⚠️ env inherited, readable via `ps -E` | ⚠️ same fix |
| **substrate** (claude — ~94%) | pane process | both | ✅ tmux **session-name** prefix; ❌ window name (never matches, §3.3a); ❌ `PPID==1` (parent = tmux server) | ✅ `kill-session`; ✅ daemon's `killProcessWithGrace(pane_pid)` |
| | grandchild, still parented | both | ❌ not in pane group (setsid'd); ❌ not in pane session; env marker present but not `PPID==1` so the sweep's own filter drops it | ❌ **nothing** — `kill-session` misses it; PL-021c refuses SIGKILL (`:761`) |
| | grandchild, orphaned to init | Linux | ✅ env inherited (`-e KEY=VAL`, `osadapter.go:613`) + `PPID==1` | ✅ handler sweep — *if* the marker is on the agent |
| | grandchild, orphaned to init | darwin | ⚠️ env readable via `ps -E` (measured) | ⚠️ handler sweep once env is read portably |
| | **launch-layer** agent's grandchild (crew/captain) | both | ❌ carries `HARMONIK_AGENT` + `HARMONIK_PROJECT` but **no `HARMONIK_PROJECT_HASH`** (measured) | ❌ nothing except the argv-matching watcher reaper (§7.1) |

**The decisive cell:** *substrate grandchild, still parented* is `❌` everywhere, both platforms, under every OD-1 option. The problem is not identification — it is that **there is no kill verb that spans a `setsid()` boundary** other than walking the process tree from a known root while the root is alive.

---

## 5. OD-5 — recommendation

**Recommend option (b): two explicitly-named regimes with separate reapers — plus a third clause that says out loud what neither regime covers.**

Rejecting the others first:
- **(a) one marker rule with a substrate carve-out** — this is effectively what the spec says today, and it is what produced C4. A carve-out phrased as an exception to PL-006a leaves PL-006's enumeration clause (`:343`) claiming completeness it lacks (D6) and leaves PL-021b §7 silent on grandchildren (A4).
- **(c) substrate exempt from PL-006, covered solely by PL-021c** — factually unsafe. PL-021c has **never fired** (0 windows / 366 sweeps) because no production window name matches its prefix, and it is normatively forbidden from escalating (`:761`). Declaring it sole owner would write a *false* coverage claim into the spec — same class of defect as C1.

**Proposed shape:**
1. **Regime A — daemon-forked (PL-006).** Sites: `handler.go:309`, `brcli/adapter.go:146`, the three `Setpgid` tool spawns. Marker: env var, read **portably** (B4). Reaper: handler sweep. Kill handle: the child's own PGID (hk-n93gq's `Pgid: 0`), safe here because it is a *handle*, not the provenance value.
2. **Regime B — substrate-hosted (PL-021b/c).** Site: `handler.go:273` → `tmux new-window -e …`. Marker: **tmux session-name prefix** (already portable, already the only reaper with a nonzero production record). Kill handle: the **pane leader's PGID** — needing **no spawn-site change**, because tmux already makes every pane leader its own group and session leader (measured §4.1). `kill(-pane_pid, …)` is a correct, free kill handle on this branch today; add it to `tmuxSubstrateSession.Kill` alongside the existing positive-pid kill.
3. **Regime B must also fix two live defects it owns**, or it is fiction: run-window names must actually carry the `hk-<hash6>-` sentinel (or PL-021c must be retired as unimplementable-as-written); and PL-021c §6's survivor probe must look past `#{pane_pid}` or stop claiming to detect survivors.
4. **An explicit non-coverage clause.** State normatively that a descendant which has called `setsid()` (which Claude Code does for every tool subprocess — measured) is outside both regimes' kill handles, and name the only two mechanisms that could reach it: (i) a pre-death process-tree walk rooted at the recorded pane/child pid, or (ii) an orphan sweep keyed on the inherited env marker *after* reparenting to init. **Silence here is exactly what produced C4, and this cell is the actual orphan leak.**

**Consequence for OD-2:** grandchild coverage comes from **env inheritance only**, and env inheritance is available on **both** platforms once `ps -E` is used. It is *not* provided by group membership, session membership, or a per-pid registry — so **B1 (per-pid registry) is the one OD-1 option that cannot cover grandchildren at all.** That is the decisive argument against it.

---

## 6. Live-box evidence (read-only census, 2026-07-22 ~10:30Z)

**6.1 tmux topology.** 13 sessions, one server (32783). `harmonik-a3dc45482890-{captain, crew-admiral, crew-assessor, crew-india, crew-juliet, crew-kilo, crew-lima, crew-mike, default, flywheel}` for this project (hash `a3dc45482890`); `harmonik-9e7137308093-{default,flywheel}` for a **peer project** under `/private/tmp/hk-rel650f-lt`; plus `ctx-watchdog`, a harmonik-created session whose name is **outside** the swept namespace. No `hk-…` run windows — no run in flight.

**6.2 Group/session structure.** Every pane leader has `pgid == sid == pid`. Daemon 65959 (`sid=65959`) is a **grandchild** of tmux via shim 60295 and has escaped the pane's session. `br` (pid 72600) sits in the daemon's group 65959 **by plain inheritance**, since `brcli/adapter.go:146` sets no `SysProcAttr`. (Re C1: the inherited work's *mechanism* claim is false, but its *outcome* claim — br in the daemon group — is true by default inheritance, and a `setsid`'d daemon does give `br` a project-scoped group for free. That strengthens OD-3(b).)

**6.3 Env-marker census — the finding that resolves OQ-PL-008.**
`ps -AEwww` on darwin returns the full exec-time environment of every **same-UID** process (and prints nothing for other users' — a natural fail-closed boundary). Results:
- **44** processes carry `HARMONIK_PROJECT=/Users/gb/github/harmonik`.
- **0** carry `HARMONIK_PROJECT_HASH` — no run in flight; the marker only ever exists on daemon-spawned run children, never on the crew/captain/keeper population.
- **13** are `PPID==1` *and* env-marked — exactly what a portable env-marker sweep would enumerate:

```
  8610  harmonik comms recv --agent mike --follow --json   ← orphaned Bash-tool grandchild
 27464  harmonik keeper --agent admiral
 40843  harmonik keeper --agent captain
 42543  harmonik keeper --agent assessor
 64675  harmonik comms recv --follow --json --agent juliet --project …
 75166  harmonik comms recv --agent kilo --follow --json
 91090  harmonik keeper --agent mike
 91164  harmonik keeper --agent kilo
 91194  harmonik keeper --agent juliet
 91231  harmonik keeper --agent india
 91307  harmonik keeper --agent lima
 83559  keyboxd --homedir /Users/gb/.gnupg --daemon        ← FALSE POSITIVE (gpg)
 88350  /tmp/hk155gs/harmonik daemon                        ← FALSE POSITIVE (peer project)
```

**Two of thirteen are false positives, and they are the two that matter:**
- **`keyboxd`** — the operator's GnuPG daemon, launched from a shell inside a harmonik agent, inherited the marker, `PPID==1`. An env-marker sweep would SIGTERM→SIGKILL the user's gpg agent.
- **`/tmp/hk155gs/harmonik daemon`** — a **different project's daemon**, launched by an agent doing e2e work, carrying `HARMONIK_PROJECT=/Users/gb/github/harmonik`. That is the problem space's design test (c) "a peer project on the same box" — and **env inheritance fails it, measured.**

Env inheritance is transitive and unbounded: it marks descendants forever, across project boundaries and unrelated daemons. It is the mirror image of the argv-matching refusal — argv matching kills *live harmonik infrastructure*; env matching kills *unrelated infrastructure*. **Any env-marker sweep MUST carry a second conjunct** (binary identity restricted to a declared set; a per-daemon-generation nonce rather than a stable project hash; or a start-time window bounded by the recording daemon's lifetime).

Also: keepers 91090/91164/91194/91231/91307 all share `pgid == sid == 91086` **whose leader is dead** — the live PGID collision from the problem space is still present right now, and it is a *session*, not just a group. Direct evidence against any recorded-PGID matcher (B2) lacking a start-time conjunct.

**6.4 Sweep effectiveness — all-time** (366 `daemon_orphan_sweep_completed`, 2026-05-14 → 2026-07-22):

| counter | total | sweeps nonzero |
|---|---:|---:|
| `tmux_sessions_killed` | **44** | 35 |
| `coordinator_sessions_reaped` | 2 | 2 |
| `bead_in_progress_reset` | 173 | 70 |
| `intents_gc_d` | 94 | 23 |
| `subprocesses_killed` (handler sweep) | **0** | **0** |
| `tmux_windows_killed` (PL-021c) | **0** | **0** |
| `br_subprocesses_killed` | **0** | 0 (counter hardcoded 0, `orphansweep.go:903-920`) |
| `locks_cleared`, `reconciliation_locks_removed` | 0 | 0 |

The handler-process sweep — the sole consumer of the marker OD-1 is choosing — has a **zero-kill production record over its entire history, on the platform it runs on.**

---

## 7. What I refuted

**7.1 "`ReapPriorAgentFollowWatchers` has zero callers" (hk-5z4ww; G1/OD-4) — FALSE at HEAD.** Two production callers: `cmd/harmonik/captain.go:486` and `cmd/harmonik/crew.go:317`, wired via `cmd/harmonik/watcherreap.go:34`. Landed in commit `d8b7b434` *"fix(launch): reap prior same-agent --follow watchers on boot (hk-6629b)"*.
This changes OD-4 materially: no longer "specify/delete/defer a dormant helper," but **a live, unscoped argv matcher running on every captain and crew launch** — the same category as hk-c6dt2 and exactly what `PL-007:475` forbids. Matcher (`agentwatcherreap.go:88-112`) requires only: argv contains `harmonik`, `--follow`, and (`comms`+`recv` or `subscribe`), with `--agent`/`--to` equal to the name. **No project scope, no liveness gate (by design), self-exclusion by pid only.** On this box `harmonik start crew mike` would SIGKILL pid 8610 *and* any same-named crew watcher of the peer projects under `/private/tmp/hk-rel650f-lt` or `/tmp/hk155gs`. Re-triage as a live hazard.

**7.2 "darwin has no readable provenance marker" (OQ-PL-008, D3, the framing of OD-1) — FALSE.** `ps -AEwww` reads any same-UID process's environment on darwin (§6.3), and the repo already uses `ps` for this class of enumeration (`agentwatcherreap.go:60`, `orphansweep.go:226`).

**7.3 D6's "structurally blind on both platforms" — narrowed, not refuted** (§2).

**7.4 "the tmux path has working provenance" (C2) — true but weaker than stated.** *Session*-name provenance works and fires (44 kills). **Window-name provenance (PL-021c) has never fired and cannot fire as deployed.**

---

## 8. A fourth option for OD-1: **B4 — portable env marker**

> **B4.** Give `ReadProcessEnviron` a darwin implementation backed by `ps -AEwww -o pid=,command=`, parsed for the `KEY=VALUE` token; keep `/proc` on Linux. One function, one platform file, no new on-disk schema, no PGID-recycling exposure — and it covers grandchildren via inheritance on both platforms, the one thing B1 structurally cannot do (OD-2).

| Design test | B4 |
|---|---|
| (a) pid/pgid recycling | Immune — no pid/pgid used as identity |
| (b) live sibling of same identity | Not distinguished — needs the generation-nonce conjunct |
| (c) peer project on the same box | **FAILS as-is** — measured, pid 88350 |
| (d) darwin | Works — measured |
| fail-closed | Yes: unreadable env (other UID, truncation) ⇒ no match ⇒ no reap |

So B4 answers "can darwin read a marker" (yes) but not "is inheritance proof of ownership" (no). That second question is the real content of OD-1 and applies equally to Linux today. **Framed correctly, OD-1 is not a darwin question at all; it is a "transitive inheritance is not ownership" question that darwin merely hid.**

**D7 is armed by B4 exactly as pass 2 predicted:** the moment env becomes readable on darwin, `orphansweep.go:270-274` starts reaching `ReadProcessCmdlineArgs` (also `/proc`-only, `provenance.go:151-153`), the `IsRelayGrandchild` guard fails open, and live `harmonik hook-relay` processes become kill targets. **B4 must land with a portable argv read (`ps -o args=`) or an explicit "exclusion input unreadable ⇒ skip the candidate" rule.**

---

## 9. Open risks

1. **The leak may be unfixable by provenance.** The measured leaked population descends from **crew/captain agents** launched by `harmonik start crew|captain`, not by the daemon — carrying `HARMONIK_AGENT` and `HARMONIK_PROJECT` but **no** `HARMONIK_PROJECT_HASH`. Fixing the sweep's marker read does not help until the launch layer sets the marker (OD-6). That is a spawn-site register question, not a darwin question.
2. **Adding the marker to the launch layer widens the false-positive blast radius** (measured: `keyboxd`). Do not do (1) without the second conjunct.
3. **`ps -E` caveats:** same-UID only; exec-time snapshot (fine for spawn-time markers); env is space-appended to the command column, so only whitespace-free values parse unambiguously (a 12-hex hash is safe; an absolute path is not — argues for hashing, not `HARMONIK_PROJECT`); ARG_MAX truncation possible and fails closed.
4. **PL-021c is dead code with a normative promise attached.** Either wire the window-name sentinel into `handler.go:428`/`tmuxsubstrate.go:895`, or retire PL-021c.
5. **hk-5z4ww is live and unscoped** — anything this work says about "reapers must not match on argv" now has a shipped violation on the launch path.
6. **The codexdriver flip does not retire the substrate path** — `ReviewerSubstrate` is tmux on every path and `standard-bead.dot`'s `review` node pins claude-code.

---

## 10. Bottom-line judgment

**OD-1 as posed — "which darwin provenance mechanism: registry, PGID+start-time, or won't-fix" — is a side quest.** It governs a reaper with a zero-kill production record, on the ~6% branch of runs, for a marker population that is empty most of the time, and its premise (darwin cannot read process env) is factually wrong.

**The main event, in order:**
1. **OD-5** — naming the two regimes and, critically, naming what *neither* covers. The substrate-hosted, still-parented, `setsid()`'d grandchild is unreachable by every mechanism on the table, and Goal 2 ("the kill path reaches grandchildren … on both spawn branches") is **not achievable** by any OD-1 option. Say so, or ship C4 again.
2. **OD-2**, decided by §4.3: grandchild coverage comes from env inheritance or from nothing — which kills B1 on the merits.
3. **The real OD-1, restated**: transitive env inheritance is not proof of ownership — measured here with a peer project's daemon and the operator's gpg agent both wearing this project's marker. Platform-independent, and unresolved on Linux today.

The cheap part of OD-1 (**B4** — make darwin read the marker) should still land: small, portable, and the only thing giving the darwin grandchild case *any* reaper. It just is not the gate, and treating it as the gate is how this work ships the smaller half again.
