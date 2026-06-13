package keeper_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

// TestReadManagedSessionID_AbsentFileReturnsEmpty verifies that a missing .managed
// file returns ("", nil) rather than an error. (Refs: hk-igt)
func TestReadManagedSessionID_AbsentFileReturnsEmpty(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	sid, err := keeper.ReadManagedSessionID(projectDir, "no-agent")
	if err != nil {
		t.Fatalf("ReadManagedSessionID absent: unexpected error: %v", err)
	}
	if sid != "" {
		t.Errorf("ReadManagedSessionID absent: got %q; want empty string", sid)
	}
}

// TestReadManagedSessionID_EmptyFileReturnsEmpty verifies that an empty .managed
// marker (old-style, no session_id content) returns ("", nil). (Refs: hk-igt)
func TestReadManagedSessionID_EmptyFileReturnsEmpty(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	keeperDir := filepath.Join(projectDir, ".harmonik", "keeper")
	if err := os.MkdirAll(keeperDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(keeperDir, "my-agent.managed"), []byte{}, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sid, err := keeper.ReadManagedSessionID(projectDir, "my-agent")
	if err != nil {
		t.Fatalf("ReadManagedSessionID empty: unexpected error: %v", err)
	}
	if sid != "" {
		t.Errorf("ReadManagedSessionID empty: got %q; want empty string", sid)
	}
}

// TestWriteAndReadManagedSessionID verifies round-trip write then read of a
// session_id in .managed, and that IsManaged still returns true afterward.
// (Refs: hk-igt)
func TestWriteAndReadManagedSessionID(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "round-trip-agent"
	want := "sess-abc123"

	if err := keeper.WriteManagedSessionID(projectDir, agent, want); err != nil {
		t.Fatalf("WriteManagedSessionID: %v", err)
	}

	// IsManaged must still return true (file exists).
	if !keeper.IsManaged(projectDir, agent) {
		t.Error("IsManaged: expected true after WriteManagedSessionID")
	}

	got, err := keeper.ReadManagedSessionID(projectDir, agent)
	if err != nil {
		t.Fatalf("ReadManagedSessionID: %v", err)
	}
	if got != want {
		t.Errorf("ReadManagedSessionID = %q; want %q", got, want)
	}
}

// TestWriteManagedSessionID_ConcurrentWrites verifies that multiple goroutines
// (simulating concurrent watcher/cycler/rebind-CLI writers) can call
// WriteManagedSessionID concurrently without leaving the .managed file in a
// partial or corrupt state. Each writer uses a unique temp path (os.CreateTemp)
// so writers never trample each other's in-flight content. After all goroutines
// complete, .managed must be readable and contain a well-formed session_id line.
// Refs: hk-b5e2 (os.CreateTemp hardening).
func TestWriteManagedSessionID_ConcurrentWrites(t *testing.T) {
	t.Parallel()

	const writers = 20
	projectDir := t.TempDir()
	agent := "concurrent-agent"

	var wg sync.WaitGroup
	wg.Add(writers)
	errs := make([]error, writers)
	sids := make([]string, writers)
	for i := range writers {
		i := i
		sid := fmt.Sprintf("sess-%04d", i)
		sids[i] = sid
		go func() {
			defer wg.Done()
			errs[i] = keeper.WriteManagedSessionID(projectDir, agent, sid)
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("writer %d: %v", i, err)
		}
	}

	// .managed must still exist and contain a valid (non-empty) session_id.
	if !keeper.IsManaged(projectDir, agent) {
		t.Fatal("IsManaged: expected true after concurrent writes")
	}
	got, err := keeper.ReadManagedSessionID(projectDir, agent)
	if err != nil {
		t.Fatalf("ReadManagedSessionID after concurrent writes: %v", err)
	}
	// The winning session_id must be one of the values written by a goroutine —
	// it cannot be empty or a partial mix of two writes.
	found := false
	for _, sid := range sids {
		if got == sid {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ReadManagedSessionID = %q; want one of the %d written session_ids", got, writers)
	}
}

// TestWriteManagedSessionID_ClearBinding verifies that passing an empty sessionID
// clears the binding while preserving the .managed marker. (Refs: hk-igt)
func TestWriteManagedSessionID_ClearBinding(t *testing.T) {
	t.Parallel()

	projectDir := t.TempDir()
	agent := "clear-binding-agent"

	// Write a session_id then clear it.
	if err := keeper.WriteManagedSessionID(projectDir, agent, "sess-to-clear"); err != nil {
		t.Fatalf("WriteManagedSessionID: %v", err)
	}
	if err := keeper.WriteManagedSessionID(projectDir, agent, ""); err != nil {
		t.Fatalf("WriteManagedSessionID clear: %v", err)
	}

	// File must still exist (IsManaged = true).
	if !keeper.IsManaged(projectDir, agent) {
		t.Error("IsManaged: expected true after clearing binding")
	}
	// Binding must be empty.
	sid, err := keeper.ReadManagedSessionID(projectDir, agent)
	if err != nil {
		t.Fatalf("ReadManagedSessionID after clear: %v", err)
	}
	if sid != "" {
		t.Errorf("ReadManagedSessionID after clear: got %q; want empty string", sid)
	}
}
