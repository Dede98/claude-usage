package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

const keychainService = "Claude Code-credentials"

type Credentials struct {
	AccessToken      string
	SubscriptionType string
	RateLimitTier    string
	ExpiresAt        int64 // unix ms
}

func getCredentials() (*Credentials, error) {
	cmd := exec.Command("security", "find-generic-password", "-s", keychainService, "-w")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("keychain: could not read credentials. Is Claude Code installed?")
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse: credentials not valid JSON")
	}

	oauth, ok := raw["claudeAiOauth"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("no_token: claudeAiOauth not found")
	}

	token, _ := oauth["accessToken"].(string)
	if token == "" {
		return nil, fmt.Errorf("no_token: accessToken empty")
	}

	creds := &Credentials{AccessToken: token}

	if v, ok := oauth["subscriptionType"].(string); ok {
		creds.SubscriptionType = v
	}
	if v, ok := oauth["rateLimitTier"].(string); ok {
		creds.RateLimitTier = v
	}
	if v, ok := oauth["expiresAt"].(float64); ok {
		creds.ExpiresAt = int64(v)
	}

	return creds, nil
}

func (c *Credentials) isExpired() bool {
	if c.ExpiresAt == 0 {
		return false
	}
	return c.ExpiresAt < time.Now().UnixMilli()
}
