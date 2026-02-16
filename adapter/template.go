package adapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
}

// TemplateResponse defines output extraction and validation rules.
type TemplateResponse struct {
	Extract   map[string]ExtractRule `yaml:"extract,omitempty"`
	Validate  *TemplateValidate      `yaml:"validate,omitempty"`
	Transform string                 `yaml:"transform,omitempty"`
}

// ExtractRule defines how to extract a single output from the response.
// For scalar values, only Path is set. For array values with element
// transformation, both Path and Fields are set.
type ExtractRule struct {
	Path   string            `yaml:"path"`
	Fields map[string]string `yaml:"fields,omitempty"`
}

// UnmarshalYAML handles both string and object forms of extract rules.
// A bare string "some.path" becomes ExtractRule{Path: "some.path"}.
// An object {path: "...", fields: {...}} is decoded fully.
func (r *ExtractRule) UnmarshalYAML(value *yaml.Node) error {
	// Try string first
	var s string
	if err := value.Decode(&s); err == nil {
		r.Path = s
		return nil
	}
	// Try object
	type rawRule ExtractRule
	var raw rawRule
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*r = ExtractRule(raw)
	return nil
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

// TemplateValidate holds optional validation configuration for responses.
type TemplateValidate struct {
	Schema string `yaml:"schema,omitempty"`
}

// TemplateAdapter implements the Adapter interface using a parsed Template.
type TemplateAdapter struct {
	tmpl Template
}

// placeholderRe matches {{key}} with optional internal whitespace.
var placeholderRe = regexp.MustCompile(`\{\{\s*([^}]+?)\s*\}\}`)

// iterOpenRe matches {{#key}} iteration block opening tags.
var iterOpenRe = regexp.MustCompile(`\{\{#(\w+)\}\}`)

// condOpenRe matches {{?key}} conditional block opening tags.
var condOpenRe = regexp.MustCompile(`\{\{\?(\w+)\}\}`)

// ParseTemplate parses YAML bytes into a Template and validates required fields.
func ParseTemplate(data []byte) (*Template, error) {
	var t Template
	if err := yaml.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
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

	return &t, nil
}

// ParseTemplateFile reads a file and parses it as a template.
func ParseTemplateFile(path string) (*Template, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading template file: %w", err)
	}
	return ParseTemplate(data)
}

// NewTemplateAdapter wraps a parsed Template into an Adapter implementation.
func NewTemplateAdapter(tmpl Template) *TemplateAdapter {
	return &TemplateAdapter{tmpl: tmpl}
}

