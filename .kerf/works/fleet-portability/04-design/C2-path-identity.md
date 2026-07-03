# Design — C2. Path & Identity Parameterization

> Pass 4 (`design`) of `fleet-portability`. Component C2 per `02-components.md`,
> grounded against the live tree on 2026-06-13 (every file:line below verified).

## 0. What C2 delivers (decisive scope)

Two coupled changes, one shared convention:

- **(a) De-hardcode the 5 shared skills** (29 hits) to a portable
  `$HARMONIK_PROJECT` convention that STILL disambiguates "the project-local
  `.claude/skills/`, NOT global `~/.claude/skills/`" — reversing 7b84264d's
  absolute-path fix to a portable form.
- **(b) Project-qualify the launch-layer tmux session names** (crew, supervisor,
  keeper, captain) using the EXISTING `ComputeProjectHash`/`tmuxStartHashDir`
  helper — one hashing scheme, no second one.
- **CRITICAL RISK solved:** moving crew/keeper/captain under the
  `harmonik-<project_hash>-` prefix exposes them to the PL-006 orphan sweep. The
  design gives each a **PL-006d-style live-owner sentinel exemption** AND routes
  them to the **orphan-TEST sweeper, never the unconditional one**, so a daemon
  restart cannot reap a live crew/keeper/captain.

The Go core is untouched beyond reusing its hash helper and extending the sweep's
exclusion set. The harmonik deployment itself becomes "project N=1" of the
portable path — no special case.

---

## 1. The critical risk, stated precisely

There are **two** session sweepers, and they have **different kill semantics** —
this distinction is the whole ballgame:

| Sweeper | File:line | Prefix matched | Kill rule |
|---|---|---|---|
| **(a1) unconditional** | `internal/lifecycle/orphansweep.go:115` `SweepOrphanTmuxSessions` | `TmuxSessionPrefix` = `harmonik-<12hex>-` (`provenance.go:106`) | **Kills ANY matching session not in `excludeSessions`** — no orphan test (`orphansweep.go:136-150`). |
| **(a1b) orphan-test** | `internal/lifecycle/tmux/orphansession.go:51` `SweepOrphanTmuxSessions` | `sessionOrphanPrefix` = `harmonik-<12hex>-` (`orphansession.go:22`) | Kills only if `sessionIsOrphaned` (`orphansession.go:108`): **all windows are `zsh`** OR first-pane PID dead. A live `claude` window is non-zsh → survives. |
| **(a2) window** | `internal/lifecycle/tmux/orphanwindow.go:44` `SweepOrphanTmuxWindows` | window names `hk-<hash6>-` (`orphanwindow.go:128`) | Kills orphan *windows* carrying the 6-hex sentinel inside operator sessions. |

Both session sweepers run every boot, sharing the **same** `excludedTmuxSessions`
map built in `internal/daemon/orphansweep.go:481-534` (`RunOrphanSweep`).

**The lethal one is (a1).** Today nothing crew/keeper/captain matches
`harmonik-<12hex>-` (they use `hk-crew-…`, `hk-daemon-supervise`, `hkdkeeper`),
so (a1) never touches them. The moment C2 renames them to
`harmonik-<hash>-crew-<name>` etc., **(a1) would kill them outright on every
daemon boot** — even while a crew is mid-bead — because (a1) does no orphan test.
(a1b) is survivable for a *busy* crew but NOT for an idle-but-live one: a crew
waiting on its comms inbox shows a window running `claude` (non-zsh) so it's fine,
but a keeper/captain session whose visible window is a bare shell would read as
orphaned. So **both** sweepers need the exemption.

The existing precedent that solves exactly this shape is **PL-006d**
(`internal/daemon/orphansweep.go:477-534`): the supervisor's `-flywheel` session
is spared by a **sentinel file + live-PID probe**. C2 generalizes that one
mechanism to crew/keeper/captain.

---

## 2. The naming scheme (decisive)

Reuse the canonical helpers — `lifecycle.TmuxSessionName(hash, suffix)` =
`"harmonik-" + hash + "-" + suffix` (`provenance.go:114`) and
`lifecycle.ComputeProjectHash` / `tmuxStartHashDir` (`subcommand.go:229`,
`provenance.go:33`, both `sha256(EvalSymlinks(dir))[:6]` → 12 hex). **No new
hashing.** New logical suffixes:

