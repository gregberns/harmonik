package daemon

import (
	"os"
	"path/filepath"
)

// gocachepath_hk137y6.go — where every Go build cache in the fleet lives.
//
// THE PROBLEM THIS REPLACES. hk-gjbpp's mitigation told every agent to run
// `GOCACHE=$(mktemp -d) go test ./...` so its build could not be corrupted by
// the daemon's `go clean -cache` reap. It worked, and it leaked: `mktemp -d`
// makes a NEW ~220 MiB cache per INVOCATION and nothing ever deletes them. 66
// abandoned caches / 7.3 GiB were measured in one night, which pushed the box
// under the daemon's 10 GiB disk watermark, which silently pauses ALL dispatch
// and presents as a code defect. Three separate crews debugged their own diffs
// against that one cause.
//
// The deeper defect is not the `mktemp` line. It is that WHERE THE CACHE LIVES
// was a convention each agent had to re-obey on every command. Guidance that
// must be remembered per-invocation does not hold — it did not hold, twice, on
// the night it was issued. So the cache location is decided HERE, by the code
// that launches things, and an agent inherits a correct GOCACHE without needing
// to know this file exists.
//
// THREE REQUIRED PROPERTIES (all three, or the fix is a symptom fix):
//
//   - BOUNDED — one directory per agent, REUSED. Not one per command. This also
//     makes builds faster: every mktemp run started from a cold cache.
//   - NON-PURGEABLE — outside `~/Library/Caches`, which macOS reclaims wholesale
//     under disk pressure (hk-pgtbr: a merge gate failed with "could not import
//     bytes/context" because Go's own stdlib cache vanished mid-build, and the
//     daemon recorded that infrastructure fault as a BEAD rejection).
//   - OUTSIDE THE DEFAULT SHARED CACHE — which is what the daemon's reap targets,
//     so an agent's build cannot be wiped mid-verification (hk-gjbpp).
//
// Beads: hk-137y6 (canonical), hk-d6xqn (subsumed), hk-pgtbr, hk-cy4ej, hk-gjbpp.

// goCacheRootDir returns the directory holding every agent's build cache for a
// project. Gitignored (`/.harmonik/*`), on the project volume, and never the
// Go default — so `go clean -cache` against the default cache cannot reach it.
func goCacheRootDir(projectDir string) string {
	return filepath.Join(projectDir, ".harmonik", "go-cache")
}

// GoCacheDirFor returns the FIXED build-cache directory for one agent in one
// project. Fixed is the point: the same agent gets the same path on every
// invocation, so the cache is reused rather than re-created and abandoned.
//
// A blank agent name yields a shared "common" directory rather than a path with
// an empty segment — a caller that cannot name itself still gets a bounded,
// non-purgeable cache, which is the property that matters.
func GoCacheDirFor(projectDir, agent string) string {
	if agent == "" {
		agent = "common"
	}
	return filepath.Join(goCacheRootDir(projectDir), agent)
}

// GoCacheEnvFor returns the environment entries pinning an agent's Go build
// cache, ready to append to a launch spec's Env or an exec.Cmd's Env.
//
// GOCACHE only, deliberately: GOPATH is NOT relocated. GOPATH holds the module
// cache, which is shared, large, and re-downloaded from the network if moved —
// relocating it would trade a disk-leak problem for a bandwidth problem and a
// cold module cache on every agent. The leak is per-invocation BUILD caches;
// that is what this fixes.
func GoCacheEnvFor(projectDir, agent string) []string {
	return []string{"GOCACHE=" + GoCacheDirFor(projectDir, agent)}
}

// ensureGoCacheDir creates an agent's cache directory. Go creates GOCACHE
// itself on first use, so a failure here is not fatal — it is reported to the
// caller only so a launch path can log it rather than silently proceed.
func ensureGoCacheDir(projectDir, agent string) error {
	return os.MkdirAll(GoCacheDirFor(projectDir, agent), 0o755)
}
