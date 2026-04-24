# Round 2 Twin-Author Implementer Review — handler-contract.md v0.2

## Lens and verdict summary

I am the author of `claude-twin`, the canonical real-handler subprocess substitute used by the scenario harness (S07) and CI per HC-035 / HC-036. My job is to build a binary that a daemon cannot distinguish from `claude-code` at the interface, wire, event, error-class, and tagging surfaces (HC-INV-002). If the spec enumerates enough shape, I succeed without guessing. If not, each guess I make is a future drift source that HC-038 (S07 drift detection) will have to chase.

Overall v0.2 is substantially more implementable than v0.1 for my lens. The decisive wins: NDJSON framing is pinned (HC-007a) and Unix domain socket is pinned (HC-007 + HC-044), so the transport guessing of r1 is closed. The heartbeat obligation (HC-026a) gives me a reproducible silent-hang test surface. The post-outcome shutdown window (HC-008a) turns "when do I exit?" from a hazard into a clock. The structural-vs-transient skill-provisioning split (HC-048 / HC-048a) gives me two separately scriptable failure modes.

Where I still guess: (1) NDJSON object shape — envelope is deferred to event-model §3.1 and the per-message payload shapes to event-model §3.2; HC-INV-002 bites only if those two documents fully pin every field, which is not testable from this spec alone. (2) Heartbeat cadence floor — "≤ T/2" is a ceiling, not a schedule; real Claude Code's synthesized heartbeat cadence is unspecified and my twin cannot match it without a cadence field in the per-handler spec. (3) Skill provisioning side-effects — HC-046 says "file drops / CLI on `$PATH` / MCP registration / reference-doc bundles" but leaves the on-disk shape to OQ-HC-007; my twin must choose whether to fake provisioning (no files written) or genuinely provision (files appear in the worktree). The two choices have different observable effects on downstream nodes. (4) Rate-limit simulation — "the adapter detects clearance" does not tell me what wire-side signal causes clearance detection.

Four of six implementation attempts below are implementable with small clarifications. Two (skill provisioning, rate-limit clearance) have structural gaps I must flag.

## Twin scripting model (prerequisite)

Before I walk the six implementation attempts I have to name the scripting model my twin uses, because every attempt below consumes it. A twin binary is not an LLM emulator; it is a tape player. `claude-twin` reads a scenario script (conventionally a YAML or JSONL file referenced from a LaunchSpec field or a twin-only env var) and emits the scripted sequence of progress-stream messages against the real daemon protocol. The script declares:

- The `handler_capabilities` version list to advertise.
- A `session_log_location` path to announce.
- A `skills_provisioned` set to claim.
- A schedule of `agent_output_chunk` / `agent_heartbeat` messages with relative delays.
- An exit behavior (`outcome_emitted` with a typed Outcome, then clean exit; or abnormal exit at a named relative time; or script-triggered silent hang).

The script is the scenario-author's lever; the twin binary is a neutral player. This model maps onto HC-036's "the subprocess script drives output instead of an LLM" — the twin has a script, not a model. Nothing in HC-035/036 pins the script format, which is correct: script format is a twin-internal detail, not a handler-contract surface. But this does mean my implementation choices for attempts 2–6 are *about the script schema*, not the handler-contract surface itself.

## Attempt 1 — Twin-parity invariant (HC-INV-002) — PARTIALLY IMPLEMENTABLE

The invariant wants "indistinguishable from real handlers to the daemon" across five surfaces: interface, wire protocol, event schema, error-class discipline, tagging. My twin checklist against the spec:

