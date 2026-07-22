# Research — Component C: the daemon-fallback commit path (confirmation)

> Source (cite): `plans/2026-07-20-codex-strategy-realignment/research/05-...md` findings #1–#3;
> DECISIONS §D2-direction (findings valid, recommendation superseded by D3); `_plan.md` §3c.

## Questions
1. Who is the designed committer in the DOT flow — daemon or agent?
2. Is the diff guaranteed to land regardless of whether codex self-commits?
3. Does anything change here, or is this a confirmation only?

## Findings (with evidence — research/05)
- **F1 — per-node model, agent is the *designed* committer** (finding #1): the implement-node seed
  prompt instructs codex to `git commit` with a `Refs:<bead>` trailer
  (`codexlaunchspec.go:68`, `agentseedprompt.go:37`); the daemon requires HEAD to advance after
  each implement node (`dot_cascade.go:1970,1979`) — commits are per-node.
- **F2 — deterministic daemon fallback = de-facto committer today** (finding #2):
  `ensureCodexRefsTrailer` (`codexcommit.go:204`), invoked per implement-node exit
  (`dot_cascade.go:1938`), stages+commits codex's edits **outside** any sandbox and adds the
  `Refs:<bead>` trailer. Designed as a backstop (`codexcommit.go:36-40`), but because deployed
  codex self-commit failed 100% under the seatbelt it has been the primary committer (proven: run
  `019f7bc5`, HEAD `123b2ae` "auto-committed by daemon fallback", `LEG-B.md:135-137`). Fires for
  `CompletionProcessExit` harnesses (codex/pi); no-op for claude.
- **F3 — no code change.** Under `danger-full-access` codex CAN self-commit; the fallback still
  runs as backstop and **idempotently no-ops** when HEAD already carries the trailer. **Either
  committer lands the diff** — acceptance (§5 crit. 2) accepts either.

## Risks / conflicts
- None. This component only records that the plan's success does NOT depend on codex self-commit;
  it is exculpatory evidence, not a change.
