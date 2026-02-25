package plan

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ExprContext provides runtime values for expression evaluation.
type ExprContext struct {
	Now    time.Time           // anchor for "today" (default: time.Now())
	Env    func(string) string // env var lookup (default: os.Getenv)
	Values map[string]any      // already-resolved inputs for relative refs
}

// defaults fills in zero-valued fields with production defaults.
func (ec ExprContext) defaults() ExprContext {
	if ec.Now.IsZero() {
		ec.Now = time.Now()
	}
	if ec.Env == nil {
		ec.Env = os.Getenv
	}
	if ec.Values == nil {
		ec.Values = make(map[string]any)
	}
	return ec
}

// ContainsExpr reports whether s contains at least one {{...}} expression.
func ContainsExpr(s string) bool {
	return strings.Contains(s, "{{")
}

// ValidateExpr checks that all {{...}} expressions in raw are syntactically valid
// without evaluating them.
func ValidateExpr(raw string) error {
	segments, err := splitExprSegments(raw)
	if err != nil {
		return err
	}
	for _, seg := range segments {
		if seg.isExpr {
			if _, err := parseExprInner(seg.text); err != nil {
				return err
			}
		}
	}
	return nil
}

// EvalExpr evaluates expression templates in raw. If raw is not a string or
// contains no {{...}} delimiters, it is returned unchanged.
func EvalExpr(raw any, ctx ExprContext) (any, error) {
	s, ok := raw.(string)
	if !ok {
		return raw, nil
	}
	if !ContainsExpr(s) {
		return raw, nil
	}

	ctx = ctx.defaults()

	segments, err := splitExprSegments(s)
	if err != nil {
		return nil, err
	}

	// Single expression with no surrounding text: return the computed value directly.
	if len(segments) == 1 && segments[0].isExpr {
		return evalOneExpr(segments[0].text, ctx)
	}

	// Mixed: concatenate all segments as strings.
	var b strings.Builder
	for _, seg := range segments {
		if seg.isExpr {
			val, err := evalOneExpr(seg.text, ctx)
			if err != nil {
				return nil, err
			}
			b.WriteString(fmt.Sprintf("%v", val))
		} else {
			b.WriteString(seg.text)
		}
	}
	return b.String(), nil
}

// segment is a piece of the input string, either literal text or an expression.
type segment struct {
	text   string
	isExpr bool
}

// splitExprSegments splits a string into literal and expression segments.
func splitExprSegments(s string) ([]segment, error) {
	var segments []segment
	for {
		start := strings.Index(s, "{{")
		if start == -1 {
			if s != "" {
				segments = append(segments, segment{text: s})
			}
			break
		}
		if start > 0 {
			segments = append(segments, segment{text: s[:start]})
		}
		end := strings.Index(s[start:], "}}")
		if end == -1 {
			return nil, fmt.Errorf("unclosed expression delimiter in %q", s)
		}
		inner := strings.TrimSpace(s[start+2 : start+end])
		if inner == "" {
			return nil, fmt.Errorf("empty expression in %q", s)
		}
		segments = append(segments, segment{text: inner, isExpr: true})
		s = s[start+end+2:]
	}
	return segments, nil
}

// exprKind identifies the type of a parsed expression.
type exprKind int

const (
	exprToday exprKind = iota // "today" optionally with offset
	exprEnv                   // "env.VAR"
	exprRef                   // "identifier" optionally with offset
)

// parsedExpr is an intermediate representation of a single expression.
type parsedExpr struct {
	kind     exprKind
	envVar   string // for exprEnv
	refName  string // for exprRef
	offset   int    // days offset (positive or negative)
	hasArith bool   // whether arithmetic was specified
}

// Regex for expression parsing.
var (
	exprEnvRe       = regexp.MustCompile(`^env\.([A-Za-z_][A-Za-z0-9_]*)$`)
	exprArithRe     = regexp.MustCompile(`^(\S+)\s*([+-])\s*(\d+)\s+days?$`)
	exprIdentOnlyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func parseExprInner(inner string) (*parsedExpr, error) {
	inner = strings.TrimSpace(inner)

	// env.VAR
	if m := exprEnvRe.FindStringSubmatch(inner); m != nil {
		return &parsedExpr{kind: exprEnv, envVar: m[1]}, nil
	}

	// Arithmetic: <something> +/- N days
	if m := exprArithRe.FindStringSubmatch(inner); m != nil {
		base := m[1]
		sign := m[2]
		n, err := strconv.Atoi(m[3])
		if err != nil {
			return nil, fmt.Errorf("invalid day count in expression %q: %w", inner, err)
		}
		offset := n
		if sign == "-" {
			offset = -n
		}
		if base == "today" {
			return &parsedExpr{kind: exprToday, offset: offset, hasArith: true}, nil
		}
		if exprIdentOnlyRe.MatchString(base) {
			return &parsedExpr{kind: exprRef, refName: base, offset: offset, hasArith: true}, nil
		}
		return nil, fmt.Errorf("invalid expression base %q in %q", base, inner)
	}

	// Plain "today"
	if inner == "today" {
		return &parsedExpr{kind: exprToday}, nil
	}

	// Plain identifier reference
	if exprIdentOnlyRe.MatchString(inner) {
		return &parsedExpr{kind: exprRef, refName: inner}, nil
	}

	return nil, fmt.Errorf("invalid expression syntax: %q", inner)
}

func evalOneExpr(inner string, ctx ExprContext) (any, error) {
	pe, err := parseExprInner(inner)
	if err != nil {
		return nil, err
	}

	switch pe.kind {
	case exprToday:
		d := ctx.Now.AddDate(0, 0, pe.offset)
		return d.Format("2006-01-02"), nil

	case exprEnv:
		val := ctx.Env(pe.envVar)
		if val == "" {
			return nil, fmt.Errorf("environment variable %q is not set or empty", pe.envVar)
		}
		return val, nil

	case exprRef:
		raw, ok := ctx.Values[pe.refName]
		if !ok {
			return nil, fmt.Errorf("reference %q not found in resolved values", pe.refName)
		}
		if !pe.hasArith {
			// Plain reference: return as-is
			return raw, nil
		}
		// Date arithmetic on a reference
		dateStr, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("reference %q is %T, not a date string", pe.refName, raw)
		}
		t, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			return nil, fmt.Errorf("reference %q value %q is not a valid date (YYYY-MM-DD): %w", pe.refName, dateStr, err)
		}
		d := t.AddDate(0, 0, pe.offset)
		return d.Format("2006-01-02"), nil

	default:
		return nil, fmt.Errorf("unknown expression kind %d", pe.kind)
	}
}
