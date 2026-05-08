package scenario

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// fileSeedFixtureUTF8 returns a minimally valid FileSeed with explicit utf8 encoding.
func fileSeedFixtureUTF8(t *testing.T) FileSeed {
	t.Helper()
	return FileSeed{
		Encoding: FileSeedEncodingUTF8,
		Contents: "hello, world\n",
		Mode:     "0644",
	}
}

// fileSeedFixtureBase64 returns a valid FileSeed with base64-encoded contents.
func fileSeedFixtureBase64(t *testing.T) FileSeed {
	t.Helper()
	return FileSeed{
		Encoding: FileSeedEncodingBase64,
		Contents: base64.StdEncoding.EncodeToString([]byte("binary\x00data")),
		Mode:     "0755",
	}
}

// fileSeedFixtureDefaults returns a FileSeed with empty Encoding and Mode (zero values).
func fileSeedFixtureDefaults(t *testing.T) FileSeed {
	t.Helper()
	return FileSeed{
		Contents: "default encoding and mode",
	}
}

func TestFileSeedEncoding_Valid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input FileSeedEncoding
		want  bool
	}{
		{name: "utf8 valid", input: FileSeedEncodingUTF8, want: true},
		{name: "base64 valid", input: FileSeedEncodingBase64, want: true},
		{name: "empty valid (defaults to utf8)", input: "", want: true},
		{name: "unknown invalid", input: FileSeedEncoding("hex"), want: false},
		{name: "uppercase UTF8 invalid", input: FileSeedEncoding("UTF8"), want: false},
		{name: "uppercase BASE64 invalid", input: FileSeedEncoding("BASE64"), want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("FileSeedEncoding(%q).Valid() = %v, want %v", string(tc.input), got, tc.want)
			}
		})
	}
}

func TestFileSeedEncoding_MarshalText(t *testing.T) {
	t.Parallel()

	t.Run("utf8 round-trips", func(t *testing.T) {
		t.Parallel()
		enc := FileSeedEncodingUTF8
		b, err := enc.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText error: %v", err)
		}
		var got FileSeedEncoding
		if err := got.UnmarshalText(b); err != nil {
			t.Fatalf("UnmarshalText error: %v", err)
		}
		if got != enc {
			t.Errorf("round-trip: got %q, want %q", string(got), string(enc))
		}
	})

	t.Run("base64 round-trips", func(t *testing.T) {
		t.Parallel()
		enc := FileSeedEncodingBase64
		b, err := enc.MarshalText()
		if err != nil {
			t.Fatalf("MarshalText error: %v", err)
		}
		var got FileSeedEncoding
		if err := got.UnmarshalText(b); err != nil {
			t.Fatalf("UnmarshalText error: %v", err)
		}
		if got != enc {
			t.Errorf("round-trip: got %q, want %q", string(got), string(enc))
		}
	})

	t.Run("unknown rejects on marshal", func(t *testing.T) {
		t.Parallel()
		enc := FileSeedEncoding("unknown")
		if _, err := enc.MarshalText(); err == nil {
			t.Error("MarshalText should return error for unknown value, got nil")
		}
	})

	t.Run("empty rejects on marshal", func(t *testing.T) {
		t.Parallel()
		enc := FileSeedEncoding("")
		if _, err := enc.MarshalText(); err == nil {
			t.Error("MarshalText should return error for empty value, got nil")
		}
	})

	t.Run("unknown rejects on unmarshal", func(t *testing.T) {
		t.Parallel()
		var got FileSeedEncoding
		if err := got.UnmarshalText([]byte("unknown")); err == nil {
			t.Error("UnmarshalText should return error for unknown value, got nil")
		}
	})
}

