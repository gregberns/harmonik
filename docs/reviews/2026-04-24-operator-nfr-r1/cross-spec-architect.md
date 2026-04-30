# Round 1 Cross-Spec Architect Review — operator-nfr.md v0.2.0

## Verdict summary

ON sits correctly in the graph as the operator-surface and cross-cutting-NFR
owner, and the ON-internal scope split is clean: ON owns the WHEN of operator-
control events, owns the exit-code taxonomy, owns the N-1 compat window as a
cross-artifact joint invariant, and correctly cedes event payload shape,
daemon-startup sequencing, handler secrets mechanism, budget arithmetic, and
run scheduling. Four material issues block a clean architect sign-off:

1. **Pervasive stale citations to `[event-model.md §3.N]`.** 14+ call sites in
   ON body + §9 cite event-model at `§3.N` anchors that no longer exist.
   Event-model's payload taxonomy lives at `§8.N`; envelope / fsync / schema-
   versioning / replay / structured logs live at `§4.1 / §4.4 / §4.7 / §4.5 /
   (not present)` respectively. This pattern is flagged as deferred in the
   task prompt, but ON is the back-citation hub for half the corpus — leaving
   it stale forces every downstream-spec reader to chase broken anchors.
2. **Stale citations to `§5.N` (workspace-model), `§8.N` (process-lifecycle),
   `§9.N` (reconciliation), `§10.N` (beads-integration), `§6.8 / §6.9 / §6.11`
   (control-points).** Every target spec uses `§4.N` for its normative
   requirements. The two specs that migrated already (control-points, work-
   space-model) cite ON at `§4.N`; ON itself still cites *them* at the old
   components.md-era anchors.
3. **Dependency cycle with process-lifecycle, weighted toward ON.** ON lists
   `process-lifecycle` in `depends-on` (line 20) and PL lists `operator-nfr`
   in `depends-on` (PL line 19). The template §9.3 co-ref rule plus the
   content of each spec's cross-references strongly indicates the edge should
   resolve directionally with ON depending on PL (operator control builds on
   top of the daemon's `ready` status). ON should drop PL from `depends-on`
   and keep PL cites in §9.3 — or, symmetrically to the PL-side advice, treat
   the two as co-ref pairs per template §9.3.
4. **Reverse-drift on ~71 inbound `[operator-nfr.md §7.N]` citations.** ON §7
   is now "Protocols and state machines" (legitimate for `§7.1` state-machine
   cites), but the bulk of inbound `§7.N` cites intend the components.md §7
   numbering. ON should publish a migration map in its revision history
   rather than require every downstream spec to discover the rename
   independently.

Dependency-graph structure (exit-code, RTO, upgrade co-ownership with PL,
operator-control state machine) is correct. Scope leaks are minor and fixable
in-place — ON does NOT redeclare event payload shape (§6.5 is exemplary),
does NOT redeclare daemon startup sequence (delegated to PL), and does NOT
re-invent budget arithmetic (delegated to CP §6.9). Scope discipline is
actually the strongest aspect of this draft.

## Dependency graph

### Front-matter `depends-on` audit (lines 14–23)

Declared: `architecture, event-model, execution-model, handler-contract,
control-points, process-lifecycle, reconciliation, beads-integration`.

| Target | ON cites | Verdict |
|---|---|---|
| architecture | §4.1, §4.2, §4.3, §4.8, §4.9 | ✓ Correct — ON's observability envelope and role-taxonomy obligations genuinely depend on architecture's axis/ZFC/role primitives. |
| event-model | Dozens of cites across §4.1 – §4.11 | ✓ Correct direction — ON emits events and declares the N-1 compat window, both of which need event-model's envelope + taxonomy + versioning rules. |
| execution-model | §4.3 (run), §4.4–§4.5 (checkpoint), §4.7 (reconstruction), §8 (failure class) | ✓ Correct — between-task invariant needs the checkpoint cadence, RTO needs the reconstruction path, §4.3.ON-007 remaps "task" ↔ `run`. |
| handler-contract | §4.1, §4.6, §4.7, §4.10, §4.11 | ✓ Correct — secrets injection, silent-hang obligation, skills, and handler binary launch are HC-owned. |
| control-points | §6.5 (policy), §6.8 (config), §6.9 (budget), §6.11 (skill decl) | ✓ Directionally correct BUT anchors are wrong — see "Citation correctness" below. |
| process-lifecycle | §8.1–§8.4 | ✗ Cycle risk — see below. |
| reconciliation | §9.1–§9.5 | ✓ Correct direction (ON consumes reconciliation status for pause carve-out and Cat 0 routing). |
| beads-integration | §10.1, §10.3, §10.6, §10.8 | ✓ Correct direction (queue-format compat needs the `br` adapter contract + bead-ID overlay). |

### The ON ↔ PL cycle (ON line 20, PL line 19)

This is a genuine two-spec cycle:

- **ON lists PL as depends-on (line 20).** The body cites PL at `§8.1`
  (per-project scope), `§8.2` (startup sequence), `§8.3` (command surface),
  and `§8.4` (queue-empty cadence). Of these, §8.1 and §8.2 are load-bearing
  for ON — §4.10.ON-042 (multi-tenancy deferral) rests on "per-project daemon
  isolation per PL §8.1"; §4.3.ON-010 (reconciliation carve-out) rests on
  "daemon status progression per PL §8.2"; §4.8.ON-031 RTO measurement
  endpoint rests on PL's `daemon_ready` emission timing. These are FORWARD
  dependencies (ON reads from PL).
- **PL lists ON as depends-on (PL line 19).** PL's body cites ON at §7.1
  (exit-code taxonomy consumed by §PL-008), §7.3 (operator-control state
  machine owned by ON, PL owns the prefix), §7.5 (upgrade contract), §7.7
  (graceful-shutdown cross-subsystem sequence owned by ON, PL owns daemon-
  level sequence), §7.8 (restart RTO target owned by ON, PL defines the
  measurement-endpoint event). These are also forward dependencies
  (PL consumes ON obligations).

So each spec legitimately reads normative content from the other. The
template §9.3 co-reference rule exists precisely for this pattern: resolve
directionally — one spec is depends-on, the other is §9.3 co-ref.

**Recommended resolution.** Given that the PL cross-spec-architect advised PL
to drop ON (on the grounds that operator semantics build on top of PL's
daemon shape), the symmetric correct move is: ON KEEPS PL in depends-on, and
PL drops ON to §9.3. Rationale:

- The structurally-prior concept is "daemon exists, has `ready` status" (PL).
  Without that, ON has no run-state surface to attach operator-control
  semantics to.
- ON's operator-control state machine explicitly notes (§7.1 informative,
  line 485) that "operator-control entry (`running`) occurs only at `ready`."
  That is ON building on top of PL, not the reverse.
