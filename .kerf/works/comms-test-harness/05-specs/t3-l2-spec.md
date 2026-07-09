# Change Spec — T3 (L2 socket/CLI e2e, serial)

Full assertions specified in design doc §2 "L2" and §3 scenarios 5, 6 (L2 half). Each of the 3 T3 beads
drives a real `scratch-daemon.sh` clean-reset daemon + real `harmonik comms` CLI processes; MUST be
dispatched as one serial queue (not fanned out) since process-kill and tmux race the same socket.
Verification: shell-driven test harness invoking the real CLI, asserting on stdout/exit codes and the
durable event log. Edge cases: daemon-restart mid-`--follow` (kill + relaunch, assert reconnect backoff +
re-anchor + 0 message loss across the restart); `--wake` to a dead pane (assert exit 0 + message still
delivered, failure only on stderr); one multi-consumer socket sanity run (2+ real CLI recv processes, assert
each gets every addressed message).
