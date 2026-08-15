package run

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDispatchRegistryCreateAdvanceScanRemove(t *testing.T) {
	projectDir := t.TempDir()
	base := testDispatchRecord()
	if err := CreateDispatchRecord(projectDir, base); err != nil {
		t.Fatal(err)
	}
	if err := CreateDispatchRecord(projectDir, base); err != nil {
		t.Fatalf("exact create replay: %v", err)
	}
	located, err := base.BindLocation(testRemoteLocation())
	if err != nil {
		t.Fatal(err)
	}
	if err := AdvanceDispatchRecord(projectDir, base, located); err != nil {
		t.Fatal(err)
	}
	handoff, err := located.BindSession("harmonik-run-0197d200", "run-0197d200")
	if err != nil {
		t.Fatal(err)
	}
	if err := AdvanceDispatchRecord(projectDir, located, handoff); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ScanRegistry(projectDir)
	if err != nil || len(snapshot.Dispatch) != 1 || len(snapshot.Legacy) != 0 {
		t.Fatalf("ScanRegistry() = (%+v, %v)", snapshot, err)
	}
	if err := RemoveDispatchRecord(projectDir, handoff); err != nil {
		t.Fatal(err)
	}
	if err := RemoveDispatchRecord(projectDir, handoff); err != nil {
		t.Fatalf("absent remove replay: %v", err)
	}
}

func TestScanRegistryPartitionsRealLegacyWriterBytes(t *testing.T) {
	projectDir := t.TempDir()
	legacy := Record{
		SchemaVersion: 1,
		RunID:         "0197d200-0000-7000-8000-000000000099",
		BeadID:        "hk-legacy",
		SessionName:   "harmonik-legacy",
		StartedAt:     time.Date(2026, 8, 11, 1, 2, 3, 0, time.UTC),
	}
	if err := Write(projectDir, legacy); err != nil {
		t.Fatal(err)
	}
	if err := CreateDispatchRecord(projectDir, testDispatchRecord()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ScanRegistry(projectDir)
	if err != nil || len(snapshot.Legacy) != 1 || len(snapshot.Dispatch) != 1 {
		t.Fatalf("ScanRegistry() = (%+v, %v)", snapshot, err)
	}
}

func TestScanRegistryFailsClosedOnEveryRecordShapedEntry(t *testing.T) {
	tests := []struct {
		name string
		make func(t *testing.T, dir string)
	}{
		{name: "unknown schema", make: func(t *testing.T, dir string) {
			writeRunTestFile(t, dir, dispatchTestRunID+".json", []byte(`{"schema_version":99}`))
		}},
		{name: "wrong identity", make: func(t *testing.T, dir string) {
			data, err := json.Marshal(testDispatchRecord())
			if err != nil {
				t.Fatal(err)
			}
			writeRunTestFile(t, dir, "0197d200-0000-7000-8000-000000000099.json", data)
		}},
		{name: "directory", make: func(t *testing.T, dir string) {
			if err := os.Mkdir(filepath.Join(dir, dispatchTestRunID+".json"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			dir := filepath.Join(projectDir, ".harmonik", "runs")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			tc.make(t, dir)
			if _, err := ScanRegistry(projectDir); err == nil {
				t.Fatal("ScanRegistry() = nil error")
			}
		})
	}
}

func TestDispatchRegistryRejectsLegacySameRunConflict(t *testing.T) {
	projectDir := t.TempDir()
	base := testDispatchRecord()
	legacy := Record{
		SchemaVersion: 1,
		RunID:         base.RunID.String(),
		BeadID:        string(base.BeadID),
		SessionName:   "legacy-session",
		StartedAt:     base.StartedAt,
	}
	if err := Write(projectDir, legacy); err != nil {
		t.Fatal(err)
	}
	var conflict *DispatchConflictError
	if err := CreateDispatchRecord(projectDir, base); !errors.As(err, &conflict) {
		t.Fatalf("CreateDispatchRecord() = %v", err)
	}
	record, err := Load(projectDir, legacy.RunID)
	if err != nil || record.SessionName != legacy.SessionName {
		t.Fatalf("legacy changed: (%+v, %v)", record, err)
	}
}

func TestScanRegistryRejectsSymlinkParent(t *testing.T) {
	projectDir := t.TempDir()
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(projectDir, ".harmonik")); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanRegistry(projectDir); err == nil {
		t.Fatal("ScanRegistry() with symlink parent = nil")
	}
}

func TestDispatchRegistryRejectsInvalidAdvanceMatrix(t *testing.T) {
	base := testDispatchRecord()
	located, err := base.BindLocation(testLocalLocation())
	if err != nil {
		t.Fatal(err)
	}
	handoff, err := located.BindSession("session", "window")
	if err != nil {
		t.Fatal(err)
	}
	otherLocation, err := base.BindLocation(ExecutionLocation{Kind: ExecutionLocalShared, RepositoryPath: base.RepositoryPath})
	if err != nil {
		t.Fatal(err)
	}
	changedBase := located
	changedBase.BeadID = "hk-other"
	for _, pair := range [][2]DispatchRecord{
		{base, handoff},
		{located, otherLocation},
		{handoff, handoff},
		{located, changedBase},
	} {
		if err := validDispatchAdvance(pair[0], pair[1]); err == nil {
			t.Fatalf("validDispatchAdvance(%+v, %+v) = nil", pair[0], pair[1])
		}
	}
}

func TestDispatchRegistryConvergesSideEffectThenError(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "runs"), 0o700); err != nil {
		t.Fatal(err)
	}
	ops := osDispatchRegistryOps()
	realLink, realRename, realRemove := ops.link, ops.rename, ops.remove
	ops.link = func(oldPath, newPath string) error {
		if err := realLink(oldPath, newPath); err != nil {
			return err
		}
		return errors.New("link failed after install")
	}
	base := testDispatchRecord()
	if err := createDispatchRecord(projectDir, base, ops); err != nil {
		t.Fatalf("create did not converge: %v", err)
	}
	ops.link = realLink
	located, err := base.BindLocation(testLocalLocation())
	if err != nil {
		t.Fatal(err)
	}
	ops.rename = func(oldPath, newPath string) error {
		if err := realRename(oldPath, newPath); err != nil {
			return err
		}
		return errors.New("rename failed after replace")
	}
	if err := advanceDispatchRecord(projectDir, base, located, ops); err != nil {
		t.Fatalf("advance did not converge: %v", err)
	}
	ops.rename = realRename
	handoff, err := located.BindSession("session", "window")
	if err != nil {
		t.Fatal(err)
	}
	if err := advanceDispatchRecord(projectDir, located, handoff, ops); err != nil {
		t.Fatal(err)
	}
	exactPath := recordPath(projectDir, base.RunID.String())
	ops.remove = func(path string) error {
		if path != exactPath {
			return realRemove(path)
		}
		if err := realRemove(path); err != nil {
			return err
		}
		return errors.New("remove failed after unlink")
	}
	if err := removeDispatchRecord(projectDir, handoff, ops); err != nil {
		t.Fatalf("remove did not converge: %v", err)
	}
}

