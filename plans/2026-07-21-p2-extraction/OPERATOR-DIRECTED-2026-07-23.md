# P2 incoming work — operator-directed 2026-07-23

Three items the operator routed INTO the P2 plan on 2026-07-23. Each is to be
**developed and reviewed by dedicated agents** — not hand-fixed inline by the
orchestrator. Beads are machine-local, so this tracked doc is the durable record.

> **How to work these (operator directive, 2026-07-23):** parallelize as much as
> possible and structure the work with **ultrawork** (multi-agent workflows /
> fan-out) wherever the task decomposes. These three are largely independent of
> each other and of the RT extraction stream — run them concurrently, not serially.

## 1. Agent-run validation command (replaces git hooks) — `hk-p58vo`

**The deliverable (operator, clarified 2026-07-23):** a concrete **agent command** —
a slash-command file under **`.claude/commands/`** — that runs **after every
`git commit`** (or thereabouts) and executes a **`make` target** to verify the code
the agent *just committed* actually passes our checks. If it comes back red, the agent
fixes and re-commits. This is **agent-driven**, not a blocking shell hook.

- **Which make target — orchestrator's pick:** `make check` (the full tier-2 gate:
  build / vet / lint + `-race` tests + coverage + govulncheck), because the stated
  intent is "all our checks." If a full check after *every* commit proves too slow,
  the implementing agent may split it: `make check-fast` per commit, `make check` at
  push / milestone boundaries.
- **Open design detail — the "after every commit" trigger.** e.g. a Claude Code
  `PostToolUse` hook in `settings.json` matching `git commit` that injects a message
  telling the agent to run the slash command. Wire it so it fires reliably without a
  git hook.
- **git hooks are OUT.** lefthook was fully uninstalled this session ("fuck lefthook,
  that shit is awful"); it auto-reinstalled itself on every commit, which is why it's
  gone entirely rather than trimmed. Do NOT reinstall it.

Design → build → review, all by agents, in P2.

## 2. Pre-commit hook wiring is broken — `hk-e1ant`

Do NOT hand-fix. Likely **superseded** by item 1, but the root causes are recorded
so the redesign learns from them:

- lefthook runs pre-commit commands in **parallel / alphabetical** order, so `fmt`
  (gci write) races `check-fast` (gci diff) → check-fast false-fails in ~2s.
- `make agent-review` + `check-verdict.sh` diff **`HEAD~1`**, which at pre-commit
  time bundles the *previous* commit with the pending change → always flags
  scope-creep.
- Never exercised before because the hooks were uninstalled (`hk-69xhf`, the day's
  "guard that is green because it never ran"). The orchestrator re-installed them,
  hit these bugs, and **backed the pre-commit/pre-push hooks back out** (kept the
  working `commit-msg` trailer hook) pending this redesign.

## 3. `Close()` errcheck coverage — MAXIMIZE — `hk-8dtiv`

Operator decision: **maximize coverage**, but implement + review via agents in P2,
not inline. Converged recommendation from a 3-agent fan-out (config / bug-surface /
idiom lenses):

- **Config:** `.golangci.yml` — drop the four `Close` entries from errcheck
  `exclude-functions` (keep `check-blank: true`), add ONE `_test.go`-scoped errcheck
  exclusion. Re-arms mechanical coverage on ~95–99 production `Close` sites; test
  noise stays zero.
- **Migration:** convert those ~99 sites to the house idiom
  `if closeErr := x.Close(); closeErr != nil { … }` (NOT `_ =`, NOT `//nolint` —
  `check-blank` rejects blank and the no-new-nolint rule stands). ~half the churn is
  `cmd/harmonik` (26) + `internal/lifecycle` (20); **15 are in `internal/daemon`**
  and must sequence with the live extraction.
- **Order:** land the config flip **last** so the gate stays green.
- **Byproduct:** catches 3 real minor latent bugs — `sessiondata.Append` lost-flush,
  `handlerpause` dir-fsync dropped, `supervise` crash-log close.
