package expressions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rendis/opcode/internal/secrets"
	"github.com/rendis/opcode/pkg/schema"
)

// InterpolationScope holds all data available for variable resolution.
type InterpolationScope struct {
	Steps    map[string]any // step ID -> output (unmarshalled)
	Inputs   map[string]any // workflow input params
	Workflow map[string]any // workflow metadata (run_id, etc.)
	Context  map[string]any // workflow context (intent, etc.)
	Loop     *LoopScope     // loop iteration variables (nil when not in a loop)
}

// LoopScope holds scoped variables for a single loop iteration.
type LoopScope struct {
	Item  any // current item value
	Index int // current iteration index (0-based)
}

// Interpolator resolves ${{...}} references in step params.
// Two-pass: first resolves non-secret variables, second resolves secrets.
type Interpolator struct {
	vault secrets.Vault
}

// NewInterpolator creates a new Interpolator with an optional Vault for secret resolution.
func NewInterpolator(vault secrets.Vault) *Interpolator {
	return &Interpolator{vault: vault}
}

// Resolve performs two-pass interpolation on raw JSON params.
// Pass 1: resolves steps.*, inputs.*, workflow.*, context.* references.
// Pass 2: resolves secrets.* references via the Vault.
// Returns the interpolated JSON bytes.
func (interp *Interpolator) Resolve(ctx context.Context, raw json.RawMessage, scope *InterpolationScope) (json.RawMessage, error) {
	if len(raw) == 0 {
		return raw, nil
	}

	s := string(raw)

	// Pass 1: non-secret variables.
	resolved, err := interp.resolvePass(ctx, s, scope, false)
	if err != nil {
		return nil, err
	}

	// Pass 2: secrets only.
	resolved, err = interp.resolvePass(ctx, resolved, scope, true)
	if err != nil {
		return nil, err
	}

	return json.RawMessage(resolved), nil
}

// resolvePass scans for ${{...}} tokens and resolves them.
// If secretPass is false, it resolves everything except secrets.* and leaves secrets untouched.
// If secretPass is true, it only resolves secrets.* references.
func (interp *Interpolator) resolvePass(ctx context.Context, input string, scope *InterpolationScope, secretPass bool) (string, error) {
	var result strings.Builder
	result.Grow(len(input))

	i := 0
	for i < len(input) {
		// Look for ${{ marker.
		idx := strings.Index(input[i:], "${{")
		if idx == -1 {
			result.WriteString(input[i:])
			break
		}

		// Write everything before the marker.
		result.WriteString(input[i : i+idx])
		start := i + idx + 3 // skip "${{".

		// Find the closing }}.
		end := strings.Index(input[start:], "}}")
		if end == -1 {
			return "", schema.NewError(schema.ErrCodeInterpolation, "unclosed ${{ expression")
		}
		end += start

		expr := strings.TrimSpace(input[start:end])

		// Reject recursive interpolation: no nested ${{ inside the expression.
		if strings.Contains(expr, "${{") {
			return "", schema.NewError(schema.ErrCodeInterpolation,
				"nested interpolation not allowed: ${{...}} cannot contain ${{")
		}

		if expr == "" {
			return "", schema.NewError(schema.ErrCodeInterpolation, "empty variable reference: ${{  }}")
		}

		isSecret := strings.HasPrefix(expr, "secrets.")

		if secretPass && !isSecret {
			// Pass 2 but not a secret — write back the original token unchanged.
			result.WriteString(input[i+idx : end+2])
			i = end + 2
			continue
		}
		if !secretPass && isSecret {
			// Pass 1 but it's a secret — write back the original token unchanged.
			result.WriteString(input[i+idx : end+2])
			i = end + 2
			continue
		}

		val, err := interp.resolveExpr(ctx, expr, scope)
		if err != nil {
			return "", err
		}

		// Embed the resolved value into the JSON string.
		result.WriteString(marshalInline(val))

		i = end + 2 // skip "}}".
	}

	return result.String(), nil
}