- The RTO measurement endpoint is an ON concept (a target) that PL realizes
  (by emitting `daemon_ready` at a specific point). ON naming `daemon_ready`
  is a reverse pointer into PL's territory — PL owns the emission, ON
  consumes the timing. Consumption is the forward direction.
- The "startup failure-mode catalog" (§4.1.ON-003 and PL-008) is genuinely
  co-owned, but the ARTIFACT lives in ON's §8 taxonomy (ON line 92: "cross-
  references from other specs … MUST resolve to §8 entries"). PL consumes
  the catalog; ON produces it. Consumer-produces-artifact is forward flow
  toward the artifact producer.

Walking the ON side independently arrives at the same answer the PL reviewer
reached: **ON-on-top-of-PL**. ON keeps PL in depends-on; PL demotes ON to
§9.3.

Alternative (less preferred): keep both as co-refs. Template §9.3 permits
this for genuine peer co-dependencies, but the conceptual directionality
(daemon-exists → operator-control-semantics) argues against it.

### §9.1 / §9.2 / §9.3 consistency

**§9.1 (lines 562–596)** enumerates the depends-on targets and is roughly
consistent with the front-matter, modulo the stale anchors (covered below).

**§9.2 (line 600)** is a one-line "populated at finalize" placeholder.
Adequate for draft.

**§9.3 (lines 603–606)** lists only three external (non-spec) co-refs:
build-practices.md, problem-space.md, STATUS.md. No same-corpus spec is
demoted to co-ref here. That's defensible given the depends-on list is
broad — every corpus spec this draft touches is already in depends-on.

**Gap.** If the reviewer accepts the "PL stays in depends-on" resolution,
no §9.3 spec-corpus additions are needed. If the reviewer prefers the peer-
co-ref resolution, then `process-lifecycle` should MOVE from depends-on
(line 20) to §9.3 with a note that ON provides the state-machine vocabulary
and PL provides the daemon-status-prefix vocabulary in peer fashion.

## Citation correctness

Walked every inter-spec citation in the body + §9. Summary: the `architecture`
and `execution-model` cites are clean; every other spec has stale anchors.

### Stale `[event-model.md §3.N]` anchors

Event-model's v0.2 structure has **no §3.N subsections**; §3 is the Glossary
(single heading, no subsections). The §3.N anchors ON cites were the
components.md-era numbering. Correct targets in current event-model:

| ON citation | ON line(s) | Current event-model target | Correct anchor |
|---|---|---|---|
| `[event-model.md §3.1]` envelope | line 201, line 438, line 568 | §4.1 Envelope (lines 202–250) + §6.1 Envelope RECORD | `§4.1` or `§6.1` |
| `[event-model.md §3.2]` payload registry / taxonomy | lines 98, 112, 165, 180, 236, 248, 352, 398, 438, 451, 453–459, 568 | §8 Event taxonomy (line 70), payload fields §6.3 | `§8.N` for event listings; `§6.3` for payload schemas |
| `[event-model.md §3.4]` fsync | lines 104, 282, 418, 569 | §4.4 Durability classes and fsync semantics (line 345) | `§4.4` |
| `[event-model.md §3.5]` schema compat | line 71, 540, 569 | §4.7 Schema versioning (line 459) | `§4.7` |
| `[event-model.md §3.6]` replay split | line 282 | §4.5 Replay semantics (line 398) | `§4.5` |
| `[event-model.md §3.7]` dead-letter | line 418, 570 | §4.3 EV-011 + §6.2 (line 622) | `§4.3` or `§6.2` |
| `[event-model.md §3.8]` structured log | line 321, 571 | Not present in event-model | See below — owner ambiguous |

