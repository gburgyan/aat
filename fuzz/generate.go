// Package fuzz generates the values a fuzz run sends to a step's inputs. It
// reads only what the project already declares: each input's type and
// constraints, and the domain file's types and value pools. Nothing about
// fuzzing is written in the graph.
//
// Cases come in three modes (see plan.FuzzPositive, FuzzNegative, and
// FuzzEdge). The list for a step is deterministic; a seed only chooses which
// cases run when a limit caps them.
package fuzz

import (
	"fmt"
	"math"
	"math/rand/v2"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/plan"
)

// Target is a step to generate cases for.
type Target struct {
	// Step is the step as instantiated: graph defaults and layers merged into
	// its values.
	Step plan.Step
	Node *graph.Node
	KB   *domain.KnowledgeBase // may be nil
	// Template is the node's request template; nil for a custom adapter.
	// It says where each input is sent, and which fields the template writes
	// itself, for the cases that leave a field out or change its type.
	Template *adapter.Template
}

// Options shape the cases Generate returns.
type Options struct {
	// Modes limits the cases to these modes; empty means all three.
	Modes []string
	// Inputs limits the cases to these inputs. Empty means every input whose
	// value is not wired from another step or input; an input named here is
	// fuzzed even when it is wired.
	Inputs []string
	// Max caps the number of cases; 0 means all of them. When it caps, Seed
	// picks which run.
	Max  int
	Seed uint64
	// Now dates the date cases; zero means time.Now.
	Now time.Time
}

// AllModes lists the case modes in the order cases are generated.
var AllModes = []string{plan.FuzzPositive, plan.FuzzNegative, plan.FuzzEdge}

// Generate returns the cases for a step, positive first, input by input in
// the order the node declares its inputs.
func Generate(t Target, opts Options) ([]plan.FuzzCase, error) {
	cases, _, err := GenerateCapped(t, opts)
	return cases, err
}

// GenerateCapped is Generate, and also reports whether Options.Max dropped
// cases, which makes the run's seed worth reporting.
func GenerateCapped(t Target, opts Options) ([]plan.FuzzCase, bool, error) {
	modes := map[string]bool{}
	for _, m := range opts.Modes {
		if !contains(AllModes, m) {
			return nil, false, fmt.Errorf("unknown fuzz mode %q: use %s", m, strings.Join(AllModes, ", "))
		}
		modes[m] = true
	}
	if len(modes) == 0 {
		for _, m := range AllModes {
			modes[m] = true
		}
	}
	named := map[string]bool{}
	for _, in := range opts.Inputs {
		if !hasInput(t.Node, in) {
			return nil, false, fmt.Errorf("node %s has no input %q to fuzz", t.Node.Name, in)
		}
		named[in] = true
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}

	var fields []adapter.RequestField
	if t.Template != nil {
		fields, _ = t.Template.RequestFields()
	}

	var cases []plan.FuzzCase
	keep := func(cs []plan.FuzzCase) {
		for _, c := range cs {
			if modes[c.Mode] {
				cases = append(cases, c)
			}
		}
	}
	for _, in := range t.Node.Inputs {
		if len(named) > 0 && !named[in.Name] {
			continue
		}
		if len(named) == 0 && wired(t.Step.Values[in.Name]) {
			continue
		}
		keep(inputCases(in, t.Step.Values[in.Name], t.KB, now))
		keep(absenceCases(in, t.Step.Values[in.Name], fields))
	}
	if len(named) == 0 {
		// A protobuf message has no room for a field its type doesn't declare.
		grpc := t.Template != nil && t.Template.Protocol == adapter.ProtocolGRPC
		keep(templateCases(fields, !grpc))
	}
	sort.SliceStable(cases, func(i, j int) bool { return modeRank(cases[i].Mode) < modeRank(cases[j].Mode) })

	if opts.Max > 0 && len(cases) > opts.Max {
		r := rand.New(rand.NewPCG(opts.Seed, 0x66757a7a)) // "fuzz"
		picked := r.Perm(len(cases))[:opts.Max]
		sort.Ints(picked)
		capped := make([]plan.FuzzCase, len(picked))
		for i, p := range picked {
			capped[i] = cases[p]
		}
		return capped, true, nil
	}
	return cases, false, nil
}

