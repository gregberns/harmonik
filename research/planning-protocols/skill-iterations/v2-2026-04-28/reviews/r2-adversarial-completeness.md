# r2 — Adversarial Completeness Review

Reviewing v2 `session-handoff` and `session-resume` from the perspective of the next session's agent. Each flagged gap names a concrete failure.

## Gaps worth flagging

### 1. No "where on disk am I" marker (branch / worktree / cwd)

`session-resume` reads `./HANDOFF.md` and "the project's standard start-of-session files." It does not say *where*. Concrete failure: previous session was working in a git worktree or feature branch, wrote HANDOFF there, and committed. Next session opens the repo at main, finds no HANDOFF (or finds a stale one from an older stream), and either invents a starting point or proceeds on the wrong tree. A single line in the handoff naming the branch/worktree the work lives on closes this — and `session-resume` should sanity-check `git branch --show-current` against it.

### 2. Nested-doc projects: "standard start-of-session files" is repo-root only

`session-resume` lists `CLAUDE.md`, `AGENT_INDEX.md`, `STATUS.md`, `TASKS.md`. This project has track-local equivalents at `research/planning-protocols/CLAUDE.md` and `research/planning-protocols/STATUS.md` whose read-order rules differ from root. Concrete failure: agent reads root STATUS (which lists "Phase 0 active"), skips track STATUS (which says Phase 2 Step 4.5 is mid-flight), and grounds itself one phase behind. The "first files to open" line in the handoff *can* carry the track files — but `session-resume`'s prose pulls the agent toward the root set first. Either drop the explicit root list from `session-resume` (let the handoff name what to read) or note that track-local equivalents take precedence.

### 3. "In flight" hides multi-step intermediate state

"What's in flight" reads as one item. Real planning sessions often leave a fan-out half-finished: 3 of 5 sub-agents launched, 2 results filed at `phases/phase-2/analysis/`, 1 still running, prompts for the remaining 2 drafted but not sent. Concrete failure: next session reads "in flight: Step 5 sub-agent fan-out" and re-launches all five from scratch, duplicating work and producing conflicting analysis files. The skill should hint that fan-outs need their per-branch state, not a single "in flight" line. One terse sentence ("if work is mid-fan-out, list each branch's state") would do it without re-introducing checklisting.

### 4. No freshness signal on the handoff itself

Only marker is `<!-- PP-TRIAL:v2 -->`. Concrete failure: HANDOFF from three days ago is still on disk; user starts a new session today, the agent reads it as current, and resumes work on a thread the user already abandoned. A date on line 1 (or alongside the trial marker) lets `session-resume` flag "this handoff is N days old, confirm it's still the active thread" before paraphrasing.

## Live-with

- No "decisions made" log — git log + STATUS cover it; re-litigation risk is real but small.
- No autonomy guidance — `session-resume`'s "normal judgment" plus project CLAUDE.md is enough.
- No out-of-scope section — project instructions own scope.
