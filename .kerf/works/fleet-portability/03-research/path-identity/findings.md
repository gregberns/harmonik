# Research — C2. Path & identity parameterization

> Pass 3 (`research`) of `fleet-portability`. Component C2 per `02-components.md`. Verified
> against the live tree on 2026-06-13 (5-agent assessment + independent code-verification pass).

## Research questions

- RQ-C2.1 — How many shared skill files hardcode `/Users/gb/github/harmonik`, and where (corrected per-file counts)?
- RQ-C2.2 — What did commit 7b84264d actually do, and is the "hardcode the absolute path into skills" framing a regression?
- RQ-C2.3 — Are the launch-layer tmux session names (crew / supervisor / keeper / captain) project-qualified? Where are they built?
- RQ-C2.4 — What is the reusable per-project hash helper the core already uses, so the fix reuses it (not a new scheme)?
- RQ-C2.5 — What does the spec corpus (process-lifecycle.md, workspace-model.md) already mandate about project-hash session namespacing, and what does it NOT yet cover?

## Findings

### F-C2.1 — 5 skill files hardcode the path; 29 total hits (RQ-C2.1) — CORRECTED ("6 skills" -> 5)

`grep -rn "/Users/gb/github/harmonik" .claude/skills/` returns **29 hits across 5 files** (the assessment said "6 skills"; the correct count is 5 — the per-file breakdown otherwise matches):

- `.claude/skills/captain/STARTUP.md` (×4: lines 5,8,34,39) + `SHUTDOWN.md` (×11: 46,65,76,91,94,233,241,246,254,255,266) = **15**
- `.claude/skills/harmonik-lifecycle/SKILL.md` — **7** (54,354,358,359,360,361,364)
- `.claude/skills/harmonik-dispatch/SKILL.md` — **3** (16,24,44)
- `.claude/skills/major-issue-fanout/SKILL.md` — **3** (36,80,84)
- `.claude/skills/keeper/SKILL.md` — **1** (305)

