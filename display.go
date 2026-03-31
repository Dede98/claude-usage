package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
)

const (
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorDim    = "\033[2m"
	colorBold   = "\033[1m"
	colorReset  = "\033[0m"
)

func runDisplay() {
	data := pingAPI()
	writeUsageFile(data)
	formatDisplay(data)
}

func runJSON() {
	data := pingAPI()
	writeUsageFile(data)
	out, _ := json.MarshalIndent(data, "", "  ")
	fmt.Println(string(out))
}

func runRead() {
	data, err := readUsageFile()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sNo data at %s%s\n", colorRed, usageFilePath(), colorReset)
		os.Exit(1)
	}
	formatDisplay(data)
}

func runStatusMode() {
	data := pingAPI()
	writeUsageFile(data)
	formatStatus(data)
}

func formatDisplay(d *UsageData) {
	if d.Error != nil {
		printError(*d.Error)
		return
	}

	// Timestamp
	ago := timeAgo(d.Timestamp)

	fmt.Println()
	fmt.Printf("%sUsage Status%s\n", colorBold, colorReset)
	fmt.Printf("%sUpdated %s%s\n", colorDim, ago, colorReset)
	fmt.Println()

	// Auth info
	if d.Auth != nil {
		tier := tierDisplay(d.Auth.RateLimitTier)
		sub := d.Auth.SubscriptionType
		if sub == "" {
			sub = "unknown"
		} else {
			sub = strings.ToUpper(sub[:1]) + sub[1:]
		}
		fmt.Printf("  %sPlan%s           %s (%s)\n", colorBlue, colorReset, tier, sub)
	}

	if d.Limits == nil {
		fmt.Printf("  %sNo limit data available%s\n\n", colorDim, colorReset)
		return
	}

	// Status
	statusColor, statusLabel := statusDisplay(d.Limits.Status)
	fmt.Printf("  %sStatus%s         %s%s%s\n", colorBlue, colorReset, statusColor, statusLabel, colorReset)
	fmt.Println()

	// Meters
	type meter struct {
		key   string
		label string
		w     *WindowInfo
	}
	meters := []meter{
		{"five_hour", "Session (5h)", d.Limits.FiveHour},
		{"seven_day", "Weekly (7d)", d.Limits.SevenDay},
	}

	for _, m := range meters {
		if m.w == nil || m.w.UtilizationPct == nil {
			continue
		}
		pct := *m.w.UtilizationPct
		color := pctColor(pct)
		bar := progressBar(pct, 20)

		reset := ""
		if m.w.ResetsAt != nil {
			reset = resetCountdown(*m.w.ResetsAt)
		}

		line := fmt.Sprintf("  %s%s%s %s%3d%%%s  %s", color, bar, colorReset, color, pct, colorReset, m.label)
		if reset != "" {
			line += fmt.Sprintf("  %s%s%s", colorDim, reset, colorReset)
		}
		fmt.Println(line)
	}

	// Overage
	if d.Limits.Overage != nil && d.Limits.Overage.Status != "" {
		oc, ol := overageDisplay(d.Limits.Overage.Status)
		fmt.Println()
		if d.Limits.Overage.Utilization != nil && *d.Limits.Overage.Utilization > 0 {
			fmt.Printf("  Extra usage: %s%s%s  |  Usage: %.0f%%\n", oc, ol, colorReset, *d.Limits.Overage.Utilization*100)
		} else {
			fmt.Printf("  Extra usage: %s%s%s\n", oc, ol, colorReset)
		}
	}

	// Fallback
	if d.Limits.Fallback == "available" {
		fmt.Printf("  %sModel fallback available%s\n", colorDim, colorReset)
	}

	fmt.Println()
}

func formatStatus(d *UsageData) {
	if d.Error != nil {
		fmt.Printf("ERR: %s\n", *d.Error)
		os.Exit(1)
	}
	if d.Limits == nil {
		fmt.Println("no data")
		return
	}

	pct5 := "?"
	pct7 := "?"
	if d.Limits.FiveHour != nil && d.Limits.FiveHour.UtilizationPct != nil {
		pct5 = fmt.Sprintf("%d", *d.Limits.FiveHour.UtilizationPct)
	}
	if d.Limits.SevenDay != nil && d.Limits.SevenDay.UtilizationPct != nil {
		pct7 = fmt.Sprintf("%d", *d.Limits.SevenDay.UtilizationPct)
	}

	status := d.Limits.Status
	if status == "" {
		status = "?"
	}

	reset := ""
	if d.Limits.FiveHour != nil && d.Limits.FiveHour.ResetsAt != nil {
		diff := int(*d.Limits.FiveHour.ResetsAt - float64(time.Now().Unix()))
		if diff > 0 {
			h := diff / 3600
			m := (diff % 3600) / 60
			if h > 0 {
				reset = fmt.Sprintf("  resets %dh%dm", h, m)
			} else {
				reset = fmt.Sprintf("  resets %dm", m)
			}
		}
	}

	fmt.Printf("session %s%%  weekly %s%%  [%s]%s\n", pct5, pct7, status, reset)
}

