// Package host is the kernel's plugin host: the subprocess launch state
// machine that gets a plugin from a discovered binary on disk to a live,
// dispatchable PluginService connection, and detects when that connection
// is gone.
//
// The pipeline is linear and moves in one direction only:
//
//	DISCOVERED -> VERIFIED -> PREWARMED -> LAUNCHING -> HANDSHAKING ->
//	DESCRIBING -> REGISTERED -> STARTING -> RUNNING
//
// VERIFIED compares the binary's own sha256 against the launch spec and
// refuses to execute anything on a mismatch. PREWARMED execs the binary once
// and discards it before the real launch: on this darwin box, the OS code-
// signature check on a binary's first exec costs on the order of 500ms, and
// paying that cost here — at registration, not on a reload — is what keeps a
// later reload single-digit milliseconds. DESCRIBING validates that a
// plugin's manifest only claims channels inside its own namespace. Once
// RUNNING, the kernel dispatches deliveries through Deliver; a dead child
// surfaces as Unavailable. There is no crash-loop budget or restart policy in
// this slice — detection only.
package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	hclog "github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
)

// State names one step of the launch pipeline.
type State int

// The pipeline states, in the order Launch moves through them. StateRunning
// is the only one Launch returns a *Host in; StateUnavailable is the only
// one Launch never reaches — Deliver moves a Host there after RUNNING.
const (
	StateDiscovered State = iota
	StateVerified
	StatePrewarmed
	StateLaunching
	StateHandshaking
	StateDescribing
	StateRegistered
	StateStarting
	StateRunning
	StateUnavailable
)

func (s State) String() string {
	switch s {
	case StateDiscovered:
		return "DISCOVERED"
	case StateVerified:
		return "VERIFIED"
	case StatePrewarmed:
		return "PREWARMED"
	case StateLaunching:
		return "LAUNCHING"
	case StateHandshaking:
		return "HANDSHAKING"
	case StateDescribing:
		return "DESCRIBING"
	case StateRegistered:
		return "REGISTERED"
	case StateStarting:
		return "STARTING"
	case StateRunning:
		return "RUNNING"
	case StateUnavailable:
		return "UNAVAILABLE"
	default:
		return fmt.Sprintf("host.State(%d)", int(s))
	}
}

// Errors returned along the pipeline. Each names the step that refused.
var (
	// ErrChecksumMismatch is VERIFIED's refusal: the binary on disk does not
	// hash to what the launch spec promised, so it is never executed.
	ErrChecksumMismatch = errors.New("host: launch spec sha256 does not match the binary on disk")
	// ErrEmptyNamespace is DESCRIBING's refusal of a manifest with no identity.
	ErrEmptyNamespace = errors.New("host: plugin manifest declares an empty namespace")
	// ErrNamespaceViolation is DESCRIBING's refusal of a channel a plugin
	// declares outside the namespace it owns.
	ErrNamespaceViolation = errors.New("host: plugin manifest declares a channel outside its own namespace")
	// ErrUndeclaredChannel is Deliver's refusal of an envelope for a channel
	// the manifest never declared or expressed interest in.
	ErrUndeclaredChannel = errors.New("host: delivery targets a channel the plugin never declared or expressed interest in")
	// ErrUnavailable wraps a Deliver failure caused by a dead child process.
	ErrUnavailable = errors.New("host: plugin process is unavailable")
)

// LaunchSpec discovers one plugin binary. Path names the binary; SHA256 is
// the hex-encoded digest the caller expects it to have — VERIFIED refuses to
// launch anything that does not match. Node, KernelEndpoint and CallerID are
// forwarded to the plugin's Start call unchanged. Args are the command-line
// arguments handed to the child process at exec, unchanged: a plugin whose
// behaviour is chosen at launch (the dispatch plugin's --role, say) reads them
// there. They are part of the spec, so a reload relaunches the same binary with
// the same arguments — a reloaded worker stays a worker.
type LaunchSpec struct {
	Path           string
	SHA256         string
	Node           string
	KernelEndpoint string
	CallerID       string
	APIVersion     uint32
	Args           []string
}

// Host is one launched plugin process, from RUNNING to whatever state a
// crash leaves it in. The zero value is not usable; build one with Launch.
type Host struct {
	mu    sync.Mutex
	state State

	client   *goplugin.Client
	service  kernelv1.PluginServiceClient
	manifest *kernelv1.PluginManifest

	pid             int
	prewarmDuration time.Duration
}