**`§3.8 structured log` has no replacement.** Event-model's current surface
does not normatively declare a structured-log schema. EV OQ-EV-002 (line
964) names `quality-checks.md` as the intended owner and EV §10.3 line 952
explicitly excludes structured-log format from EV conformance. ON's §4.9.
ON-035 and §5.ON-INV-003 currently depend on `[event-model.md §3.8]`. This
is not a stale-anchor issue — it is a missing-target issue. Recommend ON
either:
- (a) Soften §4.9.ON-035 to name the obligation (structured logs must
  exist) and cite `quality-checks.md` as the owner per EV OQ-EV-002; OR
- (b) Promote the structured-log obligation to an ON-owned normative
  statement (ON is the NFR spec; "every subsystem emits structured logs"
  is a textbook NFR). Under (b), the §4.9.ON-035 body would be self-
  sufficient without an inbound cite.

### Stale `[beads-integration.md §10.N]` anchors

Beads-integration's v0.2 uses `§4.N` (§4.1 Beads selection, §4.2 `br`-CLI
access, §4.3 Beads-managed data, §4.5 Harmonik read surface, §4.6 Bead-ID
propagation, §4.8 Version-pin + adapter layer, §4.9 Beads-CLI skill, §4.10
`br`-adapter idempotency). No `§10.N` sections exist.

| ON citation | ON line(s) | Current target | Correct anchor |
|---|---|---|---|
| `[beads-integration.md §10.1–§10.3]` | line 180 | §4.1 selection + §4.3 managed data | `§4.1 – §4.3` |
| `[beads-integration.md §10.3]` | line 594 | §4.3 | `§4.3` |
| `[beads-integration.md §10.6]` bead-ID propagation | line 440, 594 | §4.6 | `§4.6` |
| `[beads-integration.md §10.8]` `br` adapter | lines 193, 282, 594 | §4.2 `br`-CLI access + §4.8 Version-pin | `§4.2` (access) or `§4.8` (adapter layer) |

### Stale `[workspace-model.md §5.N]` anchors

Workspace-model v0.2 uses `§4.N` (§4.1 Worktree primitive, §4.2 Branch
naming, §4.3 Lease model, §4.4 Lifecycle states, §4.5 Merge, §4.7 Session-
log directory and metadata sidecar, §4.8 Failed-run persistence, §4.10
Interrupt-state representation). No `§5.N` sections exist (§5 is
Invariants).

| ON citation | ON line(s) | Current target | Correct anchor |
|---|---|---|---|
| `[workspace-model.md §5.1]` workspace lease | lines 242, 260, 595 | §4.1 Worktree primitive + §4.3 Lease model | `§4.3` (lease) or `§4.1` (worktree) |
| `[workspace-model.md §5.3]` session-log metadata | lines 180, 418, 596 | §4.7 Session-log directory and metadata sidecar | `§4.7` |

### Stale `[process-lifecycle.md §8.N]` anchors

Process-lifecycle v0.2 uses `§4.N` (§4.1 Per-project daemon scope, §4.2
Startup sequence, §4.3 Ready-state, §4.4 Shutdown, §4.5 Agent-subprocess,
§4.6 Daemon-vs-orchestrator, §4.8 Crash semantics, §4.9 Upgrade obligation,
§4.10 Command surface). No `§8.N` sections exist (§8 is the terse Error
and failure taxonomy placeholder).

| ON citation | ON line(s) | Current target | Correct anchor |
|---|---|---|---|
| `[process-lifecycle.md §8.1]` per-project scope | line 366, 585 | §4.1 | `§4.1` |
| `[process-lifecycle.md §8.2]` startup sequence | lines 98, 146, 288, 307, 483, 524, 541, 586 | §4.2 Startup sequence + §4.3 Ready-state | `§4.2` (startup) or `§4.3` (ready) |
| `[process-lifecycle.md §8.3]` command surface | lines 92, 327, 398, 587, 666 | §4.10 Command surface (daemon side) | `§4.10` |
| `[process-lifecycle.md §8.4]` queue-empty cadence | line 104, 588 | §4.4 Shutdown → see PL-013 which is actually in §4.4 | `§4.4` |

### Stale `[reconciliation.md §9.N]` anchors

Reconciliation spec structure uses `§4.N` for requirements and `§8.N` for
the taxonomy-first category list. No `§9.N` normative sections exist (§9 is
Cross-references).

| ON citation | ON line(s) | Current target | Correct anchor |
|---|---|---|---|
| `[reconciliation.md §9.1]` reconciliation-as-workflow | line 146, 589 | §4.1 Reconciliation-as-workflow | `§4.1` |
| `[reconciliation.md §9.2]` categories | lines 62, 140, 267, 282, 590 | §8 (Cat 0–6 taxonomy) + §4.2 Action-mapping dispatch | `§8.N` + `§4.2` |
| `[reconciliation.md §9.2a]` action-mapping | line 172, 590 | §4.2 + §8.12 Action-mapping layer | `§4.2` + `§8.12` |
| `[reconciliation.md §9.3]` Cat 0 / detectors | lines 62, 98, 104, 140, 591 | §4.3 Detectors + §8.1 Cat 0 | `§4.3` + `§8.1` |
| `[reconciliation.md §9.4, §9.4a]` investigator + budget | line 104, 301, 592 | §4.4 Investigator-agent contract | `§4.4` |
| `[reconciliation.md §9.5b]` verdict execution | line 172, 593, 614 | §4.5 Verdict vocabulary and execution + §7.2 Verdict-execution sequence | `§4.5` + `§7.2` |

