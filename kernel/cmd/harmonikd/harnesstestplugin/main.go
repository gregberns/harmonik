// Command harnesstestplugin is a minimal, real subprocess plugin used only
// by kernel/cmd/harmonikd's own tests: it declares one or more channels, dials
// the kernel endpoint it is handed at Start, and carries the channel-type
// behaviour its mode selects. Every knob is an environment variable so a test
// can build this binary once and launch it several ways. It exists so those
// tests exercise the same wire round trip a real launched plugin does, without
// kernel/ needing to name any real plugin.
//
// Four modes, one per channel type, all under the neutral harnesstestplugin.*
// names (no domain noun, so the kernel-vocabulary gate stays green):
//
//   - pubsub (default): declare one PUBSUB channel and an interest in it; on
//     Deliver, append the payload to a journal.
//   - point_to_point (also selected by the legacy GROUP knob): declare one
//     POINT_TO_POINT channel and a competing-consumer interest in a group; on
//     Deliver, append the payload to a journal.
//   - request_reply: declare two REQUEST_REPLY channels — one it SERVES (it
//     answers each question with a prefixed copy of the payload, which makes
//     the answer correlate to the question) and one it declares but never
//     serves (so a Request there fast-fails with INTEREST_NONE).
//   - lookup: declare one LOOKUP channel and claim a fixed key with this node's
//     name as the value, so two nodes claiming one key surface as two entries.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
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
	// modeEnv names the channel type this plugin declares and drives. It is the
	// one knob the four-type conformance harness sets; when unset, the GROUP knob
	// still selects point_to_point and a bare launch stays pubsub, so every
	// existing test keeps its behaviour.
	modeEnv = "HARMONIK_HARNESSTESTPLUGIN_MODE"
	// replyPrefixEnv is the byte prefix the request_reply server puts ahead of a
	// copy of each question's payload, so the answer correlates to the question.
	replyPrefixEnv = "HARMONIK_HARNESSTESTPLUGIN_REPLY_PREFIX"
	// lookupKeyEnv is the key the lookup mode claims. Each node claims it with
	// the node's own name as the value, so a two-node clash is two entries.
	lookupKeyEnv = "HARMONIK_HARNESSTESTPLUGIN_LOOKUP_KEY"

	defaultNamespace   = "harnesstestplugin"
	defaultJournal     = "records"
	defaultReplyPrefix = "reply:"
	defaultLookupKey   = "claimed-key"

	modePubsub       = "pubsub"
	modePointToPoint = "point_to_point"
	modeRequestReply = "request_reply"
	modeLookup       = "lookup"

	// retryBackoff paces the request_reply serve loop and the lookup claim loop
	// while they wait for the composition root to finish declaring the channel.
	// The kernel declares a plugin's channels just after its Start returns, so a
	// call these loops make during Start is briefly too early; a short retry
	// closes that window without a handshake.
	retryBackoff = 20 * time.Millisecond
)

type server struct {
	kernelv1.UnimplementedPluginServiceServer

	mode         string
	namespace    string
	channel      string // served/declared channel for pubsub, point_to_point, request_reply, lookup
	unserved     string // request_reply only: a declared-but-never-served channel (for INTEREST_NONE)
	journal      string
	group        string
	replyPrefix  []byte
	lookupKey    string
	rrServe      bool // request_reply only: does THIS node serve the channel?
	deliverDelay time.Duration
	deliverHang  bool

	mu     sync.RWMutex
	conn   *grpc.ClientConn
	kernel kernelv1.KernelServiceClient
	// bgStop cancels the serve / claim loop; only the cancel func is held, never
	// the context itself — a context belongs in a call, not a struct field.
	bgStop  context.CancelFunc
	bgGroup sync.WaitGroup
}

func (s *server) channelType() kernelv1.ChannelType {
	switch s.mode {
	case modePointToPoint:
		return kernelv1.ChannelType_CHANNEL_TYPE_POINT_TO_POINT
	case modeRequestReply:
		return kernelv1.ChannelType_CHANNEL_TYPE_REQUEST_REPLY
	case modeLookup:
		return kernelv1.ChannelType_CHANNEL_TYPE_LOOKUP
	default:
		return kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB
	}
}

