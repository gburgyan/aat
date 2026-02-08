package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

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
func Authenticate(ctx context.Context, auth AuthConfig) (*OAuthToken, error) {
	switch auth.Type {
	case "oauth2":
		return authenticateOAuth2(ctx, auth)
	case "apikey":
		return authenticateAPIKey(auth)
	case "bearer":
		return authenticateBearer(auth)
	case "none", "":
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown auth type %q", auth.Type)
	}
}

func authenticateOAuth2(ctx context.Context, auth AuthConfig) (*OAuthToken, error) {
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

	form := url.Values{
		"grant_type":    {"password"},
		"username":      {username},
		"password":      {password},
		"client_id":     {clientID},
		"client_secret": {clientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, auth.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing auth request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading auth response: %w", err)
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

	return &token, nil
}

func authenticateAPIKey(auth AuthConfig) (*OAuthToken, error) {
	ref, ok := auth.Credentials["key"]
	if !ok {
		return nil, fmt.Errorf("apikey auth missing credentials.key")
	}
	key, err := ref.Resolve()
	if err != nil {
		return nil, fmt.Errorf("resolving api key: %w", err)
	}
	return &OAuthToken{
		AccessToken: key,
		TokenType:   "apikey",
	}, nil
}

func authenticateBearer(auth AuthConfig) (*OAuthToken, error) {
	ref, ok := auth.Credentials["token"]
	if !ok {
		return nil, fmt.Errorf("bearer auth missing credentials.token")
	}
	token, err := ref.Resolve()
	if err != nil {
		return nil, fmt.Errorf("resolving bearer token: %w", err)
	}
	return &OAuthToken{
		AccessToken: token,
		TokenType:   "Bearer",
	}, nil
}
