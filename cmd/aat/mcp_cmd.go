package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/gburgyan/aat/mcp"
)

// mcpMain dispatches to MCP subcommands.
func mcpMain(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: aat mcp <subcommand> [options]")
		fmt.Fprintln(os.Stderr, "subcommands: serve")
		return 1
	}

	switch args[0] {
	case "serve":
		return mcpServeMain(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown mcp subcommand: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "subcommands: serve")
		return 1
	}
}

// mcpServeMain starts the MCP server on stdio.
func mcpServeMain(args []string) int {
	fs := flag.NewFlagSet("mcp serve", flag.ContinueOnError)
	manifestPath := fs.String("manifest", "", "path to aat-project.yaml (auto-detected if not specified)")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	// Find manifest
	var path string
	if *manifestPath != "" {
		path = *manifestPath
	} else {
		found, err := mcp.FindManifest()
		if err != nil {
			fmt.Fprintf(os.Stderr, "aat mcp serve: %s\n", err)
			fmt.Fprintln(os.Stderr, "hint: create aat-project.yaml or pass --manifest")
			return 1
		}
		path = found
	}

	// Load manifest
	manifest, err := mcp.LoadManifest(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aat mcp serve: %s\n", err)
		return 1
	}

	// Build server context
	ctx, err := mcp.BuildServerContext(manifest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aat mcp serve: %s\n", err)
		return 1
	}

	// Log to stderr (stdout is reserved for MCP protocol)
	fmt.Fprintf(os.Stderr, "aat mcp: loaded project %q (%d nodes)\n",
		manifest.Name, len(ctx.Graph.Nodes))

	// Create and serve
	srv := mcp.NewServer(ctx)
	if err := srv.Serve(); err != nil {
		fmt.Fprintf(os.Stderr, "aat mcp serve: %s\n", err)
		return 1
	}

	return 0
}
