package symbol

import (
	"bufio"
	"debug/elf"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mbergo/memscope/internal/events"
)

// ReadMaps parses /proc/<pid>/maps and returns all memory regions.
func ReadMaps(pid int) ([]events.MemRegion, error) {
	path := fmt.Sprintf("/proc/%d/maps", pid)
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var regions []events.MemRegion
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		r, err := parseMapsLine(line)
		if err != nil {
			continue // skip malformed lines
		}
		regions = append(regions, r)
	}
	return regions, scanner.Err()
}

// parseMapsLine parses a single line from /proc/<pid>/maps.
// Format: start-end perms offset dev inode [name]
// Example: 7f3a4c000000-7f3a4c001000 rw-p 00000000 00:00 0   [heap]
func parseMapsLine(line string) (events.MemRegion, error) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return events.MemRegion{}, fmt.Errorf("too few fields: %q", line)
	}

	// Parse address range
	addrParts := strings.SplitN(fields[0], "-", 2)
	if len(addrParts) != 2 {
		return events.MemRegion{}, fmt.Errorf("bad address range: %q", fields[0])
	}
	start, err := strconv.ParseUint(addrParts[0], 16, 64)
	if err != nil {
		return events.MemRegion{}, err
	}
	end, err := strconv.ParseUint(addrParts[1], 16, 64)
	if err != nil {
		return events.MemRegion{}, err
	}

	perms := fields[1]
	offset, _ := strconv.ParseUint(fields[2], 16, 64)
	dev := fields[3]
	inode, _ := strconv.ParseUint(fields[4], 10, 64)

	var name string
	if len(fields) >= 6 {
		name = fields[5]
	}

	kind := classifyRegion(perms, name)

	return events.MemRegion{
		Start:  start,
		End:    end,
		Perms:  perms,
		Offset: offset,
		Dev:    dev,
		Inode:  inode,
		Name:   name,
		Kind:   kind,
	}, nil
}

func classifyRegion(perms, name string) events.RegionKind {
	switch name {
	case "[heap]":
		return events.RegionHeap
	case "[stack]":
		return events.RegionStack
	case "[vdso]":
		return events.RegionVDSO
	case "[vvar]":
		return events.RegionVvar
	}
	if strings.HasPrefix(name, "[stack:") {
		return events.RegionStack
	}

	if len(perms) < 3 {
		return events.RegionOther
	}

	// Guard pages: no permissions
	if perms == "---p" || perms == "---s" {
		return events.RegionGuard
	}
	// Executable: text segment
	if perms[2] == 'x' {
		return events.RegionText
	}
	// Named file mapping
	if name != "" {
		return events.RegionMmap
	}
	// Anonymous rw: BSS or heap-like
	if perms[0] == 'r' && perms[1] == 'w' {
		return events.RegionBSS
	}

	return events.RegionOther
}

// Lang identifies the target process language by scanning ELF symbols.
type Lang int

const (
	LangUnknown Lang = iota
	LangGo
	LangRust
)

func (l Lang) String() string {
	switch l {
	case LangGo:
		return "go"
	case LangRust:
		return "rust"
	default:
		return "unknown"
	}
}

// DetectLang inspects /proc/<pid>/exe to determine if it is a Go or Rust binary.
func DetectLang(pid int) (Lang, error) {
	exePath := fmt.Sprintf("/proc/%d/exe", pid)
	f, err := elf.Open(exePath)
	if err != nil {
		return LangUnknown, fmt.Errorf("elf.Open %s: %w", exePath, err)
	}
	defer f.Close()

	syms, err := f.Symbols()
	if err != nil {
		// Try dynamic symbols as fallback
		syms, err = f.DynamicSymbols()
		if err != nil {
			return LangUnknown, nil
		}
	}

	for _, sym := range syms {
		switch sym.Name {
		case "runtime.mallocgc":
			return LangGo, nil
		case "__rg_alloc", "__rust_alloc":
			return LangRust, nil
		}
	}
	return LangUnknown, nil
}
