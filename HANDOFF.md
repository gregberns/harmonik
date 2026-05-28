<!-- PP-TRIAL:v2 2026-05-28 main — v70. Two tracks fully PLANNED + ready: SDLC workflow corpus (kerf sdlc-workflows) + Attractor/kilroy parity (kerf attractor-parity). Marquee multi-review proven LIVE. Implementation is the remaining bulk. Clean, pushed. -->

Roadmap: [ROADMAP.md](ROADMAP.md). Cross-project rules: `~/.claude/CLAUDE.md`. Orchestrator rules: [docs/orchestrator-rules.md](docs/orchestrator-rules.md). Known workarounds: [docs/known-workarounds.md](docs/known-workarounds.md).

ROLE: You are the orchestrator. Delegate substantively. Keep the main thread minimal.

# Where we are (v70, 2026-05-28)

**Main clean, all pushed.** Building on v69 (DOT execution proven live; 5 blockers fixed), this session ran **two big parallel tracks to completion of PLANNING**, and the remaining work is **implementation** (attended `harmonik run` batches). Everything is teed up: corpus persisted, ~38 beads filed+prioritized+dep-sequenced, two kerf works at `ready`, and every loop proven live.

## Proven live this session
- **DOT simple + complex execution** (v69): review-loop + non-agentic-intermediate topologies complete end-to-end.
- **Marquee multi-reviewer-consolidate** (`hk-3wbff`): `implement → review_correctness → review_design → consolidate → close` walked end-to-end with real agents. The 5-node multi-review cascade **works today**.
- **Fixture-landing loop** (`hk-o52fm.2` / commit `cd3e8f8`): `harmonik run` landed `specs/examples/implement-review-fix.dot` + a scenario test + README pin, agent-reviewer approved, merged + pushed. The remaining fixtures land the same way.

## Key finding (sharp): the marquee's VALUE needs per-node briefing
The multi-review STRUCTURE runs today, but node `role` is parsed into `UnknownAttrs["role"]` and **never read** into the agent brief (`dot_cascade.go` assembles from bead title/body/extraContext only). So in the smoke all reviewers ran identical generic reviews — no per-axis differentiation, no `reviews/reviewer-*.md` committed. Differentiated multi-axis review (the headline value) needs `hk-m5lmo` (cheap: surface `role`) and/or `hk-sdnzj` (full per-node `prompt`).

---

# Track 1 — SDLC workflow corpus (kerf `sdlc-workflows`, status `ready`/SQUARE)

**Corpus persisted:** [docs/sdlc-workflow-corpus.md](docs/sdlc-workflow-corpus.md) — **21 workflows (14 NOW / 5 SOON / 2 DEMO)**, all with drop-in DOT, covering the whole SDLC (planning, spec authoring, decomposition, implementation + multi-review-consolidate, debugging/triage incl. the kilroy sentry pipelines, code/security review + gates, testing, release/ops, refactoring, docs-sync) + 2 whole-SDLC demo arcs.

**Beads:** epic **`hk-o52fm`** + 21 workflow beads `hk-o52fm.1`–`.21` (NOW/DEMO=P1, SOON=P2; deps set). `hk-o52fm.2` (W-IRF) is DONE.

**Next (implement the NOW fixtures via `harmonik run`, one bead each):** each lands `specs/examples/<name>.dot` + a scenario test under `internal/workflow/scenario/` + a README pin. **Serialize** — every fixture edits `specs/examples/README.md` (concurrent runs conflict). Enrich each bead's description first (point at `docs/sdlc-workflow-corpus.md §<name>` + the W-IRF commit `cd3e8f8` as the template). Remaining NOW: `hk-o52fm.1` (dual-review-consolidate — fixture; note its VALUE needs hk-m5lmo/sdnzj), `.3` plan-review-loop, `.4` security-review-loop, `.5` triple-review-consolidate, `.6` two-reviewer-consensus, `.7` plan-review-finalize, `.8` spec-R1-R2, `.9` spec-citation-cleanup, `.10` decompose-review-load, `.11` dependency-cycle-fix, `.12` docs-sync, `.13` review-route-by-failure-class, `.14` characterize-refactor-verify. DEMO: `.15` plan-to-shipped-now (dep `.1`), `.16` plan-to-shipped-faithful (SOON). SOON (blocked on capability beads): `.17`–`.21`.

# Track 2 — Attractor/kilroy parity (kerf `attractor-parity`, status `ready`)

