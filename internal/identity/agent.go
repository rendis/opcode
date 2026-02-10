package identity

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
)

// Agent type constants.
const (
	AgentTypeLLM     = "llm"
	AgentTypeSystem  = "system"
	AgentTypeHuman   = "human"
	AgentTypeService = "service"
)

var validAgentTypes = map[string]bool{
	AgentTypeLLM:     true,
	AgentTypeSystem:  true,
	AgentTypeHuman:   true,
	AgentTypeService: true,
}

// ValidateAgentType checks that typ is one of the valid agent types.
func ValidateAgentType(typ string) error {
	if !validAgentTypes[typ] {
		return schema.NewErrorf(schema.ErrCodeValidation,
			"invalid agent type %q: must be one of llm, system, human, service", typ)
	}
	return nil
}

// ValidateAgent checks required fields on an Agent.
func ValidateAgent(agent *store.Agent) error {
	if agent.ID == "" {
		return schema.NewError(schema.ErrCodeValidation, "agent id is required")
	}
	if agent.Name == "" {
		return schema.NewError(schema.ErrCodeValidation, "agent name is required")
	}
	return ValidateAgentType(agent.Type)
}

// EnsureRegistered retrieves an existing agent or registers a new one.
// If the agent exists, it updates last_seen_at and returns the stored record.
// If not found, it registers the agent and returns the new record.
func EnsureRegistered(ctx context.Context, s store.Store, id, name, typ string, metadata json.RawMessage) (*store.Agent, error) {
	existing, err := s.GetAgent(ctx, id)
	if err == nil {
		_ = s.UpdateAgentSeen(ctx, id)
		return existing, nil
	}

	var opcErr *schema.OpcodeError
	if !errors.As(err, &opcErr) || opcErr.Code != schema.ErrCodeNotFound {
		return nil, err
	}

	agent := &store.Agent{
		ID:       id,
		Name:     name,
		Type:     typ,
		Metadata: metadata,
	}
	if err := ValidateAgent(agent); err != nil {
		return nil, err
	}
	if err := s.RegisterAgent(ctx, agent); err != nil {
		return nil, err
	}
	return s.GetAgent(ctx, id)
}
