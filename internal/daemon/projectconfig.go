package daemon

// projectconfig.go — per-project model/effort config loader for
// .harmonik/config.yaml (hk-bfvk7), extended with the daemon operational
// config block per PL-004b (hk-rcp7) and the keeper config block (hk-lhu2).
//
// Implements tier-2 of the EM-012b model/effort resolution chain:
// per-project .harmonik/config.yaml supplies per-agent-type defaults that take
// precedence over compiled-in tier-3 defaults but are overridden by per-bead
// labels (tier-1).
//
// Also implements the PL-004b daemon: block reader: LoadProjectConfig now parses
// the optional daemon: mapping under schema_version: 1, extracting workflow_mode,
// max_concurrent, and target_branch. Callers read these via ProjectConfig.Daemon
// to apply the flag > config > default precedence chain at startup.
//
// Also implements the hk-lhu2 keeper: block reader: LoadProjectConfig parses the
// optional keeper: mapping under schema_version: 1, extracting context thresholds
// and warn message overrides. Callers read these via ProjectConfig.Keeper and
// apply the CLI flag > config > default precedence chain at keeper startup.
//
// # File location
//
// .harmonik/config.yaml at the project root. Loaded ONCE at daemon startup and
// cached on the daemon Config struct. No mtime-invalidation: operators restart
// the daemon to reload (matches the pattern for WorkflowModeDefault and other
// startup-time-resolved fields). This is documented here so the decision is
// explicit.
//
// # Schema (v1)
//
//	schema_version: 1
//	agents:
//	  claude-code:
//	    model: sonnet      # optional alias; omitted = defer to tier 3
//	    effort: medium     # optional effort; omitted = defer to tier 3
//	  claude-twin:
//	    model: sonnet
//	    effort: medium
//	daemon:
//	  workflow_mode: dot       # review-loop or dot; single FORBIDDEN (PL-004a floor)
//	  max_concurrent: 4        # > 0 to override --max-concurrent default
//	  target_branch: main      # observability/symmetry only; authoritative source is branching.yaml
//	  remote_control_prefix: hk # cosmetic Claude RC session-label prefix (empty = bare name); hk-igpg
//	  restart_backoff:         # rapid daemon-boot delay; Go duration STRINGS; empty = compiled default
//	    base: 30s
//	    cap: 10m
//	    window: 1h
//	keeper:
//	  context_thresholds:
//	    warn_abs_tokens: 200000        # absolute warn gate (default 200000); ≤0 = not configured
//	    act_abs_tokens: 215000         # absolute act gate (default 215000); ≤0 = not configured
//	    force_act_abs_tokens: 240000   # hard ceiling, unconditional clear (default act+25000); ≤0 = not configured
//	    force_act_abs_offset: 25000    # offset over act when force_act_abs_tokens unset; ≤0 = not configured (hk-9kgf)
//	    idle_floor_abs_tokens: 200000  # floor below which idle crews are not idle-restarted; ≤0 = not configured (hk-9kgf)
//	    act_pct_ceil: 0.85             # pct-of-window cap for act gate (default 0.85); ≤0 = not configured; >1 = error
//	    warn_pct_ceil: 0.70            # pct-of-window cap for warn gate (default 0.70); ≤0 = not configured; >1 = error
//	  hard_ceiling:                  # hk-9kgf
//	    mode: restart                  # off|alarm|restart; other = error; empty = not configured
//	    abs_tokens: 360000             # ≤0 = not configured
//	    cooldown: 30m                  # Go duration STRING; bare number = error; empty = not configured
//	  timings:                       # all Go duration STRINGS; bare number = error; empty = not configured (hk-9kgf)
//	    poll_interval: 60s
//	    idle_quiesce: 5m
//	    staleness: 10m
//	    handoff_timeout: 5m
//	    clear_settle: 30s
//	    boot_grace: 2m
//	    max_boot_grace_total: 10m
//	  cadence:                       # all Go duration STRINGS; bare number = error; empty = not configured (hk-9kgf)
//	    warn_cooldown: 15m
//	    no_gauge_backoff: 2m
//	    respawn_grace: 1m
//	    respawn_cooldown: 5m
//	    live_recover_grace: 1m
//	    live_recover_cooldown: 5m
//	    force_retry_interval: 2m
//	    idle_restart_cooldown: 10m
//	    hard_ceiling_cooldown: 30m
//	    blind_keeper_threshold: 20m
//	  budgets:                       # hk-9kgf; ≤0 = not configured
//	    heartbeat_max_misses: 3
//	    max_handoff_timeouts: 2
//	  self_service:                  # hk-9kgf
//	    enabled: true                  # bool; default false
//	    grace_seconds: 30              # ≤0 = not configured
//	    instruct_only_when_idle: true  # bool; default false
//	    crews_enabled: true            # *bool; ABSENT = TRUE (crews self-restart, hk-vs4u); explicit false = false
//	  warn_messages:
//	    default_warn_text: ""          # warn injection text for non-captain agents; empty = compiled default
//	    actionable_warn_text: ""       # actionable self-service restart-handshake advisory override; empty = compiled default (hk-9kgf, hk-vs4u)
//	    on_demand_warn_text: ""        # DEPRECATED alias of actionable_warn_text (kept RECOGNIZED so old strict configs don't hard-error); mapped with a log warning (hk-vs4u)
//	opsmonitor:                        # hk-bi4bg: ops-monitor schedule overrides; absent = compiled defaults
//	  interval: 5m                     # Go duration STRING; empty/absent = "5m"
//	  script_path: scripts/ops-monitor-check.sh  # path passed to bash; empty/absent = default
//	supervise:                         # all duration fields are Go duration STRINGS; empty = compiled default
//	  heartbeat_ttl: 90s
//	  start_timeout: 30s
//	  crash_loop_window: 60s
//	  health_probe_interval: 15s
//	  stop_timeout: 10s
//	  restart_backoff:
//	    base: 1s
//	    cap: 60s
//	  daemon_watchdog:
//	    check_interval: 30s
//	    dial_timeout: 3s
//	    revive_backoff: 10s
//	    revive_window: 15m
//
// Unknown agent keys are silently ignored (forward-compat).
// Unknown sibling keys under daemon: are silently ignored (forward-compat per PL-004b).
// Unknown keys under keeper: (and every keeper sub-block) are REJECTED with
//   *ErrUnknownConfigKey naming the offending key path (operator decision, hk-9f3f).
//   The previous "silently ignored (forward-compat per hk-lhu2)" behaviour is removed
//   because silent-ignore masks a typo'd / fat-fingered keeper key.
// Unknown schema_version → ErrUnsupportedConfigVersion.
// Parse error on a present file → ErrMalformedConfigYAML.
// daemon.workflow_mode: single → ErrWorkflowModeFloorViolation (PL-004a floor).
// Absent file → zero-value ProjectConfig, nil error.
//
// # Spec refs
//
// specs/execution-model.md §4.3 EM-012b — tier-2 slot.
// specs/handler-contract.md §4.10 HC-055a — ModelPreference invariants.
// specs/process-lifecycle.md §4.1 PL-004a — review floor (never single from config).
// specs/process-lifecycle.md §4.1 PL-004b — flag > config > default precedence chain.
//
// Beads: hk-bfvk7, hk-rcp7, hk-lhu2, hk-exg3, hk-9kgf, hk-bi4bg.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/gregberns/harmonik/internal/core"
)

