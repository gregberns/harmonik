// Command hoststub is the out-of-process leg of kernel/host's launch-pipeline
// tests. It serves PluginService over go-plugin's gRPC transport, describing
// one channel and recording every payload it is delivered by appending a
// hex-encoded line to a file the test reads back afterward — this binary
// never shares stdout with the test the way a plain subprocess helper would,
// because go-plugin's own handshake protocol already owns stdout.
//
// Every knob a test needs comes from an environment variable so the test can
// build this binary once and launch it multiple ways: which namespace and
// channel it describes, where it writes received payloads, and how long it
// sleeps inside Deliver before replying (used to hold a call open across a
// kill).
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"time"

	goplugin "github.com/hashicorp/go-plugin"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
	"github.com/gregberns/harmonik/kernel/host"
)

const (
	namespaceEnv        = "HARMONIK_KERNEL_HOSTSTUB_NAMESPACE"
	channelEnv          = "HARMONIK_KERNEL_HOSTSTUB_CHANNEL"
	receivedFileEnv     = "HARMONIK_KERNEL_HOSTSTUB_RECEIVED_FILE"
	deliverDelayMsEnv   = "HARMONIK_KERNEL_HOSTSTUB_DELIVER_DELAY_MS"
	defaultNamespace    = "hoststub"
	defaultChannelLocal = "in" // full channel name is namespace + "." + this, unless channelEnv overrides it whole
)

type server struct {
	kernelv1.UnimplementedPluginServiceServer

	namespace    string
	channel      string
	receivedFile string
	deliverDelay time.Duration
}

func (s *server) Describe(context.Context, *kernelv1.DescribeRequest) (*kernelv1.DescribeResponse, error) {
	return &kernelv1.DescribeResponse{
		Manifest: &kernelv1.PluginManifest{
			Namespace:  s.namespace,
			Version:    "0.0.0-test",
			ApiVersion: 1,
			Channels: []*kernelv1.ChannelDecl{
				{Name: s.channel, Type: kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB},
			},
		},
	}, nil
}

func (s *server) Start(context.Context, *kernelv1.StartRequest) (*kernelv1.StartResponse, error) {
	return &kernelv1.StartResponse{}, nil
}

func (s *server) Stop(context.Context, *kernelv1.StopRequest) (*kernelv1.StopResponse, error) {
	return &kernelv1.StopResponse{}, nil
}

func (s *server) Health(context.Context, *kernelv1.HealthRequest) (*kernelv1.HealthResponse, error) {
	return &kernelv1.HealthResponse{Ok: true}, nil
}

func (s *server) Deliver(ctx context.Context, req *kernelv1.DeliverRequest) (*kernelv1.DeliverResponse, error) {
	if s.deliverDelay > 0 {
		select {
		case <-time.After(s.deliverDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if s.receivedFile != "" {
		if err := s.appendReceived(req.GetEnvelope().GetPayload()); err != nil {
			return nil, err
		}
	}

	return &kernelv1.DeliverResponse{}, nil
}

// appendReceived records payload as one hex-encoded line, so the test that
// launched this binary can read back exactly what arrived and in what order.
func (s *server) appendReceived(payload []byte) (err error) {
	f, err := os.OpenFile(s.receivedFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("hoststub: open received file: %w", err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("hoststub: close received file: %w", closeErr)
		}
	}()

	if _, err = f.WriteString(hex.EncodeToString(payload) + "\n"); err != nil {
		return fmt.Errorf("hoststub: write received file: %w", err)
	}
	return nil
}

func main() {
	namespace := os.Getenv(namespaceEnv)
	if namespace == "" {
		namespace = defaultNamespace
	}
	channel := os.Getenv(channelEnv)
	if channel == "" {
		channel = namespace + "." + defaultChannelLocal
	}

	var deliverDelay time.Duration
	if raw := os.Getenv(deliverDelayMsEnv); raw != "" {
		ms, err := strconv.Atoi(raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "hoststub: bad %s: %v\n", deliverDelayMsEnv, err)
			os.Exit(1)
		}
		deliverDelay = time.Duration(ms) * time.Millisecond
	}

	impl := &server{
		namespace:    namespace,
		channel:      channel,
		receivedFile: os.Getenv(receivedFileEnv),
		deliverDelay: deliverDelay,
	}

	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: host.Handshake,
		Plugins: map[string]goplugin.Plugin{
			host.PluginServiceName: host.NewGRPCPlugin(impl),
		},
		GRPCServer: goplugin.DefaultGRPCServer,
	})
}
