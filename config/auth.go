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

// Authenticate performs an OAuth2 Resource Owner Password Credentials (ROPC)
// token exchange against the settings' AuthURL.
func Authenticate(ctx context.Context, s *Settings) (*OAuthToken, error) {
	form := url.Values{
		"grant_type":    {"password"},
		"username":      {s.Username},
		"password":      {s.Password},
		"client_id":     {s.ClientID},
		"client_secret": {s.ClientSecret},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.AuthURL, strings.NewReader(form.Encode()))
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
