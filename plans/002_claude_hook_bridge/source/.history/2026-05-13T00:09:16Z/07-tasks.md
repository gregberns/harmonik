# Pass 7 — Tasks (bead decomposition)

`discipline-version: 0.12` applied. Mnemonic IDs in `chb-NNN` form; Beads-assigned IDs are minted at `br create` time per §2.10. Corpus prefix `hk` per §2.12 (these beads live in the existing `<repo>/.beads/` with prefix `hk`; mnemonics here are plan-level names).

## Spec-parent

- **chb-spec** — Parent bead: "Land claude-hook-bridge spec corpus changes." Holds all CHB-NNN req-beads plus the four cross-spec amendment beads as children.

## §4.1 Settings.json materialization (CHB-001..005)

- **chb-001** (CHB-001) — Materialize `.claude/settings.json` at workspace creation: define the workspace-manager call path and the file's content template. Cross-spec edge → `wm-038` (cite forward). One bead per §2.1 default; the requirement is structural.
- **chb-002** (CHB-002) — Atomic-write discipline for settings.json: temp file + fsync + rename + fsync(parent_dir) ordered before `workspace_leased`. Inherits WM-026's discipline mechanically. One bead.
- **chb-003** (CHB-003) — Settings.json content: declare the 5 required hook entries (SessionStart, Stop, SessionEnd, StopFailure, Notification) with timeout=30s and exec-form `harmonik hook-relay <event-kind>`. One bead with the content as a sub-bullet.
- **chb-004** (CHB-004) — Merge with pre-existing user settings.json: parse, append harmonik's matcher groups, overwrite-with-warning on malformed. Multi-step but §2.2 F8b: cohesive function body (one parse+merge+write transaction). One bead with sub-bullets for the three cases (no-file / existing-valid / existing-malformed).
- **chb-005** (CHB-005) — Gitignore hygiene extension: ensure `.claude/settings.json` is in the worktree's .gitignore. One bead.

## §4.2 Env-var schema (CHB-006..007)

- **chb-006** (CHB-006) — Env-var schema declaration + handler-side population: 13 required+optional env vars per the table. One bead (per §2.1 default; schema-shape requirement).
- **chb-007** (CHB-007) — Forbidden Claude flags enforcement: handler launch path refuses to add `--fork-session`, `--bare`, `--no-session-persistence`; refuses to set `CLAUDE_CODE_SKIP_PROMPT_HISTORY`. One bead.

## §4.3 claude_session_id flow (CHB-008..009)

- **chb-008** (CHB-008) — Pre-generated claude_session_id minting and propagation: mint UUIDv7 for non-resume phases; reuse LaunchSpec.claude_session_id for resume phase; pass to Claude via `--session-id` / `--resume`; include in handler_capabilities payload. One bead (cohesive launch-time discipline).
- **chb-009** (CHB-009) — Reviewer-phase fresh-mint enforcement: handler refuses to inherit reviewer claude_session_id across iterations. One bead — short, but distinct invariant per §2.1.

## §4.4 Relay subcommand (CHB-010..012, CHB-017)

- **chb-010** (CHB-010) — `harmonik hook-relay <event-kind>` subcommand scaffold: cobra/spf13 sub-command registration, argument parsing, stdin JSON read, env consumption. One bead (the umbrella subcommand work).
- **chb-011** (CHB-011) — Out-of-scope event-kind no-op exit. One bead, simple but a distinct conformance invariant.
- **chb-012** (CHB-012) — Stdin payload validation: required-field check (session_id, transcript_path, hook_event_name); mismatch exits with typed stderr. One bead.
- **chb-017** (CHB-017) — Relay exit-code discipline (0 or 1 only); stderr-only diagnostics. One bead.

## §4.5 Hook → progress-message mapping (CHB-013..014)