| Role | Today | C2 name | Builder |
|---|---|---|---|
| Crew | `hk-crew-<name>` (`tmuxsubstrate.go:983`) | `harmonik-<hash>-crew-<name>` | `crewSessionName(hash, name)` |
| Supervisor | `hk-daemon-supervise` (`subcommand.go:182`) | `harmonik-<hash>-supervise` | `SupervisorSessionName(hash)` (was const → func) |
| Keeper | `hkdkeeper` (`scripts/hk-keeper.sh:45`) | `harmonik-<hash>-keeper` | shell, computed from `$PROJ` |
| Captain | `hk-keeper-captain` (skill text only) | `harmonik-<hash>-keeper-captain` | `captain-launch.sh` (C3-owned) |

**Window-name discipline (WM-002a):** crew/keeper/captain *windows* MUST NOT carry
the `hk-<hash6>-` 6-hex sentinel, or the (a2) window sweep
(`orphanwindow.go:128`) would reap the window even inside a survived session. Keep
crew window names as descriptive (`crew-<name>`), NOT sentinel-prefixed. Only
**implementer/reviewer** windows carry the `hk-<hash6>-` sentinel
(`windowname.go:36`, `ownsSession=false` path) — that contract is unchanged.

---

## 3. The sentinel + exemption (the critical-risk solution)

### 3.1 Reuse the PL-006d shape, generalized to a per-role sentinel set

Today PL-006d hard-codes one session (flywheel) and one sentinel
(`.harmonik/cognition/supervisor.sentinel`, `orphansweep.go:256-258`). C2
generalizes to a **directory of live-owner sentinels**:

```
.harmonik/sessions/                       (gitignored, under already-isolated .harmonik/)
  crew-<name>.sentinel    + crew-<name>.pid
  keeper.sentinel         + keeper.pid
  keeper-captain.sentinel + keeper-captain.pid
```

Each `*.sentinel` is the schema_version=1 marker (mirroring
`cmd/harmonik/supervise/config.go:163` `WriteSentinel`); each `*.pid` holds the
owning process PID. The supervisor's existing
`.harmonik/cognition/supervisor.sentinel` is left exactly as-is (no migration) —
C2 ADDS a second sentinel root for the launch-layer roles, it does not move the
existing one.

### 3.2 Who writes each sentinel

- **Crew** — written by the daemon in `SpawnCrewSession`
  (`tmuxsubstrate.go:996`), right after `NewSessionIn` succeeds and the pane PID
  is known (`tmuxsubstrate.go:1030` `pid, _ := s.adapter.WindowPanePID(...)`).
  Write `crew-<name>.sentinel` + `crew-<name>.pid=<pid>`. Removed in
  `StopCrewSession` (`tmuxsubstrate.go:1051`) after the session is killed.
- **Keeper** — written by `scripts/hk-keeper.sh` after its `tmux new-session`
  (`:74`): `echo $$ > .harmonik/sessions/keeper.pid; touch
  .harmonik/sessions/keeper.sentinel`. The keeper script is a long-running loop,
  so `$$` is the live owner. (C3 versions this script; C2 specifies the sentinel
  write.)
- **Captain** — written by `captain-launch.sh` (C3-owned) after minting the
  captain session: `keeper-captain.{sentinel,pid}`. C2 specifies the contract;
  C3 lands the script edit. (Documented cross-component handoff per
  `path-identity/findings.md` Risk 3.)

### 3.3 How the sweep honors it — extend the existing probe

In `RunOrphanSweep` (`internal/daemon/orphansweep.go:481`), AFTER the existing
flywheel-sentinel block (`:495-534`) and BEFORE the (a1)/(a1b) calls
(`:538,550`), add a **launch-session sentinel scan** that adds every live-owner
session to the SAME `excludedTmuxSessions` map both sweepers already consume:

```go
// PL-006e (new): exempt live launch-layer sessions (crew/keeper/captain) from
// the orphan sweep. Mirrors PL-006d's sentinel+PID-probe, generalized to the
// .harmonik/sessions/ sentinel directory. A sentinel whose PID is dead is
// removed (stale-cleanup) and its session is NOT exempted (falls through to the
// normal sweep, which will reap it).
for _, s := range scanLaunchSentinels(projectDir, cfg.Logger) { // returns live ones only
    excludedTmuxSessions[lifecycle.TmuxSessionName(projectHash, s.suffix)] = struct{}{}
    result.LaunchSessionsSkipped++
}
```

