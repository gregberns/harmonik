package handlercontract_test

import (
	"testing"

	"github.com/gregberns/harmonik/internal/handlercontract"
)

// TestRunCtx_ProviderTupleFields_Exist is a compile-and-shape pin that confirms the
// five Pi provider tuple fields exist on RunCtx with type string and round-trip their
// values. No behavior is tested here — that lands in C4/C5.
func TestRunCtx_ProviderTupleFields_Exist(t *testing.T) {
	rc := handlercontract.RunCtx{
		Provider:   "sentinel-provider",
		APIKeyEnv:  "sentinel-api-key-env",
		APIKeyFile: "sentinel-api-key-file",
		BaseURL:    "sentinel-base-url",
		API:        "sentinel-api",
	}
	if rc.Provider != "sentinel-provider" {
		t.Fatalf("Provider: got %q, want %q", rc.Provider, "sentinel-provider")
	}
	if rc.APIKeyEnv != "sentinel-api-key-env" {
		t.Fatalf("APIKeyEnv: got %q, want %q", rc.APIKeyEnv, "sentinel-api-key-env")
	}
	if rc.APIKeyFile != "sentinel-api-key-file" {
		t.Fatalf("APIKeyFile: got %q, want %q", rc.APIKeyFile, "sentinel-api-key-file")
	}
	if rc.BaseURL != "sentinel-base-url" {
		t.Fatalf("BaseURL: got %q, want %q", rc.BaseURL, "sentinel-base-url")
	}
	if rc.API != "sentinel-api" {
		t.Fatalf("API: got %q, want %q", rc.API, "sentinel-api")
	}
}
