# Round 1 Critic Review — beads-integration.md v0.2.0

## Verdict summary

The spec has the right load-bearing shape: `br`-CLI-only access, terminal-transitions-only write discipline, an idempotency-keyed intent-log pattern for at-least-once Beads writes, git-wins on completion disagreement, and a thin adapter that localizes Beads-version breakage. The four big calls (no MCP, no direct SQLite, no intra-run Beads writes, no forking Beads) are all correctly shaped and well-justified in §A.3. At 508 lines the spec is in comfortable size range and does not need splitting.

The draft falls short in three substantive ways and one mechanical way:

1. **The `br` surface contract is almost entirely absent.** BI-002/BI-004 name `br` as *the* authoritative surface, but the spec declares nothing about it: no command list, no exit-code taxonomy, no stdout/stderr format contract, no `br --version` handshake, no JSON-vs-text output-mode discipline. An adapter that "translates typed queries and writes into `br` subprocess invocations and parses `br` output" (BI-025) cannot be implemented, tested, or reviewed without a surface contract. §10.2 waves at "contract tests against a live `br` binary" but the contract those tests would enforce lives nowhere.
2. **Cross-reference anchors are pervasively broken.** Execution-model.md v0.3.0 already performed the `[reconciliation.md §9.x]` → `§4.x`/`§8` migration and the `[beads-integration.md §10.x]` → `§4.x` migration in its round 2 review. BI v0.2's revision history claims it performed the `§1.5→§4.6` fix on the architecture citation but did NOT perform the corresponding fix on its own outgoing citations. At least 14 citation sites point at sections that do not exist in their target spec (enumerated below).
3. **The harmonik RunState → Beads coarse-status mapping is hand-waved.** BI-010 lists three terminal transitions as English ("Claim", "Close", "Reopen") without a table binding run outcomes or failure classes to `{open, in_progress, closed, deferred, tombstone}`. `deferred` and `tombstone` are in the enum but never discussed — does harmonik ever write them? What happens to Beads when a run ends `ErrCanceled` or `ErrBudget`? The spec's central contract has a visible hole.
4. **Invariants do not name sensors per AR-042.** None of BI-INV-001..004 carries a `Sensor:` clause or a §10.2 cross-reference naming the invariant-sensor binding. Architecture's v0.3 sensor obligation was added 2026-04-24 and explicitly calls out invariant-sensor grounding as AR-042-conformant; BI's four invariants fail that mechanical check.

The verdict is **proceed with revisions**. None of the findings require architectural re-work. The adapter pattern, the store-authority rules, and the invariants are correctly chosen. But three of the four gaps are blocking: the `br` surface contract must exist before the adapter can be implemented; the citation breakage must be repaired before the spec can advance past draft (a downstream spec reviewer will trip on every other cross-ref); and the status-mapping hole must be closed before the "terminal transitions only" contract is mechanically decidable. AR-042 sensor naming is blocking for advancement to `reviewed`.

## Challenges (8 load-bearing items)

### Challenge 1 — "Terminal transitions only" is enumerated in English, not as a state-map; `deferred` and `tombstone` are unreachable from the spec

- **Challenge** — BI-010 (§4.4, lines 129–138) names three transitions: `open`→`in_progress` (claim), `in_progress`→`closed` (close), `closed`→`open` (reopen). The `CoarseStatus` enum in §6.1 (lines 337–343) has five values. The mapping from harmonik's RunState/OutcomeStatus/FailureClass to Beads status is never tabulated. Does harmonik ever write `deferred`? Does it ever write `tombstone`? If not, why are they in the enum harmonik consumes? If yes, which harmonik transition produces them?

- **What the spec says** — BI-007 (§4.3, lines 108–111): "Beads status MUST be exactly one of `{open, in_progress, closed, deferred, tombstone}`. Harmonik MUST NOT introduce finer-grained statuses into Beads." BI-010: three transitions enumerated. BI-INV-001 (§5, lines 296–300): "No harmonik code path MAY write to Beads at any run transition other than the three terminal transitions named in §4.4.BI-010 (claim, close, reopen)." The three transitions touch three of the five enum values; `deferred` and `tombstone` are never produced or consumed by any harmonik requirement in the spec.

- **Is the justification adequate?** — no. The spec under-specifies the write surface in two distinct ways:
  - (a) The harmonik → Beads **status** mapping is implicit. What happens to Beads when a run reaches `FailureClass=structural` vs `compilation_loop` vs `ErrCanceled` (per `[handler-contract.md §4.5]`) vs `ErrBudget`? Run termination is a run-level concept; closing/reopening the bound bead is a bead-level concept. Do all run-failure classes reopen the bead? Only non-structural ones? The reader has to infer from `[reconciliation.md §4.5 RC-020]` verdict vocabulary which is reconciliation-triggered, not run-terminal-triggered.
  - (b) The **inverse** mapping is even more silent: harmonik reads Beads status via BI-015 (bead-detail). A bead whose status is `deferred` is presumably not `ready`, but can a ready-query return a `deferred` bead? Can harmonik observe a `tombstone` bead? What does the daemon do with a `tombstone`? Does a `tombstone` revoke an in-flight claim? None of this is declared.

- **Stronger alternative** — add §4.4.BI-010a "Status-mapping table":

  | Harmonik event | Beads transition | Op | Trigger condition |
  |---|---|---|---|
  | `run_started` for bead-bound run | `open` → `in_progress` | `claim` | daemon dispatch per [execution-model.md §4.3 EM-013] |
  | `run_completed`, terminal success, merge landed | `in_progress` → `closed` | `close` | task branch merged per [workspace-model.md §4.5 WM-007] |
  | `run_failed` with `failure_class ∈ {structural, compilation_loop}` | (no write) | — | terminal without reopen; investigator may later emit `reopen-bead` |
  | `run_failed` with `failure_class ∈ {transient, recoverable}` AND no in-run retry available | `in_progress` → `open` | `reopen` | daemon-triggered reopen (needs new requirement) |
  | `reopen-bead` verdict per [reconciliation.md §4.5 RC-020] | `closed` → `open` | `reopen` | investigator verdict execution per RC-025 |
  | Operator cancel / ErrCanceled | (deferred TBD) | — | OQ-BI-004 |

  Then explicitly state: "`deferred` and `tombstone` are operator-facing states harmonik does NOT write at MVH. A bead in either state MUST NOT appear in BI-013 ready-work query results. Harmonik MUST treat an in-flight run whose bead's status transitions to `tombstone` as a reconciliation flag per [reconciliation.md §8.4 Cat 3]."

- **How load-bearing** — blocking. The "terminal transitions only" claim is the spine of the spec and the justification for two invariants (INV-001, INV-004). An implementer cannot decide which `br` calls to emit on any given run failure without this table. The Cat 3c detector per [reconciliation.md §8.6] detects "terminal-transition-without-Beads-write" — but which run-terminations should produce a write in the first place is precisely what is missing.

### Challenge 2 — The `br` CLI error envelope, exit codes, and output modes are not declared anywhere

- **Challenge** — BI-002/BI-004/BI-025 name `br` as the sole access surface. The adapter (§4.8) parses `br` output. But the spec declares NOTHING about what `br` returns: no exit-code taxonomy, no stdout/stderr discipline, no JSON-vs-text mode declaration, no `br --version` handshake, no declared behavior on Beads-unavailable / DB-locked / bead-not-found. The reader has to believe the adapter can be written, but the spec gives it nothing to translate *into*.

