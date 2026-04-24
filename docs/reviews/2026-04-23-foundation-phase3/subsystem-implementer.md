# Downstream Implementer Review — Foundation 2026-04-21

## Summary verdict

As a subsystem author about to write S04 (Agent Runner), I can fill most of the subsystem envelope without guessing, and foundation is substantively stronger than the previous cut. The handler contract (C4), workspace-lease contract (C5), checkpoint contract (C2), and event schema (C3) are precise enough that I can cite specific §s for almost every cross-cutting concern. However, five concrete gaps would force me to guess or re-invent: (a) no subsystem-envelope template or authoritative mapping from the nine existing subsystem docs to the new foundation components, (b) the reconciliation *workflow library* has no named owner, (c) the S04-vs-daemon split for spawn mechanics is conceptually clear (§4.12) but the **per-handler goroutine ownership allocation** between the daemon's watcher and S04's per-agent-type handler is under-specified, (d) session-log ingestion path from handler emission → CASS index is described in pieces across §4.2, §5.3, §10.6 but no spec **owns the end-to-end flow**, and (e) the structured-log sink + rotation + multi-daemon file layout is unspecified. Net: I can start writing, but I'd file 3–5 amendment proposals during spec-draft.

## Gaps that would force me to guess

- **S04 envelope, goroutine allocation.** §4.3 says "orchestrator spawns a watcher goroutine per active session; the watcher owns the session's read-loop; agent runner spawns the subprocess but does not own its goroutines beyond launch." §8.5 says "daemon spawns agent processes as children." Question: is the **watcher goroutine** owned by the daemon (S01 territory) or by S04? The sentence structure suggests daemon-owned, which means S04's runtime footprint is effectively *launcher-only* after `Launch` returns. If so, S04's subsystem envelope declares almost no runtime goroutines — but S04 needs to own agent-type-specific health/ready-state detection (per-agent-type handler from the existing subsystem doc). Foundation does not resolve whether per-agent-type lifecycle logic lives inside the watcher goroutine (meaning S04 hands the daemon a `HandlerAdapter` that the daemon calls) or inside an S04-owned goroutine the daemon coordinates with. These are different subsystem specs.

