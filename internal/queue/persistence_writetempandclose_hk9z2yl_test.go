package queue

// persistence_writetempandclose_hk9z2yl_test.go — coverage for the errors.Join
// fold in writeTempAndClose (de9aac48, follow-up bead hk-9z2yl).
//
// Before de9aac48 the atomic-write dance in Persist and MigrateFromLegacy
// discarded f.Close() on every failure path (`_ = f.Close()`), so a Write that
// succeeded into the page cache followed by a Close that failed to flush was
// reported as a clean write. writeTempAndClose now JOINS every step's error, and
// closes f on all three paths.
//
// The contract these tests pin:
//   - success: data lands in the file AND f is closed;
//   - write failure: the returned error is an errors.Join fold carrying BOTH the
//     write error and the close error, not just the first one.
//
// Deleting the errors.Join and reverting to `_ = f.Close(); return writeErr`
// makes TestWriteTempAndClose_WriteFailure_JoinsCloseError fail, because the
// returned error no longer implements `Unwrap() []error`.
//
// Not covered here: the f.Sync() failure branch. There is no portable way to
// force fsync to fail on a regular file on both darwin and linux, and faking it
// would require an interface seam that writeTempAndClose deliberately does not
// have (it takes a concrete *os.File so callers cannot pass a half-real file).
//
// Bead ref: hk-9z2yl.

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteTempAndClose_Success_WritesAndCloses verifies the happy path both
// persists the bytes and leaves no open descriptor behind — the close is part
// of the function's contract, not an optional cleanup.
func TestWriteTempAndClose_Success_WritesAndCloses(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "queue.json.tmp")
	f, err := os.Create(path) //nolint:gosec // G304: test-only path under t.TempDir()
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}

	payload := []byte(`{"schema_version":1}`)
	if writeErr := writeTempAndClose(f, payload); writeErr != nil {
		t.Fatalf("writeTempAndClose on a healthy file: %v", writeErr)
	}

	// The file must already be closed: a second Close reports os.ErrClosed.
	if closeErr := f.Close(); !errors.Is(closeErr, os.ErrClosed) {
		t.Errorf("file left open by writeTempAndClose: second Close = %v, want os.ErrClosed", closeErr)
	}

	got, readErr := os.ReadFile(path) //nolint:gosec // G304: test-only path under t.TempDir()
	if readErr != nil {
		t.Fatalf("read back temp file: %v", readErr)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("file contents = %q, want %q", got, payload)
	}
}

// TestWriteTempAndClose_WriteFailure_JoinsCloseError is the regression test for
// the fold itself. A pre-closed *os.File fails BOTH Write and Close, so a
// correct implementation returns an errors.Join of two errors; the pre-de9aac48
// implementation returned only the write error and dropped the close error.
func TestWriteTempAndClose_WriteFailure_JoinsCloseError(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "already-closed.tmp")
	f, err := os.Create(path) //nolint:gosec // G304: test-only path under t.TempDir()
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if closeErr := f.Close(); closeErr != nil {
		t.Fatalf("pre-close temp file: %v", closeErr)
	}

	gotErr := writeTempAndClose(f, []byte("payload"))
	if gotErr == nil {
		t.Fatal("writeTempAndClose on a closed file returned nil, want an error")
	}

	// The fold SHAPE is what is under test, so this asserts on the concrete
	// errors.Join type rather than using errors.Is/As.
	joined, ok := gotErr.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("returned error %T (%v) is not an errors.Join fold: the close error was dropped", gotErr, gotErr)
	}
	parts := joined.Unwrap()
	if len(parts) != 2 {
		t.Fatalf("joined error carries %d causes (%v), want 2 (write error + close error)", len(parts), parts)
	}
	for i, part := range parts {
		if !errors.Is(part, os.ErrClosed) {
			t.Errorf("joined cause %d = %v, want an os.ErrClosed", i, part)
		}
	}
	// The whole fold must still answer errors.Is for its causes, which is what
	// Persist's `%w: write temp %q: %w` wrapper relies on.
	if !errors.Is(gotErr, os.ErrClosed) {
		t.Errorf("errors.Is(joined, os.ErrClosed) = false; the cause chain is broken")
	}
}
