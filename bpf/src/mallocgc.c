// SPDX-License-Identifier: GPL-2.0
// eBPF uprobes for Go runtime.mallocgc
//
// Attaches:
//   uprobe    on runtime.mallocgc entry → captures (size, goroutine_id)
//   uretprobe on runtime.mallocgc exit  → captures return address (allocated ptr)
//
// The entry probe stores partial data in a scratch map keyed by tgid_pid.
// The uretprobe completes the event and emits it to the ring buffer.
//
// Go ABI note: Go 1.17+ uses register-based ABI on amd64. The first argument
// (size uintptr) is passed in AX, not the C-ABI rdi/PARM1. We access ctx->ax
// directly for correct Go-ABI register reads.

#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>

// Minimal amd64 pt_regs definition — avoids needing vmlinux.h or asm/ptrace.h.
// Field order matches the Linux kernel's struct pt_regs for x86-64.
struct pt_regs {
    unsigned long r15, r14, r13, r12;
    unsigned long rbp, rbx;
    unsigned long r11, r10, r9, r8;
    unsigned long ax;   // Go ABI: arg0 (size) on entry, return value on exit
    unsigned long cx, dx, si, di;
    unsigned long orig_ax;
    unsigned long ip, cs, flags, sp, ss;
};

// --------------------------------------------------------------------------
// Event struct (must match pipeline.RawAllocEvent in Go)
// --------------------------------------------------------------------------
struct raw_alloc_event {
    __u64 addr;
    __u64 size;
    __u64 goroutine_id;
    __u64 timestamp_ns;
};

// --------------------------------------------------------------------------
// Maps
// --------------------------------------------------------------------------

struct alloc_entry {
    __u64 size;
    __u64 goroutine_id;
};

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 65536);
    __type(key,   __u64);
    __type(value, struct alloc_entry);
} alloc_scratch SEC(".maps");

struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 256 * 1024);
} events SEC(".maps");

// --------------------------------------------------------------------------
// Helper: goroutine ID surrogate (Phase 1 uses tgid as approximation)
// --------------------------------------------------------------------------
static __always_inline __u64 get_goroutine_id(void) {
    return bpf_get_current_pid_tgid() & 0xFFFFFFFF;
}

// --------------------------------------------------------------------------
// Uprobe: runtime.mallocgc(size uintptr, typ *_type, needzero bool)
// Go register ABI (amd64): size → AX
// --------------------------------------------------------------------------
SEC("uprobe/runtime.mallocgc")
int uprobe_mallocgc_entry(struct pt_regs *ctx) {
    __u64 key = bpf_get_current_pid_tgid();

    struct alloc_entry entry = {};
    // Read size from AX (Go register ABI, not C-ABI rdi)
    bpf_probe_read_kernel(&entry.size, sizeof(entry.size), &ctx->ax);
    entry.goroutine_id = get_goroutine_id();

    bpf_map_update_elem(&alloc_scratch, &key, &entry, BPF_ANY);
    return 0;
}

// --------------------------------------------------------------------------
// Uretprobe: runtime.mallocgc returns allocated pointer in AX.
// --------------------------------------------------------------------------
SEC("uretprobe/runtime.mallocgc")
int uretprobe_mallocgc_exit(struct pt_regs *ctx) {
    __u64 key = bpf_get_current_pid_tgid();

    struct alloc_entry *entry = bpf_map_lookup_elem(&alloc_scratch, &key);
    if (!entry)
        return 0;

    __u64 addr = 0;
    bpf_probe_read_kernel(&addr, sizeof(addr), &ctx->ax);
    if (addr == 0) {
        bpf_map_delete_elem(&alloc_scratch, &key);
        return 0;
    }

    struct raw_alloc_event *ev = bpf_ringbuf_reserve(&events, sizeof(*ev), 0);
    if (!ev) {
        bpf_map_delete_elem(&alloc_scratch, &key);
        return 0;
    }

    ev->addr         = addr;
    ev->size         = entry->size;
    ev->goroutine_id = entry->goroutine_id;
    ev->timestamp_ns = bpf_ktime_get_ns();

    bpf_map_delete_elem(&alloc_scratch, &key);
    bpf_ringbuf_submit(ev, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
