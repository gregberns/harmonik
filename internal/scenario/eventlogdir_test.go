package scenario

import (
	"path/filepath"
	"strings"
	"testing"
)

func eventLogFixtureProjectRoot(t *testing.T) string {
	t.Helper()
	return "/tmp/harmonik-fixture/my-scenario/project"
}

func eventLogFixtureOperatorRoot(t *testing.T) string {
	t.Helper()
	return "/home/user/myproject"
}

func TestEventLogRelPath(t *testing.T) {
	t.Parallel()

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

	if !filepath.IsAbs(got) {
		t.Errorf("EventLogPath(%q) = %q: expected absolute path", root, got)
	}

	if !strings.HasPrefix(got, root+string(filepath.Separator)) {
		t.Errorf("EventLogPath(%q) = %q: result is not a descendant of the project root", root, got)
	}

	wantSuffix := filepath.Join(".harmonik", "events", "events.jsonl")
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("EventLogPath(%q) = %q: does not end with %q", root, got, wantSuffix)
	}
}

func TestEventLogPath_NotOperatorRoot(t *testing.T) {
	t.Parallel()

	syntheticRoot := eventLogFixtureProjectRoot(t)
	operatorRoot := eventLogFixtureOperatorRoot(t)

	syntheticLog := EventLogPath(syntheticRoot)
	operatorLog := EventLogPath(operatorRoot)

	if syntheticLog == operatorLog {
		t.Errorf("EventLogPath returned identical path for synthetic root %q and operator root %q: %q",
			syntheticRoot, operatorRoot, syntheticLog)
	}

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

	if !filepath.IsAbs(got) {
		t.Errorf("EventLogDir(%q) = %q: expected absolute path", root, got)
	}

	if !strings.HasPrefix(got, root+string(filepath.Separator)) {
		t.Errorf("EventLogDir(%q) = %q: result is not a descendant of the project root", root, got)
	}

	wantDir := filepath.Dir(EventLogPath(root))
	if got != wantDir {
		t.Errorf("EventLogDir(%q) = %q, want %q", root, got, wantDir)
	}
}

func TestEventLogDir_EndsWithEventsDir(t *testing.T) {
	t.Parallel()

	root := eventLogFixtureProjectRoot(t)
	got := EventLogDir(root)

	wantSuffix := filepath.Join(".harmonik", "events")
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("EventLogDir(%q) = %q: does not end with %q", root, got, wantSuffix)
	}
}

func TestEventLogPath_DirEqualsEventLogDir(t *testing.T) {
	t.Parallel()

	root := eventLogFixtureProjectRoot(t)

	wantDir := filepath.Dir(EventLogPath(root))
	gotDir := EventLogDir(root)
	if gotDir != wantDir {
		t.Errorf("EventLogDir(%q) = %q, want %q (filepath.Dir of EventLogPath)", root, gotDir, wantDir)
	}
}
