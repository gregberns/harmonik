# Research/Design — No-compaction state digest & context reconstruction

> Component: `context-management-no-compaction`. Source: research sub-agent (opus), prior-art principles + external + first-principles, 2026-05-27.

## TL;DR
- The digest is a **computed view over an append-only structured log**, not a hand-written document. Two streams feed it: the **derivable** stream (events.jsonl, queue.json, git log, worktree listing — machine-rendered, zero LLM) + a tiny **non-derivable agent-note stream** the orchestrator *appends* at decision time (`harmonik note add`). The digest is regenerated fresh each cycle — never summarized-from-prior-summary → no telephone-game drift.
- **Always-in-digest** set is small and decision-relevant: loop counts, beads needing a decision (failed-twice/blocked/deferred), open agent-notes. Everything else (full bead bodies, diffs, transcripts, event detail) is **fetch-on-demand** behind 4-5 canonical commands. Fits Greg's 30-40-line budget because the digest is *pointers + flags*, not *content*.
- Staleness solved by **resolution-marking on the append-only note log**: a note is open until a later append resolves it (or a derivable fact contradicts it). The view shows only open notes → digest ~constant-size regardless of session length.

## Q1 — Where the line goes
External taxonomy (arXiv 2604.08224): working / episodic / semantic / personalized. For an orchestrator the digest carries only **working context + open episodic exceptions**; semantic/personalized live in the fixed prefix + AGENTS.md (fetched).
- **(a) ALWAYS-IN-DIGEST** (~constant size): loop pulse (counts by status, queue depth, HEAD sha clean/dirty); **exception list** (only beads needing action: failed-twice, blocked, deferred-pending-user, merge-conflict — one line each: `id — flag — one-clause why`); **open agent-notes** (decisions/hypotheses/warnings still in force); one **next-action** line.
- **(b) FETCH-ON-DEMAND**: full bead bodies (`br show`), diffs (`git show`), event detail (`grep events.jsonl`), worktree contents, prior transcripts, full queue JSON, healthy in-flight beads (just the count).
- **(c) NEVER-NEEDED**: agent chatter, heartbeats, merged-and-closed beads, resolved notes.
- **The cut rule:** a fact earns a digest line only if the orchestrator would take a *different action* this cycle for knowing it.

