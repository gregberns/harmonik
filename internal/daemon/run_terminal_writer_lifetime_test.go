//go:build !windows

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

const writerLifetimeStillBlocked = 3 * time.Second

const writerLifetimeAfterRelease = time.Second

const writerLifetimePostReturnGrace = 200 * time.Millisecond

func writerLifetimeWaitForReader(t *testing.T, path string, timeout time.Duration, driveDone <-chan struct{}) (*os.File, bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	runReturned := false
	var runReturnedAt time.Time
	for time.Now().Before(deadline) {
		fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		if err == nil {
			return os.NewFile(uintptr(fd), path), true
		}
		if !errors.Is(err, syscall.ENXIO) {
			t.Errorf("writerLifetime: open the write end of %s: %v", path, err)
			return nil, false
		}
		if runReturned && time.Now().After(runReturnedAt.Add(writerLifetimePostReturnGrace)) {
			t.Errorf("writerLifetime: nothing opened %s for reading, and the run has now "+
				"returned.\n"+
				"This is not a timeout and not a slow machine. Two things produce it and the "+
				"test cannot tell them apart from here: the run started no session-data "+
				"collection at all, or it started one that never reached its first read. "+
				"Either way there is no writer for this test to say anything about, and its "+
				"three claims would all be defended by an absence.", path)
			return nil, false
		}
		select {
		case <-driveDone:
			if !runReturned {
				runReturned = true
				runReturnedAt = time.Now()
			}
		case <-time.After(5 * time.Millisecond):
		}
	}
	t.Errorf("writerLifetime: nothing opened %s for reading within %s, and the run had not "+
		"returned either.\n"+
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

	writeEnd, gotReader := writerLifetimeWaitForReader(t, fifoPath, 60*time.Second, driveDone)
	if !gotReader {
		<-driveDone
		return
	}

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

	select {
	case <-driveDone:
	case <-time.After(60 * time.Second):
		t.Errorf("the run has not returned 60s after its session-data writer was released.\n" +
			"Waiting on the writer must not become waiting forever.")
		<-driveDone
	}

	waited := time.Since(releasedAt)
	t.Logf("the run returned %v after its session-data writer was released", waited)
	if !returnedEarly && waited > writerLifetimeAfterRelease {
		t.Errorf("the run returned %v after its session-data writer was released, which is longer than %v.\n"+
			"It is waiting on a clock rather than on the writer. A fixed delay leaves the same race "+
			"open for every writer that takes longer than the delay.", waited, writerLifetimeAfterRelease)
	}

	sessionData := filepath.Join(projectDir, ".harmonik", "session-data.jsonl")
	if _, err := os.Stat(sessionData); err != nil {
		t.Errorf("no session data at %s after the run returned: %v\n"+
			"This run must reach the collection and write its record, or the wait above defends "+
			"nothing and would pass for a run that starts no writer at all.", sessionData, err)
	}
}
