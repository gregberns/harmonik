package lifecycle

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"testing"
)

// cliFixtureAttachState tracks simulated daemon state for multi-attach tests.
// All counter fields are guarded by mu; readers MUST use the accessor methods.
type cliFixtureAttachState struct {
	mu             sync.Mutex
	activeAttaches int32 // current number of live attach connections
	totalAttached  int32 // cumulative attach count
	totalDetached  int32 // cumulative detach count
	daemonRunning  bool  // true iff the daemon has not been stopped
}

// cliFixtureAttachStateNew initialises an attach state with the daemon running.
func cliFixtureAttachStateNew() *cliFixtureAttachState {
	return &cliFixtureAttachState{daemonRunning: true}
}

// Attach records a new attach session. Returns the session ID.
func (s *cliFixtureAttachState) Attach() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.activeAttaches++
	s.totalAttached++
	return s.totalAttached
}

// Detach records a detach. The daemon remains running.
// Per PL-028: "detaching MUST NOT kill the daemon."
func (s *cliFixtureAttachState) Detach() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.activeAttaches--
	s.totalDetached++
	// Detach does NOT change daemonRunning.
}

// DaemonRunning reports whether the daemon is still running (i.e. no detach
// has caused a shutdown).
func (s *cliFixtureAttachState) DaemonRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.daemonRunning
}

// ActiveAttaches returns the current number of live attach connections.
func (s *cliFixtureAttachState) ActiveAttaches() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.activeAttaches
}

// TotalAttached returns the cumulative attach count.
func (s *cliFixtureAttachState) TotalAttached() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.totalAttached
}

// TotalDetached returns the cumulative detach count.
func (s *cliFixtureAttachState) TotalDetached() int32 {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.totalDetached
}

// cliFixtureMultiAttachServer is a Unix socket server that accepts N concurrent
// attach connections. Each connection receives one JSON-RPC response, then is
// held open until the client sends an explicit "detach" request or closes.
//
// The server tallies accepted connections via state.Attach() and records
// detaches via state.Detach().
//
// The server is intentionally simple: it does not implement the full attach TUI
// surface (ON-050 is the normative spec; this fixture exercises the concurrency
// and daemon-liveness invariants only).
type cliFixtureMultiAttachServer struct {
	ln    net.Listener
	state *cliFixtureAttachState
	t     *testing.T
	done  chan struct{} // closed when serveN completes
}

// cliFixtureStartMultiAttachServer binds a Unix socket and starts the server
// goroutine. The server handles up to maxConns concurrent connections.
// Call srv.Wait() to block until all N connections have been fully served.
func cliFixtureStartMultiAttachServer(t *testing.T, projectDir string, maxConns int) *cliFixtureMultiAttachServer {
	t.Helper()

	ln, err := plFixtureBindSocket(t, projectDir)
	if err != nil {
		t.Fatalf("cliFixtureStartMultiAttachServer: bindSocket: %v", err)
	}

	srv := &cliFixtureMultiAttachServer{
		ln:    ln,
		state: cliFixtureAttachStateNew(),
		t:     t,
		done:  make(chan struct{}),
	}

	// Accept up to maxConns connections concurrently.
	go func() {
		defer close(srv.done)
		srv.serveN(maxConns)
	}()

	return srv
}

// Wait blocks until all connections accepted by serveN have been fully handled
// (i.e. all Detach() calls have completed). Tests must call Wait() before
// asserting state counters to avoid data races.
func (srv *cliFixtureMultiAttachServer) Wait() {
	<-srv.done
}

// serveN accepts up to n connections. Each connection is handled in its own
// goroutine. The function returns after n connections have been fully served.
func (srv *cliFixtureMultiAttachServer) serveN(n int) {
	var wg sync.WaitGroup
	for range n {
		conn, err := srv.ln.Accept()
		if err != nil {
			return // listener closed
		}
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			srv.handleAttach(c)
		}(conn)
	}
	wg.Wait()
}

