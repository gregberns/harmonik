// Package core — EM-INV-005 git-wins-on-completion-disagreement sensor (hk-b3f.65).
//
// EM-INV-005 (execution-model.md §5): "If Beads reports a bead as `closed` but
// no merge commit with `Harmonik-Bead-ID` matching that bead exists in the
// project's git history, OR if a transition event in JSONL references a
// checkpoint commit that does not exist in git, the divergence MUST be treated
// as a reconciliation flag and NOT silently auto-reconciled.  Every subsystem
// that observes this class of divergence MUST route it through
// [reconciliation/spec.md §8.4 Cat 3] and [beads-integration.md §4.7 BI-022].
// No subsystem may silently prefer Beads or JSONL over git."
//
// This file exercises two synthetic divergence scenarios and asserts that the
// correct cross-spec invariant phrasing is encoded in both specs, locking the
// contract against spec drift that would silently un-anchor the invariant.
//
// Scenario A — Beads-closed / no-merge-commit divergence
//
//	A bead has CoarseStatus=closed in Beads, but git holds no merge commit
//	carrying the matching Harmonik-Bead-ID trailer.  This is the primary
//	"git wins on completion" scenario from EM-INV-005 and BI-022.
//
//	Expected outcome: divergence classified as Cat 3 reconciliation flag.
//	Forbidden outcome: silent Beads-side correction (BI-022 explicitly forbids
//	silently auto-reconciling in git's direction).
//
// Scenario B — JSONL references missing checkpoint commit
//
//	A JSONL transition event carries a checkpoint commit SHA that does not
//	exist in git's object database.  This is the second divergence arm of
//	EM-INV-005.
//
//	Expected outcome: divergence classified as Cat 3 reconciliation flag (or
//	Cat 6b per RC-014 / reconciliation/spec.md §8.11 when git's object DB is
//	itself non-traversable).  Forbidden outcome: silent reconciliation toward
//	the JSONL record.
//
// Because the Cat 3 classifier is not yet implemented, the tests assert on
// spec content (anchoring the invariant phrasing) and on the synthetic
// divergence scenarios at the type level.  Concrete behavioral assertions
// against the classifier will replace or extend these marker tests when the
// classifier lands.
//
// Requirement-traceable bead: hk-b3f.65.
package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ──────────────────────────────────────────────────────────────────────────────
// Scenario helpers
// ──────────────────────────────────────────────────────────────────────────────

// gitWinsSensorMakeClosedBead returns a BeadRecord with CoarseStatus=closed
// for the given bead ID.  This is the "Beads says closed" side of Scenario A.
func gitWinsSensorMakeClosedBead(t *testing.T, id BeadID) BeadRecord {
	t.Helper()
	return BeadRecord{
		BeadID:        id,
		Title:         "sensor-bead-closed-no-merge-commit",
		Description:   "synthetic closed bead with no matching git merge commit",
		BeadType:      "task",
		Status:        CoarseStatusClosed,
		Edges:         nil,
		AuditTrailRef: "audit-gitwins-sensor-b3f65",
	}
}

// gitWinsSensorMakeCheckpointWithMissingCommit returns a Checkpoint whose
// CommitHash represents a SHA that is absent from git's object database.
// This is the "JSONL-referenced-but-missing-from-git" side of Scenario B.
func gitWinsSensorMakeCheckpointWithMissingCommit(t *testing.T, id BeadID) Checkpoint {
	t.Helper()
	beadID := id
	runID := RunID(uuid.Must(uuid.NewV7()))
	transitionID := TransitionID(uuid.Must(uuid.NewV7()))
	return Checkpoint{
		// A plausible-looking but deliberately absent commit SHA.
		CommitHash:           "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		RunID:                runID,
		StateID:              StateID(uuid.Must(uuid.NewV7())),
		TransitionID:         transitionID,
		BeadID:               &beadID,
		SchemaVersion:        1,
		TransitionRecordPath: TransitionRecordPath(runID, transitionID),
	}
}

// gitWinsSensorScenarioADivergence captures the synthetic Scenario A state:
// a bead that Beads reports as closed but for which no matching git merge commit
// exists.  The MergeCommitFound field mirrors the absent-from-git outcome.
type gitWinsSensorScenarioADivergence struct {
	Bead             BeadRecord
	BeadID           BeadID
	MergeCommitFound bool // false = divergence present; git has no matching commit
}