// LaunchOption tunes a Launch. The zero set of options is the plain launch;
// each option names one thing the composition root needs to happen at a precise
// point in the pipeline.
type LaunchOption func(*launchOptions)

type launchOptions struct {
	onManifest func(*kernelv1.PluginManifest) error
}

// OnManifest registers a hook the launch calls once the plugin's manifest is
// known and validated (after DESCRIBING), before the plugin's own Start runs.
// It exists for one load-bearing reason: a plugin whose Start replays kernel
// journals (the dispatch plugin's rehydration) calls back into the kernel
// DURING Start, and the kernel resolves those calls against the plugin's
// namespace — which the kernel learns only from this manifest. The hook is the
// one point where the composition root can register that namespace before the
// plugin's Start-time calls arrive. A hook error aborts the launch.
func OnManifest(fn func(*kernelv1.PluginManifest) error) LaunchOption {
	return func(o *launchOptions) { o.onManifest = fn }
}

// Launch drives spec through the full pipeline to RUNNING. It returns after
// the first failing step; no *Host comes back unless the process reached
// RUNNING, and any process this call started before failing is killed
// before it returns.
func Launch(ctx context.Context, spec LaunchSpec, opts ...LaunchOption) (*Host, error) {
	var o launchOptions
	for _, opt := range opts {
		opt(&o)
	}

	if err := verify(spec); err != nil {
		return nil, err
	}

	prewarmDuration, err := prewarm(ctx, spec.Path)
	if err != nil {
		return nil, fmt.Errorf("host: prewarm: %w", err)
	}

	// The plugin process must outlive the ctx this call happens to be made
	// with — its lifetime is controlled by (*Host).Kill, not by a caller's
	// per-call deadline — so this exec is tied to context.Background(), not
	// to ctx.
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  Handshake,
		Plugins:          pluginSet,
		Cmd:              exec.CommandContext(context.Background(), spec.Path, spec.Args...), //nolint:gosec,contextcheck // spec.Path was sha256-verified above; context.Background() is deliberate, see the comment above
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Logger:           hclog.NewNullLogger(),
	})

	host, err := launchWith(ctx, client, spec, prewarmDuration, o)
	if err != nil {
		client.Kill()
		return nil, err
	}
	return host, nil
}

// launchWith carries client through HANDSHAKING, DESCRIBING and STARTING.
// The caller kills client on any error this returns.
func launchWith(ctx context.Context, client *goplugin.Client, spec LaunchSpec, prewarmDuration time.Duration, o launchOptions) (*Host, error) {
	rpcClient, err := client.Client()
	if err != nil {
		return nil, fmt.Errorf("host: handshake: %w", err)
	}

	raw, err := rpcClient.Dispense(PluginServiceName)
	if err != nil {
		return nil, fmt.Errorf("host: dispense: %w", err)
	}
	service, ok := raw.(kernelv1.PluginServiceClient)
	if !ok {
		return nil, fmt.Errorf("host: dispensed value is a %T, not a PluginServiceClient", raw)
	}

	manifest, err := describe(ctx, service)
	if err != nil {
		return nil, err
	}

	// The namespace-registration hook runs here, after the manifest is known
	// and validated but before Start — so a plugin whose Start calls back into
	// the kernel finds its namespace already registered.
	if o.onManifest != nil {
		if err := o.onManifest(manifest); err != nil {
			return nil, fmt.Errorf("host: on-manifest hook: %w", err)
		}
	}

	if _, err := service.Start(ctx, &kernelv1.StartRequest{
		Node:           spec.Node,
		KernelEndpoint: spec.KernelEndpoint,
		CallerId:       spec.CallerID,
		ApiVersion:     spec.APIVersion,
	}); err != nil {
		return nil, fmt.Errorf("host: start: %w", err)
	}

	pid := 0
	if reattach := client.ReattachConfig(); reattach != nil {
		pid = reattach.Pid
	}

	return &Host{
		state:           StateRunning,
		client:          client,
		service:         service,
		manifest:        manifest,
		pid:             pid,
		prewarmDuration: prewarmDuration,
	}, nil
}

