package panel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// toJSON marshals a value to indented JSON for template rendering.
func toJSON(v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}

// timeAgo returns a human-readable relative time string.
// Accepts time.Time or *time.Time.
func timeAgo(v any) string {
	var t time.Time
	switch val := v.(type) {
	case time.Time:
		t = val
	case *time.Time:
		if val == nil {
			return ""
		}
		t = *val
	default:
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// add returns a + b.
func add(a, b int) int { return a + b }

// subtract returns a - b, clamped to 0.
func subtract(a, b int) int {
	if a-b < 0 {
		return 0
	}
	return a - b
}

// statusBadge returns a CSS class name for a workflow/step status.
func statusBadge(status string) string {
	switch status {
	case "completed":
		return "badge-success"
	case "failed":
		return "badge-error"
	case "active", "running":
		return "badge-active"
	case "suspended":
		return "badge-warning"
	case "pending", "scheduled":
		return "badge-secondary"
	case "cancelled", "skipped":
		return "badge-muted"
	default:
		return "badge-secondary"
	}
}

// truncate shortens a string to max length, appending "..." if truncated.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// queryInt extracts an integer query param with a default value.
func queryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
