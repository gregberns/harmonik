// Package handler — claude-code handler-process responsibilities (hk-w5vra.5).
//
// This file implements the handler-process obligations for agent_type=claude-code
// per specs/claude-hook-bridge.md §4.2 CHB-006..007, §4.3 CHB-008..009,
// §4.7 CHB-018..020, and §4.9 CHB-024.
//
// # Scope
//
//   - CHB-006: env-var schema set on the Claude subprocess.
//   - CHB-007: forbidden flag/env guard (refuse launch on --fork-session, --bare,
//     --no-session-persistence, CLAUDE_CODE_SKIP_PROMPT_HISTORY).
//   - CHB-008: claude_session_id mint (UUIDv7) for single/implementer-initial/reviewer;
//     reuse from LaunchSpec for implementer-resume; passed via --session-id or --resume.
//   - CHB-009: reviewer always mints fresh, never inherits prior reviewer session.
//   - CHB-018: pre-Claude-exec emission order: handler_capabilities →
//     session_log_location → skills_provisioned → launch_initiated.
//   - CHB-019: timer-driven agent_heartbeat{phase:"reasoning"} every 300 s while Claude alive.
//   - CHB-020: terminal-event mapping on cmd.Wait() return (3 branches).
//   - CHB-024: fail-fast if .claude/settings.local.json shadows bridge hooks.
//
// # Design
//
// The handler-process responsibilities are expressed as a set of pure functions
// (CheckForbiddenFlags, MintClaudeSessionID, CheckSettingsLocalJSON,
// ClaudeEnvVars, BuildPreExecMessages) and one driver function
// (RunClaudeHandlerProcess) that orchestrates them.  Pure functions are unit-tested
// independently; RunClaudeHandlerProcess is integration-tested via the twin binary.
//
// Tags: mechanism. No cognition. All behaviour is deterministic.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// HeartbeatInterval is the timer-driven heartbeat cadence per CHB-019
// (T_silent_hang / 2 = 300 seconds per HC-026a).
const HeartbeatInterval = 300 * time.Second

// forbiddenClaudeFlags is the set of Claude CLI flags that MUST NOT be passed per
// CHB-007, mapped to their prohibition reason.
var forbiddenClaudeFlags = map[string]string{
	"--fork-session":           "would mint a new session_id on resume (CHB-007)",
	"--bare":                   "would disable hook auto-discovery (CHB-007)",
	"--no-session-persistence": "would disable session persistence (CHB-007)",
}

// forbiddenClaudeEnvVars is the set of env var names that MUST NOT be set per
// CHB-007.
var forbiddenClaudeEnvVars = map[string]string{
	"CLAUDE_CODE_SKIP_PROMPT_HISTORY": "same effect as --no-session-persistence (CHB-007)",
}

// CheckForbiddenFlags verifies that argv does not contain any forbidden Claude
// flags per CHB-007, and that env does not contain any forbidden env var names.
//
// env is a slice in "KEY=VALUE" form. Only the KEY portion is inspected.
//
// Returns a non-nil error if any forbidden flag or env var is found; the error
// names the offending item and its prohibition reason. The returned error wraps
// ErrStructural.
//
// Spec: specs/claude-hook-bridge.md §4.2 CHB-007.
func CheckForbiddenFlags(argv []string, env []string) error {
	for _, arg := range argv {
		if reason, bad := forbiddenClaudeFlags[arg]; bad {
			return fmt.Errorf("handler: claude-code: forbidden flag %q: %s: %w",
				arg, reason, ErrStructural)
		}
	}
	for _, kv := range env {
		key := kv
		if idx := strings.IndexByte(kv, '='); idx >= 0 {
			key = kv[:idx]
		}
		if reason, bad := forbiddenClaudeEnvVars[key]; bad {
			return fmt.Errorf("handler: claude-code: forbidden env var %q: %s: %w",
				key, reason, ErrStructural)
		}
	}
	return nil
}

// ClaudeSessionIDResult is returned by MintClaudeSessionID.
type ClaudeSessionIDResult struct {
	// ClaudeSessionID is the session ID to use: either freshly minted (UUIDv7)
	// or reused from LaunchSpec for implementer-resume.
	ClaudeSessionID string

	// ResumeMode is true when the session ID was reused from LaunchSpec (phase=implementer-resume)
	// and Claude should be invoked with --resume instead of --session-id.
	ResumeMode bool
}

