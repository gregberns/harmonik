# Pilot Loader Tooling

`tooling-version: 0.1` — first version. Last updated 2026-04-29 with HC pilot integration test.
Companion to `discipline.md` (decomposition rules) and `pilot-review-protocol.md` (review protocol).

This doc describes `scripts/load-pilot.py` and the `<spec>-pilot-data.yaml` schema it consumes.
The loader replaces the prior pattern of hand-writing a ~900-line Python load script per pilot
(`/tmp/load-bi.py`, `/tmp/load-em.py`, `/tmp/load-ev.py`, `/tmp/load-hc.py`), each of which had
duplicated br-shell-out boilerplate and embedded all bead data as Python literals.

Goals:
1. Cut the per-pilot load effort from ~900 lines of Python to ~300-1000 lines of YAML data.
2. Make resume-after-kill safe via the mnem-map CSV ledger.
3. Surface load-time findings (cycle rejections, missing cross-spec mnemonics, forward-deferred
   edges) uniformly across all pilots.

## When to use

Every pilot from this point forward. Existing pilots (BI, AR, EM, EV) are loaded; their data was
authored as Python literals and was not re-converted to YAML (out of scope; the data is in DB).
HC was the integration test (its prior partial-load was finished via the new tool, and its
data is now canonical at `docs/decompose-to-tasks/hc-pilot-data.yaml`).

For the remaining 5 pilots (CP, WM, PL, ON, RC), author the YAML data file directly
alongside the markdown pilot. The pilot markdown remains the human-readable narrative
(§1 spec under decomposition, §6 optional infra, §7 tally, §8 rough edges, §9 revision
history); the YAML is the loader-consumable data (per-req beads, per-invariant beads,
schemas, taxonomy bead, test-infra beads, all edges).

## YAML schema

```yaml
spec:
  prefix: hc                                  # required; matches mnem prefixes
  title: "Handler Contract spec — implementation"  # required
  spec_path: specs/handler-contract.md        # informative
  spec_version: "0.3.3"                       # informative
  pilot_version: "0.1.0"                      # informative

# Auto-applied to every non-epic bead in this file. Discipline §2.4 conventions:
# - spec:<spec-name> must match all beads
# - tag:mechanism (or tag:cognition) is per-spec posture
default_labels:
  - "spec:handler-contract"
  - "tag:mechanism"

# Spec-parent epic. Status set to draft after creation.
epic:
  mnem: hc                                    # required; matches spec.prefix
  title: "Handler Contract spec — implementation"
  description: |
    Implements specs/handler-contract.md v0.3.3 ...
  labels:
    - "kind:spec-parent"

# All non-epic beads. The loader expands per-bead labels from `kind` and other fields.
beads:
  # --- §4 requirement bead ---
  - mnem: hc-001
    kind: req
    req: HC-001                               # string OR list-of-strings (for §2.3 coalesces)
    title: "Handler is the Go interface defined in §6.1"
    description: |
      Per HC-001: every handler MUST implement ...

  # --- §2.3 coalesce ---
  - mnem: hc-007
    kind: req
    req: [HC-007, HC-007a, HC-007b]            # all three req:* labels added
    title: "..."
    description: |
      Per HC-007 + HC-007a + HC-007b ...
    extra_labels:
      - "axis:idempotency-non-idempotent"

  # --- §5 invariant bead ---
  - mnem: hc-inv-001
    kind: invariant
    req: HC-INV-001                            # string only (no coalesces in invariants)
    title: "Sensor: ..."
    description: |
      ...

  # --- §6 schema bead ---
  - mnem: hc-schema.handler
    kind: schema
    schema_kind: interface                     # REQUIRED; one of: interface | record | enum | schema
    title: "..."
    description: |
      ...

  # --- §8 error-taxonomy bead ---
  - mnem: hc-error.taxonomy
    kind: error-taxonomy
    title: "..."
    description: |
      ...

  # --- §10.4 / §10 test-infra bead ---
  - mnem: hc-test.launch-determinism
    kind: test-infra
    title: "..."
    description: |
      ...

  # --- workflow / meta bead (tasks-to-do-tasks; no req field required) ---
  - mnem: meta-cp-author-yaml
    kind: workflow
    title: "Author cp-pilot-data.yaml"
    description: |
      ...

# Edges. `from` is the citing bead (the one that depends on the prereq); `to` is the
# prerequisite. Convention from discipline §2.5: row tables list `<citing> -> <prerequisite>`,
# meaning the citing bead BLOCKS-ON the prerequisite.
#
# Mnemonic prefix tells the loader which spec-map to use:
#   - prefix matches `spec.prefix` → intra-spec, resolved via own map
#   - other lowercase prefix      → cross-spec, resolved via cross_specs[<prefix>]
#   - `forward:*`                 → forward-deferred (logged only, NOT loaded)
#
# `br dep add` returning a duplicate-edge error is treated as success (idempotent re-runs).
# `br dep add` returning a cycle error is logged as a rejection (load-time pilot finding).
edges:
  - {from: hc-001, to: hc-schema.handler}      # intra-spec
  - {from: hc-003, to: em-040}                 # cross-spec (em prefix → cross_specs.em)
  - {from: hc-007, to: forward:pl-001}         # forward-deferred (PL not yet drafted)
  - {from: hc-008, to: ev-events.outcome-emitted}  # cross-spec EV with dotted mnem

# Cross-spec mnem-map paths. CSV format (header + rows):
#     mnemonic,assigned_id,title
#     hc-001,hk-8i31.1,Handler is the Go interface defined in §6.1
# These maps are produced by prior pilot loads. The loader reads each as a string→string
# lookup table for cross-spec edge resolution.
cross_specs:
  ar: /tmp/ar-mnem-map.csv
  em: /tmp/em-mnem-map.csv
  ev: /tmp/ev-mnem-map.csv
```