func TestFileSeed_Valid(t *testing.T) {
	t.Parallel()

	validBase64Contents := base64.StdEncoding.EncodeToString([]byte("binary content"))

	tests := []struct {
		name  string
		input FileSeed
		want  bool
	}{
		{
			name:  "utf8 happy path",
			input: fileSeedFixtureUTF8(t),
			want:  true,
		},
		{
			name:  "base64 happy path",
			input: fileSeedFixtureBase64(t),
			want:  true,
		},
		{
			name: "invalid base64 contents",
			input: FileSeed{
				Encoding: FileSeedEncodingBase64,
				Contents: "not-valid-base64!!!",
				Mode:     "0644",
			},
			want: false,
		},
		{
			name:  "empty encoding defaults to utf8 (valid)",
			input: fileSeedFixtureDefaults(t),
			want:  true,
		},
		{
			name: "invalid mode xyz",
			input: FileSeed{
				Encoding: FileSeedEncodingUTF8,
				Contents: "content",
				Mode:     "xyz",
			},
			want: false,
		},
		{
			name: "invalid mode 9999 (not parseable as octal)",
			input: FileSeed{
				Encoding: FileSeedEncodingUTF8,
				Contents: "content",
				Mode:     "9999",
			},
			want: false,
		},
		{
			name: "valid mode 0755",
			input: FileSeed{
				Encoding: FileSeedEncodingUTF8,
				Contents: "content",
				Mode:     "0755",
			},
			want: true,
		},
		{
			name: "valid mode 0644",
			input: FileSeed{
				Encoding: FileSeedEncodingUTF8,
				Contents: "content",
				Mode:     "0644",
			},
			want: true,
		},
		{
			name: "valid mode 0 (no permissions)",
			input: FileSeed{
				Encoding: FileSeedEncodingUTF8,
				Contents: "content",
				Mode:     "0",
			},
			want: true,
		},
		{
			name: "unknown encoding invalid",
			input: FileSeed{
				Encoding: FileSeedEncoding("hex"),
				Contents: "content",
				Mode:     "0644",
			},
			want: false,
		},
		{
			name: "base64 with valid std-encoding contents",
			input: FileSeed{
				Encoding: FileSeedEncodingBase64,
				Contents: validBase64Contents,
			},
			want: true,
		},
		{
			name: "empty contents utf8 valid (empty file)",
			input: FileSeed{
				Encoding: FileSeedEncodingUTF8,
				Contents: "",
				Mode:     "0644",
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.input.Valid(); got != tc.want {
				t.Errorf("FileSeed{Encoding:%q, Mode:%q}.Valid() = %v, want %v",
					string(tc.input.Encoding), tc.input.Mode, got, tc.want)
			}
		})
	}
}

func TestFileSeed_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input FileSeed
	}{
		{name: "utf8 with mode", input: fileSeedFixtureUTF8(t)},
		{name: "base64 with mode", input: fileSeedFixtureBase64(t)},
		{name: "defaults (empty encoding and mode)", input: fileSeedFixtureDefaults(t)},
		{
			name: "explicit utf8 no mode",
			input: FileSeed{
				Encoding: FileSeedEncodingUTF8,
				Contents: "just some text",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tc.input)
			if err != nil {
				t.Fatalf("json.Marshal error: %v", err)
			}

			var got FileSeed
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal error: %v", err)
			}

			if got.Encoding != tc.input.Encoding {
				t.Errorf("Encoding: got %q, want %q", string(got.Encoding), string(tc.input.Encoding))
			}
			if got.Contents != tc.input.Contents {
				t.Errorf("Contents: got %q, want %q", got.Contents, tc.input.Contents)
			}
			if got.Mode != tc.input.Mode {
				t.Errorf("Mode: got %q, want %q", got.Mode, tc.input.Mode)
			}
		})
	}
}

func TestFileSeed_OmitEmptyFields(t *testing.T) {
	t.Parallel()

	// When Encoding == "" and Mode == "", marshaled JSON MUST NOT contain those keys
	// (omitempty contract on struct tags).
	f := FileSeed{Contents: "text content"}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal into map error: %v", err)
	}

	if _, ok := raw["encoding"]; ok {
		t.Errorf("marshaled JSON contains 'encoding' key when Encoding is empty; got %s", data)
	}
	if _, ok := raw["mode"]; ok {
		t.Errorf("marshaled JSON contains 'mode' key when Mode is empty; got %s", data)
	}
	if _, ok := raw["contents"]; !ok {
		t.Errorf("marshaled JSON missing 'contents' key; got %s", data)
	}
}