- **Interface surface (HC-001, HC-002, §6.1).** Implementable. `Handler.Launch`, `Handler.AgentType`, all six `Session` methods, error returns from the five-primary-plus-two-sub set — every signature is pinned in §6.1.
- **Wire protocol (HC-005 through HC-010, HC-007a).** Implementable at the framing layer: NDJSON on `.harmonik/daemon.sock`, `handler_capabilities` first, `session_log_location` before `skills_provisioned` before `agent_ready`, `outcome_emitted` last, exit within T_shutdown. The §7.2 pseudocode is a dependable ground truth.
- **Event schema.** NOT locally verifiable. HC-007 enumerates message *names* but punts payloads to event-model §3.2. My twin emits bytes; if its byte layout diverges from claude-code's byte layout by one field, the daemon cannot tell (because the envelope is the same) but any *consumer* downstream of the bus can. HC-INV-002 is scoped to "the daemon," so I meet it narrowly; the broader "scenario tests observe identical event streams" goal needs event-model §3.2 to be watertight.
- **Error-class discipline (HC-020, §8).** Implementable. Each of the five primary sentinels has a detection rule in §8; my twin can map scripted conditions to sentinels deterministically. `ErrProtocolMismatch` (advertise an incompatible version list in `handler_capabilities`) and `ErrSkillProvisioningFailed` (name an unresolvable skill in LaunchSpec) both have clean trigger scripts.
- **Tagging (HC-037).** Implementable in prose but untestable from this spec. My twin's boundary-classification tags must match claude-code's; claude-code has no per-handler spec yet, so I match "what the foundation authors document in the Claude Code handler spec when it lands."

**Guess I must make:** The event-payload shape for every event in §6.4 — I will match whatever event-model §3.2 pins and diverge if it underspecifies. Flag: **this spec claims HC-INV-002 is locally verifiable (§5 verification clause: "zero `if isTwin` branches") but the verification of the full indistinguishability claim lives in event-model, not here.**

Secondary concern for HC-INV-002: the invariant does not quantify over which daemon *generation*. HC-004 scopes `Launch` idempotency to "one daemon generation." If the daemon is restarted mid-scenario and twin-vs-real is a per-generation config-level choice (HC-003), a single scenario could cross generations. The invariant holds per generation; across generations, operator behavior could differ. Minor gap — informative more than normative — but my scenario harness needs to assume same-generation semantics.

## Attempt 2 — NDJSON wire format reproducibility — PARTIALLY IMPLEMENTABLE

HC-007a is clean at the framing layer: one JSON object per line, one `\n` terminator, no embedded unescaped newlines, no whitespace between messages. My twin's encoder:

```go
func (t *twinWriter) emit(msg any) error {
    buf, err := json.Marshal(msg)  // no indentation; Go json never emits bare \n in strings
    if err != nil { return err }
    if bytes.IndexByte(buf, '\n') >= 0 {
        return fmt.Errorf("embedded newline in encoded JSON: %w", ErrStructural)
    }
    _, err = t.conn.Write(append(buf, '\n'))
    return err
}
```

What is pinned: framing, terminator, no whitespace. What is not pinned from this spec:

- **Field ordering within objects.** JSON object key ordering is not semantic in the spec; Go's `encoding/json` emits struct-field-declaration order. If claude-code uses a different language or a map-based encoder, byte-for-byte output diverges. A `scenario-harness` byte-equality assertion would fail on a non-semantic difference. Not this spec's job to pin object ordering, but HC-INV-002 implies it — flag for S07 to resolve via canonical-form comparison rather than byte equality.
- **Number encoding (floats, timestamps).** Timestamps in the envelope (event-model §3.1) — RFC3339 string or epoch int? My twin emits whichever; claude-code emits whichever. Divergence is silent.
- **Daemon-to-handler control messages.** HC-007a says "both directions" use NDJSON. §7.2 names exactly one control message: `version_selected`. Are there others? For my twin, a `Kill` call (§6.1 Session interface) goes over the same socket as a control message — but its name and payload are not declared here.

**Guess I must make:** The set of daemon-to-handler control-message names beyond `version_selected`. Flag: **control-direction message catalog is absent. My twin supports only `version_selected` and hangs up on any other control frame; if daemon evolves to send `pause`, `resume`, or graceful-shutdown control messages post-handshake, my twin silently diverges.**

