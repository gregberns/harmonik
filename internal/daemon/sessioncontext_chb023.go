package daemon

// sessioncontext_chb023.go — daemon-side claude_session_id durability before Claude exec.
//
// Implements CHB-023: on receipt of the handler's handler_capabilities progress-stream
// message carrying claude_session_id, the daemon MUST persist the value into
// Run.context.claude_session_id with checkpoint-commit-class durability BEFORE returning
// the connection-accept ACK (version_selected) that gates the handler's claude exec.
//
// # Crash-recovery invariant (CHB-023)
//
// A mid-launch crash MUST leave the system in one of two states:
//   (a) No session ID persisted — handler_capabilities not yet received; safe to
//       re-launch under a fresh UUIDv7.
//   (b) session_id durably committed to git — safe to claude --resume.
//
// The atomicity boundary is the `git commit` step: until the commit lands, state (a)
// holds. After the commit, state (b) holds. There is no split-brain window.
//
// # Context file layout
//
// The context file is written to the run's git worktree at:
//   .harmonik/run-context/<run_id>/context.json
//
// This path is git-tracked (committed to the task branch) and is distinct from the
// in-memory reviewLoopState.claudeSessionID so that a daemon restart can recover the
// value via state-reconstruction (EM-031).
//
// # ACK ordering (EM-025a analogue)
//
// The ordering contract: git commit → transition_event published → ACK sent.
// The ACK (version_selected) is sent via sess.SendInput after the commit returns.
// Publishing the transition_event to the event bus happens in the same goroutine
// immediately after the commit, matching EM-025a ordering.
//
// Spec refs:
//   - specs/claude-hook-bridge.md §4.6.CHB-023
//   - specs/execution-model.md §4.3.EM-015d, §4.5.EM-023a, §4.7.EM-031
//   - specs/handler-contract.md §4.10.HC-045c

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/handlercontract"
)

// runContextFileName is the filename of the run-context JSON file written
// to the task branch on each context-persist checkpoint.
const runContextFileName = "context.json"

// runContextDirPrefix is the directory prefix under .harmonik/ for run-context files.
// Full path: <worktree>/.harmonik/run-context/<run_id>/context.json
const runContextDirPrefix = ".harmonik/run-context"

// runContextFile holds the persisted Run.context fields written to git.
// Only fields updated at each checkpoint pass are included; other Run.context
// fields are additive in subsequent commits.
type runContextFile struct {
	// ClaudeSessionID is the Claude Code session identifier per CHB-023 / EM-015d.
	// Written on the first handler_capabilities message from the implementer subprocess.
	ClaudeSessionID string `json:"claude_session_id"`

	// PersistedAt is a human-readable ISO-8601 timestamp for observability.
	// Not normative; included for operator diagnostics.
	PersistedAt string `json:"persisted_at"`
}

// persistClaudeSessionIDResult is the outcome of a persistClaudeSessionID call.
type persistClaudeSessionIDResult struct {
	// CommitSHA is the full git commit SHA of the checkpoint commit on the task branch.
	// Empty string if persistence was skipped (empty sessionID).
	CommitSHA string

	// Skipped is true when sessionID was empty and no git commit was made.
	Skipped bool
}