// MintClaudeSessionID derives the claude_session_id per CHB-008 and CHB-009:
//
//   - For phase ∈ {single, implementer-initial, reviewer}: mint a fresh UUIDv7.
//   - For phase = implementer-resume: reuse launchSpec.ClaudeSessionID (CHB-008).
//   - Reviewer always mints fresh — it MUST NOT inherit a prior reviewer session (CHB-009).
//
// phase should be the string value of LaunchSpec.Phase (e.g. "reviewer", "implementer-resume").
// For single-mode (LaunchSpec.Phase == nil), pass an empty string.
//
// Returns ErrStructural if phase = implementer-resume but launchSpec.ClaudeSessionID is nil/empty.
// Returns ErrStructural if phase = reviewer and priorClaudeSessionID is non-nil (CHB-009 enforcement:
// the caller MUST NOT pass a prior session ID for reviewer; doing so is a daemon defect).
//
// Spec: specs/claude-hook-bridge.md §4.3 CHB-008, CHB-009.
func MintClaudeSessionID(phase string, priorClaudeSessionID *string) (ClaudeSessionIDResult, error) {
	if phase == string(handlercontract.ReviewLoopPhaseImplementerResume) {
		// Reuse prior session ID (CHB-008).
		if priorClaudeSessionID == nil || *priorClaudeSessionID == "" {
			return ClaudeSessionIDResult{}, fmt.Errorf(
				"handler: claude-code: phase=implementer-resume but LaunchSpec.ClaudeSessionID is absent: %w",
				ErrStructural,
			)
		}
		return ClaudeSessionIDResult{
			ClaudeSessionID: *priorClaudeSessionID,
			ResumeMode:      true,
		}, nil
	}

	// CHB-009 enforcement: reviewer MUST NOT receive a prior session ID.
	// A non-nil priorClaudeSessionID for reviewer is a daemon defect — fail-fast
	// rather than silently ignoring the value, which could mask an accidental
	// inheritance bug in the call site.
	if phase == string(handlercontract.ReviewLoopPhaseReviewer) && priorClaudeSessionID != nil {
		return ClaudeSessionIDResult{}, fmt.Errorf(
			"handler: claude-code: CHB-009: phase=reviewer but priorClaudeSessionID is non-nil (%q): "+
				"reviewer must always mint fresh; passing a prior session ID is a daemon defect: %w",
			*priorClaudeSessionID, ErrStructural,
		)
	}

	// All other phases (single, implementer-initial, reviewer): mint fresh UUIDv7 (CHB-008, CHB-009).
	id, err := uuid.NewV7()
	if err != nil {
		return ClaudeSessionIDResult{}, fmt.Errorf(
			"handler: claude-code: mint claude_session_id UUIDv7: %w: %w", err, ErrStructural)
	}
	return ClaudeSessionIDResult{
		ClaudeSessionID: id.String(),
		ResumeMode:      false,
	}, nil
}

// ClaudeEnvConfig carries the values needed to build the CHB-006 env-var set.
type ClaudeEnvConfig struct {
	// Required env vars (CHB-006).
	RunID            string // HARMONIK_RUN_ID
	DaemonSocket     string // HARMONIK_DAEMON_SOCKET
	WorkspacePath    string // HARMONIK_WORKSPACE_PATH
	HandlerSessionID string // HARMONIK_HANDLER_SESSION_ID
	ClaudeSessionID  string // HARMONIK_CLAUDE_SESSION_ID
	WorkflowID       string // HARMONIK_WORKFLOW_ID
	NodeID           string // HARMONIK_NODE_ID

	// HARMONIK_AGENT_TYPE is always "claude-code" per CHB-006; not configurable.

	// Optional env vars (CHB-006); set when non-empty.
	WorkflowMode   string // HARMONIK_WORKFLOW_MODE  (optional)
	Phase          string // HARMONIK_PHASE          (optional)
	IterationCount string // HARMONIK_ITERATION_COUNT (optional; string form of int)
	BeadID         string // HARMONIK_BEAD_ID        (optional)

	// SecretVars carries HARMONIK_SECRET_* key-value pairs per HC-028.
	// Keys MUST be prefixed with "HARMONIK_SECRET_".
	// Values are included in the env as-is; the handler assumes redaction was
	// applied by the caller per HC-028.
	SecretVars map[string]string

	// BaseEnv is the base environment to pass to the subprocess.  CHB-006 vars
	// are appended (or overwrite). When nil, only CHB-006 vars are set.
	// Callers SHOULD pass os.Environ() or a trimmed-down set.
	// HARMONIK_SECRET_* keys present in BaseEnv are silently dropped (caller
	// must use SecretVars instead to ensure consistent redaction discipline).
	BaseEnv []string
}

