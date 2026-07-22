package daemon

// codexhome_testleak_hkb7rt7_test.go — the daemon test binary CANNOT reach the
// operator's real ~/.codex (hk-b7rt7).
//
// Purpose. resolveCodexHome normalises an empty codexHome to "$HOME/.codex".
// That default is correct and intended for PRODUCTION — harnessregistry.go
// registers NewCodexHarness("", "") and relies on it. It is a hazard in TESTS,
// because two daemon guards WRITE to whatever that resolves to:
//
//   - codexbillingguard.go materializeForcedLoginMethod: os.MkdirAll(codexHome)
//     then os.WriteFile(<home>/config.toml) of forced_login_method = "chatgpt".
//     A test that passes "" rewrites the operator's LIVE ~/.codex/config.toml —
//     silently flipping the key if the operator had set anything else.
//   - codexwalguard.go cleanCodexStaleWAL: copies <home>/state_*.sqlite-wal into
//     <home>/.wal-backup-<ns>/ and then os.Remove()s the sidecars.
//
// The observed symptom was TestCodexHarness_LaunchSpec_InitialDelegates failing
// once and then passing on re-run: it evaluated the fail-closed billing guard
// against the live auth state of a running codex fleet on the same box.
//
// Protection. init() below captures the real ~/.codex ONCE, before any test can
// move HOME, and installs it as resolveCodexHome's quarantined path with a
// throwaway redirect target. resolveCodexHome then cannot return the real path
// for ANY input — "", the literal real path, or a stale copy-paste — so a future
// test that forgets to pass t.TempDir() is structurally unable to touch the live
// install rather than merely discouraged from it. Production is unaffected: the
// quarantine pair is empty in every non-test binary, making quarantineCodexHome
// an identity function there.
//
// Coverage:
//   - the quarantine is ARMED (sentinel: fails if the init below is removed).
//   - resolveCodexHome("") does not resolve to the real ~/.codex and lands under
//     the quarantine dir instead.
//   - resolveCodexHome(<literal real ~/.codex>) is also diverted — an explicit
//     path is no escape hatch.
//   - a full CodexHarness("", "").LaunchSpec — the exact shape that was rewriting
//     the operator's config.toml — puts CODEX_HOME at the quarantine dir and
//     lands the billing guard's forced_login_method write there.
//   - the seam does NOT break the legitimate flows: an explicit temp CODEX_HOME
//     passes through untouched, and the hk-d170r-style t.Setenv("HOME", tmp)
//     isolation still resolves to <tmp>/.codex (not to the quarantine dir).
//   - production shape: with an empty quarantine pair the helper is identity, so
//     "" → $HOME/.codex is preserved for the daemon.
//
// This file deliberately performs NO write and NO stat against the real ~/.codex:
// a leak test that touched the live install to prove the point would cause the
// harm it exists to prevent. Every assertion is on resolved PATHS and on files
// under the quarantine/temp roots.
//
// Does NOT cover: symlinked aliases of the real home (comparison is
// filepath.Clean, not EvalSymlinks); other real-$HOME consumers in this package
// (workloop.go ClaudeProjectsDir, bootsocket.go BandwidthTuner, pibillingguard.go
// piDefaultHome) — those are a separate class reported alongside this bead; and
// sub-packages internal/daemon/{bootconfig,router,scenariotest}, which are
// separate test binaries and do not link this init.
//
// Helper prefix: b7rt7
// Bead ref: hk-b7rt7.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// b7rt7RealCodexHome is the operator's real ~/.codex as it stood at test-binary
// start — captured before any test can t.Setenv("HOME", …). Empty only when the
// home directory could not be resolved at all, in which case there is nothing to
// quarantine.
var b7rt7RealCodexHome string

