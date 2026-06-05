package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/papapumpkin/quasar/internal/config"
	"github.com/papapumpkin/quasar/internal/sensors"

	// Blank import for its side effect only: the github sensor registers itself
	// with the sensor registry from package init(). doctor reaches it through
	// sensors.Default(), never via a direct type reference.
	_ "github.com/papapumpkin/quasar/internal/sensors/github"
)

// checkStatus is the outcome of a single doctor check.
type checkStatus string

const (
	statusOK   checkStatus = "ok"   // green; check passed
	statusWarn checkStatus = "warn" // yellow; non-fatal, surfaced as guidance
	statusFail checkStatus = "fail" // red; counts toward a non-zero exit
)

// checkResult is one line of the doctor report. The JSON encoding of a slice of
// these is the stable contract `quasar doctor --json` exposes to CI.
type checkResult struct {
	Name    string      `json:"name"`
	Status  checkStatus `json:"status"`
	Message string      `json:"message"`
}

// doctorDeps bundles doctor's collaborators so gatherChecks is unit-testable
// with fakes for config loading, git detection, PATH lookups, the sensor
// registry, and secret resolution.
type doctorDeps struct {
	workDir       string
	loadConfig    func() (config.Config, error)
	findGitRoot   func(dir string) (string, bool)
	originURL     func(dir string) string
	lookPath      func(file string) (string, error)
	buildSource   func(name string, section map[string]any) (sensors.Sensor, error)
	resolveSecret func(spec sensors.SecretSpec) (string, error)
}

// productionDoctorDeps wires the real collaborators. buildSource instantiates
// the named sensor from the registry and runs its Configure step so that
// missing binaries or unreadable credentials surface as a failed check.
func productionDoctorDeps(workDir string) doctorDeps {
	return doctorDeps{
		workDir:     workDir,
		loadConfig:  config.Load,
		findGitRoot: findGitRoot,
		originURL:   detectOriginURL,
		lookPath:    exec.LookPath,
		buildSource: func(name string, section map[string]any) (sensors.Sensor, error) {
			s, err := sensors.Default().BuildSensor(name)
			if err != nil {
				return nil, err
			}
			if err := s.Configure(section, sensors.OSSecretResolver{}); err != nil {
				return nil, err
			}
			return s, nil
		},
		resolveSecret: sensors.ResolveSecret,
	}
}

// doctorCmd diagnoses the local Quasar configuration and environment.
var doctorCmd = &cobra.Command{
	Use:           "doctor",
	Short:         "Diagnose configuration, integrations, and git setup",
	Long:          "Run a series of checks over the worktree, config, integrations, credentials, and pre-commit commands, then print a report. Exits non-zero if any required check fails.",
	RunE:          runDoctor,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	doctorCmd.Flags().Bool("json", false, "Emit the report as JSON on stdout")
	rootCmd.AddCommand(doctorCmd)
}

// runDoctor is the cobra adapter shared by `quasar doctor` and the deprecated
// `quasar validate` alias. It gathers checks, renders them in the requested
// format, and returns a code-1 exit error when any check failed.
func runDoctor(cmd *cobra.Command, _ []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	asJSON, _ := cmd.Flags().GetBool("json")

	results := gatherChecks(productionDoctorDeps(wd))

	if asJSON {
		if err := writeJSONReport(cmd.OutOrStdout(), results); err != nil {
			return err
		}
	} else {
		writeTextReport(cmd.ErrOrStderr(), results)
	}

	if anyFailed(results) {
		return newExitError(1, errors.New("doctor: one or more checks failed"))
	}
	return nil
}

// gatherChecks runs every diagnostic in report order and returns the results.
// Config-load failure short-circuits the config-dependent checks since there is
// nothing meaningful to inspect without a loaded config.
func gatherChecks(deps doctorDeps) []checkResult {
	results := []checkResult{checkGit(deps)}

	cfg, err := deps.loadConfig()
	if err != nil {
		results = append(results, checkResult{"config", statusFail, fmt.Sprintf("failed to load %s: %v", configFileName, err)})
		return results
	}
	results = append(results, checkResult{"config", statusOK, fmt.Sprintf("%s loaded", configFileName)})

	results = append(results, checkIntegrations(deps, cfg)...)
	results = append(results, checkGH(deps, cfg))
	results = append(results, checkPreCommit(deps, cfg)...)
	results = append(results, checkVerify(cfg)...)
	return results
}

// checkGit verifies the working directory is inside a git worktree and reports
// the origin remote when one is configured.
func checkGit(deps doctorDeps) checkResult {
	root, ok := deps.findGitRoot(deps.workDir)
	if !ok {
		return checkResult{"git", statusFail, "not inside a git worktree; run `git init` first"}
	}
	if origin := deps.originURL(root); origin != "" {
		return checkResult{"git", statusOK, fmt.Sprintf("worktree at %s (origin: %s)", root, origin)}
	}
	return checkResult{"git", statusOK, fmt.Sprintf("worktree at %s (no origin remote)", root)}
}

