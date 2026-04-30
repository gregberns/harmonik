# BI Smoke-Load Findings

`findings-version: 0.1` — drafted 2026-04-27 after the first execution of decompose-to-tasks discipline v0.3 against live `br` v0.1.45.

> Companion: [discipline.md](discipline.md) v0.3, [bi-pilot.md](bi-pilot.md) v0.1.2. Read those first.

## 1. What loaded

The smoke load created **66 beads** in a project-local `.beads/` workspace at `/Users/gb/github/harmonik/.beads/` (prefix `bi`):

| Class | Count | Notes |
|---|---|---|
| Spec parent epic (`bi-85z`) | 1 | `--type epic`, status `draft` |
| First-class requirement beads | 40 | After BI-008+8a and BI-010+10a+10b coalesces |
| Step beads (BI-030 ×6, BI-031 ×5) | 11 | `--parent <umbrella-id>` |
| Sensor / invariant beads | 4 | `kind:invariant` label |
| Schema beads | 8 | Includes new `bi-schema.harmonik-write-status` per BI v0.4.1 |
| Error-taxonomy bead | 1 | `bi-error.taxonomy` |
| Test-infra bead | 1 | `bi-test.crash-harness` |
| **Total** | **66** | |

**Edges:** 56 of 61 attempted intra-spec `blocks` edges landed cleanly. 5 were rejected by Beads's cycle detector — each rejection surfaced a real pilot/discipline bug (see §3).

**Cross-spec edges deferred** (`em-*`, `pl-*`, `rc-*`, etc. targets do not exist yet — to be added during the all-10-specs cycle check phase).

**Verification queries:**
- `br ready --limit 0` → no ready issues (all 66 beads are `draft`, as intended).
- `br epic status` → `bi-85z` shows `0/54 children closed`. Step beads count under their umbrella, not under the epic.
- `br dep cycles` → `✓ No dependency cycles detected.`

## 2. Discipline-level findings (proposed v0.3 → v0.4 deltas)

### 2.1 ID format is *hierarchical-alphanumeric*; mnemonic IDs are author aids only

`br create` does NOT accept an `--id` flag. IDs are auto-assigned in the form `<prefix>-<base36-suffix>` for top-level issues and `<parent-id>.<n>` for `--parent`-linked issues. Concrete examples from this load:

- Epic: `bi-85z` (assigned by Beads at `br create --type epic`)
- First-class child: `bi-85z.1`, `bi-85z.2`, ..., `bi-85z.54`
- Step bead under umbrella `bi-85z.37`: `bi-85z.37.1`, ..., `bi-85z.37.6`

The discipline's mnemonic IDs (`bi-001`, `bi-030.s4`, `bi-schema.intent-log-entry`) are NOT what Beads creates. They are *plan-level names* used in the pilot doc; the load procedure must maintain a mnemonic→assigned-ID map at load time and translate when adding `--parent` references and `dep add` edges. Mnemonics survive only as label values (e.g., `req:BI-031`).

**Proposed v0.4 patch.** §2.10 (parent-child grouping) gains a paragraph:

> **Plan-level vs. assigned IDs.** The pilot doc's bead identifiers (`bi-001`, `bi-030.s4`, `bi-schema.bead-record`) are *mnemonic plan-level names* used in the discipline and pilot prose. Live Beads assigns its own IDs at create time in the form `<prefix>-<base36-suffix>` (top level) and `<parent-id>.<n>` (children). The load procedure MUST maintain a mnemonic→assigned-ID map; the actual `br dep add <citing> <prerequisite>` calls use assigned IDs, not mnemonics. The mnemonic is preserved only as the bead's `req:<XX-NNN>` label and (for non-req beads) in the bead title.

### 2.2 Beads's `parent-child` edge IS a dep edge — step beads must NOT add `blocks` to their umbrella

When a step bead is created with `--parent <umbrella-id>`, Beads materializes a `parent-child` dep edge that the cycle detector treats as a dependency. The pilot's per-step `blocks` edge from `bi-030.s1 → bi-030` (and similar) creates a **cycle** when added on top of the parent-child edge.

Cycle-detector output:
```
Error: Cycle detected in dependencies: bi-85z.37.1 -> bi-85z.37
Hint: Remove one dependency to break the cycle
```

**Proposed v0.4 patch.** §2.2 gains an explicit clause:

> **(F11) Step→umbrella edges are implicit.** When a step bead is created with `--parent <umbrella-id>`, Beads materializes the dep automatically. Step beads MUST NOT add an explicit `blocks` edge to their umbrella — Beads's cycle detector treats parent-child as a dep, so `step blocks-on umbrella` plus `umbrella → step` (parent-child) produces a cycle. Sequencing between step beads (e.g., `bi-030.s2 blocks-on bi-030.s1`) IS expressed via explicit `blocks` edges; only the step→umbrella edge is implicit.

### 2.3 Sensor and impl beads form an asymmetric edge — never bidirectional

