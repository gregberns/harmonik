package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// WorkerReportPayload is the typed event payload for the worker_report event
// (worker-report Phase 1, §Data model).
//
// It is a periodic worker resource + problem snapshot. The resource fields answer
// "at what point are we maxing out?"; the Problems flags answer "are there
// issues?". No time-series store exists in Phase 1 — each sample is emitted as an
// event, and the event log is the history mined later to pick a real max.
//
// Durability class: O (ordinary — operator observability). Event: "worker_report".
type WorkerReportPayload struct {
	// WorkerName is the name of the worker the snapshot describes.
	WorkerName string `json:"worker_name"`
	// SampledAt is the RFC 3339 UTC wall-clock timestamp at sample time.
	SampledAt string `json:"sampled_at"`

	// Load1 is the 1-minute load average.
	Load1 float64 `json:"load1"`
	// Load5 is the 5-minute load average.
	Load5 float64 `json:"load5"`
	// NCPU is the worker's CPU count, so load is interpretable.
	NCPU int `json:"ncpu"`
	// MemTotalMB is total physical memory in MB.
	MemTotalMB int64 `json:"mem_total_mb"`
	// MemFreeMB is available memory (free + inactive pages) in MB.
	MemFreeMB int64 `json:"mem_free_mb"`
	// SwapUsedMB is swap currently in use in MB — the decisive "really out of
	// headroom" signal.
	SwapUsedMB int64 `json:"swap_used_mb"`
	// DiskFreeMB is free disk on the worktree volume (repo_path) in MB.
	DiskFreeMB int64 `json:"disk_free_mb"`
	// ClaudeProcs is the count of running `claude --session-id` processes.
	ClaudeProcs int `json:"claude_procs"`
	// WorktreeCount is the number of `git worktree list` entries on the worker's
	// repo (including the main worktree). Used to detect worktree_leak: a count
	// above the expected baseline means run worktrees were not cleaned up. WR4
	// added the collector `worktrees=` line that feeds this.
	WorktreeCount int `json:"worktree_count"`

	// "Are there issues?" — problem flags (presence = problem detected),
	// e.g. "orphaned_claude", "worktree_leak", "disk_pressure".
	Problems []string `json:"problems,omitempty"`
}

func init() {
	if err := core.RegisterEventType(core.EventTypeWorkerReport, func() core.EventPayload { return &WorkerReportPayload{} }); err != nil {
		panic("workers: init: register worker_report: " + err.Error())
	}
	if err := core.RegisterPayloadCompatEntry(core.PayloadCompatEntry{
		TypeName:          core.EventTypeWorkerReport,
		CurrentVersion:    1,
		CompatWindowHolds: true,
		AdditiveOnly:      true,
	}); err != nil {
		panic("workers: init: register worker_report compatibility: " + err.Error()) //nolint:forbidigo // init-time registry wiring: a duplicate or bad registration is a build-time bug, and there is no caller to return an error to.
	}
}

// darwinCollectorScript builds the inline `sh -c` collector body for a worker,
// substituting repoPath into the `df` target. It mirrors the spec's collector
// (§"The collector") line-for-line and adds an authoritative `pagesize=` line
// (the WR1 TODO) so MemFreeMB is correct on Apple Silicon page sizes.
//
// Each line is `key=value`, order-independent, parsed by parseWorkerReport. The
// `vmstat<<` line carries the raw `vm_stat` block; `pagesize=` is emitted last so
// the explicit sysctl value wins over any vm_stat-header scrape.
func darwinCollectorScript(repoPath string) string {
	return strings.Join([]string{
		`echo "load=$(sysctl -n vm.loadavg | tr -d '{}')"`,
		`echo "ncpu=$(sysctl -n hw.ncpu)"`,
		`echo "memtotal=$(sysctl -n hw.memsize)"`,
		`echo "vmstat<<$(vm_stat)"`,
		`echo "swap=$(sysctl -n vm.swapusage)"`,
		`echo "disk=$(df -m '` + repoPath + `' | tail -1)"`,
		// This line searches for text that the line ITSELF contains, because the
		// whole collector body is one `sh -c` argument. It counts correctly
		// anyway: macOS pgrep hides the caller's own ancestors, and from this
		// position the shell is an ancestor. Measured — with a decoy process
		// carrying that argv it returns 1, not 2.
		//
		// So this is correct by ARRANGEMENT, not by design, and a rearrangement
		// breaks it silently. The same pattern as the FIRST command of an
		// `sh -c` pipeline DOES match the parent shell. Do not move it, and do
		// not copy the shape to a new call site. scripts/run-full.sh hit this
		// and wrote down the durable fix: exclude your own invocation by PID SET
		// — the process, its ancestors, and its process group — never by
		// matching command-line text. The linux sibling below does NOT get the
		// ancestor-hiding behaviour; see hk-50cp2.
		`echo "claude=$(pgrep -f 'claude --session-id' | wc -l | tr -d ' ')"`,
		// worktree_leak detection (WR4): count `git worktree list` entries on the
		// worker repo. The `^worktree ` porcelain prefix is one line per worktree
		// (the main worktree plus every linked run worktree). repoPath is quoted to
		// tolerate spaces; a `|| true` keeps a non-git path from failing the whole
		// collector (it just yields 0).
		`echo "worktrees=$(git -C '` + repoPath + `' worktree list --porcelain 2>/dev/null | grep -c '^worktree ' || true)"`,
		`echo "pagesize=$(sysctl -n hw.pagesize)"`,
	}, "\n")
}

