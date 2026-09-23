// Package yamlx decodes project YAML strictly: a key that no struct field
// accepts is an error naming its line, instead of being silently dropped. It
// is a foundation package (stdlib and yaml.v3 only) used by every loader of
// project files.
//
// Strictness reaches custom unmarshalers only through the older callback form,
// UnmarshalYAML(unmarshal func(any) error): yaml.v3 runs that callback on the
// decoder that is already decoding, with its settings, line numbers, and
// anchors, while Node.Decode inside UnmarshalYAML(*yaml.Node) starts a fresh
// decoder that ignores unknown keys. Types that need to branch on the node's
// kind call Node(unmarshal) first. Alias types used to decode "the struct
// without its methods" are named raw<Type>, which error messages rely on.
package yamlx

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Error lists every problem found in one YAML document.
type Error struct {
	Issues []Issue
}

// Issue is one problem in a YAML document. Line is 0 when unknown.
type Issue struct {
	Line    int
	Message string
}

func (i Issue) String() string {
	if i.Line > 0 {
		return fmt.Sprintf("line %d: %s", i.Line, i.Message)
	}
	return i.Message
}

// Error renders one issue inline and several as an indented list, so a caller's
// prefix ("plans/smoke.yaml: ") reads well either way.
func (e *Error) Error() string {
	if len(e.Issues) == 1 {
		return e.Issues[0].String()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d problems:", len(e.Issues))
	for _, issue := range e.Issues {
		b.WriteString("\n  ")
		b.WriteString(issue.String())
	}
	return b.String()
}

// Decode unmarshals the document in data into v, rejecting keys that no field
// of the target type accepts. An empty or comment-only document leaves v
// untouched and returns nil, as yaml.Unmarshal does. A second document with
// content is an error, since it would otherwise be silently ignored; an empty
// one, such as a trailing "---", is not. Errors are *Error values whose issues
// name the line, the key, and a suggestion for near misses; syntax errors read
// "invalid YAML: ...".
func Decode(data []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	err := dec.Decode(v)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return secondDocument(dec)
	}

	var typeErr *yaml.TypeError
	if errors.As(err, &typeErr) {
		out := &Error{}
		for _, msg := range typeErr.Errors {
			out.Issues = append(out.Issues, rewrite(msg, reflect.TypeOf(v)))
		}
		return out
	}
	return syntaxError(err)
}

// secondDocument reports a document after the first that has content.
func secondDocument(dec *yaml.Decoder) error {
	for {
		var doc yaml.Node
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return syntaxError(err)
		}
		if isEmptyDocument(&doc) {
			continue
		}
		return &Error{Issues: []Issue{{
			Line:    doc.Line,
			Message: "a second YAML document starts here; a project file holds exactly one",
		}}}
	}
}

// isEmptyDocument reports whether doc holds nothing, as after a trailing "---".
func isEmptyDocument(doc *yaml.Node) bool {
	if len(doc.Content) == 0 {
		return true
	}
	c := doc.Content[0]
	return len(doc.Content) == 1 && c.Kind == yaml.ScalarNode && c.Tag == "!!null" && c.Value == ""
}

// syntaxError renders a yaml.v3 syntax error, which leaves out the line when
// it is the first one.
func syntaxError(err error) error {
	issue := parseLine(strings.TrimPrefix(err.Error(), "yaml: "))
	issue.Message = "invalid YAML: " + issue.Message
	return &Error{Issues: []Issue{issue}}
}

// Node returns the node an UnmarshalYAML(func(any) error) method is decoding,
// so the method can branch on its kind before decoding it with the same
// callback.
func Node(unmarshal func(any) error) (*yaml.Node, error) {
	var capture nodeCapture
	if err := unmarshal(&capture); err != nil {
		return nil, err
	}
	return capture.node, nil
}

type nodeCapture struct{ node *yaml.Node }

func (c *nodeCapture) UnmarshalYAML(n *yaml.Node) error {
	c.node = n
	return nil
}

// KindError reports a node of the wrong shape, such as a list where a mapping
// belongs. It is a *yaml.TypeError, so the decoder records it and goes on
// collecting other problems in the document.
func KindError(n *yaml.Node, what, want string) error {
	return &yaml.TypeError{Errors: []string{
		fmt.Sprintf("line %d: %s must be %s, found %s", n.Line, what, want, kindName(n.Kind)),
	}}
}

func kindName(k yaml.Kind) string {
	switch k {
	case yaml.ScalarNode:
		return "a scalar"
	case yaml.SequenceNode:
		return "a list"
	case yaml.MappingNode:
		return "a mapping"
	case yaml.AliasNode:
		return "an alias"
	default:
		return "nothing"
	}
}

var (
	lineRe       = regexp.MustCompile(`^line (\d+): (.*)$`)
	unknownRe    = regexp.MustCompile(`^field (\S+) not found in type (\S+)$`)
	alreadySetRe = regexp.MustCompile(`^field (\S+) already set in type (\S+)$`)
	dupKeyRe     = regexp.MustCompile(`^mapping key "(.*)" already defined at line \d+$`)
	seqIntoMapRe = regexp.MustCompile(`^cannot unmarshal !!seq into (map\[|struct|[a-z]+\.)`)
	mapIntoSeqRe = regexp.MustCompile(`^cannot unmarshal !!map into \[\]`)
)

