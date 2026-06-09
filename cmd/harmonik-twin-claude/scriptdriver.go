// Script-driver loop for the canonical twin binary.
//
// This file implements the script-file reader and message-stream emitter that
// drive the twin subprocess in scenario-mode, satisfying the carve-out declared
// in specs/handler-contract.md §4.6.HC-026a (scripted heartbeat mode) and the
// twin-parity surface of §4.8.HC-036 (subprocess script drives output instead
// of an LLM).
//
// # Script-file schema
//
// The normative definition of the script-file format lives in
// specs/handler-contract.md §4.8.HC-036a (authored by bead hk-ahvq.48.11).
// The types and constants below MUST match that spec exactly; any drift is a
// twin-parity violation per §4.8.HC-036. The summary below is provided for
// local reference only; the spec is authoritative in case of disagreement.
//
// File location:
//
//	<fixture-root>/<scenario>/twin-scripts/<role>.yaml
//
// Top-level YAML fields:
//
//	heartbeat_mode   string   "wall_clock" | "scripted" (default: "wall_clock")
//	                          Per HC-026a scripted-mode carve-out: "scripted"
//	                          allows heartbeats at explicit relative timestamps,
//	                          bypassing the T/2 wall-clock timer so that scenario
//	                          tests produce byte-reproducible event streams.
//	                          MUST be declared on the script when using scripted
//	                          heartbeats; absence means "wall_clock".
//	messages         list     Ordered list of ScriptMessage records (see below).
//
// ScriptMessage record fields:
//
//	type                  string   Required. One of the progress-stream message
//	                               types declared in handler-contract.md §4.2
//	                               (e.g., "agent_heartbeat", "agent_output_chunk",
//	                               "outcome_emitted"). The script-driver emits
//	                               this type verbatim; the watcher validates it.
//	payload               map      Optional. Key-value pairs merged into the
//	                               emitted JSON object alongside "type". Callers
//	                               MUST include all fields required by the wire
//	                               schema for the declared type (HC-007, §6.4,
//	                               event-model §8.3.*); the driver does not
//	                               synthesise missing fields.
//	relative_timestamp_ms int      Optional. Milliseconds from the previous
//	                               message (or script start for the first
//	                               message) to wait before emitting this message.
//	                               Ignored when heartbeat_mode is "wall_clock".
//	                               MUST be >= 0. A value of 0 means "emit
//	                               immediately after the previous message."
//
// # Scripted heartbeat carve-out (HC-026a)
//
// When heartbeat_mode is "scripted", the driver emits "agent_heartbeat"
// messages at the relative_timestamp_ms offsets declared in the script,
// bypassing the T/2 wall-clock timer. The driver enforces the carve-out
// condition: heartbeat_mode MUST be "scripted" on the script (not just
// inferred). This allows scenario tests to produce byte-reproducible event
// streams without depending on system clock jitter.
//
// Cite: specs/handler-contract.md §4.6.HC-026a, §4.8.HC-036, §4.8.HC-036a.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// scriptRunConfig carries optional state needed by advanced script steps.
// Zero value is valid: advanced steps that require populated fields will emit
// an error wire message when those fields are absent.
type scriptRunConfig struct {
	// emitter is the wire emitter used by runScript to emit messages.
	// Set by runScript before calling emitScriptMessage.
	emitter *wireEmitter

	// settings holds the parsed .claude/settings.json, or nil when
	// --worktree-path was not supplied or settings could not be loaded.
	settings *cloneSettings

	// worktreePath is the operator-supplied --worktree-path value.
	// Used as cwd when executing hook commands.
	worktreePath string
}

// twinScriptFixture — per-bead helper prefix for test helpers in this file.
// (Actual test helpers are in scriptdriver_test.go; the prefix is declared here
// as a godoc anchor per implementer-protocol.md §Helper-prefix discipline.)

