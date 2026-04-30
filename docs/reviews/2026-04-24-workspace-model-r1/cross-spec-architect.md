# Round 1 Cross-Spec Architect Review — workspace-model.md v0.2.0

## Verdict summary

WM's scope boundary is cleanly drawn: the spec owns exactly what the task
prompt says it should (worktree lifecycle, branching convention, session-log
+ sidecar, merge semantics, lease-by-run), and its §2.2 ceding list pushes
the right things to EV, HC, RC, BI, and ON. The **depends-on list is wrong
in both directions**: the front matter declares only `[execution-model]`,
but the body cites architecture.md normatively (§4.1 axes, §4.9 centralized-
controller) and the §9.1 "Depends on" block itself lists architecture plus
seven foundation targets that never appear in the front matter. WM's
cross-spec citation hygiene is the real problem area: **every inter-spec
citation into event-model, handler-contract, control-points, operator-nfr,
process-lifecycle, reconciliation, and beads-integration is in the
"bootstrap `[docs/foundation/components.md §<N>]`" form**, yet all five
round-1-reviewed peers are now live and the remaining five drafts have
stable §4.x anchors. §9.3 co-references are also miscategorized: multiple
entries are either real depends-on (Run shape from EM §6.1) or stale
component-label refs (`§5.3a`, `§9.5`). Finally, WM is the target of a
**corpus-wide §5.x citation bug**: five other specs cite WM at `§5.1, §5.3,
§5.3a, §5.4, §5.5, §5.8, §5.9` — anchors that do NOT exist in WM v0.2 (WM
§5 is Invariants). WM itself is blameless here, but WM's own revision
history should acknowledge the reverse-drift so the round-1 integration
pass migrates it.

## 1. Dependency graph correctness

### 1.1 Front matter (lines 14–16)

```
depends-on:
  - execution-model
```

**Missing entries.** The body cites normatively:

- `architecture.md §4.1` — four-axis classification; §9.1 line 508 lists
  this as a depends-on. Not in front matter.
- `architecture.md §4.9` — centralized-controller principle; §9.1 line 509
  and WM-010 line 129, WM-INV-001 line 343, and A.3 rationale line 599 cite
  it. Not in front matter.
- `event-model.md §8.5` (currently cited as `[docs/foundation/components.md
  §3.2]`) — WM's workspace_* events are EV-owned payloads; §9.1 line 510
  lists this as a depends-on (via the stale cite). Not in front matter.
- `handler-contract.md` (currently cited as `[docs/foundation/components.md
  §4]`) — §9.1 line 511 and §4.6.WM-024 line 227 both cite it. Not in
  front matter.
- `operator-nfr.md §7.1` (currently cited as `[docs/foundation/components.md
  §7.3]`) — §9.1 line 512 and WM-038 / WM-040 (lines 322 / 335) cite it.
  Not in front matter.
- `beads-integration.md §4.3` (currently cited as `[docs/foundation/
  components.md §10.3]`) — §9.1 line 516 and WM-001 / WM-006 / WM-008 /
  WM-028 cite it. Not in front matter.
- `reconciliation.md §4.5 RC-020` (currently cited as `[docs/foundation/
  components.md §9.5]`) — §9.1 line 515 and WM-032, WM-034, WM-035,
  WM-036, WM-038, WM-040 cite it. Not in front matter.
- `process-lifecycle.md §4.2 PL-006` (currently cited as `[docs/foundation/
  components.md §8.2]`) — §9.1 line 514 and WM-033 cite it. Not in front
  matter.

**Cycle risk.** Adding `event-model` to `depends-on` risks a cycle (EV's
front matter already lists `workspace-model`, line 18 of event-model.md).
Per the components.md §Co-dependency rules — the same pattern EM/EV was
resolved under — the correct shape here is a directional split:

- WM *produces* events whose payload schemas EV owns → this is a
  consume-produce (bidirectional) relationship, not a pure forward dep.
- Resolution: WM declares `depends-on: [execution-model, architecture]`
  (EM and AR are genuine forward deps with no reverse edge); EV stays in
  §9.3 co-references with EV's §8.5 cited explicitly.

Analogous reasoning for `reconciliation`: RC's front-matter `depends-on`
(spec.md front matter in `reconciliation/spec.md`) would need to be
consulted, but RC-028 / RC-029 cite WM §5.9 / §5.1 / §5.3 / §5.8 in its
§9.3, not §9.1. So RC treats WM as co-reference (one-way read-from). WM
should treat RC symmetrically: keep RC in WM §9.3, NOT in `depends-on`.

