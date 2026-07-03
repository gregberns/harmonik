# SESSION — codex-harness (plan jig)

## This session (2026-06-08, crew agent `codexcrew`, reporting to captain)

Drove the kerf plan work `codex-harness` from creation through all 8 passes to **ready** in one
session. Research + design only — **no implementation code written**. The deliverable is the kerf
artifacts (below) + 20 filed beads (`codename:codex-harness`, open, NOT dispatched, DAG-wired).

### What `codex-harness` designs
Adding **OpenAI codex** as a second, selectable implementer harness in harmonik alongside Claude
Code. A run picks its harness (per-bead > per-queue > per-node > global default=claude). Claude stays
default; existing beads/queues/workflows are unchanged (N-1 safe).

### Key conclusions
- **Seam:** a Go `Harness` interface (LaunchSpec/Seed/Retask/Teardown/DetectReady + `SessionIDPolicy`
  + `Completion`) inserted at the existing `deps.launchSpecBuilder` hook + `AdapterRegistry`; two
  declared insertion points only; all shared infra (tmux substrate, worktree, git commit-detection,
  merge, review-loop) reused unchanged.
- **codex shape:** `codex exec --json` is a one-shot run-to-exit JSONL surface → the adapter is
  spawn-per-turn with a **captured** `thread_id` (caller can't mint one — NOT_PLANNED). Re-task =
  `codex exec resume`. No TUI splash/paste/`/quit` machinery needed — codex exits on completion.
- **Billing landmine:** `codex login` bills the ChatGPT **subscription**; `--with-api-key` bills the
  API **credit pool**; `codex exec` may silently honor `OPENAI_API_KEY`/`CODEX_API_KEY` (undocumented,
  version-variable). Guard = strip both env keys + `forced_login_method=chatgpt` + pre-flight
  `codex login status` fail-closed assert. Mirrors the project's `ANTHROPIC_API_KEY` credit-burn fix.
- **Biggest caught gap (decompose review):** the `agent_heartbeat` emitter is per-harness (a timer
  loop inside the claude handler, CHB-019, `claudehandler_chb006_024.go:588-617`), NOT git-derived —
  so a silent codex `exec` would trip the staleness-kill. Fixed via `Completion()==ProcessExit`
  bypassing the kill path at `dot_cascade.go:643`.

### Method
5 parallel research sub-agents (3 code-reading dims 1/3, 2 web-research dims 2/4); 4 independent
reviewer sub-agents (decompose, change-spec w/ adversarial billing+seam re-challenge, integration,
tasks). All four reviews APPROVED after in-place fixes. Repo working tree kept clean throughout
(artifacts on the gitignored kerf bench; bead ledger committed locally as `chore(beads)`).

### Artifacts on the bench (`.kerf/works/codex-harness/`)
- `01-problem-space.md`, `02-analysis.md`, `03-components.md` (+ `decompose-review.md`)
- `04-research/{current-harness,codex-cli,auth-billing,seam-design,integration}/findings.md`
- `05-specs/` — 6 component specs `C1..C6-*.md` (authoritative) + 5 dimension specs (crosswalk) +
  `change-spec-review.md`
- `06-integration.md`, `SPEC.md` (→ `specs/harness-contract.md` on finalize) (+ `integration-review.md`)
- `07-tasks.md` (+ `tasks-review.md`)

> Note on structure: the work is organized on two axes — the mission's **5 research dimensions**
> (which name the `04-research/` dirs and the dimension-spec files) and the **6 implementation
> components C1–C6** (which name the authoritative change-specs and the beads). The dimension-spec
> files are thin crosswalks pointing to the C-specs.

### Beads (20: 18 impl + 2 test) — all `codename:codex-harness`, open, NOT dispatched
Root/start-ready: **T1 hk-e8omz**. Full DAG + IDs in `07-tasks.md`. Scenario `hk-vfmn9`, exploratory
`hk-qxfj0`. Captain handles dispatch.

### Next (for the captain / implementing session)
Dispatch from `br ready` / `kerf next` (T1 first). Land in the 6-step order in `06-integration.md`.
Do NOT enable codex in production until the C3/C6 MUST-TEST checklist passes on the pinned codex
version. Land `specs/harness-contract.md` from `SPEC.md` (kerf finalize copies it).
