package workers

// telemetry_test.go — unit tests for the pure worker-report parser (WR1).
//
// These are intra-package (package workers) tests so they can exercise the
// unexported parseWorkerReport directly, mirroring the canned-output style of
// health_test.go without needing SSH or a CommandRunner.

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/lifecycle/tmux"
)

// sampleDarwinReport is a realistic block of the inline darwin collector output
// described in 05-phase1-spec.md §The collector. Used by the happy-path parse
// test. The header declares the page size (4096 here), which the parser reads
// authoritatively rather than assuming a constant. Values:
//   - load: 1.20 / 1.10 / 0.95
//   - ncpu: 8
//   - memtotal: 17179869184 bytes = 16384 MB
//   - vm_stat: free 200000 + inactive 100000 pages = 300000 * 4096 / 1MiB = 1171 MB
//   - swap used = 512.50M → 512 MB
//   - df -m available column (3rd numeric) = 250000 MB
//   - claude procs: 3
const sampleDarwinReport = `load={1.20 1.10 0.95}
ncpu=8
memtotal=17179869184
vmstat<<Mach Virtual Memory Statistics: (page size of 4096 bytes)
Pages free:                              200000.
Pages active:                            900000.
Pages inactive:                          100000.
Pages speculative:                         5000.
Pages wired down:                        300000.

swap=total = 2048.00M  used = 512.50M  free = 1535.50M  (encrypted)
disk=/dev/disk3s1   476802   220000   250000    47%  1234567  9876543   11%   /System/Volumes/Data
claude=3
`

// sampleAppleSiliconReport mirrors the actual worker (gb-mbp): the vm_stat header
// declares a 16384-byte page size. With the SAME page counts as sampleDarwinReport
// the MemFreeMB must be exactly 4× larger (16384/4096), proving the page size is
// parsed from the header rather than hardcoded. swap here carries a `G` suffix
// (under load), which must scale ×1024 to MB.
//   - vm_stat: free 200000 + inactive 100000 pages = 300000 * 16384 / 1MiB = 4687 MB
//   - swap used = 1.50G → 1536 MB
const sampleAppleSiliconReport = `load={2.00 1.80 1.50}
ncpu=12
memtotal=68719476736
vmstat<<Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                              200000.
Pages active:                            900000.
Pages inactive:                          100000.
Pages speculative:                         5000.
Pages wired down:                        300000.

swap=total = 8192.00M  used = 1.50G  free = 6656.00M  (encrypted)
disk=/dev/disk3s1   1907208   880000   1000000    47%  1234567  9876543   11%   /System/Volumes/Data
claude=5
`

