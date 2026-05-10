package core

// OperatorUpgradeRejectedReason is the typed discriminator for the `reason`
// field of operator_upgrade_rejected (event-model.md §8.7.11).
//
// Spec ref: event-model.md §8.7.11.
// Bead ref: hk-hqwn.71.
type OperatorUpgradeRejectedReason string

const (
	// OperatorUpgradeRejectedReasonHashMismatch is emitted when the binary's
	// commit hash does not match the expected hash for the upgrade version.
	OperatorUpgradeRejectedReasonHashMismatch OperatorUpgradeRejectedReason = "hash_mismatch"

	// OperatorUpgradeRejectedReasonSchemaIncompatible is emitted when the upgrade
	// binary's schema version is incompatible with the current on-disk schema.
	OperatorUpgradeRejectedReasonSchemaIncompatible OperatorUpgradeRejectedReason = "schema_incompatible"

	// OperatorUpgradeRejectedReasonNotPaused is emitted when an upgrade is
	// attempted while the daemon is not in the `paused` state.
	OperatorUpgradeRejectedReasonNotPaused OperatorUpgradeRejectedReason = "not_paused"
)

// Valid reports whether r is one of the three declared OperatorUpgradeRejectedReason constants.
func (r OperatorUpgradeRejectedReason) Valid() bool {
	switch r {
	case OperatorUpgradeRejectedReasonHashMismatch,
		OperatorUpgradeRejectedReasonSchemaIncompatible,
		OperatorUpgradeRejectedReasonNotPaused:
		return true
	default:
		return false
	}
}
