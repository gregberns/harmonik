package main

// sync_assets_cmd.go — `harmonik sync-assets` subcommand: the ONGOING update path
// that reconciles a project's on-disk instruction files against the binary's
// embedded asset bundle.
//
// # Purpose (hk-i7i3)
//
// `harmonik init` writes the embedded assets into a project ONCE. After a newer
// harmonik is `go install`ed, the project's instruction files (.claude/skills/*,
// AGENTS.md, .harmonik/context/*) are frozen at the version that ran init. This
// command pulls the improvements down via a class-aware 3-way reconcile:
//
//	embed_sha = BuildManifest()[path].Sha256   # what the binary ships now
//	lock_sha  = .harmonik/assets.lock          # what we last installed
//	disk_sha  = sha256(project file)           # what's there now (may be edited)
//
// The planner (Reconcile, asset_reconcile.go) is reused verbatim; this file is
// the EXECUTOR. SAFETY is the priority: it must never silently clobber a
// project's local edits or a content-owned body.
//
// # Flags
//
//	--dry-run   (DEFAULT) print the plan, write NOTHING.
//	--apply     execute the plan per the class policy below.
//	--commit    --apply + git commit the result.
//	--force     bypass the daemon-lull gate.
//	--project   target project dir (default: cwd; same resolution as init).
//
// # Per-class apply policy (the safety core)
//
//	Managed (skills):
//	    FastForward/Create → overwrite from embed.
//	    Conflict → write <dest>.harmonik-new + report; NEVER touch the edited file.
//	ManagedRegion (AGENTS.md):
//	    FastForward/Conflict → replace ONLY the <!-- BEGIN harmonik:managed … -->
//	    … <!-- END harmonik:managed --> region(s) from the embed template; preserve
//	    everything OUTSIDE the markers. If the markers are missing/corrupt → treat
//	    as Conflict: write .harmonik-new + report, don't clobber.
//	    Create → write the whole template if absent.
//	ContentOwned (context tiers):
//	    Create → write from template if absent.
//	    FastForward → refresh ONLY the self-describing header region (the leading
//	    <!-- TIER: … --> block); NEVER touch the body.
//	    Conflict → report only, write nothing.
//	Scaffold:
//	    Create → write once if absent; otherwise Leave.
//	Leave → never touch.
//	Skip → no write; BUT a stale Skip (disk already == embed, lock behind) is
//	    re-stamped into the lock so it does not recur.
//
// After a successful --apply the lock is re-stamped from the PRIOR lock + the
// per-item outcomes (lockFromOutcomes): written / already-current files advance
// to the embed sha, but CONFLICTED files keep their prior entry so the conflict
// re-surfaces every run until the operator reconciles it (it is NOT buried).
//
// # Daemon-lull gate (LOAD-BEARING)
//
// Writing into the main working tree while the daemon is dispatching trips the
// worktree-escape detector (implementer_escaped_worktree) and fails in-flight
// beads. So before --apply: if the daemon is up AND a queue is actively
// dispatching, REFUSE unless --force. Daemon down → proceed.
//
// Bead ref: hk-i7i3 (sync-assets command). Design: plans/2026-06-20-doc-instruction-audit/10-asset-sync.md.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/queue"
)

// agentsManagedBeginPrefix / agentsManagedEndMarker delimit the product-owned
// region of the AGENTS router. The BEGIN marker carries a region label after
// "harmonik:managed " (e.g. "agents-router"); we match on the prefix so any
// labelled region is recognised. The END marker is unlabelled.
const (
	agentsManagedBeginPrefix = "<!-- BEGIN harmonik:managed"
	agentsManagedEndMarker   = "<!-- END harmonik:managed -->"
)

// contentTierHeaderOpen opens the self-describing header comment block of every
// content-owned tier file. The header opens with "<!-- TIER:" and the FIRST
// "-->" closes it. The body is everything after that line.
const contentTierHeaderOpen = "<!-- TIER:"

// runSyncAssetsSubcommand dispatches `harmonik sync-assets [flags]`.
//
// Exit codes:
//
//	0  — success (dry-run printed, or apply completed; conflicts are NOT errors —
//	     they are written as .harmonik-new and reported, exit 0)
//	1  — argument, precondition, or I/O error
//	3  — daemon-lull gate refused (daemon dispatching, no --force)
func runSyncAssetsSubcommand(args []string) int {
	return runSyncAssets(args, os.Stdout, os.Stderr)
}

