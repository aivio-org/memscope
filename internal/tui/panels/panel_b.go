package panels

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mbergo/memscope/internal/events"
	"github.com/mbergo/memscope/internal/theme"
)

// PanelB is the memory battery visualizer.
// It shows heap and stack fill levels as phone-battery-style ASCII art,
// then a proportional address-space bar below.
type PanelB struct {
	regions        []events.MemRegion
	liveAllocs     []events.MemEvent
	liveAllocBytes uint64 // sum of sizes in liveAllocs
	cursor         int
	width          int
	height         int
	theme          theme.Theme
	focused        bool
}

// NewPanelB creates a ready-to-use PanelB.
func NewPanelB(t theme.Theme) PanelB {
	return PanelB{theme: t}
}

// SetSize updates the panel dimensions.
func (p PanelB) SetSize(w, h int) PanelB {
	p.width = w
	p.height = h
	return p
}

// SetFocused marks the panel as focused or unfocused.
func (p PanelB) SetFocused(v bool) PanelB {
	p.focused = v
	return p
}

// SetRegions updates the displayed memory map regions.
func (p PanelB) SetRegions(regions []events.MemRegion) PanelB {
	p.regions = regions
	if p.cursor >= len(regions) {
		p.cursor = 0
	}
	return p
}

// PushAlloc records a live allocation for the heap battery meter.
func (p PanelB) PushAlloc(e events.MemEvent) PanelB {
	if e.Kind != events.KindAlloc {
		return p
	}
	p.liveAllocs = append(p.liveAllocs, e)
	p.liveAllocBytes += e.Size
	if len(p.liveAllocs) > 2000 {
		trimmed := make([]events.MemEvent, 2000)
		copy(trimmed, p.liveAllocs[len(p.liveAllocs)-2000:])
		// subtract bytes from the dropped items
		dropped := p.liveAllocs[:len(p.liveAllocs)-2000]
		for _, d := range dropped {
			if p.liveAllocBytes >= d.Size {
				p.liveAllocBytes -= d.Size
			}
		}
		p.liveAllocs = trimmed
	}
	return p
}

// RemoveAlloc removes a freed address from the live alloc overlay.
func (p PanelB) RemoveAlloc(addr uint64) PanelB {
	for i, e := range p.liveAllocs {
		if e.Addr == addr {
			fresh := make([]events.MemEvent, 0, len(p.liveAllocs)-1)
			fresh = append(fresh, p.liveAllocs[:i]...)
			fresh = append(fresh, p.liveAllocs[i+1:]...)
			if p.liveAllocBytes >= e.Size {
				p.liveAllocBytes -= e.Size
			}
			p.liveAllocs = fresh
			return p
		}
	}
	return p
}

// Update handles key events when the panel is focused.
func (p PanelB) Update(msg tea.Msg) (PanelB, tea.Cmd) {
	if !p.focused {
		return p, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if p.cursor > 0 {
				p.cursor--
			}
		case "down", "j":
			if p.cursor < len(p.regions)-1 {
				p.cursor++
			}
		}
	}
	return p, nil
}

// View renders the battery panel.
func (p PanelB) View() string {
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
		Render("Memory · Heap & Stack")

	innerW := p.width - 4
	if innerW < 12 {
		innerW = 12
	}

	if len(p.regions) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(p.theme.TextDim).
			Render("Waiting for memory map…")
		return border.Render(title + "\n" + empty)
	}

	var sb strings.Builder
	sb.WriteString(title)
	sb.WriteByte('\n')

	// Compute heap fill %
	heapSize := p.heapRegionSize()
	heapFill := 0.0
	if heapSize > 0 {
		heapFill = float64(p.liveAllocBytes) / float64(heapSize)
		if heapFill > 1.0 {
			heapFill = 1.0
		}
	}
	heapLabel := fmt.Sprintf("HEAP  %3.0f%%  %s / %s",
		heapFill*100,
		formatBytes(p.liveAllocBytes),
		formatBytes(heapSize),
	)
	sb.WriteString(p.renderBattery(heapLabel, heapFill, p.batteryColor(heapFill, true), innerW))
	sb.WriteByte('\n')

	// Compute stack fill %
	stackSize := p.stackRegionSize()
	const typicalMaxStack = 8 * 1024 * 1024 // 8 MB goroutine stack limit
	stackFill := 0.0
	if typicalMaxStack > 0 {
		stackFill = float64(stackSize) / float64(typicalMaxStack)
		if stackFill > 1.0 {
			stackFill = 1.0
		}
	}
	stackLabel := fmt.Sprintf("STACK %3.0f%%  %s",
		stackFill*100,
		formatBytes(stackSize),
	)
	sb.WriteString(p.renderBattery(stackLabel, stackFill, p.batteryColor(stackFill, false), innerW))

	// If enough height, show address bar below
	if p.height > 14 {
		sb.WriteString("\n")
		sb.WriteString(lipgloss.NewStyle().Foreground(p.theme.TextDim).Render("─── Address Space ───"))
		sb.WriteString("\n")
		sb.WriteString(p.renderAddressBar(innerW))
		sb.WriteString("\n")
		sb.WriteString(p.renderFooter())
	}

	return border.Render(sb.String())
}

