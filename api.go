package main

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	apiURL    = "https://api.anthropic.com/v1/messages"
	pingModel = "claude-haiku-4-5-20251001"
)

func fetchClaudeProvider(now int64) (*ProviderSnapshot, bool) {
	creds, err := getCredentials()
	if err != nil {
		if isClaudeUnavailable(err) {
			return nil, false
		}
		errStr := fmt.Sprintf("auth_failed: %s. Run 'claude /login' first.", err)
		return &ProviderSnapshot{Timestamp: now, Source: "api", Error: &errStr}, true
	}

	auth := &ProviderAuth{
		AccountType:      "oauth",
		SubscriptionType: creds.SubscriptionType,
		RateLimitTier:    creds.RateLimitTier,
		TokenExpiresAt:   creds.ExpiresAt,
	}

	if creds.isExpired() {
		errStr := "token_expired. Open Claude Code to refresh your session."
		return &ProviderSnapshot{Timestamp: now, Source: "api", Auth: auth, Error: &errStr}, true
	}

	body := fmt.Sprintf(`{"model":"%s","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`, pingModel)
	req, _ := http.NewRequest("POST", apiURL, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+creds.AccessToken)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		errStr := fmt.Sprintf("request_failed: %s", err)
		return &ProviderSnapshot{Timestamp: now, Source: "api", Auth: auth, Error: &errStr}, true
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		errStr := fmt.Sprintf("HTTP %d", resp.StatusCode)
		return &ProviderSnapshot{Timestamp: now, Source: "api", Auth: auth, Error: &errStr}, true
	}

	return parseClaudeHeaders(resp.Header, auth, now), true
}

func isClaudeUnavailable(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "keychain") || strings.Contains(msg, "no_token")
}

func parseClaudeHeaders(h http.Header, auth *ProviderAuth, ts int64) *ProviderSnapshot {
	get := func(name string) string {
		return h.Get("anthropic-ratelimit-unified-" + name)
	}

	limits := &ProviderLimits{
		Primary:             parseClaudeWindow(get("5h-utilization"), get("5h-reset"), 300),
		Secondary:           parseClaudeWindow(get("7d-utilization"), get("7d-reset"), 10080),
		Overage:             parseOverage(get("overage-status"), get("overage-utilization")),
		Status:              get("status"),
		RepresentativeClaim: get("representative-claim"),
		Fallback:            get("fallback"),
	}

	return &ProviderSnapshot{
		Timestamp: ts,
		Source:    "api",
		Auth:      auth,
		Limits:    limits,
	}
}

func parseClaudeWindow(utilStr, resetStr string, windowMinutes int64) *WindowInfo {
	if utilStr == "" {
		return nil
	}

	w := &WindowInfo{}

	if v, err := strconv.ParseFloat(utilStr, 64); err == nil {
		w.Utilization = &v
		pct := int(math.Round(v * 100))
		w.UtilizationPct = &pct
	}

	if v, err := strconv.ParseFloat(resetStr, 64); err == nil {
		resetAt := int64(v)
		w.ResetsAt = &resetAt
		iso := time.Unix(resetAt, 0).UTC().Format("2006-01-02T15:04:05+00:00")
		w.ResetsAtISO = &iso
	}
	w.WindowMinutes = &windowMinutes

	return w
}

func parseOverage(status, utilStr string) *OverageInfo {
	if status == "" {
		return nil
	}
	o := &OverageInfo{Status: status}
	if v, err := strconv.ParseFloat(utilStr, 64); err == nil {
		o.Utilization = &v
	}
	return o
}