// destFor maps an embed asset path (e.g. "assets/skills/keeper/SKILL.md") to its
// project-relative destination, mirroring how init writes assets:
//
//	assets/skills/*                     → .claude/skills/*
//	assets/templates/AGENTS.template.md → AGENTS.md
//	assets/context/<x>.tmpl             → .harmonik/context/<x>     (HANDOFF.md.tmpl → HANDOFF.md at root)
//	assets/scaffolds/*                  → <repo root>/*
//
// Returns ("", false) for any path that has no init-defined destination (e.g.
// an Unclassified asset), so the executor can skip it safely.
func destFor(embedPath string) (string, bool) {
	rel := strings.TrimPrefix(embedPath, assetEmbedRoot+"/")
	switch {
	case strings.HasPrefix(rel, "skills/"):
		// assets/skills/<name>/<file> → .claude/skills/<name>/<file>
		return filepath.Join(".claude", "skills", strings.TrimPrefix(rel, "skills/")), true
	case rel == "templates/AGENTS.template.md":
		return "AGENTS.md", true
	case strings.HasPrefix(rel, "context/"):
		base := strings.TrimPrefix(rel, "context/")
		// HANDOFF.md.tmpl is special-cased to the repo root (init does the same).
		if base == "HANDOFF.md.tmpl" {
			return "HANDOFF.md", true
		}
		// <x>.tmpl → .harmonik/context/<x>
		return filepath.Join(".harmonik", "context", strings.TrimSuffix(base, ".tmpl")), true
	case strings.HasPrefix(rel, "scaffolds/"):
		return strings.TrimPrefix(rel, "scaffolds/"), true
	default:
		return "", false
	}
}

// sha256File returns the hex sha256 of the file at path, or "" when the file is
// absent (the reconcile planner's "disk absent" sentinel). Any other read error
// is returned.
func sha256File(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path derived from the embed manifest + project dir
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// buildDiskHashes computes the on-disk sha256 for every manifest path, keyed by
// the EMBED path (so it lines up with the manifest + lock keys the planner uses).
// Absent files map to "" per the planner's contract.
func buildDiskHashes(projectDir string, m Manifest, stderr io.Writer) (map[string]string, error) {
	disk := make(map[string]string, len(m.Files))
	for _, f := range m.Files {
		dest, ok := destFor(f.Path)
		if !ok {
			// No init-defined destination: record absent so the planner does not
			// fabricate a conflict against a path we never write.
			disk[f.Path] = ""
			continue
		}
		sum, err := sha256File(filepath.Join(projectDir, dest))
		if err != nil {
			return nil, fmt.Errorf("hash %s: %w", dest, err)
		}
		disk[f.Path] = sum
	}
	return disk, nil
}

// applyOutcome records the concrete file operation taken for one ReconcileItem,
// for the post-apply summary.
type applyOutcome struct {
	item    ReconcileItem
	dest    string // project-relative destination ("" if none)
	written bool   // a file was written from embed
	conflic bool   // a .harmonik-new conflict file was produced
	created bool   // the destination was created fresh
	skipped bool   // no write (Skip/Leave/no-dest)
	note    string // short human note
}

func runSyncAssets(args []string, stdout, stderr io.Writer) int {
	var (
		projectDir string
		dryRun     = true // DEFAULT: dry-run
		apply      bool
		commit     bool
		force      bool
	)

	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--help" || args[i] == "-h":
			fmt.Fprint(stdout, syncAssetsUsage)
			return 0
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectDir = args[i]
		case strings.HasPrefix(args[i], "--project="):
			projectDir = strings.TrimPrefix(args[i], "--project=")
		case args[i] == "--dry-run":
			dryRun = true
		case args[i] == "--apply":
			apply = true
		case args[i] == "--commit":
			apply = true
			commit = true
		case args[i] == "--force":
			force = true
		default:
			fmt.Fprintf(stderr, "harmonik sync-assets: unrecognised argument %q\n", args[i])
			fmt.Fprint(stderr, syncAssetsUsage)
			return 1
		}
	}
	// --apply / --commit override the dry-run default.
	if apply {
		dryRun = false
	}

	// Resolve project directory (same resolution as init).
	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(stderr, "harmonik sync-assets: cannot determine working directory: %v\n", err)
			return 1
		}
		projectDir = wd
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik sync-assets: cannot resolve project path %q: %v\n", projectDir, err)
		return 1
	}
	projectDir = absProject
	if _, err := os.Stat(projectDir); err != nil {
		fmt.Fprintf(stderr, "harmonik sync-assets: project directory %q does not exist or is not accessible: %v\n", projectDir, err)
		return 1
	}

	// Compute the plan.
	manifest, err := BuildManifest()
	if err != nil {
		fmt.Fprintf(stderr, "harmonik sync-assets: build manifest: %v\n", err)
		return 1
	}
	lock, err := ReadLock(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik sync-assets: read lock: %v\n", err)
		return 1
	}
	disk, err := buildDiskHashes(projectDir, manifest, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "harmonik sync-assets: hash project files: %v\n", err)
		return 1
	}
	plan := Reconcile(manifest, lock, disk)

	if dryRun {
		printPlanTable(plan, stdout)
		fmt.Fprintln(stdout, "\nharmonik sync-assets: dry-run — no files written. Re-run with --apply to update.")
		return 0
	}

	// --apply / --commit: daemon-lull gate FIRST (unless --force).
	if !force {
		dispatching, reason, gerr := daemonDispatchGate(projectDir)
		if gerr != nil {
			fmt.Fprintf(stderr, "harmonik sync-assets: daemon-lull check failed: %v\n", gerr)
			return 1
		}
		if dispatching {
			fmt.Fprintf(stderr, "harmonik sync-assets: REFUSING to apply — the daemon is actively dispatching (%s).\n", reason)
			fmt.Fprintln(stderr, "  Editing the main working tree mid-dispatch trips implementer_escaped_worktree and fails in-flight beads.")
			fmt.Fprintln(stderr, "  Wait for a lull (no active queue items), or re-run with --force to override.")
			return 3
		}
	}

	outcomes, code := applyPlan(projectDir, manifest, plan, stdout, stderr)
	if code != 0 {
		return code
	}

	// Re-stamp the lock from per-item OUTCOMES, NOT blindly from the manifest.
	// A blind LockFromManifest stamps EVERY path to the embed sha — including
	// files that came back ActionConflict (written to <dest>.harmonik-new with
	// the original untouched). The next run would then see lock==embed → Skip →
	// the conflict is silently buried forever. Building from outcomes preserves a
	// conflicted file's PRIOR lock entry so the conflict re-surfaces every run
	// until the operator reconciles it (disk hash matches embed).
	newLock := lockFromOutcomes(lock, outcomes)
	if err := WriteLock(projectDir, newLock); err != nil {
		fmt.Fprintf(stderr, "harmonik sync-assets: write lock: %v\n", err)
		return 1
	}

	printApplySummary(outcomes, stdout)

	if commit {
		if code := commitSync(projectDir, outcomes, stdout, stderr); code != 0 {
			return code
		}
	}
	return 0
}

