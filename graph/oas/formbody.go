package oas

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/pb33f/libopenapi-validator/content"
	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/pb33f/libopenapi/orderedmap"
)

// formMediaType is the media type of a form-encoded body.
const formMediaType = "application/x-www-form-urlencoded"

// maxFormKeyDepth bounds how deeply a form key's brackets nest.
const maxFormKeyDepth = 32

// formEncoding is the per-property encoding a form media type declares.
type formEncoding interface {
	GetOrZero(string) *v3high.Encoding
}

// formBodyDecoder decodes a form-encoded request body into the value its schema
// describes, so the validator checks the fields a request sends. The library's
// own form support rejects reserved characters such as @ and : in decoded values
// and ignores anyOf alternatives when it converts types. See decodeFormBody.
func formBodyDecoder() content.Decoder {
	return content.DecoderFunc(func(in *content.DecodeInput) (any, error) {
		data, err := io.ReadAll(in.Body)
		if err != nil {
			return nil, err
		}
		return decodeFormBody(string(data), in.Schema, in.Encoding)
	})
}

// decodeFormBody decodes a form body into a JSON-style value shaped by schema:
//   - Bracketed keys nest, as the deepObject style writes them: a[b][c]=v is
//     {"a": {"b": {"c": "v"}}}.
//   - A key ending in [], or a repeated key, collects an array. An object whose
//     keys are all indexes, as a[0][b]=v writes, is an array where the schema
//     expects one.
//   - A value becomes an integer, number, or boolean where the schema, or one of
//     its allOf, anyOf, or oneOf alternatives, allows that type and the value
//     parses as it. Otherwise it stays a string.
//   - A property encoded with explode: false splits its value on commas, and one
//     with a JSON contentType is parsed as JSON.
func decodeFormBody(body string, schema *base.Schema, encoding formEncoding) (map[string]any, error) {
	// A media type without encodings passes a nil map, whose GetOrZero panics.
	if m, ok := encoding.(*orderedmap.Map[string, *v3high.Encoding]); ok && m == nil {
		encoding = nil
	}
	values, err := url.ParseQuery(body)
	if err != nil {
		return nil, fmt.Errorf("form body: %w", err)
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	root := map[string]any{}
	for _, key := range keys {
		path, appendValue, err := parseFormKey(key)
		if err != nil {
			return nil, err
		}
		for _, value := range values[key] {
			if err := setFormValue(root, key, path, appendValue, value); err != nil {
				return nil, err
			}
		}
	}

	schemas := expandSchemas([]*base.Schema{schema}, 0)
	if encoding != nil {
		for name, value := range root {
			root[name] = applyFormEncoding(value, encoding.GetOrZero(name), expandSchemas(propertySchemas(schemas, name), 0))
		}
	}
	out := make(map[string]any, len(root))
	for name, value := range root {
		out[name] = convertFormValue(value, propertySchemas(schemas, name), 1)
	}
	return out, nil
}

// parseFormKey splits a form key into its path, a[b][c] into [a b c], and
// reports whether it ends in [], which appends a value. A key that isn't a name
// followed by bracketed segments, such as a[b, is one literal name.
func parseFormKey(key string) (path []string, appendValue bool, err error) {
	open := strings.IndexByte(key, '[')
	if open <= 0 || !strings.HasSuffix(key, "]") {
		return []string{key}, false, nil
	}
	path = []string{key[:open]}
	rest := key[open:]
	for rest != "" {
		end := strings.IndexByte(rest, ']')
		if rest[0] != '[' || end < 0 {
			return []string{key}, false, nil
		}
		segment := rest[1:end]
		rest = rest[end+1:]
		if segment == "" {
			if rest != "" {
				return nil, false, fmt.Errorf("form key %q: [] must come last", key)
			}
			return path, true, nil
		}
		path = append(path, segment)
		if len(path) > maxFormKeyDepth {
			return nil, false, fmt.Errorf("form key %q nests more than %d levels", key, maxFormKeyDepth)
		}
	}
	return path, false, nil
}

// setFormValue stores one form value at path in root. A value set twice at the
// same place, or appended with [], becomes an array.
func setFormValue(root map[string]any, key string, path []string, appendValue bool, value string) error {
	m := root
	for _, segment := range path[:len(path)-1] {
		switch child := m[segment].(type) {
		case nil:
			next := map[string]any{}
			m[segment] = next
			m = next
		case map[string]any:
			m = child
		default:
			return fmt.Errorf("form key %q: %q is both a value and an object", key, segment)
		}
	}

	name := path[len(path)-1]
	switch existing := m[name].(type) {
	case nil:
		if appendValue {
			m[name] = []any{value}
		} else {
			m[name] = value
		}
	case []any:
		m[name] = append(existing, value)
	case string:
		m[name] = []any{existing, value}
	default:
		return fmt.Errorf("form key %q: %q is both an object and a value", key, name)
	}
	return nil
}

// applyFormEncoding applies a top-level property's declared encoding to its
// decoded value: a JSON contentType parses the value as JSON, and explode: false
// splits a single value on commas where the property is an array.
func applyFormEncoding(value any, enc *v3high.Encoding, schemas []*base.Schema) any {
	raw, ok := value.(string)
	if enc == nil || !ok {
		return value
	}
	if strings.Contains(strings.ToLower(enc.ContentType), "json") {
		var parsed any
		if json.Unmarshal([]byte(raw), &parsed) == nil {
			return parsed
		}
		return value
	}
	if enc.Explode != nil && !*enc.Explode && schemasAllow(schemas, "array") {
		parts := strings.Split(raw, ",")
		items := make([]any, len(parts))
		for i, part := range parts {
			items[i] = part
		}
		return items
	}
	return value
}

// convertFormValue converts a decoded form value to the types schemas allow.
func convertFormValue(value any, schemas []*base.Schema, depth int) any {
	if depth > maxFormKeyDepth {
		return value
	}
	schemas = expandSchemas(schemas, 0)
	switch v := value.(type) {
	case string:
		return convertFormScalar(v, schemas)
	case []any:
		return convertFormArray(v, schemas, depth)
	case map[string]any:
		if schemasAllow(schemas, "array") && hasIndexKeys(v) {
			return convertFormArray(indexedValues(v), schemas, depth)
		}
		out := make(map[string]any, len(v))
		for name, child := range v {
			out[name] = convertFormValue(child, propertySchemas(schemas, name), depth+1)
		}
		return out
	}
	return value
}

func convertFormArray(values []any, schemas []*base.Schema, depth int) []any {
	items := itemSchemas(schemas)
	out := make([]any, len(values))
	for i, item := range values {
		out[i] = convertFormValue(item, items, depth+1)
	}
	return out
}

// convertFormScalar converts a form value to an integer, number, or boolean
// when one of schemas allows that type and the value parses as it.
func convertFormScalar(value string, schemas []*base.Schema) any {
	if schemasAllow(schemas, "integer") {
		if n, err := strconv.ParseInt(value, 10, 64); err == nil {
			return n
		}
	}
	if schemasAllow(schemas, "number") {
		if f, err := strconv.ParseFloat(value, 64); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f
		}
	}
	if schemasAllow(schemas, "boolean") && (value == "true" || value == "false") {
		return value == "true"
	}
	return value
}

