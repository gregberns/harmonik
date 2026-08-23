package lifecycle

import (
	"fmt"
	"runtime"
)

const darwinSunPathMax = 104

const linuxSunPathMax = 108

func sunPathMax() int {
	if runtime.GOOS == "darwin" {
		return darwinSunPathMax
	}
	return linuxSunPathMax
}

// ValidateSocketPathLength returns an error describing why sockPath cannot
// be bound/connected as a Unix-domain socket when it is at or beyond the
// platform's sun_path capacity (leaving no room for the NUL terminator the
// kernel appends). Returns nil when sockPath fits.
//
// Bead ref: hk-ta6dg.
func ValidateSocketPathLength(sockPath string) error {
	limit := sunPathMax()
	if len(sockPath) < limit {
		return nil
	}
	return fmt.Errorf(
		"socket path %q is %d bytes, at or beyond the platform sun_path limit of %d bytes "+
			"(%d usable, incl. NUL terminator) — bind/connect will fail with EINVAL/ENAMETOOLONG; "+
			"move the project to a shorter path (e.g. a shallower directory or a shorter symlink) "+
			"so <projectDir>/.harmonik/daemon.sock fits",
		sockPath, len(sockPath), limit, limit-1,
	)
}
