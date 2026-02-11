// gen-diagrams generates sample diagram outputs for README documentation.
// Run: go run ./cmd/gen-diagrams
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rendis/opcode/internal/diagram"
	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
)

func main() {
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "fetch-data", Type: schema.StepTypeAction, Action: "http.request"},
			{ID: "validate", Type: schema.StepTypeAction, Action: "jq", DependsOn: []string{"fetch-data"}},
			{ID: "decide", Type: schema.StepTypeReasoning, DependsOn: []string{"validate"},
				Config: mustJSON(schema.ReasoningConfig{
					PromptContext: "Choose processing strategy",
					Options: []schema.ReasoningOption{
						{ID: "fast", Description: "Quick processing"},
						{ID: "thorough", Description: "Deep analysis"},
					},
				})},
			{ID: "process", Type: schema.StepTypeAction, Action: "shell.exec", DependsOn: []string{"decide"}},
			{ID: "notify", Type: schema.StepTypeAction, Action: "http.request", DependsOn: []string{"process"}},
		},
	}

	// With status overlay
	states := []*store.StepState{
		{StepID: "fetch-data", Status: schema.StepStatusCompleted, DurationMs: 450},
		{StepID: "validate", Status: schema.StepStatusCompleted, DurationMs: 12},
		{StepID: "decide", Status: schema.StepStatusSuspended},
		{StepID: "process", Status: schema.StepStatusPending},
		{StepID: "notify", Status: schema.StepStatusPending},
	}

	model, err := diagram.Build(def, states)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build error: %v\n", err)
		os.Exit(1)
	}

	outDir := filepath.Join("docs", "assets")
	os.MkdirAll(outDir, 0o755)

	// ASCII
	ascii := diagram.RenderASCII(model)
	os.WriteFile(filepath.Join(outDir, "diagram-ascii.txt"), []byte(ascii), 0o644)
	fmt.Println("=== ASCII ===")
	fmt.Println(ascii)

	// Mermaid
	mermaid := diagram.RenderMermaid(model)
	os.WriteFile(filepath.Join(outDir, "diagram-mermaid.md"), []byte("```mermaid\n"+mermaid+"\n```\n"), 0o644)
	fmt.Println("=== Mermaid ===")
	fmt.Println(mermaid)

	// Image (PNG)
	png, imgErr := diagram.RenderImage(model)
	if imgErr != nil {
		fmt.Fprintf(os.Stderr, "image error: %v\n", imgErr)
	} else {
		os.WriteFile(filepath.Join(outDir, "diagram-sample.png"), png, 0o644)
		fmt.Printf("=== Image ===\nPNG written: %d bytes\n", len(png))
	}
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
