// Command harnesstestplugin is a minimal, real subprocess plugin used only
// by kernel/cmd/harmonikd's own tests: it declares one channel, dials the
// kernel endpoint it is handed at Start, and on Deliver calls back into
// KernelService's JournalAppend with the delivered payload. Every knob is an
// environment variable so a test can build this binary once and launch it
// several ways. It exists so those tests exercise the same wire round trip
// (Deliver -> the plugin's own kernel client -> JournalAppend) a real
// launched plugin does, without kernel/ needing to name any real plugin.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
	"github.com/gregberns/harmonik/kernel/host"
)

const (
	namespaceEnv = "HARMONIK_HARNESSTESTPLUGIN_NAMESPACE"
	channelEnv   = "HARMONIK_HARNESSTESTPLUGIN_CHANNEL"
	journalEnv   = "HARMONIK_HARNESSTESTPLUGIN_JOURNAL"
	// deliverDelayEnv makes every Deliver wait this many milliseconds before
	// it writes to the journal — a deliberately slow, but finishing, handler.
	deliverDelayEnv = "HARMONIK_HARNESSTESTPLUGIN_DELIVER_DELAY_MS"
	// deliverHangEnv makes every Deliver block until its context is cancelled
	// and never write to the journal — a hung handler the drain gate must
	// cancel and kill through.
	deliverHangEnv = "HARMONIK_HARNESSTESTPLUGIN_DELIVER_HANG"
	// groupEnv, when set, switches the declared channel from PUBSUB to
	// POINT_TO_POINT and makes the interest a competing-consumer member of the
	// named group — so two of these plugins in one mesh split one stream.
	groupEnv = "HARMONIK_HARNESSTESTPLUGIN_GROUP"

	defaultNamespace = "harnesstestplugin"
	defaultJournal   = "records"
)

type server struct {
	kernelv1.UnimplementedPluginServiceServer

	namespace    string
	channel      string
	journal      string
	group        string
	deliverDelay time.Duration
	deliverHang  bool

	mu     sync.RWMutex
	conn   *grpc.ClientConn
	kernel kernelv1.KernelServiceClient
}

func (s *server) Describe(context.Context, *kernelv1.DescribeRequest) (*kernelv1.DescribeResponse, error) {
	channelType := kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB
	if s.group != "" {
		channelType = kernelv1.ChannelType_CHANNEL_TYPE_POINT_TO_POINT
	}
	return &kernelv1.DescribeResponse{
		Manifest: &kernelv1.PluginManifest{
			Namespace:  s.namespace,
			Version:    "0.0.0-harness",
			ApiVersion: 1,
			Channels: []*kernelv1.ChannelDecl{
				{Name: s.channel, Type: channelType},
			},
			Interests: []*kernelv1.InterestDecl{
				{Kind: &kernelv1.InterestDecl_Channel{
					Channel: &kernelv1.ChannelInterest{Pattern: s.channel, Group: s.group},
				}},
			},
		},
	}, nil
}

func (s *server) Start(_ context.Context, req *kernelv1.StartRequest) (*kernelv1.StartResponse, error) {
	conn, err := grpc.NewClient(req.GetKernelEndpoint(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("harnesstestplugin: dial %q: %w", req.GetKernelEndpoint(), err)
	}
	s.mu.Lock()
	s.conn, s.kernel = conn, kernelv1.NewKernelServiceClient(conn)
	s.mu.Unlock()
	return &kernelv1.StartResponse{}, nil
}

func (s *server) Stop(context.Context, *kernelv1.StopRequest) (*kernelv1.StopResponse, error) {
	s.mu.Lock()
	conn := s.conn
	s.conn, s.kernel = nil, nil
	s.mu.Unlock()
	if conn == nil {
		return &kernelv1.StopResponse{}, nil
	}
	if err := conn.Close(); err != nil {
		return nil, fmt.Errorf("harnesstestplugin: close kernel connection: %w", err)
	}
	return &kernelv1.StopResponse{}, nil
}

func (s *server) Health(context.Context, *kernelv1.HealthRequest) (*kernelv1.HealthResponse, error) {
	return &kernelv1.HealthResponse{Ok: true}, nil
}

func (s *server) Deliver(ctx context.Context, req *kernelv1.DeliverRequest) (*kernelv1.DeliverResponse, error) {
	if s.deliverHang {
		<-ctx.Done()
		return nil, fmt.Errorf("harnesstestplugin: deliver hung until cancelled: %w", ctx.Err())
	}
	if s.deliverDelay > 0 {
		select {
		case <-time.After(s.deliverDelay):
		case <-ctx.Done():
			return nil, fmt.Errorf("harnesstestplugin: deliver cancelled mid-delay: %w", ctx.Err())
		}
	}

	s.mu.RLock()
	kernel := s.kernel
	s.mu.RUnlock()
	if kernel == nil {
		return nil, fmt.Errorf("harnesstestplugin: Deliver called before Start")
	}
	if _, err := kernel.JournalAppend(ctx, &kernelv1.JournalAppendRequest{
		Journal: s.journal,
		Records: [][]byte{req.GetEnvelope().GetPayload()},
		Sync:    true,
	}); err != nil {
		return nil, fmt.Errorf("harnesstestplugin: journal append: %w", err)
	}
	return &kernelv1.DeliverResponse{}, nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	namespace := envOrDefault(namespaceEnv, defaultNamespace)
	channel := envOrDefault(channelEnv, namespace+".in")
	journal := envOrDefault(journalEnv, defaultJournal)

	var deliverDelay time.Duration
	if ms, err := strconv.Atoi(os.Getenv(deliverDelayEnv)); err == nil && ms > 0 {
		deliverDelay = time.Duration(ms) * time.Millisecond
	}

	impl := &server{
		namespace:    namespace,
		channel:      channel,
		journal:      journal,
		group:        os.Getenv(groupEnv),
		deliverDelay: deliverDelay,
		deliverHang:  os.Getenv(deliverHangEnv) != "",
	}

	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: host.Handshake,
		Plugins: map[string]goplugin.Plugin{
			host.PluginServiceName: host.NewGRPCPlugin(impl),
		},
		GRPCServer: goplugin.DefaultGRPCServer,
	})
}
