# Kerf Beta Feedback Log

Bootstrap dogfooding of new-kerf on harmonik repo. Started 2026-05-15.
Tester: Claude (Opus 4.7).
Goal: get `kerf next` returning a useful ranked feed; capture every friction point.

Severity legend: BLOCKER (cannot proceed) / MAJOR (workflow stalls or confuses) / MINOR (small UX gap) / NIT (cosmetic).

---

## Setup

**2026-05-15 — pre-init repo state**
- Branch main clean at dcd7f7e.
- 163 beads (triage later said 168) in `.beads/` already exist (pre-kerf).
- 4 pre-existing works on the bench: extqueue, bridge-integration, claude-hook-bridge, workflow-modes (all `ready`, files in `~/.kerf/projects/gregberns-harmonik/{codename}/`, NOT in repo).
- `.kerf/` in repo contains only `project-identifier` (text: `gregberns-harmonik`) plus four orphan dirs (`claude-hook-bridge`, `extqueue`, `recon`, `workflow-modes`) — the bench's authoritative copies live in `~/.kerf/projects/gregberns-harmonik/`. The two locations are out of sync (e.g. `bridge-integration` exists on bench but not in `.kerf/`; `recon` exists in `.kerf/` but not on bench). The agent has no obvious way to know which is canonical.
  - Severity: MAJOR. Two storage locations + visible orphans + no `kerf doctor` mode = confusion on first encounter. (Note: `kerf localize` exists per top-level help — agent should probably read its help next.)

**Pre-init command behavior (baseline)**

- `kerf next` (pre-init, 2026-05-15) →
  - Prints two warnings BEFORE the error: `untriaged_beads` (163 beads match no work) and `No project.yaml for 'gregberns-harmonik'`.
  - Errors: `no project.yaml — run 'kerf init'`.
  - Surprise: warnings already leak the inferred project ID `gregberns-harmonik` even though init hasn't run yet (kerf already reads `.kerf/project-identifier`).
  - Severity: MINOR-positive — actually a *good* affordance; agents can self-correct.
- `kerf triage` (pre-init) → terse `Error: project not initialized. Run 'kerf init' first.` No prior warnings. Inconsistent with `kerf next`.
  - Severity: MINOR. Triage could mirror next's "warnings before fatal" pattern.

---

## kerf init

**2026-05-15 12:36 — `kerf init --jig spec`**

Observed output (single command, all in one block, condensed):

1. Line 1: `Project already initialized: gregberns-harmonik` — claims it's already done.
2. Line 2: `Set default_jig: spec` — claims a default was set.
3. Line 3: `Detected: 100% of beads use 'kerf:*' labels.` — false. The beads in `.beads/issues.jsonl` use labels like `kind:`, `req:`, `spec:`, `subsystem:`, `axis:`, `tag:`, `codename:`, `phase:`, `scope:`, `cite:` — I see **zero** `kerf:*` labels in the sample I read. Detection logic appears broken or running on stale data.
4. Line 4: `Set project-wide bead_filter to 'kerf:{codename}'? [Y/n] Created project.yaml with 6 active jigs: ...`
   - Interactive prompt issued in a non-interactive context (Claude CLI session). No input was supplied. kerf appears to have proceeded as if defaulted-Y, but with no echoed confirmation, and the resulting project.yaml does NOT contain a `bead_filter` field. So either the default was N silently, or the field was supposed to land and didn't.
   - Severity: BLOCKER for unattended/agent use. An interactive y/N prompt during init is a sharp edge for any automation. Either honor `--yes`/`--no` flags or default safely without prompting.
5. Two distinct "AGENT SETUP INSTRUCTIONS" blocks are printed back-to-back. Their content overlaps but differs (different list of jigs, different commands, different headings). Confusing — which one is canonical? Looks like one is hard-coded from `kerf init` and the second is the output of `kerf setup`, but they aren't labeled as such.
   - Severity: MAJOR. Agent reads the output linearly and can't tell which instruction-set wins.
6. Neither instruction block mentions `kerf next`, `kerf triage`, `kerf pin`, `kerf map`, `kerf areas`, `kerf work edit` — exactly the new commands the project would adopt now. Agents following these instructions verbatim will not learn the new surface.
   - Severity: MAJOR. The instruction text is stale relative to the CLI.
7. The instruction block says: `ADD TO .gitignore (if not already present): .kerf/   But DO commit .kerf/project-identifier`. Telling the agent to gitignore-the-dir-but-commit-one-file inside it is a tricky pattern requiring `!.kerf/project-identifier` in .gitignore. The instruction doesn't spell that out.
   - Severity: MINOR. Pattern is documented poorly; agent will probably get it right but the instruction is ambiguous.

**Side effects of `kerf init` on the repo:**
- No new files created in cwd. (project.yaml lives in `~/.kerf/projects/gregberns-harmonik/`.)
- `.gitignore` not modified (instruction says agent should do it).
- `.beads/issues.jsonl` had pre-existing modifications unrelated to kerf init (verified by stash/pop).

---

## project.yaml

Path: `~/.kerf/projects/gregberns-harmonik/project.yaml` (NOT in repo).

Contents after `kerf init --jig spec`:

```yaml
jigs:
    - bug
    - implementation
    - plan
    - retrofit
    - spec
    - spike
passes:
    implementation:
        - Breakdown
        - Dispatch
        - Implement
        - Verify
        - Complete
```

**Issues:**

- No `default_jig: spec` field, despite init claiming it was set. — MAJOR.
- No `bead_filter` field at the project level, despite the interactive prompt suggesting one would be installed. — MAJOR (and the direct cause of the 168-untriaged-bead pile-up; no project-level filter means each work is on its own and the four existing works have no filter either).
- No `project.id` field. The identifier is in `.kerf/project-identifier` (text file) — fine, but means project.yaml is incomplete-looking. — MINOR.
- Only `implementation` has its passes recorded. The `spec` jig — declared the default by the init prompt — has no `passes` entry. Maybe passes live in a separate jig-definition file kerf reads from its own install dir, but a reader of this project.yaml has no way to know that. — MINOR.

