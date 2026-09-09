// Command harmonikd is the K7 composition root: it wires the kernel's
// transport (K4), state (K3) and plugin host (K5) into one process, launches
// one configured plugin binary (registered from config: a path and a
// sha256, never a name baked into this binary), serves KernelService on a
// loopback gRPC listener for that plugin to dial back into, and exposes the
// operator-facing client verbs (publish, journal read, plugin reload) as a
// tiny local admin surface plus a KernelService gRPC client for the two
// operations KernelService already names.
//
// This binary does not touch the existing cmd/harmonik daemon; the two stay
// side by side until a later slice.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"google.golang.org/grpc"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
	"github.com/gregberns/harmonik/kernel/host"
	"github.com/gregberns/harmonik/kernel/state"
	"github.com/gregberns/harmonik/kernel/transport"
)

// adminReadHeaderTimeout bounds how long the admin HTTP surface waits for a
// client to finish sending request headers, closing the slow-header-write
// resource exhaustion an unbounded http.Server is open to (gosec G112).
const adminReadHeaderTimeout = 5 * time.Second

// Config is everything a Daemon needs to start. PluginPath and PluginSHA256
// are the "config" the package doc and this slice's acceptance criteria
// both mean: this slice registers exactly one plugin, and names it only
// through these two fields, never as a literal in source.
type Config struct {
	Node         string
	DBPath       string
	ListenAddr   string // KernelService gRPC, loopback; "" picks an ephemeral port
	AdminAddr    string // admin HTTP for the client verbs; "" picks an ephemeral port
	PluginPath   string
	PluginSHA256 string
	PluginArgs   []string     // command-line args handed to the plugin at launch; carried on LaunchSpec so a reload keeps them (the dispatch --role, say). nil for a plugin that takes none.
	Logger       *slog.Logger // drain-gate + dispatch diagnostics; nil uses slog.Default()
}

// Daemon is one running harmonikd process: the kernel gRPC listener, the
// admin HTTP listener, and the plugin this slice registered.
type Daemon struct {
	journal   *state.State
	transport transportPort
	kernel    *kernelServer
	plugin    *pluginManager

	grpcServer *grpc.Server
	grpcLis    net.Listener

	adminServer *http.Server
	adminLis    net.Listener
}

// Start wires the kernel and brings the configured plugin up to RUNNING.
// The kernel gRPC listener is live before the plugin is launched, because
// the plugin's own Start dials it back immediately.
//
// Start is the single-node path: it builds this box its own transport and
// serves a self-only roster. The mesh path (StartMesh) reuses the same bring-up
// through startNode, handing it a memmesh node in place of the lone transport
// and a peer-aware roster — the one difference between one box and N linked
// ones, kept in one place rather than in a second copy of this wiring.
func Start(ctx context.Context, cfg Config) (*Daemon, error) {
	if cfg.PluginPath == "" || cfg.PluginSHA256 == "" {
		return nil, errors.New("harmonikd: config needs a plugin path and sha256")
	}
	return startNode(ctx, cfg, transport.New(cfg.Node), nil)
}

