# Spec-draft Changelog — claude-hook-bridge kerf

## NEW

- `specs/claude-hook-bridge.md` (v0.1, status `draft` on landing; promoted to `reviewed` after Phase 3 review).
  - 24 normative requirements (CHB-001..024) + 3 invariants (CHB-INV-001..003).
  - Scope: §4.1 settings.json materialization (CHB-001..005), §4.2 env-var schema (CHB-006..007), §4.3 claude_session_id flow (CHB-008..009), §4.4 hook-relay subcommand contract (CHB-010..012, CHB-017), §4.5 hook→progress-message mapping (CHB-013..014), §4.6 daemon-socket protocol (CHB-015..017, CHB-023), §4.7 handler-process responsibilities (CHB-018..020), §4.8 twin parity (CHB-021..022), §4.9 settings-precedence verification (CHB-024).
  - 3 OQs: CHB-001 SO_PEERCRED check, CHB-002 Notification text in output_chunk, CHB-003 hook-conflict warnings. (OQ-CHB-004 promoted to CHB-024.)

## AMENDED

- `specs/handler-contract.md` v0.3.3 → v0.3.4:
  - Add HC-045a (pointer to claude-hook-bridge.md for claude-code).
  - Add HC-045b (hook-bridge one-shot NDJSON connection regime).
  - Add HC-045c (handler-side claude_session_id minting / resume / forbidden-flags discipline; orphan-reconnect git-derived lookup).
  - Clarifying sentence on HC-006 pointing forward to HC-045c and CHB-023.
  - **ID-placement note:** gap-fillers after HC-045 (matching the HC-016a / HC-026b pattern). The earlier draft had used HC-053/054/055, which collided with the existing HC-053 in §6.2.
  - No IDs renumbered or retired.

