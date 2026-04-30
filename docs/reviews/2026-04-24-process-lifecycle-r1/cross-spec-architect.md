# Round 1 Cross-Spec Architect Review — process-lifecycle.md v0.2.0

## Verdict summary

PL sits approximately right in the dependency graph: it correctly claims the
per-project daemon / startup / ready-state / shutdown / agent-subprocess /
ntm-adapter / daemon-vs-orchestrator-agent / crash / upgrade-obligation
footprint, correctly cedes event payload shapes, reconciliation taxonomy,
handler launch mechanics, workspace lease implementation, and the cross-
subsystem graceful-shutdown ordering to their owning specs, and draws the
daemon-vs-orchestrator-agent split cleanly at PL-018 / PL-019 / PL-020.

Four material issues:

1. **Direct front-matter cycle with operator-nfr.** PL declares
   `depends-on: operator-nfr` (L19); operator-nfr.md L13 declares
   `depends-on: process-lifecycle`. Per the intra-foundation co-dependency
   convention that resolves the execution-model ↔ event-model edge
   directionally, this pair needs a directional split.
2. **Widespread stale-anchor drift across five sibling specs** (58 cites:
   18 operator-nfr `§7.N`, 14 reconciliation `§9.N`, 13 event-model `§3.N`,
   5 beads-integration `§10.N`, 5 workspace-model `§5.N`, 3 execution-model
   `§2.1`). PL's v0.2.0 cleanup migrated only architecture.md anchors;
   remainder is the batch-2 corpus-wide migration. Flagged for catalog;
   not expected to fix in this review.
3. **AR-INV-007 (centralized-controller invariant) is not cited in §9.1**
   despite being the load-bearing architectural invariant PL-INV-002 and
   PL-018 / PL-019 / PL-020 all satisfy. §9.1 cites `[architecture.md §4.9]`
   (the section header, now a retirement stub at architecture.md L327–L329
   pointing to AR-INV-007). Masks that PL is the invariant's primary
   enforcer.
4. **Three daemon-startup bootstrapping obligations are silently omitted:**
   event-bus startup, control-point registry bootstrap, JSONL writer
   startup. Each is implied by AR-INV-007 "all cross-subsystem registries
   are daemon-owned and in-process" and by PL-020's composition-root
   declaration, but none is an enumerated step in PL-005's startup
   sequence.

## Dependency graph correctness

### Front-matter `depends-on` list (L14–L23)

Declared: `architecture, execution-model, event-model, handler-contract,
operator-nfr, reconciliation, beads-integration, workspace-model`. Eight
entries; no control-points. PL body does not cite control-points, so the
omission is correct (control-points is cited transitively via handler-
contract and operator-nfr).

**Direct cycle with operator-nfr.** PL depends-on operator-nfr;
operator-nfr depends-on process-lifecycle. Entanglement:
- PL cites operator-nfr 9 times on §7.3 (operator-control state machine)
  — PL's `starting/reconciling/ready/degraded` prefix hands off to
  operator-nfr's `running/paused/draining/stopped/upgrading` suffix.
- operator-nfr cites `[process-lifecycle.md §8.1]`, `§8.2`, `§8.3`, `§8.4`
  at operator-nfr.md L585–L588 — per-project daemon scope, startup
  sequence, command surface, queue-empty behavior.

The execution-model ↔ event-model precedent in the R1 exemplar (`docs/
reviews/2026-04-24-execution-model-r1/cross-spec-architect.md` L22–L29)
resolves directionally: one spec carries `depends-on`, the other carries a
§9.3 cite.

**Recommended resolution.** Since PL owns the daemon PROCESS SHAPE
(startup sequence, command surface, `starting → ... → ready` prefix) and
operator-nfr builds OPERATOR SEMANTICS on top (pause/stop/upgrade/drain
machinery that runs after PL reaches `ready`), dependency flows
operator-nfr → PL. PL should DROP operator-nfr from `depends-on` and
move operator-nfr cites to §9.3. PL-008 and PL-027 are "named obligations"
per template §2 and §9 — which are legal without a forward dependency.

Either direction breaks the cycle; the both-directions shape is invalid.

### Other cycle checks

- **reconciliation**: PL depends-on RC; RC does NOT list PL in front-
  matter but body cites `[process-lifecycle.md §8.2]` 5 times
  (reconciliation/spec.md L75, L185, L336, L496, L685) and §9.3 L685
  names the co-reference. No cycle; the resolution is directional as
  intended.
