package adapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/gburgyan/aat/internal/yamlx"
	"github.com/tidwall/gjson"
	"gopkg.in/yaml.v3"
)

// Template represents a parsed YAML template that defines how a single
// adapter translates inputs into an HTTP request and extracts outputs.
type Template struct {
	Adapter  string           `yaml:"adapter"`
	Protocol string           `yaml:"protocol"`
	Request  TemplateRequest  `yaml:"request"`
	Response TemplateResponse `yaml:"response"`
}

// TemplateRequest defines the HTTP request shape within a template.
type TemplateRequest struct {
	Method  string            `yaml:"method"`
	Path    string            `yaml:"path"`
	Headers map[string]string `yaml:"headers,omitempty"`
	Body    string            `yaml:"body,omitempty"`
	// Form is a form-encoded body written as a mapping (see FormFields). It is
	// nil when the template has none; a request has a Body or a Form.
	Form FormFields `yaml:"form,omitempty"`
}

// TemplateResponse defines how outputs are extracted from the response.
type TemplateResponse struct {
	Extract   map[string]ExtractRule `yaml:"extract,omitempty"`
	Transform string                 `yaml:"transform,omitempty"`
}

// ExtractRule defines how to extract a single output from the response.
// For scalar values, only Path is set. For array values with element
// transformation, both Path and Fields are set. When Optional is true,
// a missing path does not produce an error — the output is simply omitted.
// When Default is set, a missing path, or one that holds JSON null, gives the
// output that value as written instead. A rule with Header set reads that
// response header in place of a body path.
type ExtractRule struct {
	Path     string            `yaml:"path"`
	Header   string            `yaml:"header,omitempty"`
	Fields   map[string]string `yaml:"fields,omitempty"`
	Optional bool              `yaml:"optional,omitempty"`
	Default  any               `yaml:"default,omitempty"`
}

// ruleError describes what is wrong with the rule, or returns "". A rule reads
// a body path or a response header, not both, and a header holds no elements to
// map through fields. A rule leaves a missing output out or gives it a default,
// not both, and a rule that maps elements through fields produces a list.
func (r ExtractRule) ruleError() string {
	if r.Header != "" {
		if r.Path != "" {
			return "an extract rule takes path or header, not both"
		}
		if len(r.Fields) > 0 {
			return "a header extract rule takes no fields"
		}
	}
	if r.Default == nil {
		return ""
	}
	if r.Optional {
		return "an extract rule takes optional or default, not both"
	}
	if _, isList := r.Default.([]any); len(r.Fields) > 0 && !isList {
		return "an extract rule with fields takes a list default, such as []"
	}
	return ""
}

// UnmarshalYAML handles both string and object forms of extract rules.
// A bare string "some.path" becomes ExtractRule{Path: "some.path"}.
// An object {path: "...", fields: {...}} is decoded fully. It uses the
// callback form so strict decoding reaches the object (see internal/yamlx).
func (r *ExtractRule) UnmarshalYAML(unmarshal func(any) error) error {
	n, err := yamlx.Node(unmarshal)
	if err != nil {
		return err
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return unmarshal(&r.Path)
	case yaml.MappingNode:
		type rawExtractRule ExtractRule
		var raw rawExtractRule
		if err := unmarshal(&raw); err != nil {
			return err
		}
		if msg := ExtractRule(raw).ruleError(); msg != "" {
			return &yaml.TypeError{Errors: []string{fmt.Sprintf("line %d: %s", n.Line, msg)}}
		}
		*r = ExtractRule(raw)
		return nil
	default:
		return yamlx.KindError(n, "an extract rule", "a path or a mapping")
	}
}

// GJSONPath returns the rule's path in GJSON syntax, the form responses are
// queried with ("$.items[0].id" becomes "items.0.id").
func (r ExtractRule) GJSONPath() string {
	return normalizeJSONPath(r.Path)
}

// Source names where the rule reads its output: its body path, or "header"
// and the header's name.
func (r ExtractRule) Source() string {
	if r.Header != "" {
		return "header " + r.Header
	}
	return r.Path
}

// HasElementFields reports whether the named output has template-side
// field mappings that transform array elements into flat maps.
func (t *Template) HasElementFields(outputName string) bool {
	rule, ok := t.Extract()[outputName]
	if !ok {
		return false
	}
	return len(rule.Fields) > 0
}

