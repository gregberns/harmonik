package runmerge_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runmerge"
)

// The two pre-rebase cleanup steps destroy uncommitted work on purpose: a
// rebase refuses to start on a dirty worktree, so the churn revert and the
// untracked clean are what let a finished run land at all. What these tests
// hold is that neither one destroys anything in silence. Each writes what it is
// about to remove to a recovery artifact under the PROJECT directory — never
// the run worktree, which the run's own teardown deletes — warns on stderr, and
// names the loss in an event.
//
// Beads: hk-nqvqr (churn edits), hk-4q6ah (untracked files).

// recordingEmitter keeps every event a merge step emits so a test can ask what
// the daemon told the operator, not just what it did to the disk.
type recordingEmitter struct {
	mu     sync.Mutex
	events map[core.EventType][]byte
}

func newRecordingEmitter() *recordingEmitter {
	return &recordingEmitter{events: map[core.EventType][]byte{}}
}

func (e *recordingEmitter) Emit(_ context.Context, evType core.EventType, payload []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events[evType] = payload
	return nil
}

func (e *recordingEmitter) EmitWithRunID(ctx context.Context, _ core.RunID, evType core.EventType, payload []byte) error {
	return e.Emit(ctx, evType, payload)
}

func (e *recordingEmitter) payload(t *testing.T, evType core.EventType, into any) {
	t.Helper()
	e.mu.Lock()
	raw, ok := e.events[evType]
	e.mu.Unlock()
	if !ok {
		t.Fatalf("no %s event was emitted; the step destroyed work without naming it", evType)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("unmarshal %s payload: %v\n%s", evType, err, raw)
	}
}

func (e *recordingEmitter) emitted(evType core.EventType) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.events[evType]
	return ok
}

// discardChurnInTest runs the churn revert with a throwaway recovery
// destination and a discarding bus. It is for the tests whose subject is the
// revert rather than the rescue; the rescue has its own tests below.
func discardChurnInTest(t *testing.T, wtPath string) {
	t.Helper()
	runmerge.DiscardDirtyChurn(context.Background(), wtPath, t.TempDir(),
		newResidualRunID(t), discardingEmitter{}, core.BeadID("hk-churn"))
}

// cleanUntrackedInTest is the same arrangement for the untracked clean.
func cleanUntrackedInTest(t *testing.T, wtPath string) {
	t.Helper()
	runmerge.CleanUntrackedFiles(context.Background(), wtPath, t.TempDir(),
		newResidualRunID(t), discardingEmitter{}, core.BeadID("hk-clean"))
}

