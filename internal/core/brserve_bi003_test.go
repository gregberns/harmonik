package core

// br serve forbidden-invocation sensor — beads-integration.md §4.1 BI-003
//
// BI-003: "Harmonik MUST NOT use Beads's `br serve` MCP server. Running
// `br serve` adds another long-lived process the daemon would have to manage;
// the CLI already exposes the authoritative surface (30+ commands) and composes
// with shell plus `jq` for post-processing. Any future proposal to enable
// `br serve` requires fresh justification per the amendment protocol in
// [architecture.md §4.6]."
//
// This test walks the repository source tree and fails if the literal pattern
// `br serve` appears in any Go, shell, Makefile, or YAML file.  Legitimate
// citations of the prohibition (this test file, specs/beads-integration.md,
// docs/, research/, HANDOFF.md) are whitelisted by relative path.
//
// Requirement-traceable bead: hk-872.3.

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// brServePattern matches the literal two-word token "br serve" with word
// boundaries on both sides so it does not trip on "br_serve", "br-server",
// or "librservice".
var brServePattern = regexp.MustCompile(`\bbr serve\b`)

// brServeWhitelistedPaths is the set of repo-relative path prefixes (or exact
// paths) whose content is permitted to contain "br serve".  These are
// documentation-of-the-prohibition or spec-citation contexts, never
// invocations.
//
// Rule: prefix match — any path whose clean relative form starts with one of
// these prefixes is whitelisted.
var brServeWhitelistedPrefixes = []string{
	// The spec that defines the prohibition.
	"specs/beads-integration.md",
	// docs/ — knowledge-base prose; all "br serve" occurrences there are
	// citations of the prohibition or historical discussion.
	"docs/",
	// research/ — discovery-phase corpus; same rationale as docs/.
	"research/",
	// HANDOFF.md — session-to-session handoff notes; may quote the rule.
	"HANDOFF.md",
	// .claude/ — worktrees and project memory; agent-internal, not source.
	".claude/",
	// .kerf/ — kerf planning artifacts; not source.
	".kerf/",
}

// brServeSkippedDirPrefixes is the set of directory prefixes to prune during
// the walk.  These directories are either not source or are copies of the repo
// (worktrees).
var brServeSkippedDirPrefixes = []string{
	".git",
	"node_modules",
	"vendor",
	".claude",
	".kerf",
}

// brServeTargetExtensions lists the file extensions (and exact basenames) that
// the sensor inspects.  Markdown files are NOT in this list: all "br serve"
// occurrences found above live in docs/, specs/, or HANDOFF.md — all
// whitelisted.  Adding .md to this list would require extending the whitelist
// and is not necessary for the enforcement goal.
var brServeTargetExtensions = map[string]bool{
	".go":   true,
	".sh":   true,
	".bash": true,
	".zsh":  true,
	".yaml": true,
	".yml":  true,
}

// brServeTargetBasenames lists exact filenames (no extension match needed) that
// should always be inspected regardless of extension.
var brServeTargetBasenames = map[string]bool{
	"Makefile": true,
}

// TestBRServeBI003_ForbiddenInvocation walks the repository source tree and
// fails if the literal pattern `br serve` appears in any Go, shell, Makefile,
// or YAML file outside the whitelisted paths.
//
// BI-003 citation: beads-integration.md §4.1 BI-003.
// Requirement-traceable bead: hk-872.3.
func TestBRServeBI003_ForbiddenInvocation(t *testing.T) {
	// Locate the repo root relative to this test file using runtime.Caller so
	// the test is resilient to changes in working directory.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed — cannot locate repo root")
	}
	// This file lives at internal/core/brserve_bi003_test.go, so the repo root
	// is two directories up.
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	repoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		t.Fatalf("filepath.Abs(repoRoot): %v", err)
	}

	// thisFileRel is the repo-relative path of this test file.  Violations in
	// this file are permitted — comments reference the forbidden string as part
	// of the prohibition itself.
	thisFileRel, err := filepath.Rel(repoRoot, thisFile)
	if err != nil {
		t.Fatalf("filepath.Rel for thisFile: %v", err)
	}

	var violations []string

	walkErr := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Skip unreadable entries rather than aborting the whole walk.
			return nil
		}

		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return nil
		}
		// Normalise to forward slashes for consistent prefix matching.
		relFwd := filepath.ToSlash(rel)

		if info.IsDir() {
			// Prune skipped directories.
			base := info.Name()
			for _, skip := range brServeSkippedDirPrefixes {
				if base == skip || strings.HasPrefix(relFwd+"/", skip+"/") {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Check whether this path is whitelisted.
		if brServeWhitelisted(relFwd, thisFileRel) {
			return nil
		}

		// Check whether this file is a target for inspection.
		base := filepath.Base(path)
		ext := strings.ToLower(filepath.Ext(base))
		if !brServeTargetExtensions[ext] && !brServeTargetBasenames[base] {
			return nil
		}

		// Scan the file line-by-line for the forbidden pattern.
		f, openErr := os.Open(path) //nolint:gosec // G304: path comes from filepath.Walk within repo root; no user input
		if openErr != nil {
			// Skip unreadable files.
			return nil
		}
		defer f.Close() //nolint:errcheck // read-only scan; close error immaterial

		scanner := bufio.NewScanner(f)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if brServePattern.MatchString(line) {
				violations = append(violations,
					relFwd+":"+itoa(lineNum)+": "+strings.TrimSpace(line),
				)
			}
		}
		return nil
	})

	if walkErr != nil {
		t.Fatalf("filepath.Walk: %v", walkErr)
	}

	if len(violations) > 0 {
		t.Errorf(
			"BI-003 violation: `br serve` must not appear in source/scripts.\n"+
				"Found %d occurrence(s):\n  %s\n"+
				"See beads-integration.md §4.1 BI-003 (hk-872.3).",
			len(violations),
			strings.Join(violations, "\n  "),
		)
	}
}

// brServeWhitelisted reports whether relFwd (a repo-relative forward-slash
// path) is permitted to contain "br serve" occurrences.
func brServeWhitelisted(relFwd, thisFileRel string) bool {
	// The test file itself is always whitelisted.
	if relFwd == filepath.ToSlash(thisFileRel) {
		return true
	}
	for _, prefix := range brServeWhitelistedPrefixes {
		if relFwd == prefix || strings.HasPrefix(relFwd, prefix) {
			return true
		}
	}
	return false
}

// itoa converts an int to its decimal string representation without importing
// strconv or fmt to keep the helper local and avoid polluting the test binary's
// import graph.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
