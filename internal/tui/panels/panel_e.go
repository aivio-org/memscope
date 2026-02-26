package panels

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mbergo/memscope/internal/events"
	"github.com/mbergo/memscope/internal/theme"
)

const maxLogEntries = 1000

// PanelE is the scrollable event log / syscall log panel.
// Toggle between views with the 's' key.
type PanelE struct {
	entries    []events.MemEvent
	syscalls   []events.SyscallEvent
	viewport   viewport.Model
	width      int
	height     int
	theme      theme.Theme
	focused    bool
	startTime  time.Time
	autoScroll bool
	filter     func(events.MemEvent) bool
	syscallTab bool // true = show syscall view
}

// NewPanelE creates a ready-to-use PanelE.
func NewPanelE(t theme.Theme) PanelE {
	vp := viewport.New(80, 20)
	return PanelE{
		theme:      t,
		viewport:   vp,
		startTime:  time.Now(),
		autoScroll: true,
		filter:     func(_ events.MemEvent) bool { return true },
	}
}

// SetSize updates the panel dimensions.
func (p PanelE) SetSize(w, h int) PanelE {
	p.width = w
	p.height = h
	inner := h - 2
	if inner < 1 {
		inner = 1
	}
	p.viewport.Width = w - 2
	p.viewport.Height = inner
	p.viewport.SetContent(p.renderContent())
	return p
}

// SetFocused marks the panel as focused or unfocused.
func (p PanelE) SetFocused(v bool) PanelE {
	p.focused = v
	return p
}

// SetFilter replaces the active event filter predicate.
func (p PanelE) SetFilter(f func(events.MemEvent) bool) PanelE {
	p.filter = f
	p.viewport.SetContent(p.renderContent())
	if p.autoScroll {
		p.viewport.GotoBottom()
	}
	return p
}

// Push appends a new memory event to the event log.
func (p PanelE) Push(e events.MemEvent) PanelE {
	p.entries = append(p.entries, e)
	if len(p.entries) > maxLogEntries {
		p.entries = p.entries[len(p.entries)-maxLogEntries:]
	}
	if !p.syscallTab {
		p.viewport.SetContent(p.renderContent())
		if p.autoScroll {
			p.viewport.GotoBottom()
		}
	}
	return p
}

// PushSyscall appends a syscall event to the syscall log.
func (p PanelE) PushSyscall(e events.SyscallEvent) PanelE {
	p.syscalls = append(p.syscalls, e)
	if len(p.syscalls) > maxLogEntries {
		p.syscalls = p.syscalls[len(p.syscalls)-maxLogEntries:]
	}
	if p.syscallTab {
		p.viewport.SetContent(p.renderSyscallContent())
		if p.autoScroll {
			p.viewport.GotoBottom()
		}
	}
	return p
}

// ToggleSyscallTab switches between the event log and syscall log views.
func (p PanelE) ToggleSyscallTab() PanelE {
	p.syscallTab = !p.syscallTab
	p.viewport.SetContent(p.renderContent())
	if p.autoScroll {
		p.viewport.GotoBottom()
	}
	return p
}

// Clear removes all entries from the currently visible view.
func (p PanelE) Clear() PanelE {
	if p.syscallTab {
		p.syscalls = p.syscalls[:0]
	} else {
		p.entries = p.entries[:0]
	}
	p.viewport.SetContent("")
	return p
}

// Update handles key/scroll events when the panel is focused.
func (p PanelE) Update(msg tea.Msg) (PanelE, tea.Cmd) {
	if !p.focused {
		return p, nil
	}
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "s" {
		return p.ToggleSyscallTab(), nil
	}

	atBottom := p.viewport.AtBottom()
	var cmd tea.Cmd
	p.viewport, cmd = p.viewport.Update(msg)

	if atBottom && !p.viewport.AtBottom() {
		p.autoScroll = false
	}
	if p.viewport.AtBottom() {
		p.autoScroll = true
	}
	return p, cmd
}

// View renders the panel.
func (p PanelE) View() string {
	borderColor := p.theme.Border
	if p.focused {
		borderColor = p.theme.BorderFocus
	}

	border := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(p.width - 2).
		Height(p.height - 2)

	tabEvent := "Event Log"
	tabSyscall := "Syscalls"
	if p.syscallTab {
		tabEvent = lipgloss.NewStyle().Foreground(p.theme.TextDim).Render(tabEvent)
		tabSyscall = lipgloss.NewStyle().Foreground(p.theme.Header).Bold(true).Render("[" + tabSyscall + "]")
	} else {
		tabEvent = lipgloss.NewStyle().Foreground(p.theme.Header).Bold(true).Render("[" + tabEvent + "]")
		tabSyscall = lipgloss.NewStyle().Foreground(p.theme.TextDim).Render(tabSyscall)
	}

	scrollHint := ""
	if !p.autoScroll {
		scrollHint = lipgloss.NewStyle().Foreground(p.theme.TextDim).Render(" [scrolled]")
	}
	hint := lipgloss.NewStyle().Foreground(p.theme.TextDim).Render("  s:toggle")

	header := tabEvent + "  " + tabSyscall + scrollHint + hint

	return border.Render(header + "\n" + p.viewport.View())
}

