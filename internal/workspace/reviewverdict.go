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

// Verdict read-retry bounds (hk-clrts, widened hk-l489f, deadline-bounded
// hk-qts7r; local finalize path hk-1hgjr). A verdict read — whether a cat-over-SSH
// read of a worker's review.json (remote) or an os.ReadFile of the reviewer's
// box-A-local worktree (local finalize) — can observe a partially-written /
// not-yet-durable file while the reviewer's claude is still flushing it, so
// parseReviewVerdict returns ErrMalformed on a transient truncated read. The
// retrying readers (ReadReviewVerdictVia remote branch, ReadReviewVerdictLocalRetry)
// retry-until-valid ONLY on ErrMalformed with bounded exponential backoff (100ms
// base, doubling, capped at reviewVerdictRemoteMaxBackoff) up to
// reviewVerdictRemoteRetryBudget of total elapsed time — a DEADLINE rather than a
// fixed attempt count, so a slow-flush read whose fsync takes several seconds
// still recovers, while a genuinely-malformed verdict fails once the budget is
// spent. ctx cancellation / deadline is honored in every inter-attempt wait.
//
// NFR7 note: the bare ReadReviewVerdict and the ReadReviewVerdictVia nil/local
// branch stay byte-identical no-retry — the retry is opt-in via the dedicated
// retrying readers, so the quit-watchdog gate and other pollers keep their fast
// absent/malformed return.
//
// Declared as vars so tests can shrink the budget for fast, deterministic runs.
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

	// Remote read-retry (hk-clrts, deadline-bounded hk-qts7r): a cat-over-SSH read
	// can observe a partially-written / not-yet-durable review.json on the worker,
	// so a transient truncated read makes parseReviewVerdict return ErrMalformed
	// and the run false-fails fast. Retry-until-valid ONLY on ErrMalformed with
	// bounded exponential backoff up to reviewVerdictRemoteRetryBudget of total
	// elapsed time; cat-error (absent) and a clean parse short-circuit with the
	// existing contract, and a genuinely-malformed verdict still fails once the
	// budget is spent (no false positives — just bounded extra latency).
	return retryVerdictReadOnMalformed(ctx, func(ctx context.Context) (*ReviewVerdict, error) {
		out, err := runner.Command(ctx, "cat", target).Output()
		if err != nil {
			if tmux.IsSSHConnectionFailure(err) {
				// SSH transport failure (ssh exit 255) — NOT a confirmed-absent
				// verdict. The verdict may exist on the worker; we could not reach
				// it. Surface as inconclusive so the retry budget re-tries the read
				// and, if it persists, the caller escalates rather than mis-reading a
				// network blip as "verdict absent" and deciding the review gate. H4.
				return nil, fmt.Errorf("%w: cat %s: %w", ErrRemoteTransport, target, err)
			}
			// Non-transport cat failure (exit 1: no such file) → genuinely absent,
			// mirroring ReadReviewVerdict's os.IsNotExist branch (nil,nil = inconclusive).
			//nolint:nilnil,nilerr // caller interprets nil as "absent" per WM-027a §(e); cat-fail = absent, mirrors readAutoStatusMarkerVia
			return nil, nil
		}
		// Empty (whitespace-only) stdout → treat as absent. ROOT CAUSE: on some
		// remote workers the ssh login shell rc (e.g. a `-zsh` login profile that
		// resets $?) masks the exit code, so `ssh cat <absent-file>` returns err==nil
		// with EMPTY stdout instead of the expected non-zero — the err != nil
		// absent-branch above never fires. A real verdict is never empty, so an empty
		// read is a genuinely-absent file; short-circuit to absent (nil,nil =
		// inconclusive) INSIDE the retried closure rather than feeding "" to
		// parseReviewVerdict, which would return ErrMalformed ("unexpected end of
		// JSON input") and false-fail the run. Mirrors ParseAutoStatusMarker's
		// len(data)==0 treat-as-absent guard.
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
// Motivation: the reviewloop finalize read (reviewloop.go) reads the reviewer's
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

// verdictRead performs a single verdict read attempt. It returns:
//   - (*ReviewVerdict, nil) on a clean parse (valid verdict).
//   - (nil, nil) when the file is absent — the inconclusive condition.
//   - (nil, err) wrapping ErrMalformed for a transient/genuine malformed read.
//
// It should honor ctx for the read itself where applicable.
type verdictRead func(ctx context.Context) (*ReviewVerdict, error)

// retryVerdictReadOnMalformed runs read repeatedly, retrying on a transient
// ErrMalformed (truncated mid-write read) OR ErrRemoteTransport (SSH connection
// failure) result with bounded exponential backoff up to
// reviewVerdictRemoteRetryBudget of total elapsed time (deadline-bounded, not a
// fixed attempt count), honoring ctx cancellation in every inter-attempt wait.
//
// A clean parse and an absent (nil,nil) read short-circuit immediately with the
// existing contract; a genuinely-malformed read still returns its last
// ErrMalformed-wrapped error once the budget is spent. Shared by the remote
// ReadReviewVerdictVia branch and the local ReadReviewVerdictLocalRetry so both
// apply byte-identical retry semantics.
func retryVerdictReadOnMalformed(ctx context.Context, read verdictRead) (*ReviewVerdict, error) {
	var lastErr error
	backoff := reviewVerdictRemoteBaseBackoff
	deadline := time.Now().Add(reviewVerdictRemoteRetryBudget)
	for {
		v, err := read(ctx)
		if err == nil || (!errors.Is(err, ErrMalformed) && !errors.Is(err, ErrRemoteTransport)) {
			// Clean parse / absent (nil,nil), or a non-retryable error (defensive):
			// return as-is. ErrMalformed (transient truncated read) and
			// ErrRemoteTransport (transient SSH connection failure) are both
			// retried within the budget below, then surfaced so the caller can
			// escalate on a genuinely-malformed or persistently-unreachable read.
			return v, err
		}
		lastErr = err

		// Deadline-bounded retry: stop once the retry budget is spent, surfacing
		// the last ErrMalformed-wrapped error so a genuinely-malformed verdict still
		// fails. Don't sleep past the deadline.
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
	//nolint:gosec // G301: 0755 matches existing .harmonik dir conventions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("workspace: WriteReviewVerdictAtomic: MkdirAll %q: %w", dir, err)
	}

	tmpPath := fmt.Sprintf("%s.tmp-%d", target, os.Getpid())
	//nolint:gosec // G304: path is constructed from workspace_path + known relative segments, not user input
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("workspace: WriteReviewVerdictAtomic: OpenFile %q: %w", tmpPath, err)
	}

	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("workspace: WriteReviewVerdictAtomic: Write: %w", err)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("workspace: WriteReviewVerdictAtomic: Sync (pre-rename): %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("workspace: WriteReviewVerdictAtomic: Close (pre-rename): %w", err)
	}

	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("workspace: WriteReviewVerdictAtomic: Rename %q → %q: %w", tmpPath, target, err)
	}

	//nolint:gosec // G304: path constructed from workspace_path + known relative segments; not user input
	dirFD, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("workspace: WriteReviewVerdictAtomic: Open dir %q for fsync: %w", dir, err)
	}
	_ = dirFD.Sync() // best-effort on APFS per WM-013a precedent
	if err := dirFD.Close(); err != nil {
		return fmt.Errorf("workspace: WriteReviewVerdictAtomic: Close dir fd: %w", err)
	}

	return nil
}

