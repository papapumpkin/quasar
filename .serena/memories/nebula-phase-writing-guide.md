# Nebula Phase Writing Guide for Landing Page Feature

Based on the comprehensive Quasar TUI & nebula system architecture, here's everything needed to write precise phase specs for a landing page feature.

---

## Quick Phase Anatomy

Every phase file is named `NN-phase-id.md` where NN is a sort order prefix (01, 02, 03...).

```markdown
+++
id = "unique-phase-id"
title = "Short human-readable title"
type = "task|bug|feature"
priority = 1-5                           # 1 = highest
depends_on = ["other-phase-id"]          # Can be empty
labels = ["label1", "label2"]            # Inherit from [defaults] if empty
assignee = ""                            # Inherit from [defaults] if empty
max_review_cycles = 5                    # 0 = use nebula default
max_budget_usd = 25.0                    # 0 = use nebula default
model = ""                               # "" = use nebula default
gate = "trust|review|approve|watch"      # "" = inherit from nebula
blocks = []                              # Reverse deps: phase-id will depend on this
scope = ["src/landing/**", "*.html"]     # File glob patterns this phase owns
allow_scope_overlap = false              # Block overlap violations
+++

## Problem

2-3 paragraphs describing:
- What the current state is
- What problem that creates
- Why it matters

## Solution

Detailed solution including:
- Step-by-step implementation approach
- Code snippets if helpful (pseudo-code or specific examples)
- Any architectural considerations
- Testing approach hints

## Files

List of files to modify with brief descriptions:
- `src/landing/index.html` — Main landing page template
- `src/landing/styles.css` — Landing page styles
- `internal/tui/splash.go` — Splash animation (if modifying)

## Acceptance Criteria

Checklist of must-pass items:
- [ ] Landing page renders on /
- [ ] Mobile responsive (viewport >= 320px)
- [ ] `go build` and `go vet ./...` pass
- [ ] All existing tests pass
- [ ] New code has test coverage
```

---

## Landing Page Feature Example Phase

Here's a concrete example demonstrating the structure for a landing page feature in Quasar:

```markdown
+++
id = "landing-hero-section"
title = "Implement hero section with CTA buttons"
type = "feature"
priority = 2
depends_on = ["landing-base-layout"]
labels = ["quasar", "ui", "frontend"]
max_review_cycles = 3
max_budget_usd = 20.0
+++

## Problem

The Quasar landing page currently has only a basic header. It needs a prominent hero section that:
- Catches user attention with large typography and compelling imagery
- Clearly communicates the value proposition ("Dual-agent coder-reviewer loop")
- Provides clear call-to-action buttons (GitHub link, try demo, read docs)
- Converts visitors into users/contributors

Without this, visitors land on a sparse page with no clear next steps.

## Solution

### 1. Hero Layout Structure

Create a full-viewport hero section with:
- Background gradient (galactic theme: deep blue to purple)
- Center-aligned text block with:
  - Tagline (e.g., "AI-Powered Code Review")
  - Main heading (h1): "Quasar: Dual-Agent Code Coordination"
  - Subheading: Brief value prop (1-2 sentences)
  - CTA button group (GitHub, Try Demo, Documentation)
- Optional subtle animated accent (could use splash animation components)

### 2. Responsive Design

- Desktop (≥1024px): Hero full viewport, buttons side-by-side
- Tablet (768-1023px): Reduced padding, single button per row
- Mobile (≤767px): Stacked layout, full-width buttons

### 3. Color Scheme

Use existing Quasar galactic palette:
- Background: Linear gradient #0a0e27 → #1a1a3e
- Buttons: #79C0FF (stellar blue) with hover #5a9fd4
- Text: #e8e8e8 (off-white)
- Accent: #00E676 (success green) for emphasis

### 4. Typography

- Heading (h1): 48px bold, letter-spacing +2%
- Subheading (h2): 24px, lighter weight
- Button text: 16px, semi-bold, all-caps

## Files

- `docs/index.html` — Add hero section HTML (or `src/landing/index.html` if static site)
- `docs/styles.css` — Add hero section styles (or separate `landing.css`)
- `docs/landing.js` — (Optional) Button event handlers if needed
- Tests: Add tests in `docs/landing_test.go` if backend-rendered, or browser tests if client-side

## Implementation Notes

This example would be broken down into phases:
1. `landing-base-layout` — Main page template and global styles
2. `landing-hero-section` (this) — Hero component
3. `landing-features-section` — Feature cards below hero
4. `landing-cta-conversion` — Optimize conversion metrics

## Acceptance Criteria

- [ ] Hero section renders at full viewport height on desktop
- [ ] Background gradient matches galactic color scheme
- [ ] Heading and subheading text centered and readable
- [ ] CTA buttons clickable and properly linked (GitHub, try demo, docs)
- [ ] Responsive on mobile (≤767px), tablet (768-1023px), desktop (≥1024px)
- [ ] No text overflow or layout shifts
- [ ] Lighthouse accessibility score ≥90
- [ ] All CSS validates with no errors
- [ ] (If applicable) Unit tests for JS event handlers pass
```

