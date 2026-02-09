package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/gburgyan/aat/adapter"
	"github.com/gburgyan/aat/config"
	"github.com/gburgyan/aat/domain"
	"github.com/gburgyan/aat/engine"
	"github.com/gburgyan/aat/graph"
	"github.com/gburgyan/aat/llm"
	"github.com/gburgyan/aat/plan"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: aat <command> [options]")
		fmt.Fprintln(os.Stderr, "commands: run, prompt")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		code := runMain(os.Args[2:])
		os.Exit(code)
	case "prompt":
		code := promptMain(os.Args[2:])
		os.Exit(code)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		fmt.Fprintln(os.Stderr, "commands: run, prompt")
		os.Exit(1)
	}
}

// runArgs holds parsed CLI flags for the run command.
type runArgs struct {
	PlanPath      string
	EnvPath       string
	GraphPath     string
	TemplatesPath string
	OutputDir     string
	Mode          string
	DomainPath    string
}

// runMain parses flags and delegates to runCommand.
func runMain(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	ra := &runArgs{}
	fs.StringVar(&ra.PlanPath, "plan", "", "path to plan YAML file (required)")
	fs.StringVar(&ra.EnvPath, "env", "", "path to environment YAML file (required)")
	fs.StringVar(&ra.GraphPath, "graph", "", "path to graph YAML file (required)")
	fs.StringVar(&ra.TemplatesPath, "templates", "", "path to templates directory (required)")
	fs.StringVar(&ra.OutputDir, "output", "runs", "directory for archive output")
	fs.StringVar(&ra.Mode, "mode", "", "execution mode: strict, lean, adaptive (overrides env config)")
	fs.StringVar(&ra.DomainPath, "domain", "", "path to domain knowledge YAML file")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	if err := runCommand(context.Background(), ra); err != nil {
		fmt.Fprintf(os.Stderr, "aat: %s\n", err)
		return 1
	}
	return 0
}

