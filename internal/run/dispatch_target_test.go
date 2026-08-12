package run

import (
	"testing"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
)

func TestBuildDispatchTargetNamesGolden(t *testing.T) {
	binding := dispatchTargetTestBinding()
	tests := []struct {
		name     string
		location ExecutionLocation
		want     DispatchTargetNames
	}{
		{
			name:     "local",
			location: ExecutionLocation{Kind: ExecutionLocalIndependent},
			want: DispatchTargetNames{
				SessionName: "harmonik-run-56bcb366e7e49dcb7fbdbce655f3036c",
				WindowName:  "run-56bcb366e7e49dcb7fbdbce655f3036c",
			},
		},
		{
			name:     "remote",
			location: ExecutionLocation{Kind: ExecutionRemote, WorkerName: "worker-a"},
			want: DispatchTargetNames{
				SessionName: "harmonik-run-ba343e8747d2c745c79297abeb214fc4",
				WindowName:  "run-ba343e8747d2c745c79297abeb214fc4",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BuildDispatchTargetNames(
				"/srv/harmonik/project", binding.RunID, binding.ClaimTransitionID, tc.location,
			)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("names = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestBuildDispatchTargetNamesBindsEveryInput(t *testing.T) {
	base := dispatchTargetTestBinding()
	baseNames, err := BuildDispatchTargetNames(
		"/srv/harmonik/project",
		base.RunID,
		base.ClaimTransitionID,
		ExecutionLocation{Kind: ExecutionRemote, WorkerName: "worker-a"},
	)
	if err != nil {
		t.Fatal(err)
	}

	otherRun := base
	otherRun.RunID = core.RunID(uuid.MustParse("0197d200-0000-7000-8000-000000000012"))
	otherClaim := base
	otherClaim.ClaimTransitionID = core.TransitionID(uuid.MustParse("0197d200-0000-7000-8000-000000000013"))
	tests := []struct {
		name     string
		path     string
		binding  dispatch.Binding
		location ExecutionLocation
	}{
		{name: "project", path: "/srv/harmonik/other", binding: base, location: ExecutionLocation{Kind: ExecutionRemote, WorkerName: "worker-a"}},
		{name: "run", path: "/srv/harmonik/project", binding: otherRun, location: ExecutionLocation{Kind: ExecutionRemote, WorkerName: "worker-a"}},
		{name: "claim", path: "/srv/harmonik/project", binding: otherClaim, location: ExecutionLocation{Kind: ExecutionRemote, WorkerName: "worker-a"}},
		{name: "kind", path: "/srv/harmonik/project", binding: base, location: ExecutionLocation{Kind: ExecutionLocalShared}},
		{name: "worker", path: "/srv/harmonik/project", binding: base, location: ExecutionLocation{Kind: ExecutionRemote, WorkerName: "worker-b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, buildErr := BuildDispatchTargetNames(
				tc.path, tc.binding.RunID, tc.binding.ClaimTransitionID, tc.location,
			)
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			if got == baseNames {
				t.Fatalf("names did not bind %s", tc.name)
			}
		})
	}
}

func TestBuildDispatchTargetNamesRejectsInvalidInput(t *testing.T) {
	base := dispatchTargetTestBinding()
	tests := []struct {
		name     string
		path     string
		binding  dispatch.Binding
		location ExecutionLocation
	}{
		{name: "relative path", path: "project", binding: base, location: ExecutionLocation{Kind: ExecutionLocalIndependent}},
		{name: "unclean path", path: "/srv/project/../other", binding: base, location: ExecutionLocation{Kind: ExecutionLocalIndependent}},
		{name: "invalid run", path: "/srv/project", binding: func() dispatch.Binding {
			value := base
			value.RunID = core.RunID{}
			return value
		}(), location: ExecutionLocation{Kind: ExecutionLocalIndependent}},
		{name: "invalid claim", path: "/srv/project", binding: func() dispatch.Binding {
			value := base
			value.ClaimTransitionID = core.TransitionID{}
			return value
		}(), location: ExecutionLocation{Kind: ExecutionLocalIndependent}},
		{name: "invalid location", path: "/srv/project", binding: base, location: ExecutionLocation{Kind: ExecutionRemote}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := BuildDispatchTargetNames(
				tc.path, tc.binding.RunID, tc.binding.ClaimTransitionID, tc.location,
			); err == nil {
				t.Fatal("BuildDispatchTargetNames() = nil error")
			}
		})
	}
}

func dispatchTargetTestBinding() dispatch.Binding {
	return dispatch.Binding{
		QueueID:           dispatchTestQueueID,
		QueueName:         "main",
		GroupIndex:        0,
		ItemIndex:         0,
		BeadID:            "hk-dispatch-target",
		RunID:             core.RunID(uuid.MustParse(dispatchTestRunID)),
		ClaimTransitionID: core.TransitionID(uuid.MustParse(dispatchTestTransitionID)),
	}
}
