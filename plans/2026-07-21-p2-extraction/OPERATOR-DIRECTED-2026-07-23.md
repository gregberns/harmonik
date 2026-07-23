# P2 incoming work — operator-directed 2026-07-23

Three items the operator routed INTO the P2 plan on 2026-07-23. Each is to be
**developed and reviewed by dedicated agents** — not hand-fixed inline by the
orchestrator. Beads are machine-local, so this tracked doc is the durable record.

> ## STATUS — updated 2026-07-23 (session `dd3b5e34`)
>
> - **Item 1 `hk-p58vo` — DONE** (commit `96f0273c`, reviewed APPROVE). Interim
>   simple form delivered: `.claude/commands/check.md` (`/check` runs `make check`
>   post-commit, fix + re-commit if red) + a standing reminder in `AGENTS.md`. The
>   richer auto-trigger (a `PostToolUse` hook that injects the reminder on `git
>   commit`) is intentionally DEFERRED as the "later evolution" the operator named.
> - **Item 2 `hk-e1ant` — DONE** (same commit `96f0273c`, reviewed APPROVE).
>   lefthook fully retired: `lefthook.yml` + `scripts/check-hooks.sh` deleted;
>   `install-hooks`/`check-hooks` Makefile targets + the CI `hooks` job removed;
>   `bootstrap` == `make tools`; `validate-commit-msg.sh` + `secret-scan.sh`
>   preserved for the agent-driven flow; docs reconciled. Both beads CLOSED.
> - **Item 3 `hk-8dtiv` — RECON DONE, migration IN PROGRESS.** Full linter-verified
>   plan at [`hk-8dtiv-close-errcheck-recon.md`](hk-8dtiv-close-errcheck-recon.md):
>   **99** production Close sites newly fire (17 daemon = deferred with the
>   extraction; **82 non-daemon = this batch**, 16 packages); `_test.go` exclusion
>   keeps test noise at 0. Bug 1 (sessiondata lost-flush) + Bug 2 (handlerpause
>   dropped dir-fsync) confirmed real; Bug 3 is idiom-only. 7-commit batching,
>   bug-fixes first, **config flip LAST** — and the flip is gated on the 15
>   remaining daemon sites, which stay sequenced with the `internal/daemon`
>   extraction lane (Bug 2's fix is pulled forward as a standalone bugfix).

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
