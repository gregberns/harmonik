package lifecycle

import (
	"fmt"
	"os"
	"strings"
)

func probePidCmdline(pid int) (string, bool) {
	path := fmt.Sprintf("/proc/%d/cmdline", pid)
	//nolint:gosec // G304: path constructed from pid obtained via kill(pid,0) probe; not user input
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	argv0 := strings.SplitN(string(data), "\x00", 2)[0]
	if argv0 == "" {
		return "", false
	}
	return argv0, true
}