- **handler-contract**: the task brief says HC cites PL "for launch
  protocol on subprocess start." PL lists HC in depends-on. If HC r1
  lands with `depends-on: process-lifecycle`, that creates a second
  cycle mirroring execution-model ↔ event-model. Verify at HC r1.
- **beads-integration, workspace-model**: neither lists PL in its front-
  matter. No cycle.

### §9.1 / §9.2 / §9.3 consistency

§9.1 (L395–L419) lists 20 entries. Every `depends-on` subject has at least
one §9.1 entry. But two body cites are NOT mirrored in §9.1:
- `[operator-nfr.md §7.10]` (PL-001 at L76, multi-daemon commands).
- `[operator-nfr.md §7.6]` (§6.3 at L367, N-1 compatibility).

Add both to §9.1 or drop body cites. (They're stale anchors; §4.10 and
§4.5 respectively per the batch-2 migration, but the §9.1 omission is
separate.)

§9.3 (L426–L430) lists core-scope.md §5 / §6 and components.md §8
bootstrap. All three correct.

§9.2 reverse-deps INFORMATIVE block (L423) names HC, WM, operator-nfr,
RC, BI — consistent with task-brief's "consumed by" list.

## Citation correctness — walk

Every `[<spec>.md §N.N]` in PL was resolved against the current target
spec. Summary of stale vs valid:

| Target | PL sites | Stale? | Correct anchor for what PL means |
|---|---|---|---|
| architecture.md | §4.1, §4.4, §4.9 | Valid (but §4.9 → AR-INV-007; see below) | n/a |
| execution-model.md | `§2.1` ×3 | ALL STALE | `§4.4` (checkpoint contract) or `§6.2` (trailer format); `§2.1` is "In scope" |
| event-model.md | `§3.2` ×10, `§3.4` ×3 | ALL STALE | `§8.7` (daemon-lifecycle taxonomy) / `§6.3` (per-type schemas) / `§6.2` (JSONL file layout) / `§4.4` (fsync) |
| handler-contract.md | §4.1, §4.3, §4.6, §4.11, §4.12 | All valid | n/a |
| operator-nfr.md | `§7.1` ×5, `§7.3` ×9, `§7.5` ×2, `§7.6` ×1, `§7.7` ×2, `§7.8` ×2, `§7.10` ×1 | ALL STALE | `§4.1` (exit codes / catalog), `§4.3` (operator-control semantics), `§4.6` (upgrade), `§4.5` (N-1 compat), `§4.7` (shutdown), `§4.8` (RTO), `§4.10` (multi-daemon) |
| reconciliation/spec.md | `§9.1a` ×1, `§9.2` ×5, `§9.2a` ×3, `§9.3` ×8 | ALL STALE | `§4.1` / RC-002/003 / RC-INV-002 (idempotence), `§4.2` (action-mapping) or `§8.12`, `§4.3` (detectors incl. Cat 0 RC-012), `§8` (taxonomy) |
| beads-integration.md | `§10.8` ×4, `§10.9` ×1 | ALL STALE | `§4.10` (`br`-adapter idempotency) / `§6.2` (intent-log layout); `§4.9` (Beads-CLI skill) |
| workspace-model.md | `§5.1` ×5 | ALL STALE | `§4.3` (lease model); `§5` is Invariants |

**Total stale: 58 cites.** Per task brief this is the deferred post-batch-2
migration; do not fix in this review.

**Valid cites spot-checked:**
- L212 `[handler-contract.md §4.11]` → Skill injection (HC L446). ✓
- L218 `[handler-contract.md §4.3]` → Concurrency model (HC L167). ✓
- L225 `[handler-contract.md §4.6]` → Error propagation incl. silent-hang (HC L272). ✓
- L268 `[handler-contract.md §4.12]` → Handler as modularity boundary (HC L494). ✓
- L68, L248 `[architecture.md §4.4]` → Subsystem envelope. ✓

**Events spot-checked against event-model §8.7:**
`daemon_ready` (§8.7.2, event-model L161), `daemon_orphan_sweep_completed`
(§8.7.14, L173), `infrastructure_unavailable` (§8.7.15, L174), `agent_failed`
(§8.3). All three PL-emitted daemon-lifecycle events are registered.
Emission-ownership is clean; only the citation anchors are stale.

## Scope leaks