// ────────────────────────────────────────────────────────────────────────────
// Schema types (de-facto contract; see package godoc for normative reference)
// ────────────────────────────────────────────────────────────────────────────

// heartbeatMode is the enum controlling how heartbeats are driven.
//
// Values: wall_clock (real-time T/2 timer) or scripted (relative timestamps
// from the script).  The default is wall_clock per §4.8.HC-036a.
//
// Cite: specs/handler-contract.md §4.6.HC-026a (carve-out), §4.8.HC-036a (schema).
type heartbeatMode string

const (
	// heartbeatModeWallClock uses the real-time T/2 wall-clock timer for
	// heartbeat emission (HC-026a default; used by real handlers and resilience
	// tests per §10.2 HC-026 obligations).
	heartbeatModeWallClock heartbeatMode = "wall_clock"

	// heartbeatModeScripted drives heartbeats from relative_timestamp_ms values
	// declared in the script.  MUST be declared on the script (HC-026a
	// scripted-mode carve-out).  Limited to the canonical twin binary.
	heartbeatModeScripted heartbeatMode = "scripted"
)

// Valid reports whether hm is a declared heartbeatMode constant.
func (hm heartbeatMode) Valid() bool {
	switch hm {
	case heartbeatModeWallClock, heartbeatModeScripted:
		return true
	default:
		return false
	}
}

// ScriptMessage is one entry in the script's messages list.
//
// All fields map directly to the normative schema at
// specs/handler-contract.md §4.8.HC-036a.
type ScriptMessage struct {
	// Type is the progress-stream message type (e.g., "agent_heartbeat").
	// Required; non-empty. The driver emits this value verbatim as the "type"
	// field per HC-007 NDJSON framing.
	Type string `yaml:"type"`

	// Payload holds additional key-value pairs merged into the emitted JSON
	// object.  nil means no extra fields beyond "type".  Callers MUST include
	// all required wire-schema fields for the declared Type.
	Payload map[string]any `yaml:"payload,omitempty"`

	// RelativeTimestampMs is the milliseconds to wait before emitting this
	// message, measured from the previous message (or script start for the
	// first message).  Only honoured when heartbeat_mode is "scripted".
	// MUST be >= 0; negative values are treated as 0 (immediate).
	RelativeTimestampMs int `yaml:"relative_timestamp_ms,omitempty"`
}

// ScriptFile is the top-level type parsed from a twin script YAML file.
//
// File location: <fixture-root>/<scenario>/twin-scripts/<role>.yaml.
// Normative schema: specs/handler-contract.md §4.8.HC-036a.
type ScriptFile struct {
	// HeartbeatMode controls how heartbeats are timed (see heartbeatMode).
	// Defaults to "wall_clock" when absent or empty.
	HeartbeatMode heartbeatMode `yaml:"heartbeat_mode"`

	// StartupDelayMs is the number of milliseconds to sleep after initial
	// flag-parse but BEFORE emitting handler_capabilities (audit item 6).
	// When zero the twin emits handler_capabilities immediately (default).
	// Models the splash-dismiss window for daemon-side timeout-sensitivity
	// scenarios. Does NOT exercise the tmux pane-delivery path (that is
	// real-claude-only per docs/twin-parity-audit-2026-05-14.md §5).
	// The sleep is context-aware: if ctx is cancelled mid-sleep the twin
	// exits cleanly.
	// Cite: docs/twin-parity-audit-2026-05-14.md §4 item 6 (hk-8ys88).
	StartupDelayMs int `yaml:"startup_delay_ms"`

	// Messages is the ordered list of progress-stream messages to emit.
	// nil or empty means no messages are emitted (the driver exits immediately).
	Messages []ScriptMessage `yaml:"messages"`

	// ExitWithError, when true, causes runScript to return a non-nil error after
	// all messages have been emitted.  main.go maps a non-nil runScript error to
	// exit code 1.  Use this in scenarios that simulate a handler-fatal failure
	// where the handler process must exit non-zero so the work loop takes the
	// ReopenBead branch (exit=0 is auto-closed by the CHB-020 fallback heuristic).
	ExitWithError bool `yaml:"exit_with_error"`
}