Similarly for `beads-integration`: BI cites WM §5.3 / §5.8 as co-references
(lines 427–428 of beads-integration.md). Symmetric: WM keeps BI in §9.3
co-references, NOT in `depends-on`. However, WM's §4.2.WM-006 uses the
Beads parent-child edge as a **normative input** to branch selection — that
makes BI a genuine forward dep on this clause. Split: WM-006 cites BI §4.3
as depends-on; the §4.7.WM-028 correlation citation is co-reference.

**Minimum correct front matter:**

```yaml
depends-on:
  - architecture
  - execution-model
  - handler-contract
  - beads-integration
  - reconciliation
  - operator-nfr
  - process-lifecycle
```

With `event-model` in §9.3 (directional split resolution per EM/EV
precedent). Rationale: handler-contract HC-010 produces the
`session_log_location` event whose directory precondition WM owns (§4.7);
operator-nfr §4.3 and §7.1 drive `interrupt_state` transitions
(WM-038, WM-040); process-lifecycle §4.2 PL-006 is the mechanism behind
WM-033's orphan sweep; reconciliation §4.5 RC-020 provides the verdict
enum WM-034/035/036 switch on; beads-integration §4.3 provides the
parent-child edge WM-006/008 read.

### 1.2 §9.1 "Depends on" (lines 502–516)

§9.1 itself is *over-inclusive* relative to the front matter and carries
seven bootstrap-form citations that front matter omits. The §9.1 and
front-matter inconsistency is itself a template conformance bug (template
§0 says "list of spec-ids this spec normatively depends on" in front
matter; §9 is the narrative equivalent — they MUST agree).

**Stale bootstrap citations in §9.1.** All seven `[docs/foundation/
components.md §<N>]` lines (510–516) target content now owned by a reviewed
or drafted spec file. Each should migrate:

| WM §9.1 line | Current | Correct target |
|---|---|---|
| 510 | `components.md §3.2` | `event-model.md §8.5` (workspace lifecycle events) |
| 511 | `components.md §4` | `handler-contract.md §4.1, §4.2, §4.11` |
| 512 | `components.md §7.3` | `operator-nfr.md §4.3, §7.1` |
| 513 | `components.md §7.5` | `operator-nfr.md §4.5 ON-018` |
| 514 | `components.md §8.2` | `process-lifecycle.md §4.2 PL-006` |
| 515 | `components.md §9` | `reconciliation.md §4.5, §4.6` |
| 516 | `components.md §10.3` | `beads-integration.md §4.3 BI-006, §4.6` |

### 1.3 §9.2 "Reverse dependencies" (lines 518–520)

Template says "Populated at finalize" — the placeholder is conformant.
However, the task prompt names five known reverse consumers (S01, S04, S08,
reconciliation, and the reset-to-checkpoint verdict dispatcher). All five
are observable in the corpus as of 2026-04-24:

- `handler-contract.md` cites WM at six sites (HC-010, §2.2, §6.1
  `LaunchSpec.workspace_path`, §6.5 `session_log_location` emission-when,
  §9.3).
- `event-model.md` cites WM at three sites (§1 Purpose, EV-005 agent-
  internal detail, §6.5 emission-when map lines 820–821).
- `beads-integration.md` cites WM at four sites (BI-010 merge completion,
  BI-014 branching, BI-020 session log metadata, §9.3).
- `process-lifecycle.md` cites WM at four sites (§2.2 scope, PL-006
  worktree locks, §4.4 shutdown step 5, PL-INV-001).
- `operator-nfr.md` cites WM at six sites (ON-015 overlay schema, ON-024
  escape attempts, ON-027 drain step 6, ON-INV-003 sinks, §7.2 pseudocode,
  §9.1).
- `reconciliation/spec.md` cites WM at six sites (§1 Purpose, RC-015
  investigator inputs, RC-028 reopen-bead, §9.3, §8.4 Cat 3c detection,
  `schemas.md` verdict execution table).

The informative placeholder is compliant, but the round-1 integration
should populate it to make the reverse-drift (§1.4 below) diagnosable from
the WM side.

### 1.4 §9.3 "Co-references" (lines 522–526)

Three entries; quality mixed.