// recoveryArtifacts lists the file names under the project's recovery
// directory. An absent directory is no artifacts, not a failure — that is the
// answer the "nothing was destroyed" tests are asking for.
func recoveryArtifacts(t *testing.T, projectDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(projectDir, ".harmonik", "recovery"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read recovery dir: %v", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// sortBySequence orders artifacts by the -N suffix writeRecoveryArtifact gives
// them, as numbers.
//
// sort.Strings would order them as text, which agrees with the write order only
// while the count stays under ten. A caller that pairs the i-th artifact with
// the i-th edit would then pair them wrongly, and the failure would look like a
// defect in the code under test.
func sortBySequence(t *testing.T, artifacts []string) {
	t.Helper()
	seq := func(name string) int {
		base := strings.TrimSuffix(name, ".patch")
		n, err := strconv.Atoi(base[strings.LastIndex(base, "-")+1:])
		if err != nil {
			t.Fatalf("recovery artifact %q does not end in a -N sequence: %v", name, err)
		}
		return n
	}
	sort.Slice(artifacts, func(i, j int) bool { return seq(artifacts[i]) < seq(artifacts[j]) })
}

func readWorktreeFile(t *testing.T, wtPath, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(wtPath, rel)) //nolint:gosec // G304: path built from a test temp dir
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(raw)
}

func readRecoveryArtifact(t *testing.T, projectDir, name string) string {
	t.Helper()
	path := filepath.Join(projectDir, ".harmonik", "recovery", name)
	raw, err := os.ReadFile(path) //nolint:gosec // G304: path built from a test temp dir
	if err != nil {
		t.Fatalf("read recovery artifact %s: %v", path, err)
	}
	return string(raw)
}

// TestDiscardDirtyChurn_WritesRecoveryPatchBeforeReverting is the hk-nqvqr
// regression.
//
// An implementer edits a tracked churn path — .claude/ and .harmonik/ are both
// on the allowlist, and .harmonik/agents/_skills/ is the skill copy an agent is
// REQUIRED to mirror into — and never commits it. The pre-rebase revert put the
// committed content back and said nothing, so the edit was gone with no record
// anywhere.
//
// The revert stays: the rebase needs a clean worktree. What must also happen is
// that the unstaged delta reaches a patch under the project directory first,
// and that an event names the path.
//
// RED→GREEN: against a revert that only checks out, no artifact exists and no
// event is emitted.
func TestDiscardDirtyChurn_WritesRecoveryPatchBeforeReverting(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)
	projectDir := t.TempDir()
	runID := newResidualRunID(t)
	bus := newRecordingEmitter()

	const authored = "MERGE-HOOK-THE-IMPLEMENTER-AUTHORED"
	churnPath := filepath.Join(".claude", "settings.json")
	writeFile(t, filepath.Join(wtPath, churnPath), `{"hooks":{"note":"`+authored+`"}}`+"\n")

	runmerge.DiscardDirtyChurn(context.Background(), wtPath, projectDir, runID, bus, core.BeadID("hk-nqvqr"))

	artifacts := recoveryArtifacts(t, projectDir)
	if len(artifacts) != 1 {
		t.Fatalf("the revert destroyed an uncommitted edit and left %d recovery artifact(s): %v", len(artifacts), artifacts)
	}
	patch := readRecoveryArtifact(t, projectDir, artifacts[0])
	if !strings.Contains(patch, authored) {
		t.Errorf("the recovery patch does not hold the edit the revert destroyed; patch:\n%s", patch)
	}
	if !strings.Contains(patch, ".claude/settings.json") {
		t.Errorf("the recovery patch does not name the path it saves; patch:\n%s", patch)
	}

	// The revert still has to happen, or the rebase that follows aborts.
	restored := dirtyLedgerGit(t, wtPath, "status", "--porcelain")
	if restored != "" {
		t.Errorf("the churn path was not reverted; git status:\n%s", restored)
	}

	var pl core.RunWorktreeChurnEditsDiscardedPayload
	bus.payload(t, core.EventTypeRunWorktreeChurnEditsDiscarded, &pl)
	if !pl.Valid() {
		t.Errorf("the emitted payload does not satisfy its own Valid(): %+v", pl)
	}
	if len(pl.Paths) != 1 || pl.Paths[0] != ".claude/settings.json" {
		t.Errorf("the event must name the path whose edit was destroyed; got %v", pl.Paths)
	}
	if pl.RecoveryPatch == "" {
		t.Errorf("the event must point at the recovery patch; got an empty path")
	}
	if !strings.HasPrefix(pl.RecoveryPatch, projectDir) {
		t.Errorf("the recovery patch must live under the project dir %q, not the run worktree; got %q",
			projectDir, pl.RecoveryPatch)
	}
	// The projectDir prefix on its own proves little. In production the run
	// worktree sits UNDER the project dir, so a path can satisfy that check and
	// still be deleted by the run's own teardown. This is the assertion that
	// carries the requirement; it passes here only because the artifact is
	// really outside the worktree, not because the fixture uses two temp dirs.
	if strings.HasPrefix(pl.RecoveryPatch, wtPath) {
		t.Errorf("the recovery patch is inside the run worktree %q, which teardown deletes; got %q",
			wtPath, pl.RecoveryPatch)
	}
}

