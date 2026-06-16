package keeper

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
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

// operatorActiveWindow bounds how recently a tmux client must have had keyboard
// activity to count as an actively-engaged human operator (Refs: hk-0t5s).
//
// A client whose last keystroke is older than this is treated as NOT present —
// it is the hallmark of the operator's remote-control / iOS-mobile channel,
// whose input reaches Claude directly and NEVER passes through the tmux client,
// so that client's `#{client_activity}` is frozen at attach time even while the
// operator drives the session. The window is generous because it only governs
// the genuinely-local-typist case (a remote-control attach is always stale
// regardless of window size); 5 minutes never clobbers a human typing into the
// pane yet lifts the permanent warn-only suppression the bare any-client probe
// imposed under the operator's mobile workflow.
const operatorActiveWindow = 5 * time.Minute

// OperatorAttached reports whether a human operator is ACTIVELY attached to the
// tmux session that owns target — i.e. some client has had keyboard activity
// within operatorActiveWindow. It runs
// `tmux list-clients -t <target> -F '#{client_activity}'` and reports true when
// any client's last-activity timestamp is recent.
//
// This is the production default for CyclerConfig.OperatorAttachedFn (hk-6qf):
// when an operator is actively typing into the pane, the keeper's reset-cycle
// injection would race the operator's keystrokes and could clobber an in-flight
// turn — so the cycle suppresses injection and falls back to warn-only.
//
// The previous probe counted ANY attached client, which permanently pinned the
// captain pane to warn-only under the operator's iOS / `claude --remote-control`
// workflow: a passive terminal stays attached while the operator drives via the
// remote-control channel, so a bare attach was an over-suppression (hk-0t5s,
// ~2265 false operator_attached suppressions). `#{client_activity}` advances
// only on keystrokes through that tmux client — not on pane output — so a
// remote-control / idle attach is reliably distinguishable from a live typist.
//
// A non-zero exit (the session does not exist, or no tmux server is running) is
// treated as NOT attached — fail-open — so a transient tmux error never
// permanently suppresses the reset cycle that protects against context
// exhaustion.
//
// target accepts any tmux target form (session name, "session:window.pane", or
// a "%pane_id"); tmux resolves it to the owning session for client listing.
func OperatorAttached(target string) bool {
	if target == "" {
		return false
	}
	// context.Background(): synchronous sub-second probe, mirroring tmuxSessionLive.
	//nolint:gosec // G204: target is the resolved tmux target (derived from validated agentName / operator --tmux flag)
	cmd := exec.CommandContext(context.Background(), "tmux", "list-clients", "-t", target, "-F", "#{client_activity}")
	out, err := cmd.Output()
	if err != nil {
		// Session absent / no server / other tmux error → fail-open (not attached).
		return false
	}
	return operatorActiveSince(string(out), time.Now(), operatorActiveWindow)
}

// operatorActiveSince reports whether any tmux client in listClientsOutput (one
// `#{client_activity}` epoch-seconds value per line) had keyboard activity
// within window of now. Empty and unparseable lines are skipped. Pure, so the
// human-vs-remote-control distinction is unit-testable without a live tmux.
func operatorActiveSince(listClientsOutput string, now time.Time, window time.Duration) bool {
	for _, line := range strings.Split(listClientsOutput, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		secs, err := strconv.ParseInt(line, 10, 64)
		if err != nil {
			continue
		}
		if now.Sub(time.Unix(secs, 0)) <= window {
			return true
		}
	}
	return false
}
