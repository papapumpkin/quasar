package artifacts

import (
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"
)

// Embedded artifact directory names. They double as the per-repo override
// directory names and as the type discriminator: a file is a star because it
// lives in stars/, a constellation because it lives in constellations/.
const (
	dirConstellations = "constellations"
	dirStars          = "stars"
	dirSkills         = "skills"

	extTOML = ".toml"
	extMD   = ".md"

	embeddedRoot = "defaults"
)

// Loader discovers, parses, and resolves artifact files for a single repo.
// Per-repo override files take precedence over the embedded defaults, and skill
// references on a star are resolved transitively at load time so the runtime
// receives a flat, fully-composed Star. Constellation expressions are compiled
// once here, not on every evaluation.
type Loader struct {
	resolver PathResolver
	builtins fs.FS

	// Strict, when true, rejects TOML documents that carry keys absent from the
	// target schema (go-toml DisallowUnknownFields). `quasar lint --strict`
	// enables it; normal loading leaves it off so hand-authored files with
	// forward-compatible extras still load.
	Strict bool
}

// New returns a Loader backed by the given resolver and the embedded defaults.
func New(r PathResolver) *Loader {
	return &Loader{resolver: r, builtins: DefaultsFS}
}

// LoadStar loads the named star, preferring <repo>/stars/<name>.md over the
// embedded default. Every skill the star references is resolved: its prompt
// fragment is prepended to the star body and its tools_add are unioned into the
// star's allowed tools.
func (l *Loader) LoadStar(name string) (*Star, error) {
	data, src, err := l.read(l.resolver.StarPath(name), dirStars, name, extMD)
	if err != nil {
		return nil, err
	}

	fm, body, fmLine, err := splitFrontmatter(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src, err)
	}

	var sf starFrontmatter
	if err := l.decodeTOML([]byte(fm), src, fmLine, &sf); err != nil {
		return nil, err
	}

	star := &Star{
		Name:          sf.Name,
		Model:         sf.Model,
		FallbackModel: sf.FallbackModel,
		Skills:        sf.Skills,
		Tools:         StarTools{Allowed: sf.Tools.Allowed, Denied: sf.Tools.Denied},
		Defaults:      StarDefaults{MaxBudgetUSD: sf.Defaults.MaxBudgetUSD, Effort: sf.Defaults.Effort},
		Prompt:        strings.TrimSpace(body),
		SourcePath:    src,
	}

	if err := l.resolveSkills(star); err != nil {
		return nil, err
	}
	return star, nil
}

// resolveSkills merges each referenced skill into the star: fragments are
// concatenated ahead of the star body (in declaration order) and tools_add are
// unioned into Allowed.
func (l *Loader) resolveSkills(star *Star) error {
	if len(star.Skills) == 0 {
		return nil
	}

	var fragments []string
	for _, name := range star.Skills {
		skill, err := l.LoadSkill(name)
		if err != nil {
			return fmt.Errorf("%s: resolving skill %q: %w", star.SourcePath, name, err)
		}
		if frag := strings.TrimSpace(skill.PromptFragment); frag != "" {
			fragments = append(fragments, frag)
		}
		star.Tools.Allowed = unionStrings(star.Tools.Allowed, skill.ToolsAdd)
	}

	parts := fragments
	if star.Prompt != "" {
		parts = append(parts, star.Prompt)
	}
	star.Prompt = strings.Join(parts, "\n\n")
	return nil
}

// LoadSkill loads the named skill, preferring <repo>/skills/<name>.md over the
// embedded default.
func (l *Loader) LoadSkill(name string) (*Skill, error) {
	data, src, err := l.read(l.resolver.SkillPath(name), dirSkills, name, extMD)
	if err != nil {
		return nil, err
	}

	fm, body, fmLine, err := splitFrontmatter(string(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src, err)
	}

	var sf skillFrontmatter
	if err := l.decodeTOML([]byte(fm), src, fmLine, &sf); err != nil {
		return nil, err
	}

	return &Skill{
		Name:           sf.Name,
		ToolsAdd:       sf.ToolsAdd,
		PromptFragment: strings.TrimSpace(body),
		SourcePath:     src,
	}, nil
}

