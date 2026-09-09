package transport

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/protobuf/types/known/timestamppb"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

// ErrNoResponder carries the error string a responder set on its answer. When
// a Respond names a non-empty error, the matching Request fails with this
// wrapped around that string, rather than returning a reply — the one channel
// by which a server says "I could not answer this".
var ErrNoResponder = fmt.Errorf("transport: request failed at the responder")

// ErrUnknownRequest is returned by Respond for a request_id the transport holds
// no open correlation for: already answered, already timed out, or never
// minted here. It is a typed refusal, never a silent drop.
var ErrUnknownRequest = fmt.Errorf("transport: no open request for that request_id")

// reqReply is the kernel-held REQUEST_REPLY correlation state for one
// Transport. It carries the opaque payloads of one-question-one-answer traffic
// and never reads them. A question minted here (a Request) holds an open
// correlation keyed by a transport-minted request_id until its answer arrives
// (a Respond with the matching id) or its caller's context is done. A server
// (a Serve stream) drains the per-channel queue of questions waiting for it.
//
// All of this state is guarded by the one mu, the same single-lock shape the
// PUBSUB and POINT_TO_POINT paths keep. There is no goroutine and no lock per
// server or per open request; a server's stream goroutine and a requester's
// blocked call each wait on their own buffered wake/reply channel, signalled
// under mu.
type reqReply struct {
	mu sync.Mutex
	// byChannel holds the server set and the waiting-question queue for each
	// REQUEST_REPLY channel. A channel entry appears on the first Serve or the
	// first queued question and stays for the Transport's life.
	byChannel map[string]*rrChannel
	// open maps a minted request_id to the caller waiting for its answer. It is
	// Transport-wide, not per channel, because Respond names only a request_id.
	open map[string]*rrWaiter
}

// rrChannel is one REQUEST_REPLY channel's server set plus the queue of
// questions no server has taken yet. Both are guarded by reqReply.mu.
type rrChannel struct {
	servers []*rrServer
	queue   []*rrQuestion
}

// rrServer is one live Serve stream. wake is buffered so a signal under mu
// never blocks; the stream re-checks the queue on every wake, so no question
// is missed.
type rrServer struct {
	wake chan struct{}
}

// rrQuestion is one question waiting for, or handed to, a server: the minted
// request_id a server echoes back to Respond, and the opaque envelope.
type rrQuestion struct {
	requestID string
	env       *kernelv1.Envelope
}

// rrWaiter is the caller of Request, parked until its answer arrives. reply is
// buffered with room for one answer, so Respond never blocks and a Respond that
// wins a race against the caller's timeout still lands.
type rrWaiter struct {
	channel string
	reply   chan *rrAnswer
}

// rrAnswer is what a Respond hands back: an envelope on success, or a non-empty
// errMsg that fails the requester's call.
type rrAnswer struct {
	env    *kernelv1.Envelope
	errMsg string
}

func newReqReply() *reqReply {
	return &reqReply{
		byChannel: make(map[string]*rrChannel),
		open:      make(map[string]*rrWaiter),
	}
}

// Request asks one question on a REQUEST_REPLY channel and waits for one
// answer. It returns immediately with INTEREST_NONE when no server is attached
// to the channel — never a silent wait that only a timeout ends. Otherwise it
// mints a request_id, hands the stamped question to a server, and blocks until
// a Respond with that id arrives (INTEREST_PRESENT, the answer envelope) or ctx
// is done (ctx.Err(), typically a deadline the caller set from timeout_ms). The
// payload is opaque in both directions. A non-empty error from the responder
// fails the call with ErrNoResponder wrapped around that string.
func (t *Transport) Request(ctx context.Context, req *kernelv1.PublishRequest, producer string) (*kernelv1.RequestResponse, error) {
	if req.GetGroupKey() != "" {
		return nil, ErrGroupKeyNotImplemented
	}
	if len(req.GetPayload()) > MaxPayloadBytes {
		return nil, ErrPayloadTooLarge
	}
	if err := t.requireRequestReply(req.GetChannel()); err != nil {
		return nil, err
	}
	env := t.stampRequestEnvelope(req, producer)
	return t.rr.request(ctx, req.GetChannel(), env)
}

