package validation

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/rendis/opcode/pkg/schema"
)

// validateSemantic performs semantic analysis on the workflow definition.
// Checks: action names registered, depends_on refs valid, fallback_step refs valid,
// on_complete/on_error handlers valid, recursive sub-step validation.
func validateSemantic(def *schema.WorkflowDefinition, lookup ActionLookup) *schema.ValidationResult {
	result := &schema.ValidationResult{}

	// Build top-level step ID set.
	stepIDs := make(map[string]bool, len(def.Steps))
	for _, s := range def.Steps {
		stepIDs[s.ID] = true
	}

	for i := range def.Steps {
		path := fmt.Sprintf("steps[%d]", i)
		validateStepSemantic(&def.Steps[i], path, stepIDs, lookup, def.Timeout, result)
	}

	if def.OnComplete != nil {
		validateHandlerStep(def.OnComplete, "on_complete", lookup, result)
	}
	if def.OnError != nil {
		validateHandlerStep(def.OnError, "on_error", lookup, result)
	}

	return result
}

// validateStepSemantic checks a single top-level step.
func validateStepSemantic(step *schema.StepDefinition, path string, stepIDs map[string]bool, lookup ActionLookup, wfTimeout string, result *schema.ValidationResult) {
	stepType := step.Type
	if stepType == "" {
		stepType = schema.StepTypeAction
	}

	// Action existence.
	if stepType == schema.StepTypeAction && step.Action != "" && lookup != nil {
		if !lookup.Has(step.Action) {
			result.AddError(path+".action", schema.ErrCodeActionUnavailable,
				fmt.Sprintf("action %q not registered", step.Action))
		}
	}

	// depends_on references.
	for j, dep := range step.DependsOn {
		if !stepIDs[dep] {
			result.AddError(fmt.Sprintf("%s.depends_on[%d]", path, j),
				schema.ErrCodeValidation,
				fmt.Sprintf("references non-existent step %q", dep))
		}
	}

	// on_error.fallback_step reference.
	if step.OnError != nil && step.OnError.Strategy == schema.ErrorStrategyFallbackStep {
		if step.OnError.FallbackStep == "" {
			result.AddError(path+".on_error.fallback_step",
				schema.ErrCodeValidation, "fallback_step strategy requires a fallback_step ID")
		} else if !stepIDs[step.OnError.FallbackStep] {
			result.AddError(path+".on_error.fallback_step",
				schema.ErrCodeValidation,
				fmt.Sprintf("references non-existent step %q", step.OnError.FallbackStep))
		}
	}

	// Recursive sub-step validation for flow-control steps.
	switch stepType {
	case schema.StepTypeParallel:
		validateParallelSubSteps(step, path, lookup, result)
	case schema.StepTypeLoop:
		validateLoopSubSteps(step, path, lookup, result)
	case schema.StepTypeCondition:
		validateConditionSubSteps(step, path, lookup, result)
	}

	// Warning: high retry count.
	if step.Retry != nil && step.Retry.Max > 10 {
		result.AddWarning(path+".retry.max", schema.ErrCodeValidation,
			fmt.Sprintf("high retry count (%d) may cause excessive delays", step.Retry.Max))
	}

	// Warning: reasoning timeout exceeds step/workflow timeout.
	if stepType == schema.StepTypeReasoning && len(step.Config) > 0 {
		var cfg schema.ReasoningConfig
		if err := json.Unmarshal(step.Config, &cfg); err == nil && cfg.Timeout != "" {
			if rDur, err := time.ParseDuration(cfg.Timeout); err == nil {
				if step.Timeout != "" {
					if sDur, err := time.ParseDuration(step.Timeout); err == nil && rDur > sDur {
						result.AddWarning(path+".config.timeout", schema.ErrCodeValidation,
							fmt.Sprintf("reasoning timeout (%s) exceeds step timeout (%s); step context expires first, reasoning timeout only enforced on Resume()", cfg.Timeout, step.Timeout))
					}
				}
				if wfTimeout != "" {
					if wDur, err := time.ParseDuration(wfTimeout); err == nil && rDur > wDur {
						result.AddWarning(path+".config.timeout", schema.ErrCodeValidation,
							fmt.Sprintf("reasoning timeout (%s) exceeds workflow timeout (%s); workflow suspends before reasoning timeout fires", cfg.Timeout, wfTimeout))
					}
				}
			}
		}
	}
}