// Extract returns the response extract rules. This is a convenience
// accessor for the Response.Extract map.
func (t *Template) Extract() map[string]ExtractRule {
	return t.Response.Extract
}

// HasTransform reports whether the template has a Lua transform script.
func (t *Template) HasTransform() bool {
	return t.Response.Transform != ""
}

// TemplateAdapter implements the Adapter interface using a parsed Template.
type TemplateAdapter struct {
	tmpl Template
}

// placeholderRe matches {{key}} with optional internal whitespace.
var placeholderRe = regexp.MustCompile(`\{\{\s*([^}]+?)\s*\}\}`)

// iterOpenRe matches {{#key}} iteration block opening tags. Keys may contain
// hyphens, as input names taken from HTTP header parameters do (X-Request-Id).
var iterOpenRe = regexp.MustCompile(`\{\{#([\w-]+)\}\}`)

// condOpenRe matches {{?key}} and {{?key1|key2}} conditional block opening tags.
// Keys may contain hyphens.
var condOpenRe = regexp.MustCompile(`\{\{\?([\w|-]+)\}\}`)

// blockTagRe matches a tag that opens or closes a block: {{?key}}, {{#key}}, or
// {{/key}}. The key of a compound conditional, such as a|b, is one name.
var blockTagRe = regexp.MustCompile(`\{\{([?#/])([\w|-]+)\}\}`)

// findBlockClose returns the index in s of the {{/key}} tag that closes a block
// for key, where s starts just after the block's opening tag, or -1 when the
// block isn't closed. A conditional and an iteration for the same key both close
// with {{/key}}, so a block of either kind opened inside it for that key takes
// the next closing tag first: {{?ids}}[{{#ids}}…{{/ids}}]{{/ids}} closes the
// conditional at its second tag.
func findBlockClose(s, key string) int {
	depth := 0
	for _, m := range blockTagRe.FindAllStringSubmatchIndex(s, -1) {
		if s[m[4]:m[5]] != key {
			continue
		}
		if s[m[2]] != '/' {
			depth++
			continue
		}
		if depth == 0 {
			return m[0]
		}
		depth--
	}
	return -1
}

// ParseTemplate parses YAML bytes into a Template and validates required
// fields. Keys that no template field accepts are errors.
func ParseTemplate(data []byte) (*Template, error) {
	var t Template
	if err := yamlx.Decode(data, &t); err != nil {
		return nil, err
	}

	if t.Adapter == "" {
		return nil, fmt.Errorf("template missing required field: adapter")
	}
	if t.Request.Method == "" {
		return nil, fmt.Errorf("template missing required field: request.method")
	}
	if t.Request.Path == "" {
		return nil, fmt.Errorf("template missing required field: request.path")
	}

	if t.Protocol == "" {
		t.Protocol = "http"
	}
	if t.Protocol != "http" {
		return nil, fmt.Errorf("unsupported protocol %q (only \"http\" is supported)", t.Protocol)
	}

	if t.Request.Form != nil {
		if t.Request.Body != "" {
			return nil, fmt.Errorf("template has both request.body and request.form; a request sends one of them")
		}
		if contentType, ok := headerValue(t.Request.Headers, "Content-Type"); ok && !isFormContentType(contentType) {
			return nil, fmt.Errorf("request.form is sent as %s, but the template's Content-Type header is %q", FormContentType, contentType)
		}
	}

	return &t, nil
}

// ParseTemplateFile reads a file and parses it as a template. Errors name the
// file.
func ParseTemplateFile(path string) (*Template, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading template file: %w", err)
	}
	t, err := ParseTemplate(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return t, nil
}

// NewTemplateAdapter wraps a parsed Template into an Adapter implementation.
func NewTemplateAdapter(tmpl Template) *TemplateAdapter {
	return &TemplateAdapter{tmpl: tmpl}
}