// loadScriptFile reads and parses the YAML script file at path.
//
// Returns an error if the file cannot be read, the YAML is malformed, or
// heartbeat_mode is an unrecognised value.
func loadScriptFile(path string) (*ScriptFile, error) {
	//nolint:gosec // G304: path is operator-supplied via --script-path flag; provenance is the scenario harness
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("loadScriptFile: read %q: %w", path, err)
	}
	var sf ScriptFile
	if err := yaml.Unmarshal(raw, &sf); err != nil {
		return nil, fmt.Errorf("loadScriptFile: parse %q: %w", path, err)
	}
	// Apply default: absent or empty heartbeat_mode means wall_clock.
	if sf.HeartbeatMode == "" {
		sf.HeartbeatMode = heartbeatModeWallClock
	}
	if !sf.HeartbeatMode.Valid() {
		return nil, fmt.Errorf("loadScriptFile: %q: unknown heartbeat_mode %q (want %q or %q)",
			path, sf.HeartbeatMode, heartbeatModeWallClock, heartbeatModeScripted)
	}
	// Validate each message: type is required and MUST be non-empty per HC-036a.
	// The watcher validates message types on receipt; the driver rejects scripts
	// with missing or empty type fields at load time so failures are fast and
	// clear (spec: §4.8.HC-036a; test obligation: §10.2 HC-035..HC-038).
	for i, msg := range sf.Messages {
		if msg.Type == "" {
			return nil, fmt.Errorf("loadScriptFile: %q: message %d has missing or empty type field (HC-036a)", path, i)
		}
	}
	return &sf, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Script-driver loop
// ────────────────────────────────────────────────────────────────────────────

// callStopHookStep is the type constant for the YAML step that invokes the
// loaded Stop hook. Declared as a constant so tests and callers can reference
// it without magic strings.
const callStopHookStep = "call_stop_hook"

// commitOnCueStep is the type constant for the YAML step that writes a
// sentinel file and runs git commit in the worktree. Declared as a constant
// so tests and callers can reference it without magic strings.
// Cite: docs/twin-parity-audit-2026-05-14.md §4 item 3 (hk-8ys88).
const commitOnCueStep = "commit_on_cue"

// signalInterruptStep is the type constant for the YAML step that emits
// agent_failed with configurable error_category and reason fields, optionally
// sleeping first to simulate a running agent being interrupted.
//
// This step exercises the daemon's drain-to-cancelled path (CHB-018 §7.1).
// The twin exits with code 0 after emitting the event so that the bead is NOT
// auto-reopened by the CHB-020 fallback heuristic (exit=0 → treat as clean).
//
// Payload fields (all read from ScriptMessage.Payload):
//
//	error_category  string   Required. Maps to HC-020 sentinel classes.
//	reason          string   Required. Human-readable reason string.
//	delay_ms        int      Optional. Milliseconds to sleep before emitting.
//	                         Simulates a running agent being interrupted mid-flight.
//	                         Context-aware: cancellation during sleep returns ctx.Err().
//
// Cite: specs/handler-contract.md §4.6.HC-024, §4.5.HC-020, CHB-018 §7.1.
const signalInterruptStep = "signal_interrupt"

