package actions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/rendis/opcode/internal/isolation"
	"github.com/rendis/opcode/pkg/schema"
)

const defaultMaxReadSize = 50 * 1024 * 1024 // 50MB

// FSConfig configures the filesystem actions.
type FSConfig struct {
	Limits      isolation.ResourceLimits
	MaxReadSize int64
}

// FSActions returns all filesystem-related actions.
func FSActions(cfg FSConfig) []Action {
	if cfg.MaxReadSize <= 0 {
		cfg.MaxReadSize = defaultMaxReadSize
	}
	return []Action{
		&fsReadAction{cfg: cfg},
		&fsWriteAction{cfg: cfg},
		&fsAppendAction{cfg: cfg},
		&fsDeleteAction{cfg: cfg},
		&fsListAction{cfg: cfg},
		&fsStatAction{cfg: cfg},
		&fsCopyAction{cfg: cfg},
		&fsMoveAction{cfg: cfg},
	}
}

// fileInfoMap builds a standard stat result map from a path and fs.FileInfo.
func fileInfoMap(path string, info fs.FileInfo) map[string]any {
	return map[string]any{
		"path":        path,
		"size":        info.Size(),
		"modified_at": info.ModTime().UTC().Format(time.RFC3339),
		"is_dir":      info.IsDir(),
		"permissions": fmt.Sprintf("%04o", info.Mode().Perm()),
	}
}

// marshalOutput marshals a result map into an ActionOutput.
func marshalOutput(result map[string]any) (*ActionOutput, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "failed to marshal output: %v", err)
	}
	return &ActionOutput{Data: json.RawMessage(data)}, nil
}

// absPath resolves a path to absolute.
func absPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", schema.NewErrorf(schema.ErrCodeValidation, "invalid path %q: %v", path, err)
	}
	return abs, nil
}

// isBinary checks if data contains null bytes (binary detection heuristic).
func isBinary(data []byte) bool {
	check := data
	if len(check) > 8192 {
		check = check[:8192]
	}
	for _, b := range check {
		if b == 0 {
			return true
		}
	}
	return false
}

// --- JSON Schemas ---

const fsReadInputSchema = `{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "encoding": {"type": "string", "enum": ["text","base64","auto"], "default": "auto"}
  },
  "required": ["path"]
}`

const fsReadOutputSchema = `{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "content": {"type": "string"},
    "encoding": {"type": "string"},
    "size": {"type": "integer"}
  }
}`

const fsWriteInputSchema = `{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "content": {"type": "string"},
    "create_dirs": {"type": "boolean", "default": false},
    "mode": {"type": "integer", "default": 420}
  },
  "required": ["path", "content"]
}`

const fsWriteOutputSchema = `{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "size": {"type": "integer"}
  }
}`

const fsAppendInputSchema = `{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "content": {"type": "string"}
  },
  "required": ["path", "content"]
}`

const fsAppendOutputSchema = `{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "size": {"type": "integer"}
  }
}`

const fsDeleteInputSchema = `{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "recursive": {"type": "boolean", "default": false}
  },
  "required": ["path"]
}`

const fsDeleteOutputSchema = `{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "deleted": {"type": "boolean"}
  }
}`

const fsListInputSchema = `{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "pattern": {"type": "string"},
    "recursive": {"type": "boolean", "default": false}
  },
  "required": ["path"]
}`

const fsListOutputSchema = `{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "entries": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "name": {"type": "string"},
          "path": {"type": "string"},
          "size": {"type": "integer"},
          "modified_at": {"type": "string"},
          "is_dir": {"type": "boolean"}
        }
      }
    }
  }
}`

const fsStatInputSchema = `{
  "type": "object",
  "properties": {
    "path": {"type": "string"}
  },
  "required": ["path"]
}`

const fsStatOutputSchema = `{
  "type": "object",
  "properties": {
    "path": {"type": "string"},
    "size": {"type": "integer"},
    "modified_at": {"type": "string"},
    "is_dir": {"type": "boolean"},
    "permissions": {"type": "string"}
  }
}`

const fsCopyInputSchema = `{
  "type": "object",
  "properties": {
    "src": {"type": "string"},
    "dst": {"type": "string"},
    "create_dirs": {"type": "boolean", "default": false}
  },
  "required": ["src", "dst"]
}`

const fsCopyOutputSchema = `{
  "type": "object",
  "properties": {
    "src": {"type": "string"},
    "dst": {"type": "string"},
    "size": {"type": "integer"},
    "is_dir": {"type": "boolean"}
  }
}`

