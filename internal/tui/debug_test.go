package tui

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func TestDebugDiffNav(t *testing.T) {
	m := NewAppModel(ModeLoop)
	m.Width = 120
	m.Height = 40
	m.Detail = NewDetailPanel(80, 10)
	m.Depth = DepthAgentOutput
	m.LoopView.StartCycle(1)
	m.LoopView.StartAgent("coder")
	m.LoopView.FinishAgent("coder", 0.01, 100)
	m.LoopView.SetAgentDiff("coder", 1, "diff --git a/f.go b/f.go\n--- a/f.go\n+++ b/f.go\n@@ -1,1 +1,2 @@\n line1\n+line2\n")
	m.LoopView.Cursor = 1
	m.ShowDiff = true
	fl := &FileListView{
		Files: []FileStatEntry{
			{Path: "a.go", Additions: 1, Deletions: 0},
			{Path: "b.go", Additions: 2, Deletions: 1},
			{Path: "c.go", Additions: 0, Deletions: 3},
		},
		Cursor: 0,
		Width:  80,
	}
	m.DiffFileList = fl

	downMsg := tea.KeyMsg{Type: tea.KeyDown}

	fmt.Printf("Key string: %q\n", downMsg.String())
	fmt.Printf("Matches Down: %v\n", key.Matches(downMsg, m.Keys.Down))
	fmt.Printf("Matches Up: %v\n", key.Matches(downMsg, m.Keys.Up))
	fmt.Printf("Matches Enter: %v\n", key.Matches(downMsg, m.Keys.Enter))
	fmt.Printf("Matches OpenDiff: %v\n", key.Matches(downMsg, m.Keys.OpenDiff))
	fmt.Printf("Matches Back: %v\n", key.Matches(downMsg, m.Keys.Back))
	fmt.Printf("Matches Quit: %v\n", key.Matches(downMsg, m.Keys.Quit))
	fmt.Printf("Matches Pause: %v\n", key.Matches(downMsg, m.Keys.Pause))
	fmt.Printf("Matches Stop: %v\n", key.Matches(downMsg, m.Keys.Stop))
	fmt.Printf("Matches Retry: %v\n", key.Matches(downMsg, m.Keys.Retry))

	// Also try 'j' key.
	jMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}
	fmt.Printf("\n'j' key string: %q\n", jMsg.String())
	fmt.Printf("'j' Matches Down: %v\n", key.Matches(jMsg, m.Keys.Down))

	result, _ := m.handleKey(jMsg)
	updated := result.(AppModel)
	fmt.Printf("After handleKey with 'j': fl.Cursor=%d, updated.DiffFileList.Cursor=%d\n",
		fl.Cursor, updated.DiffFileList.Cursor)
}
