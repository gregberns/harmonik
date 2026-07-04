# 06 — Integration

How the components compose and land in the existing system.

## Build / land order (DIRECTION build order)
1. **C-A** type-folder + manifest schema + Go loader (`internal/agentmanifest/`).
2. **C-B + C-F** the `harmonik agent brief` command + boot-document ORDER (emit-only, no side effects).
3. **C-G(crew)** author the `crew` type folder end-to-end; prove `brief` produces a working crew boot
   doc. This is the proving ground (DIRECTION B1).
4. **C-C** `harmonik agent check` schema-check verb.
5. **C-D** harmonik-supplied scoped skills + shared folder + disable global autoload.
6. **C-G(admiral, watch)** + **C-H** wire `harmonik start captain` to the captain folder.
7. **C-I** declare triggers on crew/admiral manifests; daemon-side new-source delivery as follow-on.

## Seams touched
- `cmd/harmonik/` — new `agent` verb group (`brief`, `check`), sibling to `start.go`/`crew.go`.
  `start.go:81` captain case stays; `captain.go runCaptainLaunchWithOps:315` gains the symmetric
  seed pointing captain at `agent brief` (mirrors crew paste `crewstart.go:502`).
- `internal/agentmanifest/` (new) — loader + validator, reused by brief/check.
- `.harmonik/agents/` (new tree) — type folders + `_skills/` shared dir.
- `.claude/settings.json` — disable global skill autoload (embedded-asset re-sync gotcha applies).
- Keeper `cycle.go` — the KEEPER-IDENTITY-into-handoff append (:456/:1016) is superseded for boot
  identity by `brief` re-reading `soul.md`; the keeper restart path re-runs the one boot skill (C-E)
  so I1 holds. (Exact keeper change: keeper restart triggers `agent brief` rather than relying on the
  appended block; scoped as part of C-H/keeper wiring.)

## Provenance guarantee (I1) end-to-end
Fresh start AND keeper `/clear` both funnel through the one boot skill -> `agent brief` -> SOUL from
`soul.md`, handoff last + episodic-only. No path re-seeds identity from a prior session's handoff.

## Non-regression
- `harmonik start captain` / `harmonik crew start <name>` remain working verbs (content source changes,
  not the CLI surface).
- `$HARMONIK_AGENT` continues to be set on every agent process (unchanged); `brief` consumes it.
- Deferred publish seam is untouched; triggers whose target is fleet-state just tell the agent to
  post to comms / write handoff for now.
