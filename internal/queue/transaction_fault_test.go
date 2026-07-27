package queue

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func transactionFixtureQueue(t *testing.T, id string) []byte {
	t.Helper()
	q := Queue{
		SchemaVersion: 1,
		QueueID:       id,
		Name:          QueueNameMain,
		Status:        QueueStatusActive,
		Groups:        []Group{},
	}
	data, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func transactionFixturePlan(t *testing.T, projectDir string) ReplacementPlan {
	t.Helper()
	prior := transactionFixtureQueue(t, "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0001")
	candidate := transactionFixtureQueue(t, "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0001")
	var q Queue
	if err := json.Unmarshal(candidate, &q); err != nil {
		t.Fatal(err)
	}
	q.Status = QueueStatusPausedByDrain
	candidate, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	return ReplacementPlan{
		ProjectDir:     projectDir,
		OperationKind:  OperationPause,
		NormalizedName: QueueNameMain,
		QueueID:        q.QueueID,
		PriorBytes:     prior,
		CandidateBytes: candidate,
		WakeRequired:   true,
	}
}

func transactionSeedPrior(t *testing.T, plan ReplacementPlan) {
	t.Helper()
	path := queuePath(plan.ProjectDir, plan.NormalizedName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, plan.PriorBytes, 0o600); err != nil {
		t.Fatal(err)
	}
}

func transactionReadCanonical(t *testing.T, plan ReplacementPlan) []byte {
	t.Helper()
	data, err := os.ReadFile(queuePath(plan.ProjectDir, plan.NormalizedName))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestWriteReplacementFaultCuts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		cut        func(*namespaceOps)
		want       NamespaceOutcome
		wantPrior  bool
		wantIntent bool
	}{
		{
			name: "candidate-create",
			cut: func(ops *namespaceOps) {
				ops.openFile = func(string, int, os.FileMode) (*os.File, error) {
					return nil, errors.New("cut candidate create")
				}
			},
			want:      OutcomeNotCommitted,
			wantPrior: true,
		},
		{
			name: "candidate-write",
			cut: func(ops *namespaceOps) {
				ops.write = func(io.Writer, []byte) error {
					return errors.New("cut candidate write")
				}
			},
			want:      OutcomeNotCommitted,
			wantPrior: true,
		},
		{
			name: "candidate-sync",
			cut: func(ops *namespaceOps) {
				ops.syncFile = func(*os.File) error { return errors.New("cut candidate sync") }
			},
			want:      OutcomeNotCommitted,
			wantPrior: true,
		},
		{
			name: "candidate-close",
			cut: func(ops *namespaceOps) {
				ops.closeFile = func(*os.File) error { return errors.New("cut candidate close") }
			},
			want:      OutcomeNotCommitted,
			wantPrior: true,
		},
		{
			name: "intent-temp-create",
			cut: func(ops *namespaceOps) {
				openFile := ops.openFile
				count := 0
				ops.openFile = func(path string, flags int, mode os.FileMode) (*os.File, error) {
					count++
					if count == 2 {
						return nil, errors.New("cut intent temp create")
					}
					return openFile(path, flags, mode)
				}
			},
			want:      OutcomeNotCommitted,
			wantPrior: true,
		},
		{
			name: "intent-temp-write",
			cut: func(ops *namespaceOps) {
				write := ops.write
				count := 0
				ops.write = func(w io.Writer, data []byte) error {
					count++
					if count == 2 {
						return errors.New("cut intent temp write")
					}
					return write(w, data)
				}
			},
			want:      OutcomeNotCommitted,
			wantPrior: true,
		},
		{
			name: "intent-temp-sync",
			cut: func(ops *namespaceOps) {
				syncFile := ops.syncFile
				count := 0
				ops.syncFile = func(f *os.File) error {
					count++
					if count == 2 {
						return errors.New("cut intent temp sync")
					}
					return syncFile(f)
				}
			},
			want:      OutcomeNotCommitted,
			wantPrior: true,
		},
		{
			name: "intent-temp-close",
			cut: func(ops *namespaceOps) {
				closeFile := ops.closeFile
				count := 0
				ops.closeFile = func(f *os.File) error {
					count++
					if count == 2 {
						return errors.New("cut intent temp close")
					}
					return closeFile(f)
				}
			},
			want:      OutcomeNotCommitted,
			wantPrior: true,
		},
		{
			name: "intent-install",
			cut: func(ops *namespaceOps) {
				ops.link = func(string, string) error { return errors.New("cut intent link") }
			},
			want:      OutcomeNotCommitted,
			wantPrior: true,
		},
		{
			name: "intent-install-ambiguous-exact",
			cut: func(ops *namespaceOps) {
				link := ops.link
				ops.link = func(oldPath, newPath string) error {
					if err := link(oldPath, newPath); err != nil {
						return err
					}
					return errors.New("ambiguous intent link after install")
				}
			},
			want:       OutcomeCommittedDurable,
			wantIntent: true,
		},
		{
			name: "intent-parent-sync",
			cut: func(ops *namespaceOps) {
				ops.syncDir = func(*os.File) error { return errors.New("cut intent parent sync") }
			},
			want:       OutcomeCommitIndeterminate,
			wantPrior:  true,
			wantIntent: true,
		},
		{
			name: "canonical-rename",
			cut: func(ops *namespaceOps) {
				ops.rename = func(string, string) error { return errors.New("cut canonical rename") }
			},
			want:       OutcomeNotCommitted,
			wantPrior:  true,
			wantIntent: true,
		},
		{
			name: "canonical-rename-ambiguous-after-move",
			cut: func(ops *namespaceOps) {
				rename := ops.rename
				ops.rename = func(oldPath, newPath string) error {
					if err := rename(oldPath, newPath); err != nil {
						return err
					}
					return errors.New("ambiguous canonical rename after move")
				}
			},
			want:       OutcomeCommitIndeterminate,
			wantIntent: true,
		},
		{
			name: "canonical-parent-sync",
			cut: func(ops *namespaceOps) {
				count := 0
				ops.syncDir = func(f *os.File) error {
					count++
					if count == 2 {
						return errors.New("cut canonical parent sync")
					}
					return f.Sync()
				}
			},
			want:       OutcomeCommitIndeterminate,
			wantPrior:  false,
			wantIntent: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			projectDir := t.TempDir()
			plan := transactionFixturePlan(t, projectDir)
			transactionSeedPrior(t, plan)
			ops := osNamespaceOps()
			tc.cut(&ops)
			got := writeReplacement(context.Background(), plan, ops)
			if got.Outcome != tc.want {
				t.Fatalf("outcome = %q, want %q (err=%v)", got.Outcome, tc.want, got.Err)
			}
			canonical := transactionReadCanonical(t, plan)
			want := plan.CandidateBytes
			if tc.wantPrior {
				want = plan.PriorBytes
			}
			if !bytes.Equal(canonical, want) {
				t.Fatalf("canonical bytes changed contrary to outcome")
			}
			_, statErr := os.Stat(replaceIntentPath(projectDir, QueueNameMain))
			if tc.wantIntent != (statErr == nil) {
				t.Fatalf("intent presence = %v, want %v (stat=%v)", statErr == nil, tc.wantIntent, statErr)
			}
		})
	}
}

