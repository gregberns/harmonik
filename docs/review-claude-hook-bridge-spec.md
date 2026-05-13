# Spec Review — claude-hook-bridge.md v0.1

**Reviewed by:** sub-agent spec-reviewer  
**Date:** 2026-05-12  
**Commit:** 6bc2e57 (branch `spec/claude-hook-bridge`)  
**Scope:** `specs/claude-hook-bridge.md` (CHB-001..024, 3 invariants, error taxonomy) plus amendments to HC v0.3.5, WM v0.4.3, PL v0.4.2, EM v0.4.1.

---

## 1. Invariant Gaps

**Finding 1.1 — CHB-INV-001 says "zero-or-more" relay contributions; the phrase is defensively correct but leaves unsupervised the case where zero relay messages arrive before the terminal event.**

CHB-INV-001 allows a session in which no relay ever fires (e.g., all hooks fail exit-1). The terminal-event invariant (CHB-INV-002) is still satisfied by the handler's Wait-return path (CHB-020 third bullet: `claude_exit_without_outcome → ErrStructural`). However, the invariant does not say whether the daemon should raise an alarm if Stop never fires after a successful outcome_emitted. The relay-can't-dial scenario test in §10 covers the dial-failure path, but a silent no-hook path (e.g., `--bare` flag sneaking through despite CHB-007) produces no relay traffic and the daemon would never receive `outcome_emitted` before Wait-return — correctly handled by CHB-020's "no outcome_emitted observed" branch. This is fine mechanically.

**Assessment:** No invariant gap for correctness; the path is handled. However, CHB-INV-001 would be strengthened by noting explicitly that zero-relay is the fault-delivery path, not the success path — this aids auditors. *Proposed fix: add one sentence to CHB-INV-001: "Zero relay contributions in a completed session is a fault indicator (see §4.7.CHB-020 third branch); success MUST include at least one Stop relay emission."*

**Finding 1.2 — CHB-023 is in §4.6 (Daemon-socket protocol) but governs daemon-side behavior, not relay behavior. Section placement is misleading.**

CHB-023 imposes an obligation on the daemon (persist `claude_session_id` to git before returning the ACK). All surrounding requirements (CHB-015..017) govern the relay subprocess. The misplacement will confuse implementers navigating §4.6 expecting relay rules.

*Proposed fix: move CHB-023 to a new §4.7a "Daemon persistence gate" subsection, or relocate into §4.7 Handler-process responsibilities with a note cross-referencing §4.6.*

**Finding 1.3 — No invariant pins the `(run_id, claude_session_id)` uniqueness across concurrent runs.**

CHB-INV-001 asserts the two-contributor model keyed on `(run_id, claude_session_id)`. CHB-022 says the daemon routes on this key with no `isTwin` branch. But no invariant states that `(run_id, claude_session_id)` is unique across the set of live sessions — if two relay processes from different sessions both carry the same `run_id` (impossible by UUIDv7 minting) or if session_id is mis-propagated, the routing silently merges two distinct sessions. The minting rules (CHB-008) prevent this, but no invariant names the uniqueness guarantee.

*Proposed fix: Acknowledge as-is at MVH. The combination of CHB-008 (fresh UUIDv7 per non-resume phase) and CHB-023 (persisted before exec) makes collision impossible. Add a note to CHB-INV-001 citing CHB-008 as the uniqueness mechanism. Minor revision.*

---

## 2. Contradiction Sweep

**Finding 2.1 — CHB-013 maps `Stop` to `outcome_emitted` regardless of whether `outcome_emitted` was already observed. The handler-side state in CHB-020 assumes at most one `outcome_emitted{kind ∈ {WORK_COMPLETE, REVIEWER_VERDICT}}` per session, but the spec does not forbid Claude from firing Stop twice.**

Claude's `Stop` hook fires each time Claude's turn loop finishes a reply. If Claude produces a multi-turn response, Stop could fire more than once. The relay would emit multiple `outcome_emitted` messages. CHB-020 says "if `outcome_emitted` has been observed" (singular), which is ambiguous when multiple arrive. The daemon's handling of a second `outcome_emitted` is unspecified.

This is a real gap. The relay has no deduplication; the handler has no guard. A second relay-emitted `outcome_emitted` after the first hits the daemon watcher, which would process it per HC-INV-007. Whether the daemon silently drops it, overwrites state, or emits two terminal events is unspecified.

