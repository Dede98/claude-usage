package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type guardConfig struct {
	threshold  int
	warn       int
	poll       int
	pid        int
	pidFile    string
	autoResume bool
	dryRun     bool
	quiet      bool
	stateFile  string
}

type guardState struct {
	Paused      bool   `json:"paused"`
	PausedAt    string `json:"paused_at,omitempty"`
	ResumeAt    string `json:"resume_at,omitempty"`
	LastWarning string `json:"last_warning,omitempty"`
	WarningSent bool   `json:"warning_sent"`
	PauseCount  int    `json:"pause_count"`
}

func parseGuardArgs(args []string) guardConfig {
	cfg := guardConfig{
		threshold: 80,
		warn:      -1, // will default to threshold - 10
		poll:      30,
		stateFile: defaultStateFile(),
	}

	// Check for "status" subcommand
	if len(args) > 0 && args[0] == "status" {
		runGuardStatus(cfg)
		os.Exit(0)
	}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--threshold":
			if i+1 < len(args) {
				i++
				cfg.threshold, _ = strconv.Atoi(args[i])
			}
		case "--warn":
			if i+1 < len(args) {
				i++
				cfg.warn, _ = strconv.Atoi(args[i])
			}
		case "--poll":
			if i+1 < len(args) {
				i++
				cfg.poll, _ = strconv.Atoi(args[i])
			}
		case "--pid":
			if i+1 < len(args) {
				i++
				cfg.pid, _ = strconv.Atoi(args[i])
			}
		case "--pid-file":
			if i+1 < len(args) {
				i++
				cfg.pidFile = args[i]
			}
		case "--auto-resume":
			cfg.autoResume = true
		case "--dry-run":
			cfg.dryRun = true
		case "--quiet":
			cfg.quiet = true
		case "--state-file":
			if i+1 < len(args) {
				i++
				cfg.stateFile = args[i]
			}
		}
	}

	if cfg.warn < 0 {
		cfg.warn = cfg.threshold - 10
	}

	return cfg
}

func defaultStateFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "usage-guard-state.json")
}

func runGuard(args []string) {
	cfg := parseGuardArgs(args)

	if cfg.pid == 0 && cfg.pidFile == "" {
		fmt.Fprintf(os.Stderr, "%sError:%s specify --pid <PID> or --pid-file <path>\n", colorRed, colorReset)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage: claude-usage guard --pid 12345")
		fmt.Fprintln(os.Stderr, "       claude-usage guard --pid-file ~/.gsd/auto.lock")
		os.Exit(1)
	}

	state := loadGuardState(cfg.stateFile)

	if !cfg.quiet {
		fmt.Printf("%s%sUsage Guard%s\n", colorBold, colorBlue, colorReset)
		fmt.Printf("  Threshold: %d%%  Warn: %d%%  Poll: %ds\n", cfg.threshold, cfg.warn, cfg.poll)
		if cfg.dryRun {
			fmt.Printf("  %s[DRY RUN]%s Actions will be logged but not executed\n", colorYellow, colorReset)
		}
		if cfg.autoResume {
			fmt.Printf("  Auto-resume: enabled\n")
		}
		fmt.Println()
	}

	for {
		pid := resolvePID(cfg)
		if pid == 0 {
			if !cfg.quiet {
				fmt.Printf("%s%s  No process to watch — waiting...%s\n", colorDim, timestamp(), colorReset)
			}
			time.Sleep(time.Duration(cfg.poll) * time.Second)
			continue
		}

		// Check if process is still alive
		if !processAlive(pid) {
			if !cfg.quiet {
				fmt.Printf("%s%s  PID %d is gone%s\n", colorDim, timestamp(), pid, colorReset)
			}
			if state.Paused {
				state.Paused = false
				saveGuardState(cfg.stateFile, state)
			}
			time.Sleep(time.Duration(cfg.poll) * time.Second)
			continue
		}

		data, err := readUsageFile()
		if err != nil {
			if !cfg.quiet {
				fmt.Printf("%s  %sNo usage data%s\n", timestamp(), colorDim, colorReset)
			}
			time.Sleep(time.Duration(cfg.poll) * time.Second)
			continue
		}

		// Check staleness
		age := float64(time.Now().UnixMilli()-data.Timestamp) / 1000
		if age > 300 {
			if !cfg.quiet {
				fmt.Printf("%s  %sUsage data stale (%.0fs old)%s\n", timestamp(), colorDim, age, colorReset)
			}
			time.Sleep(time.Duration(cfg.poll) * time.Second)
			continue
		}

		if data.Error != nil || data.Limits == nil {
			time.Sleep(time.Duration(cfg.poll) * time.Second)
			continue
		}

		// Find binding constraint (highest utilization)
		pct, resetAt, windowName := bindingConstraint(data)

		if state.Paused {
			// Check if we should resume
			if pct < cfg.warn {
				fmt.Printf("%s  %s↓ Usage dropped to %d%% (%s) — below warn threshold%s\n",
					timestamp(), colorGreen, pct, windowName, colorReset)

				if cfg.autoResume {
					fmt.Printf("%s  %s▶ Auto-resume enabled but process must be restarted externally%s\n",
						timestamp(), colorGreen, colorReset)
				}

				state.Paused = false
				state.WarningSent = false
				saveGuardState(cfg.stateFile, state)
			} else if !cfg.quiet {
				reset := ""
				if resetAt > 0 {
					reset = " " + resetCountdown(resetAt)
				}
				fmt.Printf("%s  %s⏸ Paused — %d%% %s%s%s\n",
					timestamp(), colorYellow, pct, windowName, reset, colorReset)
			}
		} else {
			// Normal monitoring
			if pct >= cfg.threshold {
				// PAUSE
				fmt.Printf("%s  %s⚠ %d%% %s — threshold %d%% exceeded!%s\n",
					timestamp(), colorRed, pct, windowName, cfg.threshold, colorReset)

				if cfg.dryRun {
					fmt.Printf("%s  %s[DRY RUN] Would send SIGTERM to PID %d%s\n",
						timestamp(), colorYellow, pid, colorReset)
				} else {
					fmt.Printf("%s  %s■ Sending SIGTERM to PID %d%s\n",
						timestamp(), colorRed, pid, colorReset)
					if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
						fmt.Printf("%s  %sFailed to signal PID %d: %s%s\n",
							timestamp(), colorRed, pid, err, colorReset)
					}
				}

				state.Paused = true
				state.PausedAt = time.Now().UTC().Format(time.RFC3339)
				state.PauseCount++
				if resetAt > 0 {
					state.ResumeAt = time.Unix(int64(resetAt), 0).UTC().Format(time.RFC3339)
				}
				saveGuardState(cfg.stateFile, state)

			} else if pct >= cfg.warn && !state.WarningSent {
				// WARNING
				fmt.Printf("%s  %s⚡ Warning: %d%% %s — approaching threshold %d%%%s\n",
					timestamp(), colorYellow, pct, windowName, cfg.threshold, colorReset)
				state.WarningSent = true
				state.LastWarning = time.Now().UTC().Format(time.RFC3339)
				saveGuardState(cfg.stateFile, state)

			} else if !cfg.quiet {
				color := pctColor(pct)
				bar := progressBar(pct, 10)
				fmt.Printf("%s  %s%s %d%%%s %s\n",
					timestamp(), color, bar, pct, colorReset, windowName)
			}
		}

		time.Sleep(time.Duration(cfg.poll) * time.Second)
	}
}

