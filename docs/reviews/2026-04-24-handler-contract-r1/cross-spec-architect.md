# Round 1 Cross-Spec Architect Review — handler-contract.md v0.1.0

## Verdict summary

The spec sits well in the foundation graph. Its scope boundaries against
event-model (WHEN vs. WHAT), workspace-model (three-stage session-log
pipeline), control-points (skill declaration surface vs. injection mechanism),
execution-model (Outcome type, failure classes), and process-lifecycle
(subprocess parentage) are mostly clean and consistent with how those specs
describe the same boundaries from their side. The concurrency-split language
(§4.3) is the cleanest boundary declaration in the spec.

Material issues are concentrated in four areas: (1) HC-010 claims S04 is the
"authoritative emitter" of `session_log_location` but the ordering picture
across HC + workspace-model + event-model is inconsistent on who the emitter
is and what fires before what; (2) `Outcome` is declared (not just
referenced) in §6.1 in a way that over-crosses into execution-model's type
territory; (3) §8's per-class failure tables duplicate execution-model §8 at
a granularity that will drift; (4) HC-032 talks about handlers declaring
redaction patterns "in their subsystem envelope" which exceeds what HC can
legitimately obligate — envelope declarations are architecture-level, not
HC-level. Twin-parity and the watcher/adapter split are the strongest parts
of the spec and need no changes.

## Scope overlaps

### 1. Event-model: HC correctly cedes payloads, correctly owns WHEN

HC §2.2 "Event payload schemas ... owned by [event-model.md §3.2]. This spec
declares WHEN each event fires." §6.4 enumerates the co-owned events with
"normative for WHEN ... event-model is normative for the on-the-wire payload"
explicitly restated. This is the right split, and event-model §8.3 agrees:
events emitted by "handler (via daemon watcher)" with payload field lists,
with event-model §9.3 explicitly marking "`skills_provisioned` emission rule
lives there; this spec owns the payload shape."

One inconsistency though: event-model §8.3.8 has `session_log_location`
emitter = "agent-runner (S04)", not "handler (via daemon watcher)" (the
pattern every other §8.3 row uses). HC §4.2.HC-010 agrees with this ("S04 is
the authoritative emitter") and §6.4 restates it. But HC §4.2.HC-007 lists
`session_log_location` in the set of events "the handler subprocess MUST
emit ... on a named stream" consumed by the daemon watcher. Three
possibilities are tangled here:

- The handler subprocess emits a typed progress-stream event `session_log_location`;
  the watcher publishes it to the bus (same pattern as `agent_ready`).
- S04's adapter constructs the event from handler-level signals and publishes
  it; the handler subprocess does not emit it directly.
- The workspace-manager-written sidecar is the source, and S04 emits a
  synthesizing event.

HC §4.2.HC-010 reads as the first, but event-model §8.3.8's "agent-runner
(S04)" label reads as the second, and §7.2's `launch_handshake` treats it as
an event awaited on the progress stream (again the first). Pick one,
consistently, and push the choice through all three specs. The "authoritative
emitter" phrasing in HC-010 needs sharpening: if the handler subprocess emits
it on the stream and S04 is simply the daemon-side consumer/forwarder, then
"authoritative emitter" is misleading.

### 2. Workspace-model: three-stage pipeline ownership is almost clean

The pipeline as declared is: S06 (workspace-manager) creates
`.harmonik/sessions/<session_id>/` and writes `harmonik.meta.json` before
`workspace_leased` fires (WM-016, WM-025, WM-026, WM-027). S04 (handler/agent-
runner) writes the session log and emits `session_log_location`. S08 (memory
layer, CASS) reads both.

HC correctly scopes this to out-of-scope ("workspace path construction,
session-log directory creation, post-merge session-log archival — owned by
[workspace-model.md §5.3a]") — good. But HC's §4.2.HC-010 says
`session_log_location` fires "early in the session (after `handler_capabilities`
and before `agent_ready`)" without acknowledging that the directory and
sidecar already exist from S06 by the time the handler runs. The ordering
picture across the three specs is:

1. S06 creates worktree, branch, sessions dir, sidecar → emits `workspace_leased`
2. Daemon launches handler subprocess
3. Handler subprocess emits `handler_capabilities` → `session_log_location` →
   `skills_provisioned` → `agent_ready`

HC §5 HC-INV-004 pins the sub-sequence 3 but is silent on sub-sequence 1-2.
workspace-model §4.4.WM-016 pins 1-2. The gap isn't a contradiction, but
adding a sentence to HC's §4.2.HC-010 ("by the time this event fires, the
session-log directory per workspace-model §4.7 already exists; this event
announces the path, it does not create the directory") would close the loop.

### 3. Control-points: declaration-surface / injection-mechanism split is clean

CP §4.11 declares the surface (nodes MAY declare `required_skills`, roles
declare `default_skills`, effective set = union, Beads-CLI is default), and
defers provisioning to HC §4.11. HC §4.11 declares the mechanism (resolve,
provision, emit, fail-launch on unresolved). HC §4.11.HC-050 explicitly
forbids the handler from reading DOT/YAML directly — it consumes only
`LaunchSpec.required_skills[]` / `LaunchSpec.skill_search_paths[]`. This is
precisely the right boundary and both specs describe it the same way.

One small over-claim: HC §4.11.HC-046 says the handler MUST provision skills
"in the agent-type-specific shape (file drops, CLI binaries on PATH, MCP
registrations, reference-doc bundles)." The per-shape list is descriptive but
HC can't enumerate every future shape without a spec amendment. Consider
either (a) moving the enumeration to informative rationale, or (b) making it
"one or more of the following shapes as the handler chooses." Current reading
is normative-exhaustive which it can't actually be.