// resolveExpr resolves a single expression path like "steps.fetch.output.url".
func (interp *Interpolator) resolveExpr(ctx context.Context, expr string, scope *InterpolationScope) (any, error) {
	parts := strings.SplitN(expr, ".", 2)
	namespace := parts[0]

	switch namespace {
	case "steps":
		return interp.resolveSteps(expr, scope)
	case "inputs":
		return interp.resolveInputs(expr, scope)
	case "workflow":
		return interp.resolveWorkflow(expr, scope)
	case "context":
		return interp.resolveContext(expr, scope)
	case "secrets":
		return interp.resolveSecret(ctx, expr)
	case "loop":
		return interp.resolveLoop(expr, scope)
	default:
		available := []string{"steps", "inputs", "workflow", "context", "secrets", "loop"}
		return nil, schema.NewErrorf(schema.ErrCodeInterpolation,
			"unknown namespace %q in ${{%s}}; available: %s", namespace, expr, strings.Join(available, ", ")).
			WithDetails(map[string]any{"expression": expr, "available_namespaces": available})
	}
}

// resolveSteps resolves steps.<id>.output[.<field>...] references.
func (interp *Interpolator) resolveSteps(expr string, scope *InterpolationScope) (any, error) {
	// Expected: steps.<id>.output or steps.<id>.output.<field>...
	parts := strings.SplitN(expr, ".", 4) // [steps, id, output, rest...]
	if len(parts) < 3 {
		return nil, schema.NewErrorf(schema.ErrCodeInterpolation,
			"invalid step reference %q: expected steps.<id>.output[.<field>]", expr).
			WithDetails(map[string]any{"expression": expr})
	}

	stepID := parts[1]
	if parts[2] != "output" {
		return nil, schema.NewErrorf(schema.ErrCodeInterpolation,
			"invalid step reference %q: only 'output' property is supported (got %q)", expr, parts[2]).
			WithDetails(map[string]any{"expression": expr})
	}

	if scope.Steps == nil {
		return nil, interp.missingVarErr(expr, "step", stepID, scope)
	}

	output, ok := scope.Steps[stepID]
	if !ok {
		return nil, interp.missingVarErr(expr, "step", stepID, scope)
	}

	// steps.<id>.output — return the whole output.
	if len(parts) == 3 {
		return output, nil
	}

	// steps.<id>.output.<field>[.<subfield>...]
	return interp.traversePath(output, parts[3], expr)
}

// resolveInputs resolves inputs.<name> references.
func (interp *Interpolator) resolveInputs(expr string, scope *InterpolationScope) (any, error) {
	parts := strings.SplitN(expr, ".", 2)
	if len(parts) < 2 || parts[1] == "" {
		return nil, schema.NewErrorf(schema.ErrCodeInterpolation,
			"invalid input reference %q: expected inputs.<name>", expr).
			WithDetails(map[string]any{"expression": expr})
	}

	fieldPath := parts[1]
	return interp.resolveFromMap(scope.Inputs, fieldPath, expr, "input")
}

// resolveWorkflow resolves workflow.<field> references.
func (interp *Interpolator) resolveWorkflow(expr string, scope *InterpolationScope) (any, error) {
	parts := strings.SplitN(expr, ".", 2)
	if len(parts) < 2 || parts[1] == "" {
		return nil, schema.NewErrorf(schema.ErrCodeInterpolation,
			"invalid workflow reference %q: expected workflow.<field>", expr).
			WithDetails(map[string]any{"expression": expr})
	}

	fieldPath := parts[1]
	return interp.resolveFromMap(scope.Workflow, fieldPath, expr, "workflow")
}

// resolveContext resolves context.<field> references.
func (interp *Interpolator) resolveContext(expr string, scope *InterpolationScope) (any, error) {
	parts := strings.SplitN(expr, ".", 2)
	if len(parts) < 2 || parts[1] == "" {
		return nil, schema.NewErrorf(schema.ErrCodeInterpolation,
			"invalid context reference %q: expected context.<field>", expr).
			WithDetails(map[string]any{"expression": expr})
	}

	fieldPath := parts[1]
	return interp.resolveFromMap(scope.Context, fieldPath, expr, "context")
}