// renderContent returns the full text for whichever tab is active.
func (p PanelE) renderContent() string {
	if p.syscallTab {
		return p.renderSyscallContent()
	}
	var sb strings.Builder
	for _, e := range p.entries {
		if !p.filter(e) {
			continue
		}
		sb.WriteString(p.formatEvent(e))
		sb.WriteByte('\n')
	}
	return sb.String()
}

func (p PanelE) renderSyscallContent() string {
	var sb strings.Builder
	for _, e := range p.syscalls {
		sb.WriteString(p.formatSyscall(e))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// formatEvent renders one memory-event log line.
func (p PanelE) formatEvent(e events.MemEvent) string {
	elapsed := e.Timestamp.Sub(p.startTime)
	h := int(elapsed.Hours())
	m := int(elapsed.Minutes()) % 60
	s := int(elapsed.Seconds()) % 60
	ms := int(elapsed.Milliseconds()) % 1000

	ts := fmt.Sprintf("[+%02d:%02d:%02d.%03d]", h, m, s, ms)

	kindStr := fmt.Sprintf("%-10s", e.Kind.String())
	sizeStr := formatBytes(e.Size)
	typeName := e.TypeName
	if typeName == "" {
		typeName = "-"
	}
	goroutineStr := fmt.Sprintf("goroutine:%-4d", e.GoroutineID)

	kindColor := p.kindColor(e.Kind)

	return lipgloss.NewStyle().Foreground(p.theme.TextDim).Render(ts) +
		" " +
		lipgloss.NewStyle().Foreground(kindColor).Render(kindStr) +
		" " +
		lipgloss.NewStyle().Foreground(p.theme.Text).Render(fmt.Sprintf("%-10s", sizeStr)) +
		" " +
		lipgloss.NewStyle().Foreground(p.theme.TextBright).Render(fmt.Sprintf("%-32s", typeName)) +
		" " +
		lipgloss.NewStyle().Foreground(p.theme.TextDim).Render(goroutineStr)
}

// formatSyscall renders one syscall log line.
// Format: [HH:MM:SS.mmm] pid:P tid:T name              ret:R  123µs
func (p PanelE) formatSyscall(e events.SyscallEvent) string {
	ts := e.Timestamp.Format("15:04:05.000")

	name := fmt.Sprintf("%-18s", e.Name)
	pid := fmt.Sprintf("pid:%-6d", e.Pid)
	tid := fmt.Sprintf("tid:%-6d", e.Tid)

	var retStr string
	if e.Ret < 0 {
		retStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555")).
			Render(fmt.Sprintf("ret:%-8d", e.Ret))
	} else {
		retStr = lipgloss.NewStyle().Foreground(p.theme.Text).
			Render(fmt.Sprintf("ret:%-8d", e.Ret))
	}

	var durStr string
	switch {
	case e.DurationNs >= 1_000_000:
		durStr = fmt.Sprintf("%.2fms", float64(e.DurationNs)/1_000_000)
	case e.DurationNs >= 1_000:
		durStr = fmt.Sprintf("%.1fµs", float64(e.DurationNs)/1_000)
	default:
		durStr = fmt.Sprintf("%dns", e.DurationNs)
	}

	return lipgloss.NewStyle().Foreground(p.theme.TextDim).Render("["+ts+"]") +
		" " +
		lipgloss.NewStyle().Foreground(p.theme.GCPause).Render(name) +
		" " +
		lipgloss.NewStyle().Foreground(p.theme.TextDim).Render(pid+"  "+tid) +
		"  " +
		retStr +
		"  " +
		lipgloss.NewStyle().Foreground(p.theme.TextDim).Render(durStr)
}

func (p PanelE) kindColor(k events.EventKind) lipgloss.Color {
	switch k {
	case events.KindAlloc:
		return p.theme.HeapAlloc
	case events.KindDealloc:
		return p.theme.HeapDealloc
	case events.KindGCPause:
		return p.theme.GCPause
	case events.KindGCResume:
		return p.theme.GCResume
	case events.KindStackGrow:
		return p.theme.StackGrow
	default:
		return p.theme.Text
	}
}

func formatBytes(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