// TestDiscardDirtyChurn_NoPatchWhenNothingDirty holds the other half: the save
// only fires over work that is really about to be destroyed. A worktree whose
// only dirty path is the implementer's own (not churn) loses nothing to the
// revert, so writing a patch there would be noise an operator learns to ignore.
func TestDiscardDirtyChurn_NoPatchWhenNothingDirty(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)
	projectDir := t.TempDir()
	bus := newRecordingEmitter()

	// Dirty, but not churn: the revert leaves it alone (the hk-i1n7j safety
	// property), so nothing is destroyed and nothing needs saving.
	writeFile(t, filepath.Join(wtPath, "code.txt"), "code\nagent work\nuncommitted iteration\n")

	runmerge.DiscardDirtyChurn(context.Background(), wtPath, projectDir, newResidualRunID(t), bus, core.BeadID("hk-nqvqr"))

	if artifacts := recoveryArtifacts(t, projectDir); len(artifacts) != 0 {
		t.Errorf("nothing was destroyed, so nothing should be saved; got artifacts: %v", artifacts)
	}
	if bus.emitted(core.EventTypeRunWorktreeChurnEditsDiscarded) {
		t.Errorf("nothing was destroyed, so no discard event should be emitted")
	}
	if status := dirtyLedgerGit(t, wtPath, "status", "--porcelain"); !strings.Contains(status, "code.txt") {
		t.Errorf("the implementer's own uncommitted work must survive the revert; git status:\n%s", status)
	}
}

// TestDiscardDirtyChurn_NoPatchWhenChurnEditIsStaged pins the comparison the
// save uses. `git checkout -- <path>` restores from the INDEX, so a churn edit
// that is already STAGED survives the revert untouched and nothing is
// destroyed. `git diff` says exactly that; `git diff HEAD` would claim the save
// rescued something the revert never removed, and an operator who reads a
// recovery patch for work that is still there stops reading them.
func TestDiscardDirtyChurn_NoPatchWhenChurnEditIsStaged(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)
	projectDir := t.TempDir()
	bus := newRecordingEmitter()

	const staged = "STAGED-AND-THEREFORE-SAFE"
	churnPath := filepath.Join(".claude", "settings.json")
	writeFile(t, filepath.Join(wtPath, churnPath), `{"hooks":{"note":"`+staged+`"}}`+"\n")
	dirtyLedgerGit(t, wtPath, "add", ".claude/settings.json")

	runmerge.DiscardDirtyChurn(context.Background(), wtPath, projectDir, newResidualRunID(t), bus, core.BeadID("hk-nqvqr"))

	if artifacts := recoveryArtifacts(t, projectDir); len(artifacts) != 0 {
		t.Errorf("a staged churn edit survives the revert, so nothing should be saved; got artifacts: %v", artifacts)
	}
	if bus.emitted(core.EventTypeRunWorktreeChurnEditsDiscarded) {
		t.Errorf("a staged churn edit survives the revert, so no discard event should be emitted")
	}
	if got := readWorktreeFile(t, wtPath, churnPath); !strings.Contains(got, staged) {
		t.Errorf("the staged edit must survive the revert; file holds:\n%s", got)
	}
}

