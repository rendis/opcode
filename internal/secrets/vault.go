package secrets

import "context"

// Vault resolves secret references (${{secrets.KEY}}) at runtime.
// Secrets are encrypted at rest (AES-256-GCM) and resolved in-memory only.
type Vault interface {
	Resolve(ctx context.Context, key string) ([]byte, error)
	Store(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
}
