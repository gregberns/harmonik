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

	// Build paths early so the idempotency check can read before writing.
	contextDir := filepath.Join(wtPath, runContextDirPrefix, runID.String())
	contextFilePath := filepath.Join(contextDir, runContextFileName)

	// Idempotency: if context.json already exists with the same session_id, skip the
	// commit. This handles daemon-restart + resume scenarios (EM-031) where the CHB-023
	// checkpoint is already on the task branch — re-committing would produce a redundant
	// commit with only a changed persisted_at timestamp, polluting the task branch and
	// eventually main with no-op infrastructure commits.
	if existingData, err := os.ReadFile(contextFilePath); err == nil {
		var existing runContextFile
		if json.Unmarshal(existingData, &existing) == nil && existing.ClaudeSessionID == sessionID {
			return persistClaudeSessionIDResult{Skipped: true}, nil
		}
	}

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
	//nolint:gosec // G306: 0644 is correct for a readable git-tracked JSON file
	if err := os.WriteFile(contextFilePath, data, 0o644); err != nil {
		return persistClaudeSessionIDResult{}, fmt.Errorf(
			"daemon: persistClaudeSessionID: write context file %q: %w", contextFilePath, err)
	}

	// Stage the context file. Use -f because .harmonik/ is in .gitignore;
	// without -f, git add exits 1 and persistence silently fails (hk-gtgwn).
	relPath := filepath.Join(runContextDirPrefix, runID.String(), runContextFileName)
	addCmd := exec.CommandContext(ctx, "git", "add", "-f", relPath)
	addCmd.Dir = wtPath
	if out, err := addCmd.CombinedOutput(); err != nil {
		return persistClaudeSessionIDResult{}, fmt.Errorf(
			"daemon: persistClaudeSessionID: git add -f %q: %w\ngit output: %s", relPath, err, out)
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

	// capsSeen is true once the first handler_capabilities line has been
	// decoded (HC-009 negotiation runs exactly once, on that line).
	capsSeen bool

	// selectedVersion is the wire-protocol version selected by HC-009
	// negotiation (max of the daemon/handler intersection). Zero until
	// negotiation succeeds.
	selectedVersion int

	// negoErr is the sticky HC-009 failure: either an empty version
	// intersection or handler_capabilities absent within
	// HandlerCapabilitiesTimeout. Once set, every subsequent Read returns it,
	// which terminates the Watcher's read-loop and surfaces agent_failed with
	// an ErrProtocolMismatch-classed error (§8.7).
	negoErr error

	// capsTimer enforces the HandlerCapabilitiesTimeout (5s) abort of §7.2:
	// if no handler_capabilities line is observed within the window, negoErr
	// is set and inner is closed (when it is an io.Closer) so a blocked Read
	// unwedges and observes the mismatch.
	capsTimer *time.Timer
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
	s := &SessionIDInterceptor{inner: inner, cb: cb}
	// HC-009 / §7.2: abort if handler_capabilities is absent within
	// HandlerCapabilitiesTimeout of subprocess spawn (the interceptor is
	// constructed inside Handler.Launch, immediately around cmd.Start).
	// Closing inner (the session stdout io.Pipe reader) unwedges a Read that
	// is blocked waiting on a silent handler so the mismatch is observed
	// promptly rather than at the session ceiling.
	s.capsTimer = time.AfterFunc(capsAbsentTimeout, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.capsSeen || s.negoErr != nil {
			return
		}
		s.negoErr = fmt.Errorf(
			"daemon: version negotiation: handler_capabilities absent within %s: %w",
			capsAbsentTimeout, handlercontract.ErrProtocolMismatch)
		if c, ok := s.inner.(io.Closer); ok {
			_ = c.Close()
		}
	})
	return s
}

// SelectedVersion returns the wire-protocol version selected by HC-009
// negotiation, or 0 if negotiation has not (yet) succeeded.
func (s *SessionIDInterceptor) SelectedVersion() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.selectedVersion
}

// NegotiationErr returns the sticky HC-009 failure (empty version
// intersection, or capabilities-absent timeout), or nil.
func (s *SessionIDInterceptor) NegotiationErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.negoErr
}

// daemonSupportedWireVersions is the ordered list of wire-protocol versions
// this daemon supports (specs/handler-contract.md §4.2.HC-009). The ACK sender
// (sendVersionSelectedACK) derives its default selected_version from this
// list; when a second version is added the negotiated value MUST be plumbed
// from the interceptor to the ACK call sites.
var daemonSupportedWireVersions = []int{1}

