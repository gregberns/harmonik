# Research — Counter-evidence (Phase 2)

Phase 2 explicitly seeks evidence AGAINST the kickoff design constraints. Surfacing this prominently per kickoff prompt.

## COUNTER-EVIDENCE 1: stream-json + --include-hook-events is a competing architecture

**Source:** https://code.claude.com/docs/en/cli-reference (--include-hook-events), https://code.claude.com/docs/en/headless (stream-json output), https://pkg.go.dev/github.com/partio-io/claude-agent-sdk-go.

**Claim:** Instead of materializing `.claude/settings.json` + spawning a `harmonik hook-relay` subcommand per hook, harmonik could spawn Claude with `--output-format stream-json --include-hook-events --include-partial-messages` and parse the NDJSON stream directly from Claude's stdout. This is the pattern the official-adjacent Go SDK uses.

**Architectural diff:**

| Aspect | Settings.json + relay (kickoff design) | stream-json + hook events (alternative) |
|---|---|---|
| File-system surface | .claude/settings.json materialized in workspace | none |
| New subprocess shape | harmonik hook-relay (child of Claude) | none (handler reads Claude's stdout directly) |
| Daemon socket connections per session | many (one per hook fire) | one (the existing watcher-to-handler channel) |
| Env-var inheritance burden | HARMONIK_* through Claude to relay | HARMONIK_* through Claude only (handler already has them) |
| Claude version coupling | settings.json schema (stable, public API) | stream-json event schema (evolving, less documented) |
| Failure mode if Claude crashes | hooks stop firing; handler's Wait detects exit | handler's stdout read returns EOF; same path |
| Twin parity story | Twin emits NDJSON natively; relay layer is a Claude-only concern | Twin emits NDJSON natively; SAME |
| Test harness | Pipe canned hook payloads to relay binary | Pipe canned stream-json to handler's stdout reader |

**Why this is genuine counter-evidence:**
- Official Anthropic-adjacent Go SDK uses the stream-json + control protocol path, not settings.json.
- It eliminates a whole subprocess type (relay) and a whole filesystem-write step (settings.json materialization).
- It's the path Anthropic appears to be steering programmatic integrators toward (`-p` + `--bare` + stream-json is the documented "scripted callers" pattern).

**Why we DON'T pivot to it (kept the kickoff design):**
1. **Stream-json event schema is less documented and less stable.** Public docs cover the format at a high level but do not enumerate the full event-type vocabulary the way the hooks reference does. The hook reference enumerates 30 events with payload schemas; the stream-json reference enumerates ~5 (system, assistant, user, result, stream_event). The hook surface is more contract-shaped.
2. **--include-hook-events requires --output-format stream-json, which fundamentally changes Claude's interactive experience.** For agents running in headless mode (which is harmonik's case) this is fine. But it conflicts with the design constraint that the bridge should remain stable as the operator's invocation surface evolves.
3. **The relay path lets harmonik observe hook decisions Claude makes that don't necessarily land on stdout.** E.g., a Notification hook can fire without producing any output to Claude's stdout. The stream-json path may or may not surface these events; the docs are ambiguous.
4. **Per-hook short-lived processes are easier to test than a long-lived stdout-parser.** The relay subcommand can be invoked directly with stdin/env in unit tests; the stream-json parser requires either a real Claude subprocess or a fully faked stream.
5. **Settings.json hooks are a publicly-stable API (documented under hooks reference).** stream-json with --include-hook-events feels less stable.
6. **User has already decided** on settings.json + relay per kickoff prompt constraint A1/A2. This is design-pass-influencing evidence, not a re-litigation trigger.

**Cost of being wrong:** if stream-json becomes the dominant integration path and settings.json hooks degrade, the bridge has to be re-implemented. The HC-052 "shape evolution re-implements the adapter, not the watcher" rule contains this blast radius — the watcher's NDJSON-on-socket contract is unchanged either way.

**Recommendation:** Carry the kickoff design but make the bridge spec explicit that it is ONE of two viable Claude-bridging architectures, with stream-json + hook events documented as the post-MVH alternative. The relay layer is an opaque mechanism; the spec is about what messages reach the daemon socket, not how they got there.

## COUNTER-EVIDENCE 2: --bare disables hook auto-discovery

**Claim:** If we want fast Claude startup, --bare is the documented mode for CI/scripts. But --bare skips hook auto-discovery, breaking the settings.json approach.

**Resolution:** harmonik does NOT use --bare. We accept the startup cost of hook discovery. If startup latency becomes a problem post-MVH, we add `--settings <file>` to explicitly load the materialized settings.json, then --bare becomes compatible.

## COUNTER-EVIDENCE 3: pre-existing project .claude/settings.json collisions

**Claim:** A user's repo might already have `.claude/settings.json` with project-specific hooks (their lint hook, their security check, etc.). Materializing harmonik's settings.json in the per-run worktree would clobber it.

**Resolution check:** The per-run worktree is `<repo>/.harmonik/worktrees/<run_id>/`, which is a fresh worktree. `git worktree add -b` creates a fresh working tree where the files are checked out from the start-point commit. So the user's existing `.claude/settings.json` IS present in the worktree at create time, and harmonik's materialization would overwrite it.

**Two paths:**
- (a) Overwrite (clobber user settings) — simpler, but the user's hooks don't run in harmonik runs. This may break user workflows (e.g., a security check hook that prevents `rm -rf` would not fire).
- (b) Merge — harmonik's hooks are appended to existing hook arrays; the user's hooks still fire.