// persistClaudeSessionID writes claude_session_id into Run.context via a git
// checkpoint commit on the run's task branch (CHB-023, EM-023a).
//
// Parameters:
//   - ctx       — caller context; propagated to all git subprocess invocations.
//   - wtPath    — absolute path of the git worktree for this run.
//   - runID     — the run's stable identifier; used for the context file path.
//   - sessionID — the claude_session_id extracted from handler_capabilities.
//
// When sessionID is empty the function returns a skipped result with no commit.
// This preserves the crash-recovery invariant: an empty session ID means
// handler_capabilities was not yet received (state (a) per CHB-023).
//
// The commit carries:
//   - A transition context file at .harmonik/run-context/<run_id>/context.json
//   - A commit message with the Harmonik-Run-ID trailer so state-reconstruction
//     (EM-031) can discover the context-persist commit via git log.
//
// The function returns the commit SHA and an error. On success the git HEAD of
// the task branch has advanced to the new commit; the caller should NOT use the
// prior HEAD SHA for state queries after this call.
//
// Spec refs: specs/claude-hook-bridge.md §4.6.CHB-023;
// specs/execution-model.md §4.5.EM-023a, §4.7.EM-031.
func persistClaudeSessionID(ctx context.Context, wtPath string, runID core.RunID, sessionID string) (persistClaudeSessionIDResult, error) {
	if sessionID == "" {
		return persistClaudeSessionIDResult{Skipped: true}, nil
	}

	// Build the context file directory.
	contextDir := filepath.Join(wtPath, runContextDirPrefix, runID.String())
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		return persistClaudeSessionIDResult{}, fmt.Errorf(
			"daemon: persistClaudeSessionID: mkdir %q: %w", contextDir, err)
	}

	// Write the context JSON file.
	ctxFile := runContextFile{
		ClaudeSessionID: sessionID,
		PersistedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	data, marshalErr := json.Marshal(ctxFile)
	if marshalErr != nil {
		return persistClaudeSessionIDResult{}, fmt.Errorf(
			"daemon: persistClaudeSessionID: marshal context file: %w", marshalErr)
	}
	contextFilePath := filepath.Join(contextDir, runContextFileName)
	//nolint:gosec // G306: 0644 is correct for a readable git-tracked JSON file
	if err := os.WriteFile(contextFilePath, data, 0o644); err != nil {
		return persistClaudeSessionIDResult{}, fmt.Errorf(
			"daemon: persistClaudeSessionID: write context file %q: %w", contextFilePath, err)
	}

	// Stage the context file.
	relPath := filepath.Join(runContextDirPrefix, runID.String(), runContextFileName)
	addCmd := exec.CommandContext(ctx, "git", "add", relPath)
	addCmd.Dir = wtPath
	if out, err := addCmd.CombinedOutput(); err != nil {
		return persistClaudeSessionIDResult{}, fmt.Errorf(
			"daemon: persistClaudeSessionID: git add %q: %w\ngit output: %s", relPath, err, out)
	}

	// Commit with a trailerised message so EM-031 state-reconstruction can find
	// the context-persist commit via `git log --grep Harmonik-Run-ID`.
	commitMsg := fmt.Sprintf(
		"harmonik: persist claude_session_id to Run.context (CHB-023)\n\nHarmonik-Run-ID: %s\nHarmonik-Context-Event: claude_session_id_persisted",
		runID.String(),
	)
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", commitMsg)
	commitCmd.Dir = wtPath
	if out, err := commitCmd.CombinedOutput(); err != nil {
		return persistClaudeSessionIDResult{}, fmt.Errorf(
			"daemon: persistClaudeSessionID: git commit: %w\ngit output: %s", err, out)
	}

	// Resolve the new HEAD SHA to return as the checkpoint commit hash.
	sha, shaErr := resolveWorktreeHEAD(ctx, wtPath)
	if shaErr != nil {
		return persistClaudeSessionIDResult{}, fmt.Errorf(
			"daemon: persistClaudeSessionID: resolve HEAD after commit: %w", shaErr)
	}

	return persistClaudeSessionIDResult{CommitSHA: sha}, nil
}

// resolveWorktreeHEAD returns the current HEAD commit SHA in the worktree at wtPath.
func resolveWorktreeHEAD(ctx context.Context, wtPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = wtPath
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("daemon: resolveWorktreeHEAD: git rev-parse HEAD in %q: %w", wtPath, err)
	}
	sha := string(out)
	for len(sha) > 0 && sha[len(sha)-1] == '\n' {
		sha = sha[:len(sha)-1]
	}
	if sha == "" {
		return "", fmt.Errorf("daemon: resolveWorktreeHEAD: git rev-parse HEAD returned empty in %q", wtPath)
	}
	return sha, nil
}

