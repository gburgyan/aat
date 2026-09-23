package adapter

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/gburgyan/aat/internal/gjsonpath"
)

// MessageInputPaths maps each input a gRPC template sends to where its message
// puts it: the GJSON path of the value the placeholder is, or is inside. An
// input used as a key is placed at its object's path with a "*" for the key; an
// iteration block's list at the path of the array or object it fills. An input
// that reaches the request with no field to check, only in metadata, maps to an
// empty list.
//
// The second result is false when the message can't be read as one JSON object
// with placeholders in it — unbalanced brackets, an unterminated string or
// placeholder — and then the map holds nothing to rely on.
func (t *Template) MessageInputPaths() (map[string][]string, bool) {
	paths, ok := jsonPlaceholderPaths(t.Request.Message)
	if !ok {
		return nil, false
	}
	for _, value := range t.Request.Metadata {
		for _, name := range placeholderNames(value) {
			if _, placed := paths[name]; !placed {
				paths[name] = []string{}
			}
		}
	}
	return paths, true
}

// jsonFrame is an object or array the scan is inside.
type jsonFrame struct {
	array bool
	// index is the element an array is on.
	index int
	// key is the segment of the value an object is on, and keyNext says the
	// next string is a key rather than a value.
	key     string
	keyNext bool
}

// jsonPlaceholderPaths scans a JSON template for placeholders, tolerating
// what makes a template not JSON yet: conditional and iteration tags between
// members, and placeholders standing for whole values.
func jsonPlaceholderPaths(text string) (map[string][]string, bool) {
	paths := make(map[string][]string)
	ok := scanJSONTemplate(text, func(ev jsonEvent) {
		if ev.kind != eventPlaceholder {
			return
		}
		for _, p := range paths[ev.name] {
			if p == ev.path {
				return
			}
		}
		paths[ev.name] = append(paths[ev.name], ev.path)
	})
	if !ok {
		return nil, false
	}
	return paths, true
}

// jsonEventKind says what a JSON template scan found.
type jsonEventKind int

const (
	// eventPlaceholder is an input a value (or a key) holds.
	eventPlaceholder jsonEventKind = iota
	// eventLiteral is a value the template writes itself: a string without
	// placeholders, a number, true, false, or null.
	eventLiteral
	// eventContainer is an object or array value.
	eventContainer
)

// jsonEvent is one thing a JSON template scan found, at a GJSON path.
type jsonEvent struct {
	kind jsonEventKind
	path string
	// name is the input, for a placeholder.
	name string
	// valueKind is "string", "number", "boolean", "null", "object", or
	// "array", for a literal or a container.
	valueKind string
	// whole is true for a placeholder that is the entire value: {{n}} or
	// "{{s}}", not part of a longer string or a key.
	whole bool
	// inBlock is true inside a conditional or iteration block, where the
	// value may not be sent, or is sent once per element.
	inBlock bool
}

