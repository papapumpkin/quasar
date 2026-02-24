package snapshot

import (
	"fmt"
	"strings"
)

// DefaultMaxContextTokens is the default token budget for all composed context layers.
const DefaultMaxContextTokens = 10000

// charsPerToken is the heuristic ratio used for token estimation.
// 1 token ~ 4 characters is accurate enough for budget management.
const charsPerToken = 4

// projectFraction is the share of the total budget allocated to project context.
const projectFraction = 0.40

// fabricFraction is the share of the total budget allocated to fabric state.
const fabricFraction = 0.40

// priorWorkFraction is the share of the total budget allocated to prior work.
const priorWorkFraction = 0.20

// truncatedMarker is appended when content is cut to fit the budget.
const truncatedMarker = "\n[truncated]"

// ContextBudget manages token budget allocation across context layers.
type ContextBudget struct {
	MaxTokens int // Total token budget for all context (default 10000). 0 disables injection.
}

// Compose assembles context layers within the token budget.
// Layers are prioritized in order: project (most cacheable) > fabric (most actionable) > prior work.
// If MaxTokens is 0, all context injection is disabled and an empty string is returned.
// Unused budget from one layer rolls over to the next.
func (cb *ContextBudget) Compose(project, fabric, priorWork string) string {
	maxTokens := cb.MaxTokens
	if maxTokens == 0 {
		return ""
	}

	totalChars := maxTokens * charsPerToken

	// Compute initial allocations.
	projectBudget := int(float64(totalChars) * projectFraction)
	fabricBudget := int(float64(totalChars) * fabricFraction)
	priorWorkBudget := int(float64(totalChars) * priorWorkFraction)

	var b strings.Builder

	// Layer 1: Project context (highest priority — most cacheable).
	projectOut := truncateLayer(project, projectBudget)
	unused := projectBudget - len(projectOut)

	// Roll unused budget to fabric.
	fabricBudget += unused

	// Layer 2: Fabric state (second priority — most actionable).
	fabricOut := truncateFabric(fabric, fabricBudget)
	unused = fabricBudget - len(fabricOut)

	// Roll unused budget to prior work.
	priorWorkBudget += unused

	// Layer 3: Prior work (lowest priority).
	priorWorkOut := truncateLayer(priorWork, priorWorkBudget)

	// Assemble non-empty layers with separators.
	layers := []string{}
	if projectOut != "" {
		layers = append(layers, projectOut)
	}
	if fabricOut != "" {
		layers = append(layers, fabricOut)
	}
	if priorWorkOut != "" {
		layers = append(layers, priorWorkOut)
	}

	for i, layer := range layers {
		if i > 0 {
			b.WriteString("\n\n---\n\n")
		}
		b.WriteString(layer)
	}

	return b.String()
}

// EstimateTokens returns the estimated token count for a string using the
// heuristic of 1 token per 4 characters.
func EstimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	return (len(s) + charsPerToken - 1) / charsPerToken
}

// truncateLayer truncates free-text content to fit within maxChars.
// If truncation occurs, a [truncated] marker is appended.
func truncateLayer(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	if maxChars <= len(truncatedMarker) {
		return ""
	}
	cutoff := maxChars - len(truncatedMarker)
	// Cut at the last newline before the cutoff to avoid mid-line truncation.
	if idx := strings.LastIndex(s[:cutoff], "\n"); idx > 0 {
		cutoff = idx
	}
	return s[:cutoff] + truncatedMarker
}

