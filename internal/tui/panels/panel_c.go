package panels

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mbergo/memscope/internal/events"
	"github.com/mbergo/memscope/internal/theme"
)

const (
	maxAllocsPerGoroutine = 20
	maxGoroutineGroups    = 200
	maxAllocsShownPerBox  = 5
)

// goroutineGroup tracks live allocations for one goroutine.
type goroutineGroup struct {
	goroutineID uint64
	allocs      []events.MemEvent
	totalBytes  uint64
}

// PanelC shows live allocations grouped by goroutine ID as an ASCII tree.
//
// Example:
//
//	╭─ goroutine 3 ─────────── 3 allocs  48.2 KB ─╮
//	│  ├ 0xc000012000   256 B  runtime.newobject
//	│  ├ 0xc000034000  8.0 KB  bufio.NewReader
//	│  └ 0xc000045000  40.0 KB [unnamed]
//	╰─────────────────────────────────────────────╯
type PanelC struct {
	groups  map[uint64]*goroutineGroup
	ordered []uint64 // goroutine IDs in insertion order (eviction order)
	scroll  int
	width   int
	height  int
	theme   theme.Theme
	focused bool
}

// NewPanelC creates a ready-to-use PanelC.
func NewPanelC(t theme.Theme) PanelC {
	return PanelC{
		theme:  t,
		groups: make(map[uint64]*goroutineGroup),
	}
}

// SetSize updates the panel dimensions.
func (p PanelC) SetSize(w, h int) PanelC {
	p.width = w
	p.height = h
	return p
}

// SetFocused marks the panel as focused or unfocused.
func (p PanelC) SetFocused(v bool) PanelC {
	p.focused = v
	return p
}

// PushAlloc adds an allocation to the appropriate goroutine group.
func (p PanelC) PushAlloc(e events.MemEvent) PanelC {
	if e.Kind != events.KindAlloc {
		return p
	}

	gid := e.GoroutineID
	g, exists := p.groups[gid]
	if !exists {
		// Evict oldest goroutine group if at capacity.
		if len(p.ordered) >= maxGoroutineGroups {
			oldest := p.ordered[0]
			p.ordered = p.ordered[1:]
			delete(p.groups, oldest)
		}
		g = &goroutineGroup{goroutineID: gid}
		p.groups[gid] = g
		p.ordered = append(p.ordered, gid)
	}

	g.allocs = append(g.allocs, e)
	g.totalBytes += e.Size
	if len(g.allocs) > maxAllocsPerGoroutine {
		removed := g.allocs[0]
		g.allocs = g.allocs[1:]
		if g.totalBytes >= removed.Size {
			g.totalBytes -= removed.Size
		}
	}
	return p
}

// RemoveAlloc removes a freed allocation from its goroutine group.
func (p PanelC) RemoveAlloc(addr uint64) PanelC {
	for _, gid := range p.ordered {
		g := p.groups[gid]
		for i, e := range g.allocs {
			if e.Addr == addr {
				g.allocs = append(g.allocs[:i], g.allocs[i+1:]...)
				if g.totalBytes >= e.Size {
					g.totalBytes -= e.Size
				}
				if len(g.allocs) == 0 {
					delete(p.groups, gid)
					for j, id := range p.ordered {
						if id == gid {
							p.ordered = append(p.ordered[:j], p.ordered[j+1:]...)
							break
						}
					}
				}
				return p
			}
		}
	}
	return p
}

// Update handles scroll events when focused.
func (p PanelC) Update(msg tea.Msg) (PanelC, tea.Cmd) {
	if !p.focused {
		return p, nil
	}
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "up", "k":
			if p.scroll > 0 {
				p.scroll--
			}
		case "down", "j":
			p.scroll++
		}
	}
	return p, nil
}

