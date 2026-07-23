package main

// release_coverage_test.go — behavior tests for the pure-logic slices of
// `harmonik release`: the verb dispatcher, the shared parseReleaseFlags parser,
// the ledger renderer, the yank/rollback/certify validation exit codes, and the
// orDash/short formatting helpers. All paths here operate on a temp ledger file
// or fail before any git/gh shell-out. The GitHub-promotion path in
// runReleaseCertify (promoteGitHubRelease) is env/PATH-dependent and NOT driven
// here — see the report for the skipped-with-reason list.
//
// captureStateStdout is the shared stdout-capture helper from
// state_cmd_coverage_test.go (same package).

import (
	"reflect"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/release"
)

func TestRunReleaseSubcommand_Dispatch(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no args prints top usage", nil, 0},
		{"help long", []string{"--help"}, 0},
		{"help short", []string{"-h"}, 0},
		{"unrecognised verb", []string{"frobnicate"}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var code int
			out := captureStateStdout(t, func() { code = runReleaseSubcommand(tc.args) })
			if code != tc.want {
				t.Errorf("runReleaseSubcommand(%v) = %d, want %d", tc.args, code, tc.want)
			}
			if tc.want == 0 && tc.name != "unrecognised verb" && !strings.Contains(out, "harmonik release") {
				t.Errorf("expected top usage on stdout; got:\n%s", out)
			}
		})
	}
}

func TestParseReleaseFlags(t *testing.T) {
	dir := t.TempDir()

	t.Run("extracts every flag and positional", func(t *testing.T) {
		proj, pos, flags, err := parseReleaseFlags([]string{
			"--project", dir,
			"--reason", "bad merge",
			"--commit", "deadbeef",
			"--tag", "v9.9.9",
			"--bin", "/usr/local/bin/harmonik",
			"v1.2.3",
		})
		if err != nil {
			t.Fatalf("parseReleaseFlags: %v", err)
		}
		if proj != dir {
			t.Errorf("projectDir = %q, want %q", proj, dir)
		}
		if !reflect.DeepEqual(pos, []string{"v1.2.3"}) {
			t.Errorf("positional = %v, want [v1.2.3]", pos)
		}
		wantFlags := map[string]string{
			"reason": "bad merge",
			"commit": "deadbeef",
			"tag":    "v9.9.9",
			"bin":    "/usr/local/bin/harmonik",
		}
		for k, v := range wantFlags {
			if flags[k] != v {
				t.Errorf("flags[%q] = %q, want %q", k, flags[k], v)
			}
		}
	})

	t.Run("equals-form flags parse identically", func(t *testing.T) {
		proj, _, flags, err := parseReleaseFlags([]string{
			"--project=" + dir, "--reason=r", "--commit=c", "--tag=t", "--bin=b",
		})
		if err != nil {
			t.Fatalf("parseReleaseFlags: %v", err)
		}
		if proj != dir {
			t.Errorf("projectDir = %q, want %q", proj, dir)
		}
		for k, v := range map[string]string{"reason": "r", "commit": "c", "tag": "t", "bin": "b"} {
			if flags[k] != v {
				t.Errorf("flags[%q] = %q, want %q", k, flags[k], v)
			}
		}
	})

	t.Run("help flag sets marker", func(t *testing.T) {
		_, _, flags, err := parseReleaseFlags([]string{"--project", dir, "--help"})
		if err != nil {
			t.Fatalf("parseReleaseFlags: %v", err)
		}
		if flags["help"] != "1" {
			t.Errorf("help marker = %q, want %q", flags["help"], "1")
		}
	})

	t.Run("unknown flag errors", func(t *testing.T) {
		_, _, _, err := parseReleaseFlags([]string{"--project", dir, "--nope"})
		if err == nil || !strings.Contains(err.Error(), "unknown flag") {
			t.Errorf("expected unknown-flag error, got %v", err)
		}
	})
}

func TestRunReleaseLedger_EmptyAndPopulated(t *testing.T) {
	t.Run("empty ledger", func(t *testing.T) {
		dir := t.TempDir()
		var code int
		out := captureStateStdout(t, func() { code = runReleaseLedger([]string{"--project", dir}) })
		if code != 0 {
			t.Fatalf("exit = %d, want 0", code)
		}
		if !strings.Contains(out, "no entries") {
			t.Errorf("expected 'no entries'; got:\n%s", out)
		}
	})

	t.Run("renders each state", func(t *testing.T) {
		dir := t.TempDir()
		entries := []release.ReleaseEntry{
			{Semver: "v0.1.0", CommitHash: "aaaaaaaaaaaaaaaa", Prerelease: true},
			{Semver: "v0.2.0", CommitHash: "bbbbbbbbbbbbbbbb", CertifiedAt: "2026-07-01T00:00:00Z"},
			{Semver: "v0.3.0", CommitHash: "cccccccccccccccc", CertifiedAt: "2026-07-02T00:00:00Z", Yanked: true, YankedReason: "regression"},
		}
		if err := release.SaveLedgerFile(release.LedgerPath(dir), entries); err != nil {
			t.Fatalf("SaveLedgerFile: %v", err)
		}
		var code int
		out := captureStateStdout(t, func() { code = runReleaseLedger([]string{"--project", dir}) })
		if code != 0 {
			t.Fatalf("exit = %d, want 0", code)
		}
		for _, want := range []string{"pre-release", "stable", "yanked", "regression", "aaaaaaaaaaaa"} {
			if !strings.Contains(out, want) {
				t.Errorf("ledger output missing %q; got:\n%s", want, out)
			}
		}
	})

	t.Run("help path", func(t *testing.T) {
		dir := t.TempDir()
		var code int
		out := captureStateStdout(t, func() { code = runReleaseLedger([]string{"--project", dir, "--help"}) })
		if code != 0 {
			t.Fatalf("help exit = %d, want 0", code)
		}
		if !strings.Contains(out, "USAGE") {
			t.Errorf("expected usage text; got:\n%s", out)
		}
	})
}