**Line 524 — `[execution-model.md §6.1 Run]`.** Miscategorized. The
`Workspace.run_id` field and `Run.input: WorkspaceRef` form a
**bidirectional consume-produce** relationship (WM consumes Run; EM
consumes WorkspaceRef). The EM spec's §9.3 already lists
`[workspace-model.md §5.1]` as a co-reference (lines 966–968 of
execution-model.md). The symmetric treatment is: keep this as co-reference
*and* promote it to depends-on. Template §9.3 rule says a pure read-from
(no forward dep) goes in §9.3; a bidirectional consume-produce resolved
directionally goes in both (depends-on §6.1 for the Run type; §9.3 for EM's
reverse consumption of WorkspaceRef). WM's front-matter `depends-on:
[execution-model]` already carries this; line 524 is redundant with
§9.1's line 504.

**Line 525 — `[docs/foundation/components.md §5.3a Session-log pipeline]`.**
Self-citation via the legacy label. `§5.3a` is the components.md anchor
*for this very spec's §4.7 material* (components.md §Component 5 lines 458–
466 = WM §4.7). Listing a self-reference as a co-reference is a category
error; the §5.3a material now lives here (WM §4.7) and in HC (`HC-010`
emission obligation) and in memory-layer (S08 ingestion, deferred spec).
Correct form: drop this line; replace with `[handler-contract.md §4.2
HC-010]` as co-reference (S04 emission side) and `[memory-layer.md
(deferred)]` as a bootstrap forward-ref for S08.

**Line 526 — `[docs/foundation/components.md §9.5 Verdict vocabulary]`.**
Bootstrap stale. The verdict enum now lives in `reconciliation/schemas.md
§6.1 Verdict` (and is referenced from reconciliation/spec.md §4.5 RC-020).
This is a genuine read-from relationship (WM-036's classification table
consumes the enum values). Migrate to `[reconciliation.md §4.5 RC-020]` or
`[reconciliation/schemas.md §6.1 Verdict]`.

**Missing from §9.3.** Several body-text read-from relationships are not
declared anywhere:

- `[handler-contract.md §4.2 HC-010]` — `session_log_location` emission
  rule (WM-025 and A.3 rationale).
- `[handler-contract.md §4.11]` — skill provisioning (implied by WM-016's
  emission ordering before HC-049 `skills_provisioned`).
- `[event-model.md §8.5]` — workspace-lifecycle event taxonomy (cited as
  `components.md §3.2` in WM-015, WM-039, §6.3).

## 2. Citation correctness — walk of every body citation

All citations in WM body/prose form `[<spec-id>.md §<N>]` or `[docs/
foundation/<file>.md §<N>]`. Exhaustive walk:

### 2.1 Valid current-form citations (target exists)

All citations using the current `[<spec-id>.md §<N>]` form target
architecture.md or execution-model.md. Every one resolves correctly
except one:

- `[architecture.md §4.1, §4.9]` at lines 129, 343, 508, 509, 599 — all
  resolve to live sections.
- `[execution-model.md §4.3, §4.4, §4.7, §4.10, §7.1]` at lines 23, 45,
  89, 97, 141, 175, 189, 241, 254, 274, 302, 349, 603 — all resolve.
- **Line 109 is broken.** `[execution-model.md §4.4.EM-023]` should be
  `[execution-model.md §4.5.EM-023]`. EM-023 is "one commit per
  successful durable transition", which lives at EM §4.5 "Checkpoint
  cadence" (line 272), not §4.4 "Checkpoint contract". Fix in v0.3.

### 2.2 Stale bootstrap citations — migrate to owning spec

Every `[docs/foundation/components.md §<N>]` citation in WM body text
(20 unique sites) is a bootstrap form that was correct in v0.1 but is now
stale. All five reviewed peers exist in `specs/`, and the five drafts
(including WM itself) carry stable §4.x anchors:

| Line | Current | Should be |
|---|---|---|
Migration by target spec (32 stale sites in WM body + §9):

- **→ handler-contract.md** (5): lines 34, 41, 227, 511, 555. Target
  anchor `§4.2 HC-010` for the S04 emission side, `§4.1` or `§4.11` for
  skill provisioning / Handler interface.
- **→ beads-integration.md** (6): lines 43, 69, 103, 115, 516, and the
  parent-child edge refs in WM-006. Target: `§4.2` (br CLI), `§4.3 BI-006`
  (typed dep edges), `§4.6 BI-017 / BI-018` (bead_id propagation).
- **→ reconciliation.md** (8): lines 44, 280, 295, 302, 322, 335, 515,
  526. Target: `§4.5 RC-020` (verdict enum) or `reconciliation/schemas.md
  §6.2` (verdict-execution table).
- **→ event-model.md** (6): lines 46, 161, 328, 436, 510, and the §6.3
  header on line 436. Target: `§8.5` (workspace lifecycle) or `§8.5.5`
  specifically for `workspace_interrupted`.
- **→ operator-nfr.md** (7): lines 47, 121, 322, 335, 448, 512, 513,
  556. Target: `§4.3 / §4.5 / §4.9 / §7.1` as appropriate.
- **→ process-lifecycle.md** (2): lines 286, 514. Target: `§4.2 PL-006`.
- **→ control-points.md** (1): line 569. Target: `§4.7 CP-037`
  (config-loading precedence).

**Summary**: ~32 sites need migration. Four remain legitimately bootstrap
(core-scope.md §3 at lines 48, 213, 601; components.md §5.6 at line 607
for post-MVH cleanup policy with no reviewed-spec home). One broken
current-form fix at line 109 (§4.4.EM-023 → §4.5.EM-023).

### 2.3 Reverse-drift: other specs cite WM at nonexistent §5.x anchors

Independent of WM's own hygiene, **seven other specs** in the corpus cite
WM at section numbers that do not exist in WM v0.2. WM §5 is
**Invariants** (`WM-INV-001` — `WM-INV-005`). The cited numbers map to
components.md §Component 5's legacy subsection numbering (§5.1 lease
rule, §5.2 state machine, §5.3 session-log, §5.3a pipeline, §5.4 merge,
§5.5 operator, §5.8 branching, §5.9 re-run) — which became WM §4.3, §4.4,
§4.7, §4.5, §4.10, §4.2, §4.9 after the lift from components.md.

Enumerated broken inbound cites (count per spec):

- **execution-model.md** — 12 cites at `§5.1 / §5.4 / §5.8 / §5.9`
  (lines 49, 70, 155, 167, 201, 205, 373, 377, 563, 592, 662, 966–968).
- **event-model.md** — 4 cites at `§5.2 / §5.3 / §5.4` (lines 26, 248,
  821, 906).
- **handler-contract.md** — 5 cites at `§5.1 / §5.3a` (lines 51, 163
  (also has correct §4.7 cite), 592, 652, 801).
- **beads-integration.md** — 5 cites at `§5.3 / §5.8` (lines 134, 164,
  205, 427, 428).
- **operator-nfr.md** — 6 cites at `§5.1 / §5.3` (lines 180, 242, 260,
  418, 595, 596).
- **process-lifecycle.md** — 5 cites at `§5.1 / §5.5` (lines 51, 125,
  179, 322, 419).
- **reconciliation/spec.md** — 8 cites at `§5.1 / §5.3 / §5.8 / §5.9`
  (lines 27, 157, 381, 512, 681–684).
- **reconciliation/schemas.md** — 2 cites at `§5.3 / §5.9` (lines 38,
  142).
- **architecture.md** — 1 cite at `§5.3a` (line 267); AR-039 line 341
  cites `components.md §5` directly.

Total: **~48 broken inbound citations** across 9 spec files. Only
control-points.md (lines 372, 1026, both citing WM §4.2) has migrated
correctly.

This drift is exactly the `§1.N / §3.N / §10.N` pattern the task prompt
warned about — WM is the target, not the emitter. WM v0.3 integration
cannot fix these (they live in other specs) but **MUST** publish a
canonical mapping so the coordinated corpus pass knows the targets.
Proposed §A.4 Migration Map:

| Legacy `§5.x` | WM v0.2 anchor |
|---|---|
| §5.1 (Lease rule) | §4.3 (Lease model) — OR §4.1 for the Workspace record itself |
| §5.2 (State machine) | §4.4 (Lifecycle states and events) + §7.1 (state machine table) |
| §5.3 (Session-log aggregation) | §4.7 (Session-log directory and metadata sidecar) |
| §5.3a (Session-log pipeline) | §4.7 (S06 side only; S04 side in HC §4.2.HC-010) |
| §5.4 (Merge semantics) | §4.5 (Merge back to integration) |
| §5.5 (Operator-control interaction) | §4.10 (Interrupt-state representation) |
| §5.6 (Cleanup) | §4.8 (Failed-run worktree persistence) — only covers part; full retention policy is post-MVH |
| §5.7 (Non-git artifacts) | §2.2 out-of-scope bullet |
| §5.8 (Branching model) | §4.2 (Branch naming) |
| §5.9 (Re-run rule) | §4.9 (Re-run rule) |

## 3. Scope leaks

### 3.1 `interrupt_state` and operator-nfr boundary

**Potential leak at WM-037 through WM-040 (lines 312–337).** The
`interrupt_state` enum enumerates `operator-paused`,
`operator-stopped-graceful`, `operator-stopped-immediate`,
`daemon-crash-suspected`. These names redeclare operator-control state
concepts that ON §7.1 (operator-control state machine at lines 465–477
of operator-nfr.md) already names (`pausing`, `paused`, `stopping`,
`stopped`, `operator_stopped.mode` in EV §8.7.8).

The *interrupt-state projection onto the workspace record* is legitimately
WM's own concern (orthogonal to the workspace lifecycle state per WM-037),
but the *enum values* are a hybrid of two ontologies. Clarifying move:
either (a) declare `interrupt_state` as an opaque marker carrying the
originating event's type (e.g., `origin_event: "operator_pausing"`) and
cite ON/RC as owners of the possible values, or (b) explicitly state that
WM redeclares this local vocabulary by choice and pin the join rule to
`operator_pause_status.mode` / `operator_stopped.mode` / Cat 6 detectors.
As currently written, a reader cannot tell whether WM's `operator-paused`
is identical to ON's `paused` state or a separate concept — and WM-038 has
two distinct owners (ON §7.3 and RC §9) driving the field.

### 3.2 WM-INV-003 "Git append-only semantics"

WM-INV-003 (line 353) restates EM-INV-004 (execution-model.md line 563
"atomic multi-subsystem undo") in local form. It's defensible as a
workspace-layer projection, but the invariant is really *cross-spec*: it
duplicates what EM already invariant-pins. The template §5 selection test
would likely demote this to a cross-reference note rather than a WM
invariant. Not a scope leak in the ownership sense, but worth flagging.

### 3.3 WM §6.2 line `${workspace_path}/.harmonik/lease.lock`

The lease.lock file is consumed by process-lifecycle.md PL-006 (line 125)
as the orphan-sweep target. WM §4.1 and §6.2 declare the file exists but
never normatively state its **format** or **contents**. PL-006 reads
`mtime` on the file. If WM owns the file, WM §6.2 should state the
mtime-is-authoritative contract AND declare whether the file carries a
payload. If the file is an opaque marker, §6.2 should say so. Currently
silent. Not a leak, but an under-specified co-owned artifact.

### 3.4 `workspace_merge_status` vs `workspace_merge_pending` +
`workspace_merged` split

**Material cross-spec disagreement.** WM-015 (line 161) and §6.3 (line
434) name `workspace_merge_pending` and `workspace_merged` as two separate
events. EV §8.5.3 (event-model.md line 136) declares a single
`workspace_merge_status` event with a `status ∈ {pending, merged}` payload
field. Since EV is "normative for shape", WM's two-event taxonomy
contradicts EV's one-event taxonomy. Either:
- EV §8.5 changes to split (two rows), OR
- WM-015 and §7.1 collapse to one `workspace_merge_status` emission with
  status flag.

This is the single most serious **semantic inconsistency** in the corpus.
A reader following the WM → EV chain sees different event names on each
side. Flag for coordinated round-1 integration.

### 3.5 `workspace_interrupted` emission owner

**Material cross-spec disagreement.** WM-039 (line 326) says the
workspace manager emits `workspace_interrupted` on `interrupt_state`
transition. EV §8.5.5 (event-model.md line 138) says the emitter is
"reconciliation detector" (and classifies as Cat 6). These are different
ownership claims for the same event.

Also, WM-039's payload declares `{workspace_id, run_id, prior lifecycle
state, new interrupt_state}`. EV §8.5.5 payload is `{workspace_id, run_id,
detected_at, category (Cat 6)}`. The shapes do not overlap.

Resolution path: per the §6.5 co-ownership rule, WM declares WHEN
(interrupt_state transition), EV declares WHAT (payload). One of the
following must hold:
- WM is the emitter and EV's §8.5.5 row is wrong (emitter should be
  "workspace-manager (S06)", and payload fields should match WM-039).
- Reconciliation is the emitter and WM-039 is wrong (WM should cede
  emission to RC; the interrupt_state field stays but `workspace_
  interrupted` emits only from the Cat 6 detector).

This is a round-1 coordinated issue. WM v0.3 should either flag with an
OQ or propose the resolution.

### 3.6 Out-of-scope list completeness (§2.2)

WM §2.2 (lines 40–48) is well-written but has two gaps:

- **No scope cede for `workspace_merge_status` / `workspace_interrupted`
  ownership disagreements** — per §3.4 and §3.5 above, these are
  unresolved.
- **No scope cede for `schema_version` semantics beyond N-1.** WM-INV-003
  and §6.4 assert schema discipline but operator-nfr.md §4.5 ON-018 owns
  the N-1 contract corpus-wide. §2.2 should name this cede.
- **No scope cede for `workspace_path` resolution.** HC-010 (handler-
  contract.md line 163) writes into the path, but HC's own `LaunchSpec`
  schema §6.1 line 592 declares `workspace_path` as field-owned by WM.
  This is fine, but §2.2 should say "LaunchSpec field composition"
  explicitly — it doesn't, and a reader could infer WM owns the full
  LaunchSpec.

## 4. Template-level front matter

### 4.1 Required front-matter fields (template §0)

Present: `title`, `spec-id`, `requirement-prefix`, `status`, `spec-shape`,
`version`, `spec-template-version`, `owner`, `last-updated`, `depends-on`.
All fields present and of correct type. Good.

### 4.2 Missing `spec-category` field

Architecture v0.3.0 (line 581 of architecture.md) introduces
`spec-category: foundation-cross-cutting` in its own front matter and
names AR-052 / AR-053 as the normative obligation. WM does NOT carry
this field. Per AR-052 "Spec category distinguishes runtime-subsystem
from foundation-cross-cutting" (architecture.md §4.0, line 78), WM must
declare its category. WM is arguably `runtime-subsystem` (it owns the
S06 workspace manager surface), but the spec also carries cross-cutting
invariants (lease-by-run is consumed by every subsystem). WM v0.3 should
add `spec-category: runtime-subsystem` to front matter OR argue for
`foundation-cross-cutting` explicitly.

### 4.3 `spec-template-version: 1.1` consistency

Declared `1.1`, which is the current template. Template §6.5 "Co-owned
event payloads" (template line 309) is a v1.1 addition. WM's §6.3
"Lifecycle event emission rules" serves that purpose but is numbered
§6.3 instead of §6.5. This is a numbering deviation, not a content gap;
the template §6 does not force specific numbering of sub-sections, but
the reserved slot `§6.5` is template-defined. Rename to §6.5 for
consistency with other reviewed specs (cf. process-lifecycle.md §6.2
"Co-owned event payloads" which also does not use §6.5; the convention
across the corpus is mixed). Flag for round-1 review, low priority.

### 4.4 `owner` field

Set to `foundation-author`, which matches every other draft spec. Good.

### 4.5 `depends-on` field

Already discussed at §1.1. Two-entry list disagreeing with §9.1 eight-
entry list.

## 5. Ownership conflicts

### 5.1 `session_log_location` — the §6.5 co-ownership test case

Task prompt asks: is the HC-010 emission + S06 directory stamp pipeline
coherent per §6.5 co-ownership rule?

**WM §4.7 says:** S06 pre-creates the directory and stamps the sidecar
before handler launch. Specifically:
- WM-025 (line 233): directory MUST exist at the canonical path.
- WM-026 (line 239): S06 MUST write `harmonik.meta.json` BEFORE the
  handler launches.
- WM-027 (line 246): metadata stamping MUST precede `workspace_leased`
  emission.
- WM-016 (line 166): `workspace_leased` is emitted AFTER metadata sidecar
  is written.

**HC-010 (handler-contract.md line 163) says:** "the handler subprocess
MUST emit a `session_log_location` progress-stream message early in the
session (after `handler_capabilities` and before `skills_provisioned` /
`agent_ready`) ... By the time this message is emitted, the session-log
directory and sidecar already exist per `[workspace-model.md §4.7]`;
**this message announces the path, it does not create the directory.**"

**Verdict: coherent, but under-declared in WM.** The two specs agree on
the mechanism (S06 creates directory + sidecar → S06 emits
`workspace_leased` → handler launches → handler emits `session_log_
location` announcing the path → S04 watcher translates the message to a
bus event). But WM currently:

- Cites `components.md §5.3a` at line 34 and line 525 — stale.
- Does NOT cite `handler-contract.md §4.2 HC-010` anywhere as a
  co-reference. The handler's emission obligation is a genuine read-from
  that WM's §6.3 event-emission rules rely on (WM-016 says the handler
  launch confirmation comes via `agent_started` — but the actual
  location-announcement event is `session_log_location`, and WM omits
  it from the §6.3 table altogether).

**Recommended fix in WM v0.3:**

1. Add `session_log_location` to §6.3 (renumber to §6.5) as a co-owned
   event: emit-when is in `handler-contract.md §4.2 HC-010`, payload
   shape is in `event-model.md §8.3.7`, WM's normative role is that the
   directory already exists per §4.7 when the handler emits.

2. Add `handler-contract.md §4.2 HC-010` to §9.3 co-references.

3. Drop the self-reference at §9.3 line 525 (`components.md §5.3a` is
   just a stale label for WM's own §4.7).

Per the §6.5 co-ownership rule — WM declares WHEN the directory exists,
HC declares WHEN the announcement fires, EV declares the payload shape —
the pipeline is a clean three-spec split with no double-ownership.

### 5.2 `workspace_merge_status` ownership

Per §3.4 above: WM says two events (`workspace_merge_pending` +
`workspace_merged`), EV declares one (`workspace_merge_status`). Ownership
conflict is in the *taxonomy*, not the payload. Needs coordinated
resolution; WM is not demonstrably wrong, but one of the two specs must
cede.

### 5.3 `workspace_interrupted` ownership

Per §3.5 above. WM-039 and EV §8.5.5 name different emitters. Needs
coordinated resolution.

### 5.4 Verdict dispatch shape for `reset-to-checkpoint`

Task prompt says reconciliation consumes WM's `reset-to-checkpoint`
verdict dispatch shape. WM-035 (line 300) declares that
`reset-to-checkpoint` "MUST keep the same worktree and the same task
branch" with the state reverting "via git operations inside the existing
worktree per `[execution-model.md §4.10.EM-044]`". This correctly:
- cedes the verdict enum to reconciliation,
- cedes the git-operation mechanics to execution-model,
- asserts WM's own claim (worktree and branch stay).

No leak. But the *dispatch-shape contract* is implicit rather than
declared. reconciliation/schemas.md §6.2 verdict-execution table (line
142) names the workspace-side action. WM §4.9 should cite this explicitly
as co-reference, not just reconciliation spec.md §4.5. Minor.

### 5.5 WM-039's payload vs EV-025's "each event has one owning spec"

EV-025 (event-model.md line 445) says each event type has exactly one
owning spec for payload shape. WM-039 (line 326) declares the
`workspace_interrupted` payload fields (`workspace_id`, `run_id`, prior
lifecycle state, new `interrupt_state`). This is a payload declaration,
which only EV may do per EV-025. WM violates EV-025 here.

Fix: WM-039 keeps the emission obligation but MUST NOT enumerate payload
fields; cite EV §8.5.5 for the payload. (This also exposes the EV row's
payload disagreement per §3.5.)

Same issue at WM-017 (line 173): WM declares payload fields for
`workspace_merged` (merged commit hash, surviving branch name) and
`workspace_discarded` (discarded branch name). These are payload
declarations that only EV may own. Move to EV or phrase as "WM §6.3
requires these fields be present in the EV-owned payload".

## 6. Bootstrap citations — migration scope

### 6.1 Remaining legitimate bootstrap citations

After the round-1 integration pass, these `components.md`-form citations
remain legitimate because the content has no reviewed-spec home:

- Line 213, 601: `[core-scope.md §3]` — not a foundation spec; correct.
- Line 48: `[core-scope.md §3]` — correct.
- Line 607: `[components.md §5.6]` cleanup-workflow — post-MVH, no spec
  home today. Keep bootstrap with OQ.
- Line 556: `[components.md §7.8]` — observability performance envelope.
  Maps to `operator-nfr.md §4.9 ON-034..ON-040`; migrate.

### 6.2 Citations that should migrate

Per §2.2 table above: 31 sites with stale bootstrap form. All have a
reviewed-or-drafted owning spec. Migration is mechanical.

### 6.3 WM revision history acknowledges prior migrations but not this one

WM v0.2 revision history (line 593) says: "Migrated legacy architecture.md
citation anchors ... §1.1→architecture.md §4.1 ..." and "Completed
AR-MIG-001 `handler_type` → `agent_type` rename". Good — but it does not
acknowledge:

- The 31 remaining `components.md §<N>`-form citations.
- The reverse-drift: other specs' §5.x citations to WM are broken.

WM v0.3 should add an integration-note in the revision history marking
both, with the §A.4 migration-map table if the review team decides to
publish it in-spec.

## 7. Specific recommended edits for WM v0.3

### 7.1 Front matter

```yaml
spec-category: runtime-subsystem
depends-on:
  - architecture
  - execution-model
  - handler-contract
  - beads-integration
  - reconciliation
  - operator-nfr
  - process-lifecycle
