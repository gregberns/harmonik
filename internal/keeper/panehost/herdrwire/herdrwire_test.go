package herdrwire_test

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/keeper/panehost/herdrwire"
	"github.com/gregberns/harmonik/internal/keeper/panehost/herdrwire/herdrfake"
)

// logIfErr reports a fake-handler write failure without discarding it —
// these run in a server goroutine, not the test goroutine, so t.Fatal is
// not an option; a lost fake-server write also usually shows up as the
// test's own assertion failing, which carries the real signal.
func logIfErr(err error) {
	if err != nil {
		slog.WarnContext(context.Background(), "herdrwire_test: fake handler write failed", "err", err)
	}
}

func newTestClient(t *testing.T, methods map[string]herdrfake.MethodHandler) *herdrwire.Client {
	t.Helper()
	srv, err := herdrfake.NewMethodServer(herdrwire.ProtocolVersion, methods)
	if err != nil {
		t.Fatalf("start fake server: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := srv.Close(); closeErr != nil {
			t.Logf("close fake server: %v", closeErr)
		}
	})
	return herdrwire.NewClient(srv.Path)
}

func ctxT(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestPing(t *testing.T) {
	c := newTestClient(t, nil)
	got, err := c.Ping(ctxT(t))
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if got.Protocol != herdrwire.ProtocolVersion {
		t.Fatalf("Ping protocol = %d, want %d", got.Protocol, herdrwire.ProtocolVersion)
	}
}

func TestAgentStart(t *testing.T) {
	c := newTestClient(t, map[string]herdrfake.MethodHandler{
		"agent.start": func(req herdrfake.Request, conn net.Conn) {
			logIfErr(herdrfake.WriteResult(conn, req.ID, map[string]any{
				"type": "agent_started", "agent": "harmonik-abc123-crew-a", "argv": []string{"claude"},
			}))
		},
	})
	out, err := c.AgentStart(ctxT(t), herdrwire.AgentStartParams{
		Name: "harmonik-abc123-crew-a", Kind: "claude", PaneID: "w1:p2", TimeoutMs: 5000,
	})
	if err != nil {
		t.Fatalf("AgentStart: %v", err)
	}
	if out.Agent != "harmonik-abc123-crew-a" {
		t.Fatalf("AgentStart.Agent = %q", out.Agent)
	}
}

func TestAgentStartInvalidNameFailsClosed(t *testing.T) {
	c := newTestClient(t, map[string]herdrfake.MethodHandler{
		"agent.start": func(req herdrfake.Request, conn net.Conn) {
			logIfErr(herdrfake.WriteError(conn, req.ID, "invalid_agent_name",
				"agent name must start with a lowercase letter and contain only lowercase letters, digits, '-' or '_' (1-32 characters)"))
		},
	})
	_, err := c.AgentStart(ctxT(t), herdrwire.AgentStartParams{
		Name: "Not-Valid", Kind: "claude", PaneID: "w1:p2",
	})
	var wireErr *herdrwire.WireError
	if err == nil {
		t.Fatal("AgentStart: want error for invalid name, got nil")
	}
	if !asWireError(err, &wireErr) || wireErr.Code != "invalid_agent_name" {
		t.Fatalf("AgentStart error = %#v, want *WireError{Code: invalid_agent_name}", err)
	}
}

func TestAgentSendKeys(t *testing.T) {
	c := newTestClient(t, map[string]herdrfake.MethodHandler{
		"agent.send_keys": func(req herdrfake.Request, conn net.Conn) {
			logIfErr(herdrfake.WriteResult(conn, req.ID, map[string]any{"type": "ok"}))
		},
	})
	if err := c.AgentSendKeys(ctxT(t), herdrwire.AgentSendKeysParams{Target: "a", Keys: []string{"Enter"}}); err != nil {
		t.Fatalf("AgentSendKeys: %v", err)
	}
}

func TestAgentWait(t *testing.T) {
	c := newTestClient(t, map[string]herdrfake.MethodHandler{
		"agent.wait": func(req herdrfake.Request, conn net.Conn) {
			logIfErr(herdrfake.WriteResult(conn, req.ID, map[string]any{"type": "wait_matched", "agent": map[string]any{"agent_status": "idle"}}))
		},
	})
	out, err := c.AgentWait(ctxT(t), herdrwire.AgentWaitParams{Target: "a", Until: []herdrwire.AgentStatus{herdrwire.AgentStatusIdle}, TimeoutMs: 1000})
	if err != nil {
		t.Fatalf("AgentWait: %v", err)
	}
	if out.Type != "wait_matched" {
		t.Fatalf("AgentWait.Type = %q", out.Type)
	}
}

func TestAgentPrompt(t *testing.T) {
	c := newTestClient(t, map[string]herdrfake.MethodHandler{
		"agent.prompt": func(req herdrfake.Request, conn net.Conn) {
			logIfErr(herdrfake.WriteResult(conn, req.ID, map[string]any{"type": "agent_prompted", "agent": map[string]any{"agent_status": "working"}}))
		},
	})
	out, err := c.AgentPrompt(ctxT(t), herdrwire.AgentPromptParams{
		Target: "a", Text: "hello", Wait: &herdrwire.AgentPromptWaitOptions{Until: []herdrwire.AgentStatus{herdrwire.AgentStatusIdle}},
	})
	if err != nil {
		t.Fatalf("AgentPrompt: %v", err)
	}
	if out.Type != "agent_prompted" {
		t.Fatalf("AgentPrompt.Type = %q", out.Type)
	}
}

func TestPaneSplitReadCloseSequence(t *testing.T) {
	c := newTestClient(t, map[string]herdrfake.MethodHandler{
		"pane.split": func(req herdrfake.Request, conn net.Conn) {
			logIfErr(herdrfake.WriteResult(conn, req.ID, map[string]any{
				"type": "pane_info",
				"pane": map[string]any{"pane_id": "w3:p9", "terminal_id": "t1", "workspace_id": "w3", "tab_id": "w3:t1", "focused": false, "agent_status": "unknown", "revision": 0},
			}))
		},
		"pane.read": func(req herdrfake.Request, conn net.Conn) {
			logIfErr(herdrfake.WriteResult(conn, req.ID, map[string]any{
				"type": "pane_read",
				"read": map[string]any{"pane_id": "w3:p9", "workspace_id": "w3", "tab_id": "w3:t1", "source": "recent", "format": "text", "text": "hi\n", "revision": 0, "truncated": false},
			}))
		},
		"pane.close": func(req herdrfake.Request, conn net.Conn) {
			logIfErr(herdrfake.WriteResult(conn, req.ID, map[string]any{"type": "ok"}))
		},
	})
	ctx := ctxT(t)

	split, err := c.PaneSplit(ctx, herdrwire.PaneSplitParams{Direction: herdrwire.SplitDown, TargetPaneID: "w3:p1"})
	if err != nil {
		t.Fatalf("PaneSplit: %v", err)
	}
	if split.Pane.PaneID != "w3:p9" {
		t.Fatalf("PaneSplit.Pane.PaneID = %q", split.Pane.PaneID)
	}

	read, err := c.PaneRead(ctx, herdrwire.PaneReadParams{PaneID: split.Pane.PaneID, Source: herdrwire.ReadSourceRecent})
	if err != nil {
		t.Fatalf("PaneRead: %v", err)
	}
	if read.Read.Text != "hi\n" {
		t.Fatalf("PaneRead.Read.Text = %q", read.Read.Text)
	}

	if err := c.PaneClose(ctx, split.Pane.PaneID); err != nil {
		t.Fatalf("PaneClose: %v", err)
	}
}

func TestPaneProcessInfo(t *testing.T) {
	c := newTestClient(t, map[string]herdrfake.MethodHandler{
		"pane.process_info": func(req herdrfake.Request, conn net.Conn) {
			logIfErr(herdrfake.WriteResult(conn, req.ID, map[string]any{
				"type": "pane_process_info",
				"process_info": map[string]any{
					"pane_id": "w3:p9", "shell_pid": 123, "foreground_process_group_id": 123,
					"foreground_processes": []map[string]any{{"pid": 123, "name": "zsh"}},
				},
			}))
		},
	})
	out, err := c.PaneProcessInfo(ctxT(t), "w3:p9")
	if err != nil {
		t.Fatalf("PaneProcessInfo: %v", err)
	}
	if len(out.ProcessInfo.ForegroundProcesses) != 1 || out.ProcessInfo.ForegroundProcesses[0].Name != "zsh" {
		t.Fatalf("PaneProcessInfo.ForegroundProcesses = %+v", out.ProcessInfo.ForegroundProcesses)
	}
}

func TestPaneSendInput(t *testing.T) {
	c := newTestClient(t, map[string]herdrfake.MethodHandler{
		"pane.send_input": func(req herdrfake.Request, conn net.Conn) {
			logIfErr(herdrfake.WriteResult(conn, req.ID, map[string]any{"type": "ok"}))
		},
	})
	if err := c.PaneSendInput(ctxT(t), herdrwire.PaneSendInputParams{PaneID: "w3:p9", Text: "hi\n"}); err != nil {
		t.Fatalf("PaneSendInput: %v", err)
	}
}

func TestPaneList(t *testing.T) {
	c := newTestClient(t, map[string]herdrfake.MethodHandler{
		"pane.list": func(req herdrfake.Request, conn net.Conn) {
			logIfErr(herdrfake.WriteResult(conn, req.ID, map[string]any{
				"type": "pane_list", "panes": []map[string]any{{"pane_id": "w1:p1", "terminal_id": "t", "workspace_id": "w1", "tab_id": "w1:t1", "focused": true, "agent_status": "idle", "revision": 1}},
			}))
		},
	})
	out, err := c.PaneList(ctxT(t), "")
	if err != nil {
		t.Fatalf("PaneList: %v", err)
	}
	if len(out.Panes) != 1 || out.Panes[0].PaneID != "w1:p1" {
		t.Fatalf("PaneList.Panes = %+v", out.Panes)
	}
}

func TestSessionSnapshot(t *testing.T) {
	c := newTestClient(t, map[string]herdrfake.MethodHandler{
		"session.snapshot": func(req herdrfake.Request, conn net.Conn) {
			logIfErr(herdrfake.WriteResult(conn, req.ID, map[string]any{
				"type": "session_snapshot",
				"snapshot": map[string]any{
					"version": "0.9.0-fake", "protocol": herdrwire.ProtocolVersion,
					"panes": []map[string]any{}, "agents": []map[string]any{{"pane_id": "w1:p1", "agent_status": "idle", "name": "a"}},
				},
			}))
		},
	})
	out, err := c.SessionSnapshot(ctxT(t))
	if err != nil {
		t.Fatalf("SessionSnapshot: %v", err)
	}
	if len(out.Snapshot.Agents) != 1 || out.Snapshot.Agents[0].Name != "a" {
		t.Fatalf("SessionSnapshot.Agents = %+v", out.Snapshot.Agents)
	}
}

func TestPaneReportMetadata(t *testing.T) {
	c := newTestClient(t, map[string]herdrfake.MethodHandler{
		"pane.report_metadata": func(req herdrfake.Request, conn net.Conn) {
			logIfErr(herdrfake.WriteResult(conn, req.ID, map[string]any{"type": "ok"}))
		},
	})
	err := c.PaneReportMetadata(ctxT(t), herdrwire.PaneReportMetadataParams{
		PaneID: "w1:p1", Source: "keeper", Tokens: map[string]string{"band": "warn"},
	})
	if err != nil {
		t.Fatalf("PaneReportMetadata: %v", err)
	}
}

func TestSubscribePaneCreatedAndStatusChanged(t *testing.T) {
	srv, err := herdrfake.Start(func(conn net.Conn) {
		defer func() { logIfErr(conn.Close()) }()
		req, _, rerr := herdrfake.ReadRequest(conn)
		if rerr != nil {
			return
		}
		if req.Method == "ping" {
			logIfErr(herdrfake.WriteResult(conn, req.ID, herdrfake.PongResult(herdrwire.ProtocolVersion)))
			return
		}
		if req.Method != "events.subscribe" {
			logIfErr(herdrfake.WriteError(conn, req.ID, "method_not_found", "fake"))
			return
		}
		logIfErr(herdrfake.WriteResult(conn, req.ID, map[string]any{"type": "subscription_started"}))
		logIfErr(herdrfake.WriteEvent(conn, "pane_created", map[string]any{
			"pane": map[string]any{"pane_id": "w3:p9", "terminal_id": "t", "workspace_id": "w3", "tab_id": "w3:t1", "focused": false, "agent_status": "unknown", "revision": 0},
		}))
		logIfErr(herdrfake.WriteEvent(conn, "pane_agent_status_changed", map[string]any{
			"pane_id": "w3:p9", "workspace_id": "w3", "agent": "a", "agent_status": "working",
		}))
	})
	if err != nil {
		t.Fatalf("start fake server: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := srv.Close(); closeErr != nil {
			t.Logf("close fake server: %v", closeErr)
		}
	})

	c := herdrwire.NewClient(srv.Path)
	sub, err := c.Subscribe(ctxT(t), herdrwire.SubscribePaneCreated, herdrwire.SubscribePaneAgentStatusChanged)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() {
		if closeErr := sub.Close(); closeErr != nil {
			t.Logf("close subscription: %v", closeErr)
		}
	}()

	ev1, err := sub.Next(ctxT(t))
	if err != nil {
		t.Fatalf("Next (pane_created): %v", err)
	}
	if ev1.PaneCreated == nil || ev1.PaneCreated.Pane.PaneID != "w3:p9" {
		t.Fatalf("Next (pane_created) = %+v", ev1)
	}

	ev2, err := sub.Next(ctxT(t))
	if err != nil {
		t.Fatalf("Next (status_changed): %v", err)
	}
	if ev2.PaneAgentStatusChanged == nil || ev2.PaneAgentStatusChanged.AgentStatus != herdrwire.AgentStatusWorking {
		t.Fatalf("Next (status_changed) = %+v", ev2)
	}
}

func asWireError(err error, target **herdrwire.WireError) bool {
	return errors.As(err, target)
}
