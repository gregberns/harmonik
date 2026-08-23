package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/queue"
)

const (
	agentsManagedBeginPrefix = "<!-- BEGIN harmonik:managed"
	agentsManagedEndMarker   = "<!-- END harmonik:managed -->"
)

const contentTierHeaderOpen = "<!-- TIER:"

func runSyncAssetsSubcommand(args []string) int {
	return runSyncAssets(args, os.Stdout, os.Stderr)
}

func destFor(embedPath string) (string, bool) {
	rel := strings.TrimPrefix(embedPath, assetEmbedRoot+"/")
	switch {
	case strings.HasPrefix(rel, "skills/"):
		return filepath.Join(".claude", "skills", strings.TrimPrefix(rel, "skills/")), true
	case rel == "templates/AGENTS.template.md":
		return "AGENTS.md", true
	case strings.HasPrefix(rel, "context/"):
		base := strings.TrimPrefix(rel, "context/")
		if base == "HANDOFF.md.tmpl" {
			return "HANDOFF.md", true
		}
		return filepath.Join(".harmonik", "context", strings.TrimSuffix(base, ".tmpl")), true
	case strings.HasPrefix(rel, "scaffolds/"):
		return strings.TrimPrefix(rel, "scaffolds/"), true
	default:
		return "", false
	}
}

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

func buildDiskHashes(projectDir string, m Manifest) (map[string]string, error) {
	disk := make(map[string]string, len(m.Files))
	for _, f := range m.Files {
		dest, ok := destFor(f.Path)
		if !ok {
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
			if syncAssetsWritef(stdout, "%s", syncAssetsUsage) != nil {
				return 1
			}
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
			if syncAssetsWritef(stderr, "harmonik sync-assets: unrecognised argument %q\n", args[i]) != nil {
				return 1
			}
			if syncAssetsWritef(stderr, "%s", syncAssetsUsage) != nil {
				return 1
			}
			return 1
		}
	}
	if apply {
		dryRun = false
	}

	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			if syncAssetsWritef(stderr, "harmonik sync-assets: cannot determine working directory: %v\n", err) != nil {
				return 1
			}
			return 1
		}
		projectDir = wd
	}
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		if syncAssetsWritef(stderr, "harmonik sync-assets: cannot resolve project path %q: %v\n", projectDir, err) != nil {
			return 1
		}
		return 1
	}
	projectDir = absProject
	if _, err := os.Stat(projectDir); err != nil {
		if syncAssetsWritef(stderr, "harmonik sync-assets: project directory %q does not exist or is not accessible: %v\n", projectDir, err) != nil {
			return 1
		}
		return 1
	}

	manifest, err := BuildManifest()
	if err != nil {
		if syncAssetsWritef(stderr, "harmonik sync-assets: build manifest: %v\n", err) != nil {
			return 1
		}
		return 1
	}
	lock, err := ReadLock(projectDir)
	if err != nil {
		if syncAssetsWritef(stderr, "harmonik sync-assets: read lock: %v\n", err) != nil {
			return 1
		}
		return 1
	}
	disk, err := buildDiskHashes(projectDir, manifest)
	if err != nil {
		if syncAssetsWritef(stderr, "harmonik sync-assets: hash project files: %v\n", err) != nil {
			return 1
		}
		return 1
	}
	plan := Reconcile(manifest, lock, disk)

	if dryRun {
		if err := printPlanTable(plan, stdout); err != nil {
			if syncAssetsWritef(stderr, "harmonik sync-assets: write plan: %v\n", err) != nil {
				return 1
			}
			return 1
		}
		if _, err := fmt.Fprintln(stdout, "\nharmonik sync-assets: dry-run — no files written. Re-run with --apply to update."); err != nil {
			if syncAssetsWritef(stderr, "harmonik sync-assets: write dry-run summary: %v\n", err) != nil {
				return 1
			}
			return 1
		}
		return 0
	}

	if !force {
		dispatching, reason, gerr := daemonDispatchGate(projectDir)
		if gerr != nil {
			if syncAssetsWritef(stderr, "harmonik sync-assets: daemon-lull check failed: %v\n", gerr) != nil {
				return 1
			}
			return 1
		}
		if dispatching {
			if syncAssetsWritef(stderr, "harmonik sync-assets: REFUSING to apply — the daemon is actively dispatching (%s).\n", reason) != nil {
				return 1
			}
			if syncAssetsWritef(stderr, "  A merge refreshes the main working tree and can overwrite what you write here.\n") != nil {
				return 1
			}
			if syncAssetsWritef(stderr, "  Wait for a lull (no active queue items), or re-run with --force to override.\n") != nil {
				return 1
			}
			return 3
		}
	}

	outcomes, code := applyPlan(projectDir, manifest, plan, stdout, stderr)
	if code != 0 {
		return code
	}

	newLock := lockFromOutcomes(lock, outcomes)
	if err := WriteLock(projectDir, newLock); err != nil {
		if syncAssetsWritef(stderr, "harmonik sync-assets: write lock: %v\n", err) != nil {
			return 1
		}
		return 1
	}

	if err := printApplySummary(outcomes, stdout); err != nil {
		if syncAssetsWritef(stderr, "harmonik sync-assets: write apply summary: %v\n", err) != nil {
			return 1
		}
		return 1
	}

	if commit {
		if code := commitSync(projectDir, outcomes, stdout, stderr); code != 0 {
			return code
		}
	}
	return 0
}

