package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRewritePath_NilRewrite(t *testing.T) {
	assert.Equal(t, "/api/v1/search", RewritePath("/api/v1/search", nil))
}

func TestRewritePath_StripOnly(t *testing.T) {
	rewrite := &PathRewrite{Strip: "/11"}
	assert.Equal(t, "/search", RewritePath("/11/search", rewrite))
}

func TestRewritePath_StripNoMatch(t *testing.T) {
	rewrite := &PathRewrite{Strip: "/v2"}
	assert.Equal(t, "/v1/search", RewritePath("/v1/search", rewrite))
}

func TestRewritePath_PrefixOnly(t *testing.T) {
	rewrite := &PathRewrite{Prefix: "/api/v2"}
	assert.Equal(t, "/api/v2/search", RewritePath("/search", rewrite))
}

func TestRewritePath_StripAndPrefix(t *testing.T) {
	rewrite := &PathRewrite{Strip: "/11", Prefix: "/api/v2"}
	assert.Equal(t, "/api/v2/search", RewritePath("/11/search", rewrite))
}

func TestRewritePath_AvoidDoubleSlash(t *testing.T) {
	rewrite := &PathRewrite{Prefix: "/api/v2/"}
	assert.Equal(t, "/api/v2/search", RewritePath("/search", rewrite))
}

func TestRewritePath_EnsureSlashBetween(t *testing.T) {
	rewrite := &PathRewrite{Strip: "/", Prefix: "/api"}
	assert.Equal(t, "/api/search", RewritePath("/search", rewrite))
}

func TestRewritePath_EmptyPath(t *testing.T) {
	rewrite := &PathRewrite{Prefix: "/api/v2"}
	assert.Equal(t, "/api/v2", RewritePath("", rewrite))
}

func TestRewritePath_EmptyStripAndPrefix(t *testing.T) {
	rewrite := &PathRewrite{}
	assert.Equal(t, "/search", RewritePath("/search", rewrite))
}

func TestRewritePath_StripEntirePath(t *testing.T) {
	rewrite := &PathRewrite{Strip: "/api/v1/search", Prefix: "/search"}
	assert.Equal(t, "/search", RewritePath("/api/v1/search", rewrite))
}
