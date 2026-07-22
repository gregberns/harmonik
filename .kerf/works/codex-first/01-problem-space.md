# 01 — Problem Space

> **Pass 1 of the spec jig.** This artifact BACK-FILLS the formal problem-space from a
> design-complete plan that already carries operator sign-off on its locked decisions. It
> does not re-open anything.
>
> **Source material (read these first — this doc distills, it does not replace them):**
> - `plans/2026-07-21-codex-first/_plan.md` — the design-complete execution plan; every seam
>   named to `file:line`. This work is its formalization.
> - `plans/2026-07-20-codex-strategy-realignment/DECISIONS.md` — **D1, D3, D4** (locked; do
>   NOT reopen), plus D2-direction (recommendation superseded by D3, findings still valid).
> - `plans/2026-07-20-codex-strategy-realignment/research/05-commit-ownership-and-sandbox-mechanism.md`
>   and `.../06-codex-worktree-support.md` — the real `file:line` + kernel-error evidence.

## What is changing, and why

Local **Codex** cannot run a bead today. Two agent-invented obstacles block it, and both are
now formally decided against:

1. A **fail-closed isolation fence** refuses to launch Codex unless it is bound to an enabled
   remote ssh worker (`Config.CodexRequireIsolationBoundary`, `substrate_select.go`,
   `workloop.go:3626`). This was an *agent* ruling (2026-07-18), not an operator mandate, and
   **D1 drops it** — Codex MUST be runnable locally on the host, exactly like Claude.
2. Codex's **native `workspace-write` sandbox** blocks its commit and its own `exec_command`
   shell steps, because a linked worktree's real `.git` common dir lives outside the
   workspace-write writable root (research/05: kernel/Seatbelt EPERM, not an approval denial).
   **D3 turns the native sandbox OFF** → `danger-full-access`, uniform with the unsandboxed
   Claude the operator already runs every day.

**Why now:** local Codex clearing beads reliably is the operator's stated **Priority-0** —
it is the token-offload enabler. Once Codex runs beads, the Captain can staff Codex implementer
crews to carry P1/P2/P3 platform work, conserving Claude tokens (Claude retained for the review
node and oversight). Codex is simultaneously the first thing to get working AND the harness the
other crews should run on.

This is the **simple execution of already-locked decisions** — it flips a small number of code
seams and proves the result on one real bead. **No new architecture.**

## Goals — what is true about the system after this work

1. `HARMONIK_SUBSTRATE=codexdriver` with **no worker bound** launches a Codex implement node on
   `LocalRunner`, on the daemon host — no refusal, no `refusedIsolationBoundaryArgv0`.
2. The effective Codex sandbox on **both** exec argv branches (initial + resume) is
   `danger-full-access`; no `workspace-write` and no `writable_roots` remain in the codex-exec
   argv.
3. A **real bead completes end-to-end on local Codex** through the *unmodified* DOT flow:
   implement → commit (with `Refs:<bead>` trailer) → review (claude) → bead closed by the
   daemon's terminal transition.
4. Codex's own in-run shell step (`git status` / the commit_gate command) runs **without** an
   "Operation not permitted" / EPERM denial (the exec-facet is retired).
5. The Captain can **staff a Codex crew** — implementer beads on Codex, reviewer on Claude —
   and that crew clears ≥1 bead, demonstrating the token-offload split.

## Non-goals (explicit — from _plan.md §2 OUT)

- **Containers** — P3 (platform-architecture C5).
- **Remote / multi-machine execution** — P3; ssh-per-node is scrapped (D4). Its live wedges
  (hk-qxvc2, hk-daegv, reverse-tunnel/env-forward) are therefore MOOT.
- **Scheduling / leases / orphan-recovery** — P3.
- **The uniform harmonik-level sandbox** — a SEPARATE PARALLEL workstream (D3;
  FOLLOWUP-TOPICS §A; engages ON-024). We run `danger-full-access` now; secure-by-default is
  designed and landed later, in parallel, and must NOT block this.
- **De-hard-coding per-node model/harness config** — the reviewer harness is hard-coded to
  claude today (`reviewerSubstrate`). Making DOT nodes take model/harness as config is a
  **fast-follow**, tracked in platform DECISIONS.md; NOT part of this work.

## Constraints (things that must not change / must survive)

- **Keep codex-implements / claude-reviews** while Codex is unproven (independent-eyes review
  gate). `reviewerSubstrate` stays tmux/claude.
- **Keep the `-c sandbox_mode` override mechanism** from commit `49d7fde3` — do NOT `git revert`
  it wholesale. `codex exec resume` rejects `--sandbox`/`--add-dir`/`-C` (arg-parse error);
  only `-c` is accepted on resume. That structural fix is real and must survive; only the value
  changes and only the now-dead writable-roots derivation is deleted.
- The DOT flow itself is **unchanged** — no new nodes, no cascade edits.
- No external-version binding: do not pin codex 0.142.0; degrade gracefully.

## Success criteria (concrete, from _plan.md §5)

The specs/plan describe a system in which: local codex runs with no worker (crit. 1); a real
bead completes end-to-end on local codex (crit. 2); the exec/shell facet is clear of EPERM
(crit. 3); the effective posture is uniformly `danger-full-access` with no residual
workspace-write/writable_roots (crit. 4); a Codex crew is staffable and clears ≥1 bead (crit. 5);
token-offload is demonstrated (crit. 6); and the removed guard + writable-roots helpers leave
`go build`/`go vet`/`ubs` green with no dead code (crit. 7). **Verification** (operator, §7.3) is
two-layer: heavy in-crew subagent testing during implementation + an assessor complete-system
test as the gate — not merely "one bead cleared."

## Preliminary affected areas

- **Code seams** (the actual change): `cmd/harmonik/substrate_select.go`,
  `internal/daemon/workloop.go`, `internal/daemon/codexlaunchspec.go`,
  `internal/daemon/codexcommit.go` (confirmation only), the Captain crew-start env path.
- **Normative spec**: `specs/harness-contract.md` (**HN**) is the only home; today NO spec pins
  either the isolation fence or the codex sandbox mode (survey confirmed). The natural landing
  is a new HN-series requirement recording the local + `danger-full-access` posture, citing the
  Pi sibling precedent **PI-015** ("harness runs unsandboxed") and deferring the uniform sandbox
  to **ON-024**'s parallel workstream.
