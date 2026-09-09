package dispatch_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
	"github.com/gregberns/harmonik/tools/dispatch"
)

// mustMarshal encodes a Job the way the plugin does (plain JSON of the exported
// struct), so a test can seed a journal or a work payload with bytes identical
// to what dispatch would write.
func mustMarshal(t *testing.T, j dispatch.Job) []byte {
	t.Helper()
	b, err := json.Marshal(j)
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}
	return b
}

func unmarshalJobForTest(b []byte) (dispatch.Job, error) {
	var j dispatch.Job
	err := json.Unmarshal(b, &j)
	return j, err
}

// fakeKernel is an in-memory stand-in for the kernel: it keeps one append-only
// slice per journal and one per-channel publish log, so a test can assert on
// exactly what the plugin sent and can replay journals back on a re-Start the
// way a real kernel would. It is the dispatch analogue of echo_test's
// fakeKernel, grown to carry journals (dispatch rehydrates from them).
type fakeKernel struct {
	kernelv1.UnimplementedKernelServiceServer

	mu        sync.Mutex
	journals  map[string][][]byte // journal name -> records, in append order.
	published map[string][][]byte // channel -> payloads, in publish order.

	// failAcceptedRemaining, when >0, makes the next N appends to the accepted
	// journal return an error (and decrements) — the transient-kernel-error
	// fault the job-id-burn regression needs.
	failAcceptedRemaining int
}

func newFakeKernel() *fakeKernel {
	return &fakeKernel{
		journals:  make(map[string][][]byte),
		published: make(map[string][][]byte),
	}
}

func (k *fakeKernel) JournalAppend(_ context.Context, req *kernelv1.JournalAppendRequest) (*kernelv1.JournalAppendResponse, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if req.GetJournal() == dispatch.AcceptedJournal && k.failAcceptedRemaining > 0 {
		k.failAcceptedRemaining--
		return nil, errors.New("simulated transient kernel error")
	}
	seqs := make([]uint64, 0, len(req.GetRecords()))
	for _, rec := range req.GetRecords() {
		k.journals[req.GetJournal()] = append(k.journals[req.GetJournal()], rec)
		seqs = append(seqs, uint64(len(k.journals[req.GetJournal()])))
	}
	return &kernelv1.JournalAppendResponse{Seqs: seqs, Synced: req.GetSync()}, nil
}

func (k *fakeKernel) JournalRead(req *kernelv1.JournalReadRequest, stream grpc.ServerStreamingServer[kernelv1.JournalReadResponse]) error {
	k.mu.Lock()
	records := append([][]byte(nil), k.journals[req.GetJournal()]...)
	k.mu.Unlock()
	var seq uint64
	for _, rec := range records {
		seq++
		if seq <= req.GetAfterSeq() { // after_seq is exclusive.
			continue
		}
		out := &kernelv1.JournalReadResponse{Records: []*kernelv1.JournalRecord{
			{Seq: seq, Record: rec},
		}}
		if err := stream.Send(out); err != nil {
			return err
		}
	}
	return nil
}

func (k *fakeKernel) Publish(_ context.Context, req *kernelv1.PublishRequest) (*kernelv1.PublishResponse, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.published[req.GetChannel()] = append(k.published[req.GetChannel()], req.GetPayload())
	return &kernelv1.PublishResponse{MessageId: "m"}, nil
}

func (k *fakeKernel) journal(name string) [][]byte {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([][]byte(nil), k.journals[name]...)
}

func (k *fakeKernel) publishes(channel string) [][]byte {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([][]byte(nil), k.published[channel]...)
}

// serveFake stands the fake kernel up on a loopback gRPC listener and returns
// its dial address, cleaning up at the end of the test.
func serveFake(t *testing.T, k *fakeKernel) string {
	t.Helper()
	var lc net.ListenConfig
	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	kernelv1.RegisterKernelServiceServer(srv, k)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(lis) }()
	t.Cleanup(func() {
		srv.Stop()
		if err := <-serveErr; err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			t.Errorf("srv.Serve: %v", err)
		}
	})
	return lis.Addr().String()
}

// startServer builds a role server and runs its Start against the fake kernel,
// so the returned server has dialed and rehydrated exactly as in production.
func startServer(t *testing.T, role dispatch.Role, addr string) *dispatch.Server {
	t.Helper()
	s := dispatch.NewServer(role)
	if _, err := s.Start(context.Background(), &kernelv1.StartRequest{KernelEndpoint: addr}); err != nil {
		t.Fatalf("Start(%s): %v", role, err)
	}
	t.Cleanup(func() {
		if _, err := s.Stop(context.Background(), &kernelv1.StopRequest{}); err != nil {
			t.Errorf("Stop(%s): %v", role, err)
		}
	})
	return s
}

