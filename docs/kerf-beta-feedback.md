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
