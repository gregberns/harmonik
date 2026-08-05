//go:build !windows

package daemon

// run_terminal_writer_lifetime_test.go — a run may not return while a writer it
// started is still making files under the project directory.
//
// The run terminal collects session data in a goroutine, on purpose: the
// collection reads the whole event log and the agent transcripts, and a run has
// nothing left to do with the answer. Off the hot path is the right shape. An
// OWNERLESS goroutine is not. sessiondata.Append creates
// <projectDir>/.harmonik/ and appends session-data.jsonl inside it, so until
// that goroutine ends the run is still writing into a directory it has told its
// caller it is finished with.
//
// In the test suite the project directory is a t.TempDir. The write lands in
// the middle of the cleanup's RemoveAll: the cleanup deletes .harmonik, the
// goroutine re-creates it, and the final rmdir fails with "directory not
// empty". Go reports that against whichever test the cleanup happened to be
// running, which is why the victim changed from run to run and why running one
// test alone hid it — alone, the goroutine wins the race and nobody sees it.
// That is the defect. The cleanup error is only where it surfaced.
//
// The named pipe is what makes this deterministic, and it is why the file is
// !windows: syscall.Mkfifo has no Windows form. The package already carries
// that constraint for its signal tests.
//
// Helper prefix: writerLifetime.
//
// Bead ref: hk-59flr.

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// writerLifetimeStillBlocked is how long the test insists the run stays inside
// beadRunOne while the collection is held still. It is not a wait for anything
// to happen: the collection is blocked on a pipe with no writer and can never
// finish on its own, so any return within this window is a return that did not
// wait for it. Three seconds is far longer than the microseconds an unowned
// goroutine takes to lose the race in the field.
const writerLifetimeStillBlocked = 3 * time.Second

// writerLifetimeAfterRelease bounds how long the run may take to return ONCE
// the collection is released. It is what separates a run that waits for its
// writer from one that waits for a clock: a fixed delay long enough to survive
// the window above still returns at its own time, not at the writer's. The
// measured delay for a run that really waits is under two milliseconds, so this
// is a thousandfold margin, and the test logs what it actually saw.
//
// The two bounds together reject a fixed delay of any length except one between
// three and four seconds — longer than the whole run this fixture drives. That
// residual gap is stated rather than hidden: no single-drive test can close it,
// and shutting it would cost a second drive for a case nobody writes by
// accident.
const writerLifetimeAfterRelease = time.Second

// writerLifetimeWaitForReader opens the write end of a named pipe, and returns
// only once a READER has opened the other end. The second result is false when
// no reader arrived within timeout.
//
// This is a handshake, not a poll for time to pass. open(O_WRONLY|O_NONBLOCK)
// on a pipe with no reader fails with ENXIO and succeeds the instant a reader
// arrives, so a successful open is proof that the run's session-data collection
// has reached its first read and is now stuck there.
//
// It reports rather than calling t.Fatal, because the caller has a live
// goroutine to join first. A test that ends while that goroutine is still
// running panics the whole binary the moment the goroutine logs.
func writerLifetimeWaitForReader(t *testing.T, path string, timeout time.Duration) (*os.File, bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			return os.NewFile(uintptr(fd), path), true
		}
		if !errors.Is(err, syscall.ENXIO) {
			t.Errorf("writerLifetime: open the write end of %s: %v", path, err)
			return nil, false
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("writerLifetime: nothing opened %s for reading within %s.\n"+
		"The run must reach its session-data collection, which reads that file on its way to "+
		"writing session-data.jsonl. If it never does, this test measures nothing and the claim "+
		"below is defended by an absence.", path, timeout)
	return nil, false
}

