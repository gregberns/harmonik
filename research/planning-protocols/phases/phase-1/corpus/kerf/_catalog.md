# kerf -- Session Catalog

Generated: 2026-04-23 by sub-phase 1A discovery sub-agent.

## Heuristics used

- **Tool-call signature:** Read/Task/WebFetch dominant → PLANNING; Write/Edit/Bash(build/test) dominant → IMPLEMENTATION; both meaningfully present → MIXED.
- **First-user-message content:** Worker-agent dispatch prompt (starts "You are Worker N…" or "You are implementing bead…") → IMPLEMENTATION or MIXED; human design conversation (design rationale, tradeoff negotiation, scope setting) → PLANNING.
- **Message count and size:** Very short sessions (<5 msgs, no assistant) → OTHER.
- **Duration:** Computed as last_ts minus first_ts rounded to nearest minute.
- **Size:** Raw JSONL byte count rendered as KB/MB.

## Sampling notes

All 52 sessions cataloged. No sampling. Three sessions (744083ff, cacd22f2, 5290ac1c) contain only /clear events with no assistant response; one (bacaaccf) contains only shell-output capture with no assistant; classified OTHER. Multiple sessions are ntm worker-agent panes (first user message is a structured dispatch prompt, not human dialog); classified IMPLEMENTATION or MIXED based on content.

## Cross-cutting observations

- **Worker-pane sessions have a distinctive fingerprint:** first user message is a structured dispatch prompt beginning "You are Worker N…" or "You are implementing bead…"; no human presence after that; tool profile is pure Edit/Bash. These are mechanically distinguishable from human-facing sessions.
- **Orchestrator-pane sessions (ntm controller) are ambiguous:** they start as planning/coordination, often transition to spawning + monitoring, and may contain inline design discussion. These are the hardest to classify and the most interesting for planning-protocol research.
- **Spec-writing worker sessions are PLANNING by tool profile (Read/Write dominant, no Bash/Edit build loops) but not human-facing:** they are agent-only execution of a spec writing task handed off from a human planning session. The dialog-density heuristic must be applied at the handoff session level, not the worker level.
- **The largest sessions (38415843, 3fb3dc80, fa557b32, 69050eec) are orchestrator sessions** spanning multiple hours; they contain the richest evidence of planning-protocol behavior: scope negotiation, mid-stream corrections, composability debates.
- **Plan-002 (jig redesign) produced the densest cluster of activity (Apr 9 afternoon, 6–8 concurrent worker sessions).** The orchestrator session for that cluster is the emblematic planning session in this corpus.
- **Session size is not a reliable proxy for planning content:** several large sessions (25d7bd3a, 91694526, eb7699b0) are pure implementation dispatches; 38415843 at 1.3 MB is a mix of early planning + heavy ntm orchestration.

## Catalog

