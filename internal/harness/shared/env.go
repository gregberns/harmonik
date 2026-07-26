package shared

// env.go — the "KEY=VALUE" environment-entry helper shared by every harness
// launch-spec builder.
//
// codex (codexlaunchspec.go) and pi (pilaunchspec.go) both strip credential keys
// out of the inherited base environment before handing it to the child process,
// and both need the same key-portion split to do it. Leaving the helper with one
// harness would make the other import it — the back-edge P2 unit E1a exists to
// prevent.
//
// Origin: internal/daemon/codexlaunchspec.go, split out by
// plans/2026-07-21-p2-extraction/E1a-codex-harness.md unit E1a-0.

import "strings"

// EnvKey returns the key portion of a "KEY=VALUE" environment entry.
// Returns the whole string if no "=" is present.
func EnvKey(kv string) string {
	idx := strings.IndexByte(kv, '=')
	if idx < 0 {
		return kv
	}
	return kv[:idx]
}
