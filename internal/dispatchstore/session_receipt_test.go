package dispatchstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
)

func TestStoreInstallsLoadsAndListsSessionReceipt(t *testing.T) {
	store := New(t.TempDir())
	receipt := testSessionReceipt(t)
	if err := store.InstallSessionStartReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadSessionStartReceipt(receipt.Binding.RunID)
	if err != nil || !reflect.DeepEqual(loaded, receipt) {
		t.Fatalf("LoadSessionStartReceipt() = (%+v, %v)", loaded, err)
	}
	listed, err := store.ListSessionStartReceipts()
	if err != nil || len(listed) != 1 || !reflect.DeepEqual(listed[0], receipt) {
		t.Fatalf("ListSessionStartReceipts() = (%+v, %v)", listed, err)
	}
	if err := store.InstallSessionStartReceipt(receipt); err != nil {
		t.Fatalf("exact retry = %v", err)
	}
}

func TestStoreRejectsConflictingSessionReceipt(t *testing.T) {
	store := New(t.TempDir())
	receipt := testSessionReceipt(t)
	if err := store.InstallSessionStartReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	conflict := receipt
	conflict.WindowName = "run-other"
	if err := store.InstallSessionStartReceipt(conflict); err == nil {
		t.Fatal("conflicting receipt install = nil error")
	}
	loaded, err := store.LoadSessionStartReceipt(receipt.Binding.RunID)
	if err != nil || !reflect.DeepEqual(loaded, receipt) {
		t.Fatalf("receipt changed = (%+v, %v)", loaded, err)
	}
}

func TestStoreSessionReceiptInstallSyncAmbiguityKeepsExactFact(t *testing.T) {
	projectDir := t.TempDir()
	store := New(projectDir)
	receipt := testSessionReceipt(t)
	realSync := store.ops.syncDir
	root := store.receiptRoot()
	store.ops.syncDir = func(path string) error {
		if path == root {
			return errors.New("cut receipt root sync")
		}
		return realSync(path)
	}
	var ambiguous *AmbiguousError
	if err := store.InstallSessionStartReceipt(receipt); !errors.As(err, &ambiguous) {
		t.Fatalf("InstallSessionStartReceipt() error = %v", err)
	}
	store.ops.syncDir = realSync
	loaded, err := store.LoadSessionStartReceipt(receipt.Binding.RunID)
	if err != nil || !reflect.DeepEqual(loaded, receipt) {
		t.Fatalf("receipt reload = (%+v, %v)", loaded, err)
	}
}

func TestStoreSessionReceiptLinkSideEffectConverges(t *testing.T) {
	store := New(t.TempDir())
	receipt := testSessionReceipt(t)
	realLink := store.ops.link
	store.ops.link = func(oldPath, newPath string) error {
		if err := realLink(oldPath, newPath); err != nil {
			return err
		}
		return errors.New("link returned after effect")
	}
	if err := store.InstallSessionStartReceipt(receipt); err != nil {
		t.Fatalf("InstallSessionStartReceipt() = %v", err)
	}
}

