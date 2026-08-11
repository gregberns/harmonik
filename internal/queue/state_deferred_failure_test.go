package queue_test

import (
	"context"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

type failedBlockerLedger struct{}

func (failedBlockerLedger) BlocksEdge(_ context.Context, blocker, blocked core.BeadID) (bool, error) {
	return (blocker == "blocker" && blocked == "dependent") ||
		(blocker == "dependent" && blocked == "grandchild"), nil
}

func (failedBlockerLedger) LookupStatus(_ context.Context, _ core.BeadID) (queue.BeadStatus, error) {
	return queue.BeadStatusOpen, nil
}

func TestFailDeferredDependents_FailsOnlyTheFailedChain(t *testing.T) {
	t.Parallel()
	g := queue.Group{Items: []queue.Item{
		{BeadID: "blocker", Status: queue.ItemStatusFailed},
		{BeadID: "dependent", Status: queue.ItemStatusDeferredForLedgerDep},
		{BeadID: "grandchild", Status: queue.ItemStatusDeferredForLedgerDep},
		{BeadID: "independent", Status: queue.ItemStatusDeferredForLedgerDep},
	}}

	failed, err := queue.FailDeferredDependents(t.Context(), &g, "blocker", failedBlockerLedger{})
	if err != nil {
		t.Fatalf("FailDeferredDependents: %v", err)
	}
	if len(failed) != 2 || failed[0] != "dependent" || failed[1] != "grandchild" {
		t.Fatalf("failed = %v, want [dependent grandchild]", failed)
	}
	if got := g.Items[1].Status; got != queue.ItemStatusFailed {
		t.Fatalf("dependent status = %q, want %q", got, queue.ItemStatusFailed)
	}
	if got := g.Items[1].LastFailureReason; got != "dependency_failed:blocker" {
		t.Fatalf("dependent failure reason = %q, want dependency_failed:blocker", got)
	}
	if got := g.Items[2].Status; got != queue.ItemStatusFailed {
		t.Fatalf("grandchild status = %q, want %q", got, queue.ItemStatusFailed)
	}
	if got := g.Items[2].LastFailureReason; got != "dependency_failed:dependent" {
		t.Fatalf("grandchild failure reason = %q, want dependency_failed:dependent", got)
	}
	if got := g.Items[3].Status; got != queue.ItemStatusDeferredForLedgerDep {
		t.Fatalf("independent status = %q, want %q", got, queue.ItemStatusDeferredForLedgerDep)
	}
}
