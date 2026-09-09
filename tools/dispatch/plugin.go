package dispatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

// WorkDelayEnv names the env var that makes a worker's Deliver handler pause
// before it records the job done. It is a test affordance — the mirror of the
// harnesstestplugin deliver-delay knob — so the Slice B chaos gate can land a
// kill -9 while a job is genuinely in flight (leased but not yet journaled),
// which is the only way to exercise the dead-worker requeue path
// deterministically. Unset or unparsable means no delay, so it costs a
// production launch nothing.
const WorkDelayEnv = "HARMONIK_DISPATCH_WORK_DELAY_MS"

// workDelay reads WorkDelayEnv once, at construction. A zero or unparsable
// value disables the pause.
func workDelay() time.Duration {
	if ms, err := strconv.Atoi(os.Getenv(WorkDelayEnv)); err == nil && ms > 0 {
		return time.Duration(ms) * time.Millisecond
	}
	return 0
}

// ErrNotStarted is returned when Deliver is called before Start has dialed the
// kernel back.
var ErrNotStarted = errors.New("dispatch: Deliver called before Start")

// Handshake is the go-plugin magic cookie both dispatch and its launcher must
// agree on before either side trusts the connection. The values MUST match
// kernel/host's Handshake; tool-isolation forbids importing kernel/host to
// reuse it, so the literal values are duplicated here, exactly as tools/echo
// does and for the same reason.
var Handshake = goplugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "HARMONIK_KERNEL_PLUGIN",
	MagicCookieValue: "v1",
}

// PluginKey names this plugin in a go-plugin ServeConfig/Plugins map.
const PluginKey = Namespace

// Server implements kernelv1.PluginServiceServer for one role. It holds no
// durable state: the kernel connection is re-read on every Start, and the
// per-role state (the primary's next id, the worker's dedupe set) is rebuilt
// from kernel journals on every Start, so a kill -9 loses nothing.
type Server struct {
	kernelv1.UnimplementedPluginServiceServer

	role Role

	mu     sync.Mutex
	conn   *grpc.ClientConn
	kernel kernelv1.KernelServiceClient

	nextID int             // primary: the next job number to stamp.
	done   map[string]bool // worker: job ids already completed (the dedupe set).

	workDelay time.Duration // worker: test-only pause before recording a job done (see WorkDelayEnv).
}

// NewServer builds a Server for a role with its in-memory state initialized.
func NewServer(role Role) *Server {
	return &Server{role: role, done: make(map[string]bool), workDelay: workDelay()}
}

// Describe returns the manifest for this server's role.
func (s *Server) Describe(context.Context, *kernelv1.DescribeRequest) (*kernelv1.DescribeResponse, error) {
	return &kernelv1.DescribeResponse{Manifest: Manifest(s.role)}, nil
}

// Start dials the kernel endpoint and rehydrates this role's state by journal
// replay before it reports ready, so the first Deliver after a restart already
// sees the recovered state.
func (s *Server) Start(ctx context.Context, req *kernelv1.StartRequest) (*kernelv1.StartResponse, error) {
	conn, err := grpc.NewClient(req.GetKernelEndpoint(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dispatch: dial kernel at %q: %w", req.GetKernelEndpoint(), err)
	}
	kernel := kernelv1.NewKernelServiceClient(conn)
	s.mu.Lock()
	s.conn, s.kernel = conn, kernel
	s.mu.Unlock()

	switch s.role {
	case RolePrimary:
		if err := s.rehydratePrimary(ctx, kernel); err != nil {
			return nil, err
		}
	case RoleWorker:
		if err := s.rehydrateWorker(ctx, kernel); err != nil {
			return nil, err
		}
	}
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
		return nil, fmt.Errorf("dispatch: close kernel connection: %w", err)
	}
	return &kernelv1.StopResponse{}, nil
}

// Health always reports ok: dispatch has no failure mode of its own between
// deliveries.
func (s *Server) Health(context.Context, *kernelv1.HealthRequest) (*kernelv1.HealthResponse, error) {
	return &kernelv1.HealthResponse{Ok: true}, nil
}

