// Package hermetic makes a Go test binary independent of the machine it runs
// on.
//
// WHY THIS EXISTS. A test that reads the operator's home directory, the
// operator's global gitconfig, or the operator's Claude state does not test the
// code — it tests the machine. It gives a different answer on a different box,
// and an outside assessor then cannot tell a real regression from a host
// difference. Three measured examples, all in the core package set:
//
//   - A global gitconfig with commit.gpgsign=true is a common and sane setting.
//     It failed 440 tests across six core packages, because every git fixture in
//     the tree calls `git commit` and none of them turn signing off.
//   - internal/harness/claude took an exclusive flock on the operator's real
//     ~/.claude.json and appended 22 dead project entries per run. The real file
//     on the development machine had grown to 6426 entries, 4459 of them dead
//     paths under the Go test temp directory.
//   - internal/daemon read the operator's real ~/.claude/projects transcript
//     store, which was 1.9 GB on the development machine and is empty on a fresh
//     one.
//
// HOW TO REPRODUCE THE FIRST NUMBER, because a figure nobody can re-derive is
// worse than no figure. It is one run of the CORE_PKGS list on the tree AS IT
// WAS BEFORE this package existed — f5dd00f7b, no TestMain anywhere but
// internal/daemon and internal/core:
//
//	HOME=<empty dir> with a .gitconfig setting commit.gpgsign=true and
//	gpg.program=/nonexistent/gpg, GOCACHE and GOMODCACHE left at their real
//	values, HARMONIK_CLAUDE_CONFIG_PATH unset, then
//	go test -count=1 $(CORE_PKGS)
//
// That gave 440 lines matching `--- FAIL`, 6 packages reporting FAIL, and 400
// occurrences of "gpg failed to sign the data" — every failure, one cause.
//
// The count depends on which tree you start from, so it is not a constant. A
// second measurement taken by removing the new TestMains from the ALREADY-FIXED
// tree reported 248 failures across 8 packages, and is equally correct about its
// own question: that tree also has TestMains in cmd/harmonik-twin-codex and
// internal/sentinel, which the pristine one does not. Quote the method with the
// number, or quote neither.
//
// HOW IT IS USED. One line in a package's TestMain:
//
//	func TestMain(m *testing.M) { hermetic.Main(m) }
//
// A package that already has a TestMain calls Setup instead and keeps its own
// work:
//
//	func TestMain(m *testing.M) {
//	    cleanup := hermetic.Setup()
//	    ... package-specific setup ...
//	    code := m.Run()
//	    cleanup()
//	    os.Exit(code)
//	}
//
// WHY A SEPARATE PACKAGE. This package imports only the standard library.
// internal/testhelpers imports internal/core, so an in-package test file
// (`package core`) cannot import internal/testhelpers without an import cycle.
// Keeping the hermetic setup in a leaf means any package in the tree can take
// it from its TestMain, with no cycle and no ordering rule to remember.
//
// WHAT IT DOES NOT DO. It does not reassign HOME. A test binary that shells out
// to the Go toolchain resolves its build cache through HOME, so moving HOME
// would trade one host coupling for a slower and less obvious one. Each seam is
// redirected by its own variable instead, so the redirect is visible in the
// environment rather than implied.
//
// The cost of that choice is that this list is not a proof of coverage. About a
// dozen os.UserHomeDir calls remain in production code, several of them in core
// packages, and any one of them can reintroduce a host difference. What this
// package removes is the couplings that were measured; what it cannot do is
// promise there are no others. hk-core-host-coupling-residue-a5dic carries the
// ones that were found and deliberately left.
//
// THE CLAUDE PATHS ARE ONLY SET WHEN THEY ARE UNSET. An outer harness — CI, a
// bisect script, an operator reproducing a failure — stays in control of those,
// because redirecting them somewhere else is still isolation.
//
// THE GIT VARIABLES ARE SET UNCONDITIONALLY, and the ones in clearedEnv are
// always removed. Those are not redirections an outer harness can improve on:
// an inherited value there means the fixtures read the host again, which is the
// defect this package exists to remove.
package hermetic

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// gitConfigContents is the controlled global gitconfig every test fixture sees.
//
// Each entry answers a host setting that changes what `git` does:
//
//	user.*             a fresh machine has no identity, and `git commit` refuses
//	                   without one.
//	commit/tag.gpgsign signing is a common operator setting and fails in a test
//	                   fixture, which has no key and no agent.
//	init.defaultBranch the operator's default branch name leaks into fixtures
//	                   that do not pass --initial-branch.
//	init.templateDir   a global template directory installs hooks into every
//	                   fixture repository.
//	core.hooksPath     a global hooks path runs the operator's hooks in every
//	                   fixture repository.
//	core.excludesFile  a global ignore file can hide a file the fixture just
//	                   added, so `git add` silently stages nothing.
//	core.autocrlf      line-ending rewriting changes file content and therefore
//	                   every content hash the fixtures assert on.
//	gc.auto            background repacking races the assertions.
//	advice.*           advice text on stderr is noise the fixtures may match on.
const gitConfigContents = `[user]
	name = Harmonik Test
	email = test@harmonik.local
[commit]
	gpgsign = false
[tag]
	gpgsign = false
[init]
	defaultBranch = main
	templateDir =
[core]
	hooksPath =
	excludesFile =
	autocrlf = false
[gc]
	auto = 0
[advice]
	detachedHead = false
`