// init arms the quarantine for the WHOLE daemon test binary. It runs before
// TestMain and before any test, and assigns the package vars exactly once, so no
// parallel test can observe a half-installed quarantine.
//
// An opt-in helper instead of init() would be exactly the "must be remembered
// forever" guard this bead exists to remove.
//
//nolint:gochecknoinits // isolation must precede the first resolveCodexHome call
func init() {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// No resolvable home ⇒ resolveCodexHome's default is the literal
		// "$HOME/.codex" string, which is not a real directory. Nothing to
		// quarantine; leave the pair empty.
		return
	}
	b7rt7RealCodexHome = filepath.Clean(home + "/.codex")

	redirect, mkErr := os.MkdirTemp("", "harmonik-daemon-test-codexhome-")
	if mkErr != nil {
		// Failing loud is correct: running the suite WITHOUT the quarantine is
		// how the operator's live codex install got rewritten in the first place.
		panic("hk-b7rt7: cannot create quarantine CODEX_HOME; refusing to run the " +
			"daemon test binary against the real ~/.codex: " + mkErr.Error())
	}
	codexHomeQuarantinedPath = b7rt7RealCodexHome
	codexHomeQuarantineRedirect = redirect
}

// codexHomeQuarantineCleanup removes the throwaway CODEX_HOME installed by init.
// Invoked by TestMain (daemon_test.go) after m.Run.
func codexHomeQuarantineCleanup() {
	if codexHomeQuarantineRedirect != "" {
		_ = os.RemoveAll(codexHomeQuarantineRedirect)
	}
}

// b7rt7Quarantine returns the armed quarantine pair, failing the test when the
// isolation is not installed. Every case below routes through it so that
// deleting init() reds the whole file rather than silently disarming it.
func b7rt7Quarantine(t *testing.T) (real, redirect string) {
	t.Helper()
	if codexHomeQuarantinedPath == "" || codexHomeQuarantineRedirect == "" {
		t.Fatalf("hk-b7rt7 quarantine is NOT armed: codexHomeQuarantinedPath=%q "+
			"codexHomeQuarantineRedirect=%q; want both non-empty so no test in this "+
			"package can resolve CODEX_HOME to the operator's real ~/.codex "+
			"(regression shape: init() in codexhome_testleak_hkb7rt7_test.go removed "+
			"or no longer assigning the pair)",
			codexHomeQuarantinedPath, codexHomeQuarantineRedirect)
	}
	return codexHomeQuarantinedPath, codexHomeQuarantineRedirect
}

