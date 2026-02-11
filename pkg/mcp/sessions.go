package mcp

import "sync"

// SessionRegistry maps agent IDs to MCP session IDs.
// Populated automatically when agents call any tool that includes agent_id.
type SessionRegistry struct {
	mu       sync.RWMutex
	sessions map[string]string // agentID → sessionID
}

// NewSessionRegistry creates a new empty SessionRegistry.
func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{sessions: make(map[string]string)}
}

// Register associates an agent ID with a session ID.
// If the agent already has a session, it is overwritten (reconnect).
func (r *SessionRegistry) Register(agentID, sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[agentID] = sessionID
}

// SessionFor returns the session ID for the given agent, if connected.
func (r *SessionRegistry) SessionFor(agentID string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	sid, ok := r.sessions[agentID]
	return sid, ok
}

// Remove deletes all agent mappings for the given session ID.
// Called when a session disconnects.
func (r *SessionRegistry) Remove(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for aid, sid := range r.sessions {
		if sid == sessionID {
			delete(r.sessions, aid)
		}
	}
}