// capsAbsentTimeout is the handler_capabilities-absent abort window (§7.2).
// A var (not const) only so tests can shrink the window; production value is
// the normative handlercontract.HandlerCapabilitiesTimeout (5s).
var capsAbsentTimeout = handlercontract.HandlerCapabilitiesTimeout

// negotiateWireVersion implements HC-009: select the highest wire-protocol
// version present in both the daemon's and the handler's supported lists.
// An empty intersection (including a nil/empty handler list) is a
// version-negotiation failure and returns an ErrProtocolMismatch-wrapping
// error per specs/handler-contract.md §4.2.HC-009, §8.7.
func negotiateWireVersion(handlerVersions []int) (int, error) {
	best := 0
	found := false
	for _, dv := range daemonSupportedWireVersions {
		for _, hv := range handlerVersions {
			if dv == hv && (!found || dv > best) {
				best = dv
				found = true
			}
		}
	}
	if !found {
		return 0, fmt.Errorf(
			"daemon: version negotiation: no mutually supported wire-protocol version (daemon %v, handler %v): %w",
			daemonSupportedWireVersions, handlerVersions, handlercontract.ErrProtocolMismatch)
	}
	return best, nil
}

// Read implements io.Reader.  It passes all bytes from the underlying reader
// through unchanged.  When it detects a complete NDJSON line that is a
// handler_capabilities message with a non-empty claude_session_id, it fires cb
// exactly once before returning the bytes to the caller.
func (s *SessionIDInterceptor) Read(p []byte) (int, error) {
	s.mu.Lock()
	if s.negoErr != nil {
		err := s.negoErr
		s.mu.Unlock()
		return 0, err
	}
	s.mu.Unlock()

	n, err := s.inner.Read(p)
	s.mu.Lock()
	if n > 0 {
		s.buf.Write(p[:n])
		s.checkBuffer()
	}
	// HC-009: a negotiation failure (empty intersection, or the
	// capabilities-absent timer closing inner mid-Read) overrides the inner
	// error so the Watcher terminates with an ErrProtocolMismatch-classed
	// read error.
	if s.negoErr != nil {
		err = s.negoErr
	}
	s.mu.Unlock()
	return n, err
}

// checkBuffer scans s.buf for complete NDJSON lines and fires the callback on
// the first handler_capabilities line with a non-empty claude_session_id.
// Called with s.mu held.
func (s *SessionIDInterceptor) checkBuffer() {
	if s.firedOnce && s.capsSeen {
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

		// HC-009: version negotiation runs exactly once, on the FIRST
		// handler_capabilities line. Disarm the capabilities-absent timer,
		// then select max(intersection(daemon, handler)); an empty
		// intersection (including a nil/empty supported_versions list) is a
		// negotiation failure surfaced as a sticky ErrProtocolMismatch read
		// error — the cb (persist + version_selected ACK) is NOT fired.
		if !s.capsSeen {
			s.capsSeen = true
			if s.capsTimer != nil {
				s.capsTimer.Stop()
			}
			sel, negoErr := negotiateWireVersion(msg.SupportedVersions)
			if negoErr != nil {
				s.negoErr = negoErr
				return
			}
			s.selectedVersion = sel
		}

		if s.firedOnce || msg.ClaudeSessionID == "" {
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
// selected, when provided, is the wire-protocol version chosen by HC-009
// negotiation (SessionIDInterceptor.SelectedVersion). When omitted the ACK
// carries max(daemonSupportedWireVersions) — correct while the daemon supports
// a single version, because the interceptor only releases the ACK path after
// a successful negotiation, whose result is necessarily that version.
func sendVersionSelectedACK(ctx context.Context, sess interface {
	SendInput(ctx context.Context, line string) error
}, selected ...int,
) error {
	if sess == nil {
		return nil
	}
	version := 0
	for _, v := range daemonSupportedWireVersions {
		if v > version {
			version = v
		}
	}
	if len(selected) > 0 && selected[0] > 0 {
		version = selected[0]
	}
	msg := fmt.Sprintf(`{"type":%q,"selected_version":%d}`, handlercontract.VersionSelectedControlMsgType, version)
	if err := sess.SendInput(ctx, msg); err != nil {
		return fmt.Errorf("daemon: sendVersionSelectedACK: SendInput: %w", err)
	}
	return nil
}
