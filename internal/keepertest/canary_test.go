package keepertest_test

// Corpus drift canary (T10; RS-018 item 2; measurement-design §3 "canary" row
// + §1.4): the extractor's manifest.json must equal the FROZEN 2026-07-13
// anchors before any replay runs — if a re-extraction shifts any anchor, this
// fails first (D13 out-of-band principle applied to the corpus itself).
// UNGATED: runs under make test-keeper-l012.

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keepertwin"
)

// frozenAnchors are the pinned baseline-2026-07-13 aggregates
// (measurement-design §1.4; SK-R10).
var frozenAnchors = struct {
	started, complete, aborted, clearUnconfirmed, unterminated, count int
	handoffTimeoutAborts                                              int
}{
	started: 507, complete: 427, aborted: 79, clearUnconfirmed: 347,
	unterminated: 1, count: 507, handoffTimeoutAborts: 79,
}

// TestKeeperDriftCanary_ManifestAnchors asserts manifest.json == the frozen
// anchors.
func TestKeeperDriftCanary_ManifestAnchors(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(corpusRoot(t), "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var m struct {
		Started          int            `json:"started"`
		Complete         int            `json:"complete"`
		Aborted          int            `json:"aborted"`
		ClearUnconfirmed int            `json:"clear_unconfirmed"`
		Unterminated     int            `json:"unterminated"`
		Count            int            `json:"count"`
		AbortReasons     map[string]int `json:"abort_reasons"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if m.Started != frozenAnchors.started ||
		m.Complete != frozenAnchors.complete ||
		m.Aborted != frozenAnchors.aborted ||
		m.ClearUnconfirmed != frozenAnchors.clearUnconfirmed ||
		m.Unterminated != frozenAnchors.unterminated ||
		m.Count != frozenAnchors.count {
		t.Fatalf("manifest %+v != frozen anchors %+v", m, frozenAnchors)
	}
	if len(m.AbortReasons) != 1 || m.AbortReasons["handoff_timeout"] != frozenAnchors.handoffTimeoutAborts {
		t.Fatalf("abort_reasons = %v, want {handoff_timeout:%d}", m.AbortReasons, frozenAnchors.handoffTimeoutAborts)
	}
}

// TestKeeperDriftCanary_CorpusIntegrity walks every corpus cycle:
//   - 507 .jsonl / .summary.json pairs (no orphans of either kind);
//   - every .jsonl line is valid JSON with a REGISTERED event type (zero
//     unknown types — the keeper analog of codex's FrameKindRaw gate);
//   - every summary classifies into one of the four strata, the strata counts
//     match the frozen population (80/347/79/1), and event_count == len(types)
//     == the .jsonl line count.
func TestKeeperDriftCanary_CorpusIntegrity(t *testing.T) {
	t.Parallel()
	dir := corpusCyclesDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read corpus dir: %v", err)
	}
	jsonl := map[string]bool{}
	summaries := map[string]bool{}
	for _, e := range entries {
		name := e.Name()
		switch {
		case strings.HasSuffix(name, ".summary.json"):
			summaries[strings.TrimSuffix(name, ".summary.json")] = true
		case strings.HasSuffix(name, ".jsonl"):
			jsonl[strings.TrimSuffix(name, ".jsonl")] = true
		}
	}
	if len(jsonl) != 507 || len(summaries) != 507 {
		t.Fatalf("corpus files: %d .jsonl + %d .summary.json, want 507 + 507", len(jsonl), len(summaries))
	}
	for base := range jsonl {
		if !summaries[base] {
			t.Fatalf("orphan cycle stream with no summary: %s", base)
		}
	}

	registered := core.AllPayloadSchemaVersions()
	strata := map[keepertwin.Stratum]int{}
	var unknownTypes []string

	for base := range jsonl {
		sum := loadSummary(t, filepath.Join(dir, base+".summary.json"))
		stratum, err := keepertwin.Classify(sum)
		if err != nil {
			t.Fatalf("classify %s: %v (out-of-vocabulary cycle — corpus drift)", base, err)
		}
		strata[stratum]++

		f, err := os.Open(filepath.Join(dir, base+".jsonl")) //nolint:gosec // G304: test-owned corpus testdata
		if err != nil {
			t.Fatalf("open %s.jsonl: %v", base, err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		lines := 0
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			lines++
			var env struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal([]byte(line), &env); err != nil {
				t.Errorf("%s.jsonl line %d: invalid JSON: %v", base, lines, err)
				continue
			}
			if _, ok := registered[env.Type]; !ok {
				unknownTypes = append(unknownTypes, env.Type)
			}
		}
		if err := sc.Err(); err != nil {
			t.Fatalf("scan %s.jsonl: %v", base, err)
		}
		_ = f.Close() //nolint:errcheck // read-only
		if lines == 0 {
			t.Errorf("%s.jsonl is empty (truncated corpus?)", base)
		}
		if lines != sum.EventCount || len(sum.Types) != sum.EventCount {
			t.Errorf("%s: lines=%d types=%d event_count=%d — summary drifted from stream",
				base, lines, len(sum.Types), sum.EventCount)
		}
	}

	if len(unknownTypes) > 0 {
		t.Fatalf("drift canary: %d corpus events carry UNREGISTERED types: %v "+
			"(fix: register them in internal/core before replaying)", len(unknownTypes), unknownTypes)
	}
	if strata[keepertwin.StratumCleanComplete] != 80 ||
		strata[keepertwin.StratumDegradedComplete] != 347 ||
		strata[keepertwin.StratumAbortHandoffTimeout] != 79 ||
		strata[keepertwin.StratumUnterminated] != 1 {
		t.Fatalf("strata = %v, want clean:80 degraded:347 abort:79 unterminated:1", strata)
	}
	t.Logf("drift canary: PASS — 507 cycle pairs, 0 unknown types, strata 80/347/79/1")
}

// TestKeeperDriftCanary_ExtractLedger asserts the RS-018 item-3 ledger exists
// and names the frozen source (the keeper analog of CAPTURE-LOG.md; the
// corpus source is the frozen log, so the ledger records the extraction).
func TestKeeperDriftCanary_ExtractLedger(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join(corpusRoot(t), "EXTRACT-LOG.md"))
	if err != nil {
		t.Fatalf("EXTRACT-LOG.md missing (RS-018 ledger): %v", err)
	}
	if !strings.Contains(string(raw), "baseline-2026-07-13") {
		t.Fatal("EXTRACT-LOG.md does not name the frozen baseline source")
	}
}
