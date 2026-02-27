package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/intent"
	"github.com/gburgyan/aat/llm"
	"github.com/gburgyan/aat/plan"
	"github.com/spf13/cobra"
)

// promptCmd is the Cobra command for LLM-generated plan execution.
var promptCmd = &cobra.Command{
	Use:   "prompt [text]",
	Short: "Generate and execute a test plan from a natural language prompt",
	Long:  "Use an LLM to generate a test plan from a natural language prompt, then optionally execute it.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true

		// Build overrides from explicitly-set flags only
		overrides := config.ProjectPaths{}
		if cmd.Flags().Changed("manifest") {
			overrides.ExplicitManifest, _ = cmd.Flags().GetString("manifest")
		}
		if cmd.Flags().Changed("graph") {
			overrides.GraphPath, _ = cmd.Flags().GetString("graph")
		}
		if cmd.Flags().Changed("env") {
			overrides.EnvPath, _ = cmd.Flags().GetString("env")
		}
		if cmd.Flags().Changed("templates") {
			overrides.TemplatesPath, _ = cmd.Flags().GetString("templates")
		}
		if cmd.Flags().Changed("domain") {
			overrides.DomainPath, _ = cmd.Flags().GetString("domain")
		}

		resolved, err := config.ResolveProjectPaths(overrides)
		if err != nil {
			return err
		}

		savePlan, _ := cmd.Flags().GetString("save")
		if savePlan != "" {
			savePlan = resolveSavePlan(savePlan, resolved.PlanDirs)
		}
		saveFull, _ := cmd.Flags().GetBool("save-full")
		autoConfirm, _ := cmd.Flags().GetBool("yes")
		tracePlan, _ := cmd.Flags().GetBool("trace")

		outputDir := "_output/runs"
		if cmd.Flags().Changed("output") {
			outputDir, _ = cmd.Flags().GetString("output")
		} else if resolved.ArchiveDir != "" {
			outputDir = resolved.ArchiveDir
		}

		traceDir := "_output/traces"
		if cmd.Flags().Changed("trace-dir") {
			traceDir, _ = cmd.Flags().GetString("trace-dir")
		} else if resolved.TracesDir != "" {
			traceDir = resolved.TracesDir
		}

		var promptText string
		if len(args) > 0 {
			promptText = args[0]
		}

		layerFlags, _ := cmd.Flags().GetStringSlice("layer")
		noAutoOverrides, _ := cmd.Flags().GetBool("no-auto-overrides")

		// Discover auto-overrides if not disabled
		var autoOverridesPath string
		if !noAutoOverrides {
			autoOverridesPath = config.FindAutoOverrides()
			if autoOverridesPath != "" {
				fmt.Printf("aat: auto-discovered overrides: %s\n", autoOverridesPath)
			}
		}

		pa := &promptArgs{
			Prompt:            promptText,
			EnvPath:           resolved.EnvPath,
			GraphPath:         resolved.GraphPath,
			TemplatesPath:     resolved.TemplatesPath,
			DomainPath:        resolved.DomainPath,
			OutputDir:         outputDir,
			SavePlan:          savePlan,
			SaveFull:          saveFull,
			AutoConfirm:       autoConfirm,
			TracePlan:         tracePlan,
			TraceDir:          traceDir,
			Layers:            layerFlags,
			LayersDir:         resolved.LayersDir,
			AutoOverridesPath: autoOverridesPath,
		}

		return promptCommand(context.Background(), pa, os.Stdin)
	},
}

func init() {
	promptCmd.Flags().String("manifest", "", "path to aat-project.yaml or project directory")
	promptCmd.Flags().String("env", "", "path to environment YAML file")
	promptCmd.Flags().String("graph", "", "path to graph YAML file")
	promptCmd.Flags().String("templates", "", "path to templates directory")
	promptCmd.Flags().String("domain", "", "path to domain knowledge YAML file")
	promptCmd.Flags().String("output", "_output/runs", "directory for archive output")
	promptCmd.Flags().String("save", "", "save generated plan as recipe (name resolved via plans dir, or literal path)")
	promptCmd.Flags().Bool("save-full", false, "save as full expanded plan instead of compact recipe")
	promptCmd.Flags().Bool("yes", false, "skip confirmation prompt")
	promptCmd.Flags().Bool("trace", false, "capture planning pipeline trace for debugging")
	promptCmd.Flags().String("trace-dir", "_output/traces", "directory for plan trace output")
	promptCmd.Flags().StringSlice("layer", nil, "data layer to apply (repeatable, e.g. --layer european --layer amex)")
	promptCmd.Flags().Bool("no-auto-overrides", false, "disable auto-discovery of .aat-overrides.yaml")
}

