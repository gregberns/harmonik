package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/dispatch"
	"github.com/gregberns/harmonik/internal/dispatchstore"
	ltmux "github.com/gregberns/harmonik/internal/lifecycle/tmux"
	runpkg "github.com/gregberns/harmonik/internal/run"
	"github.com/gregberns/harmonik/internal/workers"
)

// SessionStartAcknowledgementHandler receives the exact receipt from a
// bootstrap that already runs inside its bound target.
type SessionStartAcknowledgementHandler interface {
	HandleSessionStartAcknowledgement(context.Context, json.RawMessage) (json.RawMessage, error)
}

type sessionStartAcknowledgementHandler struct {
	projectDir     string
	resolveAdapter sessionStartAdapterResolver
}

type sessionStartAdapterResolver func(runpkg.ExecutionLocation) (ltmux.Adapter, error)

type sessionStartAcknowledgementResponse struct {
	Acknowledged bool `json:"acknowledged"`
}

func (h sessionStartAcknowledgementHandler) HandleSessionStartAcknowledgement(
	ctx context.Context,
	payload json.RawMessage,
) (json.RawMessage, error) {
	var receipt dispatch.SessionStartReceipt
	if err := json.Unmarshal(payload, &receipt); err != nil {
		return nil, fmt.Errorf("daemon: decode session start acknowledgement: %w", err)
	}
	if err := acknowledgeSessionStartWithResolver(ctx, h.projectDir, h.resolveAdapter, receipt); err != nil {
		return nil, err
	}
	response, err := json.Marshal(sessionStartAcknowledgementResponse{Acknowledged: true})
	if err != nil {
		return nil, fmt.Errorf("daemon: encode session start acknowledgement: %w", err)
	}
	return response, nil
}

func newSessionStartAdapterResolver(local ltmux.Adapter, cfg workers.Config) sessionStartAdapterResolver {
	return newSessionStartAdapterResolverWithFactory(local, cfg, func(worker workers.Worker) ltmux.Adapter {
		return ltmux.OSAdapter{}.WithRunner(ltmux.SSHRunner{
			Host: worker.Host,
			Opts: []string{"-o", "ControlMaster=no", "-o", "ControlPath=none"},
		})
	})
}

func newSessionStartAdapterResolverWithFactory(
	local ltmux.Adapter,
	cfg workers.Config,
	remoteAdapter func(workers.Worker) ltmux.Adapter,
) sessionStartAdapterResolver {
	return func(location runpkg.ExecutionLocation) (ltmux.Adapter, error) {
		switch location.Kind {
		case runpkg.ExecutionLocalIndependent, runpkg.ExecutionLocalShared:
			if local == nil {
				return nil, errors.New("daemon: local session start adapter is not available")
			}
			return local, nil
		case runpkg.ExecutionRemote:
			return resolveRemoteSessionStartAdapter(cfg, location.WorkerName, remoteAdapter)
		default:
			return nil, fmt.Errorf("daemon: unsupported session start location %q", location.Kind)
		}
	}
}

func resolveRemoteSessionStartAdapter(
	cfg workers.Config,
	workerName string,
	remoteAdapter func(workers.Worker) ltmux.Adapter,
) (ltmux.Adapter, error) {
	for _, worker := range cfg.Workers {
		if worker.Name != workerName {
			continue
		}
		if worker.Transport != "ssh" || worker.Host == "" {
			return nil, fmt.Errorf("daemon: worker %q has no supported session start transport", worker.Name)
		}
		adapter := remoteAdapter(worker)
		if adapter == nil {
			return nil, fmt.Errorf("daemon: worker %q session start adapter is not available", worker.Name)
		}
		return adapter, nil
	}
	return nil, fmt.Errorf("daemon: session start worker %q is not configured", workerName)
}

func acknowledgeSessionStartWithResolver(
	ctx context.Context,
	projectDir string,
	resolveAdapter sessionStartAdapterResolver,
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
	if record.Location == nil || resolveAdapter == nil {
		return errors.New("daemon: session start acknowledgement has no execution adapter")
	}
	adapter, err := resolveAdapter(*record.Location)
	if err != nil {
		return err
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
