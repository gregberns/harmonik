package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// parseQueueFlags parses the --project flag from subArgs and returns:
//   - projectDir: resolved project directory (cwd if unspecified)
//   - positional: remaining non-flag arguments
//   - ok: false if an unrecoverable parse error occurred
//
// Both `--project value` and `--project=value` forms are accepted per PL-028c.
func parseQueueFlags(subArgs []string, errOut io.Writer) (projectDir string, positional []string, ok bool) {
	return parseQueueFlagsExtra(subArgs, errOut, nil)
}

// parseQueueFlagsExtra is like parseQueueFlags but accepts an optional
// extraFlagFn callback that can handle subcommand-specific flags. The callback
// receives (args, i) and returns (nextI, consumed). If consumed is false the
// flag is treated as unknown and skipped (passed to positional).
func parseQueueFlagsExtra(
	subArgs []string,
	errOut io.Writer,
	extraFlagFn func(args []string, i int) (nextI int, consumed bool),
) (projectDir string, positional []string, ok bool) {
	for i := 0; i < len(subArgs); {
		arg := subArgs[i]
		switch {
		case arg == "--project" && i+1 < len(subArgs):
			projectDir = subArgs[i+1]
			i += 2
		case strings.HasPrefix(arg, "--project="):
			projectDir = strings.TrimPrefix(arg, "--project=")
			i++
		default:
			// Delegate to extra flag handler if provided.
			if extraFlagFn != nil {
				nextI, consumed := extraFlagFn(subArgs, i)
				if consumed {
					i = nextI
					continue
				}
			}
			// Treat as positional (or unknown flag — passed through).
			positional = append(positional, arg)
			i++
		}
	}

	// Resolve project directory.
	if projectDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(errOut, "harmonik queue: cannot determine working directory: %v\n", err)
			return "", nil, false
		}
		projectDir = wd
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(errOut, "harmonik queue: cannot resolve project path %q: %v\n", projectDir, err)
		return "", nil, false
	}
	return abs, positional, true
}

// harmonikDirFromProject returns the .harmonik subdirectory under projectDir.
// Returns "" and writes an error to errOut if the project directory does not
// exist.
func harmonikDirFromProject(projectDir string, errOut io.Writer) string {
	if _, err := os.Stat(projectDir); err != nil {
		fmt.Fprintf(errOut, "harmonik queue: project directory %q not accessible: %v\n", projectDir, err)
		return ""
	}
	return filepath.Join(projectDir, ".harmonik")
}

// buildEnvelope constructs a socket request envelope by merging an "op" field
// into an existing JSON document map. This lets the CLI forward queue document
// fields (groups, schema_version, etc.) to the daemon at the top level of the
// SocketRequest, which is what HandlerAdapter.HandleQueueSubmit /
// HandleQueueDryRun expect (they unmarshal the whole raw request as the
// typed request RECORD).
func buildEnvelope(op string, fields map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(fields)+1)
	opBytes, _ := json.Marshal(op) //nolint:errcheck // constant string; cannot fail
	out["op"] = opBytes
	for k, v := range fields {
		out[k] = v
	}
	return out
}

// marshalJSON is a thin wrapper around json.Marshal that returns []byte.
// Used to keep the call sites readable.
func marshalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}