// TestCleanUntrackedFiles_RescuesUntrackedFileBeforeDeleting is the hk-4q6ah
// regression.
//
// By the time `git clean -fd` runs, CommitResidualDelta has committed every
// untracked non-ignored file its pathspec allowed it to stage. The files that
// survive are the ones it EXCLUDES — .claude/ and .harmonik/, excluded because
// a .claude/ file can hold a credential (hk-igq3). They were deleted with no
// record at all.
//
// The delete stays: the rebase aborts on an untracked file a replayed commit
// would overwrite. What must also happen is that the file's content reaches a
// recovery artifact under the project directory first.
//
// RED→GREEN: against a bare `git clean -fd`, no artifact exists.
func TestCleanUntrackedFiles_RescuesUntrackedFileBeforeDeleting(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)
	projectDir := t.TempDir()
	bus := newRecordingEmitter()

	const authored = "LOCAL-OVERRIDE-THE-CLEAN-WOULD-EAT"
	if err := os.MkdirAll(filepath.Join(wtPath, ".claude"), 0o750); err != nil {
		t.Fatalf("MkdirAll .claude: %v", err)
	}
	doomed := filepath.Join(wtPath, ".claude", "settings.local.json")
	writeFile(t, doomed, `{"note":"`+authored+`"}`+"\n")

	runmerge.CleanUntrackedFiles(context.Background(), wtPath, projectDir, newResidualRunID(t), bus, core.BeadID("hk-4q6ah"))

	artifacts := recoveryArtifacts(t, projectDir)
	if len(artifacts) != 1 {
		t.Fatalf("the clean deleted an untracked file and left %d recovery artifact(s): %v", len(artifacts), artifacts)
	}
	rescued := readRecoveryArtifact(t, projectDir, artifacts[0])
	if !strings.Contains(rescued, authored) {
		t.Errorf("the recovery artifact does not hold the content of the file the clean deleted; artifact:\n%s", rescued)
	}
	if !strings.Contains(rescued, ".claude/settings.local.json") {
		t.Errorf("the recovery artifact does not name the file it saves; artifact:\n%s", rescued)
	}

	// The clean still has to happen, or the rebase that follows aborts.
	if _, statErr := os.Stat(doomed); statErr == nil {
		t.Errorf("the untracked file survived the clean; the rebase that follows would abort on it")
	}

	var pl core.RunWorktreeUntrackedFilesRemovedPayload
	bus.payload(t, core.EventTypeRunWorktreeUntrackedFilesRemoved, &pl)
	if !pl.Valid() {
		t.Errorf("the emitted payload does not satisfy its own Valid(): %+v", pl)
	}
	if len(pl.Paths) != 1 || pl.Paths[0] != ".claude/settings.local.json" {
		t.Errorf("the event must name the file the clean deleted; got %v", pl.Paths)
	}
	if !strings.HasPrefix(pl.RecoveryPatch, projectDir) {
		t.Errorf("the recovery artifact must live under the project dir %q, not the run worktree; got %q",
			projectDir, pl.RecoveryPatch)
	}
}

// TestCleanUntrackedFiles_IgnoredFileNotRescued pins the boundary the rescue
// shares with the clean. `git clean -fd` does not delete a gitignored file, so
// the rescue must not copy one either: a rescue wider than the delete would
// park build output and daemon runtime state in the recovery directory on every
// merge, and a .harmonik/ read that way can hold a credential.
func TestCleanUntrackedFiles_IgnoredFileNotRescued(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)
	projectDir := t.TempDir()
	bus := newRecordingEmitter()

	writeFile(t, filepath.Join(wtPath, ".gitignore"), "junk.log\n")
	dirtyLedgerGit(t, wtPath, "add", ".gitignore")
	dirtyLedgerGit(t, wtPath, "commit", "-m", "add gitignore")

	ignored := filepath.Join(wtPath, "junk.log")
	writeFile(t, ignored, "i am ignored runtime junk\n")

	runmerge.CleanUntrackedFiles(context.Background(), wtPath, projectDir, newResidualRunID(t), bus, core.BeadID("hk-4q6ah"))

	if artifacts := recoveryArtifacts(t, projectDir); len(artifacts) != 0 {
		t.Errorf("a gitignored file is not deleted, so it must not be rescued; got artifacts: %v", artifacts)
	}
	if bus.emitted(core.EventTypeRunWorktreeUntrackedFilesRemoved) {
		t.Errorf("nothing was deleted, so no removal event should be emitted")
	}
	if _, statErr := os.Stat(ignored); statErr != nil {
		t.Errorf("the gitignored file must survive the clean untouched: %v", statErr)
	}
}

