package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rendis/opcode/internal/expressions"
	"github.com/rendis/opcode/internal/store"
	"github.com/rendis/opcode/pkg/schema"
)

// --- Condition step ---

// executeConditionStep evaluates a CEL expression and executes the matching branch.
func (e *executorImpl) executeConditionStep(ctx context.Context, run *workflowRun, workflowID, stepID string, stepDef *schema.StepDefinition, params map[string]any) (json.RawMessage, error) {
	if e.celEngine == nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "condition step %s requires CEL engine (not available)", stepID).WithStep(stepID)
	}

	var cfg schema.ConditionConfig
	if err := json.Unmarshal(stepDef.Config, &cfg); err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeValidation, "condition step %s: invalid config: %s", stepID, err.Error()).WithStep(stepID)
	}

	// Build CEL evaluation data from interpolation scope.
	scope := e.buildInterpolationScope(run, workflowID, params)
	celData := scopeToCELData(scope)

	// Evaluate condition expression.
	result, err := e.celEngine.Evaluate(ctx, cfg.Expression, celData)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "condition step %s: CEL evaluation failed: %s", stepID, err.Error()).WithStep(stepID).WithCause(err)
	}

	// Convert result to branch key.
	branchKey := celResultToString(result)

	// Emit condition evaluated event.
	evtPayload, _ := json.Marshal(map[string]any{
		"expression": cfg.Expression,
		"result":     branchKey,
	})
	_ = e.eventLog.AppendEvent(ctx, &store.Event{
		WorkflowID: workflowID,
		StepID:     stepID,
		Type:       schema.EventConditionEvaluated,
		Payload:    evtPayload,
	})

	// Look up matching branch.
	subSteps, found := cfg.Branches[branchKey]
	if !found {
		if len(cfg.Default) > 0 {
			subSteps = cfg.Default
			branchKey = "default"
		} else {
			// No matching branch — skip (return empty output).
			return json.RawMessage(`{"branch":"` + branchKey + `","skipped":true}`), nil
		}
	}

	if len(subSteps) == 0 {
		return json.RawMessage(`{"branch":"` + branchKey + `"}`), nil
	}

	// Execute the branch sub-steps.
	lastOutput, err := e.executeSubSteps(ctx, run, workflowID, stepID, branchKey, subSteps, params, nil)
	if err != nil {
		return nil, err
	}

	// Aggregate output.
	out := map[string]any{
		"branch": branchKey,
		"output": jsonOrNil(lastOutput),
	}
	outJSON, _ := json.Marshal(out)
	return outJSON, nil
}

// --- Loop step ---

// executeLoopStep runs a loop with for_each, while, or until semantics.
func (e *executorImpl) executeLoopStep(ctx context.Context, run *workflowRun, workflowID, stepID string, stepDef *schema.StepDefinition, params map[string]any) (json.RawMessage, error) {
	if e.celEngine == nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "loop step %s requires CEL engine (not available)", stepID).WithStep(stepID)
	}

	var cfg schema.LoopConfig
	if err := json.Unmarshal(stepDef.Config, &cfg); err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeValidation, "loop step %s: invalid config: %s", stepID, err.Error()).WithStep(stepID)
	}

	mode := cfg.Mode
	if mode == "" {
		mode = "for_each"
	}

	switch mode {
	case "for_each":
		return e.executeForEachLoop(ctx, run, workflowID, stepID, &cfg, params)
	case "while":
		return e.executeWhileLoop(ctx, run, workflowID, stepID, &cfg, params, false)
	case "until":
		return e.executeWhileLoop(ctx, run, workflowID, stepID, &cfg, params, true)
	default:
		return nil, schema.NewErrorf(schema.ErrCodeValidation, "loop step %s: unknown mode %q", stepID, mode).WithStep(stepID)
	}
}

