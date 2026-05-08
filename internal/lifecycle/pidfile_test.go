package lifecycle

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"syscall"
	"testing"
)

// TestPidfileAcquire_Success verifies that AcquirePidfile returns a non-nil
// *Pidfile whose Path() points at <projectDir>/.harmonik/daemon.pid, and that
// the file content is exactly three newline-terminated lines parseable as
// PID / PGID / instanceID.
//
// Spec ref: process-lifecycle.md §4.1 PL-002b.
func TestPidfileAcquire_Success(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pid := os.Getpid()
	pgid, _ := syscall.Getpgid(pid) //nolint:errcheck // Getpgid fails only if pid doesn't exist; os.Getpid() is always valid
	instanceID := "01950000-0000-7001-8000-000000000001"

	pf, err := AcquirePidfile(projectDir, pid, pgid, instanceID)
	if err != nil {
		t.Fatalf("AcquirePidfile: unexpected error: %v", err)
	}
	t.Cleanup(func() { _ = pf.Release() }) //nolint:errcheck // cleanup error unactionable

	wantPath := pidfileFixturePath(projectDir)
	if pf.Path() != wantPath {
		t.Errorf("Path() = %q, want %q", pf.Path(), wantPath)
	}

	// Verify file content is exactly three lines.
	wantContent := fmt.Sprintf("%d\n%d\n%s\n", pid, pgid, instanceID)
	//nolint:gosec // G304: pidfilePath derived from t.TempDir() via plFixtureTempProjectDir; not user input
	data, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != wantContent {
		t.Errorf("pidfile content = %q, want %q", string(data), wantContent)
	}

	// Verify parseable via production reader.
	gotPID, gotPGID, gotInstanceID, err := ReadPidfile(projectDir)
	if err != nil {
		t.Fatalf("ReadPidfile: %v", err)
	}
	if gotPID != pid {
		t.Errorf("ReadPidfile PID = %d, want %d", gotPID, pid)
	}
	if gotPGID != pgid {
		t.Errorf("ReadPidfile PGID = %d, want %d", gotPGID, pgid)
	}
	if gotInstanceID != instanceID {
		t.Errorf("ReadPidfile instanceID = %q, want %q", gotInstanceID, instanceID)
	}
}

// TestPidfileAcquire_ConcurrentLock verifies that a second AcquirePidfile call
// (via a second fd on the same inode) returns ErrPidfileLocked and that
// errors.Is(err, ErrPidfileLocked) is true.
//
// flock(LOCK_EX|LOCK_NB) on the same inode from a different fd in the same
// process returns EWOULDBLOCK on Linux and macOS, which AcquirePidfile maps to
// ErrPidfileLocked.
//
// Spec ref: process-lifecycle.md §4.1 PL-002a — exclusive non-blocking lock.
func TestPidfileAcquire_ConcurrentLock(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pid := os.Getpid()
	pgid, _ := syscall.Getpgid(pid) //nolint:errcheck // Getpgid fails only if pid doesn't exist; os.Getpid() is always valid

	pf, err := AcquirePidfile(projectDir, pid, pgid, "01950000-0000-7001-8000-000000000010")
	if err != nil {
		t.Fatalf("first AcquirePidfile: %v", err)
	}
	t.Cleanup(func() { _ = pf.Release() }) //nolint:errcheck // cleanup error unactionable

	// Second acquire on the same file must fail with ErrPidfileLocked.
	_, err2 := AcquirePidfile(projectDir, pid, pgid, "01950000-0000-7001-8000-000000000011")
	if err2 == nil {
		t.Fatal("second AcquirePidfile: expected ErrPidfileLocked, got nil")
	}
	if !errors.Is(err2, ErrPidfileLocked) {
		t.Errorf("second AcquirePidfile: errors.Is(err, ErrPidfileLocked) = false; err = %v", err2)
	}
}