func TestStoreSessionReceiptListFailsClosedOnEveryEntry(t *testing.T) {
	tests := []struct {
		name  string
		write func(t *testing.T, root string, receipt dispatch.SessionStartReceipt)
	}{
		{name: "corrupt", write: func(t *testing.T, root string, receipt dispatch.SessionStartReceipt) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(root, receiptBasename(receipt.Binding.RunID)), []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "directory", write: func(t *testing.T, root string, receipt dispatch.SessionStartReceipt) {
			t.Helper()
			if err := os.Mkdir(filepath.Join(root, receiptBasename(receipt.Binding.RunID)), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "temporary", write: func(t *testing.T, root string, receipt dispatch.SessionStartReceipt) {
			t.Helper()
			name := receiptTempPrefix + receipt.Binding.RunID.String() + "-0197d100-0000-7000-8000-000000000099"
			if err := os.WriteFile(filepath.Join(root, name), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unsupported", write: func(t *testing.T, root string, _ dispatch.SessionStartReceipt) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(root, "note"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			root := filepath.Join(projectDir, ".harmonik", sessionReceiptRoot)
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			receipt := testSessionReceipt(t)
			tc.write(t, root, receipt)
			if _, err := New(projectDir).ListSessionStartReceipts(); err == nil {
				t.Fatal("ListSessionStartReceipts() = nil error")
			}
		})
	}
}

func TestStoreSessionReceiptRejectsSymlinkRoots(t *testing.T) {
	for _, parent := range []bool{false, true} {
		t.Run(map[bool]string{false: "receipt root", true: "harmonik root"}[parent], func(t *testing.T) {
			projectDir, external := t.TempDir(), t.TempDir()
			if parent {
				if err := os.Symlink(external, filepath.Join(projectDir, ".harmonik")); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(filepath.Join(projectDir, ".harmonik"), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, filepath.Join(projectDir, ".harmonik", sessionReceiptRoot)); err != nil {
					t.Fatal(err)
				}
			}
			if err := New(projectDir).InstallSessionStartReceipt(testSessionReceipt(t)); err == nil {
				t.Fatal("InstallSessionStartReceipt() = nil error")
			}
			entries, err := os.ReadDir(external)
			if err != nil || len(entries) != 0 {
				t.Fatalf("external target changed = (%v, %v)", entries, err)
			}
		})
	}
}

func TestStoreSessionReceiptTemporaryNameUsesCanonicalUUIDv7(t *testing.T) {
	store := New(t.TempDir())
	receipt := testSessionReceipt(t)
	tempID := uuid.MustParse("0197d100-0000-7000-8000-000000000099")
	store.ops.newUUID = func() (uuid.UUID, error) { return tempID, nil }
	var opened string
	realOpen := store.ops.openFile
	store.ops.openFile = func(path string, flag int, mode os.FileMode) (*os.File, error) {
		opened = filepath.Base(path)
		return realOpen(path, flag, mode)
	}
	if err := store.InstallSessionStartReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	want := receiptTempPrefix + receipt.Binding.RunID.String() + "-" + tempID.String()
	if opened != want {
		t.Fatalf("temporary basename = %q, want %q", opened, want)
	}
}

func TestStoreRemovesExactSessionReceiptAndConverges(t *testing.T) {
	store := New(t.TempDir())
	receipt := testSessionReceipt(t)
	if err := store.InstallSessionStartReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveSessionStartReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveSessionStartReceipt(receipt); err != nil {
		t.Fatalf("absent retry = %v", err)
	}
	if _, err := store.LoadSessionStartReceipt(receipt.Binding.RunID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadSessionStartReceipt() error = %v", err)
	}
}

func TestStoreSessionReceiptRemoveRejectsChangedBytes(t *testing.T) {
	store := New(t.TempDir())
	receipt := testSessionReceipt(t)
	if err := store.InstallSessionStartReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	want := receipt
	want.WindowName = "run-other"
	if err := store.RemoveSessionStartReceipt(want); err == nil {
		t.Fatal("RemoveSessionStartReceipt() = nil error")
	}
	loaded, err := store.LoadSessionStartReceipt(receipt.Binding.RunID)
	if err != nil || !reflect.DeepEqual(loaded, receipt) {
		t.Fatalf("receipt changed = (%+v, %v)", loaded, err)
	}
}

func TestStoreSessionReceiptRemoveSideEffectConverges(t *testing.T) {
	store := New(t.TempDir())
	receipt := testSessionReceipt(t)
	if err := store.InstallSessionStartReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	realRemove := store.ops.remove
	store.ops.remove = func(path string) error {
		if err := realRemove(path); err != nil {
			return err
		}
		return errors.New("remove returned after effect")
	}
	if err := store.RemoveSessionStartReceipt(receipt); err != nil {
		t.Fatalf("RemoveSessionStartReceipt() = %v", err)
	}
}

func TestStoreSessionReceiptRemoveFailurePreservesExactFact(t *testing.T) {
	store := New(t.TempDir())
	receipt := testSessionReceipt(t)
	if err := store.InstallSessionStartReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	store.ops.remove = func(string) error { return errors.New("cut remove") }
	if err := store.RemoveSessionStartReceipt(receipt); err == nil {
		t.Fatal("RemoveSessionStartReceipt() = nil error")
	}
	loaded, err := store.LoadSessionStartReceipt(receipt.Binding.RunID)
	if err != nil || !reflect.DeepEqual(loaded, receipt) {
		t.Fatalf("receipt changed = (%+v, %v)", loaded, err)
	}
}

func TestStoreSessionReceiptRemoveSyncAmbiguityPreservesAbsence(t *testing.T) {
	store := New(t.TempDir())
	receipt := testSessionReceipt(t)
	if err := store.InstallSessionStartReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	realSync := store.ops.syncDir
	root := store.receiptRoot()
	store.ops.syncDir = func(path string) error {
		if path == root {
			return errors.New("cut receipt remove sync")
		}
		return realSync(path)
	}
	var ambiguous *AmbiguousError
	if err := store.RemoveSessionStartReceipt(receipt); !errors.As(err, &ambiguous) {
		t.Fatalf("RemoveSessionStartReceipt() error = %v", err)
	}
	store.ops.syncDir = realSync
	if err := store.RemoveSessionStartReceipt(receipt); err != nil {
		t.Fatalf("converged remove retry = %v", err)
	}
}

func TestStoreSessionReceiptAbsentRemoveRequiresRootSync(t *testing.T) {
	store := New(t.TempDir())
	receipt := testSessionReceipt(t)
	if err := store.InstallSessionStartReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveSessionStartReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	realSync := store.ops.syncDir
	root := store.receiptRoot()
	store.ops.syncDir = func(path string) error {
		if path == root {
			return errors.New("cut absent receipt sync")
		}
		return realSync(path)
	}
	var ambiguous *AmbiguousError
	if err := store.RemoveSessionStartReceipt(receipt); !errors.As(err, &ambiguous) {
		t.Fatalf("absent RemoveSessionStartReceipt() error = %v", err)
	}
}

func TestStoreSessionReceiptRemoveRejectsUnsupportedEntryTypes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(t *testing.T, path string) string
	}{
		{name: "directory", write: func(t *testing.T, path string) string {
			t.Helper()
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			return ""
		}},
		{name: "symlink", write: func(t *testing.T, path string) string {
			t.Helper()
			external := filepath.Join(t.TempDir(), "receipt")
			if err := os.WriteFile(external, []byte("external"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(external, path); err != nil {
				t.Fatal(err)
			}
			return external
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := New(t.TempDir())
			receipt := testSessionReceipt(t)
			root := store.receiptRoot()
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, receiptBasename(receipt.Binding.RunID))
			external := tc.write(t, path)
			if err := store.RemoveSessionStartReceipt(receipt); err == nil {
				t.Fatal("RemoveSessionStartReceipt() = nil error")
			}
			info, err := os.Lstat(path)
			if err != nil {
				t.Fatalf("entry was removed: %v", err)
			}
			if tc.name == "directory" && !info.IsDir() {
				t.Fatalf("entry mode = %v, want directory", info.Mode())
			}
			if external != "" {
				got, readErr := os.ReadFile(external) //nolint:gosec // test-owned path below t.TempDir.
				if readErr != nil || string(got) != "external" {
					t.Fatalf("external target changed = (%q, %v)", got, readErr)
				}
			}
		})
	}
}

func TestStoreSessionReceiptListRemovesOnlyConvergedTemporaryEntry(t *testing.T) {
	projectDir := t.TempDir()
	store := New(projectDir)
	receipt := testSessionReceipt(t)
	if err := store.InstallSessionStartReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	root := store.receiptRoot()
	canonicalPath := filepath.Join(root, receiptBasename(receipt.Binding.RunID))
	data, err := os.ReadFile(canonicalPath) //nolint:gosec // test path is below t.TempDir with fixed validated names.
	if err != nil {
		t.Fatal(err)
	}
	tempName := receiptTempPrefix + receipt.Binding.RunID.String() + "-0197d100-0000-7000-8000-000000000099"
	tempPath := filepath.Join(root, tempName)
	if err := os.WriteFile(tempPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListSessionStartReceipts()
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListSessionStartReceipts() = (%+v, %v)", listed, err)
	}
	if _, err := os.Lstat(tempPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary entry remains: %v", err)
	}
	got, err := os.ReadFile(canonicalPath) //nolint:gosec // test path is below t.TempDir with fixed validated names.
	if err != nil || !reflect.DeepEqual(got, data) {
		t.Fatalf("canonical bytes changed = (%q, %v)", got, err)
	}
}

func TestStoreSessionReceiptListPreservesConflictingTemporaryEntry(t *testing.T) {
	projectDir := t.TempDir()
	store := New(projectDir)
	receipt := testSessionReceipt(t)
	if err := store.InstallSessionStartReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	root := store.receiptRoot()
	tempName := receiptTempPrefix + receipt.Binding.RunID.String() + "-0197d100-0000-7000-8000-000000000099"
	tempPath := filepath.Join(root, tempName)
	if err := os.WriteFile(tempPath, []byte("different"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListSessionStartReceipts(); err == nil {
		t.Fatal("ListSessionStartReceipts() = nil error")
	}
	got, err := os.ReadFile(tempPath) //nolint:gosec // test path is below t.TempDir with fixed validated names.
	if err != nil || string(got) != "different" {
		t.Fatalf("temporary evidence changed = (%q, %v)", got, err)
	}
}

func TestStoreSessionReceiptListPreservesIdenticalInvalidFacts(t *testing.T) {
	base := testSessionReceipt(t)
	canonical, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	wrongRun := base
	wrongRun.Binding.RunID = core.RunID(uuid.MustParse("0197d100-0000-7000-8000-000000000098"))
	wrongRunBytes, err := json.Marshal(wrongRun)
	if err != nil {
		t.Fatal(err)
	}
	for name, invalid := range map[string][]byte{
		"corrupt":      []byte(`{"schema_version":1}`),
		"noncanonical": append([]byte(" "), canonical...),
		"wrong run":    wrongRunBytes,
	} {
		t.Run(name, func(t *testing.T) {
			projectDir := t.TempDir()
			store := New(projectDir)
			root := store.receiptRoot()
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			tempName := receiptTempPrefix + base.Binding.RunID.String() + "-0197d100-0000-7000-8000-000000000099"
			tempPath := filepath.Join(root, tempName)
			canonicalPath := filepath.Join(root, receiptBasename(base.Binding.RunID))
			for _, path := range []string{tempPath, canonicalPath} {
				if err := os.WriteFile(path, invalid, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := store.ListSessionStartReceipts(); err == nil {
				t.Fatal("ListSessionStartReceipts() = nil error")
			}
			for _, path := range []string{tempPath, canonicalPath} {
				got, readErr := os.ReadFile(path) //nolint:gosec // test path is below t.TempDir with fixed validated names.
				if readErr != nil || !reflect.DeepEqual(got, invalid) {
					t.Fatalf("evidence %q changed = (%q, %v)", path, got, readErr)
				}
			}
		})
	}
}

func testSessionReceipt(t *testing.T) dispatch.SessionStartReceipt {
	t.Helper()
	intent := testIntent(dispatch.PhaseHandoffDurable)
	receipt, err := dispatch.NewSessionStartReceipt(intent)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	var detached dispatch.SessionStartReceipt
	if err := json.Unmarshal(data, &detached); err != nil {
		t.Fatal(err)
	}
	return detached
}
