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
- [ ] 17. Implement confirmation UX (print plan, y/n/adjust)
- [ ] 18. Implement value resolution hierarchy (defaults → pools → LLM)
- [ ] 19. Add LLM-assisted value selection for arrays
- [ ] 20. Add constraint-aware fallback with relaxation guard
- [ ] 21. Embed GopherLua with sandbox restrictions (Tier 2 adapters)
- [ ] 22. Add semantic validation via LLM
- [ ] 22a. Implement negative assertions (expectFailure)
- [ ] 23. Build basic OAS-to-adapter generator
- [ ] 24. Build prompt-to-plan regression test suite

## Stage 3a: CI/CD, Web UI & Polish

Tasks 25-37 — expand when Stage 2 nears completion.

## Stage 3b: Team Tier Infrastructure

Tasks 38-45 — expand when Stage 3a nears completion.

## Stage 4: Marketplace and Scale

Tasks 46-67 — expand when Stage 3b nears completion.
