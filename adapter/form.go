package adapter

import (
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/gburgyan/aat/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// FormContentType is the Content-Type a template's request.form is sent with.
const FormContentType = "application/x-www-form-urlencoded"

// FormFields is a template's request.form: the fields of a form-encoded body,
// in the order the template writes them.
//   - A nested mapping writes bracketed keys: metadata: {source: web} sends
//     metadata[source]=web.
//   - A list sends one pair per item under the key as written, and a mapping in
//     a list writes indexed keys, such as items[0][sku].
//   - A value that is exactly one placeholder sends that input's value: nothing
//     when the input has none, one pair for a scalar, a repeated pair for each
//     element of a list, and bracketed keys for a map.
//   - Any other value is rendered as text, and sent unless it held only
//     conditional blocks that rendered to nothing.
//
// Keys and values are URL-encoded, except the brackets of a key.
type FormFields []FormField

// FormField is one field of a form body. It holds text in Value, a nested
// mapping in Fields, or a list in Items, whose entries have no Key. Line is the
// template line that writes it.
type FormField struct {
	Key    string
	Line   int
	Value  string
	Fields FormFields
	Items  []FormField
}

// UnmarshalYAML decodes request.form. It walks the mapping itself to keep the
// fields in order, so it also checks what decoding into a map would: duplicate
// keys, and keys that nesting would send twice. It uses the callback form so
// strict decoding reaches it (see internal/yamlx).
func (f *FormFields) UnmarshalYAML(unmarshal func(any) error) error {
	n, err := yamlx.Node(unmarshal)
	if err != nil {
		return err
	}
	var p formParser
	fields := p.mapping(n, "request.form")
	p.checkKeys(fields, "", make(map[string]int))
	if len(p.errs) > 0 {
		return &yaml.TypeError{Errors: p.errs}
	}
	*f = fields
	return nil
}

// formParser collects the problems in a request.form mapping as "line N: ..."
// messages, the form yamlx reports.
type formParser struct {
	errs []string
}

func (p *formParser) fail(line int, format string, args ...any) {
	p.errs = append(p.errs, fmt.Sprintf("line %d: %s", line, fmt.Sprintf(format, args...)))
}

// mapping decodes a mapping of form fields; what names the mapping when the node
// is something else.
func (p *formParser) mapping(n *yaml.Node, what string) FormFields {
	if n.Kind != yaml.MappingNode {
		var typeErr *yaml.TypeError
		if errors.As(yamlx.KindError(n, what, "a mapping of field names to values"), &typeErr) {
			p.errs = append(p.errs, typeErr.Errors...)
		}
		return nil
	}
	fields := FormFields{}
	seen := make(map[string]int)
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		switch {
		case k.Kind != yaml.ScalarNode:
			p.fail(k.Line, "a form field name must be text")
			continue
		case k.ShortTag() == "!!merge":
			p.fail(k.Line, "merge keys (<<) aren't allowed in a form; write each field")
			continue
		case k.Value == "":
			p.fail(k.Line, "a form field needs a name")
			continue
		case strings.Contains(k.Value, "{{"):
			p.fail(k.Line, "form field %q: a field name can't hold a placeholder", k.Value)
			continue
		}
		if first, dup := seen[k.Value]; dup {
			p.fail(k.Line, "duplicate form field %q (first at line %d)", k.Value, first)
			continue
		}
		seen[k.Value] = k.Line
		field := FormField{Key: k.Value, Line: k.Line}
		p.value(&field, v, k.Value)
		fields = append(fields, field)
	}
	return fields
}

// value decodes the value of the form field named key into field.
func (p *formParser) value(field *FormField, n *yaml.Node, key string) {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.ShortTag() == "!!null" {
			p.fail(n.Line, "form field %q has no value; write \"\" to send it empty, or leave the field out", key)
			return
		}
		field.Value = n.Value
		p.checkText(n.Line, key, n.Value)
	case yaml.MappingNode:
		field.Fields = p.mapping(n, fmt.Sprintf("form field %q", key))
	case yaml.SequenceNode:
		field.Items = []FormField{}
		for _, item := range n.Content {
			if item.Kind == yaml.SequenceNode {
				p.fail(item.Line, "form field %q: a list item must be a value or a mapping, not a list", key)
				continue
			}
			entry := FormField{Line: item.Line}
			p.value(&entry, item, key)
			field.Items = append(field.Items, entry)
		}
	case yaml.AliasNode:
		p.fail(n.Line, "form field %q: aliases aren't allowed in a form; write the value", key)
	}
}

