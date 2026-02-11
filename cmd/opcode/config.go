package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
)

// Config holds all opcode server configuration.
// Priority: env vars > settings.json > defaults.
type Config struct {
	ListenAddr string `json:"listen_addr"`
	BaseURL    string `json:"base_url"`
	DBPath     string `json:"db_path"`
	LogLevel   string `json:"log_level"`
	PoolSize   int    `json:"pool_size"`
	Panel      bool   `json:"panel"`
}

func defaultConfig() Config {
	return Config{
		ListenAddr: ":4100",
		DBPath:     filepath.Join(opcodeDir(), "opcode.db"),
		LogLevel:   "info",
		PoolSize:   10,
	}
}

func opcodeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".opcode"
	}
	return filepath.Join(home, ".opcode")
}

func settingsPath() string {
	return filepath.Join(opcodeDir(), "settings.json")
}

func loadConfig() Config {
	cfg := defaultConfig()

	// Layer 2: settings.json (ignore if missing).
	if data, err := os.ReadFile(settingsPath()); err == nil {
		_ = json.Unmarshal(data, &cfg)
	}

	// Layer 3: env vars override.
	if v := os.Getenv("OPCODE_LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}
	if v := os.Getenv("OPCODE_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if v := os.Getenv("OPCODE_DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("OPCODE_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("OPCODE_POOL_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.PoolSize = n
		}
	}
	if v := os.Getenv("OPCODE_PANEL"); v != "" {
		cfg.Panel = v == "true" || v == "1"
	}

	// Derive base_url from listen_addr if empty.
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost" + cfg.ListenAddr
	}

	return cfg
}

// configDiff describes what changed between two configurations.
type configDiff struct {
	PanelChanged    bool
	LogLevelChanged bool
	RestartNeeded   []string // fields that require a server restart
}

func diffConfigs(old, new Config) configDiff {
	var d configDiff
	if old.Panel != new.Panel {
		d.PanelChanged = true
	}
	if old.LogLevel != new.LogLevel {
		d.LogLevelChanged = true
	}
	if old.ListenAddr != new.ListenAddr {
		d.RestartNeeded = append(d.RestartNeeded, "listen_addr")
	}
	if old.BaseURL != new.BaseURL {
		d.RestartNeeded = append(d.RestartNeeded, "base_url")
	}
	if old.DBPath != new.DBPath {
		d.RestartNeeded = append(d.RestartNeeded, "db_path")
	}
	if old.PoolSize != new.PoolSize {
		d.RestartNeeded = append(d.RestartNeeded, "pool_size")
	}
	return d
}

func pidPath() string {
	return filepath.Join(opcodeDir(), "opcode.pid")
}