func syncAssetsWritef(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err
}

func applyPlan(projectDir string, m Manifest, plan []ReconcileItem, stdout, stderr io.Writer) ([]applyOutcome, int) {
	outcomes := make([]applyOutcome, 0, len(plan))
	for _, item := range plan {
		out := applyOutcome{item: item}
		dest, hasDest := destFor(item.Path)
		out.dest = dest

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
			if syncAssetsWritef(stderr, "harmonik sync-assets: read embedded asset %s: %v\n", item.Path, rerr) != nil {
				return outcomes, 1
			}
			return outcomes, 1
		}

		switch item.Class {
		case Managed:
			code := applyManaged(full, dest, embedData, item.Action, &out, stderr)
			if code != 0 {
				return outcomes, code
			}
		case ManagedRegion:
			code := applyManagedRegion(projectDir, full, dest, embedData, item.Action, &out, stderr)
			if code != 0 {
				return outcomes, code
			}
		case ContentOwned:
			code := applyContentOwned(full, dest, embedData, item.Action, &out, stderr)
			if code != 0 {
				return outcomes, code
			}
		case Scaffold:
			code := applyScaffold(full, dest, embedData, item.Action, &out, stderr)
			if code != 0 {
				return outcomes, code
			}
		default:
			out.skipped = true
			out.note = "unclassified; left untouched"
		}
		outcomes = append(outcomes, out)
	}
	return outcomes, 0
}

func lockFromOutcomes(prior Lock, outcomes []applyOutcome) Lock {
	out := Lock{
		FormatVersion: LockFormatVersion,
		Files:         make(map[string]LockEntry, len(outcomes)),
	}
	for _, o := range outcomes {
		path := o.item.Path
		switch {
		case o.conflic || o.item.Action == ActionConflict:
			if pe, ok := prior.Files[path]; ok {
				out.Files[path] = LockEntry{Path: path, Sha256: pe.Sha256}
			}
		case o.item.Action == ActionLeave:
		case o.written || o.item.Action == ActionSkip || o.item.Action == ActionFastForward:
			if o.item.EmbedSha != "" {
				out.Files[path] = LockEntry{Path: path, Sha256: o.item.EmbedSha}
			} else if pe, ok := prior.Files[path]; ok {
				out.Files[path] = LockEntry{Path: path, Sha256: pe.Sha256}
			}
		default:
			if pe, ok := prior.Files[path]; ok {
				out.Files[path] = LockEntry{Path: path, Sha256: pe.Sha256}
			}
		}
	}
	return out
}

