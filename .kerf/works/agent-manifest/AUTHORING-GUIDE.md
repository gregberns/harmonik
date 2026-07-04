# Authoring guide — agent type instruction sets (READ BEFORE DRAFTING)

This guide keeps every agent type's instruction set **short, consistent, and non-rambling.** The
operator's hard requirement: *"short and sweet — none of these huge rambling complicated instruction
sets."* Treat the line limits below as ceilings, not targets.

## The three files per type (folder = `.harmonik/agents/<type>/`)

### 1. `soul.md` — identity (PROVENANCE MASTER). HARD LIMIT ≤ 25 lines.
The durable "who am I" that gets re-pinned verbatim on every restart. Exactly these parts, terse:
- **I am** `<type>` — one sentence on my job.
- **I do** — 2–4 bullets, the core verbs of this role.
- **I do NOT** — 2–4 bullets, the adjacent role's job I must not take (the anti-drift boundary).
- **I escalate to** — who, and for what.
- Do NOT author a parent-intent line — `brief` grafts it at emit time from the parent role's `soul.md`
  (the parent named in the manifest's `identity.parent_intent`; crew→captain, captain→admiral,
  admiral→operator).

No prose paragraphs. No examples. No history/incidents. If it needs a "because," it's too long.

### 2. `operating.md` — how I work. HARD LIMIT ≤ 45 lines.
The loop + the pointers. NOT a re-teaching of the tools.
- **On wake** — the short boot ritual (join comms, read handoff, confirm identity).
- **Loop** — the core cycle in ≤6 numbered steps.
- **Skills I use** — a LIST of skill names with a one-line "when" each. DO NOT inline skill bodies —
  the boot command injects those as short-desc + pointer. Reference, don't restate.
- **Bounds** — 2–3 hard don'ts specific to operating (not identity).
Anything longer than a line belongs in a referenced skill, not here.

### 3. `manifest.yaml` — the tie-together config.
Follow the schema in `../../../plans/2026-07-03-agent-identity-and-context/MANIFEST-DESIGN.md §2 + §7`.
Fields: `type`, `cardinality`, `harness`, `identity:{soul,parent_intent}`, `context:[{ref,as,presence}]`,
`triggers`, `handoff`, `keeper`, `lifecycle`, `markers`. Point `context` skill refs at the SHARED
skills folder where a skill is used by >1 type.

## Voice rules (all files)
- Imperative, terse, second person ("You are…", "Do X"). No hedging, no rationale prose.
- Reference existing repo skills/docs by name; NEVER copy their content in.
- If you're tempted to explain *why*, delete it — the mission states what, not why.
- A reader who knows harmonik should grasp the whole file in under 60 seconds.

## Source material (mine for CONTENT, then compress hard)
Per-type behavior already exists, scattered and verbose, in:
- crew: `.claude/skills/crew-launch/SKILL.md`, `.harmonik/crew/missions/_TEMPLATE-runner.md`, `leto.md`
- captain: `.claude/skills/captain/SKILL.md` + `STARTUP.md` (VERY long — extract the essence only)
- admiral: `.harmonik/crew/missions/admiral.md`, `admiral-playbook.md` (a rambling incident dump — do NOT mirror its style)
- watch: `.claude/skills/watch/SKILL.md`, `.harmonik/crew/missions/watch.md`
- shared standing rules: `.claude/skills/orchestrator-rules/SKILL.md`
Your job is DISTILLATION, not transcription. The existing docs are the anti-pattern for length.