// startNode is the bring-up both Start and StartMesh share: open state, stand
// the kernel gRPC listener up before anything can dial it, launch the one
// configured plugin, then open the admin listener. port is this node's
// transport — a lone *transport.Transport for a single box, or a *memmesh.Node
// that routes the same calls across linked kernels for a mesh. roster is the
// peer view RosterList serves; nil leaves RosterList answering self-only, the
// single-node behaviour.
func startNode(ctx context.Context, cfg Config, port transportPort, roster *rosterView) (*Daemon, error) {
	// state.Open takes no context: it is K3's own API (kernel/state), a
	// separate change this one only wires together.
	journal, err := state.Open(cfg.DBPath) //nolint:contextcheck // state.Open's signature is K3's, not this package's to change
	if err != nil {
		return nil, fmt.Errorf("harmonikd: open state: %w", err)
	}

	kernel := newKernelServer(cfg.Node, port, journal)
	if roster != nil {
		kernel.setRoster(roster)
	}

	grpcLis, grpcServer, err := startKernelGRPC(ctx, cfg.ListenAddr, kernel)
	if err != nil {
		closeLogged(ctx, journal)
		return nil, err
	}

	// Register the plugin's namespace the moment its manifest is known, before
	// its own Start runs: a plugin that replays journals during Start (the
	// dispatch plugin) calls back into this kernel, and those calls resolve
	// against the namespace this hook sets.
	registerNamespace := func(m *kernelv1.PluginManifest) error {
		kernel.setNamespace(m.GetNamespace())
		return nil
	}

	plugin, err := launchPlugin(ctx, host.LaunchSpec{
		Path:           cfg.PluginPath,
		SHA256:         cfg.PluginSHA256,
		Node:           cfg.Node,
		KernelEndpoint: grpcLis.Addr().String(),
		CallerID:       cfg.Node,
		APIVersion:     1,
		Args:           cfg.PluginArgs,
	}, port, cfg.Logger, registerNamespace)
	if err != nil {
		grpcServer.Stop()
		closeLogged(ctx, journal)
		return nil, err
	}
	// The namespace is registered once, by the OnManifest hook above, before the
	// plugin's own Start runs. A second setNamespace here would be redundant —
	// the hook has already fired by the time Launch returns.

	d := &Daemon{
		journal:    journal,
		transport:  port,
		kernel:     kernel,
		plugin:     plugin,
		grpcServer: grpcServer,
		grpcLis:    grpcLis,
	}

	if err := d.startAdmin(ctx, cfg.AdminAddr); err != nil {
		plugin.close()
		grpcServer.Stop()
		closeLogged(ctx, journal)
		return nil, err
	}

	return d, nil
}

// startKernelGRPC opens the KernelService listener and starts serving on it
// in the background, before any plugin exists to dial it.
func startKernelGRPC(ctx context.Context, addr string, kernel *kernelServer) (net.Listener, *grpc.Server, error) {
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, fmt.Errorf("harmonikd: listen %q: %w", addr, err)
	}

	grpcServer := grpc.NewServer()
	kernelv1.RegisterKernelServiceServer(grpcServer, kernel)
	go func() {
		if serveErr := grpcServer.Serve(lis); serveErr != nil {
			slog.ErrorContext(ctx, "harmonikd: kernel gRPC server stopped", "error", serveErr)
		}
	}()

	return lis, grpcServer, nil
}

// startAdmin opens the admin listener and starts serving on it in the
// background. Plugin reload is a host-lifecycle action, not one of the
// contract's 19 kernel RPCs, so it lives on this small local surface
// instead of stretching the wire contract to carry it.
func (d *Daemon) startAdmin(ctx context.Context, addr string) error {
	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("harmonikd: admin listen %q: %w", addr, err)
	}

	d.adminLis = lis
	d.adminServer = &http.Server{Handler: d.adminMux(), ReadHeaderTimeout: adminReadHeaderTimeout}
	go func() {
		if serveErr := d.adminServer.Serve(lis); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			slog.ErrorContext(ctx, "harmonikd: admin server stopped", "error", serveErr)
		}
	}()

	return nil
}

func closeLogged(ctx context.Context, j *state.State) {
	if err := j.Close(); err != nil {
		slog.ErrorContext(ctx, "harmonikd: close state after start-up failure", "error", err)
	}
}

// KernelAddr reports the address a plugin (or a test acting as one) dials
// KernelService on.
func (d *Daemon) KernelAddr() string { return d.grpcLis.Addr().String() }

// AdminAddr reports the address the client verbs reach the admin surface on.
func (d *Daemon) AdminAddr() string { return d.adminLis.Addr().String() }

// PluginManifest reports the registered plugin's own manifest, discovered at
// launch — never assumed by this package.
func (d *Daemon) PluginManifest() *kernelv1.PluginManifest { return d.plugin.currentManifest() }

// Close tears the daemon down: the admin surface, the kernel gRPC surface,
// the plugin process, and the state handle, in that order.
func (d *Daemon) Close(ctx context.Context) error {
	if err := d.adminServer.Shutdown(ctx); err != nil {
		slog.ErrorContext(ctx, "harmonikd: shut down admin server", "error", err)
	}
	d.plugin.close()
	d.grpcServer.GracefulStop()
	return d.journal.Close()
}
