# secure-dev -- Session Catalog

Generated: 2026-04-23 by sub-phase 1A discovery sub-agent.

## Heuristics used

**Tool-ratio classification:**
- IMPLEMENTATION: Write + Edit count > 5 OR Bash count > 15, low dialog density (msgs/lines ratio), directive first-message pattern ("study specs and pick the most important thing to do")
- PLANNING: Read dominant (>10), Agent count > 2 with no corresponding Edit spike, first message involves design discussion, gap analysis, or back-and-forth dialog
- MIXED: Both patterns present in meaningful proportion; typically starts with planning prompt then drives into execution
- OTHER: <5 msgs, exit-command stubs, single-tool infrastructure tests, bug investigation unrelated to secure-dev design

**Dialog density proxy:** (user+assistant msg count) / (total lines). High ratio = more dialog, less tool data.

**Primary signals for deep-read selection:** first-message content (template vs human-authored), tool absence of Write/Edit, presence of Agent without corresponding Edit spike, multi-day timestamp spans.

## Sampling notes

**Total corpus:**
- `-Users-gb-github-secure-dev`: 131 sessions
- `-Users-gb-Developer-secure-dev`: 2 sessions (both /exit stubs → OTHER)
- ntm-worktree sub-agent dirs: **8 directories** (wave1-cc-1 through wave3-cc-3). These are sub-agent worktree sessions spawned by ntm for parallel implementation. Not cataloged in detail; they are worker sessions operating under controller direction, not human-planning sessions.

**Stubs identified by metadata (1-3 msgs, <5KB):** 18 sessions. All classified OTHER without deep-read. These appear to be session-initialization artifacts (Claude Code startup sequences) or /exit-only sessions.

**Deep-read sessions (content examined):**
- 15 earliest non-stub sessions (chronological order from 2026-03-29)
- All sessions with atypical tool profiles (Read-dominant with no Edit; Agent-heavy without Bash spike; sessions with WebFetch/WebSearch; sessions with agent-mail MCP tools)
- 5 random mid-to-late sessions for heuristic validation: 38d82665, 4294e375, 809ec5e3, cfa19c66, ecf66307 — all confirmed "study fix_plan.md" implementation template

**Metadata-only sessions (not deep-read):** ~90 sessions following the "study specs / study fix_plan.md" directive template with standard Bash+Edit+Read tool distribution. The 5-sample validation confirmed this template maps reliably to IMPLEMENTATION.

**Dominant session type discovered:** The secure-dev project used a heavily templated autonomous-agent workflow. Two recurring first-message templates account for ~80% of all substantive sessions:
1. `"study specs/ and pick the most important thing to do"` + test/commit rules → pure IMPLEMENTATION sub-agent
2. `"Study .scratch/fix_plan.md and pick the most important thing to do"` + test/commit rules → pure IMPLEMENTATION sub-agent

These templates explicitly prohibit human questions ("Never ask the human questions or wait for input"), making them unsuitable for planning-protocol research.

## Cross-cutting observations

- **Template-driven implementation dominates.** ~80% of sessions are autonomous sub-agent workers with the same directive template. No back-and-forth; human writes one turn, agent implements. This is the anti-pattern for planning-protocol research — it's the post-spec phase.
- **Planning sessions are rare and brief.** Only 4-5 sessions show genuine human-agent dialog on design/architecture. The ratio is roughly 5:95 planning:implementation.
- **Gap analysis sessions are the closest to planning.** Sessions like c4a1c009 and d1704aa0 combine spec review, multi-agent review orchestration, and task decomposition — the most dialog-like activity found.
- **The `79a42399` session is exceptional.** Explicit human framing ("collaborative effort, I'll set direction, you explore ideas") and back-and-forth on architectural choices (exploratory testing, agent loop design) — the clearest example of planning-protocol behavior in this corpus.
- **Controller-agent sessions are meta-implementation, not planning.** Sessions like b7eca5d2 (controller) coordinate workers but the human turn is still a single directive, not a design conversation.
- **Agent-mail infrastructure sessions (April 4) are tooling tests, not planning.** Short sessions verifying MCP connectivity.

## Catalog