// ClaudeEnvVars builds the subprocess env slice per CHB-006.
//
// It starts from cfg.BaseEnv (if provided), removes any HARMONIK_SECRET_* keys
// already present (to prevent double-injection), then appends all required and
// optional CHB-006 vars, and finally appends cfg.SecretVars.
//
// The returned slice is in "KEY=VALUE" form suitable for exec.Cmd.Env.
//
// Spec: specs/claude-hook-bridge.md §4.2 CHB-006.
func ClaudeEnvVars(cfg ClaudeEnvConfig) []string {
	// Start from BaseEnv with HARMONIK_SECRET_* stripped to prevent double-injection.
	var base []string
	for _, kv := range cfg.BaseEnv {
		key := kv
		if idx := strings.IndexByte(kv, '='); idx >= 0 {
			key = kv[:idx]
		}
		if strings.HasPrefix(key, "HARMONIK_SECRET_") {
			continue
		}
		base = append(base, kv)
	}

	// Required vars (CHB-006).
	required := []string{
		"HARMONIK_RUN_ID=" + cfg.RunID,
		"HARMONIK_DAEMON_SOCKET=" + cfg.DaemonSocket,
		"HARMONIK_WORKSPACE_PATH=" + cfg.WorkspacePath,
		"HARMONIK_HANDLER_SESSION_ID=" + cfg.HandlerSessionID,
		"HARMONIK_CLAUDE_SESSION_ID=" + cfg.ClaudeSessionID,
		"HARMONIK_WORKFLOW_ID=" + cfg.WorkflowID,
		"HARMONIK_NODE_ID=" + cfg.NodeID,
		"HARMONIK_AGENT_TYPE=claude-code",
	}
	env := append(base, required...)

	// Optional vars — only set when non-empty (CHB-006).
	if cfg.WorkflowMode != "" {
		env = append(env, "HARMONIK_WORKFLOW_MODE="+cfg.WorkflowMode)
	}
	if cfg.Phase != "" {
		env = append(env, "HARMONIK_PHASE="+cfg.Phase)
	}
	if cfg.IterationCount != "" {
		env = append(env, "HARMONIK_ITERATION_COUNT="+cfg.IterationCount)
	}
	if cfg.BeadID != "" {
		env = append(env, "HARMONIK_BEAD_ID="+cfg.BeadID)
	}

	// Secret vars per HC-028 — appended last so they override any stale
	// HARMONIK_SECRET_* values that leaked through base env.
	for k, v := range cfg.SecretVars {
		env = append(env, k+"="+v)
	}

	return env
}

// settingsLocalJSON is the minimal shape of .claude/settings.local.json that
// we need to inspect for CHB-024.
type settingsLocalJSON struct {
	DisableAllHooks bool                       `json:"disableAllHooks"`
	Hooks           map[string]json.RawMessage `json:"hooks,omitempty"`
}

