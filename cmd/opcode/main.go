package main

import (
	"fmt"
	"log/slog"
	"os"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// DI wiring — built incrementally per cycle.
	// Cycle 1: Store (libSQL) → Executor → WorkerPool
	// Cycle 2: Expressions, Validation, Resilience
	// Cycle 3: Action Registry + built-in actions (HTTP, Crypto, Assert)
	fmt.Println("opcode engine")
	os.Exit(0)
}
