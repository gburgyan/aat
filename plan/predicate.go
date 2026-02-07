package plan

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// EvalPredicate parses and evaluates a predicate expression against a context map.
// It returns true/false or an error if the expression is invalid or evaluation fails.
func EvalPredicate(expr string, context map[string]any) (bool, error) {
	tokens, err := tokenize(expr)
	if err != nil {
		return false, fmt.Errorf("tokenize: %w", err)
	}
	p := &parser{tokens: tokens}
	node, err := p.parseExpression()
	if err != nil {
		return false, fmt.Errorf("parse: %w", err)
	}
	if p.peek().kind != tokenEOF {
		return false, fmt.Errorf("parse: unexpected token %q after expression", p.peek().value)
	}
	result, err := eval(node, context)
	if err != nil {
		return false, err
	}
	b, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("expression result is %T, not bool", result)
	}
	return b, nil
}

// ValidatePredicate checks that an expression can be parsed without evaluating it.
func ValidatePredicate(expr string) error {
	tokens, err := tokenize(expr)
	if err != nil {
		return fmt.Errorf("tokenize: %w", err)
	}
	p := &parser{tokens: tokens}
	if _, err := p.parseExpression(); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if p.peek().kind != tokenEOF {
		return fmt.Errorf("parse: unexpected token %q after expression", p.peek().value)
	}
	return nil
}

// --- Token types ---

type tokenKind int

const (
	tokenNumber   tokenKind = iota // 500, 3.14
	tokenString                    // 'economy'
	tokenBool                      // true, false
	tokenIdent                     // price.amount, carrier
	tokenOperator                  // ==, !=, <, >, <=, >=, &&, ||, !
	tokenLParen                    // (
	tokenRParen                    // )
	tokenLBracket                  // [
	tokenRBracket                  // ]
	tokenComma                     // ,
	tokenIn                        // in (keyword)
	tokenEOF
)

type token struct {
	kind  tokenKind
	value string
}

// --- Tokenizer ---

func tokenize(input string) ([]token, error) {
	var tokens []token
	i := 0
	for i < len(input) {
		ch := input[i]

		// Skip whitespace
		if ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' {
			i++
			continue
		}

		// Single-char tokens
		switch ch {
		case '(':
			tokens = append(tokens, token{tokenLParen, "("})
			i++
			continue
		case ')':
			tokens = append(tokens, token{tokenRParen, ")"})
			i++
			continue
		case '[':
			tokens = append(tokens, token{tokenLBracket, "["})
			i++
			continue
		case ']':
			tokens = append(tokens, token{tokenRBracket, "]"})
			i++
			continue
		case ',':
			tokens = append(tokens, token{tokenComma, ","})
			i++
			continue
		}

		// Two-char operators
		if i+1 < len(input) {
			two := input[i : i+2]
			switch two {
			case "==", "!=", "<=", ">=", "&&", "||":
				tokens = append(tokens, token{tokenOperator, two})
				i += 2
				continue
			}
		}

		// Single-char operators
		switch ch {
		case '<', '>':
			tokens = append(tokens, token{tokenOperator, string(ch)})
			i++
			continue
		case '!':
			tokens = append(tokens, token{tokenOperator, "!"})
			i++
			continue
		}

		// String literal (single-quoted)
		if ch == '\'' {
			j := i + 1
			for j < len(input) && input[j] != '\'' {
				j++
			}
			if j >= len(input) {
				return nil, fmt.Errorf("unterminated string starting at position %d", i)
			}
			tokens = append(tokens, token{tokenString, input[i+1 : j]})
			i = j + 1
			continue
		}

		// Number
		if ch >= '0' && ch <= '9' {
			j := i
			for j < len(input) && (input[j] >= '0' && input[j] <= '9') {
				j++
			}
			if j < len(input) && input[j] == '.' {
				j++
				for j < len(input) && (input[j] >= '0' && input[j] <= '9') {
					j++
				}
			}
			tokens = append(tokens, token{tokenNumber, input[i:j]})
			i = j
			continue
		}

		// Negative number
		if ch == '-' && i+1 < len(input) && input[i+1] >= '0' && input[i+1] <= '9' {
			j := i + 1
			for j < len(input) && (input[j] >= '0' && input[j] <= '9') {
				j++
			}
			if j < len(input) && input[j] == '.' {
				j++
				for j < len(input) && (input[j] >= '0' && input[j] <= '9') {
					j++
				}
			}
			tokens = append(tokens, token{tokenNumber, input[i:j]})
			i = j
			continue
		}

		// Identifier or keyword (letters, digits, underscores, dots)
		if ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') {
			j := i
			for j < len(input) {
				c := rune(input[j])
				if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' || c == '.' {
					j++
				} else {
					break
				}
			}
			word := input[i:j]
			switch word {
			case "true", "false":
				tokens = append(tokens, token{tokenBool, word})
			case "in":
				tokens = append(tokens, token{tokenIn, word})
			default:
				tokens = append(tokens, token{tokenIdent, word})
			}
			i = j
			continue
		}

		return nil, fmt.Errorf("unexpected character %q at position %d", ch, i)
	}

	tokens = append(tokens, token{tokenEOF, ""})
	return tokens, nil
}

