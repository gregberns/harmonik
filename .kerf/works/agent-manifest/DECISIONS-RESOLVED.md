# Decisions resolved (orchestrator, 2026-07-03)

The kerf-spec agent surfaced 4 `DECISIONS-NEEDED`. All four are implementation/runtime-layout
details, not operator-level product forks — resolved inline (adopting the recommended option) per
the "don't queue runtime-resolvable detail" rule. None met the bar for operator convergence.

1. **Shared-skills dir location → `.harmonik/agents/_skills/`.** Harmonik-supplied skills must be
   harness-agnostic (used by codex/pi too), so they belong under `.harmonik/agents/`, NOT the
   claude-only `.claude/skills/`. A type's `manifest.context` refs point here for shared skills;
   type-private skills may live in the type folder.

2. **Wake-reason source → `--wake` flag on `harmonik agent brief`, default `fresh`.** Explicit flag
   the keeper/launcher sets (`fresh` | `keeper-restart` | `trigger:<id>`); populates boot-doc
   section 2. A flag is greppable and testable; env is the fallback if a flag can't be threaded.

3. **Trigger activity-guard → guard on recent fleet activity.** Scheduled triggers (e.g. admiral's
   6h report) fire only if the fleet has been operating in the window — matches the manifest intent
   ("only while the system is/has been operating") and avoids waking idle agents. The window is a
   config value, not hardcoded.

4. **Instance→type resolution → extend `crew.Record` with a `type` field** (legacy/default `crew`).
   Reuses the existing atomic file-backed registry (`internal/crew/registry.go:38`); no parallel
   index to keep in sync. Back-compat: a record with no `type` reads as `crew`.

Command name (operator delegated to a fresh agent): **`harmonik agent brief`** (emit boot document)
+ **`harmonik agent check`** (schema-check verb). Kept.
