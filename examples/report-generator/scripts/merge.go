// merge.go - Reads JSON from stdin with sales, users, and metrics sections,
// merges them into a unified report format, and outputs the result.
//
// Input (stdin):  {"sales": {...}, "users": {...}, "metrics": {...}}
// Output (stdout): {"title": "...", "generated_at": "...", "sections": [...]}
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type Section struct {
	Name string      `json:"name"`
	Data interface{} `json:"data"`
}

type Report struct {
	Title       string    `json:"title"`
	GeneratedAt string    `json:"generated_at"`
	Sections    []Section `json:"sections"`
}

func main() {
	var input map[string]interface{}
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fmt.Fprintf(os.Stderr, "failed to decode input: %v\n", err)
		os.Exit(1)
	}

	report := Report{
		Title:       "Consolidated Report",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Sections: []Section{
			{Name: "sales", Data: input["sales"]},
			{Name: "users", Data: input["users"]},
			{Name: "metrics", Data: input["metrics"]},
		},
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintf(os.Stderr, "failed to encode report: %v\n", err)
		os.Exit(1)
	}
}