func (s *server) Describe(context.Context, *kernelv1.DescribeRequest) (*kernelv1.DescribeResponse, error) {
	manifest := &kernelv1.PluginManifest{
		Namespace:  s.namespace,
		Version:    "0.0.0-harness",
		ApiVersion: 1,
	}

	switch s.mode {
	case modeRequestReply:
		// A REQUEST_REPLY plugin drives the channel itself through Serve/Respond,
		// so it declares the channels but holds no delivery interest. The second
		// channel is declared and never served: a Request there returns
		// INTEREST_NONE at once, the fast-fail the harness asserts.
		manifest.Channels = []*kernelv1.ChannelDecl{
			{Name: s.channel, Type: kernelv1.ChannelType_CHANNEL_TYPE_REQUEST_REPLY},
			{Name: s.unserved, Type: kernelv1.ChannelType_CHANNEL_TYPE_REQUEST_REPLY},
		}
	case modeLookup:
		// A LOOKUP plugin claims a key through LookupPut and holds no delivery
		// interest either.
		manifest.Channels = []*kernelv1.ChannelDecl{
			{Name: s.channel, Type: kernelv1.ChannelType_CHANNEL_TYPE_LOOKUP},
		}
	default:
		manifest.Channels = []*kernelv1.ChannelDecl{
			{Name: s.channel, Type: s.channelType()},
		}
		manifest.Interests = []*kernelv1.InterestDecl{
			{Kind: &kernelv1.InterestDecl_Channel{
				Channel: &kernelv1.ChannelInterest{Pattern: s.channel, Group: s.group},
			}},
		}
	}

	return &kernelv1.DescribeResponse{Manifest: manifest}, nil
}

