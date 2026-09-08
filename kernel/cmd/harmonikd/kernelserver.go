package main

import (
	"context"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
	"github.com/gregberns/harmonik/kernel/state"
	"github.com/gregberns/harmonik/kernel/transport"
)

// kernelServer implements kernelv1.KernelServiceServer for one process with
// exactly one registered plugin (this slice's scope, per K7). Every call
// that touches state or transport identity uses the single namespace that
// plugin's own manifest declared — never a value a caller supplies — so a
// caller cannot name its way into a different namespace. A general registry
// keyed by connection identity, for more than one plugin per process, is
// later-slice work; embedding UnimplementedKernelServiceServer means the 15
// methods this slice does not carry traffic for already return a clean
// Unimplemented status instead of a silent no-op.
type kernelServer struct {
	kernelv1.UnimplementedKernelServiceServer

	node      string
	transport *transport.Transport
	journal   *state.State

	mu        sync.RWMutex
	namespace string // empty until the registered plugin's manifest is known
}

func newKernelServer(node string, t *transport.Transport, j *state.State) *kernelServer {
	return &kernelServer{node: node, transport: t, journal: j}
}

// setNamespace records the registered plugin's own namespace, once its
// manifest is known. Called exactly once during start-up, before the plugin
// process is told where to dial back to.
func (k *kernelServer) setNamespace(namespace string) {
	k.mu.Lock()
	k.namespace = namespace
	k.mu.Unlock()
}

func (k *kernelServer) resolveNamespace() (string, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	if k.namespace == "" {
		return "", status.Error(codes.Unavailable, "harmonikd: no plugin namespace registered yet")
	}
	return k.namespace, nil
}

func (k *kernelServer) Publish(_ context.Context, req *kernelv1.PublishRequest) (*kernelv1.PublishResponse, error) {
	namespace, err := k.resolveNamespace()
	if err != nil {
		return nil, err
	}
	resp, err := k.transport.Publish(req, namespace)
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return resp, nil
}

func (k *kernelServer) JournalAppend(ctx context.Context, req *kernelv1.JournalAppendRequest) (*kernelv1.JournalAppendResponse, error) {
	namespace, err := k.resolveNamespace()
	if err != nil {
		return nil, err
	}
	resp, err := k.journal.Append(ctx, namespace, req)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return resp, nil
}

func (k *kernelServer) JournalRead(req *kernelv1.JournalReadRequest, stream kernelv1.KernelService_JournalReadServer) error {
	namespace, err := k.resolveNamespace()
	if err != nil {
		return err
	}
	err = k.journal.Read(stream.Context(), namespace, req, func(rec *kernelv1.JournalRecord) error {
		return stream.Send(&kernelv1.JournalReadResponse{Records: []*kernelv1.JournalRecord{rec}})
	})
	if err != nil {
		return status.Error(codes.Internal, err.Error())
	}
	return nil
}

func (k *kernelServer) Info(_ context.Context, _ *kernelv1.InfoRequest) (*kernelv1.InfoResponse, error) {
	info := k.transport.Info()
	if namespace, err := k.resolveNamespace(); err == nil {
		info.Namespace = namespace
	}
	info.ApiVersion = 1
	info.KernelVersion = "harmonikd/0.1.0-vc12"
	return info, nil
}