- **chb-013** (CHB-013) — Mapping table implementation: switch on hook_event_name + payload subfields; construct the right progress-stream message per row. Single bead per §2.2 F8b (one cohesive switch-statement function body, matching the EM-038 pattern), with 5 sub-bullets for the rows:
  - SessionStart → no-op (early-exit branch).
  - Stop → outcome_emitted (with reviewer-vs-implementer phase branching).
  - SessionEnd → no-op (handler emits agent_completed).
  - StopFailure → agent_rate_limited (for `rate_limit`) OR outcome_emitted{kind=FAILURE_SIGNAL} (for all other error_types — the relay MUST NOT emit terminal agent_failed; the handler-process emits the terminal event on Wait-return per CHB-020).
  - Notification → agent_heartbeat (idle_prompt/permission_prompt → waiting_input, other → reasoning).
- **chb-014** (CHB-014) — Reviewer verdict file read + validate: read `.harmonik/review.json`, validate against agent-reviewer schema v1, package into outcome_emitted payload. One bead with sub-bullets for the failure modes (file-absent, malformed).

## §4.6 Daemon-socket protocol (CHB-015..016)

- **chb-015** (CHB-015) — One-shot NDJSON connection regime: dial(5s), write 1 line, read 1 ack, close. Watcher-side acceptor handles per-(run_id, claude_session_id) routing. One bead. Cross-spec edge → `hc-054`.
- **chb-016** (CHB-016) — Daemon_not_ready retry with exponential backoff capped at 25s. One bead. Companion to HC-016a's 60s retry; the 25s cap is bridge-specific (fits inside hook timeout).

## §4.7 Handler-process responsibilities (CHB-018..020)

- **chb-018** (CHB-018) — Pre-Claude-exec emission ordering: handler_capabilities → session_log_location → skills_provisioned → agent_ready, all BEFORE claude exec. §2.2 candidate. F8b cohesive-function-body fires (one launch-prep function body) → ONE bead with 4 sub-bullets.
- **chb-019** (CHB-019) — Timer-driven heartbeat emission at T/2 = 300s. One bead.
- **chb-020** (CHB-020) — Terminal-event emission on Wait-return: agent_completed iff outcome_emitted observed; agent_failed otherwise. §2.2 F8b cohesive — one Wait-handler function body → one bead.

## §4.6 Daemon-side durability (CHB-023)

- **chb-023** (CHB-023) — Daemon-side `claude_session_id` persistence into `Run.context.claude_session_id` via durable-checkpoint commit before returning the connection-accept ACK that gates Claude exec. Cross-spec edge → `em-amend` (EM-015d update), `hc-045c`. One bead.

## §4.8 Twin parity (CHB-021..022)

- **chb-021** (CHB-021) — Twin emits identical wire-format sequence: extend `harmonik-twin-claude` to produce the same NDJSON sequence the bridge synthesizes. One bead.
- **chb-022** (CHB-022) — Daemon-is-twin-blind verification: lint check + scenario test confirming zero `if isTwin` / `if relay` branches in daemon code. §10.2 sensor — one bead.

## §4.9 Settings-precedence verification (CHB-024)

- **chb-024** (CHB-024) — Startup verification check that `.claude/settings.local.json` does not shadow the bridge's hooks (no `disableAllHooks: true`, no shadowing `hooks` block). On verification failure, refuse to exec Claude and emit `agent_failed{sub_reason=bridge_settings_shadowed}`. Promoted from OQ-CHB-004. One bead.

## §5 Invariants (CHB-INV-001..003)

- **chb-inv-001** (CHB-INV-001) — Two-contributor session invariant sensor: scenario test under `internal/daemon/reviewloop_test.go` (or a sibling `internal/daemon/bridge_two_contributor_test.go`) drives a fixture run with a real-Claude (or twin-Claude) session and asserts the per-watcher connection-accept log records ≥1 handler-side long-lived connection AND ≥1 relay-side one-shot connection under the same (run_id, claude_session_id) tuple. One sensor bead.
- **chb-inv-002** (CHB-INV-002) — Single terminal event sensor: scenario test under `internal/daemon/reviewloop_test.go` runs a `StopFailure {error_type=invalid_request}` fixture and asserts the daemon's event log for the run contains exactly one of `{agent_completed, agent_failed}` AND that the relay's own emissions are confined to non-terminal types (`outcome_emitted`, `agent_heartbeat`, `agent_rate_limited`). One sensor bead.
- **chb-inv-003** (CHB-INV-003) — Mechanism-no-cognition assertion: code-grep sensor confirming relay binary imports no LLM SDK. One sensor bead.

