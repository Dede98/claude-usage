//go:build darwin

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const clearLine = "\033[2K\r"
const hideCursor = "\033[?25l"
const showCursor = "\033[?25h"

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

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--threshold":
			if i+1 < len(args) {
				i++
				v, err := strconv.Atoi(args[i])
				if err != nil || v < 1 || v > 100 {
					fmt.Fprintf(os.Stderr, "Error: --threshold must be 1-100\n")
					os.Exit(1)
				}
				cfg.threshold = v
			}
		case "--warn":
			if i+1 < len(args) {
				i++
				v, err := strconv.Atoi(args[i])
				if err != nil || v < 0 || v > 100 {
					fmt.Fprintf(os.Stderr, "Error: --warn must be 0-100\n")
					os.Exit(1)
				}
				cfg.warn = v
			}
		case "--poll":
			if i+1 < len(args) {
				i++
				v, err := strconv.Atoi(args[i])
				if err != nil || v < 1 {
					fmt.Fprintf(os.Stderr, "Error: --poll must be >= 1\n")
					os.Exit(1)
				}
				cfg.poll = v
			}
		case "--pid":
			if i+1 < len(args) {
				i++
				v, err := strconv.Atoi(args[i])
				if err != nil || v < 1 {
					fmt.Fprintf(os.Stderr, "Error: --pid must be a valid process ID\n")
					os.Exit(1)
				}
				cfg.pid = v
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
		if cfg.warn < 0 {
			cfg.warn = 0
		}
	}

	return cfg
}

func defaultStateFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "usage-guard-state.json")
}

func runGuard(args []string) {
	// Handle "status" subcommand before parsing guard-specific args
	if len(args) > 0 && args[0] == "status" {
		cfg := parseGuardArgs(args[1:])
		runGuardStatus(cfg)
		return
	}

	cfg := parseGuardArgs(args)

	if cfg.pid == 0 && cfg.pidFile == "" {
		fmt.Fprintf(os.Stderr, "%sError:%s specify --pid <PID> or --pid-file <path>\n", colorRed, colorReset)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage: claude-usage guard --pid 12345")
		fmt.Fprintln(os.Stderr, "       claude-usage guard --pid-file ~/.gsd/auto.lock")
		os.Exit(1)
	}

	// Put stdin into raw mode to suppress arrow key echo, hide cursor
	var oldTermState *termios
	if state, err := setRawInput(); err == nil {
		oldTermState = state
		fmt.Print(hideCursor)

		// Drain stdin in background to prevent buffer fill
		go func() {
			buf := make([]byte, 64)
			for {
				os.Stdin.Read(buf)
			}
		}()
	}

	cleanup := func() {
		fmt.Print(showCursor)
		restoreInput(oldTermState)
		fmt.Println()
	}

	// Restore terminal on signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		cleanup()
		os.Exit(0)
	}()

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

	ticker := time.NewTicker(time.Duration(cfg.poll) * time.Second)
	defer ticker.Stop()

	// Run once immediately, then on tick
	guardTick(&cfg, &state)
	for range ticker.C {
		guardTick(&cfg, &state)
	}
}

