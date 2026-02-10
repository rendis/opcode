package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	"github.com/rendis/opcode/pkg/schema"
)

// VaultConfig configures the AES vault key derivation.
// Provide either MasterKey (raw 32 bytes) or Passphrase + Salt.
type VaultConfig struct {
	MasterKey  []byte // raw 32-byte key (takes priority)
	Passphrase string // derive key via PBKDF2
	Salt       []byte // salt for PBKDF2 (required with Passphrase)
	Iterations int    // PBKDF2 iterations (default 100_000)
}

// AESVault encrypts secrets with AES-256-GCM before persisting.
type AESVault struct {
	store SecretStore
	aead  cipher.AEAD
}

// NewAESVault creates a vault with AES-256-GCM encryption.
func NewAESVault(s SecretStore, cfg VaultConfig) (*AESVault, error) {
	key, err := deriveKey(cfg)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return &AESVault{store: s, aead: aead}, nil
}

func deriveKey(cfg VaultConfig) ([]byte, error) {
	if len(cfg.MasterKey) > 0 {
		if len(cfg.MasterKey) != 32 {
			return nil, schema.NewErrorf(schema.ErrCodeVault,
				"master key must be 32 bytes, got %d", len(cfg.MasterKey))
		}
		return cfg.MasterKey, nil
	}
	if cfg.Passphrase == "" {
		return nil, schema.NewError(schema.ErrCodeVault, "either master_key or passphrase is required")
	}
	if len(cfg.Salt) == 0 {
		return nil, schema.NewError(schema.ErrCodeVault, "salt is required with passphrase")
	}
	iterations := cfg.Iterations
	if iterations <= 0 {
		iterations = 100_000
	}
	return pbkdf2.Key(sha256.New, cfg.Passphrase, cfg.Salt, iterations, 32)
}

func (v *AESVault) encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return v.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (v *AESVault) decrypt(ciphertext []byte) ([]byte, error) {
	nonceSize := v.aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, schema.NewError(schema.ErrCodeVault, "ciphertext too short")
	}
	nonce := ciphertext[:nonceSize]
	ct := ciphertext[nonceSize:]
	plaintext, err := v.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, schema.NewErrorf(schema.ErrCodeVault, "decrypt failed: %s", err.Error())
	}
	return plaintext, nil
}

func (v *AESVault) Store(ctx context.Context, key string, value []byte) error {
	encrypted, err := v.encrypt(value)
	if err != nil {
		return err
	}
	return v.store.StoreSecret(ctx, key, encrypted)
}

func (v *AESVault) Resolve(ctx context.Context, key string) ([]byte, error) {
	encrypted, err := v.store.GetSecret(ctx, key)
	if err != nil {
		return nil, err
	}
	return v.decrypt(encrypted)
}

func (v *AESVault) Delete(ctx context.Context, key string) error {
	return v.store.DeleteSecret(ctx, key)
}

func (v *AESVault) List(ctx context.Context) ([]string, error) {
	return v.store.ListSecrets(ctx)
}