// emitClaudeSessionIDPersisted emits a transition_event to the bus after the
// checkpoint commit lands (EM-025a ordering: commit first, then event).
//
// The event uses EventTypeTransitionEvent so it is observable in the JSONL log
// with the run_id join key, matching the checkpoint_written event class defined
// in EM-023a / core/eventreg_hqwn59.go.
//
// This is a best-effort emit: a bus failure after a successful git commit does
// NOT reverse the durability guarantee; only the event observability is lost.
func emitClaudeSessionIDPersisted(
	ctx context.Context,
	bus handlercontract.EventEmitter,
	runID core.RunID,
	commitSHA string,
	claudeSessionID string,
) {
	pl := claudeSessionIDPersistedPayload{
		RunID:           runID.String(),
		CommitSHA:       commitSHA,
		ClaudeSessionID: claudeSessionID,
		PersistedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.Marshal(pl)
	if err != nil {
		return
	}
	// Use EmitWithRunID so the JSONL envelope carries the run_id join key (EM-013).
	// Error from Emit is not actionable after the git commit already landed.
	_ = bus.EmitWithRunID(ctx, runID, core.EventTypeTransitionEvent, b)
}

// claudeSessionIDPersistedPayload is the payload for the transition_event emitted
// after the claude_session_id checkpoint commit lands (CHB-023, EM-025a).
type claudeSessionIDPersistedPayload struct {
	RunID           string `json:"run_id"`
	CommitSHA       string `json:"commit_sha"`
	ClaudeSessionID string `json:"claude_session_id"`
	PersistedAt     string `json:"persisted_at"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Progress-stream interceptor (CHB-023)
// ─────────────────────────────────────────────────────────────────────────────

// SessionIDInterceptor wraps an io.Reader (the handler subprocess stdout pipe)
// and fires a callback exactly once when it observes a handler_capabilities
// line carrying a non-empty claude_session_id field.  All bytes are passed
// through to the underlying reader unchanged so the Watcher can process them
// normally.
//
// The interceptor is line-buffered: it reads one NDJSON line at a time, checks
// whether it is a handler_capabilities message, and fires the callback exactly
// once (subsequent handler_capabilities lines are passed through without re-firing).
//
// Usage:
//
//	cb := func(id string) { /* persist + ACK */ }
//	wrapped := newSessionIDInterceptor(sess.Stdout(), cb)
//	watcher := handlercontract.SpawnWatcher(ctx, handlercontract.SpawnWatcherConfig{
//	    ProgressStream: wrapped,
//	    ...
//	})
type SessionIDInterceptor struct {
	mu        sync.Mutex
	inner     io.Reader
	buf       bytes.Buffer // line accumulator
	firedOnce bool
	cb        func(string) // called with claude_session_id on first match
}

// newSessionIDInterceptor wraps inner with a SessionIDInterceptor that fires cb
// exactly once when a handler_capabilities line with a non-empty claude_session_id
// is observed.
//
// cb is called synchronously on the first matching Read call; it MUST NOT block
// indefinitely (the Watcher's read-loop is on the same goroutine).  The caller
// should dispatch blocking work (git commit, ACK) in a goroutine that the Watcher
// is NOT waiting on — or, if the ACK must precede the Watcher's next read, use a
// channel to signal the blocking work before returning from cb.
//
// For CHB-023 we use a callback that signals a channel; the review loop blocks on
// that channel before spawning the Watcher, achieving the ordering contract without
// blocking the Watcher goroutine.
func newSessionIDInterceptor(inner io.Reader, cb func(string)) *SessionIDInterceptor {
	return &SessionIDInterceptor{inner: inner, cb: cb}
}

// Read implements io.Reader.  It passes all bytes from the underlying reader
// through unchanged.  When it detects a complete NDJSON line that is a
// handler_capabilities message with a non-empty claude_session_id, it fires cb
// exactly once before returning the bytes to the caller.
func (s *SessionIDInterceptor) Read(p []byte) (int, error) {
	n, err := s.inner.Read(p)
	if n > 0 {
		s.mu.Lock()
		s.buf.Write(p[:n])
		s.checkBuffer()
		s.mu.Unlock()
	}
	return n, err
}

// checkBuffer scans s.buf for complete NDJSON lines and fires the callback on
// the first handler_capabilities line with a non-empty claude_session_id.
// Called with s.mu held.
func (s *SessionIDInterceptor) checkBuffer() {
	if s.firedOnce {
		return
	}
	for {
		b := s.buf.Bytes()
		idx := bytes.IndexByte(b, '\n')
		if idx < 0 {
			break
		}
		line := make([]byte, idx)
		copy(line, b[:idx])
		// Consume the line (including the newline) from the buffer.
		s.buf.Next(idx + 1)

		if len(line) == 0 {
			continue
		}

		// Quick check: only decode if the line contains "handler_capabilities".
		if !bytes.Contains(line, []byte(handlercontract.ProgressMsgTypeHandlerCapabilities)) {
			continue
		}

		var msg handlercontract.HandlerCapabilitiesMsg
		if jsonErr := json.Unmarshal(line, &msg); jsonErr != nil {
			continue
		}
		if msg.Type != handlercontract.ProgressMsgTypeHandlerCapabilities {
			continue
		}
		if msg.ClaudeSessionID == "" {
			continue
		}

		// Fire callback exactly once.
		s.firedOnce = true
		s.cb(msg.ClaudeSessionID)
		return
	}
}

// sendVersionSelectedACK sends the version_selected control message to the handler
// subprocess via sess.SendInput, unblocking the handler to exec Claude.
//
// The ACK is the NDJSON line: {"type":"version_selected","selected_version":1}
//
// This call MUST happen AFTER the git checkpoint commit returned by
// persistClaudeSessionID, satisfying the ordering contract of CHB-023:
// git commit → transition_event → ACK.
//
// sess may be nil (e.g., in unit tests where no subprocess is present); when nil,
// the function returns nil immediately (no-op).
//
// Spec: specs/handler-contract.md §4.11 (control message catalog), §7.2.
func sendVersionSelectedACK(ctx context.Context, sess interface {
	SendInput(ctx context.Context, line string) error
}) error {
	if sess == nil {
		return nil
	}
	msg := fmt.Sprintf(`{"type":%q,"selected_version":1}`, handlercontract.VersionSelectedControlMsgType)
	if err := sess.SendInput(ctx, msg); err != nil {
		return fmt.Errorf("daemon: sendVersionSelectedACK: SendInput: %w", err)
	}
	return nil
}