// applyPlan executes each ReconcileItem per its class policy. It returns the
// per-item outcomes (for the summary) and an exit code (non-zero only on a real
// I/O error — conflicts are reported, not errors).
func applyPlan(projectDir string, m Manifest, plan []ReconcileItem, stdout, stderr io.Writer) ([]applyOutcome, int) {
	outcomes := make([]applyOutcome, 0, len(plan))
	for _, item := range plan {
		out := applyOutcome{item: item}
		dest, hasDest := destFor(item.Path)
		out.dest = dest

		// Leave / no-destination / Skip → never write.
		if item.Action == ActionLeave || !hasDest {
			out.skipped = true
			out.note = "left untouched"
			outcomes = append(outcomes, out)
			continue
		}
		if item.Action == ActionSkip {
			out.skipped = true
			out.note = "up to date"
			outcomes = append(outcomes, out)
			continue
		}

		full := filepath.Join(projectDir, dest)
		embedData, rerr := initSkillAssets.ReadFile(item.Path)
		if rerr != nil {
			fmt.Fprintf(stderr, "harmonik sync-assets: read embedded asset %s: %v\n", item.Path, rerr)
			return outcomes, 1
		}
		// Render template substitutions for the AGENTS template (matches init).
		// Content/scaffold templates are written verbatim by init, so we do not
		// substitute there.

		switch item.Class {
		case Managed:
			code := applyManaged(full, dest, embedData, item.Action, &out, stdout, stderr)
			if code != 0 {
				return outcomes, code
			}
		case ManagedRegion:
			code := applyManagedRegion(projectDir, full, dest, embedData, item.Action, &out, stdout, stderr)
			if code != 0 {
				return outcomes, code
			}
		case ContentOwned:
			code := applyContentOwned(full, dest, embedData, item.Action, &out, stdout, stderr)
			if code != 0 {
				return outcomes, code
			}
		case Scaffold:
			code := applyScaffold(full, dest, embedData, item.Action, &out, stdout, stderr)
			if code != 0 {
				return outcomes, code
			}
		default:
			// Unclassified with a destination should not occur (destFor returns
			// false for them), but be conservative: leave untouched.
			out.skipped = true
			out.note = "unclassified; left untouched"
		}
		outcomes = append(outcomes, out)
	}
	return outcomes, 0
}

