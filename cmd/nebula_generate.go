package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/claude"
	"github.com/papapumpkin/quasar/internal/config"
	"github.com/papapumpkin/quasar/internal/nebula"
	"github.com/papapumpkin/quasar/internal/snapshot"
	"github.com/papapumpkin/quasar/internal/ui"
)

const defaultMaxSlugLen = 50

// nonAlphanumHyphen matches any character that is not alphanumeric or a hyphen.
var nonAlphanumHyphen = regexp.MustCompile(`[^a-z0-9-]`)

// multiHyphen matches two or more consecutive hyphens.
var multiHyphen = regexp.MustCompile(`-{2,}`)

// addNebulaGenerateFlags registers CLI flags for the generate subcommand.
func addNebulaGenerateFlags(cmd *cobra.Command) {
	cmd.Flags().String("name", "", "Nebula name (default: derived from prompt)")
	cmd.Flags().String("output", "", "Output directory (default: .nebulas/<name>)")
	cmd.Flags().String("model", "", "Model override for the architect agent")
	cmd.Flags().Float64("budget", 10.0, "Max budget in USD for nebula generation")
	cmd.Flags().Bool("force", false, "Overwrite existing nebula directory")
	cmd.Flags().Bool("dry-run", false, "Preview generated nebula without writing to disk")
}

// runNebulaGenerate implements the `quasar nebula generate` command.
// It analyzes the codebase, invokes the generation pipeline, and writes
// the resulting nebula to disk.
func runNebulaGenerate(cmd *cobra.Command, args []string) error {
	printer := ui.New()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	prompt := args[0]

	// Derive nebula name.
	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		name = slugify(prompt, defaultMaxSlugLen)
	}

	// Determine output directory.
	outputDir, _ := cmd.Flags().GetString("output")
	if outputDir == "" {
		outputDir = filepath.Join(".nebulas", name)
	}

	model, _ := cmd.Flags().GetString("model")
	budget, _ := cmd.Flags().GetFloat64("budget")
	force, _ := cmd.Flags().GetBool("force")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Apply config-level model if flag not set.
	if model == "" {
		model = cfg.Model
	}

	// Resolve working directory.
	workDir := cfg.WorkDir
	if workDir == "" || workDir == "." {
		workDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	// Construct the Claude invoker.
	claudeInv := claude.NewInvoker(cfg.ClaudePath, cfg.Verbose)
	if err := claudeInv.Validate(); err != nil {
		printer.Error(fmt.Sprintf("claude CLI not available: %v", err))
		return fmt.Errorf("claude CLI not available: %w", err)
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	// Step 1: Analyze codebase.
	printer.Info("Analyzing codebase...")
	analysis, err := nebula.AnalyzeCodebase(ctx, workDir, snapshot.DefaultMaxSize)
	if err != nil {
		printer.Error(fmt.Sprintf("codebase analysis failed: %v", err))
		return fmt.Errorf("codebase analysis failed: %w", err)
	}
	printer.Info(fmt.Sprintf("Found %d packages in %s", len(analysis.Packages), analysis.ModulePath))

	// Step 2: Run generation pipeline.
	printer.Info(fmt.Sprintf("Generating nebula %q from prompt...", name))
	result, err := nebula.Generate(ctx, claudeInv, nebula.GenerateRequest{
		UserPrompt:   prompt,
		NebulaName:   name,
		OutputDir:    outputDir,
		WorkDir:      workDir,
		Analysis:     analysis,
		Model:        model,
		MaxBudgetUSD: budget,
	})
	if err != nil {
		printer.Error(fmt.Sprintf("generation failed: %v", err))
		return fmt.Errorf("generation failed: %w", err)
	}

	// Print warnings if any.
	for _, w := range result.Errors {
		printer.Info(fmt.Sprintf("  warning: %s", w))
	}

	// Step 3: Validate the generated nebula before writing to disk.
	if result.Nebula != nil {
		if validationErrs := nebula.Validate(result.Nebula); len(validationErrs) > 0 {
			for _, ve := range validationErrs {
				printer.Error(fmt.Sprintf("validation: %s", ve.Error()))
			}
			return fmt.Errorf("generated nebula has %d validation error(s)", len(validationErrs))
		}
	}

	// Step 4: Handle dry-run.
	if dryRun {
		printDryRun(printer, result, outputDir)
		return nil
	}

	// Step 5: Write to disk.
	if err := nebula.WriteNebula(result, outputDir, nebula.WriteOptions{Overwrite: force}); err != nil {
		printer.Error(fmt.Sprintf("failed to write nebula: %v", err))
		return fmt.Errorf("failed to write nebula: %w", err)
	}

	// Step 6: Print summary.
	printer.Info(fmt.Sprintf("Generated %d phases in %s", len(result.Phases), outputDir))
	printer.Info(fmt.Sprintf("Total generation cost: $%.4f", result.CostUSD))
	printer.Info(fmt.Sprintf("Run 'quasar nebula validate %s' to verify.", outputDir))

	return nil
}

// printDryRun displays the generated nebula to stderr without writing files.
func printDryRun(printer *ui.Printer, result *nebula.GenerateResult, outputDir string) {
	printer.Info(fmt.Sprintf("Dry run: would write %d phases to %s", len(result.Phases), outputDir))
	printer.Info("")
	printer.Info(fmt.Sprintf("Nebula: %s", result.Manifest.Nebula.Name))
	printer.Info(fmt.Sprintf("Description: %s", result.Manifest.Nebula.Description))
	printer.Info("")

	for i, phase := range result.Phases {
		deps := "none"
		if len(phase.DependsOn) > 0 {
			deps = strings.Join(phase.DependsOn, ", ")
		}
		printer.Info(fmt.Sprintf("  %02d. %-30s depends_on=[%s]", i+1, phase.ID, deps))
		printer.Info(fmt.Sprintf("       %s", phase.Title))
	}

	printer.Info("")
	printer.Info(fmt.Sprintf("Total generation cost: $%.4f", result.CostUSD))
}

// slugify converts a human-readable prompt into a kebab-case nebula name.
// It lowercases, replaces spaces/underscores with hyphens, strips
// non-alphanumeric characters, collapses consecutive hyphens, and truncates
// to maxLen characters.
func slugify(input string, maxLen int) string {
	s := strings.ToLower(strings.TrimSpace(input))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	s = nonAlphanumHyphen.ReplaceAllString(s, "")
	s = multiHyphen.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")

	if maxLen > 0 && len(s) > maxLen {
		s = s[:maxLen]
		s = strings.TrimRight(s, "-")
	}
	return s
}
