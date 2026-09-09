package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	kernelv1 "github.com/gregberns/harmonik/contract/gen/harmonik/kernel/v1"
	"github.com/gregberns/harmonik/kernel/roster"
	"github.com/gregberns/harmonik/kernel/transport"
	"github.com/gregberns/harmonik/kernel/transport/memmesh"
)

// transportPort is the set of transport calls the composition root drives, and
// the one seam that lets a single OS process host either one kernel or N linked
// kernels without a second copy of the bring-up. A lone *transport.Transport
// satisfies it for the single-node `serve` path; a *memmesh.Node satisfies it
// for the `mesh` path, where the same call is routed across linked kernels
// (a publish fans out mesh-wide, a point-to-point subscribe attaches to the
// declaring kernel's queue). The consumer owns the port — it names exactly the
// calls kernelServer and pluginManager make, nothing wider — so it is not, and
// must not grow into, a general network-transport interface; that is the open
// Q-1 decision a later slice owns, the same guard memmesh's own doc states.
type transportPort interface {
	Declare(name string, typ kernelv1.ChannelType) error
	Publish(req *kernelv1.PublishRequest, producer string) (*kernelv1.PublishResponse, error)
	Subscribe(subscriberID, pattern string, group ...string) (*transport.Subscription, error)
	// Ack, Nack, and Detach are the POINT_TO_POINT lease verbs the dispatcher
	// drives: Ack forgets a delivered lease, Nack returns it to the group queue
	// for reassignment, and Detach nacks everything a member holds when a reload
	// takes its process away. They are no-ops for a PUBSUB-only deployment.
	Ack(subscriberID, messageID string) error
	Nack(subscriberID, messageID string) error
	Detach(subscriberID string) error
	Info() *kernelv1.InfoResponse
	Request(ctx context.Context, req *kernelv1.PublishRequest, producer string) (*kernelv1.RequestResponse, error)
	Serve(ctx context.Context, pattern string, deliver func(env *kernelv1.Envelope, requestID string) error) error
	Respond(requestID string, payload []byte, headers map[string]string, errMsg string) error
	LookupPut(channel, key string, value []byte, ttl time.Duration, now time.Time) (uint64, error)
	LookupGet(channel, key string, now time.Time) ([]*kernelv1.LookupEntry, error)
	LookupList(channel, keyPrefix string, now time.Time) ([]*kernelv1.LookupEntry, error)
}

// Both the single-box transport and the mesh node must satisfy the port, or the
// two bring-up paths would drift. These fail the build the moment either does.
var (
	_ transportPort = (*transport.Transport)(nil)
	_ transportPort = (*memmesh.Node)(nil)
)

// meshConfig is the whole `mesh` input: the set of kernels to stand up in one
// process, each with its own plugin. Nodes and plugins are NEVER baked into
// source — they come from a file an operator writes, so the same binary stands
// up any topology.
type meshConfig struct {
	Nodes []meshNodeConfig `json:"nodes"`
}

// meshNodeConfig is one kernel in the mesh: its name, its own SQLite state file,
// its own two listen addresses, and the one plugin it launches. An empty listen
// address picks an ephemeral port (the test scheme); an operator pins a real
// port by naming it.
type meshNodeConfig struct {
	Name        string           `json:"name"`
	Listen      string           `json:"listen"`       // KernelService gRPC; "" picks an ephemeral port
	AdminListen string           `json:"admin_listen"` // admin HTTP; "" picks an ephemeral port
	DBPath      string           `json:"db_path"`
	Plugin      meshPluginConfig `json:"plugin"`
}

// meshPluginConfig names the one plugin a node launches: the same path + sha256
// the single-node config takes, plus the launch args that pick the plugin's
// role (the dispatch --role). The args persist across a reload with the spec.
type meshPluginConfig struct {
	Path   string   `json:"path"`
	SHA256 string   `json:"sha256"`
	Args   []string `json:"args"`
}

// validate refuses a config that could not stand up a sound mesh: no nodes, a
// duplicate node name, a node with no plugin, or two nodes sharing one state
// file (which would corrupt each other's journal). Addresses are left to the
// OS — a port clash surfaces as a listen error at bring-up, named by node.
func (c meshConfig) validate() error {
	if len(c.Nodes) == 0 {
		return errors.New("harmonikd mesh: config has no nodes")
	}
	seenName := make(map[string]bool, len(c.Nodes))
	seenDB := make(map[string]bool, len(c.Nodes))
	for i, n := range c.Nodes {
		if n.Name == "" {
			return fmt.Errorf("harmonikd mesh: node %d has no name", i)
		}
		if seenName[n.Name] {
			return fmt.Errorf("harmonikd mesh: duplicate node name %q", n.Name)
		}
		seenName[n.Name] = true
		if n.DBPath == "" {
			return fmt.Errorf("harmonikd mesh: node %q has no db_path", n.Name)
		}
		if seenDB[n.DBPath] {
			return fmt.Errorf("harmonikd mesh: node %q reuses db_path %q; each node needs its own state file", n.Name, n.DBPath)
		}
		seenDB[n.DBPath] = true
		if n.Plugin.Path == "" || n.Plugin.SHA256 == "" {
			return fmt.Errorf("harmonikd mesh: node %q needs a plugin path and sha256", n.Name)
		}
	}
	return nil
}

// loadMeshConfig reads and validates a mesh config file.
func loadMeshConfig(path string) (meshConfig, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // path is an operator-supplied config file, the intended input
	if err != nil {
		return meshConfig{}, fmt.Errorf("harmonikd mesh: read config %q: %w", path, err)
	}
	var cfg meshConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return meshConfig{}, fmt.Errorf("harmonikd mesh: parse config %q: %w", path, err)
	}
	if err := cfg.validate(); err != nil {
		return meshConfig{}, err
	}
	return cfg, nil
}

