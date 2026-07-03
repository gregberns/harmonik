package refactorstorage

import "fmt"

// Report renders a summary of a store's contents.
//
// It takes a *memStore parameter today — the refactor must widen this to the
// Store interface so this file no longer names the concrete type.
func Report(store *memStore) string {
	return fmt.Sprintf("entries=%d", store.Len())
}