func TestDispatchRegistryLinkFailureWithoutEffectIsNotConflict(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik", "runs"), 0o700); err != nil {
		t.Fatal(err)
	}
	ops := osDispatchRegistryOps()
	ops.link = func(string, string) error { return errors.New("definite link failure") }
	err := createDispatchRecord(projectDir, testDispatchRecord(), ops)
	var conflict *DispatchConflictError
	var ambiguous *DispatchAmbiguousError
	if err == nil || errors.As(err, &conflict) || errors.As(err, &ambiguous) {
		t.Fatalf("create error = %v", err)
	}
}

func TestDispatchRegistryConvergenceSyncFailuresStayTyped(t *testing.T) {
	projectDir := t.TempDir()
	dir := filepath.Join(projectDir, ".harmonik", "runs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	base := testDispatchRecord()
	if err := CreateDispatchRecord(projectDir, base); err != nil {
		t.Fatal(err)
	}
	ops := osDispatchRegistryOps()
	realSync := ops.syncDir
	ops.syncDir = func(path string) error {
		if path == dir {
			return errors.New("cut root sync")
		}
		return realSync(path)
	}
	assertAmbiguous := func(name string, err error) {
		t.Helper()
		var ambiguous *DispatchAmbiguousError
		if !errors.As(err, &ambiguous) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
	assertAmbiguous("exact create", createDispatchRecord(projectDir, base, ops))
	located, err := base.BindLocation(testLocalLocation())
	if err != nil {
		t.Fatal(err)
	}
	ops.syncDir = realSync
	if err := AdvanceDispatchRecord(projectDir, base, located); err != nil {
		t.Fatal(err)
	}
	ops.syncDir = func(path string) error {
		if path == dir {
			return errors.New("cut root sync")
		}
		return realSync(path)
	}
	assertAmbiguous("already next", advanceDispatchRecord(projectDir, base, located, ops))
	ops.syncDir = realSync
	handoff, err := located.BindSession("session", "window")
	if err != nil {
		t.Fatal(err)
	}
	if err := AdvanceDispatchRecord(projectDir, located, handoff); err != nil {
		t.Fatal(err)
	}
	if err := RemoveDispatchRecord(projectDir, handoff); err != nil {
		t.Fatal(err)
	}
	ops.syncDir = func(path string) error {
		if path == dir {
			return errors.New("cut root sync")
		}
		return realSync(path)
	}
	assertAmbiguous("absent remove", removeDispatchRecord(projectDir, handoff, ops))
}

func TestLegacyWriterCannotReplaceUniversalRecord(t *testing.T) {
	projectDir := t.TempDir()
	base := testDispatchRecord()
	if err := CreateDispatchRecord(projectDir, base); err != nil {
		t.Fatal(err)
	}
	legacy := Record{SchemaVersion: 1, RunID: base.RunID.String(), BeadID: "hk-old", SessionName: "old", StartedAt: time.Now()}
	var conflict *DispatchConflictError
	if err := Write(projectDir, legacy); !errors.As(err, &conflict) {
		t.Fatalf("Write() = %v", err)
	}
	snapshot, err := ScanRegistry(projectDir)
	if err != nil || len(snapshot.Dispatch) != 1 {
		t.Fatalf("universal record changed: (%+v, %v)", snapshot, err)
	}
}

func TestLegacyWriterCannotReplaceUnclassifiedRecordBytes(t *testing.T) {
	runID := "0197d200-0000-7000-8000-000000000099"
	canonicalLegacy, err := json.MarshalIndent(Record{
		SchemaVersion: 1,
		RunID:         runID,
		BeadID:        "hk-existing",
		SessionName:   "existing",
		StartedAt:     time.Date(2026, 8, 11, 1, 2, 3, 0, time.UTC),
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, canonicalLegacy); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"corrupt":        []byte("{"),
		"unknown":        []byte(`{"schema_version":99}`),
		"unknown field":  bytes.Replace(canonicalLegacy, []byte(`"schema_version": 1`), []byte(`"schema_version": 1, "extra": true`), 1),
		"noncanonical":   compact.Bytes(),
		"wrong identity": bytes.Replace(canonicalLegacy, []byte(runID), []byte(dispatchTestRunID), 1),
	} {
		t.Run(name, func(t *testing.T) {
			projectDir := t.TempDir()
			dir := filepath.Join(projectDir, ".harmonik", "runs")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, runID+".json")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			legacy := Record{SchemaVersion: 1, RunID: runID, BeadID: "hk-old", SessionName: "old", StartedAt: time.Now()}
			var conflict *DispatchConflictError
			if err := Write(projectDir, legacy); !errors.As(err, &conflict) {
				t.Fatalf("Write() = %v", err)
			}
			got, err := os.ReadFile(path) //nolint:gosec // fixed test path under a temporary directory.
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("record changed: (%q, %v)", got, err)
			}
		})
	}
}

func TestScanRegistryRejectsNoncanonicalLegacyBytes(t *testing.T) {
	projectDir := t.TempDir()
	legacy := Record{
		SchemaVersion: 1,
		RunID:         "0197d200-0000-7000-8000-000000000099",
		BeadID:        "hk-legacy",
		SessionName:   "legacy",
		StartedAt:     time.Date(2026, 8, 11, 1, 2, 3, 0, time.UTC),
	}
	if err := Write(projectDir, legacy); err != nil {
		t.Fatal(err)
	}
	path := recordPath(projectDir, legacy.RunID)
	data, err := os.ReadFile(path) //nolint:gosec // test path is built from a fixed UUIDv7 fixture.
	if err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, compact.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ScanRegistry(projectDir); err == nil {
		t.Fatal("ScanRegistry() accepted noncanonical legacy bytes")
	}
}

func writeRunTestFile(t *testing.T, dir, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
