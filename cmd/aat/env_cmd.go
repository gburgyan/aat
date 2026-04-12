package main

import (
	"fmt"
	"io"
	"os"

	"github.com/gburgyan/aat/config"
	"github.com/spf13/cobra"
)

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Environment management commands",
}

var envListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available environments",
	Long:  "List all selectable environments defined in a multi-environment YAML file.",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		overrides := config.ProjectPaths{}
		if cmd.Flags().Changed("manifest") {
			overrides.ExplicitManifest, _ = cmd.Flags().GetString("manifest")
		}
		if cmd.Flags().Changed("env") {
			overrides.EnvPath, _ = cmd.Flags().GetString("env")
		}

		resolved, err := config.ResolveProjectPaths(overrides)
		if err != nil {
			return err
		}

		return envListCommand(resolved.EnvPath, os.Stdout)
	},
}

func init() {
	envCmd.AddCommand(envListCmd)

	envListCmd.Flags().String("manifest", "", "path to aat-project.yaml or project directory")
	envListCmd.Flags().String("env", "", "path to environment YAML file")
}

func envListCommand(envPath string, out io.Writer) error {
	if envPath == "" {
		return fmt.Errorf("environment file path is required (--env flag or aat-project.yaml)")
	}

	isMulti, err := config.IsMultiEnvFile(envPath)
	if err != nil {
		return fmt.Errorf("reading environment file: %w", err)
	}

	if !isMulti {
		env, err := config.LoadEnvironment(envPath)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "Single environment: %s (%s)\n", env.Name, env.APIBaseURL)
		return nil
	}

	names, err := config.ListEnvironments(envPath)
	if err != nil {
		return err
	}

	if len(names) == 0 {
		_, _ = fmt.Fprintln(out, "No environments found.")
		return nil
	}

	for _, name := range names {
		// Load each environment to show its base URL
		env, err := config.LoadNamedEnvironment(envPath, name)
		if err != nil {
			_, _ = fmt.Fprintf(out, "  %-12s (error: %s)\n", name, err)
			continue
		}
		baseURL := env.APIBaseURL
		if baseURL == "" {
			baseURL = "(overrides only)"
		}
		_, _ = fmt.Fprintf(out, "  %-12s %s\n", name, baseURL)
	}
	return nil
}
