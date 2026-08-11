package dispatchstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
)

const (
	testQueueID      = "0197d100-0000-7000-8000-000000000001"
	testRunID        = "0197d100-0000-7000-8000-000000000002"
	testTransitionID = "0197d100-0000-7000-8000-000000000003"
)

func testIntent(phase dispatch.Phase) dispatch.Intent {
	runID := core.RunID(uuid.MustParse(testRunID))
	intent := dispatch.Intent{
		SchemaVersion: 1,
		Phase:         phase,
		Binding: dispatch.Binding{
			QueueID:           testQueueID,
			QueueName:         "main",
			GroupIndex:        2,
			ItemIndex:         3,
			BeadID:            "hk-dispatch",
			RunID:             runID,
			ClaimTransitionID: core.TransitionID(uuid.MustParse(testTransitionID)),
		},
	}
	if phase == dispatch.PhaseRunDurable || phase == dispatch.PhaseHandoffDurable {
		intent.Run = &dispatch.RunBinding{RecordRunID: runID}
	}
	if phase == dispatch.PhaseHandoffDurable {
		intent.Handoff = &dispatch.HandoffBinding{
			SessionName:        "harmonik-run-0197d100",
			WorktreeLeaseRunID: runID,
		}
	}
	return intent
}

func TestStoreCreateAdvanceListAndRemove(t *testing.T) {
	store := New(t.TempDir())
	prepared := testIntent(dispatch.PhasePrepared)
	if err := store.Create(prepared); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(prepared); err != nil {
		t.Fatalf("exact replay Create() = %v", err)
	}
	claim := testIntent(dispatch.PhaseClaimDurable)
	if err := store.Advance(prepared, claim); err != nil {
		t.Fatal(err)
	}
	run := testIntent(dispatch.PhaseRunDurable)
	if err := store.Advance(claim, run); err != nil {
		t.Fatal(err)
	}
	handoff := testIntent(dispatch.PhaseHandoffDurable)
	if err := store.Advance(run, handoff); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(handoff.Binding.RunID)
	if err != nil || loaded.Phase != dispatch.PhaseHandoffDurable {
		t.Fatalf("Load() = (%q, %v)", loaded.Phase, err)
	}
	listed, err := store.List()
	if err != nil || len(listed) != 1 || listed[0].Binding.RunID != handoff.Binding.RunID {
		t.Fatalf("List() = (%+v, %v)", listed, err)
	}
	if err := store.Remove(handoff); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(handoff.Binding.RunID); err == nil {
		t.Fatal("Load() after Remove = nil error")
	}
}