The capability cluster that lets harmonik run faithful kilroy/Attractor pipelines (the `/Users/gb/github-qwick/qwick-ai/pipelines/{sentry-triage,sentry-bugfix}` DOTs). Full kerf spec work done (problem-space→…→integration **APPROVED** by independent review; unified `SPEC.md` on the bench at `~/.kerf/projects/gregberns-harmonik/attractor-parity/`). Architecture verdict: **clean-add** — all additive (minor schema bump, Outcome envelope + cascade + v69 review-loop untouched). Parallel/join is OUT (deferred EM-059).

**Implementation DAG (all P1; via `harmonik run`):**
1. **T0 `hk-jyqxe` (FIRST, solo, gates all)** — land `SPEC.md` into the real specs: `workflow-graph.md` (new WG-039…WG-046 + merged WG-002 agentic/non-agentic rows + WG-031 reserved-set), `execution-model.md` (EM-058 split-row [keystone; verified strict superset of current code], EM-015d dot-carve-out, EM-012b tier-0 rewording, §6.1 RECORDs), `handler-contract.md` (HC-063 shell handler + in-process §4.2 note). *Normative spec change — consider a spec-text check-in with the user.*
2. **Wave 1 (concurrent after T0):** `hk-l8rpd` (tool/shell node — KEYSTONE: `dispatchDotToolNode` splits the non-agentic branch on `tool_command`, `/bin/sh -c` cwd=wtPath, `handler_ref="shell"`, exit→Outcome map, reuse `Node.Timeout`) ‖ `hk-55zv2` (graph `goal` + `__PARAM__` substitution).
3. **Wave 2 (SERIAL — all edit `dispatchDotAgenticNode`, will conflict if concurrent):** `hk-m5lmo` (surface `role`) → `hk-sdnzj` (inline `prompt`) → `hk-q8nqr` (per-node model/effort) → `hk-69asi` (non-committing, dot-mode only).
4. Tests (`hk-cucz6`/`qpbpc`/`156il` + `hk-mca0b`/`xp9j7`/`4bn9o`/`9ohjf`) gated on their impl beads; T7 sidecar `hk-9t892`; v2 follow-ups `hk-9j49t`/`gv5n5`/`1xzg3`/`tksed` (P3).

Once Wave-1/2 land, the 5 Track-1 SOON workflows + the faithful kilroy sentry pipelines become runnable.

# Files to open first
1. `docs/sdlc-workflow-corpus.md` (the 21-workflow corpus) + `specs/examples/implement-review-fix.dot` + `internal/workflow/scenario/implement_review_fix_test.go` (the landed-fixture template).
2. `~/.kerf/projects/gregberns-harmonik/attractor-parity/SPEC.md` + `07-tasks.md` (the parity spec + task DAG).
3. `internal/daemon/dot_cascade.go` (dispatch — where T1/T2/T3/T4 land) + `internal/handler/claudelaunchspec.go` (brief assembly — prompt/role/goal).

# Caveats / hygiene
- Pre-existing RED test `TestMergeToMain_NoWorkAgentMainAdvanced` (`hk-zhxqx`, the `hk-cwxow` noChange regression) — still open, unrelated to this work.
- Pre-existing dep cycle `hk-11xkn ↔ hk-iuaed` in unrelated beads (not introduced here).
- `/tmp/sdlc-corpus/` + `/tmp/smoke/` hold the working corpus + extracted fixtures (ephemeral; the durable copy is `docs/sdlc-workflow-corpus.md`).
- Accumulated git stashes + stale `.harmonik/worktrees/` from prior sessions — low-priority cleanup.

# Translations glossary
- **marquee** — the multi-reviewer-consolidate pattern (N reviewers → consolidate → loop to implementer until clean). Structure proven live (`hk-3wbff`); differentiated value needs `hk-m5lmo`/`hk-sdnzj`.
- **tool/shell node (`hk-l8rpd`)** — the keystone parity capability: a `non-agentic` node with `tool_command` that runs `/bin/sh -c` and maps exit code → Outcome. ~half of kilroy pipeline nodes need it.
- **Attractor model** — the adopted upstream graph-workflow engine (strongdm/attractor, kilroy's sibling); single-threaded cascade on the Outcome envelope; baked into EM-005/EM-041/WG-020.
- **T0 (`hk-jyqxe`)** — lands the reviewed parity SPEC into the real specs; gates the whole Track-2 build.

# No hard blockers. Standing directive: on /session-resume, CONTINUE. Next action: drive Track-2 **T0 (`hk-jyqxe`)** via `harmonik run` (consider a spec-text check-in since it edits normative specs), then Wave-1 (`hk-l8rpd` ‖ `hk-55zv2`); in parallel, land the Track-1 NOW fixtures serially via `harmonik run` (template: commit `cd3e8f8`).