// parseLine splits a "line N: message" string into an Issue.
func parseLine(msg string) Issue {
	if m := lineRe.FindStringSubmatch(msg); m != nil {
		line, _ := strconv.Atoi(m[1])
		return Issue{Line: line, Message: m[2]}
	}
	return Issue{Message: msg}
}

// rewrite turns one yaml.v3 type error into a readable issue. Messages it does
// not recognize pass through unchanged.
func rewrite(msg string, target reflect.Type) Issue {
	issue := parseLine(msg)
	switch {
	case unknownRe.MatchString(issue.Message):
		m := unknownRe.FindStringSubmatch(issue.Message)
		issue.Message = unknownKeyMessage(m[1], m[2], target)
	case alreadySetRe.MatchString(issue.Message):
		m := alreadySetRe.FindStringSubmatch(issue.Message)
		issue.Message = fmt.Sprintf("duplicate key %q", m[1])
	case dupKeyRe.MatchString(issue.Message):
		m := dupKeyRe.FindStringSubmatch(issue.Message)
		issue.Message = fmt.Sprintf("duplicate key %q", m[1])
	case seqIntoMapRe.MatchString(issue.Message):
		issue.Message = "expected a mapping, found a list"
	case mapIntoSeqRe.MatchString(issue.Message):
		issue.Message = "expected a list, found a mapping"
	}
	return issue
}

// unknownKeyMessage renders `unknown key "k" in <noun>` with a suggestion.
func unknownKeyMessage(key, typeName string, target reflect.Type) string {
	msg := fmt.Sprintf("unknown key %q in %s", key, noun(typeName))
	valid := validKeys(target, typeName)
	if len(valid) == 0 {
		return msg
	}
	if suggestion := Closest(key, valid); suggestion != "" {
		return fmt.Sprintf("%s (did you mean %q?)", msg, suggestion)
	}
	return fmt.Sprintf("%s (valid keys: %s)", msg, strings.Join(valid, ", "))
}

// noun turns a Go type name such as "plan.rawStepValue" into "step value".
// Suffixes that only matter in Go ("Def", "Partial") are dropped, so
// config.EnvironmentPartial reads as "environment".
func noun(typeName string) string {
	name := typeName
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimPrefix(name, "raw")
	if name == "" || strings.ContainsAny(name, "{[ ") {
		return "mapping"
	}
	for _, suffix := range []string{"Partial", "Def"} {
		if trimmed := strings.TrimSuffix(name, suffix); trimmed != name && trimmed != "" {
			name = trimmed
			break
		}
	}
	var words []string
	runes := []rune(name)
	start := 0
	for i := 1; i < len(runes); i++ {
		prevLower := unicode.IsLower(runes[i-1])
		nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
		if unicode.IsUpper(runes[i]) && (prevLower || (unicode.IsUpper(runes[i-1]) && nextLower)) {
			words = append(words, string(runes[start:i]))
			start = i
		}
	}
	words = append(words, string(runes[start:]))
	return strings.ToLower(strings.Join(words, " "))
}

// validKeys finds the struct type named typeName (or its raw<Type> original)
// among the types reachable from target and returns the YAML keys it accepts.
func validKeys(target reflect.Type, typeName string) []string {
	want := typeName
	if i := strings.LastIndex(want, "."); i >= 0 {
		want = want[:i+1] + strings.TrimPrefix(want[i+1:], "raw")
	}
	found := findType(target, want, map[reflect.Type]bool{})
	if found == nil {
		return nil
	}
	var keys []string
	collectKeys(found, &keys)
	sort.Strings(keys)
	return keys
}

func findType(t reflect.Type, name string, seen map[reflect.Type]bool) reflect.Type {
	if t == nil || seen[t] {
		return nil
	}
	seen[t] = true
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return findType(t.Elem(), name, seen)
	case reflect.Map:
		return findType(t.Elem(), name, seen)
	case reflect.Struct:
		// Case-insensitive: stripping "raw" from rawStepValue leaves StepValue,
		// and an unexported original such as stepValue should match too.
		if strings.EqualFold(t.String(), name) {
			return t
		}
		for i := 0; i < t.NumField(); i++ {
			if found := findType(t.Field(i).Type, name, seen); found != nil {
				return found
			}
		}
	}
	return nil
}

func collectKeys(t reflect.Type, keys *[]string) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("yaml")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		inline := strings.Contains(opts, "inline")
		if !f.IsExported() && (!f.Anonymous || !inline) {
			continue
		}
		if inline {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				collectKeys(ft, keys)
			}
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		*keys = append(*keys, name)
	}
}

// Closest returns the valid key that key most likely misspells: the same key
// in another case, or one within an edit distance of 2. It returns "" when
// nothing is close.
func Closest(key string, valid []string) string {
	best, bestDist := "", 3
	for _, candidate := range valid {
		if strings.EqualFold(candidate, key) {
			return candidate
		}
		if d := editDistance(strings.ToLower(key), strings.ToLower(candidate)); d < bestDist {
			best, bestDist = candidate, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
