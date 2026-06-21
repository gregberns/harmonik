package workers

// telemetry.go — periodic worker resource + problem snapshot (worker-report
// Phase 1, WR1).
//
// This file is the foundation of the worker-report feature: the typed event
// payload, its registration, and the pure parser for the inline darwin collector
// output. It mirrors health.go's shape (typed payload + core.RegisterEventType in
// init() + EmitFunc). WR2 adds the CommandRunner-driven collector (CollectReport
// + the inline darwin `sh -c` collector script), mirroring health.go's
// runner/emit shape. The timer/poll-loop wiring still lands in WR3, and
// problem-flag derivation (Problems) in WR4.
//
// Bead refs: hk-9wbl (WR1), hk-ec9v (WR2).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

	// "At what point are we maxing out?" — resource snapshot.

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

	// "Are there issues?" — problem flags (presence = problem detected),
	// e.g. "orphaned_claude", "worktree_leak", "disk_pressure".
	Problems []string `json:"problems,omitempty"`
}

func init() {
	if err := core.RegisterEventType("worker_report", func() core.EventPayload { return &WorkerReportPayload{} }); err != nil {
		panic("workers: init: register worker_report: " + err.Error())
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
	// repoPath is quoted in the df invocation to tolerate spaces in the path.
	return strings.Join([]string{
		`echo "load=$(sysctl -n vm.loadavg | tr -d '{}')"`,
		`echo "ncpu=$(sysctl -n hw.ncpu)"`,
		`echo "memtotal=$(sysctl -n hw.memsize)"`,
		`echo "vmstat<<$(vm_stat)"`,
		`echo "swap=$(sysctl -n vm.swapusage)"`,
		`echo "disk=$(df -m '` + repoPath + `' | tail -1)"`,
		`echo "claude=$(pgrep -f 'claude --session-id' | wc -l | tr -d ' ')"`,
		`echo "pagesize=$(sysctl -n hw.pagesize)"`,
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
// CollectReport does NOT derive Problems — that is WR4's problem-detection pass,
// which will cross-check ClaudeProcs against Registry.inFlight and add a Registry
// parameter at that point. Until then no Registry is needed here (matching
// RunHealthCheck's split: it takes reg only because it mutates enabled-state).
//
// On a runner failure the collector error is returned and no event is emitted.
//
// Bead ref: hk-ec9v (WR2).
func CollectReport(ctx context.Context, runner tmux.CommandRunner, w Worker, emit EmitFunc) (WorkerReportPayload, error) {
	cmd := runner.Command(ctx, "sh", "-c", darwinCollectorScript(w.RepoPath))
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
	// TODO(WR4): derive p.Problems here (orphaned_claude / worktree_leak /
	// disk_pressure), which will require a *Registry to read inFlight.

	emitWorkerReport(ctx, p, emit)
	return p, nil
}

// emitWorkerReport marshals and emits a worker_report event. No-op when emit is
// nil (mirrors emitUnhealthyEvent in health.go).
func emitWorkerReport(ctx context.Context, p WorkerReportPayload, emit EmitFunc) {
	if emit == nil {
		return
	}
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	_ = emit(ctx, core.EventTypeWorkerReport, b)
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

// parseWorkerReport parses the inline darwin collector output (the lines emitted
// by the `sh -c` collector described in §The collector) into the resource fields
// of a WorkerReportPayload. It is pure: it does not run commands, set WorkerName,
// SampledAt, or Problems — those are populated by the caller (CollectReport, WR2)
// and the problem-detection pass (WR4).
//
// The expected line shapes (order-independent, tolerant of extra whitespace):
//
//	load={1.20 1.10 0.95}        # sysctl -n vm.loadavg, braces stripped or not
//	ncpu=8                       # sysctl -n hw.ncpu
//	memtotal=17179869184         # sysctl -n hw.memsize (bytes)
//	vmstat<<                     # marker; the raw `vm_stat` block follows, then a blank line
//	  Pages free:    123456.
//	  Pages inactive: 65432.
//	swap=total = 2048.00M  used = 512.50M  free = 1535.50M ...  # sysctl -n vm.swapusage
//	disk=/dev/disk1 ... 123456 ... /Volumes/x   # df -m <repo_path> | tail -1
//	claude=3                     # pgrep -f 'claude --session-id' | wc -l
//
// Conversions:
//   - memtotal bytes → MB (÷ 1MiB).
//   - MemFreeMB ≈ (free + inactive pages) × pageSize ÷ 1MiB, where pageSize is
//     parsed from the vm_stat header ("page size of N bytes"); it falls back to
//     defaultDarwinPageSize (16384) if the header is absent/unparseable.
//   - swap "used = N.NN{G,M,K}" → MB (suffix-scaled: G ×1024, M ×1, K ÷1024).
//   - disk: the second numeric column of `df -m` output is "Available" MB.
//
// A line whose value cannot be parsed yields an error naming the offending key.
func parseWorkerReport(raw string) (WorkerReportPayload, error) {
	var p WorkerReportPayload

	var (
		sawLoad  bool
		freePg   int64
		inactPg  int64
		inVMStat bool
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

		// Within the vm_stat block, parse the page-count lines we care about.
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
			// Other vm_stat lines (active, wired, etc.) are ignored. The block
			// ends at a blank line or at the next key=value line.
			if !strings.Contains(trimmed, "=") {
				continue
			}
			inVMStat = false
			// fall through to key=value handling below
		}

		// The vm_stat block is introduced by a `vmstat<<` marker. In the real
		// collector the marker is on the same line as the first vm_stat output
		// (`echo "vmstat<<$(vm_stat)"`), so detect the prefix rather than a
		// key=value split. Everything after the marker until the next blank line
		// or key=value line is the vm_stat block.
		if strings.HasPrefix(trimmed, "vmstat<<") {
			inVMStat = true
			after := strings.TrimSpace(strings.TrimPrefix(trimmed, "vmstat<<"))
			// The collector emits the vm_stat block starting at this marker, so
			// the authoritative page size rides in on the header line that
			// usually follows "vmstat<<": "Mach Virtual Memory Statistics:
			// (page size of N bytes)". Parse N when present.
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
			// Authoritative page size from `sysctl -n hw.pagesize` (WR2 collector).
			// Overrides the vm_stat-header scrape and the fallback so MemFreeMB is
			// correct on Apple Silicon (16384) rather than the old x86 4096 guess.
			n, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return WorkerReportPayload{}, fmt.Errorf("parseWorkerReport: pagesize: %w", err)
			}
			if n > 0 {
				pageSize = n
			}
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
		}
	}

	if !sawLoad {
		return WorkerReportPayload{}, fmt.Errorf("parseWorkerReport: missing required load= line")
	}

	p.MemFreeMB = (freePg + inactPg) * pageSize / bytesPerMB

	return p, nil
}

// parseLoadavg parses the value of a `load=` line, e.g. "{1.20 1.10 0.95}" or
// "1.20 1.10 0.95", returning the 1- and 5-minute averages.
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

// parseVMStatPages parses a vm_stat page-count line of the form
// "Pages free:    123456." returning the count for the given prefix label.
// The trailing period (present in vm_stat output) is tolerated.
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

// parseVMStatPageSize parses the page size (bytes) from a vm_stat header line of
// the form "Mach Virtual Memory Statistics: (page size of 16384 bytes)". It
// returns (N, true) on a match, (0, false) otherwise so the caller keeps its
// current (fallback or already-parsed) value.
func parseVMStatPageSize(line string) (int64, bool) {
	const marker = "page size of "
	idx := strings.Index(line, marker)
	if idx < 0 {
		return 0, false
	}
	rest := line[idx+len(marker):]
	// rest now begins with "N bytes)"; take the leading numeric run.
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

// parseSwapUsed extracts the used MB from a `vm.swapusage` string, e.g.
// "total = 2048.00M  used = 512.50M  free = 1535.50M  (encrypted)".
// The value carries a magnitude suffix that vm.swapusage scales with load:
// M/m (already MB, ×1), G/g (GB → ×1024), or K/k (KB → ÷1024). The result is
// always returned in MB.
func parseSwapUsed(val string) (int64, error) {
	fields := strings.Fields(val)
	for i, f := range fields {
		if f == "used" {
			// Expect: used = N.NN{G,M,K}  → "=" at i+1, value at i+2.
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

// parseSwapMagnitude parses a swap value with a unit suffix (G/M/K, case
// insensitive) into MB. A bare number (no suffix) is treated as MB.
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

// parseDFAvailable parses the "Available" MB column from a single `df -m` data
// line (the output of `df -m <path> | tail -1`). df -m columns are:
// Filesystem  1M-blocks  Used  Available  Capacity  iused  ifree  %iused  Mounted-on.
// The Available column is the third numeric field. To be robust against a
// device name that contains spaces, this scans the numeric fields in order and
// takes the third one (1M-blocks, Used, Available).
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