func TestClassifyReplaceIntentExactFacts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		canonical string
		candidate bool
		corrupt   bool
		want      ReplaceRecoveryAction
	}{
		{name: "candidate-canonical", canonical: "candidate", want: ReplacePromoteCanonical},
		{name: "selected-temp-plus-prior", canonical: "prior", candidate: true, want: ReplaceRetryRename},
		{name: "prior-no-temp", canonical: "prior", want: ReplaceNotCommitted},
		{name: "third-canonical", canonical: "third", want: ReplaceRefuse},
		{name: "wrong-selected-temp", canonical: "prior", candidate: true, corrupt: true, want: ReplaceRefuse},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			projectDir := t.TempDir()
			plan := transactionFixturePlan(t, projectDir)
			intent, _, err := prepareReplacement(plan)
			if err != nil {
				t.Fatal(err)
			}
			qDir := queuesDir(projectDir)
			if err := os.MkdirAll(qDir, 0o700); err != nil {
				t.Fatal(err)
			}
			var canonical []byte
			switch tc.canonical {
			case "prior":
				canonical = plan.PriorBytes
			case "candidate":
				canonical = plan.CandidateBytes
			default:
				canonical = transactionFixtureQueue(t, "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0099")
			}
			if err := os.WriteFile(queuePath(projectDir, QueueNameMain), canonical, 0o600); err != nil {
				t.Fatal(err)
			}
			if tc.candidate {
				selected := plan.CandidateBytes
				if tc.corrupt {
					selected = []byte("third bytes")
				}
				if err := os.WriteFile(filepath.Join(qDir, intent.Candidate.TempBasename), selected, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, classifyErr := ClassifyReplaceIntent(projectDir, intent)
			if got != tc.want {
				t.Fatalf("action = %q, want %q (err=%v)", got, tc.want, classifyErr)
			}
		})
	}
}