// BuildRequest constructs an HTTP Request by substituting placeholders in the
// template's path, headers, and body with values from inputs and config.
func (a *TemplateAdapter) BuildRequest(inputs map[string]any, config *EnvironmentConfig) (*Request, error) {
	path, err := substitutePlaceholders(a.tmpl.Request.Path, inputs, config)
	if err != nil {
		return nil, fmt.Errorf("path substitution: %w", err)
	}

	// Start with config headers, then overlay template headers.
	merged := make(map[string]string)
	if config != nil {
		for k, v := range config.Headers {
			merged[k] = v
		}
	}
	for k, tmplVal := range a.tmpl.Request.Headers {
		resolved, err := substitutePlaceholders(tmplVal, inputs, config)
		if err != nil {
			return nil, fmt.Errorf("header %q substitution: %w", k, err)
		}
		merged[k] = resolved
	}

	var body []byte
	if a.tmpl.Request.Body != "" {
		bodyStr, err := substitutePlaceholders(a.tmpl.Request.Body, inputs, config)
		if err != nil {
			return nil, fmt.Errorf("body substitution: %w", err)
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
// the template's extract rules (GJSON paths). When an extract rule has Fields
// and the extracted value is an array, each element is transformed into a flat
// map using the field mappings (logical name → gjson path within the element).
func (a *TemplateAdapter) ExtractOutputs(resp *Response) (map[string]any, error) {
	if len(a.tmpl.Response.Extract) == 0 {
		return map[string]any{}, nil
	}

	if !json.Valid(resp.Body) {
		return nil, fmt.Errorf("response body is not valid JSON")
	}

	bodyStr := string(resp.Body)
	outputs := make(map[string]any, len(a.tmpl.Response.Extract))

	for name, rule := range a.tmpl.Response.Extract {
		gpath := normalizeJSONPath(rule.Path)
		result := gjson.Get(bodyStr, gpath)
		if !result.Exists() {
			return nil, fmt.Errorf("extract path %q (%s) not found in response", name, rule.Path)
		}

		val := result.Value()

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
		transformed, err := runTransform(a.tmpl.Response.Transform, outputs, bodyStr)
		if err != nil {
			return nil, fmt.Errorf("transform: %w", err)
		}
		outputs = transformed
	}

	return outputs, nil
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
				flat[fieldName] = r.Value()
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
// inputs (checked first) then config.Values. Iteration blocks {{#key}}...{{/key}}
// are expanded first, then regular placeholders are substituted. Returns an
// error listing all unresolved placeholders.
func substitutePlaceholders(tmpl string, inputs map[string]any, config *EnvironmentConfig) (string, error) {
	// Phase 1: expand conditional blocks (must run before iteration/placeholders)
	condExpanded, err := expandConditionalBlocks(tmpl, inputs)
	if err != nil {
		return "", err
	}

	// Phase 2: expand iteration blocks
	expanded, err := expandIterationBlocks(condExpanded, inputs)
	if err != nil {
		return "", err
	}

	// Phase 3: substitute regular placeholders
	var missing []string

	result := placeholderRe.ReplaceAllStringFunc(expanded, func(match string) string {
		sub := placeholderRe.FindStringSubmatch(match)
		key := sub[1]

		if v, ok := inputs[key]; ok {
			return formatValue(v)
		}
		if config != nil {
			if v, ok := config.GetValue(key); ok {
				return v
			}
		}
		missing = append(missing, key)
		return match
	})

	if len(missing) > 0 {
		return "", fmt.Errorf("unresolved placeholders: %s", strings.Join(missing, ", "))
	}
	return result, nil
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
		closeIdx := strings.Index(result[loc[1]:], closeTag)
		if closeIdx < 0 {
			return "", fmt.Errorf("unclosed conditional block: {{?%s}}", key)
		}

		body := result[loc[1] : loc[1]+closeIdx]
		blockEnd := loc[1] + closeIdx + len(closeTag)

		if condPresent(inputs, key) {
			result = result[:loc[0]] + body + result[blockEnd:]
		} else {
			result = result[:loc[0]] + result[blockEnd:]
		}
	}
	return result, nil
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
// template. Each block is repeated for every element in the named array,
// with elements comma-separated in the output.
func expandIterationBlocks(tmpl string, inputs map[string]any) (string, error) {
	result := tmpl
	for {
		loc := iterOpenRe.FindStringIndex(result)
		if loc == nil {
			break
		}

		match := iterOpenRe.FindStringSubmatch(result[loc[0]:loc[1]])
		key := match[1]

		// Build closing tag pattern for this specific key
		closeTag := "{{/" + key + "}}"
		closeIdx := strings.Index(result[loc[1]:], closeTag)
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

		expanded := expandArray(body, arr)
		result = result[:loc[0]] + expanded + result[blockEnd:]
	}
	return result, nil
}

// expandArray repeats body for each element in arr, joining results with commas.
func expandArray(body string, arr []any) string {
	parts := make([]string, len(arr))
	for i, elem := range arr {
		parts[i] = expandElement(body, elem)
	}
	return strings.Join(parts, ",")
}

// expandElement substitutes {{.}} and {{.field}} placeholders within a single
// iteration element. {{.}} is replaced with the element value itself (for
// scalars). {{.field}} is replaced with the named field from a map element.
func expandElement(body string, elem any) string {
	// Replace {{.fieldName}} first (more specific), then {{.}}
	dotFieldRe := regexp.MustCompile(`\{\{\s*\.(\w+)\s*\}\}`)
	result := dotFieldRe.ReplaceAllStringFunc(body, func(match string) string {
		sub := dotFieldRe.FindStringSubmatch(match)
		fieldName := sub[1]
		if m, ok := elem.(map[string]any); ok {
			if v, exists := m[fieldName]; exists {
				return formatValue(v)
			}
		}
		return match
	})

	// Replace {{.}} with the element itself
	dotRe := regexp.MustCompile(`\{\{\s*\.\s*\}\}`)
	result = dotRe.ReplaceAllStringFunc(result, func(match string) string {
		return formatValue(elem)
	})

	return result
}

// formatValue converts a value to its string representation for template output.
// Arrays are marshaled as JSON. Strings are used directly. Other types use
// fmt.Sprintf.
func formatValue(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case []any:
		data, err := json.Marshal(val)
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return string(data)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// normalizeJSONPath converts a JSONPath expression to GJSON syntax.
// Strips leading "$." and converts bracket notation [N] to dot notation .N.
func normalizeJSONPath(path string) string {
	// Strip leading "$." or lone "$"
	if path == "$" {
		return ""
	}
	path = strings.TrimPrefix(path, "$.")

	// Convert [N] bracket notation to .N dot notation
	bracketRe := regexp.MustCompile(`\[(\d+)\]`)
	path = bracketRe.ReplaceAllString(path, ".$1")

	return path
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
			return count, fmt.Errorf("loading %s: %w", entry.Name(), err)
		}

		if err := registry.Register(tmpl.Adapter, NewTemplateAdapter(*tmpl)); err != nil {
			return count, fmt.Errorf("loading %s: %w", entry.Name(), err)
		}
		count++
	}

	return count, nil
}
