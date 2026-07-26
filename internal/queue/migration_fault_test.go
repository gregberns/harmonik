package queue

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

func TestMigrateFromLegacy_DestinationOutcomes(t *testing.T) {
	t.Parallel()

	legacy := []byte(`{"schema_version":1,"queue_id":"legacy"}`)
	for _, tc := range []struct {
		name          string
		destination   []byte
		wantErr       bool
		legacyRemains bool
	}{
		{name: "corrupt", destination: []byte(`not json`), wantErr: true, legacyRemains: true},
		{name: "empty", destination: nil, wantErr: true, legacyRemains: true},
		{name: "conflicting", destination: []byte(`{"schema_version":1,"queue_id":"other"}`), wantErr: true, legacyRemains: true},
		{name: "valid equivalent", destination: []byte("{\n  \"queue_id\": \"legacy\",\n  \"schema_version\": 1\n}\n"), legacyRemains: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			projectDir := migrationFixture(t, legacy, tc.destination, true)
			legacyPath, targetPath := migrationPaths(projectDir)
			//nolint:gosec // G304: targetPath is a test-only path under t.TempDir.
			targetBefore, readErr := os.ReadFile(targetPath)
			if readErr != nil {
				t.Fatalf("read destination before migration: %v", readErr)
			}

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
			if got := migrationParseableCopies(t, legacyPath, targetPath); got == 0 {
				t.Fatalf("parseable migration copies = 0, want at least one")
			}
			//nolint:gosec // G304: targetPath is a test-only path under t.TempDir.
			targetAfter, readErr := os.ReadFile(targetPath)
			if readErr != nil {
				t.Fatalf("read destination after migration: %v", readErr)
			}
			if !bytes.Equal(targetAfter, targetBefore) {
				t.Errorf("destination changed during %s outcome", tc.name)
			}
			if !tc.wantErr {
				if err := MigrateFromLegacy(context.Background(), projectDir); err != nil {
					t.Fatalf("idempotent retry: %v", err)
				}
			}
		})
	}
}

func TestMigrateFromLegacy_FaultsPreserveParseableCopyAndOrder(t *testing.T) {
	legacy := []byte(`{"schema_version":1,"queue_id":"legacy"}`)

	for _, tc := range []struct {
		name      string
		faultAt   string
		wantCalls []string
	}{
		{name: "read legacy", faultAt: "read legacy", wantCalls: []string{"read legacy"}},
		{name: "mkdir queues", faultAt: "mkdir queues", wantCalls: []string{"read legacy", "mkdir queues"}},
		{name: "read destination", faultAt: "read destination", wantCalls: []string{"read legacy", "mkdir queues", "read destination"}},
		{name: "write destination", faultAt: "write destination", wantCalls: []string{"read legacy", "mkdir queues", "read destination", "write destination"}},
		{name: "sync queues", faultAt: "sync queues", wantCalls: []string{"read legacy", "mkdir queues", "read destination", "write destination", "sync queues"}},
		{name: "remove legacy", faultAt: "remove legacy", wantCalls: []string{"read legacy", "mkdir queues", "read destination", "write destination", "sync queues", "remove legacy"}},
		{name: "sync parent", faultAt: "sync parent", wantCalls: []string{"read legacy", "mkdir queues", "read destination", "write destination", "sync queues", "remove legacy", "sync parent"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := migrationFixture(t, legacy, nil, false)
			legacyPath, targetPath := migrationPaths(projectDir)
			calls := make([]string, 0, len(tc.wantCalls))
			ops := migrationTestOps(t, &calls, tc.faultAt)

			if err := migrateFromLegacy(projectDir, ops); err == nil {
				t.Fatalf("migrateFromLegacy fault at %q returned nil", tc.faultAt)
			}
			if got := strings.Join(calls, ","); got != strings.Join(tc.wantCalls, ",") {
				t.Errorf("operation order = %q, want %q", got, strings.Join(tc.wantCalls, ","))
			}
			if got := migrationParseableCopies(t, legacyPath, targetPath); got == 0 {
				t.Fatal("fault left no parseable migration copy")
			}
		})
	}
}

