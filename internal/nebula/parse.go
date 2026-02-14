package nebula

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// Load reads a nebula directory, parsing nebula.toml and all *.md task files.
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

	var tasks []TaskSpec
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}

		task, err := parseTaskFile(filepath.Join(dir, e.Name()), manifest.Defaults)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", e.Name(), err)
		}
		task.SourceFile = e.Name()
		tasks = append(tasks, task)
	}

	return &Nebula{
		Dir:      dir,
		Manifest: manifest,
		Tasks:    tasks,
	}, nil
}

// parseTaskFile reads a markdown file with +++ TOML frontmatter.
func parseTaskFile(path string, defaults Defaults) (TaskSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return TaskSpec{}, err
	}

	content := string(data)
	frontmatter, body, err := splitFrontmatter(content)
	if err != nil {
		return TaskSpec{}, err
	}

	var task TaskSpec
	if err := toml.Unmarshal([]byte(frontmatter), &task); err != nil {
		return TaskSpec{}, fmt.Errorf("parsing TOML frontmatter: %w", err)
	}

	task.Body = strings.TrimSpace(body)

	// Apply defaults for zero-valued fields.
	if task.Type == "" {
		task.Type = defaults.Type
	}
	if task.Priority == 0 {
		task.Priority = defaults.Priority
	}
	if len(task.Labels) == 0 && len(defaults.Labels) > 0 {
		task.Labels = make([]string, len(defaults.Labels))
		copy(task.Labels, defaults.Labels)
	}
	if task.Assignee == "" {
		task.Assignee = defaults.Assignee
	}

	return task, nil
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
