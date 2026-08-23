// Package projectconfig is the daemon-free leaf that loads and models
// .harmonik/config.yaml. parseProjectConfig is the pure boundary (path + bytes
// in, value out); LoadProjectConfig is the one edge (os.ReadFile + UserHomeDir).
// It imports only stdlib, gopkg.in/yaml.v3, and internal/core, so the run
// machine can hold config as an immutable value without linking the daemon.
package projectconfig

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/gregberns/harmonik/internal/core"
)

const projectConfigRelPath = ".harmonik/config.yaml"

const projectConfigCurrentVersion = 1

// ErrMalformedConfigYAML is returned when .harmonik/config.yaml is present but
// cannot be parsed as valid YAML, or contains structurally invalid content.
type ErrMalformedConfigYAML struct {
	// Path is the absolute path to the file.
	Path string
	// Cause is the underlying parse or structural error.
	Cause error
}

func (e *ErrMalformedConfigYAML) Error() string {
	return fmt.Sprintf("daemon: project config: malformed YAML in %s: %v", e.Path, e.Cause)
}

func (e *ErrMalformedConfigYAML) Unwrap() error { return e.Cause }

// ErrUnknownConfigKey is returned when the keeper: block (or any of its
// sub-blocks) carries a key that the schema does not recognise. Per the
// operator decision (hk-9f3f) unknown keeper keys are a HARD ERROR — they are
// no longer silently ignored, because silent-ignore masks a fat-fingered /
// typo'd key. The daemon and `harmonik keeper` MUST refuse to start when this
// error is returned; the operator fixes the offending key.
//
// KeyPath names the offending key as a dotted path rooted at the rejecting
// block (e.g. "keeper.context_thresholds.warn_abs_token").
//
// Scope: this strict-rejection applies to the keeper: block and to entries under
// the subsystems: block (see subsystems.go). The daemon: block remains tolerant
// of unknown sibling keys per the PL-004b spec requirement
// (specs/process-lifecycle.md §4.1).
//
// Bead ref: hk-9f3f.
type ErrUnknownConfigKey struct {
	// Path is the absolute path to the file.
	Path string
	// KeyPath is the dotted path to the offending key, rooted at the block that
	// rejected it (e.g. "keeper.context_thresholds.warn_abs_token" or
	// "subsystems.reconciliation_scheduler.enbaled").
	KeyPath string
	// Cause is the underlying strict-decode error (carries the yaml.v3 message).
	Cause error
}

// Error names the block from KeyPath's first segment rather than hardcoding
// "keeper:" — the strict-rejection now covers more than one block, and telling
// an operator to look at the wrong block is worse than no hint at all.
func (e *ErrUnknownConfigKey) Error() string {
	block, _, _ := strings.Cut(e.KeyPath, ".")
	if block == "" {
		block = "config"
	}
	return fmt.Sprintf("daemon: project config: unknown key %q under %s: in %s (unknown %s keys are rejected; fix the key)",
		e.KeyPath, block, e.Path, block)
}

func (e *ErrUnknownConfigKey) Unwrap() error { return e.Cause }

// ErrUnsupportedConfigVersion is returned when .harmonik/config.yaml declares a
// schema_version other than projectConfigCurrentVersion (1).
type ErrUnsupportedConfigVersion struct {
	// Path is the absolute path to the file.
	Path string
	// Version is the declared version.
	Version int
}

func (e *ErrUnsupportedConfigVersion) Error() string {
	return fmt.Sprintf("daemon: project config: unsupported schema_version %d in %s (want %d)",
		e.Version, e.Path, projectConfigCurrentVersion)
}

// ErrWorkflowModeFloorViolation is returned when .harmonik/config.yaml carries
// daemon.workflow_mode: single, violating the PL-004a review floor. The daemon
// MUST refuse to start (fail-fast) when this error is returned.
//
// A legacy per-bead workflow:single label selects the registered no-review DOT
// graph and is audited via the review_bypassed event per PL-004a. It does not
// select a single-mode dispatcher.
//
// Spec ref: specs/process-lifecycle.md §4.1 PL-004a, PL-004b.
// Bead ref: hk-rcp7.
type ErrWorkflowModeFloorViolation struct {
	// Path is the absolute path to the file.
	Path string
	// Value is the disallowed workflow_mode string (always "single").
	Value string
}

func (e *ErrWorkflowModeFloorViolation) Error() string {
	return fmt.Sprintf(
		"daemon: project config: daemon.workflow_mode %q in %s violates the PL-004a review floor: "+
			"single is not a valid daemon-level default; a legacy per-bead workflow:single label selects the no-review DOT graph",
		e.Value, e.Path,
	)
}

type rawDaemonConfig struct {
	WorkflowMode        string                        `yaml:"workflow_mode"`
	MaxConcurrent       int                           `yaml:"max_concurrent"`
	TargetBranch        string                        `yaml:"target_branch"`         // observability/symmetry only per PL-004b
	AllowedRepos        []string                      `yaml:"allowed_repos"`         // cross-repo dispatch safelist (hk-xfuc)
	RemoteControlPrefix string                        `yaml:"remote_control_prefix"` // per-project Claude RC session-label prefix (hk-igpg)
	RestartBackoff      rawDaemonRestartBackoffConfig `yaml:"restart_backoff"`       // rapid boot-record backoff (hk-b82kn)
}

type rawDaemonRestartBackoffConfig struct {
	Base   string `yaml:"base"`
	Cap    string `yaml:"cap"`
	Window string `yaml:"window"`
}

type rawKeeperContextThresholds struct {
	WarnAbsTokens      int64   `yaml:"warn_abs_tokens"`
	ActAbsTokens       int64   `yaml:"act_abs_tokens"`
	ForceActAbsTokens  int64   `yaml:"force_act_abs_tokens"`
	ForceActAbsOffset  int64   `yaml:"force_act_abs_offset"`
	IdleFloorAbsTokens int64   `yaml:"idle_floor_abs_tokens"`
	ActPctCeil         float64 `yaml:"act_pct_ceil"`
	WarnPctCeil        float64 `yaml:"warn_pct_ceil"`
}

type rawKeeperHardCeiling struct {
	Mode      string `yaml:"mode"`
	AbsTokens int64  `yaml:"abs_tokens"`
	Cooldown  string `yaml:"cooldown"`
}

type rawKeeperTimings struct {
	PollInterval       string `yaml:"poll_interval"`
	CyclerPollInterval string `yaml:"cycler_poll_interval"` // hk-4gtu: distinct from poll_interval (watcher)
	IdleQuiesce        string `yaml:"idle_quiesce"`
	Staleness          string `yaml:"staleness"`
	HandoffTimeout     string `yaml:"handoff_timeout"`
	ClearSettle        string `yaml:"clear_settle"`
	BootGrace          string `yaml:"boot_grace"`
	MaxBootGraceTotal  string `yaml:"max_boot_grace_total"`
	FlockAcquireGrace  string `yaml:"flock_acquire_grace"` // hk-qgfme: crew keeper post-spawn liveness probe bound
}

type rawKeeperCadence struct {
	WarnCooldown         string `yaml:"warn_cooldown"`
	NoGaugeBackoff       string `yaml:"no_gauge_backoff"`
	RespawnGrace         string `yaml:"respawn_grace"`
	RespawnCooldown      string `yaml:"respawn_cooldown"`
	LiveRecoverGrace     string `yaml:"live_recover_grace"`
	LiveRecoverCooldown  string `yaml:"live_recover_cooldown"`
	ForceRetryInterval   string `yaml:"force_retry_interval"`
	IdleRestartCooldown  string `yaml:"idle_restart_cooldown"`
	HardCeilingCooldown  string `yaml:"hard_ceiling_cooldown"`
	BlindKeeperThreshold string `yaml:"blind_keeper_threshold"`
	HoldTTL              string `yaml:"hold_ttl"`
	ReapDecisionsCadence string `yaml:"reap_decisions_cadence"`
	// hk-74iyd: conversation-aware ACT suppression.
	OperatorTurnLookback string `yaml:"operator_turn_lookback"`
	PostAnswerGrace      string `yaml:"post_answer_grace"`
}

type rawKeeperBudgets struct {
	HeartbeatMaxMisses int `yaml:"heartbeat_max_misses"`
	MaxHandoffTimeouts int `yaml:"max_handoff_timeouts"`
}

type rawKeeperSelfService struct {
	Enabled              bool  `yaml:"enabled"`
	GraceSeconds         int   `yaml:"grace_seconds"`
	InstructOnlyWhenIdle bool  `yaml:"instruct_only_when_idle"`
	CrewsEnabled         *bool `yaml:"crews_enabled"`
}

type rawKeeperWarnMessages struct {
	DefaultWarnText    string `yaml:"default_warn_text"`
	OnDemandWarnText   string `yaml:"on_demand_warn_text"`
	ActionableWarnText string `yaml:"actionable_warn_text"`
	SettleWarnText     string `yaml:"settle_warn_text"`
	// LeaderDeferText overrides the compiled-in leader defer-message body (the
	// K2 finish-then-self-restart nudge). Empty = compiled default. The four
	// SK-026 structural slots are validated/filled by T3 (SK-033); T2 only
	// carries the override text. Spec: session-keeper.md §4.14 SK-032.
	LeaderDeferText string `yaml:"leader_defer_text"`
	// CrewDeferText overrides the crew keeper-message body (K7). Empty/off by
	// default: T2 ships only the config hook — nothing consumes this yet, and
	// crew self-restart stays gated on self_service.crews_enabled (default-off)
	// AND the external keeper-reliability activation gate. Spec: SK-032;
	// park-resume-protocol.md §9 (K7 — DEFERRED).
	CrewDeferText string `yaml:"crew_defer_text"`
}

