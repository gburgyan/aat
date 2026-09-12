# Roadmap

## Status

AAT's first public release is v0.1.0. It was built and proven against a 74-node
airline API, so the core loop — graph, templates, plans, engine, archives, web UI, MCP server — has
carried real traffic. It is maintained by one person.

The graph and plan YAML formats may still change before 1.0. Breaking changes will be listed in
`CHANGELOG.md` with migration notes.

## Next

Roughly in priority order. None of these have dates.

- **Resume from checkpoint.** Restart a failed or aborted run from its last checkpoint instead of
  from the first step.
- **On-demand web assets.** A binary from `go install github.com/gburgyan/aat/cmd/aat@latest` should
  be able to serve the web UI instead of exiting with an install hint.
- **More auth flows.** Client-credentials without dummy username/password fields; HTTP basic auth.
- **Request pacing.** An environment setting for a minimum delay or maximum rate between requests,
  for APIs that throttle.
- **`llms.txt`.** A machine-readable index of the docs so external LLM tools can find the right page.
- **CLI reference page.** One generated page listing every command and flag.
- **Integration kits from the manifest.** A command that packages a kit from its manifest instead of a
  copy list, and a way to keep internal-only operations and workflows out of a kit that shares the
  graph. See [Share Your API with Integrators](docs/user/integration-kit.md).
- **An MCP oracle for client code.** A tool that renders the concrete request for an operation from
  input values, and `execute_plan` results that include the exchanges.
- **More example integrations.** Duffel flight booking in test mode, GitHub, and Stripe graphs, with
  the setup chains needed to run them end to end.
- **Docs site on Zensical.** The site is built with Material for MkDocs, which gets critical fixes
  only until 2026-11-05; its successor, Zensical, aims to build existing Material projects.

## Not planned

- **An in-tool plan generator beyond `aat prompt`.** AAT exposes primitives — graph nodes,
  templates, plan steps, assertions, overrides, checkpoints, and MCP tools — and leaves plan
  authoring to external tools such as Claude Code or another MCP client. `aat prompt` stays as the
  single-prompt convenience; it will not grow into an agent loop.

## Feedback

Questions and ideas go in GitHub Discussions; bugs go in issues. See `CONTRIBUTING.md` for how
changes are reviewed.
