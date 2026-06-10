package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// configFileName is the name of the config file `quasar init` scaffolds.
const configFileName = ".quasar.yaml"

// initCmd scaffolds a .quasar.yaml in the working directory with sensible
// auto-detected defaults.
var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Scaffold a .quasar.yaml in the current directory",
	Long: "Create a .quasar.yaml in the working directory, auto-detecting the " +
		"project language for [verify] and the GitHub origin remote for " +
		"[integrations.github]. Refuses to overwrite an existing file unless " +
		"--force is given.",
	RunE:          runInit,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	initCmd.Flags().Bool("force", false, "Overwrite an existing .quasar.yaml")
	initCmd.Flags().Bool("yes", false, "Skip the overwrite confirmation prompt (requires --force)")
	rootCmd.AddCommand(initCmd)
}

// runInit is the cobra adapter for `quasar init`. It resolves the working
// directory and flags, then delegates to runInitWith with production I/O.
func runInit(cmd *cobra.Command, _ []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	force, _ := cmd.Flags().GetBool("force")
	yes, _ := cmd.Flags().GetBool("yes")

	return runInitWith(wd, force, yes, cmd.InOrStdin(), cmd.ErrOrStderr())
}

// runInitWith is the dependency-injected core of init. dir is the directory the
// config is written to; in/out are the confirmation prompt's I/O streams so
// tests can drive the --force overwrite path without a real TTY.
func runInitWith(dir string, force, yes bool, in io.Reader, out io.Writer) error {
	path := filepath.Join(dir, configFileName)

	if _, err := os.Stat(path); err == nil {
		if !force {
			return fmt.Errorf("%s already exists; pass --force to overwrite", configFileName)
		}
		if !yes && !confirmOverwrite(in, out) {
			fmt.Fprintln(out, "aborted; existing file left unchanged")
			return nil
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", path, err)
	}

	verify := detectVerifyCommands(dir)
	repo, hasRepo := detectGitHubRepo(dir)
	content := renderConfigTemplate(verify, repo, hasRepo)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", configFileName, err)
	}
	fmt.Fprintf(out, "created %s\n", path)

	// When a GitHub repo is detected, scaffold a sensor in the current model
	// (sensors/<name>.toml). The deprecated [integrations.github] block is no
	// longer written — it failed `quasar doctor` ("no Sensor registered").
	sensorCreated := false
	if hasRepo {
		created, err := scaffoldGitHubSensor(dir, repo)
		if err != nil {
			return err
		}
		sensorCreated = created
		if created {
			fmt.Fprintf(out, "created %s\n", filepath.Join("sensors", "github.toml"))
		}
	}

	reportDetections(out, verify, repo, hasRepo, sensorCreated)
	fmt.Fprintf(out, "edit %s and sensors/ as needed, then run `quasar doctor` and `quasar lint`\n", configFileName)
	return nil
}

// scaffoldGitHubSensor writes <dir>/sensors/github.toml configuring the
// github_issues sensor for repo, unless the file already exists (it is never
// clobbered). Returns whether it created the file. A token is never written:
// with none set the sensor uses the operator's authenticated `gh`.
func scaffoldGitHubSensor(dir, repo string) (bool, error) {
	path := filepath.Join(dir, "sensors", "github.toml")
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("create sensors dir: %w", err)
	}

	var b strings.Builder
	b.WriteString("# GitHub issues sensor — matching open issues become draft nebulas in\n")
	b.WriteString("# the awaiting-approval lane. Validate with `quasar lint`.\n")
	b.WriteString("name = \"github\"\n")
	b.WriteString("type = \"github_issues\"\n\n")
	b.WriteString("[config]\n")
	fmt.Fprintf(&b, "repo = %q\n", repo)
	b.WriteString("# Tokens are NEVER inlined. With none set, the sensor uses your `gh` auth.\n")
	b.WriteString("# To use a PAT instead, set token_env to the env var holding it:\n")
	b.WriteString("# token_env = \"GITHUB_TOKEN\"\n")
	b.WriteString("# Narrow to issues carrying every listed label:\n")
	b.WriteString("# labels = [\"quasar\"]\n")

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

// confirmOverwrite prompts on out and reads a yes/no answer from in. Any answer
// other than "y"/"yes" (case-insensitive) is treated as a decline.
func confirmOverwrite(in io.Reader, out io.Writer) bool {
	fmt.Fprintf(out, "%s already exists. Overwrite? [y/N]: ", configFileName)
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes"
}

// reportDetections prints a short summary of what init auto-detected so the user
// knows which sections were pre-populated versus left as placeholders.
func reportDetections(out io.Writer, v verifyCommands, repo string, hasRepo, sensorCreated bool) {
	if v.Lang != "" {
		fmt.Fprintf(out, "  detected language: %s ([verify] populated)\n", v.Lang)
	} else {
		fmt.Fprintln(out, "  language: not detected ([verify] left commented out)")
	}
	switch {
	case hasRepo && sensorCreated:
		fmt.Fprintf(out, "  detected GitHub repo: %s (scaffolded sensors/github.toml)\n", repo)
	case hasRepo:
		fmt.Fprintf(out, "  detected GitHub repo: %s (sensors/github.toml already exists; left as-is)\n", repo)
	default:
		fmt.Fprintln(out, "  GitHub remote: not detected (no sensor scaffolded)")
	}
}

