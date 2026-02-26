package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mbergo/memscope/internal/agent"
	"github.com/mbergo/memscope/internal/events"
	"github.com/mbergo/memscope/internal/pipeline"
	"github.com/mbergo/memscope/internal/symbol"
	"github.com/mbergo/memscope/internal/theme"
	"github.com/mbergo/memscope/internal/tui/panels"
)

// tickInterval is the TUI refresh rate (≤30 fps).
const tickInterval = time.Second / 30

// focus panel indices
const (
	focusA = 0
	focusB = 1
	focusC = 2
	focusE = 3
)

// tickMsg is sent on every render tick.
type tickMsg struct{}

// eventMsg wraps an incoming MemEvent for routing via the Update loop.
type eventMsg struct{ e events.MemEvent }

// regionsMsg carries a fresh /proc/<pid>/maps snapshot.
type regionsMsg struct{ regions []events.MemRegion }

// probeStartedMsg is returned by startProbeCmd when the probe is ready.
type probeStartedMsg struct {
	cancel  context.CancelFunc
	sysCh   <-chan events.SyscallEvent
}

// Model is the root bubbletea model.
type Model struct {
	pid    int
	probe  agent.Probe
	pipe   *pipeline.Pipeline
	cancel context.CancelFunc
	sysCh  <-chan events.SyscallEvent

	panelA panels.PanelA
	panelB panels.PanelB
	panelC panels.PanelC
	panelE panels.PanelE

	filter FilterModel
	keys   KeyMap
	theme  theme.Theme

	focus  int
	frozen bool

	width  int
	height int

	err     error
	showErr bool
}

// NewModel constructs a Model. The probe must not yet be started.
func NewModel(p agent.Probe, pid int, t theme.Theme) Model {
	return Model{
		pid:    pid,
		probe:  p,
		pipe:   pipeline.New(0),
		theme:  t,
		keys:   DefaultKeyMap(),
		filter: NewFilterModel(),
		panelA: panels.NewPanelA(t),
		panelB: panels.NewPanelB(t),
		panelC: panels.NewPanelC(t),
		panelE: panels.NewPanelE(t),
		focus:  focusA,
	}
}

// Init starts the probe, pipeline, and tick.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.startProbe(),
		tickCmd(),
		m.refreshRegions(),
	)
}

func (m Model) startProbe() tea.Cmd {
	probe := m.probe
	pid := m.pid
	pipe := m.pipe
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())

		memCh, sysCh, err := probe.Start(pid)
		if err != nil {
			cancel()
			return errMsg{err}
		}

		// Start the pipeline goroutine for memory events.
		go pipe.Run(ctx, memCh)

		return probeStartedMsg{cancel: cancel, sysCh: sysCh}
	}
}

// Update is the central message handler.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case probeStartedMsg:
		m.cancel = msg.cancel
		m.sysCh = msg.sysCh
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.resize()
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{tickCmd()}

		if !m.frozen {
			// Drain memory events from pipeline ring buffer.
			evts := m.pipe.RingBuffer().Drain(256)
			for _, e := range evts {
				if m.filter.Match(e) {
					m.panelA = m.panelA.Push(e)
					m.panelE = m.panelE.Push(e)
					if e.Kind == events.KindAlloc {
						m.panelB = m.panelB.PushAlloc(e)
						m.panelC = m.panelC.PushAlloc(e)
					} else if e.Kind == events.KindDealloc {
						m.panelB = m.panelB.RemoveAlloc(e.Addr)
						m.panelC = m.panelC.RemoveAlloc(e.Addr)
					}
				}
			}

			// Drain syscall events (non-blocking).
			if m.sysCh != nil {
				for drained := 0; drained < 128; drained++ {
					select {
					case se, ok := <-m.sysCh:
						if !ok {
							m.sysCh = nil
							break
						}
						m.panelE = m.panelE.PushSyscall(se)
					default:
						drained = 128 // break outer loop
					}
				}
			}
		}
		return m, tea.Batch(cmds...)

	case regionsMsg:
		if msg.regions != nil {
			m.panelB = m.panelB.SetRegions(msg.regions)
		}
		return m, refreshRegionsCmd(m.pid)

	case errMsg:
		m.err = msg.err
		m.showErr = true
		return m, nil

	case tea.KeyMsg:
		// Filter bar gets priority when active.
		if m.filter.Active() {
			switch msg.String() {
			case "esc", "enter":
				m.filter = m.filter.Toggle()
				pred := m.filter.Match
				m.panelE = m.panelE.SetFilter(pred)
				return m, nil
			}
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			return m, cmd
		}

		switch {
		case msg.String() == "q", msg.String() == "ctrl+c":
			return m, tea.Quit

		case msg.String() == "tab":
			m.focus = (m.focus + 1) % 4
			m = m.updateFocus()

		case msg.String() == "shift+tab":
			m.focus = (m.focus + 3) % 4
			m = m.updateFocus()

		case msg.String() == "f2":
			m.filter = m.filter.Toggle()

		case msg.String() == " ":
			m.frozen = !m.frozen

		case msg.String() == "c":
			m.panelE = m.panelE.Clear()

		case msg.String() == "r":
			m.panelA = m.panelA.ResetZoom()

		case msg.String() == "+", msg.String() == "=":
			m.panelA = m.panelA.ZoomIn()

		case msg.String() == "-":
			m.panelA = m.panelA.ZoomOut()

		default:
			var cmd tea.Cmd
			switch m.focus {
			case focusA:
				m.panelA, cmd = m.panelA.Update(msg)
			case focusB:
				m.panelB, cmd = m.panelB.Update(msg)
			case focusC:
				m.panelC, cmd = m.panelC.Update(msg)
			case focusE:
				m.panelE, cmd = m.panelE.Update(msg)
			}
			return m, cmd
		}
	}

	return m, nil
}

