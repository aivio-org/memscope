package tui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mbergo/memscope/internal/events"
)

// FilterModel manages the filter bar input and compiles predicates.
type FilterModel struct {
	input     textinput.Model
	active    bool
	predicate Predicate
	err       string
}

// Predicate is a compiled filter function.
type Predicate func(e events.MemEvent) bool

// matchAll is the default no-op predicate.
var matchAll Predicate = func(_ events.MemEvent) bool { return true }

// NewFilterModel creates a ready-to-use FilterModel.
func NewFilterModel() FilterModel {
	ti := textinput.New()
	ti.Placeholder = "type:*http* AND size:>1024 AND kind:heap"
	ti.CharLimit = 256
	return FilterModel{
		input:     ti,
		predicate: matchAll,
	}
}

// Toggle opens or closes the filter bar.
func (f FilterModel) Toggle() FilterModel {
	f.active = !f.active
	if f.active {
		f.input.Focus()
	} else {
		f.input.Blur()
	}
	return f
}

// Active reports whether the filter bar is visible.
func (f FilterModel) Active() bool { return f.active }

// Match applies the compiled predicate to an event.
func (f FilterModel) Match(e events.MemEvent) bool { return f.predicate(e) }

// Update handles bubbletea messages for the filter input.
func (f FilterModel) Update(msg tea.Msg) (FilterModel, tea.Cmd) {
	if !f.active {
		return f, nil
	}
	var cmd tea.Cmd
	f.input, cmd = f.input.Update(msg)

	// Recompile predicate on every keystroke
	pred, err := parseFilter(f.input.Value())
	if err != nil {
		f.err = err.Error()
		f.predicate = matchAll
	} else {
		f.err = ""
		f.predicate = pred
	}
	return f, cmd
}

// View renders the filter bar line.
func (f FilterModel) View() string {
	if !f.active {
		return ""
	}
	prefix := "Filter: "
	if f.err != "" {
		prefix = "Filter [!]: "
	}
	return prefix + f.input.View()
}

// RawValue returns the current raw filter string.
func (f FilterModel) RawValue() string { return f.input.Value() }

// --------------------------------------------------------------------------
// Filter parser
// --------------------------------------------------------------------------

// parseFilter parses a filter expression of the form:
//
//	term [AND term]*
//
// Supported terms:
//
//	type:<glob>
//	size:>N | size:<N | size:N-M
//	kind:heap|stack|gc
//	src:<filename>
func parseFilter(expr string) (Predicate, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return matchAll, nil
	}

	origParts := splitAND(expr)

	preds := make([]Predicate, 0, len(origParts))
	for _, part := range origParts {
		p, err := parseTerm(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		preds = append(preds, p)
	}

	return func(e events.MemEvent) bool {
		for _, p := range preds {
			if !p(e) {
				return false
			}
		}
		return true
	}, nil
}

// splitAND splits on " AND " (case-insensitive).
func splitAND(expr string) []string {
	upper := strings.ToUpper(expr)
	var parts []string
	for {
		idx := strings.Index(upper, " AND ")
		if idx < 0 {
			parts = append(parts, expr)
			break
		}
		parts = append(parts, expr[:idx])
		expr = expr[idx+5:]
		upper = upper[idx+5:]
	}
	return parts
}

func parseTerm(term string) (Predicate, error) {
	idx := strings.IndexByte(term, ':')
	if idx < 0 {
		// Bare string: match against TypeName
		pat := term
		return func(e events.MemEvent) bool {
			ok, _ := filepath.Match(strings.ToLower(pat), strings.ToLower(e.TypeName))
			return ok
		}, nil
	}

	key := strings.ToLower(term[:idx])
	val := term[idx+1:]

	switch key {
	case "type":
		return typeFilter(val), nil
	case "size":
		return sizeFilter(val)
	case "kind":
		return kindFilter(val), nil
	case "src":
		return srcFilter(val), nil
	default:
		return matchAll, nil
	}
}

func typeFilter(pattern string) Predicate {
	return func(e events.MemEvent) bool {
		ok, _ := filepath.Match(strings.ToLower(pattern), strings.ToLower(e.TypeName))
		return ok
	}
}

func sizeFilter(val string) (Predicate, error) {
	val = strings.TrimSpace(val)
	switch {
	case strings.HasPrefix(val, ">"):
		n, err := strconv.ParseUint(val[1:], 10, 64)
		if err != nil {
			return nil, err
		}
		return func(e events.MemEvent) bool { return e.Size > n }, nil

	case strings.HasPrefix(val, "<"):
		n, err := strconv.ParseUint(val[1:], 10, 64)
		if err != nil {
			return nil, err
		}
		return func(e events.MemEvent) bool { return e.Size < n }, nil

	case strings.Contains(val, "-"):
		parts := strings.SplitN(val, "-", 2)
		lo, err1 := strconv.ParseUint(parts[0], 10, 64)
		hi, err2 := strconv.ParseUint(parts[1], 10, 64)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("invalid size range: %q", val)
		}
		return func(e events.MemEvent) bool { return e.Size >= lo && e.Size <= hi }, nil

	default:
		n, err := strconv.ParseUint(val, 10, 64)
		if err != nil {
			return nil, err
		}
		return func(e events.MemEvent) bool { return e.Size == n }, nil
	}
}

func kindFilter(val string) Predicate {
	lower := strings.ToLower(val)
	return func(e events.MemEvent) bool {
		switch lower {
		case "heap":
			return e.Kind == events.KindAlloc || e.Kind == events.KindDealloc
		case "stack":
			return e.Kind == events.KindStackGrow
		case "gc":
			return e.Kind == events.KindGCPause || e.Kind == events.KindGCResume
		default:
			return strings.ToLower(e.Kind.String()) == lower
		}
	}
}

func srcFilter(pattern string) Predicate {
	return func(e events.MemEvent) bool {
		ok, _ := filepath.Match(strings.ToLower(pattern), strings.ToLower(e.SourceFile))
		return ok
	}
}