- **What the spec says** — BI-025 (§4.8, lines 239–244): "The adapter's responsibility is to translate harmonik's typed queries and writes into `br` subprocess invocations and to parse `br` output into typed results." BI-024 (§4.8, lines 233–237): "A harmonik release MUST name the Beads version it tested against." Nowhere in §4 or §6 does the spec name:
  - Exit-code values (`0` success / non-zero error — at what granularity?)
  - Stderr format (free text? structured?)
  - Stdout format per command (table? JSON when `--format json`?)
  - The `br --version` handshake (who calls it? when? what's the compatibility check?)
  - Behavior on Beads-unavailable (DB locked, missing binary, schema-migration-pending)
  - Timeout discipline

- **Is the justification adequate?** — no. §2.2 out-of-scope line 44 says "Beads's internal schema, SQLite layout, and CLI implementation — owned by the Beads project, not harmonik; harmonik consumes the `br` surface as declared." But nothing is "declared" — the spec defers to Beads's documentation which is (a) pre-1.0 and pinned per BI-024 and (b) not a harmonik artifact. The AR-014 obligation that "a subsystem MUST NOT ... export a type across an event payload while marking it internal to escape §4.1.AR-004" is relevant here: `br`'s output is the adapter's input type, and the spec has neither axes-tagged nor declared it.

- **Stronger alternative** — add §4.8a "br surface declaration":
  - **BI-024a — `br --version` handshake on adapter init.** The adapter MUST invoke `br --version` at daemon startup and MUST compare against the pinned version declared in the release manifest. A mismatch outside the compatibility window (see BI-024) MUST fail daemon startup with a typed error (per [process-lifecycle.md §4.2]).
  - **BI-025a — `br` exit-code taxonomy consumed by the adapter.** The adapter MUST map `br` exit codes into a harmonik-internal `BrError` enum: `{BrOK=0, BrNotFound, BrConflict, BrDbLocked, BrSchemaMismatch, BrUnavailable, BrOther}`. The mapping rule MUST be declared in §6.1a. A `br` exit code the adapter cannot classify MUST produce `BrOther` AND MUST emit `store_divergence_detected` per [event-model.md §8.6.8] so reconciliation can observe adapter-vs-Beads drift.
  - **BI-025b — JSON output mode is mandatory for adapter consumption.** The adapter MUST invoke every `br` command with `--format json` (or equivalent). The adapter MUST NOT parse `br` text output; a `br` command that lacks JSON output MUST be fenced off in the adapter until Beads adds JSON support.
  - **BI-025c — `br` timeout discipline.** Every adapter invocation of `br` MUST be bounded by a subprocess wall-clock timeout (default 5s for reads, 10s for writes, operator-tunable per [operator-nfr.md §4.9]). Timeout MUST map to `BrUnavailable`.

- **How load-bearing** — blocking. Without this, the spec's adapter requirements are unimplementable. A test obligation under §10.2 ("contract tests against a live `br` binary") cannot assert anything; the contract doesn't exist in the spec.

### Challenge 3 — Adapter idempotency key is declared but at-least-once semantics are not proved; "Beads's own idempotency" is assumed without citation

- **Challenge** — BI-031 (§4.10, lines 282–286) asserts: "Beads's own idempotency combined with the adapter's key ensures at-most-once effect." What is "Beads's own idempotency"? Does `br` accept the idempotency key as an argument? Is it a Beads-native concept or a convention? Nowhere in the spec is this declared as a `br`-surface feature the adapter can rely on.

- **What the spec says** — BI-029 (§4.10, lines 268–272): the adapter derives `<run_id>:<transition_id>:<op>`. BI-030: the adapter fsyncs an intent-log entry before calling `br`, deletes after success. BI-031: on crash-restart with a stale intent file, query Beads audit log; if found, delete intent; if not, re-issue with "the same idempotency key; Beads's own idempotency combined with the adapter's key ensures at-most-once effect." §A.3 rationale (lines 505–507): "The intent log, fsynced before the `br` call, captures the adapter's intent durably; Beads's own audit log captures completion. Their conjunction lets the Cat 3a detector decide whether to re-issue or declare done."

- **Is the justification adequate?** — no. Two load-bearing assumptions:
  - (a) **Beads accepts the idempotency key.** If `br status close <bead_id> --idempotency-key <key>` is the shape, the spec must declare it; if the key is a harmonik-side artifact that lives only in the intent log and Beads's audit log has a different key shape, "the same idempotency key ... ensures at-most-once" is false. The crash-restart recovery pseudocode in BI-031 hinges on the audit-log query succeeding with the idempotency key as the search predicate.
  - (b) **Beads's audit log is queryable by idempotency key.** BI-031 assumes an audit-log query like "does an entry for key K exist?" If Beads's audit log is timestamp-indexed-only (no content field matching the key), the adapter cannot deterministically answer "did my prior write land?" — it must instead check terminal state ("is the bead in_progress?") which is ambiguous if the adapter is racing with another caller.
  - Neither assumption is cited against Beads's actual surface. The third-party Beads project's contract is not "harmonik's to declare" per BI-024, but harmonik's adapter contract MUST declare what it asks for.

- **Stronger alternative** — close the loop by either:
  - (a) Declaring the shape: "Beads MUST accept an `--idempotency-key` argument on status-change commands AND MUST record it on the audit-log entry for that transition. If the pinned Beads version does not provide this, the adapter MUST fall back to the BI-031b mitigation below." Then BI-031b: "Absent Beads-native idempotency, the adapter's audit-log query uses `(bead_id, op, run_id)` as a composite key and accepts the narrowest match window (an audit entry from the same daemon-epoch). The adapter MUST NOT re-issue if any matching audit entry exists."
  - (b) Re-framing BI-031's recovery to be Beads-idempotency-independent: the adapter always queries the bead's current status before re-issuing; if the status matches the intended post-transition state, declare success; otherwise re-issue. This is stronger because it does not require Beads-native key support, but it has a time-of-check/time-of-use race if another caller flips the status concurrently — so BI-009 atomic-claim semantics have to cover re-issue.

- **How load-bearing** — important-to-blocking. This is the crux of the idempotency contract. If (a) the key is not accepted by Beads or (b) the audit log is not queryable by key, BI-031 is a recipe for double-writes at every adapter restart. The Cat 3a detector per [reconciliation.md §8.4a] consumes this contract; its guarantees are no stronger than the adapter's.

### Challenge 4 — Store-authority "git wins" is declared, but the BI-side mechanical reconciliation path is hand-waved

- **Challenge** — BI-022 (§4.7, lines 218–223) says "the divergence MUST be classified as a reconciliation flag ... and MUST NOT be silently auto-reconciled into git's direction. Beads status is corrected (via the §4.4 write surface, routed through the §4.10 adapter) only after the investigator's verdict lands or after a Cat 3c auto-resolver fires." This is correct as an escalation rule but leaves three mechanical questions unanswered: (a) who detects the divergence? (b) when does the detector fire? (c) what is the exact BI write path the Cat 3c auto-resolver uses?

- **What the spec says** — BI-022 defers detection to `[reconciliation.md §9.2]` (broken cite — should be §8.4) and auto-resolution to `[reconciliation.md §9.2a]` (broken — should be §8.4a–§8.6 or §4.2). BI-INV-003 restates the git-wins rule and asserts the sensor is "a reconciliation workflow or Cat 3 auto-resolver per [reconciliation.md §9.2]" — same broken cite. The Cat 3c auto-resolver per reconciliation.md §8.6 (lines 155–163) says "Auto-verdict `accept-close-with-note` with mechanical close-write (routed through the idempotency-keyed adapter per [beads-integration.md §10.8a])" — but BI has no §10.8a (that's legacy numbering); the BI write surface §4.4 + adapter §4.10 must be the referenced path.

- **Is the justification adequate?** — no. The Cat 3c auto-resolver per reconciliation.md fires a `close` write to reconcile Beads-toward-git. This mechanical write still passes through BI-010 (claim/close/reopen) — but Cat 3c's trigger is "merge commit exists in git AND Beads still in_progress." No `run_completed` event fired; no workflow terminal success; the daemon is "healing a gap," not executing a terminal transition. Does that count as a BI-010 close? If yes, the semantics of BI-010's "Close: Emitted when a run's workflow reaches a success terminal state AND the merge to the target branch has completed" need a carve-out for reconciliation-driven writes. If no, Cat 3c writes bypass the BI-010 contract — which violates BI-INV-001.

- **Stronger alternative** — split BI-010 into two sub-rules:
  - **BI-010a — Workflow-terminal writes.** Claim and close fire on the normal workflow-terminal path.
  - **BI-010b — Reconciliation-driven writes.** Reconciliation auto-resolvers (Cat 3a, 3b, 3c per [reconciliation.md §8.4a, §8.5, §8.6]) MAY fire close or reopen to reconcile Beads toward git. These writes MUST route through the §4.8 adapter and MUST carry the idempotency key of §4.10. A reconciliation-driven write's event emission uses `workspace_merge_status` or `reconciliation_verdict_executed` (per [event-model.md §8.5, §8.6]) rather than `run_completed`.
  - Then BI-INV-001 is rewritten: "No harmonik code path MAY write to Beads at any run transition other than (i) the three terminal transitions named in §4.4.BI-010a or (ii) the reconciliation-driven paths in §4.4.BI-010b. Intermediate node outcomes, per-transition state changes, failure classes, and hook fire events MUST never produce a `br` status write."

- **How load-bearing** — important. The current shape leaves Cat 3c implementors to decide where their BI write fits; if they invent a third path, the invariant is vacuously true but the spec's single-path promise is false.

### Challenge 5 — Bead-ID is "opaque" but the spec provides no versioning/namespacing clause for Beads-side ID-format changes

- **Challenge** — BI-008 (§4.3, lines 114–118) declares bead IDs stable for the lifetime of the bead. BI-INV-002 (§5, lines 302–306) says harmonik never mints a local alternate identifier. But what is the shape of a bead ID? Opaque `String`? A versioned prefix? Beads is pre-1.0 (BI-024). If Beads 0.7 uses `bd-<uuid>` and Beads 0.8 uses `bead/<ulid>`, what happens to harmonik-side persisted IDs — checkpoint trailers, event payloads, intent-log filenames, session-log metadata?

- **What the spec says** — §6.1 `BeadRecord.bead_id : String` — no further shape. No requirement names an ID-format version field or a Beads-side migration contract. BI-008: "stable from creation to tombstone" — implicit: stable within a single Beads version; the cross-version case is silent.

- **Is the justification adequate?** — no. Two scenarios the spec doesn't handle:
  - (a) **Beads changes ID format in an upgrade.** Pre-upgrade beads have `bd-abc123`; post-upgrade Beads reads them fine (stable), but new beads are minted as `ulid-01H...`. Harmonik's persisted `Harmonik-Bead-ID` trailers from pre-upgrade runs must still resolve. Does Beads rewrite old IDs? Does the adapter translate? Spec is silent.
  - (b) **Multi-project with different Beads versions.** Two projects on the same machine each have their own daemon and Beads pin. A kerf-generated task moves between them. Is `bead_id` globally unique or scoped to the project's Beads store? If the latter, what's the disambiguation key? Session-log metadata (BI-020) implies a single scope; `docs/components/external/beads.md` citation in BI-027 is not normative.

- **Stronger alternative** — add BI-008a "Bead-ID scoping and versioning":
  - "Bead IDs are scoped to a single Beads store. Harmonik artifacts that cite a `bead_id` MUST be co-scoped (the containing harmonik project's Beads store is the implicit namespace). Cross-project bead references are not supported at MVH and require a new spec (tracked in OQ-BI-005)."
  - "The adapter MUST treat `bead_id` as opaque: it MUST NOT parse structure, MUST NOT mint, MUST NOT rewrite. On a Beads upgrade that changes ID format, harmonik MUST run a reconciliation-pass per [reconciliation.md §4.3] that re-reads current Beads state for every in-flight run; if any `bead_id` in harmonik-persisted state fails lookup, the run enters Cat 3 per [reconciliation.md §8.4]."
  - Add OQ-BI-005 for cross-project references.

- **How load-bearing** — important, not blocking. The MVH assumes a single Beads store per project; the spec SHOULD state that explicitly. Without scoping, the ID's opacity is brittle.

### Challenge 6 — Version-pin compatibility window is declared one-directional and the forward-compat case is silent

- **Challenge** — BI-024 (§4.8, lines 233–237) pins the Beads version per release and requires an adapter change before upgrading. BI-026 (lines 247–250) handles backwards-incompatible Beads changes. But:
  - (a) Beads *adds* a field (forward-compatible change). Does the adapter need to be updated to surface it, or can harmonik run against a newer Beads that harmonik doesn't know about?
  - (b) How many Beads versions back does harmonik support? Operator drift is a real scenario: a harmonik release pins Beads 0.5.2 but an operator's box has Beads 0.4.8. Does the adapter fail fast? Does it attempt compatibility?

- **What the spec says** — BI-024: "A harmonik release MUST name the Beads version it tested against. Upgrading the Beads dependency MUST require a harmonik release that has verified compatibility with the new Beads version, typically accompanied by an adapter change." BI-026: backwards-incompatible change → adapter change or delay. No requirement names a version window or an explicit forward-compat policy.

- **Is the justification adequate?** — partly. The backward-incompat case is handled. The forward-compat case is silent: if Beads adds a new status (e.g., a sixth enum value `blocked_on_review`), the adapter's typed parsing of `CoarseStatus` (§6.1) returns... what? A parse error? Silent drop? Tolerant pass-through? BI-007's "Beads status MUST be exactly one of {...}" forbids harmonik from introducing new statuses, but says nothing about what happens when Beads does.

- **Stronger alternative** — add BI-024a "Compatibility window":
  - "A harmonik release MUST support exactly the one Beads version named in its release manifest. Running against any other Beads version (higher or lower) MUST fail the daemon's BI-024a `br --version` handshake per BI-025a and produce a typed startup error per [process-lifecycle.md §4.2]."
  - (Rationale: a single-version pin is simpler than a window for MVH; post-MVH can widen to N-1 once there's a `testing.md` layer to validate.)
  - Add BI-026a "Forward-compat unknown-enum-value policy": "The adapter MUST treat an unknown `CoarseStatus` value from `br` output as a structural error: emit `store_divergence_detected{divergence_kind=beads_surface_drift}` per [event-model.md §8.6.8] and fail the subject read as `BrSchemaMismatch`. The pinned-version guarantee of BI-024 makes this a crash-early signal, not a routine path."

- **How load-bearing** — important. The lack of a forward-compat policy is a trap for the first time a Beads minor version adds a field. The lack of a version-window declaration is operator-observable surface (§[operator-nfr.md §4.6] upgrade contract).

### Challenge 7 — Beads-CLI skill's capability surface and read-vs-write permission split are declared only in prose

- **Challenge** — BI-027/BI-028 (§4.9, lines 253–264) declare the skill exists, that agents have it by default, and that its contents are documented elsewhere. But the load-bearing question — what commands does the skill permit an agent to invoke? — is never bound. Can an agent invoke `br status close`? BI-027 says "agents MUST NOT issue terminal-transition `br` writes outside the harmonik workflow path" but the enforcement mechanism is the skill's capability declaration, which the spec defers entirely.

- **What the spec says** — BI-027 (lines 254–258): "The skill MUST document: `br` command surface, output formats, idiomatic `jq` pipelines, and the harmonik write discipline (agents MUST NOT issue terminal-transition `br` writes outside the harmonik workflow path; the daemon owns those writes per §4.4)." BI-028 (lines 260–263): all agents have it by default "unless a role-specific permission set explicitly excludes it (an unusual policy decision logged in the node's YAML policy per [control-points.md §6.5])." OQ-BI-002 (lines 476–481) defers the skill-package file location.

- **Is the justification adequate?** — no. Three gaps:
  - (a) **Read vs write capability.** The skill either permits all `br` commands (including status writes, violating BI-027's own discipline clause) or permits only a read subset. The spec does not declare the capability partition; an investigator-agent (which may legitimately need to inspect audit logs) is never cleanly separated from a reviewer-agent (which shouldn't touch Beads at all).
  - (b) **Enforcement mechanism.** The handler-contract skill-injection mechanism at [handler-contract.md §4.11] delivers skills as a package. The skill's enforcement of "no status writes" is convention-based unless the skill mechanically fences those commands (via a wrapper script, a permissions check, or a sandbox boundary). If convention-only, an agent CAN call `br status close` — and then BI-INV-001 is only as strong as the prompt in the skill.
  - (c) **Role-specific exclusions.** BI-028 permits policy-based exclusions but names no standard exclusion profile. The investigator role in [reconciliation.md §4.4 RC-016] presumably needs read access; a plain worker may not need any. The policy exemplar is missing.

- **Stronger alternative** — split BI-027 into a capability partition:
  - **BI-027a — Read-only capability set (default for worker agents).** The skill's read capability set covers: `br ready`, `br show <bead_id>`, `br list`, `br audit <bead_id>`, `br deps <bead_id>`. No status-change commands.
  - **BI-027b — Investigator read capability set (reconciliation role).** Adds audit-log deep queries (per [reconciliation.md §4.4 RC-015 snapshot token]). Still no status-change commands.
  - **BI-027c — No agent capability set includes status writes.** Status-change writes are daemon-only per BI-004 and BI-012. The skill's wrapper MUST intercept any `br status ...` invocation from an agent subprocess and fail with a typed error; enforcement is mechanical, not conventional.
  - **BI-028a — Standard policy profile.** The default policy names `beads-cli.readonly` as the skill identifier; `beads-cli.investigator` is available for investigator-tagged agents per reconciliation.md's playbook.

- **How load-bearing** — important. This is the mechanism by which BI-INV-001 becomes enforceable at the process boundary rather than as a hopeful convention.

### Challenge 8 — Dependency-edge typed-graph is read-only in BI, but harmonik's branching model (workspace-model §4.2) mentions parent-child as a naming input; the read integration point is underspecified

- **Challenge** — BI-006 (§4.3, lines 102–107) says Beads owns typed edges `{parent-child, blocks, conditional-blocks, waits-for}`. BI-014 (§4.5, lines 162–167) says the daemon and agents can read them. `[workspace-model.md §4.2]` uses parent-child edges as input to branch naming per WM-007 (three-level branching). But:
  - (a) Which harmonik consumer uses which edge kind? `blocks` presumably drives BI-013 ready-work (not ready if upstream is open); `parent-child` drives workspace-model branching; `waits-for` and `conditional-blocks` have no named harmonik consumer in the spec.
  - (b) Can harmonik write edges? BI-006 says "Harmonik consumes these edges read-only per §4.5." Read-only. But the task-ingestion agent per memory `project_harmonik_task_ingestion` loads beads from kerf; does it also load the dependency edges? If yes, that's a write. If no, who sets the edges in practice?

- **What the spec says** — BI-006: read-only, four edge kinds. BI-014: the daemon and agents read edges. BI-INV-001 prohibits writes outside BI-010's three terminal transitions — which covers status but is silent on edges.

- **Is the justification adequate?** — no. The read-only claim is too strong if the task-ingestion path needs to create edges (e.g., translate kerf's `blocks:` metadata into Beads edges on load). Either (a) ingestion is out of scope here (say so explicitly and cite the owning spec) or (b) the read-only claim softens to "harmonik's run-time consumption is read-only; ingestion paths MAY write edges through the same adapter."

- **Stronger alternative** — add BI-006a "Edge-write carve-out for ingestion":
  - "Harmonik's run-time consumption of dependency edges is read-only. Edges MAY be written only by the task-ingestion path per OQ-BI-006; such writes route through the §4.8 adapter but are NOT terminal-transition writes per §4.4 and do NOT invoke the §4.10 idempotency contract (edge writes are idempotent by construction: creating an edge that already exists is a no-op in Beads)."
  - Add BI-014a "Edge-kind consumer map":

    | Edge kind | Harmonik consumer |
    |---|---|
    | `blocks` | BI-013 ready-work query (filter: bead with open `blocks` from another non-closed bead is not ready) |
    | `conditional-blocks` | BI-013 ready-work query (filter applies only when a condition field on the bead is true; condition evaluation owned by the ingestion agent per OQ-BI-006) |
    | `parent-child` | [workspace-model.md §4.2 WM-005] branch naming; [reconciliation.md §4.4 RC-015] investigator snapshot token |
    | `waits-for` | Informational only at MVH; post-MVH may drive improvement-loop task sequencing per OQ-BI-007 |

  - Add OQ-BI-006 "Task-ingestion edge-write path" naming the ingestion agent's write authority.
  - Add OQ-BI-007 on `waits-for` consumption.

- **How load-bearing** — important. Four edge kinds with two named consumers and two unspecified is a scope leak into the "edges exist but no one uses them" zone.

## Scope leaks

Six items where the spec either over-reaches or under-reaches its declared scope.

1. **BI-020 §4.6 session-log `bead_id` metadata cites `[workspace-model.md §5.3]` — wrong section number.** `workspace-model.md §5` is Invariants; the session-log metadata sidecar lives at §4.7 Session-log directory and metadata sidecar (line 473). The scope note at BI's §6.4 / §2.2 is correct in principle ("workspace-model owns the surface"); the anchor is broken.

2. **BI-028 forward-cites `[handler-contract.md §4.11]` correctly but `[control-points.md §6.11 Skill declaration surface]` does NOT exist.** Control-points.md does not have a §6.11 today (spec in `specs/` is not split and has no §6.11 anchor per current layout). The spec's scope exclusion line 43 is "Skill-injection mechanism ... owned by handler-contract §4.11 and control-points §6.11 Skill declaration surface" — half the citation resolves, half does not. This is either a forward reference to a not-yet-drafted section or a wrong anchor; either way it fails the template's lint rule ("Each spec listed in depends-on exists under specs/" — control-points exists, but the §6.11 anchor does not).

3. **§6.4 Co-owned event payloads declares BI is "normative for when `bead_id` appears," but event-model.md §6.3 declares `bead_id?` on the run-lifecycle events' payload notes.** Both specs therefore declare the presence rule. Per template §6.5 the emitting spec owns the *when* and event-model owns the *shape* — but in practice each payload field's "present iff X" rule ends up declared twice. Verify event-model defers the presence rule to BI; BI should not re-declare. The §8.1 table in event-model uses `bead_id?` (optional) which is a shape-level declaration, and §6.3 `run_started` notes (line 655+) also reference the presence rule.

4. **BI-018 declares the trailer `Harmonik-Bead-ID` condition ("MUST be present when ... MUST be absent otherwise") — but `[execution-model.md §4.4 EM-017]` already declares the same presence rule.** Per BI §2.2 line 47, this spec "owns WHEN the trailer is populated" while execution-model owns the shape. That's a correct split, but BI-018 uses the word "MUST be present when/MUST be absent otherwise" which is shape-indistinguishable from execution-model's own declaration. Verify the double-declaration is intentional scope redundancy vs. a bug; if redundancy, collapse BI-018 to "Per [execution-model.md §4.4 EM-017], every checkpoint commit on a bead-bound run's task branch carries the trailer."

5. **BI-017 cites `[execution-model.md §6.1 Run]` — correct anchor — but the run-metadata `bead_id` field is ALSO governed by EM-014 in execution-model.** A reader wanting the authoritative rule sees BI-017 and EM-014 and cannot tell which is normative. Per §2.2 line 46 "Run metadata definition and run-ID allocation — owned by [execution-model.md §4.3]"; BI-017 should cite EM-014 (the requirement) not just the §6.1 type location.

6. **§4.10 intent-log directory `.harmonik/beads-intents/` is declared here, but `.harmonik/` is the harmonik-meta directory shared with JSONL events (`events.jsonl`), session logs (`sessions/`), transitions (`transitions/`), and event HWM (`event_id_hwm`) per event-model and execution-model.** No §4.10 requirement claims ownership of `.harmonik/beads-intents/` as a subdirectory; an operator running `rm -rf .harmonik/` per an operator-nfr §4.10 clean-install path (or post-MVH cleanup) may clobber intent log. Declare the directory's preservation semantics and cite `[operator-nfr.md §4.10]` multi-daemon or clean-install interaction.

## First-plausible-answer findings

Four places where the subagent picked the first workable answer and declared it normative without evidence of alternatives.

1. **Idempotency key formula `<run_id>:<transition_id>:<op>`** — BI-029 (§4.10, lines 268–272). This is the first formula that "works" (uniquely identifies a write) but the spec doesn't interrogate alternatives:
   - A content hash (SHA of the intent payload) would be idempotency-safe and portable; why the composite key?
   - A `transition_id` already identifies a transition globally (per execution-model EM-018a path-scoped by run_id); why suffix `op`? A given transition emits exactly one BI write, so `transition_id` alone is sufficient if the transition-to-op mapping is 1:1.
   - If `run_id:transition_id:op` is the right shape, why three components instead of a dedicated `bead_write_id: UUIDv7`? UUIDv7 is the spec's standard elsewhere (event-model EV generator, execution-model run_id).
   - The decision is defensible, but not defended. Add a §A.3 rationale subsection explaining why the composite-string key beats `transition_id`-only or UUIDv7.

2. **Intent log as a directory of JSON files, one per pending op** — §6.1 `IntentLogEntry`, §6.2 on-disk layout. Defensible, but:
   - A single append-only intent-log file (like the JSONL event log) would share fsync infrastructure and would not require directory-scan on recovery. Pros: one fsync path; cons: truncation after success is harder than unlink.
   - A SQLite intent-log would inherit Beads's durability; cons: introduces a second SQLite file harmonik owns.
   - A single file per op is the first-plausible answer; not interrogated.
   - Not blocking; a rationale paragraph suffices.

3. **Five-valued `CoarseStatus` enum consumed directly from Beads** — §6.1 `CoarseStatus`. Fine as a declaration of what Beads provides, but the spec doesn't say whether harmonik's internal representation of Beads-status matches the enum byte-for-byte or whether the adapter translates. If Beads 0.x strings are `"open"`, `"in_progress"`, `"closed"`, `"deferred"`, `"tombstone"`, harmonik's adapter can use them directly. If Beads uses `"inprogress"` (no underscore) or `"in-progress"` (hyphen), the adapter has a translation table. Spec is silent; subagent picked the wire-form-matches-enum-form answer without evidence.

4. **"30+ commands" (§A.3 rationale, line 501)** — the claim is colorful but count-bound: a claim that becomes false on the next Beads release. Drop the count; keep "the CLI is the authoritative surface that composes with shell + jq."

## Invariant audit

Per architecture AR-042 ("Every invariant MUST name its sensor") AND the template §5 selection test ("cross-subsystem; if the rule fits inside one subsystem's §4 without reference to others, it is a requirement, not an invariant").

- **BI-INV-001 — No intra-run writes to Beads** (§5 lines 296–300). **Passes the selection test** — spans BI (write discipline), execution-model (run transitions), event-model (event emission), reconciliation (detector evidence). **Fails AR-042** — no sensor named. Sensor should be: "Subprocess-trace inspection during §10.2 scenario tests; reviewer-persona corpus scan checks no `br status <state>` call appears outside `<br-CLI adapter module>`." Add inline.

- **BI-INV-002 — Bead ID is stable across harmonik's lifetime** (§5 lines 302–306). **Passes the selection test** — spans BI (ID source of truth), execution-model (trailer), event-model (payload), workspace-model (session metadata). **Fails AR-042.** Sensor: "§10.2 BI-017..BI-020 cross-spec tests (bead-ID appears byte-for-byte identical on all four surfaces for a bead-bound run); reviewer-persona scan for `mint_alternate_id`-style helpers in harmonik code."

- **BI-INV-003 — Git wins on completion disagreement** (§5 lines 308–312). **Passes** — spans BI, execution-model, reconciliation. **Fails AR-042.** Sensor: "[reconciliation.md §8.4 Cat 3] detector rule; §10.2 BI-021..BI-023 scenario test injecting a Beads-`closed`/no-merge-commit divergence."

- **BI-INV-004 — All Beads status writes are idempotency-keyed and intent-logged** (§5 lines 314–319). **Borderline on the selection test** — this is essentially BI-029 + BI-030 restated as an invariant. The cross-subsystem clause is the reconciliation Cat 3a detector (§A.3). **Save as invariant only if rewritten to span:** "Every subsystem observing a Beads status-change commit MUST verify via the idempotency-key presence in the audit log that BI-029/030 were followed; a missing intent log + missing audit entry triggers Cat 3a per [reconciliation.md §8.4a]." Current wording is just "every `br` status-change invocation MUST carry key + intent" which is §4.10 restated. **Fails AR-042.** Sensor: "Cat 3a detector per [reconciliation.md §4.3 RC-014 / §8.4a]; adapter unit tests per §10.2 BI-029..BI-032."

Summary: all four invariants need sensor clauses (one-liners added to each block). BI-INV-004 also needs reshape-to-cross-subsystem or promotion back into §4.10 as a plain requirement.

## Cross-reference audit — broken anchors

Systematic defect: BI cites legacy section numbers for reconciliation, event-model, workspace-model, process-lifecycle, and operator-nfr. Corrected mapping below (line numbers refer to BI):

| BI line | Broken citation | Correct citation |
|---|---|---|
| 45 | `[reconciliation.md §9.3]` | `[reconciliation.md §4.3]` (Detectors) or `[reconciliation.md §8.4a]` (Cat 3a) |
| 134 | `[workspace-model.md §5.8]` | `[workspace-model.md §4.5 WM-007]` (three-level branching) |
| 135 | `[reconciliation.md §9.5]` | `[reconciliation.md §4.5 RC-020]` (verdict vocabulary) |
| 178 | `[process-lifecycle.md §8.2]` | `[process-lifecycle.md §4.2]` (startup sequence) |
| 178 | `[reconciliation.md §9.3]` | `[reconciliation.md §4.3]` |
| 205 | `[workspace-model.md §5.3]` | `[workspace-model.md §4.7]` (session-log metadata sidecar) |
| 220 | `[reconciliation.md §9.2]` | `[reconciliation.md §8.4]` (Cat 3 taxonomy) |
| 220 | `[reconciliation.md §9.2a]` | `[reconciliation.md §4.2]` or `[reconciliation.md §8.4a/§8.5/§8.6]` |
| 227 | `[reconciliation.md §9.3a]` | `[reconciliation.md §4.3 RC-014]` (JSONL divergence-evidence bound) |
| 277 | `[handler-contract.md §4.11]` | OK — exists ✓ |
| 290 | `[reconciliation.md §9.3]` | `[reconciliation.md §4.3]` |
| 290 | `[reconciliation.md §9.2a]` | `[reconciliation.md §4.2]` |
| 310 | `[reconciliation.md §9.2]` | `[reconciliation.md §8.4]` |
| 398 | `[event-model.md §3.2]` | `[event-model.md §8.1]` (event taxonomy — taxonomy-first spec) |
| 406 | `[event-model.md §3.2]` | `[event-model.md §8.1]` / `[event-model.md §6.3]` (shape) |
| 415 | `[event-model.md §3.4]` (fsync durability) | `[event-model.md §4.4]` (Durability classes and fsync semantics) |
| 417 | `[event-model.md §3.2]` | `[event-model.md §8]` |
| 427 | `[workspace-model.md §5.3]` | `[workspace-model.md §4.7]` |
| 428 | `[workspace-model.md §5.8]` | `[workspace-model.md §4.5]` |
| 429 | `[reconciliation.md §9.2]` | `[reconciliation.md §8.4]` |
| 430 | `[reconciliation.md §9.2a]` | `[reconciliation.md §4.2]` / `[reconciliation.md §8.4a-§8.6]` |
| 431 | `[reconciliation.md §9.3]` | `[reconciliation.md §4.3]` |
| 432 | `[reconciliation.md §9.5]` | `[reconciliation.md §4.5]` |
| 433 | `[process-lifecycle.md §8.2]` | `[process-lifecycle.md §4.2]` |
| 434 | `[operator-nfr.md §7.4]` | `[operator-nfr.md §4.4]` (Queue-format compatibility) |
| 435 | `[operator-nfr.md §7.5]` | `[operator-nfr.md §4.5]` (Schema compatibility window) |

At least **19 broken-anchor sites** across the spec body and cross-references section. The v0.2 revision note correctly called out the pattern ("corpus-wide cleanup pass … §1.5→§4.6") but only performed it on outgoing architecture cites, not on reconciliation / event-model / workspace-model / process-lifecycle / operator-nfr cites. Execution-model.md v0.3.0 revision history (line 1065+) performed an identical cleanup — BI should mirror that.

## Structural / lint gaps

Six mechanical items that should fail a lint pre-flight.

1. **Missing `spec-category: foundation-cross-cutting` in front matter per AR-052.** AR-052 (architecture.md v0.3) mandates this field; BI v0.2 omits it. BI is `foundation-cross-cutting` by content (it imposes cross-cutting obligations on runtime subsystems; is not itself realized as a Go package). Adding this also clarifies the AR-053 envelope-declaration exemption.

2. **Missing `foundation-version:` front-matter declaration** (per AR-022 and the corpus-wide amendment tracked in architecture.md v0.3 revision row).

3. **§7 Protocols and state machines is absent** — optional per template, so not a blocker. But the adapter's idempotency protocol (intent-log-then-`br`-then-delete; restart recovery path of BI-031) is protocol-shaped and would benefit from §7.2 protocol pseudocode. Promoting BI-031's prose to pseudocode makes the recovery unambiguous.

4. **§8 Error and failure taxonomy is absent** — optional, but the adapter error model (`BrError` enum from Challenge 2) is taxonomy-shaped. If Challenge 2 is accepted, §8 holds the `BrOK/BrNotFound/BrConflict/…` taxonomy.

5. **§9.2 Reverse dependencies is present but empty (informative stub).** Per template §9.2 that is correct. Keep as-is.

6. **Appendix §A.2 Counter-examples is absent.** Optional, but for a spec with four specific "forbidden" patterns (no MCP, no library link, no intra-run write, no fork), a counter-example appendix would be a good fit. Not blocking.

## Definitional gaps

Terms used or assumed but not rigorously defined.

- **"Terminal lifecycle transition"** — BI-010 names three (claim/close/reopen) and BI-INV-001 forbids all others. But "terminal" is already loaded in execution-model's vocabulary ("terminal success / terminal failure" at [execution-model.md §7.4]). Is a BI terminal-transition write identical to a run-terminal event, or is it a narrower concept (claim happens at run-START, not run-end)? Glossary line 54 says "a `br` status-change invocation by harmonik at a workflow boundary: claim, close, or reopen" — correct, but the word "terminal" in the requirement text suggests run-terminality which is misleading.

- **"Workflow boundary"** (glossary line 54) — not defined. Is it every node-transition? Every run-level transition? The term is used once, in the glossary, to define "terminal-transition write"; then never used in §4. Either drop or define.

- **"Opaque correlation"** (§1 purpose line 22) — used for bead-ID propagation. Not defined elsewhere. Presumably means "harmonik does not parse the structure, only forwards the bytes" — but the word "correlation" doesn't mean opacity in common use (it means "links two things"). Clean up.

- **"Compatibility window"** (BI-024 and BI-026 surroundings) — never bounded. See Challenge 6.

- **"Harmonik release"** (BI-024) — as distinct from "harmonik version" — not defined. Is it a git tag? A binary artifact? An operator-nfr upgrade event per [operator-nfr.md §4.6]? Spec is silent.

- **"Audit trail ref"** (§6.1 `BeadRecord.audit_trail_ref`) — "opaque handle for `br` audit-log retrieval." But nothing in the spec says how the adapter obtains this handle or what command consumes it. Either declare the retrieval path (`br audit <bead_id>`) or drop the field from the record until it's needed.

## Affirmations

Five things the spec gets right that I would not change.

1. **Terminal-transitions-only writes (BI-010, BI-INV-001) + intra-run JSONL+git store-authority (BI-011)** — the right coarseness. The `blocked_issues_cache` thrash rationale in §A.3 is correct; the dashboard-observer-flood rationale is correct; the "duplicate state where it already lives" rationale is correct. This is the central call of the spec and it holds.

2. **`br` CLI only; no `br serve` (BI-002, BI-003)** — the right answer. `br serve` adds a long-lived process the daemon would have to manage and composes poorly with the adapter layer. The amendment-protocol escape hatch (requires fresh justification per architecture §4.6) is the correct locking strength.

3. **The adapter layer as a single module (BI-025) + harmonik-absorbs-breakage (BI-026)** — correct decision, well-defended in §A.3. Prevents scattered per-callsite updates on Beads version bumps. The "forking Beads is forbidden" rule is a non-trivial constraint and worth making explicit.

4. **Intent-log + audit-log conjunction for idempotency (BI-030, BI-031, §A.3 rationale)** — the pattern is correct (subject to Challenge 3's "does Beads support the key?" caveat). The alternative of a harmonik-side SQLite "did I write yet" flag is correctly rejected in §A.3 as audit-log duplication.

5. **Git wins on completion disagreement (BI-022, BI-INV-003)** — architecturally correct and walkthrough-aligned with execution-model's EM-INV-005. Rationale in §A.3 (lines 506–507) correctly identifies git as the only store carrying the actual work product and Beads as a projection that can legitimately lag.

## Hidden assumptions

Seven assumptions the spec makes that could turn out wrong.

1. **Beads 0.x is a moving target but the adapter can absorb all breakage in one module.** BI-025/026 claim this. Works for additive enum values (Challenge 6) but breaks for paradigm shifts — e.g., if Beads 0.8 changes its audit-log shape so the idempotency recovery (BI-031) has to change semantics, the adapter change is no longer a "one module update" but a multi-file cascade touching reconciliation's Cat 3a detector. Spec doesn't acknowledge this.

2. **Atomic claim (BI-009) is cross-process atomic.** SQLite is single-writer by default; if two harmonik daemons point at the same Beads SQLite (unsupported per memory `project_harmonik_process_lifecycle` "per-project daemon"), the atomicity is SQLite's. If Beads ever adopts WAL-mode across process boundaries, the atomicity shape may differ. Not MVH-blocking; flag as assumption.

3. **`br` subprocess fork cost is acceptable at harmonik's dispatch rate.** BI-002 forbids library linking, locking harmonik to subprocess-per-call. A daemon dispatching N beads/minute pays N fork+exec per minute for claim; at terminal close, another N. If N grows into the thousands, subprocess overhead becomes observable. Spec doesn't name an expected rate.

4. **Intent log survives crash+reboot.** BI-030 fsyncs the intent file. But fsync is to the filesystem, not to the disk if the filesystem is backed by tmpfs, or if the host is a VM with a non-durable paravirt disk. Spec assumes `.harmonik/` is on a durable local filesystem. Not named.

5. **Beads-CLI skill enforcement is convention-based by default.** See Challenge 7. The spec's write-discipline clause in BI-027 is hope, not mechanism, absent the wrapper fence proposed in the stronger alternative.

6. **`.harmonik/beads-intents/` filenames fit in kernel path-length limits.** Intent-log filename is `<run_id>:<transition_id>:<op>.json`. `run_id` is a UUIDv7 (36 chars); `transition_id` is a UUIDv7 (36 chars); `op` is up to 6 chars; plus `.json` (5). Total ~88 chars — fine on POSIX (255), fine on Windows (259) but the per-path-component limit (255) is not a concern. The directory tree is flat, so no deep-path risk. OK, but acknowledge.

7. **BI has no sub-workflow / nested-run interaction with Beads writes.** A run that spawns a sub-workflow per `[execution-model.md §4.8]` inherits the parent's `bead_id`; does the sub-workflow's terminal transition write to Beads, or does only the parent's top-level terminal? BI is silent. The answer is presumably "only the parent run's top-level terminal writes" (sub-workflow is compositional, not bead-bound), but stating it explicitly closes a reconciliation hole.

## Requirement-coverage audit

A pass through the eight enumerated scope items in §2.1 against §4 requirement coverage, to confirm nothing in scope went unsourced.

- **§2.1 "Selection of the Dicklesworthstone/beads_rust SQLite-backed fork; rejection of the Dolt variant."** → Covered by BI-001 (§4.1 lines 64–70). Adequate.
- **§2.1 "`br` CLI as the sole access surface; rejection of `br serve`."** → Covered by BI-002 (§4.2 lines 74–79) and BI-003 (§4.2 lines 81–85). Adequate, modulo Challenge 2's surface-contract gap.
- **§2.1 "Set of Beads-managed data: bead content, typed dependency edges, coarse status, stable IDs, atomic-claim semantics."** → Covered by BI-005 through BI-009 (§4.3). Adequate.
- **§2.1 "Harmonik write surface restricted to terminal lifecycle transitions."** → Covered by BI-010, BI-011, BI-012 (§4.4). Partial — Challenge 1 shows the mapping to `deferred` and `tombstone` is under-specified.
- **§2.1 "Harmonik read surface: ready-work query, dependency graph, bead-detail, reconciliation queries."** → Covered by BI-013..BI-016 (§4.5). Adequate in enumeration, under-specified in command-level detail (see Read-vs-write path audit).
- **§2.1 "Bead-ID propagation into run metadata, checkpoint trailers, event payloads, session-log metadata."** → Covered by BI-017..BI-020 (§4.6). Adequate.
- **§2.1 "Store-authority rules for git-vs-Beads-vs-JSONL disagreements."** → Covered by BI-021..BI-023 (§4.7). Partial — Challenge 4 flags the mechanical reconciliation path ambiguity.
- **§2.1 "Version-pin policy and `br`-CLI adapter layer that absorbs breakage."** → Covered by BI-024..BI-026 (§4.8). Partial — Challenge 6 flags the forward-compat gap and missing compatibility window.
- **§2.1 "Agent access to `br` via the Beads-CLI skill, delivered through handler-contract's skill-injection mechanism."** → Covered by BI-027, BI-028 (§4.9). Partial — Challenge 7 flags the capability-surface gap.
- **§2.1 "`br`-CLI adapter idempotency contract for terminal-transition writes."** → Covered by BI-029..BI-032 (§4.10). Adequate in shape, under-specified in Beads-side assumptions (Challenge 3).

Net: every in-scope item has §4 requirement coverage, but four of the ten items have gaps severe enough to be named as challenges above. The scope enumeration is accurate and matches the body; the body under-delivers on roughly 40% of what the scope promised.

## Coupling audit — what BI promises to other specs

A scan of what BI holds as a normative promise to sibling specs, matched against the body of this spec:

- **execution-model holds BI to:** the `bead_id` field on `Run` (EM-014); the `Harmonik-Bead-ID` trailer (EM-017); bead_id on the transition record schema; the three-store authority rule (EM-INV-005). BI commits to: BI-008 (stable ID), BI-017–BI-020 (propagation), BI-INV-002 (ID stability). Coupling is symmetric and honest.
- **event-model holds BI to:** co-owning `bead_id` presence on five event types (§6.4 co-owned payloads lines 398–406). BI commits to: BI-019 "bead-scoped event payloads carry `bead_id`." Coupling is honest but §6.4 redundantly re-declares the presence rule also given at BI-019 — see Scope leaks item 3.
- **reconciliation holds BI to:** the §4.10 intent-log shape and durability contract (RC-014, RC-024, RC-025, §8.4a Cat 3a); the §4.4 write-surface for verdict execution (RC-025); BI-008 bead-ID stability for snapshot tokens (RC-015). BI commits to: BI-029–BI-032 (intent log); BI-010 (write surface); BI-008 (stable ID). Coupling is load-bearing and mostly honest, but the key-shape inconsistency flagged in Challenge 3 shows the contract is not byte-exact.
- **handler-contract holds BI to:** the Beads-CLI skill being the agent-facing surface (HC-046, HC-049 skill-injection mechanism). BI commits to: BI-027 "only mechanism by which an agent invokes `br`." Coupling is honest; enforcement is convention-scale (Challenge 7).
- **control-points holds BI to:** the skill-declaration surface (CP-031, CP-052 reference the Beads-CLI skill as the motivating default). BI commits to: BI-027 (skill contents), BI-028 (default availability). Coupling is honest; policy-exemption path (BI-028's parenthetical) cites `[control-points.md §6.5]` correctly.
- **workspace-model holds BI to:** session-log `bead_id` metadata for CASS indexing (WM-session-log-sidecar). BI commits to: BI-020. Adequate. Branching model (parent-child edges) coupling flagged in Challenge 8.
- **operator-nfr holds BI to:** queue-format contract (ON-queue-format); checkpoint-format stability for §6.3 schema evolution. BI commits to: §6.3 "N-1 readable per [operator-nfr.md §7.5]" (stale anchor; correct is §4.5). Coupling is honest modulo the anchor rot.
- **process-lifecycle holds BI to:** startup sequence's read-only consumption of BI's §4.5 read surface (PL-startup-sequence); `br` subprocess as an enumerated out-of-process actor per AR-017(c). BI commits to: BI-016 (reconciliation queries); BI-004 (daemon invokes `br` directly). Coupling is adequate; the BI-004 split (daemon direct, agents via skill) matches AR-017(c).

Net: coupling to seven sibling specs is structurally consistent but anchor-decayed across the board. The actual semantic contracts are mostly correct; the citations through which those contracts are stitched together are stale.

## MUST/SHOULD discipline

Six places where keyword choice is wrong or permissive language hides a real requirement.

1. **BI-005 "any harmonik-side cache is observational and reconciles to Beads on disagreement"** (lines 96–100). "Observational" is a cognition term; the normative rule should read: "A harmonik-side cache of bead content MUST NOT be treated as authoritative; on disagreement with Beads, the cache MUST be refreshed from `br` output before any harmonik decision consumes it." The current text relies on the adjective "observational" to carry the MUST-force, which it does not. The word also drifts later in the spec — BI-023 uses "JSONL is observational only" to mean something slightly different ("MUST NOT drive a write back"). Pick one meaning or avoid the term.

2. **BI-011's rationale embedded in the requirement body** (lines 140–144). "Writing every intra-run micro-transition to Beads is forbidden because it would thrash Beads's `blocked_issues_cache` and flood other Beads consumers." The `because`-clause is rationale inside normative prose. Move the causal argument to §A.3 and leave BI-011 as a clean rule: "Harmonik MUST NOT write per-node workflow transitions, outcome details, or fine-grained failure types to Beads. Intra-run state MUST live in the git checkpoint trail per [execution-model.md §4.4] and the JSONL event log per [event-model.md §3.2]."

3. **BI-021 title qualifier "within its domain"** (line 211). Either redundant (authoritative-by-definition is within its domain) or carving out an undeclared exception. Drop the qualifier; the body already defines the domain.

4. **BI-028 parenthetical "(an unusual policy decision logged in the node's YAML policy per [control-points.md §6.5])"** (lines 260–263). The "unusual" framing is informative commentary inside a requirement body. Move to a `> INFORMATIVE:` block after the requirement.

5. **BI-031 closing claim "Beads's own idempotency combined with the adapter's key ensures at-most-once effect"** (lines 281–286). This is an assertion of an external property, not a requirement. Either upgrade to a requirement that BI can enforce ("the adapter MUST verify at-most-once by querying Beads's audit log before re-issue") or drop as unprovable from this spec's perspective. The current wording asserts a property whose enforcement lives in Beads.

6. **BI-024 "typically accompanied by an adapter change" (line 235).** "Typically" is a SHOULD-shaped hedge in a MUST-shaped rule. Either the adapter change is required when Beads has a breaking change (which it effectively is per BI-026) or it is not. Pick: "MUST be accompanied by an adapter change for backwards-incompatible Beads changes per §4.8.BI-026."

## Read-vs-write path audit

The spec declares a read surface (§4.5 BI-013..BI-016) and a write surface (§4.4 BI-010..BI-012). A clean audit against the full Beads CLI surface would show which commands harmonik uses and for what. The spec does not supply this table, and it would close several of the challenges above. A suggested shape:

| `br` command (illustrative) | Direction | Caller | Authorized callers | Notes |
|---|---|---|---|---|
| `br ready [--limit N]` | read | daemon | daemon-only (dispatch loop) | BI-013; JSON mode per Challenge 2 |
| `br list [--status X]` | read | daemon + agents | daemon + any agent with Beads-CLI skill | BI-016 reconciliation queries |
| `br show <bead_id>` | read | daemon + agents | daemon + any agent | BI-015 bead-detail |
| `br graph <bead_id>` | read | daemon + agents | daemon + any agent | BI-014 typed edges |
| `br audit <bead_id>` | read | daemon + investigator | daemon + investigator-tagged agents | BI-016 reconciliation queries; RC-015 snapshot token |
| `br claim <bead_id>` | write | daemon | daemon-only | BI-010 claim; via adapter §4.10 |
| `br close <bead_id>` | write | daemon | daemon-only | BI-010 close; via adapter §4.10 |
| `br reopen <bead_id>` | write | daemon | daemon-only | BI-010 reopen; via adapter §4.10 |
| `br defer <bead_id>` | write | (none) | operator-only (outside harmonik) | not in BI-010 enum; Challenge 1 |
| `br tombstone <bead_id>` | write | (none) | operator-only (outside harmonik) | not in BI-010 enum; Challenge 1 |
| `br create-edge …` | write | (ingestion) | ingestion agent only | Challenge 8; OQ-BI-006 |
| `br --version` | read | daemon | daemon-only | Challenge 2; BI-024a if added |

Adding this table would subsume Challenges 1, 2, 7, and 8 by giving the adapter a single authoritative surface map. The spec's current §4.5 read-surface requirements (BI-013..BI-016) describe *capabilities* but not *commands*; an implementer has to pattern-match Beads's CLI to each requirement.

## Conformance-profile adequacy

§10.1 declares a single profile (Core MVH) that requires every BI-NNN and every BI-INV-NNN. §10.2 names test obligations in prose (OQ-BI-001 tracks migration to testing.md).

Two observations:

1. **No "adapter-only" profile.** The spec's structure invites a future profile where a third-party implementer writes a different adapter (e.g., for a non-SQLite Beads successor). The §10.3 excluded claims exclude Beads's own internal behavior but say nothing about whether "the adapter conforms to BI" is a separable conformance claim from "the daemon conforms to BI." For a spec whose whole point is localizing Beads breakage to one module, separating adapter-conformance from daemon-conformance would be a natural test-shape. Not blocking for MVH, but useful to name.

2. **Scenario-test obligations are sparse for the failure modes.** §10.2 line 458 names "Crash-injection tests kill the adapter between intent-log fsync and `br` call completion, then restart and verify idempotent completion." Good. But the failure-mode list is a single scenario; the full set is:
   - (a) crash between intent-log write and fsync completion;
   - (b) crash between fsync and `br` invocation;
   - (c) crash during `br` subprocess execution (before `br` writes to Beads);
   - (d) crash after `br` writes to Beads but before returning to adapter;
   - (e) crash after `br` returns success but before intent file deletion;
   - (f) crash during intent file deletion.
   Each produces a different intent-log state on restart. Scenarios (b)–(e) are the interesting ones for idempotency recovery. Current §10.2 naming conflates them.

## Bootstrap-citation discipline

The spec cites `docs/components/external/beads.md` at BI-027 (line 256). Per template §Cross-reference convention bootstrap clause, transition-period citations to `docs/foundation/...` are permitted; citations to `docs/components/...` are NOT in the allowed bootstrap-citation set. This is either (a) a genuine citation to a stable docs reference that should stay, (b) a bootstrap citation that should be migrated to a spec reference once the Beads-CLI skill package is authored, or (c) a citation to be hoisted into `docs/foundation/` alongside the other bootstrap docs.

OQ-BI-002 (line 476) correctly flags that the Beads-CLI skill package path is not bound — so option (b) is the current state. The OQ resolution should name which of the three (a/b/c) this citation becomes.

## Template-adherence checklist cross-walk

Running the template's conformance checklist against BI v0.2:

**Lint-enforced (template §Conformance checklist)**

- ✓ Front matter is a valid YAML block with all required fields populated — BUT `spec-category` missing (AR-052 amendment, post-template-1.1; see structural gaps).
- ✓ `requirement-prefix: BI` exists in `specs/_registry.yaml` (verified against current registry).
- ✓ `spec-shape: requirements-first` — valid.
- ✓ `spec-template-version: 1.1` — valid.
- ✓ No `depended-on-by` field in front matter.
- ✓ §§1, 2, 3, 4, 5, 6, 9, 10, 11, 12 all present.
- ✓ Every requirement block has a `BI-NNN` heading and `Tags:` line.
- ✓ `Tags:` value is `mechanism` on every requirement (no cognition in BI, correct).
- ✗ Cross-reference anchors — 19+ broken anchors per §Cross-reference audit above.
- ✓ Revision history has two rows.
- ✓ Total line count 508 — under 1000, no split needed.
- ✓ No `TODO` / `TBD` / `FIXME` tokens.
- ✓ Each requirement ID appears exactly once as an anchor.
- ✓ Each spec in `depends-on` exists under `specs/` (execution-model, event-model both exist).

**Reviewer-enforced**

- ✓ Required-but-empty sections say "None." or provide content explicitly.
- ✓ Every `MUST / SHOULD / MAY` appears inside a requirement/invariant block (modulo the three MUST/SHOULD findings above).
- N/A Every cognition-tagged requirement names the delegation path — no cognition-tagged requirements in BI.
- ✓ Every requirement with external I/O carries an `Axes:` line — BI-002, BI-004, BI-009, BI-010, BI-012, BI-013..BI-016 all carry `Axes:`. BI-017..BI-020 are declaration-only (Axes exemption applies) — correct.
- ✓ Out-of-scope items each name a WHY via pointer to owning spec.
- ✗ Glossary has no redefinition issues (verified `bead`, `coarse status`, `terminal-transition write`, `br-CLI adapter`, `idempotency key`, `intent log`, `Beads-CLI skill` — all first introduced here).
- ✗ `depends-on` accurately reflects cross-references used in the body — `architecture` is NOT in `depends-on` but BI-003 cites `[architecture.md §4.6]`. Either architecture is a true dependency and belongs in `depends-on:`, or the BI-003 cite is informative and should move to §9.3 co-references.
- ✓ Every appendix is non-normative (§A.3 rationale is purely rationale).
- ✓ Open questions list three deferred decisions; more are surfaced above and would need to be added (OQ-BI-004 cancel/defer, OQ-BI-005 cross-project scoping, OQ-BI-006 ingestion edge writes, OQ-BI-007 waits-for consumption).
- ✓ Conformance profile is honest (Core MVH, nothing deferred).

Net: two mechanical lint items fail (broken anchors; `architecture` missing from `depends-on`); two template-level amendments unmet (`spec-category` field; `foundation-version` field). One reviewer-enforced item to verify (depends-on accuracy).

## Recommendation

**Proceed with revisions.** Five required changes before the spec advances to `reviewed`:

1. **R1 — Close the RunState → Beads-status mapping hole.** Add BI-010a status-mapping table (Challenge 1). Explicitly handle `deferred` and `tombstone` (harmonik does not write them at MVH; consumption rules for each).

2. **R2 — Declare the `br` surface contract.** Add §4.8a with BI-024a (--version handshake), BI-025a (exit-code taxonomy), BI-025b (JSON output mode mandatory), BI-025c (timeout discipline). Add §8 adapter-error taxonomy if accepting Challenge 2's framing.

3. **R3 — Close the idempotency-key / Beads-audit-log loop.** Either declare `br --idempotency-key` as a surface requirement BI requires from Beads (Challenge 3 option a), or re-frame BI-031 to be Beads-idempotency-independent via status-check-before-reissue (option b). Add the mitigation requirement BI-031b.

4. **R4 — Repair the 19 broken cross-reference anchors** (enumerated in the Cross-reference audit table). Mirror execution-model.md v0.3's revision-history cleanup.

5. **R5 — Add sensors to all four invariants per AR-042.** One-liner per invariant naming the sensor (reviewer persona / §10.2 scenario test / [reconciliation.md detector]).

Three additional revisions RECOMMENDED but not blocking:

6. **R6 — Split the Beads-CLI skill into read-only / investigator / no-write capability sets** (Challenge 7). Declare enforcement mechanism (mechanical wrapper fence, not convention).

7. **R7 — Declare `spec-category: foundation-cross-cutting` and `foundation-version:` in front matter.**

8. **R8 — Map edge-kind → consumer explicitly** (Challenge 8). Add BI-006a ingestion carve-out. Add OQ-BI-005/006/007 for the three new deferred decisions this surfaces.

With R1–R5 addressed, the spec is coherent and advance-ready. The central calls (terminal-only writes, adapter layer, git-wins, intent-log pattern) are sound; the gaps are in binding those calls to mechanical contracts the adapter and reconciliation detectors can implement against.

## Priority ordering for the revision cycle

For the r2 author, a suggested ordering of the work so that later edits do not invalidate earlier ones:

1. **Start with R7 (front-matter amendments).** Lint-level, mechanical, closes blockers on corpus tooling. Declaring `spec-category: foundation-cross-cutting` unblocks the AR-053 envelope-exemption check and prevents a reviewer persona flagging envelope-absence.

2. **R4 (cross-ref repair) next.** Every subsequent review pass on the revised draft has to trip on the broken anchors; fix them before re-review so the critic and architect personas are not re-flagging the same 19 sites. Mirror the execution-model v0.3 cleanup pattern exactly; the diff is mostly mechanical.

3. **R1 (status-mapping table) third.** This is the central-call gap and its resolution drives shape decisions for R2 and R6. Writing the table forces the author to answer the deferred questions about `deferred`/`tombstone`, run-failure routing, and reopen triggers — which in turn fixes what `br` commands the adapter must emit (R2) and which commands the skill must fence (R6).

4. **R2 (`br` surface contract) fourth.** With the status-mapping table pinned, the adapter's command set is determined. Add §4.8a + new BI-024a..BI-025c requirements + §8 taxonomy for BrError.

5. **R3 (idempotency-key loop) fifth.** Requires R2 decided because the `--idempotency-key` surface assumption depends on what the `br` command set looks like in practice. R3 may reshape BI-029's key formula and BI-031's recovery path; these changes cascade into §6 schemas.

6. **R5 (invariant sensors) sixth.** One-line additions per invariant. Easier after R1 because the BI-INV-001 sensor references the status-mapping table.

7. **R6, R8 as follow-ups** if the author has bandwidth.

## Risks introduced by the revision

A note on failure modes the r2 cycle should watch for.

1. **R2's JSON-mode mandate may over-constrain Beads.** Not every `br` command may have a JSON output mode today; the mandate should carry a carve-out for commands the adapter does not yet call, or a backlog item on Beads (which harmonik cannot drive directly).

2. **R1's status-mapping table may push decisions on operator-only states into harmonik.** If the author writes `deferred` handling into the table ("harmonik refuses to claim; treats as not-ready"), that's a load-bearing decision to ratify with the user — it affects the ingestion path's ability to load beads into a deferred state for later enablement.

3. **R3's key-shape change cascades into reconciliation.** If BI-029's formula changes to include `bead_id` or drop `transition_id`, reconciliation.md's Cat 3a detector code (which uses the key as the audit-log query predicate) needs a coordinated update. Do not ship R3 without coordinating with reconciliation spec's next revision.

4. **R4's anchor repair is silent — no runtime signal.** If a cite is mis-migrated (e.g., `§9.5` → wrong `§8.x` instead of right `§4.5`), the error won't surface until someone follows the link. Recommend a reviewer-persona scan that walks every cite and verifies the target anchor exists in the destination spec's current layout.

5. **Adding new BI-NNN requirements (BI-010a, BI-024a, BI-025a..c, BI-031b, BI-006a, etc.) increases the BI ID count from 32 to ~40.** Still under any meaningful cap, and IDs are mutable while status=draft (per template §Requirement-numbering convention). But if the ordering decision is to number them sequentially (BI-033, BI-034, ...) rather than with letter suffixes, the numbering will drift from the topical groupings in §4.1..§4.10. Recommend letter suffixes in this revision cycle to preserve topical groups.

Budget report: 509 lines, within range (500–900 target).
