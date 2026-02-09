package actions

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAction is a minimal Action for registry tests.
type stubAction struct {
	name string
	desc string
}

func (s *stubAction) Name() string { return s.name }
func (s *stubAction) Schema() ActionSchema {
	return ActionSchema{Description: s.desc}
}
func (s *stubAction) Execute(_ context.Context, _ ActionInput) (*ActionOutput, error) {
	return &ActionOutput{Data: json.RawMessage(`{"ok":true}`)}, nil
}
func (s *stubAction) Validate(_ map[string]any) error { return nil }

func TestRegistry_Register_Success(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(&stubAction{name: "test.action", desc: "A test action"})
	require.NoError(t, err)
	assert.Equal(t, 1, reg.Count())
	assert.True(t, reg.Has("test.action"))
}

func TestRegistry_Register_Duplicate(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(&stubAction{name: "dup"}))

	err := reg.Register(&stubAction{name: "dup"})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeConflict, opErr.Code)
}

func TestRegistry_Register_Nil(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(nil)
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

func TestRegistry_Register_EmptyName(t *testing.T) {
	reg := NewRegistry()
	err := reg.Register(&stubAction{name: ""})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

func TestRegistry_Get_Success(t *testing.T) {
	reg := NewRegistry()
	original := &stubAction{name: "fetch"}
	require.NoError(t, reg.Register(original))

	got, err := reg.Get("fetch")
	require.NoError(t, err)
	assert.Equal(t, "fetch", got.Name())
}

func TestRegistry_Get_NotFound(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.Get("missing")
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeActionUnavailable, opErr.Code)
}

func TestRegistry_List_Sorted(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(&stubAction{name: "z.action", desc: "last"}))
	require.NoError(t, reg.Register(&stubAction{name: "a.action", desc: "first"}))
	require.NoError(t, reg.Register(&stubAction{name: "m.action", desc: "middle"}))

	infos := reg.List()
	require.Len(t, infos, 3)
	assert.Equal(t, "a.action", infos[0].Name)
	assert.Equal(t, "first", infos[0].Description)
	assert.Equal(t, "m.action", infos[1].Name)
	assert.Equal(t, "z.action", infos[2].Name)
}

func TestRegistry_List_Empty(t *testing.T) {
	reg := NewRegistry()
	infos := reg.List()
	assert.Empty(t, infos)
}

func TestRegistry_RegisterPlugin(t *testing.T) {
	reg := NewRegistry()
	pluginActions := []Action{
		&stubAction{name: "create_issue", desc: "Create a GitHub issue"},
		&stubAction{name: "list_repos", desc: "List repositories"},
	}

	n, err := reg.RegisterPlugin("github", pluginActions)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Equal(t, 2, reg.Count())
	assert.True(t, reg.Has("github.create_issue"))
	assert.True(t, reg.Has("github.list_repos"))

	got, err := reg.Get("github.create_issue")
	require.NoError(t, err)
	assert.Equal(t, "github.create_issue", got.Name())
}

func TestRegistry_RegisterPlugin_EmptyPrefix(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.RegisterPlugin("", nil)
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

func TestRegistry_RegisterPlugin_Conflict(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.Register(&stubAction{name: "gh.create_issue"}))

	_, err := reg.RegisterPlugin("gh", []Action{
		&stubAction{name: "create_issue"},
	})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeConflict, opErr.Code)
}

func TestRegistry_Has_False(t *testing.T) {
	reg := NewRegistry()
	assert.False(t, reg.Has("nonexistent"))
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	reg := NewRegistry()
	const n = 100

	var wg sync.WaitGroup
	wg.Add(n * 3)

	// Concurrent registers.
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			name := "concurrent." + string(rune('a'+i%26)) + string(rune('0'+i/26))
			_ = reg.Register(&stubAction{name: name})
		}(i)
	}

	// Concurrent gets.
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _ = reg.Get("concurrent.a0")
		}()
	}

	// Concurrent lists.
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_ = reg.List()
		}()
	}

	wg.Wait()
	assert.True(t, reg.Count() > 0)
}
