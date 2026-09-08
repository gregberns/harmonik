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
	"sync"

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

	defaultNamespace = "harnesstestplugin"
	defaultJournal   = "records"
)

type server struct {
	kernelv1.UnimplementedPluginServiceServer

	namespace string
	channel   string
	journal   string

	mu     sync.RWMutex
	conn   *grpc.ClientConn
	kernel kernelv1.KernelServiceClient
}

func (s *server) Describe(context.Context, *kernelv1.DescribeRequest) (*kernelv1.DescribeResponse, error) {
	return &kernelv1.DescribeResponse{
		Manifest: &kernelv1.PluginManifest{
			Namespace:  s.namespace,
			Version:    "0.0.0-harness",
			ApiVersion: 1,
			Channels: []*kernelv1.ChannelDecl{
				{Name: s.channel, Type: kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB},
			},
			Interests: []*kernelv1.InterestDecl{
				{Kind: &kernelv1.InterestDecl_Channel{
					Channel: &kernelv1.ChannelInterest{Pattern: s.channel},
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

	impl := &server{namespace: namespace, channel: channel, journal: journal}

	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: host.Handshake,
		Plugins: map[string]goplugin.Plugin{
			host.PluginServiceName: host.NewGRPCPlugin(impl),
		},
		GRPCServer: goplugin.DefaultGRPCServer,
	})
}
