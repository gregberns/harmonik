# Round 2 Operator-Persona Review — operator-nfr.md v0.3.0

**Reviewer lens:** the SRE / on-call engineer / solo developer. The person who `ssh`s into a wedged daemon at 03:00, runs `harmonik upgrade` against a real project with work in flight, tails structured logs into Loki to debug a stuck reconciliation, and hits `pause` to yield the laptop to a Zoom call. Everything below is read through the question: *"could I actually run this?"*

---

## 1. Verdict summary

The R1 integration fixed most of the spec-on-spec problems (paired-phase event unification, section drift, envelope, RTO numeric target). What remains is a very specific class of gap: the **operator-observable surface** is still mostly declared obligation-shaped (ON-002, ON-003, ON-004, ON-035, ON-041) rather than surface-shaped. An operator reading v0.3.0 learns that a structured log record SHALL carry certain fields, that the taxonomy SHALL exist, that a config inventory SHALL be produced. But the operator cannot write a Vector config, cannot write a runbook for `harmonik upgrade`, cannot write a Grafana panel for RTO breach, and cannot write an incident playbook for exit code 19 vs 20. The obligation-shaping is honest spec discipline — but the gap is that almost nothing is named OPERATOR-first. Every operator surface is named INWARD-first (what subsystems must emit) and then asserted to be operator-observable as a consequence. The consequence is not a surface.

Said differently: v0.3.0 is a good spec for subsystem authors. It is not yet a good spec for SREs. The gap is ~15% of spec volume and mostly additive — there is no architectural wall to break through, just a set of concrete surfaces that have to be either named or explicitly deferred with a post-MVH boundary.

Three concrete verdicts an operator would pronounce:

1. **I can run `harmonik upgrade` and trust it** — YES, provided `PL-027`'s fd-passing rewrite is the authoritative mechanism and ON-020(e)'s "socket retry" language is reconciled with PL v0.4.0's "gap-free adoption." Currently both specs are individually coherent but cross-specify the same concern; an operator reading ON alone gets the old retry-based mental model.

2. **I can debug a wedged daemon from structured logs** — NOT YET. ON-035's inline shape is ~10 fields, no `event_id` / `correlation_id`, no versioning header, no redaction-direction commitment, no rotation policy. A Vector / Fluent Bit config cannot be written against this. OQ-ON-007 acknowledges the deferral but the minimum shape is still thin enough to wedge me.

3. **I can distinguish subsystem failures from each other at 03:00** — MOSTLY YES for startup (codes 2–10, 19, and interim 22/23 per PL) but NOT YET for runtime post-`ready` (§8 code 20 `signal-terminated` covers SIGSEGV/SIGBUS/OOM kill in aggregate; the sub-category is punted to event payload which no payload schema names). I can triage a startup failure; I cannot triage a steady-state runtime crash without reading the coredump.

None of these is a spec-architecture problem. All three are spec-surface gaps that the R2 integration can close in ~60–120 new normative lines across §4.6, §4.9, §4.10, and §8.

---

## 2. The four handoff-mandated probes

### 2.1 Operator attach/detach semantics

**Short answer:** the semantics are almost entirely deferred — to PL §4.10, to `OQ-ON-004`, or to an unwritten CLI spec — and what ON does say is underspecified in operator-relevant ways.

**What the spec says.** The sole explicit normative statement on `harmonik attach` in ON is line 42 (scope: "multi-daemon commands obligation … daemon-identification flags on all daemon-communicating commands (stop, pause, attach, status, upgrade)"), line 467 ON-046 ("Operator-observable MUST NOT require parsing the raw JSONL; a summarized view is adequate"), and an oblique mention at line 437 ON-042 ("`harmonik attach` across N daemons is the same human with the same skills"). PL §4.10 line 558 adds: "multiple simultaneous attaches MUST be supported with no foundation-imposed upper limit; detaching MUST NOT kill the daemon. Concurrent-operator-attach arbitration is deferred to [operator-nfr.md §4.3] (see OQ-ON-004 for cross-spec coordination)." ON OQ-ON-004 (lines 803–808) acknowledges the arbitration gap and picks a default ("second command observes the state-machine in the post-first-command state and either no-ops (both paused) or errors (if incompatible). No explicit lock").

**Operator probe 1: What does `harmonik attach` actually SHOW?** The spec is silent. ON-046 says the budget view is summarized. Nothing says:

- Does attach stream live events as they fire (tail-mode), or render a periodic snapshot (poll-mode), or both?
- Does attach show the current DaemonStatus enum value (per PL §6.1) or a richer view (health-check aggregation per ON-036, RTO metrics per ON-033, budget per ON-046)?
- Does attach show structured log records per ON-035 (the wire-format shape) or a filtered/summarized view?
- Does attach see events from BEFORE the attach started (with replay), or only from attach-point forward (no replay)? The latter is consistent with locked decision #12 (no DTW) but the spec doesn't say.

**Operator probe 2: Consistency model between attached observer and daemon state.** This is load-bearing. If attach shows an event stream with a latency (even 50 ms), an operator who pauses then issues upgrade sees `pausing` in the attached view for longer than the daemon is in `pausing`. That's fine if the operator understands the race; it's a bug if the operator thinks the attached view is an authoritative status. ON-046 gestures at this ("`harmonik status` and the attach UI") but does not say whether the two share a cache / are consistent / use the same query path.

**Operator probe 3: Two operators attached simultaneously.** PL §4.10 permits; OQ-ON-004 defers arbitration. The default-if-unresolved ("second command observes … and either no-ops or errors") is architect-honest but operator-hostile: two SREs both trying to `harmonik pause` during an incident get different results depending on timing. The fix is tiny: define the arbitration as "last writer wins with `operator_command_rejected` for invalid-state-transition-post-first-command" and emit `operator_attached` / `operator_detached` events carrying a `session_id` so the audit log can reconstruct who did what. The current "no explicit lock" default misses the audit-log reconstruction point.

**Operator probe 4: Attach interference with reconciliation.** No obvious interference — attach is a read-side concern and reconciliation is a workflow. But if attach consumes the same socket connection pool as an agent emit-outcome call (per PL-003a NDJSON framing), an attach with a slow TTY could backpressure a critical dispatch. ON should name whether attach connections are bounded / rate-limited / on a separate socket pool. This is a surface the spec should commit on or explicitly defer.

**Operator probe 5: Detach semantics.** PL §4.10 says detach MUST NOT kill the daemon. But does detach flush any queued emissions to the attached session before close? Does detach emit an `operator_detached` event that downstream tooling can key on? The spec is silent.

**Gap summary (attach/detach):**

| Operator-visible concern | Spec state | Gap |
|---|---|---|
| What attach shows | silent | needs normative minimum (enum + health + budget + events) |
| Stream vs snapshot | silent | needs commitment (recommend stream-with-replay-of-last-N) |
| Consistency with `harmonik status` | silent | needs commitment (recommend "both queried from in-memory model; consistent within 100ms") |
| Multiple simultaneous attaches | permitted in PL, ON-042 notes | needs session_id emission for audit |
| Two-op arbitration | OQ-ON-004 punted | needs normative answer, even if the answer is "first-wins with audit" |
| Attach vs reconciliation | silent | needs explicit "attach is read-side, does not block reconciliation" |
| Detach semantics | PL names "does not kill", silent otherwise | needs detach event emission rule |

**Proposed normative additions (R2 integration):**

- **ON-050 (new) — Attach is a read-side observer.** `harmonik attach` MUST NOT invoke any state-mutating operation on the daemon. Attach sessions MUST subscribe to the event stream starting from attach-connect time (no historical replay); the attach TUI MAY additionally render a snapshot derived from `harmonik status` at the same instant. Attach MUST emit `operator_attached` and (on close) `operator_detached` events per §6.5 carrying a `session_id` (random 128-bit ID per session) and `attach_at` / `detach_at` timestamps.
- **ON-051 (new) — Attach consistency with `harmonik status`.** `harmonik status` and the attach TUI MUST query the same in-memory DaemonStatus + health aggregation source; skew between the two MUST NOT exceed 100 ms under the nominal fixture of §4.8.ON-032.
- **OQ-ON-004 resolution:** upgrade from default to normative: "Concurrent operator commands are serialized at the daemon; the command-processing order is the socket-receipt order. A command issued in an incompatible state emits `operator_command_rejected` per §8 code 16. No explicit lock or queueing is introduced. Second-operator audit trail is reconstructable via `operator_attached` session_id in the audit record."

