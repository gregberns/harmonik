package handlercontract

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// LaunchSpecMaxStdinBytes is the maximum LaunchSpec payload size (in bytes)
// that is delivered on stdin. Payloads larger than this MUST be delivered via
// the --launch-spec <path> file-argument mechanism per HC-005.
const LaunchSpecMaxStdinBytes = 1 << 20 // 1 MiB

// LaunchSpecFileArg is the command-line argument name used to pass the path
// to the LaunchSpec JSON file when the payload exceeds LaunchSpecMaxStdinBytes
// per HC-005.
const LaunchSpecFileArg = "--launch-spec"

// MarshalLaunchSpec serialises spec to JSON and returns the byte slice.
// The caller uses the returned slice to choose the delivery mechanism per HC-005:
// if len(data) <= LaunchSpecMaxStdinBytes, deliver on stdin; else write to a
// temp file and pass --launch-spec <path>.
//
// Returns an error if spec is nil, spec.Valid() fails, or json.Marshal fails.
func MarshalLaunchSpec(spec *LaunchSpec) ([]byte, error) {
	if spec == nil {
		return nil, fmt.Errorf("handlercontract: MarshalLaunchSpec: spec is nil")
	}
	if err := spec.Valid(); err != nil {
		return nil, fmt.Errorf("handlercontract: MarshalLaunchSpec: %w", err)
	}
	data, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("handlercontract: MarshalLaunchSpec: json.Marshal: %w", err)
	}
	return data, nil
}

// ReadLaunchSpecFromArgs reads the LaunchSpec from the handler subprocess's
// runtime environment, following the HC-005 dual-delivery contract:
//
//   - If args contains "--launch-spec <path>", read and parse the JSON file at
//     <path>. Any subsequent "--launch-spec" entries are ignored.
//   - Otherwise, read and parse the JSON from r (typically os.Stdin).
//
// The returned *LaunchSpec is always validated via spec.Valid(). Returns an
// error if the spec is missing, malformed, or fails validation.
//
// Typical call-site usage in a handler subprocess's main():
//
//	spec, err := handlercontract.ReadLaunchSpecFromArgs(os.Args[1:], os.Stdin)
func ReadLaunchSpecFromArgs(args []string, r io.Reader) (*LaunchSpec, error) {
	// Scan for "--launch-spec <path>".
	for i := 0; i < len(args)-1; i++ {
		if args[i] == LaunchSpecFileArg {
			return readLaunchSpecFromFile(args[i+1])
		}
	}
	// Default: read from stdin.
	return readLaunchSpecFromReader(r)
}

// readLaunchSpecFromFile reads and validates the LaunchSpec from a JSON file.
func readLaunchSpecFromFile(path string) (*LaunchSpec, error) {
	if path == "" {
		return nil, fmt.Errorf("handlercontract: ReadLaunchSpec: %s argument has empty path", LaunchSpecFileArg)
	}
	//nolint:gosec // G304: path is supplied by the daemon via CLI arg; not user input in the attacker sense
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("handlercontract: ReadLaunchSpec: ReadFile %q: %w", path, err)
	}
	return unmarshalLaunchSpec(data, fmt.Sprintf("file %q", path))
}

// readLaunchSpecFromReader reads and validates the LaunchSpec from r (stdin).
func readLaunchSpecFromReader(r io.Reader) (*LaunchSpec, error) {
	if r == nil {
		return nil, fmt.Errorf("handlercontract: ReadLaunchSpec: reader is nil (no stdin)")
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("handlercontract: ReadLaunchSpec: ReadAll(stdin): %w", err)
	}
	return unmarshalLaunchSpec(data, "stdin")
}

// unmarshalLaunchSpec parses and validates a LaunchSpec from raw JSON bytes.
// source is a human-readable label ("stdin" or "file <path>") for error messages.
func unmarshalLaunchSpec(data []byte, source string) (*LaunchSpec, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("handlercontract: ReadLaunchSpec: empty %s", source)
	}
	var spec LaunchSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("handlercontract: ReadLaunchSpec: json.Unmarshal from %s: %w", source, err)
	}
	if err := spec.Valid(); err != nil {
		return nil, fmt.Errorf("handlercontract: ReadLaunchSpec: parsed spec from %s is invalid: %w", source, err)
	}
	return &spec, nil
}