// projectConfigRelPath is the path of the config file relative to the project root.
const projectConfigRelPath = ".harmonik/config.yaml"

// projectConfigCurrentVersion is the only schema_version this loader accepts.
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
// KeyPath names the offending key as a dotted path rooted at keeper
// (e.g. "keeper.context_thresholds.warn_abs_token").
//
// Scope: this strict-rejection applies ONLY to the keeper: block. The daemon:
// block remains tolerant of unknown sibling keys per the PL-004b spec
// requirement (specs/process-lifecycle.md §4.1).
//
// Bead ref: hk-9f3f.
type ErrUnknownConfigKey struct {
	// Path is the absolute path to the file.
	Path string
	// KeyPath is the dotted path to the offending key, rooted at keeper.
	KeyPath string
	// Cause is the underlying strict-decode error (carries the yaml.v3 message).
	Cause error
}

func (e *ErrUnknownConfigKey) Error() string {
	return fmt.Sprintf("daemon: project config: unknown key %q under keeper: in %s (unknown keeper keys are rejected; fix the key)",
		e.KeyPath, e.Path)
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
// The only path to single-mode dispatch remains an explicit per-bead
// workflow:single label audited via the review_bypassed event per PL-004a.
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
			"single is not a valid daemon-level default; only an explicit per-bead workflow:single label may enable single mode",
		e.Value, e.Path,
	)
}

// rawDaemonConfig is the per-daemon block in the config.yaml daemon: mapping.
// Unknown keys at this level are silently ignored (forward-compat per PL-004b).
type rawDaemonConfig struct {
	WorkflowMode        string                        `yaml:"workflow_mode"`
	MaxConcurrent       int                           `yaml:"max_concurrent"`
	TargetBranch        string                        `yaml:"target_branch"`         // observability/symmetry only per PL-004b
	AllowedRepos        []string                      `yaml:"allowed_repos"`         // cross-repo dispatch safelist (hk-xfuc)
	RemoteControlPrefix string                        `yaml:"remote_control_prefix"` // per-project Claude RC session-label prefix (hk-igpg)
	RestartBackoff      rawDaemonRestartBackoffConfig `yaml:"restart_backoff"`       // rapid boot-record backoff (hk-b82kn)
}

// rawDaemonRestartBackoffConfig is the daemon.restart_backoff: block.
// All fields are Go duration strings; empty = compiled default.
type rawDaemonRestartBackoffConfig struct {
	Base   string `yaml:"base"`
	Cap    string `yaml:"cap"`
	Window string `yaml:"window"`
}

// rawKeeperContextThresholds holds configurable threshold values in the
// keeper.context_thresholds block. Values ≤ 0 are treated as not configured
// (defer to CLI flag or compiled default). Unknown keys are REJECTED (hk-9f3f).
type rawKeeperContextThresholds struct {
	WarnAbsTokens      int64   `yaml:"warn_abs_tokens"`
	ActAbsTokens       int64   `yaml:"act_abs_tokens"`
	ForceActAbsTokens  int64   `yaml:"force_act_abs_tokens"`
	ForceActAbsOffset  int64   `yaml:"force_act_abs_offset"`
	IdleFloorAbsTokens int64   `yaml:"idle_floor_abs_tokens"`
	ActPctCeil         float64 `yaml:"act_pct_ceil"`
	WarnPctCeil        float64 `yaml:"warn_pct_ceil"`
}

// rawKeeperHardCeiling holds the keeper.hard_ceiling block. Mode is one of
// off|alarm|restart (validated). AbsTokens ≤ 0 = not configured. Cooldown is a
// Go duration STRING (e.g. "5m"); empty = not configured, bare number = error.
type rawKeeperHardCeiling struct {
	Mode      string `yaml:"mode"`
	AbsTokens int64  `yaml:"abs_tokens"`
	Cooldown  string `yaml:"cooldown"`
}

// rawKeeperTimings holds the keeper.timings block. All fields are Go duration
// STRINGS; empty = not configured, a bare number = error.
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

// rawKeeperCadence holds the keeper.cadence block. All fields are Go duration
// STRINGS; empty = not configured, a bare number = error.
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

// rawKeeperBudgets holds the keeper.budgets block. Values ≤ 0 = not configured.
type rawKeeperBudgets struct {
	HeartbeatMaxMisses int `yaml:"heartbeat_max_misses"`
	MaxHandoffTimeouts int `yaml:"max_handoff_timeouts"`
}

