package daemon

// pi_profile_resolve_test.go — unit tests for resolvePiProfile and
// hasSingleModelLabel (pi-provider-switch, hk-m6uu2 C3).
//
// Helper prefix: piProfFixture (implementer-protocol.md §Helper-prefix discipline).

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
)

// piProfFixtureBus is a minimal event collector for testing label-conflict events.
type piProfFixtureBus struct {
	mu     sync.Mutex
	events []core.EventType
}

func (b *piProfFixtureBus) Emit(_ context.Context, et core.EventType, _ []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, et)
	return nil
}

func (b *piProfFixtureBus) EmitWithRunID(_ context.Context, _ core.RunID, et core.EventType, payload []byte) error {
	return b.Emit(context.Background(), et, payload)
}

func (b *piProfFixtureBus) hasEventType(et core.EventType) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range b.events {
		if e == et {
			return true
		}
	}
	return false
}

func piProfFixtureCfg() PiHarnessConfig {
	return PiHarnessConfig{
		Provider:  "ornith",
		Model:     "ornith/default",
		APIKeyEnv: "ORNITH_API_KEY",
		Profiles: map[string]PiProfileConfig{
			"ornith-dgx": {
				Provider:   "ornith",
				Model:      "ornith/dgx-model",
				APIKeyEnv:  "ORNITH_DGX_API_KEY",
				APIKeyFile: "/secrets/ornith-dgx",
				BaseURL:    "http://dgx.local:8551/v1",
				API:        "openai",
			},
		},
	}
}

func TestResolvePiProfile_LabeledBead_ResolvesTuple(t *testing.T) {
	t.Parallel()

	bus := &piProfFixtureBus{}
	profile, err := resolvePiProfile(
		context.Background(),
		[]string{"profile:ornith-dgx"},
		core.AgentTypePi,
		piProfFixtureCfg(),
		bus,
		"bead-001",
	)
	if err != nil {
		t.Fatalf("resolvePiProfile: unexpected error: %v", err)
	}
	want := piProfFixtureCfg().Profiles["ornith-dgx"]
	if profile != want {
		t.Errorf("resolvePiProfile = %+v; want %+v", profile, want)
	}
	if bus.hasEventType(core.EventTypeBeadLabelConflict) {
		t.Error("no conflict event expected for a single profile: label")
	}
}

func TestResolvePiProfile_UnlabeledBead_ZeroTuple(t *testing.T) {
	t.Parallel()

	bus := &piProfFixtureBus{}
	profile, err := resolvePiProfile(
		context.Background(),
		[]string{"subsystem:daemon"},
		core.AgentTypePi,
		piProfFixtureCfg(),
		bus,
		"bead-002",
	)
	if err != nil {
		t.Fatalf("resolvePiProfile: unexpected error: %v", err)
	}
	if profile != (PiProfileConfig{}) {
		t.Errorf("resolvePiProfile = %+v; want zero tuple", profile)
	}
}

func TestResolvePiProfile_ClaudeHarness_ZeroTuple(t *testing.T) {
	t.Parallel()

	bus := &piProfFixtureBus{}
	profile, err := resolvePiProfile(
		context.Background(),
		[]string{"profile:ornith-dgx"},
		core.AgentTypeClaudeCode,
		piProfFixtureCfg(),
		bus,
		"bead-003",
	)
	if err != nil {
		t.Fatalf("resolvePiProfile: unexpected error: %v", err)
	}
	if profile != (PiProfileConfig{}) {
		t.Errorf("resolvePiProfile (harness gate) = %+v; want zero tuple", profile)
	}
	if bus.hasEventType(core.EventTypeBeadLabelConflict) {
		t.Error("harness gate must be quiet: no event expected")
	}
}

func TestResolvePiProfile_UnknownProfile_FailLoud(t *testing.T) {
	t.Parallel()

	bus := &piProfFixtureBus{}
	_, err := resolvePiProfile(
		context.Background(),
		[]string{"profile:nope"},
		core.AgentTypePi,
		piProfFixtureCfg(),
		bus,
		"bead-004",
	)
	if err == nil {
		t.Fatal("resolvePiProfile: expected error for unknown profile; got nil")
	}
	var unknownErr *PiProfileUnknownError
	if !errors.As(err, &unknownErr) {
		t.Errorf("resolvePiProfile error type = %T (%v); want *PiProfileUnknownError", err, err)
	}
}

func TestResolvePiProfile_MultipleLabels_Conflict(t *testing.T) {
	t.Parallel()

	bus := &piProfFixtureBus{}
	profile, err := resolvePiProfile(
		context.Background(),
		[]string{"profile:ornith-dgx", "profile:other"},
		core.AgentTypePi,
		piProfFixtureCfg(),
		bus,
		"bead-005",
	)
	if err != nil {
		t.Fatalf("resolvePiProfile: unexpected error: %v", err)
	}
	if profile != (PiProfileConfig{}) {
		t.Errorf("resolvePiProfile (conflict) = %+v; want zero tuple", profile)
	}
	if !bus.hasEventType(core.EventTypeBeadLabelConflict) {
		t.Error("expected bead_label_conflict event for multiple profile: labels; none emitted")
	}
}

func TestResolvePiProfile_ModelLabelOverridesProfileModelOnly(t *testing.T) {
	t.Parallel()

	bus := &piProfFixtureBus{}
	labels := []string{"profile:ornith-dgx", "model:custom"}
	profile, err := resolvePiProfile(
		context.Background(), labels, core.AgentTypePi, piProfFixtureCfg(), bus, "bead-006",
	)
	if err != nil {
		t.Fatalf("resolvePiProfile: unexpected error: %v", err)
	}
	want := piProfFixtureCfg().Profiles["ornith-dgx"]
	if profile != want {
		t.Errorf("resolvePiProfile = %+v; want %+v (triple stays atomic)", profile, want)
	}
	// The model: label is not resolvePiProfile's concern (caller coalesces via
	// hasSingleModelLabel); assert the coalesce helper agrees exactly one is present.
	if !hasSingleModelLabel(labels) {
		t.Error("hasSingleModelLabel should be true for exactly one model: label")
	}
}

func TestHasSingleModelLabel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		labels []string
		want   bool
	}{
		{"zero", []string{"subsystem:daemon"}, false},
		{"one", []string{"model:opus"}, true},
		{"two", []string{"model:opus", "model:sonnet"}, false},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := hasSingleModelLabel(c.labels); got != c.want {
				t.Errorf("hasSingleModelLabel(%v) = %v; want %v", c.labels, got, c.want)
			}
		})
	}
}