**PL-006 worktree-lock staleness criterion (L125).** PL says "Locks whose
mtime predates the current daemon's start time MUST be removed." The
"what counts as stale" predicate is workspace-model's contract, not PL's.
Tighten to "Locks meeting the staleness criterion of [workspace-model.md
§4.3] MUST be removed" and let WM own the predicate.

**PL-006 subprocess identification (L126).** "Processes that have been
re-parented to init ... whose binary path matches a handler binary under
the project's expected launch path" — the "expected launch path" notion
lives with handler-contract §4.1 / §4.10 (launch spec). PL should cite
HC as the launch-path authority rather than implicitly re-declare.

**PL-015 agent commands (L212).** PL-015 asserts `harmonik claim-next` /
`harmonik emit-outcome` route over the socket. The operator-to-agent
command set is handler-contract §4.2 (wire protocol)'s to declare; PL
owns "these commands route over the socket." Tighten PL-015 to "agent-
side commands (as defined by [handler-contract.md §4.2 wire protocol])
MUST route over the socket." PL-015's skill-injection cite to HC §4.11
and BI §4.9 is correct ownership split.

**No reconciliation-logic leak.** PL-005 step 7 (L112) dispatches through
RC's action-mapping; PL-025 (L283) names idempotence per RC's invariant;
PL-INV-003 (L332) declares ordering (sweep first), RC-INV-005 declares
scoping (detect by run_id). The two invariants compose without overlap.

**Reverse direction — obligations PL should own, does not:**
- Startup-reconciliation DISPATCH is correctly owned by PL (PL-005 step 7);
  RC owns the taxonomy/action-map that PL dispatches through. No leak.
- Cat 0 pre-check: PL owns the INVOCATION (PL-005 step 3, PL-010); RC
  owns the detection logic (RC-012). Correct split.
- Event-bus startup, control-point registry bootstrap, JSONL writer
  startup: NONE are enumerated as steps in PL-005. See "Ownership
  conflicts" below.

## Ownership conflicts — daemon as invoker

Six daemon-invoked contracts walked:

1. **Reconciliation workflow dispatch** — PL-005 step 7 (L112) cites RC's
   action-mapping (stale `§9.2a` → `§4.2`). Invocation-in-PL, contract-
   in-RC. ✓
2. **Beads probe** — PL-005 step 5 (L110) cites BI (stale `§10.8` →
   `§4.5`). PL invokes, BI owns `br` access. ✓
3. **Orphan worktree sweep** — PL-006 (L125) cites WM (stale `§5.1` →
   `§4.3`). PL owns the SWEEP; WM owns the lock shape. ✓
4. **Control-point registry bootstrap** — **NOT CITED.** AR-INV-007 says
   "all cross-subsystem registries ... are daemon-owned and in-process";
   PL-020 asserts composition root but does not enumerate registry
   instantiation as a startup step. Gap.
5. **Event bus startup** — **NOT CITED.** Event-model §4.3 declares bus
   shape; EV-014 declares subscription at registration. PL-005 has no
   "start bus, register consumers" step. Gap.
6. **JSONL writer startup** — **NOT CITED.** Event-model §6.2 declares
   on-disk path; writer runs inside the bus stack. Same gap as (5).

**Recommended edit.** Add a PL-005 step 0 (before the current step 1):
```
0. Bootstrap composition-root-resident subsystems: instantiate the event
   bus, control-point registry, handler registry, and skill registry per
   [architecture.md AR-INV-007]. Register each subsystem's consumers and
   providers before reading any external state.
```

This makes the registry-bootstrap sequence explicit and prevents a silent
assumption that the composition root magically wires these before step 1.

## Daemon vs orchestrator-agent split

**PL-018 (L232–L236):** "daemon MUST be a deterministic Go binary ... MUST
NOT call any LLM, MUST NOT import any LLM SDK, MUST NOT embed any
cognition-bearing component." Sharp, unambiguous prohibition.

**PL-019 (L238–L244):** "An orchestrator-agent MUST be a separate Claude
Code session sitting on top of the daemon ... MUST interact through the
CLI ... MUST NOT share process space." OPTIONAL in MVH. Cognition tag
carries a complete delegation path per AR-007 (role = orchestrator-agent;
model-class = Claude Code; input shape = CLI responses + Beads reads).

**PL-020 (L246–L250):** composition root at `internal/daemon`; subsystems
forbidden from importing each other directly. Enforces the process-level
LLM-freedom invariant structurally.

**§7.1 end note (L387):** "The orchestrator-agent is NOT a state in this
machine; it is a separate process with its own lifecycle." Reinforces the
boundary at the state-machine level.

