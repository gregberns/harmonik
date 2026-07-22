# 01 — Frame: Harden the DOT workflow mechanism

> **Status of this document:** exploration framing. The candidate solutions below are **IDEAS, NOT DECISIONS.**
> Nothing here is chosen. The point of this spike is to explore the problem space thoroughly and design a
> *robust* answer — the current ideas are starting points to pressure-test, not a plan to execute.

## Where this came from (the trigger)

While proving the core-loop-proof acceptance oracle ("the matrix") for the DOT workflow mode (implement → review →
feedback loop), we fixed a real product defect and then discovered a deeper set of structural problems with how the
DOT mechanism passes instructions to agents and how (un)tested the whole mechanism is. The operator's direction:
stop hacking the test fixture, and instead plan a proper hardening of the DOT mechanism — a better input model AND
robust testing — before building anything.

Concrete origin artifacts (read these for the full trail):
- `plans/2026-07-13-code-revamp/COORD.md` entries **c073** (the defect discovery) and **c075** (the fix + the
  end-to-end run that surfaced the round-trip-determinism problem).
- Commit `8dbe5a17` — the ① fix (see Problem 3).
- The repro cell: `scenarios/core-loop-proof/` cell `pi-dot:local` + seed `pi-dot` in `seed-beads.json`.

## The question being explored

**How should the DOT workflow mechanism pass distinct, bead-specific instructions to each role (implementer vs
reviewer vs future node types), and how do we test the whole mechanism robustly enough to trust it and safely
extend it (e.g. variable passing) — across every supported handler?**

Two intertwined threads:
1. **Structured per-role input routing** — a principled way to give the implementer one set of instructions and the
   reviewer another, per bead, without leaking one role's instructions to the other.
2. **In-process integration testing of the DOT cascade** — a test layer between unit tests and the full live e2e,
   with the agent executable and git mocked or twinned, so the whole mechanism (routing, substitution, verdict
   edges, resume/feedback back-edge) can be verified deterministically and extended safely — with checks across
   ALL supported handlers.

## The problems (what we actually know)

### P1 — The implementer sees the reviewer's rubric (instruction-sharing leak). HARNESS-AGNOSTIC.
The bead *body* carries both the task and the `## Review` rubric, and the daemon renders the **whole body** into
BOTH role artifacts:
- implementer → `.harmonik/agent-task.md` via `workspace.WriteAgentTaskVia` (`buildAgentTaskContent`, renders `p.Body`).
- reviewer → `.harmonik/review-target.md` via `workspace.WriteReviewTarget` (`buildReviewTargetContent`, renders `p.BeadBody`).

Both the claude path (`internal/daemon/claudelaunchspec.go:341`) and the pi/codex path
(`internal/daemon/harnessregistry.go:234`) call the SAME `WriteAgentTaskVia` with the full body. **So this is NOT a
pi/codex issue — a claude implementer sees the reviewer rubric too.** It just hadn't been exposed because no prior
test tried to force a real round-trip.

**Observed live (c075):** with the pi-dot seed (task = "add LINE-A first, then LINE-B on the next pass"; rubric =
"require both"), real ornith read the whole body and did BOTH lines in a SINGLE implementer dispatch (two commits,
one turn: `d670d3ca` LINE-A, `206f203e` LINE-B). The reviewer then approved on the first pass — no REQUEST_CHANGES,
no back-edge. The round-trip assertion (gap6) therefore cannot pass, and the resume/feedback path never runs.

### P2 — No deterministic way to force a review round-trip with a real model, given P1.
Because the implementer can see everything the reviewer will ask for, a capable model completes it up front. There
is **no test-only workaround** that forces a round-trip without separating the instructions — every scheme that
hides the pass-2 requirement in the shared body still leaks it to the implementer.

### P3 — ① (resume-feedback delivery) is fixed but has UNIT TESTS ONLY — not proven end-to-end.
Fixed defect (commit `8dbe5a17`): on a DOT back-edge the pi/codex implementer was re-sent the INITIAL seed prompt it
had already satisfied — it never referenced the reviewer-feedback file the daemon writes
(`.harmonik/reviewer-feedback.iter-<N-1>.md`). The claude harness already delivered this via paste-inject
(`pasteInjectImplementerResume`); pi/codex did not. Fix = shared `implementerResumeSeedPrompt`
(`internal/daemon/agentseedprompt.go`) selected on the resume branch of both pilaunchspec/codexlaunchspec.
**It has 4 unit tests and the loop mechanics ran green live, but the fix itself was never exercised end-to-end**
(no back-edge occurred — see P1/P2). We need a deterministic way to drive the resume path.

