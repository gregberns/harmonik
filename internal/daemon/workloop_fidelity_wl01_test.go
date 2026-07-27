package daemon_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/daemon"
	"github.com/gregberns/harmonik/internal/queue"
)

var wl01Scenarios = []struct {
	id        string
	required  []string
	forbidden []string
}{
	{"queue_success", []string{"source.queue.select", "queue.reservation.durable", "ledger.claim", "registry.register", "executor.started", "executor.terminal.success", "queue.item_terminal.success", "registry.unregister"}, []string{"source.br_ready.ready"}},
	{"br_ready_success", []string{"source.br_ready.ready", "source.br_ready.show", "ledger.claim", "registry.register", "executor.started", "executor.terminal.success", "ledger.close", "registry.unregister"}, []string{"source.queue.select", "queue.reservation.durable"}},
	{"loaded_queue_suppresses_br_ready", []string{"maintenance.queue_loaded", "gate.queue_loaded.hold"}, []string{"source.br_ready.ready"}},
	{"idle_submit_wake", []string{"maintenance.idle", "source.queue.select"}, nil},
	{"handler_pause_queue", []string{"source.queue.select", "gate.handler_pause.hold"}, []string{"queue.reservation.durable", "ledger.claim", "registry.register", "executor.started"}},
	{"operator_pause_br_ready", []string{"gate.operator_pause.hold"}, []string{"source.br_ready.ready", "ledger.claim", "registry.register", "executor.started"}},
	{"disk_low_gate", []string{"maintenance.disk", "gate.disk_low.hold"}, []string{"source.queue.select", "source.br_ready.ready", "ledger.claim", "executor.started"}},
	{"capacity_gate", []string{"maintenance.capacity", "gate.capacity.hold"}, []string{"source.queue.select", "source.br_ready.ready", "ledger.claim", "executor.started"}},
	{"reservation_persist_failure", []string{"source.queue.select", "ledger.claim"}, []string{"queue.reservation.durable", "registry.register", "executor.started"}},
	{"queue_claim_failure", []string{"source.queue.select", "queue.reservation.durable", "ledger.claim", "queue.revert_pending"}, []string{"registry.register", "executor.started"}},
	{"spawn_completion", []string{"registry.register", "executor.started", "executor.terminal.success", "queue.item_terminal.success", "registry.unregister"}, nil},
	{"shutdown_inflight", []string{"shutdown.begin", "executor.terminal.cancelled", "shutdown.queue_cancel", "shutdown.return"}, []string{"queue.item_terminal.failure"}},
	{"restart_spawn_gate", []string{"restart.spawn_gate_wait", "source.queue.select"}, nil},
	{"restart_live_session", []string{"restart.adopt_session", "ledger.reopen", "queue.revert_pending", "registry.unregister"}, nil},
}

type wl01EventEmitter struct {
	trace    *wl01Trace
	heldCh   chan struct{}
	diskLowC chan struct{}
	heldOnce sync.Once
	diskOnce sync.Once
}

func (w *wl01EventEmitter) Emit(_ context.Context, eventType core.EventType, payload []byte) error {
	switch eventType {
	case core.EventTypeRunStarted:
		w.trace.add("executor.started")
	case core.EventTypeQueueItemHeldForHandlerPause:
		w.trace.add("source.queue.select")
		w.trace.add("gate.handler_pause.hold")
		if w.heldCh != nil {
			w.heldOnce.Do(func() { close(w.heldCh) })
		}
	case core.EventTypeDiskLow:
		w.trace.add("gate.disk_low.hold")
		if w.diskLowC != nil {
			w.diskOnce.Do(func() { close(w.diskLowC) })
		}
	}
	return nil
}

func (w *wl01EventEmitter) EmitWithRunID(ctx context.Context, _ core.RunID, eventType core.EventType, payload []byte) error {
	return w.Emit(ctx, eventType, payload)
}

func wl01ProductionDeps(t *testing.T, projectDir string, ledger *wl01Ledger, bus *wl01EventEmitter, registry *daemon.RunRegistry) daemon.WorkLoopDepsParams {
	t.Helper()
	return daemon.WorkLoopDepsParams{
		BrAdapter:        ledger,
		Bus:              bus,
		ProjectDir:       projectDir,
		HandlerBinary:    "/usr/bin/true",
		IntentLogDir:     filepath.Join(projectDir, ".harmonik", "beads-intents"),
		AdapterRegistry2: NewEmptySealedAdapterRegistryForTest(t),
		RunRegistry:      registry,
		WorktreeFactory:  emptyCommitWorktreeFactory,
	}
}

