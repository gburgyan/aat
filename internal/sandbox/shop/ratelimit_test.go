package shop

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimitHeaders(t *testing.T) {
	e := newEnv(t, Options{})
	remaining := func(r resp) int {
		t.Helper()
		n, err := strconv.Atoi(r.Header.Get("RateLimit-Remaining"))
		require.NoError(t, err, "RateLimit-Remaining: %q", r.Header.Get("RateLimit-Remaining"))
		return n
	}

	first := e.must(e.shop(http.MethodGet, "/us/v1/products", nil), 200)
	second := e.must(e.shop(http.MethodGet, "/eu/v1/products", nil), 200)

	assert.Equal(t, strconv.Itoa(RateLimitPerMinute), first.Header.Get("RateLimit-Limit"))
	assert.Equal(t, RateLimitPerMinute-1, remaining(first))
	assert.Equal(t, RateLimitPerMinute-2, remaining(second), "each request counts against the token, in any region")
	assert.NotEmpty(t, first.Header.Get("RateLimit-Reset"))

	other := e.do(http.MethodGet, e.api.URL+"/us/v1/products", nil, map[string]string{"Authorization": "Bearer " + e.newToken("password")})
	assert.Equal(t, RateLimitPerMinute-1, remaining(other), "another token has its own budget")
}