// lockFromOutcomes builds the lock to stamp after an apply from the PRIOR lock
// plus the per-item outcomes — instead of blindly LockFromManifest, which would
// bury conflicts (see runSyncAssets). Per item:
//
//   - written or already-current (Skip because disk==embed): advance the entry to
//     the embed sha — the file now matches the embed.
//   - CONFLICT (a .harmonik-new was written / content-owned conflict reported;
//     original NOT updated): PRESERVE the prior lock entry unchanged (or omit it
//     when there was none) so the file re-surfaces as a conflict on every run
//     until the operator reconciles it (its disk hash matches the embed).
//   - Leave (project-authored, not in embed): no lock entry.
//
// Items with no embed sha (no manifest entry) and items we left untouched carry
// forward whatever prior entry existed (if any), so we never lose unrelated
// lock state.
func lockFromOutcomes(prior Lock, outcomes []applyOutcome) Lock {
	out := Lock{
		FormatVersion: LockFormatVersion,
		Files:         make(map[string]LockEntry, len(outcomes)),
	}
	for _, o := range outcomes {
		path := o.item.Path
		switch {
		case o.conflic || o.item.Action == ActionConflict:
			// Conflict (either a .harmonik-new was written, or a content-owned
			// conflict was reported with the original untouched): preserve the
			// prior entry unchanged so the conflict re-surfaces next run; omit if
			// there was none. NEVER advance to the embed sha — that buries it.
			if pe, ok := prior.Files[path]; ok {
				out.Files[path] = LockEntry{Path: path, Sha256: pe.Sha256}
			}
		case o.item.Action == ActionLeave:
			// Project-authored, not in the embed: no lock entry.
		case o.written || o.item.Action == ActionSkip || o.item.Action == ActionFastForward:
			// Written, or already-current (Skip because disk==embed): the file
			// now matches the embed → stamp the embed sha when we have it.
			if o.item.EmbedSha != "" {
				out.Files[path] = LockEntry{Path: path, Sha256: o.item.EmbedSha}
			} else if pe, ok := prior.Files[path]; ok {
				out.Files[path] = LockEntry{Path: path, Sha256: pe.Sha256}
			}
		default:
			// Anything else (e.g. an item we skipped without a clear class
			// outcome): carry the prior entry forward if present.
			if pe, ok := prior.Files[path]; ok {
				out.Files[path] = LockEntry{Path: path, Sha256: pe.Sha256}
			}
		}
	}
	return out
}

// applyManaged handles product-owned skill files: overwrite on FastForward/Create;
// on Conflict write <dest>.harmonik-new and NEVER touch the edited file.
func applyManaged(full, dest string, embedData []byte, action Action, out *applyOutcome, stdout, stderr io.Writer) int {
	switch action {
	case ActionFastForward, ActionCreate:
		if err := writeFileEnsureDir(full, embedData); err != nil {
			fmt.Fprintf(stderr, "harmonik sync-assets: write %s: %v\n", dest, err)
			return 1
		}
		out.written = true
		out.created = action == ActionCreate
		out.note = "overwritten from embed"
	case ActionConflict:
		newPath := full + ".harmonik-new"
		if err := writeFileEnsureDir(newPath, embedData); err != nil {
			fmt.Fprintf(stderr, "harmonik sync-assets: write %s: %v\n", dest+".harmonik-new", err)
			return 1
		}
		out.conflic = true
		out.note = "CONFLICT: local edits — embed written to " + dest + ".harmonik-new (original untouched)"
	}
	return 0
}