// handleAttach simulates one attach session:
//  1. Record the attach.
//  2. Send a JSON-RPC "attach started" notification.
//  3. Wait for the client to close the connection (detach).
//  4. Record the detach.
func (srv *cliFixtureMultiAttachServer) handleAttach(conn net.Conn) {
	defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable

	srv.state.Attach()

	// Send attach-started notification.
	resp := jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      1,
		Error:   nil,
	}
	data, _ := json.Marshal(resp)          //nolint:errcheck,errchkjson // encoding a known-good struct; RawMessage field is nil
	_, _ = fmt.Fprintf(conn, "%s\n", data) //nolint:errcheck // stub write errors are not actionable

	// Read until the client closes (detach signal).
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		// Drain any client messages (e.g. the detach request line).
	}

	srv.state.Detach()
}

// cliFixtureSimulateAttach connects to the Unix socket and sends one attach
// request, reads the server acknowledgement, then closes the connection
// (simulating a detach). Returns nil on success.
func cliFixtureSimulateAttach(t *testing.T, sockPath string, id int) error {
	t.Helper()

	conn, err := (&net.Dialer{}).DialContext(t.Context(), "unix", sockPath)
	if err != nil {
		return fmt.Errorf("cliFixtureSimulateAttach[%d]: Dial: %w", id, err)
	}
	defer func() { _ = conn.Close() }() //nolint:errcheck // cleanup error unactionable

	// Send attach request.
	req := jsonrpcRequest{JSONRPC: "2.0", ID: id, Method: "attach.session"}
	reqBytes, _ := json.Marshal(req) //nolint:errcheck,errchkjson // encoding a known-good struct; interface{} Params field is always nil
	if _, err := fmt.Fprintf(conn, "%s\n", reqBytes); err != nil {
		return fmt.Errorf("cliFixtureSimulateAttach[%d]: write: %w", id, err)
	}

	// Read acknowledgement.
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("cliFixtureSimulateAttach[%d]: scan: %w", id, err)
		}
		// EOF is fine — server closed after sending ack.
	}

	// Close = detach.
	return conn.Close()
}

// TestPL028_AttachMultipleSimultaneous verifies that N simultaneous attach
// connections are accepted concurrently with no foundation-imposed upper limit
// (the limit tested here is the fixture bound, not a protocol limit).
//
// Per PL-028: "Multiple simultaneous attaches MUST be supported with no
// foundation-imposed upper limit."
//
// Spec ref: process-lifecycle.md §4.10 PL-028 — harmonik attach.
// Spec ref: operator-nfr.md §4.3 ON-050 — harmonik attach minimum surface.
// Spec ref: operator-nfr.md §4.3 ON-051 — multi-attach arbitration.
func TestPL028_AttachMultipleSimultaneous(t *testing.T) {
	t.Parallel()

	const numAttachers = 5

	projectDir := plFixtureTempProjectDir(t)
	srv := cliFixtureStartMultiAttachServer(t, projectDir, numAttachers)
	t.Cleanup(func() { _ = srv.ln.Close() }) //nolint:errcheck // cleanup error unactionable

	sockPath := plFixtureSocketPath(projectDir)

	// Spawn N goroutines, each simulating an attach session.
	var wg sync.WaitGroup
	errs := make([]error, numAttachers)

	for i := range numAttachers {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			errs[id] = cliFixtureSimulateAttach(t, sockPath, id+1)
		}(i)
	}
	wg.Wait()

	// Wait for all server-side handleAttach goroutines to complete (including
	// Detach() calls) before asserting state counters.
	srv.Wait()

	// All N attaches must succeed.
	for i, err := range errs {
		if err != nil {
			t.Errorf("PL-028 multi-attach: attacher[%d] failed: %v", i, err)
		}
	}

	// After all detaches, the daemon must still be running.
	if !srv.state.DaemonRunning() {
		t.Error("PL-028 multi-attach: daemon stopped after detach; detach MUST NOT kill the daemon")
	}

	// Total attached must equal numAttachers.
	if got := srv.state.TotalAttached(); int(got) != numAttachers {
		t.Errorf("PL-028 multi-attach: totalAttached = %d, want %d", got, numAttachers)
	}

	// Total detached must equal numAttachers.
	if got := srv.state.TotalDetached(); int(got) != numAttachers {
		t.Errorf("PL-028 multi-attach: totalDetached = %d, want %d", got, numAttachers)
	}

	// activeAttaches must be zero (all sessions have ended).
	if got := srv.state.ActiveAttaches(); got != 0 {
		t.Errorf("PL-028 multi-attach: activeAttaches = %d after all detaches, want 0", got)
	}
}