Per discipline §2.5, the sensor `blocks-on` impl. The pilot also added the inverse (`bi-011 → bi-inv-001`, `bi-022 → bi-inv-003`), creating cycles. The inverse is **wrong direction** — impl never depends on the sensor that verifies it.

**Proposed v0.4 patch.** §2.5 final paragraph clarifies:

> **Sensor↔impl edges are one-way.** Sensor beads `blocks-on` impl beads. Impl beads do NOT block-on their sensors — implementation is independent of verification. If a pilot table has both directions, the impl→sensor entries are the bug; remove them.

### 2.4 Edges from inline cites only — bidirectional cites need disambiguation

The pilot had spurious `bi-004 ↔ bi-027` bidirectional edges. Looking at the BI source, *neither requirement actually inline-cites the other*: BI-004 cites HC §4.11 (the skill-injection mechanism); BI-027 cites CP §4.11 (the skill-declaration). The pilot author saw the conceptual relationship and emitted edges anyway.

**Proposed v0.4 patch.** §2.7 gains an emphasis paragraph:

> **Bidirectional inline cites are a smell.** If A inline-cites B AND B inline-cites A, the resulting edge graph would have a cycle. When this happens, surface to the discipline author: usually one of the cites is informational (could be in `> RATIONALE:` or `§9.3 co-references`) rather than a true dependency. Cycles are NOT acceptable; one or both cites must be reclassified or removed before the bead set loads.

### 2.5 Default priority is `P2`, not unset

Per `br create`, every bead is assigned priority `P2` unless `--priority` is passed. Discipline §2.9 says "no priority at MVH; operator concern" but in practice every bead carries P2.

**Proposed v0.4 patch.** §2.9 paragraph on priority:

> **Default priority.** Beads assigns priority `P2` (medium) by default. Discipline §2.9's "priority is unset at MVH" rule is implemented by *accepting Beads's default* (P2 for all beads); the discipline does NOT pass `--priority` at create time. Operators tune priority in the readiness workflow.

### 2.6 Workspace-prefix scheme is not yet decided

`br init --prefix <X>` accepts ONE prefix per workspace. For BI alone this works (`--prefix bi`). For the full corpus the discipline must decide:

- **(a)** One global prefix (`hk` or similar); spec scope lives only in the `spec:<spec-id>` label. IDs become `hk-1`...`hk-790`. Cross-spec dep cycle check trivial (one DB).
- **(b)** Per-spec workspaces (10 separate `.beads/` dirs); IDs are `bi-1`, `em-1`, etc. Cross-spec cycle check needs joining 10 DBs.
- **(c)** One workspace, one prefix per spec (NOT supported by `br init` — would require Beads-side change).

**Proposed v0.4 patch.** §2.10 / new §2.12 calls the decision:

> **Workspace prefix at corpus scale.** The corpus loads into ONE `.beads/` workspace (single SQLite DB) with prefix `hk` (harmonik). The 10 spec-parent epics get IDs like `hk-<suffix>` where the suffix is Beads-assigned. Spec scope is preserved by the `spec:<spec-id>` label on every bead. Per-spec workspaces are rejected because cycle detection across the corpus (§3.3) requires a single DB.

This is the recommended decision; if the user prefers per-spec DBs, §3.3 cycle-check needs a tooling change (multi-DB join).

### 2.7 Verification commands work as discipline §3 implied

The `br ready` (excludes `draft` natively), `br epic status`, `br dep cycles`, and `br list -l <label>` queries all work as the discipline assumed. F2's promotion of `draft` over `harmonik:parked` is validated.

## 3. Pilot-level findings (proposed v0.1.2 → v0.1.3 deltas)

### 3.1 Pilot tally arithmetic is off by 4-5

Pilot §7 says "36 first-class req beads + 11 step + 4 sensor + 7 schema + 1 taxonomy + 1 test-infra = 61." Actual counts from re-counting the §2/§3/§4/§6 tables:

- First-class req beads: **40** (not 36) — pilot §7 missed the count of 43 reqs minus 3 coalesced (008a, 010a, 010b) = 40, not 36.
- Step beads: 11 ✓
- Sensor: 4 ✓
- Schema: 7 (or 8 with the BI v0.4.1 `HarmonikWriteStatus` split — see §3.4)
- Taxonomy: 1 ✓
- Test-infra: 1 ✓

**Corrected total: 64–65** (depending on whether `bi-schema.harmonik-write-status` is added). Pilot §7 needs renumbering.

**Full-corpus extrapolation revisits.** With the new BI baseline of 64–65 beads, the multiplier is ~1.49× (was reported as 1.42×). Naïve extrapolation to 526 reqs: ~785 beads, range 650–950. Order of magnitude unchanged.

### 3.2 Pilot bug: spurious `bi-004 ↔ bi-027` bidirectional edges

Per discipline §2.7 (and the v0.4 emphasis in this doc §2.4), neither edge has an inline cite source.

**Pilot patch:** Remove `bi-004 → bi-027` from row `bi-004`'s blocks edges; remove `bi-027 → bi-004` from row `bi-027`'s blocks edges. Both should be empty for these specific cross-references.

