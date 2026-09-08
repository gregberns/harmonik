package herdrwire

import "context"

// AgentStatus is herdr's classification of what an agent is doing.
// Unknown means herdr could not classify the pane — it is NOT the same as
// Done, and a caller must not treat it as "exited" or "idle": treat it as a
// failed probe and take no destructive action (README §5.7).
type AgentStatus string

// The five AgentStatus values herdr reports.
const (
	AgentStatusIdle    AgentStatus = "idle"
	AgentStatusWorking AgentStatus = "working"
	AgentStatusBlocked AgentStatus = "blocked"
	AgentStatusDone    AgentStatus = "done"
	AgentStatusUnknown AgentStatus = "unknown"
)

// ReadSource selects which buffer pane.read / agent.read reads from.
// Verified against the live protocol-22 server: the third value is
// "recent_unwrapped" (underscore), not the hyphenated spelling the parcel
// brief used.
type ReadSource string

// The ReadSource values the live protocol-22 schema accepts.
const (
	ReadSourceVisible         ReadSource = "visible"
	ReadSourceRecent          ReadSource = "recent"
	ReadSourceRecentUnwrapped ReadSource = "recent_unwrapped"
	ReadSourceDetection       ReadSource = "detection"
)

// ReadFormat selects plain text or ANSI-preserving output for a read.
type ReadFormat string

// The two ReadFormat values.
const (
	ReadFormatText ReadFormat = "text"
	ReadFormatAnsi ReadFormat = "ansi"
)

// SplitDirection is the pane.split orientation.
type SplitDirection string

// The two SplitDirection values.
const (
	SplitRight SplitDirection = "right"
	SplitDown  SplitDirection = "down"
)

// PingResult is the result of the "ping" method.
type PingResult struct {
	Type     string `json:"type"`
	Version  string `json:"version"`
	Protocol uint32 `json:"protocol"`
}

// Ping round-trips the server and returns its reported version/protocol.
// It does NOT itself check the protocol pin — use CheckProtocol for that;
// Ping is for callers that want the raw values (e.g. `keeper doctor`).
func (c *Client) Ping(ctx context.Context) (PingResult, error) {
	var out PingResult
	err := c.rawCall(ctx, "ping", struct{}{}, &out)
	return out, err
}

// --- agent.start -------------------------------------------------------

// AgentStartParams starts an agent process in an existing shell pane.
// There is no Env field: AgentStartParams has no env parameter on the wire
// (confirmed live) — the agent inherits the shell pane's environment, which
// is why the respawn sequence sets env at pane.split, not here (README §5.6).
type AgentStartParams struct {
	Name   string   `json:"name"`
	Kind   string   `json:"kind"`
	PaneID string   `json:"pane_id"`
	Args   []string `json:"args,omitempty"`
	// TimeoutMs must be > 3000 and <= 300000 when set (herdr-enforced).
	TimeoutMs uint64 `json:"timeout_ms,omitempty"`
}

// AgentStartResult is the result of agent.start.
type AgentStartResult struct {
	Type  string   `json:"type"`
	Agent string   `json:"agent"`
	Argv  []string `json:"argv"`
}

// AgentStart starts an interactive agent process of kind in an existing
// shell pane, under name. name must satisfy the herdr agent-name limit
// documented in the package doc comment (1-32 chars, [a-z][a-z0-9_-]*), and
// herdr fails closed with a *WireError{Code:"invalid_agent_name"} otherwise.
func (c *Client) AgentStart(ctx context.Context, p AgentStartParams) (AgentStartResult, error) {
	var out AgentStartResult
	err := c.call(ctx, "agent.start", p, &out)
	return out, err
}

// --- agent.send_keys -----------------------------------------------------

// AgentSendKeysParams sends named key events (e.g. "Enter", "Escape") to
// the agent named by Target.
type AgentSendKeysParams struct {
	Target string   `json:"target"`
	Keys   []string `json:"keys"`
}

// AgentSendKeys sends keys to the agent named by target. Errors with
// *WireError{Code:"agent_not_found"} when target does not resolve.
func (c *Client) AgentSendKeys(ctx context.Context, p AgentSendKeysParams) error {
	return c.call(ctx, "agent.send_keys", p, nil)
}

// --- agent.wait ----------------------------------------------------------

// AgentWaitParams blocks the call until the named agent reaches one of the
// Until statuses, or TimeoutMs elapses.
type AgentWaitParams struct {
	Target    string        `json:"target"`
	Until     []AgentStatus `json:"until,omitempty"`
	TimeoutMs uint64        `json:"timeout_ms,omitempty"`
}

// AgentWaitResult is the result of agent.wait.
type AgentWaitResult struct {
	Type  string `json:"type"`
	Agent any    `json:"agent"`
}

// AgentWait blocks until the target agent reaches one of the Until
// statuses or the wait times out. Use a context deadline comfortably longer
// than TimeoutMs — herdr, not this package, owns the wait budget.
func (c *Client) AgentWait(ctx context.Context, p AgentWaitParams) (AgentWaitResult, error) {
	var out AgentWaitResult
	err := c.call(ctx, "agent.wait", p, &out)
	return out, err
}

