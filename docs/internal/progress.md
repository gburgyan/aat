# AAT Progress Tracker

Cross-references task numbers from `vision/implementation-plan.md`.

## Stage 1: Foundation

- [x] 0. Project scaffolding (module, packages, CLAUDE.md, progress tracking)
- [x] 1. Define YAML graph schema with semver versioning
- [x] 2. Author graph for one real flow (Travelport air booking, 7 nodes)
- [x] 3. Implement adapter interface and HTTPExecutor
- [x] 4. Implement Tier 1 template adapter loader
- [x] 5. Write template adapters for Travelport booking flow (7 templates)
- [x] 6. Implement sequential plan runner with dependency-aware scheduler
  - [x] 6.1 Plan model + YAML parsing (`plan` package)
  - [x] 6.2 Sequential runner + value resolution (`engine` package)
  - [x] 6.3 Auth + config loading (`config` package)
  - [x] 6.4 Cleanup stack (`engine` package)
- [x] 6a. Implement predicate expression parser and evaluator
- [x] 7. Implement array selection strategies in engine
- [x] 8. Implement error taxonomy and failure handling
- [x] 9. Implement mechanical validation
- [x] 10. Implement config/environment layer
- [x] 11. Implement archive writer
- [x] 12. Wire CLI: `aat run --plan <file> --env <env>`

## Stage 2: Intelligence

- [x] 13. Curate domain knowledge layer (13 concepts, 6 types, 6 pools)
- [x] 14. Implement deterministic backward chaining
- [x] 15. Build prompt-to-plan transformer
  - [x] 15a. LLM client package (`llm/`)
  - [x] 15b. Formatters, extraction, prompt templates (`intent/`)
  - [x] 15c. Interpret pipeline + post-processing (`intent/`)
- [x] 16. Implement plan validation enhancements
- [x] 17. Implement confirmation UX (print plan, y/n/adjust)
- [x] 17a-obs. Planning pipeline observability (plan trace)
- [x] 18. Implement value resolution hierarchy (defaults → pools → LLM)
  - [x] 18a. Value expression evaluator (`plan/expr.go`)
  - [x] 18b. Enhanced resolution with constraints + fallback pools (`engine/resolve.go`)
  - [x] 18c. Execution mode + LLM value selection (`engine/llm_values.go`, CLI wiring)
  - [x] 18d. Wire LLM fallback into resolution chain + resolution/LLM call tracking in archives
- [x] 19. Add LLM-assisted value selection for arrays
  - [x] 19a. ElementField name resolution (plans use names, engine resolves to gjson paths)
  - [x] 19b. LLM-assisted element selection (`strategy: llm` with prompt)
- [x] 20. Add constraint-aware fallback with relaxation guard
  - [x] 20a. RelaxationTracker + soft constraint lookup
  - [x] 20b. Resolution-time relaxation (resolveWithFallback)
  - [x] 20c. Filter relaxation for selections
  - [x] 20d. Step-level relaxation (adaptive mode) + archive + CLI wiring
- [ ] ~~21. GopherLua~~ — deferred indefinitely (templates + Tier 3 cover use cases)
- [x] 22a. Implement negative assertions (expectFailure)
- [x] 23. OAS integration: scaffold generation, graph-OAS validation, MCP enrichment
  - [x] 23a. OAS linking (graph nodes reference OAS spec + operationId)
  - [x] 23b. Graph-OAS validation (`ValidateOAS`)
  - [x] 23c. Scaffold generation (`aat generate --oas`)
  - [x] 23d. CLI: `aat graph validate` (absorbs H3)
- [x] 24. Build prompt-to-plan regression test suite
- [x] 28. CI/CD mode (granular exit codes, --json, --quiet, early plan validation)
- [ ] 60. `aat mcp serve` — API lifecycle platform
  - [x] 60a-WI1. Core skeleton + CLI (manifest, context, server, `aat mcp serve`)
  - [x] 60a-WI2. Graph browsing tools (list, describe, trace, search)
  - [x] 60a-WI3. Template and domain knowledge tools
  - [x] 60a-WI4. OAS tools (operation details, schema resolution, search, validate, example gen)
  - [x] 60a-WI5. Resources (graph, templates, domain, metadata, node/{name}, template/{adapter}) + Prompts (explain_workflow, generate_client_code)
  - [x] 60a. Core MCP server + API knowledge tools (graph, templates, domain)
  - [x] 60b. Documentation integration (per-node Markdown, links to OAS, expose workflows, etc.)
  - [x] 60c. Testing lifecycle tools (can we make a client integration using the MCP server as an aid, then test the system using aat)
    - [x] 60c-WI7. Plan CRUD + generation tools (generate, validate, list, load, save)
    - [x] 60c-WI8. Execution + archive inspection tools (execute_plan, list/inspect/analyze/diff archives)
    - [x] 60c-WI9. Developer workflow prompts (integration_guide, test_workflow, debug_failing_test)
- [x] 59. `aat docs generate` — Markdown + Mermaid documentation from graph definitions
- [x] H1. Pre-release hardening (ctx cancellation, schema stub, archive redaction, CLAUDE.md)
  - [x] H1d. CLAUDE.md accuracy (Go 1.24, DI example, builder pattern)
  - [x] H1b. Schema validation Skipped status
  - [x] H1c. Archive secret redaction (CollectSecrets, RedactValue, ToArchive threading)
  - [x] H1a. Context cancellation checks (engine, retry, cleanup, resolve, LLM)
- [x] H2. Quickstart example (PetStore)
  - [x] H2a. Core example files (graph, templates, env, plan)
  - [x] H2b. Extended example files (OAS spec, second plan, domain knowledge)
  - [x] H2c. Documentation (petstore README tutorial, project root README)
  - [x] H2d. Housekeeping (progress, worklog)
- [x] H3. `aat graph validate` (absorbed by Task 23d)
- [x] CT. Composable Templates & AI Sweet Spot
  - [x] CT-A. Graph schema: Workflow.Kind, Workflow.Includes, WorkflowInclude type
  - [x] CT-B. Composition algorithm: ComposeWorkflowTemplate, auto-wire, prefix, splice
  - [x] CT-C. aat prompt integration: addon detection, dynamic composition in Interpret()
  - [x] CT-D. MCP tools: list_workflows, instantiate_workflow, scaffold_template
  - [x] CT-E. Travelport examples: addon markers, 3 composed workflows

## Stage 3a: CI/CD, Web UI & Polish

- [ ] 60d. MCP CI/CD tools + production monitoring (synthetic monitors)
- [ ] 10b. Initialize SQLite for local run history indexing
- [ ] 22. Add semantic validation via LLM (moved from Stage 2 — mechanical validation sufficient for launch)
- [ ] 25. Build the local API server
- [ ] 26. Build the local web UI frontend
- [ ] 27. Implement `aat serve` command
- [ ] 29. Implement Markdown report generation
- [ ] 30. Implement `aat inspect <archive>`
- [ ] 31. Implement `aat diff <archive1> <archive2>`
- [ ] 32. Implement plan persistence (`aat plan save/list/validate`)
- [ ] 33. Implement FILO cleanup stack enhancements
- [ ] 34. Implement Tier 3 external adapter protocol
- [ ] 35. Document repo structure for team usage
- [ ] 36. Implement verification steps
- [ ] 37. (Optional) Implement opt-in telemetry

## Stage 3b: Team Tier Infrastructure

Tasks 38-45 — expand when Stage 3a nears completion.

## Stage 4: Marketplace and Scale

Tasks 46-67 (excluding 59, 60 moved to Stage 2) — expand when Stage 3b nears completion.