// applyManagedRegion handles the AGENTS router: replace only the marker-delimited
// managed region(s); preserve everything outside the markers. Markers missing →
// treat as Conflict.
func applyManagedRegion(projectDir, full, dest string, embedData []byte, action Action, out *applyOutcome, stdout, stderr io.Writer) int {
	// Render template substitutions exactly as init does, so the managed region
	// we splice in matches what init would have written.
	rendered := renderAgentsTemplate(string(embedData), projectDir)

	if action == ActionCreate {
		if err := writeFileEnsureDir(full, []byte(rendered)); err != nil {
			fmt.Fprintf(stderr, "harmonik sync-assets: write %s: %v\n", dest, err)
			return 1
		}
		out.written = true
		out.created = true
		out.note = "router created from template"
		return 0
	}

	// FastForward or Conflict → splice the managed region into the on-disk file.
	current, rerr := os.ReadFile(full) //nolint:gosec // G304: full is under the resolved project dir
	if rerr != nil {
		// Disk missing where the planner thought it present: fall back to create.
		if os.IsNotExist(rerr) {
			if err := writeFileEnsureDir(full, []byte(rendered)); err != nil {
				fmt.Fprintf(stderr, "harmonik sync-assets: write %s: %v\n", dest, err)
				return 1
			}
			out.written = true
			out.created = true
			out.note = "router (re)created from template"
			return 0
		}
		fmt.Fprintf(stderr, "harmonik sync-assets: read %s: %v\n", dest, rerr)
		return 1
	}

	merged, ok := spliceManagedRegions(string(current), rendered)
	if !ok {
		// Markers missing/corrupt on disk OR in the template → don't clobber the
		// project's file; write the fresh template alongside for manual reconcile.
		newPath := full + ".harmonik-new"
		if err := writeFileEnsureDir(newPath, []byte(rendered)); err != nil {
			fmt.Fprintf(stderr, "harmonik sync-assets: write %s: %v\n", dest+".harmonik-new", err)
			return 1
		}
		out.conflic = true
		out.note = "CONFLICT: managed markers missing/corrupt — template written to " + dest + ".harmonik-new (original untouched)"
		return 0
	}

	if merged == string(current) {
		out.skipped = true
		out.note = "managed region already current"
		return 0
	}
	if err := writeFileEnsureDir(full, []byte(merged)); err != nil {
		fmt.Fprintf(stderr, "harmonik sync-assets: write %s: %v\n", dest, err)
		return 1
	}
	out.written = true
	out.note = "managed region updated; project deltas preserved"
	return 0
}

// applyContentOwned handles the project-owned context tiers: Create writes the
// template; FastForward refreshes ONLY the TIER header region, body untouched;
// Conflict reports only.
func applyContentOwned(full, dest string, embedData []byte, action Action, out *applyOutcome, stdout, stderr io.Writer) int {
	switch action {
	case ActionCreate:
		if err := writeFileEnsureDir(full, embedData); err != nil {
			fmt.Fprintf(stderr, "harmonik sync-assets: write %s: %v\n", dest, err)
			return 1
		}
		out.written = true
		out.created = true
		out.note = "created from template"
	case ActionFastForward:
		current, rerr := os.ReadFile(full) //nolint:gosec // G304: under resolved project dir
		if rerr != nil {
			if os.IsNotExist(rerr) {
				if err := writeFileEnsureDir(full, embedData); err != nil {
					fmt.Fprintf(stderr, "harmonik sync-assets: write %s: %v\n", dest, err)
					return 1
				}
				out.written = true
				out.created = true
				out.note = "created from template"
				return 0
			}
			fmt.Fprintf(stderr, "harmonik sync-assets: read %s: %v\n", dest, rerr)
			return 1
		}
		merged, ok := replaceTierHeader(string(current), string(embedData))
		if !ok {
			// Can't locate a header in either file → report only, body is owned.
			out.skipped = true
			out.note = "header region not found; body owned — left untouched"
			return 0
		}
		if merged == string(current) {
			out.skipped = true
			out.note = "header already current"
			return 0
		}
		if err := writeFileEnsureDir(full, []byte(merged)); err != nil {
			fmt.Fprintf(stderr, "harmonik sync-assets: write %s: %v\n", dest, err)
			return 1
		}
		out.written = true
		out.note = "TIER header refreshed; body preserved"
	case ActionConflict:
		// Body is project-owned: report only, write nothing.
		out.skipped = true
		out.note = "CONFLICT on content-owned file — body is project-owned; left untouched (reconcile manually)"
	}
	return 0
}

// applyScaffold handles create-once stub files: write only on Create; otherwise
// leave (the planner only emits Create/Skip/Leave/Conflict for these — Conflict
// and FastForward on a create-once stub are treated as leave-untouched).
func applyScaffold(full, dest string, embedData []byte, action Action, out *applyOutcome, stdout, stderr io.Writer) int {
	if action == ActionCreate {
		if err := writeFileEnsureDir(full, embedData); err != nil {
			fmt.Fprintf(stderr, "harmonik sync-assets: write %s: %v\n", dest, err)
			return 1
		}
		out.written = true
		out.created = true
		out.note = "scaffold written"
		return 0
	}
	// FastForward / Conflict on a create-once scaffold → leave the project's file.
	out.skipped = true
	out.note = "scaffold present; left untouched"
	return 0
}

// renderAgentsTemplate mirrors init's renderAgentsMD substitution so the managed
// region spliced into AGENTS.md matches what init would write.
func renderAgentsTemplate(tmpl, projectDir string) string {
	rendered := strings.ReplaceAll(tmpl, "$PROJECT_DIR", projectDir)
	// $TARGET_BRANCH lives outside the managed region in practice; default to the
	// project's configured target where unknown is harmless. We leave it as-is so
	// re-splicing never rewrites a project's branch choice; init owns first write.
	return rendered
}

