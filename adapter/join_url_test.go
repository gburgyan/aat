package adapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJoinURL_KeepsPercentEncoding(t *testing.T) {
	tests := []struct {
		base, path, want string
	}{
		{"https://api.example.com/v1", "/carts/a%2Fb/items", "https://api.example.com/v1/carts/a%2Fb/items"},
		{"https://api.example.com/v1/", "/carts/a%20b", "https://api.example.com/v1/carts/a%20b"},
		{"https://api.example.com/v1", "/products?category=a%26b", "https://api.example.com/v1/products?category=a%26b"},
		{"https://api.example.com/v%201", "/x", "https://api.example.com/v%201/x"},
		{"", "/v2/search", "/v2/search"},
	}
	for _, tt := range tests {
		got, err := JoinURL(tt.base, tt.path)
		require.NoError(t, err, tt.path)
		assert.Equal(t, tt.want, got)
	}
}

func TestHTTPExecutor_SendsEncodedPathSegments(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.EscapedPath()
	}))
	defer srv.Close()

	_, err := NewHTTPExecutor(srv.URL+"/v1").Execute(context.Background(), &Request{Method: http.MethodGet, Path: "/carts/a%2Fb/items"})
	require.NoError(t, err)
	assert.Equal(t, "/v1/carts/a%2Fb/items", got, "an encoded slash stays inside its segment")
}