// clearedEnv is the set of variables a test binary must never inherit.
//
// Each one is read by production code that the tests drive, and each one changes
// what that code does. They are exported in a live harmonik agent pane and unset
// on an assessor's machine, so leaving them in place is the same defect as
// reading the operator's home: the suite answers a different question depending
// on where it runs.
//
// Held back deliberately: ANTHROPIC_API_KEY, CLAUDE_CODE_OAUTH_TOKEN and
// HARMONIK_REQUIRE_REMOTE_E2E. Those gate opt-in end-to-end tests that cost money
// or need a remote worker. Clearing them would silently disable a test an
// operator asked for. They are host-coupled and they are reported as such; the
// answer there is a decision about what the core gate should run, not an env
// var this helper may take away.
var clearedEnv = []string{
	// git, and this group is the sharpest of the list: several of these OUTRANK
	// GIT_CONFIG_GLOBAL, so leaving one in place defeats the gitconfig redirect
	// on its own.
	//
	// GIT_CONFIG_COUNT is the head of the GIT_CONFIG_KEY_n / GIT_CONFIG_VALUE_n
	// family. git ignores the numbered pairs when the count is absent, so
	// clearing the one variable disables all of them.
	"GIT_CONFIG_COUNT",
	// A fixed author or committer date makes every commit hash host-dependent,
	// and the fixtures assert on hashes.
	"GIT_AUTHOR_DATE",
	"GIT_COMMITTER_DATE",
	// These three redirect a fixture's repository out from under it.
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_INDEX_FILE",
	// Reinstalls the hooks that init.templateDir= in the controlled config exists
	// to suppress.
	"GIT_TEMPLATE_DIR",
	// Running the suite inside tmux must not change what it tests.
	"TMUX",
	"TMUX_PANE",
	// Agent identity. `harmonik comms send` falls back to $HARMONIK_AGENT when
	// --from is absent, so a test of "--from is required" passes in an agent pane
	// and fails everywhere else.
	"HARMONIK_AGENT",
	"HARMONIK_RUN_ID",
	"HARMONIK_SESSION_ID",
	// Project routing. Both are read as a default project directory.
	"HK_PROJECT",
	"HARMONIK_PROJECT",
	// Dispatch behaviour.
	"HARMONIK_DISABLE_EAGER_REFILL",
	"HARMONIK_MAX_CONCURRENT_SESSIONS",
	// The orphan sweep. HARMONIK_SWEEP_CLAUDE_WORKTREES=1 turns it from a dry run
	// into `git worktree remove --force --force` plus os.RemoveAll.
	"HARMONIK_SWEEP_CLAUDE_WORKTREES",
	"HARMONIK_CLAUDE_WORKTREE_MAX_AGE_DAYS",
	// Extra stdout that an NDJSON reader in a test can choke on.
	"HARMONIK_DEBUG_WIRING",
	// Twin-binary discovery.
	"HARMONIK_TWIN_SEARCH_PATH",
	// Claude's own directory override. HARMONIK_CLAUDE_CONFIG_PATH and
	// HARMONIK_CLAUDE_PROJECTS_DIR outrank it, but clearing it means the resolved
	// path does not depend on whether the operator exports it.
	"CLAUDE_CONFIG_HOME",
}

// Setup redirects every known host seam at a fresh temporary directory and
// returns a function that removes it.
//
// It is safe to call from TestMain before m.Run, which is the only place a test
// binary may mutate its own environment: after m.Run starts, a parallel test
// could observe a half-applied change.
func Setup() (cleanup func()) {
	root, err := os.MkdirTemp("", "harmonik-hermetic-")
	if err != nil {
		// A test binary that cannot isolate itself must not run: it would write
		// into the operator's real home and report a result that depends on it.
		die("MkdirTemp", err)
	}

	gitConfig := filepath.Join(root, "gitconfig")
	if writeErr := os.WriteFile(gitConfig, []byte(gitConfigContents), 0o600); writeErr != nil {
		die("write gitconfig", writeErr)
	}

	restoreEnv := installEnv(root, gitConfig)

	return func() {
		restoreEnv()
		if rmErr := os.RemoveAll(root); rmErr != nil {
			fmt.Fprintf(os.Stderr, "hermetic: cleanup %s: %v\n", root, rmErr)
		}
	}
}