func TestParseWorkerReport(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want WorkerReportPayload
	}{
		{
			name: "realistic darwin sample",
			raw:  sampleDarwinReport,
			want: WorkerReportPayload{
				Load1:       1.20,
				Load5:       1.10,
				NCPU:        8,
				MemTotalMB:  16384,
				MemFreeMB:   (200000 + 100000) * 4096 / (1024 * 1024), // 1171
				SwapUsedMB:  512,
				DiskFreeMB:  250000,
				ClaudeProcs: 3,
			},
		},
		{
			name: "tolerant of extra whitespace and brace-less loadavg",
			raw: "  load =  0.50 0.40 0.30  \n" +
				"ncpu =  4\n" +
				"memtotal= 8589934592\n" + // 8192 MB
				"vmstat<<Mach Virtual Memory Statistics: (page size of 4096 bytes)\n" +
				"Pages free:    50000.\n" +
				"Pages inactive:   25000.\n" +
				"\n" +
				"swap= total = 1024.00M   used = 0.00M   free = 1024.00M\n" +
				"disk=  /dev/disk1   100000  40000  60000  40%  / \n" +
				"claude= 0\n",
			want: WorkerReportPayload{
				Load1:       0.50,
				Load5:       0.40,
				NCPU:        4,
				MemTotalMB:  8192,
				MemFreeMB:   (50000 + 25000) * 4096 / (1024 * 1024), // 292 (header says 4096)
				SwapUsedMB:  0,
				DiskFreeMB:  60000,
				ClaudeProcs: 0,
			},
		},
		{
			// Apple Silicon worker: header declares 16384-byte pages, so the
			// SAME page counts yield a 4× larger MemFreeMB than the 4096 case —
			// proving the page size is parsed from the header, not hardcoded.
			name: "apple silicon 16384 page size and G-suffix swap",
			raw:  sampleAppleSiliconReport,
			want: WorkerReportPayload{
				Load1:       2.00,
				Load5:       1.80,
				NCPU:        12,
				MemTotalMB:  65536,
				MemFreeMB:   (200000 + 100000) * 16384 / (1024 * 1024), // 4687
				SwapUsedMB:  1536,                                      // 1.50G → 1.5 * 1024
				DiskFreeMB:  1000000,
				ClaudeProcs: 5,
			},
		},
		{
			// K-suffix swap (light load) must scale ÷1024 to MB, and absent a
			// vm_stat header the page size falls back to 16384 (the real worker),
			// NOT the old 4096 assumption.
			name: "K-suffix swap and header-less vm_stat fallback to 16384",
			raw: "load={0.10 0.10 0.10}\n" +
				"ncpu=8\n" +
				"memtotal=17179869184\n" +
				"vmstat<<\n" +
				"Pages free:                              10000.\n" +
				"Pages inactive:                           5000.\n" +
				"\n" +
				"swap=total = 1024.00M  used = 512.00K  free = 1023.50M\n" +
				"disk=/dev/disk1   100000  40000  60000  40%  /\n" +
				"claude=1\n",
			want: WorkerReportPayload{
				Load1:       0.10,
				Load5:       0.10,
				NCPU:        8,
				MemTotalMB:  16384,
				MemFreeMB:   (10000 + 5000) * 16384 / (1024 * 1024), // 234 (fallback 16384)
				SwapUsedMB:  0,                                      // 512.00K → 0.5 MB → int64(0)
				DiskFreeMB:  60000,
				ClaudeProcs: 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseWorkerReport(tt.raw)
			if err != nil {
				t.Fatalf("parseWorkerReport returned unexpected error: %v", err)
			}
			if got.Load1 != tt.want.Load1 {
				t.Errorf("Load1: got %v, want %v", got.Load1, tt.want.Load1)
			}
			if got.Load5 != tt.want.Load5 {
				t.Errorf("Load5: got %v, want %v", got.Load5, tt.want.Load5)
			}
			if got.NCPU != tt.want.NCPU {
				t.Errorf("NCPU: got %d, want %d", got.NCPU, tt.want.NCPU)
			}
			if got.MemTotalMB != tt.want.MemTotalMB {
				t.Errorf("MemTotalMB: got %d, want %d", got.MemTotalMB, tt.want.MemTotalMB)
			}
			if got.MemFreeMB != tt.want.MemFreeMB {
				t.Errorf("MemFreeMB: got %d, want %d", got.MemFreeMB, tt.want.MemFreeMB)
			}
			if got.SwapUsedMB != tt.want.SwapUsedMB {
				t.Errorf("SwapUsedMB: got %d, want %d", got.SwapUsedMB, tt.want.SwapUsedMB)
			}
			if got.DiskFreeMB != tt.want.DiskFreeMB {
				t.Errorf("DiskFreeMB: got %d, want %d", got.DiskFreeMB, tt.want.DiskFreeMB)
			}
			if got.ClaudeProcs != tt.want.ClaudeProcs {
				t.Errorf("ClaudeProcs: got %d, want %d", got.ClaudeProcs, tt.want.ClaudeProcs)
			}
			// The parser is pure: it must not invent WorkerName/SampledAt/Problems.
			if got.WorkerName != "" {
				t.Errorf("WorkerName: parser must leave empty, got %q", got.WorkerName)
			}
			if got.SampledAt != "" {
				t.Errorf("SampledAt: parser must leave empty, got %q", got.SampledAt)
			}
			if got.Problems != nil {
				t.Errorf("Problems: parser must leave nil, got %v", got.Problems)
			}
		})
	}
}