// TestRunTerminal_TheRunWaitsForTheSessionDataWriterItStarted asserts that
// beadRunOne does not return while the session-data collection it started is
// still running.
//
// The collection is held at its first read by a named pipe with no writer, so
// "the writer is still going" is a fact this test establishes rather than a
// race it hopes to win. Three claims are checked, and the second and third are
// what stop a delay from passing for a fix:
//
//  1. While the collection is blocked, the run has not returned.
//  2. Once the collection is released, the run returns promptly. A run that
//     sleeps for a fixed time returns when its timer says so, not when its
//     writer is done, and fails this.
//  3. session-data.jsonl is on disk by the time the run returns. This is the
//     positive half: without it, "the run waited" would also be true of a run
//     whose collection never wrote anything, and the wait would protect
//     nothing.
func TestRunTerminal_TheRunWaitsForTheSessionDataWriterItStarted(t *testing.T) {
	t.Parallel()

	var (
		projectDir string
		fifoPath   string
	)
	seeded := make(chan struct{})

	// The drive runs in a goroutine because the test has to observe the run
	// from outside it: the whole claim is about what is true at the moment
	// beadRunOne returns. The fixture's own t.Fatalf calls are setup failures
	// only; every assertion this test makes is made on the test goroutine, and
	// the failure paths below release the collection and join the drive rather
	// than abandoning it.
	driveDone := make(chan struct{})
	go func() {
		defer close(driveDone)
		surviveRunDriveWith(t, surviveRunOpts{
			piRun: true,
			seedProject: func(dir string) {
				projectDir = dir
				eventsDir := filepath.Join(dir, ".harmonik", "events")
				if err := os.MkdirAll(eventsDir, 0o700); err != nil {
					t.Errorf("writerLifetime: create %s: %v", eventsDir, err)
					return
				}
				fifoPath = filepath.Join(eventsDir, "events.jsonl")
				if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
					t.Errorf("writerLifetime: mkfifo %s: %v", fifoPath, err)
					return
				}
				close(seeded)
			},
		})
	}()

	select {
	case <-seeded:
	case <-time.After(30 * time.Second):
		<-driveDone
		t.Fatal("writerLifetime: the fixture never seeded the project directory")
	}

	writeEnd, gotReader := writerLifetimeWaitForReader(t, fifoPath, 60*time.Second)
	if !gotReader {
		<-driveDone
		return
	}

	// Claim 1. The collection cannot get past its read, so the run cannot have
	// finished with the project directory.
	returnedEarly := false
	select {
	case <-driveDone:
		returnedEarly = true
		t.Errorf("the run returned while the session-data writer it started was still going.\n"+
			"That writer creates %s/.harmonik and appends session-data.jsonl inside it. The run has "+
			"told its caller it is done, and it is still making files there. When the caller is a "+
			"test, the directory is a t.TempDir and the write lands inside the cleanup's RemoveAll: "+
			"'TempDir RemoveAll cleanup: ... directory not empty', charged to whichever test the "+
			"cleanup was running. Give the goroutine an owner that the run waits for; do not retry "+
			"or delay the cleanup, which leaves the writer loose and only moves the damage.",
			projectDir)
	case <-time.After(writerLifetimeStillBlocked):
	}

	releasedAt := time.Now()
	if err := writeEnd.Close(); err != nil {
		t.Errorf("writerLifetime: close the write end: %v", err)
	}

	// The join is unbounded on purpose after the report. Ending the test here
	// would leave the drive goroutine running, and the first thing it logs
	// panics the whole binary with "Log in goroutine after test completed" —
	// which buries the real finding. A run that truly never returns is bounded
	// by `go test -timeout`, which dumps every goroutine and names the wedge.
	select {
	case <-driveDone:
	case <-time.After(60 * time.Second):
		t.Errorf("the run has not returned 60s after its session-data writer was released.\n" +
			"Waiting on the writer must not become waiting forever.")
		<-driveDone
	}

	// Claim 2. Only meaningful when the run really did wait: a run that already
	// returned tells us nothing about what released it.
	waited := time.Since(releasedAt)
	t.Logf("the run returned %v after its session-data writer was released", waited)
	if !returnedEarly && waited > writerLifetimeAfterRelease {
		t.Errorf("the run returned %v after its session-data writer was released, which is longer than %v.\n"+
			"It is waiting on a clock rather than on the writer. A fixed delay leaves the same race "+
			"open for every writer that takes longer than the delay.", waited, writerLifetimeAfterRelease)
	}

	// Claim 3. The positive half.
	sessionData := filepath.Join(projectDir, ".harmonik", "session-data.jsonl")
	if _, err := os.Stat(sessionData); err != nil {
		t.Errorf("no session data at %s after the run returned: %v\n"+
			"This run must reach the collection and write its record, or the wait above defends "+
			"nothing and would pass for a run that starts no writer at all.", sessionData, err)
	}
}
