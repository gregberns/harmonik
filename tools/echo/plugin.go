package echo

import (
	"context"
	"errors"
	"fmt"
	"sync"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

// ErrNotStarted is returned when Deliver is called before Start has dialed
// the kernel back.
var ErrNotStarted = errors.New("echo: Deliver called before Start")

// Handshake is the go-plugin magic cookie both echo and its launcher must
// agree on before either side trusts the connection.
var Handshake = goplugin.HandshakeConfig{
	ProtocolVersion:  apiVersion,
	MagicCookieKey:   "HARMONIK_PLUGIN",
	MagicCookieValue: Namespace,
}

// PluginKey names this plugin in a go-plugin ServeConfig/Plugins map.
const PluginKey = Namespace

// Server implements kernelv1.PluginServiceServer. It holds no state that
// must survive a reload other than the kernel connection Start hands it:
// per the kernel's reload contract, kernel_endpoint is re-read on every
// Start, never assumed to persist.
type Server struct {
	kernelv1.UnimplementedPluginServiceServer

	mu     sync.RWMutex
	conn   *grpc.ClientConn
	kernel kernelv1.KernelServiceClient
}

// Describe returns echo's manifest.
func (s *Server) Describe(context.Context, *kernelv1.DescribeRequest) (*kernelv1.DescribeResponse, error) {
	return &kernelv1.DescribeResponse{Manifest: Manifest()}, nil
}

// Start dials the kernel endpoint the kernel just handed us and keeps the
// client for Deliver to use.
func (s *Server) Start(_ context.Context, req *kernelv1.StartRequest) (*kernelv1.StartResponse, error) {
	conn, err := grpc.NewClient(req.GetKernelEndpoint(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("echo: dial kernel at %q: %w", req.GetKernelEndpoint(), err)
	}
	s.mu.Lock()
	s.conn, s.kernel = conn, kernelv1.NewKernelServiceClient(conn)
	s.mu.Unlock()
	return &kernelv1.StartResponse{}, nil
}

// Stop closes the kernel connection Start opened.
func (s *Server) Stop(context.Context, *kernelv1.StopRequest) (*kernelv1.StopResponse, error) {
	s.mu.Lock()
	conn := s.conn
	s.conn, s.kernel = nil, nil
	s.mu.Unlock()
	if conn == nil {
		return &kernelv1.StopResponse{}, nil
	}
	if err := conn.Close(); err != nil {
		return nil, fmt.Errorf("echo: close kernel connection: %w", err)
	}
	return &kernelv1.StopResponse{}, nil
}

// Health always reports ok: echo has no failure mode of its own between
// deliveries.
func (s *Server) Health(context.Context, *kernelv1.HealthRequest) (*kernelv1.HealthResponse, error) {
	return &kernelv1.HealthResponse{Ok: true}, nil
}

// Deliver journals the envelope's payload as seen.
func (s *Server) Deliver(ctx context.Context, req *kernelv1.DeliverRequest) (*kernelv1.DeliverResponse, error) {
	s.mu.RLock()
	kernel := s.kernel
	s.mu.RUnlock()
	if kernel == nil {
		return nil, ErrNotStarted
	}
	if _, err := kernel.JournalAppend(ctx, SeenAppend(req.GetEnvelope().GetPayload())); err != nil {
		return nil, fmt.Errorf("echo: journal seen: %w", err)
	}
	return &kernelv1.DeliverResponse{}, nil
}

// GRPCPlugin is the go-plugin adapter: it registers Server as the
// PluginService a launched echo process serves. GRPCClient exists only to
// satisfy go-plugin's GRPCPlugin interface; echo is a plugin, never a host,
// and never dispenses itself.
type GRPCPlugin struct {
	goplugin.Plugin
	Impl *Server
}

// GRPCServer registers Impl as the PluginService for this process.
func (p *GRPCPlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	kernelv1.RegisterPluginServiceServer(s, p.Impl)
	return nil
}

// GRPCClient is unused by echo itself; it exists so GRPCPlugin satisfies
// go-plugin's interface for a hypothetical caller that dispenses this type.
func (p *GRPCPlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return kernelv1.NewPluginServiceClient(c), nil
}
