# Beads Integration

```yaml
---
title: Beads Integration
spec-id: beads-integration
requirement-prefix: BI
status: reviewed
spec-category: foundation-cross-cutting
spec-shape: requirements-first
version: 0.7.0
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-06-21
depends-on:
  - architecture
  - execution-model
  - event-model
  - handler-contract
  - control-points
  - workspace-model
  - process-lifecycle
  - operator-nfr
  - reconciliation
  - queue-model
---
```

## 1. Purpose

This spec defines harmonik's integration with Beads — the external SQLite-backed task ledger that owns bead content, typed dependency edges, coarse lifecycle status, stable bead IDs, and atomic-claim semantics. It binds together the Beads references scattered across other foundation specs and names the normative surfaces that are load-bearing across the system: access model (`br` CLI only), write discipline (terminal transitions only), read queries, submit-time validation read surface, bead-ID propagation, store-authority rules, version-pin + adapter layer, and the adapter idempotency contract.

It exists as a standalone spec because the integration shape is cross-cutting and load-bearing (locked decision #13 per [docs/foundation/problem-space.md §Locked decisions]).

## 2. Scope

### 2.1 In scope

- Selection of the `Dicklesworthstone/beads_rust` SQLite-backed fork; rejection of the Dolt variant.
- `br` CLI as the sole access surface; rejection of `br serve` (Beads's MCP server).
- Set of Beads-managed data: bead content, typed dependency edges, coarse status, stable IDs, atomic-claim semantics.
- Harmonik write surface restricted to terminal lifecycle transitions (claim / close / reopen).
- Harmonik read surface: ready-work query (orchestrator-facing), dependency graph, bead-detail, reconciliation queries.
- Submit-time validation read surface consumed by the daemon's queue-submit / queue-append / queue-dry-run accept path.
- Bead-ID propagation into run metadata, checkpoint trailers, event payloads, and session-log metadata.
- Store-authority rules for git-vs-Beads-vs-JSONL disagreements.
- Version-pin policy and the `br`-CLI adapter layer that absorbs breakage.
- Agent access to `br` via the Beads-CLI skill, delivered through handler-contract's skill-injection mechanism.
- `br`-CLI adapter idempotency contract for terminal-transition writes (idempotency key, pre-write intent log, audit-log check).

### 2.2 Out of scope

- Skill-injection mechanism (how skills are delivered into an agent's launch context) — owned by [handler-contract.md §4.11] and [control-points.md §4.11 Skill declaration surface].
- Beads's internal schema, SQLite layout, and CLI implementation — owned by the Beads project, not harmonik; harmonik consumes the `br` surface as declared.
- Cat 3a torn-write detector logic (classification and dispatch) — owned by [reconciliation/spec.md §4.3]; this spec owns only the adapter's idempotency contract consumed by the detector.
- Run metadata definition and run-ID allocation — owned by [execution-model.md §4.3].
- Checkpoint commit trailer format (shape of `Harmonik-Bead-ID`) — owned by [execution-model.md §6.2]; this spec owns WHEN the trailer is populated.
- Event payload shapes — owned by [event-model.md §6.3]; this spec owns WHEN bead-scoped events are emitted.
- Queue data model, group lifecycle, validation rules, queue persistence — owned by [queue-model.md]; this spec owns only the Beads-read surface consumed during validation.

## 3. Glossary

- **bead** — an atomic queued work item in the Beads SQLite store, carrying content, typed edges, and coarse status. (see §4.3)
- **coarse status** — Beads's `Status` enum (Beads-owned and extensible; live at v0.1.45 = 8 values; harmonik writes only the 5-value subset `{open, in_progress, closed, deferred, tombstone}` per BI-007). (see §4.3, §6.1)
- **terminal-transition write** — a `br` status-change invocation by harmonik at a workflow boundary. Subdivided into two categories by recovery posture:
  - **activity-marker write** — a write whose value asserts "harmonik is currently working this bead" but is NOT load-bearing for correctness. A stale activity-marker (daemon crashed; no terminal event landed; no adapter intent for close/reopen) is auto-resettable by the orphan-sweep duty of PL-006 (extended per BI-010d). Currently: `claim` (`open` → `in_progress`) and `reset` (`in_progress` → `open`).
  - **truth-claim write** — a write whose value asserts a durable fact about whether work is done and MUST route through reconciliation on disagreement. Currently: `close` (`in_progress` → `closed`) and `reopen` (`closed` → `open`).
  Both categories route through the §4.8 adapter and carry §4.10 idempotency keys. (see §4.4)
- **`br`-CLI adapter** — the thin harmonik module that translates typed queries and writes into `br` subprocess invocations and parses `br` output. (see §4.8, §4.10)
- **idempotency key** — the deterministic string `<run_id>:<transition_id>:<op>` identifying one terminal-transition write. (see §4.10)
- **intent log** — the on-disk record of a pending terminal-transition write at `.harmonik/beads-intents/<idempotency_key>.json`, written before the `br` call and deleted after success. (see §4.10)
- **submit-time validation read** — an adapter `br show` read performed during `queue-submit` / `queue-append` / `queue-dry-run` accept, used to validate the referenced beads against the live ledger. (see §4.5a)
- **Beads-CLI skill** — the skill package declaring the `br` command surface, output formats, and harmonik-specific write discipline; delivered to agents per [handler-contract.md §4.11]. (see §4.9)

## 4. Normative requirements

### 4.1 Beads selection

#### BI-001 — Beads SQLite fork is the adopted task ledger

Harmonik MUST adopt `github.com/Dicklesworthstone/beads_rust` (the SQLite-backed fork) as its task-ledger dependency. Harmonik MUST NOT adopt the Dolt-backed Beads variant (`gastownhall/beads`) for the MVH. Harmonik MUST NOT fork or modify the Beads codebase; Beads is consumed as an external binary via the `br` CLI.

Tags: mechanism

> RATIONALE: The user has observed persistent operational issues with Dolt in practice; the SQLite fork is local-first and fits harmonik's single-machine per-project daemon model (see [process-lifecycle.md §4.1]).

### 4.2 `br` CLI access — the only access surface

#### BI-002 — All Beads interactions route through the `br` CLI

Every harmonik interaction with Beads (query or write) MUST go through the `br` CLI. Neither the daemon nor any agent MAY access Beads's SQLite file directly or link against Beads's Rust library; the CLI is the authoritative surface.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-003 — `br serve` (Beads MCP server) is not used

Harmonik MUST NOT use Beads's `br serve` MCP server. Running `br serve` adds another long-lived process the daemon would have to manage; the CLI already exposes the authoritative surface (30+ commands) and composes with shell plus `jq` for post-processing. Any future proposal to enable `br serve` requires fresh justification per the amendment protocol in [architecture.md §4.6].

Tags: mechanism

#### BI-004 — Daemon invokes `br` directly; agents invoke `br` via the Beads-CLI skill

The daemon MUST invoke `br` as a direct subprocess for its read queries (§4.5, §4.5a) and its terminal-transition writes (§4.4). Agents MUST invoke `br` through the Beads-CLI skill delivered via the handler-contract skill-injection mechanism per [handler-contract.md §4.11] and [control-points.md §4.11]. An agent MUST NOT bypass the skill to access `br` directly, and the handler MUST NOT provision `br` outside the skill path.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.3 Beads-managed data

#### BI-005 — Beads owns bead content

Beads MUST be the source of truth for bead content: `title`, `description`, and `type`. Harmonik MUST NOT maintain a parallel authoritative copy of this content. A harmonik-side cache of bead content MUST NOT be treated as authoritative; on disagreement with Beads (detected during a §4.5 read), the cache MUST be refreshed from `br` output before any harmonik decision consumes it.

Tags: mechanism

#### BI-006 — Beads owns typed dependency edges

Beads MUST be the source of truth for typed dependency edges between beads. The supported edge kinds MUST include `parent-child`, `blocks`, `conditional-blocks`, and `waits-for`. Harmonik consumes these edges read-only per §4.5.

Tags: mechanism

#### BI-007 — Coarse status: harmonik write subset; read surface tolerates Beads's full enum

Beads's `Status` enum is owned and extended by Beads itself; harmonik MUST NOT redefine, narrow, or otherwise constrain it. Harmonik's WRITE surface (§4.4 BI-010) is restricted to the five-value subset `{open, in_progress, closed, deferred, tombstone}` — these are the only statuses harmonik may transition a bead INTO via `br` writes. Harmonik MUST NOT introduce finer-grained statuses by writing additional values.

Harmonik's READ surface (§4.5 BI-013, BI-014, BI-015, BI-016; §4.5a BI-013b, BI-013c) MUST tolerate any status Beads exposes, including values outside the write subset. As of Beads v0.1.45, the live `Status` enum is `{open, in_progress, blocked, deferred, draft, closed, tombstone, pinned}`. Statuses outside the harmonik write subset (`blocked`, `draft`, `pinned` at v0.1.45; future values via Beads's evolution) are treated as pass-through: the adapter MUST parse them, expose them to consumers as the literal Beads value, and MUST NOT map them onto write-subset values. Future Beads `Status` extensions are accepted automatically by the read surface; the write surface's five-value enum is amended only via this spec's amendment protocol.

`draft` specifically is the harmonik-side readiness mechanism for loaded but not-yet-dispatchable beads (per the decompose-to-tasks discipline). Submit-time validation per §4.5a BI-013b MUST reject any `bead_id` whose live status is `draft`, mapping the rejection through [queue-model.md §6 QM-021]. `br ready`'s native exclusion of `draft`-status beads remains available to orchestrator agents using the Beads-CLI skill per §4.9 BI-027.

Intra-run workflow state lives in harmonik's git checkpoint trail and JSONL event log per §4.4 (NOT in additional Beads statuses).

Tags: mechanism

#### BI-008 — Bead IDs are stable for the lifetime of the bead

A bead's ID MUST be stable from creation to tombstone. Harmonik relies on this stability for run-metadata bindings (§4.6), checkpoint trailers, event payloads, and session-log metadata.

Tags: mechanism

#### BI-008a — Bead-ID scoping

Bead IDs are scoped to a single Beads store per project; cross-project bead references are post-MVH and tracked under OQ-BI-005. The adapter MUST treat `bead_id` as opaque (no parsing, no minting, no rewriting per BI-008).

Tags: mechanism

#### BI-009 — Beads provides atomic-claim semantics

Beads MUST provide atomic-claim semantics such that two agents or daemons cannot simultaneously observe the same bead as claimed-by-self. Harmonik's dispatch mechanism relies on this atomicity; a successful claim returns the bead in `in_progress` to exactly one caller.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-009a — Workflow-mode label

A bead MAY carry an optional label of the form `workflow:<mode>` where `<mode> ∈ {single, review-loop, dot}`. The label's presence asserts a per-task workflow-mode override; its absence defers to lower-precedence tiers (per-project → daemon-level per [process-lifecycle.md §4.1 PL-004a] → built-in fallback `dot`, resolving the embedded `standard-bead.dot` canonical exemplar per [execution-model.md §4.3 EM-012a]). The built-in fallback carries a hard review-floor (EM-012a-FLOOR): on embedded-artifact load failure (parse failure, missing artifact, or schema-version incompatibility) the daemon MUST fall back to `review-loop`, NEVER to `single`. `single` (the no-review one-handler-per-node shape) is reachable ONLY via an explicit tier-1 per-bead `workflow:single` label (audited via `review_bypassed` per [execution-model.md §4.3 EM-012a]); a bead resolved at the per-project, daemon-level, or built-in-fallback tiers MUST NEVER be dispatched under `single`. The bead-level label is the highest-precedence input in the four-tier workflow-mode resolution chain owned by [execution-model.md §4.3].

A bead carrying two or more `workflow:<...>` labels is malformed. The `br`-CLI adapter (§4.8) MUST treat this as a hard read-error on the ready-work query (BI-013), the bead-detail query (BI-015), and the submit-time validation read (§4.5a BI-013b): the adapter MUST emit a `bead_label_conflict` observability event per [event-model.md §8.8.6] (with structured-log fallback per [operator-nfr.md §4.9 ON-035]: on event-bus emission failure the adapter MUST emit a structured-log record with `subsystem=beads-adapter`, level=error, naming the offending `bead_id` and the colliding label set), MUST surface the bead with `workflow_mode = <unresolved>`, and the daemon MUST fall back to the next-lower precedence tier rather than dispatch under an ambiguous mode.

The allowed-mode enum (`{single, review-loop, dot}`) is owned by [execution-model.md §4.3]; this requirement cites it by reference.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-009b — `## Branching` bead-body parse contract

A bead MAY declare per-bead branching configuration in a `## Branching` section of its description body. The daemon MUST parse this section at claim time using the following rules:

**Detection.** The `## Branching` section is present if the bead's description (as returned by `br show <id> --format json`, field `description`) contains the exact heading `## Branching` (ATX-style, level-2) on a line by itself. The section extends from the line after the heading to the next `## ` heading (or end of description). The daemon MUST scan the description string for this heading exactly once per claim; no caching across claims.

**Content shape.** The body of the `## Branching` section MUST be a fenced YAML block delimited by ` ```yaml ` and ` ``` ` (with no leading spaces on the fence lines). The YAML block carries zero or more of the following keys:

```yaml
start_from: <git-ref>        # optional; branch name or commit SHA
target_branch: <git-ref>     # optional; branch name
landing_strategy: <value>    # optional; "squash" | "cherry-pick"
```

All keys are optional. Unrecognised keys MUST be silently ignored (forward-compatibility). The YAML block MUST be parseable as a flat key-value mapping (no nested structure); a malformed YAML block MUST be treated as a parse error per the error rule below.

**Extraction.** The daemon MUST extract the three values and store them as the resolved `start_from`, `target_branch`, and `landing_strategy` inputs into the WM-005b precedence chain. A key present but with a null or empty-string value MUST be treated as absent (falls through to the next precedence tier).

**Error handling.** If the `## Branching` section is present but its YAML block is absent, malformed (YAML parse error), or contains a `landing_strategy` value outside `{squash, cherry-pick}`, the daemon MUST emit a `bead_body_parse_error` observability event per [event-model.md §8.8] (with structured-log fallback per [operator-nfr.md §4.9 ON-035]), MUST surface the bead to the dispatch loop with the `## Branching` fields treated as absent (falling through to project-level and spec-level defaults per WM-005b), and MUST NOT refuse to dispatch the bead. The structured-log record MUST include `subsystem=beads-adapter`, level=warn, the `bead_id`, and a `parse_error` field describing the failure.

**Agents.** Agents MUST NOT modify the `## Branching` section of a bead description from within a workflow run. This prohibition is enforced by the same intra-run write discipline as BI-010c; the Beads-CLI skill per §4.9.BI-027 MUST document it.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.4 Harmonik write surface — terminal transitions only

#### BI-010 — Harmonik writes to Beads ONLY at terminal workflow transitions

Harmonik MUST write to Beads only at the following terminal transitions:

- **Claim:** `open` → `in_progress`. Emitted when the daemon dispatches a run against a ready bead.
- **Close:** `in_progress` → `closed`. Emitted when a run's workflow reaches a success terminal state AND the merge to the target branch has completed per [workspace-model.md §4.2].
- **Reopen:** `closed` → `open`. Emitted when a failure classification or an investigator `reopen-bead` verdict per [reconciliation/spec.md §4.5] determines the work is not actually done.

The binding from harmonik run-level events to these three Beads transitions is normatively declared in BI-010a; reconciliation-driven writes (Cat 3a / 3c auto-resolvers) are normatively declared in BI-010b.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-010a — Status-mapping table

The following table binds harmonik run-level events to Beads coarse-status transitions. Every harmonik-driven Beads write MUST be classified by this table; writes outside the table are FORBIDDEN per BI-INV-001.

| Harmonik trigger | Beads transition | Op | Caller |
|---|---|---|---|
| `run_started` for bead-bound run; daemon dispatch per [execution-model.md §4.3 EM-013] | `open` → `in_progress` | claim | daemon (dispatch loop) |
| `run_completed` (terminal success) AND task branch merged per [workspace-model.md §4.5 WM-007] | `in_progress` → `closed` | close | daemon (terminal-event handler) |
| `run_failed` with `failure_class = transient` AND no in-run retry available | `in_progress` → `open` | reopen | daemon (terminal-event handler) |
| `run_failed` with `failure_class ∈ {structural, deterministic, compilation_loop}` | (no Beads write) | — | daemon emits `run_failed`; investigator may later issue `reopen-bead` verdict |
| `run_failed` with `failure_class = canceled` | (no Beads write at MVH; OQ-BI-004 tracks operator-cancel routing) | — | daemon emits `run_failed`; OQ-BI-004 tracks whether to reopen |
| `run_failed` with `failure_class = budget_exhausted` | (no Beads write; route through investigator dispatch via reconciliation) | — | reconciliation investigator dispatch |
| `reopen-bead` verdict per [reconciliation/spec.md §4.5 RC-020] / RC-025 | `closed` → `open` | reopen | reconciliation verdict executor |
| Cat 3c auto-resolver per [reconciliation/spec.md §8.6] | `in_progress` → `closed` | close | reconciliation auto-resolver |
| Operator cancel / `ErrCanceled` (per [handler-contract.md §4.5]) | (no Beads write at MVH) | — | OQ-BI-004 tracks whether to reopen |
| Daemon startup orphan-sweep observes stale `in_progress` with no in-flight run reattachment AND no adapter intent file for close/reopen on this bead (per BI-010d / PL-006 extended per hk-iuaed.2) | `in_progress` → `open` | reset | daemon (startup orphan-sweep) |

> NOTE: `reset` is an op-name in the BI-010 op set (was `{claim, close, reopen}`; becomes `{claim, close, reopen, reset}`). Reset writes route through the §4.8 adapter and carry §4.10 idempotency keys identically to other terminal-transition writes; the idempotency-key formula is `<project_hash>:<bead_id>:reset:<daemon_start_ns>`.

**`deferred` and `tombstone` are operator-facing states harmonik does NOT write at MVH.**

- A bead in `deferred` or `tombstone` status MUST be rejected by submit-time validation per §4.5a BI-013b (mapping through [queue-model.md §6 QM-021]); `br ready`'s native exclusion of these statuses remains available to orchestrator agents per §4.9 BI-027.
- Harmonik MUST treat an in-flight run whose bead's status transitions to `tombstone` mid-run (observed via reconciliation's periodic re-read) as a Cat 3 reconciliation flag per [reconciliation/spec.md §8.4].
- A bead transitioning to `deferred` mid-run is treated identically (Cat 3) until OQ-BI-004 settles operator-deferred-mid-run semantics.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-010b — Reconciliation-driven writes

Reconciliation auto-resolvers (Cat 3a, 3b, 3c per [reconciliation/spec.md §8.4a, §8.5, §8.6]) MAY fire `close` or `reopen` writes to reconcile Beads toward git. These writes MUST route through the §4.8 adapter and MUST carry the §4.10 idempotency-key infrastructure. A reconciliation-driven write's emission corresponds to `reconciliation_verdict_executed` per [event-model.md §8.6] (NOT `run_completed`). BI-INV-001's "no intra-run writes" prohibition continues to apply: reconciliation writes are run-terminal-equivalent, not intra-run.

Tags: mechanism

#### BI-010c — Workflow-mode label write discipline

Agents MUST NOT add, remove, or modify `workflow:<...>` labels per BI-009a via `br update` (or any equivalent label-mutation surface) from inside a workflow run. The label is operator-set or set at bead-creation time only. A daemon-side OR reconciliation-side label write is permitted ONLY where a workflow's design intent explicitly so dictates (e.g., a self-modifying reconciliation routine), and any such write MUST route through the §4.8 adapter and MUST carry the §4.10 idempotency-key infrastructure exactly as terminal-transition writes do.

The Beads-CLI skill per §4.9 BI-027 MUST document this prohibition so the rule appears in every agent's launch context per [handler-contract.md §4.11]. An agent observed to have issued a `workflow:<...>` label mutation from within a run is a conformance violation under BI-INV-001 — the label is treated as run-state-shape, and intra-run writes to bead state are forbidden.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-010d — Claim is an activity-marker write

A `claim` write (`open` → `in_progress`) asserts "daemon is currently working this bead." It is observability surface for `bv` dashboards and operators — NOT a load-bearing truth claim about work completion. Recovery from a stale claim (daemon crashed mid-run; no terminal event landed; no adapter intent file for a close or reopen on this bead) is an auto-resettable condition: the orphan-sweep duty of PL-006 (as extended by the sibling bead hk-iuaed.2) MUST reset the bead back to `open` via a `reset` write (`in_progress` → `open`). The reset write routes through the §4.8 adapter and carries a §4.10 idempotency key with `op=reset`.

The `reset` op is listed in the BI-010a status-mapping table (see below). Its inclusion in the BI-INV-001 permitted-write set is acknowledged in that invariant's prose.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-010e — Child-bead-spawn creates (agent-issued)

An implementer agent running inside a harmonik worktree MAY call `br create` to spawn child beads (e.g., a research bead decomposing into N task beads). Child-bead-spawn creates are permitted intra-run writes per BI-011.

**Constraints on child-bead creates:**

1. **Lineage label required.** Every child bead MUST carry the label `parent:hk-<parent-id>` (where `<parent-id>` is the parent bead's ID). This label is the orphan-sweep anchor for Cat-BL1 reconciliation; without it, discarded-run orphans cannot be mechanically identified.

2. **Idempotency check required.** Before issuing `br create`, the agent MUST check for existing child beads: `br list --label=parent:hk-<parent-id>`. If an expected child already exists (same title or equivalent), the agent MUST skip the create. `br create` has no native deduplication; the agent is the idempotency guard.

3. **Terminal transitions remain daemon-only.** Child beads start with status `open`. The daemon closes them via the normal terminal-transition adapter (BI-010). Agents MUST NOT call `br close` on child beads from inside a worktree.

4. **Merge safety.** The bead-ledger union merge-driver (BL-MRG-002) naturally preserves child-bead creates: a create is a new ID present in the worktree's JSONL but absent from main's — the driver's union algorithm takes it unconditionally (the "present in only one side beyond ancestor" branch). No additional daemon action is required at merge time for creates.

Tags: mechanism

#### BI-011 — Permitted and prohibited intra-run writes to Beads

Harmonik MUST NOT write per-node workflow transitions, outcome details, or fine-grained failure types to Beads. Intra-run state MUST live in the git checkpoint trail per [execution-model.md §4.4] and the JSONL event log per [event-model.md §6.2]. Writing every intra-run micro-transition to Beads is forbidden because it would thrash Beads's `blocked_issues_cache` and flood other Beads consumers.

**Permitted intra-run write categories:**

| Category | Who issues | Write op | Constraints |
|----------|-----------|----------|-------------|
| `claim` | Daemon (pre-spawn) | `br update --status=in_progress` | Existing; routes through adapter per BI-012; idempotency per BI-029 |
| `child-bead-spawn` | Implementer agent (inside worktree) | `br create` | New (BI-010e); MUST include `parent:hk-<parent-id>` label; MUST check for existing child beads before creating |
| `parent-bead-label` | Implementer agent (inside worktree) | `br update` (labels or notes only) | Informational only; MUST NOT change status |

All other writes from inside a worktree run are prohibited. Terminal transitions (`br update --status=closed`, `br update --status=failed`, `br close`) from inside worktrees violate BI-010 and MUST NOT be issued by agent code.

**Failure contract.** If an agent issues a prohibited terminal write from inside a worktree, the daemon's post-merge `br sync --import-only` (BL-MRG-004) will re-import the union-merged JSONL, and the daemon's terminal-close via the §4.8 adapter runs on top. Net effect: the prohibited write MAY persist if the merge-driver's `updated_at` LWW happened to favor the worktree row. This risk is pre-existing (acknowledged in BI-010); it is documented here for explicitness.

Tags: mechanism

#### BI-012 — All terminal-transition writes route through the `br`-CLI adapter

Every terminal-transition write (§4.4.BI-010) MUST route through the `br`-CLI adapter layer (§4.8) and MUST be subject to the idempotency contract of §4.10. A direct `br` subprocess invocation that bypasses the adapter for a status-change operation is a structural violation.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.5 Harmonik read surface

#### BI-013 — Ready-work query (orchestrator-facing)

The orchestrator agent uses `br ready` (or its equivalent command) to discover the set of beads whose dependencies are satisfied and whose status is `open`. The query's result is an input to the orchestrator's queue-planning surface per [queue-model.md §1 — Scope and terms]. The daemon MUST NOT consume `br ready` as a dispatch input; the daemon's dispatch input is the submitted queue per [queue-model.md §8 QM submit].

When the orchestrator (or any agent) invokes `br ready`, the response payload MUST surface each bead's labels array — including any `workflow:<mode>` label per BI-009a — so the orchestrator's planning surface can extract and apply the per-task workflow-mode override at submit time. This is a read-path observation that exploits the existing `br ... --format json` label exposure (per BI-025b); no new query surface is introduced. The daemon re-reads the bead at submit-validation time per §4.5a BI-013b; that read also surfaces labels.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-013a — `needs-attention` beads are rejected at submit-time

Beads carrying a `needs-attention` label MUST NOT be accepted into a submitted queue. The submit-time validation contract of §4.5a (BI-013b) MUST reject any `bead_id` whose live record carries the label, returning a typed validation failure per [queue-model.md §6 QM-021]. The `br`-CLI adapter's `br ready` output remains a faithful ledger view — it MAY include `needs-attention` beads if Beads itself returns them — and downstream consumers (orchestrator agents, operator tooling) are responsible for filtering them per their own policy.

The label is set by the daemon when a `review-loop` run hits the iteration cap per [execution-model.md §4.3] (and by analogous operator-drain semantics per [operator-nfr.md §4.3]); its presence asserts that operator triage is required before re-dispatch. An operator who clears the label restores the bead to the submittable set on the next queue-submit.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-013d — Ready-work query MUST use `--sort priority`

When the adapter invokes `br ready`, it MUST pass `--sort priority`. The `br` default sort policy is `hybrid`, which weights bead age into ranking and can place a lower-priority bead ahead of a higher-priority bead when the lower-priority bead is sufficiently older. The daemon's br-ready fallback path selects `readyRecords[0]` as the claim candidate and therefore requires strict priority ordering: a bead at priority P MUST appear before any bead at priority P+1 in the returned slice, with `br`'s internal `created_at` tie-break applied within a priority class.

Implementations MUST NOT rely on the default `hybrid` sort. Pinning `--sort priority` is non-negotiable because the first-element-pick pattern cannot tolerate age-weighting promotion of lower-priority beads.

*Regression note:* hk-rp48p — the daemon claimed a P1 bead while a P0 bead was simultaneously ready. Root cause: default hybrid sort promoted the older P1 above the P0. Fix: pin `--sort priority`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-014 — Dependency-graph query

The daemon and agents MUST be able to read the typed-edge set for a bead to determine its parents, children, and blockers. This query informs branching per [workspace-model.md §4.2] and is also consumed by reconciliation investigators.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-015 — Bead-detail query

The daemon and agents MUST be able to query a bead's title, description, status, edges, and audit trail given a stable bead ID.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-016 — Reconciliation queries

During the daemon's startup sequence per [process-lifecycle.md §4.2] steps 3–4, the daemon MUST be able to read Beads's audit log and current status for every in-flight bead. These queries are read-only and feed the reconciliation detectors of [reconciliation/spec.md §4.3].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-014a — Orphan `br` subprocess sweep on daemon startup

On daemon startup, the orphan sweep of [process-lifecycle.md §4.2 PL-006] MUST enumerate processes whose binary path matches the pinned `br` location and whose parent PID is 1 (re-parented to init). For each match, the adapter MUST send SIGTERM and wait up to 5s, then SIGKILL, mirroring the BI-025c termination discipline. Orphan `br` subprocesses surviving the sweep are a Cat 0 prerequisite failure (SQLite WAL contention) per [reconciliation/spec.md §8.1].

Cross-spec coordination request to PL: extend PL-006 orphan-sweep enumeration to include `br` subprocesses (alongside tmux sessions, worktree locks, agent subprocesses, and intent files). Tracked as new OQ-BI-010.

Tags: mechanism

### 4.5a Submit-time validation read surface

Under extqueue, the daemon's dispatch input is the externally-submitted queue per [queue-model.md §8 QM submit]. Before a queue-submit (or queue-append, or queue-dry-run) is accepted, the daemon MUST validate every referenced bead against the live ledger. This section names the daemon's read surface for that validation; the rules themselves (existence, status, no-double-dispatch, no-duplicate, append-target validity, parallelism-narrowed surfacing) live in [queue-model.md §6 QM-020..QM-026] and are NOT restated here.

#### BI-013b — Submit-time bead read uses `br show`

For every `bead_id` named in a `queue-submit` / `queue-append` / `queue-dry-run` request, the adapter MUST invoke `br show <bead_id> --format json` (per BI-025b) and parse the result. The read is subject to the BI-025c 5 s read-timeout discipline and the BI-025a `BrError` taxonomy. The parsed record supplies the inputs the validation rules of [queue-model.md §6 QM-020 (existence)], [queue-model.md §6 QM-021 (status)], and [queue-model.md §6 QM-022 (no-double-dispatch)] consume. The label set on the returned record is the input the validation rule of §4.5 BI-013a (`needs-attention` rejection) consumes.

The validation MUST NOT mutate Beads. Submit-time reads are read-only; BI-INV-001 continues to apply.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-013c — Pre-claim status re-read

Between the dispatcher's selection of a queue item and the `claim` write to Beads, the daemon MUST re-read the bead's status via `br show <bead_id>` and confirm `status = open`. If the re-read returns a non-open status, the daemon MUST skip the claim, emit `bead_claim_skipped{bead_id, observed_status, reason="status_changed_between_select_and_claim"}` per [event-model.md §8.8], and return the item to its group with status `deferred-for-ledger-dep` per [queue-model.md §6 QM-022] (when the change indicates an external claim by another daemon process — disallowed per BI-009 and §4.8a BI-025e operator note — or a status flip to `closed` / `tombstone` / `deferred`).

This requirement names the ShowBead pre-claim guard previously carried as implementation-only in the daemon workloop. Under extqueue, the guard is load-bearing for the queue's exactly-once dispatch guarantee per [queue-model.md §6 QM-022] and warrants a normative anchor.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.6 Bead-ID propagation

#### BI-017 — Run metadata records `bead_id`

A run dispatched against a bead MUST record its `bead_id` in the run metadata per [execution-model.md §6.1 Run]. A run not tied to any bead MUST leave the field unset.

Tags: mechanism

#### BI-018 — Checkpoint commits for bead-bound runs carry the bead-ID trailer

For every checkpoint commit on a bead-bound run's task branch, the trailer `Harmonik-Bead-ID: <bead_id>` MUST be present per [execution-model.md §6.2]. For a checkpoint commit on a non-bead-bound run, the trailer MUST be absent.

Tags: mechanism

#### BI-019 — Bead-scoped event payloads carry `bead_id`

Every event emitted for a bead-bound run MUST carry the optional `bead_id` field on its payload per [event-model.md §6.3]. Events that are not scoped to a specific run (e.g., daemon lifecycle) MUST omit the field.

Tags: mechanism

#### BI-020 — Session logs for bead-bound runs carry `bead_id` metadata

Session logs produced for an agent subprocess running as part of a bead-bound run MUST carry `bead_id` metadata per [workspace-model.md §4.7]. CASS indexing uses this metadata for join-to-Beads queries.

The per-run session sidecar (per [workspace-model.md §4.7 WM-026]) MAY additionally carry the resolved `workflow_mode` for the run (the tier that won the four-tier resolution chain, not just the raw bead label) alongside `bead_id` for audit and reconciliation use. The on-disk shape of the sidecar field is owned by workspace-model; this spec asserts only that the resolved value, when carried, MUST match the mode the daemon dispatched the run under.

Tags: mechanism

### 4.7 Store-authority rules

#### BI-021 — Beads is authoritative for bead content and coarse status

If the daemon's in-memory cache and Beads disagree on a bead's title, description, type, or coarse status, Beads MUST win. Harmonik MUST reconcile its cache to Beads, not the other way around.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-022 — Git is authoritative for completion

If Beads reports a bead as `closed` but no merge commit with `Harmonik-Bead-ID: <bead_id>` exists in the project's git history, the divergence MUST be classified as a reconciliation flag per [reconciliation/spec.md §8] Cat 3 and MUST NOT be silently auto-reconciled into git's direction. Beads status is corrected (via the §4.4 write surface, routed through the §4.10 adapter) only after the investigator's verdict lands or after a Cat 3c auto-resolver fires per [reconciliation/spec.md §8.12].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### BI-023 — JSONL is observational only

The JSONL event log MUST NOT be used to override Beads or git. JSONL reads for divergence-evidence detection during reconciliation are permitted per [reconciliation/spec.md §4.3], but JSONL MUST NOT drive a write back to Beads except through the §4.4 write surface triggered by an investigator verdict or a Cat 3 auto-resolver.

Tags: mechanism

> INFORMATIVE — Cat 3a/3c auto-resolver actions in BI terms. **Cat 3a** (intent-log present, audit gap) → the adapter reissues the write per BI-031's status-check-before-reissue protocol, so the auto-resolver's output is "let the adapter re-drive its own pending intent" rather than a fresh write. **Cat 3c** (Beads `in_progress` but git carries a matching merge commit) → the auto-resolver fires a `close` write via BI-010b, routed through the §4.8 adapter and the §4.10 idempotency contract identically to a daemon-driven close. See [reconciliation/spec.md §4.3] / [reconciliation/spec.md §8.4a] / [reconciliation/spec.md §8.6].

### 4.8 Version-pin + adapter layer

#### BI-024 — Beads version is pinned per harmonik release

A harmonik release MUST name the Beads version it tested against. Upgrading the Beads dependency MUST require a harmonik release that has verified compatibility with the new Beads version, MUST be accompanied by an adapter change for backwards-incompatible Beads changes per §4.8 BI-026. Beads is pre-1.0; silent upgrades are forbidden.

The compatibility window between a pinned Beads version `pinned` and an observed Beads version `observed` is defined as exact-match at MVH: `isCompatible(pinned, observed) ≡ pinned == observed`. Patch-level compatibility (e.g., `pinned=0.5.2` accepting `observed=0.5.3`) is NOT supported at MVH; operators MUST upgrade harmonik in lockstep with Beads. Post-MVH widening to a semver-range compatibility window is tracked as new OQ-BI-011.

The check is performed by BI-024a `--version` handshake; mismatch fails daemon startup with §8 code 8 (`beads-unavailable`) per [operator-nfr.md §8].

Tags: mechanism

#### BI-025 — `br`-CLI adapter is the sole translation layer

All Beads interactions from harmonik code MUST route through a single `br`-CLI adapter module. The adapter's responsibility is to translate harmonik's typed queries and writes into `br` subprocess invocations and to parse `br` output into typed results. A breaking change in Beads MUST produce exactly one adapter change; no scattered per-callsite updates are acceptable.

The adapter MUST expose an injectable `br` binary path via constructor parameter (default: resolved from PATH at startup); unit tests MAY substitute a mock `br` binary at the injected path. Production callers MUST NOT inject; the constructor parameter is for testability per [docs/foundation/spec-template.md] §10.2 contract-tests.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-026 — Harmonik absorbs breakage rather than forking Beads

On a backwards-incompatible Beads change, harmonik MUST either (a) remain pinned to the prior Beads version and delay upgrade, or (b) ship a harmonik release with an adapter change that handles the new surface. Forking Beads to patch is forbidden.

Tags: mechanism

### 4.8a `br` CLI surface contract

#### BI-024a — `br --version` handshake

The adapter MUST invoke `br --version` at daemon startup (during PL-005 step 4, the Cat 0 prerequisite pre-check where Beads/`br` availability is verified). The handshake MUST complete BEFORE the daemon accepts its first `queue-submit` RPC per [process-lifecycle.md §4.4 PL-003a], so that submit-time validation (§4.5a BI-013b) does not run against an incompatible `br` version. The adapter MUST compare the parsed version against the pinned version declared in the harmonik release manifest per BI-024. The version output MUST match the regex `br\s+(\d+)\.(\d+)\.(\d+)(?:[-.][a-zA-Z0-9]+)?`. A version mismatch outside the compatibility window of BI-026, OR an unparseable output, MUST fail daemon startup with exit code 8 (`beads-unavailable` per [operator-nfr.md §8]) and emit `daemon_startup_failed{failure_mode="br-version-incompatible"}`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-025a — `br` exit-code taxonomy

The adapter MUST classify every `br` invocation's exit code into the harmonik-internal `BrError` enum: `{BrOK=0, BrNotFound, BrConflict, BrDbLocked, BrSchemaMismatch, BrUnavailable, BrOther}`. The mapping rule is declared in §6.1a. An unrecognized exit code MUST produce `BrOther` AND MUST emit `divergence_inconclusive` per [event-model.md §8.6.10] with `reason=authority_unavailable` (the `br` exit code cannot be authoritatively classified) so reconciliation can observe adapter-vs-Beads drift via the inconclusive-evidence path of EV-023a.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-025b — JSON output mode mandatory

The adapter MUST invoke every `br` command with `--format json` (or whatever flag the pinned Beads version exposes for structured output). The adapter MUST NOT parse `br` text output; commands lacking a JSON output mode MUST be fenced off (the adapter does not call them) until Beads adds JSON support. Parse failures of structured output MUST classify as `BrSchemaMismatch`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-025c — `br` subprocess timeout discipline

Every adapter invocation of `br` MUST be bounded by a subprocess wall-clock timeout: 5 s for read commands (default; operator-tunable per [operator-nfr.md §4.9]), 10 s for write commands (default; operator-tunable). Timeout expiry MUST classify as `BrUnavailable`. The 5 s read-command default applies to all adapter reads, including the submit-time `br show` per §4.5a BI-013b.

On timeout expiry, the adapter MUST terminate the `br` subprocess via the standard SIGTERM-then-SIGKILL discipline of [handler-contract.md §4.3 HC-018]: send SIGTERM, wait up to 5 seconds for the subprocess to exit, send SIGKILL if still running, then `cmd.Wait()` to reap. The watcher goroutine per [process-lifecycle.md §4.5 PL-014]'s `cmd.Wait()` reap discipline applies. A SIGTERM-then-SIGKILL'd subprocess MUST classify as `BrUnavailable` (NOT `BrOther`); the adapter's intent log retains the entry for crash-recovery per BI-031.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-025d — stderr capture

The adapter MUST capture `br` stderr fully on every invocation, bounded by a maximum capture size of 1 MiB (truncate with explicit suffix `[...stderr truncated at 1 MiB]` if exceeded). On non-zero exit, the captured stderr (truncated as needed) MUST be included in the typed error structure surfaced to the daemon and to operator-facing diagnostics per [operator-nfr.md §4.1 ON-002]. Stderr is informational; the adapter MUST NOT parse it for state.

Specific stderr scenarios:

- `br` exit 0 with non-empty stderr (warnings on stdout-success): the adapter MUST classify as `BrOK`, attach the captured stderr to the structured-log emission per ON-035, and surface it for operator visibility but MUST NOT block the success path.
- `br` exit non-zero with empty stderr: the adapter MUST classify per BI-025a's exit-code mapping and attach a placeholder "(empty stderr)" string to the typed error.
- Rust panic exit (typically exit 101 with stderr containing `thread 'main' panicked at`): classify as `BrOther` (per BI-025a) with the panic stderr captured.
- Argparse error (exit 2 by Rust's clap convention) with usage text on stderr: classify as `BrOther`; the operator-facing error MUST surface the argparse text for triage.
- Partial stderr captured up to subprocess-kill (BI-025c timeout path): include partial stderr in the typed error with explicit truncation marker.

Tags: mechanism

#### BI-025e — Concurrent `br` invocation discipline

The adapter MAY invoke `br` concurrently from multiple daemon goroutines for read operations. SQLite WAL mode accommodates concurrent readers without contention.

**Terminal-transition writes (BI-010 claim/close/reopen/reset/sweep-close) MUST be serialized** via an adapter-internal mutex (`Adapter.terminalMu`). Dogfood incident hk-hdbls showed that ≥3 simultaneous `br close` calls exhaust the SQLite `.write.lock` (30 s busy timeout), producing `"OpenWrite could not open storage cursor root page"` failures and beads stuck `in_progress` even when the underlying merge succeeded. Serializing write-side `br` invocations within the daemon process eliminates the thundering-herd on the write lock. The intent-log idempotency (BI-029/BI-030) remains correct across the serialized writes.

Read operations (BI-013, BI-014, BI-015, BI-016, BI-013b, BI-013c) are NOT gated by the mutex — reads remain concurrent. On concurrent-write contention after the mutex (e.g. from an external process such as `captain`), `br` returns `BrDbLocked`; the adapter retries per BI-025c retry policy.

Operators running multiple harmonik daemons against the SAME Beads SQLite store is unsupported per [process-lifecycle.md §4.1] (per-project daemon model); detection of multi-daemon-same-Beads is tracked as OQ-BI-012.

Tags: mechanism

> INFORMATIVE — Adapter logging routes through ON-035. The adapter emits its own debug/warn/error logs through the structured-log surface of [operator-nfr.md §4.9 ON-035] with `subsystem=beads-adapter`. The adapter MUST NOT write to stderr directly except in pre-`daemon_started` startup failures (per PL-008a).

### 4.8b Bead-Ledger Merge Contract (BL-MRG)

> This section defines the git-level merge strategy for `.beads/issues.jsonl` in harmonik worktrees. It supersedes the lossy `git checkout --theirs .beads/issues.jsonl` workaround previously documented in HANDOFF.md.

#### BL-MRG-001 — Merge driver registration

`.gitattributes` MUST contain:

```
.beads/issues.jsonl merge=beads-union
```

`.git/config` (local to the repo) MUST configure the driver:

```
[merge "beads-union"]
    driver = harmonik beads-merge %O %A %B %P
    name = Bead Ledger Union Merge
```

The daemon MUST ensure the git config entry is present at startup (auto-configure if absent). `.gitattributes` is repo-tracked and will be present after merging the implementing PR.

Tags: mechanism

#### BL-MRG-002 — Driver algorithm (union-by-ID)

`harmonik beads-merge` MUST implement union-by-ID merge:

1. Parse `%O` (ancestor), `%A` (ours/main), `%B` (theirs/run-branch) as `map[id]row`.
2. For each ID in `union(O, A, B)`:
   - Present in only one of A or B beyond O → take it (covers child-bead-spawn creates and deletes).
   - Present in both A and B and equal → take either.
   - Present in both and differ → pick row with larger `updated_at`.
3. **Array field union.** For `labels` and `dependencies` arrays, perform set-union of both A and B values (not LWW on the whole array). Rationale: `br sync --merge` treats these fields as opaque LWW; concurrent label/dependency additions on both sides would drop one side. The driver must compensate with explicit union. Note: label removals on one side are respected if the removal moves the ID entirely off that side; simultaneous add-on-A + remove-on-B of the same label is resolved by including the label (additive-bias).
4. Write rows back ID-sorted to `%A`, exit 0.
5. On any semantic conflict per BL-MRG-003, emit a log line and continue (never exit non-zero from this driver).

Tags: mechanism

#### BL-MRG-003 — Semantic conflict logging

A semantic conflict occurs when the same bead exists in both A and B (beyond O), both differ from O, and both differ from each other — with the resolution being non-obvious (e.g., same bead closed on A, reopened on B within the same `updated_at` second). For such cases:

- Pick A (ours/main) as the winning row.
- Append a line to `.beads/merge-conflicts.log`:
  `<iso8601-timestamp> CONFLICT bead=<id> field=status a=<A_value> b=<B_value> resolution=took-ours`
- Exit 0 (never block the merge for a semantic conflict).

The reconciliation investigator reads `.beads/merge-conflicts.log` to surface audit items per Cat-BL3 (reconciliation/spec.md §8.BL3) and MUST emit a `bead_ledger_conflict_audit` event for each batch read (event-model.md §8.15.2).

Tags: mechanism

#### BL-MRG-004 — Post-merge SQLite refresh

After any rebase or merge that touches `.beads/issues.jsonl`, the daemon MUST call:

```
br sync --import-only
```

in the main repo's working directory, before any subsequent `br` operations (e.g., the terminal-transition `br close` for the completed bead). This ensures the daemon's SQLite reflects the union-merged JSONL state.

If `br sync --import-only` fails, the daemon MUST emit `bead_sync_failed` to `.harmonik/events/events.jsonl` and route to Cat-BL2 (reconciliation/spec.md §8.BL2) rather than silently continuing.

Tags: mechanism

#### BL-MRG-005 — Removal of `mergeRebaseAutoResolveBeadsLedger` workaround

With BL-MRG-001 in effect, `internal/daemon/workloop.go:mergeRebaseAutoResolveBeadsLedger` MUST be removed. That function's `git checkout --theirs .beads/issues.jsonl` override suppresses the registered merge driver and reintroduces lossy behavior. The driver runs automatically during `git rebase` and `git merge` when `.gitattributes` is configured per BL-MRG-001.

Tags: mechanism

#### BL-MRG-006 — Phase 2 migration path (informative)

Phase 2 enables full shared-DB mode where worktree agents point `BD_DB` at main's `beads.db`. Phase 2 resolves the stale-at-fork problem (agent's `br show <parent>` may fail if the parent bead was created on main after the worktree was forked). Phase 2 requirements (not yet normative; tracked as a follow-up bead):

- Daemon MUST set `BD_DB=<main-repo>/.beads/beads.db` in the worktree agent subprocess environment.
- Daemon MUST set `BR_LOCK_TIMEOUT=5000` (5 s) alongside `BD_DB`. Rationale: SQLite WAL mode has `busy_timeout=0` by default — concurrent write contention causes immediate `SQLITE_BUSY` failure without a retry window. 5 s is sufficient for the sparse concurrent write pattern in practice.
- Agents MUST NOT call `br sync` with Phase 2 active (daemon owns the flush cycle; `br sync --flush-only` from an agent would write JSONL into main's working tree mid-run).
- BL-MRG-001–005 are no-ops for Phase 2 worktrees (no JSONL in worktree to merge).
- Phase 2 does not affect beads whose worktrees use Phase 1 (the driver); mixed-mode is safe since Phase 2 worktrees have no JSONL to conflict.

Tags: informative

### 4.9 Beads-CLI skill

#### BI-027 — The Beads-CLI skill is the agent-facing access path

The Beads-CLI skill MUST be the only mechanism by which an agent subprocess invokes `br`. The skill's authoritative location is declared in [control-points.md §4.11] and documented in `docs/components/external/beads.md` (cited, not defined here). The skill MUST document: `br` command surface, output formats, idiomatic `jq` pipelines, and the harmonik write discipline (agents MUST NOT issue terminal-transition `br` writes outside the harmonik workflow path; the daemon owns those writes per §4.4).

Tags: mechanism

#### BI-028 — Every agent in a harmonik run has the Beads-CLI skill by default

Per [handler-contract.md §4.11], every agent operating in a harmonik run MUST have the Beads-CLI skill available in its launch context unless a role-specific permission set explicitly excludes it (an unusual policy decision logged in the node's YAML policy per [control-points.md §6.3]).

Tags: mechanism

### 4.10 `br`-adapter idempotency — terminal-transition writes

> INFORMATIVE — Intent-log directory ownership. `.harmonik/beads-intents/` is BI-owned. Operator-nfr clean-install / cleanup protocols MUST preserve this directory and any intent files for crash recovery; cite [operator-nfr.md §4.10] coordination.

> INFORMATIVE — Intent-log scope is unchanged under extqueue. The intent-log discipline of BI-029 / BI-030 covers ONLY terminal-transition writes to Beads (`op ∈ {claim, close, reopen}`). Queue submissions, appends, removes, pauses, and resumes are daemon-internal state mutations that are NOT Beads writes; they are persisted under the separate `.harmonik/queue.json` discipline of [queue-model.md §3 QM-001..QM-003]. The two on-disk contracts coexist in `.harmonik/` and are independent. BI-INV-001 ("no intra-run writes") continues to apply to Beads writes only; it does NOT constrain queue mutations.

#### BI-029 — Terminal-transition writes carry a deterministic idempotency key

The `br`-CLI adapter (§4.8) MUST derive an idempotency key for every terminal-transition write (§4.4.BI-010) using the formula `<run_id>:<transition_id>:<op>` where `op ∈ {claim, close, reopen}`. The key MUST be deterministic: identical (run_id, transition_id, op) inputs produce identical keys across invocations.

Tags: mechanism

#### BI-030 — Pre-write intent log with fsync durability

Before invoking `br` for a terminal-transition write per BI-029, the adapter MUST atomically materialize an intent-file via the temp-rename pattern:

1. Write the `IntentLogEntry` per §6.1 to `.harmonik/beads-intents/<idempotency_key>.json.tmp-<rand>` (random suffix prevents collision under concurrent recovery).
2. `fsync(temp_fd)`.
3. `rename(2)` to `.harmonik/beads-intents/<idempotency_key>.json`. The rename is atomic at the filesystem layer.
4. `fsync(parent_directory_fd)` — REQUIRED to ensure the directory entry is durable. Power-loss after step 3 without this can lose the rename on APFS / ext4-data=ordered.
5. Then invoke `br`.

On successful `br` return, the adapter MUST delete the intent file:

1. `unlink(intent_file)`.
2. `fsync(parent_directory_fd)` — REQUIRED to ensure the deletion is durable. Without parent-dir fsync, a power-loss after unlink can leave the intent file visible on remount, triggering false-positive Cat 3a detection.

The pattern MUST match the [workspace-model.md §4.7 WM-026] sidecar atomicity discipline.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### BI-031 — Idempotent crash-recovery via status-check-before-reissue

On daemon startup (PL-005 step 4 Cat 0 pre-check completing successfully and BI-024a version handshake passing), the adapter MUST scan `.harmonik/beads-intents/` for stale intent files (intent files older than the current daemon's `started_at` per [process-lifecycle.md §4.2]) and execute the recovery sequence below for each.

Reconciliation's Cat 3a auto-resolver per [reconciliation/spec.md §8.4a] does NOT directly invoke the adapter's recovery path; instead, Cat 3a is the post-emergence detection layer for divergences the adapter could not resolve (per BI-031 step 3ii / step 4f routing to `divergence_inconclusive`). The adapter's startup recovery and reconciliation's Cat 3a are layered, not concurrent.

Recovery sequence:

1. Read the intent file's recorded transition fields: `op`, `bead_id`, `idempotency_key`, `intended_post_state`.
2. Query Beads via `br show <bead_id>` (using the timeout discipline of BI-025c and the JSON mode of BI-025b) to read the bead's current `coarse_status`.
3. If the current status equals the `intended_post_state` for this transition (i.e., the prior write landed before crash OR a concurrent writer landed it), the recovery MUST attempt to disambiguate:
   (3i) If `br audit-log <bead_id> --filter-idempotency-key <idempotency_key>` (or equivalent surface — see OQ-BI-009) returns a matching audit entry, the prior write was harmonik-side. Delete the intent file (with parent-directory fsync per BI-030) and write the structured-log recovery record per [operator-nfr.md §4.9 ON-035] at level=info with `subsystem=beads-adapter`, `msg="terminal-transition recovered"`, and `fields={idempotency_key, op, bead_id, recovery_path: "status_match"}`. Adapter recovery is observability surface, not a state-mutation event. Recovery is a confirmed no-op.
   (3ii) If no matching audit entry exists OR the `br audit-log` surface is unavailable on the pinned Beads version, the recovery cannot prove harmonik-side authorship of the post-state. The adapter MUST classify this as a Cat 3a torn-write per [reconciliation/spec.md §4.3 RC-014] / [reconciliation/spec.md §8.4a] and emit `divergence_inconclusive` per [event-model.md §8.6.10] with `reason=authority_unavailable` (the adapter's single-source observation cannot corroborate against another store; reconciliation Cat 6a/Cat 3 detectors per [reconciliation/spec.md §4.3] are the multi-store corroboration layer); the intent file MUST be retained for reconciliation's auto-resolver to consume per RC-002a/RC-025.
4. If the current status is the pre-state (status_match negative; pre-state confirmed), re-issue the `br` write with the same `idempotency_key` (passed as `--idempotency-key` if the pinned Beads CLI supports it; otherwise as a positional metadata argument per the adapter's pinned-version contract). The reissue MAY return:
   (4a) `BrOK` — terminal transition completed successfully. Delete the intent file (with parent-directory fsync per BI-030) and write the structured-log recovery record per ON-035 as in step 3i.
   (4b) `BrConflict` — a concurrent writer landed the transition between step 2 and step 4. Re-execute step 3 (re-read current status); proceed via 3i/3ii based on the new state.
   (4c) `BrDbLocked` — Beads SQLite is busy. Retry up to 3 times with exponential backoff (initial 100ms, max 1s); on persistent failure, classify as `BrUnavailable` and route per (4d).
   (4c-transient) `BrUnavailable` from wall-clock timeout (subprocess killed by the BI-025c budget timer, NOT binary-missing/exec-error) — transient SQLite contention caused the `br` subprocess to exceed its write-timeout budget. Retry up to 10 times with exponential backoff (initial 50ms, max 2s per sleep); on persistent failure after 10 retries, escalate to the full `BrUnavailable` path per (4d). This sub-case is distinct from (4d): it is a transient contention burst, not a structural unavailability. The intent-log discipline (BI-029/BI-030) ensures idempotency across retries. The 10-retry budget was widened from 3 per dogfood run hk-75rij (hk-ekz5v) — 3 retries were insufficient for the tail of SQLite contention bursts under concurrent kerf/agent activity. Applies exclusively to terminal-transition writes (CloseBead, ClaimBead, ReopenBead, ResetBead); non-terminal-transition read paths still use the DBLockedRetryMax=3 budget.
   (4d) `BrUnavailable` — adapter cannot reach Beads (binary missing, exec error, or transient budget per (4c-transient) exhausted). Retain the intent file; classify the daemon as `degraded` per [operator-nfr.md §4.9 ON-037]; reconciliation Cat 0 retry per [process-lifecycle.md §4.3 PL-010] re-attempts the recovery.
   (4e) `BrSchemaMismatch` — the pinned Beads version's schema does not match. Classify as `divergence_inconclusive` per [event-model.md §8.6.10] with `reason=authority_unavailable` and route per BI-031b. Recovery cannot proceed under schema drift.
   (4f) `BrOther` (unrecognized) — emit `divergence_inconclusive` per [event-model.md §8.6.10] with `reason=authority_unavailable`; retain intent file; route as Cat 6b operator-escalation per [reconciliation/spec.md §8.11].
5. If the current status is neither pre-state nor post-state (Beads diverged), the divergence is a Cat 3a torn-write per [reconciliation/spec.md §4.3 RC-014] / [reconciliation/spec.md §8.4a]; the adapter MUST emit `divergence_inconclusive` per [event-model.md §8.6.10] with `reason=authority_unavailable` and route to reconciliation rather than reissuing.

The recovery is Beads-idempotency-independent: the post-state status check at step 3, with audit-log disambiguation when available, catches the prior-write-landed case without requiring Beads to expose an idempotency-key audit-log query as a hard prerequisite. Races in which Beads completes the write between step 2 and step 4 are observed via the `BrConflict` retry path (4b).

Under prolonged `BrUnavailable` (e.g., `br` binary missing for hours), the intent-log directory `.harmonik/beads-intents/` would grow unbounded. The adapter MUST emit a `daemon_degraded{reason=infrastructure_unavailable}` event per [event-model.md §8.7.5] when the intent-log directory exceeds 100 entries, and `harmonik status` per [operator-nfr.md §4.10] MUST surface the backpressure. Operators MUST resolve the underlying Beads-unavailability before the daemon can drain the intent log.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=recoverable-non-idempotent

#### BI-031b — `br show` JSON-consistency dependency

The status-check protocol of BI-031 MUST consume `br show <bead_id> --format json` per BI-025b. Any parse failure on the structured output MUST classify as `BrSchemaMismatch` per §6.1a, NOT as "status differs." A `BrSchemaMismatch` recovery path MUST emit `divergence_inconclusive` per [event-model.md §8.6.10] with `reason=authority_unavailable` and the `BrSchemaMismatch` reason captured in the structured-fields tag per [event-model.md §6.3], and refuse the reissue. The pinned-version handshake of BI-024a is the mechanism that prevents schema drift from arising in non-pathological configurations.

Tags: mechanism

#### BI-032 — Intent log is the Cat 3a detector's evidence source

The intent log (§4.10.BI-030) and Beads's audit log MUST be the evidence sources consumed by the Cat 3a torn-Beads-write detector per [reconciliation/spec.md §4.3]. This spec owns the intent-log shape and durability contract; [reconciliation/spec.md §8.12] owns the classification and auto-resolver.

Tags: mechanism

## 5. Invariants

#### BI-INV-001 — No intra-run writes to Beads

No harmonik code path MAY write to Beads at any run transition other than (i) the four terminal transitions named in §4.4 BI-010 / BI-010a / BI-010d (claim, close, reopen, reset), or (ii) the reconciliation-driven writes in BI-010b. Intermediate node outcomes, per-transition state changes, failure classes, and hook fire events MUST never produce a `br` status write. Queue mutations per [queue-model.md §3 QM-001..QM-003] are NOT Beads writes and are NOT constrained by this invariant.

> NOTE: `reset` is an activity-marker write (not a truth claim) and is auto-issued by the startup orphan-sweep per BI-010d; it is NOT a reconciliation-driven write (BI-010b) and is NOT an intra-run write. The intra-run prohibition continues to apply: `reset` fires only during startup, before any run is in flight on the resetting daemon.

Tags: mechanism
Sensor: corpus-wide reviewer-persona scan asserts no `br <state-change>` invocation appears outside the §4.10 adapter module; §10.2 BI-002 / BI-012 contract test asserts that the only `os/exec` site with `br` argv is the adapter; §10.2 cross-spec scenario test injects a non-terminal node and asserts no `br` write fires.

#### BI-INV-002 — Bead ID is stable across harmonik's lifetime

A bead's ID is stable from creation to tombstone. Every harmonik artifact that binds to a bead (run metadata, checkpoint trailers, event payloads, session-log metadata) MUST use the same ID across the entire bead lifetime. Harmonik MUST NOT mint harmonik-local alternate identifiers for the same bead.

Tags: mechanism
Sensor: §10.2 cross-spec test asserting bead-ID byte-equal across `Run.bead_id` per [execution-model.md §4.3 EM-014], checkpoint trailer per [execution-model.md §4.4 EM-017], event payload `bead_id` per [event-model.md §6.3], and session-log sidecar per [workspace-model.md §4.7]; reviewer-persona scan for any `mint_alternate_id`-style helper in harmonik code paths.

#### BI-INV-003 — Git wins on completion disagreement

On any disagreement between Beads's `closed` status and the absence of a merge commit in git bearing the matching `Harmonik-Bead-ID` trailer, git's view MUST be treated as authoritative for completion. Resolution MUST route through a reconciliation workflow or Cat 3 auto-resolver per [reconciliation/spec.md §8]; silent auto-reconciliation in git's direction is forbidden.

Tags: mechanism
Sensor: [reconciliation/spec.md §4.3 RC-013] Cat 3 detector rule (Beads-status disagreement with git completion); §10.2 BI-021..BI-023 scenario test injecting a Beads-`closed` / no-merge-commit divergence and asserting Cat 3 dispatch (NOT silent Beads-side correction).

#### BI-INV-004 — Beads status changes are auditable through harmonik's adapter or flagged as divergence

Every Beads status-change commit observed downstream (by the reconciliation Cat 3a detector per [reconciliation/spec.md §4.3 RC-014]) MUST be verifiable via the conjunction of (a) an idempotency-keyed intent log entry per BI-029/BI-030 (or its post-success absence), and (b) Beads's recorded transition. A status-change observed in Beads with NO matching intent-log entry (present or absented after success) MUST trigger Cat 3a per [reconciliation/spec.md §8.4a]: the prior write came from outside harmonik's adapter, violating BI-INV-001.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=recoverable-non-idempotent
Sensor: Cat 3a detector per [reconciliation/spec.md §4.3 RC-014] / [reconciliation/spec.md §8.4a]; adapter unit tests per §10.2 BI-029..BI-032 verify the keyed-intent-log discipline; cross-spec scenario test injects an out-of-band `br` write (bypassing the adapter) and asserts Cat 3a dispatch.

## 6. Schemas and data shapes

### 6.1 Record schemas

```
RECORD BeadRecord:
    bead_id          : String                 -- stable identifier for the bead's lifetime
    title            : String                 -- owned by Beads (§4.3 BI-005)
    description      : String                 -- owned by Beads
    bead_type        : String                 -- owned by Beads; harmonik treats as opaque enum
    status           : CoarseStatus           -- whatever Beads exposes; see ENUM CoarseStatus below (per BI-007 read surface)
    edges            : List<DependencyEdge>   -- typed dependency edges; see §6.1
    audit_trail_ref  : String                 -- opaque handle for `br` audit-log retrieval
```

```
ENUM CoarseStatus:                            -- Beads-owned and extensible; live enum at Beads v0.1.45 is:
    open
    in_progress
    blocked                                   -- read-surface only (NOT written by harmonik)
    deferred
    draft                                     -- read-surface only; harmonik-side readiness mechanism per BI-007
    closed
    tombstone
    pinned                                    -- read-surface only (NOT written by harmonik)
    -- Future Beads-side extensions accepted automatically.
```

```
ENUM HarmonikWriteStatus:                     -- the five-value subset harmonik MAY write per BI-007 + BI-010
    open
    in_progress
    closed
    deferred
    tombstone
-- NOTE: `reset` (BI-010d) is a TRANSITION op (in_progress→open), not a new status value.
-- HarmonikWriteStatus is unchanged; `reset` merely reuses the existing `open` post-state.
```

```
RECORD DependencyEdge:
    from_bead_id     : String                 -- source bead
    to_bead_id       : String                 -- target bead
    edge_kind        : EdgeKind               -- one of {parent-child, blocks, conditional-blocks, waits-for}
```

```
ENUM EdgeKind:
    parent-child
    blocks
    conditional-blocks
    waits-for
```

### 6.1a Adapter error taxonomy

```
ENUM BrError = OK | NotFound | Conflict | DbLocked | SchemaMismatch | Unavailable | Other
```

Concrete mapping table from `br` exit code to `BrError` (the table is illustrative; the pinned Beads version's exit-code surface is the authoritative source, and the adapter MUST be re-validated whenever BI-024's pinned version changes):

| `br` exit code | `BrError` | Meaning |
|---|---|---|
| 0 | OK | success |
| 1 | NotFound | bead-id not found |
| 2 | Conflict | concurrent claim or status collision |
| 3 | DbLocked | SQLite busy beyond timeout |
| 4 | SchemaMismatch | Beads schema or output format outside harmonik's compatibility window |
| (timeout) | Unavailable | subprocess wall-clock timeout per BI-025c |
| (exec error) | Unavailable | `br` not on PATH / fork failure |
| other | Other | unrecognized; emits `store_divergence_detected` per BI-025a |

The `BrError` enum is consumed by reconciliation's Cat 3a / Cat 0 detectors per [reconciliation/spec.md §4.3] and by the adapter's idempotency-recovery path per BI-031.

```
RECORD IntentLogEntry:
    idempotency_key     : String                 -- "<run_id>:<transition_id>:<op>" per §4.10 BI-029
    run_id              : UUID                   -- the harmonik run driving the write
    transition_id       : UUID                   -- the transition at which the write is emitted
    op                  : TerminalOp             -- one of {claim, close, reopen, reset}
    bead_id             : String                 -- target bead
    intended_post_state : CoarseStatus           -- derived from (op, current_pre_state); claim->in_progress, close->closed, reopen->open
    requested_at        : Timestamp              -- monotonic; RFC 3339 wall clock
    schema_version      : Integer                -- N-1 readable per [operator-nfr.md §4.5]
```

```
ENUM TerminalOp:
    claim    -- activity-marker write (open→in_progress); auto-resettable per BI-010d
    close    -- truth-claim write (in_progress→closed)
    reopen   -- truth-claim write (closed→open)
    reset    -- activity-marker write (in_progress→open); startup orphan-sweep only per BI-010d
```

> INFORMATIVE: The intent log is a directory of one JSON file per pending operation, keyed by `idempotency_key`. The adapter creates the file before the `br` call and deletes it on success. After a crash, the remaining files describe exactly the set of writes whose completion is ambiguous.

### 6.2 On-disk layout for intent log

The intent log lives under `.harmonik/beads-intents/`. File naming:

```
.harmonik/beads-intents/<idempotency_key>.json
```

Where `<idempotency_key>` is the string from §4.10.BI-029, URL-safe. Colons are permitted in filenames on supported filesystems; on filesystems that forbid colons, the adapter is allowed to encode them as `_` (see OQ-BI-003).

### 6.3 Schema evolution

`IntentLogEntry` carries a `schema_version` integer. The compatibility contract is N-1 readable per [operator-nfr.md §4.5]. Additive changes (new optional field) are non-breaking; renaming or removing fields is breaking and requires a migration release.

`BeadRecord` fields are owned by Beads; harmonik's consumption is read-only, and any Beads schema change is handled by adapter update per §4.8.BI-025.

### 6.4 Co-owned event payloads

This spec's requirements drive inclusion of the `bead_id` field on the following events whose payload schemas are declared in [event-model.md §6.3]:

- `run_started` — `bead_id` present iff the run is bead-bound (§4.6.BI-019).
- `run_completed` — `bead_id` present iff the run is bead-bound.
- `run_failed` — `bead_id` present iff the run is bead-bound.
- `checkpoint_written` — `bead_id` present iff the run is bead-bound.
- `store_divergence_detected` — carries `bead_id` when divergence is scoped to a specific bead.
- `bead_claim_skipped` — carries `bead_id`, `observed_status`, and `reason` per §4.5a BI-013c.

This spec is normative for WHEN `bead_id` appears on a payload; [event-model.md §6.3] is normative for the shape of each event.

## 8. Error and failure taxonomy

This spec's failure taxonomy is the `BrError` enum of §6.1a. Each `BrError` value maps to a reconciliation category as follows:

| `BrError` | Reconciliation category | Routing |
|---|---|---|
| OK | n/a | normal success |
| NotFound | Cat 3 (generic) per [reconciliation/spec.md §8.4] | Beads-vs-harmonik divergence; investigator dispatch |
| Conflict | Cat 3a (torn-write) per [reconciliation/spec.md §8.4a] | concurrent-claim race; idempotency recovery per §4.10 |
| DbLocked | Cat 0 (infrastructure) per [reconciliation/spec.md §4.3 RC-012] | bounded retry; if persistent → exit code 8 |
| SchemaMismatch | Cat 0 → daemon startup failure | exit code 8 (`beads-unavailable`); operator must align harmonik release with Beads version |
| Unavailable | Cat 0 (infrastructure) | bounded retry per PL-010 cadence |
| Other | Cat 3 (generic) | divergence-detected; investigator dispatch |

The adapter is the producer of `BrError`; reconciliation detectors are the primary consumers. See [reconciliation/spec.md §4.3] for category-detector binding.

## 9. Cross-references

### 9.1 Depends on

- **[execution-model.md §4.3]** — run-vs-bead relationship and `bead_id` on the `Run` record; this spec's §4.6.BI-017 propagation builds on execution-model's field definition.
- **[execution-model.md §6.2]** — checkpoint commit trailer format including `Harmonik-Bead-ID`; this spec's §4.6.BI-018 names WHEN the trailer appears.
- **[execution-model.md §4.4]** — git checkpoint trail semantics consumed by the store-authority rule (§4.7.BI-022).
- **[event-model.md §8]** — event taxonomy; this spec's co-owned events (§6.4) have their payload schemas there.
- **[event-model.md §4.4]** — fsync durability contract; the intent log (§4.10.BI-030) inherits it.
- **[handler-contract.md §4.11]** — skill-injection mechanism; the Beads-CLI skill (§4.9) is delivered via that surface. Target spec pending bootstrap; this citation tracks forward.
- **[control-points.md §4.11]** — skill-declaration surface where the Beads-CLI skill is declared. Target spec pending bootstrap.
- **[control-points.md §6.3]** — YAML policy where per-role exclusions of the Beads-CLI skill are recorded (§4.9.BI-028). Target spec pending bootstrap.
- **[queue-model.md §6 QM-020..QM-026]** — validation rules consumed by the §4.5a submit-time validation read surface; this spec owns the read mechanics, queue-model owns the rules.
- **[queue-model.md §8 QM submit]** — queue-submit lifecycle that consumes BI-013b reads; queue-submit is the daemon's dispatch input, not `br ready`.
- **[process-lifecycle.md §4.4 PL-003a]** — first-`queue-submit` gate consumed by the BI-024a `br --version` handshake ordering requirement.

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand from the foundation corpus. Populated at finalize.

### 9.3 Co-references (read-only consumption)

- **[workspace-model.md §4.7 Session-log metadata]** — this spec declares that session logs for bead-bound runs carry `bead_id` metadata; workspace-model owns the session-log surface itself.
- **[workspace-model.md §4.2 Branching model]** — this spec's typed parent-child edges inform branching; workspace-model owns the branch lifecycle.
- **[reconciliation/spec.md §8 Category taxonomy]** — Cat 3 store-disagreement rules consume this spec's store-authority contract; reconciliation owns the classification.
- **[reconciliation/spec.md §8.12 Action-mapping layer]** — Cat 3a / 3c auto-resolvers route through this spec's §4.4 write surface; reconciliation owns the dispatch.
- **[reconciliation/spec.md §4.3 Detection rules]** — the Cat 3a detector consumes this spec's intent-log shape (§6.1 IntentLogEntry); reconciliation owns the detector code.
- **[reconciliation/spec.md §4.5 Verdict vocabulary]** — the `reopen-bead` verdict triggers a §4.4.BI-010 reopen write; reconciliation owns the verdict vocabulary.
- **[process-lifecycle.md §4.2 Startup sequence]** — the daemon's startup queries (steps 3–4) consume this spec's §4.5 read surface; process-lifecycle owns the startup sequence.
- **[operator-nfr.md §4.4 Queue-format contract]** — the operator-facing queue format is Beads schema plus a harmonik overlay; operator-nfr owns the operator surface.
- **[operator-nfr.md §4.5 Checkpoint-format stability]** — N-1 compatibility contract consumed by §6.3 schema evolution.
- **[docs/foundation/problem-space.md §Locked decisions]** — decisions #13 (Beads adopted) and #14 (skill injection) ground this spec's scope; transition-period citation permitted per bootstrap rules.

## 10. Conformance

### 10.1 Conformance profiles

**Core MVH.** An implementation conforming to Core MVH MUST pass every requirement BI-001 through BI-032 (including BI-010d, BI-013b, BI-013c) and every invariant BI-INV-001 through BI-INV-004. BI-010d (orphan-sweep reset) depends on PL-006 extension (hk-iuaed.2); implementation is post-MVH per the imrest codename label, but the spec language is normative now.

**Post-MVH extensions.** None declared at this time. Future additions (e.g., a harmonik-side Beads read-through cache with bounded staleness, a second adapter for a non-Beads ledger) are additive and would be tracked as new requirements.

### 10.2 Test-surface obligations

During bootstrap (before `testing.md` exists) test obligations are named in prose. Each requirement's test obligation:

- **BI-001 — BI-004 (selection + access model).** Build-time dependency-manifest tests verify the pinned Beads version matches the adapter's compatibility declaration; integration tests verify no code path links Beads as a library.
- **BI-005 — BI-009 (Beads-managed data).** Contract tests against a live `br` binary at the pinned version verify that `title`, `description`, `type`, edges, and status behave as declared; atomic-claim tests spawn two concurrent claim attempts and verify only one succeeds.
- **BI-010 — BI-012 (write surface).** End-to-end scenario tests dispatch a run, observe exactly one claim write, reach terminal success + merge, observe exactly one close write, and verify no intermediate `br` status-change invocations appear in the subprocess trace.
- **BI-013 — BI-016 (read surface).** Unit tests cover the adapter's translation of typed queries into `br` invocations and parsing of `br` output; reconciliation-query tests replay a crash scenario and verify read-only access. BI-013a tests verify that a `needs-attention`-labeled bead is rejected at submit time per §4.5a BI-013b (NOT filtered at `br ready` read time).
- **BI-013b — BI-013c (submit-time validation read surface).** Contract tests against a live `br` binary verify that `br show` returns the fields consumed by [queue-model.md §6 QM-020..QM-022] validation; pre-claim-guard tests inject a status flip between dispatcher selection and claim and verify `bead_claim_skipped` emission with no claim write.
- **BI-017 — BI-020 (bead-ID propagation).** Cross-spec tests inspect a bead-bound run's git checkpoint trail, event stream, and session logs to verify `bead_id` appears on every expected surface; a non-bead-bound-run test verifies the field is absent everywhere.
- **BI-021 — BI-023 (store-authority rules).** Scenario tests inject a git-vs-Beads divergence (Beads `closed`, no merge commit) and verify the divergence surfaces as a Cat 3 classification; JSONL-driven override attempts are rejected.
- **BI-024 — BI-026 (version-pin + adapter).** Release-engineering tests verify the adapter module is the sole importer of `br` subprocess helpers; a mock-Beads test simulates a breaking surface change and verifies that only the adapter module changes.
- **BI-027 — BI-028 (Beads-CLI skill).** Agent-launch integration tests verify that a launched agent's skill list contains the Beads-CLI skill by default and that the skill's documented commands succeed.
- **BI-029 — BI-032 (adapter idempotency).** Crash-injection tests kill the adapter between intent-log fsync and `br` call completion, then restart and verify idempotent completion via the audit-log check; a torn-write scenario verifies the Cat 3a detector's evidence path reads the intent log.

Migration to `[testing.md §<layer>]` cross-references occurs within one revision cycle once testing.md lands; this obligation is tracked in OQ-BI-001.

### 10.3 Excluded conformance claims

- This spec does NOT grant conformance over: Beads's own internal behavior (owned by the Beads project); the skill-injection mechanism (owned by [handler-contract.md §4.11]); reconciliation category classification or detector implementation (owned by [reconciliation/spec.md]); operator-CLI wrappers over Beads queries (owned by [operator-nfr.md §4.4]); queue data model, group lifecycle, and validation rules (owned by [queue-model.md]).
- This spec does NOT guarantee any performance or throughput bounds on `br` invocation; those are operator-observable in [operator-nfr.md §4.8] and are not requirements of this spec.

## 11. Open questions

#### OQ-BI-001 — Migrate test-obligation prose to testing.md references

Question: Section §10.2 currently names test obligations in prose. The template §10.2 expects cross-references to `[testing.md §<layer>]` once testing.md lands.
Owner: foundation-author
Blocks: none (MVH prose obligations are in place)
Default-if-unresolved: Keep prose obligations; migrate within one revision cycle after testing.md is finalized.

#### OQ-BI-002 — Beads-CLI skill package path

Question: The authoritative path of the Beads-CLI skill package is not yet bound. §4.9.BI-027 cites `docs/components/external/beads.md` plus the skill-declaration surface in [control-points.md §4.11]; the concrete skill-package file location lands at bootstrap time.
Owner: foundation-author
Blocks: BI-027, BI-028 implementation (not the spec itself)
Default-if-unresolved: Treat the skill as "the Beads-CLI skill" by name; bind the concrete path in the first release that ships a skill registry.

#### OQ-BI-003 — Intent-log filename encoding on case-sensitive filesystems

Question: §6.2 permits colons in intent-log filenames on supported filesystems and allows encoding as `_` on filesystems that forbid them. The policy is underspecified for cross-platform portability: is the encoding mechanical (always `_`) or opportunistic (colons when allowed)?
Owner: foundation-author
Blocks: none (MVH assumes POSIX filesystems that allow colons; encoding is local to the adapter)
Default-if-unresolved: Assume POSIX behavior for MVH. Revisit when a Windows-host operator scenario emerges.

#### OQ-BI-004 — Operator-cancel / `ErrCanceled` Beads transition

Question: BI-010a's status-mapping table omits a Beads write on operator cancel / `ErrCanceled` (per [handler-contract.md §4.5]); should harmonik reopen the bead, leave the bead in `in_progress`, or defer to investigator dispatch?
Owner: foundation-author
Blocks: BI-010a row coverage
Default-if-unresolved: no Beads write on cancel (cancel is a harmonik-side concept; investigator may later issue `reopen-bead`).

#### OQ-BI-005 — Cross-project bead-ID scoping

Question: BI-008a declares bead-IDs project-scoped; cross-project bead references are post-MVH. What surface (federated lookup, ID rewriting, etc.) handles cross-project work composition when the time comes?
Owner: post-MVH ingestion-spec author
Blocks: nothing at MVH (single-project assumption)
Default-if-unresolved: bead_id is project-scoped; cross-project references are out-of-contract.

#### OQ-BI-006 — Task-ingestion edge-write authority

Question: When the task-ingestion agent (post-MVH) translates external task sources into beads with typed edges, does it write edges via the §4.8 adapter, or does ingestion get a separate write surface?
Owner: ingestion-agent spec (post-MVH)
Blocks: nothing at MVH
Default-if-unresolved: ingestion writes edges via the same §4.8 adapter; edge writes are not idempotency-keyed (Beads edge-create is idempotent by construction).

#### OQ-BI-007 — Beads-CLI skill capability partition (read-only / investigator / no-write)

Question: BI-027 requires agents to invoke `br` only via the Beads-CLI skill. Should the skill enforce capability partitioning (read-only vs investigator vs no-write) mechanically (wrapper-fence) or by convention (skill documentation)?
Owner: handler-contract
Blocks: BI-027 enforcement strength
Default-if-unresolved: convention-based at MVH; the skill package documents the discipline. Promotion to mechanical wrapper-fence is post-MVH.

#### OQ-BI-008 — EV vocabulary extension for adapter-specific divergences

Question: Should EV §8 add `divergence_kind` enum values for adapter-specific divergences (e.g., `br_unrecognized_exit_code`, `beads_status_unexpected`, `beads_schema_drift`) and a `bead_terminal_transition_recovered` event for adapter-recovery observability?
Owner: event-model author (cross-spec coordination request)
Blocks: nothing at MVH
Default-if-unresolved: stay aligned with EV's existing vocabulary; emit `divergence_inconclusive` for adapter-side divergences and structured-log records for adapter-recovery observability. The cross-spec amendment to EV is post-MVH.

#### OQ-BI-009 — `br audit-log` idempotency-key query support on the pinned Beads CLI

Question: Does the pinned Beads CLI surface support an audit-log query keyed on idempotency-key? If yes, BI-031 step 3i is the canonical disambiguation; if no, BI-031 step 3ii is the only path and recovery is more aggressive about routing to Cat 3a.
Owner: foundation-author
Blocks: BI-031 step 3 disambiguation strength
Default-if-unresolved: assume not; step 3ii is the canonical path.

#### OQ-BI-010 — PL-006 orphan-sweep extension to include `br` subprocesses

Question: BI-014a requires the daemon-startup orphan sweep of PL-006 to enumerate `br` subprocesses re-parented to init. PL-006 currently enumerates tmux sessions, worktree locks, agent subprocesses, and intent files; extension to `br` subprocesses is required for SQLite WAL contention avoidance.
Owner: process-lifecycle author (cross-spec coordination request to PL R3)
Blocks: BI-014a implementation
Default-if-unresolved: BI-014a documents the requirement; PL-006 extension lands in PL R3 integration.

#### OQ-BI-011 — Post-MVH compatibility-window widening

Question: BI-024's compatibility window is exact-match at MVH. Post-MVH widening to a semver-range window (e.g., patch-level acceptance) is desirable for operator ergonomics. What rule governs the widening?
Owner: foundation-author (post-MVH)
Blocks: nothing at MVH
Default-if-unresolved: exact-match at MVH; semver-range adoption deferred to a post-MVH revision.

#### OQ-BI-012 — Multi-daemon-same-Beads-store detection

Question: Operators running multiple harmonik daemons against the SAME Beads SQLite store is unsupported per [process-lifecycle.md §4.1]. Should the adapter or daemon detect and refuse the configuration mechanically (e.g., advisory lock on the Beads file, sentinel record), or rely on operator discipline?
Owner: foundation-author
Blocks: nothing at MVH (single-daemon-per-project model)
Default-if-unresolved: operator discipline; mechanical detection is post-MVH.

#### OQ-BI-013 — ENOSPC during intent-write routing

Question: Should ENOSPC during BI-030 intent-file write route to Cat 0 with explicit `failed_prerequisite=disk_full`?
Owner: foundation-author
Blocks: nothing at MVH
Default-if-unresolved: yes; emit `infrastructure_unavailable{failed_prerequisite=disk_full}` per [event-model.md §8.7.15] and route through Cat 0 retry per PL-010.

#### OQ-BI-014 — Beads DB corruption vs schema-skew disambiguation

Question: How does the adapter distinguish "SQLite file corrupt" from "schema-skew"?
Owner: foundation-author
Blocks: nothing at MVH
Default-if-unresolved: corruption manifests as parse errors on multiple `br` commands and routes to Cat 6b (operator escalation); schema-skew is BI-024a's responsibility and routes to BrSchemaMismatch.

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-06-21 | 0.7.0 | agent (kerf work `bead-ledger-worktree-merge` / bead hk-rhtpa) | **BL-MRG merge contract + BI-010e child-bead-spawn + BI-011 permitted-write table.** Three changes applied from `05-spec-drafts/beads-integration-bl-mrg.md` plus T0 label rename (`codename:hk-<parent-id>` → `parent:hk-<parent-id>` — `codename:` is reserved for kerf work codenames; bead lineage labels use the `parent:` prefix). **(1) BI-010e (NEW):** child-bead-spawn creates — implementer agents MAY call `br create` intra-run with four constraints: `parent:hk-<parent-id>` lineage label required, idempotency check via `br list --label=parent:hk-<parent-id>` before create, terminal transitions remain daemon-only, union merge-driver (BL-MRG-002) preserves creates unconditionally. **(2) BI-011 (amended):** retitled "Permitted and prohibited intra-run writes"; added permitted-write table with three categories (`claim` existing, `child-bead-spawn` new per BI-010e, `parent-bead-label` new); added explicit failure contract for prohibited terminal writes from inside worktrees referencing BL-MRG-004. **(3) §4.8b BL-MRG (NEW section, 6 clauses):** BL-MRG-001 (`.gitattributes` + `.git/config` driver registration; daemon auto-configures at startup), BL-MRG-002 (union-by-ID algorithm with `updated_at` LWW for conflicting rows + explicit set-union for `labels`/`dependencies` arrays), BL-MRG-003 (semantic conflict logging to `.beads/merge-conflicts.log`; exit 0 always), BL-MRG-004 (`br sync --import-only` mandatory post-merge before any subsequent `br` operation; failure routes to Cat-BL2), BL-MRG-005 (`mergeRebaseAutoResolveBeadsLedger` in `workloop.go` MUST be removed — it suppresses the driver with `git checkout --theirs`), BL-MRG-006 (Phase 2 shared-DB migration path, informative). BI IDs frozen at v0.7.0 (additive: BI-010e; BL-MRG-001..006). |
| 2026-06-11 | 0.6.3 | agent (kerf work `standard-bead-dot` / epic hk-o7j) | **BI-009a workflow-mode resolution-chain tail flipped `single` → `dot` (embedded `standard-bead.dot`) with a `review-loop` review-floor, syncing to the EM-012a tier-4 default flip.** Amended **BI-009a**: the label-absence fall-through tail changed from "→ daemon-level per [process-lifecycle.md §4.1 PL-004a] → built-in fallback `single`" to "→ daemon-level → built-in fallback `dot`, resolving the embedded `standard-bead.dot` canonical exemplar per [execution-model.md §4.3 EM-012a]." Added the review-floor note (EM-012a-FLOOR): on embedded-artifact load failure the daemon MUST fall back to `review-loop`, NEVER `single`; `single` is reachable ONLY via an explicit tier-1 per-bead `workflow:single` label (audited via `review_bypassed`), and a bead resolved at the per-project / daemon-level / built-in-fallback tiers MUST NEVER dispatch under `single`. This corrects BI-009a's contradiction with the now-NORMATIVE EM-012a default flip (execution-model v0.9.0). The allowed-mode enum `{single, review-loop, dot}` and the four-tier resolution chain remain owned by [execution-model.md §4.3] and cited by reference; only the tail of the precedence narrative changed. No requirement IDs renumbered or retired; BI IDs frozen at v0.6.3 (text-only amendment of BI-009a). Refs: hk-o7j, hk-30vlb. |
| 2026-05-20 | 0.6.2 | foundation-author | **hk-uhvjo — BI-013d NEW: `--sort priority` adapter discipline for br-ready fallback.** Added BI-013d to §4.5 documenting the normative requirement that the adapter MUST pass `--sort priority` when invoking `br ready`. Rationale: the daemon's br-ready fallback path selects `readyRecords[0]` as the claim candidate and requires strict priority ordering; the default `hybrid` sort can promote an older lower-priority bead above a higher-priority bead (regression hk-rp48p). Spec-debt item: the constraint was previously carried only in the `brReadySortPriority` Go constant comment; no spec text existed. BI-013d is additive; BI IDs frozen at v0.6.2. |
| 2026-04-23 | 0.1.0 | foundation-author | Initial draft from components.md §10 + round 2 amendment §10.8a. |
| 2026-04-24 | 0.2.0 | foundation-author | Corpus-wide cleanup pass (no semantic changes). Migrated legacy architecture.md citation anchor to the §4.N map per the v0.2 NOTE: §1.5→§4.6 (×1 in §4.1.BI-003 no-MCP-server clause). No requirement IDs, invariants, or schemas were touched. |
| 2026-04-24 | 0.2.1 | foundation-author | Corpus citation-drift cleanup pass 2: migrated legacy §N.N cross-spec anchors to current template §N.N form per the central remap table; ~25 citations fixed. EV: `§3.2→§6.3`/§8 (payload registry, taxonomy per context) ×5, `§3.4→§4.4` (fsync durability) at §4.10 intent-log, §9.1 cross-refs. WM: `§5.3→§4.7` (session-log metadata), `§5.8→§4.2` (branching) at §4.3 typed-edge query, §4.6 close rule, §9.3 cross-refs. Reconciliation path fix: `[reconciliation.md §9.N]→[reconciliation/spec.md §N]` ×7 at §2.2 scope (Cat 3a detector ref), §4.2 reopen verdict, §4.5 read-surface feeds, §4.7 divergence-classification (both §8 Cat 3 and §8.12 auto-resolver refs), §4.10 Cat 3a torn-write detector, §9.3 four cross-refs. ON: `§7.4→§4.4` (queue format), `§7.5→§4.5` (N-1 compat), `§7.8→§4.8` (throughput) at §6.1 schema_version, §6.3 schema evolution, §9.3 cross-refs, §10.3 conformance exclusion. PL: `§8.1→§4.1` (daemon scope rationale), `§8.2→§4.2` (startup sequence) at §3 Beads description and §4.5.BI-014 read-surface feeds. CP: `§6.11→§4.11` (skill declaration) ×4, `§6.5→§6.3` (YAML policy) at §2.2 scope, §4.2 br access, §4.9 skill declaration + exclusion policy, §9.3 cross-refs, §11 OQ. No requirement IDs, invariants, or schemas touched. |
| 2026-04-24 | 0.3.0 | foundation-author | R1 review integration (implementer + cross-spec-architect + critic). Front-matter expansion: added `spec-category: foundation-cross-cutting`; expanded `depends-on` to the full normative-dependency set (architecture, execution-model, event-model, handler-contract, control-points, workspace-model, process-lifecycle, operator-nfr, reconciliation), preserving the EM↔BI mutual-dependency-by-direction pattern. **§4.4 closed deferred/tombstone hole:** added BI-010a status-mapping table binding harmonik run-level events (`run_started`/`run_completed`/`run_failed`/reopen-bead verdict/Cat 3c auto-resolver/operator cancel) to Beads coarse-status transitions, and BI-010b carve-out for reconciliation-driven writes (Cat 3a/3b/3c auto-resolvers route through the §4.8 adapter and §4.10 idempotency contract; their emission corresponds to `reconciliation_verdict_executed`, not `run_completed`). Updated BI-INV-001 to reference both BI-010a and BI-010b. **§4.8a `br` CLI surface contract (NEW):** BI-024a `br --version` handshake, BI-025a exit-code taxonomy (mandatory `BrError` classification + `store_divergence_detected` on unrecognized codes), BI-025b mandatory `--format json` (text-output parsing forbidden), BI-025c subprocess timeout discipline (5s read / 10s write defaults, operator-tunable), BI-025d stderr capture obligation. **§6.1a `BrError` enum + exit-code mapping table** (illustrative; pinned-version surface is authoritative). **§8 (NEW) Error and failure taxonomy** mapping each `BrError` value to a reconciliation category and routing. **BI-031 reframed:** Beads-idempotency-independent status-check-before-reissue protocol replaces the prior "Beads's own idempotency" assertion; BI-031b adds `br show` JSON-consistency dependency with `BrSchemaMismatch` divergence emission. **AR-042 sensors:** added `Sensor:` line to all four invariants; BI-INV-004 reshaped (not retired) with widened cross-subsystem span (Cat 3a detector + adapter unit tests + cross-spec out-of-band-write scenario). **BI-008a bead-ID scoping** added (project-scoped, opaque, no parsing/minting/rewriting). **§A.4 reverse-drift migration map** publishing legacy `[beads-integration.md §10.N]` → current `§4.N` anchors. **MUST/SHOULD cleanups:** BI-005 cache-as-authoritative MUST tightened; BI-021 dropped redundant "within its domain"; BI-024 replaced "typically accompanied by an adapter change" with "MUST be accompanied" for backwards-incompatible changes. **New OQs:** OQ-BI-004 (operator-cancel Beads transition), OQ-BI-005 (cross-project bead-ID scoping), OQ-BI-006 (task-ingestion edge-write authority), OQ-BI-007 (Beads-CLI skill capability partition). **Informative additions:** §4.7 Cat 3a/3c auto-resolver actions in BI terms; §4.10 intent-log directory ownership note (operator-nfr clean-install/cleanup MUST preserve `.harmonik/beads-intents/`). **Citation re-grep verified:** zero legacy-anchor matches in this spec body as of v0.3.0 (the v0.2.1 cleanup pass landed the migrations; the only grep hits are inside the v0.2.1 revision-history row's narrative description of work-already-done). Status remains `draft` pending R2 review (skeptic + crash-adversary + adapter-author). All inserts use letter-suffix IDs (BI-008a, BI-010a/b, BI-024a, BI-025a–d, BI-031b) to preserve topical grouping; numeric IDs unchanged. |
| 2026-04-24 | 0.4.0 | foundation-author | R2 review integration (skeptic + crash-adversary + adapter-author). **A1 (EV vocabulary alignment):** Replaced 3 fabricated `divergence_kind` enum values (`br_unrecognized_exit_code` in BI-025a, `beads_status_unexpected` in BI-031, `beads_schema_drift` in BI-031b) with `divergence_inconclusive` emissions per [event-model.md §8.6.10] with `reason=authority_unavailable` per EV-023a single-authority semantics. **A2 (event removal):** Replaced fabricated `bead_terminal_transition_recovered` event in BI-031 with a structured-log emission per [operator-nfr.md §4.9 ON-035] at level=info; adapter recovery is observability surface, not a state-mutation event. **B (BI-031 step 3 + step 4 expansion):** Step 3 disambiguation via `br audit-log --filter-idempotency-key` match (3i = harmonik-side authorship confirmed → no-op recovery + structured-log; 3ii = audit-log unavailable or no match → emit `divergence_inconclusive`, retain intent file for Cat 3a auto-resolver). Step 4 error-handling branch covering `BrConflict` (re-execute step 3 with new state), `BrDbLocked` (exponential backoff 100ms→1s × 3 retries), `BrUnavailable` (daemon-degraded routing per ON-037; PL-010 retry), `BrSchemaMismatch` (`divergence_inconclusive` per BI-031b), `BrOther` (Cat 6b operator-escalation per [reconciliation/spec.md §8.11]). **C (BI-030 atomicity tightened):** Temp+rename+fsync(temp)+fsync(parent_dir) on create AND fsync(parent_dir) on delete; matches WM-026 sidecar discipline. Closes APFS / ext4-data=ordered power-loss vulnerabilities. **D (BI-025c subprocess termination):** SIGTERM-then-SIGKILL discipline via HC-018 pattern; `cmd.Wait()` reap per PL-014; SIGTERM-then-SIGKILL'd subprocess classifies as `BrUnavailable` (NOT `BrOther`). **E (BI-014a NEW):** Orphan `br` subprocess sweep on daemon startup to prevent SQLite WAL lock contention; cross-spec coordination request to PL R3 to extend PL-006 orphan-sweep enumeration. **F (PL-005 step number fix):** BI-024a corrected from "PL-005 step 6" to "PL-005 step 4 Cat 0 pre-check" — version handshake belongs in step 4, not after the step 6 `br ready` query. **G (failure_class enum alignment):** BI-010a aligned to EM §8 canonical enum values `{transient, structural, deterministic, canceled, budget_exhausted, compilation_loop}`; dropped fabricated `recoverable`; mapped `deterministic`/`budget_exhausted` rows; `canceled` row added (no Beads write at MVH per OQ-BI-004). **H (IntentLogEntry schema):** §6.1 IntentLogEntry record gains `intended_post_state: CoarseStatus` field, derivable from (op, current_pre_state). **I (compatibility-window definition):** BI-024 mechanically defines the compatibility window as exact-match at MVH (`isCompatible(pinned, observed) ≡ pinned == observed`); semver-range widening tracked as OQ-BI-011. **J (BI-025d stderr robustness):** 1 MiB capture cap with explicit truncation suffix; explicit handling of warnings-on-success / Rust panic / argparse / partial-on-timeout cases. **K (BI-031 trigger consolidation):** BI-031 trigger consolidated as adapter-driven on startup (NOT reconciliation-driven via Cat 3a); the layering between adapter startup recovery and reconciliation Cat 3a is documented (Cat 3a is the post-emergence detection layer for divergences the adapter could not resolve). **L (BI-025e NEW):** Concurrent `br` invocation discipline — concurrency permitted; SQLite WAL serializes; no adapter-side mutex; OQ-BI-012 tracks multi-daemon-same-Beads detection. **M1 (adapter logging):** Informative note appended after BI-025e — adapter logs route through ON-035 with `subsystem=beads-adapter`. **M2 (unit-test seam):** BI-025 amended to expose injectable `br` binary path via constructor parameter for testability. **M3 (backpressure):** BI-031 amended — adapter MUST emit `daemon_degraded{reason=infrastructure_unavailable}` per [event-model.md §8.7.5] when intent-log directory exceeds 100 entries. **N (new OQs):** OQ-BI-008 (EV vocabulary extension for adapter-specific divergences), OQ-BI-009 (`br audit-log` idempotency-key query support), OQ-BI-010 (PL-006 orphan-sweep extension), OQ-BI-011 (compatibility-window widening post-MVH), OQ-BI-012 (multi-daemon-same-Beads detection), OQ-BI-013 (ENOSPC during intent-write routing), OQ-BI-014 (DB corruption vs schema-skew disambiguation). Cross-spec coordination requests: PL R3 should extend PL-006 orphan-sweep to enumerate `br` subprocesses (OQ-BI-010); EV may add `divergence_kind` enum values for adapter-specific divergences post-MVH (OQ-BI-008); RC R3 may align Cat 3a detection text with BI-031's status-check protocol (concurrent RC R2 integration is also addressing this). BI IDs frozen at v0.4.0. |
| 2026-04-26 | 0.4.1 | foundation-author | Reconciliation patch: BI-007's "fixed five-valued enum" claim was factually wrong about Beads's exposed surface — live `br` v0.1.45 reports `Status.enum` of 8 values: `{open, in_progress, blocked, deferred, draft, closed, tombstone, pinned}` (from `br --json schema`). Two-surface reframe per the decompose-to-tasks discipline-author's option (c): WRITE surface stays the five-value subset `{open, in_progress, closed, deferred, tombstone}` (harmonik MUST NOT transition INTO additional values via `br` writes); READ surface tolerates whatever Beads exposes (consumers pass through `blocked`, `draft`, `pinned`, and future Beads extensions; do NOT map them onto write-subset values). `draft` specifically becomes harmonik's readiness-workflow mechanism — the dispatch loop's BI-013 ready-work query natively excludes `draft` via `br ready`, so loaded-but-not-yet-dispatchable beads use this Beads-native idiom rather than a synthetic harmonik-side discriminator. §6.1 schemas split: `CoarseStatus` is now the Beads-owned extensible 8+-value read enum; `HarmonikWriteStatus` (NEW) is the 5-value write subset. `BeadRecord.status` field-comment updated to cite the read surface. §3 glossary entry updated to match. No requirement IDs renumbered or retired; BI IDs remain FROZEN. Cross-spec coordination requested: none — this is a self-contained spec correction. Status remains `reviewed`. (Surfaced by the 2026-04-25 decompose-to-tasks discipline reviewer pass; F2 finding.) |
| 2026-05-15 | 0.6.0 | foundation-author | imrest activity-marker vs truth-claim split (hk-iuaed.1). **§3 glossary:** subdivided `terminal-transition write` into two sub-categories — *activity-marker write* (auto-resettable; claim + reset) and *truth-claim write* (reconciliation-routed on disagreement; close + reopen); added standalone `activity marker` and `truth claim` definitions. **BI-010d (NEW):** normative anchor for `claim` as an activity-marker write; introduces `reset` op (`in_progress` → `open`) for startup orphan-sweep of stale claims; idempotency-key formula `<project_hash>:<bead_id>:reset:<daemon_start_ns>`; cross-references PL-006 extension (hk-iuaed.2). **BI-010a table:** added `reset` row (daemon startup orphan-sweep path); added NOTE naming `reset` as the fourth op in the op set. **BI-INV-001:** updated to enumerate four permitted write ops (claim, close, reopen, reset) with a clarifying note that `reset` is startup-only and not an intra-run write. **§6.1 TerminalOp enum:** added `reset` with inline comments distinguishing activity-marker vs truth-claim ops; `IntentLogEntry.op` comment updated. **§6.1 HarmonikWriteStatus:** no new enum value; added NOTE that `reset` reuses the existing `open` post-state. **§10.1:** added BI-010d to the Core MVH conformance profile with a post-MVH implementation note (PL-006 dependency). BI IDs frozen at v0.6.0 (additive: BI-010d). |
| 2026-05-19 | 0.6.1 | foundation-author | **hk-ekz5v — BrUnavailable retry budget widened for terminal-transition writes.** §4.10 BI-031 step (4c-transient) NEW: distinguishes transient wall-clock-timeout `BrUnavailable` (SQLite contention burst) from structural unavailability. Terminal-transition writes (CloseBead, ClaimBead, ReopenBead, ResetBead) retry up to 10 times (initial 50ms, max 2s per sleep); non-terminal reads retain DBLockedRetryMax=3. Step (4d) updated to include "or transient budget exhausted" as a trigger. Widening from 3→10 retries motivated by dogfood run hk-75rij where concurrent kerf/agent activity caused 3 retries to exhaust before SQLite contention resolved. |
| 2026-05-14 | 0.5.0 | foundation-author | extqueue amendments (kerf `extqueue`). **A (BI-013 demoted):** `br ready` is no longer a daemon dispatch input; it becomes an orchestrator-facing tool feeding the queue-planning surface per [queue-model.md §1]. The daemon's dispatch input is the submitted queue per [queue-model.md §8 QM submit]; the label-exposure clause is preserved but consumed at submit time, not at daemon dispatch. **B (BI-013a relocated):** `needs-attention` exclusion moves from adapter read-time filtering on `br ready` to submit-time validation rejection per §4.5a BI-013b mapped through [queue-model.md §6 QM-021]; `br ready` output remains a faithful ledger view. **C (BI-024a re-anchored):** `br --version` handshake now ordered against the first `queue-submit` RPC per [process-lifecycle.md §4.4 PL-003a]; the "BEFORE step 6 `br ready`" anchor is retired. **D (BI-025c re-anchored):** 5 s read-timeout default is restated as covering all adapter reads including §4.5a BI-013b submit-time `br show`; the "aligns with PL-005 step 6's `br ready` 5s constraint" anchor is retired. **E (§4.5a NEW — Submit-time validation read surface):** BI-013b (submit-time bead read uses `br show`, citing [queue-model.md §6 QM-020..QM-022]) and BI-013c (pre-claim status re-read, formerly the hk-p4xbw guard living implementation-only in the daemon workloop; now load-bearing for the queue's exactly-once dispatch guarantee per [queue-model.md §6 QM-022]). **F (BI-007 prose cleanup):** the "dispatch loop (BI-013 ready-work) MUST exclude `draft`-status beads" parenthetical is replaced with a submit-time validation duty per §4.5a BI-013b + an orchestrator-side affordance via `br ready`. **G (§4.10 informative scope note):** explicit informative note that the intent-log discipline of BI-029/BI-030 covers ONLY terminal-transition writes; queue mutations per [queue-model.md §3 QM-001..QM-003] are NOT Beads writes and are NOT constrained by BI-INV-001. The BI-INV-001 body also gains a corresponding clarifying sentence. **Co-references added:** §4.4 BI-010a `deferred`/`tombstone` bullet now points to §4.5a BI-013b rather than the retired adapter ready-work filter. §6.4 co-owned event payloads gains `bead_claim_skipped` (per §4.5a BI-013c). §9.1 depends-on gains queue-model.md and process-lifecycle.md §4.4 PL-003a. §10.1 conformance profile updated to include BI-013b/BI-013c. §10.2 test obligations gain a new bullet for BI-013b / BI-013c. §10.3 conformance exclusions add queue-model ownership. The `enqueue` operator command (legacy reference) is retired; no surviving references in this spec. BI IDs frozen at v0.5.0 (additive: BI-013b, BI-013c; no renumbering, no retirement). |

## A. Appendices

### A.3 Rationale

**Why `br` CLI only.** The CLI is the authoritative surface of Beads (30+ commands) and composes naturally with shell plus `jq` for post-processing. `br serve` (Beads's MCP server) exposes a subset and adds a long-lived process the daemon would have to manage. The CLI composition with the adapter layer (§4.8) also localizes breakage on Beads version bumps to a single module.

**Why terminal-transition writes only.** Beads's `status` enum is deliberately coarse (five values). Writing every intra-run micro-transition to Beads would thrash the `blocked_issues_cache` and flood other Beads consumers (dashboards, third-party tools, other clients of the same Beads store). Intra-run state is cheap to maintain in the git checkpoint trail and JSONL event log where it already lives; duplicating it into Beads provides no observer benefit and costs observer stability.

**Why the intent-log + audit-log pattern for idempotency.** A terminal-transition write is the one harmonik operation that mutates state in a store harmonik does NOT own authoritatively. A crash between "adapter decides to write" and "Beads confirms the write landed" is ambiguous: did the write land? The intent log, fsynced before the `br` call, captures the adapter's intent durably; Beads's own audit log captures completion. Their conjunction lets the Cat 3a detector decide whether to re-issue or declare done without an investigator. The alternative — a separate "did I write yet" flag in harmonik's own SQLite — would duplicate Beads's audit log and invite drift.

**Why git wins on completion disagreement.** Git is the only store that carries the actual work product (source changes, merge commit). Beads's `closed` status is a projection that can legitimately lag behind git (e.g., a successful merge landed but the adapter's close write was interrupted). If Beads and git disagree, git's state is the ground truth about whether the work is done; Beads is a cache that needs updating. The reverse (Beads wins) would risk treating work as incomplete when it has already been merged, producing duplicate re-runs.

### A.4 Reverse-drift migration map (informative)

Sibling specs MAY still cite this spec at legacy `[beads-integration.md §10.N]` anchors that derive from an earlier components.md layout. This table publishes the current anchor for each legacy form so downstream specs can migrate during their own integration cycles.

| Legacy anchor | Current anchor | Topic |
|---|---|---|
| `[beads-integration.md §10.1]` | `§4.1` (BI-001) | Beads selection (SQLite fork) |
| `[beads-integration.md §10.2]` | `§4.2` (BI-002–BI-004) | `br` CLI as sole access surface |
| `[beads-integration.md §10.3]` | `§4.3` (BI-005–BI-009) | Beads-managed data |
| `[beads-integration.md §10.4]` | `§4.4` (BI-010–BI-012; BI-010a status table; BI-010b reconciliation writes) | Terminal-transition writes |
| `[beads-integration.md §10.5]` | `§4.5` (BI-013–BI-016) / `§4.5a` (BI-013b, BI-013c) | Read surface / submit-time validation read surface |
| `[beads-integration.md §10.6]` | `§4.6` (BI-017–BI-020) | Bead-ID propagation |
| `[beads-integration.md §10.7]` | `§4.7` (BI-021–BI-023) | Store-authority rules |
| `[beads-integration.md §10.8]` / `[beads-integration.md §10.8a]` | `§4.8` (BI-024–BI-026) / `§4.8a` (BI-024a, BI-025a–BI-025e) | Adapter layer / `br` surface contract |
| `[beads-integration.md §10.9]` | `§4.9` (BI-027–BI-028) | Beads-CLI skill |
| `[beads-integration.md §10.10]` | `§4.10` (BI-029–BI-032; BI-031b) | Idempotency contract |

The migration is tracked corpus-wide per [workspace-model.md §A.4] precedent.
