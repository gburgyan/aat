package plan

import (
	crand "crypto/rand"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ExprContext provides runtime values for expression evaluation.
type ExprContext struct {
	Now    time.Time           // anchor for "today", "now", and "unixtime" (default: time.Now())
	Env    func(string) string // env var lookup (default: os.Getenv)
	Values map[string]any      // already-resolved inputs for relative refs
	Random io.Reader           // source for "uuid" and "random N" (default: crypto/rand)
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
	if ec.Random == nil {
		ec.Random = crand.Reader
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
			fmt.Fprintf(&b, "%v", val)
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
	exprToday    exprKind = iota // "today" optionally with offset
	exprEnv                      // "env.VAR"
	exprRef                      // "identifier" optionally with offset
	exprUUID                     // "uuid"
	exprRandom                   // "random N"
	exprNow                      // "now" optionally with a time offset
	exprUnixtime                 // "unixtime" optionally with a time offset
)

// parsedExpr is an intermediate representation of a single expression.
type parsedExpr struct {
	kind     exprKind
	envVar   string        // for exprEnv
	refName  string        // for exprRef
	offset   int           // days offset (positive or negative)
	hasArith bool          // whether arithmetic was specified
	length   int           // for exprRandom
	duration time.Duration // time offset for exprNow and exprUnixtime
}

// maxRandomLength is the longest value {{random N}} generates.
const maxRandomLength = 64

// Regex for expression parsing.
var (
	exprEnvRe       = regexp.MustCompile(`^env\.([A-Za-z_][A-Za-z0-9_]*)$`)
	exprArithRe     = regexp.MustCompile(`^(\S+)\s*([+-])\s*(\d+)\s+days?$`)
	exprIdentOnlyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	exprClockRe     = regexp.MustCompile(`^(now|unixtime)(?:\s*([+-])\s*(\d+)\s+([A-Za-z]+))?$`)
	exprRandomRe    = regexp.MustCompile(`^random\s+(\S+)$`)
	exprOffsetRe    = regexp.MustCompile(`^(\S+)\s*([+-])\s*(\d+)\s+([A-Za-z]+)$`)
)

func parseExprInner(inner string) (*parsedExpr, error) {
	inner = strings.TrimSpace(inner)

	// env.VAR
	if m := exprEnvRe.FindStringSubmatch(inner); m != nil {
		return &parsedExpr{kind: exprEnv, envVar: m[1]}, nil
	}

	// now and unixtime, optionally +/- N seconds, minutes, hours, or days
	if m := exprClockRe.FindStringSubmatch(inner); m != nil {
		pe := &parsedExpr{kind: exprNow}
		if m[1] == "unixtime" {
			pe.kind = exprUnixtime
		}
		if m[2] != "" {
			d, err := offsetDuration(m[3], m[4], inner)
			if err != nil {
				return nil, err
			}
			if m[2] == "-" {
				d = -d
			}
			pe.duration = d
		}
		return pe, nil
	}

	// uuid
	if inner == "uuid" {
		return &parsedExpr{kind: exprUUID}, nil
	}

	// random N
	if m := exprRandomRe.FindStringSubmatch(inner); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 || n > maxRandomLength {
			return nil, fmt.Errorf("random takes a length from 1 to %d, not %q, in %q", maxRandomLength, m[1], inner)
		}
		return &parsedExpr{kind: exprRandom, length: n}, nil
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
		if base == "uuid" || base == "random" {
			return nil, fmt.Errorf("%s takes no offset, in %q", base, inner)
		}
		if base == "today" {
			return &parsedExpr{kind: exprToday, offset: offset, hasArith: true}, nil
		}
		if exprIdentOnlyRe.MatchString(base) {
			return &parsedExpr{kind: exprRef, refName: base, offset: offset, hasArith: true}, nil
		}
		return nil, fmt.Errorf("invalid expression base %q in %q", base, inner)
	}

	// An offset in a unit other than days, on a base that counts days or on a
	// generated value
	if m := exprOffsetRe.FindStringSubmatch(inner); m != nil {
		base := m[1]
		if base == "uuid" || base == "random" {
			return nil, fmt.Errorf("%s takes no offset, in %q", base, inner)
		}
		if base == "today" || exprIdentOnlyRe.MatchString(base) {
			return nil, fmt.Errorf("%s counts days, in %q; for a time use {{now %s %s %s}} or {{unixtime %s %s %s}}",
				base, inner, m[2], m[3], m[4], m[2], m[3], m[4])
		}
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

	case exprUUID:
		return newUUID(ctx.Random)

	case exprRandom:
		return randomString(ctx.Random, pe.length)

	case exprNow:
		return ctx.Now.Add(pe.duration).UTC().Format(time.RFC3339), nil

	case exprUnixtime:
		return ctx.Now.Add(pe.duration).Unix(), nil

	case exprRef:
		raw, ok := ctx.Values[pe.refName]
		if !ok {
			if pe.refName == "random" {
				return nil, fmt.Errorf("reference %q not found in resolved values; for a random value write {{random N}}", pe.refName)
			}
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

// offsetDuration converts an offset of number units, as written in expression
// inner, to a duration. A day is 24 hours.
func offsetDuration(number, unit, inner string) (time.Duration, error) {
	n, err := strconv.ParseInt(number, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid offset in expression %q: %w", inner, err)
	}
	var per time.Duration
	switch strings.TrimSuffix(strings.ToLower(unit), "s") {
	case "second":
		per = time.Second
	case "minute":
		per = time.Minute
	case "hour":
		per = time.Hour
	case "day":
		per = 24 * time.Hour
	default:
		return 0, fmt.Errorf("unknown time unit %q in expression %q; use seconds, minutes, hours, or days", unit, inner)
	}
	if n > int64(math.MaxInt64/per) {
		return 0, fmt.Errorf("offset too large in expression %q", inner)
	}
	return time.Duration(n) * per, nil
}

// newUUID returns a random version 4 UUID, in lowercase, built from bytes read
// from r.
func newUUID(r io.Reader) (string, error) {
	var b [16]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return "", fmt.Errorf("generating uuid: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// randomAlphabet is what {{random N}} draws from: digits and lowercase letters,
// which need no escaping in a path, query, header, or JSON string, and stay
// distinct where case is ignored.
const randomAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// randomString returns n characters drawn uniformly from randomAlphabet, using
// bytes read from r. A byte of 252 or more is skipped, since 252 is the largest
// multiple of 36 below 256, so every character is equally likely.
func randomString(r io.Reader, n int) (string, error) {
	out := make([]byte, 0, n)
	buf := make([]byte, n)
	for len(out) < n {
		chunk := buf[:n-len(out)]
		if _, err := io.ReadFull(r, chunk); err != nil {
			return "", fmt.Errorf("generating random value: %w", err)
		}
		for _, c := range chunk {
			if c < 252 {
				out = append(out, randomAlphabet[c%36])
			}
		}
	}
	return string(out), nil
}
