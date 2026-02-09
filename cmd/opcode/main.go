package main

import (
	"fmt"
	"os"
)

func main() {
	// DI wiring will be added as implementations are built.
	// Cycle 1: Store (libSQL) → Executor → WorkerPool
	// Cycle 2+: MCP Server, Plugins, Expressions, etc.
	fmt.Println("opcode engine")
	os.Exit(0)
}
