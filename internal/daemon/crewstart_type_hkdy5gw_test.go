package daemon

// crewstart_type_hkdy5gw_test.go — coverage for Record.Type stamping at
// crew-start (hk-dy5gw). The durable type lets the SD-3 crew-idle-reaper honour
// the manifest lifecycle.persistent flag for oversight roles (admiral, watch).

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/crew"
)

// TestCrewStart_StampsTypeFromAgentFolder: an oversight singleton launches with
// instance name == type folder name, so resolveCrewType derives Type from the
// same-named .harmonik/agents/<name>/ folder and stamps it on the record.
func TestCrewStart_StampsTypeFromAgentFolder(t *testing.T) {
	sub := &fakeSubstrate{}
	h, dir := newTestCrewHandler(t, sub, nil)

	// Seed a same-named agent type folder (as an oversight role has).
	if err := os.MkdirAll(filepath.Join(dir, ".harmonik", "agents", "admiral"), 0o755); err != nil {
		t.Fatalf("seed agent folder: %v", err)
	}

	mustCrewStart(t, h, CrewStartRequest{Name: "admiral", Queue: "admiral-q"})

	rec, err := crew.Load(dir, "admiral")
	if err != nil {
		t.Fatalf("crew.Load: %v", err)
	}
	if rec.Type != "admiral" {
		t.Errorf("registry Type = %q, want %q", rec.Type, "admiral")
	}
	if rec.EffectiveType() != "admiral" {
		t.Errorf("EffectiveType = %q, want %q", rec.EffectiveType(), "admiral")
	}
}

// TestCrewStart_ExplicitTypeWins: an explicit req.Type is stamped verbatim even
// when no same-named folder exists (future non-singleton crews).
func TestCrewStart_ExplicitTypeWins(t *testing.T) {
	sub := &fakeSubstrate{}
	h, dir := newTestCrewHandler(t, sub, nil)

	mustCrewStart(t, h, CrewStartRequest{Name: "watcher-1", Queue: "w-q", Type: "watch"})

	rec, err := crew.Load(dir, "watcher-1")
	if err != nil {
		t.Fatalf("crew.Load: %v", err)
	}
	if rec.Type != "watch" {
		t.Errorf("registry Type = %q, want %q", rec.Type, "watch")
	}
}

// TestCrewStart_OrdinaryBeadCrewHasNoStampedType: a bead-crew whose name matches
// no type folder gets an empty Type, which EffectiveType() reads as the default
// "crew" — never persistent, so the reaper still reclaims it when drained.
func TestCrewStart_OrdinaryBeadCrewHasNoStampedType(t *testing.T) {
	sub := &fakeSubstrate{}
	h, dir := newTestCrewHandler(t, sub, nil)

	mustCrewStart(t, h, CrewStartRequest{Name: "paul", Queue: "paul-q"})

	rec, err := crew.Load(dir, "paul")
	if err != nil {
		t.Fatalf("crew.Load: %v", err)
	}
	if rec.Type != "" {
		t.Errorf("registry Type = %q, want empty", rec.Type)
	}
	if rec.EffectiveType() != "crew" {
		t.Errorf("EffectiveType = %q, want %q", rec.EffectiveType(), "crew")
	}
}