// truncateFabric performs section-aware truncation of fabric state content.
// It preserves all section headers and discovery content (actionable blockers),
// truncating the longest sections first.
func truncateFabric(s string, maxChars int) string {
	if len(s) <= maxChars {
		return s
	}
	if maxChars <= len(truncatedMarker) {
		return ""
	}

	// Parse into sections by splitting on markdown headers.
	sections := parseFabricSections(s)
	if len(sections) == 0 {
		return truncateLayer(s, maxChars)
	}

	// Calculate the space used by headers alone.
	headerSize := 0
	for _, sec := range sections {
		headerSize += len(sec.header) + 1 // +1 for newline
	}

	// If headers alone exceed budget, fall back to simple truncation.
	if headerSize >= maxChars {
		return truncateLayer(s, maxChars)
	}

	bodyBudget := maxChars - headerSize
	// Separators between sections: "\n\n" = 2 chars per gap.
	if gaps := len(sections) - 1; gaps > 0 {
		bodyBudget -= gaps * 2
	}

	// Distribute body budget. Discoveries get full allocation; others are fair-shared.
	type indexed struct {
		idx        int
		isDiscov   bool
		bodyLen    int
		allocation int
	}
	items := make([]indexed, len(sections))
	discovBudget := 0
	nonDiscovCount := 0
	for i, sec := range sections {
		items[i] = indexed{idx: i, isDiscov: sec.isDiscovery, bodyLen: len(sec.body)}
		if sec.isDiscovery {
			discovBudget += len(sec.body)
		} else {
			nonDiscovCount++
		}
	}

	remaining := bodyBudget - discovBudget
	if remaining < 0 {
		// Even discoveries exceed budget — truncate discoveries too.
		remaining = 0
		discovBudget = bodyBudget
	}

	// Allocate remaining budget evenly among non-discovery sections.
	if nonDiscovCount > 0 {
		perSection := remaining / nonDiscovCount
		leftover := remaining % nonDiscovCount
		j := 0
		for i := range items {
			if items[i].isDiscov {
				items[i].allocation = items[i].bodyLen
				continue
			}
			items[i].allocation = perSection
			if j < leftover {
				items[i].allocation++
			}
			j++
		}
	} else {
		// All sections are discoveries — share discovBudget evenly.
		perSection := discovBudget / len(items)
		leftover := discovBudget % len(items)
		for i := range items {
			items[i].allocation = perSection
			if i < leftover {
				items[i].allocation++
			}
		}
	}

	// Build output with truncated sections.
	var b strings.Builder
	for i, item := range items {
		sec := sections[item.idx]
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(sec.header)
		b.WriteString("\n")
		body := sec.body
		if len(body) > item.allocation {
			body = truncateSectionBody(body, item.allocation)
		}
		b.WriteString(body)
	}

	result := b.String()
	if len(result) > maxChars {
		return truncateLayer(result, maxChars)
	}
	return result
}

// fabricSection represents a parsed section of fabric state markdown.
type fabricSection struct {
	header      string // e.g., "### Entanglements"
	body        string // content under the header
	isDiscovery bool   // true for discovery sections (preserved during truncation)
}

// parseFabricSections splits fabric state markdown into sections by ## or ### headers.
func parseFabricSections(s string) []fabricSection {
	lines := strings.Split(s, "\n")
	var sections []fabricSection
	var current *fabricSection

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") {
			if current != nil {
				current.body = strings.TrimRight(current.body, "\n")
				sections = append(sections, *current)
			}
			isDiscov := strings.Contains(strings.ToLower(line), "discover")
			current = &fabricSection{header: line, isDiscovery: isDiscov}
		} else if current != nil {
			if current.body != "" || line != "" {
				current.body += line + "\n"
			}
		}
	}
	if current != nil {
		current.body = strings.TrimRight(current.body, "\n")
		sections = append(sections, *current)
	}

	return sections
}

// truncateSectionBody truncates a section body, counting remaining items.
func truncateSectionBody(body string, maxChars int) string {
	lines := strings.Split(body, "\n")
	var b strings.Builder
	kept := 0
	total := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			total++
		}
	}

	for _, line := range lines {
		candidate := line + "\n"
		if b.Len()+len(candidate)+40 > maxChars { // 40 = room for omitted marker
			break
		}
		b.WriteString(candidate)
		if strings.TrimSpace(line) != "" {
			kept++
		}
	}

	omitted := total - kept
	if omitted > 0 {
		fmt.Fprintf(&b, "... and %d more items omitted", omitted)
	}

	result := b.String()
	if len(result) > maxChars {
		return result[:maxChars]
	}
	return result
}