Note: ON also writes `[reconciliation.md §9.2, §9.3]` at line 62 — same
pattern, same fix.

### Stale `[control-points.md §6.N]` anchors

Control-points v0.2's §6 IS "Schemas and data shapes" — so `§6.5` (Co-owned
event payloads) and `§6.3` (Policy YAML document shape) DO exist. But ON's
specific cites land on non-existent subsections:

| ON citation | ON line(s) | What ON intends | Current CP target | Correct anchor |
|---|---|---|---|---|
| `[control-points.md §6.5]` policy schema | lines 201, 248, 441, 581 | "policy schema" per ON line 201 prose | §6.5 is actually **Co-owned event payloads**; policy schema is §6.3 Policy YAML + §4.7 Policy expression grammar | `§6.3` (YAML shape) or `§4.7` (grammar) |
| `[control-points.md §6.8]` config loading | line 104, 582 | "config loading precedence" | Does not exist as §6.8; CP-037 "Config-loading precedence" lives at §4.7 | `§4.7.CP-037` |
| `[control-points.md §6.9]` budget control point | lines 104, 392, 398, 583 | "budget control point" | Does not exist as §6.9; Budget semantics lives at §4.5 (CP-027 through CP-031) | `§4.5` (Budget semantics) |
| `[control-points.md §6.11]` skill declaration | line 584 | "skill declaration" | Does not exist as §6.11; skill declaration lives at §4.11 (CP-049 through CP-052) | `§4.11` |

This class of error is the most pernicious because §6.N in CP does exist
(unlike §3.N in EV), so a reader may think the anchor resolves when it
doesn't. Fix priority: HIGH.

### Architecture and execution-model cites — clean

All `[architecture.md §4.N]` anchors resolve:
- §4.1 four-axis ✓ (line 90 of architecture)
- §4.2 ZFC ✓ (line 118)
- §4.3 verification ✓ (line 142)
- §4.6 amendment protocol ✓ (line 214) — cited at ON line 606
- §4.8 role taxonomy ✓ (line 293)
- §4.9 centralized-controller ✓ (line 325)

All `[execution-model.md §N]` anchors resolve:
- §4.3 run model ✓ (line 151 of EM) — cited at ON line 127
- §4.4 checkpoint contract ✓ (line 197) — cited at ON lines 180, 260, 340, 439, 573
- §4.5 checkpoint cadence ✓ (line 270) — cited at ON line 133, 260, 573, 691
- §4.7 state reconstruction ✓ (line 360) — cited at ON lines 221, 282, 574, 691
- §8 failure taxonomy ✓ (line 915) — cited at ON line 575
- `§8.4` canceled ✓ (line 926, row 8.4) — cited at ON line 140
- `[execution-model.md §3 run]` at ON line 67 resolves to the Glossary
  entry "run" (line 56 of EM §3) ✓

### Handler-contract cites — clean

All cites (`§4.1`, `§4.6`, `§4.7`, `§4.10`, `§4.11`) resolve cleanly to
the corresponding handler-contract §4.N requirement sections.

### `[handler-contract.md §4.6]` silent-hang event-name mismatch

ON §4.9.ON-040 (line 352) says the operator-observable consequence is "a
`handler_silent_hang` event or equivalent per [event-model.md §3.2]." The
actual event in event-model §8.3.10 and handler-contract §7.1 is
`agent_warning_silent_hang` (paired with `agent_resumed_after_warning`,
`agent_soft_terminating`, `agent_hard_terminating`). `handler_silent_hang`
does not exist in the corpus. Either:
- (a) ON is authoritative and HC/EV should rename; unlikely, given HC's
  §7.1 state machine is the detailed spec, OR
- (b) ON should rewrite ON-040's "handler_silent_hang" to
  "agent_warning_silent_hang" (and drop "or equivalent").

Recommend (b). ON is not the authoring spec for the event name.

## Scope leaks

ON's scope discipline is genuinely strong. Three soft leaks found.

### §6.5 co-owned event payloads (lines 449–461)

Exemplary. Each of the seven operator-* events is listed with `emitted on
<transition>` (the WHEN, owned here) and `payload schema in [event-model.md
§3.2]` (the SHAPE, owned there). Last line explicitly states "This spec is
normative for the *when*; event-model is normative for the *shape*." This
is the template §6.5 co-ownership rule applied correctly.

One nitpick: the payload-carried field names (`pause_reason`, `stop_mode`,
`expected_commit_hash`) are quoted in ON's §6.5 bullets. This straddles the
line — declaring that a payload "carries" a named field starts to shade
into payload-shape territory. The EV §8.7 table shows `operator_pause_status`
with `status (pausing | paused)`, `changed_at`, `operator_id?` — which is
different from what ON §6.5 implies (two separate events `operator_pausing`
and `operator_paused`). See divergence below.