// View renders the goroutine allocation tree.
func (p PanelC) View() string {
	borderColor := p.theme.Border
	if p.focused {
		borderColor = p.theme.BorderFocus
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(p.width - 2).
		Height(p.height - 2)

	title := lipgloss.NewStyle().
		Foreground(p.theme.Header).
		Bold(true).
		Render("Goroutine Allocs")

	innerW := p.width - 4
	if innerW < 20 {
		innerW = 20
	}

	if len(p.ordered) == 0 {
		empty := lipgloss.NewStyle().Foreground(p.theme.TextDim).
			Render("No live allocations")
		return border.Render(title + "\n" + empty)
	}

	innerH := p.height - 4
	if innerH < 4 {
		innerH = 4
	}

	var sb strings.Builder
	sb.WriteString(title)
	sb.WriteByte('\n')

	lines := 0
	startIdx := p.scroll
	if startIdx >= len(p.ordered) {
		startIdx = 0
	}

	// Cycling color palette for goroutine IDs
	palette := []lipgloss.Color{
		lipgloss.Color("#bd93f9"), // purple
		lipgloss.Color("#50fa7b"), // green
		lipgloss.Color("#8be9fd"), // cyan
		lipgloss.Color("#ffb86c"), // orange
		lipgloss.Color("#ff79c6"), // pink
		lipgloss.Color("#f1fa8c"), // yellow
	}

	for i := startIdx; i < len(p.ordered) && lines < innerH; i++ {
		gid := p.ordered[i]
		g := p.groups[gid]
		if g == nil {
			continue
		}

		color := palette[i%len(palette)]
		box := p.renderGroupBox(g, innerW, color)
		boxLines := strings.Count(box, "\n") + 1

		if lines+boxLines > innerH {
			break
		}
		sb.WriteString(box)
		sb.WriteByte('\n')
		lines += boxLines + 1
	}

	scrollHint := ""
	if p.scroll > 0 || startIdx+1 < len(p.ordered) {
		scrollHint = lipgloss.NewStyle().Foreground(p.theme.TextDim).
			Render(fmt.Sprintf(" ↕ %d goroutines", len(p.ordered)))
	}
	if scrollHint != "" {
		sb.WriteString(scrollHint)
	}

	return border.Render(sb.String())
}

// renderGroupBox renders a single goroutine's allocation box.
func (p PanelC) renderGroupBox(g *goroutineGroup, w int, color lipgloss.Color) string {
	// Header line: ╭─ goroutine N ──── M allocs  X KB ─╮
	allocWord := "allocs"
	if len(g.allocs) == 1 {
		allocWord = "alloc"
	}
	headerText := fmt.Sprintf(" goroutine %-6d  %d %s  %s ",
		g.goroutineID, len(g.allocs), allocWord, formatBytes(g.totalBytes))

	// Pad header to width
	padLen := w - len(headerText) - 4
	if padLen < 0 {
		padLen = 0
	}
	hpad := strings.Repeat("─", padLen)

	topLine := lipgloss.NewStyle().Foreground(color).Render(
		"╭─" + headerText + hpad + "─╮")

	var rows []string
	rows = append(rows, topLine)

	shown := g.allocs
	truncated := 0
	if len(shown) > maxAllocsShownPerBox {
		truncated = len(shown) - maxAllocsShownPerBox
		shown = shown[len(shown)-maxAllocsShownPerBox:]
	}

	for i, e := range shown {
		prefix := "├"
		if i == len(shown)-1 && truncated == 0 {
			prefix = "└"
		}

		typeName := e.TypeName
		if typeName == "" {
			typeName = "[unnamed]"
		}
		if len(typeName) > 24 {
			typeName = typeName[:21] + "..."
		}

		line := fmt.Sprintf("│  %s 0x%012x  %-8s  %-24s",
			prefix, e.Addr, formatBytes(e.Size), typeName)
		if len(line) > w {
			line = line[:w]
		}

		rows = append(rows, lipgloss.NewStyle().Foreground(p.theme.Text).Render(line))
	}

	if truncated > 0 {
		more := fmt.Sprintf("│     … %d more", truncated)
		rows = append(rows, lipgloss.NewStyle().Foreground(p.theme.TextDim).Render(more))
	}

	botLine := lipgloss.NewStyle().Foreground(color).Render(
		"╰" + strings.Repeat("─", w-2) + "╯")
	rows = append(rows, botLine)

	return strings.Join(rows, "\n")
}
