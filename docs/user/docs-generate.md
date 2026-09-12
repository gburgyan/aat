# Generating API Documentation

`aat docs generate` renders a graph as Markdown: a Mermaid diagram of how operations depend on each other, then a section per node with its inputs, outputs, defaults, error detection, and ordering. It can write one file, or an index plus one file per node. A domain file adds example values, and hand-written notes can be merged into each node's section.

Apart from the project manifest it may use to find them, the command reads only the graph, the domain file, and the notes directory. Templates, Lua transforms, environments, and the OpenAPI spec are not part of the output.

## Usage

```bash
aat docs generate --output api.md
```

```text
Generated documentation: api.md
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--graph` | path | from manifest | Graph file to document |
| `--domain` | path | from manifest | Domain file whose concepts, types, and value pools supply example values |
| `--node-docs` | path | `docs/nodes` | Directory of hand-written `<node>.md` files to merge in |
| `--output` | path | `workflow.md` | File to write, or `-` for stdout. With `--split`, the directory to write |
| `--title` | string | the graph's `title`, else `API Workflow` | Document title |
| `--split` | bool | `false` | Write `index.md` plus one file per node instead of a single file |

When `--graph` or `--domain` is omitted, the path comes from the project manifest, found through the standard [resolution chain](project-setup.md#resolution-priority). There is no `--manifest` flag. A manifest that names a domain file adds examples without `--domain`. `--node-docs` and `--output` are relative to the working directory, not to the manifest.

Errors exit with code `2`: no graph from either a flag or a manifest (`--graph is required`), a graph or domain file that fails to load, `--split` with `--output -` (`--split is incompatible with --output "-"`), or an output path that cannot be written.

## Single-File Output

From the shop example's directory, `aat docs generate --output -` prints the whole document. Its sections, in order:

| Section | Contents |
|---------|----------|
| Title | `# <title>`, a line with the node count and the graph's `version`, and the graph's `description` |
| Workflow Diagram | A Mermaid `graph TD` diagram (details below) |
| Workflows | One heading per workflow with its description; addons also say which node they splice after |
| Entry Points | Every node with no `requires` tokens, with its description. Cleanup nodes usually appear here too |
| Nodes | One section per node, in dependency order: a node comes after the nodes that satisfy its `requires` tokens |
| Cleanup | A table of cleanup nodes and the node each one cleans up |
| Notes | The graph's `notes` |

Workflows, Entry Points, Cleanup, and Notes are left out when there is nothing to list.

The diagram has one box per node, labeled with the node name and the start of its description. Solid arrows run from a node that satisfies a token to each node that requires it; dashed arrows run from a node to its cleanup node, and cleanup nodes get a dashed red style. An excerpt from the shop:

```text
graph TD
    addItem["addItem<br/>Add a product to a cart; adding a SKU again mer..."]
    ...
    createCart --> addItem
    createCart --> applyCoupon
    checkoutCart --> cancelOrder
    listProducts --> checkInventory
    ...
    checkoutCart -.-> deleteOrder
    createCart -.-> deleteCart

    classDef cleanup fill:#fee,stroke:#c33,stroke-dasharray:5 5
```

### Node Sections

Each node section has, when there is something to show:

- The node's description and its adapter name.
- The node's hand-written notes (see [Node Notes](#node-notes)).
- **Inputs:** a table of name, type, whether it is required (`yes`, `no`, or `configurable`), default, and description. A default shows as the literal value, the first three pool values, `from: node.output`, or `fromResolved: input`; a `select` is not shown. A Constraints column appears when any input has [constraints](graphs.md#inputs), and an Examples column when any input has example values (see [Examples](#examples)).
- **Outputs:** a table of name, type, and description, with each of an array output's `elementFields` on an indented row below it.
- **Error Detection:** the node's `errorDetection` rules, or the graph-level rules marked as inherited.
- **Provides data to** and **Receives data from:** the nodes that require a token this node satisfies, and the nodes that satisfy a token this node requires. They come from `requires` and `satisfies` only, not from `from` defaults: the shop's `addItem` reads `listProducts.products`, but its section lists only `createCart` as a source.

The shop's `checkInventory` section, generated with the domain file its manifest names:

```markdown
### checkInventory

Check stock for one SKU. Answers 200 with status OK or ERROR; the first read of SKU-1004 in each session is a stale ERROR that a retry clears.

**Adapter:** `checkInventory`

**Inputs:**

| Name | Type | Required | Default | Description | Examples |
|------|------|----------|---------|-------------|----------|
| sku | string | yes | SKU-1004 | Product SKU | SKU-1001, SKU-1002, SKU-1003, SKU-1004, SKU-1005 |

**Outputs:**

| Name | Type | Description |
|------|------|-------------|
| status | string | OK, or ERROR when the read failed |
| sku | string |  |
| available | integer | Units in stock |

**Error Detection:**

| Path | Rule | Details |
|------|------|---------|
| `status` | equals ERROR | message: `errorMessage`, code: `errorCode` |

**Receives data from:** listProducts
```

## Split Output

```bash
aat docs generate --split --output docs/api
```

```text
Generated documentation: docs/api/ (18 files)
```

```text
docs/api/
  index.md
  nodes/
    addItem.md
    applyCoupon.md
    ...
    shipOrder.md
```

`index.md` has the same title, diagram, workflows, cleanup table, and notes as the single file. Its entry points link to the node files, and in place of the node sections it has a table of every node (linked), its description, and its input and output counts. Each `nodes/<node>.md` holds that node's section, starting with a level-3 heading.

The output directory and its `nodes/` subdirectory are created if needed. Existing files are overwritten, and files for nodes that are no longer in the graph are not removed.

## Examples

The Examples column lists up to five values for an input, taken from the first of these that has any:

1. The `examples` of every [domain concept](domain.md#concepts) whose `applies_to` names the input, all example groups together.
2. The [value pool](domain.md#value-pools) of the domain type named like the input's type.
3. The value pool of the domain type named like the input.
4. The values of an `enum[...]` input type.

The fourth source needs no domain file, so enum inputs show examples even without one. In the shop, `sku` gets its values from the `sku` type's pool, and `category` from its `enum[gear, apparel, footwear]` type.

Because concept examples come first and apply to every field in `applies_to`, write them to fit all of those fields. The shop's `regionalPricing` concept applies to `shippingTier`, `postalCode`, `code`, and `currency`, and its examples are shipping tiers, so the `postalCode` and `code` inputs list `standard, express, overnight, ...` even though a `postalCode` type with a pool of postal codes exists.

## Node Notes

Put a Markdown file named exactly like a node (`checkInventory.md`, matching case) in the `--node-docs` directory, `docs/nodes` by default. Its contents appear in that node's section, after the adapter line and before the inputs. Files that match no node are ignored, and a missing directory is not an error.

The MCP server's per-node documentation is read from the directory set by the manifest's `docs:` key instead (see [Per-Node Documentation](mcp-server.md#per-node-documentation)). To use the same files for both, pass that directory: `aat docs generate --node-docs docs`.

## Known Limitations

- **`--split` ignores the default output name.** Without `--output`, `--split` writes to `docs/api/`, not `workflow.md`, and says so only in its final message. Passing `--output workflow.md` explicitly does the same.
- **Single-file output does not create directories.** `--output out/api.md` fails with `writing output: open out/api.md: no such file or directory` when `out/` does not exist. Split output creates its directories.
- **Table cells are not escaped.** A `|` in a description splits the cell, and a description that spans lines breaks the table. Keep descriptions on one line; a folded `>-` scalar in the graph YAML is fine.
- **Diagram labels are not escaped.** A `"` in a node description breaks the Mermaid diagram. Labels are cut at 50 bytes, which can split a multi-byte character.
- **Example order is not stable.** When a concept has more than one example group (the shop's `us` and `eu`), the groups can come out in a different order on each run, so regenerating the docs can change them with no change to the graph or domain.

---

*Source: `cmd/aat/docs_cmd.go`, `graph/docgen.go`, `graph/mermaid.go`, `config/resolver.go`.*