// promptArgs holds parsed CLI flags for the prompt command.
type promptArgs struct {
	Prompt            string
	EnvPath           string
	GraphPath         string
	TemplatesPath     string
	DomainPath        string
	OutputDir         string
	SavePlan          string
	SaveFull          bool // when true, save as full expanded plan instead of compact recipe
	AutoConfirm       bool
	TracePlan         bool
	TraceDir          string
	Layers            []string
	LayersDir         string
	AutoOverridesPath string // path to auto-discovered .aat-overrides.yaml
}

// promptCommand executes the full prompt-to-execution pipeline.
func promptCommand(ctx context.Context, args *promptArgs, reader io.Reader) error {
	// Validate required flags
	if args.Prompt == "" {
		return fmt.Errorf("prompt text is required (first positional argument)")
	}
	if args.EnvPath == "" {
		return fmt.Errorf("--env is required")
	}
	if args.GraphPath == "" {
		return fmt.Errorf("--graph is required")
	}
	if args.TemplatesPath == "" {
		return fmt.Errorf("--templates is required")
	}

	// 1. Load environment
	fmt.Printf("aat: loading environment...\n")
	env, err := config.LoadEnvironment(args.EnvPath)
	if err != nil {
		return fmt.Errorf("loading environment: %w", err)
	}
	fmt.Printf("aat: loaded environment %q\n", env.Name)

	// 2. Check LLM config
	if env.LLM.Endpoint == "" {
		return fmt.Errorf("environment %q has no LLM configuration (llm.endpoint is required for prompt command)", env.Name)
	}

	// 3. Authenticate (via cached provider)
	authProvider := config.NewAuthProvider(env.Auth)
	token, err := authProvider.Authenticate(ctx)
	if err != nil {
		return fmt.Errorf("authenticating: %w", err)
	}
	apiConfig := env.BuildAPIConfigFromToken(token, env.Auth, nil)
	fmt.Printf("aat: authenticated via %s\n", env.Auth.Type)

	// 4. Load graph
	g, err := graph.ParseFile(args.GraphPath)
	if err != nil {
		return fmt.Errorf("loading graph: %w", err)
	}
	fmt.Printf("aat: loaded graph (%d nodes)\n", len(g.Nodes))

	// 5. Load domain knowledge (optional)
	var kb *domain.KnowledgeBase
	if args.DomainPath != "" {
		kb, err = domain.ParseFile(args.DomainPath)
		if err != nil {
			return fmt.Errorf("loading domain knowledge: %w", err)
		}
		fmt.Printf("aat: loaded domain knowledge\n")
	}

	// 6. Create LLM client
	llmClient, err := llm.NewClient(env.LLM)
	if err != nil {
		return fmt.Errorf("creating LLM client: %w", err)
	}

	// 7. Interpret prompt
	fmt.Printf("aat: analyzing prompt with LLM...\n")
	result, err := intent.Interpret(ctx, intent.InterpretRequest{
		Prompt:      args.Prompt,
		Graph:       g,
		KB:          kb,
		Client:      llmClient,
		EnableTrace: args.TracePlan,
		GraphDir:    filepath.Dir(args.GraphPath),
		Layers:      args.Layers,
	})

	// Write trace if present (even on error — partial traces aid debugging).
	if result != nil && result.Trace != nil {
		tracePath := filepath.Join(args.TraceDir, result.Trace.TraceID, "plan-trace.json")
		if writeErr := intent.WritePlanTrace(result.Trace, tracePath); writeErr != nil {
			fmt.Fprintf(os.Stderr, "aat: warning: failed to write plan trace: %s\n", writeErr)
		} else {
			fmt.Printf("aat: plan trace: %s\n", tracePath)
		}
	}

	if err != nil {
		return fmt.Errorf("interpreting prompt: %w", err)
	}
	fmt.Printf("aat: plan generated (%d steps)\n\n", len(result.Plan.Execution.Steps))

	// 8. Display plan narrative
	narrative := plan.FormatNarrative(result.Plan, g)
	fmt.Println(narrative)

	// 9. Save plan if --save specified
	if args.SavePlan != "" {
		if args.SaveFull {
			// Legacy full-plan format.
			if err := plan.WriteFile(result.Plan, args.SavePlan); err != nil {
				return fmt.Errorf("saving plan: %w", err)
			}
			fmt.Printf("aat: full plan saved to %s\n", args.SavePlan)
		} else {
			// Default: compact recipe format.
			r := &plan.Recipe{
				Kind:      "recipe",
				Metadata:  result.Plan.Metadata,
				Selection: intent.WorkflowSelectionToRecipeSelection(result.WorkflowSelection),
			}
			if result.TargetedResponse != nil {
				r.Overrides = intent.TargetedResponseToRecipeOverrides(result.TargetedResponse)
			}
			if err := plan.WriteRecipe(r, args.SavePlan); err != nil {
				return fmt.Errorf("saving recipe: %w", err)
			}
			fmt.Printf("aat: recipe saved to %s\n", args.SavePlan)
		}
	}

	// 10. Confirmation loop
	p := result.Plan
	for {
		choice := confirmPlan(reader, args.AutoConfirm)
		switch choice {
		case "y":
			// proceed to execution
		case "n":
			fmt.Println("aat: plan rejected, exiting.")
			return nil
		case "a":
			adjusted, err := adjustPlan(p, g, reader)
			if err != nil {
				return fmt.Errorf("adjusting plan: %w", err)
			}
			p = adjusted
			fmt.Println()
			fmt.Println(plan.FormatNarrative(p, g))
			continue
		}
		break
	}

	// 11. Execute
	return executePlan(ctx, p, g, args, apiConfig, env, kb, llmClient, authProvider)
}

