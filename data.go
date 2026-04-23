package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func usageFilePath() string {
	if p := os.Getenv("USAGE_FILE"); p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "usage-data.json")
}

func writeUsageFile(data *UsageSnapshot) error {
	path := usageFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	out, err := json.Marshal(data)
	if err != nil {
		return err
	}

	// Atomic write: tmp file + rename
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, out, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readUsageFile() (*UsageSnapshot, error) {
	raw, err := os.ReadFile(usageFilePath())
	if err != nil {
		return nil, err
	}
	var data UsageSnapshot
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	if data.Providers == nil {
		data.Providers = map[string]*ProviderSnapshot{}
	}
	return &data, nil
}
