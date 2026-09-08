// Command echo is the echo plugin's own binary: launched as a subprocess by
// the kernel's plugin host, it speaks kernelv1.PluginService over a unix
// socket and nothing else.
package main

import (
	"github.com/gregberns/harmonik/tools/echo"
	goplugin "github.com/hashicorp/go-plugin"
)

func main() {
	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: echo.Handshake,
		Plugins: map[string]goplugin.Plugin{
			echo.PluginKey: &echo.GRPCPlugin{Impl: &echo.Server{}},
		},
		GRPCServer: goplugin.DefaultGRPCServer,
	})
}