// TestRunBranchToTarget_FailedRescueDoesNotStopTheMerge holds the constraint
// that separates these two steps from CommitResidualDelta, which MUST fail the
// merge.
//
// These two only add a RECORD. The revert and the clean still run, the rebase
// still starts, and the run still lands. A merge that failed because a rescue
// failed would be a new outage on a path that worked before the rescue existed.
//
// The failure injected here is a real one: a regular file where the recovery
// DIRECTORY has to be, so the MkdirAll cannot succeed. Nothing is stubbed.
func TestRunBranchToTarget_FailedRescueDoesNotStopTheMerge(t *testing.T) {
	t.Parallel()

	projectDir, wtPath, runID := residualMergeSetup(t)

	// Block both rescues: a file cannot be a directory.
	if err := os.MkdirAll(filepath.Join(projectDir, ".harmonik"), 0o750); err != nil {
		t.Fatalf("MkdirAll .harmonik: %v", err)
	}
	blocker := filepath.Join(projectDir, ".harmonik", "recovery")
	writeFile(t, blocker, "not a directory\n")

	// A dirty tracked churn path, so the churn save runs.
	if err := os.MkdirAll(filepath.Join(wtPath, ".beads"), 0o750); err != nil {
		t.Fatalf("MkdirAll .beads: %v", err)
	}
	ledger := filepath.Join(wtPath, ".beads", "issues.jsonl")
	writeFile(t, ledger, `{"id":"a","status":"open"}`+"\n")
	dirtyLedgerGit(t, wtPath, "add", ".beads/issues.jsonl")
	dirtyLedgerGit(t, wtPath, "commit", "-m", "commit the ledger")
	writeFile(t, ledger, `{"id":"a","status":"closed"}`+"\n")

	// An untracked excluded file, so the untracked rescue runs.
	if err := os.MkdirAll(filepath.Join(wtPath, ".claude"), 0o750); err != nil {
		t.Fatalf("MkdirAll .claude: %v", err)
	}
	doomed := filepath.Join(wtPath, ".claude", "settings.local.json")
	writeFile(t, doomed, `{"localOverride":true}`+"\n")

	bus := newRecordingEmitter()
	out := runmerge.RunBranchToTarget(
		context.Background(),
		nil, // nil Submit → runmerge.InlineSubmit
		projectDir,
		runID,
		bus,
		core.BeadID("hk-4q6ah"),
		"",     // headSHA — unset, so the no-change guard does not fire
		"main", // targetBranch
		nil,    // protectBranches
		"",     // brPath — disables the bead-ledger sync step
	)

	if !out.Success {
		t.Fatalf("a rescue that could not write its artifact stopped the merge; reason: %q", out.Reason)
	}

	// The blocker is still a file: proof the rescues really did fail rather
	// than quietly succeeding somewhere else.
	if info, statErr := os.Stat(blocker); statErr != nil || info.IsDir() {
		t.Fatalf("the injected failure did not hold; the test measured nothing (stat err %v)", statErr)
	}

	// Both destroying steps still ran.
	if _, statErr := os.Stat(doomed); statErr == nil {
		t.Errorf("the clean did not run after its rescue failed; the rebase would abort on the untracked file")
	}

	// The report MUST NOT depend on the file. A cleanup that destroyed work and
	// saved none of it is the case an operator most needs to hear about, so a
	// failed artifact has to make the event MORE important, not absent. Without
	// this the loss is invisible: no artifact, no event, nothing on the bus.
	var churn core.RunWorktreeChurnEditsDiscardedPayload
	bus.payload(t, core.EventTypeRunWorktreeChurnEditsDiscarded, &churn)
	if !churn.Valid() {
		t.Errorf("the churn event must still be emitted when its artifact failed; got %+v", churn)
	}
	if churn.RecoveryPatch != "" {
		t.Errorf("no artifact was written, so recovery_patch must be empty; got %q", churn.RecoveryPatch)
	}

	var untracked core.RunWorktreeUntrackedFilesRemovedPayload
	bus.payload(t, core.EventTypeRunWorktreeUntrackedFilesRemoved, &untracked)
	if !untracked.Valid() {
		t.Errorf("the untracked event must still be emitted when its artifact failed; got %+v", untracked)
	}
	if untracked.RecoveryPatch != "" {
		t.Errorf("no artifact was written, so recovery_patch must be empty; got %q", untracked.RecoveryPatch)
	}
}