const fsMoveInputSchema = `{
  "type": "object",
  "properties": {
    "src": {"type": "string"},
    "dst": {"type": "string"},
    "create_dirs": {"type": "boolean", "default": false}
  },
  "required": ["src", "dst"]
}`

const fsMoveOutputSchema = `{
  "type": "object",
  "properties": {
    "src": {"type": "string"},
    "dst": {"type": "string"},
    "size": {"type": "integer"},
    "is_dir": {"type": "boolean"}
  }
}`

// --- fs.read ---

type fsReadAction struct{ cfg FSConfig }

func (a *fsReadAction) Name() string { return "fs.read" }

func (a *fsReadAction) Schema() ActionSchema {
	return ActionSchema{
		Description:  "Read the contents of a file",
		InputSchema:  json.RawMessage(fsReadInputSchema),
		OutputSchema: json.RawMessage(fsReadOutputSchema),
	}
}

func (a *fsReadAction) Validate(input map[string]any) error {
	if stringParam(input, "path", "") == "" {
		return schema.NewError(schema.ErrCodeValidation, "fs.read: missing required param 'path'")
	}
	enc := stringParam(input, "encoding", "auto")
	if enc != "text" && enc != "base64" && enc != "auto" {
		return schema.NewErrorf(schema.ErrCodeValidation, "fs.read: invalid encoding %q", enc)
	}
	return nil
}

func (a *fsReadAction) Execute(_ context.Context, input ActionInput) (*ActionOutput, error) {
	params := input.Params
	if params == nil {
		params = map[string]any{}
	}

	if err := a.Validate(params); err != nil {
		return nil, err
	}

	path, err := absPath(stringParam(params, "path", ""))
	if err != nil {
		return nil, err
	}

	if err := a.cfg.Limits.ValidatePath(path, isolation.PathAccessRead); err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.read: %v", err).WithCause(err)
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, a.cfg.MaxReadSize))
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.read: failed to read file: %v", err).WithCause(err)
	}

	enc := stringParam(params, "encoding", "auto")
	if enc == "auto" {
		if isBinary(data) {
			enc = "base64"
		} else {
			enc = "text"
		}
	}

	var content string
	if enc == "base64" {
		content = base64.StdEncoding.EncodeToString(data)
	} else {
		content = string(data)
	}

	return marshalOutput(map[string]any{
		"path":     path,
		"content":  content,
		"encoding": enc,
		"size":     len(data),
	})
}

// --- fs.write ---

type fsWriteAction struct{ cfg FSConfig }

func (a *fsWriteAction) Name() string { return "fs.write" }

func (a *fsWriteAction) Schema() ActionSchema {
	return ActionSchema{
		Description:  "Write content to a file, creating or overwriting it",
		InputSchema:  json.RawMessage(fsWriteInputSchema),
		OutputSchema: json.RawMessage(fsWriteOutputSchema),
	}
}

func (a *fsWriteAction) Validate(input map[string]any) error {
	if stringParam(input, "path", "") == "" {
		return schema.NewError(schema.ErrCodeValidation, "fs.write: missing required param 'path'")
	}
	if _, ok := input["content"]; !ok {
		return schema.NewError(schema.ErrCodeValidation, "fs.write: missing required param 'content'")
	}
	return nil
}

func (a *fsWriteAction) Execute(_ context.Context, input ActionInput) (*ActionOutput, error) {
	params := input.Params
	if params == nil {
		params = map[string]any{}
	}

	if err := a.Validate(params); err != nil {
		return nil, err
	}

	path, err := absPath(stringParam(params, "path", ""))
	if err != nil {
		return nil, err
	}

	if err := a.cfg.Limits.ValidatePath(path, isolation.PathAccessWrite); err != nil {
		return nil, err
	}

	content := stringParam(params, "content", "")
	createDirs := boolParam(params, "create_dirs", false)
	fileMode := os.FileMode(intParam(params, "mode", 0644))

	if createDirs {
		dir := filepath.Dir(path)
		if err := a.cfg.Limits.ValidatePath(dir, isolation.PathAccessWrite); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.write: failed to create directories: %v", err).WithCause(err)
		}
	}

	data := []byte(content)
	if err := os.WriteFile(path, data, fileMode); err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.write: %v", err).WithCause(err)
	}

	return marshalOutput(map[string]any{
		"path": path,
		"size": len(data),
	})
}

// --- fs.append ---

type fsAppendAction struct{ cfg FSConfig }

func (a *fsAppendAction) Name() string { return "fs.append" }

func (a *fsAppendAction) Schema() ActionSchema {
	return ActionSchema{
		Description:  "Append content to a file, creating it if it does not exist",
		InputSchema:  json.RawMessage(fsAppendInputSchema),
		OutputSchema: json.RawMessage(fsAppendOutputSchema),
	}
}

