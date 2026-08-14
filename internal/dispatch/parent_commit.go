package dispatch

import (
	"encoding/hex"
	"errors"
)

// ValidateParentCommit accepts one canonical Git SHA-1 or SHA-256 object ID.
func ValidateParentCommit(value string) error {
	if len(value) != 40 && len(value) != 64 {
		return errors.New("dispatch: parent_commit must contain 40 or 64 lowercase hexadecimal characters")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || hex.EncodeToString(decoded) != value {
		return errors.New("dispatch: parent_commit must contain 40 or 64 lowercase hexadecimal characters")
	}
	return nil
}
