# Research — C3. Multi-tenant settings & global tooling

> Pass 3 (`research`) of `fleet-portability`. Component C3 per `02-components.md`. Verified
> against the live tree on 2026-06-13 (5-agent assessment + independent code-verification pass).

## Research questions

- RQ-C3.1 — Does `harmonik keeper enable` write per-project-safe hook stanzas to the single global `~/.claude/settings.json`, or do two projects collide?
- RQ-C3.2 — Are the captain-tools scripts (`captain-launch.sh`, `crewlog.sh`) versioned in the repo, and what depends on them?
- RQ-C3.3 — Which `/tmp` globals are NOT project-qualified, and which collide when two projects run fleets on one machine?
- RQ-C3.4 — What does the spec corpus say about multi-project / single-machine isolation, so C3's invariant builds on existing intent?

## Findings

### F-C3.1 — keeper enable writes 3 hooks to the GLOBAL settings file; second project's enable overwrites the first (RQ-C3.1) — CONFIRMED, with a precise collision mechanism

`cmd/harmonik/keeper_enable_doctor_cmd.go:126` — `settingsPath := filepath.Join(home, ".claude", "settings.json")` (the user-global file). It writes **3** stanzas (:203-216): a statusline command + a Stop hook + a PreCompact hook, each of the form `HARMONIK_PROJECT=<cfg.projectDir> <scriptsDir>/keeper-*.sh`.

**Important correction:** `HARMONIK_PROJECT` is **NOT hardcoded to a literal** — it's `cfg.projectDir` from the `--project` flag/CWD (:203-211). So the *value* is per-project-correct. The collision is in the **merge logic**: the idempotent merge keys on the **script basename** (`mergeStatusLineStanza` / `mergeHookStanza`, :214-216 — match by `keeper-stop-hook.sh` etc. in the command string). The global settings file has **no per-project namespacing key** (no `hooks.Stop["project-A"]` vs `["project-B"]`). Consequence:

- Project A `keeper enable` -> 3 stanzas, `HARMONIK_PROJECT=/path/A`.
- Project B `keeper enable` -> SAME 3 stanzas matched by basename, REWRITTEN to `HARMONIK_PROJECT=/path/B`.
- Result: only project B's keeper fires; project A's keeper silently receives `HARMONIK_PROJECT=/path/B` and operates on the wrong project. **P0 for coexistence.**

