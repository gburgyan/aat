# Predicate Expressions

The `internal/predicate` package is a self-contained predicate expression parser and evaluator. Predicates are boolean expressions evaluated against a `map[string]any` context — used for array filtering, constraint checking, mechanical assertions, and cleanup `when` conditions. It is a foundation package with no aat imports, so `graph` can check a cleanup pairing's `when` while it validates a graph.

## API

```go
// Parse + evaluate. Returns true/false or error.
predicate.Eval(expr string, context map[string]any) (bool, error)

// Parse once, evaluate against many contexts.
p, err := predicate.Parse(expr)
ok, err := p.Eval(context)

// Parse-only syntax check. No context needed.
predicate.Validate(expr string) error

// The field names an expression reads, and its quoted string literals.
predicate.Fields(expr string) []string
predicate.Literals(expr string) ([]string, error)

// Evaluate after replacing each string literal expand returns a value for.
predicate.EvalExpanding(expr string, context map[string]any, expand func(literal string) (any, bool, error)) (bool, error)
```

`plan.Validate()` and `graph.Validate()` call `Validate` for syntax checking, and the engine calls `Eval` and `Parse` at run time. `plan.EvalPredicateWithExprs` builds on `EvalExpanding` to expand `{{…}}` expressions in an assertion's quoted strings.

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
| String | `'economy'`, `"AA"` | Single- or double-quoted |
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

### Validation (compile-time)

`plan.Validate()` calls `predicate.Validate()` to check syntax for:

1. **`step.Values[name].Select.Filter`** and named selection filters — array filtering expressions
2. **`step.Values[name].Constraint`** — value constraint expressions
3. **`step.Assertions.Mechanical[].Expr`** — predicate assertions (when `Type == "predicate"`)

`graph.Validate()`, which runs whenever a graph loads, checks each cleanup pairing's `when`: it parses, and `predicate.Fields` names only outputs of the node that declares it.

### Runtime

| Consumer | How |
|----------|-----|
| Array selection `filter` strategy | `predicate.Parse(filter)` once, then `Eval(elementAsMap)` for each array element |
| Array selection `match` strategy | Same, counting every matching element |
| Constraint checking | `predicate.Eval(constraint, {"value": v, ...earlier inputs})` |
| Mechanical predicate assertions | `plan.EvalPredicateWithExprs(expr, responseOutputs, ectx)`, which expands `{{…}}` in quoted strings |
| Cleanup `when` | `predicate.Eval(when, outputsOfTheRegisteringStep)` |

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

- Source: `internal/predicate/predicate.go`; `plan/predicate_exprs.go` adds the `{{…}}` expansion
- Tests: `internal/predicate/predicate_test.go`
- No external dependencies — pure Go, no regex
- Tokenizer scans all tokens at once, parser consumes the token slice
- AST is five node types implementing a `node` interface