// Deliver routes an envelope to the role's handler: the primary's submit
// handler or the worker's work handler.
func (s *Server) Deliver(ctx context.Context, req *kernelv1.DeliverRequest) (*kernelv1.DeliverResponse, error) {
	s.mu.Lock()
	kernel, role := s.kernel, s.role
	s.mu.Unlock()
	if kernel == nil {
		return nil, ErrNotStarted
	}

	payload := req.GetEnvelope().GetPayload()
	var err error
	switch role {
	case RolePrimary:
		err = s.handleSubmit(ctx, kernel, payload)
	case RoleWorker:
		err = s.handleWork(ctx, kernel, payload)
	}
	if err != nil {
		return nil, err
	}
	return &kernelv1.DeliverResponse{}, nil
}

// handleSubmit is the primary's per-submission path: stamp a job id, record it
// accepted (durable), then forward it to the work channel and mark it
// forwarded. The order matters — accepted is durable before the forward, so a
// crash mid-forward leaves the job in accepted-minus-forwarded and the next
// Start re-forwards it.
func (s *Server) handleSubmit(ctx context.Context, kernel kernelv1.KernelServiceClient, payload []byte) error {
	// Hold the lock across the accepted append so the candidate id is committed
	// (nextID advanced) ONLY after the job is durably accepted. A failed append
	// must leave nextID untouched, so a retry reuses the same id instead of
	// burning it — a burned id is a gap, and a gap lets a later restart
	// re-stamp an id that already exists, which the worker dedupe would then
	// silently drop as a "duplicate" (a lost job, the slice's hard stop).
	s.mu.Lock()
	defer s.mu.Unlock()

	id := Namespace + "-" + strconv.Itoa(s.nextID)
	job := Job{ID: id, Body: payload}
	req, err := acceptedAppend(job)
	if err != nil {
		return err
	}
	if _, err := kernel.JournalAppend(ctx, req); err != nil {
		return fmt.Errorf("dispatch: journal accepted: %w", err)
	}
	s.nextID++ // committed: the id is now durably consumed.
	return s.forward(ctx, kernel, job)
}

// forward publishes a job on the work channel, then records the forwarded
// marker. Publishing before the marker means a crash between the two re-sends
// the job on the next Start — an extra copy the worker's dedupe absorbs —
// rather than dropping it.
func (s *Server) forward(ctx context.Context, kernel kernelv1.KernelServiceClient, job Job) error {
	body, err := marshalJob(job)
	if err != nil {
		return err
	}
	if _, err := kernel.Publish(ctx, &kernelv1.PublishRequest{Channel: WorkChannel, Payload: body}); err != nil {
		return fmt.Errorf("dispatch: publish work: %w", err)
	}
	if _, err := kernel.JournalAppend(ctx, forwardedAppend(job.ID)); err != nil {
		return fmt.Errorf("dispatch: journal forwarded: %w", err)
	}
	return nil
}

// handleWork is the worker's per-delivery path: dedupe on job id, do the work
// (record it done, durably), then report completion. A job id already in the
// done set is acked and dropped with a loud log — a deduped redelivery, the
// at-least-once tax, made visible rather than silently absorbed.
func (s *Server) handleWork(ctx context.Context, kernel kernelv1.KernelServiceClient, payload []byte) error {
	job, err := unmarshalJob(payload)
	if err != nil {
		return err
	}

	// Test-only pause (WorkDelayEnv): hold the job in flight — leased on the
	// declaring kernel, not yet journaled done — long enough for the chaos gate
	// to land a kill mid-job. A cancelled ctx (the drain gate, or shutdown)
	// abandons the work so the lease nacks back to the group, exactly as a crash
	// would. Production launches set no delay and skip this entirely.
	if s.workDelay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(s.workDelay):
		}
	}

	// Reserve the id before the durable write so a concurrent redelivery of the
	// same id sees it taken; release it if the write fails so a retry can win.
	s.mu.Lock()
	if s.done[job.ID] {
		s.mu.Unlock()
		log.Printf("dispatch: worker dropping duplicate job %q (already done)", job.ID)
		return nil
	}
	s.done[job.ID] = true
	s.mu.Unlock()

	req, err := doneAppend(job)
	if err != nil {
		s.release(job.ID)
		return err
	}
	if _, err := kernel.JournalAppend(ctx, req); err != nil {
		s.release(job.ID)
		return fmt.Errorf("dispatch: journal done: %w", err)
	}

	if _, err := kernel.Publish(ctx, &kernelv1.PublishRequest{Channel: StatusChannel, Payload: []byte(job.ID)}); err != nil {
		return fmt.Errorf("dispatch: publish status: %w", err)
	}
	return nil
}