// absenceCases lists the cases that leave an input out of the request, or
// send it as null, by patching the fields the template puts it in. An input
// only in the path, or only inside a conditional block, gets none.
func absenceCases(in graph.Input, sv plan.StepValue, fields []adapter.RequestField) []plan.FuzzCase {
	var remove, null []plan.RequestPatch
	for _, f := range fields {
		if f.Input != in.Name {
			continue
		}
		if f.InBlock {
			return nil // sent only when present, so leaving it out is the template's own case
		}
		remove = append(remove, plan.RequestPatch{Where: f.Where, Path: f.Path, Op: adapter.PatchRemove})
		if f.Where == adapter.FieldBody {
			null = append(null, plan.RequestPatch{Where: f.Where, Path: f.Path, Op: adapter.PatchSet})
		}
	}
	if len(remove) == 0 {
		return nil
	}
	// An optional input with no value is left out already.
	if in.Optional && sv.IsEmpty() && (in.Default == nil || !in.Default.HasValue()) {
		return nil
	}
	b := &builder{input: in.Name}
	if in.Optional {
		b.addPatch(plan.FuzzPositive, "missing", remove)
		b.addPatch(plan.FuzzEdge, "null", null)
	} else {
		b.addPatch(plan.FuzzNegative, "missing", remove)
		b.addPatch(plan.FuzzNegative, "null", null)
	}
	return b.cases
}

// templateCases lists the cases for the fields a template writes itself,
// rather than filling from an input: leaving each out, sending it as null or
// as another type, emptying an object or array, and adding a property the
// template never sends. Nothing declares whether such a field is required, so
// they are edge cases; an OpenAPI spec, when there is one, judges them.
// Fields inside blocks and elements of arrays are left alone.
func templateCases(fields []adapter.RequestField, extraProperty bool) []plan.FuzzCase {
	b := &builder{input: "body"}
	hasBody := false
	for _, f := range fields {
		if f.Where == adapter.FieldBody {
			hasBody = true
		}
		if f.Input != "" || f.InBlock || f.Where != adapter.FieldBody && f.Where != adapter.FieldQuery {
			continue
		}
		if inArray(f.Path) {
			continue
		}
		prefix := f.Where + "." + f.Path + "."
		remove := []plan.RequestPatch{{Where: f.Where, Path: f.Path, Op: adapter.PatchRemove}}
		b.addPatchID(prefix+"remove", plan.FuzzEdge, "remove", remove)
		if f.Where == adapter.FieldQuery {
			continue
		}
		set := func(v any) []plan.RequestPatch {
			return []plan.RequestPatch{{Where: f.Where, Path: f.Path, Op: adapter.PatchSet, Value: v}}
		}
		switch f.Kind {
		case "object":
			b.addPatchID(prefix+"empty", plan.FuzzEdge, "empty", set(map[string]any{}))
		case "array":
			b.addPatchID(prefix+"empty", plan.FuzzEdge, "empty", set([]any{}))
		case "string":
			b.addPatchID(prefix+"null", plan.FuzzEdge, "null", set(nil))
			b.addPatchID(prefix+"wrong-type", plan.FuzzEdge, "wrong-type", set(12345))
		case "number":
			b.addPatchID(prefix+"null", plan.FuzzEdge, "null", set(nil))
			b.addPatchID(prefix+"wrong-type", plan.FuzzEdge, "wrong-type", set("x"))
		case "boolean":
			b.addPatchID(prefix+"null", plan.FuzzEdge, "null", set(nil))
			b.addPatchID(prefix+"wrong-type", plan.FuzzEdge, "wrong-type", set("yes"))
		}
	}
	if hasBody && extraProperty {
		b.addPatchID("body.extra-property", plan.FuzzEdge, "extra-property",
			[]plan.RequestPatch{{Where: adapter.FieldBody, Path: "aatFuzzExtra", Op: adapter.PatchSet, Value: "x"}})
	}
	return b.cases
}

// inArray reports whether a GJSON path goes through an array element.
func inArray(path string) bool {
	for _, seg := range strings.Split(path, ".") {
		if seg != "" && strings.Trim(seg, "0123456789") == "" {
			return true
		}
	}
	return false
}

// wired reports whether a step value comes from another step or input, which
// fuzzing leaves alone unless asked: an ID read from an earlier response is
// what lets the step reach the state the plan built.
func wired(sv plan.StepValue) bool {
	return sv.From != "" || sv.FromSelection != "" || sv.FromResolved != "" || sv.FromInput != ""
}

