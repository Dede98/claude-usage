package main

import "time"

func fetchUsageSnapshot() *UsageSnapshot {
	now := time.Now().UnixMilli()
	snapshot := &UsageSnapshot{
		Version:   2,
		Timestamp: now,
		Providers: map[string]*ProviderSnapshot{},
	}

	if provider, ok := fetchClaudeProvider(now); ok && provider != nil {
		snapshot.Providers["claude"] = provider
	}
	if provider, ok := fetchCodexProvider(now); ok && provider != nil {
		snapshot.Providers["codex"] = provider
	}

	return snapshot
}