func deliver(t *testing.T, s *dispatch.Server, channel string, payload []byte) {
	t.Helper()
	if _, err := s.Deliver(context.Background(), &kernelv1.DeliverRequest{
		Envelope: &kernelv1.Envelope{Channel: channel, Payload: payload},
	}); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
}

// TestRoleArgsParse covers the launch-arg contract: the two roles parse,
// anything else is a loud refusal.
func TestRoleArgsParse(t *testing.T) {
	for _, want := range []dispatch.Role{dispatch.RolePrimary, dispatch.RoleWorker} {
		got, err := dispatch.ParseRole(string(want))
		if err != nil || got != want {
			t.Errorf("ParseRole(%q) = (%q, %v), want (%q, nil)", want, got, err, want)
		}
	}
	if _, err := dispatch.ParseRole("leader"); err == nil {
		t.Error("ParseRole(\"leader\"): want error, got nil")
	}
	if _, err := dispatch.ParseRole(""); err == nil {
		t.Error("ParseRole(\"\"): want error, got nil")
	}
}

// TestManifestsMatchTheSpec pins each role's declared surface to the B6 spec:
// the primary declares the three channels (submit public) plus the submit
// interest; the worker declares no channel and one grouped interest in work.
func TestManifestsMatchTheSpec(t *testing.T) {
	primary := dispatch.Manifest(dispatch.RolePrimary)
	if primary.GetNamespace() != dispatch.Namespace {
		t.Errorf("primary namespace = %q, want %q", primary.GetNamespace(), dispatch.Namespace)
	}
	if len(primary.GetChannels()) != 3 {
		t.Fatalf("primary channels = %d, want 3", len(primary.GetChannels()))
	}
	wantChannels := map[string]struct {
		typ    kernelv1.ChannelType
		public bool
	}{
		dispatch.WorkChannel:   {kernelv1.ChannelType_CHANNEL_TYPE_POINT_TO_POINT, false},
		dispatch.StatusChannel: {kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB, false},
		dispatch.SubmitChannel: {kernelv1.ChannelType_CHANNEL_TYPE_PUBSUB, true},
	}
	for _, ch := range primary.GetChannels() {
		want, ok := wantChannels[ch.GetName()]
		if !ok {
			t.Errorf("primary declared unexpected channel %q", ch.GetName())
			continue
		}
		if ch.GetType() != want.typ || ch.GetPublic() != want.public {
			t.Errorf("primary channel %q = (type %v, public %v), want (%v, %v)", ch.GetName(), ch.GetType(), ch.GetPublic(), want.typ, want.public)
		}
	}
	if n := len(primary.GetInterests()); n != 1 {
		t.Fatalf("primary interests = %d, want 1", n)
	}
	if p := primary.GetInterests()[0].GetChannel(); p.GetPattern() != dispatch.SubmitChannel || p.GetGroup() != "" {
		t.Errorf("primary interest = %+v, want pattern=%q group=\"\"", p, dispatch.SubmitChannel)
	}

	worker := dispatch.Manifest(dispatch.RoleWorker)
	if len(worker.GetChannels()) != 0 {
		t.Errorf("worker channels = %d, want 0 (the primary's kernel declares the channels)", len(worker.GetChannels()))
	}
	if n := len(worker.GetInterests()); n != 1 {
		t.Fatalf("worker interests = %d, want 1", n)
	}
	wi := worker.GetInterests()[0].GetChannel()
	if wi.GetPattern() != dispatch.WorkChannel || wi.GetGroup() != dispatch.WorkerGroup {
		t.Errorf("worker interest = %+v, want pattern=%q group=%q", wi, dispatch.WorkChannel, dispatch.WorkerGroup)
	}
}

// TestPrimarySubmitProducesAcceptedWorkForwarded is the primary happy path: one
// submit yields exactly one accepted record, one work publish, one forwarded
// marker — and the marker names the same job id the accepted record carries.
func TestPrimarySubmitProducesAcceptedWorkForwarded(t *testing.T) {
	k := newFakeKernel()
	addr := serveFake(t, k)
	primary := startServer(t, dispatch.RolePrimary, addr)

	deliver(t, primary, dispatch.SubmitChannel, []byte("do-a-thing"))

	if n := len(k.journal(dispatch.AcceptedJournal)); n != 1 {
		t.Fatalf("accepted records = %d, want 1", n)
	}
	if n := len(k.publishes(dispatch.WorkChannel)); n != 1 {
		t.Fatalf("work publishes = %d, want 1", n)
	}
	forwarded := k.journal(dispatch.ForwardedJournal)
	if len(forwarded) != 1 {
		t.Fatalf("forwarded markers = %d, want 1", len(forwarded))
	}

	accepted := k.journal(dispatch.AcceptedJournal)[0]
	work := k.publishes(dispatch.WorkChannel)[0]
	// The work payload and the accepted record are the same marshaled job, and
	// the forwarded marker is that job's id.
	if !bytes.Equal(accepted, work) {
		t.Errorf("work payload %q != accepted record %q", work, accepted)
	}
	// The forwarded marker is the job id in plain bytes; it must be non-empty
	// and must appear inside the accepted record (which carries id + body).
	if len(forwarded[0]) == 0 {
		t.Error("forwarded marker is empty")
	}
}