### 3.3 Pilot bug: impl→sensor wrong-direction edges

Per discipline §2.5 (and §2.3 of this doc), sensors `blocks-on` impl, never the inverse. Pilot rows have:
- `bi-011 → bi-inv-001` (wrong direction; remove)
- `bi-022 → bi-inv-003` (wrong direction; remove)

The pilot's §3 rows for `bi-inv-001`, `bi-inv-002`, `bi-inv-003`, `bi-inv-004` already correctly emit `bi-inv-* → bi-NNN` edges; nothing to add there.

### 3.4 Pilot bug: step→umbrella edges duplicate parent-child

Per discipline §2.2 (and §2.2 of this doc), step beads do not add `blocks` to their umbrella. Pilot rows:
- `bi-030.s1` lists `blocks: bi-030 (umbrella)` — remove.
- `bi-031.s1` lists `blocks: bi-031` — remove.

Other step beads (`bi-030.s2`–`s6`, `bi-031.s2`–`s5`) correctly chain to their predecessor step (`bi-030.s2 blocks-on bi-030.s1`, etc.). Those are the right edges.

### 3.5 Pilot is stale on BI v0.4.1 (CoarseStatus / HarmonikWriteStatus split)

The pilot was last revised at v0.1.2 against BI v0.4.0. BI v0.4.1's §6.1 schema split (`CoarseStatus` for read, `HarmonikWriteStatus` for write) means:

- A new schema bead is needed: `bi-schema.harmonik-write-status` (5-value write subset).
- `bi-007` blocks-on BOTH `bi-schema.coarse-status` AND `bi-schema.harmonik-write-status`.
- `bi-010` (the write-surface bead) blocks-on `bi-schema.harmonik-write-status` (the write surface is the 5-value subset by definition).

The smoke load added the new schema bead and both edges; the pilot doc's §2/§4 tables don't mention them.

**Pilot patch v0.1.3:** Add `bi-schema.harmonik-write-status` to §4; update `bi-007` and `bi-010` rows in §2 to include the new edge.

## 4. Open questions surfaced

These are author-decidable but warrant discipline-author attention before the v0.4 reload:

- **OQ-DTT-load-1.** Workspace-prefix decision (§2.6 above). Recommended: one global prefix `hk`. Author confirms or chooses (a)/(b).
- **OQ-DTT-load-2.** Once §3.2–§3.5 pilot patches land, should the smoke load be torn down and re-run end-to-end against the cleaned pilot, OR should the existing `.beads/` be patched in place via `br dep remove` calls? Tear-down + re-run is simpler; in-place patching preserves the 66 created bead IDs (which would then be referenced from the eventually-loaded other 9 specs' cross-spec edges). At MVH, IDs are not yet load-bearing, so tear-down is the recommended choice.
- **OQ-DTT-load-3.** When AR/EM/EV/HC/CP/WM/PL/ON/RC pilots execute, do they each follow the BI pilot's structure (one `<spec>-pilot.md`)? Recommend: yes, to keep the discipline's per-spec output uniform.

## 5. State of `.beads/` after smoke load

- Workspace exists at `/Users/gb/github/harmonik/.beads/` with prefix `bi`, 66 issues, all status `draft`.
- 56 valid `blocks` edges + 65 implicit `parent-child` edges (every child to its parent).
- Three known-incorrect edges remain in the DB (per §3.2–§3.4): `bi-004 → bi-027` (spurious cycle root), `bi-011 → bi-inv-001` (wrong direction), `bi-022 → bi-inv-003` (wrong direction). The cycle-rejected edges (5 total) are NOT in the DB.
- `.beads/` is now in `.gitignore` (added 2026-04-27).
- Recommended action before v0.4 reload: `rm -rf .beads/`, then re-run with the patched discipline + pilot.

## 6. Process notes

- The smoke load took ~5 minutes of `br` invocations (66 creates + 65 status updates + 56 dep adds = ~190 subprocess calls). Each `br` call is ~30–80 ms; total wall time tractable for the full corpus at ~790 beads (~3 minutes per spec, ~30 minutes for all 10).
- The pilot artifacts (load script `/tmp/bi-load.sh`, edges script `/tmp/bi-edges.sh`, mnem→ID map `/tmp/bi-mnem-map.csv`) are useful templates for the remaining 9 spec pilots. They demonstrate: (a) zsh assoc-array alternative via CSV+awk; (b) `--silent` flag for ID capture; (c) tolerant edge addition with failure log.
- The biggest discipline gap was the **assumption that mnemonic IDs would be loadable**. Beads's auto-assigned IDs are a fundamental constraint that the discipline v0.3 missed; v0.4 needs to be explicit about the mnem→ID translation layer.

## 7. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-04-27 | 0.1 | foundation-author | Initial findings from BI smoke-load. 6 discipline patches proposed (v0.3 → v0.4) + 5 pilot patches proposed (v0.1.2 → v0.1.3). 3 open questions surfaced. |
