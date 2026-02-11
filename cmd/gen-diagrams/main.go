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
	// Branching workflow: fetch → validate → condition(in_stock?) → two branches → merge → ship
	def := &schema.WorkflowDefinition{
		Steps: []schema.StepDefinition{
			{ID: "fetch-data", Type: schema.StepTypeAction, Action: "http.request"},
			{ID: "validate", Type: schema.StepTypeAction, Action: "jq", DependsOn: []string{"fetch-data"}},
			{ID: "check-stock", Type: schema.StepTypeCondition, DependsOn: []string{"validate"},
				Config: mustJSON(schema.ConditionConfig{
					Expression: "${{ steps.validate.output.quantity > 0 }}",
					Branches: map[string][]schema.StepDefinition{
						"in_stock": {
							{ID: "process-payment", Type: schema.StepTypeAction, Action: "http.request"},
						},
						"out_of_stock": {
							{ID: "notify-restock", Type: schema.StepTypeAction, Action: "http.request"},
						},
					},
				})},
			{ID: "approval", Type: schema.StepTypeReasoning, DependsOn: []string{"check-stock"},
				Config: mustJSON(schema.ReasoningConfig{
					PromptContext: "Approve order for shipping?",
					Options: []schema.ReasoningOption{
						{ID: "approve", Description: "Ship the order"},
						{ID: "reject", Description: "Cancel order"},
					},
				})},
			{ID: "ship", Type: schema.StepTypeAction, Action: "shell.exec", DependsOn: []string{"approval"}},
		},
	}

	states := []*store.StepState{
		{StepID: "fetch-data", Status: schema.StepStatusCompleted, DurationMs: 450},
		{StepID: "validate", Status: schema.StepStatusCompleted, DurationMs: 12},
		{StepID: "check-stock", Status: schema.StepStatusCompleted, DurationMs: 3},
		{StepID: "check-stock.in_stock.process-payment", Status: schema.StepStatusCompleted, DurationMs: 890},
		{StepID: "check-stock.out_of_stock.notify-restock", Status: schema.StepStatusSkipped},
		{StepID: "approval", Status: schema.StepStatusSuspended},
		{StepID: "ship", Status: schema.StepStatusPending},
	}

	model, err := diagram.Build(def, states)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build error: %v\n", err)
		os.Exit(1)
	}

	outDir := filepath.Join("docs", "assets")
	os.MkdirAll(outDir, 0o755)

	// ASCII (mermaid-ascii with hand-rolled fallback)
	home, _ := os.UserHomeDir()
	binDir := filepath.Join(home, ".opcode", "bin")
	ascii := diagram.RenderASCIIAuto(model, binDir)
	os.WriteFile(filepath.Join(outDir, "diagram-ascii.txt"), []byte(ascii), 0o644)
	fmt.Println("=== ASCII (mermaid-ascii) ===")
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
		pngPath := filepath.Join(outDir, "diagram-sample.png")
		os.WriteFile(pngPath, png, 0o644)
		fmt.Printf("=== Image (PNG) ===\nWritten: %s (%d bytes)\n", pngPath, len(png))
	}
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
