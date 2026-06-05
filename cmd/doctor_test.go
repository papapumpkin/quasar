package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/papapumpkin/quasar/internal/config"
	"github.com/papapumpkin/quasar/internal/integrations"
)

// fakeSource is a no-op TicketSource used to satisfy buildSource in tests.
type fakeSource struct{ name string }

func (f fakeSource) Name() string { return f.name }
func (f fakeSource) Fetch(context.Context, string) (*integrations.Ticket, error) {
	return nil, errors.New("not implemented")
}

// baseDoctorDeps returns deps where every check passes, for tests to mutate.
func baseDoctorDeps() doctorDeps {
	return doctorDeps{
		workDir:     "/repo",
		loadConfig:  func() (config.Config, error) { return config.Config{}, nil },
		findGitRoot: func(string) (string, bool) { return "/repo", true },
		originURL:   func(string) string { return "https://github.com/owner/repo.git" },
		lookPath:    func(file string) (string, error) { return "/usr/bin/" + file, nil },
		buildSource: func(name string, _ map[string]any) (integrations.TicketSource, error) {
			return fakeSource{name: name}, nil
		},
		resolveSecret: func(integrations.SecretSpec) (string, error) { return "tok", nil },
	}
}

// statusOf returns the status recorded for the named check, or "" if absent.
func statusOf(results []checkResult, name string) checkStatus {
	for _, r := range results {
		if r.Name == name {
			return r.Status
		}
	}
	return ""
}

func TestGatherChecks(t *testing.T) {
	t.Parallel()

	t.Run("clean setup has no failures", func(t *testing.T) {
		t.Parallel()
		results := gatherChecks(baseDoctorDeps())
		if anyFailed(results) {
			t.Errorf("expected no failures, got: %+v", results)
		}
		if statusOf(results, "git") != statusOK {
			t.Error("git check should pass")
		}
		if statusOf(results, "config") != statusOK {
			t.Error("config check should pass")
		}
	})

	t.Run("not a git worktree fails", func(t *testing.T) {
		t.Parallel()
		deps := baseDoctorDeps()
		deps.findGitRoot = func(string) (string, bool) { return "", false }
		results := gatherChecks(deps)
		if statusOf(results, "git") != statusFail {
			t.Error("git check should fail outside a worktree")
		}
		if !anyFailed(results) {
			t.Error("overall should fail")
		}
	})

	t.Run("config load failure short-circuits", func(t *testing.T) {
		t.Parallel()
		deps := baseDoctorDeps()
		deps.loadConfig = func() (config.Config, error) { return config.Config{}, errors.New("bad yaml") }
		results := gatherChecks(deps)
		if statusOf(results, "config") != statusFail {
			t.Error("config check should fail")
		}
		// Only git + config checks should be present.
		if len(results) != 2 {
			t.Errorf("expected short-circuit to 2 checks, got %d: %+v", len(results), results)
		}
	})

	t.Run("configured integration with resolvable credentials passes", func(t *testing.T) {
		t.Parallel()
		deps := baseDoctorDeps()
		deps.loadConfig = func() (config.Config, error) {
			return config.Config{IntegrationSections: map[string]map[string]any{
				"github": {"repo": "owner/repo", "token_env": "GITHUB_TOKEN"},
			}}, nil
		}
		results := gatherChecks(deps)
		if statusOf(results, "integrations.github") != statusOK {
			t.Error("integration construction should pass")
		}
		if statusOf(results, "integrations.github.credentials") != statusOK {
			t.Error("credential resolution should pass")
		}
		if anyFailed(results) {
			t.Errorf("expected exit-clean, got: %+v", results)
		}
	})

	t.Run("credential resolution failure fails", func(t *testing.T) {
		t.Parallel()
		deps := baseDoctorDeps()
		deps.loadConfig = func() (config.Config, error) {
			return config.Config{IntegrationSections: map[string]map[string]any{
				"github": {"token_file": "/run/secrets/github_token"},
			}}, nil
		}
		deps.resolveSecret = func(integrations.SecretSpec) (string, error) {
			return "", errors.New("insecure permissions")
		}
		results := gatherChecks(deps)
		if statusOf(results, "integrations.github.credentials") != statusFail {
			t.Error("credential check should fail when resolution errors")
		}
		if !anyFailed(results) {
			t.Error("overall should fail on credential error")
		}
	})

	t.Run("unregistered integration fails", func(t *testing.T) {
		t.Parallel()
		deps := baseDoctorDeps()
		deps.loadConfig = func() (config.Config, error) {
			return config.Config{IntegrationSections: map[string]map[string]any{
				"jira": {"project": "ABC"},
			}}, nil
		}
		deps.buildSource = func(string, map[string]any) (integrations.TicketSource, error) {
			return nil, errors.New("no TicketSource registered for \"jira\"")
		}
		results := gatherChecks(deps)
		if statusOf(results, "integrations.jira") != statusFail {
			t.Error("unregistered integration should fail")
		}
	})

	t.Run("missing gh fails when github configured", func(t *testing.T) {
		t.Parallel()
		deps := baseDoctorDeps()
		deps.loadConfig = func() (config.Config, error) {
			return config.Config{IntegrationSections: map[string]map[string]any{
				"github": {"repo": "owner/repo", "token_env": "GITHUB_TOKEN"},
			}}, nil
		}
		deps.lookPath = func(file string) (string, error) {
			if file == "gh" {
				return "", errors.New("not found")
			}
			return "/usr/bin/" + file, nil
		}
		results := gatherChecks(deps)
		if statusOf(results, "gh") != statusFail {
			t.Error("gh check should fail when gh is missing and github is configured")
		}
	})

	t.Run("gh not required without github integration", func(t *testing.T) {
		t.Parallel()
		deps := baseDoctorDeps()
		deps.lookPath = func(string) (string, error) { return "", errors.New("not found") }
		results := gatherChecks(deps)
		if statusOf(results, "gh") != statusOK {
			t.Error("gh check should pass (not required) without a github integration")
		}
	})

	t.Run("pre-commit missing binary fails", func(t *testing.T) {
		t.Parallel()
		deps := baseDoctorDeps()
		deps.loadConfig = func() (config.Config, error) {
			return config.Config{PreCommit: config.PreCommitConfig{Commands: []string{"gofmt -w ."}}}, nil
		}
		deps.lookPath = func(string) (string, error) { return "", errors.New("not found") }
		results := gatherChecks(deps)
		if statusOf(results, "pre_commit[0]") != statusFail {
			t.Error("pre_commit check should fail when the binary is absent")
		}
	})

	t.Run("verify gates warn when empty", func(t *testing.T) {
		t.Parallel()
		results := gatherChecks(baseDoctorDeps())
		if statusOf(results, "verify.test") != statusWarn {
			t.Error("empty verify.test should warn, not fail")
		}
		// Warnings must not cause a non-zero exit.
		if anyFailed(results) {
			t.Error("warnings should not fail the overall check")
		}
	})

	t.Run("verify gates pass when configured", func(t *testing.T) {
		t.Parallel()
		deps := baseDoctorDeps()
		deps.loadConfig = func() (config.Config, error) {
			return config.Config{Verify: config.VerifyConfig{Test: "go test ./..."}}, nil
		}
		results := gatherChecks(deps)
		if statusOf(results, "verify.test") != statusOK {
			t.Error("configured verify.test should pass")
		}
	})
}