`scanLaunchSentinels` reuses the existing liveness primitives verbatim:
`readSupervisorPID` (PID-file read), `syscall.Kill(pid, 0)` with the EPERM=live /
ESRCH=dead discrimination (`orphansweep.go:313-327`), and `removeStaleSentinel`
(`orphansweep.go:430`). `s.suffix` is `crew-<name>` / `keeper` /
`keeper-captain`, derived from the sentinel filename. This is a **direct
generalization of `probeCoordinatorSentinel`** — same code shape, iterating a
directory instead of one fixed file.

### 3.4 Belt-and-suspenders, mirroring `reapDeadCoordinatorSession`

The (a1) unconditional sweeper is the dangerous one. As a second guard, the (a1)
exclusion is **necessary AND sufficient** here: a live crew/keeper/captain is in
`excludedTmuxSessions`, so `orphansweep.go:140` skips it. If its sentinel PID is
dead, it is correctly reaped (the agent already exited). The only residual hazard
is a **race**: a crew spawned between the sentinel scan (`:481` region) and the
(a1) kill (`:538`). Mitigation: the daemon writes the crew sentinel **before**
returning the session handle in `SpawnCrewSession` (§3.2), and `RunOrphanSweep`
runs **once at daemon boot** (PL-006), not on a steady timer, so the window is
"crew spawned during boot sweep" — which cannot happen because no crew exists yet
at first-boot sweep, and on restart the prior crew's sentinel is already on disk.
No new lock needed.

---

## 4. Skills de-hardcoding (part a)

### 4.1 The convention

Replace every literal `/Users/gb/github/harmonik` in the 5 skills with
**`$HARMONIK_PROJECT`** (resolved at session boot), preserving 7b84264d's
disambiguation intent. The env var is **already injected** into every fleet
session — verified: crew (`internal/daemon/crewlaunchspec.go:80`
`"HARMONIK_PROJECT=" + rc.projectDir`), keeper hooks
(`keeper_enable_doctor_cmd.go:203-209`), and read by `promote_cmd.go:101`. So the
anchor exists; C2 just consumes it in the skills.

Canonical replacement forms:

- Absolute project path → `$HARMONIK_PROJECT`
  (e.g. `harmonik --project /Users/gb/github/harmonik` →
  `harmonik --project "$HARMONIK_PROJECT"`).