// --- AST nodes ---

type node interface {
	nodeMarker()
}

type literalNode struct {
	value any // float64, string, bool
}

type identNode struct {
	name string
}

type arrayLiteralNode struct {
	elements []node
}

type unaryNode struct {
	op      string
	operand node
}

type binaryNode struct {
	op    string
	left  node
	right node
}

func (literalNode) nodeMarker()      {}
func (identNode) nodeMarker()        {}
func (arrayLiteralNode) nodeMarker() {}
func (unaryNode) nodeMarker()        {}
func (binaryNode) nodeMarker()       {}

// --- Parser ---

type parser struct {
	tokens []token
	pos    int
}

func (p *parser) peek() token {
	if p.pos >= len(p.tokens) {
		return token{tokenEOF, ""}
	}
	return p.tokens[p.pos]
}

func (p *parser) advance() token {
	t := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return t
}

func (p *parser) expect(kind tokenKind) (token, error) {
	t := p.advance()
	if t.kind != kind {
		return t, fmt.Errorf("expected token kind %d, got %q", kind, t.value)
	}
	return t, nil
}

// parseExpression is the entry point: expression = logicalOr
func (p *parser) parseExpression() (node, error) {
	return p.parseLogicalOr()
}

// logicalOr = logicalAnd ( "||" logicalAnd )*
func (p *parser) parseLogicalOr() (node, error) {
	left, err := p.parseLogicalAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokenOperator && p.peek().value == "||" {
		p.advance()
		right, err := p.parseLogicalAnd()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: "||", left: left, right: right}
	}
	return left, nil
}

// logicalAnd = comparison ( "&&" comparison )*
func (p *parser) parseLogicalAnd() (node, error) {
	left, err := p.parseComparison()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tokenOperator && p.peek().value == "&&" {
		p.advance()
		right, err := p.parseComparison()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: "&&", left: left, right: right}
	}
	return left, nil
}

// comparison = inExpr ( ("==" | "!=" | "<" | ">" | "<=" | ">=") inExpr )?
func (p *parser) parseComparison() (node, error) {
	left, err := p.parseInExpr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind == tokenOperator {
		op := p.peek().value
		if op == "==" || op == "!=" || op == "<" || op == ">" || op == "<=" || op == ">=" {
			p.advance()
			right, err := p.parseInExpr()
			if err != nil {
				return nil, err
			}
			left = binaryNode{op: op, left: left, right: right}
		}
	}
	return left, nil
}

// inExpr = unary ( "in" primary )?
func (p *parser) parseInExpr() (node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	if p.peek().kind == tokenIn {
		p.advance()
		right, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		left = binaryNode{op: "in", left: left, right: right}
	}
	return left, nil
}

// unary = "!" unary | primary
func (p *parser) parseUnary() (node, error) {
	if p.peek().kind == tokenOperator && p.peek().value == "!" {
		p.advance()
		operand, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return unaryNode{op: "!", operand: operand}, nil
	}
	return p.parsePrimary()
}

// primary = "(" expression ")" | arrayLiteral | literal | identifier
func (p *parser) parsePrimary() (node, error) {
	t := p.peek()

	switch t.kind {
	case tokenLParen:
		p.advance()
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(tokenRParen); err != nil {
			return nil, fmt.Errorf("missing closing parenthesis")
		}
		return expr, nil

	case tokenLBracket:
		return p.parseArrayLiteral()

	case tokenNumber:
		p.advance()
		f, err := strconv.ParseFloat(t.value, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number %q: %w", t.value, err)
		}
		return literalNode{value: f}, nil

	case tokenString:
		p.advance()
		return literalNode{value: t.value}, nil

	case tokenBool:
		p.advance()
		return literalNode{value: t.value == "true"}, nil

	case tokenIdent:
		p.advance()
		return identNode{name: t.value}, nil

	default:
		return nil, fmt.Errorf("unexpected token %q", t.value)
	}
}

// arrayLiteral = "[" ( expression ( "," expression )* )? "]"
func (p *parser) parseArrayLiteral() (node, error) {
	p.advance() // consume '['
	var elements []node
	if p.peek().kind != tokenRBracket {
		elem, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		elements = append(elements, elem)
		for p.peek().kind == tokenComma {
			p.advance()
			elem, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			elements = append(elements, elem)
		}
	}
	if _, err := p.expect(tokenRBracket); err != nil {
		return nil, fmt.Errorf("missing closing bracket")
	}
	return arrayLiteralNode{elements: elements}, nil
}

// --- Evaluator ---

func eval(n node, ctx map[string]any) (any, error) {
	switch n := n.(type) {
	case literalNode:
		return n.value, nil

	case identNode:
		return resolveField(n.name, ctx)

	case arrayLiteralNode:
		var result []any
		for _, elem := range n.elements {
			v, err := eval(elem, ctx)
			if err != nil {
				return nil, err
			}
			result = append(result, v)
		}
		return result, nil

	case unaryNode:
		operand, err := eval(n.operand, ctx)
		if err != nil {
			return nil, err
		}
		b, ok := operand.(bool)
		if !ok {
			return nil, fmt.Errorf("operator ! requires bool operand, got %T", operand)
		}
		return !b, nil

	case binaryNode:
		return evalBinary(n, ctx)

	default:
		return nil, fmt.Errorf("unknown node type %T", n)
	}
}