func TestWriteJSONReport(t *testing.T) {
	t.Parallel()

	results := []checkResult{
		{"git", statusOK, "worktree at /repo"},
		{"verify.test", statusWarn, "not configured"},
	}
	var buf bytes.Buffer
	if err := writeJSONReport(&buf, results); err != nil {
		t.Fatalf("writeJSONReport: %v", err)
	}

	var decoded []checkResult
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if len(decoded) != 2 || decoded[0].Name != "git" || decoded[1].Status != statusWarn {
		t.Errorf("round-trip mismatch: %+v", decoded)
	}
}

func TestWriteTextReport(t *testing.T) {
	t.Parallel()

	t.Run("clean report ends ready", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		writeTextReport(&buf, []checkResult{{"git", statusOK, "ok"}})
		if !bytes.Contains(buf.Bytes(), []byte("overall: ready")) {
			t.Errorf("expected ready summary, got:\n%s", buf.String())
		}
	})

	t.Run("failing report ends failed", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		writeTextReport(&buf, []checkResult{{"git", statusFail, "nope"}})
		if !bytes.Contains(buf.Bytes(), []byte("overall: checks failed")) {
			t.Errorf("expected failed summary, got:\n%s", buf.String())
		}
	})
}

// Sanity: the documented exit error carries code 1 so CI can branch on it.
func TestDoctorExitError(t *testing.T) {
	t.Parallel()
	err := newExitError(1, fmt.Errorf("x"))
	var ec *exitCodeError
	if !errors.As(err, &ec) || ec.code != 1 {
		t.Fatalf("expected exitCodeError with code 1, got %v", err)
	}
}
