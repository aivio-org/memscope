package theme

import (
	"github.com/charmbracelet/lipgloss"
)

// Theme holds all color tokens used across TUI panels.
type Theme struct {
	// Allocation colors
	HeapAlloc   lipgloss.Color
	HeapDealloc lipgloss.Color
	StackGrow   lipgloss.Color
	GCPause     lipgloss.Color
	GCResume    lipgloss.Color

	// Memory region band colors
	RegionHeap  lipgloss.Color
	RegionStack lipgloss.Color
	RegionText  lipgloss.Color
	RegionBSS   lipgloss.Color
	RegionMmap  lipgloss.Color
	RegionGuard lipgloss.Color
	RegionVDSO  lipgloss.Color

	// UI chrome
	Background  lipgloss.Color
	Border      lipgloss.Color
	BorderFocus lipgloss.Color
	Text        lipgloss.Color
	TextDim     lipgloss.Color
	TextBright  lipgloss.Color
	Header      lipgloss.Color
	StatusBar   lipgloss.Color
	FilterBg    lipgloss.Color
	FilterText  lipgloss.Color

	// Sparkline block colors
	SparkAlloc   lipgloss.Color
	SparkDealloc lipgloss.Color
	SparkGC      lipgloss.Color
}

// Dracula returns the default Dracula-inspired theme.
func Dracula() Theme {
	return Theme{
		HeapAlloc:   lipgloss.Color("#50fa7b"), // green
		HeapDealloc: lipgloss.Color("#ff5555"), // red
		StackGrow:   lipgloss.Color("#8be9fd"), // cyan
		GCPause:     lipgloss.Color("#ffb86c"), // orange
		GCResume:    lipgloss.Color("#bd93f9"), // purple

		RegionHeap:  lipgloss.Color("#50fa7b"),
		RegionStack: lipgloss.Color("#8be9fd"),
		RegionText:  lipgloss.Color("#bd93f9"),
		RegionBSS:   lipgloss.Color("#6272a4"),
		RegionMmap:  lipgloss.Color("#f1fa8c"),
		RegionGuard: lipgloss.Color("#44475a"),
		RegionVDSO:  lipgloss.Color("#ff79c6"),

		Background:  lipgloss.Color("#282a36"),
		Border:      lipgloss.Color("#44475a"),
		BorderFocus: lipgloss.Color("#bd93f9"),
		Text:        lipgloss.Color("#f8f8f2"),
		TextDim:     lipgloss.Color("#6272a4"),
		TextBright:  lipgloss.Color("#ffffff"),
		Header:      lipgloss.Color("#bd93f9"),
		StatusBar:   lipgloss.Color("#44475a"),
		FilterBg:    lipgloss.Color("#44475a"),
		FilterText:  lipgloss.Color("#f8f8f2"),

		SparkAlloc:   lipgloss.Color("#50fa7b"),
		SparkDealloc: lipgloss.Color("#ff5555"),
		SparkGC:      lipgloss.Color("#ffb86c"),
	}
}

// Load reads a theme from a TOML file. Returns Dracula() as fallback in Phase 1.
func Load(path string) (Theme, error) {
	// Phase 1 stub — full TOML loader in Phase 4
	return Dracula(), nil
}