func TestStoreAdvancesPreparedIntentToClaimRefused(t *testing.T) {
	store := New(t.TempDir())
	prepared := testIntent(dispatch.PhasePrepared)
	if err := store.Create(prepared); err != nil {
		t.Fatal(err)
	}
	refused := prepared
	refused.Phase = dispatch.PhaseClaimRefused
	refused.Refusal = &dispatch.ClaimRefusalBinding{Cause: dispatch.ClaimRefusalDependency}
	if err := store.Advance(prepared, refused); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(prepared.Binding.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Phase != dispatch.PhaseClaimRefused || loaded.Refusal == nil || loaded.Refusal.Cause != dispatch.ClaimRefusalDependency {
		t.Fatalf("loaded = %+v", loaded)
	}
}

func TestStoreRemovesOnlyExactReplayTerminalPhases(t *testing.T) {
	for _, tc := range []struct {
		name   string
		intent func(t *testing.T, store *Store) dispatch.Intent
	}{
		{name: "prepared compensation", intent: func(t *testing.T, store *Store) dispatch.Intent {
			t.Helper()
			prepared := testIntent(dispatch.PhasePrepared)
			if err := store.Create(prepared); err != nil {
				t.Fatal(err)
			}
			return prepared
		}},
		{name: "durable refusal compensation", intent: func(t *testing.T, store *Store) dispatch.Intent {
			t.Helper()
			prepared := testIntent(dispatch.PhasePrepared)
			if err := store.Create(prepared); err != nil {
				t.Fatal(err)
			}
			refused, err := prepared.WithClaimRefused(dispatch.ClaimRefusalDependency)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Advance(prepared, refused); err != nil {
				t.Fatal(err)
			}
			return refused
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := New(t.TempDir())
			intent := tc.intent(t, store)
			if err := store.Remove(intent); err != nil {
				t.Fatalf("Remove() = %v", err)
			}
			if _, err := store.Load(intent.Binding.RunID); err == nil {
				t.Fatal("Load() after Remove = nil error")
			}
		})
	}
}

func TestStoreRefusesRemovalWhileSuccessReplayCanAdvance(t *testing.T) {
	store := New(t.TempDir())
	prepared := testIntent(dispatch.PhasePrepared)
	if err := store.Create(prepared); err != nil {
		t.Fatal(err)
	}
	claim := testIntent(dispatch.PhaseClaimDurable)
	if err := store.Advance(prepared, claim); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(claim); err == nil {
		t.Fatal("Remove(claim_durable) = nil error")
	}
	if _, err := store.Load(claim.Binding.RunID); err != nil {
		t.Fatalf("active intent changed: %v", err)
	}
	run := testIntent(dispatch.PhaseRunDurable)
	if err := store.Advance(claim, run); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(run); err == nil {
		t.Fatal("Remove(run_durable) = nil error")
	}
	loaded, err := store.Load(run.Binding.RunID)
	if err != nil || loaded.Phase != dispatch.PhaseRunDurable {
		t.Fatalf("run_durable intent changed: (%q, %v)", loaded.Phase, err)
	}
}

func TestStoreClaimRefusedCannotRejoinSuccessPath(t *testing.T) {
	store := New(t.TempDir())
	prepared := testIntent(dispatch.PhasePrepared)
	if err := store.Create(prepared); err != nil {
		t.Fatal(err)
	}
	refused := prepared
	refused.Phase = dispatch.PhaseClaimRefused
	refused.Refusal = &dispatch.ClaimRefusalBinding{Cause: dispatch.ClaimRefusalDependency}
	if err := store.Advance(prepared, refused); err != nil {
		t.Fatal(err)
	}
	for _, next := range []dispatch.Intent{
		testIntent(dispatch.PhaseClaimDurable),
		testIntent(dispatch.PhaseRunDurable),
		testIntent(dispatch.PhaseHandoffDurable),
	} {
		if err := store.Advance(refused, next); err == nil {
			t.Fatalf("claim_refused advanced to %q", next.Phase)
		}
	}
}

func TestStoreAdvanceAndRemoveRequireExactBytes(t *testing.T) {
	store := New(t.TempDir())
	prepared := testIntent(dispatch.PhasePrepared)
	if err := store.Create(prepared); err != nil {
		t.Fatal(err)
	}
	changed := prepared
	changed.Binding.BeadID = "hk-other"
	if err := store.Advance(changed, testIntent(dispatch.PhaseClaimDurable)); err == nil {
		t.Fatal("Advance with changed predecessor = nil")
	}
	if err := store.Remove(testIntent(dispatch.PhaseHandoffDurable)); err == nil {
		t.Fatal("Remove with different durable phase = nil")
	}
	loaded, err := store.Load(prepared.Binding.RunID)
	if err != nil || loaded.Phase != dispatch.PhasePrepared {
		t.Fatalf("durable predecessor changed: (%q, %v)", loaded.Phase, err)
	}
}

func TestStoreAdvanceKeepsEveryDurableBinding(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*dispatch.Intent)
	}{
		{name: "queue", mutate: func(i *dispatch.Intent) { i.Binding.QueueID = "0197d100-0000-7000-8000-000000000099" }},
		{name: "location", mutate: func(i *dispatch.Intent) { i.Binding.ItemIndex++ }},
		{name: "bead", mutate: func(i *dispatch.Intent) { i.Binding.BeadID = "hk-other" }},
		{name: "claim", mutate: func(i *dispatch.Intent) {
			i.Binding.ClaimTransitionID = core.TransitionID(uuid.MustParse("0197d100-0000-7000-8000-000000000099"))
		}},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			prior := testIntent(dispatch.PhasePrepared)
			next := testIntent(dispatch.PhaseClaimDurable)
			tc.mutate(&next)
			if _, err := prepareAdvance(prior, next); err == nil {
				t.Fatal("prepareAdvance() = nil error")
			}
		})
	}
	prior := testIntent(dispatch.PhaseRunDurable)
	next := testIntent(dispatch.PhaseHandoffDurable)
	next.Run.RecordRunID = core.RunID(uuid.MustParse("0197d100-0000-7000-8000-000000000099"))
	if _, err := prepareAdvance(prior, next); err == nil {
		t.Fatal("prepareAdvance() changed durable run binding")
	}
}