type rawKeeperConfig struct {
	ContextThresholds rawKeeperContextThresholds `yaml:"context_thresholds"`
	HardCeiling       rawKeeperHardCeiling       `yaml:"hard_ceiling"`
	Timings           rawKeeperTimings           `yaml:"timings"`
	Cadence           rawKeeperCadence           `yaml:"cadence"`
	Budgets           rawKeeperBudgets           `yaml:"budgets"`
	SelfService       rawKeeperSelfService       `yaml:"self_service"`
	WarnMessages      rawKeeperWarnMessages      `yaml:"warn_messages"`
}

func keeperBlockAbsent(raw rawKeeperConfig) bool {
	t := raw.ContextThresholds
	h := raw.HardCeiling
	tm := raw.Timings
	c := raw.Cadence
	b := raw.Budgets
	s := raw.SelfService
	w := raw.WarnMessages
	return t.WarnAbsTokens == 0 &&
		t.ActAbsTokens == 0 &&
		t.ForceActAbsTokens == 0 &&
		t.ForceActAbsOffset == 0 &&
		t.IdleFloorAbsTokens == 0 &&
		t.ActPctCeil == 0 &&
		t.WarnPctCeil == 0 &&
		h.Mode == "" &&
		h.AbsTokens == 0 &&
		h.Cooldown == "" &&
		tm.PollInterval == "" &&
		tm.CyclerPollInterval == "" &&
		tm.IdleQuiesce == "" &&
		tm.Staleness == "" &&
		tm.HandoffTimeout == "" &&
		tm.ClearSettle == "" &&
		tm.BootGrace == "" &&
		tm.MaxBootGraceTotal == "" &&
		tm.FlockAcquireGrace == "" &&
		c.WarnCooldown == "" &&
		c.NoGaugeBackoff == "" &&
		c.RespawnGrace == "" &&
		c.RespawnCooldown == "" &&
		c.LiveRecoverGrace == "" &&
		c.LiveRecoverCooldown == "" &&
		c.ForceRetryInterval == "" &&
		c.IdleRestartCooldown == "" &&
		c.HardCeilingCooldown == "" &&
		c.BlindKeeperThreshold == "" &&
		c.HoldTTL == "" &&
		c.ReapDecisionsCadence == "" &&
		c.OperatorTurnLookback == "" &&
		c.PostAnswerGrace == "" &&
		b.HeartbeatMaxMisses == 0 &&
		b.MaxHandoffTimeouts == 0 &&
		!s.Enabled &&
		s.GraceSeconds == 0 &&
		!s.InstructOnlyWhenIdle &&
		s.CrewsEnabled == nil &&
		w.DefaultWarnText == "" &&
		w.OnDemandWarnText == "" &&
		w.ActionableWarnText == "" &&
		w.SettleWarnText == "" &&
		w.LeaderDeferText == "" &&
		w.CrewDeferText == ""
}

// KeeperConfigPresence records, key-by-key, whether the operator SUPPLIED a value
// in the keeper: block — independent of whether the parsed value is the zero value.
// It is the presence signal the operator-facing resolver (cmd/harmonik.ResolveKeeperConfig)
// needs to distinguish "unset" (→ MISSING, refuse to start) from an explicit value
// that happens to be zero (e.g. boot_grace: 0s = "disable boot grace", which is a
// LEGITIMATE explicit choice, not a missing key).
//
// For duration fields the raw config value is a STRING in rawKeeper* (empty = absent),
// so a non-empty string = present even when it parses to 0 (e.g. "0s"). For
// numeric/pct fields the raw value > 0 = present (a threshold of 0 is never meaningful,
// so > 0 is the right presence test). For the mode it is the non-empty string.
//
// This struct exists so the keeper no longer silently applies compiled defaults for
// unset values: under the operator-philosophy change (no product-imposed defaults at
// runtime), an unset required value makes the keeper REFUSE TO START. Refs: keeper
// operator-required-config change.
type KeeperConfigPresence struct {
	WarnAbsTokens        bool
	ActAbsTokens         bool
	ForceActAbsTokens    bool
	ForceActAbsOffset    bool
	IdleFloorAbsTokens   bool
	ActPctCeil           bool
	WarnPctCeil          bool
	HardCeilingMode      bool
	HardCeilingAbsTokens bool

	PollInterval       bool
	CyclerPollInterval bool
	IdleQuiesce        bool
	Staleness          bool
	HandoffTimeout     bool
	ClearSettle        bool
	BootGrace          bool // true even for "0s" (explicit disable)
	FlockAcquireGrace  bool // true even for "0s" (explicit disable); hk-qgfme

	WarnCooldown         bool
	NoGaugeBackoff       bool
	RespawnGrace         bool
	RespawnCooldown      bool
	LiveRecoverGrace     bool
	LiveRecoverCooldown  bool
	ForceRetryInterval   bool
	IdleRestartCooldown  bool
	HardCeilingCooldown  bool
	BlindKeeperThreshold bool
	HoldTTL              bool
	ReapDecisionsCadence bool
	OperatorTurnLookback bool // hk-74iyd: auto-hold on recent operator turn
	PostAnswerGrace      bool // hk-74iyd: grace delay after agent's last text response

	HeartbeatMaxMisses bool
	MaxHandoffTimeouts bool
}

// KeeperConfig holds the keeper-level configuration read from the
// .harmonik/config.yaml keeper: block. All fields are optional in the file;
// zero/empty values signal "not configured — defer to CLI flag or built-in default".
// Precedence: CLI flag > config.yaml > compiled default (hk-lhu2).
//
// Present (KeeperConfigPresence) records WHICH keys the operator actually supplied,
// independent of the parsed zero value, so the operator-facing resolver can refuse
// to start on a missing required value rather than silently defaulting.
//
// Bead ref: hk-lhu2.
type KeeperConfig struct {
	// Present records which keeper keys the operator supplied (see KeeperConfigPresence).
	Present KeeperConfigPresence

	// WarnAbsTokens is the absolute warn threshold. Zero = not configured.
	WarnAbsTokens int64
	// ActAbsTokens is the absolute act threshold. Zero = not configured.
	ActAbsTokens int64
	// ForceActAbsTokens is the hard forced-clear ceiling. Zero = not configured.
	ForceActAbsTokens int64
	// ForceActAbsOffset is the offset above act used to derive the force-act gate
	// when ForceActAbsTokens is unset. Zero = not configured.
	ForceActAbsOffset int64
	// IdleFloorAbsTokens is the floor below which an idle large-context crew is
	// not idle-restarted. Zero = not configured.
	IdleFloorAbsTokens int64
	// ActPctCeil caps the act gate as a fraction of window size. Zero = not configured.
	ActPctCeil float64
	// WarnPctCeil caps the warn gate as a fraction of window size. Zero = not configured.
	WarnPctCeil float64

	// HardCeilingMode is the hard-ceiling behaviour: off|alarm|restart.
	// Empty = not configured (use compiled default).
	HardCeilingMode string
	// HardCeilingAbsTokens is the hard-ceiling token trigger. Zero = not configured.
	HardCeilingAbsTokens int64
	// HardCeilingCooldownDur is the hard-ceiling re-trigger cooldown. Zero = not configured.
	HardCeilingCooldownDur time.Duration

	// Timings (all zero = not configured).
	PollInterval       time.Duration
	CyclerPollInterval time.Duration // hk-4gtu: distinct from the watcher PollInterval
	IdleQuiesce        time.Duration
	Staleness          time.Duration
	HandoffTimeout     time.Duration
	ClearSettle        time.Duration
	BootGrace          time.Duration
	MaxBootGraceTotal  time.Duration
	// FlockAcquireGrace is the post-spawn liveness probe bound for crew keepers.
	// The daemon polls LiveKeeperPresent for up to this duration after
	// SpawnCrewSession; if the flock is never held, a session_keeper_watcher_dead
	// event + keeper-alert comms fire. Zero = probe disabled (not configured).
	// Refs: hk-qgfme.
	FlockAcquireGrace time.Duration

	// Cadence (all zero = not configured).
	WarnCooldown               time.Duration
	NoGaugeBackoff             time.Duration
	RespawnGrace               time.Duration
	RespawnCooldown            time.Duration
	LiveRecoverGrace           time.Duration
	LiveRecoverCooldown        time.Duration
	ForceRetryInterval         time.Duration
	IdleRestartCooldown        time.Duration
	CadenceHardCeilingCooldown time.Duration
	BlindKeeperThreshold       time.Duration
	HoldTTL                    time.Duration
	ReapDecisionsCadence       time.Duration
	// OperatorTurnLookback is the max age of an inbound operator user turn that
	// triggers an auto-hold: ACT is deferred when a real user turn landed within
	// this window. Zero = not configured (hk-74iyd).
	OperatorTurnLookback time.Duration
	// PostAnswerGrace is the min duration after the agent's last real assistant
	// text response before ACT may fire. Zero = not configured (hk-74iyd).
	PostAnswerGrace time.Duration

	// Budgets (zero = not configured).
	HeartbeatMaxMisses int
	MaxHandoffTimeouts int

	// SelfService.
	SelfServiceEnabled              bool
	SelfServiceGraceSeconds         int
	SelfServiceInstructOnlyWhenIdle bool
	// SelfServiceCrewsEnabled is nil when keeper.self_service.crews_enabled is
	// ABSENT and non-nil (the parsed bool) when present. The operator decision
	// (hk-vs4u) resolves an ABSENT key to TRUE — crews self-restart by default — so
	// the nil/non-nil distinction is preserved here and resolved in
	// ResolveKeeperConfig. Refs: hk-vs4u.
	SelfServiceCrewsEnabled *bool

	// DefaultWarnText overrides the compiled-in wrap-up advisory for non-captain agents.
	// Empty = not configured (use compiled default).
	DefaultWarnText string
	// ActionableWarnText overrides the compiled-in actionable self-service warn
	// advisory (the R3 restart handshake). Empty = not configured (use compiled
	// default). This is the SINGLE warn-text key for the actionable advisory; the
	// deprecated keeper.warn_messages.on_demand_warn_text ALIASES onto it (with a log
	// warning) and is kept as a RECOGNIZED key so old strict configs (hk-9f3f) do not
	// hard-error. Refs: hk-vs4u, hk-lhu2.
	ActionableWarnText string
	// SettleWarnText overrides the second-band checkpoint warning.
	SettleWarnText string
	// LeaderDeferText overrides the compiled-in leader defer-message body (K2
	// finish-then-self-restart nudge). Empty = compiled default. Carried to
	// WatcherConfig; the four SK-026 structural slots are filled/validated by T3.
	// Sourced from keeper.warn_messages.leader_defer_text. Refs: SK-032.
	LeaderDeferText string
	// CrewDeferText overrides the crew keeper-message body (K7). Empty/off by
	// default; T2 ships only the config hook (nothing consumes it yet), with crew
	// activation gated on self_service.crews_enabled (default-off). Sourced from
	// keeper.warn_messages.crew_defer_text. Refs: SK-032, park-resume-protocol §9.
	CrewDeferText string
}