// checkText reports template syntax a form value can't hold: iteration blocks
// and the element placeholders they use, which a list input replaces, and
// conditional blocks that aren't closed.
func (p *formParser) checkText(line int, key, text string) {
	if m := iterOpenRe.FindStringSubmatch(text); m != nil {
		p.fail(line, "form field %q: iteration blocks aren't allowed in a form; write %s: \"{{%s}}\", which sends one pair per element", key, key, m[1])
		return
	}
	for _, m := range placeholderRe.FindAllStringSubmatch(text, -1) {
		if name := strings.TrimSpace(m[1]); strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") {
			p.fail(line, "form field %q: {{%s}} works only inside an iteration block, which a form doesn't use", key, name)
			return
		}
	}
	if _, err := expandConditionalBlocks(text, nil); err != nil {
		p.fail(line, "form field %q: %v", key, err)
	}
}

// checkKeys reports two fields that send the same key, such as
// metadata[source] beside metadata: {source: ...}. A key ending in [] repeats
// by design.
func (p *formParser) checkKeys(fields FormFields, prefix string, seen map[string]int) {
	for _, field := range fields {
		key := formKey(prefix, field.Key)
		switch {
		case field.Fields != nil:
			p.checkKeys(field.Fields, key, seen)
			continue
		case field.Items != nil:
			scalars := false
			for i, item := range field.Items {
				if item.Fields != nil {
					p.checkKeys(item.Fields, indexedKey(key, i), seen)
				} else {
					scalars = true
				}
			}
			if !scalars {
				continue
			}
		}
		if strings.HasSuffix(key, "[]") {
			continue
		}
		if first, dup := seen[key]; dup {
			p.fail(field.Line, "form field %q is also written at line %d", key, first)
			continue
		}
		seen[key] = field.Line
	}
}

// formKey returns the key a field named key sends inside prefix: key itself at
// the top level, and otherwise prefix[name] followed by any brackets key has,
// so tags[] inside metadata is metadata[tags][].
func formKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	name, rest := key, ""
	if i := strings.IndexByte(key, '['); i >= 0 {
		name, rest = key[:i], key[i:]
	}
	return prefix + "[" + name + "]" + rest
}

// indexedKey returns the key of the element at index i of a list sent under
// key: items[0] for items or items[].
func indexedKey(key string, i int) string {
	return strings.TrimSuffix(key, "[]") + "[" + strconv.Itoa(i) + "]"
}

// formFieldName returns the field a key fills: the name before its first
// bracket, so metadata[source] fills metadata.
func formFieldName(key string) string {
	name, _, _ := strings.Cut(key, "[")
	return name
}

// render returns the form body the fields send with inputs.
func (f FormFields) render(inputs map[string]any) (string, error) {
	var pairs []string
	if err := f.appendPairs("", inputs, &pairs); err != nil {
		return "", err
	}
	return strings.Join(pairs, "&"), nil
}

// appendPairs appends the encoded pairs the fields send inside prefix.
func (f FormFields) appendPairs(prefix string, inputs map[string]any, pairs *[]string) error {
	for _, field := range f {
		key := formKey(prefix, field.Key)
		switch {
		case field.Fields != nil:
			if err := field.Fields.appendPairs(key, inputs, pairs); err != nil {
				return err
			}
		case field.Items != nil:
			for i, item := range field.Items {
				var err error
				if item.Fields != nil {
					err = item.Fields.appendPairs(indexedKey(key, i), inputs, pairs)
				} else {
					err = appendFormText(key, item.Value, inputs, pairs)
				}
				if err != nil {
					return err
				}
			}
		default:
			if err := appendFormText(key, field.Value, inputs, pairs); err != nil {
				return err
			}
		}
	}
	return nil
}

