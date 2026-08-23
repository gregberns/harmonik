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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

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

	// sentinelName, when non-empty, overrides the auto-generated
	// .harmonik-twin-commit-<unix-ns> sentinel file name in commit_on_cue.
	// Set from the step's sentinel_name payload key via runScript.
	// Use the same value in two twins to make both commit the same file,
	// producing the rebase conflict that triggers the daemon's
	// non_ff_merge / rebase_conflict path (hk-q3u57).
	sentinelName string
}

type heartbeatMode string

const (
	heartbeatModeWallClock heartbeatMode = "wall_clock"

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

	// DelayMs is a per-step pre-emit delay honoured in BOTH heartbeat modes
	// (unlike RelativeTimestampMs, which is scripted-mode only). The driver
	// sleeps this many milliseconds — context-aware, so a cancelled ctx exits
	// the sleep cleanly — before processing the step. Absent or zero means no
	// delay (current behaviour; no production drift). Negative values are
	// treated as zero.
	//
	// This is the timing knob the twin-parity property/fuzz harness
	// (WS3-Claude-C) uses to drive controlled per-edge latencies through the
	// same idiom as startup_delay_ms. It composes with RelativeTimestampMs:
	// in scripted mode both delays apply (relative first, then DelayMs).
	//
	// Cite: plans/2026-07-13-code-revamp/M6-PLAN.md §WS3-Claude-C.
	DelayMs int `yaml:"delay_ms,omitempty"`
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
	if sf.HeartbeatMode == "" {
		sf.HeartbeatMode = heartbeatModeWallClock
	}
	if !sf.HeartbeatMode.Valid() {
		return nil, fmt.Errorf("loadScriptFile: %q: unknown heartbeat_mode %q (want %q or %q)",
			path, sf.HeartbeatMode, heartbeatModeWallClock, heartbeatModeScripted)
	}
	for i, msg := range sf.Messages {
		if msg.Type == "" {
			return nil, fmt.Errorf("loadScriptFile: %q: message %d has missing or empty type field (HC-036a)", path, i)
		}
	}
	return &sf, nil
}

const callStopHookStep = "call_stop_hook"

const commitOnCueStep = "commit_on_cue"

const signalInterruptStep = "signal_interrupt"

const holdStep = "hold"

