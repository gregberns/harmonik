package host

import (
	"context"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

// PluginServiceName is the key both sides dispense PluginService under.
const PluginServiceName = "plugin_service"

// Handshake is the magic-cookie pair this host and every plugin binary it
// launches must share. A binary that does not set these two environment
// variables before completing its own handshake is not a plugin this host
// launched — it is treated as a foreign process, never dispensed.
var Handshake = goplugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "HARMONIK_KERNEL_PLUGIN",
	MagicCookieValue: "v1",
}

// GRPCPlugin adapts a PluginServiceServer implementation to go-plugin's
// GRPCPlugin interface, so a plugin binary's own main can serve it with
// goplugin.Serve. This host process only ever dispenses the client side
// (GRPCClient); GRPCServer exists so the same type serves both directions of
// the same contract from one definition.
type GRPCPlugin struct {
	goplugin.Plugin
	Impl kernelv1.PluginServiceServer
}

// NewGRPCPlugin builds the plugin-side adapter around impl.
func NewGRPCPlugin(impl kernelv1.PluginServiceServer) *GRPCPlugin {
	return &GRPCPlugin{Impl: impl}
}

// GRPCServer registers Impl against s. Called inside the plugin process,
// never inside this host.
func (p *GRPCPlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	kernelv1.RegisterPluginServiceServer(s, p.Impl)
	return nil
}

// GRPCClient wraps c as a PluginServiceClient. Called inside this host after
// a successful handshake with the plugin process.
func (p *GRPCPlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return kernelv1.NewPluginServiceClient(c), nil
}

// pluginSet is what this host dispenses: one name, the whole PluginService.
var pluginSet = map[string]goplugin.Plugin{
	PluginServiceName: &GRPCPlugin{},
}
