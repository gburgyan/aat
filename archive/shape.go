package archive

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/tidwall/gjson"
)

// maxSampleRunes is the length a shape's sample value is cut to.
const maxSampleRunes = 40

// ShapeLine describes one path of a JSON document's shape.
type ShapeLine struct {
	// Path is a gjson path from the root, with # standing for every element of
	// an array, such as data.items.#.sku, so it can go straight into an
	// extract rule. The root itself is @this.
	Path string `json:"path"`
	// Type is the JSON type at the path: object, array, string, number,
	// boolean, or null, or several joined by |, such as string|null.
	Type string `json:"type"`
	// Items is an array's length, or a range such as 1-3 when the arrays at
	// the path differ.
	Items string `json:"items,omitempty"`
	// Present and Of say that only Present of the Of objects at the parent
	// path hold this key. Both are zero when every one does.
	Present int `json:"present,omitempty"`
	Of      int `json:"of,omitempty"`
	// Sample is the first non-null scalar at the path, as JSON, cut to 40
	// characters.
	Sample string `json:"sample,omitempty"`
}

// Shape describes the structure of a JSON document, such as an archived
// response body: one line per path, in the order the paths first appear. The
// elements of an array are merged into one description, so a key that only
// some elements hold, or a value that is sometimes null, shows up. The root
// gets a line only when it is not an object. Shape walks the document with
// gjson instead of decoding it, so a body of tens of megabytes stays cheap. A
// document that is empty or not valid JSON has no shape.
func Shape(doc []byte) []ShapeLine {
	if !gjson.ValidBytes(doc) {
		return nil
	}
	root := newShapeNode("@this", false)
	root.visit(gjson.ParseBytes(doc))
	var lines []ShapeLine
	root.collect(&lines, nil)
	return lines
}

// RenderShape formats shape lines as aligned text: each path and its type,
// then, where they apply, an array's item count, how many objects hold the
// key, and a sample value.
func RenderShape(lines []ShapeLine) string {
	pathWidth, typeWidth := 0, 0
	for _, l := range lines {
		pathWidth = max(pathWidth, len(l.Path))
		typeWidth = max(typeWidth, len(l.Type))
	}
	pathWidth = min(pathWidth, 60)
	typeWidth = min(typeWidth, 20)

	var b strings.Builder
	for _, l := range lines {
		row := fmt.Sprintf("%-*s  %-*s", pathWidth, l.Path, typeWidth, l.Type)
		if l.Items != "" {
			unit := "items"
			if l.Items == "1" {
				unit = "item"
			}
			row += "  " + l.Items + " " + unit
		}
		if l.Of > 0 {
			row += fmt.Sprintf("  in %d of %d", l.Present, l.Of)
		}
		if l.Sample != "" {
			row += "  " + l.Sample
		}
		b.WriteString(strings.TrimRight(row, " "))
		b.WriteByte('\n')
	}
	return b.String()
}

// shapeNode accumulates what Shape saw at one path.
type shapeNode struct {
	path     string
	element  bool // the last component of the path is #
	types    []string
	values   int // values seen at the path
	objects  int // values that were objects
	arrays   int // values that were arrays
	minItems int
	maxItems int
	sample   string
	children []*shapeNode
	byKey    map[string]*shapeNode
}

func newShapeNode(path string, element bool) *shapeNode {
	return &shapeNode{path: path, element: element, byKey: map[string]*shapeNode{}}
}

// child returns the node for key under n, adding it when key is new.
func (n *shapeNode) child(key string) *shapeNode {
	if c, ok := n.byKey[key]; ok {
		return c
	}
	path := key
	if n.path != "@this" {
		path = n.path + "." + key
	}
	c := newShapeNode(path, key == "#")
	n.byKey[key] = c
	n.children = append(n.children, c)
	return c
}

// visit records v at n's path and walks into it.
func (n *shapeNode) visit(v gjson.Result) {
	n.values++
	n.addType(jsonType(v))
	switch {
	case v.IsObject():
		n.objects++
		v.ForEach(func(key, value gjson.Result) bool {
			n.child(gjson.Escape(key.String())).visit(value)
			return true
		})
	case v.IsArray():
		items := 0
		v.ForEach(func(_, value gjson.Result) bool {
			items++
			n.child("#").visit(value)
			return true
		})
		if n.arrays == 0 || items < n.minItems {
			n.minItems = items
		}
		n.maxItems = max(n.maxItems, items)
		n.arrays++
	case v.Type != gjson.Null && n.sample == "":
		n.sample = shortSample(v.Raw)
	}
}

func (n *shapeNode) addType(t string) {
	for _, seen := range n.types {
		if seen == t {
			return
		}
	}
	n.types = append(n.types, t)
}

// typeString joins the types seen at n, with null last.
func (n *shapeNode) typeString() string {
	types := make([]string, 0, len(n.types))
	null := false
	for _, t := range n.types {
		if t == "null" {
			null = true
			continue
		}
		types = append(types, t)
	}
	if null {
		types = append(types, "null")
	}
	return strings.Join(types, "|")
}

// collect appends the lines of n and of everything under it. parent is nil for
// the root.
func (n *shapeNode) collect(lines *[]ShapeLine, parent *shapeNode) {
	if parent != nil || n.typeString() != "object" {
		line := ShapeLine{Path: n.path, Type: n.typeString(), Sample: n.sample}
		if n.arrays > 0 {
			line.Items = itemRange(n.minItems, n.maxItems)
		}
		if parent != nil && !n.element && n.values < parent.objects {
			line.Present, line.Of = n.values, parent.objects
		}
		*lines = append(*lines, line)
	}
	for _, c := range n.children {
		c.collect(lines, n)
	}
}

// jsonType names the JSON type of v.
func jsonType(v gjson.Result) string {
	switch v.Type {
	case gjson.Null:
		return "null"
	case gjson.False, gjson.True:
		return "boolean"
	case gjson.Number:
		return "number"
	case gjson.String:
		return "string"
	case gjson.JSON:
		if v.IsArray() {
			return "array"
		}
	}
	return "object"
}

func itemRange(lo, hi int) string {
	if lo == hi {
		return strconv.Itoa(lo)
	}
	return strconv.Itoa(lo) + "-" + strconv.Itoa(hi)
}

// shortSample cuts a raw JSON value to maxSampleRunes characters.
func shortSample(raw string) string {
	if utf8.RuneCountInString(raw) <= maxSampleRunes {
		return raw
	}
	count := 0
	for i := range raw {
		if count == maxSampleRunes-1 {
			return raw[:i] + "…"
		}
		count++
	}
	return raw
}
