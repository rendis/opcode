package main

import (
	"net/http"
	"sync"
)

// handlerSwapper is an http.Handler that allows atomic handler replacement.
// Used for hot-reloading the HTTP mux when config changes (e.g., panel toggle).
type handlerSwapper struct {
	mu      sync.RWMutex
	handler http.Handler
}

func newHandlerSwapper(h http.Handler) *handlerSwapper {
	return &handlerSwapper{handler: h}
}

func (s *handlerSwapper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	h := s.handler
	s.mu.RUnlock()
	h.ServeHTTP(w, r)
}

// Swap replaces the underlying handler atomically.
func (s *handlerSwapper) Swap(h http.Handler) {
	s.mu.Lock()
	s.handler = h
	s.mu.Unlock()
}
