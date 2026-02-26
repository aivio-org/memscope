# MemScope

Real-time memory profiler for Go and Rust processes. Attaches to a live process with zero code changes and visualizes heap allocations, stack growth, goroutine activity, and syscalls — all driven by eBPF uprobes running in the Linux kernel.

```
┌──── Allocation Timeline (55%) ────────────┬──── Memory · Heap & Stack (45%) ─────┐
│ ▲ alloc  4.4MB/s                    █▁    │ ╔════════════════════════╦═╗          │
│                                           │ ║████████░░░░░░░░░░░░░░░░║ ║          │
│ ▼ dealloc 1B/s                           │ ╚════════════════════════╩═╝          │
│ -10s  -20s  -30s  -40s  -50s  -60s      │ HEAP   53%  1.2MB / 2.3MB            │
├──── Goroutine Allocs (40%) ───────────────┼──── [Event Log]  Syscalls (60%) ──────┤
│ ╭─ goroutine 3 ── 20 allocs  673KB ─╮   │ [+00:00:01.058] alloc  62.5KB  g:3    │
│ │  ├ 0xc000012000  39.8KB [unnamed] │   │ [+00:00:01.063] alloc  49.6KB  g:3    │
│ │  ├ 0xc000034000   8.0KB bufio     │   │ [+00:00:01.068] alloc  19.4KB  g:5    │
│ │     … 15 more                     │   │                                        │
│ ╰────────────────────────────────────╯   │                                        │
└──────────────────────────────────────────┴────────────────────────────────────────┘
  F2 filter  tab focus  s syscalls  space freeze  c clear  q quit
```

---

## Table of Contents