// runCommand executes the full run pipeline. Extracted for testability.
func runCommand(ctx context.Context, args *runArgs) error {
	// Validate required flags
	if args.PlanPath == "" {
		return fmt.Errorf("--plan is required")
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

	// 2. Authenticate
	apiConfig, err := env.BuildAPIConfig(ctx)
	if err != nil {
		return fmt.Errorf("building API config: %w", err)
	}
	fmt.Printf("aat: authenticated via %s\n", env.Auth.Type)

	// 3. Load graph
	g, err := graph.ParseFile(args.GraphPath)
	if err != nil {
		return fmt.Errorf("loading graph: %w", err)
	}
	fmt.Printf("aat: loaded graph (%d nodes, %d edges)\n", len(g.Nodes), len(g.Edges))

	// 4. Load plan
	p, err := plan.ParseFile(args.PlanPath)
	if err != nil {
		return fmt.Errorf("loading plan: %w", err)
	}

	// 5. Load domain knowledge (optional)
	var kb *domain.KnowledgeBase
	if args.DomainPath != "" {
		kb, err = domain.ParseFile(args.DomainPath)
		if err != nil {
			return fmt.Errorf("loading domain knowledge: %w", err)
		}
		fmt.Printf("aat: loaded domain knowledge\n")
	}

	// 6. Load templates
	registry := adapter.NewRegistry()
	count, err := adapter.LoadTemplates(args.TemplatesPath, registry)
	if err != nil {
		return fmt.Errorf("loading templates: %w", err)
	}
	fmt.Printf("aat: loaded %d templates\n", count)

	// 7. Create executor and environment config
	executor := adapter.NewHTTPExecutor(apiConfig.BaseURL)
	envConfig := &adapter.EnvironmentConfig{
		BaseURL: apiConfig.BaseURL,
		Headers: apiConfig.Headers,
		Values:  apiConfig.Values,
	}

	// 8. Determine execution mode
	effectiveMode := config.ExecutionMode(args.Mode)
	if effectiveMode == "" {
		effectiveMode = env.LLM.Mode
	}
	if effectiveMode == "" {
		effectiveMode = config.ModeStrict
	}

	// 9. Create LLM client if mode requires it
	var llmClient llm.Client
	if effectiveMode != config.ModeStrict && env.LLM.Endpoint != "" {
		llmClient, err = llm.NewClient(env.LLM)
		if err != nil {
			return fmt.Errorf("creating LLM client: %w", err)
		}
	}

	// 10. Create engine and run
	eng := engine.NewEngine(g, registry, executor, envConfig).
		WithMode(effectiveMode).
		WithDomain(kb).
		WithLLM(llmClient).
		WithMaxRelaxationDepth(env.Settings.MaxRelaxationDepth)
	fmt.Printf("aat: executing plan (%d steps, mode=%s)...\n\n", len(p.Execution.Steps), effectiveMode)

	result := eng.Run(ctx, p)

	// 8. Print summary
	printSummary(result)

	// 9. Write archive
	if err := writeArchive(result, p, env, g, args.OutputDir); err != nil {
		return err
	}

	// 10. Exit code
	if result.Outcome != engine.OutcomePassed {
		return fmt.Errorf("%s", outcomeMessage(result))
	}
	return nil
}

// printSummary writes a human-readable step-by-step summary to stdout.
func printSummary(result *engine.RunResult) {
	total := len(result.Steps)
	for i, step := range result.Steps {
		prefix := fmt.Sprintf("  [%d/%d] %-20s", i+1, total, step.Node)
		if step.Error != nil {
			if step.RetryCount > 0 {
				fmt.Printf("%s ERROR [%s] (after %d retries)\n", prefix, errorCategory(step), step.RetryCount)
			} else {
				fmt.Printf("%s ERROR: %s\n", prefix, step.Error)
			}
		} else if step.Response != nil {
			status := fmt.Sprintf("%d", step.StatusCode)
			validMark := ""
			if step.Validation != nil && !step.Validation.Passed {
				validMark = "  ASSERTIONS FAILED"
			}
			fmt.Printf("%s %s  %dms%s\n", prefix, status, step.Duration.Milliseconds(), validMark)
		} else {
			fmt.Printf("%s (no response)\n", prefix)
		}
	}

	if len(result.CleanupResults) > 0 {
		fmt.Println("\n  cleanup:")
		for _, step := range result.CleanupResults {
			prefix := fmt.Sprintf("    %-22s", step.Node)
			if step.Error != nil {
				fmt.Printf("%s ERROR: %s\n", prefix, step.Error)
			} else if step.Response != nil {
				fmt.Printf("%s %d  %dms\n", prefix, step.StatusCode, step.Duration.Milliseconds())
			} else {
				fmt.Printf("%s (no response)\n", prefix)
			}
		}
	}

	fmt.Println()
	switch result.Outcome {
	case engine.OutcomePassed:
		fmt.Printf("PASSED (%d/%d steps, %s)\n", total, total, totalDuration(result))
	case engine.OutcomeFailed:
		fmt.Printf("FAILED: %s\n", outcomeMessage(result))
	case engine.OutcomeError:
		fmt.Printf("ERROR: %s\n", outcomeMessage(result))
	}
}

// errorCategory returns the error classification category string for a step.
func errorCategory(step engine.StepResult) string {
	if step.ErrorClass != nil {
		return step.ErrorClass.Category.String()
	}
	return "unknown"
}

// totalDuration sums the duration of all steps.
func totalDuration(result *engine.RunResult) string {
	var total time.Duration
	for _, s := range result.Steps {
		total += s.Duration
	}
	for _, s := range result.CleanupResults {
		total += s.Duration
	}
	if total < time.Second {
		return fmt.Sprintf("%dms", total.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", total.Seconds())
}

// outcomeMessage produces a summary error string.
func outcomeMessage(result *engine.RunResult) string {
	if result.Error != nil {
		return result.Error.Error()
	}
	return result.Outcome.String()
}