// CheckSettingsLocalJSON parses ${workspacePath}/.claude/settings.local.json (if
// present) and verifies that it does NOT shadow the bridge's hook entries per CHB-024.
//
// Shadowing is defined as:
//   - (a) the file contains disableAllHooks: true, OR
//   - (b) the file contains a non-empty "hooks" block.
//
// On verification failure returns an error that wraps ErrStructural; the caller
// MUST NOT exec Claude and MUST emit agent_failed{class=ErrStructural,
// sub_reason="bridge_settings_shadowed"}.
//
// If the file does not exist, returns nil (no shadow).
// If the file is present but not valid JSON, returns a wrapped ErrStructural
// (malformed file = shadowing risk, fail-fast per CHB-024).
//
// Spec: specs/claude-hook-bridge.md §4.9 CHB-024.
func CheckSettingsLocalJSON(workspacePath string) error {
	settingsPath := filepath.Join(workspacePath, ".claude", "settings.local.json")
	//nolint:gosec // G304: settingsPath derived from LaunchSpec.workspace_path (operator-controlled)
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No settings.local.json — no shadow (CHB-024).
		}
		// IO error reading the file: fail-fast.
		return fmt.Errorf(
			"handler: claude-code: CHB-024: read settings.local.json: %w: %w",
			err, ErrStructural,
		)
	}

	var s settingsLocalJSON
	if jsonErr := json.Unmarshal(data, &s); jsonErr != nil {
		// Malformed JSON: fail-fast (a malformed file at this path is a shadow risk).
		return fmt.Errorf(
			"handler: claude-code: CHB-024: bridge_settings_shadowed: settings.local.json is not valid JSON: %w: %w",
			jsonErr, ErrStructural,
		)
	}

	if s.DisableAllHooks {
		return fmt.Errorf(
			"handler: claude-code: CHB-024: bridge_settings_shadowed: settings.local.json has disableAllHooks:true: %w",
			ErrStructural,
		)
	}

	if len(s.Hooks) > 0 {
		return fmt.Errorf(
			"handler: claude-code: CHB-024: bridge_settings_shadowed: settings.local.json has a hooks block that would shadow bridge hooks: %w",
			ErrStructural,
		)
	}

	return nil
}

// PreExecMessages returns the 4 ordered progress-stream messages that the claude-code
// handler-process MUST emit BEFORE exec'ing Claude per CHB-018.
//
// Order: handler_capabilities → session_log_location → skills_provisioned → launch_initiated.
//
// Parameters:
//   - runID, sessionID, workflowID, nodeID — IDs to embed in the messages.
//   - claudeSessionID — the claude_session_id minted/reused by MintClaudeSessionID.
//   - logPath — the Claude transcript path for session_log_location (HC-010).
//   - skills — the installed skill entries for skills_provisioned (HC-049).
//
// Returns a slice of 4 compact JSON lines (no trailing newline on each; the caller
// appends '\n' when writing them to the progress stream socket).
//
// Note: step 4 emits launch_initiated (not agent_ready).  Under the interactive
// (tmux) substrate, agent_ready is synthesized by the hook-relay on first
// SessionStart receipt per CHB-013 / HC-039.  launch_initiated MUST NOT
// satisfy the ready-state gate per HC-041.
//
// Spec: specs/claude-hook-bridge.md §4.7 CHB-018.
func PreExecMessages(
	runID string,
	sessionID string,
	nodeID string,
	claudeSessionID string,
	logPath string,
	skills []handlercontract.SkillProvisionedEntry,
) ([][]byte, error) {
	if skills == nil {
		skills = []handlercontract.SkillProvisionedEntry{}
	}

	// 1. handler_capabilities (CHB-018 step 1, HC-009).
	hcMsg := handlercontract.HandlerCapabilitiesMsg{
		Type:              handlercontract.ProgressMsgTypeHandlerCapabilities,
		SupportedVersions: []int{1},
		ClaudeSessionID:   claudeSessionID,
	}
	hcBytes, err := json.Marshal(hcMsg)
	if err != nil {
		return nil, fmt.Errorf("handler: PreExecMessages: marshal handler_capabilities: %w: %w", err, ErrStructural)
	}

	// 2. session_log_location (CHB-018 step 2, HC-010).
	sllMsg := handlercontract.SessionLogLocationMsg{
		Type:      handlercontract.ProgressMsgTypeSessionLogLocation,
		SessionID: sessionID,
		RunID:     runID,
		NodeID:    nodeID,
		AgentType: string(handlercontract.AgentTypeClaudeCode),
		LogPath:   logPath,
		LogFormat: "jsonl",
	}
	sllBytes, err := json.Marshal(sllMsg)
	if err != nil {
		return nil, fmt.Errorf("handler: PreExecMessages: marshal session_log_location: %w: %w", err, ErrStructural)
	}

	// 3. skills_provisioned (CHB-018 step 3, HC-049).
	spMsg := handlercontract.SkillsProvisionedMsg{
		Type:      handlercontract.ProgressMsgTypeSkillsProvisioned,
		RunID:     runID,
		SessionID: sessionID,
		Skills:    skills,
	}
	spBytes, err := json.Marshal(spMsg)
	if err != nil {
		return nil, fmt.Errorf("handler: PreExecMessages: marshal skills_provisioned: %w: %w", err, ErrStructural)
	}

	// 4. launch_initiated (CHB-018 step 4, HC-039).
	// Under the interactive (tmux) substrate the handler emits launch_initiated
	// here (not agent_ready).  The relay synthesizes agent_ready on first
	// SessionStart receipt per CHB-013 / HC-039.
	liMsg := handlercontract.LaunchInitiatedMsg{
		Type:            handlercontract.ProgressMsgTypeLaunchInitiated,
		SessionID:       sessionID,
		ClaudeSessionID: claudeSessionID,
	}
	liBytes, err := json.Marshal(liMsg)
	if err != nil {
		return nil, fmt.Errorf("handler: PreExecMessages: marshal launch_initiated: %w: %w", err, ErrStructural)
	}

	return [][]byte{hcBytes, sllBytes, spBytes, liBytes}, nil
}

