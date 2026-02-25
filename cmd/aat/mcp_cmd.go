package main

import (
	"fmt"
	"os"

	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/mcp"
	"github.com/spf13/cobra"
)

// mcpCmd is the parent Cobra command for MCP subcommands.
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "MCP server commands",
}

// mcpServeCmd is the Cobra command for starting the MCP server.
var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the MCP server on stdio",
	Long:  "Start the Model Context Protocol server for IDE-based AI tools.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		manifestFlag, _ := cmd.Flags().GetString("manifest")
		personaFlag, _ := cmd.Flags().GetString("persona")

		// Validate persona flag
		var persona mcp.ServerPersona
		switch personaFlag {
		case "", "all":
			persona = mcp.PersonaAll
		case "api":
			persona = mcp.PersonaIntegration
		case "test":
			persona = mcp.PersonaTest
		default:
			return fmt.Errorf("invalid persona %q: use 'api', 'test', or omit for all", personaFlag)
		}

		// Find manifest: explicit flag > resolver
		var manifestPath string
		if manifestFlag != "" {
			manifestPath = manifestFlag
		} else {
			resolved, err := config.ResolveProjectPaths(config.ProjectPaths{})
			if err == nil && resolved.ManifestPath != "" {
				manifestPath = resolved.ManifestPath
			} else {
				fmt.Fprintln(os.Stderr, "aat mcp serve: aat-project.yaml not found")
				fmt.Fprintln(os.Stderr, "hint: create aat-project.yaml or pass --manifest")
				return &exitError{Code: 1}
			}
		}

		// Load manifest
		manifest, err := config.LoadManifest(manifestPath)
		if err != nil {
			return fmt.Errorf("loading manifest: %w", err)
		}

		// Build server context
		ctx, err := mcp.BuildServerContext(manifest)
		if err != nil {
			return err
		}

		// Log to stderr (stdout is reserved for MCP protocol)
		personaLabel := "all"
		if persona != mcp.PersonaAll {
			personaLabel = string(persona)
		}
		fmt.Fprintf(os.Stderr, "aat mcp: loaded project %q (%d nodes, persona: %s)\n",
			manifest.Name, len(ctx.Graph.Nodes), personaLabel)

		// Create and serve
		var srv *mcp.Server
		switch persona {
		case mcp.PersonaIntegration:
			srv = mcp.NewIntegrationServer(ctx)
		case mcp.PersonaTest:
			srv = mcp.NewTestServer(ctx)
		default:
			srv = mcp.NewServer(ctx)
		}

		if err := srv.Serve(); err != nil {
			return err
		}

		return nil
	},
}

func init() {
	mcpCmd.AddCommand(mcpServeCmd)

	mcpServeCmd.Flags().String("manifest", "", "path to aat-project.yaml (auto-detected if not specified)")
	mcpServeCmd.Flags().String("persona", "", "server persona: 'api' (integration developer), 'test' (test developer), or omit for all tools")
}