// --- agent.prompt ----------------------------------------------------------

// AgentPromptWaitOptions is the optional wait clause on agent.prompt: send
// text, then block for one of Until (or TimeoutMs), in one call.
type AgentPromptWaitOptions struct {
	Until     []AgentStatus `json:"until,omitempty"`
	TimeoutMs uint64        `json:"timeout_ms,omitempty"`
}

// AgentPromptParams sends Text to the named agent and optionally waits for
// a resulting status.
type AgentPromptParams struct {
	Target string                  `json:"target"`
	Text   string                  `json:"text"`
	Wait   *AgentPromptWaitOptions `json:"wait,omitempty"`
}

// AgentPromptResult is the result of agent.prompt.
type AgentPromptResult struct {
	Type  string `json:"type"`
	Agent any    `json:"agent"`
}

// AgentPrompt delivers text to the target agent and, when Wait is set,
// blocks until the requested status (or timeout) — a send-then-wait
// sequence in one round trip.
func (c *Client) AgentPrompt(ctx context.Context, p AgentPromptParams) (AgentPromptResult, error) {
	var out AgentPromptResult
	err := c.call(ctx, "agent.prompt", p, &out)
	return out, err
}

// --- pane.split ------------------------------------------------------------

// PaneSplitParams splits a new pane off an existing one. Env is delivered
// to the new shell pane's environment at split time — this is the ONLY
// place herdr accepts env for the respawn sequence (README §5.6), since
// AgentStartParams carries no env field.
type PaneSplitParams struct {
	Direction    SplitDirection    `json:"direction"`
	Env          map[string]string `json:"env,omitempty"`
	CWD          string            `json:"cwd,omitempty"`
	Focus        bool              `json:"focus,omitempty"`
	Ratio        *float32          `json:"ratio,omitempty"`
	TargetPaneID string            `json:"target_pane_id,omitempty"`
	WorkspaceID  string            `json:"workspace_id,omitempty"`
}

// PaneInfo mirrors the subset of herdr's PaneInfo the keeper needs.
type PaneInfo struct {
	PaneID      string      `json:"pane_id"`
	TerminalID  string      `json:"terminal_id"`
	WorkspaceID string      `json:"workspace_id"`
	TabID       string      `json:"tab_id"`
	Focused     bool        `json:"focused"`
	CWD         string      `json:"cwd"`
	Agent       string      `json:"agent"`
	AgentStatus AgentStatus `json:"agent_status"`
	Revision    uint64      `json:"revision"`
}

// PaneSplitResult is the result of pane.split.
type PaneSplitResult struct {
	Type string   `json:"type"`
	Pane PaneInfo `json:"pane"`
}

// PaneSplit creates a new pane split off an existing one. Verified live:
// the result envelope is {"type":"pane_info","pane":{...}}.
func (c *Client) PaneSplit(ctx context.Context, p PaneSplitParams) (PaneSplitResult, error) {
	var out PaneSplitResult
	err := c.call(ctx, "pane.split", p, &out)
	return out, err
}

// --- pane.close ------------------------------------------------------------

// PaneClose closes the pane with the given id. Verified live: the result
// is the bare {"type":"ok"} envelope (no data).
func (c *Client) PaneClose(ctx context.Context, paneID string) error {
	return c.call(ctx, "pane.close", struct {
		PaneID string `json:"pane_id"`
	}{PaneID: paneID}, nil)
}

// --- pane.read -------------------------------------------------------------

// PaneReadParams reads pane output. StripAnsi defaults true on the wire
// when omitted; set it explicitly when the caller cares.
type PaneReadParams struct {
	PaneID    string     `json:"pane_id"`
	Source    ReadSource `json:"source"`
	Format    ReadFormat `json:"format,omitempty"`
	Lines     uint32     `json:"lines,omitempty"`
	StripAnsi *bool      `json:"strip_ansi,omitempty"`
}

// PaneReadResult is the result of pane.read.
type PaneReadResult struct {
	Type string `json:"type"`
	Read struct {
		PaneID      string     `json:"pane_id"`
		WorkspaceID string     `json:"workspace_id"`
		TabID       string     `json:"tab_id"`
		Source      ReadSource `json:"source"`
		Format      ReadFormat `json:"format"`
		Text        string     `json:"text"`
		Revision    uint64     `json:"revision"`
		Truncated   bool       `json:"truncated"`
	} `json:"read"`
}

// PaneRead reads the given pane's buffer. Verified live: the result
// envelope is {"type":"pane_read","read":{...}}.
func (c *Client) PaneRead(ctx context.Context, p PaneReadParams) (PaneReadResult, error) {
	var out PaneReadResult
	err := c.call(ctx, "pane.read", p, &out)
	return out, err
}

// --- pane.process_info -----------------------------------------------------

// PaneProcessInfoProcess is one process in a pane's foreground group.
type PaneProcessInfoProcess struct {
	PID     uint32   `json:"pid"`
	Name    string   `json:"name"`
	Argv0   string   `json:"argv0"`
	Argv    []string `json:"argv"`
	Cmdline string   `json:"cmdline"`
	CWD     string   `json:"cwd"`
}

