package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

var errMigrationInjected = errors.New("injected migration syscall fault")

func TestMigrateFromLegacy_DestinationOutcomes(t *testing.T) {
	intended := migrationIntendedQueue()
	legacy := migrationMarshalQueue(t, intended)

	queueIDOnlyConflict := intended
	queueIDOnlyConflict.Groups = append([]Group(nil), intended.Groups...)
	queueIDOnlyConflict.Groups[0].Items = append([]Item(nil), intended.Groups[0].Items...)
	queueIDOnlyConflict.Groups[0].Items[0].Context = "same QueueID, conflicting persisted field"

	differentQueue := intended
	differentQueue.QueueID = "01999999-9999-7000-8000-000000000099"

	equivalent := new(bytes.Buffer)
	if err := json.Indent(equivalent, legacy, "", "  "); err != nil {
		t.Fatalf("indent equivalent destination: %v", err)
	}

	for _, tc := range []struct {
		name          string
		destination   []byte
		wantErr       bool
		legacyRemains bool
	}{
		{name: "empty", destination: nil, wantErr: true, legacyRemains: true},
		{name: "corrupt JSON", destination: []byte(`not json`), wantErr: true, legacyRemains: true},
		{name: "wrong schema", destination: []byte(`{"schema_version":99,"queue_id":"legacy"}`), wantErr: true, legacyRemains: true},
		{name: "different QueueID", destination: migrationMarshalQueue(t, differentQueue), wantErr: true, legacyRemains: true},
		{name: "same QueueID but conflicting field", destination: migrationMarshalQueue(t, queueIDOnlyConflict), wantErr: true, legacyRemains: true},
		{name: "deeply equivalent", destination: equivalent.Bytes(), legacyRemains: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := migrationFixture(t, legacy, tc.destination, true)
			legacyPath, targetPath := migrationPaths(projectDir)
			targetBefore := migrationReadFile(t, targetPath)

			err := MigrateFromLegacy(context.Background(), projectDir)
			if tc.wantErr && err == nil {
				t.Fatal("MigrateFromLegacy error = nil, want fail-closed error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("MigrateFromLegacy error = %v", err)
			}
			if exists := migrationFileExists(t, legacyPath); exists != tc.legacyRemains {
				t.Errorf("legacy exists = %t, want %t", exists, tc.legacyRemains)
			}
			if got := migrationReadFile(t, targetPath); !bytes.Equal(got, targetBefore) {
				t.Errorf("destination changed during %s outcome", tc.name)
			}
			if got := migrationIntendedCopyCount(t, intended, legacyPath, targetPath); got == 0 {
				t.Fatal("no deeply equivalent intended queue copy remains")
			}
			if !tc.wantErr {
				if err := MigrateFromLegacy(context.Background(), projectDir); err != nil {
					t.Fatalf("idempotent retry: %v", err)
				}
			}
		})
	}
}

func TestMigrateFromLegacy_StatErrorFailsClosed(t *testing.T) {
	intended := migrationIntendedQueue()
	legacy := migrationMarshalQueue(t, intended)
	projectDir := migrationFixture(t, legacy, nil, false)
	legacyPath, targetPath := migrationPaths(projectDir)
	calls := make([]string, 0, 2)

	err := migrateFromLegacy(projectDir, migrationTestOps(&calls, "stat destination"))
	if !errors.Is(err, errMigrationInjected) {
		t.Fatalf("migrateFromLegacy error = %v, want injected stat error", err)
	}
	migrationAssertCalls(t, calls, "read legacy", "stat destination")
	if !migrationFileExists(t, legacyPath) || migrationFileExists(t, targetPath) {
		t.Fatal("non-ENOENT Stat error changed migration files")
	}
	if got := migrationIntendedCopyCount(t, intended, legacyPath, targetPath); got != 1 {
		t.Fatalf("intended queue copies = %d, want 1", got)
	}
}