- Skill-path disambiguation (7b84264d's case) →
  `"$HARMONIK_PROJECT/.claude/skills/captain/STARTUP.md"` with the **explicit
  note retained**: "the PROJECT-LOCAL skill under `$HARMONIK_PROJECT/.claude/`,
  NOT global `~/.claude/skills/`." The disambiguation 7b84264d wanted is
  preserved; only the literal path is parameterized.
- `git -C /Users/gb/github/harmonik …` → `git -C "$HARMONIK_PROJECT" …`.

### 4.2 The 29 edits (per `path-identity/findings.md` F-C2.1)

| Skill file | Lines | Count |
|---|---|---|
| `.claude/skills/captain/STARTUP.md` | 5,8,34,39 | 4 |
| `.claude/skills/captain/SHUTDOWN.md` | 46,65,76,91,94,233,241,246,254,255,266 | 11 |
| `.claude/skills/harmonik-lifecycle/SKILL.md` | 54,354,358,359,360,361,364 | 7 |
| `.claude/skills/harmonik-dispatch/SKILL.md` | 16,24,44 | 3 |
| `.claude/skills/major-issue-fanout/SKILL.md` | 36,80,84 | 3 |
| `.claude/skills/keeper/SKILL.md` | 305 | 1 |

### 4.3 Fallback when `$HARMONIK_PROJECT` is unset

A skill may load in a session that did not export it (a human-launched captain
before the C3 launch tooling sets it). Each skill's first path-use adds a
**one-line guard**: "If `$HARMONIK_PROJECT` is unset, it is the git repo root —
`export HARMONIK_PROJECT=$(git rev-parse --show-toplevel)`." This is the
repo-root half of the convention named in G3, and it makes the skills robust on a
foreign repo with no harmonik-specific launcher. (C1 / C3 ensure the launchers
export it; this guard is the safety net.)

These are **published-contract edits** (skills other agents read) → route through
the review gate per the project rule and `02-components.md` C2 SPEC-vs-code note.

---

## 5. Code changes (file:line)

### 5.1 `crewSessionName` — thread the hash

`internal/daemon/tmuxsubstrate.go:982`

```go
// BEFORE
func crewSessionName(name string) string { return "hk-crew-" + name }
// AFTER
func crewSessionName(hash core.ProjectHash, name string) string {
    return lifecycle.TmuxSessionName(hash, "crew-"+name)
}
```

`tmuxsubstrate.go` today imports only `internal/lifecycle/tmux` (aliased `tmux`),
not `internal/lifecycle`/`internal/core` — but the `internal/daemon` package
already imports both (daemon.go, orphansweep.go) and `internal/lifecycle` does NOT
import `internal/daemon`, so adding the two imports here is **cycle-free**
(verified 2026-06-13).

`tmuxSubstrate` does not currently hold the project hash (struct at
`tmuxsubstrate.go:99` has only `adapter`, `sessionName`, …). **Add a
`projectHash core.ProjectHash` field** set via a new `TmuxSubstrateOption` in
`NewTmuxSubstrate` (`:369`), populated by the daemon at construction from the
hash it already computes for `DefaultSessionName`. Both call sites
(`tmuxsubstrate.go:1002` spawn, `:1064` stop) pass `s.projectHash`. The crew
*window* name (`tmuxsubstrate.go:1005`, `crewstart.go:194`) becomes
`"crew-"+name` (descriptive, **no** `hk-<hash6>-` sentinel — §2).

### 5.2 `SupervisorSessionName` — const → func

`internal/lifecycle/tmux/subcommand.go:182`

```go
// BEFORE
const SupervisorSessionName = "hk-daemon-supervise"
// AFTER
func SupervisorSessionName(projectDir string) string {
    return "harmonik-" + tmuxStartHashDir(projectDir) + "-supervise"
}
```

Update the one consumer, `ResolveDaemonSpawnSession` (`subcommand.go:213`), to
compare against `SupervisorSessionName(projectDir)` (projectDir is already in
scope there). The supervisor generator (`cmd/harmonik/supervise/start.go`, which
writes `/tmp/hk-daemon-supervise.sh`) is **C3's** surface for the script body; C2
owns only the Go session-name constant it must produce. Coordinate the rename so
the script's `tmux new-session -s` and the Go constant agree (single source: have
the script call `harmonik supervise --print-session-name` or pass it in — C3
decision).

### 5.3 Sweep exemption — `RunOrphanSweep` + new helper

- `internal/daemon/orphansweep.go` — add `scanLaunchSentinels(projectDir, logger)
  []launchSentinel` (new func near `probeCoordinatorSentinel`, `:291`), and the
  insertion at `:534`-end (§3.3). Add `LaunchSessionsSkipped int` to
  `OrphanSweepResult` and its `ToPayload`.
- New sentinel dir helpers mirroring `:251-264`:
  `launchSentinelDir(projectDir) = .harmonik/sessions/`,
  one `*.sentinel` + `*.pid` per role.

### 5.4 Keeper script (C3 lands; C2 specifies)

`scripts/hk-keeper.sh:45` `SESS="${HK_SESS:-hkdkeeper}"` →
`SESS="${HK_SESS:-harmonik-$(hk_hash "$PROJ")-keeper}"` where `hk_hash` shells out
to `harmonik print-project-hash --project "$PROJ"` (a new tiny read-only
subcommand reusing `ComputeProjectHash`) so the **shell and Go agree on one hash
function** — no reimplemented sha256 in bash. Plus the sentinel write (§3.2).

---

## 6. Edge cases

1. **Crew name collision across projects** — solved: `harmonik-<hashA>-crew-duncan`
   ≠ `harmonik-<hashB>-crew-duncan`. The published crew *logical* name
   (`duncan`) is unchanged; only the tmux session name is qualified. Comms
   identity (`HARMONIK_AGENT=duncan`) is unaffected (`crewlaunchspec.go`).
2. **`crewlog.sh` resolves crews by session name** (C3 finding F-C3.2) — it must
   be updated to the qualified name. C2 flags this as the consumer to fix;
   `crewlog.sh` lives in C3's versioned-tooling scope. The qualified name is
   derivable in the script via the same `print-project-hash` subcommand.
