# BI Pilot — Decompose-to-Tasks Worked Example

`pilot-version: 0.1.3` — drafted 2026-04-25 against `specs/beads-integration.md` v0.4.1 (post-status-enum-reframe). v0.1.3 applies smoke-load corrections (F-pilot-1 through F-pilot-5) per [bi-smoke-load-findings.md](bi-smoke-load-findings.md) §3: §7 tally arithmetic, removal of three pilot-author edge bugs (`bi-004 ↔ bi-027` cycle, impl→sensor wrong-direction edges, step→umbrella implicit-via-parent-child redundancies), and the BI v0.4.1 schema split (added `bi-schema.harmonik-write-status` bead + edges from `bi-007` and `bi-010`). See §9 revision history.

> Companion: [discipline.md](discipline.md) v0.4. Read the discipline first; this file applies it.
>
> **Smoke-load status.** The `br` Beads CLI v0.1.45 is installed at `/Users/gb/.local/bin/br`. The first smoke load (2026-04-27, with `--prefix bi`) created 66 beads, surfaced 5 cycle-rejections (each a real pilot bug), and produced [bi-smoke-load-findings.md](bi-smoke-load-findings.md). v0.1.3 below reflects the corrected bead set; the next reload will use `--prefix hk` (corpus single-DB per discipline v0.4 §2.12).

## 1. Spec under decomposition

- **Spec:** `specs/beads-integration.md` (BI), v0.4.1, status `reviewed`. (v0.4.0 → v0.4.1 reframed BI-007 from "fixed 5-value enum" to a two-surface `CoarseStatus` (read) + `HarmonikWriteStatus` (5-value write subset) split — this pilot reflects v0.4.1.)
- **Counts:**
  - 43 §4 normative requirements (`BI-001` … `BI-032`, with letter suffixes `BI-008a`, `BI-010a`, `BI-010b`, `BI-014a`, `BI-024a`, `BI-025a`–`BI-025e`, `BI-031b`).
  - 4 §5 invariants (`BI-INV-001` … `BI-INV-004`).
  - 8 named §6 schema constructs (post-v0.4.1 split): `BeadRecord`, `CoarseStatus`, `HarmonikWriteStatus`, `DependencyEdge`, `EdgeKind`, `BrError`, `IntentLogEntry`, `TerminalOp`.
  - 1 §8 error-taxonomy table (`BrError` → reconciliation category).
  - 14 §11 open questions (NOT bead-loaded — open questions are design decisions per discipline §2.8 / Tag mapping table).
- **Front-matter `depends-on`:** architecture, execution-model, event-model, handler-contract, control-points, workspace-model, process-lifecycle, operator-nfr, reconciliation. (9 specs.)
- **Spec parent bead (mnemonic `bi`).** Loaded as `br create --type epic --status draft --title "Beads Integration spec — implementation" --description "Implements specs/beads-integration.md v0.4.1 (43 reqs, 4 invariants, 8 schemas, 1 error taxonomy)." --labels "spec:beads-integration,kind:spec-parent"`. Beads assigns the actual ID (e.g., `hk-85z` under corpus prefix `hk`); see discipline §2.10 mnem→assigned-ID rule.

---

## 2. Per-requirement task table

Columns: bead-id (proposed) · title (≤80 chars; imperative) · description (1-2 sentences citing spec ID) · tags · blocks edges (citing bead → prerequisite, loaded as `br dep add <citing> <prerequisite> -t blocks`) · notes.

Tags abbreviated: `mech` = `tag:mechanism`; `cog` = `tag:cognition`. Axes shown only when off-baseline. Every bead also implicitly carries `spec:beads-integration` and is `parent-child` of `bi` (omitted from table for terseness).