// TestPidfileRelease_AllowsReacquire verifies that after Release(), a
// subsequent AcquirePidfile on the same path succeeds (advisory flock is
// released when fd is closed).
//
// Spec ref: process-lifecycle.md §4.1 PL-002a — fd-lifetime advisory lock.
func TestPidfileRelease_AllowsReacquire(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pid := os.Getpid()
	pgid, _ := syscall.Getpgid(pid) //nolint:errcheck // Getpgid fails only if pid doesn't exist; os.Getpid() is always valid

	pf, err := AcquirePidfile(projectDir, pid, pgid, "01950000-0000-7001-8000-000000000020")
	if err != nil {
		t.Fatalf("first AcquirePidfile: %v", err)
	}

	if err := pf.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// After release, re-acquire must succeed.
	pf2, err := AcquirePidfile(projectDir, pid, pgid, "01950000-0000-7001-8000-000000000021")
	if err != nil {
		t.Fatalf("AcquirePidfile after Release: %v", err)
	}
	t.Cleanup(func() { _ = pf2.Release() }) //nolint:errcheck // cleanup error unactionable
}

// TestPidfileRelease_Idempotent verifies that calling Release() twice on a
// *Pidfile does not return an error on the second call.
//
// Spec ref: process-lifecycle.md §4.1 PL-002b — "Retain the fd for the
// daemon's lifetime; intermediate close() is FORBIDDEN" (Release is the
// single sanctioned close path).
func TestPidfileRelease_Idempotent(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pid := os.Getpid()
	pgid, _ := syscall.Getpgid(pid) //nolint:errcheck // Getpgid fails only if pid doesn't exist; os.Getpid() is always valid

	pf, err := AcquirePidfile(projectDir, pid, pgid, "01950000-0000-7001-8000-000000000030")
	if err != nil {
		t.Fatalf("AcquirePidfile: %v", err)
	}

	if err := pf.Release(); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	if err := pf.Release(); err != nil {
		t.Errorf("second Release (idempotent): unexpected error: %v", err)
	}
}

// TestPidfileAcquire_OCloexec verifies that the fd opened by AcquirePidfile
// has FD_CLOEXEC set, confirming O_CLOEXEC was applied. This is the key
// differentiator from plFixtureAcquirePidfile which omits O_CLOEXEC.
//
// Spec ref: process-lifecycle.md §4.1 PL-002b step 1 — "O_CLOEXEC is
// mandatory."
func TestPidfileAcquire_OCloexec(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pid := os.Getpid()
	pgid, _ := syscall.Getpgid(pid) //nolint:errcheck // Getpgid fails only if pid doesn't exist; os.Getpid() is always valid

	pf, err := AcquirePidfile(projectDir, pid, pgid, "01950000-0000-7001-8000-000000000040")
	if err != nil {
		t.Fatalf("AcquirePidfile: %v", err)
	}
	t.Cleanup(func() { _ = pf.Release() }) //nolint:errcheck // cleanup error unactionable

	// Access the raw fd via the unexported field — same package, so accessible.
	rawFD := pf.fd.Fd()
	// Use SYS_FCNTL directly; syscall.FcntlInt is not available on all
	// platforms in the standard library. SYS_FCNTL + F_GETFD is the portable
	// POSIX path on Linux and macOS.
	r1, _, errno := syscall.Syscall(syscall.SYS_FCNTL, rawFD, syscall.F_GETFD, 0)
	if errno != 0 {
		t.Fatalf("F_GETFD: errno %v", errno)
	}
	if r1&syscall.FD_CLOEXEC == 0 {
		t.Errorf("FD_CLOEXEC not set on pidfile fd; flags = 0x%x", r1)
	}
}

