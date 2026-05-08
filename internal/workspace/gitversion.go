package workspace

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// minGitMajor and minGitMinor are the minimum git version required by the
// workspace manager per WM-ENV-002. The pin derives from three mechanical
// dependencies:
//
//   - git merge --strategy=ort is the default merge algorithm only from 2.34
//     onward (WM-019 requires --strategy=ort explicitly).
//   - git for-each-ref --format '%(trailers:key=X,valueonly=true)' requires
//     2.34's expanded trailers format token.
//   - git worktree repair (introduced in 2.30) stabilized in 2.34.
const (
	minGitMajor = 2
	minGitMinor = 34
)

// GitVersion holds the parsed major.minor from `git --version` output.
//
// Patch level and platform suffixes (e.g., "2.34.1.windows.1") are discarded;
// version comparison is major.minor-only per WM-ENV-002.
type GitVersion struct {
	Major int
	Minor int
}

// String returns the version in "major.minor" form.
func (v GitVersion) String() string {
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}

// meetsMinimum reports whether v satisfies the WM-ENV-002 floor (≥ 2.34).
func (v GitVersion) meetsMinimum() bool {
	if v.Major != minGitMajor {
		return v.Major > minGitMajor
	}
	return v.Minor >= minGitMinor
}

// ParseGitVersion parses the output of `git --version`.
//
// git --version prints a line of the form:
//
//	git version 2.34.1
//	git version 2.34.1.windows.1
//
// ParseGitVersion extracts major and minor from the first dotted numeric token
// after "git version ". It tolerates arbitrary patch/platform suffixes.
// Returns an error if the output does not contain a recognisable version token.
func ParseGitVersion(output string) (GitVersion, error) {
	// Strip leading/trailing whitespace and find "git version " prefix.
	s := strings.TrimSpace(output)
	const prefix = "git version "
	idx := strings.Index(s, prefix)
	if idx < 0 {
		return GitVersion{}, fmt.Errorf("gitversion: cannot find %q in output: %q", prefix, s)
	}
	s = s[idx+len(prefix):]

	// s is now "2.34.1" or "2.34.1.windows.1" or similar.
	// Split on "." and parse the first two numeric fields.
	parts := strings.SplitN(s, ".", 3)
	if len(parts) < 2 {
		return GitVersion{}, fmt.Errorf("gitversion: version token %q has fewer than two dot-separated fields", s)
	}

	major, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return GitVersion{}, fmt.Errorf("gitversion: parse major from %q: %w", parts[0], err)
	}

	// minor may have trailing non-numeric content if patch is absent; take
	// only the leading numeric run.
	minorStr := strings.TrimSpace(parts[1])
	minor, err := strconv.Atoi(minorStr)
	if err != nil {
		return GitVersion{}, fmt.Errorf("gitversion: parse minor from %q: %w", minorStr, err)
	}

	return GitVersion{Major: major, Minor: minor}, nil
}

// DetectGitVersion runs `git --version` using the provided context and returns
// the parsed version. Returns ErrGitVersionTooOld (wrapped) if the detected
// version is below 2.34 (WM-ENV-002).
//
// The ctx is passed to exec.CommandContext so the caller can impose a deadline
// on the subprocess. The daemon calls this once at startup; per-operation
// detection is not required (WM-ENV-002: "detection at startup … so failure is
// surfaced ONCE at daemon-start").
func DetectGitVersion(ctx context.Context) (GitVersion, error) {
	cmd := exec.CommandContext(ctx, "git", "--version")
	out, err := cmd.Output()
	if err != nil {
		return GitVersion{}, fmt.Errorf("gitversion: exec git --version: %w", err)
	}

	v, err := ParseGitVersion(string(out))
	if err != nil {
		return GitVersion{}, err
	}

	if !v.meetsMinimum() {
		return v, fmt.Errorf("gitversion: detected git %s; workspace manager requires git ≥ %d.%d: %w",
			v, minGitMajor, minGitMinor, ErrGitVersionTooOld)
	}

	return v, nil
}
