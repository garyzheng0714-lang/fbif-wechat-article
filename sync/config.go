package sync

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

type SyncConfig struct {
	TableSuffix string `json:"tableSuffix"`
}

func getConfigPaths() []string {
	cwd, _ := os.Getwd()
	return []string{
		filepath.Join(cwd, ".sync-config.json"),
		filepath.Join(cwd, "..", ".sync-config.json"),
	}
}

func ReadConfig() (*SyncConfig, error) {
	for _, p := range getConfigPaths() {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var cfg SyncConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", p, err)
		}
		return &cfg, nil
	}
	return &SyncConfig{}, nil
}

func WriteConfig(cfg *SyncConfig) error {
	paths := getConfigPaths()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(paths[0], data, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	log.Printf("[Config] Saved: tableSuffix=%q", cfg.TableSuffix)
	return nil
}

// GetTableSuffix returns the current table name suffix (e.g. "_v2").
// Reads live from disk so changes take effect without restart.
func GetTableSuffix() string {
	cfg, err := ReadConfig()
	if err != nil || cfg == nil {
		return ""
	}
	return cfg.TableSuffix
}

// TableName appends the current table suffix to a base table name.
func TableName(base string) string {
	return base + GetTableSuffix()
}
