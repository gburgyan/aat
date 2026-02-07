# Task 6: Sequential Plan Runner — Design Document

## Overview

Task 6 connects the foundation (graph schema, adapters, templates) into a working execution engine. Given a plan YAML describing steps and input values, execute them in dependency order against APIs, passing data downstream.

## Sub-Tasks

- **6.1 Plan Model + YAML Parsing** (`plan` package) — Full plan schema as Go types, parse, validate against graph
- **6.2 Sequential Runner + Value Resolution** (`engine` package) — Engine.Run(), TopologicalSort (Kahn's), ResolveInputs
- **6.3 Auth + Config Loading** (`config` package) — Settings JSON, OAuth2 ROPC token exchange
- **6.4 Cleanup Stack** (`engine` package) — FILO cleanup execution integrated into Run()

## Key Decisions

- **StepValue custom UnmarshalYAML**: Bare scalars set Default only. Mappings unmarshal full struct. Uses yaml.Node.Kind == yaml.ScalarNode.
- **Constructor injection for Engine**: NewEngine(graph, registry, executor, config). go-ctxdep introduced at CLI wiring (Task 12).
- **TopologicalSort via Kahn's algorithm**: BFS, detects cycles, extensible to parallel execution later.
- **ResolveInputs priority**: graph edge > SELECT edge > plan StepValue.Default > graph node Input.Default > optional skip > error.
- **SELECT edge (first only)**: Marshal first array element, extract field via gjson. Full selection strategies in Task 7.
- **Fail on non-2xx**: Simplistic for Task 6. Error taxonomy in Task 8.
- **Config package stays leaf**: Assembly of EnvironmentConfig happens at call site.
- **Cleanup stack FILO**: Last pushed, first executed. Errors recorded as warnings, not propagated.