const (
	transactionFixtureID = "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0010"
	successorFixtureID   = "0190b3c4-8f12-7c4e-9a82-2bf0d4ee0011"
)

func transactionLinkedFixture(
	t *testing.T,
	projectDir string,
) (
	predecessor ReplaceIntentV1,
	predecessorBytes []byte,
	successorBytes []byte,
	facts ArchiveRecoveryFacts,
) {
	t.Helper()
	plan := transactionFixturePlan(t, projectDir)
	plan.ArchiveHandoff = &ArchiveHandoffPlan{
		ArchiveOrigin:       "operator-cancel",
		ArchiveKind:         "cancelled",
		SourceIdentity:      plan.QueueID,
		DestinationBasename: "main.json.cancelled-fixed",
	}
	predecessor, predecessorBytes, err := prepareReplacementWithIDs(
		plan,
		transactionFixtureID,
		successorFixtureID,
	)
	if err != nil {
		t.Fatal(err)
	}
	successorBytes, err = base64Decode(predecessor.ArchiveHandoff.SuccessorArchiveIntentBytesBase64)
	if err != nil {
		t.Fatal(err)
	}
	facts = ArchiveRecoveryFacts{
		NormalizedName:      QueueNameMain,
		SourceIdentity:      plan.QueueID,
		SourceSHA256:        digestHex(plan.CandidateBytes),
		DestinationBasename: "main.json.cancelled-fixed",
	}
	return predecessor, predecessorBytes, successorBytes, facts
}

func base64Decode(value string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(value)
}

func TestLinkedArchiveHandoffExactPair(t *testing.T) {
	t.Parallel()
	predecessor, predecessorBytes, successorBytes, facts := transactionLinkedFixture(t, t.TempDir())

	var rawFields map[string]json.RawMessage
	if err := json.Unmarshal(successorBytes, &rawFields); err != nil {
		t.Fatal(err)
	}
	if len(rawFields) != 9 {
		t.Fatalf("successor field count = %d, want exact v1 count 9", len(rawFields))
	}
	if _, found := rawFields["predecessor_intent_sha256"]; found {
		t.Fatal("successor unexpectedly contains predecessor_intent_sha256")
	}
	if !bytes.Contains(predecessorBytes, []byte(successorFixtureID)) {
		t.Fatal("predecessor bytes do not bind preallocated successor ID")
	}

	if action, err := ClassifyLinkedHandoff(&predecessor, successorBytes, facts); action != LinkedContinuePair || err != nil {
		t.Fatalf("exact pair = (%q, %v), want continue pair", action, err)
	}
	if action, err := ClassifyLinkedHandoff(&predecessor, nil, facts); action != LinkedCreateSuccessor || err != nil {
		t.Fatalf("predecessor-only = (%q, %v), want create successor", action, err)
	}
	if action, err := ClassifyLinkedHandoff(nil, successorBytes, facts); action != LinkedContinueArchive || err != nil {
		t.Fatalf("successor-only = (%q, %v), want continue archive", action, err)
	}
}