// ExportedOutcomeEmittedPayload is the partial shape we need from outcome_emitted
// messages to implement the CHB-020 terminal-event logic.
//
// Exported so that test packages and callers can construct values for testing
// MapWaitReturnToTerminalEvent without an OutcomeObserver.
type ExportedOutcomeEmittedPayload struct {
	Kind           string `json:"kind"`
	SubReason      string `json:"sub_reason,omitempty"`
	SuggestedClass string `json:"suggested_class,omitempty"`
}

// OutcomeObserver tracks the last outcome_emitted message received from relay
// invocations, implementing the last-received-wins dedup rule of CHB-025.
// It is concurrency-safe.
type OutcomeObserver struct {
	mu            sync.Mutex
	latestOutcome *ExportedOutcomeEmittedPayload // nil = no outcome_emitted yet
}

// Record stores the latest outcome_emitted payload, replacing any prior value
// (last-received-wins per CHB-025).
func (o *OutcomeObserver) Record(raw json.RawMessage) {
	var p ExportedOutcomeEmittedPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.latestOutcome = &p
}

// Latest returns a copy of the last recorded outcome, or nil if none was observed.
func (o *OutcomeObserver) Latest() *ExportedOutcomeEmittedPayload {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.latestOutcome == nil {
		return nil
	}
	cp := *o.latestOutcome
	return &cp
}

// TerminalEventPayload is the result of MapWaitReturnToTerminalEvent.
type TerminalEventPayload struct {
	// Type is either ProgressMsgTypeAgentCompleted or ProgressMsgTypeAgentFailed.
	Type string

	// SessionID is the handler session ID.
	SessionID string

	// ExitCode is the Claude subprocess exit code.
	ExitCode int

	// Class is set for agent_failed; one of "structural" or "transient".
	Class string

	// SubReason is the failure sub-reason string for agent_failed (CHB-020 §8).
	SubReason string
}

