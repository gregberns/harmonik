<!-- PP-TRIAL:v2 2026-05-28 main — v71. Parallelization restructure DONE. 5 of 13 Track-1 fixtures landed early (premature daemon); 8 fixtures + README-consolidation + all of Track-2 remain. Spec-text check-in constraint LIFTED. Clean, pushed. START HERE = dispatch the remaining batches via harmonik run, attended. -->

Roadmap: [ROADMAP.md](ROADMAP.md). Cross-project rules: `~/.claude/CLAUDE.md`. Orchestrator rules: [docs/orchestrator-rules.md](docs/orchestrator-rules.md). Known workarounds: [docs/known-workarounds.md](docs/known-workarounds.md). Dispatch loop: skill `harmonik-dispatch` + AGENTS/CLAUDE.md §"Daily loop".

ROLE: You are the orchestrator. Delegate substantively. Keep the main thread minimal. **This session is PURE DISPATCH** — all design+planning is done. Do not investigate inline; if a bead fails twice, dispatch an investigator sub-agent.

# Where we are (v71, 2026-05-28)

**Main clean, HEAD == origin/main == `fe6fe96`, all pushed, build green, `go test ./internal/workflow/...` passes.** Two tracks are fully PLANNED (v70) and now the parallelization restructure is DONE. The job this session is **mechanical: drive the remaining `harmonik run` batches, attended.** The hard design/validation is finished.

## NEW since v70 — read this first
1. **Spec-text check-in constraint LIFTED (2026-05-28).** User: "you can change that — I don't need to see the changes." → The orchestrator may now land normative `specs/` edits (Track-2 **T0** especially) and push **without** showing the diff first. There are **no remaining per-action push gates** (force-push / shared-ref deletion still warrant a check-in per cross-project rules). Memory updated: `feedback_push_autonomy`.
2. **Track-1 parallelization restructure DONE.** All 13 NOW-fixture beads were re-enriched so they run CONCURRENTLY: each lands ONLY its unique `specs/examples/<name>.dot` + `internal/workflow/scenario/<name>_test.go` and **must NOT edit `specs/examples/README.md`**. A new consolidation bead **`hk-9w9y5`** ("Consolidate specs/examples/README.md: add subsections for all 13 NOW fixtures") owns ALL README pins in one commit and is dep-blocked by all 13 fixtures. Beads now cite the durable `docs/sdlc-workflow-corpus.md §<n>` (ephemeral `/tmp/sdlc-corpus/` references removed).
3. **5 of 13 fixtures already LANDED early** (see next section) — a premature daemon ran a partial wave during the v70→v71 handoff exchange. They are sound (pushed, tests green). 8 fixtures + the consolidation bead remain.

## What landed early (verify-then-continue, do NOT redo)
A background `harmonik run` I launched returned exit-1 ("queue locked by pid 30909") but the daemon it spawned kept running and landed a partial wave before dying. Verified: pushed, build OK, scenario tests pass. **5 fixtures CLOSED:**

| bead | fixture | commit |
|---|---|---|
| hk-o52fm.1 | dual-review-consolidate | `5efe2f2` |
| hk-o52fm.3 | plan-review-loop | `77e7064` |
| hk-o52fm.10 | decompose-review-load | `fe6fe96` |
| hk-o52fm.11 | dependency-cycle-fix-loop | `1ccefa3` |
| hk-o52fm.12 | docs-sync | `e7a7134` |

**Two things to check on these 5 before trusting the pattern blindly (cheap, git-only):**
- **No `Reviewed-By:`/`Review-Verdict:` trailers** were found on the 5 commits — the review-loop verdict didn't land as trailers (may not have run, or trailer-write skipped). Confirm whether agent-reviewer actually ran; if not, the remaining-fixture batch should ensure review-loop is on (it's default-on per hk-g0ckv, but verify).
- Each fixture also produced a **sidecar `specs/examples/<name>.md`** (e.g. `dual-review-consolidate.md`) and one produced `<name>.scenario.md` — NOT in the proven `cd3e8f8` template (which used README subsections, not per-fixture .md). This is a benign deviation (implementers documented in a sidecar since they were told not to touch README). **Decide:** does `hk-9w9y5` still add README subsections, or does it index the sidecar .md files instead? Recommend: README consolidation still adds the 6-step subsections (Purpose/Schema/Anchors/Test surface) and links each sidecar.

# Next actions (this is the whole job — all via `harmonik run`, attended)

## Batch 1 — remaining 8 Track-1 fixtures (concurrent) ‖ Track-2 T0 (concurrent)
Track-1 and Track-2 touch DISJOINT files → run both at once.

**Track-1 remaining fixtures (8, OPEN):** `hk-o52fm.4` security-review-loop, `.5` triple-review-consolidate, `.6` two-reviewer-consensus, `.7` plan-review-finalize, `.8` spec-R1-R2-cycle, `.9` spec-citation-cleanup, `.13` review-route-by-failure-class, `.14` characterize-refactor-verify. (`.5`/`.6` were gated on `.1`, now CLOSED → all 8 dispatchable.) They share NO files with each other anymore (README is off-limits) → safe to run wide.
```
harmonik run --beads hk-o52fm.4,hk-o52fm.5,hk-o52fm.6,hk-o52fm.7,hk-o52fm.8,hk-o52fm.9,hk-o52fm.13,hk-o52fm.14 --wave --max-concurrent 4 --notify-stream
```

