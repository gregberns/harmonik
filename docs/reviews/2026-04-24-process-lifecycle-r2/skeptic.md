# Round 2 Skeptic Review — process-lifecycle.md v0.3.0

## Verdict summary

R1 integration did a lot of structural work. §4.a envelope lands, the pidfile
primitive is named and correct (flock/F_OFD_SETLK with the POSIX-fcntl
forbid), the socket wire format is pinned to JSON-RPC 2.0 over NDJSON, the
provenance-marker story closes the cross-project-kill hole R1 flagged as
blocking, a panic barrier and per-daemon concurrency ceiling appear, PL-027
was properly split into daemon-internal and operator-facing halves, and the
two new invariants (PL-INV-004 socket-path exclusivity, PL-INV-005 subprocess
parentage) close the gaps R1 flagged. Good pass.

But the integration accumulated at least one hard cross-spec inconsistency
that should be caught before `reviewed` ships — **PL-008a exit codes 11 and
12 collide with operator-nfr §8 codes 11 (`drain-timeout-escalated`) and 12
(`rto-hard-ceiling-exceeded`); ON already assigns `runtime-panic` to code
19.** PL-008a declares itself "consistent with [operator-nfr.md §8]" but is
not. This is the Challenge-1-shaped regression of R1: the integration fixed
the OUTBOUND cites, but the "interim catalog" introduces NEW numbers that
collide with the authoritative table. PL cannot transition to `reviewed`
while claiming consistency with a table it contradicts.

Beyond that, three load-bearing softnesses remain: **PL-011 step 3's
"stop-advancing" has no mechanical definition of where a mid-agent-run
stops**; **PL-006a's environment-variable marker is readable only from the
parent daemon, not from a foreign daemon doing a multi-project sweep race**;
and **PL-020a claims registries "reside in" the composition root without
addressing orchestrator-agent read access to the handler/skill registries**
— the orchestrator-agent is declared a separate OS process (PL-019) but the
registries are daemon-in-process-only per AR-INV-007.

Recommendation: **do not transition to `reviewed` yet**. One more tightening
pass on the exit-code conflict, the drain-stop-advancing mechanical
definition, and the registry-residence / orchestrator-agent access question.
Rework budget ~30–60 lines.

## Integration-fix audit (did R1 fixes actually fix things?)

For each claimed fix in the §12 0.3.0 revision-history row (line 789):