// BuildRequest constructs an HTTP Request by substituting placeholders in the
// template's path, headers, and body with values from inputs. Each value is
// escaped for where it lands (see renderContext); config supplies the headers
// every request starts with.
func (a *TemplateAdapter) BuildRequest(inputs map[string]any, config *EnvironmentConfig) (*Request, error) {
	path, err := substitutePlaceholders(a.tmpl.Request.Path, inputs, renderPath)
	if err != nil {
		return nil, fmt.Errorf("path substitution: %w", err)
	}

	// Config headers first, then template headers, then the protected headers
	// (credential, override, overlay), which a template cannot replace. Names
	// compare case-insensitively. A request.form sets its Content-Type as a
	// template header does.
	merged := make(map[string]string)
	if config != nil {
		for k, v := range config.Headers {
			setHeader(merged, k, v)
		}
	}
	if a.tmpl.Request.Form != nil {
		setHeader(merged, "Content-Type", FormContentType)
	}
	for k, tmplVal := range a.tmpl.Request.Headers {
		// A header whose whole value is one placeholder is not sent when that
		// input has no value, as a form field is left out.
		if name, whole := wholePlaceholder(tmplVal); whole && !valuePresent(inputs, name) {
			continue
		}
		resolved, err := substitutePlaceholders(tmplVal, inputs, renderRaw)
		if err != nil {
			return nil, fmt.Errorf("header %q substitution: %w", k, err)
		}
		// A header whose value is conditional ({{?x}}{{x}}{{/x}}) and resolves
		// to nothing is not sent, rather than sent empty.
		if resolved == "" && strings.Contains(tmplVal, "{{?") {
			continue
		}
		setHeader(merged, k, resolved)
	}
	if config != nil {
		for k, v := range config.Protected {
			setHeader(merged, k, v)
		}
	}

	var body []byte
	switch {
	case a.tmpl.Request.Form != nil:
		if contentType, _ := headerValue(merged, "Content-Type"); !isFormContentType(contentType) {
			return nil, fmt.Errorf("request.form sends %s, but a credential or overlay header sets Content-Type %q", FormContentType, contentType)
		}
		bodyStr, err := a.tmpl.Request.Form.render(inputs)
		if err != nil {
			return nil, fmt.Errorf("form body: %w", err)
		}
		body = []byte(bodyStr)
	case a.tmpl.Request.Body != "":
		ctx := bodyContext(merged, a.tmpl.Request.Body)
		bodyStr, err := substitutePlaceholders(a.tmpl.Request.Body, inputs, ctx)
		if err != nil {
			return nil, fmt.Errorf("body substitution: %w", err)
		}
		if ctx == renderForm {
			// Every form value is URL-encoded, so whitespace around the body can
			// only come from the template, such as the final newline a YAML block
			// scalar (body: |) keeps. Sent, it would end the last value.
			bodyStr = strings.TrimSpace(bodyStr)
		}
		body = []byte(bodyStr)
	}

	return &Request{
		Method:  a.tmpl.Request.Method,
		Path:    path,
		Headers: merged,
		Body:    body,
	}, nil
}

// ExtractOutputs parses the response body as JSON and extracts values using
// the template's extract rules (GJSON paths), and reads the response headers
// its header rules name. When an extract rule has Fields and the extracted
// value is an array, each element is transformed into a flat map using the
// field mappings (logical name → gjson path within the element).
func (a *TemplateAdapter) ExtractOutputs(resp *Response) (map[string]any, error) {
	if len(a.tmpl.Response.Extract) == 0 && a.tmpl.Response.Transform == "" {
		return map[string]any{}, nil
	}

	// Rules that read the body need a JSON body. Header rules don't, and a
	// transform-only template runs anyway; its json_path() calls simply find
	// nothing in a non-JSON body.
	if a.readsBody() && !json.Valid(resp.Body) {
		return nil, fmt.Errorf("response body is not valid JSON")
	}

	bodyStr := string(resp.Body)
	outputs := make(map[string]any, len(a.tmpl.Response.Extract))

	for name, rule := range a.tmpl.Response.Extract {
		if rule.Header != "" {
			if values := headerValues(resp.Headers, rule.Header); len(values) > 0 {
				outputs[name] = strings.Join(values, ", ")
				continue
			}
			switch {
			case rule.Default != nil:
				outputs[name] = rule.Default
			case !rule.Optional:
				return nil, fmt.Errorf("extract header %q (%s) not found in response; mark the rule optional: true or give it a default", name, rule.Header)
			}
			continue
		}

		gpath := normalizeJSONPath(rule.Path)
		result := gjson.Get(bodyStr, gpath)
		if rule.Default != nil && (!result.Exists() || result.Type == gjson.Null) {
			outputs[name] = rule.Default
			continue
		}
		if !result.Exists() {
			if rule.Optional {
				continue
			}
			return nil, fmt.Errorf("extract path %q (%s) not found in response; mark the rule optional: true or give it a default", name, rule.Path)
		}

		val := gjsonValue(result)

		// Transform array elements when Fields is set
		if len(rule.Fields) > 0 {
			arr, ok := val.([]any)
			if !ok {
				return nil, fmt.Errorf("extract rule %q has fields but extracted value is not an array (got %T)", name, val)
			}
			val = transformElements(arr, rule.Fields)
		}

		outputs[name] = val
	}

	if a.tmpl.Response.Transform != "" {
		transformed, err := runTransformWithLog(a.tmpl.Response.Transform, outputs, bodyStr, resp.Headers, os.Stderr)
		if err != nil {
			return nil, fmt.Errorf("transform: %w", err)
		}
		outputs = transformed
	}

	return outputs, nil
}

