package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	tmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// ReviewVerdict is the typed struct returned by ReadReviewVerdict.
// Fields map verbatim to the agent-reviewer JSON schema v1 per
// workspace-model.md §4.7.WM-027a and event-model.md §8.1a.3.
type ReviewVerdict struct {
	// MUST equal ReviewVerdictSchemaVersion (1).
	SchemaVersion int `json:"schema_version"`

	// MUST be one of APPROVE, REQUEST_CHANGES, BLOCK.
	Verdict string `json:"verdict"`

	// MAY be empty; a nil JSON value is treated as an empty slice.
	Flags []string `json:"flags"`

	// MUST be non-empty per the agent-reviewer skill contract.
	Notes string `json:"notes"`
}

// ReviewVerdictSchemaVersion is the current agent-reviewer JSON schema version.
// ReadReviewVerdict rejects any file whose schema_version field differs from this.
const ReviewVerdictSchemaVersion = 1

var (
	reviewVerdictRemoteRetryBudget = 6300 * time.Millisecond
	reviewVerdictRemoteBaseBackoff = 100 * time.Millisecond
	reviewVerdictRemoteMaxBackoff  = 3200 * time.Millisecond
)

// Accepted verdict strings for ReviewVerdict.Verdict per schema v1.
const (
	ReviewVerdictApprove        = "APPROVE"
	ReviewVerdictRequestChanges = "REQUEST_CHANGES"
	ReviewVerdictBlock          = "BLOCK"
)

// VerdictCameFromAReviewer reports whether a verdict string is one a REVIEWER
// can produce. Schema v1 closes that set to the three constants above, so
// anything else was written by the daemon — today GATE_FAIL (the commit gate
// went red) and NO_COMMIT (HEAD did not advance), both delivered through the
// same reviewer-feedback file the implementer-resume reads.
//
// Derive this from the verdict, never from a caller-supplied flag: a new
// daemon-produced feedback path cannot then forget to declare itself and let a
// document claim a review that did not happen (hk-2f3v4).
func VerdictCameFromAReviewer(verdict string) bool {
	switch verdict {
	case ReviewVerdictApprove, ReviewVerdictRequestChanges, ReviewVerdictBlock:
		return true
	default:
		return false
	}
}

// ErrMalformed is returned by ReadReviewVerdict when the verdict file at
// ${workspace_path}/.harmonik/review.json is present but fails schema
// validation. Callers that need to distinguish malformed from absent files
// use errors.Is(err, ErrMalformed). parseReviewVerdict states the conditions.
var ErrMalformed = errors.New("workspace: review verdict ErrMalformed")

// ErrRemoteTransport is returned by the runner-routed readers (ReadReviewVerdictVia,
// ReadAutoStatusMarkerVia) when the read over the transport FAILS at the transport
// layer — e.g. an SSH connection failure (ssh exit 255: refused/timeout/host-key)
// on a remote worker — as opposed to the remote command cleanly reporting the file
// absent (cat exit 1 → no such file). A transport failure is INCONCLUSIVE: the
// verdict/marker may well exist on the worker, we just could not reach it. Callers
// MUST distinguish this from confirmed-absent (nil, nil): treating a network blip
// as "no verdict" / "no FAIL marker" would drive the wrong review-gate / outcome
// decision. On the verdict path the retrying readers retry ErrRemoteTransport within
// the same bounded budget as ErrMalformed before surfacing it; a caller that still
// sees it should retry or escalate, never decide.
var ErrRemoteTransport = errors.New("workspace: remote read transport failure (inconclusive)")

// ReviewVerdictPath returns the canonical path for the current reviewer
// verdict file per workspace-model.md §4.7.WM-027a:
//
//	${workspace_path}/.harmonik/review.json
//
// The caller MUST pass the absolute worktree path.
func ReviewVerdictPath(workspacePath string) string {
	return filepath.Join(workspacePath, ".harmonik", "review.json")
}

// ReadReviewVerdict reads and validates the reviewer verdict file at
// ${workspace_path}/.harmonik/review.json against the agent-reviewer JSON
// schema v1 (workspace-model.md §4.7.WM-027a; event-model.md §8.1a.3).
// parseReviewVerdict states the validation rules.
//
// This is the canonical return contract for every reader in this file:
//   - (*ReviewVerdict, nil) when the file is present and valid.
//   - (nil, ErrMalformed) (wrapping ErrMalformed) for any schema violation.
//   - (nil, nil) when the file does not exist — the caller interprets absence
//     as the inconclusive condition per WM-027a §(e).
//   - (nil, <wrapped I/O error>) for I/O failures other than not-exist.
func ReadReviewVerdict(workspacePath string) (*ReviewVerdict, error) {
	target := ReviewVerdictPath(workspacePath)

	//nolint:gosec // G304: path constructed from workspace_path + known relative segments; not user input
	data, err := os.ReadFile(target)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil //nolint:nilnil // caller interprets nil as "absent" per WM-027a §(e)
		}
		return nil, fmt.Errorf("workspace: ReadReviewVerdict: ReadFile %q: %w", target, err)
	}

	return parseReviewVerdict(data, target)
}

