package panels

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mbergo/memscope/internal/events"
	"github.com/mbergo/memscope/internal/theme"
)

// sparkBlocks is the set of unicode block characters used for the sparkline.
// Index 0 = empty (space), 1-8 = increasing height.
var sparkBlocks = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

const (
	windowDuration = 60 * time.Second
	sampleInterval = time.Second
)

// sample holds one-second aggregated metrics.
type sample struct {
	t           time.Time
	allocBytes  uint64
	deallocBytes uint64
	gcPauseMs   float64
}

// PanelA is the allocation timeline sparkline panel.
type PanelA struct {
	samples    []sample // rolling 60s window
	current    sample   // in-progress current second
	width      int
	height     int
	theme      theme.Theme
	focused    bool
	zoomFactor int // 1 = 60s, 2 = 120s, etc.
	gcPausing  bool
	gcPauseStart time.Time
}

// NewPanelA creates a ready-to-use PanelA.
func NewPanelA(t theme.Theme) PanelA {
	return PanelA{
		theme:      t,
		current:    sample{t: time.Now()},
		zoomFactor: 1,
	}
}

// SetSize updates the panel dimensions.
func (p PanelA) SetSize(w, h int) PanelA {
	p.width = w
	p.height = h
	return p
}

// SetFocused marks the panel as focused or unfocused.
func (p PanelA) SetFocused(v bool) PanelA {
	p.focused = v
	return p
}

// Push ingests a new event.
func (p PanelA) Push(e events.MemEvent) PanelA {
	// Use the event's own timestamp for bucket boundaries so that events
	// drained from the ring buffer in a batch are assigned to the second they
	// actually occurred in, not the wall-clock second of the drain call.
	eventTime := e.Timestamp
	if eventTime.IsZero() {
		eventTime = time.Now()
	}

	// Flush sample if we've crossed a second boundary
	if eventTime.Sub(p.current.t) >= sampleInterval {
		p.samples = append(p.samples, p.current)
		p.current = sample{t: eventTime.Truncate(sampleInterval)}

		// Prune old samples outside the window
		window := windowDuration * time.Duration(p.zoomFactor)
		cutoff := eventTime.Add(-window)
		for len(p.samples) > 0 && p.samples[0].t.Before(cutoff) {
			p.samples = p.samples[1:]
		}
	}

	switch e.Kind {
	case events.KindAlloc:
		p.current.allocBytes += e.Size
	case events.KindDealloc:
		p.current.deallocBytes += e.Size
	case events.KindGCPause:
		p.gcPausing = true
		p.gcPauseStart = e.Timestamp
	case events.KindGCResume:
		if p.gcPausing {
			pauseMs := float64(e.Timestamp.Sub(p.gcPauseStart).Milliseconds())
			p.current.gcPauseMs += pauseMs
			p.gcPausing = false
		}
	}
	return p
}

// ZoomIn halves the time window (shows more detail).
func (p PanelA) ZoomIn() PanelA {
	if p.zoomFactor > 1 {
		p.zoomFactor--
	}
	return p
}

// ZoomOut doubles the time window.
func (p PanelA) ZoomOut() PanelA {
	if p.zoomFactor < 10 {
		p.zoomFactor++
	}
	return p
}

// ResetZoom restores the default 60s window.
func (p PanelA) ResetZoom() PanelA {
	p.zoomFactor = 1
	return p
}

// Update handles key events when the panel is focused.
func (p PanelA) Update(msg tea.Msg) (PanelA, tea.Cmd) {
	return p, nil
}

// View renders the sparkline panel.
func (p PanelA) View() string {
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
		Render("Allocation Timeline")

	innerW := p.width - 4 // account for border + padding
	if innerW < 10 {
		innerW = 10
	}
	innerH := p.height - 4 // title + border + axis
	if innerH < 3 {
		innerH = 3
	}

	content := p.renderSparklines(innerW, innerH)
	return border.Render(title + "\n" + content)
}

