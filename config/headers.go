package config

import "strings"

// AddOverlayHeaders applies headers from an overlay file (.aat-overrides.yaml or
// --overlay) to the route. They replace any header of the same name, the
// credential included, whatever the case of the name; templates cannot replace
// them; and the routes of overrides built from this one apply them too.
func (c *APIConfig) AddOverlayHeaders(headers map[string]string) {
	for k, v := range headers {
		c.Overlay = withHeader(c.Overlay, k, v)
	}
	c.addProtected(headers)
}

// addProtected sets headers on the route and protects them from template
// headers.
func (c *APIConfig) addProtected(headers map[string]string) {
	for k, v := range headers {
		c.Headers = withHeader(c.Headers, k, v)
		c.Protected = withHeader(c.Protected, k, v)
	}
}

// credentialHeader returns the header that carries an authentication token, and
// false when there is no token (auth type none).
func credentialHeader(auth AuthConfig, token *OAuthToken) (name, value string, ok bool) {
	if token == nil {
		return "", "", false
	}
	if auth.Type == "apikey" {
		return auth.HeaderName, token.AccessToken, true
	}
	return "Authorization", "Bearer " + token.AccessToken, true
}

// withHeader sets a header in headers, allocating the map when it is nil, after
// removing any header whose name differs only in case: HTTP header names are
// case-insensitive.
func withHeader(headers map[string]string, name, value string) map[string]string {
	if headers == nil {
		headers = make(map[string]string)
	}
	deleteHeader(headers, name)
	headers[name] = value
	return headers
}

// deleteHeader removes every header named name, whatever the case.
func deleteHeader(headers map[string]string, name string) {
	for existing := range headers {
		if strings.EqualFold(existing, name) {
			delete(headers, existing)
		}
	}
}