// release un-reserves a job id after a failed durable write.
func (s *Server) release(id string) {
	s.mu.Lock()
	delete(s.done, id)
	s.mu.Unlock()
}

// rehydratePrimary rebuilds the primary's state from journals: re-forward
// every accepted job that has no forwarded marker, and set the next job number
// past the highest one already accepted.
func (s *Server) rehydratePrimary(ctx context.Context, kernel kernelv1.KernelServiceClient) error {
	acceptedRaw, err := readJournal(ctx, kernel, AcceptedJournal)
	if err != nil {
		return err
	}
	forwardedRaw, err := readJournal(ctx, kernel, ForwardedJournal)
	if err != nil {
		return err
	}
	forwarded := make(map[string]bool, len(forwardedRaw))
	for _, r := range forwardedRaw {
		forwarded[string(r)] = true
	}
	// The next id is one past the HIGHEST id already accepted, not the count:
	// a gap left by a failed append (a burned id) must never let the next id
	// collide with an existing one. Re-forward the accepted-minus-forwarded set
	// in the same pass.
	maxID := -1
	for _, r := range acceptedRaw {
		job, err := unmarshalJob(r)
		if err != nil {
			return err
		}
		if n, ok := jobIDSuffix(job.ID); ok && n > maxID {
			maxID = n
		}
		if forwarded[job.ID] {
			continue
		}
		if err := s.forward(ctx, kernel, job); err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.nextID = maxID + 1
	s.mu.Unlock()
	return nil
}

// jobIDSuffix parses the numeric suffix of a dispatch-stamped job id
// ("dispatch-<n>"). It reports ok=false for any id that does not fit the
// pattern, so a malformed record cannot silently reset the id counter.
func jobIDSuffix(id string) (int, bool) {
	const prefix = Namespace + "-"
	if !strings.HasPrefix(id, prefix) {
		return 0, false
	}
	n, err := strconv.Atoi(id[len(prefix):])
	if err != nil {
		return 0, false
	}
	return n, true
}

// rehydrateWorker rebuilds the worker's dedupe set from the done journal.
func (s *Server) rehydrateWorker(ctx context.Context, kernel kernelv1.KernelServiceClient) error {
	raw, err := readJournal(ctx, kernel, DoneJournal)
	if err != nil {
		return err
	}
	done := make(map[string]bool, len(raw))
	for _, r := range raw {
		job, err := unmarshalJob(r)
		if err != nil {
			return err
		}
		done[job.ID] = true
	}
	s.mu.Lock()
	s.done = done
	s.mu.Unlock()
	return nil
}

// readJournal drains a journal from the start into a slice of records. It is a
// one-shot read (follow=false): the stream ends when the journal is exhausted.
func readJournal(ctx context.Context, kernel kernelv1.KernelServiceClient, journal string) ([][]byte, error) {
	stream, err := kernel.JournalRead(ctx, &kernelv1.JournalReadRequest{Journal: journal})
	if err != nil {
		return nil, fmt.Errorf("dispatch: read journal %q: %w", journal, err)
	}
	var out [][]byte
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("dispatch: read journal %q: %w", journal, err)
		}
		for _, rec := range resp.GetRecords() {
			out = append(out, rec.GetRecord())
		}
	}
	return out, nil
}

// GRPCPlugin is the go-plugin adapter: it registers Server as the PluginService
// a launched dispatch process serves. GRPCClient exists only to satisfy
// go-plugin's GRPCPlugin interface; dispatch is a plugin, never a host.
type GRPCPlugin struct {
	goplugin.Plugin
	Impl *Server
}

// GRPCServer registers Impl as the PluginService for this process.
func (p *GRPCPlugin) GRPCServer(_ *goplugin.GRPCBroker, s *grpc.Server) error {
	kernelv1.RegisterPluginServiceServer(s, p.Impl)
	return nil
}

// GRPCClient is unused by dispatch itself; it exists so GRPCPlugin satisfies
// go-plugin's interface for a hypothetical caller that dispenses this type.
func (p *GRPCPlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return kernelv1.NewPluginServiceClient(c), nil
}
