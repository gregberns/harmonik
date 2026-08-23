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
//
// Schema v1 fields:
//   - SchemaVersion: MUST equal ReviewVerdictSchemaVersion (1).
//   - Verdict:       MUST be one of "APPROVE", "REQUEST_CHANGES", "BLOCK".
//   - Flags:         String array; MAY be empty.
//   - Notes:         Free text; MUST be non-empty per agent-reviewer skill contract.
type ReviewVerdict struct {
	// SchemaVersion is the integer schema version of the agent-reviewer JSON
	// verdict schema. MUST equal ReviewVerdictSchemaVersion (1).
	SchemaVersion int `json:"schema_version"`

	// Verdict is the reviewer's decision. MUST be one of the values declared
	// by ReviewVerdictValue: APPROVE, REQUEST_CHANGES, BLOCK.
	Verdict string `json:"verdict"`

	// Flags is the list of issue tags from the agent-reviewer schema v1.
	// MAY be empty (nil and [] are both valid); a nil JSON value is treated
	// as an empty slice.
	Flags []string `json:"flags"`

	// Notes is the free-text reviewer rationale. MUST be non-empty per the
	// agent-reviewer skill contract (1–3 sentences per §8.1a.3).
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
// It exists so that a document cannot claim a review that did not happen
// (hk-2f3v4). The observed harm is specific: a codex implementer read
// `verdict: GATE_FAIL` under a "Reviewer feedback" heading, concluded a reviewer
// had asked for the change, and spent three passes acting on that. Deriving the
// answer from the verdict rather than from a caller-supplied flag means a new
// daemon-produced feedback path cannot forget to declare itself.
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
// use errors.Is(err, ErrMalformed).
//
// Conditions that produce ErrMalformed (per WM-027a and event-model §8.1a.3):
//   - JSON parse failure.
//   - schema_version field absent, zero, or not equal to ReviewVerdictSchemaVersion.
//   - verdict field absent or not in {APPROVE, REQUEST_CHANGES, BLOCK}.
//   - flags field absent (null token maps to empty slice; missing key is rejected).
//   - notes field absent or empty.
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
//
// Validation rules:
//   - schema_version MUST equal ReviewVerdictSchemaVersion (1).
//   - verdict MUST be one of "APPROVE", "REQUEST_CHANGES", "BLOCK".
//   - flags MUST be present (null is treated as empty slice; missing key is malformed).
//   - notes MUST be non-empty.
//
// Returns:
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
// Return contract matches ReadReviewVerdict:
//   - (*ReviewVerdict, nil) when the file is present and valid.
//   - (nil, ErrMalformed) (wrapping) for any schema violation.
//   - (nil, nil) when the file does not exist (cat exits non-zero on the worker),
//     interpreted by the caller as the inconclusive condition per WM-027a §(e).
//
// Bead: hk-f3u6o.
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
// Motivation: the finalize read reads the reviewer's
// box-A-local worktree with a nil runner. If the daemon reads review.json at the
// instant the reviewer's claude is still flushing / has not yet made the write
// durable, os.ReadFile observes a truncated file and parseReviewVerdict returns
// ErrMalformed — and the plain ReadReviewVerdict does NOT retry, so the run
// false-fails fast. This retrying reader closes that gap for the finalize read.
//
// Contract (mirrors ReadReviewVerdict / ReadReviewVerdictVia):
//   - (*ReviewVerdict, nil) when the file is present and valid — a clean parse
//     short-circuits immediately (no retry, no added latency).
//   - (nil, nil) when the file is absent — short-circuits immediately per
//     WM-027a §(e).
//   - (nil, ErrMalformed) (wrapping) ONLY after the retry budget is spent, so a
//     genuinely-malformed verdict still fails (no false positives — just bounded
//     extra latency).
//   - (nil, ctx.Err()) if ctx is cancelled during an inter-attempt wait.
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
// WriteLeaseLockAtomic:
//
//  1. json.Marshal verdict — this is the fix for hk-9w79a: a reviewer agent
//     hand-typing raw JSON text into a Write-tool call can emit an invalid
//     escape (e.g. a backtick-containing code snippet in Notes gets a stray
//     "\`" backslash-escape, which is not a legal JSON escape) whenever the
//     free-text Notes field contains a backtick. encoding/json.Marshal escapes
//     only the characters JSON actually requires (", \, control chars) and
//     leaves backtick unescaped, so a backtick in Notes can never produce
//     invalid JSON.
//  2. Write the marshaled bytes to a sibling temp file, fsync it.
//  3. rename(2) the temp file over the target (POSIX-atomic within one fs).
//  4. Best-effort fsync of the parent directory.
//
// Callers should prefer this over writing review.json by hand. The
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

// ReviewVerdictArchivePath returns the canonical path for an archived reviewer
// verdict file per workspace-model.md §4.7.WM-027a §(c):
//
//	${workspace_path}/.harmonik/review.iter-<N>.json
//
// N is the 1-indexed ordinal of the just-completed iteration (iteration cap = 3
// per execution-model.md §4.3). The caller MUST pass the absolute worktree path.
func ReviewVerdictArchivePath(workspacePath string, iterationN int) string {
	return filepath.Join(workspacePath, ".harmonik", fmt.Sprintf("review.iter-%d.json", iterationN))
}

// ArchiveVerdict renames the current reviewer verdict file
// ${workspace_path}/.harmonik/review.json to
// ${workspace_path}/.harmonik/review.iter-<N>.json, where N is iterationN.
//
// This implements the daemon-side archive step in workspace-model.md
// §4.7.WM-027a §(c): before launching iteration N+1's reviewer, the daemon
// MUST archive the prior review.json by renaming it to review.iter-<N>.json.
//
// The rename uses os.Rename (POSIX-atomic within one filesystem) followed by a
// best-effort fsync of the parent directory per the WM-026 discipline.
//
// Returns:
//   - nil on success.
//   - ErrNotFound (wrapped) when the source review.json does not exist.
//   - an error (wrapping ErrNotFound) when the destination review.iter-<N>.json
//     already exists — double-archive at the same N is a caller error.
//   - a wrapped I/O error for any other filesystem failure.
func ArchiveVerdict(workspacePath string, iterationN int) error {
	src := ReviewVerdictPath(workspacePath)
	dst := ReviewVerdictArchivePath(workspacePath, iterationN)

	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: review.json absent at %q", ErrNotFound, src)
		}
		return fmt.Errorf("workspace: ArchiveVerdict: Stat source %q: %w", src, err)
	}

	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("workspace: ArchiveVerdict: destination already exists at %q (double-archive at iteration %d)", dst, iterationN)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("workspace: ArchiveVerdict: Stat destination %q: %w", dst, err)
	}

	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("workspace: ArchiveVerdict: Rename %q → %q: %w", src, dst, err)
	}

	dir := filepath.Dir(src)
	//nolint:gosec // G304: path constructed from workspace_path + known relative segments; not user input
	dirFD, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("workspace: ArchiveVerdict: Open dir %q for fsync: %w", dir, err)
	}
	syncErr := dirFD.Sync()
	closeErr := dirFD.Close()
	if syncErr != nil || closeErr != nil {
		return fmt.Errorf("workspace: ArchiveVerdict: fsync/close dir fd: %w", errors.Join(syncErr, closeErr))
	}

	return nil
}