- **Session-log pipeline end-to-end.** §4.2 says handler emits `session_log_location` event. §5.3 says the canonical path is `${workspace_path}/.harmonik/sessions/${session_id}/`. §10.6 says bead_id is stamped in session-log metadata. §2 of Memory Layer (S08) subsystem needs to ingest these. **But no spec defines the `session_log_location` payload schema, the metadata-stamping mechanism (is it the handler? a hook on `agent_started`? a wrapper around the subprocess's stdio?), or the ingestion trigger (does CASS poll, subscribe to `session_log_location`, or follow a manifest?).** I cannot write S04 (emission) or S08 (consumption) without inventing this.

- **Per-subsystem tagging burden scope.** SC-10b says cross-subsystem types must carry tags. But a subsystem envelope also declares "events it produces/consumes." Per C3 §3.1, every event carries tags. Question: when S04 declares `agent_started` in its envelope, does S04 re-tag, or cite the tag foundation assigned in event-model.md §3.2? The answer matters a lot for spec density and for drift-detection between the subsystem spec and event-model. Foundation doesn't say.

- **Handler identification across subsystem boundaries.** S04 owns the `Handler` adapter per agent type. Multiple subsystems need to reference a specific handler — S07 substitutes twins, S05 hooks on handler lifecycle events, S02 policy references `agent_type` in freedom profiles. Foundation defines `agent_type` in §1.6a as "a handler-contract conformance class" but does not define a **stable identifier shape** (string? typed enum? URN-like?). Without this, S07's scenario DSL and S02's YAML policy schema will each invent different strings, and we're back to the naming-drift problem foundation was built to prevent.

- **Control-point registration surface at subsystem-boundary time.** C6 defines `ControlPoint` as one primitive with three kinds. It says "the three kinds share common fields but have distinct trigger types... the unification is in the primitive's shape (one Go struct, one lifecycle, one registration path)." Where is the **registration path**? Which subsystem owns the registry? S02 (Policy Engine) reads policy YAML and constructs ControlPoints — but S05 (Hook System) is the subsystem the existing docs assign to hook-firing. If ControlPoints are one unified primitive, there should be one registry with one owner. Foundation doesn't name that owner, which means S02 and S05 would each claim it and we get drift.

## Cross-cutting ambiguities (multiple subsystems would answer differently)

- **Where does fsync actually happen?** §3.4 specifies *cadence* (every run boundary + every checkpoint). It does not say **which subsystem owns the fsync call**. Event-bus S03 owns the JSONL writer? The orchestrator S01 calls `fsync` directly after emitting a run-boundary event? Both answers are plausible from foundation text. S01, S03, and any subsystem that emits a run-boundary event (reconciliation workflows too) would each reasonably claim or disclaim ownership.

- **Error categorization for events crossing subsystem boundaries.** §4.5 defines Go error types for method returns. §3.2 defines `agent_failed` and `consumer_failed` events. But when a handler emits `agent_failed`, does its payload include the typed error category enum from §4.5, or a separate wire-format string? This affects every subsystem that subscribes to failure events to make routing decisions (S01's transition selection, S09's pattern analysis). Foundation needs one event-payload schema for failure-category; currently it's implied but not specified.

- **Hook dispatch ordering across subsystems.** §6.3 says hooks execute in declaration order *within a subsystem*, then by *subsystem priority*. Foundation does not assign subsystem priorities. Any subsystem author who needs their hook to fire first will claim high priority; drift is inevitable without an authoritative list.

- **How does S09 (Improvement Loop) actually read events?** The old S03 subsystem doc says "Improvement loop reads the JSONL" (not as a live subscriber). §3.7 of foundation defines async consumers and fan-out observers, and §3.6 says observational replay walks JSONL. But S09 is out of MVH per bootstrap.md. Foundation never names S09's subscription mode. When S09 ships post-MVH, does it use the `async consumer` contract (subscribe to the bus) or the observational-replay path (tail JSONL offline)? These have different failure semantics.

- **"Which subsystem publishes the reconciliation workflows?"** C9 says reconciliation workflows live in the workflow library and run on the same primitives as normal work. But no subsystem owns the workflow library for MVH. S07 owns *scenarios* for testing. S01 owns workflow execution. Neither owns "authoring and shipping the reconciliation DOT files." Is this a bootstrap concern, an S01 concern, or a new unlabeled subsystem?

## Subsystem ownership questions that foundation didn't resolve

- **Who owns the ControlPoint registry?** (see above). Candidates: S02, S05, or a merged S02+S05.

- **Who owns the Beads-CLI skill package?** §10.9 says "Skill package: (path TBD; determined at bootstrap time)." Fine for foundation, but before S04 can spec skill provisioning it needs to know whether the skill ships in the harmonik monorepo (and thus is selected by handler-contract at build time) or ships via an external skill registry. The answer changes S04's launch-time resolution contract.

- **Who owns the redaction registry from §4.7?** Foundation mandates it as a cross-cutting middleware in the event-bus producer path. S03 (Event Bus) is the natural owner; S04 (Agent Runner) and every handler-contributor also need to register their own patterns. Foundation says "patterns are declared in the handler's subsystem envelope as redaction entries" — but no component explicitly names the registry's owner or its registration API.

- **Who owns the handler binary-path registry / config?** §4.10 says "launched from repo-relative path." Which subsystem's config section names which path? S04 owns agent-runner code; S02 owns policy YAML; the `harmonik/` config precedence rules live in §6.8. Foundation doesn't say whether `agent_type → binary_path` mapping is policy YAML or deployment config.

- **Who writes structured logs?** §3.8 says "every subsystem emits structured JSON logs." Unspecified: log sink (stdout? file? dedicated directory?), rotation policy, relationship to the JSONL event log. If each subsystem picks independently, logs will be strewn across paths. If one subsystem owns the logger, foundation should name it.

- **Who owns reconciliation workflow *authoring* (not execution)?** C9 says reconciliation runs as a workflow; each category has its own DOT. Those DOT files and their YAML policies are artifacts. S07 (scenarios) is the closest match (scenarios + reconciliation workflows both exercise the same primitives deterministically), but S07 is a test harness, not a production workflow library. Decision needed.

## Affirmations

- **Subsystem envelope (§1.4) is the right shape.** Events-produced/consumed + types-introduced + handlers-implemented + state-owned + control-points-provided + NFRs-inherited + boundary-classification is a complete template. S04's envelope would be writable in ~30 lines.

- **Handler contract is dense and precise.** §4.1–§4.12 covers the Go interface, wire protocol with explicit decision criteria, concurrency, context, errors, secrets, twin parity, ready-state, trust model, skill injection, and modularity boundary. This is the subsystem-author's favorite component.

- **Three-store cross-reference (§2.6) eliminates a whole class of drift.** Having a foundation-level "git wins on completion; Beads on content; JSONL is observational" rule means every subsystem spec that touches state can cite §2.6 instead of inventing its own consistency model.

- **Reconciliation-as-workflow (§9.1) is elegant.** It removes what would otherwise have been a `Reconciliation Subsystem` with its own envelope, and instead makes reconciliation emerge from the primitives every subsystem already supports.

- **Beads-integration as a standalone component (C10) prevents cross-subsystem Beads references from drifting.** When S04 needs to mention Beads (claim semantics on dispatch, terminal-transition writes on success), it cites §10.4 instead of restating.

- **The four-axis + mechanism/cognition tagging is actually tractable** at the subsystem-envelope level because §1.4 + SC-10b scope tagging to *cross-subsystem* surfaces. I was worried tag-everything would be prohibitive; the scoping keeps it finite.

- **DOT + YAML + JSONL triad (§2.1) is well-bounded.** Three formats, non-overlapping responsibilities, clearly delimited. S04's LaunchSpec doesn't need a new format.

Foundation is close. The gaps are concrete enough that amendments per §1.5 are probably the right path rather than re-opening the component structure.
