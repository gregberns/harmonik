package core

// OperatorCommand is the typed discriminator for the `command` field of
// operator_command_rejected (event-model.md §8.7.12) and
// operator_command_failed (event-model.md §8.7.16).
//
// Values per event-model.md §8.7.12 and §8.7.16.
//
// Spec ref: event-model.md §8.7.12, §8.7.16.
// Bead ref: hk-hqwn.71.
type OperatorCommand string

const (
	// OperatorCommandPause is the `pause` operator command.
	OperatorCommandPause OperatorCommand = "pause"

	// OperatorCommandStop is the `stop` operator command.
	OperatorCommandStop OperatorCommand = "stop"

	// OperatorCommandUpgrade is the `upgrade` operator command.
	OperatorCommandUpgrade OperatorCommand = "upgrade"

	// OperatorCommandAttach is the `attach` operator command.
	OperatorCommandAttach OperatorCommand = "attach"

	// OperatorCommandEnqueue is the `enqueue` operator command.
	OperatorCommandEnqueue OperatorCommand = "enqueue"
)

// Valid reports whether c is one of the five declared OperatorCommand constants.
func (c OperatorCommand) Valid() bool {
	switch c {
	case OperatorCommandPause, OperatorCommandStop, OperatorCommandUpgrade,
		OperatorCommandAttach, OperatorCommandEnqueue:
		return true
	default:
		return false
	}
}
