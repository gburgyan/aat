# Templates

Templates define how AAT translates graph node inputs into HTTP requests and extracts outputs from responses. Each template is a YAML file that maps to one graph node via its `adapter` name.

## Quick Start

A minimal template for a GET endpoint:

```yaml
adapter: getPet
protocol: http

request:
  method: GET
  path: /pet/{{petId}}
  headers:
    Accept: application/json

response:
  extract:
    petId: "id"
    petName: "name"
    petStatus: "status"
```

## File Structure

Templates live in a directory passed via `--templates`. AAT loads all `.yaml` and `.yml` files from this directory and registers each one by its `adapter` name. The file name doesn't matter -- only the `adapter` field links a template to a graph node.

## Schema Reference

```yaml
adapter: <name>              # required — must match a graph node's adapter field
protocol: http               # optional — defaults to "http" (only "http" is supported)

request:
  method: <HTTP method>      # required — GET, POST, PUT, DELETE, PATCH, etc.
  path: <path>               # required — URL path (appended to environment base URL)
  headers:                   # optional — request headers
    Content-Type: application/json
    X-Custom: "{{someInput}}"
  body: |                    # optional — request body (typically for POST/PUT)
    {
      "field": "{{inputName}}"
    }

response:
  extract:                   # optional — output extraction rules
    outputName: "jsonPath"
  validate:                  # optional — response validation
    schema: "schemaRef"
```

### Required Fields

| Field | Description |
|-------|-------------|
| `adapter` | Name linking this template to a graph node. Must be unique across templates. |
| `request.method` | HTTP method (GET, POST, PUT, DELETE, etc.) |
| `request.path` | URL path, appended to the environment's `apiBaseUrl` |

### Optional Fields

| Field | Default | Description |
|-------|---------|-------------|
| `protocol` | `"http"` | Protocol to use. Only `"http"` is currently supported. |
| `request.headers` | none | Key-value pairs added to the request. Supports `{{placeholder}}` substitution. |
| `request.body` | none | Request body string. Supports `{{placeholder}}` substitution. |
| `response.extract` | none | Map of output names to extraction paths. |
| `response.validate.schema` | none | Schema reference for response validation. |

## Placeholder Substitution

Templates use `{{key}}` placeholders that are resolved at execution time. Whitespace inside braces is tolerated: `{{ key }}` works the same as `{{key}}`.

### Resolution Order

1. **Step inputs** — Values resolved from the plan (literals, edge outputs, selections)
2. **Environment config values** — Values from the environment's `values` map

If a placeholder can't be resolved from either source, AAT reports an error listing all unresolved placeholders:

```
path substitution: unresolved placeholders: petId
```

### Placeholders in Different Fields

Placeholders work in `path`, `headers`, and `body`:

```yaml
request:
  method: POST
  path: /pets/{{petId}}/toys        # path parameter
  headers:
    Authorization: Bearer {{token}}  # header value
    X-Request-Id: "{{requestId}}"
  body: |                            # body fields
    {
      "name": "{{toyName}}",
      "price": {{price}}
    }
```

Values are substituted as strings using Go's `%v` formatting. For JSON bodies, string values in the body template should include quotes; numeric values should not.

## Response Extraction