// ReadReviewVerdictVia is like ReadReviewVerdict but routes the verdict-file read
// through runner (e.g. an SSHRunner for a remote-substrate worker whose worktree
// lives on a separate filesystem). For a remote run the reviewer writes
// review.json on the WORKER, so a box-A os.ReadFile never finds it → the run
// false-fails as "verdict absent". Routing through the runner (cat the file over
// the transport) reads the worker-side file and applies the identical schema
// validation via parseReviewVerdict.
//
// Callers pass nil to use ReadReviewVerdict's byte-identical bare-local path
// (NFR7). For symmetry with the rest of the remote-aware surface, a local-FS
// runner (tmux.LocalRunner) is also treated as local.
//
// On the remote path a cat-over-SSH read can observe a truncated, not-yet-durable
// review.json mid-write, so the read is retried on a transient ErrMalformed parse
// failure with bounded exponential backoff (up to ~6.3s total; see the
// reviewVerdictRemote* constants) — honoring ctx cancellation. A genuinely
// malformed verdict still returns ErrMalformed after the retry budget is spent.
// The local path does NOT retry. Beads: hk-clrts, hk-l489f.
//
// Return contract is ReadReviewVerdict's; absent means cat exited non-zero on
// the worker. Bead: hk-f3u6o.
func ReadReviewVerdictVia(ctx context.Context, runner tmux.CommandRunner, workspacePath string) (*ReviewVerdict, error) {
	if runner == nil || runnerIsLocalFS(runner) {
		return ReadReviewVerdict(workspacePath)
	}
	target := ReviewVerdictPath(workspacePath)

	return retryVerdictReadOnMalformed(ctx, func(ctx context.Context) (*ReviewVerdict, error) {
		out, err := runner.Command(ctx, "cat", target).Output()
		if err != nil {
			if tmux.IsSSHConnectionFailure(err) {
				return nil, fmt.Errorf("%w: cat %s: %w", ErrRemoteTransport, target, err)
			}
			// Non-transport cat failure (exit 1: no such file) → genuinely absent,
			// mirroring ReadReviewVerdict's os.IsNotExist branch (nil,nil = inconclusive).
			//nolint:nilnil // caller interprets nil as "absent" per WM-027a §(e); cat-fail = absent, mirrors readAutoStatusMarkerVia
			return nil, nil
		}
		if len(bytes.TrimSpace(out)) == 0 {
			return nil, nil //nolint:nilnil // empty read = absent verdict per WM-027a §(e); ssh-exit-0 masking
		}
		return parseReviewVerdict(out, target)
	})
}

// ReadReviewVerdictLocalRetry reads the LOCAL reviewer verdict file at
// ${workspace_path}/.harmonik/review.json with the same bounded
// retry-until-valid-on-ErrMalformed behavior as the remote path in
// ReadReviewVerdictVia (bead hk-1hgjr — the local twin of the remote hk-qts7r
// fix).
//
// Motivation: the finalize read uses a nil runner against the reviewer's
// box-A-local worktree. Read it while the reviewer's claude is still flushing
// and os.ReadFile sees a truncated file, so the non-retrying ReadReviewVerdict
// false-fails the run fast. This reader closes that gap.
//
// Return contract is ReadReviewVerdict's, plus: present-and-valid and absent
// both short-circuit with no added latency, ErrMalformed surfaces ONLY once the
// retry budget is spent, and ctx.Err() surfaces on cancellation mid-wait.
//
// This does NOT change ReadReviewVerdict or the ReadReviewVerdictVia nil/local
// branch — those stay byte-identical no-retry (NFR7); the retry is opt-in via
// this dedicated entry point, so the quit-watchdog gate and other local pollers
// keep their fast return.
func ReadReviewVerdictLocalRetry(ctx context.Context, workspacePath string) (*ReviewVerdict, error) {
	return retryVerdictReadOnMalformed(ctx, func(context.Context) (*ReviewVerdict, error) {
		return ReadReviewVerdict(workspacePath)
	})
}

type verdictRead func(ctx context.Context) (*ReviewVerdict, error)

