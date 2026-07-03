# Fleet Portability — Tasks (Pass 7)

> Implementation bead list. Each task = one bead (≈ one focused PR), right-sized for daemon
> dispatch. Ordering respects the HARD C2→regenerate→C1 sequence (06-integration §4). Review-gate
> flag (⚑RG) marks published-contract changes (the skill edits, the `crewSessionName` rename, the
> captain/keeper/supervisor session renames) that require an independent reviewer per project policy.
> All task beads carry the label `codename:fleet-portability`.

## Translations glossary (plain English)
- **C1** init-provisioning; **C2** path/identity de-hardcoding + session naming; **C3** multi-tenant
  global state; **C4** the cross-repo boundary (doc-only).
- **project_hash** = first 12 hex of SHA-256(realpath(project root)) — the per-project tmux/identity scope.
- **the sweep** = the daemon's startup orphan-sweep that kills stale `harmonik-<project_hash>-` tmux sessions.
- **fleet** = daemon + supervisor + captain + crews + keeper running on one repo.

---

## Task list

### Wave A — C2 path/identity FIRST (the hard prerequisite for C1's embed)

**T1 ⚑RG — De-hardcode the 6 fleet skills to `$HARMONIK_PROJECT`** (C2 / PL-030)
- Scope: Replace the 29 literal `/Users/gb/github/harmonik` hits across `.claude/skills/{captain/STARTUP.md, captain/SHUTDOWN.md, harmonik-lifecycle/SKILL.md, harmonik-dispatch/SKILL.md, major-issue-fanout/SKILL.md, keeper/SKILL.md}` with `$HARMONIK_PROJECT/...` (shell/agent contexts) or `<project root>/...` (prose), preserving the "project-local NOT `~/.claude/skills/`" disambiguation as prose at each site.
- Implements: `05-spec-drafts/process-lifecycle-C2.md` PL-030.
- Acceptance: `grep -rn "/Users/gb/github/harmonik" .claude/skills/` returns 0 hits; each former-literal site reads `$HARMONIK_PROJECT` with the fallback chain documented; the harmonik deployment itself still boots (skills resolve via the already-injected env).
- Deps: none. **Review-gate (published agent-launch contract).**

**T2 ⚑RG — `crewSessionName` → `harmonik-<project_hash>-crew-<name>`** (C2 / PL-006a)
- Scope: `internal/daemon/tmuxsubstrate.go:982` `crewSessionName(name)` → `crewSessionName(projectHash, name)` using `lifecycle.TmuxSessionName(projectHash, "crew-"+name)`; thread `projectHash` onto the `tmuxSubstrate` struct at construction; update call sites `SpawnCrewSession` (:1002) and `StopCrewSession` (:1064).
- Implements: PL-006a (amended) crew clause.
- Acceptance: a crew launches under `harmonik-<project_hash>-crew-<name>`; `crewlog.sh` (see T8) resolves it; two projects with the same crew name get distinct sessions. Existing crew tests updated.
- Deps: none (parallel with T1). **Review-gate (crew name is a published contract; `crewlog.sh` updates in lockstep — coordinate with T8).**

**T3 ⚑RG — Crew + captain/keeper orphan-sweep exemption (generalized PL-006d)** (C2 / PL-006d)
- Scope: `internal/daemon/orphansweep.go` — before both sweep calls, add live crew sessions to `excludedTmuxSessions` via a new `excludeLiveCrewSessions` helper (crew registry record `.harmonik/crew/<name>.json` + live pane-PID probe; dead crew NOT excluded + `crew.Remove` GC); generalize `probeCoordinatorSentinel` to a `(sentinelName, pidName, sessionName)` triple for captain/keeper (`captain.sentinel`/`captain.pid`, no-op until T7 writes them); add `crew_sessions_skipped` + `captain_sessions_skipped` to the sweep-completed payload. The supervisor session is `hk-`-prefixed (T4) so it is NOT prefix-matched and needs no exemption; the `-flywheel` sentinel exemption is UNCHANGED.
- Implements: PL-006d (generalized).
- Acceptance: a live crew (incl. idle-at-zsh) survives the sweep; a dead crew is reaped + its registry record GC'd; payload carries the two new counts; the live flywheel still survives. Validated by T13.
- Deps: T2 (needs the qualified crew session name). 

