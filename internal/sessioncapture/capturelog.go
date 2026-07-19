package sessioncapture

// The mechanical CAPTURE-LOG ledger (AIS-014). The keeper EXTRACT-LOG.md is the
// executed model: a ledger that is ACTUALLY written per capture, not a
// declared-but-never-written file (the anti-pattern the spec names). One line
// is appended per capture session, so the ledger is a durable index of every
// corpus the driver has recorded under this workspace.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

const captureLogHeader = "# Session capture ledger (agent-input-substrate T7, AIS-014)\n\n" +
	"| timestamp (UTC) | session_id | corpus dir |\n" +
	"|---|---|---|\n"

// appendCaptureLog appends one row to root/CAPTURE-LOG.md, creating the file
// with its header on first write. Best-effort: a ledger write failure is logged
// and swallowed — it must never block a capture (AIS-INV-002 spirit).
func appendCaptureLog(ctx context.Context, root, sessionID string, now time.Time) {
	path := filepath.Join(root, captureLogFile)

	needHeader := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		needHeader = true
	}

	//nolint:gosec // G304: path derived from operator-supplied workspace, not remote input.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, filePerm)
	if err != nil {
		slog.WarnContext(ctx, "sessioncapture_capturelog_open_error", "path", path, "error", err.Error())
		return
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			slog.WarnContext(ctx, "sessioncapture_capturelog_close_error", "path", path, "error", cerr.Error())
		}
	}()

	if needHeader {
		if _, err := f.WriteString(captureLogHeader); err != nil {
			slog.WarnContext(ctx, "sessioncapture_capturelog_write_error", "path", path, "error", err.Error())
			return
		}
	}
	row := fmt.Sprintf("| %s | %s | %s |\n",
		now.UTC().Format(time.RFC3339),
		sessionID,
		sessionID,
	)
	if _, err := f.WriteString(row); err != nil {
		slog.WarnContext(ctx, "sessioncapture_capturelog_write_error", "path", path, "error", err.Error())
	}
}