// DaemonConfig holds the daemon-level operational configuration read from the
// .harmonik/config.yaml daemon: block. All fields are optional in the file;
// zero values signal "not configured — defer to CLI flag or built-in default".
//
// Spec ref: specs/process-lifecycle.md §4.1 PL-004b.
// Bead ref: hk-rcp7.
type DaemonConfig struct {
	// WorkflowMode is the daemon-level default workflow mode.
	// Empty = not configured (defer to --workflow-mode flag or dot default per PL-004a).
	// WorkflowModeSingle is NEVER a valid config value; LoadProjectConfig returns
	// *ErrWorkflowModeFloorViolation when it is found (PL-004a review floor).
	WorkflowMode core.WorkflowMode

	// MaxConcurrent is the daemon-level max-concurrent dispatch ceiling.
	// Zero = not configured (defer to --max-concurrent flag or its default).
	// Values ≤ 0 in the file are treated as not configured per PL-004b.
	MaxConcurrent int

	// TargetBranch is the daemon-level target branch value as written in config.yaml.
	// This field is observability/symmetry only per PL-004b: it MUST NOT override
	// the branching.yaml lands_on value in the resolution chain. Callers MUST use
	// branching.Load() for the authoritative target_branch.
	TargetBranch string

	// AllowedRepos is the safelist of absolute repository paths the daemon is
	// permitted to dispatch cross-repo beads against (hk-xfuc). A bead whose
	// target_repo is not in this list is refused with CrossRepoUnsafeError.
	// An empty list means no cross-repo dispatch is allowed.
	// See docs/cross-repo-dispatch.md.
	AllowedRepos []string

	// RemoteControlPrefix is the per-project prefix folded into the Claude Code
	// --remote-control session LABEL (e.g. "hk" → label "hk-paul"). It disambiguates
	// the global-per-host Remote-Control session picker when multiple harmonik
	// projects run concurrently. Empty = not configured ⇒ the bare agent name is
	// emitted exactly as today (backward compatible). It is a COSMETIC label only:
	// harmonik's own identity keys (HARMONIK_AGENT, crew-registry name, tmux name,
	// --session-id) stay bare. Use crewrun.JoinRemoteControlName to build the
	// label so the format never drifts between launch sites. (hk-igpg)
	RemoteControlPrefix string

	// RestartBackoff configures the persistent boot-record backoff applied when
	// the daemon restarts rapidly. Zero fields mean "not configured"; startup
	// resolves each missing field to the compiled defaults in restartbackoff.go.
	// Refs: hk-b82kn.
	RestartBackoff DaemonRestartBackoffConfig
}

// DaemonRestartBackoffConfig holds daemon.restart_backoff duration overrides.
// Zero fields mean not configured and resolve to current compiled defaults.
type DaemonRestartBackoffConfig struct {
	Base   time.Duration
	Cap    time.Duration
	Window time.Duration
}

type rawSandboxNetworkConfig struct {
	Mode                   string   `yaml:"mode"`
	AllowedDomains         []string `yaml:"allowed_domains"`
	WeakerNetworkIsolation bool     `yaml:"weaker_network_isolation"`
	AllowLocalBinding      bool     `yaml:"allow_local_binding"`
}

type rawSandboxCacheConfig struct {
	WarmRead     []string `yaml:"warm_read"`
	PrivateWrite []string `yaml:"private_write"`
}

type rawSandboxConfig struct {
	Backend   string                  `yaml:"backend"`
	Harnesses []string                `yaml:"harnesses"`
	Network   rawSandboxNetworkConfig `yaml:"network"`
	Cache     rawSandboxCacheConfig   `yaml:"cache"`
}

// SandboxNetworkConfig holds the network sub-block of the sandbox: config.
type SandboxNetworkConfig struct {
	// Mode is the network mode. v1 value = "open" (locked).
	Mode string
	// AllowedDomains is the list of HTTPS domains permitted outbound by the sandbox.
	// Nil/empty = no outbound HTTPS.
	AllowedDomains []string
	// WeakerNetworkIsolation, when true, enables srt's weaker-network-isolation mode.
	// Per the TLS decision (plans/2026-07-02-pi-sandbox/SPIKE-FINDINGS-hk-f39ny.md §TLS
	// DECISION), v1 keeps this false; the field is stored verbatim from config.
	WeakerNetworkIsolation bool
	// AllowLocalBinding, when true, sets srt's network.allowLocalBinding, permitting
	// the sandboxed process to open direct sockets to local / private-LAN / loopback
	// addresses. REQUIRED to reach a locally-hosted OpenAI-compatible model endpoint
	// (e.g. a DGX vLLM on the LAN): such addresses fall in srt's no_proxy set and are
	// connected to directly, so the allowedDomains proxy path does not cover them and
	// Seatbelt denies the socket unless local binding is permitted. Default false.
	// Bead: hk-ybuts / hk-u69my.
	AllowLocalBinding bool
}

// SandboxCacheConfig holds the cache sub-block of the sandbox: config.
type SandboxCacheConfig struct {
	// WarmRead is the list of shared read-only toolchain cache directories included in
	// srt's allowRead set. These are NEVER writable from inside the sandbox to avoid
	// the concurrent-writer TOCTOU class (cache-reaper TOCTOU incident).
	WarmRead []string
	// PrivateWrite is the list of per-run private cache directories included in srt's
	// allowWrite set. Unlike WarmRead these are per-run: they are never shared with
	// concurrent runs, eliminating concurrent-write races.
	PrivateWrite []string
}

// SandboxConfig holds the sandbox: top-level config block read from
// .harmonik/config.yaml. Absent = zero value (Backend==""); callers check
// Backend != "" to determine whether the block was present.
//
// When present, Backend is REQUIRED (fail-loud per the no-hardcoded-defaults
// principle). Valid values: "srt" (argv-wrap via @anthropic-ai/sandbox-runtime)
// and "none" (explicit opt-out, equivalent to absent block but auditable).
//
// Bead ref: hk-6596l.
type SandboxConfig struct {
	// Backend is the sandbox mechanism: "srt" or "none". REQUIRED when the
	// sandbox: block is present; empty only when the block is absent.
	Backend string
	// Harnesses is the list of harness names (agent types) that run under the
	// sandbox. E.g. ["pi"]. Empty = no runs are sandboxed even when Backend="srt".
	Harnesses []string
	// Network holds the network isolation sub-config.
	Network SandboxNetworkConfig
	// Cache holds the cache access sub-config.
	Cache SandboxCacheConfig
}

// HasHarness reports whether name is in the sandbox.harnesses list.
func (c SandboxConfig) HasHarness(name string) bool {
	for _, h := range c.Harnesses {
		if h == name {
			return true
		}
	}
	return false
}

func agentTypeNamesForError() string {
	reserved := core.ReservedAgentTypes()
	names := make([]string, 0, len(reserved))
	for _, a := range reserved {
		names = append(names, string(a))
	}
	return strings.Join(names, ", ")
}

func nearestAgentType(got string) (string, bool) {
	if got == "" {
		return "", false
	}
	var match string
	for _, a := range core.ReservedAgentTypes() {
		if strings.HasPrefix(string(a), got) {
			if match != "" {
				return "", false // ambiguous — say nothing rather than guess
			}
			match = string(a)
		}
	}
	return match, match != ""
}

func sandboxBlockAbsent(raw rawSandboxConfig) bool {
	return raw.Backend == "" &&
		len(raw.Harnesses) == 0 &&
		raw.Network.Mode == "" &&
		len(raw.Network.AllowedDomains) == 0 &&
		!raw.Network.WeakerNetworkIsolation &&
		!raw.Network.AllowLocalBinding &&
		len(raw.Cache.WarmRead) == 0 &&
		len(raw.Cache.PrivateWrite) == 0
}

type rawHarnessesPiFallbackConfig struct {
	Provider  string `yaml:"provider"`
	Model     string `yaml:"model"`
	APIKeyEnv string `yaml:"api_key_env"`
}

