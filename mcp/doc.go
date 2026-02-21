// Package mcp implements an MCP (Model Context Protocol) server that exposes
// AAT's API graph, templates, domain knowledge, OAS specs, workflows, and execution
// capabilities to IDE-based AI tools such as Claude Code.
//
// The server is started via `aat mcp serve` and communicates over stdio using
// the MCP protocol. It loads project artifacts from an aat-project.yaml manifest
// and registers tools, resources, and prompts based on what's available.
package mcp
