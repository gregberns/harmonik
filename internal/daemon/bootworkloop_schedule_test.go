package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewScheduleStore_AbsentFileReturnsStore(t *testing.T) {
	t.Parallel()

	store, err := newScheduleStore(Config{ProjectDir: t.TempDir()})
	if err != nil {
		t.Fatalf("newScheduleStore: %v", err)
	}
	if store == nil {
		t.Fatal("newScheduleStore returned nil store for absent schedule file")
	}
}

func TestNewScheduleStore_MalformedFileIsFatal(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	scheduleDir := filepath.Join(projectDir, ".harmonik")
	if err := os.Mkdir(scheduleDir, 0o750); err != nil {
		t.Fatalf("create schedule directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scheduleDir, "schedules.json"), []byte("not json"), 0o600); err != nil {
		t.Fatalf("write malformed schedule file: %v", err)
	}

	store, err := newScheduleStore(Config{ProjectDir: projectDir})
	if err == nil {
		t.Fatal("newScheduleStore returned nil error for malformed schedule file")
	}
	if store != nil {
		t.Fatal("newScheduleStore returned a store for malformed schedule file")
	}
	if !strings.Contains(err.Error(), "daemon.Start: load schedule store:") {
		t.Fatalf("error = %q; want daemon startup schedule-store context", err)
	}
	if !strings.Contains(err.Error(), "schedule: Load: parse") {
		t.Fatalf("error = %q; want malformed schedule-file parse context", err)
	}
}