// Mesh is one OS process running N linked kernels: each an independent Daemon
// (its own transport, state file, kernel listener, admin listener, and plugin),
// all wired together by a memmesh double so a publish on one is seen on the
// others and a point-to-point group competes across them.
type Mesh struct {
	mesh  *memmesh.Mesh
	nodes []*Daemon
}

// StartMesh stands up every kernel the config names in one process, linked by
// an in-process memmesh double, and returns once all are RUNNING. A node is
// brought up in config order, and a node whose plugin SUBSCRIBES to a channel
// must be listed AFTER the node whose plugin DECLARES it: a declaration is
// forwarded mesh-wide when the declaring plugin launches, and a subscribe finds
// nothing until then. For the dispatch topology that means the primary
// (declarer) precedes every worker — the natural config order.
func StartMesh(ctx context.Context, cfg meshConfig, logger *slog.Logger) (*Mesh, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	mm := memmesh.New()
	// Create every node's transport first, so a declaration from any plugin is
	// forwarded to all of them — no node can miss a channel declared before it
	// joined.
	ports := make([]*memmesh.Node, len(cfg.Nodes))
	for i, nc := range cfg.Nodes {
		node, err := mm.AddNode(nc.Name)
		if err != nil {
			return nil, fmt.Errorf("harmonikd mesh: add node %q: %w", nc.Name, err)
		}
		ports[i] = node
	}

	m := &Mesh{mesh: mm}
	for i, nc := range cfg.Nodes {
		d, err := startNode(ctx, nc.daemonConfig(logger), ports[i], cfg.rosterView(i))
		if err != nil {
			// Tear down whatever already came up, on the same ctx the single-node
			// failure path uses (closeLogged). The start error is what the caller
			// needs, so a teardown error is only logged.
			if closeErr := m.Close(ctx); closeErr != nil {
				slog.ErrorContext(ctx, "harmonikd mesh: close after failed start", "error", closeErr)
			}
			return nil, fmt.Errorf("harmonikd mesh: start node %q: %w", nc.Name, err)
		}
		m.nodes = append(m.nodes, d)
	}
	return m, nil
}

// daemonConfig turns one node's mesh entry into the Config startNode takes.
func (n meshNodeConfig) daemonConfig(logger *slog.Logger) Config {
	listen := n.Listen
	if listen == "" {
		listen = "127.0.0.1:0"
	}
	admin := n.AdminListen
	if admin == "" {
		admin = "127.0.0.1:0"
	}
	return Config{
		Node:         n.Name,
		DBPath:       n.DBPath,
		ListenAddr:   listen,
		AdminAddr:    admin,
		PluginPath:   n.Plugin.Path,
		PluginSHA256: n.Plugin.SHA256,
		PluginArgs:   n.Plugin.Args,
		Logger:       logger,
	}
}

// rosterView builds node i's roster: itself, plus every other configured node
// as a linked peer. A linked memmesh peer is reachable in-process, so it is
// reported ALIVE (probed, zero failures); there is no probe loop in this slice,
// so the roster carries that one static observation per peer.
func (c meshConfig) rosterView(i int) *rosterView {
	self := &kernelv1.Node{Name: c.Nodes[i].Name}
	rv := &rosterView{self: self}
	for j, nc := range c.Nodes {
		if j == i {
			continue
		}
		rv.peers = append(rv.peers, peerObservation{
			node: &kernelv1.Node{Name: nc.Name},
			obs:  roster.Observation{Probed: true, ConsecutiveFailures: 0},
		})
	}
	return rv
}

// Node returns the running daemon for the node named name, or nil if the mesh
// has no such node.
func (m *Mesh) Node(name string) *Daemon {
	for _, d := range m.nodes {
		if d.kernel.node == name {
			return d
		}
	}
	return nil
}

// Nodes returns every running daemon, in config order.
func (m *Mesh) Nodes() []*Daemon { return m.nodes }

// Close tears every node down, in reverse bring-up order, so a node that a
// later node's plugin depends on (the declarer) outlives its dependents.
func (m *Mesh) Close(ctx context.Context) error {
	var firstErr error
	for i := len(m.nodes) - 1; i >= 0; i-- {
		if err := m.nodes[i].Close(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// meshMain is the `mesh` verb: load a config file, stand the mesh up, and block
// until a shutdown signal, then tear it down. It is the N-kernel sibling of
// serveMain; the single-node `serve` path is unchanged.
func meshMain(args []string) error {
	fs := flag.NewFlagSet("harmonikd mesh", flag.ExitOnError)
	configPath := fs.String("config", "", "path to the mesh config file (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		return errors.New("harmonikd mesh: --config is required")
	}

	cfg, err := loadMeshConfig(*configPath)
	if err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	m, err := StartMesh(ctx, cfg, nil)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer shutdownCancel()
		if closeErr := m.Close(shutdownCtx); closeErr != nil {
			slog.ErrorContext(shutdownCtx, "harmonikd mesh: close", "error", closeErr)
		}
	}()

	for _, d := range m.Nodes() {
		slog.InfoContext(ctx, "harmonikd mesh: node listening",
			"node", d.kernel.node, "kernel_addr", d.KernelAddr(), "admin_addr", d.AdminAddr(),
			"plugin_namespace", d.PluginManifest().GetNamespace())
	}

	<-ctx.Done()
	return nil
}
