package evaljsonroundtrip_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	evaljsonroundtrip "github.com/gregberns/harmonik/evaltasks/eval-json-roundtrip"
)

func TestJSONRoundtrip(t *testing.T) {
	t.Parallel()

	t.Run("marshal_unmarshal_identity", func(t *testing.T) {
		t.Parallel()
		orig := evaljsonroundtrip.Config{Timeout: 90 * time.Second}
		b, err := json.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var got evaljsonroundtrip.Config
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if got.Timeout != orig.Timeout {
			t.Errorf("roundtrip: got %v, want %v", got.Timeout, orig.Timeout)
		}
	})

	t.Run("wire_format_is_string", func(t *testing.T) {
		t.Parallel()
		cfg := evaljsonroundtrip.Config{Timeout: 90 * time.Second}
		b, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		// 90s should marshal to "1m30s"
		if !strings.Contains(string(b), "1m30s") {
			t.Errorf("wire format %s does not contain expected string '1m30s'", b)
		}
	})

	t.Run("zero_duration", func(t *testing.T) {
		t.Parallel()
		orig := evaljsonroundtrip.Config{Timeout: 0}
		b, err := json.Marshal(orig)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var got evaljsonroundtrip.Config
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if got.Timeout != 0 {
			t.Errorf("zero roundtrip: got %v, want 0", got.Timeout)
		}
	})
}
