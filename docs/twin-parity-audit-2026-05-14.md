# Twin Parity Audit — 2026-05-14

**Against:** operational 8-stage flow, 11 umbrella fixes (docs/historical/dogfood-smoke-traces/dogfood-smoke-run-2026-05-14-operational-green.md)
**Twin under review:** cmd/harmonik-twin-claude (main.go, scriptdriver.go, scenarios.go, wire.go)
**Refs:** hk-cno1z, hk-wuu5h

---

## 1. Summary

Of the 11 operational-green fixes, **5 are twin-feasible today** (the twin can exercise their regression path via YAML script + UDS/stdout NDJSON without any change), **4 require twin extensions** (small, bounded additions to the twin binary or its launch protocol), and **2 are real-claude-only** (the mechanism involves physical tmux pane render timing or OS-level terminal acceptance that no wire protocol can substitute). The single most impactful extension — making the twin read the worktree `.claude/settings.json` and call the hook commands it declares — unlocks 2 additional fixes and is the recommended first extension target.

---

## 2. Eight-Stage Flow Map

The operational-green doc describes 7 mechanism steps. The scenario-testing umbrella (hk-wuu5h) frames these as an 8-stage flow. The table below uses the 8-stage decomposition inferred from the event stream and mechanism description.

| Stage | Name | Twin coverage today | Mechanism | Gap notes |
|-------|------|---------------------|-----------|-----------|
| S1 | daemon_started + orphan_sweep | yes | harness boots daemon; twin not involved | covered by daemon boot path |
| S2 | bead_picked + workspace setup (agent-task.md, trust, settings.json) | partial | daemon side: trust + task file = daemon logic; settings.json materialization = daemon logic; twin does not need to read these to cover S2 wire events | twin exercises nothing at S2; regression coverage requires daemon integration test, not twin |
| S3 | claude launch (handler.Launch, PID store) | yes | twin is launched by daemon in place of real claude via handler.Launch path | single-happy-path scenario covers handler_capabilities + agent_ready sequence |
| S4 | splash dismiss (SendEnterToLastPane + 750ms delay) | no | requires tmux pane + physical Enter key delivery; twin has no terminal | real-claude-only; see §5 |
| S5 | paste-inject (pasteInjectOnLaunch after agent_ready) | extension-needed | twin receives paste-inject via stdin or starts working on agent_ready; needs to emit agent_output_chunk to confirm receipt | twin currently emits nothing that proves it received the paste; extension: twin reads and acks a start-cue |
| S6 | claude works + commits (Edit + Bash + git commit) | extension-needed | twin needs to write a real commit to the worktree HEAD on YAML cue; today twin emits only wire messages, never touches the filesystem | required for quit-on-commit polling (S7) to fire |
| S7 | daemon /quit-on-commit (pasteInjectQuitOnCommit polls HEAD, sends /quit Enter) | extension-needed | pasteInjectQuitOnCommit polls the worktree git HEAD every 500ms; fires when HEAD changes; twin must commit on cue to trigger this path; separately: twin must call the Stop hook (via settings.json) when /quit arrives | S6 commit-on-cue + hook-call are the two sub-requirements |
| S8 | Stop hook + bead_closed + sess.Wait unblocks | extension-needed | hook-relay fires on Stop; twin must READ .claude/settings.json and CALL the hook commands (user clarification hk-wuu5h); outcome_emitted + agent_completed are already in twin's wire repertoire | today twin emits the wire events but does NOT call the hooks; hook-call extension unlocks this |

---

## 3. Eleven-Fix Gap Table

### Fix 1 — hk-lj1p9.3: Trust env-override + flock (EnsureWorktreeTrust isolation)

| Field | Value |
|-------|-------|
| Stage | S2 (workspace setup — trust) |
| Twin coverage | yes |
| Mechanism | Trust isolation is daemon-side logic; the twin is not involved. A scenario test asserts that daemon start does not block on a trust dialog. Twin emits handler_capabilities + agent_ready normally; if trust logic fires a dialog the scenario times out. |
| Extension needed | none |