// scanJSONTemplate walks a JSON template, calling visit for each placeholder,
// literal value, and container it finds. It returns false when the text
// can't be read as one JSON object with placeholders in it.
func scanJSONTemplate(text string, visit func(jsonEvent)) bool {
	var stack []*jsonFrame
	blocks := 0
	// valuePath is the path of the value the scan is at: each frame's key or
	// index, from the outside in.
	valuePath := func() string {
		segs := make([]string, 0, len(stack))
		for _, f := range stack {
			if f.array {
				segs = append(segs, strconv.Itoa(f.index))
			} else {
				segs = append(segs, f.key)
			}
		}
		return strings.Join(segs, ".")
	}
	// containerPath is the path of the object or array the scan is in.
	containerPath := func() string {
		saved := stack
		stack = stack[:len(stack)-1]
		p := valuePath()
		stack = saved
		return p
	}
	top := func() *jsonFrame { return stack[len(stack)-1] }
	join := func(parent, seg string) string {
		if parent == "" {
			return seg
		}
		return parent + "." + seg
	}
	emit := func(ev jsonEvent) {
		ev.inBlock = blocks > 0
		visit(ev)
	}

	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "{") {
		return false
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c == '{' && strings.HasPrefix(text[i:], "{{"):
			end := strings.Index(text[i+2:], "}}")
			if end < 0 {
				return false
			}
			tag := strings.TrimSpace(text[i+2 : i+2+end])
			i += end + 3
			if len(stack) == 0 {
				return false
			}
			switch {
			case strings.HasPrefix(tag, "?"):
				blocks++ // a block's content stays where it is
			case strings.HasPrefix(tag, "/"):
				if blocks > 0 {
					blocks--
				}
			case strings.HasPrefix(tag, "#"):
				// The list fills the array or object the block is in.
				emit(jsonEvent{kind: eventPlaceholder, name: strings.TrimSpace(tag[1:]), path: containerPath()})
				blocks++
			case isElementRef(tag):
				// The element an iteration block is on, not an input.
			case !top().array && top().keyNext:
				// A placeholder written where a key goes: any key of the object.
				emit(jsonEvent{kind: eventPlaceholder, name: tag, path: join(containerPath(), "*")})
				top().keyNext = false
			default:
				emit(jsonEvent{kind: eventPlaceholder, name: tag, path: valuePath(), whole: true})
			}
		case c == '"':
			end, names, ok := scanJSONString(text, i)
			if !ok {
				return false
			}
			if len(stack) == 0 {
				return false
			}
			f := top()
			if !f.array && f.keyNext {
				f.keyNext = false
				key, isLiteral := jsonKey(text[i : end+1])
				if !isLiteral {
					for _, name := range names {
						if !isElementRef(name) {
							emit(jsonEvent{kind: eventPlaceholder, name: name, path: join(containerPath(), "*")})
						}
					}
					f.key = "*"
				} else {
					f.key = gjsonpath.KeySegment(key).Raw
				}
			} else {
				raw := text[i : end+1]
				if len(names) == 0 {
					emit(jsonEvent{kind: eventLiteral, path: valuePath(), valueKind: "string"})
				}
				for _, name := range names {
					if !isElementRef(name) {
						emit(jsonEvent{kind: eventPlaceholder, name: name, path: valuePath(), whole: raw == `"{{`+name+`}}"`})
					}
				}
			}
			i = end
		case c == '{':
			if len(stack) > 0 {
				emit(jsonEvent{kind: eventContainer, path: valuePath(), valueKind: "object"})
			}
			stack = append(stack, &jsonFrame{keyNext: true})
		case c == '[':
			if len(stack) > 0 {
				emit(jsonEvent{kind: eventContainer, path: valuePath(), valueKind: "array"})
			}
			stack = append(stack, &jsonFrame{array: true})
		case c == '}' || c == ']':
			if len(stack) == 0 || top().array != (c == ']') {
				return false
			}
			stack = stack[:len(stack)-1]
		case c == ',':
			if len(stack) == 0 {
				return false
			}
			if f := top(); f.array {
				f.index++
			} else {
				f.keyNext, f.key = true, ""
			}
		case c == '-' || (c >= '0' && c <= '9') || c == 't' || c == 'f' || c == 'n':
			// A bare literal: a number, true, false, or null.
			j := i
			for j < len(text) && !strings.ContainsRune(",}] \t\r\n", rune(text[j])) && !strings.HasPrefix(text[j:], "{{") {
				j++
			}
			if len(stack) > 0 {
				kind := "number"
				switch text[i:j] {
				case "true", "false":
					kind = "boolean"
				case "null":
					kind = "null"
				}
				emit(jsonEvent{kind: eventLiteral, path: valuePath(), valueKind: kind})
			}
			i = j - 1
		}
	}
	return len(stack) == 0
}

// scanJSONString reads the JSON string starting at text[start], a quote. It
// returns the index of the closing quote and the names of the placeholders
// inside, element references included.
func scanJSONString(text string, start int) (int, []string, bool) {
	var names []string
	for i := start + 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			i++
		case '"':
			return i, names, true
		case '{':
			if strings.HasPrefix(text[i:], "{{") {
				end := strings.Index(text[i+2:], "}}")
				if end < 0 {
					return 0, nil, false
				}
				tag := strings.TrimSpace(text[i+2 : i+2+end])
				if tag != "" && !strings.ContainsAny(tag[:1], "?/#") {
					names = append(names, tag)
				}
				i += end + 3
			}
		}
	}
	return 0, nil, false
}

// jsonKey decodes a quoted JSON key. A key holding a placeholder is not a
// literal: the input picks the key.
func jsonKey(quoted string) (string, bool) {
	if strings.Contains(quoted, "{{") {
		return "", false
	}
	var key string
	if err := json.Unmarshal([]byte(quoted), &key); err != nil {
		return strings.Trim(quoted, `"`), true
	}
	return key, true
}

// isElementRef reports whether a placeholder names the element an iteration
// block is on: {{.}}, {{.field}}, or {{@index}}.
func isElementRef(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@")
}

// placeholderNames returns the inputs a text's placeholders name, leaving out
// block tags and element references.
func placeholderNames(text string) []string {
	var names []string
	for {
		start := strings.Index(text, "{{")
		if start < 0 {
			return names
		}
		end := strings.Index(text[start+2:], "}}")
		if end < 0 {
			return names
		}
		tag := strings.TrimSpace(text[start+2 : start+2+end])
		if tag != "" && !strings.ContainsAny(tag[:1], "?/#.@") {
			names = append(names, tag)
		}
		text = text[start+2+end+2:]
	}
}