*Proposed fix (MINOR): Add a CHB requirement (gap-filler after CHB-013) that the relay MUST gate Stop-relay emission on the absence of a prior `outcome_emitted` per this session's `(run_id, claude_session_id)` key — achieved by reading a session-local marker file written after the first Stop relay. Alternatively, constrain the daemon watcher to idempotently ignore a duplicate `outcome_emitted` from the same session. The spec should close this gap explicitly.*

**Finding 2.2 — CHB-008 says `phase ∈ {single, implementer-initial, reviewer}` all mint a fresh `claude_session_id`. CHB-009 repeats the reviewer rule redundantly. HC-045c (a) says the same. No contradiction, but the redundant statement in CHB-009 will drift from CHB-008 if CHB-008 changes.**

*Proposed fix: Collapse CHB-009 into a cross-reference to CHB-008(a); the reviewer-fresh rule is already captured. Minor.*

**Finding 2.3 — WM-040a says "the workspace manager MUST attempt a merge per [claude-hook-bridge.md §4.1 CHB-004]" but calls out "merge-incompatible" as an overwrite case without defining what merge-incompatible means beyond malformed JSON.**

CHB-004 defines only the malformed-JSON overwrite case and the `disableAllHooks` removal. WM-040a adds "merge-incompatible existing content" as a separate overwrite trigger. These two formulations diverge: CHB-004 has no "merge-incompatible" class.

*Proposed fix: Align WM-040a's overwrite condition to match CHB-004's language exactly: malformed JSON only. Remove or define "merge-incompatible." Minor.*

**Finding 2.4 — CHB-004's `disableAllHooks: true` removal interacts with CHB-024's settings.local.json check in an unclear order.**

CHB-004 removes `disableAllHooks` from the merged `settings.json`. CHB-024 checks `settings.local.json` for `disableAllHooks`. But CHB-024 fires at handler startup, AFTER workspace materialization. If `settings.local.json` was written by Claude itself during a prior interrupted session (unlikely but possible), the handler would fail with `bridge_settings_shadowed` rather than silently removing the key as CHB-004 does for `settings.json`.

*Proposed fix: Acknowledge as-is with rationale. The asymmetry is intentional — `settings.json` is harmonik-owned (we can mutate it), `settings.local.json` is user-owned (we must not mutate it, only fail fast). A clarifying sentence in §4.9 CHB-024 noting this ownership distinction would resolve the apparent asymmetry.*

---

## 3. Missing Edge Cases

**Finding 3.1 (MAJOR) — OOM-kill of the relay subprocess mid-write is unaddressed.**

If the relay OS process is killed (SIGKILL, OOM) after opening the socket but before writing the NDJSON line, the daemon receives an abrupt EOF on the relay's connection. The daemon's watcher has no mechanism to distinguish "relay connected but wrote nothing" from "handler connection dropped." The `run_id` / `claude_session_id` envelope is only visible after the line is written; if writing is incomplete, the daemon cannot route the partial write. No requirement specifies how the daemon handles partial or zero-byte relay connections.

This is a gap in the daemon-side contract. The relay's CHB-016 retry covers `daemon_not_ready`, but not self-interrupted writes. An OOM-killed relay would leave the daemon watcher in the dark about the hook event it was translating. For Stop hooks specifically, this means the daemon would never receive `outcome_emitted`, and CHB-020's "no outcome_emitted observed" branch would fire → `claude_exit_without_outcome`. This is a recoverable false-negative (correct terminal event, wrong sub_reason). However the spec should state it explicitly.

*Proposed fix: Add a note to §8 error taxonomy for `bridge_relay_oom` (class `ErrTransient`, cause: relay OOM-killed; observable as daemon watcher receiving zero-byte connection from relay). State that the handler's Wait-return CHB-020 "no outcome_emitted" path is the recovery mechanism. This is an acknowledge-and-document case, not a mechanism change.*

**Finding 3.2 (MAJOR) — settings.json mid-write race with Claude exec is underspecified.**

CHB-002 requires the atomic write to complete before `workspace_leased`. CHB-024 verifies hook reachability at handler startup (after workspace_leased, before Claude exec). The write-to-exec window is therefore: WM-003 → CHB-002 write (atomic) → WM-016 `workspace_leased` → handler starts → CHB-024 check → Claude exec. This ordering appears safe.