// LoadConstellation loads the named constellation, preferring
// <repo>/constellations/<name>.toml over the embedded default. Every embedded
// expression (node inputs, edge guards, outputs) is compiled here.
func (l *Loader) LoadConstellation(name string) (*Constellation, error) {
	data, src, err := l.read(l.resolver.ConstellationPath(name), dirConstellations, name, extTOML)
	if err != nil {
		return nil, err
	}

	var cf constellationFile
	if err := l.decodeTOML(data, src, 0, &cf); err != nil {
		return nil, err
	}

	c := &Constellation{
		Name:        cf.Name,
		Description: cf.Description,
		Meta:        cf.Meta,
		SourcePath:  src,
	}

	for _, n := range cf.Nodes {
		nodeType, err := nodeTypeOf(n.Type, n.Star, n.Ref, n.Op)
		if err != nil {
			return nil, fmt.Errorf("%s: node %q: %w", src, n.ID, err)
		}
		inputs, err := compileTemplates(n.Inputs)
		if err != nil {
			return nil, fmt.Errorf("%s: node %q input: %w", src, n.ID, err)
		}
		c.Nodes = append(c.Nodes, ConstellationNode{
			ID:     n.ID,
			Type:   nodeType,
			Star:   n.Star,
			Ref:    n.Ref,
			Op:     n.Op,
			Inputs: inputs,
		})
	}

	for i, e := range cf.Edges {
		var when Expression
		if strings.TrimSpace(e.When) != "" {
			when, err = Parse(e.When)
			if err != nil {
				return nil, fmt.Errorf("%s: edge %d (%s->%s) when: %w", src, i, e.From, e.To, err)
			}
		}
		c.Edges = append(c.Edges, ConstellationEdge{From: e.From, To: e.To, When: when})
	}

	if len(cf.Outputs) > 0 {
		outputs, err := compileTemplates(cf.Outputs)
		if err != nil {
			return nil, fmt.Errorf("%s: output: %w", src, err)
		}
		c.Outputs = outputs
	}

	return c, nil
}

// LoadSensorInstance loads the per-repo sensor instance <repo>/sensors/<name>.toml.
// Sensors have no embedded default; a missing file is repos.ErrSensorNotConfigured.
// The [config] block is parsed as an opaque map and is NOT validated against any
// sensor-type schema — that is the sensor's Configure step's job.
func (l *Loader) LoadSensorInstance(name string) (*SensorInstance, error) {
	src, err := l.resolver.SensorPath(name)
	if err != nil {
		return nil, err
	}
	return l.parseSensor(src)
}