type rawHarnessesPiProfileConfig struct {
	Provider   string `yaml:"provider"`
	Model      string `yaml:"model"`
	APIKeyEnv  string `yaml:"api_key_env"`
	APIKeyFile string `yaml:"api_key_file"` // OPTIONAL
	BaseURL    string `yaml:"base_url"`     // OPTIONAL
	API        string `yaml:"api"`          // OPTIONAL; defaulted at launch, not here
}

type rawHarnessesPiConfig struct {
	Provider   string                                 `yaml:"provider"`
	Model      string                                 `yaml:"model"`
	APIKeyEnv  string                                 `yaml:"api_key_env"`
	APIKeyFile string                                 `yaml:"api_key_file"` // OPTIONAL; PI-050/hk-xmfoi
	BaseURL    string                                 `yaml:"base_url"`     // OPTIONAL; locally-hosted OpenAI-compatible endpoints (hk-z13jz)
	API        string                                 `yaml:"api"`          // OPTIONAL; Pi wire format, defaults to "openai" at launch when empty (hk-z13jz)
	Fallback   rawHarnessesPiFallbackConfig           `yaml:"fallback"`
	Profiles   map[string]rawHarnessesPiProfileConfig `yaml:"profiles"` // OPTIONAL; pi-provider-switch

	// ProviderSlots is the OPTIONAL per-provider concurrency ceiling
	// (docs/design/pi-multi-provider-slot-accounting.md, hk-8ziid). Keyed by
	// the resolved `provider` string (the same value a profile's `provider`
	// field or the top-level `provider` field carries — e.g. "openrouter",
	// "ornith" — NOT the profile name), value is the max number of
	// simultaneously in-flight runs the daemon will dispatch to that provider.
	// A provider with no entry (or entry <= 0) is unbounded — gated only by
	// the existing global max_concurrent and per-queue Workers ceilings,
	// preserving today's behavior byte-for-byte when this block is absent.
	ProviderSlots map[string]int `yaml:"provider_slots"`
}

type rawHarnessesConfig struct {
	Pi rawHarnessesPiConfig `yaml:"pi"`
}

// PiFallbackConfig holds the optional harnesses.pi.fallback sub-block.
// V1 has no automatic fallback — this block exists for operator convenience
// (manual lane flip); PI-072.
type PiFallbackConfig struct {
	// Provider is the fallback provider string.
	Provider string
	// Model is the fallback model string.
	Model string
	// APIKeyEnv is the name of the env var carrying the fallback provider key.
	APIKeyEnv string
}

// PiProfileConfig is one resolved named profile — a full switchable tuple.
// Same shape+opacity discipline as the top-level PiHarnessConfig fields.
type PiProfileConfig struct {
	// Provider is the provider string for this profile. REQUIRED.
	Provider string
	// Model is the model string for this profile. REQUIRED.
	Model string
	// APIKeyEnv is the env var name carrying this profile's provider key. REQUIRED.
	APIKeyEnv string
	// APIKeyFile is the OPTIONAL path; expanded from ~ by ResolvePiConfig when set.
	APIKeyFile string
	// BaseURL is the OPTIONAL base URL for a locally-hosted endpoint.
	BaseURL string
	// API is the OPTIONAL wire-format string; defaulted at launch by buildPiModelsJSON.
	API string
}

// PiHarnessConfig holds the harnesses.pi block read from .harmonik/config.yaml.
// REQUIRED fields are provider, model, api_key_env; missing required fields are
// caught by ResolvePiConfig (cmd/harmonik/resolve_pi_config.go, PI-051).
// The product imposes ZERO baked Pi defaults — PI-050 / R1 de-hardcode mandate.
//
// Spec refs: PI-050, PI-051, PI-052, specs/pi-harness.md §5.
// Bead ref: hk-v7q5u.
type PiHarnessConfig struct {
	// Provider is the Pi provider string. REQUIRED — no default.
	Provider string
	// Model is the Pi model string. REQUIRED — no default. Shape-validated only
	// (HC-055a: ^[A-Za-z0-9._:/-]+$, ≤128 chars). Never value-validated.
	Model string
	// APIKeyEnv is the name of the env var carrying the provider API key.
	// REQUIRED — no default. The KEY VALUE is never stored in config.
	APIKeyEnv string
	// APIKeyFile is the OPTIONAL path to a file holding the raw provider API key.
	// When set (expanded from ~ by ResolvePiConfig), the key is read from this
	// file at launch time and injected into the Pi child env ONLY — the daemon
	// ambient env never carries the secret. Precedence: file > ambient env.
	// ResolvePiConfig validates readable+non-empty at resolve time (fail loud).
	// Spec: PI-050 (api_key_file). Bead: hk-xmfoi.
	APIKeyFile string
	// BaseURL is the OPTIONAL base URL for locally-hosted OpenAI-compatible
	// endpoints (e.g. http://127.0.0.1:8551/v1 — a host name on the LAN does not
	// work from inside the sandbox, so a remote model box is tunnelled to
	// loopback). When set, buildPiLaunchSpec
	// generates a models.json with this baseUrl and injects PI_CODING_AGENT_DIR.
	// Absent = today's cloud-provider behavior unchanged. Shape-validated by
	// ResolvePiConfig when present (scheme://host[:port][/path], ≤512 chars).
	// Bead: hk-z13jz.
	BaseURL string
	// API is the OPTIONAL Pi wire-format string (e.g. "openai"). When empty and
	// BaseURL is set, buildPiLaunchSpec defaults to "openai" in the generated
	// models.json. No validation needed. Bead: hk-z13jz.
	API string
	// Fallback is the optional paid-fallback target (V1 manual flip; PI-072).
	Fallback PiFallbackConfig
	// HasFallback is true when the fallback: sub-block was present in config.
	HasFallback bool
	// Profiles are named switchable {provider,model,api_key_env,...} bundles,
	// selected per-bead by a `profile:<name>` label (pi-provider-switch). Nil/empty
	// map = no profiles defined (default path unaffected). Validated by ResolvePiConfig.
	Profiles map[string]PiProfileConfig

	// ProviderSlots is the OPTIONAL per-provider concurrency ceiling, keyed by
	// resolved provider string (not profile name). See
	// docs/design/pi-multi-provider-slot-accounting.md. Nil/empty map = every
	// provider unbounded (today's behavior; the global/per-queue ceilings still
	// apply). Consumed by RunRegistry.LenForProvider at the dispatch gate
	// (hk-8ziid.3, C3 — not yet wired as of this design bead).
	ProviderSlots map[string]int
}

// HarnessesConfig holds the harnesses: top-level block. Zero value when absent.
//
// Bead ref: hk-v7q5u.
type HarnessesConfig struct {
	Pi PiHarnessConfig
}

type rawOpsmonitorConfig struct {
	Interval   string `yaml:"interval"`    // Go duration string; empty = default "5m"
	ScriptPath string `yaml:"script_path"` // script path; empty = default "scripts/ops-monitor-check.sh"
}

// OpsmonitorConfig holds the opsmonitor schedule overrides read from the
// opsmonitor: block in .harmonik/config.yaml. Zero values mean "not configured";
// callers apply the compiled defaults ("5m" interval, "scripts/ops-monitor-check.sh" path).
//
// Bead ref: hk-bi4bg.
type OpsmonitorConfig struct {
	// Interval is the Go duration string for the ops-monitor schedule tick.
	// Empty = not configured → callers use the "5m" default.
	Interval string
	// ScriptPath is the path to the ops-monitor check script (passed as the
	// second argument to bash). Empty = not configured → callers use
	// "scripts/ops-monitor-check.sh".
	ScriptPath string
}

type rawWatchdogConfig struct {
	Enabled *bool `yaml:"enabled"`
}

type rawSuperviseBackoffConfig struct {
	Base string `yaml:"base"`
	Cap  string `yaml:"cap"`
}

type rawSuperviseDaemonWatchdogConfig struct {
	CheckInterval string `yaml:"check_interval"`
	DialTimeout   string `yaml:"dial_timeout"`
	ReviveBackoff string `yaml:"revive_backoff"`
	ReviveWindow  string `yaml:"revive_window"`
}

type rawSuperviseConfig struct {
	HeartbeatTTL        string                           `yaml:"heartbeat_ttl"`
	StartTimeout        string                           `yaml:"start_timeout"`
	CrashLoopWindow     string                           `yaml:"crash_loop_window"`
	HealthProbeInterval string                           `yaml:"health_probe_interval"`
	StopTimeout         string                           `yaml:"stop_timeout"`
	RestartBackoff      rawSuperviseBackoffConfig        `yaml:"restart_backoff"`
	DaemonWatchdog      rawSuperviseDaemonWatchdogConfig `yaml:"daemon_watchdog"`
}

// SuperviseDaemonWatchdogConfig holds the daemon-watchdog timings read from
// .harmonik/config.yaml supervise.daemon_watchdog. Zero means not configured:
// callers defer to the compiled defaults in internal/supervise.
type SuperviseDaemonWatchdogConfig struct {
	CheckInterval time.Duration
	DialTimeout   time.Duration
	ReviveBackoff time.Duration
	ReviveWindow  time.Duration
}

// SuperviseConfig holds supervisor/flywheel timings read from the supervise:
// block in .harmonik/config.yaml. All fields are optional; zero means not
// configured so callers keep the existing compiled defaults.
type SuperviseConfig struct {
	HeartbeatTTL        time.Duration
	StartTimeout        time.Duration
	CrashLoopWindow     time.Duration
	HealthProbeInterval time.Duration
	StopTimeout         time.Duration
	RestartBackoffBase  time.Duration
	RestartBackoffCap   time.Duration
	DaemonWatchdog      SuperviseDaemonWatchdogConfig
}