func TestMigrateFromLegacy_NewDestinationSyscallCutsAndRetries(t *testing.T) {
	fullNew := []string{
		"read legacy", "stat destination", "mkdir queues", "create temp",
		"write temp", "sync temp", "close temp", "rename destination",
		"open queues dir", "sync queues dir", "close queues dir",
		"remove legacy", "open parent dir", "sync parent dir", "close parent dir",
	}
	cases := []struct {
		faultAt string
		calls   []string
	}{
		{faultAt: "read legacy", calls: fullNew[:1]},
		{faultAt: "stat destination", calls: fullNew[:2]},
		{faultAt: "mkdir queues", calls: fullNew[:3]},
		{faultAt: "create temp", calls: fullNew[:4]},
		{faultAt: "write temp", calls: append(append([]string{}, fullNew[:5]...), "close temp", "remove temp")},
		{faultAt: "sync temp", calls: append(append([]string{}, fullNew[:6]...), "close temp", "remove temp")},
		{faultAt: "close temp", calls: append(append([]string{}, fullNew[:7]...), "remove temp")},
		{faultAt: "rename destination", calls: append(append([]string{}, fullNew[:8]...), "remove temp")},
		{faultAt: "open queues dir", calls: fullNew[:9]},
		{faultAt: "sync queues dir", calls: append(append([]string{}, fullNew[:10]...), "close queues dir")},
		{faultAt: "close queues dir", calls: fullNew[:11]},
		{faultAt: "remove legacy", calls: fullNew[:12]},
		{faultAt: "open parent dir", calls: fullNew[:13]},
		{faultAt: "sync parent dir", calls: append(append([]string{}, fullNew[:14]...), "close parent dir")},
		{faultAt: "close parent dir", calls: fullNew},
	}

	for _, tc := range cases {
		t.Run(tc.faultAt, func(t *testing.T) {
			intended := migrationIntendedQueue()
			legacy := migrationMarshalQueue(t, intended)
			projectDir := migrationFixture(t, legacy, nil, false)
			legacyPath, targetPath := migrationPaths(projectDir)
			calls := make([]string, 0, len(tc.calls))

			err := migrateFromLegacy(projectDir, migrationTestOps(&calls, tc.faultAt))
			if !errors.Is(err, errMigrationInjected) {
				t.Fatalf("migrateFromLegacy error = %v, want injected %s error", err, tc.faultAt)
			}
			migrationAssertCalls(t, calls, tc.calls...)
			if got := migrationIntendedCopyCount(t, intended, legacyPath, targetPath); got == 0 {
				t.Fatal("fault left no deeply equivalent intended queue copy")
			}

			legacyBeforeRetry := migrationFileExists(t, legacyPath)
			targetBeforeRetry := migrationFileExists(t, targetPath)
			retryCalls := make([]string, 0, len(fullNew))
			if err := migrateFromLegacy(projectDir, migrationTestOps(&retryCalls, "")); err != nil {
				t.Fatalf("retry after %s fault: %v", tc.faultAt, err)
			}
			migrationAssertRetryPath(t, retryCalls, legacyBeforeRetry, targetBeforeRetry)
			if migrationFileExists(t, legacyPath) {
				t.Fatal("legacy remains after successful retry")
			}
			if got := migrationIntendedCopyCount(t, intended, legacyPath, targetPath); got != 1 {
				t.Fatalf("intended queue copies after retry = %d, want 1", got)
			}
		})
	}
}

