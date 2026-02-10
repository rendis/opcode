package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestDiag_AllExamples loads and runs each example workflow to catalog errors.
func TestDiag_AllExamples(t *testing.T) {
	// List all example directories.
	entries, err := os.ReadDir(examplesDir())
	if err != nil {
		t.Fatalf("read examples dir: %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		wfPath := filepath.Join(examplesDir(), name, "workflow.json")
		if _, err := os.Stat(wfPath); err != nil {
			continue
		}

		t.Run(name, func(t *testing.T) {
			h := newExampleHarness(t)
			def := loadWorkflow(t, name)

			// Read the workflow.json to extract input_schema and build dummy inputs.
			data, _ := os.ReadFile(wfPath)
			var wrapper struct {
				InputSchema struct {
					Required   []string                   `json:"required"`
					Properties map[string]json.RawMessage `json:"properties"`
				} `json:"input_schema"`
			}
			_ = json.Unmarshal(data, &wrapper)

			// Build dummy inputs: strings get mock URLs or paths, ints get defaults.
			params := map[string]any{}
			srv := mockServer(t, map[string]any{
				"ok": true, "status": "success", "body": "<p>content</p>",
				"data": []any{map[string]any{"id": 1, "name": "item1"}},
				"items": []any{"a", "b"},
			})
			for k := range wrapper.InputSchema.Properties {
				// Determine type from raw JSON.
				var prop struct {
					Type string `json:"type"`
				}
				_ = json.Unmarshal(wrapper.InputSchema.Properties[k], &prop)
				switch prop.Type {
				case "integer":
					params[k] = 100
				case "array":
					params[k] = []any{"item1", "item2"}
				case "boolean":
					params[k] = true
				default:
					// For URLs, use mock server. For paths, use tempDir.
					if contains(k, "url", "endpoint", "api") {
						params[k] = srv.URL
					} else if contains(k, "path", "dir", "directory", "file") {
						params[k] = filepath.Join(h.tempDir, k+".txt")
						// Create the file for read operations.
						_ = os.WriteFile(params[k].(string), []byte("test content"), 0644)
					} else {
						params[k] = "test-" + k
					}
				}
			}

			result := h.run(def, params)
			fmt.Printf("DIAG %s: status=%s", name, result.Status)
			if result.Error != nil {
				fmt.Printf(" error=%s", result.Error.Message)
			}
			fmt.Println()
			if result.Status == "failed" {
				for id, sr := range result.Steps {
					if sr.Error != nil {
						fmt.Printf("  STEP %s: %s\n", id, sr.Error.Message)
					}
				}
			}
		})
	}
}

func contains(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
