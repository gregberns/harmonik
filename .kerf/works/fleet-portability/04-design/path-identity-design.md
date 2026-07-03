# C2 — Path & Identity Parameterization — DESIGN (synthesized)

> Pass 4 (`design`) of `fleet-portability`. Component C2 per `02-components.md`,
> grounded in research `03-research/path-identity/findings.md` and verified against the
> live tree on 2026-06-13 (file:line cited inline). Synthesizes two independent proposals;
> resolves the load-bearing orphan-sweep risk explicitly.

C2 has two sub-problems:
- **(a)** De-hardcode `/Users/gb/github/harmonik` from 5 skills (29 hits).
- **(b)** Project-qualify the launch-layer tmux session names (crew / supervisor / keeper /
  captain) using the per-project hash the core already uses — AND keep those newly-qualified
  *live* sessions safe from the daemon's restart orphan sweep (the load-bearing risk).

The two proposals agree on (a) and on the (b) naming scheme. They diverge on the (b)
sweep-exemption mechanism: Proposal 1 invents a new per-role sentinel directory
`.harmonik/sessions/{crew-<name>,…}.{sentinel,pid}`; Proposal 2 reuses the **existing**
crew registry (`.harmonik/crew/<name>.json`) + a live pane-PID probe. **This design adopts
Proposal 2's mechanism for crews** (strictly less new machinery; the registry is already
written before spawn) and **Proposal 1's "two sweepers with different kill rules" framing**
(it states the lethal path precisely). For captain/keeper (no Go owner) both proposals
agree on a sentinel-file that captain-launch.sh writes — a C2-defines / C3-implements seam.

---

## DECISIONS (+ rationale)

### D1 — Skill resolution convention: `$HARMONIK_PROJECT` with a `git rev-parse` fallback
Replace each literal `/Users/gb/github/harmonik/X` with `$HARMONIK_PROJECT/X`, and document
inline the fallback chain `$HARMONIK_PROJECT → git rev-parse --show-toplevel`.
**Rationale:** `$HARMONIK_PROJECT` is *already* injected by the fleet's launch paths — keeper
hooks (`keeper_enable_doctor_cmd.go:203,206,209`), crew launch specs
(`crewlaunchspec.go:80` — `"HARMONIK_PROJECT=" + rc.projectDir`), and read by `promote_cmd.go:101`.
C2 adds **no new injection point**; it reuses the established one. The fallback keeps skills
resolvable in a hand-launched session that forgot to export it.

### D2 — Preserve 7b84264d's disambiguation as PROSE, not a path
7b84264d hardcoded the absolute path so a captain with a populated *global* `~/.claude/skills/`
wouldn't misread the relative `.claude/skills/captain/SKILL.md` as the global copy. Keep that
intent at each of the 29 sites as an explicit clause: *"Read `$HARMONIK_PROJECT/.claude/skills/captain/STARTUP.md`
— the **project-local** skill, NOT `~/.claude/skills/captain/`."* The "NOT `~/.claude/skills/`"
warning survives; the literal path does not.
**Rationale:** research F-C2.2 — the hardcodes in the other 4 skills *pre-existed* 7b84264d;
the portable fix is identical regardless and must not re-lose the disambiguation.

### D3 — One hash scheme: reuse `lifecycle.TmuxSessionName` / `ComputeProjectHash`
Every launch-layer session moves to the canonical `harmonik-<project_hash>-<session_name>`
via the EXISTING `lifecycle.TmuxSessionName(hash core.ProjectHash, name string)`
(`internal/lifecycle/provenance.go:114` → `TmuxSessionPrefix(hash) + name`,
`:106` → `"harmonik-" + hash.String() + "-"`). The hash is `ComputeProjectHash(EvalSymlinks(dir))`
(`provenance.go:33`) — equivalently `tmuxStartHashDir(dir)` inside the tmux package
(`internal/lifecycle/tmux/subcommand.go:229`, reproduced inline there to avoid an import cycle).
**No second hashing scheme** (constraint, PL-006a). Mapping:

| Surface | Today | New name | Builder site |
|---|---|---|---|
| Crew | `hk-crew-<name>` | `harmonik-<hash>-crew-<name>` | `internal/daemon/tmuxsubstrate.go:982` `crewSessionName` |
| Supervisor | `hk-daemon-supervise` (const) | `harmonik-<hash>-daemon-supervise` | `internal/lifecycle/tmux/subcommand.go:182` |
| Captain | minted in captain-launch.sh | `harmonik-<hash>-captain` | **C3 tooling** (no Go owner) |
| Keeper | minted in launch tooling | `harmonik-<hash>-keeper-<role>` | **C3 tooling** |

This places crew/supervise inside the SAME `harmonik-<hash>-` prefix the daemon's own
`-default` (`subcommand.go:168`) and `-flywheel` (`orphansweep.go:363`) already use —
satisfying both the coexistence guarantee (distinct hash per project) and the PL-006a contract.

### D4 — Crew sweep-exemption proof = the crew registry record + a LIVE pane PID (NOT a new sentinel dir)
A crew is exempt from the orphan sweep iff (i) it has a registry record
`.harmonik/crew/<name>.json` (`internal/crew/registry.go` — `List` :165, `Write` :76,
`Record` :38, written BEFORE spawn at `crewstart.go:167`) AND (ii) its tmux pane PID is live
at sweep time. Liveness is probed exactly like `sessionIsOrphaned` already does:
`adapter.WindowPanePID(WindowHandle(sessionName + ":"))` (`adapter.go:179`) then `kill(pid,0)`.
**Rationale (resolves the Proposal-1-vs-2 divergence):** the crew `Record` carries `Handle`
and `SessionID` but **NO PID field** (`registry.go:38-46`) — so a *stored* PID is not available;
Proposal 1's per-role `.pid` sentinel would require adding a PID-write to every crew launch.
Proposal 2's *live pane-PID probe* needs no schema change, reuses the exact liveness primitive
the sweep already trusts (`sessionIsOrphaned`, `orphansession.go:131-150`), and the registry
record is the durable "this name is ours" marker (already written pre-spawn). This is the
PL-006d sentinel pattern with the registry record playing the sentinel-file role and the live
pane PID playing the `supervisor.pid` role — one mechanism, no new file format, no new dir.

### D5 — Supervisor/captain/keeper exemption = sentinel + pid (PL-006d generalized)
The supervisor session (`harmonik-<hash>-daemon-supervise`) is exempted via the EXISTING
`probeCoordinatorSentinel` (`orphansweep.go:291`): when `probe.Live`, add the supervise
session name to `excludedTmuxSessions` alongside the flywheel session already added at
`orphansweep.go:502-503`. The captain/keeper have no Go owner, so captain-launch.sh (C3) writes
`.harmonik/cognition/captain.sentinel` + `captain.pid` exactly as `supervise start` writes the
supervisor sentinel; C2 generalizes `probeCoordinatorSentinel` to accept a
`(sentinelName, pidName, sessionName)` triple and adds the captain/keeper names to the probe
list. **Until C3 lands, captain/keeper keep their current outside-the-prefix names so nothing
regresses** (they are simply not in the swept namespace yet).

### D6 — Crew WINDOW name stays session-scoped (do not qualify it)
The crew window name (`crewstart.go:194` / `tmuxsubstrate.go:1005`: `"hk-crew-" + name`) is a
window INSIDE the crew's own session; it does not collide across projects (it is session-scoped)
and it carries no `hk-<hash6>-` window-sentinel, so the window-orphan sweep
(`SweepOrphanTmuxWindows`, `orphansweep.go:558`) cannot reap it. Leave it — qualifying it is
unnecessary and lower-risk untouched. (Optional log-clarity rename is deferrable.)

---

## MECHANISM (concrete edits, file:line)

### M1 — Skills (sub-problem a)
Replace each literal at the 29 sites (research F-C2.1):
- `.claude/skills/captain/STARTUP.md` (5,8,34,39) + `SHUTDOWN.md` (46,65,76,91,94,233,241,246,254,255,266) — 15
- `.claude/skills/harmonik-lifecycle/SKILL.md` (54,354,358,359,360,361,364) — 7
- `.claude/skills/harmonik-dispatch/SKILL.md` (16,24,44) — 3
- `.claude/skills/major-issue-fanout/SKILL.md` (36,80,84) — 3
- `.claude/skills/keeper/SKILL.md` (305) — 1

