package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuthConfigJSONKeys checks that auth recorded in archives (a plan's auth in
// metadata.plan) uses the same camelCase keys as the YAML it came from.
func TestAuthConfigJSONKeys(t *testing.T) {
	data, err := json.Marshal(AuthConfig{
		Type:        "oauth2",
		TokenURL:    "https://auth.example.com/token",
		HeaderName:  "X-Key",
		GrantType:   "client_credentials",
		ExtraParams: map[string]string{"scope": "read"},
		Credentials: map[string]SecretRef{"clientId": {Source: "env", Var: "CLIENT_ID"}},
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"type": "oauth2",
		"tokenUrl": "https://auth.example.com/token",
		"headerName": "X-Key",
		"grantType": "client_credentials",
		"extraParams": {"scope": "read"},
		"credentials": {"clientId": {"source": "env", "var": "CLIENT_ID"}}
	}`, string(data))
}