// rawKeeperSelfService holds the keeper.self_service block. Enabled /
// InstructOnlyWhenIdle default false; GraceSeconds ≤ 0 = not configured.
//
// CrewsEnabled is a *bool (NOT bool) deliberately: the operator decision (hk-vs4u)
// is that CREWS SELF-RESTART BY DEFAULT, so an ABSENT crews_enabled must resolve to
// TRUE while an explicit `crews_enabled: false` resolves to false. A plain bool
// zero-value cannot distinguish "unset" from "explicit false"; the pointer is nil
// when the key is absent and non-nil (pointing at the parsed value) when present.
// The unset→true resolution is applied in ResolveKeeperConfig. Refs: hk-vs4u.
type rawKeeperSelfService struct {
	Enabled              bool  `yaml:"enabled"`
	GraceSeconds         int   `yaml:"grace_seconds"`
	InstructOnlyWhenIdle bool  `yaml:"instruct_only_when_idle"`
	CrewsEnabled         *bool `yaml:"crews_enabled"`
}

// rawKeeperWarnMessages holds configurable warn text overrides in the
// keeper.warn_messages block. Empty strings are treated as not configured.
type rawKeeperWarnMessages struct {
	DefaultWarnText    string `yaml:"default_warn_text"`
	OnDemandWarnText   string `yaml:"on_demand_warn_text"`
	ActionableWarnText string `yaml:"actionable_warn_text"`
}

// rawKeeperConfig is the keeper: block in config.yaml.
//
// Unknown keys at this level — and in EVERY keeper sub-block (context_thresholds,
// hard_ceiling, timings, cadence, budgets, self_service, warn_messages) — are
// REJECTED with *ErrUnknownConfigKey naming the offending key path (operator
// decision, hk-9f3f). This is enforced by strict yaml.v3 decoding (KnownFields(true))
// of the keeper sub-node ONLY; see strictDecodeKeeperBlock. The daemon: block is
// decoded SEPARATELY and stays tolerant (PL-004b spec requirement).
//
// History: hk-lhu2 originally made unknown keeper keys silently ignored for
// forward-compat while the schema was actively extended (hk-lhu2 → hk-exg3 →
// hk-9kgf). The schema is now stable and silent-ignore was masking real
// misconfiguration (a typo'd key would be silently dropped, defeating the
// operator's intent). hk-9f3f removes silent-ignore for the keeper block.
//
// # Config-schema convention (LOCKED — hk-exg3)
//
// All keeper config lives in ONE keeper: block under schema_version: 1 in
// .harmonik/config.yaml. It is NOT a second file, and it is NOT project.yaml
// (project.yaml is the captain's separate state file under .harmonik/context/,
// unrelated to this loader).
//
// Field-type convention for every present and future keeper sub-field:
//   - Token / count fields are ints (e.g. warn_abs_tokens, max_concurrent).
//   - ALL duration fields are Go duration STRINGS (e.g. "5m", "120s") parsed
//     with time.ParseDuration. A bare number for a duration field MUST fail
//     loudly (time.ParseDuration rejects it) — never silently coerce a number
//     to seconds/nanoseconds.
//
// # Absent-file fast path (hk-exg3)
//
// The empty-file sentinel in parseProjectConfig uses keeperBlockAbsent, an
// explicit field-by-field zero check, NOT `raw.Keeper == (rawKeeperConfig{})`.
// This is deliberate: the moment any forthcoming keeper config bead adds a
// slice / map / nested-non-comparable sub-struct field, the `==` form stops
// compiling. keeperBlockAbsent keeps the absent-file fast path compiling and
// MUST be extended field-by-field whenever a field is added here.
type rawKeeperConfig struct {
	ContextThresholds rawKeeperContextThresholds `yaml:"context_thresholds"`
	HardCeiling       rawKeeperHardCeiling       `yaml:"hard_ceiling"`
	Timings           rawKeeperTimings           `yaml:"timings"`
	Cadence           rawKeeperCadence           `yaml:"cadence"`
	Budgets           rawKeeperBudgets           `yaml:"budgets"`
	SelfService       rawKeeperSelfService       `yaml:"self_service"`
	WarnMessages      rawKeeperWarnMessages      `yaml:"warn_messages"`
}

// keeperBlockAbsent reports whether the keeper: block is at its zero value —
// i.e. no keeper config was supplied in .harmonik/config.yaml. It does an
// explicit field-by-field zero check rather than `raw == (rawKeeperConfig{})`
// so that adding a slice / map / nested-non-comparable sub-struct field to
// rawKeeperConfig (which forthcoming hk-exg3-initiative beads will do) cannot
// break compilation of the absent-file fast path.
//
// INVARIANT (hk-exg3): every field of rawKeeperConfig MUST be checked here.
// When a field is added to rawKeeperConfig (or its sub-structs), extend this
// helper in lockstep.
//
// Bead ref: hk-exg3.
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
		// hard_ceiling
		h.Mode == "" &&
		h.AbsTokens == 0 &&
		h.Cooldown == "" &&
		// timings
		tm.PollInterval == "" &&
		tm.CyclerPollInterval == "" &&
		tm.IdleQuiesce == "" &&
		tm.Staleness == "" &&
		tm.HandoffTimeout == "" &&
		tm.ClearSettle == "" &&
		tm.BootGrace == "" &&
		tm.MaxBootGraceTotal == "" &&
		tm.FlockAcquireGrace == "" &&
		// cadence
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
		// budgets
		b.HeartbeatMaxMisses == 0 &&
		b.MaxHandoffTimeouts == 0 &&
		// self_service
		!s.Enabled &&
		s.GraceSeconds == 0 &&
		!s.InstructOnlyWhenIdle &&
		s.CrewsEnabled == nil &&
		// warn_messages
		w.DefaultWarnText == "" &&
		w.OnDemandWarnText == "" &&
		w.ActionableWarnText == ""
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
	// --session-id) stay bare. Use JoinRemoteControlName to build the label so the
	// format never drifts between launch sites. (hk-igpg)
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

// rawSandboxNetworkConfig is the sandbox.network block in config.yaml.
// Unknown keys are silently ignored (forward-compat, matches daemon: block behaviour).
type rawSandboxNetworkConfig struct {
	Mode                   string   `yaml:"mode"`
	AllowedDomains         []string `yaml:"allowed_domains"`
	WeakerNetworkIsolation bool     `yaml:"weaker_network_isolation"`
	AllowLocalBinding      bool     `yaml:"allow_local_binding"`
}