**Track-2 T0 (`hk-jyqxe`, OPEN, spec-text landing, NO code):** lands attractor-parity `SPEC.md` into the 3 live specs (workflow-graph.md WG-039…046 + merged rows; execution-model.md EM-058 keystone etc.; handler-contract.md HC-063). Acceptance is grep-able (see `br show hk-jyqxe`). **Per the lifted constraint, just land + push — no diff review needed.** Run it solo or alongside Batch 1 (disjoint files). Source: `~/.kerf/projects/gregberns-harmonik/attractor-parity/SPEC.md`.
```
harmonik run --beads hk-jyqxe --notify-stream
```

## Batch 2 — after T0 lands: Track-2 Wave 1 (2-wide, concurrent)
`hk-l8rpd` (tool/shell node — KEYSTONE: `dispatchDotToolNode` splits non-agentic branch on `tool_command`, `/bin/sh -c`, exit→Outcome) ‖ `hk-55zv2` (graph `goal` + `__PARAM__` substitution). Both gated on T0.
```
harmonik run --beads hk-l8rpd,hk-55zv2 --wave --max-concurrent 2 --notify-stream
```

## Batch 3 — Track-2 Wave 2 (STRICTLY SERIAL — all edit `dispatchDotAgenticNode`)
Run ONE at a time, in order: `hk-m5lmo` (surface node `role` into brief) → `hk-sdnzj` (inline per-node `prompt`) → `hk-q8nqr` (per-node model/effort) → `hk-69asi` (non-committing dot-mode + reject `auto_status` with a helpful error). Do NOT `--max-concurrent >1` here — they will conflict.

## Batch 4 — README consolidation + tests
- `hk-9w9y5` — README consolidation — run ONLY after all 13 fixtures are CLOSED (it's dep-blocked, so the daemon won't start it early).
- Track-2 test beads (`hk-cucz6`/`qpbpc`/`156il`/`mca0b`/`xp9j7`/`4bn9o`/`9ohjf`), T7 sidecar `hk-9t892`, v2 follow-ups (`hk-9j49t`/`gv5n5`/`1xzg3`/`tksed`, P3) — gated on their impl beads.

# Dispatch discipline (per AGENTS.md — don't skip)
1. **Rebuild first:** `go install ./cmd/harmonik` (already fresh as of v71, but re-do at session start). 2. **Dispatch in background** with `--notify-stream`. 3. **Arm a Monitor** tailing the bash stdout file AND `.harmonik/events/events.jsonl` (pattern in AGENTS.md §"Canonical pattern") — without it you're blind from dispatch to batch-exit. 4. **CWD stays `/Users/gb/github/harmonik`** — never `cd` into a worktree (daemon removes them). 5. **On failure:** failed-once → re-dispatch next batch; failed-twice → STOP, dispatch an investigator sub-agent (do not re-dispatch). 6. Use `--wave` whenever `--max-concurrent > 1` (stream-mode HOL-blocks concurrent dispatch).

# Lesson from this session (avoid the repeat)
A `harmonik run` background launch returned exit-1 on a stale `queue.lock` but its spawned daemon kept running and landed work anyway. **Always check `.harmonik/events/events.jsonl` + `git log` after a "failed" harmonik launch — the daemon may have done work despite a CLI non-zero exit.** Before any launch: confirm no `queue.lock` + no live `pgrep -fl "harmonik run"`.

# Files to open first
1. `docs/sdlc-workflow-corpus.md` (the 21-workflow spec source) + a landed fixture as the live template: `specs/examples/dual-review-consolidate.dot` + `internal/workflow/scenario/dual_review_consolidate_test.go`.
2. `~/.kerf/projects/gregberns-harmonik/attractor-parity/SPEC.md` (parity spec) + `br show hk-jyqxe` (T0 acceptance).
3. `internal/daemon/dot_cascade.go` (where Track-2 Wave-1/2 land).

# Caveats / hygiene
- Pre-existing RED test `TestMergeToMain_NoWorkAgentMainAdvanced` (`hk-zhxqx`) — unrelated, still open.
- Pre-existing dep cycle `hk-11xkn ↔ hk-iuaed` — unrelated, not introduced here.
- `.beads/issues.jsonl` is committed clean; `kerf next` shows 166 untriaged / 104 external-drift — a `kerf triage --ack` pass is overdue but NOT blocking (low priority cleanup).
- `.claude/scheduled_tasks.lock` is untracked (harness artifact) — ignore.

# Translations glossary
- **fixture** — a `specs/examples/<name>.dot` workflow example + its scenario test; "landing" one = committing both.
- **consolidation bead (`hk-9w9y5`)** — the single bead that adds all 13 README subsections at the end, so concurrent fixture runs never collide on the shared `specs/examples/README.md`.
- **T0 (`hk-jyqxe`)** — Track-2 first task: writes the reviewed attractor-parity SPEC into the real `specs/`. Spec-text only, no code; gates all Track-2 code beads.
- **marquee** — the multi-reviewer-consolidate pattern (N reviewers → consolidate → loop). Structure proven live; differentiated per-axis value needs `hk-m5lmo`/`hk-sdnzj`.
- **tool/shell node (`hk-l8rpd`)** — KEYSTONE parity capability: a non-agentic node with `tool_command` that runs `/bin/sh -c` and maps exit code → Outcome.

# No hard blockers. Standing directive: on /session-resume, CONTINUE. Next action: rebuild harmonik, then dispatch **Batch 1** — the 8 remaining Track-1 fixtures (`--wave --max-concurrent 4`) AND Track-2 **T0 `hk-jyqxe`** (land + push the spec text directly, no diff review per lifted constraint) concurrently, each with a Monitor armed. Then Batch 2 (Wave-1) → Batch 3 (Wave-2 serial) → Batch 4 (consolidation + tests).
