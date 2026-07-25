package continuity

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

func TestServiceMintedCheckpointPrecedesExactVersionSelected(t *testing.T) {
	t.Parallel()

	order := []string{}
	checkpoints := &recordingCheckpointPort{
		order: &order,
		result: Checkpoint{
			AuthoritativeIdentity: "minted-id",
			CheckpointSHA:         "checkpoint-sha",
		},
	}
	versionSelected := &recordingVersionSelectedPort{order: &order}
	service := Service{
		Checkpoints:     checkpoints,
		VersionSelected: versionSelected,
	}

	got, err := service.Execute(context.Background(), Request{
		Policy:           IdentityMinted,
		ExpectedIdentity: "minted-id",
		ObservedIdentity: "minted-id",
		Location:         testLocalLocation(),
		Checkpoint:       testCheckpointMetadata(),
		VersionSelection: SendVersionSelection(7),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !got.Valid() {
		t.Fatalf("result = %+v, want valid", got)
	}
	if got.Checkpoint.CheckpointSHA != "checkpoint-sha" {
		t.Errorf("checkpoint SHA = %q, want exact port result", got.Checkpoint.CheckpointSHA)
	}
	if got.VersionSelectedDisposition != VersionSelectedSent {
		t.Errorf(
			"version-selected disposition = %q, want %q",
			got.VersionSelectedDisposition,
			VersionSelectedSent,
		)
	}
	if versionSelected.gotVersion != 7 {
		t.Errorf("selected version = %d, want exact negotiated version 7", versionSelected.gotVersion)
	}
	if want := []string{"checkpoint:minted-id", "version-selected:7"}; !reflect.DeepEqual(order, want) {
		t.Errorf("call order = %v, want %v", order, want)
	}
	if checkpoints.got.Policy != IdentityMinted {
		t.Errorf("checkpoint policy = %q, want %q", checkpoints.got.Policy, IdentityMinted)
	}
	if checkpoints.got.Location != testLocalLocation() {
		t.Errorf("checkpoint location = %+v, want %+v", checkpoints.got.Location, testLocalLocation())
	}
	if checkpoints.got.Metadata != testCheckpointMetadata() {
		t.Errorf("checkpoint metadata = %+v, want %+v", checkpoints.got.Metadata, testCheckpointMetadata())
	}
}

func TestServiceIdenticalCommittedReusePreservesIntroducingSHA(t *testing.T) {
	t.Parallel()

	order := []string{}
	service := Service{
		Checkpoints: &recordingCheckpointPort{
			order: &order,
			result: Checkpoint{
				AuthoritativeIdentity:  "minted-id",
				CheckpointSHA:          "introducing-checkpoint-sha",
				ReusedCommittedMapping: true,
			},
		},
		VersionSelected: &recordingVersionSelectedPort{order: &order},
	}

	got, err := service.Execute(context.Background(), Request{
		Policy:           IdentityMinted,
		ExpectedIdentity: "minted-id",
		ObservedIdentity: "minted-id",
		Location:         testRemoteLocation(),
		Checkpoint:       testCheckpointMetadata(),
		VersionSelection: SendVersionSelection(3),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Checkpoint.ReusedCommittedMapping {
		t.Error("ReusedCommittedMapping = false, want true")
	}
	if got.Checkpoint.CheckpointSHA != "introducing-checkpoint-sha" {
		t.Errorf(
			"checkpoint SHA = %q, want exact introducing SHA",
			got.Checkpoint.CheckpointSHA,
		)
	}
	if want := []string{"checkpoint:minted-id", "version-selected:3"}; !reflect.DeepEqual(order, want) {
		t.Errorf("call order = %v, want %v", order, want)
	}
}

func TestServiceCapturedIdentityCheckpointsBeforeResumeWithoutCHBAck(t *testing.T) {
	t.Parallel()

	order := []string{}
	checkpoints := &recordingCheckpointPort{
		order: &order,
		result: Checkpoint{
			AuthoritativeIdentity: "native-thread-id",
			CheckpointSHA:         "captured-checkpoint-sha",
		},
	}
	versionSelected := &recordingVersionSelectedPort{order: &order}
	service := Service{
		Checkpoints:     checkpoints,
		VersionSelected: versionSelected,
	}

	got, err := service.Execute(context.Background(), Request{
		Policy:           IdentityCaptured,
		ObservedIdentity: "native-thread-id",
		Location:         testRemoteLocation(),
		Checkpoint:       testCheckpointMetadata(),
		VersionSelection: NoVersionSelection(),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if got.VersionSelectedDisposition != VersionSelectedNotApplicable {
		t.Errorf(
			"version-selected disposition = %q, want %q",
			got.VersionSelectedDisposition,
			VersionSelectedNotApplicable,
		)
	}
	if versionSelected.calls != 0 {
		t.Errorf("version-selected calls = %d, want 0 for Captured CHB path", versionSelected.calls)
	}
	if want := []string{"checkpoint:native-thread-id"}; !reflect.DeepEqual(order, want) {
		t.Errorf("call order = %v, want %v", order, want)
	}
	if checkpoints.got.Policy != IdentityCaptured {
		t.Errorf("checkpoint policy = %q, want %q", checkpoints.got.Policy, IdentityCaptured)
	}
	if checkpoints.got.Location != testRemoteLocation() {
		t.Errorf("checkpoint location = %+v, want explicit remote location", checkpoints.got.Location)
	}
}

func TestServiceCapturedProtocolMayDefineVersionSelected(t *testing.T) {
	t.Parallel()

	order := []string{}
	service := Service{
		Checkpoints: &recordingCheckpointPort{
			order: &order,
			result: Checkpoint{
				AuthoritativeIdentity: "captured-id",
				CheckpointSHA:         "captured-sha",
			},
		},
		VersionSelected: &recordingVersionSelectedPort{order: &order},
	}

	got, err := service.Execute(context.Background(), Request{
		Policy:           IdentityCaptured,
		ObservedIdentity: "captured-id",
		Location:         testLocalLocation(),
		Checkpoint:       testCheckpointMetadata(),
		VersionSelection: SendVersionSelection(11),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.VersionSelectedDisposition != VersionSelectedSent {
		t.Errorf("disposition = %q, want sent", got.VersionSelectedDisposition)
	}
	if want := []string{"checkpoint:captured-id", "version-selected:11"}; !reflect.DeepEqual(order, want) {
		t.Errorf("call order = %v, want %v", order, want)
	}
}

func TestServiceFaultsFailClosed(t *testing.T) {
	t.Parallel()

	checkpointFailure := errors.New("checkpoint failed")
	versionFailure := errors.New("version-selected failed")
	identityConflict := &IdentityConflictError{
		CommittedIdentity: "committed-id",
		RequestedIdentity: "identity",
		CheckpointSHA:     "committed-checkpoint-sha",
	}
	tests := []struct {
		name                   string
		checkpoint             Checkpoint
		checkpointErr          error
		versionErr             error
		wantStage              FailureStage
		wantOrder              []string
		wantCommittedOnFailure bool
		wantCause              error
	}{
		{
			name:          "checkpoint failure withholds version selected",
			checkpointErr: checkpointFailure,
			wantStage:     FailureCheckpoint,
			wantOrder:     []string{"checkpoint:identity"},
			wantCause:     checkpointFailure,
		},
		{
			name:          "committed identity conflict withholds version selected",
			checkpointErr: identityConflict,
			wantStage:     FailureCheckpoint,
			wantOrder:     []string{"checkpoint:identity"},
			wantCause:     identityConflict,
		},
		{
			name: "missing checkpoint SHA withholds version selected",
			checkpoint: Checkpoint{
				AuthoritativeIdentity: "identity",
			},
			wantStage: FailureCheckpointResult,
			wantOrder: []string{"checkpoint:identity"},
		},
		{
			name: "mismatched checkpoint identity withholds version selected",
			checkpoint: Checkpoint{
				AuthoritativeIdentity: "different-identity",
				CheckpointSHA:         "checkpoint-sha",
			},
			wantStage: FailureCheckpointResult,
			wantOrder: []string{"checkpoint:identity"},
		},
		{
			name: "version-selected failure exposes committed checkpoint",
			checkpoint: Checkpoint{
				AuthoritativeIdentity:  "identity",
				CheckpointSHA:          "checkpoint-sha",
				ReusedCommittedMapping: true,
			},
			versionErr:             versionFailure,
			wantStage:              FailureVersionSelected,
			wantOrder:              []string{"checkpoint:identity", "version-selected:5"},
			wantCommittedOnFailure: true,
			wantCause:              versionFailure,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			order := []string{}
			service := Service{
				Checkpoints: &recordingCheckpointPort{
					order:  &order,
					result: tt.checkpoint,
					err:    tt.checkpointErr,
				},
				VersionSelected: &recordingVersionSelectedPort{
					order: &order,
					err:   tt.versionErr,
				},
			}
			got, err := service.Execute(context.Background(), Request{
				Policy:           IdentityMinted,
				ExpectedIdentity: "identity",
				ObservedIdentity: "identity",
				Location:         testLocalLocation(),
				Checkpoint:       testCheckpointMetadata(),
				VersionSelection: SendVersionSelection(5),
			})
			if err == nil {
				t.Fatalf("Execute() result = %+v, want error", got)
			}
			if !reflect.DeepEqual(got, Result{}) {
				t.Errorf("result on failure = %+v, want zero value", got)
			}
			var transactionErr *Error
			if !errors.As(err, &transactionErr) {
				t.Fatalf("error type = %T, want *continuity.Error", err)
			}
			if transactionErr.Stage != tt.wantStage {
				t.Errorf("failure stage = %q, want %q", transactionErr.Stage, tt.wantStage)
			}
			if tt.wantCause != nil && !errors.Is(err, tt.wantCause) {
				t.Errorf("errors.Is(%v) = false", tt.wantCause)
			}
			if tt.wantCause == identityConflict {
				var gotConflict *IdentityConflictError
				if !errors.As(err, &gotConflict) {
					t.Fatalf("errors.As(*IdentityConflictError) = false for %v", err)
				}
				if gotConflict.CommittedIdentity != "committed-id" ||
					gotConflict.RequestedIdentity != "identity" ||
					gotConflict.CheckpointSHA != "committed-checkpoint-sha" {
					t.Errorf("identity conflict = %+v, want committed/requested/checkpoint facts", gotConflict)
				}
			}
			if !reflect.DeepEqual(order, tt.wantOrder) {
				t.Errorf("call order = %v, want %v", order, tt.wantOrder)
			}
			if tt.wantCommittedOnFailure {
				if transactionErr.CommittedCheckpoint == nil {
					t.Fatal("CommittedCheckpoint = nil after version-selected failure")
				}
				if *transactionErr.CommittedCheckpoint != tt.checkpoint {
					t.Errorf(
						"CommittedCheckpoint = %+v, want %+v",
						*transactionErr.CommittedCheckpoint,
						tt.checkpoint,
					)
				}
			} else if transactionErr.CommittedCheckpoint != nil {
				t.Errorf(
					"CommittedCheckpoint = %+v before successful checkpoint validation, want nil",
					transactionErr.CommittedCheckpoint,
				)
			}
		})
	}
}

func TestServiceValidationPrecedesEffects(t *testing.T) {
	t.Parallel()

	validCheckpoint := Checkpoint{
		AuthoritativeIdentity: "identity",
		CheckpointSHA:         "checkpoint-sha",
	}
	validMinted := testMintedRequest("identity", SendVersionSelection(1))
	nilContext := validMinted
	unknownPolicy := validMinted
	unknownPolicy.Policy = IdentityPolicy("unknown")
	missingCapturedIdentity := testCapturedRequest("identity", NoVersionSelection())
	missingCapturedIdentity.ObservedIdentity = ""
	capturedWithSubstitute := testCapturedRequest("captured-id", NoVersionSelection())
	capturedWithSubstitute.ExpectedIdentity = "substitute-id"
	mintedMismatch := validMinted
	mintedMismatch.ObservedIdentity = "different-observed-id"
	invalidApplicableVersion := validMinted
	invalidApplicableVersion.VersionSelection = SendVersionSelection(0)
	invalidNotApplicableVersion := testCapturedRequest("identity", NoVersionSelection())
	invalidNotApplicableVersion.VersionSelection.SelectedVersion = 2
	invalidLocation := validMinted
	invalidLocation.Location = Location{
		Kind:          LocationRemote,
		WorkspacePath: "/worker/run/worktree",
	}
	invalidMetadata := validMinted
	invalidMetadata.Checkpoint.TransitionID = core.TransitionID{}

	tests := []struct {
		name         string
		ctx          context.Context
		request      Request
		noCheckpoint bool
		noVersion    bool
	}{
		{
			name:    "nil context",
			request: nilContext,
		},
		{
			name:    "unknown identity policy",
			ctx:     context.Background(),
			request: unknownPolicy,
		},
		{
			name:    "missing captured identity",
			ctx:     context.Background(),
			request: missingCapturedIdentity,
		},
		{
			name:    "captured identity forbids pre-launch substitute",
			ctx:     context.Background(),
			request: capturedWithSubstitute,
		},
		{
			name:    "minted observed identity must match launch artifact",
			ctx:     context.Background(),
			request: mintedMismatch,
		},
		{
			name:    "applicable version must be positive",
			ctx:     context.Background(),
			request: invalidApplicableVersion,
		},
		{
			name:    "not-applicable version must be zero",
			ctx:     context.Background(),
			request: invalidNotApplicableVersion,
		},
		{
			name:    "remote location requires worker name",
			ctx:     context.Background(),
			request: invalidLocation,
		},
		{
			name:    "checkpoint metadata requires transition ID",
			ctx:     context.Background(),
			request: invalidMetadata,
		},
		{
			name:         "checkpoint port is required",
			ctx:          context.Background(),
			noCheckpoint: true,
			request:      validMinted,
		},
		{
			name:      "version-selected port required when applicable",
			ctx:       context.Background(),
			noVersion: true,
			request:   validMinted,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			order := []string{}
			checkpoints := &recordingCheckpointPort{order: &order, result: validCheckpoint}
			versionSelected := &recordingVersionSelectedPort{order: &order}
			service := Service{
				Checkpoints:     checkpoints,
				VersionSelected: versionSelected,
			}
			if tt.noCheckpoint {
				service.Checkpoints = nil
			}
			if tt.noVersion {
				service.VersionSelected = nil
			}

			got, err := service.Execute(tt.ctx, tt.request)
			if err == nil {
				t.Fatalf("Execute() result = %+v, want validation error", got)
			}
			var transactionErr *Error
			if !errors.As(err, &transactionErr) {
				t.Fatalf("error type = %T, want *continuity.Error", err)
			}
			if transactionErr.Stage != FailureValidation {
				t.Errorf("failure stage = %q, want %q", transactionErr.Stage, FailureValidation)
			}
			if len(order) != 0 {
				t.Errorf("effects occurred before validation completed: %v", order)
			}
		})
	}
}

func TestValueValidation(t *testing.T) {
	t.Parallel()

	if !IdentityMinted.Valid() || !IdentityCaptured.Valid() {
		t.Error("declared identity policy is invalid")
	}
	if IdentityPolicy("").Valid() {
		t.Error("empty identity policy is valid")
	}
	if !NoVersionSelection().Valid() || !SendVersionSelection(1).Valid() {
		t.Error("declared version-selection shape is invalid")
	}
	if SendVersionSelection(-1).Valid() {
		t.Error("negative selected version is valid")
	}
	if !VersionSelectedNotApplicable.Valid() || !VersionSelectedSent.Valid() {
		t.Error("declared version-selected disposition is invalid")
	}
	if VersionSelectedDisposition("").Valid() {
		t.Error("empty version-selected disposition is valid")
	}
	if !testLocalLocation().Valid() || !testRemoteLocation().Valid() {
		t.Error("declared continuity location is invalid")
	}
	if (Location{}).Valid() {
		t.Error("zero continuity location is valid")
	}
	if !testCheckpointMetadata().Valid() {
		t.Error("declared checkpoint metadata is invalid")
	}
	if (CheckpointMetadata{}).Valid() {
		t.Error("zero checkpoint metadata is valid")
	}
	mismatchedRun := testCheckpointMetadata()
	mismatchedRun.State.RunID = core.RunID(uuid.MustParse(
		"019befd8-9d58-7000-8000-000000000099",
	))
	if mismatchedRun.Valid() {
		t.Error("checkpoint metadata accepts a valid state owned by another run")
	}
	missingProvenance := testCheckpointMetadata()
	missingProvenance.State.NodeID = ""
	if missingProvenance.Valid() {
		t.Error("checkpoint metadata accepts state without node provenance")
	}

	request := testMintedRequest("identity", SendVersionSelection(1))
	if !request.Valid() {
		t.Error("valid request reported invalid")
	}
	checkpointRequest := CheckpointRequest{
		Policy:                request.Policy,
		AuthoritativeIdentity: request.ObservedIdentity,
		Location:              request.Location,
		Metadata:              request.Checkpoint,
	}
	if !checkpointRequest.Valid() {
		t.Error("valid checkpoint request reported invalid")
	}
	checkpoint := Checkpoint{
		AuthoritativeIdentity: "identity",
		CheckpointSHA:         "checkpoint-sha",
	}
	if !checkpoint.ValidFor(checkpointRequest) {
		t.Error("valid checkpoint reported invalid")
	}
	result := Result{
		Checkpoint:                 checkpoint,
		VersionSelectedDisposition: VersionSelectedSent,
	}
	if !result.Valid() {
		t.Error("valid result reported invalid")
	}
	if (Result{}).Valid() {
		t.Error("zero result reported valid")
	}
}

func testMintedRequest(identity string, selection VersionSelection) Request {
	return Request{
		Policy:           IdentityMinted,
		ExpectedIdentity: identity,
		ObservedIdentity: identity,
		Location:         testLocalLocation(),
		Checkpoint:       testCheckpointMetadata(),
		VersionSelection: selection,
	}
}

func testCapturedRequest(identity string, selection VersionSelection) Request {
	return Request{
		Policy:           IdentityCaptured,
		ObservedIdentity: identity,
		Location:         testRemoteLocation(),
		Checkpoint:       testCheckpointMetadata(),
		VersionSelection: selection,
	}
}

func testLocalLocation() Location {
	return Location{
		Kind:          LocationLocal,
		WorkspacePath: "/local/run/worktree",
	}
}

func testRemoteLocation() Location {
	return Location{
		Kind:          LocationRemote,
		WorkspacePath: "/worker/run/worktree",
		WorkerName:    "worker-a",
	}
}

func testCheckpointMetadata() CheckpointMetadata {
	runID := core.RunID(uuid.MustParse(
		"019befd8-9d58-7000-8000-000000000001",
	))
	return CheckpointMetadata{
		RunID: runID,
		State: core.State{
			StateID: core.StateID(uuid.MustParse(
				"019befd8-9d58-7000-8000-000000000002",
			)),
			RunID:     runID,
			NodeID:    core.NodeID("implementer"),
			EnteredAt: time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC),
			TransitionHistory: core.CommitRange{
				FirstCommitSHA: "1111111111111111111111111111111111111111",
				LastCommitSHA:  "2222222222222222222222222222222222222222",
			},
		},
		TransitionID: core.TransitionID(uuid.MustParse(
			"019befd8-9d58-7000-8000-000000000003",
		)),
		SchemaVersion: 1,
		BeadID:        core.BeadID("hk-continuity"),
	}
}

type recordingCheckpointPort struct {
	order  *[]string
	result Checkpoint
	err    error
	got    CheckpointRequest
	calls  int
}

func (p *recordingCheckpointPort) EnsureCommitted(
	_ context.Context,
	request CheckpointRequest,
) (Checkpoint, error) {
	p.calls++
	p.got = request
	if p.order != nil {
		*p.order = append(*p.order, "checkpoint:"+request.AuthoritativeIdentity)
	}
	return p.result, p.err
}

type recordingVersionSelectedPort struct {
	order      *[]string
	err        error
	gotVersion int
	calls      int
}

func (p *recordingVersionSelectedPort) SendVersionSelected(
	_ context.Context,
	selectedVersion int,
) error {
	p.calls++
	p.gotVersion = selectedVersion
	if p.order != nil {
		*p.order = append(*p.order, fmt.Sprintf("version-selected:%d", selectedVersion))
	}
	return p.err
}