**T4 ⚑RG — `SupervisorSessionName` const→func, `hk-<project_hash>-daemon-supervise`** (C2 / 06-integration §2)
- Scope: `internal/lifecycle/tmux/subcommand.go:182` `const SupervisorSessionName` → `func SupervisorSessionName(projectDir string) string` returning `"hk-" + project_hash + "-daemon-supervise"` (keeps `hk-` prefix → OUTSIDE the sweep namespace per the integration decision); update the consumer `ResolveDaemonSpawnSession` (:213) and `resolvedaemonsession_hk9vp51_test.go` refs to pass `projectDir`.
- Implements: 06-integration §2 (resolved seam); PL-006a (supervisor NOT in the `harmonik-` list).
- Acceptance: two projects' supervisors get distinct `hk-<hash>-daemon-supervise` sessions; the sweep never prefix-matches them; `ResolveDaemonSpawnSession` tests pass.
- Deps: none. **Review-gate (published session name; one editor only — owned by C2 per integration §3).**

**T5 — `harmonik project-hash` read-only subcommand** (C3 / PL-031)
- Scope: new `cmd/harmonik/projecthash_cmd.go` — parse `[--project DIR]` (default CWD, realpath), print the 12-hex `project_hash` + newline via `lifecycle.ComputeProjectHash`, exit 0; side-effect-free (no daemon, no `$TMUX`); on error exit non-zero + empty stdout.
- Implements: PL-031.
- Acceptance: `harmonik project-hash --project <dir>` prints the same hash the Go core uses for `<dir>`; matches the daemon's `harmonik-<hash>-default` session hash; `$(... 2>/dev/null || true)` degrades cleanly on a bad path. Validated by T14 (the C2 exploratory bead hk-hwe).
- Deps: none (parallel). Consumed by T8, T9, T10.

### Wave B — C3 multi-tenant global state (parallel with Wave A except where noted)

**T6 — keeper Stop/PreCompact hook project-keyed dedup + single statusLine** (C3 / ON-058a)
- Scope: `cmd/harmonik/keeper_enable_doctor_cmd.go` — change the hook dedup key from script-basename to `(scriptBasename, "HARMONIK_PROJECT="+projectDir)` in `findHookForScript`/`updateHookCommand`/`mergeHookStanza` (+ call sites + the doctor find calls); drop the `HARMONIK_PROJECT=` prefix on the **statusLine** command only (single project-agnostic stanza resolving project from inherited env); drop the per-project statusLine sub-check in `doctor`.
- Implements: ON-058(a).
- Acceptance: enabling agent X for project A then project B leaves 2 sibling Stop groups (one per `HARMONIK_PROJECT`); re-enabling A keeps count at 2 (idempotent); a pre-existing operator Stop group survives; one project-agnostic statusLine stanza. Validated by T15 (hk-f5z).
- Deps: none (parallel).

**T7 ⚑RG — captain/keeper session naming + `captain.sentinel`/`captain.pid` writers in captain-launch.sh** (C3 / PL-006d, ties to T3)
- Scope: in the versioned `scripts/captain-tools/captain-launch.sh` (T8), name the captain session `harmonik-<project_hash>-captain` and keeper sessions `harmonik-<project_hash>-keeper-<role>` (via `harmonik project-hash`), and write `.harmonik/cognition/captain.sentinel` + `captain.pid` before `tmux new-session` (removed on clean exit). This is what makes T3's captain/keeper exemption non-noop.
- Implements: PL-006d mechanism (ii); PL-006a captain/keeper clauses.
- Acceptance: a launched captain is named `harmonik-<hash>-captain` and writes the sentinel+pid; T3's sweep skips it while live and reaps it when the sentinel is stale.
- Deps: T5 (project-hash), T8 (the script must exist to edit), T3 (the probe consumes the sentinel). **Review-gate (published Captain&Crew session-id/name minting contract — independent reviewer WITH T1/T2).**

