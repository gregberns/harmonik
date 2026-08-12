package daemon

import (
	"context"
	"errors"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/dispatchstore"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	runpkg "github.com/gregberns/harmonik/internal/run"
)

// acknowledgeSessionStart installs a receipt only after all durable and live facts agree.
func acknowledgeSessionStart(
	ctx context.Context,
	projectDir string,
	adapter ltmux.Adapter,
	receipt dispatch.SessionStartReceipt,
) error {
	intent, err := dispatchstore.New(projectDir).Load(receipt.Binding.RunID)
	if err != nil {
		return err
	}
	snapshot, err := runpkg.ScanRegistry(projectDir)
	if err != nil {
		return err
	}
	record, found := dispatchRecordForRun(snapshot.Dispatch, receipt.Binding.RunID)
	if !found {
		return errors.New("daemon: session start acknowledgement has no durable run record")
	}
	if runpkg.ClassifySessionStartReceipt(intent, &record, &receipt) != dispatch.SessionReceiptExact {
		return errors.New("daemon: session start acknowledgement conflicts with durable authority")
	}
	if readDispatchTargetFact(ctx, adapter, intent) != dispatch.SessionLive {
		return errors.New("daemon: session start acknowledgement target is not exact and live")
	}
	return dispatchstore.New(projectDir).InstallSessionStartReceipt(receipt)
}

func dispatchRecordForRun(records []runpkg.DispatchRecord, runID core.RunID) (runpkg.DispatchRecord, bool) {
	for _, record := range records {
		if record.RunID == runID {
			return record, true
		}
	}
	return runpkg.DispatchRecord{}, false
}
