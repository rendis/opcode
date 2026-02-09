package actions

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"hash"

	"github.com/google/uuid"
	"github.com/rendis/opcode/pkg/schema"
)

// CryptoActions returns all crypto-related actions.
func CryptoActions() []Action {
	return []Action{
		&cryptoHashAction{},
		&cryptoHMACAction{},
		&cryptoUUIDAction{},
	}
}

// hashFunc returns a new hash.Hash for the given algorithm name.
func hashFunc(algorithm string) (func() hash.Hash, error) {
	switch algorithm {
	case "sha256":
		return sha256.New, nil
	case "sha512":
		return sha512.New, nil
	case "sha384":
		return sha512.New384, nil
	case "md5":
		return md5.New, nil
	case "sha1":
		return sha1.New, nil
	default:
		return nil, schema.NewErrorf(schema.ErrCodeValidation, "unsupported hash algorithm: %s", algorithm)
	}
}

// --- crypto.hash ---

type cryptoHashAction struct{}

func (a *cryptoHashAction) Name() string { return "crypto.hash" }

func (a *cryptoHashAction) Schema() ActionSchema {
	return ActionSchema{
		Description: "Compute a cryptographic hash of the input data",
	}
}

func (a *cryptoHashAction) Validate(input map[string]any) error {
	if _, ok := input["data"].(string); !ok {
		return schema.NewError(schema.ErrCodeValidation, "crypto.hash requires 'data' string parameter")
	}
	return nil
}

func (a *cryptoHashAction) Execute(_ context.Context, input ActionInput) (*ActionOutput, error) {
	data, _ := input.Params["data"].(string)
	algorithm, _ := input.Params["algorithm"].(string)
	if algorithm == "" {
		algorithm = "sha256"
	}

	newHash, err := hashFunc(algorithm)
	if err != nil {
		return nil, err
	}

	h := newHash()
	h.Write([]byte(data))
	sum := hex.EncodeToString(h.Sum(nil))

	out, err := json.Marshal(map[string]any{
		"hash":      sum,
		"algorithm": algorithm,
	})
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "crypto.hash: marshal output: %v", err)
	}
	return &ActionOutput{Data: out}, nil
}

// --- crypto.hmac ---

type cryptoHMACAction struct{}

func (a *cryptoHMACAction) Name() string { return "crypto.hmac" }

func (a *cryptoHMACAction) Schema() ActionSchema {
	return ActionSchema{
		Description: "Compute an HMAC of the input data using the given key",
	}
}

func (a *cryptoHMACAction) Validate(input map[string]any) error {
	if _, ok := input["data"].(string); !ok {
		return schema.NewError(schema.ErrCodeValidation, "crypto.hmac requires 'data' string parameter")
	}
	if _, ok := input["key"].(string); !ok {
		return schema.NewError(schema.ErrCodeValidation, "crypto.hmac requires 'key' string parameter")
	}
	return nil
}

func (a *cryptoHMACAction) Execute(_ context.Context, input ActionInput) (*ActionOutput, error) {
	data, _ := input.Params["data"].(string)
	key, _ := input.Params["key"].(string)
	algorithm, _ := input.Params["algorithm"].(string)
	if algorithm == "" {
		algorithm = "sha256"
	}

	newHash, err := hashFunc(algorithm)
	if err != nil {
		return nil, err
	}

	mac := hmac.New(newHash, []byte(key))
	mac.Write([]byte(data))
	sum := hex.EncodeToString(mac.Sum(nil))

	out, err := json.Marshal(map[string]any{
		"hmac":      sum,
		"algorithm": algorithm,
	})
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "crypto.hmac: marshal output: %v", err)
	}
	return &ActionOutput{Data: out}, nil
}

// --- crypto.uuid ---

type cryptoUUIDAction struct{}

func (a *cryptoUUIDAction) Name() string { return "crypto.uuid" }

func (a *cryptoUUIDAction) Schema() ActionSchema {
	return ActionSchema{
		Description: "Generate a v4 UUID",
	}
}

func (a *cryptoUUIDAction) Validate(_ map[string]any) error { return nil }

func (a *cryptoUUIDAction) Execute(_ context.Context, _ ActionInput) (*ActionOutput, error) {
	id := uuid.New()
	out, err := json.Marshal(map[string]any{
		"uuid": id.String(),
	})
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeExecution, "crypto.uuid: marshal output: %v", err)
	}
	return &ActionOutput{Data: out}, nil
}
