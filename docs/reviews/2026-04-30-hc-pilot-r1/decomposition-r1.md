# HC Pilot v0.1.0 — Decomposition-Quality Review (r1)

`reviewer: decomposition-quality` · `date: 2026-04-30` · `pilot: hc-pilot.md v0.1.0` · `spec: handler-contract.md v0.3.3` · `discipline: discipline.md v0.8`

Sample drawn per §3.2: every coalesce (HC-007 cluster, HC-046 cluster), every multi-step candidate (§7.1, §7.2 — verifying NOT split), every sensor (HC-INV-001..007), the §8 single-bead taxonomy, plus first-class beads HC-008a, HC-011a, HC-024, HC-026a, HC-026b, HC-049a. Special-focus checks per the prompt's instruction on §8 `hc-error.taxonomy` direction landed as a BLOCKER (see F-dq-HC-1).

---

## Special-focus blocker — §8 taxonomy edge direction (5 of 6 cycle rejections)

### F-dq-HC-1 — `hc-error.taxonomy` edges run the WRONG direction (BLOCKER, lane: `local`)

`hc-pilot-data.yaml` emits 9 intra-spec edges of the form `{from: hc-error.taxonomy, to: hc-007 | hc-008 | hc-008a | hc-009 | hc-024 | hc-024a | hc-026 | hc-044a | hc-048 | hc-048a}`. This is the cause of cycle rejections F-load-HC-4..F-load-HC-7 (and arguably F-load-HC-2 transitively): each of those §4 reqs term-uses a sentinel name (`ErrStructural`, `ErrTransient`, `ErrSkillProvisioningFailed`) whose definition is owned by `hc-error.taxonomy`, so the §3.1 step 5 term-use rule emits `<req> → hc-error.taxonomy` (req blocks on taxonomy); the loader's reverse `taxonomy → req` edge then closes the cycle.

