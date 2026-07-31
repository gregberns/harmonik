package core

// InfrastructurePrerequisite is the typed discriminator for the
// `failed_prerequisite` field of infrastructure_unavailable
// (event-model.md §8.7.15; §6.3 infrastructure_unavailable block).
//
// Spec ref: event-model.md §8.7.15, §6.3.
// Bead ref: hk-hqwn.71.
type InfrastructurePrerequisite string

const (
	// InfrastructurePrerequisiteBrMissing indicates the `br` CLI binary is not
	// found on PATH.
	InfrastructurePrerequisiteBrMissing InfrastructurePrerequisite = "br_missing"

	// InfrastructurePrerequisiteBrTimeout indicates a `br` invocation timed out.
	InfrastructurePrerequisiteBrTimeout InfrastructurePrerequisite = "br_timeout"

	// InfrastructurePrerequisiteBrVersionIncompatible indicates the `br` CLI
	// version is not compatible with the expected version.
	InfrastructurePrerequisiteBrVersionIncompatible InfrastructurePrerequisite = "br_version_incompatible"

	// InfrastructurePrerequisiteBeadsSQLiteLocked indicates the Beads SQLite
	// database is locked and unavailable.
	InfrastructurePrerequisiteBeadsSQLiteLocked InfrastructurePrerequisite = "beads_sqlite_locked"

	// InfrastructurePrerequisiteGitIndexLocked indicates the Git index is locked.
	InfrastructurePrerequisiteGitIndexLocked InfrastructurePrerequisite = "git_index_locked"

	// InfrastructurePrerequisiteHarmonikDirUnwritable indicates the .harmonik
	// directory is not writable.
	InfrastructurePrerequisiteHarmonikDirUnwritable InfrastructurePrerequisite = "harmonik_dir_unwritable"

	// InfrastructurePrerequisiteFilesystemFull indicates the filesystem is full.
	InfrastructurePrerequisiteFilesystemFull InfrastructurePrerequisite = "filesystem_full"

	// InfrastructurePrerequisiteQueueWriteError indicates an atomic write to a
	// queue file failed. queue-model.md §3.1 QM-001 makes this emission
	// mandatory on that path; the daemon refuses further mutations to that
	// queue and the dispatch that asked for the write is abandoned.
	InfrastructurePrerequisiteQueueWriteError InfrastructurePrerequisite = "queue_write_error"
)

// Valid reports whether p is one of the eight declared InfrastructurePrerequisite constants.
func (p InfrastructurePrerequisite) Valid() bool {
	switch p {
	case InfrastructurePrerequisiteBrMissing,
		InfrastructurePrerequisiteBrTimeout,
		InfrastructurePrerequisiteBrVersionIncompatible,
		InfrastructurePrerequisiteBeadsSQLiteLocked,
		InfrastructurePrerequisiteGitIndexLocked,
		InfrastructurePrerequisiteHarmonikDirUnwritable,
		InfrastructurePrerequisiteFilesystemFull,
		InfrastructurePrerequisiteQueueWriteError:
		return true
	default:
		return false
	}
}
