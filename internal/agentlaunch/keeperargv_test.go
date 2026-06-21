package agentlaunch

import (
	"strings"
	"testing"

	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// flagValue returns the token immediately following flag in argv, or "".
func flagValue(argv []string, flag string) string {
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == flag {
			return argv[i+1]
		}
	}
	return ""
}

func contains(argv []string, want string) bool {
	for _, a := range argv {
		if a == want {
			return true
		}
	}
	return false
}

func TestKeeperWindowArgv_WarnOnly(t *testing.T) {
	argv := KeeperWindowArgv(KeeperWindowOpts{
		KeeperBin:  "/usr/local/bin/harmonik",
		AgentName:  "alpha",
		Session:    "harmonik-abc123-crew-alpha",
		ProjectDir: "/proj",
		WarnOnly:   true,
	})
	if argv[0] != "/usr/local/bin/harmonik" || argv[1] != "keeper" {
		t.Fatalf("argv head = %v, want <bin> keeper", argv[:2])
	}
	if got := flagValue(argv, "--agent"); got != "alpha" {
		t.Errorf("--agent = %q, want alpha", got)
	}
	want := "harmonik-abc123-crew-alpha:" + ltmux.WindowAgent
	if got := flagValue(argv, "--tmux"); got != want {
		t.Errorf("--tmux = %q, want %q", got, want)
	}
	if !contains(argv, "--warn-only") {
		t.Errorf("warn-only argv missing --warn-only: %v", argv)
	}
	if contains(argv, "--warn-abs-tokens") || contains(argv, "--act-abs-tokens") {
		t.Errorf("warn-only argv must NOT carry an abs band: %v", argv)
	}
	if got := flagValue(argv, "--project"); got != "/proj" {
		t.Errorf("--project = %q, want /proj", got)
	}
}

func TestKeeperWindowArgv_FullBand(t *testing.T) {
	argv := KeeperWindowArgv(KeeperWindowOpts{
		KeeperBin:     "harmonik",
		AgentName:     "captain",
		Session:       "harmonik-abc123-captain",
		WarnOnly:      false,
		WarnAbsTokens: 200000,
		ActAbsTokens:  215000,
		RespawnCmd:    "harmonik captain respawn --tmux x",
	})
	if contains(argv, "--warn-only") {
		t.Errorf("full-band argv must NOT contain --warn-only: %v", argv)
	}
	if got := flagValue(argv, "--warn-abs-tokens"); got != "200000" {
		t.Errorf("--warn-abs-tokens = %q, want 200000", got)
	}
	if got := flagValue(argv, "--act-abs-tokens"); got != "215000" {
		t.Errorf("--act-abs-tokens = %q, want 215000", got)
	}
	if got := flagValue(argv, "--respawn-cmd"); got != "harmonik captain respawn --tmux x" {
		t.Errorf("--respawn-cmd = %q, want the respawn invocation", got)
	}
}

// TestKeeperWindowArgv_FullBandUnsetOmitsAbsFlags asserts the operator-required-config
// behavior: in full-band mode (NOT warn-only) a 0 (unset) band OMITS the abs flags so
// the spawned keeper falls back to the operator's keeper: config (no product default).
func TestKeeperWindowArgv_FullBandUnsetOmitsAbsFlags(t *testing.T) {
	argv := KeeperWindowArgv(KeeperWindowOpts{
		KeeperBin:     "harmonik",
		AgentName:     "captain",
		Session:       "harmonik-abc123-captain",
		WarnOnly:      false,
		WarnAbsTokens: 0, // unset → omit
		ActAbsTokens:  0, // unset → omit
	})
	if contains(argv, "--warn-only") {
		t.Errorf("full-band argv must NOT contain --warn-only: %v", argv)
	}
	if contains(argv, "--warn-abs-tokens") || contains(argv, "--act-abs-tokens") {
		t.Errorf("unset band must OMIT the abs flags (keeper reads operator config): %v", argv)
	}
}

// TestKeeperWindowArgv_FullBandPartialBand asserts each abs flag is independently
// gated on > 0 (a set warn + unset act emits only --warn-abs-tokens).
func TestKeeperWindowArgv_FullBandPartialBand(t *testing.T) {
	argv := KeeperWindowArgv(KeeperWindowOpts{
		KeeperBin:     "harmonik",
		AgentName:     "captain",
		Session:       "s",
		WarnAbsTokens: 200000,
		ActAbsTokens:  0,
	})
	if got := flagValue(argv, "--warn-abs-tokens"); got != "200000" {
		t.Errorf("--warn-abs-tokens = %q, want 200000", got)
	}
	if contains(argv, "--act-abs-tokens") {
		t.Errorf("unset act must omit --act-abs-tokens: %v", argv)
	}
}

func TestKeeperWindowArgv_OmitsEmptyOptionals(t *testing.T) {
	argv := KeeperWindowArgv(KeeperWindowOpts{
		KeeperBin: "harmonik",
		AgentName: "captain",
		Session:   "s",
		WarnOnly:  true,
		// ProjectDir + RespawnCmd empty
	})
	if contains(argv, "--project") {
		t.Errorf("empty ProjectDir must omit --project: %v", argv)
	}
	if contains(argv, "--respawn-cmd") {
		t.Errorf("empty RespawnCmd must omit --respawn-cmd: %v", argv)
	}
}

func TestShellJoinArgv_QuotesSpacesAndSingleQuotes(t *testing.T) {
	got := ShellJoinArgv([]string{"/path with space/harmonik", "keeper", "--agent", "o'brien"})
	if !strings.Contains(got, "'/path with space/harmonik'") {
		t.Errorf("spaces not single-quoted: %q", got)
	}
	if !strings.Contains(got, `'o'\''brien'`) {
		t.Errorf("embedded single-quote not escaped: %q", got)
	}
}
