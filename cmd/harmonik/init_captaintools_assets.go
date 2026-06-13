package main

// init_captaintools_assets.go — embedded captain-tools scripts for the harmonik binary.
//
// The canonical source of truth for each script is scripts/captain-tools/<name>.
// The copy here (cmd/harmonik/captain-tools/<name>) is the embed target; keep in
// sync with the scripts/ copy.  The sync-guard test in captaintools_sync_test.go
// enforces byte-identical parity — it will fail if one copy is edited without
// updating the other.
//
// To re-sync after editing scripts/captain-tools/captain-launch.sh:
//
//	cp scripts/captain-tools/captain-launch.sh cmd/harmonik/captain-tools/captain-launch.sh
//
// Bead ref: hk-9df (fleet-portability T8).

import _ "embed"

//go:embed captain-tools/captain-launch.sh
var captainLaunchSh []byte
