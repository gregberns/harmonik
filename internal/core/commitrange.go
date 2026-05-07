package core

// CommitRange is an inclusive range of commits on a task branch, filtered by
// the run's Harmonik-Run-ID trailer (execution-model.md §6.1).
// Used in State.transition_history.
type CommitRange struct {
	FirstCommitSHA string
	LastCommitSHA  string
}
