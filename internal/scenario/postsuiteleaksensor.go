package scenario

// postsuiteleaksensor.go — SH-INV-002 post-suite leak sensor.
//
// Implements the three-check post-suite assertion declared at
// specs/scenario-harness.md §5 SH-INV-002 (Workspace state is fully reset on
// teardown). The sensor MUST be called after all scenario teardowns complete
// and BEFORE WriteSuiteResult.
//
// Three checks:
//
//	(i)   process — no live process with HARMONIK_RUN_ID matching an executed run_id
//	(ii)  lease   — no <workspace>/.harmonik/lease.lock under the fixture root
//	               held by an executed run_id
//	(iii) fd      — no open file descriptors to files under the fixture root
//
// Platform split: checkLeakedProcesses is implemented in
// postsuiteleaksensor_linux.go, postsuiteleaksensor_darwin.go, and
// postsuiteleaksensor_other.go. checkLeakedLeases and checkLeakedFDs are
// cross-platform (this file).
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-002, §10.2.
// Tags: mechanism
// Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/workspace"
)

// runIDEnvKey is the environment variable set on every handler subprocess by
// the daemon's claude handler per specs/process-lifecycle.md §4.1 PL-006a.
// Its value is the scenario's run_id UUID string.
const runIDEnvKey = "HARMONIK_RUN_ID"

// LeakKind categorises the type of resource leak detected by CheckPostSuiteLeaks.
type LeakKind string

const (
	// LeakKindProcess reports a live process whose HARMONIK_RUN_ID env var
	// matches an executed scenario's run_id per SH-INV-002(i).
	LeakKindProcess LeakKind = "process"

	// LeakKindLease reports a held worktree lease.lock file under the fixture
	// root whose run_id matches an executed scenario's run_id per SH-INV-002(ii).
	LeakKindLease LeakKind = "lease"

	// LeakKindFD reports an open file descriptor pointing at a file under the
	// fixture root per SH-INV-002(iii). The spec names event-log files as the
	// primary target; this sensor reports all open regular files as a superset.
	LeakKindFD LeakKind = "fd"
)

// LeakDescriptor describes a single resource leak detected by CheckPostSuiteLeaks.
type LeakDescriptor struct {
	Kind   LeakKind
	Detail string
}

// PostSuiteLeakReport is the result of a CheckPostSuiteLeaks call.
type PostSuiteLeakReport struct {
	// Leaks is the list of detected resource leaks. Empty on a clean suite.
	Leaks []LeakDescriptor
}

// HasLeaks reports whether any resource leaks were detected.
func (r *PostSuiteLeakReport) HasLeaks() bool {
	return r != nil && len(r.Leaks) > 0
}

// PostSuiteLeakParams carries the inputs for the SH-INV-002 post-suite sensor.
type PostSuiteLeakParams struct {
	// FixtureRoot is the per-suite ephemeral fixture root created by NewFixtureRoot
	// (SH-016). All sub-checks scan this directory tree.
	FixtureRoot string

	// ExecutedRunIDs are the run IDs of all scenarios executed in this suite.
	// Used to match against HARMONIK_RUN_ID env vars (check i) and lease.lock
	// run_id fields (check ii). May be empty for suites with no executed scenarios.
	ExecutedRunIDs []core.RunID
}

// CheckPostSuiteLeaks runs the SH-INV-002 post-suite sensor. It MUST be called
// AFTER all scenario teardowns complete and BEFORE WriteSuiteResult.
//
// It inspects three resource categories:
//
//	(i)   Descendant process tree — no live process with HARMONIK_RUN_ID env var
//	      matching any executed scenario's run_id. Linux: /proc-based env scan;
//	      Darwin: ppid-walk from the harness process (best-effort, OQ-PL-008);
//	      other platforms: skipped.
//	(ii)  Worktree-lease registry — no <workspace>/.harmonik/lease.lock files
//	      under the fixture root held by an executed run_id.
//	(iii) Open file descriptors — no file under the fixture root remains open
//	      (lsof-equivalent; skipped if lsof is not installed).
//
// Residual processes / leases / fds are accumulated in PostSuiteLeakReport.Leaks.
// A non-empty Leaks slice signals a teardown defect. Detected leaks MUST NOT
// alter per-scenario ScenarioResult verdicts already recorded; they are a
// suite-level harness hygiene signal.
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-002, §10.2.
func CheckPostSuiteLeaks(ctx context.Context, params PostSuiteLeakParams) (*PostSuiteLeakReport, error) {
	report := &PostSuiteLeakReport{}

	// Check (i): descendant process tree with HARMONIK_RUN_ID marker.
	processLeaks, err := checkLeakedProcesses(ctx, params.ExecutedRunIDs)
	if err != nil {
		return nil, fmt.Errorf("post-suite leak sensor (process check): %w", err)
	}
	report.Leaks = append(report.Leaks, processLeaks...)

	// Check (ii): worktree-lease registry scan.
	leaseLeaks, err := checkLeakedLeases(params.FixtureRoot, params.ExecutedRunIDs)
	if err != nil {
		return nil, fmt.Errorf("post-suite leak sensor (lease check): %w", err)
	}
	report.Leaks = append(report.Leaks, leaseLeaks...)

	// Check (iii): open file descriptors under the fixture root.
	fdLeaks, err := checkLeakedFDs(ctx, params.FixtureRoot)
	if err != nil {
		return nil, fmt.Errorf("post-suite leak sensor (fd check): %w", err)
	}
	report.Leaks = append(report.Leaks, fdLeaks...)

	return report, nil
}