## §8 Errors

- **chb-err** — Error-taxonomy registry: 8 sub_reason strings under agent_failed envelope per §8 table. One schema bead per §2.11. Tags `req:CHB-013, req:CHB-015, req:CHB-016, req:CHB-018, req:CHB-020`.

## Cross-spec amendment beads (one per amended spec)

- **chb-hc-amend** — handler-contract.md v0.3.3 → v0.3.4 patch landing HC-045a, HC-045b, HC-045c (gap-filler IDs after HC-045 — note: HC-053 in §6.2 is already occupied) + clarifying sentence on HC-006. Parent: chb-spec. Closure criteria: spec file edited, version+revision-history updated, depguard/CI passes.
- **chb-wm-amend** — workspace-model.md v0.4.2 → v0.4.3 patch landing WM-040a (gap-filler after WM-040 — note: WM-038 in §4.10 is already occupied) + WM-013e extension + §4.7 retention note.
- **chb-pl-amend** — process-lifecycle.md v0.4.1 → v0.4.2 patch landing PL-017a. Confirm with `br dep cycles` clean.
- **chb-em-amend** — execution-model.md amendment replacing EM-015d's `--output-format json` post-launch capture mechanism for `claude_session_id` with the pre-exec handler-mint + `handler_capabilities` capture path per CHB-018/CHB-023/HC-045c. Glossary entry for `claude_session_id` also updated. No new requirement IDs.
- **chb-docs-amend** — docs/subsystems/{agent-runner,hook-system}.md + AGENT_INDEX.md row. One bead (informative-only).

## Phase-1 wiring beads (implementation deltas to make the spec executable)

- **chb-impl-relay-binary** — Implement `harmonik hook-relay` subcommand in `cmd/harmonik/` consuming the env+stdin contract. Tests: piped canned hook payloads from each of the 5 supported event kinds. Dep on chb-010..017.
- **chb-impl-handler-launch-prep** — Extend `internal/handler/adapter_claudecode.go` (and a new `internal/handler/launch_claudecode.go` if needed) for the launch-prep surface: env-var population, forbidden-flag enforcement, and pre-exec claude_session_id minting/resume. Covers CHB-006, CHB-007, CHB-008. Dep on those req beads. Tags: `req:CHB-006`, `req:CHB-007`, `req:CHB-008`, `req:HC-045c`.
- **chb-impl-handler-wait** — Extend the same package for the Wait-window surface: pre-Claude-exec emission ordering (`handler_capabilities` → `session_log_location` → `skills_provisioned` → `agent_ready`), timer-driven heartbeat, terminal-event emission on Wait-return (mapping `outcome_emitted{kind=FAILURE_SIGNAL}` to the terminal `agent_failed`). Covers CHB-018, CHB-019, CHB-020. Dep on those req beads. Tags: `req:CHB-018`, `req:CHB-019`, `req:CHB-020`.
- **chb-impl-workspace-settings** — Extend `internal/workspace/` (TBD path) to materialize `.claude/settings.json` per WM-040a. Dep on chb-001..005, chb-wm-amend.
- **chb-impl-watcher-acceptor** — Extend `internal/daemon/` socket acceptor to route one-shot NDJSON connections by (run_id, claude_session_id) envelope, and land the CHB-023 durable-checkpoint write of `Run.context.claude_session_id` before ACKing the handler. Dep on chb-015, chb-023, chb-hc-amend.
- **chb-impl-twin-parity** — Update `twins/harmonik-twin-claude` to emit the same sequence per CHB-021. Dep on chb-021.

