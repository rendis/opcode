package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func runInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	listenAddr := fs.String("listen-addr", ":4100", "TCP listen address")
	baseURL := fs.String("base-url", "", "public base URL (derived from listen-addr if empty)")
	dbPath := fs.String("db-path", "", "database path (default: ~/.opcode/opcode.db)")
	logLevel := fs.String("log-level", "info", "log level: debug, info, warn, error")
	poolSize := fs.Int("pool-size", 10, "worker pool size")
	vaultKey := fs.String("vault-key", "", "vault passphrase (memory only, not persisted to disk)")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	dir := opcodeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot create %s: %v\n", dir, err)
		os.Exit(1)
	}

	cfg := Config{
		ListenAddr: *listenAddr,
		BaseURL:    *baseURL,
		LogLevel:   *logLevel,
		PoolSize:   *poolSize,
	}
	if *dbPath != "" {
		cfg.DBPath = *dbPath
	} else {
		cfg.DBPath = filepath.Join(dir, "opcode.db")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost" + cfg.ListenAddr
	}

	data, _ := json.MarshalIndent(cfg, "", "  ")
	path := settingsPath()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot write %s: %v\n", path, err)
		os.Exit(1)
	}
	fmt.Printf("Config written to %s\n", path)

	// Set vault key in env (memory only) if provided.
	if *vaultKey != "" {
		os.Setenv("OPCODE_VAULT_KEY", *vaultKey)
	}

	// Start the server.
	runServe()
}