- `specs/workspace-model.md` v0.4.2 → v0.4.3:
  - Add new sub-section §4.7a Claude-code settings.json materialization.
  - Add WM-040a (settings.json materialization + atomic-write + merge-with-existing). Renamed from the earlier draft's WM-038 to avoid collision with the existing WM-038 interrupt-state writer requirement in §4.10.
  - Extend §4.3 WM-013e gitignore set to include `.claude/settings.json`.
  - Add INFORMATIVE retention paragraph at end of §4.7.
  - Overwrite-on-malformed logs to the session log; no new bus event introduced (preserves the zero-new-event-types invariant of the bridge; the earlier draft's `workspace_warning{reason="settings_file_overwritten"}` event was a phantom and is removed).

- `specs/process-lifecycle.md` v0.4.1 → v0.4.2:
  - Add PL-017a in §4.5 (final ID; placeholder removed) clarifying that hook-bridge relay subprocesses are grandchildren of the daemon and not subject to PL-014 / PL-014a / PL-006. PL-017a is a gap-filler after PL-017; the earlier draft's PL-018 collided with the existing PL-018 in §4.6.

- `specs/execution-model.md` (amendment added in this correction pass):
  - Amended §4.3 EM-015d to replace the `claude -p ... --output-format json` post-launch capture mechanism with the pre-exec handler-mint + `handler_capabilities` capture path per CHB-018 / HC-045c, persisted via the durable-checkpoint discipline of CHB-023.
  - Glossary entry for `claude_session_id` updated to match.
  - No new requirement IDs added or renumbered.

- `specs/event-model.md`: NO normative change (recommend skip). Optional glossary additions to §3 deferred.

## DOC UPDATES

- `docs/subsystems/agent-runner.md` — replace line-28 hand-wave with cite-forward shape pointing to claude-hook-bridge.md.
- `docs/subsystems/hook-system.md` — add "Realization at MVH" section pointing to bridge spec.
- `AGENT_INDEX.md` — add bridge spec to normative spec inventory.

## Spec-graph delta

- New normative spec: 1 (claude-hook-bridge).
- New normative requirement IDs: 24 + 3 = 27 (CHB-001..024, CHB-INV-001..003) + 3 (HC-045a, HC-045b, HC-045c) + 1 (WM-040a) + 1 (PL-017a) = **32 net-new requirement IDs**.
- Renumbered IDs: 0.
- Retired IDs: 0.
- New event types: 0.
- New OQs: 3 (CHB-001..003); OQ-CHB-004 promoted to normative CHB-024.

## Cross-spec coordination

The new bridge spec is the load-bearing piece. The five amendments are coordinated:

- HC-045b NAMES the connection regime, CHB consumes it.
- HC-045c declares handler obligations (including the orphan-reconnect git-derived `claude_session_id` lookup discipline), CHB depends on them.
- WM-040a supplies the materialization mechanism, CHB depends on it.
- PL-017a clarifies subprocess-tree semantics, CHB cites it.
- EM-015d amendment aligns the daemon's `claude_session_id` capture path with the bridge's pre-exec handler-mint discipline; CHB-023 imposes the durability boundary on the daemon side.

No circular dependencies. Cite direction is CHB → HC, CHB → WM, CHB → PL, CHB → EM (CHB depends on the others; the others reference CHB via "cite forward" for context).

## 2026-05-12 correction pass

This pass applied the consolidated findings from the four spec-draft reviewer reports (one BLOCK with three blocking sub-findings; the other three REQUEST_CHANGES). The blocking findings were:

1. **ID collisions** — the earlier draft's HC-053/054/055 collided with the existing HC-053 in §6.2 of handler-contract.md; the earlier WM-038 collided with the existing WM-038 (interrupt-state writer) in workspace-model.md; the earlier PL-018 collided with the existing PL-018 (deterministic-daemon-binary) in process-lifecycle.md. All three renumbered to gap-filler IDs (HC-045a/b/c, WM-040a, PL-017a) matching the existing gap-filler convention in the corpus.
2. **Phantom event** — the earlier WM/CHB drafts prescribed a `workspace_warning{reason="settings_file_overwritten"}` event that does not exist in event-model.md and that contradicts the bridge's stated "zero new event types" design constraint. Replaced with a session-log warning line.
3. **CHB-013 ↔ CHB-INV-002 contradiction** — the earlier mapping table routed `StopFailure → agent_failed` from the relay, contradicting CHB-INV-002 (relay never emits terminal events). Mapping table changed to emit `outcome_emitted{kind=FAILURE_SIGNAL}` (non-terminal); the handler-process consumes the signal on Wait-return and emits the single terminal `agent_failed` per CHB-020.

The non-blocking findings were:

4. **Missing daemon-side durability requirement** — added CHB-023 (daemon persists `claude_session_id` into `Run.context` via a checkpoint-commit-class durable transition before the handler is permitted to exec Claude). Companion amendment added to execution-model.md §4.3 EM-015d replacing the old `--output-format json` capture wording.
5. **Orphan-reconnect lookup discipline** — added clarifying clause (e) to HC-045c requiring git-derived (per EM-031) `claude_session_id` resolution, forbidding JSONL-tail reads.
6. **Settings-precedence shadowing** — promoted OQ-CHB-004 (`settings.local.json` silently disabling hooks) to normative CHB-024 with a startup-verification check and `bridge_settings_shadowed` error sub-reason.
7. **CHB-013 5-way bead split collapsed** — single bead with 5 sub-bullets per the EM-038 pattern, since the underlying implementation is a cohesive switch statement.
8. **Impl bead split** — `chb-impl-handler-launch` split into `chb-impl-handler-launch-prep` (CHB-006/007/008) and `chb-impl-handler-wait` (CHB-018/019/020) per the launch-prep / Wait-handler natural boundary.
9. **Sensor descriptions** — CHB-INV-001 / CHB-INV-002 sensor beads tightened to name the actual test path under `internal/daemon/reviewloop_test.go`.
10. **PL-017a placeholder text** — placeholder ID and rationale parenthetical stripped from the PL amendment heading; final ID assigned.
11. **CLAUDE_CODE_SKIP_PROMPT_HISTORY parity** — HC-045c (c) explicitly forbids setting this env var, matching CHB-007.

Bead count after corrections: **43** (down from 44, per the count breakdown in 07-tasks.md §Counts).