- **PL-002a fd-lifetime advisory lock.** Genuine fix. Names flock vs
  F_OFD_SETLK, forbids POSIX F_SETLK with the correct justification
  (fd-close-releases-lock hazard), disambiguates live-held from stale-plus-
  dead, and cites `kill(pid, 0)` probe. The `proc/cmdline` corroboration is
  named. The OQ-PL-007 ambiguity refusal path is deferred honestly. Hidden
  assumption: see below (exec-replacement § 4.9 PL-027(i) re-acquires the
  lock — but in darwin `flock` specifically, not fcntl; consistency with
  PL-002a's Linux-permitted F_OFD_SETLK is silent at exec-replace time).
  Net: solid.

- **PL-003 socket mode 0600 + HC-044 reach-in.** Genuine fix but shallow.
  The spec says "socket authenticity is filesystem-permission-based per
  [handler-contract.md §4.10 HC-044]." HC-044 confirms "daemon socket MUST
  be mode `0600` owned by the daemon user; per-connection challenges are
  deferred post-MVH." That's consistent. What's NOT addressed: HC-044
  also says "On Linux, handler subprocesses SHOULD install
  `PR_SET_PDEATHSIG(SIGTERM)` at spawn time" — PL-014 is silent on whether
  the daemon IMPOSES this flag at spawn or whether the handler binary
  does. If the daemon imposes it (which is the only reading that makes
  sense for "parentage is structural"), PL-014 should say so; if the
  handler imposes it, HC-044 names the obligation but PL-014's "spawn
  carries the provenance marker" story doesn't name pdeathsig as one of
  the spawn-site obligations. Minor.

- **PL-003a JSON-RPC 2.0 wire format.** Present, defers method inventory
  to OQ-PL-005. Unresolved hidden assumption — see Hidden Assumption 1.

- **PL-006a project-hash SHA-256 first-12 + env var + PGID.** Closes
  multi-project kill hole R1 Challenge 4 flagged as blocking. But see
  Hidden Assumption 2 below — the SHA-256-first-12 truncation, the
  env-var readability assumption, and the PGID mechanism all carry
  failure modes the spec doesn't surface.

- **Startup-sequence step 0 (event bus + registries + JSONL writer).**
  Genuine fix. Adds a clean composition-root-bootstrap step so the event
  bus exists before any event can be emitted — resolves the PL-008a
  step-0-didn't-happen-yet case ("For failures that occur BEFORE step 0
  [...] the daemon MUST emit only the exit code to stderr; the event
  surface is unreachable" — line 261). Correct.

- **PL-008a exit-code catalog.** **NOT CORRECT — see Verdict summary and
  Hidden Assumption 6.** Collides with operator-nfr §8 at codes 11, 12;
  misplaces `panic` to code 12 (ON §8 uses 19).

- **PL-009a auto-resolver failure routing.** Closes R1 Challenge 2.
  Mechanically correct: on auto-resolver error, re-classify as Cat 3,
  dispatch investigator, add to `investigator_run_ids[]`, proceed to
  ready. One hidden assumption — see Hidden Assumption 3 below (the
  (a)/(b) sequencing still emits the ORIGINAL category in
  `reconciliation_category_assigned` even though the run has been
  reclassified, which is silently odd behavior).

- **PL-010 narrowed to pre-ready Cat 0.** Closes R1 Challenge 3 by
  explicitly declaring the narrow scope and naming OQ-PL-009 for the
  unresolved question. SIGTERM-while-degraded path stated. Genuine fix.

- **PL-011 suspend replaced with durable-checkpoint stop-advancing.** The
  word change is correct, but **the mechanical definition is missing**.
  See Hidden Assumption 4 below — this is the biggest load-bearing gap
  in the integration. R1 didn't flag it because "suspend" wasn't
  R1-visible as a term-of-art problem, but v0.3 made the semantic
  promise without providing the mechanism.

- **PL-014a per-daemon concurrency ceiling.** Declared. Default
  unbounded. Operator-configurable per [operator-nfr.md §4.3]. But see
  Hidden Assumption 5 — "per-daemon" is declared, but what enforces it?
  When `dispatch_deferred` fires, what state does the run enter, and
  who wakes it?

- **PL-018a panic barrier.** Correct in outline. Exit code 12 per
  PL-008a contradicts ON §8 where runtime-panic = 19. (See PL-008a
  audit.) The "escalation threshold is implementation-defined" clause
  (line 398) is a first-plausible-answer — should either be declared
  OQ-worthy or pinned.

- **PL-020a registry residence per AR-INV-007.** Present. Cites AR-INV-007
  correctly. But see Hidden Assumption 7 — the orchestrator-agent (PL-019)
  is declared a separate OS process with its own PID, yet PL-020a says
  cross-subsystem registries (handler, skill, policy, control-point, event
  bus) "MUST be instantiated inside the composition root." Orchestrator-
  agent access to these registries is left undefined — it interacts via
  the CLI per PL-019, but the CLI surface (PL-028) does not expose
  registry-inspection commands. This may be fine for MVH (orchestrator-
  agent is post-MVH per §2.1 and PL-019), but the spec should say so.

- **PL-021a ntm version-pin + absence-detection.** Genuine fix. Uses
  Cat 0 path, exit code 11 (but see PL-008a conflict), and explicitly
  forbids silent non-tmux degradation. Good.

- **PL-027 rewritten for upgrade.** Genuine fix. Splits daemon-internal
  mechanics (i)–(v) from operator-facing obligations. But — see Hidden
  Assumption 8 — the "environment marker set by the outgoing binary"
  (ii) is mentioned but never named or specified; the operator-facing
  contract in ON-020 can't consume a marker whose name isn't declared.

- **PL-028 runner as distinct entry-point.** Genuine fix. The four
  ordered steps (daemon start → wait-ready → open-tmux → optional
  orchestrator-agent) are concrete. Exit codes named (orchestrator-
  agent-unavailable, ntm-unavailable code 11).

- **§4.a envelope.** Present. Eight elements. Types declared with tags
  and axes. Good.

- **PL-INV-004 socket-path exclusivity, PL-INV-005 subprocess
  parentage.** Both genuine. Selection-test-passing — they do span
  workspace-model (worktree lease-lock pairing) and handler-contract
  (subprocess spawn-site convention). Sensors named.

- **Dropped operator-nfr from depends-on.** Cycle resolved. Operator-nfr
  still depends-on PL; PL names ON obligations via §9.3 co-references.
  §9.3 NOTE (line 680) documents the asymmetry correctly.

- **New OQs OQ-PL-004..009.** Reasonable. Some should be blocking —
  see Cross-spec promises below.

## Hidden assumptions v0.3.0 relies on but hasn't proven

### 1. PL-003a JSON-RPC 2.0 — demultiplexing, error format, handshake

PL-003a (lines 170–175) pins "JSON-RPC 2.0 request/response stream framed
as NDJSON per [handler-contract.md §4.2 HC-007a]." Three unstated
assumptions:

- **HC-007's progress-stream messages are NOT JSON-RPC envelopes.** HC-007
  (handler-contract.md line 121) enumerates types: `handler_capabilities`,
  `agent_ready`, `agent_started`, etc. — untyped JSON objects with
  application-specific semantics, not `{jsonrpc: "2.0", method, params,
  id}`. Both ride `.harmonik/daemon.sock` (PL-003 / HC-044). Demultiplexing
  is unspecified: per-connection role detection at connect time, or both
  framings coexist and JSON-RPC is enforced only on CLI connections? Pin.

- **JSON-RPC error format vs HC-007a abort-on-oversize.** HC-007a aborts
  the connection on >1 MiB lines; JSON-RPC 2.0 expects a `-32700 Parse
  error` object. Does an oversize CLI line emit the error object, or drop
  the connection raw? Different operator-observable behavior.

- **No version-negotiation message.** OQ-PL-005 defers the method
  inventory but not the version-negotiation question; HC-009 has the
  handler-side analog, no PL-side equivalent is named.

**Load-bearing because**: the first CLI implementation will hit one of
the three shapes with no guidance.

### 2. PL-006a project-hash — bit budget, symlink canonicalization, PGID fragility

Three stacked assumptions:

- **SHA-256 first-12 hex = 48 bits.** Birthday-bound crosses 50% collision
  at ~2^24 = 16M projects. Fine in practice. But "first 12" is pinned
  without rationale; first-16 (= 64 bits) would reach 2^32 projects
  before collision risk. If terseness for tmux names is the reason,
  name it; if arbitrary, document so post-MVH can widen without
  breaking provenance-marker comparisons.

- **`abspath(project_root)` is the hash input.** Not "realpath" — two
  symlinks pointing at the same repo produce distinct project_hashes,
  hence distinct provenance namespaces for the same project. The
  pidfile-lock side (which uses `.harmonik/` directly) resolves to the
  real path either way, so PL-INV-001 holds; but PL-INV-005 (parentage)
  breaks across the symlink boundary. One-line fix: canonicalize to
  `realpath`.

- **PGID "per-daemon-instance group-leader PID recorded in the
  pidfile"** (line 227). PIDs recycle; the recorded PGID is stale after
  daemon death. HARMONIK_PROJECT_HASH disambiguates on Linux; on darwin
  the env var is unreadable from foreign processes per OQ-PL-008 and
  PGID-only produces false positives after daemon-crash + PID-wrap.
  OQ-PL-008's default ("no filesystem fallback at MVH") is weaker than
  it looks; at minimum darwin should corroborate with process age
  before killing.

**Load-bearing because**: PL-INV-005's sensor IS the provenance marker.

### 3. PL-009a — event-stream lies about investigator's actual category

PL-009a (a)/(b) sequencing: emit `reconciliation_category_assigned` with
the ORIGINAL category (say Cat 3a), auto-resolver fails, reclassify to
Cat 3 and dispatch investigator. No
`reconciliation_category_reassigned` or `auto_resolver_failed` event is
emitted — so the event stream records Cat 3a for a run whose investigator
is working on Cat 3. Three options not named: (i) add a reassignment
event, (ii) suppress initial emission until auto-resolver succeeds
(changes RC-013 timing contract), (iii) accept the discrepancy and
document it. Spec picks (iii) implicitly.

**Load-bearing because**: an investigator-workflow author reading
`reconciliation_category_assigned` for their own run sees the wrong
category. First-day debug loop.

### 4. PL-011 step 3 "stop advancing" — mechanical boundary is undefined

PL-011 step 3 (line 317): "Allow in-flight runs to proceed to their next
durable checkpoint per [execution-model.md §4.4 EM-017], then stop
advancing them."

Three cases, none named:

- **Run mid-agent-work, no outcome emitted yet.** The next durable
  checkpoint does not exist until the agent emits an outcome. Does drain
  wait? Step 4 implies yes (drain timeout bounds the wait) but step 3
  never says "wait for outcome."
- **Run just landed a checkpoint, next-node not yet dispatched.** Stop
  advancing = dispatcher suspends. Clear boundary; say so.
- **Run in `gate-pending` sub-state of `running`** (execution-model.md
  line 506). No checkpoint pending; drain should treat this as
  quiescent. Pin.

Suggested PL-011b: "drain stop-advancing predicate is `in_flight(run)
AND no pending cascade`" with a cite to operator-nfr's mechanical
`in_flight(run)` definition.

**Load-bearing because**: drain timeout → SIGKILL (step 4) applies to
agent subprocesses. If PL-011 step 3 does NOT wait for agent completion,
every non-idempotent mid-work run becomes Cat 2/Cat 3a on restart. If it
DOES wait, the drain timeout is load-bearing for run-loss. The ambiguity
propagates across PL / ON / RC.

### 5. PL-014a concurrency ceiling — scope and enforcement unnamed

PL-014a (line 361): configurable ceiling on "simultaneously-running agent
subprocesses," emits `dispatch_deferred`. Three gaps:

- **"Simultaneously-running" scope.** Spawn-to-reap? Includes the HC-008a
  post-outcome 10s shutdown window? Pessimistic if yes; demands careful
  reap-timing if no.
- **Deferred-dispatch retry mechanism.** Event-driven vs poll vs queue —
  unnamed. Testing.md will need to assert "deferred-then-admitted"
  scenarios without a mechanism.
- **Orchestrator-agent inclusion.** PL-019 declares orchestrator-agent
  as a separate OS process spawned by `harmonik runner` (PL-028 step 4).
  Is it counted against the ceiling? Silent. Load-bearing when operator
  sets ceiling to N: orchestrator-agent startup could block handler
  dispatch.

**Load-bearing because**: ON-041's machine-level ceiling is explicitly
cross-daemon (ON line 429); PL-014a is the per-daemon half. Composition
requires scope discipline.

### 6. PL-008a exit codes conflict with operator-nfr §8

Verified: PL-008a (line 250–261) declares:

| Code | PL-008a name | ON §8 code | ON §8 name |
|------|--------------|------------|------------|
| 5 | pidfile-locked | 5 | pidfile-locked | OK |
| 6 | socket-bind-failed | 6 | socket-bind-failed | OK |
| 7 | git-bad-state | 7 | git-bad-state | OK |
| 8 | beads-unavailable | 8 | beads-unavailable | OK |
| 9 | filesystem-unwritable | 9 | filesystem-unwritable | OK |
| 10 | disk-full | 10 | disk-full | OK |
| **11** | **ntm-unavailable** | **11** | **drain-timeout-escalated** | **CONFLICT** |
| **12** | **panic** | **12** | **rto-hard-ceiling-exceeded** | **CONFLICT** |
| — | — | 19 | runtime-panic | ON-owned panic code |

Operator-nfr §8 is declared authoritative for the exit-code taxonomy by
ON-002: "The taxonomy lives in §8 of this spec; cross-references from
other specs [...] MUST resolve to §8 entries." PL-008a's claim of
consistency is false. Three fixes, all one-line:

- Renumber PL-008a's ntm-unavailable to an unused code (22+, since ON
  uses 0–21).
- Renumber PL-008a's panic to 19 (ON's runtime-panic). The R1 row for
  panic even cites PL-018a as the mechanism, so this is the one-line
  fix.
- Or: drop PL-008a's interim catalog entirely and cite ON §8 directly,
  since ON §8 already covers codes 5–10 and 19. PL-008a's "interim"
  framing existed because R1 thought ON-003 was still pending — but ON
  v0.3.0 landed the catalog and the taxonomy table.

PL-021a (line 433) cites "code `11` per PL-008a" — dependent; fixes
with PL-008a.

PL-018a (line 398) cites "exit code `12` per PL-008a" — dependent; fixes
with PL-008a.

**Load-bearing because**: an operator observing exit code 11 must know
which spec assigns it. Two specs assigning different meanings to the same
code is a data-integrity hazard in the operator-facing surface — the one
ON is literally the authoritative owner of.

### 7. PL-020a registry residence — orchestrator-agent access undefined

PL-020a (lines 417–421): all cross-subsystem registries "MUST be
instantiated inside the composition root (`internal/daemon`) on startup"
per AR-INV-007. PL-019 declares the orchestrator-agent is a separate OS
process driving the daemon via the CLI. PL-028's CLI surface does NOT
expose skill/handler/policy/control-point inspection commands.

Unasked: how does the orchestrator-agent know what handlers/skills are
registered? Three readings: (i) orchestrator-agent registry access is
post-MVH — say so; (ii) new inspection CLI commands deferred to OQ-PL-005
— OQ currently silent on inspection; (iii) extend `harmonik status` —
conflicts with ON-002's semantic-content-beyond-enum carve.

**Load-bearing because**: PL-020a's MUST is trivially satisfiable at MVH
(daemon-only). Post-MVH orchestrator-agent lands and either PL-020a
loosens or an inspection surface materializes. Discovering this at
amendment time is expensive.

### 8. fd-lifetime lock + daemon-crash — pidfile-write ordering unnamed

On crash, the kernel releases the lock; the file remains. The new daemon
`flock`s, disambiguates via PL-002a's kill(pid, 0) + /proc/cmdline
probe, and proceeds. Same PL-005 code path for both crash-recovery and
new-start — good.

But: PL-002 ordering of pidfile overwrite is unnamed. Sequence implicitly
needed:
1. `flock` succeeds on daemon-start
2. Read stale pidfile, capture prior PGID (needed for orphan sweep — see
   New Failure Mode 2 below)
3. Write own PID (truncate stale)
4. Proceed with PL-005

Step 2 is absent; an implementation reading "write its PID on startup"
(PL-002) may naturally overwrite before the orphan sweep runs, losing
the prior daemon's PGID. PL-INV-001's sensor ("pidfile PID matches
`getpid()`") is briefly violated between step 1 and step 3 — minor; but
the prior-PGID loss (step 2) breaks PL-006's sweep on darwin. PL-002
should name the ordering explicitly.

**Load-bearing because**: the R1 resolution of Challenge 4 (provenance
marker) depends on identifying orphans by prior-daemon PGID on darwin;
losing that PGID via eager pidfile overwrite reopens the hole.

## R1 regressions

Places R1 integration lost context or over-reached.

1. **The R1 critic flagged the cross-reference corruption (every
   `operator-nfr.md §7.N` / `beads-integration.md §10.N` / `reconciliation
   .md §9.N` was wrong) as the blocking finding (Challenge 1).** PL v0.3.0
   fixes the OUTBOUND cites — the §12 row explicitly names the migration
   table. Good. But the §12 row also says "The corpus-wide batch-2
   migration (58 stale cites across sibling specs) is tracked separately;
   only PL's OUTBOUND cites were migrated in this pass." I verified with
   grep: handler-contract.md, beads-integration.md, event-model.md,
   reconciliation/spec.md, and workspace-model.md STILL contain stale
   cites to `[process-lifecycle.md §8.N]`. Examples:
   - handler-contract.md line 119, 429, 788 cite `[process-lifecycle.md
     §8.1]` and `§8.5` and `§8.6` — all of which do not exist in PL now.
   - beads-integration.md line 70, 178, 433 cite `[process-lifecycle.md
     §8.1]` and `§8.2`.
   - event-model.md line 823, 918 cite `[process-lifecycle.md §8.2]`.
   - reconciliation/spec.md line 343, 503, 692 cite `[process-lifecycle.md
     §8.2]`.
   This is out-of-scope for PL's integration per the §12 disclaimer, so
   not a regression per se — but the §10.3 conformance claim "this
   spec's OUTBOUND citations have been migrated to current anchors in
   v0.3.0" (line 716) could mislead a casual reader into thinking the
   cross-reference corpus is now clean. It is not. The disclaimer is in
   §12 only; §10.3 should echo it.

2. **§12 revision-history row is extremely dense.** This is a
   transparency-positive (reader can tell exactly what landed) but it
   also makes it hard to verify that everything is consistent. The
   exit-code-conflict bug (Hidden Assumption 6) would probably have
   been caught by a reviewer if the PL-008a table were visually separate
   from the narrative. The row is 3000+ characters. Consider a
   one-paragraph summary followed by a bulleted list in future
   revisions.

3. **PL-011 step 4's "send SIGKILL to surviving agent subprocesses"**
   (line 318). In the v0.2 draft this step was described using the
   "suspend" vocabulary that v0.3 replaced with "stop advancing" (per
   §12). But step 4 still kills surviving subprocesses — which means
   step 3's "stop advancing" is incomplete coverage, since the
   subprocess keeps running even as its parent run has been told to
   "stop advancing." The suspend-vs-stop-advancing distinction is a
   run-level concept; the subprocess-kill is a separate concern.
   PL-011 steps 3 and 4 are adjacent but operate on different scopes
   (run vs subprocess) without naming the relationship. Minor but
   regression-adjacent.

4. **`dispatch_deferred` event's `reason` field** (line 363): "`reason =
   "machine_ceiling_exhausted"` or an equivalent reason derived from
   the ceiling policy." But ON-041 line 429 also declares machine-level
   ceiling separately and ON §8 code 18 is `machine-ceiling-exhausted`
   as the EXIT CODE. The reason-string on PL-014a's dispatch_deferred
   carries the SAME NAME as ON §8's code-18 category for a cross-daemon
   condition — but PL-014a is the PER-DAEMON event. Either the reason
   string should disambiguate (`per_daemon_ceiling_exhausted` vs
   `machine_ceiling_exhausted`) or both specs need to name the scope
   discipline together.

## Over-specification vs under-specification

**Over-specified:**

- PL-006 tmux-kill wait "100 ms cadence up to a 2-second ceiling" (line
  211). Poll cadence is an implementation detail; the ceiling is an
  operator knob per OQ-PL-002. Normative text could say "bounded by
  2-second ceiling, implementation polls at an implementation-defined
  cadence" and save a line of detail. Minor.

- PL-006a "typically `setpgid`-on-spawn with a per-daemon-instance group-
  leader PID recorded in the pidfile" (line 227). "Typically" inside a
  MUST-block is self-contradictory; either pin the mechanism or use
  SHOULD. See MUST/SHOULD discipline below.

- PL-027(iii) "`T_rebind` (default 2s)" — the default comes from
  OQ-PL-002. Could reference OQ-PL-002 without restating. Minor.

**Under-specified:**

- PL-011 step 3 — see Hidden Assumption 4.
- PL-014a dispatch_deferred retry mechanism — see Hidden Assumption 5.
- PL-009a emission ordering vs reclassification — see Hidden Assumption 3.
- PL-020a orchestrator-agent registry access — see Hidden Assumption 7.
- PL-028 `harmonik runner` step 4: "On Claude Code unavailable, exit
  with a specific error code `orchestrator-agent-unavailable` per
  [operator-nfr.md §8]" (line 504). But ON §8 does NOT declare
  `orchestrator-agent-unavailable`. It's not in codes 0–21. Either add
  the code in ON (cross-spec) or declare it inline in PL-008a with a
  placeholder number.
- PL-027(ii) "detectable by passing a specific environment marker set by
  the outgoing binary" — the marker's name is unnamed. See Hidden
  Assumption 8.

## Cross-spec promises — OQ realism, which should be blocking

- **OQ-PL-004 lease-lock-path alignment across HC-044a, WM-013a, PL-006.**
  Workspace-model.md §4.3 line 278 names this NOTE and declares WM's
  filename as authoritative for WM's writer side. PL-006 bullet
  (line 212) cites WM-013a and WM-033 for staleness but uses
  `.harmonik/lease.lock` in its own text. The spec cannot finalize with
  three differently-named files referring to the same lock. **SHOULD
  BLOCK: the bootstrap-task list will fail when the implementer hits
  three paths for one lock.** The default-if-unresolved ("PL-006 matches
  whichever filename was written by WM-013a on the same daemon
  generation") is a runtime-resolvable workaround; the problem is
  authoring-time mechanical-testability.

- **OQ-PL-005 agent-command JSON-RPC method inventory.** Defers to
  "PL r2 or HC r2 per whichever completes first." This is PL r2. HC is
  still on v0.2.0 draft; no HC r2 yet. PL r2 should own this; deferring
  further is ceremony. The inventory doesn't need exhaustive payload
  schemas — just method names for claim-next, emit-outcome, enqueue,
  status, pause, stop, upgrade, attach, list (for ON-041 cross-daemon),
  and the Beads-CLI-proxy methods of BI-027.

- **OQ-PL-006 stale-intent classification coordination with RC.** Legit
  cross-spec — defer to RC r2.

- **OQ-PL-007 PID-reuse-on-reboot.** Legit. Default-if-unresolved is
  reasonable (treat as stale, remove, warn). Not blocking.

- **OQ-PL-008 darwin provenance marker.** See Hidden Assumption 2. The
  default-if-unresolved ("PGID-only on darwin, no filesystem fallback")
  is weaker than it appears. SHOULD BLOCK or the default should be
  strengthened.

- **OQ-PL-009 post-ready degradation scope.** Reasonable defer; default
  "option b" is consistent with what the spec narrowed PL-010 to. Not
  blocking.

- **OQ-PL-002 timeouts.** Defaults named in-text. Not blocking.

- **OQ-PL-003 `.harmonik/` auto-creation.** Default "require `harmonik
  init`" is reasonable. Not blocking.

- **OQ-PL-001 testing.md migration.** Legit defer; blocks nothing.

Net: OQ-PL-004 and OQ-PL-005 should block `reviewed`. OQ-PL-008's default
should be strengthened (PGID + process-age corroboration on darwin).

## Definitional drift

- **daemon** (glossary line 63, §4.1 PL-001, §4.6 PL-018, §4.a): "the
  per-project headless Go process that owns workflow state, dispatches
  runs, and exposes the local socket." Consistent. Good.

- **orchestrator-agent** (glossary line 64, §4.6 PL-019, §7.1 NOTE, §2.1
  "Daemon-vs-orchestrator-agent distinction"): "a separate Claude Code
  session (cognition-bearing) that drives the daemon via its CLI.
  Post-MVH; NOT a component of the daemon." Consistent. The v0.3.0
  cleanup "drops '(or coordinator-agent)' parenthetical" is correct.

- **runner** (§4.10 PL-028 `harmonik runner`): "solo-dev convenience
  command." Appears only as a CLI entry-point; glossary doesn't declare
  `runner` as a separate abstract term. Consistent with CLI-command
  convention.

- **coordinator-agent** does NOT appear in PL v0.3.0. Consistent with the
  §12 row's claim. Good.

- **ready** (§4.3 PL-009, §6.1 ENUM, §7.1): defined positively as "§PL-
  009 criteria met; normal dispatch active." Consistent.

- **degraded** (§6.1 ENUM, §4.3 PL-010, §9.3 NOTE): defined as "Cat 0
  prerequisite failing; classification halted (pre-ready only; see
  §PL-010)." The v0.3.0 narrowing is explicit. PL-010 (line 302)
  correctly distinguishes "the `degraded` state declared by this spec
  is the PRE-`ready` Cat 0 side-state only." OQ-PL-009 names the
  unresolved post-ready question. NO drift; however, `daemon_degraded`
  event-model §8.7.5 emits `reason ∈ {rto_breach, reconstruction_notify,
  other}` — the `clock_regression` reason that event-model.md line 230
  emits on HWM wall-clock-regression is NOT in §8.7.5's enum. This is an
  event-model internal inconsistency, not PL's problem directly, but PL
  cites §8.7.5's enum in PL-010 context. Worth flagging.

- **draining** (§4.4 PL-011, §6.1 ENUM, §7.1): "graceful-shutdown
  sequence active (§PL-011)." Consistent. §7.1 table shows
  ready/reconciling/degraded → draining on SIGTERM. `draining` → `stopped`
  on drain complete. Consistent.

- **starting, reconciling, paused, stopped**: all consistent with §6.1
  ENUM. `paused` and `stopped` are explicitly "owned by operator-nfr per
  §4.3" — appropriate cross-spec cite.

**Net: no drift of concern. The cleanup of `coordinator-agent` and the
narrowing of `degraded` are cleanly documented.**

## Template conformance

Front matter (lines 3–25): `spec-category: runtime-subsystem` ✓,
`spec-template-version: 1.1` ✓, `depends-on` list ✓ (no cycle with ON),
`status: draft` ✓, `version: 0.3.0` ✓, `requirement-prefix: PL` ✓.

§4.a Subsystem envelope (lines 75–139): eight elements (a)–(h) per
AR-053. PL-ENV-001 requirement block ✓. Types table ✓ with Tags and Axes.
Events produced (a) and consumed (b) itemized. Handlers implemented (d):
"none" explicit per the template's "write 'none' explicitly" rule. State
owned (e) itemized. Control points provided (f): "none" explicit. NFRs
inherited/overridden (g) itemized. Boundary classification (h) per
operation ✓. Fully conformant.

Requirement blocks: every block has Tags; axes present where the block
mutates state or performs I/O. Sampled 20 blocks; all conform.

§5 Invariants: five declared (PL-INV-001 through PL-INV-005). Each
names a sensor. Selection test:
- PL-INV-001 ✓ (spans ON, BI, WM per §10.1 text).
- PL-INV-002 ✓ (spans every subsystem's Go-package declaration).
- PL-INV-003 ✓ (spans reconciliation).
- PL-INV-004 borderline — "socket-path exclusivity" pairs with PL-INV-001
  per the invariant body, but does it span another subsystem's §4? The
  sensor is "the daemon that holds the pidfile lock (PL-002) is the
  exclusive owner of the bound socket fd." That's an internal property,
  not a cross-subsystem constraint. Could be a §4.1 requirement — it's
  an invariant of how PL-003 relates to PL-002, which both live in PL's
  §4.1. R1 critic recommended this invariant; the v0.3.0 response
  correctly added it, but §5 selection-test discipline suggests it could
  live as a requirement PL-003c. Borderline; acceptable.
- PL-INV-005 ✓ (spans handler-contract via HC-044; the orphan sweep's
  correctness across projects depends on it).

Four of five invariants pass; PL-INV-004 is the soft one but defensible.

§6 Schemas: DaemonStatus ENUM (§6.1) ✓. Co-owned event payloads (§6.2)
lists the emission timing and cedes shape to event-model. Schema
evolution (§6.3) notes no persistent on-disk schema. Clean.

§7 Protocols: §7.1 state-machine table ✓. Transitions named with
from/event/guard/to/emits. The `ready/reconciling/degraded → draining`
row emits `daemon_shutdown{mode=graceful}` — matches §PL-011a. R1 critic
had flagged a wrong emit (`operator_pausing`); v0.3.0 fixed it. ✓.

§8 Error taxonomy: correctly declares "this spec does not own a failure
taxonomy" and delegates to ON §4.1/§8 and RC §8. Clean.

§9 Cross-references: §9.1 depends-on ✓; §9.2 reverse-dependencies
INFORMATIVE ✓; §9.3 co-references with the operator-nfr asymmetry NOTE
(line 680) ✓.

§10 Conformance: §10.1 profiles ✓; §10.2 test-surface obligations in
prose with OQ-PL-001 defer ✓; §10.3 excluded conformance claims ✓.

§11 Open questions: nine OQs, each with Question / Owner / Blocks /
Default-if-unresolved. ✓.

§12 Revision history: dense but transparent. ✓.

**Template conformance: clean. No §-numbering bugs, no missing Tags
lines on new requirements.**

## New failure modes surfaced by v0.3.0

Requirements added in v0.3.0 surface or enable new failure shapes that
prior reviews couldn't have flagged:

1. **Provenance marker preservation across exec-upgrade.** PL-027(i)
   preserves daemon PID → process groups survive execve → PGID
   group-leader PID is stable → provenance markers hold. But PL-006a
   never names "exec-upgrade preserves the marker" as a guarantee.
   One-sentence fix.

2. **Stale-pidfile PGID loss on daemon crash + restart.** Handler
   subprocesses from the dead daemon have the OLD PGID; sweep must
   match that PGID, but the pidfile gets overwritten on step 3 of
   PL-005 before step 4's sweep runs. Spec doesn't require the daemon
   to read + retain the old PGID before overwriting. If implementer
   overwrites eagerly, the cross-generation orphan identification on
   darwin is lost (HARMONIK_PROJECT_HASH env var is unreadable from
   foreign processes on darwin per OQ-PL-008). The R1 resolution of
   Challenge 4 resolved cross-project collisions but reopens cross-
   generation identification on darwin. Fix: PL-006a should say the
   sweep MUST retain the prior-daemon PGID (read from stale pidfile
   before overwrite) and must match by project_hash env var where
   readable; PGID is spawn-site-correctness, not cross-generation-
   identification.

3. **Upgrade exec kills in-flight handlers via HC-024a escalation.**
   PL-027(iii) allows T_rebind up to 2s for socket rebind. HC-024a
   (handler-contract line 283) allows handlers ONE 500ms reconnect
   window on socket-IO-error; beyond that, Structural termination +
   SIGKILL. T_rebind > 500ms means every in-flight handler exceeds
   HC-024a's window and is killed by the watcher on the NEW daemon
   reconnect. PL-027(iii) says "clients MAY observe socket EOF; they
   MUST retry" — but doesn't constrain T_rebind below HC-024a's
   window. Either T_rebind ≤ 500ms or HC-024a needs an upgrade-window
   carve-out. Cross-spec issue surfaced by R1's PL-027 split.

4. **Step-0 registry-bootstrap panic.** PL-005 step 0 bootstraps the
   event bus + registries. If step 0 panics (a subsystem's init
   fails), the event bus is half-initialized. PL-018a's panic barrier
   catches; PL-008a code 12 (per PL's table, which conflicts with ON's
   19 — see HA6). `daemon_startup_failed` emission requires the event
   bus, which is half-dead. Naming: PL-005 step 0 should declare "on
   step-0 failure (registry bootstrap panic), daemon exits with the
   runtime-panic code; event-bus emission is best-effort; stderr
   carries the panic."

## Affirmations

Decisions that survive R1+R2 pressure and should:

1. **PL-002a fd-lifetime advisory lock primitive.** The flock / F_OFD_SETLK
   selection, the POSIX F_SETLK forbid, and the kill(pid, 0) + `/proc/
   <pid>/cmdline` disambiguation triple is the sharpest part of the
   v0.3.0 draft. Cross-platform-correct, mechanically testable, and
   operator-honest about the edge case (OQ-PL-007).

2. **PL-006a multi-project provenance marker.** Closes R1's blocking
   Challenge 4. The env-var-plus-PGID dual-mechanism is a legitimate
   belt-and-suspenders approach on Linux; the darwin gap is honestly
   deferred to OQ-PL-008.

3. **PL-009a auto-resolver failure routing.** Closes R1's blocking
   Challenge 2 cleanly. The Cat 3 fallback + investigator dispatch is
   well-shaped; the only remaining ambiguity is emission-ordering
   (Hidden Assumption 3), which is a post-R2 nicety.

4. **PL-027 split into daemon-internal and operator-facing halves.**
   Closes R1's Challenge 6. PL owns (i)–(v) mechanical bits; ON owns
   binary-source / hash-check / schema-compat. Correct co-ownership
   shape.

5. **§4.a Subsystem envelope.** Exemplary execution of AR-053. Every
   element populated, "none" declared explicitly where empty, types
   tagged. Post-R1 corpus standard for other specs' envelope sections.

6. **PL-INV-005 (agent-subprocess parentage).** Names the cross-
   subsystem property R1 critic said was missing. Sensor is concrete
   (marker-based, not binary-path-based). Paired with HC-044's
   pdeathsig story.

7. **PL-028 runner as distinct entry-point.** Closes R1's Challenge 7.
   Four ordered steps with exit codes. "NOT a shell alias" is
   explicit. The `--orchestrator-agent` flag surface is declared.

8. **§9.3 co-references NOTE explaining the PL ↔ ON cycle break.** The
   front-matter cycle-break via one-direction depends-on + other-
   direction named-obligation is a pattern that other specs should
   adopt and cite this spec for.

## Recommendation

**Return to draft for R2-cleanup (not a full R3 cycle).** The blocking
items are:

- **PL-008a / ON-§8 exit-code conflict (Hidden Assumption 6).** One-line
  renumber. Must happen before `reviewed`.
- **PL-011 step 3 stop-advancing mechanical definition (Hidden Assumption
  4).** Add a predicate citation (one-to-three sentences). Must happen
  before `reviewed` or the drain-timeout-escalation path (step 4) is
  under-defined.
- **OQ-PL-004 lease-lock path alignment.** Either the three specs align
  (cross-spec edit) or PL-006 changes to match WM-013a's canonical
  path and HC-044a's path remains independent-and-documented. Not
  necessarily a PL-only edit, but blocks `reviewed` for the corpus.

Important items (acceptable to defer with named OQ):

- **OQ-PL-005 JSON-RPC method inventory.** Enumerate at PL r2.
- **OQ-PL-008 darwin provenance marker default strengthening.** Add
  process-age corroboration as minimum.
- **PL-006a `abspath` → `realpath` to canonicalize symlinks.** One-line
  edit.
- **Orphan-sweep sequencing (new failure mode 2).** Name "read stale
  pidfile PGID before overwriting" as an obligation.
- **PL-027 socket-rebind T_rebind vs HC-024a reconnect window.**
  Cross-spec — defer to an OQ if not resolvable in PL r2.

Minor items:

- PL-006 "typically `setpgid`" — MUST-block uses "typically"; promote to
  MUST or demote to SHOULD.
- PL-020a orchestrator-agent registry access — one sentence declaring
  "post-MVH" or add OQ.
- PL-028 orchestrator-agent-unavailable exit code — declare in PL-008a
  or cross-spec add to ON §8.
- §10.3 echo the §12 disclaimer that sibling-side stale citations
  remain.
- §12 row length — future revisions should bulletize.

R2 budget estimate: ~40–80 lines of normative-text edits + 1 cross-
spec-align pass on lease-lock path (OQ-PL-004).