### Label expansion rules

For each non-epic bead, the loader builds the label set:

1. Start with `default_labels`.
2. Append kind-specific labels:
   - `kind: req` → `req:<X>` for each X in `req` (single string or list).
   - `kind: invariant` → `req:<X>` + `kind:invariant`.
   - `kind: schema` → `kind:<schema_kind>` (`schema_kind` is REQUIRED for `kind: schema`).
   - `kind: error-taxonomy` → `kind:taxonomy` (Beads label-convention name; the YAML
     `error-taxonomy` is a more descriptive semantic name for authors and matches
     BI/EM/EV's `kind:taxonomy` label).
   - `kind: test-infra` → `kind:test-infra`.
   - `kind: workflow` → `kind:workflow` (no `req` field required; for tasks-to-do-tasks
     like authoring yaml, running reviews, backfilling cross-spec edges, etc.).
3. Append `extra_labels` (e.g., `axis:replay-safety-unsafe`).
4. De-dupe preserving order.

For the epic, labels = `default_labels` + `epic.labels` (de-duped, ordered). So
`spec:<spec-name>` and `tag:mechanism` apply to the epic too via `default_labels`;
`epic.labels` only needs the epic-specific additions like `kind:spec-parent`.

### Edge direction

The pilot row tables use the column header `blocks edges (citing bead → prerequisite)`.
"A → B" means "A blocks on B" — A depends on B. The loader emits:

    br dep add <citing-id> <prereq-id> -t blocks

which Beads interprets as "<citing> depends on <prereq>" — same semantics.

YAML edges:
- `{from: A, to: B}` is "A → B" in the pilot tables: A blocks on B.

## Loader CLI

```sh
# Standard run: load all beads not in own map, then load all edges.
python3 scripts/load-pilot.py docs/decompose-to-tasks/hc-pilot-data.yaml

# Dry-run: print intended br calls; do not execute.
python3 scripts/load-pilot.py <data.yaml> --dry-run

# Override mnem-map path (default: /tmp/<prefix>-mnem-map.csv).
python3 scripts/load-pilot.py <data.yaml> --map /tmp/hc-mnem-map.csv

# Edges-only (assume beads already loaded).
python3 scripts/load-pilot.py <data.yaml> --skip-beads

# Beads-only (skip edge processing).
python3 scripts/load-pilot.py <data.yaml> --skip-edges
```

Output (real or dry-run):
- counts at start (existing-in-map, target beads, target edges)
- per-batch progress (every 10th bead created)
- final summary: created/skipped beads; ok/forward-deferred/rejected edges (with reasons)
- br-call counts (create / update / dep)

## Resume protocol

The mnem-map CSV is the resume ledger:

    /tmp/<prefix>-mnem-map.csv          # default; --map overrides
    mnemonic,assigned_id,title
    hc,hk-8i31,Handler Contract spec — implementation
    hc-001,hk-8i31.1,Handler is the Go interface defined in §6.1
    ...

On startup, the loader reads this file. Any mnemonic listed is treated as already loaded
(skipped during creation). On every successful create, the new mnemonic→assigned_id row is
appended atomically (open-append-write).

### Known limitation: kill-mid-create race

If the load is killed between the `br create` call and the `append_map_row` call, the bead
exists in DB but not in the map. On next run, the loader sees the mnemonic missing from the
map and creates a duplicate.

**Mitigation:** before resuming a load that may have been killed, run the loader with
`--dry-run` and inspect the printed bead-creation calls. Then `br list -l spec:<spec> --status
draft` to see what's actually in DB. If counts disagree, identify the orphan via DB-Map
diff and `br delete <id> --force --reason "..."` it.

The HC load on 2026-04-29 surfaced this exact case: `hk-8i31.54` was created in the prior
run before the kill but never recorded; today's resume created `hk-8i31.55` for the same
mnemonic. Resolution: deleted `hk-8i31.54`. See `hc-load-findings.md` F-load-HC-1.

## Cross-spec edge resolution

When edge `to` mnemonic prefix differs from `spec.prefix`, the loader looks up
`cross_specs[<prefix>]` for the CSV path, reads it as a mnemonic→assigned_id map, and
resolves. Forward-deferred edges (`forward:*`) are logged-only.

If a cross-spec mnemonic is not present in the named CSV, the edge is recorded as a
rejection (with reason "cross-spec mnem ... not in <pfx> map") rather than failing the run.
This handles the case where the cross-spec pilot author shipped a mnemonic the spec doesn't
actually define — surfaces a real bug rather than crashing.

## Development guidelines for new pilots

When authoring a new `<spec>-pilot-data.yaml`:

1. Start with `spec:`, `default_labels:`, `epic:` blocks. Set `spec.prefix` to match the
   spec's discipline §2.12 prefix scheme (`hk` corpus-wide; mnemonic per-spec like `cp`,
   `wm`, etc.).