// gitWinsSensorBuildScenarioA constructs the Scenario A divergence state.
// MergeCommitFound=false models the "git says no merge commit" side.
func gitWinsSensorBuildScenarioA(t *testing.T) gitWinsSensorScenarioADivergence {
	t.Helper()
	id := BeadID("sensor-bead-gitwins-b3f65-scenA")
	bead := gitWinsSensorMakeClosedBead(t, id)
	return gitWinsSensorScenarioADivergence{
		Bead:             bead,
		BeadID:           id,
		MergeCommitFound: false, // divergence: Beads=closed, git=no merge commit
	}
}

// gitWinsSensorScenarioBDivergence captures the synthetic Scenario B state:
// a JSONL transition event that references a checkpoint commit absent from git.
type gitWinsSensorScenarioBDivergence struct {
	Checkpoint        Checkpoint
	CommitExistsInGit bool // false = divergence present; git does not have this SHA
}

// gitWinsSensorBuildScenarioB constructs the Scenario B divergence state.
// CommitExistsInGit=false models the "JSONL references a SHA not in git" outcome.
func gitWinsSensorBuildScenarioB(t *testing.T) gitWinsSensorScenarioBDivergence {
	t.Helper()
	id := BeadID("sensor-bead-gitwins-b3f65-scenB")
	cp := gitWinsSensorMakeCheckpointWithMissingCommit(t, id)
	return gitWinsSensorScenarioBDivergence{
		Checkpoint:        cp,
		CommitExistsInGit: false, // divergence: JSONL has SHA that git does not
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Spec-text readers
// ──────────────────────────────────────────────────────────────────────────────

// gitWinsSensorEmInv005Content reads specs/execution-model.md and returns the
// paragraph containing EM-INV-005.  Fails the test if the anchor is absent.
func gitWinsSensorEmInv005Content(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("gitWinsSensorEmInv005Content: runtime.Caller failed")
	}
	// Walk up: internal/core/<file> → repo root
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	specPath := filepath.Join(repoRoot, "specs", "execution-model.md")

	//nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("gitWinsSensorEmInv005Content: cannot read %s: %v", specPath, err)
	}
	content := string(raw)

	const anchor = "EM-INV-005"
	idx := strings.Index(content, anchor)
	if idx < 0 {
		t.Fatalf("EM-INV-005 anchor not found in %s; invariant may have been removed or renamed", specPath)
	}
	para := content[idx:]
	// Clip at the next section header so we don't bleed into unrelated content.
	if end := strings.Index(para, "\n####"); end > 0 {
		para = para[:end]
	}
	return para
}

// gitWinsSensorBi022Content reads specs/beads-integration.md and returns the
// paragraph containing BI-022.  Fails the test if the anchor is absent.
func gitWinsSensorBi022Content(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("gitWinsSensorBi022Content: runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	specPath := filepath.Join(repoRoot, "specs", "beads-integration.md")

	//nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("gitWinsSensorBi022Content: cannot read %s: %v", specPath, err)
	}
	content := string(raw)

	const anchor = "BI-022"
	idx := strings.Index(content, anchor)
	if idx < 0 {
		t.Fatalf("BI-022 anchor not found in %s; invariant may have been removed or renamed", specPath)
	}
	para := content[idx:]
	if end := strings.Index(para, "\n####"); end > 0 {
		para = para[:end]
	}
	return para
}

// gitWinsSensorRcCat3Content reads specs/reconciliation/spec.md and returns the
// paragraph containing Cat 3 store-disagreement detection rule.  Fails the test
// if the anchor is absent.
func gitWinsSensorRcCat3Content(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("gitWinsSensorRcCat3Content: runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	specPath := filepath.Join(repoRoot, "specs", "reconciliation", "spec.md")

	//nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("gitWinsSensorRcCat3Content: cannot read %s: %v", specPath, err)
	}
	content := string(raw)

	// The Cat 3 section header in reconciliation/spec.md §8.4.
	const anchor = "Cat 3 — Store disagreement"
	idx := strings.Index(content, anchor)
	if idx < 0 {
		t.Fatalf("Cat 3 section anchor %q not found in %s", anchor, specPath)
	}
	para := content[idx:]
	if end := strings.Index(para, "\n###"); end > 0 {
		para = para[:end]
	}
	return para
}