1. [Features](#features)
2. [Requirements](#requirements)
3. [Quick Start](#quick-start)
4. [Installation](#installation)
5. [Architecture](#architecture)
6. [TUI Reference](#tui-reference)
7. [Keyboard Shortcuts](#keyboard-shortcuts)
8. [Building from Source](#building-from-source)
9. [eBPF Programs](#ebpf-programs)
10. [Test Suite](#test-suite)
11. [Packaging](#packaging)
12. [Capabilities & Security](#capabilities--security)
13. [Roadmap](#roadmap)

---

## Features

| Feature | Status |
|---|---|
| eBPF uprobe on `runtime.mallocgc` (Go heap allocs) | ✅ |
| eBPF `raw_tracepoint` syscall log (sys_enter/sys_exit) | ✅ |
| Allocation timeline sparkline (Panel A) | ✅ |
| Heap + Stack battery-style meter (Panel B) | ✅ |
| Goroutine allocation tree (Panel C) | ✅ |
| Event log + Syscall tab (Panel E) | ✅ |
| `/proc/<pid>/maps` memory region visualizer | ✅ |
| Filter bar — `type:`, `size:`, `kind:`, `src:` predicates | ✅ |
| Zero code changes to target process | ✅ |
| Single static binary with embedded eBPF bytecode | ✅ |
| Rust `__rg_alloc`/`__rg_dealloc` probes | 🔜 Phase 3 |
| DWARF type name resolution | 🔜 Phase 3 |
| Pointer graph panel | 🔜 Phase 3 |
| JSON snapshot export | 🔜 Phase 4 |
| Theme TOML loader | 🔜 Phase 4 |

---

## Requirements

### Runtime

| Requirement | Minimum | Notes |
|---|---|---|
| Linux kernel | **5.8** | eBPF ring buffer (`BPF_MAP_TYPE_RINGBUF`) |
| Architecture | **x86-64** | arm64 support planned Phase 3 |
| Capabilities | `CAP_BPF`, `CAP_PERFMON`, `CAP_SYS_PTRACE` | via `setcap` or `sudo` |
| Terminal | 256-color | truecolor recommended |

### Build-time (only needed to regenerate eBPF bytecode)

| Tool | Version | Purpose |
|---|---|---|
| Go | ≥ 1.21 | Compile the Go binary |
| clang / LLVM | ≥ 14 | Compile `.c` → BPF bytecode |
| libbpf headers | bundled | Included via kernel-headers path |
| bpf2go | latest | `go install github.com/cilium/ebpf/cmd/bpf2go@latest` |

The pre-compiled eBPF object files (`*.o`) are committed — you **do not** need clang to build the Go binary.

---

## Quick Start

```bash
# 1. Build
make build

# 2. Grant capabilities (one-time, avoids sudo)
sudo make setcap

# 3. Attach to a running Go process
./memscope attach --pid $(pgrep myservice)

# 4. Or spawn + attach in one command
./memscope run -- ./mybinary --flag value
```

To run without `setcap`:
```bash
sudo ./memscope attach --pid <PID>
```

---

## Installation

### From source (recommended)

```bash
git clone https://github.com/mbergo/memscope
cd memscope

make build                           # produces ./memscope
sudo make install                    # installs to /usr/local/bin/memscope + setcap
```

### Debian package

```bash
make deb                             # produces build/memscope_0.2.0_amd64.deb
sudo dpkg -i build/memscope_0.2.0_amd64.deb
```

### Docker (ephemeral profiler)

```bash
make docker-build                    # builds ghcr.io/mbergo/memscope:latest

# Attach to a host process from inside the container
docker run --rm -it \
  --pid=host \
  --cap-add=BPF \
  --cap-add=PERFMON \
  --cap-add=SYS_PTRACE \
  --privileged \
  ghcr.io/mbergo/memscope:latest \
  attach --pid $(pgrep myservice)
```

---

## Architecture

### Data Flow

```
Target Process
  └─ runtime.mallocgc (Go heap alloc)
       └─ eBPF uprobe + uretprobe  (kernel space)
            └─ BPF_MAP_TYPE_RINGBUF
                 └─ gobpf.Probe.readMallocLoop()
                      └─ pipeline.Normalize()    ← converts BPF ktime → wall clock
                           └─ pipeline.Deduplicator  ← drops alloc+free < 1ms
                                └─ pipeline.RingBuffer (65536 events)
                                     └─ tui.Model tick (≤30 fps)
                                          ├─ PanelA  timeline sparkline
                                          ├─ PanelB  battery + address bar
                                          ├─ PanelC  goroutine alloc tree
                                          └─ PanelE  event log

System Calls (all processes, PID-filtered)
  └─ raw_tracepoint/sys_enter + sys_exit  (kernel space)
       └─ BPF_MAP_TYPE_RINGBUF (syscall_events)
            └─ gobpf.Probe.readSyscallLoop()
                 └─ tui.Model tick → PanelE syscall tab
```

### Directory Layout

```
memscope/
├── cmd/memscope/
│   └── main.go              Cobra CLI — attach, run, version subcommands
├── internal/
│   ├── events/
│   │   └── types.go         MemEvent, SyscallEvent, EventKind, MemRegion, RegionKind
│   ├── agent/
│   │   ├── probe.go         Probe interface: Start() → (memCh, sysCh, err)
│   │   ├── ebpf_linux.go    Linux constructor → gobpf.Probe
│   │   ├── ebpf_stub.go     Non-Linux stub → error
│   │   └── gobpf/
│   │       └── ebpf.go      Real eBPF probe: mallocgc uprobes + syscall raw_tp
│   ├── bpf/
│   │   ├── gen.go           //go:generate directives for bpf2go
│   │   ├── mallocgc_bpfel.go  Generated Go bindings (mallocgc)
│   │   ├── mallocgc_bpfel.o   Embedded BPF bytecode
│   │   ├── syscalls_bpfel.go  Generated Go bindings (syscalls)
│   │   └── syscalls_bpfel.o   Embedded BPF bytecode
│   ├── pipeline/
│   │   ├── ringbuffer.go    Thread-safe 65536-entry circular buffer
│   │   ├── normalizer.go    RawAllocEvent → MemEvent; BPF ktime → wall clock
│   │   ├── deduplicator.go  Suppresses alloc+free pairs < 1ms apart
│   │   └── pipeline.go      Wires normalizer → deduplicator → ring buffer
│   ├── symbol/
│   │   └── procmaps.go      /proc/<pid>/maps parser + language detection
│   ├── tui/
│   │   ├── model.go         bubbletea root model — 4-panel layout, event routing
│   │   ├── keymap.go        All key bindings
│   │   ├── filter.go        Filter bar model + predicate parser
│   │   └── panels/
│   │       ├── panel_a.go   Allocation timeline sparkline (60s window)
│   │       ├── panel_b.go   Battery meters (heap/stack) + address bar
│   │       ├── panel_c.go   Goroutine allocation tree (live, scrollable)
│   │       └── panel_e.go   Event log + syscall tab (1000-entry ring)
│   └── theme/
│       └── theme.go         Dracula color palette + Theme struct
├── bpf/src/
│   ├── mallocgc.c           eBPF uprobe/uretprobe for runtime.mallocgc
│   └── syscalls.c           eBPF raw_tracepoint sys_enter/sys_exit
├── Makefile
├── Dockerfile
└── go.mod
```

### Key Design Decisions

**eBPF-first, zero-instrument** — uprobes attach to the target binary's symbol table; no recompilation, no agent library, no wrapper.

**Go register ABI** — Go 1.17+ passes arguments in registers (`AX`, `BX`, `CX`) not stack/C-ABI (`rdi`, `rsi`). `mallocgc.c` reads `ctx->ax` directly for the `size` argument and the return pointer. `PT_REGS_PARM1` (which maps to `rdi`) is **not** used for Go uprobes.

**Atomic timestamp reference** — `pipeline.Normalizer` uses `atomic.Pointer[bootRef]` to store the BPF ktime ↔ wall clock pair as a single word. This prevents the torn-update window that would arise from updating two fields under a mutex held for each event.

**Bubbletea Elm architecture** — all panel methods use **value receivers** and return new copies of the panel. No shared mutable state across render frames. The `probeStartedMsg` pattern carries the `context.CancelFunc` back to the root model through the Update loop rather than closing over it.

**Deduplication** — alloc+free pairs separated by less than 1ms are suppressed. `KindStackGrow` events always pass through without entering the inflight map (stacks have no paired dealloc event).

---

## TUI Reference

### Panel A — Allocation Timeline

Displays a rolling 60-second sparkline of allocation and deallocation throughput.

- **Y-axis**: auto-scaled (B/s → KB/s → MB/s → GB/s)
- **Green bars (▲)**: `KindAlloc` bytes per second
- **Red bars (▼)**: `KindDealloc` bytes per second
- **Orange marks**: GC pause events
- **Zoom**: `+`/`-` to narrow/widen the time window; `r` to reset
- **Focus keys**: `↑`/`↓` to scroll the Y-axis

### Panel B — Memory · Heap & Stack

Shows two **battery-style** fill meters plus an address-space bar.

**Battery color scale:**

| Fill | Color | Meaning |
|---|---|---|
| 0–49% | Green | Healthy headroom |
| 50–79% | Yellow | Approaching limit |
| 80–100% | Red | Under pressure |

The **address bar** is a proportional horizontal strip where each colored band represents one `/proc/<pid>/maps` region:

| Color | Region type |
|---|---|
| Green | Heap (`[heap]`) |
| Blue | Stack (`[stack]`) |
| Yellow | Text (r-xp code segment) |
| Purple | Anonymous mmap (BSS, data) |
| Gray | Guard pages (---p) |
| Dim | vDSO, vvar, other |

Live allocations appear as `·` dots inside the heap band.

**Focus keys**: `j`/`↓` and `k`/`↑` to move the region cursor.

### Panel C — Goroutine Allocation Tree

Groups live allocations by goroutine ID. Each goroutine appears as a bordered box showing its top allocations.

```
╭─ goroutine 7 ─────────── 3 allocs  48.2 KB ─╮
│  ├ 0xc000012000   256 B  runtime.newobject
│  ├ 0xc000034000  8.0 KB  bufio.NewReader
│  └ 0xc000045000  40.0 KB [unnamed]
╰──────────────────────────────────────────────╯
```

- Up to **200 goroutine groups** tracked simultaneously (oldest evicted)
- Up to **20 allocations per goroutine** retained (ring, oldest dropped)
- Up to **5 allocations shown** per box (truncated with `… N more`)
- Type names resolved from DWARF in Phase 3 (currently `[unnamed]`)

**Focus keys**: `j`/`↓` and `k`/`↑` to scroll through groups.

### Panel E — Event Log / Syscall Tab

Dual-tab panel toggled with `s`:

**Event Log tab** — chronological `MemEvent` stream:
```
[+00:01.234]  alloc      8192B   runtime.mallocgc   goroutine:3
[+00:01.240]  dealloc    8192B   -                  goroutine:3
[+00:05.001]  gc_pause   -       -                  goroutine:0
```

**Syscalls tab** — completed syscall pairs (entry + exit):
```
[15:04:05.123]  mmap               pid:12345  tid:12348  ret:140234...  0.12ms
[15:04:05.124]  futex              pid:12345  tid:12346  ret:0          2.40µs
[15:04:05.125]  read               pid:12345  tid:12347  ret:-11        8ns
```

Return values < 0 are highlighted in red (kernel error codes).

**Focus keys**: mouse wheel or `↑`/`↓` to scroll; auto-scrolls to bottom unless manually scrolled.

---

## Keyboard Shortcuts

| Key | Scope | Action |
|---|---|---|
| `Tab` | Global | Cycle focus forward (A → B → C → E → A) |
| `Shift+Tab` | Global | Cycle focus backward |
| `F2` | Global | Open / close filter bar |
| `Space` | Global | Freeze / unfreeze event stream |
| `c` | Global | Clear current panel's entries |
| `q` / `Ctrl+C` | Global | Quit |
| `s` | Panel E | Toggle Event Log / Syscalls tab |
| `+` / `=` | Panel A | Zoom in timeline |
| `-` | Panel A | Zoom out timeline |
| `r` | Panel A | Reset zoom to 60s window |
| `j` / `↓` | Panel B, C | Scroll down |
| `k` / `↑` | Panel B, C | Scroll up |
| `Enter` / `Esc` | Filter bar | Apply / close filter |

### Filter Syntax (F2)

Predicates can be combined with `AND`:

```
type:bufio.NewReader
size:>4096
size:1024-65536
kind:heap
kind:stack
kind:gc
src:net/http
type:*http.Request AND size:>1024
```

---

## Building from Source

### Prerequisites

```bash
# Go 1.21+
go version

# CGO is required (eBPF object embedding uses cgo indirectly via cilium/ebpf)
# No external C library is needed at link time — only Go stdlib + embedded .o files

# Optional: clang (only to regenerate eBPF bytecode from .c source)
clang --version
```

### Build

```bash
# Standard build (uses pre-compiled eBPF bytecode, no clang needed)
CGO_ENABLED=1 go build -o memscope ./cmd/memscope

# Or via Makefile
make build
```

### Regenerating eBPF Bytecode

Only needed after editing `bpf/src/*.c`:

```bash
# Install bpf2go
go install github.com/cilium/ebpf/cmd/bpf2go@latest

# Compile .c → .o + generate Go bindings
cd internal/bpf && go generate gen.go

# Verify
ls -lh internal/bpf/*.o
```

The clang flags used:
```
-target bpf -O2 -g -D__TARGET_ARCH_x86
-I/usr/src/linux-headers-$(uname -r)/tools/bpf/resolve_btfids/libbpf/include
-I/usr/include
-I/usr/include/x86_64-linux-gnu
```

**No `vmlinux.h` required.** `struct pt_regs` is defined inline in `mallocgc.c` for portability.

### Cross-compile notes

eBPF programs must target the host kernel. Cross-compilation of the Go binary itself is possible but the `.o` files must be compiled on/for the target architecture.

---

## eBPF Programs

### `bpf/src/mallocgc.c`

| Section | Function | Description |
|---|---|---|
| `uprobe/runtime.mallocgc` | `uprobe_mallocgc_entry` | Captures `size` from `AX` (Go ABI); stores in `alloc_scratch` keyed by `tgid_pid` |
| `uretprobe/runtime.mallocgc` | `uretprobe_mallocgc_exit` | Reads return pointer from `AX`; emits `raw_alloc_event` to ring buffer |

**Maps:**

| Map | Type | Key | Value | Purpose |
|---|---|---|---|---|
| `alloc_scratch` | `HASH` | `u64` tgid_pid | `struct alloc_entry` | Entry↔exit correlation |
| `events` | `RINGBUF` | — | `raw_alloc_event` | Completed alloc events → user space |

**Event struct** (32 bytes, little-endian):
```c
struct raw_alloc_event {
    u64 addr;           // allocated pointer (return value)
    u64 size;           // allocation size in bytes
    u64 goroutine_id;   // tgid surrogate (real goid in Phase 3)
    u64 timestamp_ns;   // bpf_ktime_get_ns() at exit
};
```

### `bpf/src/syscalls.c`

| Section | Function | Description |
|---|---|---|
| `raw_tracepoint/sys_enter` | `tp_sys_enter` | Records entry timestamp + syscall nr in `syscall_scratch_map` |
| `raw_tracepoint/sys_exit` | `tp_sys_exit` | Computes duration; emits `raw_syscall_event` to ring buffer |

**Maps:**

| Map | Type | Key | Value | Purpose |
|---|---|---|---|---|
| `syscall_scratch_map` | `HASH` | `u32` tid | `syscall_scratch` | Entry timestamp per thread |
| `target_pid` | `ARRAY` | `u32` 0 | `u32` pid | PID filter (0 = capture all) |
| `syscall_events` | `RINGBUF` | — | `raw_syscall_event` | Completed syscall events → user space |

**Event struct** (40 bytes, little-endian):
```c
struct raw_syscall_event {
    u64 timestamp_ns;   // entry time (bpf_ktime_get_ns)
    u64 duration_ns;    // exit_ns − entry_ns
    s64 ret;            // return value
    u32 pid;
    u32 tid;
    u32 nr;             // syscall number
    u32 _pad;
};
```

---

## Test Suite

Tests live in the packages they cover. Run all:

```bash
go test ./...
```

Run with race detector:

```bash
go test -race ./...
```

### Current Test Status

```
ok   github.com/mbergo/memscope/internal/pipeline   (0.003s)  10 tests
ok   github.com/mbergo/memscope/internal/symbol     (0.002s)   2 tests
```

### `internal/pipeline` — 10 tests

| Test | Package | What it verifies |
|---|---|---|
| `TestDeduplicator_AllocPassthrough` | pipeline | `KindAlloc` events always forward; `keep=true` |
| `TestDeduplicator_ShortLivedDropped` | pipeline | alloc+free pair < 1ms → dealloc suppressed |
| `TestDeduplicator_LongLivedKept` | pipeline | alloc+free pair > 1ms → dealloc forwarded |
| `TestDeduplicator_GCAlwaysKept` | pipeline | `KindGCPause` passes through unconditionally |
| `TestDeduplicator_StackGrowNotTracked` | pipeline | `KindStackGrow` passes through; inflight map stays empty (100 events) |
| `TestDeduplicator_Flush` | pipeline | Stale allocs > TTL are evicted; inflight count becomes 0 |
| `TestNormalizer_BasicConversion` | pipeline | Addr, Size, GoroutineID fields copied verbatim |
| `TestNormalizer_TimestampConversion` | pipeline | BPF ns → wall clock within ±50ms of expected |
| `TestNormalizer_ConcurrentNoRace` | pipeline | 4 writer + 8 reader goroutines; no data race |
| `TestNormalizer_IndependentInstances` | pipeline | Two `*Normalizer` with different refs produce different timestamps |
| `TestRingBuffer_PushDrain` | pipeline | Push N, drain N; FIFO order preserved |
| `TestRingBuffer_Overflow` | pipeline | Oldest item dropped when capacity exceeded |
| `TestRingBuffer_DrainEmpty` | pipeline | Drain on empty returns nil slice |
| `TestRingBuffer_Subscribe` | pipeline | Fan-out channel receives every pushed event |

### `internal/symbol` — 2 tests

| Test | Package | What it verifies |
|---|---|---|
| `TestClassifyRegion` | symbol | `/proc/maps` lines correctly classified as heap/stack/text/bss/guard/mmap |
| `TestMemRegionSize` | symbol | `MemRegion.Size()` = `End − Start` |

### Packages without tests (integration/eBPF require live kernel)

| Package | Reason |
|---|---|
| `internal/agent/gobpf` | Requires `CAP_BPF` + a live Go process |
| `internal/bpf` | eBPF object loading requires kernel |
| `internal/tui` | Terminal rendering requires TTY |
| `internal/events` | Pure type definitions |
| `internal/theme` | Pure color constants |

---

## Packaging

### Debian package

```bash
make deb
# Output: build/memscope_0.2.0_amd64.deb

sudo dpkg -i build/memscope_0.2.0_amd64.deb
memscope version
```

The `.deb` installs:
- `/usr/bin/memscope` — binary with `cap_bpf,cap_perfmon,cap_sys_ptrace+eip` set
- `/lib/systemd/system/memscope.service` — optional systemd unit
- `/usr/share/doc/memscope/` — changelog + copyright

### Docker image

```bash
make docker-build
# Image: ghcr.io/mbergo/memscope:latest  (~20MB, distroless/static)

# Run against a host PID
docker run --rm -it \
  --pid=host \
  --cap-add=BPF --cap-add=PERFMON --cap-add=SYS_PTRACE \
  ghcr.io/mbergo/memscope:latest \
  attach --pid <PID>
```

The Docker image uses `gcr.io/distroless/static` as the base — no shell, no package manager, ≈20MB total.

---

## Capabilities & Security

MemScope requires three Linux capabilities:

| Capability | Required for |
|---|---|
| `CAP_BPF` | Loading and attaching eBPF programs and maps |
| `CAP_PERFMON` | Accessing performance monitoring (needed for uprobes) |
| `CAP_SYS_PTRACE` | Reading `/proc/<pid>/exe` for uprobe attachment |

### Recommended: `setcap` (no sudo at runtime)

```bash
sudo setcap cap_bpf,cap_perfmon,cap_sys_ptrace+eip /usr/bin/memscope
```

### Yama `ptrace_scope`

Many distributions default to `ptrace_scope=1`, which restricts ptrace to parent/child relationships. If you see `permission denied` when attaching, either:

```bash
# Temporarily allow (reverted on reboot)
echo 0 | sudo tee /proc/sys/kernel/yama/ptrace_scope

# Or run with sudo
sudo memscope attach --pid <PID>
```

---

## Roadmap

| Phase | Scope | Status |
|---|---|---|
| **1 — MVP** | Go eBPF probes, Panel A + B + E, filter bar | ✅ Done |
| **2 — Real eBPF** | Compiled bytecode, syscall tab, goroutine graph (Panel C), battery UI | ✅ Done |
| **3 — Rust + DWARF** | `__rg_alloc`/`__rg_dealloc` probes, DWARF type resolution, real goroutine IDs, Panel D | 🔜 |
| **4 — Polish** | JSON export, theme TOML, pointer graph (Panel C v2), diff mode, CI release | 🔜 |

---

## License

GPL-2.0 — required by the eBPF programs linked against libbpf.