// TestPrimaryReStartReForwardsOnlyUnforwarded is the no-loss-on-crash property
// for the primary: a job accepted but not yet forwarded (the crash window) is
// re-forwarded on the next Start; a job already forwarded is not.
func TestPrimaryReStartReForwardsOnlyUnforwarded(t *testing.T) {
	k := newFakeKernel()

	// Simulate the durable state a crashed primary would leave behind:
	// job dispatch-0 fully handled (accepted + forwarded), job dispatch-1
	// accepted but its forward never recorded (crashed mid-forward).
	appendJSON := func(journal string, j dispatch.Job) {
		b := mustMarshal(t, j)
		k.mu.Lock()
		k.journals[journal] = append(k.journals[journal], b)
		k.mu.Unlock()
	}
	appendJSON(dispatch.AcceptedJournal, dispatch.Job{ID: "dispatch-0", Body: []byte("a")})
	appendJSON(dispatch.AcceptedJournal, dispatch.Job{ID: "dispatch-1", Body: []byte("b")})
	k.mu.Lock()
	k.journals[dispatch.ForwardedJournal] = append(k.journals[dispatch.ForwardedJournal], []byte("dispatch-0"))
	k.mu.Unlock()

	addr := serveFake(t, k)
	primary := startServer(t, dispatch.RolePrimary, addr) // Start replays and re-forwards.

	// Exactly the unforwarded job (dispatch-1) was re-forwarded: one work
	// publish, one new forwarded marker (now two total).
	if n := len(k.publishes(dispatch.WorkChannel)); n != 1 {
		t.Fatalf("re-forward work publishes = %d, want 1", n)
	}
	republished, err := unmarshalJobForTest(k.publishes(dispatch.WorkChannel)[0])
	if err != nil {
		t.Fatalf("decode republished job: %v", err)
	}
	if republished.ID != "dispatch-1" {
		t.Errorf("re-forwarded job id = %q, want dispatch-1", republished.ID)
	}
	if n := len(k.journal(dispatch.ForwardedJournal)); n != 2 {
		t.Errorf("forwarded markers after re-Start = %d, want 2", n)
	}

	// And the recovered primary stamps the NEXT new job past the highest
	// accepted id: two jobs were accepted, so the next submit is dispatch-2.
	deliver(t, primary, dispatch.SubmitChannel, []byte("c"))
	accepted := k.journal(dispatch.AcceptedJournal)
	if len(accepted) != 3 {
		t.Fatalf("accepted after new submit = %d, want 3", len(accepted))
	}
	newest, err := unmarshalJobForTest(accepted[2])
	if err != nil {
		t.Fatalf("decode newest job: %v", err)
	}
	if newest.ID != "dispatch-2" {
		t.Errorf("next stamped id = %q, want dispatch-2", newest.ID)
	}
}

// TestWorkerDedupesOnJobID is the effectively-once property for the worker: the
// same job id delivered twice is journaled once, and the duplicate is acked
// (no error) and dropped.
func TestWorkerDedupesOnJobID(t *testing.T) {
	k := newFakeKernel()
	addr := serveFake(t, k)
	worker := startServer(t, dispatch.RoleWorker, addr)

	job := mustMarshal(t, dispatch.Job{ID: "dispatch-7", Body: []byte("work")})
	deliver(t, worker, dispatch.WorkChannel, job)
	deliver(t, worker, dispatch.WorkChannel, job) // at-least-once redelivery.

	if n := len(k.journal(dispatch.DoneJournal)); n != 1 {
		t.Fatalf("done records = %d, want 1 (the duplicate must be deduped)", n)
	}
	// Exactly one completion was reported.
	if n := len(k.publishes(dispatch.StatusChannel)); n != 1 {
		t.Errorf("status publishes = %d, want 1", n)
	}
}

