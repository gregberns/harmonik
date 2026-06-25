package main

// ops_monitor_cmd.go — `harmonik ops-monitor` CLI subcommand (hk-qpzsv).
//
// Installs/uninstalls a launchd LaunchAgent plist so ops-monitor-check.sh
// runs every 5 minutes independent of any Claude or captain session.
//
// Verbs:
//
//	install   [--project DIR] [--no-load]  — write plist + launchctl load
//	uninstall [--project DIR]              — launchctl unload + remove plist
//	status    [--project DIR]              — show plist + load state
//
// The plist label is com.harmonik.ops-monitor.<project-hash> (12-char SHA-256
// prefix of realpath(project-dir)), so one machine can host multiple projects
// without conflict.
//
// Plist location: ~/Library/LaunchAgents/<label>.plist
//
// Spec ref: hk-qpzsv.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/gregberns/harmonik/internal/lifecycle"
)

const opsMonitorPlistLabelPrefix = "com.harmonik.ops-monitor"

var opsMonitorPlistTmpl = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>/bin/bash</string>
        <string>{{.ScriptPath}}</string>
    </array>
    <key>WorkingDirectory</key>
    <string>{{.ProjectDir}}</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>HK_PROJECT</key>
        <string>{{.ProjectDir}}</string>
        <key>PATH</key>
        <string>{{.BinPath}}:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
    </dict>
    <key>StartInterval</key>
    <integer>300</integer>
    <key>StandardOutPath</key>
    <string>{{.LogDir}}/launchd.out.log</string>
    <key>StandardErrorPath</key>
    <string>{{.LogDir}}/launchd.err.log</string>
    <key>RunAtLoad</key>
    <false/>
</dict>
</plist>
`))

type opsMonitorPlistData struct {
	Label      string
	ScriptPath string
	ProjectDir string
	BinPath    string
	LogDir     string
}

func runOpsMonitorSubcommand(args []string) int {
	verb := ""
	if len(args) > 0 {
		verb = args[0]
	}
	rest := []string{}
	if len(args) > 1 {
		rest = args[1:]
	}
	switch verb {
	case "install":
		return runOpsMonitorInstall(rest)
	case "uninstall":
		return runOpsMonitorUninstall(rest)
	case "status":
		return runOpsMonitorStatus(rest)
	case "--help", "-h", "help", "":
		fmt.Print(opsMonitorTopUsage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "harmonik ops-monitor: unknown verb %q\n\n", verb)
		fmt.Fprint(os.Stderr, opsMonitorTopUsage)
		return 2
	}
}

const opsMonitorTopUsage = `harmonik ops-monitor — manage the launchd LaunchAgent for ops-monitor-check.sh

VERBS
  install   [--project DIR] [--no-load]
              Write plist to ~/Library/LaunchAgents/ and load it via launchctl.
              After install the probe runs every 5 min independent of any session.

  uninstall [--project DIR]
              Unload the plist from launchd and remove the file.

  status    [--project DIR]
              Print plist path, load state, and last latest.json timestamp.

FLAGS (all verbs)
  --project DIR  Project directory (default: current working directory)

  install only:
  --no-load      Write the plist but do not call launchctl load (for testing)

NOTES
  One plist per project (keyed by project hash). Multiple projects on the same
  machine install separate LaunchAgents without conflict.

  The launchd job supplements the daemon-internal schedule (hk-7xr9): it keeps
  ops-monitor running even when the daemon is temporarily down, writing
  latest.json and emitting daemon-down alerts.

BEAD
  hk-qpzsv