func evalBinary(n binaryNode, ctx map[string]any) (any, error) {
	// Short-circuit for boolean operators
	if n.op == "&&" {
		left, err := eval(n.left, ctx)
		if err != nil {
			return nil, err
		}
		lb, ok := left.(bool)
		if !ok {
			return nil, fmt.Errorf("operator && requires bool operands, got %T", left)
		}
		if !lb {
			return false, nil // short-circuit
		}
		right, err := eval(n.right, ctx)
		if err != nil {
			return nil, err
		}
		rb, ok := right.(bool)
		if !ok {
			return nil, fmt.Errorf("operator && requires bool operands, got %T", right)
		}
		return rb, nil
	}

	if n.op == "||" {
		left, err := eval(n.left, ctx)
		if err != nil {
			return nil, err
		}
		lb, ok := left.(bool)
		if !ok {
			return nil, fmt.Errorf("operator || requires bool operands, got %T", left)
		}
		if lb {
			return true, nil // short-circuit
		}
		right, err := eval(n.right, ctx)
		if err != nil {
			return nil, err
		}
		rb, ok := right.(bool)
		if !ok {
			return nil, fmt.Errorf("operator || requires bool operands, got %T", right)
		}
		return rb, nil
	}

	// Evaluate both sides for comparison and `in`
	left, err := eval(n.left, ctx)
	if err != nil {
		return nil, err
	}
	right, err := eval(n.right, ctx)
	if err != nil {
		return nil, err
	}

	if n.op == "in" {
		arr, ok := right.([]any)
		if !ok {
			return nil, fmt.Errorf("operator 'in' requires array on right side, got %T", right)
		}
		for _, elem := range arr {
			eq, err := compareEqual(left, elem)
			if err != nil {
				continue // skip incompatible types in array
			}
			if eq {
				return true, nil
			}
		}
		return false, nil
	}

	// Numeric coercion: int → float64
	left = coerceNumeric(left)
	right = coerceNumeric(right)

	// Comparison operators
	switch l := left.(type) {
	case float64:
		r, ok := right.(float64)
		if !ok {
			return nil, fmt.Errorf("cannot compare float64 with %T", right)
		}
		return compareFloat(l, r, n.op)

	case string:
		r, ok := right.(string)
		if !ok {
			return nil, fmt.Errorf("cannot compare string with %T", right)
		}
		return compareString(l, r, n.op)

	case bool:
		r, ok := right.(bool)
		if !ok {
			return nil, fmt.Errorf("cannot compare bool with %T", right)
		}
		switch n.op {
		case "==":
			return l == r, nil
		case "!=":
			return l != r, nil
		default:
			return nil, fmt.Errorf("operator %s not supported for bool", n.op)
		}

	default:
		return nil, fmt.Errorf("cannot compare type %T", left)
	}
}

func coerceNumeric(v any) any {
	switch v := v.(type) {
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	default:
		return v
	}
}

func compareEqual(a, b any) (bool, error) {
	a = coerceNumeric(a)
	b = coerceNumeric(b)
	switch av := a.(type) {
	case float64:
		bv, ok := b.(float64)
		if !ok {
			return false, fmt.Errorf("type mismatch")
		}
		return av == bv, nil
	case string:
		bv, ok := b.(string)
		if !ok {
			return false, fmt.Errorf("type mismatch")
		}
		return av == bv, nil
	case bool:
		bv, ok := b.(bool)
		if !ok {
			return false, fmt.Errorf("type mismatch")
		}
		return av == bv, nil
	default:
		return false, fmt.Errorf("unsupported type %T", a)
	}
}

func compareFloat(a, b float64, op string) (bool, error) {
	switch op {
	case "==":
		return a == b, nil
	case "!=":
		return a != b, nil
	case "<":
		return a < b, nil
	case ">":
		return a > b, nil
	case "<=":
		return a <= b, nil
	case ">=":
		return a >= b, nil
	default:
		return false, fmt.Errorf("unknown operator %s", op)
	}
}

func compareString(a, b string, op string) (bool, error) {
	switch op {
	case "==":
		return a == b, nil
	case "!=":
		return a != b, nil
	case "<":
		return a < b, nil
	case ">":
		return a > b, nil
	case "<=":
		return a <= b, nil
	case ">=":
		return a >= b, nil
	default:
		return false, fmt.Errorf("unknown operator %s", op)
	}
}

// resolveField splits an identifier on "." and traverses nested maps.
func resolveField(name string, ctx map[string]any) (any, error) {
	parts := strings.Split(name, ".")
	var current any = ctx
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("field %q: cannot traverse into %T", name, current)
		}
		val, exists := m[part]
		if !exists {
			return nil, fmt.Errorf("unknown field %q", name)
		}
		current = val
	}
	return current, nil
}