| session_id | date | duration | msgs | size | class | emb | summary |
|-----------|------|----------|------|------|-------|-----|---------|
| 38415843-98c8-4265-8872-bea0eb6b0ed6 | 2026-04-09 | 97 min | 211u/292a | 1.3 MB | MIXED | yes | Earliest orchestrator session; human defines spec-first process, converts docs to specs, negotiates AGENTS.md conventions, spawns first ntm workers. Rich planning-to-implementation handoff. |
| 7ff17283-37ed-4a7a-8d98-5512d999bfd4 | 2026-04-09 | 22 min | 108u/143a | 921 KB | MIXED | no | ntm orchestrator session; monitors worker agents implementing initial spec set; mostly coordination with some inline scope discussion. |
| 6c3016e0-814c-47c9-8571-ae7a64135eb5 | 2026-04-09 | 20 min | 37u/52a | 444 KB | PLANNING | no | Spec-writing worker: reviews jig+CLI specs for cross-reference consistency, produces review report. Agent-only execution. |
| f3db85fc-4980-4baf-b06a-fa96f29d0427 | 2026-04-09 | 20 min | 32u/44a | 456 KB | PLANNING | no | Spec-writing worker: writes finalization.md, reports via agent-mail. Agent-only execution from dispatch prompt. |
| f55e5239-0c72-4070-896c-69e60e05daa4 | 2026-04-09 | 20 min | 28u/39a | 265 KB | PLANNING | no | Spec-writing worker: writes snapshots.md, testing.md, future.md dispatched concurrently with other spec workers. |
| 729dad16-8b08-4e64-a0a1-c412c23b7fec | 2026-04-09 | 49 min | 40u/49a | 196 KB | MIXED | no | ntm orchestrator session; recovery context, human troubleshoots ntm/agent-mail setup issues, limited planning content. |
| 9a0d739d-865c-4c4e-8a6e-1fe2998216d2 | 2026-04-09 | 5 min | 14u/18a | 60 KB | OTHER | no | Debugging agent-mail installer failure (unbound variable in bash script); pure tooling troubleshoot. |
| 3bce8bf5-e4f4-434e-ae7f-837c226cd1c6 | 2026-04-09 | 2 min | 21u/24a | 247 KB | OTHER | no | Session recovery context + immediate user interrupt and exit; no substantive work. |
| af743249-0365-4a25-a784-7965321edb7b | 2026-04-09 | 1 min | 10u/8a | 33 KB | OTHER | no | Session recovery context + immediate interrupt; abandoned. |
| aca52997-d3e0-42cc-9f23-53fe912135f7 | 2026-04-09 | 1 min | 8u/11a | 56 KB | OTHER | no | Session recovery context + immediate interrupt; abandoned. |
| 47a4e085-4b7f-4446-9123-8976969aef86 | 2026-04-09 | 5 min | 10u/16a | 160 KB | PLANNING | no | Spec-writing worker: writes architecture.md + cli.md from source docs. Short, clean dispatch execution. |
| b9fb87b0-75f1-46c4-9e33-e7ff3af3be17 | 2026-04-09 | 2 min | 11u/16a | 172 KB | PLANNING | no | Spec-writing worker: writes snapshots.md and testing.md. Agent-only dispatch execution. |
| 94e49b71-2c07-4a0f-9467-212c074105be | 2026-04-09 | 2 min | 10u/13a | 135 KB | PLANNING | no | Spec-writing worker: writes sessions.md and dependencies.md. Agent-only dispatch. |
| cd9a9ad3-ca65-4889-a5b5-8fa54127e001 | 2026-04-09 | 2 min | 9u/12a | 107 KB | PLANNING | no | Spec-writing worker: writes jig-bug.md. Short, clean dispatch. |
| da4377fb-c84b-4599-964f-ea101f1121f6 | 2026-04-09 | 2 min | 9u/10a | 93 KB | PLANNING | no | Spec-writing worker: writes jig-system.md. Agent-only dispatch. |
| 5ef6169a-8254-45ce-8d6e-f2a44bd252ac | 2026-04-09 | 1 min | 9u/10a | 82 KB | PLANNING | no | Spec-writing worker: writes jig-feature.md. Agent-only dispatch. |
| 9d91e5bb-5de0-4f6c-b467-137e32eaec8d | 2026-04-09 | 1 min | 6u/9a | 94 KB | PLANNING | no | Spec-writing worker: writes works.md. Agent-only. |
| e730bcd0-e12f-47dd-820b-b0c3a21e114f | 2026-04-09 | 2 min | 10u/12a | 71 KB | PLANNING | no | Spec-writing worker: writes cli.md or architecture.md, reports via agent-mail. |
| 855268d4-d549-48c9-b44b-3ced51b2c6a5 | 2026-04-09 | 1 min | 10u/12a | 76 KB | PLANNING | no | Spec-writing worker: writes testing.md or future.md, reports via agent-mail. |
| d3e3cd1d-f668-4989-8362-b6fdafa1bdb6 | 2026-04-09 | 2 min | 18u/25a | 169 KB | PLANNING | no | Spec-writing worker: writes verification.md. Agent-only dispatch. |
| 50ae81b4-4ef8-41d1-97ea-ab3c056182f6 | 2026-04-09 | 3 min | 20u/26a | 190 KB | PLANNING | no | Spec-writing worker: writes finalization.md, registers with agent-mail, waits. |
| 69190a34-c199-454f-b9c9-0d3fa9d618fd | 2026-04-09 | 2 min | 8u/14a | 133 KB | PLANNING | no | Spec-writing worker: writes jig-feature.md or cli.md. Short dispatch. |
| 2f3a81cf-7bec-4e8b-b094-70868d5f7813 | 2026-04-09 | 4 min | 16u/23a | 263 KB | PLANNING | no | Spec-writing worker: writes commands.md from source documents. Agent-only execution. |
| 4337da7c-dd06-4c99-8ee5-ba6989f05009 | 2026-04-09 | 9 min | 81u/96a | 529 KB | IMPLEMENTATION | no | Worker implements beads (scaffold, codename, project-ID packages); pure bead-by-bead Go implementation with tests. |
| 91694526-5c71-4241-9dd8-cb2020fb5925 | 2026-04-09 | 18 min | 84u/129a | 783 KB | IMPLEMENTATION | no | Worker implements beads (bench, session, config packages); Bash/Write/Read dominant Go implementation. |
| 25d7bd3a-1122-42bd-acf8-f4b103b1de68 | 2026-04-09 | 22 min | 88u/135a | 1.1 MB | IMPLEMENTATION | no | Worker implements jig-system parsing (Bead 1c) and related packages; Go implementation with tests. |
| eb7699b0-f7bc-4e83-9dbe-7076c9294c8d | 2026-04-09 | 17 min | 147u/226a | 1.2 MB | IMPLEMENTATION | no | Worker implements multiple beads (test helpers, root command, list command); high Bash/Edit/Write ratio. |
| 3fb3dc80-1dcb-4f76-a4d4-5c9e46683603 | 2026-04-09 | ~28 hr | 424u/637a | 3.1 MB | MIXED | yes | Largest session; controller orchestrates 4 workers implementing Plan 001 (initial kerf CLI), then spans to Apr 10 scope expansion discussion. Contains mid-stream planning pivots. |
| 69050eec-b571-43e3-ab29-6535e950dc50 | 2026-04-10 | ~12 hr | 155u/234a | 1.5 MB | MIXED | yes | Controller session for Plan 003 (SDLC jig coverage); opens with human scope-expansion discussion, then orchestrates implementation wave; agent review output visible inline. |
| 978b297f-b273-4a58-bf34-3fae7e8b208b | 2026-04-09 | 5 min | 35u/43a | 611 KB | PLANNING | no | Spec-review worker: cross-spec consistency check (T3) for jig redesign; reads all specs and produces t3-review.md. |
| df6bdb72-132f-41ec-b103-aabf0f02cead | 2026-04-09 | 3 min | 17u/21a | 283 KB | PLANNING | no | Spec-update worker: updates jig-system.md with aliases, review pattern, resumability (T0 bead). |
| 9c2360ae-3c17-4f60-8f34-f74930272c80 | 2026-04-09 | 2 min | 12u/14a | 254 KB | PLANNING | no | Spec-update worker: writes jig-plan.md, removes jig-feature.md (T1a bead). |
| 081e1c1c-4310-447c-a01f-f62d78c3ee6d | 2026-04-09 | 3 min | 13u/16a | 276 KB | PLANNING | no | Spec-update worker: writes jig-bug.md rewrite (T1c bead). |
| 752c700c-8320-481c-9414-91dc4f367253 | 2026-04-09 | 3 min | 19u/29a | 285 KB | PLANNING | no | Spec-update worker: updates commands.md for jig redesign (T2a bead). |
| 9d104bff-816c-4bd3-8732-5d563b920a6b | 2026-04-09 | 3 min | 14u/18a | 283 KB | PLANNING | no | Spec-update worker: updates architecture.md, works.md, verification.md, _index.md (T2b/T2c/T2e beads). |
| 3aed30e9-c53f-4ac5-8173-cff431e43916 | 2026-04-09 | 3 min | 22u/29a | 267 KB | PLANNING | no | Spec-update worker: updates finalization.md for spec-first behavior (T2d bead). |
| f0747ceb-b3e3-42cd-938e-c76e67faa336 | 2026-04-09 | 3 min | 19u/28a | 283 KB | PLANNING | no | Spec-update worker: updates commands.md, T2a variant or T0 bead. |
| fc8c1359-ef2e-4c32-a3c0-08040715f6a9 | 2026-04-09 | 3 min | 28u/37a | 408 KB | PLANNING | no | Spec-update worker: writes jig-spec.md (T1b bead), applies spec quality fixes. |
| a3c4559f-8256-4c3e-b292-6a884f853dfe | 2026-04-09 | 27 min | 58u/99a | 490 KB | IMPLEMENTATION | no | Implementation worker: C0 bead, updates built-in jigs, alias resolution in jig system. |
| a4aca720-08e2-49f0-ba0b-e0f41eada74c | 2026-04-09 | 4 min | 40u/61a | 418 KB | IMPLEMENTATION | no | Implementation worker: C1a (config changes), C2 (spec-first finalize), C1b (onboarding flow). |
| 347db313-2aa1-4f3f-b59d-076a514449aa | 2026-04-09 | 5 min | 58u/72a | 439 KB | IMPLEMENTATION | no | Implementation worker: C3 bead, updates scenario tests for jig redesign, adds spec-first finalize test. |
| c4e5176d-74c8-4da3-bfb5-3403fdfad728 | 2026-04-10 | 2 min | 22u/31a | 220 KB | IMPLEMENTATION | no | Implementation worker: C1a config changes variant; Edit/Bash dominant. |
| bacaaccf-f28d-47a5-9390-d530c3163af5 | 2026-04-10 | 64 min | 6u/0a | 4 KB | OTHER | no | Shell capture only (pwd, /exit); no assistant responses; bypassPermissions mode artifact. |
| 744083ff-46dd-4b40-a346-278dfc510286 | 2026-04-09 | 0 min | 2u/0a | 2 KB | OTHER | no | /clear event only; no content. |
| cacd22f2-d128-43ff-8d58-82745c819a32 | 2026-04-09 | 0 min | 2u/0a | 2 KB | OTHER | no | /clear event only; no content. |
| 5290ac1c-f5c8-4a4c-b3dd-b6d952230fa2 | 2026-04-09 | 0 min | 2u/0a | 2 KB | OTHER | no | /clear event only; no content. |
| 1a13f712-003a-4e3a-b225-475aa9ddc2ff | 2026-04-09 | 17 min | 73u/117a | 1.1 MB | IMPLEMENTATION | no | Implementation worker: C0 bead, updates built-in jigs (feature→plan/spec/bug), adds alias resolution; Edit-heavy Go implementation. |
| cb7be377-9f3f-4008-b267-292b4a932b6d | 2026-04-11 | 10 min | 55u/101a | 560 KB | IMPLEMENTATION | no | Worker implements retrofit jig file (kerf-xfq bead); creates internal/jig/builtin/retrofit.md. |
| 8bc6a9e6-8c18-4276-9607-15516efb316c | 2026-04-11 | 13 min | 68u/112a | 654 KB | IMPLEMENTATION | no | Worker implements spike jig file (kerf-5rg bead); creates internal/jig/builtin/spike.md. |
| 6db1efe2-b6c3-4da0-a304-c9c650f8db90 | 2026-04-11 | 17 min | 82u/134a | 687 KB | IMPLEMENTATION | no | Worker implements implementation jig file (kerf-hid bead); creates internal/jig/builtin/implementation.md. |
| 9564a3d9-47dc-4915-8eb7-a418e929f4cb | 2026-04-11 | 22 min | 105u/174a | 1.0 MB | IMPLEMENTATION | no | Worker implements JigDefinition struct fields (kerf-kbw bead); adds phase/tools/composable fields to Go structs. |
| fa557b32-5ca1-47a1-835b-48eddb0aa2d3 | 2026-04-11 | ~58 hr | 339u/470a | 1.9 MB | MIXED | yes | Controller orchestrates Plan 003 (SDLC jig coverage) implementation across waves; large Bash count (259) but also scope discussion and wave-coordination planning. |