// appendFormText appends the pairs a text value sends under key. A value that
// is one placeholder sends its input's value, or nothing when the input has
// none; other text is rendered, and left out when only conditional blocks were
// there and they rendered to nothing.
func appendFormText(key, text string, inputs map[string]any, pairs *[]string) error {
	if name, ok := wholePlaceholder(text); ok {
		if valuePresent(inputs, name) {
			appendFormValue(key, inputs[name], pairs)
		}
		return nil
	}
	rendered, err := substitutePlaceholders(text, inputs, renderRaw)
	if err != nil {
		return fmt.Errorf("form field %q: %w", key, err)
	}
	if rendered == "" && condOpenRe.MatchString(text) {
		return nil
	}
	*pairs = append(*pairs, formPair(key, rendered))
	return nil
}

// appendFormValue appends the pairs an input value sends under key: one pair
// for a scalar; key[name] pairs for a map, in name order; and for a list, a pair
// under key for each scalar element and key[i] pairs for each map or list in
// it. A nil value sends nothing.
func appendFormValue(key string, v any, pairs *[]string) {
	if v == nil {
		return
	}
	if names, entries, ok := mapEntries(v); ok {
		for _, name := range names {
			appendFormValue(key+"["+name+"]", entries[name], pairs)
		}
		return
	}
	if elems, ok := listElements(v); ok {
		for i, elem := range elems {
			if isNestedValue(elem) {
				appendFormValue(indexedKey(key, i), elem, pairs)
			} else {
				appendFormValue(key, elem, pairs)
			}
		}
		return
	}
	*pairs = append(*pairs, formPair(key, formatValue(v)))
}

// mapEntries returns the keys of v in order and its entries by key, when v is
// a map.
func mapEntries(v any) ([]string, map[string]any, bool) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() || rv.Kind() != reflect.Map {
		return nil, nil, false
	}
	entries := make(map[string]any, rv.Len())
	for _, k := range rv.MapKeys() {
		entries[fmt.Sprint(k.Interface())] = rv.MapIndex(k).Interface()
	}
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, entries, true
}

// isNestedValue reports whether v is a map, a slice, or an array.
func isNestedValue(v any) bool {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return false
	}
	switch rv.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array:
		return true
	}
	return false
}

// formKeyEscaper restores the brackets url.QueryEscape encodes in a key: form
// parsers read them as nesting, and APIs document them unencoded.
var formKeyEscaper = strings.NewReplacer("%5B", "[", "%5D", "]")

// formPair returns key=value with both URL-encoded, the brackets of the key
// kept.
func formPair(key, value string) string {
	return formKeyEscaper.Replace(url.QueryEscape(key)) + "=" + url.QueryEscape(value)
}

// wholePlaceholderRe matches a template value that is exactly one placeholder.
var wholePlaceholderRe = regexp.MustCompile(`^\{\{\s*([\w-]+)\s*\}\}$`)