// holdStep is the type constant for the YAML step that BLOCKS the twin process
// (keeping the OS process — and therefore the tmux pane child process — alive)
// for a configurable duration WITHOUT committing or emitting a terminal event.
//
// This is the watchdog-engaging primitive for the hk-37giq concurrent-dispatch
// regression guard (validation-net VN2, bead hk-he18w). Unlike single-happy-path
// (which commits/quits immediately), commit-on-cue (which commits after ~100ms),
// or silent-hang (which stops heartbeating and trips the HC-056 silent-hang
// detector), the hold step keeps the process ALIVE so:
//
//  1. On the stdout-watcher exec path: the daemon's stdout watcher goroutine
//     stays live (the subprocess has not exited), so waitAgentReady's drain
//     goroutine continues to contend on the per-run event tap.
//  2. On the tmux-substrate path: the pane child process stays alive, so
//     perRunSubstrate.PaneHasActiveProcess returns true — keeping the
//     pasteInjectQuitOnCommit launch-suppression branch (internal/daemon/
//     pasteinject.go:679 "pane has active child") active. That is the exact
//     condition the tapCh competing-consumer starve needed: with the first
//     heartbeat never reaching the watchdog (stolen by waitAgentReady's drainer
//     on the pre-53ead2aa single-shared-channel tap), the launch deadline reset
//     loops forever and the run wedges (launch_stall_detected → run_stale).
//
// The step is deliberately a no-terminal-event blocker: the daemon cancels the
// run's context (on completion budget, watchdog kill, or daemon shutdown), which
// unblocks the hold and exits the twin cleanly. Emit at least one agent_heartbeat
// BEFORE the hold step in the scenario so the heartbeat-stream path is exercised.
//
// Payload fields (all read from ScriptMessage.Payload):
//
//	hold_ms  int  Optional. Maximum milliseconds to block before returning.
//	              0 (the default) means block until ctx is cancelled. A positive
//	              value bounds the hold so the twin self-terminates even if the
//	              daemon never cancels (defensive; the daemon normally cancels
//	              first via its watchdog/shutdown path).
//
// Cite: validation-net VN2 (hk-he18w); internal/daemon/pasteinject.go:679.
const holdStep = "hold"

