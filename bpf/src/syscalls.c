// SPDX-License-Identifier: GPL-2.0
// eBPF raw_tracepoint program for syscall tracing
//
// Attaches to raw_tracepoint/sys_enter and raw_tracepoint/sys_exit.
// Filters by target PID (set via the target_pid BPF array map).
//
// Each completed syscall emits one raw_syscall_event to the ring buffer,
// recording: nr, pid, tid, timestamp, duration, and return value.

#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>

// --------------------------------------------------------------------------
// Event struct (must match events.RawSyscallEvent in Go)
// --------------------------------------------------------------------------
struct raw_syscall_event {
    __u64 timestamp_ns;   // entry timestamp
    __u64 duration_ns;    // entry→exit elapsed time
    __s64 ret;            // return value (from exit tracepoint)
    __u32 pid;
    __u32 tid;
    __u32 nr;             // syscall number
    __u32 _pad;
};

// --------------------------------------------------------------------------
// Maps
// --------------------------------------------------------------------------

// Scratch: stores entry timestamp keyed by tid
struct syscall_scratch {
    __u64 timestamp_ns;
    __u32 nr;
    __u32 _pad;
};

struct {
    __uint(type, BPF_MAP_TYPE_HASH);
    __uint(max_entries, 65536);
    __type(key,   __u32);   // tid
    __type(value, struct syscall_scratch);
} syscall_scratch_map SEC(".maps");

// target_pid[0]: PID to filter (0 = capture all)
struct {
    __uint(type, BPF_MAP_TYPE_ARRAY);
    __uint(max_entries, 1);
    __type(key,   __u32);
    __type(value, __u32);
} target_pid SEC(".maps");

// Ring buffer: completed syscall events → user space
struct {
    __uint(type, BPF_MAP_TYPE_RINGBUF);
    __uint(max_entries, 512 * 1024);
} syscall_events SEC(".maps");

// --------------------------------------------------------------------------
// Helper: check if this PID should be traced
// --------------------------------------------------------------------------
static __always_inline int should_trace(__u32 pid) {
    __u32 key = 0;
    __u32 *tpid = bpf_map_lookup_elem(&target_pid, &key);
    if (tpid && *tpid != 0 && *tpid != pid)
        return 0;
    return 1;
}

// --------------------------------------------------------------------------
// raw_tracepoint/sys_enter
// ctx->args[0] = struct pt_regs * (syscall args)
// ctx->args[1] = syscall number
// --------------------------------------------------------------------------
SEC("raw_tracepoint/sys_enter")
int tp_sys_enter(struct bpf_raw_tracepoint_args *ctx) {
    __u64 tgid_pid = bpf_get_current_pid_tgid();
    __u32 pid = (__u32)(tgid_pid >> 32);
    __u32 tid = (__u32)tgid_pid;

    if (!should_trace(pid))
        return 0;

    __u64 nr = ctx->args[1];

    struct syscall_scratch scratch = {};
    scratch.timestamp_ns = bpf_ktime_get_ns();
    scratch.nr = (__u32)nr;

    bpf_map_update_elem(&syscall_scratch_map, &tid, &scratch, BPF_ANY);
    return 0;
}

// --------------------------------------------------------------------------
// raw_tracepoint/sys_exit
// ctx->args[0] = struct pt_regs *
// ctx->args[1] = return value
// --------------------------------------------------------------------------
SEC("raw_tracepoint/sys_exit")
int tp_sys_exit(struct bpf_raw_tracepoint_args *ctx) {
    __u64 tgid_pid = bpf_get_current_pid_tgid();
    __u32 pid = (__u32)(tgid_pid >> 32);
    __u32 tid = (__u32)tgid_pid;

    if (!should_trace(pid))
        return 0;

    struct syscall_scratch *scratch = bpf_map_lookup_elem(&syscall_scratch_map, &tid);
    if (!scratch)
        return 0;

    __u64 now  = bpf_ktime_get_ns();
    __s64 ret  = (__s64)ctx->args[1];

    struct raw_syscall_event *ev = bpf_ringbuf_reserve(&syscall_events, sizeof(*ev), 0);
    if (!ev) {
        bpf_map_delete_elem(&syscall_scratch_map, &tid);
        return 0;
    }

    ev->timestamp_ns = scratch->timestamp_ns;
    ev->duration_ns  = now - scratch->timestamp_ns;
    ev->nr           = scratch->nr;
    ev->pid          = pid;
    ev->tid          = tid;
    ev->ret          = ret;
    ev->_pad         = 0;

    bpf_map_delete_elem(&syscall_scratch_map, &tid);
    bpf_ringbuf_submit(ev, 0);
    return 0;
}

char LICENSE[] SEC("license") = "GPL";
