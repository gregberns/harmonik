package main

// init_skill_assets.go — embedded fleet skills, AGENTS template, and scaffold stubs
// for the harmonik binary.
//
// Asset layout (all under cmd/harmonik/assets/):
//
//	assets/skills/<skill-name>/<file>  — 9 fleet skills (copies of .claude/skills/)
//	assets/templates/AGENTS.template.md — foreign-repo AGENTS template variant
//	assets/scaffolds/{AGENT_INDEX,STATUS,TASKS}.md — minimal committed stub files
//
// The canonical source for each skill file is .claude/skills/<skill-name>/<file>.
// The copies in assets/skills/ must be kept byte-identical; the sync-guard test
// in init_skills_sync_test.go enforces parity and will fail if one copy is edited
// without updating the other.
//
// To re-sync after editing a skill in .claude/skills/:
//
//	cp .claude/skills/<skill>/<file> cmd/harmonik/assets/skills/<skill>/<file>
//
// The assets/templates/AGENTS.template.md is a foreign-repo variant of
// docs/templates/AGENTS.template.md with in-repo-only references pruned.
// It is NOT byte-identical to the in-repo template and has no sync-guard test.
//
// Bead ref: hk-7iyh (fleet-portability T11).

import "embed"

//go:embed assets
var initSkillAssets embed.FS