// Start dials the kernel and, for the modes that drive a channel themselves,
// spawns the serve loop (request_reply) or the claim loop (lookup). Those loops
// outlive this Start call — they keep talking to the kernel until Stop — so they
// take a background context this plugin owns, cancelled in Stop, not the Start
// call's own context. That is the same deliberate choice the kernel host makes
// for a process it launches, hence the contextcheck waiver.
//
//nolint:contextcheck // the serve/claim loops outlive Start on purpose; their context is owned by this plugin and cancelled in Stop.
func (s *server) Start(_ context.Context, req *kernelv1.StartRequest) (*kernelv1.StartResponse, error) {
	conn, err := grpc.NewClient(req.GetKernelEndpoint(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("harnesstestplugin: dial %q: %w", req.GetKernelEndpoint(), err)
	}
	bgCtx, bgStop := context.WithCancel(context.Background())
	s.mu.Lock()
	s.conn, s.kernel = conn, kernelv1.NewKernelServiceClient(conn)
	s.bgStop = bgStop
	s.mu.Unlock()

	switch {
	case s.mode == modeRequestReply && s.rrServe:
		// Only a SERVER node runs the serve loop. A client node declares the
		// channel (so it is known mesh-wide) but attaches no server, so a Request
		// issued on it has no local answerer and must route cross-node to a node
		// that does serve — which is the cross-node leg this case proves.
		s.bgGroup.Add(1)
		go s.serveLoop(bgCtx, req.GetNode())
	case s.mode == modeLookup:
		s.bgGroup.Add(1)
		go s.claimLoop(bgCtx, req.GetNode())
	}

	return &kernelv1.StartResponse{}, nil
}

func (s *server) Stop(context.Context, *kernelv1.StopRequest) (*kernelv1.StopResponse, error) {
	s.mu.Lock()
	conn := s.conn
	stop := s.bgStop
	s.conn, s.kernel, s.bgStop = nil, nil, nil
	s.mu.Unlock()

	if stop != nil {
		stop()
	}
	s.bgGroup.Wait()

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

// serveLoop answers questions on the served REQUEST_REPLY channel until bgCtx is
// cancelled. Each answer is replyPrefix, then THIS node's name, then a copy of
// the question's payload — so the requester confirms both that the answer
// correlates to its question (the payload is carried back) and WHICH node
// served it (the name), which is how the harness proves the cross-node leg.
// Serve returns an error while the channel is not yet declared (the kernel
// declares it just after Start); the loop waits a short backoff and tries
// again, so the server attaches as soon as the declaration lands.
func (s *server) serveLoop(ctx context.Context, node string) {
	defer s.bgGroup.Done()
	for ctx.Err() == nil {
		s.mu.RLock()
		kernel := s.kernel
		s.mu.RUnlock()
		if kernel == nil {
			return
		}

		stream, err := kernel.Serve(ctx, &kernelv1.ServeRequest{Pattern: s.channel})
		if err != nil {
			sleepOrDone(ctx, retryBackoff)
			continue
		}
		for {
			resp, recvErr := stream.Recv()
			if recvErr != nil {
				break // channel not yet declared, or the stream ended; re-attach
			}
			question := resp.GetEnvelope().GetPayload()
			answer := make([]byte, 0, len(s.replyPrefix)+len(node)+1+len(question))
			answer = append(answer, s.replyPrefix...)
			answer = append(answer, node...)
			answer = append(answer, ':')
			answer = append(answer, question...)
			if _, respErr := kernel.Respond(ctx, &kernelv1.RespondRequest{
				RequestId: resp.GetRequestId(),
				Payload:   answer,
			}); respErr != nil && ctx.Err() != nil {
				return
			}
		}
		sleepOrDone(ctx, retryBackoff)
	}
}

// claimLoop claims the lookup key with this node's name as the value, retrying
// until the channel is declared (just after Start) or bgCtx is cancelled. One
// successful put is enough — this node is the sole writer of its own claim — so
// the loop returns as soon as it lands.
func (s *server) claimLoop(ctx context.Context, node string) {
	defer s.bgGroup.Done()
	for ctx.Err() == nil {
		s.mu.RLock()
		kernel := s.kernel
		s.mu.RUnlock()
		if kernel == nil {
			return
		}
		if _, err := kernel.LookupPut(ctx, &kernelv1.LookupPutRequest{
			Channel: s.channel,
			Key:     s.lookupKey,
			Value:   []byte(node),
		}); err == nil {
			return
		}
		sleepOrDone(ctx, retryBackoff)
	}
}

// sleepOrDone waits d, or returns early the moment ctx is cancelled.
func sleepOrDone(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// selectMode resolves the channel-type mode from its env knobs. An explicit
// MODE wins; with none set, a GROUP still selects point_to_point and a bare
// launch stays pubsub — so every launch that predates the MODE knob is
// unchanged.
func selectMode(group string) string {
	switch os.Getenv(modeEnv) {
	case modePubsub:
		return modePubsub
	case modePointToPoint:
		return modePointToPoint
	case modeRequestReply:
		return modeRequestReply
	case modeLookup:
		return modeLookup
	default:
		if group != "" {
			return modePointToPoint
		}
		return modePubsub
	}
}

func main() {
	// --rr-role is a per-node argument (it travels on the mesh config's
	// plugin.args, the same path the dispatch plugin's --role takes), so two
	// nodes running this one binary can differ: in request_reply mode the server
	// node serves the channel and the client node does not. A node differs by
	// ARGS, never by env — env is inherited process-wide, so it cannot tell one
	// node from another in a single mesh process.
	var rrRole string
	fs := flag.NewFlagSet("harnesstestplugin", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&rrRole, "rr-role", "server", "request_reply role: server (serves the channel) | client (does not)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		// A launch with no args, or an arg this build does not know, keeps the
		// default role rather than failing the process.
		rrRole = "server"
	}

	namespace := envOrDefault(namespaceEnv, defaultNamespace)
	channel := envOrDefault(channelEnv, namespace+".in")
	journal := envOrDefault(journalEnv, defaultJournal)
	group := os.Getenv(groupEnv)

	var deliverDelay time.Duration
	if ms, err := strconv.Atoi(os.Getenv(deliverDelayEnv)); err == nil && ms > 0 {
		deliverDelay = time.Duration(ms) * time.Millisecond
	}

	impl := &server{
		mode:         selectMode(group),
		namespace:    namespace,
		channel:      channel,
		unserved:     namespace + ".unserved",
		journal:      journal,
		group:        group,
		replyPrefix:  []byte(envOrDefault(replyPrefixEnv, defaultReplyPrefix)),
		lookupKey:    envOrDefault(lookupKeyEnv, defaultLookupKey),
		rrServe:      rrRole != "client",
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
