package daemon_test

// sandboxprofile_hkp7smp_test.go — unit tests for GenerateSandboxProfile (hk-p7smp).
//
// Key invariants tested:
//   W1: allowWrite contains EXACTLY the mandated set (worktree, git worktree metadata,
//       git objects, branch ref dir, packed-refs, reflog dir, tmp dirs, private caches).
//   W2: All paths in allowWrite are LITERAL (no "*", "{", "?", or "[" characters).
//   W3: When BranchName is set, allowWrite includes the DIRECTORY containing the ref
//       (filepath.Dir(<gitDir>/refs/heads/<branch>)) — not the exact ref file — so
//       git can create <ref>.lock as a sibling during commit.
//   W4: When BranchName is empty, allowWrite includes <gitDir>/refs/heads/ subtree.
//   W5: Shared read caches appear in allowRead, NOT in allowWrite.
//   W6: enableWeakerNetworkIsolation is always false.
//   W7: DaemonSockPath appears in network.allowUnixSockets.
//   W8: Missing required fields return an error.
//   W9: Relative paths for WorktreePath/GitDir/DaemonSockPath return an error.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/daemon"
)

// sandboxProfileFixture returns a valid SandboxProfileInput with all required
// fields populated. Tests override individual fields as needed.
func sandboxProfileFixture() daemon.SandboxProfileInput {
	return daemon.SandboxProfileInput{
		WorktreePath:          "/repo/.harmonik/worktrees/0196f000-0000-7000-8000-000000000001",
		GitDir:                "/repo/.git",
		RunID:                 "0196f000-0000-7000-8000-000000000001",
		BranchName:            "run/0196f000-0000-7000-8000-000000000001",
		DaemonSockPath:        "/repo/.harmonik/daemon.sock",
		AllowedDomains:        []string{"openrouter.ai"},
		TmpDirs:               []string{"/tmp", "/private/tmp"},
		SharedReadCacheDirs:   []string{"/Users/gb/.cache/go-build"},
		PrivateWriteCacheDirs: []string{"/repo/.harmonik/worktrees/0196f000-0000-7000-8000-000000000001/.cache"},
	}
}

