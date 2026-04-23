package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type codexRPCResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type codexRateLimitsResult struct {
	RateLimits          *codexRateLimitSnapshot            `json:"rateLimits"`
	RateLimitsByLimitID map[string]*codexRateLimitSnapshot `json:"rateLimitsByLimitId"`
}

type codexRateLimitSnapshot struct {
	LimitID              string                `json:"limitId"`
	LimitName            *string               `json:"limitName"`
	Primary              *codexRateLimitWindow `json:"primary"`
	Secondary            *codexRateLimitWindow `json:"secondary"`
	Credits              *codexCreditsSnapshot `json:"credits"`
	PlanType             string                `json:"planType"`
	RateLimitReachedType *string               `json:"rateLimitReachedType"`
}

type codexRateLimitWindow struct {
	UsedPercent        int    `json:"usedPercent"`
	WindowDurationMins *int64 `json:"windowDurationMins"`
	ResetsAt           *int64 `json:"resetsAt"`
}

type codexCreditsSnapshot struct {
	HasCredits bool    `json:"hasCredits"`
	Unlimited  bool    `json:"unlimited"`
	Balance    *string `json:"balance"`
}

type codexSessionRateLimits struct {
	LimitID              string               `json:"limit_id"`
	LimitName            *string              `json:"limit_name"`
	Primary              *codexSessionWindow  `json:"primary"`
	Secondary            *codexSessionWindow  `json:"secondary"`
	Credits              *codexSessionCredits `json:"credits"`
	PlanType             string               `json:"plan_type"`
	RateLimitReachedType *string              `json:"rate_limit_reached_type"`
}

type codexSessionWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	WindowMinutes int64   `json:"window_minutes"`
	ResetsAt      int64   `json:"resets_at"`
}

type codexSessionCredits struct {
	HasCredits bool    `json:"has_credits"`
	Unlimited  bool    `json:"unlimited"`
	Balance    *string `json:"balance"`
}

type codexSessionEvent struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type       string                  `json:"type"`
		RateLimits *codexSessionRateLimits `json:"rate_limits"`
	} `json:"payload"`
}

func fetchCodexProvider(now int64) (*ProviderSnapshot, bool) {
	if _, err := exec.LookPath("codex"); err != nil {
		return nil, false
	}

	provider, err := fetchCodexViaAppServer(now)
	if err == nil {
		return provider, true
	}

	fallback, fallbackErr := fetchCodexFromSessions(now)
	if fallbackErr == nil {
		return fallback, true
	}

	if isCodexUnavailable(err) && isCodexUnavailable(fallbackErr) {
		return nil, false
	}

	errStr := fmt.Sprintf("codex_fetch_failed: %v", err)
	if fallbackErr != nil && !isCodexUnavailable(fallbackErr) {
		errStr += fmt.Sprintf(" (fallback: %v)", fallbackErr)
	}
	return &ProviderSnapshot{
		Timestamp: now,
		Source:    "codex_app_server",
		Error:     &errStr,
	}, true
}

