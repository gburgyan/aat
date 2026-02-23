package config

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAuthenticator returns a counting authenticator that returns the given token.
func stubAuthenticator(token *OAuthToken, err error) (func(context.Context, AuthConfig) (*OAuthToken, error), *int32) {
	var count int32
	return func(_ context.Context, _ AuthConfig) (*OAuthToken, error) {
		atomic.AddInt32(&count, 1)
		return token, err
	}, &count
}

func TestAuthProvider_OAuth2_CachesTwoCallsOneAuth(t *testing.T) {
	auth, count := stubAuthenticator(&OAuthToken{
		AccessToken: "tok-1",
		ExpiresIn:   3600,
	}, nil)

	p := NewAuthProvider(AuthConfig{Type: "oauth2"})
	p.authenticator = auth

	tok1, err := p.Authenticate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "tok-1", tok1.AccessToken)

	tok2, err := p.Authenticate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "tok-1", tok2.AccessToken)

	assert.Equal(t, int32(1), atomic.LoadInt32(count))
}

func TestAuthProvider_OAuth2_RefreshAfterExpiry(t *testing.T) {
	callNum := int32(0)
	auth := func(_ context.Context, _ AuthConfig) (*OAuthToken, error) {
		n := atomic.AddInt32(&callNum, 1)
		return &OAuthToken{
			AccessToken: fmt.Sprintf("tok-%d", n),
			ExpiresIn:   3600,
		}, nil
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := NewAuthProvider(AuthConfig{Type: "oauth2"})
	p.authenticator = auth
	p.now = func() time.Time { return now }

	tok1, err := p.Authenticate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "tok-1", tok1.AccessToken)

	// Advance time past expiry (3600s - 30s buffer = 3570s)
	now = now.Add(3571 * time.Second)

	tok2, err := p.Authenticate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "tok-2", tok2.AccessToken)
	assert.Equal(t, int32(2), atomic.LoadInt32(&callNum))
}

func TestAuthProvider_OAuth2_ExpiryBuffer(t *testing.T) {
	auth, count := stubAuthenticator(&OAuthToken{
		AccessToken: "tok-1",
		ExpiresIn:   60, // 60s token, buffer is 30s → effective lifetime 30s
	}, nil)

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := NewAuthProvider(AuthConfig{Type: "oauth2"})
	p.authenticator = auth
	p.now = func() time.Time { return now }

	_, err := p.Authenticate(context.Background())
	require.NoError(t, err)

	// 25s later — still within buffer
	now = now.Add(25 * time.Second)
	_, err = p.Authenticate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(count))

	// 31s later — past effective expiry (60-30=30s)
	now = now.Add(6 * time.Second)
	_, err = p.Authenticate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int32(2), atomic.LoadInt32(count))
}

func TestAuthProvider_Bearer_NeverExpires(t *testing.T) {
	auth, count := stubAuthenticator(&OAuthToken{
		AccessToken: "my-bearer",
		TokenType:   "Bearer",
		// ExpiresIn: 0 — no expiry
	}, nil)

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := NewAuthProvider(AuthConfig{Type: "bearer"})
	p.authenticator = auth
	p.now = func() time.Time { return now }

	_, err := p.Authenticate(context.Background())
	require.NoError(t, err)

	// Way into the future — still cached
	now = now.Add(24 * time.Hour)
	tok, err := p.Authenticate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "my-bearer", tok.AccessToken)
	assert.Equal(t, int32(1), atomic.LoadInt32(count))
}

func TestAuthProvider_APIKey_NeverExpires(t *testing.T) {
	auth, count := stubAuthenticator(&OAuthToken{
		AccessToken: "my-key",
		TokenType:   "apikey",
	}, nil)

	p := NewAuthProvider(AuthConfig{Type: "apikey", HeaderName: "X-Key"})
	p.authenticator = auth

	tok1, err := p.Authenticate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "my-key", tok1.AccessToken)

	tok2, err := p.Authenticate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "my-key", tok2.AccessToken)
	assert.Equal(t, int32(1), atomic.LoadInt32(count))
}

func TestAuthProvider_None_NoLockNoCall(t *testing.T) {
	auth, count := stubAuthenticator(nil, nil)

	p := NewAuthProvider(AuthConfig{Type: "none"})
	p.authenticator = auth

	tok, err := p.Authenticate(context.Background())
	require.NoError(t, err)
	assert.Nil(t, tok)
	assert.Equal(t, int32(0), atomic.LoadInt32(count))
}

func TestAuthProvider_EmptyType_TreatedAsNone(t *testing.T) {
	auth, count := stubAuthenticator(nil, nil)

	p := NewAuthProvider(AuthConfig{Type: ""})
	p.authenticator = auth

	tok, err := p.Authenticate(context.Background())
	require.NoError(t, err)
	assert.Nil(t, tok)
	assert.Equal(t, int32(0), atomic.LoadInt32(count))
}

func TestAuthProvider_Concurrent_SingleAuthCall(t *testing.T) {
	var callCount int32
	// Slow authenticator to ensure goroutines overlap
	auth := func(_ context.Context, _ AuthConfig) (*OAuthToken, error) {
		atomic.AddInt32(&callCount, 1)
		time.Sleep(50 * time.Millisecond)
		return &OAuthToken{AccessToken: "concurrent-tok", ExpiresIn: 3600}, nil
	}

	p := NewAuthProvider(AuthConfig{Type: "oauth2"})
	p.authenticator = auth

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	tokens := make([]*OAuthToken, goroutines)
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			tokens[idx], errs[idx] = p.Authenticate(context.Background())
		}(i)
	}
	wg.Wait()

	for i := 0; i < goroutines; i++ {
		require.NoError(t, errs[i])
		assert.Equal(t, "concurrent-tok", tokens[i].AccessToken)
	}
	// Only one goroutine should have called the authenticator; others waited.
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount))
}

func TestAuthProvider_RefreshFailure_NoStaleToken(t *testing.T) {
	callNum := int32(0)
	auth := func(_ context.Context, _ AuthConfig) (*OAuthToken, error) {
		n := atomic.AddInt32(&callNum, 1)
		if n == 1 {
			return &OAuthToken{AccessToken: "tok-1", ExpiresIn: 60}, nil
		}
		return nil, fmt.Errorf("auth server down")
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	p := NewAuthProvider(AuthConfig{Type: "oauth2"})
	p.authenticator = auth
	p.now = func() time.Time { return now }

	tok, err := p.Authenticate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "tok-1", tok.AccessToken)

	// Advance past expiry
	now = now.Add(time.Hour)

	tok, err = p.Authenticate(context.Background())
	require.Error(t, err)
	assert.Nil(t, tok)
	assert.Contains(t, err.Error(), "auth server down")
}

func TestAuthProvider_Config_Accessor(t *testing.T) {
	cfg := AuthConfig{
		Type:       "apikey",
		HeaderName: "X-Key",
		Credentials: map[string]SecretRef{
			"key": {Source: "literal", Value: "secret"},
		},
	}
	p := NewAuthProvider(cfg)
	assert.Equal(t, cfg, p.Config())
}
