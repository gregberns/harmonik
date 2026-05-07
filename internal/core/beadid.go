package core

// BeadID is an opaque stable bead identifier assigned by the Beads store
// (execution-model.md §6.1).
//
// Opacity discipline (beads-integration.md BI-008 + BI-008a):
//
//   - BI-008: A bead ID MUST be stable from creation to tombstone.  Harmonik
//     relies on this stability for run-metadata bindings, checkpoint trailers,
//     event payloads, and session-log metadata.
//
//   - BI-008a: Bead IDs are scoped to a single Beads store per project.  The
//     adapter (forthcoming hk-872.27 br-CLI adapter) MUST treat bead_id as
//     opaque — no parsing, no minting, no rewriting.
//
// Consequently, this package intentionally provides NO Parse/Mint/Generate/New
// helpers for BeadID.  IDs are obtained exclusively through the br CLI adapter
// and are carried through harmonik as opaque strings.
type BeadID string
