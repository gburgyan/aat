# Visualizers

Visualizers are standalone HTML plugins that render API response data in the web UI. They turn complex, reference-heavy JSON responses into readable visual displays — tables, summaries, or any custom layout that makes the data understandable at a glance.

## Why Visualizers

Many APIs return responses with deeply nested reference structures: a flight search response might contain product offerings that reference catalog entries, which reference price details, which reference fare components. Reading this as raw JSON requires mentally resolving dozens of cross-references. A visualizer does that work for you, presenting the data in a format that shows what matters — prices, carriers, times, availability — without the structural noise.

## Project Setup

Add a `visualizers` field to your project manifest pointing to a directory:

```yaml
# aat-project.yaml
name: my-api
graph: graph.yaml
templates: templates/
visualizers: visualizers/
```

The directory contains a manifest file (`visualizers.yaml`) and the HTML files it references:

```
visualizers/
  visualizers.yaml          # manifest: declares plugins and match rules
  flight-search.html        # visualizer HTML file
  reservation-detail.html   # another visualizer
```

## Manifest Format

The `visualizers.yaml` file declares which visualizers exist and when they apply:

```yaml
visualizers:
  - id: flight-search
    name: Flight Search Results
    file: flight-search.html
    match:
      bodyContains: CatalogProductOfferingsResponse

  - id: reservation
    name: Reservation Detail
    file: reservation.html
    match:
      node: CreateReservation
```

### Fields

| Field | Required | Description |
|-------|----------|-------------|
| `id` | yes | Unique identifier for the visualizer |
| `name` | no | Display name (defaults to `id` if omitted) |
| `file` | yes | Path to the HTML file, relative to the visualizers directory |
| `match.bodyContains` | no* | Top-level JSON key that must exist in the response body |
| `match.node` | no* | Graph node name that the step must match |

*At least one match criterion (`bodyContains` or `node`) is required. When both are specified, both must match (AND logic).

### Match Rules

- **`bodyContains`** — checks whether the response body has a top-level JSON key with the given name. This is the most common match rule: API responses typically wrap their payload under a known root key.
- **`node`** — matches the graph node name of the step. Use this when the response structure doesn't have a distinctive key but you know which operation produces it.
- **Both specified** — both conditions must be true (AND). Use this to narrow matches when multiple nodes share similar response shapes.

## Writing a Visualizer

A visualizer is a self-contained HTML file that communicates with the AAT web UI via `postMessage`. It runs inside a sandboxed iframe with no network access.

### Protocol

The communication follows a three-step handshake:

1. **Ready** — the visualizer signals it's loaded and ready to receive data
2. **Data** — the web UI sends the response body and theme CSS variables
3. **Resize** — the visualizer reports its content height so the iframe can size correctly

```
Visualizer                    Web UI
    |                            |
    |-- aat-visualizer-ready --> |
    |                            |
    | <-- aat-visualizer-data -- |
    |     { body, theme }        |
    |                            |
    |-- aat-visualizer-resize -> |
    |     { height }             |
```

### Minimal Example

```html
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<style>
  body {
    background: var(--color-bg, #0f1117);
    color: var(--color-text, #e4e4e7);
    font-family: system-ui, sans-serif;
    font-size: 14px;
    padding: 0.5rem;
  }
</style>
</head>
<body>
<div id="root"></div>
<script>
(function() {
  'use strict';

  // Listen for data from the web UI.
  window.addEventListener('message', function(e) {
    if (!e.data || typeof e.data !== 'object') return;
    if (e.data.type === 'aat-visualizer-data') {
      // Apply theme CSS variables if provided.
      if (e.data.theme) {
        var root = document.documentElement;
        for (var name in e.data.theme) {
          if (e.data.theme.hasOwnProperty(name)) {
            root.style.setProperty(name, e.data.theme[name]);
          }
        }
      }
      // Render your visualization.
      render(e.data.body);
      // Report content height for iframe sizing.
      requestAnimationFrame(function() {
        parent.postMessage({
          type: 'aat-visualizer-resize',
          height: document.documentElement.scrollHeight
        }, '*');
      });
    }
  });

  // Signal readiness.
  parent.postMessage({ type: 'aat-visualizer-ready' }, '*');

  function render(body) {
    var root = document.getElementById('root');
    // body is the parsed JSON response body.
    // Build your HTML here.
    root.innerHTML = '<pre>' + JSON.stringify(body, null, 2) + '</pre>';
  }
})();
</script>
</body>
</html>
```

### Message Types

| Direction | Type | Fields | Description |
|-----------|------|--------|-------------|
| Viz -> Parent | `aat-visualizer-ready` | — | Signals the visualizer is loaded and ready |
| Parent -> Viz | `aat-visualizer-data` | `body` (object), `theme` (object) | Sends the response JSON and theme variables |
| Viz -> Parent | `aat-visualizer-resize` | `height` (number) | Reports content height in pixels |

### Theme Variables

The `theme` object contains CSS custom property names and values that match the web UI's current theme. Common variables:

| Variable | Description |
|----------|-------------|
| `--color-bg` | Page background |
| `--color-surface` | Card/panel background |
| `--color-text` | Primary text color |
| `--color-text-secondary` | Secondary text color |
| `--color-text-muted` | Muted/disabled text |
| `--color-border` | Border color |
| `--color-primary` | Accent/primary color |
| `--color-success` | Success indicator |
| `--color-warning` | Warning indicator |

Apply these to your CSS to match the web UI's appearance. Define fallback values in your stylesheet for standalone testing.

### Security Constraints

Visualizer iframes run with strict security:

- **Sandbox** — the iframe has the `sandbox` attribute, preventing navigation, form submission, and other capabilities
- **CSP** — a Content Security Policy header blocks all network access (`connect-src 'none'`), allows inline scripts and styles, and permits HTTPS/data images
- **No external resources** — all CSS, JavaScript, and assets must be inline in the HTML file; external URLs are blocked

These constraints ensure visualizers cannot exfiltrate data or modify the host page.

## Testing

To verify your visualizer works:

1. **Start the web UI** — `aat web` with a `visualizers` field in your manifest
2. **Open a matching step** — navigate to a run that has a step matching your visualizer's criteria
3. **Check the visualizer tab** — the step detail view shows a tab for each matching visualizer

For quick iteration, you can also open the HTML file directly in a browser and post test data to it via the browser console:

```javascript
// In the browser console, after opening the HTML file:
var iframe = document.querySelector('iframe'); // or the window itself
iframe.contentWindow.postMessage({
  type: 'aat-visualizer-data',
  body: { /* your test JSON */ },
  theme: {}
}, '*');
```

If the visualizer appears but shows nothing, check:
- The `bodyContains` key matches a top-level key in the response (not a nested key)
- The `node` name matches the graph node (not the step ID)
- The JavaScript `render()` function handles the response structure correctly
- The `aat-visualizer-ready` message is sent on load

## See Also

- [Web UI and Archives](web-ui.md) — the web viewer where visualizers display
- [Project Setup](project-setup.md) — the `visualizers` manifest field

---

*Source: `config/visualizer.go`, `server/visualizer.go`, `server/visualizer_handler.go`.*