// Serve registers this caller as a server of a REQUEST_REPLY channel and
// delivers each incoming question to deliver as (envelope, request_id) until
// ctx is done or deliver returns an error. A question deliver reports an error
// on is requeued for another server before Serve returns, so a server shutting
// down strands no question. The server is unregistered on return.
func (t *Transport) Serve(ctx context.Context, pattern string, deliver func(env *kernelv1.Envelope, requestID string) error) error {
	if err := t.requireRequestReply(pattern); err != nil {
		return err
	}
	return t.rr.serve(ctx, pattern, deliver)
}

// Respond delivers an answer to the open request named by requestID. A
// non-empty errMsg fails the requester's call instead of answering it. An
// unknown or already-answered request_id is ErrUnknownRequest, a typed refusal.
// The answer payload is opaque.
func (t *Transport) Respond(requestID string, payload []byte, headers map[string]string, errMsg string) error {
	return t.rr.respond(requestID, payload, headers, errMsg, t.node)
}

// ServesChannel reports whether a live server is attached to channel. The
// in-process mesh double reads it to pick a node that can answer before it
// routes a blocking Request there; a bare transport caller does not need it.
func (t *Transport) ServesChannel(channel string) bool {
	t.rr.mu.Lock()
	defer t.rr.mu.Unlock()
	ch := t.rr.byChannel[channel]
	return ch != nil && len(ch.servers) > 0
}

// requireRequestReply confirms channel is declared and is a REQUEST_REPLY
// channel, the only type Request/Serve/Respond carry. A non-REQUEST_REPLY type
// is ErrChannelTypeNotImplemented, matching how Stamp/Inject refuse the types
// they do not carry.
func (t *Transport) requireRequestReply(channel string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	typ, declared := t.channels[channel]
	if !declared {
		return fmt.Errorf("%w: %q", ErrChannelNotDeclared, channel)
	}
	if typ != kernelv1.ChannelType_CHANNEL_TYPE_REQUEST_REPLY {
		return ErrChannelTypeNotImplemented
	}
	return nil
}

// stampRequestEnvelope builds the provenance-stamped envelope a question
// carries. A request/reply exchange is not a sequenced stream, so no origin_seq
// is advanced; every other provenance field is the transport's, overwriting any
// the caller set. The payload is copied by reference and never read.
func (t *Transport) stampRequestEnvelope(req *kernelv1.PublishRequest, producer string) *kernelv1.Envelope {
	return &kernelv1.Envelope{
		Channel:    req.GetChannel(),
		Payload:    req.GetPayload(),
		Headers:    req.GetHeaders(),
		OriginNode: t.node,
		OriginTime: timestamppb.Now(),
		MessageId:  newMessageID(),
		Producer:   producer,
	}
}

func (rr *reqReply) request(ctx context.Context, channel string, env *kernelv1.Envelope) (*kernelv1.RequestResponse, error) {
	rr.mu.Lock()
	ch := rr.byChannel[channel]
	if ch == nil || len(ch.servers) == 0 {
		rr.mu.Unlock()
		return &kernelv1.RequestResponse{Interest: kernelv1.Interest_INTEREST_NONE}, nil
	}
	requestID := newMessageID()
	w := &rrWaiter{channel: channel, reply: make(chan *rrAnswer, 1)}
	rr.open[requestID] = w
	ch.queue = append(ch.queue, &rrQuestion{requestID: requestID, env: env})
	ch.signalServersLocked()
	rr.mu.Unlock()

	select {
	case ans := <-w.reply:
		return answerResponse(ans)
	case <-ctx.Done():
		rr.mu.Lock()
		if _, stillOpen := rr.open[requestID]; stillOpen {
			// No Respond has claimed us: give up the correlation and drop the
			// question from the queue if a server has not taken it yet, so
			// nothing about this request_id lingers.
			delete(rr.open, requestID)
			ch.dropQuestionLocked(requestID)
			rr.mu.Unlock()
			return nil, ctx.Err()
		}
		// A Respond removed us from the open set a moment ago and is about to
		// send on our buffered reply channel: take the answer rather than lose
		// it to the race with our own deadline.
		rr.mu.Unlock()
		return answerResponse(<-w.reply)
	}
}

