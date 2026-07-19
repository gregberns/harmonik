package daemon

// versionnego_w4_test.go — Wave-4 regression: real HC-009 version negotiation
// (max intersection, ErrProtocolMismatch on empty, capabilities-absent abort
// at the HandlerCapabilitiesTimeout) instead of a hardcoded selected_version:1.

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

func TestNegotiateWireVersion_SelectsMaxIntersection(t *testing.T) {
	orig := daemonSupportedWireVersions
	daemonSupportedWireVersions = []int{1, 2}
	defer func() { daemonSupportedWireVersions = orig }()

	sel, err := negotiateWireVersion([]int{1, 2, 3})
	if err != nil {
		t.Fatalf("negotiateWireVersion: %v", err)
	}
	if sel != 2 {
		t.Errorf("selected = %d, want 2 (max of intersection)", sel)
	}
}

func TestNegotiateWireVersion_EmptyIntersection_ProtocolMismatch(t *testing.T) {
	for _, handlerVersions := range [][]int{nil, {}, {99}} {
		_, err := negotiateWireVersion(handlerVersions)
		if err == nil {
			t.Fatalf("handler versions %v: want error, got nil", handlerVersions)
		}
		if !errors.Is(err, handlercontract.ErrProtocolMismatch) {
			t.Errorf("handler versions %v: err = %v, want ErrProtocolMismatch", handlerVersions, err)
		}
		if !errors.Is(err, handlercontract.ErrStructural) {
			t.Errorf("handler versions %v: err = %v, must also wrap ErrStructural (§4.6)", handlerVersions, err)
		}
	}
}

// TestSessionIDInterceptor_NegotiationSuccess verifies the happy path: a
// handler_capabilities line with a mutually supported version fires the cb and
// records the selected version.
func TestSessionIDInterceptor_NegotiationSuccess(t *testing.T) {
	line := `{"type":"handler_capabilities","supported_versions":[1],"claude_session_id":"w4-nego-id"}` + "\n"

	fired := make(chan string, 1)
	ic := newSessionIDInterceptor(strings.NewReader(line), func(id string) { fired <- id })

	buf := make([]byte, 4096)
	if _, err := ic.Read(buf); err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("Read: %v", err)
	}

	select {
	case id := <-fired:
		if id != "w4-nego-id" {
			t.Errorf("cb id = %q, want w4-nego-id", id)
		}
	default:
		t.Fatal("cb not fired on successful negotiation")
	}
	if got := ic.SelectedVersion(); got != 1 {
		t.Errorf("SelectedVersion = %d, want 1", got)
	}
	if err := ic.NegotiationErr(); err != nil {
		t.Errorf("NegotiationErr = %v, want nil", err)
	}
}

// TestSessionIDInterceptor_EmptyIntersection_PoisonsRead verifies that a
// capabilities line with no mutually supported version suppresses the cb and
// surfaces a sticky ErrProtocolMismatch on the read path (which terminates
// the Watcher with a structural failure).
func TestSessionIDInterceptor_EmptyIntersection_PoisonsRead(t *testing.T) {
	line := `{"type":"handler_capabilities","supported_versions":[99],"claude_session_id":"w4-bad-id"}` + "\n"

	ic := newSessionIDInterceptor(strings.NewReader(line), func(string) {
		t.Error("cb fired despite failed version negotiation")
	})

	buf := make([]byte, 4096)
	_, err := ic.Read(buf)
	if !errors.Is(err, handlercontract.ErrProtocolMismatch) {
		t.Fatalf("first Read err = %v, want ErrProtocolMismatch", err)
	}
	// Sticky: subsequent reads keep returning the mismatch.
	if _, err := ic.Read(buf); !errors.Is(err, handlercontract.ErrProtocolMismatch) {
		t.Errorf("second Read err = %v, want sticky ErrProtocolMismatch", err)
	}
}

// TestSessionIDInterceptor_CapabilitiesAbsentTimeout verifies the §7.2 abort:
// when no handler_capabilities arrives within the timeout, a blocked Read
// unwedges with ErrProtocolMismatch.
func TestSessionIDInterceptor_CapabilitiesAbsentTimeout(t *testing.T) {
	origTimeout := capsAbsentTimeout
	capsAbsentTimeout = 50 * time.Millisecond
	defer func() { capsAbsentTimeout = origTimeout }()

	pr, pw := io.Pipe() // silent handler: never writes
	defer pw.Close()    //nolint:errcheck // best-effort close of test pipe writer

	ic := newSessionIDInterceptor(pr, func(string) {
		t.Error("cb fired despite capabilities-absent timeout")
	})

	errCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 128)
		_, err := ic.Read(buf)
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, handlercontract.ErrProtocolMismatch) {
			t.Fatalf("Read err = %v, want ErrProtocolMismatch", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked Read did not unwedge at the capabilities-absent timeout")
	}
}

// TestSendVersionSelectedACK_Version verifies the ACK carries the negotiated
// version: the daemon-default max when omitted, or an explicit override.
func TestSendVersionSelectedACK_Version(t *testing.T) {
	var got string
	sess := versionNegoFakeSess{captured: &got}

	if err := sendVersionSelectedACK(context.Background(), sess); err != nil {
		t.Fatalf("sendVersionSelectedACK: %v", err)
	}
	want := `{"type":"version_selected","selected_version":1}`
	if got != want {
		t.Errorf("default ACK = %q, want %q", got, want)
	}

	if err := sendVersionSelectedACK(context.Background(), sess, 2); err != nil {
		t.Fatalf("sendVersionSelectedACK(2): %v", err)
	}
	want2 := `{"type":"version_selected","selected_version":2}`
	if got != want2 {
		t.Errorf("explicit ACK = %q, want %q", got, want2)
	}
}

type versionNegoFakeSess struct{ captured *string }

func (f versionNegoFakeSess) SendInput(_ context.Context, line string) error {
	*f.captured = line
	return nil
}