The pilot author's §5.7 #27 narrative correctly identifies the problem and labels it F-pilot-HC-4, but invokes the F13 slot-rule heuristic INVERTED — concluding "taxonomy IS the slot, §4 reqs are content; emit taxonomy → reqs" and patching by *dropping* taxonomy from the §4 row predecessor lists rather than dropping reqs from the taxonomy's predecessor list. Per discipline §2.6 the §8/§6 sentinel set is a SCHEMA-shaped construct (the `kind:taxonomy` bead defines a vocabulary that downstream consumers cite by name); per the §2.6 worked example "`bi-025a` blocks-on `bi-schema.br-error`" and the §2.7 F4 collapse for type/schema cites, every consumer of a sentinel `blocks-on` the bead that owns it. The taxonomy bead is NOT a slot in the F13 sense (a slot rule is a structural carve-out that points at the content that fills it — e.g., AR-053's envelope slot); it is the OWNER of a vocabulary the §4 reqs consume. The correct direction is identical to BI's `bi-error.taxonomy` precedent: §4 reqs that cite a sentinel block-on the taxonomy. Fix: invert all 9 intra-spec edges from `{from: hc-error.taxonomy, to: <req>}` to `{from: <req>, to: hc-error.taxonomy}`. Concretely: HC-008 (cites "agent failure per §4.6"), HC-008a (cites `ErrStructural`), HC-009 (cites `ErrProtocolMismatch`), HC-021 (sub-sentinel rule — cites `ErrStructural`), HC-022 (sub-sentinel rule — cites `ErrStructural`), HC-024 (cites mapped sentinel class), HC-024a (cites `ErrTransient`/`ErrStructural`), HC-026 (cites `ErrStructural`), HC-043 (cites `ErrStructural`), HC-044a (cites `ErrStructural`), HC-048 (cites `ErrSkillProvisioningFailed` / `ErrStructural`), HC-048a (cites `ErrTransient`/`ErrSkillProvisioningFailed`/`ErrStructural`), HC-018 (cites `ErrCanceled`), HC-013/013a (cites `ErrDeterministic`/`ErrTransient`), HC-011a (cites `ErrStructural`), HC-016a (cites `ErrTransient`), HC-004 (cites `ErrTransient`) all block-on `hc-error.taxonomy`. Lane is `local` because the discipline's §2.6 schema-cite rule and the BI precedent already cover the correct direction; this is a misapplication of F13 (treating an owned-vocabulary bead as a slot rule). However, the pattern has surfaced 3x now per the pilot's own F-pilot-HC-4 commentary (EM #15, EV #15, HC #27), suggesting the discipline doc could add an explicit anti-example: "`<spec>-error.taxonomy` is NOT a slot — it is a schema-like vocabulary owner; consumers block-on it per §2.6/§3.1 step 4."

### F-dq-HC-2 — `hc-007 ↔ hc-044` bidirectional cycle (BLOCKER, lane: `local`)

Cycle rejection F-load-HC-3 in the load findings. The pilot's `hc-007` row predecessors include `hc-044` (the §4.10 socket path); `hc-044`'s row predecessors do not include `hc-007` directly — but the YAML emits `{from: hc-007, to: hc-044}` AND `{from: hc-044, to: hc-007}` (the latter implicit via the §6.1 socket-bind cite in HC-044 plus the §4.2 wire-protocol message-stream cite). Per discipline §2.7 F13 + slot-rule heuristic: HC-044 declares the daemon socket file (`.harmonik/daemon.sock` mode `0600`, the parent-child relationship, `PR_SET_PDEATHSIG`); HC-007 (post-coalesce) declares the wire-protocol contract that runs over that socket. HC-044 is the slot (the socket-as-IPC-channel rule); HC-007 is the content (the message-stream contract that uses the socket). The dep is content blocks-on slot: `hc-007 → hc-044` is the keep edge; `hc-044 → hc-007` is supporting (HC-044's main claim "subprocess is direct child of daemon, communicates over socket" is independently testable from HC-007's progress-stream specifics). Fix: drop `{from: hc-044, to: hc-007}` from `hc-044`'s predecessor list (currently lists `hc-007` per §2 row; drop). Lane `local`: standard F13 application; the pilot's §5.7 walk did not enumerate this pair.

### F-dq-HC-3 — `hc-026a ↔ hc-008a` bidirectional cycle (BLOCKER, lane: `local`)

Cycle rejection F-load-HC-2. HC-026a's body explicitly says "During the post-outcome shutdown window (§4.2.HC-008a), heartbeat emission is not required (silent-hang is suspended in that regime)" — this is term-use of HC-008a's shutdown-window concept; edge `hc-026a → hc-008a` fires per §3.1 step 5. HC-008a's body says "The daemon MUST NOT apply silent-hang detection (§4.6.HC-026, §7.1) during the shutdown window" — term-use of HC-026's silent-hang FSM concept; edge `hc-008a → hc-026` fires. HC-026's body says "...for as long as the subprocess is alive and has not emitted `outcome_emitted` ... INCLUDING heartbeats per §4.6.HC-026a" — edge `hc-026 → hc-026a` fires. Result: 3-cycle hc-026a → hc-008a → hc-026 → hc-026a. Per F-pilot-AR-10 supporting-cite test: HC-026a's "shutdown window suppresses heartbeat" sub-clause is supplemental — HC-026a's main claim "MUST emit `agent_heartbeat` at ≤T/2" is independently testable without knowing the suspension carve-out. Reclassify `hc-026a → hc-008a` as supporting; drop the edge. Alternatively the supporting cite is `hc-008a → hc-026` (HC-008a's "daemon MUST NOT apply silent-hang detection during the shutdown window" is independently testable from HC-008a's main `T_shutdown` + dirty-exit claims; HC-026 reference is supplemental). Either way one edge in the chain must be reclassified. Fix: drop `hc-026a → hc-008a` (or alternatively `hc-008a → hc-026`) per author judgment; recommend dropping `hc-026a → hc-008a` (the suspension is a one-line carve-out tacked onto the heartbeat rule). Lane `local`: standard F-pilot-AR-10 application; the pilot's §5.7 walk listed #15 (hc-026↔hc-026a) but missed the transitive 3-cycle through hc-008a.

---

## Coalesces

### F-dq-HC-4 — HC-007 + HC-007a + HC-007b coalesce sound (no finding)

§5.7 #6 confirms coalesce. Q2 check: all three live in the watcher's read-loop function body — HC-007 declares "subprocess connects back on socket and emits typed messages"; HC-007a is the framing rule (NDJSON + 1 MiB cap, both directions); HC-007b is the EOF/malformed durability rule (decoded line + bus-publish definition + partial-message discard). All three share the JSON-decode + bus-publish state in a single goroutine body; per §2.3 anchor-and-clarification test, HC-007 is the anchor and HC-007a/b are non-omittable clarifications (an implementer building HC-007 cannot ship without the framing or EOF discipline). §2.3 three-AND test fires; coalesce sound. Description on `hc-007` enumerates all three IDs and the 12 message types per §2.8 cluster tag rule. **No finding.**

### F-dq-HC-5 — HC-046 + HC-047 coalesce sound (no finding)

§5.7 #7 confirms coalesce. Q2 check: HC-046 declares the obligation ("provision required skills before agent_ready"); HC-047 declares the resolution mechanism (deterministic single-pass against `skill_search_paths[]`). HC-047's body explicitly says "This is a split from HC-046: HC-046 declares the obligation; HC-047 declares the resolution mechanism" — the spec author flagged the cluster as anchor-and-clarification. Both share one provisioning-and-resolution function body; an implementer of HC-046 cannot omit HC-047's resolution rule without producing a non-conformant adapter. §2.3 three-AND test fires. Description correctly enumerates `req:HC-046` and `req:HC-047` tags. **No finding.**

---

## Multi-step (verifying NOT split)

### F-dq-HC-6 — §7.1 silent-hang FSM correctly NOT split (no finding)

§7.1 has a 5-state state machine (active → warning → soft-terminating → hard-terminating → terminated) with 7 transitions. Surface read fires §2.2 signals 1 (≥3 transitions) and 3 (umbrella loses meaning when stripped). But signal 2 (independent testability) FAILS: per the §3.1 §7-prose no-edge rule, §7 prose itself is not a normative bead-source — the constituent rules sit in §4 (HC-026 anchors the FSM declaration; HC-026a anchors the heartbeat obligation that the FSM keys off of; HC-026b anchors the drain-forced acceptance clause; HC-008a anchors the suspension regime). The FSM's transitions are sub-bullets in `hc-026`'s description. F8b shared-function-body tiebreaker fires: the FSM is one state-machine ticker function; transitions are switch-case branches, not independent code paths with independent failure modes. Correctly resolved as `hc-026` + `hc-026a` + `hc-026b` (3 §4 beads, no step beads). **No finding.**

### F-dq-HC-7 — §7.2 launch handshake correctly NOT split (no finding)

§7.2 has 6 await-message phases (handler_capabilities → version_selected → session_log_location → skills_provisioned → agent_ready → session-construct). Surface fires §2.2 signal 1 (≥3 phases) and signal 3. But the constituent rules are owned by §4 reqs each (HC-009 = handler_capabilities, HC-010 = session_log_location, HC-049 = skills_provisioned, HC-039 = agent_ready, HC-005 = LaunchSpec delivery). Each phase IS independently testable in principle (signal 2 fires), but per §2.2 F8b the entire `launch_handshake` function in §7.2 IS one cohesive function body — a Go implementation does this in one function with sequential `await_message` calls; there is no stable testable boundary between phases (a watcher can't reach handler_capabilities-passed state without LaunchSpec-delivered state). The §4 reqs already capture the per-phase contracts; minting step beads under a `hc-handshake.umbrella` would duplicate. F8b applies → keep as separate §4 reqs with intra-spec ordering edges (HC-009 → HC-007; HC-010 → HC-007; HC-049 → HC-046; HC-039 → HC-007). The pilot's resolution is correct. **No finding.**

---

## Sensor beads

### F-dq-HC-8 — `hc-inv-006` predecessor list missing HC-021 (MAJOR, lane: `local`)

HC-INV-006's body explicitly enumerates "any other terminal condition (crash without outcome, silent-hang, socket break, watcher wedge, protocol mismatch, skill-provisioning structural failure)". The pilot's §3 row lists 12 predecessors but maps "protocol mismatch" implicitly via "HC-021 sub-sentinel cite is implicitly covered via HC-009 → HC-021 chain" (per the row's notes). Per discipline §2.5 source #4 (invariant-body term-use), the rule is that the sensor blocks-on the DEFINING bead — `ErrProtocolMismatch` is owned by HC-021 (§4.5 sub-sentinel declaration), not by HC-009 (§4.2 first-message-emission rule). HC-009 owns `handler_capabilities` first-message; HC-021 owns the sub-sentinel that wraps protocol-mismatch failure. The invariant body's "protocol mismatch" term-use resolves to HC-021. Fix: add `hc-021` to `hc-inv-006`'s predecessor list (currently lists `hc-007, hc-008, hc-008a, hc-024, hc-024a, hc-026, hc-026a, hc-026b, hc-039, hc-044a, hc-048, hc-011a` — 12 preds; should be 13). Q4 sensor-mechanism check passes otherwise (the description names the verification as scenario coverage of every termination path + assertion of exactly-one-terminal-event published per session, which is a real harness-implemented test).

### F-dq-HC-9 — `hc-inv-004` description verification mechanism is real (no finding)

Q4 check: the description names "scenario test asserts work-dispatch-before-agent_ready is rejected; assert pre-agent_ready message order" — concrete scenario-harness-implementable verification, not just a restatement. §10.2 §10.2 conformance-group "HC-039 — HC-041" cited as source; §2.5 source #4 covers HC-009 / HC-010 / HC-049 (the three pre-agent_ready messages by ID-name). 7 predecessors enumerated. **No finding.**

### F-dq-HC-10 — `hc-inv-007` sensor→sensor edges sound (no finding)

Q4 check: HC-INV-007 body explicitly cites HC-INV-003 + HC-INV-004 by ID. Per discipline v0.8 §2.5 F10 sensor→sensor explicit-ID-cite extension, edges fire. Both targets are intra-spec; no `depends-on` validation needed. The §2.5 F12 sensor↔impl one-way rule does NOT apply (sensor↔sensor); F-pilot-AR-r2-2 invariant-as-target exemption is impl→invariant-specific. Edges `hc-inv-007 → hc-inv-003` and `hc-inv-007 → hc-inv-004` correctly emitted. The pilot's F-pilot-HC-7 self-flag is correct. **No finding** (confirms `local` resolution).

### F-dq-HC-11 — `hc-inv-001` description verification mechanism is thin (MINOR, lane: `local`)

Q4 check: the description names "invariant test asserts exactly-one-watcher-per-session via daemon introspection". This is plausible but does not name a SPECIFIC test mechanism (lint? scenario test? runtime introspection probe?). §10.2 prose says "invariant test asserting exactly-one watcher per session via daemon introspection" — slightly more specific (introspection via daemon's `/health` or similar). Description could tighten to name the introspection surface (e.g., "via daemon's health-check endpoint per ON-§4.9" — though that ON cite is forward-deferred, the mechanism naming is what matters). Cosmetic; descriptions of other sensors are similar. **MINOR; pilot author discretion.**

---

## Schema beads

### F-dq-HC-12 — `hc-schema.launch-spec` field list complete (no finding)

Q5 check against §6.1: 13 fields enumerated in spec — `run_id, workflow_id, node_id, agent_type, workspace_path, required_skills, skill_search_paths, timeout, provisioning_timeout, budget, freedom_profile_ref, bead_id (optional), snapshot_token (optional), schema_version`. The pilot's bead description enumerates ALL 13 (counted: run_id, workflow_id, node_id, agent_type, workspace_path, required_skills, skill_search_paths, timeout, provisioning_timeout, budget, freedom_profile_ref, bead_id, snapshot_token, schema_version = 14 — one field of disagreement, not a real defect: the count "13-field RECORD" in the bead title is wrong; actual is 14 fields incl. schema_version. The spec table has 14 lines; the pilot description enumerates all 14. Title says "13-field" — cosmetic typo). **MINOR.**

### F-dq-HC-13 — `hc-schema.handler` field list complete (no finding)

Q5 check: §6.1 declares 2 methods on Handler (`Launch`, `AgentType`). Pilot bead description names both. Sentinel-return-set documented. Cross-spec edge to `ar-027` for the `agent_type` shape. **No finding.**

### F-dq-HC-14 — `hc-schema.adapter` Adapter method count check (no finding)

Q5 check: §6.1 declares 4 methods (`DetectReady`, `DetectRateLimit`, `CleanExitSequence`, `RotateAccount`). Pilot bead enumerates all 4 with signatures + the `ErrDeterministic` if-unsupported caveat. **No finding.**

---

## First-class beads (random sample)

### F-dq-HC-15 — `hc-008a` description matches spec (no finding)

Q1 check: spec body has 3 normative claims — (a) T_shutdown=10s default; (b) silent-hang detection SUSPENDED during window; (c) dirty-exit-inside-window collapses to single agent_completed with `shutdown_exit_code` payload + total-ordering rule. Pilot description covers all three with cite IDs. (Pilot's §5.7 #3 patch correctly drops `hc-inv-006` from predecessors per F-pilot-AR-r2-2.) Predecessor list correct after F-dq-HC-1 inversion is applied (will then include `hc-error.taxonomy` for the `ErrStructural` term-use). **No finding** beyond F-dq-HC-1.

### F-dq-HC-16 — `hc-011a` description matches spec (no finding)

Q1 check: spec body has 5 normative claims — (a) `recover()` barrier converts panic to `agent_failed{ErrStructural, watcher_panic}`; (b) subscriber-panic isolated per-subscriber via dead-letter; (c) `last_read_event_at` distinct from `last_progress_event_at` (timing semantics); (d) supervisor checks at ≤T/4 cadence; non-advance within T/2 → `agent_failed{watcher_wedged}`; (e) bounded-buffer (default 8) channel; buffer-full routes to dead-letter. Pilot description covers all 5 with cite IDs. Predecessor list correct (`hc-011`, `hc-027`, `hc-026a`) — will gain `hc-error.taxonomy` after F-dq-HC-1 fix. **No finding.**

### F-dq-HC-17 — `hc-024` description matches spec (no finding)

Q1 check: spec body declares (a) crash-emission rule (non-zero exit without outcome → `agent_failed`); (b) payload includes mapped sentinel class; (c) routing owned by EM §8. Pilot description covers all three with cite to EM §8 rendered as `em-error.taxonomy` edge. **No finding** beyond F-dq-HC-1.

### F-dq-HC-18 — `hc-026a` description matches spec (no finding)

Q1 check: spec body declares (a) ≤T/2 emission cadence; (b) phase enum `{starting, reasoning, tool_call, waiting_input, rotating, shutting_down}` (extensible additive-only); (c) LLM wrappers MUST synthesize on internal timer; (d) heartbeat resets silent-hang timer; (e) rate-limit windows continue heartbeats; (f) shutdown window suppresses heartbeat; (g) scenario-mode `heartbeat_mode: scripted` carve-out for twins. Pilot description covers all 7 with cite IDs and explicitly names the carve-out's twin-binary scope. F8b sub-bullets-not-step-beads decision sound (one heartbeat-emitter timer body across all 7 sub-clauses). **No finding** beyond F-dq-HC-3 (cycle).

### F-dq-HC-19 — `hc-026b` description matches spec (no finding)

Q1 check: spec body is an acceptance clause — it explicitly defers enforcement to ON-029 + ON-040 and states HC's obligation is "watcher MUST cooperate by NOT also emitting an HC-classified silent-hang event". Pilot description covers the cite chain plus the `forward:on-029` / `forward:on-040` deferred placeholders. Per §5.7 #4 the `hc-inv-006` predecessor was correctly dropped (invariant-as-target exemption). **No finding.**

### F-dq-HC-20 — `hc-049a` description matches spec (no finding)

Q1 check: spec body declares (a) twin-parity applies to wire signal (the `skills_provisioned` event), NOT filesystem; (b) twin MAY skip filesystem side effects; (c) twin MUST emit same `skills_provisioned` event with same claimed skill set; (d) downstream nodes inspecting worktree are NOT part of HC parity surface; SHOULD use event payload as authoritative; (e) twin failing to emit violates HC-INV-002. Pilot description covers all 5 + explicitly notes invariant-as-target exemption applied (`hc-inv-002` not in predecessors). **No finding** (sound application of §5.7 #5 patch).

---

## Missing-coalesce smell check

### F-dq-HC-21 — HC-008+HC-008a kept separate is sound (no finding)

Pilot self-flagged as F-pilot-HC-1 near-miss. Q2 check: HC-008 declares the outcome-delivery contract (`outcome_emitted` is the final progress-stream message; exit status is liveness-only); HC-008a declares the post-outcome shutdown window REGIME (T_shutdown=10s + silent-hang suspension + dirty-exit-collapse total-ordering). The spec puts these in TWO different §4 sub-numbers because the shutdown window is a distinct REGIME (silent-hang suspended; T_shutdown timer active) not a sub-rule of outcome-delivery. They share a Wait()/exit-handler function body but the testable contracts are separable: HC-008's "exit code is liveness-only" can be tested against a handler that exits 0 cleanly without ever entering the post-outcome window; HC-008a's "dirty exit with prior outcome → agent_completed" can ONLY be tested by entering the post-outcome window. Per §2.3 third test (split reduces to "see anchor"), splitting does NOT reduce HC-008a's description to "see HC-008" — HC-008a has independent normative content (`shutdown_exit_code` payload field, total-ordering rule). Correctly separate. **No finding.**

### F-dq-HC-22 — HC-026+HC-026a kept separate is sound (no finding)

Pilot self-flagged as F-pilot-HC-1 near-miss. Q2 check: HC-026 declares the FSM (the watcher's detection state machine); HC-026a declares the handler's heartbeat OBLIGATION. The watcher and the handler are different processes with different code; the FSM consumes the heartbeat as input but the heartbeat-emitter is on the handler subprocess side. Different function bodies (watcher's tick goroutine vs handler's timer goroutine). Per §2.3 first test (single shape/path), the cluster fails — they share a contract (heartbeat-as-FSM-input) but not a code path. Correctly separate. **No finding.**

### F-dq-HC-23 — HC-031+HC-032 missing-coalesce candidate (MINOR, lane: `local`)

Q2 check: HC-031 (common-prefix regex on field names — `(secret|token|password|api[_-]?key|auth)`) and HC-032 (per-handler value-shape regex patterns) are peer redaction rules. Both feed into the redaction middleware (HC-030). Both apply at the same point (event-bus producer path). §2.3 first test (single shape/path) plausibly fires (both are regex-driven middleware hooks); test 2 (anchor-and-clarification) FAILS — neither is the anchor; both are independent peer rules with different scopes (field-name match vs value-shape match). Per the EM §2.6 typed-alias-cluster precedent (F-em-r1-MIN-8), two-of-three is insufficient. Correctly separate. The pilot's §5.7 #16 walk reaches this conclusion. **No finding** beyond confirming the call.

### F-dq-HC-24 — HC-013+HC-013a missing-coalesce candidate (MINOR, lane: `local`)

Q2 check: HC-013 (Adapter surface — 4 methods declared) and HC-013a (RotateAccount call-time constraint — turn boundary, not mid-turn). §2.3 first test passes (both touch the Adapter struct + the RotateAccount method); test 2 (anchor-and-clarification) plausibly passes — HC-013a is unambiguously a constraint on HC-013's `RotateAccount` method. Test 3 (split reduces to "see anchor")... HC-013a has independent normative content (the `LaunchSpec.timeout` window, the `ErrTransient` retry rule), so splitting does NOT reduce to "see HC-013". The §2.2 F8c constraint-requirements-adjacent-to-umbrellas pattern applies more naturally — HC-013a is a constraint on HC-013 with its own bead and a `blocks` edge from the umbrella. The pilot correctly separates. **No finding.**

---

## Over-split smell check

### F-dq-HC-25 — No over-splits found (no finding)

The pilot has zero step beads (per §1 count). All §4 reqs are first-class beads or coalesced clusters. No multi-step protocols of 2 steps. §7.1 and §7.2 are correctly NOT split per F-dq-HC-6 / F-dq-HC-7. **No finding.**

---

## Summary

- **2 BLOCKERs** (F-dq-HC-1: 18 inverted taxonomy edges; F-dq-HC-2: hc-007↔hc-044 bidirectional). Both must be patched before the pilot re-loads cleanly. Both are pilot-lane local errors against existing discipline rules.
- **1 BLOCKER** (F-dq-HC-3: hc-026a↔hc-008a 3-cycle). Pilot-lane.
- **1 MAJOR** (F-dq-HC-8: hc-inv-006 missing HC-021 predecessor). Pilot-lane.
- **3 MINORs** (F-dq-HC-11 thin sensor description, F-dq-HC-12 cosmetic field-count typo, F-dq-HC-23 / F-dq-HC-24 confirmation-only).
- The 5 "no finding" coalesce/multi-step/sample checks (F-dq-HC-4..F-dq-HC-7, F-dq-HC-9..F-dq-HC-10, F-dq-HC-13..F-dq-HC-22) confirm the pilot's structural calls.

Lane breakdown: 4 of 4 BLOCKER/MAJOR findings tagged `local` — the discipline rules that govern these patterns (§2.6 schema-cite direction, §2.7 F13 + F-pilot-AR-10 supporting-cite, §2.5 source #4 term-use single-owner pin) are sound; the pilot misapplied them. However, F-dq-HC-1's recurrence (3rd surfacing of the same anti-pattern: EM #15, EV #15, HC #27 — and now reversed-direction in HC) suggests the discipline could benefit from an explicit anti-example calling out "schema-shaped umbrella beads (`<spec>-error.taxonomy`, `<spec>-events.taxonomy` if any) own a vocabulary; consumers block-on them per §2.6/§3.1 step 4 — these are NOT slot rules in the F13 sense." Class-lane recommendation is documentation-only (no behavioural rule change), at the discipline author's discretion.