// PaneProcessInfo is the process-tree probe result for one pane.
type PaneProcessInfo struct {
	PaneID                   string                   `json:"pane_id"`
	ShellPID                 uint32                   `json:"shell_pid"`
	TTY                      string                   `json:"tty"`
	ForegroundProcessGroupID uint32                   `json:"foreground_process_group_id"`
	ForegroundProcesses      []PaneProcessInfoProcess `json:"foreground_processes"`
}

// PaneProcessInfoResult is the result of pane.process_info.
type PaneProcessInfoResult struct {
	Type        string          `json:"type"`
	ProcessInfo PaneProcessInfo `json:"process_info"`
}

// PaneProcessInfo probes what is running in the foreground of paneID.
// Verified live: the result envelope is
// {"type":"pane_process_info","process_info":{...}}.
func (c *Client) PaneProcessInfo(ctx context.Context, paneID string) (PaneProcessInfoResult, error) {
	var out PaneProcessInfoResult
	err := c.call(ctx, "pane.process_info", struct {
		PaneID string `json:"pane_id,omitempty"`
	}{PaneID: paneID}, &out)
	return out, err
}

// --- pane.send_input -------------------------------------------------------

// PaneSendInputParams delivers raw text and/or named key events to a pane
// by pane id (as opposed to agent.send_keys, which targets by agent name).
type PaneSendInputParams struct {
	PaneID string   `json:"pane_id"`
	Text   string   `json:"text,omitempty"`
	Keys   []string `json:"keys,omitempty"`
}

// PaneSendInput writes text/keys into paneID's input stream. Verified
// live: the result is the bare {"type":"ok"} envelope.
func (c *Client) PaneSendInput(ctx context.Context, p PaneSendInputParams) error {
	return c.call(ctx, "pane.send_input", p, nil)
}

// --- pane.list ---------------------------------------------------------

// PaneListResult is the result of pane.list.
type PaneListResult struct {
	Type  string     `json:"type"`
	Panes []PaneInfo `json:"panes"`
}

// PaneList lists panes, optionally scoped to one workspace. Pass "" for
// workspaceID to list every pane on the server.
func (c *Client) PaneList(ctx context.Context, workspaceID string) (PaneListResult, error) {
	var out PaneListResult
	err := c.call(ctx, "pane.list", struct {
		WorkspaceID string `json:"workspace_id,omitempty"`
	}{WorkspaceID: workspaceID}, &out)
	return out, err
}

// --- session.snapshot --------------------------------------------------

// SessionSnapshotAgent mirrors the AgentInfo fields the keeper needs from a
// session snapshot.
type SessionSnapshotAgent struct {
	PaneID           string      `json:"pane_id"`
	WorkspaceID      string      `json:"workspace_id"`
	TabID            string      `json:"tab_id"`
	Name             string      `json:"name"`
	AgentStatus      AgentStatus `json:"agent_status"`
	InteractiveReady bool        `json:"interactive_ready"`
	LaunchPending    bool        `json:"launch_pending"`
	Focused          bool        `json:"focused"`
}

// SessionSnapshotResult is the result of session.snapshot.
type SessionSnapshotResult struct {
	Type     string `json:"type"`
	Snapshot struct {
		Version  string                 `json:"version"`
		Protocol uint32                 `json:"protocol"`
		Panes    []PaneInfo             `json:"panes"`
		Agents   []SessionSnapshotAgent `json:"agents"`
	} `json:"snapshot"`
}

// SessionSnapshot returns the full server-side state: workspaces, tabs,
// panes, layouts, and agents.
func (c *Client) SessionSnapshot(ctx context.Context) (SessionSnapshotResult, error) {
	var out SessionSnapshotResult
	err := c.call(ctx, "session.snapshot", struct{}{}, &out)
	return out, err
}

// --- pane.report_metadata -----------------------------------------------

// PaneReportMetadataParams pushes sidebar-visible metadata for a pane.
// Tokens is capped at 16 entries by the wire schema (maxProperties), each
// key matching ^[A-Za-z0-9_-]{1,32}$ — herdr rejects a caller that exceeds
// this, so P0 callers stay well under it.
type PaneReportMetadataParams struct {
	PaneID       string            `json:"pane_id"`
	Source       string            `json:"source"`
	Agent        string            `json:"agent,omitempty"`
	DisplayAgent string            `json:"display_agent,omitempty"`
	Title        string            `json:"title,omitempty"`
	StateLabels  map[string]string `json:"state_labels,omitempty"`
	Tokens       map[string]string `json:"tokens,omitempty"`
	TTLMs        uint64            `json:"ttl_ms,omitempty"`
}

// PaneReportMetadata pushes sidebar metadata for a pane. Verified against
// the live schema; result carries no data.
func (c *Client) PaneReportMetadata(ctx context.Context, p PaneReportMetadataParams) error {
	return c.call(ctx, "pane.report_metadata", p, nil)
}