Additional framing concern: HC-005 specifies LaunchSpec delivery as JSON-on-stdin (≤1 MiB) or file-path argument (>1 MiB). My twin MUST accept both. No guess here — but the twin has to *test* both modes per the §10.2 HC-005–HC-010 obligation, so the scripting schema must allow a "deliver LaunchSpec via file-path" flag. Implementable; flag only that the §7.2 pseudocode does not show a "LaunchSpec-consumed" acknowledgement from the handler, which means my twin cannot delay consumption for test purposes without risking the daemon treating silence as silent-hang (except it cannot — silent-hang is keyed off *progress-stream* messages, and progress stream is post-handshake). Fine.

## Attempt 3 — Heartbeat cadence in a deterministic twin — PARTIALLY IMPLEMENTABLE

HC-026a says heartbeats at cadence `≤ T/2` with phase values drawn from `{starting, reasoning, tool_call, waiting_input, shutting_down}`. For T=600s (MVH default per §7.1), "≤ 300s" is the obligation. My twin can emit every 10s without violating the spec.

The problem for *determinism*: scenario-harness tests want reproducible event streams. If my twin schedules heartbeats on wall-clock, two runs of the same scenario produce different numbers of heartbeats (depending on run duration). If I want byte-reproducible scripted scenarios, I emit heartbeats on a counter (every Nth script step) rather than on a timer. Both are spec-conformant per HC-026a, but they test different things:

- **Wall-clock heartbeats** catch timer-driven regressions in the watcher state machine (§7.1 transitions on `timer tick` guard).
- **Counter-driven heartbeats** produce byte-reproducible streams for golden-file diffing.

```go
// cadence mode is a twin flag, not a handler-contract concern
func (t *claudeTwin) heartbeat(phase string) {
    t.emit(HeartbeatMsg{SessionID: t.sid, Phase: phase})
}
// wall-clock:
go func() { for { time.Sleep(heartbeatInterval); t.heartbeat("reasoning") } }()
// counter-driven:
// every script step, if step%N==0, t.heartbeat("reasoning")
```

**Guess I must make:** Which cadence real Claude Code uses when emitting synthesized heartbeats during extended reasoning. The spec says handlers "MUST synthesize heartbeats on an internal timer" (HC-026a) — "timer" rules out my counter mode for the parity-claiming twin. Flag: **the twin that scenario-harness uses for false-positive resilience testing (HC-026 test obligation in §10.2) must be wall-clock; a counter-driven twin is not a parity twin.** This conflicts with byte-reproducible scenario goals — needs explicit call-out.

## Attempt 4 — Silent-hang state machine determinism — IMPLEMENTABLE

The §7.1 table is fully deterministic given inputs: `last_progress_event_at`, `now`, subprocess alive/exited, timer tick. My twin does not implement the state machine (it is watcher-side, not handler-side); my twin *scripts* the conditions that exercise it.

Scripted scenarios I can write:

- **False-positive resilience (HC-026a test).** Twin emits heartbeats every 300s (= T/2 at T=600s) for 2000s, no other messages. Expected: watcher stays in `active` state the whole time.
- **Silent-hang true positive (HC-026 test).** Twin emits `handler_capabilities`, `session_log_location`, `skills_provisioned`, `agent_ready`, then nothing (no heartbeats) for 4*T seconds. Expected: transition at T → `warning` (emit `agent_warning_silent_hang`); at 2*T → `soft-terminating` + graceful kill; at 4*T → `hard-terminating` + SIGKILL; terminal `agent_failed` with `class=ErrStructural`, `sub_reason=silent_hang_hard_kill`.
- **Warning recovery (HC-026 test).** Twin silent from t=0 until t=T+10s (enter `warning`), then emits a heartbeat at t=T+15s. Expected: state returns to `active`, `agent_resumed_after_warning` emits.
- **Post-outcome shutdown (HC-008a).** Twin emits `outcome_emitted` at t=0 then sleeps 15s (> T_shutdown=10s) refusing to exit. Expected: SIGKILL from watcher at t=10s; `agent_failed` with `sub_reason=post_outcome_shutdown_timeout`. Silent-hang is suppressed during this window per §7.1 paragraph 2.

