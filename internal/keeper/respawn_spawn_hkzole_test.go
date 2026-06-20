package keeper_test

// respawn_spawn_hkzole_test.go — covers the D-cycle (kill+respawn) ACTUAL-SPAWN
// path of NewLiveRecoverViaRespawn (hk-zole).
//
// Background: every prior respawn test stubbed the spawn with RespawnCmd:"true"
// (live_recover_action_test.go RunsOnValidSid) — "true" exits 0 but proves
// NOTHING actually ran. The refusal half (touch-sentinel on a bad sid) already
// proves the identity gate BLOCKS. This file closes the remaining gap: on a
// TRUSTED (valid UUIDv4) .sid the respawn command is REALLY EXECUTED — proven by
// a sentinel file the command writes — i.e. `sh -c <RespawnCmd>` is no longer a
// stubbed `true`. We pair it with the untrusted-sid refusal so the gate's BOTH
// arms are exercised against the same real, side-effecting command.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/keeper"
)

// TestRespawn_ActuallyRunsCmdOnTrustedSid: with a valid UUIDv4 .sid the gated
// action REALLY runs `sh -c <RespawnCmd>` — a harmless command that writes a
// sentinel file. The sentinel's presence (and its contents) proves the command
// executed, not just that it returned exit 0 like the old `true` stub.
func TestRespawn_ActuallyRunsCmdOnTrustedSid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	agent := "captain"
	writeSidFile(t, dir, agent, primarySID) // valid UUIDv4 → trusted

	sentinel := filepath.Join(dir, "respawned.flag")
	// A real, harmless shell command: NOT `true`. It writes a sentinel so the
	// test can prove `sh -c` actually executed the operator's launch command.
	respawnCmd := "printf RESPAWNED > " + sentinel

	fn := keeper.NewLiveRecoverViaRespawn(dir, respawnCmd)
	if fn == nil {
		t.Fatal("want non-nil action for a non-empty respawnCmd")
	}
	if err := fn(context.Background(), agent); err != nil {
		t.Fatalf("trusted .sid: want nil error from real respawn cmd; got %v", err)
	}

	got, err := os.ReadFile(sentinel)
	if err != nil {
		t.Fatalf("respawn command did not run: sentinel %q absent: %v", sentinel, err)
	}
	if string(got) != "RESPAWNED" {
		t.Errorf("sentinel = %q; want %q (sh -c did not execute the full command)", got, "RESPAWNED")
	}
}

// TestRespawn_GateBlocksUntrustedSidWithRealCmd: the SAME real side-effecting
// command must NOT run when the bound .sid is untrusted. Proves the identity
// gate fail-closes BEFORE `sh -c` for the actual-spawn path (defense-in-depth at
// the moment of firing the most destructive keeper action).
func TestRespawn_GateBlocksUntrustedSidWithRealCmd(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"absent":  "",                                     // no .sid written
		"uuidv7":  "33333333-3333-7333-8333-333333333333", // daemon implementer id (not v4)
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
			sentinel := filepath.Join(dir, "respawned.flag")
			respawnCmd := "printf RESPAWNED > " + sentinel

			fn := keeper.NewLiveRecoverViaRespawn(dir, respawnCmd)
			err := fn(context.Background(), agent)
			if !errors.Is(err, keeper.ErrLiveRecoverIdentityUntrusted) {
				t.Errorf("%s .sid: want ErrLiveRecoverIdentityUntrusted; got %v", name, err)
			}
			if _, statErr := os.Stat(sentinel); statErr == nil {
				t.Errorf("%s .sid: respawn cmd RAN despite untrusted identity (sentinel exists)", name)
			}
		})
	}
}
