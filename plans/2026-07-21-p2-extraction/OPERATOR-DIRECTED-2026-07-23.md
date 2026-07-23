# P2 incoming work — operator-directed 2026-07-23

Three items the operator routed INTO the P2 plan on 2026-07-23. Each is to be
**developed and reviewed by dedicated agents** — not hand-fixed inline by the
orchestrator. Beads are machine-local, so this tracked doc is the durable record.

## 1. Agent-run validation command (replaces fragile git pre-commit hooks) — `hk-p58vo`

**Operator's direction, verbatim intent:** git pre-commit hooks "suck" — installing
them today surfaced real wiring bugs (see item 2) and a per-commit cost far over
tolerance. Instead, build the model that worked well in another project:

- ONE CLI command (under `.claude/commands/`) that the **agent** runs to validate
  everything (build / vet / lint / tests / review).
- A hook **injects a message to the agent** telling it to run that process.
  Operator is unsure whether the injection should fire **before or after** the
  commit — that is an open design question for the build.
- The point: **agent-driven** validation, not a blocking shell hook.

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
