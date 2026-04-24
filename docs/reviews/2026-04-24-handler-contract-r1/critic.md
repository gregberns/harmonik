# Round 1 Critic Review — handler-contract.md v0.1

## Verdict summary

The draft is structurally coherent and picks up the core-scope alignments cleanly (watcher/adapter split, twin parity as invariant, skill-injection fail-launch). The load-bearing softness is concentrated in places where the spec names a policy without naming the alternative it rejected: the watcher/adapter split is declared load-bearing but never contrasted against the two honest alternatives; "fail-launch" on skill provisioning is ruled the right trade without asking whether it survives slow network installs; the six-sentinel taxonomy has two pairs with overlapping default responses; the silent-hang threshold `T` is declared per-agent-type configurable without admitting the MVH default is load-bearing policy, not parameter tuning; and the twin-binary-vs-in-process decision is reaffirmed as an invariant (HC-INV-002) but the spec never re-examines whether that forecloses legitimate in-process test doubles the locked decision did not intend to forbid.

The verdict is **proceed with revisions**: four of the five challenges below need concrete text or an explicit open question before the spec can advance past draft. None require architectural re-work; all require honest acknowledgement of the alternative path. The §A.3 rationale is the most likely home for the additions.

## Challenges (5 load-bearing items)

### Challenge 1 — Watcher/adapter split is pinned as a modularity invariant without contrasting the two honest alternatives

- **Challenge** — HC-011/HC-012/HC-052 pin the watcher-owned-by-daemon + adapter-owned-by-S04 + adapter-is-a-callback-not-a-goroutine split as load-bearing. The spec never examines the two alternative factorings a reader will ask about: (a) S04 owns the watcher goroutine too and the daemon just calls `Handler.Launch` and waits on a channel; (b) the daemon owns both the watcher and the adapter (no S04 subsystem at all). The §A.3 rationale asserts the current split "is the concrete realization of the centralized-controller principle" — which is true of (b) even more strongly than of the current choice.

- **What the spec says** — HC-011 "The daemon… MUST spawn exactly ONE watcher goroutine per active handler session." HC-012 "The Agent Runner (S04) MUST expose one `Adapter` per registered `agent_type` and MUST NOT hold per-session state or spawn per-session goroutines." HC-052 "new shapes provide new adapters; they do not move the concurrency boundary." §A.3 "Adding a new agent type — the common case for extending harmonik — is then just writing an adapter; the concurrency boundary never moves."

- **Is the justification adequate?** — partially. The "adding an agent type is just writing an adapter" argument is real; but it is an argument against (b) (daemon-owns-everything), not against (a) (S04-owns-watcher-too). Under (a) you still add an agent type by writing an adapter — the adapter just runs on S04's goroutine instead of the daemon's. The rationale collapses two comparisons into one.

- **Stronger alternative** — name the two alternatives and the concrete property that rules each out.
  - (a) S04-owns-watcher: would force the daemon to expose the event bus to S04 as a consumer-API-with-backpressure, and would move the concurrency invariant HC-INV-001 from "daemon defect" to "S04 defect" — weakening operator-NFR §7.1 health-check semantics. That is a real argument and it belongs in the spec.
  - (b) daemon-owns-everything (no S04 subsystem): would move per-agent-type dispatch logic into the daemon's routing path, coupling daemon code to agent-type identifiers. This violates HC-051 but the violation-cost needs to be stated, not asserted.
  - The current choice is defensible; the spec just needs 3-5 sentences in §A.3 naming the rejected factorings by shape, not by label.

- **How load-bearing** — important. HC-INV-001 and HC-051 both depend on the split being the right one; if a later reviewer surfaces (a) as a cheaper alternative, the current pin has no defense on record.

### Challenge 2 — Skill-injection fail-launch assumes instant provisioning; slow-install paths are not accommodated

- **Challenge** — HC-046/HC-048/HC-049 require `skills_provisioned` to emit before `agent_ready`, and any unresolvable skill fails the launch with `ErrSkillProvisioningFailed` wrapping `ErrStructural`. The launch-handshake pseudocode (§7.2) awaits `skills_provisioned` with `timeout=spec.timeout` — reusing the whole run timeout as the provisioning timeout. There is no separate provisioning budget, no "slow install" path, no retry on transient provisioning failures (network flakes during an `npm install` for an MCP skill, or a skill-package registry fetch).