func TestLinkedArchiveHandoffRejectsIncompleteAndMismatchedFacts(t *testing.T) {
	t.Parallel()
	predecessor, _, successorBytes, facts := transactionLinkedFixture(t, t.TempDir())

	tests := []struct {
		name        string
		predecessor *ReplaceIntentV1
		successor   []byte
		facts       ArchiveRecoveryFacts
	}{
		{name: "schema-only-successor", successor: []byte(`{"schema_version":1}`), facts: facts},
		{name: "unknown-successor-field", successor: append(append([]byte(nil), successorBytes[:len(successorBytes)-1]...), []byte(`,"extra":true}`)...), facts: facts},
		{name: "noncanonical-successor-bytes", successor: append([]byte(" "), successorBytes...), facts: facts},
		{name: "missing-recovery-source", successor: successorBytes, facts: ArchiveRecoveryFacts{NormalizedName: facts.NormalizedName, SourceSHA256: facts.SourceSHA256, DestinationBasename: facts.DestinationBasename}},
		{name: "changed-source", successor: successorBytes, facts: ArchiveRecoveryFacts{NormalizedName: facts.NormalizedName, SourceIdentity: "third-source", SourceSHA256: facts.SourceSHA256, DestinationBasename: facts.DestinationBasename}},
		{name: "changed-source-digest", successor: successorBytes, facts: ArchiveRecoveryFacts{NormalizedName: facts.NormalizedName, SourceIdentity: facts.SourceIdentity, SourceSHA256: digestHex([]byte("third")), DestinationBasename: facts.DestinationBasename}},
		{name: "third-destination", successor: successorBytes, facts: ArchiveRecoveryFacts{NormalizedName: facts.NormalizedName, SourceIdentity: facts.SourceIdentity, SourceSHA256: facts.SourceSHA256, DestinationBasename: "main.json.cancelled-third"}},
		{name: "destination-path-escape", successor: successorBytes, facts: ArchiveRecoveryFacts{NormalizedName: facts.NormalizedName, SourceIdentity: facts.SourceIdentity, SourceSHA256: facts.SourceSHA256, DestinationBasename: "../outside"}},
		{name: "changed-normalized-name", successor: successorBytes, facts: ArchiveRecoveryFacts{NormalizedName: "other", SourceIdentity: facts.SourceIdentity, SourceSHA256: facts.SourceSHA256, DestinationBasename: facts.DestinationBasename}},
		{name: "pair-bytes-not-bound", predecessor: &predecessor, successor: append([]byte(nil), successorBytes[:len(successorBytes)-1]...), facts: facts},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			action, err := ClassifyLinkedHandoff(tc.predecessor, tc.successor, tc.facts)
			if action != LinkedRefuse || err == nil {
				t.Fatalf("classification = (%q, %v), want refuse with error", action, err)
			}
		})
	}
}

func TestSuccessorOnlyRejectsEverySchemaFactMutation(t *testing.T) {
	t.Parallel()
	_, _, successorBytes, facts := transactionLinkedFixture(t, t.TempDir())
	var valid ArchiveIntentV1
	if err := json.Unmarshal(successorBytes, &valid); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(*ArchiveIntentV1)
	}{
		{name: "schema-version", mutate: func(v *ArchiveIntentV1) { v.SchemaVersion++ }},
		{name: "archive-intent-id", mutate: func(v *ArchiveIntentV1) { v.ArchiveIntentID = "not-a-uuid" }},
		{name: "predecessor-transaction-id", mutate: func(v *ArchiveIntentV1) { v.PredecessorTransactionID = "not-a-uuid" }},
		{name: "archive-origin", mutate: func(v *ArchiveIntentV1) { v.ArchiveOrigin = "unknown" }},
		{name: "archive-kind", mutate: func(v *ArchiveIntentV1) { v.ArchiveKind = "unknown" }},
		{name: "normalized-name", mutate: func(v *ArchiveIntentV1) { v.NormalizedName = "other" }},
		{name: "source-identity", mutate: func(v *ArchiveIntentV1) { v.SourceIdentity = "third-source" }},
		{name: "source-sha256", mutate: func(v *ArchiveIntentV1) { v.SourceSHA256 = digestHex([]byte("third")) }},
		{name: "destination-basename", mutate: func(v *ArchiveIntentV1) { v.DestinationBasename = "main.json.cancelled-third" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mutated := valid
			tc.mutate(&mutated)
			data, err := json.Marshal(mutated)
			if err != nil {
				t.Fatal(err)
			}
			action, classifyErr := ClassifyLinkedHandoff(nil, data, facts)
			if action != LinkedRefuse || classifyErr == nil {
				t.Fatalf("mutated successor = (%q, %v), want refuse", action, classifyErr)
			}
		})
	}
}