**Contract implication for the agent:** the on-disk project.yaml is a thin manifest, not a full self-describing config. Agents must run `kerf show <codename>` or `kerf jig` to learn the real pass schedule. Worth surfacing in the new agent-instruction block.

---

## triage

`kerf triage` (post-init) →

- Header: `Triage for gregberns-harmonik (baseline: never):`
- Body: `Untriaged beads (168):` followed by 168 entries, each with id, status, title, label list, and a `suggest:` line.
- The suggest lines try to be helpful: `kerf work edit claude-hook-bridge --bead-filter-add 'label=codename:claude-hook-bridge'` (good — matches an existing work) vs. `kerf new idempotency-non-idempotent --bead-filter 'label=axis:idempotency-non-idempotent'` (suggests creating brand-new works named after axis labels — almost certainly wrong; the axis labels are cross-cutting tags, not codenames).
- For a bead with `labels: -` (none), suggestion is `kerf pin <codename> hk-6x7dw` — fine fallback, but `<codename>` is a literal placeholder, not a recommendation.

**Issues:**

- Bead count discrepancy: pre-init warning said 163, triage says 168. Probably the pre-init warning ran with a stricter filter (open only?) and triage shows all statuses. — MINOR. Document the difference, or unify.
- The "suggest" output mixes high-quality suggestions (existing-work attachment) with low-quality ones (new-work-per-axis-label). An agent following suggestions naively would create dozens of useless works. — MAJOR. The suggester should rank suggestions and clearly mark "attach-to-existing" as preferred when a codename label matches an existing work.
- No `--limit` or `--top` flag visible; the output is unbounded. 168 entries is fine but a 10,000-bead project would explode the agent's context. — MAJOR for scale.
- `baseline: never` is the only header signal that this is the first triage. After `--ack` is used, this presumably becomes a delta — that's a great affordance but undocumented in `kerf triage --help`. — MINOR.
- Suggestion language for axis labels (`kerf new idempotency-non-idempotent`) names the work after a label segment that contains a colon-replaced hyphen — not a valid intuition of what a codename should be. — MAJOR.

**What an agent would do next:** group untriaged beads by codename:* label, run `kerf work edit <codename> --bead-filter-add 'label=codename:<codename>'` for the four existing works, then triage what remains. The triage report doesn't quite spell this out, but the high-quality suggestions point that way.

---

## kerf next (post-init)

`kerf next` (post-init) →

```
warning: untriaged_beads — kerf triage — top untriaged prefix: 'req:'
         168 beads match no work via current filter and are not pinned

1. clean  workflow-modes   resolved bead_filter matches zero beads in the store
          edit spec.yaml bead_filter or check the project filter
2. clean  claude-hook-bridge   resolved bead_filter matches zero beads in the store
          edit spec.yaml bead_filter or check the project filter
3. clean  bridge-integration   resolved bead_filter matches zero beads in the store
          edit spec.yaml bead_filter or check the project filter
4. clean  extqueue   resolved bead_filter matches zero beads in the store
          edit spec.yaml bead_filter or check the project filter
```

**Issues:**

- Rank column reads `clean` for every entry — but the body says `resolved bead_filter matches zero beads`. "Clean" is the wrong label for a work whose filter resolves to nothing — should be `empty` or `unfiltered` or `needs-attention`. — MAJOR. Misleading status word.
- Zero bead items appear in the feed — only work-level diagnostics. The brief expected `kerf next` to return a *ranked feed of beads*. Currently it can't, because no work has a non-empty filter. Logical, but the agent needs to know that the path to "ranked-bead-feed" is "give at least one work a filter". — MAJOR for getting to MVP state.
- `kerf next --help` mentions `--kinds`, `--only`, `--include` for filtering kinds — but the kinds in the output (warnings, work-level diagnostics, beads) aren't enumerated anywhere. Agent doesn't know which kind to ask for. — MINOR.

---

## work-attachment

`kerf show workflow-modes` →

The `spec.yaml` for workflow-modes (at `~/.kerf/projects/gregberns-harmonik/workflow-modes/spec.yaml`) has fields:
`codename`, `type`, `project.id`, `jig`, `jig_version`, `status`, `status_values`, `created`, `updated`, `sessions`, `active_session`, `depends_on`, `implementation`. **No `bead_filter` field.**

**This is the root cause of "0 beads attach to 4 works":** none of the four pre-existing works was created with a bead filter, and the new `project-wide bead_filter` field is also absent. So every bead lands as untriaged.

The triage `suggest:` lines hint at the fix per work (e.g. `kerf work edit claude-hook-bridge --bead-filter-add 'label=codename:claude-hook-bridge'`), but the bootstrap UX has no command like `kerf bootstrap --infer-filters-from-labels` that would do this for every work in one shot. — MAJOR.

Also: `kerf show workflow-modes` does not display the (missing) bead_filter slot at all. A user can't tell from `kerf show` that the filter is the missing piece — they have to know to read `spec.yaml` directly. — MAJOR. `kerf show` should print `bead_filter: (none)` or similar so the gap is visible.

---

## pin

Not exercised — out of scope per beta brief.

---

## Surprises