## Emblematic candidates

1. **38415843** (2026-04-09, 97 min, MIXED) — The origin session for the kerf spec-first workflow. Human and agent negotiate process conventions from scratch: what a plan is, how specs should be structured, role of ntm, how tasks map to beads. Contains the most explicit alignment struggle in the corpus — the human keeps redirecting the agent from building to planning. High value for decision-delegation and writing-load lenses.

2. **3fb3dc80** (2026-04-09→10, ~28 hr, MIXED) — Largest session; Plan 001 implementation but spans into the Apr 10 scope expansion where human identifies the "agent goes off rails without process" problem and proposes extending jigs to cover the full SDLC. Mid-stream planning pivot visible. Good for misaligned-assumption and context-switch lenses.

3. **69050eec** (2026-04-10→11, ~12 hr, MIXED) — Controller session for Plan 003; opens with human articulating the composability + SDLC coverage gap, then explicitly delegates scope refinement turn-by-turn. Shows the "one concept at a time" interaction pattern and the user catching agent overreach on orchestration framing. Good for decision-delegation and form-vs-content lenses.

4. **fa557b32** (2026-04-11→13, ~58 hr, MIXED) — Multi-day orchestration of Plan 003 implementation waves with inline review agent output; shows wave-coordination, agent-to-agent review loop, and human steering between waves. Good for context-switch lens.

## Questions for human

1. Sessions classified as PLANNING (spec-writing workers) are agent-only execution from a dispatch prompt with no human dialog. Should 1C extraction include these, or only sessions with actual human-agent dialog? The distinction matters for protocol analysis (no human = no protocol to study).
2. The four /clear + abandon sessions (744083ff, cacd22f2, 5290ac1c, bacaaccf) appear to be ntm session startup artifacts. Should these be excluded from the corpus entirely or retained as metadata evidence?
3. Session 3fb3dc80 spans 2026-04-09 to 2026-04-10 (appears to be a very long-running session). Is the timestamp range accurate, or does this indicate a session file that accumulated over days?