func guardTick(cfg *guardConfig, state *guardState) {
	pid := resolvePID(*cfg)
	if pid == 0 {
		if !cfg.quiet {
			fmt.Printf("%s%s  waiting for process...%s", clearLine, colorDim, colorReset)
		}
		return
	}

	if !processAlive(pid) {
		if !cfg.quiet {
			fmt.Printf("%s%s  PID %d is gone%s", clearLine, colorDim, pid, colorReset)
		}
		if state.Paused {
			state.Paused = false
			saveGuardState(cfg.stateFile, *state)
		}
		return
	}

	data, err := readUsageFile()
	if err != nil {
		if !cfg.quiet {
			fmt.Printf("%s%s  no usage data%s", clearLine, colorDim, colorReset)
		}
		return
	}

	providerName, provider := guardProvider(data)
	if provider == nil {
		if !cfg.quiet {
			fmt.Printf("%s%s  no provider data%s", clearLine, colorDim, colorReset)
		}
		return
	}

	age := float64(time.Now().UnixMilli()-provider.Timestamp) / 1000
	if age > 300 {
		if !cfg.quiet {
			fmt.Printf("%s%s  stale (%.0fs old)%s", clearLine, colorDim, age, colorReset)
		}
		return
	}

	if provider.Error != nil || provider.Limits == nil {
		return
	}

	pct, resetAt, windowName := bindingConstraint(provider)
	summary := usageSummary(providerName, provider)

	if state.Paused {
		if pct < cfg.warn {
			fmt.Printf("%s\n%s  %s↓ Usage dropped to %d%% (%s) — below warn threshold%s\n",
				clearLine, timestamp(), colorGreen, pct, windowName, colorReset)

			if cfg.autoResume {
				fmt.Printf("%s  %s▶ Auto-resume: process must be restarted externally%s\n",
					timestamp(), colorGreen, colorReset)
			}

			state.Paused = false
			state.WarningSent = false
			saveGuardState(cfg.stateFile, *state)
		} else if !cfg.quiet {
			reset := ""
			if resetAt > 0 {
				reset = "  " + resetCountdown(resetAt)
			}
			fmt.Printf("%s%s  ⏸ %s%s", clearLine, colorYellow, summary, colorReset)
			fmt.Printf("  %s%s%s", colorDim, reset, colorReset)
		}
	} else {
		if pct >= cfg.threshold {
			fmt.Printf("%s\n%s  %s⚠ %d%% %s — threshold %d%% exceeded!%s\n",
				clearLine, timestamp(), colorRed, pct, windowName, cfg.threshold, colorReset)

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
			saveGuardState(cfg.stateFile, *state)

		} else if pct >= cfg.warn && !state.WarningSent {
			fmt.Printf("%s\n%s  %s⚡ Warning: %d%% %s — approaching threshold %d%%%s\n",
				clearLine, timestamp(), colorYellow, pct, windowName, cfg.threshold, colorReset)
			state.WarningSent = true
			state.LastWarning = time.Now().UTC().Format(time.RFC3339)
			saveGuardState(cfg.stateFile, *state)

		} else if !cfg.quiet {
			fmt.Printf("%s%s  %s", clearLine, timestamp(), summary)
		}
	}
}

// usageSummary returns a compact line showing both session and weekly usage.
func usageSummary(name string, provider *ProviderSnapshot) string {
	var parts []string

	if provider.Limits.Primary != nil && provider.Limits.Primary.UtilizationPct != nil {
		pct := *provider.Limits.Primary.UtilizationPct
		c := pctColor(pct)
		bar := progressBar(pct, 10)
		s := fmt.Sprintf("%s%s %d%%%s %s", c, bar, pct, colorReset, providerWindowLabel(name, provider.Limits.Primary, 0))
		if provider.Limits.Primary.ResetsAt != nil {
			r := resetCountdown(*provider.Limits.Primary.ResetsAt)
			if r != "" {
				s += fmt.Sprintf(" %s%s%s", colorDim, r, colorReset)
			}
		}
		parts = append(parts, s)
	}

	if provider.Limits.Secondary != nil && provider.Limits.Secondary.UtilizationPct != nil {
		pct := *provider.Limits.Secondary.UtilizationPct
		c := pctColor(pct)
		bar := progressBar(pct, 10)
		s := fmt.Sprintf("%s%s %d%%%s %s", c, bar, pct, colorReset, providerWindowLabel(name, provider.Limits.Secondary, 1))
		if provider.Limits.Secondary.ResetsAt != nil {
			r := resetCountdown(*provider.Limits.Secondary.ResetsAt)
			if r != "" {
				s += fmt.Sprintf(" %s%s%s", colorDim, r, colorReset)
			}
		}
		parts = append(parts, s)
	}

	if len(parts) == 0 {
		return fmt.Sprintf("%sno data%s", colorDim, colorReset)
	}

	return strings.Join(parts, "  │  ")
}