### P4 — A static `.dot` per-node `prompt` cannot carry bead-specific task text (on its own).
`prompt` (WG-040) is a fixed attribute of a node in the workflow file, so it can't hold e.g. "LINE-A" for one
specific bead. This is why per-node prompts alone don't solve P1. (WG-045 template-params — see Ideas — may close
this gap, but that is a candidate, not a decision.)

### P5 — The reviewer's brief is sourced from the bead BODY, not its node prompt.
`review-target.md` is built from `p.BeadBody`, not from the reviewer node's `prompt`. So even a per-node reviewer
prompt would not currently reach the reviewer. **Per-role routing is not wired end-to-end** — this wiring gap is
part of the work.

### P6 — Testing gap: nothing between unit tests and the full live e2e.
Today there are unit tests and a full live e2e (scratch daemon + real ornith + real git). There is **no in-process
integration test** that drives the actual DOT cascade — dispatch, per-node input routing, param substitution,
verdict edges, the resume/feedback back-edge, terminal-node selection — with the agent executable and git **mocked
or twinned**. Without it, the mechanism can't be verified deterministically and can't be safely extended.

### P7 — DOT wiring is not verified across all supported handlers.
Checks must span every supported handler (claude, pi, codex, …), not just whichever was hand-tested. Precedent for
why this matters: c074 found the pre-existing "codex adapter" lifecycle scenario never actually tested codex — its
bead carried no harness label, so it routed through the **claude** handler. A handler-matrix of wiring checks would
have caught that.

## Candidate ideas (EXPLICITLY UNDECIDED — pressure-test all of these; invent better ones)

> These are the ideas discussed so far. They are **not** chosen. Treat them as hypotheses. The exploration's job is
> to make the eventual design *robust*, which may mean rejecting or reshaping every item here.

- **Structured per-role input model ("a dictionary"):** distinct keys route to distinct roles — one key → the
  implementer's instructions, another → the reviewer's. Possibly built on existing primitives:
  - per-node `prompt` (WG-040),
  - **WG-045 template-param substitution** — `__UPPER_SNAKE__` placeholders in the `.dot`, filled from a per-launch
    param map sealed into the Run record (this is the closest thing to variable passing that exists today, and it
    IS bead-specific — a candidate answer to P4),
  - graph-level `goal` (WG-044),
  - sub-workflow `input_mapping` (EM-034a).
  Open questions: does this become a first-class typed "node inputs" concept, or a convention over the existing
  attributes? How does a role's brief get assembled from {bead body, node prompt, params, goal}? What does the
  reviewer-side wiring (P5) look like?
- **In-process DOT integration harness:** an agent-twin seam (a shim/twin that receives the real daemon-built argv —
  bravo's codex twin in `crossharness_empty_model_test.go` / the codex twin scenario is a working model) plus a git
  seam (mock or an ephemeral scratch git), driving the full cascade in-process. Open: how much of the real daemon
  path to keep vs. stub; where the seams live; how to assert distinct sessions, same-model no-leak, and the
  round-trip deterministically.
- **REJECTED so far (record the rejection):** splitting the bead body on a string marker (e.g. `## Review`) to
  separate implementer vs reviewer text. Fragile and "icky" — do not string-split.

## What "answered" looks like (directional)

1. A **robust, decided design** for per-role structured input routing that does NOT leak one role's instructions to
   another and is NOT a string-split — captured as a spec (this touches the agent-task.md / review-target.md
   contract in `specs/execution-model.md` EM-015d and `specs/workflow-graph.md`).
2. An **in-process DOT integration test harness** (agent-twin + git seam) that drives the real cascade and can
   deterministically exercise: per-role input routing, WG-045 param substitution, the verdict edges, and the
   **resume/feedback back-edge** (finally proving ① end-to-end — P3).
3. **Handler-matrix wiring checks** — the DOT mechanism verified across every supported handler (P7), so a bead that
   silently routes to the wrong handler fails loudly.
4. A path to **safely extend** DOT (variable passing and beyond) on top of the harness.

## Follow-on (explicitly in scope, after the mechanism lands)
Per operator: once the input model + integration harness exist, add **really robust testing over the whole
beads/workflow wiring** to confirm everything is wired correctly, with **checks across all supported handlers**.
This is P6 + P7 taken to completion — the harness is the enabler; the comprehensive handler-matrix coverage is the
deliverable.

## Constraints / context to respect
- Beads are OFF this phase; work is tracked in `plans/2026-07-13-code-revamp/COORD.md`, not the bead ledger.
- The DOT code is load-bearing and important; changes here are contract-level (what each role is shown) and must be
  spec-first.
- The pi-dot cell (`scenarios/core-loop-proof/`) is the concrete repro and should stay a known-RED marker (do NOT
  fake it green) until this work lands — see the companion "flag known-broken workflows" task.
- Existing input primitives to build on / evaluate: WG-040 (`prompt`), WG-044 (`goal`), WG-045 (template params),
  EM-034a (`input_mapping`).
