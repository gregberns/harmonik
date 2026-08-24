package keeper_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper"
)

func TestRuntimeRecordRoundTripAndOwnedRemoval(t *testing.T) {
	project := t.TempDir()
	record := keeper.RuntimeRecord{
		PID: 42, Executable: "/tmp/harmonik", ExecutableSHA256: "abc",
		Commit: "deadbeef", TmuxTarget: "%7", ConfigSHA256: "def",
		StartedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
	if err := keeper.WriteRuntimeRecord(project, "alpha", record); err != nil {
		t.Fatal(err)
	}
	got, err := keeper.ReadRuntimeRecord(project, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got != record {
		t.Fatalf("runtime record = %#v; want %#v", got, record)
	}
	if err := keeper.RemoveRuntimeRecord(project, "alpha", 41); err != nil {
		t.Fatal(err)
	}
	if _, err := keeper.ReadRuntimeRecord(project, "alpha"); err != nil {
		t.Fatalf("new owner record was removed: %v", err)
	}
	if err := keeper.RemoveRuntimeRecord(project, "alpha", 42); err != nil {
		t.Fatal(err)
	}
	if _, err := keeper.ReadRuntimeRecord(project, "alpha"); !os.IsNotExist(err) {
		t.Fatalf("owned record remains: %v", err)
	}
}

func TestFileSHA256(t *testing.T) {
	path := filepath.Join(t.TempDir(), "binary")
	if err := os.WriteFile(path, []byte("harmonik\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := keeper.FileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	const want = "61d25bc214268e8ff99acad709d0937fd5ef0e832ffef1d92fe7bccc1eb1b5b5"
	if got != want {
		t.Fatalf("digest = %q; want %q", got, want)
	}
}
