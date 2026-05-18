# Design pointer — settings-env

This file is a thin pointer. The settings/env research thread's conclusions
were folded directly into the consolidated master design.

- **Source research:** `../03-research/settings-env/findings.md`
  (`.claude/settings.json` schema, hook entry shape, settings precedence
  hierarchy including `settings.local.json` shadowing, env-var inheritance
  semantics from parent process to Claude to hooks).

- **Where it landed in the master design** (`claude-hook-bridge-design.md`):
  - §D2 (env-var schema — final)
  - §D8 (pre-existing user settings.json collision and merge rule)
  - §D13 (handler materializes settings.json; relay reads env)
  - Settings-precedence shadow check (settings.local.json) per §D8 fallout