func TestInstallBoundArchiveIntentConsumesExactBytes(t *testing.T) {
	t.Parallel()
	projectDir := t.TempDir()
	predecessor, _, successorBytes, _ := transactionLinkedFixture(t, projectDir)

	got := InstallBoundArchiveIntent(projectDir, predecessor)
	if got.Outcome != OutcomeCommittedDurable {
		t.Fatalf("install outcome = %q (err=%v)", got.Outcome, got.Err)
	}
	path := archiveIntentPath(projectDir, QueueNameMain)
	installed, err := os.ReadFile(path) //nolint:gosec // path is test-owned t.TempDir data
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(installed, successorBytes) {
		t.Fatal("installed successor differs from predecessor-bound bytes")
	}
	if got = InstallBoundArchiveIntent(projectDir, predecessor); got.Outcome != OutcomeCommittedDurable {
		t.Fatalf("exact reinstall outcome = %q (err=%v)", got.Outcome, got.Err)
	}

	if err := os.WriteFile(path, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got = InstallBoundArchiveIntent(projectDir, predecessor)
	if got.Outcome != OutcomeNotCommitted || got.Err == nil {
		t.Fatalf("changed existing outcome = (%q, %v), want not committed", got.Outcome, got.Err)
	}
	unchanged, err := os.ReadFile(path) //nolint:gosec // path is test-owned t.TempDir data
	if err != nil {
		t.Fatal(err)
	}
	if string(unchanged) != `{"schema_version":1}` {
		t.Fatal("changed existing successor was overwritten")
	}
}

func TestInstallBoundArchiveIntentFaultCuts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		cut         func(*namespaceOps)
		want        NamespaceOutcome
		wantPresent bool
	}{
		{
			name: "queue-directory-create",
			cut: func(ops *namespaceOps) {
				ops.mkdirAll = func(string, os.FileMode) error { return errors.New("cut mkdir") }
			},
			want: OutcomeNotCommitted,
		},
		{
			name: "successor-temp-create",
			cut: func(ops *namespaceOps) {
				ops.openFile = func(string, int, os.FileMode) (*os.File, error) {
					return nil, errors.New("cut create")
				}
			},
			want: OutcomeNotCommitted,
		},
		{
			name: "successor-temp-write",
			cut: func(ops *namespaceOps) {
				ops.write = func(io.Writer, []byte) error { return errors.New("cut write") }
			},
			want: OutcomeNotCommitted,
		},
		{
			name: "successor-temp-sync",
			cut: func(ops *namespaceOps) {
				ops.syncFile = func(*os.File) error { return errors.New("cut file sync") }
			},
			want: OutcomeNotCommitted,
		},
		{
			name: "successor-temp-close",
			cut: func(ops *namespaceOps) {
				ops.closeFile = func(*os.File) error { return errors.New("cut file close") }
			},
			want: OutcomeNotCommitted,
		},
		{
			name: "successor-no-replace-install",
			cut: func(ops *namespaceOps) {
				ops.link = func(string, string) error { return errors.New("cut install") }
			},
			want: OutcomeNotCommitted,
		},
		{
			name: "successor-parent-sync",
			cut: func(ops *namespaceOps) {
				ops.syncDir = func(*os.File) error { return errors.New("cut parent sync") }
			},
			want:        OutcomeCommitIndeterminate,
			wantPresent: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			projectDir := t.TempDir()
			predecessor, _, successorBytes, _ := transactionLinkedFixture(t, projectDir)
			ops := osNamespaceOps()
			tc.cut(&ops)

			got := installBoundArchiveIntent(projectDir, predecessor, ops)
			if got.Outcome != tc.want || got.Err == nil {
				t.Fatalf("outcome = (%q, %v), want %q with error", got.Outcome, got.Err, tc.want)
			}
			data, err := os.ReadFile(archiveIntentPath(projectDir, QueueNameMain))
			present := err == nil
			if present != tc.wantPresent {
				t.Fatalf("successor presence = %v, want %v (err=%v)", present, tc.wantPresent, err)
			}
			if present && !bytes.Equal(data, successorBytes) {
				t.Fatal("fault cut left successor bytes other than exact predecessor binding")
			}
		})
	}
}
