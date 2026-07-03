# 04 — Research findings (Route 1)

Most engine research was completed during Analyze (see `02-analysis.md`). This
consolidates the verified facts that drive the spec, plus the `cm` CLI surface.

## Engine facts (verified against code)

- Shell tool node runs `tool_command` verbatim via `/bin/sh -c` (local,
  `dot_cascade.go:1907`) or `/bin/sh -lc 'export …; cd <wt> && <cmd>'` (remote,
  ~1933), with `cmd.Dir = wtPath` and `cmd.Env = append(os.Environ(), env...)`
  (1911). **No `${...}` interpolation** — bead data must be env vars.
- Tool-node env today = `deps.handlerEnv` (+ optional `HK_GATE_BASE_SHA`),
  project-level only (`dot_cascade.go:436–440`). The fix seam (C1) is here;
  `beadID/beadTitle/beadDescription` are in scope from `driveDotWorkflow`
  (171–178).
- Per-node `role` is prepended into the agent's ExtraContext → `.harmonik/
  agent-task.md` (`dot_cascade.go:1212–1223`, `claudelaunchspec.go:324–329`).
  This is how the implement node tells the agent to read the lessons file. No
  engine change.
- **Gap-2 correction (from Route 2 investigation):** the raw
  `core.ApplyContextUpdates` DOES run on the live path
  (`edgecascade.go:100` via `DecideNextNode`→`SelectNextEdge`, reached at
  `dot_cascade.go:1035`). The real Route-2 gaps are (a) no handler ever
  *produces* `Outcome.ContextUpdates` (shell nodes return Status+Notes only) and
  (b) the validated/observable `ValidateAndApplyContextUpdates`
  (`context_updates.go:62`) is dead code. **Implication for Route 1:** since no
  node emits structured context today, the worktree-file hand-off is the correct
  pragmatic channel — Route 1 does not touch this machinery. Durable structured
  path is captured in `docs/ideas/node-context-passing.md`.

## `cm` CLI surface (verified on this machine, cm 0.2.12)

- Query: `cm context "<task text>" --json` → JSON at `data.relevantBullets[]`
  (each `{id, content, type, maturity, finalScore, …}`) and `data.antiPatterns[]`,
  plus `data.semanticMode` (`semantic` when neural is on — it is, on this host).
- Bounding: `--limit N` caps bullets; `minRelevanceScore` filters weak matches.
- Merges global `~/.cass-memory/playbook.yaml` + repo `.cass/playbook.yaml` when
  run with `cwd` inside the repo — the tool node runs in the worktree, so
  repo-level rules apply automatically.
- Local neural embeddings (no API spend) for the read path; safe to call per
  dispatch. Missing/erroring cm → handle via `|| true` (best-effort).
- For C3 shaping: a minimal json→markdown is e.g. iterate `data.relevantBullets`
  → `- [<type>] <content>`. Avoid heavy deps; `python3 -c` (present on host) or a
  small `jq` filter both work — design-pass picks one and documents the host
  assumption.

## Open design-pass picks (small, runtime-tunable)

- Exact env var names (`HK_BEAD_*` proposed).
- `--limit` value for the example (5 proposed) and timeout (~20s proposed).
- json→markdown shaping mechanism (python3 vs jq) + where the formatter lives.
- Lessons filename (`.harmonik/cm-lessons.md` proposed).

These are tunable and do not require user sign-off; they get fixed in the spec.
