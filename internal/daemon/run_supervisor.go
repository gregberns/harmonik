package daemon

import (
	"context"
	"sync"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/runregistry"
)

// runTerminalResult is the complete result of one supervised run. A run has
// one writer for this value: the goroutine started by runSupervisor.Start.
type runTerminalResult struct {
	runID     core.RunID
	succeeded bool
}

// runSupervisor owns the goroutines that execute dispatched runs. It is the
// only place that starts them, records their terminal result, and performs
// their final cancellation and registry removal.
type runSupervisor struct {
	registry *runregistry.RunRegistry

	wg      sync.WaitGroup
	mu      sync.Mutex
	results []runTerminalResult
}

func newRunSupervisor(registry *runregistry.RunRegistry) *runSupervisor {
	return &runSupervisor{registry: registry}
}

// Start registers and starts one run. onTerminal receives the returned result
// before the run is removed from the live registry.
func (s *runSupervisor) Start(parent context.Context, runID core.RunID, handle *runregistry.RunHandle,
	run func(context.Context) bool, onTerminal func(runTerminalResult),
) {
	runCtx, cancel := context.WithCancel(parent)
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(cancel) }
	handle.Cancel = stop
	s.registry.Register(runID, handle)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.registry.Unregister(runID)
		defer stop()

		result := runTerminalResult{runID: runID, succeeded: run(runCtx)}
		s.mu.Lock()
		s.results = append(s.results, result)
		s.mu.Unlock()
		onTerminal(result)
	}()
}

// Wait returns after every run has completed and returns each terminal result.
func (s *runSupervisor) Wait() []runTerminalResult {
	s.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]runTerminalResult(nil), s.results...)
}