**No blurring found.** The split is drawn cleanly at PL-018 / PL-019 /
PL-020 and reinforced at §3 Glossary, §7.1, §10.1. The "centralized-
controller is daemon core; orchestrator-agent layers on top" framing (task
brief) is explicitly satisfied.

**Minor nit.** PL-019 L240's "(or coordinator-agent)" parenthetical alias
is noise — §3 Glossary L62 defines only "orchestrator-agent"; drop the
parenthetical unless "coordinator-agent" is a deliberately-introduced
distinct concept.

## `harmonik upgrade` contract (§4.9 / PL-027)

PL-027 (L297–L301) names the obligation and cites operator-nfr §7.5
(stale → §4.6 / ON-020) as owner. Lists the five sub-elements operator-
nfr's spec-draft MUST specify: (a) binary-source mechanism, (b) operator-
supplied hash check, (c) drain-vs-reconciliation interaction, (d) cross-
version state contract, (e) socket retry during exec-replacement. Closes
with "this spec's only obligation is that PL-005 and PL-011 be consistent
with whatever operator-nfr produces."

**Co-ownership split is template-compliant.** Per template §6.5, one spec
owns the normative shape and siblings cite; for a cross-spec obligation,
operator-nfr owns the CONTRACT (ON-020 / §7.3 upgrade protocol pseudocode),
PL names the OBLIGATION and cites operator-nfr.

**Sub-element scope check:** of the five sub-elements, (a)/(b)/(d) are
pure operator-nfr (binary source, hash check, schema compat); (c) is
joint (PL-011 drain × RC-003 in-flight investigators × ON-010 pause carve-
out); (e) touches PL-003 (socket). PL-027 lists these as REQUIRED
ATTRIBUTES of operator-nfr's draft, not ownership claims. The framing is
legitimate.

**Minor tightening.** PL-027 could add explicit cross-refs to PL-011 for
(c) drain-ordering and PL-003 for (e) socket-retry. Currently implicit via
the closing sentence.

**Verdict: clean co-ownership split.** Stale anchor (§7.5 → §4.6) is a
citation issue, not scope.

## AR-INV-007 (centralized controller) referencing

AR-INV-007 (architecture.md L435–L439) asserts:
- Daemon owns all workflow state, routing, dispatch.
- Agents perform only cognitive work.
- Agent-to-agent coordination routes through daemon.
- **All cross-subsystem registries are daemon-owned and in-process.**

**PL's enforcement:**
- PL-018 (no-LLM daemon) — direct.
- PL-019 (orchestrator-agent separate) — preserves "daemon as sole
  driver."
- PL-020 (composition root) — structurally enforces "registries in-
  process."
- PL-INV-002 (daemon is deterministic) — projection of AR-INV-007.

**Current §9.1 cite is wrong.** §9.1 L399 lists `[architecture.md §4.9]`.
Architecture v0.3.0 RETIRED the AR-037 content at §4.9 (retirement stub at
architecture.md L327–L329) and promoted it to **AR-INV-007**. The cite
`§4.9` still resolves to a section anchor, but the requirement identity is
now AR-INV-007.

**Recommended edits:**
1. §9.1 L399: replace `[architecture.md §4.9]` with
   `[architecture.md AR-INV-007]`. Edit the description to name PL-018 /
   PL-019 / PL-020 / PL-INV-002 as this spec's enforcement sites.
2. PL-INV-002 L328: replace `[architecture.md §4.9]` with
   `[architecture.md AR-INV-007]`.

**Under-cited: AR-INV-007 registry clause.** The "cross-subsystem
registries are daemon-owned in-process" clause is load-bearing for PL-020,
but PL does not explicitly cite it nor enumerate registry residence as a
PL obligation. Add either a PL-020 clause or a new PL-020a: "All cross-
subsystem registries (policy, control-point, handler, skill) declared by
foundation specs MUST be instantiated inside the composition root on
startup; no out-of-daemon registry is permitted for MVH per [architecture.md
AR-INV-007]."

## Bootstrap citations

PL contains two `components.md` cites:
- L429 (§9.3 co-references): `[docs/foundation/components.md §8]` as
  migration source. Correct bootstrap shape. Keep.
- L490 (§12 revision history v0.1.0 row): same migration note. Keep.

No other bootstrap cites. All other inter-spec references are to
finalized / draft spec files.

