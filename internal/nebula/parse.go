package nebula

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// Load reads a nebula directory, parsing nebula.toml and all *.md phase files.
func Load(dir string) (*Nebula, error) {
	manifestPath := filepath.Join(dir, "nebula.toml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoManifest
		}
		return nil, fmt.Errorf("reading nebula.toml: %w", err)
	}

	var manifest Manifest
	if err := toml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parsing nebula.toml: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading nebula directory: %w", err)
	}

	var phases []PhaseSpec
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}

		phase, err := parsePhaseFile(filepath.Join(dir, e.Name()), manifest.Defaults)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", e.Name(), err)
		}
		phase.SourceFile = e.Name()
		phases = append(phases, phase)
	}

	return &Nebula{
		Dir:      dir,
		Manifest: manifest,
		Phases:   phases,
	}, nil
}

// parsePhaseFile reads a markdown file with +++ TOML frontmatter.
func parsePhaseFile(path string, defaults Defaults) (PhaseSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PhaseSpec{}, err
	}

	content := string(data)
	frontmatter, body, err := splitFrontmatter(content)
	if err != nil {
		return PhaseSpec{}, err
	}

	var phase PhaseSpec
	if err := toml.Unmarshal([]byte(frontmatter), &phase); err != nil {
		return PhaseSpec{}, fmt.Errorf("parsing TOML frontmatter: %w", err)
	}

	phase.Body = strings.TrimSpace(body)

	// Apply defaults for zero-valued fields.
	if phase.Type == "" {
		phase.Type = defaults.Type
	}
	if phase.Priority == 0 {
		phase.Priority = defaults.Priority
	}
	if len(phase.Labels) == 0 && len(defaults.Labels) > 0 {
		phase.Labels = make([]string, len(defaults.Labels))
		copy(phase.Labels, defaults.Labels)
	}
	if phase.Assignee == "" {
		phase.Assignee = defaults.Assignee
	}

	return phase, nil
}

// splitFrontmatter splits content on +++ delimiters.
// Expected format:
//
//	+++
//	<TOML>
//	+++
//	<body>
func splitFrontmatter(content string) (string, string, error) {
	const delim = "+++"

	// Trim leading whitespace/newlines.
	content = strings.TrimLeft(content, " \t\r\n")

	if !strings.HasPrefix(content, delim) {
		return "", "", fmt.Errorf("file does not start with +++ frontmatter delimiter")
	}

	// Find closing delimiter.
	rest := content[len(delim):]
	idx := strings.Index(rest, delim)
	if idx < 0 {
		return "", "", fmt.Errorf("missing closing +++ frontmatter delimiter")
	}

	frontmatter := rest[:idx]
	body := rest[idx+len(delim):]

	return frontmatter, body, nil
}