// WatchdogConfig holds the resolved watchdog configuration (hk-sbitr).
// Enabled is true by default (absent watchdog: block → watchdog runs).
type WatchdogConfig struct {
	// Enabled gates the ctx-watchdog auto-relaunch schedule. Default: true.
	// Set watchdog.enabled: false in .harmonik/config.yaml to opt out.
	Enabled bool
}

type rawWatchConfig struct {
	StatusTarget            string `yaml:"status_target"`
	OpsmonitorTarget        string `yaml:"opsmonitor_target"`
	AbsentThreshSec         int    `yaml:"absent_thresh_s"`           // WE9: seconds before watch-down fires (fail-loud)
	StallTicks              int    `yaml:"stall_ticks"`               // WE9: frozen-cursor ticks before watch-stalled (fail-loud)
	LivenessInterval        string `yaml:"liveness_interval"`         // WE6: Go duration string for mutual-liveness ping (fail-loud)
	DigestInterval          string `yaml:"digest_interval"`           // WE6: Go duration string for verify-services-up (fail-loud)
	StaffingStarvationGrace int    `yaml:"staffing_starvation_grace"` // consecutive ops-monitor digests before staffing-starvation backstop escalates (fail-loud)
	LivenessPingBody        string `yaml:"liveness_ping_body"`        // message body for the liveness-ping schedule job (defaults to "watch-liveness-ping", NOT fail-loud)
	VerifyServicesBody      string `yaml:"verify_services_body"`      // message body for the verify-services-up schedule job (defaults to "watch-verify-services", NOT fail-loud)
}

// WatchConfig holds the watch-level routing configuration read from the
// watch: block in .harmonik/config.yaml. Both target fields default to "captain"
// (NOT fail-loud — §7 exception, WE7 load-bearing: preserves existing routing
// when the watch: block is absent). WE9 behavioral keys are fail-loud when absent.
// WE6 schedule interval keys are fail-loud when absent.
//
// Bead refs: hk-we7-sender-redirect-clhh8, hk-we9-watch-spof-4dmac, hk-we6-watch-scheduled-send-6onfu.
type WatchConfig struct {
	// StatusTarget is the comms --to target for crew status feeds.
	// Empty = not configured → callers resolve to "captain".
	StatusTarget string
	// OpsmonitorTarget is the comms --to target for ops-monitor watch-class signals.
	// Empty = not configured → callers resolve to "captain".
	OpsmonitorTarget string
	// AbsentThreshSec is seconds watch may be absent from comms-who before watch-down fires.
	// Zero = not configured; fail-loud via checkMissingWatchValues when watch is deployed.
	AbsentThreshSec int
	// StallTicks is consecutive ops-monitor ticks the watch cursor may be frozen (with pending
	// events) before watch-stalled fires. Zero = not configured; fail-loud when watch is deployed.
	StallTicks int
	// LivenessInterval is the Go duration string (e.g. "1h") for the watch<->captain
	// mutual-liveness ping schedule. Empty = not configured; fail-loud when watch is deployed (WE6).
	LivenessInterval string
	// DigestInterval is the Go duration string (e.g. "1h") for the verify-services-up schedule.
	// Empty = not configured; fail-loud when watch is deployed (WE6).
	DigestInterval string
	// StaffingStarvationGrace is how many consecutive ops-monitor digests a "ready lane + free
	// slot" condition may persist with NO captain staffing action before the watch escalates the
	// staffing-starvation backstop. Zero = not configured; fail-loud when watch is deployed.
	StaffingStarvationGrace int
	// LivenessPingBody is the comms-send message body for the liveness-ping schedule job.
	// Empty = not configured → callers resolve to "watch-liveness-ping" (NOT fail-loud).
	LivenessPingBody string
	// VerifyServicesBody is the comms-send message body for the verify-services-up schedule job.
	// Empty = not configured → callers resolve to "watch-verify-services" (NOT fail-loud).
	VerifyServicesBody string
}

type rawStallSentinelEscalation struct {
	Tier1Crew     string `yaml:"tier1_crew"`
	Tier2Captain  string `yaml:"tier2_captain"`
	Tier3Operator string `yaml:"tier3_operator"`
}

type rawStallSentinelDetection struct {
	RunSilenceStall     string `yaml:"run_silence_stall"`
	ReviewFinalizeStall string `yaml:"review_finalize_stall"`
	RunMaxAge           string `yaml:"run_max_age"`
	LaneNoprogressStall string `yaml:"lane_noprogress_stall"`
}

type rawStallSentinelConfig struct {
	Escalation rawStallSentinelEscalation `yaml:"escalation"`
	Detection  rawStallSentinelDetection  `yaml:"detection"`
}

// StallSentinelConfig holds the stall_sentinel: config block decoded from
// .harmonik/config.yaml. Zero-value durations mean "not configured";
// ResolveStallSentinelConfig in cmd/harmonik is the fail-loud gate that
// refuses startup when a required value is absent.
//
// Escalation tiers (X/Y/Z from the DESIGN.md brief):
//   - Tier1Crew     (X) → escalate to the crew after this stall age
//   - Tier2Captain  (Y) → escalate to the captain after this stall age
//   - Tier3Operator (Z) → escalate to the operator mailbox after this stall age
//
// Detection thresholds:
//   - RunSilenceStall     → Layer A heartbeat-gap trigger
//   - ReviewFinalizeStall → Layer A review-stall trigger
//   - RunMaxAge           → Layer A run-age backstop
//   - LaneNoprogressStall → Layer B no-forward-progress trigger
//
// Bead ref: hk-hm09z.
type StallSentinelConfig struct {
	// Tier1Crew is the stall age after which Tier 1 (crew) escalation fires.
	// Zero = not configured (fail-loud at sentinel startup).
	Tier1Crew time.Duration
	// Tier2Captain is the stall age after which Tier 2 (captain) escalation fires.
	// Zero = not configured.
	Tier2Captain time.Duration
	// Tier3Operator is the stall age after which Tier 3 (operator mailbox) fires.
	// Zero = not configured.
	Tier3Operator time.Duration
	// RunSilenceStall is the max silence (no agent_heartbeat/agent_message) before
	// a Layer A heartbeat-gap stall fires. Zero = not configured.
	RunSilenceStall time.Duration
	// ReviewFinalizeStall is the max time after reviewer_verdict before a Layer A
	// review-finalize stall fires. Zero = not configured.
	ReviewFinalizeStall time.Duration
	// RunMaxAge is the backstop max run age before a Layer A run-age stall fires.
	// Zero = not configured.
	RunMaxAge time.Duration
	// LaneNoprogressStall is the max time a lane may have an expectation of
	// progress with zero forward-progress events before Layer B fires.
	// Zero = not configured.
	LaneNoprogressStall time.Duration
}

type rawProjectConfig struct {
	SchemaVersion int                       `yaml:"schema_version"`
	Agents        map[string]rawAgentConfig `yaml:"agents"`
	Daemon        rawDaemonConfig           `yaml:"daemon"`     // hk-rcp7: PL-004b daemon: block
	Keeper        rawKeeperConfig           `yaml:"keeper"`     // hk-lhu2: keeper config block
	Watchdog      rawWatchdogConfig         `yaml:"watchdog"`   // hk-sbitr: ctx-watchdog schedule gate
	Watch         rawWatchConfig            `yaml:"watch"`      // hk-we7: watch routing targets
	Opsmonitor    rawOpsmonitorConfig       `yaml:"opsmonitor"` // hk-bi4bg: ops-monitor schedule overrides
	Supervise     rawSuperviseConfig        `yaml:"supervise"`
	Harnesses     rawHarnessesConfig        `yaml:"harnesses"`      // hk-v7q5u: per-harness config (PI-050)
	Sandbox       rawSandboxConfig          `yaml:"sandbox"`        // hk-6596l: sandbox backend config
	StallSentinel rawStallSentinelConfig    `yaml:"stall_sentinel"` // hk-hm09z: stall-sentinel detection thresholds
	Crews         map[string]rawCrewConfig  `yaml:"crews"`          // hk-l63b9: per-crew-name config (harness selection)

	// Subsystems is the subsystems: block — configuration-driven partitioning.
	// See subsystems.go. Absent = every subsystem enabled. Held as yaml.Node so
	// parseSubsystemsBlock can reject unknown keys inside an entry.
	Subsystems map[string]yaml.Node `yaml:"subsystems"`
}

type rawCrewConfig struct {
	// Harness is the per-crew default harness selection (e.g. "codex"). The
	// third-highest tier of the crew-scoped harness resolver — overridden by
	// --harness and by the mission's harness: front-matter field.
	Harness string `yaml:"harness"`
}

// CrewConfig holds the resolved per-crew-name config read from config.yaml's
// crews: block (hk-l63b9).
type CrewConfig struct {
	// Harness is the per-crew default harness selection. Empty = not
	// configured; the crew-scoped harness resolver falls through to the
	// compiled default ("claude").
	Harness string
}

type rawAgentConfig struct {
	Model  string `yaml:"model"`
	Effort string `yaml:"effort"`
}

type agentConfigEntry struct {
	model  string
	effort string
}