### Fix 2 — hk-tjl40: Socket bind (RunSocketListener wired in daemon.Start)

| Field | Value |
|-------|-------|
| Stage | S3 (claude launch path — socket precondition) |
| Twin coverage | yes |
| Mechanism | Twin dials the UDS at --socket-path. If the socket was never bound, dialSocket returns an error and the twin exits 1; the scenario fails on agent_ready timeout. Regression is caught without any twin change. |
| Extension needed | none |

### Fix 3 — hk-smuku: Wait uses stored PID (decouple Wait liveness from tmux name resolution)

| Field | Value |
|-------|-------|
| Stage | S8 (sess.Wait unblocks) |
| Twin coverage | yes |
| Mechanism | Twin exits 0 after emitting agent_completed. sess.Wait blocks on the PID (now stored at launch). If this regresses to tmux-name resolution, sess.Wait hangs and the scenario times out. No twin change required. |
| Extension needed | none |

### Fix 4 — hk-lj1p9.4: SetAgentReadyCallback + pasteInjectOnLaunch wired in beadRunOne

| Field | Value |
|-------|-------|
| Stage | S3/S5 (agent_ready callback → paste-inject) |
| Twin coverage | partial |
| Mechanism | Twin emits agent_ready; daemon fires pasteInjectOnLaunch. Today the twin emits agent_ready and immediately continues the script regardless of whether paste-inject was received. The scenario can assert that agent_ready arrives, but cannot assert that pasteInjectOnLaunch actually fired and was received. |
| Extension needed | Twin should expose a cue-acknowledgment mechanism: after agent_ready, wait for a configurable delay or a control message on the wire (e.g., daemon sends a paste-inject control message) before proceeding. Alternatively, a YAML field `wait_for_paste_cue: true` causes the twin to pause until stdin delivers the paste text, then emit agent_output_chunk with a receipt payload. |

### Fix 5 — hk-yngq2: Pane ID stable target (use pane %NNNN not slash-path window name)

| Field | Value |
|-------|-------|
| Stage | S4/S5 (splash dismiss + paste-inject targeting) |
| Twin coverage | no |
| Mechanism | This fix is about tmux send-keys targeting: the daemon uses %NNNN pane IDs. The twin never receives tmux send-keys; it receives stdin. If the daemon regresses to window-name targeting, the paste silently fails at the tmux layer — there is no wire signal the twin can detect. |
| Extension needed | real-claude-only — see §5. |

### Fix 6 — hk-o5eww: EvalSymlinks trust key (canonicalize worktreePath before trust lookup)

| Field | Value |
|-------|-------|
| Stage | S2 (workspace setup — trust lookup) |
| Twin coverage | yes |
| Mechanism | Trust lookup is daemon-side; twin not involved. A scenario that runs a worktree at a symlinked path exercises this. If EvalSymlinks is removed, the trust lookup fails and the daemon blocks on a prompt; the twin times out waiting for agent_ready to be triggered. |
| Extension needed | none |

### Fix 7 — hk-zchbu: Paste-inject ordering (inject AFTER waitAgentReady, not before)

| Field | Value |
|-------|-------|
| Stage | S5 (paste-inject ordering) |
| Twin coverage | extension-needed |
| Mechanism | Before the fix, paste was injected before agent_ready fired. Twin can expose this regression if it is configured to refuse input until it has emitted agent_ready: a YAML flag `reject_input_before_ready: true` causes the twin to log or emit a protocol-error wire message if WriteLastPane is called before agent_ready. The scenario can then assert no protocol-error is emitted. |
| Extension needed | Twin YAML flag `reject_input_before_ready: true`: twin emits a `twin_protocol_error` type message if the paste arrives before agent_ready is emitted. This flag is a no-op when not set (backward-compatible). |

### Fix 8 — hk-rf4ux: Splash dismiss (SendEnterToLastPane + 750ms splashDismissDelay)

