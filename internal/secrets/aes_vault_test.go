package secrets

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rendis/opcode/pkg/schema"
)

// mapStore is a simple in-memory SecretStore for vault tests.
type mapStore struct {
	data map[string][]byte
}

func newMapStore() *mapStore {
	return &mapStore{data: make(map[string][]byte)}
}

func (m *mapStore) StoreSecret(_ context.Context, key string, value []byte) error {
	cp := make([]byte, len(value))
	copy(cp, value)
	m.data[key] = cp
	return nil
}

func (m *mapStore) GetSecret(_ context.Context, key string) ([]byte, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, schema.NewErrorf(schema.ErrCodeNotFound, "secret %q not found", key)
	}
	return v, nil
}

func (m *mapStore) DeleteSecret(_ context.Context, key string) error {
	if _, ok := m.data[key]; !ok {
		return schema.NewErrorf(schema.ErrCodeNotFound, "secret %q not found", key)
	}
	delete(m.data, key)
	return nil
}

func (m *mapStore) ListSecrets(_ context.Context) ([]string, error) {
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	return keys, nil
}

func testVault(t *testing.T) (*AESVault, *mapStore) {
	t.Helper()
	s := newMapStore()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	v, err := NewAESVault(s, VaultConfig{MasterKey: key})
	require.NoError(t, err)
	return v, s
}

func TestAESVault_StoreAndResolve(t *testing.T) {
	v, _ := testVault(t)
	ctx := context.Background()

	require.NoError(t, v.Store(ctx, "api_key", []byte("sk-secret-123")))

	val, err := v.Resolve(ctx, "api_key")
	require.NoError(t, err)
	assert.Equal(t, []byte("sk-secret-123"), val)
}

func TestAESVault_EncryptedAtRest(t *testing.T) {
	v, s := testVault(t)
	ctx := context.Background()

	require.NoError(t, v.Store(ctx, "token", []byte("plaintext-value")))

	// Raw bytes in store should NOT be plaintext.
	raw := s.data["token"]
	assert.NotEqual(t, []byte("plaintext-value"), raw)
	assert.Greater(t, len(raw), len("plaintext-value"))
}

func TestAESVault_PassphraseDerivation(t *testing.T) {
	s := newMapStore()
	salt := []byte("test-salt-16byte")
	v, err := NewAESVault(s, VaultConfig{
		Passphrase: "my-secure-passphrase",
		Salt:       salt,
		Iterations: 1000, // low for test speed
	})
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, v.Store(ctx, "k", []byte("value")))
	val, err := v.Resolve(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, []byte("value"), val)
}

func TestAESVault_WrongKeyCannotDecrypt(t *testing.T) {
	s := newMapStore()
	ctx := context.Background()

	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	key2[0] = 0xFF

	v1, _ := NewAESVault(s, VaultConfig{MasterKey: key1})
	require.NoError(t, v1.Store(ctx, "secret", []byte("hidden")))

	v2, _ := NewAESVault(s, VaultConfig{MasterKey: key2})
	_, err := v2.Resolve(ctx, "secret")
	require.Error(t, err)
}

func TestAESVault_Delete(t *testing.T) {
	v, _ := testVault(t)
	ctx := context.Background()

	require.NoError(t, v.Store(ctx, "key", []byte("val")))
	require.NoError(t, v.Delete(ctx, "key"))

	_, err := v.Resolve(ctx, "key")
	require.Error(t, err)
	var opcErr *schema.OpcodeError
	require.True(t, errors.As(err, &opcErr))
	assert.Equal(t, schema.ErrCodeNotFound, opcErr.Code)
}

func TestAESVault_List(t *testing.T) {
	v, _ := testVault(t)
	ctx := context.Background()

	require.NoError(t, v.Store(ctx, "a_key", []byte("1")))
	require.NoError(t, v.Store(ctx, "b_key", []byte("2")))
	require.NoError(t, v.Store(ctx, "c_key", []byte("3")))

	keys, err := v.List(ctx)
	require.NoError(t, err)
	assert.Len(t, keys, 3)
}

func TestAESVault_Overwrite(t *testing.T) {
	v, _ := testVault(t)
	ctx := context.Background()

	require.NoError(t, v.Store(ctx, "key", []byte("v1")))
	require.NoError(t, v.Store(ctx, "key", []byte("v2")))

	val, err := v.Resolve(ctx, "key")
	require.NoError(t, err)
	assert.Equal(t, []byte("v2"), val)
}

func TestAESVault_ResolveNotFound(t *testing.T) {
	v, _ := testVault(t)
	ctx := context.Background()

	_, err := v.Resolve(ctx, "nonexistent")
	require.Error(t, err)
}

func TestAESVault_InvalidKeyLength(t *testing.T) {
	_, err := NewAESVault(newMapStore(), VaultConfig{MasterKey: []byte("too-short")})
	require.Error(t, err)
	var opcErr *schema.OpcodeError
	require.True(t, errors.As(err, &opcErr))
	assert.Equal(t, schema.ErrCodeVault, opcErr.Code)
}

func TestAESVault_UniqueNonces(t *testing.T) {
	v, s := testVault(t)
	ctx := context.Background()

	require.NoError(t, v.Store(ctx, "k1", []byte("same-value")))
	ct1 := make([]byte, len(s.data["k1"]))
	copy(ct1, s.data["k1"])

	require.NoError(t, v.Store(ctx, "k2", []byte("same-value")))
	ct2 := s.data["k2"]

	// Same plaintext must produce different ciphertext (random nonce).
	assert.False(t, bytes.Equal(ct1, ct2))
}

func TestAESVault_EmptyValue(t *testing.T) {
	v, _ := testVault(t)
	ctx := context.Background()

	require.NoError(t, v.Store(ctx, "empty", []byte{}))
	val, err := v.Resolve(ctx, "empty")
	require.NoError(t, err)
	assert.Empty(t, val)
}

func TestAESVault_NoKeyOrPassphrase(t *testing.T) {
	_, err := NewAESVault(newMapStore(), VaultConfig{})
	require.Error(t, err)
}

func TestAESVault_PassphraseWithoutSalt(t *testing.T) {
	_, err := NewAESVault(newMapStore(), VaultConfig{Passphrase: "pass"})
	require.Error(t, err)
}