// ProjectConfig is the decoded and cached representation of .harmonik/config.yaml.
// It is the zero value when the file is absent. Use LookupAgent to query per-type
// values, Daemon for daemon operational settings, and Keeper for keeper settings.
type ProjectConfig struct {
	// entries maps core.AgentType to the configured (model, effort) pair.
	// Only known-at-parse-time entries are stored; unknown keys are discarded.
	entries map[core.AgentType]agentConfigEntry

	// Daemon holds the daemon-level operational config read from the daemon: block.
	// Zero value when the block is absent.
	//
	// Spec ref: specs/process-lifecycle.md §4.1 PL-004b.
	// Bead ref: hk-rcp7.
	Daemon DaemonConfig

	// Keeper holds the keeper-level config read from the keeper: block.
	// Zero value when the block is absent.
	//
	// Bead ref: hk-lhu2.
	Keeper KeeperConfig

	// Watchdog holds the ctx-watchdog schedule gate read from the watchdog: block.
	// When the block is absent, Watchdog.Enabled defaults to true.
	//
	// Bead ref: hk-sbitr.
	Watchdog WatchdogConfig

	// Supervise holds supervisor/flywheel and daemon-watchdog timings read from
	// the supervise: block. Zero values mean "not configured"; callers keep the
	// compiled defaults.
	Supervise SuperviseConfig

	// Watch holds the watch-level routing config read from the watch: block.
	// When the block is absent, both target fields are empty strings (callers
	// default to "captain"). Bead ref: hk-we7-sender-redirect-clhh8.
	Watch WatchConfig

	// Opsmonitor holds the ops-monitor schedule overrides read from the
	// opsmonitor: block. Zero value when the block is absent; callers apply
	// compiled defaults ("5m", "scripts/ops-monitor-check.sh"). Bead ref: hk-bi4bg.
	Opsmonitor OpsmonitorConfig

	// Harnesses holds the per-harness config read from the harnesses: block.
	// Zero value when the block is absent. Bead ref: hk-v7q5u (PI-050).
	Harnesses HarnessesConfig

	// Sandbox holds the sandbox: config block read from the sandbox: block.
	// Zero value (Backend=="") when the block is absent. Bead ref: hk-6596l.
	Sandbox SandboxConfig

	// StallSentinel holds the stall_sentinel: config block (bead hk-hm09z).
	// Zero value (all durations 0) when the block is absent. All required
	// values are enforced at sentinel startup via ResolveStallSentinelConfig.
	StallSentinel StallSentinelConfig

	// Crews holds the per-crew-name config read from the crews: block, keyed by
	// crew name. Nil/absent = no per-crew config for any crew. Bead: hk-l63b9.
	Crews map[string]CrewConfig

	// Subsystems holds the resolved subsystems: block — configuration-driven
	// partitioning of the composition root. The zero value enables every
	// subsystem, so an absent block leaves behaviour unchanged. See
	// subsystems.go.
	Subsystems SubsystemsConfig
}

// LookupAgent returns the (model, effort) pair configured for agentType, or
// ("", "") when the type is absent from the config or the file was absent.
//
// Callers MUST treat an empty returned value as "not configured" and continue
// the resolution walk to tier 3 (compiled defaults).
func (c ProjectConfig) LookupAgent(agentType core.AgentType) (model, effort string) {
	if c.entries == nil {
		return "", ""
	}
	e, ok := c.entries[agentType]
	if !ok {
		return "", ""
	}
	return e.model, e.effort
}

// LoadProjectConfig reads .harmonik/config.yaml under repoRoot and returns the
// decoded ProjectConfig.
//
// Behaviour:
//   - File absent → zero-value ProjectConfig, nil error.
//   - File present, malformed YAML → *ErrMalformedConfigYAML (daemon MUST refuse to start).
//   - schema_version != 1 → *ErrUnsupportedConfigVersion (daemon MUST refuse to start).
//   - Unknown agent keys → silently ignored (forward-compat).
//   - Unknown schema_version for a zero-value file (empty YAML) → zero-value, nil error.
func LoadProjectConfig(repoRoot string) (ProjectConfig, error) {
	path := filepath.Join(repoRoot, projectConfigRelPath)

	//nolint:gosec // G304: path is constructed from operator-supplied ProjectDir, not user input
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ProjectConfig{}, nil
		}
		return ProjectConfig{}, fmt.Errorf("daemon: project config: reading %s: %w", path, err)
	}

	return parseProjectConfig(path, data)
}

func parseProjectConfig(path string, data []byte) (ProjectConfig, error) {
	var raw rawProjectConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return ProjectConfig{}, &ErrMalformedConfigYAML{Path: path, Cause: err}
	}

	sentinel := raw
	if len(sentinel.Agents) == 0 {
		sentinel.Agents = nil
	}
	if len(sentinel.Crews) == 0 {
		sentinel.Crews = nil
	}
	if len(sentinel.Subsystems) == 0 {
		sentinel.Subsystems = nil
	}
	if reflect.DeepEqual(sentinel, rawProjectConfig{}) {
		return ProjectConfig{}, nil
	}

	if raw.SchemaVersion != projectConfigCurrentVersion {
		return ProjectConfig{}, &ErrUnsupportedConfigVersion{
			Path:    path,
			Version: raw.SchemaVersion,
		}
	}

	daemonCfg, err := parseDaemonBlock(path, raw.Daemon)
	if err != nil {
		return ProjectConfig{}, err
	}

	if err := strictDecodeKeeperBlock(path, data); err != nil {
		return ProjectConfig{}, err
	}

	keeperCfg, err := parseKeeperBlock(path, raw.Keeper)
	if err != nil {
		return ProjectConfig{}, err
	}

	watchdogCfg := parseWatchdogBlock(raw.Watchdog)

	watchCfg := parseWatchBlock(raw.Watch)

	opsmonitorCfg := parseOpsmonitorBlock(raw.Opsmonitor)

	superviseCfg, err := parseSuperviseBlock(path, raw.Supervise)
	if err != nil {
		return ProjectConfig{}, err
	}

	harnessesCfg := parseHarnessesBlock(raw.Harnesses)

	var sandboxCfg SandboxConfig
	if !sandboxBlockAbsent(raw.Sandbox) {
		var sErr error
		sandboxCfg, sErr = parseSandboxBlock(path, raw.Sandbox)
		if sErr != nil {
			return ProjectConfig{}, sErr
		}
	}

	stallSentinelCfg, err := parseStallSentinelBlock(path, raw.StallSentinel)
	if err != nil {
		return ProjectConfig{}, err
	}

	subsystemsCfg, err := parseSubsystemsBlock(path, raw.Subsystems)
	if err != nil {
		return ProjectConfig{}, err
	}

	cfg := ProjectConfig{
		entries:       make(map[core.AgentType]agentConfigEntry, len(raw.Agents)),
		Daemon:        daemonCfg,
		Keeper:        keeperCfg,
		Watchdog:      watchdogCfg,
		Supervise:     superviseCfg,
		Watch:         watchCfg,
		Opsmonitor:    opsmonitorCfg,
		Harnesses:     harnessesCfg,
		Sandbox:       sandboxCfg,
		StallSentinel: stallSentinelCfg,
		Subsystems:    subsystemsCfg,
	}
	for key, agentRaw := range raw.Agents {
		at := core.AgentType(key)
		cfg.entries[at] = agentConfigEntry{
			model:  agentRaw.Model,
			effort: agentRaw.Effort,
		}
	}
	if len(raw.Crews) > 0 {
		cfg.Crews = make(map[string]CrewConfig, len(raw.Crews))
		for name, crewRaw := range raw.Crews {
			cfg.Crews[name] = CrewConfig(crewRaw)
		}
	}

	return cfg, nil
}

func parseDaemonBlock(path string, raw rawDaemonConfig) (DaemonConfig, error) {
	cfg := DaemonConfig{
		TargetBranch: raw.TargetBranch, // observability/symmetry only per PL-004b
	}

	if raw.WorkflowMode != "" {
		wm := core.WorkflowMode(raw.WorkflowMode)
		if raw.WorkflowMode == core.WorkflowModeRetiredReviewLoop {
			return DaemonConfig{}, &ErrMalformedConfigYAML{
				Path: path,
				Cause: fmt.Errorf(
					"daemon.workflow_mode %q: RETIRED (execution-model.md §4.3.EM-015d); use \"dot\", "+
						"the general workflow-graph walker review-loop was a hand-written special case of",
					raw.WorkflowMode),
			}
		}
		if !wm.Valid() {
			return DaemonConfig{}, &ErrMalformedConfigYAML{
				Path:  path,
				Cause: fmt.Errorf("daemon.workflow_mode %q: unknown value; must be dot (single is forbidden at daemon level)", raw.WorkflowMode),
			}
		}
		if wm == core.WorkflowModeSingle {
			return DaemonConfig{}, &ErrWorkflowModeFloorViolation{Path: path, Value: raw.WorkflowMode}
		}
		cfg.WorkflowMode = wm
	}

	if raw.MaxConcurrent > 0 {
		cfg.MaxConcurrent = raw.MaxConcurrent
	}

	cfg.AllowedRepos = raw.AllowedRepos

	cfg.RemoteControlPrefix = raw.RemoteControlPrefix

	for _, f := range []struct {
		key string
		val string
		dst *time.Duration
	}{
		{"daemon.restart_backoff.base", raw.RestartBackoff.Base, &cfg.RestartBackoff.Base},
		{"daemon.restart_backoff.cap", raw.RestartBackoff.Cap, &cfg.RestartBackoff.Cap},
		{"daemon.restart_backoff.window", raw.RestartBackoff.Window, &cfg.RestartBackoff.Window},
	} {
		dv, err := parseDurationField(path, f.key, f.val)
		if err != nil {
			return DaemonConfig{}, err
		}
		*f.dst = dv
	}

	return cfg, nil
}

type keeperNodeEnvelope struct {
	Keeper yaml.Node `yaml:"keeper"`
}

