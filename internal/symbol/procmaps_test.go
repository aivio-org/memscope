package symbol

import (
	"testing"

	"github.com/mbergo/memscope/internal/events"
)

// parseMapsLine is an internal function; we test it indirectly via exported behavior.
// For unit testing we expose a helper via a test file in the same package.

func TestClassifyRegion(t *testing.T) {
	cases := []struct {
		line string
		kind events.RegionKind
	}{
		{"7f3a4c000000-7f3a4c001000 rw-p 00000000 00:00 0   [heap]", events.RegionHeap},
		{"7fff5c000000-7fff5c200000 rw-p 00000000 00:00 0   [stack]", events.RegionStack},
		{"7f3a4d000000-7f3a4d001000 r-xp 00000000 08:01 12345 /usr/lib/libc.so.6", events.RegionText},
		{"7f3a4e000000-7f3a4e001000 ---p 00000000 00:00 0", events.RegionGuard},
		{"7f3a4f000000-7f3a4f001000 r--p 00000000 08:01 12345 /proc/maps", events.RegionMmap},
		{"7f3a50000000-7f3a50001000 rw-p 00000000 00:00 0   [vdso]", events.RegionVDSO},
	}

	for _, tc := range cases {
		r, err := parseMapsLine(tc.line)
		if err != nil {
			t.Errorf("parseMapsLine(%q): unexpected error: %v", tc.line, err)
			continue
		}
		if r.Kind != tc.kind {
			t.Errorf("line %q: expected kind %v, got %v", tc.line, tc.kind, r.Kind)
		}
	}
}

func TestMemRegionSize(t *testing.T) {
	r := events.MemRegion{Start: 0x1000, End: 0x3000}
	if r.Size() != 0x2000 {
		t.Errorf("expected 0x2000, got %x", r.Size())
	}
}