// ---- WR2 (hk-ec9v): CollectReport runner + emit tests ----

// cannedCollectorStdout is realistic output of darwinCollectorScript on an
// Apple-Silicon worker: the explicit `pagesize=16384` line (WR2) makes MemFreeMB
// page-size-correct (the old 4096 x86 assumption would under-count it 4×).
const cannedCollectorStdout = `load=1.20 1.10 0.95
ncpu=8
memtotal=17179869184
vmstat<<Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                              100000.
Pages inactive:                           50000.
Pages active:                            200000.

swap=total = 2048.00M  used = 512.50M  free = 1535.50M  (encrypted)
disk=/dev/disk1s1   476802   12345   400000    24%  1234567  9876543   11%   /System/Volumes/Data
claude=3
pagesize=16384
`

// collectorRunner is a fake tmux.CommandRunner returning canned stdout for the
// `sh -c <collector>` invocation. When failExit is true the command exits 1.
type collectorRunner struct {
	stdout   string
	failExit bool
}

func (r collectorRunner) Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	if r.failExit {
		return exec.CommandContext(ctx, "sh", "-c", "exit 1")
	}
	// Echo the canned stdout verbatim, passed as $0 to avoid quoting issues with
	// the multi-line content.
	return exec.CommandContext(ctx, "sh", "-c", `printf '%s' "$0"`, r.stdout)
}

var _ tmux.CommandRunner = collectorRunner{}

// captureReportEmit returns an EmitFunc that records (type, payload) pairs.
func captureReportEmit(events *[]struct {
	Type    core.EventType
	Payload []byte
},
) EmitFunc {
	return func(ctx context.Context, et core.EventType, b []byte) error {
		*events = append(*events, struct {
			Type    core.EventType
			Payload []byte
		}{et, b})
		return nil
	}
}

func reportTestWorker() Worker {
	return Worker{
		Name:      "test-worker",
		Transport: "ssh",
		Host:      "host.example.com",
		OS:        "darwin",
		RepoPath:  "/repo",
		MaxSlots:  4,
		Enabled:   true,
	}
}

// TestCollectReport_PopulatesPayloadAndEmits asserts CollectReport parses the
// canned collector output, stamps WorkerName + SampledAt, and emits a
// worker_report event with the same payload. A Registry with one in-flight slot
// is passed so the canned claude=3 does NOT trip orphaned_claude, isolating this
// test to the resource-snapshot path (problem derivation is covered separately).
func TestCollectReport_PopulatesPayloadAndEmits(t *testing.T) {
	runner := collectorRunner{stdout: cannedCollectorStdout}

	var captured []struct {
		Type    core.EventType
		Payload []byte
	}
	emit := captureReportEmit(&captured)

	// Registry with one reserved slot → InFlight()==1 → claude=3 is accounted for.
	reg := NewRegistry(Config{Version: 1, Workers: []Worker{reportTestWorker()}})
	reg.SelectWorker()
	got, err := CollectReport(context.Background(), runner, reportTestWorker(), reg, DefaultDiskFloorMB, emit)
	if err != nil {
		t.Fatalf("CollectReport returned unexpected error: %v", err)
	}

	if got.WorkerName != "test-worker" {
		t.Errorf("WorkerName: got %q, want %q", got.WorkerName, "test-worker")
	}
	if got.SampledAt == "" {
		t.Error("SampledAt must not be empty")
	}
	if got.Load1 != 1.20 || got.Load5 != 1.10 {
		t.Errorf("Load: got %v/%v, want 1.20/1.10", got.Load1, got.Load5)
	}
	if got.NCPU != 8 {
		t.Errorf("NCPU: got %d, want 8", got.NCPU)
	}
	if got.MemTotalMB != 16384 {
		t.Errorf("MemTotalMB: got %d, want 16384", got.MemTotalMB)
	}
	// (100000 free + 50000 inactive) * 16384 / 1MiB, page-size-correct.
	wantFreeMB := int64(150000) * 16384 / (1024 * 1024)
	if got.MemFreeMB != wantFreeMB {
		t.Errorf("MemFreeMB: got %d, want %d (16384 page size)", got.MemFreeMB, wantFreeMB)
	}
	if got.SwapUsedMB != 512 {
		t.Errorf("SwapUsedMB: got %d, want 512", got.SwapUsedMB)
	}
	if got.DiskFreeMB != 400000 {
		t.Errorf("DiskFreeMB: got %d, want 400000", got.DiskFreeMB)
	}
	if got.ClaudeProcs != 3 {
		t.Errorf("ClaudeProcs: got %d, want 3", got.ClaudeProcs)
	}
	// DiskFreeMB 400000 >= floor, claude=3 accounted for by the in-flight slot,
	// no worktrees= line → 0 → no leak. So a clean report carries no Problems.
	if len(got.Problems) != 0 {
		t.Errorf("Problems: got %v, want empty for a clean accounted report", got.Problems)
	}

	if len(captured) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(captured))
	}
	if captured[0].Type != core.EventTypeWorkerReport {
		t.Fatalf("event type: got %q, want %q", captured[0].Type, core.EventTypeWorkerReport)
	}
	var payload WorkerReportPayload
	if err := json.Unmarshal(captured[0].Payload, &payload); err != nil {
		t.Fatalf("unmarshal emitted payload: %v", err)
	}
	if payload.WorkerName != "test-worker" || payload.ClaudeProcs != 3 {
		t.Errorf("emitted payload mismatch: %+v", payload)
	}
	if payload.SampledAt != got.SampledAt {
		t.Errorf("emitted SampledAt %q != returned %q", payload.SampledAt, got.SampledAt)
	}
}