func strictDecodeKeeperBlock(path string, data []byte) error {
	var env keeperNodeEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		return &ErrMalformedConfigYAML{Path: path, Cause: err}
	}
	if env.Keeper.Kind == 0 {
		return nil
	}

	if keyPath, ok := unknownYAMLKey(&env.Keeper, reflect.TypeOf(rawKeeperConfig{}), "keeper"); !ok {
		return &ErrUnknownConfigKey{
			Path:    path,
			KeyPath: keyPath,
			Cause:   fmt.Errorf("unknown config key %q", keyPath),
		}
	}
	return nil
}

// unknownYAMLKey structurally validates a YAML mapping node against a Go struct
// type: every mapping key must correspond to a struct field's yaml tag. It
// recurses into sub-block mappings (struct-typed fields). On the first offending
// key it returns (dotted-key-path, false); when every key is known it returns
// ("", true).
//
// It underpins the structural unknown-key rejection for the keeper: block
// (RU-06) so detection does not depend on yaml.v3's internal error text.
//
//nolint:gocognit,cyclop // unknownYAMLKey is at/over the threshold after branch edits; splitting mid-release is riskier than the marginal complexity
func unknownYAMLKey(node *yaml.Node, typ reflect.Type, prefix string) (string, bool) {
	if node.Kind == yaml.DocumentNode && len(node.Content) == 1 {
		node = node.Content[0]
	}
	if node.Kind != yaml.MappingNode {
		return "", true
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return "", true
	}

	known := make(map[string]reflect.Type, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := f.Tag.Get("yaml")
		if idx := strings.IndexByte(tag, ','); idx >= 0 {
			tag = tag[:idx]
		}
		if tag == "" || tag == "-" {
			continue
		}
		known[tag] = f.Type
	}

	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode, valNode := node.Content[i], node.Content[i+1]
		key := keyNode.Value
		fieldType, ok := known[key]
		if !ok {
			return prefix + "." + key, false
		}
		ft := fieldType
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct && valNode.Kind == yaml.MappingNode {
			if kp, ok := unknownYAMLKey(valNode, ft, prefix+"."+key); !ok {
				return kp, false
			}
		}
	}
	return "", true
}

func parseDurationField(path, key, value string) (time.Duration, error) {
	if value == "" {
		return 0, nil // not configured — defer to default
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		fullKey := "keeper." + key
		if strings.HasPrefix(key, "supervise.") || strings.HasPrefix(key, "daemon.") ||
			strings.HasPrefix(key, "stall_sentinel.") {
			fullKey = key
		}
		return 0, &ErrMalformedConfigYAML{
			Path:  path,
			Cause: fmt.Errorf("%s %q: not a valid Go duration string (e.g. %q); a bare number is rejected — never silently coerced", fullKey, value, "5m"),
		}
	}
	return d, nil
}

func parseKeeperBlock(path string, raw rawKeeperConfig) (KeeperConfig, error) {
	cfg := KeeperConfig{}

	t := raw.ContextThresholds
	if t.WarnAbsTokens > 0 {
		cfg.WarnAbsTokens = t.WarnAbsTokens
		cfg.Present.WarnAbsTokens = true
	}
	if t.ActAbsTokens > 0 {
		cfg.ActAbsTokens = t.ActAbsTokens
		cfg.Present.ActAbsTokens = true
	}
	if t.ForceActAbsTokens > 0 {
		cfg.ForceActAbsTokens = t.ForceActAbsTokens
		cfg.Present.ForceActAbsTokens = true
	}
	if t.ForceActAbsOffset > 0 {
		cfg.ForceActAbsOffset = t.ForceActAbsOffset
		cfg.Present.ForceActAbsOffset = true
	}
	if t.IdleFloorAbsTokens > 0 {
		cfg.IdleFloorAbsTokens = t.IdleFloorAbsTokens
		cfg.Present.IdleFloorAbsTokens = true
	}
	if t.ActPctCeil > 0 {
		if t.ActPctCeil > 1 {
			return KeeperConfig{}, &ErrMalformedConfigYAML{
				Path:  path,
				Cause: fmt.Errorf("keeper.context_thresholds.act_pct_ceil %v: must be a fraction in (0, 1]", t.ActPctCeil),
			}
		}
		cfg.ActPctCeil = t.ActPctCeil
		cfg.Present.ActPctCeil = true
	}
	if t.WarnPctCeil > 0 {
		if t.WarnPctCeil > 1 {
			return KeeperConfig{}, &ErrMalformedConfigYAML{
				Path:  path,
				Cause: fmt.Errorf("keeper.context_thresholds.warn_pct_ceil %v: must be a fraction in (0, 1]", t.WarnPctCeil),
			}
		}
		cfg.WarnPctCeil = t.WarnPctCeil
		cfg.Present.WarnPctCeil = true
	}

	hc := raw.HardCeiling
	if hc.Mode != "" {
		switch hc.Mode {
		case "off", "alarm", "restart":
			cfg.HardCeilingMode = hc.Mode
			cfg.Present.HardCeilingMode = true
		default:
			return KeeperConfig{}, &ErrMalformedConfigYAML{
				Path:  path,
				Cause: fmt.Errorf("keeper.hard_ceiling.mode %q: unknown value; must be one of off, alarm, restart", hc.Mode),
			}
		}
	}
	if hc.AbsTokens > 0 {
		cfg.HardCeilingAbsTokens = hc.AbsTokens
		cfg.Present.HardCeilingAbsTokens = true
	}
	d, err := parseDurationField(path, "hard_ceiling.cooldown", hc.Cooldown)
	if err != nil {
		return KeeperConfig{}, err
	}
	cfg.HardCeilingCooldownDur = d

	tm := raw.Timings
	for _, f := range []struct {
		key     string
		val     string
		dst     *time.Duration
		present *bool
	}{
		{"timings.poll_interval", tm.PollInterval, &cfg.PollInterval, &cfg.Present.PollInterval},
		{"timings.cycler_poll_interval", tm.CyclerPollInterval, &cfg.CyclerPollInterval, &cfg.Present.CyclerPollInterval},
		{"timings.idle_quiesce", tm.IdleQuiesce, &cfg.IdleQuiesce, &cfg.Present.IdleQuiesce},
		{"timings.staleness", tm.Staleness, &cfg.Staleness, &cfg.Present.Staleness},
		{"timings.handoff_timeout", tm.HandoffTimeout, &cfg.HandoffTimeout, &cfg.Present.HandoffTimeout},
		{"timings.clear_settle", tm.ClearSettle, &cfg.ClearSettle, &cfg.Present.ClearSettle},
		{"timings.boot_grace", tm.BootGrace, &cfg.BootGrace, &cfg.Present.BootGrace},
		{"timings.max_boot_grace_total", tm.MaxBootGraceTotal, &cfg.MaxBootGraceTotal, nil},
		{"timings.flock_acquire_grace", tm.FlockAcquireGrace, &cfg.FlockAcquireGrace, &cfg.Present.FlockAcquireGrace},
	} {
		dv, derr := parseDurationField(path, f.key, f.val)
		if derr != nil {
			return KeeperConfig{}, derr
		}
		*f.dst = dv
		if f.present != nil {
			*f.present = f.val != ""
		}
	}

	c := raw.Cadence
	for _, f := range []struct {
		key     string
		val     string
		dst     *time.Duration
		present *bool
	}{
		{"cadence.warn_cooldown", c.WarnCooldown, &cfg.WarnCooldown, &cfg.Present.WarnCooldown},
		{"cadence.no_gauge_backoff", c.NoGaugeBackoff, &cfg.NoGaugeBackoff, &cfg.Present.NoGaugeBackoff},
		{"cadence.respawn_grace", c.RespawnGrace, &cfg.RespawnGrace, &cfg.Present.RespawnGrace},
		{"cadence.respawn_cooldown", c.RespawnCooldown, &cfg.RespawnCooldown, &cfg.Present.RespawnCooldown},
		{"cadence.live_recover_grace", c.LiveRecoverGrace, &cfg.LiveRecoverGrace, &cfg.Present.LiveRecoverGrace},
		{"cadence.live_recover_cooldown", c.LiveRecoverCooldown, &cfg.LiveRecoverCooldown, &cfg.Present.LiveRecoverCooldown},
		{"cadence.force_retry_interval", c.ForceRetryInterval, &cfg.ForceRetryInterval, &cfg.Present.ForceRetryInterval},
		{"cadence.idle_restart_cooldown", c.IdleRestartCooldown, &cfg.IdleRestartCooldown, &cfg.Present.IdleRestartCooldown},
		{"cadence.hard_ceiling_cooldown", c.HardCeilingCooldown, &cfg.CadenceHardCeilingCooldown, &cfg.Present.HardCeilingCooldown},
		{"cadence.blind_keeper_threshold", c.BlindKeeperThreshold, &cfg.BlindKeeperThreshold, &cfg.Present.BlindKeeperThreshold},
		{"cadence.hold_ttl", c.HoldTTL, &cfg.HoldTTL, &cfg.Present.HoldTTL},
		{"cadence.reap_decisions_cadence", c.ReapDecisionsCadence, &cfg.ReapDecisionsCadence, &cfg.Present.ReapDecisionsCadence},
		{"cadence.operator_turn_lookback", c.OperatorTurnLookback, &cfg.OperatorTurnLookback, &cfg.Present.OperatorTurnLookback},
		{"cadence.post_answer_grace", c.PostAnswerGrace, &cfg.PostAnswerGrace, &cfg.Present.PostAnswerGrace},
	} {
		dv, derr := parseDurationField(path, f.key, f.val)
		if derr != nil {
			return KeeperConfig{}, derr
		}
		*f.dst = dv
		if f.present != nil {
			*f.present = f.val != ""
		}
	}

	b := raw.Budgets
	if b.HeartbeatMaxMisses > 0 {
		cfg.HeartbeatMaxMisses = b.HeartbeatMaxMisses
		cfg.Present.HeartbeatMaxMisses = true
	}
	if b.MaxHandoffTimeouts > 0 {
		cfg.MaxHandoffTimeouts = b.MaxHandoffTimeouts
		cfg.Present.MaxHandoffTimeouts = true
	}

	s := raw.SelfService
	cfg.SelfServiceEnabled = s.Enabled
	if s.GraceSeconds > 0 {
		cfg.SelfServiceGraceSeconds = s.GraceSeconds
	}
	cfg.SelfServiceInstructOnlyWhenIdle = s.InstructOnlyWhenIdle
	cfg.SelfServiceCrewsEnabled = s.CrewsEnabled

	cfg.DefaultWarnText = raw.WarnMessages.DefaultWarnText
	cfg.ActionableWarnText = raw.WarnMessages.ActionableWarnText
	cfg.SettleWarnText = raw.WarnMessages.SettleWarnText
	cfg.LeaderDeferText = raw.WarnMessages.LeaderDeferText
	cfg.CrewDeferText = raw.WarnMessages.CrewDeferText
	if raw.WarnMessages.OnDemandWarnText != "" {
		if cfg.ActionableWarnText == "" {
			cfg.ActionableWarnText = raw.WarnMessages.OnDemandWarnText
			slog.Warn("keeper config: keeper.warn_messages.on_demand_warn_text is DEPRECATED; mapping it onto actionable_warn_text. Rename the key.")
		} else {
			slog.Warn("keeper config: keeper.warn_messages.on_demand_warn_text is DEPRECATED and IGNORED because actionable_warn_text is also set. Remove on_demand_warn_text.")
		}
	}

	return cfg, nil
}

