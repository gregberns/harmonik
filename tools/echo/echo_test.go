package echo_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"

	"google.golang.org/grpc"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
	"github.com/gregberns/harmonik/tools/echo"
)

func TestManifestDeclaresOnePingChannel(t *testing.T) {
	m := echo.Manifest()

	if m.GetNamespace() != echo.Namespace {
		t.Fatalf("namespace = %q, want %q", m.GetNamespace(), echo.Namespace)
	}
	if len(m.GetChannels()) != 1 {
		t.Fatalf("channels = %d, want 1", len(m.GetChannels()))
	}
	ch := m.GetChannels()[0]
	if ch.GetName() != echo.PingChannel || ch.GetType() != kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB {
		t.Fatalf("channel = %+v, want name=%q type=PUBSUB", ch, echo.PingChannel)
	}
	if len(m.GetInterests()) != 1 || m.GetInterests()[0].GetChannel().GetPattern() != echo.PingChannel {
		t.Fatalf("interests = %+v, want one interest in %q", m.GetInterests(), echo.PingChannel)
	}
}

// fakeKernel records every JournalAppend it receives so a test can assert on
// what the plugin actually sent, without a real kernel process.
type fakeKernel struct {
	kernelv1.UnimplementedKernelServiceServer
	received chan *kernelv1.JournalAppendRequest
}

func (k *fakeKernel) JournalAppend(_ context.Context, req *kernelv1.JournalAppendRequest) (*kernelv1.JournalAppendResponse, error) {
	k.received <- req
	return &kernelv1.JournalAppendResponse{Seqs: []uint64{1}, Synced: req.GetSync()}, nil
}

// TestDeliverJournalsThePayload drives echo.Server through the exact path the
// kernel drives it: Start hands it an endpoint, then Deliver carries a
// payload. It asserts the payload lands in the "seen" journal, sync=true,
// unchanged.
func TestDeliverJournalsThePayload(t *testing.T) {
	ctx := context.Background()
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	kernel := &fakeKernel{received: make(chan *kernelv1.JournalAppendRequest, 1)}
	srv := grpc.NewServer()
	kernelv1.RegisterKernelServiceServer(srv, kernel)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		if err := <-serveErr; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Errorf("srv.Serve: %v", err)
		}
	})

	plugin := &echo.Server{}
	if _, err := plugin.Start(ctx, &kernelv1.StartRequest{KernelEndpoint: lis.Addr().String()}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if _, err := plugin.Stop(ctx, &kernelv1.StopRequest{}); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})

	payload := []byte("hello")
	if _, err := plugin.Deliver(ctx, &kernelv1.DeliverRequest{
		Envelope: &kernelv1.Envelope{Channel: echo.PingChannel, Payload: payload},
	}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	select {
	case got := <-kernel.received:
		if got.GetJournal() != echo.SeenJournal {
			t.Errorf("journal = %q, want %q", got.GetJournal(), echo.SeenJournal)
		}
		if !got.GetSync() {
			t.Error("sync = false, want true")
		}
		if len(got.GetRecords()) != 1 || !bytes.Equal(got.GetRecords()[0], payload) {
			t.Errorf("records = %v, want [%q]", got.GetRecords(), payload)
		}
	default:
		t.Fatal("kernel never received a JournalAppend call")
	}
}

// TestDeliverBeforeStartFails guards the rule that a plugin holds no state
// across a reload: without Start re-establishing the kernel connection,
// Deliver must refuse rather than silently drop the payload.
func TestDeliverBeforeStartFails(t *testing.T) {
	plugin := &echo.Server{}
	_, err := plugin.Deliver(context.Background(), &kernelv1.DeliverRequest{
		Envelope: &kernelv1.Envelope{Channel: echo.PingChannel, Payload: []byte("x")},
	})
	if err == nil {
		t.Fatal("Deliver before Start: want error, got nil")
	}
}