### ON's operator-control events vs EV §8.7 taxonomy — genuine divergence

ON §4.3.ON-013 (line 165) declares six operator-control events:
`operator_pausing`, `operator_paused`, `operator_resuming`, `operator_
stopped`, `operator_upgrading`, `operator_upgrade_completed`. ON §6.5 adds
`operator_upgrade_rejected` for a total of seven.

EV §8.7 declares: `daemon_started`, `daemon_ready`, `daemon_shutdown`,
`daemon_startup_failed`, `daemon_degraded`, `operator_pause_status`
(paired-phase merge per EV §8.9(h)), `operator_resuming`, `operator_stopped`,
`operator_upgrading`, `operator_upgrade_completed`, `operator_upgrade_
rejected`, `operator_command_rejected`, `dispatch_deferred`,
`daemon_orphan_sweep_completed`, `infrastructure_unavailable`.

Divergences:
1. ON has `operator_pausing` + `operator_paused` (two events); EV has
   `operator_pause_status` (single event with `status ∈ {pausing, paused}`).
   EV §8.9(h) is explicit that paired-phase lifecycles MUST NOT split; the
   ON two-event shape is EV-non-conforming. Either ON updates its §6.5 and
   §4.3.ON-013 to emit a single `operator_pause_status` event with a
   `status` field, OR EV amends §8.9(h) to accept the split. Recommend
   ON-side fix — EV §8.9(h) is a normative-in-EV invariant and ON should
   not redeclare the shape inconsistently with it.
2. ON does not name `operator_command_rejected` in §6.5 (though it IS used
   in ON's §8 exit-code table, exit code 16, line 552). Add to §6.5, or
   explicitly hand off "rejected_at" to [event-model.md §8.7.12].
3. ON does not name `dispatch_deferred` in §6.5 though §4.10.ON-041 /
   §8 exit code 18 (line 554) rely on its emission. Same fix.

None of these is a scope leak in the "ON redeclares payload shape" sense;
they are emission-site coverage mismatches.

### §4.7.ON-027 graceful-shutdown sequence (line 260) vs PL §4.4

ON's §4.7.ON-027 enumerates a seven-step graceful shutdown (1 stop dispatch,
2 runs to checkpoint, 3 handler subprocesses complete, 4 event bus fsync, 5
memory flush, 6 workspace unlock, 7 exit). PL-011 (PL line 172) enumerates
a seven-step daemon-level drain (1 stop dispatch, 2 runs to checkpoint, 3
wait for subprocess, 4 fsync event bus, 5 release worktree lease, 6 release
pidfile + remove socket, 7 exit).

These are overlapping but not identical — step 5 differs (ON "memory flush",
PL "release worktree lease"); step 6 differs (ON "workspace unlock", PL
"pidfile + socket"). PL line 183 says "Subsystem-level shutdown ordering is
owned by [operator-nfr.md §7.7]; this requirement names the daemon-level
sequence" — which is a principled split (PL owns daemon-level, ON owns
cross-subsystem ordering). But the sequences as written disagree on what
step N does. Recommend ON and PL co-author a canonical step list and cross-
reference rather than each author an incomplete overlap; this is the most
subtle scope leak in ON (it is not a leak in principle but a disagreement
about ordering of shared concerns).

### Budget arithmetic — not leaked, cleanly deferred

ON §4.11.ON-045 (line 392) says budgets are "declared in policy per
[control-points.md §6.9], enforced at dispatch by the agent runner per
[control-points.md §6.9], and attributed in observability." ON makes NO
attempt to specify the threshold arithmetic, per-tenant aggregation math,
or budget enforcement algorithm. CP §4.5 (where budget semantics actually
live) is cleanly cited as the owner. The only fix here is the stale
anchor (`§6.9` → `§4.5`); the scope split is correct.

### Run scheduling — not leaked, cleanly deferred

ON does not declare any run scheduling rule. `harmonik pause` is described
as a daemon status transition (§7.1 state machine) consuming EM's checkpoint
cadence (§4.5). Scheduling proper lives in EM.

## `harmonik upgrade` co-ownership with PL §4.9

Template §6.5 co-ownership rule says: when two specs share an artifact,
one spec OWNS (normative author) and the other spec CONSUMES (informative
reference). Upgrade is genuinely co-owned:

- **ON §4.6 Upgrade contract (lines 213–222).** ON-020 enumerates the five
  sub-obligations: binary source, hash check, drain interaction, cross-
  version state contract, socket retry behavior. ON-021 asserts the
  in-flight recoverability invariant. ON §7.3 Upgrade protocol pseudocode
  (lines 506–528) shows the execution sequence. This is where the contract
  LIVES.
- **PL §4.9 Upgrade obligation (PL-027, PL lines 297–301).** PL explicitly
  names that the contract is owned by ON and PL's only obligation is that
  its §4.2 startup and §4.4 shutdown sequences be consistent with what ON
  produces. This is the "named obligation" pattern — PL does not redeclare
  any of ON's five sub-obligations.

**Verdict: clean co-ownership split.** ON is the contract author; PL
consumes the obligation as a downstream constraint. No redeclaration.