// installEnv points every host seam at root and returns a function that puts the
// environment back exactly as it was found.
//
// The restore half matters less than it looks: a TestMain calls Setup once and
// the process then exits. It is here because a helper that leaves a half-changed
// environment behind cannot be called twice, and the tests for this package have
// to call it more than once to test it at all.
func installEnv(root, gitConfig string) (restoreEnv func()) {
	env := newEnvSnapshot()

	// git. GIT_CONFIG_GLOBAL replaces ~/.gitconfig and ~/.config/git/config.
	// GIT_CONFIG_SYSTEM replaces /etc/gitconfig; GIT_CONFIG_NOSYSTEM covers the
	// git versions that predate GIT_CONFIG_SYSTEM (before 2.32).
	//
	// SET UNCONDITIONALLY, unlike everything below. Deferring to an outer value
	// here does not hand control to the outer harness, it removes the protection
	// entirely: a CI runner that exports GIT_CONFIG_GLOBAL for its own reasons
	// turns git isolation off for every package at once, and the failures then
	// read as product regressions rather than as host contamination. Measured at
	// 179 failing tests across four packages. There is no version of "the outer
	// value wins" that leaves the fixtures isolated, so it does not win.
	env.set("GIT_CONFIG_GLOBAL", gitConfig)
	env.set("GIT_CONFIG_SYSTEM", os.DevNull)
	env.set("GIT_CONFIG_NOSYSTEM", "1")

	// Claude state. Both variables are read by production code:
	// internal/workspace.defaultClaudeGlobalConfigPath reads the first, and
	// internal/workspace.DefaultClaudeProjectsDir reads the second.
	env.setIfUnset("HARMONIK_CLAUDE_CONFIG_PATH", filepath.Join(root, ".claude.json"))
	env.setIfUnset("HARMONIK_CLAUDE_PROJECTS_DIR", filepath.Join(root, "claude-projects"))

	for _, key := range clearedEnv {
		env.clear(key)
	}

	return env.restore
}

// envSnapshot records each variable's value the first time it is touched, so
// restore can put the environment back exactly as it was found.
type envSnapshot struct {
	// prior maps a variable name to its original value, or to nil when the
	// variable was not set at all.
	prior map[string]*string
}

func newEnvSnapshot() *envSnapshot {
	return &envSnapshot{prior: map[string]*string{}}
}

// remember records the current value of key, once. A second call is a no-op, so
// the first observation is the one restore puts back.
func (s *envSnapshot) remember(key string) {
	if _, seen := s.prior[key]; seen {
		return
	}
	if value, set := os.LookupEnv(key); set {
		s.prior[key] = &value
		return
	}
	s.prior[key] = nil
}

// set assigns value whether or not key is already present. Used where an
// inherited value would defeat the isolation rather than redirect it.
func (s *envSnapshot) set(key, value string) {
	s.remember(key)
	if err := os.Setenv(key, value); err != nil {
		die("setenv "+key, err)
	}
}

// setIfUnset assigns value only when key is absent, so an outer harness that has
// already chosen a path keeps it.
func (s *envSnapshot) setIfUnset(key, value string) {
	if _, present := os.LookupEnv(key); present {
		s.remember(key)
		return
	}
	s.set(key, value)
}

// clear removes key unconditionally. Used for the variables in clearedEnv, where
// inheriting any value at all is the defect.
func (s *envSnapshot) clear(key string) {
	s.remember(key)
	if err := os.Unsetenv(key); err != nil {
		die("unsetenv "+key, err)
	}
}

func (s *envSnapshot) restore() {
	for key, value := range s.prior {
		var err error
		if value == nil {
			err = os.Unsetenv(key)
		} else {
			err = os.Setenv(key, *value)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "hermetic: restore %s: %v\n", key, err)
		}
	}
}

// die aborts the test binary. Every caller is a step that must succeed for the
// binary to be isolated at all; continuing would run the suite against the
// operator's real home and report the result as if it meant something.
func die(what string, err error) {
	fmt.Fprintf(os.Stderr, "hermetic: %s: %v\n", what, err)
	os.Exit(1)
}

// Main is the whole body of a TestMain for a package that has no other setup:
//
//	func TestMain(m *testing.M) { hermetic.Main(m) }
//
// It does not return: it calls os.Exit with the test binary's status.
func Main(m *testing.M) {
	cleanup := Setup()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
