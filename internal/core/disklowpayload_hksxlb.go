package core

// disklowpayload_hksxlb.go — payload type for the disk_low event type.
//
// Emitted by the daemon work loop when available disk space on the project
// filesystem falls below the configured watermark (default 10 GiB). The daemon
// pauses new bead dispatch while disk_low is active and attempts a
// `go clean -cache` to reclaim the Go build cache (typically 10–20 GiB).
//
// Refs: hk-sxlb.

// DiskLowPayload is the event-bus payload for the disk_low event type.
//
// Emitted by the daemon when available disk space falls below the watermark.
// Carries enough context to diagnose the source and confirm whether the
// go-cache reap was attempted.
//
// # Payload fields
//
//   - available_bytes         — bytes of free disk space at detection time (>= 0)
//   - watermark_bytes         — configured free-space floor below which dispatch pauses (> 0)
//   - project_path            — absolute path of the project filesystem being probed (required)
//   - go_cache_clean_attempted — true when `go clean -cache` was run during this event
//   - go_cache_clean_error    — error message if go clean -cache failed; empty on success or skip
//   - detected_at             — RFC 3339 timestamp of the check
type DiskLowPayload struct {
	// AvailableBytes is the free bytes available to non-root processes at
	// detection time. Non-negative.
	AvailableBytes uint64 `json:"available_bytes"`

	// WatermarkBytes is the configured free-space floor. Dispatch is paused
	// while AvailableBytes < WatermarkBytes. Always positive.
	WatermarkBytes uint64 `json:"watermark_bytes"`

	// ProjectPath is the absolute path whose filesystem was probed.
	// Required (non-empty).
	ProjectPath string `json:"project_path"`

	// GoCacheCleanAttempted is true when `go clean -cache` was invoked during
	// this event cycle to reclaim build-cache space.
	GoCacheCleanAttempted bool `json:"go_cache_clean_attempted"`

	// GoCacheCleanError holds the error message if go clean -cache failed.
	// Empty when the clean succeeded or was not attempted.
	GoCacheCleanError string `json:"go_cache_clean_error,omitempty"`

	// DetectedAt is the RFC 3339 timestamp of the disk check.
	// Required (non-empty).
	DetectedAt string `json:"detected_at"`
}

// Valid reports whether p is a well-formed DiskLowPayload.
func (p DiskLowPayload) Valid() bool {
	return p.WatermarkBytes > 0 && p.ProjectPath != "" && p.DetectedAt != ""
}
