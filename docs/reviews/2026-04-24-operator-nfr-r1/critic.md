# Round 1 Critic Review — operator-nfr.md v0.2.0

## Verdict summary

**Proceed with revisions.** The spec has the right bones (state-machine at §7.1, exit-code table at §8, between-task invariant correctly derived from locked decision #10), but the normative body leans on several definitions that cannot be mechanically evaluated, most of the §5 invariants are §4 requirements repeated for emphasis (the exact pattern the template's selection test forbids), and a large number of cross-references to sibling specs are pointed at section numbers those specs no longer use — §3.N for event-model, §8.N for process-lifecycle, §7.N for operator-nfr's own subsections. The cross-reference drift alone will make the spec fail the linter once the reverse-index build runs.

Top load-bearing findings:

1. **"Between-tasks" boundary is glossary-only and circular.** §3 defines "between-task invariant" as "pause and upgrade operator controls complete in-flight runs before taking effect" (line 68), and ON-008 defines the behavior as "allow each in-flight run to proceed to its next durable checkpoint" (line 133). Neither names what "in-flight" means at ON's layer, nor what "finish" means — the predicate is delegated to execution-model's `run` lifecycle but the predicate needed here is a snapshot-boundary predicate, not a lifecycle predicate. See Challenge 1.
2. **"Pause = finish in-flight, stop pulling new" is silent on consumer-side async completion.** The drain sequence at ON-027 enumerates seven steps, but the pause state machine at §7.1 and ON-008 transition to `paused` at "last in-flight run reaches next checkpoint" — BEFORE the event-bus flush, memory-indexing flush, or workspace-unlock of ON-027 steps 4–6. Two disjoint completion predicates live in the spec. See Challenge 2.
3. **Systemic cross-reference drift.** The spec cites `[event-model.md §3.1, §3.2, §3.4, §3.5, §3.7, §3.8]` in at least a dozen places (lines 71, 98, 112, 165, 180, 236, 282, 315, 321, 340, 352, 378, 398, 418, 438, 442, 451–461, 568–584). Event-model's §3 is its Glossary; the envelope is §4.1/§6.1, payloads are §8 and §6.3, fsync is §4.4, schema is §4.7, replay is §4.5. Same drift on `[process-lifecycle.md §8.1–§8.4]` (PL's §8 is empty per grep; startup is §4.2, command surface is §4.10, queue-empty is §4.4). ON is also self-citing §7.5 (e.g., PL-027 points to `[operator-nfr.md §7.5]` for the upgrade contract), but the actual upgrade contract is §4.6 here. A spec-template-v1.1 corpus walk is needed. See §Cross-reference audit.
4. **§5 invariants are §4 requirements repeated.** Four of five invariants restate §4 requirements verbatim and do not satisfy the §5 selection test ("an invariant is a system-wide property that constrains multiple subsystems' requirements. If the rule fits inside one subsystem's §4 without reference to others, it is a requirement, not an invariant"). Same disease as execution-model-r1 Challenge on §5. See §Invariant audit.
5. **RTO target X is a placeholder, not a number.** ON-031 says "within **X seconds**" with criteria given in ON-032 ("≤ 30 seconds for 95th percentile"). The exemplar guidance ("aggressive targets don't excuse hand-wavy RTO — ON must name numeric targets with sensors") calls for a concrete target bound to a measurement fixture — the spec gives the former without the latter. See Challenge 6.

The verdict is **proceed with revisions** because none of the findings require re-architecture, but six of them require concrete text before the spec can advance past draft: (a) define "in-flight" as a predicate over runs; (b) reconcile the §7.1 and ON-027 completion predicates; (c) fix cross-references systematically; (d) delete §5 invariants that are not cross-subsystem; (e) bind RTO X to a numeric target with a declared sensor; (f) define the queue-format compat semantics operationally beyond "N-1 readable" (what "readable" means for a SQLite store + commit trailers + event references is not a single contract).

---

## Challenges (8 load-bearing items)

### Challenge 1 — "In-flight run" is the load-bearing predicate for every between-task control; it is never defined

- **Challenge** — pause, upgrade, improvement-pause, stop-graceful, stop-immediate, and the drain protocol all route on the predicate "in-flight run." The spec uses the phrase 14 times (e.g., lines 133, 140, 221, 260, 267, 424) and never names the rule by which an implementer decides whether a specific run is in-flight at a given instant.

- **What the spec says** — ON-008: "An operator `pause` or `upgrade` command issued while the daemon status is `ready` MUST NOT interrupt any in-flight run. The daemon MUST transition to `pausing`, allow each in-flight run to proceed to its next durable checkpoint per [execution-model.md §4.5]." Glossary (§3) offers "between-task invariant — pause and upgrade operator controls complete in-flight runs before taking effect" but does not define "in-flight run." Cross-reference hits `[execution-model.md §4.3]` for the `run` definition; that spec defines the lifecycle (started, completed, failed) but does not define "in-flight" either (see execution-model-r1 critic §Definitional gaps item 5).

- **Is the justification adequate?** — no. Two candidate definitions are live:
  - (a) "A run whose most recent event is `run_started` and neither `run_completed` nor `run_failed` has been emitted" — an event-sequence predicate.
  - (b) "A run whose state in the orchestrator's in-memory model is any non-terminal status" — a state predicate.
  - These are not the same: (a) is observable from the event log and JSONL; (b) is observable from the orchestrator's memory. During the drain itself the two can disagree: a run has just emitted `run_completed` but the orchestrator's cleanup (memory flush, workspace unlock) has not finished. Is that run still in-flight for pause-semantics purposes?

- **Stronger alternative** — define `in_flight(run)` in §3 as a mechanically decidable predicate over the run's durable-state-machine position:
  > "A run is in-flight iff its state per [execution-model.md §7.1] is not in `{COMPLETED, FAILED, CANCELED}` AND its last checkpoint commit's transition has been durably written to git AND the orchestrator has not yet emitted a lifecycle-terminal event for the run. The predicate is evaluated against the in-memory orchestrator model; a consumer outside the daemon (e.g., `harmonik status`) reads the predicate from the aggregated `harmonik status` surface, not by re-deriving from git or JSONL."

  Make ON-008, ON-027, and §7.1's transition guard ("last in-flight run reaches next checkpoint") all route on this single predicate.

- **Load-bearing level** — **blocking**. Every between-task guarantee rests on this predicate. An implementer reading the spec cold cannot produce a conforming pause without choosing between (a) and (b), and the spec does not help.

### Challenge 2 — Pause-semantics precision: "finish in-flight, stop pulling new" is silent on consumer-side async completion

- **Challenge** — the between-task invariant (ON-008, ON-INV-004) says pause completes in-flight runs to their next durable checkpoint before taking effect. The drain protocol (ON-027) says graceful shutdown runs seven ordered steps: dispatch-stop, runs-to-checkpoint, handlers-exit, event-bus flush, memory-index flush, workspace-unlock, exit. Steps 4–6 are consumer-side async completion — and the pause state machine at §7.1 transitions `pausing` → `paused` at "last in-flight run reaches next checkpoint" (line 473), which corresponds only to ON-027 step 2. An operator who pauses and then issues `harmonik status` during steps 4–6 sees `paused`, but event-bus has not flushed, memory-index has not flushed, workspace leases are still held. An operator who then issues `upgrade` (ON-020) begins exec-replace against a state where consumer-side work is incomplete.

- **What the spec says** — §7.1 transition: `pausing` → `paused` on "last in-flight run reaches next checkpoint." ON-027 applies only to `stop --graceful` / SIGTERM, not to `pause`. ON-008's text: "proceed to its next durable checkpoint per [execution-model.md §4.5], and only then transition to `paused`." No mention of event-bus flush, JSONL fsync, Beads-write ack, memory-index flush.

