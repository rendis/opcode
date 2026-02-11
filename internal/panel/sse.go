package panel

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/rendis/opcode/internal/streaming"
)

// handleSSEGlobal streams all events to the client via Server-Sent Events.
func (s *PanelServer) handleSSEGlobal(w http.ResponseWriter, r *http.Request) {
	s.serveSSE(w, r, streaming.EventFilter{})
}

// handleSSEWorkflow streams events for a specific workflow.
func (s *PanelServer) handleSSEWorkflow(w http.ResponseWriter, r *http.Request) {
	workflowID := r.PathValue("id")
	s.serveSSE(w, r, streaming.EventFilter{WorkflowID: workflowID})
}

// serveSSE is the common SSE implementation.
func (s *PanelServer) serveSSE(w http.ResponseWriter, r *http.Request, filter streaming.EventFilter) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, cancel, err := s.deps.Hub.Subscribe(r.Context(), filter)
	if err != nil {
		s.deps.Logger.Error("SSE subscribe failed", "error", err)
		http.Error(w, "subscribe failed", http.StatusInternalServerError)
		return
	}
	defer cancel()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(event)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.EventType, data)
			flusher.Flush()
		}
	}
}