// rawSandboxCacheConfig is the sandbox.cache block in config.yaml.
type rawSandboxCacheConfig struct {
	WarmRead     []string `yaml:"warm_read"`
	PrivateWrite []string `yaml:"private_write"`
}

// rawSandboxConfig is the sandbox: block in config.yaml (hk-6596l).
// backend is REQUIRED when the block is present (fail-loud per the
// no-hardcoded-defaults principle). Unknown keys are silently ignored.
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

// sandboxBlockAbsent reports whether the sandbox: block is at its zero value,
// i.e. no sandbox config was supplied in .harmonik/config.yaml.
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

// rawHarnessesPiFallbackConfig is the optional harnesses.pi.fallback block.
// All fields are stored as strings; absence is the empty string.
type rawHarnessesPiFallbackConfig struct {
	Provider  string `yaml:"provider"`
	Model     string `yaml:"model"`
	APIKeyEnv string `yaml:"api_key_env"`
}

// rawHarnessesPiProfileConfig is one named profile under harnesses.pi.profiles.
// Full tuple; provider/model/api_key_env required (validated in resolve_pi_config.go).
type rawHarnessesPiProfileConfig struct {
	Provider   string `yaml:"provider"`
	Model      string `yaml:"model"`
	APIKeyEnv  string `yaml:"api_key_env"`
	APIKeyFile string `yaml:"api_key_file"` // OPTIONAL
	BaseURL    string `yaml:"base_url"`     // OPTIONAL
	API        string `yaml:"api"`          // OPTIONAL; defaulted at launch, not here
}

// rawHarnessesPiConfig is the harnesses.pi block in config.yaml.
// REQUIRED: provider, model, api_key_env. OPTIONAL: fallback, api_key_file, base_url, api.
// No defaults — ResolvePiConfig (cmd/harmonik/resolve_pi_config.go) enforces
// fail-loud on any missing required field (PI-050/PI-051).
type rawHarnessesPiConfig struct {
	Provider   string                                 `yaml:"provider"`
	Model      string                                 `yaml:"model"`
	APIKeyEnv  string                                 `yaml:"api_key_env"`
	APIKeyFile string                                 `yaml:"api_key_file"` // OPTIONAL; PI-050/hk-xmfoi
	BaseURL    string                                 `yaml:"base_url"`     // OPTIONAL; locally-hosted OpenAI-compatible endpoints (hk-z13jz)
	API        string                                 `yaml:"api"`          // OPTIONAL; Pi wire format, defaults to "openai" at launch when empty (hk-z13jz)
	Fallback   rawHarnessesPiFallbackConfig           `yaml:"fallback"`
	Profiles   map[string]rawHarnessesPiProfileConfig `yaml:"profiles"` // OPTIONAL; pi-provider-switch
}

// rawHarnessesConfig is the top-level harnesses: block in config.yaml.
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
	// endpoints (e.g. http://dgx.local:8551/v1). When set, buildPiLaunchSpec
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
}

// HarnessesConfig holds the harnesses: top-level block. Zero value when absent.
//
// Bead ref: hk-v7q5u.
type HarnessesConfig struct {
	Pi PiHarnessConfig
}

// rawOpsmonitorConfig is the opsmonitor: block in config.yaml (hk-bi4bg).
// Unknown keys at this level are silently ignored (forward-compat, matches daemon: block behaviour).
// Absent fields resolve to the compiled defaults in ensureOpsMonitorSchedule.
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

// rawWatchdogConfig is the watchdog: block in config.yaml (hk-sbitr).
// Unknown keys at this level are silently ignored (forward-compat, matches daemon: block behaviour).
// Enabled is a *bool so that nil (absent) resolves to the default (true) while an explicit
// false is honoured by the caller without ambiguity.
type rawWatchdogConfig struct {
	Enabled *bool `yaml:"enabled"`
}

// rawSuperviseBackoffConfig is the supervise.restart_backoff: block.
// All fields are Go duration strings; empty = compiled default.
type rawSuperviseBackoffConfig struct {
	Base string `yaml:"base"`
	Cap  string `yaml:"cap"`
}

// rawSuperviseDaemonWatchdogConfig is the supervise.daemon_watchdog: block.
// All fields are Go duration strings; empty = compiled default.
type rawSuperviseDaemonWatchdogConfig struct {
	CheckInterval string `yaml:"check_interval"`
	DialTimeout   string `yaml:"dial_timeout"`
	ReviveBackoff string `yaml:"revive_backoff"`
	ReviveWindow  string `yaml:"revive_window"`
}

// rawSuperviseConfig is the supervise: block in config.yaml.
// Unknown keys at this level are silently ignored (forward-compat, matches daemon: block).
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

// rawWatchConfig is the watch: block in config.yaml (WE7 — captain-wake-economy).
// Unknown keys at this level are silently ignored (forward-compat, matches daemon: block).
// Both target fields default to "captain" when absent (NOT fail-loud — §7 exception).
// WE9 behavioral keys (absent_thresh_s, stall_ticks) are fail-loud when zero/absent.
// WE6 schedule interval keys (liveness_interval, digest_interval) are fail-loud when absent.
type rawWatchConfig struct {
	StatusTarget            string `yaml:"status_target"`
	OpsmonitorTarget        string `yaml:"opsmonitor_target"`
	AbsentThreshSec         int    `yaml:"absent_thresh_s"`           // WE9: seconds before watch-down fires (fail-loud)
	StallTicks              int    `yaml:"stall_ticks"`               // WE9: frozen-cursor ticks before watch-stalled (fail-loud)
	LivenessInterval        string `yaml:"liveness_interval"`         // WE6: Go duration string for mutual-liveness ping (fail-loud)
	DigestInterval          string `yaml:"digest_interval"`           // WE6: Go duration string for verify-services-up (fail-loud)
	StaffingStarvationGrace int    `yaml:"staffing_starvation_grace"` // consecutive ops-monitor digests before staffing-starvation backstop escalates (fail-loud)
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
}

// rawStallSentinelEscalation is the stall_sentinel.escalation: sub-block.
// All fields are Go duration strings. Bead: hk-hm09z.
type rawStallSentinelEscalation struct {
	Tier1Crew     string `yaml:"tier1_crew"`
	Tier2Captain  string `yaml:"tier2_captain"`
	Tier3Operator string `yaml:"tier3_operator"`
}

