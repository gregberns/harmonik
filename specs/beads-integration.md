# Beads Integration

```yaml
---
title: Beads Integration
spec-id: beads-integration
requirement-prefix: BI
status: draft
spec-shape: requirements-first
version: 0.1.0
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-04-23
depends-on:
  - execution-model
  - event-model
---
```

## 1. Purpose

This spec defines harmonik's integration with Beads — the external SQLite-backed task ledger that owns bead content, typed dependency edges, coarse lifecycle status, stable bead IDs, and atomic-claim semantics. It binds together the Beads references scattered across other foundation specs and names the normative surfaces that are load-bearing across the system: access model (`br` CLI only), write discipline (terminal transitions only), read queries, bead-ID propagation, store-authority rules, version-pin + adapter layer, and the adapter idempotency contract.

It exists as a standalone spec because the integration shape is cross-cutting and load-bearing (locked decision #13 per [docs/foundation/problem-space.md §Locked decisions]).

## 2. Scope

### 2.1 In scope

- Selection of the `Dicklesworthstone/beads_rust` SQLite-backed fork; rejection of the Dolt variant.
- `br` CLI as the sole access surface; rejection of `br serve` (Beads's MCP server).
- Set of Beads-managed data: bead content, typed dependency edges, coarse status, stable IDs, atomic-claim semantics.
- Harmonik write surface restricted to terminal lifecycle transitions (claim / close / reopen).
- Harmonik read surface: ready-work query, dependency graph, bead-detail, reconciliation queries.
- Bead-ID propagation into run metadata, checkpoint trailers, event payloads, and session-log metadata.
- Store-authority rules for git-vs-Beads-vs-JSONL disagreements.
- Version-pin policy and the `br`-CLI adapter layer that absorbs breakage.
- Agent access to `br` via the Beads-CLI skill, delivered through handler-contract's skill-injection mechanism.
- `br`-CLI adapter idempotency contract for terminal-transition writes (idempotency key, pre-write intent log, audit-log check).

### 2.2 Out of scope

- Skill-injection mechanism (how skills are delivered into an agent's launch context) — owned by [handler-contract.md §4.11] and [control-points.md §6.11 Skill declaration surface].
- Beads's internal schema, SQLite layout, and CLI implementation — owned by the Beads project, not harmonik; harmonik consumes the `br` surface as declared.
- Cat 3a torn-write detector logic (classification and dispatch) — owned by [reconciliation.md §9.3]; this spec owns only the adapter's idempotency contract consumed by the detector.
- Run metadata definition and run-ID allocation — owned by [execution-model.md §4.3].
- Checkpoint commit trailer format (shape of `Harmonik-Bead-ID`) — owned by [execution-model.md §6.2]; this spec owns WHEN the trailer is populated.
- Event payload shapes — owned by [event-model.md §3.2]; this spec owns WHEN bead-scoped events are emitted.

## 3. Glossary

- **bead** — an atomic queued work item in the Beads SQLite store, carrying content, typed edges, and coarse status. (see §4.3)
- **coarse status** — the five-valued Beads lifecycle enum `{open, in_progress, closed, deferred, tombstone}`. (see §4.3)
- **terminal-transition write** — a `br` status-change invocation by harmonik at a workflow boundary: claim, close, or reopen. (see §4.4)
- **`br`-CLI adapter** — the thin harmonik module that translates typed queries and writes into `br` subprocess invocations and parses `br` output. (see §4.8, §4.10)
- **idempotency key** — the deterministic string `<run_id>:<transition_id>:<op>` identifying one terminal-transition write. (see §4.10)
- **intent log** — the on-disk record of a pending terminal-transition write at `.harmonik/beads-intents/<idempotency_key>.json`, written before the `br` call and deleted after success. (see §4.10)
- **Beads-CLI skill** — the skill package declaring the `br` command surface, output formats, and harmonik-specific write discipline; delivered to agents per [handler-contract.md §4.11]. (see §4.9)

## 4. Normative requirements

### 4.1 Beads selection

#### BI-001 — Beads SQLite fork is the adopted task ledger

Harmonik MUST adopt `github.com/Dicklesworthstone/beads_rust` (the SQLite-backed fork) as its task-ledger dependency. Harmonik MUST NOT adopt the Dolt-backed Beads variant (`gastownhall/beads`) for the MVH. Harmonik MUST NOT fork or modify the Beads codebase; Beads is consumed as an external binary via the `br` CLI.

Tags: mechanism

> RATIONALE: The user has observed persistent operational issues with Dolt in practice; the SQLite fork is local-first and fits harmonik's single-machine per-project daemon model (see [process-lifecycle.md §8.1]).

### 4.2 `br` CLI access — the only access surface

#### BI-002 — All Beads interactions route through the `br` CLI

Every harmonik interaction with Beads (query or write) MUST go through the `br` CLI. Neither the daemon nor any agent MAY access Beads's SQLite file directly or link against Beads's Rust library; the CLI is the authoritative surface.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-003 — `br serve` (Beads MCP server) is not used

Harmonik MUST NOT use Beads's `br serve` MCP server. Running `br serve` adds another long-lived process the daemon would have to manage; the CLI already exposes the authoritative surface (30+ commands) and composes with shell plus `jq` for post-processing. Any future proposal to enable `br serve` requires fresh justification per the amendment protocol in [architecture.md §1.5].

Tags: mechanism

#### BI-004 — Daemon invokes `br` directly; agents invoke `br` via the Beads-CLI skill

The daemon MUST invoke `br` as a direct subprocess for its read queries (§4.5) and its terminal-transition writes (§4.4). Agents MUST invoke `br` through the Beads-CLI skill delivered via the handler-contract skill-injection mechanism per [handler-contract.md §4.11] and [control-points.md §6.11]. An agent MUST NOT bypass the skill to access `br` directly, and the handler MUST NOT provision `br` outside the skill path.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.3 Beads-managed data

#### BI-005 — Beads owns bead content

Beads MUST be the source of truth for bead content: `title`, `description`, and `type`. Harmonik MUST NOT maintain a parallel authoritative copy of this content; any harmonik-side cache is observational and reconciles to Beads on disagreement (§4.7).

Tags: mechanism

#### BI-006 — Beads owns typed dependency edges

Beads MUST be the source of truth for typed dependency edges between beads. The supported edge kinds MUST include `parent-child`, `blocks`, `conditional-blocks`, and `waits-for`. Harmonik consumes these edges read-only per §4.5.

Tags: mechanism

#### BI-007 — Coarse status is a fixed five-valued enum

Beads status MUST be exactly one of `{open, in_progress, closed, deferred, tombstone}`. Harmonik MUST NOT introduce finer-grained statuses into Beads; intra-run workflow state lives in harmonik's git checkpoint trail and JSONL event log per §4.4.

Tags: mechanism

#### BI-008 — Bead IDs are stable for the lifetime of the bead

A bead's ID MUST be stable from creation to tombstone. Harmonik relies on this stability for run-metadata bindings (§4.6), checkpoint trailers, event payloads, and session-log metadata.

Tags: mechanism

#### BI-009 — Beads provides atomic-claim semantics

Beads MUST provide atomic-claim semantics such that two agents or daemons cannot simultaneously observe the same bead as claimed-by-self. Harmonik's dispatch mechanism relies on this atomicity; a successful claim returns the bead in `in_progress` to exactly one caller.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.4 Harmonik write surface — terminal transitions only

#### BI-010 — Harmonik writes to Beads ONLY at terminal workflow transitions

Harmonik MUST write to Beads only at the following terminal transitions:

- **Claim:** `open` → `in_progress`. Emitted when the daemon dispatches a run against a ready bead.
- **Close:** `in_progress` → `closed`. Emitted when a run's workflow reaches a success terminal state AND the merge to the target branch has completed per [workspace-model.md §5.8].
- **Reopen:** `closed` → `open`. Emitted when a failure classification or an investigator `reopen-bead` verdict per [reconciliation.md §9.5] determines the work is not actually done.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-011 — No intra-run writes to Beads

Harmonik MUST NOT write per-node workflow transitions, outcome details, or fine-grained failure types to Beads. Intra-run state MUST live in the git checkpoint trail per [execution-model.md §4.4] and the JSONL event log per [event-model.md §3.2]. Writing every intra-run micro-transition to Beads is forbidden because it would thrash Beads's `blocked_issues_cache` and flood other Beads consumers.

Tags: mechanism

#### BI-012 — All terminal-transition writes route through the `br`-CLI adapter

Every terminal-transition write (§4.4.BI-010) MUST route through the `br`-CLI adapter layer (§4.8) and MUST be subject to the idempotency contract of §4.10. A direct `br` subprocess invocation that bypasses the adapter for a status-change operation is a structural violation.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.5 Harmonik read surface

#### BI-013 — Ready-work query

The daemon MUST be able to query the set of beads whose dependencies are satisfied and whose status is `open` via `br ready` (or its equivalent command). The ready-work query result is the input to the daemon's dispatch loop.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-014 — Dependency-graph query

The daemon and agents MUST be able to read the typed-edge set for a bead to determine its parents, children, and blockers. This query informs branching per [workspace-model.md §5.8] and is also consumed by reconciliation investigators.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-015 — Bead-detail query

The daemon and agents MUST be able to query a bead's title, description, status, edges, and audit trail given a stable bead ID.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-016 — Reconciliation queries

During the daemon's startup sequence per [process-lifecycle.md §8.2] steps 3–4, the daemon MUST be able to read Beads's audit log and current status for every in-flight bead. These queries are read-only and feed the reconciliation detectors of [reconciliation.md §9.3].

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

Every event emitted for a bead-bound run MUST carry the optional `bead_id` field on its payload per [event-model.md §3.2]. Events that are not scoped to a specific run (e.g., daemon lifecycle) MUST omit the field.

Tags: mechanism

#### BI-020 — Session logs for bead-bound runs carry `bead_id` metadata

Session logs produced for an agent subprocess running as part of a bead-bound run MUST carry `bead_id` metadata per [workspace-model.md §5.3]. CASS indexing uses this metadata for join-to-Beads queries.

Tags: mechanism

### 4.7 Store-authority rules

#### BI-021 — Beads is authoritative for bead content and coarse status within its domain

If the daemon's in-memory cache and Beads disagree on a bead's title, description, type, or coarse status, Beads MUST win. Harmonik MUST reconcile its cache to Beads, not the other way around.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-022 — Git is authoritative for completion

If Beads reports a bead as `closed` but no merge commit with `Harmonik-Bead-ID: <bead_id>` exists in the project's git history, the divergence MUST be classified as a reconciliation flag per [reconciliation.md §9.2] Cat 3 and MUST NOT be silently auto-reconciled into git's direction. Beads status is corrected (via the §4.4 write surface, routed through the §4.10 adapter) only after the investigator's verdict lands or after a Cat 3c auto-resolver fires per [reconciliation.md §9.2a].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### BI-023 — JSONL is observational only

The JSONL event log MUST NOT be used to override Beads or git. JSONL reads for divergence-evidence detection during reconciliation are permitted per [reconciliation.md §9.3a], but JSONL MUST NOT drive a write back to Beads except through the §4.4 write surface triggered by an investigator verdict or a Cat 3 auto-resolver.

Tags: mechanism

### 4.8 Version-pin + adapter layer

#### BI-024 — Beads version is pinned per harmonik release

A harmonik release MUST name the Beads version it tested against. Upgrading the Beads dependency MUST require a harmonik release that has verified compatibility with the new Beads version, typically accompanied by an adapter change (§4.8.BI-025). Beads is pre-1.0; silent upgrades are forbidden.

Tags: mechanism

#### BI-025 — `br`-CLI adapter is the sole translation layer

All Beads interactions from harmonik code MUST route through a single `br`-CLI adapter module. The adapter's responsibility is to translate harmonik's typed queries and writes into `br` subprocess invocations and to parse `br` output into typed results. A breaking change in Beads MUST produce exactly one adapter change; no scattered per-callsite updates are acceptable.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-026 — Harmonik absorbs breakage rather than forking Beads

On a backwards-incompatible Beads change, harmonik MUST either (a) remain pinned to the prior Beads version and delay upgrade, or (b) ship a harmonik release with an adapter change that handles the new surface. Forking Beads to patch is forbidden.

Tags: mechanism

### 4.9 Beads-CLI skill

#### BI-027 — The Beads-CLI skill is the agent-facing access path

The Beads-CLI skill MUST be the only mechanism by which an agent subprocess invokes `br`. The skill's authoritative location is declared in [control-points.md §6.11] and documented in `docs/components/external/beads.md` (cited, not defined here). The skill MUST document: `br` command surface, output formats, idiomatic `jq` pipelines, and the harmonik write discipline (agents MUST NOT issue terminal-transition `br` writes outside the harmonik workflow path; the daemon owns those writes per §4.4).

Tags: mechanism

#### BI-028 — Every agent in a harmonik run has the Beads-CLI skill by default

Per [handler-contract.md §4.11], every agent operating in a harmonik run MUST have the Beads-CLI skill available in its launch context unless a role-specific permission set explicitly excludes it (an unusual policy decision logged in the node's YAML policy per [control-points.md §6.5]).

Tags: mechanism

### 4.10 `br`-adapter idempotency — terminal-transition writes

#### BI-029 — Terminal-transition writes carry a deterministic idempotency key

The `br`-CLI adapter (§4.8) MUST derive an idempotency key for every terminal-transition write (§4.4.BI-010) using the formula `<run_id>:<transition_id>:<op>` where `op ∈ {claim, close, reopen}`. The key MUST be deterministic: identical (run_id, transition_id, op) inputs produce identical keys across invocations.

Tags: mechanism

#### BI-030 — Pre-write intent log with fsync durability

Before issuing the `br` subprocess call for a terminal-transition write, the adapter MUST persist an intent-log entry to `.harmonik/beads-intents/<idempotency_key>.json` and MUST fsync the file per the durability contract of [event-model.md §3.4]. The intent-log entry records the idempotency key, the intended operation, and a monotonic timestamp. After the `br` call returns success, the adapter MUST delete the intent file.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=recoverable-non-idempotent

#### BI-031 — Restart recovery via audit-log check

When the adapter is invoked with an idempotency key whose intent file already exists (indicating a prior crash or restart left an unresolved write), the adapter MUST first query Beads's audit log for an entry matching the idempotency key. If found, the prior write landed; the adapter MUST delete the intent file and return success WITHOUT re-issuing the `br` call. If not found, the adapter MUST re-issue the `br` call with the same idempotency key; Beads's own idempotency combined with the adapter's key ensures at-most-once effect.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### BI-032 — Intent log is the Cat 3a detector's evidence source

The intent log (§4.10.BI-030) and Beads's audit log MUST be the evidence sources consumed by the Cat 3a torn-Beads-write detector per [reconciliation.md §9.3]. This spec owns the intent-log shape and durability contract; [reconciliation.md §9.2a] owns the classification and auto-resolver.

Tags: mechanism

## 5. Invariants

#### BI-INV-001 — No intra-run writes to Beads

No harmonik code path MAY write to Beads at any run transition other than the three terminal transitions named in §4.4.BI-010 (claim, close, reopen). Intermediate node outcomes, per-transition state changes, failure classes, and hook fire events MUST never produce a `br` status write.

Tags: mechanism

#### BI-INV-002 — Bead ID is stable across harmonik's lifetime

A bead's ID is stable from creation to tombstone. Every harmonik artifact that binds to a bead (run metadata, checkpoint trailers, event payloads, session-log metadata) MUST use the same ID across the entire bead lifetime. Harmonik MUST NOT mint harmonik-local alternate identifiers for the same bead.

Tags: mechanism

#### BI-INV-003 — Git wins on completion disagreement

On any disagreement between Beads's `closed` status and the absence of a merge commit in git bearing the matching `Harmonik-Bead-ID` trailer, git's view MUST be treated as authoritative for completion. Resolution MUST route through a reconciliation workflow or Cat 3 auto-resolver per [reconciliation.md §9.2]; silent auto-reconciliation in git's direction is forbidden.

Tags: mechanism

#### BI-INV-004 — All Beads status writes are idempotency-keyed and intent-logged

Every `br` status-change invocation issued by harmonik MUST carry an idempotency key per §4.10.BI-029 AND MUST be preceded by a fsynced intent-log entry per §4.10.BI-030. A status-change invocation that skips either half is a structural violation detected by the Cat 3a detector in reconciliation.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=recoverable-non-idempotent

## 6. Schemas and data shapes

### 6.1 Record schemas

```
RECORD BeadRecord:
    bead_id          : String                 -- stable identifier for the bead's lifetime
    title            : String                 -- owned by Beads (§4.3 BI-005)
    description      : String                 -- owned by Beads
    bead_type        : String                 -- owned by Beads; harmonik treats as opaque enum
    status           : CoarseStatus           -- one of {open, in_progress, closed, deferred, tombstone}
    edges            : List<DependencyEdge>   -- typed dependency edges; see §6.1
    audit_trail_ref  : String                 -- opaque handle for `br` audit-log retrieval
```

```
ENUM CoarseStatus:
    open
    in_progress
    closed
    deferred
    tombstone
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

```
RECORD IntentLogEntry:
    idempotency_key  : String                 -- "<run_id>:<transition_id>:<op>" per §4.10 BI-029
    run_id           : UUID                   -- the harmonik run driving the write
    transition_id    : UUID                   -- the transition at which the write is emitted
    op               : TerminalOp             -- one of {claim, close, reopen}
    bead_id          : String                 -- target bead
    requested_at     : Timestamp              -- monotonic; RFC 3339 wall clock
    schema_version   : Integer                -- N-1 readable per [operator-nfr.md §7.5]
```

```
ENUM TerminalOp:
    claim
    close
    reopen
```

> INFORMATIVE: The intent log is a directory of one JSON file per pending operation, keyed by `idempotency_key`. The adapter creates the file before the `br` call and deletes it on success. After a crash, the remaining files describe exactly the set of writes whose completion is ambiguous.

### 6.2 On-disk layout for intent log

The intent log lives under `.harmonik/beads-intents/`. File naming:

```
.harmonik/beads-intents/<idempotency_key>.json
```

Where `<idempotency_key>` is the string from §4.10.BI-029, URL-safe. Colons are permitted in filenames on supported filesystems; on filesystems that forbid colons, the adapter is allowed to encode them as `_` (see OQ-BI-003).

### 6.3 Schema evolution

`IntentLogEntry` carries a `schema_version` integer. The compatibility contract is N-1 readable per [operator-nfr.md §7.5]. Additive changes (new optional field) are non-breaking; renaming or removing fields is breaking and requires a migration release.

`BeadRecord` fields are owned by Beads; harmonik's consumption is read-only, and any Beads schema change is handled by adapter update per §4.8.BI-025.

### 6.4 Co-owned event payloads

This spec's requirements drive inclusion of the `bead_id` field on the following events whose payload schemas are declared in [event-model.md §3.2]:

- `run_started` — `bead_id` present iff the run is bead-bound (§4.6.BI-019).
- `run_completed` — `bead_id` present iff the run is bead-bound.
- `run_failed` — `bead_id` present iff the run is bead-bound.
- `checkpoint_written` — `bead_id` present iff the run is bead-bound.
- `store_divergence_detected` — carries `bead_id` when divergence is scoped to a specific bead.

This spec is normative for WHEN `bead_id` appears on a payload; [event-model.md §3.2] is normative for the shape of each event.

## 9. Cross-references

### 9.1 Depends on

- **[execution-model.md §4.3]** — run-vs-bead relationship and `bead_id` on the `Run` record; this spec's §4.6.BI-017 propagation builds on execution-model's field definition.
- **[execution-model.md §6.2]** — checkpoint commit trailer format including `Harmonik-Bead-ID`; this spec's §4.6.BI-018 names WHEN the trailer appears.
- **[execution-model.md §4.4]** — git checkpoint trail semantics consumed by the store-authority rule (§4.7.BI-022).
- **[event-model.md §3.2]** — event taxonomy; this spec's co-owned events (§6.4) have their payload schemas there.
- **[event-model.md §3.4]** — fsync durability contract; the intent log (§4.10.BI-030) inherits it.
- **[handler-contract.md §4.11]** — skill-injection mechanism; the Beads-CLI skill (§4.9) is delivered via that surface. Target spec pending bootstrap; this citation tracks forward.
- **[control-points.md §6.11]** — skill-declaration surface where the Beads-CLI skill is declared. Target spec pending bootstrap.
- **[control-points.md §6.5]** — YAML policy where per-role exclusions of the Beads-CLI skill are recorded (§4.9.BI-028). Target spec pending bootstrap.

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand from the foundation corpus. Populated at finalize.

### 9.3 Co-references (read-only consumption)

- **[workspace-model.md §5.3 Session-log metadata]** — this spec declares that session logs for bead-bound runs carry `bead_id` metadata; workspace-model owns the session-log surface itself.
- **[workspace-model.md §5.8 Branching model]** — this spec's typed parent-child edges inform branching; workspace-model owns the branch lifecycle.
- **[reconciliation.md §9.2 Category taxonomy]** — Cat 3 store-disagreement rules consume this spec's store-authority contract; reconciliation owns the classification.
- **[reconciliation.md §9.2a Action-mapping layer]** — Cat 3a / 3c auto-resolvers route through this spec's §4.4 write surface; reconciliation owns the dispatch.
- **[reconciliation.md §9.3 Detection rules]** — the Cat 3a detector consumes this spec's intent-log shape (§6.1 IntentLogEntry); reconciliation owns the detector code.
- **[reconciliation.md §9.5 Verdict vocabulary]** — the `reopen-bead` verdict triggers a §4.4.BI-010 reopen write; reconciliation owns the verdict vocabulary.
- **[process-lifecycle.md §8.2 Startup sequence]** — the daemon's startup queries (steps 3–4) consume this spec's §4.5 read surface; process-lifecycle owns the startup sequence.
- **[operator-nfr.md §7.4 Queue-format contract]** — the operator-facing queue format is Beads schema plus a harmonik overlay; operator-nfr owns the operator surface.
- **[operator-nfr.md §7.5 Checkpoint-format stability]** — N-1 compatibility contract consumed by §6.3 schema evolution.
- **[docs/foundation/problem-space.md §Locked decisions]** — decisions #13 (Beads adopted) and #14 (skill injection) ground this spec's scope; transition-period citation permitted per bootstrap rules.

## 10. Conformance

### 10.1 Conformance profiles

**Core MVH.** An implementation conforming to Core MVH MUST pass every requirement BI-001 through BI-032 and every invariant BI-INV-001 through BI-INV-004. No requirement is deferred at MVH.

**Post-MVH extensions.** None declared at this time. Future additions (e.g., a harmonik-side Beads read-through cache with bounded staleness, a second adapter for a non-Beads ledger) are additive and would be tracked as new requirements.

### 10.2 Test-surface obligations

During bootstrap (before `testing.md` exists) test obligations are named in prose. Each requirement's test obligation:

- **BI-001 — BI-004 (selection + access model).** Build-time dependency-manifest tests verify the pinned Beads version matches the adapter's compatibility declaration; integration tests verify no code path links Beads as a library.
- **BI-005 — BI-009 (Beads-managed data).** Contract tests against a live `br` binary at the pinned version verify that `title`, `description`, `type`, edges, and status behave as declared; atomic-claim tests spawn two concurrent claim attempts and verify only one succeeds.
- **BI-010 — BI-012 (write surface).** End-to-end scenario tests dispatch a run, observe exactly one claim write, reach terminal success + merge, observe exactly one close write, and verify no intermediate `br` status-change invocations appear in the subprocess trace.
- **BI-013 — BI-016 (read surface).** Unit tests cover the adapter's translation of typed queries into `br` invocations and parsing of `br` output; reconciliation-query tests replay a crash scenario and verify read-only access.
- **BI-017 — BI-020 (bead-ID propagation).** Cross-spec tests inspect a bead-bound run's git checkpoint trail, event stream, and session logs to verify `bead_id` appears on every expected surface; a non-bead-bound-run test verifies the field is absent everywhere.
- **BI-021 — BI-023 (store-authority rules).** Scenario tests inject a git-vs-Beads divergence (Beads `closed`, no merge commit) and verify the divergence surfaces as a Cat 3 classification; JSONL-driven override attempts are rejected.
- **BI-024 — BI-026 (version-pin + adapter).** Release-engineering tests verify the adapter module is the sole importer of `br` subprocess helpers; a mock-Beads test simulates a breaking surface change and verifies that only the adapter module changes.
- **BI-027 — BI-028 (Beads-CLI skill).** Agent-launch integration tests verify that a launched agent's skill list contains the Beads-CLI skill by default and that the skill's documented commands succeed.
- **BI-029 — BI-032 (adapter idempotency).** Crash-injection tests kill the adapter between intent-log fsync and `br` call completion, then restart and verify idempotent completion via the audit-log check; a torn-write scenario verifies the Cat 3a detector's evidence path reads the intent log.

Migration to `[testing.md §<layer>]` cross-references occurs within one revision cycle once testing.md lands; this obligation is tracked in OQ-BI-001.

### 10.3 Excluded conformance claims

- This spec does NOT grant conformance over: Beads's own internal behavior (owned by the Beads project); the skill-injection mechanism (owned by [handler-contract.md §4.11]); reconciliation category classification or detector implementation (owned by [reconciliation.md]); operator-CLI wrappers over Beads queries (owned by [operator-nfr.md §7.4]).
- This spec does NOT guarantee any performance or throughput bounds on `br` invocation; those are operator-observable in [operator-nfr.md §7.8] and are not requirements of this spec.

## 11. Open questions

#### OQ-BI-001 — Migrate test-obligation prose to testing.md references

Question: Section §10.2 currently names test obligations in prose. The template §10.2 expects cross-references to `[testing.md §<layer>]` once testing.md lands.
Owner: foundation-author
Blocks: none (MVH prose obligations are in place)
Default-if-unresolved: Keep prose obligations; migrate within one revision cycle after testing.md is finalized.

#### OQ-BI-002 — Beads-CLI skill package path

Question: The authoritative path of the Beads-CLI skill package is not yet bound. §4.9.BI-027 cites `docs/components/external/beads.md` plus the skill-declaration surface in [control-points.md §6.11]; the concrete skill-package file location lands at bootstrap time.
Owner: foundation-author
Blocks: BI-027, BI-028 implementation (not the spec itself)
Default-if-unresolved: Treat the skill as "the Beads-CLI skill" by name; bind the concrete path in the first release that ships a skill registry.

#### OQ-BI-003 — Intent-log filename encoding on case-sensitive filesystems

Question: §6.2 permits colons in intent-log filenames on supported filesystems and allows encoding as `_` on filesystems that forbid them. The policy is underspecified for cross-platform portability: is the encoding mechanical (always `_`) or opportunistic (colons when allowed)?
Owner: foundation-author
Blocks: none (MVH assumes POSIX filesystems that allow colons; encoding is local to the adapter)
Default-if-unresolved: Assume POSIX behavior for MVH. Revisit when a Windows-host operator scenario emerges.

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-04-23 | 0.1.0 | foundation-author | Initial draft from components.md §10 + round 2 amendment §10.8a. |

## A. Appendices

### A.3 Rationale

**Why `br` CLI only.** The CLI is the authoritative surface of Beads (30+ commands) and composes naturally with shell plus `jq` for post-processing. `br serve` (Beads's MCP server) exposes a subset and adds a long-lived process the daemon would have to manage. The CLI composition with the adapter layer (§4.8) also localizes breakage on Beads version bumps to a single module.

**Why terminal-transition writes only.** Beads's `status` enum is deliberately coarse (five values). Writing every intra-run micro-transition to Beads would thrash the `blocked_issues_cache` and flood other Beads consumers (dashboards, third-party tools, other clients of the same Beads store). Intra-run state is cheap to maintain in the git checkpoint trail and JSONL event log where it already lives; duplicating it into Beads provides no observer benefit and costs observer stability.

**Why the intent-log + audit-log pattern for idempotency.** A terminal-transition write is the one harmonik operation that mutates state in a store harmonik does NOT own authoritatively. A crash between "adapter decides to write" and "Beads confirms the write landed" is ambiguous: did the write land? The intent log, fsynced before the `br` call, captures the adapter's intent durably; Beads's own audit log captures completion. Their conjunction lets the Cat 3a detector decide whether to re-issue or declare done without an investigator. The alternative — a separate "did I write yet" flag in harmonik's own SQLite — would duplicate Beads's audit log and invite drift.

**Why git wins on completion disagreement.** Git is the only store that carries the actual work product (source changes, merge commit). Beads's `closed` status is a projection that can legitimately lag behind git (e.g., a successful merge landed but the adapter's close write was interrupted). If Beads and git disagree, git's state is the ground truth about whether the work is done; Beads is a cache that needs updating. The reverse (Beads wins) would risk treating work as incomplete when it has already been merged, producing duplicate re-runs.