---

## Key Metadata Fields Explained

### `id`
- Must be unique within the nebula
- Used for dependency references, phase file matching, bead IDs
- Format: kebab-case, descriptive (not "phase-1", use "landing-hero-section")
- Max length: ~50 chars

### `type`
- `task` — General work item
- `bug` — Fix an issue
- `feature` — New capability
- Inherited from `[defaults]` if omitted

### `priority`
- Integer, 1 = highest
- Defaults to 2 if not set
- Useful for planning waves and parallelism

### `depends_on`
- List of phase IDs that must complete first
- Prevents cycles (validator checks)
- Empty array or omit if independent
- Affects execution wave ordering

### `max_review_cycles`
- 0 or omitted = use nebula default (from `[execution]`)
- Override for phases needing extra iteration
- Typical range: 2-5

### `max_budget_usd`
- 0 or omitted = use nebula default
- Override for expensive phases (e.g., heavy refactoring)
- Typical range: $10-50

### `model`
- "" or omitted = use nebula default (usually claude-opus-4)
- Override to use cheaper model (e.g., "claude-3-5-sonnet") for simpler phases

### `gate`
- `trust` — Fully autonomous, no human gate
- `review` — Pause after phase, show diff, need approval
- `approve` — Gate the plan first AND each phase
- `watch` — Stream diffs in real-time, no blocking
- "" or omitted = inherit from `[execution]`

### `blocks`
- Reverse dependency: "if phase X blocks phase Y, then Y depends_on X"
- Useful for expressing "downstream phases need this first"

### `scope`
- Glob patterns for files this phase "owns"
- Used for scope serialization (prevent parallel overlaps)
- Example: `["src/landing/**", "docs/**", "*.md"]`

### `allow_scope_overlap`
- `false` (default) = error if this phase's scope overlaps with another
- `true` = allow overlap (concurrent execution)
- Set to `true` if phase modifies shared files safely

---

## Nebula Manifest Guide (nebula.toml)

Every nebula starts with a manifest. Here's the template for a landing page nebula:

```toml
[nebula]
name = "landing-page-mvp"
description = "Build a compelling landing page to drive Quasar adoption and GitHub stars"

[defaults]
type = "feature"
priority = 2
labels = ["quasar", "landing", "frontend"]
assignee = ""

[execution]
max_workers = 2
max_review_cycles = 4
max_budget_usd = 120.0
model = ""
gate = "review"

[context]
repo = "github.com/papapumpkin/quasar"
working_dir = "."
goals = [
    "Create a visually compelling landing page that communicates Quasar's value",
    "Drive GitHub stars and community interest",
    "Provide clear conversion paths (GitHub, docs, demo)",
    "Showcase the TUI in action (animated GIF or embedded demo)",
]
constraints = [
    "Use only HTML, CSS, and vanilla JS (no frameworks)",
    "Maintain Quasar's galactic color theme",
    "Mobile-first responsive design",
    "Must pass Lighthouse accessibility audit",
    "Static site (no backend rendering required)",
    "All existing tests must pass",
]

[dependencies]
requires_beads = []
requires_nebulae = []
```

---

## Phase Ordering & Dependencies