```

(`event-model` stays in §9.3 per EM/EV precedent.)

### 7.2 §9.1 "Depends on" — rewrite all 13 entries with correct anchors

Use the mapping table at §2.2 above. Each current stale bootstrap line
gets replaced.

### 7.3 §9.3 "Co-references" — rewrite

```
- [execution-model.md §6.1 Run] — WorkspaceRef co-dependency; EM consumes
  WorkspaceRef, WM consumes Run type.
- [event-model.md §8.5] — workspace lifecycle event payload shapes
  (directional-split resolution with EV per EM/EV precedent).
- [event-model.md §8.3.7] — session_log_location payload.
- [handler-contract.md §4.2 HC-010] — session_log_location emission-when
  from the S04 side.
- [handler-contract.md §4.11] — skill provisioning precedes handler
  launch; WM-016's emission ordering depends.
- [reconciliation.md §4.5 RC-020] — verdict enum consumed by §4.9 re-run
  rule.
- [reconciliation/schemas.md §6.2] — verdict-execution table names the
  WM-side action for each verdict.
- [operator-nfr.md §4.3 ON-013] — operator_resuming consumed by WM-040.
```

### 7.4 §6.3 → rename to §6.5 "Co-owned event payloads"

Match template §6.5 numbering. Add `session_log_location` row per §5.1
above.

### 7.5 Fix broken cite at line 109

`[execution-model.md §4.4.EM-023]` → `[execution-model.md §4.5.EM-023]`.

### 7.6 WM-017 and WM-039 payload declarations

Either drop the payload enumerations (cede to EV) or phrase them as
"the EV-owned payload per [event-model.md §8.5.x] MUST carry X, Y, Z".

### 7.7 Flag §3.4 / §3.5 with OQs

Add `OQ-WM-005` (workspace_merge_status vs split) and `OQ-WM-006`
(workspace_interrupted emitter identity) to §11.

### 7.8 Publish reverse-drift migration map

Add §A.4 Migration map (table from §2.3 above) so downstream specs
doing their own round-1 integration have a single canonical mapping.

### 7.9 §2.2 out-of-scope additions

- Event payload shapes — clarify that WM ONLY states emission-when; all
  field declarations live in EV (currently WM-017, WM-039 violate this).
- LaunchSpec composition — cede to HC §6.1.
- Cross-subsystem schema-version ceiling — cede to ON §4.5 ON-018.

## 8. Affirmations

1. **Lease-by-run is consistently asserted.** WM-010, WM-011, WM-012,
   WM-013, WM-INV-001, WM-INV-005, and A.3 rationale all converge on the
   same rule. Downstream specs (EM-012, RC-028, PL-006) consume it
   consistently.

2. **Three-level branching is clean.** WM-005 / WM-006 / WM-007 / WM-008
   walk through task → integration → main with deterministic Beads-edge-
   driven dispatch. EM §4.4 builds on it without contradiction.
   Beads-integration §4.3 / §4.6 supply the parent-child input correctly.

3. **Conflict-resolver ownership is correct.** WM-022 / WM-023 / WM-024
   and WM-INV-004 keep the *dispatch* (mechanism-tagged) with WM and the
   *reasoning* (cognition-tagged) with HC — exactly the §A.3 mechanism/
   cognition split architecture.md §4.2 requires. No double-ownership.

4. **Failed-run persistence invariant is well-placed.** WM-031 / WM-032
   / WM-033 cleanly separate what WM does (preserve, no auto-delete)
   from what PL does (orphan sweep removes stale locks only). Clean
   co-ownership with PL §4.2 PL-006.

5. **Interrupt-state orthogonality is a strong modeling move.** §A.3
   rationale ("why interrupt-state is orthogonal to lifecycle state")
   justifies WM-037's two-dimensional state shape. Avoids the N×M
   lifecycle-state explosion the naive approach produces.

6. **Re-run vs intra-run classification is fully deterministic.** WM-034
   / WM-035 / WM-036 give an enum-keyed dispatch table that RC-028 /
   RC-029 consume without ambiguity. No cognition participates, exactly
   as the task prompt requires.

7. **Canonical-path + registry-free design is structurally sound.** A.3
   rationale explicitly argues against a workspace registry and
   demonstrates that WM-002 + WM-004 + WM-013 give daemon-restart
   recovery without extra state. Clean.

8. **§10.3 excluded-conformance list is well-scoped.** Four exclusions
   (handler session-log format, CASS indexing, event payload shapes,
   harmonik workspace CLI surface) are each cleanly ceded with a pointer
   to the owning spec. Good hygiene.

## Summary of round-1 integration workload

1. **31 citation migrations** in WM body from `components.md §<N>` to
   owning-spec `§4.x` (mechanical).
2. **1 broken citation fix** (line 109 `§4.4.EM-023` → `§4.5.EM-023`).
3. **Front-matter expansion** to seven dependencies.
4. **§9.1 / §9.3 rewrite** with correct anchors.
5. **Two payload-ownership corrections** (WM-017, WM-039 cede payloads
   to EV per EV-025).
6. **Two new OQs** for `workspace_merge_status` and
   `workspace_interrupted` emitter conflicts with EV.
7. **`spec-category` field** addition per AR-052.
8. **Optional §A.4 Migration Map** to help the coordinated corpus
   round-1 pass fix the reverse-drift (§5.x citations in five other
   specs).

None of these are scope-boundary violations that would reject the spec
at round-1 review. The dependency-graph and citation hygiene are
recoverable without re-opening any normative decision.