### 2.2 Multi-daemon coordination deferral coherence

**Short answer:** the §4.10 text is surface-level coherent post-I11 but has one important blind spot — PL-014a (per-daemon ceiling) is rlimit-derived and macOS-specific-hostile; ON-041 (machine-level ceiling) is deferred-shape (OQ-ON-003 picks filesystem-lock default). An operator running two harmonik projects simultaneously on one macOS laptop gets the worst of both: per-daemon ceilings calculated against a 256-default RLIMIT_NOFILE (macOS) and a machine-level ceiling mediated by an advisory lock file that (per critic Challenge 8) has no stale-lock-recovery protocol.

**Coherence assessment (what ON now says):**

- ON-041 line 429 explicitly distinguishes per-daemon (PL-014a) from machine-level (ON-041(c)). Good.
- ON-042 lines 437–442 lists the three deferred concerns (shared LLM budgets, shared identity, shared skill registries) and explicitly labels "deferred ≠ dismissed." Good posture.
- ON-042 line 443 uses phrasing "acknowledged as real and explicitly deferred, not solved." Operator-friendly honesty.
- OQ-ON-003 picks a default ("filesystem-based shared-counter lock at `~/.harmonik/machine-ceiling.lock` with advisory locking"). Architect-honest about load-bearing-ness.

**Coherence gaps:**

1. **Two-daemon-one-laptop workflow.** A solo developer with `~/projects/alpha` and `~/projects/beta`, each with a harmonik daemon, wants both to run. What actually happens?
   - Each daemon has its own pidfile and socket per PL-002 / PL-003.
   - Each daemon has its own PL-014a ceiling (derived from `RLIMIT_NOFILE_soft / FDS_PER_HANDLER`, capped at 1024).
   - Both daemons share the machine-level ceiling per ON-041(c) (one lock file at `~/.harmonik/machine-ceiling.lock`).
   - Both daemons query Anthropic against the same API key (per ON-042 bullet 1).
   - **Operator-visible failure:** alpha's daemon is burning through the Anthropic quota with a noisy workflow; beta's daemon's dispatches start failing silently at the Anthropic layer (rate limit / quota exhaustion). The spec does not currently tell the operator which daemon is the noisy neighbor. `harmonik list` per ON-041(a) shows project path + pid + socket + status, but not budget consumption. ON-049 budget attribution is per-daemon only; there is no cross-daemon aggregation surface.
   - **Gap:** ON-041(a) (`harmonik list`) MUST carry a budget-consumption column for this to be operator-useful. Alternatively, a new `harmonik budget-check` command that reads all machine-local daemons' budgets and computes cross-daemon consumption against an operator-configured shared quota.

2. **Lock file stale recovery.** Critic Challenge 8 flagged this and it's still unresolved. OQ-ON-003's "advisory locking" default has a specific gotcha: on Linux `flock(2)` releases on process death, so an advisory lock file at `~/.harmonik/machine-ceiling.lock` held via `flock` does release cleanly. But the file ITSELF doesn't go away — which is fine for `flock`, but if any consumer uses "file present" as a readiness signal (e.g., an outside script), it's wrong. The spec should say: the presence of the lock FILE is not the lock state; only a live `flock` acquisition on the file is the lock state. One sentence closes this.

3. **Machine boundary definition.** Critic Definitional gaps flagged this; still unresolved. The machine-level lock at `~/.harmonik/machine-ceiling.lock` means the boundary is "shares `$HOME`." That's Docker-compatible (one container == one machine, since each container has its own `$HOME`) and fine for multi-user macOS or Linux. But it's wrong for multi-user containers sharing `$HOME` (rare but real). The spec should name the boundary explicitly: "machine" here means "the set of processes sharing `/home/<operator>` filesystem visibility." One sentence.

4. **Post-MVH lift protocol.** ON-042 says the shared-quota concern "is required post-MVH." What additional contracts have to land? The spec is silent. A post-MVH budget coordinator would need its own process-lifecycle (startup, crash recovery, health reporting), its own event stream (budget-allocation events), and its own operator-visible commands. The spec should either (a) name a rough contract shape for post-MVH coordinator ("will need a process lifecycle analogous to PL §4.1 with its own pidfile at `~/.harmonik/coord.pid` … etc.") OR (b) explicitly punt to a future spec work with a codename. Option (b) is lighter and just as honest.

**Proposed normative additions (R2 integration):**

- **ON-041 extension (amend):** Add "(d) `harmonik list` output MUST include a budget-consumption column per daemon (current token count, wall-clock consumed over the last 60 minutes, active run count). This is the per-machine noisy-neighbor observability surface."
- **ON-041 extension (amend):** Add "(e) The machine boundary for the ceiling mechanism is 'processes sharing a single filesystem view of `~/.harmonik/`'. Multi-user systems where `~/.harmonik/` is per-user naturally partition; multi-user systems sharing `~/.harmonik/` across UIDs are a known gap tracked in OQ-ON-008 (new)."
- **OQ-ON-003 amend:** "The advisory lock state is asserted by a live `flock(2)` acquisition, not by the existence of the lock file at `~/.harmonik/machine-ceiling.lock`. Consumers checking 'is the lock held' MUST attempt `flock(f, LOCK_SH|LOCK_NB)` and interpret success-without-block as 'lock is available'. File presence is incidental."
- **OQ-ON-008 (new) — Post-MVH shared-budget coordinator contract:** What does the post-MVH lift look like? Default-if-unresolved: a separate foundation amendment spawns a `budget-coordinator` work with its own codename.

### 2.3 Exit-code taxonomy completeness under panic / signal / drain-failure

**Short answer:** v0.3.0 added codes 19, 20, 21 per critic I7 and this closes most of the completeness gap. Three residual gaps remain, and two are load-bearing for the PL-ON interim-absorption promise. The PL R2 integration requested codes 22 and 23 be absorbed; this integration should absorb them.

**Coverage assessment (codes 0–21 per §8):**

Codes 0–18 per v0.2 — well-covered per critic / implementer affirmations.

Codes 19–21 (new in v0.3) — covered in principle; gaps:

