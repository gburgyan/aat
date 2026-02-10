package adapter

import (
	"fmt"
	"sort"
)

// Adapter translates between the graph's logical inputs/outputs and concrete
// HTTP requests/responses. Each node in the graph is backed by one Adapter.
type Adapter interface {
	// BuildRequest constructs an HTTP request from the node's resolved inputs
	// and environment configuration. The adapter is responsible for including
	// any auth headers from config.
	BuildRequest(inputs map[string]any, config *EnvironmentConfig) (*Request, error)

	// ExtractOutputs parses the HTTP response into the node's declared outputs.
	ExtractOutputs(resp *Response) (map[string]any, error)

	// ValidateInputs checks whether the given inputs satisfy the adapter's
	// requirements. Returns nil to indicate no validation was performed.
	ValidateInputs(inputs map[string]any) *ValidationResult

	// ValidateResponse checks whether the HTTP response is acceptable.
	// Returns nil to indicate no validation was performed.
	ValidateResponse(resp *Response) *ValidationResult
}

// Registry holds named adapters and provides lookup by name.
type Registry struct {
	adapters map[string]Adapter
}

// NewRegistry creates an empty adapter registry.
func NewRegistry() *Registry {
	return &Registry{
		adapters: make(map[string]Adapter),
	}
}

// Register adds an adapter under the given name. Returns an error if the
// name is already registered.
func (r *Registry) Register(name string, a Adapter) error {
	if _, exists := r.adapters[name]; exists {
		return fmt.Errorf("adapter %q already registered", name)
	}
	r.adapters[name] = a
	return nil
}

// Get returns the adapter registered under the given name, or an error if
// no adapter is found.
func (r *Registry) Get(name string) (Adapter, error) {
	a, ok := r.adapters[name]
	if !ok {
		return nil, fmt.Errorf("adapter %q not found", name)
	}
	return a, nil
}

// GetTemplate returns the underlying Template if the named adapter is
// template-based. Returns nil, false if not found or not a TemplateAdapter.
func (r *Registry) GetTemplate(name string) (*Template, bool) {
	a, ok := r.adapters[name]
	if !ok {
		return nil, false
	}
	ta, ok := a.(*TemplateAdapter)
	if !ok {
		return nil, false
	}
	return &ta.tmpl, true
}

// Names returns all registered adapter names in sorted order.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