| session_id | date | duration | msgs | size | class | emb | summary |
|-----------|------|----------|------|------|-------|-----|---------|
| 6f8b944e-8b9c-4476-a53d-a7143ea39cbd | 2026-03-29 | 0m | 7 | 7KB | OTHER | no | Create git branch; single task, no planning content |
| f6bcb8cf-217c-4afc-868d-7650b2949300 | 2026-03-29 | 0m | 2 | 3KB | OTHER | no | (stub: session init artifact) |
| 5fc1f626-f130-4f20-81b0-7f245f1a944d | 2026-03-29 | 0m | 2 | 3KB | OTHER | no | (stub: session init artifact) |
| f444b3b8-fed4-4f24-bea9-9c4a50c20322 | 2026-03-29 | 0m | 1 | 2KB | OTHER | no | (stub: session init artifact) |
| 3bb3f78d-e756-4bbe-bebf-16c4d2e10d53 | 2026-03-29 | 0m | 1 | 2KB | OTHER | no | (stub: session init artifact) |
| cceb8a22-84e8-4edc-b155-cfe98b262f48 | 2026-03-29 | 0m | 1 | 2KB | OTHER | no | (stub: session init artifact) |
| a08d34fd-c7e3-4f68-9d2e-54d22f922bcb | 2026-03-29 | 0m | 1 | 2KB | OTHER | no | (stub: session init artifact) |
| fbe617d4-3b29-461f-b952-05b2efc930c4 | 2026-03-29 | 0m | 1 | 2KB | OTHER | no | (stub: session init artifact) |
| ef1b8619-7c10-4e74-bddc-85b3350ab996 | 2026-03-29 | 0m | 1 | 2KB | OTHER | no | (stub: session init artifact) |
| 7917bd74-ae33-424c-9432-25919f07540b | 2026-03-29 | 0m | 1 | 2KB | OTHER | no | (stub: session init artifact) |
| d1f7d316-87ff-4b51-b219-1f921283f5ec | 2026-03-29 | 0m | 1 | 2KB | OTHER | no | (stub: session init artifact) |
| 3ec9e77b-159d-4d6d-bc81-daab48383bcf | 2026-03-29 | 0m | 1 | 2KB | OTHER | no | (stub: session init artifact) |
| c6f78d27-eee6-4662-b266-f4ac288eddbb | 2026-03-29 | 0m | 1 | 2KB | OTHER | no | (stub: session init artifact) |
| 2da27e82-04c4-4d2b-b705-4fa6a371597d | 2026-03-29 | 0m | 1 | 2KB | OTHER | no | (stub: session init artifact) |
| 208b1d1e-ae01-4ec9-b86e-4fcc6fa4b588 | 2026-03-29 | 0m | 1 | 2KB | OTHER | no | (stub: session init artifact) |
| a0c8dad4-53f8-4944-970a-b50b4afade0d | 2026-03-29 | 0m | 3 | 3KB | OTHER | no | (stub: near-empty session) |
| d96e71c5-1c2b-4b17-81fb-8c54418635f4 | 2026-03-29 | 9m | 16 | 17KB | OTHER | no | Settings/hooks query; no design content |
| a9cff2d0-3dbb-400e-b463-13416ee0ac6f | 2026-03-29 | 25m | 433 | 1.9MB | IMPLEMENTATION | no | (metadata-only: 40 Edit, 30 Bash, 44 Read; "study specs" template, test+commit loop) |
| 0d204a22-5470-441c-875c-ebd68efaf380 | 2026-03-29 | 0m | 26 | 518KB | IMPLEMENTATION | no | (metadata-only: 6 Read, 3 Bash; "study specs" template, read-heavy aborted early) |
| 0fa26818-244f-4951-8d20-503252e2af26 | 2026-03-29 | 1m | 60 | 796KB | IMPLEMENTATION | no | Read-only session (17 Read, 3 Glob); "study specs" template, no writes produced |
| 1074e7f1-3b21-4d13-91b4-4110094a0920 | 2026-03-29 | 7m | 154 | 892KB | IMPLEMENTATION | no | (metadata-only: 30 Bash, 15 Read, 2 Edit; "study specs" template) |
| 67b7309d-800d-4f06-9c9d-d36b415dc295 | 2026-03-29 | 12m | 137 | 721KB | IMPLEMENTATION | no | (metadata-only: 20 Bash, 17 Read, 5 Write; "study specs" template) |
| 8c1294d4-4e44-4c95-8285-848a3b91a841 | 2026-03-29 | 11m | 137 | 780KB | IMPLEMENTATION | no | (metadata-only: 16 Bash, 19 Read, 10 Edit; "study specs" template) |
| 30d31719-34af-473b-9835-877e40327439 | 2026-03-29 | 12m | 81 | 432KB | IMPLEMENTATION | no | (metadata-only: 15 Bash, 6 Edit, 4 Write; "study specs" template) |
| 4219d23f-aaf4-49df-b246-41c025a44bdb | 2026-03-29 | 20m | 71 | 539KB | IMPLEMENTATION | no | (metadata-only: 14 Bash, 6 Edit, 6 Read; "study specs" template) |
| 264de87f-b8ba-43b8-954c-d0436292adb6 | 2026-03-29 | 14m | 72 | 501KB | IMPLEMENTATION | no | (metadata-only: 12 Bash, 2 Agent, 7 Read; "study specs" template) |
| 2bcb9cae-ce68-4686-939d-2853634f18b3 | 2026-03-29 | 15m | 91 | 548KB | IMPLEMENTATION | no | (metadata-only: 9 Agent, 10 Bash, 13 Read; "study specs" template, multi-agent) |
| 455913d9-b8e2-46b5-a2b2-fb7ede7ecb0f | 2026-03-29 | 11m | 140 | 1.0MB | IMPLEMENTATION | no | (metadata-only: 31 Read, 12 Bash, 8 Grep; "study specs" template) |
| 93460093-0e5f-4951-8037-4a23143025c9 | 2026-03-29 | 22m | 99 | 689KB | IMPLEMENTATION | no | (metadata-only: 7 Agent, 16 Bash, 7 Read; "study specs" template) |
| dd09c578-d967-4cf6-a486-8c5154f7b052 | 2026-03-29 | 10m | 60 | 435KB | IMPLEMENTATION | no | (metadata-only: 4 Agent, 9 Bash, 5 Read; "study specs" template) |
| 8baed1c6-8c25-4bc7-a615-a84f24f8c8c1 | 2026-03-29 | 24m | 118 | 878KB | IMPLEMENTATION | no | (metadata-only: 13 Bash, 8 Edit, 11 Read; "study specs" template) |
| d6940926-74e3-4f01-8fba-a5dd352023cb | 2026-03-29 | 19m | 116 | 885KB | IMPLEMENTATION | no | (metadata-only: 7 Agent, 15 Bash, 15 Read; "study specs" template) |
| edf748b7-5652-4e54-b9ba-54b8bea1f7ba | 2026-03-29 | 12m | 93 | 669KB | IMPLEMENTATION | no | (metadata-only: 3 Agent, 11 Bash, 15 Read; "study specs" template) |
| 83a63eae-52e5-4ee0-921c-96451e8f5645 | 2026-03-29 | 15m | 148 | 946KB | IMPLEMENTATION | no | (metadata-only: 21 Bash, 10 Edit, 21 Read; "study specs" template) |
| f08b6b5a-d0bf-4810-ae54-d2ccafe57378 | 2026-03-29 | 45m | 159 | 913KB | IMPLEMENTATION | no | (metadata-only: 31 Bash, 22 Read, 2 Edit; "study specs" template) |
| 765bd4e2-7ce7-4315-97f1-1ac3f632f3d0 | 2026-03-29 | 24m | 139 | 1.2MB | IMPLEMENTATION | no | (metadata-only: 5 Agent, 32 Read, 8 Bash; "study specs" template) |
| 876f92ab-7f99-4b96-85c1-b4023f4a7372 | 2026-03-29 | 19m | 125 | 938KB | IMPLEMENTATION | no | (metadata-only: 6 Agent, 18 Read, 13 Bash; "study specs" template) |
| 16a447da-2d5b-4b53-9f6b-a83880a23c73 | 2026-03-29 | 16m | 108 | 1.3MB | IMPLEMENTATION | no | Digital twin mocklimactl/mocktmux integration; "study specs" template, read-heavy |
| 2d7abed8-7de2-40f3-8ea4-48b21744bf79 | 2026-03-29 | 65m | 219 | 1.9MB | IMPLEMENTATION | no | (metadata-only: 5 Agent, 49 Read, 22 Bash; "study specs" template) |
| 6492f2fe-0d16-48ae-9330-5429c5e5e03c | 2026-03-29 | 20m | 151 | 1.5MB | IMPLEMENTATION | no | (metadata-only: 4 Agent, 36 Read, 10 Bash; "study specs" template) |
| 3dfbd90b-4aba-4071-961a-7b5e94165e44 | 2026-03-29 | 17m | 138 | 1.0MB | IMPLEMENTATION | no | (metadata-only: 18 Bash, 21 Read, 6 Edit; "study specs" template) |
| 58b7dec1-9ac2-4e6b-aac6-67afd0edcf1c | 2026-03-29 | 15m | 92 | 657KB | IMPLEMENTATION | no | (metadata-only: 5 Agent, 8 Bash, 11 Read; "study specs" template) |
| 8aab1659-9be2-4db4-b5c1-a1dec4acbe89 | 2026-03-29 | 8m | 75 | 401KB | IMPLEMENTATION | no | (metadata-only: 13 Bash, 9 Read, 2 Edit; "study specs" template) |
| 88a435dd-e12c-47c9-b5d0-93d5c7e8c760 | 2026-03-29 | 8m | 74 | 289KB | IMPLEMENTATION | no | (metadata-only: 2 Agent, 10 Bash, 8 Read; "study specs" template) |
| d230f68e-5ac8-4774-8961-6d579209c47d | 2026-03-29 | 7m | 69 | 424KB | IMPLEMENTATION | no | (metadata-only: 8 Bash, 12 Read, 2 Write; "study specs" template) |
| 6b2487ca-31f6-4eb3-b3da-079ab7a7a88c | 2026-03-29 | 6m | 73 | 362KB | IMPLEMENTATION | no | (metadata-only: 11 Bash, 11 Read, 2 Edit; "study specs" template) |
| 855730a8-5d08-48fe-ba99-e3ffbab7b59e | 2026-03-29 | 9m | 69 | 302KB | IMPLEMENTATION | no | (metadata-only: 13 Bash, 6 Read, 2 Edit; "study specs" template) |
| cd31b095-83f9-4242-88a8-558125f6218e | 2026-03-29 | 14m | 119 | 460KB | IMPLEMENTATION | no | (metadata-only: 21 Bash, 11 Read, 6 Edit; "study specs" template) |
| 38d82665-cdbe-4ecb-abb7-5f78a0dcfa77 | 2026-03-29 | 7m | 73 | 344KB | IMPLEMENTATION | no | (metadata-only: 9 Bash, 5 Edit, 8 Read; "study fix_plan" template) |
| f3bac8e6-c35b-4a91-b1a4-fb04cea3d2a0 | 2026-03-29 | 3m | 57 | 134KB | IMPLEMENTATION | no | (metadata-only: 10 Bash, 4 Edit, 5 Read; "study fix_plan" template) |
| 29382e34-e255-4b0a-9324-bd55a419e252 | 2026-03-29 | 21m | 136 | 543KB | IMPLEMENTATION | no | (metadata-only: 21 Bash, 8 Edit, 16 Read; "study fix_plan" template) |
| 4ad2f70f-3637-4b9c-aea8-76d58f3411d7 | 2026-03-29 | 12m | 108 | 556KB | IMPLEMENTATION | no | (metadata-only: 15 Bash, 5 Write, 13 Read; "study fix_plan" template) |
| 4294e375-f956-4f1e-abd5-1fbce37ddeba | 2026-03-29 | 15m | 103 | 512KB | IMPLEMENTATION | no | (metadata-only: 12 Bash, 5 Edit, 11 Read; "study fix_plan" template) |
| bee65548-a64f-4ac4-b603-75ea11f59358 | 2026-03-29 | 23m | 184 | 1.2MB | IMPLEMENTATION | no | (metadata-only: 21 Bash, 16 Edit, 25 Read; "study fix_plan" template) |
| cfa19c66-84ad-4dcf-91eb-4c2fc3aee993 | 2026-03-29 | 7m | 89 | 523KB | IMPLEMENTATION | no | (metadata-only: 11 Bash, 2 Edit, 14 Read; "study fix_plan" template) |
| ecf66307-fae9-4538-afdf-75733bb0f312 | 2026-03-29 | 14m | 94 | 602KB | IMPLEMENTATION | no | (metadata-only: 4 Agent, 12 Bash, 7 Read; "study fix_plan" template) |
| 5cc756b1-bdc0-4a2f-9bf0-8d3f027bfb55 | 2026-03-29 | 7m | 130 | 610KB | IMPLEMENTATION | no | (metadata-only: 17 Bash, 9 Write, 13 Read; "study fix_plan" template) |
| a7ab9cf5-ecc4-4438-9d5f-56d1414e6af8 | 2026-03-29 | 15m | 144 | 795KB | IMPLEMENTATION | no | (metadata-only: 17 Bash, 13 Grep, 13 Read; "study fix_plan" template) |
| 3156095b-8938-432a-a0bb-87d57f457d0e | 2026-03-29 | 20m | 153 | 651KB | IMPLEMENTATION | no | (metadata-only: 15 Bash, 19 Grep, 15 Read; "study fix_plan" template) |
| def3b61b-0346-435b-b60b-2080231a850b | 2026-03-29 | 17m | 182 | 1.2MB | IMPLEMENTATION | no | (metadata-only: 21 Bash, 13 Edit, 22 Read; "study fix_plan" template) |
| 92dc16f7-f0b9-4b24-a8e7-d8640a39f6d6 | 2026-03-29 | 24m | 185 | 1.2MB | IMPLEMENTATION | no | (metadata-only: 20 Bash, 15 Edit, 21 Read; "study fix_plan" template) |
| aba417bf-860a-4b99-94b8-3a0cfad19afc | 2026-03-29 | 12m | 166 | 734KB | IMPLEMENTATION | no | (metadata-only: 22 Bash, 10 Edit, 18 Read; "study fix_plan" template) |
| 2cab9c75-adbb-42db-a60a-443a3c2638d0 | 2026-03-29 | 20m | 330 | 1.0MB | IMPLEMENTATION | no | (metadata-only: 79 Bash, 18 Read, 16 Grep; heavy execution session) |
| f8079d9d-afa4-41b1-94fb-45aa32e327e9 | 2026-03-29 | 11m | 138 | 671KB | IMPLEMENTATION | no | (metadata-only: 14 Bash, 6 Edit, 16 Read; "study fix_plan" template) |
| ea2a4457-c723-4abd-ae0e-ff1d9010df2c | 2026-03-29 | 55m | 208 | 1.0MB | IMPLEMENTATION | no | (metadata-only: 25 Bash, 20 Edit, 18 Read; "study fix_plan" template) |
| 13507f36-95cc-4721-bd6b-60e6e1fc982b | 2026-03-29 | 24m | 246 | 1.7MB | IMPLEMENTATION | no | (metadata-only: 26 Bash, 29 Edit, 29 Read; "study fix_plan" template) |
| d10d1898-b759-44a9-aeeb-ea226b46f696 | 2026-03-29 | 30m | 210 | 1.5MB | IMPLEMENTATION | no | (metadata-only: 23 Bash, 13 Edit, 30 Read; "study fix_plan" template) |
| 24b98c48-837e-4b9b-ae0e-3d6941b46a33 | 2026-03-29 | 19m | 214 | 913KB | IMPLEMENTATION | no | (metadata-only: 22 Bash, 20 Edit, 28 Read; "study fix_plan" template) |
| 17fa1793-c2b7-4175-80c1-1465c8d96598 | 2026-03-29 | 10m | 126 | 778KB | IMPLEMENTATION | no | (metadata-only: 11 Bash, 13 Edit, 13 Read; "study fix_plan" template) |
| 1b0e6c59-e0b9-4816-9f15-d20ace94056e | 2026-03-29 | 22m | 150 | 831KB | IMPLEMENTATION | no | (metadata-only: 21 Bash, 18 Edit, 14 Read; "study fix_plan" template) |
| 58e1707c-282d-4878-bec9-bb9c6ebc960f | 2026-03-29 | 78m | 489 | 1.6MB | IMPLEMENTATION | no | (metadata-only: 7 Agent, 68 Bash, 26 Write; "study fix_plan" template, heavy execution) |
| 2109b45b-01a4-4b2f-9cbb-fe81d7e7d697 | 2026-03-29 | 36m | 256 | 1.8MB | IMPLEMENTATION | no | (metadata-only: 6 Agent, 51 Read, 34 Bash; "study specs" template) |
| 44ce7446-52d3-4177-88b6-f2a18141f4b3 | 2026-03-29 | 0m | 3 | 5KB | OTHER | no | (stub: near-empty session) |
| 468fc096-9e9b-4ca4-95ce-25215e16af41 | 2026-03-29 | 0m | 3 | 3KB | OTHER | no | (stub: near-empty session) |
| 5617a206-feb6-4bcc-9e43-6129ec6a8d84 | 2026-03-29 | 0m | 2 | 2KB | OTHER | no | (stub: session init artifact) |
| 396c4ad4-8f01-4549-beb5-86140bca7821 | 2026-03-29 | 0m | 1 | 2KB | OTHER | no | (stub: session init artifact) |
| 8bd62228-177d-4538-a943-bf09196ca0ab | 2026-03-29 | 0m | 1 | 2KB | OTHER | no | (stub: session init artifact) |
| aeaf8189-679c-41aa-a38f-e1dd49b4cc5a | 2026-03-29 | 0m | 1 | 2KB | OTHER | no | (stub: session init artifact) |
| 7c4a3d9e-a9d4-4656-a74b-414443b03c03 | 2026-03-29 | 0m | 3 | 4KB | OTHER | no | (stub: near-empty session) |
| 5fc1f626-f130-4f20-81b0-7f245f1a944d | 2026-03-29 | 0m | 2 | 3KB | OTHER | no | (stub: session init artifact) |
| 063961b4-3e7a-481e-9fad-2d8fd6297091 | 2026-03-29 | 0m | 1 | 2KB | OTHER | no | (stub: session init artifact) |
| 61cda91e-525e-4adc-ab0e-db16c81d6596 | 2026-03-29 | 0m | 1 | 2KB | OTHER | no | (stub: session init artifact) |
| f303c68e-6ef1-4671-b4e5-d13448ad5c61 | 2026-03-29 | 0m | 1 | 2KB | OTHER | no | (stub: session init artifact) |
| 7ca7fbfa-0b61-480c-8541-d4aaa503cbec | 2026-04-04 | 0m | 5 | 4KB | OTHER | no | (stub: minimal session, no content) |
| ba1119d5-3b74-4acb-bd24-e2aae7a0c63c | 2026-03-30 | 0m | 4 | 92KB | OTHER | no | Single Read call; 92KB likely large file loaded, no dialog |
| e318335e-e930-4f56-9a24-92158c4a2b14 | 2026-03-30 | 0m | 1 | 3KB | OTHER | no | (stub: session init artifact) |
| ee0519f4-f379-4321-bc9f-5bc771b6bcbe | 2026-03-30 | 0m | 1 | 3KB | OTHER | no | (stub: session init artifact) |
| fcb5c838-d8a6-4675-bbc6-16fa2d0b93e9 | 2026-03-30 | 0m | 1 | 3KB | OTHER | no | (stub: session init artifact) |
| 7f3d5691-91b7-4c2f-9b9b-c2691f0d40cd | 2026-03-30 | 9m | 71 | 392KB | IMPLEMENTATION | no | (metadata-only: 9 Bash, 11 Read, 2 Edit; "study fix_plan" template) |
| 809ec5e3-8988-4054-96d7-3d30b4b4e2f6 | 2026-03-30 | 9m | 98 | 511KB | IMPLEMENTATION | no | (metadata-only: 13 Bash, 10 Read, 5 Edit; "study fix_plan" template) |
| 181dfd9f-ee5a-4c90-8964-981f359a753d | 2026-03-30 | 12m | 151 | 634KB | IMPLEMENTATION | no | (metadata-only: 14 Bash, 21 Read, 11 Edit; "study fix_plan" template) |
| 84764bc7-1b68-428b-8580-a66b4d466022 | 2026-03-30 | 19m | 176 | 908KB | IMPLEMENTATION | no | (metadata-only: 19 Bash, 26 Read, 13 Edit; "study fix_plan" template) |
| 46ea3b91-eeb7-49f0-85fb-fa1c9fad16c6 | 2026-03-30 | 11m | 154 | 635KB | IMPLEMENTATION | no | (metadata-only: 18 Bash, 20 Edit, 15 Read; "study fix_plan" template) |
| 568c794f-71bd-4d54-b700-cc66ca385102 | 2026-03-30 | 19m | 173 | 956KB | IMPLEMENTATION | no | (metadata-only: 16 Bash, 13 Edit, 20 Read; "study fix_plan" template) |
| 6a5a33fb-fa33-493f-a43b-58ea5ea5e932 | 2026-03-30 | 25m | 240 | 902KB | IMPLEMENTATION | no | (metadata-only: 38 Bash, 19 Edit, 25 Read; "study fix_plan" template) |
| b59ff417-6492-4212-9e8f-eefb85dfeda7 | 2026-03-30 | 21m | 340 | 1.2MB | IMPLEMENTATION | no | (metadata-only: 50 Edit, 46 Read, 24 Bash; heavy edit session) |
| 982f4abb-acda-407d-bd69-2c4fd2943a41 | 2026-03-30 | 19m | 139 | 740KB | IMPLEMENTATION | no | (metadata-only: 16 Bash, 13 Grep, 10 Read; "study fix_plan" template) |
| d89a5b00-fb58-4328-b060-af5807d95268 | 2026-03-30 | 20m | 200 | 943KB | IMPLEMENTATION | no | (metadata-only: 20 Bash, 20 Grep, 21 Read; "study fix_plan" template) |
| 34996446-2abd-40b4-9c13-cd393cff753d | 2026-03-30 | 23m | 242 | 1.1MB | IMPLEMENTATION | no | (metadata-only: 19 Bash, 28 Grep, 28 Read; "study fix_plan" template) |
| 71abb8eb-7f2a-47e8-ae4b-46a09bc67d98 | 2026-03-30 | 68m | 515 | 2.0MB | IMPLEMENTATION | no | (metadata-only: 67 Read, 65 Grep, 34 Bash; "study fix_plan" template, large) |
| 02cd5a87-8347-4bf6-a8f9-de84b1347c75 | 2026-03-30 | 18m | 31 | 44KB | IMPLEMENTATION | no | (metadata-only: 6 Bash, 1 Edit, 1 Read; smaller implementation session) |
| 79afa3ff-6b54-4d61-9dbf-0d6ed84b4fe6 | 2026-03-30 | 80m | 376 | 1.6MB | IMPLEMENTATION | no | (metadata-only: 32 Bash, 64 Read, 31 Grep; "study fix_plan" template, large) |
| b3451018-8212-45d0-b21b-fc26c8ea5e42 | 2026-03-30 | 1m | 13 | 134KB | IMPLEMENTATION | no | "Study fix_plan" template; 1 Bash, 2 Read, large file context loaded |
| d1e7182b-3319-4cca-b030-647fa7953df8 | 2026-03-30 | 7m | 54 | 348KB | IMPLEMENTATION | no | "Study fix_plan" template, read-only (2 Agent, 4 Glob, 10 Read, no edits) |
| c64b58d6-40ce-42f6-8448-b547a29fd52d | 2026-03-30 | 14m | 210 | 977KB | IMPLEMENTATION | no | (metadata-only: 20 Bash, 17 Grep, 27 Read; "study fix_plan" template) |
| 0e54743d-e8cb-4409-92dd-c3a18c40c0f3 | 2026-03-31 | ~multi-day | 21 | 79KB | MIXED | yes | Human asks about ntm tool behavior, project path; WebFetch used; back-and-forth dialog |
| 45b2a315-6b8a-45a1-8ad2-ec45bd600f5c | 2026-03-31 | 6m | 30 | 61KB | IMPLEMENTATION | no | (metadata-only: 2 Bash, 1 Edit, 3 Read; small fix session) |
| d1704aa0-6003-4c17-99ed-d48f69937e5b | 2026-03-31 | 82m | 631 | 2.0MB | MIXED | yes | Gap analysis, bead creation, agent spawning; human-authored planning prompt driving implementation |
| 31635feb-6cfc-4827-b8f4-0b752be35fd1 | 2026-04-04 | 4m | 62 | 224KB | IMPLEMENTATION | no | (metadata-only: 18 Bash, 1 Edit, 2 Read; execution-heavy) |
| 5fcd6f06-6a56-4a05-80a8-41b827054e41 | 2026-04-04 | 5m | 107 | 282KB | IMPLEMENTATION | no | (metadata-only: 35 Bash, 2 Read; execution-heavy) |
| 33362ec6-a3b2-4912-b7fd-0787972cfeac | 2026-04-04 | 0m | 10 | 12KB | OTHER | no | Agent-mail MCP infrastructure test (pane 5 check-in) |
| 0de3aaac-1620-41b2-b61b-50f3cccf6a00 | 2026-04-04 | 1m | 20 | 25KB | OTHER | no | Agent-mail MCP infrastructure test (pane 6 check-in) |
| 51ab3ba6-fb9d-4814-81ae-3cccb744c7c9 | 2026-04-04 | 1m | 33 | 37KB | OTHER | no | Tool search and glob exploration; infrastructure probing |
| 49fd0d26-a238-4447-850d-9e1fa5bfba69 | 2026-04-04 | 1m | 17 | 20KB | OTHER | no | Glob + ToolSearch; infrastructure probing |
| 59f6e7a2-0f24-4f6a-bcc8-85818acbc2da | 2026-04-04 | 0m | 7 | 9KB | OTHER | no | (stub: ToolSearch-only, 2 calls) |
| 48ed4a7e-feb2-47e0-b8fe-019218fa7702 | 2026-04-04 | 0m | 3 | 2KB | OTHER | no | (stub: near-empty session) |
| 7b96776d-a17b-47f5-aefc-546de54f5aa5 | 2026-04-04 | 1m | 15 | 27KB | OTHER | no | Agent-mail registration + send; infrastructure test |
| 81655549-2261-4ac6-b1a8-5e1a08cfc1cf | 2026-04-04 | 0m | 10 | 15KB | OTHER | no | Agent-mail inbox fetch; infrastructure test |
| 943e7701-33d2-414c-b279-e4373efff5fa | 2026-04-04 | 0m | 11 | 19KB | OTHER | no | Agent-mail ensure_project + register; infrastructure test |
| 9afb37c0-9804-4bac-97a3-b6068a9b4978 | 2026-04-04 | 0m | 18 | 26KB | OTHER | no | Agent-mail list+register+send; infrastructure test |
| bc06b375-e22f-49a2-94e3-f23f13582070 | 2026-04-04 | 1m | 26 | 45KB | OTHER | no | Agent-mail registration; infrastructure test |
| 6f650921-89f8-4b5f-95ae-93d4e9a94b91 | 2026-04-04 | 0m | 5 | 12KB | OTHER | no | (stub: 1 Glob call only) |
| c6d1bd16-4262-4ad5-b848-d4baee9605fb | 2026-04-04 | 148m | 968 | 12.2MB | MIXED | yes | Spec-first workflow discussion, exploratory testing plan, in-memory/docker impl; human defines process |
| 79a42399-0ca2-4a57-bf19-f80307706dba | 2026-04-04 | ~5d | 857 | 2.0MB | MIXED | yes | Collaborative design of exploratory testing loop; explicit human framing of collaborative mode |
| c4a1c009-cf60-4f34-ab6b-bcb5d84b4ace | 2026-04-02 | 32m | 186 | 870KB | MIXED | yes | Gap analysis + 3-agent review (architect/critic/security); plan synthesis and bead creation |
| 0e1c0b21-82c0-4960-be2c-9c42374e412e | 2026-04-02 | 1m | 43 | 192KB | IMPLEMENTATION | no | (metadata-only: 6 Bash, 8 Read, 1 Edit; small session) |
| df4cddd1-33d1-4afc-a2a4-f48949865dd6 | 2026-04-02 | ~7d | 743 | 2.3MB | IMPLEMENTATION | no | (metadata-only: 19 Agent, 164 Bash, 38 Edit; large multi-day implementation session) |
| 78c43471-18db-4368-9979-7955d8f2e61f | 2026-04-02 | 4m | 10 | 27KB | OTHER | no | Claude Code v2.1.90 terminal history bug investigation (product issue, not secure-dev design) |
| b7eca5d2-22e9-43d7-9801-29e23bbb4e7b | 2026-04-09 | ~25h | 1119 | 2.9MB | IMPLEMENTATION | no | Controller agent coordinating workers via ntm; single directive, no planning dialog |
| e366868d-fdfe-4314-86f3-0f02131670d5 | 2026-04-09 | 0m | 3 | 2KB | OTHER | no | (stub: session init artifact) |
| f8f7470a-d5a1-4f8d-a862-6f2fdec19c2d | 2026-04-09 | 0m | 5 | 4KB | OTHER | no | (stub: near-empty session) |
| 1ad9ee01-c148-44f4-bca3-5d80fa11fb94 | 2026-03-31 | 0m | 3 | 2KB | OTHER | no | /exit stub (Developer path) |
| 13db2f37-8490-419f-a91d-3b1737e48c60 | 2026-04-02 | 0m | 3 | 2KB | OTHER | no | /exit stub (Developer path) |