// ──────────────────────────────────────────────────────────────────────────────
// Spec-invariant phrase tests
// ──────────────────────────────────────────────────────────────────────────────

// TestGitWinsB3F65_EmInv005PhrasesCat3AndNoSilentReconcile verifies that
// execution-model.md §5 EM-INV-005 encodes the mandatory phrases for the
// "git wins on completion disagreement" invariant.
//
// Required phrases:
//   - "Cat 3"              — required classification category
//   - "Harmonik-Bead-ID"  — the git trailer that is proof of completion
//   - "silently auto-reconciled" — the forbidden action (exact canonical phrase)
//   - "BI-022"            — the cross-spec anchor in beads-integration.md
//
// Renaming any of these in the spec is a breaking change and MUST be accompanied
// by a corresponding update to this test.
//
// Requirement-traceable bead: hk-b3f.65.
func TestGitWinsB3F65_EmInv005PhrasesCat3AndNoSilentReconcile(t *testing.T) {
	t.Parallel()

	para := gitWinsSensorEmInv005Content(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "Cat 3",
			hint:   "EM-INV-005 MUST require Cat 3 routing; renaming breaks hk-b3f.65 invariant",
		},
		{
			phrase: "Harmonik-Bead-ID",
			hint:   "EM-INV-005 MUST name the Harmonik-Bead-ID trailer as the completion-proof mechanism",
		},
		{
			phrase: "silently auto-reconciled",
			hint:   "EM-INV-005 MUST forbid silent auto-reconcile; exact phrase anchors no-silent-overwrite contract",
		},
		{
			phrase: "BI-022",
			hint:   "EM-INV-005 MUST cross-reference BI-022 (beads-integration.md §4.7) for the completion-authority rule",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(para, tc.phrase) {
			t.Errorf("EM-INV-005 paragraph missing %q — %s\nParagraph:\n%s", tc.phrase, tc.hint, para)
		}
	}
}

// TestGitWinsB3F65_EmInv005CoversJSONLArm verifies that EM-INV-005 encodes
// BOTH arms of the "git wins" invariant:
//
//  1. Beads-closed / no-merge-commit arm — primary scenario
//  2. JSONL-references-missing-commit arm — secondary scenario
//
// The second arm is distinctive: EM-INV-005 must call out JSONL separately so
// that detectors reading JSONL for divergence evidence cannot silently accept
// a missing commit as authoritative.
//
// Requirement-traceable bead: hk-b3f.65.
func TestGitWinsB3F65_EmInv005CoversJSONLArm(t *testing.T) {
	t.Parallel()

	para := gitWinsSensorEmInv005Content(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			// The primary (Beads-closed) arm.
			phrase: "Beads reports a bead as `closed`",
			hint:   "EM-INV-005 primary arm: Beads-closed-no-merge-commit must be named explicitly",
		},
		{
			// The secondary (JSONL-references-missing-commit) arm.
			phrase: "JSONL",
			hint:   "EM-INV-005 must cover the JSONL-references-missing-commit arm so detectors cannot silently accept JSONL as authoritative",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(para, tc.phrase) {
			t.Errorf("EM-INV-005 paragraph missing %q — %s\nParagraph:\n%s", tc.phrase, tc.hint, para)
		}
	}
}

// TestGitWinsB3F65_Bi022PhrasesAutoReconcileForbidden verifies that
// beads-integration.md §4.7 BI-022 encodes the auto-reconcile prohibition with
// the required canonical phrases.
//
// Required phrases:
//   - "silently auto-reconciled" — canonical phrasing of the forbidden action
//   - "Cat 3"                   — required routing for the divergence
//   - "Harmonik-Bead-ID"        — completion-proof mechanism
//
// Requirement-traceable bead: hk-b3f.65.
func TestGitWinsB3F65_Bi022PhrasesAutoReconcileForbidden(t *testing.T) {
	t.Parallel()

	para := gitWinsSensorBi022Content(t)

	cases := []struct {
		phrase string
		hint   string
	}{
		{
			phrase: "silently auto-reconciled",
			hint:   "BI-022 must forbid silent auto-reconcile with this exact phrase",
		},
		{
			phrase: "Cat 3",
			hint:   "BI-022 must name Cat 3 as the required routing for the divergence",
		},
		{
			phrase: "Harmonik-Bead-ID",
			hint:   "BI-022 must cite the Harmonik-Bead-ID trailer as the completion-proof mechanism",
		},
	}

	for _, tc := range cases {
		if !strings.Contains(para, tc.phrase) {
			t.Errorf("BI-022 paragraph missing %q — %s\nParagraph:\n%s", tc.phrase, tc.hint, para)
		}
	}
}