func (rr *reqReply) serve(ctx context.Context, channel string, deliver func(*kernelv1.Envelope, string) error) error {
	s := &rrServer{wake: make(chan struct{}, 1)}
	rr.mu.Lock()
	ch := rr.byChannel[channel]
	if ch == nil {
		ch = &rrChannel{}
		rr.byChannel[channel] = ch
	}
	ch.servers = append(ch.servers, s)
	rr.mu.Unlock()
	defer rr.removeServer(channel, s)

	for {
		rr.mu.Lock()
		var q *rrQuestion
		if len(ch.queue) > 0 {
			q = ch.queue[0]
			ch.queue = ch.queue[1:]
		}
		rr.mu.Unlock()

		if q != nil {
			if err := deliver(q.env, q.requestID); err != nil {
				rr.requeue(channel, q)
				return err
			}
			continue
		}

		select {
		case <-ctx.Done():
			return nil
		case <-s.wake:
		}
	}
}

func (rr *reqReply) respond(requestID string, payload []byte, headers map[string]string, errMsg, node string) error {
	rr.mu.Lock()
	w, ok := rr.open[requestID]
	if !ok {
		rr.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrUnknownRequest, requestID)
	}
	delete(rr.open, requestID)
	rr.mu.Unlock()

	var env *kernelv1.Envelope
	if errMsg == "" {
		env = &kernelv1.Envelope{
			Channel:    w.channel,
			Payload:    payload,
			Headers:    headers,
			OriginNode: node,
			OriginTime: timestamppb.Now(),
			MessageId:  newMessageID(),
		}
	}
	// Buffered with room for one answer, and the request_id is removed from the
	// open set above under the lock, so exactly one Respond reaches this send
	// and it never blocks — no dropped answer, no leaked sender.
	w.reply <- &rrAnswer{env: env, errMsg: errMsg}
	return nil
}

// requeue puts a question a failing server could not deliver back on its
// channel's queue and wakes the remaining servers. If none remain it waits
// there for the next Serve, or the requester's deadline ends it.
func (rr *reqReply) requeue(channel string, q *rrQuestion) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	ch := rr.byChannel[channel]
	if ch == nil {
		return
	}
	ch.queue = append(ch.queue, q)
	ch.signalServersLocked()
}

func (rr *reqReply) removeServer(channel string, s *rrServer) {
	rr.mu.Lock()
	defer rr.mu.Unlock()
	ch := rr.byChannel[channel]
	if ch == nil {
		return
	}
	for i, existing := range ch.servers {
		if existing == s {
			ch.servers = append(ch.servers[:i], ch.servers[i+1:]...)
			return
		}
	}
}

// signalServersLocked wakes every server of the channel. Each wake is buffered
// with room for one token, so the send never blocks and a server that is mid
// check collapses repeat signals into the one re-check it already does. Caller
// holds reqReply.mu.
func (ch *rrChannel) signalServersLocked() {
	for _, s := range ch.servers {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

// dropQuestionLocked removes a still-queued question by request_id, used when
// its caller's deadline ends before any server took it. A question already
// handed to a server is not in the queue and is left alone. Caller holds
// reqReply.mu.
func (ch *rrChannel) dropQuestionLocked(requestID string) {
	for i, q := range ch.queue {
		if q.requestID == requestID {
			ch.queue = append(ch.queue[:i], ch.queue[i+1:]...)
			return
		}
	}
}

func answerResponse(ans *rrAnswer) (*kernelv1.RequestResponse, error) {
	if ans.errMsg != "" {
		return nil, fmt.Errorf("%w: %s", ErrNoResponder, ans.errMsg)
	}
	return &kernelv1.RequestResponse{Envelope: ans.env, Interest: kernelv1.Interest_INTEREST_PRESENT}, nil
}