func wl01Wait(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-t.Context().Done():
		t.Fatalf("timed out waiting for %s", what)
	}
}

func wl01PersistedQueueItem(projectDir string) (queue.Item, error) {
	raw, err := os.ReadFile(filepath.Join(projectDir, ".harmonik", "queues", "main.json"))
	if err != nil {
		return queue.Item{}, err
	}
	var persisted queue.Queue
	if err := json.Unmarshal(raw, &persisted); err != nil {
		return queue.Item{}, err
	}
	if len(persisted.Groups) != 1 || len(persisted.Groups[0].Items) != 1 {
		return queue.Item{}, errors.New("persisted queue does not contain one item")
	}
	return persisted.Groups[0].Items[0], nil
}

func wl01RequireQueueItem(item queue.Item, status queue.ItemStatus, runID *string) error {
	if item.Status != status {
		return errors.New("queue item has unexpected status")
	}
	if runID == nil {
		if item.RunID != nil {
			return errors.New("queue item unexpectedly retains a run ID")
		}
		return nil
	}
	if item.RunID == nil || *item.RunID != *runID {
		return errors.New("queue item does not retain the expected run ID")
	}
	return nil
}

func TestWL01_QueueSuccessProductionTrace(t *testing.T) {
	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)
	trace := &wl01Trace{}
	registry := daemon.NewRunRegistry()
	store := daemon.ExportedNewQueueStore()
	q := queueDispatchFixtureWaveQueue(t, core.BeadID("wl01-queue-success"))
	store.SetQueue(q)
	ledger := &wl01Ledger{
		trace:       trace,
		queuePath:   true,
		readyCalled: make(chan struct{}),
		claimCalled: make(chan struct{}),
		closed:      make(chan struct{}),
	}
	ledger.claimCheck = func(runID core.RunID) error {
		live := store.Queue()
		if live == nil || len(live.Groups) != 1 || len(live.Groups[0].Items) != 1 {
			return errors.New("queue reservation is absent at ClaimBead")
		}
		item := live.Groups[0].Items[0]
		if item.Status != queue.ItemStatusDispatched || item.RunID == nil || *item.RunID != runID.String() {
			return errors.New("queue reservation is not dispatched with the claimed run ID")
		}
		persistedItem, err := wl01PersistedQueueItem(projectDir)
		if err != nil {
			return err
		}
		runIDString := runID.String()
		if err := wl01RequireQueueItem(persistedItem, queue.ItemStatusDispatched, &runIDString); err != nil {
			return errors.New("queue reservation is not durable at ClaimBead")
		}
		trace.add("queue.reservation.durable")
		return nil
	}
	bus := &wl01EventEmitter{trace: trace}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	params := wl01ProductionDeps(t, projectDir, ledger, bus, registry)
	params.QueueStore = store
	params.CancelOnQueueDrain = func() {
		trace.add("queue.item_terminal.success")
		cancel()
	}
	params.WorktreeFactory = func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		if registry.Len() != 1 {
			return "", nil, errors.New("run was not registered before worktree creation")
		}
		trace.add("registry.register")
		return emptyCommitWorktreeFactory(ctx, projectDir, runID, headSHA)
	}
	done := make(chan error, 1)
	go func() { done <- daemon.ExportedRunWorkLoop(ctx, daemon.ExportedWorkLoopDeps(params)) }()
	wl01Wait(t, ledger.closed, "ledger close")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ExportedRunWorkLoop: %v", err)
		}
	case <-t.Context().Done():
		t.Fatal("work loop did not exit after queue drain")
	}
	if registry.Len() != 0 {
		t.Fatalf("run registry still has %d entry after loop return", registry.Len())
	}
	trace.add("registry.unregister")
	wl01AssertTrace(t, trace.snapshot(), wl01Scenarios[0].required, wl01Scenarios[0].forbidden)
}