- **Is the justification adequate?** — no. Three real consumer-side completions are non-obvious between pause and upgrade:
  - **JSONL fsync.** An fsync-boundary event (per event-model's durability classes) may not have flushed at the moment of `pausing` → `paused` transition.
  - **Beads-write ack.** Checkpoint commits carry `Harmonik-Bead-ID` trailers; the corresponding Beads CLI call (`br` per beads-integration §10.8) may not have returned success — and the upgrade-preserves-recoverability invariant (ON-021) depends on Beads being ack'd.
  - **Workspace lease release.** Workspace-model §5.1 leases are held by running handlers; the "last run reaches checkpoint" predicate doesn't guarantee leases are released.

- **Stronger alternative** — either:
  - (a) Extend the `pausing` → `paused` transition in §7.1 to require the same sub-sequence as ON-027 steps 4–6 (event-bus flush, Beads-write ack, memory-index flush, workspace leases released), OR
  - (b) Add a new intermediate state `quiescing` between `pausing` and `paused`: `pausing` = dispatch-stop + runs-to-checkpoint; `quiescing` = consumer-side completions (fsync, Beads-ack, memory-flush, workspace-unlock); `paused` = all complete. Emit `operator_quiescing` on entry and `operator_paused` on exit. Then the upgrade pre-condition at §7.3 can key off `paused` with full guarantee.

  The spec currently relies on (a) implicitly but never states it. Option (b) is cleaner and makes `harmonik status` more honest.

- **Load-bearing level** — **blocking** for upgrade. ON-021 ("Upgrade preserves in-flight run recoverability") cannot hold across exec-replace if exec-replace begins before Beads-write ack has completed. The spec asserts the invariant but does not specify the mechanism.

### Challenge 3 — Exit-code taxonomy is incomplete: panic-recovery, SIGBUS/SIGSEGV, and mid-transition crash have no code

- **Challenge** — §8 enumerates 18 exit codes (lines 536–554) with a `generic-failure` (code 1) fallback. Several daemon failure modes are missing or under-specified:
  - **Go runtime panic (unrecovered).** The daemon's Go runtime crashing with a stack trace does not cleanly exit via the code-returning path. What exit code does the OS report, and is that observable to the operator?
  - **Signals (SIGBUS, SIGSEGV, SIGTERM during drain).** ON-028 handles `stop --immediate` = SIGKILL; ON-027 handles SIGTERM; nothing addresses SIGSEGV (segfault during agent-subprocess management) or SIGBUS.
  - **Mid-checkpoint crash.** The daemon crashes between checkpoint commit write and transition-record sibling-file write (execution-model §4.4 failure mode). Is this code 10 (disk-full) or code 7 (git-bad-state) or does it escape to generic-failure?
  - **Crash during graceful drain (after step 1, before step 7).** ON-027 says "orchestrator exits with code 0 if clean, or the exit code for `drain-timeout-escalated` per §8 if any step exceeded its bound." What if a step didn't *time out* but *errored*?

- **What the spec says** — §8 table. ON-003 obligates a startup failure-mode catalog for startup failures; no equivalent obligation for runtime failures.

- **Is the justification adequate?** — no. Code 1 (`generic-failure`) is explicitly a "rare fallback; presence in a release indicates missing taxonomy entry" (§8 note). But the spec has no obligation to add taxonomy entries for runtime panics, signal-based terminations, or drain-step errors — so code 1 will absorb all of them silently.

- **Stronger alternative** — add at least three taxonomy entries and an obligation for panic coverage:
  - Code 19: `runtime-panic` — unrecovered Go panic; emit `daemon_crashed` event post-restart; operator remediation: file incident with stack trace.
  - Code 20: `signal-terminated` — killed by a non-INT/TERM signal; sub-category via event payload (SIGSEGV, SIGBUS, SIGABRT).
  - Code 21: `drain-step-errored` — distinct from `drain-timeout-escalated`; a drain step produced an error other than timeout.
  - Add ON-027b: "The daemon MUST install a panic handler at process-main entry that emits a `daemon_panic` event carrying the stack trace to the dead-letter log before exiting with code 19."
  - Add a cross-reference: code 0 through 21 are "observable via `harmonik list` post-restart" — the operator can distinguish a crash (code 19) from a missing-prereq (code 2–10).

- **Load-bearing level** — **important**. Without these, the failure-mode catalog (ON-003) has a runtime sibling that is unenumerated, and `harmonik status` has no vocabulary for crash-cause diagnosis.

### Challenge 4 — Queue-format compatibility: "Beads is the queue" conflates three distinct compat surfaces

- **Challenge** — ON-015 says "Beads is the queue; overlay schema is harmonik's half" and names three overlay points: the `Harmonik-Bead-ID` trailers in checkpoint commits, the bead-ID references in events, and the session-log bead-ID metadata. Each of these has a different compatibility story:
  - Commit trailers are written-once, read-many, live in git; readers MUST understand trailer shape across versions.
  - Event references are written-once, read-many, live in JSONL; readers MUST understand event payload shapes.
  - Session-log metadata is written-once, read-many, lives in workspace files; readers MUST understand the session-log format.
  - **Beads SQLite schema** is a fourth surface, managed upstream, version-pinned per ON-017.

  ON-018 (N-1 compat) lumps these into a single invariant but each has its own version field, its own writer, and its own consumer. The "queue-format compatibility" language obscures that this is a union of four independent compat contracts.

- **What the spec says** — ON-015: "the union of (a) Beads schema compat (managed upstream) AND (b) harmonik's overlay schema compat." ON-016: startup checks "both the Beads SQLite schema version and harmonik's overlay schema version against the running binary's supported set (current N and prior N-1)." ON-018 extends to "Every versioned on-disk or wire artifact declared by foundation specs." §8 code 2 is `queue-format-unsupported` (single code for both halves).

- **Is the justification adequate?** — no. Three symptoms:
  - **A single exit code hides which surface failed.** "queue-format-unsupported" does not distinguish "Beads schema too new" from "harmonik overlay trailer unrecognized" from "event payload references a bead-ID format from a future version."
  - **"N-1 readable" has different meaning per surface.** Readers of commit trailers can tolerate unknown trailers (strip-unknown is legal per git). Readers of SQLite cannot tolerate schema mismatch without a migration. Readers of event JSONL can additive-tolerate unknown fields per event-model §4.7.
  - **The overlay schema version is unnamed.** Where is the overlay version stored? The spec names commit trailers, event references, and session-log metadata as overlay points, but does not say which field on which surface holds the overlay version number. Beads's own version is in its SQLite schema; harmonik's overlay version has no declared home.

- **Stronger alternative** — either:
  - (a) Split ON-015 into four requirements (Beads-schema, commit-trailer-shape, event-reference-shape, session-log-format) each with its own N-1 contract and an individual exit code when violated; OR
  - (b) Add a `Harmonik-Overlay-Version` trailer to checkpoint commits and declare it as the single versioned surface for the overlay half, with explicit cross-references from event-model and workspace-model confirming they consume this version. Keep ON-016's startup check simple, but have ON-018's N-1 contract key on this single version.

  Option (b) is simpler and localizes the overlay-version concept. Either way, "N-1 readable" needs a per-surface expansion in §3 or §4.4.

- **Load-bearing level** — **important**. The spec names "compat contract" but the implementer has four contracts to satisfy with no single test.

### Challenge 5 — Schema compatibility window: ON-018 generalizes over artifacts whose own specs declare incompatible contracts

- **Challenge** — ON-018 asserts "Every versioned on-disk or wire artifact declared by foundation specs … MUST maintain N-1 readability." The artifacts enumerated are event-envelope schema, event payload schemas, checkpoint trailers + sibling files, queue overlay, policy schema. But event-model's own §4.7 (Schema versioning) declares per-type additive rules, and execution-model's schema-version trailer (EM-022 in that spec) has its own N-1 contract narrower than ON's. ON is either the parent contract that EV and EM are derived from, or ON is a separate contract that duplicates (and risks drifting from) EV's per-type rules.

- **What the spec says** — ON-018 treats every artifact as subject to a single N-1 rule. ON-INV-001 elevates this to an invariant. §A.3 rationale ("Why N-1 and not N-2 or wider") justifies the window but does not clarify whether ON-018 is derivative or original.

- **Is the justification adequate?** — no. Two resolution paths, neither chosen:
  - ON is the parent contract; EV and EM inherit "N-1 readable" from ON-018 and their per-spec mentions are informative redeclarations.
  - ON is an operator-facing summary of per-spec contracts; the real contracts live in EV/EM and ON just cross-references them.

  The difference is load-bearing. Under (a), a post-MVH EV extension that relaxes to "N-2 readable for event payloads" also relaxes ON-018 — or ON-018 over-constrains EV. Under (b), ON-018 is a summary that must be kept in sync with EV/EM.

- **Stronger alternative** — declare ON as the parent contract explicitly: "ON-018 is normative for the N-1 window; defining specs (event-model, execution-model, control-points, beads-integration) are normative for the version-field location, increment policy, and the per-artifact semantics of 'readable.' A defining spec MAY declare a narrower per-artifact contract (e.g., additive-only) but MUST NOT declare a wider one without a foundation amendment." Move §6.4's "schemas referenced" language to reflect this.

  Alternatively, demote ON-018 to a summary cross-reference and let EV/EM own the contract. Pick one.

- **Load-bearing level** — **important**. The ambiguity will surface the first time an EV change proposal asks "does ON need to bump?"

### Challenge 6 — RTO target X is a placeholder, not a number with a sensor

- **Challenge** — ON-031 reads "Restart MUST reach the pre-restart state within **X seconds**, measured from SIGTERM (or crash) to the daemon emitting the `ready` status event per [process-lifecycle.md §8.2]. The target X MUST satisfy all three criteria of §4.8.ON-032 simultaneously." ON-032 supplies three criteria: 30s p95 nominal, reconstruction complexity proportional to git-log walk + Beads query, 300s hard ceiling. The 300s ceiling is numeric and testable; the 30s p95 is conditional ("under nominal conditions (≤ a few hundred open beads, ≤ a few dozen in-flight runs)") with the word "MAY be relaxed with reason if measurements show 30 seconds is unachievable at MVH scale." There is no declared sensor that measures X, no declared fixture that reproduces "nominal conditions," and no rule for what "measured" means (every restart? rolling p95 over what window?).

- **What the spec says** — ON-031 — X seconds. ON-032 — three criteria, the first of which is conditionally hand-wavy. ON-033 — RTO "MUST be measured from SIGTERM (or daemon crash timestamp recorded by the OS) to the daemon's `ready` status event emission per [process-lifecycle.md §8.2]."

- **Is the justification adequate?** — no. The exemplar guidance in the critic-reviewer charter is explicit: "aggressive targets don't excuse hand-wavy RTO — ON must name numeric targets with sensors." Three specific gaps:
  - **No sensor.** Which subsystem records the SIGTERM timestamp? Which subsystem records the `ready` event timestamp? What is the measurement rig for p95 (rolling window? last N restarts?)?
  - **"Nominal conditions" is a moving target.** "A few hundred open beads" is not a bound; "a few dozen in-flight runs" is not a bound.
  - **p95 is a distribution statistic; one-off restart events cannot be measured against p95.** Either the 30s is a SLO over rolling restarts (and needs a window), or it's a target a single restart should reliably hit (then p95 is the wrong statistic).

- **Stronger alternative** — bind X to a specific value and a specific measurement rig:
  - "ON-031 — Restart MUST reach the pre-restart state within **30 seconds** under the nominal-conditions fixture defined below, and within **300 seconds** under any state within MVH scale. The SIGTERM-to-`ready` interval is recorded by the daemon-startup event pipeline; the 30-second SLO is evaluated over a rolling window of the last 50 restarts across the fleet."
  - Add a `nominal-conditions fixture` definition in §3 or §6.1: "≤ 500 open beads, ≤ 50 in-flight runs, git-log walk depth ≤ 10,000 commits, disk I/O latency ≤ 10ms p95."
  - Add a sensor requirement ON-032b: "The daemon MUST emit a `restart_rto` metric as part of the `daemon_ready` event payload, carrying `sigterm_timestamp_ms`, `ready_timestamp_ms`, and `reconstruction_path_breakdown`."

- **Load-bearing level** — **important**. Without numeric + sensor, the RTO is wish-ware. The 300s hard ceiling is the one thing a tester can write; everything else is aspirational.

### Challenge 7 — Integrity gate for binary install: commit-hash check has no named verifier, no named source of expected hash

- **Challenge** — ON-005 ("Commit-hash integrity gate") says "the to-be-installed binary's source-commit hash must match the operator-supplied expected hash." Three gaps:
  - **Who computes `actual_hash`?** §7.3 pseudocode says `actual_hash = compute_commit_hash(new_binary_path)` but does not specify the procedure. Is it "git log on the binary's source tree"? "The git commit embedded in the binary via ldflags"? "The binary's sha256"?
  - **Where does the operator get `expected_hash`?** The spec says "operator-supplied" but does not say how the operator obtained it (checked the upstream release page, ran `git rev-parse HEAD`, trusted a Slack message).
  - **Who is the verifier?** The fail-closed behavior is clear; the verifier's trust model is not. If the daemon computes `actual_hash` from the binary itself, a compromised binary can report any hash.

- **What the spec says** — ON-005 (line 112): "The pause-to-upgrade path … MUST verify the to-be-installed binary's source-commit hash against an operator-supplied expected hash before the daemon's exec-replacement step." §A.3 rationale says "commit-hash match" is the MVH gate; full signing is post-MVH. §7.3 pseudocode at line 513 calls `compute_commit_hash(new_binary_path)` but doesn't name how.

- **Is the justification adequate?** — no. The MVH gate is weaker than "integrity" suggests. A commit-hash check where (a) the daemon computes the hash from the binary (vulnerable to a malicious binary lying), (b) the operator supplies the hash from an unverified source, and (c) the binary is installed from an arbitrary path — is closer to "version check" than "integrity check."

- **Stronger alternative** — tighten ON-005 with three sub-requirements:
  - ON-005a: "The expected commit-hash source MUST be one of (a) the binary's repo at a known path verifiable by the operator (`git rev-parse HEAD` on the repo), or (b) an explicit `--expected-hash` CLI flag. The daemon MUST record which source was used in the `operator_upgrading` event payload."
  - ON-005b: "The daemon MUST compute `actual_hash` from the build-time embedded ldflags stamp, NOT from the binary file contents. Binaries without an ldflags stamp MUST fail the integrity gate with exit code 14."
  - ON-005c: "The commit-hash check is a version-identity check, not a cryptographic integrity check. Operators relying on the MVH gate for supply-chain security MUST treat ON-006 (deferred signing) as a known gap."

- **Load-bearing level** — **important**. Without this, "integrity gate" is a misleading name. ON-006 already acknowledges signing is post-MVH; ON-005 should be explicit that it is not an integrity guarantee but a version-identity guarantee.

### Challenge 8 — Multi-daemon deferral (§4.10) is coupled elsewhere in ways the spec does not acknowledge

- **Challenge** — §4.10 claims multi-tenancy is "explicitly deferred post-MVH" (ON-042) and that per-project daemon isolation is the MVH answer. But three concrete couplings in the spec assume single-daemon-per-machine and will break in multi-daemon:
  - **Machine-level agent-subprocess ceiling (ON-041).** The spec obligates a "cross-daemon bound on concurrently running agent subprocesses enforced by a shared lock or a machine-level coordinator process." This is a multi-daemon coordination mechanism inside a spec that says multi-tenancy is deferred. OQ-ON-003 acknowledges the ambiguity but defaults to "filesystem-based shared-counter lock at `~/.harmonik/machine-ceiling.lock`" — which is itself multi-daemon infrastructure.
  - **`harmonik list` (ON-041).** Enumerating running daemons "machine-wide with project path, pid, socket path, and current status" requires a machine-level registry. Where does this registry live? Daemon writes to a shared directory? Socket-scan? Pid-file scan? No requirement names the mechanism.
  - **Upgrade exec-replace (ON-020, §7.3).** "Exec-replace (same socket path; clients retry)" assumes no other daemon is racing on the same socket path. In single-daemon-per-project this holds; across machine there can be N daemons. The upgrade contract implicitly assumes one daemon at a time per project path; that assumption is not stated.

- **What the spec says** — ON-042: "Per-project daemon isolation (one daemon per project per [process-lifecycle.md §8.1]) is the MVH answer to multi-tenancy. Per-tenant cost attribution is out of scope for MVH; running N daemons does not auto-partition costs." §A.3 rationale: "What it does NOT address — shared LLM quotas, shared skill installations, shared operator identity — is not tractable at MVH without a machine-level coordinator that would itself need a process-lifecycle contract." OQ-ON-003: coordinator-vs-lock is unresolved.

- **Is the justification adequate?** — no. The spec both claims multi-tenancy is deferred and specifies a multi-daemon machine-ceiling mechanism. Two disjoint shapes:
  - If multi-tenancy is truly deferred, remove ON-041's machine-ceiling obligation; let each daemon cap its own subprocess count and accept over-allocation when N daemons run.
  - If a machine-ceiling is actually needed (because Anthropic's per-account quota is real — see ON-042 bullet 1), acknowledge that MVH has a small multi-daemon coordinator (the shared lock) and document its lifecycle, failure modes, and reconciliation protocol explicitly.

- **Stronger alternative** — explicitly pick:
  - (a) Remove ON-041's machine-ceiling obligation from MVH; add an informative note that a ceiling is desirable post-MVH. `harmonik list` stays as a socket-path-scan utility.
  - (b) Keep the machine-ceiling; add ON-041b defining the lock file's location, lifecycle, failure mode ("lock file orphaned from a crashed daemon"), and recovery procedure. Add a cross-reference from process-lifecycle's startup sequence to acquire/release the lock. Resolve OQ-ON-003 with coordinator-vs-lock chosen.

  The current state — obligation named, OQ open, default "filesystem-based lock" with no lifecycle — is the worst of both.

- **Load-bearing level** — **important**. The machine-ceiling is declared normative (ON-041) with exit code 18 (`machine-ceiling-exhausted`) allocated in §8. An implementer writing the enforcement mechanism does not know which shape to code against.

### Additional Challenge 9 — Resource budgets: declared, enforced, attributed — but no default caps, no exhaustion protocol, no attribution schema

- **Challenge** — §4.11 obligates budget declaration, dispatch-time enforcement, and per-run/per-role/per-workflow/per-instance attribution. Three gaps:
  - **No default caps.** The spec says budgets are "declared in policy per [control-points.md §6.5]" — but what if policy does not declare a budget? Is there an unbounded default? A conservative default? A fail-closed "must-declare" rule?
  - **No exhaustion protocol.** ON-046 says "Budget-threshold events (`budget_warning`, `budget_exhausted`, `budget_accrual`) MUST be operator-observable." But what does `budget_exhausted` trigger? Does the run terminate immediately? Does it complete the current transition and then terminate? Does it emit the event and continue?
  - **No attribution schema.** "Attributed in observability per run, per role, aggregated to per-workflow and per-harmonik-instance" — but attribution has to live on some event payload. Which payload carries the per-run/per-role breakdown? Is it on every event (overhead) or on a periodic summary event (latency)?

- **What the spec says** — ON-045, ON-046. Cross-references to `[control-points.md §6.9]` for budget control point.

- **Is the justification adequate?** — no. Three loose ends that the spec acknowledges but does not close. Runtime behavior on budget exhaustion is the most operationally load-bearing — the operator needs to know whether `budget_exhausted` is a soft warning or a hard termination.

- **Stronger alternative** — add three requirements:
  - ON-045a: "Runs with no budget declared in policy MUST receive a default budget: token = 100,000; wall-clock = 3600 seconds; iterations = 100. The defaults are part of the config inventory per ON-004 and are operator-configurable."
  - ON-046a: "On `budget_exhausted`, the run MUST proceed to its next durable checkpoint (ON-008-equivalent behavior), emit `budget_exhausted`, and terminate with class `budget_exhausted` per [execution-model.md §8]. The in-flight agent subprocess MUST be signaled SIGTERM with a bounded window matching `stop --immediate` semantics per ON-028."
  - ON-046b: "Budget attribution is carried on the `run_completed` / `run_failed` event payload as a typed `budget_consumption` record (token count, wall-clock elapsed, iteration count). Per-workflow and per-instance aggregates are derived from these records; no separate attribution stream is introduced."

- **Load-bearing level** — **important**. Resource budgets are one of the named in-scope items (§2.1); without these three mechanisms the budget surface is declarative, not operational.

---

## Cross-reference audit (systemic)

Citations in the spec body resolve incorrectly or inconsistently in at least the following places. This is a corpus-wide drift, not a single typo; fixing it requires a section-renumbering walk.

- **[event-model.md §3.1]** — cited at lines 438, 568 for "Event envelope." Event-model's §3 is Glossary; envelope is §4.1 (normative) and §6.1 (RECORD).
- **[event-model.md §3.2]** — cited at lines 98, 112, 165, 180, 236, 315, 340, 352, 378, 398, 438, 442, 451–461, 568 for "event payload registry" or "event types." Event-model's payloads are at §6.3 with the taxonomy at §8.
- **[event-model.md §3.4]** — cited at lines 104, 282, 418, 442, 495, 569 for "fsync policy." Event-model's fsync is at §4.4.
- **[event-model.md §3.5]** — cited at line 71 for "N-1 compatibility" and line 540 for "Event envelope or payload schema version." Event-model's schema versioning is at §4.7.
- **[event-model.md §3.6]** — cited at line 282 for "JSONL not replayed." Event-model's replay is at §4.5 (and the no-DTW rule is at §4.5 or §5 INV).
- **[event-model.md §3.7]** — cited at line 418, 570 for "dead-letter log." Event-model's dead-letter is at §4.3 (bus taxonomy) and §7.2 (protocol).
- **[event-model.md §3.8]** — cited at lines 321, 571 for "structured log schema." Event-model's structured logs do not appear at §3.8; a scan of event-model's outline shows no §3.8.
- **[process-lifecycle.md §8.1]** — cited at line 364, 585 for "per-project daemon scope." PL's per-project daemon scope is §4.1.
- **[process-lifecycle.md §8.2]** — cited at lines 98, 146, 283, 288, 307, 483, 524, 586 for "startup sequence." PL's startup is §4.2.
- **[process-lifecycle.md §8.3]** — cited at lines 92, 398, 587 for "command surface." PL's command surface is §4.10.
- **[process-lifecycle.md §8.4]** — cited at line 104, 588 for "queue-empty behavior." PL's queue-empty is §4.4 (PL-013 inside Shutdown subsection).
- **[operator-nfr.md §7.1]** — PL cites this (lines 141, 164) for "startup failure-mode catalog." ON-003 puts the catalog obligation at §4.1, not §7.1.
- **[operator-nfr.md §7.3]** — PL cites this (lines 197, 314) for "operator-control state machine / §7.3." ON's state machine is at §7.1, not §7.3.
- **[operator-nfr.md §7.5]** — PL cites this (lines 299, 314) for "harmonik upgrade contract." ON's upgrade contract is §4.6, not §7.5.

**Implication.** The drift is bidirectional — ON points to neighbor specs' old section numbers AND neighbor specs point to ON's old section numbers. Someone did a rename pass on ON's §4.N but neither updated ON's outbound citations nor the neighbors' inbound citations. The revision history at line 683 claims "Migrated legacy architecture.md citation anchors" but only for architecture.md; event-model and process-lifecycle were not touched.

**Stronger alternative.** Run a corpus-wide citation audit as a single cleanup pass before ON advances to `reviewed`. Do not fix by hand — the drift spans at least three spec pairs (ON↔EV, ON↔PL, ON↔HC). A simple linter that parses `[spec-id.md §N.N]` and checks that the target section heading exists in the target spec would catch all of these.

---

## Scope leaks — ON declaring WHAT another spec owns

Two §4 requirements declare content that belongs in other specs per their declared scope:

1. **ON-026 (Prompt-injection defense, line 252).** ON-026 says "Input sanitization for user-provided content in the input workspace MUST be the handler's responsibility per [handler-contract.md §4.1]. Handlers MUST NOT let user-provided content in the input workspace alter the agent's system-prompt instructions." The first sentence is a cross-reference; the second sentence is a NEW normative constraint on handlers ("MUST NOT let user-provided content … alter the agent's system-prompt"). Handler-contract §4.1 does not (as of its current draft) declare this rule; ON is introducing it here. Either handler-contract owns the rule (ON-026 is a cross-reference only) or ON owns it (and handler-contract cross-references to here). Pick one.

2. **ON-038 (Audit records, line 338).** ON-038 says "Audit records MUST be produced as a subset of transition records per [execution-model.md §4.4]: the subset where `actor_role` is in a privileged role (per [architecture.md §4.8]) AND the `chosen_action` affected policy, role permissions, or budget." The subset predicate (`actor_role ∈ privileged ∧ chosen_action ∈ {policy, role, budget}`) is a new query definition. It's not owned by execution-model (which owns the transition record shape, not the audit-subset query) or architecture (which owns the role taxonomy, not the audit predicate). ON implicitly owns it — but the §2 scope does not list "audit-record query definition." Either add to §2.1 ("Audit-record query predicate over transition records") or move the predicate into execution-model.

Minor: ON-027 step 4 says "event bus flushes pending events (fsync per [event-model.md §3.4])" — event-model owns the fsync policy; ON owns the ordering of the step relative to other steps. Not a scope leak, but a reader might think ON is declaring fsync semantics.

---

## First-plausible-answer findings

Four places where the subagent picked a plausible answer without evidence of a comparison:

1. **ON-031's 30s / 300s pair.** The numbers are cited to "[problem-space.md] recon findings" in §A.3 (line 695, "300s matches operator-patience research"). A grep of problem-space.md does not surface an "operator-patience research" section (I did not re-verify, but the §A.3 cite is the only one). 30s / 300s is a defensible pair but "first plausible" — a 10s nominal with 60s ceiling, or 60s nominal with 600s ceiling, would be equally defensible without the spec explaining why 30/300 is the chosen pair beyond "operator patience."

2. **Exit code 1 = `generic-failure` as a rare fallback.** Unix convention is exit code 1 = generic error; exit codes 2–255 = specific categories. ON follows convention. But ON's code 1 is "MUST be rare; presence in a release indicates missing taxonomy entry." Three other shapes are possible and not compared:
   - Reserve code 1 for argument-parsing errors (common CLI convention).
   - Reserve codes 1–64 for OS-signaled failures (128+signal convention).
   - Use code 255 for generic-failure to free 1 for parse errors.
   The spec picked "code 1 = fallback" as first-plausible without comparing alternatives.

3. **OQ-ON-003 default: filesystem-based shared-counter lock at `~/.harmonik/machine-ceiling.lock`.** The default is picked without a comparison to alternatives: a coordinator daemon, a systemd socket-activated helper, a shared-memory counter. The default "revisit if contention measurements show thrash" is a wait-and-see rule, which is fine, but the spec doesn't name the thrash-detection threshold or the sensor that would flag it.

4. **ON-007's "operator task = execution-model run" mapping.** The spec picks "operator-facing copy says 'task'; specs say 'run'" (line 127). This is a user-friendliness call. An alternative — align surfaces with specs ("task" in both, since "run" is a term-of-art and operators already know "task" from other systems) — is not compared. The translation obligation on every operator surface ("surfaces that render human-facing copy MAY translate") is a real maintenance burden, and the rationale for keeping two terms is left implicit.

---

## Invariant audit — §5

Per the template's selection test (§5): "an invariant is a system-wide property that constrains multiple subsystems' requirements. If the rule fits inside one subsystem's §4 without reference to others, it is a requirement, not an invariant."

- **ON-INV-001 — N-1 compat window holds across every versioned artifact.** Genuine cross-subsystem invariant — constrains event-model, execution-model, control-points, beads-integration together. **HOLDS**, but note the Challenge 5 ambiguity about whether ON owns or consumes the N-1 rule. Sensor: no named sensor; consider adding "CI MUST run a cross-spec schema-compat lint that fails the build if any writer produces an artifact a prior-release reader cannot parse."

- **ON-INV-002 — No PR-gated rollout for MVH.** This is an *operational* invariant — it constrains how the team builds the system, not how the system runs. The template's selection test is about runtime properties that span subsystems. INV-002 is really "a constraint on the subsystem spec authors." It does not have a runtime sensor, cannot be violated by a running system, and does not constrain any §4 requirement in this spec. **FAILS** the test. Move to §A.4 (Migration notes) or to a §2.1 "assumption" bullet. It is important information; it is not an invariant.

- **ON-INV-003 — Secrets never appear in durable sinks unredacted.** Arguably cross-subsystem (event log, dead-letter log, session log all touched). Restated from ON-022 + ON-023 + handler-contract's redaction rule. **HOLDS borderline** — the cross-subsystem scope (three distinct sinks owned by three specs) makes it genuinely an invariant rather than a single-spec requirement. Sensor: "at release time, a grep over test-corpus logs for HARMONIK_SECRET_* prefixes MUST return zero."

- **ON-INV-004 — Between-task invariant covers pause, upgrade, and improvement-pause.** This is ON-008 + ON-020/021 + ON-012 restated with an OR-clause. All three requirements are inside this spec's §4.3 and §4.6. **FAILS** the test — it's a restatement of three §4 requirements, not a cross-subsystem property. Delete, OR rewrite as a cross-subsystem claim: "No subsystem MAY introduce a control-surface that bypasses the between-task invariant." The latter is cross-subsystem; the current form is §4 spelled louder.

- **ON-INV-005 — Restart RTO hard ceiling is non-negotiable.** Restates ON-032 criterion 3. **FAILS** the test — entirely inside ON-032. Delete the §5 copy, OR promote to cross-subsystem: "Every subsystem MUST report its reconstruction contribution to the aggregated RTO; any subsystem whose reconstruction exceeds 60 seconds is a bug against this invariant." The latter is cross-subsystem and testable.

**Summary.** Three of five invariants (INV-002, INV-004, INV-005) fail the selection test and should be deleted or rewritten. INV-001 holds with a sensor gap. INV-003 holds borderline.

---

## Observability envelope concreteness

§4.9 (ON-034 through ON-040) declares six required signal classes:
- Typed events (ON-034).
- Structured logs (ON-035).
- Health-check interface returning `{OK, degraded, failed}` (ON-036).
- Liveness heartbeats (ON-037).
- Audit records as transition-record subset (ON-038).
- Mechanism-tagging of every observability op (ON-039).

Concreteness scorecard:

- **Events.** ON-034 says "every subsystem MUST emit events per event-model." Event taxonomy itself is rich (event-model §8 enumerates 40+ event types). Concrete.
- **Structured logs.** ON-035 says "structured logs per event-model §3.8." Event-model has no §3.8 (see cross-ref audit). This signal class has no schema declared in the current corpus. **Aspirational.**
- **Health-check.** ON-036 names `health_status ∈ {OK, degraded, failed}` inline. What does the health-check interface method signature look like? When is it called — every heartbeat? every status query? on demand? Is `degraded` a terminal state or can a subsystem recover to `OK` without restart? **Under-specified.**
- **Heartbeats.** ON-037 says "defined cadence" and "operator-configurable tolerance." No default cadence. No default tolerance. "Missing heartbeats beyond tolerance MUST trigger a `degraded` classification" — but for how long missing? **Under-specified.**
- **Audit records.** ON-038 defines the subset predicate. The predicate is concrete. But the consumer-side story — how does an operator query audit records? what's the access pattern? — is unaddressed. **Partially concrete.**
- **Mechanism-tagging.** ON-039 is a discipline rule, not a signal. Concrete as a constraint.

**Summary.** Two of six signal classes (structured logs, heartbeats) are aspirational in their current form. Health-check is under-specified. Audit is half-concrete. Events are the only fully-specified signal class, and that's because event-model does the work.

**Stronger alternative.** Add three missing pieces:
- ON-035b: "Structured log records MUST carry the fields `timestamp`, `subsystem`, `level`, `event_correlation_id`, `run_id` (if scoped), `message`, and `fields` (typed map). Schema version in [event-model.md §4.7]."
- ON-036b: "Health-check MUST be callable at any time via `harmonik status --subsystem <id>`; the response latency MUST be ≤ 100ms p95; degraded→OK recovery requires the subsystem to assert recovery via a state event."
- ON-037b: "Default heartbeat cadence is 10 seconds; default tolerance is 3 missed heartbeats (30 seconds)."

**Sensor proposals to tie observability to testable invariants:**

| Invariant | Sensor | Where to read |
|---|---|---|
| ON-INV-001 (N-1 compat) | CI lint: cross-spec schema-compat test writing at N, reading at N-1, failing on mismatch | `make check-compat` in build-practices three-tier gauntlet |
| ON-INV-003 (no secrets in durable sinks) | Post-test grep over JSONL/session-log/DLQ corpus for `HARMONIK_SECRET_*` prefixes; fail CI on any match | `scripts/redaction-gate.sh` — analog of `coverage-gate.sh` |
| ON-INV-005 (300s RTO ceiling) | Restart-scenario benchmark with SIGTERM-injection fixture; assert p95 ≤ 30s, p100 ≤ 300s | `scripts/rto-gate.sh`; part of tier-3-slow per project-level/quality-checks |
| ON-027 (drain ordering) | Scenario harness injects failures at each drain step; verify ordering-preserved via emitted event sequence | Twin-subprocess scenarios per [handler-contract.md §4.8] |
| ON-037 (heartbeat cadence) | Daemon-level assertion: for every subsystem, time-since-last-heartbeat MUST be ≤ tolerance; violation emits `subsystem_degraded` | In-process check emitting into event log |

The table is concrete enough for a test engineer to start writing fixtures. The spec's current §10.2 prose-obligations are aspirational by comparison.

---

## MUST/SHOULD discipline

Eight places where keyword choice is wrong or where permissive language hides a real requirement:

1. **ON-001 "stable across releases within the N-1 compatibility window."** "Stable" is used without naming what changes. Does adding a new code break stability? (Probably not — §8 says new codes may be added.) Does changing a code's category break stability? (Yes, but this is implicit.) Clarify: "a given non-zero code MUST refer to the same category across releases within the N-1 window; introducing a new code MUST NOT repurpose an existing code's meaning."

2. **ON-027 step 3 "wait for handler subprocesses to complete or reach the drain timeout."** "Or" is ambiguous — is this the worker-level timeout (single subprocess) or the step-level timeout? ON-029 says "the drain timeout (the bound on steps 2 and 3)" — so it's a shared bound on two steps. Then how is the bound partitioned between step 2 and step 3? The pseudocode at §7.2 shows `timeout.step_2` and `timeout.step_3` as separate fields, contradicting the prose. Pick one.

3. **ON-037 "Missing heartbeats beyond tolerance MUST trigger a `degraded` classification."** What triggers the transition back to `OK`? Re-appearance of a heartbeat? A specific event? The spec has no recovery rule, which is a gap for health-check freshness. Add ON-037b: "On resumption of heartbeat emission within a bounded recovery window, the subsystem MUST emit a `subsystem_recovered` event and transition back to `OK`. Recovery MUST be event-driven, not inferred from heartbeat absence."

4. **ON-042 "Deferred here means not solved by per-project-daemon isolation; it does NOT mean dismissed."** Informative prose mixing with normative. The "does NOT mean dismissed" clause has no normative force — it's a stance. Move to §A.3 (rationale).

5. **ON-008 "allow each in-flight run to proceed to its next durable checkpoint."** "Allow" is permissive — is the daemon obligated to allow, or is this descriptive? In the worst case, an agent is deep in a 30-minute tool call with no durable checkpoint visible ahead; the daemon has to either wait (honoring "allow") or force-terminate at some timeout. The spec gives no bound on how long "allow" lasts. Add ON-008b: "The daemon MUST apply a bounded wait (operator-configurable, default 300 seconds matching the RTO hard ceiling) for each in-flight run to reach its next durable checkpoint. Runs exceeding the bound MUST be treated as stop-immediate per ON-028 with an `operator_pause_escalated_to_stop` event."

6. **ON-013 "The daemon MUST emit one typed event per operator-control state transition."** "One typed event per state transition" is not honored by the §7.1 table — several transitions emit NO event (`resuming` → `running` at line 477 shows em-dash "—" under Emits). Either (a) every transition MUST emit exactly one event (then `resuming` → `running` needs one), or (b) ON-013 says "one event per state transition WHERE an event is listed." Tighten the table or loosen the requirement.

7. **ON-022 "Secrets MUST NOT appear in the event log under any circumstance."** "Under any circumstance" is absolute; a subprocess's stdout captured into a session log and inlined into an event payload could carry secrets. The requirement names the obligation but does not say who enforces it at the point of inlining. ON-023 addresses payload-schema time; this leaves a runtime gap for payloads that carry `arbitrary_text` fields. Either forbid `arbitrary_text` fields in payloads (compile-time enforceable) or declare a runtime scrubber obligation.

8. **ON-018 "a reader pinned to version N-1 MUST successfully parse and interpret artifacts written by version N, with additive fields treated as unknown but non-fatal."** "Interpret" is load-bearing and undefined. Does interpret mean "the semantic action triggered by the new version's additional fields is attempted"? (Then the reader must understand the field.) Or "the additional fields are seen as opaque, and the reader proceeds with its pre-N semantics"? (Then "interpret" is too strong.) Tighten to: "A reader pinned to version N-1 MUST parse artifacts written by version N without error. Unknown fields MUST be preserved on read-modify-write paths (no strip-unknown), ignored on read-only paths, and MUST NOT cause the reader to fail."

---

## Definitional gaps

Terms used heavily in normative text but not defined rigorously:

- **"In-flight run"** (14 uses) — see Challenge 1. Glossary provides "between-task invariant" but not the constituent predicate.

- **"Durable checkpoint"** (ON-008, ON-021, ON-027, §7.1, and §A.3) — deferred to [execution-model.md §4.5], which per execution-model-r1 critic is itself circular. The operator-nfr spec inherits this circularity. ON cannot safely route on a predicate that execution-model has not nailed down.

- **"Operator"** — the spec uses "operator" as a role throughout without naming who/what an operator is. Is an operator a human at a terminal? A script calling `harmonik` commands? An orchestrator-agent (another LLM session per [process-lifecycle.md §4.6])? The `harmonik confirm-verdict` / `harmonik veto-verdict` commands of ON-014 imply human judgment; auto-resume of improvement-pause (ON-012) implies programmatic. The role taxonomy in [architecture.md §4.8] may cover this; ON should cross-reference the definition.

- **"Migration release"** (ON-019) — "any release that bumps an N-1-covered schema version to break the compat window — i.e., a change no longer readable by readers at the current N." This is a one-sentence inline definition inside the requirement body; it belongs in §3. Also silent on whether a migration release can bump multiple schema versions simultaneously or must be scoped to one artifact.

- **"Next checkpoint"** (ON-008, ON-027, §7.1) — "the next durable checkpoint per [execution-model.md §4.5]." If a run has never emitted a checkpoint (brand new run in a long-running tool call), what is the "next" checkpoint? The first one? What if the agent never emits a checkpoint at all (hangs)? See MUST/SHOULD item 5.

- **"Machine-level"** (ON-041, ON-042) — the machine boundary is implicit (one OS, one hostname, one kernel). In a container world, is a Docker container a "machine"? A pod? The machine-ceiling lock at `~/.harmonik/machine-ceiling.lock` names a filesystem path — which means the boundary is "shares `$HOME`." That's a container-compatible definition but the spec should say so.

- **"Agent subprocess"** (ON-041, ON-044, ON-028) — Handler-contract defines handler subprocesses; are agent subprocesses the same thing or a superset? ON uses both phrases. Pick one.

- **"Nominal conditions"** (ON-032) — see Challenge 6. "A few hundred open beads, a few dozen in-flight runs" is not a bound.

- **"Prerequisite failure"** (ON-003) — "every daemon-startup prerequisite failure" — the set of prerequisite failures is enumerated in parentheses but the enumeration is non-exhaustive ("etc." implicit). ON-003 says "at minimum" — which means the catalog can be extended, but by whom and when?

---

## Counter-examples — concrete failure modes the spec does not catch

Scenarios an implementer could code up that would pass every requirement in the spec as written and still violate operator expectations:

1. **The 25-minute `paused`.** Operator issues `pause`. A handler subprocess is in the middle of a 30-minute LLM reasoning call with no intermediate durable checkpoint. The daemon is in `pausing`. The operator's TUI shows `pausing` for 25 minutes. Operator escalates to `stop --immediate`. Per the spec this is OK — ON-008's "allow each in-flight run to proceed to its next durable checkpoint" has no bound. Per operator expectation (and ON-032's 300s hard ceiling on RTO, implying operator patience), this is a wedge. Fix: MUST/SHOULD item 5.

2. **The exec-replace race.** Operator issues `pause`. Daemon transitions to `paused` at "last in-flight run reaches next checkpoint" per §7.1 line 473. Operator immediately issues `upgrade`. Daemon validates hash (ON-005), validates schema (ON-019), transitions to `upgrading`, calls `exec_replace`. But event-bus has not flushed (ON-027 step 4), Beads-write has not ack'd, workspace leases are still held. The new binary starts up, its startup sequence (§PL-005 / process-lifecycle) discovers unreleased leases and an event log with non-fsync'd tail entries. Reconciliation Cat 0 fires. Per the spec this sequence is legal — the §7.1 transition requires only "last run reaches checkpoint." Per operator expectation, `upgrade` should succeed cleanly. See Challenge 2.

3. **The code-1 silent broadening.** A daemon release introduces a new runtime failure mode (Go runtime panic on a memory-corruption bug). The daemon exits with code 1. The operator's monitoring dashboard is keyed on exit codes; code 1 is "generic-failure" so the alert fires. The operator investigates. The next release fixes the bug. The release after that introduces a different runtime panic — code 1 again. Operators cannot distinguish the two panics from the exit code. Per the spec this is legal (code 1 is the fallback). Per operator expectation, each distinct failure mode has a code. Fix: Challenge 3.

4. **The two-operator race.** Operator A issues `pause`. Daemon is in `pausing`. Operator B issues `upgrade <hash>` via a separate `harmonik attach`. Per the state machine at §7.1 the `upgrade` transition is `paused` → `upgrading`; from `pausing` this is not a legal source state. So `upgrade` errors with code 13 (`upgrade-requires-paused`). Operator B retries. Meanwhile Operator C issues `stop --graceful`. The sequence is: `pausing` in-progress, then `stopped` (overriding pause per line 482 "any → `stop --immediate`" wait actually `stop --graceful` has a more specific row at line 481 "running → drain → stopped" but we're in `pausing`…). The spec's state machine does not have a `pausing` + `stop --graceful` row. OQ-ON-004 acknowledges concurrent-operator arbitration but punts. This is a real gap — a well-formed table should have a row for every (state × event) pair.

5. **The N-1-readable write-through.** Daemon at version N writes an event payload with a new field `x`. Daemon at version N-1 is running the *same project* (operator downgraded via `harmonik upgrade` at a paused boundary, which is legal for N→N-1 per ON-020(d) "MUST succeed for same and N-1"). N-1 reads the payload, sees `x` as unknown (ON-018 permits), and on its own write emits a new payload WITHOUT `x`. The improvement-loop data pipeline consuming both payloads sees `x` appear and disappear based on which daemon version wrote. Per the spec this is legal. Per operator expectation (data pipeline), this is a regression. Fix: MUST/SHOULD item 8 — preserve unknown fields on read-modify-write paths.

6. **The multi-daemon lock orphan.** Daemon A acquires `~/.harmonik/machine-ceiling.lock`. Daemon A crashes (SIGKILL or host reboot). Lock file is stale. Daemon B starts up, tries to acquire the lock, finds it held, fails. Per OQ-ON-003 default ("advisory locking"), advisory locks are released on process death — but a file at a path is not released; it's just a marker. The spec says nothing about stale-lock detection. Daemon B waits forever OR Daemon B deletes the stale marker (risking race with Daemon A if A was in fact alive). Fix: Challenge 8.

7. **The audit-record ambiguity.** ON-038 says audit records are "the subset of transition records where `actor_role` is in a privileged role AND `chosen_action` affected policy, role permissions, or budget." What if a transition has `chosen_action = modify_budget` but the actor is a non-privileged workflow? Per ON-038 this is NOT an audit record (AND not OR). But operator expectation is that budget modifications are ALWAYS audited. The predicate should be OR, not AND. Fix: rewrite ON-038's predicate.

8. **The silent schema-version advance on startup.** Daemon at version N starts up. On-disk state is at schema version N (same). All prereqs pass. Daemon transitions to `ready`. Two weeks later, the operator installs version N+1 via `upgrade`. The N+1 binary's startup sees on-disk state at version N, which is within its N-1 window (so it accepts per ON-019). But N+1 may silently upgrade the on-disk state to N+1 shape on its first write — is that legal? ON-019 says "Installing a migration release MUST NOT auto-migrate on-disk state." But installing a non-migration release (within the N-1 window) is not covered. Does a non-migration release auto-advance writes to the new schema, or does it continue writing at the older shape until the operator explicitly opts in? The spec is silent.

---

## Hidden assumptions

Things the spec assumes that could turn out wrong:

1. **`harmonik upgrade` is always operator-driven.** ON-020 and §7.3 assume upgrade is initiated by an operator with an explicit command. What about auto-updates (daemon detects new binary in a repo path and offers upgrade)? Not an MVH feature, but the between-task + commit-hash contract was designed around operator initiation. If auto-update is ever introduced, the "operator-supplied expected hash" requirement becomes load-bearing: who supplies the hash in auto-update? §A.3 does not close this door explicitly.

2. **Exec-replace preserves the socket path.** ON-020(e) says "daemon MUST re-bind the same socket path after exec-replace." This assumes the socket path is stored in a location the new binary can read (environment variable? command-line arg? pidfile?). The spec does not say where. Process-lifecycle §4.2 may own this, but the upgrade contract cross-reference does not flag the dependency.

3. **Reconciliation verdict-execution is pause-able.** ON-014 obligates an operator override on verdict execution. This assumes the verdict-execution step is pauseable — i.e., there's a bounded point in the workflow at which the daemon can suspend and wait for operator confirmation. Reconciliation §9.5b is cited; the assumption is that reconciliation workflows have a clean point between "verdict produced" and "verdict applied." If that point doesn't exist in some category (e.g., Cat 6a escalation), ON-014 does not hold.

4. **Per-project daemon never runs on a shared host with strict concurrent-run caps.** The "few dozen in-flight runs" nominal-condition bound (ON-032) assumes a dev workstation, not a shared CI host running 10 harmonik daemons each with 10 concurrent runs. The machine-ceiling (ON-041) addresses this at a machine level but not the per-daemon RTO expectation.

5. **Beads pre-1.0 breakage is a one-time absorption event.** ON-017 says "a Beads breaking change MUST produce one localized adapter update." Beads pre-1.0 is by definition pre-stability; multiple breaking changes are plausible. The spec assumes each is absorbable with one adapter update each. An unstable upstream with breaking changes per week would invalidate this assumption quickly.

6. **Event-bus flush is strictly ordered before memory-index flush.** ON-027 steps 4 and 5. If the memory layer consumes events asynchronously, its indexing may depend on events flushed in step 4. Ordering is correct under that assumption. If the memory layer has a separate path (not event-driven), the ordering is arbitrary. The spec does not state the dependency.

7. **`harmonik list` is run from outside a daemon.** ON-041 says `harmonik list` enumerates running daemons. It does not say whether `harmonik list` requires any daemon to be running (no) or whether it can be run by any user (implied yes). If it can be run by any user, the machine-wide registry it reads is world-readable — which may leak project paths and socket paths across users on multi-user hosts.

---

## Graceful-shutdown vs operator-pause: overlap or orthogonal?

The spec has both `stop --graceful` (ON-027, 7 ordered steps) and `pause` (ON-008 + §7.1 transition to `paused` at "last run reaches checkpoint"). The relationship between the two is not crisply stated. Three interpretations:

- **Orthogonal.** Pause is "stop pulling new, finish in-flight, stay alive"; stop is "finish in-flight, exit." Pause is a state, stop is a terminal. Under this reading, the drain protocol of ON-027 applies to stop, not to pause — and pause's state-machine transition at §7.1 is a *prefix* of the drain protocol (steps 1 + 2, not 3–7).
- **Overlapping.** Pause and graceful-stop share the same drain protocol; stop adds "then exit" at the end. Under this reading, ON-027's 7 steps apply to both; pause just stops at step 7 minus the exit.
- **Nested.** Pause is a subtree of graceful-stop; if you want to stop cleanly, you pause first then exit. Under this reading, `stop --graceful` is really `pause` + `exit` and the state machine should reflect that.

The current spec text is consistent with (a) — ON-027 explicitly applies to `stop --graceful` and SIGTERM — but §7.1's `pausing` → `paused` transition guard ("all runs drained") smells like (b) and the operator expectation (per Challenge 2) is closer to (c).

**Stronger alternative.** Pick one. If (a), add ON-027b: "Pause does NOT execute the full drain protocol; pause completes steps 1–2 of ON-027 but does NOT flush consumer-side state (steps 4–6). Consumer-side state is flushed only at `stop --graceful` or SIGTERM. Runtime observables (event log, workspace leases) MAY be stale during `paused`." If (c), restructure §7.1 so `stop --graceful` is a `paused` transition to `stopped` rather than a direct `running` → drain → `stopped` edge.

---

## Recommendation

**Proceed with revisions.** Complete the following before advancing past `draft`:

1. **Define `in_flight(run)` as a mechanically decidable predicate in §3** (Challenge 1). All between-task requirements route on this predicate.
2. **Reconcile the §7.1 `pausing` → `paused` transition with ON-027 steps 4–6** (Challenge 2) — either extend the transition to require consumer-side flushes or add a `quiescing` intermediate state.
3. **Fix cross-references corpus-wide** (Cross-reference audit). At least three sibling specs (event-model, process-lifecycle, handler-contract) have bidirectional drift.
4. **Delete or rewrite §5 invariants that fail the selection test** (Invariant audit). INV-002, INV-004, INV-005 are restatements of §4 requirements.
5. **Bind RTO target X to a numeric value with a declared sensor and a fixture** (Challenge 6). "X seconds" is a placeholder.
6. **Add runtime-failure exit codes** (Challenge 3). At least: `runtime-panic`, `signal-terminated`, `drain-step-errored`.
7. **Clarify queue-format compat as a union of four per-surface contracts** (Challenge 4), either by splitting ON-015 or by introducing a single `Harmonik-Overlay-Version` field.
8. **Pick the owner of the N-1 window** (Challenge 5): ON as parent contract, or ON as summary of per-spec contracts.
9. **Tighten ON-005's commit-hash gate** (Challenge 7) — name the actual-hash source, the expected-hash source, and the trust model.
10. **Resolve the multi-tenancy-is-deferred-but-machine-ceiling-is-obligated contradiction** (Challenge 8). Pick one shape.
11. **Fill in observability-envelope gaps** (Observability section): structured-log schema cite, health-check interface contract, heartbeat cadence defaults.
12. **Add budget default caps, exhaustion protocol, and attribution schema** (Challenge 9). Without these, resource budgets are declarative, not operational.
13. **Decide graceful-shutdown-vs-pause relationship** (Graceful-shutdown vs operator-pause section). Three candidate shapes; the spec is currently ambiguous.
14. **Complete the §7.1 state-machine table** so every `(state × event)` pair has an explicit row. Currently `pausing` + `stop --graceful` has no row; this gap is where Counter-example 4 (two-operator race) lives.
15. **Define `operator` as a role** with a cross-reference to [architecture.md §4.8] (Definitional gaps).
16. **Bound ON-008's "allow each in-flight run"** — see MUST/SHOULD item 5. The 25-minute `pausing` counter-example motivates this.
17. **Bind ON-037's heartbeat cadence and recovery rule** — MUST/SHOULD item 3, Counter-example 1 (alive-but-silent subsystem classification) would otherwise persist as `degraded` indefinitely.

None of these require architectural re-work. Eleven of seventeen items are "the spec knows the right answer but did not write it down." The remaining six (Challenges 4, 5, 8; Graceful-shutdown decision; ON-006 trust-model; state-machine completeness) require a design choice that the current text evades.

Once items 1–17 land, the spec is ready for r2 reviewer.

---

## Minor findings (non-blocking)

Spot fixes surfaced during the read but not load-bearing enough to list as challenges:

- **Line 167 ON-013 emits list** — six events enumerated, but `operator_upgrade_rejected` is not in that list. ON-013 says "The daemon MUST emit one typed event per operator-control state transition" and lists six. `operator_upgrade_rejected` is introduced separately at ON-005 and registered in §6.5 (line 459). Either add to ON-013's list or note that rejection is not a state transition (both `paused` → `paused`).
- **Line 294 ON-032 "Criterion 1 MAY be relaxed with reason"** — MAY-with-a-condition is the weakest form of normative language. Either specify the condition rigorously or upgrade to SHOULD. "With reason" is placeholder.
- **Line 400 ON-045 "attributed in observability per run, per role, aggregated to per-workflow and per-harmonik-instance"** — the aggregation hierarchy has four levels but no declared aggregation interval. Is this continuous aggregation, per-run snapshot, per-shutdown rollup?
- **Line 542 §8 code 5 `pidfile-locked`** — detection rule says "Another daemon holds the pidfile lock for this project." A stale pidfile (daemon crashed without removing) is a different failure mode. Split into `pidfile-locked` (actively held) and `pidfile-stale` (abandoned) or state that stale-pidfile recovery is automatic per process-lifecycle §4.2 step 1a.
- **Line 554 §8 code 18 `machine-ceiling-exhausted`** — emitted event is `dispatch_deferred`. "Deferred" implies the dispatch will retry; is there a retry cadence? At what point does a deferred dispatch give up? The code is operational but the event-driven retry loop is undeclared.
- **Line 683 revision history** — v0.2.0 summary says "No requirement IDs, invariants, or schemas were touched." This is true for §4.N renames but the cross-references audit shows outbound citations to event-model and process-lifecycle were NOT migrated. The v0.2.0 row overstates what the pass accomplished.
- **§10.1 Conformance profiles** — "Core MVH" is named but no "Extension X" profile is listed even though ON-006 (signing), ON-043 (Prom/OTel), ON-044 (distributed tracing) are candidate post-MVH extensions. The template allows listing these as explicit profiles; doing so would clarify conformance boundaries.
- **§11 Open questions** — five OQs. OQ-ON-001 (config inventory location) and OQ-ON-003 (machine-ceiling shape) have aggressive defaults that are effectively picking an answer without flagging it. Consider upgrading defaults to resolutions or marking OQs as "resolved-with-default."

---

## Test-surface audit — §10.2

§10.2 lists eight test-obligation groupings (ON-001—004, ON-005—006, ON-007—014, ON-015—017, ON-018—019, ON-020—021, ON-022—029, ON-030—033, ON-034—040, ON-041—046). Evaluating each against the template's "cite [testing.md §<layer>] and the requirement IDs each test proves":

- **ON-001 — ON-004 (exit codes and obligations).** "Negative-path tests covering every exit code listed in §8; static-check test verifying that every requirement with a cross-reference to §4.1 resolves to a §8 entry." Static-check is concrete; negative-path is prose. Missing: which test layer owns each (unit vs scenario vs integration)?
- **ON-005 — ON-006 (integrity gate).** "Upgrade scenario tests with matching and mismatched commit hashes" — concrete. "Verify post-MVH signing extension does not break MVH conformance" — this is a *future* test, not a current obligation.
- **ON-007 — ON-014 (operator-control semantics).** "State-machine scenario tests enumerating every transition in §7.1" — concrete, but §7.1 has missing rows (Counter-example 4). Until the state machine is complete, the test obligation is also incomplete.
- **ON-015 — ON-017 (queue-format compat).** "Upgrade scenario tests with N-1, N, and N+1 Beads schemas" — concrete, but requires a fixture that simulates N+1 Beads. That fixture doesn't exist; the obligation implicitly requires building it.
- **ON-018 — ON-019 (schema compat window).** "Cross-artifact compat tests: write at N, read at N-1, for every listed artifact" — this is the correct shape. Sensor table above codifies this as `scripts/compat-gate.sh`.
- **ON-020 — ON-021 (upgrade contract).** "Full upgrade scenario tests covering all five sub-obligations of ON-020" — prose-obligations translated directly. Requires a fixture where a daemon can exec-replace into another daemon process mid-scenario; non-trivial.
- **ON-022 — ON-029 (security and shutdown).** "Schema-level tests asserting no field is typed as `Secret`" — this is a compile-time lint, not a test; should be under "Tier 1" in build-practices. "Sandbox escape-attempt tests" — open-ended; who owns the test corpus?
- **ON-030 — ON-033 (restart RTO).** "Restart scenario benchmarks measuring SIGTERM-to-`ready` across representative hardware at MVH scale" — needs RTO-gate script per sensor table.
- **ON-034 — ON-040 (observability envelope).** "Per-subsystem-conformance tests verifying typed event emission, structured log emission, health-check interface presence, liveness heartbeat cadence, audit-record derivation, and mechanism-tagging of every observability operation" — this is a six-dimensional matrix per subsystem. For N subsystems, 6N tests. The spec does not name N or the subsystem registry.
- **ON-041 — ON-046 (multi-daemon, budgets).** "Multi-daemon scenario tests verifying `harmonik list`, flag-based targeting, and machine-ceiling enforcement; budget tests verifying declared-enforced-attributed pipeline." Prose. The machine-ceiling tests need a fixture with multiple daemons on one machine — is this a Docker-compose setup? A host-level test?

**Summary.** §10.2 is prose-obligations throughout, which is legal per the bootstrap allowance (note at line 622) but opaque. OQ-ON-002 tracks migration to `[testing.md §<layer>]` once testing.md lands. A stronger shape for now would be to cite the three tiers of project-level/quality-checks explicitly: tier 1 (static/lint), tier 2 (unit/property), tier 3 (scenario/integration). Every requirement could be placed into one tier.

---

## Affirmations

Decisions that hold up under pressure:

1. **Between-task invariant derived from locked decision #10 is coherent and operator-facing.** ON-008 + ON-009's `stop --immediate` carve-out is the right shape; `pause --immediate` and `upgrade --immediate` are correctly forbidden.
2. **Exit-code taxonomy at §8 is concrete and cross-referenced.** 18 codes with detection rule, emitted event, and remediation pointer. This is the right shape even though it's incomplete (Challenge 3).
3. **Improvement-pause as a subtype of pause (ON-012).** Reuse of the pause state machine is correct; auto-resume semantics are clean.
4. **Secrets-redaction compile-time schema check (ON-023).** The observation that "redaction cannot be forgotten if no payload schema carries a secret-typed field" closes the loop at the right layer — compile time, not runtime.
5. **Operator-control events enumerated at §6.5.** The "this spec is normative for the *when*; event-model is normative for the *shape*" pattern matches the template §6.5 co-ownership shape.
6. **§A.3 rationale cites locked decisions explicitly** (#10, #12) and explains the boundary-of-acceptability ("the ceiling is the boundary where the degraded-notification obligation kicks in"). This is how rationale should cross-reference problem-space without rewriting it.
7. **§7.3 upgrade pseudocode enumerates branch points with requirement IDs.** Template §7.2 compliance is good.
8. **Reconciliation carve-out for pause (ON-010).** "Pauses issued during `reconciling` are queued and applied when reconciliation completes" — this is the right shape. An operator who panics during startup and issues `pause` should not block reconciliation from finishing; queueing is correct.
9. **ON-INV-002's anti-design-for-PR-gate directive.** Though it fails the §5 selection test (see Invariant audit), the content is correct and load-bearing for the whole spec corpus — subsystem specs MUST NOT design contracts that assume a pre-merge human review gate. This should be preserved (as §2.1 or §A.4), not deleted.
10. **ON-017's localized-adapter obligation.** "A Beads breaking change MUST produce one localized adapter update; harmonik MUST NOT fork Beads" — this is the structural posture that prevents a pre-1.0 upstream from pulling harmonik into an unbounded absorption loop. The posture belongs at this layer (cross-cutting) rather than buried inside beads-integration.

---

## Summary for r2 reviewer

The spec is proceed-with-revisions. The structural bones are correct: between-task invariant, state machine, exit-code taxonomy, schema-compat window, upgrade contract, multi-daemon coordination. The revisions needed are almost entirely tightening — predicates that need mechanical definitions (in-flight run, durable checkpoint), placeholders that need numbers (RTO X, budget defaults, heartbeat cadence), state-machine rows that need to be filled in, cross-references that need a corpus-wide migration, and §5 invariants that need to be deleted or rewritten. Two design choices are evaded (Challenges 4, 8); picking either shape is fine, but picking is required.

The spec's best section is §8 (exit-code taxonomy) — concrete, tabular, cross-referenced. Its weakest is §4.9 (observability envelope) — aspirational in half its signal classes. The middle is §4.3 (operator-control semantics) — correct in aggregate but with precision gaps around pause-completion, in-flight predicate, and state-machine coverage.

**Priority order for r2 response:** The cross-reference drift (Challenge / audit) should be fixed first because it is mechanical and touches every subsequent reviewer — they cannot verify a citation that resolves to the wrong section. Second, the in-flight predicate (Challenge 1) and pause-completion precision (Challenge 2) together unblock the pause/upgrade story. Third, the RTO number (Challenge 6) and the state-machine completion are quick fixes that close open counter-examples. The design-choice items (Challenges 4, 5, 8; graceful-vs-pause relationship) can follow once the mechanical and precision gaps are closed.

**Estimated edit scope:** 60–100 new lines of normative text across §3 (glossary additions for `in_flight`, `operator`, `migration release`, `nominal conditions`), §4.3 (bounded-pause, state-machine completion), §4.7 (drain-timeout partitioning), §4.8 (numeric RTO, sensor), §4.11 (budget defaults + exhaustion protocol), §8 (three new exit codes), plus 20–40 lines of deletions in §5 (invariants that fail the selection test) and a corpus-wide citation rewrite pass (mechanical).

Nothing requires re-opening a locked decision. The spec respects decision #10 correctly; the issues are all at the precision layer beneath decision #10.
