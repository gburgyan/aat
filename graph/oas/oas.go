package oas

import (
	"fmt"
	"os"

	"github.com/pb33f/libopenapi"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/gburgyan/aat/graph"
)

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
// Returns the HTTP method, path, and operation. Returns an error if not found.
func FindOperation(model *v3high.Document, operationID string) (method string, path string, op *v3high.Operation, err error) {
	if model.Paths == nil {
		return "", "", nil, fmt.Errorf("operationId %q not found: spec has no paths", operationID)
	}

	for pathStr, pathItem := range model.Paths.PathItems.FromOldest() {
		type methodOp struct {
			method string
			op     *v3high.Operation
		}
		candidates := []methodOp{
			{"GET", pathItem.Get},
			{"POST", pathItem.Post},
			{"PUT", pathItem.Put},
			{"DELETE", pathItem.Delete},
			{"PATCH", pathItem.Patch},
		}
		for _, c := range candidates {
			if c.op != nil && c.op.OperationId == operationID {
				return c.method, pathStr, c.op, nil
			}
		}
	}

	return "", "", nil, fmt.Errorf("operationId %q not found in spec", operationID)
}

// ResolveNodeSpec returns the effective OAS spec path for a node.
// The node-level spec overrides the graph-level default. Returns empty string if neither is set.
func ResolveNodeSpec(node *graph.Node, graphOAS string) string {
	if node.OAS != nil && node.OAS.Spec != "" {
		return node.OAS.Spec
	}
	return graphOAS
}
