# Research Summary — Workflow Modes

Three research findings. Two are clean inputs to design; one is a **decision-pending pivot** that requires user input before the design pass can close.

## Decisions ready to lock in

- **Beads encoding:** `workflow:<mode>` label prefix on the bead (e.g., `workflow:ralph`). Matches existing harmonik convention, no schema change, queryable. (→ `beads-encoding/findings.md`)
- **Session-resume mechanism is supported by Claude Code:** capture session_id via `claude -p ... --output-format json | jq -r '.session_id'`, resume with `claude --resume <id> -p "<new-message>"`. Headless throughout, no TTY needed. (→ `session-resume/findings.md`)
- **Reviewer hardening:** require file:line citations in reviewer JSON's `notes` field for any `REQUEST_CHANGES`; rotate prompt seed per iteration to reduce verdict-gaming. Cheap, evidence-light, do it.
- **Cap=3 with safety rails:** early-exit on APPROVE, plus a diff-hash no-progress detector that exits early to `needs-attention` if iteration N's diff ≈ iteration N-1's.
- **`needs-attention` queue is operator-drained, no auto-retry.** Bake into operator-NFR spec.

## Decisions pending — REQUIRES USER

### D-RES-1: Default iteration shape — session-resume vs fresh-per-iteration

**User's earlier choice:** Resume the same implementer Claude instance across iterations, with reviewer feedback as the next user message. Reason cited: tasks shouldn't be too big, context bloat is manageable.

**External evidence challenges this** (→ `ralph-prior-art/findings.md` §F2). The dominant failure modes for same-session iteration are:

- **Compounded sycophancy** — RLHF-trained implementers prioritize agreement; sycophancy grows turn-over-turn. (ACL 2025 multi-turn sycophancy benchmark.)
- **Mode collapse / oscillation** — implementer reproduces near-identical solutions across retries despite feedback. Effectiveness decays exponentially per iteration. (Observability Gap paper; IaC feedback-loop study.)
- **Reviewer "Yes Man"** — same base model on reviewer biases toward approval of own family's output.

Huntley's actual ralph loop (the source of the name) **resets context per iteration** for exactly these reasons. The user's design is closer to Reflexion (Shinn et al. 2023). Sycophancy / mode-collapse are why Huntley reset.

**Counter-evidence for session-resume:** fresh implementer can simplify away constraints whose rationale lived only in prior reasoning. (Google Developers Blog on context transfer; Manus / LangChain converge on "rolling summary + structured session-state document" as the middle path.) In our case the durable artifacts — git diff, reviewer JSON, worktree file state — already function as that document.

**Agent recommendation:** Default = fresh-implementer-per-iteration. Session-resume becomes an opt-in mode (`workflow:review-loop-resume` or similar) added later if a specific case shows it helps.

**User call:** keep session-resume as the default per original choice, or pivot to fresh-per-iteration default?

### D-NAME-1: Name in spec/config

The user used "ralph loop" in conversation. **"Ralph loop" in current usage means Huntley's pattern** (context-reset), which is the opposite of what the user designed. Calling our mode `ralph` in code/spec will confuse readers familiar with the term.

**Agent recommendation:** Use a neutral name in spec/config — `review-loop` or `critic`. "Ralph" can stay in user-facing conversation.

**User call:** rename in spec/config, or keep `ralph` with a note acknowledging the divergence from Huntley?

## What I can write without the user's decision

- C4 (process-lifecycle daemon config) — independent of D-RES-1 and D-NAME-1.
- C5 (beads-integration label encoding) — independent.
- C6 (workspace-model clarification) — trivial.
- C7 (operator-NFR) — mostly independent; cap-mitigation events depend on D-RES-1 only at event-naming level.

## What needs the user's decision

- C1 (handler-contract LaunchSpec fields) — `phase` field shape depends on D-RES-1.
- C2 (execution-model Run record) — `context` carries `session_id` only in resume mode; if default is fresh-per-iteration, the spec text is materially different.
- C3 (event-model) — event-name pivot if D-NAME-1 changes; iteration-shape events differ between fresh and resume.

## Next step

Surface this to the user, lock D-RES-1 and D-NAME-1, then write change-design docs.