func (a *fsAppendAction) Validate(input map[string]any) error {
	if stringParam(input, "path", "") == "" {
		return schema.NewError(schema.ErrCodeValidation, "fs.append: missing required param 'path'")
	}
	if _, ok := input["content"]; !ok {
		return schema.NewError(schema.ErrCodeValidation, "fs.append: missing required param 'content'")
	}
	return nil
}

func (a *fsAppendAction) Execute(_ context.Context, input ActionInput) (*ActionOutput, error) {
	params := input.Params
	if params == nil {
		params = map[string]any{}
	}

	if err := a.Validate(params); err != nil {
		return nil, err
	}

	path, err := absPath(stringParam(params, "path", ""))
	if err != nil {
		return nil, err
	}

	if err := a.cfg.Limits.ValidatePath(path, isolation.PathAccessWrite); err != nil {
		return nil, err
	}

	content := stringParam(params, "content", "")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.append: %v", err).WithCause(err)
	}

	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.append: failed to write: %v", err).WithCause(err)
	}
	f.Close()

	info, err := os.Stat(path)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.append: failed to stat after write: %v", err).WithCause(err)
	}

	return marshalOutput(map[string]any{
		"path": path,
		"size": info.Size(),
	})
}

// --- fs.delete ---

type fsDeleteAction struct{ cfg FSConfig }

func (a *fsDeleteAction) Name() string { return "fs.delete" }

func (a *fsDeleteAction) Schema() ActionSchema {
	return ActionSchema{
		Description:  "Delete a file or directory",
		InputSchema:  json.RawMessage(fsDeleteInputSchema),
		OutputSchema: json.RawMessage(fsDeleteOutputSchema),
	}
}

func (a *fsDeleteAction) Validate(input map[string]any) error {
	if stringParam(input, "path", "") == "" {
		return schema.NewError(schema.ErrCodeValidation, "fs.delete: missing required param 'path'")
	}
	return nil
}

func (a *fsDeleteAction) Execute(_ context.Context, input ActionInput) (*ActionOutput, error) {
	params := input.Params
	if params == nil {
		params = map[string]any{}
	}

	if err := a.Validate(params); err != nil {
		return nil, err
	}

	path, err := absPath(stringParam(params, "path", ""))
	if err != nil {
		return nil, err
	}

	if err := a.cfg.Limits.ValidatePath(path, isolation.PathAccessWrite); err != nil {
		return nil, err
	}

	recursive := boolParam(params, "recursive", false)

	if recursive {
		if err := os.RemoveAll(path); err != nil {
			return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.delete: %v", err).WithCause(err)
		}
	} else {
		if err := os.Remove(path); err != nil {
			return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.delete: %v", err).WithCause(err)
		}
	}

	return marshalOutput(map[string]any{
		"path":    path,
		"deleted": true,
	})
}

// --- fs.list ---

type fsListAction struct{ cfg FSConfig }

func (a *fsListAction) Name() string { return "fs.list" }

func (a *fsListAction) Schema() ActionSchema {
	return ActionSchema{
		Description:  "List files and directories in a path, optionally filtered by glob pattern",
		InputSchema:  json.RawMessage(fsListInputSchema),
		OutputSchema: json.RawMessage(fsListOutputSchema),
	}
}

func (a *fsListAction) Validate(input map[string]any) error {
	if stringParam(input, "path", "") == "" {
		return schema.NewError(schema.ErrCodeValidation, "fs.list: missing required param 'path'")
	}
	return nil
}