func TestWL01_BRReadySuccessProductionTrace(t *testing.T) {
	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)
	trace := &wl01Trace{}
	registry := daemon.NewRunRegistry()
	ledger := &wl01Ledger{
		trace:       trace,
		ready:       []core.BeadRecord{{BeadID: "wl01-br-ready-success"}},
		readyCalled: make(chan struct{}),
		claimCalled: make(chan struct{}),
		closed:      make(chan struct{}),
	}
	bus := &wl01EventEmitter{trace: trace}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	params := wl01ProductionDeps(t, projectDir, ledger, bus, registry)
	params.WorktreeFactory = func(ctx context.Context, projectDir, runID, headSHA string) (string, func(), error) {
		if registry.Len() != 1 {
			return "", nil, errors.New("run was not registered before worktree creation")
		}
		trace.add("registry.register")
		return emptyCommitWorktreeFactory(ctx, projectDir, runID, headSHA)
	}
	done := make(chan error, 1)
	go func() { done <- daemon.ExportedRunWorkLoop(ctx, daemon.ExportedWorkLoopDeps(params)) }()
	wl01Wait(t, ledger.closed, "ledger close")
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ExportedRunWorkLoop: %v", err)
		}
	case <-t.Context().Done():
		t.Fatal("work loop did not exit after cancellation")
	}
	if registry.Len() != 0 {
		t.Fatalf("run registry still has %d entry after loop return", registry.Len())
	}
	trace.add("registry.unregister")
	wl01AssertTrace(t, trace.snapshot(), wl01Scenarios[1].required, wl01Scenarios[1].forbidden)
}

func TestWL01_QueueClaimFailureProductionTrace(t *testing.T) {
	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)
	trace := &wl01Trace{}
	store := daemon.ExportedNewQueueStore()
	store.SetQueue(queueDispatchFixtureWaveQueue(t, core.BeadID("wl01-claim-failure")))
	showRelease := make(chan struct{})
	ledger := &wl01Ledger{
		trace:       trace,
		queuePath:   true,
		claimErr:    errors.New("forced claim failure"),
		readyCalled: make(chan struct{}),
		claimCalled: make(chan struct{}, 1),
		// The claim-error path calls ShowBead once more to classify the error.
		// Holding call three therefore proves the prior failure completed its
		// live-and-durable queue reversion before a fresh selection may proceed.
		holdShowCall: 3,
		showEntered:  make(chan struct{}),
		showRelease:  showRelease,
	}
	ledger.claimCheck = func(runID core.RunID) error {
		live := store.Queue()
		if live == nil || len(live.Groups) != 1 || len(live.Groups[0].Items) != 1 {
			return errors.New("claim failure path has no live queue item")
		}
		runIDString := runID.String()
		if err := wl01RequireQueueItem(live.Groups[0].Items[0], queue.ItemStatusDispatched, &runIDString); err != nil {
			return errors.New("claim failure path reached ClaimBead without durable reservation")
		}
		persisted, err := wl01PersistedQueueItem(projectDir)
		if err != nil {
			return err
		}
		if err := wl01RequireQueueItem(persisted, queue.ItemStatusDispatched, &runIDString); err != nil {
			return errors.New("claim failure path reached ClaimBead without durable reservation")
		}
		trace.add("queue.reservation.durable")
		return nil
	}
	bus := &wl01EventEmitter{trace: trace}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	params := wl01ProductionDeps(t, projectDir, ledger, bus, daemon.NewRunRegistry())
	params.QueueStore = store
	done := make(chan error, 1)
	go func() { done <- daemon.ExportedRunWorkLoop(ctx, daemon.ExportedWorkLoopDeps(params)) }()
	wl01Wait(t, ledger.claimCalled, "first failed claim")
	wl01Wait(t, ledger.showEntered, "second pre-claim show after claim reversion")
	live := store.Queue()
	if live == nil || len(live.Groups) != 1 || len(live.Groups[0].Items) != 1 {
		t.Fatal("claim failure removed the live queue item")
	}
	if err := wl01RequireQueueItem(live.Groups[0].Items[0], queue.ItemStatusPending, nil); err != nil {
		t.Fatalf("claim failure did not revert live queue item: %v", err)
	}
	persisted, err := wl01PersistedQueueItem(projectDir)
	if err != nil {
		t.Fatalf("read persisted queue after claim failure: %v", err)
	}
	if err := wl01RequireQueueItem(persisted, queue.ItemStatusPending, nil); err != nil {
		t.Fatalf("claim failure did not revert persisted queue item: %v", err)
	}
	trace.add("queue.revert_pending")
	cancel()
	close(showRelease)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ExportedRunWorkLoop: %v", err)
		}
	case <-t.Context().Done():
		t.Fatal("work loop did not exit after cancellation")
	}
	wl01AssertTrace(t, trace.snapshot(), wl01Scenarios[9].required, wl01Scenarios[9].forbidden)
}

