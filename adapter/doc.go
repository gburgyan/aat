// Package adapter defines the adapter interface, HTTP executor, and adapter registry.
//
// An Adapter translates between the graph's logical inputs/outputs and concrete
// HTTP requests and responses. Each node in a graph is backed by one Adapter
// that knows how to build the HTTP request from resolved inputs and extract
// outputs from the HTTP response.
//
// The HTTPExecutor is responsible for actually sending requests over HTTP. It
// owns the base URL and HTTP client; adapters produce only relative paths and
// headers. This separation keeps adapters transport-agnostic and testable.
//
// The Registry provides named lookup of adapters, used by the engine to match
// graph node adapter references to their implementations.
//
// Adapters are loaded via tiered loaders (template, Lua, external) defined in
// subsequent tasks. This package defines only the core interface and executor.
package adapter
