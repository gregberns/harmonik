//go:build integration

package herdrwire_test

// KH-2 self-check item 2: a live smoke test against the RUNNING herdr
// server on this box. Splits a throwaway pane off an existing one, sends
// text, reads it back, probes its process info, then closes it — and
// asserts every observed shape matches the protocol-22 wire contract this
// package pins. Skips (does not fail) when no herdr socket is present, so
// CI without herdr is unaffected; run explicitly with:
//
//	go test -tags integration ./internal/keeper/panehost/herdrwire/... -run TestLiveHerdrSmoke -v

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper/panehost/herdrwire"
)

func liveHerdrSocketPath(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("herdrwire live: cannot resolve home dir")
	}
	sock := filepath.Join(home, ".config", "herdr", "herdr.sock")
	if _, err := os.Stat(sock); err != nil {
		t.Skip("herdrwire live: no herdr.sock on this box; skipping live smoke test")
	}
	return sock
}

func TestLiveHerdrSmoke(t *testing.T) {
	sock := liveHerdrSocketPath(t)
	c := herdrwire.NewClient(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pong, err := c.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if pong.Protocol != herdrwire.ProtocolVersion {
		t.Fatalf("live server protocol = %d, this package pins %d", pong.Protocol, herdrwire.ProtocolVersion)
	}
	t.Logf("live herdr: version=%s protocol=%d", pong.Version, pong.Protocol)

	panes, err := c.PaneList(ctx, "")
	if err != nil {
		t.Fatalf("PaneList: %v", err)
	}
	if len(panes.Panes) == 0 {
		t.Skip("herdrwire live: no existing panes to split from")
	}
	targetPaneID := panes.Panes[0].PaneID
	workspaceID := panes.Panes[0].WorkspaceID

	tmpDir := t.TempDir()

	split, err := c.PaneSplit(ctx, herdrwire.PaneSplitParams{
		Direction:    herdrwire.SplitDown,
		CWD:          tmpDir,
		TargetPaneID: targetPaneID,
		WorkspaceID:  workspaceID,
		Focus:        false,
	})
	if err != nil {
		t.Fatalf("PaneSplit: %v", err)
	}
	if split.Type != "pane_info" {
		t.Fatalf("PaneSplit.Type = %q, want %q", split.Type, "pane_info")
	}
	panePaneID := split.Pane.PaneID
	if panePaneID == "" {
		t.Fatal("PaneSplit returned an empty pane id")
	}
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		_ = c.PaneClose(closeCtx, panePaneID)
	})

	if err := c.PaneSendInput(ctx, herdrwire.PaneSendInputParams{PaneID: panePaneID, Text: "echo herdrwire_kh2_smoke\n"}); err != nil {
		t.Fatalf("PaneSendInput: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	read, err := c.PaneRead(ctx, herdrwire.PaneReadParams{PaneID: panePaneID, Source: herdrwire.ReadSourceRecent, Format: herdrwire.ReadFormatText})
	if err != nil {
		t.Fatalf("PaneRead: %v", err)
	}
	if read.Type != "pane_read" {
		t.Fatalf("PaneRead.Type = %q, want %q", read.Type, "pane_read")
	}
	if read.Read.PaneID != panePaneID {
		t.Fatalf("PaneRead.Read.PaneID = %q, want %q", read.Read.PaneID, panePaneID)
	}
	t.Logf("live pane.read text: %q", read.Read.Text)

	procInfo, err := c.PaneProcessInfo(ctx, panePaneID)
	if err != nil {
		t.Fatalf("PaneProcessInfo: %v", err)
	}
	if procInfo.ProcessInfo.PaneID != panePaneID {
		t.Fatalf("PaneProcessInfo.ProcessInfo.PaneID = %q, want %q", procInfo.ProcessInfo.PaneID, panePaneID)
	}
	if procInfo.ProcessInfo.ShellPID == 0 {
		t.Fatal("PaneProcessInfo.ProcessInfo.ShellPID = 0, want a live shell pid")
	}
	t.Logf("live pane.process_info: shell_pid=%d foreground=%+v", procInfo.ProcessInfo.ShellPID, procInfo.ProcessInfo.ForegroundProcesses)

	// Fault matrix, against the real server: a bad pane id must fail
	// closed with the error code observed live during KH-2 probing.
	_, err = c.PaneRead(ctx, herdrwire.PaneReadParams{PaneID: "w0:pDoesNotExist", Source: herdrwire.ReadSourceRecent})
	if err == nil {
		t.Fatal("PaneRead with a bogus pane id against the live server: want error, got nil")
	}
	var wireErr *herdrwire.WireError
	if !asErr(err, &wireErr) || wireErr.Code != "pane_not_found" {
		t.Fatalf("live PaneRead bogus-pane error = %#v, want *WireError{Code: pane_not_found}", err)
	}

	// Agent-name limit, against the real server (package-doc claim, KH-2
	// open question 3): 32 chars of [a-z][a-z0-9_-]* passes name
	// validation; anything longer or uppercase is refused before the
	// kind is even checked.
	_, err = c.AgentStart(ctx, herdrwire.AgentStartParams{
		Kind: "nonexistent-kind-for-kh2-probe", Name: "harmonik-abcdef012345-crew-alpha", PaneID: panePaneID, TimeoutMs: 5000,
	})
	if !asErr(err, &wireErr) || wireErr.Code != "unsupported_agent_kind" {
		t.Fatalf("live AgentStart(32-char valid name) error = %#v, want *WireError{Code: unsupported_agent_kind} (name validation should have passed)", err)
	}
	_, err = c.AgentStart(ctx, herdrwire.AgentStartParams{
		Kind: "nonexistent-kind-for-kh2-probe", Name: "Not-Valid", PaneID: panePaneID, TimeoutMs: 5000,
	})
	if !asErr(err, &wireErr) || wireErr.Code != "invalid_agent_name" {
		t.Fatalf("live AgentStart(invalid name) error = %#v, want *WireError{Code: invalid_agent_name}", err)
	}

	// events.subscribe round trip (iter-2 review: the self-check never
	// exercised subscribe against the live server, which is how the
	// missing-pane_id defect on pane.agent_status_changed slipped through).
	//
	// pane.created is global: subscribe first, then split a second
	// throwaway pane and observe the event it produces.
	createdSub, err := c.Subscribe(ctx, herdrwire.PaneCreatedSpec())
	if err != nil {
		t.Fatalf("Subscribe(PaneCreatedSpec): %v", err)
	}
	t.Cleanup(func() {
		if closeErr := createdSub.Close(); closeErr != nil {
			t.Logf("close pane.created subscription: %v", closeErr)
		}
	})

	secondSplit, err := c.PaneSplit(ctx, herdrwire.PaneSplitParams{
		Direction:    herdrwire.SplitDown,
		CWD:          tmpDir,
		TargetPaneID: targetPaneID,
		WorkspaceID:  workspaceID,
		Focus:        false,
	})
	if err != nil {
		t.Fatalf("PaneSplit (for pane.created subscribe trigger): %v", err)
	}
	secondPaneID := secondSplit.Pane.PaneID
	t.Cleanup(func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer closeCancel()
		if closeErr := c.PaneClose(closeCtx, secondPaneID); closeErr != nil {
			t.Logf("close second pane: %v", closeErr)
		}
	})

	nextCtx, nextCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer nextCancel()
	ev, err := createdSub.Next(nextCtx)
	if err != nil {
		t.Fatalf("live Subscribe(pane.created) Next: %v", err)
	}
	if ev.PaneCreated == nil || ev.PaneCreated.Pane.PaneID == "" {
		t.Fatalf("live pane.created event = %+v, want a populated PaneCreated payload", ev)
	}
	t.Logf("live pane.created event: pane_id=%s", ev.PaneCreated.Pane.PaneID)

	// pane.agent_status_changed is scoped: this is the exact regression
	// iter-2 caught — a spec with no pane_id is refused by the live server,
	// so the ack itself (not an event) is the claim under test.
	statusSub, err := c.Subscribe(ctx, herdrwire.PaneAgentStatusChangedSpec(panePaneID))
	if err != nil {
		t.Fatalf("Subscribe(PaneAgentStatusChangedSpec(%q)): %v (this must succeed against the live server: pane_id is required and now sent)", panePaneID, err)
	}
	if closeErr := statusSub.Close(); closeErr != nil {
		t.Logf("close pane.agent_status_changed subscription: %v", closeErr)
	}

	// And the negative case, live: no pane_id must still fail closed even
	// if this package's local pre-dial check were ever removed by mistake —
	// but Subscribe rejects it before dialing, so assert that directly
	// (see TestFaultSubscribeAgentStatusChangedMissingPaneID for the
	// against-a-fake-socket version of this same claim).
	_, err = c.Subscribe(ctx, herdrwire.SubscriptionSpec{Type: herdrwire.SubscribePaneAgentStatusChanged})
	if err == nil {
		t.Fatal("live Subscribe(pane.agent_status_changed, no pane_id): want error, got nil")
	}
}

func asErr(err error, target **herdrwire.WireError) bool {
	return errors.As(err, target)
}