For a landing page feature, typical phase flow:

```
Phase 1: landing-base-layout
  ↓
Phase 2: landing-hero-section (depends_on = ["landing-base-layout"])
  ↓
Phase 3: landing-features-section (depends_on = ["landing-base-layout"])
  ↓
Phase 4: landing-cta-conversion (depends_on = ["landing-hero-section", "landing-features-section"])
  ↓
Phase 5: landing-mobile-responsive (depends_on = ["landing-cta-conversion"])
  ↓
Phase 6: landing-performance (depends_on = ["landing-mobile-responsive"])
```

**Wave Execution**:
- Wave 1: Base layout (serial)
- Wave 2: Hero + Features (parallel, no dependencies on each other)
- Wave 3: CTA (depends on both hero/features)
- Wave 4: Mobile + Performance (parallel if no scope overlap)

---

## Problem & Solution Templates

### Problem Template
```
Current state:
- [Describe what exists today]
- [Describe user pain point]
- [Why it matters]

Desired state:
- [What should happen]
- [Benefits]
```

### Solution Template
```
## Implementation Approach

1. **Component Design**
   - Describe structure/hierarchy
   - Reference design patterns

2. **Styling & Layout**
   - CSS grid/flexbox approach
   - Responsive breakpoints
   - Color/typography strategy

3. **Functionality**
   - Event handlers (if JS needed)
   - User interactions
   - Accessibility considerations

4. **Testing**
   - Unit tests (if applicable)
   - Visual regression tests
   - Accessibility checks (WCAG 2.1 AA)

5. **Integration**
   - How does it fit into existing landing page
   - Build/deploy considerations
```

---

## Acceptance Criteria Best Practices

**GOOD**:
```markdown
- [ ] Hero section renders full-viewport height on desktop
- [ ] CTA buttons link to correct destinations (GitHub, docs, demo)
- [ ] Mobile layout stacks vertically on screens ≤767px
- [ ] Text contrast meets WCAG AA (4.5:1)
- [ ] `go test ./...` passes with coverage ≥80%
```

**BAD**:
```markdown
- [ ] Hero section looks good
- [ ] Responsive design works
- [ ] Tests pass
```

→ The bad version is vague and unmeasurable. The good version is testable and verifiable.

---

## Common Mistakes to Avoid

1. **Circular dependencies** ❌
   - Phase A depends_on B, Phase B depends_on A
   - Validator will catch this, but design to prevent it

2. **Overly broad phases** ❌
   - Phase: "Build entire landing page"
   - Better: Break into hero, features, CTA, etc.

3. **Missing scope definitions** ❌
   - If multiple phases touch same files, define `scope` to prevent conflicts

4. **Vague acceptance criteria** ❌
   - "Looks good" → "Lighthouse score ≥90"
   - "Works on mobile" → "Responsive at ≤767px, ≤480px breakpoints"

5. **No testing plan** ❌
   - Every phase should mention tests in solution section
   - Quasar expects test coverage

6. **Ignoring gate mode** ❌
   - `gate = "trust"` = fully autonomous (risky for large changes)
   - `gate = "review"` = safer default (pause after, show diff)
   - Choose based on risk/impact

---

## Practical Checklist Before Committing Phase Files

- [ ] Phase ID is unique and descriptive (kebab-case)
- [ ] Title is concise (< 80 chars)
- [ ] Problem section explains the "why"
- [ ] Solution section is actionable (not vague)
- [ ] Files list includes all paths to be modified
- [ ] Acceptance criteria are testable and measurable
- [ ] All `depends_on` phase IDs exist in the nebula
- [ ] No circular dependencies
- [ ] `scope` and `allow_scope_overlap` set if needed
- [ ] `max_review_cycles` and `max_budget_usd` are reasonable for phase complexity
- [ ] TOML frontmatter is valid (test with `quasar nebula validate`)

---

## Example Full Nebula

See: `/Users/aaronsalm/Documents/Computer/quasar/.nebulas/tui-refinement/`
- Manifest with 8 phases
- Phase dependencies chained correctly
- Good examples of problem/solution structure
- Real-world scope and budget decisions