// validateHandlerStep validates on_complete/on_error workflow-level handlers.
func validateHandlerStep(step *schema.StepDefinition, path string, lookup ActionLookup, result *schema.ValidationResult) {
	stepType := step.Type
	if stepType == "" {
		stepType = schema.StepTypeAction
	}

	if stepType == schema.StepTypeAction && step.Action != "" && lookup != nil {
		if !lookup.Has(step.Action) {
			result.AddError(path+".action", schema.ErrCodeActionUnavailable,
				fmt.Sprintf("action %q not registered", step.Action))
		}
	}

	if len(step.DependsOn) > 0 {
		result.AddWarning(path+".depends_on", schema.ErrCodeValidation,
			"lifecycle handler should not have depends_on (ignored at runtime)")
	}
}

// validateParallelSubSteps validates sub-steps in parallel branches.
func validateParallelSubSteps(step *schema.StepDefinition, path string, lookup ActionLookup, result *schema.ValidationResult) {
	if len(step.Config) == 0 {
		return
	}
	var cfg schema.ParallelConfig
	if err := json.Unmarshal(step.Config, &cfg); err != nil {
		return // structural catches malformed config
	}

	for bi, branch := range cfg.Branches {
		branchIDs := buildSubStepIDSet(branch)
		for si := range branch {
			subPath := fmt.Sprintf("%s.config.branches[%d][%d]", path, bi, si)
			validateSubStep(&branch[si], subPath, branchIDs, lookup, result)
		}
	}
}

// validateLoopSubSteps validates sub-steps in loop body.
func validateLoopSubSteps(step *schema.StepDefinition, path string, lookup ActionLookup, result *schema.ValidationResult) {
	if len(step.Config) == 0 {
		return
	}
	var cfg schema.LoopConfig
	if err := json.Unmarshal(step.Config, &cfg); err != nil {
		return
	}

	bodyIDs := buildSubStepIDSet(cfg.Body)
	for si := range cfg.Body {
		subPath := fmt.Sprintf("%s.config.body[%d]", path, si)
		validateSubStep(&cfg.Body[si], subPath, bodyIDs, lookup, result)
	}
}

// validateConditionSubSteps validates sub-steps in condition branches.
func validateConditionSubSteps(step *schema.StepDefinition, path string, lookup ActionLookup, result *schema.ValidationResult) {
	if len(step.Config) == 0 {
		return
	}
	var cfg schema.ConditionConfig
	if err := json.Unmarshal(step.Config, &cfg); err != nil {
		return
	}

	for branchName, steps := range cfg.Branches {
		branchIDs := buildSubStepIDSet(steps)
		for si := range steps {
			subPath := fmt.Sprintf("%s.config.branches[%s][%d]", path, branchName, si)
			validateSubStep(&steps[si], subPath, branchIDs, lookup, result)
		}
	}
	if len(cfg.Default) > 0 {
		defaultIDs := buildSubStepIDSet(cfg.Default)
		for si := range cfg.Default {
			subPath := fmt.Sprintf("%s.config.default[%d]", path, si)
			validateSubStep(&cfg.Default[si], subPath, defaultIDs, lookup, result)
		}
	}
}

// validateSubStep validates a nested sub-step (action name, recursive flow control).
func validateSubStep(step *schema.StepDefinition, path string, localIDs map[string]bool, lookup ActionLookup, result *schema.ValidationResult) {
	stepType := step.Type
	if stepType == "" {
		stepType = schema.StepTypeAction
	}

	if stepType == schema.StepTypeAction && step.Action != "" && lookup != nil {
		if !lookup.Has(step.Action) {
			result.AddError(path+".action", schema.ErrCodeActionUnavailable,
				fmt.Sprintf("action %q not registered", step.Action))
		}
	}

	// Recursive flow control.
	switch stepType {
	case schema.StepTypeParallel:
		validateParallelSubSteps(step, path, lookup, result)
	case schema.StepTypeLoop:
		validateLoopSubSteps(step, path, lookup, result)
	case schema.StepTypeCondition:
		validateConditionSubSteps(step, path, lookup, result)
	}
}

// buildSubStepIDSet builds a set of step IDs for local reference checking.
func buildSubStepIDSet(steps []schema.StepDefinition) map[string]bool {
	ids := make(map[string]bool, len(steps))
	for _, s := range steps {
		if s.ID != "" {
			ids[s.ID] = true
		}
	}
	return ids
}