// applyRecoveryArtifact applies a churn recovery patch to a worktree with
// `git apply -3` and returns the error, if any.
//
// -3, not a plain apply: the churn patch's base is the run worktree's INDEX,
// not HEAD, so a plain apply usually fails on context. Three-way needs only the
// `index <sha>..<sha>` blobs the patch names, and those live in the shared
// object store.
func applyRecoveryArtifact(t *testing.T, wtPath, projectDir, name string) error {
	t.Helper()
	path := filepath.Join(projectDir, ".harmonik", "recovery", name)
	//nolint:gosec // G204: test fixture runs the literal git binary with fixed subcommand and flags; the only variable is a t.TempDir path.
	cmd := exec.CommandContext(t.Context(), "git", "apply", "-3", path)
	cmd.Dir = wtPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

// TestDiscardDirtyChurn_RecoveryPatchActuallyApplies is the assertion the rest
// of this file was missing.
//
// Every other test here asks whether the patch CONTAINS the destroyed text. A
// patch can contain the text and still not apply, and that is not a hypothetical
// distinction — it is exactly the state the append-mode artifact was in. Restore
// the content and compare it, and the question is answered rather than approached.
func TestDiscardDirtyChurn_RecoveryPatchActuallyApplies(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)
	projectDir := t.TempDir()

	const authored = "MERGE-HOOK-THE-IMPLEMENTER-AUTHORED"
	churnPath := filepath.Join(".claude", "settings.json")
	body := `{"hooks":{"note":"` + authored + `"}}` + "\n"
	writeFile(t, filepath.Join(wtPath, churnPath), body)

	runmerge.DiscardDirtyChurn(context.Background(), wtPath, projectDir,
		newResidualRunID(t), newRecordingEmitter(), core.BeadID("hk-nqvqr"))

	if got := readWorktreeFile(t, wtPath, churnPath); strings.Contains(got, authored) {
		t.Fatalf("precondition: the revert did not run, so this test proves nothing; got %q", got)
	}

	artifacts := recoveryArtifacts(t, projectDir)
	if len(artifacts) != 1 {
		t.Fatalf("want exactly 1 recovery artifact, got %d: %v", len(artifacts), artifacts)
	}
	if err := applyRecoveryArtifact(t, wtPath, projectDir, artifacts[0]); err != nil {
		t.Fatalf("the recovery patch does not apply, so it recovers nothing: %v", err)
	}
	if got := readWorktreeFile(t, wtPath, churnPath); got != body {
		t.Errorf("applying the recovery patch did not restore the destroyed edit\n got: %q\nwant: %q", got, body)
	}
}

// TestDiscardDirtyChurn_EachInvocationGetsItsOwnApplicablePatch is the
// regression for the append-mode defect.
//
// One run reaches the pre-rebase cleanup more than once — prepareInitialMerge,
// then once per push or non-fast-forward retry in prepareRebase — and the churn
// paths re-dirty in between, which is the whole reason they are on the churn
// allowlist. When both patches went into ONE file, the second `git diff` for
// the same path had the same index base as the first, so the concatenation did
// not apply. `git apply` is atomic, so a retried merge produced an artifact that
// restored NOTHING while the event still advertised it as the rescue.
//
// RED→GREEN: against append mode this fails on the second apply with
// "patch does not apply".
func TestDiscardDirtyChurn_EachInvocationGetsItsOwnApplicablePatch(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)
	projectDir := t.TempDir()
	runID := newResidualRunID(t)
	churnPath := filepath.Join(".claude", "settings.json")

	first := `{"hooks":{"note":"FIRST-EDIT"}}` + "\n"
	writeFile(t, filepath.Join(wtPath, churnPath), first)
	runmerge.DiscardDirtyChurn(context.Background(), wtPath, projectDir, runID,
		newRecordingEmitter(), core.BeadID("hk-nqvqr"))

	// The retry: same run, same path, dirty again with different content.
	second := `{"hooks":{"note":"SECOND-EDIT"}}` + "\n"
	writeFile(t, filepath.Join(wtPath, churnPath), second)
	runmerge.DiscardDirtyChurn(context.Background(), wtPath, projectDir, runID,
		newRecordingEmitter(), core.BeadID("hk-nqvqr"))

	artifacts := recoveryArtifacts(t, projectDir)
	if len(artifacts) != 2 {
		t.Fatalf("two cleanups destroyed two edits and must leave two artifacts; got %d: %v",
			len(artifacts), artifacts)
	}

	// Each one must apply on its own and restore the edit it saved. Applying
	// them in either order is not the claim; that one file recovers one loss is.
	sortBySequence(t, artifacts)
	for i, want := range []string{first, second} {
		// Back to the committed state before each apply. Both patches were cut
		// against that same base, so this is the state an operator recovers
		// into. `git reset --hard` and not the churn revert: a three-way apply
		// touches the index, and reverting from a moved index would change the
		// base out from under the next patch and fail the test for a reason
		// that has nothing to do with the artifacts.
		dirtyLedgerGit(t, wtPath, "reset", "--hard")

		name := artifacts[i]
		if err := applyRecoveryArtifact(t, wtPath, projectDir, name); err != nil {
			t.Fatalf("recovery artifact %s does not apply, so that cleanup recovered nothing: %v", name, err)
		}
		if got := readWorktreeFile(t, wtPath, churnPath); got != want {
			t.Errorf("artifact %s restored %q, want %q", name, got, want)
		}
	}
}

