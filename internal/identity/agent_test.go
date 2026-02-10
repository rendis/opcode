package identity

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
)

// mockAgentStore satisfies the store.Store methods used by identity.
// Only agent methods are implemented; others panic.
type mockAgentStore struct {
	store.Store // embed to satisfy interface; unused methods panic
	agents      map[string]*store.Agent
}

func newMockAgentStore() *mockAgentStore {
	return &mockAgentStore{agents: make(map[string]*store.Agent)}
}

func (m *mockAgentStore) RegisterAgent(_ context.Context, a *store.Agent) error {
	if _, exists := m.agents[a.ID]; exists {
		return schema.NewErrorf(schema.ErrCodeConflict, "agent %q already exists", a.ID)
	}
	cp := *a
	m.agents[a.ID] = &cp
	return nil
}

func (m *mockAgentStore) GetAgent(_ context.Context, id string) (*store.Agent, error) {
	a, ok := m.agents[id]
	if !ok {
		return nil, schema.NewErrorf(schema.ErrCodeNotFound, "agent %q not found", id)
	}
	cp := *a
	return &cp, nil
}

func (m *mockAgentStore) UpdateAgentSeen(_ context.Context, id string) error {
	if _, ok := m.agents[id]; !ok {
		return schema.NewErrorf(schema.ErrCodeNotFound, "agent %q not found", id)
	}
	return nil
}

// --- ValidateAgentType ---

func TestValidateAgentType_Valid(t *testing.T) {
	for _, typ := range []string{AgentTypeLLM, AgentTypeSystem, AgentTypeHuman, AgentTypeService} {
		assert.NoError(t, ValidateAgentType(typ), "type %q should be valid", typ)
	}
}

func TestValidateAgentType_Invalid(t *testing.T) {
	err := ValidateAgentType("robot")
	require.Error(t, err)
	var opcErr *schema.OpcodeError
	require.True(t, errors.As(err, &opcErr))
	assert.Equal(t, schema.ErrCodeValidation, opcErr.Code)
}

func TestValidateAgentType_Empty(t *testing.T) {
	err := ValidateAgentType("")
	require.Error(t, err)
}

// --- ValidateAgent ---

func TestValidateAgent_EmptyID(t *testing.T) {
	err := ValidateAgent(&store.Agent{ID: "", Name: "n", Type: AgentTypeLLM})
	require.Error(t, err)
	var opcErr *schema.OpcodeError
	require.True(t, errors.As(err, &opcErr))
	assert.Equal(t, schema.ErrCodeValidation, opcErr.Code)
	assert.Contains(t, opcErr.Message, "id")
}

func TestValidateAgent_EmptyName(t *testing.T) {
	err := ValidateAgent(&store.Agent{ID: "x", Name: "", Type: AgentTypeLLM})
	require.Error(t, err)
	var opcErr *schema.OpcodeError
	require.True(t, errors.As(err, &opcErr))
	assert.Contains(t, opcErr.Message, "name")
}

func TestValidateAgent_InvalidType(t *testing.T) {
	err := ValidateAgent(&store.Agent{ID: "x", Name: "n", Type: "invalid"})
	require.Error(t, err)
}

func TestValidateAgent_Valid(t *testing.T) {
	err := ValidateAgent(&store.Agent{ID: "x", Name: "n", Type: AgentTypeService})
	assert.NoError(t, err)
}

// --- EnsureRegistered ---

func TestEnsureRegistered_NewAgent(t *testing.T) {
	s := newMockAgentStore()
	ctx := context.Background()

	agent, err := EnsureRegistered(ctx, s, "agent-1", "Agent One", AgentTypeLLM, nil)
	require.NoError(t, err)
	assert.Equal(t, "agent-1", agent.ID)
	assert.Equal(t, "Agent One", agent.Name)
	assert.Equal(t, AgentTypeLLM, agent.Type)
}

func TestEnsureRegistered_ExistingAgent(t *testing.T) {
	s := newMockAgentStore()
	ctx := context.Background()

	// Pre-register.
	require.NoError(t, s.RegisterAgent(ctx, &store.Agent{
		ID: "agent-1", Name: "Agent One", Type: AgentTypeSystem,
	}))

	agent, err := EnsureRegistered(ctx, s, "agent-1", "Agent One Updated", AgentTypeLLM, nil)
	require.NoError(t, err)
	// Should return existing, not re-register.
	assert.Equal(t, "agent-1", agent.ID)
	assert.Equal(t, "Agent One", agent.Name) // original name preserved
	assert.Equal(t, AgentTypeSystem, agent.Type)
}

func TestEnsureRegistered_WithMetadata(t *testing.T) {
	s := newMockAgentStore()
	ctx := context.Background()

	meta := json.RawMessage(`{"model":"claude-4"}`)
	agent, err := EnsureRegistered(ctx, s, "agent-2", "Bot", AgentTypeLLM, meta)
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"claude-4"}`, string(agent.Metadata))
}

func TestEnsureRegistered_InvalidType(t *testing.T) {
	s := newMockAgentStore()
	ctx := context.Background()

	_, err := EnsureRegistered(ctx, s, "agent-1", "Bot", "robot", nil)
	require.Error(t, err)
	var opcErr *schema.OpcodeError
	require.True(t, errors.As(err, &opcErr))
	assert.Equal(t, schema.ErrCodeValidation, opcErr.Code)
}

func TestEnsureRegistered_EmptyID(t *testing.T) {
	s := newMockAgentStore()
	ctx := context.Background()

	_, err := EnsureRegistered(ctx, s, "", "Bot", AgentTypeLLM, nil)
	require.Error(t, err)
}
