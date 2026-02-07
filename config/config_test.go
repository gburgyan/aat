package config

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSettings_Valid(t *testing.T) {
	s, err := LoadSettings("testdata/settings.json")
	require.NoError(t, err)
	assert.Equal(t, "testuser", s.Username)
	assert.Equal(t, "testpass", s.Password)
	assert.Equal(t, "test-client-id", s.ClientID)
	assert.Equal(t, "test-client-secret", s.ClientSecret)
	assert.Equal(t, "TEST-GROUP-ID", s.AccessGroup)
	assert.Equal(t, "https://auth.example.com/oauth/token", s.AuthURL)
	assert.Equal(t, "api.example.com", s.APIBase)
}

func TestLoadSettings_MissingFile(t *testing.T) {
	_, err := LoadSettings("testdata/nonexistent.json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading settings file")
}

func TestLoadSettings_MalformedJSON(t *testing.T) {
	tmpFile := t.TempDir() + "/bad.json"
	require.NoError(t, os.WriteFile(tmpFile, []byte("{not json"), 0644))

	_, err := LoadSettings(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing settings JSON")
}

func TestLoadSettings_MissingAuthURL(t *testing.T) {
	tmpFile := t.TempDir() + "/no_auth_url.json"
	require.NoError(t, os.WriteFile(tmpFile, []byte(`{"api_base": "api.example.com"}`), 0644))

	_, err := LoadSettings(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "auth_url")
}

func TestLoadSettings_MissingAPIBase(t *testing.T) {
	tmpFile := t.TempDir() + "/no_api_base.json"
	require.NoError(t, os.WriteFile(tmpFile, []byte(`{"auth_url": "https://auth.example.com/oauth/token"}`), 0644))

	_, err := LoadSettings(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api_base")
}

func TestAuthenticate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

		require.NoError(t, r.ParseForm())
		assert.Equal(t, "password", r.FormValue("grant_type"))
		assert.Equal(t, "testuser", r.FormValue("username"))
		assert.Equal(t, "testpass", r.FormValue("password"))
		assert.Equal(t, "test-client-id", r.FormValue("client_id"))
		assert.Equal(t, "test-client-secret", r.FormValue("client_secret"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(OAuthToken{
			AccessToken: "test-token-123",
			TokenType:   "Bearer",
			ExpiresIn:   3600,
		})
	}))
	defer server.Close()

	s := &Settings{
		Username:     "testuser",
		Password:     "testpass",
		ClientID:     "test-client-id",
		ClientSecret: "test-client-secret",
		AuthURL:      server.URL,
	}

	token, err := Authenticate(context.Background(), s)
	require.NoError(t, err)
	assert.Equal(t, "test-token-123", token.AccessToken)
	assert.Equal(t, "Bearer", token.TokenType)
	assert.Equal(t, 3600, token.ExpiresIn)
}

func TestAuthenticate_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "invalid_client"}`))
	}))
	defer server.Close()

	s := &Settings{
		Username: "bad",
		Password: "creds",
		AuthURL:  server.URL,
	}

	_, err := Authenticate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "status 401")
}

func TestAuthenticate_MalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer server.Close()

	s := &Settings{AuthURL: server.URL}

	_, err := Authenticate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing auth response")
}

func TestAuthenticate_MissingAccessToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"token_type": "Bearer",
			"expires_in": 3600,
		})
	}))
	defer server.Close()

	s := &Settings{AuthURL: server.URL}

	_, err := Authenticate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing access_token")
}

func TestAuthenticate_NetworkError(t *testing.T) {
	s := &Settings{AuthURL: "http://localhost:1"}
	_, err := Authenticate(context.Background(), s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "executing auth request")
}

func TestAuthenticate_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s := &Settings{AuthURL: "https://auth.example.com/oauth/token"}
	_, err := Authenticate(ctx, s)
	require.Error(t, err)
}