| Field | Value |
|-------|-------|
| Stage | S4 (splash dismiss) |
| Twin coverage | real-claude-only (with judgment-call exception — see below) |
| Mechanism | The splash dismiss is a tmux send-keys Enter to a live terminal. The twin has no terminal to dismiss. However, the observable downstream effect is that the REPL accepts input after the dismiss delay. |
| Judgment call | A configurable startup-delay knob (`startup_delay_ms` in the twin YAML) can model the dismiss window in time-domain scenarios. This does NOT exercise the tmux send-keys path itself — it only validates that the daemon's 750ms delay does not cause a timeout. The send-keys correctness (does Enter reach the right pane?) is irreducibly tmux-level and cannot be wire-tested. Verdict: the timing aspect is twin-fakeable via startup-delay knob; the pane-delivery correctness is real-claude-only. Track `startup_delay_ms` as a cheap twin extension; track pane-delivery correctness in hk-7uasg or a sibling conformance bead. |

### Fix 9 — hk-53y35: permissions.allow (dangerouslyAllowedPermissions written to settings.json)

| Field | Value |
|-------|-------|
| Stage | S2 (workspace setup — settings.json materialization) |
| Twin coverage | extension-needed |
| Mechanism | The daemon writes `dangerouslyAllowedPermissions` into `.claude/settings.json` before launch. Real claude reads this and suppresses permission dialogs. The twin, per user clarification (hk-wuu5h notes), MUST read `.claude/settings.json` and behave accordingly. For permissions.allow, the behavioral parity means: twin reads settings.json and can assert (or log) that the `dangerouslyAllowedPermissions` key is present. More importantly, by reading settings.json the twin can discover and call the hook commands — see Fix 11. |
| Extension needed | Twin reads `--worktree-path/.claude/settings.json` at startup (new flag: `--worktree-path`). Logs/emits a wire message confirming the permissions field is present. This is the same capability that unlocks Fix 11. |

### Fix 10 — hk-cmybm layer 1: Session completion instruction (agent-task.md ## Session Completion section)