**Decision:** Merge. The settings.json `hooks` field is structurally additive — each event type is an array of matcher+hooks groups, and a new matcher group simply adds new hooks. Harmonik's hooks (Stop / SessionEnd / Notification / StopFailure) target the events the bridge cares about; they coexist with user hooks. **Settings.json templating** (the merge mechanism) becomes a sub-decision for design pass.

**If merge is infeasible** (e.g., user's hooks conflict on the same event with side effects), fall back to overwrite + emit a `workspace_warning` event noting the displacement. This is a runtime classification, not a spec-time decision; the spec specifies the obligation (preserve user hooks where possible), the impl picks the algorithm.

## COUNTER-EVIDENCE 4: --session-id with --resume reuses the UUID; is that documented as stable?

**Claim:** The pre-generated session_id design assumes `--session-id <uuid>` at first launch + `--resume <uuid>` on subsequent launches produces a stable claude_session_id across the entire review-loop iteration cycle. Is this documented?

**Evidence:**
- --session-id doc: "Use a specific session ID for the conversation (must be a valid UUID)" — first-launch behavior clear.
- --resume doc: "Resume a specific session by ID or name" — resume behavior clear; doesn't explicitly say "and the resumed session keeps the same session_id."
- --fork-session doc: "When resuming, create a NEW session ID instead of reusing the original" — IMPLICITLY confirms that without --fork-session, --resume reuses the original session_id.
- Headless docs: "If you're running multiple conversations, capture the session ID to resume a specific one" — the captured session_id is the same one used on resume.

**Conclusion:** the design works. The bridge spec MUST explicitly forbid --fork-session in implementer-resume flow. Document this.

## COUNTER-EVIDENCE 5: hook command timeout

**Claim:** settings.json hook entries have a `timeout` (default 600s). If a relay invocation takes longer than the timeout (e.g., daemon-not-ready retry exhaustion), Claude kills the relay. What does Claude do with a killed hook?

**Evidence:** From the hooks doc, non-blocking events (SessionEnd, StopFailure, Notification) cannot block regardless. Blocking events (Stop) with a killed hook: the exit code is non-zero (likely 124 or similar), which per the exit-code table means "non-blocking error; show first line of stderr; continue execution". So a Stop hook timeout means Claude proceeds with the stop anyway. From harmonik's perspective, this means a relay timeout MAY lose the outcome_emitted event.

**Resolution:** Set relay timeout HIGHER than the HC-016a bound (60s) but LOWER than the post-outcome shutdown window (T_shutdown = 10s ... wait, that's shorter). Actually the relay needs to publish FAST. Recommend: settings.json hook timeout 30s, relay-internal daemon-socket dial 5s, retry on daemon-not-ready 1-2 attempts only within the 30s envelope. If the daemon is fully unreachable within 30s, the relay exits non-zero and Claude continues. The handler's Wait-return will detect Claude's exit and emit agent_failed.

This is consistent with HC-INV-006 (exactly one terminal event per session) — the handler's Wait-return is the terminal path of last resort.

## COUNTER-EVIDENCE 6: transcript_path is in ~/.claude/projects, not the workspace

**Claim:** Claude writes its transcripts to `~/.claude/projects/<slug>/<session-uuid>.jsonl`, NOT to the workspace's `.harmonik/sessions/` directory. This breaks the WM-025 "session-log directory layout" assumption.

**Evidence:** Confirmed. The transcript path comes from Claude, hardcoded to `~/.claude/projects/`. Hooks have access via `transcript_path` field.

**Resolution:** Distinguish two log surfaces:
- (a) **Claude's own transcript** at `~/.claude/projects/<slug>/<session-uuid>.jsonl` — this is what hooks see and what S08 (memory layer) ultimately wants to index for CASS. Harmonik treats this as read-only and emits `session_log_location` pointing at it (per HC-010, the path the handler announces).
- (b) **Harmonik's per-session metadata sidecar** at `${workspace_path}/.harmonik/sessions/${session_id}/harmonik.meta.json` (per WM-026) — this is a separate artifact, harmonik-controlled, with the harmonik-session-id-keyed metadata (run_id, node_id, agent_type, etc.).

These are TWO distinct artifacts with different ownership. The harmonik sidecar's `log_path` field (or the session_log_location event's `log_path` field) carries the Claude transcript path. WM-025's session-log directory still exists for the harmonik-side sidecar; Claude's transcript lives outside.

**Spec consequence:** the bridge spec must clarify that for the `claude-code` agent type, the session_log_location event's `log_path` is the Claude transcript path (in ~/.claude/...), NOT a path under the workspace. This is consistent with HC-010 ("session log path emission") and WM-025 (which OWNS the harmonik sidecar directory but does NOT mandate that the handler-written log lives inside it).

## Net counter-evidence summary

1. Settings.json + relay is one of two viable architectures; stream-json + --include-hook-events is the other. Carry kickoff design but document the alternative as post-MVH evolution path. Risk is contained by HC-052.
2. --bare disables hooks; harmonik doesn't use --bare. Documented.
3. Pre-existing settings.json: design pass must decide merge vs overwrite; recommend merge.
4. --session-id reuse on --resume is implicitly confirmed (via --fork-session doc). Spec MUST forbid --fork-session in resume path.
5. Hook command timeouts: relay must complete fast (<30s) to avoid losing events on slow daemon path. HC-INV-006 contains the failure mode.
6. Claude transcript is in ~/.claude, not workspace. Bridge spec must clarify session_log_location.log_path is the Claude transcript path. Doesn't conflict with WM-025 but worth a spec clarification.

None of these are blockers. All are surfacing items to be recorded in design decisions.
