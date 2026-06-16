package keeper

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// sidFilePath returns the absolute path to the single-writer session-id channel
// <projectDir>/.harmonik/keeper/<agent>.sid (hk-8prq).
func sidFilePath(projectDir, agent string) string {
	return filepath.Join(projectDir, ".harmonik", "keeper", agent+".sid")
}

// ReadSessionIDFile reads the single-writer <agent>.sid channel (hk-8prq) and
// returns the normalised (trimmed + lowercased) session_id, the file's mod-time,
// and any read error. The .sid channel is written ONLY by the SessionStart hook
// (scripts/keeper-sessionstart-hook.sh) — the daemon never touches it — so it is
// the unambiguous source of the live interactive session's identity, free of the
// multi-writer races the gauge (.ctx) suffers.
//
// Normalisation (lowercase + trim) keeps the watcher's identity comparisons
// stable regardless of the on-disk byte form. The caller is responsible for
// deciding whether the value is trustworthy as PRIMARY identity (isPrimarySID);
// a present-but-malformed channel is NOT silently trusted (see ReadCtxFile).
//
// Returns os.ErrNotExist (wrapped by os.ReadFile) when the channel is absent.
// Refs: hk-8prq.
func ReadSessionIDFile(projectDir, agent string) (string, time.Time, error) {
	if err := validateAgent(agent); err != nil {
		return "", time.Time{}, err
	}
	path := sidFilePath(projectDir, agent)
	//nolint:gosec // G304: path derived from operator-controlled projectDir and agent validated above
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", time.Time{}, err
	}
	var mod time.Time
	if stat, statErr := os.Stat(path); statErr == nil {
		mod = stat.ModTime()
	}
	sid := strings.ToLower(strings.TrimSpace(string(raw)))
	return sid, mod, nil
}

// IsPrimarySID is the exported form of isPrimarySID: it reports whether sid is
// trustworthy as the keeper's PRIMARY identity (a well-formed lowercase UUIDv4).
// Used by `harmonik keeper doctor` to report whether the .sid channel carries a
// usable primary id or the keeper is on the fallback path. Refs: hk-8prq.
func IsPrimarySID(sid string) bool { return isPrimarySID(sid) }

// isPrimarySID reports whether sid is trustworthy as the keeper's PRIMARY
// identity: a well-formed, lowercase UUIDv4. Interactive captain/crew sessions
// use UUIDv4; daemon-spawned implementers use UUIDv7 (rejected here), and the
// conversation/transcript-dir id is an uppercase UUID (rejected by the
// lowercase-hex requirement). An empty or non-UUID value is likewise not
// primary, so the gauge fallback is used instead of binding a worse identity.
// Refs: hk-8prq, hk-lap (UUIDv7 vs v4), hk-mzdm (uppercase id).
func isPrimarySID(sid string) bool {
	return isUUIDv4(sid)
}

// isUUIDv4 reports whether s is a canonical, lowercase UUID version 4:
// 36 bytes, hyphens at indices 8/13/18/23, version nibble '4' at index 14, and
// all other characters lowercase hex. Uppercase hex is rejected so the
// conversation/transcript-dir UUID (which Claude Code occasionally surfaces) is
// never mistaken for the real session id. Refs: hk-8prq.
func isUUIDv4(s string) bool {
	if len(s) != 36 {
		return false
	}
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	if s[14] != '4' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		c := s[i]
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			continue
		}
		return false
	}
	return true
}
