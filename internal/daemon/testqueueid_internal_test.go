package daemon

import "github.com/google/uuid"

func newTestQueueID() string {
	return uuid.Must(uuid.NewV7()).String()
}
