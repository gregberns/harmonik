package keeper_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/keeper"
)

// TestAcquireLock_SucceedsOnFreshDir verifies that acquiring a lock in an
// empty project dir creates the lockfile and returns a non-nil Lock.
func TestAcquireLock_SucceedsOnFreshDir(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	lock, err := keeper.AcquireLock(projectDir, "test-agent")
	if err != nil {
		t.Fatalf("AcquireLock: unexpected error: %v", err)
	}
	defer func() {
		if releaseErr := lock.Release(); releaseErr != nil {
			t.Errorf("Release: %v", releaseErr)
		}
	}()

	lockPath := filepath.Join(projectDir, ".harmonik", "keeper", "test-agent.lock")
	if _, statErr := os.Stat(lockPath); os.IsNotExist(statErr) {
		t.Errorf("lockfile not created at %q", lockPath)
	}
}

// TestAcquireLock_SecondAcquireFails verifies that a second AcquireLock call
// for the same agent returns ErrLockHeld while the first Lock is still open.
func TestAcquireLock_SecondAcquireFails(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()

	lock1, err := keeper.AcquireLock(projectDir, "agent-x")
	if err != nil {
		t.Fatalf("first AcquireLock: %v", err)
	}
	defer func() { _ = lock1.Release() }() //nolint:errcheck // test cleanup

	_, err2 := keeper.AcquireLock(projectDir, "agent-x")
	if err2 == nil {
		t.Fatal("second AcquireLock succeeded; want ErrLockHeld")
	}
	if !errors.Is(err2, keeper.ErrLockHeld) {
		t.Errorf("second AcquireLock returned %v; want errors.Is(err, ErrLockHeld) == true", err2)
	}
}

// TestAcquireLock_SucceedsAfterRelease verifies that a second acquire for the
// same agent succeeds once the first Lock is released.
func TestAcquireLock_SucceedsAfterRelease(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()

	lock1, err := keeper.AcquireLock(projectDir, "agent-y")
	if err != nil {
		t.Fatalf("first AcquireLock: %v", err)
	}
	if err := lock1.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	lock2, err := keeper.AcquireLock(projectDir, "agent-y")
	if err != nil {
		t.Fatalf("second AcquireLock after release: %v", err)
	}
	_ = lock2.Release() //nolint:errcheck // test cleanup; explicit call avoids unnecessaryDefer lint
}

// TestAcquireLock_DifferentAgents verifies that locks for different agent names
// do not conflict with each other.
func TestAcquireLock_DifferentAgents(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()

	lock1, err := keeper.AcquireLock(projectDir, "agent-alpha")
	if err != nil {
		t.Fatalf("AcquireLock(agent-alpha): %v", err)
	}
	defer func() { _ = lock1.Release() }() //nolint:errcheck // test cleanup

	lock2, err := keeper.AcquireLock(projectDir, "agent-beta")
	if err != nil {
		t.Fatalf("AcquireLock(agent-beta) with agent-alpha locked: %v", err)
	}
	_ = lock2.Release() //nolint:errcheck // test cleanup; explicit call (not defer) avoids unnecessaryDefer lint
}

// TestAcquireLock_RejectsPathTraversal verifies that agent names containing
// path-traversal sequences are rejected before any filesystem access.
func TestAcquireLock_RejectsPathTraversal(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	cases := []string{"../daemon", "foo/bar", "../../etc/passwd", "a..b/c"}
	for _, agent := range cases {
		_, err := keeper.AcquireLock(projectDir, agent)
		if err == nil {
			t.Errorf("AcquireLock(%q): expected error, got nil", agent)
			continue
		}
		if !errors.Is(err, keeper.ErrInvalidAgent) {
			t.Errorf("AcquireLock(%q): got %v; want ErrInvalidAgent", agent, err)
		}
	}
}

// TestIsManaged_RejectsPathTraversal verifies that agent names with traversal
// sequences return false rather than resolving outside the keeper directory.
func TestIsManaged_RejectsPathTraversal(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	cases := []string{"../daemon", "foo/bar", "../../etc/passwd"}
	for _, agent := range cases {
		if keeper.IsManaged(projectDir, agent) {
			t.Errorf("IsManaged(%q): expected false for traversal agent name", agent)
		}
	}
}

// TestIsManaged_AbsentReturnsFalse verifies the fail-safe: absent .managed
// marker returns false.
func TestIsManaged_AbsentReturnsFalse(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	if keeper.IsManaged(projectDir, "my-agent") {
		t.Error("IsManaged: expected false when .managed marker is absent")
	}
}

// TestIsManaged_PresentReturnsTrue verifies that creating the .managed marker
// causes IsManaged to return true.
func TestIsManaged_PresentReturnsTrue(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o750); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	markerPath := filepath.Join(keeperDir, "my-agent.managed")
	if err := os.WriteFile(markerPath, []byte{}, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if !keeper.IsManaged(projectDir, "my-agent") {
		t.Error("IsManaged: expected true when .managed marker is present")
	}
}
