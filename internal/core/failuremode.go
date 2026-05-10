package core

// FailureMode is the typed alias for the `failure_mode` field of the
// daemon_startup_failed event (event-model.md §8.7.4).
//
// The failure mode string identifies the category of startup failure per
// operator-nfr.md §8. The set is open (new modes may be added in future
// revisions per the N-1 window rule); consumers MUST treat unrecognised values
// as opaque strings and MUST NOT reject them.
//
// Known MVH values (informative; normative source is operator-nfr.md §8):
//
//   - "binary-stamp-missing"             — binary lacks embedded ldflags commit-hash stamp (ON-005a)
//   - "upgrade-hash-mismatch-on-restart" — restarted binary hash differs from the .harmonik/daemon.upgrading marker (ON-020a)
//
// Spec ref: event-model.md §8.7.4; operator-nfr.md §8.
// Bead ref: hk-hqwn.71.
type FailureMode string
