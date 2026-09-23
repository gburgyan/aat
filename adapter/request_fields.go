package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/gburgyan/aat/internal/gjsonpath"
)

// Where a request field is sent.
const (
	FieldBody   = "body"
	FieldQuery  = "query"
	FieldHeader = "header"
)

// RequestField is a place in the request a template builds: a value of its
// JSON body or gRPC message, a query parameter, or a header.
type RequestField struct {
	// Where is FieldBody, FieldQuery, or FieldHeader.
	Where string
	// Path is the GJSON path of a body value, or the name of a query
	// parameter or header.
	Path string
	// Input is the input the value comes from, when a placeholder that is the
	// whole value fills it; empty for a value the template writes itself.
	Input string
	// Kind is the JSON kind of a literal body value: "string", "number",
	// "boolean", "null", "object", or "array". It is empty for inputs.
	Kind string
	// InBlock is true when the field sits inside a conditional or iteration
	// block, so it may not be sent, or is sent once per element.
	InBlock bool
}

// RequestFields lists the fields of the request the template builds, in the
// order the template writes them. The second result is false when the body
// or message is not a JSON object the scan can read; the query parameters and
// headers are listed either way. Path segments are not fields: leaving one out
// changes the route.
func (t *Template) RequestFields() ([]RequestField, bool) {
	var fields []RequestField
	bodyOK := true
	body := t.Request.Body
	if t.Protocol == "grpc" {
		body = t.Request.Message
	}
	if strings.TrimSpace(body) != "" {
		bodyOK = scanJSONTemplate(body, func(ev jsonEvent) {
			switch ev.kind {
			case eventPlaceholder:
				if ev.whole && !strings.Contains(ev.path, "*") {
					fields = append(fields, RequestField{Where: FieldBody, Path: ev.path, Input: ev.name, InBlock: ev.inBlock})
				}
			case eventLiteral, eventContainer:
				fields = append(fields, RequestField{Where: FieldBody, Path: ev.path, Kind: ev.valueKind, InBlock: ev.inBlock})
			}
		})
		if !bodyOK {
			fields = nil
		}
	} else if t.Request.Form != nil {
		bodyOK = false // a form body has no JSON to patch
	}

	for _, param := range queryParams(t.Request.Path) {
		name, value, _ := strings.Cut(param.text, "=")
		if name == "" || strings.Contains(name, "{{") {
			continue
		}
		f := RequestField{Where: FieldQuery, Path: name, InBlock: param.inBlock}
		if names := placeholderNames(value); len(names) == 1 && strings.TrimSpace(value) == "{{"+names[0]+"}}" {
			f.Input = names[0]
		} else {
			f.Kind = "string"
		}
		fields = append(fields, f)
	}

	headers := t.Request.Headers
	if t.Protocol == "grpc" {
		headers = t.Request.Metadata
	}
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := headers[name]
		if ph := placeholderNames(value); len(ph) == 1 && strings.TrimSpace(value) == "{{"+ph[0]+"}}" {
			fields = append(fields, RequestField{Where: FieldHeader, Path: name, Input: ph[0]})
		}
	}
	return fields, bodyOK
}

// queryParam is one name=value pair of a templated query string, with its
// block tags taken out.
type queryParam struct {
	text string
	// inBlock is true when the pair starts inside a conditional or iteration
	// block.
	inBlock bool
}

// queryParams splits the query of a templated path into its pairs. The query
// starts at the first "?" outside a {{…}} tag, which may itself sit inside a
// block, as in /products{{?category}}?category={{category}}{{/category}}.
// Pairs split at "&" outside tags, and each is in a block when one is open
// where the pair starts.
func queryParams(path string) []queryParam {
	var params []queryParam
	depth := 0
	inQuery := false
	var cur *queryParam
	for i := 0; i < len(path); {
		if strings.HasPrefix(path[i:], "{{") {
			end := strings.Index(path[i+2:], "}}")
			if end < 0 {
				break
			}
			tag := strings.TrimSpace(path[i+2 : i+2+end])
			full := path[i : i+4+end]
			i += 4 + end
			switch {
			case strings.HasPrefix(tag, "?"), strings.HasPrefix(tag, "#"):
				depth++
			case strings.HasPrefix(tag, "/"):
				depth--
			default:
				if inQuery {
					if cur == nil {
						params = append(params, queryParam{inBlock: depth > 0})
						cur = &params[len(params)-1]
					}
					cur.text += full
				}
			}
			continue
		}
		c := path[i]
		i++
		switch {
		case !inQuery:
			inQuery = c == '?'
		case c == '&':
			cur = nil
		default:
			if cur == nil {
				params = append(params, queryParam{inBlock: depth > 0})
				cur = &params[len(params)-1]
			}
			cur.text += string(c)
		}
	}
	return params
}

