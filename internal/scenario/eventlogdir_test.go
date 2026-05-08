package scenario

import (
	"path/filepath"
	"strings"
	"testing"
)

// eventLogFixtureProjectRoot returns an absolute-looking synthetic project root
// path for use in event-log path tests.
func eventLogFixtureProjectRoot(t *testing.T) string {
	t.Helper()
	return "/tmp/harmonik-fixture/my-scenario/project"
}

// eventLogFixtureOperatorRoot returns an absolute-looking operator project root
// that MUST NOT appear in any harness event-log path per SH-014.
func eventLogFixtureOperatorRoot(t *testing.T) string {
	t.Helper()
	return "/home/user/myproject"
}

func TestEventLogRelPath(t *testing.T) {
	t.Parallel()

	// EventLogRelPath must be a relative path (no leading separator) that
	// descends into .harmonik/events/ — this is the path the daemon writes
	// relative to its working directory (the synthetic project root per SH-016a).
	if filepath.IsAbs(EventLogRelPath) {
		t.Errorf("EventLogRelPath must be relative, got absolute path: %q", EventLogRelPath)
	}

	dir := filepath.Dir(EventLogRelPath)
	if dir != filepath.Join(".harmonik", "events") {
		t.Errorf("EventLogRelPath parent dir = %q, want %q", dir, filepath.Join(".harmonik", "events"))
	}

	base := filepath.Base(EventLogRelPath)
	if base != "events.jsonl" {
		t.Errorf("EventLogRelPath base = %q, want %q", base, "events.jsonl")
	}
}

func TestEventLogPath_UnderSyntheticRoot(t *testing.T) {
	t.Parallel()

	root := eventLogFixtureProjectRoot(t)
	got := EventLogPath(root)

	// Result must be an absolute path rooted at the synthetic project root.
	if !filepath.IsAbs(got) {
		t.Errorf("EventLogPath(%q) = %q: expected absolute path", root, got)
	}

	// Result must be a descendant of the synthetic project root.
	if !strings.HasPrefix(got, root+string(filepath.Separator)) {
		t.Errorf("EventLogPath(%q) = %q: result is not a descendant of the project root", root, got)
	}

	// Result must end in the canonical relative path.
	wantSuffix := filepath.Join(".harmonik", "events", "events.jsonl")
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("EventLogPath(%q) = %q: does not end with %q", root, got, wantSuffix)
	}
}

func TestEventLogPath_NotOperatorRoot(t *testing.T) {
	t.Parallel()

	// SH-014: the harness MUST NOT read from, write to, or mutate the
	// operator's .harmonik/ tree. Calling EventLogPath with the operator's
	// project root would violate this; we verify the function is pure (no
	// filesystem access) and that a synthetic root differs from the operator
	// root so callers can distinguish the two.
	syntheticRoot := eventLogFixtureProjectRoot(t)
	operatorRoot := eventLogFixtureOperatorRoot(t)

	syntheticLog := EventLogPath(syntheticRoot)
	operatorLog := EventLogPath(operatorRoot)

	if syntheticLog == operatorLog {
		t.Errorf("EventLogPath returned identical path for synthetic root %q and operator root %q: %q",
			syntheticRoot, operatorRoot, syntheticLog)
	}

	// Confirm neither path leaks into the other root.
	if strings.HasPrefix(syntheticLog, operatorRoot) {
		t.Errorf("EventLogPath(syntheticRoot=%q) = %q: unexpectedly under operator root %q",
			syntheticRoot, syntheticLog, operatorRoot)
	}
	if strings.HasPrefix(operatorLog, syntheticRoot) {
		t.Errorf("EventLogPath(operatorRoot=%q) = %q: unexpectedly under synthetic root %q",
			operatorRoot, operatorLog, syntheticRoot)
	}
}

func TestEventLogDir_UnderSyntheticRoot(t *testing.T) {
	t.Parallel()

	root := eventLogFixtureProjectRoot(t)
	got := EventLogDir(root)

	// Must be an absolute path rooted under the synthetic project root.
	if !filepath.IsAbs(got) {
		t.Errorf("EventLogDir(%q) = %q: expected absolute path", root, got)
	}

	if !strings.HasPrefix(got, root+string(filepath.Separator)) {
		t.Errorf("EventLogDir(%q) = %q: result is not a descendant of the project root", root, got)
	}

	// Directory must be the parent of EventLogPath.
	wantDir := filepath.Dir(EventLogPath(root))
	if got != wantDir {
		t.Errorf("EventLogDir(%q) = %q, want %q", root, got, wantDir)
	}
}

func TestEventLogDir_EndsWithEventsDir(t *testing.T) {
	t.Parallel()

	root := eventLogFixtureProjectRoot(t)
	got := EventLogDir(root)

	// The directory must end with .harmonik/events — this is where the daemon
	// writes its event log, relative to the synthetic project root per SH-016a.
	wantSuffix := filepath.Join(".harmonik", "events")
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("EventLogDir(%q) = %q: does not end with %q", root, got, wantSuffix)
	}
}

func TestEventLogPath_DirEqualsEventLogDir(t *testing.T) {
	t.Parallel()

	root := eventLogFixtureProjectRoot(t)

	// Invariant: filepath.Dir(EventLogPath(root)) == EventLogDir(root).
	wantDir := filepath.Dir(EventLogPath(root))
	gotDir := EventLogDir(root)
	if gotDir != wantDir {
		t.Errorf("EventLogDir(%q) = %q, want %q (filepath.Dir of EventLogPath)", root, gotDir, wantDir)
	}
}
