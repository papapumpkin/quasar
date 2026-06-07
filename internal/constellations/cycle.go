package constellations

import (
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/papapumpkin/quasar/internal/artifacts"
	"github.com/papapumpkin/quasar/internal/fabric"
)

// resolveMaxCycles computes the effective master-review cycle cap for a run.
//
// The cap is declarative, never a Go constant. It originates in the
// constellation's [meta] max_cycles (the embedded default, repo-overridable at
// the file level) and is overridden per run by the nebula's
// [execution].max_review_cycles when that value is positive. The resolved value
// is stored on the run's State so edge guards can gate on meta.max_cycles. An
// absent meta value yields 0, which the master-review edges treat as "no fix
// cycle is ever within the cap" — a misconfiguration the embedded default
// prevents.
func resolveMaxCycles(con *artifacts.Constellation, neb *fabric.Nebula) int {
	limit := metaInt(con.Meta, "max_cycles")
	if neb != nil {
		if override := executionMaxReviewCycles(neb.ExecutionTOML); override > 0 {
			limit = override
		}
	}
	return limit
}

// metaInt reads an integer-valued key from a constellation's [meta] table. TOML
// decoding into an untyped map yields int64 for bare integers; float64 is
// tolerated for forgiving authoring. A missing or non-numeric value yields 0.
func metaInt(meta map[string]any, key string) int {
	switch v := meta[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

// executionMaxReviewCycles extracts max_review_cycles from a nebula's
// serialized [execution] config. The column stores the marshaled Execution
// struct (bare keys, no table header), so the value is read directly. A blank
// or unparseable blob yields 0, meaning "no per-run override".
func executionMaxReviewCycles(executionTOML string) int {
	if strings.TrimSpace(executionTOML) == "" {
		return 0
	}
	var exec struct {
		MaxReviewCycles int `toml:"max_review_cycles"`
	}
	if err := toml.Unmarshal([]byte(executionTOML), &exec); err != nil {
		// A malformed execution blob is non-fatal here: the run falls back to the
		// constellation default rather than failing to fire. Surface it for
		// diagnosis without aborting.
		fmt.Fprintf(os.Stderr, "constellations: parse nebula execution config: %v\n", err)
		return 0
	}
	return exec.MaxReviewCycles
}