// executePlan loads templates, creates the engine, runs the plan, and writes an archive.
func executePlan(ctx context.Context, p *plan.Plan, g *graph.Graph, args *promptArgs, apiConfig *config.APIConfig, env *config.Environment, kb *domain.KnowledgeBase, llmClient llm.Client, authProvider *config.AuthProvider) error {
	// If the plan has its own auth (e.g., user manually edited after --save), re-authenticate
	planOverridesAuth := p.Auth != nil
	if planOverridesAuth {
		effectiveAuth := *p.Auth
		fmt.Printf("aat: plan has its own auth (%s), re-authenticating...\n", effectiveAuth.Type)
		token, err := config.Authenticate(ctx, effectiveAuth)
		if err != nil {
			return fmt.Errorf("authenticating with plan auth: %w", err)
		}
		apiConfig = env.BuildAPIConfigFromToken(token, effectiveAuth, p.Headers)
	} else if len(p.Headers) > 0 {
		// Plan has extra headers but no auth override — use cached token, rebuild with plan headers
		token, err := authProvider.Authenticate(ctx)
		if err != nil {
			return fmt.Errorf("authenticating: %w", err)
		}
		apiConfig = env.BuildAPIConfigFromToken(token, env.Auth, p.Headers)
	}

	// Determine effective auth for override inheritance
	effectiveAuth := env.Auth
	if planOverridesAuth {
		effectiveAuth = *p.Auth
	}

	// Load templates
	registry := adapter.NewRegistry()
	count, err := adapter.LoadTemplates(args.TemplatesPath, registry)
	if err != nil {
		return fmt.Errorf("loading templates: %w", err)
	}
	fmt.Printf("aat: loaded %d templates\n", count)

	// Create executor and environment config
	executor := adapter.NewHTTPExecutor(apiConfig.BaseURL)
	envConfig := &adapter.EnvironmentConfig{
		BaseURL: apiConfig.BaseURL,
		Headers: apiConfig.Headers,
		Values:  apiConfig.Values,
	}
	router := engine.NewExecutorRouter(executor, envConfig)

	// Apply env-file overrides (inherit plan auth if present)
	if len(env.Overrides) > 0 {
		var resolvedOverrides []config.ResolvedOverride
		var overrideErr error
		if planOverridesAuth {
			resolvedOverrides, overrideErr = env.BuildOverrideConfigsWithAuth(ctx, apiConfig.Headers, effectiveAuth)
		} else {
			resolvedOverrides, overrideErr = env.BuildOverrideConfigsWithProvider(ctx, apiConfig.Headers, authProvider)
		}
		if overrideErr != nil {
			return fmt.Errorf("building overrides: %w", overrideErr)
		}
		for _, ov := range resolvedOverrides {
			addResolvedOverride(router, ov)
		}
	}

	// Apply auto-discovered .aat-overrides.yaml
	if args.AutoOverridesPath != "" {
		autoOverrides, autoErr := config.LoadOverlayFile(args.AutoOverridesPath)
		if autoErr != nil {
			return fmt.Errorf("loading auto-overrides %s: %w", args.AutoOverridesPath, autoErr)
		}
		autoOverrideEnv := &config.Environment{
			APIBaseURL: env.APIBaseURL,
			Auth:       effectiveAuth,
			Overrides:  autoOverrides,
		}
		var resolvedOverrides []config.ResolvedOverride
		if planOverridesAuth {
			resolvedOverrides, autoErr = autoOverrideEnv.BuildOverrideConfigsWithAuth(ctx, apiConfig.Headers, effectiveAuth)
		} else {
			resolvedOverrides, autoErr = autoOverrideEnv.BuildOverrideConfigsWithProvider(ctx, apiConfig.Headers, authProvider)
		}
		if autoErr != nil {
			return fmt.Errorf("building auto-overrides: %w", autoErr)
		}
		for _, ov := range resolvedOverrides {
			addResolvedOverride(router, ov)
		}
	}

	// Compute layered defaults if layers are specified
	var layeredDefaults map[string]*graph.InputDefault
	if len(args.Layers) > 0 && args.LayersDir != "" {
		layers, layerErr := graph.ResolveLayerNames(args.Layers, args.LayersDir)
		if layerErr != nil {
			return fmt.Errorf("loading layers: %w", layerErr)
		}
		layeredDefaults, layerErr = graph.ApplyLayers(g, args.Layers, layers)
		if layerErr != nil {
			return fmt.Errorf("applying layers: %w", layerErr)
		}
	}

	// Create engine and run
	ti := DetectTerminal()
	observer := &CLIProgressObserver{out: os.Stdout, term: ti}
	eng := engine.NewEngine(g, registry, router).
		WithDomain(kb).
		WithProgress(observer).
		WithLayers(layeredDefaults)
	fmt.Printf("aat: executing plan (%d steps)...\n\n", len(p.Execution.Steps))

	result := eng.Run(ctx, p)

	// Write archive
	archivePath, archiveErr := writeRunArchive(result, p, env, g, args.OutputDir, args.Layers)
	if archiveErr != nil {
		return archiveErr
	}
	fmt.Printf("Archive: %s\n", archivePath)

	// Exit code
	if result.Outcome != engine.OutcomePassed {
		return fmt.Errorf("%s", outcomeMessage(result))
	}
	return nil
}

