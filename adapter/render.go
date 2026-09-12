package adapter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

// renderContext is where a template string is rendered. It decides how a
// substituted value is escaped, so a value cannot change the structure of the
// request around it.
type renderContext int

const (
	// renderRaw inserts values as text: header values, and bodies that are
	// neither JSON nor form-encoded.
	renderRaw renderContext = iota
	// renderPath URL-encodes each value: as one path segment before the
	// template's first literal "?", and as a query component after it.
	renderPath
	// renderJSON escapes a value inside a JSON string literal and writes it as a
	// JSON value outside one.
	renderJSON
	// renderForm URL-encodes each value of an application/x-www-form-urlencoded
	// body.
	renderForm
)

// elementTokenPrefix starts the key of the placeholder an iteration block
// leaves for each element value ({{\x00N}}), so element values are escaped by
// the same pass as every other placeholder.
const elementTokenPrefix = "\x00"

// elementPlaceholder appends v to elements and returns the placeholder that
// stands for it.
func elementPlaceholder(elements *[]any, v any) string {
	*elements = append(*elements, v)
	return "{{" + elementTokenPrefix + strconv.Itoa(len(*elements)-1) + "}}"
}

// placeholderValue looks up the value of a placeholder key: an element an
// iteration block left, or a step input.
func placeholderValue(key string, inputs map[string]any, elements []any) (any, bool) {
	if index, ok := strings.CutPrefix(key, elementTokenPrefix); ok {
		i, err := strconv.Atoi(index)
		if err != nil || i < 0 || i >= len(elements) {
			return nil, false
		}
		return elements[i], true
	}
	v, ok := inputs[key]
	return v, ok
}

// bodyContext decides how values are escaped in a request body: by the
// Content-Type the request is sent with or, without one, by whether the body
// template looks like JSON.
func bodyContext(headers map[string]string, body string) renderContext {
	for name, value := range headers {
		if !strings.EqualFold(name, "Content-Type") {
			continue
		}
		contentType := strings.ToLower(value)
		switch {
		case strings.Contains(contentType, "json"):
			return renderJSON
		case strings.Contains(contentType, "x-www-form-urlencoded"):
			return renderForm
		default:
			return renderRaw
		}
	}
	if trimmed := strings.TrimSpace(body); strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return renderJSON
	}
	return renderRaw
}

// contextScanner follows the literal text of a template between placeholders
// and escapes each value for the position it fills.
type contextScanner struct {
	ctx      renderContext
	inQuery  bool // renderPath: a literal "?" has been seen
	inString bool // renderJSON: inside a string literal
	escaped  bool // renderJSON: the previous character was a backslash in a string
}

// feed advances the scanner over literal template text. Substituted values are
// not fed: each is balanced for its position, so it cannot move the scanner
// out of a string or into the query.
func (s *contextScanner) feed(literal string) {
	switch s.ctx {
	case renderPath:
		if strings.Contains(literal, "?") {
			s.inQuery = true
		}
	case renderJSON:
		for i := 0; i < len(literal); i++ {
			switch c := literal[i]; {
			case s.escaped:
				s.escaped = false
			case s.inString && c == '\\':
				s.escaped = true
			case c == '"':
				s.inString = !s.inString
			}
		}
	}
}

// escape renders v for the position the scanner is at.
func (s *contextScanner) escape(v any) string {
	switch s.ctx {
	case renderPath:
		if s.inQuery {
			return url.QueryEscape(formatValue(v))
		}
		return url.PathEscape(formatValue(v))
	case renderForm:
		return url.QueryEscape(formatValue(v))
	case renderJSON:
		if s.inString {
			return jsonStringContent(formatValue(v))
		}
		return jsonValue(v)
	default:
		return formatValue(v)
	}
}

// formatValue returns the text of a substituted value: a string as it is, a
// number in plain digits (never exponent form), arrays, maps, and structs as
// JSON, and nil as nothing.
func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(val), 'f', -1, 32)
	case json.Number:
		return val.String()
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.Struct:
		if text, err := marshalJSON(v); err == nil {
			return text
		}
	}
	return fmt.Sprintf("%v", v)
}

// jsonValue writes v where a JSON value is expected, outside any string
// literal. A string goes in as it is, so a template can build JSON text from a
// value; nil is null, and every other value is written as JSON.
func jsonValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case nil:
		return "null"
	case float64, float32, json.Number:
		return formatValue(val)
	}
	if text, err := marshalJSON(v); err == nil {
		return text
	}
	return formatValue(v)
}

// jsonStringContent returns s escaped for the inside of a JSON string literal,
// without the surrounding quotes. HTML characters stay as they are.
func jsonStringContent(s string) string {
	text, _ := marshalJSON(s) // a string always encodes
	return text[1 : len(text)-1]
}

// marshalJSON encodes v as compact JSON without HTML escaping.
func marshalJSON(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}