However: on workspace re-use (e.g., after a crash-recovery where the worktree is preserved), does CHB-002's atomic write re-run? WM-031 says failed-run worktrees persist. If a subsequent handler re-uses an existing worktree, the materialization step may be skipped (the spec doesn't address worktree reuse). If the prior run left a `settings.json` without the current hook set (e.g., from an older harmonik binary), CHB-024 may pass (the hooks exist) even though the hook entries point to an older relay binary interface.

*Proposed fix: Add a note in CHB-002 or §2.2 out-of-scope: worktree reuse semantics for `settings.json` are deferred to the workspace-manager re-run rule (WM-034); implementors must re-materialize on worktree reuse. Minor clarification.*

**Finding 3.3 (MODERATE) — `harmonik hook-relay` invoked OUTSIDE a harmonik-managed session (manual user invocation).**

If a developer runs `harmonik hook-relay SessionStart` manually in a shell without `HARMONIK_RUN_ID` or `HARMONIK_DAEMON_SOCKET` set, the relay will attempt to dial the socket, fail to connect (no socket file), and exit 1 with `bridge_dial_failed`. This is likely acceptable but unspecified. More importantly: if `HARMONIK_RUN_ID` is set from a parent shell (a prior session's env leaking) and the daemon socket exists but belongs to a different run, the relay will connect, write a message with a stale `run_id`, and receive either `unknown_session` or `daemon_not_ready`. The relay would then exit 1 with a confusing error.

The spec does not address environmental contamination from stale `HARMONIK_*` env vars in the user's shell. CHB-006 says the handler MUST set these vars, implying they are set fresh per session, but env inheritance through subshells is a real risk.

*Proposed fix: Add to §2.2 out-of-scope: "User-invoked `harmonik hook-relay` outside a harmonik-managed session is unsupported; behavior on missing or stale `HARMONIK_*` env vars is unspecified beyond exit-1 per CHB-017." This closes the spec gap without adding mechanism. Minor.*

**Finding 3.4 (MODERATE) — No specification of relay behavior when `HARMONIK_PHASE` env var is absent for Stop → outcome_emitted mapping.**

CHB-013 maps Stop → `outcome_emitted{kind = WORK_COMPLETE if phase ∈ {single, implementer-initial, implementer-resume}}` and `{kind = REVIEWER_VERDICT if phase = reviewer}`. But CHB-006 marks `HARMONIK_PHASE` as optional ("only set when non-default"). If `HARMONIK_PHASE` is absent, the relay cannot determine whether to read `review.json`. The "default" phase is presumably `single`, but this is not stated.

*Proposed fix: Add a sentence to CHB-013 mapping row: "If `HARMONIK_PHASE` is absent, the relay MUST treat the phase as `single` and emit `WORK_COMPLETE`." Minor.*

---

## 4. Forward-Compatibility

**Finding 4.1 — §11 stream-json evolution path is genuinely watcher-agnostic, but CHB locks in one assumption: the relay envelope carries `claude_session_id` as a top-level routing key.**

Post-MVH migration to stream-json eliminates the relay subprocess. The handler would parse Claude's stdout NDJSON directly and emit progress-stream messages from the handler process (not from relay subprocesses). The `claude_session_id` routing key would still appear in the envelope — but it would now be the handler process itself writing it rather than relay subprocesses. This is fully compatible with CHB-022 (daemon is twin-blind) because routing on `(run_id, claude_session_id)` is source-agnostic.

**Assessment:** §11's claim that "the adapter and the bridge spec change, the wire-level invariants do not" is accurate. HC-052 (watcher-agnostic) holds. No forward-compatibility hole here.

**Finding 4.2 — The one-shot relay connection regime (CHB-015) produces N short-lived connections per session (one per hook event). The daemon must accept many connections per session from multiple PIDs. This is compatible with HC-045b but places a constraint on the daemon's accept loop that is not quantified.**

If Claude fires many Notification hooks rapidly (e.g., during a long reasoning pass with many idle_prompt events), the relay spawns one subprocess per event. At high throughput this could saturate the daemon's accept loop or the OS's connection backlog. No connection rate limit is specified.

*Proposed fix: Acknowledge as-is for MVH. Add an OQ: "OQ-CHB-004 — Should the daemon's accept loop enforce a per-session max-connections-per-second rate limit on relay connections?" Inform-only; no mechanism required at MVH.*

---

## 5. Conformance Test Completeness

§10 lists four scenario tests. Coverage analysis against normative requirements:

| Requirement group | Scenario covering it |
|---|---|
| CHB-001..005 (settings.json materialization) | Scenario 1 (single run) — implicit; not explicit |
| CHB-006..007 (env-var schema, forbidden flags) | Not covered by any named scenario |
| CHB-008..009 (session_id flow) | Scenario 2 (review-loop, resume + fresh reviewer) |
| CHB-010..012 (relay subcommand surface + stdin schema) | Scenario 1 — implicit |
| CHB-013 (mapping table) | Scenario 1 — partial (Stop path only) |
| CHB-014 (reviewer verdict file read) | Scenario 2 |
| CHB-015..016 (socket protocol, retry) | Scenarios 3 + 4 |
| CHB-017 (exit codes) | Scenarios 3 + 4 — partial |
| CHB-018..019 (pre-exec ordering, heartbeat) | Not explicitly covered |
| CHB-020 (terminal event on Wait-return) | Scenario 3 — implicit (relay can't dial → no outcome_emitted → handler emits terminal) |
| CHB-021..022 (twin parity) | Scenario 1 (real vs twin comparison) |
| CHB-023 (daemon durability gate) | Not covered by any scenario |
| CHB-024 (settings.local.json shadow check) | Not covered by any scenario |

**Finding 5.1 (MODERATE) — Four requirements have no scenario coverage: CHB-006/007 (env-var contract), CHB-018/019 (pre-exec ordering and heartbeat), CHB-023 (durability gate), CHB-024 (shadow check).**

*Proposed fix: Add two scenarios:*
- *"Env-var injection scenario": verify that the relay subprocess receives all HARMONIK_* vars with correct values derived from the LaunchSpec fields.*
- *"Settings shadow scenario": place a `settings.local.json` with `disableAllHooks: true` in the workspace before handler exec; verify the handler emits `agent_failed{sub_reason: bridge_settings_shadowed}` and does NOT exec Claude.*

*Minor revision: the existing four scenarios are necessary but not sufficient.*

---

## 6. Open Questions — Recommended Dispositions

**OQ-CHB-001 — SO_PEERCRED / LOCAL_PEERCRED check.**
Disposition: Defer to MVH+1. The filesystem-permission discipline (socket owned by the daemon UID, in the repo's `.harmonik/` directory which is not world-writable) provides adequate isolation for a single-user local development context. SO_PEERCRED adds defense-in-depth on multi-user servers but is platform-divergent (Linux vs macOS API difference). Track as a post-MVH security hardening item.

**OQ-CHB-002 — Notification idle_prompt → agent_output_chunk.**
Disposition: Resolve as "no" at MVH with an explicit close. Operator-side observability of idle prompts is better served by the notification text appearing in the transcript JSONL than by synthesizing a separate progress-stream message. If future operators need the text, the correct surface is a post-MVH `Notification` → `agent_output_chunk` mapping; the mapping table in CHB-013 can be extended without breaking existing consumers. Close the OQ now rather than leaving it open.

**OQ-CHB-003 — Conflicting user hooks warning.**
Disposition: Resolve as "no" at MVH, but promote the issue to a future spec requirement rather than relying on user discipline. The real risk is a user-authored hook in `settings.json` that shadows harmonik's bridge hook for the same event type via a different matcher. The merge rule (CHB-004: bridge entries APPENDED) guarantees bridge hooks fire alongside user hooks, but user hooks that exit non-zero and block Claude's hook execution could suppress the bridge relay. Document this as a known limitation in §2.2 out-of-scope. Close OQ-CHB-003.

---

## 7. Twin Parity Feasibility

**CHB-021 says twin emits the same wire format. CHB-022 says daemon has zero `if isTwin` branches.**

Analysis: The twin (`harmonik-twin-claude`) emits the progress-stream sequence from a single long-lived process, NOT from relay subprocesses. This means the twin's connections to the daemon socket use the handler's long-lived stream (not the one-shot relay connections). The `(run_id, claude_session_id)` envelope is present on both the handler's stream and on relay short-lived connections in a real run. In the twin, ALL messages arrive on the handler's single connection.

**Finding 7.1 — Connection-regime divergence between real and twin is not addressed.**

In a real run, the daemon receives: (a) long-lived handler connection emitting pre-exec messages + heartbeats; (b) N short-lived relay connections emitting hook-derived messages. The wire-format NDJSON sequence is the same, but the connection multiplicity differs. CHB-022 says the daemon must handle both, and HC-045b clarifies that relay connections are separate from the handler's long-lived stream. For the twin, all messages arrive on one connection.

The daemon must therefore handle: (a) handler connection + relay connections in real runs; (b) handler connection only in twin runs. The daemon's watcher must accept and merge messages from multiple concurrent connections. This is architecturally non-trivial and the spec does not enumerate how the watcher merges messages from multiple concurrent connections into one per-session bus event flow.

*Finding:* CHB-022's "zero if isTwin branches" claim is plausible if the watcher treats any authenticated `(run_id, claude_session_id)` connection as a valid source, regardless of origin. But the spec does not say whether the daemon's watcher serializes messages from concurrent connections or whether message ordering is defined when relay and handler messages arrive simultaneously. This is a MODERATE gap.

*Proposed fix: Add a note in §4.6 or CHB-INV-001: "The daemon watcher MUST serialize messages from concurrent (handler + relay) connections per the session's `(run_id, claude_session_id)` key in arrival order. No ordering guarantee is provided between handler-emitted and relay-emitted messages beyond the invariants in §4.7.CHB-018 (pre-exec handler messages precede Claude exec, which precedes any relay messages)."*

**Finding 7.2 — The twin feasibility claim is sound given the envelope design.**

The `(run_id, claude_session_id)` envelope is sufficient for routing regardless of source process. The twin need only include both keys in each emitted message, which it already would as a single process with access to both values. CHB-022's twin-blind daemon is achievable. No blocking concern here.

---

## Summary of Concerns by Severity

### MAJOR (2)
1. **Finding 3.1** — OOM-kill of relay mid-write produces an unspecified daemon-side behavior; the recovery path through CHB-020 is correct but the spec is silent on the mid-write connection EOF case. Add to §8 error taxonomy.
2. **Finding 2.1** — Multiple Stop hook firings (multi-turn Claude session) can produce duplicate `outcome_emitted` emissions with no deduplication guard in relay or daemon. Requires a new requirement or explicit daemon idempotency rule.

### MODERATE (4)
3. **Finding 7.1** — Concurrent handler + relay connections to the daemon watcher: serialization semantics and message ordering are unspecified. Add a normative note.
4. **Finding 5.1** — Four requirements (CHB-006/007, CHB-018/019, CHB-023, CHB-024) have no corresponding scenario tests. Add two scenarios.
5. **Finding 3.3** — Manual/stale-env invocation of `hook-relay` outside a managed session is unspecified. Add to §2.2 out-of-scope.
6. **Finding 3.4** — Absent `HARMONIK_PHASE` env var leaves Stop → outcome_emitted kind mapping ambiguous. Add a default.

### MINOR (6)
7. **Finding 1.2** — CHB-023 misplaced in §4.6 (relay section); governs daemon behavior. Relocate.
8. **Finding 2.3** — WM-040a "merge-incompatible" language diverges from CHB-004's overwrite trigger. Align.
9. **Finding 2.4** — `disableAllHooks` removal asymmetry between settings.json (CHB-004) and settings.local.json (CHB-024). Add a clarifying sentence.
10. **Finding 1.1** — CHB-INV-001 does not name zero-relay-traffic as a fault indicator. Add one sentence.
11. **Finding 2.2** — CHB-009 redundant with CHB-008(a) + HC-045c(d). Collapse with a cross-reference.
12. **Finding 3.2** — Worktree reuse / re-materialization of settings.json on crash-recovery is unaddressed. Add a note in CHB-002.

---

## Verdict

**MINOR_REVISIONS**

12 concerns total: 2 MAJOR, 4 MODERATE, 6 MINOR. No BLOCK-level issue. The two MAJOR items can be resolved with a new relay deduplication requirement (or daemon idempotency rule) and a §8 error taxonomy addition — neither requires rethinking the architecture. The spec is internally coherent, cross-references are accurate, and the twin-parity claim is sound given the envelope design.