`

func resolveOpsMonitorProjectDir(args []string) (projectDir string, remaining []string, err error) {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--project" && i+1 < len(args):
			i++
			projectDir = args[i]
		case len(args[i]) > 10 && args[i][:10] == "--project=":
			projectDir = args[i][10:]
		default:
			remaining = append(remaining, args[i])
		}
	}
	if projectDir == "" {
		wd, werr := os.Getwd()
		if werr != nil {
			return "", nil, fmt.Errorf("cannot determine working directory: %w", werr)
		}
		projectDir = wd
	}
	abs, aerr := filepath.Abs(projectDir)
	if aerr != nil {
		return "", nil, fmt.Errorf("cannot resolve path %q: %w", projectDir, aerr)
	}
	real, rerr := filepath.EvalSymlinks(abs)
	if rerr != nil {
		return "", nil, fmt.Errorf("cannot resolve real path of %q: %w", abs, rerr)
	}
	return real, remaining, nil
}


func opsMonitorPlistPath(projectDir string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	label := opsMonitorPlistLabelFor(projectDir)
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
}

func opsMonitorPlistLabelFor(projectDir string) string {
	hash := lifecycle.ComputeProjectHash(projectDir)
	return opsMonitorPlistLabelPrefix + "." + hash.String()
}

func buildOpsMonitorPlistData(projectDir string) (opsMonitorPlistData, error) {
	label := opsMonitorPlistLabelFor(projectDir)
	scriptPath := filepath.Join(projectDir, "scripts", "ops-monitor-check.sh")
	logDir := filepath.Join(projectDir, ".harmonik", "ops-monitor")

	// Determine bin directory: prefer the directory containing the running binary,
	// fall back to exec.LookPath("harmonik") so the plist PATH can find it at runtime.
	binDir := ""
	if exe, err := os.Executable(); err == nil {
		binDir = filepath.Dir(exe)
	}
	if binDir == "" {
		if hk, err := exec.LookPath("harmonik"); err == nil {
			binDir = filepath.Dir(hk)
		}
	}
	if binDir == "" {
		binDir = "/usr/local/bin"
	}

	return opsMonitorPlistData{
		Label:      label,
		ScriptPath: scriptPath,
		ProjectDir: projectDir,
		BinPath:    binDir,
		LogDir:     logDir,
	}, nil
}

func runOpsMonitorInstall(args []string) int {
	noLoad := false
	var remaining []string
	for _, a := range args {
		if a == "--no-load" {
			noLoad = true
		} else if a == "--help" || a == "-h" {
			fmt.Print(opsMonitorTopUsage)
			return 0
		} else {
			remaining = append(remaining, a)
		}
	}

	projectDir, _, err := resolveOpsMonitorProjectDir(remaining)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik ops-monitor install: %v\n", err)
		return 1
	}

	// Verify the script exists.
	scriptPath := filepath.Join(projectDir, "scripts", "ops-monitor-check.sh")
	if _, serr := os.Stat(scriptPath); serr != nil {
		fmt.Fprintf(os.Stderr, "harmonik ops-monitor install: script not found at %s\n", scriptPath)
		fmt.Fprintf(os.Stderr, "  Is --project pointing to the harmonik repo root?\n")
		return 1
	}

	data, derr := buildOpsMonitorPlistData(projectDir)
	if derr != nil {
		fmt.Fprintf(os.Stderr, "harmonik ops-monitor install: %v\n", derr)
		return 1
	}

	plistPath, perr := opsMonitorPlistPath(projectDir)
	if perr != nil {
		fmt.Fprintf(os.Stderr, "harmonik ops-monitor install: %v\n", perr)
		return 1
	}

	// Ensure log directory exists.
	//nolint:gosec // G301: log dir created with explicit 0755 permissions under project tree
	if merr := os.MkdirAll(data.LogDir, 0o755); merr != nil {
		fmt.Fprintf(os.Stderr, "harmonik ops-monitor install: cannot create log dir %s: %v\n", data.LogDir, merr)
		return 1
	}

	// Render plist.
	var buf strings.Builder
	if terr := opsMonitorPlistTmpl.Execute(&buf, data); terr != nil {
		fmt.Fprintf(os.Stderr, "harmonik ops-monitor install: render plist: %v\n", terr)
		return 1
	}

	// If a plist already exists, unload it first so the reload picks up changes.
	if _, existErr := os.Stat(plistPath); existErr == nil {
		_ = exec.CommandContext(context.Background(), "launchctl", "unload", "-w", plistPath).Run()
	}

	// Write plist.
	if werr := os.WriteFile(plistPath, []byte(buf.String()), 0o644); werr != nil {
		fmt.Fprintf(os.Stderr, "harmonik ops-monitor install: write plist: %v\n", werr)
		return 1
	}
	fmt.Printf("ops-monitor: wrote plist %s\n", plistPath)

	if noLoad {
		fmt.Println("ops-monitor: --no-load: skipping launchctl load")
		return 0
	}

	// Load the plist.
	out, lerr := exec.CommandContext(context.Background(), "launchctl", "load", "-w", plistPath).CombinedOutput()
	if lerr != nil {
		fmt.Fprintf(os.Stderr, "harmonik ops-monitor install: launchctl load: %v\n%s\n", lerr, out)
		fmt.Fprintf(os.Stderr, "  Plist written to %s — load it manually with:\n", plistPath)
		fmt.Fprintf(os.Stderr, "  launchctl load -w %s\n", plistPath)
		return 1
	}
	fmt.Printf("ops-monitor: loaded — probe runs every 5m independent of any session\n")
	fmt.Printf("  label:   %s\n", data.Label)
	fmt.Printf("  project: %s\n", data.ProjectDir)
	fmt.Printf("  script:  %s\n", data.ScriptPath)
	fmt.Printf("  logs:    %s/launchd.{out,err}.log\n", data.LogDir)
	return 0
}

func runOpsMonitorUninstall(args []string) int {
	projectDir, _, err := resolveOpsMonitorProjectDir(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik ops-monitor uninstall: %v\n", err)
		return 1
	}

	plistPath, perr := opsMonitorPlistPath(projectDir)
	if perr != nil {
		fmt.Fprintf(os.Stderr, "harmonik ops-monitor uninstall: %v\n", perr)
		return 1
	}

	if _, serr := os.Stat(plistPath); os.IsNotExist(serr) {
		fmt.Printf("ops-monitor: not installed (no plist at %s)\n", plistPath)
		return 0
	}

	// Unload.
	out, lerr := exec.CommandContext(context.Background(), "launchctl", "unload", "-w", plistPath).CombinedOutput()
	if lerr != nil {
		fmt.Fprintf(os.Stderr, "harmonik ops-monitor uninstall: launchctl unload: %v\n%s\n", lerr, out)
		// Continue to remove the plist even if unload fails (e.g., already unloaded).
	} else {
		fmt.Println("ops-monitor: unloaded from launchd")
	}

	if rerr := os.Remove(plistPath); rerr != nil {
		fmt.Fprintf(os.Stderr, "harmonik ops-monitor uninstall: remove plist: %v\n", rerr)
		return 1
	}
	fmt.Printf("ops-monitor: removed plist %s\n", plistPath)
	return 0
}

func runOpsMonitorStatus(args []string) int {
	projectDir, _, err := resolveOpsMonitorProjectDir(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "harmonik ops-monitor status: %v\n", err)
		return 1
	}

	label := opsMonitorPlistLabelFor(projectDir)
	plistPath, perr := opsMonitorPlistPath(projectDir)
	if perr != nil {
		fmt.Fprintf(os.Stderr, "harmonik ops-monitor status: %v\n", perr)
		return 1
	}

	plistExists := false
	if _, serr := os.Stat(plistPath); serr == nil {
		plistExists = true
	}

	fmt.Printf("label:   %s\n", label)
	fmt.Printf("plist:   %s", plistPath)
	if plistExists {
		fmt.Println(" (exists)")
	} else {
		fmt.Println(" (not installed)")
	}

	// Check launchctl list for the label.
	loaded := false
	if plistExists {
		out, _ := exec.CommandContext(context.Background(), "launchctl", "list", label).CombinedOutput()
		outStr := strings.TrimSpace(string(out))
		if !strings.Contains(outStr, "Could not find service") && outStr != "" {
			loaded = true
			fmt.Printf("launchd: loaded\n")
			fmt.Printf("  %s\n", strings.ReplaceAll(outStr, "\n", "\n  "))
		} else {
			fmt.Printf("launchd: not loaded\n")
		}
	}

	// Show latest.json timestamp.
	latestPath := filepath.Join(projectDir, ".harmonik", "ops-monitor", "latest.json")
	if info, serr := os.Stat(latestPath); serr == nil {
		fmt.Printf("latest.json: %s (mtime %s)\n", latestPath, info.ModTime().Format("2006-01-02T15:04:05Z07:00"))
	} else {
		fmt.Printf("latest.json: not present (%s)\n", latestPath)
	}

	if !plistExists {
		fmt.Println("\nRun: harmonik ops-monitor install [--project DIR]")
		return 1
	}
	if !loaded {
		fmt.Printf("\nPlist exists but not loaded. Load with:\n  launchctl load -w %s\n", plistPath)
		return 1
	}
	return 0
}
