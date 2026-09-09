package main

import (
	"context"
	"errors"
	"sync"
	"time"

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
	namespace string      // empty until the registered plugin's manifest is known
	roster    *rosterView // nil until the composition root sets the peer view; RosterList then falls back to self-only
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

// setRoster records the static peer view the composition root assembled, once
// the configured peer set is known. Called at most once during start-up,
// mirroring setNamespace. Until it is called RosterList answers self-only.
func (k *kernelServer) setRoster(rv *rosterView) {
	k.mu.Lock()
	k.roster = rv
	k.mu.Unlock()
}

// RosterList returns this box plus its configured peer set, each peer's liveness
// computed by the pure roster functions from the observation the composition
// root fed. With no peer view set (the single-node path), it answers self-only:
// this box, ALIVE, because it is the one answering.
func (k *kernelServer) RosterList(_ context.Context, _ *kernelv1.RosterListRequest) (*kernelv1.RosterListResponse, error) {
	k.mu.RLock()
	rv := k.roster
	k.mu.RUnlock()

	if rv == nil {
		return &kernelv1.RosterListResponse{
			Self: k.node,
			Nodes: []*kernelv1.NodeStatus{{
				Node:     &kernelv1.Node{Name: k.node},
				Liveness: &kernelv1.Liveness{State: kernelv1.Liveness_STATE_ALIVE},
			}},
		}, nil
	}
	return rv.list(), nil
}

// Request carries one REQUEST_REPLY question and returns its one answer. It
// returns INTEREST_NONE at once when no server is attached — never a silent
// wait that only the timeout ends. timeout_ms, when set, bounds the wait: the
// call fails DeadlineExceeded if no answer arrives in time. A non-empty error
// from the responder fails the call with Aborted. This is one of the three
// REQUEST_REPLY methods the embedded Unimplemented server no longer covers.
func (k *kernelServer) Request(ctx context.Context, req *kernelv1.RequestRequest) (*kernelv1.RequestResponse, error) {
	namespace, err := k.resolveNamespace()
	if err != nil {
		return nil, err
	}

	reqCtx := ctx
	if ms := req.GetTimeoutMs(); ms > 0 {
		var cancel context.CancelFunc
		reqCtx, cancel = context.WithTimeout(ctx, time.Duration(ms)*time.Millisecond)
		defer cancel()
	}

	pub := &kernelv1.PublishRequest{
		Channel: req.GetChannel(),
		Payload: req.GetPayload(),
		Headers: req.GetHeaders(),
	}
	resp, err := k.transport.Request(reqCtx, pub, namespace)
	if err != nil {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return nil, status.Error(codes.DeadlineExceeded, "harmonikd: request timed out with no response")
		case errors.Is(err, context.Canceled):
			return nil, status.Error(codes.Canceled, "harmonikd: request canceled")
		case errors.Is(err, transport.ErrNoResponder):
			return nil, status.Error(codes.Aborted, err.Error())
		default:
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
	}
	return resp, nil
}

// Serve streams incoming REQUEST_REPLY questions to this caller, each with the
// request_id it echoes back to Respond, until the stream's context ends. It
// delegates to the transport exactly as JournalRead does, one Send per item.
func (k *kernelServer) Serve(req *kernelv1.ServeRequest, stream kernelv1.KernelService_ServeServer) error {
	err := k.transport.Serve(stream.Context(), req.GetPattern(), func(env *kernelv1.Envelope, requestID string) error {
		return stream.Send(&kernelv1.ServeResponse{Envelope: env, RequestId: requestID})
	})
	if err != nil {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	return nil
}

// Respond delivers an answer to an open request by its request_id. A non-empty
// error fails the requester's call. An unknown or already-answered request_id
// is a typed NotFound refusal, never a silent drop.
func (k *kernelServer) Respond(_ context.Context, req *kernelv1.RespondRequest) (*kernelv1.RespondResponse, error) {
	err := k.transport.Respond(req.GetRequestId(), req.GetPayload(), req.GetHeaders(), req.GetError())
	if err != nil {
		if errors.Is(err, transport.ErrUnknownRequest) {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return &kernelv1.RespondResponse{}, nil
}

// LookupPut writes this node's claim on a key in a LOOKUP channel and returns
// the monotonic revision the node stamped. The writer is this node, not a value
// the caller supplies. ttl_seconds of zero never expires; otherwise the claim
// expires that many seconds ahead on the local clock. The clock is read here, at
// the composition root, and passed into the transport, which takes the instant
// as an argument — the injected-clock discipline the roster functions hold. This
// is one of the three LOOKUP methods the embedded Unimplemented server no longer
// covers.
func (k *kernelServer) LookupPut(_ context.Context, req *kernelv1.LookupPutRequest) (*kernelv1.LookupPutResponse, error) {
	ttl := time.Duration(req.GetTtlSeconds()) * time.Second
	revision, err := k.transport.LookupPut(req.GetChannel(), req.GetKey(), req.GetValue(), ttl, time.Now())
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return &kernelv1.LookupPutResponse{Revision: revision}, nil
}

// LookupGet returns every claimant of a key. With one kernel that is zero or one
// entry; the cross-node all-claimants merge (two nodes, one key, two entries)
// lives in the mesh double, which reads each node's map directly. An unclaimed
// or expired key is zero entries and no error, never a failure.
func (k *kernelServer) LookupGet(_ context.Context, req *kernelv1.LookupGetRequest) (*kernelv1.LookupGetResponse, error) {
	entries, err := k.transport.LookupGet(req.GetChannel(), req.GetKey(), time.Now())
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return &kernelv1.LookupGetResponse{Entries: entries}, nil
}

// LookupList returns this node's live claims whose key begins with key_prefix.
// An empty prefix lists every live claim on the channel. Expiry is honored on
// the local clock read here.
func (k *kernelServer) LookupList(_ context.Context, req *kernelv1.LookupListRequest) (*kernelv1.LookupListResponse, error) {
	entries, err := k.transport.LookupList(req.GetChannel(), req.GetKeyPrefix(), time.Now())
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	return &kernelv1.LookupListResponse{Entries: entries}, nil
}
