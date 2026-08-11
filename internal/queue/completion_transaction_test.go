package queue

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func completionReplacementFixture(t *testing.T) (ReplacementPlan, CompletionPlan) {
	t.Helper()
	prior := completionFixtureQueue()
	prepared, err := PrepareCompletion(
		prior,
		completionTransactionID,
		completionReceiptID,
		completionFixtureTime(),
	)
	if err != nil {
		t.Fatal(err)
	}
	priorBytes, err := jsonMarshal(prior)
	if err != nil {
		t.Fatal(err)
	}
	plan := ReplacementPlan{
		ProjectDir:               t.TempDir(),
		TransactionID:            completionTransactionID,
		OperationKind:            OperationCompletion,
		NormalizedName:           QueueNameMain,
		QueueID:                  completionQueueID,
		PriorBytes:               priorBytes,
		CandidateBytes:           prepared.CandidateBytes,
		CompletionReceiptBinding: prepared.Binding,
	}
	transactionSeedPrior(t, plan)
	return plan, prepared
}

func jsonMarshal(value any) ([]byte, error) {
	return json.Marshal(value)
}

func TestWriteReplacementCompletionReceiptFaultCutsReportCanonicalPhase(t *testing.T) {
	tests := []struct {
		name string
		cut  func(*namespaceOps)
	}{
		{
			name: "root create",
			cut: func(ops *namespaceOps) {
				mkdir := ops.mkdirAll
				ops.mkdirAll = func(path string, mode os.FileMode) error {
					if strings.HasSuffix(path, ".completion-receipts") {
						return errors.New("cut receipt root create")
					}
					return mkdir(path, mode)
				}
			},
		},
		{
			name: "receipt create",
			cut: func(ops *namespaceOps) {
				open := ops.openFile
				ops.openFile = func(path string, flags int, mode os.FileMode) (*os.File, error) {
					if strings.Contains(path, ".completion-receipts") {
						return nil, errors.New("cut receipt create")
					}
					return open(path, flags, mode)
				}
			},
		},
		{
			name: "receipt root parent sync",
			cut: func(ops *namespaceOps) {
				syncDir := ops.syncDir
				queueSyncs := 0
				ops.syncDir = func(f *os.File) error {
					if strings.HasSuffix(f.Name(), filepath.Join(".harmonik", "queues")) {
						queueSyncs++
						if queueSyncs == 3 {
							return errors.New("cut receipt root parent sync")
						}
					}
					return syncDir(f)
				}
			},
		},
		{
			name: "receipt write",
			cut: func(ops *namespaceOps) {
				write := ops.write
				ops.write = func(w io.Writer, data []byte) error {
					if f, ok := w.(*os.File); ok && strings.Contains(f.Name(), ".completion-receipts") {
						return errors.New("cut receipt write")
					}
					return write(w, data)
				}
			},
		},
		{
			name: "receipt sync",
			cut: func(ops *namespaceOps) {
				syncFile := ops.syncFile
				ops.syncFile = func(f *os.File) error {
					if strings.Contains(f.Name(), ".completion-receipts") {
						return errors.New("cut receipt sync")
					}
					return syncFile(f)
				}
			},
		},
		{
			name: "receipt close",
			cut: func(ops *namespaceOps) {
				closeFile := ops.closeFile
				ops.closeFile = func(f *os.File) error {
					if strings.Contains(f.Name(), ".completion-receipts") {
						return errors.New("cut receipt close")
					}
					return closeFile(f)
				}
			},
		},
		{
			name: "receipt install",
			cut: func(ops *namespaceOps) {
				link := ops.link
				ops.link = func(oldPath, newPath string) error {
					if strings.Contains(newPath, ".completion-receipts") {
						return errors.New("cut receipt install")
					}
					return link(oldPath, newPath)
				}
			},
		},
		{
			name: "receipt root sync",
			cut: func(ops *namespaceOps) {
				syncDir := ops.syncDir
				ops.syncDir = func(f *os.File) error {
					if strings.HasSuffix(f.Name(), ".completion-receipts") {
						return errors.New("cut receipt root sync")
					}
					return syncDir(f)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan, prepared := completionReplacementFixture(t)
			ops := osNamespaceOps()
			tc.cut(&ops)
			result := writeReplacement(context.Background(), plan, ops)
			if result.Phase != CompletionPhaseCanonicalCommitted {
				t.Fatalf("phase = %q, want %q; err=%v", result.Phase, CompletionPhaseCanonicalCommitted, result.Err)
			}
			canonical, err := os.ReadFile(queuePath(plan.ProjectDir, QueueNameMain))
			if err != nil || !bytes.Equal(canonical, prepared.CandidateBytes) {
				t.Fatalf("completed canonical = %q, %v", canonical, err)
			}
		})
	}
}

func TestWriteReplacementCompletionReceiptDurable(t *testing.T) {
	plan, prepared := completionReplacementFixture(t)
	result := WriteReplacement(context.Background(), plan)
	if !result.Committed() || result.Phase != CompletionPhaseReceiptDurable {
		t.Fatalf("result = %+v", result)
	}
	path := filepath.Join(completionReceiptsDir(plan.ProjectDir), prepared.Binding.Basename)
	got, err := os.ReadFile(path) //nolint:gosec // path is derived from a validated test binding under t.TempDir
	if err != nil || !bytes.Equal(got, prepared.ReceiptBytes) {
		t.Fatalf("receipt = %q, %v", got, err)
	}
}

func TestCompletionReceiptNoReplaceAmbiguityReloadsExactInstalledFacts(t *testing.T) {
	plan, prepared := completionReplacementFixture(t)
	ops := osNamespaceOps()
	link := ops.link
	linkCalls := 0
	ops.link = func(oldPath, newPath string) error {
		if !strings.Contains(newPath, ".completion-receipts") {
			return link(oldPath, newPath)
		}
		linkCalls++
		if err := link(oldPath, newPath); err != nil {
			return err
		}
		return errors.New("receipt link reported ambiguity after install")
	}
	result := writeReplacement(t.Context(), plan, ops)
	if !result.Committed() || result.Phase != CompletionPhaseReceiptDurable {
		t.Fatalf("result = %+v", result)
	}
	if linkCalls != 1 {
		t.Fatalf("receipt link calls = %d, want 1", linkCalls)
	}
	receiptPath := filepath.Join(completionReceiptsDir(plan.ProjectDir), prepared.Binding.Basename)
	got, err := os.ReadFile(receiptPath) //nolint:gosec // path is under t.TempDir and uses a validated basename.
	if err != nil || !bytes.Equal(got, prepared.ReceiptBytes) {
		t.Fatalf("receipt = %q, err=%v", got, err)
	}
}

func TestCompletionReceiptNoReplaceAmbiguityPreservesConflictingInstalledFacts(t *testing.T) {
	plan, _ := completionReplacementFixture(t)
	ops := osNamespaceOps()
	link := ops.link
	conflict := []byte("conflicting installed receipt")
	ops.link = func(oldPath, newPath string) error {
		if !strings.Contains(newPath, ".completion-receipts") {
			return link(oldPath, newPath)
		}
		if err := link(oldPath, newPath); err != nil {
			return err
		}
		if err := os.WriteFile(newPath, conflict, 0o600); err != nil {
			return err
		}
		return errors.New("receipt link reported ambiguity with conflicting target")
	}
	result := writeReplacement(t.Context(), plan, ops)
	if result.Phase != CompletionPhaseCanonicalCommitted || result.Err == nil {
		t.Fatalf("result = %+v", result)
	}
	entries, err := os.ReadDir(completionReceiptsDir(plan.ProjectDir))
	if err != nil {
		t.Fatal(err)
	}
	var installed []byte
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			installed, err = os.ReadFile(filepath.Join(completionReceiptsDir(plan.ProjectDir), entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if !bytes.Equal(installed, conflict) {
		t.Fatalf("conflicting receipt changed: %q", installed)
	}
}

func TestWriteCompletionReceiptRejectsSymlinkRoot(t *testing.T) {
	plan, _ := completionReplacementFixture(t)
	target := t.TempDir()
	root := completionReceiptsDir(plan.ProjectDir)
	if err := os.Symlink(target, root); err != nil {
		t.Fatal(err)
	}
	result := WriteReplacement(context.Background(), plan)
	if result.Phase != CompletionPhaseCanonicalCommitted || result.Err == nil {
		t.Fatalf("result = %+v", result)
	}
	if entries, err := os.ReadDir(target); err != nil || len(entries) != 0 {
		t.Fatalf("symlink target was modified: entries=%v err=%v", entries, err)
	}
}

func TestCompletionCanonicalCutsReportExplicitPhases(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cut       func(*namespaceOps)
		wantPhase CompletionPhase
	}{
		{
			name: "candidate create",
			cut: func(ops *namespaceOps) {
				ops.openFile = func(string, int, os.FileMode) (*os.File, error) { return nil, errors.New("cut candidate create") }
			},
			wantPhase: CompletionPhaseNotCommitted,
		},
		{
			name: "intent parent sync",
			cut: func(ops *namespaceOps) {
				ops.syncDir = func(*os.File) error { return errors.New("cut intent sync") }
			},
			wantPhase: CompletionPhaseCommitIndeterminate,
		},
		{
			name: "canonical rename",
			cut: func(ops *namespaceOps) {
				ops.rename = func(string, string) error { return errors.New("cut canonical rename") }
			},
			wantPhase: CompletionPhaseNotCommitted,
		},
		{
			name: "canonical parent sync",
			cut: func(ops *namespaceOps) {
				syncDir := ops.syncDir
				calls := 0
				ops.syncDir = func(f *os.File) error {
					calls++
					if calls == 2 {
						return errors.New("cut canonical sync")
					}
					return syncDir(f)
				}
			},
			wantPhase: CompletionPhaseCommitIndeterminate,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, _ := completionReplacementFixture(t)
			ops := osNamespaceOps()
			tc.cut(&ops)
			result := writeReplacement(context.Background(), plan, ops)
			if result.Phase != tc.wantPhase {
				t.Fatalf("phase = %q, want %q; outcome=%q err=%v", result.Phase, tc.wantPhase, result.Outcome, result.Err)
			}
		})
	}
}

func TestCleanupCompletedCanonicalUsesIdentityAndDigestCAS(t *testing.T) {
	t.Run("exact", func(t *testing.T) {
		plan, prepared := completionReplacementFixture(t)
		if result := WriteReplacement(context.Background(), plan); !result.Committed() {
			t.Fatal(result.Err)
		}
		if err := CleanupCompletedCanonical(plan.ProjectDir, QueueNameMain, completionQueueID, prepared.Receipt.CompletedQueueSHA256); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(queuePath(plan.ProjectDir, QueueNameMain)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("canonical still present: %v", err)
		}
	})
	t.Run("newer identity", func(t *testing.T) {
		plan, prepared := completionReplacementFixture(t)
		newer := prepared.Candidate
		newer.QueueID = "0197c451-0000-7000-8000-000000000099"
		data, err := jsonMarshal(newer)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(queuePath(plan.ProjectDir, QueueNameMain), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := CleanupCompletedCanonical(plan.ProjectDir, QueueNameMain, completionQueueID, prepared.Receipt.CompletedQueueSHA256); err == nil {
			t.Fatal("cleanup accepted newer identity")
		}
		got, readErr := os.ReadFile(queuePath(plan.ProjectDir, QueueNameMain))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(got, data) {
			t.Fatal("cleanup changed newer canonical")
		}
	})
	t.Run("same identity changed bytes", func(t *testing.T) {
		plan, prepared := completionReplacementFixture(t)
		changed := prepared.Candidate
		changed.Workers++
		data, err := jsonMarshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(queuePath(plan.ProjectDir, QueueNameMain), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := CleanupCompletedCanonical(plan.ProjectDir, QueueNameMain, completionQueueID, prepared.Receipt.CompletedQueueSHA256); err == nil {
			t.Fatal("cleanup accepted changed bytes")
		}
		got, readErr := os.ReadFile(queuePath(plan.ProjectDir, QueueNameMain))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if !bytes.Equal(got, data) {
			t.Fatal("cleanup changed mismatched canonical")
		}
	})
	t.Run("absent is idempotent", func(t *testing.T) {
		plan, prepared := completionReplacementFixture(t)
		if err := os.Remove(queuePath(plan.ProjectDir, QueueNameMain)); err != nil {
			t.Fatal(err)
		}
		if err := CleanupCompletedCanonical(plan.ProjectDir, QueueNameMain, completionQueueID, prepared.Receipt.CompletedQueueSHA256); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCleanupCompletedCanonicalFaultCuts(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cut     func(*namespaceOps)
		wantErr bool
	}{
		{name: "remove", wantErr: true, cut: func(ops *namespaceOps) {
			ops.remove = func(string) error { return errors.New("cut remove") }
		}},
		{name: "directory open", wantErr: true, cut: func(ops *namespaceOps) {
			ops.openDir = func(string) (*os.File, error) { return nil, errors.New("cut open") }
		}},
		{name: "directory sync", wantErr: true, cut: func(ops *namespaceOps) {
			ops.syncDir = func(*os.File) error { return errors.New("cut sync") }
		}},
		{name: "directory close after sync", wantErr: false, cut: func(ops *namespaceOps) {
			ops.closeDir = func(*os.File) error { return errors.New("cut close") }
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, prepared := completionReplacementFixture(t)
			if result := WriteReplacement(context.Background(), plan); !result.Committed() {
				t.Fatal(result.Err)
			}
			ops := osNamespaceOps()
			tc.cut(&ops)
			err := cleanupCompletedCanonical(plan.ProjectDir, QueueNameMain, completionQueueID, prepared.Receipt.CompletedQueueSHA256, ops)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