func TestMigrateFromLegacy_ExistingDestinationSyscallCutsAndRetries(t *testing.T) {
	fullExisting := []string{
		"read legacy", "stat destination", "read destination",
		"open queues dir", "sync queues dir", "close queues dir",
		"remove legacy", "open parent dir", "sync parent dir", "close parent dir",
	}
	cases := []struct {
		faultAt string
		calls   []string
	}{
		{faultAt: "read legacy", calls: fullExisting[:1]},
		{faultAt: "stat destination", calls: fullExisting[:2]},
		{faultAt: "read destination", calls: fullExisting[:3]},
		{faultAt: "open queues dir", calls: fullExisting[:4]},
		{faultAt: "sync queues dir", calls: append(append([]string{}, fullExisting[:5]...), "close queues dir")},
		{faultAt: "close queues dir", calls: fullExisting[:6]},
		{faultAt: "remove legacy", calls: fullExisting[:7]},
		{faultAt: "open parent dir", calls: fullExisting[:8]},
		{faultAt: "sync parent dir", calls: append(append([]string{}, fullExisting[:9]...), "close parent dir")},
		{faultAt: "close parent dir", calls: fullExisting},
	}

	for _, tc := range cases {
		t.Run(tc.faultAt, func(t *testing.T) {
			intended := migrationIntendedQueue()
			data := migrationMarshalQueue(t, intended)
			projectDir := migrationFixture(t, data, data, true)
			legacyPath, targetPath := migrationPaths(projectDir)
			calls := make([]string, 0, len(tc.calls))

			err := migrateFromLegacy(projectDir, migrationTestOps(&calls, tc.faultAt))
			if !errors.Is(err, errMigrationInjected) {
				t.Fatalf("migrateFromLegacy error = %v, want injected %s error", err, tc.faultAt)
			}
			migrationAssertCalls(t, calls, tc.calls...)
			if got := migrationIntendedCopyCount(t, intended, legacyPath, targetPath); got == 0 {
				t.Fatal("fault left no deeply equivalent intended queue copy")
			}

			legacyBeforeRetry := migrationFileExists(t, legacyPath)
			targetBeforeRetry := migrationFileExists(t, targetPath)
			retryCalls := make([]string, 0, len(fullExisting))
			if err := migrateFromLegacy(projectDir, migrationTestOps(&retryCalls, "")); err != nil {
				t.Fatalf("retry after %s fault: %v", tc.faultAt, err)
			}
			migrationAssertRetryPath(t, retryCalls, legacyBeforeRetry, targetBeforeRetry)
			if migrationFileExists(t, legacyPath) {
				t.Fatal("legacy remains after successful retry")
			}
			if got := migrationIntendedCopyCount(t, intended, legacyPath, targetPath); got != 1 {
				t.Fatalf("intended queue copies after retry = %d, want 1", got)
			}
		})
	}
}

func TestMigrateFromLegacy_RenameThenQueuesSyncFailureRetriesBeforeRemove(t *testing.T) {
	intended := migrationIntendedQueue()
	data := migrationMarshalQueue(t, intended)
	projectDir := migrationFixture(t, data, nil, false)
	legacyPath, targetPath := migrationPaths(projectDir)
	firstCalls := make([]string, 0, 11)

	err := migrateFromLegacy(projectDir, migrationTestOps(&firstCalls, "sync queues dir"))
	if !errors.Is(err, errMigrationInjected) {
		t.Fatalf("first migrate error = %v, want queues-sync fault", err)
	}
	migrationAssertCalls(t, firstCalls,
		"read legacy", "stat destination", "mkdir queues", "create temp",
		"write temp", "sync temp", "close temp", "rename destination",
		"open queues dir", "sync queues dir", "close queues dir",
	)
	if !migrationFileExists(t, legacyPath) || !migrationFileExists(t, targetPath) {
		t.Fatal("rename-success/queues-sync-fail must leave both intended copies")
	}

	retryCalls := make([]string, 0, 10)
	if err := migrateFromLegacy(projectDir, migrationTestOps(&retryCalls, "")); err != nil {
		t.Fatalf("retry: %v", err)
	}
	migrationAssertCalls(t, retryCalls,
		"read legacy", "stat destination", "read destination",
		"open queues dir", "sync queues dir", "close queues dir",
		"remove legacy", "open parent dir", "sync parent dir", "close parent dir",
	)
	if migrationFileExists(t, legacyPath) {
		t.Fatal("retry did not remove legacy after completing queues-directory sync")
	}
}