func parseWatchdogBlock(raw rawWatchdogConfig) WatchdogConfig {
	if raw.Enabled == nil {
		return WatchdogConfig{Enabled: true}
	}
	return WatchdogConfig{Enabled: *raw.Enabled}
}

func parseSuperviseBlock(path string, raw rawSuperviseConfig) (SuperviseConfig, error) {
	cfg := SuperviseConfig{}
	for _, f := range []struct {
		key string
		val string
		dst *time.Duration
	}{
		{"heartbeat_ttl", raw.HeartbeatTTL, &cfg.HeartbeatTTL},
		{"start_timeout", raw.StartTimeout, &cfg.StartTimeout},
		{"crash_loop_window", raw.CrashLoopWindow, &cfg.CrashLoopWindow},
		{"health_probe_interval", raw.HealthProbeInterval, &cfg.HealthProbeInterval},
		{"stop_timeout", raw.StopTimeout, &cfg.StopTimeout},
		{"restart_backoff.base", raw.RestartBackoff.Base, &cfg.RestartBackoffBase},
		{"restart_backoff.cap", raw.RestartBackoff.Cap, &cfg.RestartBackoffCap},
		{"daemon_watchdog.check_interval", raw.DaemonWatchdog.CheckInterval, &cfg.DaemonWatchdog.CheckInterval},
		{"daemon_watchdog.dial_timeout", raw.DaemonWatchdog.DialTimeout, &cfg.DaemonWatchdog.DialTimeout},
		{"daemon_watchdog.revive_backoff", raw.DaemonWatchdog.ReviveBackoff, &cfg.DaemonWatchdog.ReviveBackoff},
		{"daemon_watchdog.revive_window", raw.DaemonWatchdog.ReviveWindow, &cfg.DaemonWatchdog.ReviveWindow},
	} {
		dv, err := parseDurationField(path, "supervise."+f.key, f.val)
		if err != nil {
			return SuperviseConfig{}, err
		}
		*f.dst = dv
	}
	return cfg, nil
}

func parseWatchBlock(raw rawWatchConfig) WatchConfig {
	return WatchConfig(raw)
}

func parseOpsmonitorBlock(raw rawOpsmonitorConfig) OpsmonitorConfig {
	return OpsmonitorConfig(raw)
}

func parseStallSentinelBlock(path string, raw rawStallSentinelConfig) (StallSentinelConfig, error) {
	e := raw.Escalation
	d := raw.Detection
	type pair struct {
		key string
		val string
		dst *time.Duration
	}
	fields := []pair{
		{"stall_sentinel.escalation.tier1_crew", e.Tier1Crew, new(time.Duration)},
		{"stall_sentinel.escalation.tier2_captain", e.Tier2Captain, new(time.Duration)},
		{"stall_sentinel.escalation.tier3_operator", e.Tier3Operator, new(time.Duration)},
		{"stall_sentinel.detection.run_silence_stall", d.RunSilenceStall, new(time.Duration)},
		{"stall_sentinel.detection.review_finalize_stall", d.ReviewFinalizeStall, new(time.Duration)},
		{"stall_sentinel.detection.run_max_age", d.RunMaxAge, new(time.Duration)},
		{"stall_sentinel.detection.lane_noprogress_stall", d.LaneNoprogressStall, new(time.Duration)},
	}
	for _, f := range fields {
		dv, err := parseDurationField(path, f.key, f.val)
		if err != nil {
			return StallSentinelConfig{}, err
		}
		*f.dst = dv
	}
	return StallSentinelConfig{
		Tier1Crew:           *fields[0].dst,
		Tier2Captain:        *fields[1].dst,
		Tier3Operator:       *fields[2].dst,
		RunSilenceStall:     *fields[3].dst,
		ReviewFinalizeStall: *fields[4].dst,
		RunMaxAge:           *fields[5].dst,
		LaneNoprogressStall: *fields[6].dst,
	}, nil
}

func parseHarnessesBlock(raw rawHarnessesConfig) HarnessesConfig {
	pi := raw.Pi
	hasFallback := pi.Fallback.Provider != "" || pi.Fallback.Model != "" || pi.Fallback.APIKeyEnv != ""
	apiKeyFile, expandErr := daemonExpandHomePath(pi.APIKeyFile)
	if expandErr != nil {
		fmt.Fprintf(os.Stderr, "daemon: parseHarnessesBlock: expand harnesses.pi.api_key_file %q: %v\n", pi.APIKeyFile, expandErr)
	}
	var profiles map[string]PiProfileConfig
	if len(pi.Profiles) > 0 {
		profiles = make(map[string]PiProfileConfig, len(pi.Profiles))
		for name, rp := range pi.Profiles {
			profiles[name] = PiProfileConfig(rp)
		}
	}
	var providerSlots map[string]int
	if len(pi.ProviderSlots) > 0 {
		providerSlots = make(map[string]int, len(pi.ProviderSlots))
		for name, n := range pi.ProviderSlots {
			providerSlots[name] = n
		}
	}
	return HarnessesConfig{
		Pi: PiHarnessConfig{
			Provider:    pi.Provider,
			Model:       pi.Model,
			APIKeyEnv:   pi.APIKeyEnv,
			APIKeyFile:  apiKeyFile,
			BaseURL:     pi.BaseURL,
			API:         pi.API,
			HasFallback: hasFallback,
			Fallback: PiFallbackConfig{
				Provider:  pi.Fallback.Provider,
				Model:     pi.Fallback.Model,
				APIKeyEnv: pi.Fallback.APIKeyEnv,
			},
			Profiles:      profiles,
			ProviderSlots: providerSlots,
		},
	}
}

func parseSandboxBlock(path string, raw rawSandboxConfig) (SandboxConfig, error) {
	if raw.Backend == "" {
		return SandboxConfig{}, &ErrMalformedConfigYAML{
			Path:  path,
			Cause: fmt.Errorf("sandbox.backend is required when the sandbox: block is present; set to \"srt\" or \"none\""),
		}
	}
	switch raw.Backend {
	case "srt", "none":
	default:
		return SandboxConfig{}, &ErrMalformedConfigYAML{
			Path:  path,
			Cause: fmt.Errorf("sandbox.backend %q: unknown value; must be one of srt, none", raw.Backend),
		}
	}
	for _, h := range raw.Harnesses {
		if core.AgentType(h).Reserved() {
			continue
		}
		cause := fmt.Errorf("sandbox.harnesses[%q]: unknown agent type; must be one of %s",
			h, agentTypeNamesForError())
		if suggestion, ok := nearestAgentType(h); ok {
			cause = fmt.Errorf("sandbox.harnesses[%q]: unknown agent type; did you mean %q? must be one of %s",
				h, suggestion, agentTypeNamesForError())
		}
		return SandboxConfig{}, &ErrMalformedConfigYAML{Path: path, Cause: cause}
	}
	return SandboxConfig{
		Backend:   raw.Backend,
		Harnesses: raw.Harnesses,
		Network: SandboxNetworkConfig{
			Mode:                   raw.Network.Mode,
			AllowedDomains:         raw.Network.AllowedDomains,
			WeakerNetworkIsolation: raw.Network.WeakerNetworkIsolation,
			AllowLocalBinding:      raw.Network.AllowLocalBinding,
		},
		Cache: SandboxCacheConfig{
			WarmRead:     raw.Cache.WarmRead,
			PrivateWrite: raw.Cache.PrivateWrite,
		},
	}, nil
}

func daemonExpandHomePath(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p, fmt.Errorf("cannot determine home directory: %w", err)
	}
	if p == "~" {
		return home, nil
	}
	return filepath.Join(home, p[2:]), nil
}