All four are scriptable from the §7.1 table + HC-008a without guessing.

**Flag:** The table's *absolute-from-last* semantic (soft-terminate at `2*T` from last message, not `2*T` after warning entry) is explicit in §7.1's prose. Good — the v0.1 version was ambiguous and I would have guessed wrong.

Secondary flag: timer-tick cadence is `≤ T/10`, which means my scenario tests have up to 60s (at T=600s) of jitter between the spec-ideal transition time and the observed transition time. Scenario-harness assertions must tolerate this jitter, but my twin does not need to know — the tick is watcher-side. For test-time productivity I will often configure T downward (e.g., T=10s) for hang-related scenarios; the spec permits override via subsystem envelope. Flag that override direction is "only downward with justification" (OQ-HC-001 default): for a twin-hosted scenario this is trivially satisfied but worth naming so scenario authors know they may adjust.

## Attempt 5 — Skill-provisioning mock strategy — NOT IMPLEMENTABLE WITHOUT GUESS

HC-046 says handler "MUST ensure the agent subprocess has every skill named in `LaunchSpec.required_skills[]` available in an agent-type-specific shape" before `agent_ready`. Shapes: "file drops into an agent-visible directory, CLI binaries on `$PATH`, MCP-server registrations, reference-doc bundles." OQ-HC-007 explicitly flags that the on-disk shape is not pinned.

My twin has three strategy choices, each with spec-visible consequences:

1. **Skip provisioning entirely.** Emit `skills_provisioned` with the requested names, do nothing on disk. HC-049 is met at the wire layer; HC-046 is violated in spirit (the subprocess does not "have" the skill). Detectable by any scenario test that inspects the worktree post-`agent_ready`. Silently wrong.
2. **Mock-provision.** Create placeholder files in the declared locations, matching the shape claude-code would install. Requires me to know claude-code's on-disk shape — which OQ-HC-007 says is unpinned.
3. **Genuinely provision** by copying real skill packages. Defeats the twin's purpose (no LLM = no skill needed) and makes my twin's behavior depend on skill-registry availability, which breaks CI hermeticity.

I cannot choose between (1) and (2) without additional spec — which HC-046 defers to `agent-configuration.md` that does not exist yet.

**Flag:** This is the single biggest gap for my lens. HC-046 / HC-047 / HC-049 pin the *signaling* but not the *side effect*. A twin author either invents the side-effect shape (and drifts from real claude-code when it lands) or ships a wire-conformant twin that fails any downstream test that inspects skill artifacts. Recommendation: the spec should either (a) pin a minimum "mock provisioning writes a sentinel file per skill at a declared path" obligation that applies to twins specifically, or (b) accept that skill-provisioning side-effects are out of twin parity scope and document this as a known non-parity axis.

Related but separable gap: HC-048a says transient provisioning failures "MUST be retried in-handler with exponential backoff (base 1s, cap 16s, max 4 attempts) bounded by `LaunchSpec.provisioning_timeout`." For my twin to exercise HC-048a's retry semantics, my script needs a "fail N times then succeed" directive — which is scriptable — but the *exponential backoff* is internal to the handler and therefore my twin's reproducible-timing scripts have to match the spec's 1s/2s/4s/8s schedule. Real claude-code could implement this identically; my twin's per-handler retry loop is its own code path. Implementable, but requires me to hand-code the schedule in the twin rather than derive it from the script. Flag that the timing contract for HC-048a is normatively pinned (good) but that twins re-implement it in twin code, not in script — minor.

