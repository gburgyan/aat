# Predicate Expressions

The `plan` package includes a self-contained predicate expression parser and evaluator. Predicates are boolean expressions evaluated against a `map[string]any` context — used for array filtering (Task 7), constraint checking, and mechanical assertions (Task 9).

## API

```go
// Parse + evaluate. Returns true/false or error.
plan.EvalPredicate(expr string, context map[string]any) (bool, error)

// Parse-only syntax check. No context needed.
plan.ValidatePredicate(expr string) error
```

`ValidatePredicate` is called during `Validate()` for plan-time syntax checking. `EvalPredicate` is called at runtime by the engine.

## Grammar

Recursive descent, precedence low to high:

```
expression   = logicalOr
logicalOr    = logicalAnd ( "||" logicalAnd )*
logicalAnd   = comparison ( "&&" comparison )*
comparison   = inExpr ( ("==" | "!=" | "<" | ">" | "<=" | ">=") inExpr )?
inExpr       = unary ( "in" primary )?
unary        = "!" unary | primary
primary      = "(" expression ")" | arrayLiteral | literal | identifier
arrayLiteral = "[" ( expression ( "," expression )* )? "]"
```

- Comparison is **non-associative** — `a < b < c` is a parse error.
- `&&` and `||` are left-associative.
- `in` sits between comparison and unary in precedence.

## Token Types

| Kind | Examples | Notes |
|------|----------|-------|
| Number | `500`, `3.14`, `-10` | Parsed as float64 |
| String | `'economy'`, `'AA'` | Single-quoted only |
| Bool | `true`, `false` | Keywords |
| Ident | `carrier`, `price.amount` | Dots included in token |
| Operator | `==`, `!=`, `<`, `>`, `<=`, `>=`, `&&`, `\|\|`, `!` | |
| In | `in` | Keyword, not an operator |
| Parens | `(`, `)` | Grouping |
| Brackets | `[`, `]` | Array literals |
| Comma | `,` | Array element separator |

## Evaluation Semantics

### Field Resolution

Identifiers are resolved by splitting on `.` and traversing nested `map[string]any`:

```
"price.amount" → ctx["price"].(map[string]any)["amount"]
```

Missing keys or non-map intermediates produce an error.

### Type Coercion

YAML unmarshals integers as Go `int`, JSON as `float64`. The evaluator normalizes `int`, `int32`, and `int64` to `float64` before comparisons. No other implicit coercion — comparing a string to a number is an error.

### Comparison

- **float64**: all six operators (`==`, `!=`, `<`, `>`, `<=`, `>=`)
- **string**: all six operators (lexicographic)
- **bool**: `==` and `!=` only; ordering (`<`, `>`) is an error

### Short-Circuit

`&&` and `||` short-circuit:

- `false && <anything>` → `false` (right side not evaluated, no error even if fields are missing)
- `true || <anything>` → `true`

This is important for guards like `hasField && field.value > 0`.

### `in` Operator

LHS is a scalar, RHS must evaluate to `[]any`. Each element is compared for equality with the LHS. Incompatible types in the array are silently skipped (not errors).

```
carrier in ['AA', 'UA', 'DL']
status in [200, 201]
```

### Errors

The evaluator returns errors (never panics) for:

- **Parse errors**: unterminated string, unexpected token, missing operand
- **Unknown field**: identifier not found in context
- **Type mismatch**: comparing incompatible types (e.g., float64 vs string)
- **Non-bool operand**: `!`, `&&`, `||` applied to non-bool
- **Non-bool result**: top-level expression doesn't produce bool
- **Non-array RHS**: `in` with non-array right side

## Where Predicates Are Used

### Plan Validation (compile-time)

`Validate()` calls `ValidatePredicate()` to check syntax for:

1. **`step.Values[name].Select.Filter`** — array filtering expressions
2. **`step.Values[name].Constraint`** — value constraint expressions
3. **`step.Assertions.Mechanical[].Expr`** — predicate assertions (when `Type == "predicate"`)

### Runtime (downstream tasks)

| Consumer | Task | How |
|----------|------|-----|
| Array selection `filter` strategy | Task 7 | `EvalPredicate(filter, elementAsMap)` for each array element |
| Array selection `match` strategy | Task 7 | Same mechanism, different strategy wrapper |
| Constraint checking | Task 7+ | `EvalPredicate(constraint, {"value": v})` |
| Mechanical predicate assertions | Task 9 | `EvalPredicate(expr, responseOutputs)` |

## Example Expressions

```
# Simple comparison
price.amount < 500

# Boolean combination
stops == 0 && cabinClass == 'economy'

# Membership test
carrier in ['AA', 'UA', 'DL']

# Negation
!active

# Grouped precedence
(carrier == 'AA' || carrier == 'UA') && stops == 0

# Nested field access
response.price.currency == 'USD'
```

## Implementation Notes

- Source: `plan/predicate.go` (~290 lines)
- Tests: `plan/predicate_test.go` (~350 lines, ~55 test cases)
- No external dependencies — pure Go, no regex
- Tokenizer scans all tokens at once, parser consumes the token slice
- AST is five node types implementing a `node` interface