func applyManaged(full, dest string, embedData []byte, action Action, out *applyOutcome, stderr io.Writer) int {
	switch action {
	case ActionFastForward, ActionCreate:
		if err := writeFileEnsureDir(full, dest, embedData); err != nil {
			if syncAssetsWritef(stderr, "harmonik sync-assets: write %s: %v\n", dest, err) != nil {
				return 1
			}
			return 1
		}
		out.written = true
		out.created = action == ActionCreate
		out.note = "overwritten from embed"
	case ActionConflict:
		newPath := full + ".harmonik-new"
		if err := writeFileEnsureDir(newPath, dest, embedData); err != nil {
			if syncAssetsWritef(stderr, "harmonik sync-assets: write %s: %v\n", dest+".harmonik-new", err) != nil {
				return 1
			}
			return 1
		}
		out.conflic = true
		out.note = "CONFLICT: local edits — embed written to " + dest + ".harmonik-new (original untouched)"
	case ActionSkip, ActionLeave:
		if syncAssetsWritef(stderr, "harmonik sync-assets: internal error: action %q reached applyManaged for %s\n", action, dest) != nil {
			return 1
		}
		return 1
	}
	return 0
}

func applyManagedRegion(projectDir, full, dest string, embedData []byte, action Action, out *applyOutcome, stderr io.Writer) int {
	rendered := renderAgentsTemplate(string(embedData), projectDir)

	if action == ActionCreate {
		if err := writeFileEnsureDir(full, dest, []byte(rendered)); err != nil {
			if syncAssetsWritef(stderr, "harmonik sync-assets: write %s: %v\n", dest, err) != nil {
				return 1
			}
			return 1
		}
		out.written = true
		out.created = true
		out.note = "router created from template"
		return 0
	}

	current, rerr := os.ReadFile(full) //nolint:gosec // G304: full is under the resolved project dir
	if rerr != nil {
		if os.IsNotExist(rerr) {
			if err := writeFileEnsureDir(full, dest, []byte(rendered)); err != nil {
				if syncAssetsWritef(stderr, "harmonik sync-assets: write %s: %v\n", dest, err) != nil {
					return 1
				}
				return 1
			}
			out.written = true
			out.created = true
			out.note = "router (re)created from template"
			return 0
		}
		if syncAssetsWritef(stderr, "harmonik sync-assets: read %s: %v\n", dest, rerr) != nil {
			return 1
		}
		return 1
	}

	merged, ok := spliceManagedRegions(string(current), rendered)
	if !ok {
		newPath := full + ".harmonik-new"
		if err := writeFileEnsureDir(newPath, dest, []byte(rendered)); err != nil {
			if syncAssetsWritef(stderr, "harmonik sync-assets: write %s: %v\n", dest+".harmonik-new", err) != nil {
				return 1
			}
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
	if err := writeFileEnsureDir(full, dest, []byte(merged)); err != nil {
		if syncAssetsWritef(stderr, "harmonik sync-assets: write %s: %v\n", dest, err) != nil {
			return 1
		}
		return 1
	}
	out.written = true
	out.note = "managed region updated; project deltas preserved"
	return 0
}

func applyContentOwned(full, dest string, embedData []byte, action Action, out *applyOutcome, stderr io.Writer) int {
	switch action {
	case ActionCreate:
		if err := writeFileEnsureDir(full, dest, embedData); err != nil {
			if syncAssetsWritef(stderr, "harmonik sync-assets: write %s: %v\n", dest, err) != nil {
				return 1
			}
			return 1
		}
		out.written = true
		out.created = true
		out.note = "created from template"
	case ActionFastForward:
		current, rerr := os.ReadFile(full) //nolint:gosec // G304: under resolved project dir
		if rerr != nil {
			if os.IsNotExist(rerr) {
				if err := writeFileEnsureDir(full, dest, embedData); err != nil {
					if syncAssetsWritef(stderr, "harmonik sync-assets: write %s: %v\n", dest, err) != nil {
						return 1
					}
					return 1
				}
				out.written = true
				out.created = true
				out.note = "created from template"
				return 0
			}
			if syncAssetsWritef(stderr, "harmonik sync-assets: read %s: %v\n", dest, rerr) != nil {
				return 1
			}
			return 1
		}
		merged, ok := replaceTierHeader(string(current), string(embedData))
		if !ok {
			out.skipped = true
			out.note = "header region not found; body owned — left untouched"
			return 0
		}
		if merged == string(current) {
			out.skipped = true
			out.note = "header already current"
			return 0
		}
		if err := writeFileEnsureDir(full, dest, []byte(merged)); err != nil {
			if syncAssetsWritef(stderr, "harmonik sync-assets: write %s: %v\n", dest, err) != nil {
				return 1
			}
			return 1
		}
		out.written = true
		out.note = "TIER header refreshed; body preserved"
	case ActionConflict:
		out.skipped = true
		out.note = "CONFLICT on content-owned file — body is project-owned; left untouched (reconcile manually)"
	case ActionSkip, ActionLeave:
		if syncAssetsWritef(stderr, "harmonik sync-assets: internal error: action %q reached applyContentOwned for %s\n", action, dest) != nil {
			return 1
		}
		return 1
	}
	return 0
}

func applyScaffold(full, dest string, embedData []byte, action Action, out *applyOutcome, stderr io.Writer) int {
	if action == ActionCreate {
		if err := writeFileEnsureDir(full, dest, embedData); err != nil {
			if syncAssetsWritef(stderr, "harmonik sync-assets: write %s: %v\n", dest, err) != nil {
				return 1
			}
			return 1
		}
		out.written = true
		out.created = true
		out.note = "scaffold written"
		return 0
	}
	out.skipped = true
	out.note = "scaffold present; left untouched"
	return 0
}

func renderAgentsTemplate(tmpl, projectDir string) string {
	rendered := strings.ReplaceAll(tmpl, "$PROJECT_DIR", projectDir)
	return rendered
}

func spliceManagedRegions(current, template string) (string, bool) {
	curRegions := findManagedRegions(current)
	tplRegions := findManagedRegions(template)
	if len(curRegions) == 0 || len(tplRegions) == 0 {
		return current, false
	}
	if len(curRegions) != len(tplRegions) {
		return current, false
	}
	merged := current
	for i := len(curRegions) - 1; i >= 0; i-- {
		c := curRegions[i]
		t := tplRegions[i]
		merged = merged[:c.start] + template[t.start:t.end] + merged[c.end:]
	}
	return merged, true
}

type region struct {
	start int
	end   int
}

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
			return nil
		}
		eend := bstart + ei + len(agentsManagedEndMarker)
		if eend < len(s) && s[eend] == '\n' {
			eend++
		}
		regions = append(regions, region{start: bstart, end: eend})
		idx = eend
	}
	return regions
}

