package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenize(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect []token
	}{
		{
			name:  "simple comparison",
			input: "price.amount < 500",
			expect: []token{
				{tokenIdent, "price.amount"},
				{tokenOperator, "<"},
				{tokenNumber, "500"},
				{tokenEOF, ""},
			},
		},
		{
			name:  "boolean combo",
			input: "stops == 0 && cabinClass == 'economy'",
			expect: []token{
				{tokenIdent, "stops"},
				{tokenOperator, "=="},
				{tokenNumber, "0"},
				{tokenOperator, "&&"},
				{tokenIdent, "cabinClass"},
				{tokenOperator, "=="},
				{tokenString, "economy"},
				{tokenEOF, ""},
			},
		},
		{
			name:  "membership",
			input: "carrier in ['AA', 'UA']",
			expect: []token{
				{tokenIdent, "carrier"},
				{tokenIn, "in"},
				{tokenLBracket, "["},
				{tokenString, "AA"},
				{tokenComma, ","},
				{tokenString, "UA"},
				{tokenRBracket, "]"},
				{tokenEOF, ""},
			},
		},
		{
			name:  "unary not",
			input: "!active",
			expect: []token{
				{tokenOperator, "!"},
				{tokenIdent, "active"},
				{tokenEOF, ""},
			},
		},
		{
			name:  "parenthesized",
			input: "(a || b) && c",
			expect: []token{
				{tokenLParen, "("},
				{tokenIdent, "a"},
				{tokenOperator, "||"},
				{tokenIdent, "b"},
				{tokenRParen, ")"},
				{tokenOperator, "&&"},
				{tokenIdent, "c"},
				{tokenEOF, ""},
			},
		},
		{
			name:  "bare true",
			input: "true",
			expect: []token{
				{tokenBool, "true"},
				{tokenEOF, ""},
			},
		},
		{
			name:  "bare false",
			input: "false",
			expect: []token{
				{tokenBool, "false"},
				{tokenEOF, ""},
			},
		},
		{
			name:  "empty input",
			input: "",
			expect: []token{
				{tokenEOF, ""},
			},
		},
		{
			name:  "all comparison operators",
			input: "a == b != c < d > e <= f >= g",
			expect: []token{
				{tokenIdent, "a"},
				{tokenOperator, "=="},
				{tokenIdent, "b"},
				{tokenOperator, "!="},
				{tokenIdent, "c"},
				{tokenOperator, "<"},
				{tokenIdent, "d"},
				{tokenOperator, ">"},
				{tokenIdent, "e"},
				{tokenOperator, "<="},
				{tokenIdent, "f"},
				{tokenOperator, ">="},
				{tokenIdent, "g"},
				{tokenEOF, ""},
			},
		},
		{
			name:  "float number",
			input: "price == 3.14",
			expect: []token{
				{tokenIdent, "price"},
				{tokenOperator, "=="},
				{tokenNumber, "3.14"},
				{tokenEOF, ""},
			},
		},
		{
			name:  "negative number",
			input: "temp > -10",
			expect: []token{
				{tokenIdent, "temp"},
				{tokenOperator, ">"},
				{tokenNumber, "-10"},
				{tokenEOF, ""},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokens, err := tokenize(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expect, tokens)
		})
	}
}