## Attempt 6 — Rate-limit simulation (`agent_rate_limited` pending → cleared) — NOT IMPLEMENTABLE WITHOUT GUESS

HC-025 and the §6.4 event catalog name two events: `agent_rate_limited` (carrying `retry_after`) and `agent_rate_limit_cleared`. The Adapter surface (HC-013) declares `DetectRateLimit(event) -> (limited bool, retry_after time.Duration)`. Clearance is described as "the adapter's detection of rate-limit clearance (the session resumes producing output)" in HC-025 prose.

From the twin's side, I need to script a wire sequence that causes the *watcher* (not the adapter — the watcher is the event emitter per HC-011) to publish `agent_rate_limited` and then `agent_rate_limit_cleared`. But the adapter is what recognizes rate-limit — the twin handler writes *what* to the wire that triggers `DetectRateLimit` to return `(true, retry_after)`?

Two interpretations:

- **Interpretation A**: The twin emits a dedicated `agent_rate_limited` progress-stream message (named in HC-007's message-type list). The watcher passes it to `DetectRateLimit` (which for a twin adapter returns `(true, retry_after)` trivially), and publishes the bus event. Clearance is signaled by the twin emitting `agent_rate_limit_cleared` directly, OR by the twin resuming `agent_output_chunk` messages (per HC-025 "session resumes producing output"). The two clearance signals have different observable latency.
- **Interpretation B**: The twin emits a provider-shaped rate-limit signal (e.g., a fake Anthropic 429 response embedded in an `agent_output_chunk`), and the Claude Code adapter's `DetectRateLimit` parses it. Twin must mimic the provider's on-the-wire signal — not handler-contract-level, per-handler-spec-level.

The spec does not say which. HC-007's message-type list includes `agent_rate_limited` and `agent_rate_limit_cleared` as progress-stream messages, which suggests Interpretation A. HC-025's "DetectRateLimit returns limited=true" language suggests the adapter does *something* besides trivial pass-through, which suggests Interpretation B. The two are mutually incompatible: A makes the twin self-sufficient; B pushes twin's rate-limit fidelity into per-handler-spec territory.

**Guess I must make:** I default to Interpretation A (twin emits the event directly; adapter's `DetectRateLimit` is a trivial pass-through for the twin adapter). For clearance, I default to emitting `agent_rate_limit_cleared` as a distinct progress-stream message after a scripted delay equal to the prior `retry_after`. Flag: **this is the largest per-attempt guess in this review, and either interpretation is consistent with v0.2 prose. A one-sentence normative clarification in HC-025 — "handlers MAY emit `agent_rate_limited` as a direct progress-stream message, OR signal rate-limit via provider-shaped content for adapter parsing; twins SHOULD use the direct form" — closes this hazard.**

Related: the question asked specifically about "`agent_rate_limit_status pending`" — I read this as a phrasing of "the twin simulates a pending/in-flight rate-limit state." Under Interpretation A, my twin scripts it as:

```yaml
# scenario script excerpt
- at_relative_ms: 5000
  emit: agent_rate_limited
  payload: { retry_after_seconds: 30 }
- at_relative_ms: 35000
  emit: agent_rate_limit_cleared
- at_relative_ms: 35100
  emit: agent_output_chunk
  payload: { text: "resumed output after rate limit" }
```

The "pending" interval is the 30-second window between the two events; during this window the daemon observes no `agent_output_chunk` messages. Silent-hang detection during this window is a subtle case: HC-026 keys off ANY progress message including heartbeats. My twin MUST keep emitting heartbeats during the rate-limited window (phase=`waiting_input` is the natural choice from HC-026a's enumerated phases). HC-025 does not say this explicitly — it treats rate-limit and silent-hang as independent regimes — but the heartbeat obligation from HC-026a is unconditional. Confirming this in one sentence of HC-025 prose would help ("during rate-limited periods, handlers MUST continue to emit heartbeats per HC-026a").

## Secondary concern — secrets handling in a twin

HC-028 mandates `HARMONIK_SECRET_*` environment-variable delivery and HC-029 forbids the envelope in `agent_started`. My twin does not need real secrets (no LLM call), but to be parity-indistinguishable it must:

- Read the `HARMONIK_SECRET_*` env vars on startup (without using their values) and verify they are present — a "missing secret" scenario is a legitimate script-driven failure mode.
- NOT leak env values into any event payload or stdout/stderr. This is a zero-effort property if my twin never touches the values.
- Declare the same redaction patterns (HC-032) as real claude-code. If real claude-code declares `sk-ant-*` as a per-handler pattern, my twin declares the same — otherwise a twin-specific test injecting an Anthropic-shaped string into an `agent_output_chunk` is redacted by one but not the other. Under HC-INV-002, the declaration is *per-handler*, not per-binary, so both share the claude-code subsystem envelope.

Implementable; flag only that my twin must be listed under the claude-code subsystem envelope's redaction-pattern set, not as a separate subsystem. This is consistent with HC-045's "twin's expected commit hash pinned at workflow/policy configuration time" — twins are handler-bound, not standalone.

## Summary of guesses

| # | Area | Guess | Risk |
|---|---|---|---|
| 1 | Event payload bytes | match event-model §3.2 when it pins fields | HC-INV-002 holds for daemon, not for bus consumers |
| 2 | Daemon→handler control messages | only `version_selected` exists | silent divergence if daemon adds control frames |
| 3 | Heartbeat cadence mode | wall-clock timer (per HC-026a "timer"); byte-reproducibility lost | scenario-harness must tolerate cadence jitter |
| 4 | Skill provisioning side-effects | choose between wire-only or mock-file shape; both wrong | downstream tests inspecting artifacts break |
| 5 | Rate-limit clearance signal | twin emits `agent_rate_limit_cleared` directly | wrong if real handlers use content-based clearance |
| 6 | Object key ordering / number encoding | language-idiomatic (Go struct order, RFC3339) | byte-equality tests fail on non-semantic diff |

Four of six implementation attempts are unblocked by v0.2 (interfaces, NDJSON framing at the byte level, silent-hang exercises, basic handshake). Two (skill provisioning, rate-limit clearance) carry structural gaps where my twin's choice will drift from real claude-code when the per-handler spec lands. Fixing attempts 5 and 6 — either by tightening HC-046/HC-049 for the twin case and adding one-sentence clarity to HC-025 — would make the twin-parity invariant locally verifiable from this spec.

## Recommendations ranked

1. **(blocking twin implementation)** Add one normative sentence to HC-025 disambiguating rate-limit emission: twin-direct event vs. adapter-parsed provider content. Without this, every twin author guesses and the S07 drift-detector has to treat rate-limit as a non-parity axis.
2. **(reduces silent drift)** Add one normative sentence to HC-046 or HC-049 declaring whether the *side effect* of provisioning (files, registrations) is part of the parity contract or explicitly out. Either direction is fine; silence is the worst option.
3. **(one-liner hardening)** Add to HC-025 that heartbeats continue during rate-limited state per HC-026a. This is implied but not spelled out, and getting it wrong in a twin is invisible until a rate-limited scenario crosses T seconds.
4. **(non-blocking)** Expand HC-007 / §7.2 with a note on the expected catalog of daemon-to-handler control messages (MVH: `version_selected` only; anything else is a foundation amendment). Protects against silent drift when post-MVH control messages are introduced.
5. **(non-blocking)** Note in §6.2 or §6.4 that byte-equality comparison of twin vs. real event streams is not the right fidelity test; canonical-form JSON comparison is. This protects S07's drift detector from false positives on non-semantic encoding differences.

Net: v0.2 is *close* to being sufficient to implement `claude-twin` from scratch with no guessing. Two small clarifications close the last two structural gaps.