// inputCases lists the cases for one input.
func inputCases(in graph.Input, sv plan.StepValue, kb *domain.KnowledgeBase, now time.Time) []plan.FuzzCase {
	ft, err := graph.ParseFieldType(in.Type)
	if err != nil {
		ft = graph.FieldType{Kind: graph.TypeScalar, Name: "string"}
	}
	b := &builder{input: in.Name}
	if ft.IsArray {
		b.add(plan.FuzzEdge, "empty-list", []any{})
		return b.cases
	}

	c := in.Constraints
	if c == nil {
		c = &graph.Constraint{}
	}
	pattern := c.Pattern
	var pool []string

	switch ft.Kind {
	case graph.TypeEnum:
		enumCases(b, ft.EnumValues)
		return b.cases
	case graph.TypeCustom:
		if td := kb.GetType(ft.Name); td != nil {
			if pattern == "" {
				pattern = td.Validation
			}
			if td.Pool != "" {
				pool = kb.AllValues(td.Pool)
			}
		}
		ft = graph.FieldType{Kind: graph.TypeScalar, Name: "string"}
	}
	if sv.PoolRef != "" {
		if values, err := kb.PoolRefValues(sv.PoolRef); err == nil {
			pool = values
		}
	}
	for _, v := range sv.Pool {
		if s, ok := v.(string); ok && !strings.Contains(s, "{{") {
			pool = append(pool, s)
		}
	}

	switch ft.Name {
	case "integer":
		numberCases(b, c, true)
	case "float", "money":
		numberCases(b, c, false)
	case "boolean":
		b.add(plan.FuzzPositive, "true", true)
		b.add(plan.FuzzPositive, "false", false)
		b.add(plan.FuzzNegative, "wrong-type", wrongType("not-a-boolean"))
	case "date":
		b.add(plan.FuzzPositive, "today", now.Format("2006-01-02"))
		b.add(plan.FuzzPositive, "next-year", now.AddDate(1, 0, 0).Format("2006-01-02"))
		b.add(plan.FuzzNegative, "invalid-date", "2026-13-45")
		b.add(plan.FuzzNegative, "not-a-date", "not-a-date")
		b.add(plan.FuzzEdge, "far-past", "1900-01-01")
	case "datetime":
		b.add(plan.FuzzPositive, "now", now.UTC().Format(time.RFC3339))
		b.add(plan.FuzzNegative, "invalid-datetime", "2026-13-45T25:61:61Z")
		b.add(plan.FuzzNegative, "not-a-datetime", "not-a-datetime")
	default:
		stringCases(b, c, pattern, pool)
	}
	return b.cases
}

func enumCases(b *builder, values []string) {
	for _, v := range values {
		b.add(plan.FuzzPositive, "enum-"+v, v)
	}
	b.add(plan.FuzzNegative, "not-in-enum", "aat-fuzz-not-a-member")
	if len(values) > 0 {
		if flipped := flipCase(values[0]); flipped != values[0] && !contains(values, flipped) {
			b.add(plan.FuzzNegative, "enum-wrong-case", flipped)
		}
	}
	b.add(plan.FuzzNegative, "empty", "")
}

func numberCases(b *builder, c *graph.Constraint, integer bool) {
	num := func(f float64) any {
		if integer {
			return int64(f)
		}
		return f
	}
	inRange := func(f float64) bool {
		return (c.Min == nil || f >= *c.Min) && (c.Max == nil || f <= *c.Max)
	}
	step := 1.0
	if !integer {
		step = 0.01
	}
	if c.Min != nil {
		b.add(plan.FuzzPositive, "at-min", num(*c.Min))
		b.add(plan.FuzzNegative, "below-min", num(*c.Min-step))
	}
	if c.Max != nil {
		b.add(plan.FuzzPositive, "at-max", num(*c.Max))
		b.add(plan.FuzzNegative, "above-max", num(*c.Max+step))
	}
	bounded := c.Min != nil || c.Max != nil
	if inRange(0) && (c.Min == nil || *c.Min != 0) && (c.Max == nil || *c.Max != 0) {
		// Without bounds the graph doesn't say whether 0 is allowed.
		if bounded {
			b.add(plan.FuzzPositive, "zero", num(0))
		} else {
			b.add(plan.FuzzEdge, "zero", num(0))
		}
	}
	if c.Min == nil && inRange(-1) {
		b.add(plan.FuzzEdge, "negative", num(-1))
	}
	if c.Max == nil {
		b.add(plan.FuzzEdge, "large", num(2147483648))
	}
	if integer {
		b.add(plan.FuzzNegative, "fraction", 1.5)
		b.add(plan.FuzzNegative, "overflow", uint64(math.MaxUint64))
	}
	b.add(plan.FuzzNegative, "wrong-type", wrongType("not-a-number"))
}