3. **Stale sentinel after a crash** — handled by §3.3's ESRCH branch: dead PID →
   `removeStaleSentinel` → session falls through to normal sweep and is reaped.
   No leak.
4. **`$HARMONIK_PROJECT` points at a symlink** — the hash uses
   `EvalSymlinks` (`subcommand.go:230`), and `print-project-hash` must too, so
   the skill's `--project "$HARMONIK_PROJECT"` and the daemon's own session names
   land on the same hash. The skills themselves don't hash; they only pass the
   path.
5. **Harmonik's own deployment** — its hash is just one concrete
   `<hash>`; it boots on the identical code path. No `if project == harmonik`
   anywhere. (Success-criterion: harmonik = project N=1.)
6. **Window sweep (a2) false-positive on crew window** — prevented by §2: crew
   windows are named `crew-<name>`, which does NOT start with `hk-<hash6>-`, so
   `orphanwindow.go:128` never matches them.
7. **PID reuse** — same risk PL-006d already accepts; the sentinel+PID probe is
   no weaker than the existing flywheel mechanism. Out of scope to harden beyond
   parity.

---

## 7. Spec changes (this is a spec-first project)

`specs/process-lifecycle.md` is the owner (`path-identity/findings.md` F-C2.5).

- **Extend PL-006a** (`:278-291`): the `harmonik-<project_hash>-<session_name>`
  session-naming contract now explicitly enumerates the launch-layer suffixes —
  `crew-<name>`, `supervise`, `keeper`, `keeper-captain` — in addition to
  `-default`/`-flywheel`. All fleet sessions carry the prefix.
- **New PL-006e** (sibling of PL-006d at `:322-331`): "Live launch-layer sessions
  (crew/keeper/captain) MUST be exempted from the orphan sweep via a
  per-role live-owner sentinel under `.harmonik/sessions/`, using the same
  sentinel-present + `kill(pid,0)`-live discrimination as PL-006d. A
  sentinel whose PID is dead MUST be removed and its session swept normally."
- **WM-002a note** (`windowname.go`-backed): launch-layer *windows* (crew/keeper)
  are descriptively named and MUST NOT carry the `hk-<hash6>-` window sentinel;
  only implementer/reviewer windows do.
- **`$HARMONIK_PROJECT` convention** (PL or a portability section): the shared
  skills resolve the project root from `$HARMONIK_PROJECT` (falling back to
  `git rev-parse --show-toplevel`), explicitly the project-local
  `$HARMONIK_PROJECT/.claude/skills/`, NOT global `~/.claude/skills/`.

---

## 8. Dependency handoffs

- **→ C1:** the skills C2 de-hardcodes are what `init` provisions; C1 ships the
  `$HARMONIK_PROJECT`-portable versions. The new `print-project-hash` read-only
  subcommand C2 adds is reused by C1's provisioned scripts.
- **→ C3:** the captain session sentinel/name lands in `captain-launch.sh`
  (C3-versioned); the keeper `SESS` + sentinel land in `hk-keeper.sh`
  (C3-versioned); `crewlog.sh` must learn the qualified crew name. C2 specifies
  the names + sentinel contract; C3 lands the script bodies. Same `hash6`.
- **Review gate:** the skill edits (§4) and the crew-name change (§5.1) are
  published-contract changes → independent reviewer required.

---

## 9. Verification (maps to success criteria)

1. `grep -rn "/Users/gb/github/harmonik" .claude/skills/` → 0 hits (SC-3).
2. Two repos, two daemons: `tmux ls` shows
   `harmonik-<hashA>-crew-*` and `harmonik-<hashB>-crew-*` distinct; restart
   daemon A while a crew runs in each → both crews survive
   (`scanLaunchSentinels` exempts them); kill A's fleet → B's sessions untouched
   (SC-2). This is the **direct test of the critical risk**: a scenario test that
   spawns a crew, writes its sentinel, runs `RunOrphanSweep`, and asserts the
   crew session is still alive (mirror the existing flywheel-exclusion scenario).
3. `harmonik print-project-hash --project <symlinked-path>` equals the daemon's
   own `-default` session hash (one scheme).
4. Boot a captain with `$HARMONIK_PROJECT` unset on a foreign repo → skill's
   `git rev-parse` fallback resolves the root (§4.3).