func TestMigrateFromLegacy_AbsentLegacySyncFaultsAndRetries(t *testing.T) {
	for _, tc := range []struct {
		faultAt string
		calls   []string
	}{
		{faultAt: "open parent dir", calls: []string{"read legacy", "open parent dir"}},
		{faultAt: "sync parent dir", calls: []string{"read legacy", "open parent dir", "sync parent dir", "close parent dir"}},
		{faultAt: "close parent dir", calls: []string{"read legacy", "open parent dir", "sync parent dir", "close parent dir"}},
	} {
		t.Run(tc.faultAt, func(t *testing.T) {
			projectDir := migrationFixture(t, nil, nil, false)
			legacyPath, _ := migrationPaths(projectDir)
			if err := os.Remove(legacyPath); err != nil {
				t.Fatalf("remove legacy fixture: %v", err)
			}
			calls := make([]string, 0, len(tc.calls))

			err := migrateFromLegacy(projectDir, migrationTestOps(&calls, tc.faultAt))
			if !errors.Is(err, errMigrationInjected) {
				t.Fatalf("migrateFromLegacy error = %v, want injected %s error", err, tc.faultAt)
			}
			migrationAssertCalls(t, calls, tc.calls...)

			retryCalls := make([]string, 0, 4)
			if err := migrateFromLegacy(projectDir, migrationTestOps(&retryCalls, "")); err != nil {
				t.Fatalf("retry after %s fault: %v", tc.faultAt, err)
			}
			migrationAssertCalls(t, retryCalls, "read legacy", "open parent dir", "sync parent dir", "close parent dir")
		})
	}
}

func migrationIntendedQueue() Queue {
	submitted := time.Date(2026, 7, 26, 18, 0, 0, 123, time.UTC)
	started := submitted.Add(time.Minute)
	appended := submitted.Add(2 * time.Minute)
	runID := "01977777-7777-7000-8000-000000000077"
	return Queue{
		SchemaVersion:  1,
		QueueID:        "01966666-6666-7000-8000-000000000066",
		Name:           QueueNameMain,
		Workers:        3,
		SpendCapUSD:    12.5,
		DefaultHarness: core.AgentTypeCodex,
		LocalOnly:      true,
		WorkerTarget:   "worker-a",
		SubmittedAt:    submitted,
		Status:         QueueStatusActive,
		Groups: []Group{{
			GroupIndex: 0,
			Kind:       GroupKindStream,
			Status:     GroupStatusActive,
			CreatedAt:  submitted,
			StartedAt:  &started,
			Items: []Item{{
				BeadID:             core.BeadID("hk-migration-intended"),
				Status:             ItemStatusDispatched,
				RunID:              &runID,
				AppendedAt:         &appended,
				Context:            "preserve every persisted field",
				WorkflowMode:       "dot",
				WorkflowRef:        "workflow.dot",
				TemplateParams:     map[string]string{"MODE": "strict"},
				Attempts:           2,
				LastFailureReason:  "retry",
				ReviewLoopFailures: 1,
			}},
		}},
	}
}

func migrationMarshalQueue(t *testing.T, q Queue) []byte {
	t.Helper()
	data, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("marshal queue: %v", err)
	}
	return data
}

func migrationFixture(t *testing.T, legacy, destination []byte, destinationExists bool) string {
	t.Helper()
	projectDir := t.TempDir()
	harmonikPath := harmonikDir(projectDir)
	if err := os.MkdirAll(harmonikPath, core.HarmonikDirMode); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	if err := os.WriteFile(legacyQueuePath(projectDir), legacy, 0o600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	if destinationExists {
		if err := os.MkdirAll(queuesDir(projectDir), core.HarmonikDirMode); err != nil {
			t.Fatalf("mkdir queues: %v", err)
		}
		if err := os.WriteFile(queuePath(projectDir, QueueNameMain), destination, 0o600); err != nil {
			t.Fatalf("write destination: %v", err)
		}
	}
	return projectDir
}

func migrationPaths(projectDir string) (legacyPath, targetPath string) {
	return legacyQueuePath(projectDir), queuePath(projectDir, QueueNameMain)
}

func migrationReadFile(t *testing.T, path string) []byte {
	t.Helper()
	//nolint:gosec // G304: test-only path under t.TempDir.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %q: %v", path, err)
	}
	return data
}

func migrationFileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	t.Fatalf("stat %q: %v", path, err)
	return false
}

func migrationIntendedCopyCount(t *testing.T, intended Queue, paths ...string) int {
	t.Helper()
	count := 0
	for _, path := range paths {
		//nolint:gosec // G304: test-only migration paths under t.TempDir.
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("read %q: %v", path, err)
		}
		got, err := UnmarshalQueue(data)
		if err == nil && reflect.DeepEqual(got, intended) {
			count++
		}
	}
	return count
}

