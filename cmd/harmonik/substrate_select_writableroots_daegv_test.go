package main

import (
	"reflect"
	"testing"
)

// TestCodexGitCommonDir_daegv pins the git-common-dir derivation (hk-daegv): a
// harmonik linked worktree at <repo>/.harmonik/worktrees/<name> has git common dir
// <repo>/.git. The derivation is pure string logic over "/" so it holds for REMOTE
// (ssh worker) POSIX paths, and returns "" for a cwd that is not under the worktree
// root (adds no git dir — degrades gracefully).
func TestCodexGitCommonDir_daegv(t *testing.T) {
	cases := []struct {
		name string
		cwd  string
		want string
	}{
		{
			name: "linked worktree default layout",
			cwd:  "/home/worker/harmonik/.harmonik/worktrees/hk-abc12",
			want: "/home/worker/harmonik/.git",
		},
		{
			name: "repo at filesystem root-ish path",
			cwd:  "/srv/repo/.harmonik/worktrees/wt-1",
			want: "/srv/repo/.git",
		},
		{
			name: "not a worktree path",
			cwd:  "/home/worker/harmonik",
			want: "",
		},
		{
			name: "empty",
			cwd:  "",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := codexGitCommonDir(tc.cwd); got != tc.want {
				t.Fatalf("codexGitCommonDir(%q) = %q, want %q", tc.cwd, got, tc.want)
			}
		})
	}
}

// TestCodexWorktreeWritableRoots_daegv pins the writable-roots hook (hk-daegv): it
// ALWAYS includes the worktree cwd itself (runtimeWorkspaceRoots REPLACES the
// thread's roots) and appends the git common dir when the cwd matches the worktree
// layout. An empty cwd yields nil (omit the field).
func TestCodexWorktreeWritableRoots_daegv(t *testing.T) {
	cwd := "/home/worker/harmonik/.harmonik/worktrees/hk-abc12"
	got := codexWorktreeWritableRoots(cwd)
	want := []string{cwd, "/home/worker/harmonik/.git"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("codexWorktreeWritableRoots(%q) = %q, want %q", cwd, got, want)
	}

	// A non-worktree cwd still keeps the cwd writable but adds no git dir.
	if got := codexWorktreeWritableRoots("/tmp/plain"); !reflect.DeepEqual(got, []string{"/tmp/plain"}) {
		t.Fatalf("non-worktree cwd: got %q, want [/tmp/plain]", got)
	}

	// Empty cwd → nil (the driver omits runtimeWorkspaceRoots).
	if got := codexWorktreeWritableRoots(""); got != nil {
		t.Fatalf("empty cwd: got %q, want nil", got)
	}
}

// TestCodexSubstrateOptions_WiresWritableRoots_daegv pins that the composition root
// wires the writable-roots hook into codexdriver.Options (hk-daegv) — without it
// the driver never stamps the git common dir and codex's own commit fails EPERM.
func TestCodexSubstrateOptions_WiresWritableRoots_daegv(t *testing.T) {
	router := &codexWorkerRoutingRunner{requireBoundary: true}
	opts, sess := codexSubstrateOptions("codex", router)
	if sess != nil {
		t.Cleanup(func() { _ = sess.Close() }) //nolint:errcheck // test cleanup, unactionable
	}
	if opts.WritableRoots == nil {
		t.Fatal("hk-daegv: Options.WritableRoots is nil — the git-common-dir hook was not wired")
	}
	cwd := "/home/worker/harmonik/.harmonik/worktrees/hk-abc12"
	got := opts.WritableRoots(cwd)
	want := []string{cwd, "/home/worker/harmonik/.git"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hk-daegv: Options.WritableRoots(%q) = %q, want %q", cwd, got, want)
	}
}
