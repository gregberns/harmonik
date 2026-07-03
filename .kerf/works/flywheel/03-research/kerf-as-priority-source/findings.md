# Research/Design — Kerf as priority source

> Component: `kerf-as-priority-source`. Round-3. Source: sub-agent (sonnet), CLAUDE.md + live `kerf next`/`kerf triage` runs, 2026-05-30.

## TL;DR
- **`kerf next --format=json --only=bead`** = single read command for "what to dispatch next" — returns scored bead IDs with `work_codename` attached, feeds directly into `harmonik run --beads`. [SOURCED — live output confirmed]
- Epic-scoping at supervise-start = a `--epic <codename>` flag the agent applies as a **client-side filter on the JSON feed** (no kerf CLI flag for this; kerf scores across all works). [DESIGN]
- Agent's contract on kerf is strictly **read-only** with two exceptions: `kerf triage --ack` after acting on a triage cycle, and `kerf pin` to disambiguate multi-matched beads.

## Agent's kerf playbook
| Decision Point | Commands | Reaction | Status |
|---|---|---|---|
| Startup / orientation | `kerf map` → `kerf list` → `kerf show <active-epic>` | Load area topology, progress fractions, jig pass status. | SOURCED |
| Compose batch | `kerf next --format=json --only=bead --limit=N` | Parse `items[].bead_id`+`work_codename`. Pre-screen each: `git log --all --grep "Refs: <id>"`. Drop hits>0. Feed survivors to `harmonik run --beads`. | SOURCED |
| Epic filter | client-side: filter `items` where `work_codename == <epic>` | No kerf flag exists; agent applies it. Documents in cycle-start note. | DESIGN |
| After batch | (daemon closes beads on success — agent does NOT re-close) → `kerf triage --ack` | `--ack` advances drift baseline; once per batch, not per bead. | SOURCED |
| Drift check (each cycle) | `kerf triage --format=json --kind=untriaged,multi_matched,external_drift` | Untriaged → `kerf pin <codename> <id>` for clear matches, else note+defer; multi_matched → `kerf pin` to disambiguate; external_close/new → reconcile with `br show`; then `--ack`. If `--resolved` exit 2 repeats (no progress) → wake LLM for human-in-loop. | SOURCED (exit-code matrix) |
| Empty queue | `kerf triage --format=json` → `br ready --priority=1` | (1) drain triage; (2) check `kerf next` w/o `--only=bead` for `cleanup` items (surface to operator, don't execute); (3) `br ready` for high-priority unblocked; (4) check dependency unblocks. If all empty → IDLE. Do NOT manufacture work. | DESIGN (fallback chain) |
| Kerf flaky/broken | `kerf next` empty BUT 100+ untriaged; OR JSON parse fails | `note(kind=warning, "kerf next returned empty; falling back to br ready --priority=1")`. Use `br ready --priority=1 --format=json` for this cycle. Re-arm kerf next cycle. | DESIGN |
| Override (agent picks Y over ranked X) | dispatch Y | `note(kind=decision, refs=[X,Y], "kerf ranked X (score=N) first but dispatching Y because <reason>")`. X stays in feed next cycle. | DESIGN |
| Write boundary | `kerf work edit`/`archive`/`status <work> <next>` | **BLOCK.** Agent never mutates work-level config or advances jig passes. Exceptions: `kerf pin` (disambiguation) + `kerf triage --ack` (after acting). | SOURCED (CLAUDE.md + triage docs) |

## Epic representation — recommend option (a)
Three options: (a) `--epic <codename>` at supervise-start + client-side filter; (b) `.flywheel/goals.md` with `active_epic`; (c) kerf areas (too coarse). **Recommend (a)** with fallback to no-filter (full ranked feed) if `--epic` not supplied. Layer-2 prompt text: "Your priority source is `kerf next`. Filter to `work_codename='<epic>'`. If filtered empty, expand to full feed and log a note."

## Supervise-start layer-2 prompt block (14 lines — within Greg's 30-40 rule)
```
PRIORITY SOURCE: kerf (work: <epic-codename>)
Commands:
  Read queue:    kerf next --format=json --only=bead
  Filter to:     items where work_codename == "<epic-codename>"
  After batch:   kerf triage --ack
  Drift check:   kerf triage --format=json
  Orientation:   kerf show <epic-codename>
Rules:
  Never mutate kerf work config. Never advance jig passes.
  If filtered feed is empty, expand to full feed and log a note.
  If kerf next is broken, fall back to: br ready --priority=1 --format=json
```

## Beta-test caveats
Live data: 8 of 13 works have `unwired`/`empty` status. `--only=bead --format=json` filters these out (confirmed). Triage phantom-suggestions handled via `--resolved` exit-code loop; if exit 2 repeats (same drift, no progress), stop+surface to operator. Graceful degradation flag via `note(kind=warning)`.