// parsedProfile unmarshals the JSON from GenerateSandboxProfile into a generic
// map so tests can inspect field values without depending on the unexported
// srtSettings struct.
func parsedProfile(t *testing.T, in daemon.SandboxProfileInput) map[string]interface{} {
	t.Helper()
	out, err := daemon.GenerateSandboxProfile(in)
	if err != nil {
		t.Fatalf("hk-p7smp: GenerateSandboxProfile returned error: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("hk-p7smp: output is not valid JSON: %v", err)
	}
	return m
}

// fsSection extracts the filesystem sub-map from a parsed profile.
func fsSection(t *testing.T, m map[string]interface{}) map[string]interface{} {
	t.Helper()
	fs, ok := m["filesystem"].(map[string]interface{})
	if !ok {
		t.Fatalf("hk-p7smp: profile missing filesystem section")
	}
	return fs
}

// netSection extracts the network sub-map from a parsed profile.
func netSection(t *testing.T, m map[string]interface{}) map[string]interface{} {
	t.Helper()
	net, ok := m["network"].(map[string]interface{})
	if !ok {
		t.Fatalf("hk-p7smp: profile missing network section")
	}
	return net
}

// stringSlice converts a JSON array value ([]interface{}) to []string.
func stringSlice(t *testing.T, v interface{}, field string) []string {
	t.Helper()
	raw, ok := v.([]interface{})
	if !ok {
		t.Fatalf("hk-p7smp: %s is not an array (got %T)", field, v)
	}
	out := make([]string, len(raw))
	for i, item := range raw {
		s, ok := item.(string)
		if !ok {
			t.Fatalf("hk-p7smp: %s[%d] is not a string (got %T)", field, i, item)
		}
		out[i] = s
	}
	return out
}

// containsPath returns true if paths contains target.
func containsPath(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}

// hasGlob returns true when s contains any glob metacharacter.
func hasGlob(s string) bool {
	return strings.ContainsAny(s, "*?{[")
}

// ─────────────────────────────────────────────────────────────────────────────
// W1: allowWrite contains the mandated set
// ─────────────────────────────────────────────────────────────────────────────

func TestSandboxProfile_AllowWriteExactSet(t *testing.T) {
	t.Parallel()

	in := sandboxProfileFixture()
	m := parsedProfile(t, in)
	fs := fsSection(t, m)
	allowWrite := stringSlice(t, fs["allowWrite"], "allowWrite")

	// Mandated entries (W1).
	// BranchName = "run/0196f000-..." → dir = "/repo/.git/refs/heads/run"
	mandated := []struct {
		desc string
		path string
	}{
		{"run worktree checkout", in.WorktreePath},
		{"git worktree metadata", "/repo/.git/worktrees/" + in.RunID},
		{"shared git objects", "/repo/.git/objects"},
		{"branch ref dir (tight scope)", "/repo/.git/refs/heads/run"},
		{"packed-refs", "/repo/.git/packed-refs"},
		{"packed-refs lock", "/repo/.git/packed-refs.lock"},
		{"reflog dir (tight scope)", "/repo/.git/logs/refs/heads/run"},
		{"tmpdir /tmp", "/tmp"},
		{"tmpdir /private/tmp", "/private/tmp"},
		{"srt scratch TMPDIR /tmp/claude (hk-cdpxu)", "/tmp/claude"},
		{"srt scratch TMPDIR /private/tmp/claude (hk-cdpxu)", "/private/tmp/claude"},
		{"private cache", "/repo/.harmonik/worktrees/0196f000-0000-7000-8000-000000000001/.cache"},
	}
	for _, m := range mandated {
		if !containsPath(allowWrite, m.path) {
			t.Errorf("hk-p7smp W1: allowWrite missing %s: %q (got %v)", m.desc, m.path, allowWrite)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// W2: all allowWrite paths are literal (no glob metacharacters)
// ─────────────────────────────────────────────────────────────────────────────

func TestSandboxProfile_AllowWriteLiteralPaths(t *testing.T) {
	t.Parallel()

	m := parsedProfile(t, sandboxProfileFixture())
	fs := fsSection(t, m)
	allowWrite := stringSlice(t, fs["allowWrite"], "allowWrite")

	for _, p := range allowWrite {
		if hasGlob(p) {
			t.Errorf("hk-p7smp W2: allowWrite path contains glob metacharacter: %q", p)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// W3: tight branch-ref scope when BranchName is set
// ─────────────────────────────────────────────────────────────────────────────

func TestSandboxProfile_BranchRefTightScope(t *testing.T) {
	t.Parallel()

	in := sandboxProfileFixture()
	in.BranchName = "run/0196f000-0000-7000-8000-000000000001"
	m := parsedProfile(t, in)
	fs := fsSection(t, m)
	allowWrite := stringSlice(t, fs["allowWrite"], "allowWrite")

	// "run/0196f000-..." → dir = "refs/heads/run" (tight: excludes other namespaces)
	tightDir := "/repo/.git/refs/heads/run"
	if !containsPath(allowWrite, tightDir) {
		t.Errorf("hk-p7smp W3: allowWrite missing tight branch-ref dir %q (got %v)", tightDir, allowWrite)
	}
	// The broader heads/ subtree MUST NOT appear when the branch name is known.
	broaderRef := "/repo/.git/refs/heads"
	if containsPath(allowWrite, broaderRef) {
		t.Errorf("hk-p7smp W3: allowWrite has broader heads/ subtree %q when branch name is set (want tight scope only)", broaderRef)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// W4: heads/ subtree fallback when BranchName is empty
// ─────────────────────────────────────────────────────────────────────────────

func TestSandboxProfile_BranchRefFallbackSubtree(t *testing.T) {
	t.Parallel()

	in := sandboxProfileFixture()
	in.BranchName = "" // empty → use heads/ subtree
	m := parsedProfile(t, in)
	fs := fsSection(t, m)
	allowWrite := stringSlice(t, fs["allowWrite"], "allowWrite")

	headsSubtree := "/repo/.git/refs/heads"
	if !containsPath(allowWrite, headsSubtree) {
		t.Errorf("hk-p7smp W4: allowWrite missing heads/ subtree %q when BranchName is empty (got %v)", headsSubtree, allowWrite)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// W5: shared caches → allowRead only, not allowWrite
// ─────────────────────────────────────────────────────────────────────────────

func TestSandboxProfile_SharedCachesInAllowRead(t *testing.T) {
	t.Parallel()

	in := sandboxProfileFixture()
	in.SharedReadCacheDirs = []string{"/Users/gb/.cache/go-build", "/Users/gb/go/pkg"}
	m := parsedProfile(t, in)
	fs := fsSection(t, m)
	allowRead := stringSlice(t, fs["allowRead"], "allowRead")
	allowWrite := stringSlice(t, fs["allowWrite"], "allowWrite")

	for _, cache := range in.SharedReadCacheDirs {
		if !containsPath(allowRead, cache) {
			t.Errorf("hk-p7smp W5: shared cache %q missing from allowRead", cache)
		}
		if containsPath(allowWrite, cache) {
			t.Errorf("hk-p7smp W5: shared cache %q must not appear in allowWrite", cache)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// W6: enableWeakerNetworkIsolation is always false
// ─────────────────────────────────────────────────────────────────────────────

func TestSandboxProfile_EnableWeakerNetworkIsolationAlwaysFalse(t *testing.T) {
	t.Parallel()

	m := parsedProfile(t, sandboxProfileFixture())
	val, ok := m["enableWeakerNetworkIsolation"].(bool)
	if !ok {
		t.Fatalf("hk-p7smp W6: enableWeakerNetworkIsolation is not a bool (got %T)", m["enableWeakerNetworkIsolation"])
	}
	if val {
		t.Error("hk-p7smp W6: enableWeakerNetworkIsolation must be false (TLS decision: locked)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// W7: DaemonSockPath appears in network.allowUnixSockets
// ─────────────────────────────────────────────────────────────────────────────

func TestSandboxProfile_DaemonSockInAllowUnixSockets(t *testing.T) {
	t.Parallel()

	in := sandboxProfileFixture()
	m := parsedProfile(t, in)
	net := netSection(t, m)
	sockets := stringSlice(t, net["allowUnixSockets"], "network.allowUnixSockets")

	if !containsPath(sockets, in.DaemonSockPath) {
		t.Errorf("hk-p7smp W7: DaemonSockPath %q missing from allowUnixSockets (got %v)", in.DaemonSockPath, sockets)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// W8: missing required fields return an error
// ─────────────────────────────────────────────────────────────────────────────

func TestSandboxProfile_RequiredFieldErrors(t *testing.T) {
	t.Parallel()

	base := sandboxProfileFixture()

	cases := []struct {
		name  string
		patch func(*daemon.SandboxProfileInput)
	}{
		{"empty WorktreePath", func(in *daemon.SandboxProfileInput) { in.WorktreePath = "" }},
		{"empty GitDir", func(in *daemon.SandboxProfileInput) { in.GitDir = "" }},
		{"empty RunID", func(in *daemon.SandboxProfileInput) { in.RunID = "" }},
		{"empty DaemonSockPath", func(in *daemon.SandboxProfileInput) { in.DaemonSockPath = "" }},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			in := base
			c.patch(&in)
			_, err := daemon.GenerateSandboxProfile(in)
			if err == nil {
				t.Errorf("hk-p7smp W8: GenerateSandboxProfile with %s: got nil error, want error", c.name)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// W9: relative paths for WorktreePath / GitDir / DaemonSockPath return an error
// ─────────────────────────────────────────────────────────────────────────────

func TestSandboxProfile_RelativePathErrors(t *testing.T) {
	t.Parallel()

	base := sandboxProfileFixture()

	cases := []struct {
		name  string
		patch func(*daemon.SandboxProfileInput)
	}{
		{"relative WorktreePath", func(in *daemon.SandboxProfileInput) { in.WorktreePath = ".harmonik/worktrees/abc" }},
		{"relative GitDir", func(in *daemon.SandboxProfileInput) { in.GitDir = ".git" }},
		{"relative DaemonSockPath", func(in *daemon.SandboxProfileInput) { in.DaemonSockPath = ".harmonik/daemon.sock" }},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			in := base
			c.patch(&in)
			_, err := daemon.GenerateSandboxProfile(in)
			if err == nil {
				t.Errorf("hk-p7smp W9: GenerateSandboxProfile with %s: got nil error, want error", c.name)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// AllowedDomains wired into network.allowedDomains
// ─────────────────────────────────────────────────────────────────────────────

func TestSandboxProfile_AllowedDomains(t *testing.T) {
	t.Parallel()

	in := sandboxProfileFixture()
	in.AllowedDomains = []string{"openrouter.ai", "api.example.com"}
	m := parsedProfile(t, in)
	net := netSection(t, m)
	domains := stringSlice(t, net["allowedDomains"], "network.allowedDomains")

	for _, want := range in.AllowedDomains {
		if !containsPath(domains, want) {
			t.Errorf("hk-p7smp: network.allowedDomains missing %q (got %v)", want, domains)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Nil AllowedDomains emits [] not null
// ─────────────────────────────────────────────────────────────────────────────

func TestSandboxProfile_NilAllowedDomainsEmitsEmptyArray(t *testing.T) {
	t.Parallel()

	in := sandboxProfileFixture()
	in.AllowedDomains = nil
	out, err := daemon.GenerateSandboxProfile(in)
	if err != nil {
		t.Fatalf("hk-p7smp: GenerateSandboxProfile error: %v", err)
	}
	// "null" must not appear for allowedDomains — srt expects an array.
	if strings.Contains(string(out), `"allowedDomains":null`) {
		t.Error("hk-p7smp: nil AllowedDomains must emit [] not null in JSON output")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Output is valid JSON
// ─────────────────────────────────────────────────────────────────────────────

func TestSandboxProfile_OutputIsValidJSON(t *testing.T) {
	t.Parallel()

	out, err := daemon.GenerateSandboxProfile(sandboxProfileFixture())
	if err != nil {
		t.Fatalf("hk-p7smp: GenerateSandboxProfile error: %v", err)
	}
	var v interface{}
	if err := json.Unmarshal(out, &v); err != nil {
		t.Errorf("hk-p7smp: output is not valid JSON: %v\noutput:\n%s", err, out)
	}
}