func linuxCollectorScript(repoPath string) string {
	return strings.Join([]string{
		`echo "load=$(awk '{print $1, $2, $3}' /proc/loadavg)"`,
		`echo "ncpu=$(grep -c '^processor' /proc/cpuinfo 2>/dev/null || echo 1)"`,
		`echo "memtotal=$(awk '/^MemTotal:/{print $2*1024}' /proc/meminfo)"`,
		`echo "memfree=$(awk '/^MemAvailable:/{print $2*1024}' /proc/meminfo)"`,
		`echo "swapused=$(awk 'BEGIN{st=0;sf=0}/^SwapTotal:/{st=$2}/^SwapFree:/{sf=$2}END{print (st-sf)*1024}' /proc/meminfo)"`,
		`echo "disk=$(df -m '` + repoPath + `' | tail -1)"`,
		`echo "claude=$(ps 2>/dev/null | grep 'claude --session-id' | grep -v grep | wc -l | tr -d ' ')"`,
		`echo "worktrees=$(git -C '` + repoPath + `' worktree list --porcelain 2>/dev/null | grep -c '^worktree ' || echo 0)"`,
	}, "\n")
}

// CollectReport runs the inline darwin resource collector on worker w via runner,
// parses its output into a WorkerReportPayload, stamps WorkerName + SampledAt
// (RFC 3339 UTC), and emits a worker_report event via emit.
//
// It is the resource-snapshot sibling of RunHealthCheck: same runner
// (tmux.CommandRunner → SSH in production), same EmitFunc contract (nil emit
// suppresses emission without error), same `runner.Command(ctx, ...)` +
// bytes.Buffer stdout-capture shape.
//
// CollectReport derives Problems (WR4) via deriveProblems before emit: it
// cross-checks ClaudeProcs against reg.InFlight (orphaned_claude), DiskFreeMB
// against diskFloorMB (disk_pressure), and WorktreeCount against the worktree
// baseline (worktree_leak), derived from w.MaxSlots (1 main worktree + max_slots
// concurrent run worktrees). reg may be nil — deriveProblems treats a nil
// Registry as "no in-flight runs" so the orphaned_claude check still fires.
// Passing diskFloorMB <= 0 selects the package default (DefaultDiskFloorMB);
// WR3 will thread a configured floor through here.
//
// On a runner failure the collector error is returned and no event is emitted.
//
// Bead refs: hk-ec9v (WR2), hk-b2f9 (WR4).
func CollectReport(ctx context.Context, runner tmux.CommandRunner, w Worker, reg *Registry, diskFloorMB int64, emit EmitFunc) (WorkerReportPayload, error) {
	var script string
	if w.OS == "linux" {
		script = linuxCollectorScript(w.RepoPath)
	} else {
		script = darwinCollectorScript(w.RepoPath)
	}
	cmd := runner.Command(ctx, "sh", "-c", script)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return WorkerReportPayload{}, fmt.Errorf("CollectReport: %s: collector failed: %w (output: %q)", w.Name, err, out.String())
	}

	p, err := parseWorkerReport(out.String())
	if err != nil {
		return WorkerReportPayload{}, fmt.Errorf("CollectReport: %s: %w", w.Name, err)
	}

	p.WorkerName = w.Name
	p.SampledAt = time.Now().UTC().Format(time.RFC3339)
	p.Problems = deriveProblems(p, reg, diskFloorMB, w.MaxSlots)

	emitWorkerReport(ctx, p, emit)
	return p, nil
}

// DefaultDiskFloorMB is the disk_pressure floor used when CollectReport is
// called with diskFloorMB <= 0: DiskFreeMB below this many MB flags
// disk_pressure. It is a sensible default (2 GB) for Phase 1; WR3 will make the
// floor configurable per worker via workers.yaml. Kept a package const here —
// NOT hardcoded deep in the derivation — so the config knob can override it.
const DefaultDiskFloorMB int64 = 2048

