package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/mcp"
	"github.com/mark3labs/mcp-go/server"
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
	Short: "Start the MCP server on stdio or HTTP",
	Long:  "Start the Model Context Protocol server for IDE-based AI tools.\nUse --http to serve over Streamable HTTP instead of stdio.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		manifestFlag, _ := cmd.Flags().GetString("manifest")
		personaFlag, _ := cmd.Flags().GetString("persona")
		httpFlag, _ := cmd.Flags().GetBool("http")
		portFlag, _ := cmd.Flags().GetInt("port")
		basePathFlag, _ := cmd.Flags().GetString("http-base-path")

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

		// HTTP mode forces remote-integration persona.
		if httpFlag {
			if personaFlag != "" && personaFlag != "api" && personaFlag != "all" {
				fmt.Fprintf(os.Stderr, "aat mcp: warning: --http forces api persona, ignoring --persona %q\n", personaFlag)
			}
			persona = mcp.PersonaRemoteIntegration
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

		// Log to stderr (stdout is reserved for MCP protocol in stdio mode)
		personaLabel := "all"
		if persona != mcp.PersonaAll {
			personaLabel = string(persona)
		}
		fmt.Fprintf(os.Stderr, "aat mcp: loaded project %q (%d nodes, persona: %s)\n",
			manifest.Name, len(ctx.Graph.Nodes), personaLabel)

		// Create server
		var srv *mcp.Server
		switch persona {
		case mcp.PersonaIntegration:
			srv = mcp.NewIntegrationServer(ctx)
		case mcp.PersonaRemoteIntegration:
			srv = mcp.NewRemoteIntegrationServer(ctx)
		case mcp.PersonaTest:
			srv = mcp.NewTestServer(ctx)
		default:
			srv = mcp.NewServer(ctx)
		}

		if httpFlag {
			return serveMCPHTTP(srv, portFlag, basePathFlag)
		}

		return srv.Serve()
	},
}

// serveMCPHTTP starts the MCP server over Streamable HTTP with graceful shutdown.
func serveMCPHTTP(srv *mcp.Server, port int, basePath string) error {
	addr := fmt.Sprintf(":%d", port)

	opts := []server.StreamableHTTPOption{
		server.WithStateLess(true),
		server.WithEndpointPath(basePath),
	}

	fmt.Fprintf(os.Stderr, "aat mcp: serving HTTP on http://localhost:%d%s\n", port, basePath)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	err := srv.ServeHTTP(ctx, addr, opts...)
	if err != nil {
		return err
	}

	fmt.Fprintln(os.Stderr, "aat mcp: shut down")
	return nil
}

func init() {
	mcpCmd.AddCommand(mcpServeCmd)

	mcpServeCmd.Flags().String("manifest", "", "path to aat-project.yaml (auto-detected if not specified)")
	mcpServeCmd.Flags().String("persona", "", "server persona: 'api' (integration developer), 'test' (test developer), or omit for all tools")
	mcpServeCmd.Flags().Bool("http", false, "serve over Streamable HTTP instead of stdio")
	mcpServeCmd.Flags().Int("port", 8080, "HTTP listen port (used with --http)")
	mcpServeCmd.Flags().String("http-base-path", "/mcp", "HTTP endpoint path (used with --http)")
}
