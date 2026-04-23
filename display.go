package main

import (
	"encoding/json"
	"fmt"
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
	data := fetchUsageSnapshot()
	writeUsageFile(data)
	formatDisplay(data)
}

func runJSON() {
	data := fetchUsageSnapshot()
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
	data := fetchUsageSnapshot()
	writeUsageFile(data)
	formatStatus(data)
}

func formatDisplay(d *UsageSnapshot) {
	fmt.Println()
	fmt.Printf("%sUsage Status%s\n", colorBold, colorReset)
	fmt.Printf("%sUpdated %s%s\n", colorDim, timeAgo(d.Timestamp), colorReset)
	fmt.Println()

	order := availableProviders(d)
	if len(order) == 0 {
		fmt.Printf("  %sNo supported provider data available.%s\n\n", colorDim, colorReset)
		return
	}

	for idx, name := range order {
		if idx > 0 {
			fmt.Println()
		}
		formatProviderDisplay(name, d.Providers[name])
	}
	fmt.Println()
}

func formatProviderDisplay(name string, provider *ProviderSnapshot) {
	title := providerDisplayName(name)
	fmt.Printf("  %s%s%s\n", colorBold, title, colorReset)
	fmt.Printf("  %sUpdated %s%s\n", colorDim, timeAgo(provider.Timestamp), colorReset)

	if provider.Error != nil {
		fmt.Printf("  %sError%s         %s\n", colorBlue, colorReset, *provider.Error)
		return
	}

	if provider.Auth != nil {
		if plan := providerPlanLabel(name, provider.Auth); plan != "" {
			fmt.Printf("  %sPlan%s          %s\n", colorBlue, colorReset, plan)
		}
		if provider.Auth.Email != "" {
			fmt.Printf("  %sAccount%s       %s\n", colorBlue, colorReset, provider.Auth.Email)
		}
	}

	if provider.Limits == nil {
		fmt.Printf("  %sNo limit data available%s\n", colorDim, colorReset)
		return
	}

	if status := providerStatusLabel(name, provider.Limits); status != "" {
		fmt.Printf("  %sStatus%s        %s\n", colorBlue, colorReset, status)
	}
	if provider.Limits.Credits != nil {
		fmt.Printf("  %sCredits%s       %s\n", colorBlue, colorReset, formatCredits(provider.Limits.Credits))
	}
	fmt.Println()

	windows := []*WindowInfo{provider.Limits.Primary, provider.Limits.Secondary}
	for i, window := range windows {
		if window == nil || window.UtilizationPct == nil {
			continue
		}
		pct := providerDisplayPct(name, *window.UtilizationPct)
		color := pctColor(pct)
		bar := progressBar(pct, 20)
		label := providerWindowLabel(name, window, i)
		reset := ""
		if window.ResetsAt != nil {
			reset = resetCountdown(*window.ResetsAt)
		}

		line := fmt.Sprintf("  %s%s%s %s%3d%%%s  %s", color, bar, colorReset, color, pct, colorReset, label)
		if reset != "" {
			line += fmt.Sprintf("  %s%s%s", colorDim, reset, colorReset)
		}
		fmt.Println(line)
	}

	if name == "claude" {
		if provider.Limits.Overage != nil && provider.Limits.Overage.Status != "" {
			oc, ol := overageDisplay(provider.Limits.Overage.Status)
			fmt.Println()
			if provider.Limits.Overage.Utilization != nil && *provider.Limits.Overage.Utilization > 0 {
				fmt.Printf("  Extra usage: %s%s%s  |  Usage: %.0f%%\n", oc, ol, colorReset, *provider.Limits.Overage.Utilization*100)
			} else {
				fmt.Printf("  Extra usage: %s%s%s\n", oc, ol, colorReset)
			}
		}
		if provider.Limits.Fallback == "available" {
			fmt.Printf("  %sModel fallback available%s\n", colorDim, colorReset)
		}
	}
}

func formatStatus(d *UsageSnapshot) {
	order := availableProviders(d)
	if len(order) == 0 {
		fmt.Println("no providers")
		return
	}

	for _, name := range order {
		provider := d.Providers[name]
		if provider.Error != nil {
			fmt.Printf("%s ERR: %s\n", name, *provider.Error)
			continue
		}
		fmt.Println(formatProviderStatus(name, provider))
	}
}

