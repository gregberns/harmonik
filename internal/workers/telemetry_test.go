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
// canned collector output, stamps WorkerName + SampledAt, emits a worker_report
// event with the same payload, and does NOT derive Problems (WR4).
func TestCollectReport_PopulatesPayloadAndEmits(t *testing.T) {
	runner := collectorRunner{stdout: cannedCollectorStdout}

	var captured []struct {
		Type    core.EventType
		Payload []byte
	}
	emit := captureReportEmit(&captured)

	got, err := CollectReport(context.Background(), runner, reportTestWorker(), emit)
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
	if len(got.Problems) != 0 {
		t.Errorf("Problems: got %v, want empty (WR4 owns derivation)", got.Problems)
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
	got, err := CollectReport(context.Background(), runner, reportTestWorker(), nil)
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

	got, err := CollectReport(context.Background(), runner, reportTestWorker(), emit)
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
