# Next-Agent Handoff -- Planning Protocols Deep-Dive

> Paste the entire body of §1 below into a fresh Claude Code session opened in this repo. It is the self-contained handoff prompt. It does not assume any inherited context.

---

## 1. Handoff prompt (paste this)

I want to do a deep-dive review of the planning-protocols research track in this repo. Your role is to be my guide through it -- help me digest what was produced, where to focus, what next steps mean, and how to act on it. I need you to pace this so I can actually process the material rather than drinking from a firehose.

**First actions (do these before anything else, in order):**

1. Read `/Users/gb/github/harmonik/research/planning-protocols/INDEX.md` -- this is the entry point and it names the reading path for my role (Path A -- user deep-dive).
2. Read in the order INDEX §4 Path A prescribes:
   - `/Users/gb/github/harmonik/research/planning-protocols/phases/phase-2/findings.md` -- full read
   - `/Users/gb/github/harmonik/research/planning-protocols/phases/phase-2/evaluation-framework.md` §1-§5
   - `/Users/gb/github/harmonik/research/planning-protocols/phases/phase-2/analysis/reviewer-challenge-observed.md`
   - `/Users/gb/github/harmonik/research/planning-protocols/phases/phase-2/analysis/reviewer-multi-framing.md`
   - `/Users/gb/github/harmonik/research/planning-protocols/phases/phase-2/kerf-integration-draft.md`
3. Read `/Users/gb/github/harmonik/research/planning-protocols/STATUS.md` for the full session history, especially the Phase 2 session entry.
4. Skim `/Users/gb/github/harmonik/research/planning-protocols/phases/phase-2/analysis/unified-protocol-catalog.md` -- 87 protocols on a shared schema; you don't need to absorb all of them, but you need to be able to find any one I ask about.
5. Read `/Users/gb/github/harmonik/research/planning-protocols/phases/phase-1/research-statement.md` if you need Phase 1 context.

Once you've done that, check in with me. Don't summarize everything -- just tell me you're ready and ask where I want to start. I'll drive from there.

**How I want you to work with me:**

- **User-led pacing.** Follow my curiosity. Do not push me through a fixed agenda unless I ask for one. If I want to spend 20 minutes on one reviewer's output, let me.
- **Small bites.** Discuss one thing at a time. Do not answer with 800-word summaries when 3 sentences and a follow-up question would serve me better.
- **Surface tensions.** Where did the six reviewers disagree? Where is the evidence thin vs strong? Where does the user's observed practice survive challenge vs where was it displaced? I need to know which findings are safe and which are bets.
- **Decision-oriented.** There are open decisions I need to reach. Guide me toward them without rushing:
  1. Authorize Step 4.5 (corpus-signal filter across 195 sessions)? See `phase-2-findings.md` §9 item 1 and `evaluation-framework.md` §4.
  2. Adopt Layer 1 foundation stack in next kerf works? (`phase-2-findings.md` §4.1)
  3. Adopt Layer 6 safe swaps in next kerf works? (`phase-2-findings.md` §4.6)
  4. Authorize any Layer 7 A/B experiments? (`phase-2-findings.md` §4.7) Experiment #1 is the highest-leverage.
  5. Disposition of `phase-2-kerf-integration-draft.md` §8 open questions -- turn it into a kerf work or not?
  6. Follow-ups flagged as open questions in `phase-2-findings.md` §9.
- **Honest about limits.** Many Phase 2 rankings carry `[filter-dep]` flags because Step 4.5 wasn't run. Treat those as hypotheses, not settled findings. If I'm about to act on a filter-dep claim, flag it.
- **Track where we leave off.** When I pause or shift topic, restate where we are so the session is resumable.

**What you should NOT do:**

- Do NOT start new research, spawn new sub-agents, or launch new analytical passes. The research is done; this session is digestion.
- Do NOT re-run any of the Phase 2 steps or re-evaluate protocols. Defer to the existing reviewer outputs.
- Do NOT add to or overwrite existing artifacts. If we uncover a genuinely new finding during discussion, capture it in a new dated file; never rewrite prior work. (See `research/planning-protocols/CLAUDE.md` hard rules.)
- Do NOT commit to turning the kerf-integration draft into an actual kerf work without me explicitly authorizing it.
- Do NOT assume I want to act on the convergent-winner recommendations just because reviewers converged. I get to push back on reviewer conclusions; your job is to help me think, not to sell the recommendations.
- Do NOT skip the reading in "First actions." Shortcutting the read-in leads you to a shallow conversation I don't want.

**Context you should know up front:**

- The research spans Phase 1 (corpus mining, scoping) + Phase 2 (deep research). Both are closed.
- Phase 2 produced three top-level deliverables: `phase-2-findings.md` (main output), `evaluation-framework.md` (durable instrument), `phase-2-kerf-integration-draft.md` (draft for my review).
- The research's central risk is local-maxima anchoring on my own observed patterns. Phase 2 was deliberately structured to guard against it -- external-source pass before refinement; multi-framing requirement; challenge-observed reviewer with explicit anti-deferential instruction. You should honor that same discipline when walking me through.
- The most contested finding is `numbered-question-close` being challenged as an anti-pattern based on aviation CRM evidence. That's the single biggest bet in the recommendations -- don't let me wave past it.
- I flagged the evaluation criteria as uncertain going into Phase 2; Step 1 interrogated them but did not replace them wholesale. The multi-framing requirement (provisional + Framings A/B/C) is the compromise outcome. I may want to push on whether that's right.

Ready when you are.

---

## 2. Notes for the user (not for the agent)

- This handoff is written for a fresh session in this same repo. The absolute paths above assume `/Users/gb/github/harmonik/...`.
- The agent's first message back should be a short "I've done the read-in, ready when you are, where do you want to start?" -- not a summary dump. If it dumps a summary, redirect it.
- Decision points in the prompt (§Decision-oriented) are the minimum set. You may surface others; don't feel constrained to six.
- Suggested first topic if you don't have a strong preference: ask the agent to walk you through `phase-2-findings.md` §3 (cross-reviewer convergence) and §4.6 (safe swaps) first -- these are the lowest-cost wins and will orient you to the shape of the recommendations.
