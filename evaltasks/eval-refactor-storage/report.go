package refactorstorage

import "fmt"

// Report renders a summary of a store's contents.
func Report(store Store) string {
	return fmt.Sprintf("entries=%d", store.Len())
}