// wrongType returns a JSON string literal, quotes included, for an input that
// takes a number or a boolean. Such an input usually sits in an unquoted
// template slot, {"quantity": {{quantity}}}, where a string goes in as JSON
// text, so the quotes make it a JSON string rather than broken JSON. In a
// quoted or URL slot it is still text where a number belongs.
func wrongType(s string) string {
	return `"` + s + `"`
}

func stringCases(b *builder, c *graph.Constraint, pattern string, pool []string) {
	for i, v := range samplePool(pool) {
		b.add(plan.FuzzPositive, fmt.Sprintf("pool-%d", i+1), v)
	}
	if c.MinLength != nil && *c.MinLength > 0 {
		b.add(plan.FuzzPositive, "at-min-length", fill(*c.MinLength))
		b.add(plan.FuzzNegative, "below-min-length", fill(*c.MinLength-1))
	}
	if c.MaxLength != nil {
		b.add(plan.FuzzPositive, "at-max-length", fill(*c.MaxLength))
		b.add(plan.FuzzNegative, "above-max-length", fill(*c.MaxLength+1))
	}
	if pattern != "" {
		if re, err := regexp.Compile(pattern); err == nil {
			for _, candidate := range []string{"aat fuzz!", "!@#$%", "0", "a", ""} {
				if !re.MatchString(candidate) {
					b.add(plan.FuzzNegative, "pattern-mismatch", candidate)
					break
				}
			}
		}
	}
	for _, e := range edgeStrings {
		b.add(plan.FuzzEdge, e.name, e.value)
	}
}

// edgeStrings are strings a declared type or constraint rarely rules out but
// servers often mishandle.
var edgeStrings = []struct{ name, value string }{
	{"empty", ""},
	{"whitespace", "   "},
	{"unicode", "Zoë Ünïcödé 名前 🚀"},
	{"right-to-left", "\u202eabc"},
	{"control-chars", "a\x00b\x1bc"},
	{"long", strings.Repeat("a", 10000)},
	{"sql-quote", "' OR '1'='1"},
	{"markup", "<script>alert(1)</script>"},
	{"path-traversal", "../../etc/passwd"},
	{"format-string", "%s%s%n"},
	{"template", "{{7*7}}"},
}

// samplePool returns up to three values of a pool, spread across it, so a
// large pool does not flood the cases.
func samplePool(pool []string) []string {
	if len(pool) <= 3 {
		return pool
	}
	return []string{pool[0], pool[len(pool)/2], pool[len(pool)-1]}
}

type builder struct {
	input string
	cases []plan.FuzzCase
}

func (b *builder) add(mode, strategy string, value any) {
	id := b.input + "." + strategy
	for _, c := range b.cases {
		if c.ID == id {
			return
		}
	}
	b.cases = append(b.cases, plan.FuzzCase{ID: id, Mode: mode, Input: b.input, Strategy: strategy, Value: value})
}

func (b *builder) addPatch(mode, strategy string, patch []plan.RequestPatch) {
	if len(patch) == 0 {
		return
	}
	b.addPatchID(b.input+"."+strategy, mode, strategy, patch)
}

func (b *builder) addPatchID(id, mode, strategy string, patch []plan.RequestPatch) {
	for _, c := range b.cases {
		if c.ID == id {
			return
		}
	}
	input := b.input
	if input == "body" {
		input = ""
	}
	b.cases = append(b.cases, plan.FuzzCase{ID: id, Mode: mode, Input: input, Strategy: strategy, Patch: patch})
}

func fill(n int) string {
	if n < 0 {
		n = 0
	}
	return strings.Repeat("a", n)
}

func flipCase(s string) string {
	if u := strings.ToUpper(s); u != s {
		return u
	}
	return strings.ToLower(s)
}

func modeRank(m string) int {
	for i, x := range AllModes {
		if x == m {
			return i
		}
	}
	return len(AllModes)
}

func hasInput(n *graph.Node, name string) bool {
	for _, in := range n.Inputs {
		if in.Name == name {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
