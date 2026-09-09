package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	defaultKernelAddr = "127.0.0.1:47990"
	shutdownGrace     = 5 * time.Second
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "publish", "journal", "plugin":
			if err := clientMain(os.Args[1:]); err != nil {
				slog.ErrorContext(context.Background(), "harmonikd", "error", err)
				os.Exit(1)
			}
			return
		case "mesh":
			if err := meshMain(os.Args[2:]); err != nil {
				slog.ErrorContext(context.Background(), "harmonikd", "error", err)
				os.Exit(1)
			}
			return
		}
	}

	if err := serveMain(os.Args[1:]); err != nil {
		slog.ErrorContext(context.Background(), "harmonikd", "error", err)
		os.Exit(1)
	}
}

// serveMain starts the daemon and blocks until it is asked to stop.
func serveMain(args []string) error {
	fs := flag.NewFlagSet("harmonikd", flag.ExitOnError)
	node := fs.String("node", "box-1", "this box's name")
	dbPath := fs.String("db", "harmonikd.db", "state database path")
	listen := fs.String("listen", defaultKernelAddr, "KernelService gRPC listen address")
	adminListen := fs.String("admin-listen", "127.0.0.1:47991", "admin surface listen address")
	pluginPath := fs.String("plugin-path", "", "path to the plugin binary to launch (required)")
	pluginSHA256 := fs.String("plugin-sha256", "", "expected sha256 of the plugin binary (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	d, err := Start(ctx, Config{
		Node:         *node,
		DBPath:       *dbPath,
		ListenAddr:   *listen,
		AdminAddr:    *adminListen,
		PluginPath:   *pluginPath,
		PluginSHA256: *pluginSHA256,
	})
	if err != nil {
		return err
	}
	defer func() {
		// ctx is already Done by the time this runs (the shutdown signal that
		// unblocked <-ctx.Done() below), so shutdown gets its own bounded
		// context rather than one that would make Shutdown return instantly.
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer shutdownCancel()
		if closeErr := d.Close(shutdownCtx); closeErr != nil {
			slog.ErrorContext(shutdownCtx, "harmonikd: close", "error", closeErr)
		}
	}()

	slog.InfoContext(ctx, "harmonikd: listening",
		"kernel_addr", d.KernelAddr(), "admin_addr", d.AdminAddr(), "plugin_namespace", d.PluginManifest().GetNamespace())

	<-ctx.Done()
	return nil
}

// clientMain dispatches one of the three client verbs against an already
// running daemon.
func clientMain(args []string) error {
	fs := flag.NewFlagSet("harmonikd "+args[0], flag.ExitOnError)
	kernelAddr := fs.String("addr", defaultKernelAddr, "KernelService gRPC address")
	adminAddr := fs.String("admin-addr", "127.0.0.1:47991", "admin surface address")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	rest := fs.Args()

	ctx := context.Background()

	switch args[0] {
	case "publish":
		if len(rest) != 2 {
			return errors.New("usage: harmonikd publish <channel> <payload>")
		}
		id, err := clientPublish(ctx, *kernelAddr, rest[0], []byte(rest[1]))
		if err != nil {
			return err
		}
		slog.InfoContext(ctx, "harmonikd: published", "message_id", id)
		return nil

	case "journal":
		if len(rest) != 2 || rest[0] != "read" {
			return errors.New("usage: harmonikd journal read <name>")
		}
		lines, err := clientJournalRead(ctx, *kernelAddr, rest[1])
		if err != nil {
			return err
		}
		for _, line := range lines {
			slog.InfoContext(ctx, "harmonikd: journal record", "hex", line)
		}
		return nil

	case "plugin":
		if len(rest) != 2 || rest[0] != "reload" {
			return errors.New("usage: harmonikd plugin reload <namespace>")
		}
		return requestReload(ctx, *adminAddr, reloadRequest{Namespace: rest[1]})

	default:
		return fmt.Errorf("unknown verb %q", args[0])
	}
}