## Q2 — Derived vs written
**Derivable, zero-LLM** (`harmonik digest` command): everything traceable to durable artifacts. `queue.json`→per-bead status/attempts/run_id/workflow_mode; `events.jsonl`→run_started/completed/failed/reviewer_verdict/review_loop_cycle_complete/merge_conflict; `git log --grep "Refs:<id>"`→merged-ness; `git worktree list`→live runs. **`attempts>=2` + a `run_failed` = "failed-twice→investigate" computed mechanically, no LLM.**
**Non-derivable (agent-authored):** *why* a decision was made, a hypothesis, a negative result ("tried X, failed because Y"), a deferral reason, a cross-bead insight. No artifact encodes intent. Capture it by **appending a structured note at the moment of decision**, never by end-of-session summarization. (Factory.ai measured free-form summaries at 2.19-2.45/5.0 on artifact tracking + decision-rationale loss — append-at-decision is lossless for the thing that matters because it's written when the agent holds the intent.) PROPOSAL: `harmonik note add --kind=decision|hypothesis|warning|defer --refs hk-X "text"` → one append to `notes.jsonl`.

## Q3 — Append-only log + computed view
**Log = two append-only streams, never rewritten:** (1) events.jsonl+queue.json+git (derivable, daemon owns); (2) `notes.jsonl` (non-derivable; orchestrator appends, never edits; record `{id,ts,kind,refs:[bead_ids],text,resolves:<note_id|null>}`).
**View = `harmonik digest`**, a pure function: fold events+queue+git → current per-bead status + counts (event-sourcing projection); filter to exception set; fold notes.jsonl (a note is open unless a later record `resolves` it; emit open only); render ≤40 lines.
**Why no telephone-game:** summarization is `summary_n = LLM(summary_{n-1}+new)` — lossy composition, error compounds. The view is `digest_n = f(full_log)` — recomputed from source every cycle → cycle N as faithful as cycle 1. "Artifacts as state, not memory" made literal.

## Q4 — Staleness & resolution (constant-size)
1. **Explicit resolution-marking** — `harmonik note resolve <id>` appends a resolving record; view drops it. Log grows on disk; *view* stays bounded.
2. **Derivable auto-resolution** — a `{kind:defer, refs:[hk-X]}` note auto-closes when the derivable stream shows hk-X merged/closed (view cross-checks notes vs current bead state, suppresses notes whose beads left the exception set). GCs stale notes without agent action.
3. **Tiered detail/TTL** — `warning`/`decision` persist until resolved; `hypothesis` older than N cycles w/ no follow-up demotes to `…+3 older hypotheses (fetch)`.
Net: digest size tracks *open exceptions + open decisions*, not session length.

## Q5 — The 30-40 line budget
The budget is for **instructions** (the fixed prefix); the digest is *data* kept small by the Q1 cut rule. PROPOSED prefix (~30-40 lines): ROLE (orchestrator; delegate; default dispatcher = harmonik run); LOOP (read digest → dispatch ready / investigate exceptions → append notes on decisions → repeat); RULES (~12: never re-dispatch failed-twice w/o investigator; never terminal-write beads — daemon owns; CWD stays repo root, never cd into worktree; push autonomy granted; pause only for genuine user decisions; review gate before merge); FETCH COMMANDS (~8 canonical: `harmonik digest`, `br show <id>`, `git show`/`git log --grep`, `grep <run_id> events.jsonl`, `br ready`/`kerf next`, `harmonik note add|resolve`); GLOSSARY POINTER (AGENTS.md §glossary). Everything else fetched. **Protocols embedded in command output** (prior-art principle): `harmonik digest` ends each render with the 2-3 fetch commands relevant to current exceptions → the agent never recalls them.

## Q6 — Worked example (`harmonik digest` output)
```
=== harmonik digest @ 2026-05-27T22:55  HEAD=05a0a0b (clean) ===
LOOP: dispatched=8  merged=3  in-review=2  failed=2  | queue: ready=12  active-group=0
EXCEPTIONS (act on these):
  hk-3yz2d  FAILED×2   no_commit ×2 — INVESTIGATE, do not re-dispatch  → grep run 019e6d27 events.jsonl
  hk-7af1q  BLOCKED    dep hk-karlz not landed                          → br show hk-7af1q
  hk-9c2dx  DEFER      pending user input on schema (see note n-0007)
IN-REVIEW (no action):  hk-kk201, hk-4ab9z
MERGED this loop (git is record):  hk-1aa, hk-2bb, hk-9dn
OPEN NOTES:
  n-0007 [defer]    hk-9c2dx: needs user call on whether schema field is nullable
  n-0005 [warning]  do NOT split this batch into sub-agents; single harmonik run --beads
  n-0004 [hypothesis] hk-3yz2d no_commit may be the StaleWatcher hang, not impl — confirm in events
NEXT: dispatch next 3 from kerf next (skip hk-7af1q until hk-karlz lands); spawn investigator for hk-3yz2d.
FETCH: `br show <id>` · `grep <run_id> events.jsonl` · `harmonik note resolve <id>`
```
In digest: counts, 3 exceptions, 3 open notes, next action. Fetched only if needed: 5 in-review/merged bodies, hk-3yz2d full events, every healthy in-flight diff, the 12-deep ready queue, resolved notes — none change *this* cycle's decision.

## Q7 — Comparison (why digest+fetch wins here)
(a) LLM compaction — lossy composition (Factory 2.19-2.45/5.0), telephone drift → rejected. (b) Sliding window — keeps *recent* but loop needs *open* work regardless of recency (a 4-hr-old still-open deferral falls off). (c) Full-transcript resume — faithful but unbounded; 95% is happy-path noise. (d) RAG-over-history — probabilistic (might miss the one critical warning), top-k-similar is wrong for a loop needing *exhaustive* open-exceptions (fine as a *fetch backend*, poor as digest). **digest+fetch wins** because orchestrator state is small/structured/mostly-derivable and the non-derivable residue (intent) is captured losslessly at write-time. **Risks (honest):** (1) if the agent forgets `note add`, intent is lost → prompt it in the loop + daemon auto-derives what it can; (2) the `harmonik digest` projector is load-bearing code needing tests (it's the executable HANDOFF.md); (3) auto-resolution can wrongly suppress a live note → explicit resolve is authority, auto is convenience only.

## Open questions for user
1. Note authorship: manual (`note add`) vs daemon-prompted on every defer/block transition? (Lean: prompted on defer/block, manual otherwise.)
2. Does `harmonik digest` fully replace HANDOFF.md, or coexist as the executable view? (Lean: digest replaces the derivable parts; HANDOFF.md's directives-block survives as the fixed prefix.)
3. notes.jsonl ownership: beside events.jsonl under `.harmonik/` written by orchestrator (new write surface daemon doesn't own), or via `harmonik note` CLI the daemon serializes? (Memory "agents see only terminal transitions" → CLI-mediated.)

## Sources
arXiv 2604.08224 (Externalization in LLM Agents); factory.ai/news/evaluating-compression; mem0.ai state-of-ai-agent-memory-2026; newsletter.victordibia.com context-engineering-101; arXiv 2601.07190 (Active Context Compression). Harmonik grounding: HANDOFF.md, .harmonik/events/events.jsonl, .harmonik/queue.json, session-handoff/resume SKILL.md.
