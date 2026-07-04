# 02 — Launch-time context injection mechanics

How context reaches a freshly-launched Claude session on the Go daemon + tmux + claude path.
Two distinct launch paths (captain vs crew) and one re-hydration path (keeper cycle).

## 1. `harmonik start captain` / `harmonik captain`

Entry: `cmd/harmonik/captain.go` → `runCaptainSubcommand` (:217) → `runCaptainLaunchWithOps` (:315).

Sequence:
- `ensureBootAssets(project,...)` (captain.go:435, defined :251) — create-if-missing provisions
  `.claude/skills/`, scaffolds, **context tiers**, and renders `AGENTS.md` router (`renderAgentsMD`,
  substitutes `$TARGET_BRANCH`). This is the ONLY file-content injection captain gets at launch —
  everything else is env + a tmux session with NO seed paste.
- keeper hooks wired into `~/.claude/settings.json` BEFORE tmux launch (:440-450) via `enableKeeper`
  (statusLine + Stop + PreCompact stanzas).
- **tmux launch** — `buildCaptainTmuxCmd` (captain.go:202):
  `tmux new-session -d -s <session> -n agent -e HARMONIK_AGENT=<name> claude --dangerously-skip-permissions --remote-control <label> --session-id <uuid>`
- session-id: `--session-id` minted UUIDv4 or validated (`keeper.IsPrimarySID`) (:379-386). Stable id is
  load-bearing for the keeper clear→resume rebind.
- sentinel/pid written to `.harmonik/cognition/captain.{sentinel,pid}` (:466) so orphan-sweep skips it.
- keeper WATCHER armed in sibling `keeper` window (:493, `SpawnKeeperWindow`).

**KEY GAP / injection point:** the captain gets NO seed prompt / bracketed paste. The pane comes up on a
bare `claude` REPL. Mission/operating-instructions today reach a captain only via files it reads at boot
(`AGENTS.md`, `STARTUP.md`, `HANDOFF.md`) — nothing points it there at launch. A launch-time seed paste
(mirroring the crew path below) is the natural place to inject "read STARTUP.md / HANDOFF-captain.md".

## 2. `harmonik crew start <name>` — has an actual seed paste

CLI: `cmd/harmonik/crew.go` → `runCrewStartCore` (:201). Parses args (`resolveCrewStartArgs` :131),
`ensureBootAssets` (:236), wires keeper hooks (:242), then sends a `crew-start` RPC to the daemon with
payload `{name, queue, mission_path}` (crew.go:249). `--mission` is the ONLY mission source on a fresh
start (D3, never reads the on-disk default). Seeds `.harmonik/keeper/<name>.sid` (crew.go:304 `seedSID`).

Daemon side: `internal/daemon/crewstart.go` `HandleCrewStart` (:205):
1. resolveSessionID (:218) — new UUID or `--resume` if session already on disk (`resolveSessionID` :370).
2. write crew registry record (:224).
3. ensure named queue (:235).
4. build launch spec (`buildCrewLaunchSpec`, `crewlaunchspec.go:92`) + spawn tmux session with `agent` +
   `keeper` windows (`SpawnCrewSession` :286).
5. **paste mission kick-off** (crewstart.go:302-304 → `pasteCrewMissionToSession` :502 → `pasteCrewMission` :437).

The seed text (crewstart.go:447), delivered via bracketed paste (`WriteLastPane` = tmux load-buffer +
paste-buffer), then a settle + submit Enter:
```
Please read <handoffPath> and run /session-resume on it, then begin your operating loop.
```
`<handoffPath>` = the `--mission` path. So the crew launcher points the agent at its handoff and the
**session-resume skill** — that skill reads the file and the crew re-derives its operating loop
(crew-launch skill). Splash-dismiss + post-paste settle discipline lives in `pasteinject.go` (verify-and-
retry loop, `pasteVerify*`, hk-zexsj) — the shared bracketed-paste substrate.

launch-spec argv (`crewlaunchspec.go:112/114`):
`claude --dangerously-skip-permissions --remote-control <label> (--session-id|--resume) <uuid> [--model <m>]`
env (`crewlaunchspec.go:124`): `HARMONIK_AGENT=<name>`, `HARMONIK_PROJECT=<projectDir>`.
`--model` optionally pinned from mission YAML front-matter `model:` (`readMissionModel` :556).

**This is the concrete injection point** where a crew's mission/operating-instructions could be layered:
either into the pasted seed string (crewstart.go:447) or into the referenced HANDOFF/mission file.

## 3. KEEPER-IDENTITY block — re-hydration on restart

Authored in `internal/keeper/cycle.go`, `identityBlock(agentName)` (:456). Emitted verbatim:
```
<!-- KEEPER-IDENTITY -->
**Agent identity (keeper-authoritative):** You are `<name>`. Your HARMONIK_AGENT environment variable is `<name>`.
Use `harmonik comms send --from <name>` (or rely on $HARMONIK_AGENT).
Do not reconstruct identity from conversation history — trust this line.
<!-- /KEEPER-IDENTITY -->
```
Injected during a keeper reset cycle, NOT at first launch. In `runCycle` (cycle.go:895), AFTER the agent's
`/session-handoff` nonce is confirmed (Step 3b, :1016) it is APPENDED to `HANDOFF-<name>.md`
(`defaultHandoffFilePath` :422 → `<projectDir>/HANDOFF-<agent>.md`) via `AppendHandoffFn`. Step 3c
(:1022) also sets `HARMONIK_AGENT` in the tmux session env (`SetTmuxEnv`) so the post-`/clear` claude
process inherits it. Then `/clear` (:1027) and later `/session-resume` re-read that handoff, so the resumed
agent reads its identity from the pinned block rather than guessing.

So identity survives a keeper cycle via TWO channels: (a) the KEEPER-IDENTITY text pinned into the handoff
file the resume reads, and (b) the `HARMONIK_AGENT` tmux env var inherited by the new process.

## 4. Data the launcher has per-role that it *could* inject

Captain (`runCaptainLaunchWithOps`): name, tmuxSession, sessionID, project dir, rcPrefix, keeper band
flags, `HARMONIK_AGENT` env, `TargetBranch` (from `LoadProjectConfig`). No mission concept, no seed paste.

Crew (`buildCrewLaunchSpec` + `HandleCrewStart`): name, queue, sessionID/resume flag, projectDir,
rcPrefix, `mission_path`, model (from mission front-matter), env `HARMONIK_AGENT` + `HARMONIK_PROJECT`,
plus the free-form seed string at crewstart.go:447.

Env vars available to any launched session: `HARMONIK_AGENT` (identity, read as default `--from`/`--name`
throughout `cmd/harmonik/comms.go`), `HARMONIK_PROJECT` (crew only), `HARMONIK_SESSION_ID`
(comms.go:813, operator-set session token). No `HARMONIK_ROLE`/`HARMONIK_MISSION` env exists today.

## Where to layer mission / operating-instructions / handoff

- Crew: extend the seed paste (`crewstart.go:437/447`) or the mission file it references — cleanest hook.
- Captain: NO seed paste exists — adding one in `runCaptainLaunchWithOps` (after tmux launch, mirroring
  the crew paste path) is the missing symmetric injection point; today captain relies wholly on
  `ensureBootAssets`-provisioned `AGENTS.md`/`STARTUP.md` files it must find on its own.
- Restart continuity for both: the handoff file (`HANDOFF-<name>.md`) + KEEPER-IDENTITY append +
  `HARMONIK_AGENT` tmux env (`cycle.go` Steps 3b/3c) — the established channel for re-injecting identity
  and could carry operating-instructions the same way.
