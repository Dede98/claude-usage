package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	plistLabel = "com.claude-usage.daemon"
	configName = "claude-usage-config.json"
)

type installConfig struct {
	OriginalStatusline string `json:"original_statusline,omitempty"`
	BinaryPath         string `json:"binary_path"`
	InstalledAt        string `json:"installed_at"`
}

// ── Install ────────────────────────────────────────────────────────

func runInstall(args []string) {
	withDaemon := false
	for _, a := range args {
		if a == "--daemon" {
			withDaemon = true
		}
	}

	home, _ := os.UserHomeDir()
	binPath := resolveOwnBinary()
	claudeDir := filepath.Join(home, ".claude")
	binDir := filepath.Join(home, ".local", "bin")
	symlink := filepath.Join(binDir, "claude-usage")

	// 1. Symlink to ~/.local/bin
	os.MkdirAll(binDir, 0755)
	os.Remove(symlink) // remove stale symlink
	if binPath != symlink {
		if err := os.Symlink(binPath, symlink); err != nil {
			fmt.Printf("  Note: could not create symlink at %s: %s\n", symlink, err)
			fmt.Printf("  Make sure %s is in your PATH.\n", filepath.Dir(binPath))
		} else {
			fmt.Printf("  %s✓%s CLI linked at %s\n", colorGreen, colorReset, symlink)
		}
	} else {
		fmt.Printf("  %s✓%s CLI already at %s\n", colorGreen, colorReset, symlink)
	}

	// 2. Configure statusline
	settingsPath := filepath.Join(claudeDir, "settings.json")
	originalCmd := configureStatusline(settingsPath, symlink)

	// 3. Save install config
	cfg := installConfig{
		OriginalStatusline: originalCmd,
		BinaryPath:         symlink,
		InstalledAt:        fmt.Sprintf("%d", os.Getpid()), // simple marker
	}
	cfgData, _ := json.MarshalIndent(cfg, "", "  ")
	os.MkdirAll(claudeDir, 0755)
	os.WriteFile(filepath.Join(claudeDir, configName), cfgData, 0644)

	// 4. Daemon
	if withDaemon {
		installDaemon(symlink, home)
	}

	// 5. Verify
	fmt.Println()
	fmt.Printf("%sVerifying...%s\n", colorDim, colorReset)
	data := fetchUsageSnapshot()
	writeUsageFile(data)
	if len(data.Providers) == 0 {
		fmt.Println()
		fmt.Println("No provider data available. Make sure at least one is configured:")
		fmt.Println("  1. Claude Code is installed and logged in (run: claude /login)")
		fmt.Println("  2. Codex is installed and logged in with ChatGPT")
	} else {
		formatStatus(data)
		fmt.Printf("\n%sDone.%s Run %sclaude-usage%s to see your limits.\n", colorGreen, colorReset, colorBold, colorReset)
		fmt.Println("Claude Code statusline was configured automatically.")
		fmt.Println("Codex CLI has its own native status line. In Codex, run /statusline and enable:")
		fmt.Println("  - Remaining usage on 5-hour usage limit")
		fmt.Println("  - Remaining usage on weekly usage limit")
	}
}

func configureStatusline(settingsPath, binaryPath string) string {
	ourCmd := binaryPath + " statusline"
	var originalCmd string

	// Read existing settings
	settings := make(map[string]interface{})
	if raw, err := os.ReadFile(settingsPath); err == nil {
		json.Unmarshal(raw, &settings)
	}

	// Check for existing statusline
	if sl, ok := settings["statusLine"].(map[string]interface{}); ok {
		if cmd, ok := sl["command"].(string); ok && cmd != "" {
			// Already ours?
			if strings.Contains(cmd, "claude-usage") {
				fmt.Printf("  %s✓%s Statusline already configured\n", colorGreen, colorReset)
				return ""
			}
			// Wrap existing
			originalCmd = cmd
			ourCmd = fmt.Sprintf("%s statusline --wrap %q", binaryPath, cmd)
		}
	}

	settings["statusLine"] = map[string]interface{}{
		"type":    "command",
		"command": ourCmd,
	}

	out, _ := json.MarshalIndent(settings, "", "  ")
	os.WriteFile(settingsPath, append(out, '\n'), 0644)

	if originalCmd != "" {
		fmt.Printf("  %s✓%s Statusline configured (wrapping existing)\n", colorGreen, colorReset)
	} else {
		fmt.Printf("  %s✓%s Statusline configured\n", colorGreen, colorReset)
	}
	return originalCmd
}