**T8 — Version + de-hardcode captain-tools scripts; embed them** (C3 / ON-058b)
- Scope: move `captain-launch.sh` + `crewlog.sh` into `scripts/captain-tools/`, de-hardcoded: `HK_PROJECT` falls back to `${HARMONIK_PROJECT:-$(git rev-parse --show-toplevel)}`; `crewlog.sh` transcript slug computed `SLUG="$(echo "$PROJ" | sed 's#/#-#g')"`; crew session lookup updated for T2's rename; new `cmd/harmonik/init_assets.go` with `//go:embed captain-tools/...`; a guard test (embedded bytes == repo bytes).
- Implements: ON-058(b).
- Acceptance: `grep -rn "/Users/gb/github/harmonik" scripts/captain-tools/` = 0; embed-sync test passes; `crewlog.sh` resolves the T2-renamed crew sessions.
- Deps: T2 (crew rename), T5 (project-hash). Provisioned by T12.

**T9 — Per-project last-good binary** (C3 / ON-058c, release-pipeline §7.2)
- Scope: `internal/release/lastgood.go:27` `DefaultLastGoodStatePath()` → takes `projectDir`, returns `<projectDir>/.harmonik/state/last-good-binary`; thread `projectDir` through callers `release_cmd.go:357`, `supervise/shim.go:196`; no migration (absent file ⇒ fresh).
- Implements: ON-058(c); release-pipeline §7.2 (amended).
- Acceptance: `DefaultLastGoodStatePath(dir)` returns the per-project path; two projects don't share a last-good file; absent-file ⇒ `ErrNoLastGood`. Validated by T16 (hk-lbh).
- Deps: none (parallel).

**T10 — `/tmp` global de-collision: daemon log + keeper session + supervise fallback** (C3 / ON-058c)
- Scope: `scripts/hk-keeper.sh:44` qualify `LOG="${HK_LOG:-/tmp/hk-<hash>-daemon.log}"` + `SESS="${HK_SESS:-hkdkeeper-<hash>}"` via `harmonik project-hash`; new `scripts/hk-supervise.sh` (de-hardcoded PROJECT/BIN; session `hk-<project_hash>-daemon-supervise` matching T4); repoint the retired `/tmp/hk-daemon-supervise.sh` to `harmonik supervise`.
- Implements: ON-058(c); release-pipeline §7.2 / process-lifecycle C3 note.
- Acceptance: two projects' daemon logs + keeper sessions don't collide; `hk-supervise.sh` has no hardcoded path and names `hk-<hash>-daemon-supervise`.
- Deps: T5 (project-hash), T4 (the supervise session name must match).

### Wave C — C1 init (LAST among code: snapshots the de-hardcoded skills + versioned tooling)

**T11 — Init: embedded skills + AGENTS template + scaffolds + runtime dirs + config precedence + remove phantom guard** (C1 / PL-029, PL-004b)
- Scope: new `cmd/harmonik/initassets.go` (directory `embed.FS` for `assets/skills` (8 fleet skills), `assets/AGENTS.template.md`, `assets/crew-mission.template.md`; `go:generate` rsync from `.claude/skills/` + `docs/templates/AGENTS.template.md` + CI diff-check); `init_cmd.go` — `provisionSkills`/`provisionScaffolds`/`provisionCrewMission` steps (idempotent, never clobber siblings), `renderAgentsMD` reads from embed (not disk), runtime dirs `.harmonik/{comms,crew,keeper,queues}` + gitignore, DELETE the phantom `--target-branch` guard (:129-138) + scrub `hk-m8vy2` refs (init_cmd.go, `docs/setup-agent-prompt.md`, `harmonik-lifecycle/SKILL.md`); `projectconfig.go` — add the `daemon:` block (`max_concurrent`/`workflow_mode`/`target_branch`) + getters; `main.go` — `flag.Visit` was-set check + `LoadProjectConfig` pre-Config resolution for the precedence chain.
- Implements: PL-029, PL-004b.
- Acceptance: `harmonik init` from an INSTALLED binary on a foreign tmpdir repo exits 0, provisions the 8 skills + 3 scaffolds + runtime dirs, renders an AGENTS.md with no dangling links, accepts `--target-branch integration` (no phantom guard); a `config.yaml daemon:` block resolves `max_concurrent`/`workflow_mode` when the flags are omitted. Validated by T17 (hk-oa5), T18 (hk-9xi).
- Deps: **T1 (skills de-hardcoded BEFORE the embed snapshot — HARD), T8 (versioned captain-tools to provision)**. The go:generate snapshot MUST run after T1.

