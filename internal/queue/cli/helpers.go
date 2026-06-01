package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// parseQueueFlags parses the --project and --json/--format flags from subArgs
// and returns:
//   - projectDir: resolved project directory (cwd if unspecified)
//   - positional: remaining non-flag arguments
//   - outputJSON: true when --json or --format json was given
//   - ok: false if an unrecoverable parse error occurred
//
// Both `--project value` and `--project=value` forms are accepted per PL-028c.
// --json and --format json|text are accepted per hk-5553i policy.
func parseQueueFlags(subArgs []string, errOut io.Writer) (projectDir string, positional []string, outputJSON bool, ok bool) {
	return parseQueueFlagsExtra(subArgs, errOut, nil)
}

// parseQueueFlagsExtra is like parseQueueFlags but accepts an optional
// extraFlagFn callback that can handle subcommand-specific flags. The callback
// receives (args, i) and returns (nextI, consumed). If consumed is false the
// flag is treated as unknown and skipped (passed to positional).
//
// The --json flag (shorthand) and --format json (long form) are handled here
// for all queue subcommands. --json ≡ --format json per hk-5553i policy.
func parseQueueFlagsExtra(
	subArgs []string,
	errOut io.Writer,
	extraFlagFn func(args []string, i int) (nextI int, consumed bool),
) (projectDir string, positional []string, outputJSON bool, ok bool) {
	for i := 0; i < len(subArgs); {
		arg := subArgs[i]
		switch {
		case arg == "--project" && i+1 < len(subArgs):
			projectDir = subArgs[i+1]
			i += 2
		case strings.HasPrefix(arg, "--project="):
			projectDir = strings.TrimPrefix(arg, "--project=")
			i++
		case arg == "--json":
			// Convenience alias: --json ≡ --format json (mirrors handler status convention).
			outputJSON = true
			i++
		case arg == "--format" && i+1 < len(subArgs):
			outputJSON = subArgs[i+1] == "json"
			i += 2
		case strings.HasPrefix(arg, "--format="):
			outputJSON = strings.TrimPrefix(arg, "--format=") == "json"
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
			return "", nil, false, false
		}
		projectDir = wd
	}
	abs, err := filepath.Abs(projectDir)
	if err != nil {
		fmt.Fprintf(errOut, "harmonik queue: cannot resolve project path %q: %v\n", projectDir, err)
		return "", nil, false, false
	}
	return abs, positional, outputJSON, true
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

// beadsToQueueDoc builds a minimal QueueSubmitRequest JSON map from a list of
// bead IDs. Produces a single stream group (kind=stream, status=pending,
// group_index=0) containing one item per bead ID.
//
// queueName is the optional routing key (absent/empty → default "main"). When
// non-empty it is embedded as the "name" field in the returned document.
//
// The returned map is in the same shape expected by buildEnvelope — a
// map[string]json.RawMessage keyed by field name — so the caller can pass it
// directly to buildEnvelope("queue-submit", ...) or buildEnvelope("queue-dry-run", ...).
//
// Bead ref: hk-tigaf.8.
func beadsToQueueDoc(beadIDs []string, queueName string) (map[string]json.RawMessage, error) {
	type itemDoc struct {
		BeadID string `json:"bead_id"`
		Status string `json:"status"`
	}
	type groupDoc struct {
		GroupIndex int       `json:"group_index"`
		Kind       string    `json:"kind"`
		Status     string    `json:"status"`
		Items      []itemDoc `json:"items"`
	}
	type queueDoc struct {
		SchemaVersion int        `json:"schema_version"`
		Name          string     `json:"name,omitempty"`
		Groups        []groupDoc `json:"groups"`
	}

	items := make([]itemDoc, len(beadIDs))
	for i, id := range beadIDs {
		items[i] = itemDoc{BeadID: id, Status: "pending"}
	}
	doc := queueDoc{
		SchemaVersion: 1,
		Name:          queueName,
		Groups: []groupDoc{
			{GroupIndex: 0, Kind: "stream", Status: "pending", Items: items},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// parseBeadsFlag splits a --beads value (comma- or space-separated bead IDs)
// into individual IDs. Also accepts repeated --beads flags via the accumulator
// pattern (pass a pointer to []string and append to it).
func parseBeadsFlag(raw string) []string {
	var ids []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			ids = append(ids, part)
		}
	}
	return ids
}