// runnerIsLocalFS reports whether r operates on the daemon box's local filesystem
// — i.e. the worktree paths it is given are directly readable with os.ReadFile.
// A nil runner (defensive) and tmux.LocalRunner both qualify; an SSHRunner (or
// any other transport) does NOT, because its worktree lives on a remote worker.
// Mirrors the daemon-package runnerIsLocalFS so the workspace remote-aware
// readers share the same local/remote classification.
func runnerIsLocalFS(r tmux.CommandRunner) bool {
	switch r.(type) {
	case nil, tmux.LocalRunner:
		return true
	default:
		return false
	}
}

// parseReviewVerdict validates raw verdict-file bytes against the agent-reviewer
// JSON schema v1 and returns the typed verdict. target is used only for error
// messages (the path the bytes came from). Shared by ReadReviewVerdict (local
// os.ReadFile) and ReadReviewVerdictVia (runner-routed read) so both paths apply
// byte-identical validation (NFR7).
func parseReviewVerdict(data []byte, target string) (*ReviewVerdict, error) {
	// Unmarshal into a raw map first so we can detect missing keys vs. zero values.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%w: json parse error at %q: %v", ErrMalformed, target, err)
	}

	// Unmarshal into typed struct for field access.
	var v ReviewVerdict
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("%w: json unmarshal into ReviewVerdict at %q: %v", ErrMalformed, target, err)
	}

	// Validate schema_version: key must be present and equal ReviewVerdictSchemaVersion.
	if _, ok := raw["schema_version"]; !ok {
		return nil, fmt.Errorf("%w: schema_version field missing in %q", ErrMalformed, target)
	}
	if v.SchemaVersion != ReviewVerdictSchemaVersion {
		return nil, fmt.Errorf("%w: schema_version = %d; want %d in %q",
			ErrMalformed, v.SchemaVersion, ReviewVerdictSchemaVersion, target)
	}

	// Validate verdict: key must be present and a recognised value.
	if _, ok := raw["verdict"]; !ok {
		return nil, fmt.Errorf("%w: verdict field missing in %q", ErrMalformed, target)
	}
	switch v.Verdict {
	case ReviewVerdictApprove, ReviewVerdictRequestChanges, ReviewVerdictBlock:
		// valid
	default:
		return nil, fmt.Errorf("%w: verdict = %q; must be APPROVE, REQUEST_CHANGES, or BLOCK in %q",
			ErrMalformed, v.Verdict, target)
	}

	// Validate flags: key must be present (null → empty slice is acceptable).
	if _, ok := raw["flags"]; !ok {
		return nil, fmt.Errorf("%w: flags field missing in %q", ErrMalformed, target)
	}
	if v.Flags == nil {
		v.Flags = []string{}
	}

	// Validate notes: key must be present and non-empty.
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

	// Check that the source exists; report ErrNotFound if absent.
	//nolint:gosec // G304: path constructed from workspace_path + known relative segments; not user input
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: review.json absent at %q", ErrNotFound, src)
		}
		return fmt.Errorf("workspace: ArchiveVerdict: Stat source %q: %w", src, err)
	}

	// Check that the destination does not exist; error on double-archive.
	//nolint:gosec // G304: path constructed from workspace_path + known relative segments; not user input
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("workspace: ArchiveVerdict: destination already exists at %q (double-archive at iteration %d)", dst, iterationN)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("workspace: ArchiveVerdict: Stat destination %q: %w", dst, err)
	}

	// Atomic rename: POSIX rename(2) is atomic within one filesystem.
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("workspace: ArchiveVerdict: Rename %q → %q: %w", src, dst, err)
	}

	// Fsync the parent directory so the rename is durable — best-effort on
	// macOS/APFS per WM-026 precedent.
	dir := filepath.Dir(src)
	//nolint:gosec // G304: path constructed from workspace_path + known relative segments; not user input
	dirFD, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("workspace: ArchiveVerdict: Open dir %q for fsync: %w", dir, err)
	}
	_ = dirFD.Sync() // best-effort on APFS per WM-026 / WM-013a precedent
	if err := dirFD.Close(); err != nil {
		return fmt.Errorf("workspace: ArchiveVerdict: Close dir fd: %w", err)
	}

	return nil
}