func installDaemon(binaryPath, home string) {
	plistDir := filepath.Join(home, "Library", "LaunchAgents")
	plistPath := filepath.Join(plistDir, plistLabel+".plist")
	logPath := filepath.Join(home, ".claude", "usage-daemon.log")

	os.MkdirAll(plistDir, 0755)

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>--daemon</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardErrorPath</key>
    <string>%s</string>
    <key>StandardOutPath</key>
    <string>/dev/null</string>
    <key>ThrottleInterval</key>
    <integer>60</integer>
</dict>
</plist>
`, plistLabel, binaryPath, logPath)

	os.WriteFile(plistPath, []byte(plist), 0644)

	// Stop existing, start new
	exec.Command("launchctl", "unload", plistPath).Run()
	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		fmt.Printf("  %sWarning: could not start daemon: %s%s\n", colorYellow, err, colorReset)
	} else {
		fmt.Printf("  %s✓%s Daemon installed and started (polls every 60s)\n", colorGreen, colorReset)
	}
}

// ── Uninstall ──────────────────────────────────────────────────────

func runUninstall() {
	home, _ := os.UserHomeDir()
	claudeDir := filepath.Join(home, ".claude")

	// 1. Stop and remove daemon
	plistPath := filepath.Join(home, "Library", "LaunchAgents", plistLabel+".plist")
	exec.Command("launchctl", "unload", plistPath).Run()
	os.Remove(plistPath)
	fmt.Printf("  %s✓%s Daemon removed\n", colorGreen, colorReset)

	// 2. Restore original statusline
	cfgPath := filepath.Join(claudeDir, configName)
	var cfg installConfig
	if raw, err := os.ReadFile(cfgPath); err == nil {
		json.Unmarshal(raw, &cfg)
	}

	settingsPath := filepath.Join(claudeDir, "settings.json")
	restoreStatusline(settingsPath, cfg.OriginalStatusline)

	// 3. Remove symlink
	symlink := filepath.Join(home, ".local", "bin", "claude-usage")
	os.Remove(symlink)
	fmt.Printf("  %s✓%s Symlink removed\n", colorGreen, colorReset)

	// 4. Cleanup
	os.Remove(cfgPath)
	os.Remove(filepath.Join(claudeDir, "usage-daemon.pid"))
	os.Remove(filepath.Join(claudeDir, "usage-daemon.log"))

	fmt.Printf("\n%sUninstalled.%s Data file preserved: %s\n", colorGreen, colorReset, usageFilePath())
}

func restoreStatusline(settingsPath, originalCmd string) {
	settings := make(map[string]interface{})
	if raw, err := os.ReadFile(settingsPath); err == nil {
		json.Unmarshal(raw, &settings)
	}

	if originalCmd != "" {
		settings["statusLine"] = map[string]interface{}{
			"type":    "command",
			"command": originalCmd,
		}
		fmt.Printf("  %s✓%s Statusline restored to original\n", colorGreen, colorReset)
	} else {
		delete(settings, "statusLine")
		fmt.Printf("  %s✓%s Statusline removed\n", colorGreen, colorReset)
	}

	out, _ := json.MarshalIndent(settings, "", "  ")
	os.WriteFile(settingsPath, append(out, '\n'), 0644)
}

// ── Statusline ─────────────────────────────────────────────────────

func runStatusline(args []string) {
	var wrapCmd string
	for i, a := range args {
		if a == "--wrap" && i+1 < len(args) {
			wrapCmd = args[i+1]
		}
	}

	// Read all of stdin (Claude Code sends session JSON)
	stdinData, _ := os.ReadFile("/dev/stdin")

	var prefix string
	if wrapCmd != "" {
		cmd := exec.Command("sh", "-c", wrapCmd)
		cmd.Stdin = bytes.NewReader(stdinData)
		out, err := cmd.Output()
		if err == nil {
			prefix = strings.TrimRight(string(out), "\n")
		}
	}

	usageBar := formatUsageBar()

	switch {
	case prefix != "" && usageBar != "":
		fmt.Print(prefix + " │ " + usageBar)
	case prefix != "":
		fmt.Print(prefix)
	case usageBar != "":
		fmt.Print(usageBar)
	}
}

// ── Helpers ────────────────────────────────────────────────────────

func resolveOwnBinary() string {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not resolve binary path: %s\n", err)
		os.Exit(1)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe
	}
	return resolved
}