// Helpers

func printError(err string) {
	if strings.Contains(err, "keychain") || strings.Contains(err, "auth") {
		fmt.Printf("%sAuth failed.%s Make sure Claude Code is installed and you are logged in.\n", colorRed, colorReset)
		fmt.Printf("Run: %sclaude /login%s\n", colorBold, colorReset)
	} else if strings.Contains(err, "expired") {
		fmt.Printf("%sOAuth token expired.%s Open Claude Code to refresh your session.\n", colorRed, colorReset)
	} else if strings.Contains(err, "HTTP") {
		fmt.Printf("%sAPI error: %s%s\n", colorRed, err, colorReset)
	} else {
		fmt.Printf("%sError: %s%s\n", colorRed, err, colorReset)
	}
}

func progressBar(pct, width int) string {
	filled := pct * width / 100
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func pctColor(pct int) string {
	if pct > 80 {
		return colorRed
	}
	if pct > 50 {
		return colorYellow
	}
	return colorGreen
}

func resetCountdown(resetsAt float64) string {
	diff := int(resetsAt - float64(time.Now().Unix()))
	if diff <= 0 {
		return ""
	}
	days := diff / 86400
	hrs := (diff % 86400) / 3600
	mins := (diff % 3600) / 60
	if days > 0 {
		return fmt.Sprintf("resets in %dd %dh", days, hrs)
	}
	if hrs > 0 {
		return fmt.Sprintf("resets in %dh %dm", hrs, mins)
	}
	return fmt.Sprintf("resets in %dm", mins)
}

func timeAgo(tsMs int64) string {
	diff := int(time.Now().Unix() - tsMs/1000)
	if diff < 10 {
		return "just now"
	}
	if diff < 60 {
		return fmt.Sprintf("%ds ago", diff)
	}
	if diff < 3600 {
		return fmt.Sprintf("%dm ago", diff/60)
	}
	return fmt.Sprintf("%dh %dm ago", diff/3600, (diff%3600)/60)
}

func tierDisplay(tier string) string {
	switch {
	case strings.Contains(tier, "max_5x"):
		return "Max 5x"
	case strings.Contains(tier, "max"):
		return "Max"
	case strings.Contains(tier, "pro"):
		return "Pro"
	case strings.Contains(tier, "default_raven"):
		return "Team (API)"
	case strings.Contains(tier, "free"):
		return "Free"
	default:
		return tier
	}
}

func statusDisplay(s string) (string, string) {
	switch s {
	case "allowed":
		return colorGreen, "OK"
	case "allowed_warning":
		return colorYellow, "Warning"
	case "rejected":
		return colorRed, "Rate limited"
	default:
		return colorDim, s
	}
}

func overageDisplay(s string) (string, string) {
	switch s {
	case "allowed":
		return colorGreen, "available"
	case "allowed_warning":
		return colorYellow, "warning"
	case "rejected":
		return colorRed, "exhausted"
	default:
		return colorDim, s
	}
}

// formatUsageBar returns a compact colored usage bar for statusline integration.
func formatUsageBar() string {
	data, err := readUsageFile()
	if err != nil {
		return ""
	}

	age := float64(time.Now().UnixMilli()-data.Timestamp) / 1000
	if age > 300 || data.Error != nil {
		return ""
	}

	if data.Limits == nil || data.Limits.FiveHour == nil || data.Limits.FiveHour.UtilizationPct == nil {
		return ""
	}

	pct := *data.Limits.FiveHour.UtilizationPct
	color := pctColor(pct)
	bar := progressBar(pct, 10)

	reset := ""
	if data.Limits.FiveHour.ResetsAt != nil {
		diff := *data.Limits.FiveHour.ResetsAt - float64(time.Now().Unix())
		if diff > 0 {
			h := int(math.Floor(diff / 3600))
			m := int(math.Floor(math.Mod(diff, 3600) / 60))
			if h > 0 {
				reset = fmt.Sprintf("  resets %dh %dm", h, m)
			} else {
				reset = fmt.Sprintf("  resets %dm", m)
			}
		}
	}

	result := fmt.Sprintf("%s%s %d%%%s session%s%s%s", color, bar, pct, colorReset, colorDim, reset, colorReset)

	// Weekly when >= 75%
	if data.Limits.SevenDay != nil && data.Limits.SevenDay.UtilizationPct != nil {
		weekly := *data.Limits.SevenDay.UtilizationPct
		if weekly >= 75 {
			wc := colorYellow
			if weekly >= 80 {
				wc = colorRed
			}
			result += fmt.Sprintf(" %s7d:%d%%%s", wc, weekly, colorReset)
		}
	}

	return result
}