func TestWL01_HandlerPauseQueueProductionTrace(t *testing.T) {
	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)
	trace := &wl01Trace{}
	store := daemon.ExportedNewQueueStore()
	q := queueDispatchFixtureWaveQueue(t, core.BeadID("wl01-handler-paused"))
	if err := queue.Persist(context.Background(), projectDir, q); err != nil {
		t.Fatalf("persist initial queue: %v", err)
	}
	store.SetQueue(q)
	ledger := &wl01Ledger{trace: trace, queuePath: true, readyCalled: make(chan struct{}), claimCalled: make(chan struct{})}
	bus := &wl01EventEmitter{trace: trace, heldCh: make(chan struct{})}
	ctrl := hpcNewController(t)
	hpcPause(t, ctrl)
	registry := daemon.NewRunRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	params := wl01ProductionDeps(t, projectDir, ledger, bus, registry)
	params.QueueStore = store
	params.HandlerPauseController = ctrl
	done := make(chan error, 1)
	go func() { done <- daemon.ExportedRunWorkLoop(ctx, daemon.ExportedWorkLoopDeps(params)) }()
	wl01Wait(t, bus.heldCh, "handler-pause held event")
	live := store.Queue()
	if live == nil || len(live.Groups) != 1 || len(live.Groups[0].Items) != 1 {
		t.Fatal("handler-pause gate removed live queue item")
	}
	if err := wl01RequireQueueItem(live.Groups[0].Items[0], queue.ItemStatusPending, nil); err != nil {
		t.Fatalf("handler-pause gate stamped live item: %v", err)
	}
	persisted, err := wl01PersistedQueueItem(projectDir)
	if err != nil {
		t.Fatalf("read persisted paused queue: %v", err)
	}
	if err := wl01RequireQueueItem(persisted, queue.ItemStatusPending, nil); err != nil {
		t.Fatalf("handler-pause gate stamped persisted item: %v", err)
	}
	if registry.Len() != 0 {
		t.Fatal("handler-pause gate registered a run")
	}
	select {
	case <-ledger.claimCalled:
		t.Fatal("handler-pause gate claimed a bead")
	default:
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ExportedRunWorkLoop: %v", err)
		}
	case <-t.Context().Done():
		t.Fatal("work loop did not exit after cancellation")
	}
	wl01AssertTrace(t, trace.snapshot(), wl01Scenarios[4].required, wl01Scenarios[4].forbidden)
}

func TestWL01_DiskLowGateProductionTrace(t *testing.T) {
	projectDir, _ := workloopFixtureProjectDir(t)
	trace := &wl01Trace{}
	ledger := &wl01Ledger{trace: trace, ready: []core.BeadRecord{{BeadID: "wl01-disk-low"}}, readyCalled: make(chan struct{}), claimCalled: make(chan struct{})}
	bus := &wl01EventEmitter{trace: trace, diskLowC: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	params := wl01ProductionDeps(t, projectDir, ledger, bus, daemon.NewRunRegistry())
	params.DiskFreeBytesFunc = func(string) (uint64, error) { return 1, nil }
	params.GoCacheCleanFunc = func() error {
		trace.add("maintenance.disk")
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- daemon.ExportedRunWorkLoop(ctx, daemon.ExportedWorkLoopDeps(params)) }()
	wl01Wait(t, bus.diskLowC, "disk-low event")
	select {
	case <-ledger.readyCalled:
		t.Fatal("disk-low gate selected br-ready work")
	default:
	}
	select {
	case <-ledger.claimCalled:
		t.Fatal("disk-low gate claimed a bead")
	default:
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ExportedRunWorkLoop: %v", err)
		}
	case <-t.Context().Done():
		t.Fatal("work loop did not exit after cancellation")
	}
	wl01AssertTrace(t, trace.snapshot(), wl01Scenarios[6].required, wl01Scenarios[6].forbidden)
}

