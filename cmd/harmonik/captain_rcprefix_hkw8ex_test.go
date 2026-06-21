package main

// captain_rcprefix_hkw8ex_test.go — N1 (hk-w8ex): table-driven assertion that
// --rc-prefix is propagated identically by buildCaptainTmuxCmd (launch) and
// buildCaptainRespawnWindowCmd (respawn), locking launch==respawn parity at the
// CLI layer (previously only transitive via daemon TestJoinRemoteControlName).

import (
	"testing"
)

// TestBuildCaptainTmuxCmd_RcPrefix_hkw8ex asserts that buildCaptainTmuxCmd
// folds --rc-prefix into the --remote-control label.
func TestBuildCaptainTmuxCmd_RcPrefix_hkw8ex(t *testing.T) {
	cases := []struct {
		name     string
		rcPrefix string
		wantRC   string
	}{
		{"bare (no prefix)", "", "captain"},
		{"prefixed hk", "hk", "hk-captain"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := buildCaptainTmuxCmd("captain", "test-session", "11111111-2222-4333-8444-555555555555", c.rcPrefix)
			if got := flagValueHkly0n(cmd.Args, "--remote-control"); got != c.wantRC {
				t.Errorf("buildCaptainTmuxCmd --remote-control = %q, want %q (rcPrefix=%q)", got, c.wantRC, c.rcPrefix)
			}
			// HARMONIK_AGENT stays bare regardless of prefix.
			if got := flagValueHkly0n(cmd.Args, "-e"); got != "HARMONIK_AGENT=captain" {
				t.Errorf("HARMONIK_AGENT = %q, want %q (prefix must not bleed into env)", got, "HARMONIK_AGENT=captain")
			}
		})
	}
}

// TestBuildCaptainRespawnWindowCmd_RcPrefix_hkw8ex asserts that
// buildCaptainRespawnWindowCmd folds --rc-prefix into the --remote-control label.
func TestBuildCaptainRespawnWindowCmd_RcPrefix_hkw8ex(t *testing.T) {
	cases := []struct {
		name     string
		rcPrefix string
		wantRC   string
	}{
		{"bare (no prefix)", "", "captain"},
		{"prefixed hk", "hk", "hk-captain"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := buildCaptainRespawnWindowCmd("captain", "test-session:agent", "11111111-2222-4333-8444-555555555555", c.rcPrefix)
			if got := flagValueHkly0n(cmd.Args, "--remote-control"); got != c.wantRC {
				t.Errorf("buildCaptainRespawnWindowCmd --remote-control = %q, want %q (rcPrefix=%q)", got, c.wantRC, c.rcPrefix)
			}
			// HARMONIK_AGENT stays bare regardless of prefix.
			if got := flagValueHkly0n(cmd.Args, "-e"); got != "HARMONIK_AGENT=captain" {
				t.Errorf("HARMONIK_AGENT = %q, want %q (prefix must not bleed into env)", got, "HARMONIK_AGENT=captain")
			}
		})
	}
}

// TestCaptainLaunchRespawnParity_RcPrefix_hkw8ex is the parity assertion:
// launch and respawn must produce the SAME --remote-control label for the same
// (name, rcPrefix) pair. A drift between the two would break the RC-label picker.
func TestCaptainLaunchRespawnParity_RcPrefix_hkw8ex(t *testing.T) {
	const (
		name    = "captain"
		sid     = "11111111-2222-4333-8444-555555555555"
		session = "test-session"
	)
	cases := []struct{ rcPrefix string }{
		{""},
		{"hk"},
		{"myproject"},
	}
	for _, c := range cases {
		t.Run("prefix="+c.rcPrefix, func(t *testing.T) {
			launchCmd := buildCaptainTmuxCmd(name, session, sid, c.rcPrefix)
			respawnCmd := buildCaptainRespawnWindowCmd(name, session+":agent", sid, c.rcPrefix)

			launchRC := flagValueHkly0n(launchCmd.Args, "--remote-control")
			respawnRC := flagValueHkly0n(respawnCmd.Args, "--remote-control")

			if launchRC != respawnRC {
				t.Errorf("launch --remote-control %q != respawn --remote-control %q (rcPrefix=%q); label drift breaks the RC picker",
					launchRC, respawnRC, c.rcPrefix)
			}
		})
	}
}
