package sessioncapture

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