func (e *executorImpl) executeForEachLoop(ctx context.Context, run *workflowRun, workflowID, stepID string, cfg *schema.LoopConfig, params map[string]any) (json.RawMessage, error) {
	// Evaluate the 'over' expression to get the iterable.
	scope := e.buildInterpolationScope(run, workflowID, params)
	celData := scopeToCELData(scope)

	overExpr := cfg.Over
	if overExpr == "" {
		return nil, schema.NewErrorf(schema.ErrCodeValidation, "loop step %s: for_each mode requires 'over' expression", stepID).WithStep(stepID)
	}

	result, err := e.celEngine.Evaluate(ctx, overExpr, celData)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "loop step %s: CEL evaluation of 'over' failed: %s", stepID, err.Error()).WithStep(stepID).WithCause(err)
	}

	// Convert result to a slice.
	items, ok := toSlice(result)
	if !ok {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "loop step %s: 'over' expression must produce a list, got %T", stepID, result).WithStep(stepID)
	}

	var results []any
	for i, item := range items {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Max iteration guard.
		if cfg.MaxIter > 0 && i >= cfg.MaxIter {
			break
		}

		// Emit loop iteration started.
		iterPayload, _ := json.Marshal(map[string]any{"iteration": i, "item": item})
		_ = e.eventLog.AppendEvent(ctx, &store.Event{
			WorkflowID: workflowID,
			StepID:     stepID,
			Type:       schema.EventLoopIterStarted,
			Payload:    iterPayload,
		})

		loopVars := &expressions.LoopScope{Item: item, Index: i}
		namespace := fmt.Sprintf("iter_%d", i)
		lastOutput, err := e.executeSubSteps(ctx, run, workflowID, stepID, namespace, cfg.Body, params, loopVars)
		if err != nil {
			return nil, err
		}

		// Emit loop iteration completed.
		_ = e.eventLog.AppendEvent(ctx, &store.Event{
			WorkflowID: workflowID,
			StepID:     stepID,
			Type:       schema.EventLoopIterCompleted,
			Payload:    iterPayload,
		})

		results = append(results, jsonOrNil(lastOutput))
	}

	// Emit loop completed.
	_ = e.eventLog.AppendEvent(ctx, &store.Event{
		WorkflowID: workflowID,
		StepID:     stepID,
		Type:       schema.EventLoopCompleted,
	})

	out := map[string]any{
		"iterations": len(results),
		"results":    results,
	}
	outJSON, _ := json.Marshal(out)
	return outJSON, nil
}

// executeWhileLoop handles both while and until loops.
// For while: evaluate condition before body, continue while true.
// For until: execute body first, then evaluate condition, stop when true.
func (e *executorImpl) executeWhileLoop(ctx context.Context, run *workflowRun, workflowID, stepID string, cfg *schema.LoopConfig, params map[string]any, isUntil bool) (json.RawMessage, error) {
	var results []any
	i := 0

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Max iteration guard.
		if cfg.MaxIter > 0 && i >= cfg.MaxIter {
			break
		}

		// For while mode: check condition BEFORE executing body.
		if !isUntil {
			shouldContinue, err := e.evaluateLoopCondition(ctx, run, workflowID, stepID, cfg.Condition, params)
			if err != nil {
				return nil, err
			}
			if !shouldContinue {
				break
			}
		}

		// Emit iteration started.
		iterPayload, _ := json.Marshal(map[string]any{"iteration": i})
		_ = e.eventLog.AppendEvent(ctx, &store.Event{
			WorkflowID: workflowID,
			StepID:     stepID,
			Type:       schema.EventLoopIterStarted,
			Payload:    iterPayload,
		})

		// Execute body.
		loopVars := &expressions.LoopScope{Item: nil, Index: i}
		namespace := fmt.Sprintf("iter_%d", i)
		lastOutput, err := e.executeSubSteps(ctx, run, workflowID, stepID, namespace, cfg.Body, params, loopVars)
		if err != nil {
			return nil, err
		}

		// Emit iteration completed.
		_ = e.eventLog.AppendEvent(ctx, &store.Event{
			WorkflowID: workflowID,
			StepID:     stepID,
			Type:       schema.EventLoopIterCompleted,
			Payload:    iterPayload,
		})

		results = append(results, jsonOrNil(lastOutput))
		i++

		// For until mode: check condition AFTER executing body.
		if isUntil {
			shouldStop, err := e.evaluateLoopCondition(ctx, run, workflowID, stepID, cfg.Condition, params)
			if err != nil {
				return nil, err
			}
			if shouldStop {
				break
			}
		}
	}

	// Emit loop completed.
	_ = e.eventLog.AppendEvent(ctx, &store.Event{
		WorkflowID: workflowID,
		StepID:     stepID,
		Type:       schema.EventLoopCompleted,
	})

	out := map[string]any{
		"iterations": len(results),
		"results":    results,
	}
	outJSON, _ := json.Marshal(out)
	return outJSON, nil
}

