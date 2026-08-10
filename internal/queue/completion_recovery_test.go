package queue

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverCompletionIntentProcessDeathMatrix(t *testing.T) {
	tests := []struct {
		name      string
		canonical string
		candidate string
		receipt   string
		want      ReplaceRecoveryAction
		wantPhase CompletionPhase
	}{
		{name: "before candidate rename", canonical: "prior", candidate: "exact", receipt: "absent", want: ReplaceRetryRename, wantPhase: CompletionPhaseCleaned},
		{name: "before canonical commit", canonical: "prior", candidate: "absent", receipt: "absent", want: ReplaceNotCommitted, wantPhase: CompletionPhaseNotCommitted},
		{name: "after canonical commit", canonical: "candidate", candidate: "absent", receipt: "absent", want: ReplacePromoteCanonical, wantPhase: CompletionPhaseCleaned},
		{name: "after receipt install", canonical: "candidate", candidate: "absent", receipt: "exact", want: ReplacePromoteCanonical, wantPhase: CompletionPhaseCleaned},
		{name: "after canonical cleanup", canonical: "absent", candidate: "absent", receipt: "exact", want: ReplacePromoteCanonical, wantPhase: CompletionPhaseCleaned},
		{name: "canonical absent without receipt", canonical: "absent", candidate: "absent", receipt: "absent", want: ReplaceRefuse, wantPhase: CompletionPhaseCommitIndeterminate},
		{name: "receipt before commit", canonical: "prior", candidate: "exact", receipt: "exact", want: ReplaceRefuse, wantPhase: CompletionPhaseCommitIndeterminate},
		{name: "conflicting receipt", canonical: "candidate", candidate: "absent", receipt: "wrong", want: ReplaceRefuse, wantPhase: CompletionPhaseCommitIndeterminate},
		{name: "changed canonical", canonical: "wrong", candidate: "absent", receipt: "exact", want: ReplaceRefuse, wantPhase: CompletionPhaseCommitIndeterminate},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan, prepared := completionReplacementFixture(t)
			intent, intentBytes, err := prepareReplacement(plan)
			if err != nil {
				t.Fatal(err)
			}
			qDir := queuesDir(plan.ProjectDir)
			if err := os.MkdirAll(qDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(replaceIntentPath(plan.ProjectDir, QueueNameMain), intentBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			writeCompletionRecoveryFact(t, queuePath(plan.ProjectDir, QueueNameMain), tc.canonical, plan.PriorBytes, plan.CandidateBytes)
			writeCompletionRecoveryFact(
				t,
				filepath.Join(qDir, intent.CandidateTempBasename),
				tc.candidate,
				nil,
				plan.CandidateBytes,
			)
			receiptPath := filepath.Join(completionReceiptsDir(plan.ProjectDir), prepared.Binding.Basename)
			writeCompletionRecoveryFact(t, receiptPath, tc.receipt, nil, prepared.ReceiptBytes)
			factPaths := []string{
				queuePath(plan.ProjectDir, QueueNameMain),
				filepath.Join(qDir, intent.CandidateTempBasename),
				receiptPath,
				replaceIntentPath(plan.ProjectDir, QueueNameMain),
			}
			before := readCompletionRecoveryFacts(t, factPaths)

			action, phase, recoverErr := recoverCompletionReplaceIntent(
				plan.ProjectDir,
				intent,
				intentBytes,
				osNamespaceOps(),
			)
			if action != tc.want {
				t.Fatalf("action = %q, want %q; err=%v", action, tc.want, recoverErr)
			}
			if phase != tc.wantPhase {
				t.Fatalf("phase = %q, want %q; err=%v", phase, tc.wantPhase, recoverErr)
			}
			if tc.want == ReplaceRefuse {
				if recoverErr == nil {
					t.Fatal("refusal has no error")
				}
				if _, err := os.Stat(replaceIntentPath(plan.ProjectDir, QueueNameMain)); err != nil {
					t.Fatalf("refusal changed intent: %v", err)
				}
				after := readCompletionRecoveryFacts(t, factPaths)
				for i := range before {
					if before[i].present != after[i].present || !bytes.Equal(before[i].data, after[i].data) {
						t.Fatalf("refusal changed fact %s: before=%+v after=%+v", factPaths[i], before[i], after[i])
					}
				}
				return
			}
			if recoverErr != nil {
				t.Fatal(recoverErr)
			}
			for _, path := range []string{replaceIntentPath(plan.ProjectDir, QueueNameMain)} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("resolved path %s remains: %v", path, err)
				}
			}
			if tc.want == ReplaceNotCommitted {
				gotCanonical, err := os.ReadFile(queuePath(plan.ProjectDir, QueueNameMain))
				if err != nil || !bytes.Equal(gotCanonical, plan.PriorBytes) {
					t.Fatalf("rollback canonical = %q, err=%v", gotCanonical, err)
				}
				if _, err := os.Stat(receiptPath); !os.IsNotExist(err) {
					t.Fatalf("rollback installed receipt: %v", err)
				}
				return
			}
			if _, err := os.Stat(queuePath(plan.ProjectDir, QueueNameMain)); !os.IsNotExist(err) {
				t.Fatalf("resolved canonical remains: %v", err)
			}
			gotReceipt, err := os.ReadFile(receiptPath) //nolint:gosec // path is under t.TempDir
			if err != nil || !bytes.Equal(gotReceipt, prepared.ReceiptBytes) {
				t.Fatalf("receipt = %q, err=%v", gotReceipt, err)
			}
		})
	}
}

