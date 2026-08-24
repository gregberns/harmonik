---
id: reviewer-subagent-drift
title: Spawned reviewer sub-agents still enforce the comment rule that was retired yesterday
type: bug
priority: 0
labels: [agent-config, review-gate, clear-the-ground]
depends_on: []
blocks: [reviewer-portability-review]
workstream: W7
batch: 1
---

## Problem

Commit `9e70fbe4b` ("stop the review loop that manufactures comment work") added an exclusion stating
that a comment inside a `.go` file is not a normative doc, so the normative-doc review section does
not apply to it. It landed in `.claude/skills/agent-reviewer/SKILL.md` only.

`.claude/agents/agent-reviewer.md` is the definition the Agent tool actually loads when a reviewer
sub-agent is spawned. It is 1,936 bytes shorter and is missing that exclusion entirely — from both
§2 and the flag vocabulary. Verified by diff on 2026-08-23.

So every reviewer sub-agent spawned today still enforces the retired rule. The measured cost of that
rule, recorded in the exclusion itself: over the 300 commits before 23 Aug, eight changed five or
fewer lines of Go code and twenty or more lines of Go comment, five of them re-editing the same two
daemon test files. A `REQUEST_CHANGES` on comment prose spawns a comment-only commit, which draws
another review, which falsifies the replacement prose.

The file carries a banner that names exactly this failure: *"A fix that lands only in the skill never
reaches a spawned sub-agent."*

## Scope

`.claude/agents/agent-reviewer.md` — bring it into line with
`.claude/skills/agent-reviewer/SKILL.md`.

Also check `.claude/agents/agent-config-reviewer.md` against
`.claude/skills/agent-config-reviewer/SKILL.md`; as of 23 Aug it differed only by the banner.

## Done when

1. The two agent-reviewer files differ only by their frontmatter and the source-of-truth banner —
   proved by a diff in the commit body.
2. A spawned `agent-reviewer` sub-agent, given a commit that is mostly Go comment changes, does not
   raise the normative-doc flag.

## Limits

- Copy the skill's content; do not re-word it. The skill is the source of truth and re-wording
  creates a third variant.
- This task fixes the instance. Governing the four copies so it cannot recur is
  `skill-copy-governance` and is deliberately separate.
