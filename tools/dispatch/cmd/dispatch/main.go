// Command dispatch is the dispatch plugin's own binary: launched as a
// subprocess by the kernel's plugin host, it speaks kernelv1.PluginService
// over a unix socket and nothing else. The --role flag picks which half of the
// dispatcher this process runs; the kernel passes it through LaunchSpec.Args,
// and it persists across a reload so a reloaded worker stays a worker.
package main

import (
	"flag"
	"log"

	goplugin "github.com/hashicorp/go-plugin"

	"github.com/gregberns/harmonik/tools/dispatch"
)

func main() {
	roleArg := flag.String("role", "", "dispatch role: primary or worker")
	flag.Parse()

	role, err := dispatch.ParseRole(*roleArg)
	if err != nil {
		log.Fatalf("dispatch: %v", err)
	}

	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: dispatch.Handshake,
		Plugins: map[string]goplugin.Plugin{
			dispatch.PluginKey: &dispatch.GRPCPlugin{Impl: dispatch.NewServer(role)},
		},
		GRPCServer: goplugin.DefaultGRPCServer,
	})
}
