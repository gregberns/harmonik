package lifecycle

import (
	"crypto/sha256"
	"fmt"
	"os"
	"syscall"
)

// ProvenanceEnvKey is the environment variable name set on every subprocess
// spawned by the daemon. Its value is the project_hash (see ComputeProjectHash).
// Reading this variable via /proc/<pid>/environ (Linux) identifies the process
// as harmonik-owned and scoped to a specific project.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "setting the environment
// variable HARMONIK_PROJECT_HASH=<project_hash> on every spawned subprocess."
const ProvenanceEnvKey = "HARMONIK_PROJECT_HASH"

// ComputeProjectHash returns the stable project_hash for projectRoot: the first
// 12 hexadecimal characters of SHA-256(realpath(projectRoot)).
//
// The caller MUST pass the canonicalised (realpath) project root so that
// symlinks do not produce different hashes for the same physical directory.
// The function performs the os.Readlink-free SHA-256 computation; callers are
// responsible for resolving symlinks via filepath.EvalSymlinks (or equivalent)
// before calling.
//
// The return type is string (placeholder). TODO(hk-8mup.60): promote to
// core.ProjectHash once the typed wrapper lands in core/.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "The daemon MUST compute a
// stable project_hash at startup as the first 12 hexadecimal characters of
// SHA-256(realpath(project_root))."
func ComputeProjectHash(projectRoot string) string {
	sum := sha256.Sum256([]byte(projectRoot))
	return fmt.Sprintf("%x", sum[:6]) // 6 bytes → 12 lowercase hex chars
}

// ProvenanceEnvVar returns the environment variable assignment string
// "HARMONIK_PROJECT_HASH=<hash>" suitable for appending to cmd.Env.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — provenance marker (i): env
// var on every spawned subprocess.
func ProvenanceEnvVar(hash string) string {
	return ProvenanceEnvKey + "=" + hash
}

// SpawnSysProcAttr returns a *syscall.SysProcAttr that, when applied to an
// exec.Cmd, places the child process into the process group identified by pgid.
// This implements the PGID side of the PL-006a provenance marker.
//
// The returned value sets Setpgid=true and Pgid=pgid. The daemon's caller MUST
// retry once on EACCES (the race where the child has already called execve
// before the parent's setpgid(2) runs; Go's os/exec retries this
// transparently when SysProcAttr is used).
//
// The pgid parameter is int (placeholder). TODO(hk-8mup.60): promote to
// core.PGID once the typed wrapper lands in core/.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "On every handler subprocess
// spawn, the daemon MUST set Go's SysProcAttr{Setpgid: true,
// Pgid: <recorded_pgid>} and MUST retry once on EACCES."
func SpawnSysProcAttr(pgid int) *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Setpgid: true,
		Pgid:    pgid,
	}
}

// SetsidDaemon calls syscall.Setsid() to make the calling process a session
// leader. The resulting PGID equals the daemon's PID at the moment of the
// call. The daemon MUST call this at PL-005 step 0, before spawning any
// subprocess.
//
// Returns the new session ID (equal to the daemon PID post-Setsid) and any
// error. On success, syscall.Getpgrp() will return the same value as
// os.Getpid() immediately after this call.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "The daemon MUST call
// syscall.Setsid() immediately on startup (PL-005 step 0) before spawning any
// subprocess, producing a session whose PGID equals the daemon's PID at that
// moment. This PGID MUST be recorded in the pidfile per PL-002b (line 2)."
func SetsidDaemon() (int, error) {
	sid, err := syscall.Setsid()
	if err != nil {
		return 0, fmt.Errorf("lifecycle: SetsidDaemon: syscall.Setsid: %w", err)
	}
	return sid, nil
}

// RecordedPGID returns the daemon's current process group ID via
// syscall.Getpgrp(). After SetsidDaemon() succeeds, this equals os.Getpid().
// The value returned here is what the daemon writes into the pidfile (line 2
// per PL-002b) and what it passes to SpawnSysProcAttr on every handler spawn.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "This PGID MUST be recorded
// in the pidfile per PL-002b (line 2). On every handler subprocess spawn, the
// daemon MUST set Go's SysProcAttr{Setpgid: true, Pgid: <recorded_pgid>}."
func RecordedPGID() int {
	return syscall.Getpgrp()
}

// TmuxSessionPrefix returns the tmux session name prefix for the given project
// hash: "harmonik-<hash>-". Session names MUST be of the form
// "harmonik-<project_hash>-<session_name>" so the orphan sweep can enumerate
// them by prefix match.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "Scope tmux session names
// (harmonik-<project_hash>-<session_name>)."
func TmuxSessionPrefix(hash string) string {
	return "harmonik-" + hash + "-"
}

// TmuxSessionName returns the full tmux session name for a given project hash
// and logical session name: "harmonik-<hash>-<sessionName>".
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "harmonik-<project_hash>-<session_name>".
func TmuxSessionName(hash, sessionName string) string {
	return TmuxSessionPrefix(hash) + sessionName
}

// MatchesProvenanceMarker reports whether the given process environment (as a
// slice of "KEY=VALUE" strings) carries a valid project_hash provenance marker
// matching wantHash.
//
// On Linux, the daemon reads /proc/<pid>/environ to build env. On darwin, only
// the PGID side of the marker is available (OQ-PL-008); callers on darwin
// SHOULD use PGID matching instead of this function.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "The orphan sweep MUST match
// on the environment variable on Linux and on the PGID on darwin."
func MatchesProvenanceMarker(env []string, wantHash string) bool {
	target := ProvenanceEnvVar(wantHash)
	for _, e := range env {
		if e == target {
			return true
		}
	}
	return false
}

// ReadProcessEnviron reads /proc/<pid>/environ and returns the environment as a
// slice of "KEY=VALUE" strings. Available on Linux only; on other platforms
// this function always returns (nil, os.ErrNotExist).
//
// The /proc path is predictable and derived from the integer pid.
//
// Spec ref: process-lifecycle.md §4.2 PL-006a — "readable via
// /proc/<pid>/environ on Linux."
func ReadProcessEnviron(pid int) ([]string, error) {
	path := fmt.Sprintf("/proc/%d/environ", pid)
	//nolint:gosec // G304: path is /proc/<pid>/environ where pid is an integer; not user-controlled string
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// /proc/<pid>/environ is NUL-separated.
	var entries []string
	for _, entry := range splitNul(data) {
		if entry != "" {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// splitNul splits a NUL-terminated byte slice into strings. Adjacent NUL bytes
// produce empty entries that callers filter.
func splitNul(data []byte) []string {
	var result []string
	start := 0
	for i, b := range data {
		if b == 0 {
			result = append(result, string(data[start:i]))
			start = i + 1
		}
	}
	if start < len(data) {
		result = append(result, string(data[start:]))
	}
	return result
}