// TestPidfileAcquire_InodePreserved verifies that AcquirePidfile uses the
// truncate-rewrite pattern (NOT temp+rename), so the inode of a pre-existing
// pidfile is unchanged after acquire. This confirms the flock association is
// preserved on the same inode.
//
// Spec ref: process-lifecycle.md §4.1 PL-002b — "NOT temp+rename, which would
// break the flock association by giving the new file a different inode."
func TestPidfileAcquire_InodePreserved(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pidfilePath := pidfileFixturePath(projectDir)

	// Pre-populate the pidfile with garbage content to establish an inode.
	garbage := make([]byte, 100)
	for i := range garbage {
		garbage[i] = 'X'
	}
	if err := os.WriteFile(pidfilePath, garbage, 0o600); err != nil {
		t.Fatalf("WriteFile (pre-populate): %v", err)
	}

	// Capture the inode before acquire.
	var preStat syscall.Stat_t
	if err := syscall.Stat(pidfilePath, &preStat); err != nil {
		t.Fatalf("Stat (pre-acquire): %v", err)
	}
	preIno := preStat.Ino

	pid := os.Getpid()
	pgid, _ := syscall.Getpgid(pid) //nolint:errcheck // Getpgid fails only if pid doesn't exist; os.Getpid() is always valid

	pf, err := AcquirePidfile(projectDir, pid, pgid, "01950000-0000-7001-8000-000000000050")
	if err != nil {
		t.Fatalf("AcquirePidfile: %v", err)
	}
	t.Cleanup(func() { _ = pf.Release() }) //nolint:errcheck // cleanup error unactionable

	// Capture the inode after acquire.
	var postStat syscall.Stat_t
	if err := syscall.Stat(pidfilePath, &postStat); err != nil {
		t.Fatalf("Stat (post-acquire): %v", err)
	}
	postIno := postStat.Ino

	if preIno != postIno {
		t.Errorf("inode changed: pre=%d post=%d; AcquirePidfile must NOT use temp+rename", preIno, postIno)
	}
}

