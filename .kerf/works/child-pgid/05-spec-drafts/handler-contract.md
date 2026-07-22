<!--
Spec-draft AMENDMENT for specs/handler-contract.md §4.10 HC-044.
This is a DRAFT on the kerf bench. It is NOT applied to specs/ by this work.
Presented as the replacement text for the affected requirement(s); surrounding
requirements (HC-044a, HC-045, …) are unchanged and elided.
-->

# handler-contract.md — §4.10 amendment (HC-044)

## HC-044 — Subprocess is a child of the daemon; run children lead their own process group

The daemon MUST spawn every handler subprocess as a direct child process (per
[process-lifecycle.md §4.5]). The handler subprocess MUST communicate back to the daemon on
the Unix domain socket at `.harmonik/daemon.sock` (per [process-lifecycle.md §4.1]); this is
the same socket that carries the progress stream per §4.2.HC-007 and §4.2.HC-007a. There is
one bidirectional socket-backed channel per session; there is no separate "control channel"
at MVH. Socket authenticity is filesystem-permission-based for MVH (daemon socket MUST be
mode `0600` owned by the daemon user); per-connection challenges are deferred post-MVH.

The daemon MUST spawn every handler subprocess as the **leader of its own process group**:
the spawn attribute MUST be `SysProcAttr{Setpgid: true, Pgid: 0}` (Go maps `Pgid: 0` to
`setpgid(child, child)`, so the child's PGID equals its own PID). The daemon MUST NOT place
handler subprocesses in the daemon's process group. Placing the child in its own group is
what makes a group-directed signal (below) able to reach descendants the child forks.

`session.Kill` MUST signal the child's **process group**, not the bare child PID: it MUST
send SIGTERM to `-child_pgid` (equivalently `-child_pid`, since the child is a group
leader), and on grace-window expiry MUST escalate to SIGKILL to `-child_pgid`. `ESRCH`
(group already fully reaped) MUST be treated as success, not an error. Because grandchildren
the handler forks inherit the child's process group (unless they call `setsid` — see
[process-lifecycle.md §4.2] OQ-PL-011), the group-directed kill reaches the immediate child
**and every in-group grandchild** on both Linux and darwin while the daemon is alive.

On Linux, handler subprocesses SHOULD additionally install `PR_SET_PDEATHSIG(SIGTERM)` at
spawn time (kernel-delivered SIGTERM if the daemon thread exits); macOS has no equivalent and
subprocess survival across daemon death is a platform reality addressed by §4.10.HC-044a and
by the daemon's orphan sweep per [process-lifecycle.md §4.2 PL-006]. The provenance marker the
orphan sweep matches on is the `HARMONIK_PROJECT_HASH` environment variable per
[process-lifecycle.md §4.2 PL-006a]; the child's own-group PGID is a kill handle, NOT a
provenance signal.

Tags: mechanism