func TestWL01_ReservationPersistFailureObservedDebt(t *testing.T) {
	projectDir, _ := workloopFixtureProjectDir(t)
	workloopFixtureGitRepo(t, projectDir)
	trace := &wl01Trace{}
	store := daemon.ExportedNewQueueStore()
	store.SetQueue(queueDispatchFixtureWaveQueue(t, core.BeadID("wl01-persist-failure")))
	if err := os.WriteFile(filepath.Join(projectDir, ".harmonik", "queues"), []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("create persistence blocker: %v", err)
	}
	showRelease := make(chan struct{})
	ledger := &wl01Ledger{
		trace:        trace,
		queuePath:    true,
		readyCalled:  make(chan struct{}),
		claimCalled:  make(chan struct{}, 1),
		holdShowCall: 3,
		showEntered:  make(chan struct{}),
		showRelease:  showRelease,
	}
	ledger.claimCheck = func(_ core.RunID) error {
		if _, err := os.ReadFile(filepath.Join(projectDir, ".harmonik", "queues", "main.json")); err == nil {
			return errors.New("reservation unexpectedly reached durable queue storage")
		}
		return errors.New("stop after observing claim without durable reservation")
	}
	bus := &wl01EventEmitter{trace: trace}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	params := wl01ProductionDeps(t, projectDir, ledger, bus, daemon.NewRunRegistry())
	params.QueueStore = store
	done := make(chan error, 1)
	go func() { done <- daemon.ExportedRunWorkLoop(ctx, daemon.ExportedWorkLoopDeps(params)) }()
	wl01Wait(t, ledger.claimCalled, "claim after persistence failure")
	wl01Wait(t, ledger.showEntered, "second selection after persistence-failure claim")
	cancel()
	close(showRelease)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ExportedRunWorkLoop: %v", err)
		}
	case <-t.Context().Done():
		t.Fatal("work loop did not exit after cancellation")
	}
	wl01AssertTrace(t, trace.snapshot(), wl01Scenarios[8].required, wl01Scenarios[8].forbidden)
}

func TestWL01_OracleRejectsReorderedReservation(t *testing.T) {
	queue := wl01Scenarios[0]
	if !wl01TraceAccepts(queue.required, queue.required, queue.forbidden) {
		t.Fatal("oracle rejected the production queue trace")
	}
	mutant := []string{"source.queue.select", "ledger.claim", "queue.reservation.durable", "registry.register", "executor.started", "executor.terminal.success", "queue.item_terminal.success", "registry.unregister"}
	if wl01TraceAccepts(mutant, queue.required, queue.forbidden) {
		t.Fatal("oracle accepted ledger.claim before queue.reservation.durable")
	}
}

func TestWL01_OracleRejectsDuplicateClaimCompensation(t *testing.T) {
	for _, scenarioIndex := range []int{0, 1} {
		scenario := wl01Scenarios[scenarioIndex]
		if !wl01TraceAccepts(scenario.required, scenario.required, scenario.forbidden) {
			t.Fatalf("oracle rejected production trace for %s", scenario.id)
		}
	}
	queue := wl01Scenarios[0]
	mutant := []string{"source.queue.select", "ledger.claim", "queue.reservation.durable", "ledger.claim", "registry.register", "executor.started", "executor.terminal.success", "queue.item_terminal.success", "registry.unregister"}
	if wl01TraceAccepts(mutant, queue.required, queue.forbidden) {
		t.Fatal("oracle accepted a compensating second ledger.claim after durable reservation")
	}
}

func wl01TraceAccepts(got, required, forbidden []string) bool {
	if _, duplicate := wl01DuplicateSingleOwnerEffect(got); duplicate {
		return false
	}
	pos := 0
	for _, want := range required {
		for pos < len(got) && got[pos] != want {
			pos++
		}
		if pos == len(got) {
			return false
		}
		pos++
	}
	for _, forbiddenEvent := range forbidden {
		for _, event := range got {
			if event == forbiddenEvent {
				return false
			}
		}
	}
	return true
}