// wholePlaceholder returns the input a template value names when the value is
// exactly one placeholder, such as "{{customer}}". A form field or a header
// written that way is left out when the input has no value.
func wholePlaceholder(text string) (string, bool) {
	m := wholePlaceholderRe.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// valuePresent reports whether inputs holds a value for key: present, not nil,
// not "", and not an empty list or map.
func valuePresent(inputs map[string]any, key string) bool {
	v, ok := inputs[key]
	if !ok || v == nil {
		return false
	}
	if s, isString := v.(string); isString {
		return s != ""
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map, reflect.Slice, reflect.Array:
		return rv.Len() > 0
	}
	return true
}

// isFormContentType reports whether a Content-Type value is form-urlencoded,
// with or without parameters such as a charset.
func isFormContentType(value string) bool {
	return strings.Contains(strings.ToLower(value), "x-www-form-urlencoded")
}

// headerValue returns the value of the header named name, compared
// case-insensitively.
func headerValue(headers map[string]string, name string) (string, bool) {
	for k, v := range headers {
		if strings.EqualFold(k, name) {
			return v, true
		}
	}
	return "", false
}

// texts returns the text of every value in the fields, in template order.
func (f FormFields) texts() []string {
	var out []string
	for _, field := range f {
		switch {
		case field.Fields != nil:
			out = append(out, field.Fields.texts()...)
		case field.Items != nil:
			for _, item := range field.Items {
				if item.Fields != nil {
					out = append(out, item.Fields.texts()...)
				} else {
					out = append(out, item.Value)
				}
			}
		default:
			out = append(out, field.Value)
		}
	}
	return out
}

// alwaysSent reports whether a field sends a pair whatever the inputs: its
// text, or text inside it, is neither one placeholder nor conditional.
func (field FormField) alwaysSent() bool {
	switch {
	case field.Fields != nil:
		return slices.ContainsFunc(field.Fields, FormField.alwaysSent)
	case field.Items != nil:
		return slices.ContainsFunc(field.Items, FormField.alwaysSent)
	}
	_, whole := wholePlaceholder(field.Value)
	return !whole && !condOpenRe.MatchString(field.Value)
}

// FormInputFields maps each input that is the whole value of a request.form
// field to the top-level fields it fills: customer: "{{customerId}}" fills
// customer with customerId, and metadata: {source: "{{source}}"} fills metadata
// with source. The static OpenAPI check uses it to match such an input to the
// spec field it is sent as.
func (t *Template) FormInputFields() map[string][]string {
	inputs := make(map[string][]string)
	for _, field := range t.Request.Form {
		name := formFieldName(field.Key)
		for _, text := range (FormFields{field}).texts() {
			if input, ok := wholePlaceholder(text); ok && !slices.Contains(inputs[input], name) {
				inputs[input] = append(inputs[input], name)
			}
		}
	}
	return inputs
}

// PathTemplate returns the request path before its query, written the way an
// OpenAPI path is: a segment that is exactly one placeholder becomes {input},
// and any other segment stays as written, so /orders/{{orderId}}/items gives
// /orders/{orderId}/items. Blocks are removed first. The static OpenAPI check
// lines it up with the operation's path to match an input to the path
// parameter it fills.
func (t *Template) PathTemplate() string {
	path, _, _ := strings.Cut(withoutBlocks(t.Request.Path), "?")
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if input, ok := wholePlaceholder(segment); ok {
			segments[i] = "{" + input + "}"
		}
	}
	return strings.Join(segments, "/")
}

// QueryInputParams maps each input that is the whole value of a query
// parameter written into the path to the parameters it fills, conditional
// blocks included: {{?startingAfter}}&starting_after={{startingAfter}}{{/startingAfter}}
// fills starting_after with startingAfter. The static OpenAPI check uses it to
// match such an input to the spec parameter it is sent as.
func (t *Template) QueryInputParams() map[string][]string {
	inputs := make(map[string][]string)
	_, query, ok := strings.Cut(blockTagRe.ReplaceAllString(t.Request.Path, ""), "?")
	if !ok {
		return inputs
	}
	for _, pair := range strings.Split(query, "&") {
		key, value, _ := strings.Cut(pair, "=")
		name := pairName(key)
		if input, whole := wholePlaceholder(value); whole && name != "" && !slices.Contains(inputs[input], name) {
			inputs[input] = append(inputs[input], name)
		}
	}
	return inputs
}

// WholeValueInputs returns, sorted, the inputs that are the whole value of a
// request.form field or a header. Such a field or header is left out when its
// input has no value, so a misspelled name would send nothing, silently; the
// static template check reports a name that isn't an input of the node.
func (t *Template) WholeValueInputs() []string {
	names := make(map[string]bool)
	for _, text := range t.Request.Form.texts() {
		if name, ok := wholePlaceholder(text); ok {
			names[name] = true
		}
	}
	for _, value := range t.Request.Headers {
		if name, ok := wholePlaceholder(value); ok {
			names[name] = true
		}
	}
	return sortedKeys(names)
}

// YAML returns the fields as the request.form mapping a template writes.
func (f FormFields) YAML() string {
	var b strings.Builder
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(f.node()); err != nil {
		return ""
	}
	_ = enc.Close()
	return b.String()
}

// node returns the fields as a YAML mapping node.
func (f FormFields) node() *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode}
	for _, field := range f {
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: field.Key}, field.valueNode())
	}
	return n
}

// valueNode returns a field's value as a YAML node.
func (field FormField) valueNode() *yaml.Node {
	switch {
	case field.Fields != nil:
		return field.Fields.node()
	case field.Items != nil:
		n := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range field.Items {
			n.Content = append(n.Content, item.valueNode())
		}
		return n
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Value: field.Value}
}
