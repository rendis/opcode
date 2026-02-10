package secrets

import "context"

// Vault resolves secret references (${{secrets.KEY}}) at runtime.
// Secrets are encrypted at rest (AES-256-GCM) and resolved in-memory only.
type Vault interface {
	Resolve(ctx context.Context, key string) ([]byte, error)
	Store(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context) ([]string, error)
}

// SecretStore is the minimal persistence interface needed by the vault.
// Satisfied by store.Store.
type SecretStore interface {
	StoreSecret(ctx context.Context, key string, value []byte) error
	GetSecret(ctx context.Context, key string) ([]byte, error)
	DeleteSecret(ctx context.Context, key string) error
	ListSecrets(ctx context.Context) ([]string, error)
}