// readsBody reports whether any extract rule reads the response body.
func (a *TemplateAdapter) readsBody() bool {
	for _, rule := range a.tmpl.Response.Extract {
		if rule.Header == "" {
			return true
		}
	}
	return false
}

// transformElements applies field mappings to each array element, producing
// flat maps keyed by logical field name. Each element is marshaled to JSON
// and then fields are extracted via gjson.
func transformElements(arr []any, fields map[string]string) []any {
	result := make([]any, len(arr))
	for i, elem := range arr {
		data, err := json.Marshal(elem)
		if err != nil {
			// Keep original element if marshal fails
			result[i] = elem
			continue
		}

		flat := make(map[string]any, len(fields))
		for fieldName, fieldPath := range fields {
			gpath := normalizeJSONPath(fieldPath)
			r := gjson.GetBytes(data, gpath)
			if r.Exists() {
				flat[fieldName] = gjsonValue(r)
			}
			// Missing fields are skipped (not an error)
		}
		result[i] = flat
	}
	return result
}

// ValidateInputs returns nil — template adapters defer to graph-level type checking.
func (a *TemplateAdapter) ValidateInputs(inputs map[string]any) *ValidationResult {
	return nil
}

// ValidateResponse returns nil — response validation is deferred to Task 9.
func (a *TemplateAdapter) ValidateResponse(resp *Response) *ValidationResult {
	return nil
}

// substitutePlaceholders replaces {{key}} tokens in tmpl with values from
// inputs, each escaped for the position it fills in ctx (see renderContext).
// Conditional blocks are expanded first, then iteration blocks, then every
// placeholder in one pass. Returns an error listing all unresolved placeholders.
func substitutePlaceholders(tmpl string, inputs map[string]any, ctx renderContext) (string, error) {
	// Phase 1: expand conditional blocks (must run before iteration/placeholders)
	condExpanded, err := expandConditionalBlocks(tmpl, inputs)
	if err != nil {
		return "", err
	}

	// Phase 2: expand iteration blocks. Their element values become
	// placeholders, so phase 3 escapes them like the rest.
	var elements []any
	expanded, err := expandIterationBlocks(condExpanded, inputs, &elements, ctx)
	if err != nil {
		return "", err
	}

	// Phase 3: substitute placeholders, following the literal text between
	// them to know where each value lands.
	var b strings.Builder
	var missing []string
	scanner := &contextScanner{ctx: ctx}
	last := 0
	for _, m := range placeholderRe.FindAllStringSubmatchIndex(expanded, -1) {
		literal := expanded[last:m[0]]
		b.WriteString(literal)
		scanner.feed(literal)
		last = m[1]

		key := expanded[m[2]:m[3]]
		if v, ok := placeholderValue(key, inputs, elements); ok {
			b.WriteString(scanner.escape(v))
			continue
		}
		missing = append(missing, key)
		b.WriteString(expanded[m[0]:m[1]])
	}
	b.WriteString(expanded[last:])

	if len(missing) > 0 {
		return "", fmt.Errorf("unresolved placeholders: %s", strings.Join(missing, ", "))
	}
	return b.String(), nil
}

// expandConditionalBlocks finds and expands {{?key}}...{{/key}} blocks in the
// template. If the key exists in inputs and has a non-empty value, the block
// content is included; otherwise the entire block (including tags) is removed.
func expandConditionalBlocks(tmpl string, inputs map[string]any) (string, error) {
	result := tmpl
	for {
		loc := condOpenRe.FindStringIndex(result)
		if loc == nil {
			break
		}

		match := condOpenRe.FindStringSubmatch(result[loc[0]:loc[1]])
		key := match[1]

		closeTag := "{{/" + key + "}}"
		closeIdx := findBlockClose(result[loc[1]:], key)
		if closeIdx < 0 {
			return "", fmt.Errorf("unclosed conditional block: {{?%s}}", key)
		}

		body := result[loc[1] : loc[1]+closeIdx]
		blockEnd := loc[1] + closeIdx + len(closeTag)

		if anyPresent(inputs, key) {
			result = result[:loc[0]] + body + result[blockEnd:]
		} else {
			result = result[:loc[0]] + result[blockEnd:]
		}
	}
	return result, nil
}

