package actions

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/rendis/opcode/pkg/schema"
)

// Registry is the concrete thread-safe ActionRegistry implementation.
type Registry struct {
	mu      sync.RWMutex
	actions map[string]Action
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		actions: make(map[string]Action),
	}
}

// Register adds an action to the registry. Returns error on duplicate name.
func (r *Registry) Register(action Action) error {
	if action == nil {
		return schema.NewError(schema.ErrCodeValidation, "action is nil")
	}
	name := action.Name()
	if name == "" {
		return schema.NewError(schema.ErrCodeValidation, "action name is empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.actions[name]; exists {
		return schema.NewErrorf(schema.ErrCodeConflict, "action %q already registered", name)
	}

	r.actions[name] = action
	return nil
}

// Get retrieves an action by name.
func (r *Registry) Get(name string) (Action, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	action, ok := r.actions[name]
	if !ok {
		return nil, schema.NewErrorf(schema.ErrCodeActionUnavailable, "action %q not registered", name)
	}
	return action, nil
}

// List returns info for all registered actions, sorted by name.
func (r *Registry) List() []ActionInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	infos := make([]ActionInfo, 0, len(r.actions))
	for _, a := range r.actions {
		s := a.Schema()
		infos = append(infos, ActionInfo{
			Name:        a.Name(),
			Description: s.Description,
		})
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})
	return infos
}

// RegisterPlugin bulk-registers actions under a prefixed namespace.
// Each action name becomes "prefix.originalName" (e.g. "github.create_issue").
func (r *Registry) RegisterPlugin(prefix string, acts []Action) (int, error) {
	if prefix == "" {
		return 0, schema.NewError(schema.ErrCodeValidation, "plugin prefix is empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	registered := 0
	for _, a := range acts {
		prefixed := fmt.Sprintf("%s.%s", prefix, a.Name())
		if _, exists := r.actions[prefixed]; exists {
			return registered, schema.NewErrorf(schema.ErrCodeConflict, "plugin action %q already registered", prefixed)
		}
		r.actions[prefixed] = &prefixedAction{inner: a, name: prefixed}
		registered++
	}
	return registered, nil
}

// Has checks if an action is registered.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.actions[name]
	return ok
}

// Count returns the number of registered actions.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.actions)
}

// prefixedAction wraps a plugin action with a prefixed name.
type prefixedAction struct {
	inner Action
	name  string
}

func (p *prefixedAction) Name() string                { return p.name }
func (p *prefixedAction) Schema() ActionSchema         { return p.inner.Schema() }
func (p *prefixedAction) Validate(input map[string]any) error { return p.inner.Validate(input) }

func (p *prefixedAction) Execute(ctx context.Context, input ActionInput) (*ActionOutput, error) {
	return p.inner.Execute(ctx, input)
}