- **Code 19 (runtime-panic).** Remediation says "Inspect structured-log records around the panic timestamp." Good, but structured logs per ON-035 do not currently carry a panic stack (there's no `panic_stack` field named). PL-018a names the panic barrier but doesn't name the stack-emission. An operator at 03:00 who sees code 19 can't actually read the stack without attaching a debugger — the spec promises observability that the wire doesn't deliver.
- **Code 20 (signal-terminated).** Aggregates SIGSEGV / SIGBUS / SIGABRT / OOM-kill into one code. Remediation "Inspect OS-level logs for the signal source." Acceptable for MVH, but the operator would benefit from the daemon emitting a last-gasp event at the panic barrier before exiting under SIGTERM-observable paths (SIGSEGV is not recoverable; SIGABRT may be). The spec doesn't separate interceptable-in-panic-barrier from uninterceptable-OS-signal cases; the remediation is "go read syslog" for all of them.
- **Code 21 (drain-step-errored).** Distinct from timeout-escalated (code 11). Good. But remediation says "Inspect the step-specific error category." The spec doesn't currently say how that category is surfaced — `daemon_shutdown` event augmented with `drain_error={step, error_category}` per §8 row. That sub-event shape is only named here; EV §8.7 would need to absorb the augmented payload. Cross-spec coordination open.

**Gaps the taxonomy still doesn't name:**

1. **Exit code 22 (ntm-unavailable) and 23 (orchestrator-agent-unavailable).** PL-INTERIM per PL §4.10 PL-008a and PL-021a. The R2 handoff explicitly called this out. These are genuine operator-observable failure modes ("you tried to start harmonik but ntm/tmux isn't installed"). Without them, an operator sees `harmonik daemon` exit code 1 (or worse, 127 from shell) and has no idea which prerequisite was missing.

2. **Exit code collisions with shell conventions.** Unix shell convention reserves 126 (command-not-executable), 127 (command-not-found), 128+N (signaled). Harmonik exit code 1 collides with the shell's generic-error-during-setup-or-internal-error convention. A wrapper script running `harmonik daemon || echo "daemon died"` gets code 1 from both "generic-failure" AND from any internal shell setup error. The spec should either (a) state that harmonik's exit codes live in a namespace that may collide with shell codes, with remediation "wrap in a shell that distinguishes by PID/command" OR (b) move generic-failure to code 64 (BSD-convention start of custom exit codes) freeing 1 for "command not understood" parity with shell. Option (a) is simpler and matches Git's approach.

3. **Exit code surface for non-daemon commands.** §8 is written for `harmonik daemon` / upgrade / stop. But `harmonik list`, `harmonik status`, `harmonik enqueue`, `harmonik attach` are also operator-invoked (ON-001 line 138 says so). What does `harmonik list` return when `.harmonik/` doesn't exist? Code 9 (filesystem-unwritable)? Code 17 (multi-daemon-target-missing)? The spec doesn't name per-command exit-code contracts; the implementer has to guess.

4. **Post-panic observability.** Code 19's remediation says "inspect structured-log records around the panic timestamp." But the panic barrier in PL-018a fires after the panic but before any event bus flush — so the panic stack MAY not reach the event log. PL-025a "lifecycle-event pairing tolerance" covers the consumer side. But the operator reading code 19 with no structured-log tail records has no forensic path. The spec should name a mechanism: "Before exiting with code 19, the panic barrier MUST write the panic stack to a dedicated file at `.harmonik/last-panic.log` synchronously (no event bus). This file is the authoritative post-panic forensic surface."

5. **Remediation for multi-code operators.** Exit codes are per-command. A cluster / shared-host scenario where many `harmonik daemon` invocations fail with different codes means an operator has to triage by code frequency. The spec doesn't currently name a `harmonik status --all` or `harmonik list --include-failed` surface that would let the operator see last-crash-code per project daemon.

**Proposed normative additions (R2 integration):**

- **ON-002 amend:** Add codes 22 and 23 per PL's cross-spec coordination request:
  - Code 22 `ntm-unavailable`: `ntm` or tmux not on PATH, or version outside supported set per PL-021a. Event: `infrastructure_unavailable{failed_prerequisite=ntm_unavailable}`. Remediation: install ntm per project bootstrap docs.
  - Code 23 `orchestrator-agent-unavailable`: Claude Code or configured orchestrator-agent binary not resolvable at `harmonik runner --orchestrator-agent` time per PL-028. Event: `infrastructure_unavailable{failed_prerequisite=orchestrator_agent_unavailable}`. Remediation: install Claude Code or disable the flag.
- **ON-053 (new) — Post-panic forensic file.** Before exiting with code 19 (runtime-panic), the panic barrier per PL-018a MUST write the panic stack trace + timestamp + daemon pid + binary commit-hash to `.harmonik/last-panic.log` synchronously (no event bus, direct file write with fsync). Successive panics append (no overwrite); file MAY be rotated by operator. This file is the authoritative forensic surface for code-19 incidents and is cited in §8's remediation for code 19.
- **ON-052 (new) — Per-command exit-code contracts.** Each operator-invoked command (`daemon`, `attach`, `enqueue`, `status`, `pause`, `stop`, `upgrade`, `list`, `runner`) MUST declare which codes from §8 it can emit; declaration lives in §8.1 as a two-column table (command × reachable codes). This is the operator-CLI equivalent of a function signature. Missing from v0.3 entirely.
- **§8 note amend:** "Harmonik exit codes live in a namespace that may collide with shell conventions (1=generic, 126–127=shell, 128+N=signaled). Operators scripting harmonik invocations MUST disambiguate by command identity + exit code, not exit code alone. This is equivalent to Git's convention; we accept the collision for MVH and DO NOT remap codes to avoid it."

### 2.4 Observability envelope concreteness

**Short answer:** ON-035's promoted-to-ON ownership is the right move. But the "minimum wire-format shape" is too thin for an SRE to write a production ingestion config, and three specific wire-format concerns are undeclared or deferred in ways that materially block operator adoption.

**What ON-035 v0.3 declares (lines 383–390):**

> The minimum structured-log shape is a newline-delimited JSON record carrying the fields: `ts` (RFC3339 with ms), `level` ∈ `{debug, info, warn, error}`, `subsystem` (the `source_subsystem` identifier registered per [event-model.md §4.9] EV-034a), `run_id?`, `node_id?`, `msg` (short human-readable), and `fields` (map of typed values). Secrets-redaction per §4.7.ON-022 MUST apply to structured logs before emission.

**Operator surface audit — can I write a Vector config against this?** Not quite. An SRE needs:

1. **Stable field ordering OR JSON-any ordering.** Vector / Fluent Bit's JSON parsers don't care about key order, so this is fine. ✓
2. **Timestamp format.** RFC3339 with ms. ✓ (minor: does the ms precision include microseconds? Go `time.RFC3339Nano` is the usual pick; spec should cite `RFC3339Nano` explicitly).
3. **Level vocabulary.** `{debug, info, warn, error}` — no `fatal` or `trace`. Acceptable for MVH.
4. **Subsystem identifier stability.** "registered per EV-034a" — ON correctly delegates. ✓
5. **Correlation ID.** Not named. An SRE who sees a `warn` record can't correlate to the event that produced it without a `event_id` field. This is a load-bearing gap.
6. **Record versioning.** Not named. When ON-035's schema grows (addition of fields per OQ-ON-007), an SRE's ingestion pipeline needs to detect the version change. Without a `v:` or `schema_version:` field, upgrades to the log format are invisible to the ingestion pipeline, which is worst-case for a production SRE who relies on parser stability.
7. **Event stream vs log stream.** ON-035's structured logs are distinct from ON-034's typed events. Are they emitted to the SAME JSONL file or different files? If same, how does the ingestion pipeline tell them apart? Presumably by the presence of `level` (logs) vs `event_type` (events) but this isn't said.
8. **Error/exception rendering.** The `fields` map is "typed values." Is a stacktrace a string? A structured record? The spec doesn't say.
9. **Redaction direction.** ON-022 says "MUST apply pre-emission." Good — this is the producer-redacts model. But "pre-emission" means before the write hits the sink. A more careful spec would name the redaction boundary: is it pre-format (redaction applied to the template before values are interpolated) or post-format (redaction applied to the rendered string before write)? These are different enough that `HARMONIK_SECRET_FOO=bar` can leak through post-format redaction if the log record was `msg: "Handler got value {x}"` with `fields.x: "bar-wrapped-in-something"`. Pre-format is stronger; spec should pick one.
10. **Log rotation / disk budget.** Not named. An unbounded log file is an operator-hostile default. Some rotation policy ("rotate at 100 MiB; keep 10 generations" or "rotate daily; keep 7 days") is a standard expectation; spec defers to OQ-ON-007.
11. **Per-record redaction log.** When a secret is redacted, is the replacement stable? Does the operator see `[REDACTED]` or `[REDACTED:api_key:prefix=sk_]` or just `***`? Different choices affect downstream analytics (can a log analyzer show "frequency of api_key usage per workflow"?). Spec doesn't pick.
12. **Consumer-side parser contract.** ON-035 lists producer-side fields but says nothing about how a consumer should handle unknown future fields. The N-1 compat rule of ON-018 isn't named as applying here; the compat story for structured logs is not written.

**Which of these blocks an SRE from writing a Vector config?** Practically: #5 (correlation), #6 (versioning), and #10 (rotation) are the ones where the operator has to make a choice the spec should make.

**Proposed normative additions (R2 integration):**

- **ON-035 amend (add minimum fields):** The minimum fields become `ts` (RFC3339Nano), `level`, `subsystem`, `run_id?`, `node_id?`, `event_id?` (UUID correlating to an event-model record if the log line is paired with an event emission), `msg`, `fields` (map of typed values), AND `log_schema_version` (integer, currently 1, bumped per the N-1 window of §4.5.ON-018). The redaction direction is pre-format: secret values MUST be redacted before any template interpolation that would render them into `msg` or `fields`.
- **ON-035 amend (co-location rule):** Structured logs and typed events MAY share a JSONL file at `.harmonik/events.jsonl` + `.harmonik/logs.jsonl` (two files) OR a unified `.harmonik/stream.jsonl` with discriminator field `record_kind ∈ {event, log}`. The choice is per-deployment (operator config, registered in the config inventory per §4.1.ON-004). Default: two separate files.
- **ON-035 amend (rotation):** Default log rotation is size-triggered at 100 MiB with 10 generations retained; default is in the config inventory per §4.1.ON-004 and is operator-configurable. Compressed rotation (gzip) is the default.
- **ON-035 amend (redaction placeholder):** When a secret is redacted, the replacement MUST be `[REDACTED:<category>]` where category is one of `{api_key, token, password, credential, unknown}`. This gives operators a category-level analytics surface without exposing values.
- **OQ-ON-007 amend:** The minimum shape above is sufficient for Vector/Loki ingestion. The FULL schema (including typed-field enumeration, log-rotation ops, parser contract) still lives in `quality-checks.md` per default-if-unresolved.

---

## 3. `harmonik upgrade` operator UX

This is the probe I care most about for operator-trust. I walked the full upgrade workflow and found three surfaces that don't yet cohere.

**Workflow an SRE would write (runbook shape):**

```
1. Tag the current release as rollback target:
   (no spec command for this; operator-external convention)

2. Verify the current daemon is in a resumable state:
   harmonik status   # expect: "ready" with no in-flight reconciliation

3. Put daemon into paused:
   harmonik pause
   # daemon transitions running → pausing → paused
   # expect drain to complete in well under RTO hard-ceiling (300s)

4. Verify drain completed cleanly (ON-027 steps 4-6):
   # no explicit spec surface for "drain-done-cleanly" vs "drain-timeout-escalated"
   # gap: operator reads exit code only at stop, not at pause-complete
   # gap: operator needs to verify Beads ack'd, event bus flushed, workspace leases released
   #      BEFORE running upgrade (per critic Challenge 2)

5. Install new binary:
   harmonik upgrade <binary-path> --expected-hash <hash>
   # daemon validates hash (ON-005), schema (ON-019), exec-replaces (PL-027 fd-passing)
   # new binary starts, runs startup sequence (PL-005), eventually emits daemon_ready
   # expect: operator_upgrade_completed event

6. If anything fails, roll back to old binary:
   # Same harmonik upgrade command with old binary + old hash
   # gap: spec doesn't explicitly name this as "rollback"; it's just re-upgrade
   # gap: cross-version-N-1 allows same-or-downgrade but only if the old binary's
   #      schema is in the new binary's supported set

7. Resume the daemon:
   harmonik resume
   # daemon transitions paused → resuming → running
```

**Gaps identified:**

1. **Step 4 has no observable surface.** An SRE pausing in preparation for upgrade needs to know the drain finished cleanly. ON-027 steps 4–6 are invisible at the operator level. The operator sees `paused` in status and assumes the drain finished. Per critic Challenge 2, this is a real gap — ON-008's §7.1 transition guard was rewritten in v0.3 to require all seven drain steps complete before entering `paused`, which addresses the critic's structural concern. But there's no drain-outcome emission with, e.g., `drain_steps_completed: [1,2,3,4,5,6,7]` or a count of Beads-acks, workspace-releases, etc. The operator has to trust the spec's invariant without a verifiable surface. Recommendation: add `operator_pause_status{status: paused, drain_summary: { ... }}` payload fields per ON-013. The R2 integration should extend the payload list.

2. **Step 5's hash-supply provenance is undefined.** ON-005 says "operator-supplied expected hash" but critic Challenge 7 flagged that "operator-supplied" has no source. An SRE at 03:00 needs to know where to get this hash. The common answer: "run `git rev-parse HEAD` on the commit that built the new binary." The spec doesn't say. Without this, the hash supply is theater — an operator cut-and-pastes a hash from Slack and hopes.

3. **Step 5's `exec_replace` semantics diverge between ON and PL.** ON-020(e) says "socket/client-CLI retry behavior during exec-replacement (clients MUST retry on broken socket for a bounded window; daemon MUST re-bind the same socket path after exec-replace)." PL §4.9 PL-027 (v0.4) says: "PL-027(iii) socket-rebind self-contradiction fixed via fd-passing rewrite (Design A — nginx/HAProxy/envoy pattern): outgoing daemon clears `FD_CLOEXEC` on the listener fd, passes it via `HARMONIK_LISTENER_FD=<n>` alongside `HARMONIK_UPGRADE=1`; new binary adopts via `net.FileListener(os.NewFile(...))` and re-sets `FD_CLOEXEC`. Adoption is gap-free; `T_rebind` interval no longer load-bearing." The two specs describe the same concern with incompatible mental models. Under PL's rewrite, clients DO NOT need to retry — adoption is gap-free. Under ON's current text, clients MUST retry for a bounded window. The R2 integration should reconcile: ON-020(e) should be rewritten to align with PL-027's fd-passing, or explicitly carve out a retry-nevertheless window for clients not using the same daemon socket connection.

4. **Step 6 (rollback) is not named as a first-class operation.** An operator reading ON cannot see "rollback" as a supported workflow; it looks like "re-upgrade to the old binary." This is technically correct but operator-hostile during an incident. A mature upgrade contract names rollback as a specific operation with its own idempotency and observability rules. Recommendation: add ON-020(f) "Rollback is the upgrade operation with the previous binary path + hash. The daemon MUST preserve the pre-upgrade binary path + hash at a defined location (e.g., `.harmonik/previous-upgrade.json`) to enable one-command rollback via `harmonik upgrade --rollback`."

5. **Step 7's resume semantics are thin.** §7.1 says `paused → resuming` on `resume`, and `resuming → running` on "dispatch loop re-entered." An SRE needs to know: does resume retry any deferred dispatches (dispatch_deferred per §8 code 18)? Does resume re-run the Cat 0 pre-check from PL §4.3? The spec is silent. Recommendation: one sentence in §4.3 saying "Resume does NOT re-execute the Cat 0 pre-check; the daemon is already past `ready`. Resume re-enables dispatch; deferred dispatches from `dispatch_deferred` MAY retry per the deferral rule."

6. **Failure-mode: new binary doesn't start.** ON-020 covers the case where the hash check fails (reject, stay in `paused`) and the schema check fails (reject). It doesn't cover the case where the new binary successfully exec-replaces but then crashes during its startup sequence. PL-025a (lifecycle-event pairing tolerance) covers this at the event-pairing layer. But what does the operator SEE? The outgoing daemon is gone; the new daemon crashed; the socket is bound to a dead process; `harmonik status` gets connection-refused. Recovery path: the operator notices, runs `harmonik list` to see a stale pidfile + live-but-dead socket, and ... ? The spec should name the recovery procedure. Recommendation: ON-020(g): "If the exec-replaced binary fails to emit `daemon_ready` within the RTO hard ceiling per §4.8.ON-032, the upgrade is classified as failed-post-exec. Operator recovery: delete the `.harmonik/daemon.pid` stale file and re-run `harmonik daemon` with the rollback binary. The exit code of the crashed new-binary is observable via pidfile-surviving syscall inspection per PL §4.8."

7. **One-command rollback.** No such surface today. An SRE wants `harmonik upgrade --rollback` that does steps 3, 5 (against previous binary), 7 in one call. Currently they have to do all three manually.

**Proposed normative additions (R2 integration):**

- **ON-020 amend (a):** Name the expected-hash sources: "The operator-supplied expected hash MUST be one of (1) the output of `git rev-parse HEAD` on the repository that built the new binary, (2) an explicit `--expected-hash` CLI flag value, or (3) the build-time ldflags-stamped hash read from the new binary via `harmonik <new-binary> version --hash`. The daemon MUST record the source in the `operator_upgrading` event payload as `hash_source ∈ {git-rev, flag-supplied, binary-stamp}`."
- **ON-020 amend (e):** Align with PL-027 fd-passing: "During exec-replacement, the outgoing daemon MUST pass the listener fd to the new binary per PL-027(iii); socket continuity is gap-free. Client-side retry is NOT required for correctness; clients SHOULD tolerate a brief `ECONNRESET` during the exec transition per usual TCP/Unix-socket hygiene."
- **ON-020 amend (f) (new sub-obligation):** Rollback is a first-class operation. The daemon MUST persist the pre-upgrade binary identity (path + hash) at `.harmonik/previous-upgrade.json` at each successful upgrade. `harmonik upgrade --rollback` MUST install the recorded previous binary. Semantics are identical to a forward upgrade.
- **ON-020 amend (g) (new sub-obligation):** Upgrade-post-exec failure path: if the new binary fails to emit `daemon_ready` within the RTO hard ceiling (§4.8.ON-032 Criterion 3), the upgrade is classified failed-post-exec. The operator MUST be able to recover via `harmonik daemon --rollback` which starts the previous binary.
- **ON-013 amend:** `operator_pause_status{status=paused}` payload MUST carry a `drain_summary` field with the counts of (a) in-flight runs checkpointed, (b) handler subprocesses waited-for, (c) event bus bytes flushed, (d) memory-indexing flushes completed, (e) workspace leases released. This is the operator-visible drain completion surface.

---

## 4. Pause / stop / improvement-pause distinguishability

This is the scenario where an SRE attaches to a harmonik daemon at 03:00 and needs to know: "why is the daemon not running?" The answer must come from `harmonik status` alone.

**What the spec says today.** §7.1 lists states: `running`, `pausing`, `paused`, `resuming`, `stopped`, `upgrading`, `improvement-pausing`, `improvement-paused`. ON-013 emits `operator_pause_status` with `pause_reason ∈ {operator, improvement}`. §6.1 of PL (not shown, but referenced) owns the startup prefix (`starting → reconciling → ready`); §4.10 PL-028 says `harmonik status` reports the enum.

**Can an SRE tell `paused` from `improvement-paused`?** Yes — they are different enum values in the state machine. ✓

**Can an SRE tell "why" the daemon is paused?** Partially. If the daemon is in `paused`, the state alone tells the operator it was an operator pause. The `pause_reason` field on the last `operator_pause_status` event tells the full story. Good ✓.

**But: `harmonik status` doesn't currently surface pause_reason.** PL §4.10 PL-028 says "report daemon status over the socket. MUST report the §6.1 DaemonStatus enum value." The enum value is `paused`. The semantic content (pause_reason) lives on the event — which `harmonik status` doesn't query. An SRE who runs `harmonik status` sees "paused" without knowing why.

**Operator-surface gap:** `harmonik status` should name whether the pause is operator-initiated (block on `resume`) or improvement-initiated (will auto-resume when the improvement loop completes). Without this, the SRE doesn't know whether to run `harmonik resume` or wait.

**Proposed normative addition:**

- **ON-054 (new) — `harmonik status` pause subphase reporting.** When the daemon is in `paused` or `improvement-paused`, `harmonik status` MUST report the `pause_reason` as part of its output (not just the enum). When in `improvement-paused`, status MUST additionally report the estimated-completion-time (per improvement-cycle policy, if knowable). When in `paused`, status MUST additionally report how long the pause has been held (`paused_since`). The reporting surface is the same as enum reporting per PL-028; semantic content is owned by ON.

**Can an SRE distinguish `pausing` from `draining-for-stop`?** Currently, no clean way. `pausing` per §7.1 is "drain in progress before paused." `draining-for-stop` per PL §7.1 (`ready/reconciling/degraded → draining` emitting `daemon_shutdown{mode=graceful}`) is drain-in-progress before daemon exit. The two phases look identical to an operator running `harmonik status` — both show the daemon as "not accepting new work; completing in-flight." The spec needs a clean way to distinguish: if the daemon is draining for pause, status should say `pausing`; if for stop, `draining-for-stop` or `stopping`. The enum distinction exists at the PL side (`draining`) but ON's `pausing` and PL's `draining` are two different states on overlapping paths. Cross-spec coordination.

**Proposed normative addition:**

- **ON-055 (new) — Distinguishable pre-pause vs pre-stop drain states.** The pre-pause drain (`running → pausing`) and pre-stop drain (`running → draining` per PL §7.1) are two distinct states despite sharing drain semantics (both execute ON-027's seven steps). `harmonik status` MUST report them as distinct enum values: `pausing` for the pre-pause path; `stopping` (operator-facing label for PL's `draining`) for the pre-stop path. Event emissions distinguish by event type: `operator_pause_status{status=pausing}` for the former; `daemon_shutdown{mode=graceful}` for the latter.

---

## 5. Silent-hang and reconciliation observability

### 5.1 Silent-hang observability

**What the spec says.** ON-040 obligates silent-hang detection (per HC §4.6), names the event as `agent_warning_silent_hang` (I2 rename), and says the operator-observable consequence is (a) the event emission and (b) a subsystem `degraded` classification per ON-037.

**What an SRE sees when silent-hang fires.** Three surfaces:

1. **The event in the event log** (`agent_warning_silent_hang`). If the SRE is tailing the event log, they see it. If the SRE is attached via `harmonik attach`, they see it (assuming attach streams events per ON-050 proposed). ✓
2. **A `degraded` status from the daemon** per ON-037. `harmonik status` should report this. But currently ON-037 says "the subsystem" is classified `degraded`, not the daemon-wide status. An SRE running `harmonik status` might see the daemon as `ready` (because the daemon itself isn't degraded — only the handler subsystem is) with no surface for "one of N handlers is silent-hanging." Gap.
3. **Actionable remediation.** ON-040 obligates the event; HC owns the detection. But what does an SRE DO when silent-hang fires? The event payload per EV §8.3 presumably carries the run_id of the hung run; the SRE's options are: wait (the event-follow-up chain emits `agent_resumed_after_warning` eventually), kill it (`stop --immediate` the whole daemon), or intervene (no surface). Spec doesn't name the operator-level remediation.

**Gap:** `harmonik status` needs a "degraded-runs" surface. When one or more runs are in silent-hang detection window, status should list them by run_id and elapsed-hang-duration.

**Proposed normative addition:**

- **ON-056 (new) — Degraded-run reporting in `harmonik status`.** When one or more runs are in a state that triggered a subsystem-degraded classification (e.g., silent-hang per ON-040, budget-warning per ON-046, handler stalled per HC §4.6), `harmonik status` MUST list the affected run_ids with the degradation reason and elapsed time since degradation-began. This is the operator's triage surface for "my daemon looks healthy but something is wrong with one run."

### 5.2 Reconciliation observability

**What the spec says.** ON-010 says pause is queued during `reconciling` (daemon status per PL). ON-014 names the operator-override command for verdict-pausing. Reconciliation-driven runs are classified per RC §4.3 as Cat 0–6.

**What an SRE sees when reconciliation is active.** The PL daemon status surfaces `reconciling` during startup (PL §4.2 — the `starting → reconciling → ready` progression). Post-`ready`, reconciliation is just another workflow; `harmonik status` sees it as "a run is in-flight" with no special marking. But reconciliation runs are semantically different: they classify other runs, they have their own budget (§4.11 table "Wall-clock budget per-reconciliation-workflow: 10 minutes"), and a stuck reconciliation can block other dispatches via the carve-out of ON-010.

**Gap:** `harmonik status` has no surface for "this run is a reconciliation workflow." An SRE seeing a 45-minute "run in-flight" can't tell if it's a user workflow taking long or a reconciliation workflow that violated its 10-minute budget.

**Proposed normative addition:**

- **ON-057 (new) — Reconciliation-run identification in `harmonik status`.** In-flight runs that are reconciliation-dispatched per RC §4.2 MUST be tagged in `harmonik status` output as such, with an elapsed-time column and a budget-remaining column. Reconciliation workflows exceeding their budget per the category table of §4.11 MUST appear first (sorted by over-budget amount).

### 5.3 Pause-during-reconciliation operator UX

**Scenario.** SRE issues `harmonik pause`. Daemon is in `reconciling`. Per ON-010, pause is queued. But queued for how long? Reconciliation could run 10 minutes. The SRE sees `harmonik status` return `reconciling`, runs `harmonik pause`, gets ... what? No immediate feedback. The pause is queued. The status stays `reconciling`. The SRE has no visibility into "my pause is queued, and will fire in X."

**Gap:** `harmonik pause` when reconciliation is active should return a specific exit/status, not silently queue. An SRE running the command without knowing about ON-010 thinks the command succeeded; then wonders why status stays `reconciling` and then becomes `running` (not `paused`).

**Proposed normative addition:**

- **ON-058 (new) — Pause-during-reconciliation returns informative exit.** When `harmonik pause` is invoked while the daemon status is `reconciling` per PL §4.3, the CLI MUST return a non-zero exit code (new: code 24 `pause-queued-during-reconciling`) with a message indicating the pause is queued. When reconciliation completes, the daemon MUST emit `operator_pause_status{status=pausing, pause_reason=operator}` as usual. The code 24 is advisory, not an error; it allows operator scripting to detect the queued state.

---

## 6. Failure stories — three concrete walkthroughs

### 6.1 Story 1: Daemon crash + restart (the 03:00 scenario)

**Scenario.** At 03:12 the on-call SRE's pager goes off: their monitoring system saw a harmonik daemon process exit unexpectedly for the `alpha` project. They ssh into the box and run `harmonik status`. Response: connection refused (socket path is stale or the new daemon hasn't bound yet).

**What the spec promises:**
- PL §4.8 (Crash semantics): stale pidfile detected; next `harmonik daemon` invocation removes it, startup proceeds.
- ON §4.8.ON-031: RTO target 30s p95 / 300s ceiling.
- ON §8 code 19 (runtime-panic): if the crash was an unrecovered panic, exit code 19 is set, remediation is "Inspect structured-log records around the panic timestamp."
- ON §8 code 20 (signal-terminated): if the crash was a signal (SIGSEGV/SIGBUS/OOM-kill), exit code 20, remediation "Inspect OS-level logs for the signal source."

**What the SRE actually does:**

```
$ harmonik status
Error: connection refused (socket .harmonik/daemon.sock)
$ harmonik list
harmonik-alpha: pid=12345 (stale), socket=.harmonik/daemon.sock, status=unknown
$ cat .harmonik/last-panic.log   # per proposed ON-053
(empty or not present if exit code 20 / SIGSEGV)
$ ls -la .harmonik/
(operator checks pidfile mtime as a proxy for crash time)
$ dmesg | grep -i "harmonik\|OOM"
(OS-level signal source)
$ harmonik daemon    # restart
(waits for ready; `harmonik status` eventually returns "ready")
```

**Where the spec holds up:** stale pidfile recovery (PL), eventual-ready emission (PL-009b), reconciliation of in-flight runs (RC §4.2). All good.

**Where the spec holds up weakly:**
- `harmonik list` on a dead daemon shows "stale" but the spec doesn't currently name a `last_exit_code` column. Operator can't tell if code 19 (panic → forensic file exists) or code 20 (signal → OS-level investigation).
- Post-panic forensic file (ON-053 proposed) is the key surface. Without it the operator is flying blind.
- RTO hard-ceiling breach is an observable per ON-032 ("operator MUST be notified"), but the notification mechanism is `daemon_degraded` event — which the operator sees only if they're watching the event stream. If the operator is watching `harmonik status`, they see `reconciling` for 5 minutes then `ready`. The hard-ceiling breach has to surface in `harmonik status` output.

**Spec fixes this story needs:**
- ON-053 (post-panic forensic file).
- ON-041 amend `harmonik list` to include `last_exit_code` column.
- ON-056 (degraded-run reporting in status) — enables RTO-breach visibility.

### 6.2 Story 2: Runaway resource consumption + pause

**Scenario.** Alice is running `alpha` project and also the `beta` project on her laptop. She starts a workflow in `alpha` that has an unbounded loop (wasn't supposed to, but the prompt was ambiguous). The `alpha` daemon burns through tokens rapidly. Alice's Anthropic bill starts accruing at 10× normal rate. She wants to stop it without losing in-flight work in `beta`.

**What the spec promises:**
- ON-046: budget events are operator-observable. ON-047: per-run budget default (200k tokens). ON-048: exhaustion protocol terminates at safe boundary and routes through pause-and-escalate policy.
- Per-project daemon isolation (PL §4.1): pausing `alpha` doesn't affect `beta`.

**What Alice actually does:**

```
$ harmonik list
harmonik-alpha: pid=12345, socket=..., status=ready, budget=78% of per-run
harmonik-beta:  pid=12346, socket=..., status=running, budget=45% of per-run
$ harmonik --cwd ~/projects/alpha pause
operator_pause_status: pausing (pause_reason=operator)
... drain completes ...
operator_pause_status: paused (drain_summary: {in_flight: 1 → 0 ...})
$ harmonik --cwd ~/projects/alpha status
paused (since 03:42:15, pause_reason=operator, paused_since=15s)
```

**Where the spec holds up:**
- The `--cwd` flag (ON-041 daemon-identification) lets Alice target the right daemon.
- The pause operator-control state machine (§7.1) drives the transition.
- The improvement-pause subtype (ON-012) is distinct from the operator pause, so Alice doesn't confuse them.

**Where the spec holds up weakly:**
- `harmonik list`'s budget column (proposed in ON-041 amend) doesn't exist today. Alice has to know which daemon is noisy by other means.
- ON-048's "exhaustion-routing policy: default is `pause-and-escalate`" is fine in principle, but the default (`pause-on-exhaustion=false`) means the run transitions to a failed state WITHOUT pausing the daemon. Alice's scenario is pre-exhaustion; the automated pause doesn't help. She has to detect the runaway behaviorally and pause manually. Fine, but the spec should name "budget_warning at 80% → operator action" as an expected workflow. Currently the 80% warning is an event, not a surfaced-in-`harmonik status` summary.

**Spec fixes this story needs:**
- ON-041 amend (budget column in `harmonik list`).
- ON-054 (pause subphase reporting in status).
- Rationale note: budget-warning at 80% is a surface the operator is expected to monitor; explicit workflow recommendation.

### 6.3 Story 3: Queue-format mismatch on upgrade

**Scenario.** The team releases harmonik v2.0 which bumps the Beads overlay schema (migration release per §4.5.ON-019). Bob upgrades his daemon without realizing v2.0 is a migration release.

**What the spec promises:**
- ON-019: migration release refuses to install without an operator pause.
- ON-020(d): cross-version state contract refuses upgrade if schema incompatible.
- ON §8 code 15 (upgrade-schema-incompatible): exit code, `operator_upgrade_rejected` event, remediation "Install migration release."

**What Bob actually does:**

```
$ harmonik pause
... successful pause ...
$ harmonik upgrade /path/to/harmonik-v2.0 --expected-hash abc123
operator_upgrade_rejected: reason=schema-incompatible
Exit code: 15
$ echo "remediation?"
# Spec says: "Install migration release." — but Bob IS trying to install the migration release.
# The message should be: "v2.0 is a migration release; follow migration workflow X"
# Currently spec doesn't name migration workflow X.
```

**Where the spec holds up:**
- Hash check passes; schema check catches the mismatch.
- Operator stays in `paused`, daemon is not in a broken state.

**Where the spec holds up weakly:**
- The remediation message "Install migration release" is confusing when Bob was already trying to. The real remediation is "run the migration workflow" — but ON-019 says "a dedicated migration workflow (post-MVH) is the path." So Bob's daemon is stuck in `paused` with no post-MVH migration workflow available. MVH answer: rollback or wait for a point release with the migration workflow.
- Exit code 15's event `operator_upgrade_rejected` carries `reason` but the reason vocabulary doesn't distinguish "schema is older than supported" from "schema requires migration" from "schema is newer than supported." Bob can't tell which case he's in.
- For a solo developer in MVH with no migration workflow, the practical answer is: the team maintains a manual migration procedure in project-level/runbook docs. The spec should name this as the expected MVH posture ("MVH operators receive migration instructions via release-note channel; automated migration is post-MVH").

**Spec fixes this story needs:**
- ON-019 amend: "For MVH, migration releases MUST be accompanied by a manual migration procedure in the release notes. Automated in-place migration is deferred per OQ-ON-009 (new)." OR: an explicit statement that MVH operators using migration releases are in the "stuck-but-recoverable-via-manual-migration" state.
- `operator_upgrade_rejected` event reason vocabulary: distinguish `schema-older-than-supported`, `schema-newer-than-supported`, `schema-requires-migration`.

---

## 7. Operator-surface gaps

This section consolidates the places the spec is still too sparse to operate against — the questions an SRE would ask that the spec doesn't answer.

1. **`harmonik init` is not named.** Critic and PL both note this gap. OQ-PL-003 (PL's side) picks "require `harmonik init` (explicit opt-in); daemon fails with a specific exit code if `.harmonik/` is absent." ON should absorb this exit code into §8 and name `harmonik init` as a first-class command. Current state: missing.

2. **`harmonik list` budget column.** See story 2 and §2.2 discussion. Needed for noisy-neighbor triage.

3. **`harmonik list` last_exit_code column.** See story 1. Needed for crash triage.

4. **`harmonik status` richer output.** ON-056, ON-057, ON-058 proposed above: degraded-runs, reconciliation-run tagging, pause-during-reconciling exit. All are operator-visible surfaces currently missing.

5. **Operator override on reconciliation verdict.** ON-014 names the obligation + the command naming convention (`harmonik confirm-verdict`, `harmonik veto-verdict`). But the command syntax, the surface for listing pending verdicts, and the timeout behavior (what if the operator never responds?) are unspecified. An SRE at 03:00 who doesn't know there's a pending verdict has no way to discover it. Needs: a `harmonik status --include-pending-verdicts` flag or a dedicated `harmonik verdict list` surface.

6. **Attach UI minimum contents.** See §2.1. Without a normative minimum, implementers will build whatever they want; operators get inconsistent TUI across versions.

7. **Migration procedure is not named for MVH.** See story 3. A single rationale-appendix note ("MVH operators receive manual migration procedures via release notes") closes this without a spec work.

8. **`harmonik upgrade --rollback` as first-class.** See §3. Currently possible but not named.

9. **Drain-summary surface on `operator_pause_status{status=paused}`.** See §3. Operator trust in the drain depends on this.

10. **Audit log query surface.** ON-038 defines audit records as a subset of traces. But the operator-side query path — "show me all budget modifications by role X in the last week" — is not named. ON-038 says "No separate audit-log store is introduced; audit is a query over the transition-record sibling files and their projections." But the query tool is not named. For MVH, either a `harmonik audit` command OR an explicit "audit queries are operator-scripted against `.harmonik/transitions/*.jsonl`" statement would close this.

11. **`harmonik pause --improvement` flag distinguishability.** Currently `harmonik pause` is ambiguous: operator-pause. To surface whether a pause is operator or improvement, the spec already uses `pause_reason` in the event. But can an operator explicitly issue an improvement-pause? No — improvement-pause is triggered by improvement-loop policy per ON-012. Fine, but the spec should state this explicitly: operator CAN NOT explicitly issue an improvement-pause. This is a negative design affirmation.

12. **Structured-log correlation with typed events.** See §2.4. Without a shared `event_id` / `correlation_id`, SREs can't join the two streams. Load-bearing.

---

## 8. Operator-trust concerns

Places where the spec promises operator behavior that real SREs won't trust without more rigor.

1. **RTO 30s-p95 target under a "nominal fixture."** ON-032 Criterion 1. The fixture is "≤ a few hundred open beads, ≤ a few dozen in-flight runs." An SRE running a real project doesn't know if "a few hundred" is 200 or 500, nor "a few dozen" is 20 or 80. OQ-ON-005 acknowledges the fixture-relax ambiguity. The spec should name the fixture numerically: 500 open beads, 50 in-flight runs. Otherwise the 30s p95 target is an aspirational number operators can't hold the spec to.

2. **"RTO measurement" mechanism.** ON-033 measures SIGTERM to `daemon_ready`. But who captures the SIGTERM timestamp? The OS kernel via syslog? The daemon itself via a signal handler? A wrapper script? Critic Challenge 6 flagged this. An SRE can't set up the measurement pipeline without knowing the sensor.

3. **"Operator-observable" for budget events.** ON-046 says "MUST NOT require parsing the raw JSONL; a summarized view is adequate." An SRE reads this as: `harmonik status` surfaces current budget per run. But the spec doesn't say that. It says "`harmonik status` and the attach UI." Two separate surfaces. Which one is authoritative? Both? What if they disagree during a load spike?

4. **"Secrets are never logged."** ON-022's "under any circumstance" is an absolute that an SRE has to trust. ON-023 adds the compile-time check. But runtime redaction (the producer-side regex per ON-022) is still the enforcement for string payloads. An SRE who wants to verify the claim has no sensor — ON-INV-003's "regression test harness that writes each durable sink under a fixture whose secrets-injection set is known, then scans the sink's output" is a good sensor, but it's declared at the spec level, not at the operator-tool level. Needs: `harmonik audit-secrets` command that an operator can run against a corpus of JSONL and session logs to check for leaks.

5. **Multi-daemon ceiling under advisory lock.** §2.2 discussed. Advisory locks are OS-level honest but operator-opaque; an SRE watching `harmonik list` can't tell if the machine-ceiling has actually throttled a dispatch. The `dispatch_deferred` event emission is the surface, but deferred dispatches don't appear in `harmonik list` — only running ones do. An SRE who sees two daemons idle but dispatches queueing can't tell if it's the machine ceiling or another cause.

6. **Handler-contract skill injection + egress policy.** ON-025. "Skills requiring filesystem access outside the workspace MUST fail provisioning." An SRE wants to audit: "which skills are provisioned against this daemon, and with what egress policy?" The `skills_provisioned` event is the surface, but this event fires once per handler launch and isn't easily queryable. Needs: `harmonik skill list` showing the operative skill set per active handler.

7. **Startup failure-mode catalog (ON-003) obligation satisfaction.** ON-003 declares the obligation; §10.1 says "production of a co-owned startup failure-mode catalog by spec-draft satisfies ON-003." But the catalog artifact doesn't exist in the current corpus (the implementer review flagged this; v0.3 didn't add it). An SRE reading the spec expects to find a normative list; they find an obligation statement. The spec should either produce the catalog as a §8 sub-table or explicitly name where it lives.

---

## 9. Affirmations — operator-friendly decisions NOT to reopen

1. **`stop --immediate` as the sole carve-out (ON-009).** This is operator-critical. An SRE needs one-command emergency abort. The spec correctly makes this the only path; `pause --immediate` and `upgrade --immediate` are explicitly forbidden. Do not reopen.

2. **Per-project daemon isolation (via PL §4.1, cited at ON-042).** Two projects on one laptop are two daemons, two sockets, two pidfiles. This is architecturally simple and operator-honest. Solo-dev ergonomics work. Do not reopen.

3. **Improvement-pause as a subtype of operator-pause (ON-012).** The state machine stays small (8 states) because improvement-pause is `pause_reason=improvement`, not a new state class. Operators understand "paused = paused, with a reason field." Do not reopen.

4. **N-1 compat window (ON-018).** Operators will install migration releases rarely; N-1 is the smallest window that lets them upgrade without coordinating daemon + state. Wider windows add reader complexity without proportional operator benefit. Do not reopen.

5. **Commit-hash integrity gate as the MVH binary-install check (ON-005).** Full signing is deferred; commit-hash is the MVH gate. This is the right MVH posture — signing is load-bearing for supply-chain security but also load-bearing for the build pipeline. Deferring signing to post-MVH while keeping the integrity-checking posture (fail-closed on mismatch) is honest.

6. **Reconciliation carve-out for pause (ON-010).** Operators don't pause during reconciliation (correct — reconciliation is load-bearing for startup consistency). Queueing the pause is the right behavior; operator sees the pause apply at the earliest safe boundary.

7. **Exit-code taxonomy at §8 (now codes 0–21).** 21 distinct codes with category + detection-rule + emitted-event + remediation-pointer is the right shape for operator triage. Even code 1 (generic-failure) is called out as "MUST be rare." Operators can build monitoring on this.

8. **`harmonik list` as the machine-level fleet view.** ON-041. This is the operator's entry-point command ("where are my daemons"). Per-project daemon identity + socket + status is the minimum honest surface.

9. **`operator_pause_status` paired-phase merge (I3 in v0.3).** The merger into one event with `status ∈ {pausing, paused}` is operator-friendly — consumers don't have to correlate two event types. Do not reopen.

10. **Multi-tenancy deferred, not dismissed (ON-042).** The explicit list of three unsolved concerns (shared LLM quotas, identity, skills) is honest architecture. An SRE can plan around "MVH doesn't solve multi-tenancy; here are the failure modes." Do not reopen.

---

## 10. Cross-spec coordination from the operator's seat

What an operator needs from sibling specs that ON should orchestrate.

1. **`harmonik attach` contents and consistency model (ON + PL).** ON and PL both touch attach (PL owns the socket; ON owns the data model). Needs joint specification of what attach shows and how it stays consistent with `harmonik status`. Recommend ON owns the data model; PL owns the socket protocol.

2. **Codes 22 and 23 absorption (ON §8 from PL-INTERIM).** PL v0.4 explicitly requested this. ON R2 integration MUST land this.

3. **Post-panic forensic file (ON + PL).** PL-018a names the panic barrier. ON should add ON-053 declaring the forensic file. PL should cite it.

4. **Upgrade fd-passing reconciliation (ON + PL).** ON-020(e) currently says "clients MUST retry"; PL-027 says adoption is gap-free via fd-passing. One of the two needs to yield. Recommend ON yields to PL.

5. **Drain-step-errored augmented payload (ON + EV).** §8 code 21 promises a `daemon_shutdown{drain_error={step, error_category}}` payload that EV doesn't currently declare. EV needs to absorb the augmented payload shape.

6. **Reconciliation verdict-pause operator surface (ON + RC).** ON-014 names the obligation; RC §4.5 owns execution. The command syntax, pending-verdict list surface, and timeout semantics need to resolve on one side (recommend RC).

7. **Structured-log parser contract (ON + quality-checks.md future spec).** ON-035 has the minimum shape; the parser contract, rotation policy, and versioning header are deferred to OQ-ON-007. Landing `quality-checks.md` as a future spec work unblocks this.

8. **`harmonik init` naming (ON + PL).** PL OQ-PL-003 picks the default; ON should either absorb the command name into ON §4.10 or explicitly defer to the CLI-surface spec per §2.2.

9. **Handler-contract skill list + egress policy observability (ON + HC).** ON-025 names the obligation; HC owns provisioning. The operator audit surface (`harmonik skill list` or equivalent) is unowned. Recommend ON owns the operator-facing query; HC continues to own the provisioning event.

---

## 11. Recommendation — checklist for the integration author, ordered by severity

**Tier 1 — BLOCKING for operator trust (must land in R2 integration):**

1. **Absorb exit codes 22 and 23 into §8.** PL-INTERIM; cross-spec coordination requested explicitly.
2. **Reconcile ON-020(e) with PL-027 fd-passing.** The current retry-based text contradicts PL v0.4's gap-free adoption.
3. **ON-053: post-panic forensic file.** Without this, code 19's remediation is theater.
4. **ON-054: `harmonik status` pause-reason reporting.** Without this, operators can't distinguish operator-pause from improvement-pause at the CLI.

**Tier 2 — IMPORTANT for operator-surface completeness (should land in R2 integration):**

5. **ON-050 + ON-051: `harmonik attach` normative minimum.** Close the load-bearing attach/detach silence.
6. **ON-013 amend: drain_summary on `operator_pause_status{status=paused}`.** Operator trust in drain completion.
7. **ON-041 amend: `harmonik list` budget + last_exit_code columns.** Story 1 + Story 2 triage.
8. **ON-035 amend: event_id correlation field + log_schema_version + rotation defaults.** Structured-log ingestion configs need these.
9. **ON-020(f) + ON-020(g): rollback as first-class + upgrade-post-exec-failure path.** Story 3 operator recovery.
10. **OQ-ON-004 resolution: last-writer-wins with operator_attached session_id audit.** Don't leave the concurrent-op arbitration silent.

**Tier 3 — RECOMMENDED for operator-friendliness (nice-to-have for R2):**

11. **ON-055: distinguishable pre-pause vs pre-stop drain states.** `pausing` vs `stopping` enum values.
12. **ON-056 + ON-057 + ON-058: status surfaces for degraded runs, reconciliation runs, pause-during-reconciling.** Fine-grained operator triage.
13. **ON-020(a) amend: hash-source naming (`git-rev` / `flag-supplied` / `binary-stamp`).** Tighten the commit-hash-check trust model.
14. **OQ-ON-003 amend: advisory-lock state is `flock` acquisition, not file presence.** Clarify the lock protocol.
15. **ON-041(e): machine-boundary definition (shared `~/.harmonik/` filesystem view).** Close the Docker/multi-user ambiguity.
16. **§8 note: harmonik exit codes may collide with shell conventions; we accept the collision.** Operator expectation management.
17. **ON-052: per-command exit-code contract table.** Which codes each command can emit.
18. **RTO fixture numerification: "500 open beads, 50 in-flight runs" replaces "a few hundred / a few dozen."** Close OQ-ON-005's fixture ambiguity.

**Tier 4 — DEFER to post-integration (new OQs):**

19. **OQ-ON-008 (new): post-MVH shared-budget-coordinator contract shape.** Name the post-MVH lift as a future work.
20. **OQ-ON-009 (new): automated migration workflow is post-MVH; MVH uses manual procedure in release notes.** Close the migration-release remediation ambiguity.
21. **`harmonik audit-secrets` / `harmonik skill list` / `harmonik verdict list` commands.** Operator-facing audit + skill + verdict query surfaces; candidates for the CLI-surface spec per §2.2.

---

**Final assessment.** v0.3.0 is 80% of a production-ready operator spec. The remaining 20% is surface-concreteness: naming the commands and their outputs, naming the wire-format fields operators will parse, naming the failure forensic paths, and naming the mechanism reconciliations between sibling specs. None of this requires re-architecture. An on-call SRE reading v0.3.0 today would say "I can see the shape of what you want me to run, but I can't actually operate against it at 03:00." With the Tier 1 + Tier 2 additions above, the SRE would say "yes, I can run this in production."

**Estimated R2 edit scope:** ~90–140 new normative lines across §4.3, §4.6, §4.9, §4.10, §8, plus one new §4.10 sub-bullet (`harmonik list` columns) and 2–3 new OQs. No requirement retirements; no architectural changes.