func TestRunReleaseYank_ExitCodes(t *testing.T) {
	seed := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		entries := []release.ReleaseEntry{
			{Semver: "v0.2.0", CommitHash: "cafebabe", CertifiedAt: "2026-07-01T00:00:00Z"},
		}
		if err := release.SaveLedgerFile(release.LedgerPath(dir), entries); err != nil {
			t.Fatalf("SaveLedgerFile: %v", err)
		}
		return dir
	}

	t.Run("missing reason exits 1", func(t *testing.T) {
		dir := seed(t)
		var code int
		captureStateStdout(t, func() { code = runReleaseYank([]string{"--project", dir, "v0.2.0"}) })
		if code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
	})

	t.Run("wrong positional count exits 1", func(t *testing.T) {
		dir := seed(t)
		var code int
		captureStateStdout(t, func() {
			code = runReleaseYank([]string{"--project", dir, "--reason", "x", "v0.2.0", "extra"})
		})
		if code != 1 {
			t.Errorf("exit = %d, want 1", code)
		}
	})

	t.Run("unknown semver exits 2", func(t *testing.T) {
		dir := seed(t)
		var code int
		captureStateStdout(t, func() {
			code = runReleaseYank([]string{"--project", dir, "--reason", "x", "v9.9.9"})
		})
		if code != 2 {
			t.Errorf("exit = %d, want 2", code)
		}
	})

	t.Run("happy yank exits 0 and persists", func(t *testing.T) {
		dir := seed(t)
		var code int
		captureStateStdout(t, func() {
			code = runReleaseYank([]string{"--project", dir, "--reason", "critical regression", "v0.2.0"})
		})
		if code != 0 {
			t.Fatalf("exit = %d, want 0", code)
		}
		got, err := release.LoadLedgerFile(release.LedgerPath(dir))
		if err != nil {
			t.Fatalf("reload ledger: %v", err)
		}
		if len(got) != 1 || !got[0].Yanked || got[0].YankedReason != "critical regression" {
			t.Errorf("ledger not yanked as expected: %+v", got)
		}
	})

	t.Run("help path exits 0", func(t *testing.T) {
		dir := t.TempDir()
		var code int
		captureStateStdout(t, func() { code = runReleaseYank([]string{"--project", dir, "--help"}) })
		if code != 0 {
			t.Errorf("help exit = %d, want 0", code)
		}
	})
}

func TestRunReleaseCertify_RequiresExactlyOneArg(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"--project", dir},             // zero positionals
		{"--project", dir, "v1", "v2"}, // two positionals
	} {
		var code int
		captureStateStdout(t, func() { code = runReleaseCertify(args) })
		if code != 1 {
			t.Errorf("runReleaseCertify(%v) = %d, want 1", args, code)
		}
	}
}

func TestRunReleaseRollback_NoLastGood(t *testing.T) {
	dir := t.TempDir()
	binTarget := t.TempDir() + "/harmonik-bin"

	var code int
	captureStateStdout(t, func() {
		code = runReleaseRollback([]string{"--project", dir, "--bin", binTarget})
	})
	if code != 4 {
		t.Errorf("rollback with no last-good state = %d, want 4", code)
	}
}

func TestRunReleaseRollback_HelpPath(t *testing.T) {
	dir := t.TempDir()
	var code int
	out := captureStateStdout(t, func() { code = runReleaseRollback([]string{"--project", dir, "--help"}) })
	if code != 0 {
		t.Fatalf("help exit = %d, want 0", code)
	}
	if !strings.Contains(out, "restore the last-good binary") {
		t.Errorf("expected rollback usage; got:\n%s", out)
	}
}

func TestOrDashAndShort(t *testing.T) {
	if got := orDash(""); got != "-" {
		t.Errorf("orDash(\"\") = %q, want %q", got, "-")
	}
	if got := orDash("x"); got != "x" {
		t.Errorf("orDash(\"x\") = %q, want %q", got, "x")
	}
	if got := short("abc"); got != "abc" {
		t.Errorf("short(short input) = %q, want %q", got, "abc")
	}
	if got := short("0123456789abcdef"); got != "0123456789ab" {
		t.Errorf("short(long input) = %q, want first 12 chars", got)
	}
}