func fetchCodexViaAppServer(now int64) (*ProviderSnapshot, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "codex", "app-server", "--listen", "stdio://")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	lines := make(chan string, 32)
	errCh := make(chan error, 2)
	go scanCodexStream(stdout, lines, errCh)
	go scanCodexStream(stderr, lines, errCh)

	if err := sendCodexRPC(stdin, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"clientInfo": map[string]any{
				"name":    "ccusage",
				"title":   "ccusage",
				"version": "0.1.0",
			},
			"capabilities": map[string]any{
				"experimentalApi":           true,
				"optOutNotificationMethods": []string{"thread/started", "thread/status/changed"},
			},
		},
	}); err != nil {
		return nil, err
	}
	initResp, err := waitForCodexResponse(ctx, lines, errCh, 1)
	if err != nil {
		return nil, err
	}
	if initResp.Error != nil {
		return nil, errors.New(initResp.Error.Message)
	}

	if err := sendCodexRPC(stdin, map[string]any{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]any{},
	}); err != nil {
		return nil, err
	}
	if err := sendCodexRPC(stdin, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "account/rateLimits/read",
		"params":  map[string]any{},
	}); err != nil {
		return nil, err
	}

	limitsResp, err := waitForCodexResponse(ctx, lines, errCh, 2)
	if err != nil {
		return nil, err
	}
	if limitsResp.Error != nil {
		return nil, errors.New(limitsResp.Error.Message)
	}

	var limits codexRateLimitsResult
	if err := json.Unmarshal(limitsResp.Result, &limits); err != nil {
		return nil, err
	}
	if limits.RateLimits == nil {
		return nil, errors.New("codex rate limits missing")
	}

	provider := &ProviderSnapshot{
		Timestamp: now,
		Source:    "codex_app_server",
		Auth: &ProviderAuth{
			AccountType: "chatgpt",
			PlanType:    limits.RateLimits.PlanType,
		},
		Limits: codexSnapshotToLimits(limits.RateLimits, limits.RateLimitsByLimitID),
	}
	return provider, nil
}

func sendCodexRPC(w io.Writer, req map[string]any) error {
	enc := json.NewEncoder(w)
	return enc.Encode(req)
}

func scanCodexStream(r io.Reader, lines chan<- string, errCh chan<- error) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		lines <- scanner.Text()
	}
	if err := scanner.Err(); err != nil {
		errCh <- err
	}
}

func waitForCodexResponse(ctx context.Context, lines <-chan string, errCh <-chan error, wantID int) (*codexRPCResponse, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-errCh:
			return nil, err
		case line := <-lines:
			if !strings.HasPrefix(line, "{") {
				continue
			}
			var resp codexRPCResponse
			if err := json.Unmarshal([]byte(line), &resp); err != nil {
				continue
			}
			if resp.ID == wantID {
				return &resp, nil
			}
		}
	}
}

func codexSnapshotToLimits(primary *codexRateLimitSnapshot, buckets map[string]*codexRateLimitSnapshot) *ProviderLimits {
	limits := &ProviderLimits{
		Primary:   codexWindowToInfo(primary.Primary),
		Secondary: codexWindowToInfo(primary.Secondary),
		LimitID:   primary.LimitID,
		Credits:   codexCreditsToInfo(primary.Credits),
	}
	if primary.LimitName != nil {
		limits.LimitName = *primary.LimitName
	}
	if primary.PlanType != "" {
		limits.Status = primary.PlanType
	}
	if primary.RateLimitReachedType != nil {
		limits.RateLimitReachedType = *primary.RateLimitReachedType
	}

	if len(buckets) > 0 {
		limits.Buckets = make(map[string]*ProviderBucket, len(buckets))
		for key, bucket := range buckets {
			if bucket == nil {
				continue
			}
			entry := &ProviderBucket{
				LimitID:   bucket.LimitID,
				Primary:   codexWindowToInfo(bucket.Primary),
				Secondary: codexWindowToInfo(bucket.Secondary),
				Credits:   codexCreditsToInfo(bucket.Credits),
				PlanType:  bucket.PlanType,
			}
			if bucket.LimitName != nil {
				entry.LimitName = *bucket.LimitName
			}
			if bucket.RateLimitReachedType != nil {
				entry.RateLimitReachedType = *bucket.RateLimitReachedType
			}
			limits.Buckets[key] = entry
		}
	}

	return limits
}

func codexWindowToInfo(w *codexRateLimitWindow) *WindowInfo {
	if w == nil {
		return nil
	}
	pct := w.UsedPercent
	util := float64(pct) / 100
	info := &WindowInfo{
		Utilization:    &util,
		UtilizationPct: &pct,
		ResetsAt:       w.ResetsAt,
		WindowMinutes:  w.WindowDurationMins,
	}
	if w.ResetsAt != nil {
		iso := time.Unix(*w.ResetsAt, 0).UTC().Format("2006-01-02T15:04:05+00:00")
		info.ResetsAtISO = &iso
	}
	return info
}