// TestCodexHomeQuarantine_ResolveNeverReturnsRealCodexHome_b7rt7 is the core
// leak assertion: no input to resolveCodexHome yields the operator's real
// ~/.codex.
//
// Regression shape: before this bead, resolveCodexHome("") returned
// $HOME/.codex, and every `NewCodexHarness("", "")` in a test therefore drove
// materializeForcedLoginMethod's MkdirAll+WriteFile at the live install.
func TestCodexHomeQuarantine_ResolveNeverReturnsRealCodexHome_b7rt7(t *testing.T) {
	t.Parallel()

	realHome, redirect := b7rt7Quarantine(t)

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty-codexhome-default-is-diverted",
			input: "",
			want:  redirect,
		},
		{
			name:  "explicit-real-codexhome-is-diverted",
			input: realHome,
			want:  redirect,
		},
		{
			name:  "explicit-real-codexhome-unclean-is-diverted",
			input: realHome + "/./",
			want:  redirect,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := resolveCodexHome(tc.input)
			if got == realHome {
				t.Fatalf("resolveCodexHome(%q) = %q, the operator's REAL codex home; "+
					"want the quarantine dir %q. A test resolving to the real home lets "+
					"materializeForcedLoginMethod rewrite the live ~/.codex/config.toml "+
					"and lets cleanCodexStaleWAL delete live state_*.sqlite-wal sidecars",
					tc.input, got, redirect)
			}
			if got != tc.want {
				t.Errorf("resolveCodexHome(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestCodexHomeQuarantine_HarnessLaunchSpecLandsUnderQuarantine_b7rt7 exercises
// the exact call shape that was leaking — CodexHarness built with an empty
// codexHome, then LaunchSpec, which runs the fail-closed billing guard — and
// asserts BOTH the child env and the guard's on-disk write stay inside the
// quarantine dir.
//
// Regression shape: this is TestCodexHarness_LaunchSpec_InitialDelegates'
// configuration. Its intermittent red was the billing guard being evaluated
// against the live auth state of a running codex fleet.
func TestCodexHomeQuarantine_HarnessLaunchSpecLandsUnderQuarantine_b7rt7(t *testing.T) {
	t.Parallel()

	realHome, redirect := b7rt7Quarantine(t)

	h := NewCodexHarness("", "")
	spawn, err := h.LaunchSpec(handlercontract.RunCtx{
		WorkspacePath: t.TempDir(),
		BeadID:        "hk-b7rt7-quarantine-probe",
		BaseEnv:       []string{"PATH=/usr/bin"},
	})
	if err != nil {
		t.Fatalf("CodexHarness(\"\",\"\").LaunchSpec: %v", err)
	}

	wantEnv := "CODEX_HOME=" + redirect
	badEnv := "CODEX_HOME=" + realHome
	var gotEnv string
	for _, kv := range spawn.Env {
		if strings.HasPrefix(kv, "CODEX_HOME=") {
			gotEnv = kv
		}
	}
	if gotEnv == badEnv {
		t.Fatalf("SpawnSpec.Env carries %q — the child codex would read and write the "+
			"operator's real codex install; want %q", gotEnv, wantEnv)
	}
	if gotEnv != wantEnv {
		t.Errorf("SpawnSpec.Env CODEX_HOME = %q; want %q", gotEnv, wantEnv)
	}

	// The billing guard's write must have landed in the quarantine dir, proving
	// the write path (not just the env string) was diverted.
	cfgPath := filepath.Join(redirect, "config.toml")
	data, readErr := os.ReadFile(cfgPath) //nolint:gosec // G304: MkdirTemp path, not user input.
	if readErr != nil {
		t.Fatalf("billing guard did not materialize %s: %v; want the guard's "+
			"forced_login_method write inside the quarantine dir (if it is absent, the "+
			"write went somewhere else — possibly the real ~/.codex)", cfgPath, readErr)
	}
	if !strings.Contains(string(data), "forced_login_method") {
		t.Errorf("%s = %q; want it to declare forced_login_method (the guard's write)",
			cfgPath, string(data))
	}
}

// TestCodexHomeQuarantine_LegitimateHomesPassThrough_b7rt7 pins the other half
// of the contract: the quarantine intercepts the REAL home only. An explicit
// temp CODEX_HOME and a t.Setenv("HOME", tmp) isolation (the per-test pattern
// hk-d170r introduced) must both keep resolving to their own directories, or the
// seam would silently collapse every test onto one shared codex home.
func TestCodexHomeQuarantine_LegitimateHomesPassThrough_b7rt7(t *testing.T) {
	// NOT parallel: t.Setenv forbids it.
	realHome, redirect := b7rt7Quarantine(t)

	explicit := t.TempDir()
	if got := resolveCodexHome(explicit); got != explicit {
		t.Errorf("resolveCodexHome(%q) = %q; want the explicit path unchanged "+
			"(regression shape: the quarantine swallowing every codexHome, not just "+
			"the real one)", explicit, got)
	}

	fakeHome := t.TempDir()
	t.Setenv("HOME", fakeHome)
	wantFromFakeHome := fakeHome + "/.codex"
	got := resolveCodexHome("")
	if got == redirect {
		t.Errorf("resolveCodexHome(\"\") under HOME=%q = the quarantine dir %q; want %q "+
			"— per-test HOME isolation must still win", fakeHome, redirect, wantFromFakeHome)
	}
	if got != wantFromFakeHome {
		t.Errorf("resolveCodexHome(\"\") under HOME=%q = %q; want %q", fakeHome, got, wantFromFakeHome)
	}
	if got == realHome {
		t.Errorf("resolveCodexHome(\"\") under HOME=%q reached the real codex home %q", fakeHome, realHome)
	}
}

// TestCodexHomeQuarantine_ProductionShapeIsIdentity_b7rt7 asserts the seam is
// inert in production. Production binaries never assign the quarantine pair, so
// the helper must be a pure identity — including for the "$HOME/.codex" default
// that harnessregistry.go's NewCodexHarness("", "") depends on.
//
// Regression shape: a seam that rewrote paths with an empty quarantine pair would
// change where the live daemon writes codex config — a production behaviour
// change this bead must not make.
func TestCodexHomeQuarantine_ProductionShapeIsIdentity_b7rt7(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"",
		"/Users/somebody/.codex",
		"/var/folders/xx/tmpdir/.codex",
		"$HOME/.codex",
	}
	for _, in := range inputs {
		if got := quarantineCodexHomeWith(in, "", ""); got != in {
			t.Errorf("quarantineCodexHomeWith(%q, \"\", \"\") = %q; want %q "+
				"(production must see an identity function)", in, got, in)
		}
	}
}