1. **`kerf init` runs in non-interactive sessions and issues an interactive y/N prompt with no `--yes` escape hatch.** BLOCKER for agent automation.
2. **`kerf init` is partially idempotent.** It says "Project already initialized" (because `.kerf/project-identifier` exists) but then also says "Created project.yaml" — the agent can't tell what state changed.
3. **`project.yaml` lives in `~/.kerf/projects/{id}/`, not in the repo.** This is a global-bench architecture, but the agent-facing instructions don't say so. — MAJOR. Surface this explicitly. Mention `kerf localize` early in the init output.
4. **`Detected: 100% of beads use 'kerf:*' labels.`** is wrong on this repo — none of the visible beads use that prefix. The detector appears to be misfiring or running on a different store. — MAJOR.
5. **Two AGENT SETUP INSTRUCTIONS blocks** print back-to-back from a single `kerf init` invocation, with overlapping but inconsistent content. — MAJOR.
6. **Project.yaml shape does not match what init said it created** (missing default_jig, bead_filter, all-jig passes).  — BLOCKER for the beta goal.
7. **Triage's "suggest" output proposes `kerf new <axis-label-stem>` works** for cross-cutting tag labels — would produce dozens of phantom works if followed naively. — MAJOR.
8. **`kerf next` returns `clean` for works that resolve to zero beads.** Misleading word; status should be `empty` or `unfiltered`. — MAJOR.
9. **`.kerf/` directory in the repo is half-orphaned** (4 dirs locally vs. 4 different dirs on the bench, only some overlapping). No reconciliation tool surfaced. — MAJOR.
10. **Init's printed instructions don't mention any of the new commands** — `next`, `triage`, `pin`, `map`, `areas`, `work edit`. Agents that adopt these instructions never learn the modern surface. — MAJOR.

---

## Command-UX gaps