// TestPidfileAcquire_TruncatesGarbage verifies that a pre-existing file with
// garbage content is fully truncated by AcquirePidfile, leaving exactly 3
// lines with no leftover bytes.
//
// Spec ref: process-lifecycle.md §4.1 PL-002b steps 3-4.
func TestPidfileAcquire_TruncatesGarbage(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pidfilePath := pidfileFixturePath(projectDir)

	// Write 100 bytes of garbage so any residual bytes would be detectable.
	garbage := make([]byte, 100)
	for i := range garbage {
		garbage[i] = 'Z'
	}
	if err := os.WriteFile(pidfilePath, garbage, 0o600); err != nil {
		t.Fatalf("WriteFile (garbage): %v", err)
	}

	pid := os.Getpid()
	pgid, _ := syscall.Getpgid(pid) //nolint:errcheck // Getpgid fails only if pid doesn't exist; os.Getpid() is always valid
	instanceID := "01950000-0000-7001-8000-000000000060"

	pf, err := AcquirePidfile(projectDir, pid, pgid, instanceID)
	if err != nil {
		t.Fatalf("AcquirePidfile: %v", err)
	}
	t.Cleanup(func() { _ = pf.Release() }) //nolint:errcheck // cleanup error unactionable

	//nolint:gosec // G304: pidfilePath derived from t.TempDir() via plFixtureTempProjectDir; not user input
	data, err := os.ReadFile(pidfilePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	wantContent := fmt.Sprintf("%d\n%d\n%s\n", pid, pgid, instanceID)
	if string(data) != wantContent {
		t.Errorf("post-acquire content = %q, want %q (garbage not fully truncated)", string(data), wantContent)
	}
}

// TestReadPidfile_ThreeLine verifies the production reader correctly parses a
// well-formed three-line pidfile.
//
// Spec ref: process-lifecycle.md §4.1 PL-002b.
func TestReadPidfile_ThreeLine(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pidfilePath := pidfileFixturePath(projectDir)

	wantPID := 12345
	wantPGID := 12340
	wantInstanceID := "01950000-0000-7001-8000-000000000070"
	content := fmt.Sprintf("%d\n%d\n%s\n", wantPID, wantPGID, wantInstanceID)

	if err := os.WriteFile(pidfilePath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	gotPID, gotPGID, gotInstanceID, err := ReadPidfile(projectDir)
	if err != nil {
		t.Fatalf("ReadPidfile: %v", err)
	}
	if gotPID != wantPID {
		t.Errorf("PID = %d, want %d", gotPID, wantPID)
	}
	if gotPGID != wantPGID {
		t.Errorf("PGID = %d, want %d", gotPGID, wantPGID)
	}
	if gotInstanceID != wantInstanceID {
		t.Errorf("instanceID = %q, want %q", gotInstanceID, wantInstanceID)
	}
}

// TestReadPidfile_TwoLineTolerance verifies that ReadPidfile tolerates the
// v0.4.0 two-line format, returning instanceID = "unknown".
//
// Spec ref: process-lifecycle.md §4.1 PL-002b — two-line v0.4.0 tolerance.
func TestReadPidfile_TwoLineTolerance(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pidfilePath := pidfileFixturePath(projectDir)

	wantPID := 22345
	wantPGID := 22340
	content := fmt.Sprintf("%d\n%d\n", wantPID, wantPGID)

	if err := os.WriteFile(pidfilePath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	gotPID, gotPGID, gotInstanceID, err := ReadPidfile(projectDir)
	if err != nil {
		t.Fatalf("ReadPidfile: %v", err)
	}
	if gotPID != wantPID {
		t.Errorf("PID = %d, want %d", gotPID, wantPID)
	}
	if gotPGID != wantPGID {
		t.Errorf("PGID = %d, want %d", gotPGID, wantPGID)
	}
	if gotInstanceID != "unknown" {
		t.Errorf("instanceID = %q, want %q (two-line tolerance)", gotInstanceID, "unknown")
	}
}

// TestReadPidfile_OneLineTolerance verifies that ReadPidfile tolerates the
// v0.2.x one-line format, returning pgid = 0 and instanceID = "unknown".
//
// Spec ref: process-lifecycle.md §4.1 PL-002b — one-line v0.2.x tolerance.
func TestReadPidfile_OneLineTolerance(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pidfilePath := pidfileFixturePath(projectDir)

	wantPID := 32345
	content := strconv.Itoa(wantPID) + "\n"

	if err := os.WriteFile(pidfilePath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	gotPID, gotPGID, gotInstanceID, err := ReadPidfile(projectDir)
	if err != nil {
		t.Fatalf("ReadPidfile: %v", err)
	}
	if gotPID != wantPID {
		t.Errorf("PID = %d, want %d", gotPID, wantPID)
	}
	if gotPGID != 0 {
		t.Errorf("PGID = %d, want 0 (one-line tolerance)", gotPGID)
	}
	if gotInstanceID != "unknown" {
		t.Errorf("instanceID = %q, want %q (one-line tolerance)", gotInstanceID, "unknown")
	}
}

// TestReadPidfile_EmptyFile verifies that ReadPidfile returns an error for an
// empty pidfile.
//
// Spec ref: process-lifecycle.md §4.1 PL-002b — "Torn/unparseable pidfile →
// stale per PL-024."
func TestReadPidfile_EmptyFile(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pidfilePath := pidfileFixturePath(projectDir)

	if err := os.WriteFile(pidfilePath, []byte{}, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, _, _, err := ReadPidfile(projectDir)
	if err == nil {
		t.Error("ReadPidfile: expected error for empty pidfile, got nil")
	}
}

// TestReadPidfile_UnparsablePID verifies that ReadPidfile returns an error when
// line 1 is not a valid integer.
//
// Spec ref: process-lifecycle.md §4.1 PL-002b — "Torn/unparseable pidfile →
// stale per PL-024."
func TestReadPidfile_UnparsablePID(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pidfilePath := pidfileFixturePath(projectDir)

	if err := os.WriteFile(pidfilePath, []byte("not-a-pid\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, _, _, err := ReadPidfile(projectDir)
	if err == nil {
		t.Error("ReadPidfile: expected error for unparseable PID, got nil")
	}
}

// TestReadPidfile_UnparsablePGID verifies that ReadPidfile returns an error
// when line 2 is present but not a valid integer.
//
// Spec ref: process-lifecycle.md §4.1 PL-002b — "Torn/unparseable pidfile →
// stale per PL-024."
func TestReadPidfile_UnparsablePGID(t *testing.T) {
	t.Parallel()

	projectDir := plFixtureTempProjectDir(t)
	pidfilePath := pidfileFixturePath(projectDir)

	content := "42345\nnot-a-pgid\n"
	if err := os.WriteFile(pidfilePath, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, _, _, err := ReadPidfile(projectDir)
	if err == nil {
		t.Error("ReadPidfile: expected error for unparseable PGID, got nil")
	}
}

// pidfileFixturePath returns the canonical pidfile path for a project. This is
// the per-bead helper (prefix: pidfileFixture) used internally within
// pidfile_test.go.
func pidfileFixturePath(projectDir string) string {
	return plFixturePidfilePath(projectDir)
}