// anyPresent returns true if any of the pipe-separated keys are present in
// inputs with a non-empty value. For a single key (no pipes), it behaves
// identically to condPresent.
func anyPresent(inputs map[string]any, keys string) bool {
	for _, k := range strings.Split(keys, "|") {
		if condPresent(inputs, k) {
			return true
		}
	}
	return false
}

// condPresent returns true if the key exists in inputs and has a non-empty value.
func condPresent(inputs map[string]any, key string) bool {
	v, ok := inputs[key]
	if !ok {
		return false
	}
	if s, isStr := v.(string); isStr {
		return s != ""
	}
	return v != nil
}

// expandIterationBlocks finds and expands {{#key}}...{{/key}} blocks in the
// template. Each block is repeated for every element in the named array, with
// the copies joined as iterationSeparator decides for ctx. {{@index}} becomes
// the element's index, and the values {{.}} and {{.field}} stand for are
// appended to elements and left as placeholders for substitutePlaceholders to
// escape and fill.
func expandIterationBlocks(tmpl string, inputs map[string]any, elements *[]any, ctx renderContext) (string, error) {
	result := tmpl
	for {
		loc := iterOpenRe.FindStringIndex(result)
		if loc == nil {
			break
		}

		match := iterOpenRe.FindStringSubmatch(result[loc[0]:loc[1]])
		key := match[1]

		closeTag := "{{/" + key + "}}"
		closeIdx := findBlockClose(result[loc[1]:], key)
		if closeIdx < 0 {
			return "", fmt.Errorf("unclosed iteration block: {{#%s}}", key)
		}

		body := result[loc[1] : loc[1]+closeIdx]
		blockEnd := loc[1] + closeIdx + len(closeTag)

		val, ok := inputs[key]
		if !ok {
			return "", fmt.Errorf("iteration variable %q not found in inputs", key)
		}

		arr, ok := val.([]any)
		if !ok {
			return "", fmt.Errorf("iteration variable %q is not an array (got %T)", key, val)
		}

		expanded := expandArray(body, arr, elements, iterationSeparator(ctx, result[:loc[0]], body))
		result = result[:loc[0]] + expanded + result[blockEnd:]
	}
	return result, nil
}

// expandArray repeats body for each element in arr, joining the copies with sep.
func expandArray(body string, arr []any, elements *[]any, sep string) string {
	parts := make([]string, len(arr))
	for i, elem := range arr {
		parts[i] = expandElement(indexRe.ReplaceAllLiteralString(body, fmt.Sprint(i)), elem, elements)
	}
	return strings.Join(parts, sep)
}

// iterationSeparator returns what joins the copies of an iteration block with
// body, which follows the template text before and is rendered in ctx. In a
// form body, or in a path after its first "?", the copies are key=value pairs of
// their own: a body that starts or ends with "&" brings its separator, so the
// copies are concatenated, and a body that starts a pair is joined with "&".
// Anywhere else, including a block in the middle of a pair, the copies are
// joined with commas.
func iterationSeparator(ctx renderContext, before, body string) string {
	var pair string
	switch ctx {
	case renderForm:
		pair = before
	case renderPath:
		_, query, inQuery := strings.Cut(before, "?")
		if !inQuery {
			return ","
		}
		pair = query
	default:
		return ","
	}
	pair = pair[strings.LastIndexByte(pair, '&')+1:]

	trimmed := strings.TrimSpace(body)
	switch {
	case strings.HasPrefix(trimmed, "&") || strings.HasSuffix(trimmed, "&"):
		return ""
	case strings.TrimSpace(pair) == "" && strings.Contains(body, "="):
		return "&"
	default:
		return ","
	}
}

// dotFieldRe matches {{.field}}, dotRe matches {{.}}, and indexRe matches
// {{@index}} inside an iteration block.
var (
	dotFieldRe = regexp.MustCompile(`\{\{\s*\.(\w+)\s*\}\}`)
	dotRe      = regexp.MustCompile(`\{\{\s*\.\s*\}\}`)
	indexRe    = regexp.MustCompile(`\{\{\s*@index\s*\}\}`)
)