// renderConfigTemplate builds the .quasar.yaml body. Detected sections are
// written live; undetected ones are emitted as commented placeholders so the
// file always documents every section. Secrets are never written: the
// integration block references token_env / token_file only.
func renderConfigTemplate(v verifyCommands, repo string, hasRepo bool) string {
	var b strings.Builder

	b.WriteString("# Quasar configuration — generated by `quasar init`.\n")
	b.WriteString("# Edit the placeholders below, then run `quasar doctor` to validate.\n\n")

	b.WriteString("# Path to the claude CLI binary.\n")
	b.WriteString("claude_path: claude\n\n")

	b.WriteString("# Safety limits for the coder-reviewer loop.\n")
	b.WriteString("max_review_cycles: 3\n")
	b.WriteString("max_budget_usd: 5.0\n\n")

	b.WriteString("# Claude model (empty = CLI default).\n")
	b.WriteString("model: \"\"\n\n")

	writeVerifySection(&b, v)
	writeSensorsSection(&b, repo, hasRepo)
	writeForgeSection(&b)
	writePreCommitSection(&b)

	return b.String()
}

// writeVerifySection emits [verify]. Detected commands are written live; when no
// language was detected the whole block is commented out with an example.
func writeVerifySection(b *strings.Builder, v verifyCommands) {
	b.WriteString("# Verification gates (reserved; enforced in a later release).\n")
	if v.Lang == "" {
		b.WriteString("# Detected no known project markers; uncomment and fill in:\n")
		b.WriteString("# verify:\n")
		b.WriteString("#   test: \"go test ./...\"\n")
		b.WriteString("#   lint: \"go vet ./...\"\n")
		b.WriteString("#   build: \"go build ./...\"\n\n")
		return
	}
	fmt.Fprintf(b, "# Detected %s.\n", v.Lang)
	b.WriteString("verify:\n")
	fmt.Fprintf(b, "  test: %q\n", v.Test)
	fmt.Fprintf(b, "  lint: %q\n", v.Lint)
	fmt.Fprintf(b, "  build: %q\n\n", v.Build)
}

// writeSensorsSection documents that sensors are configured as TOML files under
// sensors/ — NOT in this file (the old [integrations.*] block is gone). When a
// GitHub repo was detected, init also scaffolds sensors/github.toml; otherwise
// this shows how to add one. No `integrations:`/`sensors:` YAML key is emitted,
// so `quasar doctor` no longer fails on a phantom sensor lookup.
func writeSensorsSection(b *strings.Builder, repo string, hasRepo bool) {
	b.WriteString("# Sensors (external work sources) are configured as TOML files under\n")
	b.WriteString("# sensors/<name>.toml — not in this file. Each sets a `type` (e.g.\n")
	b.WriteString("# \"github_issues\") and a [config] block; tokens use token_env or defer\n")
	b.WriteString("# to `gh` auth. Validate them with `quasar lint`.\n")
	if hasRepo {
		fmt.Fprintf(b, "# A sensors/github.toml watching %s was scaffolded for you.\n\n", repo)
		return
	}
	b.WriteString("# No github.com origin detected — add one like:\n")
	b.WriteString("#   # sensors/github.toml\n")
	b.WriteString("#   name = \"github\"\n")
	b.WriteString("#   type = \"github_issues\"\n")
	b.WriteString("#   [config]\n")
	b.WriteString("#   repo = \"owner/repo\"\n\n")
}

// writeForgeSection emits the reserved [forge.github] placeholder. The forge
// (PR-creation) surface is reserved; this documents the schema without enabling
// anything in the current release.
func writeForgeSection(b *strings.Builder) {
	b.WriteString("# Forge (PR creation) — reserved for a later release.\n")
	b.WriteString("# forge:\n")
	b.WriteString("#   github:\n")
	b.WriteString("#     repo: \"owner/repo\"\n")
	b.WriteString("#     token_env: \"GITHUB_TOKEN\"\n\n")
}

// writePreCommitSection emits [pre_commit] with an empty command list and a
// commented example of common formatters for the user to uncomment.
func writePreCommitSection(b *strings.Builder) {
	b.WriteString("# Commands run in the worktree before every Quasar commit.\n")
	b.WriteString("pre_commit:\n")
	b.WriteString("  commands: []\n")
	b.WriteString("  fail_on_error: true\n")
	b.WriteString("  # Uncomment the formatters relevant to your project:\n")
	b.WriteString("  # commands:\n")
	b.WriteString("  #   - \"gofmt -w .\"\n")
	b.WriteString("  #   - \"tofu fmt\"\n")
	b.WriteString("  #   - \"prettier --write .\"\n")
	b.WriteString("  #   - \"ruff format .\"\n")
	b.WriteString("  #   - \"cargo fmt\"\n")
}