// TestGitWinsB3F65_RcCat3IsInvestigatorDispatched verifies that
// reconciliation/spec.md §8.4 Cat 3 declares the investigator-dispatch default
// response and does NOT list this category as an auto-resolver.
//
// Per EM-INV-005 the divergence MUST NOT be silently auto-reconciled; the RC
// spec enforces this by requiring an investigator workflow for Cat 3 generic.
//
// Requirement-traceable bead: hk-b3f.65.
func TestGitWinsB3F65_RcCat3IsInvestigatorDispatched(t *testing.T) {
	t.Parallel()

	para := gitWinsSensorRcCat3Content(t)

	// Cat 3 MUST declare an investigator workflow (NOT an auto-resolver).
	if !strings.Contains(para, "investigator") {
		t.Errorf(
			"reconciliation/spec.md Cat 3 section does not mention 'investigator'; "+
				"the EM-INV-005 / BI-022 no-silent-reconcile contract requires Cat 3 to dispatch "+
				"an investigator rather than auto-resolving\nParagraph:\n%s",
			para,
		)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Scenario A — Beads-closed / no-merge-commit (structural assertions)
// ──────────────────────────────────────────────────────────────────────────────

// TestGitWinsB3F65_ScenarioA_BeadsClosedNoMergeCommitIsDivergence constructs
// the Scenario A divergence state and asserts that:
//
//  1. The bead record is structurally valid (well-formed BeadRecord).
//  2. The bead's status is CoarseStatusClosed.
//  3. MergeCommitFound=false, modelling the absent-from-git side of the
//     divergence.
//  4. The divergence struct is a valid carrier for Cat 3 routing; specifically,
//     a subsystem observing (Status=closed AND MergeCommitFound=false) MUST NOT
//     auto-correct Beads — it MUST route to Cat 3 per BI-022.
//
// The Cat 3 classifier is not yet implemented; step 4 is enforced here by
// asserting the divergence preconditions rather than calling the classifier.
// When the classifier lands, this test SHOULD be extended with a concrete
// dispatch assertion.
//
// Requirement-traceable bead: hk-b3f.65.
func TestGitWinsB3F65_ScenarioA_BeadsClosedNoMergeCommitIsDivergence(t *testing.T) {
	t.Parallel()

	scen := gitWinsSensorBuildScenarioA(t)

	// Step 1: bead record is structurally valid.
	if !scen.Bead.Valid() {
		t.Fatal("ScenarioA: BeadRecord.Valid() = false; fixture must be structurally valid")
	}

	// Step 2: Beads side says closed.
	if scen.Bead.Status != CoarseStatusClosed {
		t.Errorf("ScenarioA: Bead.Status = %q, want %q (Beads-closed arm of EM-INV-005)",
			scen.Bead.Status, CoarseStatusClosed)
	}

	// Step 3: git has no matching merge commit — divergence is present.
	if scen.MergeCommitFound {
		t.Error("ScenarioA: MergeCommitFound = true; fixture must model the absent-merge-commit divergence arm")
	}

	// Step 4: preconditions for Cat 3 routing are met.
	// A subsystem observing (Status=closed AND NOT MergeCommitFound) MUST route to
	// Cat 3 per EM-INV-005 / BI-022; it MUST NOT silently correct Beads.
	//
	// We assert that the scenario ID is non-empty (carrier is valid) and that the
	// BeadID matches between the bead record and the scenario, confirming the
	// scenario fixture was constructed correctly.
	if scen.BeadID == "" {
		t.Error("ScenarioA: BeadID is empty; cannot identify the divergent bead")
	}
	if scen.Bead.BeadID != scen.BeadID {
		t.Errorf("ScenarioA: Bead.BeadID = %q, want %q", scen.Bead.BeadID, scen.BeadID)
	}

	t.Logf("ScenarioA: divergence confirmed: Bead.Status=%q, MergeCommitFound=%v, BeadID=%q",
		scen.Bead.Status, scen.MergeCommitFound, scen.BeadID)
	t.Log("ScenarioA: required outcome = Cat 3 investigator dispatch (NOT silent Beads-side correction)")
	t.Log("ScenarioA: forbidden outcome = silently auto-reconciled in git's direction (BI-022)")
	t.Log("ScenarioA: Cat 3 classifier not yet implemented; extend with dispatch assertion when it lands (hk-b3f.65)")
}

// TestGitWinsB3F65_ScenarioA_BeadIDPropagatesAcrossCheckpoint verifies that a
// Checkpoint built for Scenario A carries the same BeadID as the BeadRecord.
// This cross-type consistency check anchors the requirement that the bead
// identifier is byte-equal across every harmonik surface when the "closed"
// divergence is detected (per BI-INV-002, cross-referenced by EM-INV-005).
//
// Requirement-traceable bead: hk-b3f.65.
func TestGitWinsB3F65_ScenarioA_BeadIDPropagatesAcrossCheckpoint(t *testing.T) {
	t.Parallel()

	scen := gitWinsSensorBuildScenarioA(t)
	cp := gitWinsSensorMakeCheckpointWithMissingCommit(t, scen.BeadID)

	if cp.BeadID == nil {
		t.Fatal("ScenarioA propagation: Checkpoint.BeadID is nil; want non-nil")
	}
	if *cp.BeadID != scen.BeadID {
		t.Errorf("ScenarioA propagation: Checkpoint.BeadID = %q, want %q (must match BeadRecord.BeadID)",
			*cp.BeadID, scen.BeadID)
	}
	if !cp.Valid() {
		t.Error("ScenarioA propagation: Checkpoint.Valid() = false; fixture must be structurally valid")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Scenario B — JSONL references missing checkpoint commit (structural assertions)
// ──────────────────────────────────────────────────────────────────────────────

// TestGitWinsB3F65_ScenarioB_JSONLMissingCommitIsDivergence constructs the
// Scenario B divergence state and asserts that:
//
//  1. The Checkpoint is structurally valid.
//  2. The CommitHash is non-empty (a JSONL event carries this SHA reference).
//  3. CommitExistsInGit=false, modelling the "SHA absent from git" side.
//  4. The divergence preconditions for Cat 3 routing are present.
//
// When the Cat 3 classifier (or Cat 6b escalator for the JSONL arm) lands, this
// test SHOULD be extended with a dispatch assertion per RC-014 and RC-INV-001.
//
// Requirement-traceable bead: hk-b3f.65.
func TestGitWinsB3F65_ScenarioB_JSONLMissingCommitIsDivergence(t *testing.T) {
	t.Parallel()

	scen := gitWinsSensorBuildScenarioB(t)

	// Step 1: Checkpoint is structurally valid.
	if !scen.Checkpoint.Valid() {
		t.Fatal("ScenarioB: Checkpoint.Valid() = false; fixture must be structurally valid")
	}

	// Step 2: CommitHash is present (this is the SHA a JSONL event would reference).
	if scen.Checkpoint.CommitHash == "" {
		t.Error("ScenarioB: CommitHash is empty; JSONL divergence requires a non-empty SHA reference")
	}

	// Step 3: git does not have this commit — divergence is present.
	if scen.CommitExistsInGit {
		t.Error("ScenarioB: CommitExistsInGit = true; fixture must model the absent-SHA divergence arm")
	}

	// Step 4: BeadID in the checkpoint is non-nil and non-empty.
	if scen.Checkpoint.BeadID == nil {
		t.Fatal("ScenarioB: Checkpoint.BeadID is nil; want non-nil for traceability")
	}
	if *scen.Checkpoint.BeadID == "" {
		t.Error("ScenarioB: Checkpoint.BeadID is empty string; want non-empty")
	}

	t.Logf("ScenarioB: divergence confirmed: CommitHash=%q, CommitExistsInGit=%v, BeadID=%q",
		scen.Checkpoint.CommitHash, scen.CommitExistsInGit, *scen.Checkpoint.BeadID)
	t.Log("ScenarioB: required outcome = Cat 3 (or Cat 6b) reconciliation flag dispatch")
	t.Log("ScenarioB: forbidden outcome = silent reconciliation toward the JSONL record (EM-INV-005)")
	t.Log("ScenarioB: classifier not yet implemented; extend with dispatch assertion when it lands (hk-b3f.65)")
}

// TestGitWinsB3F65_ScenarioB_JSONLMustNotOverrideGit verifies that the
// beads-integration.md BI-023 paragraph (JSONL is observational only) encodes
// the restriction that JSONL MUST NOT override Beads or git.  This is the
// BI-023 complement to BI-022 and enforces the second arm of EM-INV-005.
//
// Required phrases:
//   - "MUST NOT be used to override" or "MUST NOT drive a write" — the
//     forbidden-override rule for JSONL
//   - "BI-023"                    — the section anchor
//
// Requirement-traceable bead: hk-b3f.65.
func TestGitWinsB3F65_ScenarioB_JSONLMustNotOverrideGit(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("TestGitWinsB3F65_ScenarioB_JSONLMustNotOverrideGit: runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	specPath := filepath.Join(repoRoot, "specs", "beads-integration.md")

	//nolint:gosec // G304: path is constructed from runtime.Caller + known relative segments, not user input
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("cannot read %s: %v", specPath, err)
	}
	content := string(raw)

	const anchor = "BI-023"
	idx := strings.Index(content, anchor)
	if idx < 0 {
		t.Fatalf("BI-023 anchor not found in %s; JSONL-observational-only rule may have been removed", specPath)
	}
	para := content[idx:]
	if end := strings.Index(para, "\n####"); end > 0 {
		para = para[:end]
	}

	// BI-023 must forbid JSONL overriding git or Beads.
	const forbiddenPhrase = "MUST NOT"
	if !strings.Contains(para, forbiddenPhrase) {
		t.Errorf(
			"BI-023 paragraph does not contain %q; the JSONL observational-only rule (EM-INV-005 second arm) "+
				"requires an explicit MUST NOT prohibition\nParagraph:\n%s",
			forbiddenPhrase, para,
		)
	}

	// BI-023 must specifically mention the write prohibition.
	const writePhrase = "write"
	if !strings.Contains(strings.ToLower(para), strings.ToLower(writePhrase)) {
		t.Errorf(
			"BI-023 paragraph does not mention 'write'; the JSONL-observational-only rule must "+
				"explicitly forbid JSONL-driven writes back to Beads\nParagraph:\n%s",
			para,
		)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Forward-doc marker for the Cat 3 classifier
// ──────────────────────────────────────────────────────────────────────────────

// TestGitWinsB3F65_ForwardDocMarkerCat3Classifier is a forward-doc marker for
// hk-b3f.65 (EM-INV-005 behavioral sensor).
//
// The Cat 3 classifier (which routes Beads-closed/no-merge-commit and
// JSONL-missing-commit divergences to investigator dispatch) is not yet
// implemented.  When it lands, the implementer SHOULD:
//
//  1. Delete this forward-doc marker, OR
//  2. Replace the t.SkipNow() call with concrete assertions against the
//     classifier — verifying that Scenario A → Cat 3 dispatch and Scenario B →
//     Cat 3 (or Cat 6b) dispatch, and that neither route silently auto-reconciles.
//
// Spec refs: execution-model.md §5 EM-INV-005; beads-integration.md §4.7 BI-022;
// reconciliation/spec.md §8.4 Cat 3; reconciliation/spec.md §8.4 RC-INV-001.
// Requirement-traceable bead: hk-b3f.65.
func TestGitWinsB3F65_ForwardDocMarkerCat3Classifier(t *testing.T) {
	t.Log("EM-INV-005 (hk-b3f.65): git wins on completion disagreement — behavioral sensor.")
	t.Log("")
	t.Log("Scenario A: Beads-closed / no-merge-commit → MUST route to Cat 3 investigator dispatch.")
	t.Log("Scenario B: JSONL-references-missing-commit → MUST route to Cat 3 (or Cat 6b) flag.")
	t.Log("")
	t.Log("Forbidden in both scenarios: silent auto-reconciliation in any direction (EM-INV-005, BI-022).")
	t.Log("")
	t.Log("Cat 3 classifier not yet implemented.")
	t.Log("When the classifier lands, extend ScenarioA and ScenarioB tests with concrete dispatch assertions.")
	t.SkipNow()
}