// checkIntegrations attempts to construct each configured integration via the
// registry and resolves its credentials. It emits two results per integration:
// one for construction/registration and one for credential resolution.
func checkIntegrations(deps doctorDeps, cfg config.Config) []checkResult {
	if len(cfg.IntegrationSections) == 0 {
		return []checkResult{{"integrations", statusWarn, "no [integrations.*] configured"}}
	}

	names := make([]string, 0, len(cfg.IntegrationSections))
	for name := range cfg.IntegrationSections {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic report order

	var results []checkResult
	for _, name := range names {
		section := cfg.IntegrationSections[name]
		label := "integrations." + name
		if _, err := deps.buildSource(name, section); err != nil {
			results = append(results, checkResult{label, statusFail, err.Error()})
			continue
		}
		results = append(results, checkResult{label, statusOK, "registered and constructable"})
		results = append(results, checkCredentials(deps, name, section))
	}
	return results
}

// checkCredentials resolves the token_env/token_file for one integration without
// ever printing the secret value. A configured-but-broken source (e.g. a
// token_file with bad permissions) fails; an empty result is reported as a
// warning since adapters like GitHub fall back to their own auth chain.
func checkCredentials(deps doctorDeps, name string, section map[string]any) checkResult {
	label := "integrations." + name + ".credentials"
	spec := sensors.SecretSpec{
		Env:  stringFromSection(section, "token_env"),
		File: stringFromSection(section, "token_file"),
	}
	if spec.Env == "" && spec.File == "" {
		return checkResult{label, statusWarn, "no token_env/token_file set; deferring to the adapter's own auth"}
	}
	value, err := deps.resolveSecret(spec)
	if err != nil {
		return checkResult{label, statusFail, err.Error()}
	}
	if value == "" {
		return checkResult{label, statusWarn, "configured source resolved to an empty value; deferring to the adapter's own auth"}
	}
	return checkResult{label, statusOK, credentialSource(spec)}
}

// credentialSource describes which source supplied a resolved credential,
// without revealing the value. File takes precedence (matching ResolveSecret).
func credentialSource(spec sensors.SecretSpec) string {
	if spec.File != "" {
		return fmt.Sprintf("resolved from token_file %s", spec.File)
	}
	return fmt.Sprintf("resolved from token_env %s", spec.Env)
}

// checkGH reports whether the gh CLI is on PATH, but only when a GitHub
// integration is configured. It uses LookPath rather than invoking gh: gh is a
// forge-specific binary whose use Quasar confines to the github adapter.
func checkGH(deps doctorDeps, cfg config.Config) checkResult {
	if _, configured := cfg.IntegrationSections["github"]; !configured {
		return checkResult{"gh", statusOK, "not required (no github integration configured)"}
	}
	path, err := deps.lookPath("gh")
	if err != nil {
		return checkResult{"gh", statusFail, "gh CLI not found on PATH; install it from https://cli.github.com/"}
	}
	return checkResult{"gh", statusOK, "found at " + path}
}

// checkPreCommit verifies the first token of each configured pre-commit command
// resolves on PATH. An empty command list is a passing no-op.
func checkPreCommit(deps doctorDeps, cfg config.Config) []checkResult {
	cmds := cfg.PreCommit.Commands
	if len(cmds) == 0 {
		return []checkResult{{"pre_commit", statusOK, "no pre-commit commands configured"}}
	}
	var results []checkResult
	for i, command := range cmds {
		label := fmt.Sprintf("pre_commit[%d]", i)
		bin := firstToken(command)
		if bin == "" {
			results = append(results, checkResult{label, statusFail, "empty command"})
			continue
		}
		if _, err := deps.lookPath(bin); err != nil {
			results = append(results, checkResult{label, statusFail, fmt.Sprintf("%q not found on PATH", bin)})
			continue
		}
		results = append(results, checkResult{label, statusOK, fmt.Sprintf("%q on PATH", bin)})
	}
	return results
}

// checkVerify reports which [verify] gates are configured. Empty gates are a
// warning, not a failure: verify is optional and enforced in a later release.
func checkVerify(cfg config.Config) []checkResult {
	gates := []struct {
		name string
		cmd  string
	}{
		{"verify.test", cfg.Verify.Test},
		{"verify.lint", cfg.Verify.Lint},
		{"verify.build", cfg.Verify.Build},
	}
	results := make([]checkResult, 0, len(gates))
	for _, g := range gates {
		if g.cmd == "" {
			results = append(results, checkResult{g.name, statusWarn, "not configured (optional; enables future gates)"})
			continue
		}
		results = append(results, checkResult{g.name, statusOK, g.cmd})
	}
	return results
}

// writeTextReport renders results as a glyph-prefixed report plus an overall
// summary line.
func writeTextReport(w io.Writer, results []checkResult) {
	for _, r := range results {
		fmt.Fprintf(w, "%s %s: %s\n", glyph(r.Status), r.Name, r.Message)
	}
	if anyFailed(results) {
		fmt.Fprintf(w, "%s overall: checks failed\n", glyph(statusFail))
	} else {
		fmt.Fprintf(w, "%s overall: ready\n", glyph(statusOK))
	}
}

// writeJSONReport encodes results as a JSON array on w.
func writeJSONReport(w io.Writer, results []checkResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(results); err != nil {
		return fmt.Errorf("encode doctor report: %w", err)
	}
	return nil
}

// glyph maps a status to its report symbol.
func glyph(s checkStatus) string {
	switch s {
	case statusOK:
		return "✓"
	case statusWarn:
		return "!"
	default:
		return "✗"
	}
}

// anyFailed reports whether any result has statusFail.
func anyFailed(results []checkResult) bool {
	for _, r := range results {
		if r.Status == statusFail {
			return true
		}
	}
	return false
}

// firstToken returns the first whitespace-delimited token of a command string,
// i.e. the executable name. It returns "" for a blank command.
func firstToken(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// stringFromSection reads a string value from a config section map, returning ""
// when the key is absent or not a string.
func stringFromSection(section map[string]any, key string) string {
	if section == nil {
		return ""
	}
	if v, ok := section[key].(string); ok {
		return v
	}
	return ""
}