**T12 — Init provisions the versioned captain-tools (C1↔C3 seam)** (C1+C3 / ON-058b)
- Scope: `init` writes the embedded `captain-tools/*` to `~/.claude/captain-tools/` (chmod 0755) ONLY IF ABSENT (never clobber an operator copy).
- Implements: ON-058(b) provisioning obligation.
- Acceptance: `init` on a machine with no `~/.claude/captain-tools/` writes both scripts; `init` with an existing operator copy leaves it untouched.
- Deps: T8 (the embed asset), T11 (the init step framework).

### Wave D — docs + cross-spec payload

**T13 — Scenario test: crew sweep-exemption** (already filed: **hk-ndq**, scenario-test)
- Validates T3: live crew (incl idle-at-zsh) skipped; dead crew reaped + record GC'd; `crew_sessions_skipped` asserted.
- Deps: T2, T3.

**T14 — Exploratory test: `harmonik project-hash` + no hardcoded skill path** (already filed: **hk-hwe**)
- Validates T1, T5: hash matches the Go core; `grep` of shipped skills = 0 hits.
- Deps: T1, T5.

**T15 — Scenario test: two-project keeper-enable coexistence** (already filed: **hk-f5z**)
- Validates T6: two Stop groups coexist as siblings; single project-agnostic statusLine.
- Deps: T6.

**T16 — Exploratory test: two-fleet coexistence** (already filed: **hk-lbh**)
- Validates T2/T4/T7/T9/T10: project-qualified crew/captain/keeper/supervise sessions; per-project last-good; no `/tmp` collision.
- Deps: T2, T4, T7, T9, T10.

**T17 — Scenario test: init from installed binary on a foreign repo** (already filed: **hk-oa5**)
- Validates T11: installed-binary init on a fresh repo provisions skills + self-consistent AGENTS.md + exit 0.
- Deps: T11.

**T18 — Exploratory test: init --target-branch integration + config precedence** (already filed: **hk-9xi**)
- Validates T11: phantom guard gone; `config.yaml daemon:` block resolves the ops keys.
- Deps: T11.