- **What the spec says** — HC-046 "A handler MUST ensure the agent subprocess has every skill named in `LaunchSpec.required_skills[]` available… before the subprocess accepts work." HC-048 "If any name in `required_skills[]` cannot be resolved against `skill_search_paths[]`, the handler MUST fail-launch with `ErrSkillProvisioningFailed`." §A.3 "Fail-launch is expensive in operator attention but cheap in wrong work; fail-soft is the opposite."

- **Is the justification adequate?** — partially. HC-047's resolution rule is mechanism-tagged and deterministic — a missing name is a structural bug. That case the fail-launch argument is correct for. But the spec conflates two failures under one sentinel:
  - (i) name does not resolve against search paths (structural; can only fail differently by changing the plan)
  - (ii) name resolves but provisioning the resolved package into the agent-process shape fails at runtime — network fetch times out, disk full, MCP registration handshake fails
  - Case (ii) is classified as `ErrStructural` via HC-022 but it is often genuinely `ErrTransient` (the same provisioning would succeed on retry). The spec's own §8.6 detection rule conflates "does not resolve" with "provisioning fails" and routes both to fail-launch.

- **Stronger alternative** — split HC-048 into two requirements:
  - HC-048a (structural) — unresolvable name against declared search paths; fail-launch with `ErrSkillProvisioningFailed`.
  - HC-048b (conditionally transient) — resolved-but-provisioning-failed; emit the failure with the adapter's per-agent-type classification returning `ErrTransient` or `ErrStructural` based on the failure shape. An `npm install` ENOTFOUND is transient; a package-integrity-check failure is structural.
  - And: declare a separate provisioning timeout on LaunchSpec (`provisioning_timeout`, default 60s) distinct from `timeout`, so §7.2 does not conflate "install time" with "wall-clock work budget."

- **How load-bearing** — blocking. Any handler that provisions a skill via a network package manager (Beads CLI update, MCP-server install, reference-doc bundle fetch) will hit case (ii) in the first week and the spec mis-classifies it.

### Challenge 3 — Six sentinel classes overlap on default response; two pairs collapse

- **Challenge** — §4.5 and §8 declare the six classes as orthogonal, but §8.1 through §8.7 reveal two pairs whose distinctness is load-bearing only at the classification site, not at any consumer. `ErrProtocolMismatch`/`ErrSkillProvisioningFailed` both wrap `ErrStructural` and route identically ("fail-launch, route to re-planning"). `ErrCanceled` and `ErrBudget` both deny further work and both fire a distinct terminal event (`operator_stopped` / `budget_exhausted`) — the distinction is operator-visible, not routing-structural.

- **What the spec says** — §8.2 `ErrStructural` default response: "Route to a re-planning node; do not retry the failing step unchanged." §8.6 `ErrSkillProvisioningFailed` default response: "Fail-launch; emit `agent_failed` with the unresolved skill name in the payload." §8.7 `ErrProtocolMismatch` default response: "Terminate the subprocess; fail `Launch` with `ErrProtocolMismatch`." All three routing eventually at `ErrStructural` per HC-021/HC-022. §A.3 rationale admits this: "`ErrProtocolMismatch` and `ErrSkillProvisioningFailed` are sub-sentinels because… downstream consumers want to `errors.Is` on the narrower class."

- **Is the justification adequate?** — partially. The "downstream wants narrower class" argument is real but it is not a taxonomy argument — it is a labeling argument. The six classes claim orthogonality; the two sub-sentinels collapse under the routing rule. Per the components.md taxonomy criterion ("two classes with the same response collapse to one"), `ErrProtocolMismatch` and `ErrSkillProvisioningFailed` are sub-labels on `ErrStructural` — and the spec itself says as much in HC-021 and HC-022. The claim of "six sentinel classes" is four-classes-plus-two-sub-tags, and should be stated that way.

