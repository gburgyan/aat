package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAdapter satisfies the Adapter interface for registry tests.
type stubAdapter struct{}

func (s *stubAdapter) BuildRequest(_ map[string]any, _ *EnvironmentConfig) (*Request, error) {
	return nil, nil
}
func (s *stubAdapter) ExtractOutputs(_ *Response) (map[string]any, error) { return nil, nil }
func (s *stubAdapter) ValidateInputs(_ map[string]any) *ValidationResult   { return nil }
func (s *stubAdapter) ValidateResponse(_ *Response) *ValidationResult      { return nil }

func TestRegistry(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*Registry)
		action  func(*Registry) error
		check   func(*testing.T, *Registry, error)
	}{
		{
			name: "register and get",
			setup: func(r *Registry) {
				require.NoError(t, r.Register("alpha", &stubAdapter{}))
			},
			action: func(r *Registry) error {
				a, err := r.Get("alpha")
				if err != nil {
					return err
				}
				assert.NotNil(t, a)
				return nil
			},
			check: func(t *testing.T, _ *Registry, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name:  "get unknown returns error",
			setup: func(_ *Registry) {},
			action: func(r *Registry) error {
				_, err := r.Get("nonexistent")
				return err
			},
			check: func(t *testing.T, _ *Registry, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "not found")
			},
		},
		{
			name: "duplicate register returns error",
			setup: func(r *Registry) {
				require.NoError(t, r.Register("dup", &stubAdapter{}))
			},
			action: func(r *Registry) error {
				return r.Register("dup", &stubAdapter{})
			},
			check: func(t *testing.T, _ *Registry, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "already registered")
			},
		},
		{
			name: "Names returns sorted list",
			setup: func(r *Registry) {
				require.NoError(t, r.Register("charlie", &stubAdapter{}))
				require.NoError(t, r.Register("alpha", &stubAdapter{}))
				require.NoError(t, r.Register("bravo", &stubAdapter{}))
			},
			action: func(r *Registry) error { return nil },
			check: func(t *testing.T, r *Registry, _ error) {
				assert.Equal(t, []string{"alpha", "bravo", "charlie"}, r.Names())
			},
		},
		{
			name:   "Names on empty registry",
			setup:  func(_ *Registry) {},
			action: func(r *Registry) error { return nil },
			check: func(t *testing.T, r *Registry, _ error) {
				assert.Empty(t, r.Names())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry()
			tt.setup(r)
			err := tt.action(r)
			tt.check(t, r, err)
		})
	}
}

func TestGetTemplate(t *testing.T) {
	t.Run("found template adapter", func(t *testing.T) {
		r := NewRegistry()
		tmpl := Template{
			Adapter:  "search",
			Protocol: "http",
			Request:  TemplateRequest{Method: "GET", Path: "/search"},
		}
		require.NoError(t, r.Register("search", NewTemplateAdapter(tmpl)))

		got, ok := r.GetTemplate("search")
		assert.True(t, ok)
		require.NotNil(t, got)
		assert.Equal(t, "GET", got.Request.Method)
		assert.Equal(t, "/search", got.Request.Path)
	})

	t.Run("found non-template adapter", func(t *testing.T) {
		r := NewRegistry()
		require.NoError(t, r.Register("custom", &stubAdapter{}))

		got, ok := r.GetTemplate("custom")
		assert.False(t, ok)
		assert.Nil(t, got)
	})

	t.Run("not found", func(t *testing.T) {
		r := NewRegistry()

		got, ok := r.GetTemplate("missing")
		assert.False(t, ok)
		assert.Nil(t, got)
	})
}

func TestValidationResult(t *testing.T) {
	t.Run("NewValidationPass", func(t *testing.T) {
		vr := NewValidationPass()
		assert.True(t, vr.Valid)
		assert.Empty(t, vr.Errors)
		assert.Equal(t, "", vr.Error())
	})

	t.Run("NewValidationFailure with field errors", func(t *testing.T) {
		vr := NewValidationFailure(
			ValidationError{Field: "origin", Message: "required"},
			ValidationError{Field: "destination", Message: "must be IATA code"},
		)
		assert.False(t, vr.Valid)
		assert.Len(t, vr.Errors, 2)

		errStr := vr.Error()
		assert.Contains(t, errStr, "validation failed:")
		assert.Contains(t, errStr, "origin: required")
		assert.Contains(t, errStr, "destination: must be IATA code")
	})

	t.Run("NewValidationFailure with general error", func(t *testing.T) {
		vr := NewValidationFailure(
			ValidationError{Message: "request body too large"},
		)
		assert.False(t, vr.Valid)

		errStr := vr.Error()
		assert.Contains(t, errStr, "validation failed:")
		assert.Contains(t, errStr, "  request body too large")
		// General error line has no "field: " prefix
		assert.NotContains(t, errStr, "  request body too large:")
	})

	t.Run("nil means no validation performed", func(t *testing.T) {
		var vr *ValidationResult
		assert.Nil(t, vr)
	})
}

func TestEnvironmentConfig(t *testing.T) {
	t.Run("GetValue found", func(t *testing.T) {
		cfg := &EnvironmentConfig{
			Values: map[string]string{
				"auth.token": "abc123",
			},
		}
		v, ok := cfg.GetValue("auth.token")
		assert.True(t, ok)
		assert.Equal(t, "abc123", v)
	})

	t.Run("GetValue not found", func(t *testing.T) {
		cfg := &EnvironmentConfig{
			Values: map[string]string{},
		}
		v, ok := cfg.GetValue("missing")
		assert.False(t, ok)
		assert.Equal(t, "", v)
	})

	t.Run("GetValue nil map", func(t *testing.T) {
		cfg := &EnvironmentConfig{}
		v, ok := cfg.GetValue("anything")
		assert.False(t, ok)
		assert.Equal(t, "", v)
	})
}