One minor drift: PL-027 (PL line 297) cites `[operator-nfr.md §7.5]` as
the upgrade-contract owner, but ON's upgrade contract is now at §4.6
(not §7.5 — §7.5 doesn't exist in ON's current structure; §7 is Protocols
with §7.1, §7.2, §7.3). This is PL's side of the citation migration debt,
NOT an ON bug — but since ON is the upstream for the concept, flagging
here so the fix coordinates: ON's upgrade contract is at §4.6 (contract
statement) + §7.3 (protocol pseudocode).

Same drift applies to PL's citation of `[operator-nfr.md §7.7]` (graceful
shutdown — now §4.7 of ON) and `[operator-nfr.md §7.8]` (restart RTO —
now §4.8 of ON) and `[operator-nfr.md §7.1]` (exit-code taxonomy — now
§4.1 of ON, with the taxonomy itself at §8).

This is the "reverse-drift" issue in a different form: PL cites ON at the
components.md-era §7.N anchors, which don't exist in ON v0.2.

## Event emissions (ON §6.5, §8.7 of EV)

ON §6.5 (lines 449–461) declares WHEN events are emitted (tied to §4.3 /
§4.6 state-machine transitions). The closing sentence (line 461): "This
spec is normative for the *when*; event-model is normative for the *shape*"
is exactly the template §6.5 co-ownership phrasing.

Cross-checking against EV §8.7:

| Event | ON §6.5 claims | EV §8.7 declares | Consistent? |
|---|---|---|---|
| `operator_pausing` | emitted on `running → pausing` | Not present (EV uses `operator_pause_status` paired-phase) | ✗ (see Scope leaks above) |
| `operator_paused` | emitted on `pausing → paused` | Not present (EV uses `operator_pause_status`) | ✗ |
| `operator_resuming` | emitted on `paused → resuming` | §8.7.7 — present; payload `resumed_at` | ✓ |
| `operator_stopped` | emitted on entry to `stopped` | §8.7.8 — present; payload `stopped_at`, `mode (graceful / immediate)` | ✓ (ON uses `stop_mode` instead of `mode` — cosmetic) |
| `operator_upgrading` | emitted on `paused → upgrading` | §8.7.9 — present; payload `upgrade_version`, `started_at` | ✓ (ON says payload carries `expected_commit_hash`; EV says `upgrade_version`; divergence in field name — ON shouldn't be specifying) |
| `operator_upgrade_completed` | emitted on `upgrading → running` | §8.7.10 — present; payload `upgrade_version`, `completed_at`, `binary_commit_hash` | ✓ |
| `operator_upgrade_rejected` | emitted when hash check fails | §8.7.11 — present; payload `upgrade_version?`, `rejected_at`, `reason (hash_mismatch / schema_incompatible / not_paused)` | ✓ |

**Ownership boundary verdict:**
- ON correctly declares WHEN (transition triggering) for all seven events.
- ON stays out of the shape conversation except for the mid-bullet phrases
  like "carries `pause_reason`" which hint at a payload field. These should
  be demoted to informative notes or removed — the OWNER of that field is
  EV.
- The ON-vs-EV disagreement on the paired-phase pause shape is a real
  conflict that must resolve in one direction. EV §8.9(h) is the normative
  invariant; ON should yield.

## Reverse-drift — the ~71 inbound `[operator-nfr.md §7.N]` citations

Inventory:

| Citing spec | count | Predominant anchors |
|---|---|---|
| process-lifecycle.md | 34 | §7.3, §7.5, §7.7, §7.8, §7.1, §7.10 |
| execution-model.md | 13 | §7.3 (pause-between-runs), §7.5 (compat contract) |
| handler-contract.md | 7 | §7.1 (health surface) |
| reconciliation.md | 7 | §7.3, §7.5, §7.8, §7.10 |
| beads-integration.md | 6 | §7.4, §7.5, §7.8 |
| event-model.md | 3 | §7.3, §7.5, §7.8 |
| (control-points and workspace-model migrated to §4.N already) | 0 | n/a |
| **total** | **~70** | |

ON's current §7 is "Protocols and state machines" with §7.1 Operator-control
state machine, §7.2 Drain protocol pseudocode, §7.3 Upgrade protocol
pseudocode. So `§7.1` and `§7.3` cites do resolve to ON content, but not
to the content the citer intended:

- `[operator-nfr.md §7.1]` in PL-008 intends "exit-code taxonomy and
  health surface" — that now lives at §4.1 + §8 in ON v0.2. PL gets §7.1
  "Operator-control state machine" instead. Wrong target.
- `[operator-nfr.md §7.3]` in EM, PL, RC intends "Operator control
  semantics" — that now lives at §4.3 in ON v0.2. Citers get §7.3
  "Upgrade protocol pseudocode" instead. Wrong target.
- `[operator-nfr.md §7.5]` in EM, PL, RC, BI, EV intends "Checkpoint-
  format stability" / "Upgrade contract" — that now lives at §4.5 / §4.6
  in ON v0.2. §7.5 doesn't exist in ON. Target missing.
- `[operator-nfr.md §7.7]` in PL intends "Graceful-shutdown" — §4.7 in
  v0.2. §7.7 doesn't exist.
- `[operator-nfr.md §7.8]` in PL, EV, BI, RC intends "Restart RTO" —
  §4.8 in v0.2. §7.8 doesn't exist.
- `[operator-nfr.md §7.10]` in PL, RC intends "CLI surface" reference —
  there isn't an ON §7.10 and the "separate spec" note lives in ON's §1
  Purpose (line 30) referring to [docs/foundation/components.md §7.10].

**Recommendation: ON publishes a migration map in revision history.**

The lightest-weight fix is a table in ON's §12 Revision history for v0.2
(or a new v0.2.1 entry) mapping components.md-era §7.N numbering to the
current structure:

```
| legacy §7.N | current ON v0.2 target |
|---|---|
| §7.1 exit-code taxonomy, health | §4.1 (obligations) + §8 (table) + §4.9.ON-036 (health) |
| §7.2 not used | — |
| §7.3 operator-control semantics | §4.3 |
| §7.4 queue-format contract | §4.4 |
| §7.5 N-1 compat window | §4.5 |
| §7.5 (alt intent) upgrade contract | §4.6 |
| §7.6 N-1 compat for consumers | §4.5.ON-018 |
| §7.7 graceful shutdown | §4.7 |
| §7.8 restart RTO | §4.8 |
| §7.9 observability envelope | §4.9 |
| §7.10 multi-daemon / CLI deferral | §4.10 + §1 Purpose CLI-surface note |
```

This frees downstream specs to migrate at their own pace without chasing
ON's headings. Without this map, every downstream reviewer has to
reverse-engineer the rename.

Alternative: require each downstream spec to migrate on its own. This is
what the corpus-cleanup plan seems to assume (per the task-prompt note:
"deferred"). Fine, but then ON has 70-ish broken inbound anchors that
linger until the cleanup completes. Publishing the map is a cheap
insurance policy.

Recommend: ON SHOULD publish the migration map in its §12 Revision history.

## Bootstrap citations

Scanned ON body + §9 for `[docs/foundation/components.md §N]`,
`[docs/foundation/problem-space.md §…]`, `[docs/foundation/OVERVIEW.md §…]`
patterns.

Found:
- Line 30 (§1 Purpose): `[docs/foundation/components.md §7.10]` — CLI
  surface spec locus. This cite is a **legitimate** bootstrap citation —
  the separate "operator-CLI-surface spec work" has not been drafted, so
  components.md is the extant locus. OK per template bootstrap rule.
- Line 605 (§9.3): `[docs/foundation/problem-space.md §Locked decisions]`.
  Legitimate bootstrap cite — problem-space.md is a foundation document,
  not a spec.
- Line 606 (§9.3): `[STATUS.md §Decisions Locked In]`. Legitimate.
- Line 604 (§9.3): `[docs/foundation/project-level/build-practices.md
  §Branch model]`. Legitimate.

Two observations:
1. Per ON-INV-002 "No PR-gated rollout for MVH" (line 411), ON consumes
   build-practices.md. Cited appropriately as a co-ref, not as a
   depends-on.
2. ON line 30 cites components.md §7.10 in the SPEC PURPOSE (not a §9
   cross-ref). This is legitimate under the bootstrap rule but the cite
   should migrate to the CLI-surface spec once it's drafted (tracked
   implicitly — no OQ needed).

