package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSessionRegistry_RegisterAndLookup(t *testing.T) {
	r := NewSessionRegistry()

	r.Register("agent-1", "session-abc")
	sid, ok := r.SessionFor("agent-1")
	assert.True(t, ok)
	assert.Equal(t, "session-abc", sid)
}

func TestSessionRegistry_NotFound(t *testing.T) {
	r := NewSessionRegistry()

	_, ok := r.SessionFor("unknown")
	assert.False(t, ok)
}

func TestSessionRegistry_Overwrite(t *testing.T) {
	r := NewSessionRegistry()

	r.Register("agent-1", "session-old")
	r.Register("agent-1", "session-new")

	sid, ok := r.SessionFor("agent-1")
	assert.True(t, ok)
	assert.Equal(t, "session-new", sid)
}

func TestSessionRegistry_Remove(t *testing.T) {
	r := NewSessionRegistry()

	r.Register("agent-1", "session-abc")
	r.Register("agent-2", "session-abc")
	r.Register("agent-3", "session-xyz")

	r.Remove("session-abc")

	_, ok := r.SessionFor("agent-1")
	assert.False(t, ok, "agent-1 should be removed")

	_, ok = r.SessionFor("agent-2")
	assert.False(t, ok, "agent-2 should be removed")

	sid, ok := r.SessionFor("agent-3")
	assert.True(t, ok, "agent-3 should still exist")
	assert.Equal(t, "session-xyz", sid)
}

func TestSessionRegistry_MultipleAgents(t *testing.T) {
	r := NewSessionRegistry()

	r.Register("agent-1", "session-1")
	r.Register("agent-2", "session-2")

	sid1, ok := r.SessionFor("agent-1")
	assert.True(t, ok)
	assert.Equal(t, "session-1", sid1)

	sid2, ok := r.SessionFor("agent-2")
	assert.True(t, ok)
	assert.Equal(t, "session-2", sid2)
}
