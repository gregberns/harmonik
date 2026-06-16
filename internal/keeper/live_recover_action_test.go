package keeper_test

// live_recover_action_test.go — unit tests for NewLiveRecoverViaRespawn, the
// gated ForceRestart action wired into WatcherConfig.LiveRecoverFn (hk-75mr).
// The action runs the operator's --respawn-cmd, but REFUSES (no restart) unless
// the bound .sid identity is a valid UUIDv4 — defense-in-depth at the moment of
// firing the most destructive keeper action.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/keeper"
)

// TestNewLiveRecoverViaRespawn_NilWhenNoCmd: with no --respawn-cmd there is no
// action to wire, so the factory returns nil (live-pane recovery disabled).
func TestNewLiveRecoverViaRespawn_NilWhenNoCmd(t *testing.T) {
	t.Parallel()
	if fn := keeper.NewLiveRecoverViaRespawn(t.TempDir(), ""); fn != nil {
		t.Errorf("empty respawnCmd: want nil action (disabled); got non-nil")
	}
}

// TestNewLiveRecoverViaRespawn_RunsOnValidSid: a valid UUIDv4 .sid → the action
// runs the respawn command (here "true", which succeeds → nil error).
func TestNewLiveRecoverViaRespawn_RunsOnValidSid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agent := "captain"
	writeSidFile(t, dir, agent, primarySID) // valid UUIDv4

	fn := keeper.NewLiveRecoverViaRespawn(dir, "true")
	if fn == nil {
		t.Fatal("want non-nil action for a non-empty respawnCmd")
	}
	if err := fn(context.Background(), agent); err != nil {
		t.Errorf("valid .sid: want nil error from `true`; got %v", err)
	}
}

// TestNewLiveRecoverViaRespawn_RefusesOnInvalidSid: an absent or non-UUIDv4 .sid
// → the action REFUSES (ErrLiveRecoverIdentityUntrusted) and never runs the
// command. We prove the command did NOT run by using a command that would create
// a sentinel file; the file must be absent after a refusal.
func TestNewLiveRecoverViaRespawn_RefusesOnInvalidSid(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"absent":  "",                                     // no .sid written
		"uuidv7":  "33333333-3333-7333-8333-333333333333", // daemon implementer id
		"garbage": "not-a-uuid",
	}
	for name, sid := range cases {
		sid := sid
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			agent := "captain"
			if sid != "" {
				writeSidFile(t, dir, agent, sid)
			}
			sentinel := filepath.Join(dir, "ran")
			// Command would touch the sentinel IF it ran — it must not.
			fn := keeper.NewLiveRecoverViaRespawn(dir, "touch "+sentinel)
			err := fn(context.Background(), agent)
			if !errors.Is(err, keeper.ErrLiveRecoverIdentityUntrusted) {
				t.Errorf("%s .sid: want ErrLiveRecoverIdentityUntrusted; got %v", name, err)
			}
			if _, statErr := os.Stat(sentinel); statErr == nil {
				t.Errorf("%s .sid: respawn command ran despite identity refusal (sentinel exists)", name)
			}
		})
	}
}