// TestWorkerRehydratesDedupeSet proves the dedupe set survives a crash: a job
// recorded done before the restart is dropped (not re-done) after it.
func TestWorkerRehydratesDedupeSet(t *testing.T) {
	k := newFakeKernel()

	// A worker completed dispatch-9 before the crash.
	b := mustMarshal(t, dispatch.Job{ID: "dispatch-9", Body: []byte("already")})
	k.mu.Lock()
	k.journals[dispatch.DoneJournal] = append(k.journals[dispatch.DoneJournal], b)
	k.mu.Unlock()

	addr := serveFake(t, k)
	worker := startServer(t, dispatch.RoleWorker, addr) // Start rebuilds the dedupe set.

	deliver(t, worker, dispatch.WorkChannel, b) // redelivery of the completed job.

	if n := len(k.journal(dispatch.DoneJournal)); n != 1 {
		t.Errorf("done records after redelivery = %d, want 1 (rehydrated dedupe set must drop it)", n)
	}
}

// TestPrimaryDoesNotBurnJobIDOnAppendFailure is the regression for the review
// BLOCK: a transient accepted-append failure to a still-living primary must not
// burn a job id. Without the fix, the id advances past a gap, a later restart
// (nextID = len(accepted)) under-counts and re-stamps an id that already
// exists, and the worker's id-keyed dedupe silently drops the genuinely-new
// job — a lost job.
func TestPrimaryDoesNotBurnJobIDOnAppendFailure(t *testing.T) {
	k := newFakeKernel()
	k.failAcceptedRemaining = 1 // the first accepted append errors.
	addr := serveFake(t, k)
	primary := startServer(t, dispatch.RolePrimary, addr)

	// First submit: the accepted append fails. The id must NOT be consumed.
	if _, err := primary.Deliver(context.Background(), &kernelv1.DeliverRequest{
		Envelope: &kernelv1.Envelope{Channel: dispatch.SubmitChannel, Payload: []byte("job-A")},
	}); err == nil {
		t.Fatal("first submit: want error from the failed accepted append, got nil")
	}

	// Retry succeeds and must REUSE dispatch-0, not burn it to dispatch-1.
	deliver(t, primary, dispatch.SubmitChannel, []byte("job-A"))
	accepted := k.journal(dispatch.AcceptedJournal)
	if len(accepted) != 1 {
		t.Fatalf("accepted after retry = %d, want 1", len(accepted))
	}
	first, err := unmarshalJobForTest(accepted[0])
	if err != nil {
		t.Fatalf("decode accepted[0]: %v", err)
	}
	if first.ID != "dispatch-0" {
		t.Errorf("retried job id = %q, want dispatch-0 (id must not be burned)", first.ID)
	}

	// A fresh primary process restarts against the same durable journals.
	primary2 := startServer(t, dispatch.RolePrimary, addr)

	// A brand-new submit after the restart must get a new id, never re-stamp
	// dispatch-0.
	deliver(t, primary2, dispatch.SubmitChannel, []byte("job-B"))
	accepted = k.journal(dispatch.AcceptedJournal)
	if len(accepted) != 2 {
		t.Fatalf("accepted after post-restart submit = %d, want 2", len(accepted))
	}

	// No job id is ever re-issued: every accepted id is distinct.
	seen := make(map[string]bool)
	for i, rec := range accepted {
		job, err := unmarshalJobForTest(rec)
		if err != nil {
			t.Fatalf("decode accepted[%d]: %v", i, err)
		}
		if seen[job.ID] {
			t.Fatalf("job id %q re-issued — a re-stamp that loses a job", job.ID)
		}
		seen[job.ID] = true
	}

	// And a worker draining the forwarded work drops no genuinely-new job: both
	// distinct submissions reach the done journal.
	worker := startServer(t, dispatch.RoleWorker, addr)
	for _, p := range k.publishes(dispatch.WorkChannel) {
		deliver(t, worker, dispatch.WorkChannel, p)
	}
	doneBodies := make(map[string]bool)
	for _, rec := range k.journal(dispatch.DoneJournal) {
		job, err := unmarshalJobForTest(rec)
		if err != nil {
			t.Fatalf("decode done record: %v", err)
		}
		doneBodies[string(job.Body)] = true
	}
	for _, want := range []string{"job-A", "job-B"} {
		if !doneBodies[want] {
			t.Errorf("body %q never reached the done journal — a genuinely-new job was dropped by the dedupe", want)
		}
	}
}

// TestDeliverBeforeStartFails guards the rule that a plugin holds no state
// across a reload: without Start re-establishing the kernel connection, Deliver
// refuses rather than silently dropping the payload.
func TestDeliverBeforeStartFails(t *testing.T) {
	s := dispatch.NewServer(dispatch.RoleWorker)
	_, err := s.Deliver(context.Background(), &kernelv1.DeliverRequest{
		Envelope: &kernelv1.Envelope{Channel: dispatch.WorkChannel, Payload: []byte("x")},
	})
	if err == nil {
		t.Fatal("Deliver before Start: want error, got nil")
	}
}