// spliceManagedRegions replaces each <!-- BEGIN harmonik:managed … --> …
// <!-- END harmonik:managed --> region in current with the SAME-INDEX region
// from template, preserving everything outside the markers in current. Returns
// (merged, true) on success; (current, false) when the marker structure can't be
// matched (counts differ, or a BEGIN has no matching END) — the caller treats
// that as a conflict and never clobbers.
func spliceManagedRegions(current, template string) (string, bool) {
	curRegions := findManagedRegions(current)
	tplRegions := findManagedRegions(template)
	if len(curRegions) == 0 || len(tplRegions) == 0 {
		return current, false
	}
	if len(curRegions) != len(tplRegions) {
		// Structural mismatch: don't risk a wrong splice.
		return current, false
	}
	// Replace from the LAST region backwards so earlier offsets stay valid.
	merged := current
	for i := len(curRegions) - 1; i >= 0; i-- {
		c := curRegions[i]
		t := tplRegions[i]
		merged = merged[:c.start] + template[t.start:t.end] + merged[c.end:]
	}
	return merged, true
}

// region is a half-open [start,end) byte span covering a full managed block
// INCLUDING the BEGIN and END marker lines.
type region struct {
	start int
	end   int
}

// findManagedRegions locates every managed block in s. Each region spans from the
// first byte of the BEGIN marker line to the byte just after the END marker line.
// Returns nil when an unbalanced marker structure is found (a BEGIN with no END).
func findManagedRegions(s string) []region {
	var regions []region
	idx := 0
	for {
		bi := strings.Index(s[idx:], agentsManagedBeginPrefix)
		if bi < 0 {
			break
		}
		bstart := idx + bi
		ei := strings.Index(s[bstart:], agentsManagedEndMarker)
		if ei < 0 {
			// BEGIN without a matching END → unbalanced.
			return nil
		}
		eend := bstart + ei + len(agentsManagedEndMarker)
		// Extend end to include the rest of the END marker's line (trailing \n).
		if eend < len(s) && s[eend] == '\n' {
			eend++
		}
		regions = append(regions, region{start: bstart, end: eend})
		idx = eend
	}
	return regions
}

// replaceTierHeader replaces the leading TIER header comment block of current
// with the one from template, preserving current's body byte-for-byte. The
// header is the span from contentTierHeaderOpen to the first "-->" (inclusive),
// plus a trailing newline. Returns (merged, true) on success; (current, false)
// when either file lacks a locatable header.
func replaceTierHeader(current, template string) (string, bool) {
	ch, cok := tierHeaderSpan(current)
	th, tok := tierHeaderSpan(template)
	if !cok || !tok {
		return current, false
	}
	merged := template[th.start:th.end] + current[ch.end:]
	return merged, true
}

// tierHeaderSpan locates the leading TIER header comment in s: from the
// contentTierHeaderOpen sentinel to the first "-->" (plus a trailing newline if
// present). Returns (span, true) only when the header begins within the first
// non-whitespace content of the file.
func tierHeaderSpan(s string) (region, bool) {
	open := strings.Index(s, contentTierHeaderOpen)
	if open < 0 {
		return region{}, false
	}
	// The header must be at the very top (only whitespace may precede it).
	if strings.TrimSpace(s[:open]) != "" {
		return region{}, false
	}
	closeRel := strings.Index(s[open:], "-->")
	if closeRel < 0 {
		return region{}, false
	}
	end := open + closeRel + len("-->")
	if end < len(s) && s[end] == '\n' {
		end++
	}
	return region{start: open, end: end}, true
}

// writeFileEnsureDir writes data to path, creating the parent directory tree.
func writeFileEnsureDir(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // G301: 0755 matches .harmonik/.claude conventions
		return err
	}
	//nolint:gosec // G306: 0644 matches init's file-mode conventions
	return os.WriteFile(path, data, 0o644)
}

// ---------------------------------------------------------------------------
// Daemon-lull gate
// ---------------------------------------------------------------------------

