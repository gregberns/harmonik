---
schema_version: 1
crew_name: lima
queue: lima-q
epic_id: hk-8juwz
goal: "Dispatch health: make claude:local bead dispatch actually reach agent_ready and produce a commit"
captain_name: captain
---

# Mission: Dispatch health (P1) — make claude:local dispatch reach agent_ready and commit

You are crew member **lima**, owning the dispatch-health lane (epic **hk-8juwz**) on queue **lima-q**. Report status to **captain**.

**Why this matters:** the theme/onboarding-modal wedge is **eating dispatched beads** — a `claude:local` worker parks on the v2.1.214 theme modal, `agent_ready` never fires, and the bead dies with turn_count=0. The prior "fix" (commit d13ae1cf / hk-oga33) is a **FALSE CLOSE** — the assessor confirmed the wedge live @33dad0fd. Until this is genuinely fixed, daemon dispatch cannot be trusted to produce commits at all. This lane restores dispatch to health and proves it.

## The subsystem you touch
The daemon's **claude launch spec** and its onboarding/config-dir isolation. READ FIRST:
- `br show hk-8juwz` in full — the two comments carry the mechanism history: shared-global `~/.claude.json` lost-update race, the refuted theme-seed, and the CANDIDATE `CLAUDE_CONFIG_DIR` isolation fix (a964cbcb, UNIT-VERIFIED ONLY, never live-proven).
- `internal/daemon/claudelaunchspec_configdir_hk8juwz_test.go` — the known lever: an **isolated `CLAUDE_CONFIG_DIR`** per worktree, seeded from the operator's onboarded `~/.claude.json`, removes the shared-global fleet race by construction.
- The claude launch-spec builder + agent-ready seed path under `internal/daemon` and `cmd/harmonik`.

## What "done" means (the shape, not a checklist)
A `claude:local` bead dispatch actually works end-to-end:
- the worker **does not wedge on the theme/onboarding modal** — isolated onboarding state (private per-worktree `CLAUDE_CONFIG_DIR`) is applied so the modal never parks the pane;
- **`agent_ready` fires** on a real `claude:local` run — LIVE-verified, not just unit-green (the assessor's explicit bar: static/unit green is insufficient);
- an **ISOLATED E2E proves dispatch produces a commit** — `queue submit` in a fully isolated temp project spins a worker that receives its prompt+Enter and lands a real commit.

## CRITICAL operating instruction — how you produce commits
The daemon's queue-dispatch reliability is **currently UNPROVEN** on the rolled-back binary — this very wedge (hk-8juwz) can eat dispatched beads. So:
- **IMPLEMENT via your OWN in-crew subagents** (the Agent tool) and **commit reviewed diffs directly on a branch**. Do NOT rely on `harmonik queue submit` daemon-dispatch to produce your commits. (The E2E in hk-g4flm *drives* `queue submit` deliberately as the proof-under-test, in a fully isolated temp project — that is the test target, not your commit vehicle.)
- **Every non-trivial commit gets an independent review** — spawn a reviewer subagent to review the diff before you commit; the verdict lands as the commit's review trailer.
- The **daemon still owns terminal bead status transitions**. Do NOT set beads to `closed` or `in_progress` yourself — leave the terminal transition to the daemon. (This especially matters for hk-8juwz: the last close was FALSE — do not re-close on unit-green; live proof gates the close.)

## The beads (dispatch-health lane — run `br show <id>` for full detail)
- **hk-8juwz** (P1 bug) — `claude:local` wedges on the theme/onboarding modal so `agent_ready` times out; the prior fix (d13ae1cf) is a FALSE CLOSE. *Intent: root-cause and actually fix the wedge via isolated `CLAUDE_CONFIG_DIR` onboarding state; LIVE-prove `agent_ready` before it can close.*
- **hk-g4flm** (P1 task) — Dispatch re-enable ISOLATED E2E. *Intent: prove `queue submit` spins a worker that gets prompt+Enter and produces a commit, in a fully isolated temp project (separate socket + separate `.beads`, MUST NOT touch the live fleet daemon/socket). This is the proof that dispatch works once hk-8juwz is fixed.*

## Verification is TWO layers — NOT "one bead cleared"
1. **(a) Heavy in-crew testing during implementation** — run a VERY significant testing process directly via YOUR OWN SUBAGENTS. Beads may not be the right vehicle for this testing; use subagents to exercise the change thoroughly (config-dir isolation argv/env unit tests, a live `claude:local` smoke that observes `agent_ready` actually firing, the isolated E2E producing a real commit, no leakage onto the live fleet socket). You design the testing; it need not be bead-shaped.
2. **(b) Assessor complete-system test as the GATE** — when implementation is complete, hand to the **assessor** (comms `--to assessor` / captain will route) for a full system test before it is considered done. The bar is "thoroughly tested by the crew + assessor-signed-off," not one bead — and for hk-8juwz specifically, a LIVE `agent_ready` on a real `claude:local` run is a hard gate before close.

## Shared-file watchpoint (coordination)
The sibling crew (**kilo**, keeper-reliability lane, lead bead hk-220lv) may also touch **internal/daemon** and **cmd/harmonik**. Before finalizing any daemon-file bead, **rebase onto the target branch** so the daemon merge stays clean. If you hit a real conflict on a shared daemon file or test helper, post `--topic status` to captain and it will serialize.

## Model / posture
**Sonnet** — this is an implementation lane, not a design lane. Harness = **CLAUDE** (codex is unproven/unauthorized as a crew driver right now; do NOT route work onto codex).
