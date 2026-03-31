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

type UsageData struct {
	Version   int        `json:"version"`
	Timestamp int64      `json:"timestamp"`
	Source    string     `json:"source"`
	Auth      *AuthInfo  `json:"auth"`
	Limits    *LimitsInfo `json:"limits"`
	Error     *string    `json:"error"`
}

type AuthInfo struct {
	SubscriptionType string `json:"subscription_type"`
	RateLimitTier    string `json:"rate_limit_tier"`
	TokenExpiresAt   int64  `json:"token_expires_at"`
}

type LimitsInfo struct {
	FiveHour            *WindowInfo  `json:"five_hour"`
	SevenDay            *WindowInfo  `json:"seven_day"`
	Overage             *OverageInfo `json:"overage"`
	Status              string       `json:"status"`
	RepresentativeClaim string       `json:"representative_claim"`
	Fallback            string       `json:"fallback"`
}

type WindowInfo struct {
	Utilization    *float64 `json:"utilization"`
	UtilizationPct *int     `json:"utilization_pct"`
	ResetsAt       *float64 `json:"resets_at"`
	ResetsAtISO    *string  `json:"resets_at_iso"`
}

type OverageInfo struct {
	Status      string   `json:"status"`
	Utilization *float64 `json:"utilization"`
}

func pingAPI() *UsageData {
	now := time.Now().UnixMilli()

	creds, err := getCredentials()
	if err != nil {
		errStr := fmt.Sprintf("auth_failed: %s. Run 'claude /login' first.", err)
		return &UsageData{Version: 1, Timestamp: now, Source: "api", Error: &errStr}
	}

	if creds.isExpired() {
		errStr := "token_expired. Open Claude Code to refresh your session."
		auth := &AuthInfo{
			SubscriptionType: creds.SubscriptionType,
			RateLimitTier:    creds.RateLimitTier,
			TokenExpiresAt:   creds.ExpiresAt,
		}
		return &UsageData{Version: 1, Timestamp: now, Source: "api", Auth: auth, Error: &errStr}
	}

	auth := &AuthInfo{
		SubscriptionType: creds.SubscriptionType,
		RateLimitTier:    creds.RateLimitTier,
		TokenExpiresAt:   creds.ExpiresAt,
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
		return &UsageData{Version: 1, Timestamp: now, Source: "api", Auth: auth, Error: &errStr}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		errStr := fmt.Sprintf("HTTP %d", resp.StatusCode)
		return &UsageData{Version: 1, Timestamp: now, Source: "api", Auth: auth, Error: &errStr}
	}

	return parseHeaders(resp.Header, auth, now)
}

func parseHeaders(h http.Header, auth *AuthInfo, ts int64) *UsageData {
	get := func(name string) string {
		return h.Get("anthropic-ratelimit-unified-" + name)
	}

	limits := &LimitsInfo{
		FiveHour:            parseWindow(get("5h-utilization"), get("5h-reset")),
		SevenDay:            parseWindow(get("7d-utilization"), get("7d-reset")),
		Overage:             parseOverage(get("overage-status"), get("overage-utilization")),
		Status:              get("status"),
		RepresentativeClaim: get("representative-claim"),
		Fallback:            get("fallback"),
	}

	return &UsageData{
		Version:   1,
		Timestamp: ts,
		Source:    "api",
		Auth:      auth,
		Limits:    limits,
	}
}

func parseWindow(utilStr, resetStr string) *WindowInfo {
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
		w.ResetsAt = &v
		iso := time.Unix(int64(v), 0).UTC().Format("2006-01-02T15:04:05+00:00")
		w.ResetsAtISO = &iso
	}

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
