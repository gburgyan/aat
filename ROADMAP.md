# Roadmap

## Status

AAT's current release is v0.4.0, which adds fuzzing (`--fuzz`, the `fuzz:` block, `--fuzz-save`), seeded replay, `poolRef` defaults, and the status an error inside a 200 stands for, on top of v0.3's gRPC support and `knownIssue` primitive; the public launch release was v0.2.0. It was built and proven against a private 74-node airline
booking API with 63 workflows, 53 recipes, and 6 environments, so the core loop — graph, templates,
plans, engine, archives, web UI, MCP server — has carried real traffic. Four further projects run
against real, public APIs: [aat-duffel](https://github.com/gburgyan/aat-duffel),
[aat-stripe](https://github.com/gburgyan/aat-stripe), and
[aat-shippo](https://github.com/gburgyan/aat-shippo) in test mode, and
[aat-qdrant](https://github.com/gburgyan/aat-qdrant) over gRPC against a local container. It is
maintained by one person.

The graph and plan YAML formats may still change before 1.0. Breaking changes will be listed in
`CHANGELOG.md` with migration notes.

## Next

Roughly in priority order. None of these have dates.

- **A run report in Markdown.** `aat run show <ref> --markdown`, so CI can put the outcome, the
  plans, and any [known issues](https://gburgyan.github.io/aat/plans/#known-issues-a-failure-with-a-deadline)
  into a job summary with one line, instead of each project piping terminal output into a fenced
  block or parsing archives with `jq`. A batch rollup of the entries it applied belongs with it:
  today they are visible per step in the stream but are not gathered at the end of a batch.
- **Resume from checkpoint.** Restart a failed or aborted run from its last checkpoint instead of
  from the first step.
- **On-demand web assets.** A binary from `go install github.com/gburgyan/aat/cmd/aat@latest` should
  be able to serve the web UI instead of exiting with an install hint.
- **More auth flows.** Client-credentials without dummy username/password fields; HTTP basic auth.
- **CLI reference page.** One generated page listing every command and flag.
- **Integration kits from the manifest.** A command that packages a kit from its manifest instead of a
  copy list, and a way to keep internal-only operations and workflows out of a kit that shares the
  graph. See [Share Your API with Integrators](docs/user/integration-kit.md).
- **An MCP oracle for client code.** A tool that renders the concrete request for an operation from
  input values, and `execute_plan` results that include the exchanges.
- **More example integrations.** Duffel, Stripe, and Shippo have shipped as sister repositories. Still
  wanted: an API with no sandbox, where cleanup matters and rate limits bite, and nightly CI for the
  projects that exist, so their published numbers are reproducible from the repository.
- **More of gRPC.** Unary gRPC is done: every surface reads it, it is tested over TLS and mutual TLS,
  an offline demo exercises it in CI, and [aat-qdrant](https://github.com/gburgyan/aat-qdrant) drives a
  real API's 52 methods with it on every push. What is left is more of it: `aat generate` does not
  scaffold a graph from a descriptor set, `repeat.next` follows a string or integer cursor but not one
  that is a message, the MCP server cannot browse a descriptor set as it can an OpenAPI spec, and
  server-streaming, as a bounded collect, would come after those.
- **Docs site on Zensical.** The site is built with Material for MkDocs, which gets critical fixes
  only until 2026-11-05; its successor, Zensical, aims to build existing Material projects.

## Not planned

- **Bidirectional streaming gRPC.** A step is a declarative function of its resolved inputs, and in a
  bidirectional stream the next message depends on the previous reply — that is a program, not a
  step. Supporting it would mean a second execution model rather than a longer version of this one.
  The validator rejects it by name. For a flow that genuinely needs one, `--stop-after` with
  `--dump-state` hands a live run to a tool that can.
- **An in-tool plan generator beyond `aat prompt`.** AAT exposes primitives — graph nodes,
  templates, plan steps, assertions, overrides, checkpoints, and MCP tools — and leaves plan
  authoring to external tools such as Claude Code or another MCP client. `aat prompt` stays as the
  single-prompt convenience; it will not grow into an agent loop.

## Feedback

Questions and ideas go in GitHub Discussions; bugs go in issues. See `CONTRIBUTING.md` for how
changes are reviewed.