func (p PanelA) renderSparklines(w, h int) string {
	samples := p.visibleSamples(w)

	if len(samples) == 0 {
		return lipgloss.NewStyle().
			Foreground(p.theme.TextDim).
			Render("Waiting for events…")
	}

	// Find max values for auto-scaling
	maxAlloc := uint64(1)
	maxDealloc := uint64(1)
	maxGC := 0.01

	for _, s := range samples {
		if s.allocBytes > maxAlloc {
			maxAlloc = s.allocBytes
		}
		if s.deallocBytes > maxDealloc {
			maxDealloc = s.deallocBytes
		}
		if s.gcPauseMs > maxGC {
			maxGC = s.gcPauseMs
		}
	}

	rows := h - 1 // reserve last row for time axis
	if rows < 1 {
		rows = 1
	}

	var sb strings.Builder

	// Render alloc sparkline (green)
	allocLine := renderSparkRow(samples, w, func(s sample) float64 {
		return float64(s.allocBytes) / float64(maxAlloc)
	})
	sb.WriteString(
		lipgloss.NewStyle().Foreground(p.theme.SparkAlloc).Render("▲ alloc   ") +
			lipgloss.NewStyle().Foreground(p.theme.SparkAlloc).Render(allocLine) +
			" " + formatBytes(maxAlloc) + "/s\n",
	)

	// Render dealloc sparkline (red)
	deallocLine := renderSparkRow(samples, w, func(s sample) float64 {
		return float64(s.deallocBytes) / float64(maxDealloc)
	})
	sb.WriteString(
		lipgloss.NewStyle().Foreground(p.theme.SparkDealloc).Render("▼ dealloc ") +
			lipgloss.NewStyle().Foreground(p.theme.SparkDealloc).Render(deallocLine) +
			" " + formatBytes(maxDealloc) + "/s\n",
	)

	// Render GC pause bar (orange) only if there were any pauses
	if maxGC > 0.01 {
		gcLine := renderSparkRow(samples, w, func(s sample) float64 {
			return s.gcPauseMs / maxGC
		})
		sb.WriteString(
			lipgloss.NewStyle().Foreground(p.theme.SparkGC).Render("◆ gc_ms   ") +
				lipgloss.NewStyle().Foreground(p.theme.SparkGC).Render(gcLine) +
				fmt.Sprintf(" %.1fms\n", maxGC),
		)
	}

	// Time axis
	window := windowDuration * time.Duration(p.zoomFactor)
	axisStr := renderTimeAxis(w-10, window)
	sb.WriteString(
		lipgloss.NewStyle().Foreground(p.theme.TextDim).Render("          " + axisStr),
	)

	return sb.String()
}

// visibleSamples returns up to w samples, padded with zeros if there are fewer.
// Uses a fresh slice to avoid aliasing the shared backing array of p.samples.
func (p PanelA) visibleSamples(w int) []sample {
	capacity := w - 10 // leave room for label prefix
	if capacity <= 0 {
		return nil
	}
	// Build a fresh slice so append cannot clobber p.samples' backing array.
	all := make([]sample, len(p.samples)+1)
	copy(all, p.samples)
	all[len(p.samples)] = p.current

	if len(all) >= capacity {
		return all[len(all)-capacity:]
	}
	// Pad with zero samples at the front
	pad := make([]sample, capacity-len(all))
	return append(pad, all...)
}

// renderSparkRow converts a slice of samples into a unicode sparkline string.
func renderSparkRow(samples []sample, w int, val func(sample) float64) string {
	capacity := w - 10
	if capacity <= 0 || len(samples) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, s := range samples {
		v := val(s)
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		idx := int(v * float64(len(sparkBlocks)-1))
		sb.WriteRune(sparkBlocks[idx])
	}
	return sb.String()
}

// renderTimeAxis builds the bottom time ruler string.
func renderTimeAxis(w int, window time.Duration) string {
	if w <= 0 {
		return ""
	}
	// Place tick marks at 10s intervals
	tickInterval := 10 * time.Second
	numTicks := int(window / tickInterval)
	if numTicks == 0 {
		numTicks = 1
	}
	spacing := w / numTicks

	var sb strings.Builder
	for i := numTicks; i >= 0; i-- {
		if i*spacing >= w {
			continue
		}
		label := fmt.Sprintf("-%ds", int(window.Seconds())-i*int(tickInterval.Seconds()))
		// Truncate to spacing so a long label never overflows its slot.
		if spacing > 0 && len(label) > spacing {
			label = label[:spacing]
		}
		sb.WriteString(fmt.Sprintf("%-*s", spacing, label))
	}
	return sb.String()
}
