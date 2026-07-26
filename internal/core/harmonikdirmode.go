package core

import "io/fs"

// harmonikdirmode.go — the single mode used when creating a directory under a
// project's .harmonik/ state tree.
//
// Why this constant exists (and why it is exported from core rather than from
// the package that owns the daemon file surface): the .harmonik/ tree is created
// on demand from BOTH sides of the CLI/library boundary — cmd/harmonik creates
// .harmonik/, .harmonik/events/ and .harmonik/cognition/ before the daemon is
// up, and a dozen internal/ packages create their own leaves lazily on first
// write. os.MkdirAll does NOT chmod a directory that already exists: it returns
// nil and leaves the mode untouched. So if the two sides disagree on the mode,
// the resulting permissions depend on which process happened to run first —
// which is a worse outcome than either mode chosen consistently. One constant,
// used on both sides, removes the ordering dependency.
//
// internal/core is the home because it is the module's universal leaf (the
// depguard component matrix lets essentially every subsystem import it and lets
// core import nothing back) and because it already owns .harmonik-relative
// layout constants — see transitionpath.go and jsonlformat_hqwn58.go.

// HarmonikDirMode is the permission mode for directories created under a
// project's .harmonik/ state tree.
//
// 0o750 — owner rwx, group rx, world nothing. Nothing in harmonik reads
// .harmonik/ as a different uid: the daemon, the CLI, the keeper, the tmux
// panes and the ssh-reached worker agents all run as the owning user, and the
// worker-side .harmonik/ is created by a remote `mkdir -p` under the worker
// user's own umask (internal/transport/tunnel.EnsureWorkerHarmonikDir), not by
// this constant. World-traversable state was never a requirement and is a
// small leak on a multi-user host (project paths, socket paths, bead ids). It
// also satisfies gosec G301, which the previous 0o755 did not — every one of
// these call sites carried a //nolint:gosec waiver instead.
//
// This is the DEFAULT, not a ceiling. A directory that holds credentials or
// capability tokens is deliberately created TIGHTER (0o700) and must stay that
// way: .harmonik/runs/ (internal/run), .harmonik/decision_acks/
// (internal/sentinel), and the per-worktree .harmonik/claude-config/
// (internal/workspace) are the current examples. Never widen one of those to
// this constant.
//
// Migration: existing installs keep whatever mode their .harmonik/ already has,
// because os.MkdirAll will not tighten it and this code does NOT chmod. That is
// deliberate. An unconditional chmod on a create path would (a) issue a
// permission-changing write on every lazy directory creation, on a tree the
// process may not own, (b) silently revert any widening an operator applied on
// purpose, and (c) fail or warn noisily on a directory owned by another uid.
// The upside would be cosmetic: 0o755 and 0o750 grant identical access to the
// owning user, so a legacy install is not degraded — it is merely one uniform
// notch looser than a fresh one. Uniformity per install is what the ordering
// bug threatened; that is what this constant restores.
const HarmonikDirMode fs.FileMode = 0o750
