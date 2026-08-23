package codextest_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gregberns/harmonik/internal/codexdigitaltwin"
	"github.com/gregberns/harmonik/internal/codexinput"
)

func aisReactorScenariosDir(t *testing.T) string {
	t.Helper()
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(self), "..", "..",
		"testdata", "codex-app-server", "reactor-scenarios")
}

// TestAISDriftCanary_ReactorScenariosWellFormed asserts every reactor-scenario
// corpus file is non-empty, valid JSON per line, and carries a non-empty
// input-event `type` on each line's `in` object.
func TestAISDriftCanary_ReactorScenariosWellFormed(t *testing.T) {
	t.Parallel()
	dir := aisReactorScenariosDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read reactor-scenarios dir: %v", err)
	}
	files := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		files++
		path := filepath.Join(dir, e.Name())
		f, err := os.Open(path) //nolint:gosec // G304: test-owned corpus testdata
		if err != nil {
			t.Fatalf("open %s: %v", e.Name(), err)
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
			var rec struct {
				In struct {
					Type string `json:"type"`
				} `json:"in"`
			}
			if err := json.Unmarshal([]byte(line), &rec); err != nil {
				t.Errorf("%s line %d: invalid JSON: %v", e.Name(), lines, err)
				continue
			}
			if rec.In.Type == "" {
				t.Errorf("%s line %d: missing in.type — scenario drift", e.Name(), lines)
			}
		}
		if err := sc.Err(); err != nil {
			t.Fatalf("scan %s: %v", e.Name(), err)
		}
		_ = f.Close()
		if lines == 0 {
			t.Errorf("%s is empty (truncated corpus?)", e.Name())
		}
	}
	if files == 0 {
		t.Fatal("no reactor-scenario corpus files found")
	}
	t.Logf("drift canary: PASS — %d reactor-scenario corpus files well-formed", files)
}

// TestAISDriftCanary_SynthesizerIntegrity asserts each stratum synthesizes a
// non-empty, valid, decodable schedule.
func TestAISDriftCanary_SynthesizerIntegrity(t *testing.T) {
	t.Parallel()
	for _, stratum := range codexdigitaltwin.AllInputStrata {
		events, err := codexdigitaltwin.SynthesizeInputStimulus(stratum)
		if err != nil {
			t.Fatalf("synthesize %s: %v", stratum, err)
		}
		if len(events) == 0 {
			t.Fatalf("%s: empty schedule", stratum)
		}
		raw, err := codexdigitaltwin.EncodeInputStimulus(events)
		if err != nil {
			t.Fatalf("encode %s: %v", stratum, err)
		}
		for i, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			var ev codexinput.Event
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				t.Fatalf("%s line %d: invalid JSON: %v", stratum, i, err)
			}
			if ev.Type == "" {
				t.Fatalf("%s line %d: empty event type", stratum, i)
			}
		}
	}
}