// runScript drives the wireEmitter through the ordered message list in sf.
//
// For each ScriptMessage:
//   - If sf.HeartbeatMode is "scripted" and RelativeTimestampMs > 0, the
//     driver waits that many milliseconds (or until ctx is cancelled) before
//     emitting.  This implements the HC-026a scripted-mode carve-out.
//   - If sf.HeartbeatMode is "wall_clock", relative timestamps are ignored
//     and messages are emitted immediately in declaration order.
//   - If the message type is "call_stop_hook", the driver executes the Stop
//     hook loaded at startup and emits twin_hook_called instead of the raw
//     message type (cfg.settings must be non-nil and stopHookPresent).
//
// runScript returns the first emit error encountered, or ctx.Err() if the
// context is cancelled before the stream completes.
func runScript(ctx context.Context, e *wireEmitter, sf *ScriptFile, cfg scriptRunConfig) error {
	scripted := sf.HeartbeatMode == heartbeatModeScripted
	cfg.emitter = e

	for i, msg := range sf.Messages {
		// Respect relative delay in scripted mode only.
		if scripted && msg.RelativeTimestampMs > 0 {
			delay := time.Duration(msg.RelativeTimestampMs) * time.Millisecond
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		} else {
			// In wall_clock mode (or zero delay) still honour cancellation.
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		// call_stop_hook is a special step handled by the script runner, not
		// emitted verbatim. It executes the loaded Stop hook and emits
		// twin_hook_called with the exit code and duration.
		if msg.Type == callStopHookStep {
			if err := runCallStopHook(ctx, e, cfg); err != nil {
				return fmt.Errorf("runScript: message %d (type=%q): %w", i, msg.Type, err)
			}
			continue
		}

		// commit_on_cue is a special step that writes a sentinel file and runs
		// git commit in the worktree, emitting twin_committed with the result.
		// This lets pasteInjectQuitOnCommit detect a HEAD change and fire /quit.
		// Cite: docs/twin-parity-audit-2026-05-14.md §4 item 3 (hk-8ys88).
		if msg.Type == commitOnCueStep {
			if err := runCommitOnCue(ctx, e, cfg); err != nil {
				return fmt.Errorf("runScript: message %d (type=%q): %w", i, msg.Type, err)
			}
			continue
		}

		// signal_interrupt emits agent_failed with configurable error_category and
		// reason, optionally after a delay, to exercise the daemon's
		// drain-to-cancelled path (CHB-018 §7.1). Returns nil so exit code is 0.
		if msg.Type == signalInterruptStep {
			if err := runSignalInterrupt(ctx, e, msg); err != nil {
				return fmt.Errorf("runScript: message %d (type=%q): %w", i, msg.Type, err)
			}
			return nil // stop script after emitting; exit code 0 per CHB-020
		}

		// hold blocks the twin process (keeping the pane child alive) until ctx is
		// cancelled or hold_ms elapses, WITHOUT committing or emitting a terminal
		// event. This is the watchdog-engaging step for the hk-37giq regression
		// guard (VN2). After the hold returns (ctx cancelled by the daemon, or
		// hold_ms elapsed) the script stops; the twin exits 0.
		if msg.Type == holdStep {
			runHold(ctx, msg)
			return nil // stop script after the hold; exit code 0
		}

		if err := emitScriptMessage(e, msg); err != nil {
			return fmt.Errorf("runScript: message %d (type=%q): %w", i, msg.Type, err)
		}
	}
	if sf.ExitWithError {
		return fmt.Errorf("scenario exit_with_error: handler-fatal simulation")
	}
	return nil
}

// runCallStopHook handles the call_stop_hook script step.
//
// Error policy per bead spec:
//   - cfg.settings nil (--worktree-path never set) → emit twin_error + return error (caller exits 1).
//   - settings loaded but stopHookPresent false → emit twin_error + return error (caller exits 1).
//   - Hook executed and exits non-zero → emit twin_hook_called with code, no error (do NOT exit twin).
func runCallStopHook(ctx context.Context, e *wireEmitter, cfg scriptRunConfig) error {
	if cfg.settings == nil {
		// Settings were never loaded (--worktree-path not supplied).
		_ = e.emitTwinError("call_stop_hook: settings not loaded (--worktree-path was not supplied)")
		return fmt.Errorf("call_stop_hook: settings not loaded; --worktree-path is required for this step")
	}
	if !cfg.settings.stopHookPresent {
		// Settings were loaded but no Stop hook was found.
		_ = e.emitTwinError("call_stop_hook: no Stop hook command found in .claude/settings.json")
		return fmt.Errorf("call_stop_hook: no Stop hook command in settings.json")
	}

	exitCode, durationMs := callStopHook(ctx, cfg.settings.stopHookCommand, cfg.worktreePath)
	if err := e.emitTwinHookCalled("Stop", exitCode, durationMs); err != nil {
		return fmt.Errorf("call_stop_hook: emit twin_hook_called: %w", err)
	}
	// Non-zero exit code is reported but does not fail the script step per bead
	// error policy (real claude doesn't exit on non-zero hook exit either).
	return nil
}

// runCommitOnCue handles the commit_on_cue script step.
//
// It:
//  1. Verifies --worktree-path was set (cfg.worktreePath non-empty), else emits
//     twin_error and returns an error (caller exits 1).
//  2. Writes a sentinel file at <worktree>/.harmonik-twin-commit-<unix-ns>.
//  3. Runs git add + git commit via exec.CommandContext with cwd=worktreePath.
//     Git author/committer identity is set to harmonik-twin / twin@harmonik.local
//     via env vars to avoid touching the project's git config.
//  4. Emits twin_committed with commit_sha, exit_code, duration_ms.
//     Non-zero git exit → emit twin_committed with exit_code and stderr_excerpt;
//     do NOT exit the twin (let the YAML script continue per bead error policy).
//
// Cite: docs/twin-parity-audit-2026-05-14.md §4 item 3 (hk-8ys88).
func runCommitOnCue(ctx context.Context, e *wireEmitter, cfg scriptRunConfig) error {
	if cfg.worktreePath == "" {
		_ = e.emitTwinError("commit_on_cue: --worktree-path was not supplied")
		return fmt.Errorf("commit_on_cue: --worktree-path is required for this step")
	}

	// Use nanosecond timestamp in the filename so parallel invocations don't collide.
	ts := strconv.FormatInt(time.Now().UnixNano(), 10)
	sentinelName := ".harmonik-twin-commit-" + ts
	sentinelPath := filepath.Join(cfg.worktreePath, sentinelName)
	sentinelContent := "commit-on-cue " + ts + "\n"

	//nolint:gosec // G306: sentinel file is world-readable; not sensitive.
	if err := os.WriteFile(sentinelPath, []byte(sentinelContent), 0o644); err != nil {
		_ = e.emitTwinError("commit_on_cue: write sentinel: " + err.Error())
		return fmt.Errorf("commit_on_cue: write sentinel %q: %w", sentinelPath, err)
	}

	start := time.Now()

	// Git author/committer identity set via env vars to avoid touching git config.
	gitEnv := append(os.Environ(), //nolint:gocritic // appendAssign: intentional new slice
		"GIT_AUTHOR_NAME=harmonik-twin",
		"GIT_AUTHOR_EMAIL=twin@harmonik.local",
		"GIT_COMMITTER_NAME=harmonik-twin",
		"GIT_COMMITTER_EMAIL=twin@harmonik.local",
	)

	// git add <sentinelName>
	addCmd := exec.CommandContext(ctx, "git", "add", sentinelName) //nolint:gosec // G204: sentinelName is a timestamp-derived literal
	addCmd.Dir = cfg.worktreePath
	addCmd.Env = gitEnv
	if addOut, addErr := addCmd.CombinedOutput(); addErr != nil {
		stderrExcerpt := strings.TrimSpace(string(addOut))
		if len(stderrExcerpt) > 200 {
			stderrExcerpt = stderrExcerpt[:200]
		}
		durationMs := int(time.Since(start).Milliseconds())
		_ = e.emitTwinCommitted("", 1, durationMs, stderrExcerpt)
		// Non-zero git exit → do NOT return error; let script continue per bead policy.
		return nil
	}

	// git commit -m "twin commit-on-cue at <ts>"
	commitMsg := "twin commit-on-cue at " + ts
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", commitMsg) //nolint:gosec // G204: commitMsg is a timestamp-derived literal
	commitCmd.Dir = cfg.worktreePath
	commitCmd.Env = gitEnv
	commitOut, commitErr := commitCmd.CombinedOutput()
	durationMs := int(time.Since(start).Milliseconds())

	if commitErr != nil {
		stderrExcerpt := strings.TrimSpace(string(commitOut))
		if len(stderrExcerpt) > 200 {
			stderrExcerpt = stderrExcerpt[:200]
		}
		exitCode := extractExitCode(commitErr)
		if err := e.emitTwinCommitted("", exitCode, durationMs, stderrExcerpt); err != nil {
			return fmt.Errorf("commit_on_cue: emit twin_committed (error path): %w", err)
		}
		// Non-zero git commit → do NOT exit twin per bead error policy.
		return nil
	}

	// Extract the commit SHA from HEAD.
	revCmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD") //nolint:gosec // G204: constant args
	revCmd.Dir = cfg.worktreePath
	revCmd.Env = gitEnv
	shaOut, shaErr := revCmd.Output()
	commitSHA := ""
	if shaErr == nil {
		commitSHA = strings.TrimSpace(string(shaOut))
	}

	if err := e.emitTwinCommitted(commitSHA, 0, durationMs, ""); err != nil {
		return fmt.Errorf("commit_on_cue: emit twin_committed: %w", err)
	}
	return nil
}

// runSignalInterrupt handles the signal_interrupt script step.
//
// It reads error_category, reason, and delay_ms from msg.Payload, sleeps for
// delay_ms (context-aware), then emits agent_failed with the configured fields.
// Returns nil so the caller exits with code 0 (CHB-020: exit=0 → no auto-reopen).
//
// Error policy:
//   - error_category missing or empty → emit twin_error + return error (caller exits 1).
//   - reason missing or empty → emit twin_error + return error (caller exits 1).
//   - Context cancelled during delay → return ctx.Err() (caller propagates).
//
// Cite: specs/handler-contract.md §4.6.HC-024, §4.5.HC-020, CHB-018 §7.1.
func runSignalInterrupt(ctx context.Context, e *wireEmitter, msg ScriptMessage) error {
	// Extract error_category (required).
	errorCategory, _ := msg.Payload["error_category"].(string)
	if errorCategory == "" {
		_ = e.emitTwinError("signal_interrupt: error_category is required and must be non-empty")
		return fmt.Errorf("signal_interrupt: error_category missing or empty")
	}
	// Extract reason (required).
	reason, _ := msg.Payload["reason"].(string)
	if reason == "" {
		_ = e.emitTwinError("signal_interrupt: reason is required and must be non-empty")
		return fmt.Errorf("signal_interrupt: reason missing or empty")
	}
	// Extract delay_ms (optional; 0 means emit immediately).
	var delayMs int
	switch v := msg.Payload["delay_ms"].(type) {
	case int:
		delayMs = v
	case float64:
		// YAML unmarshals numbers as float64 via map[string]any.
		delayMs = int(v)
	}
	if delayMs > 0 {
		delay := time.Duration(delayMs) * time.Millisecond
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	// Emit agent_failed. run_id and session_id are left empty; this step is
	// intentionally a lightweight injection mechanism — the scenario YAML owns
	// the run/session context at the watcher level, not inside the twin.
	if err := e.emitAgentFailed("", "", time.Now().UTC(), errorCategory, reason, ""); err != nil {
		return fmt.Errorf("signal_interrupt: emit agent_failed: %w", err)
	}
	return nil
}

// runHold handles the hold script step (VN2, hk-he18w).
//
// It blocks (keeping the twin OS process — and thus the tmux pane child — alive)
// until ctx is cancelled or, when hold_ms > 0, until that many milliseconds
// elapse. It emits NO event and makes NO commit: the goal is solely to keep the
// process alive so the daemon's watcher / pane-liveness probe observes an active
// child while the watchdog and waitAgentReady contend on the per-run event tap.
//
// hold_ms semantics:
//   - 0 (default): block until ctx is cancelled (the daemon cancels on its
//     completion budget, watchdog kill, or shutdown). This is the canonical mode
//     for the regression guard.
//   - >0: bound the block so the twin self-terminates even if the daemon never
//     cancels (defensive backstop against a test that forgets to cancel).
func runHold(ctx context.Context, msg ScriptMessage) {
	var holdMs int
	switch v := msg.Payload["hold_ms"].(type) {
	case int:
		holdMs = v
	case float64:
		// YAML / canned-scenario numbers may arrive as float64 via map[string]any.
		holdMs = int(v)
	}
	if holdMs <= 0 {
		// Block until the daemon cancels the run context.
		<-ctx.Done()
		return
	}
	timer := time.NewTimer(time.Duration(holdMs) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// emitScriptMessage serialises one ScriptMessage as a NDJSON-framed JSON object.
//
// The emitted object always contains "type"; all fields from Payload are merged
// in alongside it.  The "type" key in Payload is silently overwritten by msg.Type
// to prevent scripts from spoofing the type field.
func emitScriptMessage(e *wireEmitter, msg ScriptMessage) error {
	// Build the output map: start with the declared payload, then set "type"
	// last so that a script cannot override it from the payload map.
	out := make(map[string]any, len(msg.Payload)+1)
	for k, v := range msg.Payload {
		out[k] = v
	}
	out["type"] = msg.Type

	raw, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("emitScriptMessage: marshal: %w", err)
	}
	raw = append(raw, '\n')
	_, err = e.w.Write(raw)
	return err
}
