package web

import (
	"fmt"

	"github.com/papapumpkin/quasar/internal/diff"
)

// collapseThreshold is the number of changed lines above which a file
// section defaults to collapsed in the diff viewer.
const collapseThreshold = 200

// DiffViewData is the template context for the diff viewer page.
type DiffViewData struct {
	NebulaName string
	PhaseID    string
	Cycle      int
	Files      []FileDiffView
	Stat       DiffStatView
}

// DiffStatView holds aggregate diff statistics for template rendering.
type DiffStatView struct {
	FilesChanged int
	Insertions   int
	Deletions    int
}

// FileDiffView wraps a parsed FileDiff with HTML-ready data.
type FileDiffView struct {
	Path      string
	Hunks     []HunkView
	Additions int
	Deletions int
	Collapsed bool // collapse by default if file has > 200 lines changed
}

// HunkView holds lines ready for HTML rendering.
type HunkView struct {
	Lines []DiffLineView
}

// DiffLineView is a single diff line with CSS class and line numbers.
type DiffLineView struct {
	Type    string // "context", "add", "remove"
	Content string
	OldNum  string // blank for additions
	NewNum  string // blank for deletions
}

// RenderDiffHTML parses a unified diff string and returns structured
// data ready for template rendering.
func RenderDiffHTML(raw string) DiffViewData {
	files := diff.ParseUnifiedDiff(raw)
	stat := diff.ComputeDiffStat(files)

	data := DiffViewData{
		Stat: DiffStatView{
			FilesChanged: stat.FilesChanged,
			Insertions:   stat.Insertions,
			Deletions:    stat.Deletions,
		},
	}

	for _, fs := range stat.FileStats {
		fv := FileDiffView{
			Path:      fs.Path,
			Additions: fs.Additions,
			Deletions: fs.Deletions,
			Collapsed: (fs.Additions + fs.Deletions) > collapseThreshold,
		}

		// Find the matching parsed file to extract hunks.
		for _, f := range files {
			if f.Path != fs.Path {
				continue
			}
			for _, h := range f.Hunks {
				hv := HunkView{}
				for _, l := range h.Lines {
					hv.Lines = append(hv.Lines, diffLineToView(l))
				}
				fv.Hunks = append(fv.Hunks, hv)
			}
			break
		}

		data.Files = append(data.Files, fv)
	}

	return data
}

// diffLineToView converts a diff.DiffLine into a DiffLineView for templates.
func diffLineToView(l diff.DiffLine) DiffLineView {
	v := DiffLineView{
		Content: l.Content,
	}

	switch l.Type {
	case diff.DiffLineAdd:
		v.Type = "add"
	case diff.DiffLineRemove:
		v.Type = "remove"
	default:
		v.Type = "context"
	}

	if l.OldNum > 0 {
		v.OldNum = fmt.Sprintf("%d", l.OldNum)
	}
	if l.NewNum > 0 {
		v.NewNum = fmt.Sprintf("%d", l.NewNum)
	}

	return v
}