// DefaultMaxSlotsFallback is the max-slots assumed for the worktree_leak baseline
// when a worker's MaxSlots is unset (<= 0). It is a sane mid-range concurrency so
// the derived baseline (1 + maxSlots) stays >= 2 — never so low that a single
// legitimate run worktree trips the leak signal.
const DefaultMaxSlotsFallback = 4

func worktreeBaseline(maxSlots int) int {
	if maxSlots <= 0 {
		maxSlots = DefaultMaxSlotsFallback
	}
	baseline := 1 + maxSlots
	if baseline < 2 {
		baseline = 2
	}
	return baseline
}

const (
	problemOrphanedClaude = "orphaned_claude"
	problemDiskPressure   = "disk_pressure"
	problemWorktreeLeak   = "worktree_leak"
)

func deriveProblems(rep WorkerReportPayload, reg *Registry, diskFloorMB int64, maxSlots int) []string {
	var problems []string

	inFlight := 0
	if reg != nil {
		inFlight = reg.InFlight()
	}
	if rep.ClaudeProcs > 0 && inFlight == 0 {
		problems = append(problems, problemOrphanedClaude)
	}

	floor := diskFloorMB
	if floor <= 0 {
		floor = DefaultDiskFloorMB
	}
	if rep.DiskFreeMB < floor {
		problems = append(problems, problemDiskPressure)
	}

	if rep.WorktreeCount > worktreeBaseline(maxSlots) {
		problems = append(problems, problemWorktreeLeak)
	}

	return problems
}

func emitWorkerReport(ctx context.Context, p WorkerReportPayload, emit EmitFunc) {
	if emit == nil {
		return
	}
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	if err := emit(ctx, core.EventTypeWorkerReport, b); err != nil {
		slog.ErrorContext(ctx, "worker event emit failed", "event_type", core.EventTypeWorkerReport, "error", err)
	}
}

// defaultDarwinPageSize is the fallback vm_stat page size (bytes) used only when
// the collector's vm_stat header does not carry an authoritative page size. It is
// 16384 — correct for the actual Apple Silicon worker (gb-mbp) — NOT the old 4096
// x86 assumption, which under-counted MemFreeMB 4×. The real page size is parsed
// from the vm_stat header line ("Mach Virtual Memory Statistics: (page size of N
// bytes)"); this constant is only the safety net.
//
// WR2 resolved the WR1 TODO: the collector now emits an explicit
// `pagesize=$(sysctl -n hw.pagesize)` line (see darwinCollectorScript), so page
// size is authoritative rather than inferred from the vm_stat header. This
// constant remains the last-resort safety net (header absent AND no pagesize=
// line, e.g. a future non-darwin collector).
const defaultDarwinPageSize = 16384

const bytesPerMB = 1024 * 1024