func TestTokenize_Errors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "unterminated string",
			input:   "name == 'hello",
			wantErr: "unterminated string",
		},
		{
			name:    "unknown character",
			input:   "a @ b",
			wantErr: "unexpected character",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tokenize(tt.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestEvalPredicate(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		context map[string]any
		want    bool
	}{
		// Comparison operators with numeric values
		{
			name:    "equal numeric",
			expr:    "stops == 0",
			context: map[string]any{"stops": float64(0)},
			want:    true,
		},
		{
			name:    "not equal numeric",
			expr:    "stops != 1",
			context: map[string]any{"stops": float64(0)},
			want:    true,
		},
		{
			name:    "less than",
			expr:    "price < 500",
			context: map[string]any{"price": float64(300)},
			want:    true,
		},
		{
			name:    "greater than",
			expr:    "price > 500",
			context: map[string]any{"price": float64(700)},
			want:    true,
		},
		{
			name:    "less than or equal",
			expr:    "price <= 500",
			context: map[string]any{"price": float64(500)},
			want:    true,
		},
		{
			name:    "greater than or equal",
			expr:    "price >= 500",
			context: map[string]any{"price": float64(500)},
			want:    true,
		},
		{
			name:    "less than false",
			expr:    "price < 500",
			context: map[string]any{"price": float64(600)},
			want:    false,
		},

		// String equality/inequality
		{
			name:    "string equal",
			expr:    "carrier == 'AA'",
			context: map[string]any{"carrier": "AA"},
			want:    true,
		},
		{
			name:    "string not equal",
			expr:    "carrier != 'UA'",
			context: map[string]any{"carrier": "AA"},
			want:    true,
		},
		{
			name:    "string less than",
			expr:    "carrier < 'BB'",
			context: map[string]any{"carrier": "AA"},
			want:    true,
		},

		// Boolean operators with short-circuit
		{
			name:    "and true",
			expr:    "stops == 0 && cabinClass == 'economy'",
			context: map[string]any{"stops": float64(0), "cabinClass": "economy"},
			want:    true,
		},
		{
			name:    "and false short-circuit",
			expr:    "false && missing",
			context: map[string]any{},
			want:    false,
		},
		{
			name:    "or true short-circuit",
			expr:    "true || missing",
			context: map[string]any{},
			want:    true,
		},
		{
			name:    "or both false",
			expr:    "active || premium",
			context: map[string]any{"active": false, "premium": false},
			want:    false,
		},

		// Unary negation
		{
			name:    "not active",
			expr:    "!active",
			context: map[string]any{"active": false},
			want:    true,
		},
		{
			name:    "not grouped expression",
			expr:    "!(stops > 0)",
			context: map[string]any{"stops": float64(0)},
			want:    true,
		},

		// `in` with array literals
		{
			name:    "in array true",
			expr:    "carrier in ['AA', 'UA']",
			context: map[string]any{"carrier": "AA"},
			want:    true,
		},
		{
			name:    "in array false",
			expr:    "carrier in ['AA', 'UA']",
			context: map[string]any{"carrier": "DL"},
			want:    false,
		},
		{
			name:    "in numeric array",
			expr:    "status in [200, 201]",
			context: map[string]any{"status": float64(200)},
			want:    true,
		},

		// Dot-notation field access
		{
			name: "dot notation",
			expr: "price.amount < 500",
			context: map[string]any{
				"price": map[string]any{
					"amount": float64(300),
				},
			},
			want: true,
		},
		{
			name: "deep nesting",
			expr: "a.b.c == 'deep'",
			context: map[string]any{
				"a": map[string]any{
					"b": map[string]any{
						"c": "deep",
					},
				},
			},
			want: true,
		},

		// `value` is just a regular identifier
		{
			name:    "value identifier",
			expr:    "value == 42",
			context: map[string]any{"value": float64(42)},
			want:    true,
		},

		// Parenthesized grouping
		{
			name:    "parens override precedence",
			expr:    "(a || b) && c",
			context: map[string]any{"a": false, "b": true, "c": true},
			want:    true,
		},
		{
			name:    "parens change result",
			expr:    "(a || b) && c",
			context: map[string]any{"a": false, "b": true, "c": false},
			want:    false,
		},

		// Int/float coercion
		{
			name:    "int coercion to float",
			expr:    "stops == 0",
			context: map[string]any{"stops": int(0)},
			want:    true,
		},
		{
			name:    "int64 coercion",
			expr:    "count > 5",
			context: map[string]any{"count": int64(10)},
			want:    true,
		},

		// Bool comparison
		{
			name:    "bool equal",
			expr:    "active == true",
			context: map[string]any{"active": true},
			want:    true,
		},
		{
			name:    "bool not equal",
			expr:    "active != false",
			context: map[string]any{"active": true},
			want:    true,
		},

		// Complex expressions
		{
			name:    "complex and/or",
			expr:    "stops == 0 && (carrier == 'AA' || carrier == 'UA')",
			context: map[string]any{"stops": float64(0), "carrier": "UA"},
			want:    true,
		},
		{
			name: "nested dot with comparison",
			expr: "price.amount <= 1000 && price.currency == 'USD'",
			context: map[string]any{
				"price": map[string]any{
					"amount":   float64(500),
					"currency": "USD",
				},
			},
			want: true,
		},

		// Bare boolean literals
		{
			name:    "bare true",
			expr:    "true",
			context: map[string]any{},
			want:    true,
		},
		{
			name:    "bare false",
			expr:    "false",
			context: map[string]any{},
			want:    false,
		},

		// Negative numbers
		{
			name:    "negative number comparison",
			expr:    "temp > -10",
			context: map[string]any{"temp": float64(5)},
			want:    true,
		},

		// Empty array for in
		{
			name:    "in empty array",
			expr:    "x in []",
			context: map[string]any{"x": "anything"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EvalPredicate(tt.expr, tt.context)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEvalPredicate_Errors(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		context map[string]any
		wantErr string
	}{
		{
			name:    "unknown field",
			expr:    "missing == 1",
			context: map[string]any{},
			wantErr: "unknown field",
		},
		{
			name:    "type mismatch comparison",
			expr:    "x == 'hello'",
			context: map[string]any{"x": float64(1)},
			wantErr: "cannot compare",
		},
		{
			name:    "non-bool result",
			expr:    "x",
			context: map[string]any{"x": float64(42)},
			wantErr: "not bool",
		},
		{
			name:    "non-array in RHS",
			expr:    "x in y",
			context: map[string]any{"x": "a", "y": "not-array"},
			wantErr: "requires array",
		},
		{
			name:    "parse error - unterminated string",
			expr:    "x == 'hello",
			context: map[string]any{},
			wantErr: "unterminated string",
		},
		{
			name:    "parse error - unexpected token",
			expr:    "==",
			context: map[string]any{},
			wantErr: "unexpected token",
		},
		{
			name:    "not operator on non-bool",
			expr:    "!x",
			context: map[string]any{"x": float64(5)},
			wantErr: "requires bool",
		},
		{
			name:    "and on non-bool",
			expr:    "x && true",
			context: map[string]any{"x": float64(5)},
			wantErr: "requires bool",
		},
		{
			name:    "or on non-bool",
			expr:    "x || false",
			context: map[string]any{"x": float64(5)},
			wantErr: "requires bool",
		},
		{
			name:    "ordering on bool",
			expr:    "x < true",
			context: map[string]any{"x": true},
			wantErr: "not supported for bool",
		},
		{
			name:    "dot traverse non-map",
			expr:    "a.b == 1",
			context: map[string]any{"a": "string-not-map"},
			wantErr: "cannot traverse",
		},
		{
			name:    "missing closing paren",
			expr:    "(x == 1",
			context: map[string]any{"x": float64(1)},
			wantErr: "missing closing parenthesis",
		},
		{
			name:    "trailing garbage",
			expr:    "true false",
			context: map[string]any{},
			wantErr: "unexpected token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := EvalPredicate(tt.expr, tt.context)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidatePredicate(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantErr bool
	}{
		{"valid simple", "x == 1", false},
		{"valid complex", "stops == 0 && carrier in ['AA', 'UA']", false},
		{"valid negation", "!active", false},
		{"valid parens", "(a || b) && c", false},
		{"invalid unterminated string", "x == 'hello", true},
		{"invalid unknown char", "x @ y", true},
		{"invalid trailing token", "true false", true},
		{"invalid empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePredicate(tt.expr)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