func TestMigrateFromLegacy_RetrySyncsParentAfterAbsentLegacyConvergence(t *testing.T) {
	legacy := []byte(`{"schema_version":1,"queue_id":"legacy"}`)
	projectDir := migrationFixture(t, legacy, nil, false)
	legacyPath, targetPath := migrationPaths(projectDir)

	firstCalls := make([]string, 0, 7)
	if err := migrateFromLegacy(projectDir, migrationTestOps(t, &firstCalls, "sync parent")); err == nil {
		t.Fatal("first migration error = nil, want parent-sync fault")
	}
	if migrationFileExists(t, legacyPath) {
		t.Fatal("legacy file remains after the successful delete")
	}
	if got := migrationParseableCopies(t, legacyPath, targetPath); got != 1 {
		t.Fatalf("parseable migration copies after parent-sync fault = %d, want 1", got)
	}

	retryCalls := make([]string, 0, 3)
	if err := migrateFromLegacy(projectDir, migrationTestOps(t, &retryCalls, "")); err != nil {
		t.Fatalf("retry after parent-sync fault: %v", err)
	}
	if got, want := strings.Join(retryCalls, ","), "read legacy,read destination,sync parent"; got != want {
		t.Errorf("retry operation order = %q, want %q", got, want)
	}
	if got := migrationParseableCopies(t, legacyPath, targetPath); got != 1 {
		t.Fatalf("parseable migration copies after retry = %d, want 1", got)
	}
}

func migrationFixture(t *testing.T, legacy, destination []byte, destinationExists bool) string {
	t.Helper()
	projectDir := t.TempDir()
	harmonikDir := filepath.Join(projectDir, ".harmonik")
	if err := os.MkdirAll(harmonikDir, core.HarmonikDirMode); err != nil {
		t.Fatalf("mkdir .harmonik: %v", err)
	}
	if err := os.WriteFile(filepath.Join(harmonikDir, "queue.json"), legacy, 0o600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	if destinationExists {
		queuesDir := filepath.Join(harmonikDir, "queues")
		if err := os.MkdirAll(queuesDir, core.HarmonikDirMode); err != nil {
			t.Fatalf("mkdir queues: %v", err)
		}
		if err := os.WriteFile(filepath.Join(queuesDir, "main.json"), destination, 0o600); err != nil {
			t.Fatalf("write destination: %v", err)
		}
	}
	return projectDir
}

func migrationPaths(projectDir string) (legacyPath, targetPath string) {
	return filepath.Join(projectDir, ".harmonik", "queue.json"), filepath.Join(projectDir, ".harmonik", "queues", "main.json")
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

func migrationParseableCopies(t *testing.T, paths ...string) int {
	t.Helper()
	parseable := 0
	for _, path := range paths {
		//nolint:gosec // G304: paths are test-only migration paths under t.TempDir.
		data, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("read %q: %v", path, err)
		}
		if _, err := UnmarshalQueue(data); err == nil {
			parseable++
		}
	}
	return parseable
}

func migrationTestOps(t *testing.T, calls *[]string, faultAt string) migrateFromLegacyOps {
	t.Helper()
	fault := func(operation string) error {
		*calls = append(*calls, operation)
		if operation == faultAt {
			return fmt.Errorf("injected %s fault", operation)
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
			//nolint:gosec // G304: path is a test-only migration path under t.TempDir.
			return os.ReadFile(path)
		},
		mkdirAll: func(path string, mode os.FileMode) error {
			if err := fault("mkdir queues"); err != nil {
				return err
			}
			return os.MkdirAll(path, mode)
		},
		writeTarget: func(path string, data []byte) error {
			if err := fault("write destination"); err != nil {
				return err
			}
			return os.WriteFile(path, data, 0o600)
		},
		remove: func(path string) error {
			if err := fault("remove legacy"); err != nil {
				return err
			}
			return os.Remove(path)
		},
		syncDir: func(path string) error {
			operation := "sync queues"
			if filepath.Base(path) == ".harmonik" {
				operation = "sync parent"
			}
			if err := fault(operation); err != nil {
				return err
			}
			return fsyncDir(path)
		},
	}
}