// rawStallSentinelDetection is the stall_sentinel.detection: sub-block.
// All fields are Go duration strings. Bead: hk-hm09z.
type rawStallSentinelDetection struct {
	RunSilenceStall     string `yaml:"run_silence_stall"`
	ReviewFinalizeStall string `yaml:"review_finalize_stall"`
	RunMaxAge           string `yaml:"run_max_age"`
	LaneNoprogressStall string `yaml:"lane_noprogress_stall"`
}

// rawStallSentinelConfig is the stall_sentinel: block in config.yaml.
// All duration fields are Go duration strings (fail-loud on bare numbers via
// parseDurationField). All fields are optional at the YAML level; the
// fail-loud enforcement (for sentinel startup) is in
// cmd/harmonik/resolve_stall_sentinel_config.go. Bead: hk-hm09z.
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

// stallSentinelBlockAbsent reports whether the stall_sentinel: block is absent
// (all fields at their zero values). Used by the empty-file fast path.
// Field-by-field check avoids the struct-equality footgun (comparable constraint).
// INVARIANT: extend whenever rawStallSentinelConfig gains a new field.
func stallSentinelBlockAbsent(raw rawStallSentinelConfig) bool {
	e := raw.Escalation
	d := raw.Detection
	return e.Tier1Crew == "" &&
		e.Tier2Captain == "" &&
		e.Tier3Operator == "" &&
		d.RunSilenceStall == "" &&
		d.ReviewFinalizeStall == "" &&
		d.RunMaxAge == "" &&
		d.LaneNoprogressStall == ""
}

// rawProjectConfig is the top-level YAML shape for .harmonik/config.yaml.
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
}

// rawAgentConfig is the per-agent-type block inside the agents map.
type rawAgentConfig struct {
	Model  string `yaml:"model"`
	Effort string `yaml:"effort"`
}

// agentConfigEntry holds the resolved (model, effort) pair for a single agent type.
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

// parseProjectConfig decodes raw YAML bytes into a ProjectConfig.
func parseProjectConfig(path string, data []byte) (ProjectConfig, error) {
	var raw rawProjectConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return ProjectConfig{}, &ErrMalformedConfigYAML{Path: path, Cause: err}
	}

	// Empty-file sentinel: schema_version 0 + no agents + no daemon block + no keeper block
	// + no watchdog block → absent semantics. A file with only a daemon: or keeper: block
	// but no schema_version: 1 falls through to the version check below and returns
	// ErrUnsupportedConfigVersion (fail-fast).
	daemonAbsent := raw.Daemon.WorkflowMode == "" && raw.Daemon.MaxConcurrent == 0 &&
		raw.Daemon.TargetBranch == "" && len(raw.Daemon.AllowedRepos) == 0 &&
		raw.Daemon.RemoteControlPrefix == "" &&
		raw.Daemon.RestartBackoff.Base == "" &&
		raw.Daemon.RestartBackoff.Cap == "" &&
		raw.Daemon.RestartBackoff.Window == ""
	watchdogAbsent := raw.Watchdog.Enabled == nil
	watchBlockAbsent := raw.Watch.StatusTarget == "" && raw.Watch.OpsmonitorTarget == ""
	opsmonitorAbsent := raw.Opsmonitor.Interval == "" && raw.Opsmonitor.ScriptPath == ""
	superviseAbsent := superviseBlockAbsent(raw.Supervise)
	harnessesAbsent := raw.Harnesses.Pi.Provider == "" && raw.Harnesses.Pi.Model == "" &&
		raw.Harnesses.Pi.APIKeyEnv == "" &&
		raw.Harnesses.Pi.Fallback == (rawHarnessesPiFallbackConfig{})
	if raw.SchemaVersion == 0 && len(raw.Agents) == 0 &&
		daemonAbsent && keeperBlockAbsent(raw.Keeper) && watchdogAbsent && watchBlockAbsent &&
		opsmonitorAbsent && superviseAbsent && harnessesAbsent && sandboxBlockAbsent(raw.Sandbox) &&
		stallSentinelBlockAbsent(raw.StallSentinel) {
		return ProjectConfig{}, nil
	}

	if raw.SchemaVersion != projectConfigCurrentVersion {
		return ProjectConfig{}, &ErrUnsupportedConfigVersion{
			Path:    path,
			Version: raw.SchemaVersion,
		}
	}

	// hk-rcp7 PL-004b: parse and validate the daemon: block.
	daemonCfg, err := parseDaemonBlock(path, raw.Daemon)
	if err != nil {
		return ProjectConfig{}, err
	}

	// hk-9f3f: REJECT unknown keys under the keeper: block (and every sub-block).
	// This is a strict decode of the keeper sub-node ONLY — the daemon: block
	// above was decoded tolerantly (PL-004b) and is untouched by this check.
	if err := strictDecodeKeeperBlock(path, data); err != nil {
		return ProjectConfig{}, err
	}

	// hk-lhu2 / hk-9kgf: parse the keeper: block. Most values are optional, but
	// a malformed duration string (e.g. a bare number) fails loudly (hk-9kgf).
	keeperCfg, err := parseKeeperBlock(path, raw.Keeper)
	if err != nil {
		return ProjectConfig{}, err
	}

	// hk-sbitr: parse the watchdog: block. Absent → Enabled defaults to true.
	watchdogCfg := parseWatchdogBlock(raw.Watchdog)

	// hk-we7: parse the watch: block. Absent → both target fields are empty strings
	// (callers default to "captain").
	watchCfg := parseWatchBlock(raw.Watch)

	// hk-bi4bg: parse the opsmonitor: block. Both fields are optional; absent =
	// empty string → callers apply the compiled defaults.
	opsmonitorCfg := parseOpsmonitorBlock(raw.Opsmonitor)

	superviseCfg, err := parseSuperviseBlock(path, raw.Supervise)
	if err != nil {
		return ProjectConfig{}, err
	}

	// hk-v7q5u: parse the harnesses: block (PI-050). All fields are optional at
	// the YAML level; ResolvePiConfig enforces fail-loud on missing required values.
	harnessesCfg := parseHarnessesBlock(raw.Harnesses)

	// hk-6596l: parse the sandbox: block. backend is REQUIRED when the block is
	// present; absent block → zero SandboxConfig (Backend=="", no sandboxing).
	var sandboxCfg SandboxConfig
	if !sandboxBlockAbsent(raw.Sandbox) {
		var sErr error
		sandboxCfg, sErr = parseSandboxBlock(path, raw.Sandbox)
		if sErr != nil {
			return ProjectConfig{}, sErr
		}
	}

	// hk-hm09z: parse the stall_sentinel: block. All fields are optional at the
	// YAML level; fail-loud enforcement lives in ResolveStallSentinelConfig.
	stallSentinelCfg, err := parseStallSentinelBlock(path, raw.StallSentinel)
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
	}
	for key, agentRaw := range raw.Agents {
		at := core.AgentType(key)
		// Unknown agent keys are silently ignored (forward-compat per bead spec).
		// We store all keys since AgentType.Valid() is a syntax check; semantic
		// filtering happens at LookupAgent call time via the caller's key.
		cfg.entries[at] = agentConfigEntry{
			model:  agentRaw.Model,
			effort: agentRaw.Effort,
		}
	}

	return cfg, nil
}