### 4. Execution-model: Outcome type is over-declared

Execution-model §4.1.EM-005 owns `Outcome` ("The outcome shape is the
handler's obligation per [handler-contract.md §4.1]; this spec owns the
type"). HC §2.2 correctly lists payload schemas as out-of-scope but HC's §6.1
contains:

```
RECORD Outcome:
    -- defined in [execution-model.md §6.1 Outcome]; carried over the wire
       as the payload of outcome_emitted
```

This is fine as a pointer. But HC §4.2.HC-008 says "The handler subprocess
MUST deliver the run's `Outcome` (per [execution-model.md §4.1]) as the
payload of a final `outcome_emitted` event." Better. The §6.1 stub should
match §4.2.HC-008's explicit reference-not-re-declaration pattern.

Also check: event-model §8.1.8 has `outcome_emitted` emitter = "orchestrator-
core" with payload fields `run_id, node_id, outcome_status, preferred_label?,
suggested_next_ids?`. HC §4.2.HC-008 has the handler subprocess emitting
`outcome_emitted` on the progress stream. §6.4 lists `outcome_emitted` as
co-owned with event-model. Who emits `outcome_emitted` to the event bus?

Most plausible reading: the handler subprocess emits a progress-stream
`outcome_emitted` carrying the `Outcome` payload; the watcher forwards to the
bus as the event `outcome_emitted` carrying the projection fields
(`outcome_status` etc.). Same pattern as agent-lifecycle events. But
event-model labels the emitter "orchestrator-core" which suggests the
orchestrator — not the handler subprocess — constructs the bus event. This
is the same ambiguity as §1 above, and resolving it for `session_log_location`
should resolve it for `outcome_emitted` too.

### 5. Process-lifecycle: subprocess ownership is clean

PL §4.5 has subprocess-as-child-of-daemon (PL-014), subprocess↔daemon via
socket (PL-015), failure observed by watcher (PL-016), silent-hang named but
owned by HC (PL-017). HC §4.10.HC-044 restates the child-of-daemon rule with
references to `[process-lifecycle.md §8.5]` (child) and `§8.1` (socket).
Mutual-consistency is strong; this is a good example of how to cross-cite.

## Type ownership

### Outcome — declared in execution-model, referenced here

Correct direction; the §6.1 stub should tighten to "see [execution-model.md
§6.1 Outcome]" with no field list at all (as done for `SessionID`).

### LaunchSpec — declared here, referenced nowhere else

Correct; this is HC's territory.

### Handler / Session / Adapter interfaces — declared here, referenced by execution-model EM-007

Correct. Execution-model just says "handler registered per [handler-contract.md
§4.1]" which is the right reference shape.

### Error sentinels (ErrTransient etc.) — declared here, routing rules here AND in execution-model §8

Overlap. HC §8.1-§8.7 and execution-model §8 both carry the same five-/six-
class taxonomy with the same wording ("detection rule", "default response",
"escalation", "emitted event"). These will drift.

Recommendation: HC §8 owns the sentinel declarations (the `ErrX` Go values)
and the per-handler detection rules for when a handler/adapter emits which
class. Execution-model §8 owns the daemon-level routing rules (what the
daemon does with `ErrX` at the run-level — retry policy, reclassification to
`compilation_loop`, terminal `run_failed` emission). Today both specs include
both. Split them: HC §8 trims to the mechanical detection rule + the sentinel
Go value, and cites execution-model §8 for routing. Execution-model §8 cites
HC §8 for the sentinel values. This is what HC §4.5.HC-020 already implies
("consumers MUST use `errors.Is` or `errors.As`") but §8's body doesn't hold
the line — §8.1 has "retry unchanged, with bounded attempts" which is
execution-model's call, not HC's.

### Event-payload schemas — declared in event-model, referenced here

Correct, modulo the emitter-identity ambiguity flagged in §1 above.

## Cross-reference issues

### HC-032 "subsystem envelope" for redaction patterns — possible over-reach

HC-032: "Each handler spec MAY contribute additional value-shaped regex
patterns ... Patterns MUST be declared in the handler's subsystem envelope as
redaction entries and registered at daemon init." The "subsystem envelope"
concept belongs to architecture.md §1.4. HC claims the right to extend what
the envelope declares (a new "redaction entries" category). That's an
architecture-level addition.

Two fixes: (a) ensure architecture.md §1.4 enumerates "redaction-pattern
entries" as an envelope category HC can populate, or (b) move the declaration
surface to HC's own spec (a `redaction_patterns` field on per-handler specs
that HC-032 declares is required). Either is fine; leaving HC as it is
implies the envelope already supports this, and §9.1's cite of "[architecture.md
§1.4] — subsystem envelope; handlers declare redaction patterns (§4.7.HC-032)
and silent-hang thresholds (§7.1) in their subsystem envelope" suggests HC
believes (a) is true. Confirm against architecture.md §1.4 to be sure.

### §9.1 cites "[reconciliation.md §9.4b]" for snapshot-token semantics

HC-006 and the LaunchSpec record both carry `snapshot_token` with a cite to
`[reconciliation.md §9.4b]`. reconciliation.md exists as a directory
(`specs/reconciliation/`) but not yet as a single-file spec. If the snapshot-
token semantics haven't landed there, HC should use the bootstrap-citation
pattern per spec-template §0 ("[docs/foundation/reconciliation.md §9.4b]
(bootstrap; migrates to reconciliation.md §9.4b when finalized)") until the
reconciliation spec is finalized.

### §7.1 silent-hang state machine emits events not in §6.4

§7.1 emits `agent_warning_silent_hang`, `agent_resumed_after_warning`,
`agent_soft_terminating`, `agent_hard_terminating`. None are in HC §6.4's
co-owned events list and none are in event-model §8.3. Either these are
silent-hang-specific events that need adding to event-model's §8.3 with
payload schemas, or they are internal-only signals and §7.1 should say so.
Current reading implies bus emission without schema declarations.

## Recommendations

1. Resolve the emitter-identity question for `session_log_location` and
   `outcome_emitted` consistently across HC §4.2 / §6.4, event-model §8.1-§8.3,
   and workspace-model §5.3a. Pick one of: (a) handler subprocess emits
   progress-stream event, watcher publishes to bus; (b) S04/orchestrator-core
   constructs bus event from handler-side signals. The three specs disagree
   today; it's latent drift.

2. Tighten HC §6.1 Outcome stub to no field list — just a pointer to
   execution-model §6.1. Match the SessionID pattern.

3. Split HC §8 and execution-model §8 on detection-vs-routing. HC owns the
   `ErrX` values and detection triggers; execution-model owns what the daemon
   does with each class at the run level. Trim per-class "default response"
   prose in HC §8 to detection only.

4. Add the ordering cross-reference to HC §4.2.HC-010 acknowledging that the
   session-log directory/sidecar already exist per workspace-model §4.7 by
   the time `session_log_location` fires.

5. Add the silent-hang events of §7.1 to event-model §8.3 with payload schemas,
   or mark them as internal signals that do not reach the event bus.

6. Confirm architecture.md §1.4 envelope schema includes a
   `redaction_patterns` slot; if not, either extend architecture or move the
   declaration surface to HC directly.

7. Soften the per-shape enumeration in HC §4.11.HC-046 (file drops, CLI, MCP,
   ref-doc) from exhaustive-normative to open-set or move to rationale.

8. Use bootstrap-citation pattern for `[reconciliation.md §9.4b]` cites until
   that spec is finalized.

Twin-parity (§4.8, HC-INV-002) and the watcher/adapter concurrency split
(§4.3, HC-INV-001) are the spec's strongest boundary declarations and should
not be touched; cross-spec consumers (process-lifecycle PL-016, architecture
§1.8) cite them correctly.