Each `/Users/gb/github/harmonik/X` → `$HARMONIK_PROJECT/X` (shell/agent-readable contexts), or
`<project root>/X` in prose with the D2 disambiguation clause. The captain-tools script ref
(`~/.claude/captain-tools/captain-launch.sh`) is C3's surface — leave it. **These are
published-contract edits → independent review gate** (constraint; research risk #2).

### M2 — `crewSessionName` project-qualification
`internal/daemon/tmuxsubstrate.go:982`:
- `func crewSessionName(name string) string { return "hk-crew-" + name }`
  → `func crewSessionName(projectHash core.ProjectHash, name string) string {
       return lifecycle.TmuxSessionName(projectHash, "crew-"+name) }`
- Thread `projectHash core.ProjectHash` onto the `tmuxSubstrate` struct at construction
  (it already has `projectDir` available for spawns), NOT recomputed per call. Update the two
  call sites — `SpawnCrewSession` (`:1002`) and `StopCrewSession` (`:1064`), both methods on
  `*tmuxSubstrate` — to pass `s.projectHash`. Confirmed cycle-free: `internal/daemon` already
  imports `internal/lifecycle` (`orphansweep.go` uses `lifecycle.TmuxSessionName`).
- **Published-contract change** (crew names) → review gate; `crewlog.sh` (C3) resolves crew
  sessions by name and MUST be updated in lockstep (C2↔C3 coordination contract).

### M3 — `SupervisorSessionName` const → func
`internal/lifecycle/tmux/subcommand.go:182`:
- `const SupervisorSessionName = "hk-daemon-supervise"`
  → `func SupervisorSessionName(projectDir string) string {
       return "harmonik-" + tmuxStartHashDir(projectDir) + "-daemon-supervise" }`
- The one in-tree consumer is `ResolveDaemonSpawnSession` (`subcommand.go:211-217`), which
  compares `live == SupervisorSessionName` at `:213` — update to
  `live == SupervisorSessionName(projectDir)` (projectDir is already a parameter there).
- The live `/tmp/hk-daemon-supervise.sh` script that *creates* the supervise session is C3's
  generator surface; C2 changes the Go const/comparison, C3 changes the script. **C2/C3 seam.**

### M4 — Sweep exemption in `RunOrphanSweep`
`internal/daemon/orphansweep.go`, inside `RunOrphanSweep`, BEFORE the two sweep calls
(`:538` `lifecycle.SweepOrphanTmuxSessions`, `:550` `ltmux.SweepOrphanTmuxSessions`),
extending the EXISTING `excludedTmuxSessions` map (built at `:481`):

1. **Crew exemption (D4).** New helper
   `excludeLiveCrewSessions(projectDir, projectHash, adapter, logger) → adds to excludedTmuxSessions`:
   - `crew.List(projectDir)` (`registry.go:165`) → all `Record`s.
   - For each record, `session = lifecycle.TmuxSessionName(projectHash, "crew-"+rec.Name)`.
   - Probe `adapter.WindowPanePID(WindowHandle(session+":"))` then `kill(pid,0)`:
     - **alive (nil or EPERM)** → `excludedTmuxSessions[session] = struct{}{}` (exempt from BOTH paths).
     - **dead (ESRCH) / pid<=0 / lookup-fails-because-session-absent** → do NOT exclude (genuine
       orphan; normal sweep reaps it) AND `crew.Remove(projectDir, rec.Name)` to GC the stale record
       (mirrors `removeStaleSentinel`).
     - **session not yet in `adapter.ListSessions` (launch-in-flight)** → exclude conservatively
       (see Risk R3); it won't be enumerated by this sweep's snapshot anyway.
   - Increment `result.CrewSessionsSkipped` per exclusion.

2. **Supervisor exemption (D5).** Inside the existing `if probe.Live` block (`:500-507`), add
   `excludedTmuxSessions[ltmux.SupervisorSessionName(projectDir)] = struct{}{}` next to the
   flywheel exclusion at `:502-503`.

3. **Captain/keeper exemption (D5).** Generalize `probeCoordinatorSentinel` (`:291`) to a
   `(sentinelName, pidName, sessionName)` triple; probe `captain.sentinel`/`captain.pid` and the
   keeper sentinel; on `Live`, add `harmonik-<hash>-captain` / `harmonik-<hash>-keeper-<role>` to
   the exclude set. Increment `result.CaptainSessionsSkipped`. **C3 writes the sentinel files;
   until then this probe finds nothing and is a no-op** — no regression.

### M5 — Spec changes (`specs/process-lifecycle.md`)
- **PL-006a** (:282): extend the enumerated session-name users from the implicit
  `-default`/`-flywheel` to explicitly include `crew-<name>`, `daemon-supervise`, `captain`,
  `keeper-<role>` — all carrying `harmonik-<project_hash>-`.
- **PL-006d** (:322-339): rename from "coordinator/orchestrator tmux sessions" to
  **"live-owned launch-layer tmux sessions"** and generalize the exclusion rule: the exclusion
  applies to any prefix-matched session whose owner is provably live, where the owner-proof is
  (i) `supervisor.sentinel`+`supervisor.pid` for supervisor/flywheel (unchanged),
  (ii) `captain.sentinel`+`captain.pid` for captain/keeper (new; C3-written),
  (iii) a crew registry record + a live crew pane PID for crews (new).
  Add `crew_sessions_skipped` and `captain_sessions_skipped` integer fields to the
  `daemon_orphan_sweep_completed` payload (additive, N-1 tolerant, exactly like the existing
  `coordinator_sessions_skipped` at :333).
- **PL-021b / WM-002a** (:601-603) window-sentinel discipline is unaffected — these are
  *sessions*; the `hk-<hash6>-` window sentinel for $TMUX-reuse stays as-is.
- Document the `$HARMONIK_PROJECT` skill-resolution convention (D1/D2) in the same spec.

### M6 — Shared hash for shell + Go (handoff to C3)
C2 defines a new read-only `harmonik print-project-hash [--project <dir>]` subcommand so
captain-launch.sh / crewlog.sh (C3) derive the SAME 12-char hash via the Go helper rather than
re-implementing sha256-of-EvalSymlinks in shell. C2 ships the subcommand; C3 consumes it.

---

## RISK RESOLUTION (the load-bearing risk)

**Risk (research risk #1, "Load-bearing"): orphan-sweep kill-the-namespace vs newly-qualified
live sessions.** Today crews are invisible to the sweep (prefix `hk-crew-` ≠ `harmonik-<hash>-`)
— this accidental protection is exactly why the `SpawnCrewSession` decoupling comment
(`tmuxsubstrate.go:986`, hk-mmlqt) holds. Moving crews under `harmonik-<hash>-` REMOVES that
protection and exposes them to **two sweepers with different kill rules** (verified in code):

1. `lifecycle.SweepOrphanTmuxSessions` (`orphansweep.go:538`, **path a1**) kills EVERY
   prefix-matched session **unconditionally** — there is NO `sessionIsOrphaned` check in this
   path (`internal/lifecycle/orphansweep.go:140-160`); only `excludeSessions` saves a session.
   **This path is lethal to a live crew mid-bead.**
2. `ltmux.SweepOrphanTmuxSessions` (`orphansweep.go:550`, **path a1b**) kills only if
   `sessionIsOrphaned` (`orphansession.go:100-150`: all-zsh windows OR dead first-pane PID). A
   crew running a live Claude pane survives this — but a crew **idle at a zsh prompt between
   tasks** is classified orphaned (condition 1, all-zsh) and would be killed.

**Resolution (D4/D5/M4) — safe by construction:**
- A **live** crew/supervisor/captain is added to `excludedTmuxSessions`. Path a1 skips it
  (`orphansweep.go:140`: `if _, skip := excludeSessions[name]; skip { continue }`) and path a1b
  skips it (`orphansession.go:75`). **Cannot be reaped, even when idle-at-zsh** — the exclusion
  is checked BEFORE `sessionIsOrphaned`, so the all-zsh classification never applies to an
  excluded live crew. (Exactly how PL-006d exempts the live flywheel regardless of its window
  state.)
- A **dead** crew (pane PID ESRCH, or session absent) is NOT excluded → reaped normally, and its
  stale registry record is `crew.Remove`d. This is a leak we *want* swept now that crews are
  in-namespace — net improvement over today.
- The exemption is **read-only liveness at sweep time** (same `kill(pid,0)` discipline as PL-006d
  and `sessionIsOrphaned`) → deterministic given filesystem + process state (preserves PL-007).
- **Two projects, same crew name** (both have `crew-alpha`): distinct hashes →
  `harmonik-<hashA>-crew-alpha` vs `harmonik-<hashB>-crew-alpha`; each daemon's sweep filters on
  its own `TmuxSessionPrefix(hash)` (`provenance.go:106`) and never sees the other's. Zero
  cross-project reap.

---

## OPEN / VERIFY-FIRST ITEMS (for the captain)

- **[VERIFY — pre-impl] Does `tmuxSubstrate` already hold a usable `projectHash`/`projectDir`?**
  M2 threads `projectHash` onto the struct. Confirm the constructor has `projectDir` in scope
  (it builds spawns with `spawn.Cwd`) and that adding a `core.ProjectHash` field introduces no
  import cycle (spot-check: `internal/daemon` already imports `internal/lifecycle`). If the
  substrate constructor does not receive `projectDir`, the field must be wired from the daemon's
  config — flag the wiring point.
- **[C2↔C3 SEAM — load-bearing] Captain/keeper session names + sentinels are C3-owned.**
  C2 generalizes `probeCoordinatorSentinel` and reserves the names; C3 must (a) name the captain
  session `harmonik-<hash>-captain` in captain-launch.sh and (b) write `captain.sentinel`+`captain.pid`.
  Until C3 lands, captain/keeper keep current names (outside the swept prefix) — verify the C2
  probe is a no-op (no false skip) when those sentinels are absent.
- **[C2↔C3 SEAM] `crewlog.sh` must update in lockstep with the `crewSessionName` rename.**
  It resolves crew sessions by name; a stale `hk-crew-<name>` lookup will break after M2. Name
  this as the coordination contract for the review gate (research risk #2).
- **[C2↔C3 SEAM] `harmonik print-project-hash` (M6)** is the shared shell↔Go hash source —
  confirm C3 wants this surface vs. re-deriving in shell; if C3 prefers an env var, swap M6 for
  injecting `HARMONIK_PROJECT_HASH` into the captain/keeper launch env (note: that var already
  exists for handler subprocesses — `provenance.go:19`).
- **[REVIEW GATE] The 29 skill edits + the `crewSessionName` rename are published-contract
  changes** → independent reviewer required (constraint).
- **[TEST] Extend the PL-006d sweep tests** (`orphansweepcomplete_pl_inv_test.go`,
  `clicommands_pl028_test.go`) with a live-crew fixture asserting `crew_sessions_skipped` and
  non-kill of an idle-at-zsh crew; assert a dead crew IS reaped and its record GC'd.

---

## Critical files for implementation
- `internal/daemon/orphansweep.go` (M4 — add crew + supervisor/captain live-owner exemption to
  `excludedTmuxSessions` at :481-538; generalize `probeCoordinatorSentinel` :291)
- `internal/daemon/tmuxsubstrate.go` (M2 — `crewSessionName` :982 → `lifecycle.TmuxSessionName`;
  thread `projectHash` onto `tmuxSubstrate`; call sites :1002, :1064)
- `internal/lifecycle/tmux/subcommand.go` (M3 — `SupervisorSessionName` :182 const→func;
  `ResolveDaemonSpawnSession` :213 comparison; `tmuxStartHashDir` :229 reused)
- `internal/crew/registry.go` (D4 — `List`/`Record`/`Remove` are the durable crew sentinel
  substrate the exemption reads; NOTE: `Record` has no PID — liveness is a live pane-PID probe)
- `internal/lifecycle/provenance.go` (`TmuxSessionName` :114, `TmuxSessionPrefix` :106,
  `ComputeProjectHash` :33 — the single hashing scheme, reused not replaced)
- `specs/process-lifecycle.md` (M5 — PL-006a :282 session-name extension; PL-006d :322-339
  generalize sentinel to crew/captain + new skip-count payload fields)
- Skill edit sites (review-gated): `.claude/skills/{captain/STARTUP.md, captain/SHUTDOWN.md,
  harmonik-lifecycle/SKILL.md, harmonik-dispatch/SKILL.md, major-issue-fanout/SKILL.md,
  keeper/SKILL.md}`