func migrationAssertCalls(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("operation order:\n got: %s\nwant: %s", strings.Join(got, " → "), strings.Join(want, " → "))
	}
}

func migrationAssertRetryPath(t *testing.T, calls []string, legacyExists, targetExists bool) {
	t.Helper()
	switch {
	case !legacyExists:
		migrationAssertCalls(t, calls, "read legacy", "open parent dir", "sync parent dir", "close parent dir")
	case targetExists:
		migrationAssertCalls(t, calls,
			"read legacy", "stat destination", "read destination",
			"open queues dir", "sync queues dir", "close queues dir",
			"remove legacy", "open parent dir", "sync parent dir", "close parent dir",
		)
	default:
		migrationAssertCalls(t, calls,
			"read legacy", "stat destination", "mkdir queues", "create temp",
			"write temp", "sync temp", "close temp", "rename destination",
			"open queues dir", "sync queues dir", "close queues dir",
			"remove legacy", "open parent dir", "sync parent dir", "close parent dir",
		)
	}
}

func migrationTestOps(calls *[]string, faultAt string) migrateFromLegacyOps {
	fault := func(operation string) error {
		*calls = append(*calls, operation)
		if operation == faultAt {
			return fmt.Errorf("%w: %s", errMigrationInjected, operation)
		}
		return nil
	}
	return migrateFromLegacyOps{
		readFile: func(path string) ([]byte, error) {
			operation := "read destination"
			if filepath.Base(path) == "queue.json" {
				operation = "read legacy"
			}
			if err := fault(operation); err != nil {
				return nil, err
			}
			//nolint:gosec // G304: test-only path under t.TempDir.
			return os.ReadFile(path)
		},
		stat: func(path string) (os.FileInfo, error) {
			if err := fault("stat destination"); err != nil {
				return nil, err
			}
			return os.Stat(path)
		},
		mkdirAll: func(path string, mode os.FileMode) error {
			if err := fault("mkdir queues"); err != nil {
				return err
			}
			return os.MkdirAll(path, mode)
		},
		createTemp: func(path string, flag int, mode os.FileMode) (*os.File, error) {
			if err := fault("create temp"); err != nil {
				return nil, err
			}
			//nolint:gosec // G304: test-only path under t.TempDir.
			return os.OpenFile(path, flag, mode)
		},
		writeFile: func(file *os.File, data []byte) (int, error) {
			if err := fault("write temp"); err != nil {
				return 0, err
			}
			return file.Write(data)
		},
		syncFile: func(file *os.File) error {
			if err := fault("sync temp"); err != nil {
				return err
			}
			return file.Sync()
		},
		closeFile: func(file *os.File) error {
			if err := fault("close temp"); err != nil {
				return errors.Join(err, file.Close())
			}
			return file.Close()
		},
		rename: func(oldPath, newPath string) error {
			if err := fault("rename destination"); err != nil {
				return err
			}
			return os.Rename(oldPath, newPath)
		},
		openDir: func(path string) (*os.File, error) {
			operation := "open queues dir"
			if filepath.Base(path) == ".harmonik" {
				operation = "open parent dir"
			}
			if err := fault(operation); err != nil {
				return nil, err
			}
			//nolint:gosec // G304: test-only path under t.TempDir.
			return os.Open(path)
		},
		syncDir: func(dir *os.File) error {
			operation := "sync queues dir"
			if filepath.Base(dir.Name()) == ".harmonik" {
				operation = "sync parent dir"
			}
			if err := fault(operation); err != nil {
				return err
			}
			return dir.Sync()
		},
		closeDir: func(dir *os.File) error {
			operation := "close queues dir"
			if filepath.Base(dir.Name()) == ".harmonik" {
				operation = "close parent dir"
			}
			if err := fault(operation); err != nil {
				return errors.Join(err, dir.Close())
			}
			return dir.Close()
		},
		remove: func(path string) error {
			operation := "remove legacy"
			if strings.Contains(filepath.Base(path), ".tmp-migrate-") {
				operation = "remove temp"
			}
			if err := fault(operation); err != nil {
				return err
			}
			return os.Remove(path)
		},
	}
}