| Field | Value |
|-------|-------|
| Stage | S5/S6 (claude works — task file drives /quit instruction) |
| Twin coverage | yes |
| Mechanism | The twin reads agent-task.md only if told to (it's a YAML-scripted binary). The scenario tests that agent-task.md is written by the daemon before launch (workspace assertion: file exists with ## Session Completion section). The twin does not need to parse agent-task.md; the scenario harness workspace-state predicate covers file presence. No twin change required for the regression path. |
| Extension needed | none |

### Fix 11 — hk-cmybm layer 2: Daemon-side quit injection (pasteInjectQuitOnCommit polls HEAD every 500ms)

| Field | Value |
|-------|-------|
| Stage | S7 (daemon /quit-on-commit → Stop hook) |
| Twin coverage | extension-needed |
| Mechanism | Two sub-requirements: (a) twin commits to worktree HEAD on YAML cue so pasteInjectQuitOnCommit detects the change; (b) twin reads `.claude/settings.json` and calls the Stop hook command when it receives `/quit` (or on a scripted cue), delivering the outcome envelope to the daemon. |
| Extension needed | (a) Commit-on-cue: a YAML `commit_on_cue: true` flag causes the twin, after emitting agent_output_chunk, to write + stage + commit a sentinel file in the worktree before emitting outcome_emitted. The daemon's pasteInjectQuitOnCommit goroutine detects the HEAD change and sends /quit. (b) Hook-call: twin reads `.claude/settings.json` (requires --worktree-path flag from Fix 9 extension), extracts the Stop hook command, and executes it with the appropriate env before emitting agent_completed. This is the primary mechanism that makes the Stop hook → bead_closed path twin-testable. Judgment call: commit-on-cue is feasible and clean. The hook-call extension is the critical path item; without it, S8 cannot be twin-tested. |

---

## 4. Twin Extension List

Proposed bead-shaped scopes for follow-up. These are listed for the orchestrator to convert to beads; this audit does not create them.

1. **settings-json-reader**: Twin accepts `--worktree-path` flag; reads `.claude/settings.json` at startup; emits wire message `twin_settings_loaded` with permissions and hooks fields present/absent. Unlocks: Fix 9, Fix 11b.

2. **hook-call-on-cue**: Twin extracts Stop/PostToolUse hook commands from the loaded settings.json and executes them at the appropriate YAML cue (after outcome_emitted, before agent_completed exit). Unlocks: Fix 11b, S8 end-to-end. Depends on: settings-json-reader.

3. **commit-on-cue**: Twin accepts YAML field `commit_on_cue: true`; after emitting agent_output_chunk, writes a sentinel file + `git commit` in `--worktree-path` before emitting outcome_emitted. Unlocks: Fix 11a, pasteInjectQuitOnCommit regression path.

4. **paste-receipt-cue**: Twin YAML field `wait_for_paste_cue: true`; after agent_ready, pauses until stdin delivers text (the paste), emits `agent_output_chunk` with `chunk_source: paste_receipt`. Unlocks: Fix 4 (pasteInjectOnLaunch ordering regression detection).

5. **reject-input-before-ready**: Twin YAML flag; emits `twin_protocol_error` wire message if WriteLastPane/paste arrives before agent_ready is emitted. Unlocks: Fix 7 (paste-inject ordering regression detection).

6. **startup-delay-knob**: Twin YAML field `startup_delay_ms`; twin sleeps that many ms before emitting handler_capabilities. Models the splash-dismiss window for timeout-sensitivity scenarios. Unlocks: Fix 8 timing aspect.

---

## 5. Real-Claude Conformance Carve-Outs

Two fixes cannot be exercised by any wire-protocol twin:

**Fix 5 — hk-yngq2 (pane %NNNN stable target):** The correctness question is whether `tmux send-keys -t %NNNN` lands in the right pane vs. window-name targeting. This is a tmux topology question; no NDJSON message can observe it. A real-claude conformance test must: spawn a tmux session, run claude, confirm that paste reaches the correct pane (e.g., by asserting REPL picks up the injected prompt). Track under hk-7uasg or a dedicated conformance bead.

**Fix 8 — hk-rf4ux (splash dismiss Enter delivery):** Whether `SendEnterToLastPane` clears the Claude Code welcome splash is a terminal-render question. The twin has no terminal. The startup-delay-knob extension (§4 item 6) covers timing sensitivity only. Pane-delivery correctness requires real claude in a real tmux pane; track under hk-7uasg or a dedicated conformance bead alongside hk-yngq2.

Both carve-outs are already partially addressed by hk-7uasg ("Real-Claude end-to-end review-loop integration test"), which asserts the same event sequence the twin produces but against real claude. hk-7uasg should be updated to explicitly cover pane-ID stability and splash-dismiss timing as named assertions.

---

## 6. Recommended Next-Step Ordering

**First: settings-json-reader + hook-call-on-cue (items 1 + 2)**

Justification: this pair is the critical path. Without the twin calling the Stop hook, S8 is untestable and the scenario harness cannot assert end-to-end bead closure. It also directly implements the user clarification from hk-wuu5h ("the twin should READ [settings.json] and CALL the hook commands in the same way real claude would"). The two beads are tightly coupled (hook-call depends on reader) and together small enough to land in one implementation pass.

**Second: commit-on-cue (item 3)**

Justification: pasteInjectQuitOnCommit is the daemon's primary quit-detection mechanism and Fix 11 is the most recent and fragile operational piece. Verifying it regresses correctly requires the twin to write a real commit. This is a self-contained addition (git operations on the worktree path) with no spec entanglement.

**Third: paste-receipt-cue + reject-input-before-ready (items 4 + 5)**

Justification: these cover paste-inject ordering (Fix 7) and paste receipt confirmation (Fix 4). They are lower risk regressions (both fixes are stable daemon logic), so they can wait until the critical path items (1+2+3) are in.

**Fourth: startup-delay-knob (item 6)**

Justification: Fix 8 timing sensitivity is a low-probability regression. The real-claude conformance path (hk-7uasg) is the right primary coverage for splash-dismiss; the startup-delay-knob is a supplementary twin-side tool that can be added in a small follow-up.
