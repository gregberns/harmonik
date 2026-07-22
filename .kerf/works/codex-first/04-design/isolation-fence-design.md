# 04 — Change Design: isolation-fence (component A / D1)

> Pass 4. See also the overview in `code-seams-design.md` §A and research
> `03-research/isolation-fence/findings.md`.

## Current state
`codexdriver` selection forces `requireIsolationBoundary=true` and a
`codexWorkerRoutingRunner{requireBoundary:true}` (`substrate_select.go:74`); with no ssh worker
bound the runner emits `refusedIsolationBoundaryArgv0` (`substrate_select.go:123`); `beadRunOne`
independently refuses via `deps.codexRequireIsolationBoundary` (`workloop.go:3626`), fed from
`Config.CodexRequireIsolationBoundary` (`daemon.go:586`, set `main.go:1357`, `run.go:715`).

## Target state
`requireIsolationBoundary=false` and `requireBoundary:false` → a nil/disabled/non-ssh registry
state falls through to `LocalRunner`. Remove the `refusedIsolationBoundaryArgv0` diagnostic + doc
block (dead). Remove the `workloop.go:3626` guard block and the now-always-false
`codexRequireIsolationBoundary` plumbing (Config field, `workloop.go:759,1198`, both call sites).
Leave `codexWorkerRoutingRunner`/SSHRunner inert, marked deprecated (P3 removes it).

## Rationale
D1: the fence was an agent-invented rule (2026-07-18), not an operator mandate; danger-full-access
codex is the same host posture as the unsandboxed Claude the operator runs daily. Research F2/F4.

## Requirements traceability
02 area A "must be true" → this target state. Normative home: HN-025.