## Emblematic candidates

1. **79a42399** (2026-04-04) — Explicit human statement of collaborative planning mode; back-and-forth on exploratory testing architecture, ntm usage, and agent-loop design. Best example of planning dialog in this corpus.

2. **c4a1c009** (2026-04-02) — Gap analysis orchestration with 3-agent review (architect/critic/security); multi-pass plan synthesis; the closest this corpus gets to structured planning protocol.

3. **c6d1bd16** (2026-04-04) — Human defines spec-first workflow process mid-session; session spans planning-to-execution transition; captures the handoff moment from design to implementation.

4. **d1704aa0** (2026-03-31) — Human-authored planning prompt with explicit step decomposition (study → identify gaps → create beads → spawn agents); shows human directing a structured workflow rather than issuing a template directive.

5. **0e54743d** (2026-03-31) — Short but genuine back-and-forth on tooling setup; human asks "why doesn't it work this way" and iterates on the answer; small but clean planning-dialog pattern.

## Questions for human

1. **Corpus thinness for planning:** Only ~4-5 of 133 sessions show genuine planning dialog. The secure-dev project was overwhelmingly in autonomous-implementation mode. Is this expected? Should Phase 1C extract these 4-5 sessions, or should we weight the corpus from another project (kerf/harmonik) that likely has more planning?

2. **Session templates as a finding:** The "study specs, never ask questions" template is itself a planning-protocol artifact — the human pre-solved the decision-delegation problem by making the agent fully autonomous. Should we treat this as a protocol variant to analyze (the "push-down all decisions" anti-pattern) or just exclude it?

3. **Controller sessions:** b7eca5d2 and c4a1c009 involve orchestrator agents spawning reviewer agents. This is a form of planning protocol (agent-mediated design review). Include in Phase 1C or out-of-scope?