// TestWriteRecoveryArtifact_IsOwnerReadOnly holds the permission bit.
//
// rescueUntrackedFiles does NOT filter by path, and the untracked files that
// reach it are by construction the ones the merge pathspec excluded — which is
// how .claude/settings.local.json, holding a plaintext auth token, gets copied
// into a recovery artifact. Operator-readable has to mean readable by the owner.
func TestWriteRecoveryArtifact_IsOwnerReadOnly(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)
	projectDir := t.TempDir()
	writeFile(t, filepath.Join(wtPath, ".claude", "settings.json"), `{"hooks":{"note":"SECRET"}}`+"\n")

	runmerge.DiscardDirtyChurn(context.Background(), wtPath, projectDir,
		newResidualRunID(t), newRecordingEmitter(), core.BeadID("hk-nqvqr"))

	artifacts := recoveryArtifacts(t, projectDir)
	if len(artifacts) != 1 {
		t.Fatalf("want exactly 1 recovery artifact, got %d: %v", len(artifacts), artifacts)
	}
	info, err := os.Stat(filepath.Join(projectDir, ".harmonik", "recovery", artifacts[0]))
	if err != nil {
		t.Fatalf("stat recovery artifact: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("a recovery artifact can hold a plaintext auth token; mode = %04o, want 0600", perm)
	}
}

// applyRecoveryArtifactPlain applies an artifact with plain `git apply`, no
// three-way merge.
//
// The untracked rescue is a set of add-this-file patches against /dev/null, so
// it needs no base and plain `git apply` is what the payload documentation
// tells an operator to run. Using -3 here would prove a different claim than
// the one the type makes.
func applyRecoveryArtifactPlain(t *testing.T, wtPath, projectDir, name string) error {
	t.Helper()
	path := filepath.Join(projectDir, ".harmonik", "recovery", name)
	//nolint:gosec // G204: test fixture runs the literal git binary with fixed subcommand and flags; the only variable is a t.TempDir path.
	cmd := exec.CommandContext(t.Context(), "git", "apply", path)
	cmd.Dir = wtPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, out)
	}
	return nil
}