// parseDaemonBlock validates and converts a rawDaemonConfig into a DaemonConfig.
//
// Validation rules per PL-004b:
//   - workflow_mode absent → zero DaemonConfig.WorkflowMode (defer to flag/default).
//   - workflow_mode present but not in {review-loop, dot, single} → *ErrMalformedConfigYAML.
//   - workflow_mode == single → *ErrWorkflowModeFloorViolation (PL-004a review floor).
//   - max_concurrent ≤ 0 → treated as not configured (zero DaemonConfig.MaxConcurrent).
//   - target_branch → stored for observability/symmetry only; not used in resolution chain.
func parseDaemonBlock(path string, raw rawDaemonConfig) (DaemonConfig, error) {
	cfg := DaemonConfig{
		TargetBranch: raw.TargetBranch, // observability/symmetry only per PL-004b
	}

	if raw.WorkflowMode != "" {
		wm := core.WorkflowMode(raw.WorkflowMode)
		if !wm.Valid() {
			return DaemonConfig{}, &ErrMalformedConfigYAML{
				Path:  path,
				Cause: fmt.Errorf("daemon.workflow_mode %q: unknown value; must be one of review-loop, dot (single is forbidden at daemon level)", raw.WorkflowMode),
			}
		}
		// PL-004a review floor: single MUST NOT be reachable from the daemon-level
		// default or any config file path.  Only an explicit per-bead workflow:single
		// label (audited via review_bypassed) may dispatch in single mode.
		if wm == core.WorkflowModeSingle {
			return DaemonConfig{}, &ErrWorkflowModeFloorViolation{Path: path, Value: raw.WorkflowMode}
		}
		cfg.WorkflowMode = wm
	}

	// Values ≤ 0 are treated as "not configured" per PL-004b.
	if raw.MaxConcurrent > 0 {
		cfg.MaxConcurrent = raw.MaxConcurrent
	}

	// allowed_repos: stored as-is; nil/empty = cross-repo dispatch not permitted.
	cfg.AllowedRepos = raw.AllowedRepos

	// remote_control_prefix: stored as-is; empty = not configured (bare label). No
	// validation/length cap (operator decision hk-igpg: short default, no hard cap).
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

// keeperNodeEnvelope captures the keeper: sub-node of the top-level config YAML
// as a raw yaml.Node WITHOUT strict decoding, so that sibling top-level keys
// (schema_version, agents, daemon) are tolerated. The captured node is then
// re-decoded strictly IN ISOLATION (strictDecodeKeeperBlock), which is what
// scopes the unknown-key rejection to the keeper block alone. (hk-9f3f)
type keeperNodeEnvelope struct {
	Keeper yaml.Node `yaml:"keeper"`
}

// keeperTypeToPrefix maps each keeper sub-struct's Go type name (as it appears
// in yaml.v3's KnownFields(true) error message) to its dotted key-path prefix
// rooted at keeper. Used to render a precise KeyPath in *ErrUnknownConfigKey.
//
// Bead ref: hk-9f3f.
var keeperTypeToPrefix = map[string]string{
	"rawKeeperConfig":            "keeper",
	"rawKeeperContextThresholds": "keeper.context_thresholds",
	"rawKeeperHardCeiling":       "keeper.hard_ceiling",
	"rawKeeperTimings":           "keeper.timings",
	"rawKeeperCadence":           "keeper.cadence",
	"rawKeeperBudgets":           "keeper.budgets",
	"rawKeeperSelfService":       "keeper.self_service",
	"rawKeeperWarnMessages":      "keeper.warn_messages",
}

// keeperUnknownFieldRe extracts the field name and owning Go type from a single
// yaml.v3 KnownFields(true) error line, e.g.:
//
//	line 6: field warn_abs_token not found in type daemon.rawKeeperContextThresholds
var keeperUnknownFieldRe = regexp.MustCompile(`field (\S+) not found in type (?:[\w.]+\.)?(\w+)`)

// strictDecodeKeeperBlock re-decodes ONLY the keeper: sub-node of the config
// YAML with yaml.v3 KnownFields(true) so that an unknown key anywhere under
// keeper: (the block itself or any sub-block) is REJECTED rather than silently
// ignored (operator decision, hk-9f3f).
//
// SCOPE: it decodes a strictKeeperEnvelope (only a keeper field), so the
// daemon:, agents:, and schema_version: top-level keys are NEVER subjected to
// the strict check — the daemon block keeps its PL-004b unknown-key tolerance.
//
// On an unknown key it returns *ErrUnknownConfigKey whose KeyPath names the
// offending key rooted at keeper (e.g. keeper.context_thresholds.warn_abs_token).
// A malformed-YAML error from the strict decoder that is NOT an unknown-field
// error is surfaced as *ErrMalformedConfigYAML (defensive; the tolerant decode
// in parseProjectConfig already caught structural errors upstream).
func strictDecodeKeeperBlock(path string, data []byte) error {
	// 1. Tolerantly capture ONLY the keeper sub-node, ignoring sibling top-level
	//    keys (schema_version, agents, daemon). No KnownFields here — top-level
	//    tolerance must be preserved.
	var env keeperNodeEnvelope
	if err := yaml.Unmarshal(data, &env); err != nil {
		// Structural error — already surfaced upstream as malformed; be defensive.
		return &ErrMalformedConfigYAML{Path: path, Cause: err}
	}
	// Absent keeper block: zero node (Kind 0) → nothing to validate.
	if env.Keeper.Kind == 0 {
		return nil
	}

	// 2. Re-marshal the isolated keeper node and strict-decode it. KnownFields(true)
	//    now applies ONLY to the keeper sub-tree, so an unknown key under keeper:
	//    or any of its sub-blocks is rejected — while the daemon: block (decoded
	//    separately and tolerantly in parseProjectConfig) is untouched.
	keeperBytes, err := yaml.Marshal(&env.Keeper)
	if err != nil {
		return &ErrMalformedConfigYAML{Path: path, Cause: err}
	}
	var probe rawKeeperConfig
	dec := yaml.NewDecoder(bytes.NewReader(keeperBytes))
	dec.KnownFields(true)
	err = dec.Decode(&probe)
	if err == nil || errors.Is(err, io.EOF) {
		return nil
	}

	// yaml.v3 reports unknown fields as a TypeError whose message lists one line
	// per offending field: "field <name> not found in type <pkg>.<Type>".
	msg := err.Error()
	if m := keeperUnknownFieldRe.FindStringSubmatch(msg); m != nil {
		field, typeName := m[1], m[2]
		prefix, ok := keeperTypeToPrefix[typeName]
		if !ok {
			// Unknown owning type (should not happen for a keeper sub-node) —
			// fall back to a keeper-rooted path so the operator still sees the key.
			prefix = "keeper"
		}
		return &ErrUnknownConfigKey{
			Path:    path,
			KeyPath: prefix + "." + field,
			Cause:   err,
		}
	}

	// Not an unknown-field error: surface as malformed (defensive).
	return &ErrMalformedConfigYAML{Path: path, Cause: err}
}

// parseDurationField parses a Go duration STRING into a time.Duration.
//
// Contract (hk-9kgf, operator decision): a duration field MUST be a Go duration
// STRING (e.g. "5m", "120s", "1h30m"). It FAILS LOUDLY — returning
// *ErrMalformedConfigYAML naming the offending key — on a bare number or any
// other unparseable value. It MUST NEVER silently coerce a number to
// seconds/nanoseconds: bad config is an operator error and must surface.
//
// An empty string means "not configured": parseDurationField returns (0, nil)
// so the resolver later applies the compiled default.
//
// Bead ref: hk-9kgf.
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

// parseKeeperBlock converts a rawKeeperConfig into a KeeperConfig.
//
// Most values are optional; ≤ 0 / empty strings are stored as zero values so
// callers can detect "not configured" and defer to the CLI flag or compiled
// default. Two classes of value FAIL LOUDLY (hk-9kgf):
//   - any duration field whose string is unparseable (e.g. a bare number) →
//     *ErrMalformedConfigYAML naming the key (via parseDurationField).
//   - hard_ceiling.mode whose value is not one of off|alarm|restart.
//
// Per-field validation (pct in 0..1, mode enum, duration parses) is done HERE.
// Cross-field invariants (warn < act < force) are NOT checked here — they run
// post-resolution in a later bead.
//
// Unknown YAML keys at any level under keeper: are REJECTED (operator decision,
// hk-9f3f) — strict decoding happens upstream in strictDecodeKeeperBlock before
// this function runs; parseKeeperBlock receives an already-validated rawKeeperConfig.
//
// Bead ref: hk-lhu2, hk-9kgf, hk-9f3f.
func parseKeeperBlock(path string, raw rawKeeperConfig) (KeeperConfig, error) {
	cfg := KeeperConfig{}

	// ── context_thresholds ──
	t := raw.ContextThresholds
	// Values ≤ 0 are treated as "not configured" — defer to CLI flag or compiled default.
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
	// pct fields: per-field validation — must be in (0, 1]. ≤ 0 = not configured.
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

	// ── hard_ceiling ──
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
	// NOTE: Present.HardCeilingCooldown tracks cadence.hard_ceiling_cooldown (the key
	// the resolver's HardCeilingCooldown reads), set in the cadence loop below — NOT
	// this hard_ceiling.cooldown field (HardCeilingCooldownDur), which the resolver
	// does not consume.

	// ── timings (all durations) ──
	// present records whether the raw STRING was non-empty (present even for "0s"),
	// so the operator-facing resolver can tell unset from an explicit zero (boot_grace).
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

	// ── cadence (all durations) ──
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

	// ── budgets ──
	b := raw.Budgets
	if b.HeartbeatMaxMisses > 0 {
		cfg.HeartbeatMaxMisses = b.HeartbeatMaxMisses
		cfg.Present.HeartbeatMaxMisses = true
	}
	if b.MaxHandoffTimeouts > 0 {
		cfg.MaxHandoffTimeouts = b.MaxHandoffTimeouts
		cfg.Present.MaxHandoffTimeouts = true
	}

	// ── self_service ──
	s := raw.SelfService
	cfg.SelfServiceEnabled = s.Enabled
	if s.GraceSeconds > 0 {
		cfg.SelfServiceGraceSeconds = s.GraceSeconds
	}
	cfg.SelfServiceInstructOnlyWhenIdle = s.InstructOnlyWhenIdle
	// crews_enabled: carry the nil/non-nil pointer through verbatim. ResolveKeeperConfig
	// resolves nil (absent) → true (operator decision: crews self-restart, hk-vs4u).
	cfg.SelfServiceCrewsEnabled = s.CrewsEnabled

	// ── warn_messages ── empty strings are "not configured" — defer to compiled default.
	cfg.DefaultWarnText = raw.WarnMessages.DefaultWarnText
	cfg.ActionableWarnText = raw.WarnMessages.ActionableWarnText
	// Dedup (hk-vs4u): on_demand_warn_text is DEPRECATED in favour of the single key
	// actionable_warn_text, but it stays a RECOGNIZED key (rawKeeperWarnMessages still
	// declares it) so old strict configs (hk-9f3f) do not hard-error. Map the
	// deprecated value onto ActionableWarnText with a log warning, UNLESS the new key
	// was already set (the new key wins on conflict).
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

// parseWatchdogBlock converts a rawWatchdogConfig into a WatchdogConfig.
//
// When Enabled is nil (absent from the YAML) the function defaults to true —
// the operator brief (hk-sbitr) specifies "cheap to leave on" and default-ON
// is the correct behaviour when the key is omitted. An explicit false is
// honoured verbatim.
//
// Bead ref: hk-sbitr.
func parseWatchdogBlock(raw rawWatchdogConfig) WatchdogConfig {
	if raw.Enabled == nil {
		return WatchdogConfig{Enabled: true}
	}
	return WatchdogConfig{Enabled: *raw.Enabled}
}

func superviseBlockAbsent(raw rawSuperviseConfig) bool {
	return raw.HeartbeatTTL == "" &&
		raw.StartTimeout == "" &&
		raw.CrashLoopWindow == "" &&
		raw.HealthProbeInterval == "" &&
		raw.StopTimeout == "" &&
		raw.RestartBackoff.Base == "" &&
		raw.RestartBackoff.Cap == "" &&
		raw.DaemonWatchdog.CheckInterval == "" &&
		raw.DaemonWatchdog.DialTimeout == "" &&
		raw.DaemonWatchdog.ReviveBackoff == "" &&
		raw.DaemonWatchdog.ReviveWindow == ""
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

// parseWatchBlock converts a rawWatchConfig into a WatchConfig.
// Both target fields are optional; empty strings mean "not configured"
// and callers apply the "captain" default (NOT here — so callers can
// distinguish "absent" from an explicit "captain").
func parseWatchBlock(raw rawWatchConfig) WatchConfig {
	return WatchConfig{
		StatusTarget:            raw.StatusTarget,
		OpsmonitorTarget:        raw.OpsmonitorTarget,
		AbsentThreshSec:         raw.AbsentThreshSec,
		StallTicks:              raw.StallTicks,
		LivenessInterval:        raw.LivenessInterval,
		DigestInterval:          raw.DigestInterval,
		StaffingStarvationGrace: raw.StaffingStarvationGrace,
	}
}

// parseOpsmonitorBlock converts a rawOpsmonitorConfig into an OpsmonitorConfig.
// Both fields are optional; empty strings mean "not configured" and callers
// apply the compiled defaults in ensureOpsMonitorSchedule ("5m" interval,
// "scripts/ops-monitor-check.sh" script path).
//
// Bead ref: hk-bi4bg.
func parseOpsmonitorBlock(raw rawOpsmonitorConfig) OpsmonitorConfig {
	return OpsmonitorConfig{
		Interval:   raw.Interval,
		ScriptPath: raw.ScriptPath,
	}
}

// parseStallSentinelBlock converts a rawStallSentinelConfig into a
// StallSentinelConfig. All duration fields are optional at the YAML level;
// a bare number (not a Go duration string) fails loudly via parseDurationField.
// Required-value enforcement (fail-loud when any field is zero at startup) is
// delegated to cmd/harmonik/resolve_stall_sentinel_config.go.
//
// Bead ref: hk-hm09z.
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

// parseHarnessesBlock converts a rawHarnessesConfig into a HarnessesConfig.
//
// All Pi fields are stored verbatim from the YAML except APIKeyFile, where a
// leading ~ is expanded to the user's home directory (PI-050/hk-sv3vg). All
// other validation and required-field enforcement is left to ResolvePiConfig
// (cmd/harmonik/resolve_pi_config.go), the operator-facing chokepoint.
//
// HasFallback is set to true when any fallback sub-field is non-empty (i.e. the
// operator wrote at least one key under harnesses.pi.fallback:).
//
// Spec refs: PI-050. Bead refs: hk-v7q5u, hk-sv3vg.
func parseHarnessesBlock(raw rawHarnessesConfig) HarnessesConfig {
	pi := raw.Pi
	hasFallback := pi.Fallback.Provider != "" || pi.Fallback.Model != "" || pi.Fallback.APIKeyEnv != ""
	apiKeyFile, _ := daemonExpandHomePath(pi.APIKeyFile)
	// Copy profiles verbatim (APIKeyFile ~ expansion is done later in ResolvePiConfig).
	var profiles map[string]PiProfileConfig
	if len(pi.Profiles) > 0 {
		profiles = make(map[string]PiProfileConfig, len(pi.Profiles))
		for name, rp := range pi.Profiles {
			profiles[name] = PiProfileConfig{
				Provider:   rp.Provider,
				Model:      rp.Model,
				APIKeyEnv:  rp.APIKeyEnv,
				APIKeyFile: rp.APIKeyFile,
				BaseURL:    rp.BaseURL,
				API:        rp.API,
			}
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
			Profiles: profiles,
		},
	}
}

// parseSandboxBlock converts a rawSandboxConfig into a SandboxConfig.
//
// Validation:
//   - backend MUST be present when the block is present (fail-loud per the
//     no-hardcoded-defaults principle; the caller checks sandboxBlockAbsent before
//     calling here so we only arrive when at least one field is non-zero).
//   - backend must be one of "srt" or "none".
//   - All other fields are optional; nil slices are preserved as nil.
//
// v1 network mode is "open" (locked per SPIKE-FINDINGS §TLS DECISION); the value
// is stored verbatim from config for forward-compat but not further validated here.
//
// Bead ref: hk-6596l.
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

// daemonExpandHomePath expands a leading ~ to the user's home directory.
// Returns the path unchanged when it does not start with ~.
// Mirrors expandHomePath in cmd/harmonik/resolve_pi_config.go (CLI path);
// kept separate to avoid a cmd→internal import cycle. Bead: hk-sv3vg.
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
