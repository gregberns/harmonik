<!-- PP-TRIAL:v2 2026-05-26 main — v60 (commit 7367249). Clean. 8 beads landed: 3 daemon friction fixes (liveness check, bracketed-paste, stream HOL), 2 spec-corpus (Role permission schema, deferred-role shells), failure-class classifier, NodeType cleanup, launch-verification heartbeat. Two systemic issues remain: ~60% empty-pane rate on concurrent dispatch, ~80% implementer-exits-without-committing rate. -->

Roadmap: [ROADMAP.md](ROADMAP.md). Cross-project working-style rules: `~/.claude/CLAUDE.md`. Plans index: [plans/README.md](plans/README.md).

**Orchestrator rules (permanent directives): [docs/orchestrator-rules.md](docs/orchestrator-rules.md). Known workarounds: [docs/known-workarounds.md](docs/known-workarounds.md).**

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read on `/session-resume`. Append new entries when you observe friction. Promote durable rules to `docs/orchestrator-rules.md` or `.claude/implementer-protocol.md`.

# Where we are (v60, 2026-05-26)

**Main at `7367249`** (origin parity, working tree clean). 8 beads landed this session.

## What v60 landed

8 commits on main:

| Commit | Bead | Description |
|--------|------|-------------|
| `7c70921` | hk-ex9c4 | Failure-class classifier (T-IMPL-006) |
| `90b6037` | hk-3xknp | Remove NodeTypeControlPoint (WG-001) |
| `08903dd` | hk-3gq0b | Launch-verification heartbeat window |
| `da89ce4` | hk-fbydv | Pane liveness check (pgrep-based, prevents killing active thinking sessions) |
| `01d5aca` | hk-a8bg.28 | Role permission_schema presence (CP-028) |
| `81921b4` | hk-8cq23 | Post-paste SendEnterToLastPane (bracketed-paste race fix) |
| `b81a76b` | hk-9a27q | Stream HOL blocking fix (streamEligible skips dispatched) |
| `7367249` | hk-a8bg.30 | Deferred roles carry empty shells (CP-030) |

Plus closed 5 stale-open beads from v59 (hk-jon6r, hk-pphof, hk-8uy6m, hk-b0cyc, hk-ortkx).

## TWO SYSTEMIC ISSUES remain

### 1. ~60% empty-pane rate on concurrent dispatch
Paste delivered to tmux pane but claude never starts processing. Bracketed-paste fix (hk-8cq23) didn't fully resolve it. Sub-agent investigation found a splash-dismiss timing race but the fix didn't eliminate it. Needs deeper investigation — may be a Claude Max concurrent session limit or a tmux pane lifecycle issue.

### 2. ~80% implementer-exits-without-committing rate
Implementers read the bead, run `br close`, and exit without producing code. Happens consistently across multiple batches and different beads. Possible causes: (a) bead descriptions too terse (title-only, no implementation guidance), (b) implementer protocol not being followed, (c) beads are too complex for a single-shot implementer.

## Next-session intent

1. **Investigate the two systemic issues above** — dispatch sub-agents, don't do it inline.
2. **Continue spec-corpus implementation** — hk-a8bg.29 (role default permissions), hk-a8bg.70 (DelegationPath), hk-hqwn.37 (event schema_version), hk-a8bg.31 (Beads-CLI default skill).
3. **DOT impl chain** — hk-7okmx (T-IMPL-003 loader) is unblocked now that validator landed.
4. **Friction beads still open** — hk-rnsjs (claim-failure auto-close), hk-24xn1 (daemon wake-on-submit), hk-aq17j (runCtx refactor).

## Files to open first

1. `HANDOFF.md` (this)
2. `internal/daemon/pasteinject.go` — liveness check + bracketed-paste fix landed here
3. `internal/queue/state.go` — stream HOL fix landed here

## Plain-English glossary

- **hk-fbydv** — pane liveness check: daemon uses `pgrep` to distinguish "claude thinking" from "empty pane" before killing
- **hk-8cq23** — bracketed-paste fix: sends Enter after paste to ensure text submission
- **hk-9a27q** — stream HOL fix: `streamEligible()` no longer blocks on dispatched items
- **hk-a8bg.28/30** — control-points spec corpus: role permission schema + deferred-role shells
- **empty-pane** — tmux pane has prompt text but claude never started processing (~60% of concurrent sessions)
- **close-without-impl** — implementer runs `br close` then exits without committing code (~80% of sessions)
- **`--wave`** — queue mode that allows concurrent dispatch; use instead of stream-default when `--max-concurrent > 1`

## No hard blockers requiring user input.