The fix lets N per-project `HARMONIK_PROJECT` hooks coexist in the one global file — e.g. a project-keyed stanza array, or a per-project wrapper the global hook dispatches to. (Note: the harness's hook model may itself constrain this — design must check whether Claude Code supports multiple Stop hooks; if not, a single dispatcher hook that fans out by some per-session project marker is the fallback.)

### F-C3.2 — captain-tools scripts are UNVERSIONED user-global; the keeper respawn + captain skill depend on them (RQ-C3.2) — CONFIRMED

`~/.claude/captain-tools/` holds `captain-launch.sh` (3278 B) and `crewlog.sh` (1834 B). They are NOT in the repo (`find /Users/gb/github/harmonik -name captain-launch.sh -o -name crewlog.sh` -> nothing). Yet the repo DEPENDS on them:

- `cmd/harmonik/keeper_cmd.go:286` documents `--respawn-cmd '~/.claude/captain-tools/captain-launch.sh'`.
- `.claude/skills/captain/SKILL.md:571` — "launched via `~/.claude/captain-tools/captain-launch.sh`, which mints that id."
- `.claude/skills/captain/STARTUP.md:261` — boot shows `~/.claude/captain-tools/captain-launch.sh captain`.
- `captain-tools/crewlog.sh` is the captain's crew-status polling tool (MEMORY: "Captain crew-status polling protocol").

So `captain-launch.sh` is a **load-bearing launch-layer dependency** (it mints the crew/captain session ids and is the `--respawn-cmd` the keeper uses to restart a context-filled captain) that exists nowhere in version control. A fresh machine / foreign project has neither script. The fix versions them in-repo (and `init`/C1 provisions them, or they're embedded + written out). NOTE: `captain-launch.sh` is also where the captain *session name* is minted — so C2's captain-session project-qualification lands HERE, in this script, not in Go.

### F-C3.3 — `/tmp` globals collide; the supervise script is the most acute (RQ-C3.3) — CONFIRMED, with a NEW finding

- **`/tmp/hk-last-good-binary`** — `internal/release/lastgood.go:28` returns the literal path, NOT project-qualified. Two daemons' release supervisors race on binary pin/restore. (P2)
- **`/tmp/hk-daemon.log`** — `scripts/hk-keeper.sh:44` default `LOG="${HK_LOG:-/tmp/hk-daemon.log}"`. Two daemons interleave logs. Mitigated only if the operator sets `HK_LOG` per project. (P2)
- **`/tmp/hk-daemon-supervise.sh` [NEW, P1]** — this ephemeral, *actively-running* supervise script has `PROJECT=/Users/gb/github/harmonik` and `BIN=/Users/gb/go/bin/harmonik` HARDCODED (lines 12-13; it is a "RECONSTRUCTED 2026-06-08 recovery artifact" — original deleted from /tmp while its bash loop kept running). It is a **live multi-tenant failure vector**: a second project's supervise startup would overwrite it (same /tmp path), and the running supervisor loop would then launch daemons for the WRONG project. More acute than a config nit because the script is a persistent process, not just config. (MEMORY corroborates: "auto-revive at /tmp/hk-daemon-supervise.sh".)

Test-only `/tmp/harmonik/*` paths exist but use distinct test-fixture naming and are not a production collision risk.

### F-C3.4 — Spec corpus already asserts multi-project intent (RQ-C3.4)

The spec already takes a clear position that the same machine runs multiple projects and the same binary serves them: `process-lifecycle.md:265` (orphan-sweep) — "binary-path matching is insufficient on multi-project machines where the same handler binary serves multiple projects" — and PL-006a's whole provenance-marker design exists precisely to disambiguate per project. So C3's invariant ("harmonik's contributions to shared *global* surfaces MUST be project-namespaced so N fleets coexist") is a natural **extension of an already-stated principle** from the per-project *file/process* surface to the *global Claude-harness + /tmp* surface. There is no spec section today that governs the global `~/.claude/` surface or `/tmp` globals — that section is **new** (likely an operator-NFR or process-lifecycle addition; `operator-nfr.md` is the candidate home, given it owns ON-series operability requirements).

## Patterns to follow

- Mirror PL-006a's provenance-marker / project-hash discipline when namespacing global state — same `hash6` as C2.
- Where the harness limits per-project hooks (single Stop hook), use a **dispatcher** pattern (one global hook fans out by a per-session project marker) rather than N raw stanzas.
- Version + provision (C1) the captain-tools scripts; `init` is the natural provisioning site.
- Project-qualify every `/tmp` global (`/tmp/hk-<hash6>-...`) or move it under the project's own `.harmonik/` (already-isolated) surface.

## Risks / conflicts (flag for design)

1. **Harness hook model may not allow N coexisting Stop hooks.** If Claude Code merges/overwrites by hook-type, the per-project-stanza approach won't work — needs the dispatcher fallback. **Verify the harness contract before designing the settings.json fix.**
2. **Global settings.json is shared with the operator and with non-harmonik tooling.** C3 must namespace harmonik's stanzas WITHOUT clobbering the operator's own hooks — the merge logic must be additive and project-scoped, never a wholesale rewrite.
3. **captain-launch.sh mints session ids AND names** — versioning it intersects C2 (captain session qualification) and the Captain&Crew published contract (crew/captain session-id minting). Coordinate C2+C3 on this script; changes to the minting are review-gated.
4. **`/tmp/hk-daemon-supervise.sh` is generated/reconstructed at runtime, not checked in.** The fix must change the GENERATOR (wherever supervise writes it) to emit a project-qualified path + non-hardcoded PROJECT, not just edit the live artifact. Find the generator (likely `harmonik supervise` per process-lifecycle.md PL-019).
