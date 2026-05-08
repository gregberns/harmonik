package core

import (
	"encoding/json"
	"testing"
)

// TestActorRoleValid verifies all declared constants pass Valid() and
// that non-declared values are rejected.
func TestActorRoleValid(t *testing.T) {
	t.Parallel()

	valid := []ActorRole{
		ActorRolePlanner,
		ActorRoleResearcher,
		ActorRoleBuilder,
		ActorRoleReviewer,
		ActorRoleVerifier,
		ActorRoleScheduler,
		ActorRoleGovernor,
		ActorRoleDaemon,
		ActorRoleReconciliation,
	}
	for _, r := range valid {
		if !r.Valid() {
			t.Errorf("expected %q to be valid", r)
		}
	}

	invalid := []ActorRole{
		"",
		"planner",
		"builder",
		"PLANNER",
		"BUILDER",
		"unknown",
		"orchestrator",
		"Planner ",
		" Builder",
	}
	for _, r := range invalid {
		if r.Valid() {
			t.Errorf("expected %q to be invalid", r)
		}
	}
}

// TestActorRoleMarshalText verifies MarshalText accepts valid values and
// rejects invalid ones.
func TestActorRoleMarshalText(t *testing.T) {
	t.Parallel()

	got, err := ActorRoleBuilder.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "Builder" {
		t.Errorf("MarshalText = %q, want %q", string(got), "Builder")
	}

	got, err = ActorRoleDaemon.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText error: %v", err)
	}
	if string(got) != "daemon" {
		t.Errorf("MarshalText = %q, want %q", string(got), "daemon")
	}

	if _, err := ActorRole("bogus").MarshalText(); err == nil {
		t.Error("MarshalText accepted invalid value")
	}

	if _, err := ActorRole("").MarshalText(); err == nil {
		t.Error("MarshalText accepted empty string")
	}
}

// TestActorRoleUnmarshalText verifies JSON round-trip behaviour via UnmarshalText.
func TestActorRoleUnmarshalText(t *testing.T) {
	t.Parallel()

	type actorRoleFixtureWrapper struct {
		Role ActorRole `json:"actor_role"`
	}

	tests := []struct {
		name    string
		input   string
		want    ActorRole
		wantErr bool
	}{
		{name: "Planner", input: `{"actor_role":"Planner"}`, want: ActorRolePlanner},
		{name: "Researcher", input: `{"actor_role":"Researcher"}`, want: ActorRoleResearcher},
		{name: "Builder", input: `{"actor_role":"Builder"}`, want: ActorRoleBuilder},
		{name: "Reviewer", input: `{"actor_role":"Reviewer"}`, want: ActorRoleReviewer},
		{name: "Verifier", input: `{"actor_role":"Verifier"}`, want: ActorRoleVerifier},
		{name: "Scheduler", input: `{"actor_role":"Scheduler"}`, want: ActorRoleScheduler},
		{name: "Governor", input: `{"actor_role":"Governor"}`, want: ActorRoleGovernor},
		{name: "daemon", input: `{"actor_role":"daemon"}`, want: ActorRoleDaemon},
		{name: "reconciliation", input: `{"actor_role":"reconciliation"}`, want: ActorRoleReconciliation},
		{name: "empty rejected", input: `{"actor_role":""}`, wantErr: true},
		{name: "lowercase planner rejected", input: `{"actor_role":"planner"}`, wantErr: true},
		{name: "uppercase BUILDER rejected", input: `{"actor_role":"BUILDER"}`, wantErr: true},
		{name: "unknown rejected", input: `{"actor_role":"Orchestrator"}`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var w actorRoleFixtureWrapper
			err := json.Unmarshal([]byte(tc.input), &w)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tc.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for input %q: %v", tc.input, err)
				return
			}
			if w.Role != tc.want {
				t.Errorf("got %q, want %q", string(w.Role), string(tc.want))
			}
		})
	}
}

// TestActorRoleAllConstantsRoundTrip verifies every declared constant survives
// a json.Marshal / json.Unmarshal round-trip.
func TestActorRoleAllConstantsRoundTrip(t *testing.T) {
	t.Parallel()

	actorRoleFixtureAllRoles := []ActorRole{
		ActorRolePlanner,
		ActorRoleResearcher,
		ActorRoleBuilder,
		ActorRoleReviewer,
		ActorRoleVerifier,
		ActorRoleScheduler,
		ActorRoleGovernor,
		ActorRoleDaemon,
		ActorRoleReconciliation,
	}

	for _, r := range actorRoleFixtureAllRoles {
		t.Run(string(r), func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(r)
			if err != nil {
				t.Fatalf("json.Marshal(%q): %v", r, err)
			}

			var decoded ActorRole
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("json.Unmarshal(%q): %v", data, err)
			}

			if decoded != r {
				t.Errorf("round-trip: got %q, want %q", decoded, r)
			}
		})
	}
}

// TestTraceValid_InvalidActorRole verifies that a Trace with an unknown ActorRole fails Valid().
func TestTraceValid_InvalidActorRole(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	tr.ActorRole = ActorRole("Orchestrator") // not a declared constant
	if tr.Valid() {
		t.Error("Valid() = true with unknown ActorRole, want false")
	}
}

// TestTraceValid_EmptyActorRoleRejectedByValid verifies that an empty ActorRole fails Valid().
func TestTraceValid_EmptyActorRoleRejectedByValid(t *testing.T) {
	t.Parallel()

	tr := traceFixture(t)
	tr.ActorRole = ActorRole("")
	if tr.Valid() {
		t.Error("Valid() = true with empty ActorRole, want false")
	}
}