// expandElement replaces {{.}} with a placeholder for the element itself (for
// scalars) and {{.field}} with a placeholder for the named field of a map
// element. A field the element lacks is left as it is.
func expandElement(body string, elem any, elements *[]any) string {
	// Replace {{.fieldName}} first (more specific), then {{.}}
	result := dotFieldRe.ReplaceAllStringFunc(body, func(match string) string {
		fieldName := dotFieldRe.FindStringSubmatch(match)[1]
		if m, ok := elem.(map[string]any); ok {
			if v, exists := m[fieldName]; exists {
				return elementPlaceholder(elements, v)
			}
		}
		return match
	})

	return dotRe.ReplaceAllStringFunc(result, func(string) string {
		return elementPlaceholder(elements, elem)
	})
}

// gjsonValue extracts a Go value from a gjson.Result, using json.Number for
// numeric values to preserve the original text representation. This avoids
// float64 precision loss for large integers (e.g. 9223372036854775807).
func gjsonValue(r gjson.Result) any {
	if r.Type == gjson.Number {
		return json.Number(r.Raw)
	}
	return r.Value()
}

// normalizeJSONPath converts a JSONPath expression to GJSON syntax.
// Strips leading "$." and converts bracket notation [N] to dot notation .N.
func normalizeJSONPath(path string) string {
	// Strip leading "$." or lone "$"
	if path == "$" {
		return "@this"
	}
	path = strings.TrimPrefix(path, "$.")

	// Convert [N] bracket notation to .N dot notation
	bracketRe := regexp.MustCompile(`\[(\d+)\]`)
	path = bracketRe.ReplaceAllString(path, ".$1")

	return path
}

// SuppliedFields returns the request fields a template always sends: query
// parameters written into the path, header names, the top-level keys of a JSON
// body, and the keys of a form-encoded body. A bracketed key counts as the name
// before its first bracket, so metadata[source]=web supplies metadata. Fields
// inside {{?key}} or {{#key}} blocks are left out, since they are sent only
// sometimes, and so is a request.form field whose whole value is one
// placeholder (see FormInputFields). The static OpenAPI check uses this to accept a
// required parameter or body property that the template supplies itself, for
// example a literal "photoUrls": [] with no graph input behind it.
func (t *Template) SuppliedFields() map[string]bool {
	fields := make(map[string]bool)

	path := withoutBlocks(t.Request.Path)
	if _, query, ok := strings.Cut(path, "?"); ok {
		for _, name := range pairNames(query) {
			fields[name] = true
		}
	}

	for name, value := range t.Request.Headers {
		if withoutBlocks(value) != "" {
			fields[name] = true
		}
	}

	if t.Request.Form != nil {
		for _, field := range t.Request.Form {
			if field.alwaysSent() {
				fields[formFieldName(field.Key)] = true
			}
		}
		return fields
	}

	body := withoutBlocks(t.Request.Body)
	if bodyContext(t.Request.Headers, t.Request.Body) == renderForm {
		for _, name := range pairNames(strings.TrimSpace(body)) {
			fields[name] = true
		}
		return fields
	}
	for _, key := range topLevelJSONKeys(body) {
		fields[key] = true
	}
	return fields
}

// HeaderOnlyInputs returns the inputs the template sends only in request
// headers: placeholders and block keys that appear in a header value but not in
// the path, body, or form. The static OpenAPI check uses this to accept an input
// such as an idempotency key, whose header the template names and specs often
// leave undeclared.
func (t *Template) HeaderOnlyInputs() map[string]bool {
	elsewhere := placeholderKeys(append([]string{t.Request.Path, t.Request.Body}, t.Request.Form.texts()...)...)
	inputs := make(map[string]bool)
	for _, value := range t.Request.Headers {
		for key := range placeholderKeys(value) {
			if !elsewhere[key] {
				inputs[key] = true
			}
		}
	}
	return inputs
}

// placeholderKeys returns every input key the sources reference: placeholders
// and the keys of {{?key}} and {{#key}} blocks.
func placeholderKeys(sources ...string) map[string]bool {
	keys := make(map[string]bool)
	for _, src := range sources {
		classifySource(src, map[string]bool{}, map[string]bool{}, map[string]bool{}, keys)
	}
	return keys
}