// confirmPlan prompts the user and returns "y", "n", or "a" (adjust).
// Enter defaults to "y". EOF defaults to "n".
func confirmPlan(reader io.Reader, autoConfirm bool) string {
	if autoConfirm {
		return "y"
	}

	fmt.Print("Execute this plan? [Y/n/a(djust)] ")
	scanner := bufio.NewScanner(reader)
	if !scanner.Scan() {
		return "n" // EOF
	}
	input := strings.TrimSpace(strings.ToLower(scanner.Text()))
	switch input {
	case "", "y", "yes":
		return "y"
	case "n", "no":
		return "n"
	case "a", "adjust":
		return "a"
	default:
		return "n"
	}
}

// adjustPlan saves the plan to a temp file, waits for the user to edit it,
// then reloads and revalidates.
func adjustPlan(p *plan.Plan, g *graph.Graph, reader io.Reader) (*plan.Plan, error) {
	// Save to temp file
	tmpDir, err := os.MkdirTemp("", "aat-plan-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp directory: %w", err)
	}
	tmpPath := filepath.Join(tmpDir, "plan.yaml")

	if err := plan.WriteFile(p, tmpPath); err != nil {
		return nil, fmt.Errorf("saving plan for editing: %w", err)
	}

	fmt.Printf("\naat: plan saved to %s\n", tmpPath)
	fmt.Println("aat: edit the file, then press Enter to reload...")

	// Wait for Enter
	scanner := bufio.NewScanner(reader)
	scanner.Scan() // consume one line (Enter)

	// Reload
	edited, err := plan.ParseFile(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("reloading edited plan: %w", err)
	}

	// Revalidate
	if _, err := plan.InstantiateAndValidate(edited, g); err != nil {
		return nil, fmt.Errorf("edited plan validation failed: %w", err)
	}

	fmt.Println("aat: edited plan loaded and validated.")
	return edited, nil
}

// resolveSavePlan resolves the --save flag value to a file path.
// If planDirs are configured and the name is not an absolute path or a literal
// path with a recognized extension, it resolves through the plan directories.
func resolveSavePlan(name string, planDirs []string) string {
	// Absolute paths are used as-is
	if filepath.IsAbs(name) {
		return name
	}

	// Paths with recognized extensions are treated as literal (backward compat)
	ext := filepath.Ext(name)
	if ext == ".yaml" || ext == ".yml" {
		return name
	}

	// Resolve through plan dirs if configured
	if len(planDirs) > 0 {
		if resolved, err := config.ResolvePlanWritePath(planDirs, name); err == nil {
			return resolved
		}
	}

	// CWD fallback: just append .yaml
	return name + ".yaml"
}
