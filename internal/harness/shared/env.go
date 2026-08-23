package shared

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