- `kerf init --yes` / `--no` flags to suppress interactive prompts. (Currently `--force` exists but it just re-runs init, doesn't answer prompts.)
- `kerf next --top N` / `--format=json` flag exists per `--help` but JSON shape isn't documented; agent has to discover by running.
- `kerf next --kinds` enumeration: list the valid kind values in `--help`.
- `kerf triage --top N` / `--group-by codename-label` to bound output.
- `kerf doctor` (or `kerf status --project`) — single command answering "is my project healthy" (project.yaml has all expected fields? .kerf/ in sync with bench? works have bead_filters?). The agent would run this first to know what to do.
- `kerf bootstrap-filters` — infer per-work `bead_filter` from `codename:` labels on existing beads in one pass. Would close the 168-bead untriaged gap for codename-tagged beads in a single command.
- `kerf show <codename>` should print `bead_filter: (none)` slot rather than omitting it silently.
- `kerf init` output should clearly label its two instruction blocks ("static" vs. "from `kerf setup`") and mention `kerf setup` as the regenerate-instructions command.
- The instruction block should include `kerf next` and `kerf triage` prominently — these are the daily-use commands now.

---

## 2026-05-15: kerf new + spec-pass-1 dogfood

Dogfood of `kerf new phase-3-dot --jig spec` plus pass-1 (problem-space) authoring + status advance. Mostly smooth — single-command setup worked end-to-end.

### What worked

1. **`kerf new <codename> --jig spec` ran clean** on first try. Output included the work path, the full 8-pass overview, and the pass-1 instructions inline. Agent did not need a second command to know what to do. — POSITIVE.
2. **`kerf show <codename>` is consistent with `kerf new`'s instructions.** Both surfaced the same pass-1 "what to do / what done looks like" block. No instruction drift. — POSITIVE.
3. **`kerf status <codename> decompose` advanced status and immediately printed pass-2 instructions.** One command, two useful outputs. — POSITIVE.

### Friction items

1. **No `kerf review <codename>` command for spawning a review sub-agent.** The pass-2 instruction block mentions "spawn a review sub-agent with: 02-components.md, 01-problem-space.md, ..." but `kerf` itself does not orchestrate this. Agent has to do it out-of-band (Agent/Task tool) — but in a sub-agent context, Agent is not always loaded by default, only via ToolSearch, and may not be available at all. Either kerf should ship a `kerf review` command that fires the canned review prompt, or the instruction block should acknowledge "use whatever review primitive your harness exposes; fresh-context re-read is an acceptable substitute." — MAJOR (blocks the pass-1 review gate when Agent tool isn't loaded).
2. **Pass-1 instructions do not enumerate the output file's section structure.** Pass 1 says "save to `01-problem-space.md`" with bullets on *what to cover* but no template / skeleton. An agent without prior kerf experience has to invent the structure. Two different agents will produce wildly different layouts. — MINOR. Consider shipping an `01-problem-space.md.template` alongside the jig spec.
3. **`kerf new` does not surface that `.kerf/{codename}/` on the bench (under `~/.kerf/projects/{id}/`) is the working directory, not anything in the repo.** The path is printed once but easy to miss. Agents writing files relative to the repo root would silently produce orphan files. CLAUDE.md mentions this but kerf's own output should reinforce it. — MINOR.

### Surprises

1. **`kerf show` lists `Files: spec.yaml` even before pass-1 artifact exists.** Reasonable — that's the jig metadata — but a new agent might mistake it for the pass-1 output filename. Consider grouping `Files:` by category (jig-meta vs. pass-output). — MINOR.

---

---

## 2026-05-15: work bead_filter bootstrap

Goal: unblock `kerf next` by attaching the 4 existing works to their codename-label cohorts.

### Commands run (in order)

```
kerf list                                      # 4 works, all ready
find ~/.kerf -name "*.yaml"                    # discovered ~/.kerf/projects/gregberns-harmonik/{work}/spec.yaml
                                                # NOTE: file is spec.yaml, NOT work.yaml as the task brief assumed — minor doc drift
br list --status=open --json                   # 168 issues
br list --status=open --json | jq '.issues[].labels[]?' | grep '^codename:' | sort | uniq -c
                                                # → 31 codename:claude-hook-bridge, 1 codename:imrest. THAT'S IT.
kerf triage                                    # baseline: never. 168 untriaged.
kerf next                                      # only warnings + 4 "clean: filter matches zero" — no ranked beads
kerf work edit claude-hook-bridge --bead-filter-add 'label=codename:claude-hook-bridge'  # +43 beads (open+closed)
kerf work edit extqueue           --bead-filter-add 'label=codename:extqueue'            # +0
kerf work edit bridge-integration --bead-filter-add 'label=codename:bridge-integration'  # +0
kerf work edit workflow-modes     --bead-filter-add 'label=codename:workflow-modes'      # +0
kerf triage                                    # untriaged 168 → 137
kerf next                                      # top 31 items are now ranked CHB beads — SUCCESS
kerf triage --ack                              # baseline advanced to 2026-05-15T20:52:18Z
```

### Outcomes

- `kerf next` ranked feed: **WORKING**. Top 5 IDs: `hk-7uasg`, `hk-pcgms`, `hk-cw56j`, `hk-s2vpx`, `hk-q7atz` — all `claude-hook-bridge`-attached integration / scenario beads.
- Triage counts: **untriaged 168 → 137**; multi_matched 0; external_drift 0.
- The 3 non-CHB works still attach 0 beads because **no bead in the corpus carries `codename:extqueue`, `codename:bridge-integration`, or `codename:workflow-modes`**. The task brief's premise ("168 untriaged collapse to ~4 codename:* cohorts") does not hold for this corpus — only one cohort exists.

### Friction items (this session)

1. **BLOCKER (premise mismatch).** Task brief assumed 4 codename:* cohorts map onto the 4 works. Actual corpus has only 1 codename cohort (`claude-hook-bridge`). `kerf next` is unblocked anyway because that one cohort produces 31 ranked beads — but 3 of the 4 work filters are decorative until future beads adopt the convention. The 137 still-untriaged beads need a *different* attachment strategy (probably `spec:reconciliation`, `scope:bootstrap`, etc.) or a new "reconciliation"/"bootstrap" work.
2. **MAJOR.** `kerf work edit` reports `Now matches: N beads (was: 0)`, but the displayed count for claude-hook-bridge was **43** while `br list --status=open` shows 31. The delta (12) is closed beads. `kerf work edit` should disambiguate `open / closed` to avoid the confusing 43-vs-31-vs-137 arithmetic. The follow-on `Per-work bead health` line in triage does disambiguate (`31 open / 12 closed`), so this is purely the edit-confirmation message.
3. **MAJOR.** Triage `suggest:` lines are *aggressively* wrong for cross-cutting labels. E.g. they propose `kerf new idempotency-non-idempotent --bead-filter 'label=axis:idempotency-non-idempotent'` — `axis:*` is a cross-cutting taxonomy, not a work cohort. Following these suggestions naively would create dozens of phantom works. The suggester should prefer `codename:` and `spec:` prefixes, and refuse to suggest `axis:`, `tag:`, `kind:`, `scope:` as new-work seeds.
4. **MAJOR.** Triage `suggest` for the 1-bead `codename:imrest` cohort says `kerf new imrest` even though `imrest` is archived (`~/.kerf/archive/kerf-explore-b/imrest/`). The suggester has no awareness of archive state. Should at least say "(archived — consider unarchive or re-pin)".
5. **MAJOR.** `kerf triage --ack` re-prints the **entire** triage report (including all 137 untriaged) before advancing baseline. Expected: terse confirmation `Baseline advanced to <timestamp>` only. Today, agents that pipe `--ack` output to logs get N×(137-line dump).
6. **MAJOR.** A 5th work `phase-3-dot` appeared in the per-work health table during this session that I did not create. Likely a parallel agent's work. There is no `kerf list` flag to show works created by *other agents* / sessions vs. the bench-owner. Confusing for multi-agent dogfooding.
7. **MAJOR (doc-drift).** Task brief said work configs live at `~/.kerf/projects/<id>/<work>/work.yaml`. Actual file is `spec.yaml`. The `--help` text on `kerf work edit` also says "edit a work's bead-attachment configuration" without naming the file — agent has to grep to find it.
8. **MAJOR.** `kerf show <codename>` still doesn't print `bead_filter:` line (replicates earlier finding). After `kerf work edit` succeeds, `kerf show` is the obvious next call to verify — but it's silent. Had to `cat` spec.yaml directly.
9. **MINOR.** `kerf work edit`'s "Now matches: N beads (was: 0 beads)." reads better as "Bead filter now matches N beads (open+closed). Previously: 0." — explicit about scope.
10. **MINOR.** No `kerf work show <codename>` to dump the bead_filter for a single work without parsing yaml. `kerf triage`'s Per-work-bead-health table is the workaround.
11. **NIT.** "Resolved bead_filter matches zero beads in the store" — the word `resolved` here means "after evaluation", but reads like "fixed". Rename to `evaluated`.
12. **NIT.** `kerf triage --kind=multi_matched` ignores the flag when there are zero of that kind and prints the full report header anyway. Confusing — should print "no multi_matched items" and exit.

### Not exercised

- `kerf pin` — no need yet; all 137 still-untriaged beads need cohort-level filters, not 1:1 pins.
- `kerf areas` / `kerf map` — out of scope.
- `kerf localize` — `.kerf/`-vs-bench reconciliation deferred.

### Next-session candidates

- Decide cohort strategy for the 137 still-untriaged beads. Strongest signals: `spec:reconciliation` (8), `scope:bootstrap`-without-codename (sizable). Likely needs a new `reconciliation` work + a `bootstrap` work, or relabel beads with `codename:*`. Latter is one-shot, former is more works.
- Pin or relabel the lone `codename:imrest` bead — archived work shouldn't leave orphans.

---

## 2026-05-15: kerf phase-3-dot pass-2 (decompose) dogfood

Pass-2 (decompose) of the `phase-3-dot` spec work. Following a clean pass-1 (`01-problem-space.md` already landed). Output: `02-components.md` (215 lines) + `decompose-review.md`. Status advanced `decompose -> research`.

### Friction items (this pass)

1. **MAJOR — reviewer-sub-agent path unavailable in deferred-tool harness.** The spec jig's pass-2 contract hard-requires "spawn a review sub-agent ... up to 3 review rounds, save findings to `decompose-review.md`." In this harness session the `Agent` tool is not in the deferred-tools list — it is genuinely absent, not merely lazy-loaded. Workaround: structured re-read against the explicit review-criteria checklist (documented as a limitation inside `decompose-review.md`). Kerf's instructions need a fallback path for harnesses without sub-agent dispatch — e.g. "single-agent reviewers must use a fresh context window and explicitly document the gap." Today the jig silently assumes Agent-tool availability.

2. **MAJOR — pass-2 instructions name the output as `02-components.md` but pass-1 landed as `01-problem-space.md` (full word "problem-space", not abbreviated).** The agent inferred consistency by emitting `02-components.md` literally, but the naming convention is `NN-<pass-name>.md` with the pass-name string drawn from the jig — and `kerf show` doesn't print the canonical filename anywhere for an agent to confirm. Recommend `kerf show <codename>` print `**Pass 2: Decompose (decompose)** Output: 02-components.md` with a copy-pasteable canonical name in a fixed location. (Today it does say "Output: 02-components.md" but only in mid-paragraph; easy to miss.)

3. **MINOR — review-criteria duplication.** The `kerf show` output prints the review-criteria checklist twice in slightly different framings (once in "What done looks like:" and once in "Review Criteria"). Agents have to read both to know whether they overlap or differ. Recommend a single normative source ("Done = reviewer APPROVE on these N criteria: ...") so the agent doesn't have to cross-check.

4. **MINOR — `kerf show` doesn't list which prior-pass files exist on the bench.** To find `01-problem-space.md` the agent must already know its path. `kerf show phase-3-dot` *does* list `Files: 01-problem-space.md spec.yaml` at the bottom, which is good, but the path (`~/.kerf/projects/.../phase-3-dot/`) is not printed alongside. Agent has to remember it.

5. **NIT — `kerf status <codename> <next>` transition prints the full next-pass instruction block.** Useful when continuing immediately; noisy when scripted (e.g. CI advancing status as part of a larger workflow). Recommend a `--quiet` flag for the script path.

### What worked well

- Pass-1 → pass-2 → status-advance flow was clean and natural. The dependency between passes is well-modeled (pass-2 reads pass-1's artifact by name).
- The review-criteria list in `kerf show` was exactly the right reviewer-instruction surface. When the Agent tool *is* available, this would directly drive a useful sub-agent prompt.
- Pass-2's "what done looks like" criteria mapped cleanly onto the audit-derived problem space — no impedance mismatch between the spec-jig's generic prompts and this work's concrete content.


---

## 2026-05-15: Phase-2 dogfood #2 — hk-cd92e (worktree task-injection leak)

Second Phase-2 harmonik-dispatch attempt. Target bead `hk-cd92e` (worktree
task-injection leak / agent-task.md to gitignored path). Run did not produce a
clean dogfood — bead's underlying fix is already shipped, and bead selection
went sideways. Closing the bead as already-fixed; capturing daemon-side friction
here.

### What ran

- Bumped hk-cd92e P3 → P0; `br ready --priority 0` showed it as sole P0 ready.
- Pushed commit `f786e1e` (priority bump).
- Built `/tmp/hk-dogfood2` from `./cmd/harmonik` (clean build, 8.6MB binary).
- Launched `/tmp/hk-dogfood2 --project /Users/gb/github/harmonik --max-concurrent 1` inside tmux session `harmonik` (PID 72208).
- Daemon ran ~50s before I SIGTERM'd it on confirming wrong-bead claim.
- Two commits landed on main during the daemon's brief life (78addb3, ca6026c) — appear to be recovered orphan work from previous crashed runs (13:17, 13:22).

### What I expected vs what happened

- **Expected:** Daemon claims hk-cd92e (sole P0 ready), spawns claude in worktree, claude observes the agent-task.md leak, fixes it, commits.
- **Happened:** Daemon claimed `hk-a0htu` (P1 IN_PROGRESS — different bead entirely). agent-task.md was already written to `.harmonik/agent-task.md` (gitignored path) — the fix hk-cd92e describes is already deployed in the daemon's worktree-bootstrap.

### Disposition

- hk-cd92e closed as already-fixed with evidence: `.harmonik/agent-task.md` path observed in run worktree `.harmonik/worktrees/019e2d6d-e8f7-772f-9469-24eda2eac0f7/`.
- hk-a0htu reset from in_progress → open (daemon killed mid-run).

### Friction items (NEW vs Track 1)

- **MAJOR — Stale-bead-by-priority selection bypass.** `br ready --priority 0` returned only hk-cd92e (P0), but daemon claimed hk-a0htu (P1, IN_PROGRESS, no P0 label). Suggests daemon resumes IN_PROGRESS beads (or has its own ranking) without consulting `br ready` priority ordering. Means **operators cannot reliably steer the daemon by raising bead priority** — the Phase-2 dogfood mechanism the brief uses (bump-P0 → expect-claim) is unreliable.
- **MAJOR — Orphan-sweep does not catch stale IN_PROGRESS.** `daemon_orphan_sweep_completed` event: `bead_in_progress_reset=0, locks_cleared=0, stale_intents_observed=4`. hk-a0htu was IN_PROGRESS from the 13:17 crashed run and was not reset. Daemon then re-picked it. The `stale_intents_observed=4` count is non-zero with no action taken.
- **MAJOR — `br close` 10s timeout fails runs that succeeded.** Visible in event log for runs 13:17 and 13:22: `run_failed: close-error: br subprocess wall-clock timeout (10s): brcli: Unavailable` AFTER `outcome_emitted` showed `approved`. This is the hk-yjsk8 bug observed live, twice in 12 minutes. The work landed (commits 78addb3 + ca6026c on main, attributed to "Claude Sonnet 4.6") but the runs were marked FAILED. **Run-state and reality diverge.**
- **MAJOR — Daemon dirties parent repo's working tree on launch.** Pre-run, `git status` in `/Users/gb/github/harmonik` showed `.gitignore` modified (added `.harmonik/agent-task*` and `.claude/settings.json`). I stashed it. The same diff reappeared in the run worktree's `.gitignore` — confirming the daemon's worktree-bootstrap (or some hook) writes that change without committing it. Friction-item is: **modification leaks into the parent repo's worktree under some condition.** The brief expected "Clean tree" — this trips operators who follow the brief literally.
- **MINOR — Tmux window orphaned on SIGTERM.** Killing the daemon with SIGTERM stopped the daemon process but left tmux window 2 (containing the live claude session) running. Had to `tmux kill-window` manually. Daemon shutdown is not propagating to child claude sessions / tmux windows.
- **NIT — No documented invocation in HANDOFF.md.** The canonical `hk --project <dir> --max-concurrent N` pattern is only documented inside a smoke-run dogfood doc (`dogfood-smoke-run-2026-05-15-bridge-substrate.md`). HANDOFF mentions the mechanism existence but not the exact invocation. Bead `hk-icecw` ("Add `harmonik run <bead-id>` subcommand") would address this entirely.

### Carry-over from Track 1 (still present)

- `br sync --flush-only` race / timeout (manifests now as the `br close` 10s timeout). Same family.
- No way to scope harmonik to a specific bead id (workaround: priority-bump didn't work this run — see MAJOR-1).

### Phase-2 dogfooding readiness verdict

**Not ready to scale.** Three specific blockers, in priority order:

1. **Bead-selection determinism** — operators must be able to specify exactly which bead a daemon will claim. Either `harmonik run <bead-id>` (hk-icecw) lands, or the daemon's claim path is documented and guaranteed to match `br ready --priority 0` ordering. Without this, the dogfood loop is non-deterministic.
2. **`br close` 10s timeout / hk-yjsk8** — every successful run currently mis-reports as `run_failed`. Until this is fixed, "did the run succeed?" requires inspecting commits, not the events stream. Severely undermines the value proposition of harmonik-as-dispatcher.
3. **Orphan-sweep coverage gap** — sweep observed 4 stale intents and reset 0 beads. Stale IN_PROGRESS rows accumulate and steer future runs. Bug PL-006 was supposed to fix this; it hasn't, at least not for the case observed here.

Items 2 and 3 are bug-level. Item 1 is a feature gap with an existing bead (hk-icecw). All three are tractable; none require redesign. Once they land, Phase-2 scale-up is plausible.

### Suggested next-session targets

- `hk-icecw` (P1) — `harmonik run <bead-id>` subcommand. Most-impactful unblock.
- `hk-yjsk8` (P1) — br close 10s timeout fix. Removes false-fail noise.
- Re-investigate `daemon_orphan_sweep` policy: `stale_intents_observed=4` with zero action is the smoking gun.

### Tracking beads

Filed 2026-05-15 from this dogfood run:

- `hk-rp48p` (P1, bug) — Daemon claim-path ignores priority order — claimed P1 IN_PROGRESS stale bead instead of P0 ready bead
- `hk-jvzc2` (P1, bug) — Daemon writes uncommitted .gitignore + .claude/settings.json edits into parent repo's working tree
- `hk-44w19` (P2, bug) — SIGTERM to harmonik daemon doesn't propagate to child claude/tmux windows
- `hk-sc3o4` (P1, bug) — Orphan-sweep: stale_intents_observed=4 but bead_in_progress_reset=0 — PL-006 gap

---

## 2026-05-15: phase-3-dot pass-3 (research) dogfood

Pass-3 `research` on the `phase-3-dot` kerf work, run via 5 parallel sub-agents (one per component C1–C5), then a finalizer pass to produce `03-research/SUMMARY.md` and advance to `change-design`.

### MAJOR-1 — Research-pass instructions assume sub-agent file-write availability

2 of 5 research sub-agents (R3 handler-contract-outcome, R4 control-points-binding) hit a harness rule that blocks `.md` writes for research-pass artifacts; both had to return their findings inline and the parent (orchestrator) persisted the file to `03-research/{component}/findings.md`. R1 / R2 / R5 wrote directly without issue.

The kerf `research` pass instructions (per `kerf show <work>`) tell the sub-agent to "Save findings per area to `03-research/{component}/findings.md`." That directive is unsafe-by-default across harnesses — Claude Code's sub-agent role has tooling guidance ("Subagents should return findings as text, not write report files") that overrides it. The finalizer hit the same block when trying to Write SUMMARY.md and had to fall back to a Bash heredoc, which silently dropped line wrapping (93 lines persisted vs. 200+ intended — content present, structure compressed).

**Recommended fix:** the kerf research-pass instruction template should explicitly tell the parent to **collect inline returns** and own the persistence step, not delegate the write to sub-agents. Alternatively, document the heredoc fallback as the recommended sub-agent path.

Severity: MAJOR. Workflow proceeded, but with a confusion tax (2 retries) and a content-fidelity hit on the finalizer's SUMMARY.md.

### MINOR-1 — `kerf status` doesn't surface "what does the next pass want"

`kerf status phase-3-dot change-design` advanced cleanly and dumped the full Pass-4 instructions. Good. But there is no `kerf preview <next-status>` to show the next-pass instructions *before* committing the advance — the finalizer had to either grep `kerf show` output or just advance and read. For multi-session works this is fine; for the finalizer dispatching to pass-4 immediately it's an extra round-trip.

### MINOR-2 — No cross-component "SUMMARY.md" slot in the pass-3 jig output template

The jig says "Save findings per area to `03-research/{component}/findings.md`." It does not anticipate a cross-cutting summary file. The finalizer chose `03-research/SUMMARY.md` as the natural location, but the convention is ad-hoc; pass-4 instructions don't reference it. Consider adding an optional `03-research/SUMMARY.md` to the jig contract so the cross-cutting decision matrix and "already-resolved" list has a canonical home.

### NIT-1 — `kerf show` output is long

`kerf show phase-3-dot` prints the full pass-3 instructions plus the file tree plus the session ledger plus the command palette. For a returning finalizer this is overkill — a `kerf show --compact` mode (just "status: research → next: change-design", file count, last-session marker) would be a faster context recovery.

---

## 2026-05-15: phase-3-dot pass-4 (change-design) — D3

Sub-agent dispatched to land design decision D3 (control-point framing — node-type vs. policy primitive vs. discriminant). Brief specified "one decision only"; other pass-4 design decisions parked for parallel agents. Output: `04-design/control-point-node-type-design.md` (~150 lines). Decision: Framing A (drop control-point from node-type catalog; 5→4 taxonomy; `gate` retained as the one Kind whose trigger maps to a node-shaped execution slot).

### MAJOR-1 — Reviewer-dispatch directive presumes Agent tool availability

Pass-4 brief said: "Dispatch a reviewer sub-agent via the Agent tool if available — fresh-context, general-purpose, foreground. If Agent tool unavailable, do a fresh-context re-read pass and state so explicitly in the design doc footer." The Agent tool was NOT available in the sub-agent's harness — only the deferred-tool surface (Monitor, EnterWorktree, etc.) was offered via ToolSearch. The fallback (self-re-read) was honored, but a self-re-read is structurally weaker than a fresh-context reviewer because the same agent's context is biased toward the decision it just made. Recommendation: the orchestrator (main thread) should run the reviewer pass after the sub-agent returns its draft, rather than asking the sub-agent to self-review or dispatch its own reviewer.

### MINOR-1 — `04-design/` directory not pre-created by `kerf status ... change-design`

The status advance from `research` → `change-design` did not create the `04-design/` directory; the sub-agent had to `mkdir -p` before its first Write. The pass-3 jig pre-creates the `03-research/{component}/` substructure when entering research; pass-4 could do the same with a `04-design/` empty dir to signal "this is where design decisions land."

### MINOR-2 — No convention for "one design decision per file" vs. monolithic design doc

Pass-4 ran D3 as a standalone file (`control-point-node-type-design.md`); pass-3 SUMMARY enumerated 20 decisions, several of which will land in parallel. The jig is silent on whether each decision is one file or a single `04-design/design.md` aggregates them. The per-decision-file convention is better for parallel sub-agents (no merge conflict) but ad-hoc. Worth declaring in the jig.

---

## 2026-05-18 — Re-trying kerf next with broken filters

Returning after several days of bead churn. `kerf next` opened with three works in `clean: bead_filter matches zero beads` state, plus an entry-friction wall of "175 untriaged beads · 70 beads changed externally" before any actionable item appeared. Goal: get the ranked feed useful again.

### Entry friction — drift wall before payload (MAJOR)

`kerf next` led with three lines of warnings (untriaged count + external_close + external_new) before showing the ranked feed. That is the **right information** but the **wrong placement** — when an agent is dispatched to "find the next bead," it has to skip past 70-bead drift noise and three `clean` rows to get to anything dispatchable. Suggestion: invert — show the top-N ranked items first, then a single one-line drift footer with the `kerf triage` hint. Drift is a hygiene task, not a routing answer.

### Broken filters — root cause: filter syntax convention mismatch (MAJOR)

Three of four broken works used the same wrong filter pattern. Inspecting `~/.kerf/projects/gregberns-harmonik/<codename>/spec.yaml`:

| Work | Old filter (broken) | Actual matching label in `.beads/` | New filter | Beads attached after fix |
|---|---|---|---|---|
| `bridge-integration` | `label: codename:bridge-integration` | `bridge-integration` (no prefix) | `label=bridge-integration` | **28** (2 open / 26 closed) |
| `extqueue` | `label: codename:extqueue` | `queue` | `label=queue` | **5** (4 open / 1 closed) |
| `phase-3-dot` | *(no `bead_filter` field at all)* | none — work is in `change-design` pass; no implementation beads exist yet | `label=codename:phase-3-dot` | **0** (forward-wired for upcoming beads) |
| `workflow-modes` | `label: codename:workflow-modes` | `codename:workflow-modes` | (unchanged — was correct) | 1 open / 0 closed |

The pattern: bead authors used **bare-label** convention (`bridge-integration`, `queue`) for some works and **prefixed-label** convention (`codename:claude-hook-bridge`, `codename:workflow-modes`) for others, while kerf works appear to have been seeded uniformly with the prefixed convention. There is no enforcement on either side. **Root cause is convention drift, not a kerf bug per se** — but kerf's `kerf init` / `kerf new` should probably (a) sample existing bead labels and suggest the most-likely-correct filter clause when creating a work, and (b) warn at `kerf next` time when a filter is `clean` *and* the codename matches a non-empty label that differs only in prefix (e.g. `codename:bridge-integration` filter + `bridge-integration` label both exist → suggest the swap inline).

### `kerf work edit` UX — accepts both `=` and `:` separators, returns count delta (NIT-positive)

`kerf work edit <codename> --bead-filter-add 'label=<value>' --bead-filter-remove 'label=<value>'` worked cleanly first try on all three fixes. The "`Now matches: N beads (was: M beads)`" feedback is excellent — instant confirmation. Good surface.

### `phase-3-dot` had no `bead_filter` field at all (MINOR)

`spec.yaml` for `phase-3-dot` was missing the `bead_filter:` key entirely; only `pinned_beads: []` was present. `kerf next` rendered this as `clean: bead_filter matches zero beads` — but the actual condition is "no filter declared." Worth a distinct status line: `unwired: no bead_filter declared` vs. `clean: bead_filter matches zero beads`. Different fixes (declare a filter vs. broaden a filter).

### `kerf triage --ack` advanced baseline cleanly

After fixes, `kerf triage --ack` advanced the baseline past 70-bead external drift. Next `kerf next` ran with only `! 169 untriaged beads` (the legitimate signal — most beads still match no work). Baseline ack is doing its job; `kerf next` should consider whether `untriaged beads` should also be footer-only, since it's a hygiene cue and not a routing one.

### Final `kerf next` top-10 (post-fix, 2026-05-18)

1. `hk-pwfhk` — queue-append: no in-memory mutation visible to running workloop (extqueue)
2. `hk-ug821` — workloop: completion path never persists queue mutations (extqueue)
3. `hk-1fubv` — queue-submit: HandlerAdapter discards mutated *Queue and events (extqueue)
4. `hk-a0htu` — br ready --format json does not surface labels; BI-013 unmet (bridge-integration)
5. `hk-7uasg` — Real-Claude end-to-end review-loop integration test (claude-hook-bridge)
6. `hk-go9k3` — queue events from handlers + workloop never emitted to event bus (extqueue)
7. `hk-ebcw2` — Codify bridge-integration GREEN smoke as runnable go test (bridge-integration)
8. `hk-j4lct` — dot-mode dispatch: silently falls through to single, no warning event (workflow-modes)
9. `hk-pcgms` — Relay-failure scenario: daemon socket missing → bridge_dial_failed (claude-hook-bridge)
10. `hk-ocisx` — docs/subsystems/agent-runner.md + hook-system.md docs amendment (claude-hook-bridge)

### Cross-references

- `hk-43ate` — pre-existing upstream bead on `clean`-status confusion. This session re-confirms its premise: `clean` conflates "filter is right but no matches yet" with "filter is wrong" with "no filter declared." Three different operator-actions needed; same surface message.
- Existing `kerf-upstream` corpus (14 beads, see label counter above) already covers `kerf-init`, `kerf-triage`, `kerf-show`, `kerf-work-edit` friction surfaces — the issues recorded here align with those buckets, so no new upstream beads created from this pass.

### Did not encounter

- Repetitive triage suggestions (task brief flagged this as a possible issue). After the filter fixes + `--ack`, triage output was tight and non-repetitive. May have been resolved by an intermediate kerf release, or may surface again with different drift shapes.


---

## 2026-05-18 — Queue realignment pass 2

Second realignment pass after `kerf next` drifted out of alignment with the user's stated objective ("define queue → hand to harmonik → merge → identify follow-up"). Three problems surfaced: keystone bead `hk-icecw` invisible, 9 handler-pause P1 beads unattached, `phase-3-dot` filter matching zero beads again.

### New works created

- **`handler-pause`** — `bead_filter: label=handler-pause` — matches 15 beads (9 P1 + 6 P2/P3).
- **`phase-2-completion`** — `bead_filter` with 3 `--bead-filter-add` clauses: `label=phase2-dogfood-friction`, `label=harmonik-cli`, `label=phase-1-acceptance`. Matches 15 beads (7 open / 8 closed). hk-icecw now surfaces at top-3 of `kerf next`.

### Kerf filter OR-semantics

`kerf work edit --bead-filter-add` can be **repeated**, and repeated clauses are evaluated as **OR** (verified by counting matches across the 3 separate labels above and confirming the union). The triage output renders this as `filter: any=[label=A, label=B, label=C]` — explicit and correct. **No upstream gap on OR-support.** The earlier hypothesis in this doc ("kerf filter syntax may lack OR") is resolved: OR is supported via repeated clauses; AND-semantics across heterogeneous keys would be the next question (not exercised here).

### phase-3-dot filter — not a regression

Earlier note in this doc said the phase-3-dot filter "was correct earlier, regressed." Re-checked today: the filter is `label=codename:phase-3-dot` (intent-correct, same value as `claude-hook-bridge` uses). The reason it matches zero is that **no beads with that label exist yet** — phase-3-dot is still in `change-design` status and its spec-amend / task beads haven't been spawned. The "regression" was a misdiagnosis; the filter has not changed. Leaving the filter as-is is correct: when phase-3-dot reaches `tasks` and emits its bead corpus, the filter will start matching automatically.

Surface-improvement candidate (already covered by hk-43ate): `kerf next`'s `clean: bead_filter matches zero beads` line should distinguish "filter intent-correct, beads not yet created" from "filter is wrong" — they have different remedies.

### Final `kerf next` top-10 (post-fix, 2026-05-18 evening)

1. `hk-sc3o4` — Orphan-sweep: stale_intents_observed=4 but bead_in_progress_reset=0 (phase-2-completion)
2. `hk-rp48p` — Daemon claim-path ignores priority order (phase-2-completion)
3. `hk-icecw` — Add `harmonik run <bead-id>` subcommand (phase-2-completion) ← **keystone restored**
4. `hk-7uasg` — Real-Claude end-to-end review-loop integration test (phase-2-completion)
5. `hk-lgtq2` — Ship Cat 3a auto-reconciler (phase-2-completion)
6. `hk-ajchp` — Daemon should idle/exit after target bead completes (phase-2-completion)
7. `hk-pcgms` — Relay-failure scenario: daemon socket missing → bridge_dial_failed (phase-2-completion)
8. `hk-ifqnj` — Handler-pause: event-model.md §8 events (handler-pause)
9. `hk-siuo2` — Handler-pause: QueueValidationReason 'handler_paused' (handler-pause)
10. `hk-39ryh` — Handler-pause: 'harmonik handler status' CLI (handler-pause)

The dispatch queue now reflects the operational objective: orphan-sweep + claim-priority bugs, the keystone `harmonik run` CLI, real-Claude integration, then the handler-pause subsystem.

### Multi-match warnings (info, not problem)

After realignment, 2 beads (hk-7uasg, hk-pcgms) match both `claude-hook-bridge` and `phase-2-completion` — expected, since `claude-hook-bridge` work is sealed (`status: ready`) but its `bead_filter` still actively claims its label-set. `kerf pin` is the documented remedy. Question for upstream: should `status: ready` works stop claiming new beads in triage by default? Captured as a comment, not a bead — low-priority observation.