// TestCollectReport_NilEmitNoError asserts a nil EmitFunc suppresses emission
// without error and still returns the parsed payload.
func TestCollectReport_NilEmitNoError(t *testing.T) {
	runner := collectorRunner{stdout: cannedCollectorStdout}
	got, err := CollectReport(context.Background(), runner, reportTestWorker(), nil, DefaultDiskFloorMB, nil)
	if err != nil {
		t.Fatalf("CollectReport (nil emit): unexpected error: %v", err)
	}
	if got.NCPU != 8 {
		t.Errorf("NCPU: got %d, want 8", got.NCPU)
	}
}

// TestCollectReport_RunnerFailure asserts a non-zero collector exit yields an
// error, a zero-value payload, and no emitted event.
func TestCollectReport_RunnerFailure(t *testing.T) {
	runner := collectorRunner{failExit: true}

	var captured []struct {
		Type    core.EventType
		Payload []byte
	}
	emit := captureReportEmit(&captured)

	got, err := CollectReport(context.Background(), runner, reportTestWorker(), nil, DefaultDiskFloorMB, emit)
	if err == nil {
		t.Fatal("CollectReport: expected error on runner failure, got nil")
	}
	if got.WorkerName != "" {
		t.Errorf("expected zero-value payload on failure, got WorkerName %q", got.WorkerName)
	}
	if len(captured) != 0 {
		t.Errorf("expected no event on runner failure, got %d", len(captured))
	}
	if !strings.Contains(err.Error(), "collector failed") {
		t.Errorf("error should name the collector failure, got: %v", err)
	}
}

func TestParseWorkerReport_Malformed(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantInErr string
	}{
		{
			name:      "missing load line",
			raw:       "ncpu=8\nmemtotal=17179869184\nclaude=2\n",
			wantInErr: "missing required load",
		},
		{
			name:      "non-numeric ncpu",
			raw:       "load={1.0 1.0 1.0}\nncpu=eight\n",
			wantInErr: "ncpu",
		},
		{
			name:      "non-numeric memtotal",
			raw:       "load={1.0 1.0 1.0}\nmemtotal=lots\n",
			wantInErr: "memtotal",
		},
		{
			name:      "load with too few fields",
			raw:       "load={1.0}\n",
			wantInErr: "load",
		},
		{
			name:      "non-numeric swap used value",
			raw:       "load={1.0 1.0 1.0}\nswap=total = 1024.00M  used = lotsG  free = 512.00M\n",
			wantInErr: "swap",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseWorkerReport(tt.raw)
			if err == nil {
				t.Fatalf("parseWorkerReport: expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantInErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tt.wantInErr)
			}
		})
	}
}

