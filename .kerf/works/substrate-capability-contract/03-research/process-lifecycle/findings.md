# Research — Process Lifecycle

## Questions

1. Which session and tmux guarantees are already normative?
2. What does the specification require when selected hosting is unavailable?
3. Can Step 14 make pane paste, capture, or remote support a new promise?

## Findings

`specs/process-lifecycle.md` PL-021b defines the selected tmux-hosting
guarantees. They include the three session-resolution outcomes, deterministic
window names, the composition-root substrate seam, wait and kill behavior, and
the `agent_started.tmux_window_name` observation field. PL-006 and PL-006d
define the project session sweep and live-owner exclusions.

PL-021b item 2 and PL-028b distinguish two missing-hosting outcomes. Tmux is
fatal before dispatch when the selected substrate requires it. A non-tmux
selection is a boot-time announced degradation. The announcement must name
only capabilities that resolution did not provide.

The specification does not define generic missing behavior for the private
daemon interfaces. The Step 14 design must classify those behaviors and prove
them with focused tests.

PL-021d makes daemon-run task delivery and inter-phase injection an
agent-input responsibility. It retains tmux paste for keeper and interactive
nudge paths. The post-AIS PL-021b text makes capture-pane observation only,
never a daemon-run acknowledgment or liveness signal.

Remote-worker substrate guarantees remain unsettled. The process-lifecycle
specification does not authorize a new runner-swap or remote-session promise.

## Pattern to Follow

A narrow process-lifecycle amendment may say that the selected substrate
declares the capabilities needed for its selected hosting mode. It may require
an explicit, tested degradation for an optional capability and existing
pre-dispatch failure for a required capability.

It must not name private Go interfaces, change the three session outcomes,
restore daemon-run paste obligations, treat capture as an ACK, or claim an
unsettled remote behavior.

## Risk

A broad capability list in the specification would turn implementation details
into public guarantees. That would prevent the later tmux-host extraction from
changing its internal shape.
