package daemon_test

// projectconfig_keeper_delivery_e1mdc_test.go — T2 (hk-keeper-delivery-config-surface-e1mdc):
// the new keeper.warn_messages keys (leader_defer_text, crew_defer_text) parse
// into KeeperConfig, an unknown sibling key STILL hard-errors with
// *ErrUnknownConfigKey (strict KnownFields decode is not loosened), and the crew
// key defaults empty/off (K7 config hook only). Spec: session-keeper.md §4.14
// SK-032; park-resume-protocol.md §9.

import (
	"errors"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

func TestKeeperBlock_DeliveryKeys_Parse_e1mdc(t *testing.T) {
	t.Parallel()

	const leaderText = "leader: finish your unit, then harmonik keeper restart-now"
	const crewText = "crew: finish and self-restart on wake"

	root := keeperBlkFixtureDir(t, `
schema_version: 1
keeper:
  warn_messages:
    leader_defer_text: "`+leaderText+`"
    crew_defer_text: "`+crewText+`"
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	k := cfg.Keeper
	if k.LeaderDeferText != leaderText {
		t.Errorf("LeaderDeferText: want %q, got %q", leaderText, k.LeaderDeferText)
	}
	if k.CrewDeferText != crewText {
		t.Errorf("CrewDeferText: want %q, got %q", crewText, k.CrewDeferText)
	}
}

func TestKeeperBlock_CrewDeferKey_DefaultOff_e1mdc(t *testing.T) {
	t.Parallel()

	// A warn_messages block with NO crew_defer_text: the crew key must default to
	// empty (K7 config hook is default-off; nothing consumes it in T2).
	root := keeperBlkFixtureDir(t, `
schema_version: 1
keeper:
  warn_messages:
    leader_defer_text: "leader only"
`)
	cfg, err := daemon.ExportedLoadProjectConfig(root)
	if err != nil {
		t.Fatalf("LoadProjectConfig: unexpected error: %v", err)
	}
	if cfg.Keeper.CrewDeferText != "" {
		t.Errorf("CrewDeferText default: want empty (off), got %q", cfg.Keeper.CrewDeferText)
	}
}

func TestKeeperBlock_UnknownWarnMessagesSibling_StillRejected_e1mdc(t *testing.T) {
	t.Parallel()

	// Adding the two new recognized keys must NOT loosen strict decoding: an
	// unknown sibling in warn_messages still yields *ErrUnknownConfigKey naming it.
	root := keeperBlkFixtureDir(t, `
schema_version: 1
keeper:
  warn_messages:
    leader_defer_text: "ok"
    leader_defer_txet: "typo"
`)
	_, err := daemon.ExportedLoadProjectConfig(root)
	if err == nil {
		t.Fatal("LoadProjectConfig: want *ErrUnknownConfigKey for a typo'd warn_messages key; got nil")
	}
	var uerr *daemon.ExportedErrUnknownConfigKey
	if !errors.As(err, &uerr) {
		t.Fatalf("error type = %T (%v); want *ErrUnknownConfigKey", err, err)
	}
	if uerr.KeyPath != "keeper.warn_messages.leader_defer_txet" {
		t.Errorf("KeyPath = %q; want %q", uerr.KeyPath, "keeper.warn_messages.leader_defer_txet")
	}
}
