package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// authVerboseKey is the context key for verbose auth logging.
type authVerboseKey struct{}

// WithAuthVerbose returns a child context that enables verbose auth logging
// to the given writer. When set, Authenticate writes request/response details
// to w for debugging.
func WithAuthVerbose(ctx context.Context, w io.Writer) context.Context {
	return context.WithValue(ctx, authVerboseKey{}, w)
}

// authVerboseWriter returns the verbose writer from the context, or nil.
func authVerboseWriter(ctx context.Context) io.Writer {
	if w, ok := ctx.Value(authVerboseKey{}).(io.Writer); ok {
		return w
	}
	return nil
}

// OAuthToken holds the result of an OAuth2 token exchange.
type OAuthToken struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// Authenticate performs authentication based on the AuthConfig type.
// For oauth2, it performs an OAuth2 ROPC token exchange.
// For apikey, it returns the resolved key as the access token.
// For bearer, it returns the resolved token.
// For none, it returns nil.
// When the context carries a verbose writer (via WithAuthVerbose), request and
// response details are logged to that writer for debugging.
func Authenticate(ctx context.Context, auth AuthConfig) (*OAuthToken, error) {
	vw := authVerboseWriter(ctx)
	if vw != nil {
		_, _ = fmt.Fprintf(vw, "[auth] authenticating with type=%s\n", auth.Type)
	}
	switch auth.Type {
	case "oauth2":
		return authenticateOAuth2(ctx, auth)
	case "apikey":
		return authenticateAPIKey(ctx, auth)
	case "bearer":
		return authenticateBearer(ctx, auth)
	case "none", "":
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown auth type %q", auth.Type)
	}
}

func authenticateOAuth2(ctx context.Context, auth AuthConfig) (*OAuthToken, error) {
	vw := authVerboseWriter(ctx)

	username, err := auth.Credentials["username"].Resolve()
	if err != nil {
		return nil, fmt.Errorf("resolving username: %w", err)
	}
	password, err := auth.Credentials["password"].Resolve()
	if err != nil {
		return nil, fmt.Errorf("resolving password: %w", err)
	}
	clientID, err := auth.Credentials["clientId"].Resolve()
	if err != nil {
		return nil, fmt.Errorf("resolving clientId: %w", err)
	}
	clientSecret, err := auth.Credentials["clientSecret"].Resolve()
	if err != nil {
		return nil, fmt.Errorf("resolving clientSecret: %w", err)
	}

	grantType := auth.GrantType
	if grantType == "" {
		grantType = "password"
	}

	form := url.Values{
		"grant_type":    {grantType},
		"username":      {username},
		"password":      {password},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}

	for k, v := range auth.ExtraParams {
		form.Set(k, v)
	}

	if vw != nil {
		_, _ = fmt.Fprintf(vw, "[auth] POST %s\n", auth.TokenURL)
		// Print form params in sorted order for deterministic output.
		keys := make([]string, 0, len(form))
		for k := range form {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := form.Get(k)
			if k == "client_secret" || k == "password" {
				v = v[:min(4, len(v))] + "..."
			}
			_, _ = fmt.Fprintf(vw, "[auth]   %s = %s\n", k, v)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, auth.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if vw != nil {
			_, _ = fmt.Fprintf(vw, "[auth] request error: %s\n", err)
		}
		return nil, fmt.Errorf("executing auth request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading auth response: %w", err)
	}

	if vw != nil {
		_, _ = fmt.Fprintf(vw, "[auth] response status: %d\n", resp.StatusCode)
		_, _ = fmt.Fprintf(vw, "[auth] response body: %s\n", string(body))
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("auth failed with status %d: %s", resp.StatusCode, string(body))
	}

	var token OAuthToken
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, fmt.Errorf("parsing auth response: %w", err)
	}

	if token.AccessToken == "" {
		return nil, fmt.Errorf("auth response missing access_token")
	}

	if vw != nil {
		_, _ = fmt.Fprintf(vw, "[auth] token type=%s expires_in=%d access_token=%s...\n",
			token.TokenType, token.ExpiresIn, token.AccessToken[:min(20, len(token.AccessToken))])
	}

	return &token, nil
}

func authenticateAPIKey(ctx context.Context, auth AuthConfig) (*OAuthToken, error) {
	ref, ok := auth.Credentials["key"]
	if !ok {
		return nil, fmt.Errorf("apikey auth missing credentials.key")
	}
	key, err := ref.Resolve()
	if err != nil {
		return nil, fmt.Errorf("resolving api key: %w", err)
	}
	if vw := authVerboseWriter(ctx); vw != nil {
		_, _ = fmt.Fprintf(vw, "[auth] apikey resolved (%d chars)\n", len(key))
	}
	return &OAuthToken{
		AccessToken: key,
		TokenType:   "apikey",
	}, nil
}

func authenticateBearer(ctx context.Context, auth AuthConfig) (*OAuthToken, error) {
	ref, ok := auth.Credentials["token"]
	if !ok {
		return nil, fmt.Errorf("bearer auth missing credentials.token")
	}
	token, err := ref.Resolve()
	if err != nil {
		return nil, fmt.Errorf("resolving bearer token: %w", err)
	}
	if vw := authVerboseWriter(ctx); vw != nil {
		_, _ = fmt.Fprintf(vw, "[auth] bearer token resolved (%d chars)\n", len(token))
	}
	return &OAuthToken{
		AccessToken: token,
		TokenType:   "Bearer",
	}, nil
}