2. Author beads in groups: §4 reqs first, then §5 invariants, then §6 schemas (with
   `schema_kind`), then §8 taxonomy (single bead per discipline §2.11(c) unless 11+ rows),
   then test-infra beads.
3. For coalesces (`req: [X, Y, Z]`), document the coalesce decision in the description body
   per discipline §2.3.
4. Author edges: intra-spec first (matching `from` and `to` prefixes), then per-cross-spec
   groups, then forward-deferred. Order is preserved in load output, helpful for diffing.
5. Validate via `python3 scripts/load-pilot.py <data.yaml> --dry-run` — checks YAML
   structure, counts mnemonics, resolves cross-spec lookups against existing maps.
6. After review, run for real. Capture any rejections in a `<spec>-load-findings.md` file
   (analog to `bi-smoke-load-findings.md` and `hc-load-findings.md`).

## What the loader does NOT do

- **No markdown parsing.** The YAML is the data; markdown narrative remains decoupled.
  This is per NEXT_AGENT.md prior-session wisdom: "Don't write a markdown-parsing checker.
  Markdown tables are brittle to parse and Beads catches most structural bugs anyway."
- **No discipline validation.** The loader doesn't enforce discipline §2.X rules
  (granularity, coalesce justification, sensor↔impl direction, etc.). Those are caught by
  the 3-reviewer review protocol per `pilot-review-protocol.md` and by Beads's cycle
  detector at load time.
- **No DOT/spec generation.** This is purely a data-loading tool. Spec-text changes go
  through the normal review flow.

## Schema versioning

`tooling-version: 0.1` is the initial schema. If we extend (new bead `kind`, new edge type,
etc.), bump the version and add a `tooling-version:` field to data files for compatibility.

## Future extensions (not built)

- `--validate` mode that checks discipline rules (e.g., kind:invariant beads must be the
  target of at least one impl-blocks-on-sensor edge per §2.5; sensors must NOT block on
  reqs per F12).
- Bulk-import via `br create -f <file>.md` if a future Beads CLI version supports the right
  shape (currently the Beads markdown bulk-import format is undocumented and doesn't help
  with cross-spec edge resolution; YAML + custom loader remains simpler).
- Atomic create+map-append (would need transactions or a WAL pattern); current mitigation
  is the dry-run-before-resume protocol above.