- **Stronger alternative** — rename §4.5 to "Five sentinel error classes plus two structural sub-sentinels." Structure §8 as five primary classes (Transient, Structural, Deterministic, Canceled, Budget) plus two sub-sentinels that wrap Structural. Explicitly: the primary/sub-sentinel distinction is what "six classes" is hiding. Alternative: genuinely make the sub-sentinels orthogonal by giving `ErrProtocolMismatch` a distinct response (e.g., "terminate run immediately, no re-plan" — which would make version-mismatch a deterministic failure, which is arguably what it should be since a re-plan against the same binary cannot resolve it).

- **How load-bearing** — important. Every consumer that dispatches on class via `errors.Is` currently has to check narrower-before-broader to avoid miscounting; the spec does not say this is an obligation. If a consumer writes `if errors.Is(err, ErrStructural)` first and never checks `ErrProtocolMismatch` it misses the narrower class silently.

### Challenge 4 — Silent-hang threshold `T` is declared per-agent-type-configurable; the MVH default of 120s is the load-bearing policy and the spec pretends it is a parameter

- **Challenge** — §7.1 sets T = 120s as the MVH default with escalation multipliers 2×T (soft-terminate) and 4×T (hard-kill). OQ-HC-001 names T's per-agent-type tunability but the actual policy — "four minutes of silence is a soft-kill; eight minutes is a hard-kill" — is arbitrary and the spec does not acknowledge that most of the cost of getting it wrong is on the operator (killing healthy-but-slow agents, especially Claude Code during large-context reasoning). Claude Code responses routinely exceed 120s of no-output during extended thinking; 480s (hard-kill) is perhaps-generous-enough for MVH but nothing about 120/240/480 is load-bearing-justified.

- **What the spec says** — §7.1 "Per-agent-type threshold `T` is declared in the handler's subsystem envelope (MVH default: T = 120 seconds). Escalation multipliers `M_soft = 2 * T`, `M_hard = 4 * T`." OQ-HC-001 "The MVH default is 120s, but individual agent types (Pi vs. Claude Code) may warrant different values based on their natural tick cadence." Default-if-unresolved: "T = 120s for all agent types."