**No stale bootstrap cites** (i.e., no cites to components.md for concepts
that HAVE been migrated to a live spec). Good.

## Recommended dependency-graph edits

Prioritized, lowest-risk-first.

1. **Republish §7.N → §4.N migration map in §12 Revision history.**
   Single-table edit. Immediately unblocks ~70 inbound citations across
   the corpus. No semantic change to ON. Priority HIGH.

2. **Fix stale `[event-model.md §3.N]` anchors** across ON body and §9.
   Batch update; 14+ sites. Mapping is mechanical:
   - §3.1 → §4.1 (envelope)
   - §3.2 → §8 (taxonomy) with per-event `§8.N.M` where the citation
     targets a specific event type
   - §3.4 → §4.4
   - §3.5 → §4.7
   - §3.6 → §4.5
   - §3.7 → §4.3 or §6.2
   - §3.8 → (does not exist in EV) — either soften to name-only
     obligation or promote to ON-owned normative statement
   Priority HIGH.

3. **Fix stale `§6.8 / §6.9 / §6.11` anchors for control-points.**
   Mechanical:
   - §6.5 policy schema → §6.3 (YAML) or §4.7 (grammar)
   - §6.8 config loading → §4.7.CP-037
   - §6.9 budget → §4.5
   - §6.11 skill declaration → §4.11
   Priority HIGH — these shade deceptively (CP §6 exists, just with
   different subsections).

4. **Fix stale anchors for reconciliation (§9.N → §4.N + §8.N), beads-
   integration (§10.N → §4.N), workspace-model (§5.N → §4.N), process-
   lifecycle (§8.N → §4.N).** Mechanical per the mapping tables above.
   Priority MEDIUM (same drift pattern as other specs in the deferred
   cleanup, but leaving it visible in ON is worst-case for downstream
   readers).

5. **Rename `handler_silent_hang` to `agent_warning_silent_hang`** in
   §4.9.ON-040 (line 352). Matches the canonical name in EV §8.3.10 +
   HC §7.1. Priority MEDIUM.