func formatProviderStatus(name string, provider *ProviderSnapshot) string {
	var primary, secondary string
	if provider.Limits != nil && provider.Limits.Primary != nil && provider.Limits.Primary.UtilizationPct != nil {
		primary = fmt.Sprintf("%d", providerDisplayPct(name, *provider.Limits.Primary.UtilizationPct))
	} else {
		primary = "?"
	}
	if provider.Limits != nil && provider.Limits.Secondary != nil && provider.Limits.Secondary.UtilizationPct != nil {
		secondary = fmt.Sprintf("%d", providerDisplayPct(name, *provider.Limits.Secondary.UtilizationPct))
	} else {
		secondary = "?"
	}

	reset := ""
	if provider.Limits != nil && provider.Limits.Primary != nil && provider.Limits.Primary.ResetsAt != nil {
		diff := int(*provider.Limits.Primary.ResetsAt - time.Now().Unix())
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

	switch name {
	case "codex":
		plan := "?"
		if provider.Auth != nil && provider.Auth.PlanType != "" {
			plan = provider.Auth.PlanType
		}
		return fmt.Sprintf("codex  5h left %s%%  weekly left %s%%  [%s]%s", primary, secondary, plan, reset)
	default:
		status := "?"
		if provider.Limits != nil && provider.Limits.Status != "" {
			status = provider.Limits.Status
		}
		return fmt.Sprintf("claude used %s%%  weekly used %s%%  [%s]%s", primary, secondary, status, reset)
	}
}

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

func resetCountdown(resetsAt int64) string {
	diff := int(resetsAt - time.Now().Unix())
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
	if tsMs == 0 {
		return "unknown"
	}
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

func formatUsageBar() string {
	data, err := readUsageFile()
	if err != nil {
		return ""
	}

	var parts []string
	for _, name := range availableProviders(data) {
		provider := dProvider(data, name)
		if provider == nil || provider.Error != nil || provider.Limits == nil || provider.Limits.Primary == nil || provider.Limits.Primary.UtilizationPct == nil {
			continue
		}
		age := float64(time.Now().UnixMilli()-provider.Timestamp) / 1000
		if age > 300 {
			continue
		}
		parts = append(parts, formatProviderUsageBar(name, provider))
	}

	return strings.Join(parts, " │ ")
}

func formatProviderUsageBar(name string, provider *ProviderSnapshot) string {
	pct := providerDisplayPct(name, *provider.Limits.Primary.UtilizationPct)
	color := pctColor(pct)
	bar := progressBar(pct, 10)
	label := providerBarLabel(name)

	reset := ""
	if provider.Limits.Primary.ResetsAt != nil {
		diff := *provider.Limits.Primary.ResetsAt - time.Now().Unix()
		if diff > 0 {
			h := int(diff / 3600)
			m := int((diff % 3600) / 60)
			if h > 0 {
				reset = fmt.Sprintf("  resets %dh %dm", h, m)
			} else {
				reset = fmt.Sprintf("  resets %dm", m)
			}
		}
	}

	result := fmt.Sprintf("%s%s %d%%%s %s%s%s", color, bar, pct, colorReset, label, colorDim, reset)
	if reset != "" {
		result += colorReset
	}
	return result
}

func providerDisplayName(name string) string {
	switch name {
	case "codex":
		return "Codex"
	default:
		return "Claude"
	}
}

func providerPlanLabel(name string, auth *ProviderAuth) string {
	switch name {
	case "codex":
		if auth.PlanType == "" {
			return ""
		}
		return strings.ToUpper(auth.PlanType[:1]) + auth.PlanType[1:]
	default:
		tier := tierDisplay(auth.RateLimitTier)
		sub := auth.SubscriptionType
		if sub == "" {
			return tier
		}
		return fmt.Sprintf("%s (%s)", tier, strings.ToUpper(sub[:1])+sub[1:])
	}
}

func providerStatusLabel(name string, limits *ProviderLimits) string {
	switch name {
	case "codex":
		if limits.RateLimitReachedType != "" {
			return limits.RateLimitReachedType
		}
		if limits.Status != "" {
			return limits.Status
		}
		return ""
	default:
		if limits.Status == "" {
			return ""
		}
		c, label := statusDisplay(limits.Status)
		return c + label + colorReset
	}
}

func providerWindowLabel(name string, window *WindowInfo, index int) string {
	if name == "codex" {
		if window.WindowMinutes != nil {
			switch *window.WindowMinutes {
			case 300:
				return "5h left"
			case 10080:
				return "Weekly left"
			default:
				return fmt.Sprintf("%dm left", *window.WindowMinutes)
			}
		}
	}
	if index == 0 {
		return "Session used (5h)"
	}
	return "Weekly used (7d)"
}

func providerBarLabel(name string) string {
	if name == "codex" {
		return "codex left"
	}
	return "claude used"
}

func providerDisplayPct(name string, usedPct int) int {
	if name != "codex" {
		return usedPct
	}
	remaining := 100 - usedPct
	if remaining < 0 {
		return 0
	}
	if remaining > 100 {
		return 100
	}
	return remaining
}

func formatCredits(c *CreditsInfo) string {
	if c.Unlimited {
		return "unlimited"
	}
	if !c.HasCredits {
		return "depleted"
	}
	if c.Balance != nil && *c.Balance != "" {
		return "$" + *c.Balance
	}
	return "available"
}

func availableProviders(d *UsageSnapshot) []string {
	order := []string{"claude", "codex"}
	var providers []string
	for _, name := range order {
		if d.Providers[name] != nil {
			providers = append(providers, name)
		}
	}
	return providers
}

func dProvider(d *UsageSnapshot, name string) *ProviderSnapshot {
	if d == nil || d.Providers == nil {
		return nil
	}
	return d.Providers[name]
}