// daemonDispatchGate reports whether the daemon is up AND actively dispatching.
// Returns (dispatching, reason, err). When the daemon socket is absent or refuses
// the connection, the daemon is down → (false, "", nil) and apply proceeds.
func daemonDispatchGate(projectDir string) (bool, string, error) {
	up := daemonSocketUp(projectDir)
	if !up {
		return false, "", nil
	}
	// Daemon up: load every queue and decide via the pure check.
	names, err := queue.EnumerateQueueNames(projectDir)
	if err != nil {
		return false, "", fmt.Errorf("enumerate queues: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var queues []*queue.Queue
	for _, name := range names {
		q, lerr := queue.Load(ctx, projectDir, name)
		if lerr != nil || q == nil {
			continue
		}
		queues = append(queues, q)
	}
	if q, reason := dispatchingQueue(queues); q {
		return true, reason, nil
	}
	return false, "", nil
}

// daemonSocketUp reports whether the daemon Unix socket exists AND accepts a
// connection. A present-but-refused socket (stale file from a killed daemon)
// counts as DOWN so apply can proceed.
func daemonSocketUp(projectDir string) bool {
	sockPath := filepath.Join(projectDir, ".harmonik", "daemon.sock")
	if _, err := os.Stat(sockPath); err != nil {
		return false
	}
	conn, err := (&net.Dialer{Timeout: 2 * time.Second}).Dial("unix", sockPath)
	if err != nil {
		return false
	}
	_ = conn.Close() //nolint:errcheck
	return true
}

// dispatchingQueue is the PURE daemon-lull decision: a daemon is "actively
// dispatching" when any loaded ACTIVE queue holds at least one item that is
// pending or already dispatched — REGARDLESS of its enclosing group's status.
//
// We deliberately do NOT gate on GroupStatusActive (the prior behavior): a group
// already marked complete-with-failures or in a transitioning state can still
// hold a Dispatched/pending item mid-flight, and writing into the main worktree
// then would trip implementer_escaped_worktree and fail that in-flight bead. So
// ANY in-flight item in an active queue blocks --apply (unless --force). Returns
// (true, reason) on the first such item, else (false, "").
func dispatchingQueue(queues []*queue.Queue) (bool, string) {
	for _, q := range queues {
		if q == nil || q.Status != queue.QueueStatusActive {
			continue
		}
		for _, g := range q.Groups {
			for _, it := range g.Items {
				if it.Status == queue.ItemStatusPending || it.Status == queue.ItemStatusDispatched {
					return true, fmt.Sprintf("queue %q has in-flight work (item %s = %s)", queueLabel(q), it.BeadID, it.Status)
				}
			}
		}
	}
	return false, ""
}

// queueLabel returns a human label for a queue (Name, or QueueID fallback).
func queueLabel(q *queue.Queue) string {
	if q.Name != "" {
		return q.Name
	}
	if q.QueueID != "" {
		return q.QueueID
	}
	return "main"
}

// ---------------------------------------------------------------------------
// Output
// ---------------------------------------------------------------------------

// printPlanTable prints the dry-run plan as a path | class | action table.
func printPlanTable(plan []ReconcileItem, out io.Writer) {
	fmt.Fprintln(out, "harmonik sync-assets — plan (dry-run)")
	fmt.Fprintln(out, "")
	// Column widths.
	maxPath := len("PATH")
	for _, it := range plan {
		dest, ok := destFor(it.Path)
		label := dest
		if !ok {
			label = it.Path
		}
		if len(label) > maxPath {
			maxPath = len(label)
		}
	}
	fmt.Fprintf(out, "  %-*s  %-14s  %s\n", maxPath, "PATH", "CLASS", "ACTION")
	fmt.Fprintf(out, "  %-*s  %-14s  %s\n", maxPath, strings.Repeat("-", maxPath), "--------------", "------")
	// Sort by destination for stable, readable output.
	rows := make([]ReconcileItem, len(plan))
	copy(rows, plan)
	sort.Slice(rows, func(i, j int) bool {
		di, _ := destFor(rows[i].Path)
		dj, _ := destFor(rows[j].Path)
		return di < dj
	})
	for _, it := range rows {
		dest, ok := destFor(it.Path)
		label := dest
		if !ok {
			label = it.Path
		}
		fmt.Fprintf(out, "  %-*s  %-14s  %s\n", maxPath, label, it.Class, it.Action)
	}
}

// printApplySummary prints the applied/created/conflicted/skipped tallies and
// prominently lists any .harmonik-new conflicts the operator must reconcile.
func printApplySummary(outcomes []applyOutcome, out io.Writer) {
	var applied, created, conflicted, skipped int
	var conflicts []applyOutcome
	for _, o := range outcomes {
		switch {
		case o.conflic:
			conflicted++
			conflicts = append(conflicts, o)
		case o.created:
			created++
			applied++
		case o.written:
			applied++
		case o.skipped:
			skipped++
		}
	}
	fmt.Fprintln(out, "\nharmonik sync-assets — apply summary")
	fmt.Fprintf(out, "  applied:    %d  (created: %d)\n", applied, created)
	fmt.Fprintf(out, "  conflicted: %d\n", conflicted)
	fmt.Fprintf(out, "  skipped:    %d\n", skipped)
	if len(conflicts) > 0 {
		fmt.Fprintln(out, "\n  CONFLICTS — review and reconcile these by hand:")
		for _, c := range conflicts {
			fmt.Fprintf(out, "    - %s: %s\n", c.dest, c.note)
		}
	}
}

// commitSync stages and commits the applied changes. The orchestrator normally
// owns commits; --commit is the convenience path for the manual post-go-install
// step. Only runs when something was written.
func commitSync(projectDir string, outcomes []applyOutcome, stdout, stderr io.Writer) int {
	anyChange := false
	for _, o := range outcomes {
		if o.written || o.conflic {
			anyChange = true
			break
		}
	}
	if !anyChange {
		fmt.Fprintln(stdout, "harmonik sync-assets: nothing to commit (no files changed)")
		return 0
	}
	// Stage ONLY the paths this run touched — NEVER `git add -A`, which would
	// sweep up unrelated dirty files in the working tree. For each written or
	// conflicted outcome stage its dest, and (when a .harmonik-new was written)
	// the .harmonik-new sidecar too.
	for _, o := range outcomes {
		if o.dest == "" {
			continue
		}
		if !o.written && !o.conflic {
			continue
		}
		if code := gitAddPath(projectDir, o.dest, stdout, stderr); code != 0 {
			return code
		}
		if o.conflic {
			if code := gitAddPath(projectDir, o.dest+".harmonik-new", stdout, stderr); code != 0 {
				return code
			}
		}
	}
	msg := "chore(assets): sync embedded instruction assets via harmonik sync-assets"
	commit := exec.Command("git", "-C", projectDir, "commit", "-m", msg) //nolint:gosec // G204: projectDir operator-controlled
	commit.Stdout = stdout
	commit.Stderr = stderr
	if err := commit.Run(); err != nil {
		fmt.Fprintf(stderr, "harmonik sync-assets: git commit failed: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "harmonik sync-assets: committed asset sync")
	return 0
}

// gitAddPath stages exactly one project-relative path via `git add -- <path>`,
// using `--` so a path that looks like a flag is never misinterpreted. Returns
// 0 on success, 1 on failure (after printing the error).
func gitAddPath(projectDir, relPath string, stdout, stderr io.Writer) int {
	add := exec.Command("git", "-C", projectDir, "add", "--", relPath) //nolint:gosec // G204: projectDir + manifest-derived relPath
	add.Stdout = stdout
	add.Stderr = stderr
	if err := add.Run(); err != nil {
		fmt.Fprintf(stderr, "harmonik sync-assets: git add %s failed: %v\n", relPath, err)
		return 1
	}
	return 0
}

const syncAssetsUsage = `harmonik sync-assets — reconcile a project's instruction files with the binary's embedded assets

USAGE
  harmonik sync-assets [--project DIR] [--dry-run | --apply | --commit] [--force]

FLAGS
  --project DIR   Project directory (default: current working directory)
  --dry-run       (DEFAULT) Print the reconcile plan; write NOTHING.
  --apply         Execute the plan per the class policy below.
  --commit        --apply, then git-commit the result.
  --force         Bypass the daemon-lull gate (apply even while the daemon dispatches).

WHAT IT DOES (per asset class)
  Managed (.claude/skills/*):
    fast-forward/create → overwrite from embed; CONFLICT → write <file>.harmonik-new
    (the edited file is NEVER clobbered).
  ManagedRegion (AGENTS.md):
    update only the <!-- BEGIN harmonik:managed … --> … <!-- END --> region(s);
    project text outside the markers is preserved. Markers missing → conflict
    (.harmonik-new), file untouched.
  ContentOwned (.harmonik/context/*, HANDOFF.md):
    create if absent; fast-forward refreshes only the <!-- TIER: … --> header,
    NEVER the body; conflict is reported, file untouched.
  Scaffold (AGENT_INDEX.md, STATUS.md):
    written once if absent; otherwise left to the project.

SAFETY
  --dry-run is the default and writes nothing. --apply refuses while the daemon is
  actively dispatching (editing the main worktree mid-dispatch fails in-flight
  beads); pass --force to override. After apply, .harmonik/assets.lock is restamped.

EXIT CODES
   0  Success (dry-run printed, or apply completed; conflicts are reported, not errors)
   1  Argument, precondition, or I/O error
   3  Daemon-lull gate refused (daemon dispatching, no --force)

EXAMPLES
  harmonik sync-assets                       # dry-run plan
  harmonik sync-assets --apply               # apply (refuses if daemon dispatching)
  harmonik sync-assets --apply --force       # apply even while dispatching
  harmonik sync-assets --commit              # apply + git commit
  harmonik sync-assets --project /path/to/p  # target another project
`