`[core-scope.md §5]` (L240) and `[core-scope.md §6]` (L248) resolve to
core-scope.md's "Section 5 — Orchestrator loop" and "Section 6 —
Subsystem organization" headings. Valid per core-scope's heading
convention.

The one effective bootstrap problem is `[architecture.md §4.9]` (addressed
above) — migrated-in-source-but-not-migrated-in-cite.

## Recommended dependency-graph edits

### Can land in this review

1. **§9.1 L399** — migrate `[architecture.md §4.9]` to
   `[architecture.md AR-INV-007]`. Rewrite description to name PL-018 /
   PL-019 / PL-020 / PL-INV-002 as enforcement sites.

2. **PL-INV-002 L328** — replace `[architecture.md §4.9]` with
   `[architecture.md AR-INV-007]`.

3. **§9.1** — add entries for `[operator-nfr.md §7.6]` and
   `[operator-nfr.md §7.10]` (both cited in body, neither in §9.1). Mark
   stale anchors for batch-2 migration.

4. **PL-019 L240** — drop "(or coordinator-agent)" parenthetical unless
   coordinator-agent is a distinct concept.

### Requires cross-spec coordination

5. **Front-matter `depends-on` — drop `operator-nfr`** (cycle
   resolution). Move every operator-nfr cite from §9.1 to §9.3 co-
   references. Alternative: keep PL's dependency, coordinate operator-nfr
   to drop its PL dep. Either direction breaks the cycle.

6. **Handler-contract dependency direction** — coordinate at HC r1. If
   HC adds PL to its front-matter depends-on, one side must drop.

### Scope tightening

7. **PL-006 L125** — cite WM §4.3 for the worktree-lock staleness
   predicate rather than asserting the mtime rule locally.

8. **PL-015 L212** — cite HC §4.2 (wire protocol) as the authority on
   agent-side commands; PL owns the "route over socket" clause.

### Bootstrapping gap

9. **PL-005 — add composition-root bootstrap step** (new step 0 before
   the current step 1) to cover event-bus startup, registry
   instantiation, JSONL writer startup per AR-INV-007.

10. **PL-020 or new PL-020a** — explicitly declare registry residence
    inside the composition root per AR-INV-007's registry clause.

### Deferred batch-2

11. Migrate 58 stale cites per the citation-correctness walk. Not
    expected in this review.

## Affirmations

1. **Daemon-vs-orchestrator-agent split is drawn cleanly.** PL-018 /
   PL-019 / PL-020 / §7.1 end-note / §10.1 MVH carve-out / §3 Glossary all
   reinforce the boundary without language ambiguity. PL-019's cognition
   tag carries a complete AR-007 delegation path.

2. **PL-027 `harmonik upgrade` obligation is a template-compliant name-
   and-cite.** PL names, operator-nfr produces; PL lists the five required
   sub-elements without claiming ownership of any. §10.3 conformance-
   excluded-claims correctly disclaims upgrade-contract test coverage.

3. **PL-008 startup failure-mode catalog is correctly delegated** to
   operator-nfr §4.1 (stale cite `§7.1`) without re-declaring catalog
   content.

4. **Orphan-sweep-before-classification ordering** (PL-INV-003) is a
   load-bearing invariant correctly scoped. PL says "sweep before detect,"
   RC-INV-005 says "detect by run_id." Invariants compose without overlap.

5. **Pidfile / socket / file-surface ownership at PL-001 / -002 / -003 /
   -004 is exhaustive and per-file precise.** Daemon-resident file set
   fully enumerated (L94–L96); each citation names the owning spec.

6. **ntm adapter boundary is drawn at the handler contract** (PL-021 /
   PL-022 / PL-023). PL-022's enumeration of what ntm adapter MUST NOT
   import (Pipeline, SwarmPlan, checkpoint/recovery, Agent Mail) is
   explicit and load-bearing for the "Gas Town anti-pattern" resolution.

7. **§7.1 state-machine ownership split is clean.** PL's
   `starting → reconciling → ready` + `degraded` prefix; operator-nfr's
   `ready → paused → draining → stopped → upgrading` suffix. Table at
   L376–L386 correctly marks hand-offs without double-ownership.

8. **§10.2 test-surface obligations** name mechanism-tagged sensors where
   available (PL-018 / PL-INV-002 import-graph lint; PL-020 go-arch-lint;
   PL-021 / PL-022 / PL-023 ntm-import lint). OQ-PL-001 names the
   migration to testing.md citations. No reviewer-enforced sensor is
   claimed without justification.
