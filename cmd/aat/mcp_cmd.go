package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/mcp"
	webserver "github.com/gburgyan/aat/server"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
)

// mcpCmd is the parent Cobra command for MCP subcommands.
var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "MCP server commands",
	RunE:  groupRunE,
}

// mcpServeCmd is the Cobra command for starting the MCP server.
var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the MCP server on stdio or HTTP",
	Long:  "Start the Model Context Protocol server for IDE-based AI tools.\nUse --http to serve over Streamable HTTP instead of stdio.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		manifestFlag, _ := cmd.Flags().GetString("manifest")
		personaFlag, _ := cmd.Flags().GetString("persona")
		httpFlag, _ := cmd.Flags().GetBool("http")
		portFlag, _ := cmd.Flags().GetInt("port")
		basePathFlag, _ := cmd.Flags().GetString("http-base-path")
		logFlag, _ := cmd.Flags().GetBool("log")

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

		// Find manifest: explicit flag > resolver. An MCP client shows only that
		// the server exited, so every reason it cannot start is in the error.
		var manifestPath string
		if manifestFlag != "" {
			manifestPath = manifestFlag
		} else {
			resolved, err := config.ResolveProjectPaths(config.ProjectPaths{})
			if err != nil {
				return err
			}
			if resolved.ManifestPath == "" {
				return errors.New("no manifest found (checked AAT_PROJECT, CWD walk-up, and user config): create aat-project.yaml or pass --manifest")
			}
			manifestPath = resolved.ManifestPath
		}

		// Load manifest
		manifest, err := config.LoadManifest(manifestPath)
		if err != nil {
			return fmt.Errorf("loading manifest: %w", err)
		}

		// Override default environment from flag or env var
		envName := resolveEnvName(cmd)
		if envName != "" {
			manifest.DefaultEnvironment = envName
		}

		varFlags, _ := cmd.Flags().GetStringArray("var")
		vars, err := config.ParseVars(varFlags)
		if err != nil {
			return err
		}

		// Build server context
		ctx, err := mcp.BuildServerContextWithVars(manifest, vars)
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

		// Build server options
		var serverOpts []mcp.ServerOption
		if logFlag {
			logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
			serverOpts = append(serverOpts, mcp.WithLogger(logger))
		}

		// Create server
		var srv *mcp.Server
		switch persona {
		case mcp.PersonaIntegration:
			srv = mcp.NewIntegrationServer(ctx, serverOpts...)
		case mcp.PersonaRemoteIntegration:
			srv = mcp.NewRemoteIntegrationServer(ctx, serverOpts...)
		case mcp.PersonaTest:
			srv = mcp.NewTestServer(ctx, serverOpts...)
		default:
			srv = mcp.NewServer(ctx, serverOpts...)
		}

		if httpFlag {
			return serveMCPHTTP(srv, resolveHost(cmd), portFlag, basePathFlag)
		}

		return srv.Serve()
	},
}

// serveMCPHTTP starts the MCP server over Streamable HTTP on host:port with
// graceful shutdown.
func serveMCPHTTP(srv *mcp.Server, host string, port int, basePath string) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	opts := []server.StreamableHTTPOption{
		server.WithStateLess(true),
		server.WithEndpointPath(basePath),
	}

	fmt.Fprintf(os.Stderr, "aat mcp: serving HTTP on %s%s\n", webserver.BrowseURL(host, port), basePath)

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
	mcpServeCmd.Flags().String("host", webserver.DefaultHost, hostFlagHelp+" (used with --http)")
	mcpServeCmd.Flags().String("http-base-path", "/mcp", "HTTP endpoint path (used with --http)")
	mcpServeCmd.Flags().Bool("log", false, "enable structured JSON logging of tool calls to stderr")
	mcpServeCmd.Flags().String("env", "", "environment name (for multi-environment files)")
	mcpServeCmd.Flags().StringArray("var", nil, "set a var of a multi-environment file, KEY=VALUE (repeatable; wins over the file's vars)")
}