func (a *fsListAction) Execute(_ context.Context, input ActionInput) (*ActionOutput, error) {
	params := input.Params
	if params == nil {
		params = map[string]any{}
	}

	if err := a.Validate(params); err != nil {
		return nil, err
	}

	path, err := absPath(stringParam(params, "path", ""))
	if err != nil {
		return nil, err
	}

	if err := a.cfg.Limits.ValidatePath(path, isolation.PathAccessRead); err != nil {
		return nil, err
	}

	pattern := stringParam(params, "pattern", "")
	recursive := boolParam(params, "recursive", false)

	var entries []map[string]any

	if recursive {
		err = filepath.WalkDir(path, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			// Skip the root directory itself.
			if p == path {
				return nil
			}
			if pattern != "" {
				matched, matchErr := filepath.Match(pattern, d.Name())
				if matchErr != nil {
					return schema.NewErrorf(schema.ErrCodeValidation, "fs.list: invalid pattern %q: %v", pattern, matchErr)
				}
				if !matched {
					return nil
				}
			}
			info, infoErr := d.Info()
			if infoErr != nil {
				return infoErr
			}
			entries = append(entries, map[string]any{
				"name":        d.Name(),
				"path":        p,
				"size":        info.Size(),
				"modified_at": info.ModTime().UTC().Format(time.RFC3339),
				"is_dir":      d.IsDir(),
			})
			return nil
		})
		if err != nil {
			return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.list: %v", err).WithCause(err)
		}
	} else if pattern != "" {
		matches, globErr := filepath.Glob(filepath.Join(path, pattern))
		if globErr != nil {
			return nil, schema.NewErrorf(schema.ErrCodeValidation, "fs.list: invalid pattern %q: %v", pattern, globErr)
		}
		for _, m := range matches {
			info, infoErr := os.Stat(m)
			if infoErr != nil {
				continue
			}
			entries = append(entries, map[string]any{
				"name":        filepath.Base(m),
				"path":        m,
				"size":        info.Size(),
				"modified_at": info.ModTime().UTC().Format(time.RFC3339),
				"is_dir":      info.IsDir(),
			})
		}
	} else {
		dirEntries, readErr := os.ReadDir(path)
		if readErr != nil {
			return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.list: %v", readErr).WithCause(readErr)
		}
		for _, d := range dirEntries {
			info, infoErr := d.Info()
			if infoErr != nil {
				continue
			}
			entries = append(entries, map[string]any{
				"name":        d.Name(),
				"path":        filepath.Join(path, d.Name()),
				"size":        info.Size(),
				"modified_at": info.ModTime().UTC().Format(time.RFC3339),
				"is_dir":      d.IsDir(),
			})
		}
	}

	if entries == nil {
		entries = []map[string]any{}
	}

	return marshalOutput(map[string]any{
		"path":    path,
		"entries": entries,
	})
}

// --- fs.stat ---

type fsStatAction struct{ cfg FSConfig }

func (a *fsStatAction) Name() string { return "fs.stat" }

func (a *fsStatAction) Schema() ActionSchema {
	return ActionSchema{
		Description:  "Get file or directory metadata",
		InputSchema:  json.RawMessage(fsStatInputSchema),
		OutputSchema: json.RawMessage(fsStatOutputSchema),
	}
}

func (a *fsStatAction) Validate(input map[string]any) error {
	if stringParam(input, "path", "") == "" {
		return schema.NewError(schema.ErrCodeValidation, "fs.stat: missing required param 'path'")
	}
	return nil
}

func (a *fsStatAction) Execute(_ context.Context, input ActionInput) (*ActionOutput, error) {
	params := input.Params
	if params == nil {
		params = map[string]any{}
	}

	if err := a.Validate(params); err != nil {
		return nil, err
	}

	path, err := absPath(stringParam(params, "path", ""))
	if err != nil {
		return nil, err
	}

	if err := a.cfg.Limits.ValidatePath(path, isolation.PathAccessRead); err != nil {
		return nil, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.stat: %v", err).WithCause(err)
	}

	return marshalOutput(fileInfoMap(path, info))
}

// --- fs.copy ---

type fsCopyAction struct{ cfg FSConfig }

func (a *fsCopyAction) Name() string { return "fs.copy" }

func (a *fsCopyAction) Schema() ActionSchema {
	return ActionSchema{
		Description:  "Copy a file or directory to a new location",
		InputSchema:  json.RawMessage(fsCopyInputSchema),
		OutputSchema: json.RawMessage(fsCopyOutputSchema),
	}
}

func (a *fsCopyAction) Validate(input map[string]any) error {
	if stringParam(input, "src", "") == "" {
		return schema.NewError(schema.ErrCodeValidation, "fs.copy: missing required param 'src'")
	}
	if stringParam(input, "dst", "") == "" {
		return schema.NewError(schema.ErrCodeValidation, "fs.copy: missing required param 'dst'")
	}
	return nil
}