// ── Guard status (one-shot) ────────────────────────────────────────

func runGuardStatus(cfg guardConfig) {
	data, err := readUsageFile()
	if err != nil {
		fmt.Printf("%sNo usage data available.%s\n", colorRed, colorReset)
		os.Exit(1)
	}

	if data.Error != nil {
		printError(*data.Error)
		os.Exit(1)
	}

	if data.Limits == nil {
		fmt.Printf("%sNo limit data.%s\n", colorDim, colorReset)
		os.Exit(0)
	}

	pct, resetAt, windowName := bindingConstraint(data)
	color := pctColor(pct)
	bar := progressBar(pct, 20)

	fmt.Println()
	fmt.Printf("  %s%s%s %s%d%%%s  %s", color, bar, colorReset, color, pct, colorReset, windowName)
	if resetAt > 0 {
		fmt.Printf("  %s%s%s", colorDim, resetCountdown(resetAt), colorReset)
	}
	fmt.Println()

	// Show state if exists
	state := loadGuardState(cfg.stateFile)
	if state.PauseCount > 0 {
		fmt.Println()
		status := "monitoring"
		if state.Paused {
			status = "paused"
		}
		fmt.Printf("  Guard: %s  |  Pauses: %d\n", status, state.PauseCount)
	}
	fmt.Println()
}

// ── Helpers ────────────────────────────────────────────────────────

func bindingConstraint(data *UsageData) (int, float64, string) {
	var pct int
	var resetAt float64
	var name string

	if data.Limits.FiveHour != nil && data.Limits.FiveHour.UtilizationPct != nil {
		pct = *data.Limits.FiveHour.UtilizationPct
		name = "session (5h)"
		if data.Limits.FiveHour.ResetsAt != nil {
			resetAt = *data.Limits.FiveHour.ResetsAt
		}
	}

	if data.Limits.SevenDay != nil && data.Limits.SevenDay.UtilizationPct != nil {
		weeklyPct := *data.Limits.SevenDay.UtilizationPct
		if weeklyPct > pct {
			pct = weeklyPct
			name = "weekly (7d)"
			if data.Limits.SevenDay.ResetsAt != nil {
				resetAt = *data.Limits.SevenDay.ResetsAt
			}
		}
	}

	return pct, resetAt, name
}

func resolvePID(cfg guardConfig) int {
	if cfg.pid > 0 {
		return cfg.pid
	}
	if cfg.pidFile != "" {
		raw, err := os.ReadFile(cfg.pidFile)
		if err != nil {
			return 0
		}
		content := strings.TrimSpace(string(raw))

		// Try plain number first
		if pid, err := strconv.Atoi(content); err == nil {
			return pid
		}

		// Try JSON with "pid" field (e.g. GSD auto.lock format)
		var lockFile struct {
			PID int `json:"pid"`
		}
		if err := json.Unmarshal(raw, &lockFile); err == nil && lockFile.PID > 0 {
			return lockFile.PID
		}

		return 0
	}
	return 0
}

func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func timestamp() string {
	return fmt.Sprintf("%s[%s]%s", colorDim, time.Now().Format("15:04:05"), colorReset)
}

func loadGuardState(path string) guardState {
	var state guardState
	if raw, err := os.ReadFile(path); err == nil {
		json.Unmarshal(raw, &state)
	}
	return state
}

func saveGuardState(path string, state guardState) {
	os.MkdirAll(filepath.Dir(path), 0755)
	data, _ := json.MarshalIndent(state, "", "  ")
	tmp := path + ".tmp"
	os.WriteFile(tmp, data, 0644)
	os.Rename(tmp, path)
}
