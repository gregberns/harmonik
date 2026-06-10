package keeper

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os/exec"
	"path/filepath"
)

// HarmonikSessionName returns the conventional tmux session name for a
// harmonik-managed agent: "harmonik-<hash12>-<agentName>", where hash12 is
// the first 12 hexadecimal characters of SHA-256(realpath(projectDir)).
//
// This mirrors lifecycle.TmuxSessionName but avoids importing the lifecycle
// package (depguard: keeper MUST only import $gostd, core, eventbus, and
// self per hk-ekap1 / hk-fzzc6).
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "harmonik-<project_hash>-<session_name>".
func HarmonikSessionName(projectDir, agentName string) string {
	resolved, err := filepath.EvalSymlinks(projectDir)
	if err != nil {
		resolved = projectDir
	}
	sum := sha256.Sum256([]byte(resolved))
	hash12 := fmt.Sprintf("%x", sum[:6])
	return "harmonik-" + hash12 + "-" + agentName
}

// ResolveTmuxTarget determines the effective tmux target for a keeper session.
//
// Priority:
//  1. explicit — if non-empty, returned as-is (caller-supplied --tmux flag).
//  2. convention — derives "harmonik-<hash12>-<agentName>" and verifies the
//     session exists in tmux; returns it if live.
//  3. "" — no usable target; caller proceeds without tmux injection.
//
// sessionExistsFn may be nil, in which case a real tmux display-message check
// is performed. Inject a stub for unit tests.
func ResolveTmuxTarget(projectDir, agentName, explicit string, sessionExistsFn func(string) bool) string {
	if explicit != "" {
		return explicit
	}
	if agentName == "" || projectDir == "" {
		return ""
	}
	candidate := HarmonikSessionName(projectDir, agentName)
	if sessionExistsFn == nil {
		sessionExistsFn = tmuxSessionLive
	}
	if sessionExistsFn(candidate) {
		return candidate
	}
	return ""
}

// tmuxSessionLive reports whether a tmux session with the given name is live by
// running `tmux has-session -t "=<name>"` — exits 0 only if a session whose
// name EXACTLY equals sessionName exists.
//
// Two deliberate choices, both validated by the integration test in
// tmuxresolve_integration_test.go (hk-2ojne):
//
//   - has-session, NOT display-message. `tmux display-message -t <name>` exits 0
//     even for a NONEXISTENT target — it silently falls back to the current
//     client's session — so it returns a false positive whenever a tmux server
//     has any attached client (the normal daemon-under-supervisor environment).
//     `has-session` exits non-zero for an absent session, which is the liveness
//     signal we actually want.
//   - the "=" exact-match anchor. Without it, tmux `-t <name>` does prefix/fuzzy
//     matching (e.g. "captai" would match a live "captain"), so resolution could
//     latch onto the wrong session. "=<name>" forces an exact name match.
func tmuxSessionLive(sessionName string) bool {
	// context.Background() is appropriate: this is a synchronous, sub-second
	// liveness probe with no caller-supplied cancellation context (the public
	// ResolveTmuxTarget signature does not thread one through).
	//nolint:gosec // G204: sessionName is derived from projectDir (filepath-resolved) + validated agentName
	cmd := exec.CommandContext(context.Background(), "tmux", "has-session", "-t", "="+sessionName)
	return cmd.Run() == nil
}