// verify is the VERIFIED step: spec.Path must hash to spec.SHA256, or this
// returns ErrChecksumMismatch without ever executing the binary.
func verify(spec LaunchSpec) (err error) {
	if spec.Path == "" {
		return errors.New("host: launch spec has an empty path")
	}
	if spec.SHA256 == "" {
		return errors.New("host: launch spec has an empty sha256")
	}

	f, err := os.Open(spec.Path)
	if err != nil {
		return fmt.Errorf("host: open %q: %w", spec.Path, err)
	}
	defer func() {
		if closeErr := f.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("host: close %q: %w", spec.Path, closeErr)
		}
	}()

	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return fmt.Errorf("host: sha256 %q: %w", spec.Path, err)
	}

	got := hex.EncodeToString(h.Sum(nil))
	want := strings.ToLower(spec.SHA256)
	if got != want {
		return fmt.Errorf("%w: got %s, want %s", ErrChecksumMismatch, got, want)
	}
	return nil
}

// prewarm execs path once and discards it before the real launch. The
// process never completes its own handshake here — no magic-cookie
// environment is set — so it exits on its own almost immediately; this call
// does not depend on why it exits, only on the exec() having happened.
func prewarm(ctx context.Context, path string) (time.Duration, error) {
	start := time.Now()

	cmd := exec.CommandContext(ctx, path)
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("start: %w", err)
	}
	waitErr := cmd.Wait()
	_ = waitErr // a non-zero exit here is expected and carries no information

	return time.Since(start), nil
}

// describe calls Describe and validates the manifest DESCRIBING is
// responsible for: a plugin must name itself, and every channel it declares
// must sit inside the namespace it just named. A cross-plugin check — a name
// some other plugin already owns — needs a registry this package does not
// hold, and belongs where that registry lives.
func describe(ctx context.Context, service kernelv1.PluginServiceClient) (*kernelv1.PluginManifest, error) {
	resp, err := service.Describe(ctx, &kernelv1.DescribeRequest{})
	if err != nil {
		return nil, fmt.Errorf("host: describe: %w", err)
	}

	manifest := resp.GetManifest()
	namespace := manifest.GetNamespace()
	if namespace == "" {
		return nil, ErrEmptyNamespace
	}

	prefix := namespace + "."
	for _, decl := range manifest.GetChannels() {
		if !strings.HasPrefix(decl.GetName(), prefix) {
			return nil, fmt.Errorf("%w: %q declares channel %q outside namespace %q", ErrNamespaceViolation, namespace, decl.GetName(), namespace)
		}
	}

	return manifest, nil
}

// State reports which pipeline step h last completed.
func (h *Host) State() State {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.state
}

// Manifest returns the manifest DESCRIBING validated.
func (h *Host) Manifest() *kernelv1.PluginManifest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.manifest
}

// PID reports the plugin process's own process id, distinct from this
// host's.
func (h *Host) PID() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pid
}

// PrewarmDuration reports how long the PREWARMED step's discarded exec took.
func (h *Host) PrewarmDuration() time.Duration {
	return h.prewarmDuration
}

// Deliver forwards env to the plugin as a unary Deliver call, but only for a
// channel the manifest declared or expressed interest in — anything else is
// refused here, before any call reaches the process. A dead child surfaces
// as ErrUnavailable and moves h to StateUnavailable; Deliver never retries
// and never restarts the process.
func (h *Host) Deliver(ctx context.Context, env *kernelv1.Envelope) (*kernelv1.DeliverResponse, error) {
	h.mu.Lock()
	manifest := h.manifest
	service := h.service
	h.mu.Unlock()

	if !declaresChannel(manifest, env.GetChannel()) {
		return nil, fmt.Errorf("%w: %q", ErrUndeclaredChannel, env.GetChannel())
	}

	resp, err := service.Deliver(ctx, &kernelv1.DeliverRequest{Envelope: env})
	if err != nil {
		if status.Code(err) == codes.Unavailable {
			h.mu.Lock()
			h.state = StateUnavailable
			h.mu.Unlock()
			return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		return nil, fmt.Errorf("host: deliver: %w", err)
	}
	return resp, nil
}

// declaresChannel reports whether manifest owns channel outright or holds a
// channel interest matching it exactly.
func declaresChannel(manifest *kernelv1.PluginManifest, channel string) bool {
	for _, decl := range manifest.GetChannels() {
		if decl.GetName() == channel {
			return true
		}
	}
	for _, interest := range manifest.GetInterests() {
		if chInterest := interest.GetChannel(); chInterest != nil && chInterest.GetPattern() == channel {
			return true
		}
	}
	return false
}

// Kill terminates the plugin process without a graceful Stop call. The
// drain gate (a later change) owns the graceful path; this is the direct
// teardown a test or an emergency shutdown reaches for.
func (h *Host) Kill() {
	h.client.Kill()
}
