package keeper

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

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

func isPrimarySID(sid string) bool {
	return isUUIDv4(sid)
}

func isUUIDv4(s string) bool {
	if len(s) != 36 {
		return false
	}
	if s[14] != '4' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if isUUIDHyphenIndex(i) {
			if s[i] != '-' {
				return false
			}
			continue
		}
		if !isLowerHexDigit(s[i]) {
			return false
		}
	}
	return true
}

func isUUIDHyphenIndex(i int) bool {
	return i == 8 || i == 13 || i == 18 || i == 23
}

func isLowerHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}