// LoadAllSensorInstances parses every <repo>/sensors/*.toml. The supervisor uses
// it at startup to learn which schedulers to spin up. A missing sensors
// directory yields an empty slice, not an error.
func (l *Loader) LoadAllSensorInstances() ([]*SensorInstance, error) {
	paths, err := l.resolver.AllSensorPaths()
	if err != nil {
		return nil, err
	}
	instances := make([]*SensorInstance, 0, len(paths))
	for _, p := range paths {
		inst, err := l.parseSensor(p)
		if err != nil {
			return nil, err
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

// parseSensor decodes a sensor instance from an on-disk TOML path.
func (l *Loader) parseSensor(src string) (*SensorInstance, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return nil, fmt.Errorf("artifacts: read sensor %q: %w", src, err)
	}

	var sf sensorFile
	if err := l.decodeTOML(data, src, 0, &sf); err != nil {
		return nil, err
	}

	if _, inline := sf.Config["token"]; inline {
		return nil, fmt.Errorf("%s: inline token in [config] is forbidden; use token_env or token_file", src)
	}

	var interval time.Duration
	if s := strings.TrimSpace(sf.PollInterval); s != "" {
		interval, err = time.ParseDuration(s)
		if err != nil {
			return nil, fmt.Errorf("%s: poll_interval %q: %w", src, sf.PollInterval, err)
		}
	}

	inst := &SensorInstance{
		Name:         sf.Name,
		Type:         sf.Type,
		PollInterval: interval,
		MaxInflight:  sf.MaxInflight,
		Config:       sf.Config,
		SourcePath:   src,
	}
	for _, t := range sf.Triggers {
		inst.Triggers = append(inst.Triggers, SensorTrigger{Constellation: t.Constellation, When: t.When})
	}
	return inst, nil
}

// nodeTypeOf resolves a node's declared type, inferring it from whichever of
// star/ref/op is populated when the type field is omitted.
func nodeTypeOf(declared, star, ref, op string) (NodeType, error) {
	switch declared {
	case string(NodeStar), string(NodeConstellation), string(NodePhaseIterator), string(NodeBuiltin):
		return NodeType(declared), nil
	case "":
		switch {
		case star != "":
			return NodeStar, nil
		case ref != "":
			return NodeConstellation, nil
		case op != "":
			return NodeBuiltin, nil
		default:
			return "", fmt.Errorf("missing type and no star/ref/op to infer it from")
		}
	default:
		return "", fmt.Errorf("unknown node type %q", declared)
	}
}

// compileTemplates parses each value of a TOML inputs/outputs map into an
// interpolation Expression, preserving the keys.
func compileTemplates(raw map[string]string) (map[string]Expression, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string]Expression, len(raw))
	for k, v := range raw {
		expr, err := ParseTemplate(v)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", k, err)
		}
		out[k] = expr
	}
	return out, nil
}

// unionStrings appends each element of add to base that is not already present,
// preserving order and producing a de-duplicated union.
func unionStrings(base, add []string) []string {
	seen := make(map[string]bool, len(base)+len(add))
	out := make([]string, 0, len(base)+len(add))
	for _, s := range append(append([]string{}, base...), add...) {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// --- TOML decode targets ---

type starFrontmatter struct {
	Name          string   `toml:"name"`
	Model         string   `toml:"model"`
	FallbackModel string   `toml:"fallback_model"`
	Skills        []string `toml:"skills"`
	Tools         struct {
		Allowed []string `toml:"allowed"`
		Denied  []string `toml:"denied"`
	} `toml:"tools"`
	Defaults struct {
		MaxBudgetUSD float64 `toml:"max_budget_usd"`
		Effort       string  `toml:"effort"`
	} `toml:"defaults"`
}

type skillFrontmatter struct {
	Name     string   `toml:"name"`
	ToolsAdd []string `toml:"tools_add"`
}

type constellationFile struct {
	Name        string         `toml:"name"`
	Description string         `toml:"description"`
	Meta        map[string]any `toml:"meta"`
	Nodes       []struct {
		ID     string            `toml:"id"`
		Type   string            `toml:"type"`
		Star   string            `toml:"star"`
		Ref    string            `toml:"ref"`
		Op     string            `toml:"op"`
		Inputs map[string]string `toml:"inputs"`
	} `toml:"nodes"`
	Edges []struct {
		From string `toml:"from"`
		To   string `toml:"to"`
		When string `toml:"when"`
	} `toml:"edges"`
	Outputs map[string]string `toml:"outputs"`
}

type sensorFile struct {
	Name         string         `toml:"name"`
	Type         string         `toml:"type"`
	PollInterval string         `toml:"poll_interval"`
	MaxInflight  int            `toml:"max_inflight"`
	Config       map[string]any `toml:"config"`
	Triggers     []struct {
		Constellation string `toml:"constellation"`
		When          string `toml:"when"`
	} `toml:"triggers"`
}
