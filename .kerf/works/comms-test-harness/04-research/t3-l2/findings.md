# Research — T3 (L2 socket/CLI e2e)

Key risk (identified in design doc §4): daemon-restart and tmux tests race the real socket — mitigated by
running T3 as ONE serial queue, never fanned out to parallel workers. `scratch-daemon.sh` already provides
clean-reset; no new infra needed. Option considered: could T3's daemon-restart scenario be simulated at L1
instead? Rejected — the design doc is explicit that killing a real process is required to exercise the
client's actual backoff/reconnect path (`runCommsRecvFollowIO`), which an in-process fake cannot exercise
faithfully.
