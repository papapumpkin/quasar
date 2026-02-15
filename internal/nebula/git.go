package nebula

// InterventionFileNames returns the filenames that should be excluded from
// git commits in a nebula directory. These are the PAUSE and STOP intervention
// files used for human control of nebula execution.
func InterventionFileNames() []string {
	names := make([]string, 0, len(interventionFiles))
	for name := range interventionFiles {
		names = append(names, name)
	}
	return names
}

// GitExcludePatterns returns gitignore-style patterns for intervention files.
// Callers can use these patterns with git add --exclude or .gitignore entries.
func GitExcludePatterns() []string {
	names := InterventionFileNames()
	patterns := make([]string, len(names))
	for i, name := range names {
		patterns[i] = name
	}
	return patterns
}
