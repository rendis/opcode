package actions

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"

	"github.com/rendis/opcode/pkg/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findCryptoAction(name string) Action {
	for _, a := range CryptoActions() {
		if a.Name() == name {
			return a
		}
	}
	return nil
}

func execCrypto(t *testing.T, name string, params map[string]any) (map[string]any, error) {
	t.Helper()
	a := findCryptoAction(name)
	require.NotNil(t, a, "action %s not found", name)
	out, err := a.Execute(context.Background(), ActionInput{Params: params})
	if err != nil {
		return nil, err
	}
	var result map[string]any
	require.NoError(t, json.Unmarshal(out.Data, &result))
	return result, nil
}

func TestCryptoHash_SHA256(t *testing.T) {
	result, err := execCrypto(t, "crypto.hash", map[string]any{
		"algorithm": "sha256",
		"data":      "hello",
	})
	require.NoError(t, err)
	assert.Equal(t, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", result["hash"])
	assert.Equal(t, "sha256", result["algorithm"])
}

func TestCryptoHash_SHA512(t *testing.T) {
	result, err := execCrypto(t, "crypto.hash", map[string]any{
		"algorithm": "sha512",
		"data":      "hello",
	})
	require.NoError(t, err)
	assert.Equal(t, "9b71d224bd62f3785d96d46ad3ea3d73319bfbc2890caadae2dff72519673ca72323c3d99ba5c11d7c7acc6e14b8c5da0c4663475c2e5c3adef46f73bcdec043", result["hash"])
	assert.Equal(t, "sha512", result["algorithm"])
}

func TestCryptoHash_MD5(t *testing.T) {
	result, err := execCrypto(t, "crypto.hash", map[string]any{
		"algorithm": "md5",
		"data":      "hello",
	})
	require.NoError(t, err)
	assert.Equal(t, "5d41402abc4b2a76b9719d911017c592", result["hash"])
}

func TestCryptoHash_SHA1(t *testing.T) {
	result, err := execCrypto(t, "crypto.hash", map[string]any{
		"algorithm": "sha1",
		"data":      "hello",
	})
	require.NoError(t, err)
	assert.Equal(t, "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d", result["hash"])
}

func TestCryptoHash_DefaultAlgorithm(t *testing.T) {
	result, err := execCrypto(t, "crypto.hash", map[string]any{
		"data": "hello",
	})
	require.NoError(t, err)
	assert.Equal(t, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", result["hash"])
	assert.Equal(t, "sha256", result["algorithm"])
}

func TestCryptoHash_UnsupportedAlgorithm(t *testing.T) {
	_, err := execCrypto(t, "crypto.hash", map[string]any{
		"algorithm": "blake2",
		"data":      "hello",
	})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

func TestCryptoHash_EmptyData(t *testing.T) {
	result, err := execCrypto(t, "crypto.hash", map[string]any{
		"data": "",
	})
	require.NoError(t, err)
	// sha256 of empty string
	assert.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", result["hash"])
}

func TestCryptoHash_Validate_MissingData(t *testing.T) {
	a := findCryptoAction("crypto.hash")
	require.NotNil(t, a)

	err := a.Validate(map[string]any{"algorithm": "sha256"})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

func TestCryptoHMAC_SHA256(t *testing.T) {
	result, err := execCrypto(t, "crypto.hmac", map[string]any{
		"data": "hello",
		"key":  "secret",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result["hmac"])
	assert.Equal(t, "sha256", result["algorithm"])
}

func TestCryptoHMAC_Validate_MissingKey(t *testing.T) {
	a := findCryptoAction("crypto.hmac")
	require.NotNil(t, a)

	err := a.Validate(map[string]any{"data": "hello"})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

func TestCryptoHMAC_Validate_MissingData(t *testing.T) {
	a := findCryptoAction("crypto.hmac")
	require.NotNil(t, a)

	err := a.Validate(map[string]any{"key": "secret"})
	require.Error(t, err)

	var opErr *schema.OpcodeError
	require.True(t, errors.As(err, &opErr))
	assert.Equal(t, schema.ErrCodeValidation, opErr.Code)
}

func TestCryptoUUID_Format(t *testing.T) {
	result, err := execCrypto(t, "crypto.uuid", map[string]any{})
	require.NoError(t, err)

	uuidStr, ok := result["uuid"].(string)
	require.True(t, ok)
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`), uuidStr)
}

func TestCryptoUUID_Uniqueness(t *testing.T) {
	r1, err := execCrypto(t, "crypto.uuid", map[string]any{})
	require.NoError(t, err)
	r2, err := execCrypto(t, "crypto.uuid", map[string]any{})
	require.NoError(t, err)
	assert.NotEqual(t, r1["uuid"], r2["uuid"])
}
