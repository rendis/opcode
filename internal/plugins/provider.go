package plugins

import (
	"context"

	"github.com/rendis/opcode/internal/actions"
)

// PluginProvider is an external action source (e.g. MCP server).
// When started, it discovers available tools and registers them as actions.
type PluginProvider interface {
	Name() string
	Actions() []actions.Action
	HealthCheck(ctx context.Context) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}