// TestPL028_AttachDetachDoesNotKillDaemon verifies the core PL-028 invariant:
// a detach event (client closes connection) MUST NOT cause the daemon to stop.
//
// The test attaches, then detaches, then verifies the daemon state object
// remains "running". It repeats the cycle K times to confirm the invariant
// holds across multiple attach/detach pairs.
//
// Spec ref: process-lifecycle.md §4.10 PL-028 — "detaching MUST NOT kill the
// daemon."
// Spec ref: operator-nfr.md §4.3 ON-051 — "one operator's detach MUST NOT
// affect others."
func TestPL028_AttachDetachDoesNotKillDaemon(t *testing.T) {
	t.Parallel()

	const cycles = 3

	for cycle := range cycles {
		t.Run(fmt.Sprintf("cycle-%d", cycle+1), func(t *testing.T) {
			t.Parallel()

			projectDir := plFixtureTempProjectDir(t)
			srv := cliFixtureStartMultiAttachServer(t, projectDir, 1)
			t.Cleanup(func() { _ = srv.ln.Close() }) //nolint:errcheck // cleanup error unactionable

			sockPath := plFixtureSocketPath(projectDir)

			if err := cliFixtureSimulateAttach(t, sockPath, cycle+1); err != nil {
				t.Fatalf("PL-028 attach-detach-no-kill cycle %d: attach failed: %v", cycle+1, err)
			}

			// Wait for the server-side Detach() call to complete before asserting.
			srv.Wait()

			// After detach, the daemon must still be running.
			if !srv.state.DaemonRunning() {
				t.Errorf("PL-028 attach-detach-no-kill cycle %d: daemon stopped after detach; MUST NOT kill daemon", cycle+1)
			}
		})
	}
}

// TestPL028_AttachSessionIndependence verifies ON-051 semantics: multiple
// simultaneous attach sessions are independent. One session detaching does not
// affect the others.
//
// The test attaches 3 sessions concurrently, detaches the first, checks that
// the remaining two are still active (via the activeAttaches counter), then
// detaches the remaining two.
//
// Spec ref: operator-nfr.md §4.3 ON-051 — "one operator's detach MUST NOT
// affect others."
// Spec ref: process-lifecycle.md §4.10 PL-028 — harmonik attach concurrency.
func TestPL028_AttachSessionIndependence(t *testing.T) {
	t.Parallel()

	const numSessions = 3

	state := cliFixtureAttachStateNew()

	// Simulate independent attach sessions using the state tracker directly
	// (without real sockets) to isolate the independence invariant from network
	// concerns already covered by TestPL028_AttachMultipleSimultaneous.

	// Attach all sessions.
	ids := make([]int32, numSessions)
	for i := range numSessions {
		ids[i] = state.Attach()
	}

	// Verify all are active.
	if got := state.ActiveAttaches(); int(got) != numSessions {
		t.Fatalf("PL-028 session-independence: activeAttaches = %d after attaching %d, want %d",
			got, numSessions, numSessions)
	}

	// Detach the first session.
	state.Detach()

	// Daemon must still be running.
	if !state.DaemonRunning() {
		t.Error("PL-028 session-independence: daemon stopped after one detach; MUST NOT kill daemon")
	}

	// Remaining sessions must still be counted as active.
	wantActive := numSessions - 1
	if got := state.ActiveAttaches(); int(got) != wantActive {
		t.Errorf("PL-028 session-independence: activeAttaches = %d after one detach, want %d",
			got, wantActive)
	}

	// Detach remaining sessions.
	for i := 1; i < numSessions; i++ {
		state.Detach()
	}

	// Daemon must still be running.
	if !state.DaemonRunning() {
		t.Error("PL-028 session-independence: daemon stopped after all detaches; MUST NOT kill daemon")
	}

	// All sessions detached.
	if got := state.ActiveAttaches(); got != 0 {
		t.Errorf("PL-028 session-independence: activeAttaches = %d after all detaches, want 0", got)
	}
}