// ── Guard status (one-shot) ────────────────────────────────────────

func runGuardStatus(cfg guardConfig) {
	data, err := readUsageFile()
	if err != nil {
		fmt.Printf("%sNo usage data available.%s\n", colorRed, colorReset)
		os.Exit(1)
	}

	name, provider := guardProvider(data)
	if provider == nil {
		fmt.Printf("%sNo limit data.%s\n", colorDim, colorReset)
		os.Exit(0)
	}
	if provider.Error != nil {
		printError(*provider.Error)
		os.Exit(1)
	}
	if provider.Limits == nil {
		fmt.Printf("%sNo limit data.%s\n", colorDim, colorReset)
		os.Exit(0)
	}

	fmt.Println()
	fmt.Printf("  %s\n", usageSummary(name, provider))

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

func bindingConstraint(provider *ProviderSnapshot) (int, int64, string) {
	var pct int
	var resetAt int64
	var name string

	if provider.Limits.Primary != nil && provider.Limits.Primary.UtilizationPct != nil {
		pct = *provider.Limits.Primary.UtilizationPct
		name = providerWindowLabel("guard", provider.Limits.Primary, 0)
		if provider.Limits.Primary.ResetsAt != nil {
			resetAt = *provider.Limits.Primary.ResetsAt
		}
	}

	if provider.Limits.Secondary != nil && provider.Limits.Secondary.UtilizationPct != nil {
		weeklyPct := *provider.Limits.Secondary.UtilizationPct
		if weeklyPct > pct {
			pct = weeklyPct
			name = providerWindowLabel("guard", provider.Limits.Secondary, 1)
			if provider.Limits.Secondary.ResetsAt != nil {
				resetAt = *provider.Limits.Secondary.ResetsAt
			}
		}
	}

	return pct, resetAt, name
}

func guardProvider(data *UsageSnapshot) (string, *ProviderSnapshot) {
	if data == nil {
		return "", nil
	}
	if provider := dProvider(data, "claude"); provider != nil {
		return "claude", provider
	}
	if provider := dProvider(data, "codex"); provider != nil {
		return "codex", provider
	}
	return "", nil
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
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "\n%sWarning: cannot create state dir: %s%s\n", colorYellow, err, colorReset)
		return
	}
	out, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "\n%sWarning: cannot write state: %s%s\n", colorYellow, err, colorReset)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		fmt.Fprintf(os.Stderr, "\n%sWarning: cannot save state: %s%s\n", colorYellow, err, colorReset)
	}
}

// ── Terminal raw mode (macOS-specific, suppress input echo) ────────

type termios struct {
	Iflag  uint64
	Oflag  uint64
	Cflag  uint64
	Lflag  uint64
	Cc     [20]byte
	Ispeed uint64
	Ospeed uint64
}

func tcget(fd uintptr) (*termios, error) {
	var t termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCGETA), uintptr(unsafe.Pointer(&t)))
	if errno != 0 {
		return nil, errno
	}
	return &t, nil
}

func tcset(fd uintptr, t *termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TIOCSETA), uintptr(unsafe.Pointer(t)))
	if errno != 0 {
		return errno
	}
	return nil
}

func setRawInput() (*termios, error) {
	old, err := tcget(os.Stdin.Fd())
	if err != nil {
		return nil, err
	}
	raw := *old
	raw.Lflag &^= syscall.ECHO | syscall.ICANON
	if err := tcset(os.Stdin.Fd(), &raw); err != nil {
		return nil, err
	}
	return old, nil
}

func restoreInput(t *termios) {
	if t != nil {
		tcset(os.Stdin.Fd(), t)
	}
}