// resolveSecret resolves secrets.<key> via the Vault.
func (interp *Interpolator) resolveSecret(ctx context.Context, expr string) (any, error) {
	parts := strings.SplitN(expr, ".", 2)
	if len(parts) < 2 || parts[1] == "" {
		return nil, schema.NewErrorf(schema.ErrCodeInterpolation,
			"invalid secret reference %q: expected secrets.<KEY>", expr).
			WithDetails(map[string]any{"expression": expr})
	}

	key := parts[1]

	if interp.vault == nil {
		return nil, schema.NewErrorf(schema.ErrCodeInterpolation,
			"cannot resolve secret %q: no vault configured", key).
			WithDetails(map[string]any{"expression": expr})
	}

	val, err := interp.vault.Resolve(ctx, key)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeInterpolation,
			"failed to resolve secret %q: %s", key, err.Error()).
			WithDetails(map[string]any{"expression": expr}).WithCause(err)
	}

	return string(val), nil
}

// resolveLoop resolves loop.item and loop.index references.
func (interp *Interpolator) resolveLoop(expr string, scope *InterpolationScope) (any, error) {
	parts := strings.SplitN(expr, ".", 2)
	if len(parts) < 2 || parts[1] == "" {
		return nil, schema.NewErrorf(schema.ErrCodeInterpolation,
			"invalid loop reference %q: expected loop.item or loop.index", expr).
			WithDetails(map[string]any{"expression": expr})
	}

	if scope.Loop == nil {
		return nil, schema.NewErrorf(schema.ErrCodeInterpolation,
			"loop variable %q referenced outside of a loop context", expr).
			WithDetails(map[string]any{"expression": expr})
	}

	field := parts[1]
	switch {
	case field == "item":
		return scope.Loop.Item, nil
	case field == "index":
		return scope.Loop.Index, nil
	case strings.HasPrefix(field, "item."):
		// Support nested field access on loop.item: loop.item.name
		subpath := strings.TrimPrefix(field, "item.")
		return interp.traversePath(scope.Loop.Item, subpath, expr)
	default:
		return nil, schema.NewErrorf(schema.ErrCodeInterpolation,
			"unknown loop field %q in ${{%s}}; available: item, index", field, expr).
			WithDetails(map[string]any{"expression": expr, "available_fields": []string{"item", "index"}})
	}
}

// resolveFromMap resolves a dot-delimited field path from a map.
func (interp *Interpolator) resolveFromMap(data map[string]any, fieldPath, expr, namespace string) (any, error) {
	if data == nil {
		return nil, schema.NewErrorf(schema.ErrCodeInterpolation,
			"cannot resolve %q: %s scope is empty", expr, namespace).
			WithDetails(map[string]any{"expression": expr})
	}

	// Try direct key lookup first (supports keys with dots).
	if val, ok := data[fieldPath]; ok {
		return val, nil
	}

	// Traverse by splitting on dots.
	return interp.traversePath(data, fieldPath, expr)
}

// traversePath navigates into nested maps/slices using a dot-delimited path.
func (interp *Interpolator) traversePath(root any, path, expr string) (any, error) {
	segments := strings.Split(path, ".")
	current := root

	for i, seg := range segments {
		if seg == "" {
			return nil, schema.NewErrorf(schema.ErrCodeInterpolation,
				"empty segment in path %q at position %d", expr, i).
				WithDetails(map[string]any{"expression": expr})
		}

		switch v := current.(type) {
		case map[string]any:
			val, ok := v[seg]
			if !ok {
				availableKeys := mapKeys(v)
				return nil, schema.NewErrorf(schema.ErrCodeInterpolation,
					"field %q not found in %q; available: [%s]", seg, expr, strings.Join(availableKeys, ", ")).
					WithDetails(map[string]any{"expression": expr, "available_fields": availableKeys})
			}
			current = val
		default:
			return nil, schema.NewErrorf(schema.ErrCodeInterpolation,
				"cannot traverse into non-object at %q in %q (type: %T)", seg, expr, current).
				WithDetails(map[string]any{"expression": expr})
		}
	}

	return current, nil
}