func codexCreditsToInfo(c *codexCreditsSnapshot) *CreditsInfo {
	if c == nil {
		return nil
	}
	return &CreditsInfo{
		HasCredits: c.HasCredits,
		Unlimited:  c.Unlimited,
		Balance:    c.Balance,
	}
}

func fetchCodexFromSessions(now int64) (*ProviderSnapshot, error) {
	root, err := codexSessionsRoot()
	if err != nil {
		return nil, err
	}

	files, err := collectRecentCodexSessionFiles(root)
	if err != nil {
		return nil, err
	}
	for _, path := range files {
		limits, ts, err := readCodexSessionSnapshot(path)
		if err != nil {
			continue
		}
		snapshot := &ProviderSnapshot{
			Timestamp: ts,
			Source:    "codex_sessions",
			Auth: &ProviderAuth{
				AccountType: "chatgpt",
				PlanType:    limits.PlanType,
			},
			Limits: codexSessionLimitsToProvider(limits),
		}
		if snapshot.Timestamp == 0 {
			snapshot.Timestamp = now
		}
		return snapshot, nil
	}

	return nil, errors.New("no codex session rate limits found")
}

func codexSessionsRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "sessions"), nil
}

func collectRecentCodexSessionFiles(root string) ([]string, error) {
	type fileInfo struct {
		path string
		mod  time.Time
	}

	var files []fileInfo
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, fileInfo{path: path, mod: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].mod.After(files[j].mod)
	})

	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.path)
	}
	return paths, nil
}

func readCodexSessionSnapshot(path string) (*codexSessionRateLimits, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	var latest *codexSessionRateLimits
	var latestTS int64

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		var event codexSessionEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		if event.Type != "event_msg" || event.Payload.Type != "token_count" || event.Payload.RateLimits == nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339, event.Timestamp)
		if err != nil {
			continue
		}
		copied := *event.Payload.RateLimits
		latest = &copied
		latestTS = ts.UnixMilli()
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	if latest == nil {
		return nil, 0, errors.New("no rate limits in session file")
	}
	return latest, latestTS, nil
}

func codexSessionLimitsToProvider(snapshot *codexSessionRateLimits) *ProviderLimits {
	limits := &ProviderLimits{
		Primary:   codexSessionWindowToInfo(snapshot.Primary),
		Secondary: codexSessionWindowToInfo(snapshot.Secondary),
		LimitID:   snapshot.LimitID,
		Credits:   codexSessionCreditsToInfo(snapshot.Credits),
		Status:    snapshot.PlanType,
	}
	if snapshot.LimitName != nil {
		limits.LimitName = *snapshot.LimitName
	}
	if snapshot.RateLimitReachedType != nil {
		limits.RateLimitReachedType = *snapshot.RateLimitReachedType
	}
	return limits
}

func codexSessionWindowToInfo(w *codexSessionWindow) *WindowInfo {
	if w == nil {
		return nil
	}
	pct := int(w.UsedPercent)
	util := w.UsedPercent / 100
	info := &WindowInfo{
		Utilization:    &util,
		UtilizationPct: &pct,
		WindowMinutes:  &w.WindowMinutes,
	}
	if w.ResetsAt > 0 {
		resetAt := w.ResetsAt
		info.ResetsAt = &resetAt
		iso := time.Unix(resetAt, 0).UTC().Format("2006-01-02T15:04:05+00:00")
		info.ResetsAtISO = &iso
	}
	return info
}

func codexSessionCreditsToInfo(c *codexSessionCredits) *CreditsInfo {
	if c == nil {
		return nil
	}
	return &CreditsInfo{
		HasCredits: c.HasCredits,
		Unlimited:  c.Unlimited,
		Balance:    c.Balance,
	}
}

func isCodexUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "subscription login not available") ||
		strings.Contains(msg, "no codex session rate limits found")
}
