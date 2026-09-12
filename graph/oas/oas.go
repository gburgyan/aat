package oas

import (
	"fmt"
	"os"
	"sync"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/gburgyan/aat/graph"
)

// SpecEntry holds the raw document, parsed model, and a pre-built validator for runtime validation.
type SpecEntry struct {
	Document  libopenapi.Document // needed by libopenapi-validator
	Model     *v3high.Document    // used by existing OAS functions (FindOperation, etc.)
	Validator validator.Validator // created once per spec, reused for all steps; safe for concurrent use
}

// SpecCache loads and caches OAS specs by reference path.
type SpecCache struct {
	mu      sync.RWMutex
	entries map[string]*SpecEntry
}

// NewSpecCache creates an empty SpecCache.
func NewSpecCache() *SpecCache {
	return &SpecCache{entries: make(map[string]*SpecEntry)}
}

// Load reads an OAS spec from fsPath and stores it under refPath.
func (c *SpecCache) Load(refPath, fsPath string) error {
	data, err := os.ReadFile(fsPath)
	if err != nil {
		return fmt.Errorf("reading OAS spec %q: %w", fsPath, err)
	}

	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		return fmt.Errorf("parsing OAS spec %q: %w", fsPath, err)
	}

	v3Model, err := doc.BuildV3Model()
	if err != nil {
		return fmt.Errorf("building OAS V3 model for %q: %w", fsPath, err)
	}

	// Create the validator once from the pre-built model.
	// This runs warmSchemaCaches once per spec instead of once per step.
	v := validator.NewValidatorFromV3Model(&v3Model.Model)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[refPath] = &SpecEntry{
		Document:  doc,
		Model:     &v3Model.Model,
		Validator: v,
	}
	return nil
}

// Get returns the SpecEntry for the given reference path, or nil if not loaded.
func (c *SpecCache) Get(refPath string) *SpecEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.entries[refPath]
}

// Len returns the number of loaded specs.
func (c *SpecCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// LoadSpec loads and parses an OpenAPI spec file, returning the high-level V3 model.
func LoadSpec(path string) (*v3high.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading OAS spec: %w", err)
	}

	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		return nil, fmt.Errorf("parsing OAS spec: %w", err)
	}

	v3Model, err := doc.BuildV3Model()
	if err != nil {
		return nil, fmt.Errorf("building OAS V3 model: %w", err)
	}

	return &v3Model.Model, nil
}

// FindOperation looks up an operation by operationId across all paths in the spec.
// Returns the HTTP method, path, path item, and operation. Returns an error if not found.
func FindOperation(model *v3high.Document, operationID string) (method string, path string, pathItem *v3high.PathItem, op *v3high.Operation, err error) {
	if model.Paths == nil {
		return "", "", nil, nil, fmt.Errorf("operationId %q not found: spec has no paths", operationID)
	}

	for pathStr, pi := range model.Paths.PathItems.FromOldest() {
		for _, mo := range PathOperations(pi) {
			if mo.Operation.OperationId == operationID {
				return mo.Method, pathStr, pi, mo.Operation, nil
			}
		}
	}

	return "", "", nil, nil, fmt.Errorf("operationId %q not found in spec", operationID)
}

// MethodOperation is one operation of a path item and its HTTP method.
type MethodOperation struct {
	Method    string
	Operation *v3high.Operation
}

// PathOperations returns the operations a path item defines, in the order GET,
// POST, PUT, DELETE, PATCH, HEAD, OPTIONS, TRACE.
func PathOperations(pi *v3high.PathItem) []MethodOperation {
	if pi == nil {
		return nil
	}
	var ops []MethodOperation
	for _, mo := range []MethodOperation{
		{Method: "GET", Operation: pi.Get},
		{Method: "POST", Operation: pi.Post},
		{Method: "PUT", Operation: pi.Put},
		{Method: "DELETE", Operation: pi.Delete},
		{Method: "PATCH", Operation: pi.Patch},
		{Method: "HEAD", Operation: pi.Head},
		{Method: "OPTIONS", Operation: pi.Options},
		{Method: "TRACE", Operation: pi.Trace},
	} {
		if mo.Operation != nil {
			ops = append(ops, mo)
		}
	}
	return ops
}

// OperationParameters returns the parameters that apply to an operation: the
// ones declared on its path item followed by the operation's own, where an
// operation parameter replaces a path-item parameter with the same name and
// location (OAS 3.0 §4.7.9). Path-item parameters are how most specs declare
// shared path variables such as /carts/{cartId}.
func OperationParameters(pathItem *v3high.PathItem, op *v3high.Operation) []*v3high.Parameter {
	var params []*v3high.Parameter
	index := make(map[string]int)
	add := func(p *v3high.Parameter) {
		if p == nil {
			return
		}
		key := p.In + ":" + p.Name
		if i, ok := index[key]; ok {
			params[i] = p
			return
		}
		index[key] = len(params)
		params = append(params, p)
	}
	if pathItem != nil {
		for _, p := range pathItem.Parameters {
			add(p)
		}
	}
	if op != nil {
		for _, p := range op.Parameters {
			add(p)
		}
	}
	return params
}

// ResolveNodeSpec returns the effective OAS spec path for a node.
// The node-level spec overrides the graph-level default. Returns empty string if neither is set.
func ResolveNodeSpec(node *graph.Node, graphOAS string) string {
	if node.OAS != nil && node.OAS.Spec != "" {
		return node.OAS.Spec
	}
	return graphOAS
}