func retryVerdictReadOnMalformed(ctx context.Context, read verdictRead) (*ReviewVerdict, error) {
	var lastErr error
	backoff := reviewVerdictRemoteBaseBackoff
	deadline := time.Now().Add(reviewVerdictRemoteRetryBudget)
	for {
		v, err := read(ctx)
		if err == nil || (!errors.Is(err, ErrMalformed) && !errors.Is(err, ErrRemoteTransport)) {
			return v, err
		}
		lastErr = err

		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		wait := backoff
		if wait > remaining {
			wait = remaining
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
		backoff *= 2
		if backoff > reviewVerdictRemoteMaxBackoff {
			backoff = reviewVerdictRemoteMaxBackoff
		}
	}
	return nil, lastErr
}

// WriteReviewVerdictAtomic writes verdict to the canonical review-verdict path
// ${workspace_path}/.harmonik/review.json using encoding/json (not hand-rolled
// string construction) and the same atomic-write discipline as
// WriteLeaseLockAtomic.
//
// An agent MUST NOT hand-write this JSON (hk-9w79a): hand-typing it into a
// Write-tool call emits a stray "\`" escape whenever the free-text Notes holds
// a backtick, and that is not legal JSON. json.Marshal escapes only what JSON
// requires, so a backtick in Notes can never produce an invalid file. The
// write-review-verdict CLI subcommand is the reviewer-facing entry point.
func WriteReviewVerdictAtomic(workspacePath string, verdict *ReviewVerdict) error {
	if verdict.Flags == nil {
		verdict.Flags = []string{}
	}

	target := ReviewVerdictPath(workspacePath)
	content, err := json.Marshal(verdict)
	if err != nil {
		return fmt.Errorf("workspace: WriteReviewVerdictAtomic: marshal: %w", err)
	}

	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, core.HarmonikDirMode); err != nil {
		return fmt.Errorf("workspace: WriteReviewVerdictAtomic: MkdirAll %q: %w", dir, err)
	}

	tmpPath := fmt.Sprintf("%s.tmp-%d", target, os.Getpid())
	//nolint:gosec // G304: path is constructed from workspace_path + known relative segments, not user input
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("workspace: WriteReviewVerdictAtomic: OpenFile %q: %w", tmpPath, err)
	}

	if _, err := f.Write(content); err != nil {
		return withCleanupErrs(fmt.Errorf("workspace: WriteReviewVerdictAtomic: Write: %w", err),
			f.Close(), os.Remove(tmpPath))
	}

	if err := f.Sync(); err != nil {
		return withCleanupErrs(fmt.Errorf("workspace: WriteReviewVerdictAtomic: Sync (pre-rename): %w", err),
			f.Close(), os.Remove(tmpPath))
	}
	if err := f.Close(); err != nil {
		return withCleanupErrs(fmt.Errorf("workspace: WriteReviewVerdictAtomic: Close (pre-rename): %w", err),
			os.Remove(tmpPath))
	}

	if err := os.Rename(tmpPath, target); err != nil {
		return withCleanupErrs(fmt.Errorf("workspace: WriteReviewVerdictAtomic: Rename %q → %q: %w", tmpPath, target, err),
			os.Remove(tmpPath))
	}

	//nolint:gosec // G304: path constructed from workspace_path + known relative segments; not user input
	dirFD, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("workspace: WriteReviewVerdictAtomic: Open dir %q for fsync: %w", dir, err)
	}
	syncErr := dirFD.Sync()
	closeErr := dirFD.Close()
	if syncErr != nil || closeErr != nil {
		return fmt.Errorf("workspace: WriteReviewVerdictAtomic: fsync/close dir fd: %w", errors.Join(syncErr, closeErr))
	}

	return nil
}

func runnerIsLocalFS(r tmux.CommandRunner) bool {
	switch r.(type) {
	case nil, tmux.LocalRunner:
		return true
	default:
		return false
	}
}

func parseReviewVerdict(data []byte, target string) (*ReviewVerdict, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%w: json parse error at %q: %w", ErrMalformed, target, err)
	}

	var v ReviewVerdict
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("%w: json unmarshal into ReviewVerdict at %q: %w", ErrMalformed, target, err)
	}

	if _, ok := raw["schema_version"]; !ok {
		return nil, fmt.Errorf("%w: schema_version field missing in %q", ErrMalformed, target)
	}
	if v.SchemaVersion != ReviewVerdictSchemaVersion {
		return nil, fmt.Errorf("%w: schema_version = %d; want %d in %q",
			ErrMalformed, v.SchemaVersion, ReviewVerdictSchemaVersion, target)
	}

	if _, ok := raw["verdict"]; !ok {
		return nil, fmt.Errorf("%w: verdict field missing in %q", ErrMalformed, target)
	}
	switch v.Verdict {
	case ReviewVerdictApprove, ReviewVerdictRequestChanges, ReviewVerdictBlock:
	default:
		return nil, fmt.Errorf("%w: verdict = %q; must be APPROVE, REQUEST_CHANGES, or BLOCK in %q",
			ErrMalformed, v.Verdict, target)
	}

	if _, ok := raw["flags"]; !ok {
		return nil, fmt.Errorf("%w: flags field missing in %q", ErrMalformed, target)
	}
	if v.Flags == nil {
		v.Flags = []string{}
	}

	if _, ok := raw["notes"]; !ok {
		return nil, fmt.Errorf("%w: notes field missing in %q", ErrMalformed, target)
	}
	if v.Notes == "" {
		return nil, fmt.Errorf("%w: notes field is empty in %q", ErrMalformed, target)
	}

	return &v, nil
}