## Phase-1 verification beads

- **chb-test-real-claude-loop** — End-to-end integration test: real `claude` binary running review-loop against `internal/daemon/reviewloop.go`. ASSERTS the same event sequence the twin currently produces. Dep on all chb-impl-* beads. This is the load-bearing acceptance criterion for G1.
- **chb-test-implementer-resume** — 3-iteration review-loop test verifying claude_session_id stability across implementer-resume launches. Dep on chb-impl-handler-launch.
- **chb-test-relay-failure** — Test: delete daemon socket mid-session; verify chb-013/chb-015/chb-020 produce agent_failed with `bridge_dial_failed` sub_reason. Dep on chb-impl-*.

## Counts

- Spec req beads: 24 (CHB-001..024)
- Invariant sensor beads: 3 (CHB-INV-001..003)
- Schema beads: 1 (error-taxonomy)
- Step beads under CHB-013: 0 (collapsed per spec-draft-review; CHB-013 is one bead with 5 sub-bullets, matching the EM-038 pattern)
- Cross-spec amendment beads: 5 (HC, WM, PL, EM, docs)
- Impl beads: 6 (relay-binary, handler-launch-prep, handler-wait, workspace-settings, watcher-acceptor, twin-parity)
- Verification beads: 3
- Spec-parent: 1

**Total: 43 beads.** Plus integration-pass-assigned `forward:*` edges to citing specs (HC, WM, PL, EM).

Delta from prior pass (44 → 43): CHB-013 collapsed from umbrella+5 to one bead with sub-bullets (−5); chb-impl-handler-launch split into chb-impl-handler-launch-prep + chb-impl-handler-wait (+1); chb-023 new spec req bead for daemon-side durability (+1); chb-024 new spec req bead promoted from OQ-CHB-004 (+1); chb-em-amend new cross-spec amendment bead for EM-015d's capture-mechanism rewrite (+1).

## Dependency edges (sketch — final wired at finalize)

- chb-spec → parent of all chb-NNN, chb-inv-*, chb-err, chb-hc-amend, chb-wm-amend, chb-pl-amend, chb-em-amend, chb-docs-amend.
- chb-impl-relay-binary `blocks` chb-test-real-claude-loop, chb-test-relay-failure.
- chb-impl-handler-launch-prep `blocks` chb-test-real-claude-loop, chb-test-implementer-resume.
- chb-impl-handler-wait `blocks` chb-test-real-claude-loop, chb-test-implementer-resume, chb-test-relay-failure.
- chb-impl-workspace-settings `blocks` chb-test-real-claude-loop.
- chb-impl-watcher-acceptor `blocks` chb-test-real-claude-loop, chb-test-relay-failure.
- chb-impl-twin-parity `blocks` chb-test-real-claude-loop (the twin-vs-real equivalence is the test's core assertion).
- chb-hc-amend `blocks` chb-impl-handler-launch-prep, chb-impl-handler-wait, chb-impl-watcher-acceptor.
- chb-wm-amend `blocks` chb-impl-workspace-settings.
- chb-em-amend `blocks` chb-023 impl path (the daemon-side durability requirement consumes EM-015d's updated mechanism).
- chb-pl-amend has no impl blocker (clarifying-only).
- Cross-spec `forward:` edges (one per req bead with a §9 cite): chb-001 → wm-040a, chb-002 → wm-026, chb-008 → hc-006/hc-045c, chb-015 → hc-045b, chb-023 → em-015d, etc. Final list at finalize.

## Tags

All beads tagged: `scope:phase-1`, `kerf:claude-hook-bridge`, `spec:claude-hook-bridge` (or `spec:handler-contract` / `spec:workspace-model` / `spec:process-lifecycle` for amendment beads).

Verification beads additionally tagged `phase-1-acceptance` so the verification-gate workflow picks them up.

## Priority

Default `P2` per §2.9 (Beads's default). Override to `P1` on `chb-test-real-claude-loop` because it's the load-bearing G1 acceptance test; everything else flows from it.
