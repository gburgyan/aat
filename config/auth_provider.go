package config

import (
	"context"
	"sync"
	"time"
)

// expiryBuffer is subtracted from the token expiry time to ensure tokens are
// refreshed before they actually expire.
const expiryBuffer = 30 * time.Second

// AuthProvider wraps an AuthConfig and caches its token. Thread-safe.
// For auth types that don't expire (apikey, bearer), the token is cached
// indefinitely. For oauth2, the token is refreshed when it nears expiry.
type AuthProvider struct {
	cfg AuthConfig

	mu        sync.Mutex
	token     *OAuthToken
	expiresAt time.Time

	// Injected for testing; defaults applied in NewAuthProvider.
	authenticator func(ctx context.Context, auth AuthConfig) (*OAuthToken, error)
	now           func() time.Time
}

// NewAuthProvider creates an AuthProvider for the given auth config.
func NewAuthProvider(cfg AuthConfig) *AuthProvider {
	return &AuthProvider{
		cfg:           cfg,
		authenticator: Authenticate,
		now:           time.Now,
	}
}

// Config returns the underlying AuthConfig.
func (p *AuthProvider) Config() AuthConfig {
	return p.cfg
}

// Authenticate returns a cached token if still valid, otherwise performs a
// fresh authentication. For type "none" (or empty), returns (nil, nil) without
// locking.
func (p *AuthProvider) Authenticate(ctx context.Context) (*OAuthToken, error) {
	// Short-circuit for no-auth — no lock needed.
	if p.cfg.Type == "none" || p.cfg.Type == "" {
		return nil, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Return cached token if it hasn't expired.
	if p.token != nil && !p.isExpired() {
		return p.token, nil
	}

	// Fetch a fresh token.
	token, err := p.authenticator(ctx, p.cfg)
	if err != nil {
		// Don't serve stale tokens on error.
		p.token = nil
		return nil, err
	}

	p.token = token

	// Track expiry for tokens that have a finite lifetime.
	if token != nil && token.ExpiresIn > 0 {
		p.expiresAt = p.now().Add(time.Duration(token.ExpiresIn)*time.Second - expiryBuffer)
	} else {
		// apikey, bearer, or ExpiresIn==0 → never expires.
		p.expiresAt = time.Time{}
	}

	return token, nil
}

// isExpired reports whether the cached token has expired. Must be called with
// p.mu held. A zero expiresAt means the token never expires.
func (p *AuthProvider) isExpired() bool {
	if p.expiresAt.IsZero() {
		return false
	}
	return p.now().After(p.expiresAt)
}