- **Is the justification adequate?** — no. "Natural tick cadence" is not a definition for T; it is a hand-wave. The real question is: what is the operator-cost of a false-positive silent-hang kill (a healthy agent silenced for 2-3 minutes during extended reasoning) versus a false-negative (a truly hung agent consuming budget for 8 minutes before kill)? The answer depends on whether agents typically emit heartbeats, which is a handler obligation the spec never states. If the spec required every handler to emit a `heartbeat` progress event every 30-60s while reasoning (which Claude Code can do via its output stream; Pi's "tmux progress-bar tick" would need a shim), then T = 120s is defensible. Without that obligation, T is tuned against what agents happen to do today.

- **Stronger alternative** — either (a) add HC-026b: handler subprocesses SHOULD emit a `heartbeat` progress event at least every T/2 seconds during extended reasoning; silent-hang then genuinely means the process is stuck, not reasoning; or (b) make T = 600s default (an order of magnitude larger than Claude Code's empirically-longest silent stretch) and accept the longer detection interval in exchange for ~zero false-positive kills. The current T=120s is defensible only if (a) is also adopted; stating that coupling in the spec would clarify the policy.

- **How load-bearing** — important. Every real-agent smoke run (core-scope §4 Tier 2) will hit this threshold at least once; if false-positive kills are common the drift-test signal is polluted and the scenario-harness (S07) post-MVH drift detection picks up noise.

### Challenge 5 — Twin-as-separate-binary vs in-process test doubles: the locked decision is overreached into forbidding in-process fakes the user never intended to forbid

- **Challenge** — HC-INV-002 and HC-035/HC-036 pin that twin handlers implement the same `Handler` interface, use the same wire protocol (including `handler_capabilities` handshake, `session_log_location`, `skills_provisioned`, `agent_ready` — §7.2 shows at least three await-for-event hops before `Launch` returns), and are launched as separate subprocesses. User memory `feedback_design_preferences.md` records the decision "twin binaries not in-process mocks" as a locked position. But the wire-protocol handshake cost imposed by this spec (stdin/file-path delivery, five-event handshake, subprocess spawn, commit-hash check) is fixed per-session; for a token-free scenario test under core-scope §4 Tier 1, every session pays it. In-process `Handler` implementations (registered as a `Handler` whose `Launch` returns a struct satisfying `Session` without a subprocess) would skip every fixed cost while still passing the interface-conformance tests HC-INV-002 asks for.

- **What the spec says** — HC-035 "A twin handler MUST implement the `Handler` interface… with the SAME method signatures and the SAME error-class discipline as a real handler." HC-036 "A twin handler subprocess MUST emit the same event types… The only permitted differences are: (a) the subprocess script drives output instead of an LLM; (b) model budget is not charged… (c) the binary name suffixes `-twin`." HC-045 "Twin binaries MUST also be launched from a known repo-relative path with an expected-commit-hash check."

- **Is the justification adequate?** — partially. The locked decision is correct for scenario tests (Tier 1-B, daemon↔handler end-to-end): you want the subprocess boundary exercised in test. It is weakly-justified for unit tests of the adapter or the watcher, where in-process test doubles would be both cheaper and more-pointed. The spec conflates "the twin we use in scenario tests" with "every test-double a developer might write." HC-036's "subprocess" wording closes the door on in-process handler implementations even for pure unit tests, which is not the user's locked decision — the locked decision was about test fidelity, not about forbidding in-process fakes.

- **Stronger alternative** — clarify HC-035/HC-036 to apply to "the twin handler used as the real-handler substitute in scenario tests." Unit-test fakes and hand-written `Handler` implementations for targeted adapter tests are NOT twins in this spec's sense and are NOT required to be subprocesses. Add an Out-of-scope bullet: "Hand-written unit-test fakes implementing `Handler` for targeted per-adapter tests are not twins; the twin-parity invariant applies to the canonical twin binary, not to every implementation of `Handler`." This preserves the locked decision and opens the cheap-test path.

- **How load-bearing** — important but not blocking. The spec as written will push developers to write twin-driven tests even for cases where an in-process fake is strictly better; this shows up as slow unit-test suites and test fixtures in a place the locked decision did not intend to forbid.

## MUST/SHOULD discipline

Five items where keyword choice is wrong or permissive language hides a real requirement.

1. **HC-007 vs HC-044 — transport for handler progress events is double-defined.** HC-007 says transport is "named pipe, or Unix socket, or file chosen at launch; transport is the handler's choice subject to per-handler spec." HC-044 says "The handler subprocess MUST communicate back to the daemon via the local Unix socket at `.harmonik/daemon.sock`." These are inconsistent: either the daemon socket IS the transport for handler progress events (then HC-007's delegation-to-handler is wrong) or it is a separate control channel (then HC-044 needs to say so and specify the division). Pick one.

2. **HC-004 idempotency is under-specified on the concurrent-launch case.** HC-004 declares idempotency on `(run_id, node_id)` within a daemon generation; it covers sequential double-launch ("returns the existing Session or `ErrTransient` if the prior session is terminating"). It does not say what happens when a second `Launch` call arrives while the first is still executing its handshake. Is the caller expected to retry after `ErrTransient`? After what delay? The requirement should name that retry policy or explicitly punt it to the per-handler spec.

3. **HC-042 "system handlers MAY resolve via $PATH" has no subject.** The MAY gives the choice to the implementation; the spec does not say who makes the declaration. Should be: "A handler whose `agent_type` declaration carries `system_handler=true` in configuration MAY resolve via $PATH; all other handlers MUST NOT." Without that, the MAY floats.

4. **HC-046 "before the subprocess accepts work"** — "accepts work" is not defined anywhere. The following sentence ("Begins work is the emission of `agent_ready`") clarifies, but the terminology drift is load-bearing: "accepts work" suggests the first input-dispatch, while "begins work" is the self-declared readiness. Pick one and use it consistently.

5. **HC-032 "Each handler spec MAY contribute additional value-shaped regex patterns."** The MAY is correct per RFC 2119 (it is permission, not obligation). But the sentence is followed by HC-INV-003 which requires no-secret-leaks. The combination is: handlers SHOULD declare value-patterns for their known secret formats, and MAY omit only when no handler-specific pattern exists. Clarify with a note: "A handler that emits provider-specific secrets without declaring a pattern is a spec defect caught by review; HC-INV-003 is not self-enforcing for handler-specific patterns."

## Invariants that hold vs. need reinforcement

Per template §5 selection test: an invariant spans multiple subsystems.

- **HC-INV-001 (exactly one watcher per session).** Genuinely cross-subsystem (daemon + S04 + operator-NFR health-check observability). HOLDS as an invariant, but add an observation surface requirement: the daemon SHOULD expose the live watcher count and the active-session count via the health-check endpoint ([operator-nfr.md §7.1]) so operators can detect the invariant violation without attaching a debugger.

- **HC-INV-002 (twins indistinguishable from real handlers).** Genuinely cross-subsystem (daemon + handler + S07 scenario-harness). HOLDS. The "zero `if isTwin` branches" verification is concrete and testable.

- **HC-INV-003 (no secret value crosses the event-bus or log-emission boundary).** Genuinely cross-subsystem (handler + event-bus + structured-log). HOLDS, but missing: an explicit sub-invariant that REDACTED values themselves do not leak the secret — e.g., an error message "bad API key: sk-ant-xxxx..." would be redacted by the value-pattern rule, but a stack trace containing the same string may not run through the same middleware. Add HC-INV-003b requiring the redaction middleware to apply to structured-log fields AND to stack traces emitted via `log/slog`.

- **HC-INV-004 (agent_ready precedes work dispatch).** Genuinely cross-subsystem (daemon routing + handler emission). HOLDS, but ambiguously: the invariant describes the external property ("no dispatch precedes agent_ready") and relies on the daemon's internal state machine honoring the order. Add: the watcher MUST NOT publish `agent_ready` to subscribers before it has delivered `handler_capabilities`, `session_log_location`, and `skills_provisioned` in that order. Currently HC-INV-004 describes the ordering as an external property but does not pin the watcher's emission order.

- **Missing invariant: no handler subprocess is launched without a resolvable and verified binary path.** HC-042 and HC-043 together imply this but never state it as an invariant — the spec defines the rules in §4.10 and then never ties them to a system-wide "launch cannot succeed without a verified binary" invariant. Add HC-INV-005.

## Affirmations

Six decisions that hold up under pressure.

1. **Outcome-delivery-via-event, not exit-code (HC-008).** This is the correct factoring; exit code as liveness signal only is the right pin. Keeps the Outcome schema owned by execution-model and the transport owned by this spec. Crash-without-outcome-event as a structured failure class is the natural corollary.

2. **Version negotiation via `handler_capabilities` (HC-009) as the first event.** Mutual-version selection before any work dispatch is exactly right; the `ErrProtocolMismatch` sentinel as a structural sub-sentinel is the right routing shape even though the taxonomy claim of "six orthogonal classes" is loose (see Challenge 3). The 5-second timeout in §7.2 is a sensible default.

3. **Compile-time event-schema payload check (HC-033).** A startup-time schema rejection on secret-shaped field names is an infrequent but high-value guard; preventing schema drift from silently shipping unredacted secrets is a real win. The invariant is testable and enforceable at boot.

4. **Declaration-only §4.10 trust model (repo-relative path + commit hash, no signing).** The spec is honest that binary signing is post-MVH; the commit-hash gate is the right minimal substitute. Filesystem-permission authenticity on `.harmonik/daemon.sock` without per-connection challenges is also honest — that is the real MVH posture, not wishful per-connection security theatre.

5. **Handler-as-modularity-boundary framing (§4.12).** HC-051/HC-052/HC-053 correctly identify that the execution shape is the most-likely-to-change surface and pin the stable seam at the handler contract. This is a real architectural claim, not just a scope declaration. The concurrency-pin in HC-052 ("execution-shape evolution re-implements the adapter, not the watcher") is load-bearing and correctly-placed.

6. **Twin-parity as invariant, not test discipline (HC-INV-002).** The zero-test-mode-branches assertion is the right pin; even if Challenge 5's in-process-fake exception is granted, the daemon-has-no-isTwin-branches property is preserved. The verification shape ("reviewing the daemon codebase yields zero `if isTwin` branches") is checkable by static analysis.

## Definitional gaps

Terms and predicates used in MUST-triggers that are not rigorously defined.

- **"Begins work" vs "accepts work"** — HC-046 says the handler provisions skills "before the subprocess accepts work," then clarifies that "'Begins work' is the emission of `agent_ready`." Two different predicates in two sentences. The intended meaning is that `agent_ready` is the gate. Normalize: pick one phrase and use it throughout §4.11.

- **"Active session"** — HC-011, HC-INV-001, HC-INV-004 all trigger on "active session." Candidate definitions: (a) the interval `[Launch returns, Wait returns)`; (b) the interval `[Launch called, subprocess exit observed)`; (c) the interval between `agent_started` event and `agent_completed`/`agent_failed` event. Each has different operational consequences — under (b), a session that fails `handler_capabilities` handshake is active; under (a) it never becomes a session. The spec does not pick.

- **"Session id" uniqueness scope** — §6.1 SessionID "unique within daemon generation." What about across daemon generations? Reconciliation investigator handlers carrying `snapshot_token` may need cross-generation session correlation. If session IDs are per-generation-unique only, the correlation needs another field.

- **"Cognition participates in classification"** — HC-023 "No cognition MAY participate in classification." Semantic judgment ("is this the same bug twice?") is explicitly deferred to reconciliation-investigator nodes. But the adapter's `DetectRateLimit` returns `(bool, retry_after)` based on parsing output text — is that classification? The spec implicitly says no because it is deterministic pattern-matching, but does not name that distinction. The rule "cognition = LLM invocation" would suffice if stated.

- **"Orderly termination"** — HC-013 `CleanExitSequence(ctx, session) -> error` is "orderly termination on normal cancellation." Never defined: does orderly mean (a) flushes in-flight outcome before exit, (b) sends an explicit shutdown signal the agent can respond to, (c) waits for `agent_completed` event before killing? Handler writers will implement three different things.

- **"First event on the progress stream"** — HC-009 "The handler subprocess MUST emit a `handler_capabilities` event as the FIRST event on the progress stream." What counts as "on the progress stream"? Does a debug log on stderr count as an event? §4.2 says progress events go on a named stream; the implicit exclusion of stderr works only if stated.

## Hidden assumptions worth surfacing

Seven things the spec assumes that could turn out wrong.

1. **The daemon is single-process.** HC-011, HC-INV-001, and the "daemon-owned watcher goroutine" language all assume a single daemon process. If harmonik ever grows a multi-daemon topology (even for HA failover), "exactly one watcher per session" needs a qualifier. Surface as an assumption in §A.3 or as an out-of-scope bullet: "Multi-daemon topologies are out of scope; HC-INV-001 is scoped per daemon process."

2. **Agent subprocesses are cooperative on `ctx` cancellation.** HC-018 gives Go-side 500ms and subprocess cleanup 5s. The 5s budget assumes the subprocess honors SIGTERM promptly. Claude Code's actual TERM-to-exit latency is not bounded anywhere in the spec. An uncooperative subprocess forces escalation to SIGKILL per §7.1 hard-terminate, but HC-018 does not say that; it just declares the bound. Surface the assumption: "Subprocesses are expected to honor SIGTERM within 5s; the daemon escalates to SIGKILL on exceed per §7.1."

3. **Every skill's provisioning is synchronous and completes before `agent_ready`.** HC-049 requires `skills_provisioned` before `agent_ready`. An MCP-server-backed skill that warms up in the background — reachable by the time the agent first calls it but not at `agent_ready` — is forbidden by this shape. That may be right (fail-launch on unavailable skill is safer) but is not stated as an assumption. The alternative ("provisioning completes in the background, reported via a later event") was not rejected; it was not considered.

4. **The redaction registry sees every event payload before emission.** HC-030 says the redaction middleware is in the event-bus producer path. This assumes no subsystem emits events via a side channel (direct log write, direct file append). The invariant depends on every event going through the bus; the spec should state that explicitly. A subsystem that fsyncs JSONL from inside a panic handler (per core-scope §2) would bypass the middleware; that needs a carve-out or a separate rule.

5. **`snapshot_token` is opaque but known to be reconciliation-specific.** LaunchSpec carries `snapshot_token` as an optional field for investigator handlers. Regular handlers ignore it. This couples LaunchSpec to reconciliation's existence; if another subsystem later wants a similar "bind this launch to a snapshot" field, will it add another opaque field or share this one? Surface the generality question as an Open Question OQ-HC-006.

6. **`agent_type` is a single string, not a versioned pair.** HC-003 and §6.1 treat `agent_type` as a `String`. Claude Code 1.0 and Claude Code 2.0 are the same `agent_type` today; if a future version has incompatible adapter needs, the single-string identifier forces either a new type name (`claude-code-v2`) or adapter branching on version. Neither is declared as the pattern to follow. The existing version-negotiation via `handler_capabilities` handles wire-protocol version, not agent-behavior version — distinct concerns.

7. **Silent-hang timer and context-deadline do not interact.** §7.1 escalates on silent-hang; HC-017/HC-018 escalate on ctx cancellation. Both can fire at once — e.g., a context deadline expires during `soft-terminating`. The spec does not say which path wins; the natural answer is "ctx cancellation supersedes silent-hang escalation, downgrades to ErrCanceled" but this is not declared. A reader implementing the watcher will guess.

## Suggested revision priorities

- **Must add before `reviewed`:**
  - Challenge 2 — split HC-048 into resolution-failure (structural) and provisioning-failure (adapter-classified); add `provisioning_timeout` to LaunchSpec.
  - Challenge 4 — either add a heartbeat obligation on handlers (HC-026b) or raise T default to 600s.
  - HC-007/HC-044 consistency — pick a single owner of the progress-stream transport; resolve the contradiction between "handler's choice" and "daemon socket."
- **Should add before `reviewed`:**
  - Challenge 3 — restructure §4.5 as "five primary classes plus two sub-sentinels"; add an `errors.Is` dispatch-order rule for consumers.
  - Challenge 5 — carve out hand-written in-process fakes from HC-035/HC-036; preserve the twin-subprocess rule only for scenario tests.
  - HC-INV-004 — pin the watcher's internal publication order, not just the external dispatch property.
  - Definitional fixes for "active session" and "orderly termination."
- **Nice-to-have:**
  - Challenge 1 — expand §A.3 with the three-way factoring comparison.
  - New HC-INV-005 for launch-without-verified-binary.
  - Surface the seven hidden assumptions in §A.3 or as Out-of-scope bullets.
  - Open Question OQ-HC-006 on the generality of `snapshot_token` vs. a general "launch-binding" slot.

## Cross-reference sanity

- §9.1 "depends-on" lists `architecture`, `execution-model`, `event-model`, `process-lifecycle`. Missing from the body but used: `control-points` (freedom profile, budget, skill-declaration — §6.1 fields cite control-points), `workspace-model` (session-log pipeline), `reconciliation` (snapshot-token). These are listed under §9.3 "Co-references" correctly — but the test of co-reference vs. depends-on is "does a breaking change in X require a breaking change here?" A change to the skill-declaration surface in control-points §6.11 would force a change here; a change to LaunchSpec.budget's structure ditto. The current allocation between §9.1 and §9.3 errs toward loose coupling; revisit control-points and workspace-model as potential depends-on, not co-references.

- §6.1's LaunchSpec has `snapshot_token : String | None` with a reconciliation back-reference. The co-reference pattern is right (this spec consumes the surface; reconciliation owns it). But the type is `String | None` — opaque to this spec. If reconciliation changes the token shape to a record, nothing fails here. That is genuine loose coupling and correctly-placed.

- The §10.2 prose test obligations are reasonable for bootstrap but should be tagged with the failure modes each test covers, not just the requirement IDs. For example, "HC-026 + §7.1 silent-hang" test names the state transitions but not the false-positive resilience test — which is the actual load-bearing property given Challenge 4. The migration to `testing.md` should capture false-positive rates as a first-class obligation for the silent-hang path.