// pairNames returns the field names of the key=value pairs in a query string or
// form body. A bracketed key gives the name before its first bracket, and a key
// written as a placeholder gives none.
func pairNames(pairs string) []string {
	var names []string
	for _, pair := range strings.Split(pairs, "&") {
		key, _, _ := strings.Cut(pair, "=")
		if name := pairName(key); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// pairName returns the field name of a query or form key: the name before its
// first bracket, or "" for a key written as a placeholder.
func pairName(key string) string {
	name, _, _ := strings.Cut(key, "[")
	if strings.Contains(name, "{{") {
		return ""
	}
	return name
}

// withoutBlocks removes every {{?key}}...{{/key}} and {{#key}}...{{/key}} block,
// tags and content, leaving the text a template renders unconditionally.
func withoutBlocks(s string) string {
	for _, open := range []*regexp.Regexp{condOpenRe, iterOpenRe} {
		for {
			loc := open.FindStringSubmatchIndex(s)
			if loc == nil {
				break
			}
			key := s[loc[2]:loc[3]]
			closeTag := "{{/" + key + "}}"
			closeIdx := findBlockClose(s[loc[1]:], key)
			if closeIdx < 0 {
				break
			}
			s = s[:loc[0]] + s[loc[1]+closeIdx+len(closeTag):]
		}
	}
	return s
}

// topLevelJSONKeys scans text shaped like a JSON object and returns the keys at
// its top level. It tolerates placeholders in value positions and does not
// require the text to be valid JSON.
func topLevelJSONKeys(body string) []string {
	body = placeholderRe.ReplaceAllString(body, "0")
	var keys []string
	depth := 0
	inString, escaped, afterString := false, false, false
	var current, last strings.Builder
	for _, r := range body {
		if inString {
			switch {
			case escaped:
				escaped = false
				current.WriteRune(r)
			case r == '\\':
				escaped = true
			case r == '"':
				inString = false
				afterString = true
				last.Reset()
				last.WriteString(current.String())
			default:
				current.WriteRune(r)
			}
			continue
		}
		switch r {
		case '"':
			inString = true
			current.Reset()
		case '{', '[':
			depth++
			afterString = false
		case '}', ']':
			depth--
			afterString = false
		case ':':
			if depth == 1 && afterString {
				keys = append(keys, last.String())
			}
			afterString = false
		case ' ', '\t', '\n', '\r':
		default:
			afterString = false
		}
	}
	return keys
}

// ClassifyInputs scans a template's path, headers, and body to classify each
// placeholder into one of three categories:
//   - required: placeholders that appear in unconditional context
//   - conditional: placeholders that appear only inside {{?key}}...{{/key}} blocks
//     (both the gate key and any {{innerKey}} only referenced inside), or as
//     the whole value of a header or form field, which is left out without one
//   - iterable: placeholders that appear inside {{#key}}...{{/key}} blocks
//
// Returns sorted, deduplicated slices.
func ClassifyInputs(tmpl *Template) (required, conditional, iterable []string) {
	iterKeys := make(map[string]bool)
	condKeys := make(map[string]bool)
	condInnerKeys := make(map[string]bool)
	allKeys := make(map[string]bool)

	// Analyze all text sources: path, header values, body, and form values. A
	// header or form value that is one placeholder is left out when its input
	// has no value, so that input is conditional unless another part of the
	// template needs it.
	sources := []string{tmpl.Request.Path}
	var whole []string
	addSource := func(src string) {
		if name, ok := wholePlaceholder(src); ok {
			whole = append(whole, name)
			return
		}
		sources = append(sources, src)
	}
	for _, v := range tmpl.Request.Headers {
		addSource(v)
	}
	if tmpl.Request.Body != "" {
		sources = append(sources, tmpl.Request.Body)
	}
	for _, text := range tmpl.Request.Form.texts() {
		addSource(text)
	}

	for _, src := range sources {
		classifySource(src, iterKeys, condKeys, condInnerKeys, allKeys)
	}

	reqSet := make(map[string]bool)
	for k := range allKeys {
		if !iterKeys[k] && !condKeys[k] && !condInnerKeys[k] {
			reqSet[k] = true
		}
	}
	for _, name := range whole {
		if !reqSet[name] && !iterKeys[name] {
			condKeys[name] = true
		}
	}

	required = sortedKeys(reqSet)
	conditional = sortedKeys(condKeys)
	// Also add inner-only keys to conditional (they are only inside cond blocks).
	for k := range condInnerKeys {
		if !condKeys[k] {
			conditional = append(conditional, k)
		}
	}
	sort.Strings(conditional)
	iterable = sortedKeys(iterKeys)
	return
}

// classifySource analyzes a single template string for placeholder classification.
func classifySource(src string, iterKeys, condKeys, condInnerKeys, allKeys map[string]bool) {
	// First, find all iteration blocks and mark their keys.
	remaining := src
	for {
		loc := iterOpenRe.FindStringIndex(remaining)
		if loc == nil {
			break
		}
		match := iterOpenRe.FindStringSubmatch(remaining[loc[0]:loc[1]])
		key := match[1]
		closeTag := "{{/" + key + "}}"
		closeIdx := findBlockClose(remaining[loc[1]:], key)
		if closeIdx < 0 {
			break
		}
		iterKeys[key] = true
		allKeys[key] = true
		remaining = remaining[:loc[0]] + remaining[loc[1]+closeIdx+len(closeTag):]
	}

	// Find all conditional blocks and mark their gate keys and inner-only keys.
	remaining = src
	for {
		loc := condOpenRe.FindStringIndex(remaining)
		if loc == nil {
			break
		}
		match := condOpenRe.FindStringSubmatch(remaining[loc[0]:loc[1]])
		key := match[1]
		closeTag := "{{/" + key + "}}"
		closeIdx := findBlockClose(remaining[loc[1]:], key)
		if closeIdx < 0 {
			break
		}
		body := remaining[loc[1] : loc[1]+closeIdx]
		// Compound keys like "a|b" — register each individual key as conditional.
		for _, k := range strings.Split(key, "|") {
			condKeys[k] = true
			allKeys[k] = true
		}

		// Find inner placeholders that only appear in this conditional block.
		// First, find iteration blocks inside the conditional to mark their keys.
		innerBody := body
		for {
			iloc := iterOpenRe.FindStringIndex(innerBody)
			if iloc == nil {
				break
			}
			imatch := iterOpenRe.FindStringSubmatch(innerBody[iloc[0]:iloc[1]])
			ikey := imatch[1]
			icloseTag := "{{/" + ikey + "}}"
			icloseIdx := findBlockClose(innerBody[iloc[1]:], ikey)
			if icloseIdx < 0 {
				break
			}
			iterKeys[ikey] = true
			allKeys[ikey] = true
			condInnerKeys[ikey] = true
			innerBody = innerBody[:iloc[0]] + innerBody[iloc[1]+icloseIdx+len(icloseTag):]
		}

		// Now find regular placeholders in the remaining body text.
		innerMatches := placeholderRe.FindAllStringSubmatch(innerBody, -1)
		for _, m := range innerMatches {
			innerKey := strings.TrimSpace(m[1])
			// Skip dot-access, {{@index}}, block open/close, and iteration tags
			if strings.HasPrefix(innerKey, ".") || strings.HasPrefix(innerKey, "@") || strings.HasPrefix(innerKey, "#") ||
				strings.HasPrefix(innerKey, "?") || strings.HasPrefix(innerKey, "/") {
				continue
			}
			allKeys[innerKey] = true
			condInnerKeys[innerKey] = true
		}

		remaining = remaining[:loc[0]] + remaining[loc[1]+closeIdx+len(closeTag):]
	}

	// Find all remaining regular placeholders (outside conditional/iteration blocks).
	matches := placeholderRe.FindAllStringSubmatch(remaining, -1)
	for _, m := range matches {
		key := strings.TrimSpace(m[1])
		if strings.HasPrefix(key, ".") || strings.HasPrefix(key, "@") || strings.HasPrefix(key, "#") ||
			strings.HasPrefix(key, "?") || strings.HasPrefix(key, "/") {
			continue
		}
		allKeys[key] = true
	}
}

// sortedKeys returns the keys of a map as a sorted slice.
func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// LoadTemplates reads all .yaml/.yml files from dir, parses each as a template,
// and registers the resulting adapter in the registry. Returns the count of
// loaded templates. Fails fast on the first error.
func LoadTemplates(dir string, registry *Registry) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("reading template directory: %w", err)
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		tmpl, err := ParseTemplateFile(path)
		if err != nil {
			return count, err
		}

		if err := registry.Register(tmpl.Adapter, NewTemplateAdapter(*tmpl)); err != nil {
			return count, fmt.Errorf("%s: %w", path, err)
		}
		count++
	}

	return count, nil
}
