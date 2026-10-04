package adapter

import "strings"

// DropsEmpty reports whether the request the template builds leaves input out
// wherever it would send it as "": every placeholder of the input is inside a
// {{?input}} block, or is the whole value of a header (or gRPC metadata) or
// of a request.form field, all of which the builder leaves out for an empty
// value. A case that sends "" to such an input sends what leaving it out
// sends. An input the template doesn't send at all is not dropped.
func (t *Template) DropsEmpty(input string) bool {
	found := false
	for _, text := range t.requestTexts() {
		_, whole := wholePlaceholder(text.value)
		dropped := text.wholeDrops && whole
		for _, guarded := range placeholderGuards(text.value, input) {
			found = true
			if !guarded && !dropped {
				return false
			}
		}
	}
	return found
}

// SendsBare reports whether the template writes input's placeholder outside a
// JSON string in its body or message, as in {"quantity": {{quantity}}}, where
// a string value goes in as JSON text rather than as a string.
func (t *Template) SendsBare(input string) bool {
	body := t.Request.Body
	if t.Protocol == ProtocolGRPC {
		body = t.Request.Message
	}
	bare := false
	if strings.TrimSpace(body) != "" {
		scanJSONTemplate(body, func(ev jsonEvent) {
			bare = bare || ev.kind == eventPlaceholder && ev.bare && ev.name == input
		})
	}
	return bare
}

// HeaderRefuses reports whether a header (gRPC metadata) the template fills
// from input could not carry value: the client would refuse the request
// before sending it, as HTTP refuses a control character and gRPC anything
// but printable ASCII in a value whose key doesn't end in -bin.
func (t *Template) HeaderRefuses(input string, value string) bool {
	headers := t.Request.Headers
	if t.Protocol == ProtocolGRPC {
		headers = t.Request.Metadata
	}
	for name, text := range headers {
		if len(placeholderGuards(text, input)) == 0 {
			continue
		}
		var err error
		if t.Protocol == ProtocolGRPC {
			err = metadataError(strings.ToLower(name), value)
		} else {
			err = headerError(name, value)
		}
		if err != nil {
			return true
		}
	}
	return false
}

// requestText is one text of a template's request, and whether the builder
// leaves out a value of it that is one placeholder when that input is empty.
type requestText struct {
	value      string
	wholeDrops bool
}

// requestTexts returns every text the template's request is built from: its
// path, body or message, headers or metadata, and form values.
func (t *Template) requestTexts() []requestText {
	texts := []requestText{{value: t.Request.Path}, {value: t.Request.Body}, {value: t.Request.Message}}
	headers := t.Request.Headers
	if t.Protocol == ProtocolGRPC {
		headers = t.Request.Metadata
	}
	for _, v := range headers {
		texts = append(texts, requestText{value: v, wholeDrops: true})
	}
	for _, v := range t.Request.Form.texts() {
		texts = append(texts, requestText{value: v, wholeDrops: true})
	}
	return texts
}

// placeholderGuards returns, for each placeholder of input in text, whether a
// {{?input}} block encloses it, so the builder leaves it out when the input
// is empty. A block for several inputs, {{?a|input}}, does not guard it: the
// block is sent when another of them has a value.
func placeholderGuards(text, input string) []bool {
	var guards []bool
	var open []string // the keys of the blocks enclosing the scan, innermost last
	for {
		start := strings.Index(text, "{{")
		if start < 0 {
			return guards
		}
		end := strings.Index(text[start+2:], "}}")
		if end < 0 {
			return guards
		}
		tag := strings.TrimSpace(text[start+2 : start+2+end])
		text = text[start+2+end+2:]
		switch {
		case tag == "":
		case tag[0] == '?' || tag[0] == '#':
			open = append(open, tag)
		case tag[0] == '/':
			if len(open) > 0 {
				open = open[:len(open)-1]
			}
		case tag == input:
			guarded := false
			for _, block := range open {
				guarded = guarded || block == "?"+input
			}
			guards = append(guards, guarded)
		}
	}
}