// TestCleanUntrackedFiles_RescueActuallyRestoresEveryFile is the untracked half
// of the apply assertion.
//
// The sibling churn test proves a patch that CONTAINS the text can still fail to
// apply. That gap was real once and it is not closed for this half by a
// strings.Contains check. The shapes differ in every way that matters: these are
// `--no-index --binary` add-file patches, several of them concatenated into one
// artifact, applied with plain `git apply` rather than -3.
//
// The three files are chosen to break it if it is breakable: one ordinary text
// file, one holding bytes no text patch survives, and one whose name carries a
// space and a double quote.
func TestCleanUntrackedFiles_RescueActuallyRestoresEveryFile(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)
	projectDir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(wtPath, ".claude"), 0o750); err != nil {
		t.Fatalf("MkdirAll .claude: %v", err)
	}
	binary := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, '\n', 0x00, 'z'}
	want := map[string][]byte{
		filepath.Join(".claude", "settings.local.json"): []byte(`{"note":"LOCAL-OVERRIDE"}` + "\n"),
		"payload.bin":         binary,
		`a file "quoted".txt`: []byte("odd name, real content\n"),
	}
	for rel, body := range want {
		if err := os.WriteFile(filepath.Join(wtPath, rel), body, 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	runmerge.CleanUntrackedFiles(context.Background(), wtPath, projectDir,
		newResidualRunID(t), newRecordingEmitter(), core.BeadID("hk-4q6ah"))

	for rel := range want {
		if _, err := os.Stat(filepath.Join(wtPath, rel)); err == nil {
			t.Fatalf("precondition: the clean did not delete %s, so this test proves nothing", rel)
		}
	}

	artifacts := recoveryArtifacts(t, projectDir)
	if len(artifacts) != 1 {
		t.Fatalf("want exactly 1 recovery artifact, got %d: %v", len(artifacts), artifacts)
	}
	if err := applyRecoveryArtifactPlain(t, wtPath, projectDir, artifacts[0]); err != nil {
		t.Fatalf("the untracked rescue does not apply, so it recovers nothing: %v", err)
	}

	for rel, body := range want {
		got, err := os.ReadFile(filepath.Join(wtPath, rel)) //nolint:gosec // G304: the path is a test-authored name under t.TempDir.
		if err != nil {
			t.Errorf("applying the rescue did not restore %s: %v", rel, err)
			continue
		}
		if !bytes.Equal(got, body) {
			t.Errorf("applying the rescue restored %s with the wrong bytes\n got: %q\nwant: %q", rel, got, body)
		}
	}
}

// TestCleanUntrackedFiles_NamesTheFilesItCouldNotSave is the regression for an
// event that reported a save it did not make.
//
// When the rescue cannot read one of the doomed files it skips that file and
// keeps going, which is right — the other files are still worth saving. What was
// wrong is that the event then listed the unreadable file in Paths beside a
// non-empty RecoveryPatch, and that patch does not hold it. A reader had no way
// to tell a full rescue from a partial one, which is the exact failure this
// whole event exists to prevent.
//
// RED→GREEN: before UnsavedPaths existed, the payload named both files and
// pointed at a patch holding one.
func TestCleanUntrackedFiles_NamesTheFilesItCouldNotSave(t *testing.T) {
	t.Parallel()

	wtPath := dirtyLedgerSetup(t)
	projectDir := t.TempDir()
	bus := newRecordingEmitter()

	readable := filepath.Join(wtPath, "readable.txt")
	writeFile(t, readable, "THIS-ONE-IS-SAVED\n")
	locked := filepath.Join(wtPath, "locked.txt")
	writeFile(t, locked, "THIS-ONE-IS-LOST\n")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatalf("chmod locked.txt: %v", err)
	}
	// TempDir removal needs the mode back. The clean normally deletes the file
	// first, so a "no such file" here is the expected path, not a problem.
	t.Cleanup(func() {
		if err := os.Chmod(locked, 0o600); err != nil && !os.IsNotExist(err) {
			t.Errorf("restore mode on locked.txt: %v", err)
		}
	})

	runmerge.CleanUntrackedFiles(context.Background(), wtPath, projectDir,
		newResidualRunID(t), bus, core.BeadID("hk-4q6ah"))

	var pl core.RunWorktreeUntrackedFilesRemovedPayload
	bus.payload(t, core.EventTypeRunWorktreeUntrackedFilesRemoved, &pl)

	if len(pl.UnsavedPaths) != 1 || pl.UnsavedPaths[0] != "locked.txt" {
		t.Fatalf("the event must name the file the rescue could not read; Paths=%v UnsavedPaths=%v RecoveryPatch=%q",
			pl.Paths, pl.UnsavedPaths, pl.RecoveryPatch)
	}
	if len(pl.Paths) != 2 {
		t.Errorf("the clean deletes both files, so both belong in Paths; got %v", pl.Paths)
	}
	artifacts := recoveryArtifacts(t, projectDir)
	if len(artifacts) != 1 {
		t.Fatalf("want exactly 1 recovery artifact, got %d: %v", len(artifacts), artifacts)
	}
	rescued := readRecoveryArtifact(t, projectDir, artifacts[0])
	if !strings.Contains(rescued, "THIS-ONE-IS-SAVED") {
		t.Errorf("one unreadable file must not stop the others being saved; artifact:\n%s", rescued)
	}
	if strings.Contains(rescued, "THIS-ONE-IS-LOST") {
		t.Errorf("the artifact holds content the test believes was unreadable, so it measures nothing")
	}
}