func replaceTierHeader(current, template string) (string, bool) {
	ch, cok := tierHeaderSpan(current)
	th, tok := tierHeaderSpan(template)
	if !cok || !tok {
		return current, false
	}
	merged := template[th.start:th.end] + current[ch.end:]
	return merged, true
}

func tierHeaderSpan(s string) (region, bool) {
	open := strings.Index(s, contentTierHeaderOpen)
	if open < 0 {
		return region{}, false
	}
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

const claudeAssetDirMode fs.FileMode = 0o755

func destUnderHarmonik(dest string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(dest), "/") {
		if seg == ".harmonik" {
			return true
		}
	}
	return false
}

func writeFileEnsureDir(path, dest string, data []byte) error {
	dir := filepath.Dir(path)
	if destUnderHarmonik(dest) {
		if err := os.MkdirAll(dir, core.HarmonikDirMode); err != nil {
			return err
		}
	} else if err := os.MkdirAll(dir, claudeAssetDirMode); err != nil { //dirmode:allow .claude/ + repo-root asset tree, not .harmonik state — matches init's provisionSkills
		return err
	}
	//nolint:gosec // G306: 0644 matches init's file-mode conventions
	return os.WriteFile(path, data, 0o644)
}

func daemonDispatchGate(projectDir string) (dispatching bool, reason string, err error) {
	up := daemonSocketUp(projectDir)
	if !up {
		return false, "", nil
	}
	names, err := queue.EnumerateQueueNames(projectDir)
	if err != nil {
		return false, "", fmt.Errorf("enumerate queues: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	queues := make([]*queue.Queue, 0, len(names))
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

func daemonSocketUp(projectDir string) bool {
	sockPath := filepath.Join(projectDir, ".harmonik", "daemon.sock")
	if _, err := os.Stat(sockPath); err != nil {
		return false
	}
	conn, err := (&net.Dialer{Timeout: 2 * time.Second}).Dial("unix", sockPath)
	if err != nil {
		return false
	}
	if closeErr := conn.Close(); closeErr != nil {
		fmt.Fprintf(os.Stderr, "harmonik sync-assets: close daemon probe connection: %v\n", closeErr)
	}
	return true
}

func dispatchingQueue(queues []*queue.Queue) (dispatching bool, reason string) {
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

func queueLabel(q *queue.Queue) string {
	if q.Name != "" {
		return q.Name
	}
	if q.QueueID != "" {
		return q.QueueID
	}
	return "main"
}

func printPlanTable(plan []ReconcileItem, out io.Writer) error {
	p := syncAssetsPrinter{out: out}
	p.println("harmonik sync-assets — plan (dry-run)")
	p.println("")
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
	p.printf("  %-*s  %-14s  %s\n", maxPath, "PATH", "CLASS", "ACTION")
	p.printf("  %-*s  %-14s  %s\n", maxPath, strings.Repeat("-", maxPath), "--------------", "------")
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
		p.printf("  %-*s  %-14s  %s\n", maxPath, label, it.Class, it.Action)
	}
	return p.err
}

func printApplySummary(outcomes []applyOutcome, out io.Writer) error {
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
	p := syncAssetsPrinter{out: out}
	p.println("\nharmonik sync-assets — apply summary")
	p.printf("  applied:    %d  (created: %d)\n", applied, created)
	p.printf("  conflicted: %d\n", conflicted)
	p.printf("  skipped:    %d\n", skipped)
	if len(conflicts) > 0 {
		p.println("\n  CONFLICTS — review and reconcile these by hand:")
		for _, c := range conflicts {
			p.printf("    - %s: %s\n", c.dest, c.note)
		}
	}
	return p.err
}

type syncAssetsPrinter struct {
	out io.Writer
	err error
}

func (p *syncAssetsPrinter) printf(format string, args ...any) {
	if p.err != nil {
		return
	}
	_, p.err = fmt.Fprintf(p.out, format, args...)
}

func (p *syncAssetsPrinter) println(args ...any) {
	if p.err != nil {
		return
	}
	_, p.err = fmt.Fprintln(p.out, args...)
}

func commitSync(projectDir string, outcomes []applyOutcome, stdout, stderr io.Writer) int {
	anyChange := false
	for _, o := range outcomes {
		if o.written || o.conflic {
			anyChange = true
			break
		}
	}
	if !anyChange {
		if syncAssetsWritef(stdout, "harmonik sync-assets: nothing to commit (no files changed)\n") != nil {
			return 1
		}
		return 0
	}
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
	commit := exec.CommandContext(context.Background(), "git", "-C", projectDir, "commit", "-m", msg)
	commit.Stdout = stdout
	commit.Stderr = stderr
	if err := commit.Run(); err != nil {
		if syncAssetsWritef(stderr, "harmonik sync-assets: git commit failed: %v\n", err) != nil {
			return 1
		}
		return 1
	}
	if syncAssetsWritef(stdout, "harmonik sync-assets: committed asset sync\n") != nil {
		return 1
	}
	return 0
}

func gitAddPath(projectDir, relPath string, stdout, stderr io.Writer) int {
	add := exec.CommandContext(context.Background(), "git", "-C", projectDir, "add", "--", relPath)
	add.Stdout = stdout
	add.Stderr = stderr
	if err := add.Run(); err != nil {
		if syncAssetsWritef(stderr, "harmonik sync-assets: git add %s failed: %v\n", relPath, err) != nil {
			return 1
		}
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