// View renders the full TUI layout.
func (m Model) View() string {
	if m.width == 0 {
		return "Initializing…"
	}

	if m.showErr && m.err != nil {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff5555")).
			Render(fmt.Sprintf("Error: %v\n\nPress q to quit.", m.err))
	}

	header := m.renderHeader()

	// Panel A (top-left 55%) + Panel B (top-right 45%)
	topRow := lipgloss.JoinHorizontal(lipgloss.Top, m.panelA.View(), m.panelB.View())
	// Panel C (bottom-left 40%) + Panel E (bottom-right 60%)
	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, m.panelC.View(), m.panelE.View())

	filterView := ""
	if m.filter.Active() {
		filterView = lipgloss.NewStyle().
			Background(m.theme.FilterBg).
			Foreground(m.theme.FilterText).
			Width(m.width).
			Render(m.filter.View())
	}

	statusBar := m.renderStatus()

	parts := []string{header, topRow, bottomRow}
	if filterView != "" {
		parts = append(parts, filterView)
	}
	parts = append(parts, statusBar)

	return strings.Join(parts, "\n")
}

// resize recalculates panel sizes after a terminal resize.
func (m Model) resize() Model {
	totalH := m.height - 3 // header + status bar + filter row
	topH := totalH * 45 / 100
	if topH < 6 {
		topH = 6
	}
	bottomH := totalH - topH
	if bottomH < 5 {
		bottomH = 5
	}

	// Top row: A=55%, B=45%
	aW := m.width * 55 / 100
	bW := m.width - aW

	// Bottom row: C=40%, E=60%
	cW := m.width * 40 / 100
	eW := m.width - cW

	m.panelA = m.panelA.SetSize(aW, topH)
	m.panelB = m.panelB.SetSize(bW, topH)
	m.panelC = m.panelC.SetSize(cW, bottomH)
	m.panelE = m.panelE.SetSize(eW, bottomH)
	return m
}

func (m Model) updateFocus() Model {
	m.panelA = m.panelA.SetFocused(m.focus == focusA)
	m.panelB = m.panelB.SetFocused(m.focus == focusB)
	m.panelC = m.panelC.SetFocused(m.focus == focusC)
	m.panelE = m.panelE.SetFocused(m.focus == focusE)
	return m
}

func (m Model) renderHeader() string {
	lang := m.probe.Lang()
	frozen := ""
	if m.frozen {
		frozen = " [FROZEN]"
	}
	title := fmt.Sprintf(" MemScope  pid:%d  lang:%s%s", m.pid, lang, frozen)

	return lipgloss.NewStyle().
		Background(m.theme.Header).
		Foreground(m.theme.Background).
		Bold(true).
		Width(m.width).
		Render(title)
}

func (m Model) renderStatus() string {
	hints := []string{
		"F2 filter",
		"tab focus",
		"s syscalls",
		"space freeze",
		"c clear",
		"q quit",
	}
	bar := " " + strings.Join(hints, "  ")
	return lipgloss.NewStyle().
		Background(m.theme.StatusBar).
		Foreground(m.theme.Text).
		Width(m.width).
		Render(bar)
}

// refreshRegions reads /proc/<pid>/maps and returns a regionsMsg.
func (m Model) refreshRegions() tea.Cmd {
	return refreshRegionsCmd(m.pid)
}

// --------------------------------------------------------------------------
// Commands
// --------------------------------------------------------------------------

func tickCmd() tea.Cmd {
	return tea.Tick(tickInterval, func(_ time.Time) tea.Msg {
		return tickMsg{}
	})
}

func refreshRegionsCmd(pid int) tea.Cmd {
	return tea.Tick(2*time.Second, func(_ time.Time) tea.Msg {
		if pid == 0 {
			return regionsMsg{}
		}
		regions, err := symbol.ReadMaps(pid)
		if err != nil {
			return regionsMsg{}
		}
		return regionsMsg{regions: regions}
	})
}

// Close cancels the pipeline context and stops the probe.
func (m Model) Close() {
	if m.cancel != nil {
		m.cancel()
	}
	if m.probe != nil {
		_ = m.probe.Stop()
	}
}

type errMsg struct{ err error }
