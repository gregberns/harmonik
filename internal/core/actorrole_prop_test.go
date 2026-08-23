package core

import (
	"testing"

	"pgregory.net/rapid"
)

// TestProp_ActorRole_AllRolesMarshalRoundTrip checks that every ActorRole
// returned by AllActorRoles() survives a MarshalText → UnmarshalText round-trip.
func TestProp_ActorRole_AllRolesMarshalRoundTrip(t *testing.T) {
	roles := AllActorRoles()
	rapid.Check(t, func(rt *rapid.T) {
		r := rapid.SampledFrom(roles).Draw(rt, "role")

		text, err := r.MarshalText()
		if err != nil {
			rt.Fatalf("MarshalText(%q) failed: %v", r, err)
		}

		var recovered ActorRole
		if err := recovered.UnmarshalText(text); err != nil {
			rt.Fatalf("UnmarshalText(%q) failed: %v", string(text), err)
		}

		if recovered != r {
			rt.Errorf("round-trip mismatch: got %q, want %q", recovered, r)
		}
	})
}

// TestProp_ActorRole_InvalidRejected checks that strings outside the declared
// set are rejected by both MarshalText and UnmarshalText.
func TestProp_ActorRole_InvalidRejected(t *testing.T) {
	validSet := make(map[string]bool)
	for _, r := range AllActorRoles() {
		validSet[string(r)] = true
	}

	rapid.Check(t, func(rt *rapid.T) {
		raw := rapid.StringN(1, 64, -1).Draw(rt, "raw")
		if validSet[raw] {
			rt.Skip("valid value, skipping rejection test")
		}

		r := ActorRole(raw)
		if _, err := r.MarshalText(); err == nil {
			rt.Errorf("MarshalText(%q): expected error for unknown role, got nil", raw)
		}

		var out ActorRole
		if err := out.UnmarshalText([]byte(raw)); err == nil {
			rt.Errorf("UnmarshalText(%q): expected error for unknown role, got nil", raw)
		}
	})
}

// TestProp_ActorRole_AllActorRolesCompleteness checks that every declared
// ActorRole constant appears in AllActorRoles() exactly once.
func TestProp_ActorRole_AllActorRolesCompleteness(t *testing.T) {
	declared := []ActorRole{
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

	all := AllActorRoles()
	counts := make(map[ActorRole]int, len(all))
	for _, r := range all {
		counts[r]++
	}

	for _, r := range declared {
		if counts[r] == 0 {
			t.Errorf("AllActorRoles() missing declared role %q", r)
		} else if counts[r] > 1 {
			t.Errorf("AllActorRoles() contains role %q %d times, want 1", r, counts[r])
		}
	}

	if len(all) != len(declared) {
		t.Errorf("AllActorRoles() length = %d, want %d", len(all), len(declared))
	}
}
