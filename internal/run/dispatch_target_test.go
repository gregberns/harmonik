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
			location: testLocalLocation(),
			want: DispatchTargetNames{
				SessionName: "harmonik-run-c51448c8583dc9727f7886b7991be10d",
				WindowName:  "run-c51448c8583dc9727f7886b7991be10d",
			},
		},
		{
			name:     "remote",
			location: testRemoteLocation(),
			want: DispatchTargetNames{
				SessionName: "harmonik-run-9376952234a9af2549fbc17deea515ac",
				WindowName:  "run-9376952234a9af2549fbc17deea515ac",
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
		testRemoteLocation(),
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
		{name: "project", path: "/srv/harmonik/other", binding: base, location: testRemoteLocation()},
		{name: "run", path: "/srv/harmonik/project", binding: otherRun, location: testRemoteLocation()},
		{name: "claim", path: "/srv/harmonik/project", binding: otherClaim, location: testRemoteLocation()},
		{name: "kind", path: "/srv/harmonik/project", binding: base, location: ExecutionLocation{Kind: ExecutionLocalShared, RepositoryPath: "/srv/harmonik/project"}},
		{name: "worker", path: "/srv/harmonik/project", binding: base, location: func() ExecutionLocation {
			value := testRemoteLocation()
			value.WorkerName = "worker-b"
			return value
		}()},
		{name: "transport", path: "/srv/harmonik/project", binding: base, location: func() ExecutionLocation {
			value := testRemoteLocation()
			value.Transport = "other"
			return value
		}()},
		{name: "host", path: "/srv/harmonik/project", binding: base, location: func() ExecutionLocation {
			value := testRemoteLocation()
			value.Host = "other.example"
			return value
		}()},
		{name: "repository", path: "/srv/harmonik/project", binding: base, location: func() ExecutionLocation {
			value := testRemoteLocation()
			value.RepositoryPath = "/srv/worker/other"
			return value
		}()},
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
		{name: "relative path", path: "project", binding: base, location: testLocalLocation()},
		{name: "unclean path", path: "/srv/project/../other", binding: base, location: testLocalLocation()},
		{name: "invalid run", path: "/srv/project", binding: func() dispatch.Binding {
			value := base
			value.RunID = core.RunID{}
			return value
		}(), location: testLocalLocation()},
		{name: "invalid claim", path: "/srv/project", binding: func() dispatch.Binding {
			value := base
			value.ClaimTransitionID = core.TransitionID{}
			return value
		}(), location: testLocalLocation()},
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