func TestStoreListFailsClosedOnIntentShapedEntries(t *testing.T) {
	tests := []struct {
		name  string
		write func(t *testing.T, root string)
	}{
		{name: "corrupt", write: func(t *testing.T, root string) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(root, testRunID+intentSuffix), []byte("{"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "directory", write: func(t *testing.T, root string) {
			t.Helper()
			if err := os.Mkdir(filepath.Join(root, testRunID+intentSuffix), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "wrong path identity", write: func(t *testing.T, root string) {
			t.Helper()
			data, err := json.Marshal(testIntent(dispatch.PhasePrepared))
			if err != nil {
				t.Fatal(err)
			}
			name := "0197d100-0000-7000-8000-000000000099.json"
			if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			root := filepath.Join(projectDir, ".harmonik", "dispatch-intents")
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			tc.write(t, root)
			if _, err := New(projectDir).List(); err == nil {
				t.Fatal("List() = nil error")
			}
		})
	}
}

func TestStoreRejectsSymlinkRoot(t *testing.T) {
	projectDir := t.TempDir()
	external := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectDir, ".harmonik"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(projectDir, ".harmonik", "dispatch-intents")); err != nil {
		t.Fatal(err)
	}
	if _, err := New(projectDir).List(); err == nil {
		t.Fatal("List() with symlink root = nil")
	}
	entries, err := os.ReadDir(external)
	if err != nil || len(entries) != 0 {
		t.Fatalf("external target changed: (%v, %v)", entries, err)
	}
}

func TestStoreRejectsSymlinkHarmonikParent(t *testing.T) {
	projectDir := t.TempDir()
	external := t.TempDir()
	if err := os.Mkdir(filepath.Join(external, "dispatch-intents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(projectDir, ".harmonik")); err != nil {
		t.Fatal(err)
	}
	store := New(projectDir)
	if _, err := store.List(); err == nil {
		t.Fatal("List() with symlink parent = nil")
	}
	if err := store.Create(testIntent(dispatch.PhasePrepared)); err == nil {
		t.Fatal("Create() with symlink parent = nil")
	}
	entries, err := os.ReadDir(filepath.Join(external, "dispatch-intents"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("external target changed: (%v, %v)", entries, err)
	}
}

func TestStoreClassifiesParentSyncAmbiguityAtEachMutation(t *testing.T) {
	errSync := errors.New("cut parent sync")
	projectDir := t.TempDir()
	root := filepath.Join(projectDir, ".harmonik", "dispatch-intents")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store := New(projectDir)
	realSync := store.ops.syncDir
	store.ops.syncDir = func(path string) error {
		if path == root {
			return errSync
		}
		return realSync(path)
	}
	prepared := testIntent(dispatch.PhasePrepared)
	var ambiguous *AmbiguousError
	if err := store.Create(prepared); !errors.As(err, &ambiguous) {
		t.Fatalf("Create() error = %v", err)
	}
	store.ops.syncDir = realSync
	loaded, err := store.Load(prepared.Binding.RunID)
	if err != nil || loaded.Phase != dispatch.PhasePrepared {
		t.Fatalf("prepared reload = (%q, %v)", loaded.Phase, err)
	}

	store.ops.syncDir = func(path string) error {
		if path == root {
			return errSync
		}
		return realSync(path)
	}
	claim := testIntent(dispatch.PhaseClaimDurable)
	ambiguous = nil
	if err := store.Advance(prepared, claim); !errors.As(err, &ambiguous) {
		t.Fatalf("Advance() error = %v", err)
	}
	store.ops.syncDir = realSync
	loaded, err = store.Load(claim.Binding.RunID)
	if err != nil || loaded.Phase != dispatch.PhaseClaimDurable {
		t.Fatalf("claim reload = (%q, %v)", loaded.Phase, err)
	}
	if err := store.Advance(claim, testIntent(dispatch.PhaseRunDurable)); err != nil {
		t.Fatal(err)
	}
	run := testIntent(dispatch.PhaseRunDurable)
	handoff := testIntent(dispatch.PhaseHandoffDurable)
	if err := store.Advance(run, handoff); err != nil {
		t.Fatal(err)
	}
	store.ops.syncDir = func(path string) error {
		if path == root {
			return errSync
		}
		return realSync(path)
	}
	ambiguous = nil
	if err := store.Remove(handoff); !errors.As(err, &ambiguous) {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, testRunID+intentSuffix)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("removed intent stat error = %v", err)
	}
}

func TestStoreCreateLinkFailureLeavesNoCanonicalOrTemp(t *testing.T) {
	projectDir := t.TempDir()
	root := filepath.Join(projectDir, ".harmonik", "dispatch-intents")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store := New(projectDir)
	store.ops.link = func(string, string) error { return errors.New("cut link") }
	if err := store.Create(testIntent(dispatch.PhasePrepared)); err == nil {
		t.Fatal("Create() = nil")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("root entries = (%v, %v)", entries, err)
	}
}

func TestStoreConvergesSideEffectThenError(t *testing.T) {
	projectDir := t.TempDir()
	store := New(projectDir)
	realLink, realRename, realRemove := store.ops.link, store.ops.rename, store.ops.remove
	store.ops.link = func(oldPath, newPath string) error {
		if err := realLink(oldPath, newPath); err != nil {
			return err
		}
		return errors.New("link reported failure after install")
	}
	prepared := testIntent(dispatch.PhasePrepared)
	if err := store.Create(prepared); err != nil {
		t.Fatalf("Create() did not converge: %v", err)
	}
	store.ops.link = realLink

	store.ops.rename = func(oldPath, newPath string) error {
		if err := realRename(oldPath, newPath); err != nil {
			return err
		}
		return errors.New("rename reported failure after replace")
	}
	claim := testIntent(dispatch.PhaseClaimDurable)
	if err := store.Advance(prepared, claim); err != nil {
		t.Fatalf("Advance() did not converge: %v", err)
	}
	store.ops.rename = realRename
	if err := store.Advance(claim, testIntent(dispatch.PhaseRunDurable)); err != nil {
		t.Fatal(err)
	}
	if err := store.Advance(testIntent(dispatch.PhaseRunDurable), testIntent(dispatch.PhaseHandoffDurable)); err != nil {
		t.Fatal(err)
	}

	store.ops.remove = func(path string) error {
		if err := realRemove(path); err != nil {
			return err
		}
		return errors.New("remove reported failure after unlink")
	}
	if err := store.Remove(testIntent(dispatch.PhaseHandoffDurable)); err != nil {
		t.Fatalf("Remove() did not converge: %v", err)
	}
	store.ops.remove = realRemove
	if err := store.Remove(testIntent(dispatch.PhaseHandoffDurable)); err != nil {
		t.Fatalf("absent Remove() retry did not converge: %v", err)
	}
}

func TestStoreConvergenceSyncErrorsStayTyped(t *testing.T) {
	projectDir := t.TempDir()
	store := New(projectDir)
	prepared := testIntent(dispatch.PhasePrepared)
	if err := store.Create(prepared); err != nil {
		t.Fatal(err)
	}
	errSync := errors.New("cut convergence sync")
	realSync := store.ops.syncDir
	store.ops.syncDir = func(path string) error {
		if path == store.root() {
			return errSync
		}
		return realSync(path)
	}
	assertAmbiguous := func(name string, err error) {
		t.Helper()
		var ambiguous *AmbiguousError
		if !errors.As(err, &ambiguous) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
	assertAmbiguous("exact create", store.Create(prepared))
	store.ops.syncDir = realSync
	claim := testIntent(dispatch.PhaseClaimDurable)
	if err := store.Advance(prepared, claim); err != nil {
		t.Fatal(err)
	}
	store.ops.syncDir = func(path string) error {
		if path == store.root() {
			return errSync
		}
		return realSync(path)
	}
	assertAmbiguous("already next", store.Advance(prepared, claim))
	store.ops.syncDir = realSync
	if err := store.Advance(claim, testIntent(dispatch.PhaseRunDurable)); err != nil {
		t.Fatal(err)
	}
	if err := store.Advance(testIntent(dispatch.PhaseRunDurable), testIntent(dispatch.PhaseHandoffDurable)); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(testIntent(dispatch.PhaseHandoffDurable)); err != nil {
		t.Fatal(err)
	}
	store.ops.syncDir = func(path string) error {
		if path == store.root() {
			return errSync
		}
		return realSync(path)
	}
	assertAmbiguous("already absent", store.Remove(testIntent(dispatch.PhaseHandoffDurable)))
}

func TestStoreAdvanceCleansTempWhenPredecessorReloadFails(t *testing.T) {
	projectDir := t.TempDir()
	store := New(projectDir)
	prepared := testIntent(dispatch.PhasePrepared)
	if err := store.Create(prepared); err != nil {
		t.Fatal(err)
	}
	realCreateTemp := store.ops.createTemp
	store.ops.createTemp = func(dir, pattern string) (*os.File, error) {
		file, err := realCreateTemp(dir, pattern)
		if err == nil {
			if removeErr := os.Remove(filepath.Join(dir, testRunID+intentSuffix)); removeErr != nil {
				t.Fatal(removeErr)
			}
		}
		return file, err
	}
	if err := store.Advance(prepared, testIntent(dispatch.PhaseClaimDurable)); err == nil {
		t.Fatal("Advance() = nil")
	}
	entries, err := os.ReadDir(store.root())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if filepath.Ext(entry.Name()) != intentSuffix {
			t.Fatalf("temporary file remained: %s", entry.Name())
		}
	}
}