// renderBattery draws one battery widget.
//
//	╔══════════════╦═╗
//	║████████░░░░░░║ ║
//	╚══════════════╩═╝
//	HEAP  53%  1.2MB / 2.3MB
func (p PanelB) renderBattery(label string, fillPct float64, fillColor lipgloss.Color, maxW int) string {
	// Body width = maxW - 4 (nipple takes 3 chars + 1 space)
	bodyW := maxW - 4
	if bodyW < 8 {
		bodyW = 8
	}
	if bodyW > 24 {
		bodyW = 24
	}

	filled := int(float64(bodyW) * fillPct)
	if filled > bodyW {
		filled = bodyW
	}
	empty := bodyW - filled

	topLine := "╔" + strings.Repeat("═", bodyW) + "╦═╗"
	midFill := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	midLine := "║" +
		lipgloss.NewStyle().Foreground(fillColor).Render(midFill) +
		"║ ║"
	botLine := "╚" + strings.Repeat("═", bodyW) + "╩═╝"

	return topLine + "\n" + midLine + "\n" + botLine + "\n" +
		lipgloss.NewStyle().Foreground(p.theme.TextDim).Render(label)
}

func (p PanelB) batteryColor(fill float64, isHeap bool) lipgloss.Color {
	switch {
	case fill >= 0.8:
		return lipgloss.Color("#ff5555") // red
	case fill >= 0.5:
		return lipgloss.Color("#f1fa8c") // yellow
	default:
		if isHeap {
			return p.theme.HeapAlloc // green-ish
		}
		return p.theme.RegionStack
	}
}

func (p PanelB) heapRegionSize() uint64 {
	for _, r := range p.regions {
		if r.Kind == events.RegionHeap {
			return r.Size()
		}
	}
	return 0
}

func (p PanelB) stackRegionSize() uint64 {
	var total uint64
	for _, r := range p.regions {
		if r.Kind == events.RegionStack {
			total += r.Size()
		}
	}
	return total
}

// renderAddressBar draws a proportional horizontal bar of colored region bands.
func (p PanelB) renderAddressBar(w int) string {
	if len(p.regions) == 0 {
		return ""
	}

	minAddr := p.regions[0].Start
	maxAddr := p.regions[len(p.regions)-1].End
	for _, r := range p.regions {
		if r.Start < minAddr {
			minAddr = r.Start
		}
		if r.End > maxAddr {
			maxAddr = r.End
		}
	}
	totalSpan := maxAddr - minAddr
	if totalSpan == 0 {
		totalSpan = 1
	}

	type band struct {
		color lipgloss.Color
		width int
	}
	var bands []band
	used := 0
	for i, r := range p.regions {
		regionW := int(float64(r.Size()) / float64(totalSpan) * float64(w))
		if regionW < 1 && r.Size() > 0 {
			regionW = 1
		}
		if i == len(p.regions)-1 {
			regionW = w - used
		}
		if regionW <= 0 {
			continue
		}
		bands = append(bands, band{color: p.regionColor(r.Kind), width: regionW})
		used += regionW
	}

	heapStart, heapEnd := uint64(0), uint64(0)
	heapOffset := 0
	offsetSoFar := 0
	for i, r := range p.regions {
		if r.Kind == events.RegionHeap {
			heapStart = r.Start
			heapEnd = r.End
			heapOffset = offsetSoFar
			break
		}
		if i < len(bands) {
			offsetSoFar += bands[i].width
		}
	}

	var topRow strings.Builder
	dotRunes := make([]rune, 0, w)

	for _, b := range bands {
		chunk := strings.Repeat("█", b.width)
		topRow.WriteString(lipgloss.NewStyle().Foreground(b.color).Render(chunk))
		for i := 0; i < b.width; i++ {
			dotRunes = append(dotRunes, ' ')
		}
	}

	heapSpan := heapEnd - heapStart
	heapBandWidth := int(float64(heapEnd-heapStart) / float64(totalSpan) * float64(w))
	if heapBandWidth < 1 {
		heapBandWidth = 1
	}
	if heapSpan > 0 {
		for _, alloc := range p.liveAllocs {
			if alloc.Addr < heapStart || alloc.Addr >= heapEnd {
				continue
			}
			relPos := int(float64(alloc.Addr-heapStart) / float64(heapSpan) * float64(heapBandWidth))
			absPos := heapOffset + relPos
			if absPos >= 0 && absPos < len(dotRunes) {
				dotRunes[absPos] = '·'
			}
		}
	}

	return topRow.String() + "\n" + string(dotRunes)
}

// renderFooter shows the cursor region's details.
func (p PanelB) renderFooter() string {
	if len(p.regions) == 0 || p.cursor >= len(p.regions) {
		return ""
	}
	r := p.regions[p.cursor]
	info := fmt.Sprintf("%s  %016x–%016x  %s  %s  %s",
		r.Kind.String(), r.Start, r.End,
		formatBytes(r.Size()), r.Perms, r.Name,
	)
	return lipgloss.NewStyle().Foreground(p.theme.Text).Render(info)
}

func (p PanelB) regionColor(kind events.RegionKind) lipgloss.Color {
	switch kind {
	case events.RegionHeap:
		return p.theme.RegionHeap
	case events.RegionStack:
		return p.theme.RegionStack
	case events.RegionText:
		return p.theme.RegionText
	case events.RegionBSS:
		return p.theme.RegionBSS
	case events.RegionMmap:
		return p.theme.RegionMmap
	case events.RegionGuard:
		return p.theme.RegionGuard
	case events.RegionVDSO, events.RegionVvar:
		return p.theme.RegionVDSO
	default:
		return p.theme.TextDim
	}
}
