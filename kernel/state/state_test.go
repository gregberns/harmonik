package state

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

func withDeadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func openTemp(t *testing.T) (st *State, path string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "journal.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return st, path
}

func readAll(t *testing.T, st *State, namespace string, req *kernelv1.JournalReadRequest) []*kernelv1.JournalRecord {
	t.Helper()
	var got []*kernelv1.JournalRecord
	if err := st.Read(withDeadline(t), namespace, req, func(rec *kernelv1.JournalRecord) error {
		got = append(got, rec)
		return nil
	}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	return got
}

func TestAppendAssignsMonotonicSeqsAndReadReturnsThemInOrder(t *testing.T) {
	st, _ := openTemp(t)

	resp, err := st.Append(withDeadline(t), "echo", &kernelv1.JournalAppendRequest{
		Journal: "seen",
		Records: [][]byte{[]byte("a"), []byte("b"), []byte("c")},
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if got, want := resp.GetSeqs(), []uint64{1, 2, 3}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("Seqs = %v, want %v", got, want)
	}

	records := readAll(t, st, "echo", &kernelv1.JournalReadRequest{Journal: "seen"})
	if len(records) != 3 {
		t.Fatalf("Read returned %d records, want 3", len(records))
	}
	for i, want := range []string{"a", "b", "c"} {
		if string(records[i].GetRecord()) != want {
			t.Fatalf("record %d = %q, want %q", i, records[i].GetRecord(), want)
		}
		//nolint:gosec // i ranges over a 3-element literal slice; the uint64(i) conversion cannot overflow
		if wantSeq := uint64(i) + 1; records[i].GetSeq() != wantSeq {
			t.Fatalf("record %d seq = %d, want %d", i, records[i].GetSeq(), wantSeq)
		}
		if records[i].GetAppendedAt() == nil {
			t.Fatalf("record %d has no appended_at", i)
		}
	}
}

func TestSeqIsMonotonicAcrossARestart(t *testing.T) {
	st, path := openTemp(t)

	if _, err := st.Append(withDeadline(t), "echo", &kernelv1.JournalAppendRequest{
		Journal: "seen", Records: [][]byte{[]byte("first")}, Sync: true,
	}); err != nil {
		t.Fatalf("first Append: %v", err)
	}
	if err := st.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen Open: %v", err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	resp, err := reopened.Append(withDeadline(t), "echo", &kernelv1.JournalAppendRequest{
		Journal: "seen", Records: [][]byte{[]byte("second")},
	})
	if err != nil {
		t.Fatalf("second Append: %v", err)
	}
	if len(resp.GetSeqs()) != 1 || resp.GetSeqs()[0] != 2 {
		t.Fatalf("second Append seq = %v, want [2] — seq reset across restart", resp.GetSeqs())
	}
}