func parseWorkerReport(raw string) (WorkerReportPayload, error) {
	var p WorkerReportPayload

	var (
		sawLoad    bool
		sawMemFree bool // set when memfree= key seen (Linux); prevents vm_stat overwrite
		freePg     int64
		inactPg    int64
		inVMStat   bool
		// pageSize is the vm_stat page size in bytes. The collector now emits an
		// authoritative `pagesize=` line (sysctl -n hw.pagesize, WR2); when that is
		// absent it is parsed from the vm_stat header ("page size of N bytes"); and
		// until either is seen it holds the fallback (16384, correct for the actual
		// Apple Silicon worker).
		pageSize int64 = defaultDarwinPageSize
	)

	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			inVMStat = false
			continue
		}

		if inVMStat {
			if v, ok := parseVMStatPageSize(trimmed); ok {
				pageSize = v
				continue
			}
			if v, ok := parseVMStatPages(trimmed, "Pages free:"); ok {
				freePg = v
				continue
			}
			if v, ok := parseVMStatPages(trimmed, "Pages inactive:"); ok {
				inactPg = v
				continue
			}
			if !strings.Contains(trimmed, "=") {
				continue
			}
			inVMStat = false
		}

		if strings.HasPrefix(trimmed, "vmstat<<") {
			inVMStat = true
			after := strings.TrimSpace(strings.TrimPrefix(trimmed, "vmstat<<"))
			if v, ok := parseVMStatPageSize(after); ok {
				pageSize = v
			}
			if v, ok := parseVMStatPages(after, "Pages free:"); ok {
				freePg = v
			} else if v, ok := parseVMStatPages(after, "Pages inactive:"); ok {
				inactPg = v
			}
			continue
		}

		key, val, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

		switch key {
		case "pagesize":
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return WorkerReportPayload{}, fmt.Errorf("parseWorkerReport: pagesize: %w", err)
			}
			if n > 0 {
				pageSize = n
			}
		case "memfree":
			b, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return WorkerReportPayload{}, fmt.Errorf("parseWorkerReport: memfree: %w", err)
			}
			p.MemFreeMB = b / bytesPerMB
			sawMemFree = true
		case "swapused":
			b, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return WorkerReportPayload{}, fmt.Errorf("parseWorkerReport: swapused: %w", err)
			}
			p.SwapUsedMB = b / bytesPerMB
		case "load":
			l1, l5, err := parseLoadavg(val)
			if err != nil {
				return WorkerReportPayload{}, fmt.Errorf("parseWorkerReport: load: %w", err)
			}
			p.Load1, p.Load5 = l1, l5
			sawLoad = true
		case "ncpu":
			n, err := strconv.Atoi(val)
			if err != nil {
				return WorkerReportPayload{}, fmt.Errorf("parseWorkerReport: ncpu: %w", err)
			}
			p.NCPU = n
		case "memtotal":
			b, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return WorkerReportPayload{}, fmt.Errorf("parseWorkerReport: memtotal: %w", err)
			}
			p.MemTotalMB = b / bytesPerMB
		case "swap":
			used, err := parseSwapUsed(val)
			if err != nil {
				return WorkerReportPayload{}, fmt.Errorf("parseWorkerReport: swap: %w", err)
			}
			p.SwapUsedMB = used
		case "disk":
			avail, err := parseDFAvailable(val)
			if err != nil {
				return WorkerReportPayload{}, fmt.Errorf("parseWorkerReport: disk: %w", err)
			}
			p.DiskFreeMB = avail
		case "claude":
			n, err := strconv.Atoi(val)
			if err != nil {
				return WorkerReportPayload{}, fmt.Errorf("parseWorkerReport: claude: %w", err)
			}
			p.ClaudeProcs = n
		case "worktrees":
			if val == "" {
				p.WorktreeCount = 0
				break
			}
			n, err := strconv.Atoi(val)
			if err != nil {
				return WorkerReportPayload{}, fmt.Errorf("parseWorkerReport: worktrees: %w", err)
			}
			p.WorktreeCount = n
		}
	}

	if !sawLoad {
		return WorkerReportPayload{}, fmt.Errorf("parseWorkerReport: missing required load= line")
	}

	if !sawMemFree {
		p.MemFreeMB = (freePg + inactPg) * pageSize / bytesPerMB
	}

	return p, nil
}

func parseLoadavg(val string) (load1, load5 float64, err error) {
	val = strings.TrimSpace(val)
	val = strings.Trim(val, "{}")
	fields := strings.Fields(val)
	if len(fields) < 2 {
		return 0, 0, fmt.Errorf("expected at least 2 fields, got %q", val)
	}
	load1, err = strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("load1 %q: %w", fields[0], err)
	}
	load5, err = strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return 0, 0, fmt.Errorf("load5 %q: %w", fields[1], err)
	}
	return load1, load5, nil
}

func parseVMStatPages(line, label string) (int64, bool) {
	if !strings.HasPrefix(line, label) {
		return 0, false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, label))
	rest = strings.TrimSuffix(rest, ".")
	rest = strings.TrimSpace(rest)
	v, err := strconv.ParseInt(rest, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func parseVMStatPageSize(line string) (int64, bool) {
	const marker = "page size of "
	idx := strings.Index(line, marker)
	if idx < 0 {
		return 0, false
	}
	rest := line[idx+len(marker):]
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return 0, false
	}
	v, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
}

func parseSwapUsed(val string) (int64, error) {
	fields := strings.Fields(val)
	for i, f := range fields {
		if f == "used" {
			if i+2 < len(fields) {
				mb, err := parseSwapMagnitude(fields[i+2])
				if err != nil {
					return 0, fmt.Errorf("used value %q: %w", fields[i+2], err)
				}
				return mb, nil
			}
		}
	}
	return 0, fmt.Errorf("no 'used = N.NN{G,M,K}' token in %q", val)
}

func parseSwapMagnitude(tok string) (int64, error) {
	if tok == "" {
		return 0, fmt.Errorf("empty value")
	}
	mult := 1.0
	num := tok
	switch tok[len(tok)-1] {
	case 'G', 'g':
		mult = 1024
		num = tok[:len(tok)-1]
	case 'M', 'm':
		mult = 1
		num = tok[:len(tok)-1]
	case 'K', 'k':
		mult = 1.0 / 1024.0
		num = tok[:len(tok)-1]
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, err
	}
	return int64(f * mult), nil
}

func parseDFAvailable(val string) (int64, error) {
	fields := strings.Fields(val)
	var nums []int64
	for _, f := range fields {
		if n, err := strconv.ParseInt(f, 10, 64); err == nil {
			nums = append(nums, n)
		}
		if len(nums) == 3 {
			break
		}
	}
	if len(nums) < 3 {
		return 0, fmt.Errorf("expected at least 3 numeric columns in df output, got %d in %q", len(nums), val)
	}
	return nums[2], nil
}