func TestRecoverCompletionIntentDoesNotClaimCleanedBeforeIntentDurability(t *testing.T) {
	tests := []struct {
		name string
		cut  func(*namespaceOps, string)
	}{
		{
			name: "intent remove",
			cut: func(ops *namespaceOps, intentPath string) {
				remove := ops.remove
				ops.remove = func(path string) error {
					if path == intentPath {
						return errors.New("cut intent remove")
					}
					return remove(path)
				}
			},
		},
		{
			name: "intent parent sync",
			cut: func(ops *namespaceOps, intentPath string) {
				qDir := filepath.Dir(intentPath)
				syncDir := ops.syncDir
				queueSyncs := 0
				ops.syncDir = func(file *os.File) error {
					if file.Name() == qDir {
						queueSyncs++
						if queueSyncs == 3 {
							return errors.New("cut intent parent sync")
						}
					}
					return syncDir(file)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan, prepared := completionReplacementFixture(t)
			intent, intentBytes, err := prepareReplacement(plan)
			if err != nil {
				t.Fatal(err)
			}
			intentPath := replaceIntentPath(plan.ProjectDir, QueueNameMain)
			if err := os.WriteFile(intentPath, intentBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(queuePath(plan.ProjectDir, QueueNameMain), plan.CandidateBytes, 0o600); err != nil {
				t.Fatal(err)
			}
			ops := osNamespaceOps()
			tc.cut(&ops, intentPath)
			action, phase, recoverErr := recoverCompletionReplaceIntent(plan.ProjectDir, intent, intentBytes, ops)
			if action != ReplaceRefuse || recoverErr == nil || phase != CompletionPhaseReceiptDurable {
				t.Fatalf("result = (%q, %q, %v)", action, phase, recoverErr)
			}
			receiptPath := filepath.Join(completionReceiptsDir(plan.ProjectDir), prepared.Binding.Basename)
			got, err := os.ReadFile(receiptPath) //nolint:gosec // path is under t.TempDir
			if err != nil || !bytes.Equal(got, prepared.ReceiptBytes) {
				t.Fatalf("receipt = %q, err=%v", got, err)
			}
			if _, err := os.Stat(queuePath(plan.ProjectDir, QueueNameMain)); !os.IsNotExist(err) {
				t.Fatalf("canonical cleanup did not finish: %v", err)
			}
		})
	}
}

func TestRecoverCompletionIntentSecondIntentReadIsIndeterminate(t *testing.T) {
	plan, _ := completionReplacementFixture(t)
	_, intentBytes, err := prepareReplacement(plan)
	if err != nil {
		t.Fatal(err)
	}
	intentPath := replaceIntentPath(plan.ProjectDir, QueueNameMain)
	if err := os.WriteFile(intentPath, intentBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	ops := osNamespaceOps()
	readFile := ops.readFile
	reads := 0
	ops.readFile = func(path string) ([]byte, error) {
		if path == intentPath {
			reads++
			if reads == 2 {
				return nil, errors.New("cut second intent read")
			}
		}
		return readFile(path)
	}
	recoveries, err := recoverReplaceIntents(plan.ProjectDir, ops)
	if err != nil {
		t.Fatal(err)
	}
	if len(recoveries) != 1 || recoveries[0].Completion == nil {
		t.Fatalf("recoveries = %+v", recoveries)
	}
	got := recoveries[0]
	if got.Action != ReplaceRefuse || got.Err == nil || got.Completion.Phase != CompletionPhaseCommitIndeterminate {
		t.Fatalf("recovery = %+v", got)
	}
}

func TestRecoverReplaceIntentsRoutesCompletionTransaction(t *testing.T) {
	plan, prepared := completionReplacementFixture(t)
	_, intentBytes, err := prepareReplacement(plan)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replaceIntentPath(plan.ProjectDir, QueueNameMain), intentBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(queuePath(plan.ProjectDir, QueueNameMain), plan.CandidateBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	recoveries, err := RecoverReplaceIntents(plan.ProjectDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(recoveries) != 1 || !recoveries[0].Resolved() || recoveries[0].Action != ReplacePromoteCanonical {
		t.Fatalf("recoveries = %+v", recoveries)
	}
	completion := recoveries[0].Completion
	if completion == nil || completion.Phase != CompletionPhaseCleaned || !completion.ReleaseMarkerPending {
		t.Fatalf("completion handoff = %+v", completion)
	}
	for _, path := range []string{
		queuePath(plan.ProjectDir, QueueNameMain),
		replaceIntentPath(plan.ProjectDir, QueueNameMain),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("resolved path %s remains: %v", path, err)
		}
	}
	receiptPath := filepath.Join(completionReceiptsDir(plan.ProjectDir), prepared.Binding.Basename)
	got, err := os.ReadFile(receiptPath) //nolint:gosec // path is under t.TempDir
	if err != nil || !bytes.Equal(got, prepared.ReceiptBytes) {
		t.Fatalf("receipt = %q, err=%v", got, err)
	}
	markerBasename, err := CompletionReleaseMarkerBasename(prepared.Receipt.QueueID, prepared.Receipt.ReceiptID)
	if err != nil {
		t.Fatal(err)
	}
	markerPath := filepath.Join(completionReceiptsDir(plan.ProjectDir), markerBasename)
	if _, err := os.Stat(markerPath); !os.IsNotExist(err) {
		t.Fatalf("C09 installed C11 marker: %v", err)
	}
	replayed, err := RecoverReplaceIntents(plan.ProjectDir)
	if err != nil || len(replayed) != 0 {
		t.Fatalf("resolved replay = %+v, err=%v", replayed, err)
	}
	replayedReceipt, err := os.ReadFile(receiptPath) //nolint:gosec // path is under t.TempDir
	if err != nil || !bytes.Equal(replayedReceipt, prepared.ReceiptBytes) {
		t.Fatalf("replay changed receipt = %q, err=%v", replayedReceipt, err)
	}
}

type completionRecoveryFact struct {
	data    []byte
	present bool
}

func readCompletionRecoveryFacts(t *testing.T, paths []string) []completionRecoveryFact {
	t.Helper()
	facts := make([]completionRecoveryFact, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path) //nolint:gosec // paths are under t.TempDir
		if os.IsNotExist(err) {
			facts = append(facts, completionRecoveryFact{})
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		facts = append(facts, completionRecoveryFact{data: data, present: true})
	}
	return facts
}

func writeCompletionRecoveryFact(t *testing.T, path, state string, prior, exact []byte) {
	t.Helper()
	if state == "absent" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var data []byte
	switch state {
	case "prior":
		data = prior
	case "candidate", "exact":
		data = exact
	case "wrong":
		data = []byte("third data")
	default:
		t.Fatalf("unknown recovery fact %q", state)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
