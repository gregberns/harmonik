# Bead Reassessment — Assessor Rubric (READ-ONLY)

You are one of five parallel assessors auditing harmonik's OPEN beads. The project
has been active ~7 weeks and moved fast; many open beads may be stale, already
landed, superseded by a decision, or mis-prioritized. Your job is to judge each
bead assigned to you on THREE axes and emit a structured verdict. **You must NOT
mutate any bead** (no `br close`, no `br update`). Read-only investigation only.
Write your verdicts to the output file named in your task prompt.

## The three axes
1. **Should it still be done?** Is the work still relevant, or superseded/obsolete/
   already-landed/duplicate?
2. **Is the approach still correct?** The bead's described approach may predate
   current code, specs, or locked decisions. The codebase moved under it.
3. **Reprioritization** — does its priority (P0..P4) still match reality?

## CRITICAL: verify against CURRENT reality, not the bead text
The bead text reflects what was true when filed. Check what is true NOW:
- `git -C /Users/gb/github/harmonik log --all --oneline --grep "<bead-id>"` — but
  MANY implementations landed WITHOUT a bead ref. So ALSO grep the actual code/
  feature: does the function/flag/file the bead asks for already exist? Use Grep/Read.
- Read `specs/` for the relevant spec (specs are normative; code is expected to match).
- Read `STATUS.md` (locked decisions, phase framing) and skim recent `git log --oneline -40`.
- For an epic: check its children's status (`br list --status=open | grep "<epic-id>\."`)
  and whether the spec it implements is largely landed. An epic whose children are
  all closed is stale-open and should close (precedent: hk-uxm0j closed this session).

## Key project context (anchors — read these)
- **Phase 0 closed 2026-05-06.** Phase 1 (MVH) achieved 2026-05-14 = harmonik runs
  claude end-to-end on a bead with zero human input.
- **Daemonization was DEFERRED in the phase-1 specs** (STATUS.md §"Phase-1 scope:
  daemonization deferred 2026-05-08") — BUT a persistent daemon is NOW LIVE and in
  production (this very session dispatched beads through it). So any bead that says
  "parked behind daemonization" or assumes a foreground-only binary needs fresh eyes:
  the thing it waited on may have HAPPENED (→ now actionable) or been built DIFFERENTLY
  than the bead assumed (→ APPROACH-STALE or OBSOLETE).
- **10 architectural decisions locked 2026-04-19** (STATUS.md). Reopening one needs
  strong new evidence — flag beads that contradict a locked decision.
- **Avoid MVH framing** — MVH was a one-time milestone (2026-05-14), NOT a per-feature
  scope label. A bead leaning on MVH framing may be mis-scoped.
- Specs live in `specs/`. Knowledge base in `docs/`. kerf is an EXTERNAL planning tool
  in beta-test here (its feedback beads are about kerf, not harmonik features).

## Verdict vocabulary (pick exactly one per bead)
- **DONE** — the work has landed. MUST cite the commit sha or the existing code path.
- **OBSOLETE** — no longer relevant (superseded by a decision, a different design that
  shipped, or the premise no longer holds). Explain what changed.
- **DUPLICATE** — of bead `hk-XXXX`. Cite it.
- **APPROACH-STALE** — still wanted, but the described approach predates current code/
  decisions. Say what changed and sketch the corrected approach in one line.
- **KEEP** — still valid and correctly scoped/prioritized as-is.
- **REPRIORITIZE** — keep, but priority should change. Give from→to and why.

(A bead can be KEEP on relevance but still REPRIORITIZE — if so, use REPRIORITIZE and
note it's otherwise valid.)

## Output format — one block per bead, exactly this shape
```
### hk-XXXXX — <short title>
- VERDICT: <DONE|OBSOLETE|DUPLICATE|APPROACH-STALE|KEEP|REPRIORITIZE>
- ACTION: <concrete next step — e.g. "br close (landed as 6232b303)", "br update --priority 1", "revise desc: <what>", "route upstream to kerf project", "none — keep">
- NEW_PRIORITY: <P0..P4 or "-">
- EVIDENCE: <commit sha / spec ref / code path / decision — be specific and durable>
- CONFIDENCE: <high|med|low>
```
After all blocks, add a `## Cluster summary` with counts per verdict and any
cross-bead duplicates or themes you noticed (e.g. "5 of these are subsumed by epic X").

Be decisive but honest about confidence. When unsure between KEEP and OBSOLETE,
prefer KEEP with CONFIDENCE: low and say what evidence would settle it. Keep each
EVIDENCE line concrete (a sha, a file:line, a spec ID) — vague evidence is useless
for the apply phase.