6. **Fix the operator-pause paired-phase shape.** §4.3.ON-013 (line 165)
   and §6.5 (line 453–454) declare `operator_pausing` + `operator_paused`
   as two distinct events. EV §8.7.6 + §8.9(h) declare a single
   `operator_pause_status` event with `status ∈ {pausing, paused}`.
   Either ON updates its emission declarations to a single event with a
   status field, or EV relaxes §8.9(h) for this specific case. Recommend
   ON-side fix. Priority MEDIUM.

7. **Add `operator_command_rejected` and `dispatch_deferred` to ON §6.5**
   co-owned event list. Both are emitted by ON-declared surfaces (§8
   exit codes 16 and 18) but missing from §6.5. Priority LOW.

8. **Resolve ON ↔ PL cycle.** Recommend: ON keeps PL in depends-on; PL
   drops ON to §9.3. PL-side work is already advised by PL's architect
   reviewer. ON-side action: no change — keep PL in `depends-on`.
   Priority MEDIUM (cycle remains until PL side is updated).

9. **Consider promoting structured-log declaration to ON.** §4.9.ON-035
   cites `[event-model.md §3.8]` for structured logs, but EV §10.3
   explicitly excludes structured-log format and EV OQ-EV-002 names
   `quality-checks.md` (not yet existing) as owner. Since ON is the NFR
   spec, declaring "every subsystem emits structured logs" as an ON-
   owned requirement (citing quality-checks.md as the future wire-format
   spec) is more honest than a cite-to-nowhere. Priority LOW.

10. **Coordinate ON §4.7 vs PL §4.4 drain step lists.** The two specs
    enumerate overlapping-but-distinct seven-step sequences. Either ON
    owns the cross-subsystem sequence and PL defers to it entirely, or
    PL owns the daemon-level sequence and ON only names the cross-
    subsystem concerns (workspace unlock, memory flush) that PL doesn't
    touch. Recommend ON owns cross-subsystem; PL cites ON and does not
    duplicate steps. Priority LOW (both lists are internally consistent;
    this is a sibling-spec coordination issue).

## Affirmations

1. **§6.5 co-ownership shape is textbook.** The ON-emits-WHEN /
   EV-emits-WHAT split is declared explicitly on line 461 and applied
   consistently to all seven operator-control events. This is the
   template §6.5 pattern done correctly and should be cited as an
   exemplar for other specs doing similar co-ownership.

2. **Scope discipline is strong.** ON does NOT redeclare: event payload
   shape (EV owns), daemon startup sequence (PL owns), handler secrets
   propagation (HC owns), budget arithmetic (CP owns), run scheduling
   (EM owns). ON-INV-001 frames the cross-artifact N-1 invariant as a
   JOINT constraint across EV + EM + CP + BI without redeclaring any
   version field — the invariant cites the owning spec for each
   artifact's version field. This is the right shape.

3. **Exit-code taxonomy is complete and well-cross-referenced.** §8
   table (lines 534–554) covers codes 0–18 with detection rule,
   emitted event, and remediation pointer for each. Every downstream
   reference (e.g., PL-002's "pidfile-locked" exit code) resolves into
   this taxonomy. The §8 taxonomy note (line 556) preserves the code-
   stability rule under N-1.

4. **Between-task invariant is well-articulated.** §4.3.ON-008 + ON-009
   + ON-INV-004 + §7.1 state-machine transitions form a coherent
   single-exception system: pause, upgrade, improvement-pause all
   drain; only `stop --immediate` aborts. The locked-decision-#10
   anchor (§9.3 line 605, §A.3 Rationale line 691) makes the reopen
   protocol explicit.

5. **Restart RTO criteria are honestly framed.** §4.8.ON-032 criteria 1
   (30s p95 nominal) and criterion 3 (300s hard ceiling) are pinned as
   "relaxable with reason" vs. "non-negotiable" — the right split for
   an MVH target where the nominal number is an estimate and the
   ceiling is an operator-observability boundary. ON-INV-005 codifies
   the ceiling invariant cleanly.

6. **`harmonik upgrade` contract is a single-author artifact.** §4.6
   + §7.3 together enumerate the five sub-obligations (binary source,
   hash check, drain interaction, cross-version state, socket retry)
   in one place. PL-027 correctly delegates as a named-obligation
   reference. Template §6.5 co-ownership rule honored.

7. **`operator_pausing` field / `pause_reason = improvement` design**
   for the improvement-pause subtype (§4.3.ON-012) avoids introducing
   a new top-level state for a cognitive subclass of pause. This is
   the right MVH choice — locked decision #10 already says improvement-
   pause is a subtype — and the ON realization (extra payload field
   instead of extra state) keeps the state-set small.

8. **OQ-ON-001 through OQ-ON-005 are well-scoped.** Each OQ has an
   owner, a blocking-dependence note, and a default-if-unresolved.
   None is load-bearing on a locked decision. OQ-ON-002 explicitly
   wires in the testing.md migration debt; OQ-ON-003 calls out the
   machine-ceiling-coordinator implementation-choice tension without
   deferring it to "whatever"; OQ-ON-004 acknowledges the concurrent-
   attach arbitration silence honestly. Good OQ hygiene.