func (e *executorImpl) evaluateLoopCondition(ctx context.Context, run *workflowRun, workflowID, stepID, condition string, params map[string]any) (bool, error) {
	scope := e.buildInterpolationScope(run, workflowID, params)
	celData := scopeToCELData(scope)

	result, err := e.celEngine.Evaluate(ctx, condition, celData)
	if err != nil {
		return false, schema.NewErrorf(schema.ErrCodeExecution, "loop step %s: condition evaluation failed: %s", stepID, err.Error()).WithStep(stepID).WithCause(err)
	}

	b, ok := result.(bool)
	if !ok {
		return false, schema.NewErrorf(schema.ErrCodeExecution, "loop step %s: condition must evaluate to bool, got %T", stepID, result).WithStep(stepID)
	}
	return b, nil
}

// --- Parallel step ---

// executeParallelStep runs branches concurrently in all or race mode.
func (e *executorImpl) executeParallelStep(ctx context.Context, run *workflowRun, workflowID, stepID string, stepDef *schema.StepDefinition, params map[string]any) (json.RawMessage, error) {
	var cfg schema.ParallelConfig
	if err := json.Unmarshal(stepDef.Config, &cfg); err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeValidation, "parallel step %s: invalid config: %s", stepID, err.Error()).WithStep(stepID)
	}

	mode := cfg.Mode
	if mode == "" {
		mode = "all"
	}

	// Emit parallel started.
	evtPayload, _ := json.Marshal(map[string]any{
		"mode":     mode,
		"branches": len(cfg.Branches),
	})
	_ = e.eventLog.AppendEvent(ctx, &store.Event{
		WorkflowID: workflowID,
		StepID:     stepID,
		Type:       schema.EventParallelStarted,
		Payload:    evtPayload,
	})

	switch mode {
	case "all":
		return e.executeParallelAll(ctx, run, workflowID, stepID, cfg.Branches, params)
	case "race":
		return e.executeParallelRace(ctx, run, workflowID, stepID, cfg.Branches, params)
	default:
		return nil, schema.NewErrorf(schema.ErrCodeValidation, "parallel step %s: unknown mode %q", stepID, mode).WithStep(stepID)
	}
}

func (e *executorImpl) executeParallelAll(ctx context.Context, run *workflowRun, workflowID, stepID string, branches [][]schema.StepDefinition, params map[string]any) (json.RawMessage, error) {
	type branchResult struct {
		index  int
		output json.RawMessage
		err    error
	}

	results := make([]branchResult, len(branches))
	var wg sync.WaitGroup

	for i, branch := range branches {
		wg.Add(1)
		go func(idx int, steps []schema.StepDefinition) {
			defer wg.Done()
			namespace := fmt.Sprintf("branch_%d", idx)
			out, err := e.executeSubSteps(ctx, run, workflowID, stepID, namespace, steps, params, nil)
			results[idx] = branchResult{index: idx, output: out, err: err}
		}(i, branch)
	}

	wg.Wait()

	// Check for errors.
	var branchOutputs []any
	for _, r := range results {
		if r.err != nil {
			return nil, r.err
		}
		branchOutputs = append(branchOutputs, jsonOrNil(r.output))
	}

	// Emit parallel completed.
	_ = e.eventLog.AppendEvent(ctx, &store.Event{
		WorkflowID: workflowID,
		StepID:     stepID,
		Type:       schema.EventParallelCompleted,
	})

	out := map[string]any{
		"mode":     "all",
		"branches": branchOutputs,
	}
	outJSON, _ := json.Marshal(out)
	return outJSON, nil
}