func runScript(ctx context.Context, e *wireEmitter, sf *ScriptFile, cfg scriptRunConfig) error {
	scripted := sf.HeartbeatMode == heartbeatModeScripted
	cfg.emitter = e

	for i, msg := range sf.Messages {
		if scripted && msg.RelativeTimestampMs > 0 {
			delay := time.Duration(msg.RelativeTimestampMs) * time.Millisecond
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		} else {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		if msg.DelayMs > 0 {
			select {
			case <-time.After(time.Duration(msg.DelayMs) * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		if msg.Type == callStopHookStep {
			if err := runCallStopHook(ctx, e, cfg); err != nil {
				return fmt.Errorf("runScript: message %d (type=%q): %w", i, msg.Type, err)
			}
			continue
		}

		if msg.Type == commitOnCueStep {
			stepCfg := cfg
			if name, ok := msg.Payload["sentinel_name"].(string); ok && name != "" {
				stepCfg.sentinelName = name
			}
			if err := runCommitOnCue(ctx, e, stepCfg); err != nil {
				return fmt.Errorf("runScript: message %d (type=%q): %w", i, msg.Type, err)
			}
			continue
		}

		if msg.Type == signalInterruptStep {
			if err := runSignalInterrupt(ctx, e, msg); err != nil {
				return fmt.Errorf("runScript: message %d (type=%q): %w", i, msg.Type, err)
			}
			return nil // stop script after emitting; exit code 0 per CHB-020
		}

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

func emitTwinErrorResult(e *wireEmitter, message string, primary error) error {
	if emitErr := e.emitTwinError(message); emitErr != nil {
		return errors.Join(primary, fmt.Errorf("emit twin_error: %w", emitErr))
	}
	return primary
}

func runCallStopHook(ctx context.Context, e *wireEmitter, cfg scriptRunConfig) error {
	if cfg.settings == nil {
		return emitTwinErrorResult(e,
			"call_stop_hook: settings not loaded (--worktree-path was not supplied)",
			fmt.Errorf("call_stop_hook: settings not loaded; --worktree-path is required for this step"))
	}
	if !cfg.settings.stopHookPresent {
		return emitTwinErrorResult(e,
			"call_stop_hook: no Stop hook command found in .claude/settings.json",
			fmt.Errorf("call_stop_hook: no Stop hook command in settings.json"))
	}

	exitCode, durationMs := callStopHook(ctx, cfg.settings.stopHookCommand, cfg.worktreePath)
	if err := e.emitTwinHookCalled("Stop", exitCode, durationMs); err != nil {
		return fmt.Errorf("call_stop_hook: emit twin_hook_called: %w", err)
	}
	return nil
}

func runCommitOnCue(ctx context.Context, e *wireEmitter, cfg scriptRunConfig) error {
	if cfg.worktreePath == "" {
		return emitTwinErrorResult(e,
			"commit_on_cue: --worktree-path was not supplied",
			fmt.Errorf("commit_on_cue: --worktree-path is required for this step"))
	}

	ts := strconv.FormatInt(time.Now().UnixNano(), 10)
	sentinelName := ".harmonik-twin-commit-" + ts
	if cfg.sentinelName != "" {
		sentinelName = cfg.sentinelName
	}
	sentinelPath := filepath.Join(cfg.worktreePath, sentinelName)
	sentinelContent := "commit-on-cue " + ts + "\n"

	//nolint:gosec // G306: sentinel file is world-readable; not sensitive.
	if err := os.WriteFile(sentinelPath, []byte(sentinelContent), 0o644); err != nil {
		return emitTwinErrorResult(e,
			"commit_on_cue: write sentinel: "+err.Error(),
			fmt.Errorf("commit_on_cue: write sentinel %q: %w", sentinelPath, err))
	}

	start := time.Now()

	gitEnv := append(os.Environ(),
		"GIT_AUTHOR_NAME=harmonik-twin",
		"GIT_AUTHOR_EMAIL=twin@harmonik.local",
		"GIT_COMMITTER_NAME=harmonik-twin",
		"GIT_COMMITTER_EMAIL=twin@harmonik.local",
	)

	addCmd := exec.CommandContext(ctx, "git", "add", sentinelName) //nolint:gosec // G204: sentinelName is a timestamp-derived literal
	addCmd.Dir = cfg.worktreePath
	addCmd.Env = gitEnv
	if addOut, addErr := addCmd.CombinedOutput(); addErr != nil {
		stderrExcerpt := strings.TrimSpace(string(addOut))
		if len(stderrExcerpt) > 200 {
			stderrExcerpt = stderrExcerpt[:200]
		}
		durationMs := int(time.Since(start).Milliseconds())
		if emitErr := e.emitTwinCommitted("", 1, durationMs, stderrExcerpt); emitErr != nil {
			return fmt.Errorf("commit_on_cue: emit twin_committed (git add error): %w", emitErr)
		}
		return nil
	}

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
		return nil
	}

	revCmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
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

func runSignalInterrupt(ctx context.Context, e *wireEmitter, msg ScriptMessage) error {
	errorCategory, ok := msg.Payload["error_category"].(string)
	if !ok || errorCategory == "" {
		return emitTwinErrorResult(e,
			"signal_interrupt: error_category is required and must be non-empty",
			fmt.Errorf("signal_interrupt: error_category missing or empty"))
	}
	reason, ok := msg.Payload["reason"].(string)
	if !ok || reason == "" {
		return emitTwinErrorResult(e,
			"signal_interrupt: reason is required and must be non-empty",
			fmt.Errorf("signal_interrupt: reason missing or empty"))
	}
	var delayMs int
	switch v := msg.Payload["delay_ms"].(type) {
	case int:
		delayMs = v
	case float64:
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
	if err := e.emitAgentFailed("", "", time.Now().UTC(), errorCategory, reason, ""); err != nil {
		return fmt.Errorf("signal_interrupt: emit agent_failed: %w", err)
	}
	return nil
}

func runHold(ctx context.Context, msg ScriptMessage) {
	var holdMs int
	switch v := msg.Payload["hold_ms"].(type) {
	case int:
		holdMs = v
	case float64:
		holdMs = int(v)
	}
	if holdMs <= 0 {
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

func emitScriptMessage(e *wireEmitter, msg ScriptMessage) error {
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