// MapWaitReturnToTerminalEvent applies the CHB-020 3-branch logic on Wait-return
// to determine which terminal event to emit.
//
// Parameters:
//   - sessionID — the handler session ID.
//   - exitCode  — exit code from cmd.Wait(); 0 = clean exit.
//   - waitErr   — the error returned by cmd.Wait() (non-nil if process exited non-zero).
//   - outcome   — the last outcome_emitted payload observed (nil if none).
//
// Branch 1: outcome ∈ {WORK_COMPLETE, REVIEWER_VERDICT} → agent_completed.
// Branch 2: outcome.Kind = FAILURE_SIGNAL → agent_failed{class=suggested_class, sub_reason}.
// Branch 3: no outcome_emitted → agent_failed{ErrStructural, claude_exit_without_outcome or claude_crashed}.
//
// Spec: specs/claude-hook-bridge.md §4.7 CHB-020.
func MapWaitReturnToTerminalEvent(sessionID string, exitCode int, waitErr error, outcome *ExportedOutcomeEmittedPayload) TerminalEventPayload {
	if outcome != nil {
		switch outcome.Kind {
		case "WORK_COMPLETE", "REVIEWER_VERDICT":
			// Branch 1: clean or dirty exit after a valid outcome.
			return TerminalEventPayload{
				Type:      handlercontract.ProgressMsgTypeAgentCompleted,
				SessionID: sessionID,
				ExitCode:  exitCode,
			}
		case "FAILURE_SIGNAL":
			// Branch 2: failure signal observed.
			class := outcome.SuggestedClass
			if class == "" {
				class = "structural"
			}
			subReason := outcome.SubReason
			if subReason == "" {
				subReason = "claude_failure"
			}
			return TerminalEventPayload{
				Type:      handlercontract.ProgressMsgTypeAgentFailed,
				SessionID: sessionID,
				ExitCode:  exitCode,
				Class:     class,
				SubReason: subReason,
			}
		}
	}

	// Branch 3: no outcome_emitted observed.
	subReason := "claude_crashed"
	if waitErr == nil || exitCode == 0 {
		subReason = "claude_exit_without_outcome"
	}
	return TerminalEventPayload{
		Type:      handlercontract.ProgressMsgTypeAgentFailed,
		SessionID: sessionID,
		ExitCode:  exitCode,
		Class:     "structural",
		SubReason: subReason,
	}
}

// HeartbeatEmitter is the function signature for emitting a heartbeat.
// The caller implements this to write an agent_heartbeat message to the
// progress stream.
//
// Spec: specs/claude-hook-bridge.md §4.7 CHB-019.
type HeartbeatEmitter func(ctx context.Context, sessionID string, phase string) error

// RunHeartbeatLoop runs a timer-driven heartbeat loop per CHB-019.
//
// It emits agent_heartbeat{phase:"reasoning"} every HeartbeatInterval (300 s)
// until ctx is cancelled or done is closed.
//
// done should be closed when the Claude subprocess exits (cmd.Wait returned).
//
// The heartbeat emitter is called synchronously; if it returns an error the
// loop logs the error and continues (heartbeat failures are non-fatal; the
// daemon's silent-hang timer is the authoritative liveness guard).
//
// Spec: specs/claude-hook-bridge.md §4.7 CHB-019; HC-026a.
func RunHeartbeatLoop(ctx context.Context, sessionID string, interval time.Duration, done <-chan struct{}, emit HeartbeatEmitter) {
	// Emit the first heartbeat immediately so pasteInjectQuitOnCommit sees
	// a heartbeat within its 60s launchHeartbeatTimeout window, even though
	// the ticker interval (300s) is much longer.
	if emitErr := emit(ctx, sessionID, string(handlercontract.HeartbeatPhaseReasoning)); emitErr != nil {
		fmt.Fprintf(os.Stderr, "handler: claude-code: heartbeat emit error: %v\n", emitErr)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			if emitErr := emit(ctx, sessionID, string(handlercontract.HeartbeatPhaseReasoning)); emitErr != nil {
				fmt.Fprintf(os.Stderr, "handler: claude-code: heartbeat emit error: %v\n", emitErr)
			}
		}
	}
}

// DeriveCIaudeTranscriptPath derives the Claude transcript path for a given
// workspacePath and claudeSessionID, per CHB-018 session_log_location.
//
// Claude stores session transcripts at:
//
//	~/.claude/projects/<slug>/<session-uuid>.jsonl
//
// The slug is derived from the absolute workspace path by replacing path separators
// with hyphens and stripping leading slashes, matching Claude Code's slug convention.
//
// Spec: specs/claude-hook-bridge.md §4.7 CHB-018 step 2 (session_log_location).
func DeriveCIaudeTranscriptPath(workspacePath, claudeSessionID string) string {
	// Derive the Claude project slug from the workspace path.
	// Claude Code converts the workspace path to a slug by replacing '/' with '-'
	// and removing the leading '-'.
	slug := strings.ReplaceAll(workspacePath, "/", "-")
	slug = strings.ReplaceAll(slug, " ", "-")
	// Remove leading separator that results from the leading '/'.
	slug = strings.TrimPrefix(slug, "-")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fall back to a relative path (observable in tests).
		homeDir = "~"
	}

	return filepath.Join(homeDir, ".claude", "projects", slug, claudeSessionID+".jsonl")
}