func (e *executorImpl) executeParallelRace(ctx context.Context, run *workflowRun, workflowID, stepID string, branches [][]schema.StepDefinition, params map[string]any) (json.RawMessage, error) {
	type raceResult struct {
		index  int
		output json.RawMessage
		err    error
	}

	raceCtx, raceCancel := context.WithCancel(ctx)
	defer raceCancel()

	resultCh := make(chan raceResult, len(branches))

	for i, branch := range branches {
		go func(idx int, steps []schema.StepDefinition) {
			namespace := fmt.Sprintf("branch_%d", idx)
			out, err := e.executeSubSteps(raceCtx, run, workflowID, stepID, namespace, steps, params, nil)
			resultCh <- raceResult{index: idx, output: out, err: err}
		}(i, branch)
	}

	// Wait for first successful result, or collect all errors.
	var firstErr error
	errCount := 0

	for range branches {
		r := <-resultCh
		if r.err == nil {
			// Winner! Cancel all others.
			raceCancel()

			// Emit parallel completed.
			_ = e.eventLog.AppendEvent(ctx, &store.Event{
				WorkflowID: workflowID,
				StepID:     stepID,
				Type:       schema.EventParallelCompleted,
			})

			out := map[string]any{
				"mode":   "race",
				"winner": r.index,
				"output": jsonOrNil(r.output),
			}
			outJSON, _ := json.Marshal(out)
			return outJSON, nil
		}
		errCount++
		if firstErr == nil {
			firstErr = r.err
		}
	}

	// All branches failed.
	return nil, firstErr
}

// --- Wait step ---

// executeWaitStep handles duration-based or signal-based waiting.
func (e *executorImpl) executeWaitStep(ctx context.Context, run *workflowRun, workflowID, stepID string, stepDef *schema.StepDefinition, params map[string]any) (json.RawMessage, error) {
	var cfg schema.WaitConfig
	if err := json.Unmarshal(stepDef.Config, &cfg); err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeValidation, "wait step %s: invalid config: %s", stepID, err.Error()).WithStep(stepID)
	}

	// Emit wait started.
	evtPayload, _ := json.Marshal(map[string]any{
		"duration": cfg.Duration,
		"signal":   cfg.Signal,
	})
	_ = e.eventLog.AppendEvent(ctx, &store.Event{
		WorkflowID: workflowID,
		StepID:     stepID,
		Type:       schema.EventWaitStarted,
		Payload:    evtPayload,
	})

	if cfg.Duration != "" {
		return e.executeWaitDuration(ctx, workflowID, stepID, cfg.Duration)
	}

	return e.executeWaitSignal(ctx, run, workflowID, stepID, cfg.Signal)
}