// Request patch operations.
const (
	PatchRemove = "remove"
	PatchSet    = "set"
)

// ApplyPatch changes a built request: it removes a field, or sets it to value.
// A body path is a GJSON path into the JSON body (or gRPC message); a query or
// header path is the parameter's or header's name. Setting an object key that
// is not there adds it. It returns an error when the body isn't JSON, when a
// path goes through something that isn't there, or for a field to remove that
// isn't there, so a case that can't be built is not sent as something else.
func ApplyPatch(req *Request, where, path, op string, value any) error {
	if op != PatchRemove && op != PatchSet {
		return fmt.Errorf("unknown patch operation %q", op)
	}
	switch where {
	case FieldBody:
		return patchBody(req, path, op, value)
	case FieldQuery:
		base, query, _ := strings.Cut(req.Path, "?")
		params, err := url.ParseQuery(query)
		if err != nil {
			return fmt.Errorf("reading the query: %w", err)
		}
		if op == PatchRemove {
			if _, ok := params[path]; !ok {
				return fmt.Errorf("the query has no parameter %q", path)
			}
			params.Del(path)
		} else {
			params.Set(path, fmt.Sprint(value))
		}
		req.Path = base
		if encoded := params.Encode(); encoded != "" {
			req.Path += "?" + encoded
		}
		return nil
	case FieldHeader:
		if op == PatchRemove {
			found := false
			for k := range req.Headers {
				if strings.EqualFold(k, path) {
					delete(req.Headers, k)
					found = true
				}
			}
			if !found {
				return fmt.Errorf("the request has no header %q", path)
			}
			return nil
		}
		if req.Headers == nil {
			req.Headers = map[string]string{}
		}
		req.Headers[path] = fmt.Sprint(value)
		return nil
	default:
		return fmt.Errorf("unknown patch target %q", where)
	}
}

func patchBody(req *Request, path, op string, value any) error {
	dec := json.NewDecoder(bytes.NewReader(req.Body))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("the body is not JSON: %w", err)
	}
	segs := gjsonpath.Split(path)
	if len(segs) == 0 {
		return fmt.Errorf("empty body path")
	}
	for _, s := range segs {
		if s.Kind != gjsonpath.Key {
			return fmt.Errorf("body path %q: only plain keys and indexes can be patched", path)
		}
	}
	parent := doc
	for _, s := range segs[:len(segs)-1] {
		next, ok := child(parent, s.Key)
		if !ok {
			return fmt.Errorf("the body has nothing at %q", path)
		}
		parent = next
	}
	last := segs[len(segs)-1].Key
	switch p := parent.(type) {
	case map[string]any:
		if op == PatchRemove {
			if _, ok := p[last]; !ok {
				return fmt.Errorf("the body has nothing at %q", path)
			}
			delete(p, last)
		} else {
			p[last] = value
		}
	case []any:
		i, err := strconv.Atoi(last)
		if err != nil || i < 0 || i >= len(p) {
			return fmt.Errorf("the body has nothing at %q", path)
		}
		if op == PatchRemove {
			// Removing an element from its parent needs the parent's parent;
			// setting it to the array without it is the same thing.
			return removeIndex(doc, segs, i, req)
		}
		p[i] = value
	default:
		return fmt.Errorf("the body has nothing at %q", path)
	}
	return encodeBody(req, doc)
}

// removeIndex removes element i of the array at segs[:len(segs)-1].
func removeIndex(doc any, segs []gjsonpath.Segment, i int, req *Request) error {
	if len(segs) == 1 {
		arr := doc.([]any)
		return encodeBody(req, append(arr[:i:i], arr[i+1:]...))
	}
	grand := doc
	for _, s := range segs[:len(segs)-2] {
		grand, _ = child(grand, s.Key)
	}
	key := segs[len(segs)-2].Key
	arr, _ := child(grand, key)
	a := arr.([]any)
	shorter := append(a[:i:i], a[i+1:]...)
	switch g := grand.(type) {
	case map[string]any:
		g[key] = shorter
	case []any:
		j, _ := strconv.Atoi(key)
		g[j] = shorter
	}
	return encodeBody(req, doc)
}

func child(v any, key string) (any, bool) {
	switch c := v.(type) {
	case map[string]any:
		next, ok := c[key]
		return next, ok
	case []any:
		i, err := strconv.Atoi(key)
		if err != nil || i < 0 || i >= len(c) {
			return nil, false
		}
		return c[i], true
	}
	return nil, false
}

func encodeBody(req *Request, doc any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("encoding the patched body: %w", err)
	}
	req.Body = bytes.TrimRight(buf.Bytes(), "\n")
	return nil
}