// missingVarErr builds an error for missing step references with available steps listed.
func (interp *Interpolator) missingVarErr(expr, kind, id string, scope *InterpolationScope) *schema.OpcodeError {
	available := mapKeys(scope.Steps)
	return schema.NewErrorf(schema.ErrCodeInterpolation,
		"%s %q not found in ${{%s}}; available steps: [%s]", kind, id, expr, strings.Join(available, ", ")).
		WithDetails(map[string]any{"expression": expr, "available_steps": available})
}

// marshalInline converts a resolved value into its inline JSON representation.
// Strings are embedded without extra quotes when the reference is the entire JSON value
// (e.g. "${{inputs.url}}" as a field value). For embedded references within strings,
// the value is stringified. For complex types (maps, slices), JSON-encode inline.
func marshalInline(val any) string {
	switch v := val.(type) {
	case string:
		return v
	case nil:
		return "null"
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		return fmt.Sprintf("%v", v)
	case int:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case json.RawMessage:
		return string(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}

// mapKeys returns sorted keys from a map[string]any.
func mapKeys(m map[string]any) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple insertion sort for small slices.
	for i := 1; i < len(keys); i++ {
		key := keys[i]
		j := i - 1
		for j >= 0 && keys[j] > key {
			keys[j+1] = keys[j]
			j--
		}
		keys[j+1] = key
	}
	return keys
}

// HasInterpolation checks if a JSON blob contains any ${{...}} references.
func HasInterpolation(raw json.RawMessage) bool {
	return strings.Contains(string(raw), "${{")
}

// DetectCircularRefs checks for circular references in step params.
// A circular reference occurs when step A's params reference step B's output
// and step B's params reference step A's output (at the same DAG level).
// This is called before execution with the set of step definitions at one level.
func DetectCircularRefs(steps map[string]*schema.StepDefinition) error {
	// Build a dependency graph from ${{steps.<id>.output}} references in params.
	refs := make(map[string]map[string]bool) // stepID -> set of referenced stepIDs

	for id, step := range steps {
		if len(step.Params) == 0 {
			continue
		}
		s := string(step.Params)
		extracted := extractStepRefs(s)
		if len(extracted) > 0 {
			refs[id] = extracted
		}
	}

	// Detect cycles using DFS.
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(refs))

	var visit func(id string) error
	visit = func(id string) error {
		color[id] = gray
		for dep := range refs[id] {
			switch color[dep] {
			case gray:
				return schema.NewErrorf(schema.ErrCodeInterpolation,
					"circular variable reference detected: %s -> %s", id, dep)
			case white:
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		color[id] = black
		return nil
	}

	for id := range refs {
		if color[id] == white {
			if err := visit(id); err != nil {
				return err
			}
		}
	}

	return nil
}

// extractStepRefs finds all step IDs referenced via ${{steps.<id>.output...}} in a string.
func extractStepRefs(s string) map[string]bool {
	refs := make(map[string]bool)
	for {
		idx := strings.Index(s, "${{steps.")
		if idx == -1 {
			break
		}
		// Skip past "${{steps."
		rest := s[idx+len("${{steps."):]
		dotIdx := strings.IndexByte(rest, '.')
		closeIdx := strings.Index(rest, "}}")
		if closeIdx == -1 {
			break
		}
		var stepID string
		if dotIdx != -1 && dotIdx < closeIdx {
			stepID = rest[:dotIdx]
		} else {
			stepID = rest[:closeIdx]
		}
		stepID = strings.TrimSpace(stepID)
		if stepID != "" {
			refs[stepID] = true
		}
		s = rest[closeIdx+2:]
	}
	return refs
}
