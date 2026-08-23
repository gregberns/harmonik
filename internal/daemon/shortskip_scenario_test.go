//go:build scenario

package daemon

import "testing"

func skipRealDaemonE2EInShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("real-daemon E2E — excluded from -short (the inner loop). Runs in `make core` and `make test-scenario`.")
	}
}