func (e *executorImpl) executeWaitDuration(ctx context.Context, workflowID, stepID, duration string) (json.RawMessage, error) {
	dur, err := time.ParseDuration(duration)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeValidation, "wait step %s: invalid duration %q: %s", stepID, duration, err.Error()).WithStep(stepID)
	}

	select {
	case <-time.After(dur):
		// Emit wait completed.
		_ = e.eventLog.AppendEvent(context.Background(), &store.Event{
			WorkflowID: workflowID,
			StepID:     stepID,
			Type:       schema.EventWaitCompleted,
		})
		out := map[string]any{"waited": dur.Milliseconds()}
		outJSON, _ := json.Marshal(out)
		return outJSON, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (e *executorImpl) executeWaitSignal(ctx context.Context, run *workflowRun, workflowID, stepID, signalName string) (json.RawMessage, error) {
	// Wait for a signal on the run's signal channel that matches our step/name.
	for {
		select {
		case sig := <-run.signals:
			if sig.StepID == stepID || string(sig.Type) == signalName {
				// Emit wait completed.
				_ = e.eventLog.AppendEvent(context.Background(), &store.Event{
					WorkflowID: workflowID,
					StepID:     stepID,
					Type:       schema.EventWaitCompleted,
				})
				out := map[string]any{
					"signal":  signalName,
					"payload": sig.Payload,
				}
				outJSON, _ := json.Marshal(out)
				return outJSON, nil
			}
			// Not our signal — put it back.
			select {
			case run.signals <- sig:
			default:
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// --- Shared helper: executeSubSteps ---

// executeSubSteps executes a sequence of inline sub-steps within a flow control step.
// Sub-step IDs are namespaced as parentStepID.namespace.subStepID.
// Each sub-step gets its own state in run.stepStates and emits events with namespaced IDs.
func (e *executorImpl) executeSubSteps(ctx context.Context, run *workflowRun, workflowID, parentStepID, namespace string, steps []schema.StepDefinition, params map[string]any, loopVars *expressions.LoopScope) (json.RawMessage, error) {
	var lastOutput json.RawMessage

	for i := range steps {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		subStep := &steps[i]
		subStepID := fmt.Sprintf("%s.%s.%s", parentStepID, namespace, subStep.ID)

		// Default sub-step type to action.
		if subStep.Type == "" {
			subStep.Type = schema.StepTypeAction
		}

		// Create step state for sub-step.
		run.mu.Lock()
		run.stepStates[subStepID] = &store.StepState{
			WorkflowID: workflowID,
			StepID:     subStepID,
			Status:     schema.StepStatusPending,
		}
		run.mu.Unlock()

		// If loop vars are set, inject them into the scope for interpolation.
		// We do this by temporarily enriching params for the sub-step.
		subParams := params
		if loopVars != nil {
			subParams = e.enrichParamsWithLoopVars(run, workflowID, params, loopVars)
		}

		// Execute the sub-step through the normal step execution path.
		err := e.executeStep(ctx, run, workflowID, subStepID, subStep, subParams)
		if err != nil {
			return nil, err
		}

		// Capture the output of this sub-step.
		run.mu.Lock()
		if ss, ok := run.stepStates[subStepID]; ok && ss.Status == schema.StepStatusCompleted {
			lastOutput = ss.Output
		}
		run.mu.Unlock()
	}

	return lastOutput, nil
}

// enrichParamsWithLoopVars creates an enriched params map that includes loop variables
// so that interpolation of ${{loop.item}} and ${{loop.index}} works for sub-steps.
// This works by setting the loop scope on the interpolation context.
func (e *executorImpl) enrichParamsWithLoopVars(run *workflowRun, workflowID string, params map[string]any, loopVars *expressions.LoopScope) map[string]any {
	// Create a copy of params with loop context.
	enriched := make(map[string]any, len(params)+1)
	for k, v := range params {
		enriched[k] = v
	}
	enriched["__loop_vars__"] = loopVars
	return enriched
}

// --- Helpers ---

// scopeToCELData converts an InterpolationScope to a CEL-compatible data map.
func scopeToCELData(scope *expressions.InterpolationScope) map[string]any {
	data := map[string]any{
		"steps":   scope.Steps,
		"inputs":  scope.Inputs,
		"workflow": scope.Workflow,
		"context": scope.Context,
	}
	if scope.Loop != nil {
		data["iter"] = map[string]any{
			"item":  scope.Loop.Item,
			"index": scope.Loop.Index,
		}
	} else {
		data["iter"] = map[string]any{}
	}
	return data
}

// celResultToString converts a CEL evaluation result to a string key for branch lookup.
func celResultToString(result any) string {
	switch v := result.(type) {
	case bool:
		if v {
			return "true"
		}
		return "false"
	case string:
		return v
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%g", v)
	case uint64:
		return fmt.Sprintf("%d", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// toSlice attempts to convert a CEL result to a []any.
func toSlice(v any) ([]any, bool) {
	switch val := v.(type) {
	case []any:
		return val, true
	default:
		// Try JSON round-trip for CEL list types.
		data, err := json.Marshal(v)
		if err != nil {
			return nil, false
		}
		var result []any
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, false
		}
		return result, true
	}
}

// jsonOrNil parses json.RawMessage to any for embedding in output maps.
func jsonOrNil(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return v
}