// ---- WR4 (hk-b2f9): deriveProblems flag tests ----

// regWithInFlight builds a single-worker Registry and reserves `n` slots so
// InFlight() == n, exercising the orphaned_claude cross-check. MaxSlots is set
// high enough that all n reservations succeed.
func regWithInFlight(n int) *Registry {
	w := reportTestWorker()
	w.MaxSlots = n + 4
	reg := NewRegistry(Config{Version: 1, Workers: []Worker{w}})
	for i := 0; i < n; i++ {
		reg.SelectWorker()
	}
	return reg
}

func TestDeriveProblems(t *testing.T) {
	const floor = int64(2048)
	// The fully-loaded healthy worker holds 1 (main worktree) + maxSlots run
	// worktrees. With maxSlots=4 the baseline is 5: count 5 must NOT flag, 6 must.
	const maxSlots = 4

	tests := []struct {
		name        string
		rep         WorkerReportPayload
		reg         *Registry
		diskFloorMB int64
		maxSlots    int
		want        []string
	}{
		{
			name:        "clean — no flags",
			rep:         WorkerReportPayload{ClaudeProcs: 0, DiskFreeMB: 50000, WorktreeCount: 1},
			reg:         regWithInFlight(0),
			diskFloorMB: floor,
			maxSlots:    maxSlots,
			want:        nil,
		},
		{
			// claude lingers with zero in-flight runs → the chani-handoff symptom.
			name:        "orphaned_claude — procs running, no in-flight",
			rep:         WorkerReportPayload{ClaudeProcs: 1, DiskFreeMB: 50000, WorktreeCount: 1},
			reg:         regWithInFlight(0),
			diskFloorMB: floor,
			maxSlots:    maxSlots,
			want:        []string{"orphaned_claude"},
		},
		{
			// Same procs, but a run is in flight → accounted for, no flag.
			name:        "orphaned_claude NOT set — procs accounted for by in-flight",
			rep:         WorkerReportPayload{ClaudeProcs: 3, DiskFreeMB: 50000, WorktreeCount: 1},
			reg:         regWithInFlight(1),
			diskFloorMB: floor,
			maxSlots:    maxSlots,
			want:        nil,
		},
		{
			// nil Registry → treated as zero in-flight → orphaned fires.
			name:        "orphaned_claude — nil registry treated as zero in-flight",
			rep:         WorkerReportPayload{ClaudeProcs: 2, DiskFreeMB: 50000, WorktreeCount: 1},
			reg:         nil,
			diskFloorMB: floor,
			maxSlots:    maxSlots,
			want:        []string{"orphaned_claude"},
		},
		{
			name:        "disk_pressure — below floor",
			rep:         WorkerReportPayload{ClaudeProcs: 0, DiskFreeMB: floor - 1, WorktreeCount: 1},
			reg:         regWithInFlight(0),
			diskFloorMB: floor,
			maxSlots:    maxSlots,
			want:        []string{"disk_pressure"},
		},
		{
			name:        "disk_pressure NOT set — exactly at floor",
			rep:         WorkerReportPayload{ClaudeProcs: 0, DiskFreeMB: floor, WorktreeCount: 1},
			reg:         regWithInFlight(0),
			diskFloorMB: floor,
			maxSlots:    maxSlots,
			want:        nil,
		},
		{
			// diskFloorMB <= 0 selects DefaultDiskFloorMB (2048).
			name:        "disk_pressure — default floor when diskFloorMB <= 0",
			rep:         WorkerReportPayload{ClaudeProcs: 0, DiskFreeMB: DefaultDiskFloorMB - 1, WorktreeCount: 1},
			reg:         regWithInFlight(0),
			diskFloorMB: 0,
			maxSlots:    maxSlots,
			want:        []string{"disk_pressure"},
		},
		{
			// A fully-loaded healthy worker holds 1+maxSlots worktrees and must NOT
			// flag — this is the false-flag the off-by-one fix targets.
			name:        "worktree_leak NOT set — fully loaded at 1+max_slots",
			rep:         WorkerReportPayload{ClaudeProcs: 0, DiskFreeMB: 50000, WorktreeCount: 1 + maxSlots},
			reg:         regWithInFlight(0),
			diskFloorMB: floor,
			maxSlots:    maxSlots,
			want:        nil,
		},
		{
			// One worktree above the fully-loaded baseline (2+max_slots) → real leak.
			name:        "worktree_leak — one above 1+max_slots baseline",
			rep:         WorkerReportPayload{ClaudeProcs: 0, DiskFreeMB: 50000, WorktreeCount: 2 + maxSlots},
			reg:         regWithInFlight(0),
			diskFloorMB: floor,
			maxSlots:    maxSlots,
			want:        []string{"worktree_leak"},
		},
		{
			// maxSlots unset (0) → baseline falls back to 1+DefaultMaxSlotsFallback.
			// Count at the fallback baseline must NOT flag; one above must.
			name:        "worktree_leak NOT set — maxSlots unset uses fallback baseline",
			rep:         WorkerReportPayload{ClaudeProcs: 0, DiskFreeMB: 50000, WorktreeCount: 1 + DefaultMaxSlotsFallback},
			reg:         regWithInFlight(0),
			diskFloorMB: floor,
			maxSlots:    0,
			want:        nil,
		},
		{
			name:        "worktree_leak — maxSlots unset, one above fallback baseline",
			rep:         WorkerReportPayload{ClaudeProcs: 0, DiskFreeMB: 50000, WorktreeCount: 2 + DefaultMaxSlotsFallback},
			reg:         regWithInFlight(0),
			diskFloorMB: floor,
			maxSlots:    0,
			want:        []string{"worktree_leak"},
		},
		{
			// All three conditions at once → stable order: orphaned, disk, worktree.
			name:        "all three flags in stable order",
			rep:         WorkerReportPayload{ClaudeProcs: 5, DiskFreeMB: 10, WorktreeCount: 99},
			reg:         regWithInFlight(0),
			diskFloorMB: floor,
			maxSlots:    maxSlots,
			want:        []string{"orphaned_claude", "disk_pressure", "worktree_leak"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveProblems(tt.rep, tt.reg, tt.diskFloorMB, tt.maxSlots)
			if len(got) != len(tt.want) {
				t.Fatalf("deriveProblems = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("deriveProblems[%d] = %q, want %q (full: %v)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}

// TestParseWorkerReport_WorktreeCount asserts the WR4 collector `worktrees=` line
// is parsed into WorktreeCount, and that its absence (pre-WR4 output) yields 0.
func TestParseWorkerReport_WorktreeCount(t *testing.T) {
	withWT := "load={1.0 1.0 1.0}\nclaude=0\nworktrees=7\n"
	got, err := parseWorkerReport(withWT)
	if err != nil {
		t.Fatalf("parseWorkerReport: unexpected error: %v", err)
	}
	if got.WorktreeCount != 7 {
		t.Errorf("WorktreeCount: got %d, want 7", got.WorktreeCount)
	}

	noWT := "load={1.0 1.0 1.0}\nclaude=0\n"
	got2, err := parseWorkerReport(noWT)
	if err != nil {
		t.Fatalf("parseWorkerReport (no worktrees line): unexpected error: %v", err)
	}
	if got2.WorktreeCount != 0 {
		t.Errorf("WorktreeCount: got %d, want 0 when line absent", got2.WorktreeCount)
	}
}

// sampleLinuxReport is a realistic block from linuxCollectorScript output running
// in a Linux container (e.g. alpine or debian). The key differences from darwin:
//   - load= has no braces: "0.52 0.31 0.22" (awk '{print $1,$2,$3}' /proc/loadavg)
//   - No vmstat<< block; no pagesize= line.
//   - memfree= (MemAvailable bytes) replaces the darwin vm_stat page-count path.
//   - swapused= (bytes) replaces the darwin `swap=total... used=...` format.
//
// Values:
//   - load: 0.52 / 0.31
//   - ncpu: 4
//   - memtotal: 8589934592 bytes → 8192 MB
//   - memfree: 7516192768 bytes → 7168 MB
//   - swapused: 536870912 bytes → 512 MB
//   - disk: 250000 MB available (3rd numeric column)
//   - claude: 0, worktrees: 2
const sampleLinuxReport = `load=0.52 0.31 0.22
ncpu=4
memtotal=8589934592
memfree=7516192768
swapused=536870912
disk=overlay         102400  51200  51200  50% /
claude=0
worktrees=2
`

// TestParseWorkerReport_Linux verifies that parseWorkerReport correctly handles
// the Linux collector format: no braces in load=, memfree= + swapused= keys
// replace the darwin vm_stat block, and backward-compat darwin keys are unaffected.
// Bead: hk-yflqo (L3 Linux OS target, decision 4).
func TestParseWorkerReport_Linux(t *testing.T) {
	t.Run("linux format with memfree and swapused", func(t *testing.T) {
		got, err := parseWorkerReport(sampleLinuxReport)
		if err != nil {
			t.Fatalf("parseWorkerReport Linux: unexpected error: %v", err)
		}
		if got.Load1 != 0.52 {
			t.Errorf("Load1: got %v, want 0.52", got.Load1)
		}
		if got.Load5 != 0.31 {
			t.Errorf("Load5: got %v, want 0.31", got.Load5)
		}
		if got.NCPU != 4 {
			t.Errorf("NCPU: got %d, want 4", got.NCPU)
		}
		if got.MemTotalMB != 8192 {
			t.Errorf("MemTotalMB: got %d, want 8192", got.MemTotalMB)
		}
		if got.MemFreeMB != 7168 {
			t.Errorf("MemFreeMB: got %d, want 7168 (from memfree= key)", got.MemFreeMB)
		}
		if got.SwapUsedMB != 512 {
			t.Errorf("SwapUsedMB: got %d, want 512 (from swapused= key)", got.SwapUsedMB)
		}
		if got.DiskFreeMB != 51200 {
			t.Errorf("DiskFreeMB: got %d, want 51200 (3rd numeric col of df -m)", got.DiskFreeMB)
		}
		if got.ClaudeProcs != 0 {
			t.Errorf("ClaudeProcs: got %d, want 0", got.ClaudeProcs)
		}
		if got.WorktreeCount != 2 {
			t.Errorf("WorktreeCount: got %d, want 2", got.WorktreeCount)
		}
		// Parser must not populate metadata fields.
		if got.WorkerName != "" || got.SampledAt != "" || got.Problems != nil {
			t.Errorf("parser must leave WorkerName/SampledAt/Problems empty, got %+v", got)
		}
	})

	t.Run("memfree= wins over vm_stat page counts when both present", func(t *testing.T) {
		// Mixing both keys in one report (e.g. a test that emits both by accident):
		// memfree= must win; the vm_stat block must be silently ignored.
		mixed := "load=1.0 1.0 1.0\n" +
			"ncpu=2\n" +
			"memtotal=4294967296\n" + // 4096 MB
			"memfree=2147483648\n" + // 2048 MB — must be used
			"vmstat<<Mach Virtual Memory Statistics: (page size of 4096 bytes)\n" +
			"Pages free:                              999999.\n" + // would give >>2048 MB — must be ignored
			"Pages inactive:                          999999.\n" +
			"\n" +
			"swap=total = 1024.00M  used = 0.00M  free = 1024.00M\n" +
			"disk=/dev/sda1 200000 100000 100000 50% /\n" +
			"claude=0\n"
		got, err := parseWorkerReport(mixed)
		if err != nil {
			t.Fatalf("parseWorkerReport mixed: unexpected error: %v", err)
		}
		if got.MemFreeMB != 2048 {
			t.Errorf("MemFreeMB: got %d, want 2048 (memfree= key must win over vm_stat)", got.MemFreeMB)
		}
	})
}