The `response.extract` section defines how to pull values from a JSON response body. Each entry maps an output name (matching the graph node's output) to a path expression.

```yaml
response:
  extract:
    petId: "id"
    petName: "name"
    ownerEmail: "owner.email"
    firstTag: "tags.0.name"
    pets: "$"
```

### Path Syntax

Extraction paths use [gjson](https://github.com/tidwall/gjson) syntax with some normalization:

| Path | Extracts | Notes |
|------|----------|-------|
| `"id"` | `{"id": 42}` → `42` | Simple field |
| `"owner.email"` | `{"owner": {"email": "a@b.com"}}` → `"a@b.com"` | Nested field |
| `"tags.0.name"` | `{"tags": [{"name": "cute"}]}` → `"cute"` | Array index |
| `"$"` | entire body | Extracts the whole response as-is |

### Path Normalization

AAT normalizes paths before evaluation:

- Leading `$.` is stripped: `$.owner.email` becomes `owner.email`
- Lone `$` returns the entire response body
- Bracket notation is converted: `[0]` becomes `.0`

So `$.tags[0].name`, `tags[0].name`, and `tags.0.name` all work equivalently.

### Extraction Errors

If an extract path doesn't match anything in the response, AAT reports an error:

```
extract path "petId" (id) not found in response
```

The response body must be valid JSON for extraction to work. Non-JSON responses produce:

```
response body is not valid JSON
```

## Header Merge Order

When a request is built, headers come from two sources and are merged in this order:

1. **Environment headers** — Static headers from the environment file's `headers` section (applied first)
2. **Template headers** — Headers defined in the template (overlaid on top)

Template headers override environment headers with the same key. Authentication headers (added by the auth system) are separate and take final precedence.

This means you can set common headers like `Accept` or `Content-Type` in the environment and override them per-template when needed.

## Common Patterns

### GET with Path Parameter

```yaml
adapter: getPet
request:
  method: GET
  path: /pet/{{petId}}
  headers:
    Accept: application/json
response:
  extract:
    petId: "id"
    petName: "name"
```

### GET with Query Parameters

```yaml
adapter: findByStatus
request:
  method: GET
  path: /pet/findByStatus?status={{status}}
  headers:
    Accept: application/json
response:
  extract:
    pets: "$"
```

Query parameters are part of the path string. Use `{{placeholder}}` substitution for dynamic values.

### POST with JSON Body

```yaml
adapter: createPet
request:
  method: POST
  path: /pet
  headers:
    Content-Type: application/json
    Accept: application/json
  body: |
    {
      "name": "{{name}}",
      "status": "{{status}}",
      "photoUrls": []
    }
response:
  extract:
    petId: "id"
    petName: "name"
    petStatus: "status"
```

### DELETE with No Response Body

```yaml
adapter: deletePet
request:
  method: DELETE
  path: /pet/{{petId}}
  headers:
    Accept: application/json
response: {}
```

When there's nothing to extract, use an empty response object.

### Extracting the Entire Response (Array Endpoints)

```yaml
adapter: findByStatus
request:
  method: GET
  path: /pet/findByStatus?status={{status}}
  headers:
    Accept: application/json
response:
  extract:
    pets: "$"
```

Use `"$"` to extract the whole response body. This is typical for list endpoints where the graph node's output is an array type. The engine's selection system then picks elements from this array.

## Loading Templates

AAT loads templates from the directory specified by `--templates`:

```bash
aat run --templates templates/ ...
```

All `.yaml` and `.yml` files in the directory are parsed. Subdirectories are ignored. Each template's `adapter` field must be unique — duplicate adapter names cause a load error.

Templates are matched to graph nodes by the `adapter` field on the node:

```yaml
# graph.yaml
nodes:
  getPet:
    adapter: getPet    # ← matches template with adapter: getPet
    ...
```

## Validating Templates Against the Graph

Use `aat graph validate --templates` to check that graph-declared outputs match template extract keys:

```bash
aat graph validate --graph graph.yaml --templates templates/
```

This catches outputs declared in the graph that the template never extracts (will be nil at runtime) and extract keys in the template that the graph doesn't declare (dead extraction, likely a typo). See [Graph Authoring — Adapter output validation](graph-authoring.md#adapter-output-validation) for details.

This check also runs automatically at the start of `aat run`, so mismatches are caught before any API calls are made.

## See Also

- [Graph Authoring](graph-authoring.md) — how nodes reference templates via `adapter`
- [Environments](environments.md) — base URL, headers, and config values
- [Petstore Example](../../examples/petstore/README.md) — runnable templates in action