// expandSchemas returns schemas followed by the allOf, anyOf, and oneOf
// alternatives they compose, to maxShapeDepth, since a spec can be circular.
func expandSchemas(schemas []*base.Schema, depth int) []*base.Schema {
	var out []*base.Schema
	for _, s := range schemas {
		if s == nil {
			continue
		}
		out = append(out, s)
		if depth >= maxShapeDepth {
			continue
		}
		for _, group := range [][]*base.SchemaProxy{s.AllOf, s.AnyOf, s.OneOf} {
			for _, proxy := range group {
				if proxy != nil {
					out = append(out, expandSchemas([]*base.Schema{proxy.Schema()}, depth+1)...)
				}
			}
		}
	}
	return out
}

// schemasAllow reports whether one of schemas declares type t.
func schemasAllow(schemas []*base.Schema, t string) bool {
	for _, s := range schemas {
		for _, declared := range s.Type {
			if declared == t {
				return true
			}
		}
	}
	return false
}

// propertySchemas returns the schemas of property name: the declared property
// of each schema, or its additionalProperties schema where it declares none.
func propertySchemas(schemas []*base.Schema, name string) []*base.Schema {
	var out []*base.Schema
	for _, s := range schemas {
		if s.Properties != nil {
			if property := s.Properties.GetOrZero(name); property != nil {
				out = append(out, property.Schema())
				continue
			}
		}
		if extra := s.AdditionalProperties; extra != nil && extra.IsA() && extra.A != nil {
			out = append(out, extra.A.Schema())
		}
	}
	return out
}

// itemSchemas returns the item schemas of the array schemas among schemas.
func itemSchemas(schemas []*base.Schema) []*base.Schema {
	var out []*base.Schema
	for _, s := range schemas {
		if s.Items != nil && s.Items.IsA() && s.Items.A != nil {
			out = append(out, s.Items.A.Schema())
		}
	}
	return out
}

// hasIndexKeys reports whether every key of m is an array index: 0, 1, 2, ...
func hasIndexKeys(m map[string]any) bool {
	if len(m) == 0 {
		return false
	}
	for key := range m {
		n, err := strconv.Atoi(key)
		if err != nil || n < 0 || strconv.Itoa(n) != key {
			return false
		}
	}
	return true
}

// indexedValues returns the values of an object with index keys, in index order.
func indexedValues(m map[string]any) []any {
	indexes := make([]int, 0, len(m))
	for key := range m {
		n, _ := strconv.Atoi(key)
		indexes = append(indexes, n)
	}
	sort.Ints(indexes)
	values := make([]any, len(indexes))
	for i, n := range indexes {
		values[i] = m[strconv.Itoa(n)]
	}
	return values
}