| bead-id | title | description | tags | blocks edges (citing bead → prerequisite) | notes |
|---|---|---|---|---|---|
| `bi-001` | Adopt Beads SQLite fork; reject Dolt variant | Per BI-001: declare `Dicklesworthstone/beads_rust` as the dependency; document the rejection of the Dolt fork; ensure no Beads source is forked into harmonik. | mech, `req:BI-001` | (none) | Manifest entry + ADR-style note. |
| `bi-002` | Route all Beads I/O through the `br` CLI | Per BI-002: enforce that no harmonik code path links Beads as a library or touches the SQLite file directly; lint check + adapter gate. | mech, `req:BI-002` | `bi-025` | Pairs with the §10.2 `os/exec` lint test. |
| `bi-003` | Forbid `br serve` (Beads MCP server) | Per BI-003: no `br serve` invocations anywhere; enforced by lint. | mech, `req:BI-003` | (none) | Trivial; close once lint lands. |
| `bi-004` | Daemon → `br` direct; agents → `br` via Beads-CLI skill | Per BI-004: daemon adapter invokes `br` as subprocess; agents access only via the Beads-CLI skill from HC §4.11; handler MUST NOT provision `br` outside skill path. | mech, `req:BI-004` | `bi-025`, cross: `hc-NNN-skill-injection`, `cp-NNN-skill-decl` | Cross-spec edges to HC §4.11 + CP §4.11 surfaces (specific HC/CP req IDs resolved during HC/CP pilot). v0.1.3: removed `bi-027` edge (no inline cite of BI-027 in BI-004's body; pilot-author cycle bug per discipline v0.4 §2.7). |
| `bi-005` | Beads is authoritative for bead content | Per BI-005: harmonik MUST NOT keep parallel authoritative copy of `title`/`description`/`type`; on cache disagreement during a §4.5 read, refresh from `br` first. | mech, `req:BI-005` | `bi-schema.bead-record`, `bi-013`, `bi-015` | Cache-refresh logic on read divergence. |
| `bi-006` | Beads owns typed dependency edges | Per BI-006: support edge kinds `parent-child`, `blocks`, `conditional-blocks`, `waits-for`; harmonik consumes read-only. | mech, `req:BI-006` | `bi-schema.dependency-edge`, `bi-schema.edge-kind` | |
| `bi-007` | Coarse status — read = `CoarseStatus`, write = `HarmonikWriteStatus` (5-value subset) | Per BI-007 (v0.4.1 reframe): write surface restricted to the 5-value `HarmonikWriteStatus` subset `{open, in_progress, closed, deferred, tombstone}`; read surface tolerates Beads's full `Status.enum` (8+ values, owned by Beads). | mech, `req:BI-007` | `bi-schema.coarse-status`, `bi-schema.harmonik-write-status` | v0.1.3: added `bi-schema.harmonik-write-status` edge per BI v0.4.1 §6.1 split. |
| `bi-008` | Bead IDs stable for bead lifetime | Per BI-008 + BI-008a (coalesced per discipline §2.3): adapter treats `bead_id` as opaque (no parsing/minting/rewriting); IDs are project-scoped per BI-008a. | mech, `req:BI-008`, `req:BI-008a` | (none) | Coalesced cluster. |
| `bi-009` | Beads provides atomic-claim semantics | Per BI-009: rely on Beads's claim atomicity; document and unit-test the dispatch invariant that two callers cannot both observe claimed-by-self. | mech, `req:BI-009` | `bi-schema.bead-record` | Includes the §10.2 atomic-claim contract test. |
| `bi-010` | Implement terminal-transition write surface (claim/close/reopen) | Per BI-010 + BI-010a + BI-010b (coalesced): the three-op write surface + run-event → coarse-status mapping table + carve-out for reconciliation-driven writes via BI-010b. The write surface IS `HarmonikWriteStatus` by definition (the 5-value subset). | mech, axis:idempotency-non-idempotent (none here — BI-010 is `idempotent`, BI-010b is unmarked), `req:BI-010`, `req:BI-010a`, `req:BI-010b` | `bi-schema.coarse-status`, `bi-schema.harmonik-write-status`, `bi-schema.terminal-op`, `bi-029`, `bi-030`, `bi-012`, cross: `em-NNN-failure-class-enum` (EM §8 canonical enum cited in BI-010a), cross: `wm-007` (cited), cross: `rc-020` (reopen-bead verdict) | Largest coalesce in the pilot. v0.1.3: added `bi-schema.harmonik-write-status` edge per BI v0.4.1 §6.1 split. |
| `bi-011` | Forbid intra-run Beads writes | Per BI-011: lint + structural check that no node-level transition or hook fire emits a `br` write. | mech, `req:BI-011` | `bi-010` | Enforced primarily by `bi-inv-001` sensor. v0.1.3: removed `bi-inv-001` edge (impl→sensor wrong direction per discipline v0.4 §2.5; sensor→impl edge already lives on `bi-inv-001`'s row). |
| `bi-012` | Route every terminal-transition write through the adapter | Per BI-012: a direct `br` invocation that bypasses the adapter for any state-change is a structural violation; lint check on `os/exec` callsites with `br` argv. | mech, `req:BI-012` | `bi-025`, `bi-029`, `bi-030`, `bi-031` | Companion to `bi-002`; this is the write-side gate. |
| `bi-013` | Implement `br ready` query (ready-work) | Per BI-013: adapter method `Ready() ([]BeadID, error)` invoking `br ready --format json`; consumed by daemon dispatch loop. | mech, `req:BI-013` | `bi-025`, `bi-025b`, `bi-025c` | |
| `bi-014` | Implement dependency-graph query | Per BI-014: adapter method returning typed-edge set for a bead (parents/children/blockers); consumed by branching + reconciliation. | mech, `req:BI-014` | `bi-025`, `bi-schema.dependency-edge` | |
| `bi-015` | Implement bead-detail query | Per BI-015: adapter method returning title/description/status/edges/audit-trail handle for a stable bead ID. | mech, `req:BI-015` | `bi-025`, `bi-schema.bead-record` | |
| `bi-016` | Implement reconciliation queries (audit log + status) | Per BI-016: read-only adapter methods exposing Beads's audit log and current status for in-flight beads; consumed by RC detectors + PL startup. | mech, `req:BI-016` | `bi-025`, cross: `pl-NNN-startup-step3-4` (PL §4.2 steps 3-4), cross: `rc-NNN-cat3a-detector` | |
| `bi-014a` | Orphan `br` subprocess sweep on daemon startup | Per BI-014a: enumerate `br` processes re-parented to init; SIGTERM 5s → SIGKILL; survivors are Cat 0 prereq failure. | mech, `req:BI-014a` | `bi-025c`, cross: `pl-006` (orphan sweep — extension request), cross: `rc-NNN-cat0` | OQ-BI-010 tracks the PL extension. |
| `bi-017` | Run metadata records `bead_id` | Per BI-017: extend the `Run` record's `bead_id` field handling; unset for non-bead-bound runs. | mech, `req:BI-017` | cross: `em-014` (Run record per EM §6.1) | Cross-spec edge into EM. |
| `bi-018` | Checkpoint trailer `Harmonik-Bead-ID` for bead-bound runs | Per BI-018: trailer present on every checkpoint commit of a bead-bound run; absent for non-bead-bound. | mech, `req:BI-018` | cross: `em-017` (trailer format per EM §6.2), `bi-017` | |
| `bi-019` | Bead-scoped event payloads carry `bead_id` | Per BI-019: emit `bead_id` on payloads of bead-bound run events; omit for daemon-lifecycle events. | mech, `req:BI-019` | cross: `ev-NNN-payload-shape` (EV §6.3), `bi-017` | |
| `bi-020` | Session logs for bead-bound runs carry `bead_id` metadata | Per BI-020: write `bead_id` into session-log sidecar metadata; CASS uses for join-to-Beads queries. | mech, `req:BI-020` | cross: `wm-NNN-session-log-sidecar` (WM §4.7), `bi-017` | |
| `bi-021` | Beads is authoritative for content + coarse status | Per BI-021: cache reconciles to Beads on disagreement; never the inverse. | mech, `req:BI-021` | `bi-005`, `bi-013`, `bi-015` | |
| `bi-022` | Git is authoritative for completion | Per BI-022: Beads `closed` without matching `Harmonik-Bead-ID` merge commit is Cat 3 reconciliation flag; never silently auto-reconciled in git's direction. | mech, axis:idempotency-non-idempotent, `req:BI-022` | cross: `rc-NNN-cat3`, `bi-018` | Off-baseline axis. v0.1.3: removed `bi-inv-003` edge (impl→sensor wrong direction per discipline v0.4 §2.5; sensor→impl edge already lives on `bi-inv-003`'s row). |
| `bi-023` | JSONL is observational only | Per BI-023: JSONL never overrides Beads or git; permitted as evidence source but never drives a `br` write outside §4.4. | mech, `req:BI-023` | `bi-010` | |
| `bi-024` | Pin Beads version per harmonik release; exact-match window at MVH | Per BI-024: harmonik release manifest names tested Beads version; compatibility window is exact-match at MVH; semver-range is OQ-BI-011. | mech, `req:BI-024` | (none) | Manifest contract; release-engineering. |
| `bi-024a` | `br --version` handshake at startup | Per BI-024a: invoke `br --version` at PL-005 step 4 Cat 0 pre-check; parse via regex; fail with exit code 8 on mismatch/unparseable; emit `daemon_startup_failed{failure_mode="br-version-incompatible"}`. | mech, `req:BI-024a` | `bi-024`, `bi-025`, cross: `pl-005` (step 4 Cat 0), cross: `on-NNN-exit-codes`, cross: `ev-NNN-daemon-startup-failed-payload` | |
| `bi-025` | Implement single `br`-CLI adapter (sole translation layer) | Per BI-025: one Go module wraps `os/exec` calls to `br`, parses output, returns typed results; expose injectable `br` binary path via constructor; production callers MUST NOT inject. | mech, `req:BI-025` | (none — root of adapter graph) | Foundation bead; many depend on it. |
| `bi-025a` | `br` exit-code → `BrError` classification | Per BI-025a: classify every `br` invocation; unrecognized → `BrOther` + emit `divergence_inconclusive` per EV-023a with `reason=authority_unavailable`. | mech, `req:BI-025a` | `bi-025`, `bi-schema.br-error`, `bi-error.taxonomy`, cross: `ev-023a` (single-authority semantics) | |
| `bi-025b` | Mandatory `--format json` invocation; no text parsing | Per BI-025b: every `br` call uses JSON output; commands lacking JSON support are fenced off; parse failure → `BrSchemaMismatch`. | mech, `req:BI-025b` | `bi-025`, `bi-025a` | |
| `bi-025c` | `br` subprocess timeout discipline (5s read / 10s write) | Per BI-025c: bounded wall-clock timeout (operator-tunable per ON §4.9); SIGTERM 5s → SIGKILL via HC-018; reap via PL-014; classify timeout as `BrUnavailable`. | mech, `req:BI-025c` | `bi-025`, cross: `hc-018` (SIGTERM-then-SIGKILL discipline), cross: `pl-014` (cmd.Wait reap), cross: `on-NNN-tunables` | |
| `bi-025d` | Capture `br` stderr with 1 MiB cap and 5 explicit scenarios | Per BI-025d: bounded capture; truncation suffix; handle (a) exit 0 + stderr (warnings), (b) exit ≠ 0 + empty stderr, (c) Rust panic exit 101, (d) argparse exit 2, (e) partial stderr at SIGKILL. | mech, `req:BI-025d` | `bi-025`, `bi-025c`, cross: `on-002` (operator-facing diagnostics), cross: `on-035` (structured log) | |
| `bi-025e` | Allow concurrent `br` invocations; no adapter-side mutex | Per BI-025e: concurrent calls permitted; SQLite WAL serializes writes; on `BrDbLocked` retry per BI-025c policy. | mech, `req:BI-025e` | `bi-025`, `bi-025a` | OQ-BI-012 (multi-daemon-same-Beads) tracked; not a bead. |
| `bi-026` | Absorb Beads breakage in adapter; no forking Beads | Per BI-026: on backwards-incompatible Beads change, ship adapter change OR remain pinned; never fork. | mech, `req:BI-026` | `bi-025`, `bi-024` | Process bead; closes when policy doc lands. |
| `bi-027` | Beads-CLI skill is the agent-facing access path | Per BI-027: skill documents `br` surface, output formats, jq pipelines, and the no-terminal-write discipline for agents; authoritative location declared in CP §4.11 and `docs/components/external/beads.md`. | mech, `req:BI-027` | cross: `cp-NNN-skill-decl` (CP §4.11), `bi-002` | OQ-BI-002 tracks concrete skill-package path. v0.1.3: removed `bi-004` edge (no inline cite of BI-004 in BI-027's body; bidirectional pilot bug per discipline v0.4 §2.7). |
| `bi-028` | Beads-CLI skill present in every agent's launch context | Per BI-028: handler injects skill by default; only role-specific YAML policy may exclude. | mech, `req:BI-028` | `bi-027`, cross: `hc-NNN-skill-injection` (HC §4.11), cross: `cp-NNN-yaml-policy` (CP §6.3) | |
| `bi-029` | Derive deterministic idempotency key `<run_id>:<transition_id>:<op>` | Per BI-029: key is deterministic across invocations; covers claim/close/reopen. | mech, `req:BI-029` | `bi-schema.intent-log-entry`, `bi-schema.terminal-op` | |
| `bi-030` | Pre-write intent log — atomic write + parent-dir fsync on create AND delete | Per BI-030 (multi-step protocol per discipline §2.2): umbrella + 5 step beads (write tmp, fsync tmp, rename, fsync parent on create, fsync parent on delete). Matches WM-026 sidecar discipline. | mech, axis:idempotency-non-idempotent, `req:BI-030` | `bi-schema.intent-log-entry`, `bi-029`, cross: `wm-026` (sidecar atomicity), cross: `ev-NNN-fsync-durability` (EV §4.4) | See sub-beads `bi-030.s1`–`bi-030.s5`+`bi-030.s6` (delete) below. |
| `bi-030.s1` | Write IntentLogEntry to `<key>.json.tmp-<rand>` | Per BI-030 step 1 — temp-file write before fsync. | mech | (none) | Step bead. v0.1.3: removed explicit `bi-030 (umbrella)` edge — the parent-child edge created by `--parent <bi-030-id>` already encodes the dep per discipline v0.4 §2.2 F11. |
| `bi-030.s2` | `fsync(temp_fd)` before rename | Per BI-030 step 2 — temp-file fsync. | mech | `bi-030.s1` | Step bead. |
| `bi-030.s3` | `rename(2)` to canonical `<key>.json` | Per BI-030 step 3 — atomic rename. | mech | `bi-030.s2` | Step bead. |
| `bi-030.s4` | `fsync(parent_dir_fd)` after rename (create durability) | Per BI-030 step 4 — REQUIRED for APFS / ext4-data=ordered durability. | mech | `bi-030.s3` | Step bead. |
| `bi-030.s5` | Invoke `br` only AFTER step 4 | Per BI-030 step 5 — ordering invariant. | mech | `bi-030.s4`, `bi-025` | Step bead. |
| `bi-030.s6` | On success: `unlink` + `fsync(parent_dir_fd)` (delete durability) | Per BI-030 (delete sequence) — without parent-dir fsync, false-positive Cat 3a on remount. | mech | `bi-030.s5` | Step bead. |
| `bi-031` | Idempotent crash-recovery via status-check-before-reissue | Per BI-031 (multi-step protocol): umbrella + 5 step beads. Layering with RC Cat 3a documented (adapter recovery vs post-emergence detection). Includes 100-entry intent-log backpressure → `daemon_degraded`. | mech, axis:idempotency-recoverable-non-idempotent, `req:BI-031` | `bi-029`, `bi-030`, `bi-024a`, `bi-025`, `bi-025a`, `bi-025b`, `bi-025c`, cross: `pl-005` (step 4), cross: `pl-010` (Cat 0 retry), cross: `rc-014` (RC Cat 3a detector), cross: `rc-002a` (lock primitive), cross: `rc-025` (verdict-executor), cross: `ev-023a`, cross: `ev-NNN-daemon-degraded-payload`, cross: `on-035`, cross: `on-037` | See sub-beads `bi-031.s1`–`bi-031.s5`. |
| `bi-031.s1` | Read intent file fields (`op`, `bead_id`, `idempotency_key`, `intended_post_state`) | Per BI-031 step 1. | mech | (none) | Step bead. v0.1.3: removed explicit `bi-031` edge — parent-child edge encodes the dep per discipline v0.4 §2.2 F11. |
| `bi-031.s2` | `br show <bead_id>` to read current `coarse_status` | Per BI-031 step 2. | mech | `bi-031.s1`, `bi-025`, `bi-025b`, `bi-025c` | Step bead. |
| `bi-031.s3` | Status-equals-intended branch with audit-log disambiguation (3i / 3ii) | Per BI-031 step 3: harmonik-side authorship → no-op + ON-035 record; otherwise emit `divergence_inconclusive`, retain intent file. | mech, axis:idempotency-recoverable-non-idempotent | `bi-031.s2`, cross: `ev-023a`, cross: `on-035` | Step bead; OQ-BI-009 tracks audit-log surface. |
| `bi-031.s4` | Status-equals-prestate reissue + 6 BrError branches (4a–4f) | Per BI-031 step 4: reissue with idempotency key; classify result; route per (4a) success / (4b) BrConflict re-step3 / (4c) BrDbLocked retry / (4d) BrUnavailable degraded / (4e) BrSchemaMismatch divergence / (4f) BrOther escalation. | mech, axis:idempotency-recoverable-non-idempotent | `bi-031.s2`, `bi-025a`, `bi-error.taxonomy`, cross: `rc-NNN-cat6b` | Step bead; the 6 sub-cases live as sub-bullets in description. |
| `bi-031.s5` | Status-neither-pre-nor-post → Cat 3a divergence emission | Per BI-031 step 5: emit `divergence_inconclusive` per EV-023a with `reason=authority_unavailable`; refuse reissue. | mech | `bi-031.s2`, cross: `rc-014`, cross: `ev-023a` | Step bead. |
| `bi-031b` | `br show` JSON-consistency dependency | Per BI-031b: parse failures classify as `BrSchemaMismatch` (NOT "status differs"); emit `divergence_inconclusive`; refuse reissue. | mech | `bi-031`, `bi-025a`, `bi-025b`, cross: `ev-023a` | |
| `bi-032` | Intent log is the Cat 3a detector's evidence source | Per BI-032: this spec owns intent-log shape + durability; RC §8.12 owns classification + auto-resolver. | mech | `bi-030`, cross: `rc-014`, cross: `rc-NNN-cat3a-action-mapping` (RC §8.12) | |

**Per-requirement bead count:** 43 requirement IDs map to 36 distinct first-class beads (after BI-008+8a coalesce, BI-010+10a+10b coalesce). Plus 11 step beads (`bi-030.s1`–`s6` × 6 + `bi-031.s1`–`s5` × 5). **Total req beads = 47.**

---

## 3. Sensor / invariant task table

| bead-id | title | description | tags | blocks edges (citing bead → prerequisite) | notes |
|---|---|---|---|---|---|
| `bi-inv-001` | Sensor: no intra-run Beads writes (corpus + os/exec lint + scenario) | Per BI-INV-001 sensor: (a) corpus reviewer-persona scan for `br <state-change>` outside §4.10 adapter; (b) §10.2 contract test that the only `os/exec` callsite with `br` argv is the adapter; (c) cross-spec scenario test injecting non-terminal node and asserting no `br` write. | mech, `req:BI-INV-001`, `kind:invariant` | `bi-010`, `bi-011`, `bi-012`, `bi-025` | Three-pronged sensor. |
| `bi-inv-002` | Sensor: bead ID byte-equal across run/trailer/event/session-log | Per BI-INV-002 sensor: cross-spec test asserting bead-ID byte-equal across `Run.bead_id` (EM §4.3 EM-014), checkpoint trailer (EM §4.4 EM-017), event payload `bead_id` (EV §6.3), session-log sidecar (WM §4.7); reviewer scan for `mint_alternate_id` helpers. | mech, `req:BI-INV-002`, `kind:invariant` | `bi-017`, `bi-018`, `bi-019`, `bi-020`, cross: `em-014`, cross: `em-017`, cross: `ev-NNN-bead-id-payload`, cross: `wm-NNN-session-log-sidecar` | Multi-spec scenario. |
| `bi-inv-003` | Sensor: git wins on completion disagreement | Per BI-INV-003 sensor: RC §4.3 RC-013 Cat 3 detector + §10.2 BI-021..BI-023 scenario test injecting Beads-`closed` / no-merge-commit divergence and asserting Cat 3 dispatch (NOT silent Beads-side correction). | mech, `req:BI-INV-003`, `kind:invariant` | `bi-021`, `bi-022`, `bi-023`, cross: `rc-013` | |
| `bi-inv-004` | Sensor: Beads status changes auditable through adapter or flagged | Per BI-INV-004 sensor: Cat 3a detector per RC §4.3 RC-014 + §8.4a; adapter unit tests per §10.2 BI-029..BI-032; cross-spec scenario injecting out-of-band `br` write and asserting Cat 3a dispatch. | mech, axis:idempotency-recoverable-non-idempotent, `req:BI-INV-004`, `kind:invariant` | `bi-029`, `bi-030`, `bi-031`, `bi-032`, cross: `rc-014`, cross: `rc-NNN-cat3a-action-mapping` | |

**Sensor-bead count:** 4.

---

## 4. Schema / error-taxonomy task table

| bead-id | title | description | tags | blocks edges (citing bead → prerequisite) | notes |
|---|---|---|---|---|---|
| `bi-schema.bead-record` | Define `BeadRecord` (§6.1) | Implement the 7-field `BeadRecord`: `bead_id`, `title`, `description`, `bead_type`, `status: CoarseStatus`, `edges: List<DependencyEdge>`, `audit_trail_ref`. | mech, `kind:schema` | `bi-schema.coarse-status`, `bi-schema.dependency-edge` | |
| `bi-schema.coarse-status` | Define `CoarseStatus` enum (§6.1) | Implement the read-surface enum: Beads-owned, extensible. Live Beads v0.1.45 has 8 values; harmonik reads tolerate the full enum + future Beads extensions per BI-007 v0.4.1 reframe. | mech, `kind:enum` | (none) | Root of the schema graph; v0.1.3: description updated for BI v0.4.1 split. |
| `bi-schema.harmonik-write-status` | Define `HarmonikWriteStatus` enum (§6.1, BI v0.4.1 split) | Implement the 5-value write subset `{open, in_progress, closed, deferred, tombstone}`. The harmonik adapter writes only these values; Beads's wider enum is read-only per BI-007. | mech, `kind:enum` | (none) | v0.1.3: NEW bead per BI v0.4.1 §6.1 schema split. Roots the write-surface graph. |
| `bi-schema.dependency-edge` | Define `DependencyEdge` (§6.1) | Implement the 3-field record + `EdgeKind`. | mech, `kind:schema` | `bi-schema.edge-kind` | |
| `bi-schema.edge-kind` | Define `EdgeKind` enum (§6.1) | Implement `{parent-child, blocks, conditional-blocks, waits-for}`. | mech, `kind:enum` | (none) | |
| `bi-schema.br-error` | Define `BrError` enum (§6.1a) | Implement `{OK, NotFound, Conflict, DbLocked, SchemaMismatch, Unavailable, Other}` + exit-code mapping table. | mech, `kind:enum` | (none) | Mapping table re-validated when BI-024 pinned version changes. |
| `bi-schema.intent-log-entry` | Define `IntentLogEntry` (§6.1) | Implement the 8-field record including `intended_post_state: CoarseStatus` and `schema_version: Integer` (N-1 readable per ON §4.5). | mech, `kind:schema` | `bi-schema.coarse-status`, `bi-schema.terminal-op`, cross: `on-NNN-n-minus-1` (ON §4.5) | |
| `bi-schema.terminal-op` | Define `TerminalOp` enum (§6.1) | Implement `{claim, close, reopen}`. | mech, `kind:enum` | (none) | |
| `bi-error.taxonomy` | `BrError` → reconciliation-category routing table (§8) | Implement the routing logic: NotFound → Cat 3 generic; Conflict → Cat 3a (idempotency recovery); DbLocked → Cat 0 (bounded retry → exit 8); SchemaMismatch → Cat 0 daemon startup failure (exit 8); Unavailable → Cat 0 (PL-010 cadence); Other → Cat 3 generic. | mech, `kind:taxonomy` | `bi-schema.br-error`, cross: `rc-NNN-cat3-detector`, cross: `rc-NNN-cat0-detector`, cross: `pl-010` (Cat 0 retry cadence), cross: `on-NNN-exit-code-8` | Distinct from `bi-schema.br-error` (the enum); this is the routing logic. |

**Schema + error-taxonomy bead count:** 8 (7 schemas + 1 taxonomy).

---

## 5. Cross-spec edge summary

These are the cross-spec `blocks` edges this pilot would emit (per discipline v0.3 §2.7 / §3.1 — edge label `blocks`, not the deprecated `blockedBy`). Every target spec must appear in BI's front-matter `depends-on` (validated below).

**To architecture (AR):** none. (BI cites AR only via §9.3 co-reference for amendment protocol; no specific AR-NNN cited inline.)

**To execution-model (EM):**
- `bi-017` → `em-014` (Run.bead_id field per EM §6.1)
- `bi-018` → `em-017` (checkpoint trailer per EM §6.2)
- `bi-inv-002` → `em-014`, `em-017`

**To event-model (EV):**
- `bi-019` → `ev-NNN-payload-shape` (EV §6.3 — section-level fanout per discipline §3.1.3; bead tagged `cite:wide-fanout`; resolved during EV pilot pass)
- `bi-024a` → `ev-NNN-daemon-startup-failed-payload`
- `bi-025a` → `ev-023a`
- `bi-031` → `ev-023a`, `ev-NNN-daemon-degraded-payload`
- `bi-031.s3` → `ev-023a`
- `bi-031.s5` → `ev-023a`
- `bi-031b` → `ev-023a`
- `bi-030` → `ev-NNN-fsync-durability` (EV §4.4)
- `bi-inv-002` → `ev-NNN-bead-id-payload`

**To handler-contract (HC):**
- `bi-004` → `hc-NNN-skill-injection` (HC §4.11)
- `bi-025c` → `hc-018` (SIGTERM-then-SIGKILL discipline)
- `bi-028` → `hc-NNN-skill-injection`

**To control-points (CP):**
- `bi-004` → `cp-NNN-skill-decl` (CP §4.11)
- `bi-027` → `cp-NNN-skill-decl`
- `bi-028` → `cp-NNN-yaml-policy` (CP §6.3)

**To workspace-model (WM):**
- `bi-010` → `wm-007` (task branch merge per WM §4.5)
- `bi-020` → `wm-NNN-session-log-sidecar` (WM §4.7)
- `bi-030` → `wm-026` (sidecar atomicity discipline)
- `bi-inv-002` → `wm-NNN-session-log-sidecar`

**To process-lifecycle (PL):**
- `bi-014a` → `pl-006` (orphan sweep — extension request per OQ-BI-010)
- `bi-016` → `pl-NNN-startup-step3-4` (PL §4.2 steps 3-4)
- `bi-024a` → `pl-005` (step 4 Cat 0 pre-check)
- `bi-025c` → `pl-014` (cmd.Wait reap)
- `bi-031` → `pl-005`, `pl-010` (Cat 0 retry)
- `bi-error.taxonomy` → `pl-010`

**To operator-nfr (ON):**
- `bi-024a` → `on-NNN-exit-codes` (ON §8 exit code 8 `beads-unavailable`)
- `bi-025c` → `on-NNN-tunables` (ON §4.9 operator-tunable)
- `bi-025d` → `on-002` (operator-facing diagnostics), `on-035` (structured log)
- `bi-031` → `on-035`, `on-037` (degraded routing)
- `bi-031.s3` → `on-035`
- `bi-schema.intent-log-entry` → `on-NNN-n-minus-1` (ON §4.5)
- `bi-error.taxonomy` → `on-NNN-exit-code-8`

**To reconciliation (RC):**
- `bi-010` → `rc-020` (reopen-bead verdict)
- `bi-014a` → `rc-NNN-cat0`
- `bi-016` → `rc-NNN-cat3a-detector`
- `bi-022` → `rc-NNN-cat3`
- `bi-031` → `rc-014`, `rc-002a` (lock primitive), `rc-025` (verdict-executor)
- `bi-031.s4` → `rc-NNN-cat6b`
- `bi-031.s5` → `rc-014`
- `bi-031b` → (via EV-023a — no direct RC edge)
- `bi-032` → `rc-014`, `rc-NNN-cat3a-action-mapping` (RC §8.12)
- `bi-error.taxonomy` → `rc-NNN-cat3-detector`, `rc-NNN-cat0-detector`
- `bi-inv-003` → `rc-013`
- `bi-inv-004` → `rc-014`, `rc-NNN-cat3a-action-mapping`

**`depends-on` validation:** every target spec above (`em`, `ev`, `hc`, `cp`, `wm`, `pl`, `on`, `rc`) appears in BI's front-matter `depends-on`. AR is in `depends-on` but produces no inline edges; this is permissible (AR is the universal classification base).

**`NNN`-marked targets** are edges where BI cites a section anchor (`[X.md §N]`) rather than a specific requirement ID; per discipline §3.1.3 these are placeholders to be resolved during the corresponding spec's pilot pass. Count: 19.

**Cycle check:** none detected. The known BI ↔ EM mutual dependency splits cleanly at bead level: BI beads block on EM beads; no EM bead in this pilot blocks on a BI bead.

---

## 6. Optional infrastructure tasks

Per discipline §2.4, shared test infrastructure is extracted as a separate bead with `blocks` edges to consumers.

| bead-id | title | description | blocks-on | gates (other beads) |
|---|---|---|---|---|
| `bi-test.crash-harness` | Crash-injection test harness for adapter idempotency | Per §10.2 (BI-029..BI-032 obligation): crash the adapter between intent-log fsync and `br` call completion; restart and assert idempotent completion. Shared infrastructure for the BI-029..BI-032 family. | (none) | `bi-029`, `bi-030`, `bi-031`, `bi-032`, `bi-inv-004` |

**Test-infra bead count:** 1.

---

## 7. Tally

### BI bead count

| Class | Count |
|---|---|
| Spec parent bead (`bi`) | 1 |
| Requirement beads (after coalesces: BI-008+8a, BI-010+10a+10b) | 40 |
| Step beads (BI-030 × 6, BI-031 × 5) | 11 |
| Sensor / invariant beads | 4 |
| Schema beads (post-v0.4.1 split) | 8 |
| Error-taxonomy bead | 1 |
| Test-infrastructure beads | 1 |
| **Total BI beads** | **66** |

(v0.1.3 correction: prior tally said 36 req beads / 7 schemas / 61 total — both numbers were wrong. Actual: 43 reqs minus 3 coalesced = 40 first-class req beads; 8 schemas after BI v0.4.1 added `HarmonikWriteStatus`; total 66.)

### Full-corpus extrapolation

The 10 specs hold 526 distinct `XX-NNN` requirement IDs (per NEXT_AGENT.md). BI has 43 requirement IDs and produces 51 first-class req-or-step beads (40 + 11 step). That is a multiplier of **~1.19× requirement→bead** before sensors/schemas/infra.

Adding the supporting fixtures: BI's 43 reqs → 66 total beads = **~1.53× requirement→total** multiplier.

Extrapolating naively to 526 reqs: **~805 beads total** across the corpus. Range: 700–950 depending on per-spec coalesce/split rates and how many specs have BI-030-style multi-step protocols (PL-005 startup sequence and ON drain protocol are likely candidates for high fan-out; RC has many invariants and protocols and may exceed BI's per-req multiplier).

Per-spec rough estimates (using BI's 1.53× as the central multiplier, adjusted up for spec sizes with known multi-step content):

| Spec | Reqs | Est. beads | Notes |
|---|---|---|---|
| AR | 52 | ~70 | Mostly declarations; lower multiplier likely (~1.3×). |
| EM | 66 | ~95 | Several schemas + outcome spine; moderate multiplier. |
| EV | 48 | ~75 | Big taxonomy (§3.2); moderate. |
| HC | 65 | ~95 | Skill injection + handler protocols; moderate. |
| CP | 55 | ~80 | Skill declaration + YAML policy; moderate. |
| WM | 53 | ~80 | Branching + worktree state; multi-step likely. |
| PL | 42 | ~70 | Startup sequence is multi-step heavy; ~1.6×. |
| ON | 59 | ~85 | Exit codes + structured logs + drain; moderate. |
| RC | 43 | ~80 | 11-category taxonomy + multi-step verdict-executor; ~1.8× possible. |
| BI | 43 | 66 | (this pilot, v0.1.3 corrected count) |
| **Total** | **526** | **~795** | (BI revised from 61 to 66) |

---

## 8. Items in BI that did NOT decompose cleanly

These are notable rough edges from the pilot that the discipline needs to address before scaling.

1. **BI-031 is genuinely 5 step beads + 6 sub-cases inside step 4.** The discipline's "≥3 steps with independent testability" guideline (v0.2 §2.2) produces 5 step beads. The 6 BrError sub-cases (4a–4f) inside step 4 are not separate step beads (they share the reissue state machine), but they ARE independently buggy and individually testable. The pilot collapses them into sub-bullets in `bi-031.s4`'s description per discipline §2.2 "err toward fewer beads." Reconsider for v0.3 if implementer feedback suggests the sub-cases need their own work tracking.

2. **BI-010 is genuinely 3 requirements that should be 1 bead.** BI-010 + BI-010a (table) + BI-010b (carve-out) are the cleanest example of the §2.3 coalescible-cluster rule in this spec. The status-mapping table inside BI-010a has 9 rows; an aggressive decomposer might mint a bead per table row. The pilot does NOT do this — table rows are sub-cases, not separate beads — but this is a discipline boundary worth confirming.

3. **BI-025 family has an asymmetric coalesce shape.** BI-025 (the adapter-as-sole-translation-layer) is the foundation; BI-025a–BI-025e are five orthogonal concerns (exit codes, JSON, timeout, stderr, concurrency). The pilot keeps each as a separate bead per §2.1 / §2.3 (orthogonal concerns don't coalesce). But ALL of them depend on `bi-025` as their root, producing a star-shape dependency. This is correct but worth noting: the adapter bead is the highest-fanout dependency root in the BI bead set.

4. **BI-014a, BI-024a, BI-031** all describe **cross-cutting startup-time work**. They cite PL-005 step 4 (Cat 0 pre-check), PL-006 (orphan sweep), and PL-010 (Cat 0 retry) extensively. The actual implementation will likely live in a single "daemon startup adapter integration" code module, but the bead set keeps them separate (correctly — each has its own contract). The implementer of `pl-005` and the implementer of `bi-024a` will need to coordinate; the cross-spec edge graph captures the dependency direction (BI blocks on PL).

5. **§9.3 co-references produced zero edges.** All §9.3 entries (WM session log, RC categories, PL startup sequence, ON queue format) are READ-ONLY consumption. Per discipline §2.7, co-references do NOT generate bead-level edges — they're spec metadata describing read relationships at the surface level. Edges come from inline cites only. The cross-spec edges captured in §5 above all came from inline cites in BI's body, not from §9.3. **(v0.3 update — F4 collapse.)** Discipline v0.3 collapses the prior `waits-for` vs `blockedBy` distinction to a single `blocks` edge label at MVH (`waits-for` reserved for post-MVH operational evidence). Inline cites all use `blocks`; co-references still generate no edges. Discipline v0.3 also added the F5 missing-inline-cite catcher: if a §9.3 entry's consumers cannot be tested without producer impl, surface as a spec-edit task, not as an invented edge — BI did not surface any such case in this pilot.

6. **OQ-BI-010 (orphan-sweep extension to PL-006) is a "spec-needs-patch" bead, not an impl bead.** BI-014a documents its requirement assuming PL-006 will be extended; PL-006 has not yet been extended (cross-spec coordination request). The pilot emits the cross-spec edge `bi-014a → pl-006` as if PL-006 covered `br` subprocesses; in practice the PL bead must first be expanded by a v0.4.x patch wave before this implementation work can begin. **This is the discipline's first observed case of a bead that is gated on a spec edit, not an upstream impl.** **(v0.3 update — F6 carve-out.)** Discipline v0.3 §2.8 now has an explicit carve-out: at OQ-resolution time the team mints TWO beads — a `kind:spec-edit` bead for the PL-006 amendment + a `kind:impl` bead for the resulting code work; the `kind:impl` bead `blocks` `bi-014a`. Until OQ-BI-010 resolves, `bi-014a` carries the transient tag `gated-by-spec-edit` so the readiness workflow can defer it from `br ready` results.

---

## 9. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-04-25 | 0.1 | foundation-author | Initial pilot: BI v0.4.0 → 61 beads (1 parent + 36 req + 11 step + 4 sensor + 7 schema + 1 error-taxonomy + 1 test-infra). 19 placeholder cross-spec edges marked `NNN`. 6 decomposition rough edges noted in §8. |
| 2026-04-25 | 0.1.1 | foundation-author | Patch: updated OQ-DTT-NNN references to discipline v0.2 (10 OQs collapsed to rule clauses). §1 OQ-bead reference points at discipline §2.8 / Tag mapping table; §5 edge example notes `cite:wide-fanout` tag per discipline §3.1.3; §8 item 1 (BI-031 sub-cases) updated to cite discipline §2.2 guideline; §8 item 5 (§9.3 co-references) updated to reflect discipline v0.2's no-edge-from-§9.3 stance. Bead set itself unchanged (61 beads). |
| 2026-04-27 | 0.1.3 | foundation-author | Applied 5 smoke-load corrections from [bi-smoke-load-findings.md](bi-smoke-load-findings.md) §3, against discipline v0.4. **(F-pilot-1, tally arithmetic)** §7 corrected: 40 first-class req beads (not 36; v0.1.2 dropped the 008a/010a/010b coalesce arithmetic), 8 schemas (post-v0.4.1 split adds `HarmonikWriteStatus`), total 66 (not 61). Multiplier revised to ~1.53×. Per-spec estimate row for BI updated; corpus total 790 → 795. **(F-pilot-2, bidirectional cycle)** Removed `bi-004 → bi-027` from `bi-004`'s blocks edges and `bi-027 → bi-004` from `bi-027`'s blocks edges. Inspection of BI-004 + BI-027 in the spec body shows neither requirement actually inline-cites the other; the v0.1.2 edges were pilot-author inventions per discipline v0.4 §2.7. **(F-pilot-3, impl→sensor wrong direction)** Removed `bi-011 → bi-inv-001` and `bi-022 → bi-inv-003`. Sensor↔impl edges are one-way per discipline v0.4 §2.5: sensor blocks-on impl, never the inverse. The §3 sensor table already had the correct direction (`bi-inv-001 → bi-011`, `bi-inv-003 → bi-022`); the §2 impl-row entries duplicated the relationship in the wrong direction. **(F-pilot-4, step→umbrella implicit)** Removed `bi-030.s1 → bi-030 (umbrella)` and `bi-031.s1 → bi-031`. Step beads created with `--parent <umbrella-id>` get an implicit `parent-child` dep; explicit `blocks` produces a cycle per discipline v0.4 §2.2 F11. Other step beads (`bi-030.s2`–`s6`, `bi-031.s2`–`s5`) correctly chain to their predecessor step and are unchanged. **(F-pilot-5, BI v0.4.1 schema split)** §1 spec-version updated v0.4.0 → v0.4.1; §2 row `bi-007` rewritten ("Coarse status — read = `CoarseStatus`, write = `HarmonikWriteStatus` (5-value subset)") + edge to `bi-schema.harmonik-write-status` added; §2 row `bi-010` description amended + edge to `bi-schema.harmonik-write-status` added; §4 schema table gains a new row `bi-schema.harmonik-write-status`; §1 schema count 7 → 8 + spec-parent description updated. **Verification.** This patch wave is intended to produce a clean reload (`rm -rf .beads/` + `br init --prefix hk` + re-run): zero cycle rejections, 66 beads created (1 epic + 54 first-level children + 11 step beads), all attempted edges land. Bead set summary: 66 beads / 56+ intra-spec `blocks` edges (cross-spec edges deferred to all-10-specs cycle-check phase). **Other:** dry-run notice in preamble updated to "Smoke-load status" reflecting `br` v0.1.45 install + first-load completion. |
| 2026-04-26 | 0.1.2 | foundation-author | Aligned pilot to discipline v0.3 review fixes (F1, F4, F9). **(F1)** Renamed every `blockedBy` cell / column header / prose mention to `blocks` (canonical Beads `DependencyType` value). The §2 / §3 / §4 / §5 tables now have a `blocks edges (citing bead → prerequisite)` column, loaded as `br dep add <citing> <prerequisite> -t blocks`. The §6 infra table relabeled `blockedBy → blocks-on` and `blocks → gates (other beads)` for symmetry. The §5 prose noting "cross-spec `blockedBy` edges" now says "cross-spec `blocks` edges per discipline v0.3 §2.7 / §3.1." Note: BI-006's bead description and `bi-schema.edge-kind`'s description still mention `waits-for` because those beads define Beads's `EdgeKind` enum, which legitimately includes that value — those are not pilot edges. **(F4)** The BI-019 → `ev-NNN-payload-shape` edge was previously framed as `waits-for` (declaration-only); per the F4 collapse it is now `blocks` and lives in the renamed `blocks edges` column unchanged. The §8 item 5 paragraph now records the F4 collapse: inline cites all use `blocks` at MVH; co-references still don't generate edges; `waits-for` reserved for post-MVH operational evidence. **(F6, in §8 item 6)** Updated the OQ-BI-010 / PL-006 worked example to reference discipline v0.3 §2.8's two-bead carve-out (mint `kind:spec-edit` + `kind:impl` at OQ-resolution time; consumers carry transient `gated-by-spec-edit` tag). **(F9)** Walked the §2 / §3 / §4 / §5 tables — the BI-018 → em-017 edge was already aligned (it always was `blocks`); the BI-019 → ev-NNN edge is the only post-F4 reclassification. Bead set itself unchanged (61 beads); only edge-label semantics and prose annotations changed. **F2, F3, F5, F7, F8, F10** are discipline-only fixes; they do not change the pilot's bead inventory. |
