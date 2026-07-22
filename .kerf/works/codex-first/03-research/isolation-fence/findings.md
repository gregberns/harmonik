# Research — Component A: the fail-closed isolation fence (D1)

> Source: `plans/2026-07-20-codex-strategy-realignment/DECISIONS.md` §D1/§D4;
> `plans/2026-07-21-codex-first/_plan.md` §3a; fresh tree read on `phase1-session-restart-substrate`.
> Not re-derived here — pointing to the seams already named.

## Questions
1. Where is the fence enforced, and off what selection is it keyed?
2. What is the origin/authority of the rule?
3. What must the LocalRunner fallback do once the fence is dropped?
4. What becomes dead code, and what should stay inert vs. be removed?

## Findings (with evidence)
- **F1 — three enforcement points, all keyed off the `codexdriver` substrate selection** (_plan
  §3a): (1) `cmd/harmonik/substrate_select.go:74` returns `requireIsolationBoundary=true` and
  builds `codexWorkerRoutingRunner{requireBoundary:true}`; (2) `codexWorkerRoutingRunner.Command`
  + `refusedIsolationBoundaryArgv0` (`substrate_select.go:123`) emit a refused-argv0 binary when
  no ssh worker is bound; (3) `internal/daemon/workloop.go:3626` refuses in `beadRunOne` off
  `deps.codexRequireIsolationBoundary`, fed by `Config.CodexRequireIsolationBoundary`
  (`daemon.go:586`) set at `main.go:1357` and `run.go:715` from `codexRequireBoundary`.
- **F2 — origin: agent-invented, now revoked.** D1: the rule was an *admiral-agent* ruling
  (2026-07-18), not an operator mandate; `danger-full-access` codex is the same host posture as
  the unsandboxed Claude the operator runs daily, so a codex-only fence is inconsistent. D1
  removes it. (Operator: nominal approval was a rubber-stamp of a too-convoluted write-up, now
  corrected.)
- **F3 — fallback behavior:** with `requireBoundary=false`, a nil/disabled/non-ssh registry state
  falls through to `LocalRunner` instead of the refused-argv0 binary. Net:
  `HARMONIK_SUBSTRATE=codexdriver` with no worker → codex runs on `LocalRunner`, on the daemon
  host, like Claude.
- **F4 — disposition:** the refused-argv0 diagnostic + its doc block become dead → delete. The
  `codexRequireIsolationBoundary` plumbing (Config field, `workloop.go:759,1198`, the two
  composition-root call sites) becomes always-false → remove the guard block + plumbing. The
  `codexWorkerRoutingRunner`/SSHRunner routing seam stays **inert, marked deprecated** — D4 scraps
  ssh but its final extract-or-delete is **P3** (platform-architecture); leaving it keeps the diff
  small (operator-confirmed, _plan §7.2).

## Risks / conflicts
- Removing plumbing touches multiple call sites (`main.go`, `run.go`, `daemon.go`, `workloop.go`)
  — mechanical but must leave `go build`/`go vet` green (acceptance 7). No design conflict.
