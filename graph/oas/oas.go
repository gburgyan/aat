package oas

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	"github.com/pb33f/libopenapi-validator/config"
	"github.com/pb33f/libopenapi/datamodel"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/index"
	"github.com/pb33f/libopenapi/orderedmap"
	"github.com/pb33f/libopenapi/utils"
	"go.yaml.in/yaml/v4"

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

// Load reads an OAS spec from fsPath and stores it under refPath, with a
// validator built for every operation in the spec.
func (c *SpecCache) Load(refPath, fsPath string) error {
	return c.load(refPath, fsPath, nil, true)
}

// LoadOperations is Load for a run that uses only operationIDs. Building a
// validator compiles the schemas of every operation it covers, which takes most
// of a minute for a spec with hundreds of operations, so this validator covers
// only the path items that define one of operationIDs. The stored model still
// has every operation.
func (c *SpecCache) LoadOperations(refPath, fsPath string, operationIDs []string) error {
	return c.load(refPath, fsPath, operationIDs, false)
}

func (c *SpecCache) load(refPath, fsPath string, operationIDs []string, allOperations bool) error {
	data, err := os.ReadFile(fsPath)
	if err != nil {
		return fmt.Errorf("reading OAS spec %q: %w", fsPath, err)
	}

	doc, err := libopenapi.NewDocumentWithConfiguration(data, documentConfiguration())
	if err != nil {
		return fmt.Errorf("parsing OAS spec %q: %w", fsPath, err)
	}

	model, err := buildV3Model(doc)
	if err != nil {
		return fmt.Errorf("building OAS V3 model for %q: %w", fsPath, err)
	}

	// Create the validator once from the pre-built model.
	// This runs warmSchemaCaches once per spec instead of once per step.
	validatorModel := model
	if !allOperations {
		validatorModel = operationsModel(model, operationIDs)
	}
	v := validator.NewValidatorFromV3Model(validatorModel, config.WithBodyDecoder(formMediaType, formBodyDecoder()))

	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[refPath] = &SpecEntry{
		Document:  doc,
		Model:     model,
		Validator: v,
	}
	return nil
}

// documentConfiguration is the libopenapi configuration specs are loaded with:
// the library's defaults without its logger, which writes JSON lines to stdout
// and would mix them into command output such as a graph written to stdout.
func documentConfiguration() *datamodel.DocumentConfiguration {
	cfg := datamodel.NewDocumentConfiguration()
	cfg.Logger = slog.New(slog.DiscardHandler)
	return cfg
}

// buildV3Model builds a document's OpenAPI 3 model. libopenapi reports each
// circular reference as an error but still builds the model, and it renders and
// validates those schemas from the cycles it recorded, so a build whose only
// errors are circular references succeeds. Large real-world specs have such
// cycles, for example an object whose error schema refers back to the object.
// Any other error fails the build.
func buildV3Model(doc libopenapi.Document) (*v3high.Document, error) {
	if info := doc.GetSpecInfo(); info != nil && info.SpecFormat == datamodel.OAS3 {
		allowNullInCompositions(info.RootNode)
	}
	built, err := doc.BuildV3Model()
	if err != nil && (built == nil || !onlyCircularReferences(err)) {
		return nil, err
	}
	return &built.Model, nil
}

// onlyCircularReferences reports whether err, or every error joined in it, is
// a circular reference found while resolving the spec.
func onlyCircularReferences(err error) bool {
	errs := utils.UnwrapErrors(err)
	if len(errs) == 0 {
		return false
	}
	for _, e := range errs {
		var refErr *index.ResolvingError
		if !errors.As(e, &refErr) || refErr.CircularReference == nil {
			return false
		}
	}
	return true
}

// allowNullInCompositions makes each OpenAPI 3.0 schema under node that is
// nullable and built with anyOf or oneOf accept null. nullable applies to the
// whole schema, but the validator adds null only to a schema's own type, allOf,
// and enum, so every alternative still rejected it: a field declared as a
// nullable anyOf of an ID string and an object failed on null. Each such anyOf
// and oneOf gains a {type: "null"} alternative. Aliases are not followed; the
// nodes they point to are visited where they are defined.
func allowNullInCompositions(node *yaml.Node) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range node.Content {
			allowNullInCompositions(child)
		}
	case yaml.MappingNode:
		nullable := false
		var compositions []*yaml.Node
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			switch key.Value {
			case "nullable":
				nullable = value.Kind == yaml.ScalarNode && value.Value == "true"
			case "anyOf", "oneOf":
				if value.Kind == yaml.SequenceNode {
					compositions = append(compositions, value)
				}
			}
			allowNullInCompositions(value)
		}
		if !nullable {
			return
		}
		for _, alternatives := range compositions {
			if !hasNullAlternative(alternatives) {
				alternatives.Content = append(alternatives.Content, &yaml.Node{
					Kind: yaml.MappingNode,
					Tag:  "!!map",
					Content: []*yaml.Node{
						{Kind: yaml.ScalarNode, Tag: "!!str", Value: "type"},
						{Kind: yaml.ScalarNode, Tag: "!!str", Value: "null"},
					},
				})
			}
		}
	}
}

// hasNullAlternative reports whether a list of anyOf or oneOf alternatives
// already has a {type: "null"} alternative.
func hasNullAlternative(alternatives *yaml.Node) bool {
	for _, alt := range alternatives.Content {
		if alt.Kind == yaml.MappingNode && len(alt.Content) == 2 &&
			alt.Content[0].Value == "type" && alt.Content[1].Value == "null" {
			return true
		}
	}
	return false
}

// operationsModel returns a copy of model whose paths hold only the path items
// that define one of operationIDs. The copy shares those path items with model,
// so a step validated against a path item that FindOperation returns from model
// uses the schemas a validator built from the copy compiled.
func operationsModel(model *v3high.Document, operationIDs []string) *v3high.Document {
	if model.Paths == nil || model.Paths.PathItems == nil {
		return model
	}
	wanted := make(map[string]bool, len(operationIDs))
	for _, id := range operationIDs {
		wanted[id] = true
	}

	items := orderedmap.New[string, *v3high.PathItem]()
	for path, pathItem := range model.Paths.PathItems.FromOldest() {
		for _, mo := range PathOperations(pathItem) {
			if wanted[mo.Operation.OperationId] {
				items.Set(path, pathItem)
				break
			}
		}
	}

	pruned := *model
	pruned.Paths = &v3high.Paths{PathItems: items, Extensions: model.Paths.Extensions}
	return &pruned
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

	doc, err := libopenapi.NewDocumentWithConfiguration(data, documentConfiguration())
	if err != nil {
		return nil, fmt.Errorf("parsing OAS spec: %w", err)
	}

	model, err := buildV3Model(doc)
	if err != nil {
		return nil, fmt.Errorf("building OAS V3 model: %w", err)
	}

	return model, nil
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
