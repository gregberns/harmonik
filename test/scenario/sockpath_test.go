//go:build scenario

package scenario

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/gregberns/harmonik/internal/lifecycle"
)

// TestSockBinds asserts that the socket path scenarioFixtureProjectDir approves
// is the socket path that actually binds.
//
// The bug this defends against was a disagreement between two strings. The
// fixture measured the UNRESOLVED temp path against the limit, callers resolved
// it before building the socket path, and on darwin resolving adds 8 bytes
// (/var → /private/var). So the guard passed on a 98-byte string while the
// kernel was handed a 106-byte one and refused it — surfacing as "isolated
// daemon socket did not start", which reads as a broken daemon rather than an
// over-long path.
//
// Asserting the length alone would re-implement the guard and agree with it by
// construction, so this binds a real listener as well. Be clear about which
// half does the work: the resolved-directory assertion is what fires on the
// defect above. The bind is belt and braces — it settles whether the kernel
// actually accepts the path, and it would catch a length rule that drifted away
// from what the kernel enforces.
func TestSockBinds(t *testing.T) {
	t.Parallel()

	project := scenarioFixtureProjectDir(t)

	resolved, err := filepath.EvalSymlinks(project.projectDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", project.projectDir, err)
	}
	if resolved != project.projectDir {
		t.Errorf("scenarioFixtureProjectDir returned an unresolved path:\n  got      %q (%d bytes)\n  resolves %q (%d bytes)\nthe guard and the bind would measure different strings",
			project.projectDir, len(project.projectDir), resolved, len(resolved))
	}

	if err := lifecycle.ValidateSocketPathLength(project.sockPath); err != nil {
		t.Errorf("fixture returned a socket path its own guard rejects: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(project.sockPath), 0o755); err != nil { //nolint:gosec // G301: matches .harmonik dir conventions
		t.Fatalf("MkdirAll: %v", err)
	}
	ln, err := net.Listen("unix", project.sockPath)
	if err != nil {
		t.Fatalf("bind %q (%d bytes): %v\nthis is the failure the length guard exists to prevent",
			project.sockPath, len(project.sockPath), err)
	}
	if err := ln.Close(); err != nil {
		t.Errorf("close listener: %v", err)
	}
}