func (a *fsCopyAction) Execute(_ context.Context, input ActionInput) (*ActionOutput, error) {
	params := input.Params
	if params == nil {
		params = map[string]any{}
	}

	if err := a.Validate(params); err != nil {
		return nil, err
	}

	src, err := absPath(stringParam(params, "src", ""))
	if err != nil {
		return nil, err
	}
	dst, err := absPath(stringParam(params, "dst", ""))
	if err != nil {
		return nil, err
	}

	if err := a.cfg.Limits.ValidatePath(src, isolation.PathAccessRead); err != nil {
		return nil, err
	}
	if err := a.cfg.Limits.ValidatePath(dst, isolation.PathAccessWrite); err != nil {
		return nil, err
	}

	createDirs := boolParam(params, "create_dirs", false)
	if createDirs {
		dstDir := filepath.Dir(dst)
		if err := a.cfg.Limits.ValidatePath(dstDir, isolation.PathAccessWrite); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(dstDir, 0755); err != nil {
			return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.copy: failed to create directories: %v", err).WithCause(err)
		}
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.copy: %v", err).WithCause(err)
	}

	var totalSize int64
	if srcInfo.IsDir() {
		totalSize, err = copyDir(src, dst)
		if err != nil {
			return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.copy: %v", err).WithCause(err)
		}
	} else {
		totalSize, err = copyFile(src, dst, srcInfo.Mode())
		if err != nil {
			return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.copy: %v", err).WithCause(err)
		}
	}

	return marshalOutput(map[string]any{
		"src":    src,
		"dst":    dst,
		"size":   totalSize,
		"is_dir": srcInfo.IsDir(),
	})
}

// copyFile copies a single file from src to dst, preserving the given file mode.
func copyFile(src, dst string, mode os.FileMode) (int64, error) {
	srcFile, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return 0, err
	}
	defer dstFile.Close()

	n, err := io.Copy(dstFile, srcFile)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// copyDir recursively copies a directory from src to dst.
func copyDir(src, dst string) (int64, error) {
	var totalSize int64

	return totalSize, filepath.WalkDir(src, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		n, err := copyFile(p, target, info.Mode())
		if err != nil {
			return err
		}
		totalSize += n
		return nil
	})
}

// --- fs.move ---

type fsMoveAction struct{ cfg FSConfig }

func (a *fsMoveAction) Name() string { return "fs.move" }

func (a *fsMoveAction) Schema() ActionSchema {
	return ActionSchema{
		Description:  "Move or rename a file or directory",
		InputSchema:  json.RawMessage(fsMoveInputSchema),
		OutputSchema: json.RawMessage(fsMoveOutputSchema),
	}
}

func (a *fsMoveAction) Validate(input map[string]any) error {
	if stringParam(input, "src", "") == "" {
		return schema.NewError(schema.ErrCodeValidation, "fs.move: missing required param 'src'")
	}
	if stringParam(input, "dst", "") == "" {
		return schema.NewError(schema.ErrCodeValidation, "fs.move: missing required param 'dst'")
	}
	return nil
}

func (a *fsMoveAction) Execute(_ context.Context, input ActionInput) (*ActionOutput, error) {
	params := input.Params
	if params == nil {
		params = map[string]any{}
	}

	if err := a.Validate(params); err != nil {
		return nil, err
	}

	src, err := absPath(stringParam(params, "src", ""))
	if err != nil {
		return nil, err
	}
	dst, err := absPath(stringParam(params, "dst", ""))
	if err != nil {
		return nil, err
	}

	if err := a.cfg.Limits.ValidatePath(src, isolation.PathAccessWrite); err != nil {
		return nil, err
	}
	if err := a.cfg.Limits.ValidatePath(dst, isolation.PathAccessWrite); err != nil {
		return nil, err
	}

	createDirs := boolParam(params, "create_dirs", false)
	if createDirs {
		dstDir := filepath.Dir(dst)
		if err := a.cfg.Limits.ValidatePath(dstDir, isolation.PathAccessWrite); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(dstDir, 0755); err != nil {
			return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.move: failed to create directories: %v", err).WithCause(err)
		}
	}

	// Stat before move to capture size and type.
	srcInfo, err := os.Stat(src)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.move: %v", err).WithCause(err)
	}

	var totalSize int64
	isDir := srcInfo.IsDir()

	// Try rename first (fast, same filesystem).
	if err := os.Rename(src, dst); err != nil {
		// Cross-device fallback: copy + delete.
		if isDir {
			totalSize, err = copyDir(src, dst)
			if err != nil {
				return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.move: copy fallback: %v", err).WithCause(err)
			}
			if err := os.RemoveAll(src); err != nil {
				return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.move: failed to remove source after copy: %v", err).WithCause(err)
			}
		} else {
			totalSize, err = copyFile(src, dst, srcInfo.Mode())
			if err != nil {
				return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.move: copy fallback: %v", err).WithCause(err)
			}
			if err := os.Remove(src); err != nil {
				return nil, schema.NewErrorf(schema.ErrCodeExecution, "fs.move: failed to remove source after copy: %v", err).WithCause(err)
			}
		}
	} else {
		totalSize = srcInfo.Size()
	}

	return marshalOutput(map[string]any{
		"src":    src,
		"dst":    dst,
		"size":   totalSize,
		"is_dir": isDir,
	})
}