No symlink in `~/.claude/skills/` — these skills are **project-local only**, so a foreign repo gets none of them (ties to C1's skills-provisioning gap) AND the ones it would get are path-poisoned with harmonik's absolute path.

### F-C2.2 — Commit 7b84264d hardcoded the absolute path into BOOT DOCS to fix a relative-path misread; the hardcodes elsewhere PRE-EXISTED (RQ-C2.2) — CONFIRMED, with nuance

`git show 7b84264d`: "docs(captain): make captain/crew skill paths explicitly project-local in boot instructions." A booting captain with a populated **global** `~/.claude/skills/` read the bare relative `.claude/skills/captain/SKILL.md` as the *global* path and hit "file does not exist" — so the commit made the path unambiguous by writing the absolute `/Users/gb/github/harmonik/.claude/skills/...` into three boot-first surfaces (AGENTS.md boot pointer, STARTUP.md header, SHUTDOWN.md handoff template). **Nuance (corrects the assessment's "introduced"):** the hardcoded paths in the *other* skills (harmonik-lifecycle, harmonik-dispatch, major-issue-fanout, keeper) **pre-existed** 7b84264d (confirmed via `git show 7b84264d^:.claude/skills/harmonik-lifecycle/SKILL.md`). So 7b84264d is the *most recent and most visible* instance of the anti-pattern, not its origin. The portable fix is the same regardless: a `$HARMONIK_PROJECT` / repo-root convention that resolves the project root WITHOUT a literal path, while preserving the disambiguation 7b84264d was after (explicitly "the project-local skills, NOT `~/.claude/skills/`").

### F-C2.3 — Launch-layer session names are NOT project-qualified (RQ-C2.3) — CONFIRMED

- **Crew [P0]:** `internal/daemon/tmuxsubstrate.go:983` — `func crewSessionName(name string) string { return "hk-crew-" + name }`. No project hash. Called by `SpawnCrewSession` (:1002). Two projects' crews named the same collide.
- **Supervisor [P1]:** `internal/lifecycle/tmux/subcommand.go:182` — `const SupervisorSessionName = "hk-daemon-supervise"`. A bare constant; no hash. Two projects' supervisors collide on this exact name.
- **Keeper / captain [P1]:** `hk-keeper-captain` / `hk-keeper-*` appear in the captain skill boot context (STARTUP.md:259,264) with no project hash. There is **no dedicated captain session constant in Go** — the captain is a human/LLM-invoked session; its session name comes from the launch tooling (captain-tools, see C3), not the daemon.

This is the exact opposite of the daemon's OWN sessions, which ARE qualified (F-C2.4/F-C2.5).

### F-C2.4 — The reusable per-project hash helper (RQ-C2.4) — CONFIRMED

`internal/lifecycle/tmux/subcommand.go:229` — `tmuxStartHashDir(dir)`:

    resolved, err := filepath.EvalSymlinks(dir)   // fall back to dir on err
    sum := sha256.Sum256([]byte(resolved))
    return fmt.Sprintf("%x", sum[:6])             // 6 bytes -> 12 lowercase hex chars

This is the canonical per-project hash (replicates `lifecycle.ComputeProjectHash`, spec PL-006a; reproduced inline to avoid an import cycle). It is already consumed by the daemon's own session namer: `DefaultSessionName(projectDir) = "harmonik-" + tmuxStartHashDir(projectDir) + "-default"` (:168). The window-level equivalent is `windowNameSentinelPrefix` (`internal/lifecycle/tmux/windowname.go:63`) producing the `hk-<hash6>-` sentinel (WM-002a, first 6 hex chars). **The portable fix REUSES `tmuxStartHashDir` / `ComputeProjectHash`** to qualify crew/supervisor/keeper/captain names: e.g. `hk-<hash6>-crew-<name>`, `hk-<hash6>-daemon-supervise`, `hk-<hash6>-keeper-<role>` — no new hashing scheme.

### F-C2.5 — Spec already mandates project-hash namespacing for daemon sessions, but does NOT cover the launch-layer sessions (RQ-C2.5)

`specs/process-lifecycle.md` is the owner. PL-006a (:278-291) defines the project hash + provenance marker and mandates: "Scope tmux session names (`harmonik-<project_hash>-<session_name>`)" (:282). PL-006 (:263) requires the orphan sweep to enumerate the `harmonik-<project-hash>-` prefix. PL-019 (f) (:555) names the supervisor's flywheel session `harmonik-<project_hash>-flywheel`. PL-021b (:601) and WM-002a (:603) define the `hk-<hash6>-` window sentinel for the $TMUX-reuse mode.

**Gap:** the spec covers `-default` / `-flywheel` / window sentinels — i.e. the **daemon-owned and supervisor-owned** sessions — but does NOT cover the **crew / keeper / captain / supervise** launch-layer sessions (those are created by tmuxsubstrate.go crew-spawn, the keeper, and captain-tools, outside the spec'd namespace). C2's spec change is to **extend PL-006a's session-naming contract** to require ALL fleet sessions (crew/keeper/captain/supervise) carry the `harmonik-<project_hash>-` (or `hk-<hash6>-`) prefix — bringing them into the orphan-sweep namespace AND the coexistence guarantee. This is a clean extension of an existing, already-normative pattern, not a new abstraction.

**Consistency hazard to flag:** PL-006 kills every session matching `harmonik-<project_hash>-` that lacks a supervisor sentinel (PL-006d exclusion). If crew/keeper/captain sessions are brought into that prefix, the orphan sweep would kill them on daemon restart unless they ALSO get a PL-006d-style live-owner sentinel (or an explicit exclusion). The design MUST address this: project-qualifying the names is necessary for coexistence but interacts with the sweep's "kill everything in the namespace" rule. (See PL-006d, :322-331, and the PL-021c window-orphan extension, :614.)

## Patterns to follow

- Reuse `tmuxStartHashDir` / `ComputeProjectHash` (PL-006a) — single hashing scheme.
- Extend, don't replace, the PL-006a `harmonik-<project_hash>-<session_name>` contract to cover crew/keeper/captain/supervise.
- Mirror the PL-006d sentinel-exclusion discipline so qualified-but-live launch sessions survive the orphan sweep.
- For skills, adopt a `$HARMONIK_PROJECT` (or repo-root) resolution convention that preserves 7b84264d's disambiguation intent ("project-local, NOT global ~/.claude/skills/") without a literal path.

## Risks / conflicts (flag for design)

1. **Orphan-sweep kill-the-namespace vs newly-qualified live sessions.** Bringing crew/keeper/captain into the `harmonik-<project_hash>-` prefix exposes them to PL-006's sweep. Needs a sentinel/exclusion (PL-006d analog) so a daemon restart doesn't kill live crew. **Load-bearing.**
2. **Crew name is a PUBLISHED contract.** `hk-crew-<name>` is referenced by skills, comms identities, and the captain's spawn/log tooling. Re-namespacing to `hk-<hash6>-crew-<name>` is a contract change — route through the review gate, and update every consumer (captain-tools crewlog.sh resolves crew sessions by name, see C3).
3. **Captain session has no Go owner.** Its name lives in captain-tools (C3), so C2's session-qualification for the captain is really a C3 (tooling) change — coordinate the two components on the captain session.
4. **`$HARMONIK_PROJECT` must be reliably set.** The skills' new convention depends on the env var being present in every fleet session (daemon, captain, crew, keeper). Verify the launch paths export it; if not, the keeper-enable hooks already inject `HARMONIK_PROJECT=<path>` (C3) — reuse that injection point.
