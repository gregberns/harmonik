package lifecycle

// processenv_darwin.go — per-process environment lookup on darwin, backed by
// `ps -Ewww`.
//
// darwin has no /proc, but `ps -E` prints each process's exec-time environment
// appended to its command column, for every process owned by the SAME UID.
//
// Two populations print no environment, and both are fail-closed boundaries —
// an unreadable environment yields ok=false and the caller leaves the process
// alone:
//
//   - processes owned by another user;
//   - SIP-protected platform binaries (verified: /bin/sleep prints its command
//     and nothing else, while an ordinary user-built binary in the same shell
//     prints its full environment).
//
// The second is why callers must not reach for this to identify an arbitrary
// process. It is sound for the population it is used on — harmonik's own
// binaries, which are user-built and therefore readable.
//
// Used by the launch-path watcher reap (agentwatcherreap.go) to prove a kill
// candidate belongs to THIS project before signalling it.

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// darwinEnvAssignmentRe matches the start of a shell-style environment
// assignment in `ps -E` output: a start-of-string or whitespace boundary
// followed by IDENT=.
//
// The leading boundary is what keeps command-line flags out of the results:
// in "--agent=mike" the character before "agent=" is '-', not whitespace, so
// the flag is not mistaken for an environment assignment.
var darwinEnvAssignmentRe = regexp.MustCompile(`(^|\s)([A-Za-z_][A-Za-z0-9_]*)=`)

// processEnvValue returns the value environment variable key held at exec time
// for pid.
//
// ok is false whenever the value cannot be established for ANY reason — the
// process is gone, it belongs to another UID, or the key is absent. Callers
// MUST read !ok as "cannot prove this process is ours" and leave the process
// alone; this function never guesses.
//
// Value boundaries. `ps -E` flattens the environment into a single
// space-separated column, so a value that itself contains whitespace cannot be
// delimited unambiguously. This implementation ends a value at the next
// IDENT= assignment, which means a whitespace-bearing value is returned
// over-long rather than truncated short. Either way it fails to compare equal
// to the caller's expected value, so the ambiguity resolves to "no match" —
// fail-closed, never a false positive.
func processEnvValue(ctx context.Context, pid int, key string) (value string, ok bool) {
	//nolint:gosec // G204: pid is an integer; key is not passed to the command
	out, err := exec.CommandContext(ctx, "ps", "-Ewww", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", false
	}
	line := string(out)

	matches := darwinEnvAssignmentRe.FindAllStringSubmatchIndex(line, -1)
	// Scan from the end: the environment is appended AFTER the command, so the
	// last assignment bearing this key is the environment's, not an argument
	// that happens to look like one.
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		if line[m[4]:m[5]] != key {
			continue
		}
		end := len(line)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		return strings.TrimSpace(line[m[1]:end]), true
	}
	return "", false
}