func TestReadAfterSeqExcludesAlreadySeenRecords(t *testing.T) {
	st, _ := openTemp(t)

	if _, err := st.Append(withDeadline(t), "echo", &kernelv1.JournalAppendRequest{
		Journal: "seen", Records: [][]byte{[]byte("a"), []byte("b"), []byte("c")},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	records := readAll(t, st, "echo", &kernelv1.JournalReadRequest{Journal: "seen", AfterSeq: 1})
	if len(records) != 2 {
		t.Fatalf("Read after_seq=1 returned %d records, want 2", len(records))
	}
	if string(records[0].GetRecord()) != "b" || string(records[1].GetRecord()) != "c" {
		t.Fatalf("Read after_seq=1 = %q, %q, want b, c", records[0].GetRecord(), records[1].GetRecord())
	}
}

func TestNamespacesDoNotCollideOnTheSameJournalName(t *testing.T) {
	st, _ := openTemp(t)

	if _, err := st.Append(withDeadline(t), "plugin-a", &kernelv1.JournalAppendRequest{
		Journal: "seen", Records: [][]byte{[]byte("a-record")},
	}); err != nil {
		t.Fatalf("Append plugin-a: %v", err)
	}

	records := readAll(t, st, "plugin-b", &kernelv1.JournalReadRequest{Journal: "seen"})
	if len(records) != 0 {
		t.Fatalf("plugin-b saw %d records in a journal it never wrote to, want 0", len(records))
	}
}

func TestFollowDeliversAnAppendMadeAfterReadStarted(t *testing.T) {
	st, _ := openTemp(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	received := make(chan *kernelv1.JournalRecord, 1)
	readErr := make(chan error, 1)
	go func() {
		readErr <- st.Read(ctx, "echo", &kernelv1.JournalReadRequest{Journal: "seen", Follow: true}, func(rec *kernelv1.JournalRecord) error {
			received <- rec
			return nil
		})
	}()

	// Give Read a moment to register its watch before the append lands.
	time.Sleep(50 * time.Millisecond)

	if _, err := st.Append(context.Background(), "echo", &kernelv1.JournalAppendRequest{
		Journal: "seen", Records: [][]byte{[]byte("late")},
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	select {
	case rec := <-received:
		if string(rec.GetRecord()) != "late" {
			t.Fatalf("followed record = %q, want %q", rec.GetRecord(), "late")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("follow never delivered the post-read append")
	}

	cancel()
	if err := <-readErr; !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Read returned %v after cancel, want context.Canceled", err)
	}
}

// TestSyncAppendSurvivesKillNine is the K3 durability probe: a child process
// appends one record with sync=true, announces it, and is then killed with
// SIGKILL — a real, uncooperative process death, not a clean shutdown. The
// record must still be there when the parent reopens the same database file.
func TestSyncAppendSurvivesKillNine(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "journal.db")

	//nolint:gosec // os.Args[0] is this test binary re-executing itself, the standard Go helper-process pattern
	cmd := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestDurabilityHelperProcess$")
	cmd.Env = append(os.Environ(),
		durabilityHelperEnv+"=1",
		durabilityHelperDBEnv+"="+dbPath,
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start helper: %v", err)
	}

	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("read helper announcement: %v", err)
	}
	if line != durabilityAppendedLine+"\n" {
		t.Fatalf("helper announcement = %q, want %q", line, durabilityAppendedLine)
	}

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill -9 helper: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Logf("helper process exited via kill -9, as expected: %v", err)
	}

	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("reopen after kill -9: %v", err)
	}
	defer func() {
		if err := st.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	records := readAll(t, st, durabilityNamespace, &kernelv1.JournalReadRequest{Journal: durabilityJournal})
	if len(records) != 1 {
		t.Fatalf("records after kill -9 = %d, want 1 (sync=true append did not survive)", len(records))
	}
	if string(records[0].GetRecord()) != durabilityPayload {
		t.Fatalf("record after kill -9 = %q, want %q", records[0].GetRecord(), durabilityPayload)
	}
}

const (
	durabilityHelperEnv    = "HARMONIK_KERNEL_STATE_DURABILITY_HELPER"
	durabilityHelperDBEnv  = "HARMONIK_KERNEL_STATE_DURABILITY_DB"
	durabilityNamespace    = "durability-ns"
	durabilityJournal      = "durability-journal"
	durabilityPayload      = "durable-payload"
	durabilityAppendedLine = "APPENDED"
)

// TestDurabilityHelperProcess is not a real test: it only runs when
// durabilityHelperEnv is set, as the re-exec'd child of
// TestSyncAppendSurvivesKillNine. It appends one record with sync=true,
// prints an announcement, then blocks so the parent can SIGKILL it.
func TestDurabilityHelperProcess(t *testing.T) {
	dbPath := os.Getenv(durabilityHelperDBEnv)
	if os.Getenv(durabilityHelperEnv) == "" || dbPath == "" {
		t.Skip("not invoked as the durability helper process")
	}

	st, err := Open(dbPath)
	if err != nil {
		t.Fatalf("helper Open: %v", err)
	}

	if _, err := st.Append(context.Background(), durabilityNamespace, &kernelv1.JournalAppendRequest{
		Journal: durabilityJournal,
		Records: [][]byte{[]byte(durabilityPayload)},
		Sync:    true,
	}); err != nil {
		t.Fatalf("helper Append: %v", err)
	}

	if _, err := os.Stdout.WriteString(durabilityAppendedLine + "\n"); err != nil {
		t.Fatalf("helper announce: %v", err)
	}

	// Block until the parent sends SIGKILL. Never returns on its own.
	select {}
}