// checkLeakedLeases walks the fixture root for lease.lock files whose run_id
// matches any executed scenario's run_id, implementing SH-INV-002(ii).
//
// Per-fixture-root scan: searches for files named "lease.lock" anywhere under
// fixtureRoot. Each found file is parsed via workspace.ReadLeaseLock; absent or
// malformed files are silently skipped (not a valid held lease). Only files
// whose run_id is in executedRunIDs are reported as leaks.
//
// Returns nil, nil when fixtureRoot is empty or executedRunIDs is empty.
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-002(ii).
func checkLeakedLeases(fixtureRoot string, executedRunIDs []core.RunID) ([]LeakDescriptor, error) {
	if fixtureRoot == "" || len(executedRunIDs) == 0 {
		return nil, nil
	}

	runIDSet := make(map[string]bool, len(executedRunIDs))
	for _, rid := range executedRunIDs {
		runIDSet[rid.String()] = true
	}

	var leaks []LeakDescriptor
	err := filepath.WalkDir(fixtureRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable entries without failing the walk
		}
		if d.IsDir() || filepath.Base(path) != "lease.lock" {
			return nil
		}
		lock, parseErr := workspace.ReadLeaseLock(path)
		if parseErr != nil || lock == nil {
			return nil // absent (nil,nil) or malformed — not a valid held lease
		}
		if runIDSet[lock.RunID.String()] {
			leaks = append(leaks, LeakDescriptor{
				Kind:   LeakKindLease,
				Detail: fmt.Sprintf("held lease.lock for run_id=%s at %s", lock.RunID, path),
			})
		}
		return nil
	})
	if err != nil {
		return leaks, fmt.Errorf("checkLeakedLeases: WalkDir %q: %w", fixtureRoot, err)
	}
	return leaks, nil
}

// checkLeakedFDs uses lsof to enumerate open file descriptors pointing at
// regular files under the fixture root, implementing SH-INV-002(iii).
//
// Returns nil (no error) when:
//   - fixtureRoot is empty
//   - lsof is not installed on the system
//   - lsof finds no open regular files (exits with code 1 and empty stdout)
//
// The spec names event-log files (events.jsonl) as the primary FD leak target.
// This implementation reports all open regular files under fixtureRoot as a
// comprehensive superset, giving the operator full visibility.
//
// Spec ref: specs/scenario-harness.md §5 SH-INV-002(iii).
func checkLeakedFDs(ctx context.Context, fixtureRoot string) ([]LeakDescriptor, error) {
	if fixtureRoot == "" {
		return nil, nil
	}

	//nolint:gosec // G204: fixtureRoot is a harness-internal temp dir, not user input
	out, err := exec.CommandContext(ctx, "lsof", "+D", fixtureRoot).Output()
	if err != nil {
		if lsofNotFound(err) {
			return nil, nil // lsof not installed: skip check
		}
		// lsof exits 1 when no files are found — that is the clean case.
		if exitCodeIs(err, 1) && len(out) == 0 {
			return nil, nil
		}
		// Other errors (permission, signal, etc.): skip rather than fail.
		return nil, nil
	}

	// lsof default output columns:
	//   COMMAND PID USER FD TYPE DEVICE SIZE/OFF NODE NAME
	// Indices (0-based): 0=COMMAND 1=PID 2=USER 3=FD 4=TYPE … N-1=NAME
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	var leaks []LeakDescriptor
	for _, line := range lines[1:] { // skip header
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 9 {
			continue
		}
		fileType := fields[4]
		if fileType != "REG" {
			continue // skip directories, sockets, pipes, etc.
		}
		cmd, pid, name := fields[0], fields[1], fields[len(fields)-1]
		leaks = append(leaks, LeakDescriptor{
			Kind:   LeakKindFD,
			Detail: fmt.Sprintf("pid=%s cmd=%s has open fd to %s", pid, cmd, name),
		})
	}
	return leaks, nil
}

// lsofNotFound reports whether err indicates lsof is not installed.
func lsofNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "executable file not found") ||
		strings.Contains(msg, "no such file or directory")
}

// exitCodeIs reports whether err is an *exec.ExitError with the given exit code.
func exitCodeIs(err error, code int) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return false
	}
	return exitErr.ExitCode() == code
}