**T19 — event-model.md §8.7.14 additive payload fields** (C2 / changelog item 6)
- Scope: add `crew_sessions_skipped` + `captain_sessions_skipped` (integers ≥ 0) to the `daemon_orphan_sweep_completed` payload schema in `specs/event-model.md` §8.7.14 (additive / N-1-tolerant, mirroring `coordinator_sessions_skipped`). If §8.7.14 mandates a schema bump, file a companion EV item.
- Implements: 06-integration §5 (event-model coordination).
- Acceptance: §8.7.14 lists both fields with N-1-tolerance language; consistent with PL-006d.
- Deps: none (spec-text, lands with T3's behavior).

**T20 — C4 doc-only: adopt hk-3r3 as the named cross-repo boundary** (C4)
- Scope: add the "known boundary — single supervised repo" note (06-integration §6) to the portability narrative + a one-line scope cross-reference in process-lifecycle/operator-nfr; reference hk-3r3; document the out-of-band path until it lands. NO code, NO new requirement ID.
- Implements: C4 (02-components §C4).
- Acceptance: the boundary is stated and hk-3r3 is referenced; `br show hk-3r3` confirmed open and adopted (label `codename:fleet-portability` added).
- Deps: none.

---

## Dependency graph (DAG)

```
T1 (skills de-hardcode) ─┐
                         ├─→ T11 (init/embed) ──→ T12 (provision tools) ──→ T17, T18 (init tests)
T8 (versioned tools) ────┘         ▲
T2 (crew rename) ──→ T3 (crew sweep exempt) ──→ T13 (sweep test)
T2 ──→ T8 (crewlog lookup)                       T19 (event payload, with T3)
T5 (project-hash) ──→ T7, T8, T10
T4 (supervise name) ──→ T10, and feeds T16
T6 (keeper hooks) ──→ T15
T7 (captain sentinel) needs T5,T8,T3
T9 (last-good) ─┐
T10 (/tmp) ─────┼─→ T16 (two-fleet test)  (also needs T2,T4,T7)
T20 (C4 doc) — independent
T14 (project-hash + skill-grep test) needs T1,T5
```

No cycles. Roots (no deps): T1, T2, T4, T5, T6, T9, T19, T20.

## Parallelization plan
- **Batch 1 (parallel):** T1, T2, T4, T5, T6, T9, T19, T20 — all root tasks, no shared file region (per 06-integration §3). NOTE T1, T2, T4, T7 are review-gated; their merge waits on an independent reviewer.
- **Batch 2 (after batch 1):** T3 (needs T2), T8 (needs T2,T5), T10 (needs T4,T5).
- **Batch 3:** T7 (needs T5,T8,T3), T11 (needs T1,T8 — the HARD C2→regenerate→C1 gate).
- **Batch 4:** T12 (needs T8,T11), then the test tasks T13–T18 as their impl deps land.

## Hard ordering note (load-bearing)
T1 (skill de-hardcoding) MUST merge BEFORE T11 runs its `go:generate` skill snapshot — otherwise the
embedded `assets/skills/` ships the literal `/Users/gb/github/harmonik` to every foreign repo. Enforce
via the T11→T1 dep and a CI diff-check that the embedded copy has no hardcoded path.

## Review-gate (⚑RG) tasks requiring an independent reviewer
T1 (skill edits), T2 (`crewSessionName` rename), T4 (`SupervisorSessionName` rename), T7
(captain/keeper session-name + sentinel minting). These are published-contract changes; per project
policy they do not land without an independent review pass (T1/T2/T4/T7 reviewed together since they
touch the coupled session-naming/skill contract).

---

## Task → bead ID map (filed in br, label `codename:fleet-portability`, all `open`)

| Task | Bead | Task | Bead |
|---|---|---|---|
| T1 (skills de-hardcode) ⚑RG | hk-yt5 | T11 (init embed/config/guard) | hk-7iyh |
| T2 (crewSessionName) ⚑RG | hk-ohd | T12 (init provisions tools) | hk-da3k |
| T3 (PL-006d sweep exempt) | hk-qp3 | T13 scenario crew-sweep | hk-ndq |
| T4 (SupervisorSessionName) ⚑RG | hk-3tj | T14 explore project-hash/skill-grep | hk-hwe |
| T5 (project-hash subcmd) | hk-dmw | T15 scenario keeper coexist | hk-f5z |
| T6 (keeper hooks/statusLine) | hk-qbx | T16 explore two-fleet | hk-lbh |
| T7 (captain sentinel) ⚑RG | hk-zv5 | T17 scenario init-foreign-repo | hk-oa5 |
| T8 (versioned captain-tools) | hk-9df | T18 explore init-target-branch/config | hk-9xi |
| T9 (per-project last-good) | hk-2bnk | T19 event-model payload | hk-zlwi |
| T10 (/tmp de-collide) | hk-n672 | T20 C4 doc adopt hk-3r3 | hk-1kzd (+ hk-3r3 adopted) |

Deps wired in br (br dep cycles: clean): T3→T2; T8→T2,T5; T7→T5,T8,T3; T10→T4,T5;
**T11→T1 (HARD: skills before embed),T8**; T12→T8,T11. Test beads depend on their impl tasks
(hk-ndq→T3; hk-hwe→T1,T5; hk-f5z→T6; hk-oa5/hk-9xi→T11; hk-lbh→T2,T4,T7,T9,T10).
